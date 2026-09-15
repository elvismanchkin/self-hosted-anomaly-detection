# Comparison matrix

**In short:**

- **Logs:** we adopt free add-ons on top of Elasticsearch Basic: ElastAlert2, a template key
  added at ingest, and Grafana dashboards. Elastic's own anomaly detection needs a paid license.
- **Metrics:** we adopt Grafana's `promql-anomaly-detection` rules, and Sloth or Pyrra for
  Service Level Objective (SLO) alerts. Our own statistical scorer follows in iteration 2.
- **Traces:** we adopt the OpenTelemetry `span_metrics` and `tail_sampling` components in
  front of Jaeger. Tail sampling also cuts storage cost.
- **Alerts:** Alertmanager stays the single alert bus. Triage with a large language model (LLM)
  comes later, with a cheap model and a hard spend cap.
- **Our own engine:** anomalyd is a pilot for iteration 2. It is the cheap way to get real Drain
  templates at 2 TB/day.
- **Cost:** commercial products for logs + metrics with anomaly detection cost from ≈ $2.3k to
  ≈ $609k per month at our volume. Our plan costs $0 in licenses, plus compute.

Terms and abbreviations: [glossary](glossary.md).

Sections:

- [How to read this page](#how-to-read-this-page)
- [Logs: which options work with Elasticsearch Basic?](#logs-which-options-work-with-elasticsearch-basic)
- [Metrics: which options work with Prometheus and Grafana OSS?](#metrics-which-options-work-with-prometheus-and-grafana-oss)
- [Traces: which options work with Jaeger?](#traces-which-options-work-with-jaeger)
- [Correlation, root cause analysis and LLMs](#correlation-root-cause-analysis-and-llms)
- [What do commercial products cost?](#what-do-commercial-products-cost)

## How to read this page

We want anomaly detection built from free parts where they fit, plus a small amount of our own
code. This page rates every option we looked at, for logs, metrics, traces and alert handling.

Every verdict follows from these limits:

- We run self-managed, free editions. For Grafana this is Grafana OSS, the open-source edition.
- We have 200 GB – 2 TB/day of logs and 1–10 M active series.
- Iteration 1 uses add-ons only. It does not swap any tool.
- LLM spend stays small.

We checked versions and dates on 2026-09-14. Sources are in the linked research files and in
[LINKS.md](../LINKS.md).

Verdicts:

- **Adopt**: use in iteration 1.
- **Pilot**: try on a subset first.
- **Later**: iteration 2–3.
- **No**: do not adopt for our stack.
- **Situational**: use only under the stated condition.

Table cells are short. The numbered notes under each table give the details. The license column
uses the standard short license names, for example Apache-2.0, MIT or AGPL-3.0. "PoC" means proof
of concept: code in this repo that we tested, but not in production.

Tiers T0–T3 are the layers of our [reference architecture](reference-architecture.md):

- T0: SLO rules. The only tier that pages a person.
- T1: cheap statistics on aggregated series.
- T2: a statistical scorer on a selected set of series.
- T3: LLM triage.

## Logs: which options work with Elasticsearch Basic?

| Option | License / cost | What it detects | Fit at 200 GB–2 TB/day | Add-on or swap | Version | Verdict |
|---|---|---|---|---|---|---|
| Elastic ML, AIOps, ES\|QL `CATEGORIZE` / `CHANGE_POINT` (note 1) | Platinum or higher | anomalies, log categories | native to Elasticsearch | license upgrade | Elasticsearch 9.5.3 (2026-09-03) | **No**: not free ([01 §2](research/01-logs-elastic-basic.md)) |
| Kibana alerting on Basic (note 2) | free (ELv2 Basic) | thresholds; week-over-week ratio | fine on a rollup index, not on raw indices | built in | Elasticsearch / Kibana 9.5.x | **Pilot**: alerts need a relay (note 2) |
| **ElastAlert2** (note 3) | Apache-2.0 | `new_term`, `spike`, `spike_aggregation`, `flatline`, `percentage_match`, `cardinality` | OK if every rule uses aggregations | add-on (one container) | 2.31.0 (2026-07-22) | **Adopt**: one instance plus a dead-man's switch ([01 §3](research/01-logs-elastic-basic.md)) |
| **Template key at ingest + transform rollup** (note 4) | free (Basic) | new templates; rate per template | CPU spread across hosts; regex cost to measure | add-on (config) | Filebeat ≥ 7.8; transforms on Basic | **Adopt**: validated on Elasticsearch 9.5.3 Basic ([poc](../poc/logs/elastic/)) |
| OpenTelemetry (OTel) Collector `drain` + `count` (note 5) | Apache-2.0 | real Drain templates, counted in Prometheus | ≈ 138k lines/s on one core (measured here) | add-on (agent fleet) | contrib v0.160.0 (2026-09-02); `drain` is alpha | **Pilot**: validated after 3 config fixes ([poc](../poc/otel/logs-drain-count.yaml)) |
| Drain3 exporter, this repo (note 6) | MIT library, stale (0.9.11, 2022-07-17) | Drain templates as Prometheus counters | 96–110k lines/s per core in the PoC benchmark (an upper bound) | add-on (needs a copy of the logs) | PoC | **Pilot**: best for offline template discovery on samples |
| anomalyd agent + server, this repo, Go (note 7) | our own code, one dependency | new templates; rate per template; volume per level | 1.3–1.5 M lines/s per core (an upper bound) | add-on (second reader next to Filebeat) | PoC; all tests pass | **Pilot** (iteration 2): the cheap way to get real Drain templates at 2 TB/day |
| Grafana OSS Elasticsearch data source + alerting (note 8) | AGPL-3.0 | week-over-week ratios | good on the rollup | add-on | Grafana v13.2.1; needs Elasticsearch ≥ 7.17 | **Adopt** for dashboards and seasonal ratios |
| OpenSearch Anomaly Detection (note 9) | Apache-2.0 | streaming Random Cut Forest, per entity | thousands of entities, not millions | **swap** | OpenSearch 3.8.0; plugin 3.8.0.0 | **Later**: only if the platform ever moves ([01 §7](research/01-logs-elastic-basic.md)) |
| Sentry self-hosted (note 10) | FSL-1.1 | error grouping | needs instrumentation in each app | parallel pipeline | 26.8.0 | **No** for iteration 1 |
| Salesforce LogAI (note 11) | BSD-3 | log anomaly detection toolkit | n/a | add-on | dead | **No** |
| LLM over raw logs (note 12) | per token | classifies every line | ≈ $375k/month at 2 TB/day | n/a | n/a | **No**: not feasible ([04 §2.5](research/04-aiops-rca-llm-cost.md)) |

Notes:

1. **Elastic's own anomaly detection.** This row covers three features:
   - machine learning (ML) anomaly detection jobs, with log categorization
   - AIOps, that is, AI for IT operations: log rate analysis, log pattern analysis and change
     points
   - the ES|QL commands `CATEGORIZE` and `CHANGE_POINT`. ES|QL is the Elasticsearch Query Language.

   All of them need Platinum or higher. Elastic sells self-managed Platinum only to existing
   customers. So a new buyer needs an Enterprise quote.
2. **Kibana alerting on Basic.** The free rule types are Elasticsearch query (ES|QL included),
   index threshold and log threshold. They give thresholds, and a week-over-week ratio through
   ES|QL. The license is the Basic tier of the Elastic License 2.0 (ELv2). Basic has only two
   connectors: Index and Server log. The Cases connector needs Platinum. So Kibana on Basic cannot
   send an alert out by itself, and alerts need a relay.
3. **ElastAlert2.** It fits our volume only when every rule uses aggregations. Each rule runs
   either on the rollup index or as a terms aggregation on the raw indices. It supports
   Elasticsearch 7, 8 and 9, and OpenSearch 1–3. A dead-man's switch is an alert that must always
   be firing. If it stops arriving, alerting is broken.
4. **Template key at ingest + transform rollup.** A template is a log line with its variable parts
   (numbers, IDs) masked. Filebeat masks them with `replace` and hashes the rest with
   `fingerprint`. An Elasticsearch ingest pipeline can do the same with `gsub` + `fingerprint`.
   The result is a `log.template_id` field. The key is stateless: each line gets its key alone,
   with no memory of earlier lines. An Elasticsearch transform then counts lines per template key
   per minute (the rollup). This gives novelty (new templates) and a rate per template.
   The CPU work is spread across the log hosts, but we still have to measure the regex cost.
   `replace` needs Filebeat 7.8 or newer.
5. **OTel Collector `drain` + `count`.** The Collector runs as a second log reader
   on each host. The `drain` processor finds real [Drain](https://jiemingzhu.github.io/pub/pjhe_icws2017.pdf)
   templates. Drain is a log template mining algorithm: it groups similar lines into one template.
   The `count` connector turns the templates into Prometheus counts.
   - Speed: it is written in Go and runs inline on each host. There is no published throughput
     figure. We measured ≈ 138k lines/s on one core, on a synthetic file, JSON parsing included.
     On container stdout with the `container` operator it did 92k lines/s.
   - Status: `drain` is alpha since v0.151.0. We validated our config on 40k container-stdout lines
     after 3 config fixes.
6. **Drain3 exporter (this repo, Python).** It turns Drain templates into Prometheus counters and
   caps the number of templates. The Drain3 library is MIT-licensed but stale: its last release is
   0.9.11 (2022-07-17).
   - Speed: 96–110k lines/s per core in the PoC benchmark, on synthetic lines with few
     templates. A separate side-by-side run with anomalyd gave 94–95k lines/s. Both are upper
     bounds. Drain on the [Loghub-2.0](https://github.com/logpai/loghub-2.0) datasets runs at
     7–10k lines/s per core. So shard by service.
7. **anomalyd agent + server (this repo, Go).** An agent on each host mines Drain templates. It
   sends only counts to a central server. The server reports three kinds of findings:
   - a new template
   - a change in the rate of one template
   - a change in log volume per level

   It compares each count with a seasonal baseline and sends findings straight to Alertmanager.
   The baseline is a [seasonal median](https://about.gitlab.com/blog/anomaly-detection-using-prometheus/):
   the median of the same time slot in earlier seasons (weeks for metrics, days for log counts).
   The spread is the
   [median absolute deviation (MAD)](https://en.wikipedia.org/wiki/Median_absolute_deviation).
   - Speed: measured on the same synthetic file as Drain3, so it is also an upper bound.
   - Size: the agent is ~7 MB. Only counts leave the host.
   - Code: our own, with one dependency (klauspost/compress).
   - Status: PoC. The Go tests, the parity test against Drain3 and the Docker end-to-end test
     pass.
8. **Grafana OSS Elasticsearch data source + alerting.** It runs date-histogram queries with
   relative time ranges, for example this week against last week.
9. **OpenSearch Anomaly Detection.** The plugin runs streaming
   [Random Cut Forest (RCF)](https://proceedings.mlr.press/v48/guha16.html) models, one detector
   per entity. Entity models live in the Java heap. So it handles thousands of entities, not
   millions. It needs a move from Elasticsearch to OpenSearch.
10. **Sentry self-hosted.** The license is the Functional Source License (FSL-1.1). It is not
    approved by the Open Source Initiative (OSI). The code becomes Apache-2.0 after 2 years.
    Sentry groups errors, but every app needs the Sentry SDK (software development kit).
11. **Salesforce LogAI** is dead: the repo returns 404, and the last release is 0.1.5
    (2023-03-02).
12. **LLM over raw logs.** Here an LLM would classify every log line, paid per token. 2 TB/day is
    ≈ 500 billion tokens/day. That costs ≈ $375k/month even at the cheapest batch price
    (gpt-5-nano).

## Metrics: which options work with Prometheus and Grafana OSS?

| Option | License / cost | What it detects | Fit at 1–10 M series | Add-on or swap | Version | Verdict |
|---|---|---|---|---|---|---|
| **grafana/promql-anomaly-detection** (note 13) | Apache-2.0 | values outside adaptive or robust bands | only on a tagged, pre-aggregated subset | add-on (rule files) | v0.2.1 (2025-10-20) | **Adopt**: validated end to end with promtool ([02 §1.1](research/02-metrics-prometheus.md)) |
| **Sloth / Pyrra** (note 14) | Apache-2.0 | SLO burn-rate alerts | tiny load | add-on | Sloth v0.16.0 (2026-04-04); Pyrra v0.10.1 (2026-06-25) | **Adopt**: Tier 0, the only tier that pages ([02 §8](research/02-metrics-prometheus.md)) |
| GitLab z-score and seasonal rules, hand-written (note 15) | free | z-score; 3-week seasonal median | needs ≥ 4 weeks of retention; heavy `[1w]` windows | add-on | pattern from Monitorama 2019 | **No**: the upstream framework covers it better ([02 §1.2](research/02-metrics-prometheus.md)) |
| Prometheus built-in functions (note 16) | Apache-2.0 | building blocks only | fine on aggregates | built in | Prometheus 3.14.0 (2026-08-18) | **Adopt** `predict_linear` for capacity (note 16) |
| Tier-2 scorer, this repo, Python (note 17) | this repo | robust z-score against past weeks | hundreds to thousands of curated series | add-on (one container) | PoC; selftest and end-to-end test pass | **Later** (iteration 2): start here, then upgrade to MSTL |
| anomalyd server, this repo, Go (note 18) | our own code | the Tier-2 score, on every input | 5k inputs ≈ 125 MB; full evaluation < 1 s of CPU | add-on (`remote_write` target) | PoC; tests pass with Prometheus 3.14.0 | **Pilot** (iteration 2): removes the scorer's `query_range` load |
| statsforecast MSTL + MAD residuals (note 19) | Apache-2.0 | residuals of a multi-seasonal forecast | 2–15 k series nightly on one virtual machine (estimate) | add-on | 2.1.1 (2026-07-16) | **Later**: the Tier-2 upgrade path ([02 §3](research/02-metrics-prometheus.md)) |
| river / PyOD (note 20) | BSD-3 / BSD-2 | streaming / multivariate outliers | per sample / per timestamp | add-on | river 0.26.1 (2026-08-21); PyOD 3.6.5 (2026-08-17) | **Later**: situational |
| Prophet (note 21) | MIT | trend, seasonality, holidays | hours for 5 k series | add-on | 1.4.0 (2026-08-15) | **No** at our scale (note 21) |
| Grafana ML, Sift, Adaptive Metrics (note 22) | Grafana Cloud only | forecasting, outlier detection | caps far below 1–10 M series | hosted-service swap | n/a (cloud service) | **No**: not in Grafana OSS ([02 §2](research/02-metrics-prometheus.md)) |
| VictoriaMetrics vmanomaly (note 23) | proprietary, Enterprise only | many models, including online ones | writes only to VictoriaMetrics | add-on plus a VictoriaMetrics store | license key since v1.5.0 | **No**: not free ([02 §4](research/02-metrics-prometheus.md)) |
| Netdata (note 24) | agent GPL-3.0+; UI closed | an anomaly bit per metric | 2,000 series per job by default | parallel agent | active | **No** as a central engine |
| Merlion / ADTK / Luminaire / Kats (note 25) | BSD-3 / MPL-2.0 / Apache-2.0 / MIT | various | n/a | add-on | stale or dead | **No** |
| Time-series foundation models (note 26) | Apache-2.0 weights | forecast residuals or direct anomaly scores | thousands of aggregates every 5 min on CPU (estimate) | add-on | chronos-forecasting 2.3.2 (2026-09-08) | **Later**, and only after a back-test ([05 §2](research/05-methods-benchmarks.md)) |

Notes:

13. **grafana/promql-anomaly-detection** is a set of Prometheus rule files. It builds two kinds of
    bands around each series:
    - adaptive bands: mean ± 2σ, where σ is the standard deviation. They use a
      [coefficient of variation](https://en.wikipedia.org/wiki/Coefficient_of_variation) (CoV)
      filter, 26 h smoothing and a look-back of 1 day or 1 week.
    - robust bands: median ± 2·MAD.

    Use it only on a tagged, pre-aggregated subset of series. Each adaptive input creates ~9
    derived series. Each robust input creates ~17. The last commit was on 2026-05-08.
14. **Sloth and Pyrra** generate Service Level Objective (SLO) alerts. The alerts watch the
    [error-budget burn rate](https://sre.google/workbook/alerting-on-slos/): how fast a service uses
    up the errors its SLO allows. Each alert checks several time windows and several burn rates
    (multi-window, multi-burn-rate). This is Tier 0.
15. **GitLab rules.** The pattern comes from a GitLab talk at Monitorama 2019
    ([blog post](https://about.gitlab.com/blog/anomaly-detection-using-prometheus/)). One rule
    computes a [z-score](https://en.wikipedia.org/wiki/Standard_score) against the mean and standard
    deviation of the last week. Another predicts the value from the median of the same time in the
    last 3 weeks (a seasonal median). The upstream framework is grafana/promql-anomaly-detection
    (note 13).
16. **Prometheus built-ins.** The useful functions are `predict_linear`, `quantile_over_time`, and
    the experimental `double_exponential_smoothing` and `mad_over_time`. None of them gives a
    seasonal [Holt-Winters](https://www.statsmodels.org/stable/generated/statsmodels.tsa.holtwinters.ExponentialSmoothing.html)
    forecast (a forecast with trend and seasonality). Use `predict_linear` for capacity alerts.
    Keep the experimental functions out of alerts that page.
17. **Tier-2 scorer.** It computes a robust z-score: the distance from the seasonal median,
    divided by the usual error (1.4826 × MAD of past residuals, never below a floor). The
    seasonal median uses the same time in previous weeks. Start Tier 2 with
    this scorer in iteration 2, then upgrade to MSTL (note 19).
18. **anomalyd server.** Prometheus sends it every input with `remote_write`, and a keep-filter
    decides which series go. The server uses the same seasonal median + MAD score as the Python
    scorer and matches it to 1e-9. It keeps the history in memory. It fills that history once from
    `query_range`.
    - The 125 MB figure is for 3 weeks of 5-minute history.
    - The Go tests and the Docker end-to-end test pass.
    - Gain: the Python scorer loads its history with `query_range` on every run. anomalyd does
      not.
19. **statsforecast MSTL.** [MSTL](https://arxiv.org/abs/2107.13462) splits a series into a trend
    and several seasonal parts, for example daily and weekly. The residual is what is left. We
    score the residual with MAD.
20. **river and PyOD.** river is a streaming library: it scores each sample when it arrives, for
    example with [Half-Space Trees](https://riverml.xyz/latest/api/anomaly/HalfSpaceTrees/). PyOD is
    a multivariate library: it scores each timestamp across several series. Its methods include
    [isolation forest](https://en.wikipedia.org/wiki/Isolation_forest) (IForest),
    [ECOD](https://arxiv.org/abs/2201.00382) and peer outliers.
21. **[Prophet](https://facebook.github.io/prophet/)** fits a forecast with trend, seasonality and
    holidays. Fitting one series takes seconds, so 5 k series take hours. It is fine for fewer than
    500 business metrics (key performance indicators, KPIs).
22. **Grafana ML (machine learning), Sift and Adaptive Metrics** exist only in Grafana Cloud, a
    hosted service. ML is included in the Cloud tiers. By default, each instance is capped at:
    - 10 forecasts × 100 series
    - 10 outlier detectors × 1,000 series

    Grafana raises the caps on request.
23. **vmanomaly** needs an Enterprise license. A 2-month trial exists. It reads from Prometheus,
    but it writes results only to VictoriaMetrics. So we would also have to run a VictoriaMetrics
    store.
24. **Netdata.** The agent is GPL-3.0+. The user interface (UI) is closed source. Its license is the
    Netdata Cloud UI License (NCUL1). Netdata Cloud is free for ≤ 5 nodes. The agent trains a
    [k-means](https://en.wikipedia.org/wiki/K-means_clustering) model per metric. It sets an
    "anomaly bit" when a value does not fit the model. The 2,000-series limit is the default of its
    Prometheus collector.
25. **Last releases:** Merlion 2.0.4 (2024-06), ADTK 0.6.2 (2020), Luminaire 0.4.3 (2024-01),
    Kats 0.2.0 (2022). ADTK is the Anomaly Detection Toolkit.
26. **Time-series foundation models** are large models pre-trained on many time series.
    - Candidates with Apache-2.0 weights: Chronos-2, TimesFM 2.5 weights, Toto 2.0 and TSPulse.
    - Excluded for their licenses: Moirai is CC-BY-NC-4.0, and the TimesFM 3.0 weights are
      non-commercial.
    - They score either by forecast residuals or with a model built for anomaly detection.
    - On the [TSB-AD](https://github.com/TheDatumOrg/TSB-AD) benchmark, forecasting foundation
      models score below simple methods. Two examples: Sub-PCA, based on principal component
      analysis, and POLY, a polynomial fit. So test them on our past data (a back-test) before any
      use.

## Traces: which options work with Jaeger?

| Option | License / cost | What it detects | Fit | Add-on or swap | Version | Verdict |
|---|---|---|---|---|---|---|
| **OTel `span_metrics` connector** (note 27) | Apache-2.0 | RED metrics from 100 % of spans | ≈ 80 series per operation per collector replica | add-on (collector tier) | contrib v0.160.0; alpha | **Adopt** ([03 §2](research/03-traces-jaeger.md)) |
| **OTel `tail_sampling` + `load_balancing`** (note 28) | Apache-2.0 | keeps errors, slow traces and a baseline | memory grows with spans/s × `decision_wait` | add-on (2 tiers) | beta / beta | **Adopt**: also a cost cut ([03 §3](research/03-traces-jaeger.md)) |
| Jaeger SPM, the Monitor tab (note 29) | Apache-2.0 | RED per operation, read from Prometheus | n/a | config | Jaeger v2.20.0 (2026-07-20); **v1 end-of-life since 2025-12-31** | **Adopt**; plan the v2 upgrade if still on v1 |
| OTel `service_graph` connector | Apache-2.0 | request and failure counts per service-to-service edge | needs trace-ID load balancing | add-on | alpha | **Pilot** |
| Elasticsearch aggregations on `jaeger-span-*` | free | duration and errors per operation | biased after sampling; nested tags unusable in Kibana Lens | built in | n/a | **Situational**: drill-down only |
| Grafana Jaeger data source | AGPL-3.0 | trace view | no metrics, no alerting | built in | Grafana v13.2.1 | **Situational**: visualisation only |
| CRISP from Uber (note 30) | Apache-2.0 | critical-path attribution | batch | add-on | active | **Later**, for root cause analysis |
| Jaeger v2 MCP tools (note 31) | Apache-2.0 | trace evidence for LLM bundles | on demand | Jaeger v2 only | v2.20.0 | **Later**, with LLM triage |
| Research methods for trace structure: TraceAnomaly, TraceVAE | unlicensed / stale | structural anomalies | n/a | n/a | research only | **No** |

Notes:

27. **OTel `span_metrics` connector.** It turns every span into
    [RED metrics](https://grafana.com/blog/2018/08/02/the-red-method-how-to-instrument-your-services/):
    Rate, Errors and Duration. It sees 100 % of spans. The metric bands and SLO alerts then run on
    these metrics. See the
    [connector README](https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/v0.160.0/connector/spanmetricsconnector/README.md).
    - The ≈ 80 series per operation and replica assume the default buckets.
    - It also multiplies by the number of app pods, unless you set
      `resource_metrics_key_attributes: [service.name]`.
    - Limit the count with `aggregation_cardinality_limit`.
28. **OTel `tail_sampling` + `load_balancing`.**
    [Tail sampling](https://opentelemetry.io/docs/concepts/sampling/) decides whether to keep a trace
    after the trace is complete. We keep errors, slow traces and a baseline sample. So
    Elasticsearch stores ~95–97 % fewer spans. `load_balancing` sends all spans of one trace to the
    same collector, so the setup needs 2 collector tiers.
29. **Jaeger SPM.** Service Performance Monitoring (SPM) is the Monitor tab in the Jaeger user
    interface. It shows RED metrics per operation, read from Prometheus. It needs only
    configuration.
30. **[CRISP](https://github.com/uber-research/CRISP)** finds the critical path of a trace: the
    chain of spans that sets the total duration. It runs in batch, not live.
31. **Jaeger v2 MCP tools.** Jaeger v2 has a Model Context Protocol (MCP) server. An LLM can call
    its tools, for example `get_critical_path` and `get_trace_errors`. We would use them to add
    trace evidence to an LLM bundle. A bundle is the alert context that we send to the model.

## Correlation, root cause analysis and LLMs

| Option | License / cost | What it does | Free self-hosted? | Add-on or swap | Version | Verdict |
|---|---|---|---|---|---|---|
| **Alertmanager** | Apache-2.0 | grouping, inhibition, silences, routing | yes | already present | v0.34.0 (2026-08-16) | **Adopt**: the single alert bus ([poc](../poc/prometheus/alertmanager.yml)) |
| Keep OSS | MIT | deduplication, rule and topology correlation, incidents | yes, except AI correlation (Cloud/Enterprise) | add-on | v0.54.3 (2026-09-09) | **Pilot** (iteration 2, optional) |
| Grafana OnCall OSS | n/a | on-call | n/a | n/a | **archived 2026-03-24** | **No** |
| HolmesGPT (note 32) | Apache-2.0, CNCF Sandbox | on-demand LLM investigation; no Jaeger toolset | yes (bring your own LLM) | add-on | 0.41.0 (2026-09-08) | **Later**: started by a person, ≤ 20/day |
| Coroot CE (note 33) | Apache-2.0 | eBPF-based APM; SLO-based root cause analysis | partly | parallel stack (own ClickHouse) | v1.26.0 (2026-09-07) | **Pilot** on a subset only |
| k8sgpt / Robusta | Apache-2.0 / MIT | Kubernetes diagnosis / alert enrichment | yes | add-on (Kubernetes) | k8sgpt v0.4.39; Robusta 0.49.0 | **Situational**: only if on Kubernetes |
| SigNoz / OpenObserve | open core | full backends; anomaly alerts only in paid editions | no | **swap** | v0.141.1 / v1.0.0 | **No** |
| LLM triage via API, one bundle per Alertmanager group | $1–$780/month, by model and volume | summary and hypotheses | n/a (paid API) | add-on (small service) | n/a | **Later**: cheap model, hard caps ([04 §2](research/04-aiops-rca-llm-cost.md)) |
| Self-hosted 7–14B model on 1 GPU (note 34) | ~$290–$588/month | one-shot bundle summaries; weak at agentic tool use | yes | add-on | n/a | **Situational** (note 34) |

Notes:

32. **HolmesGPT** is a Cloud Native Computing Foundation (CNCF) Sandbox project. An LLM agent
    investigates an alert on demand. It has toolsets for Prometheus, Elasticsearch and Grafana, but
    none for Jaeger. You bring your own LLM.
33. **Coroot Community Edition (CE)** is a tool for application performance monitoring, or APM. It
    collects data with [eBPF](https://ebpf.io/what-is-ebpf/): small programs that run inside the
    Linux kernel. It finds root causes based on SLOs. The AI-based root cause analysis is only in
    the Enterprise edition. Coroot runs as a parallel stack with its own ClickHouse database.
34. **Self-hosted model.** A model with 7–14 billion parameters runs on one graphics processing
    unit (GPU). The cost assumes an Amazon Web Services (AWS) g6.xlarge for 12–24 h/day. The model can summarise a
    bundle in one call. It is weak at agentic tool use, that is, when the model calls tools in a
    loop by itself. Use it only in one of two cases:
    - the data must not leave the network
    - API spend goes above ~$600/month

## What do commercial products cost?

These are list prices, to show the scale. We compare two sizes:

- **A**: 200 GB/day of logs and 1 M series.
- **B**: 2 TB/day of logs and 10 M series.

We re-checked list prices on 2026-09-15. The totals exclude support, egress and discounts.
[cost_model.py](cost_model.py) computes every figure.
[cost-sizing.md §8](cost-sizing.md#8-prices-formulas-and-sources) lists the formulas, the unit
prices and their source links.

Two vendors have a price range:

- **Grafana:** its calculator also bills logs retention on every GB. But its billing doc says
  30 days are included.
- **Elastic:** it bills an enriched size, not the raw size. Its estimator puts that at ≈ 1.66× raw.

| Vendor | A (per month) | B (per month) | Anomaly detection included? |
|---|---|---|---|
| Grafana Cloud Pro, 60 s scrape | ≈ $8.1k–$8.7k | ≈ $79k–$85k | ML included but capped (note 22) |
| Grafana Cloud Pro, 15 s scrape | ≈ $24.6k–$25.2k | ≈ $244k–$250k | same |
| Datadog (ingest + 15-day index + custom metrics + 100 hosts) | ≈ $62k | ≈ $609k | Watchdog built in |
| Elastic Cloud Serverless Complete (graduated tiers) | ≈ $2.3k–$3.5k | ≈ $10.6k–$17.4k | ML in Complete |
| Elastic self-managed Platinum / Enterprise | quote only | quote only | yes |
| **Our open-source add-on plan** (note 35) | $0 licenses + compute + LLM ≤ $100/month cap | same, with more collector and scorer compute | tiers T0–T2 self-built; T3 budget-capped |

Notes:

35. **Our plan.** [cost-sizing.md](cost-sizing.md) sizes the compute. We build tiers T0–T2
    ourselves. T3 (LLM triage) has a hard budget cap.
