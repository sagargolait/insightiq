package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sagargolait/insightiq/services/api/internal/platform/database"
)

// End-to-end dashboard/widget checks through the real router and Postgres,
// proving RLS isolates tenants and the permission matrix is enforced.
// Requires TEST_DATABASE_URL (migrated DB, insightiq_app role); skipped otherwise.

func TestDashboardTenantIsolationAndRBAC(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping dashboard integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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

	server := httptest.NewServer(newRouter(pool))
	defer server.Close()

	alice := newSignedInClient(t, server.URL, "dash-alice")
	bob := newSignedInClient(t, server.URL, "dash-bob")

	createOrg := func(c *apiClient, label string) uuid.UUID {
		status, body := c.do(http.MethodPost, "/api/v1/orgs/", map[string]string{
			"name": label, "slug": "dash-" + label + "-" + uuid.NewString()[:8],
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

	// Alice creates a dashboard + widget in org A.
	status, body := alice.do(http.MethodPost, "/api/v1/orgs/"+orgA.String()+"/dashboards",
		map[string]any{"name": "Revenue"})
	if status != http.StatusCreated {
		t.Fatalf("alice create dashboard: %d %v", status, body)
	}
	dashA := dataID(t, body, "id")

	status, body = alice.do(http.MethodPost,
		"/api/v1/orgs/"+orgA.String()+"/dashboards/"+dashA.String()+"/widgets",
		map[string]any{"type": "kpi", "title": "MRR", "config": map[string]any{"metric": "mrr"}})
	if status != http.StatusCreated {
		t.Fatalf("alice create widget: %d %v", status, body)
	}
	widgetA := dataID(t, body, "id")

	dashPathInB := "/api/v1/orgs/" + orgB.String() + "/dashboards/" + dashA.String()

	// Bob, authenticated in his own org, tries to reach Alice's dashboard by
	// swapping org B's ID into the path. Every attempt must be a 404 — RLS
	// hides the row and membership hides the org.
	attacks := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"read A's dashboard via B", http.MethodGet, dashPathInB, nil},
		{"list A's dashboards via B", http.MethodGet, "/api/v1/orgs/" + orgB.String() + "/dashboards/" + dashA.String() + "/widgets", nil},
		{"patch A's dashboard via B", http.MethodPatch, dashPathInB, map[string]any{"name": "pwned"}},
		{"delete A's dashboard via B", http.MethodDelete, dashPathInB, nil},
		{"read A's widget via B", http.MethodGet, dashPathInB + "/widgets/" + widgetA.String(), nil},
	}
	for _, atk := range attacks {
		t.Run(atk.name, func(t *testing.T) {
			status, body := bob.do(atk.method, atk.path, atk.body)
			if status != http.StatusNotFound {
				t.Fatalf("want 404, got %d: %v", status, body)
			}
		})
	}

	// Bob is also not a member of org A, so reaching it directly is a 404 too.
	status, body = bob.do(http.MethodGet, "/api/v1/orgs/"+orgA.String()+"/dashboards", nil)
	if status != http.StatusNotFound {
		t.Fatalf("bob read org A dashboards: want 404, got %d %v", status, body)
	}

	// Alice's dashboard is untouched.
	status, body = alice.do(http.MethodGet, "/api/v1/orgs/"+orgA.String()+"/dashboards/"+dashA.String(), nil)
	if status != http.StatusOK {
		t.Fatalf("alice dashboard gone: %d %v", status, body)
	}
	if data, _ := body["data"].(map[string]any); data["name"] != "Revenue" {
		t.Fatalf("alice dashboard mutated: %v", body)
	}
}

func TestDashboardViewerIsReadOnly(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping dashboard RBAC test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := database.NewPool(ctx, database.Config{
		URL: url, MaxConns: 4, MaxConnLifetime: time.Hour, MaxConnIdleTime: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	server := httptest.NewServer(newRouter(pool))
	defer server.Close()

	owner := newSignedInClient(t, server.URL, "rbac-owner")
	viewer := newSignedInClient(t, server.URL, "rbac-viewer")

	status, body := owner.do(http.MethodPost, "/api/v1/orgs/", map[string]string{
		"name": "rbac", "slug": "rbac-" + uuid.NewString()[:8],
	})
	if status != http.StatusCreated {
		t.Fatalf("create org: %d %v", status, body)
	}
	orgID := dataID(t, body, "id")

	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = ANY($1)`, []uuid.UUID{owner.userID, viewer.userID})
	})

	// Owner invites the viewer, viewer accepts.
	status, body = owner.do(http.MethodPost, "/api/v1/orgs/"+orgID.String()+"/invitations",
		map[string]string{"email": viewer.email, "role": "viewer"})
	if status != http.StatusCreated {
		t.Fatalf("invite viewer: %d %v", status, body)
	}
	token, _ := body["data"].(map[string]any)["token"].(string)
	if status, body := viewer.do(http.MethodPost, "/api/v1/invitations/accept",
		map[string]string{"token": token}); status != http.StatusOK {
		t.Fatalf("accept invite: %d %v", status, body)
	}

	base := "/api/v1/orgs/" + orgID.String() + "/dashboards"

	// Viewer cannot create.
	if status, body := viewer.do(http.MethodPost, base, map[string]any{"name": "Nope"}); status != http.StatusForbidden {
		t.Fatalf("viewer create: want 403, got %d %v", status, body)
	}

	// Owner creates; viewer can read it.
	status, body = owner.do(http.MethodPost, base, map[string]any{"name": "Shared"})
	if status != http.StatusCreated {
		t.Fatalf("owner create: %d %v", status, body)
	}
	dashID := dataID(t, body, "id")

	if status, _ := viewer.do(http.MethodGet, base+"/"+dashID.String(), nil); status != http.StatusOK {
		t.Fatalf("viewer read: want 200, got %d", status)
	}

	// Viewer cannot delete.
	if status, _ := viewer.do(http.MethodDelete, base+"/"+dashID.String(), nil); status != http.StatusForbidden {
		t.Fatalf("viewer delete: want 403, got %d", status)
	}
}
