package server

import (
	"math"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/drain"
	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/series"
)

// TestFootprint measures memory and evaluation time at the default settings (5m step; metrics:
// 1w season x 2; logs: 1d season x 3) for a realistic load. Run with
//
//	ANOMALYD_FOOTPRINT=1 go test ./internal/server -run Footprint -v
func TestFootprint(t *testing.T) {
	if os.Getenv("ANOMALYD_FOOTPRINT") == "" {
		t.Skip("set ANOMALYD_FOOTPRINT=1")
	}
	nProm, nLog := 5000, 30000
	if v, _ := strconv.Atoi(os.Getenv("ANOMALYD_PROM_SERIES")); v > 0 {
		nProm = v
	}
	cfg := Config{Step: 5 * time.Minute, EvalInterval: 30 * time.Second, ForBuckets: 2, Threshold: 4,
		MaxSeries: 1 << 20, PromSeason: 7 * 24 * time.Hour, PromSeasons: 2, PromLookback: 2 * time.Hour,
		PromRelFloor: 0.05, MaxLogSeries: 1 << 20, LogSeason: 24 * time.Hour, LogSeasons: 3,
		LogLookback: 2 * time.Hour, LogMinCount: 10, NoveltyWarmup: time.Hour, NoveltyMin: 5,
		NoveltyWindow: 15 * time.Minute, NoveltyTTL: time.Hour, Drain: drain.DefaultConfig(), MaxServices: 1000}
	runtime.GC()
	var m0 runtime.MemStats
	runtime.ReadMemStats(&m0)
	s, _ := New(cfg)
	now := time.Now().Unix() / 300
	for i := 0; i < nProm; i++ {
		ls := series.Labels{{Name: "__name__", Value: "anomaly:svc:requests:rate5m"},
			{Name: "anomaly_type", Value: "requests"}, {Name: "service", Value: "svc-" + strconv.Itoa(i)}}
		s.prom.Update(ls.Key(), func() (series.Labels, series.Agg, any) { return ls, series.Mean, s.promInfo(ls) },
			func(sr *series.Series) {
				for b := now - int64(s.prom.Capacity()) + 2; b <= now; b++ {
					sr.Add(b, 100+10*math.Sin(float64(b+int64(i))/40))
				}
			})
	}
	for i := 0; i < nLog; i++ {
		ls := series.Labels{{Name: "__name__", Value: nameTemplate}, {Name: "service", Value: "svc-" + strconv.Itoa(i/150)},
			{Name: "template_id", Value: strconv.Itoa(i % 150)}}
		s.logs.Update(ls.Key(), func() (series.Labels, series.Agg, any) { return ls, series.Sum, s.logInfo(ls) },
			func(sr *series.Series) {
				for b := now - int64(s.logs.Capacity()) + 2; b <= now; b++ {
					sr.Add(b, float64(50+(b+int64(i))%7))
				}
			})
	}
	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	heap := float64(m1.HeapAlloc-m0.HeapAlloc) / 1e6
	s.promClock.Store(now*300 + 300 + 60)
	evalAt := time.Unix(now*300+300+60, 0)
	t0 := time.Now()
	s.Evaluate(evalAt) // cold: robust scale computed for every series
	cold := time.Since(t0)
	for _, st := range []*series.Store{s.prom, s.logs} {
		st.ForEachShard(1, func(ss map[string]*series.Series) {
			for _, sr := range ss {
				sr.LastEval-- // score the same bucket again, scale now cached
			}
		})
	}
	t1 := time.Now()
	s.Evaluate(evalAt)
	warm := time.Since(t1)
	t.Logf("series: %d metric (ring %d buckets) + %d log (ring %d buckets)", nProm, s.prom.Capacity(), nLog, s.logs.Capacity())
	t.Logf("heap: %.1f MB total, %.1f KB per metric series + log series mix", heap, heap*1e3/float64(nProm+nLog))
	t.Logf("evaluate on GOMAXPROCS=%d: cold %s, warm (cached scale) %s", runtime.GOMAXPROCS(0), cold.Round(time.Millisecond), warm.Round(time.Millisecond))
}
