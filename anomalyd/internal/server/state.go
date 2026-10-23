package server

import (
	"encoding/gob"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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
	Findings  []findingSnap // firing findings, so a restart neither drops nor re-dates them
}

type findingSnap struct {
	Key     string
	Finding Finding
}

type clusterSnap struct {
	ID      int
	Tokens  []string
	Size    int64
	Levels  map[string]int64
	Created time.Time
}

func (s *Server) statePath() string { return filepath.Join(s.cfg.StateDir, "anomalyd.state") }

// stateWriter wraps the snapshot file; tests use it to slow the write down.
var stateWriter = func(w io.Writer) io.Writer { return w }

// save writes a gob snapshot atomically (temp file + rename). It copies the state under the
// server locks and encodes with none held, so a slow disk never stalls pushes or evaluation.
func (s *Server) save() error {
	t0 := time.Now()
	st := s.copyState(t0)
	err := s.writeState(&st)
	s.stats.snapNanos.Store(int64(time.Since(t0)))
	if err != nil {
		s.stats.snapFailures.Add(1)
		return err
	}
	s.stats.snapLastOK.Store(time.Now().Unix())
	slog.Info("state saved", "series", len(st.Prom)+len(st.Logs), "services", len(st.Templates),
		"took", time.Since(t0).Round(time.Millisecond))
	return nil
}

// copyState returns a snapshot that shares nothing mutable with the live server.
func (s *Server) copyState(now time.Time) stateFile {
	st := stateFile{Version: stateVersion, SavedAt: now, Step: s.step, PromCap: s.prom.Capacity(),
		LogCap: s.logs.Capacity(), Prom: s.prom.Snapshot(), Logs: s.logs.Snapshot(),
		Templates: map[string][]clusterSnap{}, LogStart: s.logStart.Load()}
	s.findMu.Lock()
	for k, f := range s.active {
		c := *f // evaluation rewrites the annotations of firing findings in place
		c.Labels, c.Annotations = maps.Clone(f.Labels), maps.Clone(f.Annotations)
		st.Findings = append(st.Findings, findingSnap{Key: k, Finding: c})
	}
	s.findMu.Unlock()
	s.pool.Each(func(svc string, m *drain.Miner) {
		m.Clusters(func(c *drain.Cluster) {
			// Copy: pushes generalise c.Tokens in place under the miner lock.
			st.Templates[svc] = append(st.Templates[svc], clusterSnap{ID: c.ID, Tokens: slices.Clone(c.Tokens), Size: c.Size})
		})
	})
	s.tmplMu.Lock()
	for svc, cs := range st.Templates {
		for i := range cs {
			if m := s.tmpl[tmplKey{svc, cs[i].ID}]; m != nil {
				cs[i].Levels, cs[i].Created = maps.Clone(m.levels), m.created
			}
		}
	}
	s.tmplMu.Unlock()
	return st
}

func (s *Server) writeState(st *stateFile) error {
	if err := os.MkdirAll(s.cfg.StateDir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(s.cfg.StateDir, "anomalyd.state.*")
	if err != nil {
		return err
	}
	err = gob.NewEncoder(stateWriter(f)).Encode(st)
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
	return nil
}

// load restores a snapshot; series need the same step and ring sizes (it warns when they
// differ), templates always load.
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
			id := c.ID
			if used != svc || m.Get(id) != nil {
				// Folded into the overflow miner (--max-services lowered since the snapshot):
				// ids from different services collide, so let the miner assign or merge one.
				cl, _ := m.Add(c.Tokens, c.Size)
				id = cl.ID
			} else {
				m.Restore(id, c.Tokens, c.Size)
			}
			k := tmplKey{used, id}
			if old := s.tmpl[k]; old != nil {
				for l, n := range c.Levels {
					old.levels[l] += n
				}
				if !c.Created.IsZero() && (old.created.IsZero() || c.Created.Before(old.created)) {
					old.created = c.Created
				}
			} else {
				lv := c.Levels
				if lv == nil {
					lv = map[string]int64{}
				}
				s.tmpl[k] = &tmplMeta{levels: lv, created: c.Created}
			}
			nt++
		}
		s.tmplMu.Unlock()
		m.Unlock()
	}
	np, nl := 0, 0
	if st.Step != s.step {
		slog.Warn("snapshot step differs; metric and log series not restored",
			"old_step", time.Duration(st.Step)*time.Second, "new_step", s.cfg.Step,
			"metric_series", len(st.Prom), "log_series", len(st.Logs))
	} else {
		if st.PromCap == s.prom.Capacity() {
			np = s.prom.Restore(st.Prom, s.promInfo)
		} else {
			slog.Warn("snapshot metric ring size differs (--prom-season or --prom-seasons changed); metric series not restored",
				"old_buckets", st.PromCap, "new_buckets", s.prom.Capacity(), "metric_series", len(st.Prom))
		}
		if st.LogCap == s.logs.Capacity() {
			nl = s.logs.Restore(st.Logs, s.logInfo)
		} else {
			slog.Warn("snapshot log ring size differs (--log-season or --log-seasons changed); log series not restored",
				"old_buckets", st.LogCap, "new_buckets", s.logs.Capacity(), "log_series", len(st.Logs))
		}
		// lastData is not in the snapshot; without it gc would drop every restored log series.
		s.logs.ForEachShard(1, func(ss map[string]*series.Series) {
			for _, sr := range ss {
				sr.Info.(*seriesInfo).lastData = sr.Head()
			}
		})
	}
	nf := s.restoreFindings(st.Findings)
	if st.LogStart > 0 {
		// Templates are known already, so novelty needs no second warm-up.
		s.logStart.Store(min(st.LogStart, time.Now().Add(-s.cfg.NoveltyWarmup).Unix()))
	}
	slog.Info("state loaded", "saved_at", st.SavedAt, "templates", nt, "metric_series", np, "log_series", nl,
		"findings", nf)
	return nil
}

// restoreFindings re-activates firing findings whose series (or new template) was restored, so
// they keep their start time and resolve through the normal path.
func (s *Server) restoreFindings(snaps []findingSnap) int {
	s.findMu.Lock()
	defer s.findMu.Unlock()
	n := 0
	for _, fs := range snaps {
		f := fs.Finding
		f.key = fs.Key
		ok := false
		switch f.Kind {
		case kindLogNew:
			id, err := strconv.Atoi(f.Labels["template_id"])
			s.tmplMu.Lock()
			if m := s.tmpl[tmplKey{f.Labels["service"], id}]; err == nil && m != nil {
				m.novel, m.fired, ok = true, f.StartsAt, true // resolves after --novelty-ttl
			}
			s.tmplMu.Unlock()
		case kindMetric, kindLogTemplate, kindLogVolume:
			st := s.logs
			if f.Kind == kindMetric {
				st = s.prom
			}
			ok = st.Lookup(fs.Key, func(sr *series.Series) { sr.Info.(*seriesInfo).firing = true })
		}
		if ok {
			s.active[fs.Key] = &f
			n++
		}
	}
	return n
}
