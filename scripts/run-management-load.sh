#!/bin/sh
# Each invocation restores an isolated dataset and records one repetition.
set -eu
cd "$(dirname "$0")/.."

case "${LOAD_DATASET:-}" in
  1m|10m) ;;
  *) echo 'Set LOAD_DATASET to 1m or 10m' >&2; exit 1 ;;
esac
scenario=${SCENARIO:-get}
case "$scenario" in
  create|get|list|list_deep|update|delete) default_rate=1010 ;;
  mixed) default_rate=200 ;;
  *) echo 'SCENARIO must be create, get, list, list_deep, update, delete or mixed' >&2; exit 1 ;;
esac
case "${LOAD_MANAGEMENT_PREPARE:-restore}" in
  snapshot|restore) prepare=${LOAD_MANAGEMENT_PREPARE:-restore} ;;
  *) echo 'LOAD_MANAGEMENT_PREPARE must be snapshot or restore' >&2; exit 1 ;;
esac
rate=${RATE:-$default_rate}
duration=${DURATION_SECONDS:-300}
warmup=${WARMUP_REQUESTS:-1000}
vus=${VUS:-200}
max_vus=${MAX_VUS:-1000}
for value in "$rate" "$duration" "$warmup" "$vus" "$max_vus"; do
  case "$value" in *[!0-9]*|'') echo 'Rate, duration, warmup and VU settings must be non-negative integers' >&2; exit 1 ;; esac
done
if [ "$rate" -eq 0 ] || [ "$duration" -eq 0 ] || [ "$vus" -eq 0 ] || [ "$max_vus" -lt "$vus" ]; then
  echo 'Rate, duration and VUS must be positive and MAX_VUS must be at least VUS' >&2; exit 1
fi
command -v python3 >/dev/null || { echo 'python3 is required' >&2; exit 1; }
command -v curl >/dev/null || { echo 'curl is required' >&2; exit 1; }
project_prefix=${LOAD_PROJECT_PREFIX:-shortner-load}
case "$project_prefix" in *[!a-z0-9_-]*|''|[-_]*) echo 'Invalid Compose project prefix' >&2; exit 1 ;; esac
case "${LOAD_PROFILING:-0}" in 0|1) ;; *) echo 'LOAD_PROFILING must be 0 or 1' >&2; exit 1 ;; esac
compose() {
  if [ "${LOAD_PROFILING:-0}" = 1 ]; then
    docker compose -p "$project_prefix-$LOAD_DATASET" -f docker/docker-compose.load.yaml -f docker/docker-compose.management.yaml -f docker/docker-compose.management-profile.yaml "$@"
  else
    docker compose -p "$project_prefix-$LOAD_DATASET" -f docker/docker-compose.load.yaml -f docker/docker-compose.management.yaml "$@"
  fi
}

# Share the historical runner's lock because both use the same ports and datasets.
lock=shortner-load-run-lock
if ! docker network create "$lock" >/dev/null; then
  echo 'Another load run holds the load-test lock' >&2; exit 1
fi
cleanup() {
  exit_status=$?
  if [ -n "${diagnostics_pid:-}" ]; then
    kill -TERM "$diagnostics_pid" 2>/dev/null || true
    wait "$diagnostics_pid" 2>/dev/null || true
  fi
  if [ -n "${generator_name:-}" ] && [ -n "${LOAD_RESULTS_DIR:-}" ]; then
    docker inspect --format '{{json .State}}' "$generator_name" > "$LOAD_RESULTS_DIR/generator-state.json" 2>/dev/null || true
    docker rm -f "$generator_name" >/dev/null 2>&1 || true
  fi
  if [ -n "${LOAD_RESULTS_DIR:-}" ] && [ -d "$LOAD_RESULTS_DIR" ]; then
    printf 'finished_utc=%s\nexit_status=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$exit_status" >> "$LOAD_RESULTS_DIR/run.txt"
  fi
  docker network rm "$lock" >/dev/null 2>&1 || true
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
mkdir -p reports/load
LOAD_RESULTS_DIR=$(cd reports/load && mktemp -d "$(pwd)/management-XXXXXXXX")
LOAD_UID=$(id -u)
LOAD_GID=$(id -g)
LOAD_MANAGEMENT_WRITE_COUNT=$((rate * duration))
LOAD_MANAGEMENT_WARMUP_WRITES=$warmup
export LOAD_RESULTS_DIR LOAD_UID LOAD_GID LOAD_MANAGEMENT_WRITE_COUNT LOAD_MANAGEMENT_WARMUP_WRITES
printf 'Results: %s\n' "$LOAD_RESULTS_DIR"
{
  printf 'started_utc=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  printf 'dataset=%s scenario=%s preparation=%s rate=%s duration_seconds=%s warmup_requests=%s vus=%s max_vus=%s\n' \
    "$LOAD_DATASET" "$scenario" "$prepare" "$rate" "$duration" "$warmup" "$vus" "$max_vus"
  printf 'application_revision='; git rev-parse HEAD
  printf 'docker_server_version='; docker version --format '{{.Server.Version}}'
  printf 'compose_version='; docker compose version --short
  printf 'load_script_sha256='; python3 -c 'import hashlib; print(hashlib.sha256(open("test/load/management.js", "rb").read()).hexdigest())'
  git status --short
} > "$LOAD_RESULTS_DIR/run.txt"

if [ "${LOAD_SKIP_BUILD:-0}" != 1 ]; then
  compose build migrate
fi
printf 'preparation_started_utc=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" >> "$LOAD_RESULTS_DIR/run.txt"
./scripts/prepare-management-run.sh "$prepare" > "$LOAD_RESULTS_DIR/preparation.log" 2>&1
printf 'preparation_finished_utc=%s\nreadiness_started_utc=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" >> "$LOAD_RESULTS_DIR/run.txt"
compose up -d --no-build jaeger proxy prometheus
attempt=0
until curl -fsS "http://127.0.0.1:${LOAD_PORT:-8088}/health" >/dev/null; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 60 ]; then echo 'Management readiness timed out' >&2; exit 1; fi
  sleep 1
done
printf 'readiness_utc=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" >> "$LOAD_RESULTS_DIR/run.txt"
python3 scripts/record-management-resources.py --project "$project_prefix-$LOAD_DATASET" \
  --output "$LOAD_RESULTS_DIR/resources-before.json"

printf 'profiling_enabled=%s\n' "${LOAD_PROFILING:-0}" >> "$LOAD_RESULTS_DIR/run.txt"
case "${LOAD_DIAGNOSTICS:-0}" in
  0) if [ "${LOAD_PROFILING:-0}" = 1 ]; then
       printf 'instrumentation=profiling-enabled; diagnostic only\n' >> "$LOAD_RESULTS_DIR/run.txt"
     else
       printf 'instrumentation=baseline\n' >> "$LOAD_RESULTS_DIR/run.txt"
     fi ;;
  1)
    printf 'instrumentation=diagnostic-sampling; not comparable to baseline without overhead assessment\n' >> "$LOAD_RESULTS_DIR/run.txt"
    python3 scripts/collect-management-diagnostics.py --project "$project_prefix-$LOAD_DATASET" \
      --output "$LOAD_RESULTS_DIR/diagnostics" > "$LOAD_RESULTS_DIR/diagnostics.log" 2>&1 &
    diagnostics_pid=$!
    ;;
  *) echo 'LOAD_DIAGNOSTICS must be 0 or 1' >&2; exit 1 ;;
esac

status=0
generator_name="$project_prefix-$LOAD_DATASET-$(basename "$LOAD_RESULTS_DIR")-k6"
compose run --name "$generator_name" --no-deps \
  -e "SCENARIO=$scenario" -e "RATE=$rate" -e "WARMUP_REQUESTS=$warmup" \
  -e "DURATION_SECONDS=$duration" -e "VUS=$vus" -e "MAX_VUS=$max_vus" \
  -e DATA_FILE=/results/management-dataset.json -e K6_OUT= \
  k6 run --out json=/results/events.json /scripts/management.js > "$LOAD_RESULTS_DIR/k6.log" 2>&1 || status=$?
printf 'k6_exit_status=%s\n' "$status" >> "$LOAD_RESULTS_DIR/run.txt"
if [ -n "${diagnostics_pid:-}" ]; then
  kill -TERM "$diagnostics_pid" 2>/dev/null || true
  wait "$diagnostics_pid" || status=1
  diagnostics_pid=
fi
python3 scripts/record-management-resources.py --project "$project_prefix-$LOAD_DATASET" \
  --output "$LOAD_RESULTS_DIR/resources-after.json" || status=1

# The reporter validates the marker and measured window for every operation.
python3 scripts/report-management-load.py "$LOAD_RESULTS_DIR/events.json" \
  --operation "$scenario" --duration-seconds "$duration" --rate "$rate" \
  --output "$LOAD_RESULTS_DIR/report.json" || status=1
if [ "${LOAD_DIAGNOSTICS:-0}" = 1 ]; then
  python3 scripts/report-management-diagnostics.py --samples "$LOAD_RESULTS_DIR/diagnostics/samples.jsonl" \
    --report "$LOAD_RESULTS_DIR/report.json" --summary "$LOAD_RESULTS_DIR/summary.json" \
    --output "$LOAD_RESULTS_DIR/diagnostics-report.json" || status=1
fi
compose exec -T postgres psql -X -v ON_ERROR_STOP=1 -U shortner_load -d shortner_load \
  -c 'SELECT count(*) AS physical_rows, count(*) FILTER (WHERE deleted_at IS NULL) AS visible_rows FROM links' \
  -c 'SELECT sum(total) AS maintained FROM link_counts' > "$LOAD_RESULTS_DIR/counts-after.txt" 2>&1 || status=1
printf 'Run exit status: %s. Results: %s\n' "$status" "$LOAD_RESULTS_DIR"
exit "$status"
