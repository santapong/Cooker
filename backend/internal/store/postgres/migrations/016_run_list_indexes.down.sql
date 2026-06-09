-- 016_run_list_indexes.down.sql
--
-- Reverse 016: drop the new indexes and restore the single-column
-- pipeline_id index from 001 that the composite superseded.

DROP INDEX IF EXISTS idx_users_email_lower;
DROP INDEX IF EXISTS idx_apps_updated_at;
DROP INDEX IF EXISTS idx_pipelines_updated_at;
CREATE INDEX IF NOT EXISTS idx_pipeline_runs_pipeline_id
    ON pipeline_runs (pipeline_id);
DROP INDEX IF EXISTS idx_pipeline_runs_pipeline_created;
