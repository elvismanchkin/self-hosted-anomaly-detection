package server

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/detect"
	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/drain"
	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/logparse"
	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/promrw"
	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/series"
	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/wire"
)

const (
	kindMetric      = "metric"
	kindLogTemplate = "log_template"
	kindLogVolume   = "log_volume"
	kindLogNew      = "log_new_template"

	nameTemplate = "anomalyd_log_template_lines"
	nameVolume   = "anomalyd_log_lines"
)

// seriesInfo is the per-series detector state, guarded by the store shard lock.
type seriesInfo struct {
	kind     string
	dir      int // +1 only increases count, -1 only decreases, 0 both
	params   detect.Params
	minVol   float64
	service  string
	cache    detect.Cache
	pending  int
	normal   int
	missing  int
	firing   bool
	last     detect.Result
	lastB    int64
	lastOK   bool
	lastData int64 // bucket of the last non-zero sample (log series)
}

// AbsFloors maps anomaly_type label values to a minimum scale (value units), e.g. errors=0.001.
var AbsFloors = map[string]float64{}

func (s *Server) promInfo(ls series.Labels) any {
	info := &seriesInfo{kind: kindMetric, params: s.promP, service: ls.Get("service")}
	t := ls.Get("anomaly_type")
	if t == "errors" || t == "latency" {
		info.dir = +1 // a drop in errors or latency is not a finding
	}
	if f, ok := AbsFloors[t]; ok {
		info.params.AbsFloor = f
	}
	return info
}

func (s *Server) logInfo(ls series.Labels) any {
	info := &seriesInfo{kind: kindLogTemplate, params: s.logP, minVol: s.cfg.LogMinCount, service: ls.Get("service")}
	if ls.Get("__name__") == nameVolume {
		info.kind = kindLogVolume
		if l := ls.Get("level"); l == "error" || l == "warn" {
			info.dir = +1
		}
	}
	return info
}

// --- Prometheus remote write --------------------------------------------------------------

var bufPool = sync.Pool{New: func() any { return new(rwBuf) }}

type rwBuf struct {
	body, raw, key []byte
	ts             promrw.TimeSeries
}

func (s *Server) handleRemoteWrite(w http.ResponseWriter, r *http.Request) {
	s.stats.rwRequests.Add(1)
	if strings.Contains(r.Header.Get("Content-Type"), "io.prometheus.write.v2") {
		s.stats.rwErrors.Add(1)
		http.Error(w, "remote write 2.0 is not supported: use the default prometheus.WriteRequest (1.0)",
			http.StatusUnsupportedMediaType)
		return
	}
	b := bufPool.Get().(*rwBuf)
	defer bufPool.Put(b)
	var err error
	b.body, err = readAll(b.body[:0], http.MaxBytesReader(w, r.Body, 32<<20))
	if err == nil {
		b.raw, err = promrw.Decompress(b.raw, b.body, 256<<20)
	}
	if err != nil {
		s.stats.rwErrors.Add(1)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var newest int64
	maxTS := time.Now().Add(5 * time.Minute).Unix()
	err = promrw.Decode(b.raw, &b.ts, func(ts *promrw.TimeSeries) error {
		b.key = b.key[:0]
		for _, l := range ts.Labels {
			if s.ignore[string(l.Name)] {
				continue
			}
			b.key = append(append(append(append(b.key, l.Name...), 0xfe), l.Value...), 0xff)
		}
		mk := func() (series.Labels, series.Agg, any) {
			ls := make(series.Labels, 0, len(ts.Labels))
			for _, l := range ts.Labels {
				if !s.ignore[string(l.Name)] {
					ls = append(ls, series.Label{Name: string(l.Name), Value: string(l.Value)})
				}
			}
			ls = series.Sorted(ls)
			return ls, series.Mean, s.promInfo(ls)
		}
		ok := s.prom.UpdateBytes(b.key, mk, func(sr *series.Series) {
			for _, smp := range ts.Samples {
				sec := smp.Timestamp / 1000
				// NaN includes Prometheus staleness markers; ±Inf (e.g. x/0 in a ratio rule) would
				// poison the baseline; future timestamps would move the data clock ahead.
				if math.IsNaN(smp.Value) || math.IsInf(smp.Value, 0) || sec > maxTS {
					continue
				}
				sr.Add(sec/s.step, smp.Value)
				newest = max(newest, sec)
			}
		})
		if ok {
			s.stats.rwSamples.Add(int64(len(ts.Samples)))
		} else {
			s.stats.rwDropped.Add(int64(len(ts.Samples)))
		}
		return nil
	})
	if err != nil {
		s.stats.rwErrors.Add(1)
		http.Error(w, err.Error(), http.StatusBadRequest) // 4xx: Prometheus drops the batch, no retry loop
		return
	}
	for {
		old := s.promClock.Load()
		if newest <= old || s.promClock.CompareAndSwap(old, newest) {
			break
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func readAll(dst []byte, r io.Reader) ([]byte, error) {
	for {
		if len(dst) == cap(dst) {
			dst = append(dst, 0)[:len(dst)]
		}
		n, err := r.Read(dst[len(dst):cap(dst)])
		dst = dst[:len(dst)+n]
		if err == io.EOF {
			return dst, nil
		}
		if err != nil {
			return dst, err
		}
	}
}

// --- Backfill from the Prometheus HTTP API ---------------------------------------------------

// backfill loads (seasons+1) seasons of history for each selector with query_range, one day per
// request (Prometheus caps a query at 11,000 points per series).
func (s *Server) backfill(ctx context.Context) {
	ready := strings.TrimRight(s.cfg.BackfillURL, "/") + "/-/ready"
	for i := 0; ; i++ { // wait up to ~2 minutes for Prometheus
		resp, err := http.Get(ready)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if i == 60 || ctx.Err() != nil {
			slog.Error("backfill skipped: Prometheus not ready", "url", ready, "err", err)
			return
		}
		time.Sleep(2 * time.Second)
	}
	stepD := time.Duration(s.step) * time.Second
	end := time.Now().Truncate(stepD)
	start := end.Add(-time.Duration(s.cfg.PromSeasons+1) * s.cfg.PromSeason)
	for _, match := range s.cfg.BackfillMatch {
		n := 0
		for from := start; from.Before(end); from = from.Add(24 * time.Hour) {
			to := from.Add(24*time.Hour - stepD)
			if to.After(end) {
				to = end
			}
			k, err := s.queryRange(ctx, match, from, to)
			if err != nil {
				slog.Error("backfill query failed", "match", match, "from", from, "err", err)
				break
			}
			n += k
		}
		slog.Info("backfill done", "match", match, "samples", n, "series", s.prom.Len())
	}
}

func (s *Server) queryRange(ctx context.Context, match string, from, to time.Time) (int, error) {
	q := url.Values{"query": {match}, "start": {strconv.FormatInt(from.Unix(), 10)},
		"end": {strconv.FormatInt(to.Unix(), 10)}, "step": {strconv.FormatInt(s.step, 10)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(s.cfg.BackfillURL, "/")+"/api/v1/query_range?"+q.Encode(), nil)
	if err != nil {
		return 0, err
	}
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var body struct {
		Status string `json:"status"`
		Error  string `json:"error"`
		Data   struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Values [][2]json.RawMessage
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, fmt.Errorf("decode: %w", err)
	}
	if body.Status != "success" {
		return 0, fmt.Errorf("prometheus: %s", body.Error)
	}
	n := 0
	for _, r := range body.Data.Result {
		ls := make(series.Labels, 0, len(r.Metric))
		for k, v := range r.Metric {
			if !s.ignore[k] {
				ls = append(ls, series.Label{Name: k, Value: v})
			}
		}
		ls = series.Sorted(ls)
		s.prom.Update(ls.Key(), func() (series.Labels, series.Agg, any) { return ls, series.Mean, s.promInfo(ls) },
			func(sr *series.Series) {
				for _, p := range r.Values {
					ts, err1 := strconv.ParseFloat(string(p[0]), 64)
					v, err2 := strconv.ParseFloat(strings.Trim(string(p[1]), `"`), 64)
					if err1 != nil || err2 != nil || math.IsNaN(v) || math.IsInf(v, 0) {
						continue
					}
					sr.Add(int64(ts)/s.step, v)
					n++
				}
			})
	}
	s.stats.backfilled.Add(int64(n))
	return n, nil
}

// --- Logs ----------------------------------------------------------------------------------

// handlePush ingests template counts from agents.
func (s *Server) handlePush(w http.ResponseWriter, r *http.Request) {
	s.stats.logPushes.Add(1)
	var body io.Reader = http.MaxBytesReader(w, r.Body, 64<<20)
	if r.Header.Get("Content-Encoding") == "gzip" {
		zr, err := gzip.NewReader(body)
		if err != nil {
			s.stats.logPushErrors.Add(1)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer zr.Close()
		body = io.LimitReader(zr, 256<<20)
	}
	var p wire.Push
	if err := json.NewDecoder(body).Decode(&p); err != nil {
		s.stats.logPushErrors.Add(1)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	now := time.Now()
	s.markLogStart(now)
	for _, it := range p.Items {
		if it.Template == "" {
			continue
		}
		var total int64
		for _, c := range it.Counts {
			total += max(c[1], 0)
		}
		level := logparse.Level(it.Level)
		svc, id := s.globalTemplate(it.Service, it.Template, total, now)
		for _, c := range it.Counts {
			if c[1] > 0 {
				s.addLog(svc, id, level, c[0], c[1])
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// globalTemplate maps an agent template onto the server-wide template for the service.
func (s *Server) globalTemplate(service, text string, n int64, now time.Time) (string, int) {
	m, svc := s.pool.Get(cleanService(service))
	m.Lock()
	defer m.Unlock()
	s.tmplMu.Lock()
	defer s.tmplMu.Unlock()
	al := s.aliases[svc]
	if al == nil || len(al) > 20*max(s.cfg.Drain.MaxClusters, 1000) {
		al = map[string]int{}
		s.aliases[svc] = al
	}
	if id, ok := al[text]; ok && m.Hit(id, n) != nil {
		return svc, id
	}
	c, ch := m.Add(strings.Fields(text), n)
	al[text] = c.ID
	if ch == drain.Created {
		s.newTemplateLocked(svc, c.ID, now)
	}
	return svc, c.ID
}

func (s *Server) newTemplateLocked(svc string, id int, now time.Time) {
	start := s.logStart.Load()
	s.tmpl[tmplKey{svc, id}] = &tmplMeta{levels: map[string]int64{}, created: now,
		novel: start > 0 && now.Unix()-start >= int64(s.cfg.NoveltyWarmup/time.Second)}
}

func (s *Server) markLogStart(now time.Time) { s.logStart.CompareAndSwap(0, now.Unix()) }

func cleanService(svc string) string {
	svc = strings.TrimSpace(svc)
	if svc == "" {
		return "unknown"
	}
	if len(svc) > 128 {
		svc = svc[:128]
	}
	return svc
}

// addLog adds n lines at unix time ts to the template and volume series of a service.
func (s *Server) addLog(svc string, id int, level string, ts, n int64) {
	if level == "" {
		level = "unknown"
	}
	s.tmplMu.Lock()
	if m := s.tmpl[tmplKey{svc, id}]; m != nil {
		m.levels[level] += n
		if m.novel {
			m.count += n
		}
	}
	s.tmplMu.Unlock()
	b := ts / s.step
	add := func(sr *series.Series) {
		sr.Add(b, float64(n))
		if info := sr.Info.(*seriesInfo); b > info.lastData {
			info.lastData = b
		}
	}
	tid := strconv.Itoa(id)
	tl := series.Labels{{Name: "__name__", Value: nameTemplate}, {Name: "service", Value: svc}, {Name: "template_id", Value: tid}}
	s.logs.Update(tl.Key(), func() (series.Labels, series.Agg, any) { return tl, series.Sum, s.logInfo(tl) }, add)
	vl := series.Labels{{Name: "__name__", Value: nameVolume}, {Name: "level", Value: level}, {Name: "service", Value: svc}}
	s.logs.Update(vl.Key(), func() (series.Labels, series.Agg, any) { return vl, series.Sum, s.logInfo(vl) }, add)
	s.stats.logLines.Add(n)
}

// handleLogs mines raw NDJSON (or plain text) lines posted directly to the server.
// ?service= sets the service when the line has none.
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	defSvc := r.URL.Query().Get("service")
	ex := logparse.NewExtractor(s.cfg.MessageField, s.cfg.ServiceField, s.cfg.LevelField)
	sc := bufio.NewScanner(http.MaxBytesReader(w, r.Body, 256<<20))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	type key struct {
		svc   string
		id    int
		level string
	}
	counts := map[key]int64{}
	now := time.Now()
	s.markLogStart(now)
	for sc.Scan() {
		line := sc.Text()
		msg, svc, level := line, defSvc, ""
		if vals, ok := ex.Extract(line); ok {
			msg, level = vals[0], logparse.Level(vals[2])
			if vals[1] != "" {
				svc = vals[1]
			}
		}
		if level == "" {
			level = logparse.DetectLevel(msg)
		}
		m, used := s.pool.Get(cleanService(svc))
		m.Lock()
		c, ch := m.AddLine(msg)
		if ch == drain.Created {
			s.tmplMu.Lock()
			s.newTemplateLocked(used, c.ID, now)
			s.tmplMu.Unlock()
		}
		m.Unlock()
		counts[key{used, c.ID, level}]++
	}
	if err := sc.Err(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for k, n := range counts {
		s.addLog(k.svc, k.id, k.level, now.Unix(), n)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleTemplates lists current templates, optionally for one service.
func (s *Server) handleTemplates(w http.ResponseWriter, r *http.Request) {
	type row struct {
		Service  string `json:"service"`
		ID       int    `json:"template_id"`
		Template string `json:"template"`
		Lines    int64  `json:"lines"`
		Level    string `json:"level"`
	}
	want := r.URL.Query().Get("service")
	out := []row{}
	s.pool.Each(func(svc string, m *drain.Miner) {
		if want != "" && svc != want {
			return
		}
		m.Clusters(func(c *drain.Cluster) {
			out = append(out, row{Service: svc, ID: c.ID, Template: c.Template(), Lines: c.Size})
		})
	})
	s.tmplMu.Lock()
	for i := range out {
		if m := s.tmpl[tmplKey{out[i].Service, out[i].ID}]; m != nil {
			out[i].Level = m.level()
		}
	}
	s.tmplMu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Service != out[j].Service {
			return out[i].Service < out[j].Service
		}
		return out[i].Lines > out[j].Lines
	})
	writeJSON(w, out)
}

// templateText returns the current template text for a service/id ("" if evicted).
func (s *Server) templateText(svc string, id int) string {
	m, _ := s.pool.Get(svc)
	m.Lock()
	defer m.Unlock()
	if c := m.Get(id); c != nil {
		return c.Template()
	}
	return ""
}
