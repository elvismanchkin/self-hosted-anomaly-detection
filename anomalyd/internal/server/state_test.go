package server

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/drain"
	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/wire"
)

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
