DROP INDEX IF EXISTS idx_pipeline_runs_running_heartbeat;
ALTER TABLE pipeline_runs DROP COLUMN IF EXISTS heartbeat_at;
