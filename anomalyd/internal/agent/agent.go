// Package agent tails log files on a host (plain files, or the container stdout/stderr files the
// runtime writes under /var/log/containers), mines Drain templates locally and pushes
// per-template line counts (not lines) to an anomalyd server.
package agent

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/drain"
	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/logparse"
	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/wire"
)

type Config struct {
	Paths           []string // globs
	Container       string   // "" = plain lines; cri | docker | auto = container log envelopes
	Format          string   // json | text (of the line, or of the text inside the envelope)
	MessageField    string
	ServiceField    string
	LevelField      string
	Service         string         // used when the line / path gives none
	ServiceFromPath *regexp.Regexp // needs a (?P<service>...) group
	Server          string         // anomalyd server base URL; "" = don't push
	Name            string
	Flush           time.Duration
	Poll            time.Duration
	FromStart       bool  // read files discovered at start-up from the beginning
	Step            int64 // seconds per count bucket
	Drain           drain.Config
	MaxServices     int
	MaxBacklog      int // count keys kept while the server is unreachable
	Listen          string
	Once            bool // read to EOF, push once, exit
}

type countKey struct {
	m     *drain.Locked
	id    int
	level string
}

// localCount is what one read() collected for a key, with the template text at read time.
type localCount struct {
	n    int64
	tmpl string
}

type countVal struct {
	svc     string
	tmpl    string // last known text, used if the template is evicted before the push
	buckets map[int64]int64
}

type Agent struct {
	cfg    Config
	pool   *drain.Pool
	ex     *logparse.Extractor
	unwrap *logparse.Unwrapper // nil for plain files
	files  map[string]*tailFile
	buf    []byte
	client *http.Client

	mu     sync.Mutex
	counts map[countKey]*countVal

	lines, bytesRead, pushes, pushErrors, dropped, pushedBytes atomic.Int64
}

type tailFile struct {
	path    string
	svc     string
	f       *os.File
	fi      os.FileInfo
	off     int64
	partial []byte
	cpart   [2][]byte // partial container chunks (runtime-split long lines) for stdout, stderr
	gone    time.Time // when the path was first seen missing
}

// Kubernetes deletes a pod's log files after the pod goes away. Once a path has been missing for
// removeGrace, the handle is closed (a deleted file's disk space is only freed when the last reader
// closes it) and forgotten. kubelet's rotation (rename, then the runtime reopens the path) leaves
// the path missing for far less than this.
var removeGrace = 10 * time.Second

func New(cfg Config) *Agent {
	if cfg.Step <= 0 {
		cfg.Step = 60
	}
	a := &Agent{cfg: cfg, pool: drain.NewPool(cfg.Drain, cfg.MaxServices),
		ex:    logparse.NewExtractor(cfg.MessageField, cfg.ServiceField, cfg.LevelField),
		files: map[string]*tailFile{}, buf: make([]byte, 256<<10), counts: map[countKey]*countVal{},
		client: &http.Client{Timeout: 15 * time.Second}}
	if cfg.Container != "" {
		a.unwrap = logparse.NewUnwrapper(cfg.Container)
	}
	return a
}

func (a *Agent) Run(ctx context.Context) error {
	if a.cfg.Listen != "" {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /metrics", a.handleMetrics)
		srv := &http.Server{Addr: a.cfg.Listen, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
		go func() {
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("agent metrics listener", "err", err)
			}
		}()
		defer srv.Close()
	}
	a.scan(true)
	if a.cfg.Once {
		for a.pollAll() > 0 {
		}
		return a.push(ctx)
	}
	flush := time.NewTicker(a.cfg.Flush)
	defer flush.Stop()
	poll := time.NewTicker(a.cfg.Poll)
	defer poll.Stop()
	rescan := time.NewTicker(5 * time.Second)
	defer rescan.Stop()
	for {
		select {
		case <-ctx.Done():
			a.pollAll()
			return a.push(context.Background())
		case <-poll.C:
			for a.pollAll() > 0 { // keep reading while there is a backlog
				if ctx.Err() != nil {
					break
				}
			}
		case <-rescan.C:
			a.scan(false)
		case <-flush.C:
			if err := a.push(ctx); err != nil {
				slog.Warn("push failed; counts kept for the next push", "err", err)
			}
		}
	}
}

// scan picks up files matching the globs. Files present at start-up are read from the end
// unless FromStart; files that appear later (rotation, new services) from the beginning.
func (a *Agent) scan(startup bool) {
	for _, g := range a.cfg.Paths {
		matches, _ := filepath.Glob(g)
		for _, p := range matches {
			if _, ok := a.files[p]; ok {
				continue
			}
			tf := &tailFile{path: p, svc: a.serviceFor(p)}
			if err := a.open(tf, !startup || a.cfg.FromStart); err != nil {
				slog.Warn("open", "path", p, "err", err)
				continue
			}
			a.files[p] = tf
			slog.Info("tailing", "path", p, "service", tf.svc, "offset", tf.off)
		}
	}
}

func (a *Agent) serviceFor(path string) string {
	if re := a.cfg.ServiceFromPath; re != nil {
		if m := re.FindStringSubmatch(path); m != nil {
			if i := re.SubexpIndex("service"); i > 0 && m[i] != "" {
				return m[i]
			}
		}
	}
	return a.cfg.Service
}

func (a *Agent) open(tf *tailFile, fromStart bool) error {
	f, err := os.Open(tf.path)
	if err != nil {
		return err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	tf.f, tf.fi, tf.off, tf.partial = f, fi, 0, tf.partial[:0]
	tf.cpart[0], tf.cpart[1] = tf.cpart[0][:0], tf.cpart[1][:0]
	if !fromStart {
		tf.off, _ = f.Seek(0, io.SeekEnd)
	}
	return nil
}

// pollAll reads new data from every file once and returns the bytes read.
func (a *Agent) pollAll() int {
	total := 0
	for p, tf := range a.files {
		n := a.read(tf)
		total += n
		fi, err := os.Stat(tf.path)
		if err != nil { // removed, or between rename and reopen: keep reading the old handle for now
			if tf.gone.IsZero() {
				tf.gone = time.Now()
			} else if n == 0 && time.Since(tf.gone) > removeGrace {
				tf.f.Close()
				delete(a.files, p)
				slog.Info("stopped tailing removed file", "path", p)
			}
			continue
		}
		tf.gone = time.Time{}
		switch {
		case !os.SameFile(fi, tf.fi): // rotated: finish the old file, then follow the new one
			total += a.read(tf)
			tf.f.Close()
			if err := a.open(tf, true); err != nil {
				slog.Warn("reopen after rotation", "path", tf.path, "err", err)
			}
		case fi.Size() < tf.off: // truncated in place (copytruncate)
			tf.f.Seek(0, io.SeekStart)
			tf.off, tf.partial = 0, tf.partial[:0]
		}
	}
	return total
}

// read consumes up to 8 MiB from one file.
func (a *Agent) read(tf *tailFile) int {
	if tf.f == nil {
		return 0
	}
	now := time.Now().Unix()
	bucket := now / a.cfg.Step * a.cfg.Step
	local := map[countKey]*localCount{}
	total := 0
	for total < 8<<20 {
		n, err := tf.f.Read(a.buf)
		if n > 0 {
			total += n
			tf.off += int64(n)
			data := a.buf[:n]
			for {
				i := bytes.IndexByte(data, '\n')
				if i < 0 {
					tf.partial = append(tf.partial, data...)
					if len(tf.partial) > 1<<20 { // runaway line: count what we have
						a.line(tf, tf.partial, local)
						tf.partial = tf.partial[:0]
					}
					break
				}
				if len(tf.partial) > 0 {
					tf.partial = append(tf.partial, data[:i]...)
					a.line(tf, tf.partial, local)
					tf.partial = tf.partial[:0]
				} else {
					a.line(tf, data[:i], local)
				}
				data = data[i+1:]
			}
		}
		if err != nil || n == 0 {
			break
		}
	}
	if total > 0 {
		a.bytesRead.Add(int64(total))
		a.merge(local, bucket)
	}
	return total
}

// line mines one log line into the local counts.
func (a *Agent) line(tf *tailFile, b []byte, local map[countKey]*localCount) {
	b = bytes.TrimRight(b, "\r")
	if len(b) == 0 {
		return
	}
	s := string(b)
	if a.unwrap != nil {
		if env, ok := a.unwrap.Unwrap(s); ok { // not an envelope: mine the line as it is
			k := 0
			if env.Stderr {
				k = 1
			}
			if env.Partial {
				tf.cpart[k] = append(tf.cpart[k], env.Text...)
				if len(tf.cpart[k]) < 1<<20 { // runaway line: count what we have
					return
				}
			} else if len(tf.cpart[k]) > 0 {
				tf.cpart[k] = append(tf.cpart[k], env.Text...)
			}
			if len(tf.cpart[k]) > 0 {
				env.Text = string(tf.cpart[k])
				tf.cpart[k] = tf.cpart[k][:0]
			}
			if s = strings.TrimRight(env.Text, "\r"); s == "" {
				return
			}
		}
	}
	msg, svc, level := s, tf.svc, ""
	if a.cfg.Format == "json" {
		if vals, ok := a.ex.Extract(s); ok {
			msg, level = vals[0], logparse.Level(vals[2])
			if vals[1] != "" {
				svc = vals[1]
			}
		}
	}
	if level == "" {
		level = logparse.DetectLevel(msg)
	}
	if svc == "" {
		svc = "unknown"
	}
	m, _ := a.pool.Get(svc)
	m.Lock()
	c, ch := m.AddLine(msg)
	k := countKey{m, c.ID, level}
	lc := local[k]
	if lc == nil {
		lc = &localCount{}
		local[k] = lc
	}
	// Keep the text now: the template may be evicted from the miner before the next push.
	if lc.tmpl == "" || ch != drain.None {
		lc.tmpl = c.Template()
	}
	m.Unlock()
	lc.n++
}

func (a *Agent) merge(local map[countKey]*localCount, bucket int64) {
	var n int64
	a.mu.Lock()
	defer a.mu.Unlock()
	for k, c := range local {
		v := a.counts[k]
		if v == nil {
			v = &countVal{buckets: map[int64]int64{}}
			a.counts[k] = v
		}
		v.tmpl = c.tmpl
		v.buckets[bucket] += c.n
		n += c.n
	}
	a.lines.Add(n)
}

// push sends and clears the counts; on failure they are merged back (bounded by MaxBacklog).
func (a *Agent) push(ctx context.Context) error {
	a.mu.Lock()
	counts := a.counts
	a.counts = map[countKey]*countVal{}
	a.mu.Unlock()
	if len(counts) == 0 || a.cfg.Server == "" {
		return nil
	}
	p := wire.Push{Agent: a.cfg.Name, Step: a.cfg.Step}
	// Resolve service names and current template texts under each miner's lock.
	svcOf := a.pool.Keys()
	for k, v := range counts {
		k.m.Lock()
		if c := k.m.Get(k.id); c != nil {
			v.tmpl = c.Template()
		}
		k.m.Unlock()
		v.svc = svcOf[k.m]
		it := wire.Item{Service: v.svc, Template: v.tmpl, Level: k.level}
		for b, n := range v.buckets {
			it.Counts = append(it.Counts, [2]int64{b, n})
		}
		sort.Slice(it.Counts, func(i, j int) bool { return it.Counts[i][0] < it.Counts[j][0] })
		p.Items = append(p.Items, it)
	}
	var body bytes.Buffer
	zw := gzip.NewWriter(&body)
	_ = json.NewEncoder(zw).Encode(&p)
	zw.Close()
	err := a.post(ctx, body.Bytes())
	a.pushes.Add(1)
	if err == nil {
		a.pushedBytes.Add(int64(body.Len()))
		return nil
	}
	a.pushErrors.Add(1)
	a.mu.Lock()
	defer a.mu.Unlock()
	for k, v := range counts {
		cur := a.counts[k]
		if cur == nil {
			// Only new keys grow the backlog; counts for keys already in it are always kept.
			if len(a.counts) >= a.cfg.MaxBacklog {
				a.dropped.Add(1)
				continue
			}
			a.counts[k] = v
			continue
		}
		for b, n := range v.buckets {
			cur.buckets[b] += n
		}
		if cur.tmpl == "" {
			cur.tmpl = v.tmpl
		}
	}
	return err
}

func (a *Agent) post(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(a.cfg.Server, "/")+"/api/v1/templates", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("server: %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	return nil
}

func (a *Agent) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	e := wire.NewExpo(w)
	c := func(name, help string, v int64) {
		e.Family(name, help, "counter")
		e.Sample(name, nil, float64(v))
	}
	c("anomalyd_agent_lines_total", "Log lines read and mined.", a.lines.Load())
	c("anomalyd_agent_bytes_total", "Bytes read from log files.", a.bytesRead.Load())
	c("anomalyd_agent_pushes_total", "Pushes to the server.", a.pushes.Load())
	c("anomalyd_agent_push_errors_total", "Failed pushes.", a.pushErrors.Load())
	c("anomalyd_agent_pushed_bytes_total", "Compressed bytes pushed.", a.pushedBytes.Load())
	c("anomalyd_agent_backlog_dropped_total", "Count keys dropped because the backlog was full.", a.dropped.Load())
	e.Family("anomalyd_agent_templates", "Templates per service on this host.", "gauge")
	a.pool.Each(func(svc string, m *drain.Miner) {
		e.Sample("anomalyd_agent_templates", [][2]string{{"service", svc}}, float64(m.Len()))
	})
	_ = e.Flush()
}

// Templates calls fn for every mined template (bench output).
func (a *Agent) Templates(fn func(svc string, c *drain.Cluster)) {
	a.pool.Each(func(svc string, m *drain.Miner) { m.Clusters(func(c *drain.Cluster) { fn(svc, c) }) })
}

// Stats is used by the bench command.
func (a *Agent) Stats() (lines, bytes int64, templates int) {
	a.pool.Each(func(_ string, m *drain.Miner) { templates += m.Len() })
	return a.lines.Load(), a.bytesRead.Load(), templates
}
