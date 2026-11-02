# Operations

## Configuration

Install Docker and Docker Compose. Host execution also requires Go 1.27.3. `make setup` creates `.env` with local credentials if it does not exist. Never commit credentials. For host execution, `DATABASE_PASSWORD` must match `POSTGRES_PASSWORD`. Set `COMPOSE_ENV_FILE` when invoking Make to use a different file.

The default public address is `http://localhost:8080`. If you change `APP_PORT`, update `PUBLIC_BASE_URL` to the address clients should use. Compose binds service ports to localhost. See [.env.example](../.env.example) for all settings. Redis must be reachable when the application starts.

## Set up the project

Run `make setup` from the repository root. It creates `.env` if needed, starts PostgreSQL and Redis, applies migrations, starts the application and seeds example links. It leaves existing configuration and database data intact. The seed creates `example-go`, `example-postgres` and `example-redis`, skipping any code already reserved. Repeating setup does not overwrite links. The setup requires `openssl` and `curl`. When creating `.env`, unset any inherited `API_KEY`, `POSTGRES_PASSWORD`, `POSTGRES_USER` and `POSTGRES_DB` values.

## Run with Docker Compose

For manual startup, first create `.env` from `.env.example` and set local credentials. Then run:

```sh
make docker-up
make migrate
make docker-run
```

`docker-up` starts PostgreSQL and Redis. `migrate` builds the app image and applies migrations; `docker-run` starts the server. Migration is separate from serving HTTP. Stop the stack with `make docker-down`; the database volume remains. To remove the volume and all its data, run:

```sh
docker compose --env-file .env -f docker/docker-compose.yaml -f docker/docker-compose.app.yaml down -v
```

## Local observability services

The observability stack is optional and does not start with `make setup`. Start it on the same Compose network as the application:

```sh
docker compose --env-file .env -f docker/docker-compose.yaml -f docker/docker-compose.app.yaml --profile observability up -d prometheus grafana tempo
```

Grafana is available at `http://localhost:3000` and Prometheus at `http://localhost:9091`. Set `GRAFANA_PORT` or `PROMETHEUS_PORT` to change those localhost-only ports. Grafana's local default login is `admin` / `admin`; change the password when prompted. Prometheus and Tempo datasources are provisioned as `prometheus` and `tempo`. This local configuration is not intended for internet exposure.

Prometheus scrapes `app:9090` on the Compose network. Tempo accepts OTLP gRPC at `tempo:4317` and OTLP HTTP at `tempo:4318` on that network. The application does not yet expose metrics or export traces; those integrations are separate work. Neither service is required for application startup.

Stop these services with the same Compose files and `down`, or use `make docker-down` to stop the whole project. Named Prometheus, Grafana and Tempo volumes remain after `down`. Add `-v` to the Compose `down` command with the observability profile enabled to remove those volumes and the PostgreSQL volume. Prometheus retains data for 15 days, and Tempo retains trace blocks for 24 hours.

## Run on the host

Set `DATABASE_PASSWORD` in `.env`, then start the dependencies, apply migrations and run the server:

```sh
make docker-up
make migrate-local
make run
```

The host commands source `.env` as a shell file when present. Use only a trusted file with shell-compatible values. Keep `DATABASE_HOST`, `DATABASE_PORT`, `REDIS_URL` and `HTTP_ADDR` set to the host-accessible values in `.env.example`. Stop the server with Ctrl-C and the dependencies with `make docker-down`.

## Checks

`go build ./...` builds the application. `make test` runs the default Go tests. `make test-e2e` runs the HTTP end-to-end suite with Docker Compose and real PostgreSQL and Redis. It creates and removes its own Compose stack. CI runs formatting, build, end-to-end tests, lint, vulnerability scanning and nil-safety analysis. The Makefile also exposes `make test-integration`, but its `test/integration` directory is absent in this checkout. See [Make commands](agents/make-commands.md) for prerequisites and side effects.

## Internet exposure

This is a portfolio application, not an antiabuse service. Configure proxy-level rate limiting and other abuse protection before exposing it to the internet. The application has no rate limiting, blacklist or phishing detection. Localhost and private IP destinations are allowed.
