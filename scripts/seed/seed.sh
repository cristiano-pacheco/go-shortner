#!/bin/sh
set -eu

if [ -z "${API_KEY:-}" ]; then
    echo "Set API_KEY before running the seed." >&2
    exit 1
fi

base_url=${BASE_URL:-http://localhost:8080}
base_url=${base_url%/}

seed_link() {
    alias=$1
    destination=$2
    status=$(printf 'X-API-Key: %s\n' "$API_KEY" | curl --silent --show-error \
        --output /dev/null --write-out '%{http_code}' --max-time 10 \
        -H @- -H 'Content-Type: application/json' \
        --data "{\"alias\":\"$alias\",\"destination\":\"$destination\"}" \
        "$base_url/api/v1/links") || return 1

    case "$status" in
        201) printf 'Created %s\n' "$alias" ;;
        409) printf 'Skipped reserved code %s\n' "$alias" ;;
        *) printf 'Failed to create %s (HTTP %s)\n' "$alias" "$status" >&2; return 1 ;;
    esac
}

seed_link example-go https://go.dev/
seed_link example-postgres https://www.postgresql.org/
seed_link example-redis https://redis.io/
