-- Flesh out data_sources into a connectable PostgreSQL source, and add the
-- schema-metadata and background-job tables the connection layer needs.
--
-- data_sources already has RLS (migration 000003); adding columns leaves the
-- tenant_isolation policy unchanged. The two new tables are tenant-owned, so
-- they get the same ENABLE + FORCE ROW LEVEL SECURITY + tenant_isolation
-- treatment here. Grants are automatic via the default privileges set in 000003.

-- data_sources ---------------------------------------------------------------

-- Connection metadata (host/port/database/username) is plaintext; the password
-- lives only in encrypted_credentials (an AES-256-GCM envelope) tagged with the
-- key_version that sealed it so credentials survive key rotation. last_error is
-- a sanitized, non-sensitive failure reason surfaced to the UI.
ALTER TABLE data_sources
    ADD COLUMN host                  TEXT,
    ADD COLUMN port                  INTEGER,
    ADD COLUMN database_name         TEXT,
    ADD COLUMN username              TEXT,
    ADD COLUMN encrypted_credentials TEXT,
    ADD COLUMN key_version           TEXT,
    ADD COLUMN last_error            TEXT,
    ADD COLUMN created_by            UUID REFERENCES users(id),
    ADD COLUMN updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW();

-- Constrain status to the known connection states. A narrow CHECK keeps bad
-- rows out even if a caller bypasses the service-layer validation.
ALTER TABLE data_sources
    ADD CONSTRAINT data_sources_status_check
    CHECK (status IN ('pending', 'active', 'failed', 'disabled'));

-- One source name per organization.
ALTER TABLE data_sources
    ADD CONSTRAINT data_sources_org_name_key UNIQUE (organization_id, name);

CREATE INDEX idx_data_sources_org
    ON data_sources(organization_id, created_at DESC);

-- data_source_schemas --------------------------------------------------------

-- Discovered schema metadata: one row per column of each introspected table.
-- The query executor's schema allowlist is built from these rows.
CREATE TABLE data_source_schemas (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    data_source_id  UUID NOT NULL,
    schema_name     TEXT NOT NULL,
    table_name      TEXT NOT NULL,
    column_name     TEXT NOT NULL,
    data_type       TEXT NOT NULL,
    ordinal         INTEGER NOT NULL,
    discovered_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (data_source_id, schema_name, table_name, column_name),
    FOREIGN KEY (data_source_id, organization_id)
        REFERENCES data_sources(id, organization_id) ON DELETE CASCADE
);

CREATE INDEX idx_data_source_schemas_source
    ON data_source_schemas(organization_id, data_source_id, schema_name, table_name);

ALTER TABLE data_source_schemas ENABLE ROW LEVEL SECURITY;
ALTER TABLE data_source_schemas FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON data_source_schemas
    USING (organization_id = app_current_tenant())
    WITH CHECK (organization_id = app_current_tenant());

-- data_source_jobs -----------------------------------------------------------

-- Background work (schema discovery/refresh). Discovery runs synchronously for
-- now; a polling worker will claim these rows in a later milestone. The partial
-- unique index keeps at most one open job per (source, kind), making enqueue
-- idempotent.
CREATE TABLE data_source_jobs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    data_source_id  UUID NOT NULL,
    kind            TEXT NOT NULL CHECK (kind IN ('schema_discovery')),
    status          TEXT NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending', 'running', 'succeeded', 'failed')),
    attempts        INTEGER NOT NULL DEFAULT 0,
    max_attempts    INTEGER NOT NULL DEFAULT 3,
    last_error      TEXT,
    run_after       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (data_source_id, organization_id)
        REFERENCES data_sources(id, organization_id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX idx_data_source_jobs_open
    ON data_source_jobs(data_source_id, kind)
    WHERE status IN ('pending', 'running');

CREATE INDEX idx_data_source_jobs_claim
    ON data_source_jobs(status, run_after);

ALTER TABLE data_source_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE data_source_jobs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON data_source_jobs
    USING (organization_id = app_current_tenant())
    WITH CHECK (organization_id = app_current_tenant());
