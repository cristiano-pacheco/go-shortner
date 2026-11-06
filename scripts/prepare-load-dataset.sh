#!/bin/sh
set -eu
cd "$(dirname "$0")/.."

case "${LOAD_DATASET:-}" in
  1m) default_target=1000000 ;;
  10m) default_target=10000000 ;;
  *) echo 'LOAD_DATASET must be 1m or 10m' >&2; exit 1 ;;
esac
LOAD_SEED_COUNT=${LOAD_SEED_COUNT:-$default_target}
LOAD_SEED_BATCH_SIZE=${LOAD_SEED_BATCH_SIZE:-10000}
for value in "$LOAD_SEED_COUNT" "$LOAD_SEED_BATCH_SIZE"; do
  case "$value" in
    *[!0-9]*|''|0) echo 'Seed count and batch size must be positive integers' >&2; exit 1 ;;
  esac
done
project_prefix=${LOAD_PROJECT_PREFIX:-shortner-load}
case "$project_prefix" in
  *[!a-z0-9_-]*|''|[-_]*) echo 'LOAD_PROJECT_PREFIX must be a valid lowercase Compose project prefix' >&2; exit 1 ;;
esac
compose() {
  docker compose -p "$project_prefix-$LOAD_DATASET" -f docker/docker-compose.load.yaml "$@"
}
psql_load() {
  compose exec -T postgres psql -X -v ON_ERROR_STOP=1 -U shortner_load -d shortner_load "$@"
}
mkdir -p reports/load/datasets
report_dir=$(mktemp -d "reports/load/datasets/${LOAD_DATASET}-XXXXXXXX")
report="$report_dir/preparation.txt"
start=$(date +%s)
printf 'dataset=%s target=%s batch_size=%s\nstarted_utc=%s\n' "$LOAD_DATASET" "$LOAD_SEED_COUNT" "$LOAD_SEED_BATCH_SIZE" "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" | tee "$report"
if [ "${LOAD_SKIP_BUILD:-0}" != 1 ]; then
  compose build migrate
fi
compose up -d --wait postgres
compose run --rm --no-deps migrate
while :; do
  output=$(psql_load -qAt -v target="$LOAD_SEED_COUNT" -v batch_size="$LOAD_SEED_BATCH_SIZE" < docker/load/seed.sql)
  seeded=$(printf '%s\n' "$output" | tail -n 1)
  printf 'committed=%s target=%s utc=%s\n' "$seeded" "$LOAD_SEED_COUNT" "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" | tee -a "$report"
  if [ "$seeded" -gt "$LOAD_SEED_COUNT" ]; then
    echo 'Existing seed exceeds target; use a fresh dataset instead of lowering the target.' >&2
    exit 1
  fi
  [ "$seeded" -lt "$LOAD_SEED_COUNT" ] || break
done
psql_load -c 'ANALYZE links' >/dev/null
psql_load <<'SQL' >> "$report"
SELECT count(*) AS physical_rows, count(*) FILTER (WHERE deleted_at IS NULL) AS visible_rows FROM links;
SELECT count(*) AS buckets, sum(total) AS maintained FROM link_counts;
SELECT pg_relation_size('links') AS table_bytes, pg_indexes_size('links') AS index_bytes;
SELECT n_live_tup, n_dead_tup, last_vacuum, last_autovacuum, last_analyze, last_autoanalyze, vacuum_count, autovacuum_count, analyze_count, autoanalyze_count FROM pg_stat_user_tables WHERE relid = 'links'::regclass;
SELECT attname, n_distinct, null_frac, avg_width FROM pg_stats WHERE schemaname = current_schema() AND tablename = 'links' ORDER BY attname;
DO $$ BEGIN
  IF (SELECT count(*) FROM link_counts) <> 64 OR EXISTS (
    SELECT 1 FROM link_counts c FULL JOIN
      (SELECT id % 64 AS bucket, count(*) AS total FROM links WHERE deleted_at IS NULL GROUP BY id % 64) a USING (bucket)
    WHERE c.bucket IS NULL OR c.total <> COALESCE(a.total, 0)
  ) THEN RAISE EXCEPTION 'link count invariant failed'; END IF;
END $$;
SQL
printf 'finished_utc=%s\nelapsed_seconds=%s\nreport=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$(($(date +%s) - start))" "$report" | tee -a "$report"
