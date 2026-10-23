# 06 — Feature gaps: what comparable products and recent research offer that we lack

**In short:**

- On 2026-10-23 we compared anomalyd and our plans with comparable products, with recent
  research, and with the code itself. The result is the plan
  [feature-gaps.md](../plans/feature-gaps.md) (gaps G1–G37). This page keeps the evidence behind
  it: what each product documents, which research results we used, and how each candidate was
  checked.
- **Most product features are already in our plans.** Of 68 distinct candidates, 12 were
  already built or fully planned, 19 were refuted, and 37 were confirmed as gaps.
- **The confirmed gaps cluster in four areas:**
  - evidence for root cause analysis (RCA): which label slice, which related series, which
    templates and which traces explain a finding;
  - the finding lifecycle (flapping near the threshold);
  - baseline hygiene (past incidents, daylight saving time);
  - log severity for lines without a level word.
- **The vendor claims are not re-verified.** They come from vendor documentation read on
  2026-10-23. We did not re-check them against a second source, as
  [verification-review.md](../verification-review.md) did for research 01–05. Treat them as
  UNVERIFIED.

Terms and abbreviations: [glossary](../glossary.md).

Sections:
[How the research ran](#how-the-research-ran) ·
[Products](#what-comparable-products-document) ·
[Research](#research-results-we-used) ·
[Rejected](#research-we-considered-and-rejected) ·
[What the code scan added](#what-the-code-scan-added)

## How the research ran

Four sweeps ran in parallel. A merge step and skeptical reviewers followed.

| Step | What it did | Output |
|---|---|---|
| Inventory | Read the READMEs, the roadmap, the reference architecture, the comparison matrix and all of `docs/plans/`. Listed what is built, what is planned (with ids) and what is rejected. | The baseline to classify against |
| Product survey | Read the documentation of the products below for user-facing capabilities. | 27 candidates |
| Code gap scan | Read `anomalyd/internal`, `cmd`, `deploy`, `test`, `poc` and CI for missing product and operational features, with file references. | 32 candidates |
| Methods research | Read 2022–2026 research on log and time-series anomaly detection, RCA, alert fatigue, LLM triage and evaluation. Started from research 04 and 05 so as not to repeat them. | 22 candidates |
| Merge and classify | Merged duplicates. Classified each candidate as built, planned, partly planned, rejected by an existing decision, or missing. | 68 features: 12 covered, 56 to check |
| Verify | Three reviewers tried to refute each "missing" or "partly planned" candidate: with code and plan evidence, or because it does not fit our constraints (self-hosted, small team, Elasticsearch Basic, add-ons only). | 37 confirmed, 19 refuted |

Each confirmed gap got a value and an effort rating from the reviewer. The plan uses those
ratings to order the batches.

## What comparable products document

| Product | Capability (as documented) | Where it lands for us | Source |
|---|---|---|---|
| Datadog Watchdog Explains | Ranks the tag values that explain an anomaly on a graph | G15 dimension attribution | [docs](https://docs.datadoghq.com/dashboards/graph_insights/watchdog_explains/) |
| Datadog Watchdog Insights (logs) | Error outliers: attributes that are over-represented in a log spike | G3 log rate analysis | [docs](https://docs.datadoghq.com/logs/explorer/watchdog_insights/) |
| Datadog outlier monitors | A member of a group (host, pod) that behaves unlike the others | G19 peer-group outliers | [docs](https://docs.datadoghq.com/monitors/types/outlier/) |
| Datadog forecast monitors | Alert before a metric crosses a limit | G24 seasonal forecast | [docs](https://docs.datadoghq.com/monitors/types/forecasts/) |
| Datadog anomaly monitors | A time zone setting, so seasonality follows local time across DST | G7 DST-aware lags | [docs](https://docs.datadoghq.com/monitors/guide/how-to-update-anomaly-monitor-timezone/), [docs](https://docs.datadoghq.com/monitors/types/anomaly/) |
| Datadog faulty deployment detection | A verdict per deployment from its error rates | Rejected as a feature; residue goes to X4 (change events) | [docs](https://docs.datadoghq.com/watchdog/faulty_deployment_detection/) |
| Datadog Pattern Inspector | The value distribution behind each wildcard of a log pattern | G26 parameter tracking | [blog](https://www.datadoghq.com/blog/pattern-inspector/) |
| Elastic log rate analysis | Field/value pairs that explain a change in log rate. Platinum licence. | G3, rebuilt from `significant_terms`, which works on Basic and OSS 7.10 | [blog](https://www.elastic.co/blog/elastic-stack-aiops-labs-8-12-ga-of-log-rate-analysis), [aggregation](https://www.elastic.co/docs/reference/aggregations/search-aggregations-bucket-significantterms-aggregation) |
| Elastic ML anomaly explanation | Multi-bucket impact and other factors behind a score | G4 lifecycle, G21 priority | [docs](https://www.elastic.co/docs/explore-analyze/machine-learning/anomaly-detection/ml-ad-explain) |
| Elastic ES\|QL `CHANGE_POINT` | Classifies a change as a spike, a dip, a step or a trend | G21 shape | [docs](https://www.elastic.co/docs/reference/query-languages/esql/commands/change-point) |
| Elastic ML custom rules | "Skip model update" keeps an incident out of the baseline | G14 contamination-resistant baselines | [docs](https://www.elastic.co/docs/explore-analyze/machine-learning/anomaly-detection/ml-ad-run-jobs) |
| Grafana Cloud outlier detection | Peer-group outliers in Grafana | G19 | [docs](https://grafana.com/docs/grafana-cloud/machine-learning/outlier-detection/) |
| Grafana Cloud forecasting | Forecasts with holidays and seasonality settings | G24, X5 calendars | [docs](https://grafana.com/docs/grafana-cloud/machine-learning/dynamic-alerting/forecasting/), [holidays](https://grafana.com/docs/grafana-cloud/machine-learning/machine-learning/forecasting/holidays-and-seasonality/) |
| Grafana Sift | Fixed, cheap checks attached to an alert (restarts, errors, deployments) | G20 investigation checks | [docs](https://grafana.com/docs/grafana-cloud/alerting-and-irm/machine-learning/sift/analyses/) |
| Grafana Adaptive Logs | Log volume per pattern and recommendations on what to drop | G23 byte accounting | [docs](https://grafana.com/docs/grafana-cloud/adaptive-telemetry/adaptive-logs/introduction/) |
| Grafana Logs patterns | Patterns over time, next to the log view | G10, G16 | [docs](https://grafana.com/docs/grafana/latest/visualizations/simplified-exploration/logs/patterns/) |
| AWS CloudWatch Logs anomaly detection | Pattern anomalies, token values, and a priority from keywords | G1 keyword severity, G26, G21 | [docs](https://docs.aws.amazon.com/AmazonCloudWatch/latest/logs/LogsAnomalyDetection.html) |
| AWS CloudWatch anomaly detectors | Excluded time ranges and a time zone per detector | G14, G7 | [API](https://docs.aws.amazon.com/AmazonCloudWatch/latest/APIReference/API_AnomalyDetectorConfiguration.html) |
| Splunk ITSI | Adaptive thresholds, entity cohesion, predictive analytics | G19, G24 | [docs](https://docs.splunk.com/Documentation/ITSI/latest/SI/AD), [predictive](https://help.splunk.com/en/splunk-it-service-intelligence/splunk-it-service-intelligence/visualize-and-assess-service-health/5.0/predictive-analytics/use-the-predictive-analytics-dashboard-in-itsi) |
| Dynatrace Davis | Event correlation along the topology; frequent-issue detection | G27 topology grouping; recurring-issue detection rejected | [correlation](https://docs.dynatrace.com/docs/dynatrace-intelligence-classic/root-cause-analysis/event-analysis-and-correlation), [frequent issues](https://docs.dynatrace.com/docs/shortlink/frequent-issues) |
| Netdata | Per-metric ML ensemble, anomaly rate, metric correlations | G34 consensus gate (low), G16; the anomaly-rate rollup is a PromQL rule, not a feature | [anomaly detection](https://learn.netdata.cloud/docs/netdata-ai/anomaly-detection), [correlations](https://learn.netdata.cloud/docs/netdata-ai/troubleshooting/metric-correlations) |
| Coroot | Deployment tracking, RCA over the service map | G20, G27 | [deployments](https://docs.coroot.com/inspections/deployment-tracking), [RCA](https://coroot.com/blog/we-built-ai-powered-root-cause-analysis-that-actually-works/) |
| Keep | Enrichment from a mapping table; topology correlation | G9 ownership, G27 | [mapping](https://docs.keephq.dev/overview/enrichment/mapping), [topology](https://docs.keephq.dev/overview/correlation-topology) |
| Robusta and HolmesGPT | Built-in alert enrichment; custom toolsets for the agent | G20, G31 HolmesGPT toolset | [enrichment](https://docs.robusta.dev/master/playbook-reference/builtin-alert-enrichment.html), [toolsets](https://docs.robusta.dev/master/configuration/holmesgpt/custom_toolsets.html) |
| LinkedIn ThirdEye | Dimension analysis of an anomaly | G15 | [blog](https://www.linkedin.com/blog/engineering/analytics/analyzing-anomalies-with-thirdeye) |
| Uber uVitals | Per-metric seasonality and dimension drill-down | G15; online seasonality detection rejected (X5 covers lag choice) | [blog](https://www.uber.com/blog/uvitals-an-anomaly-detection-alerting-system/) |
| Microsoft Kusto | `series_periods` seasonality detection | Rejected, as above | [docs](https://learn.microsoft.com/en-us/kusto/query/anomaly-detection) |
| New Relic change tracking | Deployments and changes next to the charts | X4 change events (already planned) | [docs](https://docs.newrelic.com/docs/change-tracking/view-analyze-data/) |

## Research results we used

| Result | What it gives | Where it lands for us | Sources |
|---|---|---|---|
| BARO (RobustScorer), RCAEval | Ranks root-cause metrics by their robust change after an estimated onset. Strong on the RCAEval testbeds; the results come from testbeds, not production. | G16 related-series ranking | [BARO](https://arxiv.org/pdf/2405.09330), [RCAEval](https://arxiv.org/html/2412.17015v1), [survey](https://arxiv.org/html/2408.13729v1) |
| Adtributor, iDice, PSqueeze and successors | Which dimension values explain a change in an aggregate | G15 | [Adtributor](https://www.usenix.org/conference/nsdi14/technical-sessions/presentation/bhagwan), [iDice](https://www.microsoft.com/en-us/research/wp-content/uploads/2016/07/ICSE-2016-1-iDice-Problem-Identification-for-Emerging-Issues.pdf), [2205.10004](https://arxiv.org/abs/2205.10004), [2305.03331](https://arxiv.org/abs/2305.03331), [PSqueeze](https://github.com/NetManAIOps/PSqueeze) |
| Nezha | Contrasts the normal and the faulty window over templates and metrics | G10 contrastive template ranking | [FSE 2023](https://doi.org/10.1145/3611643.3616249), [2407.01710](https://arxiv.org/pdf/2407.01710) |
| TraceRCA | Ranks operations that are over-represented in abnormal traces | G25 trace contrast | [paper](https://ieeexplore.ieee.org/abstract/document/9521340) |
| Alert-storm handling and topology correlation | Grouping alerts into incidents along dependencies | G27, G4 | [ICSE-SEIP 2020](https://2020.icse-conferences.org/details/icse-2020-Software-Engineering-in-Practice/24/Understanding-and-Handling-Alert-Storm-for-Online-Service-Systems), [2403.06485](https://arxiv.org/abs/2403.06485), [2403.18639](https://arxiv.org/pdf/2403.18639) |
| Outbreak surveillance and count models | Baselines that ignore past outbreaks; counts scored against an exposure (traffic) | G14, G17 | [surveillance](https://cran.r-project.org/package=surveillance), [2308.12925](https://arxiv.org/abs/2308.12925), [2007.14254](https://arxiv.org/pdf/2007.14254) |
| Semantic log labelling with LLMs | Labels a new template as indicative of failure, or as benign churn | G22, off the hot path | [2605.22779](https://arxiv.org/abs/2605.22779), [2510.22986](https://arxiv.org/abs/2510.22986), [2406.07467](https://arxiv.org/pdf/2406.07467) |
| LLM-assisted log parsing (LILAC, LibreLog and later) | Proposes masks and template merges | G37, low value: parsing accuracy does not strongly predict detection accuracy ([research 05](05-methods-benchmarks.md)) | [2604.20553](https://arxiv.org/abs/2604.20553), [2601.22590](https://arxiv.org/abs/2601.22590), [LibreLog](https://arxiv.org/pdf/2408.01585), [LILAC](https://arxiv.org/pdf/2310.01796) |
| Parameter-aware log anomaly detection, DDSketch | Tracks the values behind template wildcards | G26 | [2411.15250](https://arxiv.org/abs/2411.15250), [2409.20503](https://arxiv.org/abs/2409.20503), [DDSketch](https://www.vldb.org/pvldb/vol12/p2195-masson.pdf) |
| OpenRCA and agent-RCA evaluation | How to score LLM triage on replayed incidents | G35 triage evaluation | [OpenRCA](https://proceedings.iclr.cc/paper_files/paper/2025/file/d29b8d53678015079e1d245c023e49d2-Paper-Conference.pdf), [2606.27154](https://arxiv.org/abs/2606.27154), [PMLR v267](https://proceedings.mlr.press/v267/jha25a.html), [critique](https://www.traversal.com/blog/openrca-isnt-root-cause-analysis-why-that-matters) |
| RCACopilot | Retrieves similar past incidents | G36, low value at our incident count | [2305.15778](https://arxiv.org/html/2305.15778) |
| Alerting on SLOs | Burn-rate alerts as the impact signal | G18 SLO-burn gate | [SRE workbook](https://sre.google/workbook/alerting-on-slos/), [Sloth](https://github.com/slok/sloth) |

## Research we considered and rejected

[feature-gaps.md](../plans/feature-gaps.md#considered-and-rejected) has the full list. The
research-based ones:

| Result | Why not here | Sources |
|---|---|---|
| Adaptive conformal p-values per series | An empirical p-value from about 2,000 points per season cannot reach the tail that \|z\| ≥ 4 needs. MT already plans SPOT/GPD. | [2604.20122](https://arxiv.org/abs/2604.20122), [PMLR v329](https://proceedings.mlr.press/v329/xu26a.html) |
| Global false discovery rate (FDR) control | Our scores are not calibrated p-values, the independence assumptions fail, and findings never page. | [NeurIPS 2021](https://proceedings.neurips.cc/paper/2021/hash/def130d0b67eb38b7a8f4e7121ed432c-Abstract.html), [2112.03196](https://arxiv.org/pdf/2112.03196) |
| Affiliation and PATE metrics, TSB-AD runs of our scorer | With about 20 labelled incidents the confidence intervals dominate; TSB-AD lacks the seasonality our scorer assumes. | [affiliation](https://arxiv.org/pdf/2206.13167), [PATE](https://arxiv.org/html/2405.12096v1) |
| OneShotSTL online decomposition | One more candidate for the planned scorer interface (L5/MT). It does not handle several seasons and would break parity with the Python scorer. | [VLDB 16](https://www.vldb.org/pvldb/vol16/p1399-he.pdf) |

## What the code scan added

The code scan found mostly operational gaps, and the plans already cover most of them:
readiness, server manifests, TLS and authentication, sinks, agent delivery, multi-source
config, high availability (see
[Already covered](../plans/feature-gaps.md#already-covered-by-the-existing-plans)). What it
added on top:

- silent failures in the code at that time: a metric series that stops resolves as healthy
  (G13); a dead agent looks like a quiet service, and a wrong agent step puts counts in the
  wrong buckets (G6); a slow snapshot blocks agent pushes (G5);
- small missing pieces: Grafana dashboards (G2), self-metrics (G11), fuzz targets and manifest
  checks (G12).

Batch 1 built these seven items. Their status lines in
[feature-gaps.md](../plans/feature-gaps.md#batch-1-built) say what landed and what was
deferred.
