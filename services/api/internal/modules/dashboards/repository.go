package dashboards

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sagargolait/insightiq/services/api/internal/platform/database"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrDashboardNotFound = errors.New("dashboard not found")
	ErrWidgetNotFound    = errors.New("widget not found")
)

// Repository is the data-access contract for dashboards and widgets. Every
// method is tenant-scoped: orgID is both an argument and the RLS tenant, so a
// query can never reach another organization's rows.
type Repository interface {
	MemberRole(ctx context.Context, orgID, userID uuid.UUID) (string, bool, error)

	CreateDashboard(ctx context.Context, d NewDashboard) (Dashboard, error)
	ListDashboards(ctx context.Context, orgID uuid.UUID) ([]Dashboard, error)
	GetDashboard(ctx context.Context, orgID, dashboardID uuid.UUID) (Dashboard, error)
	UpdateDashboard(ctx context.Context, orgID, dashboardID uuid.UUID, p DashboardPatch) (Dashboard, error)
	DeleteDashboard(ctx context.Context, orgID, dashboardID uuid.UUID) error

	CreateWidget(ctx context.Context, w NewWidget) (Widget, error)
	ListWidgets(ctx context.Context, orgID, dashboardID uuid.UUID) ([]Widget, error)
	GetWidget(ctx context.Context, orgID, dashboardID, widgetID uuid.UUID) (Widget, error)
	UpdateWidget(ctx context.Context, orgID, dashboardID, widgetID uuid.UUID, p WidgetPatch) (Widget, error)
	DeleteWidget(ctx context.Context, orgID, dashboardID, widgetID uuid.UUID) error
}

type PostgresRepository struct {
	db *pgxpool.Pool
}

func NewPostgresRepository(db *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{db: db}
}

// MemberRole returns the actor's role in orgID. memberships is not an RLS
// table, so this reads the bare pool. ok is false when the user is not a
// member (so the caller can return a 404 that hides the org's existence).
func (r *PostgresRepository) MemberRole(ctx context.Context, orgID, userID uuid.UUID) (string, bool, error) {
	var role string
	err := r.db.QueryRow(ctx, `
        SELECT m.role
        FROM memberships m
        JOIN organizations o ON o.id = m.organization_id
        WHERE m.organization_id = $1
          AND m.user_id = $2
          AND o.status = 'active'
    `, orgID, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get member role: %w", err)
	}
	return role, true, nil
}

func scanDashboard(row pgx.CollectableRow) (Dashboard, error) {
	var d Dashboard
	err := row.Scan(
		&d.ID, &d.OrganizationID, &d.Name, &d.Description, &d.Layout,
		&d.CreatedBy, &d.UpdatedBy, &d.CreatedAt, &d.UpdatedAt,
	)
	return d, err
}

const dashboardColumns = `
    id, organization_id, name, description, layout,
    created_by, updated_by, created_at, updated_at`

func (r *PostgresRepository) CreateDashboard(ctx context.Context, d NewDashboard) (Dashboard, error) {
	var created Dashboard
	err := database.WithTenantTx(ctx, r.db, d.OrganizationID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
            INSERT INTO dashboards (organization_id, name, description, layout, created_by, updated_by)
            VALUES ($1, $2, $3, COALESCE($4, '{}'::jsonb), $5, $5)
            RETURNING `+dashboardColumns, d.OrganizationID, d.Name, d.Description, d.Layout, d.CreatedBy)
		if err != nil {
			return fmt.Errorf("insert dashboard: %w", err)
		}
		created, err = pgx.CollectExactlyOneRow(rows, scanDashboard)
		return err
	})
	if err != nil {
		return Dashboard{}, err
	}
	return created, nil
}

func (r *PostgresRepository) ListDashboards(ctx context.Context, orgID uuid.UUID) ([]Dashboard, error) {
	var list []Dashboard
	err := database.WithTenantTx(ctx, r.db, orgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
            SELECT `+dashboardColumns+`
            FROM dashboards
            WHERE organization_id = $1
            ORDER BY created_at DESC`, orgID)
		if err != nil {
			return fmt.Errorf("list dashboards: %w", err)
		}
		list, err = pgx.CollectRows(rows, scanDashboard)
		return err
	})
	if err != nil {
		return nil, err
	}
	return list, nil
}

func (r *PostgresRepository) GetDashboard(ctx context.Context, orgID, dashboardID uuid.UUID) (Dashboard, error) {
	var d Dashboard
	err := database.WithTenantTx(ctx, r.db, orgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
            SELECT `+dashboardColumns+`
            FROM dashboards
            WHERE id = $1 AND organization_id = $2`, dashboardID, orgID)
		if err != nil {
			return fmt.Errorf("get dashboard: %w", err)
		}
		d, err = pgx.CollectExactlyOneRow(rows, scanDashboard)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrDashboardNotFound
		}
		return err
	})
	if err != nil {
		return Dashboard{}, err
	}
	return d, nil
}

func (r *PostgresRepository) UpdateDashboard(
	ctx context.Context,
	orgID, dashboardID uuid.UUID,
	p DashboardPatch,
) (Dashboard, error) {
	var d Dashboard
	err := database.WithTenantTx(ctx, r.db, orgID, func(tx pgx.Tx) error {
		// COALESCE keeps the existing value when a patch field is nil, so a
		// partial update only touches the fields the caller supplied.
		rows, err := tx.Query(ctx, `
            UPDATE dashboards
            SET name        = COALESCE($3, name),
                description = CASE WHEN $4::boolean THEN $5 ELSE description END,
                layout      = COALESCE($6, layout),
                updated_by  = $7,
                updated_at  = NOW()
            WHERE id = $1 AND organization_id = $2
            RETURNING `+dashboardColumns,
			dashboardID, orgID,
			p.Name,
			p.Description != nil, p.Description,
			p.Layout,
			p.UpdatedBy,
		)
		if err != nil {
			return fmt.Errorf("update dashboard: %w", err)
		}
		d, err = pgx.CollectExactlyOneRow(rows, scanDashboard)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrDashboardNotFound
		}
		return err
	})
	if err != nil {
		return Dashboard{}, err
	}
	return d, nil
}

func (r *PostgresRepository) DeleteDashboard(ctx context.Context, orgID, dashboardID uuid.UUID) error {
	return database.WithTenantTx(ctx, r.db, orgID, func(tx pgx.Tx) error {
		// Widgets cascade via the (dashboard_id, organization_id) FK.
		tag, err := tx.Exec(ctx, `
            DELETE FROM dashboards
            WHERE id = $1 AND organization_id = $2`, dashboardID, orgID)
		if err != nil {
			return fmt.Errorf("delete dashboard: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrDashboardNotFound
		}
		return nil
	})
}

func scanWidget(row pgx.CollectableRow) (Widget, error) {
	var w Widget
	err := row.Scan(
		&w.ID, &w.OrganizationID, &w.DashboardID, &w.Type, &w.Title,
		&w.Config, &w.Position, &w.Size, &w.CreatedAt, &w.UpdatedAt,
	)
	return w, err
}

const widgetColumns = `
    id, organization_id, dashboard_id, type, title,
    config, position, size, created_at, updated_at`

// dashboardExists checks within the current tenant tx that the dashboard is
// present, so a widget write against a missing dashboard reports the dashboard
// as not found rather than tripping a foreign-key error.
func dashboardExists(ctx context.Context, tx pgx.Tx, orgID, dashboardID uuid.UUID) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, `
        SELECT EXISTS (
            SELECT 1 FROM dashboards WHERE id = $1 AND organization_id = $2
        )`, dashboardID, orgID).Scan(&exists)
	return exists, err
}

func (r *PostgresRepository) CreateWidget(ctx context.Context, w NewWidget) (Widget, error) {
	var created Widget
	err := database.WithTenantTx(ctx, r.db, w.OrganizationID, func(tx pgx.Tx) error {
		ok, err := dashboardExists(ctx, tx, w.OrganizationID, w.DashboardID)
		if err != nil {
			return fmt.Errorf("check dashboard: %w", err)
		}
		if !ok {
			return ErrDashboardNotFound
		}

		rows, err := tx.Query(ctx, `
            INSERT INTO widgets (organization_id, dashboard_id, type, title, config, position, size)
            VALUES ($1, $2, $3, $4, COALESCE($5,'{}'::jsonb), COALESCE($6,'{}'::jsonb), COALESCE($7,'{}'::jsonb))
            RETURNING `+widgetColumns,
			w.OrganizationID, w.DashboardID, w.Type, w.Title, w.Config, w.Position, w.Size)
		if err != nil {
			return fmt.Errorf("insert widget: %w", err)
		}
		created, err = pgx.CollectExactlyOneRow(rows, scanWidget)
		return err
	})
	if err != nil {
		return Widget{}, err
	}
	return created, nil
}

func (r *PostgresRepository) ListWidgets(ctx context.Context, orgID, dashboardID uuid.UUID) ([]Widget, error) {
	var list []Widget
	err := database.WithTenantTx(ctx, r.db, orgID, func(tx pgx.Tx) error {
		ok, err := dashboardExists(ctx, tx, orgID, dashboardID)
		if err != nil {
			return fmt.Errorf("check dashboard: %w", err)
		}
		if !ok {
			return ErrDashboardNotFound
		}

		rows, err := tx.Query(ctx, `
            SELECT `+widgetColumns+`
            FROM widgets
            WHERE organization_id = $1 AND dashboard_id = $2
            ORDER BY created_at`, orgID, dashboardID)
		if err != nil {
			return fmt.Errorf("list widgets: %w", err)
		}
		list, err = pgx.CollectRows(rows, scanWidget)
		return err
	})
	if err != nil {
		return nil, err
	}
	return list, nil
}

func (r *PostgresRepository) GetWidget(ctx context.Context, orgID, dashboardID, widgetID uuid.UUID) (Widget, error) {
	var w Widget
	err := database.WithTenantTx(ctx, r.db, orgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
            SELECT `+widgetColumns+`
            FROM widgets
            WHERE id = $1 AND dashboard_id = $2 AND organization_id = $3`,
			widgetID, dashboardID, orgID)
		if err != nil {
			return fmt.Errorf("get widget: %w", err)
		}
		w, err = pgx.CollectExactlyOneRow(rows, scanWidget)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrWidgetNotFound
		}
		return err
	})
	if err != nil {
		return Widget{}, err
	}
	return w, nil
}

func (r *PostgresRepository) UpdateWidget(
	ctx context.Context,
	orgID, dashboardID, widgetID uuid.UUID,
	p WidgetPatch,
) (Widget, error) {
	var w Widget
	err := database.WithTenantTx(ctx, r.db, orgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
            UPDATE widgets
            SET title    = COALESCE($4, title),
                config   = COALESCE($5, config),
                position = COALESCE($6, position),
                size     = COALESCE($7, size),
                updated_at = NOW()
            WHERE id = $1 AND dashboard_id = $2 AND organization_id = $3
            RETURNING `+widgetColumns,
			widgetID, dashboardID, orgID,
			p.Title, p.Config, p.Position, p.Size)
		if err != nil {
			return fmt.Errorf("update widget: %w", err)
		}
		w, err = pgx.CollectExactlyOneRow(rows, scanWidget)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrWidgetNotFound
		}
		return err
	})
	if err != nil {
		return Widget{}, err
	}
	return w, nil
}

func (r *PostgresRepository) DeleteWidget(ctx context.Context, orgID, dashboardID, widgetID uuid.UUID) error {
	return database.WithTenantTx(ctx, r.db, orgID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
            DELETE FROM widgets
            WHERE id = $1 AND dashboard_id = $2 AND organization_id = $3`,
			widgetID, dashboardID, orgID)
		if err != nil {
			return fmt.Errorf("delete widget: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrWidgetNotFound
		}
		return nil
	})
}
