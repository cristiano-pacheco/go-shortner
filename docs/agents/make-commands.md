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
| `make lint` | Run golangci-lint across `./...`, allowing parallel lint runners. Resolves the executable from `GOBIN`, falling back to `GOPATH/bin`. |
| `make vuln-check` | Run govulncheck across `./...` with verbose findings. |
| `make nilaway` | Run nilaway for nil-safety analysis; see the package-filter limitation below. |
| `make test` | Run `go test ./...` with default build tags; excludes tests gated by `integration` or `e2e`. |
| `make test-integration` | Delegate integration tests to `./scripts/integration-test.sh test`. |
| `make test-integration-race` | Delegate integration tests with race detection to `./scripts/integration-test.sh test-race`. |
| `make test-integration-cover` | Delegate integration coverage to `./scripts/integration-test.sh test-cover`. |
| `make test-e2e` | Request verbose, race-enabled tests under `./test/e2e/...`, with the `e2e` tag, a 30-second timeout, and `APP_BASE_URL=http://localhost:9000`. See limitations below. |
| `make cover` | Run tests with race detection and coverage for `./...`; write `reports/cover.out` and, on success, `reports/cover.html`. |
| `make update-mocks` | Run mockery to generate mocks using its configuration. |
| `make update-swagger` | Install swag, then generate Swagger documentation in `swagger/`, parsing dependencies. |

## Prerequisites

- Use the Go version declared in `go.mod`. Race-enabled targets require CGO support and a C compiler.
- The Makefile loads and exports `.env` when present. Check the selected environment before starting the server or applying migrations; `.env.example` and `config/` describe application configuration.
- Install development tools with `make install-libs` when needed. Targets invoking tools by name require Go's binary directory on `PATH`; `lint` resolves its own executable path.
- Integration tests require Docker. The integration targets expect their helper script to configure Docker/testcontainers; inspect that script before relying on socket detection or overrides.

## Current limitations

These describe the current checkout. Recheck the referenced files when working on these targets.

- **Integration:** `scripts/integration-test.sh` is absent, so all three `test-integration*` targets fail before running tests. Tests exist under `test/integration/` with the `integration` build tag.
- **E2E:** `test/e2e/` is absent. The recipe also sets `CGO_ENABLED=0` together with `-race`, which requires CGO. The target does not start a server.
- **Nilaway:** the inclusion filter is `github.com/cristiano-pacheco/jbn`, while `go.mod` declares `github.com/cristiano-pacheco/go-shortner`. Treat its result as insufficient coverage of this module until the filter is corrected.
- **Mocks:** no mockery configuration is present in the repository. Check generation configuration before using `update-mocks`.
