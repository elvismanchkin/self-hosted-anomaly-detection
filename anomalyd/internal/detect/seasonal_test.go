package detect

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

const (
	step   = 300
	season = 86400
	end    = 1_789_000_000 / step * step
)

type mapSeries map[int64]float64 // bucket -> value

func (m mapSeries) At(b int64) float64 {
	if v, ok := m[b]; ok {
		return v
	}
	return math.NaN()
}

func splitmix(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

// fixture: `back` seconds of 5m points up to end, daily sine plus deterministic noise in [-5, 5).
func fixture(back int64) map[int64]float64 {
	pts := map[int64]float64{}
	for t := int64(end) - back; t <= end; t += step {
		daily := 100 + 60*math.Sin(2*math.Pi*float64(t%season)/season)
		noise := (float64(splitmix(uint64(t))>>11)/float64(1<<53) - 0.5) * 10
		pts[t] = daily + noise
	}
	return pts
}

func toBuckets(pts map[int64]float64) mapSeries {
	m := mapSeries{}
	for t, v := range pts {
		m[t/step] = v
	}
	return m
}

var params = Params{Season: season / step, Seasons: 3, Lookback: 7200 / step, Threshold: 4, AbsFloor: 1e-9, RelFloor: 0.05}

// Reference values from poc/ml/prom_anomaly_job.py score_series() on the same fixture
// (regenerate: ANOMALYD_FIXTURE_DIR=/some/dir go test ./internal/detect -run Parity, then
// run testdata/parity.py against the written JSON files).
var pyRef = map[string][2]float64{ // case -> {expected, score}
	"normal": {105.3332183795194, 0.9951410131008771},
	"spike":  {105.3332183795194, 23.779976005540902},
	"cold":   {89.72403310813698, 0.9531572702400453}, // cold-start path
}

func TestParityWithPython(t *testing.T) {
	// cold: 12 hours only, so there is no value one season back -> cold-start path.
	cases := map[string]map[int64]float64{"normal": fixture(14 * season), "spike": fixture(14 * season), "cold": fixture(season / 2)}
	cases["spike"][end] += 120
	dir := os.Getenv("ANOMALYD_FIXTURE_DIR")
	for name, pts := range cases {
		if dir != "" {
			js := map[string]float64{}
			for t, v := range pts {
				js[itoa(t)] = v
			}
			b, _ := json.Marshal(js)
			if err := os.WriteFile(dir+"/"+name+".json", b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		r, ok := Score(toBuckets(pts), end/step, params, nil, nil)
		if !ok {
			t.Fatalf("%s: not scorable", name)
		}
		t.Logf("%s: expected=%.12f score=%.12f seasonal=%v", name, r.Expected, r.Score, r.Seasonal)
		ref := pyRef[name]
		if math.Abs(r.Expected-ref[0]) > 1e-9 || math.Abs(r.Score-ref[1]) > 1e-9 {
			t.Errorf("%s: got expected=%.9f score=%.9f, python %.9f %.9f", name, r.Expected, r.Score, ref[0], ref[1])
		}
	}
}

func itoa(x int64) string { b, _ := json.Marshal(x); return string(b) }

func TestSelftestShape(t *testing.T) {
	pts := fixture(14 * season)
	normal, _ := Score(toBuckets(pts), end/step, params, nil, nil)
	pts[end] += 120
	spike, _ := Score(toBuckets(pts), end/step, params, nil, nil)
	flat := mapSeries{}
	for b := range toBuckets(pts) {
		flat[b] = 7
	}
	f, ok := Score(flat, end/step, params, nil, nil)
	if math.Abs(normal.Score) >= 4 || spike.Score <= 4 || !ok || f.Score != 0 || math.Abs(f.Scale-0.35) > 1e-12 {
		t.Fatalf("normal=%v spike=%v flat=%v", normal.Score, spike.Score, f)
	}
	p := params
	p.Poisson = true
	counts := mapSeries{}
	for b := int64(end/step - 400); b <= end/step; b++ {
		counts[b] = 0
	}
	c, _ := Score(counts, end/step, p, nil, nil)
	if c.Scale != 1 {
		t.Fatalf("poisson floor on zeros: scale=%v", c.Scale)
	}
}

func BenchmarkScoreWeekly(b *testing.B) {
	// 1-week season, 2 seasons of history at 5m: the default for Prometheus series.
	p := Params{Season: 2016, Seasons: 2, Lookback: 24, Threshold: 4, RelFloor: 0.05}
	s := mapSeries{}
	for i := int64(0); i < 3*2016+2; i++ {
		s[i] = 100 + 10*math.Sin(float64(i)/50)
	}
	now := int64(3*2016 + 1)
	scratch := make([]float64, 0, 4096)
	b.Run("full", func(b *testing.B) {
		for b.Loop() {
			Score(s, now, p, &scratch, nil)
		}
	})
	b.Run("cached-scale", func(b *testing.B) {
		p.ScaleEvery = 12 // recompute hourly at 5m
		var c Cache
		for i := int64(0); b.Loop(); i++ {
			Score(s, now, p, &scratch, &c)
			if i%12 == 11 {
				c.At -= 12 // simulate time passing so the scale is refreshed every 12 calls
			}
		}
	})
}

func TestCachedScale(t *testing.T) {
	pts := toBuckets(fixture(14 * season))
	p := params
	p.ScaleEvery = 12
	var c Cache
	full, _ := Score(pts, end/step, params, nil, nil)
	first, _ := Score(pts, end/step, p, nil, &c)
	again, _ := Score(pts, end/step, p, nil, &c)
	if !c.Valid || first != full || again != full {
		t.Fatalf("cached scale differs: full=%v first=%v again=%v", full, first, again)
	}
}
