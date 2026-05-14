-- 012_schedules.up.sql
-- Cron-triggered pipeline runs (Phase 2 / F2).
--
-- The scheduler scans rows with enabled=TRUE and next_run_at <= NOW(),
-- enqueues a pipeline-run job for each, and bumps next_run_at to
-- the next cron occurrence. A pg_advisory_lock outside this table
-- ("cooker.scheduler") ensures only one replica drives the loop at
-- a time — see internal/scheduler/scheduler.go.

CREATE TABLE IF NOT EXISTS schedules (
    id           TEXT PRIMARY KEY,
    pipeline_id  TEXT NOT NULL,
    name         TEXT NOT NULL DEFAULT '',
    cron_expr    TEXT NOT NULL,
    timezone     TEXT NOT NULL DEFAULT 'UTC',
    last_run_at  TIMESTAMPTZ,
    last_run_id  TEXT NOT NULL DEFAULT '',
    next_run_at  TIMESTAMPTZ NOT NULL,
    enabled      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Pickup index: scheduler scans by (enabled, next_run_at). Partial
-- index keeps it small once disabled schedules accumulate.
CREATE INDEX IF NOT EXISTS schedules_due_idx
    ON schedules (next_run_at)
    WHERE enabled = TRUE;

-- Pipeline lookup for the future settings UI.
CREATE INDEX IF NOT EXISTS schedules_pipeline_idx
    ON schedules (pipeline_id);
