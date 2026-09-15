#!/usr/bin/env python3
"""Tier-2 anomaly scorer for a curated set of Prometheus series.

Pulls a few hundred/thousand *pre-aggregated* series (recording rules, topk)
from the Prometheus HTTP API, scores the latest point against a seasonal
baseline, and re-exposes the result as metrics that Prometheus scrapes back:

  anomaly_score{anomaly_query, <series labels>}      robust z-score of the latest point (signed)
  anomaly_expected / anomaly_observed{...}           baseline and actual value
  anomaly_band_upper / anomaly_band_lower{...}       expected +/- threshold * scale (Grafana band)
  anomaly_job_series{anomaly_query}                  series scored in the last run
  anomaly_job_last_success_timestamp_seconds, anomaly_job_errors_total

Method (per series, stdlib only):
  expected(t) = median(value(t - k*season) for k in 1..seasons)   # seasonal-naive, robust to one bad week if seasons >= 3
                falls back to median(last lookback window) when there is no history yet
  residual(t) = value(t) - expected(t)
  scale       = max(1.4826 * MAD(residuals over the last season), floor)
  score       = residual(now) / scale
Alert with a `for:` clause (see ../prometheus/anomaly-alerts.yml) instead of on a single point.
Input labels starting with `anomaly_` are dropped on export: re-exposed with them, the scores would
be picked up again by promql-anomaly-detection's selector and break its rules.

Run:
  python prom_anomaly_job.py --config queries.example.yml
  python prom_anomaly_job.py --config queries.example.yml --once     # print JSON, no server
  python prom_anomaly_job.py --selftest                              # synthetic data, no Prometheus
"""
import argparse
import json
import logging
import math
import random
import re
import statistics
import sys
import time

import requests
import yaml
from prometheus_client import Counter, disable_created_metrics, start_http_server
from prometheus_client.core import REGISTRY, GaugeMetricFamily, Metric

log = logging.getLogger("anomaly-job")
ERRORS = Counter("anomaly_job_errors_total", "Failed query/score runs", ["anomaly_query"])
UNITS = {"s": 1, "m": 60, "h": 3600, "d": 86400, "w": 604800}


def seconds(d) -> int:
    if isinstance(d, (int, float)):
        return int(d)
    m = re.fullmatch(r"(\d+)([smhdw])", str(d).strip())
    if not m:
        raise ValueError(f"bad duration {d!r}")
    return int(m.group(1)) * UNITS[m.group(2)]


def score_series(points: dict, now_ts: int, step: int, season: int, seasons: int,
                 lookback: int, threshold: float, abs_floor: float, rel_floor: float):
    """points: {timestamp: value}. Returns a dict for the latest point, or None if not scorable."""
    if now_ts not in points:
        return None

    def expected_at(t):
        hist = [points[t - k * season] for k in range(1, seasons + 1) if (t - k * season) in points]
        return statistics.median(hist) if hist else None

    exp_now = expected_at(now_ts)
    seasonal = exp_now is not None
    if not seasonal:  # cold start: no history one season back -> compare with the recent median
        recent = [v for t, v in points.items() if now_ts - lookback <= t < now_ts]
        if len(recent) < 3:
            return None
        exp_now = statistics.median(recent)

    residuals = []
    for t in range(now_ts - season, now_ts, step):
        if t not in points:
            continue
        e = expected_at(t) if seasonal else exp_now
        if e is not None:
            residuals.append(points[t] - e)
    if len(residuals) < 10:
        return None
    med = statistics.median(residuals)
    mad = statistics.median(abs(r - med) for r in residuals)
    scale = max(1.4826 * mad, abs_floor, rel_floor * abs(exp_now))
    obs = points[now_ts]
    return {
        "observed": obs,
        "expected": exp_now,
        "score": (obs - exp_now) / scale,
        "upper": exp_now + threshold * scale,
        "lower": exp_now - threshold * scale,
        "seasonal": seasonal,
    }


class Job:
    def __init__(self, cfg: dict, prom_url: str):
        self.cfg = cfg
        self.prom = prom_url.rstrip("/")
        self.step = seconds(cfg.get("step", "5m"))
        self.season = seconds(cfg.get("season", "1w"))
        self.seasons = int(cfg.get("seasons", 1))
        self.lookback = seconds(cfg.get("lookback", "2h"))
        self.threshold = float(cfg.get("threshold", 4))
        self.max_series = int(cfg.get("max_series", 500))
        if self.season % self.step:
            raise ValueError("season must be a multiple of step")
        if (self.seasons + 1) * self.season // self.step > 11000:
            raise ValueError("query_range would exceed Prometheus' 11,000 points per series; raise step or lower seasons")
        self.latest = []  # [(query_name, labels, result)] swapped atomically after each run
        self.series_count = {}
        self.last_success = time.time()  # grace period at start-up

    def fetch(self, expr: str, end: int) -> list:
        start = end - (self.seasons + 1) * self.season
        r = requests.get(f"{self.prom}/api/v1/query_range", timeout=60, params={
            "query": expr, "start": start, "end": end, "step": self.step})
        r.raise_for_status()
        body = r.json()
        if body.get("status") != "success":
            raise RuntimeError(body.get("error", "query failed"))
        return body["data"]["result"]

    def run_once(self) -> list:
        end = int(time.time()) // self.step * self.step
        out, counts = [], {}
        for q in self.cfg["queries"]:
            name = q["name"]
            try:
                result = self.fetch(q["expr"], end)
            except Exception as e:  # keep scoring the other queries
                ERRORS.labels(name).inc()
                log.warning("query %s failed: %s", name, e)
                continue
            if len(result) > self.max_series:
                log.warning("query %s returned %d series, scoring first %d (use topk/recording rules)",
                            name, len(result), self.max_series)
            n = 0
            for series in result[: self.max_series]:
                points = {int(float(t)): float(v) for t, v in series["values"] if not math.isnan(float(v))}
                res = score_series(points, end, self.step, self.season, self.seasons, self.lookback,
                                   self.threshold, float(q.get("abs_floor", 1e-9)), float(q.get("rel_floor", 0.05)))
                if res is None:
                    continue
                labels = {k: v for k, v in series["metric"].items()
                          if k != "__name__" and not k.startswith("anomaly_")}  # see docstring
                out.append((name, labels, res))
                n += 1
            counts[name] = n
        self.latest, self.series_count = out, counts
        if counts:  # at least one query answered; a run where every query failed is not a success
            self.last_success = time.time()
        return out

    def collect(self):  # prometheus_client custom collector: series labels vary per query
        families = {k: Metric(f"anomaly_{k}", f"anomaly job {k}", "gauge")
                    for k in ("score", "expected", "observed", "band_upper", "band_lower")}
        for name, labels, res in self.latest:
            lbl = dict(labels, anomaly_query=name)
            families["score"].add_sample("anomaly_score", lbl, res["score"])
            families["expected"].add_sample("anomaly_expected", lbl, res["expected"])
            families["observed"].add_sample("anomaly_observed", lbl, res["observed"])
            families["band_upper"].add_sample("anomaly_band_upper", lbl, res["upper"])
            families["band_lower"].add_sample("anomaly_band_lower", lbl, res["lower"])
        yield from families.values()
        g = GaugeMetricFamily("anomaly_job_series", "series scored in the last run", labels=["anomaly_query"])
        for name, n in self.series_count.items():
            g.add_metric([name], n)
        yield g
        yield GaugeMetricFamily("anomaly_job_last_success_timestamp_seconds",
                                "last run in which at least one query answered", value=self.last_success)


def selftest() -> int:
    """Two weeks of 5m data with daily seasonality + noise; spike injected at the last point."""
    rnd = random.Random(1)
    step, season = 300, 86400
    end = 1_789_000_000 // step * step
    pts = {}
    for t in range(end - 14 * season, end + step, step):
        daily = 100 + 60 * math.sin(2 * math.pi * (t % season) / season)
        pts[t] = daily + rnd.gauss(0, 5)
    normal = score_series(pts, end, step, season, 3, 7200, 4, 1e-9, 0.05)
    pts[end] = pts[end] + 120
    spike = score_series(pts, end, step, season, 3, 7200, 4, 1e-9, 0.05)
    flat = score_series({t: 7.0 for t in pts}, end, step, season, 3, 7200, 4, 1e-9, 0.05)
    print(json.dumps({"normal": normal, "spike": spike, "flat": flat}, indent=1))
    ok = abs(normal["score"]) < 4 and spike["score"] > 4 and flat["score"] == 0
    print("SELFTEST", "PASS" if ok else "FAIL")
    return 0 if ok else 1


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--config", help="YAML with step/season/seasons/lookback/threshold/queries")
    ap.add_argument("--prometheus", default="http://localhost:9090")
    ap.add_argument("--port", type=int, default=9106)
    ap.add_argument("--interval", type=int, default=60, help="seconds between runs")
    ap.add_argument("--once", action="store_true", help="score once, print JSON, exit")
    ap.add_argument("--selftest", action="store_true")
    args = ap.parse_args()
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
    if args.selftest:
        sys.exit(selftest())
    if not args.config:
        ap.error("--config is required")
    with open(args.config) as f:
        cfg = yaml.safe_load(f)
    job = Job(cfg, args.prometheus)
    if args.once:
        print(json.dumps([{"query": n, "labels": l, **r} for n, l, r in job.run_once()], indent=1))
        return
    disable_created_metrics()
    REGISTRY.register(job)
    start_http_server(args.port)
    while True:
        t0 = time.time()
        job.run_once()
        log.info("scored %s in %.1fs", job.series_count, time.time() - t0)
        time.sleep(max(1, args.interval - (time.time() - t0)))


if __name__ == "__main__":
    main()
