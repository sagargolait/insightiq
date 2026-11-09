DROP TABLE IF EXISTS data_source_jobs;
DROP TABLE IF EXISTS data_source_schemas;

DROP INDEX IF EXISTS idx_data_sources_org;

ALTER TABLE data_sources
    DROP CONSTRAINT IF EXISTS data_sources_org_name_key,
    DROP CONSTRAINT IF EXISTS data_sources_status_check,
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS created_by,
    DROP COLUMN IF EXISTS last_error,
    DROP COLUMN IF EXISTS key_version,
    DROP COLUMN IF EXISTS encrypted_credentials,
    DROP COLUMN IF EXISTS username,
    DROP COLUMN IF EXISTS database_name,
    DROP COLUMN IF EXISTS port,
    DROP COLUMN IF EXISTS host;
