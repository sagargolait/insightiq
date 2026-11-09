package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sagargolait/insightiq/services/api/internal/modules/datasources"
	"github.com/sagargolait/insightiq/services/api/internal/platform/crypto"
	"github.com/sagargolait/insightiq/services/api/internal/platform/database"
)

// testRouter builds the real router with a throwaway dev cipher and a live
// query executor, so integration tests exercise the same wiring as production.
func testRouter(t *testing.T, pool *pgxpool.Pool) http.Handler {
	t.Helper()
	// Deterministic 32-byte key: fine for tests, never for real data.
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	cipher, err := crypto.New(map[string][]byte{"v1": key}, "v1")
	if err != nil {
		t.Fatalf("test cipher: %v", err)
	}
	exec := datasources.NewExecutor()
	t.Cleanup(exec.Close)
	handler := datasources.NewHandler(
		datasources.NewService(datasources.NewPostgresRepository(pool), cipher, datasources.NewConnector(), exec),
	)
	return newRouter(pool, nil, handler)
}

// externalTarget derives a ConnectionConfig the executor can actually reach by
// parsing TEST_DATABASE_URL. The test DB stands in for a customer's database.
func externalTarget(t *testing.T, url string) map[string]any {
	t.Helper()
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	return map[string]any{
		"host":     cfg.Host,
		"port":     int(cfg.Port),
		"database": cfg.Database,
		"username": cfg.User,
		"password": cfg.Password,
	}
}

func TestDataSourceTenantIsolationAndNoCredentialLeak(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping data source integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	pool, err := database.NewPool(ctx, database.Config{
		URL: url, MaxConns: 4, MaxConnLifetime: time.Hour, MaxConnIdleTime: time.Minute,
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

	alice := newSignedInClient(t, server.URL, "ds-alice")
	bob := newSignedInClient(t, server.URL, "ds-bob")

	createOrg := func(c *apiClient, label string) uuid.UUID {
		status, body := c.do(http.MethodPost, "/api/v1/orgs/", map[string]string{
			"name": label, "slug": "ds-" + label + "-" + uuid.NewString()[:8],
		})
		if status != http.StatusCreated {
			t.Fatalf("create org %s: %d %v", label, status, body)
		}
		return dataID(t, body, "id")
	}

	orgA := createOrg(alice, "a")
	orgB := createOrg(bob, "b")

	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = ANY($1)`, []uuid.UUID{orgA, orgB})
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = ANY($1)`, []uuid.UUID{alice.userID, bob.userID})
	})

	target := externalTarget(t, url)

	// Alice creates a data source in org A with a secret password.
	const secret = "alice-super-secret-password"
	createBody := map[string]any{
		"name": "warehouse", "type": "postgres",
		"host": target["host"], "port": target["port"], "database": target["database"],
		"username": target["username"], "password": secret,
	}
	status, body := alice.do(http.MethodPost, "/api/v1/orgs/"+orgA.String()+"/data-sources", createBody)
	if status != http.StatusCreated {
		t.Fatalf("alice create source: %d %v", status, body)
	}
	sourceA := dataID(t, body, "id")

	// The create response must not echo the password or any credential field.
	assertNoCredentialLeak(t, body, secret)

	// GET and LIST must likewise never carry the password.
	status, getBody := alice.do(http.MethodGet, "/api/v1/orgs/"+orgA.String()+"/data-sources/"+sourceA.String(), nil)
	if status != http.StatusOK {
		t.Fatalf("alice get source: %d %v", status, getBody)
	}
	assertNoCredentialLeak(t, getBody, secret)

	status, listBody := alice.do(http.MethodGet, "/api/v1/orgs/"+orgA.String()+"/data-sources", nil)
	if status != http.StatusOK {
		t.Fatalf("alice list sources: %d %v", status, listBody)
	}
	assertNoCredentialLeak(t, listBody, secret)

	// Bob tries to reach Alice's source by swapping his org ID into the path.
	// RLS + membership must make every attempt a 404.
	srcInB := "/api/v1/orgs/" + orgB.String() + "/data-sources/" + sourceA.String()
	attacks := []struct {
		name, method, path string
		body               any
	}{
		{"read", http.MethodGet, srcInB, nil},
		{"patch", http.MethodPatch, srcInB, map[string]any{"name": "pwned"}},
		{"delete", http.MethodDelete, srcInB, nil},
		{"test", http.MethodPost, srcInB + "/test", nil},
		{"schema", http.MethodGet, srcInB + "/schema", nil},
		{"query", http.MethodPost, srcInB + "/query", map[string]any{"sql": "SELECT 1"}},
	}
	for _, atk := range attacks {
		t.Run("cross-org "+atk.name, func(t *testing.T) {
			status, body := bob.do(atk.method, atk.path, atk.body)
			if status != http.StatusNotFound {
				t.Fatalf("want 404, got %d: %v", status, body)
			}
		})
	}

	// Bob's own org lists zero sources — RLS scopes the list to his tenant.
	status, bobList := bob.do(http.MethodGet, "/api/v1/orgs/"+orgB.String()+"/data-sources", nil)
	if status != http.StatusOK {
		t.Fatalf("bob list: %d %v", status, bobList)
	}
	if data, _ := bobList["data"].([]any); len(data) != 0 {
		t.Fatalf("bob sees %d sources, want 0 (RLS leak)", len(data))
	}
}

func TestDataSourceTestSchemaQueryHappyPath(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping data source happy-path test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	pool, err := database.NewPool(ctx, database.Config{
		URL: url, MaxConns: 4, MaxConnLifetime: time.Hour, MaxConnIdleTime: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	server := httptest.NewServer(testRouter(t, pool))
	defer server.Close()

	owner := newSignedInClient(t, server.URL, "ds-owner")
	status, body := owner.do(http.MethodPost, "/api/v1/orgs/", map[string]string{
		"name": "dsh", "slug": "dsh-" + uuid.NewString()[:8],
	})
	if status != http.StatusCreated {
		t.Fatalf("create org: %d %v", status, body)
	}
	orgID := dataID(t, body, "id")

	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, owner.userID)
	})

	target := externalTarget(t, url)
	base := "/api/v1/orgs/" + orgID.String() + "/data-sources"

	status, body = owner.do(http.MethodPost, base, map[string]any{
		"name": "selfdb", "type": "postgres",
		"host": target["host"], "port": target["port"], "database": target["database"],
		"username": target["username"], "password": target["password"],
	})
	if status != http.StatusCreated {
		t.Fatalf("create: %d %v", status, body)
	}
	sourceID := dataID(t, body, "id")
	sbase := base + "/" + sourceID.String()

	// Query before activation is rejected (409 NOT_ACTIVE).
	if status, _ := owner.do(http.MethodPost, sbase+"/query", map[string]any{"sql": "SELECT 1"}); status != http.StatusConflict {
		t.Fatalf("query before test: want 409, got %d", status)
	}

	// Test connectivity → active.
	status, body = owner.do(http.MethodPost, sbase+"/test", nil)
	if status != http.StatusOK {
		t.Fatalf("test: %d %v", status, body)
	}
	if data, _ := body["data"].(map[string]any); data["status"] != "active" {
		t.Fatalf("status after test = %v, want active", body)
	}

	// Refresh schema, then it lists at least our own known tables.
	if status, body := owner.do(http.MethodPost, sbase+"/schema/refresh", nil); status != http.StatusOK {
		t.Fatalf("schema refresh: %d %v", status, body)
	}
	status, schemaBody := owner.do(http.MethodGet, sbase+"/schema", nil)
	if status != http.StatusOK {
		t.Fatalf("get schema: %d %v", status, schemaBody)
	}
	cols, _ := schemaBody["data"].([]any)
	if len(cols) == 0 {
		t.Fatal("expected discovered schema columns")
	}

	// A valid aggregate query returns rows.
	status, qBody := owner.do(http.MethodPost, sbase+"/query",
		map[string]any{"sql": "SELECT count(*) AS n FROM organizations"})
	if status != http.StatusOK {
		t.Fatalf("query: %d %v", status, qBody)
	}
	data, _ := qBody["data"].(map[string]any)
	if cols, _ := data["columns"].([]any); len(cols) != 1 {
		t.Fatalf("expected 1 column, got %v", data["columns"])
	}

	// A write/DDL query is rejected 422 before it ever reaches the database.
	if status, _ := owner.do(http.MethodPost, sbase+"/query", map[string]any{"sql": "DROP TABLE organizations"}); status != http.StatusUnprocessableEntity {
		t.Fatalf("DROP query: want 422, got %d", status)
	}
	// A stacked statement is rejected too.
	if status, _ := owner.do(http.MethodPost, sbase+"/query", map[string]any{"sql": "SELECT 1; DROP TABLE organizations"}); status != http.StatusUnprocessableEntity {
		t.Fatalf("stacked query: want 422, got %d", status)
	}
}

// assertNoCredentialLeak fails if the serialized response contains the secret or
// any obvious credential field name.
func assertNoCredentialLeak(t *testing.T, body map[string]any, secret string) {
	t.Helper()
	raw := strings.ToLower(sprintBody(body))
	if strings.Contains(raw, strings.ToLower(secret)) {
		t.Fatalf("response leaked the password: %v", body)
	}
	for _, field := range []string{"password", "encrypted_credentials", "encryptedcredentials", "key_version"} {
		if strings.Contains(raw, field) {
			t.Fatalf("response contains forbidden field %q: %v", field, body)
		}
	}
}

// sprintBody serializes the response (keys included) so both leaked values and
// forbidden field names are caught by a substring scan.
func sprintBody(body map[string]any) string {
	raw, _ := json.Marshal(body)
	return string(raw)
}
