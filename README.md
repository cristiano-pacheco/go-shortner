# URL shortener

A Go modular monolith for managing short links and redirecting visitors. PostgreSQL stores links and Redis caches public lookups. See the [glossary](GLOSSARY.md) and [architecture decisions](docs/architecture.md).

## Run with Compose

Install Docker with Compose. For host development, install Go 1.27.3. The examples also use `curl`.

Create an environment file outside the repository and set nonempty `POSTGRES_PASSWORD` and `API_KEY` values. Do not commit it. For host execution, set `DATABASE_PASSWORD` to the same value as `POSTGRES_PASSWORD`.

```sh
cp .env.example "$HOME/shortener.env"
# Edit $HOME/shortener.env and choose local credentials.
docker compose --env-file "$HOME/shortener.env" -f docker/docker-compose.yaml -f docker/docker-compose.app.yaml build app
docker compose --env-file "$HOME/shortener.env" -f docker/docker-compose.yaml -f docker/docker-compose.app.yaml run --rm app migrate
docker compose --env-file "$HOME/shortener.env" -f docker/docker-compose.yaml -f docker/docker-compose.app.yaml up -d app
```

The migration command starts the required PostgreSQL and Redis services and applies the schema before the HTTP server starts. Redis must be reachable at startup. The default public address is `http://localhost:8080`. If you change `APP_PORT`, update `PUBLIC_BASE_URL` to the public address clients should use. Compose binds service ports to localhost. See [.env.example](.env.example) for database, Redis and host settings.

Stop the stack with the same environment file and Compose files:

```sh
docker compose --env-file "$HOME/shortener.env" -f docker/docker-compose.yaml -f docker/docker-compose.app.yaml down
```

Add `-v` to remove the PostgreSQL volume and its data. For host execution, export the host settings in the environment, start the dependency services with `docker compose --env-file "$HOME/shortener.env" -f docker/docker-compose.yaml up -d`, then run `go run ./main.go migrate` and `make run`. The retained `make migrate` target uses an obsolete command name. The Makefile also reads `.env` if present. Migrations are explicit and do not run as part of server startup.

## HTTP API

Management routes require `X-API-Key`. Set `API_KEY` in your shell to the value configured for the server. Avoid putting credentials in shell history or scripts. The following examples assume the default address and a new database. Replace `1` with the `id` returned by creation; sequence values are not guaranteed to be consecutive.

```sh
# Create a link with a custom alias. Omit alias for a generated code.
curl -i -X POST http://localhost:8080/api/v1/links \
  -H "X-API-Key: $API_KEY" -H 'Content-Type: application/json' \
  -d '{"destination":"https://example.com","alias":"example"}'

curl -i http://localhost:8080/api/v1/links/1 -H "X-API-Key: $API_KEY"
curl -i 'http://localhost:8080/api/v1/links?page=1&page_size=20' -H "X-API-Key: $API_KEY"
curl -i -X PATCH http://localhost:8080/api/v1/links/1 \
  -H "X-API-Key: $API_KEY" -H 'Content-Type: application/json' \
  -d '{"destination":"https://example.org","expires_at":null}'
curl -i http://localhost:8080/example
curl -I http://localhost:8080/example
curl -i -X DELETE http://localhost:8080/api/v1/links/1 -H "X-API-Key: $API_KEY"
```

Creation returns 201. Get, list and update return 200, and delete returns 204. Create, get, list and update responses contain `data`; link representations include `id`, `code`, `destination`, `short_url`, `active`, `expires_at`, `created_at` and `updated_at`. IDs are positive signed 64-bit integers. The configured public base URL, not the request Host header, determines `short_url`.

A destination must be an absolute HTTP or HTTPS URL with a hostname, without user credentials, and at most 4096 bytes. Localhost and private IP addresses are allowed. The service does not probe destinations or detect phishing. An alias has 3 to 32 ASCII letters, digits, hyphens or underscores. Codes are case-sensitive, and `api` and `health` are reserved. Generated codes have eight alphanumeric characters. Deleted codes remain reserved.

An optional `expires_at` is an RFC3339 timestamp in the future. PATCH accepts `destination`, `active` and `expires_at`; omitted fields remain unchanged, while `"expires_at": null` clears expiration. Set `active` to `false` or `true` to deactivate or reactivate. The code cannot be changed. List includes inactive and expired links, ordered newest first with an ID tie-breaker. Pages start at 1, default to 20 items and accept `page_size` from 1 to 100. The response includes `items`, `page`, `page_size` and `total`.

Public GET and HEAD requests need no key. Available links return 302, `Location` and `Cache-Control: no-store`; HEAD has no response body. Unknown, inactive, expired and deleted links return 404, subject to the [cache consistency bound](docs/architecture.md#redirect-cache) after changes. Errors use English messages in an `error` object with `code` and `message`. Invalid input returns 400, missing or invalid keys 401, missing links 404, code conflicts 409, oversized bodies 413, non-JSON content types 415 and unexpected failures 500.

## Checks

`go build ./...` builds the application. `make test` runs the default Go tests. `make test-e2e` runs the HTTP end-to-end suite with Docker Compose and real PostgreSQL and Redis. It creates and removes its own Compose stack. CI runs formatting, build, end-to-end tests, lint, vulnerability scanning and nil-safety analysis. The Makefile also exposes `make test-integration`, but its `test/integration` directory is absent in this checkout. See [Make commands](docs/agents/make-commands.md) for prerequisites and side effects.

## Internet exposure

This is a portfolio application, not an antiabuse service. Configure proxy-level rate limiting and other abuse protection before exposing it to the internet. The application has no rate limiting, blacklist or phishing detection.
