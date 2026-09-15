package server

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/drain"
	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/promrw"
	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/wire"
)

func testConfig() Config {
	return Config{Step: time.Minute, EvalInterval: time.Second, Grace: 0, ForBuckets: 2, Threshold: 4,
		MaxSeries: 100, PromSeason: 24 * time.Hour, PromSeasons: 1, PromLookback: time.Hour, PromRelFloor: 0.05,
		MaxLogSeries: 100, LogSeason: time.Hour, LogSeasons: 1, LogLookback: 30 * time.Minute, LogMinCount: 10,
		NoveltyWarmup: time.Hour, NoveltyMin: 5, NoveltyWindow: 15 * time.Minute, NoveltyTTL: time.Hour,
		Drain: drain.DefaultConfig(), MaxServices: 10, MessageField: "message", ServiceField: "service.name",
		LevelField: "log.level", ExportBands: true}
}

type fakeAM struct {
	mu     sync.Mutex
	alerts []amAlert
}

func (f *fakeAM) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/v2/alerts" {
		http.NotFound(w, r)
		return
	}
	var a []amAlert
	_ = json.NewDecoder(r.Body).Decode(&a)
	f.mu.Lock()
	f.alerts = append(f.alerts, a...)
	f.mu.Unlock()
}

func (f *fakeAM) last(alertname string) (amAlert, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.alerts) - 1; i >= 0; i-- {
		if f.alerts[i].Labels["alertname"] == alertname {
			return f.alerts[i], true
		}
	}
	return amAlert{}, false
}

func post(t *testing.T, h http.Handler, path, ctype string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", ctype)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// 2 days of a daily sine per service at 1m; "checkout" triples for the last `spike` minutes.
func history(end int64, spike int) []promrw.Series {
	var out []promrw.Series
	for i, svc := range []string{"auth", "checkout", "search"} {
		s := promrw.Series{Labels: [][2]string{{"__name__", "anomaly:svc:requests:rate1m"},
			{"anomaly_type", "requests"}, {"prometheus", "p1"}, {"service", svc}}}
		for t := end - 2*86400; t < end; t += 60 {
			v := 100 * float64(i+1) * (1 + 0.5*math.Sin(2*math.Pi*float64(t%86400)/86400))
			v *= 1 + 0.02*math.Sin(float64(t)/7) // deterministic wiggle
			if svc == "checkout" && t >= end-int64(spike)*60 {
				v *= 3
			}
			s.Samples = append(s.Samples, promrw.Sample{Value: v, Timestamp: t * 1000})
		}
		out = append(out, s)
	}
	return out
}

func TestRemoteWriteFindingAndAlertmanager(t *testing.T) {
	am := &fakeAM{}
	amSrv := httptest.NewServer(am)
	defer amSrv.Close()
	cfg := testConfig()
	cfg.AlertmanagerURLs = []string{amSrv.URL}
	cfg.IgnoreLabels = []string{"prometheus"}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	end := time.Now().Unix() / 60 * 60
	var enc promrw.Encoder
	if rec := post(t, h, "/api/v1/write", "application/x-protobuf", enc.Encode(history(end, 5))); rec.Code != http.StatusNoContent {
		t.Fatalf("remote write: %d %s", rec.Code, rec.Body)
	}
	if s.prom.Len() != 3 {
		t.Fatalf("series = %d, want 3 (external label dropped)", s.prom.Len())
	}
	s.Evaluate(time.Now())

	s.findMu.Lock()
	n := len(s.active)
	var f *Finding
	for _, x := range s.active {
		f = x
	}
	s.findMu.Unlock()
	if n != 1 || f.Labels["service"] != "checkout" || f.Labels["alertname"] != "AnomalydMetricAnomaly" ||
		f.Labels["tier"] != "2" || f.Labels["direction"] != "up" || f.Score < 4 {
		t.Fatalf("want one checkout finding, got %d: %+v", n, f)
	}
	a, ok := am.last("AnomalydMetricAnomaly")
	if !ok || a.Labels["service"] != "checkout" || !a.EndsAt.After(time.Now()) || a.Labels["prometheus"] != "" {
		t.Fatalf("alertmanager payload: %+v ok=%v", a, ok)
	}

	// /metrics exposes scores without anomaly_* labels (they would feed upstream band rules).
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `anomalyd_score{metric="anomaly:svc:requests:rate1m",service="checkout"}`) ||
		strings.Contains(body, "anomaly_type=") {
		t.Fatalf("unexpected /metrics:\n%s", body)
	}

	// ±Inf and future-dated samples are ignored; they must not reach findings or the data clock.
	clock := s.promClock.Load()
	bad := []promrw.Series{{Labels: [][2]string{{"__name__", "anomaly:svc:errors:ratio1m"}, {"service", "auth"}},
		Samples: []promrw.Sample{{Value: math.Inf(1), Timestamp: end * 1000}, {Value: 1, Timestamp: (end + 86400) * 1000}}}}
	if rec := post(t, h, "/api/v1/write", "application/x-protobuf", enc.Encode(bad)); rec.Code != http.StatusNoContent {
		t.Fatalf("remote write: %d", rec.Code)
	}
	if s.promClock.Load() != clock {
		t.Fatalf("future sample moved the clock: %d -> %d", clock, s.promClock.Load())
	}
	s.Evaluate(time.Now())
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/findings", nil))
	var fs []Finding
	if err := json.Unmarshal(rec.Body.Bytes(), &fs); err != nil || len(fs) != 1 {
		t.Fatalf("findings after bad samples: %d %v", len(fs), err)
	}

	// Remote write 2.0 is refused with 415 so Prometheus reports it clearly.
	if rec := post(t, h, "/api/v1/write", "application/x-protobuf;proto=io.prometheus.write.v2.Request", nil); rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("rw2: %d", rec.Code)
	}
}

func push(t *testing.T, h http.Handler, items ...wire.Item) {
	t.Helper()
	b, _ := json.Marshal(wire.Push{Agent: "host-a", Step: 60, Items: items})
	if rec := post(t, h, "/api/v1/templates", "application/json", b); rec.Code != http.StatusNoContent {
		t.Fatalf("push: %d %s", rec.Code, rec.Body)
	}
}

func TestNoveltyAndRestore(t *testing.T) {
	cfg := testConfig()
	cfg.StateDir = t.TempDir()
	s, _ := New(cfg)
	h := s.Handler()
	now := time.Now().Unix() / 60 * 60
	known := wire.Item{Service: "checkout", Template: "Payment authorized order=<NUM> provider=stripe latency_ms=<NUM>",
		Level: "info", Counts: [][2]int64{{now, 50}}}
	push(t, h, known)
	s.logStart.Store(time.Now().Add(-2 * time.Hour).Unix()) // warm-up over

	// Another agent's spelling of the same template must not count as new.
	push(t, h, wire.Item{Service: "checkout", Template: "Payment authorized order=<NUM> provider=paypal latency_ms=<NUM>",
		Level: "info", Counts: [][2]int64{{now, 40}}})
	// A genuinely new error template.
	push(t, h, wire.Item{Service: "checkout", Template: "ERROR db pool exhausted: timeout after <NUM> (active=<NUM> idle=<NUM>)",
		Level: "error", Counts: [][2]int64{{now, 3}}})
	s.Evaluate(time.Now())
	if got := countKind(s, kindLogNew); got != 0 {
		t.Fatalf("fired below --novelty-min-lines: %d", got)
	}
	push(t, h, wire.Item{Service: "checkout", Template: "ERROR db pool exhausted: timeout after <NUM> (active=<NUM> idle=<NUM>)",
		Level: "error", Counts: [][2]int64{{now, 4}}})
	s.Evaluate(time.Now())
	s.findMu.Lock()
	var f *Finding
	for _, x := range s.active {
		if x.Kind == kindLogNew {
			f = x
		}
	}
	s.findMu.Unlock()
	if f == nil || f.Labels["severity"] != "warning" || f.Labels["level"] != "error" ||
		!strings.HasPrefix(f.Annotations["template"], "ERROR db pool exhausted") {
		t.Fatalf("new-template finding: %+v", f)
	}
	if countKind(s, kindLogNew) != 1 {
		t.Fatalf("want exactly one new-template finding")
	}

	// Snapshot, restart: known templates stay known, novelty needs no second warm-up.
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	s2, _ := New(cfg)
	if err := s2.load(); err != nil {
		t.Fatal(err)
	}
	restored := s2.logs.Len()
	s2.Evaluate(time.Now())
	if restored == 0 || s2.logs.Len() != restored {
		t.Fatalf("restored log series lost by gc: %d -> %d", restored, s2.logs.Len())
	}
	h2 := s2.Handler()
	push(t, h2, known, wire.Item{Service: "checkout", Template: "ERROR db pool exhausted: timeout after <NUM> (active=<NUM> idle=<NUM>)",
		Level: "error", Counts: [][2]int64{{now, 20}}})
	s2.Evaluate(time.Now())
	if got := countKind(s2, kindLogNew); got != 0 {
		t.Fatalf("restored templates reported as new: %d", got)
	}
	push(t, h2, wire.Item{Service: "checkout", Template: "Refund issued order=<NUM> amount=<NUM>", Level: "info",
		Counts: [][2]int64{{now, 9}}})
	s2.Evaluate(time.Now())
	if got := countKind(s2, kindLogNew); got != 1 {
		t.Fatalf("new template after restore: %d findings", got)
	}
	rec := httptest.NewRecorder()
	h2.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/templates?service=checkout", nil))
	b, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(b), `"Payment authorized order=<NUM> <*> latency_ms=<NUM>"`) {
		t.Fatalf("merged template missing:\n%s", b)
	}
}

func TestRawLogsEndpoint(t *testing.T) {
	s, _ := New(testConfig())
	h := s.Handler()
	var b bytes.Buffer
	for i := 0; i < 30; i++ {
		b.WriteString(`{"service":{"name":"auth"},"log":{"level":"warn"},"message":"User 42 logged in from 10.0.0.` + string(rune('1'+i%9)) + `"}` + "\n")
	}
	b.WriteString("plain ERROR text line 7\n")
	if rec := post(t, h, "/api/v1/logs?service=legacy", "application/x-ndjson", b.Bytes()); rec.Code != http.StatusNoContent {
		t.Fatalf("logs: %d %s", rec.Code, rec.Body)
	}
	if got := s.stats.logLines.Load(); got != 31 {
		t.Fatalf("lines = %d", got)
	}
	if s.logs.Len() != 4 { // auth template + auth/warn volume, legacy template + legacy/error volume
		t.Fatalf("log series = %d", s.logs.Len())
	}
}

func countKind(s *Server, kind string) int {
	s.findMu.Lock()
	defer s.findMu.Unlock()
	n := 0
	for _, f := range s.active {
		if f.Kind == kind {
			n++
		}
	}
	return n
}
