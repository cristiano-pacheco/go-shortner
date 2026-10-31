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
