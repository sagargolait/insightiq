-- Tenant isolation: a restricted application role plus row-level security on
-- every tenant-owned table.
--
-- Migrations keep running as the schema owner (superuser in local dev). The
-- API must connect as insightiq_app, which is not a superuser, does not own
-- the tables and cannot bypass RLS. The role is created NOLOGIN here so no
-- password lives in a migration; enable login per environment
-- (locally: `make db-app-role`).

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'insightiq_app') THEN
        CREATE ROLE insightiq_app NOLOGIN NOSUPERUSER NOBYPASSRLS
            NOCREATEDB NOCREATEROLE NOINHERIT;
    END IF;
END
$$;

GRANT USAGE ON SCHEMA public TO insightiq_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO insightiq_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO insightiq_app;

-- golang-migrate's bookkeeping table is not the app's business.
REVOKE ALL ON TABLE schema_migrations FROM insightiq_app;

-- Tables created by later migrations (as this same owner) get the same grants.
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO insightiq_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO insightiq_app;

-- The current tenant, set per transaction by database.WithTenantTx via
-- set_config('app.tenant_id', <uuid>, true).
--
-- NULLIF matters: once a transaction-local setting has been used on a pooled
-- connection, later reads return '' instead of NULL. Mapping '' to NULL means
-- "no tenant" matches no rows instead of raising a uuid cast error.
CREATE FUNCTION app_current_tenant() RETURNS uuid
    LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT NULLIF(current_setting('app.tenant_id', true), '')::uuid
$$;

-- RLS on tenant-owned tables. FORCE also applies the policy to the table
-- owner, so only superusers / BYPASSRLS roles skip it. USING filters reads,
-- updates and deletes; WITH CHECK rejects writes of another tenant's rows.
ALTER TABLE projects ENABLE ROW LEVEL SECURITY;
ALTER TABLE projects FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON projects
    USING (organization_id = app_current_tenant())
    WITH CHECK (organization_id = app_current_tenant());

ALTER TABLE dashboards ENABLE ROW LEVEL SECURITY;
ALTER TABLE dashboards FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON dashboards
    USING (organization_id = app_current_tenant())
    WITH CHECK (organization_id = app_current_tenant());

ALTER TABLE widgets ENABLE ROW LEVEL SECURITY;
ALTER TABLE widgets FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON widgets
    USING (organization_id = app_current_tenant())
    WITH CHECK (organization_id = app_current_tenant());

ALTER TABLE data_sources ENABLE ROW LEVEL SECURITY;
ALTER TABLE data_sources FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON data_sources
    USING (organization_id = app_current_tenant())
    WITH CHECK (organization_id = app_current_tenant());

ALTER TABLE conversations ENABLE ROW LEVEL SECURITY;
ALTER TABLE conversations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON conversations
    USING (organization_id = app_current_tenant())
    WITH CHECK (organization_id = app_current_tenant());

ALTER TABLE messages ENABLE ROW LEVEL SECURITY;
ALTER TABLE messages FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON messages
    USING (organization_id = app_current_tenant())
    WITH CHECK (organization_id = app_current_tenant());
