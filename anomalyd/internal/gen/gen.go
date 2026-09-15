// Package gen produces synthetic logs and metrics with injected anomalies for tests and demos.
// Nothing here talks to real systems; the metric model is deterministic in (service, time),
// so history files and live values line up.
package gen

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/elvismanchkin/self-hosted-anomaly-detection/anomalyd/internal/promrw"
)

// Templates follow poc/logs/gen_logs.py (same services and messages).
var Templates = map[string][]string{
	"checkout": {
		"Order {oid} created for user {uid} total={amount} currency=EUR",
		"Payment authorized order={oid} provider=stripe latency_ms={ms}",
		"GET /api/v1/cart/{uid} 200 {ms}ms",
		"POST /api/v1/checkout 201 {ms}ms",
		"Retrying payment call attempt={n} order={oid}",
		"Inventory reserved sku={sku} qty={n} warehouse=wh-{n}",
	},
	"auth": {
		"User {uid} logged in from {ip}",
		"Token refreshed for session {uuid}",
		"Login failed for user {uid} from {ip}: bad password",
		"GET /oauth/authorize 302 {ms}ms",
		"JWKS cache refreshed keys={n}",
	},
	"search": {
		"Query executed q_hash={hex} hits={n} took={ms}ms",
		"Cache miss key=search:{hex}",
		"Cache hit key=search:{hex}",
		"Shard {n} responded in {ms}ms",
		"Slow query detected took={ms}ms threshold=500ms",
	},
	"gateway": {
		`{ip} - - "GET /api/v1/products/{n} HTTP/1.1" 200 {bytes} {ms}ms`,
		`{ip} - - "POST /api/v1/orders HTTP/1.1" 201 {bytes} {ms}ms`,
		`{ip} - - "GET /healthz HTTP/1.1" 200 2 0ms`,
		"Upstream checkout responded 503 retry={n}",
		"Rate limit applied client={ip} limit=100/s",
	},
	"worker": {
		"Job {uuid} started type=email",
		"Job {uuid} finished in {ms}ms",
		"Consumed {n} messages from topic orders partition={n}",
		"Committed offset {n} for partition {n}",
	},
}

const (
	AnomalyService  = "checkout"
	AnomalyTemplate = "ERROR db pool exhausted: timeout acquiring connection after {ms}ms (active={n} idle=0)"
)

func Services() []string {
	s := make([]string, 0, len(Templates))
	for k := range Templates {
		s = append(s, k)
	}
	sort.Strings(s)
	return s
}

func render(tpl string, r *rand.Rand) string {
	var b strings.Builder
	for {
		i := strings.IndexByte(tpl, '{')
		if i < 0 {
			b.WriteString(tpl)
			return b.String()
		}
		j := strings.IndexByte(tpl[i:], '}')
		b.WriteString(tpl[:i])
		switch tpl[i+1 : i+j] {
		case "oid":
			fmt.Fprint(&b, 1_000_000+r.IntN(9_000_000))
		case "uid":
			fmt.Fprint(&b, 1+r.IntN(1_000_000))
		case "amount":
			fmt.Fprintf(&b, "%.2f", 1+r.Float64()*499)
		case "ms":
			fmt.Fprint(&b, 1+r.IntN(2000))
		case "n":
			fmt.Fprint(&b, r.IntN(65))
		case "sku":
			fmt.Fprintf(&b, "SKU-%d", 1000+r.IntN(9000))
		case "ip":
			fmt.Fprintf(&b, "10.%d.%d.%d", r.IntN(256), r.IntN(256), 1+r.IntN(254))
		case "uuid":
			u := r.Uint64()
			v := r.Uint64()
			fmt.Fprintf(&b, "%08x-%04x-%04x-%04x-%012x", u>>32, (u>>16)&0xffff, u&0xffff, v>>48, v&0xffffffffffff)
		case "hex":
			fmt.Fprintf(&b, "%016x", r.Uint64())
		case "bytes":
			fmt.Fprint(&b, 100+r.IntN(49_900))
		}
		tpl = tpl[i+j+1:]
	}
}

type LogOptions struct {
	Lines           int           // batch mode: total lines (0 = live mode)
	AnomalyFraction float64       // batch: tail fraction with the injected burst
	Rate            float64       // live: lines per second (all services)
	Duration        time.Duration // live: stop after (0 = forever)
	InjectAfter     time.Duration // live: start the burst after this long (<0 = never)
	DropService     string        // live: this service stops logging after DropAfter
	DropAfter       time.Duration
	OutDir          string // one <dir>/<service>/app.log per service; "" = Out
	Container       string // "" | cri | docker: wrap each line as the container runtime writes stdout
	AppFormat       string // json (ECS-style NDJSON) | text ("<ts> LEVEL message")
	Out             io.Writer
	Seed            uint64
}

// containerFile names a service's log the way kubelet links it: /var/log/containers/<pod>_<ns>_<container>-<id>.log.
func containerFile(svc string) string {
	h := hash(svc)
	return fmt.Sprintf("%s-%09x-%05x_shop_%s-%x.log", svc, h>>28, h&0xfffff, svc, sha256.Sum256([]byte(svc)))
}

type event struct {
	Timestamp string `json:"@timestamp"`
	Service   struct {
		Name string `json:"name"`
	} `json:"service"`
	Log struct {
		Level string `json:"level"`
	} `json:"log"`
	Message string `json:"message"`
}

// Logs writes Filebeat-like NDJSON, plain text, or either wrapped as container stdout.
func Logs(ctx context.Context, o LogOptions) error {
	r := rand.New(rand.NewPCG(o.Seed, 42))
	svcs := Services()
	writers := map[string]*bufio.Writer{}
	var files []*os.File
	defer func() {
		for _, w := range writers {
			w.Flush()
		}
		for _, f := range files {
			f.Close()
		}
	}()
	out := func(svc string) (*bufio.Writer, error) {
		if o.OutDir == "" {
			if w := writers[""]; w != nil {
				return w, nil
			}
			w := bufio.NewWriterSize(o.Out, 256<<10)
			writers[""] = w
			return w, nil
		}
		if w := writers[svc]; w != nil {
			return w, nil
		}
		dir, name := filepath.Join(o.OutDir, svc), "app.log"
		if o.Container != "" {
			dir, name = o.OutDir, containerFile(svc)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
		w := bufio.NewWriterSize(f, 64<<10)
		writers[svc] = w
		return w, nil
	}
	emit := func(ts time.Time, svc, tpl string) error {
		var ev event
		ev.Timestamp = ts.UTC().Format("2006-01-02T15:04:05.000Z")
		ev.Service.Name = svc
		ev.Message = render(tpl, r)
		ev.Log.Level = "info"
		if strings.HasPrefix(tpl, "ERROR") || strings.Contains(tpl, "failed") {
			ev.Log.Level = "error"
		} else if strings.HasPrefix(tpl, "Retrying") || strings.HasPrefix(tpl, "Slow") || strings.Contains(tpl, "503") {
			ev.Log.Level = "warn"
		}
		w, err := out(svc)
		if err != nil {
			return err
		}
		var b []byte
		if o.AppFormat == "text" {
			b = fmt.Appendf(nil, "%s %s %s", ev.Timestamp, strings.ToUpper(ev.Log.Level), ev.Message)
		} else {
			b, _ = json.Marshal(&ev)
		}
		stream := "stdout"
		if ev.Log.Level == "error" {
			stream = "stderr"
		}
		switch o.Container {
		case "cri":
			w.WriteString(ts.UTC().Format("2006-01-02T15:04:05.000000000Z07:00") + " " + stream + " F ")
		case "docker":
			b, _ = json.Marshal(struct {
				Log    string `json:"log"`
				Stream string `json:"stream"`
				Time   string `json:"time"`
			}{string(b) + "\n", stream, ts.UTC().Format(time.RFC3339Nano)})
		}
		w.Write(b)
		return w.WriteByte('\n')
	}
	pick := func(inject bool, drop string) (string, string) {
		for {
			svc := svcs[r.IntN(len(svcs))]
			if svc == drop {
				continue
			}
			if inject && r.Float64() < 0.3 {
				return AnomalyService, AnomalyTemplate
			}
			t := Templates[svc]
			return svc, t[r.IntN(len(t))]
		}
	}

	if o.Lines > 0 { // batch
		start := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
		from := int(float64(o.Lines) * (1 - o.AnomalyFraction))
		for i := 0; i < o.Lines; i++ {
			svc, tpl := pick(i >= from, "")
			if err := emit(start.Add(time.Duration(i)*time.Millisecond), svc, tpl); err != nil {
				return err
			}
		}
		return nil
	}

	// Live: write in 100 ms ticks at the requested rate, flushing each tick.
	t0 := time.Now()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	carry := 0.0
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-tick.C:
			el := now.Sub(t0)
			if o.Duration > 0 && el >= o.Duration {
				return nil
			}
			inject := o.InjectAfter >= 0 && el >= o.InjectAfter
			drop := ""
			if o.DropService != "" && el >= o.DropAfter {
				drop = o.DropService
			}
			carry += o.Rate / 10
			for ; carry >= 1; carry-- {
				svc, tpl := pick(false, drop)
				if err := emit(now, svc, tpl); err != nil {
					return err
				}
				// Live bursts come on top of normal traffic (batch mode replaces lines, like gen_logs.py).
				if inject && r.Float64() < 0.3 {
					if err := emit(now, AnomalyService, AnomalyTemplate); err != nil {
						return err
					}
				}
			}
			for _, w := range writers {
				w.Flush()
			}
		}
	}
}

// --- Metrics model ---------------------------------------------------------------------------

// Metric is one synthetic series family, named like the recording rules that would produce it.
type Metric struct {
	Name, Raw   string // recorded name (history, remote write) and raw gauge name (live scrape)
	Type        string // anomaly_type label
	AnomalyName string
	Strategy    string
}

var Metrics = []Metric{
	{"anomaly:svc:requests:rate1m", "synthetic_requests_rate", "requests", "svc_requests", "adaptive"},
	{"anomaly:svc:errors:ratio1m", "synthetic_error_ratio", "errors", "svc_error_ratio", "robust"},
}

// Injection changes one service's values from At on.
type Injection struct {
	Service string
	At      time.Time
	Kind    string // spike (requests x3), drop (requests x0.2), errors (error ratio 0.08)
}

func splitmix(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

func hash(s string) uint64 {
	h := uint64(14695981039346656037)
	for i := 0; i < len(s); i++ {
		h = (h ^ uint64(s[i])) * 1099511628211
	}
	return h
}

// unit is a deterministic uniform [0,1) per (service, metric, minute).
func unit(svc, metric string, t int64) float64 {
	return float64(splitmix(hash(svc+metric)^uint64(t/60))>>11) / float64(1<<53)
}

// Value of metric m for service svc at unix time t.
func Value(m Metric, svc string, t int64, inj *Injection) float64 {
	h := hash(svc)
	day := 2 * math.Pi * float64(t%86400) / 86400
	injected := inj != nil && inj.Service == svc && t >= inj.At.Unix()
	switch m.Type {
	case "requests":
		base := 50 + float64(h%350)
		phase := float64(h%24) / 24 * 2 * math.Pi / 6
		wk := 1.0
		if wd := time.Unix(t, 0).UTC().Weekday(); wd == time.Saturday || wd == time.Sunday {
			wk = 0.7
		}
		v := base * (1 + 0.5*math.Sin(day+phase)) * wk * (1 + 0.06*(unit(svc, m.Name, t)-0.5))
		if injected && inj.Kind == "spike" {
			v *= 3
		}
		if injected && inj.Kind == "drop" {
			v *= 0.2
		}
		return v
	case "errors":
		v := 0.002 + 0.002*unit(svc, m.Name, t)
		if injected && inj.Kind == "errors" {
			v = 0.08
		}
		return v
	}
	return 0
}

func (m Metric) labels(svc string) [][2]string { // sorted by name
	return [][2]string{{"__name__", m.Name}, {"anomaly_name", m.AnomalyName}, {"anomaly_strategy", m.Strategy},
		{"anomaly_type", m.Type}, {"service", svc}}
}

// History writes OpenMetrics text for `promtool tsdb create-blocks-from openmetrics`: the recorded
// series for [end-days, end) at step.
func History(w io.Writer, end time.Time, days int, step time.Duration) error {
	bw := bufio.NewWriterSize(w, 1<<20)
	from := end.Add(-time.Duration(days) * 24 * time.Hour).Unix()
	st := int64(step / time.Second)
	for _, m := range Metrics {
		fmt.Fprintf(bw, "# TYPE %s gauge\n", m.Name)
		for _, svc := range Services() {
			lbl := fmt.Sprintf(`anomaly_name="%s",anomaly_strategy="%s",anomaly_type="%s",service="%s"`,
				m.AnomalyName, m.Strategy, m.Type, svc)
			for t := from / st * st; t < end.Unix(); t += st {
				fmt.Fprintf(bw, "%s{%s} %g %d\n", m.Name, lbl, Value(m, svc, t, nil), t)
			}
		}
	}
	bw.WriteString("# EOF\n")
	return bw.Flush()
}

// ServeMetrics exposes the raw gauges (live values, with the injection) for Prometheus to scrape.
func ServeMetrics(ctx context.Context, addr string, inj *Injection) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, _ *http.Request) {
		now := time.Now().Unix()
		var b bytes.Buffer
		for _, m := range Metrics {
			fmt.Fprintf(&b, "# HELP %s synthetic %s\n# TYPE %s gauge\n", m.Raw, m.Type, m.Raw)
			for _, svc := range Services() {
				fmt.Fprintf(&b, "%s{service=%q} %g\n", m.Raw, svc, Value(m, svc, now, inj))
			}
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		w.Write(b.Bytes())
	})
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { <-ctx.Done(); srv.Close() }()
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

// RemoteWrite pushes [from, to) of every series at step to url in batches, as Prometheus would.
func RemoteWrite(ctx context.Context, url string, from, to time.Time, step time.Duration, inj *Injection) (int, error) {
	var enc promrw.Encoder
	client := &http.Client{Timeout: 30 * time.Second}
	st := int64(step / time.Second)
	total := 0
	const chunk = 6 * 3600 // seconds of data per request
	for start := from.Unix() / st * st; start < to.Unix(); start += chunk {
		end := min(start+chunk, to.Unix())
		var batch []promrw.Series
		for _, m := range Metrics {
			for _, svc := range Services() {
				s := promrw.Series{Labels: m.labels(svc)}
				for t := start; t < end; t += st {
					s.Samples = append(s.Samples, promrw.Sample{Value: Value(m, svc, t, inj), Timestamp: t * 1000})
				}
				total += len(s.Samples)
				batch = append(batch, s)
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(enc.Encode(batch)))
		if err != nil {
			return total, err
		}
		req.Header.Set("Content-Type", "application/x-protobuf")
		req.Header.Set("Content-Encoding", "snappy")
		req.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")
		resp, err := client.Do(req)
		if err != nil {
			return total, err
		}
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return total, fmt.Errorf("remote write: %s: %s", resp.Status, msg)
		}
	}
	return total, nil
}
