package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/detect"
	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/drain"
	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/series"
	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/wire"
)

// Finding is one detected anomaly, as served by /api/v1/findings and sent to Alertmanager.
type Finding struct {
	ID          string            `json:"id"`
	Kind        string            `json:"kind"`
	State       string            `json:"state"` // firing | resolved
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    time.Time         `json:"startsAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`
	EndsAt      time.Time         `json:"endsAt,omitzero"`
	Bucket      time.Time         `json:"bucket"`
	Score       float64           `json:"score"`
	Observed    float64           `json:"observed"`
	Expected    float64           `json:"expected"`
	Lower       float64           `json:"lower"`
	Upper       float64           `json:"upper"`
	Seasonal    bool              `json:"seasonal"`

	key         string // store key (series) or novelty key
	resolveSent bool
}

// readableID renders labels as name{k="v",...}.
func readableID(ls series.Labels) string {
	var b strings.Builder
	b.WriteString(ls.Get("__name__"))
	b.WriteByte('{')
	first := true
	for _, l := range ls {
		if l.Name == "__name__" {
			continue
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		fmt.Fprintf(&b, "%s=%q", l.Name, l.Value)
	}
	b.WriteByte('}')
	return b.String()
}

type evType uint8

const (
	evFire evType = iota
	evUpdate
	evResolve
)

type event struct {
	typ    evType
	key    string
	kind   string
	labels series.Labels
	r      detect.Result
	bucket int64
	dir    int
}

const maxCatchUp = 3 // buckets scored per series per evaluation after a gap

// Evaluate scores every complete bucket not yet scored and updates findings.
func (s *Server) Evaluate(now time.Time) {
	t0 := time.Now()
	grace := int64(s.cfg.Grace / time.Second)
	// Metrics: data time (newest remote-write sample) decides which buckets are complete, so
	// replayed or backfilled data evaluates the same way as live data.
	promOpen := (s.promClock.Load() - grace) / s.step
	// Logs: wall clock, because agents push partial counts for the current bucket.
	logOpen := (now.Unix() - grace) / s.step

	var mu sync.Mutex
	var events []event
	var scored int64
	workers := s.cfg.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	run := func(st *series.Store, open int64, isLog bool) {
		st.ForEachShard(workers, func(ss map[string]*series.Series) {
			scratch := make([]float64, 0, 4096)
			var local []event
			var n int64
			for _, sr := range ss {
				if isLog {
					sr.Advance(open)
				} else if sr.Head() < open {
					sr.Advance(open)
				}
				n += s.evalSeries(sr, open-1, &scratch, &local)
			}
			mu.Lock()
			events = append(events, local...)
			scored += n
			mu.Unlock()
		})
	}
	if s.promClock.Load() > 0 {
		run(s.prom, promOpen, false)
	}
	run(s.logs, logOpen, true)
	s.apply(events, now)
	s.evalNovelty(now)
	s.gc(promOpen, logOpen)
	s.stats.evalNanos.Store(int64(time.Since(t0)))
	s.stats.evalSeries.Store(scored)
	s.sendAlerts(now)
}

func (s *Server) evalSeries(sr *series.Series, last int64, scratch *[]float64, out *[]event) int64 {
	info := sr.Info.(*seriesInfo)
	start := max(sr.LastEval+1, last-maxCatchUp+1)
	var n int64
	for b := start; b <= last; b++ {
		sr.LastEval = b
		r, ok := detect.Score(sr, b, info.params, scratch, &info.cache)
		if !ok {
			// A gap breaks the run of consecutive anomalous buckets --for counts.
			info.missing++
			info.pending = 0
			if info.firing && info.missing >= 3 {
				info.firing, info.pending, info.normal = false, 0, 0
				*out = append(*out, event{typ: evResolve, key: sr.Key})
			}
			continue
		}
		n++
		info.missing = 0
		info.last, info.lastB, info.lastOK = r, b, true
		anom := anomalous(r, info, s.cfg.Threshold)
		if anom {
			info.pending++
			info.normal = 0
		} else {
			info.normal++
			info.pending = 0
		}
		dir := 1
		if r.Score < 0 {
			dir = -1
		}
		ev := event{key: sr.Key, kind: info.kind, labels: sr.Labels, r: r, bucket: b, dir: dir}
		switch {
		case !info.firing && info.pending >= s.cfg.ForBuckets:
			info.firing = true
			ev.typ = evFire
			*out = append(*out, ev)
		case info.firing && anom:
			ev.typ = evUpdate
			*out = append(*out, ev)
		case info.firing && info.normal >= 2:
			info.firing = false
			ev.typ = evResolve
			*out = append(*out, ev)
		}
	}
	return n
}

func anomalous(r detect.Result, info *seriesInfo, thr float64) bool {
	if info.minVol > 0 && max(r.Observed, r.Expected) < info.minVol {
		return false
	}
	switch info.dir {
	case +1:
		return r.Score >= thr
	case -1:
		return r.Score <= -thr
	}
	return math.Abs(r.Score) >= thr
}

func (s *Server) apply(events []event, now time.Time) {
	s.findMu.Lock()
	defer s.findMu.Unlock()
	for _, ev := range events {
		f := s.active[ev.key]
		switch ev.typ {
		case evFire:
			f = s.newFinding(ev, now)
			s.active[ev.key] = f
			slog.Info("finding", "id", f.ID, "alertname", f.Labels["alertname"], "service", f.Labels["service"],
				"score", round(f.Score), "observed", round(f.Observed), "expected", round(f.Expected))
		case evUpdate:
			if f != nil {
				s.fill(f, ev)
				f.UpdatedAt = now
			}
		case evResolve:
			if f != nil {
				s.resolveLocked(f, now)
			}
		}
	}
}

func (s *Server) resolveLocked(f *Finding, now time.Time) {
	f.State, f.EndsAt, f.UpdatedAt = "resolved", now, now
	delete(s.active, f.key)
	s.resolved = append([]*Finding{f}, s.resolved...)
	if len(s.resolved) > 500 {
		s.resolved = s.resolved[:500]
	}
	slog.Info("resolved", "id", f.ID, "alertname", f.Labels["alertname"], "service", f.Labels["service"])
}

func (s *Server) newFinding(ev event, now time.Time) *Finding {
	f := &Finding{ID: readableID(ev.labels), key: ev.key, Kind: ev.kind, State: "firing", StartsAt: now, UpdatedAt: now,
		Labels: map[string]string{"source": "anomalyd", "anomaly_kind": ev.kind}, Annotations: map[string]string{}}
	dir := "up"
	if ev.dir < 0 {
		dir = "down"
	}
	f.Labels["direction"] = dir
	for _, l := range ev.labels {
		if l.Name != "__name__" && wire.ValidLabelName(l.Name) {
			f.Labels[l.Name] = l.Value
		}
	}
	name := ev.labels.Get("__name__")
	switch ev.kind {
	case kindMetric:
		f.Labels["alertname"], f.Labels["tier"], f.Labels["severity"] = "AnomalydMetricAnomaly", "2", "warning"
		f.Labels["metric"] = name
	case kindLogTemplate:
		id, _ := strconv.Atoi(ev.labels.Get("template_id"))
		svc := ev.labels.Get("service")
		level := s.templateLevel(svc, id)
		f.Labels["alertname"], f.Labels["tier"], f.Labels["level"] = "AnomalydLogTemplateRate", "2", level
		f.Labels["severity"] = severity(level, dir)
		f.Annotations["template"] = s.templateText(svc, id)
	case kindLogVolume:
		level := ev.labels.Get("level")
		f.Labels["alertname"], f.Labels["tier"] = "AnomalydLogVolume", "2"
		f.Labels["severity"] = severity(level, dir)
		if dir == "down" && (level == "info" || level == "unknown") {
			f.Labels["severity"] = "warning" // a service that stops logging is worth a look
		}
	}
	s.fill(f, ev)
	return f
}

func severity(level, dir string) string {
	if (level == "error" || level == "warn") && dir == "up" {
		return "warning"
	}
	return "info"
}

func (s *Server) fill(f *Finding, ev event) {
	r := ev.r
	f.Score, f.Observed, f.Expected, f.Lower, f.Upper, f.Seasonal = r.Score, r.Observed, r.Expected, r.Lower, r.Upper, r.Seasonal
	f.Bucket = time.Unix(ev.bucket*s.step, 0).UTC()
	base := "seasonal median"
	if !r.Seasonal {
		base = "recent median (no seasonal history yet)"
	}
	subject := f.Labels["metric"]
	if subject == "" {
		subject = ev.labels.Get("__name__")
	}
	f.Annotations["summary"] = fmt.Sprintf("%s %s: observed %s vs expected %s (z=%+.1f)",
		f.Labels["service"], subject, fmtNum(r.Observed), fmtNum(r.Expected), r.Score)
	f.Annotations["description"] = fmt.Sprintf("Bucket %s (%ds). Band %s … %s from the %s ± %.0f robust σ.",
		f.Bucket.Format(time.RFC3339), s.step, fmtNum(r.Lower), fmtNum(r.Upper), base, s.cfg.Threshold)
}

func fmtNum(v float64) string { return strconv.FormatFloat(round(v), 'g', 6, 64) }
func round(v float64) float64 { return math.Round(v*1000) / 1000 }

func (s *Server) templateLevel(svc string, id int) string {
	s.tmplMu.Lock()
	defer s.tmplMu.Unlock()
	if m := s.tmpl[tmplKey{svc, id}]; m != nil {
		return m.level()
	}
	return ""
}

// evalNovelty fires a finding for templates first seen after warm-up once they reach
// NoveltyMin lines within NoveltyWindow; one-off lines below that are forgotten.
func (s *Server) evalNovelty(now time.Time) {
	type cand struct {
		k     tmplKey
		level string
		count int64
	}
	var fire, resolve []cand
	s.tmplMu.Lock()
	for k, m := range s.tmpl {
		if !m.novel {
			continue
		}
		switch {
		case m.fired.IsZero() && m.count >= s.cfg.NoveltyMin:
			m.fired = now
			fire = append(fire, cand{k, m.level(), m.count})
		case m.fired.IsZero() && now.Sub(m.created) > s.cfg.NoveltyWindow:
			m.novel = false
		case !m.fired.IsZero() && now.Sub(m.fired) > s.cfg.NoveltyTTL:
			m.novel = false
			resolve = append(resolve, cand{k: k})
		}
	}
	s.tmplMu.Unlock()
	texts := make([]string, len(fire))
	for i, c := range fire {
		texts[i] = s.templateText(c.k.service, c.k.id)
	}
	s.findMu.Lock()
	defer s.findMu.Unlock()
	for i, c := range fire {
		id := "new\xff" + c.k.service + "\xff" + strconv.Itoa(c.k.id)
		f := &Finding{ID: fmt.Sprintf("log_new_template{service=%q,template_id=\"%d\"}", c.k.service, c.k.id), key: id, Kind: kindLogNew, State: "firing", StartsAt: now, UpdatedAt: now,
			Bucket: now.UTC().Truncate(time.Second), Observed: float64(c.count),
			Labels: map[string]string{"alertname": "AnomalydLogNewTemplate", "source": "anomalyd", "tier": "1",
				"anomaly_kind": kindLogNew, "service": c.k.service, "template_id": strconv.Itoa(c.k.id),
				"level": c.level, "severity": severity(c.level, "up")},
			Annotations: map[string]string{"template": texts[i],
				"summary": fmt.Sprintf("%s: new log template (%d lines since first seen)", c.k.service, c.count),
				"description": fmt.Sprintf("First seen after the %s warm-up; masked template: %s",
					s.cfg.NoveltyWarmup, texts[i])}}
		s.active[id] = f
		slog.Info("finding", "id", f.ID, "alertname", "AnomalydLogNewTemplate",
			"service", c.k.service, "template", texts[i], "lines", c.count)
	}
	for _, c := range resolve {
		if f := s.active["new\xff"+c.k.service+"\xff"+strconv.Itoa(c.k.id)]; f != nil {
			s.resolveLocked(f, now)
		}
	}
}

// gc drops series that saw no data for a full ring (metrics) or a full season (log series), and
// template metadata whose template the miner has evicted.
func (s *Server) gc(promOpen, logOpen int64) {
	s.gcRuns++
	if s.gcRuns%100 == 0 {
		live := map[tmplKey]bool{}
		s.pool.Each(func(svc string, m *drain.Miner) {
			m.Clusters(func(c *drain.Cluster) { live[tmplKey{svc, c.ID}] = true })
		})
		s.tmplMu.Lock()
		for k, m := range s.tmpl {
			if !live[k] && !m.novel {
				delete(s.tmpl, k)
			}
		}
		s.tmplMu.Unlock()
	}
	pc := int64(s.prom.Capacity())
	s.prom.Delete(func(sr *series.Series) bool { return sr.Head() < promOpen-pc })
	season := int64(s.logP.Season)
	s.logs.Delete(func(sr *series.Series) bool {
		return sr.Info.(*seriesInfo).lastData < logOpen-season && !sr.Info.(*seriesInfo).firing
	})
}

// --- Alertmanager --------------------------------------------------------------------------

type amAlert struct {
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL,omitempty"`
}

// sendAlerts re-sends every firing finding (endsAt in the future, so Alertmanager keeps it
// open until we stop) and each resolved one once with endsAt = resolve time.
func (s *Server) sendAlerts(now time.Time) {
	if len(s.cfg.AlertmanagerURLs) == 0 {
		return
	}
	hold := max(4*s.cfg.EvalInterval, 2*time.Minute)
	var alerts []amAlert
	s.findMu.Lock()
	for _, f := range s.active {
		alerts = append(alerts, s.toAM(f, now.Add(hold)))
	}
	for _, f := range s.resolved {
		if !f.resolveSent {
			f.resolveSent = true
			alerts = append(alerts, s.toAM(f, f.EndsAt))
		}
	}
	s.findMu.Unlock()
	if len(alerts) == 0 {
		return
	}
	body, _ := json.Marshal(alerts)
	for _, u := range s.cfg.AlertmanagerURLs {
		resp, err := s.client.Post(strings.TrimRight(u, "/")+"/api/v2/alerts", "application/json", bytes.NewReader(body))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode/100 != 2 {
				err = fmt.Errorf("status %s", resp.Status)
			}
		}
		if err != nil {
			s.stats.alertErrors.Add(1)
			slog.Warn("alertmanager post failed", "url", u, "err", err)
			continue
		}
		s.stats.alertsSent.Add(int64(len(alerts)))
	}
}

func (s *Server) toAM(f *Finding, ends time.Time) amAlert {
	gen := ""
	if s.cfg.ExternalURL != "" {
		gen = strings.TrimRight(s.cfg.ExternalURL, "/") + "/api/v1/findings?id=" + url.QueryEscape(f.ID)
	}
	return amAlert{Labels: f.Labels, Annotations: f.Annotations, StartsAt: f.StartsAt, EndsAt: ends, GeneratorURL: gen}
}

// handleFindings serves active and recently resolved findings (?id= for one, ?state=firing).
func (s *Server) handleFindings(w http.ResponseWriter, r *http.Request) {
	id, state := r.URL.Query().Get("id"), r.URL.Query().Get("state")
	s.findMu.Lock()
	out := []Finding{} // [] rather than null when empty
	for _, f := range s.active {
		out = append(out, *f)
	}
	if state != "firing" {
		for _, f := range s.resolved {
			out = append(out, *f)
		}
	}
	s.findMu.Unlock()
	if id != "" {
		keep := out[:0]
		for _, f := range out {
			if f.ID == id {
				keep = append(keep, f)
			}
		}
		out = keep
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartsAt.After(out[j].StartsAt) })
	writeJSON(w, out)
}
