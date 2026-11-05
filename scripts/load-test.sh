#!/bin/sh
set -eu

cd "$(dirname "$0")/.."
compose() {
  docker compose -p shortner-load -f docker/docker-compose.load.yaml "$@"
}

open_browser() {
  target=$1
  printf '%s\n' "$target"
  if [ "${LOAD_OPEN_BROWSER:-1}" = 0 ]; then
    return
  fi
  if [ "$(uname -s)" = Darwin ]; then
    open "$target" || echo "Could not open browser; use the address above." >&2
  elif command -v xdg-open >/dev/null 2>&1 && [ -n "${DISPLAY:-}${WAYLAND_DISPLAY:-}" ]; then
    xdg-open "$target" >/dev/null 2>&1 || echo "Could not open browser; use the address above." >&2
  else
    echo "No graphical browser available; use the address above." >&2
  fi
}

wait_for() {
  url=$1
  attempts=0
  until curl -fsS "$url" >/dev/null 2>&1; do
    attempts=$((attempts + 1))
    if [ "$attempts" -ge 60 ]; then
      echo "Timed out waiting for $url" >&2
      return 1
    fi
    sleep 1
  done
}

command -v curl >/dev/null || { echo 'curl is required' >&2; exit 1; }
command -v tar >/dev/null || { echo 'tar is required' >&2; exit 1; }
LOAD_SEED_COUNT=${LOAD_SEED_COUNT:-1000000}
case "$LOAD_SEED_COUNT" in
  *[!0-9]*|'') echo 'LOAD_SEED_COUNT must be a non-negative integer' >&2; exit 1 ;;
esac
case "${LOAD_WORKLOAD:-mixed}" in
  mixed|redirect|list) ;;
  *) echo 'LOAD_WORKLOAD must be mixed, redirect or list' >&2; exit 1 ;;
esac
case "${LOAD_HOT_SET:-1000}" in
  *[!0-9]*|''|0) echo 'LOAD_HOT_SET must be a positive integer' >&2; exit 1 ;;
esac
if [ "${LOAD_WORKLOAD:-mixed}" = redirect ] && [ "$LOAD_SEED_COUNT" -ne 0 ] && \
   [ "$LOAD_SEED_COUNT" -lt "${LOAD_HOT_SET:-1000}" ]; then
  echo 'Redirect workload needs LOAD_SEED_COUNT >= LOAD_HOT_SET (or a preseeded database).' >&2
  exit 1
fi
for value in "${LOAD_RATE:-0}" "${LOAD_WARMUP_REQUESTS:-0}"; do
  case "$value" in
    *[!0-9]*|'') echo 'LOAD_RATE and LOAD_WARMUP_REQUESTS must be non-negative integers' >&2; exit 1 ;;
  esac
done
# Docker enforces network-name uniqueness across clients sharing the daemon.
lock=shortner-load-run-lock
if ! docker network create "$lock" >/dev/null; then
  echo "Could not acquire load-test lock ($lock); another run may be active." >&2
  exit 1
fi
sampler_pid=
cleanup() {
  if [ -n "$sampler_pid" ]; then
    kill "$sampler_pid" 2>/dev/null || true
    wait "$sampler_pid" 2>/dev/null || true
  fi
  docker network rm "$lock" >/dev/null || true
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
mkdir -p reports/load
LOAD_RESULTS_DIR=$(cd reports/load && mktemp -d "$(pwd)/run-XXXXXXXX")
LOAD_UID=$(id -u)
LOAD_GID=$(id -g)
export LOAD_RESULTS_DIR LOAD_UID LOAD_GID

if [ "${LOAD_SKIP_BUILD:-0}" != 1 ]; then
  # All roles use the same image; build it once, not once per service.
  compose build migrate
fi
compose up -d --no-build --remove-orphans proxy grafana
# Bind-mounted configuration changes do not recreate containers automatically.
compose restart proxy prometheus
wait_for "http://127.0.0.1:${LOAD_PROMETHEUS_PORT:-9092}/-/ready"
wait_for "http://127.0.0.1:${LOAD_GRAFANA_PORT:-3001}/api/health"
open_browser "http://127.0.0.1:${LOAD_GRAFANA_PORT:-3001}/d/shortener-operations?orgId=1&refresh=5s"
open_browser "http://127.0.0.1:${LOAD_GRAFANA_PORT:-3001}/d/load-test/load-test-live?orgId=1&refresh=5s"

printf 'Seeding up to %s links in the load database...\n' "$LOAD_SEED_COUNT"
compose exec -T postgres psql -X -U shortner_load -d shortner_load \
  -v target="$LOAD_SEED_COUNT" < docker/load/seed.sql

printf 'Results: %s\n' "$LOAD_RESULTS_DIR"
{
  date -u '+UTC: %Y-%m-%dT%H:%M:%SZ'
  printf 'workload=%s vus=%s duration=%s seed_target=%s hot_set=%s rate=%s max_vus=%s warmup_requests=%s\n' \
    "${LOAD_WORKLOAD:-mixed}" "${LOAD_VUS:-10}" "${LOAD_DURATION:-2m}" "$LOAD_SEED_COUNT" "${LOAD_HOT_SET:-1000}" "${LOAD_RATE:-0}" "${LOAD_MAX_VUS:-200}" "${LOAD_WARMUP_REQUESTS:-0}"
  docker version --format 'Docker server: {{.Server.Version}}'
  docker info --format 'Host: {{.OSType}}/{{.Architecture}} CPUs={{.NCPU}} Memory={{.MemTotal}} Kernel={{.KernelVersion}}'
  git rev-parse HEAD
  git status --short
} > "$LOAD_RESULTS_DIR/environment.txt"
compose config > "$LOAD_RESULTS_DIR/compose.yaml"
# Keep uncommitted/new build inputs too; a revision plus dirty paths is insufficient.
tar -czf "$LOAD_RESULTS_DIR/source.tar.gz" go.mod go.sum main.go cmd internal config \
  Dockerfile docker scripts/load-test.sh test/load
compose exec -T postgres psql -X -U shortner_load -d shortner_load \
  -c 'SELECT count(*) AS rows, count(*) FILTER (WHERE deleted_at IS NULL) AS visible_rows FROM links' \
  > "$LOAD_RESULTS_DIR/dataset.txt"
# Sample the whole stack; application budget is not the database/proxy/monitoring budget.
(
  while :; do
    date -u '+%Y-%m-%dT%H:%M:%SZ'
    docker stats --no-stream --format '{{.Name}} {{.CPUPerc}} {{.MemUsage}}' \
      $(docker ps -q --filter label=com.docker.compose.project=shortner-load) || true
    sleep 2
  done
) > "$LOAD_RESULTS_DIR/resources.txt" 2>&1 &
sampler_pid=$!
status=0
run_started_at=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
compose run --rm --no-deps k6 > "$LOAD_RESULTS_DIR/k6.log" 2>&1 || status=$?
# Preserve k6's exit status even when thresholds fail; retain all evidence.
kill "$sampler_pid" 2>/dev/null || true
wait "$sampler_pid" 2>/dev/null || true
sampler_pid=
for container in $(compose ps -q); do
  docker inspect --format '{{.Name}} memory_limit={{.HostConfig.Memory}} nano_cpus={{.HostConfig.NanoCpus}} oom_killed={{.State.OOMKilled}} restarts={{.RestartCount}} image={{.Image}}' "$container"
done > "$LOAD_RESULTS_DIR/containers.txt"
for query in \
  'histogram_quantile(0.95,sum by(le,operation)(rate(shortener_repository_operation_duration_seconds_bucket[1m])))' \
  'sum by(source)(rate(shortener_resolution_success_total[1m]))' \
  'sum by(instance)(rate(process_cpu_seconds_total[1m]))'; do
  printf '\n%s\n' "$query" >> "$LOAD_RESULTS_DIR/prometheus.txt"
  curl -fsSG "http://127.0.0.1:${LOAD_PROMETHEUS_PORT:-9092}/api/v1/query" \
    --data-urlencode "query=$query" >> "$LOAD_RESULTS_DIR/prometheus.txt" || true
done
compose exec -T postgres psql -X -v ON_ERROR_STOP=1 -U shortner_load -d shortner_load \
  -c 'SELECT (SELECT sum(total) FROM link_counts) AS maintained, (SELECT count(*) FROM links WHERE deleted_at IS NULL) AS actual' \
  -c 'DO $$ BEGIN IF (SELECT count(*) FROM link_counts) <> 64 OR (SELECT sum(total) FROM link_counts) <> (SELECT count(*) FROM links WHERE deleted_at IS NULL) THEN RAISE EXCEPTION '\''link count invariant failed'\''; END IF; END $$;' \
  > "$LOAD_RESULTS_DIR/counts.txt" 2>&1 || status=1
compose logs --no-color --since "$run_started_at" --tail 1000 > "$LOAD_RESULTS_DIR/services.log" 2>&1 || true
printf 'Run exit status: %s (see %s/k6.log and counts.txt)\n' "$status" "$LOAD_RESULTS_DIR"
if [ -f "$LOAD_RESULTS_DIR/index.html" ]; then
  open_browser "$LOAD_RESULTS_DIR/index.html"
else
  echo "No k6 report was generated; check the output above." >&2
fi
exit "$status"
