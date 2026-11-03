# Check to see if we can use ash, in Alpine images, or default to BASH.
SHELL_PATH = /bin/ash
SHELL = $(if $(wildcard $(SHELL_PATH)),/bin/ash,/bin/bash)

COMPOSE_ENV_FILE ?= .env

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
	@set -a; if [ -f .env ]; then . ./.env; fi; set +a; go run ./main.go server

.PHONY: migrate-local
migrate-local:
	@set -a; if [ -f .env ]; then . ./.env; fi; set +a; go run ./main.go migrate

# ==============================================================================
# Docker Compose

COMPOSE = docker compose --env-file "$(COMPOSE_ENV_FILE)" -f docker/docker-compose.yaml -f docker/docker-compose.app.yaml

.PHONY: setup
setup:
	@COMPOSE_ENV_FILE="$(COMPOSE_ENV_FILE)" ./scripts/setup.sh
	$(COMPOSE) --profile observability up -d prometheus grafana tempo

.PHONY: docker-up
docker-up:
	$(COMPOSE) up -d --wait postgres redis

.PHONY: migrate
migrate:
	$(COMPOSE) build app
	$(COMPOSE) run --rm app migrate

.PHONY: docker-run
docker-run:
	$(COMPOSE) up -d --build app

.PHONY: docker-down
docker-down:
	$(COMPOSE) down

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
# Integration and end-to-end tests
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
	go test -count=1 -v -timeout=10m -tags=e2e ./test/e2e/...

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
