package datasources

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sagargolait/insightiq/services/api/internal/platform/database"
)

var (
	ErrDataSourceNotFound = errors.New("data source not found")
	ErrNameTaken          = errors.New("a data source with this name already exists")
)

// Audit actions recorded against resource_type='data_source'.
const (
	auditCreated   = "data_source.created"
	auditUpdated   = "data_source.updated"
	auditDeleted   = "data_source.deleted"
	auditTested    = "data_source.tested"
	auditActivated = "data_source.activated"
	auditFailed    = "data_source.failed"
	auditDiscovery = "data_source.schema_refreshed"
	auditQueried   = "data_source.queried"
)

// Repository is the tenant-scoped data-access contract. orgID is both an
// argument and the RLS tenant on every method, so a query can never reach
// another organization's rows.
type Repository interface {
	MemberRole(ctx context.Context, orgID, userID uuid.UUID) (string, bool, error)

	Create(ctx context.Context, d NewDataSource, actorID uuid.UUID) (DataSource, error)
	List(ctx context.Context, orgID uuid.UUID) ([]DataSource, error)
	Get(ctx context.Context, orgID, sourceID uuid.UUID) (DataSource, error)
	Update(ctx context.Context, orgID, sourceID uuid.UUID, p DataSourcePatch, actorID uuid.UUID) (DataSource, error)
	Delete(ctx context.Context, orgID, sourceID uuid.UUID, actorID uuid.UUID) error

	// GetCredentials returns the sealed credential envelope and key version for
	// a source. Used only by the connector path, never by response paths.
	GetCredentials(ctx context.Context, orgID, sourceID uuid.UUID) (envelope, keyVersion string, err error)

	// UpdateStatus records the outcome of a connectivity test. lastErr is a
	// sanitized, non-sensitive reason (nil clears it).
	UpdateStatus(ctx context.Context, orgID, sourceID uuid.UUID, status string, lastErr *string, actorID uuid.UUID, action string) error

	// ReplaceSchema atomically replaces the stored schema for a source. It is the
	// idempotent unit of work shared by the HTTP path and the background worker;
	// job lifecycle is handled separately (see JobRepository).
	ReplaceSchema(ctx context.Context, orgID, sourceID uuid.UUID, cols []SchemaColumn, actorID uuid.UUID) error
	ListSchema(ctx context.Context, orgID, sourceID uuid.UUID) ([]SchemaColumn, error)

	// RecordQuery writes an audit row for an executed (or failed) query.
	RecordQuery(ctx context.Context, orgID, sourceID uuid.UUID, actorID uuid.UUID, durationMs int64, failure *string) error

	// EnqueueJob queues a background job (idempotent while one is open).
	EnqueueJob(ctx context.Context, orgID, sourceID uuid.UUID, kind string) error
}

// JobRepository is the worker-facing contract for the background job queue.
// *PostgresRepository implements it alongside Repository.
type JobRepository interface {
	ActiveTenants(ctx context.Context) ([]uuid.UUID, error)
	ClaimJob(ctx context.Context, orgID uuid.UUID) (Job, bool, error)
	CompleteJob(ctx context.Context, orgID, jobID uuid.UUID) error
	FailJob(ctx context.Context, orgID, jobID uuid.UUID, reason string, backoff time.Duration) error
}

type PostgresRepository struct {
	db *pgxpool.Pool
}

func NewPostgresRepository(db *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{db: db}
}

// MemberRole returns the actor's role in orgID. memberships is not an RLS
// table, so this reads the bare pool. ok is false when the user is not a member
// (so the caller can return a 404 that hides the org's existence).
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

// writeAudit inserts an audit_logs row inside the caller's tenant tx, so the
// audit trail commits (or rolls back) atomically with the change it describes.
func writeAudit(ctx context.Context, tx pgx.Tx, orgID, actorID, resourceID uuid.UUID, action string, metadata string) error {
	var actor *uuid.UUID
	if actorID != uuid.Nil {
		actor = &actorID
	}
	if metadata == "" {
		metadata = "{}"
	}
	_, err := tx.Exec(ctx, `
        INSERT INTO audit_logs (organization_id, actor_id, action, resource_type, resource_id, metadata)
        VALUES ($1, $2, $3, 'data_source', $4, $5::jsonb)
    `, orgID, actor, action, resourceID, metadata)
	if err != nil {
		return fmt.Errorf("write audit %s: %w", action, err)
	}
	return nil
}

const dataSourceColumns = `
    id, organization_id, name, type, status,
    host, port, database_name, username, last_error,
    created_by, created_at, updated_at`

func scanDataSource(row pgx.CollectableRow) (DataSource, error) {
	var d DataSource
	err := row.Scan(
		&d.ID, &d.OrganizationID, &d.Name, &d.Type, &d.Status,
		&d.Host, &d.Port, &d.Database, &d.Username, &d.LastError,
		&d.CreatedBy, &d.CreatedAt, &d.UpdatedAt,
	)
	return d, err
}

func (r *PostgresRepository) Create(ctx context.Context, d NewDataSource, actorID uuid.UUID) (DataSource, error) {
	var created DataSource
	err := database.WithTenantTx(ctx, r.db, d.OrganizationID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
            INSERT INTO data_sources
                (organization_id, name, type, status, host, port, database_name,
                 username, encrypted_credentials, key_version, created_by)
            VALUES ($1, $2, $3, 'pending', $4, $5, $6, $7, $8, $9, $10)
            RETURNING `+dataSourceColumns,
			d.OrganizationID, d.Name, d.Type, d.Host, d.Port, d.Database,
			d.Username, d.EncryptedCredentials, d.KeyVersion, d.CreatedBy)
		if err != nil {
			return mapWriteError(err)
		}
		created, err = pgx.CollectExactlyOneRow(rows, scanDataSource)
		if err != nil {
			return err
		}
		return writeAudit(ctx, tx, d.OrganizationID, actorID, created.ID, auditCreated, "")
	})
	if err != nil {
		return DataSource{}, err
	}
	return created, nil
}

func (r *PostgresRepository) List(ctx context.Context, orgID uuid.UUID) ([]DataSource, error) {
	var list []DataSource
	err := database.WithTenantTx(ctx, r.db, orgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
            SELECT `+dataSourceColumns+`
            FROM data_sources
            WHERE organization_id = $1
            ORDER BY created_at DESC`, orgID)
		if err != nil {
			return fmt.Errorf("list data sources: %w", err)
		}
		list, err = pgx.CollectRows(rows, scanDataSource)
		return err
	})
	if err != nil {
		return nil, err
	}
	return list, nil
}

func (r *PostgresRepository) Get(ctx context.Context, orgID, sourceID uuid.UUID) (DataSource, error) {
	var d DataSource
	err := database.WithTenantTx(ctx, r.db, orgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
            SELECT `+dataSourceColumns+`
            FROM data_sources
            WHERE id = $1 AND organization_id = $2`, sourceID, orgID)
		if err != nil {
			return fmt.Errorf("get data source: %w", err)
		}
		d, err = pgx.CollectExactlyOneRow(rows, scanDataSource)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrDataSourceNotFound
		}
		return err
	})
	if err != nil {
		return DataSource{}, err
	}
	return d, nil
}

func (r *PostgresRepository) GetCredentials(ctx context.Context, orgID, sourceID uuid.UUID) (string, string, error) {
	var envelope, keyVersion string
	err := database.WithTenantTx(ctx, r.db, orgID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
            SELECT COALESCE(encrypted_credentials, ''), COALESCE(key_version, '')
            FROM data_sources
            WHERE id = $1 AND organization_id = $2`, sourceID, orgID).Scan(&envelope, &keyVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrDataSourceNotFound
		}
		if err != nil {
			return fmt.Errorf("get credentials: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", "", err
	}
	return envelope, keyVersion, nil
}

func (r *PostgresRepository) Update(
	ctx context.Context,
	orgID, sourceID uuid.UUID,
	p DataSourcePatch,
	actorID uuid.UUID,
) (DataSource, error) {
	var d DataSource
	err := database.WithTenantTx(ctx, r.db, orgID, func(tx pgx.Tx) error {
		// COALESCE keeps the existing value when a patch field is nil, so a
		// partial update only touches the fields the caller supplied. A changed
		// connection resets status to pending so it must be re-tested.
		connChanged := p.Host != nil || p.Port != nil || p.Database != nil ||
			p.Username != nil || p.EncryptedCredentials != nil

		rows, err := tx.Query(ctx, `
            UPDATE data_sources
            SET name                  = COALESCE($3, name),
                host                  = COALESCE($4, host),
                port                  = COALESCE($5, port),
                database_name         = COALESCE($6, database_name),
                username              = COALESCE($7, username),
                encrypted_credentials = COALESCE($8, encrypted_credentials),
                key_version           = COALESCE($9, key_version),
                status                = CASE WHEN $10::boolean THEN 'pending' ELSE status END,
                last_error            = CASE WHEN $10::boolean THEN NULL ELSE last_error END,
                updated_at            = NOW()
            WHERE id = $1 AND organization_id = $2
            RETURNING `+dataSourceColumns,
			sourceID, orgID,
			p.Name, p.Host, p.Port, p.Database, p.Username,
			p.EncryptedCredentials, p.KeyVersion, connChanged)
		if err != nil {
			return mapWriteError(err)
		}
		d, err = pgx.CollectExactlyOneRow(rows, scanDataSource)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrDataSourceNotFound
		}
		if err != nil {
			return err
		}
		return writeAudit(ctx, tx, orgID, actorID, sourceID, auditUpdated, "")
	})
	if err != nil {
		return DataSource{}, err
	}
	return d, nil
}

func (r *PostgresRepository) Delete(ctx context.Context, orgID, sourceID uuid.UUID, actorID uuid.UUID) error {
	return database.WithTenantTx(ctx, r.db, orgID, func(tx pgx.Tx) error {
		// Schema rows and jobs cascade via the (data_source_id, organization_id) FK.
		tag, err := tx.Exec(ctx, `
            DELETE FROM data_sources
            WHERE id = $1 AND organization_id = $2`, sourceID, orgID)
		if err != nil {
			return fmt.Errorf("delete data source: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrDataSourceNotFound
		}
		return writeAudit(ctx, tx, orgID, actorID, sourceID, auditDeleted, "")
	})
}

func (r *PostgresRepository) UpdateStatus(
	ctx context.Context,
	orgID, sourceID uuid.UUID,
	status string,
	lastErr *string,
	actorID uuid.UUID,
	action string,
) error {
	return database.WithTenantTx(ctx, r.db, orgID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
            UPDATE data_sources
            SET status = $3, last_error = $4, updated_at = NOW()
            WHERE id = $1 AND organization_id = $2`, sourceID, orgID, status, lastErr)
		if err != nil {
			return fmt.Errorf("update status: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrDataSourceNotFound
		}
		if action != "" {
			return writeAudit(ctx, tx, orgID, actorID, sourceID, action, "")
		}
		return nil
	})
}

func scanSchemaColumn(row pgx.CollectableRow) (SchemaColumn, error) {
	var c SchemaColumn
	err := row.Scan(&c.SchemaName, &c.TableName, &c.ColumnName, &c.DataType, &c.Ordinal, &c.DiscoveredAt)
	return c, err
}

func (r *PostgresRepository) ReplaceSchema(
	ctx context.Context,
	orgID, sourceID uuid.UUID,
	cols []SchemaColumn,
	actorID uuid.UUID,
) error {
	return database.WithTenantTx(ctx, r.db, orgID, func(tx pgx.Tx) error {
		// The source must exist (within this tenant) before we attach schema.
		var exists bool
		if err := tx.QueryRow(ctx, `
            SELECT EXISTS (SELECT 1 FROM data_sources WHERE id = $1 AND organization_id = $2)
        `, sourceID, orgID).Scan(&exists); err != nil {
			return fmt.Errorf("check source: %w", err)
		}
		if !exists {
			return ErrDataSourceNotFound
		}

		// Refresh = replace. Delete the old metadata, then insert the new.
		if _, err := tx.Exec(ctx, `
            DELETE FROM data_source_schemas
            WHERE data_source_id = $1 AND organization_id = $2`, sourceID, orgID); err != nil {
			return fmt.Errorf("clear schema: %w", err)
		}

		// A multi-row INSERT (not COPY): RLS-forced tables reject COPY FROM for a
		// non-BYPASSRLS role, which insightiq_app is. Batch in chunks so a very
		// wide schema stays under the parameter limit (7 params/row).
		if err := insertSchemaRows(ctx, tx, orgID, sourceID, cols); err != nil {
			return fmt.Errorf("insert schema: %w", err)
		}

		// ReplaceSchema is deliberately job-agnostic: it is the idempotent unit of
		// work that both the synchronous HTTP path and the background worker call.
		// Job lifecycle (enqueue/claim/complete/fail) is managed separately.
		return writeAudit(ctx, tx, orgID, actorID, sourceID, auditDiscovery, "")
	})
}

// Job is a claimed unit of background work.
type Job struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	DataSourceID   uuid.UUID
	Kind           string
	Attempts       int
	MaxAttempts    int
}

// EnqueueJob inserts a pending job, idempotent via the partial unique index on
// (data_source_id, kind) WHERE status IN ('pending','running'): a second
// enqueue while one is open is a no-op.
func (r *PostgresRepository) EnqueueJob(ctx context.Context, orgID, sourceID uuid.UUID, kind string) error {
	return database.WithTenantTx(ctx, r.db, orgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
            INSERT INTO data_source_jobs (organization_id, data_source_id, kind, status)
            VALUES ($1, $2, $3, 'pending')
            ON CONFLICT (data_source_id, kind) WHERE status IN ('pending','running')
            DO NOTHING
        `, orgID, sourceID, kind)
		if err != nil {
			return fmt.Errorf("enqueue job: %w", err)
		}
		return nil
	})
}

// ClaimJob atomically claims the oldest due pending job for one tenant and marks
// it running. data_source_jobs is RLS-forced, so the claim runs inside the
// tenant tx (WithTenantTx sets app.tenant_id). FOR UPDATE SKIP LOCKED means
// concurrent workers never claim the same row. found is false when nothing is
// due for this tenant.
func (r *PostgresRepository) ClaimJob(ctx context.Context, orgID uuid.UUID) (Job, bool, error) {
	var job Job
	found := false
	err := database.WithTenantTx(ctx, r.db, orgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
            UPDATE data_source_jobs
            SET status = 'running', attempts = attempts + 1, updated_at = NOW()
            WHERE id = (
                SELECT id FROM data_source_jobs
                WHERE organization_id = $1
                  AND status = 'pending'
                  AND run_after <= NOW()
                ORDER BY run_after
                FOR UPDATE SKIP LOCKED
                LIMIT 1
            )
            RETURNING id, organization_id, data_source_id, kind, attempts, max_attempts
        `, orgID)
		if err != nil {
			return fmt.Errorf("claim job: %w", err)
		}
		job, err = pgx.CollectExactlyOneRow(rows, func(row pgx.CollectableRow) (Job, error) {
			var j Job
			e := row.Scan(&j.ID, &j.OrganizationID, &j.DataSourceID, &j.Kind, &j.Attempts, &j.MaxAttempts)
			return j, e
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // nothing due for this tenant
		}
		if err != nil {
			return err
		}
		found = true
		return nil
	})
	if err != nil {
		return Job{}, false, err
	}
	return job, found, nil
}

// ActiveTenants lists active organization IDs from the non-RLS organizations
// table. The worker iterates these and calls ClaimJob per tenant; tenants with
// nothing due simply return found=false. This avoids a tenant-less read of the
// RLS-forced data_source_jobs table.
func (r *PostgresRepository) ActiveTenants(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `SELECT id FROM organizations WHERE status = 'active'`)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	ids, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (uuid.UUID, error) {
		var id uuid.UUID
		e := row.Scan(&id)
		return id, e
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// CompleteJob marks a running job succeeded.
func (r *PostgresRepository) CompleteJob(ctx context.Context, orgID, jobID uuid.UUID) error {
	return database.WithTenantTx(ctx, r.db, orgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
            UPDATE data_source_jobs
            SET status = 'succeeded', last_error = NULL, updated_at = NOW()
            WHERE id = $1 AND organization_id = $2`, jobID, orgID)
		if err != nil {
			return fmt.Errorf("complete job: %w", err)
		}
		return nil
	})
}

// FailJob records a failed attempt. If attempts remain it is re-queued with an
// exponential backoff via run_after; at max_attempts it becomes terminally
// failed. reason must be a sanitized, non-sensitive string.
func (r *PostgresRepository) FailJob(ctx context.Context, orgID, jobID uuid.UUID, reason string, backoff time.Duration) error {
	return database.WithTenantTx(ctx, r.db, orgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
            UPDATE data_source_jobs
            SET status = CASE WHEN attempts >= max_attempts THEN 'failed' ELSE 'pending' END,
                run_after = NOW() + $3::interval,
                last_error = $4,
                updated_at = NOW()
            WHERE id = $1 AND organization_id = $2`,
			jobID, orgID, fmt.Sprintf("%d milliseconds", backoff.Milliseconds()), reason)
		if err != nil {
			return fmt.Errorf("fail job: %w", err)
		}
		return nil
	})
}

func (r *PostgresRepository) ListSchema(ctx context.Context, orgID, sourceID uuid.UUID) ([]SchemaColumn, error) {
	var list []SchemaColumn
	err := database.WithTenantTx(ctx, r.db, orgID, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `
            SELECT EXISTS (SELECT 1 FROM data_sources WHERE id = $1 AND organization_id = $2)
        `, sourceID, orgID).Scan(&exists); err != nil {
			return fmt.Errorf("check source: %w", err)
		}
		if !exists {
			return ErrDataSourceNotFound
		}

		rows, err := tx.Query(ctx, `
            SELECT schema_name, table_name, column_name, data_type, ordinal, discovered_at
            FROM data_source_schemas
            WHERE organization_id = $1 AND data_source_id = $2
            ORDER BY schema_name, table_name, ordinal`, orgID, sourceID)
		if err != nil {
			return fmt.Errorf("list schema: %w", err)
		}
		list, err = pgx.CollectRows(rows, scanSchemaColumn)
		return err
	})
	if err != nil {
		return nil, err
	}
	return list, nil
}

func (r *PostgresRepository) RecordQuery(
	ctx context.Context,
	orgID, sourceID uuid.UUID,
	actorID uuid.UUID,
	durationMs int64,
	failure *string,
) error {
	metadata := fmt.Sprintf(`{"duration_ms":%d,"ok":%t}`, durationMs, failure == nil)
	return database.WithTenantTx(ctx, r.db, orgID, func(tx pgx.Tx) error {
		action := auditQueried
		if failure != nil {
			action = auditFailed
		}
		return writeAudit(ctx, tx, orgID, actorID, sourceID, action, metadata)
	})
}

// insertSchemaRows bulk-inserts discovered columns with a multi-row INSERT,
// chunked to stay well under Postgres's 65535 bound-parameter limit (7 params
// per row → ~900 rows per statement).
func insertSchemaRows(ctx context.Context, tx pgx.Tx, orgID, sourceID uuid.UUID, cols []SchemaColumn) error {
	const perRow = 7
	const chunk = 900

	for start := 0; start < len(cols); start += chunk {
		end := start + chunk
		if end > len(cols) {
			end = len(cols)
		}
		batch := cols[start:end]

		args := make([]any, 0, len(batch)*perRow)
		var sb strings.Builder
		sb.WriteString(`INSERT INTO data_source_schemas
            (organization_id, data_source_id, schema_name, table_name, column_name, data_type, ordinal) VALUES `)
		for i, c := range batch {
			if i > 0 {
				sb.WriteByte(',')
			}
			n := i * perRow
			fmt.Fprintf(&sb, "($%d,$%d,$%d,$%d,$%d,$%d,$%d)",
				n+1, n+2, n+3, n+4, n+5, n+6, n+7)
			args = append(args, orgID, sourceID, c.SchemaName, c.TableName, c.ColumnName, c.DataType, c.Ordinal)
		}
		if _, err := tx.Exec(ctx, sb.String(), args...); err != nil {
			return err
		}
	}
	return nil
}

// mapWriteError translates Postgres constraint violations on data_sources into
// domain sentinels.
func mapWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" &&
		pgErr.ConstraintName == "data_sources_org_name_key" {
		return ErrNameTaken
	}
	return fmt.Errorf("write data source: %w", err)
}
