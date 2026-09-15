package promrw

import (
	"math"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	in := []Series{
		{Labels: [][2]string{{"__name__", "anomaly:svc:requests:rate5m"}, {"service", "checkout"}},
			Samples: []Sample{{Value: 12.5, Timestamp: 1_789_000_000_000}, {Value: math.Inf(1), Timestamp: 1_789_000_060_000}}},
		{Labels: [][2]string{{"__name__", "up"}}, Samples: []Sample{{Value: 1, Timestamp: 1}}},
	}
	var e Encoder
	body := e.Encode(in)
	raw, err := Decompress(nil, body, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	var got []Series
	var ts TimeSeries
	err = Decode(raw, &ts, func(ts *TimeSeries) error {
		s := Series{Samples: append([]Sample(nil), ts.Samples...)}
		for _, l := range ts.Labels {
			s.Labels = append(s.Labels, [2]string{string(l.Name), string(l.Value)})
		}
		got = append(got, s)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Labels[1][1] != "checkout" || got[0].Samples[1].Timestamp != 1_789_000_060_000 ||
		!math.IsInf(got[0].Samples[1].Value, 1) || got[1].Labels[0][1] != "up" {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if _, err := Decompress(nil, body, 10); err == nil {
		t.Fatal("size limit not enforced")
	}
	if err := Decode(raw[:len(raw)-3], &ts, func(*TimeSeries) error { return nil }); err == nil {
		t.Fatal("truncated message accepted")
	}
}

func TestSkipsUnknownFields(t *testing.T) {
	// metadata (field 3) and an exemplar inside the series (field 3) must be skipped.
	var e Encoder
	body := e.Encode([]Series{{Labels: [][2]string{{"__name__", "x"}}, Samples: []Sample{{Value: 2, Timestamp: 5}}}})
	raw, _ := Decompress(nil, body, 1<<20)
	raw = appendBytes(raw, 3, []byte{0x08, 0x01}) // WriteRequest.metadata
	n := 0
	var ts TimeSeries
	if err := Decode(raw, &ts, func(ts *TimeSeries) error { n += len(ts.Samples); return nil }); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}
