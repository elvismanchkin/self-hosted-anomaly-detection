package server

import (
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/drain"
	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/wire"
)

// Agents beyond maxAgents share one entry (drain.OverflowKey); each entry tracks at most
// maxAgentServices services.
const (
	maxAgents        = 10000
	maxAgentServices = 256
)

// agentInfo is what the server knows about one agent from its pushes.
type agentInfo struct {
	last     time.Time // last accepted push (heartbeats included)
	step     int64     // of the last push, accepted or not
	pushes   int64
	rejected int64
	lines    int64
	services map[string]int64 // service -> last push with lines for it, unix seconds
}

type agentRegistry struct {
	mu     sync.Mutex
	agents map[string]*agentInfo
}

func (r *agentRegistry) getLocked(name string) *agentInfo {
	name = cleanService(name) // same rules as service names: trimmed, "unknown" when empty
	a := r.agents[name]
	if a != nil {
		return a
	}
	if len(r.agents) >= maxAgents {
		name = drain.OverflowKey
		if a = r.agents[name]; a != nil {
			return a
		}
	}
	a = &agentInfo{services: map[string]int64{}}
	r.agents[name] = a
	return a
}

// record counts an accepted push; services are the ones it carried lines for.
func (r *agentRegistry) record(name string, step, lines int64, services map[string]struct{}, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.getLocked(name)
	a.last, a.step = now, step
	a.pushes++
	a.lines += lines
	t := now.Unix()
	if len(a.services) >= maxAgentServices {
		for k, seen := range a.services {
			if t-seen > 3600 {
				delete(a.services, k)
			}
		}
	}
	for svc := range services {
		if _, ok := a.services[svc]; ok || len(a.services) < maxAgentServices {
			a.services[svc] = t
		}
	}
}

func (r *agentRegistry) reject(name string, step int64) {
	r.mu.Lock()
	a := r.getLocked(name)
	a.step = step
	a.rejected++
	r.mu.Unlock()
}

type agentRow struct {
	Agent    string    `json:"agent"`
	LastPush time.Time `json:"last_push,omitzero"`
	Step     int64     `json:"step"`
	Pushes   int64     `json:"pushes"`
	Rejected int64     `json:"rejected"`
	Lines    int64     `json:"lines"`
	Services []string  `json:"services"` // with lines in the last hour
}

func (r *agentRegistry) rows(now time.Time) []agentRow {
	r.mu.Lock()
	out := make([]agentRow, 0, len(r.agents))
	for name, a := range r.agents {
		row := agentRow{Agent: name, LastPush: a.last, Step: a.step, Pushes: a.pushes, Rejected: a.rejected,
			Lines: a.lines, Services: []string{}}
		for svc, t := range a.services {
			if now.Unix()-t <= 3600 {
				row.Services = append(row.Services, svc)
			}
		}
		sort.Strings(row.Services)
		out = append(out, row)
	}
	r.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Agent < out[j].Agent })
	return out
}

func (r *agentRegistry) write(e *wire.Expo) {
	rows := r.rows(time.Now())
	e.Family("anomalyd_agent_last_push_timestamp_seconds", "Last accepted push per agent (0 = none yet).", "gauge")
	for _, a := range rows {
		ts := 0.0
		if !a.LastPush.IsZero() {
			ts = float64(a.LastPush.Unix())
		}
		e.Sample("anomalyd_agent_last_push_timestamp_seconds", [][2]string{{"agent", a.Agent}}, ts)
	}
	e.Family("anomalyd_agent_lines_total", "Log lines received per agent.", "counter")
	for _, a := range rows {
		e.Sample("anomalyd_agent_lines_total", [][2]string{{"agent", a.Agent}}, float64(a.Lines))
	}
}

// handleAgents lists the agents that pushed since start-up.
func (s *Server) handleAgents(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.agents.rows(time.Now()))
}
