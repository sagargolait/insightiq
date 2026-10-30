# InsightIQ — Milestone 1 foundation starter

This repository contains the runnable development foundation:
- Next.js premium dashboard shell
- Go HTTP API with liveness/readiness endpoints
- PostgreSQL + pgvector and Redis through Docker Compose
- Initial relational schema and rollback migration
- A Go health endpoint test

**Important:** this is the foundation slice, not the complete Milestone 1 acceptance scope.
Registration/login, refresh-session rotation, organization membership APIs, RBAC enforcement,
PostgreSQL RLS policies, frontend auth flows and their integration tests are the next build slice.
Do not expose this starter to the public internet.

## Prerequisites
- Node.js 24 LTS
- pnpm 12
- Go 1.27+
- Docker Desktop
- Git

## Start infrastructure
```bash
docker compose up -d
docker compose ps
```

## Start API
```bash
cd services/api
export APP_ENV=development
export APP_PORT=8080
export DATABASE_URL='postgres://insightiq:local_dev_password@localhost:5432/insightiq?sslmode=disable'
go mod tidy
go run ./cmd/api
```

In another terminal, test:
```bash
curl -i http://localhost:8080/health/live
curl -i http://localhost:8080/health/ready
```

## Start web
From repository root:
```bash
pnpm install
pnpm --filter @insightiq/web dev
```
Open http://localhost:3000.

## Run tests
```bash
cd services/api && go test ./...
```

## Apply schema
Install golang-migrate, then from repository root:
```bash
migrate -path services/api/db/migrations   -database 'postgres://insightiq:local_dev_password@localhost:5432/insightiq?sslmode=disable' up
```

## Important
The starter API currently exposes health endpoints only. Authentication and tenant APIs are not
implemented in this foundation archive yet. We will implement and test those before calling
Milestone 1 complete.
