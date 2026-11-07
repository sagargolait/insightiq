package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrNoTenant is returned when a tenant-scoped transaction is requested
// without an organization ID.
var ErrNoTenant = errors.New("tenant id is required")

// TxBeginner is satisfied by *pgxpool.Pool (and pgx.Tx, for nesting via
// savepoints).
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// WithTenantTx runs fn inside a transaction whose RLS tenant is orgID.
//
// The tenant is set with set_config(..., is_local => true), so it lives
// exactly as long as the transaction: commit or rollback clears it, and the
// pooled connection goes back to the pool with no tenant attached. Every
// query against an RLS table (projects, dashboards, widgets, data_sources,
// conversations, messages) must run on the tx passed to fn; a query on the
// pool itself has no tenant and matches no rows.
//
// fn's error is returned unchanged (so callers can errors.Is sentinels) and
// the transaction is rolled back.
func WithTenantTx(
	ctx context.Context,
	db TxBeginner,
	orgID uuid.UUID,
	fn func(tx pgx.Tx) error,
) error {
	if orgID == uuid.Nil {
		return ErrNoTenant
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tenant tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	if _, err := tx.Exec(ctx,
		`SELECT set_config('app.tenant_id', $1, true)`, orgID.String(),
	); err != nil {
		return fmt.Errorf("set tenant: %w", err)
	}

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tenant tx: %w", err)
	}

	return nil
}

// ErrPrivilegedRole means the connection role ignores row-level security.
var ErrPrivilegedRole = errors.New("database role bypasses row-level security")

// CheckRestrictedRole verifies the pool's role is neither a superuser nor
// BYPASSRLS. Either would silently disable every tenant_isolation policy.
func CheckRestrictedRole(ctx context.Context, db interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}) error {
	var role string
	var super, bypass bool
	err := db.QueryRow(ctx, `
		SELECT rolname, rolsuper, rolbypassrls
		FROM pg_roles
		WHERE rolname = current_user
	`).Scan(&role, &super, &bypass)
	if err != nil {
		return fmt.Errorf("inspect database role: %w", err)
	}
	if super || bypass {
		return fmt.Errorf("%w: connected as %q (superuser=%t, bypassrls=%t)",
			ErrPrivilegedRole, role, super, bypass)
	}
	return nil
}
