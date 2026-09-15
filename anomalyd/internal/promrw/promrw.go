// Package promrw decodes and encodes Prometheus remote-write 1.0 requests
// (prometheus.WriteRequest, snappy block-compressed protobuf) without generated code.
//
// Only the fields anomalyd needs are read: labels and float samples. Exemplars, native
// histograms and metadata are skipped.
package promrw

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/klauspost/compress/snappy"
)

// TimeSeries is valid only during the Decode callback: label strings alias the request buffer.
type TimeSeries struct {
	Labels  []Label
	Samples []Sample
}

type Label struct{ Name, Value []byte }

type Sample struct {
	Value     float64
	Timestamp int64 // milliseconds
}

var errTrunc = errors.New("promrw: truncated message")

// Decompress checks the decoded size against max before allocating.
func Decompress(dst, body []byte, max int) ([]byte, error) {
	n, err := snappy.DecodedLen(body)
	if err != nil {
		return nil, fmt.Errorf("promrw: snappy header: %w", err)
	}
	if n > max {
		return nil, fmt.Errorf("promrw: decoded size %d exceeds limit %d", n, max)
	}
	if cap(dst) < n {
		dst = make([]byte, n)
	}
	out, err := snappy.Decode(dst[:n], body)
	if err != nil {
		return nil, fmt.Errorf("promrw: snappy: %w", err)
	}
	return out, nil
}

// Decode walks a WriteRequest and calls fn once per series. ts is reused between calls.
func Decode(buf []byte, ts *TimeSeries, fn func(*TimeSeries) error) error {
	for len(buf) > 0 {
		field, wt, n := tag(buf)
		if n <= 0 {
			return errTrunc
		}
		buf = buf[n:]
		if field == 1 && wt == 2 { // repeated TimeSeries timeseries = 1
			msg, rest, err := bytesField(buf)
			if err != nil {
				return err
			}
			buf = rest
			if err := decodeSeries(msg, ts); err != nil {
				return err
			}
			if err := fn(ts); err != nil {
				return err
			}
			continue
		}
		rest, err := skip(buf, wt)
		if err != nil {
			return err
		}
		buf = rest
	}
	return nil
}

func decodeSeries(buf []byte, ts *TimeSeries) error {
	ts.Labels, ts.Samples = ts.Labels[:0], ts.Samples[:0]
	for len(buf) > 0 {
		field, wt, n := tag(buf)
		if n <= 0 {
			return errTrunc
		}
		buf = buf[n:]
		if wt != 2 {
			rest, err := skip(buf, wt)
			if err != nil {
				return err
			}
			buf = rest
			continue
		}
		msg, rest, err := bytesField(buf)
		if err != nil {
			return err
		}
		buf = rest
		switch field {
		case 1: // Label labels = 1
			var l Label
			if err := decodeLabel(msg, &l); err != nil {
				return err
			}
			ts.Labels = append(ts.Labels, l)
		case 2: // Sample samples = 2
			var s Sample
			if err := decodeSample(msg, &s); err != nil {
				return err
			}
			ts.Samples = append(ts.Samples, s)
		}
	}
	return nil
}

func decodeLabel(buf []byte, l *Label) error {
	for len(buf) > 0 {
		field, wt, n := tag(buf)
		if n <= 0 {
			return errTrunc
		}
		buf = buf[n:]
		if wt != 2 {
			rest, err := skip(buf, wt)
			if err != nil {
				return err
			}
			buf = rest
			continue
		}
		v, rest, err := bytesField(buf)
		if err != nil {
			return err
		}
		buf = rest
		switch field {
		case 1:
			l.Name = v
		case 2:
			l.Value = v
		}
	}
	return nil
}

func decodeSample(buf []byte, s *Sample) error {
	for len(buf) > 0 {
		field, wt, n := tag(buf)
		if n <= 0 {
			return errTrunc
		}
		buf = buf[n:]
		switch {
		case field == 1 && wt == 1: // double value = 1
			if len(buf) < 8 {
				return errTrunc
			}
			s.Value = math.Float64frombits(binary.LittleEndian.Uint64(buf))
			buf = buf[8:]
		case field == 2 && wt == 0: // int64 timestamp = 2
			v, n := binary.Uvarint(buf)
			if n <= 0 {
				return errTrunc
			}
			s.Timestamp = int64(v)
			buf = buf[n:]
		default:
			rest, err := skip(buf, wt)
			if err != nil {
				return err
			}
			buf = rest
		}
	}
	return nil
}

func tag(buf []byte) (field uint64, wireType uint8, n int) {
	v, n := binary.Uvarint(buf)
	return v >> 3, uint8(v & 7), n
}

func bytesField(buf []byte) (msg, rest []byte, err error) {
	l, n := binary.Uvarint(buf)
	if n <= 0 || uint64(len(buf)-n) < l {
		return nil, nil, errTrunc
	}
	return buf[n : n+int(l)], buf[n+int(l):], nil
}

func skip(buf []byte, wt uint8) ([]byte, error) {
	switch wt {
	case 0:
		_, n := binary.Uvarint(buf)
		if n <= 0 {
			return nil, errTrunc
		}
		return buf[n:], nil
	case 1:
		if len(buf) < 8 {
			return nil, errTrunc
		}
		return buf[8:], nil
	case 2:
		_, rest, err := bytesField(buf)
		return rest, err
	case 5:
		if len(buf) < 4 {
			return nil, errTrunc
		}
		return buf[4:], nil
	}
	return nil, fmt.Errorf("promrw: unsupported wire type %d", wt)
}

// Encoder builds WriteRequests (used by the synthetic sender and tests).
type Encoder struct{ buf, tmp []byte }

type Series struct {
	Labels  [][2]string // sorted by name, including __name__
	Samples []Sample
}

// Encode returns the snappy-compressed WriteRequest for series.
func (e *Encoder) Encode(series []Series) []byte {
	e.buf = e.buf[:0]
	for _, s := range series {
		e.tmp = e.tmp[:0]
		for _, l := range s.Labels {
			lb := appendBytes(appendBytes(nil, 1, []byte(l[0])), 2, []byte(l[1]))
			e.tmp = appendBytes(e.tmp, 1, lb)
		}
		for _, smp := range s.Samples {
			var sb []byte
			sb = binary.AppendUvarint(sb, 1<<3|1)
			sb = binary.LittleEndian.AppendUint64(sb, math.Float64bits(smp.Value))
			sb = binary.AppendUvarint(sb, 2<<3|0)
			sb = binary.AppendUvarint(sb, uint64(smp.Timestamp))
			e.tmp = appendBytes(e.tmp, 2, sb)
		}
		e.buf = appendBytes(e.buf, 1, e.tmp)
	}
	return snappy.Encode(nil, e.buf)
}

func appendBytes(dst []byte, field uint64, v []byte) []byte {
	dst = binary.AppendUvarint(dst, field<<3|2)
	dst = binary.AppendUvarint(dst, uint64(len(v)))
	return append(dst, v...)
}
