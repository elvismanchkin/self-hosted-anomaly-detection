package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/drain"
	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/wire"
)

func pushAs(t *testing.T, h http.Handler, agent string, step int64, items ...wire.Item) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(wire.Push{Agent: agent, Step: step, Items: items})
	return post(t, h, "/api/v1/templates", "application/json", b)
}

func get(t *testing.T, h http.Handler, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d", path, rec.Code)
	}
	return rec.Body.String()
}

func agentRows(t *testing.T, h http.Handler) map[string]agentRow {
	t.Helper()
	var rows []agentRow
	if err := json.Unmarshal([]byte(get(t, h, "/api/v1/agents")), &rows); err != nil {
		t.Fatal(err)
	}
	out := map[string]agentRow{}
	for _, r := range rows {
		out[r.Agent] = r
	}
	return out
}

// An agent step that does not divide the server step would put counts in the wrong buckets.
func TestPushStepMustDivideServerStep(t *testing.T) {
	s, _ := New(testConfig()) // step 60s
	h := s.Handler()
	now := time.Now().Unix() / 60 * 60
	it := wire.Item{Service: "gw", Template: "GET /health <NUM>", Level: "info", Counts: [][2]int64{{now, 7}}}
	rec := pushAs(t, h, "host-a", 120, it)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "agent step 120s does not divide server step 60s") {
		t.Fatalf("step 120: %d %s", rec.Code, rec.Body)
	}
	if s.logs.Len() != 0 || s.stats.logLines.Load() != 0 {
		t.Fatalf("rejected push was ingested: %d series", s.logs.Len())
	}
	if a := agentRows(t, h)["host-a"]; a.Rejected != 1 || a.Step != 120 || a.Pushes != 0 || !a.LastPush.IsZero() {
		t.Fatalf("rejected push in registry: %+v", a)
	}
	if rec := pushAs(t, h, "host-a", -60, it); rec.Code != http.StatusBadRequest {
		t.Fatalf("step -60: %d", rec.Code)
	}
	for _, step := range []int64{0, 30, 60} { // 0: older agents that do not send a step
		if rec := pushAs(t, h, "host-a", step, it); rec.Code != http.StatusNoContent {
			t.Fatalf("step %d: %d %s", step, rec.Code, rec.Body)
		}
	}
	m := get(t, h, "/metrics")
	if !strings.Contains(m, `anomalyd_log_push_rejected_total{reason="step"} 2`+"\n") ||
		!strings.Contains(m, "anomalyd_log_push_errors_total 2\n") {
		t.Fatalf("rejections not counted:\n%s", m)
	}
	if a := agentRows(t, h)["host-a"]; a.Rejected != 2 || a.Pushes != 3 || a.Lines != 21 || a.Step != 60 {
		t.Fatalf("registry: %+v", a)
	}
}

// An empty push is a heartbeat: the agent shows as alive, but it is not log data.
func TestEmptyPushIsHeartbeat(t *testing.T) {
	s, _ := New(testConfig())
	h := s.Handler()
	if rec := pushAs(t, h, "host-a", 60); rec.Code != http.StatusNoContent {
		t.Fatalf("heartbeat: %d %s", rec.Code, rec.Body)
	}
	if s.logStart.Load() != 0 {
		t.Fatal("heartbeat started the novelty warm-up clock")
	}
	if s.logs.Len() != 0 {
		t.Fatalf("heartbeat created %d log series", s.logs.Len())
	}
	a := agentRows(t, h)["host-a"]
	if a.Pushes != 1 || a.Lines != 0 || time.Since(a.LastPush) > time.Minute || len(a.Services) != 0 {
		t.Fatalf("heartbeat not recorded: %+v", a)
	}

	now := time.Now().Unix() / 60 * 60
	pushAs(t, h, "host-a", 60, wire.Item{Service: "checkout", Template: "Order <NUM> created", Level: "info",
		Counts: [][2]int64{{now - 60, 30}, {now, 20}}})
	pushAs(t, h, " ", 60) // no name
	if s.logStart.Load() == 0 {
		t.Fatal("log data did not start the warm-up clock")
	}
	rows := agentRows(t, h)
	if a := rows["host-a"]; a.Pushes != 2 || a.Lines != 50 || len(a.Services) != 1 || a.Services[0] != "checkout" {
		t.Fatalf("after data: %+v", a)
	}
	if rows["unknown"].Pushes != 1 {
		t.Fatalf("unnamed agent: %+v", rows)
	}
	m := get(t, h, "/metrics")
	ts := wire.FormatFloat(float64(rows["host-a"].LastPush.Unix()))
	for _, want := range []string{
		`anomalyd_agent_last_push_timestamp_seconds{agent="host-a"} ` + ts + "\n",
		`anomalyd_agent_lines_total{agent="host-a"} 50` + "\n",
		`anomalyd_agent_lines_total{agent="unknown"} 0` + "\n",
	} {
		if !strings.Contains(m, want) {
			t.Errorf("/metrics lacks %q", want)
		}
	}
}

func TestAgentRegistryCap(t *testing.T) {
	s, _ := New(testConfig())
	now := time.Now()
	for i := range maxAgents + 5 {
		s.agents.record("node-"+strconv.Itoa(i), 60, 1, map[string]struct{}{"svc": {}}, now)
	}
	s.agents.record("node-0", 60, 1, nil, now) // known agents keep their entry
	rows := agentRows(t, s.Handler())
	if len(rows) != maxAgents+1 {
		t.Fatalf("agents = %d, want %d", len(rows), maxAgents+1)
	}
	if o := rows[drain.OverflowKey]; o.Pushes != 5 || o.Lines != 5 {
		t.Fatalf("overflow entry: %+v", o)
	}
	if rows["node-0"].Pushes != 2 {
		t.Fatalf("node-0: %+v", rows["node-0"])
	}
}

// Agents idle for agentTTL (removed nodes) are dropped and free their slot; rejected pushes
// keep an agent.
func TestAgentRegistryExpiry(t *testing.T) {
	s, _ := New(testConfig())
	now := time.Now()
	for i := range maxAgents - 1 {
		s.agents.record("old-"+strconv.Itoa(i), 60, 1, nil, now.Add(-25*time.Hour))
	}
	s.agents.reject("bad-step", 120, now.Add(-2*time.Hour))
	s.agents.record("new", 60, 1, nil, now)
	rows := s.agents.rows(now, true)
	if len(rows) != 2 || rows[0].Agent != "bad-step" || rows[1].Agent != "new" {
		t.Fatalf("rows = %+v", rows)
	}
	if m := get(t, s.Handler(), "/metrics"); strings.Contains(m, `agent="old-0"`) {
		t.Fatal("idle agent still on /metrics")
	}
}

// Long names are cut on a rune boundary: invalid UTF-8 in a label fails the whole scrape.
func TestLongAgentNameStaysUTF8(t *testing.T) {
	s, _ := New(testConfig())
	h := s.Handler()
	pushAs(t, h, strings.Repeat("a", 127)+"é", 60)
	m := get(t, h, "/metrics")
	if !utf8.ValidString(m) || !strings.Contains(m, `{agent="`+strings.Repeat("a", 127)+`"}`) {
		t.Fatalf("agent label not cut on a rune boundary:\n%s", m)
	}
}

// Services per agent are capped; ones without lines for an hour make room.
func TestAgentServicesCap(t *testing.T) {
	s, _ := New(testConfig())
	now := time.Now()
	svcs := map[string]struct{}{}
	for i := range maxAgentServices + 10 {
		svcs["svc-"+strconv.Itoa(i)] = struct{}{}
	}
	s.agents.record("node-1", 60, 1, svcs, now.Add(-2*time.Hour))
	if n := len(s.agents.agents["node-1"].services); n != maxAgentServices {
		t.Fatalf("services = %d, want %d", n, maxAgentServices)
	}
	if a := agentRows(t, s.Handler())["node-1"]; len(a.Services) != 0 {
		t.Fatalf("services without lines for 2h listed: %v", a.Services)
	}
	s.agents.record("node-1", 60, 1, map[string]struct{}{"new": {}}, now)
	if a := agentRows(t, s.Handler())["node-1"]; len(a.Services) != 1 || a.Services[0] != "new" {
		t.Fatalf("stale services not replaced: %v", a.Services)
	}
}

func TestSelfMetrics(t *testing.T) {
	cfg := testConfig()
	cfg.Drain.MaxClusters = 1 // the second template evicts the first
	cfg.Version, cfg.Revision = "v1.2.3", "abc123"
	s, _ := New(cfg)
	h := s.Handler()
	now := time.Now().Unix() / 60 * 60
	push(t, h, wire.Item{Service: "gw", Template: "GET /health <NUM>", Level: "info", Counts: [][2]int64{{now, 7}}},
		wire.Item{Service: "gw", Template: "db pool exhausted after <NUM> ms", Level: "error", Counts: [][2]int64{{now, 2}}})
	push(t, h, wire.Item{Service: "gw", Template: "db pool exhausted after <NUM> ms", Level: "error", Counts: [][2]int64{{now, 2}}})
	m := get(t, h, "/metrics")
	for _, want := range []string{
		`anomalyd_templates{service="gw"} 1` + "\n",
		`anomalyd_templates_created_total{service="gw"} 2` + "\n",
		`anomalyd_templates_evicted_total{service="gw"} 1` + "\n",
		`anomalyd_build_info{version="v1.2.3",revision="abc123",goversion="go`,
	} {
		if !strings.Contains(m, want) {
			t.Errorf("/metrics lacks %q", want)
		}
	}
}
