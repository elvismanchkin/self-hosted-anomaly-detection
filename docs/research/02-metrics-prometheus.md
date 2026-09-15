# 02 — Metrics anomaly detection on Prometheus + Grafana OSS

**In short:**
- The best free option is a set of Prometheus Query Language (PromQL) [recording rules](https://prometheus.io/docs/prometheus/latest/configuration/recording_rules/) from `grafana/promql-anomaly-detection`. A recording rule is a query that Prometheus runs on a schedule and stores as a new series. These rules draw a band of normal values around each series. They run on plain Prometheus.
- Run anomaly detection only on about 3–5 k pre-aggregated service series. Never run it on the 1–10 M raw series.
- Only Service Level Objective (SLO) [burn-rate](https://sre.google/workbook/alerting-on-slos/) alerts should page someone. Anomaly alerts go to Slack or a ticket.
- Grafana OSS, the free open-source software (OSS) edition, has no machine-learning anomaly detection. Those features exist only in Grafana Cloud.
- Later, a small Python job can add seasonal scores. vmanomaly and Netdata do not fit a free stack at 1–10 M series.

Terms and abbreviations: [glossary](../glossary.md).

We want our own anomaly detection: free open-source parts plus a small amount of our own code, not a vendor product. This file covers metrics. It shows what Prometheus and Grafana OSS give for free, which open-source parts we can reuse, and what we must write ourselves.

Status: complete draft, researched on 2026-09-14. We checked versions with the GitHub API and Atom feeds, the PyPI JSON API and vendor docs. Anything we could not confirm is marked UNVERIFIED.
Scope: Prometheus + Grafana OSS with 1–10 M active series. Iteration 1 uses add-ons only, with no swap of a core part. All fetched links are in [LINKS.md](../../LINKS.md) (merged, without duplicates).

Sections:
- §0 Summary of findings
- §1 Anomaly detection in plain PromQL
- §2 What Grafana OSS gives, and what only Grafana Cloud has
- §3 An external detector job (Python or Go)
- §4 VictoriaMetrics vmanomaly
- §5 Netdata
- §6 Other open-source engines and nearby tools
- §7 How to keep series count and cost low
- §8 SLO burn-rate alerts (Sloth, Pyrra): Tier 0
- §9 What we recommend for metrics

## 0. Summary of findings
- **Best free option: PromQL recording-rule bands.** The project is `grafana/promql-anomaly-detection` (Apache-2.0, v0.2.1 from 2025-10-20, last commit 2026-05-08). It has two strategies:
  - `adaptive`: mean ± 2σ, that is two [standard deviations](https://en.wikipedia.org/wiki/Standard_deviation). It adds a high-pass filter on the [coefficient of variation](https://en.wikipedia.org/wiki/Coefficient_of_variation) (CoV). It smooths over 26 h and looks back 1 d and 1 w for seasonality.
  - `robust`: median ± 2·[MAD](https://en.wikipedia.org/wiki/Median_absolute_deviation) (median absolute deviation).

  Each monitored series creates about 9 derived series with `adaptive`, or 17 with `robust`. The rules run on plain Prometheus with no feature flags. Grafana uses the same logic in Grafana Cloud Asserts and Application Observability.
- **Pre-aggregate first.** Run anomaly detection on about 3–5 k (at most 15 k) service-level recording-rule series. Never run it on the 1–10 M raw series.
  - The inputs are service-level metrics of three kinds. [RED](https://grafana.com/blog/2018/08/02/the-red-method-how-to-instrument-your-services/) covers requests: rate, errors, duration. [USE](https://www.brendangregg.com/usemethod.html) covers resources: utilization, saturation, errors. Business KPIs are key performance indicators, such as orders per minute.
  - This adds about 30–90 k head series: the inputs plus 9–17 derived series per input. Head series are the active series that Prometheus holds in memory.
  - That is under 1 % of 10 M series, and up to about 9 % of 1 M.
- **Retention.** Seasonal checks need 7 d to 30 d of *recorded* series: 7 d for the bands, 30 d for the Sloth SLO window. Prometheus has only one global retention setting. So we add a small second Prometheus that serves only anomaly detection. We call it the **AD Prometheus**.
  - The main Prometheus sends it only the recorded series (`remote_write` with a keep filter). The AD Prometheus keeps them for 90 d.
  - The other option is a Thanos sidecar, which uploads Prometheus data blocks to object storage. It adds to the stack without replacing anything, but it turns off local compaction.
- **Tier 0: SLO burn-rate alerts.** The burn rate says how fast we use up the error budget of an SLO. Use Sloth v0.16.0 or Pyrra v0.10.1 (both Apache-2.0). These are the only alerts that should page. Anomaly alerts go to Slack or a ticket.
- **Grafana OSS has no machine-learning (ML) anomaly detection.** These are Grafana Cloud features: Grafana ML forecasting, outlier detection and dynamic alerting, Sift, and Adaptive Metrics. The `grafana-ml-app` plugin is not in the public plugin catalog. Grafana OSS gives us alert rules and band overlays on panels.
- **Prometheus 3.14.0** (2026-08-18) is the latest release. 3.15.0-rc.0 was out on the research date. What matters here:
  - In 3.0, `holt_winters` was renamed to `double_exponential_smoothing`. It is experimental. It is [Holt-linear](https://en.wikipedia.org/wiki/Exponential_smoothing) smoothing, so it follows a trend but is *not* seasonal.
  - `mad_over_time` has been experimental since 2.49.
  - `quantile_over_time`, `stddev_over_time` and `predict_linear` are stable.
  - Duration expressions are on by default since 3.14.
  - [Native histograms](https://prometheus.io/docs/specs/native_histograms/) are stable since 3.8.
- **External scorer: statsforecast MSTL.** [STL](https://www.statsmodels.org/stable/generated/statsmodels.tsa.seasonal.STL.html) is seasonal-trend decomposition using Loess smoothing. It splits a series into trend, season and residual. [MSTL](https://arxiv.org/abs/2107.13462) (multiple STL) does the same for several seasons, for example daily and weekly. We use statsforecast 2.1.1 (Apache-2.0).
  - We score the residuals with MAD and export `anomaly_score` on `/metrics`. A score above 1 means anomalous. This is the vmanomaly convention. Our PoC scorer exports a robust [z-score](https://en.wikipedia.org/wiki/Standard_score) instead, and alerts at `|z| > 4` (see §3).
  - Use river for streaming and PyOD for multivariate checks.
  - Avoid Merlion, ADTK, Luminaire, Kats, and [Prophet](https://facebook.github.io/prophet/) at scale. Merlion has had no release since 2024-06. ADTK is dead since 2020. Luminaire is dormant. Kats has had no release since 2022.
- **vmanomaly** (from VictoriaMetrics) is Enterprise-only, with a 2-month trial. It can read from plain Prometheus through `/query_range`. But it writes through the VictoriaMetrics `/api/v1/import` endpoint. So it needs a VictoriaMetrics single-node instance to store the scores. It is not free, so it is not for iteration 1.
- **Netdata**: its agent, under the GNU General Public License (GPL), does good per-metric [k-means](https://en.wikipedia.org/wiki/K-means_clustering) anomaly detection. But it cannot be a central anomaly-detection engine at 1–10 M series:
  - the user interface (UI) is closed source, under Netdata's own NCUL1 licence,
  - the free Cloud tier allows only 5 nodes,
  - its Prometheus collector reads at most 2 000 series per job by default.

---

## 1. Anomaly detection in plain PromQL

### 1.1 grafana/promql-anomaly-detection

This project is a set of Prometheus rule files. It draws a band of normal values around each tagged series. It alerts when the series leaves the band.

- **License:** Apache-2.0.
- **What is free:** everything (plain rule files and Grafana dashboards).
- **Method:** recording-rule bands, with two strategies:
  - `adaptive`: mean ± 2·stddev, a CoV high-pass filter, 26 h smoothing, and a seasonal look-back of 1 d + 1 w.
  - `robust`: median ± 2·MAD over a 1 d window, with a seasonal look-back of 1 d + 1 w.
- **Scale:** use it only on a pre-aggregated subset of series that carry the tag label. Each input series creates about 9–17 derived series (see the cost below).
- **Integration:** a pure add-on. Add `rules/*.yml` to the Prometheus `rule_files`. Tag the input series with our own recording rules.
- **Effort to run:** low (YAML). The only tuning is the multipliers per `anomaly_type`.
- **Maturity:** v0.2.1 was released on 2025-10-20. The last commit on `main` is from 2026-05-08. The repo was last pushed on 2026-08-30. It has 420 stars and is not archived. Grafana Labs uses the same approach in Grafana Cloud Application Observability / Asserts.
- **Limits:**
  - It needs a 24–26 h warm-up. Before that, the bands are too sensitive.
  - The weekly look-back needs 7 d+ of retention.
  - The selector `{anomaly_name!=""}` scans every series that carries that label.
  - `robust` runs `quantile_over_time(...[1d])` at a 1 m interval. This costs a lot of CPU.
  - There is one global `AnomalyDetected` alert. We must add the routing labels ourselves.
- **Sources:** [repo](https://github.com/grafana/promql-anomaly-detection), [releases API](https://api.github.com/repos/grafana/promql-anomaly-detection/releases), [Grafana blog, 2024-10-03](https://grafana.com/blog/2024/10/03/how-to-use-prometheus-to-efficiently-detect-anomalies-at-scale/).

**How it plugs in** (from the [README](https://raw.githubusercontent.com/grafana/promql-anomaly-detection/main/README.md)):
- The rules pick up any series that has the label `anomaly_name`.
- The optional label `anomaly_strategy` is `adaptive` (default) or `robust`.
- The optional label `anomaly_type` is one of `requests|latency|errors|resource`. It is a hook for per-type thresholds. But v0.2.1 uses the same constants for every type.
- The `requests` type is *meant* to drop sparse series with `avg_over_time(...[1h]) < 5/60`. This filter has no effect. The final catch-all `OR {anomaly_name!="", anomaly_select="", …} > 0` adds those series back.
  - We re-tested this on 2026-09-14 with `promtool test rules` on v3.14.0. A 0.01/s `requests` series still appears in `anomaly:adaptive:select` and `anomaly:robust:select`.
  - So our own tagging rule must drop sparse inputs.
- Each strategy must emit 3 series: `anomaly:upper_band`, `anomaly:lower_band` and `anomaly:level`. It sets `anomaly_select="1"` so that it does not process its own outputs again ([rules/README.md](https://raw.githubusercontent.com/grafana/promql-anomaly-detection/main/rules/README.md)).
- The final band is the union of three "prediction types":
  - `short_term`: the variability over about 26 h.
  - `margin`: a minimum width of ±0.5·avg, so that flat series do not alert on noise.
  - `long_term` / `seasonality`: the same hour 1 d and 1 w ago.
- The upper band is the `max` of the three. The lower band is the `min`, clamped at 0. Taking the widest of the three is a conservative design with few false positives.

**Tagging rule for an input series.** Copy this pattern from `rules/examples/otel_demo.yml`:
```yaml
- record: anomaly:request:rate5m
  expr: sum(rate(traces_span_metrics_duration_milliseconds_count{span_kind=~"SPAN_KIND_SERVER|SPAN_KIND_CONSUMER"}[5m])) by (job)
  labels:
    anomaly_name: "otel_demo_requests"
    anomaly_type: "requests"
    anomaly_strategy: "adaptive"
```

**The `adaptive` strategy.** This is the structure of `rules/adaptive.yml`, copied as is and shortened with `...` ([source](https://raw.githubusercontent.com/grafana/promql-anomaly-detection/main/rules/adaptive.yml)):
```yaml
groups:
  - name: AnomalyAdaptiveShortTerm          # default evaluation_interval
    rules:
      - record: anomaly:adaptive:threshold_by_covar   # constant 0.5 (CoV high-pass)
        expr: 0.5
      - record: anomaly:adaptive:sparse_threshold     # constant 5/60
        expr: 5/60
      - record: anomaly:adaptive:stddev_multiplier    # constant 2
        expr: 2
      - record: anomaly:adaptive:margin_multiplier    # constant 0.5
        expr: 0.5
      - record: anomaly:adaptive:select
        expr: |-
          ( {anomaly_name!="", anomaly_type="requests", anomaly_select="", anomaly_strategy=~"^$|adaptive"} > 0
            unless avg_over_time({anomaly_name!="", anomaly_type="requests", anomaly_select="", anomaly_strategy=~"^$|adaptive"}[1h]) < on() group_left anomaly:adaptive:sparse_threshold )
          OR {anomaly_name!="", anomaly_type="latency",  anomaly_select="", anomaly_strategy=~"^$|adaptive"} > 0
          OR {anomaly_name!="", anomaly_type="errors",   anomaly_select="", anomaly_strategy=~"^$|adaptive"} > 0
          OR {anomaly_name!="", anomaly_type="resource", anomaly_select="", anomaly_strategy=~"^$|adaptive"} > 0
          OR {anomaly_name!="", anomaly_select="", anomaly_strategy=~"^$|adaptive"} > 0
        labels: { anomaly_select: "1", anomaly_strategy: adaptive }
      - record: anomaly:adaptive:avg_1h
        expr: avg_over_time(anomaly:adaptive:select[1h])
      - record: anomaly:adaptive:stddev_1h:filtered          # high-pass: keep only periods with CoV > 0.5
        expr: stddev_over_time(anomaly:adaptive:select[1h]) > anomaly:adaptive:avg_1h * on() group_left anomaly:adaptive:threshold_by_covar
      - record: anomaly:adaptive:stddev_st                   # "smoothing": mean of filtered 1h-stddev over 26h
        expr: avg_over_time(anomaly:adaptive:stddev_1h:filtered[26h])
      - record: anomaly:level
        expr: anomaly:adaptive:select
      - record: anomaly:lower_band
        expr: |-
          clamp_min(min without(prediction_type)(
              label_replace(last_over_time(anomaly:adaptive:avg_1h[2m]) - last_over_time(anomaly:adaptive:stddev_st[2m]) * on() group_left anomaly:adaptive:stddev_multiplier, "prediction_type","short_term","","")
           or label_replace(last_over_time(anomaly:adaptive:avg_1h[2m]) - last_over_time(anomaly:adaptive:avg_1h[2m])    * on() group_left anomaly:adaptive:margin_multiplier, "prediction_type","margin","","")
           or label_replace(last_over_time(anomaly:adaptive:lower_band_lt[10m]), "prediction_type","long_term","","")
          ), 0)
      - record: anomaly:upper_band        # mirror image with max without(prediction_type) and "+"
        expr: ...
  - name: AnomalyAdaptiveLongTerm
    interval: 5m                          # cheaper cadence for seasonal look-back
    rules:
      - record: anomaly:adaptive:upper_band_lt
        expr: |-
          max without(look_back)(
              label_replace(avg_over_time(anomaly:adaptive:select[1h] offset 167h30m) + stddev_over_time(anomaly:adaptive:select[1h] offset 167h30m) * on() group_left anomaly:adaptive:stddev_multiplier, "look_back","1w","","")
           or label_replace(avg_over_time(anomaly:adaptive:select[1h] offset 23h30m)  + stddev_over_time(anomaly:adaptive:select[1h] offset 23h30m)  * on() group_left anomaly:adaptive:stddev_multiplier, "look_back","1d","","")
          )
      - record: anomaly:adaptive:lower_band_lt   # mirror with min/"-"
        expr: ...
  - name: AnomalyAdaptiveAlerts
    rules:
      - alert: AnomalyDetected
        for: 5m
        expr: |
          last_over_time(anomaly:level[2m]) < last_over_time(anomaly:lower_band{anomaly_strategy="adaptive"}[2m])
          or
          last_over_time(anomaly:level[2m]) > last_over_time(anomaly:upper_band{anomaly_strategy="adaptive"}[2m])
        labels: { severity: warning, anomaly_strategy: adaptive }
```
Why `offset 23h30m` with a `[1h]` window? This centres the 1 h window on "the same time yesterday" (from 23:30 to 24:30 ago). That is ±30 min around the seasonal point. `167h30m` does the same for last week.

The snippet follows the `main` branch. There, the long-term part of the band has a `label_replace(…, "prediction_type", "long_term", …)` wrapper (and `"seasonality"` in `robust.yml`).
- This wrapper came with pull request (PR) #63, merged on 2026-05-08.
- It is **not in the v0.2.1 tag** that our proof of concept (PoC) pins. In v0.2.1 the term is a bare `last_over_time(anomaly:adaptive:lower_band_lt[10m])` ([v0.2.1 adaptive.yml](https://raw.githubusercontent.com/grafana/promql-anomaly-detection/v0.2.1/rules/adaptive.yml)).
- The PR says that the long-term band was dropped from the `max`. But on Prometheus 3.14.0 both versions give the same `anomaly:upper_band`. We re-tested this with `promtool test rules`, and the long-term band wins in both. So the pin is fine.

How does it react to a lasting step on a series with low variance? We re-tested this with `promtool` on the end-to-end (e2e) setup: 2 flat days, then a step.
- Steps of ×2 to ×10, and a drop to 40 %, fire 7–10 min after the start. The bands absorb them 17–22 min in.
- A ×1.5 step never fires. The margin band is ±0.5·avg_1h, and the CoV filter (> 0.5) never passes.

**The `robust` strategy: key rules** ([rules/robust.yml](https://raw.githubusercontent.com/grafana/promql-anomaly-detection/main/rules/robust.yml)). The main group runs every 1 m, and the seasonal group every 5 m:
```yaml
- record: anomaly:robust:median_lt
  expr: quantile_over_time(0.5, anomaly:robust:select[1d])
- record: anomaly:robust:abs_dev_lt
  expr: abs(anomaly:robust:select - anomaly:robust:median_lt)
- record: anomaly:robust:mad_lt                       # MAD approximated via recorded abs-dev (no feature flag needed)
  expr: quantile_over_time(0.5, anomaly:robust:abs_dev_lt[1d])
- record: anomaly:level
  expr: quantile_over_time(0.5, anomaly:robust:select[1h])   # smoothed level (1h median) — fewer false alerts on spikes
# bands = median_lt ± 2*mad_lt, margin = ±0.5*median_lt, seasonality = 1h median/MAD at offset 23h45m and 167h45m
- record: anomaly:robust:median_seasonality_1w
  expr: quantile_over_time(0.5, anomaly:robust:select[1h] offset 167h45m)
```
`robust` does not use the Prometheus function `mad_over_time`, which needs an experimental flag. It approximates MAD with two `quantile_over_time` rules, so it runs on plain Prometheus. `anomaly:level` is a 1 h median. So a shift must last > 30 min before the level moves, plus `for: 5m`. This makes `robust` a strategy for slow, lasting changes, not for fast onsets.

**Series and rule cost per monitored input series** (counted from the rule files):

| Strategy | Derived series per input series | Rule evaluations | Heaviest query |
|---|---|---|---|
| adaptive | 9, plus 4 global constants (note 1) | 9 rules per evaluation (note 2) | `avg_over_time(stddev_1h:filtered[26h])`: reads ~1 560 samples per series at a 1 m interval |
| robust | 17 | 17 rules | `quantile_over_time(0.5, …[1d])` ×2: sorts ~1 440 samples per series every minute |

1. The 9 series: `select`, `avg_1h`, `stddev_1h:filtered`, `stddev_st`, `level`, `lower_band`, `upper_band`, `upper_band_lt`, `lower_band_lt`.
2. The count is per evaluation, not per series. Each rule is vectorised: one rule covers all tagged series at once.

**Sizing for our stack** (arithmetic, not a benchmark):
- Series: 5 000 tagged series × 9 = 45 k extra active series for `adaptive`. That is 0.45–4.5 % of 1–10 M. For `robust` it is × 17 = 85 k.
- Samples *read* per evaluation for `adaptive`: about 5 000 × (60 + 60 + 1 560 + …) ≈ 9–10 M samples/min.
- For `robust`: about 5 000 × 2 × 1 440 × sort ≈ 15 M samples/min, with sorting.
- Past a few thousand series, plan a dedicated Prometheus for rule evaluation (or `concurrent-rule-eval`).
- **Do not** point the rules at raw series. Tag about 2–15 k pre-aggregated Service Level Indicator (SLI) series instead. §7.1 lists them.

These numbers are estimates, UNVERIFIED by a benchmark. Grafana's blog gives no numbers. We confirmed the rule counts with `promtool check rules` on v3.14.0:
- `adaptive.yml` = 14 rules (4 constants + 9 derived + 1 alert).
- `robust.yml` = 21 rules (3 constants + 17 derived + 1 alert).

**Retention.** The long-term band needs ≥ 7 d + 1 h of the `select` series. This is the recording-rule output, not raw data. The default Prometheus retention is 15 d, so local storage is enough.

Guard against runaway tagging with the rule-group `limit:` field.
- It applies per rule. If a rule would emit more series than the limit, Prometheus discards *all* its output for that evaluation. It also marks the rule as failed.
- The other rules in the group still run.
- Also watch `prometheus_rule_group_iterations_missed_total` ([recording_rules.md](https://raw.githubusercontent.com/prometheus/prometheus/v3.14.0/docs/configuration/recording_rules.md), metric name from [group.go](https://raw.githubusercontent.com/prometheus/prometheus/v3.14.0/rules/group.go)).

### 1.2 GitLab's z-score approach (Monitorama 2019)
Source: a [GitLab blog post](https://about.gitlab.com/blog/anomaly-detection-using-prometheus/), published on 2019-07-23. Sara Kassabian wrote it as a summary of Andrew Newdigate's Monitorama 2019 talk. A [z-score](https://en.wikipedia.org/wiki/Standard_score) says how many standard deviations a value is from the mean.

1. **Aggregate to service level first.** Keep `job` and `environment`. Drop instance, method, controller and status_code:
   ```yaml
   - record: job:http_requests:rate5m
     expr: sum without(instance, method, controller, status_code) (rate(http_requests_total[5m]))
   ```
2. **Compute the 1-week mean and stddev, then the z-score:**
   ```yaml
   - record: job:http_requests:rate5m:avg_over_time_1w
     expr: avg_over_time(job:http_requests:rate5m[1w])
   - record: job:http_requests:rate5m:stddev_over_time_1w
     expr: stddev_over_time(job:http_requests:rate5m[1w])
   # z = (job:http_requests:rate5m - job:http_requests:rate5m:avg_over_time_1w) / job:http_requests:rate5m:stddev_over_time_1w
   ```
   First check that the data is close to a normal distribution. `(max_over_time(x[1w]) - avg_over_time(x[1w])) / stddev_over_time(x[1w])` should be about ±4. A value of ±20 means the tail is too long, and the z-score is not valid. Error rates, latencies and queue lengths "probably don't have normal distributions". For them, prefer fixed thresholds or SLOs.
3. **Seasonal prediction: the median of the 3 previous weeks, plus a growth trend.** One holiday week does not break it:
   ```yaml
   - record: job:http_requests:rate5m_prediction
     expr: >
       quantile(0.5,
         label_replace(avg_over_time(job:http_requests:rate5m[4h] offset 166h)
           + job:http_requests:rate5m:avg_over_time_1w - job:http_requests:rate5m:avg_over_time_1w offset 1w, "offset", "1w", "", "")
         or label_replace(avg_over_time(job:http_requests:rate5m[4h] offset 334h)
           + job:http_requests:rate5m:avg_over_time_1w - job:http_requests:rate5m:avg_over_time_1w offset 2w, "offset", "2w", "", "")
         or label_replace(avg_over_time(job:http_requests:rate5m[4h] offset 502h)
           + job:http_requests:rate5m:avg_over_time_1w - job:http_requests:rate5m:avg_over_time_1w offset 3w, "offset", "3w", "", "")
       ) without (offset)
   ```
   `offset 166h` with `[4h]` gives a window centred on the same time last week.
4. **Alert.** It goes to Slack and does not page:
   ```yaml
   - alert: RequestRateOutsideNormalRange
     expr: abs((job:http_requests:rate5m - job:http_requests:rate5m_prediction) / job:http_requests:rate5m:stddev_over_time_1w) > 2
     for: 10m
   ```

**Retention.** This approach needs ≥ 3 w + 4 h of the aggregated series (`offset 502h` + `[4h]`). `avg_over_time_1w offset 3w` also needs ≥ 4 weeks of the recorded `avg_over_time_1w`. The default local retention of 15 d is **not enough**.

Prometheus retention is global, not per series. So raising it on the server with 1–10 M series is expensive. Instead, keep the recorded series in a small side Prometheus with 35–90 d retention, or add a Thanos sidecar (§7).

The retention setting itself also changed:
- The `--storage.tsdb.retention.time` flag is **deprecated**. It is marked so since 3.8.0 and is still there in 3.14. The replacement is `storage: tsdb: retention: time:` in the config file.
- Use the config-file form only on ≥ 3.11.0. That release fixed a unit bug (#18200): retention set in the file lasted 1e6 times longer than set.
- Sources: [command-line flags](https://raw.githubusercontent.com/prometheus/prometheus/v3.14.0/docs/command-line/prometheus.md), [configuration.md](https://raw.githubusercontent.com/prometheus/prometheus/v3.14.0/docs/configuration/configuration.md), [CHANGELOG](https://raw.githubusercontent.com/prometheus/prometheus/v3.14.0/CHANGELOG.md).

### 1.3 Prometheus functions for anomaly detection (checked against the v3.14.0 docs)
Sources: [functions.md](https://raw.githubusercontent.com/prometheus/prometheus/v3.14.0/docs/querying/functions.md), [CHANGELOG](https://raw.githubusercontent.com/prometheus/prometheus/v3.14.0/CHANGELOG.md), [feature flags](https://raw.githubusercontent.com/prometheus/prometheus/v3.14.0/docs/feature_flags.md).

| Function | Status in 3.14 | Flag | Use for anomaly detection |
|---|---|---|---|
| `avg_over_time`, `stddev_over_time`, `stdvar_over_time`, `min/max_over_time` | stable | none | z-score, bands. `stddev_over_time` = population stddev |
| `quantile_over_time(φ, r)` | stable | none | median / p95 bands, robust baselines |
| `mad_over_time(r)` | **experimental**; added in 2.49.0 (2024-01-15); NaN (not-a-number) bug fixed in 3.14.0 | `--enable-feature=promql-experimental-functions` | robust MAD in one call, instead of 2 recorded quantile rules |
| `predict_linear(r, t)` | stable (gauges only) | none | capacity alerts ("disk full in 4 h"); trend detection |
| `double_exponential_smoothing(r, sf, tf)` | **experimental**; renamed from `holt_winters` in **3.0.0** (note 1) | `promql-experimental-functions` | trend smoothing; **no seasonality**, so not a seasonal baseline |
| `first_over_time` | stable since **3.14.0** (experimental from 3.7.0) | none | value at the window start |
| `ts_of_min/max/last/first_over_time` | experimental (3.5.0 / 3.7.0) | `promql-experimental-functions` | "when did the spike happen" |
| `histogram_quantiles` (multi-φ) | experimental (3.11.0) | `promql-experimental-functions` | p50/p90/p99 in one rule |
| `limitk`, `limit_ratio` | experimental aggregation operators (added in 2.54.0) | `promql-experimental-functions` (operators.md) | deterministic sampling of series |
| `fill()` / `fill_left()` / `fill_right()` binary-operator modifiers | experimental (3.10.0) | `promql-binop-fill-modifiers` | no gaps when the error series is absent (error ratio = 0) |
| `anchored` / `smoothed` range selectors | experimental (3.7.0) | `promql-extended-range-selectors` | cleaner `increase()` on sparse counters |
| duration expressions (`[step()*4]`, `offset (1w - 30m)`) | **on by default since 3.14.0** | none now (was `promql-duration-expr`) | seasonal offsets with parameters |

1. `double_exponential_smoothing` is Holt-linear (double) exponential smoothing. It is not the triple, seasonal Holt-Winters method.

In practice, use only stable functions in alerting rules. Experimental ones "might change their name, syntax, or semantics" (feature_flags.md). `mad_over_time` is tempting, but it saves only one recording rule.

### 1.4 Prometheus versions and 3.x features that matter here
- **Latest: v3.14.0**, published on 2026-08-18 ([GitHub API](https://api.github.com/repos/prometheus/prometheus/releases/latest)). The changelog dates it 2026-08-17. A new minor release comes about every 6 weeks (3.12.0 on 2026-05-28, 3.13.0 on 2026-07-01).
- 3.0.0 (2024-11-14): UTF-8 metric and label names are on by default. `holt_winters` became `double_exponential_smoothing`, behind a flag. There is a new UI.
- 3.8.0 (2025-11-28): **native histograms are stable**. Scraping them is opt-in, with `scrape_native_histograms`. Receiving Remote-Write (RW) 2.0 is at rc.4.
  - Native histograms collapse the many `_bucket` series of a latency metric into one series. This is the biggest single way to cut cardinality (the number of distinct series) for latency inputs.
  - Exporters must emit them. Or Prometheus can convert classic histograms at scrape time with `convert_classic_histograms_to_nhcb: true`. The query syntax then changes (see §7.3).
- The `concurrent-rule-eval` flag evaluates independent rules in a group in parallel (`--rules.max-concurrent-evals`, default 4). It helps the upstream anomaly rules only a little:
  - A group with any selector that has no metric name is "indeterminate". It stays fully sequential. The `select` rule's `{anomaly_name!=""…}` is such a selector.
  - So `AnomalyAdaptiveShortTerm` and `AnomalyRobust` gain nothing. Only the 5 m long-term/seasonal groups can gain.
  - Rule groups already run in parallel with each other ([rules/group.go](https://raw.githubusercontent.com/prometheus/prometheus/v3.14.0/rules/group.go) `buildDependencyMap`).
- The rule-group `limit:` field caps the series that each recording rule in the group can produce. Use it as a cardinality guard on the anomaly rules. §1.1 explains what happens when a rule goes over the limit. Missed iterations show in `prometheus_rule_group_iterations_missed_total` ([recording_rules.md](https://raw.githubusercontent.com/prometheus/prometheus/v3.14.0/docs/configuration/recording_rules.md)).

---

## 2. What Grafana OSS gives, and what only Grafana Cloud has

The latest Grafana is **v13.2.1** (2026-09-02). Patch releases 13.1.5, 13.0.8 and 12.4.10 came out the same day ([releases](https://api.github.com/repos/grafana/grafana/releases)).

### 2.1 What Grafana OSS alerting gives us for free
| Capability | In OSS? | Source |
|---|---|---|
| **Grafana-managed alert rules** | Yes (note 1) | [create-data-source-managed-rule.md](https://raw.githubusercontent.com/grafana/grafana/main/docs/sources/alerting/alerting-rules/create-data-source-managed-rule.md) |
| **Data source-managed alert rules** | Partly: create and edit only for **Mimir and Loki** (note 2) | same as above |
| **Grafana-managed recording rules** | Yes, but they must be enabled in OSS/Enterprise (note 3) | [create-grafana-managed-recording-rules.md](https://raw.githubusercontent.com/grafana/grafana/main/docs/sources/alerting/alerting-rules/create-recording-rules/create-grafana-managed-recording-rules.md) |
| **Forecast alerts** | Yes, with PromQL `predict_linear` (note 4) | [alerting-on-forecasts.md](https://raw.githubusercontent.com/grafana/grafana/main/docs/sources/alerting/guides/alerting-on-forecasts.md) |

1. Grafana-managed rules can query any backend data source that supports alerting, and several data sources in one rule. They support:
   - server-side expressions (Math, Reduce, Resample, Threshold),
   - no-data and error states, images and state history,
   - role-based access control (RBAC) and Terraform provisioning.

   Grafana stores these rules in its own database and evaluates them itself.
2. **Prometheus rules are view-only** in Grafana: "you cannot create or edit these rules in Grafana". So rules for plain Prometheus stay in `rule_files` (in Git).
3. Grafana writes the results to *our* Prometheus-compatible time-series database (TSDB). The setting is `[recording_rules] default_datasource_uid`. Since 12.1, each rule can have its own target data source. The docs say: "Grafana does not contain an embedded time-series database". Writing into plain Prometheus needs its remote-write receiver (`--web.enable-remote-write-receiver`). This is UNVERIFIED end to end.
4. Example: `predict_linear(<m>[14d], 7 * 24 * 3600) > 85`. The docs say that it is not suitable for seasonal signals.

**Recommendation:** keep the anomaly recording rules and alert rules as **Prometheus rule files**, in Git and tested with `promtool test rules`. Route alerts through Alertmanager. Use Grafana-managed rules only when a rule must combine Prometheus with another data source. For example: Elasticsearch log counts and a metric band in one condition.

### 2.2 Features only in Grafana Cloud (not available to us)
| Feature | Status | Evidence |
|---|---|---|
| Grafana Machine Learning: metric **forecasting**, **outlier detection**, **dynamic alerting** (note 1) | Grafana Cloud: "free to use with all Grafana Cloud accounts" ([docs](https://grafana.com/docs/grafana-cloud/ai-tools/machine-learning/)) | Cloud-only in practice (note 2) |
| **Sift** (automated investigations) | Grafana Cloud: "a powerful, free diagnostic assistant included in Grafana Cloud" | [Sift docs](https://grafana.com/docs/grafana-cloud/alerting-and-irm/machine-learning/sift/); no self-managed option mentioned |
| **Adaptive Metrics** (aggregates unused series, based on usage) | Grafana Cloud: "a metrics management and cardinality optimization feature in Grafana Cloud" | [docs](https://grafana.com/docs/grafana-cloud/adaptive-telemetry/adaptive-metrics/); listed on the [pricing page](https://grafana.com/pricing/); no self-managed option mentioned |
| Asserts / Application Observability anomaly bands | Grafana Cloud, but the band logic is the **open** `promql-anomaly-detection` rules (§1.1) | [Grafana blog](https://grafana.com/blog/2024/10/03/how-to-use-prometheus-to-efficiently-detect-anomalies-at-scale/) |

1. Forecasting is seasonal and outputs yhat / yhat_upper / yhat_lower. Outlier detection uses [DBSCAN](https://en.wikipedia.org/wiki/DBSCAN) (a density-based clustering method), or MAD against a rolling 24 h median. It needs ≥ 3 series.
2. Why we say Cloud-only:
   - The OSS docs call the section "Seasonal forecast with **Grafana Cloud** Machine Learning". The results are stored in `grafanacloud-ml-metrics` ([alerting-on-forecasts.md](https://raw.githubusercontent.com/grafana/grafana/main/docs/sources/alerting/guides/alerting-on-forecasts.md)).
   - `grafana-ml-app` is **not in the public plugin catalog**. `/api/plugins/grafana-ml-app` returns 404, and the list of versions is empty.
   - A third-party blog claims that `grafana-cli plugins install grafana-ml-app` works on self-hosted Grafana. The catalog contradicts this ([blog](https://mustafa.net/2026/03/12/grafana-machine-learning-predictive-analytics-for-your-homelab/)).
   - Does a self-managed **Enterprise** license unlock it? UNVERIFIED. ML is not in the [Grafana Enterprise feature list](https://grafana.com/docs/grafana/latest/introduction/grafana-enterprise/).
   - Cloud default limits per hosted Grafana instance: 10 forecasts × 100 series, and 10 outlier detectors × 1,000 series. Grafana raises them on request ([limits](https://grafana.com/docs/grafana-cloud/ai-tools/machine-learning/additional-configuration/limits/)).

**What replaces each Cloud feature in OSS:**
- Forecasting → `predict_linear`, or the external statsforecast job (§3).
- Dynamic thresholds → the bands from §1.1.
- Adaptive Metrics → Prometheus `metric_relabel_configs` drop rules, plus usage analysis in the style of `mimirtool analyze`. This is UNVERIFIED for plain Prometheus (see §7).
- Outlier detection across a peer group (like the Grafana ML "MAD" mode) → pure PromQL with stable functions only:
```yaml
# pod whose p95 deviates > 3 MAD from its service's peers (placeholder series pod:latency:p95 with a `service` label)
- record: service:latency_p95:median
  expr: quantile by (service) (0.5, pod:latency:p95)
- record: service:latency_p95:mad
  expr: quantile by (service) (0.5, abs(pod:latency:p95 - on(service) group_left service:latency_p95:median))
- alert: PodLatencyPeerOutlier
  expr: |
    abs(pod:latency:p95 - on(service) group_left service:latency_p95:median)
      > on(service) group_left() (3 * service:latency_p95:mad)
    and on(service) count by (service) (pod:latency:p95) >= 3
  for: 15m
```
We verified this with `promtool check rules` and a `promtool test rules` case on `prom/prometheus:v3.14.0`. The test had 4 pods, one of them at 5×, and only that pod fired. Two warnings:
- `group_left()` needs the empty parentheses before a right-hand side in parentheses. Otherwise the parser reads `(3 * …)` as a label list.
- If all peers are identical, MAD = 0. Add a guard with a minimum absolute deviation.

### 2.3 Grafana OSS plugins for anomaly detection or forecasting
We scanned the public catalog (357 listed plugins, [API](https://grafana.com/api/plugins)) for the keywords anomaly, forecast, outlier and prophet. We found **nothing good enough for production anomaly detection on metrics**:
- `sarika1731-smartanalytics-panel` 1.0.2 (community, ~5 k downloads, 2026-05-11): trend and anomaly hints inside a panel. A toy, for visualization only.
- `kensobi-spc*`: commercial statistical process control (SPC, control limits). Not free.
- `grafana-lokiexplore-app` (Logs Drilldown): anomalies in log volume and log patterns. This belongs to the logs file 01.
- `grafana-metricsdrilldown-app` v2.5.1 (AGPL-3.0, the GNU Affero General Public License, 2026-08-24): browse and segment metrics without writing queries. Helpful for triage, but **not** anomaly detection ([README](https://raw.githubusercontent.com/grafana/metrics-drilldown/main/README.md)).
- `grafana-llm-app` v1.0.8 (2026-04-17): connects Grafana features to an LLM, a large language model. It could summarise anomaly bundles. But the LLM features in Grafana are mostly aimed at Cloud. It is UNVERIFIED which ones work self-hosted.

Conclusion: in OSS, **anomaly detection lives in Prometheus rules or in an external job. Grafana only shows the bands.** It overlays `anomaly:upper_band`/`lower_band` on panels, as the promql-anomaly-detection dashboards do.

---

## 3. An external detector job (Python or Go)

A separate job reads series from Prometheus and fits a seasonal model. It then exports scores back to Prometheus as metrics.

```
            ┌────────────── Prometheus (unchanged) ───────────────┐
 scrape ──► │ raw series (1–10 M) ─► recording rules ─► slo:/AD-input series (2–15 k) │
            └───────────────┬──────────────────────────────▲──────┘
                            │ /api/v1/query_range          │ scrape /metrics  (or remote-write receiver)
                            ▼  (top-K SLI series, step=1–5m)│
                   ┌──────────────────────────────┐        │
                   │ ad-scorer (Python, cron/loop)│────────┘  exports: anomaly_score{...}, yhat, yhat_lower, yhat_upper
                   │ fit nightly, score every 1–5m│
                   └──────────────┬───────────────┘
                                  ▼ only when score>1 for N min: small bundle (series, window, top correlated series) → LLM summary
```
Design rules:
1. **Read only pre-aggregated series** (`job:`/`slo:` recording-rule outputs), never raw ones.
   - A nightly fit with `query_range` over 5 k series × 28 d × 5 m step = 5 k × 8 064 ≈ 40 M points. This is fine in chunks (per series, or batched with a regex).
   - `--query.max-samples` limits one query to 50 000 000 loaded samples by default ([command-line flags](https://raw.githubusercontent.com/prometheus/prometheus/v3.14.0/docs/command-line/prometheus.md)). So batch about 100–500 series per request, and page through time.
   - `query_range` also rejects more than 11 000 points per series: "exceeded maximum resolution of 11,000 points per timeseries" ([api.go](https://raw.githubusercontent.com/prometheus/prometheus/v3.14.0/web/api/v1/api.go)). So 28 d in one request needs a step ≥ 4 m.
2. **Two schedules.** Each night, *fit* the seasonal baseline on 2–4 weeks of history. Every 1–5 min, *score* the last window against the stored forecast or quantiles. Scoring is cheap.
3. **Write the results back.**
   - Simplest: expose a `/metrics` endpoint with `prometheus_client` gauges (`anomaly_score`, `yhat`, `yhat_lower`, `yhat_upper`), and let Prometheus scrape it. This needs no new infrastructure, and stale markers work for free.
   - Another option: remote-write into Prometheus (`--web.enable-remote-write-receiver`), if we need back-filled timestamps.
   - Copy the vmanomaly convention: **`anomaly_score` is normalised so that > 1 = anomalous** ([models.md](https://raw.githubusercontent.com/VictoriaMetrics/VictoriaMetrics/master/docs/anomaly-detection/components/models.md)). Then one generic alert is enough: `anomaly_score > 1 for 10m`.
   - The PoC scorer `poc/ml/prom_anomaly_job.py` exports a signed robust z-score instead. It alerts on `abs(anomaly_score) > 4`. Divide by the threshold to get the > 1 convention.
   - Strip the `anomaly_*` labels from everything that the scorer exposes again. Series that still carry `anomaly_name` are picked up again by the `select` rule of promql-anomaly-detection. `anomaly_expected`, `anomaly_observed` and the band series share one label set. So the `select` rule then fails every evaluation with "vector cannot contain metrics with the same labelset" (re-tested with `promtool` v3.14.0). That means all band processing stops.
4. **Output series:** 4–5 × the monitored series (for example 5 k × 5 = 25 k series). This is very small.
5. **Libraries for the connection to Prometheus:**
   - `prometheus-api-client` 0.7.2 (MIT, 2026-04-13, [PyPI](https://pypi.org/pypi/prometheus-api-client/json)).
   - `prometheus_client` 0.26.0 (Apache-2.0 AND BSD-2-Clause, 2026-07-24, [PyPI](https://pypi.org/pypi/prometheus-client/json)).

### 3.1 Python libraries compared

| Library | License | Latest (PyPI) | Last commit | Method | Fit for 1–10 k series (batch) | Verdict |
|---|---|---|---|---|---|---|
| **statsforecast** (Nixtla) | Apache-2.0 | 2.1.1, 2026-07-16 | 2026-09-11 | AutoARIMA, ETS, Theta, **MSTL** (note 1) | vendor claims (note 2) | **Pragmatic pick** for seasonal baselines |
| **statsmodels** | BSD-3 | 0.15.0, 2026-08-27 | active | STL / MSTL decomposition, ETS | Python loop per series; OK for ≤ a few k series | Good alternative with few dependencies (STL residual + MAD) |
| **river** | BSD-3 | 0.26.1, 2026-08-21 | 2026-09-14 | online detectors and forecasters (note 3) | `learn_one`/`score_one` per sample; streaming; state per series in memory | Pick for **streaming**: score each scrape (note 4) |
| **PyOD** | BSD-2-Clause | 3.6.5, 2026-08-17 | 2026-09-08 | 61 detectors, plus time-series detectors (note 5) | fast per timestamp for multivariate checks (note 6) | Use for **multivariate / outliers across peers**, not seasonal baselines |
| **Prophet** | MIT | 1.4.0, 2026-08-15 | 2026-08-27 | additive trend + daily/weekly seasonality + holidays (Stan) | slow: seconds per series (Stan fit), so ~hours for 5 k each night | OK for < 500 business KPIs with holidays; not at scale |
| **Merlion** (Salesforce) | BSD-3 | 2.0.4, 2024-06-20 | 2026-03-11 (one-off), otherwise 2024 | ensembles, AutoML, many detectors | not assessed | **Stale**: no release in > 2 years; avoid for new work |
| **ADTK** (Arundo) | MPL-2.0 | 0.6.2, 2020-04-17 | 2020-04-17 | rule-based detectors | not assessed | **Dead** |
| **Luminaire** (Zillow) | Apache-2.0 | 0.4.3, 2024-01-31 | 2026-06-02 (only a continuous-integration change) | structural models + auto-config | not assessed | Dormant; avoid |
| **Kats** (Meta) | MIT | 0.2.0, 2022-03-15 | 2026-08-19 (note 7) | CUSUM, BOCPD, Prophet-based | not assessed | No PyPI release since 2022, so **effectively unmaintained** for open-source users |
| **sktime** | BSD-3 | 1.1.0, 2026-07-28 | active | one API for forecasting and detection (wraps statsforecast etc.) | framework overhead | Only if the team already uses it |
| **darts** (Unit8) | Apache-2.0 | 0.47.0, 2026-09-04 | 2026-09-07 | forecasting + `darts.ad` scorers; deep models | heavier dependencies (torch optional) | Optional for a later deep-learning iteration |

1. MSTL = multi-seasonal STL. statsforecast also gives prediction intervals, and in-sample intervals for anomaly detection. It uses numba and runs on Spark, Dask or Ray.
2. The README claims "1,000,000 series in 30 min" with Ray, and "500x faster than Prophet". These are vendor claims.
3. Online detectors: `HalfSpaceTrees`, `GaussianScorer`, `QuantileFilter`, `PredictiveAnomalyDetection`, `StandardAbsoluteDeviation`, LODA, LOF. Forecasters: `time_series.HoltWinters`, `SNARIMAX`.
4. river is weaker on seasonality, unless the features include the hour of the week.
5. 61 detectors, for example IForest, ECOD and COPOD. Time-series detectors: MatrixProfile, SpectralResidual, KShape, windowed bridge. PyOD also has ADEngine and MCP/agentic features.
6. For example: "is this service's vector of RED metrics weird?" at each timestamp.
7. Only internal Pyre type-suppression syncs.
8. Abbreviations in the table:
   - ARIMA = autoregressive integrated moving average. ETS = error, trend, seasonality (exponential smoothing).
   - IForest = [isolation forest](https://en.wikipedia.org/wiki/Isolation_forest). [ECOD](https://arxiv.org/abs/2201.00382) = outlier detection with empirical cumulative distribution functions. COPOD = copula-based outlier detection.
   - LODA = lightweight on-line detector of anomalies. LOF = local outlier factor.
   - [CUSUM](https://en.wikipedia.org/wiki/CUSUM) = cumulative sum. BOCPD = Bayesian online change-point detection.
   - MCP = Model Context Protocol. Stan = the statistical modelling engine that Prophet uses.
   - Licences: BSD = Berkeley Software Distribution licence. MPL = Mozilla Public License.

Sources: versions from the PyPI JSON API `https://pypi.org/pypi/<pkg>/json` for each library. Commit dates from `https://github.com/<o>/<r>/commits.atom`. Features from the READMEs: [statsforecast](https://raw.githubusercontent.com/Nixtla/statsforecast/main/README.md), [river anomaly module](https://raw.githubusercontent.com/online-ml/river/main/river/anomaly/__init__.py), [PyOD](https://raw.githubusercontent.com/yzhao062/pyod/master/README.rst).

**Pragmatic pick for robust seasonal baselines at scale:**
- Use **statsforecast `MSTL`** with `season_length=[288, 2016]`. For 5-min data this means daily + weekly seasons.
- Fit it each night on 2–4 weeks of recorded SLI series.
- **Score = |y − ŷ| / (k · MAD of the in-sample residuals).** Clip it to a normalised `anomaly_score` (> 1 = anomalous).

Why this pick:
- It is seasonal and robust (MAD).
- It runs in parallel across many series (`n_jobs`, or Spark/Dask/Ray per the README).
- It is Apache-2.0 and has active releases.

What to add, and what to skip:
- Add **river `HalfSpaceTrees`/`QuantileFilter`** only if we need streaming scores on every scrape.
- Add **PyOD ECOD/IForest** for multivariate vectors per service (RED + USE together).
- Skip Prophet at scale. Skip Merlion, ADTK, Luminaire and Kats.

**Card: external job based on statsforecast**
- **License:** Apache-2.0, plus MIT/Apache libraries for the connection.
- **What is free:** all of it.
- **Method:** MSTL forecast plus a robust residual z-score (MAD).
- **Scale:** 2–15 k series each night on 1 virtual machine (our estimate). The vendor claims 1 M series in 30 min on Ray. That is UNVERIFIED for our data.
- **Integration:** add-on. It reads `/api/v1/query_range` and exposes `/metrics` for Prometheus to scrape.
- **Effort to run:** medium. It is our own code (~300–500 lines), one container, with state in memory or in Parquet files.
- **Maturity:** statsforecast 2.1.1 (2026-07-16).
- **Limits:**
  - Weekly seasonality needs ≥ 2 weekly cycles.
  - Missing data and NaN values need handling.
  - Counters reset, so always feed recorded series based on `rate()`.
  - Holidays.
  - Deploy events cause level shifts. Suppress them with deploy annotations.
- **Sources:** above.

---

## 4. VictoriaMetrics vmanomaly

- **License:** proprietary and **Enterprise-only**. The FAQ (frequently asked questions) says: "`vmanomaly` is a part of enterprise package… You need to get a free trial license for evaluation". A license key has been required since v1.5.0 ([FAQ](https://raw.githubusercontent.com/VictoriaMetrics/VictoriaMetrics/master/docs/anomaly-detection/FAQ.md), [docs](https://docs.victoriametrics.com/anomaly-detection/)).
- **What is free:** a **2-month** Enterprise trial ([trial page](https://victoriametrics.com/products/enterprise/trial/)). There is no free tier. Pricing is not public (UNVERIFIED).
- **Method:** built-in models ([models.md](https://raw.githubusercontent.com/VictoriaMetrics/VictoriaMetrics/master/docs/anomaly-detection/components/models.md)):
  - AutoTuned.
  - Temporal Envelope, the preferred online model. It handles trends, changepoints, calendar patterns and holidays.
  - Online Z-score, Online MAD, Rolling Quantile, Online Seasonal Quantile, Seasonal Trend Decomposition.
  - Prophet, Isolation Forest, Holt-Winters (legacy offline models).
  - Custom Python models.
- **Output:** `anomaly_score` (0–1 normal, > 1 anomaly), `yhat`, `yhat_lower`, `yhat_upper`, `y`.
- **Scale:** it is designed around "queries" (PromQL/MetricsQL expressions). So the same pre-aggregation discipline applies. It has a multiprocessing mode.
- **Integration with our stack:**
  - *Reader*: `VmReader` calls `/query_range`. In the config, `datasource_url` has the comment "source victoriametrics/prometheus". So **reading from plain Prometheus works**, but features that exist only in MetricsQL are not available.
  - *Writer*: `VmWriter` pushes to **`/api/v1/import`** (the VictoriaMetrics JSON import). **Plain Prometheus has no such endpoint.** So write-back needs a small VictoriaMetrics single-node instance to store the scores.
  - Grafana would read that instance as a data source. Or Prometheus would federate `/federate` from it. This is UNVERIFIED end to end ([writer.md](https://raw.githubusercontent.com/VictoriaMetrics/VictoriaMetrics/master/docs/anomaly-detection/components/writer.md), [reader.md](https://raw.githubusercontent.com/VictoriaMetrics/VictoriaMetrics/master/docs/anomaly-detection/components/reader.md)).
  - This is still an **add-on** (a side store for a few k score series), not a core swap.
- **Effort to run:** low to medium: one container, a license and a VictoriaMetrics single-node.
- **Maturity:** v1.30.5 was released on 2026-09-10. Releases are frequent. It has a UI with an AI Copilot (an artificial-intelligence assistant) ([CHANGELOG](https://raw.githubusercontent.com/VictoriaMetrics/VictoriaMetrics/master/docs/anomaly-detection/CHANGELOG.md)).
- **Limits:**
  - The paid licence breaks our "free/OSS" goal.
  - When the licence expires, the service stops.
  - The writer works only with VictoriaMetrics.
- **Verdict:** the best turnkey engine if budget appears later, or if a later iteration moves to VictoriaMetrics. For iteration 1, copy its **output contract** and **model menu** into our free external job.

## 5. Netdata

- **License:**
  - The agent is **GPL-3.0+** (GNU General Public License). This covers collection, storage, ML, alerting and APIs.
  - The **Netdata UI is closed source**, under the "NCUL1" licence: "Closed-source but free to use with Netdata Agent and Cloud". It is delivered through a content delivery network (CDN).
  - Netdata Cloud is software as a service (SaaS). Sources: [README](https://raw.githubusercontent.com/netdata/netdata/master/README.md), [LICENSE](https://raw.githubusercontent.com/netdata/netdata/master/LICENSE).
- **What is free:** the agent and its ML, fully. Plans ([pricing page](https://www.netdata.cloud/pricing/)):
  - The Cloud **Community** plan allows "Max 5 Active Connected Nodes" and 1 custom dashboard per room.
  - Business costs $4.50 per node per month. "Netdata AI" (automated troubleshooting) is only in Business.
  - Enterprise On-Prem starts from 200 node licences.
- **Method:** unsupervised **k-means with k=2**, per metric (per dimension).
  - Each model trains on the last 4 h of preprocessed feature vectors. Netdata retrains every 3 h.
  - There are **18 models per dimension (~54 h)**. A point is anomalous only if **all** models agree: the distance is above the 99th percentile of the training data.
  - The result is an "anomaly bit" stored inside the sample, so it needs no extra storage.
  - Node and Dimension Anomaly Rate charts are under `anomaly_detection.*`. `/api/v1/data?...&options=anomaly-bit` returns them ([ml/README.md](https://raw.githubusercontent.com/netdata/netdata/master/src/ml/README.md), [ml-configuration.md](https://raw.githubusercontent.com/netdata/netdata/master/src/ml/ml-configuration.md)).
- **Scale at 1–10 M series:** **not realistic as a central anomaly-detection engine.**
  - The go.d `prometheus` collector defaults to `max_time_series: 2000` per job and `max_time_series_per_metric: 200`. The docs say: "If the final output exceeds it, the data is not processed" ([collector docs](https://raw.githubusercontent.com/netdata/netdata/master/src/go/plugin/go.d/collector/prometheus/integrations/prometheus_endpoint.md)).
  - Netdata is designed as one agent per node, plus parent nodes. Loading Prometheus' 1–10 M series again into Netdata parents means building a second TSDB. That is a swap in disguise.
- **Integration without a swap:** viable only as a **node-level add-on**.
  - Run the agent only on some critical hosts (database, ingress, Kafka). It does host and container anomaly detection locally.
  - Prometheus scrapes Netdata's exported anomaly-rate charts from `http://<node>:19999/api/v1/allmetrics?format=prometheus&source=as-collected` ([exporting docs](https://raw.githubusercontent.com/netdata/netdata/master/src/exporting/prometheus/README.md)).
  - Filter to the `anomaly_detection.*` contexts with `metric_relabel_configs`, so that Prometheus cardinality does not explode.
- **Effort to run:** low per node, but it is a second fleet of monitoring agents.
- **Maturity:** v2.11.0 from 2026-08-12, with commits every day ([releases](https://github.com/netdata/netdata/releases.atom)).
- **Limits:**
  - The UI is closed, and multi-node views need Cloud.
  - ML on each node costs CPU on every host ("While ML impacts CPU usage…").
  - The model works at 1 s granularity. This does not match Prometheus' 15–60 s series.
  - Anomaly rates are noisy at fleet scale without aggregation.
- **Verdict:** skip for iteration 1. Maybe pilot it on ≤ 5 critical hosts under the free Community plan.

---

## 6. Other open-source engines and nearby tools

| Tool | Status (verified) | Role for us |
|---|---|---|
| **Thanos** (sidecar + store + compactor) | Apache-2.0; v0.42.4 (2026-07-30) ([releases](https://github.com/thanos-io/thanos/releases.atom)) | **Truly additive** for long retention (note 1). Not anomaly detection. |
| **Grafana Mimir** | AGPL-3.0; 3.2.1 (2026-09-10) ([releases](https://github.com/grafana/mimir/releases.atom)) | Core **swap**, so a later iteration only. It would make data-source-managed rules editable in Grafana. |
| **VictoriaMetrics** (single/cluster) | Apache-2.0 core; v1.152.0 (2026-09-14) ([releases](https://github.com/VictoriaMetrics/VictoriaMetrics/releases.atom)) | Core swap, so a later iteration. A small single-node only as the vmanomaly score store (§4). |
| **Coroot** | Apache-2.0 (community edition); 1.26.0 (2026-09-07) ([releases](https://github.com/coroot/coroot/releases.atom)) | Service map + SLO/RCA, with Prometheus as storage (note 2) |
| **Robusta KRR** | MIT; v1.30.0 (2026-08-24) ([releases](https://github.com/robusta-dev/krr/releases.atom)) | Right-sizes Kubernetes resources from Prometheus history. **Not anomaly detection**, but a useful cost win. |
| **numalogic** (Intuit/numaproj) | Apache-2.0; 0.13.2 (2024-09-19), last commit 2024-09 ([PyPI](https://pypi.org/pypi/numalogic/json), [commits](https://github.com/numaproj/numalogic/commits.atom)) | Anomaly library for Prometheus-style metrics on Numaflow (note 3). **Stalled for 2 years**, do not adopt. |
| **Prometheus Anomaly Detector** (Red Hat AICoE) | last commit 2023-05-27, no releases ([commits](https://github.com/AICoE/prometheus-anomaly-detector/commits.atom)) | Dead, but the exact reference design for §3 (note 4) |
| **Skyline** (Etsy) | last commit 2015 ([commits](https://github.com/etsy/skyline/commits.atom)); fork earthgecko/skyline v4.0.0 2023-12 | Dead and built around Graphite. Do not use. |
| **Twitter AnomalyDetection** (R) | last commit 2015 ([commits](https://github.com/twitter/AnomalyDetection/commits.atom)) | Dead. Do not use. |
| **Time-series foundation models** (Amazon Chronos, Google TimesFM) | chronos-forecasting 2.3.2 (2026-09-08, Apache-2.0); timesfm 3.0.2 (2026-09-09, Apache-2.0) ([PyPI](https://pypi.org/pypi/chronos-forecasting/json), [PyPI](https://pypi.org/pypi/timesfm/json)) | **Later iteration** (note 5) |

1. Thanos: a sidecar next to each Prometheus uploads 2 h blocks to object storage.
   - It requires `--storage.tsdb.min-block-duration=2h` = `max-block-duration=2h`. This turns local compaction off.
   - It also requires unique `external_labels`, and recommends a local retention ≥ 6 h ([sidecar docs](https://raw.githubusercontent.com/thanos-io/thanos/main/docs/components/sidecar.md)).
   - Querier, Store and Compactor (downsampling to 5 m / 1 h) are extra components.
2. Coroot builds its service map with eBPF, a Linux kernel feature for tracing. It does SLOs and root cause analysis (RCA). The RCA research file covers it. It is not a metrics anomaly-detection add-on as such.
3. numalogic is an AIOps library, that is machine learning for IT operations.
4. It is the project of Red Hat's AI Center of Excellence (AICoE). It trains Prophet/Fourier models on metrics pulled from Prometheus. It serves `yhat`/`yhat_lower`/`yhat_upper` on `/metrics`, port 8080 ([README](https://raw.githubusercontent.com/AICoE/prometheus-anomaly-detector/master/README.md)).
5. Foundation models give zero-shot forecasts with intervals. They need no training per series. But each inference costs CPU or graphics card (GPU) time, so use them only for the top few hundred KPIs. Their accuracy against MSTL on our data is UNVERIFIED.

---

## 7. How to keep series count and cost low at 1–10 M series

**Principle: anomaly detection never touches raw series.** The data flows like this:
1. Raw 1–10 M series.
2. Recording rules turn them into **≈ 2–15 k input series** for anomaly detection.
3. Bands and scores: 9 derived series per `adaptive` input and 17 per `robust` input. That is ≈ 30–85 k for the 3–5 k target, and up to ≈ 135–255 k at 15 k.
4. Alerts.

For the 3–5 k target, this adds < 1 % head series at 10 M, and up to ≈ 9 % at 1 M.

### 7.1 What to monitor (the input set)
| Layer | Series per unit | Units (our assumption; real numbers not known) | Input series |
|---|---|---|---|
| Service RED: request rate, error ratio, latency p95 (p99 optional) | 3–4 | 200–500 services | 600–2 000 |
| Top-10 endpoints per critical service (RED) | 30 | 50 critical services | 1 500 |
| Saturation (USE) per cluster or node pool: CPU, memory, disk I/O, network errors | 4–5 | 50–200 pools | 250–1 000 |
| Queues and brokers (lag, consumer rate), databases (connections, replication lag, queries per second) | 2–3 | 100–300 | 300–900 |
| Business KPIs (orders/min, logins, payments) | 1 | 20–100 | 20–100 |
| **Total** | – | – | **≈ 2.7–5.5 k** (≤ 15 k with more endpoints) |

Series per node and per pod stay on **static thresholds or SLOs**, not on anomaly detection. There are too many of them, and they are too noisy. Use a `topk`-style drill-down only after a service-level anomaly fires.

### 7.2 Recording rules for RED and USE inputs (ready to copy)
```yaml
groups:
- name: ad-input-red            # evaluate every 1m
  interval: 1m
  limit: 20000                  # cardinality guard: a rule over the limit records nothing instead of exploding
  rules:
  - record: service:http_requests:rate5m
    expr: sum by (namespace, service) (rate(http_server_request_duration_seconds_count[5m]))
    labels: { anomaly_name: "svc_rps", anomaly_type: "requests" }
  - record: service:http_errors:ratio_rate5m
    expr: |
      sum by (namespace, service) (rate(http_server_request_duration_seconds_count{http_response_status_code=~"5.."}[5m]))
      / sum by (namespace, service) (rate(http_server_request_duration_seconds_count[5m]))
    labels: { anomaly_name: "svc_err_ratio", anomaly_type: "errors", anomaly_strategy: "robust" }
  - record: service:http_latency:p95_5m
    expr: histogram_quantile(0.95, sum by (namespace, service, le) (rate(http_server_request_duration_seconds_bucket[5m])))
    labels: { anomaly_name: "svc_p95", anomaly_type: "latency", anomaly_strategy: "robust" }
- name: ad-input-use
  interval: 1m
  rules:
  - record: pool:node_cpu:util_avg5m
    expr: 1 - avg by (cluster, nodepool) (rate(node_cpu_seconds_total{mode="idle"}[5m]))
    labels: { anomaly_name: "pool_cpu", anomaly_type: "resource" }
```
Notes on this snippet:
- Metric and label names are placeholders. Adapt them to our instrumentation: OpenTelemetry (OTel) semantic conventions, or the classic `http_requests_total`.
- Error ratio and latency are usually not normally distributed. So they use the `robust` strategy, following GitLab's normality warning (§1.2).
- We verified the syntax with `promtool check rules` on v3.14.0. We checked it together with the GitLab rules from §1.2 and the upstream `adaptive.yml`/`robust.yml` (14 and 21 rules, all SUCCESS).
- The remote_write/retention snippet in §7.3 passed `promtool check config`.

### 7.3 How to keep Prometheus healthy
- **Clean up at scrape time.** This reduces the 1–10 M base and frees room for rules. The tools ([configuration.md](https://raw.githubusercontent.com/prometheus/prometheus/v3.14.0/docs/configuration/configuration.md)):
  - `metric_relabel_configs`: `labeldrop` for pod-hash, uuid and URL-path labels, and `drop` for unused `_bucket`s.
  - Per-job `sample_limit`, `label_limit`, `label_value_length_limit` and `target_limit`. All default to 0 = unlimited.
- **Native histograms** (stable since 3.8.0) or `convert_classic_histograms_to_nhcb: true` collapse N `_bucket` series into 1. This is a big win for latency cardinality.
  - But the query syntax changes: `histogram_quantile(0.95, rate(m[5m]))`, without `_bucket`.
  - This breaks existing dashboards, so plan it as a separate change ([CHANGELOG](https://raw.githubusercontent.com/prometheus/prometheus/v3.14.0/CHANGELOG.md)).
- **Rule-evaluation budget:**
  - Rules within a group run one after another.
  - Enable `--enable-feature=concurrent-rule-eval` (`--rules.max-concurrent-evals`, default 4) for independent anomaly rules. It has no effect on groups with wildcard selectors, such as the upstream `select` rules (see §1.4).
  - Keep heavy seasonal rules in a separate group with `interval: 5m`, as promql-anomaly-detection does.
  - `--query.max-samples` also limits rule queries. Its default is 50 000 000 per query ([feature_flags.md](https://raw.githubusercontent.com/prometheus/prometheus/v3.14.0/docs/feature_flags.md), [command-line flags](https://raw.githubusercontent.com/prometheus/prometheus/v3.14.0/docs/command-line/prometheus.md)).
- **Rough evaluation cost** (estimate, UNVERIFIED by a benchmark):
  - `adaptive` bands on 5 k series ≈ 5 k × ~1.7 k samples/min (plus smaller terms) ≈ 9–10 M samples/min decoded. That is a fraction of one CPU core.
  - `robust` (2 × `quantile_over_time[1d]` every minute) ≈ 15 M samples/min, plus sorting. That is about 1 core.
  - Measure it with `prometheus_rule_group_last_duration_seconds`.
- **Where to run the anomaly rules.** If the main Prometheus is already busy (10 M series), run the anomaly rules on a **separate small "AD Prometheus"**. It receives only the input series. This adds a part and replaces nothing:
  ```yaml
  # main prometheus.yml
  remote_write:
  - url: http://ad-prometheus:9090/api/v1/write      # ad-prometheus started with --web.enable-remote-write-receiver
    write_relabel_configs:
    - source_labels: [__name__]
      regex: "(service|pool|slo|job):.*"
      action: keep
  ```
  Another option is hierarchical federation: `/federate?match[]={__name__=~"service:.*"}`. The docs name this as a use case for "aggregated data" ([federation.md](https://raw.githubusercontent.com/prometheus/prometheus/v3.14.0/docs/federation.md)).

  Who does what:
  - **Tagging and aggregation rules (§7.2) run on the main Prometheus.** They need raw data.
  - **Band, SLO and burn-rate rules run on the AD Prometheus.** They only read recorded series. The AD Prometheus also serves the Grafana band overlays and sends alerts to Alertmanager.
  - `promtool check config` passed for this remote_write + `storage.tsdb.retention.time: 90d` snippet (v3.14.0).

### 7.4 How much retention does seasonality need?
- What each method needs:
  - promql-anomaly-detection: ≥ 7 d.
  - GitLab 3-week median: ≥ 4 w.
  - Sloth 30 d SLO window: ≥ 30 d.
  - External MSTL fit: 2–4 w. "Same week last year" works only with Thanos.
- Storage math. Prometheus uses an "average of only 1–2 bytes per sample" ([storage.md](https://raw.githubusercontent.com/prometheus/prometheus/v3.14.0/docs/storage.md)).
  - 50 k recorded series at 1 sample/min ≈ 72 M samples/day ≈ **0.1 GB/day, so 90 d ≈ 10 GB**.
  - Raw 10 M series at 30 s ≈ 29 G samples/day ≈ **43 GB/day, so 35 d ≈ 1.5 TB**.
  - So keep the main Prometheus at 15 d, and give the AD Prometheus 90 d.
- A **Thanos sidecar** is the other additive route. It gives object storage, downsampling and "same week last year".
  - The cost: local compaction is off, and we must run Store, Querier and Compactor.
  - Choose it if we also want long-term raw metrics. Otherwise the AD Prometheus is simpler.

---

## 8. SLO burn-rate alerts (Sloth, Pyrra): Tier 0

Multi-window, multi-burn-rate (MWMB) SLO alerts are the **noise-free baseline**. The error budget is the amount of failure that the SLO allows. The burn rate is how fast we use it up. MWMB alerts:
- page only when the user-visible error budget burns fast,
- need no training,
- are pure Prometheus rules.

Anomaly detection then adds *early-warning* and *non-SLO* signals: traffic drops, changes in latency shape, saturation drift. These go to Slack or a ticket, not to the pager.

| Aspect | **Sloth** | **Pyrra** |
|---|---|---|
| License | Apache-2.0 ([LICENSE](https://raw.githubusercontent.com/slok/sloth/HEAD/LICENSE)) | Apache-2.0 ([LICENSE](https://raw.githubusercontent.com/pyrra-dev/pyrra/HEAD/LICENSE)) |
| Latest | v0.16.0 (2026-04-04); last commit 2026-05-26 ([releases](https://github.com/slok/sloth/releases.atom)) | v0.10.1 (2026-06-25); last commit 2026-08-22 ([releases](https://github.com/pyrra-dev/pyrra/releases.atom)) |
| How it works | Command-line tool (CLI) generates a Prometheus rule file (note 1) | Operator generates recording rules; has a **web UI** (note 2) |
| Rules per SLO | **15 recording + 2 alerts** in the generated example (note 3) | Burn-rate recording rules per window + **4 MWMB alerts** (note 4) |
| Fit here | Best for rule files managed in Git (GitOps) on plain Prometheus; no Kubernetes needed | Best if the team wants an SLO UI; filesystem mode works without Kubernetes |

1. `sloth generate -i slo.yml` → Prometheus rule file. Sloth also has a Kubernetes operator with custom resource definitions (CRDs), OpenSLO v1alpha support and SLI plugins.
2. The Pyrra operator runs in Kubernetes or in **filesystem** mode. The web UI lists SLOs sorted by remaining budget and shows burn-rate graphs. Grafana dashboards come via `--generic-rules`. Pyrra is Thanos-aware.
3. The rules are `slo:sli_error:ratio_rate{5m,30m,1h,2h,6h,1d,3d,30d}`, plus 7 metadata rules and the page/ticket alerts ([getting-started.yml, generated](https://raw.githubusercontent.com/slok/sloth/main/examples/_gen/getting-started.yml)).
4. Rules like `…:burnrate3m/15m/30m/1h/…`. The 4 MWMB alerts have different severities ([README](https://raw.githubusercontent.com/pyrra-dev/pyrra/main/README.md)).

**The Sloth page alert.** Here is its structure, copied as is, for a 99.9 % SLO (error budget 0.001):
`(err_ratio_5m > 14.4*0.001 and err_ratio_1h > 14.4*0.001) or (err_ratio_30m > 6*0.001 and err_ratio_6h > 6*0.001)`
- The ticket alert uses factor 3 with the 2h/1d windows, and factor 1 with the 6h/3d windows ([getting-started.yml, generated](https://raw.githubusercontent.com/slok/sloth/main/examples/_gen/getting-started.yml)).
- Its 30 d SLI is `sum_over_time(slo:sli_error:ratio_rate5m[30d]) / count_over_time(...[30d])`. So it needs **30 d retention of the 5 m recorded series**. This is another reason for the AD Prometheus in §7.3.
- The page factors 14.4× and 6×, and the 1× (3 d/6 h) ticket, follow Table 5-8 of the Site Reliability Engineering [SRE Workbook](https://sre.google/workbook/alerting-on-slos/). The extra 3× (1 d/2 h) ticket tier is Sloth's own addition.

**Integration warning.** Sloth copies the `labels:` of the SLO spec onto every rule and alert. The getting-started spec sets `tier: "2"` and `severity: pageteam`.
- With this repo's Alertmanager routing, set `page_alert.labels.severity: page`.
- Do not use a `tier` label in SLO specs.
- Otherwise a Sloth page lands on the `tier=~"1|2"` chat route instead of the pager ([getting-started.yml spec](https://raw.githubusercontent.com/slok/sloth/main/examples/getting-started.yml)).

**Cost:** ~17 rules × ~300 SLOs ≈ 5 k rules. Each rule produces 1 series, so ~5 k series. That is very small next to 1–10 M.

## 9. What we recommend for metrics

Everything below is **add-on only**: extra rule files, one small extra Prometheus and one small Python container. We change nothing in the scrapers, the Grafana edition or the main TSDB.

The tiers in this table are **method tiers**. They map to the tiers T0–T3 of the [reference architecture](../reference-architecture.md#what-each-tier-does) like this: 0 → T0, 1 → T1, 2 → T2, 2b → T3. The `predict_linear` alerts of tier 0.5 belong to T0. Tier 3 lists later options and has no T-tier.

| Method tier | What | Tools (licence) | Effort | Output / routing |
|---|---|---|---|---|
| **0 — SLO baseline** (week 1–2) | MWMB burn-rate alerts for the top 10–20 user-facing services (note 1) | Sloth v0.16.0 (Apache-2.0) as CLI → rule files in Git; Pyrra if an SLO UI is wanted | 2–4 days | **Only tier that pages.** 14.4×/6× page, 3×/1× ticket |
| **0.5 — hygiene** (in parallel) | Cardinality audit, per-job limits, relabel drops, `predict_linear` capacity alerts (note 2) | Prometheus (Apache-2.0) | 2–3 days | Ticket |
| **1 — PromQL bands** (week 2–4) | ≈ 3–5 k input recording rules → **AD Prometheus** → upstream bands (note 3) | promql-anomaly-detection v0.2.1 (Apache-2.0), Prometheus 3.14 | 1–2 weeks incl. tuning | Slack/ticket, `severity: warning` (note 4) |
| **2 — external scorer** (month 2) | Python job: nightly statsforecast **MSTL** fit, per-minute scoring (note 5) | statsforecast 2.1.1, PyOD 3.6.5, prometheus_client 0.26.0 (all permissive) | 2–3 weeks | Same routing (note 6) |
| **2b — LLM triage** (month 2+) | On a *grouped* alert, send a small bundle to an LLM (note 7) | any LLM API; cap N bundles/hour | 1 week | Posted to the incident channel; never raw data |
| **3 — later iterations** | Thanos, native histograms, a Mimir/VictoriaMetrics swap, Chronos/TimesFM (note 8) | not chosen yet | not estimated | not decided |

1. Availability and latency SLIs. The top 10–20 services match the roadmap. We extend the list later.
2. Hygiene tasks:
   - Cardinality audit: the TSDB status page and `prometheus_tsdb_head_series`.
   - `sample_limit`/`label_limit` per job, and `metric_relabel_configs` drops.
   - `predict_linear` capacity alerts: disk, quotas, cert expiry.
3. Tier 1 steps:
   - Build ≈ 3–5 k input recording rules (§7.1–7.2).
   - Send them to the **AD Prometheus** (remote_write keep-filter, 90 d retention). `concurrent-rule-eval` helps only the seasonal groups.
   - Run grafana/promql-anomaly-detection: `adaptive` for traffic and resources, `robust` for errors and latency.
   - After 4 weeks, add the GitLab median-of-3-weeks prediction for the top traffic series.
4. Grafana shows band overlays. Alertmanager `inhibit_rules` mute a service's anomaly alerts while its SLO alert fires.
5. The job fits daily + weekly seasons on the input series each night, and scores every minute. It exports `/metrics` with `anomaly_score` (> 1 = anomalous), `yhat`, `yhat_lower` and `yhat_upper`. PyOD (ECOD/IForest) on per-service RED + USE vectors finds multivariate outliers.
6. Alert = `anomaly_score > 1` for 10 m, **and** at least 1 other signal of the same service agrees.
7. How the LLM triage works:
   - Alertmanager groups alerts with `group_by: [namespace, service]`.
   - We build a bundle of ≤ 5–10 KB. It holds:
     - the anomalous series and its band
     - the 3–5 most-correlated anomaly series
     - recent deploy and config-change events
     - the related SLO burn.
   - We ask the LLM for a hypothesis and the next query to run.
8. Later options:
   - Thanos (sidecar + store + compactor) for long retention and downsampling.
   - Migration to native histograms.
   - A Mimir or VictoriaMetrics swap. Then vmanomaly becomes an option, if there is budget.
   - Chronos/TimesFM zero-shot forecasts for the top KPIs.

**Why this order:**
- SLO alerts give low-noise paging from day one.
- PromQL bands give seasonal anomaly detection with no new runtime, and only ≈ 30–90 k extra series (< 1–9 % of head series).
- The Python scorer is worth it only after the bands show where PromQL is too blunt: several seasons, holidays, multivariate checks.

**Suggested success criteria for the PoC.** These targets are proposals, not sourced benchmarks.
1. Input and band series stay within the §7 budget: ≈ 30–90 k, < 1 % of head series at 10 M, ≤ 9 % at 1 M.
2. For the anomaly rule groups, `prometheus_rule_group_last_duration_seconds` < 50 % of the interval, and `prometheus_rule_group_iterations_missed_total` = 0.
3. Over 2 weeks, on-call judges ≥ 60–70 % of anomaly warnings "worth knowing", with ≤ 5 anomaly warnings/day/team.
4. At least 1 incident where anomaly detection fired before the SLO alert.

**Key risks:**
- Warm-up: 24–26 h for bands, 4 w for the GitLab style, 2 w for weekly MSTL.
- Level shifts from holidays and deploys. Reduce them with a median of weeks and by suppressing alerts around deploy events.
- Error and latency ratios are not normally distributed. Use `robust`.
- Experimental PromQL functions may change. Avoid them in alert paths.
