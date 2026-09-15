# Roadmap

**In short:**

- **Step 0 (half a day):** check what runs today. The plan depends on the Elasticsearch license
  and on the versions.
- **Iteration 1 (weeks 1–4):** add free components only. Nothing is swapped. Only Service Level
  Objective (SLO) burn-rate alerts page a person.
- **Any time:** upgrade Jaeger v1 to v2 if we still run v1. v1 is end-of-life. This is an
  upgrade, not a swap.
- **Iteration 2 (months 2–3):** add a second-stage scorer, a log-mining pilot, and large language
  model (LLM) triage with a $100/month cap.
- **Iteration 3 (quarter 2+):** swap components or add heavier models. We do this only if the
  numbers from iterations 1–2 justify it.
- Every step names the proof-of-concept (PoC) file it starts from and the evidence that ends it.

Terms and abbreviations: [glossary](glossary.md).

Each step answers three questions:

- **What we do.**
- **Why** we do it.
- **Done when:** how we know that it worked.

The tiers T0–T3 are the detection tiers from the
[reference architecture](reference-architecture.md#scope): T0 = SLO alerts (the only tier that
pages), T1 = cheap statistics, T2 = a second-stage scorer, T3 = LLM summaries.

Sections:
[Step 0](#step-0-confirm-what-is-actually-running-half-a-day) ·
[Iteration 1](#iteration-1-add-ons-only-weeks-14) ·
[Jaeger v2 upgrade](#any-time-the-jaeger-v2-upgrade) ·
[Iteration 2](#iteration-2-second-stage-confirmation-and-llm-triage-months-23) ·
[Iteration 3](#iteration-3-optional-swaps-and-heavier-models-quarter-2) ·
[Branch for the legacy OSS 7.10 build](#branch-elasticsearch-is-the-legacy-oss-710-build)

## Step 0: confirm what is actually running (half a day)

**What we do:** read the version, license and features of each tool, and collect an inventory.

**Why:** the plan depends on these facts. The free default distribution of Elasticsearch has
transforms and ES|QL (the Elasticsearch query language), but no machine learning. A legacy
open-source (OSS) 7.10 build has none of these. It follows the
[branch at the end of this page](#branch-elasticsearch-is-the-legacy-oss-710-build).

**Done when:** we know the versions and the license, and the inventory below is complete.

**Elasticsearch / Kibana.** Set `ES` first, for example
`export ES=http://elasticsearch.internal:9200`. Add credentials if security is on.
```bash
curl -s "$ES/"
```
```bash
curl -s "$ES/_license"
```
```bash
curl -s "$ES/_xpack?categories=features"
```
Expected output on the free default distribution:

- `version.build_flavor: "default"`
- `license.type: "basic"`, with no expiry date
- in `_xpack`: `ml.available: false`, `watcher.available: false`, `transform.available: true`,
  `esql.available: true`

We confirmed this output on a local Elasticsearch 9.5.3 container. A legacy Apache-2.0 7.10 build
reports `build_flavor: "oss"` and has no `_license` endpoint. For that case, see the branch at the
end of this page.

**Prometheus, Grafana, Jaeger:**
```bash
curl -s http://prometheus:9090/api/v1/status/buildinfo
```
```bash
curl -s http://grafana:3000/api/health
```
```bash
curl -s http://prometheus:9090/api/v1/status/tsdb | head -c 600
```
Also check the Jaeger collector version, with `--version` on the binary or from the container
image tag. Jaeger v1 reached end-of-life on 2025-12-31.

**Inventory to collect:**

- services and owners
- log GB/day per service
- active series and the top cardinality sources (`/api/v1/status/tsdb`)
- existing SLOs
- the Filebeat version (the `replace` processor needs ≥ 7.8)
- whether the apps emit OTLP (the OpenTelemetry Protocol) or use Jaeger SDKs, that is, Jaeger
  client libraries

## Iteration 1: add-ons only (weeks 1–4)

In this iteration we only add components. We swap nothing.

### 1. Make Alertmanager the single alert bus

- **What we do:** route alerts by `severity` and group them by `service`. Inhibit (mute)
  findings while a page fires.
- **Why:** then every alert source uses the same grouping and muting rules.
- **Starts from:** [alertmanager.yml](../poc/prometheus/alertmanager.yml)
- **Done when:** `amtool config routes test` passes for page, finding and ElastAlert cases.

### 2. Tier 0: SLO burn-rate alerts

- **What we do:** add SLO burn-rate alerts for the top 10–20 user-facing services. Sloth or Pyrra
  generate the rules.
- **Why:** T0 is the only tier that pages. A
  [burn-rate alert](https://sre.google/workbook/alerting-on-slos/) fires when a service uses up
  its error budget too fast.
- **Starts from:** [anomaly-alerts.yml](../poc/prometheus/anomaly-alerts.yml)
- **Done when:** every paging alert is an SLO burn alert, and the old static pagers are reviewed.

### 3. Traces: OTel tier 1 + tier 2 in front of Jaeger

- **What we do:** put two layers of OpenTelemetry (OTel) Collectors in front of Jaeger. They
  compute span_metrics on 100 % of spans, then apply
  [tail sampling](https://opentelemetry.io/docs/concepts/sampling/).
- **Why:** the
  [RED metrics](https://grafana.com/blog/2018/08/02/the-red-method-how-to-instrument-your-services/)
  (rate, errors, duration) stay exact, and Elasticsearch stores far fewer spans.
- **Starts from:** [otel/](../poc/otel/)
- **Done when:**
  - the Jaeger Monitor tab shows RED metrics
  - span ingest into Elasticsearch drops ~95 %
  - the `traces_span_metrics_*` series stay under the cardinality limit

### 4. Logs path A: template key in Filebeat

- **What we do:** add the template key in Filebeat. Start on a canary host, that is, one host that
  gets the change first. Then roll it out to the whole fleet. A continuous transform counts lines
  into `anomaly-logs-tmpl-1m`.
- **Why:** Elastic's own log anomaly detection is not free. Path A gives us counts per template
  without a new data path.
- **Starts from:** [logs/elastic/](../poc/logs/elastic/)
- **Done when:**
  - the Filebeat CPU delta is measured on the canary
  - the transform health is green
  - the template count per service is stable after a week

### 5. ElastAlert2 rules for logs

- **What we do:** run one ElastAlert2 instance plus a dead-man's switch. A dead-man's switch is an
  alert that must keep firing. If it stops, the alerting itself is broken. The rules are
  `new-log-template`, `error-template-spike`, `log-source-flatline` and `error-ratio-per-service`.
  All of them send to Alertmanager.
- **Why:** Kibana on the Basic license cannot send webhooks. ElastAlert2 brings the log alerts to
  Alertmanager.
- **Starts from:** [elastalert2/](../poc/logs/elastalert2/)
- **Done when:**
  - no new documents appear in the `elastalert_status_error` index. When ElastAlert2 runs with
    `--prometheus_port`, the check is that `elastalert_errors` does not increase.
  - a heartbeat rule (not in the PoC yet) keeps the dead-man's switch alive.
  - findings arrive in chat, grouped per service.

### 6. Metric bands

- **What we do:**
  - tag 3–5 k service-level inputs, that is, give them the labels that the band rules look for
  - load the upstream adaptive and robust band rules
  - optionally add the 90-day anomaly-detection Prometheus
- **Why:** plain PromQL (the Prometheus query language) is the best free metric detector. It must
  run only on service-level series.
- **Starts from:** [anomaly-inputs.yml](../poc/prometheus/anomaly-inputs.yml)
- **Done when:**
  - `promtool test rules` passes in continuous integration (CI)
  - 7 days of warm-up are done
  - per-rule notification counts are reviewed weekly

### Exit criteria for iteration 1

- Zero anomaly findings page anyone. Only Tier 0 pages.
- Every T1 rule has a measured precision (actionable / total) from a weekly review. We tune or
  delete rules below ~30 %. This follows the Huawei alert-quality idea in
  [research/05 §5](research/05-methods-benchmarks.md).
- An incident log with timestamps exists. We need it to evaluate iteration 2.

## Any time: the Jaeger v2 upgrade

**What we do:** upgrade Jaeger from v1 to v2, if we still run v1. This is an upgrade of a
component we already run, not a swap. So it can happen at any time, in any iteration.

**Why:** Jaeger v1 reached end-of-life on 2025-12-31
([research/03](research/03-traces-jaeger.md)).

**What to keep:**

- Jaeger v2 bundles `tail_sampling` and `spanmetrics`, but not `transform` or `service_graph`.
- So keep otelcol-contrib, the OTel Collector build with extra components, for OTel tier 2.
  We can drop it only if we stop using those two.

## Iteration 2: second-stage confirmation and LLM triage (months 2–3)

In this iteration we add checks and summaries on top of the iteration-1 findings.

### 1. Tier-2 scorer

- **What we do:** run the seasonal scorer on the top-K series. We choose these from the T1 series
  that proved useful.
  - The scorer compares each value with the seasonal median, that is, the median of the same time
    slot in past seasons. It divides the difference by the usual error: 1.4826 × the
    [median absolute deviation (MAD)](https://en.wikipedia.org/wiki/Median_absolute_deviation) of
    past residuals, never below a floor.
  - If the seasonal median is too crude, we upgrade the method to statsforecast
    [MSTL](https://arxiv.org/abs/2107.13462) (a decomposition with several seasons).
  - anomalyd, our own small Go engine, runs the same score on every remote-written input. It needs
    no per-run `query_range`.
- **Why:** a second stage confirms T1 findings before anyone acts on them. Uber and Netflix page
  on anomalies only after a confirmation stage or a health model
  ([research/05 §5](research/05-methods-benchmarks.md)).
- **Starts from:** [prom_anomaly_job.py](../poc/ml/prom_anomaly_job.py),
  [anomalyd](../anomalyd/README.md)
- **Done when:** on the incident log, event-level precision/recall beats T1 alone.
  - Precision is the share of findings that are real. Recall is the share of incidents that we
    catch.
  - Event-level means that we count whole incidents, not single time points.
  - We never use point-adjusted F1 (a score that combines precision and recall). It can make even
    random scores look good ([Kim et al.](https://arxiv.org/abs/2109.05257)).

### 2. Logs path B or C pilot

- **What we do:** run one of these pilots, then compare its templates with the regex key:
  - OTel `drain` + `count` on a few hosts
  - `drain3_exporter.py`, fed with a copy of the log stream
  - the anomalyd agent + server (Go)
- **Why:** regex masking is coarser than [Drain](https://github.com/logpai/Drain3) clustering.
  Drain is a log template mining algorithm, and it handles free-text words
  ([reference architecture](reference-architecture.md#three-ways-to-turn-logs-into-counts)).
- **Starts from:** [logs-drain-count.yaml](../poc/otel/logs-drain-count.yaml),
  [anomalyd](../anomalyd/README.md)
- **Done when:**
  - we decide whether Drain templates catch incidents that the regex key misses.
  - for anomalyd: agent CPU at the host's p95 log rate stays under 2 % of a core. The p95 log
    rate is the rate that the host stays under 95 % of the time.

### 3. LLM triage webhook

- **What we do:** send one bundle per Alertmanager group to a cheap default model. A bundle is
  the context of one alert group. We set a $100/month hard cap and redact the data.
- **Why:** an LLM is affordable only on a few grouped bundles, not on raw logs
  ([README, finding 5](../README.md#findings)).
- **Starts from:** the design in [research/04 §2](research/04-aiops-rca-llm-cost.md) and the
  route in [alertmanager.yml](../poc/prometheus/alertmanager.yml)
- **Done when:** on-call rates the summaries useful in ≥ 50 % of sampled incidents, and spend
  stays under the cap.

### 4. HolmesGPT on demand

- **What we do:** run HolmesGPT, an open-source LLM investigation agent. A human clicks
  "investigate". We allow ≤ 20 runs/day, and each run has a step limit.
- **Why:** an agent makes many LLM calls per investigation. So it runs only when a person asks.
- **Starts from:** [research/04 §1.4](research/04-aiops-rca-llm-cost.md)
- **Done when:** it is used in real incidents, and we track the cost per investigation.

### 5. Optional: Keep OSS

- **What we do:** add Keep OSS, an open-source alert-correlation tool, for incidents that span
  several sources.
- **Why:** only if Alertmanager grouping is not enough.
- **Starts from:** [research/04 §1.2](research/04-aiops-rca-llm-cost.md)
- **Done when:** we get fewer duplicate notifications per incident.

## Iteration 3: optional swaps and heavier models (quarter 2+)

We do these only with evidence from iterations 1–2. Each item says when it makes sense.

- **Kafka as a copy point for logs** (Filebeat → Kafka → Elasticsearch sink + template miners),
  if several consumers need the raw stream.
- **Long-term metrics store**, if seasonal baselines need more than 90 days. A Thanos sidecar is
  additive. Mimir or VictoriaMetrics are swaps.
- **OpenSearch**, only if log anomaly detection on the rollup (the per-minute template counts)
  proves insufficient and we plan a platform move anyway. OpenSearch offers anomaly detection with
  [Random Cut Forest](https://proceedings.mlr.press/v48/guha16.html) and
  [Brain patterns](https://docs.opensearch.org/latest/sql-and-ppl/ppl/commands/patterns/).
- **Time-series foundation models** for forecasting and for cold-start series, that is, new series
  with little history. A foundation model is a large model pretrained on many datasets.
  - Candidates: Chronos-2, TimesFM 2.5 weights, Toto 2.0.
  - Do it only if a back-test on the incident log shows that they do better than T1/T2.
  - Weight licences rule out Moirai and the TimesFM 3.0 weights for commercial use.

## Branch: Elasticsearch is the legacy OSS 7.10 build

**What changes:**

- This build has no `_license`, no Kibana alerting, no transforms and no ES|QL.
- The Grafana Elasticsearch data source needs Elasticsearch ≥ 7.17.
- The cluster is also far past end-of-life.

**What we do instead in iteration 1:** move the log counts **outside** Elasticsearch.

- Use OTel `drain` + `count` (path B), or ElastAlert2 aggregations pushed to Alertmanager.
- The PoC Filebeat processors (`copy_fields` + `replace` + `fingerprint`) work on Filebeat 7.10.
  `replace` exists since 7.8.0.
- The Elasticsearch ingest-pipeline variant does not load on 7.10. `set` `copy_from` needs 7.11,
  and ingest `fingerprint` needs 7.12.

**Plan the upgrade early.** Upgrading to the default distribution (8.19 / 9.x) with the free
Basic license is itself free. Details: [research/01 §8](research/01-logs-elastic-basic.md).
