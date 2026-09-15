# 04 — Alert correlation, root cause analysis, LLM triage and its cost, commercial prices

**In short:**

- **Alert correlation in iteration 1: a tuned Alertmanager plus ElastAlert2**. Kibana on the
  Basic license cannot send webhooks (only Server Log and Index connectors). So ElastAlert2
  carries Elasticsearch detections to Alertmanager. **Keep OSS**, the open-source software
  (OSS) edition of Keep, is optional (iteration 2), for incidents that span several sources.
  It has an MIT license and is very active. Its **artificial intelligence (AI) correlation is
  not open source**: it is in Cloud and Enterprise only.
- **Do not adopt Grafana OnCall OSS**. It went into maintenance mode on 2025-03-11 and was
  **archived on 2026-03-24**. Its replacement, Grafana IRM (an on-call service), runs only in
  Grafana Cloud.
- **HolmesGPT is the best open-source on-demand investigator for our stack**. It uses a
  [large language model](https://en.wikipedia.org/wiki/Large_language_model) (LLM) to find
  the root cause of an alert. This is called
  [root cause analysis](https://en.wikipedia.org/wiki/Root_cause_analysis) (RCA). It has
  toolsets for Prometheus, Elasticsearch/OpenSearch and Grafana. It has **no Jaeger
  toolset**, so use its custom REST toolset. Its own benchmarks put the **average cost per
  investigation at $0.02–$0.32** (16 tests, 2026-03-15). A newer run gives **$0.01–$0.39**
  (63 tests, 2026-08-05, gpt-5.6-luna to opus-5). The cost depends on the model.
- **LLM triage on small evidence bundles is cheap**. A bundle is 5–20k tokens in and
  0.5–1k tokens out. Monthly cost:
  - **$1–$24/month** at 50 small bundles/day.
  - **$9–$202/month** at 200 bundles/day.
  - **$36–$780/month** at 500 large bundles/day.
  - Batch processing halves these numbers, but it does not suit paging.
  - Self-hosting on one NVIDIA L4 graphics card (GPU) costs **about $588/month**, running
    24×7. The machine is an Amazon Web Services (AWS) g6.xlarge. That is more than any
    cheap-API scenario.
- **An LLM cannot read all raw logs**. 2 TB/day is about 500 billion tokens/day. That costs
  **about $12.5k/day, or about $375k/month**. This is at the cheapest batch rate we found:
  OpenAI gpt-5-nano at $0.025 per million input tokens. At Gemini 2.5 Flash-Lite batch
  ($0.05 per million) it is about $750k/month. It would also need about 5.8 M tokens/s, all day.
- **Commercial products at list prices:**
  - Grafana Cloud Pro: about **$8.1k–$8.7k/month** at 200 GB/day + 1 M series with a 60 s
    scrape interval. About **$79k–$85k/month** at 2 TB/day + 10 M series. A 15 s scrape
    roughly triples it.
  - Datadog: about **$62k** to about **$609k/month**, mostly for custom metrics.
  - Elastic Serverless Complete: about **$2.3k–$3.5k** to about **$10.6k–$17.4k/month**
    with its graduated volume tiers (§3.2).
  - Elastic self-managed Platinum/Enterprise: **quote-only**. Elastic sells Platinum to
    existing customers only.

Terms and abbreviations: [glossary](../glossary.md).

We want anomaly detection built from free parts and a little of our own code, not a vendor
product. This file covers what happens after a detector fires: grouping alerts, finding the
root cause, and an optional LLM summary. It also prices the commercial products we would
otherwise buy.

Checked on **2026-09-14**. Every "checked" date below means this date, unless the text says
otherwise. All links are in [LINKS.md](../../LINKS.md) (merged, no duplicates).

Our stack: Filebeat → Elasticsearch/Kibana (Basic license, self-managed), Prometheus +
Grafana OSS + Alertmanager, Jaeger. Volume: 200 GB–2 TB/day of logs and 1–10 M active
series. Iteration 1 only adds tools. It does not replace core parts. We use an LLM only at
the tail: at the end of the chain, on data that detectors have already reduced.

Sections:

1. Which open-source tools group alerts and find root causes? (§1.1–§1.10)
2. LLM triage: how it works and what it costs (§2.1–§2.6)
3. What would commercial products cost? (§3.1–§3.2)
4. What do we recommend?
5. What is still UNVERIFIED?

---

## 1. Which open-source tools group alerts and find root causes?

Many of these tools call themselves AIOps tools. [AIOps](https://en.wikipedia.org/wiki/AIOps)
means "AI for IT operations". It uses AI and machine learning (ML) to detect, group and
explain IT problems. RCA means finding the first fault behind a problem. Each tool card
below uses the same fields: license, what is free, what it does, scale, fit with our stack,
effort, maturity and limits.

### 1.1 Prometheus Alertmanager (baseline, free)

- **License:** Apache-2.0 ([GitHub API](https://api.github.com/repos/prometheus/alertmanager), 2026-09-14).
- **What is free:** everything.
- **What it does** (all four points are from the [Alertmanager docs](https://prometheus.io/docs/alerting/latest/alertmanager/)):
  - **Grouping:** it "categorizes alerts of similar nature into a single notification".
  - **Inhibition:** it holds back notifications for an alert "if certain other alerts are
    already firing".
  - **Silences:** they mute alerts for a set time.
  - **High availability (HA):** several Alertmanagers form a cluster and share state by
    [gossip](https://en.wikipedia.org/wiki/Gossip_protocol). Prometheus must point at all
    peers, not at a load balancer.
  - It has no ML and no enrichment. It has no incident object that joins alerts from
    several sources.
- **Scale:** it handles alert volume, not telemetry volume. Any realistic alert rate is fine.
- **How it fits our stack:**
  - We already run it. So it is an **add-on: config only**.
  - Grafana OSS alerting can send alerts to an external Alertmanager. Set it up as a data
    source with forwarding on, or as a contact point
    ([Grafana docs](https://grafana.com/docs/grafana/latest/alerting/set-up/configure-alertmanager/)).
  - Elasticsearch alerts need a bridge. Kibana on **Basic** has only the "Server Log and
    Index" connectors. Webhook, Slack, PagerDuty and other "third-party alerting actions"
    need Gold or higher. Gold is discontinued, and Platinum is "Existing Customers Only"
    ([elastic.co/subscriptions](https://www.elastic.co/subscriptions)).
  - So we use **ElastAlert2**: Apache-2.0, `elastalert2-2.31.0` released 2026-07-22
    ([release](https://github.com/jertel/elastalert2/releases/latest)). It has a native
    Alertmanager alerter, `alertmanager_hosts`
    ([docs](https://raw.githubusercontent.com/jertel/elastalert2/master/docs/source/alerts.rst)).
  - Another option: the Kibana Index connector plus a small program that polls the index
    ([Elastic forum](https://discuss.elastic.co/t/alerts-webhook-with-basic-license-elk-8-12/353216/3)).
- **Ops effort:** low. Add an HA pair, and keep the routing, inhibition and silence templates
  under version control.
- **Maturity:** v0.34.0 from 2026-08-16, not archived, 8.6k stars, last push 2026-09-14
  ([GitHub API](https://api.github.com/repos/prometheus/alertmanager)).
- **Limits:**
  - Grouping only matches equal label values. So correlation is only as good as our labels.
    `service`, `cluster`, `env` and `team` must be the same in Prometheus, ElastAlert2 and
    the rules built on Jaeger data.
  - It keeps no memory of incidents between group windows.
  - Silences are manual.
- **Sources:** above.

### 1.2 Keep (keephq/keep): open-source AIOps and alert management

- **License:** MIT, **except the `ee/` folder**. `ee/` carries the Keep Enterprise License:
  production use needs a subscription. Today `ee/` holds only `identitymanager`
  ([LICENSE](https://raw.githubusercontent.com/keephq/keep/main/LICENSE),
  [ee/LICENSE](https://raw.githubusercontent.com/keephq/keep/main/ee/LICENSE)). So Keep is
  open core.
- **Owner:** Elastic bought Keep in 2025. The announcement says Keep "will integrate with
  Elasticsearch and Kibana and remain open source"
  ([Elastic press release](https://ir.elastic.co/news/news-details/2025/Elastic-Completes-Acquisition-of-Keep/default.aspx),
  [Elastic blog](https://www.elastic.co/blog/elastic-and-keep-join-forces)). This is a
  roadmap risk. Watch it.
- **What is free (OSS):**
  - Deduplication: partial, by fingerprint, and full
    ([docs](https://docs.keephq.dev/overview/deduplication.md)).
  - **Manual correlation rules** on alert attributes, with AND/OR and templated incident
    names ([docs](https://docs.keephq.dev/overview/correlation-rules.md)).
  - **Topology correlation**, turned on with `KEEP_TOPOLOGY_PROCESSOR=true`
    ([docs](https://docs.keephq.dev/overview/correlation-topology.md)).
  - Maintenance windows ([docs](https://docs.keephq.dev/overview/maintenance-windows.md)).
    Keep uses [CEL](https://cel.dev/) expressions for them (Common Expression Language).
  - Workflows, including **"AI in Workflows"** (OSS ✅). It uses an LLM that we choose:
    OpenAI, Anthropic, Gemini, [Ollama](https://ollama.com/),
    [llama.cpp](https://github.com/ggml-org/llama.cpp), [vLLM](https://docs.vllm.ai/), …
    ([docs](https://docs.keephq.dev/overview/ai-in-workflows.md)).
  - A local LLM through [LiteLLM](https://docs.litellm.ai/) or `OPENAI_BASE_URL`
    ([docs](https://docs.keephq.dev/deployment/local-llm/keep-with-litellm.md)).
- **Paid or limited:**
  - **AI Correlation** is marked "Keep Open Source: ⛔️". It uses a "proprietary model
    developed and hosted by Keep" ([docs](https://docs.keephq.dev/overview/ai-correlation.md)).
  - AI Incident Assistant and AI Semi-Automatic Correlation are "experimental" in OSS
    ([docs](https://docs.keephq.dev/overview/ai-incident-assistant.md),
    [docs](https://docs.keephq.dev/overview/ai-semi-automatic-correlation.md)).
  - Login: Keycloak, Auth0 and AzureAD need the Enterprise Edition (**EE**). Database login,
    OAuth2Proxy, Okta and OneLogin are OSS
    ([auth matrix](https://docs.keephq.dev/deployment/authentication/overview.md)).
- **What it does:** one "single pane" for alerts and incidents from all sources. It adds
  enrichment, routing, workflows (automatic actions) and incident objects.
- **Scale:** built for alert volume. The docs say it was designed for fewer than 10k alerts in
  total. Above 1 M alerts in total, or tens of thousands per day, it needs more parts. Run
  Elasticsearch for alert storage, plus Redis with the ARQ job queue
  ([stress-testing](https://docs.keephq.dev/deployment/stress-testing.md)). That is fine for
  our alert rate. Keep never touches raw telemetry.
- **How it fits our stack (add-on):**
  - Prometheus/Alertmanager and Grafana providers (pull or push).
  - Elastic provider: it queries Elasticsearch.
  - Kibana provider: it gets alerts by webhook, and Basic cannot send webhooks. So use
    ElastAlert2 → Keep webhook, or let Keep poll Elasticsearch.
  - We found no Jaeger provider in the docs index ([llms.txt](https://docs.keephq.dev/llms.txt)).
- **Ops effort:** medium. It needs a backend, a frontend and a database (Postgres/MySQL).
  Elasticsearch and Redis are optional. Install it with a Kubernetes Helm chart or Docker.
- **Maturity:** v0.54.3 from 2026-09-09, 12.3k stars, last push 2026-09-13, not archived
  ([GitHub API](https://api.github.com/repos/keephq/keep)).
- **Limits:**
  - The headline feature, "AIOps 2.0" AI correlation, is not OSS.
  - Single sign-on (SSO) through Keycloak needs EE. Use OAuth2Proxy in OSS.
  - After the purchase, the roadmap may move toward features built into Kibana.
- **Sources:** above.

### 1.3 Grafana OnCall OSS and Grafana IRM

- **License:** AGPL-3.0.
- **Status: ARCHIVED**. Maintenance mode started on 2025-03-11. Grafana archived the repo on
  2026-03-24. The `grafana/oncall` repo now redirects to `grafana-cold-storage/oncall`,
  which shows `archived: true` ([GitHub API](https://api.github.com/repos/grafana/oncall),
  [repo README](https://github.com/grafana-cold-storage/oncall)). The last release is
  v1.16.11 (2026-02-11).
- **Cloud features are gone:** since the archiving, OSS users no longer get the features that
  need the cloud connection: mobile push, text messages (SMS) and phone calls. The
  [Grafana notice](https://grafana.com/docs/oncall/latest/set-up/open-source/) lists "End of
  Cloud Connection support" on 2026-03-24. See also the
  [blog](https://grafana.com/blog/grafana-oncall-maintenance-mode/).
- **Replacement:** **Grafana Cloud IRM**, Grafana's on-call and incident service. It is
  software as a service (SaaS) only. The free tier includes 3 active IRM users/month. Pro
  costs **$20 per active IRM user** ([pricing](https://grafana.com/pricing/)).
- **How it fits our stack:** IRM is a cloud service, so it adds a SaaS dependency. It covers
  on-call only, so no telemetry has to leave our network.
- **Recommendation:** do not deploy OnCall OSS in 2026. For paging, use Alertmanager receivers
  (PagerDuty, Opsgenie, Slack). Or use Grafana Cloud IRM, if SaaS on-call is acceptable.

### 1.4 HolmesGPT: an LLM investigation agent

- **License:** Apache-2.0. The repo description says "SRE Agent - **CNCF Sandbox Project**".
  SRE means site reliability engineering. The Cloud Native Computing Foundation (CNCF)
  accepted it at Sandbox level on 2025-10-08, says its
  [CNCF project page](https://www.cncf.io/projects/holmesgpt/). The repo moved from
  `robusta-dev/holmesgpt` to `HolmesGPT/holmesgpt`
  ([GitHub API](https://api.github.com/repos/HolmesGPT/holmesgpt),
  [README](https://raw.githubusercontent.com/HolmesGPT/holmesgpt/master/README.md)).
- **What is free:** the whole agent: the command-line tool (CLI), the Helm chart and operator
  mode. The Slack/Teams interface comes "via Robusta" (the Robusta platform).
- **What it does:**
  - It runs an agent loop. The LLM calls read-only tools ("toolsets"), reads the results and
    repeats until it has a root-cause hypothesis.
  - It fetches alerts from Alertmanager, PagerDuty, OpsGenie and Jira, and writes its findings
    back.
  - "Operator mode" runs scheduled health checks in the background
    ([README](https://raw.githubusercontent.com/HolmesGPT/holmesgpt/master/README.md)).
- **Toolsets that matter for us**
  ([built-in list](https://holmesgpt.dev/latest/data-sources/builtin-toolsets/)):
  - `prometheus/metrics`
    ([docs](https://holmesgpt.dev/latest/data-sources/builtin-toolsets/prometheus/)).
  - `elasticsearch/data` + `elasticsearch/cluster`
    ([docs](https://holmesgpt.dev/latest/data-sources/builtin-toolsets/elasticsearch/)).
  - Grafana dashboards, Loki, Tempo, Kubernetes, Bash, databases.
  - **No Jaeger toolset**. Use a custom REST API toolset against the Jaeger query API. The
    effort for this is UNVERIFIED.
- **LLM providers** ([docs](https://holmesgpt.dev/latest/ai-providers/)):
  - Hosted: Anthropic, Bedrock, Azure AI Foundry, Gemini, Vertex, OpenAI, OpenRouter.
  - Self-hosted: **OpenAI-compatible** endpoints such as vLLM or LiteLLM. They "must support
    function calling" ([docs](https://holmesgpt.dev/latest/ai-providers/openai-compatible/)).
  - Self-hosted: **Ollama**. It is "experimental … Tool-calling capabilities are limited and
    may produce inconsistent results" ([docs](https://holmesgpt.dev/latest/ai-providers/ollama/)).
- **Tokens and cost per investigation**. The primary source is HolmesGPT's own evaluation
  suite (150+ evals, [docs](https://holmesgpt.dev/latest/development/evaluations/)). Its fast
  benchmark has 16 tests, run on 2026-03-15
  ([results](https://holmesgpt.dev/latest/development/evaluations/history/results_20260315_041151/)):

  | Model | Pass rate | Average $ per investigation | Max $ | Average latency |
  |---|---|---|---|---|
  | opus-4.6 | 16/16 | $0.32 | $0.51 | 42 s |
  | sonnet-4.6 | 16/16 | $0.18 | $0.31 | 35 s |
  | gpt-5.4 | 13/16 | $0.13 | $0.30 | 48 s |
  | gemini-3.1-pro-preview | 14/16 | $0.12 | $0.39 | 38 s |
  | haiku-4.5 | 13/16 | $0.06 | $0.12 | 29 s |
  | qwen-next-80B-instruct | 12/16 | $0.04 | $0.09 | 32 s |
  | deepseek-v3.2-chat | 13/16 | $0.02 | $0.04 | 188 s |

  - **A newer run with current models** (2026-08-05, 63 tests,
    [results](https://holmesgpt.dev/latest/development/evaluations/history/results_20260805_121417/)):
    - gpt-5.6-luna: 45/63 passed, average $0.01 (max $0.03).
    - gpt-5.6-terra: 52/63, $0.07 (max $0.24).
    - sonnet-5: 52/63, $0.12 (max $0.53).
    - opus-5: 59/63, $0.39 (max **$3.90**).
    - Haiku 4.5 was not in this run.
  - **Implied tokens:** Haiku 4.5 costs $1 per million input tokens and $5 per million output
    tokens. So its $0.06 is about 40–55k input tokens per agent investigation. That is roughly
    3–10× a pre-built bundle sent in one call (derived, not measured).
  - **Self-hosted benchmark** (2025-10-08,
    [results](https://holmesgpt.dev/latest/development/evaluations/history/custom_self_hosted_results_20251008_053744/)):
    llama-4-maverick scored **0%**, glm-4.6 **2%**, deepseek-v3.1-terminus 84%. So open models
    *can* work. But only large ones were tested, and some fail completely at tool calling.
    **We found no results for 7–14B models**. Treat small models as unproven for agent-style
    RCA.
- **Scale:** it queries the backends on demand, and the backends filter the data on the
  server. It never reads the raw stream. So 2 TB/day does not affect it.
- **How it fits our stack:** **add-on**. It needs read-only credentials for Prometheus,
  Elasticsearch and Grafana.
- **Ops effort:** low to medium. Run the CLI or the Helm chart, and give it an LLM key or
  endpoint.
- **Maturity:** 0.41.0 (2026-09-08), 3.3k stars, last push 2026-09-14.
- **Limits:**
  - Cost per investigation has no upper bound unless we limit steps and tool calls. The
    highest costs seen: $0.84 with sonnet-4 in 2025-10, and $3.90 with opus-5 in 2026-08.
  - Tool output can carry log content to the LLM vendor.
  - The version is still below 1.0.

### 1.5 k8sgpt

- **License:** Apache-2.0. v0.4.39 from 2026-09-14, 8.2k stars
  ([GitHub API](https://api.github.com/repos/k8sgpt-ai/k8sgpt)).
- **What it does:** analyzers for Kubernetes objects only: pod, pvc, rs, service, event,
  ingress, statefulset, deployment, … An LLM explains what they find. Backends include
  `localai`, `ollama`, `litellm`, `customrest`, OpenAI, Bedrock and more. An operator mode "can
  integrate with your existing monitoring such as Prometheus and Alertmanager"
  ([README](https://raw.githubusercontent.com/k8sgpt-ai/k8sgpt/main/README.md)).
- **Relevance for us: low**. It does not read Elasticsearch logs, Prometheus metric anomalies
  or Jaeger traces. It only diagnoses the state of Kubernetes objects. It helps only if our
  workloads run on Kubernetes, as a cheap "why is this pod broken" add-on.
- **CNCF status:** Sandbox since 2023-12-19 ([CNCF](https://www.cncf.io/projects/k8sgpt/)).
  The README does not say this.

### 1.6 Coroot Community Edition (CE)

- **License:** Apache-2.0. v1.26.0 from 2026-09-07, 7.9k stars
  ([GitHub API](https://api.github.com/repos/coroot/coroot)).
- **What is free (CE):**
  - An [eBPF](https://ebpf.io/what-is-ebpf/) node agent. eBPF runs small, sandboxed programs
    inside the Linux kernel. The agent builds a service map by itself. It collects
    [RED metrics](https://grafana.com/blog/2018/08/02/the-red-method-how-to-instrument-your-services/)
    (rate, errors, duration), logs, traces and profiles.
  - Alerting based on [Service Level Objectives](https://sre.google/workbook/alerting-on-slos/)
    (SLOs).
  - Built-in inspections.
- **Enterprise only:** **AI-based RCA**, **SSO**, **RBAC** and support
  ([coroot.com/enterprise](https://coroot.com/enterprise/)). RBAC means role-based access
  control. Enterprise starts at **$1 per monitored CPU core/month**. CE users can get AI RCA
  "by connecting to Coroot Cloud with **10 free investigations per month**"
  ([docs/ai](https://docs.coroot.com/ai/overview), [pricing](https://coroot.com/pricing/)).
- **A design worth copying:** Coroot's RCA follows the service dependency graph with ML, not
  with an LLM. The LLM only summarizes the findings. Coroot "sends only its findings to the
  selected model, not all the raw telemetry data"
  ([docs/ai](https://docs.coroot.com/ai/overview)). This is exactly our "LLM at the tail"
  pattern.
- **How it fits our stack (partly an add-on, partly a parallel stack):**
  - It needs Prometheus with the remote-write receiver turned on, or ClickHouse for metrics.
    It **also needs its own [ClickHouse](https://clickhouse.com/docs/intro)** database for
    logs, traces and profiles ([docs](https://docs.coroot.com/),
    [prometheus config](https://docs.coroot.com/configuration/prometheus/)).
  - It keeps its own metric cache on disk.
  - It needs Linux kernel ≥5.1 and privileged node agents
    ([requirements](https://docs.coroot.com/installation/)).
  - It does **not** read our existing Elasticsearch logs or Jaeger. It collects its own data
    with eBPF, so we would ingest logs and traces twice.
- **Scale:** at the full 2 TB/day it means a second log store. A pilot on some of the nodes
  is better. The sizing for that is UNVERIFIED.
- **Ops effort:** medium to high: a ClickHouse cluster and a privileged DaemonSet (an agent on
  every node).
- **Limits:**
  - Enterprise is priced per CPU core, and we do not know our core count. Formula:
    cores × $1/month.
  - With CE, AI RCA goes through Coroot Cloud.

### 1.7 Robusta

- **License:** MIT. The repo is now called "**Robusta Classic** - Prometheus Alert Enrichment
  for Kubernetes". AI RCA moved to HolmesGPT
  ([README](https://raw.githubusercontent.com/robusta-dev/robusta/master/README.md)). Version
  0.49.0 (2026-09-08).
- **OSS runner:** a receiver for Prometheus alert webhooks. It adds:
  - Smart grouping (Slack threads) and advanced routing.
  - Enrichment (pod logs, etc.).
  - Self-healing playbooks.
  - Tracking of Kubernetes changes.
  - Optional AI investigation through HolmesGPT.
- **SaaS platform:** a user interface and a timeline. Pricing is **quote-only** (contact form,
  [pricing](https://home.robusta.dev/pricing)). The docs say it is available as "SaaS,
  self-hosted, or open source" ([docs](https://docs.robusta.dev/master/index.html)).
- **Fit:** an add-on **centered on Kubernetes**. Its enrichment is about Kubernetes objects,
  not Elasticsearch logs or Jaeger. It matters only if our workloads run on Kubernetes. It
  overlaps with Alertmanager + HolmesGPT.

### 1.8 SigNoz: a **swap** (not an add-on)

- **License:** MIT Expat, except `ee/` and `cmd/enterprise/`
  ([LICENSE](https://raw.githubusercontent.com/SigNoz/signoz/main/LICENSE)). Open core.
  v0.141.1 (2026-09-09), 32.1k stars.
- **Is anomaly detection free? No**. The
  [docs](https://signoz.io/docs/alerts-management/anomaly-based-alerts/) say:
  - **Anomaly-based alerts are tagged "SigNoz Cloud, Self-Hosted Enterprise"**.
  - They use [seasonal decomposition](https://otexts.com/fpp3/decomposition.html) plus a
    [z-score](https://en.wikipedia.org/wiki/Standard_score), with hourly, daily or weekly
    seasonality.
  - They work in the **metrics Query Builder only**. They do not support PromQL (Prometheus
    Query Language) or ClickHouse SQL.
  - The pricing page lists anomaly detection only on Teams and Enterprise
    ([pricing](https://signoz.io/pricing/)).
- **Prices:**
  - Teams (cloud): $49/month including usage. Logs and traces: $0.30/GB ingested (15-day
    retention). Metrics: $0.10 per million samples.
  - Enterprise: "starts at $4000/month".
- **Fit:** a backend on ClickHouse for
  [OpenTelemetry](https://opentelemetry.io/docs/what-is-opentelemetry/) (OTel) data. It would
  replace the storage of Elasticsearch, Jaeger and Prometheus. **Out of scope for iteration
  1**, and its anomaly detection is not free anyway.

### 1.9 OpenObserve: a **swap**

- **License:** AGPL-3.0. v1.0.0 from 2026-09-11, 21.8k stars
  ([GitHub API](https://api.github.com/repos/openobserve/openobserve)).
- **Is anomaly detection free? No**.
  - Anomaly Detection is "**available in OpenObserve Enterprise (self-hosted)**"
    ([docs](https://openobserve.ai/docs/user-guide/analytics/alerts/anomaly-detection/)). It
    uses [Random Cut Forest](https://github.com/aws/random-cut-forest-by-aws), a tree-based
    anomaly detection method, on any stream of logs, metrics or traces. The training window is
    ≥1 day.
  - Also Enterprise only: AI SRE agent, AI assistant, log pattern extraction, correlation of
    logs, metrics and traces, and SSO
    ([enterprise features](https://openobserve.ai/docs/enterprise-setup/enterprise-features/)).
- **Prices:**
  - Self-hosted Enterprise is **free up to 50 GB/day** of ingestion. Above that, the price is
    custom. At 200 GB–2 TB/day we would pay.
  - Cloud: $0.50/GB ingested + $0.01/GB queried. The $0.50 "Includes 30% discount for annual
    commitment" ([pricing](https://openobserve.ai/pricing/)).
- **Fit:** it would replace Elasticsearch as the log store. For a proof of concept (PoC),
  Filebeat could send a copy of the logs to both stores. But at our volume the anomaly
  detection features need an Enterprise license.

### 1.10 Summary: which tools fit iteration 1?

| Tool | Role | Add-on or swap | Anomaly detection / AI free when self-hosted? | Latest (date) | Iteration 1 verdict |
|---|---|---|---|---|---|
| Alertmanager | grouping, inhibition, silences | add-on (already present) | n/a (no ML) | v0.34.0 (2026-08-16) | **Yes: tune** |
| ElastAlert2 | Elasticsearch rules → Alertmanager bridge | add-on | rule-based only | 2.31.0 (2026-07-22) | **Yes: needed on Basic** |
| Keep OSS | deduplication, correlation, incidents across sources | add-on | rules + topology yes; **AI correlation no** | v0.54.3 (2026-09-09) | Optional (iteration 2) |
| Grafana OnCall OSS | on-call | n/a (archived) | n/a | **archived 2026-03-24** | **No** |
| Grafana Cloud IRM | on-call SaaS | SaaS add-on | n/a | SaaS | Only if SaaS is acceptable |
| HolmesGPT | on-demand LLM RCA | add-on | yes (own LLM) | 0.41.0 (2026-09-08) | Iteration 2 (on demand) |
| k8sgpt | Kubernetes object diagnosis | add-on | yes (own LLM) | v0.4.39 (2026-09-14) | Only on Kubernetes |
| Coroot CE | eBPF application monitoring + RCA | parallel stack (own ClickHouse) | ML RCA inspections yes; **AI RCA no** (note 1) | v1.26.0 (2026-09-07) | Pilot on a subset only |
| Robusta Classic | Kubernetes alert enrichment | add-on (Kubernetes) | via HolmesGPT | 0.49.0 (2026-09-08) | Only on Kubernetes |
| SigNoz | full backend | **swap** | **no** (Cloud/Enterprise) | v0.141.1 (2026-09-09) | No |
| OpenObserve | full backend | **swap** | **no** (Enterprise; free ≤50 GB/day) | v1.0.0 (2026-09-11) | No |

Notes:

1. Coroot CE gets AI RCA only through Coroot Cloud: 10 investigations/month for free.

---

## 2. LLM triage: how it works and what it costs

### 2.1 How does the triage pipeline work?

We follow the Coroot pattern (§1.6): statistics pick the evidence, and the LLM only
summarizes it. A detector fires, and Alertmanager groups the alerts. A small triage service
(`triage-svc`) then builds one evidence **bundle** per group, without an LLM. The bundle is a
short text with the alert, the key metrics, logs, traces and recent changes. The service makes
one LLM call on it and posts the answer.

```
detector fires (Prometheus rule / log-template-delta job / ElastAlert2)
  → Alertmanager (group_by service,env; inhibition) → webhook → triage-svc
      ├─ gate: severity ≥ warning, not silenced, not seen in last 30 min (dedup key = AM groupKey)
      ├─ budget check: daily token budget left? else post "triage skipped: budget"
      ├─ gather bundle (deterministic, no LLM):
      │    alert group labels/annotations ............ ≤1k tok
      │    metrics: alerting series + 5–10 related RED/USE series, −60m..+5m,
      │             pre-summarized (min/max/p95, change-point time, vs same time −1d/−7d) ... 2–4k
      │    logs: top-20 log templates by count delta vs baseline + 1–2 redacted samples each ... 2–8k
      │    traces: top-5 error/slow trace IDs + critical-path span summary (Jaeger API) ... 1–3k
      │    changes: deploys/config events in window ..... ≤1k
      │    system prompt + JSON output schema (static, cacheable) ... ~1.5k
      ├─ PII/secret redaction pass (regex + allowlist of fields)
      ├─ LLM call (hard max_tokens=1000), output JSON {summary, hypotheses[{cause, evidence_refs, confidence}], next_checks[]}
      └─ post to Slack thread / Keep incident enrichment / Grafana annotation; log tokens to Prometheus counter
on-demand: "Investigate deeper" button → HolmesGPT (agentic, step-limited)
```

Terms in the diagram:

- `tok` = tokens. A token is a small piece of text. LLM vendors bill per token.
- RED/USE series: [RED](https://grafana.com/blog/2018/08/02/the-red-method-how-to-instrument-your-services/)
  = rate, errors, duration of a service.
  [USE](https://www.brendangregg.com/usemethod.html) = utilization, saturation, errors of a
  resource.
- p95: the 95th [percentile](https://en.wikipedia.org/wiki/Percentile).
- Change-point time: the moment the series changed its level
  ([change detection](https://en.wikipedia.org/wiki/Change_detection)).
- `−1d/−7d`: the same time one day and one week earlier.
- Log templates: log lines with the variable parts (numbers, IDs) masked, so that similar
  lines are counted together.
- PII: personally identifiable information, such as names or email addresses.
- AM groupKey: AM = Alertmanager. `groupKey` is the key that Alertmanager gives each alert
  group.

### 2.2 What do LLM APIs cost per token?

Prices per 1 M tokens, standard tier. Checked 2026-09-14, re-checked 2026-09-15: unchanged.

| Vendor / model | Input | Output | Cache read | Batch | Source |
|---|---|---|---|---|---|
| Google Gemini 2.5 Flash-Lite | $0.10 | $0.40 | $0.01 | $0.05 / $0.20 | [ai.google.dev pricing](https://ai.google.dev/gemini-api/docs/pricing) |
| Google Gemini 3.1 Flash-Lite (shutdown 2027-05-07) | $0.25 | $1.50 | $0.025 | $0.125 / $0.75 | same, [deprecations](https://ai.google.dev/gemini-api/docs/deprecations) |
| Google Gemini 3.8 Flash | $0.75 → **$1.50 from 2027-01-01** | $3.75 → **$7.50** | $0.075 → $0.15 | $0.375 / $1.875 (2026) | same |
| OpenAI gpt-5-nano (older generation, cheapest found) | $0.05 | $0.40 | $0.005 | $0.025 / $0.20 | [developers.openai.com pricing](https://developers.openai.com/api/docs/pricing) |
| OpenAI gpt-5.6-luna (small) | $0.20 | $1.20 | $0.02 | $0.10 / $0.60 | same |
| OpenAI gpt-5.6-terra (mid) | $2.00 | $12.00 | $0.20 | $1.00 / $6.00 | same |
| Anthropic Claude Haiku 4.5 | $1.00 | $5.00 | $0.10 | −50% | [claude.com/pricing](https://claude.com/pricing) |
| Anthropic Claude Sonnet 5 | $2.00 | $10.00 | $0.20 | −50% | same |

Two columns need a definition:

- **Cache read** is the price for input that the vendor has cached from an earlier call with
  the same start.
- **Batch** is the input / output price when results come back later, not at once.

Pricing notes:

- Gemini 3.8 Flash has promotional pricing "through December 31, 2026".
- Claude Sonnet 5's $2 / $10 was an introductory price until 2026-08-31. The pricing page now
  calls it the standard price. The planned rise to $3 / $15 will not happen
  ([pricing](https://platform.claude.com/docs/en/about-claude/pricing), 2026-09-15).
- OpenAI charges 10% more for data-residency endpoints on models released since 2026-03-05.
  These endpoints keep the data in one region.
- Anthropic charges 1.1× for inference in the US only (`inference_geo: "us"`) on Claude 4.6
  and later models. So it applies to Sonnet 5, but not to Haiku 4.5.
- Anthropic's cache-read price is the same for both cache lifetimes: 5 min and 1 h (time to
  live, TTL). Writing to the cache costs 1.25× the base input price for 5 min, or 2× for 1 h.
- Claude 4.7 and later models, Sonnet 5 included, use a newer tokenizer (the part that splits
  text into tokens). It "produces approximately 30% more tokens for the same text"
  ([pricing](https://platform.claude.com/docs/en/about-claude/pricing)). §2.3 counts tokens.
  So the same bundle on Sonnet 5 can cost up to about 30% more than shown. At 500 large (L)
  bundles/day that is $750 → about $975/month.
- Gemini 3.1 Flash-Lite shuts down on 2027-05-07. Its listed replacement, Gemini 3.5
  Flash-Lite, costs $0.30 / $2.50. Gemini 2.5 Flash-Lite has no announced shutdown date.
- gpt-5-nano is the cheapest text model on the three vendors' pages. It sets the price floor
  that §2.5 uses. It is not in the §2.3 table: it is an older generation, and we did not test
  its quality. In the three §2.3 scenarios it would cost $0.67, $5.52 and $21 per month.
- All three vendors give −50% for batch processing.

### 2.3 What does triage cost per month?

Assumptions:

- Bundle sizes in tokens: **S** = 5k in / 0.5k out, **M** = 12k in / 0.8k out, **L** = 20k
  in / 1k out.
- 30 days/month.
- One bundle per Alertmanager group, not per alert.
- We apply no caching discount. Caching the static prefix of about 1.5k tokens would cut the
  input cost by about 10% on M bundles.
- The batch column is L bundles at 500/day on the Batch API. It fits only digests that do not
  page anyone.

| Model | $/bundle S | $/bundle M | $/bundle L | **50/day × S** | **200/day × M** | **500/day × L** | 500/day × L, batch |
|---|---|---|---|---|---|---|---|
| Gemini 2.5 Flash-Lite | $0.0007 | $0.0015 | $0.0024 | **$1.05/mo** | **$9/mo** | **$36/mo** | $18/mo |
| OpenAI gpt-5.6-luna | $0.0016 | $0.0034 | $0.0052 | $2.40 | $20 | $78 | $39 |
| Gemini 3.1 Flash-Lite | $0.0020 | $0.0042 | $0.0065 | $3.00 | $25 | $98 | $49 |
| Gemini 3.8 Flash (2026 promo) | $0.0056 | $0.0120 | $0.0187 | $8.44 | $72 | $281 | $141 |
| Gemini 3.8 Flash (2027 price) | $0.0112 | $0.0240 | $0.0375 | $16.88 | $144 | $563 | $281 |
| Claude Haiku 4.5 | $0.0075 | $0.0160 | $0.0250 | $11.25 | $96 | $375 | $188 |
| Claude Sonnet 5 | $0.0150 | $0.0320 | $0.0500 | $22.50 | $192 | $750 | $375 |
| OpenAI gpt-5.6-terra | $0.0160 | $0.0336 | $0.0520 | $24.00 | $202 | $780 | $390 |

**For comparison: on-demand agent investigations with HolmesGPT**. With the average benchmark
cost per investigation (§1.4), 20 deep investigations/day cost:

- haiku-4.5 ($0.06): ≈ **$36/month**.
- sonnet-4.6 ($0.18): ≈ **$108/month**.
- opus-4.6 ($0.32): ≈ **$192/month**.
- Current models from the 2026-08-05 run, per month:
  - gpt-5.6-luna ($0.01): ≈ $6.
  - gpt-5.6-terra ($0.07): ≈ $42.
  - sonnet-5 ($0.12): ≈ $72.
  - opus-5 ($0.39): ≈ $234.

These are benchmark tasks, mostly about Kubernetes. Real cost grows with the size of the
tool output. So set limits on steps and on tool output.

### 2.4 Is a small self-hosted model (7–14B) on one GPU cheaper?

7–14B means a model with 7 to 14 billion parameters. Prices below: AWS us-east-1, Linux,
on-demand, from the price file published on 2026-09-10.

| Instance | $/h | $/month 24×7 (730 h) | $/month 12 h/day |
|---|---|---|---|
| g4dn.xlarge (1× T4 16 GB) | $0.526 | $384 | $189 |
| **g6.xlarge (1× L4 24 GB)** | **$0.8048** | **$588** | $290 |
| g5.xlarge (1× A10G 24 GB) | $1.006 | $734 | $362 |
| g6e.xlarge (1× L40S 48 GB, note 1) | $1.861 | $1,359 | $670 |
| c7i.4xlarge (16 virtual CPUs, no GPU) | $0.714 | $521 | $257 |

Note 1: the L40S is for 14–32B models in FP8 (8-bit floating point), or for long contexts.

Source: [AWS on-demand price file](https://b0.p.awsstatic.com/pricing/2.0/meteredUnitMaps/ec2/USD/current/ec2-ondemand-without-sec-sel/US%20East%20%28N.%20Virginia%29/Linux/index.json).
Savings plans and spot instances cost less. We do not quote them here.

**How fast is it? An estimate** for an 8B model on vLLM, with FP8 weights (about 8 GB) on an
L4. These are ESTIMATES from the specs, not measurements:

- L4 specs, from NVIDIA's [L4 page](https://www.nvidia.com/en-us/data-center/l4/): 300 GB/s
  memory bandwidth, 242 TFLOPS at 16-bit precision (FP16) and 485 TFLOPS at FP8. TFLOPS =
  trillion floating-point operations per second. These figures assume sparsity, a speed-up
  for weights with many zeros. The page footnote says "Specifications are one-half lower
  without sparsity". So the dense figures are about 121 FP16 / 242 FP8.
- Decode (writing the answer) for one request at a time is limited by memory bandwidth. It
  reaches at most about 37 tokens/s in theory, and about 20–30 tokens/s in practice.
- Prefill means reading the prompt. For a 20k-token bundle it needs ≈ 2 × 8e9 × 20k = 3.2e14
  floating-point operations (FLOP) → about 3–7 s.
- ⇒ One L bundle takes about 40–60 s, one request at a time. 500 L bundles/day ≈ 6–8
  GPU-hours/day, even without batching. So **one GPU is enough**.
- A measured reference: Qwen3-8B in FP16 on one A10 gives 851 tokens/s in total with 64
  requests at once, on short prompts
  ([TrueFoundry, 2026-07-28](https://www.truefoundry.com/blog/vllm-benchmark)).
- The key-value (KV) cache holds the model's working state for the prompt. For 20k tokens on
  an 8B model with [grouped-query attention](https://arxiv.org/abs/2305.13245) (GQA), it is
  about 3 GB. This is an estimate. So it fits in 24 GB.

**Monthly cost:** about $290–$588 for the L4, at any volume.

- It costs **more** than every API scenario at 50 S/day and 200 M/day ($1–$202).
- At 500 L/day it costs about the same as Haiku 4.5 ($375). Gemini 3.8 Flash at 2027 prices
  is close too ($563).
- It is clearly cheaper only than Sonnet 5 and gpt-5.6-terra at 500 L/day ($750–$780).

**CPU only:** in the cloud, a c7i.4xlarge ($521/mo) costs about the same as an L4. It is
roughly 10–50× slower at reading prompts. Here is a rough estimate for an 8B model with 4-bit
weights (Q4) in llama.cpp on 16 virtual CPUs. A 20k-token prefill takes minutes. Decode runs at
about 5–15 tokens/s. This is UNVERIFIED: we found no benchmark. It works only for up to about
50 bundles/day, not in real time, on idle hardware **that we already own on-premises**. It is
not for triage on the paging path.

**Quality warning:** in HolmesGPT's own benchmarks, some open models fail completely at tool
calling (§1.4). No results exist for 7–14B models. So use small self-hosted models only to
**summarize a pre-built bundle in one call**. Do not use them for agent investigations.

### 2.5 Why can't an LLM read all raw logs?

Assumption: about 4 bytes per token for log text that is mostly English. This is an
UNVERIFIED estimate. JSON and hex IDs split into more tokens, so real counts would be higher.

| Volume | Tokens/day | gpt-5-nano batch ($0.025/M) | Gemini 2.5 Flash-Lite batch ($0.05/M) | Flash-Lite standard ($0.10/M) | gpt-5.6-luna ($0.20/M) | Haiku 4.5 ($1/M) | Sustained rate needed |
|---|---|---|---|---|---|---|---|
| 200 GB/day | 50 billion | $1,250/day ≈ **$37.5k/mo** | $2,500/day ≈ $75k/mo | $150k/mo | $300k/mo | $1.5M/mo | ~0.58 M tokens/s |
| 2 TB/day | 500 billion | $12,500/day ≈ **$375k/mo** | $25,000/day ≈ $750k/mo | $1.5M/mo | $3M/mo | $15M/mo | ~5.8 M tokens/s |

The table counts input only and ignores output. Self-hosting does not help: about 5.8 M
tokens/s ÷ about 3–6k prefill tokens/s per L4 (estimate) ≈ 1,000–2,000 L4s ≈
**$0.6–1.2 M/month**.

Compare the bundle approach at **$1–$780/month**. It is **roughly 3–6 orders of magnitude
cheaper**: from ≈ $375k ÷ $780 ≈ 480× up to ≈ $750k ÷ $1.05 ≈ 710,000×. The reason: detectors
reduce the data before the LLM sees it. These detectors are Prometheus rules, log-template
counting and Elasticsearch aggregations.

### 2.6 Which budget limits are required?

1. **Hard limits in two places:**
   - A monthly spend limit at the vendor, set in its console.
   - A daily token budget in our app (= monthly limit / 30). Count the tokens in a Prometheus
     counter, for example `llm_tokens_total{model,direction}`. Add a
     [burn-rate](https://sre.google/workbook/alerting-on-slos/) alert: it fires when we use
     the budget too fast.
2. **Limits per bundle:** at most 20k input tokens. Cut the lowest-priority sections first:
   extra log samples, then traces. Output: `max_tokens` = 1000.
3. **Rate and deduplication:** one bundle per Alertmanager `groupKey` per 30 min. Triage again
   only when something important changes: a new alertname in the group, or a higher
   severity. Allow at most N bundles/day (for example 500). After that, continue with "no
   LLM".
4. **Limits for agent investigations:** run HolmesGPT only on demand (a person clicks), at most
   20/day, with a limit on steps and tool calls.
5. **Model ladder:** use a cheap model by default (Flash-Lite, gpt-5.6-luna or Haiku 4.5).
   Move up to a mid-range model, Sonnet 5 or terra, only when someone asks for it. Also move
   up for priority-1 (P1) incidents.
6. **Data:** remove PII and secrets before anything leaves our network. Prefer vendors or
   settings that do not train on API data. For example, the Gemini free tier says "Used to
   improve our products: Yes", and the paid tier says "No"
   ([pricing page](https://ai.google.dev/gemini-api/docs/pricing)).

---

## 3. What would commercial products cost? (public list prices only)

### 3.1 Unit prices

| Vendor | Logs | Metrics | Anomaly detection / ML | Source |
|---|---|---|---|---|
| **Elastic self-managed** | n/a (quote-only) | n/a (quote-only) | ML = **Platinum+**, **quote-only** (note 1) | [self-managed](https://www.elastic.co/pricing/self-managed), [subscriptions](https://www.elastic.co/subscriptions) |
| **Elastic Cloud Serverless Observability** | graduated ingest, $0.50 → $0.0925/GB (note 2) | metrics at 25 % of the log prices (note 3) | ML in **Complete** only (note 4) | [serverless pricing](https://www.elastic.co/pricing/serverless-observability), [price table](https://cloud.elastic.co/cloud-pricing-table?productType=serverless), [estimator](https://cloud.elastic.co/pricing/serverless), [billing dims](https://www.elastic.co/docs/deploy-manage/cloud-organization/billing/elastic-observability-billing-dimensions) |
| **Grafana Cloud Pro** | graduated Process + Write, 50 GB free, $19/month fee (note 5) | $6.50 → $5.50 per 1k series. **15 s scrape = 4 data points/min (DPM) = 4× bill** (note 6) | Grafana ML **free in all Cloud tiers**, with limits (note 7) | [pricing](https://grafana.com/pricing/), [logs invoice](https://grafana.com/docs/grafana-cloud/cost-management-and-billing/manage-invoices/understand-your-invoice/logs-invoice/), [metrics invoice](https://grafana.com/docs/grafana-cloud/cost-management-and-billing/manage-invoices/understand-your-invoice/metrics-invoice/), [ML limits](https://grafana.com/docs/grafana-cloud/ai-tools/machine-learning/additional-configuration/limits/) |
| **Datadog** | ingest $0.10/GB + index **$1.70** per 1M events, 15 days (note 8) | custom metrics **$5 per 100/month** (note 9) | **Watchdog built-in** (note 10) | [list pricing](https://www.datadoghq.com/pricing/list/), [custom metrics](https://docs.datadoghq.com/account_management/billing/custom_metrics/), [Watchdog](https://docs.datadoghq.com/watchdog/alerts/) |
| SigNoz Cloud Teams (reference) | $0.30/GB (15-day retention) | $0.10 per M samples | anomaly alerts only in Cloud/Enterprise | [pricing](https://signoz.io/pricing/) |
| OpenObserve Cloud (reference) | $0.50/GB ingest + $0.01/GB queried (note 11) | same per-GB price | Enterprise only, "not available in Cloud" | [pricing](https://openobserve.ai/pricing/), [anomaly detection docs](https://openobserve.ai/docs/user-guide/analytics/alerts/anomaly-detection/) |

Notes:

1. **Elastic self-managed:** ML anomaly detection and log categorization need **Platinum** or
   higher. Platinum is "*Existing customers only*". Enterprise is "Contact us". The page does
   not state the pricing unit. It may be ERUs, a license unit based on RAM: UNVERIFIED.
2. **Elastic Serverless logs (Complete tier)**. Prices from the price table as of 2026-09-14,
   AWS us-east-1:
   - Ingest is graduated per GB/month: $0.50 (0–1.5k GB), $0.325, $0.225 (3k–6k), $0.15,
     $0.105, $0.10, $0.095, and $0.0925 above 150k GB. The page's "as low as $0.09" is that
     last marginal rate.
   - Retention: $0.040 (0–10k GB-month) down to $0.0188.
   - Logs Essentials: $0.35 → $0.0648 ingest.
   - Billing meters the enriched size. The estimator uses ≈ 1.66× the raw size.
3. **Elastic Serverless metrics:** since 2026-07-01, Complete has its own price items for
   time series data stream (TSDS) metrics. They cost exactly 25 % of the log bands: $0.125 →
   $0.0231 ingest, $0.010 → $0.0047 retention.
4. **Elastic Serverless ML** is included in **Complete** only. The managed LLM add-on costs
   $4.50/$21 per M tokens.
5. **Grafana Cloud Pro logs** are graduated:
   - Process: $0.050 (to 10k GB) / $0.046 (to 25k) / $0.044.
   - Write: $0.400 (to 1k GB) / $0.370 (to 2.5k) / $0.355.
   - 50 GB free, and a $19/month platform fee.
   - The pricing page and its calculator add a third line on every GB: Retain, $0.100 /
     $0.092 / $0.088 per GB. But the
     [logs billing doc](https://grafana.com/docs/grafana-cloud/platform/pricing-and-usage/logs/)
     says 30 days are included, and it charges $0.10/GB per extra 30 days (checked
     2026-09-15).
6. **Grafana Cloud Pro metrics:** $6.50/1k series (10k–100k) → $5.90 (100k–200k) → $5.50
   (200k+). Billable = max(active series, total DPM / 1). DPM means data points per minute.
   So a **15 s scrape = 4 DPM = 4× bill**.
7. **Grafana ML** (forecast, outlier, Sift) is **free in all Cloud tiers**. By default it is
   limited to 10 forecasts × 100 series and 10 outlier detectors × 1,000 series. Grafana
   raises these limits on request. Enterprise: ≥$25k/yr commit, and metrics "as low as $3/1k".
8. **Datadog logs:** ingest $0.10/GB. The index price per 1M events/month (annual) depends on
   retention: 3 days $1.06, 7 days $1.27, **15 days $1.70**, 30 days $2.50. On-demand, 15 days
   costs $2.55. Flex storage: $0.05/M events.
9. **Datadog custom metrics:** **$5 per 100/month**. The allotment is 100 per host on Pro, or
   200 per host on Enterprise. A custom metric = metric name + tag values.
10. **Datadog Watchdog** is built in. It analyzes **ingested** logs at intake, plus
    application performance monitoring (APM) and infrastructure. Infra Pro: $15/host,
    Enterprise: $23/host (annual).
11. **OpenObserve:** the $0.50 "Includes 30% discount for annual commitment".

### 3.2 Monthly cost at our volume

Assumptions:

- 30-day month. 1 GB = 10^9 bytes.
- Scenario **A** = 200 GB/day of logs (6,000 GB/month) + 1 M series.
- Scenario **B** = 2 TB/day (60,000 GB/month) + 10 M series.
- Tiers are graduated. We confirmed this with the Grafana and Elastic calculators on
  2026-09-15. [cost_model.py](../cost_model.py) recomputes every figure below and prints the
  formulas. The only exceptions are the two rows marked "rough estimate, not in
  cost_model.py".
- Datadog: a log event is 1 KB on average. So there are 6 billion events/month in A, and 60
  billion in B.
  All logs are indexed with 15-day retention. 100 hosts on Infra Pro. We do not know the host
  count, but hosts are a small share of the bill.
- Every vendor uses a 60 s scrape interval unless noted. For Elastic metric GB we assume
  30–100 bytes per sample (UNVERIFIED).
- No total includes support fees or egress.

| Vendor | Scenario A | Scenario B | What drives it |
|---|---|---|---|
| **Grafana Cloud Pro**, 60 s scrape | logs $2.5k–$3.1k + metrics $5.6k + $19 ≈ **$8.1k–$8.7k/mo** | logs $24.1k–$29.4k + metrics $55.1k ≈ **$79k–$85k/mo** | metrics (logs range = the Retain line) |
| Grafana Cloud Pro, 15 s scrape (4 DPM) | $2.5k–$3.1k + $22.1k ≈ **$24.6k–$25.2k/mo** | $24.1k–$29.4k + $220k ≈ **$244k–$250k/mo** | DPM multiplier |
| Grafana Cloud Enterprise ("as low as" $3/1k, 60 s) | metrics ≈ $3k + logs (custom) | metrics ≈ $30k + logs (custom) | negotiated. Rough estimate, not in cost_model.py |
| **Datadog** (ingest + 15-day index + custom metrics + 100 hosts) | $600 + $10.2k + $49.5k + $1.5k ≈ **$62k/mo** | $6k + $102k + $499.5k + $1.5k ≈ **$609k/mo** | custom metrics (1 M series ≈ $50k/mo at list) |
| Datadog logs only, ingest + Flex storage (Watchdog on ingested logs) | $600 + $300 ≈ **$0.9k/mo** (+ Flex compute, UNVERIFIED) | $6k + $3k ≈ **$9k/mo** (+ Flex compute) | cheap path, but a pipeline swap. Rough estimate, not in cost_model.py |
| **Elastic Serverless Complete** (graduated) | ingest $1.9k + retention $240 + metrics $175 ≈ **$2.3k–$3.5k/mo** (note 1) | ingest $7.8k + retention $1.9k + metrics $0.9k ≈ **$10.6k–$17.4k/mo** (note 1) | graduated bands (note 2) |
| **Elastic self-managed Platinum/Enterprise** | quote-only | quote-only | license + our own hardware (already paid for) |
| SigNoz Cloud Teams (logs only) | $1.8k/mo | $18k/mo | per-GB ingest price |
| OpenObserve Cloud (logs ingest only, annual-commit rate) | $3k/mo | $30k/mo | per-GB ingest price |

Notes:

1. Elastic: the sum of logs ingest, retention and metrics is the low end. The high end uses
   1.66× metering and 100 bytes per sample: up to ≈ $3.5k in A, and up to ≈ $17.4k in B.
2. Elastic: the "as low as" rate starts only at 150k GB/month. The earlier estimate applied
   that floor rate to every GB: ≈ $0.7–0.9k in A, $7–9k in B.

Takeaways:

- Among public prices, the cheapest managed product for logs + metrics with anomaly detection
  included is Elastic Serverless Complete (≈ $2.3k–$17k/month). But it means a migration off
  self-managed Elasticsearch.
- Grafana Cloud's ML is free, but by default it covers only about 11k series across all
  detectors. Grafana raises it on request. Even so, it cannot cover 1–10 M series.
- Most of the Datadog bill comes from Prometheus series, because Datadog bills them as custom
  metrics.
- All of these cost roughly 1–4 orders of magnitude more than our whole OSS add-on plan.
  Examples: Elastic Serverless at A ≈ 23×, Datadog at B ≈ 6,000×. The plan is Alertmanager +
  ElastAlert2 + Keep + HolmesGPT, plus LLM tokens capped at $100/month. It costs engineering
  time and some compute, not licenses.

---

## 4. What do we recommend?

**Iteration 1: the correlation layer (weeks 1–4, $0 for licenses)**

1. **Alertmanager is the single alert bus:**
   - Run an HA pair.
   - Require the same labels everywhere: `service`, `env`, `cluster`, `team`,
     `signal={metric,log,trace}`.
   - Grouping: `group_by: [service, env]`, `group_wait` 30 s, `group_interval` 5 min.
   - **Inhibition rules:** when a node, the network or a database is down, hold back the
     symptom alerts of the apps. While a "deploy in progress" alert fires, hold back latency
     warnings.
   - Create maintenance silences through the API, from continuous integration (CI).
2. **ElastAlert2 → Alertmanager** for every detection on the Elasticsearch side, because
   Kibana Basic cannot send webhooks. Examples: changes in log rate, spikes of error
   templates, and anomaly results from the pipeline in
   [file 01](01-logs-elastic-basic.md).
3. **Grafana OSS alerting** sends to the same external Alertmanager. Alerts built on Jaeger
   data also go through Alertmanager. An example is the error rate from
   [span metrics](https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/v0.160.0/connector/spanmetricsconnector/README.md)
   in Prometheus.
4. **Skip Grafana OnCall OSS**, because it is archived. Page through Alertmanager receivers.
5. **Optional (iteration 2): Keep OSS**. Adopt it only if grouping by labels is not enough. For
   example, we may need:
   - incident objects that span metric, log and trace alerts,
   - topology correlation, or
   - workflow enrichment.

   Use only rule-based and topology correlation. Plan without AI correlation, because it is
   not OSS. Use OAuth2Proxy for SSO.

**When to add LLM triage: iteration 2**, once:

- the detectors are stable,
- we know the false-positive rate, and it is below about 30% of pages, and
- there are fewer than about 500 grouped incidents/day.

Start **on demand only**: a Slack button runs HolmesGPT with the Prometheus and Elasticsearch
toolsets, plus a custom Jaeger REST toolset. Evaluate it for 2–4 weeks on ≥50 past incidents.
Then turn on **automatic triage for P1/P2 groups** (priority 1 and 2), with the deterministic
bundle from §2.1.

**Budget limits (start values):**

- A hard limit of $100/month at the vendor → about $3.3/day budget in our app. That covers
  about 200 M bundles/day on Haiku 4.5, or all 500 L bundles/day on gpt-5.6-luna or Gemini
  Flash-Lite.
- At most 20 agent investigations/day.
- Alert at 80% of the daily budget. Raise the limit only with evidence: a change in
  [mean time to recovery](https://en.wikipedia.org/wiki/Mean_time_to_recovery) (MTTR) or in
  triage time.

**Self-hosted or API? The decision rule:**

- **Default: an API, cheap tier**. At 50–500 bundles/day it costs $1–$100/month with small
  models. With top-tier models at 500 L/day it costs ≤$800/month. On Sonnet 5 that can reach
  about $1k, once its larger tokenizer is counted (§2.2).
- **Self-host** only if **any** of these is true:
  - A policy does not allow log or trace snippets to leave the network, even redacted.
  - Steady API spend is above the cost of about 1 GPU-month, that is, about $600/month.
  - There are more than about 5k bundles/day.

  Self-hosting means 1× L4 ≈ $290–$588/month on-demand, or a GPU that we already own
  on-premises.
- Even then, use the self-hosted 7–14B model only for **bundle summaries in one call**. Keep
  agent-style RCA on a larger model, because small models are unproven at tool calling
  (§1.4).
- **Never** send raw log streams to any LLM (§2.5).

---

## What is still UNVERIFIED?

- Elastic self-managed Enterprise: price and pricing unit. It is quote-only, and the page does
  not confirm that it is based on ERUs (RAM).
- Elastic Serverless: the factor between our stated GB/day and the GB that Elastic meters
  after enrichment. Its estimator uses ≈ 1.66× the raw size.
- Datadog Flex Logs: prices of the compute tiers.
- 4 bytes per token for logs.
- Throughput of an 8B model on an L4 (derived from specs), and the speed on CPU only.
- The effort to build a Jaeger toolset for HolmesGPT.
- Coroot sizing when it runs on only some nodes.
- Exact token counts for the same bundle on Sonnet 5 and on Haiku 4.5. The vendor says
  "approximately 30% more".
