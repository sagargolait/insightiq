# InsightIQ --- Local Development Quick Start

This guide collects the initial commands and configuration needed to run
the InsightIQ starter project locally on macOS.

> **Current scope:** This starts the Next.js frontend, Go API,
> PostgreSQL, and Redis. It does not mean all Milestone 1 features are
> complete. Authentication routes must be wired into the API router
> before login/register requests will work.

## 1. Prerequisites

Install or verify Docker Desktop, Node.js 24 LTS, pnpm 12, Go 1.27.3,
and Git.

``` bash
docker --version
docker compose version
node --version
pnpm --version
go version
git --version
```

Install pnpm if needed:

``` bash
npm install --global pnpm@12
```

Start Docker Desktop.

## 2. Open the project

``` bash
cd /path/to/insightiq
```

For example:

``` bash
cd ~/Downloads/insightiq
```

## 3. Start PostgreSQL and Redis

From the repository root:

``` bash
docker compose up -d
docker compose ps
```

View logs:

``` bash
docker compose logs -f postgres
docker compose logs -f redis
```

Stop containers:

``` bash
docker compose down
```

**Warning:** `docker compose down -v` deletes local database volumes and
all local database data.

## 4. Configure the Go API

The current Go API setup does not automatically load `.env`; export the
variables in the terminal where you start the API.

``` bash
export PORT=8080
export DATABASE_URL='postgres://postgres:postgres@localhost:5432/insightiq?sslmode=disable'
```

Adjust the username, password, database, and port to match your
`docker-compose.yml`.

Check PostgreSQL readiness:

``` bash
docker compose exec postgres pg_isready -U postgres
```

## 5. Apply the database migration

From the repository root, run:

``` bash
docker compose exec -T postgres psql -U postgres -d insightiq -v ON_ERROR_STOP=1 < services/api/migrations/001_init.sql
```

Alternatively, if `psql` is installed on your Mac:

``` bash
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f services/api/migrations/001_init.sql
```

Verify the tables:

``` bash
psql "$DATABASE_URL" -c '\dt'
```

You should see the starter tables, including `users`, `organizations`,
`memberships`, `sessions`, `projects`, `dashboards`, `widgets`,
`data_sources`, `conversations`, `messages`, and `audit_logs`.

If a migration runner is configured in your checkout, use that runner
rather than manually applying the same SQL repeatedly.

## 6. Run the Go API

``` bash
cd services/api
go test ./...
go run ./cmd/api
```

Keep this terminal open. Stop the server with `Ctrl+C`.

In another terminal, test the health route:

``` bash
curl -i http://localhost:8080/health
```

If the starter uses a different health path, check
`services/api/cmd/api/main.go`.

## 7. Install and run the Next.js frontend

Open a new terminal at the repository root:

``` bash
cd /path/to/insightiq
pnpm install
pnpm dev
```

Open `http://localhost:3000`.

If the root workspace does not define a `dev` script, inspect the root
`package.json` and run the frontend package directly. For example:

``` bash
cd apps/web
pnpm dev
```

Use the actual frontend directory if it differs.

## 8. Daily quick start

**Terminal 1 --- dependencies**

``` bash
cd /path/to/insightiq
docker compose up -d
```

**Terminal 2 --- Go API**

``` bash
cd /path/to/insightiq
export PORT=8080
export DATABASE_URL='postgres://postgres:postgres@localhost:5432/insightiq?sslmode=disable'
cd services/api
go run ./cmd/api
```

**Terminal 3 --- frontend**

``` bash
cd /path/to/insightiq
pnpm dev
```

Then visit `http://localhost:3000`.

## 9. Authentication smoke tests

Run these only after the register/login handlers have been implemented
and registered on the router used by the HTTP server.

Register:

``` bash
curl -i -X POST http://localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "Sagar",
    "email": "sagar@example.com",
    "password": "StrongPassword123!"
  }'
```

Login and save the session cookie:

``` bash
curl -i -X POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -c cookies.txt \
  -d '{
    "email": "sagar@example.com",
    "password": "StrongPassword123!"
  }'
```

Send the cookie on a later request:

``` bash
curl -i http://localhost:8080/api/v1/auth/me -b cookies.txt
```

The `/auth/me` route is only available after it has been implemented.
Keep `cookies.txt` local; do not commit it.

## 10. Troubleshooting

### `404 page not found` on `/api/v1/auth/login`

A plain 404 means the request reached an HTTP server, but that server
did not match the route. It is not evidence of an incorrect password or
a database failure.

Check that the login route is registered:

``` go
r.Route("/api/v1", func(r chi.Router) {
    r.Post("/auth/register", authHandler.Register)
    r.Post("/auth/login", authHandler.Login)
})
```

Check that the server uses the same router:

``` go
server := &http.Server{
    Addr:    ":" + port,
    Handler: r,
}
```

After changing routes, stop the old process with `Ctrl+C` and restart
it. Compare the register route too: if both routes return 404, the auth
route group may be missing or the wrong router may be served.

### Database connection refused

-   Confirm Docker Desktop is running.
-   Check `docker compose ps`.
-   Verify the port in `DATABASE_URL` matches the Compose port mapping.
-   Inspect `docker compose logs -f postgres`.

### Table or relation does not exist

Confirm the migration was applied to the same database named in
`DATABASE_URL`.

### `go.mod file not found`

Run Go commands from the API module directory:

``` bash
cd services/api
go test ./...
```

### Frontend cannot reach the API

-   Confirm the API is listening at `http://localhost:8080`.
-   Check the frontend API base URL.
-   Check browser and API logs.
-   If browser requests are cross-origin, configure CORS for
    `http://localhost:3000`.

## 11. Useful commands

From the repository root:

``` bash
docker compose up -d
docker compose ps
docker compose down
pnpm install
pnpm dev
```

From `services/api`:

``` bash
go test ./...
go test -count=1 ./...
gofmt -w .
go run ./cmd/api
```

## 12. Keep local secrets out of Git

Do not commit `.env` files containing credentials, `cookies.txt`,
database dumps with personal data, API keys, or access tokens. Ensure
`.gitignore` excludes local environment files and cookie files. Use
Azure Key Vault or another secret manager for production secrets.

## Setup checklist

-   [ ] Docker Desktop running
-   [ ] PostgreSQL container healthy
-   [ ] Redis container running
-   [ ] Database migration applied
-   [ ] Go tests passing
-   [ ] Go API running on port `8080`
-   [ ] Next.js frontend running on port `3000`
-   [ ] Register route verified
-   [ ] Login route verified
-   [ ] Session cookie verified

**Note:** This guide reflects the starter structure and setup discussed
so far. If a script, directory, port, or migration filename differs in
your local checkout, use the actual value in your repository.
