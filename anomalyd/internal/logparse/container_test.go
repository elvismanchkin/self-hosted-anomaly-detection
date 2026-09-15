package logparse

import "testing"

func TestUnwrap(t *testing.T) {
	cases := []struct {
		format, line string
		want         Envelope
		ok           bool
	}{
		{"cri", `2026-09-15T10:00:00.123456789Z stdout F {"message":"Order 1 created"}`, Envelope{Text: `{"message":"Order 1 created"}`}, true},
		{"cri", "2026-09-15T10:00:00.1Z stderr P first half ", Envelope{Text: "first half ", Stderr: true, Partial: true}, true},
		{"cri", "2026-09-15T10:00:00.1Z stdout F ", Envelope{}, true},
		{"cri", "2026-09-15T10:00:00.1Z stdout F", Envelope{}, true},
		{"cri", "2026-09-15T10:00:00.1Z stdout F:x tagged", Envelope{Text: "tagged"}, true},
		{"cri", "2026-09-15T10:00:00.1Z stdin F nope", Envelope{}, false},
		{"cri", "ERROR plain text line", Envelope{}, false},
		{"docker", `{"log":"GET /healthz 200\n","stream":"stdout","time":"2026-09-15T10:00:00.1Z"}`, Envelope{Text: "GET /healthz 200"}, true},
		{"docker", `{"log":"{\"message\":\"a \\\"q\\\"\"}\n","stream":"stderr","time":"x"}`, Envelope{Text: `{"message":"a \"q\""}`, Stderr: true}, true},
		{"docker", `{"log":"chunk","stream":"stdout","time":"x"}`, Envelope{Text: "chunk", Partial: true}, true},
		{"docker", `{"message":"app JSON, not an envelope"}`, Envelope{}, false},
		{"auto", `2026-09-15T10:00:00.1Z stdout F cri line`, Envelope{Text: "cri line"}, true},
		{"auto", `{"log":"docker line\n","stream":"stdout","time":"x"}`, Envelope{Text: "docker line"}, true},
		{"auto", `{"message":"raw app line"}`, Envelope{}, false},
	}
	for _, c := range cases {
		got, ok := NewUnwrapper(c.format).Unwrap(c.line)
		if ok != c.ok || ok && got != c.want {
			t.Errorf("%s Unwrap(%q) = %+v ok=%v, want %+v ok=%v", c.format, c.line, got, ok, c.want, c.ok)
		}
	}
}

func BenchmarkUnwrapCRI(b *testing.B) {
	u := NewUnwrapper("cri")
	line := "2026-09-15T10:00:00.123456789Z stdout F " + sample
	b.SetBytes(int64(len(line)))
	for b.Loop() {
		u.Unwrap(line)
	}
}
