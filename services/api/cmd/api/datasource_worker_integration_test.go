package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/sagargolait/insightiq/services/api/internal/modules/datasources"
	"github.com/sagargolait/insightiq/services/api/internal/platform/crypto"
	"github.com/sagargolait/insightiq/services/api/internal/platform/database"
)

// TestWorkerDiscoversSchemaFromQueue proves the background worker drains an
// enqueued schema-discovery job end to end: it claims the job, connects to the
// external database, stores the discovered schema, and marks the job succeeded.
//
// Requires TEST_DATABASE_URL (app DB, insightiq_app) and PROBE_OWNER_URL (owner,
// to provision the external database and seed the source row).
func TestWorkerDiscoversSchemaFromQueue(t *testing.T) {
	appURL := os.Getenv("TEST_DATABASE_URL")
	ownerURL := os.Getenv("PROBE_OWNER_URL")
	if appURL == "" || ownerURL == "" {
		t.Skip("TEST_DATABASE_URL and PROBE_OWNER_URL required; skipping worker integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ext := provisionExternalDB(t, ctx, ownerURL, "worker_ext", "invoices", "gamma-invoice")

	// Owner connection to seed the app-side org/user/source rows directly.
	owner, err := pgx.Connect(ctx, ownerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close(ctx)

	var orgID, userID, sourceID uuid.UUID
	if err := owner.QueryRow(ctx, `INSERT INTO users(name,email,password_hash) VALUES('w',$1,'x') RETURNING id`,
		"worker-"+uuid.NewString()[:8]+"@x.test").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRow(ctx, `INSERT INTO organizations(name,slug) VALUES('w',$1) RETURNING id`,
		"worker-"+uuid.NewString()[:8]).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner')`, orgID, userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = owner.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, orgID)
		_, _ = owner.Exec(ctx, `DELETE FROM users WHERE id=$1`, userID)
	})

	// Encrypt the external password with a test cipher and store an active source.
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	cipher, err := crypto.New(map[string][]byte{"v1": key}, "v1")
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := cipher.Encrypt(ext.password)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRow(ctx, `
        INSERT INTO data_sources
            (organization_id, name, type, status, host, port, database_name, username, encrypted_credentials, key_version, created_by)
        VALUES ($1,'wh','postgres','active',$2,$3,$4,$5,$6,'v1',$7)
        RETURNING id`,
		orgID, ext.host, ext.port, ext.database, ext.username, envelope, userID).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}

	// App-role pool + repo + service + worker, exactly as production wires them.
	pool, err := database.NewPool(ctx, database.Config{
		URL: appURL, MaxConns: 4, MaxConnLifetime: time.Hour, MaxConnIdleTime: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	repo := datasources.NewPostgresRepository(pool)
	svc := datasources.NewService(repo, cipher, datasources.NewConnector(), datasources.NewExecutor())
	worker := datasources.NewWorker(repo, svc, nil)

	// Enqueue a discovery job, then run a single worker pass.
	if err := repo.EnqueueJob(ctx, orgID, sourceID, datasources.JobSchemaDiscovery); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	worker.DrainOnce(ctx)

	// Schema must now be populated with the external table.
	cols, err := repo.ListSchema(ctx, orgID, sourceID)
	if err != nil {
		t.Fatalf("list schema: %v", err)
	}
	seen := false
	for _, c := range cols {
		if c.TableName == "invoices" {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("worker did not discover table 'invoices'; got %d columns", len(cols))
	}

	// The job must be marked succeeded (no longer pending/running).
	var status string
	if err := queryJobStatus(ctx, owner, sourceID, &status); err != nil {
		t.Fatalf("read job status: %v", err)
	}
	if status != "succeeded" {
		t.Fatalf("job status = %q, want succeeded", status)
	}
}

func queryJobStatus(ctx context.Context, conn *pgx.Conn, sourceID uuid.UUID, dst *string) error {
	return conn.QueryRow(ctx, `
        SELECT status FROM data_source_jobs
        WHERE data_source_id = $1 AND kind = 'schema_discovery'
        ORDER BY updated_at DESC LIMIT 1`, sourceID).Scan(dst)
}
