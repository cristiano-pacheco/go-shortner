#!/bin/sh
# Capture on a diagnostic-only stack started with docker-compose.management-profile.yaml.
# Profiles contain sensitive runtime information and are not suitable for publication.
set -eu
cd "$(dirname "$0")/.."
if [ "$#" -ne 4 ]; then
  echo 'Usage: LOAD_DATASET=1m|10m capture-management-profile.sh management|public cpu|heap|goroutine seconds output-file' >&2
  exit 1
fi
case "${LOAD_DATASET:-}" in 1m|10m) ;; *) echo 'Invalid LOAD_DATASET' >&2; exit 1 ;; esac
case "$1" in management|public) ;; *) echo 'Invalid service' >&2; exit 1 ;; esac
case "$2" in cpu|heap|goroutine) ;; *) echo 'Invalid profile type' >&2; exit 1 ;; esac
case "$3" in *[!0-9]*|'') echo 'Invalid duration' >&2; exit 1 ;; esac
prefix=${LOAD_PROJECT_PREFIX:-shortner-load}
case "$prefix" in *[!a-z0-9_-]*|''|[-_]*) echo 'Invalid Compose project prefix' >&2; exit 1 ;; esac
umask 077
tmp=$(mktemp "${4}.partial.XXXXXXXX")
trap 'rm -f "$tmp"' EXIT HUP INT TERM
container=$(docker compose -p "$prefix-$LOAD_DATASET" -f docker/docker-compose.load.yaml \
  -f docker/docker-compose.management.yaml -f docker/docker-compose.management-profile.yaml ps -q "$1")
if [ -z "$container" ]; then echo 'Service is not running' >&2; exit 1; fi
docker exec "$container" /app/go-shortner profile-capture "$2" "$3" > "$tmp"
mv "$tmp" "$4"
trap - EXIT HUP INT TERM
