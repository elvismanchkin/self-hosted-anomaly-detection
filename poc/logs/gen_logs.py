#!/usr/bin/env python3
"""Synthetic log generator for the Drain3 exporter PoC.

Emits Filebeat-like NDJSON ({"@timestamp", "service": {"name"}, "message"}) or plain text.
The last --anomaly-fraction of lines contains a burst of a never-seen-before error template
for one service, so the exporter's "new template" and per-template rate signals can be tested.

    python gen_logs.py --lines 1000000 > sample.ndjson
"""
import argparse
import json
import random
import sys
import uuid
from datetime import datetime, timedelta, timezone

TEMPLATES = {
    "checkout": [
        "Order {oid} created for user {uid} total={amount} currency=EUR",
        "Payment authorized order={oid} provider=stripe latency_ms={ms}",
        "GET /api/v1/cart/{uid} 200 {ms}ms",
        "POST /api/v1/checkout 201 {ms}ms",
        "Retrying payment call attempt={n} order={oid}",
        "Inventory reserved sku={sku} qty={n} warehouse=wh-{n}",
    ],
    "auth": [
        "User {uid} logged in from {ip}",
        "Token refreshed for session {uuid}",
        "Login failed for user {uid} from {ip}: bad password",
        "GET /oauth/authorize 302 {ms}ms",
        "JWKS cache refreshed keys={n}",
    ],
    "search": [
        "Query executed q_hash={hex} hits={n} took={ms}ms",
        "Cache miss key=search:{hex}",
        "Cache hit key=search:{hex}",
        "Shard {n} responded in {ms}ms",
        "Slow query detected took={ms}ms threshold=500ms",
    ],
    "gateway": [
        "{ip} - - \"GET /api/v1/products/{n} HTTP/1.1\" 200 {bytes} {ms}ms",
        "{ip} - - \"POST /api/v1/orders HTTP/1.1\" 201 {bytes} {ms}ms",
        "{ip} - - \"GET /healthz HTTP/1.1\" 200 2 0ms",
        "Upstream checkout responded 503 retry={n}",
        "Rate limit applied client={ip} limit=100/s",
    ],
    "worker": [
        "Job {uuid} started type=email",
        "Job {uuid} finished in {ms}ms",
        "Consumed {n} messages from topic orders partition={n}",
        "Committed offset {n} for partition {n}",
    ],
}

ANOMALY_SERVICE = "checkout"
ANOMALY_TEMPLATE = "ERROR db pool exhausted: timeout acquiring connection after {ms}ms (active={n} idle=0)"


def render(tpl: str, rnd: random.Random) -> str:
    return tpl.format(
        oid=rnd.randint(10**6, 10**7),
        uid=rnd.randint(1, 10**6),
        amount=f"{rnd.uniform(1, 500):.2f}",
        ms=rnd.randint(1, 2000),
        n=rnd.randint(0, 64),
        sku=f"SKU-{rnd.randint(1000, 9999)}",
        ip=f"10.{rnd.randint(0, 255)}.{rnd.randint(0, 255)}.{rnd.randint(1, 254)}",
        uuid=uuid.UUID(int=rnd.getrandbits(128)),
        hex=f"{rnd.getrandbits(64):016x}",
        bytes=rnd.randint(100, 50000),
    )


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--lines", type=int, default=200_000)
    ap.add_argument("--format", choices=["json", "text"], default="json")
    ap.add_argument("--anomaly-fraction", type=float, default=0.05,
                    help="tail fraction of lines where the new error template bursts")
    ap.add_argument("--seed", type=int, default=42)
    args = ap.parse_args()

    rnd = random.Random(args.seed)
    services = list(TEMPLATES)
    start = datetime(2026, 9, 14, tzinfo=timezone.utc)
    anomaly_from = int(args.lines * (1 - args.anomaly_fraction))
    out = sys.stdout
    for i in range(args.lines):
        svc = rnd.choice(services)
        tpl = rnd.choice(TEMPLATES[svc])
        if i >= anomaly_from and rnd.random() < 0.3:
            svc, tpl = ANOMALY_SERVICE, ANOMALY_TEMPLATE
        msg = render(tpl, rnd)
        if args.format == "json":
            ts = (start + timedelta(milliseconds=i)).isoformat().replace("+00:00", "Z")
            out.write(json.dumps({"@timestamp": ts, "service": {"name": svc}, "message": msg}) + "\n")
        else:
            out.write(f"{svc} {msg}\n")


if __name__ == "__main__":
    main()
