package series

import (
	"math"
	"testing"
)

func TestOpenBucketIsNotFinal(t *testing.T) {
	s := newSeries("k", nil, Mean, 10)
	if !math.IsNaN(s.At(0)) {
		t.Fatal("empty series must read NaN")
	}
	s.Add(5, 1)
	s.Add(5, 3)
	if !math.IsNaN(s.At(5)) {
		t.Fatal("open bucket must read NaN")
	}
	s.Add(6, 9)
	if got := s.At(5); got != 2 {
		t.Fatalf("bucket 5 = %v, want mean 2", got)
	}
}

func TestGapsMeanAndSum(t *testing.T) {
	m := newSeries("m", nil, Mean, 10)
	c := newSeries("c", nil, Sum, 10)
	for _, s := range []*Series{m, c} {
		s.Add(1, 4)
		s.Add(4, 4)
	}
	if !math.IsNaN(m.At(2)) || !math.IsNaN(m.At(3)) {
		t.Fatalf("mean gap = %v %v, want NaN", m.At(2), m.At(3))
	}
	if c.At(2) != 0 || c.At(3) != 0 {
		t.Fatalf("sum gap = %v %v, want 0", c.At(2), c.At(3))
	}
	if !math.IsNaN(c.At(0)) {
		t.Fatal("sum bucket before the first sample must read NaN")
	}
}

func TestRingWraparound(t *testing.T) {
	s := newSeries("k", nil, Mean, 4)
	for b := int64(0); b < 10; b++ {
		s.Add(b, float64(b))
	}
	// head = 9; the ring holds 9 slots minus the open one: buckets 6, 7, 8.
	for b := int64(0); b <= 5; b++ {
		if !math.IsNaN(s.At(b)) {
			t.Fatalf("bucket %d = %v, want NaN (overwritten)", b, s.At(b))
		}
	}
	for b := int64(6); b <= 8; b++ {
		if s.At(b) != float64(b) {
			t.Fatalf("bucket %d = %v, want %d", b, s.At(b), b)
		}
	}
	// A long jump clears the whole ring.
	s.Add(100, 1)
	for b := int64(96); b < 100; b++ {
		if !math.IsNaN(s.At(b)) {
			t.Fatalf("bucket %d = %v, want NaN after a long gap", b, s.At(b))
		}
	}
}

func TestLateSamplesJoinTheMean(t *testing.T) {
	s := newSeries("k", nil, Mean, 20)
	s.Add(1, 10)
	s.Add(1, 20)
	s.Advance(3) // the data clock moved on before every shard arrived
	s.Add(1, 30)
	s.Add(1, 100)
	if got := s.At(1); math.Abs(got-40) > 1e-4 {
		t.Fatalf("bucket 1 = %v, want 40 (mean of 10, 20, 30, 100)", got)
	}
	// A bucket closed empty takes late samples as its first ones.
	s.Add(2, 6)
	s.Add(2, 2)
	if got := s.At(2); got != 4 {
		t.Fatalf("bucket 2 = %v, want 4", got)
	}
	// Late counts survive a snapshot.
	st := NewStore(20, 0)
	st.Restore([]Snap{{Key: "k", Vals: append([]float32(nil), s.vals...), Head: s.head, Late: s.late}},
		func(Labels) any { return nil })
	st.Update("k", nil, func(r *Series) {
		r.Add(1, 40)
		if got := r.At(1); math.Abs(got-40) > 1e-4 {
			t.Fatalf("restored bucket 1 = %v, want 40", got)
		}
	})
}

func TestOldLateSampleKeepsValue(t *testing.T) {
	s := newSeries("k", nil, Mean, 100)
	s.Add(1, 10)
	s.Advance(50)
	s.Add(1, 1000) // e.g. backfill overlapping live data: count unknown, keep the live mean
	if got := s.At(1); got != 10 {
		t.Fatalf("bucket 1 = %v, want 10", got)
	}
	s.Add(2, 7) // but an empty old bucket is filled
	if got := s.At(2); got != 7 {
		t.Fatalf("bucket 2 = %v, want 7", got)
	}
}

func TestLateSumAdds(t *testing.T) {
	s := newSeries("k", nil, Sum, 10)
	s.Add(1, 3)
	s.Advance(5)
	s.Add(1, 2)
	s.Add(3, 4)
	if s.At(1) != 5 || s.At(3) != 4 {
		t.Fatalf("buckets 1, 3 = %v, %v, want 5, 4", s.At(1), s.At(3))
	}
}

func TestStoreCapAndDelete(t *testing.T) {
	st := NewStore(10, 2)
	mk := func() (Labels, Agg, any) { return nil, Mean, nil }
	for _, k := range []string{"a", "b", "c"} {
		st.Update(k, mk, func(s *Series) { s.Add(1, 1) })
	}
	if st.Len() != 2 || st.Dropped() != 1 {
		t.Fatalf("len %d dropped %d, want 2 and 1", st.Len(), st.Dropped())
	}
	if n := st.Delete(func(s *Series) bool { return s.Key == "a" }); n != 1 || st.Len() != 1 {
		t.Fatalf("deleted %d, len %d", n, st.Len())
	}
	if !st.UpdateBytes([]byte("c"), mk, func(*Series) {}) {
		t.Fatal("a slot freed by Delete must be reusable")
	}
}
