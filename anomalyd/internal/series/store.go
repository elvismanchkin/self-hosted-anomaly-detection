// Package series keeps fixed-step ring buffers for a bounded set of time series.
package series

import (
	"hash/maphash"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// Agg is how samples inside one bucket combine.
type Agg uint8

const (
	Mean Agg = iota // gauges, recording-rule rates and ratios: missing bucket = NaN
	Sum             // event counts (log lines): missing bucket after first sample = 0
)

type Label struct{ Name, Value string }

// Labels are sorted by name.
type Labels []Label

func (ls Labels) Get(name string) string {
	for _, l := range ls {
		if l.Name == name {
			return l.Value
		}
	}
	return ""
}

// Key renders sorted labels into a map key.
func (ls Labels) Key() string {
	var b strings.Builder
	for _, l := range ls {
		b.WriteString(l.Name)
		b.WriteByte(0xfe)
		b.WriteString(l.Value)
		b.WriteByte(0xff)
	}
	return b.String()
}

func Sorted(ls Labels) Labels {
	sort.Slice(ls, func(i, j int) bool { return ls[i].Name < ls[j].Name })
	return ls
}

const noHead = math.MinInt64

// Series is one ring buffer. All access goes through the owning shard's lock.
type Series struct {
	Key      string
	Labels   Labels
	Agg      Agg
	LastEval int64 // last scored bucket
	Info     any   // owner's per-series settings

	vals []float32
	head int64 // open bucket, not yet final
	acc  float64
	cnt  uint32
}

func newSeries(key string, ls Labels, agg Agg, capacity int) *Series {
	s := &Series{Key: key, Labels: ls, Agg: agg, vals: make([]float32, capacity), head: noHead, LastEval: noHead}
	nan := float32(math.NaN())
	for i := range s.vals {
		s.vals[i] = nan
	}
	return s
}

// Head is the open (incomplete) bucket, or math.MinInt64 before the first sample.
func (s *Series) Head() int64 { return s.head }

// At returns the final value of bucket b, NaN if unknown, open or out of range.
func (s *Series) At(b int64) float64 {
	if s.head == noHead || b < 0 || b >= s.head || b <= s.head-int64(len(s.vals)) {
		return math.NaN()
	}
	return float64(s.vals[b%int64(len(s.vals))])
}

// Add puts one sample into bucket b (b >= 0).
func (s *Series) Add(b int64, v float64) {
	switch {
	case b < 0:
	case s.head == noHead:
		s.head, s.acc, s.cnt = b, v, 1
	case b == s.head:
		s.acc += v
		s.cnt++
	case b > s.head:
		s.Advance(b)
		s.acc, s.cnt = v, 1
	case b > s.head-int64(len(s.vals)): // late sample for a closed bucket
		i := b % int64(len(s.vals))
		old := s.vals[i]
		switch {
		case old != old: // NaN
			s.vals[i] = float32(v)
		case s.Agg == Sum:
			s.vals[i] = old + float32(v)
		default:
			s.vals[i] = (old + float32(v)) / 2
		}
	}
}

// Advance closes buckets up to b-1 and opens b. Gaps become NaN (Mean) or 0 (Sum).
func (s *Series) Advance(b int64) {
	if s.head == noHead || b <= s.head {
		return
	}
	n := int64(len(s.vals))
	s.vals[s.head%n] = float32(s.final())
	gap := float32(math.NaN())
	if s.Agg == Sum {
		gap = 0
	}
	for x, i := s.head+1, int64(0); x < b && i < n; x, i = x+1, i+1 {
		s.vals[x%n] = gap
	}
	s.head, s.acc, s.cnt = b, 0, 0
}

func (s *Series) final() float64 {
	if s.Agg == Sum {
		return s.acc
	}
	if s.cnt == 0 {
		return math.NaN()
	}
	return s.acc / float64(s.cnt)
}

const shards = 64

// Store is a sharded, capped set of series with a common bucket capacity.
type Store struct {
	capacity  int
	maxSeries int64
	n         atomic.Int64
	dropped   atomic.Int64
	seed      maphash.Seed
	sh        [shards]struct {
		sync.Mutex
		m map[string]*Series
	}
}

func NewStore(capacity, maxSeries int) *Store {
	st := &Store{capacity: capacity, maxSeries: int64(maxSeries), seed: maphash.MakeSeed()}
	for i := range st.sh {
		st.sh[i].m = map[string]*Series{}
	}
	return st
}

func (st *Store) Len() int       { return int(st.n.Load()) }
func (st *Store) Dropped() int64 { return st.dropped.Load() }
func (st *Store) Capacity() int  { return st.capacity }

// Update runs fn on the series for key under its shard lock, creating it with mk() if absent.
// It returns false when the series is new and the store is full.
func (st *Store) Update(key string, mk func() (Labels, Agg, any), fn func(*Series)) bool {
	sh := &st.sh[maphash.String(st.seed, key)%shards]
	sh.Lock()
	defer sh.Unlock()
	s := sh.m[key]
	if s == nil {
		if st.maxSeries > 0 && st.n.Load() >= st.maxSeries {
			st.dropped.Add(1)
			return false
		}
		ls, agg, info := mk()
		key = strings.Clone(key)
		s = newSeries(key, ls, agg, st.capacity)
		s.Info = info
		sh.m[key] = s
		st.n.Add(1)
	}
	fn(s)
	return true
}

// UpdateBytes is Update for a key held in a reusable buffer; it allocates only for new series.
func (st *Store) UpdateBytes(key []byte, mk func() (Labels, Agg, any), fn func(*Series)) bool {
	sh := &st.sh[maphash.Bytes(st.seed, key)%shards]
	sh.Lock()
	defer sh.Unlock()
	s := sh.m[string(key)] // no allocation for the lookup
	if s == nil {
		if st.maxSeries > 0 && st.n.Load() >= st.maxSeries {
			st.dropped.Add(1)
			return false
		}
		ls, agg, info := mk()
		s = newSeries(string(key), ls, agg, st.capacity)
		s.Info = info
		sh.m[s.Key] = s
		st.n.Add(1)
	}
	fn(s)
	return true
}

// ForEachShard calls fn for each shard's series with the shard locked; shards run on
// `workers` goroutines.
func (st *Store) ForEachShard(workers int, fn func(ss map[string]*Series)) {
	if workers < 1 {
		workers = 1
	}
	var wg sync.WaitGroup
	next := atomic.Int64{}
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := next.Add(1) - 1
				if i >= shards {
					return
				}
				sh := &st.sh[i]
				sh.Lock()
				fn(sh.m)
				sh.Unlock()
			}
		}()
	}
	wg.Wait()
}

// Delete removes series for which drop returns true.
func (st *Store) Delete(drop func(*Series) bool) int {
	removed := 0
	st.ForEachShard(1, func(ss map[string]*Series) {
		for k, s := range ss {
			if drop(s) {
				delete(ss, k)
				removed++
			}
		}
	})
	st.n.Add(int64(-removed))
	return removed
}

// Snap is the serialisable form of a Series.
type Snap struct {
	Key      string
	Labels   Labels
	Agg      Agg
	LastEval int64
	Vals     []float32
	Head     int64
	Acc      float64
	Cnt      uint32
}

func (st *Store) Snapshot() []Snap {
	var out []Snap
	var mu sync.Mutex
	st.ForEachShard(1, func(ss map[string]*Series) {
		for _, s := range ss {
			sn := Snap{Key: s.Key, Labels: s.Labels, Agg: s.Agg, LastEval: s.LastEval,
				Vals: append([]float32(nil), s.vals...), Head: s.head, Acc: s.acc, Cnt: s.cnt}
			mu.Lock()
			out = append(out, sn)
			mu.Unlock()
		}
	})
	return out
}

// Restore loads snapshots whose capacity matches; info derives per-series settings.
func (st *Store) Restore(snaps []Snap, info func(Labels) any) (loaded int) {
	for _, sn := range snaps {
		if len(sn.Vals) != st.capacity {
			continue
		}
		st.Update(sn.Key, func() (Labels, Agg, any) { return sn.Labels, sn.Agg, info(sn.Labels) },
			func(s *Series) {
				copy(s.vals, sn.Vals)
				s.head, s.acc, s.cnt, s.LastEval = sn.Head, sn.Acc, sn.Cnt, sn.LastEval
				loaded++
			})
	}
	return loaded
}
