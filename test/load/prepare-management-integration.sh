#!/bin/sh
set -eu
cd "$(dirname "$0")/../.."

# Use a disposable Compose project and never remove a campaign volume.
prefix="shortner-management-test-$(date +%s)-$$"
tmp=$(mktemp -d)
compose() {
  docker compose -p "$prefix-1m" -f docker/docker-compose.load.yaml "$@"
}
psql_load() {
  compose exec -T postgres psql -X -qAt -v ON_ERROR_STOP=1 -U shortner_load -d shortner_load "$@"
}
prepare_run() {
  LOAD_PROJECT_PREFIX="$prefix" LOAD_DATASET=1m LOAD_RESULTS_DIR="$tmp" \
    LOAD_MANAGEMENT_SEED=123 LOAD_MANAGEMENT_READ_COUNT=10 \
    LOAD_MANAGEMENT_WRITE_COUNT="${1:-20}" LOAD_MANAGEMENT_WARMUP_WRITES=3 \
    ./scripts/prepare-management-run.sh "$2"
}
cleanup() {
  status=$?
  trap - EXIT INT TERM
  if [ "$status" -ne 0 ]; then
    cat "$tmp"/*.log >&2 2>/dev/null || true
  fi
  compose down -v --remove-orphans >/dev/null 2>&1 || status=1
  rm -rf "$tmp"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if [ "${LOAD_SKIP_BUILD:-0}" != 1 ]; then
  compose build migrate >"$tmp/build.log" 2>&1
fi
LOAD_PROJECT_PREFIX="$prefix" LOAD_DATASET=1m LOAD_SEED_COUNT=100 \
  LOAD_SEED_BATCH_SIZE=20 LOAD_SKIP_BUILD=1 ./scripts/prepare-load-dataset.sh >"$tmp/seed.log" 2>&1
# Leave gaps in IDs and include inactive, expired and soft-deleted records.
psql_load <<'SQL'
DELETE FROM links WHERE id IN (3, 7, 9);
UPDATE links SET active = FALSE, expires_at = now() - interval '1 day' WHERE id = 5;
UPDATE links SET deleted_at = now() WHERE id = 6;
SQL
baseline=$(psql_load -c "SELECT md5(string_agg(row_to_json(l)::text, ',' ORDER BY id)) FROM links l")
sequence=$(psql_load -c 'SELECT last_value, is_called FROM links_id_seq')
prepare_run 20 snapshot >"$tmp/snapshot.log" 2>&1
cp "$tmp/management-dataset.json" "$tmp/first.json"
python3 - "$tmp/first.json" <<'PY'
import json
import sys
with open(sys.argv[1], encoding='utf-8') as source:
    data = json.load(source)
assert data['visible_count'] == 96
sets = [data['read_ids'], data['update_ids'], data['delete_ids']]
assert [len(part) for part in sets] == [10, 23, 23]
assert len(set(sum(sets, []))) == 56
assert not {'3', '6', '7', '9'} & set(sum(sets, []))
PY
# Simulate writes made by a repetition, including a newly created row.
psql_load <<'SQL'
UPDATE links SET destination = 'https://example.org/changed' WHERE id = 5;
UPDATE links SET deleted_at = now() WHERE id = 8;
INSERT INTO links (code, destination) VALUES ('run-created', 'https://example.org/created');
SQL
prepare_run 20 restore >"$tmp/restore.log" 2>&1
[ "$(psql_load -c "SELECT md5(string_agg(row_to_json(l)::text, ',' ORDER BY id)) FROM links l")" = "$baseline" ]
[ "$(psql_load -c 'SELECT last_value, is_called FROM links_id_seq')" = "$sequence" ]
cmp "$tmp/first.json" "$tmp/management-dataset.json"
psql_load <<'SQL' >/dev/null
SELECT 1 / CASE WHEN count(*) = 64 AND bool_and(c.total = COALESCE(a.total, 0)) THEN 1 ELSE 0 END
FROM link_counts c LEFT JOIN (SELECT id % 64 AS bucket, count(*) AS total FROM links WHERE deleted_at IS NULL GROUP BY id % 64) a USING (bucket);
SQL
if prepare_run 50 restore >"$tmp/exhaustion.log" 2>&1; then
  echo 'Expected preparation to reject an insufficient population.' >&2
  exit 1
fi
if prepare_run 20 snapshot >"$tmp/duplicate-snapshot.log" 2>&1; then
  echo 'Expected preparation to reject an existing snapshot.' >&2
  exit 1
fi
# k6 consumes JSON numbers for API IDs. Reject a population whose IDs cannot
# be represented exactly by JavaScript instead of silently addressing another row.
psql_load -c 'UPDATE management_load_baseline SET id = 9007199254740992 WHERE id = 1'
if prepare_run 20 restore >"$tmp/unsafe-id.log" 2>&1; then
  echo 'Expected preparation to reject IDs outside the JavaScript safe range.' >&2
  exit 1
fi
printf 'Management preparation snapshot, restore, disjoint pools and exhaustion passed.\n'
