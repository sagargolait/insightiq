package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sagargolait/insightiq/services/api/internal/platform/database"
)

// End-to-end cross-tenant checks through the real router and Postgres.
// Requires TEST_DATABASE_URL (migrated DB, insightiq_app role); skipped otherwise.

type apiClient struct {
	t      *testing.T
	base   string
	http   *http.Client
	userID uuid.UUID
	email  string
}

func (c *apiClient) do(method, path string, body any) (int, map[string]any) {
	c.t.Helper()

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		reader = strings.NewReader(string(raw))
	}

	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		c.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()

	var decoded map[string]any
	_ = json.NewDecoder(res.Body).Decode(&decoded)
	return res.StatusCode, decoded
}

func dataID(t *testing.T, body map[string]any, key string) uuid.UUID {
	t.Helper()
	data, _ := body["data"].(map[string]any)
	id, err := uuid.Parse(fmt.Sprint(data[key]))
	if err != nil {
		t.Fatalf("response has no data.%s: %v", key, body)
	}
	return id
}

func newSignedInClient(t *testing.T, base, label string) *apiClient {
	t.Helper()

	jar, _ := cookiejar.New(nil)
	c := &apiClient{
		t:     t,
		base:  base,
		http:  &http.Client{Jar: jar, Timeout: 10 * time.Second},
		email: fmt.Sprintf("tenant-%s-%s@example.test", label, uuid.NewString()[:8]),
	}
	const password = "correct-horse-battery-staple"

	status, body := c.do(http.MethodPost, "/api/v1/auth/register",
		map[string]string{"name": "Tenant " + label, "email": c.email, "password": password})
	if status != http.StatusCreated {
		t.Fatalf("register %s: %d %v", label, status, body)
	}
	c.userID = dataID(t, body, "id")

	status, body = c.do(http.MethodPost, "/api/v1/auth/login",
		map[string]string{"email": c.email, "password": password})
	if status != http.StatusOK {
		t.Fatalf("login %s: %d %v", label, status, body)
	}

	return c
}

func TestCrossTenantAPIAccessIsDenied(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping API integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := database.NewPool(ctx, database.Config{
		URL: url, MaxConns: 4, MaxConnLifetime: time.Hour, MaxConnIdleTime: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	// t.Cleanup (not defer): cleanups run LIFO after the test, so the
	// data cleanup registered below still has an open pool.
	t.Cleanup(pool.Close)

	if err := database.CheckRestrictedRole(ctx, pool); err != nil {
		t.Fatalf("TEST_DATABASE_URL must use the insightiq_app role: %v", err)
	}

	server := httptest.NewServer(testRouter(t, pool))
	defer server.Close()

	alice := newSignedInClient(t, server.URL, "alice")
	bob := newSignedInClient(t, server.URL, "bob")

	createOrg := func(c *apiClient, label string) uuid.UUID {
		status, body := c.do(http.MethodPost, "/api/v1/orgs/", map[string]string{
			"name": label, "slug": "xt-" + label + "-" + uuid.NewString()[:8],
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
		if _, err := pool.Exec(ctx, `DELETE FROM organizations WHERE id = ANY($1)`, []uuid.UUID{orgA, orgB}); err != nil {
			t.Errorf("cleanup orgs: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = ANY($1)`, []uuid.UUID{alice.userID, bob.userID}); err != nil {
			t.Errorf("cleanup users: %v", err)
		}
	})

	// Bob creates a pending invitation in B so there is something to target.
	status, body := bob.do(http.MethodPost, "/api/v1/orgs/"+orgB.String()+"/invitations",
		map[string]string{"email": "invitee-" + uuid.NewString()[:8] + "@example.test", "role": "viewer"})
	if status != http.StatusCreated {
		t.Fatalf("bob invite: %d %v", status, body)
	}
	invitationB := dataID(t, body, "id")

	b := "/api/v1/orgs/" + orgB.String()

	// Alice is authenticated and owns org A, but swaps in org B's ID.
	// Every attempt must look exactly like a non-existent org (404).
	attacks := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"read org", http.MethodGet, b + "/", nil},
		{"list members", http.MethodGet, b + "/members", nil},
		{"list invitations", http.MethodGet, b + "/invitations", nil},
		{"invite self", http.MethodPost, b + "/invitations",
			map[string]string{"email": alice.email, "role": "owner"}},
		{"promote self", http.MethodPatch, b + "/members/" + alice.userID.String(),
			map[string]string{"role": "owner"}},
		{"demote owner", http.MethodPatch, b + "/members/" + bob.userID.String(),
			map[string]string{"role": "viewer"}},
		{"remove owner", http.MethodDelete, b + "/members/" + bob.userID.String(), nil},
		{"revoke invitation", http.MethodDelete, b + "/invitations/" + invitationB.String(), nil},
	}

	for _, atk := range attacks {
		t.Run(atk.name, func(t *testing.T) {
			status, body := alice.do(atk.method, atk.path, atk.body)
			if status != http.StatusNotFound {
				t.Fatalf("want 404, got %d: %v", status, body)
			}
		})
	}

	// Alice's org list contains only A.
	status, body = alice.do(http.MethodGet, "/api/v1/orgs/", nil)
	if status != http.StatusOK {
		t.Fatalf("list orgs: %d", status)
	}
	for _, o := range body["data"].([]any) {
		if o.(map[string]any)["id"] == orgB.String() {
			t.Fatal("org B appears in alice's org list")
		}
	}

	// Nothing in B changed: Bob is still the sole owner and the invite is open.
	status, body = bob.do(http.MethodGet, b+"/members", nil)
	if status != http.StatusOK || len(body["data"].([]any)) != 1 {
		t.Fatalf("org B membership changed: %d %v", status, body)
	}
	if m := body["data"].([]any)[0].(map[string]any); m["role"] != "owner" {
		t.Fatalf("bob is no longer owner: %v", m)
	}
	status, body = bob.do(http.MethodGet, b+"/invitations", nil)
	if status != http.StatusOK || len(body["data"].([]any)) != 1 {
		t.Fatalf("org B invitation changed: %d %v", status, body)
	}

	// Unauthenticated requests get 401, not 404.
	anon := &apiClient{t: t, base: server.URL, http: &http.Client{Timeout: 10 * time.Second}}
	if status, _ := anon.do(http.MethodGet, b+"/", nil); status != http.StatusUnauthorized {
		t.Fatalf("anonymous: want 401, got %d", status)
	}
}
