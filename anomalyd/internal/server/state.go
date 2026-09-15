package server

import (
	"encoding/gob"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/drain"
	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/series"
)

const stateVersion = 1

type stateFile struct {
	Version   int
	SavedAt   time.Time
	Step      int64
	PromCap   int
	LogCap    int
	Prom      []series.Snap
	Logs      []series.Snap
	Templates map[string][]clusterSnap
	LogStart  int64
}

type clusterSnap struct {
	ID      int
	Tokens  []string
	Size    int64
	Levels  map[string]int64
	Created time.Time
}

func (s *Server) statePath() string { return filepath.Join(s.cfg.StateDir, "anomalyd.state") }

// save writes a gob snapshot atomically (temp file + rename).
func (s *Server) save() error {
	t0 := time.Now()
	st := stateFile{Version: stateVersion, SavedAt: t0, Step: s.step, PromCap: s.prom.Capacity(),
		LogCap: s.logs.Capacity(), Prom: s.prom.Snapshot(), Logs: s.logs.Snapshot(),
		Templates: map[string][]clusterSnap{}, LogStart: s.logStart.Load()}
	s.pool.Each(func(svc string, m *drain.Miner) {
		m.Clusters(func(c *drain.Cluster) {
			st.Templates[svc] = append(st.Templates[svc], clusterSnap{ID: c.ID, Tokens: c.Tokens, Size: c.Size})
		})
	})
	s.tmplMu.Lock()
	for svc, cs := range st.Templates {
		for i := range cs {
			if m := s.tmpl[tmplKey{svc, cs[i].ID}]; m != nil {
				cs[i].Levels, cs[i].Created = m.levels, m.created
			}
		}
	}
	if err := os.MkdirAll(s.cfg.StateDir, 0o755); err != nil {
		s.tmplMu.Unlock()
		return err
	}
	f, err := os.CreateTemp(s.cfg.StateDir, "anomalyd.state.*")
	if err != nil {
		s.tmplMu.Unlock()
		return err
	}
	err = gob.NewEncoder(f).Encode(&st)
	s.tmplMu.Unlock()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), s.statePath())
	}
	if err != nil {
		os.Remove(f.Name())
		return fmt.Errorf("write state: %w", err)
	}
	slog.Info("state saved", "series", len(st.Prom)+len(st.Logs), "services", len(st.Templates),
		"took", time.Since(t0).Round(time.Millisecond))
	return nil
}

// load restores a snapshot taken with the same step and ring sizes; templates always load.
func (s *Server) load() error {
	f, err := os.Open(s.statePath())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	var st stateFile
	if err := gob.NewDecoder(f).Decode(&st); err != nil {
		return fmt.Errorf("decode state: %w", err)
	}
	if st.Version != stateVersion {
		return fmt.Errorf("state version %d, want %d", st.Version, stateVersion)
	}
	nt := 0
	for svc, cs := range st.Templates {
		m, used := s.pool.Get(svc)
		m.Lock()
		s.tmplMu.Lock()
		for i := len(cs) - 1; i >= 0; i-- { // oldest first so the LRU order survives
			c := cs[i]
			m.Restore(c.ID, c.Tokens, c.Size)
			lv := c.Levels
			if lv == nil {
				lv = map[string]int64{}
			}
			s.tmpl[tmplKey{used, c.ID}] = &tmplMeta{levels: lv, created: c.Created}
			nt++
		}
		s.tmplMu.Unlock()
		m.Unlock()
	}
	np, nl := 0, 0
	if st.Step == s.step {
		np = s.prom.Restore(st.Prom, s.promInfo)
		nl = s.logs.Restore(st.Logs, s.logInfo)
		// lastData is not in the snapshot; without it gc would drop every restored log series.
		s.logs.ForEachShard(1, func(ss map[string]*series.Series) {
			for _, sr := range ss {
				sr.Info.(*seriesInfo).lastData = sr.Head()
			}
		})
	}
	if st.LogStart > 0 {
		// Templates are known already, so novelty needs no second warm-up.
		s.logStart.Store(min(st.LogStart, time.Now().Add(-s.cfg.NoveltyWarmup).Unix()))
	}
	slog.Info("state loaded", "saved_at", st.SavedAt, "templates", nt, "metric_series", np, "log_series", nl)
	return nil
}
