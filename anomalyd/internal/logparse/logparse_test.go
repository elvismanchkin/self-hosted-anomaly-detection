package logparse

import (
	"encoding/json"
	"testing"
)

func TestExtract(t *testing.T) {
	e := NewExtractor("message", "service.name", "log.level", "@timestamp")
	cases := []struct {
		line string
		want [4]string
	}{
		{`{"@timestamp":"2026-09-14T00:00:00Z","service":{"name":"checkout"},"message":"Order 1 created","log":{"level":"INFO"}}`,
			[4]string{"Order 1 created", "checkout", "INFO", "2026-09-14T00:00:00Z"}},
		{`{"service.name":"auth","log.level":"error","message":"a \"quoted\" é \/ word","x":[1,{"y":"}"}],"n":-1.5e3}`,
			[4]string{`a "quoted" é / word`, "auth", "error", ""}},
		{` { "message" : "spaced" , "service" : { "other" : {"deep":[1,2]}, "name" : "gw" } } `,
			[4]string{"spaced", "gw", "", ""}},
		{`{"message":42,"service":{"name":null}}`, [4]string{"42", "null", "", ""}},
	}
	for _, c := range cases {
		got, ok := e.Extract(c.line)
		if !ok || [4]string(got) != c.want {
			t.Errorf("Extract(%s) = %q ok=%v, want %q", c.line, got, ok, c.want)
		}
	}
	for _, bad := range []string{`not json`, `{"message":"unterminated`, `[1,2]`, ``} {
		if _, ok := e.Extract(bad); ok {
			t.Errorf("Extract(%q) ok, want failure", bad)
		}
	}
}

func TestLevel(t *testing.T) {
	for in, want := range map[string]string{
		"ERROR db pool exhausted": "error", "[WARN] slow": "warn", "level=info msg=x": "info",
		`level="debug" x`: "debug", "User 1 logged in": "", "Upstream returned 503 ERROR": "error",
	} {
		if got := DetectLevel(in); got != want {
			t.Errorf("DetectLevel(%q) = %q, want %q", in, got, want)
		}
	}
}

var sample = `{"@timestamp":"2026-09-14T00:00:00.123Z","service":{"name":"checkout"},"log":{"level":"info"},"message":"Payment authorized order=1234567 provider=stripe latency_ms=1606","host":{"name":"web-01","ip":["10.0.0.1"]},"agent":{"type":"filebeat","version":"9.5.3"}}`

func BenchmarkExtract(b *testing.B) {
	e := NewExtractor("message", "service.name", "log.level")
	b.SetBytes(int64(len(sample)))
	for b.Loop() {
		e.Extract(sample)
	}
}

func BenchmarkStdlibJSON(b *testing.B) {
	type ev struct {
		Message string `json:"message"`
		Service struct {
			Name string `json:"name"`
		} `json:"service"`
		Log struct {
			Level string `json:"level"`
		} `json:"log"`
	}
	line := []byte(sample)
	b.SetBytes(int64(len(line)))
	for b.Loop() {
		var v ev
		_ = json.Unmarshal(line, &v)
	}
}
