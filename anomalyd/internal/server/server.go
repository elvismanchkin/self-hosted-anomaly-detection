// Package server is the central anomalyd process: it ingests Prometheus remote-write samples
// and log-template counts, scores every series against a seasonal baseline, and sends findings
// to Alertmanager.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/detect"
	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/drain"
	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/series"
)

type Config struct {
	Listen       string
	Step         time.Duration // bucket width for every series
	EvalInterval time.Duration
	Grace        time.Duration // wait this long after a bucket ends before scoring it
	ForBuckets   int           // consecutive anomalous buckets before a finding fires
	Threshold    float64       // |robust z| for a finding
	Workers      int

	// Metrics received by remote write (or backfilled from Prometheus).
	MaxSeries     int
	PromSeason    time.Duration
	PromSeasons   int
	PromLookback  time.Duration
	PromRelFloor  float64
	IgnoreLabels  []string // dropped on ingest, e.g. external labels added by Prometheus
	BackfillURL   string
	BackfillMatch []string

	// Log template counts (from agents or /api/v1/logs).
	MaxLogSeries   int
	LogSeason      time.Duration
	LogSeasons     int
	LogLookback    time.Duration
	LogMinCount    float64 // ignore log buckets where max(observed, expected) is below this
	NoveltyWarmup  time.Duration
	NoveltyMin     int64
	NoveltyWindow  time.Duration
	NoveltyTTL     time.Duration
	Drain          drain.Config
	MaxServices    int
	MessageField   string
	ServiceField   string
	LevelField     string
	ExportTemplate bool // export anomalyd_score for every log-template series
	ExportBands    bool

	AlertmanagerURLs []string
	ExternalURL      string
	StateDir         string
	SnapshotEvery    time.Duration
}

type Server struct {
	cfg        Config
	step       int64
	prom, logs *series.Store
	promP      detect.Params
	logP       detect.Params
	pool       *drain.Pool
	ignore     map[string]bool
	promClock  atomic.Int64 // newest remote-write sample, unix seconds
	logStart   atomic.Int64 // first log data, unix seconds (0 = none yet)
	started    time.Time
	client     *http.Client

	tmplMu  sync.Mutex
	tmpl    map[tmplKey]*tmplMeta
	aliases map[string]map[string]int // service -> agent template text -> global template id

	findMu   sync.Mutex
	active   map[string]*Finding
	resolved []*Finding // newest first, capped
	gcRuns   int        // evaluation goroutine only

	stats stats
}

type stats struct {
	rwRequests, rwErrors, rwSamples, rwDropped atomic.Int64
	logLines, logPushes, logPushErrors         atomic.Int64
	alertsSent, alertErrors                    atomic.Int64
	evalNanos, evalSeries                      atomic.Int64
	backfilled                                 atomic.Int64
}

type tmplKey struct {
	service string
	id      int
}

type tmplMeta struct {
	levels  map[string]int64
	created time.Time
	novel   bool  // created after warm-up: novelty candidate
	count   int64 // lines since creation (for novelty)
	fired   time.Time
}

func (m *tmplMeta) level() string {
	best, n := "", int64(0)
	for l, c := range m.levels {
		if c > n || (c == n && l < best) {
			best, n = l, c
		}
	}
	return best
}

func buckets(d, step time.Duration) int { return int(math.Ceil(float64(d) / float64(step))) }

func New(cfg Config) (*Server, error) {
	if cfg.Step <= 0 || cfg.PromSeason%cfg.Step != 0 || cfg.LogSeason%cfg.Step != 0 {
		return nil, errors.New("season durations must be positive multiples of step")
	}
	s := &Server{cfg: cfg, step: int64(cfg.Step / time.Second), started: time.Now(),
		client: &http.Client{Timeout: 10 * time.Second}, ignore: map[string]bool{},
		tmpl: map[tmplKey]*tmplMeta{}, aliases: map[string]map[string]int{}, active: map[string]*Finding{}}
	for _, l := range cfg.IgnoreLabels {
		if l = strings.TrimSpace(l); l != "" {
			s.ignore[l] = true
		}
	}
	s.promP = detect.Params{Season: buckets(cfg.PromSeason, cfg.Step), Seasons: cfg.PromSeasons,
		Lookback: buckets(cfg.PromLookback, cfg.Step), Threshold: cfg.Threshold, RelFloor: cfg.PromRelFloor,
		AbsFloor: 1e-9, ScaleEvery: max(1, buckets(time.Hour, cfg.Step))}
	s.logP = detect.Params{Season: buckets(cfg.LogSeason, cfg.Step), Seasons: cfg.LogSeasons,
		Lookback: buckets(cfg.LogLookback, cfg.Step), Threshold: cfg.Threshold, RelFloor: 0.1,
		AbsFloor: 1, Poisson: true, ScaleEvery: max(1, buckets(time.Hour, cfg.Step))}
	// Ring = (seasons+1) seasons of history plus the open bucket and one spare.
	s.prom = series.NewStore((cfg.PromSeasons+1)*s.promP.Season+2, cfg.MaxSeries)
	s.logs = series.NewStore((cfg.LogSeasons+1)*s.logP.Season+2, cfg.MaxLogSeries)
	s.pool = drain.NewPool(cfg.Drain, cfg.MaxServices)
	return s, nil
}

// Run serves HTTP and evaluates until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	if s.cfg.StateDir != "" {
		if err := s.load(); err != nil {
			slog.Warn("state not loaded", "err", err)
		}
	}
	if s.cfg.BackfillURL != "" {
		go s.backfill(ctx)
	}
	srv := &http.Server{Addr: s.cfg.Listen, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 2 * time.Minute, WriteTimeout: 2 * time.Minute, IdleTimeout: 5 * time.Minute}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("anomalyd server listening", "addr", s.cfg.Listen, "step", s.cfg.Step,
		"prom_ring_buckets", s.prom.Capacity(), "log_ring_buckets", s.logs.Capacity())

	tick := time.NewTicker(s.cfg.EvalInterval)
	defer tick.Stop()
	var snap <-chan time.Time
	if s.cfg.StateDir != "" && s.cfg.SnapshotEvery > 0 {
		t := time.NewTicker(s.cfg.SnapshotEvery)
		defer t.Stop()
		snap = t.C
	}
	for {
		select {
		case <-ctx.Done():
			shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = srv.Shutdown(shut)
			if s.cfg.StateDir != "" {
				if err := s.save(); err != nil {
					slog.Error("final snapshot failed", "err", err)
				}
			}
			return nil
		case err := <-errc:
			return err
		case now := <-tick.C:
			s.Evaluate(now)
		case <-snap:
			if err := s.save(); err != nil {
				slog.Error("snapshot failed", "err", err)
			}
		}
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/write", s.handleRemoteWrite)
	mux.HandleFunc("POST /api/v1/templates", s.handlePush)
	mux.HandleFunc("GET /api/v1/templates", s.handleTemplates)
	mux.HandleFunc("POST /api/v1/logs", s.handleLogs)
	mux.HandleFunc("GET /api/v1/findings", s.handleFindings)
	mux.HandleFunc("GET /metrics", s.handleMetrics)
	mux.HandleFunc("GET /-/healthy", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("OK\n")) })
	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false) // templates contain <*>, <NUM>
	enc.SetIndent("", " ")
	_ = enc.Encode(v)
}
