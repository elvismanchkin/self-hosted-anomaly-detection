# Metrics, logs and traces anomaly detection on a free, self-hosted stack

**In short:**

- We want **our own anomaly-detection engine for our stack**, not a vendor product. We build it
  from open-source parts where they fit, plus a small amount of our own code.
- Iteration 1 only adds free components. Nothing in our stack is replaced.
- Only Service Level Objective (SLO)
  [burn-rate alerts](https://sre.google/workbook/alerting-on-slos/) page a person. Anomaly
  findings go to chat or tickets.
- Our "model" is simple statistics: a [seasonal median](anomalyd/README.md) with the
  [median absolute deviation (MAD)](https://en.wikipedia.org/wiki/Median_absolute_deviation).
  Public benchmarks show that simple statistics do as well as most deep-learning and foundation
  models, or better.
- Our own small Go engine, `anomalyd`, keeps up with our scale on synthetic data. It is a pilot
  for iteration 2.
- A large language model (LLM) only summarises alert groups, and it has a budget cap.

Terms and abbreviations: [glossary](docs/glossary.md).

Sections:
[What we want](#what-we-want) ·
[Four questions, short answers](#four-questions-short-answers) ·
[Findings](#findings) ·
[Recommended plan](#recommended-plan) ·
[Our own engine: anomalyd](#our-own-engine-anomalyd-go) ·
[Contents](#contents) ·
[Verification status](#verification-status)

## What we want

We want to detect anomalies in our logs, metrics and traces. We do not want to buy licences or
replace parts of our stack. So we build **our own engine, made for our needs**. It uses
open-source parts where they fit, plus a small amount of our own code. This repo holds the
research, the comparisons and a validated proof of concept (PoC).

Our stack stays as it is:

- **Logs:** apps log to stdout. Filebeat reads the container runtime's files
  (`/var/log/containers`) and sends the lines to Elasticsearch + Kibana. Both are self-managed and
  use the free Basic license.
- **Metrics:** Prometheus + Grafana OSS (the free open-source edition) + Alertmanager.
- **Traces:** Jaeger.
- **Scale:** 200 GB – 2 TB of logs/day, 1–10 M active Prometheus series.
- **Limits:** iteration 1 only adds components. LLMs are allowed, but cost matters.

Research date: 2026-09-14. We checked versions, licences and prices against primary sources on
that date. We checked prices again on 2026-09-15. Anything we could not confirm is marked
UNVERIFIED in the research files.

Interactive summary report: https://elvismanchkin.github.io/self-hosted-anomaly-detection/

## Four questions, short answers

**1. What do the tools we already run give us for free?**
Little for logs, a lot for metrics. Elastic's log anomaly detection needs a paid license, and
Kibana on Basic cannot even send a webhook. Prometheus can compute seasonal bands in plain
PromQL (the Prometheus query language). Alertmanager can group and route all findings. Details: [Findings 1–3](#findings).

**2. Which open-source parts can we reuse?**
Most of the engine:

- Filebeat processors and an Elasticsearch transform turn log lines into counts per template.
- ElastAlert2 runs alert rules on those counts.
- `grafana/promql-anomaly-detection` gives seasonal bands for metrics.
- Sloth or Pyrra generate the SLO burn-rate rules.
- The OpenTelemetry Collector turns traces into metrics and samples them.

**3. What do we have to write ourselves?**
Configs and rules for the parts above, plus a small scorer. For each important series, the scorer
takes the median of the same time slot in past seasons (for example, past weeks). Then it
measures how far the current value is from that median, compared with the usual spread (MAD). It
exists as a Python PoC and as a Go port in `anomalyd`. This is our "own model": simple
statistics, not a neural network. [Finding 4](#findings) explains why. The LLM summary step is
also ours, but it exists only as a design so far.

**4. Does a small engine of our own (`anomalyd`) work at our scale?**
On synthetic data, yes. One core mines 1.3–1.5 M log lines/s. The server holds 5 k metric and
30 k log series in 291 MB, and we plan 3–5 k service-level metric inputs. We have not tested it
on our real logs yet. So it is a pilot for iteration 2, not part of iteration 1. Details:
[Our own engine: anomalyd](#our-own-engine-anomalyd-go).

## Findings

1. **Elastic's own log anomaly detection is not free.**
   - These features all need a Platinum or higher license:
     - machine-learning (ML) jobs and log categorization
     - the AIOps (AI for IT operations) tools
     - the commands `CATEGORIZE` and `CHANGE_POINT` in ES|QL, the Elasticsearch query language
   - Kibana on Basic can alert, but only through the Index and Server log connectors. The Cases
     connector needs Platinum. So Kibana on Basic cannot even send a webhook.

   So log anomaly detection must be an add-on. We use three free parts:

   - Filebeat gives each log line a **template key**. A template is the fixed text of a log line.
     Filebeat masks the variable parts (numbers, IDs) with `replace` and hashes the rest with
     `fingerprint`. The key is stateless: it is the same on every host and after every restart.
   - An Elasticsearch **transform** counts lines per template key per minute. Transforms are free
     on the Basic license.
   - **ElastAlert2** runs alert rules on those counts.
2. **The best free metric detector is plain PromQL.**
   - `grafana/promql-anomaly-detection` computes
     [adaptive and robust seasonal bands](https://grafana.com/blog/2024/10/03/how-to-use-prometheus-to-efficiently-detect-anomalies-at-scale/).
     A band is the normal range of a series, learned from its own history.
   - The bands are recording rules: queries that Prometheus runs on a schedule and stores as new
     series. They need no plugin and no feature flag.
   - Run them only on 3–5 k service-level series, never on the raw 1–10 M series.
   - Grafana OSS has no machine learning. Grafana ML and Sift exist only in Grafana Cloud.
3. **Treat traces as metrics.**
   - We put a tier of OpenTelemetry (OTel) Collectors in front of Jaeger.
   - The collectors compute
     [RED metrics](https://grafana.com/blog/2018/08/02/the-red-method-how-to-instrument-your-services/)
     (rate, errors, duration) from 100 % of spans. A span is one timed operation inside a trace.
   - Then they apply [tail sampling](https://opentelemetry.io/docs/concepts/sampling/). They look
     at each whole trace and keep errors, slow traces and 2 % of the rest.
   - The RED metrics feed the same bands and SLO rules as all other metrics.
   - Elasticsearch stores about 95 % fewer spans.
   - Jaeger v1 has been end-of-life since 2025-12-31.
4. **Simple statistics beat clever models at this scale.** So our "model" is simple statistics,
   not a neural network. The evidence:
   - **Time series.** [TSB-AD](https://github.com/TheDatumOrg/TSB-AD) is a public benchmark for
     time-series anomaly detection. Simple statistical methods top its univariate ranking (one
     series at a time).
     - Forecasting foundation models (TimesFM, Chronos) and most deep-learning detectors score
       lower. A foundation model is a large model pretrained on many datasets.
     - The best general foundation model (MOMENT) only ties the second-tier statistical methods.
     - Only models submitted by their own authors beat the statistical methods. Most of them are
       pretrained for anomaly detection. Nobody has reproduced these results independently.
   - **Logs.** On the [public labelled datasets](https://github.com/logpai/loghub), detectors of
     new templates and of count changes are competitive with deep learning.
   - **Paging.** No large operator in our research pages a person on raw per-series anomalies.
     Uber and Netflix page on anomaly output, but only after a confirmation stage or a health
     model.

   So **SLO burn-rate alerts page a person. Anomaly findings go to chat or tickets.**
5. **An LLM fits only at the end of the chain.**
   - Summarising one bundle per Alertmanager group costs **$1–$780/month**, depending on model and
     volume. A bundle holds the context of one alert group: alert labels, related series, log
     template changes, trace IDs and recent deploys.
   - Sending raw logs through an LLM would cost about **$375k/month** at 2 TB/day, even at the
     cheapest batch price. At the Gemini 2.5 Flash-Lite batch price it would cost about $750k/month.
   - Managed logs + metrics with anomaly detection cost about $8.1k–$609k/month at list price, on
     Grafana Cloud Pro and Datadog.
   - Elastic Serverless Complete costs about $2.3k–$17k/month. Its prices come in graduated
     tiers: the "as low as" rate applies only above 150,000 GB/month. It also means moving off
     self-managed Elasticsearch.
   - [docs/cost_model.py](docs/cost_model.py) recomputes every figure from linked list prices. The
     formulas are in [docs/cost-sizing.md §8](docs/cost-sizing.md#8-prices-formulas-and-sources).

## Recommended plan

We detect in four tiers. Each tier is a group of detectors with its own cost and its own alert
route. Cheap detectors run on all aggregates. Expensive ones run only on a few candidates.

| Tier | What runs | Pages a person? |
|---|---|---|
| T0 | SLO multi-window burn-rate alerts, generated by Sloth or Pyrra (note 1) | **yes, the only tier that pages** |
| T1 | Cheap statistics on aggregated metrics and logs (note 2) | no: chat/tickets, grouped per service |
| T2 | Seasonal-median + MAD scorer on the top-K series (note 3) | no: second-stage confirmation |
| T3 | LLM bundle summaries, on demand, capped at ≤ $100/month | no |

Notes:

1. An SLO is a reliability target for a service. A
   [burn-rate alert](https://sre.google/workbook/alerting-on-slos/) fires when the service uses up
   its error budget too fast. Multi-window alerts check a long and a short time window.
2. T1 has these parts:
   - PromQL adaptive and robust bands on service-level series
   - ElastAlert2 new-template and template-spike rules on the log rollup (the per-minute counts
     from the transform)
   - ElastAlert2 flatline and error-ratio rules, as terms aggregations on the raw log indices
   - a log-novelty alert
3. The top-K series are a short list chosen from the T1 series that proved useful. The scorer
   exists as a Python PoC and as a Go port in anomalyd. It can be upgraded to
   [MSTL](https://arxiv.org/abs/2107.13462), a decomposition with several seasons.

Full plan with exit criteria: [docs/roadmap.md](docs/roadmap.md).

## Our own engine: anomalyd (Go)

[anomalyd/](anomalyd/README.md) is our own small detection engine. It is a second, separate PoC.
It is one static Go binary of 7 MB, with one third-party dependency. It does the T1/T2 detection
work of the Python pieces, but:

- it uses far less CPU
- it does not re-query Prometheus
- it does not add template series to Prometheus

It has two roles:

- **`anomalyd agent`** reads the same files that Filebeat reads. Filebeat stays unchanged.
  - For container stdout these are the runtime's files under `/var/log/containers`.
  - The agent removes the log envelope of the Container Runtime Interface (CRI) or of Docker.
    The envelope is the prefix that the runtime adds to each line.
  - It runs as a DaemonSet next to Filebeat, so one copy runs on every node
    ([agent-daemonset.yaml](anomalyd/deploy/agent-daemonset.yaml)).
  - It mines templates on the node with [Drain](https://github.com/logpai/Drain3), a log
    template mining algorithm. Drain3 is its Python implementation.
  - It sends only the line count per template: 68 KB for 160 MB of logs in the end-to-end test.
- **`anomalyd server`** does the rest:
  - receives the anomaly-input recording rules by `remote_write`, and loads their history from
    Prometheus once (backfill)
  - scores every series against a seasonal median + MAD baseline
  - detects new log templates
  - sends findings with `tier` labels to Alertmanager

Measured on synthetic data (details and commands in [anomalyd/README.md](anomalyd/README.md)):

| Measure | anomalyd (Go) | Python PoC |
|---|---|---|
| Log mining on one core, same 300k-line file | 1.3–1.5 M lines/s | 94–95 k lines/s (Drain3 exporter) |
| Seasonal score per series (1-week season, 5 min step) | 9 µs with a cached scale, 110 µs full | 1.1–1.6 ms, plus a `query_range` per run |
| Server memory for 5 k metric + 30 k log series | 291 MB | n/a (stateless, re-queries Prometheus) |

The Drain3 figure comes from the side-by-side run with anomalyd. The PoC's own Drain3 benchmark
gave 96k–110k lines/s ([poc/README.md](poc/README.md#what-was-verified-2026-09-14)).

We also compared anomalyd with the OTel Collector's Go `drain` processor path. Both runs used the
same 3M-line file and were limited to one CPU in Docker. The OTel path mined 138k lines/s.
anomalyd mined 1.56 M lines/s.

Three cross-checks back these figures:

- On the test file the templates are identical to Drain3's.
- The scores match the Python scorer to 1e-9.
- A Docker end-to-end run with Prometheus 3.14.0 and Alertmanager 0.34.0 raised exactly the
  three injected incidents.

anomalyd is a pilot candidate for iteration 2. It does not replace the iteration-1 add-ons.

## Contents

| Document | What's in it |
|---|---|
| [docs/reference-architecture.md](docs/reference-architecture.md) | Data-flow diagram, tiers, the three ways to turn logs into counts, series/storage budget |
| [docs/comparison-matrix.md](docs/comparison-matrix.md) | Verdict per option for logs, metrics, traces, correlation/LLM, and commercial baselines |
| [docs/cost-sizing.md](docs/cost-sizing.md) | Volume arithmetic, measured throughput, LLM cost model, iteration-1 footprint |
| [docs/roadmap.md](docs/roadmap.md) | Step 0 (check what runs today), iterations 1–3, branch for the legacy OSS 7.10 build |
| [docs/verification-review.md](docs/verification-review.md) | Second check against primary sources: 415 claims re-checked, corrections, PoC defects fixed |
| [docs/research/01-logs-elastic-basic.md](docs/research/01-logs-elastic-basic.md) | Elastic licences and features by tier, ElastAlert2, template mining, pipeline options |
| [docs/research/02-metrics-prometheus.md](docs/research/02-metrics-prometheus.md) | PromQL anomaly-detection frameworks, Grafana OSS vs Cloud, Python libraries, cardinality strategy, SLOs |
| [docs/research/03-traces-jaeger.md](docs/research/03-traces-jaeger.md) | Jaeger v1/v2, span metrics, tail sampling, pipeline topology, trace-level anomaly detection |
| [docs/research/04-aiops-rca-llm-cost.md](docs/research/04-aiops-rca-llm-cost.md) | Alert correlation, root cause analysis (RCA) tools, LLM triage design and cost, commercial baselines |
| [docs/research/05-methods-benchmarks.md](docs/research/05-methods-benchmarks.md) | Benchmark evidence (TSB-AD, Loghub), foundation models, method tiers, industry lessons |
| [poc/README.md](poc/README.md) | Configs and small services, how to run them, what was validated, gotchas found |
| [anomalyd/README.md](anomalyd/README.md) | Go PoC: log miner on each node, central seasonal detector, benchmarks, resource model, end-to-end test |
| [report/anomaly-detection-report.html](report/anomaly-detection-report.html) | Interactive summary report; live at https://elvismanchkin.github.io/self-hosted-anomaly-detection/ |
| [LINKS.md](LINKS.md) | Every URL consulted, with notes |

## Verification status

We validated all PoC pieces locally on synthetic data. Nothing touched our real systems. We used
these tools:

- `promtool` (unit and end-to-end tests)
- `amtool`
- `otelcol-contrib validate`, plus runtime tests for the logs and traces pipelines
- Filebeat 9.5.3
- a throwaway Elasticsearch 9.5.3 container on the default Basic license (ingest pipeline,
  transform, ElastAlert2 rules)
- Python self-tests and benchmarks
- Go unit tests with the race detector
- parity tests of anomalyd against the Python scorer and Drain3
- a Docker end-to-end run of anomalyd with Prometheus 3.14.0 and Alertmanager 0.34.0

The results and the problems they found are in
[poc/README.md](poc/README.md#what-was-verified-2026-09-14).

Not verified:

- behaviour and throughput on our real logs and metrics
- Filebeat regex CPU cost
- tail-sampling memory at real span rates
- ElastAlert2 against Elasticsearch 7.x
- vendor prices that are available only on request (quote-only)

## License

- Code, configuration and scripts: [Apache License 2.0](LICENSE).
- Documentation and research text (all Markdown files and the report):
  [CC BY 4.0](LICENSE-docs). You may reuse and adapt it if you credit the source.
- `anomalyd/internal/drain` is a Go port of [Drain3](https://github.com/logpai/Drain3), which
  uses the MIT License. Its notice is in [NOTICE](NOTICE).
