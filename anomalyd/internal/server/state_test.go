package server

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/drain"
	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/promrw"
	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/wire"
)

// gate holds the first snapshot's writes until open is called.
type gate struct {
	entered, release chan struct{}
	enter, open      func()
	saves            atomic.Int32 // saves that reached the writer
}

func (g *gate) Write(p []byte) (int, error) { g.enter(); <-g.release; return len(p), nil }

// gateSnapshots routes the first snapshot through a gate (its bytes reach the file once the
// gate opens); later ones write straight through.
func gateSnapshots(t *testing.T) *gate {
	g := &gate{entered: make(chan struct{}), release: make(chan struct{})}
	g.enter = sync.OnceFunc(func() { close(g.entered) })
	g.open = sync.OnceFunc(func() { close(g.release) })
	old := stateWriter
	stateWriter = func(w io.Writer) io.Writer {
		if g.saves.Add(1) > 1 {
			return w
		}
		return io.MultiWriter(g, w)
	}
	t.Cleanup(func() { g.open(); stateWriter = old })
	return g
}

func waitFor(t *testing.T, what string, c <-chan struct{}) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func readState(t *testing.T, dir string) stateFile {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "anomalyd.state"))
	if err != nil {
		t.Fatal(err)
	}
	var st stateFile
	if err := gob.NewDecoder(bytes.NewReader(b)).Decode(&st); err != nil {
		t.Fatal(err)
	}
	return st
}

// A snapshot stuck on a slow disk must not hold the locks agent pushes need: they time out
// after 15 s and retry. The snapshot keeps the state from before the push.
func TestSaveDoesNotBlockPushes(t *testing.T) {
	cfg := testConfig()
	cfg.StateDir = t.TempDir()
	s, _ := New(cfg)
	h := s.Handler()
	now := time.Now().Unix() / 60 * 60
	known := wire.Item{Service: "svc", Template: "user <*> logged in", Level: "info", Counts: [][2]int64{{now, 5}}}
	push(t, h, known)
	s.active["k"] = &Finding{ID: "x", Kind: kindLogNew, Labels: map[string]string{"service": "svc"},
		Annotations: map[string]string{"summary": "before"}}

	g := gateSnapshots(t)
	saved := make(chan struct{})
	var err error
	go func() { defer close(saved); err = s.save() }()
	t.Cleanup(func() { g.open(); <-saved })
	waitFor(t, "the snapshot write", g.entered)

	pushed := make(chan struct{})
	var code int
	go func() {
		defer close(pushed)
		b, _ := json.Marshal(wire.Push{Agent: "host-a", Step: 60, Items: []wire.Item{known,
			{Service: "svc", Template: "disk <*> full", Level: "error", Counts: [][2]int64{{now, 3}}}}})
		code = post(t, h, "/api/v1/templates", "application/json", b).Code
	}()
	waitFor(t, "a push during the snapshot write", pushed)
	if code != http.StatusNoContent {
		t.Fatalf("push: %d", code)
	}
	s.findMu.Lock()
	s.active["k"].Annotations["summary"] = "after" // as evaluation does
	s.findMu.Unlock()

	g.open()
	<-saved
	if err != nil {
		t.Fatal(err)
	}
	st := readState(t, cfg.StateDir)
	if cs := st.Templates["svc"]; len(cs) != 1 || cs[0].Levels["info"] != 5 {
		t.Fatalf("snapshot templates = %+v, want the one from before the push with 5 info lines", cs)
	}
	if len(st.Findings) != 1 || st.Findings[0].Finding.Annotations["summary"] != "before" {
		t.Fatalf("snapshot findings = %+v, want the annotation from before the update", st.Findings)
	}
}

// Run snapshots in the background: a stuck save neither stalls evaluation nor starts a second
// save, and shutdown waits for it before the final save.
func TestRunSnapshotsInBackground(t *testing.T) {
	cfg := testConfig()
	cfg.Listen = "127.0.0.1:0"
	cfg.StateDir = t.TempDir()
	cfg.EvalInterval = 5 * time.Millisecond
	cfg.SnapshotEvery = 5 * time.Millisecond
	s, _ := New(cfg)
	g := gateSnapshots(t)
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	var err error
	go func() { defer close(stopped); err = s.Run(ctx) }()
	t.Cleanup(func() { cancel(); g.open(); <-stopped })
	waitFor(t, "the first snapshot", g.entered)

	s.stats.evalNanos.Store(-1)
	evaluated := make(chan struct{})
	go func() {
		defer close(evaluated)
		for s.stats.evalNanos.Load() < 0 {
			time.Sleep(time.Millisecond)
		}
	}()
	waitFor(t, "an evaluation during the snapshot", evaluated)
	time.Sleep(30 * time.Millisecond) // several snapshot ticks
	if n := g.saves.Load(); n != 1 {
		t.Fatalf("%d snapshots running, want 1", n)
	}

	cancel()
	select {
	case <-stopped:
		t.Fatal("Run returned before the running snapshot finished")
	case <-time.After(50 * time.Millisecond):
	}
	g.open()
	waitFor(t, "Run to return", stopped)
	if err != nil {
		t.Fatal(err)
	}
	if n := g.saves.Load(); n < 2 {
		t.Fatalf("saves = %d, want the running one and the final one", n)
	}
	readState(t, cfg.StateDir)
}

func metricValue(t *testing.T, h http.Handler, name string) float64 {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	for line := range strings.Lines(rec.Body.String()) {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), name+" "); ok {
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				t.Fatal(err)
			}
			return f
		}
	}
	t.Fatalf("%s not in /metrics", name)
	return 0
}

func TestSnapshotMetrics(t *testing.T) {
	cfg := testConfig()
	dir := t.TempDir()
	cfg.StateDir = filepath.Join(dir, "file", "state") // under a regular file: cannot be created
	if err := os.WriteFile(filepath.Join(dir, "file"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	s, _ := New(cfg)
	h := s.Handler()
	if err := s.save(); err == nil {
		t.Fatal("save succeeded under a regular file")
	}
	if v := metricValue(t, h, "anomalyd_snapshot_failures_total"); v != 1 {
		t.Fatalf("failures = %v, want 1", v)
	}
	if v := metricValue(t, h, "anomalyd_snapshot_last_success_timestamp_seconds"); v != 0 {
		t.Fatalf("last success = %v before any good save", v)
	}
	s.cfg.StateDir = dir
	before := time.Now().Unix()
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	if v := metricValue(t, h, "anomalyd_snapshot_last_success_timestamp_seconds"); v < float64(before) {
		t.Fatalf("last success = %v, want >= %d", v, before)
	}
	if v := metricValue(t, h, "anomalyd_snapshot_duration_seconds"); v <= 0 {
		t.Fatalf("duration = %v", v)
	}
	if v := metricValue(t, h, "anomalyd_snapshot_failures_total"); v != 1 {
		t.Fatalf("failures = %v, want 1", v)
	}
}

func captureLog(t *testing.T) *bytes.Buffer {
	var b bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&b, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &b
}

// A changed --step or ring size skips the saved series; load says so with old and new values.
func TestLoadWarnsOnShapeMismatch(t *testing.T) {
	cfg := testConfig()
	cfg.StateDir = t.TempDir()
	s, _ := New(cfg)
	h := s.Handler()
	now := time.Now().Unix() / 60 * 60
	push(t, h, wire.Item{Service: "svc", Template: "user <*> logged in", Level: "info", Counts: [][2]int64{{now, 5}}})
	var enc promrw.Encoder
	rw := []promrw.Series{{Labels: [][2]string{{"__name__", "m"}, {"service", "a"}},
		Samples: []promrw.Sample{{Value: 1, Timestamp: now * 1000}}}}
	if rec := post(t, h, "/api/v1/write", "application/x-protobuf", enc.Encode(rw)); rec.Code != http.StatusNoContent {
		t.Fatalf("remote write: %d", rec.Code)
	}
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	buckets := func(n int) string { return strconv.Itoa(n) }
	for _, tc := range []struct {
		name       string
		change     func(*Config)
		prom, logs int // series restored
		want       func(s2 *Server) []string
	}{
		{"same", func(*Config) {}, 1, 2, nil},
		{"step", func(c *Config) { c.Step = 2 * time.Minute }, 0, 0, func(*Server) []string {
			return []string{"snapshot step differs", "old_step=1m0s", "new_step=2m0s", "metric_series=1", "log_series=2"}
		}},
		{"metric ring", func(c *Config) { c.PromSeasons = 2 }, 0, 2, func(s2 *Server) []string {
			return []string{"metric ring size differs", "old_buckets=" + buckets(s.prom.Capacity()),
				"new_buckets=" + buckets(s2.prom.Capacity()), "metric_series=1"}
		}},
		{"log ring", func(c *Config) { c.LogSeasons = 2 }, 1, 0, func(s2 *Server) []string {
			return []string{"log ring size differs", "old_buckets=" + buckets(s.logs.Capacity()),
				"new_buckets=" + buckets(s2.logs.Capacity()), "log_series=2"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureLog(t)
			c := cfg
			tc.change(&c)
			s2, err := New(c)
			if err != nil {
				t.Fatal(err)
			}
			if err := s2.load(); err != nil {
				t.Fatal(err)
			}
			if s2.prom.Len() != tc.prom || s2.logs.Len() != tc.logs {
				t.Fatalf("restored %d metric and %d log series, want %d and %d", s2.prom.Len(), s2.logs.Len(), tc.prom, tc.logs)
			}
			if n := len(s2.tmpl); n != 1 {
				t.Fatalf("templates = %d, want 1 (they always load)", n)
			}
			out := logs.String()
			if tc.want == nil {
				if out != "" {
					t.Fatalf("unexpected warning: %s", out)
				}
				return
			}
			for _, w := range tc.want(s2) {
				if !strings.Contains(out, w) {
					t.Errorf("warning lacks %q: %s", w, out)
				}
			}
		})
	}
}

// save must not read template tokens that concurrent raw-log mining generalises in place
// (go test -race).
func TestSaveConcurrentWithMining(t *testing.T) {
	cfg := testConfig()
	cfg.StateDir = t.TempDir()
	s, _ := New(cfg)
	h := s.Handler()
	word := func(i int) string { // letters only: digits would send the token to a wildcard node
		return string([]byte{'a' + byte(i/676%26), 'a' + byte(i/26%26), 'a' + byte(i%26)})
	}
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(done)
		for i := range 400 {
			// A new template, then a variant: the miner widens the first one's tokens in place.
			// Raw lines are mined under the miner lock only (agent pushes also hold tmplMu).
			w := word(i)
			body := w + " cache miss for key alpha\n" + w + " cache miss for key beta\n"
			if rec := post(t, h, "/api/v1/logs?service=svc", "text/plain", []byte(body)); rec.Code != http.StatusNoContent {
				t.Errorf("logs: %d", rec.Code)
				return
			}
		}
	}()
	for {
		select {
		case <-done:
			wg.Wait()
			return
		default:
		}
		if err := s.save(); err != nil {
			t.Fatal(err)
		}
	}
}

// Lowering --max-services folds several services into the overflow miner on restore; their
// template ids overlap and must not overwrite each other.
func TestLoadFoldsServicesWithoutIDCollision(t *testing.T) {
	cfg := testConfig()
	cfg.StateDir = t.TempDir()
	s, _ := New(cfg)
	h := s.Handler()
	now := time.Now().Unix() / 60 * 60
	// Distinct lengths per service so the shared overflow miner cannot merge them.
	tmpls := map[string][2]string{
		"a": {"alpha started", "ERROR alpha down"},
		"b": {"bravo started on port <NUM>", "ERROR bravo cannot reach the database now"},
		"c": {"charlie started on port <NUM> with <NUM> workers ok", "ERROR charlie lost its lease after <NUM> retries and will exit now"},
	}
	for svc, tt := range tmpls {
		push(t, h, wire.Item{Service: svc, Template: tt[0], Level: "info", Counts: [][2]int64{{now, 5}}},
			wire.Item{Service: svc, Template: tt[1], Level: "error", Counts: [][2]int64{{now, 2}}})
	}
	if err := s.save(); err != nil {
		t.Fatal(err)
	}

	cfg.MaxServices = 1 // "a" or another keeps its own miner, the rest share _other
	s2, _ := New(cfg)
	if err := s2.load(); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	nClusters := 0
	s2.pool.Each(func(svc string, m *drain.Miner) {
		m.Clusters(func(c *drain.Cluster) {
			nClusters++
			got[c.Template()] = true
			if m.Get(c.ID) != c {
				t.Errorf("%s: id %d maps to another cluster", svc, c.ID)
			}
			s2.tmplMu.Lock()
			meta := s2.tmpl[tmplKey{svc, c.ID}]
			s2.tmplMu.Unlock()
			if meta == nil {
				t.Errorf("%s/%d %q: no metadata", svc, c.ID, c.Template())
			}
		})
	})
	if nClusters != 6 {
		t.Fatalf("restored %d templates, want 6: %v", nClusters, got)
	}
	for svc, tt := range tmpls {
		if !got[tt[0]] || !got[tt[1]] {
			t.Errorf("lost template for %s: %v", svc, got)
		}
	}

	// gc keeps every restored template's metadata (a collision left some unreachable).
	s2.gcRuns = 99
	s2.gc(0, 0)
	s2.tmplMu.Lock()
	defer s2.tmplMu.Unlock()
	if len(s2.tmpl) != 6 {
		t.Fatalf("template metadata after gc = %d, want 6", len(s2.tmpl))
	}
}
