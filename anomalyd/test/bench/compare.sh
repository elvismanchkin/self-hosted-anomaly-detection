#!/usr/bin/env bash
# One-core log-mining throughput, same file, both in Docker with --cpus 1:
#   anomalyd bench (extract JSON fields → mask → Drain, per service)
#   otelcol-contrib 0.160.0 (file_log + json_parser → drain processor → count connector)
# Needs the anomalyd:e2e image (built by ../e2e/run.sh). Synthetic data only.
# CONTAINER=cri wraps every line as containerd writes container stdout; both readers then unwrap it.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
N=${N:-3000000}
OTEL=otel/opentelemetry-collector-contrib:0.160.0
if [ "${CONTAINER:-}" = cri ]; then
  FILE=s.cri CFG=otel-drain-cri.yaml GEN=(--container cri) BENCH=(--container cri)
else
  FILE=s.ndjson CFG=otel-drain.yaml GEN=() BENCH=()
fi
cleanup() { docker rm -f ad-bench-otel >/dev/null 2>&1 || true; docker volume rm ad-bench >/dev/null 2>&1 || true; }
trap cleanup EXIT
cleanup
docker volume create ad-bench >/dev/null
docker run --rm anomalyd:e2e gen logs --lines "$N" ${GEN[@]+"${GEN[@]}"} |
  docker run --rm -i -v ad-bench:/data debian:bookworm-slim sh -c "cat > /data/$FILE && wc -lc /data/$FILE"

echo "== anomalyd bench (--cpus 1, GOMAXPROCS=1)"
docker run --rm --cpus 1 -v ad-bench:/data:ro anomalyd:e2e bench --file /data/$FILE --procs 1 ${BENCH[@]+"${BENCH[@]}"}

echo "== otelcol-contrib drain path (--cpus 1)"
t0=$(date +%s.%N)
docker run -d --name ad-bench-otel --cpus 1 -p 127.0.0.1:18888:8888 -v ad-bench:/data:ro \
  -v "$here/$CFG":/etc/otel.yaml:ro $OTEL --config=/etc/otel.yaml >/dev/null
metric() { curl -s 127.0.0.1:18888/metrics | awk -v m="$1" '$1 ~ "^"m {s+=$2} END {print s+0}'; }
until [ "$(metric otelcol_processor_drain_log_records_annotated)" -ge $((N - 100)) ] 2>/dev/null; do
  sleep 0.5
  [ "$(docker inspect -f '{{.State.Running}}' ad-bench-otel)" = true ] || { docker logs ad-bench-otel 2>&1 | tail -3; exit 1; }
  [ "$(echo "$(date +%s.%N) - $t0 > 900" | bc)" = 1 ] && { echo TIMEOUT; break; }
done
t1=$(date +%s.%N)
annotated=$(metric otelcol_processor_drain_log_records_annotated)
clusters=$(metric otelcol_processor_drain_clusters_active)
echo "lines_annotated=$annotated clusters=$clusters elapsed_s=$(echo "$t1 - $t0" | bc) lines/s=$(echo "$annotated / ($t1 - $t0)" | bc)"
docker stats --no-stream --format 'otelcol mem={{.MemUsage}}' ad-bench-otel
