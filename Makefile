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
	./scripts/install-tools.sh

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
	./scripts/install-tools.sh golangci-lint
	$(shell go env GOBIN | grep . || printf '%s/bin' "$(shell go env GOPATH)")/golangci-lint run ./... --allow-parallel-runners

.PHONY: vuln-check
vuln-check:
	./scripts/install-tools.sh govulncheck
	govulncheck -show verbose ./... 

.PHONY: nilaway
nilaway:
	./scripts/install-tools.sh nilaway
	nilaway --include-pkgs="github.com/cristiano-pacheco/go-shortner" --exclude-pkgs="vendor/" ./...

.PHONY: test
test:
	go test ./...

# ==============================================================================
# Integration Tests
# Integration test commands use scripts/integration-test.sh and the Docker CLI
# configuration. For more information, run: ./scripts/integration-test.sh help
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
	./scripts/install-tools.sh swag
	swag i --parseDependency -o swagger
