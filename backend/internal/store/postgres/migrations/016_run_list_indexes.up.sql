-- 016_run_list_indexes.up.sql
--
-- Indexes for the list queries the UI hits hardest (spof-and-database.md
-- severity items #3 #4 #9 #10). Before this migration every list was a
-- sequential scan + sort once history accumulated.

-- RunStore.List filters WHERE pipeline_id = $1 ORDER BY created_at DESC.
-- The composite index serves both the filter and the sort, and makes the
-- single-column idx_pipeline_runs_pipeline_id from 001 redundant (any
-- pipeline_id-only lookup uses the composite's prefix).
CREATE INDEX IF NOT EXISTS idx_pipeline_runs_pipeline_created
    ON pipeline_runs (pipeline_id, created_at DESC);
DROP INDEX IF EXISTS idx_pipeline_runs_pipeline_id;

-- PipelineStore.List and AppStore.List order by updated_at DESC.
CREATE INDEX IF NOT EXISTS idx_pipelines_updated_at
    ON pipelines (updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_apps_updated_at
    ON apps (updated_at DESC);

-- UserStore.GetByEmail matches WHERE LOWER(email) = LOWER($1); the raw
-- column index from 005 cannot serve that predicate, so every local-auth
-- sign-in scanned the table.
CREATE INDEX IF NOT EXISTS idx_users_email_lower
    ON users (LOWER(email));
