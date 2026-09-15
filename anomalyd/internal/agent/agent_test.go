package agent

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/drain"
)

func testConfig(paths ...string) Config {
	return Config{Paths: paths, Format: "json", MessageField: "message", ServiceField: "service.name",
		LevelField: "log.level", FromStart: true, Once: true, MaxServices: 10, MaxBacklog: 1000, Step: 60,
		Drain: drain.DefaultConfig()}
}

func templates(a *Agent) map[string]int64 {
	got := map[string]int64{}
	a.Templates(func(svc string, c *drain.Cluster) { got[svc+"|"+c.Template()] += c.Size })
	return got
}

// Kubernetes layout: /var/log/containers/<pod>_<namespace>_<container>-<id>.log, CRI envelopes,
// long lines split by the runtime into P chunks, stdout and stderr interleaved in one file.
func TestContainerCRI(t *testing.T) {
	dir := t.TempDir()
	id := strings.Repeat("ab", 32)
	p := filepath.Join(dir, "checkout-7d9c8b5f4-x2x9q_shop_checkout-"+id+".log")
	lines := []string{
		`2026-09-15T10:00:00.000000001Z stdout F {"message":"Order 1 created for user 7","log":{"level":"info"}}`,
		`2026-09-15T10:00:00.000000002Z stdout F {"message":"Order 2 created for user 9","log":{"level":"info"}}`,
		`2026-09-15T10:00:00.000000003Z stdout P {"message":"Payment authorized order=5 `,
		`2026-09-15T10:00:00.000000004Z stderr F ERROR db pool exhausted after 30ms`,
		`2026-09-15T10:00:00.000000005Z stdout P provider=stripe `,
		`2026-09-15T10:00:00.000000006Z stdout F latency_ms=12","log":{"level":"info"}}`,
		`2026-09-15T10:00:00.000000007Z stdout F `,
	}
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := testConfig(filepath.Join(dir, "*.log"))
	c.Container = "cri"
	c.ServiceFromPath = regexp.MustCompile(`_(?P<service>[^_]+)-[0-9a-f]{64}\.log$`)
	a := New(c)
	if err := a.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{
		"checkout|Order <NUM> created for user <NUM>":                              2,
		"checkout|Payment authorized order=<NUM> provider=stripe latency_ms=<NUM>": 1,
		"checkout|ERROR db pool exhausted after <NUM>":                             1,
	}
	if got := templates(a); !equal(got, want) {
		t.Errorf("templates = %v, want %v", got, want)
	}
	if lines, _, _ := a.Stats(); lines != 4 {
		t.Errorf("lines = %d, want 4 (partials joined, empty line skipped)", lines)
	}
}

func TestContainerDocker(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c-json.log")
	lines := []string{
		`{"log":"GET /healthz 200 3ms\n","stream":"stdout","time":"2026-09-15T10:00:00.1Z"}`,
		`{"log":"GET /healthz 200 ","stream":"stdout","time":"2026-09-15T10:00:00.2Z"}`,
		`{"log":"5ms\r\n","stream":"stdout","time":"2026-09-15T10:00:00.3Z"}`,
		`plain line that is not an envelope`,
	}
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := testConfig(p)
	c.Container, c.Format, c.Service = "auto", "text", "gw"
	a := New(c)
	if err := a.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{"gw|GET /healthz <NUM> <NUM>": 2, "gw|plain line that is not an envelope": 1}
	if got := templates(a); !equal(got, want) {
		t.Errorf("templates = %v, want %v", got, want)
	}
}

// A deleted pod's file must be closed and forgotten, or its disk space stays allocated.
func TestRemovedFileIsForgotten(t *testing.T) {
	defer func(g time.Duration) { removeGrace = g }(removeGrace)
	removeGrace = 0
	dir := t.TempDir()
	p := filepath.Join(dir, "a.log")
	if err := os.WriteFile(p, []byte("hello 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := testConfig(filepath.Join(dir, "*.log"))
	c.Format, c.Service = "text", "svc"
	a := New(c)
	a.scan(true)
	a.pollAll()
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	a.pollAll() // first seen missing
	if len(a.files) != 1 {
		t.Fatalf("forgotten on the first missing poll")
	}
	a.pollAll()
	if len(a.files) != 0 {
		t.Fatalf("files = %d, want 0 after the grace period", len(a.files))
	}
	if err := os.WriteFile(p, []byte("hello 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.scan(false)
	a.pollAll()
	if lines, _, _ := a.Stats(); lines != 2 {
		t.Errorf("lines = %d, want 2 (a new file at the same path is read from the start)", lines)
	}
}

func equal(a, b map[string]int64) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
