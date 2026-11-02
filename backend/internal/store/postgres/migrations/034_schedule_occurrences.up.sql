-- Job admission and schedule advancement share one transaction. This journal
-- survives terminal-job retention and schedule deletion; an old occurrence
-- cannot become new work when its job has been cleaned up.
ALTER TABLE schedules ADD COLUMN IF NOT EXISTS version INTEGER NOT NULL DEFAULT 1;

CREATE TABLE IF NOT EXISTS schedule_occurrences (
    schedule_id      TEXT NOT NULL,
    scheduled_for    TIMESTAMPTZ NOT NULL,
    schedule_version INTEGER NOT NULL,
    pipeline_id      TEXT NOT NULL,
    cron_expr        TEXT NOT NULL,
    timezone         TEXT NOT NULL,
    run_id           TEXT NOT NULL UNIQUE,
    job_id           TEXT NOT NULL UNIQUE,
    admitted_at      TIMESTAMPTZ NOT NULL,
    next_run_at      TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (schedule_id, scheduled_for)
);
