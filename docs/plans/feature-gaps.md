# Feature gaps beyond the existing plans

**In short:**

- This plan lists **37 gaps** (G1–G37) that the existing plans do not cover, or cover only in
  part: [future improvements](future-improvements.md) (N, X and L items), the
  [model trainer](model-trainer.md) (MT) and the [deployment plan](deployment.md) (DP). A review
  of the code and of the plans found them. Each value and effort rating is the reviewer's
  corrected rating.
- **We start small.** Batch 1 (7 small items) is in progress on branch
  `claude/gracious-cerf-2gt6lx`. Batch 2 has 9 more small items. 13 medium items come next, and 8
  large or low-value items come later.
- **Three batch-1 items fix silent failures in today's code:** a metric series that stops
  arriving resolves as healthy (G13); a dead agent looks like a quiet service, and a wrong agent
  step puts counts in the wrong buckets (G6); a slow snapshot write can hold agent pushes past
  their timeout (G5).
- **The two cheapest visible wins are also in batch 1:** severity from the template text, for
  logs with no level word such as a Java `OutOfMemoryError` or a Go `panic:` (G1), and Grafana
  dashboards for the scores that anomalyd already exports (G2).
- **The largest unplanned area is evidence for root cause analysis (RCA):** which label slice,
  which related series, which templates and which traces explain a finding (G3, G10, G15, G16,
  G20, G25). The X13 bundle names these sections, but no plan says how to fill them.
- **The finding lifecycle and the baselines need work** before the precision targets of the
  shadow phase ([DP §14](deployment.md#phase-2-anomalyd-in-shadow-mode-24-weeks)) are realistic.
  Findings flap near the threshold (G4), last week's incident leaks into this week's baseline
  (G14), and a daylight saving time switch shifts daily ramps by one hour (G7).
- Each item says what the existing plan item already covers and what this item adds, so nothing
  is built twice. Mistakes found in the existing plans are in
  [Fixes to existing plans](#fixes-to-existing-plans).

Terms and abbreviations: [glossary](../glossary.md), plus [Terms used here](#terms-used-here).
Code references are to functions, not line numbers, as of commit `3bff8b4`.

Status: plan, 2026-10-23. Only batch 1 is being built. Nothing else here exists yet.

Sections:
[Scope and conventions](#scope-and-conventions) ·
[Terms used here](#terms-used-here) ·
[Plan at a glance](#plan-at-a-glance) ·
[Batch 1](#batch-1-in-progress) ·
[Batch 2](#batch-2-small-next) ·
[Next](#next-medium) ·
[Later](#later-large-or-low-value) ·
[Fixes to existing plans](#fixes-to-existing-plans) ·
[Already covered by the existing plans](#already-covered-by-the-existing-plans) ·
[Considered and rejected](#considered-and-rejected) ·
[Open questions](#open-questions)

## Scope and conventions

- **IDs.** G1–G37 keep the numbers of the review (#1 is G1). The review ranks by value, then
  effort; this plan groups by batch.
- **Plan ids.**
  - N1–N11, X1–X14 and L1–L7 are items in [future-improvements.md](future-improvements.md).
  - MT §n is section n of [model-trainer.md](model-trainer.md). DP §n is section n of
    [deployment.md](deployment.md).
  - DP §15 numbers its own rows N1–N9. "DP §15 N5" means row N5 of that table, not
    future-improvements N5.
  - RefArch is [reference-architecture.md](../reference-architecture.md); T0–T3 are its detection
    tiers (glossary).
  - I1.3 means [roadmap](../roadmap.md) iteration 1, item 3; I3 is iteration 3.
  - research/01 … research/05 are the files in [docs/research](../research/).
- **Groups.** Batch 1, Batch 2, Next and Later are the build order for G items only. They are not
  the Now/Next/Later groups of future-improvements. Batches 1 and 2 need none of the N items, so
  they can run in parallel with that plan's Now group.
- **Effort:** S ≤ 1 engineer-week, M 1–3 weeks, L > 3 weeks (the same scale as
  future-improvements). **Value:** high, medium or low, for our stack and scale.
- **Each item has:** what it is, why it matters here, existing plans (what they cover and what
  this item adds), what to build, depends on, done when, effort and value, and sources.
- **Rules that apply to every item:**
  - The [design principles](future-improvements.md#design-principles) of future-improvements:
    reduce before you detect, bulkheads, keep the binary small, only SLO burn rate pages.
  - RCA work (G3, G10, G15, G16, G20, G25) never runs on the evaluation goroutine. Each piece
    has its own timeout and cache, and fails open, like an N5 sink.
  - **The label set of a finding is its Alertmanager identity.** Nothing here changes labels
    while a finding fires. New information goes into annotations, or into labels that are fixed
    when the finding fires.

## Terms used here

Terms that are not in the [glossary](../glossary.md).

| Term | Meaning |
|---|---|
| Adtributor | A method that finds which dimension values (for example a route or a status code) explain most of a change in a total ([NSDI 2014](https://www.usenix.org/conference/nsdi14/technical-sessions/presentation/bhagwan)). |
| BARO | An RCA method. It estimates when an incident began, then ranks every series by how far it moved after that time, in robust units (median and interquartile range of the time before) ([fse-baro](https://pypi.org/project/fse-baro/)). |
| DST | Daylight saving time. Local clocks move by one hour twice a year in many regions. |
| episode | One fire-to-resolve span of a finding. The finding id stays the same across episodes; `StartsAt` changes. |
| flapping | A finding that fires, resolves and fires again within a short time. |
| fuzz test | A test that feeds generated random inputs to a function to find crashes ([Go fuzzing](https://go.dev/doc/security/fuzz/)). |
| hysteresis | Two thresholds: a higher one to fire and a lower one to resolve, so a value that hovers near one threshold does not flip the state. |
| IANA time zone | A time zone name from the tz database, for example `Europe/Berlin`. |
| Infinity data source | A Grafana data-source plugin that reads JSON or CSV from an HTTP API. |
| interquartile range (IQR) | The distance between the 25th and 75th percentile; a robust measure of spread. |
| Jaccard similarity | The size of the overlap of two sets divided by the size of their union. |
| k-of-n firing | Fire when at least k of the last n buckets are anomalous, instead of n buckets in a row. |
| keep-firing | Keep a finding open for some quiet buckets before resolving it. Prometheus calls this `keep_firing_for` ([alerting rules](https://prometheus.io/docs/prometheus/3.9/configuration/alerting_rules/)). |
| ldflags `-X` | A Go linker flag that sets a string variable at build time; used to embed the version. |
| log-likelihood ratio (LLR) | How much better a changed rate explains an observed count than the expected rate does. A large value means a surprising change. |
| RE2 | The regular-expression syntax of Go's `regexp` package. It runs in linear time and has no backreferences. |
| service catalog | A file that maps each service to its team, owner, runbook and chat channel. |
| `significant_terms` | An Elasticsearch aggregation that returns terms that are more common in a foreground set than in a background set. Its scoring heuristics are JLH, chi_square, gnd, mutual_information, percentage and p_value ([docs](https://www.elastic.co/docs/reference/aggregations/search-aggregations-bucket-significantterms-aggregation)). |
| single-flight | At most one run of a job at a time; a new request while one runs is skipped. |
| TraceRCA | A trace-based RCA method that ranks operations which appear more in abnormal traces than in normal ones ([paper](https://ieeexplore.ieee.org/abstract/document/9521340)). |

## Plan at a glance

| ID | Gap | Group | Value | Effort | Existing coverage | Depends on |
|---|---|---|---|---|---|---|
| G1 | [Severity from template text, signature watchlist](#g1-severity-from-template-text-and-a-signature-watchlist) | Batch 1 | high | S | none | none |
| G2 | [Grafana dashboards and a findings table](#g2-grafana-dashboards-and-a-findings-table) | Batch 1 | high | S | none (N7 is Kibana only) | G11 for one row |
| G3 | [Log rate analysis on ES Basic](#g3-log-rate-analysis-on-elasticsearch-basic-with-significant_terms) | Next | high | M | none | N5 or DP §5.3, N6, X13 |
| G4 | [Finding lifecycle: k-of-n, hysteresis, flap damping](#g4-finding-lifecycle-k-of-n-hysteresis-keep-firing-flap-damping) | Next | high | M | partial: X11, MT §6.1, MT §7.1, X4 | MT §7.1 parity |
| G5 | [Non-blocking snapshot, mismatch warnings](#g5-non-blocking-snapshot-and-mismatch-warnings) | Batch 1 | medium | S | partial: X14 | none |
| G6 | [Agent registry, heartbeat, step validation](#g6-agent-registry-heartbeat-and-step-validation) | Batch 1 | medium | S | partial: X9, N4 | none |
| G7 | [Time-zone and DST-aware seasonal lags](#g7-time-zone-and-dst-aware-seasonal-lags) | Batch 2 | medium | S | none | before or with X5 |
| G8 | [Namespace on log series](#g8-namespace-dimension-on-log-series) | Batch 2 | medium | S | partial: X1, X6, L7 | G5 |
| G9 | [Owner and runbook from a service catalog](#g9-owner-and-runbook-enrichment-from-a-service-catalog) | Batch 2 | medium | S | none | G8 (namespace match) |
| G10 | [Contrastive template ranking](#g10-contrastive-template-ranking-grew-new-vanished) | Batch 2 | medium | S | partial: X13 | none; X13 consumes it |
| G11 | [Self-metrics, build info, log format](#g11-self-metrics-build-info-and-log-format) | Batch 1 | medium | S | partial: X9 | none; G29 adds the version |
| G12 | [Fuzz targets for network decoders, kubeconform](#g12-fuzz-targets-for-network-decoders-and-kubeconform-in-ci) | Batch 1 | medium | S | partial: X10 | after G6 |
| G13 | [Absent metric series instead of resolve-on-gap](#g13-absent-metric-series-instead-of-resolve-on-gap) | Batch 1 (metric part) | medium | M | partial: RefArch T0, X9 | none |
| G14 | [Contamination-resistant baselines](#g14-contamination-resistant-seasonal-baselines) | Next | medium | M | partial: X5, MT §6.2, X7 | G5, X14 |
| G15 | [Dimension attribution](#g15-dimension-attribution-which-label-slice-explains-it) | Next | medium | M | partial: X3, X13 | X13, X1 or G8 |
| G16 | [Onset-anchored related-series ranking (BARO)](#g16-onset-anchored-related-series-ranking-baro) | Next | medium | M | partial: X6, X13, RefArch T3 | X13, N8; X4 optional |
| G17 | [Count model with traffic exposure](#g17-count-model-with-traffic-exposure) | Next | medium | M | partial: X2, X3, MT §6 | X2, X3, N8 |
| G18 | [SLO-burn impact gate and miss counter](#g18-slo-burn-impact-gate-and-miss-counter) | Next | medium | M | partial: MT §5.1, DP §14, L4 | DP §6, N5 |
| G19 | [Peer-group outlier detector](#g19-peer-group-outlier-detector) | Next | medium | M | none | G6, N4 |
| G20 | [Deterministic investigation checks](#g20-deterministic-investigation-checks) | Next | medium | M | partial: X13, RefArch T3, I2.4 | X1 or G8, X13 |
| G21 | [Priority, shape and lifecycle tags](#g21-per-finding-priority-shape-and-lifecycle-tags) | Next | medium | M | partial: X6, MT §6.3 | G4, X6 |
| G22 | [Semantic template labels](#g22-semantic-template-labels) | Next | medium | M | none | G1, N9, N10 |
| G23 | [Template byte accounting, log-reduction report](#g23-template-byte-accounting-and-log-reduction-report) | Next | medium | M | none | G1, N9 |
| G24 | [Seasonal forecast and ForecastBreach](#g24-seasonal-forecast-and-forecastbreach) | Next | medium | M | partial: RefArch T0, I3 | X11 |
| G25 | [Trace contrast via Jaeger](#g25-on-demand-trace-contrast-via-jaeger-tracerca) | Later | medium | M | partial: RefArch T3, L3 | I1.3, X13 |
| G26 | [Template parameter tracking](#g26-template-parameter-tracking) | Later | medium | L | none | N10, N9, X3 |
| G27 | [Topology-aware incident grouping](#g27-topology-aware-incident-grouping-with-origin-and-symptom) | Later | medium | L | partial: X6, L3, I2.5 | X6, G16 |
| G28 | [Per-service series and template-churn quotas](#g28-per-service-live-series-and-template-churn-quotas) | Batch 2 | low | S | partial: L7 | G8 |
| G29 | [Release pipeline](#g29-release-pipeline-version-multi-stage-image-tag-workflow) | Batch 2 | low | S | partial: N10, N11 | G11 |
| G30 | [Series listing and explain detail](#g30-series-listing-and-explain-detail) | Batch 2 | low | S | partial: MT §9.5, X13 | none |
| G31 | [HolmesGPT toolset for the anomalyd API](#g31-holmesgpt-toolset-for-the-anomalyd-api) | Batch 2 | low | S | none | G30, G6 |
| G32 | [Metric label-value novelty](#g32-metric-label-value-novelty) | Batch 2 | low | S | partial: L3 | none |
| G33 | [ES rollup replay at start-up](#g33-es-rollup-replay-at-start-up) | Later | low | S | partial: X12 | X12, N4 |
| G34 | [Multi-baseline consensus gate](#g34-multi-baseline-consensus-gate) | Later | low | S | none | N8, G14 |
| G35 | [Triage evaluation harness](#g35-triage-evaluation-harness) | Later | low | M | partial: I2.3, I2.4, N8, MT §7 | G16, X13, N8 |
| G36 | [Similar-incident retrieval](#g36-similar-incident-retrieval) | Later | low | M | none | X6, X7, N6, N9 |
| G37 | [Offline LLM mask synthesis](#g37-offline-llm-mask-synthesis-and-merge-review) | Later | low | M | partial: N8, N2, N10, MT §3 | N8, N9, N10 |

## Batch 1: in progress

Batch 1 is being built on branch `claude/gracious-cerf-2gt6lx`. All seven items are small and
need no other plan item. G5, G6 and G13 fix failure modes in today's code; the other four add
features or checks.

| ID | Batch 1 builds | Batch 1 defers |
|---|---|---|
| G1 | keyword severity inference on the server; `--signature-file` and `AnomalydLogSignature` | the stderr hint (needs a wire field); the ElastAlert2 path-A rule; moving the file into the X11 config |
| G2 | service, overview and health dashboards; provisioning file; JSON check in CI | pushing annotations to Grafana's HTTP API (a later N5 sink); the findings table if the Infinity plugin is not allowed |
| G5 | copy under the lock, then encode and write off it (single-flight); loud mismatch warnings | re-bucketing on a step change; off-node snapshot copies; the resolved ring in the snapshot |
| G6 | agent registry, `GET /api/v1/agents`, step validation, heartbeat pushes | `cause=agent_silent` on volume-down findings |
| G11 | evicted and created template counters, snapshot and backfill metrics, `anomalyd_build_info`, `anomalyd version`, `--log-format`, `--log-level` | the `-X main.version` build step (G29); a counter for skipped native-histogram samples |
| G12 | `FuzzDecode`, `FuzzHandlePush`, kubeconform in CI | a server-restart end-to-end test (goes into the X10 soak) |
| G13 | `AnomalydSeriesAbsent` for metric series; no resolve-as-healthy on a gap; `--absent-ttl` | absence of periodic log templates; the interim PromQL rule |

**Order inside batch 1.** The ratings put G1 and G2 (high value) first. We suggest a different
order, because of what each item needs:

1. G5 with G11: G11's snapshot metrics are how we see that G5 works.
2. G6, then G13: the other two failure-mode fixes.
3. G12: `FuzzHandlePush` should cover the step check that G6 adds to `handlePush`.
4. G1 runs in parallel from the start; it touches different code (`logparse`, novelty).
5. G2 last: its health row plots the G11 metrics.

The review's own first batch had G7 instead of G11. G7 moves to batch 2 because it changes the
detector's hot path and the Python parity test, while batch 1 stays with fixes and observability.

### G1. Severity from template text and a signature watchlist

- **What it is.** When a template has no level word, or only `info`, infer a severity from
  keywords anywhere in its constant tokens: `panic`, `FATAL`, `OutOfMemoryError`, `OOM`,
  `deadlock`, `Traceback`, `Exception`, `segfault`, `connection refused`. Also, a list of
  known-bad signatures that always produce a finding on their first match per service per day,
  even when the rate is normal.
- **Why it matters here.** Raw container stdout and legacy files are a main input. A JVM OOM, a
  Go panic or a Python traceback usually has no level word at the start of the line.
  `logparse.DetectLevel` looks only at the first 6 tokens, and `agent.line` has no other
  fallback. `server.severity` gives `warning` only for error or warn going up. So the most
  incident-relevant line in these sources becomes an `info` novelty, or never fires if it stays
  under `--novelty-min-lines`.
- **Existing plans.** None. [N1](future-improvements.md#n1-source-abstraction-in-the-agent)
  and [N2](future-improvements.md#n2-raw-file-parsing-multi-line-header-parsers-timestamp-masks)
  map levels per source, which helps only formats that have a level field. The ElastAlert2 rule
  `poc/logs/elastalert2/rules/new-error-type-per-service.yaml` covers only ECS `error.type` on
  path A.
- **What to build.**
  - `internal/logparse/severity.go`: a keyword table and a function that returns the inferred
    level and the rule that matched. Words match whole tokens, ignoring case. A class-name rule
    looks at the last dot-separated part of a token (`java.lang.OutOfMemoryError` →
    `OutOfMemoryError`).
  - Server: compute it in `newTemplateLocked`, and again when `globalTemplate` generalises a
    template's text. Keep it in `tmplMeta`. Recompute it in `load` instead of saving it, so the
    state format does not change.
  - `newFinding` and `evalNovelty` use the inferred level for `severity` when the dominant level
    is `unknown` or `info`. The `level` label stays the observed level. A new annotation
    `severity_source="keyword:<rule>"` says why.
  - Watchlist: `--signature-file`, one `name: regex` per line (RE2). It matches the masked
    template text, so signatures use `<NUM>`, not digits. Matching runs per template on the
    server, not per line on the agent, so its cost grows with templates, not with lines.
  - The first match per (service, signature, UTC day) fires
    `AnomalydLogSignature{service,template_id,signature}`, tier 1, severity `warning`, whatever
    `--novelty-min-lines` says. It resolves after `--novelty-ttl`, like a novelty finding.
  - Metric `anomalyd_signature_matches_total{signature}`. A bad regex stops start-up and names
    the line.
- **Batch 1 defers.**
  - The stderr hint ("stderr plus an exception keyword gives `warning`"). The agent parses the
    CRI stderr flag (`Envelope.Stderr`), but the server never sees it, so this needs a new wire
    field.
  - The no-code interim for path A: an ElastAlert2 `any` or `frequency` rule with a
    `match_phrase` on `message` for the same signatures.
  - Moving the signature list into the X11 config file, once X11 exists.
- **Depends on** nothing. G22 phase 1 and G23 reuse the keyword table.
- **Done when.**
  - A table test covers every keyword, the class-name rule, and at least two look-alike tokens
    that must not match.
  - A server test: a template `panic: runtime error: index out of range` with level `unknown`
    fires a novelty finding with `severity=warning` and the `severity_source` annotation.
  - With `--novelty-min-lines=5` and a signature `oom: OutOfMemoryError`, one matching line fires
    `AnomalydLogSignature`. A second line on the same day does not; a line on the next UTC day
    does.
  - A signature file with an invalid regex fails start-up with a message that names the line.
- **Effort** S. **Value** high.
- **Sources.** https://docs.aws.amazon.com/AmazonCloudWatch/latest/logs/LogsAnomalyDetection.html ·
  https://www.elastic.co/docs/solutions/observability/streams/management/significant-events

### G2. Grafana dashboards and a findings table

- **What it is.** Provisioned Grafana OSS dashboards for the series anomalyd already exports, a
  health view of anomalyd itself, and a table of findings.
- **Why it matters here.** Grafana OSS is the metrics UI of our stack. anomalyd exports
  `anomalyd_score`, `anomalyd_expected` and `anomalyd_band_upper`/`_lower` (`handleMetrics`), and
  the anomalyd README says Grafana "can plot" them. But no dashboard ships: `anomalyd/deploy/`
  holds only `agent-daemonset.yaml`. Every team would rebuild the band charts by hand, and the
  shadow phase has no shared view for tuning. This is the cheapest visible win in the list.
- **Existing plans.** [N7](future-improvements.md#n7-kibana-dashboards-data-views-and-drill-down)
  is Kibana only. [X8](future-improvements.md#x8-score-export-to-elasticsearch-for-band-charts)
  says "today only Grafana can" show bands, yet nothing ships for Grafana. This item adds the
  Grafana side; N7 and X8 stay as they are.
- **What to build.**
  - `anomalyd/deploy/grafana/anomalyd-service.json`, `anomalyd-overview.json`,
    `anomalyd-health.json` and a file-provisioning `dashboards.yaml`, all pinned to one Grafana
    major version (the one Step 0 finds).
  - **Service dashboard.** Variables `service` and `metric` (the series anomalyd scored).
    Observed (the input recording rule) against `anomalyd_expected`, with the band as a filled
    range; `abs(anomalyd_score)` with the threshold line; log volume by level.
  - **Overview dashboard.** `topk(20, abs(anomalyd_score))`; `anomalyd_findings_active` by kind;
    a service × time heatmap of the anomaly rate,
    `count by (service)(abs(anomalyd_score) >= 2) / count by (service)(anomalyd_score)`. This is
    the anomaly-rate rollup from [Considered and rejected](#considered-and-rejected); as a panel
    query it needs no anomalyd code.
  - **Health dashboard.** `anomalyd_eval_duration_seconds`, `anomalyd_series`,
    `*_dropped_total`, agent push and line rates, templates per service, and the G11 metrics.
  - Template series need `--export-template-scores`, which is off by default, so panels default
    to volume series.
  - **Findings table.** The Infinity data source on `/api/v1/findings`, so anomalyd needs no
    Grafana token.
  - Use the upstream `grafana/promql-anomaly-detection` dashboards as the starting layout.
  - The CI configs job parses each JSON file with `jq`.
- **Batch 1 defers.** Pushing findings to Grafana's annotations HTTP API: that would be one more
  N5 sink later. The findings table ships only if the Infinity plugin may be installed (see
  [Open questions](#open-questions)).
- **Depends on** G11 for part of the health dashboard. Nothing else.
- **Done when.**
  - CI parses every dashboard file.
  - Imported into the pinned Grafana version next to the e2e stack, with data from
    `anomalyd gen`, every panel shows data and the band and threshold line render. Screenshots
    go in the PR.
  - The provisioning file loads all three dashboards with no manual step.
- **Effort** S. **Value** high.
- **Sources.** https://grafana.com/docs/grafana/latest/developers/http_api/annotations/ ·
  https://github.com/grafana/promql-anomaly-detection

### G5. Non-blocking snapshot and mismatch warnings

- **What it is.** Take the state snapshot without holding the template lock during the encode
  and the file write, and warn loudly when a snapshot does not fit the current ring shape.
- **Why it matters here.**
  - `save` runs on the evaluation loop (`Run`) and holds `tmplMu` through the gob encode and the
    file write. `handlePush`, `globalTemplate` and `addLog` need the same lock.
  - On a slow PVC the write can hold agent requests past the agent's 15 s timeout. The agent
    then retries, and until N4 dedupe exists, retrying a push that the server already applied
    double-counts it.
  - `load` silently skips every ring when the step or the ring sizes differ. A config change
    then looks like a cold start, with no message.
- **Existing plans.** [X14](future-improvements.md#x14-versioned-state-format-and-safe-upgrades)
  plans a versioned header and a refusal on mismatch. Its premise is outdated: `stateFile`
  already has `Version` (`stateVersion = 1`), `Step`, `PromCap` and `LogCap` (see
  [Fixes](#fixes-to-existing-plans)). X14 does not mention the lock. This item adds the lock fix
  and the warnings. Migrations per version stay in X14.
- **What to build.**
  - In `save`, deep-copy the per-template level maps and created times under `tmplMu`, release
    the lock, then encode and write. The ring snapshots and the findings are already copies.
  - Run the encode and write in a single-flight goroutine. If a save still runs, skip the new one
    and count it in `anomalyd_snapshot_skipped_total`.
  - In `load`, log at WARN when rings are skipped and at ERROR when nothing loads, with the old
    and new step and ring sizes.
  - The duration and failure metrics come with G11.
- **Batch 1 defers.** Re-bucketing old rings on a step change, and automatic off-node copies;
  both are low value. Keeping the ring of resolved findings in the snapshot (a residue from
  [Considered and rejected](#considered-and-rejected)) goes with the X14 rewrite.
- **Depends on** nothing. Do it before G8 and N9, which add state.
- **Done when.**
  - A test slows the write path to 5 s through a test hook. `handlePush` still answers in under
    100 ms while the save runs, under the race detector.
  - Two saves started back to back give one write and one skip.
  - A restart with a different `--step` logs one WARN with both steps and both ring sizes.
  - The existing save and restore tests pass.
- **Effort** S. **Value** medium.

### G6. Agent registry, heartbeat and step validation

- **What it is.** The server keeps a small registry of agents: last push, step, lines, and the
  share of each service's lines. It rejects pushes whose step does not fit the server step.
  Agents send an empty push when they have nothing to send.
- **Why it matters here.**
  - `handlePush` never reads `wire.Push.Agent` or `wire.Push.Step`. A dead agent looks the same
    as a service that went quiet.
  - The server step must be a multiple of the agent step, but this is only flag help text in
    `main`. With an agent step of 120 s and a server step of 300 s, counts land in the wrong
    buckets.
  - `agent.push` returns early when there are no counts. A healthy idle agent and a dead agent
    look the same to the server.
- **Existing plans.** [X9](future-improvements.md#x9-self-observability-and-health-alerts) plans
  a node-level "agent not pushing" alert from the agent's own `/metrics`. That alert cannot
  inhibit a service-level finding, because the labels differ.
  [N4](future-improvements.md#n4-idempotent-agent-pushes) adds `agent_id` and `seq` for dedupe.
  This item adds the server-side view (which agents feed which service) and the step check. X9's
  rules can then use the server metrics too.
- **What to build.**
  - A registry in `internal/server`, keyed by `wire.Push.Agent` (the agent's `--name`, by default
    the host name), and by N4's `agent_id` once that exists. It is capped; agents beyond the cap
    are counted, not tracked.
  - Step check in `handlePush`: if `p.Step > 0` and the server step is not a multiple of it,
    answer 400 with both values and count `anomalyd_push_rejected_total{reason="step_mismatch"}`.
    `p.Step == 0` (unknown) is accepted, for old agents.
  - Heartbeat: `agent.push` sends a push with no items when there is nothing to send. The server
    already answers 204 to an empty push; now it also updates the registry.
  - `GET /api/v1/agents`: agent, last push, step, lines in the last hour, and the top services
    with their share.
  - Metrics: `anomalyd_agents` and `anomalyd_agent_last_push_timestamp_seconds{agent}`. One
    series per node is fine at our scale.
- **Batch 1 defers.** `cause=agent_silent` as an annotation on volume-down findings, when agents
  that carried most of a service's lines stopped pushing. It needs share data that the registry
  first has to collect, and a rule for agents that leave on purpose (a drained or removed node).
  It follows after batch 1, at the earliest with batch 2.
- **Depends on** nothing. Pairs with N4. G19 uses the registry.
- **Done when.**
  - Server tests: a push with `Step=120` to a 300 s server gets 400 and increments the counter;
    `Step=60` and `Step=0` get 204.
  - An agent with no lines still updates `anomalyd_agent_last_push_timestamp_seconds` on every
    push interval.
  - `/api/v1/agents` lists two test agents with their per-service shares, each summing to 1.
- **Effort** S. **Value** medium.

### G11. Self-metrics, build info and log format

- **What it is.** The counters anomalyd is missing about itself, a build-info metric and a
  version command, and log output that an operator can parse.
- **Why it matters here.** A silent detector looks like a healthy system.
  - `drain.Miner.Evicted` is counted but never exported, so template churn is invisible.
  - New-template counts are the anomalyd equivalent of the `LogNewTemplatesBurst` input in
    `poc/prometheus/anomaly-alerts.yml`, and the trainer's churn signal.
  - The trainer's model set gates on `anomalyd_min`
    ([MT §8.1](model-trainer.md#81-the-model-set)), but anomalyd cannot report its version.
  - Logs are slog text at Info, hard-wired in `main`.
- **Existing plans.** [X9](future-improvements.md#x9-self-observability-and-health-alerts) plans
  health alerts on sinks, sources and snapshot failures, and per-service ingestion lag. It does
  not list these metrics. This item adds the metrics that X9's rules need; the rules stay in X9.
- **What to build.**
  - `anomalyd_templates_evicted_total{service}` from `Miner.Evicted`, and
    `anomalyd_templates_created_total{service}` from `newTemplateLocked`.
  - `anomalyd_snapshot_duration_seconds` and `anomalyd_snapshot_failures_total` (with G5), and
    `anomalyd_backfill_status{state}` (not started, running, done, failed).
  - `anomalyd_build_info{version,revision,goversion} 1` on the server and agent `/metrics`, and
    an `anomalyd version` subcommand. Until G29 sets `-X main.version`, the version is `dev`;
    the revision comes from `runtime/debug.ReadBuildInfo` when the build has VCS information.
  - `--log-format=text|json` and `--log-level=debug|info|warn|error` for every subcommand.
  - Not `anomalyd_template_info`: template text as a label is a cardinality hazard.
- **Batch 1 defers.** A counter for native-histogram samples that `promrw` skips (a residue from
  [Considered and rejected](#considered-and-rejected)); add it when G12 touches `promrw`. The
  version string itself (G29).
- **Depends on** nothing. G29 fills in the version.
- **Done when.**
  - A test parses `/metrics` and finds every new family; `promtool check metrics` passes on the
    output.
  - With `--drain-max-templates=2` and three templates that do not merge, the evicted counter is
    1 and the created counter is 3.
  - `--log-format=json` output parses as JSON, line by line. `--log-level=warn` hides Info lines.
  - `anomalyd version` prints the version, the revision and the Go version.
- **Effort** S. **Value** medium.

### G12. Fuzz targets for network decoders and kubeconform in CI

- **What it is.** Go fuzz tests for the two parsers that read untrusted network input, and
  manifest validation in CI.
- **Why it matters here.** `promrw` is a hand-written protobuf parser behind snappy, and
  `handlePush` decodes gzip JSON from any agent. Both face the network, and N10's auth does not
  exist yet. The DaemonSet manifest says it was validated by hand. There are no `Fuzz*` tests
  today.
- **Existing plans.** [X10](future-improvements.md#x10-test-and-benchmark-suite) plans fuzzing
  for `logparse`, multi-line joining and `drain.Tokenizer`, plus Loghub golden tests, benchmark
  gates, and soak and chaos tests. It does not cover the network decoders or the manifests. This
  item adds those two; the rest stays in X10.
- **What to build.**
  - `FuzzDecode` in `internal/promrw`: `Decompress` with the size limit, then `Decode`. The seed
    corpus comes from `Encoder.Encode` output.
  - `FuzzHandlePush` in `internal/server`, through `httptest`, with plain and gzip bodies. It
    aims at the `globalTemplate` and `strings.Fields` path. Invariants: no panic, status 204 or
    400, store sizes within their caps.
  - `kubeconform -strict anomalyd/deploy` in the CI configs job.
  - A longer `-fuzz` run per target in the weekly e2e workflow. Crashers are committed under
    `testdata/fuzz` as regression seeds.
- **Batch 1 defers.** A server-restart end-to-end test. It goes into the X10 soak test; restore
  is already unit-tested.
- **Depends on** nothing. Write `FuzzHandlePush` after G6, so it covers the step check.
- **Done when.**
  - `go test ./...` runs both seed corpora on every PR.
  - A 10-minute `-fuzz` run per target finds no crash.
  - The kubeconform step fails on a deliberately broken manifest (checked once in the PR) and
    passes on the real one.
- **Effort** S. **Value** medium.
- **Sources.** https://go.dev/doc/security/fuzz/

### G13. Absent metric series instead of resolve-on-gap

- **What it is.** When one metric series stops arriving while others still arrive, report it as
  absent, instead of resolving its finding as if it were healthy.
- **Why it matters here.** In `evalSeries`, 3 missing buckets resolve a firing finding: the same
  event as a recovery. `gc` later deletes the series. A service whose exporter died, or a
  recording rule that broke, then looks healthy.
- **Existing plans.** RefArch [T0](../reference-architecture.md#what-each-tier-does) lists
  heartbeat and flatline rules, and
  [X9](future-improvements.md#x9-self-observability-and-health-alerts) watches anomalyd's own
  inputs (a remote-write stall). Neither covers one series going missing inside anomalyd. This
  item adds that.
- **What to build.**
  - A `detect` helper that returns the seasonal expected value for a bucket with no observation
    (today this is the `expectedAt` closure inside `detect.Score`).
  - In `evalSeries`, for metric series only: a bucket counts as absent when it is missing, the
    prom clock (`promClock`) has moved on (other series still arrive), and the expected value is
    above `--absent-floor`. After `--absent-buckets` (default 3) such buckets, fire
    `AnomalydSeriesAbsent{service,metric}`, tier 2, severity `warning`.
  - A firing finding on that series then resolves with the annotation
    `resolved_reason="absent"`, not as a plain recovery.
  - `--absent-ttl` (for example 24 h) resolves the absent finding for a decommissioned series.
    After that, `gc` drops the series as today.
  - A gauge `anomalyd_series_absent`. If more than a set share of metric series go absent in one
    bucket, fire nothing per series and count it instead: that is an input outage, which X9
    covers.
- **Batch 1 defers.**
  - Absence of periodic log templates. It is lower value; watch batch jobs with
    `kube_cronjob_status_last_successful_time` instead.
  - The interim PromQL rule
    `count by (service)(anomaly:svc:requests:rate5m offset 1w) unless count by (service)(anomaly:svc:requests:rate5m)`.
    It is needed only where anomalyd does not run.
- **Depends on** nothing. G4 later makes the missing-bucket count a flag.
- **Done when.**
  - A test: a firing metric finding whose series stops while another series continues does not
    resolve as healthy, and `AnomalydSeriesAbsent` fires after 3 buckets.
  - When all series stop (the prom clock stands still), nothing fires.
  - When the series comes back, the absent finding resolves. After `--absent-ttl` it resolves on
    its own.
  - A series whose expected value is below `--absent-floor` never fires.
- **Effort** M for the whole item; batch 1 builds the metric-series part, which is S.
  **Value** medium.
- **Sources.** https://github.com/grafana/promql-anomaly-detection ·
  https://www.elastic.co/docs/reference/machine-learning/ml-count-functions

## Batch 2: small, next

Nine small items. Like batch 1, they need no N item.

**Order inside batch 2.** By the ratings, the four medium items (G7–G10) come before the five low
ones (G28–G32). Within that, dependencies decide:

1. G8 first. It changes the wire format and the series keys; G9 matches on namespace and G28
   keys its quotas by it. The earlier it lands, the less state there is to migrate.
2. G7, G9, G10.
3. G29 and G30, then G31 (it calls G30's endpoint), then G28.
4. G32 any time: it is a rules file. Two PromQL rules from
   [Fixes to existing plans](#fixes-to-existing-plans) can ride with it: the missing
   `predict_linear` T0 rules (G24) and the `PodLatencyPeerOutlier` rule (G19's metrics half).

Move G29 to the front if the trainer starts before batch 2 ends: the model set gates on the
anomalyd version.

### G7. Time-zone and DST-aware seasonal lags

- **What it is.** Seasonal lags that follow local time, so "same time last week" is still the
  same local hour after a DST switch.
- **Why it matters here.** `detect.Score` takes lags as `b - k*Season` in UTC buckets, and
  `poc/ml/prom_anomaly_job.py` uses the same offsets. With a 168 h season and 2 seasons, traffic
  ramps that follow local business hours are one hour off for two weeks after each switch, and
  the residual MAD inflates too.
- **Existing plans.** None. The only time zone in the plans is the trainer CronJob's `Etc/UTC`
  ([MT §11](model-trainer.md#11-scheduling)).
  [X5](future-improvements.md#x5-multi-season-baselines-and-calendars)'s calendars have no time
  zone either (see [Fixes](#fixes-to-existing-plans)).
- **What to build.**
  - `--tz` (an IANA name, default `UTC`), plus a per-service override in X11 later.
  - A table of DST transitions over the ring's span, computed at start-up. `detect.Params` gets a
    lag function that shifts a lag by ±1 h across a transition. The rings stay in UTC, and the
    hot path stays O(1).
  - The fixed-offset path stays the default, so the Python parity test still holds.
  - A README note: upstream PromQL `offset 1w` bands keep the problem.
- **Depends on** nothing. Build it before or with X5, because holiday dates need the same
  local-time mapping.
- **Done when.**
  - Table tests for one `Europe/*` and one `America/*` zone, including the skipped and the
    repeated hour.
  - On a synthetic series with a local-time daily ramp across a switch, the expected value
    follows the ramp at the same local hour; with `--tz=UTC` it is one hour off.
  - The parity test passes unchanged with the default.
- **Effort** S. **Value** medium.
- **Sources.** https://docs.datadoghq.com/monitors/guide/how-to-update-anomaly-monitor-timezone/ ·
  https://docs.datadoghq.com/monitors/types/anomaly/ ·
  https://docs.aws.amazon.com/AmazonCloudWatch/latest/APIReference/API_AnomalyDetectorConfiguration.html

### G8. Namespace dimension on log series

- **What it is.** Carry the Kubernetes namespace on log series and log findings.
- **Why it matters here.** `wire.Item` and the log series keys built in `addLog` carry only
  service, template id and level. So `checkout` in two namespaces merges into one series and one
  template set. X6 and L7 group or split by namespace, but nothing carries it.
- **Existing plans.** [X1](future-improvements.md#x1-kubernetes-metadata-for-service-identity)
  uses pod metadata only to choose the service name.
  [X6](future-improvements.md#x6-findings-grouping-into-incident-candidates) groups "with X1, the
  same namespace", and [L7](future-improvements.md#l7-multi-tenancy-and-per-team-caps) derives a
  tenant from the namespace. This item adds the field they assume.
- **What to build.**
  - `namespace` (omitempty) on `wire.Item`. For container sources the agent takes it from the
    file name under `/var/log/containers` (`<pod>_<namespace>_<container>-<id>.log`); X1
    replaces this later. Raw files have none, unless the N1 source config sets one.
  - The server's miner-pool key, the series labels and the finding labels include `namespace`
    when it is set.
  - The cluster comes from DP §15 N5's planned `--alert-label`, not from the agent.
  - Never put the node into series keys.
  - Migration: switching it on re-keys every log series. To avoid a cold start and a burst of
    "new template" findings, the first namespaced push for a service takes over that service's
    old entry (through X14's migration hook, or a one-off step in `load` if X14 is not built).
- **Depends on** G5. Old agents without the field keep today's keys.
- **Done when.**
  - Pushes for `checkout` from two namespaces give two template sets and two volume series, and
    their findings carry `namespace`.
  - Pushes without `namespace` behave exactly as today; the existing tests pass unchanged.
  - A snapshot written before the change loads after it, with no burst of novelty findings.
- **Effort** S. **Value** medium.

### G9. Owner and runbook enrichment from a service catalog

- **What it is.** A small file that maps services to team, owner, runbook and chat channel.
  anomalyd copies these onto its findings.
- **Why it matters here.** research/04 requires `team` on every alert. `newFinding` and
  `evalNovelty` emit only service, template id and level, and the input recording rules use
  `sum by (service)`, which drops `team`. Without an owner, chat findings go nowhere, and X7's
  open question "who labels findings?" has no answer.
- **Existing plans.** None. The `runbook_url` part of the rejected "annotation templates" idea
  lands here.
- **What to build.**
  - `--service-catalog` (JSON or CSV): a service glob or a namespace (G8) maps to `team`,
    `owner`, `runbook_url` and `chat_channel`. The file is reloaded when it changes; a bad file
    is rejected and the old one kept. It moves into X11's hot reload once X11 exists.
  - `team` becomes a label, fixed when the finding fires, so Alertmanager can route on it.
    `owner`, `runbook_url` and `chat_channel` become annotations. All four go into the N6
    document.
  - Metric-only alternative: `* on(service) group_left(team) service_info` in the recording
    rules.
  - Generalise to `--annotation-template` only if a second use appears.
- **Depends on** G8 for namespace matching; globs work without it.
- **Done when.**
  - A finding for a service that matches a catalog glob carries `team` and the three
    annotations. A service with no match carries none and is counted in
    `anomalyd_catalog_unmatched_services`.
  - Editing the file changes new findings after one reload. A malformed file keeps the old
    catalog and increments `anomalyd_catalog_reload_errors_total`.
  - Firing findings keep their labels across a reload.
- **Effort** S. **Value** medium.
- **Sources.** https://docs.keephq.dev/overview/enrichment/mapping ·
  https://docs.robusta.dev/master/playbook-reference/builtin-alert-enrichment.html

### G10. Contrastive template ranking (grew, new, vanished)

- **What it is.** For a service and a time window, rank the templates that grew, are new or
  vanished, with a score that does not favour chatty templates.
- **Why it matters here.** The bundle shows the LLM and the operator which templates changed. A
  raw count delta favours chatty templates, so the noisiest lines win, not the changed ones.
- **Existing plans.** [X13](future-improvements.md#x13-llm-bundle-endpoint) has "top template
  deltas", and the T3 design in research/04 says "top-20 templates by count delta", with no
  scoring method. This item supplies the method; X13 shows the result.
  - Template series are already scored in both directions (`logInfo`), so a vanished template
    with more than about 16 expected lines per bucket already fires "down". The vanished list
    adds value only for low-rate heartbeat templates under `--log-min-count`.
- **What to build.**
  - A new package `internal/rca`. Per template, a Poisson LLR of the observed count against the
    seasonal expected value, normalised by the service's own observed/expected ratio, so a
    traffic surge alone ranks nothing.
  - Three capped lists: grew; new (from `tmplMeta.created`); vanished (expected ≥ k, observed 0).
  - `GET /api/v1/templates/contrast?service=&from=&to=` until X13 exists; X13 then puts the lists
    into the bundle.
  - Later (M): lead/lag Spearman correlation against the finding's series.
- **Depends on** nothing to build. X13 consumes it; N9's `template_hash` gives stable keys once it
  exists.
- **Done when.**
  - A unit test: traffic doubles in a synthetic service and one rare error template goes from 0
    to 5 lines. The error template ranks first, and no template ranks on traffic alone.
  - A heartbeat template with expected 3 and observed 0 appears in "vanished".
  - Each list is capped (default 10), and the endpoint answers within 100 ms for 2,000
    templates.
- **Effort** S. **Value** medium.
- **Sources.** https://doi.org/10.1145/3611643.3616249 · https://arxiv.org/pdf/2407.01710 ·
  https://arxiv.org/abs/2409.20503

### G28. Per-service live-series and template-churn quotas

- **What it is.** Caps per service on live log series and on new templates per hour.
- **Why it matters here.** One churning service can fill the global `--max-log-series` (50,000)
  within a day: LRU-evicted template ids are replaced by new ids, while the old series live until
  `gc`. Then every other service loses new series.
- **Existing plans.** [L7](future-improvements.md#l7-multi-tenancy-and-per-team-caps) plans
  per-tenant caps, after N10 and X11. This item adds per-service caps now; L7 later groups them
  by tenant. The 429 rate-limit half of the original idea was dropped: clients are internal, and
  a 429 to Prometheus only moves the backlog into its WAL.
- **What to build.**
  - `--max-log-series-per-service` (default 2 × `--drain-max-templates`) and
    `--max-new-templates-per-hour`, checked in `addLog` and `globalTemplate` before
    `Store.Update`.
  - `anomalyd_log_series_quota_dropped_total{service,reason}`.
- **Depends on** G8: the quota key is (namespace, service) once G8 lands. It can land before L7
  and X1.
- **Done when.**
  - A test service that creates 10,000 distinct templates stays at its quota, another service
    still creates series, and the drop counter names the service and the reason.
  - With no churn, behaviour is unchanged.
- **Effort** S. **Value** low.

### G29. Release pipeline: version, multi-stage image, tag workflow

- **What it is.** A version set at build time, an image built from source, and a release
  workflow on tags.
- **Why it matters here.** The trainer's model set gates on `anomalyd_min`
  ([MT §8.1](model-trainer.md#81-the-model-set)), but anomalyd cannot report its version. The
  `anomalyd/Dockerfile` copies a prebuilt amd64 binary onto an unpinned distroless tag.
- **Existing plans.** [N10](future-improvements.md#n10-security-baseline) covers govulncheck,
  an SBOM, cosign signatures and a pinned base digest;
  [N11](future-improvements.md#n11-readiness-config-validation-and-packaging) covers the
  manifests. This item adds the version and the build steps that those attach to.
- **What to build.**
  - `var version = "dev"` in `cmd/anomalyd`, set with `-ldflags "-X main.version=<tag>"`.
  - A multi-stage Dockerfile: a Go build stage and the distroless runtime stage, both pinned by
    digest, with `CGO_ENABLED=0` and `-trimpath`.
  - A tag-triggered workflow (`v*`) that builds and pushes with buildx, and later carries N10's
    SBOM and cosign steps.
  - arm64 only if the Step-0 inventory shows arm nodes.
- **Depends on** G11 (`anomalyd_build_info` and `anomalyd version`).
- **Done when.**
  - An image built by the workflow reports the tag in `anomalyd version` and in
    `anomalyd_build_info`.
  - A test tag in a fork builds and pushes an image.
  - `docker build` works from a clean checkout, with no prebuilt binary.
- **Effort** S. **Value** low.

### G30. Series listing and explain detail

- **What it is.** More detail on each finding about why it fired, and an endpoint that lists
  series and their state.
- **Why it matters here.** An operator cannot see why a finding fired, or why one did not.
  `detect.Score` computes the scale, the raw MAD and which floor won, but the finding JSON drops
  them, and the pending, normal and missing counters stay internal.
- **Existing plans.** [MT §9.5](model-trainer.md#95-new-endpoints-flags-and-subcommands-design)
  plans `GET /api/v1/model/explain?series=…` (which rules matched, the final settings). X13 plans
  the bundle. This item adds the score internals and a listing. When MT lands, merge the two so
  there is one explain endpoint.
- **What to build.**
  - Finding JSON gets `scale`, `mad_scale`, the winning `floor` (`abs`, `rel`, `poisson` or none)
    and the `pending`, `normal` and `missing` counters.
  - `GET /api/v1/series?match=&kind=&service=` with a result cap (default 500): labels, last
    score, expected value, band and the counters.
- **Depends on** nothing.
- **Done when.**
  - A test finding on a flat series shows the absolute floor as the winner.
  - `/api/v1/series?service=x` returns at most the cap, with the counters.
- **Effort** S. **Value** low.

### G31. HolmesGPT toolset for the anomalyd API

- **What it is.** A toolset file so that HolmesGPT (I2.4) can call the anomalyd API during an
  investigation.
- **Why it matters here.** HolmesGPT already sees Alertmanager annotations, the scraped
  `anomalyd_*` gauges and the N6 index. It cannot ask anomalyd for one finding, for a service's
  templates, or later for a bundle.
- **Existing plans.** None. Roadmap I2.4 runs HolmesGPT on demand. The scope is smaller than the
  original "MCP server" idea: a built-in `/mcp` endpoint would add surface to a binary that
  design principle 6 wants small.
- **What to build.** `anomalyd/deploy/holmesgpt/anomalyd-toolset.yaml`: a custom toolset over
  `/api/v1/findings?id=`, `/api/v1/templates?service=`, `/api/v1/series` (G30),
  `/api/v1/agents` (G6), and later X13's `/api/v1/bundles`, with N10's read token once it
  exists.
- **Depends on** G30 and G6 for two of the tools; N10 for auth.
- **Done when.**
  - HolmesGPT loads the toolset without errors.
  - In a local run against the e2e stack, an investigation of a synthetic finding calls the
    findings and templates tools and quotes the template text in its answer.
- **Effort** S. **Value** low.
- **Sources.** https://docs.robusta.dev/master/configuration/holmesgpt/custom_toolsets.html ·
  https://holmesgpt.dev/latest/data-sources/remote-mcp-servers/ ·
  https://coroot.com/agentic-observability ·
  https://coroot.com/blog/we-built-ai-powered-root-cause-analysis-that-actually-works/ ·
  https://docs.datadoghq.com/bits_ai/bits_ai_sre/ ·
  https://www.anthropic.com/engineering/writing-tools-for-agents ·
  https://modelcontextprotocol.io/specification

### G32. Metric label-value novelty

- **What it is.** A PromQL rule that fires when a label value appears that was not seen in the
  last 7 days, for example a new status code, route or error type.
- **Why it matters here.** anomalyd's novelty logic covers log templates only. A new
  `status_code="507"` on a service's recording rule goes unnoticed if the totals stay in band.
- **Existing plans.** [L3](future-improvements.md#l3-trace-structure-novelty) covers new
  service-graph edges and error span names only. This item adds a T1 rule; no anomalyd code.
- **What to build.** A rule of the form `<series> unless max_over_time(<series>[7d] offset 15m)`
  in `poc/prometheus/anomaly-alerts.yml`, on recording rules that keep the label, with a promtool
  test. Generalise anomalyd's novelty code only if the rule proves useful.
- **Depends on** nothing.
- **Done when.** A promtool test: a new label value fires; a value last seen 3 days ago does not.
- **Effort** S. **Value** low.

## Next: medium

Thirteen medium items. Several need N or X items first (N5, N6, N8, X1, X2, X3, X6, X13).

**Order inside Next.** G3 and G4 are the two high-value items. G4 goes first: it bears directly
on the precision and volume targets of the shadow phase, and it needs no other item. G3 is high
value but lives outside anomalyd and needs N5's webhook sink or DP §5.3's findings-shipper. After
those, dependencies decide:

1. G14 (needs G5's persistence) and G17 (inside the X2/X3 work).
2. G18, which uses data anomalyd already receives.
3. The evidence group for X13: G16, then G15 and G20.
4. G21 (needs G4's episodes), G22 (phase 1 shares G1's table), G23, G19, G24.

G19's metrics half and G24's `predict_linear` rules are small rules files; they can ride with G32
in batch 2.

### G3. Log rate analysis on Elasticsearch Basic with `significant_terms`

- **What it is.** For each log finding (template rate, level volume, error ratio), one bounded
  aggregation on the raw Filebeat indices returns the field:value pairs that are more common in
  the finding window than in a baseline window. It rebuilds Kibana's Platinum log rate analysis,
  and Datadog's error outliers, from core aggregations.
- **Why it matters here.** By design only counts leave the host, so anomalyd can never say which
  node, pod or image tag drove an error burst. One query per finding, bounded to one service and
  a few minutes, is cheap even at 2 TB/day. It is probably the cheapest per-finding RCA the stack
  can get.
- **Existing plans.** None. research/01 marks Kibana's log rate analysis as Platinum and calls
  `significant_terms` "building blocks for our own log rate analysis", but no item builds it.
  [DP §8.3](deployment.md#83-from-an-anomalyd-finding-to-the-raw-lines) builds only a Discover
  link from service, time and level.
- **What to build.**
  - **Placement:** outside anomalyd, because the deployment plan keeps ES clients and
    credentials out of it. It runs in the triage service (`triage-svc`) or the X13 bundle
    builder, or in a small sidecar that reads the N5 webhook sink or polls `/api/v1/findings`
    like the [findings-shipper](deployment.md#53-findings-shipper-s4).
  - **Foreground:** `service.name`; plus `log.level` for level findings; plus a `match_phrase`
    on the template's constant tokens for template findings. The match is approximate, because
    Drain ids and ES keys differ ([DP §4.2](deployment.md#42-where-the-template-key-is-computed)).
    Window: `[bucket − step, bucket + 2·step]`.
  - **Background:** the same filter one day or one week earlier, or the previous hour.
  - **Fields:** `kubernetes.node.name`, `kubernetes.pod.name`, `container.image.name`,
    `host.name`, plus `http.response.status_code` and `url.path` where apps log them as keywords.
    `add_kubernetes_metadata` is already in [DP §5.1](deployment.md#51-filebeat-stdout-s1).
  - **Heuristics:** JLH, chi_square, gnd, mutual_information and percentage are core and also
    work on OSS 7.10. Only `p_value` needs Basic.
  - **Portable variant:** if `background_filter` is slow with many terms, use two bounded
    `terms` aggregations (foreground and background) and score them in the client.
  - **Output:** the top 3–5 pairs with their foreground and background shares, plus a Discover
    link, into the N6 findings document and the X13 bundle. Do not re-post to Alertmanager to add
    annotations: anomalyd's own resends of the same label set would overwrite them.
  - **Guards:** a timeout per query (for example 5 s), a daily query cap, only tier-2 or
    error-level findings, a cache per (service, window), and fail open.
- **Depends on** N5's webhook sink or DP §5.3's findings-shipper as the trigger; N6 and X13 for
  the output. Decide N6 against the findings-shipper first ([Fixes](#fixes-to-existing-plans)).
- **Done when.**
  - In a test ES with a synthetic error burst from one pod, the top pair for the matching finding
    is that pod's `kubernetes.pod.name`.
  - Measured in Step 0 on a production-size index, each query stays under its timeout. The daily
    cap stops queries when reached, and this is counted.
  - With ES down, findings still reach Alertmanager on time (an X10 chaos test).
- **Effort** M. **Value** high.
- **Sources.** https://www.elastic.co/blog/elastic-stack-aiops-labs-8-12-ga-of-log-rate-analysis ·
  https://www.elastic.co/docs/reference/aggregations/search-aggregations-bucket-significantterms-aggregation ·
  https://docs.datadoghq.com/logs/explorer/watchdog_insights/

### G4. Finding lifecycle: k-of-n, hysteresis, keep-firing, flap damping

- **What it is.** Rules for firing and resolving that tolerate noise near the threshold: k-of-n
  firing, a lower resolve threshold (hysteresis), several quiet buckets before resolving
  (keep-firing), a flap counter per series, and flags for the constants that are hard-coded
  today.
- **Why it matters here.** The shadow phase targets precision of at least 50 % and 30 % and at
  most 5 findings per service per day
  ([DP §14](deployment.md#phase-2-anomalyd-in-shadow-mode-24-weeks)). Today, in `evalSeries`:
  - one normal bucket resets `pending`, so a moderate slow burn may never reach `--for`;
  - 2 normal buckets resolve, and 3 missing buckets resolve;
  - `maxCatchUp` is 3, and the Alertmanager hold in `sendAlerts` is `max(4 × eval interval, 2 min)`.

  A series that hovers near the threshold fires, resolves and fires again. Each re-fire is a new
  notification after `group_interval`, and each counts against the budgets. The finding id does
  not change (`readableID` is deterministic), but `StartsAt` does, and X7 labels keyed by id
  become ambiguous across episodes.
- **Existing plans.** [X11](future-improvements.md#x11-per-service-overrides-and-hot-reload) and
  [MT §6.1](model-trainer.md#61-family-1-what-exactly-gets-tuned) tune only `for_buckets`.
  [X4](future-improvements.md#x4-level-shift-detector-and-deploy-awareness)'s CUSUM targets level
  shifts, not the firing logic. [MT §7.1](model-trainer.md#71-backtesting)'s backtester
  hard-codes "resolve after 2 normal buckets". This item adds the state machine; X11 and the
  model set then carry its fields.
- **What to build.** All of it is in `evalSeries` and `seriesInfo`.
  - **k-of-n:** a `uint16` bitmask of the last n buckets; fire when at least k bits are set.
    Flags `--fire-k` and `--fire-n`; the default k = n = `--for` keeps today's behaviour.
  - **Hysteresis:** `--resolve-threshold-ratio` (for example 0.6). Resolve only when |z| is below
    ratio × threshold.
  - **Keep-firing:** `--resolve-buckets` replaces the hard-coded 2. Also expose
    `--missing-resolve-buckets` (3), `--max-catch-up` (3) and `--am-hold`. They move into X11 and
    the model set later.
  - **Flap damping:** a small ring of fire times per series. At 3 or more cycles in 6 h, extend
    the quiet period instead of resolving, so one finding stays open with the annotation
    `flapping=true`. Labels never change while the finding fires; a severity downgrade applies
    only when the next episode fires. Metric `anomalyd_finding_flaps_total{kind}`.
  - **Episode key:** `episode` (the bucket of `StartsAt`) in the finding JSON, so X7 labels
    attach to one episode.
  - **Parity:** mirror the state machine in the trainer's Python backtester (MT §7.1), or parity
    breaks. The N8 report adds "notifications per incident".
- **Depends on** nothing to build. MT §7.1 must follow it; X11 carries the flags later. After
  G13, a gap no longer resolves a metric finding as healthy; `--missing-resolve-buckets` then
  applies to log series and to the absent path.
- **Done when.**
  - With default flags, the existing tests pass and an N8 replay (or the e2e data, if N8 is not
    ready) produces exactly today's findings.
  - A series that alternates 3 anomalous and 3 normal buckets for 6 h gives one finding with
    `flapping=true` under damping, where today it gives one per cycle.
  - A series anomalous in every other bucket for 2 h fires with `--fire-k=3 --fire-n=5` and never
    fires with the defaults.
  - With non-default settings, the Python backtester and the Go replay agree within 1 finding per
    1,000 (the MT §7.1 rule).
- **Effort** M. **Value** high.
- **Sources.** https://www.elastic.co/docs/explore-analyze/machine-learning/anomaly-detection/ml-ad-explain ·
  https://docs.datadoghq.com/monitors/types/anomaly/ ·
  https://docs.splunk.com/Documentation/ITSI/latest/SI/AD ·
  https://prometheus.io/docs/prometheus/3.9/configuration/alerting_rules/ ·
  https://2020.icse-conferences.org/details/icse-2020-Software-Engineering-in-Practice/24/Understanding-and-Handling-Alert-Storm-for-Online-Service-Systems ·
  https://arxiv.org/abs/2308.12925

### G14. Contamination-resistant seasonal baselines

- **What it is.** Keep known-bad buckets out of the seasonal median.
- **Why it matters here.** With `--prom-seasons 2` (the default, which Prometheus' 15-day
  retention forces for backfill), the median of two points is their mean. So half of last week's
  incident lands in this week's expected value, and a repeat at the same slot fires late or not
  at all. Logs default to 3 seasons, so the problem is mainly metrics.
- **Existing plans.** [X5](future-improvements.md#x5-multi-season-baselines-and-calendars) adds
  maintenance and holiday calendars. [MT §6.2](model-trainer.md#62-family-2-robust-statistics-add-ons)
  adds a calendar of days to skip and an offline hour-of-week profile.
  [X7](future-improvements.md#x7-feedback-loop-labels-suppressions-and-per-rule-precision) labels
  are used only to measure precision. None marks bad buckets during live operation. This item
  adds that.
- **What to build.**
  - A 1-bit "excluded" flag per ring bucket in `series.Series`. It is set while `info.firing`,
    and through `POST /api/v1/baseline/exclude {selector, from, to}`; later X7 labels feed it too.
  - `expectedAt` skips flagged lags and falls back to the remaining lags, or to the lookback
    median.
  - The flags are saved in the snapshot (with G5 and X14).
  - "Revert the baseline to time T" is the same call, from T to now.
  - The ±w neighbourhood median is a separate, riskier change: A/B-test it in N8 only.
- **Depends on** G5 and X14 for persistence; X7 later.
- **Done when.**
  - Synthetic test: an incident fires in week 1. In week 2 the expected value at that slot equals
    the clean week's value, not the mean with the incident, and a repeat incident fires as fast as
    the first.
  - The exclude call marks only the matching series and time range, and the flags survive a
    restart.
  - Memory per series grows by at most capacity / 8 bytes.
- **Effort** M. **Value** medium.
- **Sources.** https://docs.aws.amazon.com/AmazonCloudWatch/latest/APIReference/API_AnomalyDetectorConfiguration.html ·
  https://www.elastic.co/docs/explore-analyze/machine-learning/anomaly-detection/ml-ad-run-jobs ·
  https://discuss.elastic.co/t/custom-rules-parameter/383337 ·
  https://grafana.com/docs/grafana-cloud/machine-learning/machine-learning/forecasting/holidays-and-seasonality/ ·
  https://community.dynatrace.com/t5/Alerting/Auto-adaptive-threshold-Seasonal-baseline-Metric-event-with/m-p/257114 ·
  https://docs.datadoghq.com/monitors/types/anomaly/ ·
  https://cran.r-project.org/package=surveillance · https://arxiv.org/pdf/2007.14254

### G15. Dimension attribution (which label slice explains it)

- **What it is.** When a metric finding fires, find the label slice (route, status code,
  operation) that explains most of the change.
- **Why it matters here.** Findings fire on service-level recording rules ("reduce before you
  detect"), which drop the labels that would explain them. Today the operator opens Grafana and
  slices by hand.
- **Existing plans.** [X3](future-improvements.md#x3-template-mix-and-error-ratio-detectors-per-service)
  lists contributing templates, but only for its own divergence finding.
  [X13](future-improvements.md#x13-llm-bundle-endpoint) has "top template deltas". Nothing covers
  metric slices.
- **What to build.** In `internal/rca/slices.go`:
  - A config that maps each `anomaly_name` to a raw PromQL expression and its allowed dimensions,
    for example `svc_error_ratio → traces_span_metrics_calls_total by span_name,status_code`.
  - Two bounded `topk` queries per dimension (the anomalous window, and one season earlier)
    through the existing `queryRange` client.
  - Adtributor-style ranking by share of the delta: single dimensions first, pairs only under a
    cap.
  - A `top_slice` annotation (on the next resend) and a bundle section.
  - Risk: span metrics collapse to `service.name` (research/03), so expect route, status and
    operation slices, not pod or version slices. On the log side the gap is smaller, because
    template findings fire together.
- **Depends on** X13 for the bundle; X1 or G8 for namespace filters.
- **Done when.**
  - With errors injected on one `span_name`, that `span_name` ranks first with at least 80 % of
    the delta.
  - A finding causes at most 2 queries per dimension, each with a timeout.
  - With Prometheus down, the finding is sent on time and an error counter increases.
- **Effort** M. **Value** medium.
- **Sources.** https://docs.datadoghq.com/dashboards/graph_insights/watchdog_explains/ ·
  https://www.elastic.co/docs/explore-analyze/machine-learning/anomaly-detection/ml-ad-view-results ·
  https://www.linkedin.com/blog/engineering/analytics/analyzing-anomalies-with-thirdeye ·
  https://www.uber.com/blog/uvitals-an-anomaly-detection-alerting-system/ ·
  https://www.microsoft.com/en-us/research/wp-content/uploads/2016/07/ICSE-2016-1-iDice-Problem-Identification-for-Emerging-Issues.pdf ·
  https://arxiv.org/abs/2205.10004 · https://arxiv.org/abs/2305.03331 ·
  https://www.usenix.org/conference/nsdi14/technical-sessions/presentation/bhagwan ·
  https://github.com/NetManAIOps/PSqueeze · https://arxiv.org/pdf/2301.13572

### G16. Onset-anchored related-series ranking (BARO)

- **What it is.** Estimate when the incident began, then rank every series anomalyd holds by how
  much it moved after that time, including moves below the finding threshold.
- **Why it matters here.** The bundle should carry "5–10 related RED/USE series" (research/04),
  but nothing selects them, and a series that moved just below the threshold never fires.
- **Existing plans.** [X6](future-improvements.md#x6-findings-grouping-into-incident-candidates)
  orders only findings that fired. X13 and RefArch T3 want related series but do not say how to
  choose them. This item supplies the selection.
- **What to build.** In `internal/rca`:
  - Onset: X4's CUSUM, or until X4 exists, the first bucket with |z| ≥ 2.
  - A BARO score over every in-memory series, `rho = max over t' ≥ t of |x − median_pre| / IQR_pre`.
    It is cheap: tens of buckets × about 35 k series.
  - Roll-up to services, `GET /api/v1/related?finding=` and `GET /api/v1/rca?service=&from=`, and
    a "ranked causes" section in X13.
  - AC@1 and AC@3 (the true cause is first, or in the top 3) on N8 replays.
  - Limits: without topology (L3) it ranks globally. Published RCAEval numbers come from
    testbeds, not production.
- **Depends on** X13 and N8; X4 is optional.
- **Done when.**
  - AC@1 and AC@3 are reported on the N8 replay of the labelled incidents.
  - On the synthetic e2e data with an injected upstream fault, the upstream service ranks in the
    top 3.
  - One call over 35 k series takes under 1 s, off the evaluation goroutine.
- **Effort** M. **Value** medium.
- **Sources.** https://learn.netdata.cloud/docs/netdata-ai/troubleshooting/metric-correlations ·
  https://learn.netdata.cloud/docs/netdata-ai/troubleshooting/anomaly-advisor ·
  https://grafana.com/docs/grafana/latest/visualizations/simplified-exploration/logs/patterns/ ·
  https://docs.datadoghq.com/logs/explorer/watchdog_insights/ ·
  https://arxiv.org/html/2412.17015v1 · https://arxiv.org/pdf/2405.09330 ·
  https://arxiv.org/html/2408.13729v1 · https://pypi.org/project/fse-baro/

### G17. Count model with traffic exposure

- **What it is.** Score each template's share of its service's lines, or suppress template-rate
  findings that the service's own volume change explains.
- **Why it matters here.** A 2× traffic surge can flag many templates at once. They are info
  severity and grouped by service, but they still count against the 5-per-day budget.
- **Existing plans.** [X2](future-improvements.md#x2-count-detectors-that-fit-log-data) adds
  Poisson and negative-binomial tails on counts.
  [X3](future-improvements.md#x3-template-mix-and-error-ratio-detectors-per-service)'s
  Jensen–Shannon divergence scores template shares, but only as one service-level finding.
  [MT §6](model-trainer.md#6-model-families-ranked-by-cost-and-benefit) tunes the existing
  scorer. None scores the share per template. This item adds it, inside the X2/X3 work. Score
  smoothing overlaps with G4, so it goes there.
- **What to build.**
  - Per template series, a rate ratio, (observed / expected of the template) ÷ (observed /
    expected of the service), scored with X2's tail; or a suppression rule that drops
    `AnomalydLogTemplateRate` when the service volume moved the same way and the template's
    ratio is within the service's band.
  - A/B the two in N8.
  - Defer Gamma-Poisson pooling and hurdle models until N8 shows rare-template errors that X2
    plus `min_count` cannot fix.
- **Depends on** X2, X3 and N8.
- **Done when.** In an N8 replay with a 2× traffic surge and no incident, template findings drop
  by at least 80 % against today, and recall on the incident log does not fall.
- **Effort** M. **Value** medium.
- **Sources.** https://arxiv.org/abs/2308.12925 ·
  https://cran.r-project.org/web/packages/sparsesurv/refman/sparsesurv.html ·
  https://image-ppubs.uspto.gov/dirsearch-public/print/downloadPdf/11757931

### G18. SLO-burn impact gate and miss counter

- **What it is.** When a finding fires, look up the SLO burn for its service and mark whether
  users are affected. Also count SLO pages that no finding came before.
- **Why it matters here.** Today a finding with no user impact goes to chat with the same weight
  as one during an SLO burn. And nobody measures misses: pages that anomalyd did not see first.
- **Existing plans.** `slo:*` series are already remote-written to anomalyd
  ([DP §6](deployment.md#6-anomalyd-configuration)).
  [MT §5.1](model-trainer.md#51-weak-labels-from-incidents-and-alerts) uses SLO alerts as weak
  labels offline. [DP §14](deployment.md#14-rollout-phases) phase 4 links findings into pages.
  [L4](future-improvements.md#l4-per-service-health-score) plans a health score. None gates or
  counts at run time. This item adds both.
- **What to build.**
  - When a finding fires, read `slo:sli_error:ratio_rate1h` and the burn series from the
    in-memory store. Add the annotation `slo_burn_1h` and the label `slo_impact=true|false`,
    fixed at fire time.
  - Routing: `false` goes to Kibana only, `true` goes to chat, and nothing goes to a page.
  - `anomalyd_slo_pages_without_finding_total`, fed by an Alertmanager webhook or by
    remote-writing `ALERTS{severity="page"}`.
  - A config-only interim: an `SLOQuiet{service}` alert as an inhibition source. It covers only
    the top 10–20 SLO services.
- **Depends on** DP §6's remote write of `slo:*`; N5's webhook sink for the page counter.
- **Done when.**
  - A finding for a service with a 1 h burn above 1 gets `slo_impact=true`. A service with no SLO
    series gets no `slo_impact` label.
  - A remote-written page with no finding for that service in the 30 minutes before increments
    the counter.
  - An amtool test routes `slo_impact="false"` to the Kibana-only receiver.
- **Effort** M. **Value** medium.
- **Sources.** https://sre.google/workbook/alerting-on-slos/ · https://github.com/slok/sloth ·
  https://www.dynatrace.com/news/blog/slo-monitoring-alerting-on-slos-error-budget-burn-rates/

### G19. Peer-group outlier detector

- **What it is.** Flag the one pod or node that behaves unlike its peers in the same service.
- **Why it matters here.** A bad node or a bad pod hides in service-level sums. The agent sets
  `wire.Push.Agent`, but the server reads it only in a test, and `addLog` keys by service,
  template and level.
- **Existing plans.** None. research/02 has a promtool-verified `PodLatencyPeerOutlier` rule
  that is not in `poc/prometheus/anomaly-alerts.yml` (see [Fixes](#fixes-to-existing-plans)).
- **What to build.**
  - **Metrics half (S).** Ship that rule: median plus MAD across pods, at least 3 pods, and a
    minimum absolute deviation guard (MAD is 0 when all peers are equal). Do not remote-write
    per-pod series to anomalyd: that breaks the pre-aggregation premise.
  - **Logs half (M).** A compact count per (service, template, agent) over about 6 buckets, only
    for error and warn templates and the top-K templates per service, kept outside the seasonal
    store. Compare each agent's share of the template with its share of the service's total lines;
    this controls for uneven pod placement. Require at least 3 agents. Fire
    `AnomalydPeerOutlier{service,agent}` after `--for`. The worst case is about 100 MB, so cap it.
- **Depends on** G6's registry for the line shares; N4's `agent_id` for a stable agent key.
- **Done when.**
  - The metrics rule has a promtool test.
  - In a synthetic run with 5 agents where one emits 10× the error lines for one service, only
    that agent fires.
  - With uneven placement (one agent carries 3× the service's traffic, at the same error ratio),
    nothing fires.
  - Memory stays under the cap.
- **Effort** M (the metrics half is S). **Value** medium.
- **Sources.** https://docs.datadoghq.com/monitors/types/outlier/ ·
  https://grafana.com/docs/grafana-cloud/machine-learning/outlier-detection/ ·
  https://docs.splunk.com/Documentation/ITSI/latest/SI/AD ·
  https://lantern.splunk.com/Observability_Use_Cases/Monitor_Business/Understanding_anomaly_detection_in_ITSI ·
  https://www.elastic.co/search-labs/blog/elasticsearch-anomaly-detection-jobs ·
  https://www.usenix.org/conference/srecon24americas/presentation/li ·
  https://www.usenix.net/system/files/fast23-lu.pdf

### G20. Deterministic investigation checks

- **What it is.** A fixed list of named PromQL checks that run when a tier-2 finding fires, with
  the results attached as evidence. Grafana Sift does this, but only on Grafana Cloud.
- **Why it matters here.** The first minutes of most investigations are the same queries:
  restarts, OOM kills, crash loops, throttling, rollouts. A deterministic list runs them in
  seconds, with no LLM cost.
- **Existing plans.** X13 and the RefArch T3 bundle want "related signals". Roadmap I2.4 lets
  HolmesGPT run such queries on demand, with an LLM. This item adds a no-LLM version.
- **What to build.**
  - A config file of named checks with `{{service}}`, `{{namespace}}` and `{{window}}`
    placeholders:
    - `increase(kube_pod_container_status_restarts_total)`
    - `last_terminated_reason="OOMKilled"`
    - `waiting_reason="CrashLoopBackOff"`
    - the CFS throttled ratio
    - node load1 / cores
    - rollout generation and HPA replica changes
    - the namespace 5xx rate
  - It runs when a tier-2 finding or an X6 candidate fires, with a timeout per check, and fails
    open. Results go into annotations and the ES document.
  - Before building: evaluate Robusta's built-in enrichment first.
- **Depends on** X1 or G8 for the namespace and pod mapping; X13 for the bundle.
- **Done when.**
  - In a kind cluster with a pod OOM-killed, the finding for that service carries the
    OOMKilled check result.
  - A check that times out is reported as skipped, and the finding is still sent on time.
- **Effort** M. **Value** medium.
- **Sources.** https://grafana.com/docs/grafana-cloud/alerting-and-irm/machine-learning/sift/analyses/ ·
  https://docs.robusta.dev/master/playbook-reference/builtin-alert-enrichment.html ·
  https://docs.coroot.com/inspections/deployment-tracking ·
  https://coroot.com/blog/we-built-ai-powered-root-cause-analysis-that-actually-works/

### G21. Per-finding priority, shape and lifecycle tags

- **What it is.** A 0–100 priority per finding, a shape class at fire time (spike, step, trend,
  variance), and lifecycle tags (new, ongoing, recurring, escalating).
- **Why it matters here.** All tier-2 findings look alike in chat, so operators cannot tell which
  one to open first.
- **Existing plans.** [X6](future-improvements.md#x6-findings-grouping-into-incident-candidates)
  scores candidates only. [MT §6.3](model-trainer.md#63-family-4-second-stage-confirmers-optional)
  adds a `confirm_score` from a second-stage model. This item adds a priority per finding.
- **What to build.**
  - Priority from peak |z|, duration, member count, level, novelty and the deviation-to-floor
    ratio, renormalised per (service, kind).
  - A shape classifier at fire time, and lifecycle tags. "Recurring" covers the residue of the
    rejected recurring-issue idea; add it only if shadow data shows a need.
  - All as annotations and ES fields. At most one coarse `priority=p1..p4` label, computed once
    per episode (G4). It is not the incident priority P1/P2 of the glossary.
- **Depends on** G4 for episodes; X6.
- **Done when.**
  - Table tests classify synthetic spike, step, trend and variance shapes.
  - Labels never change within an episode (a test).
  - With X7 labels, the top 10 % of findings by priority hold a larger share of "useful" labels
    than the rest.
- **Effort** M. **Value** medium.
- **Sources.** https://www.elastic.co/docs/explore-analyze/machine-learning/anomaly-detection/ml-ad-explain ·
  https://www.elastic.co/docs/reference/query-languages/esql/commands/change-point ·
  https://docs.aws.amazon.com/AmazonCloudWatch/latest/logs/LogsAnomalyDetection.html ·
  https://docs.datadoghq.com/logs/explorer/watchdog_insights/ ·
  https://www.uber.com/blog/uvitals-an-anomaly-detection-alerting-system/

### G22. Semantic template labels

- **What it is.** Tag templates with `indicative` (does the line point to a failure?) and a
  `failure_domain` (for example network, storage, auth, resources, dependency).
- **Why it matters here.** Novelty severity comes from the level only (`severity`), and N9's
  templates index has no labels. research/05 recommends LLM template labelling off the hot path,
  but no item builds it.
- **Existing plans.** None. G1 handles severity; this item adds the category.
- **What to build.**
  - **Phase 1 (S).** A Go keyword table (exhausted, refused, timeout, denied, …) sets
    `indicative` and `failure_domain` annotations on novelty findings. It shares G1's table.
  - **Phase 2 (M).** An external labeller in `triage-svc` reads new templates from N9, applies
    N10 redaction, makes one capped LLM call per `template_hash`, and writes a rules file that
    anomalyd reloads. anomalyd stays free of LLM calls.
- **Depends on** G1; N9 and N10 for phase 2; X11 for the reload.
- **Done when.**
  - Phase 1: table tests, and the N8 report shows the share of novelty findings that get a
    domain.
  - Phase 2: the daily LLM cost stays under the configured cap, and no template text leaves
    without N10 redaction (a test with a seeded secret).
- **Effort** M. **Value** medium.
- **Sources.** https://arxiv.org/abs/2605.22779 · https://arxiv.org/abs/2510.22986 ·
  https://arxiv.org/pdf/2406.07467

### G23. Template byte accounting and log-reduction report

- **What it is.** Bytes per template, and a report of templates that cost a lot of ES storage but
  never help detection.
- **Why it matters here.** `wire.Item` counts lines only, and the agent counts bytes only as a
  total. At 200 GB–2 TB/day, a few chatty debug and info templates can dominate ES ingest. This
  may save more than the project costs, but it is a side benefit, not detection.
- **Existing plans.** None.
- **What to build.**
  - An optional `bytes` per count key on `wire.Item` (omitempty, so old agents still work),
    stored in `tmplMeta` and in the snapshot.
  - `GET /api/v1/templates?sort=bytes`, plus a report: the top templates by bytes per day and
    their share, and debug or info templates never linked to a finding in N days.
  - Exempt error and warn templates, templates used in findings, and G1 watchlist matches.
  - Suggested Filebeat `drop_event` rules built from the constant tokens. They are approximate,
    and only a human applies them.
  - The agent reads the files independently of Filebeat, so detection still sees the dropped
    templates.
- **Depends on** G1 for the exemptions; N9 for stable report rows.
- **Done when.**
  - Byte totals per service match the agent's total byte counter within 1 %.
  - Old agents without `bytes` still work.
  - The report suggests rules, and none for exempt templates.
- **Effort** M. **Value** medium.
- **Sources.** https://grafana.com/docs/grafana-cloud/adaptive-telemetry/adaptive-logs/introduction/ ·
  https://grafana.com/docs/grafana-cloud/adaptive-telemetry/adaptive-logs/manage-recommendations/

### G24. Seasonal forecast and ForecastBreach

- **What it is.** A seasonal forecast from the rings for an opt-in set of series, and a finding
  when the forecast crosses a capacity limit.
- **Why it matters here.** "Will we hit the limit before Monday?" has no answer today.
  `predict_linear` is marked "Adopt" in RefArch T0, but no rule exists in `poc/prometheus/*.yml`,
  and a straight line misses daily cycles.
- **Existing plans.** RefArch [T0](../reference-architecture.md#what-each-tier-does) plans
  `predict_linear` (not written); roadmap I3 keeps time-series foundation models; the comparison
  matrix lists forecasting options. This item adds a seasonal-naive forecast in anomalyd and the
  missing T0 rules. It sits outside core anomaly detection.
- **What to build.**
  - `forecast(b+h) = median(v(b+h−k·season))` for h up to one season, plus a robust damped trend.
  - Exported for an opt-in subset only (X11), to bound cardinality.
  - `GET /api/v1/forecast?series=&horizon=`: Grafana cannot draw future points from Prometheus,
    so G2's dashboards read this through the Infinity data source.
  - A tier-2 `ForecastBreach` against `capacity:svc:*` rules or static limits.
  - The `predict_linear` T0 rules with promtool tests; they can go early, with G32.
- **Depends on** X11 for the opt-in list; G2 for display.
- **Done when.**
  - On synthetic weekly data with growth, the forecast error (MASE) over 24 h is lower than
    `predict_linear`'s.
  - `ForecastBreach` fires at least 6 h before a synthetic limit crossing.
  - The `predict_linear` rules pass their promtool tests.
- **Effort** M. **Value** medium.
- **Sources.** https://docs.datadoghq.com/monitors/types/forecasts/ ·
  https://grafana.com/docs/grafana-cloud/machine-learning/dynamic-alerting/forecasting/ ·
  https://help.splunk.com/en/splunk-it-service-intelligence/splunk-it-service-intelligence/visualize-and-assess-service-health/5.0/predictive-analytics/use-the-predictive-analytics-dashboard-in-itsi

## Later: large or low value

Eight items. Why each waits:

- G25 is medium value, but it needs the OTel tier 2 in production (I1.3) first.
- G26 and G27 are large (L). G26 also relaxes the "only counts leave the host" rule.
- G33 and G34 are small, but low value and gated: G33 on X12 and the accepted cold-start risk
  F9 ([DP §13](deployment.md#13-ha-and-failure-modes)), G34 on an N8 result.
- G35–G37 are low value until there are enough labelled incidents or N8 reports.

### G25. On-demand trace contrast via Jaeger (TraceRCA)

- **What it is.** Per (service, operation), compare abnormal traces with normal ones, and rank the
  operations that appear more often, or spend more self-time, in the abnormal ones.
- **Why it matters here.** The bundle fetches the top-5 error or slow trace IDs and a
  critical-path summary. Those are examples, not a comparison.
- **Existing plans.** RefArch T3 (the trace part of the bundle); the comparison matrix lists CRISP
  and the Jaeger v2 MCP server as "Later";
  [L3](future-improvements.md#l3-trace-structure-novelty) covers structural novelty. This item
  adds the contrast step in the bundle builder.
- **What to build.**
  - Per (service, operation): abnormal against normal occurrence and self-time change, ranked with
    TraceRCA's support and confidence rule.
  - **Sampling bias:** tail sampling keeps 100 % of error and slow traces but only about 2 % of
    normal ones. Only traces kept by the baseline policy (the `tailsampling.policy` attribute)
    are valid normal samples.
- **Depends on** the OTel tier 2 in production (I1.3) and X13.
- **Done when.**
  - On a synthetic trace set with one slow downstream operation, that operation ranks first.
  - A test shows that normal samples come only from the baseline policy.
- **Effort** M. **Value** medium.
- **Sources.** https://ieeexplore.ieee.org/abstract/document/9521340 ·
  https://arxiv.org/html/2412.17015v1

### G26. Template parameter tracking

- **What it is.** Track the value mix of a few key slots in a template (status code, error code,
  exception class), so "same template, different status" becomes visible.
- **Why it matters here.** `drain/mask.go` masks status codes and durations as `<NUM>`. A shift
  from 200 to 503 inside one access-log template is invisible.
- **Existing plans.** None. Path A's ElastAlert2 new-error-type rule and the per-source level
  mapping of N1 and N2 cover small parts.
- **What to build.** Start narrow:
  - A top-K (about 8) of raw values, only for allow-listed slots (`status=`, `code=`, the N2
    nginx status position, the exception class), and only for warn, error and access templates.
  - Values pass the N10 redaction masks first. This relaxes "only counts leave the host", so it
    is opt-in.
  - A `Params` field (omitempty) on `wire.Item`.
  - On the server, value-share series under a per-service cap, reusing X3's divergence and the
    novelty logic. Slots are keyed by `template_hash` plus position, and reset when the template
    text changes.
  - Defer DDSketch latency per slot until legacy apps without spans need it.
- **Depends on** N10, N9 and X3.
- **Done when.**
  - In a synthetic access log where the 503 share goes from 0.1 % to 5 % at constant volume, a
    finding fires and names `status=503`.
  - Nothing outside the allow-list leaves the node (a test with a seeded secret in another slot).
- **Effort** L. **Value** medium.
- **Sources.** https://docs.aws.amazon.com/AmazonCloudWatch/latest/logs/LogsAnomalyDetection.html ·
  https://www.datadoghq.com/blog/pattern-inspector/ · https://arxiv.org/abs/2411.15250 ·
  https://arxiv.org/pdf/2409.20503 · https://www.vldb.org/pvldb/vol12/p2195-masson.pdf

### G27. Topology-aware incident grouping with origin and symptom

- **What it is.** Group findings across services that call each other, and mark the likely
  origin.
- **Why it matters here.** One database fault produces findings in every caller. X6 groups by
  service or namespace only, so each caller becomes its own candidate.
- **Existing plans.** [X6](future-improvements.md#x6-findings-grouping-into-incident-candidates)
  groups by service or namespace; [L3](future-improvements.md#l3-trace-structure-novelty) uses
  service-graph edges only for novelty; roadmap I2.5 (Keep OSS) has topology correlation as an
  add-on alternative. This item adds the neighbour lookup and the roles.
- **What to build.**
  - A neighbour lookup in X6's `correlate.go`, from a static dependency file or an hourly query
    of `traces_service_graph_request_total{client,server}`.
  - Merge candidates within 1 hop.
  - The origin is the most-downstream callee with its own error or latency finding; the earliest
    onset (G16) breaks ties.
  - Labels `incident_id` and `incident_role=origin|symptom`.
  - **Risk:** a wrong origin plus inhibition hides the real cause. So inhibit only in chat
    routing, and never drop findings.
  - Defer co-occurrence lift mining and an LLM tie-break until X7 has enough labels.
- **Depends on** X6, G16, and L3 or a dependency file.
- **Done when.**
  - Synthetic: a database fault makes callers A and B fire. The result is one candidate, with the
    database as origin.
  - With no topology data, behaviour equals X6.
- **Effort** L. **Value** medium.
- **Sources.** https://docs.dynatrace.com/docs/dynatrace-intelligence-classic/root-cause-analysis/event-analysis-and-correlation ·
  https://docs.keephq.dev/overview/correlation-topology ·
  https://coroot.com/blog/we-built-ai-powered-root-cause-analysis-that-actually-works/ ·
  https://newrelic.com/platform/applied-intelligence/features ·
  https://grafana.com/docs/grafana-cloud/knowledge-graph/troubleshoot-infra-apps/workbench/ ·
  https://arxiv.org/abs/2403.06485 · https://arxiv.org/pdf/2403.18639 ·
  https://2020.icse-conferences.org/details/icse-2020-Software-Engineering-in-Practice/24/Understanding-and-Handling-Alert-Storm-for-Online-Service-Systems

### G33. ES rollup replay at start-up

- **What it is.** On a cold start, X12's `es_rollup` source replays (seasons + 1) seasons of its
  own rollup rows, so path-A series have a baseline at once.
- **Why it matters here.** After a state loss, series from X12 need a full season of warm-up.
- **Existing plans.** [X12](future-improvements.md#x12-elasticsearch-rollup-as-an-input-source)
  plans a live pseudo-agent only. This item adds the start-up replay.
- **What to build.**
  - A replay with the original timestamps, using N4's `seq` so a re-read is idempotent.
  - Optional: a one-time, throttled composite aggregation (service × level × date histogram) for
    per-level volume.
  - Not feasible: backfilling Drain template series from ES, because the keys differ.
- **Depends on** X12 and N4.
- **Done when.** After a cold start, X12 series score with seasonal history (`seasonal=true`) in
  the first evaluation, and a second replay adds no counts.
- **Effort** S. **Value** low.

### G34. Multi-baseline consensus gate

- **What it is.** An optional k-of-m vote across baselines (weekly lags, same-weekday daily lags,
  a recent window) before a finding fires.
- **Why the value is low.**
  - Netdata's "99 %" figure comes from a k-means ensemble and is not comparable.
  - Log rings hold only daily seasons.
  - A daily-lag vote disagrees every Monday and weekend on weekly-seasonal metrics, so it can add
    false positives.
  - The "bad reference week" motivation is better served by G14.
- **Existing plans.** None. `detect.Score` computes one expected value, and X5 plans one blended
  median.
- **What to build.** An optional gate in `internal/detect`, enabled only if the N8 replay shows a
  gain.
- **Depends on** N8; G14 first.
- **Done when.** An N8 replay shows higher precision at equal recall with the gate. Otherwise it
  is not merged.
- **Effort** S. **Value** low.
- **Sources.** https://learn.netdata.cloud/docs/netdata-ai/anomaly-detection

### G35. Triage evaluation harness

- **What it is.** A way to tell whether LLM triage helps: a grounding check on every LLM answer,
  and a replay that compares the LLM's root-cause hits with the no-LLM ranking of G16.
- **Why it matters here.** Roadmap I2.3 says "evaluate on ≥ 50 past incidents" (research/04),
  with no method.
- **Existing plans.** I2.3, I2.4, [N8](future-improvements.md#n8-replay-and-shadow-evaluation-harness-for-real-logs)
  and [MT §7](model-trainer.md#7-evaluation) evaluate detectors, not triage. This item adds the
  triage method.
- **What to build.**
  - An online grounding check: reject hypotheses whose `evidence_refs` are not in the bundle.
  - A replay script that compares LLM hit@1 and hit@3 with G16's BARO ranking, and records
    tokens, cost and latency.
  - A `root_cause_service` column in `incidents.csv`.
  - Gate automatic triage of P1/P2 incidents on beating the no-LLM ranking.
  - Caveat: with 10–50 incidents the confidence intervals are wide.
- **Depends on** G16, X13, N8 and I2.3.
- **Done when.**
  - The replay prints hit@1 and hit@3 with confidence intervals for both methods on the incident
    log.
  - The grounding check rejects a seeded made-up reference.
- **Effort** M. **Value** low.
- **Sources.** https://proceedings.iclr.cc/paper_files/paper/2025/file/d29b8d53678015079e1d245c023e49d2-Paper-Conference.pdf ·
  https://arxiv.org/abs/2606.27154 · https://proceedings.mlr.press/v267/jha25a.html ·
  https://www.traversal.com/blog/openrca-isnt-root-cause-analysis-why-that-matters ·
  https://www.anthropic.com/engineering/building-effective-agents

### G36. Similar-incident retrieval

- **What it is.** "Seen before?" in the bundle: the past candidates most like this one, with their
  verdicts and notes.
- **Why the value is low.** The plans expect dozens of labelled incidents
  ([MT §1](model-trainer.md#1-goals-and-limits)), not the thousands that RCACopilot had. N6 plus
  N9 already allow manual "seen before?" searches in Kibana.
- **Existing plans.** None.
- **What to build.** After X6, X7, N6 and N9: query past candidates that share (service,
  alertname, `template_hash`), weight them by Jaccard similarity over member keys with time
  decay, and add `similar_to` (top 3 ids, verdicts, notes) to the bundle. No separate index and
  no TF-IDF until there are hundreds of labelled candidates.
- **Depends on** X6, X7, N6 and N9.
- **Done when.** A repeated synthetic incident retrieves its earlier occurrence first.
- **Effort** M. **Value** low.
- **Sources.** https://arxiv.org/html/2305.15778 · https://kiro.dev/blog/trust-agent-triage/

### G37. Offline LLM mask synthesis and merge review

- **What it is.** An offline batch command that asks an LLM to propose masks and template merges
  or splits from N8 reports, validates them, and opens a PR to the N2 masks.
- **Why the value is low.** research/05 says parsing accuracy does not strongly predict detection
  accuracy.
- **Existing plans.** N8 lists templates with likely-variable tokens; N2 adds per-source masks;
  MT §3 extracts training data. Only redacted examples may leave
  ([MT §17](model-trainer.md#17-open-questions-and-risks)). This item adds the proposal and
  review step.
- **What to build.**
  - A batch command over N8 reports, with redacted examples only.
  - RE2 validation: a mask compiles, matches the variable positions and matches no constant
    token.
  - Output: a PR to the N2 masks.
  - A server alias table keyed by N9's `template_hash`; today's alias map in `globalTemplate` is
    keyed by text.
- **Depends on** N8, N2, N9 and N10.
- **Done when.**
  - On a Loghub sample, every proposed mask passes validation or is dropped with a reason.
  - The PR diff touches only mask files.
- **Effort** M. **Value** low.
- **Sources.** https://arxiv.org/abs/2604.20553 · https://arxiv.org/abs/2601.22590 ·
  https://arxiv.org/pdf/2408.01585 · https://arxiv.org/pdf/2310.01796

## Fixes to existing plans

Problems found in the existing plans while checking them against the code. Each needs an edit
to that plan, not new code (except where noted).

| Where | Problem | Fix |
|---|---|---|
| [X14](future-improvements.md#x14-versioned-state-format-and-safe-upgrades) | Says the snapshot has no version. `stateFile` already writes `Version` (`stateVersion = 1`), `Step` and the ring caps, and `load` already skips rings that do not fit, silently. | Rewrite X14 around what is left: a migration per version, novelty muting on an unreadable snapshot. The loud warnings and the lock fix are G5. |
| [N11](future-improvements.md#n11-readiness-config-validation-and-packaging) and [DP §9.1](deployment.md#91-chart-layout-proposed-nothing-of-this-exists-yet) | N11 says Kustomize and "no Helm until there is a second consumer". DP §9.1 proposes a Helm chart. | Decide once, then edit the other document. |
| [N6](future-improvements.md#n6-elasticsearch-findings-sink-and-index-templates) and [DP §5.3](deployment.md#53-findings-shipper-s4) | N6 has anomalyd write to ES with an API key. The deployment plan ships findings through a Filebeat findings-shipper, to keep ES credentials out of anomalyd. | Pick one. If the shipper wins, N6 shrinks to the index templates, and N10's ES role moves to the shipper. G3 and G9 follow the same choice. |
| N6 | Its data-stream templates do not address the legacy OSS 7.10 branch of the roadmap. | Add an OSS 7.10 variant or mark N6 as Basic only; check what the OSS build supports in Step 0. |
| RefArch [T0](../reference-architecture.md#what-each-tier-does) | `predict_linear` rules are marked "Adopt", but none is in `poc/prometheus/*.yml`. | Write them with promtool tests (with G32 in batch 2; see G24). |
| research/02 | The promtool-verified `PodLatencyPeerOutlier` rule is not in `poc/prometheus/anomaly-alerts.yml`. | Add it with its test, and the MAD = 0 guard (G19's metrics half). |
| [DP §9.2](deployment.md#92-key-manifests-rendered-form) and [N10](future-improvements.md#n10-security-baseline) | DP §9.2 gives the agent DaemonSets `priorityClassName: log-readers`, the same class as Filebeat. N10 wants the agent lower, so it is evicted first. | Add a lower class for the agents in DP §9.2. While there, set `GOMEMLIMIT` from the downward API. N11 adds an agent `/-/healthy` that fails when `pollAll` stalls, never on push failures (code). |
| [X5](future-improvements.md#x5-multi-season-baselines-and-calendars) | The holiday and maintenance calendars have no time zone. | Give each calendar an IANA zone; build G7 first. |
| X5 | "Scored against the median of past holidays" works only after about one year of archive. | Say so in X5. |
| [MT §7.1](model-trainer.md#71-backtesting) | The backtester hard-codes "resolve after 2 normal buckets". | Use G4's settings, with the same state machine in Python and Go. |
| [X7](future-improvements.md#x7-feedback-loop-labels-suppressions-and-per-rule-precision) | Labels are keyed by the finding id, which is deterministic, so a label is ambiguous across episodes. | Key labels by (id, episode) from G4. |
| [X8](future-improvements.md#x8-score-export-to-elasticsearch-for-band-charts) | Says "today only Grafana can" show bands, but no Grafana dashboard ships. | Point to G2. |
| X6 and L7 | Both assume a namespace on findings and series that nothing carries. | Add G8 to their dependencies. |
| research/01 | Suggests `significant_terms` needs a fallback on OSS 7.10. | Only the `p_value` heuristic needs Basic; JLH, chi_square, gnd, mutual_information and percentage are core. |
| [DP §15](deployment.md#15-new-code-needed-in-anomalyd) | Its rows are numbered N1–N9, which clash with future-improvements N1–N11 (for example DP §15 N8 is readiness, which is future-improvements N11). Found while writing this plan. | Rename the DP §15 rows, for example to D1–D9. |

## Already covered by the existing plans

The review also checked these candidates. They need no G item.

- Remote write 2.0 receiver: not planned, by decision (DP §6, DP §15; anomalyd answers 415).
- Readiness separate from liveness: N11 and DP §15 N8 (`/-/ready`).
- Server Kubernetes manifests (StatefulSet, PVC, Service, PDB, NetworkPolicy, ServiceMonitor): N11,
  DP §9–10.
- Prometheus self-monitoring rules and a watchdog: X9, DP §16–18.
- TLS and authentication on inbound and outbound connections: N10, DP §15 N1.
- Suppressions, feedback labels, maintenance windows: X7, X5.
- Config file, hot reload, `check-config`: N1, X11, N11, MT §9.5.
- Isolated sinks with queues (Alertmanager, ES, webhook, log): N5, N6.
- Agent delivery correctness (offset registry, event-time buckets, idempotent pushes): N3, N4,
  DP §15 N3 and N9.
- Multi-source agent config (per-path parsers, excludes, multi-line): N1, N2.
- High availability (agent fan-out, active-active servers): L1, DP §15 N4.
- Derived and multivariate detectors (error ratio, template mix, count tails, incident
  candidates, health score): X2, X3, X6, L4.
- Findings history, filters and retention: the N5 log sink, N6, X7, the MT export.
- Per-series overrides: X11 and the MT model set.
- Level-shift and multi-season baselines: X4, X5, L5.
- Agent DaemonSet hardening: DP §9–10, N10.

## Considered and rejected

One line each, with the reason. A "residue" is a small part worth keeping, and where it goes.

| Idea | Why not |
|---|---|
| Change-aware version diff and deploy verdict per rollout | Needs per-pod or per-version series, which breaks "reduce before you detect", and duplicates Argo Rollouts and Flagger analysis; X4 covers change events. Residue for X4: a `change_window=true` label and info default for novelty in the rollout window, and post-event templates in X13. |
| Online seasonality auto-detection | X5 already picks lags per series online, and MT covers "none" and EWMA offline. Residue for X5: the lookback median as a third candidate; weekly log lags are a ring-sizing decision. |
| Recurring-issue detection and auto-acceptance | Novelty already fires once (`evalNovelty`), the seasonal median absorbs fixed-time cron spikes, and X7 suppressions and MT budgets exist. Residue: a 7-day fire counter for `recurring=true` (G21), only if shadow data shows a need. |
| Anomaly-rate rollups in anomalyd | One PromQL expression on the exported `anomalyd_score` does it; it is a G2 panel. |
| Exemplar capture from remote write | The inputs are recording-rule outputs, which carry no exemplars. Use a lookup at fire time (`query_exemplars` or a Jaeger search URL). |
| Threshold preview or dry-run API | PromQL over the exported scores already previews it; MT §9.5's replay covers season changes. |
| Scoring partial buckets early | Findings never page, T0 covers urgent impact, and novelty already fires within one bucket. Extrapolating partial counts hurts precision. |
| Long-horizon baselines from Thanos or Mimir | There is no long-term store in the stack, half the series exist only in anomalyd, and the trainer archive covers long history. Doc fix for X5 in [Fixes](#fixes-to-existing-plans). |
| Histogram distribution-shift detection, native-histogram ingest | Contradicts pre-aggregation; add p50 and p99 recording rules instead. Residue: count the native-histogram samples `promrw` skips (G11, deferred). |
| Agent liveness and readiness tied to pushes | A server outage would restart every agent in the fleet and lose their backlogs. Residue: an agent `/-/healthy` that fails when `pollAll` stalls, `GOMEMLIMIT`, and the priorityClass fix ([Fixes](#fixes-to-existing-plans)). |
| A findings history API in anomalyd | History belongs to ES and Kibana (N5, N6). Residue: keep the ring of resolved findings in the snapshot (G5, deferred to the X14 rewrite). |
| Label-driven per-series overrides | Labels are series identity, so editing them resets history. Config-driven overrides are planned in X11 and the model set. |
| Static labels and annotation templates | `--alert-label` (DP §15 N5) and N7's `discover_url` are planned. Residue: a general `--annotation-template` later; `runbook_url` is in G9. |
| OTLP ingest on the server | It ships raw lines centrally, and duplicates L6 (on the agent) and the existing `prometheusremotewrite` path. |
| Trend-aware baselines as a new item | X4, X5 and L5 cover it; the bias is about 1–1.5 z at 5–7 % growth per week. Add a slope scorer only if N8 shows drift. |
| Adaptive conformal p-values | An empirical p-value from 2,016 points cannot reach the \|z\| ≥ 4 tail; MT already plans SPOT/GPD thresholds. |
| Global false-discovery-rate (FDR) control | Scores are not calibrated p-values, the Benjamini–Hochberg assumptions fail, and findings never page. Use MT budgets and SPOT. |
| Evaluation upgrades (affiliation metrics, PATE, TSB-AD) | Lead time and volume are planned; with about 20 incidents the confidence intervals dominate; TSB-AD lacks the seasonality our scorer assumes. Residue: "notifications per incident" in N8 (G4). |
| OneShotSTL detector | Just one candidate for the planned L5 / MT scorer interface. It does not handle multiple seasons, and it would end Python parity. |
| 429 rate limits per client (part of G28) | Clients are internal behind NetworkPolicy and N10 tokens, and a 429 to Prometheus only moves the backlog into its WAL. |
| A built-in `/mcp` server (part of G31) | Adds surface to a binary that should stay small; a HolmesGPT toolset over the existing API does the job. |

## Open questions

- **Grafana:** which major version runs, and may we install the Infinity data-source plugin? G2's
  findings table and G24's forecast panel depend on it.
- **Time zones (G7):** do all services follow one local time zone, or do some need their own?
- **Service catalog (G9):** who owns the file, and where does it live? This is the same owner
  question as X7's "who labels findings?".
- **ES writes (G3, G9, N6):** does anomalyd write to ES (N6), or does the findings-shipper
  (DP §5.3)? See [Fixes](#fixes-to-existing-plans).
- **Parameter values (G26):** may redacted slot values leave the node at all, as an opt-in?
