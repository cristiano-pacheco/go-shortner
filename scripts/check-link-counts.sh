#!/bin/sh
set -eu

cd "$(dirname "$0")/.."

compose() {
  docker compose -p shortner-load -f docker/docker-compose.load.yaml "$@"
}

# All sessions exclude public from search_path. A missing smoke table therefore
# fails instead of accidentally reading or mutating the load dataset.
schema="link_counts_check_$(date +%s)_$$"
psql_check() {
  compose exec -T -e "PGOPTIONS=-c search_path=$schema" postgres \
    psql -X -v ON_ERROR_STOP=1 -U shortner_load -d shortner_load "$@"
}

cleanup() {
  status=$?
  trap - EXIT INT TERM
  if ! psql_check -c "DROP SCHEMA \"$schema\" CASCADE"; then
    printf 'Could not remove smoke schema %s; remove it manually.\n' "$schema" >&2
    if [ "$status" -eq 0 ]; then status=1; fi
  fi
  exit "$status"
}

# Requires the existing load PostgreSQL service; never starts or rebuilds it.
psql_check -c "CREATE SCHEMA \"$schema\""
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

migration=internal/modules/shortener/migration
contract=test/migration/link_counts.sql
phase() {
  printf '\\set stage %s\n' "$1"
  cat "$contract"
  printf '\n'
}

# Stream the production migration sources unchanged, with fixtures/assertions
# between phases. No duplicated migration SQL or schema_migrations changes.
{
  cat "$migration/20261030000100_create_links.up.sql"
  printf '\n'
  phase seed
  cat "$migration/20261105000100_create_link_counts.up.sql"
  printf '\n'
  phase verify
  cat "$migration/20261105000100_create_link_counts.down.sql"
  printf '\n'
  phase down
  cat "$migration/20261105000100_create_link_counts.up.sql"
  printf '\n'
  phase truncate
} | psql_check

printf 'Link-count migration contract passed in isolated schema %s.\n' "$schema"
