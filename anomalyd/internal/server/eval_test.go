package server

import (
	"math"
	"testing"

	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/detect"
	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/series"
)

// --for counts consecutive anomalous buckets: a missing bucket between two spikes must not fire.
func TestForBucketsResetOnGap(t *testing.T) {
	s, err := New(testConfig()) // ForBuckets: 2
	if err != nil {
		t.Fatal(err)
	}
	info := &seriesInfo{kind: kindMetric, params: detect.Params{Season: 1440, Seasons: 1, Lookback: 30, Threshold: 4, RelFloor: 0.05}}
	st := series.NewStore(2000, 10)
	var sr *series.Series
	vals := map[int64]float64{}
	for b := int64(0); b < 60; b++ {
		vals[b] = 100 + float64(b%3)
	}
	vals[60] = 1000 // anomalous
	vals[61] = math.NaN()
	vals[62] = 1000 // anomalous again, but not consecutive
	for b := int64(0); b <= 63; b++ {
		v, ok := vals[b]
		if b == 63 {
			v, ok = 1000, true // open bucket; final once 64 arrives
		}
		st.Update("k", func() (series.Labels, series.Agg, any) {
			return series.Labels{{Name: "__name__", Value: "m"}}, series.Mean, info
		}, func(x *series.Series) {
			sr = x
			if ok && !math.IsNaN(v) {
				x.Add(b, v)
			}
		})
	}
	sr.LastEval = 59
	var out []event
	var scratch []float64
	s.evalSeries(sr, 62, &scratch, &out)
	for _, ev := range out {
		if ev.typ == evFire {
			t.Fatalf("fired at bucket %d across a gap", ev.bucket)
		}
	}
	if info.pending != 1 {
		t.Fatalf("pending = %d, want 1 (reset by the gap)", info.pending)
	}

	// Buckets 62 and 63 are consecutive and anomalous: that fires.
	sr.Add(64, 100)
	s.evalSeries(sr, 63, &scratch, &out)
	if len(out) == 0 || out[len(out)-1].typ != evFire {
		t.Fatalf("want a fire after two consecutive anomalous buckets, got %+v", out)
	}
}
