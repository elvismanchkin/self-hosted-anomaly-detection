package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/promrw"
	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/series"
)

// absentRig remote-writes flat 1-minute series of anomaly_type requests (both directions
// count) and errors (only up counts) per service.
type absentRig struct {
	t   *testing.T
	s   *Server
	enc promrw.Encoder
}

var rigValue = map[string]float64{"checkout": 100, "auth": 50, "idle": 0}

func (r *absentRig) write(from, to int64, svcs ...string) {
	r.t.Helper()
	var batch []promrw.Series
	for _, svc := range svcs {
		for _, typ := range []string{"requests", "errors"} {
			x := promrw.Series{Labels: [][2]string{{"__name__", "anomaly:svc:" + typ + ":rate1m"},
				{"anomaly_type", typ}, {"service", svc}}}
			for ts := from; ts < to; ts += 60 {
				x.Samples = append(x.Samples, promrw.Sample{Value: rigValue[svc], Timestamp: ts * 1000})
			}
			batch = append(batch, x)
		}
	}
	if rec := post(r.t, r.s.Handler(), "/api/v1/write", "application/x-protobuf", r.enc.Encode(batch)); rec.Code != http.StatusNoContent {
		r.t.Fatalf("remote write: %d", rec.Code)
	}
	r.s.Evaluate(time.Now())
}

func absentKey(svc string) string {
	ls := series.Labels{{Name: "__name__", Value: "anomaly:svc:requests:rate1m"}, {Name: "anomaly_type", Value: "requests"},
		{Name: "service", Value: svc}}
	return ls.Key()
}

func absentConfig() Config {
	cfg := testConfig()
	cfg.PromSeason = time.Hour // ring: 2 h + 2 buckets
	cfg.AbsentBuckets = 3
	cfg.AbsentTTL = 24 * time.Hour
	return cfg
}

// A request series that stops while others keep arriving fires AnomalydSeriesAbsent after
// --absent-buckets; error series, series at zero and a gap in the whole feed do not. The
// finding survives a restart and resolves when data returns.
func TestSeriesAbsent(t *testing.T) {
	cfg := absentConfig()
	cfg.StateDir = t.TempDir()
	s, _ := New(cfg)
	r := &absentRig{t: t, s: s}
	base := time.Now().Unix()/60*60 - 6*3600
	all := []string{"checkout", "auth", "idle"}
	r.write(base, base+3*3600, all...)

	// Prometheus down for 10 minutes: no series arrives, so none is absent.
	t1 := base + 3*3600 + 600
	r.write(t1, t1+60, all...)
	r.write(t1+60, t1+120, all...)
	s.findMu.Lock()
	n := len(s.active) + len(s.resolved)
	s.findMu.Unlock()
	if n != 0 {
		t.Fatalf("findings after a gap in the whole feed: %d", n)
	}

	// Only auth keeps arriving: checkout requests go absent on the 3rd missing bucket.
	t2 := t1 + 120
	for i := int64(0); i < 4; i++ {
		if countKind(s, kindMetricAbsent) != 0 {
			t.Fatalf("absent after %d missing buckets", i-1)
		}
		r.write(t2+60*i, t2+60*(i+1), "auth")
	}
	f := findingBy(s, kindMetricAbsent, "service", "checkout")
	if f == nil || countKind(s, kindMetricAbsent) != 1 || f.Labels["alertname"] != "AnomalydSeriesAbsent" ||
		f.Labels["tier"] != "2" || f.Labels["severity"] != "warning" || f.Labels["anomaly_kind"] != "metric_absent" ||
		f.Labels["metric"] != "anomaly:svc:requests:rate1m" || f.Expected != 100 ||
		!f.Bucket.Equal(time.Unix(t2, 0)) || !strings.HasPrefix(f.ID, "absent(") {
		t.Fatalf("absent finding: %+v (want one, for checkout requests only)", f)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/findings?state=firing", nil))
	var fs []Finding
	if err := json.Unmarshal(rec.Body.Bytes(), &fs); err != nil || len(fs) != 1 {
		t.Fatalf("findings API: %d %v %s", len(fs), err, rec.Body)
	}
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(rec.Body.String(), `anomalyd_findings_active{kind="metric_absent"} 1`) {
		t.Fatal("/metrics: no active metric_absent finding")
	}

	// Restart: the finding keeps its start time; checkout's data returns and resolves it.
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	s2, _ := New(cfg)
	if err := s2.load(); err != nil {
		t.Fatal(err)
	}
	if f2 := findingBy(s2, kindMetricAbsent, "service", "checkout"); f2 == nil || !f2.StartsAt.Equal(f.StartsAt) {
		t.Fatalf("after restore: %+v", f2)
	}
	r2 := &absentRig{t: t, s: s2}
	t3 := t2 + 4*60
	r2.write(t3, t3+60, all...)
	if countKind(s2, kindMetricAbsent) != 1 {
		t.Fatal("resolved before checkout's data was scored")
	}
	r2.write(t3+60, t3+120, all...)
	s2.findMu.Lock()
	defer s2.findMu.Unlock()
	if len(s2.active) != 0 || len(s2.resolved) != 1 || s2.resolved[0].Kind != kindMetricAbsent || !s2.resolved[0].StartsAt.Equal(f.StartsAt) {
		t.Fatalf("active %d, resolved %+v", len(s2.active), s2.resolved)
	}
}

// After --absent-ttl the finding resolves and does not fire again; gc keeps the series while the
// finding fires, and drops it once a full ring has gone by without data and the finding is over.
func TestSeriesAbsentTTLAndGC(t *testing.T) {
	for _, ttl := range []time.Duration{30 * time.Minute, 3 * time.Hour} {
		cfg := absentConfig()
		cfg.AbsentTTL = ttl
		s, _ := New(cfg)
		r := &absentRig{t: t, s: s}
		base := time.Now().Unix()/60*60 - 6*3600
		r.write(base, base+3*3600, "checkout", "auth")
		key := absentKey("checkout")
		fired, resolved, deleted := 0, 0, 0
		for i := 1; i <= 200; i++ {
			from := base + 3*3600 + int64(i-1)*60
			r.write(from, from+60, "auth")
			n := countKind(s, kindMetricAbsent)
			exists := s.prom.Lookup(key, func(*series.Series) {})
			switch {
			case n == 1 && fired == 0:
				fired = i
			case n == 0 && fired > 0 && resolved == 0:
				resolved = i
			case n == 1 && resolved > 0:
				t.Fatalf("ttl %s: fired again at %d after resolving at %d", ttl, i, resolved)
			}
			if n == 1 && !exists {
				t.Fatalf("ttl %s: series deleted at %d while its absent finding fires", ttl, i)
			}
			if !exists && deleted == 0 {
				deleted = i
			}
		}
		// Missing from bucket 2 on: fires at 4; resolves ttl buckets after the first missing one;
		// gc after a full ring (122 buckets) without data, but not before the finding is over.
		ttlB := int(ttl / time.Minute)
		if fired != 4 || resolved != ttlB+2 || deleted != max(123, ttlB+2) {
			t.Fatalf("ttl %s: fired %d resolved %d deleted %d, want 4, %d, %d", ttl, fired, resolved, deleted, ttlB+2, max(123, ttlB+2))
		}
	}
}

// A backfilled copy that remote write never delivers (here: Prometheus adds an external label
// that is not in --ignore-labels) is not absent; only a series that arrived and stopped is.
func TestBackfillOnlySeriesNotAbsent(t *testing.T) {
	now := time.Now().Unix() / 60 * 60
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		var vals []string
		for ts := now - 3*3600; ts < now; ts += 60 {
			vals = append(vals, fmt.Sprintf(`[%d,"100"]`, ts))
		}
		fmt.Fprintf(w, `{"status":"success","data":{"result":[{"metric":{"__name__":"anomaly:svc:requests:rate1m",`+
			`"anomaly_type":"requests","service":"checkout"},"values":[%s]}]}}`, strings.Join(vals, ","))
	}))
	defer prom.Close()
	cfg := absentConfig()
	cfg.BackfillURL = prom.URL
	s, _ := New(cfg)
	if _, err := s.queryRange(context.Background(), "x", time.Unix(now-3*3600, 0), time.Unix(now, 0)); err != nil {
		t.Fatal(err)
	}
	var enc promrw.Encoder
	for i := int64(0); i < 6; i++ {
		x := promrw.Series{Labels: [][2]string{{"__name__", "anomaly:svc:requests:rate1m"}, {"anomaly_type", "requests"},
			{"cluster", "prod"}, {"service", "checkout"}}, Samples: []promrw.Sample{{Value: 100, Timestamp: (now + 60*i) * 1000}}}
		post(t, s.Handler(), "/api/v1/write", "application/x-protobuf", enc.Encode([]promrw.Series{x}))
		s.Evaluate(time.Now())
	}
	if s.prom.Len() != 2 || countKind(s, kindMetricAbsent) != 0 {
		t.Fatalf("series %d, absent findings %d; want 2 and none", s.prom.Len(), countKind(s, kindMetricAbsent))
	}
}

// Restored metric series keep the bucket of their last sample, so gc does not drop the ones the
// first remote writes after a restart have not carried yet.
func TestRestoredMetricSeriesKeptByGC(t *testing.T) {
	cfg := absentConfig()
	cfg.StateDir = t.TempDir()
	s, _ := New(cfg)
	base := time.Now().Unix()/60*60 - 3*3600
	(&absentRig{t: t, s: s}).write(base, base+3*3600, "checkout", "auth")
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	s2, _ := New(cfg)
	if err := s2.load(); err != nil {
		t.Fatal(err)
	}
	(&absentRig{t: t, s: s2}).write(base+3*3600, base+3*3600+60, "auth")
	if !s2.prom.Lookup(absentKey("checkout"), func(*series.Series) {}) {
		t.Fatal("gc dropped a restored series with recent data")
	}
}
