# Cost and sizing

**In short:**

- Our open-source plan has no license fees. It costs compute, a little storage and engineering
  time.
- At 2 TB/day, Drain-based template mining needs 7–23 cores at the conservative speed. The
  anomalyd agent needs < 0.2 cores at its synthetic speed, and < 1 core if real logs run 5×
  slower.
- Traces are the one part likely to *save* money: Elasticsearch stores only a few percent of
  spans.
- Triage with a large language model (LLM) costs $1–$780/month, by model and volume. We recommend
  a cap of $100/month from iteration 2.
- Managed products for logs + metrics with anomaly detection cost ≈ $2.3k–$609k/month at our
  volume (§8). Logs-only products start lower, for example SigNoz at $1.8k/month.
- **Treat every figure as an order of magnitude.** Measure again on a real host before you buy
  hardware.

Terms and abbreviations: [glossary](glossary.md).

Sections:

- [1. How much log data is that?](#1-how-much-log-data-is-that)
- [2. Log template mining: how many cores?](#2-log-template-mining-how-many-cores)
- [3. Template key at ingest: what does it cost?](#3-template-key-at-ingest-what-does-it-cost)
- [4. What does it add to Prometheus?](#4-what-does-it-add-to-prometheus)
- [5. Traces: what do they cost or save?](#5-traces-what-do-they-cost-or-save)
- [6. What does LLM triage cost?](#6-what-does-llm-triage-cost)
- [7. What does iteration 1 add?](#7-what-does-iteration-1-add)
- [8. Prices, formulas and sources](#8-prices-formulas-and-sources)

This page sizes the compute that our plan adds. The figures come from measurements in this repo
and from the research files. We measured on synthetic data, on a laptop, in Docker. Measure again
on a canary host (one host where we test first) with real logs before you buy hardware.

Every US$ figure comes from [cost_model.py](cost_model.py), a Python script that needs only the
standard library. It holds:

- the list prices, each with its source link
- the scenario assumptions
- the formulas

§8 is its output. To change a price, edit it in cost_model.py and run
`python3 docs/cost_model.py --write`. That command rewrites the §6 table, §8 and the report's
cost chart.

## 1. How much log data is that?

| Measure | 200 GB/day | 2 TB/day |
|---|---|---|
| Average ingest | ≈ 2.3 MB/s | ≈ 23 MB/s |
| Lines/s at 300–1,000 B/line (average) | ≈ 2.3k–7.7k | ≈ 23k–77k |
| Lines/s at a 3× peak (assumption) | ≈ 7k–23k | ≈ 69k–231k |

Source: [research/01 §4](research/01-logs-elastic-basic.md). The 3× peak factor is our assumption.
We did not measure it on our own logs.

## 2. Log template mining: how many cores?

A template is a log line with its variable parts (numbers, IDs) masked. In paths B and C of the
[reference architecture](reference-architecture.md), each host mines templates and sends only
counts:

- Path B: the OTel Collector or Drain3 counts templates into Prometheus. OTel is short for
  OpenTelemetry.
- Path C: the anomalyd agent sends counts to the anomalyd server.

All of them use [Drain](https://jiemingzhu.github.io/pub/pjhe_icws2017.pdf), a log template mining
algorithm. We size them with these speeds per core:

- **Conservative: 10k lines/s.** This is Python `logparser` Drain on the real
  [Loghub-2.0](https://github.com/logpai/loghub-2.0) datasets: 7–10k lines/s. We derived it from
  the published parse times (dataset lines ÷ wall-clock seconds). That implementation is
  single-threaded, so the figure is ≈ per core ([research/01 §4.1](research/01-logs-elastic-basic.md)).
- **Optimistic: 100k lines/s (an upper bound).** This repo's `drain3_exporter.py --bench` ran at
  96k–110k lines/s on one core in the PoC benchmark
  ([poc/README.md](../poc/README.md#what-was-verified-2026-09-14)). A separate side-by-side run
  with anomalyd gave 94–95k lines/s
  ([anomalyd/README.md](../anomalyd/README.md#why-build-this-instead-of-using-the-python-pieces)).
  Both runs used 300k synthetic lines: 141 B/line, 23 Drain3 templates. Drain's cost grows with the number of templates. The four Loghub-2.0 datasets behind
  the 10k anchor have 46–1,241 templates each. So real logs will run slower.
- **Go, anomalyd agent: 1.3–1.5 M lines/s (also an upper bound).** `anomalyd bench` ran on the
  same 300k-line file, on one core, JSON field extraction included. It found the same 23 templates
  with the same counts as Drain3 ([anomalyd/README.md](../anomalyd/README.md)). The same limit
  applies: real logs with more templates run slower.
  - Container stdout adds an envelope around each line. CRI (Container Runtime Interface) is the
    log format of containerd and CRI-O. On CRI lines the agent did 1.53 M lines/s (1.49–1.60 M
    over runs), in Docker on one CPU.
  - Docker's json-file envelope costs more: 0.54–0.59 M lines/s on the laptop. Each line needs a
    second JSON decode.

| Peak load | Cores at 10k lines/s/core | Cores at 100k lines/s/core | Cores at 1.4M lines/s/core (anomalyd) |
|---|---|---|---|
| 200 GB/day (7k–23k lines/s) | 1–3 | < 1 | < 0.02 |
| 2 TB/day (69k–231k lines/s) | 7–23 | 1–3 | < 0.2 (< 1 even if real logs run 5× slower) |

**OTel `drain` processor (Go): about 138k lines/s, or 92k on container stdout.** We ran
otelcol-contrib 0.160.0 in a Docker container limited to one CPU. The pipeline was `file_log` +
`json_parser` → `drain` → `count`.

- A 3M-line synthetic file took 21.8 s, start-up included. Memory was 78 MiB RSS (resident set
  size: the RAM the process uses).
- The same lines as CRI container stdout, with the `container` operator in front, took 32.5 s:
  92k lines/s, 95 MiB.
- The anomalyd agent did the same two files under the same limit at 1.56 M and 1.53 M lines/s
  ([anomalyd/README.md](../anomalyd/README.md#why-build-this-instead-of-using-the-python-pieces)).
- All runs found 23 templates. The script is
  [anomalyd/test/bench/compare.sh](../anomalyd/test/bench/compare.sh), with `CONTAINER=cri`.

As a per-node agent, each node sees only its own share of the load.

**Sharding:** each process has its own Drain tree. So shard by service, with one tree per shard.
Otherwise the template IDs differ between shards.

## 3. Template key at ingest: what does it cost?

This is path A: Filebeat adds a template key to each line, and the counts stay in Elasticsearch.

| Item | Observation or estimate | Status |
|---|---|---|
| Filebeat `replace` + `fingerprint` CPU | 6 regex replacements + 1 hash per event, on every host | **Unmeasured** (note 1) |
| Elasticsearch ingest-pipeline variant | 300k synthetic docs in 7.9 s, indexing included (note 2) | laptop smoke test |
| Continuous transform | roughly 1 s of search per 1-minute checkpoint at 2 TB/day (note 3) | extrapolated from a single tiny run |
| Rollup storage | ≈ 13 M rows/day, a few GB/day (note 4) | assumption ([reference-architecture.md](reference-architecture.md)) |
| ElastAlert2 | 10–30 rules × 1 aggregation per minute; one small container (note 5) | negligible query load on the rollup; the raw-index aggregations are unmeasured |

Notes:

1. Test on one canary host. Compare Filebeat CPU before and after.
2. A local Elasticsearch 9.5.3 single node with a 1 GB heap indexed the docs through the pipeline.
   This test does not isolate the pipeline's own share of the time.
3. A continuous transform works in checkpoints. Each checkpoint processes the documents that
   arrived since the last one. The local test processed 300k docs with 91 ms of search time. A
   1-minute checkpoint at 2 TB/day holds ~2.8 M docs. Linear extrapolation gives roughly 1 s of
   search per checkpoint. In production, watch `_transform/_stats`.
4. The rollup is the per-minute count of lines per template key. Rows/day = services × templates
   present per minute × 1,440. The 13 M rows/day is an example.
5. The rules run on the rollup index. Flatline and error-ratio rules run as terms aggregations on
   the raw indices instead.

## 4. What does it add to Prometheus?

The anomaly-detection inputs are recording rules: Prometheus rules that save a query result as a
new series.

| Item | Estimate | Source |
|---|---|---|
| Anomaly-detection input series | 3–5 k service-level recording rules (≤ 15 k) | [research/02 §7](research/02-metrics-prometheus.md) |
| Series derived by the band rules | ~30–85 k: 9 per adaptive input, 17 per robust input | [research/02 §1.1](research/02-metrics-prometheus.md) |
| Band-rule evaluation load | ~9–15 M samples read per minute at 5 k inputs (note 6) | research estimate, **unverified** |
| Retention | ≥ 7 days for weekly bands, ≥ 30 days for SLO windows (note 7) | [research/02 §7.3–7.4](research/02-metrics-prometheus.md) |
| Tier-2 scorer CPU | 600–900 series/s per core (note 8) | measured, synthetic series |
| Tier-2 query load | 20–40 M samples fetched from Prometheus per run (note 9) | arithmetic |

Notes:

6. Upstream, the long-window rule groups run every 5 min.
7. SLO means Service Level Objective. A separate anomaly-detection Prometheus with 90 days of
   retention holds only the ~100 k recorded series.
8. Measured locally: 1.1 ms/series with 2 weeks of 5-min history, and 1.6 ms/series with 4 weeks.
9. 5 k series × 4–8 k points per run = 20–40 M samples per run. So run every 5 min, with one query
   per recording rule.

## 5. Traces: what do they cost or save?

| Item | Estimate | Source |
|---|---|---|
| Elasticsearch trace storage | ~95–97 % fewer spans written (note 10) | [research/03 §3.3](research/03-traces-jaeger.md) |
| `tail_sampling` buffer | ≈ 0.6–3 GB per instance at 40k spans/s (note 11) | [research/03 §3.2](research/03-traces-jaeger.md); size of our spans **unverified** |
| Span-metric series | ≈ 80 × operations × collector replicas (note 12) | [research/03 §2.4, tool cards](research/03-traces-jaeger.md) |

Notes:

10. We keep a 2 % baseline plus all error and slow traces. The kept volume jumps during incidents.
    So size storage for that, or cap it with the `rate_limiting` / `composite` policies.
11. Set `num_traces ≥ 2 × new traces/s per instance × decision_wait`. Memory ≈ spans/s ×
    `decision_wait` × 1–5 KB per span.
    - 0.6 KB is what we measured for minimal spans.
    - ≈ 5 KiB is implied by Elastic's OTel-demo benchmark.
    - Example: 40k spans/s × 15 s ≈ 0.6–3 GB per instance.
12. The ≈ 80 assumes the default buckets. Jaeger's formula says 72, because it leaves out `_sum`
    and `_count`. With the 11 buckets of our proof of concept (PoC), it is 60. The count also multiplies by the app pods per
    service, unless you set `resource_metrics_key_attributes: [service.name]`.

The trace tier is the one part likely to *save* money. Elasticsearch stores only a few percent of
spans. The RED metrics (Rate, Errors, Duration) still reflect 100 % of traffic.

## 6. What does LLM triage cost?

We make one LLM call per Alertmanager group. The call carries a bundle: the alert context in one
prompt. The formula line below gives the bundle sizes S, M and L.

- Prices are standard API list prices. We checked them on 2026-09-15 at the vendor pages linked
  in §8. The Anthropic prices are also in the Claude API reference.
- Claude Sonnet 5's $2/$10 (input/output per 1M tokens) is now its standard price. The planned rise
  to $3/$15 was cancelled.
- Sonnet 5 uses a newer Claude tokenizer. The vendor says it produces ~30 % more tokens for the
  same text. So the Sonnet 5 row can run up to ~30 % higher: ≈ $975/month at 500/day × L.

<!-- cost_model:llm -->
Formula: `bundles/day × 30 × (tokens_in × $in + tokens_out × $out) / 1e6`. Bundle sizes in tokens: S = 5k in / 0.5k out, M = 12k in / 0.8k out, L = 20k in / 1k out.

| Model | 50/day × S | 200/day × M | 500/day × L | 500/day × L, batch |
|---|---|---|---|---|
| Gemini 2.5 Flash-Lite | $1.05 | $9.12 | $36.00 | $18.00 |
| OpenAI gpt-5.6-luna | $2.40 | $20.16 | $78.00 | $39.00 |
| Gemini 3.1 Flash-Lite (shutdown 2027-05-07) | $3.00 | $25.20 | $97.50 | $48.75 |
| Gemini 3.8 Flash, promo to 2026-12-31 | $8.44 | $72.00 | $281 | $141 |
| Gemini 3.8 Flash, from 2027-01-01 | $16.88 | $144 | $563 | $281 |
| Claude Haiku 4.5 | $11.25 | $96.00 | $375 | $188 |
| Claude Sonnet 5 | $22.50 | $192 | $750 | $375 |
| OpenAI gpt-5.6-terra | $24.00 | $202 | $780 | $390 |
| Self-hosted 8B, g6.xlarge 24×7 (`$0.8048/h × 730 h`) | $588 | $588 | $588 | $588 |
| Self-hosted 8B, g6.xlarge 12 h/day (`$0.8048/h × 360 h`) | $290 | $290 | $290 | $290 |
<!-- /cost_model:llm -->

**Raw logs through an LLM are not an option.**

- 2 TB/day ≈ 500 billion tokens/day, at ~4 bytes per token. The bytes per token is an assumption.
- That costs about $375k/month even at the cheapest batch price we found: OpenAI gpt-5-nano at
  $0.025/M input tokens.
- At the Gemini 2.5 Flash-Lite batch price it is ≈ $750k/month.
- Self-hosted, it would need around 1,000–2,000 NVIDIA L4 GPUs (graphics processing units).
- Formula and table: §8.

**Guardrails** ([research/04 §2.6](research/04-aiops-rca-llm-cost.md)):

- a monthly spend limit at the vendor, plus a daily token budget in our app, tracked as a
  Prometheus counter
- ≤ 20k input / 1k output tokens per bundle
- one bundle per Alertmanager group per 30 min
- on-demand agent runs ≤ 20/day
- a cheap default model, with a stronger model only for P1 (highest-priority) incidents
- removal of personal data (PII) and secrets before anything leaves the network

**Recommended cap for iteration 2:** $100/month. At the M-bundle price, that covers ~2,000
bundles/day on Gemini 2.5 Flash-Lite, or ~200/day on Haiku 4.5.

## 7. What does iteration 1 add?

This is the compute that iteration 1 adds to our stack.

| Component | Where | Rough size | Confidence |
|---|---|---|---|
| Filebeat template-key processors | every log host | a few % CPU per host (unmeasured) | measure on a canary host |
| Elasticsearch transform | existing Elasticsearch cluster | ~1 s of search per minute at 2 TB/day (extrapolated) | low; watch transform stats |
| ElastAlert2 | 1 container | < 1 vCPU (virtual CPU), < 1 GB | medium |
| Anomaly-detection Prometheus (optional) | 1 virtual machine or pod | small single node: ~100 k series, 90 days | medium |
| OTel collector tiers 1 and 2 in front of Jaeger | collector pods | by span rate; tier 2 needs ~0.6–3 GB of buffer per 40k spans/s | medium |
| Alertmanager changes | existing | none | high |
| LLM | API | $0 in iteration 1; ≤ $100/month cap from iteration 2 | high (hard cap) |

The anomalyd pilot (iteration 2, optional) adds two things:

- **One server.** About 0.3 GB of heap holds:
  - 5k metric series with three weeks of 5-minute history
  - 30k log-template series with four days of history

  A full evaluation takes under 1 s of CPU the first time (cold). Later runs (warm) take about
  1 ms.
- **A static ~7 MB agent per log host.** Static means one binary with no outside libraries. In the
  end-to-end test it used 6.7–7.5 MiB RSS and 0.76–0.81 % of a core at 1k lines/s
  ([anomalyd/README.md](../anomalyd/README.md#verification-2026-09-15)).

For comparison, here is what managed products for logs + metrics with anomaly detection cost at
list price at our volume:

- from ≈ $2.3k/month: Elastic Serverless Complete at 200 GB/day. This would mean a migration off
  self-managed Elasticsearch.
- ≈ $8.1k–$85k/month: Grafana Cloud Pro.
- up to ≈ $609k/month: Datadog at 2 TB/day.

Formulas and sources are in §8.

## 8. Prices, formulas and sources

Scenario assumptions:

- A month has 30 days.
- Logs are kept 30 days, so one month of logs is stored. Datadog is the exception: its index keeps
  15 days.
- Every vendor uses a 60 s scrape unless noted.
- Datadog: 1 KB per log event and 100 Infra Pro hosts.
- Elastic metrics: 30–100 bytes per sample (UNVERIFIED).

The Elastic range runs from the volume as stated to the enriched size that Elastic's estimator
meters. The enriched size is ≈ 1.66× larger.

Tiered prices are graduated: each volume band is billed at its own rate. In the unit-price table,
a list such as $0.050→0.046→0.044 gives the rate of each band, from the first band to the last.

Other short forms in the tables:

- AWS is Amazon Web Services. EC2 is its virtual machine service.
- DPM means data points per minute.
- TSDS is an Elasticsearch time series data stream, the index type for metrics.

<!-- cost_model:prices -->
Prices checked 2026-09-15. `python3 docs/cost_model.py --write` generates this block. Edit the prices in that file, not here.

### Unit prices

| Item | Price | Source |
|---|---|---|
| Gemini 2.5 Flash-Lite | $0.10 in / $0.40 out per 1M tokens | [gemini](https://ai.google.dev/gemini-api/docs/pricing) |
| OpenAI gpt-5.6-luna | $0.20 in / $1.20 out per 1M tokens | [openai](https://developers.openai.com/api/docs/pricing) |
| Gemini 3.1 Flash-Lite (shutdown 2027-05-07) | $0.25 in / $1.50 out per 1M tokens | [gemini](https://ai.google.dev/gemini-api/docs/pricing) |
| Gemini 3.8 Flash, promo to 2026-12-31 | $0.75 in / $3.75 out per 1M tokens | [gemini](https://ai.google.dev/gemini-api/docs/pricing) |
| Gemini 3.8 Flash, from 2027-01-01 | $1.50 in / $7.50 out per 1M tokens | [gemini](https://ai.google.dev/gemini-api/docs/pricing) |
| Claude Haiku 4.5 | $1 in / $5 out per 1M tokens | [anthropic](https://platform.claude.com/docs/en/about-claude/pricing) |
| Claude Sonnet 5 | $2 in / $10 out per 1M tokens | [anthropic](https://platform.claude.com/docs/en/about-claude/pricing) |
| OpenAI gpt-5.6-terra | $2 in / $12 out per 1M tokens | [openai](https://developers.openai.com/api/docs/pricing) |
| OpenAI gpt-5-nano, batch | $0.025 in per 1M tokens (cheapest input found) | [openai](https://developers.openai.com/api/docs/pricing) |
| Gemini 2.5 Flash-Lite, batch | $0.05 in per 1M tokens | [gemini](https://ai.google.dev/gemini-api/docs/pricing) |
| AWS g6.xlarge (1× L4 24 GB), us-east-1 on-demand | $0.8048/h (4 vCPU, 16 GiB) | [aws](https://b0.p.awsstatic.com/pricing/2.0/meteredUnitMaps/ec2/USD/current/ec2-ondemand-without-sec-sel/US%20East%20%28N.%20Virginia%29/Linux/index.json) |
| AWS c7i.large, us-east-1 on-demand | $0.08925/h (2 vCPU, 4 GiB) | [aws](https://b0.p.awsstatic.com/pricing/2.0/meteredUnitMaps/ec2/USD/current/ec2-ondemand-without-sec-sel/US%20East%20%28N.%20Virginia%29/Linux/index.json) |
| Grafana Cloud Pro logs | process $0.050→0.046→0.044, write $0.400→0.370→0.355, retain $0.100→0.092→0.088 per GB; 50 GB free; $19/month platform fee | [grafana](https://grafana.com/pricing/), [grafana-logs](https://grafana.com/docs/grafana-cloud/platform/pricing-and-usage/logs/) |
| Grafana Cloud Pro metrics | $6.50 / $5.90 / $5.50 per 1k series (10k–100k / 100k–200k / 200k+); 10k included; billable = max(series, DPM ÷ 1) | [grafana](https://grafana.com/pricing/), [grafana-metrics](https://grafana.com/docs/grafana-cloud/platform/pricing-and-usage/metrics/) |
| Datadog | ingest $0.10/GB; index $1.70 per 1M events (15 days); custom metrics $5 per 100; 100 per host included; Infra Pro $15/host (annual) | [datadog](https://www.datadoghq.com/pricing/list/), [datadog-cm](https://docs.datadoghq.com/account_management/billing/custom_metrics/) |
| Elastic Serverless Observability Complete | ingest $0.50→…→$0.0925 per GB (8 bands, 0–150k+ GB); retention $0.040→…→$0.0188 per GB-month; TSDS metrics at 25 % of both | [elastic](https://cloud.elastic.co/cloud-pricing-table?productType=serverless) |
| SigNoz Cloud Teams logs | $0.30/GB (15 days) | [signoz](https://signoz.io/pricing/) |
| OpenObserve Cloud logs | $0.50/GB (annual commit) | [openobserve](https://openobserve.ai/pricing/) |

### Raw logs through an LLM (input only)

Formula: `GB/day × 1e9 ÷ 4 B/token × $/1M ÷ 1e6 × 30`.

| Logs | Tokens/day | gpt-5-nano batch | Gemini 2.5 Flash-Lite batch |
|---|---|---|---|
| 200 GB/day | 50 billion | $37.5k | $75.0k |
| 2,000 GB/day | 500 billion | $375k | $750k |

### Managed products at list price

A = 200 GB/day of logs + 1 M series. B = 2,000 GB/day + 10 M series. The totals exclude support, egress and discounts.

| Product | Formula | A / month | B / month |
|---|---|---|---|
| Grafana Cloud Pro, 60 s scrape | $19 + logs (process + write; + retain per the calculator) + metrics on 1× series, all graduated | $8.1k–$8.7k | $79.2k–$84.5k |
| Grafana Cloud Pro, 15 s scrape | $19 + logs (process + write; + retain per the calculator) + metrics on 4× series, all graduated | $24.6k–$25.2k | $244k–$250k |
| Datadog (logs indexed 15 days, custom metrics, 100 hosts) | GB × $0.10 + events × $1.70/M + (series − 10k) × $0.05 + 100 × $15 | $61.8k | $609k |
| Elastic Serverless Complete | graduated ingest + one month retained, logs and TSDS metrics; GB × 1.0–1.66 (metering) | $2.3k–$3.5k | $10.6k–$17.4k |
| SigNoz Cloud Teams, logs only | GB × $0.30 | $1.8k | $18.0k |
| OpenObserve Cloud, logs only | GB × $0.50 | $3.0k | $30.0k |

Cost components, per scenario:

- Scenario A:
  - Grafana: logs $2.5k–$3.1k, metrics $5.6k
  - Datadog: ingest $600, index $10.2k, custom metrics $49.5k, hosts $1.5k
  - Elastic (as stated, 30 bytes per sample): logs ingest $1.9k, retention $240, metrics $175
- Scenario B:
  - Grafana: logs $24.1k–$29.4k, metrics $55.1k
  - Datadog: ingest $6.0k, index $102k, custom metrics $500k, hosts $1.5k
  - Elastic (as stated, 30 bytes per sample): logs ingest $7.8k, retention $1.9k, metrics $863

### Compute for our open-source add-ons, at list price

One vCPU-month = c7i.large $0.08925/h ÷ 2 vCPU × 730 h = $32.58. Core counts come from §2: peak lines/s ÷ measured lines/s per core. We measured the cores on a laptop, not on an EC2 vCPU.

Low end: 69k lines/s (2 TB/day, 1 KB lines, 3× peak) at the measured synthetic speed. High end: 231k lines/s (300 B lines), with real logs 5× slower than synthetic. The 5× is an assumption. For Drain3, the high end uses its low speed anchor instead: the measured Loghub-2.0 speed.

| Log template mining at 2 TB/day | Measured lines/s per core | vCPU | $/month |
|---|---|---|---|
| anomalyd agent (CRI container logs) | 1,500k synthetic | 0.05–0.8 | $1–$25 |
| OTel `drain` path (CRI container logs) | 92k synthetic | 0.75–12.6 | $24–$409 |
| Drain3 exporter (Python) | 100k synthetic | 0.69–23.1 | $22–$753 |
<!-- /cost_model:prices -->

### What changed on 2026-09-15?

We re-checked every price at its source.

- **Elastic Serverless is priced in graduated bands.** Its "as low as $0.09/GB" is the rate only
  for volume above 150,000 GB/month. Ingest starts at $0.50/GB. The old figure (≈ $0.7–9k/month)
  applied the lowest rate to the whole volume. The graduated total is ≈ $2.3k–$17k/month.
- **Grafana's calculator bills a third logs line, Retain ($0.100 → $0.088/GB), on every GB.** But
  the logs billing doc still says 30 days are included. The range covers both readings: +22–23 %
  on logs.
- **Unchanged:**
  - the Gemini, OpenAI and Anthropic token prices
  - the AWS instance prices (price file of 2026-09-10)
  - the Grafana metrics tiers
  - Datadog, SigNoz and OpenObserve

