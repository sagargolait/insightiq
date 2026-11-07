# InsightIQ local Docker helpers. Run `make help` for the list.

COMPOSE        ?= docker compose
DB_USER        ?= insightiq
DB_PASSWORD    ?= local_dev_password
DB_NAME        ?= insightiq
DB_PORT        ?= 5432
# Restricted role the API connects as (created NOLOGIN by migration 000003).
APP_DB_PASSWORD ?= local_app_password
MIGRATIONS_DIR ?= services/api/db/migrations
MIGRATE_IMAGE  ?= migrate/migrate:v4.18.3
# golang-migrate runs in its own container and reaches Postgres via the host port.
MIGRATE_DB_URL ?= postgres://$(DB_USER):$(DB_PASSWORD)@host.docker.internal:$(DB_PORT)/$(DB_NAME)?sslmode=disable
MIGRATE        = docker run --rm -v "$(CURDIR)/$(MIGRATIONS_DIR):/migrations" \
                 --add-host=host.docker.internal:host-gateway \
                 $(MIGRATE_IMAGE) -path=/migrations -database "$(MIGRATE_DB_URL)"

# s = optional service name (make logs s=postgres)
# n = step count for migrate-down
s ?=
n ?= 1

.DEFAULT_GOAL := help
.PHONY: help up down restart stop start ps logs pull wait psql redis-cli \
        migrate-up migrate-down migrate-version migrate-force migrate-new \
        db-app-role sqlc sqlc-check db-reset clean

help: ## Show available commands
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

## ---- Containers ----
up: ## Start postgres + redis in background and wait until healthy
	$(COMPOSE) up -d --wait

down: ## Stop and remove containers (keeps data volumes)
	$(COMPOSE) down

stop: ## Stop containers without removing them
	$(COMPOSE) stop $(s)

start: ## Start previously stopped containers
	$(COMPOSE) start $(s)

restart: ## Restart containers (all, or `s=postgres`)
	$(COMPOSE) restart $(s)

ps: ## Show container status
	$(COMPOSE) ps

logs: ## Follow logs (all, or `s=postgres` / `s=redis`)
	$(COMPOSE) logs -f --tail=100 $(s)

pull: ## Pull latest images
	$(COMPOSE) pull

wait: ## Block until postgres accepts connections
	@until $(COMPOSE) exec -T postgres pg_isready -U $(DB_USER) -d $(DB_NAME) >/dev/null 2>&1; do \
		echo "waiting for postgres..."; sleep 1; done
	@echo "postgres ready"

## ---- Shells ----
psql: ## Open psql shell in postgres container
	$(COMPOSE) exec postgres psql -U $(DB_USER) -d $(DB_NAME)

redis-cli: ## Open redis-cli in redis container
	$(COMPOSE) exec redis redis-cli

## ---- Migrations (golang-migrate via Docker) ----
migrate-up: wait ## Apply all pending migrations
	$(MIGRATE) up

migrate-down: wait ## Roll back `n` migrations (default 1)
	$(MIGRATE) down $(n)

migrate-version: wait ## Print current migration version
	$(MIGRATE) version

migrate-force: wait ## Force version after failed migration: `make migrate-force v=1`
	@test -n "$(v)" || (echo "usage: make migrate-force v=<version>"; exit 1)
	$(MIGRATE) force $(v)

migrate-new: ## Create new migration pair: `make migrate-new name=add_reports`
	@test -n "$(name)" || (echo "usage: make migrate-new name=<snake_case_name>"; exit 1)
	docker run --rm -v "$(CURDIR)/$(MIGRATIONS_DIR):/migrations" -u "$$(id -u):$$(id -g)" \
		$(MIGRATE_IMAGE) create -ext sql -dir /migrations -seq -digits 6 $(name)

db-app-role: wait ## Enable login for the RLS-restricted API role insightiq_app (run after migrate-up)
	$(COMPOSE) exec -T postgres psql -U $(DB_USER) -d $(DB_NAME) -v ON_ERROR_STOP=1 \
		-c "ALTER ROLE insightiq_app WITH LOGIN PASSWORD '$(APP_DB_PASSWORD)'"

## ---- Code generation ----
SQLC = docker run --rm -v "$(CURDIR)/services/api:/src" -w /src -u "$$(id -u):$$(id -g)" sqlc/sqlc:1.29.0

sqlc: ## Generate Go code from services/api/db/queries via sqlc
	$(SQLC) generate

sqlc-check: ## Validate sqlc queries against schema without writing files
	$(SQLC) compile

## ---- Destructive ----
db-reset: ## DESTRUCTIVE: wipe volumes, recreate containers, re-run migrations
	@printf "This deletes all local postgres + redis data. Continue? [y/N] "; read ans; [ "$$ans" = "y" ]
	$(COMPOSE) down -v
	$(MAKE) up migrate-up db-app-role

clean: ## DESTRUCTIVE: stop containers and delete data volumes
	@printf "This deletes all local postgres + redis data. Continue? [y/N] "; read ans; [ "$$ans" = "y" ]
	$(COMPOSE) down -v --remove-orphans
