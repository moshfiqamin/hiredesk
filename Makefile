# HireDesk developer commands. Run `make` to list them.
#
# Every recipe line must begin with a literal TAB, not spaces.

# Load .env so targets such as `psql` know the credentials. Guarded so the
# Makefile still works on a fresh clone, before .env exists.
ifneq (,$(wildcard .env))
include .env
export
endif

POSTGRES_USER ?= hiredesk
POSTGRES_DB   ?= hiredesk
API_PORT      ?= 8080

COMPOSE := docker compose
BINARY  := bin/hiredesk-api

.DEFAULT_GOAL := help

.PHONY: help up down logs build docker-build test lint fmt psql env reset ps

help: ## Show this help
	@echo "HireDesk — available commands:"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'
	@echo ""

# Depends on the .env file target below, so a fresh clone needs only `make up`.
up: .env ## Start the whole stack (postgres + api) in the background
	$(COMPOSE) up --build -d
	@echo ""
	@echo "API      → http://localhost:$(API_PORT)/healthz"
	@echo "Postgres → localhost:$${POSTGRES_PORT:-5432}"
	@echo "Logs     → make logs"

down: ## Stop and remove containers (database data is KEPT)
	$(COMPOSE) down

logs: ## Follow logs from all services
	$(COMPOSE) logs -f

ps: ## Show container status and health
	$(COMPOSE) ps

build: ## Compile the Go binary to ./bin
	@mkdir -p bin
	go build -trimpath -ldflags="-s -w" -o $(BINARY) ./cmd/api
	@echo "built $(BINARY)"

docker-build: ## Rebuild the Docker image without starting anything
	$(COMPOSE) build

# -race needs cgo, so CGO_ENABLED is left at its default here, unlike the
# Docker build. Comments stay at column 0: a TAB-indented # is passed to the
# shell and echoed as if it were a command.
test: ## Run all tests with the race detector and coverage
	go test -race -cover ./...

lint: ## Run golangci-lint
	@command -v golangci-lint >/dev/null 2>&1 \
		|| { echo "golangci-lint not found. Install: brew install golangci-lint"; exit 1; }
	golangci-lint run ./...

fmt: ## Format all Go code and tidy go.mod
	gofmt -w .
	go mod tidy
	@echo "formatted"

# Uses the container's own psql, so no local Postgres client is needed.
psql: ## Open a psql shell inside the postgres container
	$(COMPOSE) exec postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB)

env: .env ## Create .env from .env.example with a generated JWT_SECRET

# A real file target: Make runs this only when .env is absent.
.env:
	@test -f .env.example || { echo ".env.example is missing"; exit 1; }
	@sed 's|^JWT_SECRET=.*|JWT_SECRET='"$$(openssl rand -hex 32)"'|' .env.example > .env
	@echo "created .env from .env.example, with a freshly generated JWT_SECRET"
	@echo "   .env is gitignored — never commit it"

# -v deletes the named volume. Needed after changing POSTGRES_USER/PASSWORD/DB,
# which the image reads only while initialising an empty data directory.
reset: ## Destroy containers AND the database volume, then start fresh
	@printf "This deletes all database data. Continue? [y/N] " && read ans && [ "$$ans" = "y" ]
	$(COMPOSE) down -v
	$(COMPOSE) up --build -d
