# Glossary

**In short:**

- This page explains, in plain English, the abbreviations and technical terms that the other
  files in this repo use.
- Terms are grouped by topic. Inside each group they are in alphabetical order. Use your
  browser's search (Ctrl+F) to find a term quickly.
- "Learn more" points to the original paper, the official docs, or Wikipedia for basic
  statistics. Terms that we made up for this project point to our own files.

Sections:
[Our project terms](#our-project-terms) ·
[Detection methods and statistics](#detection-methods-and-statistics) ·
[Benchmarks and model types](#benchmarks-and-model-types) ·
[Metrics (Prometheus, Grafana)](#metrics-prometheus-grafana) ·
[Logs (Elastic, Filebeat)](#logs-elastic-filebeat) ·
[Traces (Jaeger, OpenTelemetry)](#traces-jaeger-opentelemetry) ·
[Alerting and operations](#alerting-and-operations) ·
[Kubernetes and containers](#kubernetes-and-containers) ·
[LLMs and cost](#llms-and-cost) ·
[Licences](#licences) ·
[General computing terms](#general-computing-terms)

## Our project terms

| Term | Meaning | Learn more |
|---|---|---|
| AD Prometheus | Anomaly-detection Prometheus: an optional small second Prometheus. It receives only the service-level recording rules by `remote_write` and keeps them 90 days. | [reference architecture](reference-architecture.md#notes-on-the-diagram) |
| anomaly input | A service-level series tagged for detection with the labels `anomaly_name`, `anomaly_type` and, optionally, `anomaly_strategy`. We plan 3–5 k of them. | [research/02 §1.1](research/02-metrics-prometheus.md) |
| anomaly score floor | The scale is the largest of 1.4826 × MAD of past residuals and the floors, so flat series do not give huge scores. Log-count floors: 10 % of expected, 1 line, √expected. | [anomalyd/README.md](../anomalyd/README.md) |
| `anomaly_score` | Our tier-2 scorer's output: a signed robust z-score of the latest point. The PoC alert fires when the absolute score is above 4. In vmanomaly, the same name means anomalous above 1. | [prom_anomaly_job.py](../poc/ml/prom_anomaly_job.py) |
| anomalyd | Our own small Go engine, a second PoC. `anomalyd agent` mines log templates on each node. `anomalyd server` scores series and sends findings. | [anomalyd/README.md](../anomalyd/README.md) |
| backfill | Loading the past values of a series once, at start-up, so that scoring can begin at once. anomalyd backfills from Prometheus with `query_range`. | [anomalyd/README.md](../anomalyd/README.md) |
| bundle | The context of one Alertmanager alert group, sent to the LLM in one call: alert labels, related series, template changes, trace IDs and recent deploys. | [reference architecture, note 4](reference-architecture.md#what-each-tier-does) |
| e2e test (end-to-end test) | Our Docker test of anomalyd with real Prometheus 3.14.0 and Alertmanager 0.34.0. The logs are written as CRI container stdout. | [anomalyd verification](../anomalyd/README.md#verification-2026-09-15) |
| finding | An anomaly alert from tier 1 or tier 2. It carries the label `tier: "1"` or `"2"` and goes to chat or tickets, never to a pager. | [reference architecture](reference-architecture.md#what-each-tier-does) |
| gotcha | A surprising problem that we hit while testing. poc/README lists them with their fixes. | [poc/README.md](../poc/README.md#gotchas-found-while-validating) |
| incident log | Our list of past incidents with timestamps. Iteration 1 must create it, so that we can measure iteration 2 against it. | [roadmap](roadmap.md#exit-criteria-for-iteration-1) |
| `.internal` host names | Placeholder host names in our examples, such as `prometheus.internal`. Replace them with your own hosts. | — |
| iteration 1 | Weeks 1–4 of the plan. We add free components only and swap nothing. Only SLO burn-rate alerts page a person. | [roadmap](roadmap.md#iteration-1-add-ons-only-weeks-14) |
| iteration 2 | Months 2–3 of the plan. We add a second-stage scorer, a log-mining pilot and LLM triage with a $100/month cap. | [roadmap](roadmap.md#iteration-2-second-stage-confirmation-and-llm-triage-months-23) |
| iteration 3 | Quarter 2 and later. Optional swaps and heavier models, only if the numbers from iterations 1–2 justify them. | [roadmap](roadmap.md#iteration-3-optional-swaps-and-heavier-models-quarter-2) |
| log rollup | The Elasticsearch index `anomaly-logs-tmpl-1m`. A transform fills it with line counts per template per minute. | [reference architecture](reference-architecture.md#how-the-data-flows) |
| method tiers (research/02, research/05) | The tier numbers in research/02 §9 and research/05 §6. They group methods by cost and are not our detection tiers T0–T3. research/02 has tiers 0, 0.5, 1, 2, 2b (LLM triage) and 3. In research/05, Tier 2 is machine learning on the top-K series and Tier 3 is foundation models and LLMs. | [research/05 §6](research/05-methods-benchmarks.md#6-method-tiers-for-our-stack) |
| our stack | Filebeat → Elasticsearch/Kibana on the free Basic licence, Prometheus + Grafana OSS + Alertmanager, and Jaeger. 200 GB–2 TB/day of logs, 1–10 M series. | [README](../README.md#what-we-want) |
| path A / B / C | Our three ways to turn log lines into counts per template. A: in Elasticsearch (iteration 1). B: OTel on each host, into Prometheus. C: anomalyd. | [reference architecture](reference-architecture.md#three-ways-to-turn-logs-into-counts) |
| pilot | A limited trial on a few hosts or services before we decide on a full rollout. Paths B and C, and anomalyd, are pilots. | [roadmap](roadmap.md#2-logs-path-b-or-c-pilot) |
| PoC (proof of concept) | A small working version that tests an idea. Our PoC configs and code are validated on synthetic data, not in production. | [poc/README.md](../poc/README.md) |
| scorer (tier-2 scorer) | The program that compares each top-K series with its seasonal median and MAD. It exists in Python (`prom_anomaly_job.py`) and in Go (anomalyd). | [roadmap](roadmap.md#1-tier-2-scorer) |
| second reader | A second program that reads the same container log files as Filebeat, for example the OTel `file_log` receiver or the anomalyd agent. Filebeat stays unchanged. | [reference architecture](reference-architecture.md#notes-on-the-diagram) |
| Step 0 | A half-day check before iteration 1. We read the version, licence and features of each tool, and collect an inventory. | [roadmap](roadmap.md#step-0-confirm-what-is-actually-running-half-a-day) |
| synthetic data | Generated test data, not our real logs or metrics. All our measurements so far use synthetic data. | [Wikipedia](https://en.wikipedia.org/wiki/Synthetic_data) |
| T0 | Detection tier 0: rules and SLO burn-rate alerts, plus heartbeat, flatline and `predict_linear` alerts. The only tier that pages a person. | [reference architecture](reference-architecture.md#what-each-tier-does) |
| T1 | Detection tier 1: cheap statistics on all aggregates. PromQL bands for metrics, ElastAlert2 rules and a novelty alert for logs. Findings go to chat. | [reference architecture](reference-architecture.md#what-each-tier-does) |
| T2 | Detection tier 2: the seasonal-median + MAD scorer on the top-K series. It confirms T1 findings as a second stage and never pages. In research/02 and research/05, "tier 2" means heavier methods (see "method tiers"). | [reference architecture](reference-architecture.md#what-each-tier-does) |
| T3 | Detection tier 3: one LLM summary per Alertmanager alert group, on demand, capped at ≤ $100/month. Design only so far. research/02 calls this step tier 2b (see "method tiers"). | [reference architecture](reference-architecture.md#what-each-tier-does) |
| template key, template id | A stateless id of a log line's template. Filebeat masks the variable parts with `replace` and hashes the rest with `fingerprint`. Every host gets the same key. | [README, finding 1](../README.md#findings) |
| tier (detection tier) | A group of detectors with the same cost and the same alert route (T0–T3). Do not confuse it with OTel tier 1 and tier 2 (see "collector tier"), or with the method tiers in research/02 and research/05. | [reference architecture](reference-architecture.md#scope) |
| top-K series | A short list of the most useful series, chosen from the T1 series that proved useful. Only these go to the tier-2 scorer. | [roadmap](roadmap.md#1-tier-2-scorer) |
| UNVERIFIED | Our marker for a claim that we could not confirm from a primary source, such as vendor docs, source code or a package registry. | [verification review](verification-review.md) |
| warm-up | The time a detector needs before its baseline is useful. PromQL bands need 24–26 h. Weekly seasons need at least 7 days of history. | [research/02 §1.1](research/02-metrics-prometheus.md) |

## Detection methods and statistics

| Term | Meaning | Learn more |
|---|---|---|
| adaptive band | A band of mean ± 2 standard deviations, with a daily and weekly look-back. One of the two strategies in `grafana/promql-anomaly-detection`. | [Grafana blog](https://grafana.com/blog/2024/10/03/how-to-use-prometheus-to-efficiently-detect-anomalies-at-scale/) |
| ARIMA (autoregressive integrated moving average) | A classic model that forecasts one series from its own past values and past forecast errors. | [Wikipedia](https://en.wikipedia.org/wiki/Autoregressive_integrated_moving_average) |
| band | The normal range of a series, learned from its own history. A value outside the band is an anomaly candidate. | [promql-anomaly-detection](https://github.com/grafana/promql-anomaly-detection) |
| BOCPD (Bayesian online change-point detection) | A change detector. After each new point, it updates how likely it is that the series has just changed its behaviour. | [paper](https://arxiv.org/abs/0710.3742) |
| change point detection | Finding the moment when a series changes its behaviour: a step, a spike, a dip or a trend change. | [Wikipedia](https://en.wikipedia.org/wiki/Change_detection) |
| cold-start series | A new series with little or no history, so seasonal methods cannot score it yet. anomalyd uses a recent median until a day of data exists. | [anomalyd/README.md](../anomalyd/README.md) |
| COPOD, LODA | Two more outlier detectors in the PyOD library. COPOD: copula-based outlier detection. LODA: lightweight on-line detector of anomalies. | [README](https://raw.githubusercontent.com/yzhao062/pyod/master/README.rst) |
| CoV (coefficient of variation) | The standard deviation divided by the mean. It compares the spread of series of very different sizes. | [Wikipedia](https://en.wikipedia.org/wiki/Coefficient_of_variation) |
| CUSUM (cumulative sum) | A change detector. It adds up the deviations from a target and alerts when the sum grows too large. | [Wikipedia](https://en.wikipedia.org/wiki/CUSUM) |
| DAMP (Discord Aware Matrix Profile) | A fast Matrix Profile method that finds discords in a stream of data. | [paper](https://www.cs.ucr.edu/~eamonn/DAMP_long_version.pdf) |
| DBSCAN (density-based spatial clustering of applications with noise) | A clustering method. Points far from any dense group are outliers. | [Wikipedia](https://en.wikipedia.org/wiki/DBSCAN) |
| ECOD (empirical cumulative distribution-based outlier detection) | An outlier detector that checks how extreme each value is within its own distribution. It needs no training labels. | [paper](https://arxiv.org/abs/2201.00382) |
| ETS (error, trend, seasonality) | A family of forecast models based on exponential smoothing. Holt-Winters belongs to it. | [Wikipedia](https://en.wikipedia.org/wiki/Exponential_smoothing) |
| EVT (extreme value theory) | Statistics of rare, extreme values. SPOT uses it to set an alert threshold automatically. | [Wikipedia](https://en.wikipedia.org/wiki/Extreme_value_theory) |
| EWMA (exponentially weighted moving average) | An average in which recent values count more than old ones. | [Wikipedia](https://en.wikipedia.org/wiki/Exponential_smoothing) |
| forecast-residual detection | A model forecasts the next values. The anomaly score is the forecast error, that is, the residual. | [research/05](research/05-methods-benchmarks.md#how-to-read-the-scores) |
| Half-Space Trees | A streaming anomaly detector built from random trees. It is part of the `river` Python library. | [river docs](https://riverml.xyz/latest/api/anomaly/HalfSpaceTrees/) |
| Holt-Winters | A forecast by exponential smoothing with three parts: level, trend and season. Holt's linear method has only level and trend. | [statsmodels](https://www.statsmodels.org/stable/generated/statsmodels.tsa.holtwinters.ExponentialSmoothing.html) |
| isolation forest (IForest) | An outlier detector. Random splits isolate unusual points faster than normal ones. | [Wikipedia](https://en.wikipedia.org/wiki/Isolation_forest) |
| k-means | Clustering into k groups around their centres. Points far from every centre are anomalies. | [Wikipedia](https://en.wikipedia.org/wiki/K-means_clustering) |
| KNN (k-nearest neighbours) | Scores a point by its distance to its k closest neighbours. A point far from all of them is unusual. | [Wikipedia](https://en.wikipedia.org/wiki/K-nearest_neighbors_algorithm) |
| LOESS (locally estimated scatterplot smoothing) | Smoothing by fitting many small local regressions. It is the "L" in STL and MSTL. | [Wikipedia](https://en.wikipedia.org/wiki/Local_regression) |
| LOF (local outlier factor) | An outlier detector that compares the density around a point with the density around its neighbours. | [Wikipedia](https://en.wikipedia.org/wiki/Local_outlier_factor) |
| MAD (median absolute deviation) | The median of the distances from the median. A measure of spread that a few outliers barely change. For normal data, 1.4826 × MAD ≈ σ. | [Wikipedia](https://en.wikipedia.org/wiki/Median_absolute_deviation) |
| Matrix Profile, discord | Matrix Profile finds, for each part of a series, its most similar other part. A discord is the part least like all the rest. The STUMPY library computes it. | [STUMPY tutorial](https://stumpy.readthedocs.io/en/latest/Tutorial_STUMPY_Basics.html) |
| MSTL (Multiple Seasonal-Trend decomposition using LOESS) | Splits a series into a trend, several seasons (for example daily and weekly) and a residual. Anomalies show up in the residual. | [paper](https://arxiv.org/abs/2107.13462) |
| novelty detection | Alerting when something appears that was never seen before, for example a new log template. | [Wikipedia](https://en.wikipedia.org/wiki/Novelty_detection) |
| OCSVM (one-class support vector machine) | Learns a boundary around normal data. Points outside the boundary are anomalies. | [Wikipedia](https://en.wikipedia.org/wiki/One-class_classification) |
| onset detector | A detector that fires at the start of a change, then adapts to the new level and stops. The upstream adaptive bands behave this way. | [poc gotchas](../poc/README.md#gotchas-found-while-validating) |
| PCA (principal component analysis), Sub-PCA | PCA finds the main directions of variation in data. Sub-PCA applies it to windows of a series. Windows far from those directions are anomalies. | [Wikipedia](https://en.wikipedia.org/wiki/Principal_component_analysis) |
| percentile, p95 | The value below which that percentage of observations fall. p95: 95 % of the values are lower. | [Wikipedia](https://en.wikipedia.org/wiki/Percentile) |
| Poisson floor | A minimum scale of √expected for count data. Random noise in counts is about √expected. | [Wikipedia](https://en.wikipedia.org/wiki/Poisson_distribution) |
| Prophet | A forecasting library from Meta. It models a trend, seasons and holidays. | [Prophet](https://facebook.github.io/prophet/) |
| PyOD | A Python library with many outlier-detection algorithms, such as isolation forest, ECOD and LOF. | [README](https://raw.githubusercontent.com/yzhao062/pyod/master/README.rst) |
| RCF (Random Cut Forest) | A streaming, tree-based anomaly detector. OpenSearch uses it. | [paper](https://proceedings.mlr.press/v48/guha16.html) |
| residual | What is left after removing the expected value, for example the trend and the seasons. Large residuals are anomaly candidates. | [FPP3 decomposition](https://otexts.com/fpp3/decomposition.html) |
| river | A Python library for machine learning on streams. It includes Half-Space Trees. | [README](https://raw.githubusercontent.com/online-ml/river/main/README.md) |
| robust band | A band of median ± 2·MAD over a 1-day window, with a daily and weekly look-back. Outliers move it less than the adaptive band. | [Grafana blog](https://grafana.com/blog/2024/10/03/how-to-use-prometheus-to-efficiently-detect-anomalies-at-scale/) |
| robust z-score | Like a z-score, but with the median and MAD instead of the mean and σ. Our scorer divides (observed − seasonal median) by the usual error: 1.4826 × MAD of past residuals, never below a floor. | [Wikipedia: MAD](https://en.wikipedia.org/wiki/Median_absolute_deviation) |
| seasonal median | The median of the same time slot in past seasons, for example the same 5-minute slot in past weeks. Our scorer uses it as the expected value. | [anomalyd/README.md](../anomalyd/README.md) |
| seasonal naive forecast | A forecast that repeats the value from the same time in the last season, for example last week. | [FPP3 simple methods](https://otexts.com/fpp3/simple-methods.html) |
| seasonality, season | A pattern that repeats, usually daily or weekly. A season is one period of that pattern. | [FPP3 decomposition](https://otexts.com/fpp3/decomposition.html) |
| SPC (statistical process control) | Control charts with an upper and a lower control limit. A value outside the limits needs a look. | [Wikipedia](https://en.wikipedia.org/wiki/Statistical_process_control) |
| Spectral Residual (SR) | A fast detector from Microsoft. It finds points where the frequency spectrum of the series looks unusual. | [paper](https://arxiv.org/abs/1906.03821) |
| SPOT, DSPOT (streaming peaks-over-threshold) | Not a detector itself: it sets an alert threshold from the tail of any score stream. It fits a GPD (generalized Pareto distribution). DSPOT also follows drift. | [libspot](https://asiffer.github.io/libspot) |
| standard deviation (σ, stddev) | The usual distance of values from their mean. | [Wikipedia](https://en.wikipedia.org/wiki/Standard_deviation) |
| statsforecast | A Python forecasting library from Nixtla. It includes MSTL. | [README](https://raw.githubusercontent.com/Nixtla/statsforecast/main/README.md) |
| STL (Seasonal-Trend decomposition using LOESS) | Splits a series into a trend, one season and a residual. | [statsmodels](https://www.statsmodels.org/stable/generated/statsmodels.tsa.seasonal.STL.html) |
| yhat, yhat_lower, yhat_upper | A forecast value and its lower and upper limits. Prophet and vmanomaly use these names. | [Prophet](https://facebook.github.io/prophet/) |
| z-score (standard score) | How many standard deviations a value lies from the mean. | [Wikipedia](https://en.wikipedia.org/wiki/Standard_score) |

## Benchmarks and model types

| Term | Meaning | Learn more |
|---|---|---|
| AUC-PR (area under the precision-recall curve) | Rates an anomaly score over all thresholds. It runs from 0 to 1, and higher is better. | [Wikipedia](https://en.wikipedia.org/wiki/Precision_and_recall) |
| AUC-ROC (area under the ROC curve) | Like AUC-PR, but for the receiver operating characteristic (ROC) curve: true-positive rate against false-positive rate. 0 to 1, higher is better. | [Wikipedia](https://en.wikipedia.org/wiki/Receiver_operating_characteristic) |
| back-test | Replaying our past data, for example the incident log, to check whether a method would have caught the incidents. | [roadmap](roadmap.md#iteration-3-optional-swaps-and-heavier-models-quarter-2) |
| BOOM | Datadog's public benchmark for forecasting observability data, with about 350M points. | [dataset](https://huggingface.co/datasets/Datadog/BOOM) |
| CNN (convolutional neural network) | A neural network that learns local patterns with small sliding filters. | [Wikipedia](https://en.wikipedia.org/wiki/Convolutional_neural_network) |
| CRPS (continuous ranked probability score) | Rates a forecast that is given as a range (quantiles). Lower is better. | [Wikipedia](https://en.wikipedia.org/wiki/Scoring_rule#Continuous_ranked_probability_score) |
| deep learning, neural network | Models built from many layers of learned weights, for example LSTM, VAE or Transformer models. | [Wikipedia](https://en.wikipedia.org/wiki/Deep_learning) |
| event-level precision and recall | Precision and recall counted per incident, not per time point. We use them to judge tier 2 on our incident log. | [roadmap](roadmap.md#1-tier-2-scorer) |
| F1 score | One number that combines precision and recall: their harmonic mean. | [Wikipedia](https://en.wikipedia.org/wiki/F-score) |
| fine-tuned (FT) | A pretrained model that got extra training for one task. | [research/05](research/05-methods-benchmarks.md#how-to-read-the-scores) |
| foundation model (FM) | A large model pretrained on many datasets, then used on new data without training on it. Most time-series foundation models forecast. | [Wikipedia](https://en.wikipedia.org/wiki/Foundation_model) |
| GA, PA, FGA, FTA (log parser scores) | Loghub-2.0 scores for log parsers. GA, group accuracy: lines in the right group. PA, parsing accuracy: lines parsed exactly right. FGA, FTA: F1 of group and template accuracy. | [paper](https://arxiv.org/abs/2308.10828) |
| HDFS, BGL, Thunderbird, Spirit, ADFA | Public log datasets used in log anomaly detection papers. HDFS: Hadoop Distributed File System. BGL: Blue Gene/L supercomputer. ADFA: an intrusion-detection dataset. | [Loghub](https://github.com/logpai/loghub) |
| Loghub, Loghub-2.0 | Public collections of real system logs, used to test log parsers and log anomaly detectors. Some datasets have anomaly labels. | [Loghub](https://github.com/logpai/loghub) |
| LSTM (long short-term memory) | A neural network for sequences that can remember earlier inputs. | [Wikipedia](https://en.wikipedia.org/wiki/Long_short-term_memory) |
| MASE (mean absolute scaled error) | Rates a forecast of single values, compared with a naive forecast. Lower is better. | [Wikipedia](https://en.wikipedia.org/wiki/Mean_absolute_scaled_error) |
| mTSBench | A benchmark for multivariate time-series anomaly detection (TMLR 2026). | [paper](https://arxiv.org/abs/2506.21550) |
| multivariate, MTS (multivariate time series) | Several related series analysed together. | [research/05](research/05-methods-benchmarks.md#how-to-read-the-scores) |
| other TSB-AD methods | More detectors in the TSB-AD ranking. CBLOF: cluster-based LOF. DWT: discrete wavelet transform. MCD: minimum covariance determinant. USAD: an autoencoder. Also KMeansAD, KShapeAD, POLY. | [TSB-AD methods](https://github.com/TheDatumOrg/TSB-AD#detection-algorithm) |
| PA (point adjustment), PA-F1 | If a detector flags one point inside an anomaly range, PA counts the whole range as found. This inflates F1: even random scores can look good. | [paper (Kim et al.)](https://arxiv.org/abs/2109.05257) |
| paper venues (NeurIPS, PVLDB, ICSE, ISSRE …) | The conferences and journals where the cited papers appeared. research/05 spells them out. | [research/05](research/05-methods-benchmarks.md#where-the-cited-papers-were-published) |
| precision, recall | Precision: the share of alerts that are real. Recall: the share of real anomalies or incidents that were found. | [Wikipedia](https://en.wikipedia.org/wiki/Precision_and_recall) |
| SMD, SMAP, MSL, MGAB, ECG, SWaT | Public time-series anomaly datasets. SMD: Server Machine Dataset. SMAP, MSL: NASA spacecraft data. MGAB: Mackey-Glass anomaly benchmark. ECG: electrocardiogram heart signals. SWaT: Secure Water Treatment. | — |
| T5 (Text-to-Text Transfer Transformer) | A Google language-model design. Chronos and MOMENT build on it. | — |
| TAB | A time-series anomaly detection benchmark (PVLDB 2025). | [GitHub](https://github.com/decisionintelligence/TAB) |
| time-series foundation models (Chronos, TimesFM, MOMENT, Moirai, Toto) | The foundation models that research/05 compares. Most of them forecast. Their weight licences differ, so check each one. | [research/05 §2](research/05-methods-benchmarks.md#2-foundation-models-feasible-on-aggregates-weak-evidence) |
| TSB-AD | A public benchmark for time-series anomaly detection (NeurIPS 2024): 1,070 curated series from 40 datasets, 40 algorithms. Its main score is VUS-PR. | [TSB-AD](https://github.com/TheDatumOrg/TSB-AD) |
| TSB-AD-U, TSB-AD-M | The univariate (350 series) and multivariate (180 series) evaluation sets of TSB-AD, each with its own ranking. | [TSB-AD](https://github.com/TheDatumOrg/TSB-AD) |
| TSB-AutoAD | A benchmark from the TSB-AD group for automatic model selection and ensembles in anomaly detection. | [GitHub](https://github.com/thedatumorg/TSB-AutoAD) |
| TSB-UAD | An older univariate anomaly-detection benchmark (PVLDB 2022, 13,766 series) from the same research group as TSB-AD. | [TSB-UAD](https://github.com/TheDatumOrg/TSB-UAD) |
| TTM (Tiny Time Mixers) | IBM's small time-series foundation models. | — |
| UCR anomaly archive | A cleaned anomaly benchmark from the University of California, Riverside (Wu & Keogh). It avoids the flaws of older datasets. | [paper](https://arxiv.org/abs/2009.13807) |
| univariate | One series at a time. | [research/05](research/05-methods-benchmarks.md#how-to-read-the-scores) |
| VAE (variational autoencoder) | A neural network that learns a compressed model of normal data. Inputs that it cannot rebuild well are unusual. | [Wikipedia](https://en.wikipedia.org/wiki/Variational_autoencoder) |
| VUS-PR (volume under the surface of the precision-recall curve) | Rates an anomaly score without choosing a threshold, and handles anomalies that span a range of points. 0 to 1, higher is better. | [paper](https://www.vldb.org/pvldb/vol15/p2774-paparrizos.pdf) |
| zero-shot (ZS) | A pretrained model used as downloaded, with no extra training on our data. | [research/05](research/05-methods-benchmarks.md#how-to-read-the-scores) |

## Metrics (Prometheus, Grafana)

| Term | Meaning | Learn more |
|---|---|---|
| active series, head series | The series that Prometheus holds in memory because they got samples recently. Our main Prometheus has 1–10 M. | [Prometheus storage](https://prometheus.io/docs/prometheus/latest/storage/) |
| cardinality, label cardinality | The number of distinct label combinations, that is, the number of series. High cardinality costs memory and CPU. | [Prometheus naming](https://prometheus.io/docs/practices/naming/#labels) |
| delta, cumulative | Two ways to report a sum. A delta reports the change since the last report. A cumulative sum reports the running total. | [OTel data model](https://opentelemetry.io/docs/specs/otel/metrics/data-model/#sums) |
| federation | One Prometheus scrapes chosen series from another Prometheus through its `/federate` endpoint. | [Prometheus docs](https://prometheus.io/docs/prometheus/latest/federation/) |
| golden signals | Four signals to watch for each service: latency, traffic, errors and saturation (Google SRE book). | [Google SRE book](https://sre.google/sre-book/monitoring-distributed-systems/) |
| Grafana Cloud | Grafana Labs' paid, hosted service. Grafana ML and Sift exist only there. | [Grafana pricing](https://grafana.com/pricing/) |
| Grafana ML, Sift | Grafana Cloud's machine-learning features: forecasts, outlier detection, and Sift for automatic incident checks. Grafana OSS does not have them. | [Grafana ML docs](https://grafana.com/docs/grafana-cloud/ai-tools/machine-learning/) |
| Grafana OSS | The free, open-source (AGPLv3) edition of Grafana that we run. | [Grafana OSS](https://grafana.com/oss/grafana/) |
| `grafana/promql-anomaly-detection` | An open-source set of Prometheus recording rules from Grafana Labs. It computes adaptive and robust bands. | [GitHub](https://github.com/grafana/promql-anomaly-detection) |
| histogram, bucket, `le` | A Prometheus metric that counts observations per bucket. `le` ("less or equal") is the upper limit of a bucket. Used for latency percentiles. | [Prometheus docs](https://prometheus.io/docs/practices/histograms/) |
| keep-filter | A `remote_write` relabel rule that forwards only the matching series, for example only `anomaly:svc:*`. | [Prometheus config](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#remote_write) |
| label | A key=value pair on a series, for example `service="checkout"`. Each label combination is a separate series. | [Prometheus data model](https://prometheus.io/docs/concepts/data_model/) |
| MetricsQL | VictoriaMetrics' query language, a superset of PromQL. | [VictoriaMetrics docs](https://docs.victoriametrics.com/victoriametrics/metricsql/) |
| Mimir | Grafana's long-term, scalable metric store for Prometheus data. For us it would be a swap, not an add-on. | [Mimir docs](https://grafana.com/docs/mimir/latest/) |
| native histograms | A newer Prometheus histogram type, stored as one series instead of one series per bucket. | [Prometheus spec](https://prometheus.io/docs/specs/native_histograms/) |
| `predict_linear` | A PromQL function that extends the recent trend of a series into the future. We use it to warn early about disks and quotas. | [Prometheus functions](https://prometheus.io/docs/prometheus/latest/querying/functions/) |
| PromQL (Prometheus Query Language) | The query language of Prometheus. | [Prometheus docs](https://prometheus.io/docs/prometheus/latest/querying/basics/) |
| promtool | Prometheus' command-line tool. We use it to check rule files and to run rule unit tests (`promtool test rules`). | [Prometheus docs](https://prometheus.io/docs/prometheus/latest/configuration/unit_testing_rules/) |
| `query_range` | The Prometheus HTTP API call that returns the values of a query over a time range. | [Prometheus HTTP API](https://prometheus.io/docs/prometheus/latest/querying/api/) |
| recording rule | A PromQL query that Prometheus runs on a schedule and stores as a new series. | [Prometheus docs](https://prometheus.io/docs/prometheus/latest/configuration/recording_rules/) |
| RED metrics (rate, errors, duration) | The three request metrics per service: requests per second, failed requests, and how long requests take. | [Grafana blog](https://grafana.com/blog/2018/08/02/the-red-method-how-to-instrument-your-services/) |
| remote_write | A Prometheus feature that sends copies of chosen samples to another server as they arrive. | [Prometheus spec](https://prometheus.io/docs/specs/prw/remote_write_spec/) |
| retention | How long Prometheus keeps data. Weekly bands need 7+ days. SLO windows need 30 days. | [Prometheus storage](https://prometheus.io/docs/prometheus/latest/storage/) |
| sample | One value with a timestamp, inside a series. | [Prometheus data model](https://prometheus.io/docs/concepts/data_model/) |
| scrape, scrape interval | Prometheus pulls the current metric values from each target's HTTP endpoint. The scrape interval says how often, for example every 15 s or 60 s. | [Prometheus docs](https://prometheus.io/docs/concepts/jobs_instances/) |
| series (time series) | A stream of timestamped values with one metric name and one set of labels. | [Prometheus data model](https://prometheus.io/docs/concepts/data_model/) |
| service-level series | A series summed per service, not per pod or host. Detection runs only on these (3–5 k), never on the raw 1–10 M series. | [reference architecture](reference-architecture.md#three-rules-the-design-follows) |
| Thanos sidecar | A process next to Prometheus that uploads its data blocks to object storage for long-term history. For us it is an add-on. | [Thanos docs](https://raw.githubusercontent.com/thanos-io/thanos/main/docs/components/sidecar.md) |
| TSDB (time series database) | A database for timestamped values. Prometheus has one built in. | [Prometheus storage](https://prometheus.io/docs/prometheus/latest/storage/) |
| USE method (utilization, saturation, errors) | Three metrics to check for each resource, such as CPU, memory or disk. | [Brendan Gregg](https://www.brendangregg.com/usemethod.html) |
| VictoriaMetrics | A metric store and query engine that is compatible with Prometheus. For us it would be a swap, not an add-on. | [VictoriaMetrics docs](https://docs.victoriametrics.com/) |
| vmanomaly | VictoriaMetrics' anomaly-detection service. It reads series, runs models and writes anomaly scores back. | [VictoriaMetrics docs](https://docs.victoriametrics.com/anomaly-detection/) |

## Logs (Elastic, Filebeat)

| Term | Meaning | Learn more |
|---|---|---|
| Brain (log parser) | A log template algorithm. OpenSearch's `patterns` command can use it. | [OpenSearch docs](https://docs.opensearch.org/latest/sql-and-ppl/ppl/commands/patterns/) |
| `CATEGORIZE` | An ES\|QL command that groups similar log messages into categories. It needs a Platinum licence. | [Elastic docs](https://www.elastic.co/docs/reference/query-languages/esql/functions-operators/grouping-functions/categorize) |
| `CHANGE_POINT` | An ES\|QL command that finds spikes, dips and trend changes in a series. It needs a Platinum licence. | [Elastic docs](https://www.elastic.co/docs/reference/query-languages/esql/commands/change-point) |
| checkpoint (transform) | One run of a continuous transform over the documents that arrived since the last run. | [Elastic docs](https://www.elastic.co/docs/explore-analyze/transforms/transform-checkpoints) |
| ClickHouse | A column-oriented SQL database for analytics. Coroot, for example, runs its own ClickHouse database. | [ClickHouse docs](https://clickhouse.com/docs/intro) |
| cluster (Drain) | A group of similar log lines in Drain. One cluster is one template. | [Drain3](https://github.com/logpai/Drain3) |
| connector (Kibana) | How a Kibana alert rule sends its output, for example to an index, the server log, Slack or a webhook. On Basic only Index and Server log work. | [Elastic docs](https://www.elastic.co/docs/reference/kibana/connectors-kibana) |
| `container` parser, `ndjson` parser | Filebeat parsers. `container` removes the CRI or Docker envelope. `ndjson` then decodes JSON log lines (NDJSON). | [Elastic docs](https://www.elastic.co/docs/reference/beats/filebeat/filebeat-input-filestream) |
| `copy_fields` | A Filebeat processor that copies a field. We copy the message before we mask the copy. | [Elastic docs](https://www.elastic.co/docs/reference/beats/filebeat/copy-fields) |
| Drain | An online log parsing algorithm. It groups log lines into templates with a fixed-depth tree (He et al., 2017). | [paper](https://jiemingzhu.github.io/pub/pjhe_icws2017.pdf) |
| `drain` processor (OTel) | An alpha OTel Collector processor that mines Drain templates from log records. Path B uses it. | [README](https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/main/processor/drainprocessor/README.md) |
| Drain3 | The Python streaming version of Drain, under the MIT licence. It has had no release since 2022. | [GitHub](https://github.com/logpai/Drain3) |
| DSL (data stream lifecycle) | Elastic's built-in retention and downsampling for data streams. Not the same as Query DSL. | [Elastic docs](https://www.elastic.co/docs/manage-data/data-store/data-streams/downsampling-concepts) |
| DSL (Query DSL) | Elasticsearch's JSON query format. DSL here means domain-specific language. | [Elastic docs](https://www.elastic.co/docs/explore-analyze/query-filter/languages/querydsl) |
| ECS (Elastic Common Schema) | Elastic's standard field names, for example `service.name` and `error.type`. | [Elastic docs](https://www.elastic.co/docs/reference/ecs) |
| ElastAlert2 | An open-source (Apache-2.0) alert rule engine. It queries Elasticsearch on a schedule and sends alerts, in our design to Alertmanager. | [README](https://raw.githubusercontent.com/jertel/elastalert2/master/README.md) |
| ElastAlert2 rule types | `new_term`: a new value appears. `spike_aggregation`: a sudden rise. `flatline`: too few events. `percentage_match`: a ratio is too high. | [rule types](https://raw.githubusercontent.com/jertel/elastalert2/master/docs/source/ruletypes.rst) |
| ES\|QL (Elasticsearch Query Language) | Elastic's piped query language. It is free on Basic, but some commands need Platinum. | [Elastic docs](https://www.elastic.co/docs/reference/query-languages/esql) |
| Filebeat | Elastic's log shipper. It reads log files and sends the lines to Elasticsearch. It allows exactly one output. | [Elastic docs](https://www.elastic.co/docs/reference/beats/filebeat/configuring-output) |
| Filebeat processor | A step in Filebeat that changes each event before it is sent, for example `replace` or `fingerprint`. | [Elastic docs](https://www.elastic.co/docs/reference/beats/filebeat/filtering-enhancing-data) |
| `filestream` input | The Filebeat input that reads log files line by line. | [Elastic docs](https://www.elastic.co/docs/reference/beats/filebeat/filebeat-input-filestream) |
| `fingerprint` | A Filebeat processor (and an ingest processor) that hashes chosen fields. We hash the masked message into the template key. | [Elastic docs](https://www.elastic.co/docs/reference/beats/filebeat/fingerprint) |
| grok | A pattern parser that pulls fields out of text lines. Logstash and ingest pipelines have it. | [Elastic docs](https://www.elastic.co/docs/reference/ingest-processor/grok-processor) |
| ILM (index lifecycle management) | An Elastic feature that rolls over, shrinks and deletes indices by age. | [Elastic docs](https://www.elastic.co/docs/manage-data/lifecycle/index-lifecycle-management) |
| ingest pipeline | A chain of Elasticsearch processors that change documents before they are stored. Our alternative to Filebeat processors for the template key. | [Elastic docs](https://www.elastic.co/docs/manage-data/ingest/transform-enrich/ingest-pipelines) |
| ISM (Index State Management) | OpenSearch's version of ILM. | [OpenSearch docs](https://docs.opensearch.org/latest/im-plugin/ism/index/) |
| Kafka | A distributed message log. An iteration-3 option: Filebeat writes to Kafka, and both Elasticsearch and the template miners read from it. | [Apache Kafka](https://kafka.apache.org/) |
| Kibana | Elastic's web UI for search, dashboards and alert rules. On Basic, its alerts can only write to an index or to the server log. | [Kibana](https://www.elastic.co/kibana) |
| KQL (Kibana Query Language) | Kibana's simple text query syntax. | [Elastic docs](https://www.elastic.co/docs/explore-analyze/alerting/alerts/rule-type-es-query) |
| log parsing, template mining | Finding the templates in raw log lines automatically. | [paper (Zhu et al.)](https://arxiv.org/abs/1811.03509) |
| Logstash, PQ (persistent queue) | Logstash is Elastic's log processing server. Its PQ is a queue on disk that survives restarts. | [Elastic docs](https://www.elastic.co/docs/reference/logstash/persistent-queues) |
| masking | Replacing the variable parts of a log line (numbers, IDs, timestamps) with placeholders such as `<NUM>` or `<TS>`. | [Drain3](https://github.com/logpai/Drain3) |
| NDJSON (newline-delimited JSON) | A format with one JSON object per line. Filebeat's `ndjson` parser reads it. | [Elastic docs](https://www.elastic.co/docs/reference/beats/filebeat/filebeat-input-filestream) |
| nested field | An Elasticsearch field type in which each element is a hidden sub-document. It needs `nested` queries, and Kibana Lens cannot use it. | [Elastic docs](https://www.elastic.co/docs/reference/elasticsearch/mapping-reference/nested) |
| OOV (out-of-vocabulary) | Words that never appeared in the training data. An OOV detector flags log lines with such words. | — |
| OpenSearch | An open-source fork of Elasticsearch. Its free plugins include anomaly detection with RCF and log pattern grouping. | [OpenSearch docs](https://docs.opensearch.org/latest/observing-your-data/ad/index/) |
| PIT (point in time) | An Elasticsearch view of an index frozen at one moment, for consistent paging through results. | [Elastic docs](https://www.elastic.co/docs/reference/elasticsearch/rest-apis/paginate-search-results) |
| PPL (Piped Processing Language) | OpenSearch's piped query language. Its `patterns` command groups log lines. | [OpenSearch docs](https://docs.opensearch.org/latest/sql-and-ppl/ppl/commands/patterns/) |
| `replace` | A Filebeat processor that replaces text in a field with a regex. We use it to mask variable parts. It exists since Filebeat 7.8. | [Elastic docs](https://www.elastic.co/docs/reference/beats/filebeat/replace-fields) |
| similarity threshold (`sim_th`) | In Drain: how alike a line must be to a template to join it. anomalyd's flag for it is `--drain-sim-th`. | [Drain3](https://github.com/logpai/Drain3) |
| template (log template) | The fixed text of a log line, with the variable parts (numbers, IDs) masked. | [paper (Zhu et al.)](https://arxiv.org/abs/1811.03509) |
| terms aggregation | An Elasticsearch aggregation that groups documents by a field value, for example per service. | [Elastic docs](https://www.elastic.co/docs/reference/aggregations/search-aggregations-bucket-terms-aggregation) |
| transform (Elasticsearch) | A continuous job that summarises one index into a smaller index. We use it for per-minute template counts. It is free on Basic. | [Elastic docs](https://www.elastic.co/docs/explore-analyze/transforms) |
| TSDS (time series data stream) | An Elasticsearch data stream type for metrics. It allows downsampling. | [Elastic docs](https://www.elastic.co/docs/manage-data/data-store/data-streams/time-series-data-stream-tsds) |
| Watcher | Elastic's older alerting engine. It needs Gold or higher. | [Elastic subscriptions](https://www.elastic.co/subscriptions) |
| x-pack | The Elasticsearch code that is only under ELv2. It holds the Basic and the paid features. The OSS build does not have it. | [LICENSE.txt](https://raw.githubusercontent.com/elastic/elasticsearch/main/LICENSE.txt) |

## Traces (Jaeger, OpenTelemetry)

| Term | Meaning | Learn more |
|---|---|---|
| adjusted count | How many spans one sampled span stands for: 1 ÷ sampling probability. At 2 % sampling, one kept span stands for 50. | [OTel spec](https://opentelemetry.io/docs/specs/otel/trace/tracestate-probability-sampling/) |
| APM (application performance monitoring) | Tools that trace and time the requests inside apps. | [Wikipedia](https://en.wikipedia.org/wiki/Application_performance_management) |
| collector tier (OTel tier 1, tier 2) | A group of identical OTel Collector pods doing one job. Tier 1 routes spans by trace ID. Tier 2 computes span metrics, then samples. | [OTel scaling](https://opentelemetry.io/docs/collector/scaling/) |
| connector (OTel) | A Collector component that joins two pipelines. It is the exporter of one and the receiver of the next, for example `span_metrics`. | [OTel docs](https://opentelemetry.io/docs/collector/configuration/#connectors) |
| context propagation | Passing the trace ID from one service to the next, usually in HTTP headers. | [OTel docs](https://opentelemetry.io/docs/concepts/context-propagation/) |
| `count` connector | An OTel Collector connector that counts records, here log lines, and emits the counts as metrics. | [README](https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/v0.160.0/connector/countconnector/README.md) |
| critical path | The chain of dependent calls that sets the total time of a request. | [Uber blog (CRISP)](https://www.uber.com/blog/crisp-critical-path-analysis-for-microservice-architectures/) |
| dimension (span metrics) | A span attribute that becomes a metric label in span metrics. | [README](https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/v0.160.0/connector/spanmetricsconnector/README.md) |
| eBPF | A Linux kernel feature that runs small sandboxed programs. Tools use it to trace calls without changing the apps. | [ebpf.io](https://ebpf.io/what-is-ebpf/) |
| exemplar | A trace ID stored next to a metric value. You can jump from a graph to one example trace. | [Grafana docs](https://grafana.com/docs/grafana/latest/fundamentals/exemplars/) |
| feature gate | A named switch for new Collector behaviour. Alpha gates are off by default, beta gates are on. | [README](https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector/main/featuregate/README.md) |
| `file_log` receiver | An OTel Collector receiver that reads log files. | [README](https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/main/receiver/filelogreceiver/README.md) |
| head sampling | Deciding to keep or drop a trace at its start, before its result is known. | [OTel sampling](https://opentelemetry.io/docs/concepts/sampling/) |
| Jaeger v1, v2 | Jaeger is our tracing system. v1 has been end-of-life since 2025-12-31. v2 is built on the OTel Collector and bundles `tail_sampling` and `spanmetrics`. Moving to v2 is an upgrade, not a swap, and can happen any time. | [Jaeger migration](https://www.jaegertracing.io/docs/latest/migration/) |
| `load_balancing` exporter | Sends all spans of one trace to the same next-tier collector, chosen by the trace ID. | [OTel scaling](https://opentelemetry.io/docs/collector/scaling/) |
| OTel (OpenTelemetry) | An open standard, with SDKs and a Collector, for traces, metrics and logs. | [What is OpenTelemetry?](https://opentelemetry.io/docs/what-is-opentelemetry/) |
| OTel Collector | A service that receives, processes and exports telemetry in pipelines of receivers, processors and exporters. | [OTel docs](https://opentelemetry.io/docs/collector/) |
| otelcol-contrib | The OTel Collector build with the extra ("contrib") components, such as `tail_sampling` and `drain`. We validated version 0.160.0. | [releases](https://github.com/open-telemetry/opentelemetry-collector-contrib/releases/latest) |
| OTLP (OpenTelemetry Protocol) | The protocol that OTel components use to send spans, metrics and logs. | [OTLP spec](https://opentelemetry.io/docs/specs/otlp/) |
| OTTL (OpenTelemetry Transformation Language) | A small language for conditions and edits on telemetry inside the Collector. | [README](https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/v0.160.0/pkg/ottl/README.md) |
| policy (tail sampling) | One rule in tail sampling that votes to keep or drop a trace, for example "keep all errors". | [README](https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/v0.160.0/processor/tailsamplingprocessor/README.md) |
| resource (OTel) | The description of the data source, for example the service, pod or host that sent the data. | [OTel docs](https://opentelemetry.io/docs/concepts/resources/) |
| SDK (software development kit) | A library added to the app code, for example to create spans. Jaeger SDKs are the older Jaeger client libraries. | [Wikipedia](https://en.wikipedia.org/wiki/Software_development_kit) |
| semantic conventions (semconv) | Standard OpenTelemetry names for spans and attributes, for example `service.name`. | [OTel docs](https://opentelemetry.io/docs/concepts/semantic-conventions/) |
| service graph | Metrics about calls between services: one set per caller → callee pair. The OTel `service_graph` connector builds them. | [README](https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/v0.160.0/connector/servicegraphconnector/README.md) |
| span | One timed operation, for example one HTTP request in one service. It has a name, a start time, a duration and attributes. | [OTel traces](https://opentelemetry.io/docs/concepts/signals/traces/) |
| span kind | The role of a span: server, client, producer, consumer or internal. | [OTel traces](https://opentelemetry.io/docs/concepts/signals/traces/) |
| span metrics | RED metrics computed from spans by the OTel `span_metrics` connector. | [README](https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/v0.160.0/connector/spanmetricsconnector/README.md) |
| SPM (Service Performance Monitoring) | The Monitor tab in Jaeger. It shows RED metrics per service and operation, read from Prometheus. | [Jaeger docs](https://www.jaegertracing.io/docs/latest/architecture/spm/) |
| stability level | The OTel maturity label of a component: development, alpha, beta or stable. | [OTel docs](https://github.com/open-telemetry/opentelemetry-collector/blob/main/docs/component-stability.md) |
| tail sampling | Deciding to keep or drop a whole trace after it ends. We keep errors, slow traces and 2 % of the rest. | [OTel sampling](https://opentelemetry.io/docs/concepts/sampling/) |
| telemetrygen | An OTel tool that generates test spans, metrics or logs. We used it in our runtime tests. | — |
| trace | All the spans of one request across services. They share one trace ID. | [OTel traces](https://opentelemetry.io/docs/concepts/signals/traces/) |
| W3C (World Wide Web Consortium) Trace Context | A web standard for the HTTP headers `traceparent` and `tracestate`, which carry trace IDs between services. | [W3C](https://www.w3.org/TR/trace-context/) |

## Alerting and operations

| Term | Meaning | Learn more |
|---|---|---|
| alert group, grouping | Alerts that Alertmanager joins into one notification, by labels such as `service` and `anomaly_name`. | [Alertmanager docs](https://prometheus.io/docs/alerting/latest/alertmanager/) |
| Alertmanager | The Prometheus component that groups, deduplicates, silences, inhibits and routes alerts. Our single alert bus. | [Alertmanager docs](https://prometheus.io/docs/alerting/latest/alertmanager/) |
| amtool | Alertmanager's command-line tool. We use `amtool check-config` and `amtool config routes test`. | [GitHub](https://github.com/prometheus/alertmanager#amtool) |
| ARQ | A job queue on Redis. Keep uses it at scale. | [arq docs](https://arq-docs.helpmanual.io/) |
| burn rate | How fast a service uses up its error budget. At a burn rate of 1, the budget lasts exactly the SLO window. | [Google SRE workbook](https://sre.google/workbook/alerting-on-slos/) |
| canary host | One host that gets a change first, before the whole fleet. | [Martin Fowler](https://martinfowler.com/bliki/CanaryRelease.html) |
| CEL (Common Expression Language) | A small expression language for conditions. Keep uses it for its maintenance windows. | [cel.dev](https://cel.dev/) |
| CI (continuous integration) | Automatic builds and tests on every change. | [Wikipedia](https://en.wikipedia.org/wiki/Continuous_integration) |
| dead-man's switch | An alert that must always be firing, like a heartbeat. If it stops arriving, alerting is broken. We use one to catch a dead ElastAlert2. | [Wikipedia](https://en.wikipedia.org/wiki/Dead_man%27s_switch) |
| error budget | The amount of failure that an SLO allows. For a 99.9 % SLO it is 0.1 % of the requests. | [Google SRE workbook](https://sre.google/workbook/alerting-on-slos/) |
| gossip protocol | A peer-to-peer way for copies of a service to share state. Alertmanager replicas in a cluster use it. | [Wikipedia](https://en.wikipedia.org/wiki/Gossip_protocol) |
| Grafana OnCall, Grafana IRM | Grafana's on-call tools. OnCall OSS was archived on 2026-03-24. Its replacement, IRM (incident response and management), runs only in Grafana Cloud. | [Grafana blog](https://grafana.com/blog/grafana-oncall-maintenance-mode/) |
| groupKey | The key that Alertmanager gives each alert group. | [Alertmanager config](https://prometheus.io/docs/alerting/latest/configuration/) |
| HA (high availability) | Running more than one instance, so that one failure does not stop the service. | [Wikipedia](https://en.wikipedia.org/wiki/High_availability) |
| heartbeat alert, flatline alert | An alert that fires when a source stops sending data. | [ElastAlert2 rule types](https://raw.githubusercontent.com/jertel/elastalert2/master/docs/source/ruletypes.rst) |
| inhibition | Alertmanager mutes some alerts while another alert fires. We mute findings while a page for the same service fires. | [Alertmanager docs](https://prometheus.io/docs/alerting/latest/alertmanager/) |
| Keep (Keep OSS) | An open-source alert-correlation tool. It groups alerts from several sources into incidents. Optional in iteration 2. | [GitHub](https://github.com/keephq/keep) |
| KPI (key performance indicator) | A business metric, such as orders per minute. | [Wikipedia](https://en.wikipedia.org/wiki/Performance_indicator) |
| meta alert | An alert about the health of the detectors themselves, with the label `tier="meta"`. It goes to a platform chat channel. | [poc/README.md](../poc/README.md#what-was-verified-2026-09-14) |
| MTTR (mean time to recovery) | The average time from the start of an incident until the service works again. | [Wikipedia](https://en.wikipedia.org/wiki/Mean_time_to_recovery) |
| MWMB (multi-window, multi-burn-rate) | SLO alerts that check a long and a short window at several burn rates. Ours: 14.4× over 1 h/5 m and 6× over 6 h/30 m. | [Google SRE workbook](https://sre.google/workbook/alerting-on-slos/) |
| on-call | The person on duty who receives pages. | [Google SRE book](https://sre.google/sre-book/being-on-call/) |
| P1, P2 | Priority-1 and priority-2 incidents: the most urgent ones. | — |
| page, paging | An alert that wakes the person on call. In our design only T0 alerts, with `severity: page`, may page. | [Google SRE workbook](https://sre.google/workbook/alerting-on-slos/) |
| Pyrra | An open-source tool that generates SLO recording rules and burn-rate alerts for Prometheus, with a web UI. | [GitHub](https://github.com/pyrra-dev/pyrra) |
| QoA (Quality of Alert) | Huawei's measure of alert quality: indicativeness, precision and handleability. | [paper](https://arxiv.org/abs/2204.09670) |
| route (Alertmanager) | A rule that decides where an alert goes, based on its labels. | [Alertmanager config](https://prometheus.io/docs/alerting/latest/configuration/#route) |
| `severity` label | Our alert label. `page` goes to on-call. `warning` and `info` go to chat. | [alertmanager.yml](../poc/prometheus/alertmanager.yml) |
| silence | A manual mute in Alertmanager for a set time, for example during maintenance. | [Alertmanager docs](https://prometheus.io/docs/alerting/latest/alertmanager/) |
| SLI (service level indicator) | The measured value behind an SLO, for example the share of failed requests. | [Google SRE workbook](https://sre.google/workbook/alerting-on-slos/) |
| SLO (service level objective) | A reliability target for a service, for example "99.9 % of requests succeed over 30 days". | [Google SRE workbook](https://sre.google/workbook/alerting-on-slos/) |
| Sloth | An open-source tool that generates SLO recording rules and multi-window burn-rate alerts for Prometheus. | [GitHub](https://github.com/slok/sloth) |
| SRE (site reliability engineering) | Google's practice for running reliable services. The burn-rate alert method comes from its workbook. | [Google SRE workbook](https://sre.google/workbook/alerting-on-slos/) |
| `tier` label | Our alert label for findings: `"1"` or `"2"`. Alertmanager routes these to chat or tickets. | [alertmanager.yml](../poc/prometheus/alertmanager.yml) |
| webhook | An HTTP call that one tool sends to another when something happens, for example an alert. Kibana on Basic cannot send one. | [Elastic docs](https://www.elastic.co/docs/reference/kibana/connectors-kibana/webhook-action-type) |

## Kubernetes and containers

| Term | Meaning | Learn more |
|---|---|---|
| container runtime | The software that runs containers on a node, for example containerd or CRI-O. It writes each container's stdout and stderr to a file. | [Kubernetes docs](https://kubernetes.io/docs/setup/production-environment/container-runtimes/) |
| CRD (custom resource definition) | A Kubernetes extension type. Sloth and Pyrra use one for SLO objects. | [Kubernetes docs](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/) |
| CRI (Container Runtime Interface) | The Kubernetes interface between kubelet and the container runtime. Its log format adds `<time> <stream> <P\|F>` before each line. | [Kubernetes docs](https://kubernetes.io/docs/concepts/containers/cri/) |
| DaemonSet | A Kubernetes object that runs one copy of a pod on every node. Filebeat and the anomalyd agent run this way. | [Kubernetes docs](https://kubernetes.io/docs/concepts/workloads/controllers/daemonset/) |
| Deployment | A Kubernetes object that runs a set of identical, stateless pods. | [Kubernetes docs](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/) |
| distroless image | A container image with only the app and what it needs to run: no shell and no package manager. Our anomalyd image is one. | [GitHub](https://github.com/GoogleContainerTools/distroless) |
| envelope (log envelope) | The prefix that the container runtime adds to each log line: time, stream, and a flag for partial or full lines. | [Kubernetes logging](https://kubernetes.io/docs/concepts/cluster-administration/logging/) |
| headless service | A Kubernetes Service without one virtual IP. Its DNS name returns the address of each pod. | [Kubernetes docs](https://kubernetes.io/docs/concepts/services-networking/service/#headless-services) |
| `hostPath` | A pod volume that mounts a directory of the node, for example `/var/log`. | [Kubernetes docs](https://kubernetes.io/docs/concepts/storage/volumes/#hostpath) |
| HPA (Horizontal Pod Autoscaler) | A Kubernetes object that adds or removes pods according to load. | [Kubernetes docs](https://kubernetes.io/docs/concepts/workloads/autoscaling/horizontal-pod-autoscale/) |
| kubeconform | A tool that checks Kubernetes manifests against the API schemas. We check `agent-daemonset.yaml` with it. | [GitHub](https://github.com/yannh/kubeconform) |
| kubelet | The Kubernetes agent on each node. It has the runtime write container logs to files and rotates them (10 MiB, 5 files by default). | [Kubernetes logging](https://kubernetes.io/docs/concepts/cluster-administration/logging/#log-rotation) |
| node | One machine, virtual or physical, in the cluster. | [Kubernetes docs](https://kubernetes.io/docs/concepts/architecture/nodes/) |
| pod | The smallest unit that Kubernetes runs: one or more containers that share network and storage. | [Kubernetes docs](https://kubernetes.io/docs/concepts/workloads/pods/) |
| RBAC (role-based access control) | Permissions given to roles, not to single users. | [Kubernetes docs](https://kubernetes.io/docs/reference/access-authn-authz/rbac/) |
| sidecar | A helper process that runs next to the main one, for example the Thanos sidecar next to Prometheus. | [Thanos docs](https://raw.githubusercontent.com/thanos-io/thanos/main/docs/components/sidecar.md) |
| StatefulSet | A Kubernetes object for pods that need stable names and their own storage. | [Kubernetes docs](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/) |
| stdout, stderr | The standard output and standard error streams of a program. Our apps log to stdout. | [Wikipedia](https://en.wikipedia.org/wiki/Standard_streams) |
| `/var/log/containers`, `/var/log/pods` | Node directories with container log files. Files in `/var/log/containers` are links to files in `/var/log/pods`. | [Kubernetes logging](https://kubernetes.io/docs/concepts/cluster-administration/logging/) |

## LLMs and cost

| Term | Meaning | Learn more |
|---|---|---|
| agent (LLM agent) | An LLM that calls tools in a loop to investigate a problem, for example HolmesGPT. It makes many LLM calls per run. | [Wikipedia](https://en.wikipedia.org/wiki/AI_agent) |
| AI (artificial intelligence) | Software that learns or reasons. In vendor tools it mostly means LLM features. | [Wikipedia](https://en.wikipedia.org/wiki/Artificial_intelligence) |
| AIOps (AI for IT operations) | Tools that apply machine learning to alerts, logs and metrics. Elastic's AIOps tools (log rate analysis, log pattern analysis, change point detection) need Platinum. | [Wikipedia](https://en.wikipedia.org/wiki/AIOps) |
| batch API | An LLM API mode that returns results within 24 hours instead of at once, at half the price. Good for digests, too slow for paging. | [OpenAI docs](https://developers.openai.com/api/docs/guides/batch) |
| commercial baseline | Managed vendor products that we priced only for comparison: Grafana Cloud, Datadog and Elastic Serverless. | [cost sizing](cost-sizing.md) |
| custom metric (Datadog) | Datadog's billing unit for metrics: one metric name with one combination of tag values. It is billed per 100. | [Datadog docs](https://docs.datadoghq.com/account_management/billing/custom_metrics/) |
| DPM (data points per minute) | Grafana Cloud's metrics billing unit: the samples it receives per minute. | [Grafana docs](https://grafana.com/docs/grafana-cloud/cost-management-and-billing/manage-invoices/understand-your-invoice/metrics-invoice/) |
| EC2 (Elastic Compute Cloud) | Amazon Web Services' virtual machine service. We price compute with EC2 on-demand rates. | [AWS](https://aws.amazon.com/ec2/) |
| Elastic Serverless | Elastic's fully managed cloud offering, billed by the data volume. Using it means moving off self-managed Elasticsearch. | [Elastic pricing](https://www.elastic.co/pricing/serverless-observability) |
| ERU | Elastic's self-managed licence unit, based on RAM. The full name is not confirmed on an Elastic page (UNVERIFIED). | — |
| Flex Logs, Flex storage | Datadog's cheaper log storage tier. Its compute price is UNVERIFIED. | [Datadog pricing](https://www.datadoghq.com/pricing/list/) |
| GPU (graphics processing unit) | A processor for parallel maths. A self-hosted LLM needs one to run fast. | [Wikipedia](https://en.wikipedia.org/wiki/Graphics_processing_unit) |
| graduated pricing, graduated tiers | Each volume band is billed at its own rate. The lowest rate applies only to the volume above its threshold. | [Elastic pricing](https://www.elastic.co/pricing/serverless-observability) |
| HF (Hugging Face) | A website that hosts open model weights and datasets. We read model licences and sizes from its API. | [Hugging Face docs](https://huggingface.co/docs/hub/index) |
| HolmesGPT | An open-source LLM agent that investigates an alert on demand. It queries tools such as Prometheus and Elasticsearch. | [README](https://raw.githubusercontent.com/HolmesGPT/holmesgpt/master/README.md) |
| KV cache, GQA | The KV (key-value) cache is memory that holds the processed prompt. GQA (grouped-query attention) makes it smaller by sharing key-value heads. | [GQA paper](https://arxiv.org/abs/2305.13245) |
| list price | The public price before any discount. | — |
| LiteLLM | A library and proxy that calls more than 100 LLM APIs in one common (OpenAI) format. | [LiteLLM docs](https://docs.litellm.ai/) |
| llama.cpp | A C/C++ program that runs LLMs on a CPU or GPU, with quantized weights. | [GitHub](https://github.com/ggml-org/llama.cpp) |
| LLM (large language model) | An AI model that reads and writes text, for example GPT, Gemini or Claude. | [Wikipedia](https://en.wikipedia.org/wiki/Large_language_model) |
| LLM triage | Our T3 step: one LLM call per Alertmanager alert group writes a summary and hypotheses into the chat thread. | [research/04](research/04-aiops-rca-llm-cost.md) |
| MCP (Model Context Protocol) | An open standard that lets LLM apps call external tools and data sources. | [modelcontextprotocol.io](https://modelcontextprotocol.io/) |
| ML (machine learning) | Models learned from data. Elastic ML and Grafana ML are paid features. | [Wikipedia](https://en.wikipedia.org/wiki/Machine_learning) |
| Ollama | A tool that runs open models locally. | [ollama.com](https://ollama.com/) |
| on-demand price | Pay-per-hour cloud pricing without a long-term commitment. | [AWS](https://aws.amazon.com/ec2/pricing/on-demand/) |
| on-prem (on-premises) | Hardware in our own data centre, not rented from a cloud. | [Wikipedia](https://en.wikipedia.org/wiki/On-premises_software) |
| parameters (8B, 7–14B) | The learned numbers inside a model. 8B means 8 billion parameters. More parameters need more memory. | [Wikipedia](https://en.wikipedia.org/wiki/Large_language_model) |
| PII (personally identifiable information) | Personal data. We redact it before any LLM call. | [Wikipedia](https://en.wikipedia.org/wiki/Personal_data) |
| prefill, decode | Prefill: the model reads the whole prompt. Decode: it writes the answer, one token at a time. | — |
| prompt caching, cache read, TTL | Reusing an unchanged prompt prefix across calls. Cached input gets a lower "cache read" price. The cache lives 5 min or 1 h (TTL, time to live). | [Anthropic docs](https://platform.claude.com/docs/en/build-with-claude/prompt-caching) |
| quantization, FP32, FP16, FP8, Q4 | Storing model weights with fewer bits, so that the model needs less memory. FP32, FP16 and FP8 are 32-, 16- and 8-bit formats. Q4 means 4-bit weights. | [llama.cpp](https://github.com/ggml-org/llama.cpp) |
| quote-only | A price that the vendor gives only on request, so we could not check it. | — |
| RCA (root cause analysis) | Finding what caused an incident. | [Wikipedia](https://en.wikipedia.org/wiki/Root_cause_analysis) |
| self-hosted model | An open model that runs on our own GPU or CPU, for example with vLLM, llama.cpp or Ollama, instead of a vendor API. | [vLLM docs](https://docs.vllm.ai/) |
| SKU (stock keeping unit) | One price item in a vendor's price list, for example Elastic's TSDS metrics SKUs. | [Wikipedia](https://en.wikipedia.org/wiki/Stock_keeping_unit) |
| sparsity | A speed-up for model weights with many zeros. NVIDIA's L4 specs assume it. | [NVIDIA L4](https://www.nvidia.com/en-us/data-center/l4/) |
| token | The unit of text that an LLM reads and writes. APIs bill per million tokens. We assume about 4 bytes of log text per token (UNVERIFIED). | [Wikipedia](https://en.wikipedia.org/wiki/Large_language_model#Tokenization) |
| tokenizer | The part of an LLM that splits text into tokens. Newer Claude models produce about 30 % more tokens for the same text. | [Anthropic pricing](https://platform.claude.com/docs/en/about-claude/pricing) |
| toolset | HolmesGPT's name for a group of read-only tools, for example Prometheus queries. | [HolmesGPT docs](https://holmesgpt.dev/latest/data-sources/builtin-toolsets/) |
| triage | The first look at an alert group: a summary, the likely cause and the next checks. | [research/04](research/04-aiops-rca-llm-cost.md) |
| vCPU (virtual CPU) | One CPU thread of a cloud virtual machine. Our cost model prices one vCPU-month at $32.58. | [cost sizing §8](cost-sizing.md#8-prices-formulas-and-sources) |
| vLLM | A library for fast LLM inference and serving. | [vLLM docs](https://docs.vllm.ai/) |
| Watchdog (Datadog) | Datadog's built-in anomaly detection. | [Datadog docs](https://docs.datadoghq.com/watchdog/) |

## Licences

| Term | Meaning | Learn more |
|---|---|---|
| AGPL, AGPLv3 (GNU Affero General Public License v3) | A copyleft open-source licence. If you offer changed code as a network service, you must share its source. Grafana OSS uses it. | [Wikipedia](https://en.wikipedia.org/wiki/GNU_Affero_General_Public_License) |
| Apache-2.0, MIT, BSD-2-Clause, BSD-3-Clause | Permissive open-source licences. You may use, change and ship the code with few conditions. BSD means Berkeley Software Distribution. | [Wikipedia](https://en.wikipedia.org/wiki/Permissive_software_license) |
| Basic (Elastic) | Elastic's free, never-expiring default licence for self-managed clusters. We run it. | [Elastic subscriptions](https://www.elastic.co/subscriptions) |
| CC-BY-NC | Creative Commons Attribution-NonCommercial: a licence that forbids commercial use. Moirai's model weights use it. | [Creative Commons](https://creativecommons.org/licenses/by-nc/4.0/) |
| CE (Community Edition) | The free edition of a product that also has paid editions, for example Coroot CE. | — |
| CNCF (Cloud Native Computing Foundation) | The foundation that hosts open-source projects such as Kubernetes, Prometheus, Jaeger and OpenTelemetry. | [Wikipedia](https://en.wikipedia.org/wiki/Cloud_Native_Computing_Foundation) |
| copyleft | A licence rule: if you share changed code, you must share it under the same licence. | [Wikipedia](https://en.wikipedia.org/wiki/Copyleft) |
| EE (Enterprise Edition) | The paid edition of an open-core product. | — |
| ELv2 (Elastic License 2.0) | Elastic's source-available licence for its default distribution. Free to use inside a company. | [Elastic licensing FAQ](https://www.elastic.co/pricing/faq/licensing) |
| Enterprise (Elastic) | Elastic's highest paid tier. For a new buyer it is in practice the next step after Basic. | [Elastic subscriptions](https://www.elastic.co/subscriptions) |
| EOL (end of life) | The vendor no longer ships updates or security patches. Jaeger v1 has been EOL since 2025-12-31. | [Elastic EOL policy](https://www.elastic.co/support/eol) |
| FSL (Functional Source License) | Sentry's source-available licence. Each version becomes Apache-2.0 after 2 years. | [LICENSE](https://raw.githubusercontent.com/getsentry/sentry/master/LICENSE.md) |
| GA (generally available) | A feature is stable and supported, no longer in preview or beta. | [Wikipedia](https://en.wikipedia.org/wiki/Software_release_life_cycle#General_availability) |
| Gold (Elastic) | A paid tier that adds more Kibana connectors and Watcher. It is closed to new customers. | [Elastic subscriptions](https://www.elastic.co/subscriptions) |
| GPL (GNU General Public License) | A copyleft open-source licence. AGPL adds the same duty for software used over a network. | [Wikipedia](https://en.wikipedia.org/wiki/GNU_General_Public_License) |
| LGPL (GNU Lesser General Public License) | A weaker copyleft licence. Changes to the library must stay open, but programs that only use it need not. | [Wikipedia](https://en.wikipedia.org/wiki/GNU_Lesser_General_Public_License) |
| MPL-2.0 (Mozilla Public License 2.0) | A weak copyleft licence that works per file. Changed files must stay open, other files need not. | [Wikipedia](https://en.wikipedia.org/wiki/Mozilla_Public_License) |
| NCUL1 (Netdata Cloud UI License) | The closed licence of the Netdata dashboard UI. | [LICENSE](https://raw.githubusercontent.com/netdata/netdata/master/LICENSE) |
| open core | A business model: a free open-source core, with paid features on top. | [Wikipedia](https://en.wikipedia.org/wiki/Open-core_model) |
| OSI (Open Source Initiative) | The organisation that decides which licences count as open source. | [Wikipedia](https://en.wikipedia.org/wiki/Open_Source_Initiative) |
| OSS (open-source software) | Software under an open-source licence. "Grafana OSS" is Grafana's free edition. | [Wikipedia](https://en.wikipedia.org/wiki/Open-source_software) |
| OSS build (Elasticsearch OSS 7.10.2) | Elastic's last Apache-2.0 build. It has no x-pack, so no ILM, transforms, Kibana alerting or ES\|QL. It is end-of-life. | [Elastic licensing FAQ](https://www.elastic.co/pricing/faq/licensing) |
| Platinum (Elastic) | A paid tier. It unlocks Elastic ML, AIOps, `CATEGORIZE` and `CHANGE_POINT`. Self-managed Platinum is sold only to existing customers. | [Elastic subscriptions](https://www.elastic.co/subscriptions) |
| SaaS (software as a service) | Software that the vendor runs for you and that you use over the internet. | [Wikipedia](https://en.wikipedia.org/wiki/Software_as_a_service) |
| source-available | The source code is public, but the licence is not open source. ELv2 and SSPL are examples. | [Wikipedia](https://en.wikipedia.org/wiki/Source-available_software) |
| SPDX (Software Package Data Exchange) licence id | A short standard name for a licence, for example Apache-2.0, MIT or BSD-3-Clause. | [SPDX list](https://spdx.org/licenses/) |
| SSPL (Server Side Public License) | A copyleft licence from MongoDB. One of the three licences of the Elasticsearch source code. | [MongoDB](https://www.mongodb.com/legal/licensing/server-side-public-license) |
| trial (Elastic) | A 30-day trial of all paid Elastic features. It works once per major version. | [Elastic docs](https://www.elastic.co/docs/deploy-manage/license/manage-your-license-in-self-managed-cluster) |
| weights licence | The licence of a model's trained weights. It can differ from the code licence. Moirai and TimesFM 3.0 weights forbid commercial use. | [research/05](research/05-methods-benchmarks.md) |

## General computing terms

| Term | Meaning | Learn more |
|---|---|---|
| 1e-9 | Scientific notation for 0.000000001. "Scores match to 1e-9" means that they differ by less than that. | [Wikipedia](https://en.wikipedia.org/wiki/Scientific_notation) |
| AVX-512 (Advanced Vector Extensions) | 512-bit vector instructions on some x86 CPUs. They speed up maths on many numbers at once. | [Wikipedia](https://en.wikipedia.org/wiki/AVX-512) |
| CDN (content delivery network) | Servers that deliver web files from a place close to the user. | [Wikipedia](https://en.wikipedia.org/wiki/Content_delivery_network) |
| CLI (command-line interface) | A tool that you run by typing commands in a terminal. | [Wikipedia](https://en.wikipedia.org/wiki/Command-line_interface) |
| consistent hashing | A way to spread keys over servers. When a server is added or removed, only a small share of the keys move. | [Wikipedia](https://en.wikipedia.org/wiki/Consistent_hashing) |
| DAG (directed acyclic graph) | A graph with one-way edges and no loops, for example the calls inside one trace. | [Wikipedia](https://en.wikipedia.org/wiki/Directed_acyclic_graph) |
| DNS (Domain Name System) | The system that turns host names into IP addresses. | [Wikipedia](https://en.wikipedia.org/wiki/Domain_Name_System) |
| FLOP (floating-point operation) | One arithmetic step on decimal numbers. GFLOP and TFLOP mean 10⁹ and 10¹² of them. We use them to estimate LLM speed. | [Wikipedia](https://en.wikipedia.org/wiki/Floating_point_operations_per_second) |
| FNV-1a (Fowler–Noll–Vo) | A fast hash function. It is not meant for security. | [Wikipedia](https://en.wikipedia.org/wiki/Fowler%E2%80%93Noll%E2%80%93Vo_hash_function) |
| GC (garbage collection) | Automatic freeing of unused memory, for example in the Go runtime. | [Wikipedia](https://en.wikipedia.org/wiki/Garbage_collection_%28computer_science%29) |
| GitOps | Keeping configs in Git and applying them to the systems from there. | [OpenGitOps](https://opengitops.dev/) |
| gob | Go's own binary data format. anomalyd saves its snapshots in it. | [Go docs](https://pkg.go.dev/encoding/gob) |
| hash | A short, fixed-size value computed from data. The same input always gives the same hash. | [Wikipedia](https://en.wikipedia.org/wiki/Hash_function) |
| heap | Memory that a program allocates while it runs. Go's heap does not include all the memory that the process holds (see RSS). | [Wikipedia](https://en.wikipedia.org/wiki/Memory_management) |
| IO (input/output) | Reads and writes, usually to disk or over the network. | [Wikipedia](https://en.wikipedia.org/wiki/Input/output) |
| ISO 8601 timestamp | A standard date-time format, for example `2026-09-14T00:00:00.001Z`. | [Wikipedia](https://en.wikipedia.org/wiki/ISO_8601) |
| JVM (Java virtual machine) | The runtime for Java programs. OpenSearch keeps its anomaly models in the JVM's memory (heap). | [Wikipedia](https://en.wikipedia.org/wiki/Java_virtual_machine) |
| k, M, B (in numbers) | Thousand, million, billion. "3–5 k series" means 3,000–5,000 series. "1–10 M" means 1–10 million. | [Wikipedia](https://en.wikipedia.org/wiki/Metric_prefix) |
| LRU (least recently used) | A cache rule that drops the entry that was unused for the longest time. | [Wikipedia](https://en.wikipedia.org/wiki/Cache_replacement_policies#LRU) |
| MiB, GiB | Binary size units: 1 MiB = 1,048,576 bytes, 1 GiB = 1,073,741,824 bytes. MB and GB are 10⁶ and 10⁹ bytes. | [Wikipedia](https://en.wikipedia.org/wiki/Byte) |
| ms, µs | Milliseconds (thousandths of a second) and microseconds (millionths of a second). | [Wikipedia](https://en.wikipedia.org/wiki/Microsecond) |
| PR (pull request) | A proposed code change on GitHub. Others review it before it is merged. | [GitHub docs](https://docs.github.com/en/pull-requests/reference/pull-requests) |
| race detector | A Go test mode (`go test -race`) that finds unsafe shared-memory access between threads. | [Go docs](https://go.dev/doc/articles/race_detector) |
| regex (regular expression) | A text pattern, for example `[0-9]+` for any number. Filebeat's `replace` uses regexes to mask log lines. | [Wikipedia](https://en.wikipedia.org/wiki/Regular_expression) |
| ring buffer | A fixed-size buffer in which new values overwrite the oldest ones. anomalyd keeps each series in one. | [Wikipedia](https://en.wikipedia.org/wiki/Circular_buffer) |
| RSS (resident set size) | The memory that a process really holds in RAM. | [Wikipedia](https://en.wikipedia.org/wiki/Resident_set_size) |
| sharding | Splitting the work into parts (shards), for example by service, with one Drain tree per shard. | [Wikipedia](https://en.wikipedia.org/wiki/Shard_%28database_architecture%29) |
| SIGTERM | The Unix signal that asks a process to stop. anomalyd saves a snapshot when it gets one. | [Wikipedia](https://en.wikipedia.org/wiki/Signal_%28IPC%29#SIGTERM) |
| SSO (single sign-on) | One login for many apps. Vendors often sell it only in paid editions. | [Wikipedia](https://en.wikipedia.org/wiki/Single_sign-on) |
| stateless | Keeping no memory between inputs. A stateless template key is the same on every host and after every restart. | — |
| TLS (Transport Layer Security) | Encryption for network connections, as used by HTTPS. | [Wikipedia](https://en.wikipedia.org/wiki/Transport_Layer_Security) |
| token bucket | A rate limit that allows short bursts up to a fixed size, then a steady rate. | [Wikipedia](https://en.wikipedia.org/wiki/Token_bucket) |
| UI (user interface) | The screens that a person uses to work with a tool. | [Wikipedia](https://en.wikipedia.org/wiki/User_interface) |
| UUID (universally unique identifier) | A random 128-bit id. | [Wikipedia](https://en.wikipedia.org/wiki/Universally_unique_identifier) |
