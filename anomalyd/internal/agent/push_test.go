package agent

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/wire"
)

type fakeServer struct {
	mu     sync.Mutex
	pushes []wire.Push
	status int
	onPost func()
}

func (f *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	zr, err := gzip.NewReader(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var p wire.Push
	if err := json.NewDecoder(zr).Decode(&p); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.pushes = append(f.pushes, p)
	f.mu.Unlock()
	if f.onPost != nil {
		f.onPost()
	}
	if f.status != 0 {
		w.WriteHeader(f.status)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Counts for a template that the per-service LRU evicts before the push keep its text.
func TestPushKeepsTextOfEvictedTemplate(t *testing.T) {
	fs := &fakeServer{}
	srv := httptest.NewServer(fs)
	defer srv.Close()
	dir := t.TempDir()
	p := filepath.Join(dir, "app.log")
	data := "user 1 logged in\nuser 2 logged in\ncache warmed in 5 ms for region west\n"
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(p)
	cfg.Format, cfg.Service, cfg.Server = "text", "app", srv.URL
	cfg.Drain.MaxClusters = 1 // the second template evicts the first
	if err := New(cfg).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fs.pushes) != 1 {
		t.Fatalf("pushes = %d", len(fs.pushes))
	}
	var total int64
	for _, it := range fs.pushes[0].Items {
		if it.Template == "" {
			t.Errorf("item with empty template: %+v", it)
		}
		for _, c := range it.Counts {
			total += c[1]
		}
	}
	if total != 3 {
		t.Fatalf("pushed %d lines, want 3: %+v", total, fs.pushes[0].Items)
	}
}

// When a push fails with the backlog full, counts for keys already in the backlog are merged,
// not dropped: they don't make it any bigger.
func TestFailedPushMergesIntoExistingBacklogKeys(t *testing.T) {
	fs := &fakeServer{status: http.StatusServiceUnavailable}
	srv := httptest.NewServer(fs)
	defer srv.Close()
	dir := t.TempDir()
	p := filepath.Join(dir, "app.log")
	if err := os.WriteFile(p, []byte("user 1 logged in\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(p)
	cfg.Format, cfg.Service, cfg.Server, cfg.MaxBacklog = "text", "app", srv.URL, 1
	a := New(cfg)
	a.scan(true)
	a.pollAll()
	// While the push is in flight the tailer reads more lines of the same template.
	fs.onPost = func() {
		f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
		f.WriteString("user 2 logged in\n")
		f.Close()
		a.pollAll()
	}
	if err := a.push(context.Background()); err == nil {
		t.Fatal("push succeeded, want error")
	}
	if d := a.dropped.Load(); d != 0 {
		t.Fatalf("dropped %d keys that were already in the backlog", d)
	}
	var total int64
	for _, v := range a.counts {
		for _, n := range v.buckets {
			total += n
		}
	}
	if len(a.counts) != 1 || total != 2 {
		t.Fatalf("backlog = %d keys / %d lines, want 1 / 2", len(a.counts), total)
	}
}

// With nothing to send, the agent still pushes: an empty push is its heartbeat.
func TestEmptyPushIsHeartbeat(t *testing.T) {
	fs := &fakeServer{}
	srv := httptest.NewServer(fs)
	defer srv.Close()
	p := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(p)
	cfg.Server, cfg.Name = srv.URL, "node-1"
	if err := New(cfg).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fs.pushes) != 1 {
		t.Fatalf("pushes = %d, want 1 heartbeat", len(fs.pushes))
	}
	if h := fs.pushes[0]; h.Agent != "node-1" || h.Step != 60 || len(h.Items) != 0 {
		t.Fatalf("heartbeat = %+v", h)
	}
}

func TestMetricsBuildInfo(t *testing.T) {
	cfg := testConfig()
	cfg.Version = "v1.2.3"
	rec := httptest.NewRecorder()
	New(cfg).handleMetrics(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if want := `anomalyd_agent_build_info{version="v1.2.3",revision="unknown",goversion="go`; !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("/metrics lacks %q:\n%s", want, rec.Body)
	}
}
