# 05 — Evidence: which anomaly detection methods are worth it at our scale

**In short:**

- **Simple statistics win.** So our own "model" is simple statistics, not a neural network. On
  the [TSB-AD](https://github.com/TheDatumOrg/TSB-AD) benchmark, simple methods lead the
  univariate ranking. Sub-PCA is first with 0.42 [VUS-PR](#how-to-read-the-scores). VUS-PR runs
  from 0 to 1, and higher is better. Forecasting foundation models score lower:
  [TimesFM](https://github.com/google-research/timesfm) 0.30,
  [Chronos](https://github.com/amazon-science/chronos-forecasting) 0.27. The deep-learning
  model AnomalyTransformer is last at 0.12 (§1.1). So we use, for example, a seasonal median with
  [MAD](https://en.wikipedia.org/wiki/Median_absolute_deviation) (median absolute deviation).
- **Only self-reported results beat the simple methods.** Most are models pretrained for anomaly
  detection. Their own authors submitted them, and nobody has re-run them. One of the papers
  (Time-RCD) was withdrawn (§1.2, §2.1).
- **[Point adjustment](https://arxiv.org/abs/2109.05257) (PA) inflates many deep-learning claims.**
  With it, even random scores can look like the state of the art. A one-line rule solves 316 of 367
  Yahoo benchmark series (§1.4).
- **Foundation models can handle thousands of aggregated series, not millions (our estimate).**
  The estimate is for scoring every 5 min on about one CPU node. Evidence that they detect
  anomalies better on ops data is weak or comes from vendors. Non-commercial licences exclude
  Moirai and TimesFM 3.0 (§2).
- **Logs: count templates, then use statistics.** A template is a log line with its variable parts
  (numbers, IDs) masked. On public log data, classical methods do as well as deep learning or
  better. So a [Drain](https://jiemingzhu.github.io/pub/pjhe_icws2017.pdf)-style parser plus
  template counts runs on every line. A large language model (LLM) call per line would cost
  ~$400 k/day at 2024 GPT-3.5 prices. For today's cheapest batch price, see
  [research/04 §2.5](04-aiops-rca-llm-cost.md#25-why-cant-an-llm-read-all-raw-logs): about
  $12.5k/day for 2 TB/day of raw log text, input only. So LLMs only handle new templates and
  incidents (§3).
- **Industry: no cited operator pages on raw per-series anomalies.** Precision comes from 2-stage
  confirmation, health models, deduplication and Service Level Objective (SLO) burn-rate alerts
  (§5).

Terms and abbreviations: [glossary](../glossary.md).

Status: DRAFT COMPLETE (sections 1–6). Date: 2026-09-14. Links log: [LINKS.md](../../LINKS.md)
(merged, deduplicated).

Scope: our stack is Filebeat → Elasticsearch/Kibana (free Basic license), Prometheus + Grafana OSS
(the free open-source edition), and Jaeger. It carries 200 GB–2 TB/day of logs and 1–10 M active
series. So pre-aggregation is mandatory (see §6).

Paper venues (NeurIPS, PVLDB, ICSE and others) are spelled out in
[Where the cited papers were published](#where-the-cited-papers-were-published).

Sections:

- [How to read the scores](#how-to-read-the-scores)
- [1. Benchmarks: simple methods lead](#1-benchmarks-simple-methods-lead)
- [2. Foundation models: feasible on aggregates, weak evidence](#2-foundation-models-feasible-on-aggregates-weak-evidence)
- [3. Logs: count templates, then use statistics](#3-logs-count-templates-then-use-statistics)
- [4. Streaming methods for aggregated metrics](#4-streaming-methods-for-aggregated-metrics)
- [5. Industry: how operators keep alerts precise](#5-industry-how-operators-keep-alerts-precise)
- [6. Method tiers for our stack](#6-method-tiers-for-our-stack)
- [UNVERIFIED items](#unverified-items)
- [Where the cited papers were published](#where-the-cited-papers-were-published)

## How to read the scores

Most numbers in this file are one of these scores:

- **VUS-PR** (Volume Under the Surface of the Precision-Recall curve,
  [Paparrizos et al., PVLDB 2022](https://www.vldb.org/pvldb/vol15/p2774-paparrizos.pdf)). It rates
  an anomaly score without choosing a threshold. It also handles anomalies that span a range of
  points. It runs from 0 to 1, and higher is better. TSB-AD uses it as its main measure.
- **AUC-PR** and **AUC-ROC**: the area under the curve (AUC) of the
  [precision-recall curve](https://en.wikipedia.org/wiki/Precision_and_recall) or of the
  [ROC curve](https://en.wikipedia.org/wiki/Receiver_operating_characteristic) (receiver operating
  characteristic). Both run from 0 to 1, and higher is better.
- **Precision** is the share of alerts that are real. **Recall** is the share of real anomalies
  that were found. **F1** is their [harmonic mean](https://en.wikipedia.org/wiki/F-score).
- **PA-F1** is F1 after **point adjustment (PA)**. If a detector flags one point inside an anomaly
  range, PA counts the whole range as found. This makes scores look much better than they are (§1.4).
- **MASE** ([mean absolute scaled error](https://en.wikipedia.org/wiki/Mean_absolute_scaled_error))
  rates a forecast of single values. **CRPS**
  ([continuous ranked probability score](https://en.wikipedia.org/wiki/Scoring_rule#Continuous_ranked_probability_score))
  rates a forecast given as a range (quantiles). For both, lower is better.

Words used for models:

- **Foundation model (FM):** a large model pretrained on many datasets, so it can be used for many
  tasks ([definition](https://en.wikipedia.org/wiki/Foundation_model)). Most time-series foundation
  models forecast.
- **Zero-shot (ZS):** the model runs as downloaded, with no extra training. **Fine-tuned (FT):** the
  model got extra training for the task.
- **Univariate:** one series at a time. **Multivariate:** several related series together (MTS =
  multivariate time series).
- **Forecast-residual detection:** a model forecasts the next values, and the anomaly score is the
  forecast error.

## 1. Benchmarks: simple methods lead

This section covers the time-series benchmarks TSB-AD, TSB-UAD and newer ones.

### 1.1 TSB-AD results: statistical methods rank first

TSB-AD is a time-series anomaly detection benchmark by Liu & Paparrizos ("The Elephant in the
Room"). It appeared at NeurIPS 2024, in the Datasets and Benchmarks track. It has 1,070 curated
series from 40 datasets and tests 40 algorithms. The authors chose VUS-PR as the most reliable
measure. Their abstract says:

- simpler architectures and statistical methods often do better than advanced neural networks
- neural networks show promise on multivariate data
- foundation models show promise on point anomalies (single odd points)

Sources: [NeurIPS abstract](https://proceedings.neurips.cc/paper_files/paper/2024/hash/c3f3c690b7a99fba16d0efd35cb83b2c-Abstract-Datasets_and_Benchmarks_Track.html),
[repo, Apache-2.0](https://github.com/TheDatumOrg/TSB-AD), [leaderboard](https://thedatumorg.github.io/TSB-AD/).

The table shows the paper authors' own results: mean VUS-PR over the evaluation split. We
recomputed the means from the repo's per-series CSV files
([uni](https://raw.githubusercontent.com/TheDatumOrg/TSB-AD/main/benchmark_exp/benchmark_eval_results/uni_mergedTable_VUS-PR.csv),
[multi](https://raw.githubusercontent.com/TheDatumOrg/TSB-AD/main/benchmark_exp/benchmark_eval_results/multi_mergedTable_VUS-PR.csv)).
They match the [published U table](https://raw.githubusercontent.com/TheDatumOrg/TSB-AD/main/docs/static/leaderboard/TSB-AD-U.html)
and the [updated leaderboard](https://raw.githubusercontent.com/TheDatumOrg/TSB-AD/main/docs/static/leaderboard/TSB-AD-updated.html).

| Rank | TSB-AD-U (univariate, 350 series) | VUS-PR | TSB-AD-M (multivariate, 180 series) | VUS-PR |
|---|---|---|---|---|
| 1 | Sub-PCA (statistical) | 0.42 | CNN (simple neural network) | 0.31 |
| 2 | KShapeAD (clustering) | 0.40 | OmniAnomaly (deep learning) | 0.31 |
| 3 | POLY (polynomial-fit residual) | 0.39 | PCA (statistical) | 0.31 |
| 4 | Series2Graph | 0.39 | LSTMAD | 0.31 |
| 5 | MOMENT (FT) — foundation model | 0.39 | USAD | 0.30 |
| 6 | MOMENT (ZS) — foundation model | 0.38 | AutoEncoder | 0.30 |
| 7 | KMeansAD | 0.37 | KMeansAD | 0.29 |
| … | MatrixProfile 0.35, CNN 0.34, LSTMAD 0.33, SR 0.32 | scores inline | CBLOF 0.27, MCD 0.27 | scores inline |
| mid | **TimesFM 0.30, Chronos 0.27, Lag-Llama 0.27** (zero-shot forecast-residual foundation models) | scores inline | IForest 0.20, TimesNet 0.19, TranAD 0.18 | scores inline |
| last | AnomalyTransformer | 0.12 | AnomalyTransformer | 0.12 |

Notes:

1. The [TSB-AD README](https://github.com/TheDatumOrg/TSB-AD#detection-algorithm) describes every
   method in one line. Sub-PCA applies
   [principal component analysis (PCA)](https://en.wikipedia.org/wiki/Principal_component_analysis)
   to subsequences (short windows) of the series. KMeansAD uses
   [k-means clustering](https://en.wikipedia.org/wiki/K-means_clustering). IForest is
   [isolation forest](https://en.wikipedia.org/wiki/Isolation_forest).
2. Other short names: CNN = convolutional neural network. LSTMAD = a long short-term memory (LSTM)
   network. USAD = an adversarially trained autoencoder. CBLOF = cluster-based local outlier
   factor. MCD = minimum covariance determinant. SR =
   [Spectral Residual](https://arxiv.org/abs/1906.03821).

What the table shows:

- The top univariate scores are only ~0.4 VUS-PR. So even the best method is far from reliable.
- The best general foundation model is MOMENT (FT) at 0.386. Sub-PCA (0.423) beats it by ≈0.04.
  POLY (0.390) ties with it (gap ≈0). So the best general foundation model ties the second-tier
  statistical methods but does not beat them.
- The forecast-based foundation models (TimesFM, Chronos, Lag-Llama) score **below** POLY, Sub-PCA
  and MatrixProfile on VUS-PR.
- The same table shows how point adjustment inflates scores. PA-F1 reverses the ranking
  ([TSB-AD-U table](https://raw.githubusercontent.com/TheDatumOrg/TSB-AD/main/docs/static/leaderboard/TSB-AD-U.html)):

| Method | VUS-PR | PA-F1 |
|---|---|---|
| SR | 0.32 | 0.87 |
| TimesFM | 0.30 | 0.84 |
| Chronos | 0.27 | 0.83 |
| Sub-PCA (the VUS-PR winner) | 0.42 | only 0.56 |

### 1.2 Community leaderboard: higher scores, but self-reported

TSB-AD opened submissions in Apr 2026. The page says it was last updated on 2026-07-01. The
[updated leaderboard](https://raw.githubusercontent.com/TheDatumOrg/TSB-AD/main/docs/static/leaderboard/TSB-AD-updated.html)
adds methods that their own authors submitted. The per-series CSV files are in
[leaderboard_results/](https://github.com/TheDatumOrg/TSB-AD/tree/main/benchmark_exp/leaderboard_results).

| Method | Pretrained? | TSB-AD-U VUS-PR | TSB-AD-M VUS-PR |
|---|---|---|---|
| Time-RCD + MAFT (FT) | ✓ | 0.59 | not reported |
| TSPulse (FT) (IBM Granite) | ✓ | 0.55 | 0.39 |
| Time-RCD | ✓ | 0.52 | 0.32 |
| CHARM | ✓ | 0.50 | 0.39 |
| TSPulse (ZS) | ✓ | 0.48 | 0.36 |
| StreamVAE | ✗ | 0.45 | 0.44 |
| MMPAD | ✗ | 0.44 | 0.35 |
| (ref) Sub-PCA / PCA | ✗ | 0.42 | 0.31 |

(ref) = the classical leader from §1.1, for comparison.

**Warning:** the method authors submitted these results themselves. The
[submission guide](https://raw.githubusercontent.com/TheDatumOrg/TSB-AD/main/benchmark_exp/README.md)
says results are added "once verified". But nobody has published an independent re-run
(UNVERIFIED as independent reproductions).

These results suggest three things:

- Models pretrained for anomaly detection (Time-RCD, TSPulse, CHARM) gain the most over the
  classical leader: +0.06 to +0.17 VUS-PR on univariate data. General forecasting foundation models
  do not.
- Two submissions without pretraining (StreamVAE, MMPAD) also edge past it, by +0.02–0.03.
- StreamVAE leads TSB-AD-M (0.44).

### 1.3 Other benchmarks: no single best method

| Benchmark | Scale | Main finding for us |
|---|---|---|
| TSB-UAD, a univariate benchmark (PVLDB 15, 2022) ([paper](https://www.paparrizos.org/papers/PaparrizosVLDB22a.pdf), [repo](https://github.com/TheDatumOrg/TSB-UAD)) | 13,766 series (note 1); 12 methods evaluated | No single best method. Winners differ per dataset (note 2) |
| TSB-AutoAD (PVLDB 18(11), 2025) ([repo](https://github.com/thedatumorg/TSB-AutoAD)) | 20 automated methods / 70 variants | More than half of model-selection and ensembling solutions do not statistically beat a random choice (note 3) |
| TAB (PVLDB 2025) ([arXiv html](https://arxiv.org/html/2506.18046v1), [repo](https://github.com/decisionintelligence/TAB)) | 29 MTS datasets + 1,635 univariate series; 11 foundation methods | Univariate: non-learning and classical machine learning methods do best. Foundation methods are still "relatively poor" (note 4) |
| mTSBench (TMLR 2026) ([arXiv](https://arxiv.org/abs/2506.21550)) | 344 MTS, 19 datasets, 24 detectors | No detector dominates. The best automatic selectors are still far from the best choice per dataset |
| Pinet et al., Jun 2026 (MiLeTS workshop at KDD 2026) ([arXiv 2606.02670](https://arxiv.org/abs/2606.02670)) | 8 MTS benchmarks | Anomalies in current MTS benchmarks almost always show a univariate deviation (note 5) |

Notes:

1. The paper says 13,766 series. The repo README says 12,686, but its own parts (1,980 + 958 +
   10,828) sum to 13,766.
2. Winners by mean AUC-ROC (paper Table 3):
   - NORMA on ECG (electrocardiogram) data
   - [LOF](https://en.wikipedia.org/wiki/Local_outlier_factor) (local outlier factor) on MGAB
     (Mackey-Glass anomaly benchmark), with MatrixProfile 2nd
   - CNN on Yahoo
   - POLY on SMD (Server Machine Dataset)
3. TSB-AutoAD also finds that foundation models have not delivered the promised one-size-fits-all
   detector. Naive ensembling is robust but costly.
4. The best univariate methods in TAB are KMeans, DWT, S2G (Series2Graph) and OCSVM. DWT =
   discrete wavelet transform. OCSVM = one-class support vector machine. "Foundation methods" here
   means LLM-based and time-series-pretrained models. LLM-based methods use the most GPU (graphics
   processor) memory. Non-learning methods are the fastest.
5. For one recent state-of-the-art detector, the channel-dependent variant (which looks across
   series) gave no measurable gain over the channel-independent one.

### 1.4 Many benchmarks are flawed

- **Wu & Keogh** ([arXiv 2009.13807](https://arxiv.org/abs/2009.13807),
  [PDF](https://arxiv.org/pdf/2009.13807)) studied the Yahoo, Numenta, NASA and OMNI (Pei's lab)
  benchmarks. Most examples in them have at least one of four flaws:
  - triviality (a simple rule finds the anomaly)
  - unrealistic anomaly density (too many anomalies)
  - mislabeled ground truth
  - run-to-failure bias (anomalies sit near the end of the series)

  **A one-line rule solves 316 of 367 Yahoo series (86.1%)**, for example a `diff` or `movstd`
  threshold. The authors then introduced the UCR anomaly archive (UCR = University of California,
  Riverside).
- **Kim et al.** ([arXiv 2109.05257](https://arxiv.org/abs/2109.05257)): with the
  point-adjustment protocol, a random anomaly score can reach state-of-the-art F1. Even without
  point adjustment, an untrained model matches published methods.
- **What this means for us:** vendor or paper claims that quote PA-F1 on SMD, SMAP, MSL or Yahoo
  do not prove value in production. SMAP and MSL are NASA spacecraft telemetry sets. We require
  VUS-PR, AUC-PR, or event-level precision and recall on our own labelled incidents.

## 2. Foundation models: feasible on aggregates, weak evidence

This section covers Chronos, TimesFM, Moirai, MOMENT, TimeGPT, Toto/BOOM and Lag-Llama.

### 2.1 Which models exist, and under which licence?

We took the weight licences and parameter counts from the Hugging Face (HF) API on 2026-09-14.
M = million parameters, B = billion. CC-BY-NC-4.0 = Creative Commons Attribution-NonCommercial 4.0
(no commercial use).

| Model (latest) | Weights licence | Params | Built for anomaly detection? | TSB-AD-U VUS-PR (zero-shot) | Notes |
|---|---|---|---|---|---|
| Chronos-2 (Oct 2025) | Apache-2.0 | 120M ([HF](https://huggingface.co/api/models/amazon/chronos-2)); chronos-2-small 28M | No: forecast quantiles → residual/interval | Original Chronos (T5): 0.27 | Note 1 |
| Chronos-Bolt | Apache-2.0 | tiny 9M / mini 21M / small 48M / base 205M | No | not evaluated | Up to 250× faster and 20× more memory-efficient than original Chronos; runs on CPU ([AWS blog](https://aws.amazon.com/blogs/machine-learning/fast-and-accurate-zero-shot-forecasting-with-chronos-bolt-and-autogluon/)) |
| TimesFM 2.5 (Sep 2025) | Apache-2.0 | named 200M; 231M in HF safetensors ([HF](https://huggingface.co/api/models/google/timesfm-2.5-200m-pytorch)) | No | TimesFM (v1): 0.30 | 16k context, quantile head ([README](https://raw.githubusercontent.com/google-research/timesfm/master/README.md)) |
| TimesFM 3.0 (Aug 2026) | **`timesfm-non-commercial-license-v1.0`**: non-commercial, non-production | 331M ([HF](https://huggingface.co/api/models/google/timesfm-3.0-pytorch)) | No | not evaluated | **Not usable in our production** (note 2) |
| Moirai 1.0/1.1/MoE/2.0 (2.0: Aug 2025) | **CC-BY-NC-4.0** (weights); code Apache-2.0 ([uni2ts](https://github.com/SalesforceAIResearch/uni2ts)) | 2.0-small 11M; 1.1 small 14M / base 91M / large 311M ([HF](https://huggingface.co/api/models?author=Salesforce&search=moirai)) | No | not in TSB-AD | Non-commercial weights → **not usable in production** |
| MOMENT-1 (ICML 2024) | MIT | small 38M / base 113M / large 346M ([HF](https://huggingface.co/api/models/AutonLab/MOMENT-1-large)) | **Yes** (reconstruction error) ([repo](https://github.com/moment-timeseries-foundation-model/moment)) | 0.38 (ZS), 0.39 (FT) (note 3) | Best general FM in the original TSB-AD table, still below Sub-PCA 0.42 |
| Lag-Llama | Apache-2.0 | 2.45M ([HF](https://huggingface.co/api/models/time-series-foundation-models/Lag-Llama)) | No | 0.27 | Superseded; not recommended |
| Toto 1.0 (May 2025) | Apache-2.0 | 151M ([HF](https://huggingface.co/api/models/Datadog/Toto-Open-Base-1.0)) | No (forecast) | not evaluated | Trained on >2T points, ~1T of them Datadog internal observability metrics ([repo](https://github.com/DataDog/toto)) |
| Toto 2.0 (Apr–May 2026) | Apache-2.0 | 4M / 22M / 313M / 1B / 2.5B ([HF](https://huggingface.co/api/models?author=Datadog)) | No (forecast) | not evaluated | Built for observability. 22m card: ~5 ms per 1024-step forecast at batch 8 on an A100 ([card](https://huggingface.co/Datadog/Toto-2.0-22m)) |
| TSPulse (IBM Granite, May 2025) | Apache-2.0 | **1.08M** ([HF](https://huggingface.co/api/models/ibm-granite/granite-timeseries-tspulse-r1)) | **Yes** (dual time/frequency masked reconstruction) | 0.48 ZS / 0.55 FT (self-reported, §1.2) | Note 4 |
| Time-RCD | Apache-2.0 | 37M ([HF](https://huggingface.co/thu-sail-lab/Time-RCD)) | Yes (pretrained for anomaly detection) | 0.52 / 0.59 (MAFT), self-reported | **Do not depend on it** (note 5) |
| TimeGPT (Nixtla) | Proprietary API; self-hosted = enterprise | UNVERIFIED | Yes: `detect_anomalies` (note 6) ([docs](https://www.nixtla.io/docs/capabilities-anomaly-detection-anomaly_detection)) | not evaluated | Out of scope: not free or open source (note 7) |

Notes:

1. Chronos-2 handles univariate and multivariate series plus covariates (extra input series). The
   repo claims state-of-the-art zero-shot results on fev-bench and GIFT-Eval. It also claims a >90%
   head-to-head win rate against Bolt ([repo](https://github.com/amazon-science/chronos-forecasting)).
   chronos-2-small is published as `autogluon/chronos-2-small`. T5 is the Google language-model
   architecture that the original Chronos builds on.
2. TimesFM 3.0 handles multivariate series plus covariates. Its README claims #1 on fev-bench, TIME
   and GIFT-Eval ([README](https://raw.githubusercontent.com/google-research/timesfm/master/README.md)).
   fev-bench, TIME and GIFT-Eval are forecasting benchmarks.
3. TSB-AD ran MOMENT-1-base.
4. The TSPulse card advertises GPU-free inference
   ([card](https://huggingface.co/ibm-granite/granite-timeseries-tspulse-r1)). TSPulse is the
   smallest permissively licensed pretrained model built for anomaly detection that we found.
   Another small one, Time-RCD at 37M, has a withdrawn paper (see its row).
5. RCD stands for Relative Context Discrepancy, its pretraining method. The authors **withdrew** the
   arXiv paper on 2026-05-29 ([arXiv 2509.21190](https://arxiv.org/abs/2509.21190)). But the model
   card claims ICML 2026 acceptance (UNVERIFIED). The TSB-AD repo ships MAFT only as a compiled
   `.so` file ([repo](https://github.com/TheDatumOrg/TSB-AD)).
6. `detect_anomalies` = forecast + 99% interval by default.
7. There is no public pricing, only a 30-day trial. Enterprise and self-hosted plans are by contact
   ([plans](https://www.nixtla.io/docs/introduction/timegpt_subscription_plans)).

### 2.2 What is the evidence on observability data?

**BOOM, the vendor's benchmark.** BOOM (Datadog) has ~350M points, 2,807 series and 32,887
variates of Datadog pre-production telemetry. It covers infra, network, databases, security and
applications ([dataset card](https://huggingface.co/datasets/Datadog/BOOM)). Table 2 of the Toto 1.0
paper ([PDF](https://arxiv.org/pdf/2505.14766)) gives zero-shot results. MASE and CRPS are
normalised to the [seasonal-naive](https://otexts.com/fpp3/simple-methods.html) forecast, and lower
is better:

| Model | MASE | CRPS |
|---|---|---|
| Toto | 0.617 | 0.375 |
| Moirai-base | 0.710 | 0.428 |
| TimesFM 2.0 | 0.725 | 0.447 |
| Chronos-Bolt-base | 0.726 | 0.451 |
| **Auto-ARIMA** | **0.824** | **0.736** |
| **Auto-ETS** | **0.842** | **1.975** |
| **Auto-Theta** | **1.123** | **1.018** |

The last three are classical forecasting models with automatic tuning:

- [ARIMA](https://en.wikipedia.org/wiki/Autoregressive_integrated_moving_average) (autoregressive
  integrated moving average)
- ETS ([exponential smoothing](https://en.wikipedia.org/wiki/Exponential_smoothing) with error,
  trend and seasonal parts)
- the Theta method

On observability *forecasting*, foundation models beat Auto-ARIMA by ~12–25% MASE. They beat
Auto-ETS and Auto-Theta by more. But this is the vendor's own benchmark. It measures forecast
accuracy, not anomaly detection precision.

**Toto 2.0 on BOOM** ([Datadog blog, 2026-05-14](https://www.datadoghq.com/blog/ai/toto-2/)). CRPS
rank, lower is better: 2.5B 3.88, 1B 3.96, 313m 4.25, 22m 5.52, Toto 1.0 6.94, Chronos-2 7.39.

**Counter-evidence: Toner et al.** ([arXiv 2502.12944](https://arxiv.org/abs/2502.12944),
[PDF](https://arxiv.org/pdf/2502.12944)). This Huawei study appeared at the ICLR 2025 workshop
"I Can't Believe It's Not Better". It used Huawei Cloud function-request data. Two simple models
consistently beat the foundation models: a per-channel online linear model and a naive seasonal
forecaster. Seasonal naive beat every tested foundation model on every dataset and horizon. Its
MASE was often about half of TimesFM's. **Limit:** the study tested older, smaller models:
TimesFM 1.x, Chronos-tiny, Moirai-small, TTM (IBM Tiny Time Mixers), VisionTS and Mamba4Cast. It
did not test Chronos-2, TimesFM-2.5 or Toto.

**Why forecast-residual detection with foundation models does poorly.** Uray et al.
([arXiv 2607.12454](https://arxiv.org/abs/2607.12454), Jul 2026, EUROCAST 2026) tested one model on
one industrial benchmark, SWaT (Secure Water Treatment). TimesFM keeps its forecast error low
*inside* a sustained anomaly, because the anomaly is already in its context. Its error peaks only at
the anomaly boundaries. So it behaves like a
[change-point detector](https://en.wikipedia.org/wiki/Change_detection), not a detector for
sustained anomalies. This matches TSB-AD, where forecasting models (TimesFM 0.30, Chronos 0.27)
trail POLY and Sub-PCA (§1.1).

**Other studies.** TSB-AutoAD concludes that general foundation models have not yet delivered a
one-size-fits-all detector ([repo](https://github.com/thedatumorg/TSB-AutoAD)). A study of
operational viability (May 2026, [arXiv 2605.24381](https://arxiv.org/abs/2605.24381)) looked at
forecasting on finance, transport, energy and demand data, not ops telemetry. It finds foundation
models useful for cold-start series (little history), long-tail series and periodic series. It
proposes to send only such series to foundation models.

### 2.3 Can a CPU run them on thousands of aggregated series every few minutes?

Published speed figures:

- TimesFM 3.0 (330M) ran on an Apple M4 Max, through Apple's MLX framework on the Apple GPU.
  Settings: fp32 (32-bit floating point), context 512, horizon 64. Speed: 90 series/s at batch 1,
  666 series/s at batch 32 ([README](https://raw.githubusercontent.com/google-research/timesfm/master/README.md)).
- Toto 2.0-22m: ~5 ms per batch-8 forecast on an NVIDIA A100 GPU
  ([card](https://huggingface.co/Datadog/Toto-2.0-22m)).

No vendor publishes x86 CPU numbers. AutoGluon's docs say only two things: all Bolt models run on
CPU or GPU, and the original Chronos small and larger need a GPU
([AutoGluon docs](https://auto.gluon.ai/1.2.0/tutorials/timeseries/forecasting-chronos.html)). So
the CPU table below is an **estimate, not measured**.

Assumptions:

- context of 512 points (≈8.5 h at a 1-min step)
- one forward pass per series (no sampling)
- forward FLOPs ≈ 2 × params × tokens (FLOP = floating-point operation, GFLOP = 10^9 FLOP,
  TFLOP = 10^12 FLOP)
- ~20–40 patch tokens per series (TSPulse up to ~130, including its frequency view)
- sustained PyTorch fp32 speed ≈ 50 GFLOP/s per modern x86 core, ≈25% of the AVX-512
  per-core peak (AVX-512 = 512-bit vector instructions)
- a 16-core node ≈ 0.8 TFLOP/s

| Model | ≈GFLOP / series | 5,000 series / cycle | 50,000 series / cycle | Fits a 5-min cycle on one 16-core node? |
|---|---|---|---|---|
| TSPulse 1M | ~0.05–0.3 | < 2 s | ~3–20 s | Yes, trivially |
| Toto-2.0-22m / Chronos-Bolt-small 48M | ~1–3 | ~6–20 s | ~1–3 min | Yes |
| Chronos-2 120M | ~8 | ~50 s | ~8–9 min | 5k yes; 50k no (2–3 nodes or 1 GPU) |
| TimesFM 2.5 (231M) | ~8–15 | ~1–1.5 min | ~8–15 min | 5k yes; 50k no |

**Sanity check:** TimesFM 3.0 ≈ 12 GFLOP/series × 666 series/s ≈ 8 TFLOP/s on the M4 Max GPU. That
is plausible for a laptop GPU with a peak in the low tens of TFLOP/s (peak figure UNVERIFIED). So
the FLOP model is in the right range.

**For comparison:** a robust [z-score](https://en.wikipedia.org/wiki/Standard_score),
[EWMA](https://en.wikipedia.org/wiki/Exponential_smoothing) or
[Holt-Winters](https://www.statsmodels.org/stable/generated/statsmodels.tsa.holtwinters.ExponentialSmoothing.html)
update is O(1) per point. That is constant time (~µs). So it is 10^5–10^6× cheaper per series.
EWMA = exponentially weighted moving average.

**Conclusion (§2):**

- **Thousands of pre-aggregated series: feasible.** Take a permissively licensed foundation model
  (Chronos-Bolt/Chronos-2, TimesFM 2.5, Toto 2.0, TSPulse). On CPU it can score **thousands** of
  pre-aggregated series every 5 min (≈1 node).
- **The raw 1–10 M Prometheus series: not practical.** The FLOP model above gives the node
  counts. Chronos-2 or TimesFM 2.5 needs ≈30–600 nodes. Toto-22m or Bolt-small needs ≈4–125.
  Even TSPulse needs ≈1–13 nodes. On top of that, each cycle must re-read a 512-point context
  for every raw series from Prometheus.
- **Weak evidence for detection.** Do foundation models improve *anomaly detection precision* on
  ops data over POLY, PCA or robust baselines? The evidence is weak or vendor-produced. The
  strongest case is forecasting and capacity, and cold-start series.
- **Licences:** non-commercial weight licences exclude Moirai and TimesFM 3.0.

## 3. Logs: count templates, then use statistics

This section covers log datasets (Loghub), log parsers, and deep learning vs classical methods for
logs.

### 3.1 Which labelled log datasets exist?

- **[Loghub](https://github.com/logpai/loghub)** (ISSRE 2023, [paper](https://arxiv.org/abs/2008.06448))
  has 19 datasets. The paper gives ~77 GB in total. The README table now adds up to about
  83 GiB (checked 2026-09-15). Only 6 carry labels: HDFS_v1, HDFS_v3/TraceBench, BGL, Thunderbird,
  Hadoop, OpenStack. HDFS is the Hadoop Distributed File System. BGL is the Blue Gene/L
  supercomputer.
- **Loghub-2.0** (ISSTA 2024, [repo](https://github.com/logpai/loghub-2.0),
  [arXiv 2308.10828](https://arxiv.org/abs/2308.10828)) has 14 datasets. They average 3.6 M
  annotated lines each (HDFS > 11 M, Thunderbird > 16 M). It is meant for evaluating *parsing*.

### 3.2 Log parsing: Drain vs newer parsers vs LLM parsers

A log parser splits each line into a template (the fixed text) and parameters (the variable parts).
[Drain](https://jiemingzhu.github.io/pub/pjhe_icws2017.pdf) is a streaming parser that uses a
fixed-depth tree. Parsers are rated by Group Accuracy (GA): the share of lines put in the right
template group. FGA (F1-score of Group Accuracy) is the same idea counted per template, so rare
templates count as much as common ones.

| Finding | Source |
|---|---|
| **9 of 15 parsers could not finish all 14 Loghub-2.0 datasets within 12 h.** Drain finished, with the best average GA of the six finishers (note 1) | [Loghub-2.0 PDF](https://arxiv.org/pdf/2308.10828), [effectiveness CSV](https://raw.githubusercontent.com/logpai/loghub-2.0/main/RQs_experiments/RQ2/effectiveness_results.csv), [efficiency CSV](https://raw.githubusercontent.com/logpai/loghub-2.0/main/RQs_experiments/RQ2/efficiency_results.csv) |
| All parsers stay weak on rare and parameter-heavy templates. Those are exactly the logs that matter in incidents | same |
| Parsing accuracy does **not** strongly predict anomaly detection accuracy. Template *distinguishability* matters | Khan et al., EMSE 2024 ([arXiv 2305.15897](https://arxiv.org/abs/2305.15897)) |
| With a template cache, LLM parsers call the LLM only for *new* templates: ≈84 k tokens for ~16 M lines, ≈$0.04 (note 2) | LogBatcher, ASE 2024 (note 3) ([PDF Table 5](https://arxiv.org/pdf/2406.06156v1), [repo](https://github.com/LogIntelligence/LogBatcher)) |
| Naive per-line LLM parsing: (66 instruction + 55 log) tokens × 100 M lines × $0.5/M = **$6,050 per 100 M lines** | same paper, §3 |

Notes:

1. Drain's average GA is 0.84. AEL scores 0.86 but timed out on Spark. Drain's FGA (0.55) trails
   IPLoM (0.61) and LogPPT (0.59). The semantic parsers (UniParser, LogPPT) need a GPU and are much
   slower on CPU. AEL, IPLoM, LogPPT and UniParser are other published parsers.
2. LogBatcher ran on four Loghub-2.0 datasets: HDFS, BGL, OpenStack and Zookeeper. It used 11,646
   tokens for HDFS (11.2 M lines) and 47,428 for BGL (4.6 M). That is ≈84 k tokens in total for
   ~16 M lines. LILAC, another LLM parser, used ≈144 k tokens. GA on BGL: LogBatcher 0.952 vs LILAC
   0.910. At the paper's GPT-3.5 price of $0.5 per million tokens, ≈84 k tokens cost ≈$0.04.
3. The proceedings title is "Demonstration-Free: Towards More Practical Log Parsing with Large
   Language Models".

**Scale check for our logs (estimate):** 2 TB/day at ~300 B/line ≈ 6–7 × 10^9 lines/day ≈ 77 k
lines/s.

- An LLM call per line would cost ≈ $400 k/day at the price above (2024 GPT-3.5). That is
  impossible. For today's cheapest batch price, see
  [research/04 §2.5](04-aiops-rca-llm-cost.md#25-why-cant-an-llm-read-all-raw-logs). There,
  2 TB/day of raw log text costs about $12.5k/day, counting input tokens only.
- Calling the LLM only for new templates is cheap. Templates number 10^3–10^5, not 10^9
  (UNVERIFIED for our logs).
- So the code that runs for every line must be a Drain-style streaming matcher, or ingest-time
  dissect/grok parsing. It produces template IDs, and we count them.

### 3.3 Does deep learning beat classical methods on logs?

| Study | Finding |
|---|---|
| Le & Zhang, ICSE 2022 ([arXiv 2202.04301](https://arxiv.org/abs/2202.04301)) | Results of 5 deep-learning models change a lot with evaluation choices. The authors conclude log anomaly detection is not solved (note 1) |
| Yu et al., ICSE 2024 (CUHK-SZ + Huawei Cloud) ([ICSE page](https://conf.researchr.org/details/icse-2024/icse-2024-research-track/21/Deep-Learning-or-Classical-Machine-Learning-An-Empirical-Study-on-Log-Based-Anomaly-)) | Simple algorithms beat deep learning on both accuracy and time. On Thunderbird, KNN trains ~1,000× faster than NeuralLog with +0.0625 F1 (note 2) |
| Landauer et al., FSE 2024 ([arXiv 2309.02854](https://arxiv.org/abs/2309.02854)) | Most anomalies do not show as sequence changes. Simple detectors are competitive with published deep-learning results (note 3) |
| Ali et al., EMSE 2025 ([arXiv 2307.16714](https://arxiv.org/html/2307.16714v5)) | Supervised Random Forest ≈ deep learning. **Semi-supervised (the realistic no-label setting) is significantly worse than supervised** (note 4) |
| Nyyssölä & Mäntylä (QRS) ([arXiv 2312.01934](https://arxiv.org/abs/2312.01934)) | Parser-free, unsupervised: OOV detector on char-trigrams AUC-ROC 0.846; IsolationForest on event counts 0.829 (note 5) |
| Patel, Apr 2026, single-author benchmark ([arXiv 2604.12218](https://arxiv.org/abs/2604.12218)) | Fine-tuned transformers F1 0.96–0.99. Zero-shot prompted LLMs F1 0.82–0.91 (note 6) |
| LLM4Log systematic review, 145 papers, rev. Sep 2026 ([arXiv 2604.16359](https://arxiv.org/abs/2604.16359)) | LLMs add semantic generalisation but bring new problems (note 7) |

Notes:

1. Le & Zhang tested 5 deep-learning models on 4 datasets, with no classical baselines. Several
   settings change results substantially: training-data selection, grouping, class imbalance,
   label noise and early detection. Random rather than chronological training splits can leak data
   and inflate scores. The models do not consistently work well.
2. CUHK-SZ = The Chinese University of Hong Kong, Shenzhen. KNN = k-nearest neighbours. The authors
   name three causes: redundant preprocessing, simple datasets, and the binary-classification
   nature of the task.
3. Landauer et al. reviewed six datasets: HDFS, BGL, Thunderbird, OpenStack, Hadoop and ADFA (an
   intrusion-detection dataset). In a semi-supervised experiment on five of them, three simple
   detectors reach detection rates competitive with published deep-learning results: new event
   type, sequence length and count vector. **Limit:** the HDFS score depends on the dataset
   version. F1 is 90.4% on the LogDeep preprocessed split vs 72.0% on the original.
4. Supervised traditional machine learning (Random Forest) ≈ deep learning in accuracy and
   prediction time. It is also less sensitive to hyper-parameters.
5. OOV = out-of-vocabulary: words not seen in training. The OOV detector trains on normal logs
   only. OOV and rarity models are the fastest.
6. Datasets: HDFS, BGL, Thunderbird and Spirit. Landauer et al. show that these datasets are easy.
7. The problems: context-length limits, latency and cost, privacy, and hallucination. The review
   calls for drift-robust, verifiable deployments.

### 3.4 Conclusion for 2 TB/day: template counts plus statistics

Yes. The evidence supports **template-ID counts plus statistical detection** as capturing most of
the value we can detect. The detectors are:

- a new (unseen) template
- a rate change per template × service
- the error-level ratio
- an outlier in the count vector

Three findings support this:

- Most anomalies in public datasets are not about event order, and novelty or count methods find
  them (Landauer).
- Classical machine learning does as well as deep learning or better in the supervised, labelled
  setting (Yu, Ali). In Ali's study, semi-supervised learning was worse.
- Parsing accuracy beyond good-enough grouping does not buy detection accuracy (Khan).

LLMs belong *off* the code that runs for every line. Use them to name new templates, explain a
flagged burst of a template, or summarise an incident. That is 10^1–10^3 events/day, not 10^9
lines.

**Limit:** all academic evidence comes from a handful of small labelled datasets (HDFS, BGL,
Thunderbird). No public benchmark exists for 2 TB/day of mixed microservice logs (UNVERIFIED that
the ranking transfers).

## 4. Streaming methods for aggregated metrics

These methods score pre-aggregated metric series. Most of them update with each new point
(streaming, or "online"). We took library versions and licences from PyPI JSON
(`https://pypi.org/pypi/<pkg>/json`) and GitHub `releases/latest`, checked 2026-09-14.

### 4.1 What each method catches and needs

| Method | What it catches | Cost per series | Seasonality | Cold start |
|---|---|---|---|---|
| Robust z-score / MAD band | Spikes and drops against a recent baseline; robust to outliers in the baseline | O(1)/point with rolling stats (≈µs). In PromQL: one recording rule per aggregate | Only via a seasonal baseline (note 1) | ≈1 baseline window (hours); 1–4 weeks for weekly offsets |
| EWMA / exponentially weighted variance | Level shifts and spikes; fast reaction | O(1) time and memory | None: use a deseasonalised residual, or one per hour-of-week | ≈1/α points (α = smoothing factor) |
| STL / MSTL residual + threshold | Anomalies left after removing trend + daily + weekly cycles (note 2) | Batch LOESS refit per series (note 3) | **Native, multiple periods** ([statsmodels MSTL](https://www.statsmodels.org/stable/generated/statsmodels.tsa.seasonal.MSTL.html), [paper](https://arxiv.org/abs/2107.13462)) | ≥2 full longest periods (≥2 weeks for weekly) |
| Holt-Winters (triple exponential smoothing) forecast + interval | Deviations from a seasonal forecast. Also gives capacity forecasts | Fit O(n·iterations), update O(1) | Single season (additive or multiplicative) | ≥2 seasons |
| Random Cut Forest (RCF) | Multivariate and shingled-shape anomalies (note 4) | Per-point update ≈ O(trees × dimensions × tree depth) (note 5) | Implicit via shingles; no explicit weekly model | Needs history (note 6) |
| Half-Space Trees | Streaming isolation of scattered outliers in feature vectors, for example per-service [rate, errors, p99] (note 7) | Linear in trees, exponential in tree height (note 7) | None: feed deseasonalised residuals or time features | 1 window (250 points by default) |
| SPOT / DSPOT (EVT thresholds) | Not a detector itself. Turns *any* score or residual stream into a threshold (note 8) | O(1) for normal points; GPD tail refit when a new excess arrives (note 9) | None (apply to residuals) | Initial calibration batch to fit the GPD tail (size UNVERIFIED) |
| Matrix Profile discords | Unusual *shapes*: subsequences unlike any seen before (note 10) | Batch exact Matrix Profile is quadratic in length (note 11) | Implicit (compares to all history) | Needs history with normal behaviour; window m must be chosen |

Notes:

1. The baseline is the same time yesterday or last week. Grafana's rules add a band with a 23h30m
   offset ([Grafana blog](https://grafana.com/blog/how-to-use-prometheus-to-efficiently-detect-anomalies-at-scale/)).
2. STL = seasonal-trend decomposition using [LOESS](https://en.wikipedia.org/wiki/Local_regression)
   (local regression). MSTL = STL with multiple seasonal periods. It works best for strongly
   periodic [RED](https://grafana.com/blog/2018/08/02/the-red-method-how-to-instrument-your-services/)
   metrics (rate, errors, duration).
3. The timing is UNVERIFIED. We expect ms-scale for a few weeks of data at a 5-min step. Refit
   hourly or daily, and score new points against the last fit.
4. RCF is streaming and unsupervised, and gives a score plus a confidence. A shingle is a run of
   consecutive buckets treated as one vector.
5. Tree depth is ~log(sample size) for typical points (Guha et al.,
   [PMLR](https://proceedings.mlr.press/v48/guha16.html)). Per-model memory is UNVERIFIED. RCF is
   fine for 10^3–10^4 models, not 10^6.
6. OpenSearch uses up to 10,000 history points for the cold-start model
   ([OpenSearch anomaly detection docs](https://docs.opensearch.org/latest/observing-your-data/ad/index/)).
7. p99 = 99th-percentile latency. Cost from the river docs. Defaults: 10 trees, height 8, window
   250.
8. SPOT = Streaming Peaks-Over-Threshold, based on
   [extreme value theory](https://en.wikipedia.org/wiki/Extreme_value_theory) (EVT). It sets the
   threshold at a chosen tail probability q. DSPOT also tracks drift.
9. GPD = generalized Pareto distribution, fitted to the tail. SPOT has a small footprint: it is
   written in C99 (standard C) with no dependencies ([docs](https://asiffer.github.io/libspot)).
10. For example an odd saw-tooth, or a series stuck at a constant. A discord is the subsequence
    most unlike all others.
11. See [Matrix Profile I](https://www.cs.ucr.edu/~eamonn/PID4481997_extend_Matrix%20Profile_I.pdf).
    `stumpi` is the incremental version. DAMP (Discord Aware Matrix Profile) computes exact
    left-discords, which compare only with earlier data. It does this on streams up to 300 kHz on a
    desktop ([DAMP PDF](https://www.cs.ucr.edu/~eamonn/DAMP_long_version.pdf)).

### 4.2 Which libraries implement them?

| Library | Methods | Version (date) | Licence | Source / limits |
|---|---|---|---|---|
| PromQL `stddev_over_time`, `quantile_over_time` | z-score / MAD band | stable | built into Prometheus | [PromQL docs](https://prometheus.io/docs/prometheus/latest/querying/functions/) |
| PromQL `mad_over_time` | MAD band | **experimental flag** | built into Prometheus | same docs |
| `grafana/promql-anomaly-detection` | z-score / MAD band | v0.2.1 | Apache-2.0 | [repo](https://github.com/grafana/promql-anomaly-detection) |
| river `stats.EWMean/EWVar` | EWMA | river 0.26.1 (2026-08-21) | BSD-3 | PyPI |
| PromQL `double_exponential_smoothing` | Holt *linear*: level + trend, **no seasonal term** | experimental; called `holt_winters` in v2 | built into Prometheus | [PromQL docs](https://prometheus.io/docs/prometheus/latest/querying/functions/). So Holt-Winters is not native in Prometheus |
| statsmodels | STL/MSTL; Holt-Winters `ExponentialSmoothing` | 0.15.0 (2026-08-27) | BSD-3 | [docs](https://www.statsmodels.org/stable/generated/statsmodels.tsa.holtwinters.ExponentialSmoothing.html) |
| statsforecast | MSTL; AutoETS | 2.1.1 (2026-07-16) | Apache-2.0 | PyPI |
| AWS `random-cut-forest-by-aws` | RCF (Java/Rust) | release 4.4.0-java | Apache-2.0 | [repo](https://github.com/aws/random-cut-forest-by-aws) |
| Python `rrcf` | RCF | 0.4.4 (**last release 2023-04**) | MIT | pure Python = slow |
| OpenSearch anomaly detection plugin | RCF (embedded) | 3.8.0.0 (2026-07-31) | not checked | Requires OpenSearch, not Elasticsearch |
| river `anomaly.HalfSpaceTrees` | Half-Space Trees | river (same package) | BSD-3 | [river docs](https://riverml.xyz/latest/api/anomaly/HalfSpaceTrees/); Tan et al. IJCAI 2011 ([PDF](https://www.ijcai.org/Proceedings/11/Papers/254.pdf)) (note 1) |
| `libspot` | SPOT / DSPOT | 3.1.0 (2026-09-06) | **LGPL-3.0-or-later** | Python + JavaScript bindings ([repo](https://github.com/asiffer/libspot)); Siffer et al. KDD 2017 ([DOI](https://dl.acm.org/doi/10.1145/3097983.3098144)) |
| `pylibspot` | SPOT | 1.1.3 | GPL-3.0 | older package |
| `stumpy` | Matrix Profile discords | 1.14.1 (2026-02-08) | BSD-3 | [docs](https://stumpy.readthedocs.io/en/latest/Tutorial_STUMPY_Basics.html), [API](https://stumpy.readthedocs.io/en/latest/api.html) (note 2) |

Notes:

1. The river docs warn about two limits: features must be scaled to [0,1], and the method is weak
   when anomalies arrive clustered in a window.
2. MatrixProfile scores 0.35 VUS-PR on TSB-AD-U: mid-pack, above all forecast foundation models.
3. Licences: BSD, MIT and Apache-2.0 are permissive. LGPL = GNU Lesser General Public License.
   GPL = GNU General Public License. MPL = Mozilla Public License.

Other libraries:

- PyOD 3.6.5 (2026-08-17, BSD-2) has batch outlier detectors (IForest, PCA, KNN).
- `tsb-ad` 1.5 (Apache-2.0) packages the TSB-AD algorithms, including Sub-PCA and POLY.
- `adtk` 0.6.2 (MPL-2.0, **last release 2020**), `streamad` 0.3.1 (2023) and `salesforce-merlion`
  2.0.4 (2024-06) look unmaintained. Do not build on them.

**Practical note:** the TSB-AD leaders (Sub-PCA, POLY, KShapeAD) are **batch** window methods. At
scale they run as periodic jobs over a sliding window of pre-aggregated series, not per sample.

## 5. Industry: how operators keep alerts precise

This section collects evidence on alert fatigue and precision from companies that run anomaly
detection at scale.

| Source | Evidence | Lesson for us |
|---|---|---|
| Etsy Kale/Skyline, Jun 2013 ([Etsy blog](https://www.etsy.com/codeascraft/introducing-kale), [archived original](http://web.archive.org/web/20220127183214/https://codeascraft.com/2013/06/11/introducing-kale/)) | >250k metrics. Skyline did not alert, because alerting "would be very noisy" (note 1) | Anomaly detection over all metrics gives a false-positive feed, not pages. Don't page on raw per-metric anomalies |
| Uber Argos, Nov 2015 ([Uber blog](https://www.uber.com/blog/argos-real-time-alerts/), [archived original, 2015-11-24](http://web.archive.org/web/20191118111719/https://eng.uber.com/argos/)) | Tens of millions of metrics. Seasonal thresholds plus a **second-stage outage detector**. 9 of 10 pages were real outages (note 2) | Precision came from the 2-stage design, not from a better single detector (note 3) |
| Netflix Telltale, Aug 2020 ([Netflix TechBlog](https://netflixtechblog.com/telltale-netflix-application-monitoring-simplified-5c08bfa780ba), [archived](http://web.archive.org/web/2020/https://netflixtechblog.com/telltale-netflix-application-monitoring-simplified-5c08bfa780ba)) | A per-app health model that combines many signals. One notification per issue (note 4) | Correlate, route and dedupe. Anomaly detection is one signal inside a health model |
| LinkedIn ThirdEye Smart Alerts, Jun 2019 ([LinkedIn blog](https://www.linkedin.com/blog/engineering/analytics/smart-alerts-in-thirdeye-linkedins-real-time-monitoring-platfor)) | Noise filters after detection (note 5). No precision numbers published | Post-detection filters (min duration, min % change, business impact) remove much of the noise |
| Huawei Cloud, DSN 2022 ([arXiv 2204.09670](https://arxiv.org/abs/2204.09670)) | Millions of alerts over 2 years + 18 engineers: 6 alert anti-patterns (note 6) | Measure alert quality explicitly (precision per rule), and prune rules |
| Google Site Reliability Engineering (SRE) Workbook, "Alerting on SLOs" ([SRE workbook](https://sre.google/workbook/alerting-on-slos/)) | Judge alerts on precision, recall, detection time and reset time. Use multiwindow, multi-burn-rate alerts (note 7) | **SLO burn-rate alerts are the paging layer.** Anomaly output should be tickets, annotations or context unless tied to an SLO |

Notes:

1. Skyline's stated philosophy was to "err on the side of noise". Its author noted in the comments
   that Skyline did not alert because alerting "would be very noisy". A 2015 talk abstract says
   many methods from the literature fail on terabytes of noisy data. It also says v1 hit problems
   that needed a 2.0 redesign ([Clegg 2015](http://www.andrewclegg.org/tech/KaleTalk.html)). The
   repo was archived on 2019-12-18 ([GitHub](https://github.com/etsy/skyline)).
2. Argos uses hourly-updated dynamic thresholds with daily and weekly seasonality. On top it has a
   second-stage outage detector and a System Health Index. Uber reports 9 of 10 pages indicating a
   real outage. It also says it explicitly puts never missing an outage first.
3. The 2 stages: a cheap outlier filter, then an expensive confirmation on the few candidates.
4. On-call pain points were too many alerts and dashboards, and too much tuning. The per-app health
   model combines metrics, deployments, canaries, and upstream/downstream health. It mixes
   statistical, rule-based and machine learning algorithms. Teams opt in to alerts via Slack, email
   or PagerDuty. Each issue gets one notification, with feedback buttons. More than 100 production
   apps use it.
5. The filters: merge consecutive anomalies (maxGap/maxDuration), a duration filter, a
   percentage-change filter, a site-wide-impact filter, and suppression windows for deploys and
   holidays.
6. 4 individual + 2 collective alert anti-patterns (misleading, uninformative, non-actionable
   alerts). The paper proposes Quality of Alert (QoA) = indicativeness, precision, handleability.
7. Burn rate = how fast the service uses up its error budget. The recommended setup:
   - page at 14.4× burn (1 h & 5 m windows, 2% of the budget)
   - page at 6× burn (6 h & 30 m, 5%)
   - open a ticket at 1× (3 d & 6 h, 10%)

**Net result:** none of these operators describes paging directly on unsupervised per-series
anomalies. Two do page from anomaly detection output:

- Uber (Argos), after a second-stage outage detector
- Netflix (Telltale, PagerDuty opt-in), from a per-application health model

So the lesson is "confirm and correlate before paging", not "anomaly detection never pages". Only
Uber quantifies precision (~90%). It came from multi-stage confirmation plus seasonality-aware
baselines. The others rely on health models, correlation, dedupe and SLO gating.

## 6. Method tiers for our stack

This section sorts the methods into **method tiers** 0–3, by cost and evidence. They are not the
detection tiers T0–T3 of the [reference architecture](../reference-architecture.md#scope). The
mapping:

- Method tier 0 = T0: SLO alerts, the only tier that pages.
- Method tier 1 = T1: cheap statistics on all aggregates.
- T2 = a seasonal-median + MAD scorer, which is a method-tier-1 statistic. It runs as a second
  stage on the top-K series (a short list of the most important series). Its upgrade path is
  MSTL (method tier 1), then Matrix Profile or PyOD (method tier 2).
- T3 = LLM summaries, the LLM part of method tier 3. Foundation models are a later option
  (iteration 3 in the [roadmap](../roadmap.md)), outside T0–T3.

**Premise:** detection runs on **pre-aggregated** series, that is 10^3–10^5 series:

- RED metrics per service × endpoint × status
- saturation per node pool
- log template-ID counts per service

It never runs on the raw 1–10 M Prometheus series or on raw log lines.

### Method tier 0 — Rules and SLO alerts

- **Methods:**
  - SLO multiwindow multi-burn-rate alerts
  - hard limits (`predict_linear` for disk/quotas)
  - heartbeat/`absent()`
  - per-service log error-rate thresholds
- **Catches:** user-visible breaches, known failure modes, resource exhaustion.
- **False positives:** lowest. The SRE workbook rates budget-based burn-rate alerts good on both
  precision and recall ([link](https://sre.google/workbook/alerting-on-slos/)). Static thresholds
  on non-SLO metrics drift and get noisy (Netflix, Uber §5).
- **Compute:** ≈0. It is only recording and alerting rules over aggregates.
- **Libraries (licence, latest):** Prometheus/Grafana alerting, Sloth v0.16.0 (Apache-2.0,
  [repo](https://github.com/slok/sloth)), Pyrra v0.10.1 (Apache-2.0,
  [repo](https://github.com/pyrra-dev/pyrra)).
- **Add it:** on day 1. **The only method tier that should page by default.**

### Method tier 1 — Cheap statistics on aggregates

- **Methods:**
  - seasonal robust bands: mean or median ± k·stddev or k·MAD, with 1-day or 1-week offsets
  - EWMA
  - STL/MSTL residual
  - SPOT auto-thresholds on residuals
  - **logs: Drain-style template IDs → per-template rate bands + new-template + error-ratio**
- **Catches:**
  - spikes, drops and level shifts on
    [golden signals](https://sre.google/sre-book/monitoring-distributed-systems/) (latency,
    traffic, errors, saturation)
  - new error templates
  - bursty templates
- **False positives:** moderate to high if paged (Etsy §5). Reduce them with min duration, min %
  change, merging, and gating on SLO or impact movement (ThirdEye, Uber §5). Send the output to
  tickets, Grafana annotations or chat.
- **Compute:** µs per point. 10^4–10^5 aggregate series fit on 1–2 cores (estimate), or run as
  PromQL recording rules.
- **Libraries (licence, latest):** `grafana/promql-anomaly-detection` v0.2.1 (Apache-2.0),
  statsmodels 0.15.0 (BSD-3), river 0.26.1 (BSD-3), libspot 3.1.0 (LGPL-3.0+), Drain3 0.9.11 (MIT,
  last release 2022).
- **Add it:** in iteration 1. Evidence: statistical and simple methods lead TSB-AD-U (§1.1).
  Novelty and count methods capture most public log anomalies (§3).

### Method tier 2 — Machine learning on the top-K series

Top-K series = a short list of the most important series.

- **Methods:**
  - TSB-AD leaders as windowed batch jobs: Sub-PCA, POLY, KShapeAD, Series2Graph (`tsb-ad`)
  - Matrix-Profile discords (stumpy)
  - IForest/PCA on per-service multivariate vectors (PyOD)
  - RCF / Half-Space Trees for streaming multivariate data
- **Catches:**
  - shape anomalies (stuck, saw-tooth, flatline)
  - multivariate combinations (latency ↑ with traffic flat)
  - log count-vector outliers
- **False positives:** the best in class still scores only ≈0.4 VUS-PR on TSB-AD-U, so expect many
  false positives. Use it as a **second-stage ranker or confirmer** of method-tier-1 candidates
  (Uber-style 2 stages), not as a pager.
- **Compute:** ms–s per series per window. 10^2–10^3 top-K series per 5 min on 1 node (estimate).
- **Libraries (licence, latest):** `tsb-ad` 1.5 (Apache-2.0), PyOD 3.6.5 (BSD-2), stumpy 1.14.1
  (BSD-3), river 0.26.1 (BSD-3), AWS RCF 4.4.0 (Apache-2.0, Java/Rust).
- **Add it:** after method tier 1 has produced ~2–3 months of operator feedback and labels.
  Then use them to prove a gain on our own incidents.

### Method tier 3 — Foundation models and LLMs

- **Methods:**
  - forecasting foundation models for intervals, capacity and cold-start series: Chronos-Bolt /
    Chronos-2, Toto 2.0 (22m/313m), TimesFM 2.5
  - small models built for anomaly detection: TSPulse (1M), MOMENT
  - LLMs only outside the code that runs for every line: label new log templates, summarise or
    explain an incident bundle
- **Catches:** forecasts and capacity, series with no history, and semantic grouping and
  explanation of logs. **Not shown to beat method tiers 1 and 2 on anomaly detection
  precision for ops data** (§2.2).
- **False positives:** unproven for anomaly detection. Forecast-residual models miss sustained
  anomalies ([Uray 2026](https://arxiv.org/abs/2607.12454)). They lost to seasonal naive on Huawei
  cloud data ([Toner 2025](https://arxiv.org/abs/2502.12944)). LLMs risk hallucination (§3.3).
- **Compute:** ~0.05–15 GFLOP per series forecast. So one 16-core node handles ~2×10^4 (TimesFM
  2.5) to 10^5–10^6 (Toto-22m, TSPulse) series per 5 min (FLOP estimate, §2.3). LLM cost grows with
  incidents and new templates (10^1–10^3 calls/day), not with log volume.
- **Libraries (licence, latest):**
  - chronos-forecasting 2.3.2 (Apache-2.0)
  - the timesfm 3.0.2 package with the **2.5 weights** (Apache-2.0), not the non-commercial 3.0
    weights
  - Toto 2.0 (Apache-2.0)
  - TSPulse (Apache-2.0)
- **Exclude:** **Moirai (CC-BY-NC-4.0)**, the TimesFM 3.0 weights, and TimeGPT (proprietary).
- **Add it:** in iteration 2 or later, and only in two cases. First, if a back-test on labelled
  incidents shows misses by method tiers 1 and 2 that foundation models catch. Second, for
  capacity forecasting. Add an LLM for explanations once method tier 1 emits structured anomalies.

### Steps for all method tiers 1–3

Anomalies → dedupe/merge → impact or SLO gate → route. Measure precision per rule (the Huawei QoA
idea, [DSN'22](https://arxiv.org/abs/2204.09670)). Evaluate any new method tier on our own incident
log with event-level precision/recall or VUS-PR. Never use point-adjusted F1 (§1.4).

## UNVERIFIED items

- TSB-AD community-leaderboard entries (Time-RCD, TSPulse, CHARM, StreamVAE, MMPAD, xLSTMAD) are
  author-submitted. We found no independent reproduction. Time-RCD MAFT ships only as a compiled
  `.so` in the TSB-AD repo.
- CPU throughput for all foundation models (§2.3) is a FLOP-based **estimate**. No vendor publishes
  x86 CPU numbers. We did not measure it locally (no PyTorch on this machine). The Apple M4 Max GPU
  peak FLOPs used in the sanity check has no source.
- Time-RCD's claimed ICML 2026 acceptance (model card) vs its withdrawn arXiv paper. TimeGPT
  parameter count and pricing (not public).
- RCF per-model memory, SPOT calibration-batch size, and STL/MSTL per-series fit timings.
- Template count for our logs (assumed 10^3–10^5) and average line size (assumed ~300 B).
- Whether rankings from HDFS/BGL/Thunderbird (log anomaly detection) and TSB-AD (metrics) transfer
  to our microservice telemetry. No public benchmark exists at 2 TB/day of mixed logs.
- Not used in tables:
  - GitLab's 2019 PromQL z-score post (403 to fetcher, seen only via search snippet)
  - Zhao et al. ICSE-SEIP'20 alert-storm figures (search snippet only, PDF not read)

## Where the cited papers were published

| Short name | Full name |
|---|---|
| NeurIPS (D&B) | Conference on Neural Information Processing Systems (Datasets and Benchmarks track) |
| PVLDB | Proceedings of the VLDB Endowment (VLDB = Very Large Data Bases) |
| TMLR | Transactions on Machine Learning Research (journal) |
| KDD | ACM SIGKDD Conference on Knowledge Discovery and Data Mining |
| MiLeTS | Workshop on Mining and Learning from Time Series (at KDD) |
| ICML | International Conference on Machine Learning |
| ICLR | International Conference on Learning Representations |
| PMLR | Proceedings of Machine Learning Research (publishes ICML papers) |
| IJCAI | International Joint Conference on Artificial Intelligence |
| EUROCAST | International Conference on Computer Aided Systems Theory |
| ICSE (SEIP) | International Conference on Software Engineering (Software Engineering in Practice track) |
| FSE | ACM International Conference on the Foundations of Software Engineering |
| ASE | IEEE/ACM International Conference on Automated Software Engineering |
| ISSTA | ACM International Symposium on Software Testing and Analysis |
| ISSRE | IEEE International Symposium on Software Reliability Engineering |
| QRS | IEEE International Conference on Software Quality, Reliability and Security |
| EMSE | Empirical Software Engineering (journal) |
| DSN | IEEE/IFIP International Conference on Dependable Systems and Networks |
