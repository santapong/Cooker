DROP INDEX IF EXISTS idx_apps_unhealthy;
ALTER TABLE apps
    DROP COLUMN IF EXISTS health_status,
    DROP COLUMN IF EXISTS health_checked_at,
    DROP COLUMN IF EXISTS health_message;
