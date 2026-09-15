#!/usr/bin/env python3
"""Every US$ figure in the docs and the report, recomputed from list prices.

    python3 docs/cost_model.py          # stdlib only; prints the Markdown tables
    python3 docs/cost_model.py --report # the COST array of report/anomaly-detection-report.html
    python3 docs/cost_model.py --write  # rewrites both in docs/cost-sizing.md and the report

Each price carries its source URL; all were checked on CHECKED. Month = 30 days, 1 GB = 1e9 B.
Tiered prices are graduated: each band is billed at its own rate (vendor calculators confirm this
for Grafana and Elastic). Quote-only prices (Elastic self-managed Platinum/Enterprise) are not modelled.
"""
import math

CHECKED = "2026-09-15"
DAYS = 30
HOURS = 730  # AWS monthly hours (24 × 365 / 12)

SRC = {
    "gemini": "https://ai.google.dev/gemini-api/docs/pricing",
    "openai": "https://developers.openai.com/api/docs/pricing",
    "anthropic": "https://platform.claude.com/docs/en/about-claude/pricing",
    "aws": "https://b0.p.awsstatic.com/pricing/2.0/meteredUnitMaps/ec2/USD/current/ec2-ondemand-without-sec-sel/US%20East%20%28N.%20Virginia%29/Linux/index.json",
    "grafana": "https://grafana.com/pricing/",
    "grafana-logs": "https://grafana.com/docs/grafana-cloud/platform/pricing-and-usage/logs/",
    "grafana-metrics": "https://grafana.com/docs/grafana-cloud/platform/pricing-and-usage/metrics/",
    "datadog": "https://www.datadoghq.com/pricing/list/",
    "datadog-cm": "https://docs.datadoghq.com/account_management/billing/custom_metrics/",
    "elastic": "https://cloud.elastic.co/cloud-pricing-table?productType=serverless",
    "elastic-est": "https://cloud.elastic.co/pricing/serverless",
    "signoz": "https://signoz.io/pricing/",
    "openobserve": "https://openobserve.ai/pricing/",
}
INF = math.inf

# --- Unit prices ---------------------------------------------------------------------------------
# LLM APIs, $ per 1M tokens, standard tier: (name, input, output, source). Batch is -50 % at all three.
LLM = [
    ("Gemini 2.5 Flash-Lite", 0.10, 0.40, "gemini"),
    ("OpenAI gpt-5.6-luna", 0.20, 1.20, "openai"),
    ("Gemini 3.1 Flash-Lite (shutdown 2027-05-07)", 0.25, 1.50, "gemini"),
    ("Gemini 3.8 Flash, promo to 2026-12-31", 0.75, 3.75, "gemini"),
    ("Gemini 3.8 Flash, from 2027-01-01", 1.50, 7.50, "gemini"),
    ("Claude Haiku 4.5", 1.00, 5.00, "anthropic"),
    ("Claude Sonnet 5", 2.00, 10.00, "anthropic"),
    ("OpenAI gpt-5.6-terra", 2.00, 12.00, "openai"),
]
NANO_BATCH_IN = 0.025  # OpenAI gpt-5-nano batch input, the cheapest text-model input found (openai)
FLASH_LITE_BATCH_IN = 0.05  # Gemini 2.5 Flash-Lite batch input (gemini)
EC2 = {  # us-east-1, Linux, on-demand $/h, price file published 2026-09-10 (aws): (vCPU, GiB, $/h)
    "g6.xlarge (1× L4 24 GB)": (4, 16, 0.8048),
    "c7i.large": (2, 4, 0.08925),
}
VCPU_MONTH = EC2["c7i.large"][2] / EC2["c7i.large"][0] * HOURS  # $/vCPU-month, compute-optimized

# Grafana Cloud Pro: graduated tiers as (upper bound, $ per unit); 50 GB/month free on each logs line.
GRAFANA_FEE = 19  # platform fee, includes 10k series and 50 GB logs (grafana)
G_PROCESS = [(10_000, 0.050), (25_000, 0.046), (INF, 0.044)]   # $/GB
G_WRITE = [(1_000, 0.400), (2_500, 0.370), (INF, 0.355)]       # $/GB
G_RETAIN = [(5_000, 0.100), (12_500, 0.092), (INF, 0.088)]     # $/GB; billed on every GB by the calculator
G_SERIES = [(10_000, 0), (100_000, 6.50e-3), (200_000, 5.90e-3), (INF, 5.50e-3)]  # $/series; billable =
# max(active series, DPM / 1): a 15 s scrape (4 DPM) bills 4× the series (grafana-metrics)

# Datadog, billed annually (datadog, datadog-cm)
DD_INGEST = 0.10    # $/GB ingested
DD_INDEX15 = 1.70   # $ per 1M log events indexed, 15-day retention
DD_CM = 5 / 100     # $ per custom metric (= time series) per month
DD_CM_PER_HOST = 100  # Pro allotment, pooled
DD_HOST = 15        # Infrastructure Pro, $/host/month

# Elastic Cloud Serverless Observability Complete, AWS us-east-1, graduated per SKU (elastic).
# "As low as $0.09/GB" is the marginal rate above 150,000 GB/month, not an average.
E_INGEST = [(1_500, 0.50), (3_000, 0.325), (6_000, 0.225), (15_000, 0.15), (30_000, 0.105),
            (60_000, 0.10), (150_000, 0.095), (INF, 0.0925)]
E_RETAIN = [(10_000, 0.040), (20_000, 0.032), (50_000, 0.030), (100_000, 0.028), (250_000, 0.026),
            (1_000_000, 0.022), (2_500_000, 0.020), (INF, 0.0188)]
E_M_INGEST = [(1_500, 0.125), (3_000, 0.0813), (6_000, 0.0563), (15_000, 0.0375), (30_000, 0.0263),
              (60_000, 0.025), (150_000, 0.0238), (INF, 0.0231)]  # TSDS metrics SKUs, 25 % of logs
E_M_RETAIN = [(10_000, 0.010), (20_000, 0.008), (50_000, 0.0075), (100_000, 0.007), (250_000, 0.0065),
              (1_000_000, 0.0055), (2_500_000, 0.005), (INF, 0.0047)]
E_METER = (1.0, 1.66)  # metered GB ÷ stated GB: as stated … the estimator's enrichment factor (elastic-est)

SIGNOZ_LOGS = 0.30      # $/GB, 15-day retention (0.40 for 30 days) (signoz)
OPENOBSERVE_LOGS = 0.50  # $/GB, annual-commit rate (openobserve)

# --- Scenarios ----------------------------------------------------------------------------------
SCEN = {"A": (200, 1_000_000), "B": (2_000, 10_000_000)}  # GB/day of logs, active series
SCRAPE_S = 60           # base scrape interval for every vendor; Grafana also at 15 s
LOG_EVENT_B = 1_000     # Datadog: average log event size (assumption)
DD_HOSTS = 100          # Datadog: paid hosts (our host count unknown; hosts are a small share)
SAMPLE_B = (30, 100)    # Elastic metrics: bytes per sample on ingest (assumption, UNVERIFIED)
BUNDLES = {"S": (5_000, 500), "M": (12_000, 800), "L": (20_000, 1_000)}  # tokens in, out
TRIAGE = [("50/day × S", 50, "S"), ("200/day × M", 200, "M"), ("500/day × L", 500, "L")]
BYTES_PER_TOKEN = 4     # raw logs (assumption; JSON and hex ids tokenize worse)


def grad(q, tiers, free=0.0):
    """Graduated price of quantity q; `free` units come out of the first band."""
    cost, lo = 0.0, 0.0
    for hi, rate in tiers:
        if q > lo:
            cost += (min(q, hi) - lo) * rate
        lo = hi
    return cost - min(q, free) * tiers[0][1]


def bundle_cost(pin, pout, size):
    tin, tout = BUNDLES[size]
    return (tin * pin + tout * pout) / 1e6


def grafana(gb_day, series, scrape_s, retain):
    gb = gb_day * DAYS
    logs = grad(gb, G_PROCESS, 50) + grad(gb, G_WRITE, 50) + (grad(gb, G_RETAIN, 50) if retain else 0)
    metrics = grad(series * 60 / scrape_s, G_SERIES)
    return logs, metrics, logs + metrics + GRAFANA_FEE


def datadog(gb_day, series):
    gb = gb_day * DAYS
    parts = (gb * DD_INGEST, gb * 1e9 / LOG_EVENT_B / 1e6 * DD_INDEX15,
             max(0, series - DD_HOSTS * DD_CM_PER_HOST) * DD_CM, DD_HOSTS * DD_HOST)
    return parts, sum(parts)


def elastic(gb_day, series, meter, sample_b):
    gb = gb_day * DAYS * meter
    mgb = series * 60 / SCRAPE_S * 60 * 24 * DAYS * sample_b / 1e9 * meter
    # 30-day retention at steady state: one month of data stored
    parts = (grad(gb, E_INGEST), grad(gb, E_RETAIN), grad(mgb, E_M_INGEST) + grad(mgb, E_M_RETAIN))
    return parts, sum(parts)


def money(v):
    v += 1e-9  # round half up
    if v >= 100_000:
        return f"${v / 1000:,.0f}k"
    if v >= 1_000:
        return f"${v / 1000:,.1f}k"
    return f"${v:,.2f}" if v < 100 else f"${v:,.0f}"


def usd(x):
    if x == int(x):
        return f"${x:g}"
    t = f"{x:.4f}".rstrip("0")
    return "$" + (t if len(t.split(".")[1]) >= 2 else f"{x:.2f}")


def link(key):
    return f"[{key}]({SRC[key]})"


def main():
    """Markdown sections by name: llm (for §6), prices (for §8)."""
    out = {"llm": [], "prices": []}
    cur = "prices"

    def P(*a):
        out[cur].append(" ".join(str(x) for x in a))

    P(f"Prices checked {CHECKED}. `python3 docs/cost_model.py --write` generates this block. "
      "Edit the prices in that file, not here.\n")

    P("### Unit prices\n")
    P("| Item | Price | Source |\n|---|---|---|")
    for name, pin, pout, src in LLM:
        P(f"| {name} | {usd(pin)} in / {usd(pout)} out per 1M tokens | {link(src)} |")
    P(f"| OpenAI gpt-5-nano, batch | {usd(NANO_BATCH_IN)} in per 1M tokens (cheapest input found) | {link('openai')} |")
    P(f"| Gemini 2.5 Flash-Lite, batch | {usd(FLASH_LITE_BATCH_IN)} in per 1M tokens | {link('gemini')} |")
    for name, (vcpu, gib, ph) in EC2.items():
        P(f"| AWS {name}, us-east-1 on-demand | ${ph:g}/h ({vcpu} vCPU, {gib} GiB) | {link('aws')} |")
    P(f"| Grafana Cloud Pro logs | process $0.050→0.046→0.044, write $0.400→0.370→0.355, retain "
          f"$0.100→0.092→0.088 per GB; 50 GB free; ${GRAFANA_FEE}/month platform fee | {link('grafana')}, {link('grafana-logs')} |")
    P(f"| Grafana Cloud Pro metrics | $6.50 / $5.90 / $5.50 per 1k series (10k–100k / 100k–200k / 200k+); "
          f"10k included; billable = max(series, DPM ÷ 1) | {link('grafana')}, {link('grafana-metrics')} |")
    P(f"| Datadog | ingest {usd(DD_INGEST)}/GB; index ${DD_INDEX15:.2f} per 1M events (15 days); custom metrics "
          f"$5 per 100; {DD_CM_PER_HOST} per host included; Infra Pro ${DD_HOST}/host (annual) | {link('datadog')}, {link('datadog-cm')} |")
    P(f"| Elastic Serverless Observability Complete | ingest $0.50→…→$0.0925 per GB (8 bands, 0–150k+ GB); "
          f"retention $0.040→…→$0.0188 per GB-month; TSDS metrics at 25 % of both | {link('elastic')} |")
    P(f"| SigNoz Cloud Teams logs | {usd(SIGNOZ_LOGS)}/GB (15 days) | {link('signoz')} |")
    P(f"| OpenObserve Cloud logs | {usd(OPENOBSERVE_LOGS)}/GB (annual commit) | {link('openobserve')} |")

    cur = "llm"
    P("Formula: `bundles/day × 30 × (tokens_in × $in + tokens_out × $out) / 1e6`. "
          "Bundle sizes in tokens: " + ", ".join(f"{k} = {i // 1000}k in / {o / 1000:g}k out" for k, (i, o) in BUNDLES.items()) + ".\n")
    P("| Model | " + " | ".join(s for s, _, _ in TRIAGE) + " | 500/day × L, batch |\n|---|---|---|---|---|")
    for name, pin, pout, _ in LLM:
        cells = [money(n * DAYS * bundle_cost(pin, pout, b)) for _, n, b in TRIAGE]
        cells.append(money(500 * DAYS * bundle_cost(pin, pout, "L") / 2))
        P(f"| {name} | " + " | ".join(cells) + " |")
    for label, (_, _, ph) in [("g6.xlarge 24×7", EC2["g6.xlarge (1× L4 24 GB)"]),
                              ("g6.xlarge 12 h/day", EC2["g6.xlarge (1× L4 24 GB)"])]:
        h = HOURS if "24" in label else 12 * DAYS
        P(f"| Self-hosted 8B, {label} (`${ph}/h × {h} h`) | " + " | ".join([money(ph * h)] * 4) + " |")

    cur = "prices"
    P("\n### Raw logs through an LLM (input only)\n")
    P(f"Formula: `GB/day × 1e9 ÷ {BYTES_PER_TOKEN} B/token × $/1M ÷ 1e6 × 30`.\n")
    P("| Logs | Tokens/day | gpt-5-nano batch | Gemini 2.5 Flash-Lite batch |\n|---|---|---|---|")
    for k, (gbd, _) in SCEN.items():
        tok = gbd * 1e9 / BYTES_PER_TOKEN
        P(f"| {gbd:,} GB/day | {tok / 1e9:,.0f} billion | {money(tok / 1e6 * NANO_BATCH_IN * DAYS)} | "
              f"{money(tok / 1e6 * FLASH_LITE_BATCH_IN * DAYS)} |")

    P("\n### Managed products at list price\n")
    P(f"A = {SCEN['A'][0]} GB/day of logs + {SCEN['A'][1] / 1e6:g} M series. "
          f"B = {SCEN['B'][0]:,} GB/day + {SCEN['B'][1] / 1e6:g} M series. The totals exclude support, egress and discounts.\n")
    P("| Product | Formula | A / month | B / month |\n|---|---|---|---|")
    rows = []
    for scrape in (60, 15):
        lo = [grafana(*SCEN[k], scrape, False) for k in "AB"]
        hi = [grafana(*SCEN[k], scrape, True) for k in "AB"]
        rows.append((f"Grafana Cloud Pro, {scrape} s scrape",
                     "$19 + logs (process + write; + retain per the calculator) + metrics on "
                     f"{60 // scrape}× series, all graduated",
                     [f"{money(a[2])}–{money(b[2])}" for a, b in zip(lo, hi)], lo[0][2]))
    dd = [datadog(*SCEN[k]) for k in "AB"]
    rows.append(("Datadog (logs indexed 15 days, custom metrics, 100 hosts)",
                 "GB × $0.10 + events × $1.70/M + (series − 10k) × $0.05 + 100 × $15",
                 [money(t) for _, t in dd], dd[0][1]))
    el = [[elastic(*SCEN[k], m, s)[1] for m, s in ((E_METER[0], SAMPLE_B[0]), (E_METER[1], SAMPLE_B[1]))]
          for k in "AB"]
    rows.append(("Elastic Serverless Complete",
                 "graduated ingest + one month retained, logs and TSDS metrics; GB × 1.0–1.66 (metering)",
                 [f"{money(a)}–{money(b)}" for a, b in el], el[0][0]))
    rows.append(("SigNoz Cloud Teams, logs only", "GB × $0.30",
                 [money(SCEN[k][0] * DAYS * SIGNOZ_LOGS) for k in "AB"], 0))
    rows.append(("OpenObserve Cloud, logs only", "GB × $0.50",
                 [money(SCEN[k][0] * DAYS * OPENOBSERVE_LOGS) for k in "AB"], 0))
    for name, formula, cells, _ in rows:
        P(f"| {name} | {formula} | {cells[0]} | {cells[1]} |")

    P("\nCost components, per scenario:\n")
    for k in "AB":
        gbd, series = SCEN[k]
        lo, hi = grafana(gbd, series, 60, False), grafana(gbd, series, 60, True)
        (ing, idx, cm, host), _ = datadog(gbd, series)
        e = elastic(gbd, series, 1.0, SAMPLE_B[0])[0]
        P(f"- Scenario {k}:\n  - Grafana: logs {money(lo[0])}–{money(hi[0])}, metrics {money(lo[1])}\n"
              f"  - Datadog: ingest {money(ing)}, index {money(idx)}, custom metrics {money(cm)}, hosts {money(host)}\n"
              f"  - Elastic (as stated, 30 bytes per sample): logs ingest {money(e[0])}, retention {money(e[1])}, metrics {money(e[2])}")

    P(f"\n### Compute for our open-source add-ons, at list price\n")
    P(f"One vCPU-month = c7i.large ${EC2['c7i.large'][2]}/h ÷ 2 vCPU × {HOURS} h = {money(VCPU_MONTH)}. "
          "Core counts come from §2: peak lines/s ÷ measured lines/s per core. We measured the cores on a laptop, not on an EC2 vCPU.\n")
    P("Low end: 69k lines/s (2 TB/day, 1 KB lines, 3× peak) at the measured synthetic speed. High end: 231k "
      "lines/s (300 B lines), with real logs 5× slower than synthetic. The 5× is an assumption. For Drain3, the "
      "high end uses its low speed anchor instead: the measured Loghub-2.0 speed.\n")
    P("| Log template mining at 2 TB/day | Measured lines/s per core | vCPU | $/month |\n|---|---|---|---|")
    for name, rate, slow in (("anomalyd agent (CRI container logs)", 1.5e6, 1.5e6 / 5),
                             ("OTel `drain` path (CRI container logs)", 92e3, 92e3 / 5),
                             ("Drain3 exporter (Python)", 100e3, 10e3)):
        lo_c, hi_c = 69e3 / rate, 231e3 / slow
        P(f"| {name} | {rate / 1e3:,.0f}k synthetic | {lo_c:.2f}–{hi_c:.1f} | ${lo_c * VCPU_MONTH:,.0f}–${hi_c * VCPU_MONTH:,.0f} |")
    return {k: "\n".join(v).strip() for k, v in out.items()}


def report_cost():
    """The COST array of report/anomaly-detection-report.html (ascending: bars render in order)."""
    def bundle(n, size, name):
        _, pin, pout, src = next(m for m in LLM if m[0] == name)
        tin, tout = BUNDLES[size]
        return n * DAYS * bundle_cost(pin, pout, size), f"{n} × 30 × ({tin:,} × {usd(pin)} + {tout:,} × {usd(pout)}) ÷ 1M", SRC[src]
    gpu = EC2["g6.xlarge (1× L4 24 GB)"][2]
    mining = 231e3 / (1.5e6 / 5) * VCPU_MONTH
    rows = [
        ("LLM triage, 50 small bundles/day", "Gemini 2.5 Flash-Lite", "oss", *bundle(50, "S", "Gemini 2.5 Flash-Lite")),
        ("Log template mining CPU, 2 TB/day peak", "anomalyd agents, ≤ 0.8 vCPU", "oss", mining,
         f"231k lines/s ÷ (1.5M ÷ 5) lines/s per vCPU × ${EC2['c7i.large'][2]}/h ÷ 2 × {HOURS} h (c7i.large)", SRC["aws"]),
        ("LLM triage, 200 medium bundles/day", "Claude Haiku 4.5", "oss", *bundle(200, "M", "Claude Haiku 4.5")),
        ("Self-hosted 8B model, 1× L4, 24×7", "AWS g6.xlarge on-demand", "oss", gpu * HOURS, f"${gpu}/h × {HOURS} h", SRC["aws"]),
        ("LLM triage, 500 large bundles/day", "Claude Sonnet 5", "oss", *bundle(500, "L", "Claude Sonnet 5")),
    ]
    for k, size in (("A", "200 GB/day"), ("B", "2 TB/day")):
        lo, hi = (elastic(*SCEN[k], m, b)[1] for m, b in zip(E_METER, SAMPLE_B))
        rows.append((f"Elastic Serverless Complete, {size}", f"graduated tiers, {money(lo)}–{money(hi)}", "vendor", lo,
                     "graduated ingest + 1 month retained, logs + TSDS metrics; high end: GB × 1.66 metering, 100 B/sample",
                     SRC["elastic"]))
        g_lo, g_hi = grafana(*SCEN[k], 60, False)[2], grafana(*SCEN[k], 60, True)[2]
        rows.append((f"Grafana Cloud Pro, {size}", f"60 s scrape, {money(g_lo)}–{money(g_hi)}", "vendor", g_lo,
                     "$19 + logs process + write (+ retain per the calculator) + metrics, graduated", SRC["grafana"]))
        rows.append((f"Datadog, {size}", "ingest + 15-day index + custom metrics", "vendor", datadog(*SCEN[k])[1],
                     "GB × $0.10 + events × $1.70/M + (series − 10k) × $0.05 + 100 hosts × $15", SRC["datadog"]))
    tok = SCEN["B"][0] * 1e9 / BYTES_PER_TOKEN
    rows.append(("Raw logs through an LLM, 2 TB/day", "cheapest batch price (gpt-5-nano), input only", "ruled",
                 tok / 1e6 * NANO_BATCH_IN * DAYS, f"2 TB ÷ {BYTES_PER_TOKEN} B/token × ${NANO_BATCH_IN}/M × 30", SRC["openai"]))
    rows.sort(key=lambda r: r[3])
    import json
    return ("const COST = [\n" + ",\n".join("  " + json.dumps(dict(label=r[0], sub=r[1], v=round(r[3], 2), g=r[2], f=r[4], src=r[5]),
                                                            ensure_ascii=False) for r in rows) + "\n];")


def write():
    """Replace the generated blocks in the doc and the report."""
    import os
    root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    sec = main()
    doc = os.path.join(root, "docs", "cost-sizing.md")
    s = open(doc).read()
    for name, text in sec.items():
        a, b = f"<!-- cost_model:{name} -->", f"<!-- /cost_model:{name} -->"
        s = s[:s.index(a) + len(a)] + "\n" + text + "\n" + s[s.index(b):]
    open(doc, "w").write(s)
    rep = os.path.join(root, "report", "anomaly-detection-report.html")
    h = open(rep).read()
    i = h.index("const COST = ["); j = h.index("];", i) + 2
    open(rep, "w").write(h[:i] + report_cost() + h[j:])


if __name__ == "__main__":
    import sys
    if "--write" in sys.argv:
        write()
    elif "--report" in sys.argv:
        print(report_cost())
    else:
        sec = main()
        print(sec["llm"] + "\n\n" + sec["prices"])
