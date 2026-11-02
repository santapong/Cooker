CREATE TABLE IF NOT EXISTS stage_log_streams (
    run_id TEXT NOT NULL,
    stage_id TEXT NOT NULL,
    last_seq BIGINT NOT NULL DEFAULT 0 CHECK (last_seq >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (run_id, stage_id)
);
CREATE INDEX IF NOT EXISTS stage_log_streams_updated ON stage_log_streams(updated_at);
CREATE TABLE IF NOT EXISTS stage_log_entries (
    run_id TEXT NOT NULL,
    stage_id TEXT NOT NULL,
    seq BIGINT NOT NULL CHECK (seq > 0),
    timestamp TIMESTAMPTZ NOT NULL,
    line TEXT NOT NULL,
    PRIMARY KEY (run_id, stage_id, seq),
    FOREIGN KEY (run_id, stage_id) REFERENCES stage_log_streams(run_id, stage_id) ON DELETE CASCADE
);
