package logparse

import "testing"

func TestInferLevel(t *testing.T) {
	cases := []struct{ tmpl, want string }{
		// error keywords, anywhere in the line
		{"panic: runtime error: invalid memory address or nil pointer dereference", "error"},
		{"thread <*> panicked at src/main.rs:<NUM>:<NUM>:", "error"},
		{"fatal error: all goroutines are asleep - deadlock!", "error"},
		{"Exception in thread <*> java.lang.OutOfMemoryError: Java heap space", "error"},
		{"worker <NUM> exited: java.lang.StackOverflowError", "error"},
		{"Out of memory: Killed process <NUM> (java)", "error"},
		{"out-of-memory while compacting", "error"},
		{"container app reason=OOMKilled exit=<NUM>", "error"},
		{"Traceback (most recent call last):", "error"},
		{"worker <NUM> got SIGSEGV", "error"},
		{"Segmentation fault (core dumped)", "error"},
		{"ERROR: deadlock detected", "error"},
		{"<NUM>-<NUM>-<NUM> <NUM>:<NUM>:<NUM> [main] [pool-<NUM>] com.acme.Billing ERROR charge failed", "error"},
		{"write /data/<*>: no space left on device", "error"},
		{"MemoryError", "error"},
		{"Unhandled exception, shutting down", "error"},
		// warn
		{"java.lang.NullPointerException: null", "warn"},
		{"KeyError: <*>", "warn"},
		{"Caused by: java.io.IOException: stream closed", "warn"},
		{"dial tcp <IP>:<NUM>: connect: connection refused", "warn"},
		{"read: Connection reset by peer", "warn"},
		{"Error: connect ECONNREFUSED <IP>:<NUM>", "warn"},
		{"WARNING: An illegal reflective access operation has occurred", "warn"},
		// the strongest match wins
		{"java.io.IOException: Too many open files", "error"},
		// not keywords
		{"Payment authorized order=<NUM> provider=stripe latency_ms=<NUM>", ""},
		{"batch done errors=<NUM> retries=<NUM>", ""},
		{"no error", ""},
		{"stats error_count=<NUM> panic_count=<NUM>", ""},
		{"Error rate <NUM>% over the last minute", ""},
		{"health fatal=<NUM> warn=<NUM>", ""},
		{"non-fatal error while flushing, retrying", ""},
		{`<IP> - - "GET /api/v1/panic HTTP/1.1" <NUM> <NUM>`, ""},
		{"Registered ErrorHandler and ExceptionMapper", ""},
		{"handleError called for <*>", ""},
		{"connection <*> refused", ""},
		{"Mirror sync done in <NUM>", ""},
		{"<*> <*> <NUM>", ""},
		{"oom_score_adj=<NUM> set for <*>", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := InferLevel(c.tmpl); got != c.want {
			t.Errorf("InferLevel(%q) = %q, want %q", c.tmpl, got, c.want)
		}
	}
}
