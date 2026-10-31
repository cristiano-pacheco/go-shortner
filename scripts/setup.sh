#!/bin/sh
set -eu

cd "$(dirname "$0")/.."
env_file=${COMPOSE_ENV_FILE:-.env}

if [ ! -e "$env_file" ]; then
    if [ "${API_KEY+x}" = x ] || [ "${POSTGRES_PASSWORD+x}" = x ] ||
        [ "${POSTGRES_USER+x}" = x ] || [ "${POSTGRES_DB+x}" = x ]; then
        printf 'Unset API_KEY, POSTGRES_PASSWORD, POSTGRES_USER and POSTGRES_DB before generating an environment file.\n' >&2
        exit 1
    fi
    umask 077
    temp_file=$(mktemp "${env_file}.XXXXXX")
    trap 'rm -f "$temp_file"' EXIT HUP INT TERM
    postgres_password=$(openssl rand -hex 32)
    api_key=$(openssl rand -hex 32)
    while IFS= read -r line || [ -n "$line" ]; do
        case "$line" in
            POSTGRES_PASSWORD=*) printf 'POSTGRES_PASSWORD=%s\n' "$postgres_password" ;;
            DATABASE_PASSWORD=*) printf 'DATABASE_PASSWORD=%s\n' "$postgres_password" ;;
            API_KEY=*) printf 'API_KEY=%s\n' "$api_key" ;;
            *) printf '%s\n' "$line" ;;
        esac
    done < .env.example > "$temp_file"
    if ln "$temp_file" "$env_file" 2>/dev/null; then
        printf 'Created environment file: %s\n' "$env_file"
    elif [ -f "$env_file" ]; then
        printf 'Using existing environment file: %s\n' "$env_file"
    else
        printf 'Could not create environment file: %s\n' "$env_file" >&2
        exit 1
    fi
    rm -f "$temp_file"
    trap - EXIT HUP INT TERM
else
    printf 'Using existing environment file: %s\n' "$env_file"
fi

compose() {
    docker compose --env-file "$env_file" -f docker/docker-compose.yaml -f docker/docker-compose.app.yaml "$@"
}

# Compose applies shell overrides before values in the environment file.
api_key=$(compose config --environment | awk 'index($0, "API_KEY=") == 1 { print substr($0, 9); exit }')
if [ -z "$api_key" ]; then
    printf 'API_KEY must be set in the environment or environment file.\n' >&2
    exit 1
fi

compose up -d --wait postgres redis
COMPOSE_ENV_FILE="$env_file" make migrate
compose up -d app

port=$(compose port app 8080)
port=${port##*:}
base_url="http://127.0.0.1:$port"
attempt=0
until curl --fail --silent --output /dev/null --max-time 2 "$base_url/health"; do
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 60 ]; then
        printf 'Application did not become ready at %s.\n' "$base_url" >&2
        exit 1
    fi
    sleep 1
done

API_KEY="$api_key" BASE_URL="$base_url" ./scripts/seed/seed.sh
