// Package detect scores the latest bucket of a series against a seasonal baseline.
package detect

import (
	"math"
	"slices"
)

// Params for Score. Buckets are fixed-width; Season and Lookback are in buckets.
type Params struct {
	Season    int     // buckets per season (e.g. 1 day at 5m = 288)
	Seasons   int     // seasons of history used for the median
	Lookback  int     // cold-start window (buckets) when there is no seasonal history
	Threshold float64 // band half-width in scale units
	AbsFloor  float64 // minimum scale, in value units
	RelFloor  float64 // minimum scale as a fraction of |expected|
	Poisson   bool    // count data: scale >= sqrt(max(expected, 1))
	// ScaleEvery > 0 reuses the robust scale (1.4826*MAD of last-season residuals) for this many
	// buckets instead of recomputing it every bucket. The residual pass is ~99 % of the cost.
	ScaleEvery int
}

// Cache holds a reusable robust scale for one series (see Params.ScaleEvery).
type Cache struct {
	MADScale float64
	At       int64
	Seasonal bool
	Valid    bool
}

// Result for the scored bucket.
type Result struct {
	Observed, Expected, Score, Upper, Lower, Scale float64
	Seasonal                                       bool
}

// Series gives read access to bucket values; NaN means missing.
type Series interface {
	At(b int64) float64
}

// Score is a Go port of score_series() in poc/ml/prom_anomaly_job.py, on buckets:
//
//	expected(b) = median(v(b - k*season), k = 1..seasons)   (seasonal-naive median)
//	              or median(v in [b-lookback, b)) when there is no history one season back
//	scale       = max(1.4826 * MAD(residuals over the last season), floors)
//	score       = (v(now) - expected(now)) / scale
//
// scratch is reused between calls to avoid allocation; pass nil to allocate. cache may be nil.
func Score(s Series, now int64, p Params, scratch *[]float64, cache *Cache) (Result, bool) {
	obs := s.At(now)
	if math.IsNaN(obs) {
		return Result{}, false
	}
	if scratch == nil {
		scratch = new([]float64)
	}
	hist := make([]float64, 0, 8)
	expectedAt := func(b int64) (float64, bool) {
		hist = hist[:0]
		for k := 1; k <= p.Seasons; k++ {
			if v := s.At(b - int64(k*p.Season)); !math.IsNaN(v) {
				hist = append(hist, v)
			}
		}
		if len(hist) == 0 {
			return 0, false
		}
		return median(hist), true
	}

	exp, seasonal := expectedAt(now)
	if !seasonal {
		rec := (*scratch)[:0]
		for b := now - int64(p.Lookback); b < now; b++ {
			if v := s.At(b); !math.IsNaN(v) {
				rec = append(rec, v)
			}
		}
		*scratch = rec
		if len(rec) < 3 {
			return Result{}, false
		}
		exp = median(rec)
	}

	var madScale float64
	if cache != nil && cache.Valid && cache.Seasonal == seasonal && p.ScaleEvery > 0 &&
		now >= cache.At && now-cache.At < int64(p.ScaleEvery) {
		madScale = cache.MADScale
	} else {
		var ok bool
		if madScale, ok = residualScale(s, now, p, seasonal, exp, expectedAt, scratch); !ok {
			return Result{}, false
		}
		if cache != nil {
			*cache = Cache{MADScale: madScale, At: now, Seasonal: seasonal, Valid: true}
		}
	}
	scale := max(madScale, p.AbsFloor, p.RelFloor*math.Abs(exp))
	if p.Poisson {
		scale = max(scale, math.Sqrt(max(exp, 1)))
	}
	return Result{
		Observed: obs, Expected: exp, Score: (obs - exp) / scale, Scale: scale,
		Upper: exp + p.Threshold*scale, Lower: exp - p.Threshold*scale, Seasonal: seasonal,
	}, true
}

// residualScale returns 1.4826 * MAD of the residuals over the last season.
func residualScale(s Series, now int64, p Params, seasonal bool, exp float64,
	expectedAt func(int64) (float64, bool), scratch *[]float64) (float64, bool) {
	res := (*scratch)[:0]
	for b := now - int64(p.Season); b < now; b++ {
		v := s.At(b)
		if math.IsNaN(v) {
			continue
		}
		e, ok := exp, true
		if seasonal {
			e, ok = expectedAt(b)
		}
		if ok {
			res = append(res, v-e)
		}
	}
	*scratch = res
	if len(res) < 10 {
		return 0, false
	}
	med := median(res)
	for i := range res {
		res[i] = math.Abs(res[i] - med)
	}
	return 1.4826 * median(res), true
}

// median sorts xs in place. Matches Python statistics.median (mean of the middle pair).
func median(xs []float64) float64 {
	slices.Sort(xs)
	n := len(xs)
	if n%2 == 1 {
		return xs[n/2]
	}
	return (xs[n/2-1] + xs[n/2]) / 2
}
