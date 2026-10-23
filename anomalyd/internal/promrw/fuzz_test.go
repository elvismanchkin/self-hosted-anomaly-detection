package promrw

import (
	"bytes"
	"math"
	"testing"
)

// decodeAll copies every series out of buf (Decode's labels alias it).
func decodeAll(buf []byte) ([]Series, error) {
	var out []Series
	var ts TimeSeries
	err := Decode(buf, &ts, func(ts *TimeSeries) error {
		s := Series{Samples: append([]Sample{}, ts.Samples...)}
		for _, l := range ts.Labels {
			s.Labels = append(s.Labels, [2]string{string(l.Name), string(l.Value)})
		}
		out = append(out, s)
		return nil
	})
	return out, err
}

func sameSeries(a, b []Series) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i].Labels) != len(b[i].Labels) || len(a[i].Samples) != len(b[i].Samples) {
			return false
		}
		for j := range a[i].Labels {
			if a[i].Labels[j] != b[i].Labels[j] {
				return false
			}
		}
		for j, s := range a[i].Samples {
			t := b[i].Samples[j]
			if math.Float64bits(s.Value) != math.Float64bits(t.Value) || s.Timestamp != t.Timestamp {
				return false
			}
		}
	}
	return true
}

// FuzzDecode feeds arbitrary bytes to the remote-write path: snappy with the size limit, then
// the protobuf walk. Whatever decodes must survive an encode/decode round trip unchanged.
func FuzzDecode(f *testing.F) {
	var e Encoder
	for _, in := range [][]Series{
		nil,
		{{Labels: [][2]string{{"__name__", "anomaly:svc:requests:rate5m"}, {"service", "checkout"}},
			Samples: []Sample{{Value: 12.5, Timestamp: 1_789_000_000_000}, {Value: math.NaN(), Timestamp: 1_789_000_060_000}}}},
		{{Labels: [][2]string{{"__name__", "up"}}, Samples: []Sample{{Value: 1, Timestamp: -1}}},
			{Labels: [][2]string{{"", ""}}}},
	} {
		body := e.Encode(in)
		f.Add(bytes.Clone(body))
		raw, _ := Decompress(nil, body, 1<<20)
		f.Add(raw)
	}
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0x0f}) // snappy length beyond the limit
	f.Fuzz(func(t *testing.T, body []byte) {
		const limit = 1 << 16
		raw, err := Decompress(nil, body, limit)
		if err == nil && len(raw) > limit {
			t.Fatalf("decoded %d bytes, limit %d", len(raw), limit)
		}
		for _, buf := range [][]byte{raw, body} {
			got, err := decodeAll(buf)
			if err != nil {
				continue
			}
			again, err := decodeAll(mustDecompress(t, e.Encode(got)))
			if err != nil || !sameSeries(got, again) {
				t.Fatalf("round trip changed the request: %v\n%+v\n%+v", err, got, again)
			}
		}
	})
}

func mustDecompress(t *testing.T, body []byte) []byte {
	raw, err := Decompress(nil, body, math.MaxInt)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
