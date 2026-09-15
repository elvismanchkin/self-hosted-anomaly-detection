package server

import (
	"net/http"
	"runtime/metrics"
	"sort"
	"strings"
	"time"

	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/drain"
	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/series"
	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/wire"
)

type scoreRow struct {
	labels [][2]string
	r      [4]float64 // score, expected, upper, lower
}

func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	e := wire.NewExpo(w)
	var rows []scoreRow
	collect := func(st *series.Store, open int64, templates bool) {
		st.ForEachShard(1, func(ss map[string]*series.Series) {
			for _, sr := range ss {
				info := sr.Info.(*seriesInfo)
				if !info.lastOK || info.lastB < open-3 || (info.kind == kindLogTemplate && !templates) {
					continue
				}
				ls := make([][2]string, 0, len(sr.Labels))
				for _, l := range sr.Labels {
					if l.Name == "metric" {
						continue // our own "metric" label carries __name__
					}
					if l.Name == "__name__" {
						l.Name = "metric"
					}
					// Upstream promql-anomaly-detection rules select every series carrying
					// anomaly_* labels; re-exporting them would feed our scores back in.
					if strings.HasPrefix(l.Name, "anomaly_") {
						continue
					}
					ls = append(ls, [2]string{l.Name, l.Value})
				}
				rows = append(rows, scoreRow{ls, [4]float64{info.last.Score, info.last.Expected, info.last.Upper, info.last.Lower}})
			}
		})
	}
	grace := int64(s.cfg.Grace / time.Second)
	collect(s.prom, (s.promClock.Load()-grace)/s.step, true)
	collect(s.logs, (time.Now().Unix()-grace)/s.step, s.cfg.ExportTemplate)
	sort.Slice(rows, func(i, j int) bool { return lessLabels(rows[i].labels, rows[j].labels) })
	fams := []struct{ name, help string }{
		{"anomalyd_score", "Robust z-score of the last scored bucket (signed)."},
		{"anomalyd_expected", "Baseline (seasonal or recent median) for the last scored bucket."},
		{"anomalyd_band_upper", "expected + threshold * scale."},
		{"anomalyd_band_lower", "expected - threshold * scale."},
	}
	for i, f := range fams {
		if i > 0 && !s.cfg.ExportBands {
			break
		}
		e.Family(f.name, f.help, "gauge")
		for _, r := range rows {
			e.Sample(f.name, r.labels, r.r[i])
		}
	}

	s.findMu.Lock()
	byKind := map[string]int{kindMetric: 0, kindLogTemplate: 0, kindLogVolume: 0, kindLogNew: 0}
	for _, f := range s.active {
		byKind[f.Kind]++
	}
	s.findMu.Unlock()
	e.Family("anomalyd_findings_active", "Findings currently firing.", "gauge")
	for _, k := range []string{kindMetric, kindLogTemplate, kindLogVolume, kindLogNew} {
		e.Sample("anomalyd_findings_active", [][2]string{{"kind", k}}, float64(byKind[k]))
	}
	e.Family("anomalyd_series", "Series held in memory.", "gauge")
	e.Sample("anomalyd_series", [][2]string{{"source", "remote_write"}}, float64(s.prom.Len()))
	e.Sample("anomalyd_series", [][2]string{{"source", "logs"}}, float64(s.logs.Len()))
	e.Family("anomalyd_series_dropped_total", "Samples or counts dropped because a store was full.", "counter")
	e.Sample("anomalyd_series_dropped_total", [][2]string{{"source", "remote_write"}}, float64(s.prom.Dropped()))
	e.Sample("anomalyd_series_dropped_total", [][2]string{{"source", "logs"}}, float64(s.logs.Dropped()))

	counter := func(name, help string, v int64) {
		e.Family(name, help, "counter")
		e.Sample(name, nil, float64(v))
	}
	counter("anomalyd_remote_write_requests_total", "Remote-write requests received.", s.stats.rwRequests.Load())
	counter("anomalyd_remote_write_errors_total", "Remote-write requests rejected.", s.stats.rwErrors.Load())
	counter("anomalyd_remote_write_samples_total", "Samples accepted.", s.stats.rwSamples.Load())
	counter("anomalyd_remote_write_samples_dropped_total", "Samples of series beyond --max-series.", s.stats.rwDropped.Load())
	counter("anomalyd_backfill_samples_total", "Samples loaded from Prometheus query_range.", s.stats.backfilled.Load())
	counter("anomalyd_log_lines_total", "Log lines counted (agents and /api/v1/logs).", s.stats.logLines.Load())
	counter("anomalyd_log_pushes_total", "Agent pushes received.", s.stats.logPushes.Load())
	counter("anomalyd_log_push_errors_total", "Agent pushes rejected.", s.stats.logPushErrors.Load())
	counter("anomalyd_alerts_sent_total", "Alerts posted to Alertmanager (per receiver).", s.stats.alertsSent.Load())
	counter("anomalyd_alert_errors_total", "Failed Alertmanager posts.", s.stats.alertErrors.Load())

	e.Family("anomalyd_templates", "Log templates held per service.", "gauge")
	type tc struct {
		svc string
		n   int
	}
	var tcs []tc
	s.pool.Each(func(svc string, m *drain.Miner) { tcs = append(tcs, tc{svc, m.Len()}) })
	sort.Slice(tcs, func(i, j int) bool { return tcs[i].svc < tcs[j].svc })
	for _, t := range tcs {
		e.Sample("anomalyd_templates", [][2]string{{"service", t.svc}}, float64(t.n))
	}
	e.Family("anomalyd_eval_duration_seconds", "Duration of the last evaluation.", "gauge")
	e.Sample("anomalyd_eval_duration_seconds", nil, time.Duration(s.stats.evalNanos.Load()).Seconds())
	e.Family("anomalyd_eval_scored_buckets", "Buckets scored in the last evaluation.", "gauge")
	e.Sample("anomalyd_eval_scored_buckets", nil, float64(s.stats.evalSeries.Load()))
	e.Family("anomalyd_remote_write_newest_sample_timestamp_seconds", "Newest remote-write sample.", "gauge")
	e.Sample("anomalyd_remote_write_newest_sample_timestamp_seconds", nil, float64(s.promClock.Load()))
	writeRuntime(e, s.started)
	_ = e.Flush()
}

func lessLabels(a, b [][2]string) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			if a[i][0] != b[i][0] {
				return a[i][0] < b[i][0]
			}
			return a[i][1] < b[i][1]
		}
	}
	return len(a) < len(b)
}

var runtimeSamples = []metrics.Sample{
	{Name: "/memory/classes/total:bytes"},
	{Name: "/memory/classes/heap/objects:bytes"},
	{Name: "/sched/goroutines:goroutines"},
	{Name: "/cpu/classes/total:cpu-seconds"},
}

// writeRuntime exports a few Go runtime figures (no stop-the-world ReadMemStats).
func writeRuntime(e *wire.Expo, started time.Time) {
	ss := make([]metrics.Sample, len(runtimeSamples))
	copy(ss, runtimeSamples)
	metrics.Read(ss)
	val := func(i int) float64 {
		switch ss[i].Value.Kind() {
		case metrics.KindUint64:
			return float64(ss[i].Value.Uint64())
		case metrics.KindFloat64:
			return ss[i].Value.Float64()
		}
		return 0
	}
	e.Family("anomalyd_go_memory_total_bytes", "All memory mapped by the Go runtime.", "gauge")
	e.Sample("anomalyd_go_memory_total_bytes", nil, val(0))
	e.Family("anomalyd_go_heap_objects_bytes", "Live and unswept heap objects.", "gauge")
	e.Sample("anomalyd_go_heap_objects_bytes", nil, val(1))
	e.Family("anomalyd_go_goroutines", "Goroutines.", "gauge")
	e.Sample("anomalyd_go_goroutines", nil, val(2))
	e.Family("anomalyd_go_cpu_seconds_total", "CPU time available to the Go runtime (estimate).", "counter")
	e.Sample("anomalyd_go_cpu_seconds_total", nil, val(3))
	e.Family("process_start_time_seconds", "Start time of the process.", "gauge")
	e.Sample("process_start_time_seconds", nil, float64(started.Unix()))
}
