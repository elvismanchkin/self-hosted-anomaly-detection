package server

import (
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/drain"
	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/wire"
)

// Agents beyond maxAgents share one entry (drain.OverflowKey); each entry tracks at most
// maxAgentServices services. An agent that sent nothing for agentTTL is forgotten, so removed
// nodes leave /metrics and do not use up maxAgents.
const (
	maxAgents        = 10000
	maxAgentServices = 256
	agentTTL         = 24 * time.Hour
)

// agentInfo is what the server knows about one agent from its pushes.
type agentInfo struct {
	last     time.Time // last accepted push (heartbeats included)
	seen     time.Time // last push, accepted or not
	step     int64     // of the last push, accepted or not
	pushes   int64
	rejected int64
	lines    int64
	services map[string]int64 // service -> last push with lines for it, unix seconds
}

type agentRegistry struct {
	mu     sync.Mutex
	agents map[string]*agentInfo
	swept  time.Time
}

// expireLocked drops agents idle for agentTTL, at most once a minute.
func (r *agentRegistry) expireLocked(now time.Time) {
	if now.Sub(r.swept) < time.Minute {
		return
	}
	r.swept = now
	for name, a := range r.agents {
		if now.Sub(a.seen) > agentTTL {
			delete(r.agents, name)
		}
	}
}

func (r *agentRegistry) getLocked(name string, now time.Time) *agentInfo {
	name = cleanService(name) // same rules as service names: trimmed, "unknown" when empty
	a := r.agents[name]
	if a == nil {
		r.expireLocked(now)
		if len(r.agents) >= maxAgents {
			name = drain.OverflowKey
		}
		if a = r.agents[name]; a == nil {
			a = &agentInfo{services: map[string]int64{}}
			r.agents[strings.Clone(name)] = a // name may share the push body's memory
		}
	}
	a.seen = now
	return a
}

// record counts an accepted push; services are the ones it carried lines for.
func (r *agentRegistry) record(name string, step, lines int64, services map[string]struct{}, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.getLocked(name, now)
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
		if _, ok := a.services[svc]; ok {
			a.services[svc] = t
		} else if len(a.services) < maxAgentServices {
			a.services[strings.Clone(svc)] = t
		}
	}
}

func (r *agentRegistry) reject(name string, step int64, now time.Time) {
	r.mu.Lock()
	a := r.getLocked(name, now)
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

// rows lists the agents; services (with lines in the last hour) only when withServices.
func (r *agentRegistry) rows(now time.Time, withServices bool) []agentRow {
	r.mu.Lock()
	r.expireLocked(now)
	out := make([]agentRow, 0, len(r.agents))
	for name, a := range r.agents {
		row := agentRow{Agent: name, LastPush: a.last, Step: a.step, Pushes: a.pushes, Rejected: a.rejected,
			Lines: a.lines, Services: []string{}}
		if withServices {
			for svc, t := range a.services {
				if now.Unix()-t <= 3600 {
					row.Services = append(row.Services, svc)
				}
			}
			sort.Strings(row.Services)
		}
		out = append(out, row)
	}
	r.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Agent < out[j].Agent })
	return out
}

func (r *agentRegistry) write(e *wire.Expo) {
	rows := r.rows(time.Now(), false)
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

// handleAgents lists the agents that pushed in the last agentTTL.
func (s *Server) handleAgents(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.agents.rows(time.Now(), true))
}
