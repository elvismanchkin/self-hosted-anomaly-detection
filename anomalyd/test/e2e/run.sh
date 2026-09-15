#!/usr/bin/env bash
# End-to-end test in Docker, synthetic data only, nothing leaves the Docker network:
#   Prometheus v3.14.0 (8 days of history imported with promtool, recording rules, remote_write)
#   Alertmanager v0.34.0, anomalyd server + agent, synthetic metric and log generators.
# Expected findings: checkout request spike (metric), new checkout ERROR template (log novelty),
# search stops logging (log volume drop). Logs are written as containerd writes container stdout
# (CRI envelopes, /var/log/containers file names) and the agent unwraps them. Takes about 14 minutes.
#   anomalyd/test/e2e/run.sh          # run, check, clean up
#   anomalyd/test/e2e/run.sh keep     # leave containers running
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
out=$here/out
net=anomalyd-e2e
PROM=prom/prometheus:v3.14.0
AM=prom/alertmanager:v0.34.0
arch=$(docker version --format '{{.Server.Arch}}')
mkdir -p "$out"

cleanup() {
  docker rm -f ad-prom ad-am ad-server ad-agent ad-genm ad-genl >/dev/null 2>&1 || true
  docker volume rm ad-prom-data ad-logs >/dev/null 2>&1 || true
  docker network rm $net >/dev/null 2>&1 || true
}
[ "${1:-}" = keep ] || trap cleanup EXIT
cleanup

echo "== build linux/$arch binary and image"
(cd "$root" && CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -ldflags='-s -w' -o "$out/anomalyd" ./cmd/anomalyd)
COPYFILE_DISABLE=1 tar --no-xattrs -c -C "$out" anomalyd -C "$root" Dockerfile | docker build -q -t anomalyd:e2e - >/dev/null
ls -l "$out/anomalyd" | awk '{print "binary bytes:", $5}'

echo "== import 8 days of 1m history into Prometheus with promtool"
docker network create $net >/dev/null
docker volume create ad-prom-data >/dev/null
docker volume create ad-logs >/dev/null
docker run --rm anomalyd:e2e gen history --days 8 --step 1m --end-ago 2m |
  docker run --rm -i -v ad-prom-data:/prometheus --entrypoint sh $PROM -c \
    'cat > /tmp/h.om && promtool tsdb create-blocks-from openmetrics /tmp/h.om /prometheus >/dev/null && echo "blocks: $(ls -d /prometheus/01* | wc -l)"'
docker run --rm -v ad-logs:/logs debian:bookworm-slim chown 65532:65532 /logs

upstream=()
if ls "$root/../poc/prometheus/vendor/"*.yml >/dev/null 2>&1; then
  upstream=(-v "$root/../poc/prometheus/vendor":/etc/prometheus/upstream:ro)
  echo "upstream promql-anomaly-detection rules mounted"
fi

echo "== start"
t0=$(date +%s)
docker run -d --name ad-am --network $net -v "$here/alertmanager.yml":/etc/am.yml:ro $AM --config.file=/etc/am.yml >/dev/null
docker run -d --name ad-server --network $net -p 127.0.0.1:19400:9400 anomalyd:e2e server \
  --step 1m --eval-interval 10s --grace 20s --prom-season 168h --prom-seasons 1 \
  --backfill-url http://ad-prom:9090 --backfill-match '{__name__=~"anomaly:svc:.+"}' \
  --alertmanager-url http://ad-am:9093 --external-url http://ad-server:9400 \
  --novelty-warmup 1m --log-lookback 30m >/dev/null
docker run -d --name ad-genm --network $net anomalyd:e2e gen metrics --listen :9500 \
  --inject-service checkout --inject-kind spike --inject-after 5m >/dev/null
docker run -d --name ad-prom --network $net -p 127.0.0.1:19490:9090 -v ad-prom-data:/prometheus \
  -v "$here/prometheus.yml":/etc/prometheus/prometheus.yml:ro -v "$here/rules.yml":/etc/prometheus/rules.yml:ro \
  "${upstream[@]}" $PROM --config.file=/etc/prometheus/prometheus.yml --storage.tsdb.path=/prometheus >/dev/null
docker run -d --name ad-genl --network $net -v ad-logs:/logs anomalyd:e2e gen logs --out-dir /logs --container cri \
  --rate 1000 --inject-after 4m --drop-service search --drop-after 11m >/dev/null
docker run -d --name ad-agent --network $net -v ad-logs:/logs:ro anomalyd:e2e agent \
  --path '/logs/*.log' --container cri --service-from-path '_(?P<service>[^_]+)-[0-9a-f]{64}\.log$' --server http://ad-server:9400 --from-start --flush-interval 10s --listen :9401 >/dev/null

echo "== wait for the three expected findings (max 20 min)"
check() {
  curl -sf 127.0.0.1:19400/api/v1/findings | python3 -c '
import json, sys
f = json.load(sys.stdin) or []
want = {
  "metric spike (checkout)": lambda x: x["labels"]["alertname"] == "AnomalydMetricAnomaly" and x["labels"].get("service") == "checkout",
  "new template (checkout)": lambda x: x["labels"]["alertname"] == "AnomalydLogNewTemplate" and x["labels"].get("service") == "checkout",
  "volume drop (search)":    lambda x: x["labels"]["alertname"] == "AnomalydLogVolume" and x["labels"].get("service") == "search" and x["labels"]["direction"] == "down",
}
got = {k: any(p(x) for x in f) for k, p in want.items()}
print(" ".join(f"{k}={v}" for k, v in got.items()))
sys.exit(0 if all(got.values()) else 1)'
}
until check; do
  [ $(($(date +%s) - t0)) -gt 1200 ] && { echo "TIMEOUT"; break; }
  sleep 30
done
echo "elapsed: $(($(date +%s) - t0))s"

echo "== results -> $out"
curl -s 127.0.0.1:19400/api/v1/findings >"$out/findings.json"
python3 - "$out/findings.json" <<'EOF' | tee "$out/summary.txt"
import json, sys
f = json.load(open(sys.argv[1]))
print(f"findings: {len(f)}")
for x in f:
    l = x["labels"]
    print(f'  {x["state"]:8} {l["alertname"]:24} service={l.get("service")} level={l.get("level","-")} '
          f'dir={l.get("direction","-")} score={x["score"]:+.1f} obs={x["observed"]:.4g} exp={x["expected"]:.4g} '
          f'seasonal={x["seasonal"]} {x["annotations"].get("template","")[:70]}')
EOF
echo "-- alertmanager (amtool alert query source=anomalyd)" | tee -a "$out/summary.txt"
docker exec ad-am amtool --alertmanager.url=http://localhost:9093 alert query source=anomalyd | tee -a "$out/summary.txt"
echo "-- promtool check metrics on anomalyd /metrics" | tee -a "$out/summary.txt"
if curl -s 127.0.0.1:19400/metrics | docker run --rm -i --entrypoint promtool $PROM check metrics >"$out/promtool-metrics.txt" 2>&1; then
  echo "promtool check metrics: OK" | tee -a "$out/summary.txt"
else
  echo "promtool check metrics: FAILED" | tee -a "$out/summary.txt"; cat "$out/promtool-metrics.txt" | tee -a "$out/summary.txt"
fi
echo "-- inhibited by the AnomalydLogVolume rule (amtool alert query --inhibited)" | tee -a "$out/summary.txt"
docker exec ad-am amtool --alertmanager.url=http://localhost:9093 alert query --inhibited source=anomalyd | tee -a "$out/summary.txt"
echo "-- prometheus self-checks" | tee -a "$out/summary.txt"
for q in 'sum(prometheus_remote_storage_samples_total)' 'sum(prometheus_remote_storage_samples_failed_total)' \
         'sum(prometheus_rule_evaluation_failures_total)' 'min(up)' 'count(anomalyd_score)'; do
  printf '%s = ' "$q" | tee -a "$out/summary.txt"
  curl -sG 127.0.0.1:19490/api/v1/query --data-urlencode "query=$q" |
    python3 -c 'import json,sys; r=json.load(sys.stdin)["data"]["result"]; print(r[0]["value"][1] if r else "none")' | tee -a "$out/summary.txt"
done
echo "-- anomalyd counters" | tee -a "$out/summary.txt"
curl -s 127.0.0.1:19400/metrics | grep -E '^anomalyd_(series|backfill_samples_total|remote_write_samples_total|log_lines_total|log_pushes_total|alerts_sent_total|alert_errors_total|eval_duration_seconds|go_memory_total_bytes)' | tee -a "$out/summary.txt"
docker exec ad-prom wget -qO- http://ad-agent:9401/metrics | grep -E '^anomalyd_agent_(lines|bytes|pushed_bytes|push_errors)_total' | tee -a "$out/summary.txt"
echo "-- docker stats" | tee -a "$out/summary.txt"
docker stats --no-stream --format '{{.Name}} cpu={{.CPUPerc}} mem={{.MemUsage}}' ad-server ad-agent ad-prom ad-genl | tee -a "$out/summary.txt"
check >/dev/null && echo "E2E PASS" | tee -a "$out/summary.txt" || { echo "E2E FAIL" | tee -a "$out/summary.txt"; exit 1; }
