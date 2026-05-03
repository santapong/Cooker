-- Cooker pipeline run heartbeat
-- Powers boot-time orphan-sweep: rows where status='running' but
-- heartbeat_at is stale by > threshold get marked failed on next boot.

ALTER TABLE pipeline_runs
    ADD COLUMN IF NOT EXISTS heartbeat_at TIMESTAMPTZ;

-- Partial index — only rows in the running state matter for sweep.
CREATE INDEX IF NOT EXISTS idx_pipeline_runs_running_heartbeat
    ON pipeline_runs (heartbeat_at)
    WHERE status = 'running';
