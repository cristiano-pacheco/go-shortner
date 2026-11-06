#!/bin/sh
set -eu
cd "$(dirname "$0")/../.."

# A separate project and volume avoid modifying the campaign dataset.
prefix="shortner-diagnostics-$(date +%s)-$$"
tmp=$(mktemp -d)
export LOAD_PROJECT_PREFIX="$prefix" LOAD_DATASET=1m
export LOAD_PORT=${TEST_LOAD_PORT:-38088} LOAD_PROMETHEUS_PORT=${TEST_PROMETHEUS_PORT:-39092}
export LOAD_JAEGER_PORT=${TEST_JAEGER_PORT:-36687}
export LOAD_MANAGEMENT_READ_COUNT=50
compose() {
  docker compose -p "$prefix-1m" -f docker/docker-compose.load.yaml -f docker/docker-compose.management.yaml "$@"
}
cleanup() {
  status=$?
  trap - EXIT INT TERM
  compose down -v --remove-orphans >/dev/null 2>&1 || status=1
  if [ "$status" -ne 0 ]; then
    echo "Diagnostic integration logs: $tmp" >&2
    cat "$tmp"/*.log >&2 2>/dev/null || true
  else
    rm -rf "$tmp"
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

LOAD_SEED_COUNT=200 LOAD_SEED_BATCH_SIZE=100 ./scripts/prepare-load-dataset.sh >"$tmp/seed.log" 2>&1
# The low rate is deliberately below the campaign threshold; report failure is expected.
status=0
LOAD_DIAGNOSTICS=1 LOAD_PROFILING="${TEST_LOAD_PROFILING:-0}" SCENARIO=get LOAD_MANAGEMENT_PREPARE=snapshot RATE=10 DURATION_SECONDS=6 \
  WARMUP_REQUESTS=1 VUS=10 MAX_VUS=10 ./scripts/run-management-load.sh >"$tmp/run.log" 2>&1 || status=$?
result=$(sed -n 's/^Results: //p' "$tmp/run.log" | head -n 1)
if [ -z "$result" ] || [ ! -f "$result/report.json" ]; then
  echo "Missing short-run report (status $status)" >&2
  exit 1
fi
if [ "${TEST_LOAD_PROFILING:-0}" = 1 ]; then
  for service in management public; do
    ./scripts/capture-management-profile.sh "$service" heap 1 "$tmp/$service.heap"
    go tool pprof -top "$tmp/$service.heap" > "$tmp/$service-profile.log"
  done
  ./scripts/capture-management-profile.sh management cpu 1 "$tmp/management.cpu"
  go tool pprof -top "$tmp/management.cpu" > "$tmp/cpu-profile.log"
  for service in management public; do
    container=$(compose ps -q "$service")
    docker inspect --format '{{json .HostConfig.PortBindings}}' "$container" > "$tmp/$service-ports.json"
    python3 - "$tmp/$service-ports.json" <<'PY'
import json
import sys
with open(sys.argv[1], encoding='utf-8') as source:
    assert not json.load(source), 'profiling server must not publish TCP ports'
PY
  done
fi
python3 - "$result" "$status" <<'PY'
import datetime as dt
import json
from pathlib import Path
import sys

root = Path(sys.argv[1])
assert int(sys.argv[2]) != 0, 'a six-second low-rate run must not pass the isolated capacity target'
run = (root / 'run.txt').read_text()
assert 'instrumentation=diagnostic' in run, run
assert 'preparation_started_utc=' in run and 'readiness_utc=' in run, run
assert 'exit_status=' in run, run
before = json.loads((root / 'resources-before.json').read_text())
after = json.loads((root / 'resources-after.json').read_text())
assert not before['errors'] and not after['errors'], (before, after)
for resources in (before, after):
    for name in ('management', 'public'):
        service = resources['services'][name]
        assert service['nano_cpus'] == 500000000, service
        assert service['memory_bytes'] == 33554432, service
        assert service['image_id'], service
    for name in ('postgres', 'redis', 'proxy', 'jaeger', 'prometheus'):
        assert resources['services'][name]['image_id'], name
report = json.loads((root / 'report.json').read_text())
operation = report['operations']['get']
start = dt.datetime.fromisoformat(operation['window']['start'])
end = dt.datetime.fromisoformat(operation['window']['end'])
assert (end - start).total_seconds() == 6
assert operation['offered_rate_per_second'] == 10
assert operation['mean_completed_rate_per_second'] < 1000
assert not report['valid']
samples = [json.loads(line) for line in (root / 'diagnostics/samples.jsonl').read_text().splitlines()]
assert samples[0]['kind'] == 'collector_start'
assert any(sample['kind'] == 'container' and sample['service'] == 'management' and 'stats' in sample for sample in samples), samples
assert any(sample['kind'] == 'postgres' and 'data' in sample for sample in samples), samples
assert any(sample['kind'] == 'redis' and 'data' in sample for sample in samples), samples
assert any(sample['kind'] == 'prometheus' and 'data' in sample for sample in samples), samples
assert any(sample['kind'] == 'collector_stop' for sample in samples), samples
print(f'Diagnostic run artifacts checked: {root}')
PY
