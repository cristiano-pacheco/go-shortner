# Make commands

Run commands from the repository root. Use the table to select a target, then check its prerequisites and limitations below. `Makefile` is the source of truth for recipes, tool versions, and flags; check it when changing or diagnosing a target.

## Command reference

| Command | Purpose and effects |
| --- | --- |
| `make` / `make all` | Install development tools, run lint and tests, then generate coverage. Tests run again during coverage. |
| `make install-libs` | Install golangci-lint, govulncheck, mockery, swag, and nilaway with `go install`. Requires network access and writes to Go's binary directory. |
| `make setup` | Create `.env` if absent, start dependencies, run migrations, start and seed the app, then start Prometheus, Grafana and Tempo. Keeps existing credentials and database data. |
| `make run` | Start management with `go run ./main.go management-server`. |
| `make run-public` | Start redirects with `go run ./main.go public-server`; `PUBLIC_HTTP_ADDR` defaults to `:8081`, `PUBLIC_APP_ENV` to `public-local` (metrics port 9093). |
| `make migrate` | Build the app image and run migrations in Compose; starts the dependency services and changes the database. |
| `make migrate-local` | Apply database migrations from the host with `go run ./main.go migrate`; changes the configured database. |
| `make docker-up` | Start PostgreSQL and Redis with Compose and wait for their health checks. |
| `make docker-run` | Build and start management (`app`), redirects (`public`) and dependencies in Compose. Apply migrations separately first. |
| `make docker-down` | Stop and remove the Compose containers and network. Keep the database volume. |
| `make static` | Run `lint`, `vuln-check`, and `nilaway`. |
| `make lint` | Install the pinned golangci-lint version, then run it across `./...`, allowing parallel lint runners. Resolves the executable from `GOBIN`, falling back to `GOPATH/bin`. |
| `make vuln-check` | Install govulncheck, then run it across `./...` with verbose findings. |
| `make nilaway` | Install nilaway, then run it for nil-safety analysis. |
| `make test` | Run `go test ./...` with default build tags; excludes tests gated by `e2e`. |
| `make test-integration` | Delegate integration tests to `./scripts/integration-test.sh test`. |
| `make test-integration-race` | Delegate integration tests with race detection to `./scripts/integration-test.sh test-race`. |
| `make test-integration-cover` | Delegate integration coverage to `./scripts/integration-test.sh test-cover`. |
| `make test-e2e` | Run the end-to-end suite under `./test/e2e/...` with the `e2e` tag. The suite starts and removes its Docker Compose stack. |
| `make cover` | Run tests with race detection and coverage for `./...`; write `reports/cover.out` and, on success, `reports/cover.html`. |
| `make update-mocks` | Run mockery to generate mocks using its configuration. |
| `make update-swagger` | Install swag, then generate Swagger documentation in `swagger/`, parsing dependencies. |

## Prerequisites

- Use the Go version declared in `go.mod`. Race-enabled targets require CGO support and a C compiler.
- `make run`, `make run-public` and `make migrate-local` source `.env` as a shell file when present; use only a trusted file. Git ignores `.env`. Compose targets use `.env` by default. Set `COMPOSE_ENV_FILE` to select another file and `COMPOSE_PROJECT_NAME` to isolate a stack. Compose gives shell variables precedence over the selected file. `COMPOSE_ENV_FILE` does not configure host commands.
- `make setup` requires Docker, Compose, curl and OpenSSL. It generates credentials only when the selected environment file is absent. Unset `API_KEY`, `POSTGRES_PASSWORD`, `POSTGRES_USER` and `POSTGRES_DB` before generating a file. An existing file must contain a nonempty API key and database password. The seed adds example links to the selected database.
- Install development tools with `make install-libs`, or install selected tools with `./scripts/install-tools.sh` followed by their names. These commands, `make lint`, `make vuln-check`, and `make nilaway` need network access and write to Go's binary directory. Targets invoking tools by name require that directory on `PATH`; `lint` resolves its own executable path.
- Compose targets and integration and end-to-end tests require Docker and Docker Compose. The app build and end-to-end tests also need access to build and pull the required images.

## Current limitations

These describe the current checkout. Recheck the referenced files when working on these targets.

- **Mocks:** no mockery configuration is present in the repository. Check generation configuration before using `update-mocks`.

`make test-load` starts the isolated load stack, opens its Grafana dashboard and runs k6; it writes per-run reports under `reports/load/`. It requires Docker, Compose, curl, image/build access and an available graphical browser unless `LOAD_OPEN_BROWSER=0`. It leaves the stack and its database running; see [load testing](../load-testing.md) for cleanup and ports.
