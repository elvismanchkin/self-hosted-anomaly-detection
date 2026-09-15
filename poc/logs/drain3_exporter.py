#!/usr/bin/env python3
"""Log lines -> Drain3 templates -> Prometheus metrics.

Turns an unbounded log stream into a small, bounded set of time series
("lines per template per service") so log anomaly detection can reuse the
same cheap PromQL rules as metrics (see ../prometheus/anomaly-inputs.yml and
../prometheus/anomaly-alerts.yml).

Input: NDJSON (Filebeat/Logstash-style events) or plain text on stdin.
Feed it from a tee at ingest (Logstash `pipe`/`http` output, Kafka consumer,
Filebeat file output) rather than re-reading Elasticsearch.

Exposed metrics (default :9105/metrics):
  log_lines_total{service}                      all lines seen (after sampling)
  log_template_lines_total{service,template_id} lines per template ("other" once the cap is hit)
  log_templates_created_total{service}          new templates discovered (novelty signal)
  log_templates_active{service}                 templates currently held by Drain3
  log_template_info{service,template_id,template} 1, maps id -> template text for dashboards

Run:
  python gen_logs.py --lines 500000 | python drain3_exporter.py --port 9105
  python gen_logs.py --lines 500000 > s.ndjson && python drain3_exporter.py --bench < s.ndjson
"""
import argparse
import json
import logging
import os
import random
import re
import sys
import time
from collections import Counter as TallyCounter

from drain3 import TemplateMiner
from drain3.file_persistence import FilePersistence
from drain3.masking import MaskingInstruction
from drain3.template_miner_config import TemplateMinerConfig
from prometheus_client import Counter, Gauge, disable_created_metrics, start_http_server

# Order matters: the most specific patterns first.
MASKS = [
    (r"[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}", "UUID"),
    (r"((?<=[^A-Za-z0-9])|^)(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})((?=[^A-Za-z0-9])|$)", "IP"),
    (r"((?<=[^A-Za-z0-9])|^)(0x[0-9a-fA-F]+|[0-9a-fA-F]{12,})((?=[^A-Za-z0-9])|$)", "HEX"),
    (r"((?<=[^A-Za-z0-9])|^)(\d+(\.\d+)?(ns|us|ms|s|KB|MB|GB|B|%))((?=[^A-Za-z0-9])|$)", "NUM"),
    (r"((?<=[^A-Za-z0-9])|^)([\-\+]?\d+(\.\d+)?)((?=[^A-Za-z0-9])|$)", "NUM"),
]

LINES = Counter("log_lines_total", "Log lines processed", ["service"])
TEMPLATE_LINES = Counter("log_template_lines_total", "Log lines per Drain3 template", ["service", "template_id"])
CREATED = Counter("log_templates_created_total", "New Drain3 templates discovered", ["service"])
ACTIVE = Gauge("log_templates_active", "Templates currently held by Drain3", ["service"])
INFO = Gauge("log_template_info", "Template id -> text", ["service", "template_id", "template"])

SAFE_NAME = re.compile(r"[^A-Za-z0-9_.-]")


def get_field(event: dict, dotted: str):
    """Filebeat events can be flat ("service.name") or nested ({"service": {"name"}})."""
    if dotted in event:
        return event[dotted]
    cur = event
    for part in dotted.split("."):
        if not isinstance(cur, dict) or part not in cur:
            return None
        cur = cur[part]
    return cur


def make_config(args) -> TemplateMinerConfig:
    cfg = TemplateMinerConfig()
    cfg.drain_sim_th = args.sim_th
    cfg.drain_depth = args.depth
    cfg.drain_max_clusters = args.max_clusters  # LRU-evicts old templates -> bounded memory
    cfg.masking_instructions = [MaskingInstruction(p, name) for p, name in MASKS]
    cfg.snapshot_interval_minutes = 5
    return cfg


class Exporter:
    def __init__(self, args):
        self.args = args
        self.cfg = make_config(args)
        self.miners = {}
        self.exported = {}  # (service, cluster_id) -> template text last exported in log_template_info
        self.rnd = random.Random(0)

    def miner(self, service: str) -> TemplateMiner:
        m = self.miners.get(service)
        if m is None:
            persistence = None
            if self.args.state_dir:
                os.makedirs(self.args.state_dir, exist_ok=True)
                persistence = FilePersistence(os.path.join(self.args.state_dir, f"{SAFE_NAME.sub('_', service)}.bin"))
            m = self.miners[service] = TemplateMiner(persistence, self.cfg)
        return m

    def parse(self, line: str):
        if self.args.format == "text":
            return self.args.default_service, line
        try:
            event = json.loads(line)
        except ValueError:
            return self.args.default_service, line
        msg = get_field(event, self.args.message_field)
        svc = get_field(event, self.args.service_field) or self.args.default_service
        return str(svc), (msg if isinstance(msg, str) else line)

    def process(self, line: str, emit: bool = True):
        line = line.rstrip("\n")
        if not line or (self.args.sample < 1.0 and self.rnd.random() >= self.args.sample):
            return None
        service, msg = self.parse(line)
        res = self.miner(service).add_log_message(msg)
        if not emit:
            return service, res
        cid = res["cluster_id"]
        key = (service, cid)
        if key not in self.exported and len(self.exported) < self.args.max_exported:
            self.exported[key] = None
        if key in self.exported:
            tid = str(cid)
            text = res["template_mined"][:200]
            if self.exported[key] != text:  # new template, or Drain generalised it: replace the info series
                if self.exported[key] is not None:
                    INFO.remove(service, tid, self.exported[key])
                INFO.labels(service, tid, text).set(1)
                self.exported[key] = text
        else:
            tid = "other"
        LINES.labels(service).inc()
        TEMPLATE_LINES.labels(service, tid).inc()
        if res["change_type"] == "cluster_created":
            CREATED.labels(service).inc()
        ACTIVE.labels(service).set(res["cluster_count"])
        return service, res


def bench(exp: Exporter) -> None:
    lines = sys.stdin.readlines()
    nbytes = sum(len(line) for line in lines)
    created_at = {}
    per_template = TallyCounter()
    t0 = time.perf_counter()
    for i, line in enumerate(lines):
        out = exp.process(line, emit=False)
        if out is None:
            continue
        service, res = out
        per_template[(service, res["cluster_id"])] += 1
        if res["change_type"] == "cluster_created":
            created_at[(service, res["cluster_id"])] = i
    dt = time.perf_counter() - t0
    n = len(lines)
    print(f"lines={n} bytes={nbytes} avg_line_bytes={nbytes / max(n, 1):.0f}")
    print(f"elapsed_s={dt:.2f} lines_per_s={n / dt:,.0f} MB_per_s={nbytes / dt / 1e6:.2f} (single core, Python)")
    for svc, m in sorted(exp.miners.items()):
        print(f"service={svc} templates={len(m.drain.clusters)}")
    late = [(k, i) for k, i in created_at.items() if i > 0.5 * n]
    print(f"templates first seen in 2nd half of input: {len(late)}")
    for (svc, cid), i in sorted(late, key=lambda x: x[1])[:10]:
        tpl = exp.miners[svc].drain.id_to_cluster[cid].get_template()
        print(f"  line={i} service={svc} id={cid} count={per_template[(svc, cid)]} template={tpl[:120]}")


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--format", choices=["json", "text"], default="json")
    ap.add_argument("--message-field", default="message")
    ap.add_argument("--service-field", default="service.name")
    ap.add_argument("--default-service", default="unknown")
    ap.add_argument("--port", type=int, default=9105)
    ap.add_argument("--sim-th", type=float, default=0.4, help="Drain similarity threshold")
    ap.add_argument("--depth", type=int, default=4, help="Drain parse-tree depth")
    ap.add_argument("--max-clusters", type=int, default=2000, help="per-service template cap (LRU)")
    ap.add_argument("--max-exported", type=int, default=1000,
                    help="global cap on (service, template_id) label pairs; the rest count as 'other'")
    ap.add_argument("--sample", type=float, default=1.0,
                    help="process this fraction of lines (rates scale by 1/sample; rare templates may be missed)")
    ap.add_argument("--state-dir", help="persist Drain3 state here so template ids survive restarts")
    ap.add_argument("--bench", action="store_true", help="read all stdin, report throughput, no HTTP server")
    args = ap.parse_args()

    logging.basicConfig(level=logging.WARNING)
    disable_created_metrics()  # drop *_created series: less cardinality, not needed for rate()
    exp = Exporter(args)
    if args.bench:
        bench(exp)
        return
    start_http_server(args.port)
    for line in sys.stdin:
        exp.process(line)


if __name__ == "__main__":
    main()
