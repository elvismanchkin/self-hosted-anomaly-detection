package server

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/promrw"
)

// A resolve is re-sent until an Alertmanager accepts it; one healthy peer is enough.
func TestResolveRetriedUntilDelivered(t *testing.T) {
	am := &fakeAM{}
	var fail atomic.Bool
	fail.Store(true)
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		am.ServeHTTP(w, r)
	}))
	defer good.Close()
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer dead.Close()

	cfg := testConfig()
	cfg.AlertmanagerURLs = []string{dead.URL, good.URL}
	s, _ := New(cfg)
	now := time.Now()
	f := &Finding{ID: "x", key: "x", State: "firing", StartsAt: now.Add(-time.Hour),
		Labels: map[string]string{"alertname": "AnomalydMetricAnomaly"}, Annotations: map[string]string{}}
	s.active["x"] = f
	s.findMu.Lock()
	s.resolveLocked(f, now)
	s.findMu.Unlock()

	s.sendAlerts(now)
	if f.resolveSent {
		t.Fatal("resolve marked sent although every Alertmanager failed")
	}
	fail.Store(false)
	s.sendAlerts(now)
	a, ok := am.last("AnomalydMetricAnomaly")
	if !f.resolveSent || !ok || !a.EndsAt.Equal(now) {
		t.Fatalf("resolve not delivered: sent=%v ok=%v %+v", f.resolveSent, ok, a)
	}
	am.mu.Lock()
	n := len(am.alerts)
	am.mu.Unlock()
	s.sendAlerts(now)
	am.mu.Lock()
	defer am.mu.Unlock()
	if len(am.alerts) != n {
		t.Fatal("resolve sent again after delivery")
	}
}

// A firing metric finding survives a restart with its start time and resolves normally.
func TestMetricFindingSurvivesRestart(t *testing.T) {
	cfg := testConfig()
	cfg.StateDir = t.TempDir()
	s, _ := New(cfg)
	end := time.Now().Unix() / 60 * 60
	var enc promrw.Encoder
	if rec := post(t, s.Handler(), "/api/v1/write", "application/x-protobuf", enc.Encode(history(end, 5))); rec.Code != http.StatusNoContent {
		t.Fatalf("remote write: %d", rec.Code)
	}
	s.Evaluate(time.Now())
	if countKind(s, kindMetric) != 1 {
		t.Fatalf("want one metric finding before restart, got %d", countKind(s, kindMetric))
	}
	var started time.Time
	for _, f := range s.active {
		started = f.StartsAt
	}
	if err := s.save(); err != nil {
		t.Fatal(err)
	}

	s2, _ := New(cfg)
	if err := s2.load(); err != nil {
		t.Fatal(err)
	}
	if countKind(s2, kindMetric) != 1 {
		t.Fatalf("finding lost on restart")
	}
	var key string
	for k, f := range s2.active {
		key = k
		if !f.StartsAt.Equal(started) {
			t.Fatalf("start time %v, want %v", f.StartsAt, started)
		}
	}
	// Back to normal for two buckets: the restored finding resolves through the usual path.
	if rec := post(t, s2.Handler(), "/api/v1/write", "application/x-protobuf", enc.Encode(history(end+180, 0))); rec.Code != http.StatusNoContent {
		t.Fatalf("remote write: %d", rec.Code)
	}
	s2.Evaluate(time.Now())
	s2.findMu.Lock()
	defer s2.findMu.Unlock()
	if len(s2.active) != 0 || len(s2.resolved) != 1 || s2.resolved[0].key != key ||
		!s2.resolved[0].StartsAt.Equal(started) {
		t.Fatalf("active %d, resolved %+v", len(s2.active), s2.resolved)
	}
}
