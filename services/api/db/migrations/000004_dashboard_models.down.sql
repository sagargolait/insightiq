DROP INDEX IF EXISTS idx_widgets_dashboard;
CREATE INDEX idx_widgets_dashboard ON widgets(organization_id, dashboard_id);

ALTER TABLE widgets
    DROP CONSTRAINT IF EXISTS widgets_id_org_key,
    DROP CONSTRAINT IF EXISTS widgets_type_check,
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS size;

DROP INDEX IF EXISTS idx_dashboards_org;

ALTER TABLE dashboards
    DROP COLUMN IF EXISTS updated_by,
    DROP COLUMN IF EXISTS layout;

ALTER TABLE dashboards RENAME COLUMN name TO title;
