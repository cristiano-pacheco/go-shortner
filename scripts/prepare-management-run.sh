#!/bin/sh
# Run against an isolated load dataset, never against a production database.
set -eu
cd "$(dirname "$0")/.."

case "${1:-}" in
  snapshot|restore) action=$1 ;;
  *) echo 'Usage: prepare-management-run.sh snapshot|restore' >&2; exit 1 ;;
esac
case "${LOAD_DATASET:-}" in
  1m|10m) ;;
  *) echo 'LOAD_DATASET must be 1m or 10m' >&2; exit 1 ;;
esac
project_prefix=${LOAD_PROJECT_PREFIX:-shortner-load}
case "$project_prefix" in
  *[!a-z0-9_-]*|''|[-_]*) echo 'Invalid Compose project prefix' >&2; exit 1 ;;
esac
: "${LOAD_RESULTS_DIR:?Set LOAD_RESULTS_DIR to the directory mounted at /results}"
LOAD_MANAGEMENT_SEED=${LOAD_MANAGEMENT_SEED:-20261106}
LOAD_MANAGEMENT_READ_COUNT=${LOAD_MANAGEMENT_READ_COUNT:-100000}
LOAD_MANAGEMENT_WRITE_COUNT=${LOAD_MANAGEMENT_WRITE_COUNT:-303000}
LOAD_MANAGEMENT_WARMUP_WRITES=${LOAD_MANAGEMENT_WARMUP_WRITES:-0}
for value in "$LOAD_MANAGEMENT_SEED" "$LOAD_MANAGEMENT_READ_COUNT" "$LOAD_MANAGEMENT_WRITE_COUNT" "$LOAD_MANAGEMENT_WARMUP_WRITES"; do
  case "$value" in *[!0-9]*|'') echo 'Seed and pool sizes must be non-negative integers' >&2; exit 1 ;; esac
done
if [ "$LOAD_MANAGEMENT_READ_COUNT" -eq 0 ] || [ "$LOAD_MANAGEMENT_WRITE_COUNT" -eq 0 ]; then
  echo 'Read and write pool sizes must be positive' >&2; exit 1
fi
write_count=$((LOAD_MANAGEMENT_WRITE_COUNT + LOAD_MANAGEMENT_WARMUP_WRITES))
compose() {
  docker compose -p "$project_prefix-$LOAD_DATASET" -f docker/docker-compose.load.yaml "$@"
}
psql_load() {
  compose exec -T postgres psql -X -v ON_ERROR_STOP=1 -U shortner_load -d shortner_load "$@"
}
mkdir -p "$LOAD_RESULTS_DIR"
# Stop all HTTP writers and readers. The caller restarts and waits for readiness.
compose stop proxy management public
compose up -d --wait postgres redis
if [ "$action" = snapshot ]; then
  # Refuse to silently replace a baseline with a dataset altered by a prior run.
  psql_load <<'SQL'
BEGIN;
LOCK TABLE links IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
  IF to_regclass('management_load_baseline') IS NOT NULL THEN
    RAISE EXCEPTION 'management baseline already exists; restore it instead of replacing it';
  END IF;
  IF (SELECT count(*) FROM link_counts) <> 64 OR EXISTS (
    SELECT 1 FROM link_counts c FULL JOIN
      (SELECT id % 64 AS bucket, count(*) AS total FROM links WHERE deleted_at IS NULL GROUP BY id % 64) a USING (bucket)
    WHERE c.bucket IS NULL OR c.total <> COALESCE(a.total, 0)
  ) THEN RAISE EXCEPTION 'link count invariant failed'; END IF;
END $$;
CREATE TABLE management_load_baseline AS TABLE links;
CREATE TABLE management_load_sequence AS SELECT last_value, is_called FROM links_id_seq;
COMMIT;
SQL
fi
# Restoring the complete baseline removes created rows, reverses updates and
# soft deletes, and restores the exact pre-run sequence and count buckets.
psql_load <<'SQL'
BEGIN;
LOCK TABLE links IN ACCESS EXCLUSIVE MODE;
TRUNCATE links;
INSERT INTO links (id, code, destination, active, expires_at, created_at, updated_at, deleted_at)
SELECT id, code, destination, active, expires_at, created_at, updated_at, deleted_at
FROM management_load_baseline;
SELECT setval('links_id_seq', last_value, is_called) FROM management_load_sequence;
DO $$ BEGIN
  IF (SELECT count(*) FROM link_counts) <> 64 OR EXISTS (
    SELECT 1 FROM link_counts c FULL JOIN
      (SELECT id % 64 AS bucket, count(*) AS total FROM links WHERE deleted_at IS NULL GROUP BY id % 64) a USING (bucket)
    WHERE c.bucket IS NULL OR c.total <> COALESCE(a.total, 0)
  ) THEN RAISE EXCEPTION 'link count invariant failed after restore'; END IF;
END $$;
COMMIT;
ANALYZE links;
SQL
# Direct database restoration bypasses the application's cache invalidation.
compose exec -T redis redis-cli FLUSHDB >/dev/null
output="$LOAD_RESULTS_DIR/management-dataset.json"
tmp="$output.tmp.$$"
trap 'rm -f "$tmp"' EXIT HUP INT TERM
# Hash ordering samples without assuming IDs are contiguous or public codes.
# Split a single deterministic permutation into disjoint read and write pools.
if ! psql_load -qAt -v seed="$LOAD_MANAGEMENT_SEED" -v reads="$LOAD_MANAGEMENT_READ_COUNT" -v writes="$write_count" > "$tmp" <<'SQL'
SELECT 1 / CASE WHEN count(*) >= :reads::bigint + 2 * :writes::bigint
                      AND COALESCE(bool_and(id > 0 AND id <= 9007199254740991), false)
                THEN 1 ELSE 0 END
FROM links WHERE deleted_at IS NULL \g /dev/null
WITH ranked AS MATERIALIZED (
  SELECT id, row_number() OVER (ORDER BY md5(:'seed' || ':' || id::text), id) AS n
  FROM links WHERE deleted_at IS NULL
), selected AS MATERIALIZED (
  SELECT id, n FROM ranked WHERE n <= :reads::bigint + 2 * :writes::bigint
)
SELECT json_build_object(
  'seed', :'seed',
  'visible_count', (SELECT count(*) FROM links WHERE deleted_at IS NULL),
  'read_ids', COALESCE(json_agg(id::text ORDER BY n) FILTER (WHERE n <= :reads::bigint), '[]'::json),
  'update_ids', COALESCE(json_agg(id::text ORDER BY n) FILTER (WHERE n > :reads::bigint AND n <= :reads::bigint + :writes::bigint), '[]'::json),
  'delete_ids', COALESCE(json_agg(id::text ORDER BY n) FILTER (WHERE n > :reads::bigint + :writes::bigint), '[]'::json)
) FROM selected;
SQL
then
  echo 'Could not select pools: check population size, ID range, and database errors.' >&2
  exit 1
fi
mv "$tmp" "$output"
trap - EXIT HUP INT TERM
printf 'prepared_utc=%s\naction=%s\nseed=%s\nread_ids=%s\nupdate_ids=%s\ndelete_ids=%s\n' \
  "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$action" "$LOAD_MANAGEMENT_SEED" "$LOAD_MANAGEMENT_READ_COUNT" "$write_count" "$write_count" > "$LOAD_RESULTS_DIR/management-preparation.txt"
psql_load -c 'SELECT count(*) AS physical_rows, count(*) FILTER (WHERE deleted_at IS NULL) AS visible_rows FROM links' \
  -c 'SELECT sum(total) AS maintained FROM link_counts' >> "$LOAD_RESULTS_DIR/management-preparation.txt"
printf 'Prepared %s; restart services and wait for readiness before warming up.\n' "$output"
