# 01 — Log anomaly detection on the free Elastic tier (Basic / OSS / AGPL)

**In short:**

- **Elastic's own "smart" log features are all paid.** Every Elastic feature that finds anomalies or groups log messages needs Platinum or higher. The free Basic license has none of them (§2, [subscriptions](https://www.elastic.co/subscriptions)).
- **Kibana alerts on Basic can only go to an index or to the server log.** Slack, email, webhook, PagerDuty, Teams and Jira connectors need Gold or higher. So does Watcher. Gold is closed to new customers, and self-managed Platinum is for existing customers only. So for a new buyer, the next step after Basic is in practice **Enterprise** (§2).
- **Some useful parts are free:** transforms (per-minute count indices), ES|QL (Elasticsearch Query Language), the threshold-style rule types and the Index connector (§2). Downsampling is also free, but only for metrics in time series data streams (TSDS), not for logs. So log anomaly detection must be an **add-on** outside Kibana.
- **ElastAlert2 is the best free rule engine** (§3). It is Apache-2.0, version 2.31.0 (2026-07-22), and supports Elasticsearch 7/8/9 and OpenSearch 1–3. Its `new_term`, `flatline` and `spike` rules are real anomaly detectors. It runs as a single instance only, with no high availability.
- **[Template mining](https://arxiv.org/abs/1811.03509) turns each log line into a template:** the fixed text, with the variable parts masked (§4). Drain3 (MIT) has had no release since 2022-07-17. The OpenTelemetry Collector now has an alpha `drainprocessor` (since v0.151.0). It is written in Go, runs inline and saves its state. It is the most practical [Drain](https://jiemingzhu.github.io/pub/pjhe_icws2017.pdf) engine for reading logs on each host without changing the core. The LogAI GitHub repo is gone (HTTP 404).
- **Our plan for iteration 1** (§8):
  1. Filebeat gives each line a stateless template key with `replace` + `fingerprint` (both since Filebeat 7.8). An offline Drain3 run on a sample gives the masks.
  2. A Basic **transform** counts lines per `(service, template)` per minute.
  3. ElastAlert2 alerts on new templates, flatlines and spikes, and sends the alerts to Alertmanager.
  4. Grafana alerting on Elasticsearch checks week-over-week ratios.

  Later (iteration 2): a large language model (LLM) reads only the alert bundles.

Terms and abbreviations: [glossary](../glossary.md).

We want our own anomaly detection for logs, built from free parts plus a little code of our own. This file answers two questions. What does the free Elastic tier give us? Which open-source add-ons fill the gaps?

> Status: complete (v1). Research date: 2026-09-14. All fetched links: [LINKS.md](../../LINKS.md) (merged, deduplicated).
> Scope: iteration 1 = add-ons only, on top of Filebeat → Elasticsearch + Kibana (self-managed, free). We do not replace any core part (no "core swap").
> Scale: 200 GB – 2 TB/day of logs. Machine learning (ML) on raw data is not practical at this scale, so we pre-aggregate.
> Current Elastic releases ([GitHub API](https://api.github.com/repos/elastic/elasticsearch/releases)): **9.5.3** (2026-09-03), 9.4.6 (2026-09-01), **8.19.21** (2026-09-01).

**Sections:**

1. [Which Elastic builds and licenses exist today?](#1-which-elastic-builds-and-licenses-exist-today)
2. [Which features does each license include?](#2-which-features-does-each-license-include)
3. [ElastAlert2: the free rule engine](#3-elastalert2-the-free-rule-engine)
4. [Log template mining](#4-log-template-mining)
5. [Where can we add a pipeline without replacing the core?](#5-where-can-we-add-a-pipeline-without-replacing-the-core)
6. [Other free options on Elasticsearch data](#6-other-free-options-on-elasticsearch-data)
7. [OpenSearch Anomaly Detection: only if we ever move](#7-opensearch-anomaly-detection-only-if-we-ever-move)
8. [Our recommendation](#8-our-recommendation)

---

## 1. Which Elastic builds and licenses exist today?

Elastic ships three kinds of build:

- the default distribution
- an open-source software (OSS) build
- a source option under the GNU Affero General Public License v3 (AGPLv3).

*x-pack* is the part of the code that is only under the Elastic License 2.0 (ELv2). It holds the Basic and the paid features.

| Build | License of the binary | What it includes | Status |
|---|---|---|---|
| **Default distribution** (note 1) | **ELv2** ([licensing FAQ](https://www.elastic.co/pricing/faq/licensing)) | All x-pack code. Runs as **Basic** by default (note 2). | Current (9.5.x, 8.19.x) |
| **OSS-only build** (`elasticsearch-oss`, `kibana-oss`) | Apache-2.0 | No x-pack (note 3) | Last release **7.10.2**. End of life (EOL), no security patches (note 4). |
| **AGPLv3 option** (added Sept 2024, from 8.16) | Source code only. Releases stay under ELv2 (note 5). | Free parts of the source only, so **no x-pack** (note 6) | For us: about zero, unless we build from source |

Notes:

1. The default distribution is what you get from `elastic.co/downloads`, from the Docker image `docker.elastic.co/elasticsearch/elasticsearch`, and from apt/yum.
2. It runs with "a Basic license that never expires" ([manage license](https://www.elastic.co/docs/deploy-manage/license/manage-your-license-in-self-managed-cluster)). Paid features unlock when you install a license or start a 30-day trial. The trial works once per major version.
3. Without x-pack there is no security, no index lifecycle management (ILM), no transforms and no Kibana alerting. There is also no ES|QL, no Lens and no data streams. See the OSS column on [subscriptions](https://www.elastic.co/subscriptions).
4. Elastic states: "we no longer produce an Apache 2.0 distribution" ([licensing FAQ](https://www.elastic.co/pricing/faq/licensing)).
5. Elastic: "Our releases will continue to be under the Elastic License" ([licensing FAQ](https://www.elastic.co/pricing/faq/licensing)).
6. The AGPL option applies to "the free portions of the source code". The repo LICENSE says that by default the code has three licenses: AGPLv3, the Server Side Public License (SSPL) and ELv2. It also says: "Code that is licensed solely under the Elastic License 2.0 is found only in the x-pack folder" ([LICENSE.txt](https://raw.githubusercontent.com/elastic/elasticsearch/main/LICENSE.txt)). So a binary you build yourself under AGPL only has **no x-pack**, and so none of the Basic features either.

**What this means for us:** "self-managed, probably free" means the default distribution with the Basic license. The Basic features are under ELv2 and are free to use inside the company. SSPL and AGPL matter only if you redistribute Elasticsearch or offer it as a service.

### How to check which build and license you run

```http
GET /
```
On the default distribution (any 7.x–9.x) this returns `version.build_flavor: "default"`. The API docs example shows `"number": "9.1.0", "build_flavor": "default", "build_type": "docker"` ([GET / API](https://www.elastic.co/docs/api/doc/elasticsearch/operation/operation-info)).

- OSS 7.x builds report `build_flavor: "oss"`. The enum in [7.10 Build.java](https://raw.githubusercontent.com/elastic/elasticsearch/7.10/server/src/main/java/org/elasticsearch/Build.java) is `DEFAULT("default"), OSS("oss"), UNKNOWN("unknown")`.
- In 8.x and later (main), the flavor is hard-coded to `"default"` ([main Build.java](https://raw.githubusercontent.com/elastic/elasticsearch/main/server/src/main/java/org/elasticsearch/Build.java)). The value `serverless` appears only on Elastic Cloud Serverless.

```http
GET _license
```
On the free default distribution this returns `license.type: "basic"` and `license.status: "active"`. There is no `expiry_date`, because Basic never expires. From the [GET _license API](https://www.elastic.co/docs/api/doc/elasticsearch/operation/operation-license-get) docs:

- Values of `type`: `missing, trial, basic, standard, dev, silver, gold, platinum, enterprise`.
- Example response fields: `status, uid, type, issue_date, max_nodes: 1000, issued_to, issuer: "elasticsearch", start_date_in_millis: -1`.

An OSS 7.10 build has no `_license` endpoint (no x-pack), so it returns an error. *(Exact OSS error text UNVERIFIED.)*

```http
GET _xpack?categories=features
```
This returns `available` and `enabled` flags for each feature (`ml`, `watcher`, `transform`, `esql`, `rollup` …) ([GET _xpack API](https://www.elastic.co/docs/api/doc/elasticsearch/operation/operation-xpack-info)). On Basic, expect:

- `ml.available: false`
- `watcher.available: false`
- `transform.available: true`
- `esql.available: true`

We confirmed this exact output on a local Elasticsearch 9.5.3 container with the default Basic license (2026-09-14). `rollup` and `logsdb` were also available.

In Kibana, Stack Management → License Management shows "Your Basic license is active" *(UI text UNVERIFIED)*.

---

## 2. Which features does each license include?

This table lists the features that matter for anomaly detection, on self-managed clusters.

- ✅ = included, ❌ = not included. "GA" = generally available.
- The columns come from [elastic.co/subscriptions](https://www.elastic.co/subscriptions). We parsed the page's raw data on 2026-09-14. "fn N" means footnote N on that page.
- Gold is discontinued for new customers (fn 9). **Platinum is for existing customers only (fn 18).**
- For the OSS column, see note 1.
- AIOps (AI for IT operations) is the name of Elastic's AIOps Labs tools in Kibana.
- Query DSL is Elasticsearch's JSON query language (DSL = domain-specific language).

| Feature | OSS (note 1) | **Basic** | Gold | Platinum | Enterprise | GA / version notes | Source |
|---|---|---|---|---|---|---|---|
| ML anomaly detection jobs (single/multi-metric, population, rare, forecasting) | ❌ | ❌ | ❌ | ✅ | ✅ | GA | [subscriptions → Anomaly detection](https://www.elastic.co/subscriptions), [anomaly detection docs](https://www.elastic.co/docs/explore-analyze/machine-learning/anomaly-detection) |
| ML **log message categorization** (anomaly detection jobs with `mlcategory`) | ❌ | ❌ | ❌ | ✅ | ✅ | GA | [subscriptions](https://www.elastic.co/subscriptions) |
| Anomaly detection **alert rule types** (ML) | ❌ | ❌ | ❌ | ✅ | ✅ | GA (note 2) | [subscriptions](https://www.elastic.co/subscriptions), [anomaly detection rule](https://www.elastic.co/docs/solutions/observability/incident-management/create-an-anomaly-detection-rule) |
| AIOps: **Explain log rate spikes / Log rate analysis** | ❌ | ❌ | ❌ | ✅ | ✅ | GA (AIOps Labs) | [subscriptions → AIOps](https://www.elastic.co/subscriptions), [AIOps Labs](https://www.elastic.co/docs/explore-analyze/machine-learning/machine-learning-in-kibana/xpack-ml-aiops) |
| AIOps: **Log pattern analysis** (note 3) | ❌ | ❌ | ❌ | ✅ | ✅ | GA. Gating of the Discover "Patterns" entry point: UNVERIFIED | same |
| AIOps: **Change point detection** (Kibana UI) | ❌ | ❌ | ❌ | ✅ | ✅ | GA | same |
| ES\|QL **`CATEGORIZE`** (in `STATS … BY`) | ❌ | ❌ | ❌ | ✅ | ✅ | Preview 9.0, **GA 9.1** (note 4) | [CATEGORIZE](https://www.elastic.co/docs/reference/query-languages/esql/functions-operators/grouping-functions/categorize) |
| ES\|QL **`CHANGE_POINT`** command | ❌ | ❌ | ❌ | ✅ | ✅ | Preview 9.1, **GA 9.2** (note 5) | [CHANGE_POINT](https://www.elastic.co/docs/reference/query-languages/esql/commands/change-point), [Discover change points](https://www.elastic.co/docs/explore-analyze/discover/detect-change-points) |
| `categorize_text` aggregation | ❌ | ❌ | ❌ | ✅ | ✅ | Algorithm rewritten in 8.3 (note 6) | [subscriptions](https://www.elastic.co/subscriptions), [aggregation docs](https://www.elastic.co/docs/reference/aggregations/search-aggregations-bucket-categorize-text-aggregation) |
| `change_point` **aggregation** (Query DSL) and three related aggregations (note 7) | ❌ | ❌ | ❌ | ✅ | ✅ | Preview 9.0–9.1, GA 9.2 | [change_point agg](https://www.elastic.co/docs/reference/aggregations/search-aggregations-change-point-aggregation), [MachineLearning.java @v9.5.3](https://raw.githubusercontent.com/elastic/elasticsearch/v9.5.3/x-pack/plugin/ml/src/main/java/org/elasticsearch/xpack/ml/MachineLearning.java) |
| Data frame analytics (outlier detection and more) | ❌ | ❌ | ❌ | ✅ | ✅ | GA | [subscriptions](https://www.elastic.co/subscriptions) |
| ES\|QL (the language itself) | ❌ | ✅ | ✅ | ✅ | ✅ | GA (exact version not re-checked here). Lookup joins are also Basic. | [subscriptions](https://www.elastic.co/subscriptions), [ES\|QL ref](https://www.elastic.co/docs/reference/query-languages/esql) |
| Kibana alerting framework ("Kibana alerting and in-stack actions") | ❌ | ✅ | ✅ | ✅ | ✅ | GA | [subscriptions](https://www.elastic.co/subscriptions) |
| Rule: **Elasticsearch query** (KQL, Query DSL or **ES\|QL**) (note 8) | ❌ | ✅ | ✅ | ✅ | ✅ | ES\|QL "time field + group alerts" GA 9.2 | [Elasticsearch query rule](https://www.elastic.co/docs/explore-analyze/alerting/alerts/rule-type-es-query) |
| Rule: **Index threshold** (count/avg/sum/min/max, grouped) | ❌ | ✅ | ✅ | ✅ | ✅ | GA | [rule types](https://www.elastic.co/docs/explore-analyze/alerting/alerts/rule-types) |
| Rule: **Log threshold** (count per group-by, **ratio** A/B) | ❌ | ✅ (note 9) | ✅ | ✅ | ✅ | GA in the stack. Not available on Serverless. | [log threshold](https://www.elastic.co/docs/solutions/observability/incident-management/create-log-threshold-rule) |
| Rule: **Custom threshold** (Observability: ratios, multiple aggregations) | ❌ | ✅ (note 10) | ✅ | ✅ | ✅ | GA | [custom threshold](https://www.elastic.co/docs/solutions/observability/incident-management/create-custom-threshold-rule) |
| Rule: Transform health ("Operational rule type for transforms") | ❌ | ✅ | ✅ | ✅ | ✅ | GA | [subscriptions](https://www.elastic.co/subscriptions) |
| Alert noise reduction (snooze, mute, dedupe) | ❌ | ✅ | ✅ | ✅ | ✅ | – | [subscriptions](https://www.elastic.co/subscriptions) |
| Maintenance windows | ❌ | ❌ | ❌ | ✅ | ✅ | – | [subscriptions](https://www.elastic.co/subscriptions) |
| Service Level Objectives (SLOs) and [burn-rate](https://sre.google/workbook/alerting-on-slos/) rules | ❌ | ❌ | ❌ | ✅ | ✅ | – | [subscriptions](https://www.elastic.co/subscriptions) |
| Connectors: **Index, Server log** (note 11) | ❌ | ✅ | ✅ | ✅ | ✅ | `minimumLicenseRequired: 'basic'` in Kibana 9.5.3 source | [subscriptions](https://www.elastic.co/subscriptions), [connectors](https://www.elastic.co/docs/reference/kibana/connectors-kibana), [es_index](https://raw.githubusercontent.com/elastic/kibana/v9.5.3/x-pack/platform/plugins/shared/stack_connectors/server/connector_types/es_index/index.ts), [server_log](https://raw.githubusercontent.com/elastic/kibana/v9.5.3/x-pack/platform/plugins/shared/stack_connectors/server/connector_types/server_log/index.ts) |
| Connector: **Cases** (a rule action that opens or updates a case) | ❌ | ❌ | ❌ | ✅ | ✅ | `minimumLicenseRequired: 'platinum'` (system action). The Case Management UI is Basic. | [cases connector source](https://raw.githubusercontent.com/elastic/kibana/v9.5.3/x-pack/platform/plugins/shared/cases/server/connectors/cases/index.ts), [subscriptions → Case Management](https://www.elastic.co/subscriptions) |
| Connectors: **email, webhook, Slack, PagerDuty** and 13 more (note 12) | ❌ | ❌ | ✅ | ✅ | ✅ | "Kibana third-party alerting actions" first appear in the Gold card | [subscriptions](https://www.elastic.co/subscriptions) |
| Connectors: GenAI (OpenAI, Bedrock, Gemini, AI Connector) | ❌ | ❌ | ❌ | ❌ | ✅ | – | [subscriptions](https://www.elastic.co/subscriptions) |
| **Watcher** | ❌ | ❌ | ✅ | ✅ | ✅ | – | [subscriptions](https://www.elastic.co/subscriptions) |
| **Transforms** (pivot / latest, continuous) | ❌ | ✅ | ✅ | ✅ | ✅ | Default `max_page_search_size` 500 (note 13) | [subscriptions](https://www.elastic.co/subscriptions), [transform limits](https://www.elastic.co/docs/explore-analyze/transforms/transform-limitations) |
| **Rollups** (legacy) | ❌ | ✅ | ✅ | ✅ | ✅ | Legacy. Elastic points users to downsampling. | [subscriptions](https://www.elastic.co/subscriptions), [rollup→downsampling](https://www.elastic.co/docs/manage-data/lifecycle/rollup/migrating-from-rollup-to-downsampling) |
| **Downsampling**: only for metrics in time series data streams (TSDS), not free-text logs | ❌ / ✅ (note 14) | ✅ | ✅ | ✅ | ✅ | Works with ILM and data stream lifecycle (DSL) | [subscriptions](https://www.elastic.co/subscriptions), [downsampling](https://www.elastic.co/docs/manage-data/data-store/data-streams/downsampling-concepts) |
| Logsdb index mode | ✅ | ✅ | ✅ | ✅ | ✅ | Synthetic `_source` and pattern-based compression are Enterprise (note 15) | [subscriptions](https://www.elastic.co/subscriptions) |
| Significant terms p-value, random sampler, rate, t-test, top_metrics aggregations | ❌ | ✅ | ✅ | ✅ | ✅ | Building blocks for our own "log rate analysis" | [subscriptions](https://www.elastic.co/subscriptions) |
| AI Assistant / Agent Builder | ❌ | ❌ | ❌ | ❌ | ✅ | – | [subscriptions](https://www.elastic.co/subscriptions) |

Notes:

1. The page's "Open Source" column describes today's code outside x-pack. In places it contradicts itself: for example, ES|QL is ✅ in one section and ❌ in another. For a legacy **7.10 OSS** build, every row here is ❌. ES|QL, transforms and Kibana alerting are x-pack, or did not exist in 7.10.
2. The page says: "Alerting rules based on anomaly detection or SLOs are only available on Platinum and Enterprise tiers" (fn 5).
3. Log pattern analysis "uses the same algorithms as a machine learning categorization job".
4. The docs say `CATEGORIZE` "requires a platinum license". Named options are GA in 9.2: `analyzer`, `output_format` and `similarity_threshold` (default 70). `CATEGORIZE` must be the first grouping, and you can use it only once.
5. The docs say `CHANGE_POINT` "requires a platinum license".
   - The `BY` grouping form has its own **9.5+** badge. The Discover integration is also GA in 9.5.
   - It needs at least 22 values. It ignores values after the first 1,000 (per group).
   - Change types: dip, spike, step_change, distribution_change, trend_change.
6. The page row is "Text categorization aggregation". The aggregation uses only the first 100 tokens. The docs warn about circuit-breaker errors, so run it under a sampler.
7. The three related aggregations are `frequent_item_sets`, `bucket_correlation` and `bucket_count_ks_test`. `change_point` needs at least 22 buckets. These aggregations have no row on the subscriptions page. But the Elasticsearch source registers `CHANGE_POINT_AGG_FEATURE`, `CATEGORIZE_TEXT_AGG_FEATURE`, `FREQUENT_ITEM_SETS_AGG_FEATURE` … with `License.OperationMode.PLATINUM`.
8. The page calls this rule "Search threshold rule types for Discover". KQL is the Kibana Query Language. The rule also has a "Create an alert for each row" option.
9. From the Logs row "Kibana alerting and actions".
10. Custom threshold has no row on the subscriptions page. But the [Kibana 9.5.3 source](https://raw.githubusercontent.com/elastic/kibana/v9.5.3/x-pack/solutions/observability/plugins/observability/server/lib/rules/custom_threshold/register_custom_threshold_rule_type.ts) sets `minimumLicenseRequired: 'basic'`. The Elasticsearch query, index threshold and log threshold rules have the same setting.
11. The page row: "Elastic Connectors (e.g., Server Log and Index)".
12. The full list: email, webhook, Slack, PagerDuty, Microsoft Teams, Opsgenie, Jira, Jira Service Management (JSM), ServiceNow, xMatters, TheHive, Tines, Torq, Swimlane, D3, Resilient, XSOAR.
13. Continuous transforms use `sync.time.delay`. The destination cannot be a data stream.
14. The OSS column shows ❌ for downsampling but ✅ in the TSDB (time series database) row.
15. Synthetic `_source` is Enterprise from 8.17 (fn 15). "Pattern-based compression for log messages" is also Enterprise.

**Limits we found**

- **`CATEGORIZE` and `CHANGE_POINT` do not work in an ES|QL Elasticsearch query rule on Basic.** The query itself fails the license check. We tested this on a local Elasticsearch 9.5.3 Basic container, through `_query` and not through a Kibana rule. The error is `verification_exception` "current license is non-compliant for [CATEGORIZE(message)]". `CHANGE_POINT` gives the same error. A `categorize_text` aggregation returns HTTP 403 `security_exception`.
- **Do not trust summaries of the subscriptions page.** An LLM summary of the page (made with WebFetch) said that ML anomaly detection and AIOps were free. That is **wrong**. Always read the raw matrix.
- **Kibana alerting on Basic can still reach the outside world.** The **Index connector** writes alerts to an index. An external relay reads that index and forwards the alerts: ElastAlert2, a small script, or Grafana alerting. This is the standard workaround for the missing Slack and webhook connectors on Basic.

---

## 3. ElastAlert2: the free rule engine

Repo: `jertel/elastalert2`.

- **License:** Apache-2.0 ([GitHub API](https://api.github.com/repos/jertel/elastalert2), [README](https://raw.githubusercontent.com/jertel/elastalert2/master/README.md)).
- **What is free:** everything. ElastAlert2 is a standalone Python daemon. It queries Elasticsearch and keeps its state in its own `elastalert_*` writeback indices. It ships **about 45 alerters** ([alerts.rst](https://raw.githubusercontent.com/jertel/elastalert2/master/docs/source/alerts.rst)), for example:
  - Alertmanager, Slack, PagerDuty, OpsGenie
  - Microsoft Power Automate / Teams-style, Email, HTTP POST / POST 2 (webhook)
  - Jira, ServiceNow, Telegram, Mattermost, Zabbix, "Indexer"

  So it fills exactly the **connector gap of Basic**. In Kibana, Slack, webhook and PagerDuty need Gold or higher.
- **Detection method:** rules over time windows, no machine learning. Rule types ([ruletypes.rst](https://raw.githubusercontent.com/jertel/elastalert2/master/docs/source/ruletypes.rst)):

| Rule type | What it does | Does it find anomalies? |
|---|---|---|
| `spike` | Count in current `timeframe` is `spike_height`× above/below the previous one, per `query_key` (note 1) | **Yes**: detects relative change, the closest to anomaly detection. No seasonality. |
| `spike_aggregation` | The same two-window ratio, over a metric aggregation (note 2) | **Yes** (for example, a spike in a p95 latency field, the 95th percentile) |
| `flatline` | Fewer than `threshold` events in `timeframe`, per `query_key` | **Yes**: "service went silent", missing logs. High value. |
| `new_term` | A value not seen in the field during the last `terms_window_size` (note 3) | **Yes**: novelty. Excellent on a *template-ID* field (a new error template appears). |
| `cardinality` | Unique values of a field above `max_cardinality` or below `min_cardinality` in `timeframe` | Partly (for example, the number of distinct error templates per service) |
| `percentage_match` | Share (%) of documents matching `match_bucket_filter` is above or below a bound, per `query_key` | Partly: an error-ratio alert on a service level indicator (SLI). Static bounds. |
| `frequency` | ≥ `num_events` in `timeframe` | No: static threshold |
| `metric_aggregation` | Metric aggregation above or below a static threshold | No: static |
| `any`, `blacklist`, `whitelist`, `change` | Match per document, or a field changes | No |

Notes:

1. `spike_type` is up, down or both. `threshold_ref` and `threshold_cur` set minimum counts.
2. Supported aggregations: `min/max/avg/sum/cardinality/value_count/percentiles`.
3. The default is 30 d, queried in `window_step_size` steps of 1 d.

**Does it scale to 200 GB–2 TB/day?** Yes, but **only if every rule uses aggregations**.

- By default ElastAlert "will download every document in full before processing". It queries overlapping `buffer_time` windows (default 45 min) every `run_every` (default 5 min) ([faq.rst](https://raw.githubusercontent.com/jertel/elastalert2/master/docs/source/recipes/faq.rst), [running_elastalert.rst](https://raw.githubusercontent.com/jertel/elastalert2/master/docs/source/running_elastalert.rst)).
- So each rule must use one of two options ([ruletypes.rst](https://raw.githubusercontent.com/jertel/elastalert2/master/docs/source/ruletypes.rst)):
  - `use_count_query: true` uses the count API. It cannot be combined with `query_key`.
  - `use_terms_query: true` uses a terms aggregation with a single `query_key`. **`terms_size` defaults to 50**, and it silently drops the long tail.
- The same `terms_size` default of 50 also limits the `query_key` terms aggregation of the aggregation rules: `spike_aggregation`, `percentage_match` and `metric_aggregation`. The code is `get_hits_aggregation`, which uses `rule.get('terms_size', 50)` ([elastalert.py @2.31.0](https://raw.githubusercontent.com/jertel/elastalert2/2.31.0/elastalert/elastalert.py)).
- It does **not** limit the start-up baseline of `new_term`, which uses size 2147483647 ([ruletypes.py @2.31.0](https://raw.githubusercontent.com/jertel/elastalert2/2.31.0/elastalert/ruletypes.py)).
- At start-up, `new_term` runs 30 daily aggregation queries per field. This is expensive on multi-TB indices. Point it at a small pre-aggregated index instead.
- `max_threads` limits how many rules run at the same time. The docs warn that raising it "could overload the Elasticsearch cluster" ([configuration.rst](https://raw.githubusercontent.com/jertel/elastalert2/master/docs/source/configuration.rst)).
- Rough load: N rules × (1 aggregation query per `run_every`). So 200 rules every minute ≈ 3–4 aggregation queries/s. This is fine if they read a **rollup or transform index**, or narrow time-bounded data-stream backing indices. For dated indices, use `use_strftime_index`. *(The load estimate is our own arithmetic, not a vendor figure.)*

**Can we add it without replacing anything?** Yes.

- It reads the existing Elasticsearch. It writes its state to `elastalert_status*` indices in the same cluster (set with `writeback_index`).
- The repo has a Docker image and a Helm chart ([README](https://raw.githubusercontent.com/jertel/elastalert2/master/README.md)).
- It can send alerts directly to **Prometheus Alertmanager**. So logs and metrics share one place for routing and deduplication.
- With `--prometheus_port` it exposes Prometheus metrics: `elastalert_hits`, `elastalert_matches`, `elastalert_errors`, `elastalert_alerts_sent` … ([elastalert.py](https://raw.githubusercontent.com/jertel/elastalert2/master/elastalert/elastalert.py), [prometheus_wrapper.py](https://raw.githubusercontent.com/jertel/elastalert2/master/elastalert/prometheus_wrapper.py)).

**Which Elasticsearch versions?**

- The requirements list "Elasticsearch 7, 8, or 9 or OpenSearch 1, 2, or 3" ([running_elastalert.rst @2.31.0](https://raw.githubusercontent.com/jertel/elastalert2/2.31.0/docs/source/running_elastalert.rst)). The README also names **OpenSearch** ("data in Elasticsearch and OpenSearch").
- Elasticsearch 8 is supported "but requires new indices to be created". Drop the old writeback indices before you upgrade. The FAQ warns that if you do not, you can get "a non-working Elasticsearch cluster".
- Elasticsearch 9 is supported: "No manual ElastAlert 2 steps are required". The cluster must upgrade from 8.18 to 9.x.
- The `ES_VERSION` environment variable overrides version detection, for example for OpenSearch in compatible mode ([faq.rst](https://raw.githubusercontent.com/jertel/elastalert2/master/docs/source/recipes/faq.rst)).

**Operations effort:** low to medium. It is one almost stateless container plus YAML rules in Git. It has no UI and no REST API (the FAQ answer is "No plan"). It listens on no port, except the optional Prometheus port ([faq.rst](https://raw.githubusercontent.com/jertel/elastalert2/master/docs/source/recipes/faq.rst)).

**Maturity** ([releases API](https://api.github.com/repos/jertel/elastalert2/releases), [PyPI](https://pypi.org/pypi/elastalert2/json), [commits](https://api.github.com/repos/jertel/elastalert2/commits)):

- Latest release: **2.31.0 (2026-07-22)**. Releases come about every 2 months: 2.30.0 on 2026-05-27, 2.29.0 on 2026-03-21, 2.28.0 on 2026-01-11.
- Last push: 2026-09-03. Unreleased changes are under `2.TBD.TBD` in the master [CHANGELOG](https://raw.githubusercontent.com/jertel/elastalert2/master/CHANGELOG.md).
- 1,132 stars, 3 open issues.
- The PyPI package `elastalert2` 2.31.0 has `requires_python >=3.12`.

**Limits:**

- **No high availability:** "a single instance is all that is supported, with no concept of coordination between instances". With `replicaCount: 2`, every alert is sent twice ([discussion #451](https://github.com/jertel/elastalert2/discussions/451)).
  - The only safe way to run several instances: split the rules across instances, each with its own `writeback_index` ([discussion #938](https://github.com/jertel/elastalert2/discussions/938)).
  - Run 1 replica, and let Kubernetes or systemd restart it. Add a **dead-man's switch** rule: a heartbeat sent to Alertmanager or a health-check service. If the heartbeat stops, ElastAlert2 is down ([discussion #544](https://github.com/jertel/elastalert2/discussions/544), [#865](https://github.com/jertel/elastalert2/discussions/865)).
- **Old client.** It pins the **`elasticsearch==7.10.1` Python client** ([requirements.txt](https://raw.githubusercontent.com/jertel/elastalert2/master/requirements.txt)). It talks to 8.x and 9.x through that client, and the FAQ says this works.
  - ES|QL support arrived in **2.31.0** as an `esql:` filter item, but only in part. It "Cannot be used with aggregation rule types". It also does not work with `percentage_match`, `use_count_query`, scrolling or OpenSearch ([CHANGELOG 2.31.0](https://raw.githubusercontent.com/jertel/elastalert2/2.31.0/CHANGELOG.md), [writing_filters.rst](https://raw.githubusercontent.com/jertel/elastalert2/2.31.0/docs/source/recipes/writing_filters.rst)).
  - It does not use point in time (PIT), the Elasticsearch feature for paging through a fixed view of an index.
- **`spike` has no seasonality.** It compares only with the window just before. So it is noisy when day turns into night and back. To reduce this, use `threshold_ref` or a longer `timeframe`. Or run it on a *deseasonalized* metric (daily pattern removed) from the template-count pipeline.
- **Rare keys get lost.** `use_terms_query` with `terms_size: 50` drops rare keys. Aggregation rules with `query_key` do the same (see above).
- **`new_term` and `.keyword`.** `new_term` on analyzed (full-text) fields gives false positives unless it uses `.keyword`. `use_keyword_postfix` is true by default.
  - But ElastAlert2 always appends the postfix ([util.py @2.31.0](https://raw.githubusercontent.com/jertel/elastalert2/2.31.0/elastalert/util.py)).
  - Filebeat's template maps Elastic Common Schema (ECS) fields such as `service.name` and `error.type` as plain `keyword`, with no `.keyword` sub-field. We checked this with `filebeat export template` 9.5.3.
  - So on those fields set `use_keyword_postfix: false`. Otherwise the baseline comes back empty.
- **`realert` works per rule** unless `query_key` is set. The docs: "All matches for a given rule, or for matches with the same query_key, will be ignored" ([ruletypes.rst](https://raw.githubusercontent.com/jertel/elastalert2/2.31.0/docs/source/ruletypes.rst)). So a `new_term` rule without `query_key` mutes every other new term for the `realert` period.
- A blacklist with more than 1024 entries fails, because of the Elasticsearch `max_clause_count` limit ([faq.rst](https://raw.githubusercontent.com/jertel/elastalert2/master/docs/source/recipes/faq.rst)).
- By default a rule disables itself on error (`disable_rules_on_error: true`). Monitor `elastalert_errors`.

## 4. Log template mining

**Goal:** turn the free-text `message` into a key with few distinct values (low cardinality): `template_id`. A template is the fixed text of a log line, with the variable parts (numbers, IDs) masked. Then "log anomaly detection" becomes two simpler tasks:

- anomaly detection on a metric: **`count{service, template_id}` per minute**
- **novelty detection**: a new template appears.

**Volume:** 2 TB/day ≈ **23 MB/s** on average. At 300–1,000 B/line, that is **about 23k–77k lines/s** on average. For 200 GB/day it is about 2.3k–7.7k lines/s. This is our arithmetic. Peaks are typically 2–3× higher (UNVERIFIED for our stack).

### 4.1 How good are log parsers? (Loghub-2.0 benchmark, ISSTA 2024)

Loghub-2.0 is a benchmark of 14 log datasets with annotated lines ([Loghub-2.0 README](https://raw.githubusercontent.com/logpai/loghub-2.0/main/README.md)). The biggest is Thunderbird, with up to 16.6 M annotated lines. Spark has 16.1 M and HDFS 11.2 M. The paper is Jiang et al., "A Large-scale Evaluation for Log Parsing Techniques: How Far are We?" ([arXiv 2308.10828](https://arxiv.org/abs/2308.10828)). It appeared at ISSTA 2024, a software testing conference.

The table shows averages on the full Loghub-2.0 datasets ([effectiveness CSV](https://raw.githubusercontent.com/logpai/loghub-2.0/main/RQs_experiments/RQ2/effectiveness_results.csv), [efficiency CSV](https://raw.githubusercontent.com/logpai/loghub-2.0/main/RQs_experiments/RQ2/efficiency_results.csv)). In the published CSVs, this is the value after "/". The value before "/" is from the old Loghub-2k (2,000 annotated lines per system).

The accuracy metrics come from the paper. Higher is better. Here GA does **not** mean "generally available".

- **GA** (Group Accuracy): share of log lines put in the correct group.
- **PA** (Parsing Accuracy): share of log lines whose fixed parts and variables are all correct.
- **FGA** and **FTA**: [F1 scores](https://en.wikipedia.org/wiki/F-score) of Group Accuracy and of Template Accuracy. They count per template, not per line.

| Parser | GA | FGA | PA | FTA | Avg parse time (s) | Notes |
|---|---|---|---|---|---|---|
| **Drain** | **0.84** | 0.55 | 0.47 | 0.28 | **425** | Best accuracy/speed balance. One of few statistical parsers that finished all 14 datasets. |
| AEL | 0.86 | 0.56 | 0.44 | 0.25 | 1,794 | Timed out on Spark (-1) |
| IPLoM | 0.79 | 0.61 | 0.19 | 0.12 | 342 | Fast, poor template accuracy |
| Spell | 0.73 | 0.35 | 0.24 | 0.09 | 780 | -1 on several datasets |
| UniParser (GPU, semantic) | 0.66 | 0.50 | 0.68 | 0.26 | 2,255 | Needs a GPU |
| LogPPT (GPU, semantic) | 0.56 | 0.59 | **0.76** | **0.49** | 4,110 | Needs a GPU |

**Drain speed.** We divided the times in the efficiency CSV by the dataset sizes (our arithmetic). The result for Drain on four datasets, in the single-threaded Python `logparser` implementation:

- HDFS ≈ 9.9k lines/s
- Spark ≈ 10.1k lines/s
- BGL ≈ 9.3k lines/s
- Thunderbird ≈ 6.7k lines/s

So Python Drain at 2 TB/day needs **about 3–12 cores on average**, split by service (one tree per shard).

**What we learn:**

- Grouping accuracy (GA ≈ 0.8) is usable. But the template *text* is poor for every parser (FTA ≤ 0.3), except the GPU-based LogPPT (0.49). This does not matter for counting. It matters for LLM summaries.
- Loghub-2k numbers overstate accuracy. Most READMEs quote those numbers.
- Brain is **not** in the Loghub-2.0 CSV. It publishes only Loghub-2k results: F1 ≈ 0.99+, accuracy 0.94–1.0 ([Brain README](https://raw.githubusercontent.com/logpai/logparser/main/logparser/Brain/README.md)).

### 4.2 Template mining tools

#### Drain3 (logpai/Drain3, ex-IBM)

Drain3 is a streaming version of the [Drain](https://jiemingzhu.github.io/pub/pjhe_icws2017.pdf) algorithm. Drain puts log lines into groups with a parse tree of fixed depth.

- **License:** MIT ([PyPI](https://pypi.org/pypi/drain3/json)).
- **What is free:** everything ([README](https://raw.githubusercontent.com/logpai/Drain3/master/README.md)):
  - streaming Drain with masking (regex → `<IP>`, `<NUM>`…)
  - state saved to Kafka, Redis or a file
  - a `max_clusters` cap with least-recently-used (LRU) eviction
  - a fast `match()` against learned clusters only
  - parameter extraction
- **Detection method:** none. It produces a `cluster_id` and a template. Anomaly detection runs later, on the counts per template.
- **Scale:** single-threaded Python, about 7–10k lines/s per core. This is the Drain figure from Loghub-2.0 (see 4.1). Drain3 publishes no throughput figure. At 2 TB/day, either:
  - split the work by service or by consumer group, or
  - use it only for **template discovery on a 1–5% sample**. Then turn the results into regex masks or seed templates for a faster engine.
- **Integration:** a Python consumer that reads from Kafka, Redis or Elasticsearch (with PIT). It saves state snapshots to Redis or Kafka.
- **Operations effort:** medium. We would own a stateful service and the mapping of services to shards.
- **Maturity:** **stale** ([PyPI](https://pypi.org/pypi/drain3/json), [commits feed](https://github.com/logpai/Drain3/commits/master.atom), [releases/latest](https://github.com/logpai/Drain3/releases/latest)). The last PyPI release is **0.9.11 on 2022-07-17**. The last commit is from 2025-02-04. After the move to `logpai`, the README asks for "more contributors and maintainers".
- **Limits:**
  - Template IDs belong to one process. They change after a restart unless the state is saved. They also differ between shards.
  - Trees drift when message formats change.
  - `sim_th` and `depth` need tuning for each log family.
  - No multi-line handling. Do that in Filebeat.

#### OpenTelemetry Collector `drainprocessor` (contrib): new, and the most relevant engine

- **License:** Apache-2.0, as part of OpenTelemetry (OTel) contrib.
- **What is free:** everything ([README](https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/main/processor/drainprocessor/README.md)):
  - It adds `log.record.template` to each log record. Optional named parameters come from `masking_rules`.
  - `max_clusters` with LRU eviction
  - `seed_templates` / `seed_logs` and `warmup_min_clusters`
  - snapshots saved through a storage extension
  - its own metrics: `otelcol_processor_drain_clusters_active`, `…_log_records_annotated`
- **Detection method:** none. Pair it with the **`count` connector** (alpha), which counts logs grouped by attributes ([count connector](https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/main/connector/countconnector/README.md)). Export `log_template_lines_total{service, template_id}` and `log_lines_total{service}` as metrics to Prometheus.
- **Scale:** written in Go, runs inline, and scales out with one agent per host. No published throughput (UNVERIFIED). The README says: "Two instances processing the same log patterns will converge on identical templates". So it is deterministic, except during early training. Seeds and warm-up reduce this ([README](https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/main/processor/drainprocessor/README.md)).
- **Integration without replacing anything:** run the collector as a **second, independent reader** of the same log files. The pipeline: `file_log` receiver → `drain` → `count` connector → Prometheus remote-write or exporter.
  - `file_log` is beta. It was renamed from `filelog` in v0.149.0, and `filelog` stays as a deprecated alias ([CHANGELOG](https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/v0.160.0/CHANGELOG.md)).
  - The Filebeat → Elasticsearch path does not change.
  - Only log *counts* leave the host, so it is cheap.
  - Another option: put it behind Kafka, if we already send a copy of the logs there.
- **Operations effort:** medium. It is another agent on each host (a Kubernetes DaemonSet, one pod per node). It also needs mask tuning and a limit on cardinality.
- **Maturity:** the component is **alpha**. [metadata.yaml @v0.160.0](https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/v0.160.0/processor/drainprocessor/metadata.yaml) says `alpha: [logs]`, with the distributions contrib and `k8s`. From the [CHANGELOG @v0.160.0](https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/v0.160.0/CHANGELOG.md):
  - It was added and promoted to alpha in **v0.151.0**.
  - It changed a lot up to v0.158.0. Masking rules replaced `extract_parameters`: **breaking config changes**.
  - The latest contrib release is **v0.160.0** (2026-09-02).
- **Limits:**
  - It is alpha, so the config keeps changing.
  - With weak masks, `log.record.template` strings have high cardinality. Hash them and keep only the top-N templates per service before you export them as a label.
  - Reading the files twice costs disk input/output (IO) and CPU on the hosts.
  - The `count` connector is alpha too.

#### Brain (logparser, TSC'23)

TSC'23 = the journal IEEE Transactions on Services Computing, 2023.

- **License:** the `logparser` repo bundles third-party code with mixed licenses. Its README says to "be aware of the licenses of third-party libraries". It also says that logparser's "main goal … is used for research and benchmark purpose" ([logparser README](https://raw.githubusercontent.com/logpai/logparser/main/README.md)).
- **Method and fit:** a batch (offline) parser that uses a bidirectional parallel tree. It has top accuracy on Loghub-2k ([Brain README](https://raw.githubusercontent.com/logpai/logparser/main/logparser/Brain/README.md)). It does not stream, so use it only for **offline discovery on samples**. OpenSearch ships a Brain variant as the Piped Processing Language (PPL) command `patterns … method=brain` (see §7, [OpenSearch patterns](https://docs.opensearch.org/latest/sql-and-ppl/ppl/commands/patterns/)).
- **Maturity:** research code, no releases.

#### LogAI (Salesforce)

- **License:** BSD-3 ([PyPI](https://pypi.org/pypi/logai/json)).
- **Status:** **dead**. The GitHub repo `salesforce/logai` returns **HTTP 404** (checked 2026-09-14). The last PyPI release is **0.1.5 on 2023-03-02**. The docs site is still online ([docs](https://opensource.salesforce.com/logai/latest/)). Do not adopt.

#### Elastic's own categorization

ML categorization jobs, the `categorize_text` aggregation, ES|QL `CATEGORIZE` and Kibana log pattern analysis are **all Platinum+** (§2). On Basic there is nothing.

#### Our own deterministic template key (regex masks + hash): cheapest, works on Basic

- **How it works:** copy `message`. In the copy, mask the variable tokens: numbers, hex, UUIDs, IP addresses, quoted strings and paths. Hash the result into `log.template_id` (keyword). Keep the masked string as `log.template` (keyword, with `ignore_above`).
- **Where:** two options.
  - **Filebeat:** `copy_fields` ([doc](https://www.elastic.co/docs/reference/beats/filebeat/copy-fields)) → `replace` → `fingerprint` ([replace](https://www.elastic.co/docs/reference/beats/filebeat/replace-fields), [fingerprint](https://www.elastic.co/docs/reference/beats/filebeat/fingerprint)). `replace` maps a regex `pattern` to a `replacement` and can target `@metadata.*`. `fingerprint` supports `xxhash` and sha256.
  - **Elasticsearch ingest pipeline:** `gsub` → `fingerprint` ([gsub](https://www.elastic.co/docs/reference/ingest-processor/gsub-processor), [fingerprint](https://www.elastic.co/docs/reference/ingest-processor/fingerprint-processor)). The ingest `fingerprint` supports MurmurHash3 and others. Its docs have no license note.
  - Avoid `redact`: it is a commercial feature ([redact](https://www.elastic.co/docs/reference/ingest-processor/redact-processor)).
- **Version limits:**
  - The Filebeat **`replace` processor exists since 7.8.0**. Evidence:
    - The 7.8.0 release notes in the [7.10.2 CHANGELOG](https://raw.githubusercontent.com/elastic/beats/v7.10.2/CHANGELOG.asciidoc) say "Add `replace` processor for replacing string values of fields" (#17342).
    - [replace.go @v7.10.2](https://raw.githubusercontent.com/elastic/beats/v7.10.2/libbeat/processors/actions/replace.go) registers `replace` with the same `fields` / `ignore_missing` / `fail_on_error` keys. The file is 404 at v7.7.1.
    - Only its docs page came late: [8.5](https://www.elastic.co/guide/en/beats/filebeat/8.5/replace-fields.html) returns 200, [8.4](https://www.elastic.co/guide/en/beats/filebeat/8.4/replace-fields.html) returns 404.
  - Filebeat `copy_fields` and `fingerprint` (with `xxhash`) exist in 7.10 ([7.10 fingerprint](https://www.elastic.co/guide/en/beats/filebeat/7.10/fingerprint.html)).
  - The Elasticsearch ingest `fingerprint` processor first appears in **7.12** (x-pack). The [7.12](https://www.elastic.co/guide/en/elasticsearch/reference/7.12/fingerprint-processor.html) page exists, but [7.11](https://www.elastic.co/guide/en/elasticsearch/reference/7.11/fingerprint-processor.html) is 404. `FingerprintProcessor.java` is 404 at v7.11.2.
  - Ingest `set` … `copy_from` first appears in **7.11**. It is absent from [SetProcessor.java @v7.10.2](https://raw.githubusercontent.com/elastic/elasticsearch/v7.10.2/modules/ingest-common/src/main/java/org/elasticsearch/ingest/common/SetProcessor.java) and present at v7.11.0. On 7.10 use `"value": "{{{message}}}"`.
  - So the proof-of-concept (PoC) ingest pipeline does not load on 7.10.
- **Pros:** stateless, and the same on every host and after restarts. No new services. Works on Basic, and on 7.10 with Filebeat `copy_fields` + `replace` + `fingerprint`.
- **Cons:**
  - Coarser than Drain: variable free-text words stay in the key.
  - Regex costs CPU at ingest. Benchmark it before you enable it on all hosts (cost UNVERIFIED).
  - The two variants give **different ids for the same template**. Filebeat hashes `|field|value` with xxhash and hex output: `fmt.Fprintf(to, "|%v|%v", k, v)` in [fingerprint.go @v9.5.3](https://raw.githubusercontent.com/elastic/beats/v9.5.3/libbeat/processors/fingerprint/fingerprint.go). The ingest processor uses MurmurHash3 with base64. So pick one variant for all hosts.
- **Best combination:** discover templates offline with Drain3 or Brain on a sample. Turn the learned variable positions into masks. Then build the deterministic key at ingest.

### 4.3 How to get counts per service per minute cheaply

| Path | How | Where the counts live | Cost |
|---|---|---|---|
| **A. Inside Elasticsearch** (Basic only) | Template key at ingest (see above), then a **continuous transform** into `anomaly-logs-tmpl-1m` (note 1) | A small Elasticsearch index (note 2) | One composite aggregation per checkpoint. Two traps lose data silently (note 3). |
| **B. At the edge → Prometheus** | OTel `file_log` → `drain` → `count` connector → Prometheus | Prometheus series `log_template_lines_total{service,template_id}` | No Elasticsearch reads. Cap cardinality: top-N templates per service + `other`. |
| **C. Read back from Elasticsearch** (no ingest change) | A Python consumer reads with PIT, runs Drain3 and pushes counts (note 4) | Prometheus or Elasticsearch | Re-reads `_source`, the equivalent of 23 MB/s. Sensible only on a **sample** (template discovery). |

Notes:

1. The transform is a pivot. `group_by`: `service.name`, `log.template_id`, `date_histogram 1m`. It writes `count` into `anomaly-logs-tmpl-1m`. This works on Basic.
2. About services × active templates × 1,440 docs/day.
3. At each checkpoint, the transform runs a composite aggregation over the new buckets. `max_page_search_size` defaults to 500 ([transform limits](https://www.elastic.co/docs/explore-analyze/transforms/transform-limitations)). The destination cannot be a data stream. We tested two silent-loss traps on Elasticsearch 9.5.3:
   - A `terms` group_by without `missing_bucket: true` drops every document that lacks that key. This includes a `log.template` longer than Filebeat's `ignore_above: 1024`.
   - With `sync.time.field: @timestamp`, events that arrive later than `delay` are never counted. The API docs advise a field "that contains the ingest timestamp" ([put transform](https://www.elastic.co/docs/api/doc/elasticsearch/operation/operation-transform-put-transform)).
4. It reads with PIT + `search_after`, sorted by `_shard_doc`. This is the fastest way to read everything ([paginate](https://www.elastic.co/docs/reference/elasticsearch/rest-apis/paginate-search-results)).

---

## 5. Where can we add a pipeline without replacing the core?

**Hard limit:** Filebeat supports exactly **one output**: "Only a single output may be defined" ([Filebeat outputs](https://www.elastic.co/docs/reference/beats/filebeat/configuring-output)). So to send a copy of the logs somewhere else, we need either an extra hop or a second reader.

**Our apps log to stdout.** On Kubernetes, that still means files:

- The kubelet (the agent on each node) has the container runtime write each container's stdout/stderr under `/var/log/pods`. `/var/log/containers` links to them.
- By default it rotates them at `containerLogMaxSize` 10Mi and keeps `containerLogMaxFiles` 5 ([Kubernetes logging](https://kubernetes.io/docs/concepts/cluster-administration/logging/#log-rotation)).
- Elastic's DaemonSet reads them with a `filestream` input, the `container` parser and `add_kubernetes_metadata` ([filebeat-kubernetes.yaml @v9.5.3](https://raw.githubusercontent.com/elastic/beats/v9.5.3/deploy/kubernetes/filebeat-kubernetes.yaml)).
- A second reader (5.3) is another DaemonSet with the same read-only mounts.

We tested this on files in the Container Runtime Interface (CRI) format (2026-09-15). The template key needs the app's JSON decoded (`ndjson` parser). Plain-text apps also need a timestamp mask. Details: [poc/README.md](../../poc/README.md#gotchas-found-while-validating).

| Option | Change to the core path | Adds | Pros | Cons / cost |
|---|---|---|---|---|
| **5.1 Filebeat processors** (note 1) | Config only | Nothing | CPU spread over all hosts. Can **drop** debug noise, so Elasticsearch costs less (note 2). | Regex CPU per event on app hosts. Config rollout to every Filebeat. |
| **5.2 Elasticsearch ingest pipeline** (note 3) | Pipeline on the index template (`index.default_pipeline`) | Nothing | Central: one place to change | CPU on ingest nodes at 23k–77k events/s. May need dedicated ingest nodes (UNVERIFIED sizing). |
| **5.3 Second reader on hosts** (note 4) | None | An agent per host | No load on Elasticsearch. Only counts leave the host. | Files read twice on hosts. Another fleet to manage. OTel drain is alpha ([drainprocessor](https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/main/processor/drainprocessor/README.md)). |
| **5.4 Filebeat → Logstash → {Elasticsearch, miner}** | Output switch to `logstash` ([Logstash output](https://www.elastic.co/docs/reference/beats/filebeat/logstash-output)) | A Logstash tier | Clean copy. A slow miner cannot slow down Elasticsearch (note 5). | Tier sized for the full 2 TB/day. Disk queues. Close to a "core change" (note 6). |
| **5.5 Filebeat → Kafka → {Elasticsearch sink, miner consumer group}** ([Kafka output](https://www.elastic.co/docs/reference/beats/filebeat/kafka-output)) | Output switch + Kafka + Elasticsearch sink | Kafka cluster + sink (Logstash / Kafka Connect) | Buffer that can be replayed. Any number of consumers (Drain3, later ML). | Biggest change: new infrastructure → **later iteration**. About 23 MB/s is easy for Kafka. |
| **5.6 Sidecar reading from Elasticsearch** (note 7) | None | A reader service | No pipeline change. Good for **sampled** discovery and backfills. | A full re-read is as big as ingest. PITs pin segments (note 8). |

Notes:

1. Processors: `dissect`, `drop_event`, `copy_fields`, `replace`, `fingerprint`, and `script` (JavaScript in ECMAScript 5.1, or ES5.1) ([dissect](https://www.elastic.co/docs/reference/beats/filebeat/dissect), [drop_event](https://www.elastic.co/docs/reference/beats/filebeat/drop-event), [script](https://www.elastic.co/docs/reference/beats/filebeat/processor-script)).
2. `template_id`, `service` and `level` are present at index time.
3. `gsub` + `fingerprint`, and `dissect`/`grok` ([gsub](https://www.elastic.co/docs/reference/ingest-processor/gsub-processor), [fingerprint](https://www.elastic.co/docs/reference/ingest-processor/fingerprint-processor)).
4. The OTel Collector `file_log` → `drain` → `count` → Prometheus. Or a second Filebeat with its own `path.data` → Redis/Kafka/Logstash.
5. Logstash has **forked-path** and **output-isolator** pipeline-to-pipeline patterns. They send a clean copy and keep a slow miner away from Elasticsearch ([pipeline-to-pipeline](https://www.elastic.co/docs/reference/logstash/pipeline-to-pipeline)). Logstash also has `fingerprint` and `mutate gsub` filters.
6. Persistent queues (PQ) on disk ([PQ](https://www.elastic.co/docs/reference/logstash/persistent-queues)).
7. It reads with PIT + `search_after` on `_shard_doc`, in slices ([paginate](https://www.elastic.co/docs/reference/elasticsearch/rest-apis/paginate-search-results)).
8. A full re-read fetches `_source` for every document. That means about 2 TB/day to decompress and send as JSON over HTTP, ≈ 23–77k docs/s of sustained fetch. It is a second "search workload" as big as ingest. This is our estimate, not a benchmark. Also, an open PIT keeps old index segments alive (it "pins" them).

**Cost comparison (estimate):**

- Reading logs at ingest (5.1, 5.3) costs host CPU for regex or Drain. It also sends a few KB/s of count metrics per host.
- Re-reading 2 TB/day from Elasticsearch (5.6) costs fetch-phase CPU and IO in Elasticsearch. That is about the same as a large continuous search load. It also sends the full raw volume over the network.
- So: **read logs at ingest. Read back from Elasticsearch only for samples (for example 1% using time slices) and for incident bundles.**

---

## 6. Other free options on Elasticsearch data

All options in this section work on Basic.

### 6.1 Kibana alerting on Basic

This covers the Elasticsearch query rule (with ES|QL), the log threshold rule and the index threshold rule.

- **License:** the ELv2 default distribution, included in Basic ✅ (§2).
- **What is free** ([Elasticsearch query rule](https://www.elastic.co/docs/explore-analyze/alerting/alerts/rule-type-es-query), [log threshold](https://www.elastic.co/docs/solutions/observability/incident-management/create-log-threshold-rule), [subscriptions](https://www.elastic.co/subscriptions)):
  - the Elasticsearch query rule: KQL, Query DSL or **ES|QL**, "alert for each row", grouping GA in 9.2
  - the index threshold rule
  - the log threshold rule: group-by and **ratio A/B**
  - the custom threshold rule
  - alert suppression and snooze
  - **only the Index and Server log connectors**. The Cases connector is Platinum (§2).
- **Detection method:** thresholds. A simple seasonal check in ES|QL compares the last 10 minutes with the same window last week. The example below is an **untested sketch**:
  ```esql
  FROM anomaly-logs-tmpl-1m
  | WHERE @timestamp >= NOW() - 10 minutes
       OR (@timestamp >= NOW() - 7 days - 10 minutes AND @timestamp < NOW() - 7 days)
  | EVAL is_cur = CASE(@timestamp >= NOW() - 10 minutes, 1, 0)
  | EVAL cur_c = count * is_cur
  | STATS cur = SUM(cur_c), total = SUM(count) BY service.name, log.template_id
  | EVAL ref = total - cur, ratio = (cur + 1.0) / (ref + 1.0)
  | WHERE cur > 100 AND (ratio > 5 OR ratio < 0.2)
  ```
  The query does not use `CHANGE_POINT` or `CATEGORIZE`, because they need Platinum.
- **Scale:** fine **on the 1-minute template-count index**. Do not run rules every minute over the raw 2 TB/day indices.
- **Integration:** alerts → **Index connector** → `alerts-relay` index. Then ElastAlert2 (an `any` rule → Slack or Alertmanager) or Grafana forwards them. This is the standard workaround from §2.
- **Operations effort:** low.
- **Limits:**
  - Rules live in Kibana saved objects. Export them to keep them in Git (GitOps).
  - Maintenance windows are Platinum.
  - `xpack.encryptedSavedObjects.encryptionKey` is required ([alerting setup](https://www.elastic.co/docs/explore-analyze/alerting/alerts/alerting-setup)).

### 6.2 Grafana OSS + Elasticsearch data source + Grafana Alerting

- **License:** Grafana OSS is AGPLv3 ([LICENSE](https://raw.githubusercontent.com/grafana/grafana/main/LICENSE)). The Elasticsearch data source is a standalone plugin. It comes preinstalled in OSS since Grafana v13.0 ([Elasticsearch data source](https://grafana.com/docs/grafana/latest/datasources/elasticsearch/)).
- **What is free** ([Elasticsearch data source](https://grafana.com/docs/grafana/latest/datasources/elasticsearch/)):
  - Elasticsearch ≥ v7.17, 8.x and 9.x
  - query types: metrics, logs, raw DSL and **ES|QL**
  - alerting "based on Elasticsearch query results"
- **Alerting limits** ([Elasticsearch alerting](https://grafana.com/docs/grafana/latest/datasources/elasticsearch/alerting/)):
  - Alerting is "Supported" for **metrics queries with a date histogram**. Without one it is "Limited".
  - Logs and Raw data queries are **not supported** for alerting.
  - Alert queries cannot use template variables.
  - ES|QL in alert rules: UNVERIFIED.
- **Detection method:** expressions (math, reduce, threshold) across several queries. Each query can have its own **fixed relative time range**. So a rule can compute the ratio of the current value to a `now-7d` baseline, the same idea as in 6.1 ([queries & conditions](https://grafana.com/docs/grafana/latest/alerting/fundamentals/alert-rules/queries-conditions/)). Grafana's ML and outlier features are Cloud-only (covered in other files, UNVERIFIED here).
- **Scale:** good on the pre-aggregated index. Each rule runs one `date_histogram` aggregation per evaluation.
- **Integration without replacing anything:** yes. It gives **one place for alerts and notifications**: logs (Elasticsearch), metrics (Prometheus), and free contact points (Slack, webhook, PagerDuty…). So it avoids Kibana's Gold-only connectors. It does **not** support OSS 7.10, because it needs ≥ 7.17.
- **Limits:** Elasticsearch 7.10 is not supported. Alert queries must return metrics. Many rules mean many Elasticsearch aggregations.

### 6.3 ElastAlert2: see §3

It is the best free engine for novelty, flatline and spike rules over Elasticsearch. It is also the relay to Slack, PagerDuty and Alertmanager on Basic.

### 6.4 Error grouping ("Sentry-like")

- Sentry self-hosted uses the **FSL-1.1-Apache-2.0** license, a Functional Source License ([LICENSE](https://raw.githubusercontent.com/getsentry/sentry/master/LICENSE.md)). This is not open source by the Open Source Initiative (OSI) definition. The code converts to Apache-2.0 after 2 years. The latest self-hosted release is **26.8.0** ([releases](https://github.com/getsentry/self-hosted/releases/latest)).
- Sentry needs instrumentation with its software development kit (SDK) in the apps. So it is a parallel pipeline, not an add-on to logs. **Not recommended for iteration 1.**
- We can do the same on logs. For records with `error.stack_trace`, fingerprint `error.type` plus the top N normalized stack frames. Use a Filebeat `script` or ingest `gsub` + `fingerprint`. The result is `error.group_id`. An ElastAlert2 `new_term` rule on `error.group_id` then means "a new error type appeared". It is the same mechanism as template IDs.

### 6.5 Not on Basic

For completeness, these need a paid license (§2): Watcher (Gold+), ML jobs, AIOps, `CHANGE_POINT`, `CATEGORIZE`, SLOs and maintenance windows.

---

## 7. OpenSearch Anomaly Detection: only if we ever move

This is for a later iteration, and only if we ever replace Elasticsearch with OpenSearch.

- **License:** Apache-2.0 for the plugin ([anomaly detection README](https://raw.githubusercontent.com/opensearch-project/anomaly-detection/main/README.md)).
- **Versions:** the latest OpenSearch is **3.8.0** (2026-08-05) ([OpenSearch releases](https://github.com/opensearch-project/OpenSearch/releases/latest)). The anomaly detection plugin is **3.8.0.0** (2026-07-31). GitHub's `releases/latest` still points at 3.6.0.0. But the [plugin releases feed](https://github.com/opensearch-project/anomaly-detection/releases.atom) lists 3.7.0.0 and 3.8.0.0.
- **What is free** ([OpenSearch anomaly detection docs](https://docs.opensearch.org/latest/observing-your-data/ad/index/)):
  - streaming [Random Cut Forest](https://proceedings.mlr.press/v48/guha16.html) (RCF) detectors over aggregation features
  - real-time and historical analysis
  - **categorical (high-cardinality) detectors**, one model per entity
  - integration with the alerting plugin
  - by default at most **5 features** per detector
  - a cold start uses up to **10,000** historical points
  - entity capacity ≈ `(data nodes × heap × AD memory %) / entity model size`. For example, 3 nodes × 8 GB × 10% / 1 MB ≈ **2,429 entities**.
- **Log patterns:** PPL `patterns` has a **Brain** method, for free log pattern grouping ([PPL patterns](https://docs.opensearch.org/latest/sql-and-ppl/ppl/commands/patterns/)).
- **Scale:** entity models live in the heap of the Java virtual machine (JVM). So it is fine for the top-N `service × template` pairs (thousands), but not for millions of entities. It would use the same idea of a pre-aggregated count index.
- **Why not now:** it means replacing Elasticsearch + Kibana with OpenSearch + Dashboards. That also needs a data migration and a log shipper that works with OpenSearch. We did not check whether current Filebeat works with OpenSearch (UNVERIFIED). This breaks the iteration-1 rule of add-ons only. ElastAlert2 and Grafana both already support OpenSearch. So the add-ons we choose now would survive such a move.

---

## 8. Our recommendation

For Basic, 200 GB–2 TB/day, add-ons only.

**Principle:** never run anomaly detection on raw logs.

1. Turn logs into counts per minute of `(service, template_id, level)`, at the edge or at ingest.
2. Run cheap statistics and novelty detection on the counts.
3. Send only small anomaly bundles to an LLM.

**Step 0: confirm what we run (1 hour).**

- `GET /`: check `build_flavor` and `version.number`.
- `GET _license`: is it `type: basic`?
- `GET _xpack?categories=features`: are `transform` and `esql` available?
- Check the Elasticsearch major version for ElastAlert2 and Grafana. Grafana needs ≥ 7.17.

**Iteration 1 (default distribution + Basic):**

1. **Template key at ingest (stateless).** Filebeat `copy_fields` → `replace` masks (Filebeat ≥ 7.8) → `fingerprint` → `log.template_id` (and `log.template`). Use `drop_event` for known debug noise. Build the first mask list from an **offline Drain3 run on a 1% sample**, read with PIT. (§4.2 own template key, §5.1)
2. **Pre-aggregate on Basic.** One continuous **transform** writes `anomaly-logs-tmpl-1m`: `service.name`, `log.template_id`, `log.level` and a 1-minute `count`. The template rules read this index. The flatline and error-ratio rules run terms aggregations on the raw indices. (§4.3 A)
3. **Detection with ElastAlert2** (single replica + dead-man's switch). Rules:
   - `new_term` on `log.template_id`: a new template
   - `flatline` per service: logs stopped
   - `spike` / `spike_aggregation` per (service, template), with `threshold_ref`
   - `percentage_match` for the error ratio

   Alerts go to **Alertmanager**, so logs and metrics share routing and silencing. (§3)
4. **Dashboards and seasonal checks.** Grafana OSS with the Elasticsearch data source on `anomaly-logs-tmpl-1m`, with week-over-week ratio rules. Or Kibana ES|QL Elasticsearch query rules → Index connector → relayed by ElastAlert2. (§6.1, §6.2)

**Later (iteration 2):**

- **LLM triage.** On an alert, fetch ≤ N sample lines per anomalous template, plus the count series. The query uses PIT and is limited to the service, the template and a 10-minute window. An LLM writes a summary. The cost grows with the number of alerts, not with log volume.

**Parallel proof of concept (optional, still nothing replaced):** run the OTel Collector `file_log → drain → count` on a few hosts. It exports `log_template_lines_total{service,template_id}` to Prometheus. This gives real Drain templates. It also puts log-template anomaly detection into the same Prometheus anomaly detection tools as metrics. Treat it as alpha. (§4.2, §5.3)

**Not recommended now:**

- Watcher, ML, AIOps: paid. Self-managed Platinum is not sold to new customers, so we would need an Enterprise quote.
- A full read-back of 2 TB/day from Elasticsearch.
- A Kafka copy of the log stream (later).
- A move to OpenSearch (later).
- LogAI (dead).

### If our cluster is actually OSS-only 7.10.x

- `GET /` shows `build_flavor: "oss"`. The build has none of these (OSS column on [subscriptions](https://www.elastic.co/subscriptions)): `_license`, Kibana alerting, transforms, ES|QL, Lens, ILM and data streams.
- 7.10 is far past its end of life. Elastic lists 7.17.x as the last 7.x. Its maintenance ended on 15-Apr-2025, and its support ended on 15-Jan-2026. Anything older is unsupported ([EOL](https://www.elastic.co/support/eol)).
- What still works:
  - **ElastAlert2.** Its requirements list "Elasticsearch 7, 8, or 9", and it uses the 7.10.1 Python client. A run against a 7.10 OSS server is still UNVERIFIED.
  - The Filebeat processors `copy_fields`, `replace` and `fingerprint`. All are in Beats 7.10 (`replace` since 7.8.0).
  - Ingest `gsub`. But the PoC ingest pipeline does not work as written: ingest `set … copy_from` needs 7.11, and ingest `fingerprint` needs 7.12 (see §4.2).
- Pre-aggregation must happen **outside Elasticsearch**, because there are no transforms. Options: OTel `drain → count → Prometheus` (§5.3), or a small cron job that runs Elasticsearch aggregations and pushes to Prometheus/Pushgateway.
- The Grafana Elasticsearch data source requires **≥ 7.17**. So Grafana alerting over Elasticsearch is out. Route everything through Prometheus counts instead.
- Our strong advice for that case: plan an upgrade or an OpenSearch migration as iteration 2. The upgrade would go to the default distribution 8.19/9.x with Basic, which is free. Both are "core swaps". But an end-of-life 7.10 cluster is the bigger risk, because of its security exposure.
