package server

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/wire"
)

// FuzzHandlePush posts arbitrary agent pushes: plain, gzip-compressed, or not gzip at all
// despite the header. The handler must answer 2xx or 4xx, never panic, and keep every
// service name it creates valid UTF-8 (Prometheus and Alertmanager reject anything else).
func FuzzHandlePush(f *testing.F) {
	now := time.Now().Unix()
	for _, p := range []wire.Push{
		{Agent: "host-a", Step: 60, Items: []wire.Item{{Service: "checkout", Template: "GET /cart <NUM> took <NUM>",
			Level: "info", Counts: [][2]int64{{now - 120, 7}, {now - 60, 3}}}}},
		{Items: []wire.Item{{Service: " ", Template: "   ", Counts: [][2]int64{{-60, 1}, {now + 86400, 1}, {now, -5}}}}},
		{Items: []wire.Item{{Service: "paiement-é", Template: "a <*> b", Level: "ERROR",
			Counts: [][2]int64{{now, 1 << 62}, {now, 1 << 62}}}}},
	} {
		b, _ := json.Marshal(p)
		for mode := range uint8(3) {
			f.Add(b, mode)
		}
	}
	f.Add([]byte(`{"items":[{"counts":[[1,2,3]]}]}`), uint8(0))
	f.Add([]byte(`{"items":null}`), uint8(1))
	s, err := New(testConfig())
	if err != nil {
		f.Fatal(err)
	}
	h := s.Handler()
	f.Fuzz(func(t *testing.T, body []byte, mode uint8) {
		gz := mode%3 != 0
		if mode%3 == 1 {
			var zb bytes.Buffer
			zw := gzip.NewWriter(&zb)
			zw.Write(body)
			zw.Close()
			body = zb.Bytes()
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/templates", bytes.NewReader(body))
		if gz {
			req.Header.Set("Content-Encoding", "gzip")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if c := rec.Code / 100; c != 2 && c != 4 {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		for _, svc := range s.pool.Keys() {
			if !utf8.ValidString(svc) {
				t.Fatalf("service %q is not valid UTF-8", svc)
			}
		}
	})
}

func TestCleanServiceKeepsUTF8(t *testing.T) {
	long := strings.Repeat("s", 127)
	for in, want := range map[string]string{
		long + "é":     long, // byte 128 is the middle of é
		"\xffcheckout": "checkout",
		" \xff ":       "unknown",
		" search\n":    "search",
	} {
		if got := cleanService(in); got != want {
			t.Errorf("cleanService(%q) = %q, want %q", in, got, want)
		}
	}
	// Raw lines are not JSON, so the query parameter reaches the miner pool as bytes.
	s, _ := New(testConfig())
	post(t, s.Handler(), "/api/v1/logs?service=%ffpay", "text/plain", []byte("payment failed\n"))
	for _, svc := range s.pool.Keys() {
		if svc != "pay" {
			t.Fatalf("service %q, want pay", svc)
		}
	}
}
