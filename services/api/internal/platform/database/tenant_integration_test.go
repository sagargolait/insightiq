package database

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Integration tests run against TEST_DATABASE_URL, which must point at a
// migrated database and connect as the restricted insightiq_app role.
// Unset → skipped.

func testPool(t *testing.T, maxConns int32) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping database integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := NewPool(ctx, Config{
		URL:             url,
		MaxConns:        maxConns,
		MinConns:        0,
		MaxConnLifetime: time.Hour,
		MaxConnIdleTime: time.Minute,
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	// RLS tests are meaningless as a superuser — they would all "pass" by
	// seeing everything. Fail loudly instead.
	if err := CheckRestrictedRole(ctx, pool); err != nil {
		t.Fatalf("TEST_DATABASE_URL must use the insightiq_app role: %v", err)
	}

	return pool
}

// tenant is one organization with a user and one row in every RLS table.
type tenant struct {
	orgID          uuid.UUID
	userID         uuid.UUID
	projectID      uuid.UUID
	dashboardID    uuid.UUID
	widgetID       uuid.UUID
	dataSourceID   uuid.UUID
	conversationID uuid.UUID
	messageID      uuid.UUID
}

// rlsTables maps each tenant-owned table to the tenant field holding its row.
var rlsTables = []struct {
	name string
	id   func(tenant) uuid.UUID
}{
	{"projects", func(t tenant) uuid.UUID { return t.projectID }},
	{"dashboards", func(t tenant) uuid.UUID { return t.dashboardID }},
	{"widgets", func(t tenant) uuid.UUID { return t.widgetID }},
	{"data_sources", func(t tenant) uuid.UUID { return t.dataSourceID }},
	{"conversations", func(t tenant) uuid.UUID { return t.conversationID }},
	{"messages", func(t tenant) uuid.UUID { return t.messageID }},
}

func seedTenant(t *testing.T, pool *pgxpool.Pool, label string) tenant {
	t.Helper()
	ctx := context.Background()
	suffix := uuid.NewString()[:8]

	var tn tenant

	// users / organizations / memberships are not RLS tables.
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, name)
		VALUES ($1, 'x', $2) RETURNING id
	`, fmt.Sprintf("rls-%s-%s@example.test", label, suffix), label).Scan(&tn.userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO organizations (name, slug) VALUES ($1, $2) RETURNING id
	`, label, "rls-"+label+"-"+suffix).Scan(&tn.orgID); err != nil {
		t.Fatalf("seed org: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO memberships (organization_id, user_id, role) VALUES ($1, $2, 'owner')
	`, tn.orgID, tn.userID); err != nil {
		t.Fatalf("seed membership: %v", err)
	}

	err := WithTenantTx(ctx, pool, tn.orgID, func(tx pgx.Tx) error {
		steps := []struct {
			dst  *uuid.UUID
			sql  string
			args []any
		}{
			{&tn.projectID, `INSERT INTO projects (organization_id, name)
				VALUES ($1, $2) RETURNING id`, []any{tn.orgID, label + " project"}},
			{&tn.dashboardID, `INSERT INTO dashboards (organization_id, project_id, title, created_by)
				VALUES ($1, $2, $3, $4) RETURNING id`, []any{tn.orgID, &tn.projectID, label + " dash", tn.userID}},
			{&tn.widgetID, `INSERT INTO widgets (organization_id, dashboard_id, type, title)
				VALUES ($1, $2, 'kpi', $3) RETURNING id`, []any{tn.orgID, &tn.dashboardID, label + " widget"}},
			{&tn.dataSourceID, `INSERT INTO data_sources (organization_id, name, type)
				VALUES ($1, $2, 'postgres') RETURNING id`, []any{tn.orgID, label + " source"}},
			{&tn.conversationID, `INSERT INTO conversations (organization_id, user_id, title)
				VALUES ($1, $2, $3) RETURNING id`, []any{tn.orgID, tn.userID, label + " chat"}},
			{&tn.messageID, `INSERT INTO messages (organization_id, conversation_id, role, content)
				VALUES ($1, $2, 'user', $3) RETURNING id`, []any{tn.orgID, &tn.conversationID, label + " secret"}},
		}
		for _, s := range steps {
			// Dereference pointer args lazily so later steps see IDs from earlier ones.
			args := make([]any, len(s.args))
			for i, a := range s.args {
				if p, ok := a.(*uuid.UUID); ok {
					a = *p
				}
				args[i] = a
			}
			if err := tx.QueryRow(ctx, s.sql, args...).Scan(s.dst); err != nil {
				return fmt.Errorf("%s: %w", s.sql, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed tenant data: %v", err)
	}

	t.Cleanup(func() {
		ctx := context.Background()
		// conversations reference memberships without ON DELETE CASCADE, so
		// they must go before the org; messages cascade from conversations.
		_ = WithTenantTx(ctx, pool, tn.orgID, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `DELETE FROM conversations WHERE organization_id = $1`, tn.orgID)
			return err
		})
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, tn.orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, tn.userID)
	})

	return tn
}

func isRLSViolation(err error) bool {
	var pgErr *pgconn.PgError
	// 42501 insufficient_privilege: "new row violates row-level security policy"
	return errors.As(err, &pgErr) && pgErr.Code == "42501"
}

func TestRLSTenantSeesOnlyOwnRows(t *testing.T) {
	pool := testPool(t, 4)
	a := seedTenant(t, pool, "a")
	b := seedTenant(t, pool, "b")
	ctx := context.Background()

	for _, tbl := range rlsTables {
		t.Run(tbl.name, func(t *testing.T) {
			err := WithTenantTx(ctx, pool, a.orgID, func(tx pgx.Tx) error {
				// Read B's row by primary key — the "swap the ID" attack.
				var n int
				if err := tx.QueryRow(ctx,
					`SELECT count(*) FROM `+tbl.name+` WHERE id = $1`, tbl.id(b),
				).Scan(&n); err != nil {
					return err
				}
				if n != 0 {
					t.Errorf("tenant A can read tenant B's %s row", tbl.name)
				}

				// Explicitly filtering on B's org ID must not help either.
				if err := tx.QueryRow(ctx,
					`SELECT count(*) FROM `+tbl.name+` WHERE organization_id = $1`, b.orgID,
				).Scan(&n); err != nil {
					return err
				}
				if n != 0 {
					t.Errorf("tenant A sees %d of tenant B's %s rows by org filter", n, tbl.name)
				}

				// Own row is visible.
				if err := tx.QueryRow(ctx,
					`SELECT count(*) FROM `+tbl.name+` WHERE id = $1`, tbl.id(a),
				).Scan(&n); err != nil {
					return err
				}
				if n != 1 {
					t.Errorf("tenant A cannot see its own %s row", tbl.name)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRLSCrossTenantUpdateAndDeleteAffectNothing(t *testing.T) {
	pool := testPool(t, 4)
	a := seedTenant(t, pool, "a")
	b := seedTenant(t, pool, "b")
	ctx := context.Background()

	for _, tbl := range rlsTables {
		t.Run(tbl.name, func(t *testing.T) {
			err := WithTenantTx(ctx, pool, a.orgID, func(tx pgx.Tx) error {
				// organization_id = organization_id is a no-op update that
				// works on every table; RLS should make it match zero rows.
				tag, err := tx.Exec(ctx,
					`UPDATE `+tbl.name+` SET organization_id = organization_id WHERE id = $1`, tbl.id(b))
				if err != nil {
					return err
				}
				if tag.RowsAffected() != 0 {
					t.Errorf("tenant A updated tenant B's %s row", tbl.name)
				}

				tag, err = tx.Exec(ctx, `DELETE FROM `+tbl.name+` WHERE id = $1`, tbl.id(b))
				if err != nil {
					return err
				}
				if tag.RowsAffected() != 0 {
					t.Errorf("tenant A deleted tenant B's %s row", tbl.name)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}

			// B's row is still there, seen from B.
			err = WithTenantTx(ctx, pool, b.orgID, func(tx pgx.Tx) error {
				var n int
				if err := tx.QueryRow(ctx,
					`SELECT count(*) FROM `+tbl.name+` WHERE id = $1`, tbl.id(b),
				).Scan(&n); err != nil {
					return err
				}
				if n != 1 {
					t.Errorf("tenant B's %s row is gone", tbl.name)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRLSRejectsWritesIntoAnotherTenant(t *testing.T) {
	pool := testPool(t, 4)
	a := seedTenant(t, pool, "a")
	b := seedTenant(t, pool, "b")
	ctx := context.Background()

	t.Run("insert with other org id", func(t *testing.T) {
		err := WithTenantTx(ctx, pool, a.orgID, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx,
				`INSERT INTO projects (organization_id, name) VALUES ($1, 'planted')`, b.orgID)
			return err
		})
		if !isRLSViolation(err) {
			t.Fatalf("want RLS violation, got %v", err)
		}
	})

	t.Run("move own row to other org", func(t *testing.T) {
		err := WithTenantTx(ctx, pool, a.orgID, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx,
				`UPDATE data_sources SET organization_id = $1 WHERE id = $2`, b.orgID, a.dataSourceID)
			return err
		})
		if !isRLSViolation(err) {
			t.Fatalf("want RLS violation, got %v", err)
		}
	})
}

func TestRLSWithoutTenantSeesNothing(t *testing.T) {
	pool := testPool(t, 4)
	a := seedTenant(t, pool, "a")
	ctx := context.Background()

	// Directly on the pool: no transaction, no tenant.
	for _, tbl := range rlsTables {
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM `+tbl.name+` WHERE id = $1`, tbl.id(a),
		).Scan(&n); err != nil {
			t.Fatalf("%s: %v", tbl.name, err)
		}
		if n != 0 {
			t.Errorf("%s readable without tenant context", tbl.name)
		}
	}

	_, err := pool.Exec(ctx,
		`INSERT INTO projects (organization_id, name) VALUES ($1, 'no tenant')`, a.orgID)
	if !isRLSViolation(err) {
		t.Fatalf("insert without tenant: want RLS violation, got %v", err)
	}
}

func TestWithTenantTxRequiresTenant(t *testing.T) {
	called := false
	err := WithTenantTx(context.Background(), nil, uuid.Nil, func(pgx.Tx) error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrNoTenant) || called {
		t.Fatalf("want ErrNoTenant without calling fn, got err=%v called=%t", err, called)
	}
}

// TestTenantContextDoesNotLeakAcrossPooledConnections forces every
// transaction onto one physical connection and checks the tenant is gone
// after commit and after rollback.
func TestTenantContextDoesNotLeakAcrossPooledConnections(t *testing.T) {
	pool := testPool(t, 1)
	a := seedTenant(t, pool, "a")
	ctx := context.Background()

	var backendInTx, backendAfter uint32

	assertNoTenant := func(t *testing.T, when string) {
		t.Helper()
		var tenantSet bool
		var visible int
		if err := pool.QueryRow(ctx,
			`SELECT pg_backend_pid(), app_current_tenant() IS NOT NULL,
			        (SELECT count(*) FROM projects WHERE id = $1)`, a.projectID,
		).Scan(&backendAfter, &tenantSet, &visible); err != nil {
			t.Fatal(err)
		}
		if backendAfter != backendInTx {
			t.Fatalf("test invalid: expected the same backend (%d), got %d", backendInTx, backendAfter)
		}
		if tenantSet || visible != 0 {
			t.Fatalf("tenant leaked %s: tenant_set=%t visible_rows=%d", when, tenantSet, visible)
		}
	}

	t.Run("after commit", func(t *testing.T) {
		if err := WithTenantTx(ctx, pool, a.orgID, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&backendInTx)
		}); err != nil {
			t.Fatal(err)
		}
		assertNoTenant(t, "after commit")
	})

	t.Run("after rollback", func(t *testing.T) {
		boom := errors.New("boom")
		err := WithTenantTx(ctx, pool, a.orgID, func(tx pgx.Tx) error {
			if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&backendInTx); err != nil {
				return err
			}
			return boom
		})
		if !errors.Is(err, boom) {
			t.Fatalf("want fn error passed through, got %v", err)
		}
		assertNoTenant(t, "after rollback")
	})
}

// TestConcurrentTenantsStayIsolated hammers a small pool with interleaved
// tenants; any leak shows up as a tenant seeing the other's row.
func TestConcurrentTenantsStayIsolated(t *testing.T) {
	pool := testPool(t, 2)
	a := seedTenant(t, pool, "a")
	b := seedTenant(t, pool, "b")
	ctx := context.Background()

	var wg sync.WaitGroup
	errs := make(chan error, 200)

	for i := 0; i < 100; i++ {
		for _, pair := range [][2]tenant{{a, b}, {b, a}} {
			self, other := pair[0], pair[1]
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs <- WithTenantTx(ctx, pool, self.orgID, func(tx pgx.Tx) error {
					var own, foreign int
					if err := tx.QueryRow(ctx, `
						SELECT count(*) FILTER (WHERE id = $1),
						       count(*) FILTER (WHERE id = $2)
						FROM projects WHERE id IN ($1, $2)
					`, self.projectID, other.projectID).Scan(&own, &foreign); err != nil {
						return err
					}
					if own != 1 || foreign != 0 {
						return fmt.Errorf("tenant %s: own=%d foreign=%d", self.orgID, own, foreign)
					}
					return nil
				})
			}()
		}
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}
