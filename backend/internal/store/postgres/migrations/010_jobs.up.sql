-- 010_jobs.up.sql
-- Durable async job queue (Phase 1 / A1 of the architecture-first plan).
--
-- Producers Enqueue rows; the worker pool (slice 2) claims them via
-- SELECT ... FOR UPDATE SKIP LOCKED so N workers can pick concurrently
-- without coordination. concurrency_key enforces per-key serialisation
-- (one running job per key) without a global mutex.

CREATE TABLE IF NOT EXISTS jobs (
    id              TEXT PRIMARY KEY,
    kind            TEXT NOT NULL,
    payload         JSONB NOT NULL DEFAULT '{}'::jsonb,
    status          TEXT NOT NULL DEFAULT 'pending',
    attempts        INTEGER NOT NULL DEFAULT 0,
    max_attempts    INTEGER NOT NULL DEFAULT 1,
    run_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_by       TEXT NOT NULL DEFAULT '',
    locked_at       TIMESTAMPTZ,
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ,
    last_error      TEXT NOT NULL DEFAULT '',
    concurrency_key TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT jobs_status_check CHECK (
        status IN ('pending','running','succeeded','failed','cancelled')
    )
);

-- Pickup hot path: workers scan pending rows by run_at. Partial
-- index keeps it small once terminal rows accumulate.
CREATE INDEX IF NOT EXISTS jobs_pickup_idx
    ON jobs (run_at)
    WHERE status = 'pending';

-- Concurrency-key probe: the NOT EXISTS subquery in Dequeue scans
-- (concurrency_key) for status='running'. Partial index again.
CREATE INDEX IF NOT EXISTS jobs_running_concurrency_idx
    ON jobs (concurrency_key)
    WHERE status = 'running' AND concurrency_key <> '';

-- Recent-history index for an eventual /admin/jobs UI.
CREATE INDEX IF NOT EXISTS jobs_created_idx
    ON jobs (created_at DESC);
