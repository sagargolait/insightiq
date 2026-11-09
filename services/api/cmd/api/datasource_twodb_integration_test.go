package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/sagargolait/insightiq/services/api/internal/platform/database"
)

// TestTwoOrgsSeparateExternalDatabases is the end-to-end proof that two
// organizations, each connecting its OWN external PostgreSQL database, are fully
// isolated: schema discovery and queries for org A see only database A, org B
// only database B, and neither can reach the other's source.
//
// Requires:
//   - TEST_DATABASE_URL: the InsightIQ app DB (insightiq_app role, migrated).
//   - PROBE_OWNER_URL:    an owner/superuser URL on the SAME server, used only to
//     provision the two throwaway external databases and a read-only role.
//
// The two external databases are separate PostgreSQL databases (separate
// connection targets), matching the "separate external databases" requirement.
func TestTwoOrgsSeparateExternalDatabases(t *testing.T) {
	appURL := os.Getenv("TEST_DATABASE_URL")
	ownerURL := os.Getenv("PROBE_OWNER_URL")
	if appURL == "" || ownerURL == "" {
		t.Skip("TEST_DATABASE_URL and PROBE_OWNER_URL required; skipping two-DB test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	// Provision the two external databases + a read-only analytics role.
	extA := provisionExternalDB(t, ctx, ownerURL, "warehouse_a", "orders", "alpha-order")
	extB := provisionExternalDB(t, ctx, ownerURL, "warehouse_b", "shipments", "beta-shipment")

	// Bring up the API against the app DB.
	pool, err := database.NewPool(ctx, database.Config{
		URL: appURL, MaxConns: 4, MaxConnLifetime: time.Hour, MaxConnIdleTime: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	if err := database.CheckRestrictedRole(ctx, pool); err != nil {
		t.Fatalf("TEST_DATABASE_URL must use the insightiq_app role: %v", err)
	}

	server := httptest.NewServer(testRouter(t, pool))
	defer server.Close()

	alice := newSignedInClient(t, server.URL, "two-alice")
	bob := newSignedInClient(t, server.URL, "two-bob")

	orgA := mustCreateOrg(t, alice, "two-a")
	orgB := mustCreateOrg(t, bob, "two-b")
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = ANY($1)`, []uuid.UUID{orgA, orgB})
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = ANY($1)`, []uuid.UUID{alice.userID, bob.userID})
	})

	// Each org connects its own external DB and activates it.
	sourceA := connectAndActivate(t, alice, orgA, "warehouse", extA)
	sourceB := connectAndActivate(t, bob, orgB, "warehouse", extB)

	// Discover schema in each; A must see table "orders", B must see "shipments",
	// and neither must see the other's table.
	tablesA := refreshAndTables(t, alice, orgA, sourceA)
	tablesB := refreshAndTables(t, bob, orgB, sourceB)

	if !tablesA["orders"] {
		t.Errorf("org A schema missing its own table 'orders': %v", keys(tablesA))
	}
	if tablesA["shipments"] {
		t.Errorf("org A schema leaked org B's table 'shipments': %v", keys(tablesA))
	}
	if !tablesB["shipments"] {
		t.Errorf("org B schema missing its own table 'shipments': %v", keys(tablesB))
	}
	if tablesB["orders"] {
		t.Errorf("org B schema leaked org A's table 'orders': %v", keys(tablesB))
	}

	// Query each external DB through its own org. A's row marker must be alpha,
	// B's must be beta — proving the queries hit different databases.
	if got := scalarQuery(t, alice, orgA, sourceA, "SELECT marker FROM orders LIMIT 1"); got != "alpha-order" {
		t.Errorf("org A query returned %q, want alpha-order", got)
	}
	if got := scalarQuery(t, bob, orgB, sourceB, "SELECT marker FROM shipments LIMIT 1"); got != "beta-shipment" {
		t.Errorf("org B query returned %q, want beta-shipment", got)
	}

	// Org A querying its own source for org B's table must fail (table absent in
	// database A) — a clean failure, not a cross-database leak.
	status, _ := alice.do(http.MethodPost,
		"/api/v1/orgs/"+orgA.String()+"/data-sources/"+sourceA.String()+"/query",
		map[string]any{"sql": "SELECT marker FROM shipments LIMIT 1"})
	if status != http.StatusBadGateway { // CONNECTION_FAILED: relation does not exist in DB A
		t.Errorf("org A querying B's table: want 502, got %d", status)
	}

	// Bob cannot touch org A's source at all (tenant isolation): 404.
	status, _ = bob.do(http.MethodGet, "/api/v1/orgs/"+orgB.String()+"/data-sources/"+sourceA.String(), nil)
	if status != http.StatusNotFound {
		t.Errorf("bob reaching A's source via his org: want 404, got %d", status)
	}
}

// externalConn describes a provisioned external database the API will connect to
// as a read-only user.
type externalConn struct {
	host, database, username, password string
	port                               int
}

func (e externalConn) createBody(name string) map[string]any {
	return map[string]any{
		"name": name, "type": "postgres",
		"host": e.host, "port": e.port, "database": e.database,
		"username": e.username, "password": e.password,
	}
}

// provisionExternalDB creates a fresh database on the owner's server, a
// read-only role scoped to it, and seeds one marker table. Returns the
// connection details the API will use. Registers cleanup.
func provisionExternalDB(t *testing.T, ctx context.Context, ownerURL, dbName, table, marker string) externalConn {
	t.Helper()

	ownerCfg, err := pgx.ParseConfig(ownerURL)
	if err != nil {
		t.Fatalf("parse PROBE_OWNER_URL: %v", err)
	}

	// Unique suffix so parallel/re-runs don't collide.
	suffix := strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	fullDB := dbName + "_" + suffix
	roUser := "ro_" + suffix
	roPass := "ro-pass-" + suffix

	admin, err := pgx.ConnectConfig(ctx, ownerCfg)
	if err != nil {
		t.Fatalf("connect owner: %v", err)
	}
	// CREATE DATABASE cannot run inside a transaction; pgx.Exec auto-commits.
	if _, err := admin.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", pgx.Identifier{fullDB}.Sanitize())); err != nil {
		admin.Close(ctx)
		t.Fatalf("create database: %v", err)
	}
	if _, err := admin.Exec(ctx, fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD %s",
		pgx.Identifier{roUser}.Sanitize(), quoteLiteral(roPass))); err != nil {
		admin.Close(ctx)
		t.Fatalf("create ro role: %v", err)
	}
	admin.Close(ctx)

	t.Cleanup(func() {
		ctx := context.Background()
		a, err := pgx.ConnectConfig(ctx, ownerCfg)
		if err != nil {
			return
		}
		defer a.Close(ctx)
		_, _ = a.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", pgx.Identifier{fullDB}.Sanitize()))
		_, _ = a.Exec(ctx, fmt.Sprintf("DROP ROLE IF EXISTS %s", pgx.Identifier{roUser}.Sanitize()))
	})

	// Connect to the new database to seed the table and grant read-only access.
	seedCfg := ownerCfg.Copy()
	seedCfg.Database = fullDB
	seed, err := pgx.ConnectConfig(ctx, seedCfg)
	if err != nil {
		t.Fatalf("connect new db: %v", err)
	}
	defer seed.Close(ctx)

	stmts := []string{
		fmt.Sprintf("CREATE TABLE %s (id serial primary key, marker text not null)", pgx.Identifier{table}.Sanitize()),
		fmt.Sprintf("INSERT INTO %s (marker) VALUES (%s)", pgx.Identifier{table}.Sanitize(), quoteLiteral(marker)),
		fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s", pgx.Identifier{fullDB}.Sanitize(), pgx.Identifier{roUser}.Sanitize()),
		fmt.Sprintf("GRANT USAGE ON SCHEMA public TO %s", pgx.Identifier{roUser}.Sanitize()),
		fmt.Sprintf("GRANT SELECT ON ALL TABLES IN SCHEMA public TO %s", pgx.Identifier{roUser}.Sanitize()),
	}
	for _, s := range stmts {
		if _, err := seed.Exec(ctx, s); err != nil {
			t.Fatalf("seed stmt %q: %v", s, err)
		}
	}

	return externalConn{
		host:     ownerCfg.Host,
		port:     int(ownerCfg.Port),
		database: fullDB,
		username: roUser,
		password: roPass,
	}
}

func mustCreateOrg(t *testing.T, c *apiClient, label string) uuid.UUID {
	t.Helper()
	status, body := c.do(http.MethodPost, "/api/v1/orgs/", map[string]string{
		"name": label, "slug": label + "-" + uuid.NewString()[:8],
	})
	if status != http.StatusCreated {
		t.Fatalf("create org %s: %d %v", label, status, body)
	}
	return dataID(t, body, "id")
}

func connectAndActivate(t *testing.T, c *apiClient, orgID uuid.UUID, name string, ext externalConn) uuid.UUID {
	t.Helper()
	base := "/api/v1/orgs/" + orgID.String() + "/data-sources"
	status, body := c.do(http.MethodPost, base, ext.createBody(name))
	if status != http.StatusCreated {
		t.Fatalf("create source: %d %v", status, body)
	}
	sourceID := dataID(t, body, "id")

	status, body = c.do(http.MethodPost, base+"/"+sourceID.String()+"/test", nil)
	if status != http.StatusOK {
		t.Fatalf("test source: %d %v", status, body)
	}
	if data, _ := body["data"].(map[string]any); data["status"] != "active" {
		t.Fatalf("source not active after test: %v", body)
	}
	return sourceID
}

func refreshAndTables(t *testing.T, c *apiClient, orgID, sourceID uuid.UUID) map[string]bool {
	t.Helper()
	sbase := "/api/v1/orgs/" + orgID.String() + "/data-sources/" + sourceID.String()
	if status, body := c.do(http.MethodPost, sbase+"/schema/refresh", nil); status != http.StatusOK {
		t.Fatalf("schema refresh: %d %v", status, body)
	}
	status, body := c.do(http.MethodGet, sbase+"/schema", nil)
	if status != http.StatusOK {
		t.Fatalf("get schema: %d %v", status, body)
	}
	tables := map[string]bool{}
	rows, _ := body["data"].([]any)
	for _, r := range rows {
		if m, ok := r.(map[string]any); ok {
			if tn, ok := m["table_name"].(string); ok {
				tables[tn] = true
			}
		}
	}
	return tables
}

func scalarQuery(t *testing.T, c *apiClient, orgID, sourceID uuid.UUID, sql string) string {
	t.Helper()
	status, body := c.do(http.MethodPost,
		"/api/v1/orgs/"+orgID.String()+"/data-sources/"+sourceID.String()+"/query",
		map[string]any{"sql": sql})
	if status != http.StatusOK {
		t.Fatalf("query %q: %d %v", sql, status, body)
	}
	data, _ := body["data"].(map[string]any)
	rows, _ := data["rows"].([]any)
	if len(rows) == 0 {
		t.Fatalf("query %q returned no rows: %v", sql, body)
	}
	first, _ := rows[0].([]any)
	if len(first) == 0 {
		t.Fatalf("query %q returned empty row: %v", sql, body)
	}
	return fmt.Sprint(first[0])
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// quoteLiteral wraps a string as a SQL literal for DDL where parameters are not
// allowed (CREATE ROLE ... PASSWORD). Values here are test-generated, never user
// input, and single quotes are doubled defensively.
func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
