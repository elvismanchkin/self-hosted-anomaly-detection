# Re-verification against primary sources (2026-09-14/15)

**In short:**

- We checked 415 claims (414 with a verdict; one suggestion row has none) a second time against
  primary sources. 308 were right, 20 wrong, 7 outdated, 72 imprecise and 7 could not be
  verified.
- The corrections changed several statements, but the main conclusions stay the same. For example,
  raw logs through a large language model (LLM) cost ≈ $375k/month, not $750k. That is still ruled
  out.
- We found and fixed several defects in the proof-of-concept (PoC) configs. We re-ran each fix with
  the real tool.
- On 2026-09-15 we re-checked every price and re-ran the log paths on container stdout.
- Still not verified: behaviour on our real logs and metrics, and some sizing figures.

Terms and abbreviations: [glossary](glossary.md).

After the first research pass, we checked every claim, version, price and config key in the repo a
second time. We used primary sources:

- vendor docs and pricing pages
- source code at the pinned tags
- package registries and release feeds
- the papers themselves

Then we fixed the PoC configs and ran them again with the real tools:

- promtool and amtool
- otelcol-contrib validate, plus runtime tests
- Elasticsearch 9.5.3 and Filebeat 9.5.3
- ElastAlert2 2.31.0

The new URLs are in [LINKS.md](../LINKS.md) under "Re-verification". The research files in
`docs/research/` carry the corrected text and citations.

## How many claims were right?

| Area | Claims checked | OK | Wrong | Outdated | Imprecise | Unverifiable |
|---|---|---|---|---|---|---|
| Logs, Elastic, ElastAlert2, template mining ([01](research/01-logs-elastic-basic.md)) | 78 | 56 | 7 | 3 | 11 | 1 |
| Metrics, Prometheus, Grafana, SLOs ([02](research/02-metrics-prometheus.md)) | 66 | 46 | 6 | 0 | 14 | 0 |
| Traces, OTel Collector, Jaeger ([03](research/03-traces-jaeger.md)) | 93 | 75 | 1 | 2 | 12 | 2 |
| AIOps, LLM and commercial prices ([04](research/04-aiops-rca-llm-cost.md)) | 87 | 72 | 1 | 1 | 10 | 3 |
| Methods and benchmarks ([05](research/05-methods-benchmarks.md)) | 91 | 59 | 5 | 1 | 25 | 1 |
| **Total** | **415** | **308** | **20** | **7** | **72** | **7** |

Short forms in the table: SLO is Service Level Objective. OTel is OpenTelemetry. AIOps is AI for IT
operations.

The verdicts add up to 414, not 415. The traces row counts 93 claims but has 92 verdicts: one
row there is an optional suggestion with no verdict.

A separate pass checked that the README, the docs, the PoC README and the report agree with each
other. It found 35 issues:

- 17 figures that disagreed between files
- 10 places where a doc described a config differently from the file
- 3 broken paths
- 5 other issues

We fixed all but two. We kept those two on purpose, because the research file supports the
existing figure.

## Which corrections changed a statement or a recommendation?

- **Kibana Basic has only the Index and Server log connectors.** The Cases connector needs
  Platinum: the Kibana 9.5.3 source sets `minimumLicenseRequired: 'platinum'`. The conclusion
  stays the same. Basic can't send a webhook, so ElastAlert2 is still the alert path.
- **Raw logs through an LLM cost ≈ $375k/month, not $750k.** gpt-5-nano batch input is
  $0.025/M tokens. The conclusion stays the same: it is still ruled out.
- **Span metrics cost ≈ 80 series per operation with default buckets, not 72.** Span metrics are
  metrics that the OTel Collector computes from trace spans. Jaeger's formula leaves out
  `_sum`/`_count`. The series also multiply by app pods, unless `resource_metrics_key_attributes`
  is set. The PoC now sets it.
- **We softened the benchmark wording.**
  - Simple statistical methods *top the univariate [TSB-AD](https://github.com/TheDatumOrg/TSB-AD)
    ranking*. Univariate means one series at a time. But some models submitted by their own
    authors beat them.
  - Classical log detectors are *competitive on the public datasets*.
  - Uber and Netflix do page on-call staff from anomaly output, but only after a confirmation
    stage.
- **Other version and fact fixes:**
  - Filebeat `replace` exists since 7.8.0, not 8.5.
  - The OpenSearch Anomaly Detection plugin is at 3.8.0.0.
  - Jaeger v2.20 rejects the old Elasticsearch `use_aliases`/`use_ilm` keys.
  - The OTel `otlp` exporter type is a deprecated alias of `otlp_grpc`.
  - [Loghub](https://github.com/logpai/loghub) has 19 datasets and about 77 GB.
  - Toto was trained on more than 2 trillion (2 T) points.
  - The 10k lines/s speed anchor for [Drain](https://jiemingzhu.github.io/pub/pjhe_icws2017.pdf)
    is derived, not published. The 100k anchor is an upper bound.

## Which PoC defects did we fix?

We re-ran each fix with the real tool.

| Defect | Effect before the fix | Fix |
|---|---|---|
| The tier-2 scorer re-exported `anomaly_*` labels | once scraped, every upstream band rule failed with "same labelset" (note 1) | labels stripped; anomalyd strips them too |
| The LLM-triage route matched only `tier` | `AnomalyDetected` and ElastAlert2 findings never reached it | route matches on `severity`, excluding `tier="meta"` |
| `AnomalyJobStale` used the scorer's own timestamp | silent exactly when the scorer is down | `or absent_over_time(...)` plus a test that fails on the old rule |
| The OTel logs path exported `log_record_template` names | the Prometheus log rules and bands got no data from path B | config emits `log_lines_total` / `log_template_lines_total{template_id}` |
| `count` → `delta_to_cumulative` without `batch` | 2 of 6 replays lost 0.5–2.6 % of lines (out-of-order deltas) | `batch` in front of `count`; 11 of 11 runs complete |
| The transform's `terms` group-by had no `missing_bucket` | 510 of 300,510 lines silently dropped (note 2) | `missing_bucket: true` |
| ElastAlert2 rules: three defects (note 3) | findings not grouped per service; `service="null"`; muted matches | labels, `key` for flatline, `query_key`, `use_keyword_postfix: false` |
| The upstream sparse-series filter does nothing in v0.2.1 | sparse templates got bands anyway | own filter in `anomaly-inputs.yml`, with a test |

Notes:

1. The upstream rules are the band rules of grafana/promql-anomaly-detection.
2. The dropped lines had no `service.name`, or a template longer than 1,024 characters.
3. The three ElastAlert2 defects:
   - The rules had no `service`/`tier` labels. So findings were not grouped per service.
   - Flatline read the wrong field. So flatline alerts said `service="null"`.
   - One rule lacked `query_key`. So one match muted all others for 6 h.

   The fix adds the labels, uses `key` for flatline, adds `query_key` and sets
   `use_keyword_postfix: false`.

[poc/README.md](../poc/README.md#gotchas-found-while-validating) describes each defect under
"Gotchas found while validating".

## What changed on 2026-09-15: prices and container stdout

**Prices.** We fetched every unit price again from the vendor page, price table or price file. That
covers nine vendors. The source links are in
[cost-sizing.md §8](cost-sizing.md#8-prices-formulas-and-sources). [cost_model.py](cost_model.py)
now computes each monthly figure and prints the formula next to the result. The report shows the
formula and source of every bar.

A and B are the two sizes we price: A = 200 GB/day of logs + 1 M series, B = 2 TB/day + 10 M series.

| Item | Before | After | Why |
|---|---|---|---|
| Elastic Serverless Complete, A / B | ≈ $0.7–0.9k / $7–9k | ≈ $2.3k–$3.5k / $10.6k–$17.4k | ingest is billed in 8 graduated bands (note 4) |
| Grafana Cloud Pro logs, A / B | ≈ $2.5k / $24.1k | $2.5k–$3.1k / $24.1k–$29.4k | two readings of retention, kept as a range (note 5) |
| Claude Sonnet 5 | $2 / $10, introductory | $2 / $10, standard | the planned rise to $3 / $15 was cancelled |
| Gemini, OpenAI, Anthropic tokens; Amazon Web Services (AWS) instances; Grafana metrics; Datadog; SigNoz; OpenObserve | n/a | unchanged | no change at the source |

Notes:

4. Graduated means each band is billed at its own rate. The bands start at $0.50/GB. "As low as
   $0.09" is the marginal rate above 150,000 GB/month. The old figure applied it to every GB.
5. The calculator bills a Retain line on every GB. The billing doc says 30 days are included.

**Container stdout.** Our apps log to stdout. So Filebeat reads the container runtime's files under
`/var/log/containers`. Each line there is wrapped in a CRI envelope. CRI is the Container Runtime
Interface. We re-ran the log paths on files in that format:

| Finding | Effect | Fix |
|---|---|---|
| Plain-text lines keep their timestamp in `message` | Filebeat template key: 13,218 ids for 20k lines | `<TS>` mask first, in Filebeat and the ingest pipeline: 24 ids |
| Plain-text lines have no `log.level` | level-based ElastAlert2 rules miss text apps | ingest-pipeline grok on a leading level word |
| Container logs carry no `service.name` unless the app's JSON sets it | rollup and rules group those lines under "missing" | fall back to `kubernetes.container.name` |
| Filebeat's `container` parser joins a split stdout line with an interleaved stderr line | one merged and one broken event | none in Filebeat; anomalyd joins per stream |
| OTel `container` operator: one resource per pod | Prometheus exporter showed one pod's count per series | drop the resource after deriving `service` |
| OTel `count` stamps deltas with record times, which the `container` operator now sets | up to 79 % of lines dropped as out-of-order | clear the record time; `batch` + `groupbyattrs` before `count` (note 6) |
| The anomalyd agent read plain lines only | CRI envelopes mined as text | `--container cri\|docker\|auto`; split lines joined per stream |
| The anomalyd agent kept deleted files open | a deleted pod's disk space stayed allocated | files gone for 10 s are closed |

Notes:

6. After the fix, 10 of 10 runs were exact.

## What is still not verified?

- behaviour on our real logs and metrics, and throughput on real log shapes
- Filebeat regex CPU on a real host
- tail-sampling memory at real span rates
- ElastAlert2 against Elasticsearch 7.x
- quote-only vendor prices, and how Elastic's metered (enriched) GB relates to our GB/day
- the log paths on a real Kubernetes node (kubelet rotation under load, `add_kubernetes_metadata`)
