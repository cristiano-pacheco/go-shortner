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
LOAD_SEED_COUNT=${LOAD_SEED_COUNT:-1000000}
case "$LOAD_SEED_COUNT" in
  *[!0-9]*|'') echo 'LOAD_SEED_COUNT must be a non-negative integer' >&2; exit 1 ;;
esac
# Docker enforces network-name uniqueness across clients sharing the daemon.
lock=shortner-load-run-lock
if ! docker network create "$lock" >/dev/null; then
  echo "Could not acquire load-test lock ($lock); another run may be active." >&2
  exit 1
fi
trap 'docker network rm "$lock" >/dev/null || true' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
mkdir -p reports/load
LOAD_RESULTS_DIR=$(cd reports/load && mktemp -d "$(pwd)/run-XXXXXXXX")
LOAD_UID=$(id -u)
LOAD_GID=$(id -g)
export LOAD_RESULTS_DIR LOAD_UID LOAD_GID

compose up -d --build proxy grafana
wait_for "http://127.0.0.1:${LOAD_PROMETHEUS_PORT:-9092}/-/ready"
wait_for "http://127.0.0.1:${LOAD_GRAFANA_PORT:-3001}/api/health"
open_browser "http://127.0.0.1:${LOAD_GRAFANA_PORT:-3001}/d/shortener-operations?orgId=1&refresh=5s"
open_browser "http://127.0.0.1:${LOAD_GRAFANA_PORT:-3001}/d/load-test/load-test-live?orgId=1&refresh=5s"

printf 'Seeding up to %s links in the load database...\n' "$LOAD_SEED_COUNT"
compose exec -T postgres psql -X -U shortner_load -d shortner_load \
  -v target="$LOAD_SEED_COUNT" < docker/load/seed.sql

status=0
compose run --rm --no-deps k6 || status=$?
if [ -f "$LOAD_RESULTS_DIR/index.html" ]; then
  open_browser "$LOAD_RESULTS_DIR/index.html"
else
  echo "No k6 report was generated; check the output above." >&2
fi
exit "$status"
