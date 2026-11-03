#!/usr/bin/env bash
set -eu

cleanup_dir=$1
project=$2
shift 2
lock="$cleanup_dir/lock-$project"

# A cleanup that has started must prevent a delayed child from launching Docker.
for ((attempt=0; attempt<50; attempt++)); do
    if mkdir "$lock" 2>/dev/null; then
        break
    fi
    if [[ -e "$cleanup_dir/stopping" ]]; then
        exit 1
    fi
    sleep 0.1
done
if (( attempt == 50 )); then
    exit 1
fi
trap 'rmdir "$lock" 2>/dev/null || true' EXIT
if [[ -e "$cleanup_dir/stopping" ]]; then
    exit 1
fi
printf '%s\n' "$$" > "$cleanup_dir/pid-$project-$$"
rmdir "$lock"
trap - EXIT
exec docker "$@"
