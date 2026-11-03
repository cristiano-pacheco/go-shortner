#!/usr/bin/env bash
set -uo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cleanup_dir=$(mktemp -d "${TMPDIR:-/tmp}/shortner-e2e.XXXXXXXX") || exit 1
export SHORTNER_E2E_CLEANUP_DIR="$cleanup_dir"
export SHORTNER_E2E_BUILD_ID="$(basename "$cleanup_dir")"

cleanup() {
    local status=$? cleanup_failed=0 marker project pid_file pid command state containers container attempt locked
    trap - EXIT INT TERM
    shopt -s nullglob
    touch "$cleanup_dir/stopping"
    for marker in "$cleanup_dir"/project-shortner-e2e-*; do
        project=${marker##*/project-}
        locked=0
        for ((attempt=0; attempt<50; attempt++)); do
            if mkdir "$cleanup_dir/lock-$project" 2>/dev/null; then
                locked=1
                break
            fi
            sleep 0.1
        done
        if (( locked == 0 )); then
            echo "Failed to lock E2E project $project" >&2
            cleanup_failed=1
            continue
        fi
        for pid_file in "$cleanup_dir/pid-$project-"*; do
            pid=$(cat "$pid_file")
            for ((attempt=0; attempt<60; attempt++)); do
                state=$(ps -p "$pid" -o stat= 2>/dev/null || true)
                command=$(ps -p "$pid" -o command= 2>/dev/null || true)
                state=${state//[[:space:]]/}
                case "$state:$command" in
                    :*|Z*:*) break ;;
                    *"docker compose -p $project "*|*"scripts/e2e-compose.sh $cleanup_dir $project "*)
                        if (( attempt == 0 )); then
                            kill "$pid" 2>/dev/null || true
                        elif (( attempt == 30 )); then
                            kill -KILL "$pid" 2>/dev/null || true
                        fi
                        sleep 0.1
                        ;;
                    *) break ;;
                esac
            done
            if (( attempt == 60 )); then
                echo "Failed to stop E2E Compose process $pid" >&2
                cleanup_failed=1
                break
            fi
        done
        rmdir "$cleanup_dir/lock-$project"
        if (( attempt == 60 )); then
            continue
        fi
        if ! POSTGRES_PASSWORD=e2e-only docker compose -p "$project" \
            -f "$root/docker/docker-compose.yaml" \
            -f "$root/docker/docker-compose.app.yaml" \
            down --volumes --remove-orphans --rmi local; then
            echo "Failed to clean up E2E project $project" >&2
            cleanup_failed=1
        fi
    done
    if (( cleanup_failed == 0 )) && containers=$(docker ps -aq --filter "label=io.go-shortner.e2e-build=$SHORTNER_E2E_BUILD_ID"); then
        for container in $containers; do
            if ! docker rm -f "$container"; then
                echo "Failed to remove E2E build container $container" >&2
                cleanup_failed=1
            fi
        done
    else
        echo "Failed to list E2E build containers" >&2
        cleanup_failed=1
    fi
    if (( cleanup_failed == 0 )) && ! docker image prune -f --filter "label=io.go-shortner.e2e-build=$SHORTNER_E2E_BUILD_ID"; then
        echo "Failed to clean up E2E build images" >&2
        cleanup_failed=1
    fi
    if (( cleanup_failed == 0 )); then
        rm -f "$cleanup_dir"/project-shortner-e2e-* "$cleanup_dir"/pid-shortner-e2e-* "$cleanup_dir/stopping"
        rmdir "$cleanup_dir"
    else
        echo "E2E cleanup records remain in $cleanup_dir" >&2
        if (( status == 0 )); then
            status=1
        fi
    fi
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

cd "$root" || exit 1
go test -count=1 -v -timeout=10m -tags=e2e "$@" ./test/e2e/...
