-- Bring the dashboards and widgets tables up to the full data model: named
-- dashboards with a layout document and authorship, and widgets that carry a
-- type, a config, and separate position/size geometry.
--
-- RLS already covers both tables (migration 000003); this migration only
-- changes columns and indexes, so the tenant_isolation policies still apply
-- unchanged. New columns default to NULL/'{}' and need no policy edits.

-- Dashboards -----------------------------------------------------------------

-- The initial schema named the column `title`; the data model calls it `name`.
ALTER TABLE dashboards RENAME COLUMN title TO name;

ALTER TABLE dashboards
    ADD COLUMN layout JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN updated_by UUID REFERENCES users(id);

CREATE INDEX idx_dashboards_org ON dashboards(organization_id, created_at DESC);

-- Widgets --------------------------------------------------------------------

ALTER TABLE widgets
    ADD COLUMN size JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

-- Constrain type to the supported widget kinds. A narrow CHECK keeps bad
-- rows out even if a caller bypasses the service-layer validation.
ALTER TABLE widgets
    ADD CONSTRAINT widgets_type_check
    CHECK (type IN ('kpi', 'bar_chart', 'line_chart', 'pie_chart', 'table'));

-- Composite key so future tenant-aware FKs can reference a widget by
-- (id, organization_id) the way dashboards and projects already do.
ALTER TABLE widgets
    ADD CONSTRAINT widgets_id_org_key UNIQUE (id, organization_id);

-- idx_widgets_dashboard (organization_id, dashboard_id) from 000001 already
-- serves per-dashboard lookups; add created_at so listing is ordered cheaply.
DROP INDEX IF EXISTS idx_widgets_dashboard;
CREATE INDEX idx_widgets_dashboard
    ON widgets(organization_id, dashboard_id, created_at);
