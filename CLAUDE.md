# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

InsightIQ is a business-intelligence platform in an early (Milestone 1) state. Monorepo with two independent stacks:

- `apps/web/` — Next.js (App Router) frontend, managed by pnpm + Turbo.
- `services/api/` — Go HTTP API, a **standalone Go module** (not a pnpm workspace).

The two stacks share no build graph and communicate (eventually) over HTTP. The web app currently ships a demo dashboard UI with hardcoded data and **no API integration yet**.

## Local dev startup order

Infra runs in Docker (Postgres 17 + pgvector, Redis 7). Bring it up before the API.

```sh
make up                               # start postgres + redis, wait until healthy
make migrate-up                       # apply DB migrations (golang-migrate in Docker)
make db-app-role                      # enable login for RLS-restricted API role
cd services/api && go run ./cmd/api   # API on :8080
pnpm install && pnpm dev              # (from repo root) Next.js on :3000
```

Config is env-only (no config file). API reads `DATABASE_URL`, `APP_PORT` (default 8080), `APP_ENV` via `os.Getenv`. See `.env.example`. Pool limits are hardcoded in `cmd/api/main.go`.

## Commands

### Infra / DB (root `Makefile` — `make help` for all)
- `make up` / `make down` — start/stop postgres + redis.
- `make migrate-up` / `make migrate-down n=2` — apply / roll back migrations.
- `make migrate-new name=add_reports` — scaffold timestamped `.up.sql`/`.down.sql` pair.
- `make migrate-version` / `make migrate-force` — inspect / fix migration state.
- `make sqlc` / `make sqlc-check` — regenerate / validate sqlc output.
- `make db-reset` — drop and re-migrate.

Migrate + sqlc run inside Docker containers reaching Postgres via `host.docker.internal`.

### Go API (run from `services/api/`)
- `go run ./cmd/api` — run server.
- `go test ./...` — all tests.
- `go test -run TestLiveHealthEndpoint ./cmd/api/` — single test.
- Integration tests (RLS, cross-tenant API) need `TEST_DATABASE_URL` exported, pointing at a migrated DB as `insightiq_app`; otherwise skipped. They refuse to run as a superuser.

### Web (pnpm + Turbo; package manager pinned to pnpm 12.16.1)
- `pnpm dev` / `pnpm build` — Turbo across workspaces.
- `pnpm lint` — eslint. `pnpm typecheck` — `tsc --noEmit`. `pnpm test` — vitest (`vitest run`).
- Path alias `@/*` → `apps/web/src/*`.

## API architecture (`services/api/`)

Go 1.27, chi v5 router, pgx/v5 (`pgxpool`). Layered per feature module under `internal/modules/<name>/`:

**Handler → Service → Repository → Postgres**

- **handler.go** — HTTP parse/validate/respond. Enforces max 1 MB body, single JSON object, no unknown fields. Maps module sentinel errors to HTTP status in a `writeServiceError()` switch. Responses use structured `errorResponse{code, message, request_id}`.
- **service.go** — business logic, authorization, input validation (email via `mail.ParseAddress`, slug regex, password strength). Takes a Repository **interface** (DI → mockable in tests).
- **repository.go** — hand-written SQL via `pgxpool.Pool`. Catches constraint violations and maps to domain sentinel errors (e.g. `ErrEmailAlreadyExists`).

Modules present: `auth`, `organizations`, `authorization`.

### Authorization & tenant isolation (`internal/modules/authorization/`, `platform/database/tenant.go`)
- **Permission matrix** in `permissions.go` is the only place role→permission is decided. Use `authorization.Can(role, perm)`; never compare role strings. `permissions_test.go` pins the full table — update both together.
- `Middleware.RequirePermission("orgID", perm)` for org-scoped routes: 401 no user, **404 non-member** (hides existence), 403 insufficient role; puts `Principal{User, OrgID, Role}` in context. The organizations module enforces the same rules in its service (`requirePermission`) instead.
- **RLS** (migration `000003`) on projects, dashboards, widgets, data_sources, conversations, messages: policy `organization_id = app_current_tenant()`. Every query on those tables must run inside `database.WithTenantTx(ctx, pool, orgID, fn)` using the `tx` it passes; a query on the bare pool sees zero rows and cannot insert.
- API must connect as **`insightiq_app`** (NOSUPERUSER, NOBYPASSRLS; login enabled via `make db-app-role`). Startup calls `CheckRestrictedRole`: fatal outside `APP_ENV=development`, warning in dev. Migrations still run as owner `insightiq`.
- New tenant tables: add `ENABLE` + `FORCE ROW LEVEL SECURITY` + `tenant_isolation` policy in the same migration; grants are automatic via default privileges.

### sqlc is barely used yet
Despite `sqlc.yaml` + `internal/platform/database/sqlcdb/`, only `db/queries/users.sql` is generated (`GetUserByEmail`). **Most queries are hand-written in repository.go, not sqlc.** When adding queries, match the existing hand-written style in that module unless deliberately migrating to sqlc. sqlc type overrides: UUID→`google/uuid.UUID`, timestamptz→`time.Time`, jsonb→`json.RawMessage`.

### Migrations
`db/migrations/`, golang-migrate naming `<seq>_<name>.up.sql`/`.down.sql`. `000001` = full initial schema (users, organizations, memberships, sessions, projects, dashboards, widgets, data_sources, conversations, messages, audit_logs). `000002` = invitations. `000003` = `insightiq_app` role + RLS.

### Routing
`/api/v1` prefix. Public: `/auth/{register,login,logout}`. Behind `RequireSession` middleware: `/auth/me`, `/orgs/...` (CRUD + members + invitations), `/invitations/accept`. 15s request timeout on all routes. Health: `/health/live` (always 200), `/health/ready` (pings DB, 503 if down).

### Auth & sessions (`internal/modules/auth/`)
- Session token = 32 random bytes hex (64 chars); **only its SHA-256 hash is stored**. Passwords bcrypt cost 12, 12–72 byte policy.
- Login sets `insightiq_session` cookie (7-day, HttpOnly, SameSiteLax; `Secure` omitted in dev).
- `RequireSession` validates the cookie (join sessions→users, check hash valid + not revoked/expired + user active), puts the user in request context under `userContextKey`.

### Organizations & invitations RBAC (`internal/modules/organizations/`)
- Roles: `owner`, `admin`, `analyst`, `viewer`. Only owner/admin pass `canManage()`.
- `requireMember()` returns **404** for non-members (hides existence); `requireManager()` returns 403.
- Invitation token is **returned once** at creation (only the hash persists). Partial unique index enforces one open invite per `(org, email)`.
- Transactions + `FOR UPDATE` row locks guard: org creation (+owner membership), role change, member removal, invite acceptance (prevents double-accept).

## Reference docs
- `docs/backend-architecture.md` — fuller API design.
- `insightiq-local-run-guide.md` — dev setup walkthrough.
- `README.md` — foundation overview.
