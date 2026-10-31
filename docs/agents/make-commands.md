# Make commands

Run commands from the repository root. Use the table to select a target, then check its prerequisites and limitations below. `Makefile` is the source of truth for recipes, tool versions, and flags; check it when changing or diagnosing a target.

## Command reference

| Command | Purpose and effects |
| --- | --- |
| `make` / `make all` | Install development tools, run lint and tests, then generate coverage. Tests run again during coverage. |
| `make install-libs` | Install golangci-lint, govulncheck, mockery, swag, and nilaway with `go install`. Requires network access and writes to Go's binary directory. |
| `make run` | Start the server with `go run ./main.go server`. |
| `make migrate` | Apply database migrations with `go run ./main.go db:migrate`; changes the configured database. |
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
- The Makefile loads and exports `.env` when present. Check the selected environment before starting the server or applying migrations; `.env.example` and `config/` describe application configuration.
- Install development tools with `make install-libs`, or install selected tools with `./scripts/install-tools.sh` followed by their names. These commands, `make lint`, `make vuln-check`, and `make nilaway` need network access and write to Go's binary directory. Targets invoking tools by name require that directory on `PATH`; `lint` resolves its own executable path.
- Integration and end-to-end tests require Docker and Docker Compose. End-to-end tests also need access to build and pull the required images.

## Current limitations

These describe the current checkout. Recheck the referenced files when working on these targets.

- **Integration:** `test/integration/` is absent; the retained integration targets require integration tests to be added before they can run.
- **Mocks:** no mockery configuration is present in the repository. Check generation configuration before using `update-mocks`.
