// anomalyd: seasonal anomaly detection for Prometheus series and log-template counts.
//
//	anomalyd server  central process: remote-write receiver, log-count receiver, detector, alerts
//	anomalyd agent   per-host log tailer: Drain templates locally, pushes counts to the server
//	anomalyd gen     synthetic logs / metrics / remote-write history for tests
//	anomalyd bench   log-mining throughput on a file (no server)
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/agent"
	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/drain"
	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/gen"
	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/server"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var err error
	switch os.Args[1] {
	case "server":
		err = runServer(ctx, os.Args[2:])
	case "agent":
		err = runAgent(ctx, os.Args[2:])
	case "gen":
		err = runGen(ctx, os.Args[2:])
	case "bench":
		err = runBench(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		slog.Error("exit", "err", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: anomalyd server|agent|gen|bench [flags]   (-h for flags)")
	os.Exit(2)
}

type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

func drainFlags(fs *flag.FlagSet) *drain.Config {
	c := drain.DefaultConfig()
	fs.IntVar(&c.Depth, "drain-depth", c.Depth, "Drain parse-tree depth (Drain3 semantics)")
	fs.Float64Var(&c.SimTh, "drain-sim-th", c.SimTh, "Drain similarity threshold")
	fs.IntVar(&c.MaxChildren, "drain-max-children", c.MaxChildren, "max children per tree node")
	fs.IntVar(&c.MaxClusters, "drain-max-templates", c.MaxClusters, "templates per service (LRU)")
	return &c
}

func runServer(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	c := server.Config{}
	var am, match listFlag
	var ignore, floors string
	fs.StringVar(&c.Listen, "listen", ":9400", "HTTP listen address")
	fs.DurationVar(&c.Step, "step", 5*time.Minute, "bucket width for all series")
	fs.DurationVar(&c.EvalInterval, "eval-interval", 30*time.Second, "how often complete buckets are scored")
	fs.DurationVar(&c.Grace, "grace", 30*time.Second, "delay after a bucket ends before it is scored")
	fs.IntVar(&c.ForBuckets, "for", 2, "consecutive anomalous buckets before a finding fires")
	fs.Float64Var(&c.Threshold, "threshold", 4, "robust z-score threshold")
	fs.IntVar(&c.Workers, "workers", 0, "evaluation goroutines (0 = GOMAXPROCS)")
	fs.IntVar(&c.MaxSeries, "max-series", 20000, "cap on remote-write series")
	fs.DurationVar(&c.PromSeason, "prom-season", 7*24*time.Hour, "seasonality of metric series")
	fs.IntVar(&c.PromSeasons, "prom-seasons", 2, "seasons of history in the seasonal median")
	fs.DurationVar(&c.PromLookback, "prom-lookback", 2*time.Hour, "cold-start baseline window")
	fs.Float64Var(&c.PromRelFloor, "prom-rel-floor", 0.05, "scale floor as a fraction of the expected value")
	fs.StringVar(&floors, "abs-floor", "errors=0.001", "scale floors per anomaly_type label, e.g. errors=0.001,latency=5")
	fs.StringVar(&ignore, "ignore-labels", "prometheus,prometheus_replica", "labels dropped on ingest (Prometheus external labels)")
	fs.StringVar(&c.BackfillURL, "backfill-url", "", "Prometheus URL to load history from at start-up")
	fs.Var(&match, "backfill-match", "series selector to backfill (repeatable), e.g. '{__name__=~\"anomaly:.+\"}'")
	fs.IntVar(&c.MaxLogSeries, "max-log-series", 50000, "cap on log template + volume series")
	fs.DurationVar(&c.LogSeason, "log-season", 24*time.Hour, "seasonality of log series")
	fs.IntVar(&c.LogSeasons, "log-seasons", 3, "seasons of log history in the seasonal median")
	fs.DurationVar(&c.LogLookback, "log-lookback", 2*time.Hour, "cold-start baseline window for log series")
	fs.Float64Var(&c.LogMinCount, "log-min-count", 10, "ignore log buckets where max(observed, expected) is below this")
	fs.DurationVar(&c.NoveltyWarmup, "novelty-warmup", time.Hour, "no new-template findings until logs have flowed this long")
	fs.Int64Var(&c.NoveltyMin, "novelty-min-lines", 5, "lines a new template needs before it is reported")
	fs.DurationVar(&c.NoveltyWindow, "novelty-window", 15*time.Minute, "time a new template has to reach --novelty-min-lines")
	fs.DurationVar(&c.NoveltyTTL, "novelty-ttl", time.Hour, "how long a new-template finding stays firing")
	fs.IntVar(&c.MaxServices, "max-services", 1000, "services with their own template miner")
	fs.StringVar(&c.MessageField, "message-field", "message", "JSON field with the log message (/api/v1/logs)")
	fs.StringVar(&c.ServiceField, "service-field", "service.name", "JSON field with the service")
	fs.StringVar(&c.LevelField, "level-field", "log.level", "JSON field with the level")
	fs.BoolVar(&c.ExportTemplate, "export-template-scores", false, "export anomalyd_score for every log template series")
	fs.BoolVar(&c.ExportBands, "export-bands", true, "export expected and band gauges next to anomalyd_score")
	fs.Var(&am, "alertmanager-url", "Alertmanager base URL (repeatable)")
	fs.StringVar(&c.ExternalURL, "external-url", "", "URL of this server, used in alert generatorURL")
	fs.StringVar(&c.StateDir, "state-dir", "", "directory for snapshots (templates, series)")
	fs.DurationVar(&c.SnapshotEvery, "snapshot-interval", 5*time.Minute, "snapshot period")
	c.Drain = *drainFlags(fs)
	fs.Parse(args)
	c.AlertmanagerURLs, c.BackfillMatch, c.IgnoreLabels = am, match, strings.Split(ignore, ",")
	for _, kv := range strings.Split(floors, ",") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return fmt.Errorf("--abs-floor %q: %w", kv, err)
			}
			server.AbsFloors[k] = f
		}
	}
	s, err := server.New(c)
	if err != nil {
		return err
	}
	return s.Run(ctx)
}

func runAgent(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("agent", flag.ExitOnError)
	c := agent.Config{}
	var paths listFlag
	var svcRe string
	host, _ := os.Hostname()
	fs.Var(&paths, "path", "log file glob (repeatable), e.g. /var/log/containers/*.log")
	fs.StringVar(&c.Container, "container", "", "container stdout files: cri (containerd, CRI-O), docker (json-file) or auto; empty = plain lines")
	fs.StringVar(&c.Format, "format", "json", "json (NDJSON) or text; inside the container envelope when --container is set")
	fs.StringVar(&c.MessageField, "message-field", "message", "JSON field with the message")
	fs.StringVar(&c.ServiceField, "service-field", "service.name", "JSON field with the service")
	fs.StringVar(&c.LevelField, "level-field", "log.level", "JSON field with the level")
	fs.StringVar(&c.Service, "service", "", "service name when neither the line nor the path gives one")
	fs.StringVar(&svcRe, "service-from-path", "", "regexp with a (?P<service>...) group applied to file paths")
	fs.StringVar(&c.Server, "server", "", "anomalyd server URL, e.g. http://anomalyd:9400")
	fs.StringVar(&c.Name, "name", host, "agent name sent with each push")
	fs.DurationVar(&c.Flush, "flush-interval", 10*time.Second, "push period")
	fs.DurationVar(&c.Poll, "poll-interval", 250*time.Millisecond, "file poll period")
	fs.BoolVar(&c.FromStart, "from-start", false, "read files found at start-up from the beginning")
	fs.Int64Var(&c.Step, "step", 60, "seconds per count bucket (server --step must be a multiple)")
	fs.IntVar(&c.MaxServices, "max-services", 200, "services with their own miner on this host")
	fs.IntVar(&c.MaxBacklog, "max-backlog", 100000, "count keys kept while the server is unreachable")
	fs.StringVar(&c.Listen, "listen", "", "address for the agent's own /metrics (empty = off)")
	fs.BoolVar(&c.Once, "once", false, "read files to EOF, push once and exit")
	c.Drain = *drainFlags(fs)
	fs.Parse(args)
	c.Paths = paths
	if len(c.Paths) == 0 {
		return fmt.Errorf("at least one --path is required")
	}
	if err := checkContainer(c.Container); err != nil {
		return err
	}
	if svcRe != "" {
		re, err := regexp.Compile(svcRe)
		if err != nil {
			return fmt.Errorf("--service-from-path: %w", err)
		}
		c.ServiceFromPath = re
	}
	return agent.New(c).Run(ctx)
}

func checkContainer(v string) error {
	switch v {
	case "", "cri", "docker", "auto":
		return nil
	}
	return fmt.Errorf("--container: want cri, docker or auto, got %q", v)
}

func runGen(ctx context.Context, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: anomalyd gen logs|metrics|history|rw [flags]")
	}
	fs := flag.NewFlagSet("gen "+args[0], flag.ExitOnError)
	switch args[0] {
	case "logs":
		o := gen.LogOptions{Out: os.Stdout}
		fs.IntVar(&o.Lines, "lines", 0, "batch mode: number of lines (0 = live mode)")
		fs.Float64Var(&o.AnomalyFraction, "anomaly-fraction", 0.05, "batch: tail fraction with the injected error burst")
		fs.Float64Var(&o.Rate, "rate", 1000, "live: lines per second")
		fs.DurationVar(&o.Duration, "duration", 0, "live: stop after (0 = run until killed)")
		fs.DurationVar(&o.InjectAfter, "inject-after", -1, "live: start the error burst after (negative = never)")
		fs.StringVar(&o.DropService, "drop-service", "", "live: service that stops logging")
		fs.DurationVar(&o.DropAfter, "drop-after", 0, "live: when --drop-service stops")
		fs.StringVar(&o.OutDir, "out-dir", "", "write <dir>/<service>/app.log instead of stdout (with --container: <dir>/<pod>_<ns>_<service>-<id>.log)")
		fs.StringVar(&o.Container, "container", "", "wrap lines as container stdout: cri or docker (json-file)")
		fs.StringVar(&o.AppFormat, "app-format", "json", "what the app writes: json (ECS-style NDJSON) or text")
		fs.Uint64Var(&o.Seed, "seed", 42, "random seed")
		fs.Parse(args[1:])
		if o.Container == "auto" {
			return fmt.Errorf("gen --container: cri or docker")
		}
		if err := checkContainer(o.Container); err != nil {
			return err
		}
		return gen.Logs(ctx, o)
	case "metrics":
		addr := fs.String("listen", ":9500", "listen address")
		inj := injFlags(fs)
		fs.Parse(args[1:])
		return gen.ServeMetrics(ctx, *addr, inj())
	case "history":
		days := fs.Int("days", 8, "days of history")
		step := fs.Duration("step", time.Minute, "sample step")
		endAgo := fs.Duration("end-ago", 2*time.Minute, "history ends this long before now")
		fs.Parse(args[1:])
		return gen.History(os.Stdout, time.Now().Add(-*endAgo).Truncate(*step), *days, *step)
	case "rw":
		url := fs.String("url", "http://localhost:9400/api/v1/write", "remote-write endpoint")
		days := fs.Float64("days", 8, "days of history to send, ending now")
		step := fs.Duration("step", time.Minute, "sample step")
		inj := injFlags(fs)
		fs.Parse(args[1:])
		to := time.Now().Truncate(*step)
		t0 := time.Now()
		n, err := gen.RemoteWrite(ctx, *url, to.Add(-time.Duration(*days*24)*time.Hour), to, *step, inj())
		fmt.Printf("sent %d samples in %s (%.0f samples/s)\n", n, time.Since(t0).Round(time.Millisecond),
			float64(n)/time.Since(t0).Seconds())
		return err
	}
	return fmt.Errorf("unknown gen mode %q", args[0])
}

func injFlags(fs *flag.FlagSet) func() *gen.Injection {
	svc := fs.String("inject-service", "", "service to inject an anomaly into (empty = none)")
	kind := fs.String("inject-kind", "spike", "spike | drop | errors")
	after := fs.Duration("inject-after", 0, "live: inject this long after start")
	ago := fs.Duration("inject-ago", 0, "history: inject from this long before now")
	return func() *gen.Injection {
		if *svc == "" {
			return nil
		}
		at := time.Now().Add(*after)
		if *ago > 0 {
			at = time.Now().Add(-*ago)
		}
		return &gen.Injection{Service: *svc, Kind: *kind, At: at}
	}
}

// runBench mines a log file through the agent pipeline on one core and reports throughput.
func runBench(args []string) error {
	fs := flag.NewFlagSet("bench", flag.ExitOnError)
	file := fs.String("file", "", "NDJSON or text log file")
	container := fs.String("container", "", "cri, docker or auto: the file holds container stdout envelopes")
	format := fs.String("format", "json", "json or text")
	procs := fs.Int("procs", 1, "GOMAXPROCS for the run")
	show := fs.Bool("templates", false, "print the mined templates")
	dc := drainFlags(fs)
	fs.Parse(args)
	if *file == "" {
		return fmt.Errorf("--file is required")
	}
	if err := checkContainer(*container); err != nil {
		return err
	}
	runtime.GOMAXPROCS(*procs)
	a := agent.New(agent.Config{Paths: []string{*file}, Container: *container, Format: *format, MessageField: "message",
		ServiceField: "service.name", LevelField: "log.level", Service: "bench", FromStart: true, Once: true,
		Drain: *dc, MaxServices: 1000, MaxBacklog: 1 << 20, Step: 60})
	var m0, m1 runtime.MemStats
	runtime.ReadMemStats(&m0)
	t0 := time.Now()
	if err := a.Run(context.Background()); err != nil {
		return err
	}
	el := time.Since(t0)
	runtime.ReadMemStats(&m1)
	lines, bytes, tmpl := a.Stats()
	fmt.Printf("lines=%d bytes=%d templates=%d elapsed=%s lines/s=%.0f MB/s=%.1f heap_alloc=%.1fMB total_alloc=%.1fMB gomaxprocs=%d\n",
		lines, bytes, tmpl, el.Round(time.Millisecond), float64(lines)/el.Seconds(), float64(bytes)/el.Seconds()/1e6,
		float64(m1.HeapAlloc)/1e6, float64(m1.TotalAlloc-m0.TotalAlloc)/1e6, *procs)
	if *show {
		a.Templates(func(svc string, c *drain.Cluster) { fmt.Printf("%s\t%d\t%s\n", svc, c.Size, c.Template()) })
	}
	return nil
}
