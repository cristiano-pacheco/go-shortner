#!/bin/sh
set -eu
cd "$(dirname "$0")/../.."

# Isolated project and ports; never restore or remove a campaign database.
prefix="shortner-management-interface-$(date +%s)-$$"
tmp=$(mktemp -d)
export LOAD_PROJECT_PREFIX="$prefix" LOAD_DATASET=1m LOAD_SKIP_BUILD=1
export LOAD_PORT=${TEST_LOAD_PORT:-38088} LOAD_PROMETHEUS_PORT=${TEST_PROMETHEUS_PORT:-39092}
export LOAD_MANAGEMENT_READ_COUNT=50
compose() {
  docker compose -p "$prefix-1m" -f docker/docker-compose.load.yaml "$@"
}
cleanup() {
  status=$?
  trap - EXIT INT TERM
  compose down -v --remove-orphans >/dev/null 2>&1 || status=1
  if [ "$status" -ne 0 ]; then
    echo "Management load integration logs: $tmp" >&2
    cat "$tmp"/*.log >&2 2>/dev/null || true
  else
    rm -rf "$tmp"
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

LOAD_SEED_COUNT=100 LOAD_SEED_BATCH_SIZE=50 ./scripts/prepare-load-dataset.sh >"$tmp/seed.log" 2>&1
run() {
  name=$1
  scenario=$2
  prepare=$3
  warmup=${4:-1}
  vus=${5:-10}
  # Short runs intentionally cannot meet the 1,000 requests/s acceptance target.
  status=0
  SCENARIO="$scenario" LOAD_MANAGEMENT_PREPARE="$prepare" RATE=10 DURATION_SECONDS=1 \
    WARMUP_REQUESTS="$warmup" VUS="$vus" MAX_VUS="$vus" ./scripts/run-management-load.sh >"$tmp/$name.log" 2>&1 || status=$?
  result=$(sed -n 's/^Results: //p' "$tmp/$name.log" | head -n 1)
  if [ -z "$result" ] || [ ! -f "$result/report.json" ]; then
    echo "Missing report for $name (status $status)" >&2
    return 1
  fi
  printf '%s\n' "$result" > "$tmp/$name.path"
  printf '%s\n' "$status" > "$tmp/$name.status"
}

first=1
for scenario in create get list list_deep update delete mixed; do
  prepare=restore
  if [ "$first" = 1 ]; then prepare=snapshot; first=0; fi
  run "$scenario" "$scenario" "$prepare"
  python3 - "$(cat "$tmp/$scenario.path")/report.json" "$scenario" "$(cat "$tmp/$scenario.status")" <<'PY'
import json
import sys

path, scenario, status = sys.argv[1:]
with open(path, encoding='utf-8') as stream:
    report = json.load(stream)
operations = ('create', 'get', 'list', 'update', 'delete') if scenario == 'mixed' else (scenario,)
assert set(report['operations']) == set(operations), report
for operation in operations:
    result = report['operations'][operation]
    assert result['offered_rate_per_second'] == 10, result
    assert result['requests_per_iteration'] == 1, result
    assert result['started_in_window'] > 0, result
    assert result['completed_in_window'] > 0, result
    assert result['checks'] > 0 and result['failed_checks'] == 0, result
    assert result['http_failures'] == 0, result
    assert result['mean_completed_rate_per_second'] < 1000, result
    assert result['peak']['duration_seconds'] > 0, result
if scenario != 'mixed':
    assert int(status) != 0 and not report['valid'], report
    assert any('1000' in issue for issue in report['issues']), report
PY
done

# Inject a wrong but successful PATCH response by changing the stored value.
compose exec -T postgres psql -X -v ON_ERROR_STOP=1 -U shortner_load -d shortner_load <<'SQL' >"$tmp/inject.log"
CREATE FUNCTION management_test_wrong_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN NEW.destination := 'https://example.net/wrong'; RETURN NEW; END $$;
CREATE TRIGGER management_test_wrong_update BEFORE UPDATE ON links
FOR EACH ROW EXECUTE FUNCTION management_test_wrong_update();
SQL
run wrong-update update restore 0
python3 - "$(cat "$tmp/wrong-update.path")/report.json" "$(cat "$tmp/wrong-update.status")" <<'PY'
import json
import sys
with open(sys.argv[1], encoding='utf-8') as stream:
    report = json.load(stream)
assert int(sys.argv[2]) != 0 and not report['valid'], report
assert report['operations']['update']['failed_checks'] > 0, report
PY
compose exec -T postgres psql -X -v ON_ERROR_STOP=1 -U shortner_load -d shortner_load <<'SQL' >>"$tmp/inject.log"
DROP TRIGGER management_test_wrong_update ON links;
DROP FUNCTION management_test_wrong_update();
SQL
# A slow write with one VU must report discarded iterations, not pass silently.
compose exec -T postgres psql -X -v ON_ERROR_STOP=1 -U shortner_load -d shortner_load <<'SQL' >>"$tmp/inject.log"
CREATE FUNCTION management_test_slow_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN PERFORM pg_sleep(0.3); RETURN NEW; END $$;
CREATE TRIGGER management_test_slow_update BEFORE UPDATE ON links
FOR EACH ROW EXECUTE FUNCTION management_test_slow_update();
SQL
run dropped update restore 0 1
python3 - "$(cat "$tmp/dropped.path")/report.json" "$(cat "$tmp/dropped.status")" <<'PY'
import json
import sys
with open(sys.argv[1], encoding='utf-8') as stream:
    report = json.load(stream)
assert int(sys.argv[2]) != 0 and not report['valid'], report
assert report['dropped_iterations'] > 0, report
PY
compose exec -T postgres psql -X -v ON_ERROR_STOP=1 -U shortner_load -d shortner_load <<'SQL' >>"$tmp/inject.log"
DROP TRIGGER management_test_slow_update ON links;
DROP FUNCTION management_test_slow_update();
SQL

# Reserving more real IDs than available must fail before running k6.
if LOAD_MANAGEMENT_READ_COUNT=200 SCENARIO=get LOAD_MANAGEMENT_PREPARE=restore RATE=10 \
  DURATION_SECONDS=1 WARMUP_REQUESTS=1 VUS=10 MAX_VUS=10 \
  ./scripts/run-management-load.sh >"$tmp/exhaustion.log" 2>&1; then
  echo 'Insufficient initial population unexpectedly succeeded' >&2
  exit 1
fi
result=$(sed -n 's/^Results: //p' "$tmp/exhaustion.log" | head -n 1)
[ -n "$result" ] && [ -f "$result/preparation.log" ] && [ ! -f "$result/report.json" ]
printf 'Management interface scenarios, invalid response, dropped iterations and exhaustion checks passed.\n'
