# Proof of concept (PoC): configs and small services for tiered anomaly detection

**In short:**

- This folder holds the configs and small services of our proof of concept (PoC). The configs
  set up open-source tools that we reuse. The small Python services are our own code.
- Everything here is an add-on to our stack. Nothing replaces a core component.
- The files cover all three signals: metrics (Prometheus rules), traces (OpenTelemetry
  Collector) and logs (Filebeat, Elasticsearch, ElastAlert2, Drain3).
- We tested every file with its real tool, on synthetic data only
  ([What was verified](#what-was-verified-2026-09-14)).
- The tests found many surprising problems. Some of them lose data or silence alerts without any
  error ([Gotchas](#gotchas-found-while-validating)).
- Our own small Go engine, anomalyd, puts the log mining and the tier-2 scoring into one binary:
  [../anomalyd/](../anomalyd/README.md).

Terms and abbreviations: [glossary](../docs/glossary.md).

Sections:
[What is here](#what-is-here) ·
[How to try each piece](#how-to-try-each-piece) ·
[What was verified](#what-was-verified-2026-09-14) ·
[Gotchas](#gotchas-found-while-validating)

Our stack stays as it is:

- Filebeat → Elasticsearch/Kibana, on the free Basic license
- Prometheus + Grafana OSS + Alertmanager
- Jaeger

Apps log to stdout. The container runtime writes that output to files on each node
(`/var/log/containers/*.log` → `/var/log/pods/...`). The files use the
[CRI logging format](https://kubernetes.io/docs/concepts/cluster-administration/logging/)
(Container Runtime Interface). Filebeat tails these files, that is, it reads new lines as they
are written. The "second readers" in this folder read the same files next to Filebeat, without
changing it.

Hostnames such as `elasticsearch.internal` and `alertmanager.internal` are placeholders.

See [../docs/reference-architecture.md](../docs/reference-architecture.md) for how the pieces fit together.

A separate Go PoC lives in [../anomalyd/](../anomalyd/README.md). It is our own small engine. It
puts the log mining and the tier-2 scoring into one binary: an agent on each node (the edge) and
a central server.

## What is here

The **Tier** column uses the detection tiers of the reference architecture:

- **T0:** rules and Service Level Objective (SLO)
  [burn-rate alerts](https://sre.google/workbook/alerting-on-slos/). Only T0 pages a person.
- **T1:** cheap statistics on all aggregates.
- **T2:** a seasonal scorer on the top-K series (a short list of the most useful series).

"T1 input" means that the file produces data that a T1 detector reads. The OpenTelemetry (OTel)
"tier 1" and "tier 2" in the file names are different. They are two layers of OTel Collectors in
the trace path.

| Path | Tier | Signal | What it does |
|---|---|---|---|
| [prometheus/anomaly-inputs.yml](prometheus/anomaly-inputs.yml) | T1 | metrics, traces, logs | Service-level [recording rules](https://prometheus.io/docs/prometheus/latest/configuration/recording_rules/), in the input format of promql-anomaly-detection (note 1) |
| [prometheus/anomaly-alerts.yml](prometheus/anomaly-alerts.yml) | T0, T1, T2 | all | Burn-rate, log-novelty, tier-2 score and detector-health alerts (note 2) |
| [prometheus/alertmanager.yml](prometheus/alertmanager.yml) | routing | all | One router for all alerts. It pages only on `severity=page` (note 3). |
| [prometheus/tests/](prometheus/tests/) | – | – | `promtool test rules` unit tests, plus an end-to-end test through the upstream adaptive rules |
| [otel/traces-tier1-loadbalancer.yaml](otel/traces-tier1-loadbalancer.yaml) | – | traces | Stateless OTel Collector tier. It routes all spans of a trace to one tier-2 instance. |
| [otel/traces-tier2-spanmetrics-tailsampling.yaml](otel/traces-tier2-spanmetrics-tailsampling.yaml) | T1 input | traces | [RED metrics](https://grafana.com/blog/2018/08/02/the-red-method-how-to-instrument-your-services/) from 100 % of spans, then [tail sampling](https://opentelemetry.io/docs/concepts/sampling/) for Jaeger (note 4) |
| [otel/logs-drain-count.yaml](otel/logs-drain-count.yaml) | T1 input | logs | Second reader on each node: [Drain](https://jiemingzhu.github.io/pub/pjhe_icws2017.pdf) templates → line counts → Prometheus (note 5) |
| [logs/elastic/filebeat-template-key.yml](logs/elastic/filebeat-template-key.yml) | T1 input | logs | Filebeat processors that add a stateless `log.template_id` (note 6) |
| [logs/elastic/ingest-pipeline-template-key.json](logs/elastic/ingest-pipeline-template-key.json) | T1 input | logs | The same template key as an Elasticsearch ingest pipeline, a central alternative (note 7) |
| [logs/elastic/transform-logs-tmpl-1m.json](logs/elastic/transform-logs-tmpl-1m.json) | T1 input | logs | Continuous transform (free on Basic): 1-minute counts per service × template × level (note 8) |
| [logs/elastalert2/](logs/elastalert2/) | T1 | logs | ElastAlert2 config and rules (note 9) |
| [logs/drain3_exporter.py](logs/drain3_exporter.py) | T1 input | logs | Python alternative to the OTel drain path (note 10) |
| [logs/gen_logs.py](logs/gen_logs.py) | – | – | Synthetic Filebeat-like logs with an injected new-error burst. All log tests use it. |
| [ml/prom_anomaly_job.py](ml/prom_anomaly_job.py) | T2 | metrics | Scores top-K series against a seasonal median and [median absolute deviation (MAD)](https://en.wikipedia.org/wiki/Median_absolute_deviation) (note 11) |
| [ml/queries.example.yml](ml/queries.example.yml) | T2 | metrics | Example query set for the scorer |

Notes:

1. A recording rule stores the result of a query as a new series. Our rules carry the labels `anomaly_name` /
   `anomaly_type` / `anomaly_strategy`. That is the input format of
   [grafana/promql-anomaly-detection](https://github.com/grafana/promql-anomaly-detection) v0.2.1.
   It computes two kinds of band, adaptive and robust. A band is the range of normal values.
2. The file has four kinds of alert:
   - SLO multi-window burn-rate alerts. They are the only alerts that page.
   - A log-novelty alert, for a new log template.
   - An alert on the machine-learning (ML) score of the tier-2 scorer.
   - Detector-health alerts.
3. Anomaly findings go to chat and to a webhook for large language model (LLM) triage. The LLM
   webhook has a budget cap. Findings are grouped per `service`. Alertmanager inhibits them (holds
   them back) while a page is firing.
4. `span_metrics` + `service_graph` run on 100 % of spans. They produce
   [span metrics](https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/v0.160.0/connector/spanmetricsconnector/README.md),
   that is, RED metrics (rate, errors, duration). Then `tail_sampling` decides which traces
   Jaeger keeps. It keeps errors, slow traces and a 2 % baseline. Tail sampling decides after
   it has seen the whole trace.
5. The pipeline is `file_log` on `/var/log/pods` → `container` operator → `drain` (alpha) →
   `count` → Prometheus. Drain groups similar log lines and turns each group into a template. The metric names for lines and
   templates are the same as in the Drain3 exporter. `service` = container name. Filebeat is
   untouched.
6. The processors mask the variable parts of each line and hash the rest. "Stateless" means that
   each line gets its key on its own, without memory of earlier lines. The file also has the
   container-stdout input that the processors sit behind (`container` + `ndjson` parsers). The
   container name is the fallback for `service.name`.
7. The pipeline also sets `service.name` from the container name. It sets `log.level` from a
   leading level word on plain-text lines.
8. The output index is `anomaly-logs-tmpl-1m`. We call it the rollup: one row of counts per
   minute.
9. The recommended rules read the `anomaly-logs-tmpl-1m` rollup: `new-log-template` and
   `error-template-spike`. The raw-index rules work without a template key:
   - `error-spike-per-service`
   - `error-ratio-per-service`
   - `log-source-flatline`
   - `new-error-type-per-service`
10. It reads lines from stdin, mines [Drain3](https://github.com/logpai/Drain3) templates and
    exports Prometheus counters. It caps label cardinality, that is, the number of distinct label
    values. It reads parsed events (a copy sent on after Filebeat), not the runtime's container
    files.
11. The scorer reads top-K pre-aggregated series. It exports `anomaly_score` for Prometheus to
    scrape. It uses the standard library plus `requests`, `PyYAML` and `prometheus-client`. For
    each time slot, the seasonal median is the median of the same slot in earlier seasons (for
    example, the same hour in earlier weeks). The MAD measures how far values usually are from
    the median. See
    [../anomalyd/README.md](../anomalyd/README.md#how-detection-works) for the full method.

## How to try each piece

**Prometheus rules.** The repo has no copy of the upstream rules. Fetch them first, then run
the tests:
```bash
mkdir -p poc/prometheus/vendor && curl -sL https://github.com/grafana/promql-anomaly-detection/archive/refs/tags/v0.2.1.tar.gz | tar xz -C poc/prometheus/vendor --strip-components=2 promql-anomaly-detection-0.2.1/rules/adaptive.yml promql-anomaly-detection-0.2.1/rules/robust.yml
```
```bash
cd poc/prometheus/tests && promtool test rules anomaly.test.yml e2e-adaptive.test.yml
```
Then add these four files to `rule_files` in the Prometheus config:

- `anomaly-inputs.yml`
- `anomaly-alerts.yml`
- `vendor/adaptive.yml`
- `vendor/robust.yml`

**OTel Collector configs** (otelcol-contrib v0.160.0). Validate a config:
```bash
docker run --rm -v "$PWD/poc/otel":/cfg otel/opentelemetry-collector-contrib:0.160.0 validate --config=/cfg/traces-tier2-spanmetrics-tailsampling.yaml
```

**Elasticsearch template key and rollup** (Basic license). First set `ES` to your cluster URL,
for example `export ES=http://elasticsearch.internal:9200`. Then create the ingest pipeline,
create the transform and start it:
```bash
curl -XPUT "$ES/_ingest/pipeline/logs-template-key" -H 'Content-Type: application/json' --data-binary @poc/logs/elastic/ingest-pipeline-template-key.json
```
```bash
curl -XPUT "$ES/_transform/logs-tmpl-1m" -H 'Content-Type: application/json' --data-binary @poc/logs/elastic/transform-logs-tmpl-1m.json
```
```bash
curl -XPOST "$ES/_transform/logs-tmpl-1m/_start"
```

**Drain3 exporter.** Create a Python virtual environment. Then generate 300,000 synthetic lines
and run the benchmark on them:
```bash
python3 -m venv .venv && .venv/bin/pip install -r poc/logs/requirements.txt
```
```bash
.venv/bin/python poc/logs/gen_logs.py --lines 300000 > /tmp/s.ndjson && .venv/bin/python poc/logs/drain3_exporter.py --bench < /tmp/s.ndjson
```

**Tier-2 scorer.** Install its requirements and run the self-test. Then run one scoring pass
against a Prometheus server:
```bash
.venv/bin/pip install -r poc/ml/requirements.txt && .venv/bin/python poc/ml/prom_anomaly_job.py --selftest
```
```bash
.venv/bin/python poc/ml/prom_anomaly_job.py --config poc/ml/queries.example.yml --prometheus http://prometheus:9090 --once
```

## What was verified (2026-09-14)

We ran all checks locally, on synthetic data. Nothing ran against our production systems.

| Component | Check | Result |
|---|---|---|
| Prometheus rules | `promtool check rules` (Prometheus 3.14.0) on our 2 files and upstream `adaptive.yml`/`robust.yml` | SUCCESS (5 + 10 + 14 + 21 rules) |
| Prometheus rules | `promtool test rules` (note 1) | SUCCESS (note 1) |
| Prometheus rules + upstream adaptive bands | End-to-end test: 2 flat days at 100 req/s, then a 3× step | `AnomalyDetected` fires about 7 min after onset, resolves about 17 min in (note 2) |
| Alertmanager | `amtool check-config` (v0.34.0) plus `config routes test --verify.receivers` (note 3) | SUCCESS (note 3) |
| OTel configs | `otelcol-contrib validate` (v0.160.0) | All 3 OK, after we fixed 3 real errors |
| OTel logs path | Runtime test on container stdout, 10 runs (note 4) | With the final config, 10 of 10 runs counted every line (note 4) |
| OTel traces path | Runtime test with `telemetrygen`, spans from 2 app pods of one service (note 5) | Metric names and labels match the Prometheus rules (note 5) |
| Filebeat snippet | Filebeat 9.5.3 `test config`, plus a run over 40,003 CRI lines (note 6) | Config OK. 24 template ids for JSON apps and 24 for text apps (note 6). |
| Ingest pipeline + transform | Local Elasticsearch 9.5.3 container, default Basic license (note 7) | 24 template ids. Rollup of 116 rows with sum(count) = 300,000 (note 7). |
| ElastAlert2 2.31.0 | All 6 rules load and run against the local Elasticsearch (note 8) | 10 alerts, each with `service` and `tier="1"` labels (note 8) |
| Drain3 exporter | Benchmark on 300k synthetic lines, `/metrics` scraped | 96k–110k lines/s on one core in this PoC benchmark. It detects the new template (note 9). |
| Tier-2 scorer | `--selftest`, `--once` and server mode (note 10) | PASS. Spike z = 19.7, normal z = 1.0 (note 10). |

Notes:

1. The unit tests check these cases:
   - The burn-rate alert fires at 3 % errors and stays quiet at 0.05 %.
   - The novelty alert fires. It stays quiet during exporter warm-up. A restart of one exporter
     shard does not mute the others.
   - Templates under 5 lines/min get no band.
   - The score, stale and cap alerts fire. The stale alert also fires when the scorer's series
     is gone.

   The shard, sparse-template and scorer-down cases fail against the previous rules.
2. "Onset" is the moment the step starts. See the gotcha about adaptive bands below.
3. We tested the routes for a page, tier-1, tier-2, `AnomalyDetected`, ElastAlert2
   (`source=elastalert`, `tier=1`) and a meta alert. A meta alert reports the health of the
   detectors themselves. The results:
   - The page goes to `oncall-pager` only.
   - All four finding types go to `llm-triage` + `chat-anomalies`.
   - Meta alerts go to `chat-platform`.
4. The input was 40,003 CRI lines in the `/var/log/pods` layout. It had 5 services, JSON and
   plain-text apps, and split lines. The collector read 12 files at once.
   - The config exports `log_lines_total{service}` and
     `log_template_lines_total{service, template_id}`. These are the names that the Prometheus
     rules read, with `service` = container name.
   - The final config clears the record time. It puts `batch` + `groupbyattrs` in front of
     `count`. It removes the per-pod resource. See the gotchas.
   - With the final config, 10 of 10 runs counted every line, with 62–77 template series.
   - Before those fixes, runs counted 8–36k of 40k lines.
   - The earlier plain-file config counted 300k lines in 6 of 6 interleaved replays.
5. `telemetrygen` is the synthetic trace generator of OTel Collector contrib. The output has
   `traces_span_metrics_calls_total` and `traces_span_metrics_duration_milliseconds_bucket`
   with `service_name`, `span_kind` and `status_code` labels. These match the Prometheus rules.
   The spans of both pods land in one series per operation.
6. The files were named like `/var/log/containers`. They had 5 services, JSON and plain-text
   apps, and split lines. A `dissect` of the path stood in for `add_kubernetes_metadata`.
   - Text apps gave 13,218 template ids before we added the `<TS>` mask.
   - `service.name` comes from the app's JSON or from the container name.
   - The template text is the same as in the ingest pipeline.
7. The test ran `_simulate`, a bulk load of 300k docs and a continuous transform.
   - **Why 24 ids:** the 25 synthetic templates collapse to 23 under the masks, because `<STR>`
     merges the 3 gateway access-log lines. The injected template makes 24.
   - **Re-run:** we stretched the lines over 60 min and added 510 extra lines. 500 of them had
     no `service.name`, and 10 had a 1,160-char template. The result was 1,831 rows,
     sum(count) = 300,510. Without `missing_bucket`, the transform dropped those 510 lines.
   - **License check:** `_xpack` reports ml ✗, watcher ✗, transform ✓, esql ✓.
   - **Container-stdout additions**, tested with `_simulate` on Elasticsearch 9.5.3:
     - `service.name` falls back to `kubernetes.container.name` only when it is missing.
     - `log.level` comes from `INFO`/`ERROR`/`[WARN]`/`ERROR:` after an optional timestamp.
     - `<TS>` masks ISO timestamps with `T` or space and with `.`/`,` fractions.
8. All 6 rules load through the rules loader (schema, rule type and alerter). All 6 ran with
   `elastalert --start … --end …` against the local Elasticsearch. They posted to a local
   stand-in for Alertmanager. The 10 alerts were:
   - template spike, new template, new error type, error spike and error ratio on the injected
     `checkout` burst.
   - 5 flatlines once logging stopped. A flatline alert fires when a source goes silent.
9. The data was synthetic: 141 B/line, 23 templates. A separate side-by-side run with anomalyd
   gave 94–95k lines/s
   ([../anomalyd/README.md](../anomalyd/README.md#why-build-this-instead-of-using-the-python-pieces)).
10. The self-test used Python 3.14 and pinned requirements. `--once` and server mode ran
    against a stub Prometheus API. The z value is a
    [z-score](https://en.wikipedia.org/wiki/Standard_score): the distance from the expected
    value, divided by the usual spread.
    - Exported series keep only `anomaly_query` and the input's own labels (no `anomaly_*`).
    - The last-success timestamp does not advance while every query fails.
    - Scoring takes about 1.0–1.1 ms per series with 2 weeks of 5-min history. With 4 weeks it
      takes 1.5–1.6 ms.

We did not verify:

- behaviour on our real logs and metrics
- throughput on real log shapes
- the OTel logs path at real line rates
- tail-sampling memory at real span rates
- ElastAlert2 against Elasticsearch 7.x

## Gotchas found while validating

A gotcha is a surprising problem: the config looks right, but the tool does something we did
not expect. We found these while testing. Each item says what goes wrong and how we fixed or
avoided it.

### Container stdout: plain text and split lines

- **Plain-text stdout breaks a stateless template key unless timestamps are masked**. Lines like
  `2026-09-14T00:00:00.001Z INFO …` gave 13,218 template ids for 20k lines under the `<NUM>`
  mask alone, because the milliseconds survive it. A `<TS>` mask first brings it to 24. Readers
  based on Drain (anomalyd, OTel `drain`) cope without it.
- **Plain-text stdout has no `log.level`**. Filebeat has no grok (a pattern parser). So the
  level-based ElastAlert2 rules see only JSON apps, unless the grok processor in the ingest
  pipeline sets the level.
- **Filebeat 9.5.3's `container` parser joins a split (P) stdout line with whatever line comes
  next, stderr included**. The runtime tags each partial chunk of a long line with `P`. In our
  test, a stderr line was written between two chunks of a split stdout line. Filebeat merged it
  into the stdout line, and the rest of the stdout line became a second event. The OTel
  `container` operator also mangled that case. anomalyd keeps one buffer per stream.

### OpenTelemetry Collector

- **`count` emits one stream per resource**. In OTel, a resource describes the source of the
  data, for example a pod. The container operator makes each pod a resource. The Prometheus
  exporter then keeps one pod's value per label set instead of the sum. With two pods per
  service, `log_lines_total` showed about half of the lines. The fix: remove the resource after
  deriving `service`.
- **The `count` connector emits delta sums**. A delta reports only the change since the last
  report, not the running total. If you send them straight to the Prometheus exporter, only the
  latest batch shows up (536 of 300k lines in the test). `delta_to_cumulative` is required.
- **`delta_to_cumulative` drops out-of-order deltas, and `count` produces them**.
  - `count` stamps each delta with the min/max timestamp of its records. When the records have
    no timestamp, it uses the current time
    ([counter.go](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/v0.160.0/connector/countconnector/counter.go)).
  - The `container` operator sets record times, and `file_log` reads files concurrently. So
    batches carry overlapping time ranges.
  - Up to 79 % of 40k lines were dropped
    (`otelcol_deltatocumulative_datapoints{error="delta.ErrOutOfOrder"}`).
  - Our fix: the config clears the record time (`transform`). Then `batch` (one sender) and
    `groupbyattrs` (one ResourceLogs per batch) run in front of `count`. With this, 10 of 10 runs
    counted every line.
  - On plain files, `batch` alone had fixed 2 of 6 lossy replays (1,400 and 7,855 lines lost).
- **The drain processor's `warmup_min_clusters` counts clusters across all services on the
  collector**. A cluster is a group of similar lines, that is, one template. Until the collector
  reaches the setting, lines count as `template_id="other"`.
  - When one service's file was read before the others, all 63,857 of its lines landed in
    `other`.
  - A host with fewer distinct templates than the setting never leaves warm-up.
    `LogTemplateCapReached` then fires.
  - The first line of each new cluster gets its literal text as template. That makes a 1-line
    series, and the 5 lines/min filter keeps it out of the bands.
- **`file_log` is the current receiver name**. `filelog` still works, but it is a deprecated
  alias in v0.160.0. In the same way, the `otlp` exporter is now `otlp_grpc`.
- **`span_metrics` splits every series per app pod**, because `service.instance.id` becomes the
  `instance` label. Set `resource_metrics_key_attributes: [service.name]` to stop this. With it,
  two pods' spans land in one series, labelled with the first pod seen.
- **The `load_balancing` Domain Name System (DNS) resolver `port` must be a string** (`"4317"`).
  An integer fails config decoding.
- **OpenTelemetry Transformation Language (OTTL) `set_semconv_span_name(...)` needs an explicit
  `context: span` block**. Without it, context inference fails.
- **The OTel Prometheus exporter adds a `job` label from `service.name`**. On scrape it becomes
  `exported_job`.

### Elasticsearch and Filebeat

- **A transform's destination must not match its source pattern**. For example,
  `logs-tmpl-1m` is inside `logs-*`. So the rollup is named `anomaly-logs-tmpl-1m`.
- **A transform `terms` group-by drops documents with a missing key**. Two examples: no
  `service.name`, or a `log.template` longer than the keyword `ignore_above` of 1,024.
  `missing_bucket: true` keeps them.
- **The Filebeat variant (xxhash) and the ingest variant (MurmurHash3) produce different ids for
  the same template**. Pick one per cluster.

### ElastAlert2

- **ElastAlert2 `alertmanager_api_version` defaults to `v1`**. Current Alertmanager no longer
  serves `v1`. Set `v2`.
- **ElastAlert2 aggregation rules (`spike_aggregation`) query only in full `buffer_time`
  chunks**. This changes only if you set `use_run_every_query_size: true`.
- **`flatline` puts the silent key in `key`**, not in the `query_key` field. The silent key is
  the value that stopped logging, for example the service. Use
  `alertmanager_fields: {service: key}`. With `service.name`, all 5 test alerts had
  `service="null"`.
- **A compound `query_key` on `spike_aggregation` reaches the match only as one joined field**,
  for example `"service.name,log.template_id": "checkout,<id>"`. Split it in
  `alertmanager_labels` with Jinja, for example
  `"{{ _data['service.name,log.template_id'].split(',')[0] }}"`.
- **Set `query_key` on every rule with `realert`**. `realert` sets how long a rule waits before
  it alerts again. Without `query_key`, the first match silences the whole rule. A `new_term`
  rule without it sent 1 of 2 new (service, error type) pairs.
- **`use_keyword_postfix` defaults to true and appends `.keyword`**. On Elastic Common Schema
  (ECS) keyword fields, the `new_term` baseline then comes back empty. So every existing pair
  alerts as new. Set `use_keyword_postfix: false`.
- **Document-download rules stop at `max_query_size` × `max_scrolling_count` docs per run**
  (1,000 × 5 in the PoC). They never read the later docs in that run's window. A replay from an
  empty baseline missed the injected pair.
- **`new_term` seeds its baseline at start-up** with a scan of `terms_window_size`. Point it at
  the rollup index, not at raw indices. `terms_size` (default 50) caps `use_terms_query` and
  aggregation rules with `query_key`. The `new_term` baseline ignores it.

### Prometheus rules and promql-anomaly-detection

- **Upstream adaptive bands are onset detectors**: they catch the start of a change, not a
  lasting one. A sustained step raises the short-term stddev (standard deviation).
  - So the alert fires 7–10 min after onset and resolves 17–22 min in. We measured this for ×2,
    ×3 and ×10 steps and for a drop to 40 %.
  - A ×1.5 step never fires, because the band is at least ±50 % of the 1 h mean.
  - Sustained shifts are the job of SLO burn-rate alerts.
- **In promql-anomaly-detection v0.2.1, the sparse-series filter for `anomaly_type: requests`
  has no effect**. A catch-all `or` re-adds every tagged series. So `anomaly-inputs.yml` drops
  templates under 5 lines/min itself.
- **Series scraped back from the scorer must not carry `anomaly_name`**. Otherwise the upstream
  `select` rule fails on every evaluation (`vector cannot contain metrics with the same
  labelset`), and every band stops. The scorer drops `anomaly_*` input labels.
- **Detector-health alerts go quiet when the detector dies**.
  - `time() - anomaly_job_last_success_timestamp_seconds > 600` returns nothing once the
    scorer's series is gone. So `AnomalyJobStale` also checks `absent_over_time(…[10m])`.
  - `max(process_start_time_seconds)` for the novelty warm-up muted every exporter shard after
    one restart. It is matched per instance now.
- **Upstream `AnomalyDetected` has no `tier` label**. The LLM-triage route matches on
  `severity` and excludes `tier="meta"`. So these findings reach it.

### Drain

- **Drain merges look-alike lines**. At `sim_th` 0.4 in the Drain3 exporter, `Cache hit …` and
  `Cache miss …` share one template, and GET/POST access lines merge. `sim_th` is the
  similarity threshold: how alike two lines must be to share a template. Tune `sim_th`/`depth`
  per log family, or add masks.
