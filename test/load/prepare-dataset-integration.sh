#!/bin/sh
set -eu

cd "$(dirname "$0")/../.."

# Each test gets its own Compose projects and volumes. Never remove campaign data.
prefix="shortner-seed-test-$(date +%s)-$$"
tmp=$(mktemp -d)
seed_pid=
compose() {
  dataset=$1
  shift
  docker compose -p "$prefix-$dataset" -f docker/docker-compose.load.yaml "$@"
}
psql_dataset() {
  dataset=$1
  shift
  compose "$dataset" exec -T postgres psql -X -qAt -v ON_ERROR_STOP=1 \
    -U shortner_load -d shortner_load "$@"
}
prepare() {
  dataset=$1
  target=$2
  LOAD_PROJECT_PREFIX="$prefix" LOAD_DATASET="$dataset" LOAD_SEED_COUNT="$target" \
    LOAD_SEED_BATCH_SIZE=20 LOAD_SKIP_BUILD=1 ./scripts/prepare-load-dataset.sh
}
cleanup() {
  status=$?
  trap - EXIT INT TERM
  if [ -n "$seed_pid" ]; then
    kill "$seed_pid" 2>/dev/null || true
    wait "$seed_pid" 2>/dev/null || true
  fi
  for dataset in 1m 10m; do
    compose "$dataset" down -v --remove-orphans >/dev/null 2>&1 || status=1
  done
  if [ "$status" -ne 0 ]; then
    echo 'Dataset preparation logs:' >&2
    cat "$tmp"/*.log >&2 2>/dev/null || true
  fi
  rm -rf "$tmp"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# Build once unless the caller has already built the current application image.
if [ "${LOAD_SKIP_BUILD:-0}" != 1 ]; then
  compose 1m build migrate >"$tmp/build.log" 2>&1
fi
prepare 1m 1 >"$tmp/initial.log" 2>&1

# Slow inserts make it possible to stop PostgreSQL after a committed batch and
# while a subsequent batch is in flight, without modifying the seed itself.
psql_dataset 1m <<'SQL'
CREATE FUNCTION test_slow_seed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  PERFORM pg_sleep(0.01);
  RETURN NEW;
END $$;
CREATE TRIGGER test_slow_seed BEFORE INSERT ON links
FOR EACH ROW EXECUTE FUNCTION test_slow_seed();
SQL
prepare 1m 200 >"$tmp/interrupted.log" 2>&1 &
seed_pid=$!
partial=0
attempt=0
while [ "$partial" -lt 21 ]; do
  if ! kill -0 "$seed_pid" 2>/dev/null; then
    echo 'Seed exited before a committed batch could be interrupted.' >&2
    exit 1
  fi
  partial=$(psql_dataset 1m -c 'SELECT count(*) FROM links')
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 120 ]; then
    echo 'Timed out waiting for a committed seed batch.' >&2
    exit 1
  fi
  sleep 0.2
done
if [ "$partial" -ge 200 ]; then
  echo 'Seed completed before interruption.' >&2
  exit 1
fi
container=$(compose 1m ps -q postgres)
docker kill "$container" >/dev/null
if wait "$seed_pid"; then
  echo 'Interrupted seed unexpectedly succeeded.' >&2
  seed_pid=
  exit 1
fi
seed_pid=
compose 1m up -d --wait postgres >"$tmp/restart.log" 2>&1
partial=$(psql_dataset 1m -c 'SELECT count(*) FROM links')
if [ "$partial" -lt 21 ] || [ "$partial" -ge 200 ]; then
  echo "Unexpected persisted count after interruption: $partial" >&2
  exit 1
fi
psql_dataset 1m -c 'DROP TRIGGER test_slow_seed ON links; DROP FUNCTION test_slow_seed()'

prepare 1m 200 >"$tmp/resume.log" 2>&1
prepare 1m 200 >"$tmp/repeat.log" 2>&1
prepare 10m 75 >"$tmp/independent.log" 2>&1

verify() {
  dataset=$1
  expected=$2
  psql_dataset "$dataset" -v expected="$expected" <<'SQL'
SELECT 1 / (CASE WHEN count(*) = :expected AND count(DISTINCT code) = :expected
                      AND count(*) FILTER (WHERE deleted_at IS NULL) = :expected
                 THEN 1 ELSE 0 END)
FROM links;
SELECT 1 / (CASE WHEN count(*) = 64 AND COALESCE(sum(counters.total), 0) = :expected
                      AND bool_and(counters.total = COALESCE(actual.total, 0))
                 THEN 1 ELSE 0 END)
FROM link_counts AS counters
LEFT JOIN (
  SELECT id % 64 AS bucket, count(*) AS total
  FROM links WHERE deleted_at IS NULL GROUP BY id % 64
) AS actual USING (bucket);
SQL
}
verify 1m 200 >/dev/null
verify 10m 75 >/dev/null
printf 'Dataset seed interruption, resume, reconciliation and isolation passed.\n'
