package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/series"
	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/wire"
)

func findingBy(s *Server, kind, label, value string) *Finding {
	s.findMu.Lock()
	defer s.findMu.Unlock()
	for _, f := range s.active {
		if f.Kind == kind && f.Labels[label] == value {
			return f
		}
	}
	return nil
}

type tmplRow struct {
	ID         int      `json:"template_id"`
	Template   string   `json:"template"`
	Inferred   string   `json:"inferred_level"`
	Signatures []string `json:"signatures"`
}

func templates(t *testing.T, h http.Handler, svc string) map[string]tmplRow {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/templates?service="+svc, nil))
	var rows []tmplRow
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	out := map[string]tmplRow{}
	for _, r := range rows {
		out[r.Template] = r
	}
	return out
}

// Lines without a level (or at info) are rated by keywords in their template; the level label
// stays the observed one.
func TestKeywordSeverity(t *testing.T) {
	s, _ := New(testConfig())
	h := s.Handler()
	now := time.Now().Unix() / 60 * 60
	push(t, h, wire.Item{Service: "warm", Template: "started", Level: "info", Counts: [][2]int64{{now, 1}}})
	s.logStart.Store(time.Now().Add(-2 * time.Hour).Unix())
	oom := "Exception in thread <*> java.lang.OutOfMemoryError: Java heap space"
	push(t, h,
		wire.Item{Service: "billing", Template: oom, Counts: [][2]int64{{now, 6}}},
		wire.Item{Service: "mailer", Template: "Invoice <NUM> sent to <EMAIL>", Level: "info", Counts: [][2]int64{{now, 6}}},
		wire.Item{Service: "api", Template: "handler failed: NullPointerException", Level: "error", Counts: [][2]int64{{now, 6}}},
		wire.Item{Service: "batch", Template: "retry after KeyError: <*>", Level: "debug", Counts: [][2]int64{{now, 6}}})
	s.Evaluate(time.Now())

	for _, c := range []struct{ svc, level, sev, source string }{
		{"billing", "unknown", "warning", "template keywords"},
		{"mailer", "info", "info", ""},
		{"api", "error", "warning", ""},
		{"batch", "debug", "info", ""}, // debug is a level: keywords do not override it
	} {
		f := findingBy(s, kindLogNew, "service", c.svc)
		if f == nil || f.Labels["level"] != c.level || f.Labels["severity"] != c.sev || f.Annotations["severity_source"] != c.source {
			t.Errorf("%s: new-template finding %+v, want level=%s severity=%s source=%q", c.svc, f, c.level, c.sev, c.source)
		}
	}

	// A rate finding on the same template is rated the same way.
	f := findingBy(s, kindLogNew, "service", "billing")
	tl := series.Labels{{Name: "__name__", Value: nameTemplate}, {Name: "service", Value: "billing"},
		{Name: "template_id", Value: f.Labels["template_id"]}}
	rf := s.newFinding(event{kind: kindLogTemplate, labels: tl, dir: +1}, time.Now())
	if rf.Labels["level"] != "unknown" || rf.Labels["severity"] != "warning" || rf.Annotations["severity_source"] != "template keywords" {
		t.Fatalf("rate finding: %+v %+v", rf.Labels, rf.Annotations)
	}
}

// The keyword level follows the template text as Drain generalises it (agent pushes and raw
// lines), and is derived again after a restore rather than saved.
func TestKeywordLevelFollowsTemplate(t *testing.T) {
	cfg := testConfig()
	cfg.StateDir = t.TempDir()
	s, _ := New(cfg)
	h := s.Handler()
	now := time.Now().Unix() / 60 * 60
	push(t, h, wire.Item{Service: "svc", Template: "Caught NullPointerException in handler", Counts: [][2]int64{{now, 1}}},
		wire.Item{Service: "svc", Template: "Out of memory: Killed process <NUM> (java)", Counts: [][2]int64{{now, 1}}})
	if got := templates(t, h, "svc")["Caught NullPointerException in handler"].Inferred; got != "warn" {
		t.Fatalf("inferred = %q, want warn", got)
	}
	push(t, h, wire.Item{Service: "svc", Template: "Caught timeout in handler", Counts: [][2]int64{{now, 1}}})
	if r, ok := templates(t, h, "svc")["Caught <*> in handler"]; !ok || r.Inferred != "" {
		t.Fatalf("generalised template: %+v ok=%v, want no inferred level", r, ok)
	}

	body := "worker 1 hit OutOfMemoryError now\n"
	if rec := post(t, h, "/api/v1/logs?service=raw", "text/plain", []byte(body)); rec.Code != http.StatusNoContent {
		t.Fatalf("logs: %d", rec.Code)
	}
	if got := templates(t, h, "raw")["worker <NUM> hit OutOfMemoryError now"].Inferred; got != "error" {
		t.Fatalf("raw line: inferred = %q, want error", got)
	}
	if rec := post(t, h, "/api/v1/logs?service=raw", "text/plain", []byte("worker 2 hit deadline now\n")); rec.Code != http.StatusNoContent {
		t.Fatalf("logs: %d", rec.Code)
	}
	if r, ok := templates(t, h, "raw")["worker <NUM> hit <*> now"]; !ok || r.Inferred != "" {
		t.Fatalf("generalised raw template: %+v ok=%v", r, ok)
	}

	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	s2, _ := New(cfg)
	if err := s2.load(); err != nil {
		t.Fatal(err)
	}
	if got := templates(t, s2.Handler(), "svc")["Out of memory: Killed process <NUM> (java)"].Inferred; got != "error" {
		t.Fatalf("after restore: inferred = %q, want error", got)
	}
}

func TestParseSignatures(t *testing.T) {
	sigs, err := ParseSignatures(strings.NewReader("# known-bad\n\npool: db pool exhausted: timeout after <NUM>\n  oom : (?i)out of memory|OutOfMemoryError  \n"))
	if err != nil || len(sigs) != 2 || sigs[0].Name != "pool" || sigs[1].Name != "oom" ||
		!sigs[1].Re.MatchString("java.lang.OutOfMemoryError") || sigs[1].Re.String() != "(?i)out of memory|OutOfMemoryError" {
		t.Fatalf("got %+v, %v", sigs, err)
	}
	for in, want := range map[string]string{
		"a: x\n# c\nbad: (unclosed\n": "line 3",
		"no colon here\n":             "line 1",
		"a: x\na: y\n":                `line 2: duplicate signature "a"`,
		"has space: x\n":              "line 1",
		"empty:\n":                    "line 1",
	} {
		if _, err := ParseSignatures(strings.NewReader(in)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseSignatures(%q) err = %v, want %q", in, err, want)
		}
	}
}

// A template matching a signature fires on its first lines, below --novelty-min-lines and
// during warm-up, at most once per service and signature per day, also across restarts.
func TestSignatureFinding(t *testing.T) {
	cfg := testConfig()
	cfg.StateDir = t.TempDir()
	var err error
	cfg.Signatures, err = ParseSignatures(strings.NewReader("pool: db pool exhausted\nunused: never matches\n"))
	if err != nil {
		t.Fatal(err)
	}
	s, _ := New(cfg)
	h := s.Handler()
	now := time.Now()
	ts := now.Unix() / 60 * 60
	t1 := "ERROR db pool exhausted: timeout after <NUM>"
	t2 := "WARN db pool exhausted for tenant <*> after retries"
	push(t, h, wire.Item{Service: "checkout", Template: "Payment authorized order=<NUM>", Level: "info", Counts: [][2]int64{{ts, 50}}},
		wire.Item{Service: "checkout", Template: t1, Level: "error", Counts: [][2]int64{{ts, 1}}})
	s.Evaluate(now)
	f := findingBy(s, kindLogSignature, "service", "checkout")
	if f == nil || f.Labels["alertname"] != "AnomalydLogSignature" || f.Labels["signature"] != "pool" ||
		f.Labels["tier"] != "1" || f.Labels["severity"] != "warning" || f.Labels["template_id"] == "" ||
		f.Annotations["template"] != t1 {
		t.Fatalf("signature finding: %+v", f)
	}
	if countKind(s, kindLogNew) != 0 {
		t.Fatal("novelty fired during warm-up")
	}
	if r := templates(t, h, "checkout")[t1]; len(r.Signatures) != 1 || r.Signatures[0] != "pool" {
		t.Fatalf("templates endpoint: %+v", r)
	}

	// More lines, and another template with the same signature: still one finding.
	push(t, h, wire.Item{Service: "checkout", Template: t1, Level: "error", Counts: [][2]int64{{ts, 3}}},
		wire.Item{Service: "checkout", Template: t2, Level: "warn", Counts: [][2]int64{{ts, 2}}})
	s.Evaluate(now.Add(time.Minute))
	if countKind(s, kindLogSignature) != 1 {
		t.Fatalf("want one signature finding, got %d", countKind(s, kindLogSignature))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	for _, want := range []string{`anomalyd_signature_matches_total{signature="pool"} 6`,
		`anomalyd_signature_matches_total{signature="unused"} 0`, `anomalyd_findings_active{kind="log_signature"} 1`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("/metrics lacks %s", want)
		}
	}

	// Restart while firing: the finding keeps its start time and still resolves after --novelty-ttl.
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	s2, _ := New(cfg)
	if err := s2.load(); err != nil {
		t.Fatal(err)
	}
	if f2 := findingBy(s2, kindLogSignature, "service", "checkout"); f2 == nil || !f2.StartsAt.Equal(f.StartsAt) {
		t.Fatalf("after restore: %+v", f2)
	}
	s2.Evaluate(now.Add(cfg.NoveltyTTL + time.Minute))
	if countKind(s2, kindLogSignature) != 0 {
		t.Fatal("signature finding did not resolve after --novelty-ttl")
	}

	// Resolved, restarted again, more matching lines within the day: no second finding.
	if err := s2.save(); err != nil {
		t.Fatal(err)
	}
	s3, _ := New(cfg)
	if err := s3.load(); err != nil {
		t.Fatal(err)
	}
	h3 := s3.Handler()
	push(t, h3, wire.Item{Service: "checkout", Template: t2, Level: "warn", Counts: [][2]int64{{ts, 2}}})
	s3.Evaluate(now.Add(3 * time.Hour))
	if countKind(s3, kindLogSignature) != 0 {
		t.Fatal("second signature finding within 24 h")
	}
	push(t, h3, wire.Item{Service: "checkout", Template: t2, Level: "warn", Counts: [][2]int64{{ts, 2}}})
	s3.Evaluate(now.Add(25 * time.Hour))
	if f3 := findingBy(s3, kindLogSignature, "service", "checkout"); f3 == nil || f3.Annotations["template"] != t2 {
		t.Fatalf("no signature finding after 24 h: %+v", f3)
	}
}
