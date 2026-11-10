# HireDesk developer commands. Run `make` to list them.
#
# Every recipe line must begin with a literal TAB, not spaces.

# Load .env so targets such as `psql` know the credentials. Guarded so the
# Makefile still works on a fresh clone, before .env exists.
ifneq (,$(wildcard .env))
include .env
export
endif

POSTGRES_USER     ?= hiredesk
POSTGRES_PASSWORD ?= hiredesk_dev_password
POSTGRES_DB       ?= hiredesk
API_PORT          ?= 8080
ADMINER_PORT      ?= 8081

COMPOSE := docker compose
BINARY  := bin/hiredesk-api

# Migrations run in a container on the Compose network, so there is nothing to
# install locally and the connection never leaves Docker's internal DNS. That
# also makes these targets immune to whatever is listening on the host's 5432.
#
# Assembled from the same POSTGRES_* values Compose uses, so .env stays the one
# place credentials are defined. "postgres" is the service name, and 5432 is the
# container's own port, not the published one.
MIGRATE_URL := postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@postgres:5432/$(POSTGRES_DB)?sslmode=disable
MIGRATE     := $(COMPOSE) run --rm migrate -path=/migrations -database="$(MIGRATE_URL)"

.DEFAULT_GOAL := help

.PHONY: help up down logs build docker-build test test-integration lint fmt psql seed env reset ps \
        migrate-up migrate-down migrate-create migrate-version adminer

help: ## Show this help
	@echo "HireDesk — available commands:"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(firstword $(MAKEFILE_LIST)) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'
	@echo ""

# Depends on the .env file target below, so a fresh clone needs only `make up`.
up: .env ## Start the whole stack (postgres + migrations + api + adminer)
	$(COMPOSE) up --build -d
	@echo ""
	@echo "API      → http://localhost:$(API_PORT)/healthz"
	@echo "Readiness→ http://localhost:$(API_PORT)/readyz"
	@echo "Database → http://localhost:$(ADMINER_PORT)/?pgsql=postgres&username=$(POSTGRES_USER)&db=$(POSTGRES_DB)"
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

# The store tests that run real SQL. They sit behind a build tag so that plain
# `make test` needs no database. The URL points at the published port on
# localhost, because these run on the host, not inside the Compose network.
test-integration: .env ## Run the database tests against the running stack (needs `make up`)
	DATABASE_URL="postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:$${POSTGRES_PORT:-5432}/$(POSTGRES_DB)?sslmode=disable" \
		go test -race -count=1 -tags integration ./internal/store/...

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

# The query string pre-selects the driver, server, user and database, so only
# the password is left to type. Without it Adminer defaults to MySQL and an
# empty server, which is four fields of guesswork on every visit.
adminer: ## Open the database in your browser (prints a pre-filled login link)
	@echo "Adminer → http://localhost:$(ADMINER_PORT)/?pgsql=postgres&username=$(POSTGRES_USER)&db=$(POSTGRES_DB)"
	@echo ""
	@echo "  Everything is pre-filled except the password:"
	@echo "  POSTGRES_PASSWORD from your .env (default: hiredesk_dev_password)"

# `make up` already applies migrations; this is for applying them without
# restarting the stack, e.g. after writing a new pair.
migrate-up: .env ## Apply every pending migration
	$(MIGRATE) up
	@$(MAKE) --no-print-directory migrate-version

# One step, not all of them: `migrate down` with no argument prompts for
# confirmation, and an unattended rollback of the entire schema is never what
# was wanted. Run it repeatedly to go further back.
migrate-down: .env ## Roll back exactly one migration
	$(MIGRATE) down 1
	@$(MAKE) --no-print-directory migrate-version

migrate-version: .env ## Show the applied migration version and dirty flag
	@$(MIGRATE) version

# -seq gives zero-padded sequential prefixes (000007_...) rather than Unix
# timestamps. --user keeps the new files owned by you instead of root, which
# is what happens by default when a container writes to a bind mount.
migrate-create: .env ## Create a migration pair: make migrate-create NAME=add_foo
	@test -n "$(NAME)" || { echo "NAME is required, e.g. make migrate-create NAME=add_interview_slots"; exit 1; }
	$(COMPOSE) run --rm --no-deps --user $$(id -u):$$(id -g) migrate \
		create -ext sql -dir /migrations -seq $(NAME)
	@echo "created migrations/*_$(NAME).{up,down}.sql"

# Runs inside the api container, which already has DATABASE_URL pointing at the
# database on the Compose network. Safe to repeat: existing accounts are left
# untouched.
seed: ## Create the development accounts (needs `make up`; safe to run twice)
	$(COMPOSE) exec api /usr/local/bin/hiredesk-seed

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
