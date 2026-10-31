# Check to see if we can use ash, in Alpine images, or default to BASH.
SHELL_PATH = /bin/ash
SHELL = $(if $(wildcard $(SHELL_PATH)),/bin/ash,/bin/bash)

# Load environment variables from .env file if it exists
ifneq (,$(wildcard ./.env))
    include .env
    export
endif

.PHONY: all
all: install-libs lint test cover

# ==============================================================================
# Install dependencies

.PHONY: install-libs
install-libs:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
	go install golang.org/x/vuln/cmd/govulncheck@latest
	go install github.com/vektra/mockery/v3@latest
	go install github.com/swaggo/swag/cmd/swag@latest
	go install go.uber.org/nilaway/cmd/nilaway@latest

# ==============================================================================
# Administration

.PHONY: run
run:
	go run ./main.go server

.PHONY: migrate
migrate:
	go run ./main.go db:migrate

# ==============================================================================
# Running tests within the local computer

.PHONY: static
static: lint vuln-check nilaway

.PHONY: lint
lint:
	$(shell go env GOBIN | grep . || printf '%s/bin' "$(shell go env GOPATH)")/golangci-lint run ./... --allow-parallel-runners

.PHONY: vuln-check
vuln-check:
	govulncheck -show verbose ./... 

.PHONY: nilaway
nilaway:
	nilaway --include-pkgs="github.com/cristiano-pacheco/jbn" --exclude-pkgs="vendor/" ./...

.PHONY: test
test:
	go test ./...

# ==============================================================================
# Integration Tests
# All integration test commands are delegated to scripts/integration-test.sh
# which handles Docker socket detection and testcontainers configuration.
#
# The script auto-detects Docker (Colima or Docker Desktop) and sets up
# all required environment variables for testcontainers.
#
# Override Docker configuration by setting environment variables:
#   DOCKER_HOST=unix:///path/to/docker.sock make test-integration
#   TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/path/to/docker.sock make test-integration
#
# For more information, run: ./scripts/integration-test.sh help
# ==============================================================================

.PHONY: test-integration
test-integration:
	@./scripts/integration-test.sh test

.PHONY: test-integration-race
test-integration-race:
	@./scripts/integration-test.sh test-race

.PHONY: test-integration-cover
test-integration-cover:
	@./scripts/integration-test.sh test-cover

.PHONY: test-e2e
test-e2e:
	APP_BASE_URL=http://localhost:9000 CGO_ENABLED=0 go test -v -race -timeout=30s -tags=e2e ./test/e2e/...

.PHONY: cover
cover:
	mkdir -p reports
	go test -race -coverprofile=reports/cover.out -coverpkg=./... ./... && \
	go tool cover -html=reports/cover.out -o reports/cover.html

.PHONY: update-mocks
update-mocks:
	mockery

.PHONY: update-swagger
update-swagger:
	go install github.com/swaggo/swag/cmd/swag@latest
	swag i --parseDependency -o swagger
