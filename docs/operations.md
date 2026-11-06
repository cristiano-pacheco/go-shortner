# Operations

## Configuration

Install Docker and Docker Compose. Host execution also requires Go 1.27.3. `make setup` creates `.env` with local credentials if it does not exist. Never commit credentials. For host execution, `DATABASE_PASSWORD` must match `POSTGRES_PASSWORD`. Set `COMPOSE_ENV_FILE` when invoking Make to use a different file.

PostgreSQL and Redis use the Bricks configuration sections `app.database` and `app.redis` in `config/base.yaml`. `DATABASE_SSLMODE` is a boolean: `false` disables PostgreSQL TLS and `true` requires it; `verify-ca` and `verify-full` are not supported by the Bricks database module. Existing `.env` files using `disable` or `require` must be updated to `false` or `true` respectively. Redis uses the `db` and `password` fields in `app.redis` (the latter reads `REDIS_PASSWORD`), overriding values embedded in `REDIS_URL`. A nonempty Redis password enables TLS in Bricks; configure a TLS-enabled Redis endpoint when using one. Avoid placing credentials in Redis URLs, as connection errors can expose them. Bricks' PostgreSQL DSN builder does not escape all credential characters, so credentials containing spaces or DSN-special characters may not work.

The public server uses Bricks' Chi Fx module and reads `app.http` in `config/base.yaml`. `HTTP_ADDR` supplies its address and defaults to `:8080` when unset. Read and write timeouts are 15 seconds, the idle timeout is 60 seconds, and graceful server shutdown is bounded at 20 seconds. Bricks does not expose the former separate 5-second request-header timeout: its 15-second read timeout applies to headers and body together. The module does not report unexpected main-listener serve failures to Fx. The application waits for active handlers after server shutdown, within the overall 30-second shutdown budget; unlike the former server, Bricks does not forcibly close active connections when its graceful shutdown times out. The existing `/health` endpoint remains the health check; Bricks' built-in `/healthz` is overridden to preserve that existing short-code namespace.

Compose binds management to `localhost:8080` through `APP_PORT` and redirects to `localhost:8081` through `PUBLIC_PORT`. Set `PUBLIC_BASE_URL` to the redirect address. Update existing `.env` files that still point it at port 8080.

Each Go server has a 32 MiB limit, `GOMEMLIMIT=24MiB` and `GOMAXPROCS=1`. Management has a 0.5-CPU cap. Public has a 1-CPU cap. The combined 64 MiB excludes dependencies. See [.env.example](../.env.example) for settings.

## Set up the project

Run `make setup` from the repository root. It creates `.env` if needed, starts PostgreSQL and Redis, applies migrations, starts the application and observability services, and seeds example links. It leaves existing configuration and database data intact. The seed creates `example-go`, `example-postgres` and `example-redis`, skipping any code already reserved. Repeating setup does not overwrite links. The setup requires `openssl` and `curl`. When creating `.env`, unset any inherited `API_KEY`, `POSTGRES_PASSWORD`, `POSTGRES_USER` and `POSTGRES_DB` values.

## Run with Docker Compose

For manual startup, first create `.env` from `.env.example` and set local credentials. Then run:

```sh
make docker-up
make migrate
make docker-run
```

`docker-up` starts PostgreSQL and Redis. `migrate` builds the app image and applies migrations; `docker-run` starts the independent `management-server` (`app`) and `public-server` (`public`) processes. Migration is separate from serving HTTP. Stop the stack with `make docker-down`; the database volume remains. To remove the volume and all its data, run:

```sh
docker compose --env-file .env -f docker/docker-compose.yaml -f docker/docker-compose.app.yaml down -v
```

## Local observability services

`make setup` starts Prometheus, Grafana and Jaeger alongside the application. To start only the observability services after the application is running, use:

```sh
docker compose --env-file .env -f docker/docker-compose.yaml -f docker/docker-compose.app.yaml --profile observability up -d prometheus grafana jaeger
```

Grafana is available at `http://localhost:3000`, Prometheus at `http://localhost:9091`, and Jaeger at `http://localhost:16686`. Set `GRAFANA_PORT`, `PROMETHEUS_PORT` or `JAEGER_PORT` to change those localhost-only ports. Grafana's local default login is `admin` / `admin`; change the password when prompted. Prometheus and Jaeger datasources are provisioned as `prometheus` and `jaeger`. This local configuration is not intended for internet exposure.

Prometheus scrapes `app:9090` and `public:9090` with role labels. Management metrics use host port `METRICS_PORT`, default 9090. Public metrics use `PUBLIC_METRICS_PORT`, default 9093. Check metrics and targets:

```sh
curl --fail http://localhost:9090/metrics
curl --fail 'http://localhost:9091/api/v1/targets?state=active'
```

Generate traffic first, for example `curl -i http://localhost:8081/example-go`. Open Jaeger, select service `go-shortner` and search for traces. Alternatively, use **Explore traces in Jaeger** in Grafana's **Shortener operations** dashboard. The dashboard can show empty panels when there are no requests; that alone does not indicate an outage. The provisioned datasource and dashboard files are in `docker/observability/`. Grafana reads them at startup without a manual import.

Jaeger accepts OTLP gRPC at `jaeger:4317` and OTLP HTTP at `jaeger:4318` on the Compose network. The application exports traces over gRPC. Neither Prometheus nor Jaeger is required for application startup.

Stop these services with the same Compose files and `down`, or use `make docker-down` to stop the whole project. Named Prometheus and Grafana volumes remain after `down`. Add `-v` to the Compose `down` command with the observability profile enabled to remove those volumes and the PostgreSQL volume. Prometheus retains data for 15 days. Jaeger uses bounded in-memory storage for local development: traces are lost when its container restarts. Existing Tempo volumes are not migrated or removed automatically.

## Tracing configuration

The application declares Bricks `trace.Module` in `cmd/server.go`. Bricks initializes and stops the go-otel provider through Fx; application code does not initialize a second provider. `config/base.yaml` supplies `app.open-telemetry`: `trace_enabled`, `trace_url`, `exporter_type`, `insecure`, `sample_rate`, `batch_timeout`, `max_batch_size`, `max_queue_size`, `export_timeout` and `shutdown_timeout`. The supplied configuration enables gRPC export to `jaeger:4317`, uses root sampling rate `1.0`, a queue of 2048 spans, a batch size of 512, a 3-second export timeout and a 5-second shutdown timeout. Sampling respects a parent decision; set `sample_rate` between 0 and 1 for root spans. Set `trace_enabled: false` to disable export. Invalid local configuration rejects startup. Export errors, including an unavailable Jaeger backend, do not fail business requests. HTTP drains before tracing stops, within the application's 30-second shutdown budget.

Bricks supports independent `app.database.tracing.enabled` and `app.redis.tracing.enabled` switches, both disabled by default. Consumers must also declare the corresponding Bricks instrumentation modules. This application does not currently declare those modules or enable the switches. It creates use-case, repository and cache spans and extracts incoming W3C trace context, but does not create HTTP server spans. Plugin controls can disable SQL and Redis statement capture, but third-party exception or status messages can still contain sensitive details. Do not assume all traces are sanitized.

## Run on the host

Set `DATABASE_PASSWORD` in `.env`, then start the dependencies, apply migrations and run the server:

```sh
make docker-up
make migrate-local
make run
# In another terminal, use a separate listener:
PUBLIC_HTTP_ADDR=127.0.0.1:8081 make run-public
```

`make run` starts management. `make run-public` starts redirects without an API key, on HTTP port 8081 and metrics port 9093. It selects `config/public-local.yaml`. Override with `PUBLIC_HTTP_ADDR` and `PUBLIC_APP_ENV`. Keep both listeners distinct from management. Host commands source `.env` when present. Use only a trusted file with shell-compatible values. Keep `DATABASE_HOST`, `DATABASE_PORT`, `REDIS_URL` and `HTTP_ADDR` set to the host-accessible values in `.env.example`. The supplied trace endpoint is only reachable on the Compose network. For host execution, configure a reachable OTLP endpoint in YAML or disable tracing. Stop the server with Ctrl-C and the dependencies with `make docker-down`.

## Checks

`go build ./...` builds the application. `make test` runs the default Go tests. `make test-e2e` runs the HTTP end-to-end suite with Docker Compose and real PostgreSQL and Redis. It creates and removes its own Compose stack. CI runs formatting, build, end-to-end tests, lint, vulnerability scanning and nil-safety analysis. `make test-integration` runs tagged use-case/resource integration tests; `make test-integration-race` adds race detection. See [Make commands](agents/make-commands.md) for prerequisites and side effects.

## Internet exposure

Configure proxy-level rate limiting and other abuse protection before exposing it to the internet. The application has no rate limiting, blacklist or phishing detection. Localhost and private IP destinations are allowed.
