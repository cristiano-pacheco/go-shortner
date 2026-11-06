#!/bin/sh
# Extra five-minute workloads on a restored management baseline.
set -eu
cd "$(dirname "$0")/.."

workload=${1:-}
case "$workload" in
  mixed) default_rate=200; repeats=2; batch=1; warmup=0; prefix=full-api ;;
  redirect) default_rate=1500; repeats=20; batch=10; warmup=1000; prefix=redirect-stress ;;
  *) echo 'Usage: run-api-load.sh mixed|redirect' >&2; exit 1 ;;
esac
repeats=${REDIRECT_REPEATS:-$repeats}
batch=${REDIRECT_BATCH_SIZE:-$batch}
case "${LOAD_DATASET:-}" in
  1m|10m) ;;
  *) echo 'Set LOAD_DATASET to 1m or 10m' >&2; exit 1 ;;
esac
rate=${RATE:-$default_rate}
duration=${DURATION_SECONDS:-300}
vus=${VUS:-200}
max_vus=${MAX_VUS:-1000}
case "${LOAD_PUBLIC_MEMORY:-32m}" in
  32m) public_memory_mib=32 ;;
  64m) public_memory_mib=64 ;;
  *) echo 'LOAD_PUBLIC_MEMORY must be 32m or 64m' >&2; exit 1 ;;
esac
for value in "$rate" "$duration" "$vus" "$max_vus" "$repeats" "$batch"; do
  case "$value" in *[!0-9]*|''|0) echo 'Rate, duration and VU settings must be positive integers' >&2; exit 1 ;; esac
done
if [ "$max_vus" -lt "$vus" ]; then
  echo 'MAX_VUS must be at least VUS' >&2; exit 1
fi
compose() {
  docker compose -p "shortner-load-$LOAD_DATASET" -f docker/docker-compose.load.yaml \
    -f docker/docker-compose.management.yaml "$@"
}
docker network create shortner-load-run-lock >/dev/null
cleanup() {
  code=$?
  if [ -n "${generator:-}" ]; then
    docker inspect --format '{{json .State}}' "$generator" > "$LOAD_RESULTS_DIR/generator-state.json" 2>/dev/null || true
    docker rm -f "$generator" >/dev/null 2>&1 || true
  fi
  if [ -n "${LOAD_RESULTS_DIR:-}" ]; then
    printf 'finished_utc=%s\nexit_status=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$code" >> "$LOAD_RESULTS_DIR/run.txt"
  fi
  docker network rm shortner-load-run-lock >/dev/null 2>&1 || true
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
mkdir -p reports/load
LOAD_RESULTS_DIR=$(mktemp -d "$PWD/reports/load/$prefix-XXXXXXXX")
LOAD_UID=$(id -u)
LOAD_GID=$(id -g)
export LOAD_RESULTS_DIR LOAD_UID LOAD_GID
printf 'Results: %s\n' "$LOAD_RESULTS_DIR"
if [ "$workload" = mixed ]; then
  requests_per_iteration=7
  outputs=json,experimental-prometheus-rw
else
  requests_per_iteration=$repeats
  outputs=experimental-prometheus-rw
fi
{
  printf 'started_utc=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  printf 'application_revision='; git rev-parse HEAD
  printf 'load_script_sha256='; python3 -c 'import hashlib; print(hashlib.sha256(open("test/load/redirect.js", "rb").read()).hexdigest())'
  printf 'runner_sha256='; python3 -c 'import hashlib; print(hashlib.sha256(open("scripts/run-api-load.sh", "rb").read()).hexdigest())'
  printf 'workload=%s\nrate_iterations_per_second=%s\nduration_seconds=%s\nrequests_per_successful_iteration=%s\n' "$workload" "$rate" "$duration" "$requests_per_iteration"
  printf 'offered_requests_per_second=%s\nhot_set=1000\nredirect_batch_size=%s\n' "$((rate * requests_per_iteration))" "$batch"
  printf 'preallocated_vus=%s\nmax_vus=%s\nwarmup_requests=%s\nk6_outputs=%s\n' "$vus" "$max_vus" "$warmup" "$outputs"
  printf 'rate_stages=%s\n' "${RATE_STAGES:-}"
  printf 'redirect_method=%s\npublic_memory=%s\npublic_gomemlimit=%s\n' "${REDIRECT_METHOD:-alternating}" "${LOAD_PUBLIC_MEMORY:-32m}" "${LOAD_PUBLIC_GOMEMLIMIT:-24MiB}"
  git status --short
} > "$LOAD_RESULTS_DIR/run.txt"
if [ "${LOAD_SKIP_BUILD:-0}" != 1 ]; then
  compose build migrate
fi
./scripts/prepare-management-run.sh restore > "$LOAD_RESULTS_DIR/preparation.log" 2>&1
compose up -d --no-build jaeger proxy prometheus grafana
python3 scripts/record-management-resources.py --project "shortner-load-$LOAD_DATASET" \
  --public-memory-mib "$public_memory_mib" --output "$LOAD_RESULTS_DIR/resources-before.json"
generator="shortner-load-$LOAD_DATASET-$(basename "$LOAD_RESULTS_DIR")-k6"
status=0
printf 'load_started_utc=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" >> "$LOAD_RESULTS_DIR/run.txt"
set -- --out experimental-prometheus-rw
if [ "$workload" = mixed ]; then
  set -- "$@" --out json=/results/events.json
fi
compose run --name "$generator" --no-deps \
  -e "WORKLOAD=$workload" -e "RATE=$rate" -e "DURATION=${duration}s" \
  -e "VUS=$vus" -e "MAX_VUS=$max_vus" -e HOT_SET=1000 \
  -e "REDIRECT_REPEATS=$repeats" -e "REDIRECT_BATCH_SIZE=$batch" \
  -e "WARMUP_REQUESTS=$warmup" -e K6_OUT= \
  -e "RATE_STAGES=${RATE_STAGES:-}" \
  -e "REDIRECT_METHOD=${REDIRECT_METHOD:-}" \
  k6 run "$@" /scripts/redirect.js > "$LOAD_RESULTS_DIR/k6.log" 2>&1 || status=$?
printf 'load_finished_utc=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" >> "$LOAD_RESULTS_DIR/run.txt"
python3 scripts/record-management-resources.py --project "shortner-load-$LOAD_DATASET" \
  --public-memory-mib "$public_memory_mib" --output "$LOAD_RESULTS_DIR/resources-after.json" || status=1
compose exec -T postgres psql -X -v ON_ERROR_STOP=1 -U shortner_load -d shortner_load \
  -c 'SELECT count(*) AS physical_rows, count(*) FILTER (WHERE deleted_at IS NULL) AS visible_rows FROM links' \
  -c 'SELECT sum(total) AS maintained FROM link_counts' \
  -c "DO \$\$ BEGIN IF (SELECT count(*) FROM link_counts) <> 64 OR (SELECT sum(total) FROM link_counts) <> (SELECT count(*) FROM links WHERE deleted_at IS NULL) THEN RAISE EXCEPTION 'link count invariant failed'; END IF; END \$\$;" \
  > "$LOAD_RESULTS_DIR/counts-after.txt" 2>&1 || status=1
printf 'Run exit status: %s. Results: %s\n' "$status" "$LOAD_RESULTS_DIR"
exit "$status"
