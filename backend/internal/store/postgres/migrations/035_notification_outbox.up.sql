-- Delivery events contain safe identity/status metadata only. Credentials stay
-- in the encrypted target catalog and are never copied into the outbox.
CREATE SEQUENCE notification_target_versions;
ALTER TABLE notification_targets ADD COLUMN IF NOT EXISTS version BIGINT NOT NULL DEFAULT nextval('notification_target_versions');
CREATE OR REPLACE FUNCTION cooker_notification_target_version() RETURNS trigger AS $$
BEGIN
    NEW.version := nextval('notification_target_versions');
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER notification_target_version BEFORE UPDATE ON notification_targets
FOR EACH ROW EXECUTE FUNCTION cooker_notification_target_version();

CREATE TABLE notification_policy (
    id TEXT PRIMARY KEY CHECK (id = 'v1'),
    cutover_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO notification_policy (id) VALUES ('v1');

CREATE TABLE notification_events (
    id TEXT PRIMARY KEY,
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- Retained independently of jobs. This is a receipt, not a queue FK that
-- disappears during retention. Non-notifiable/pre-cutover jobs also get one.
CREATE TABLE notification_job_receipts (
    job_id TEXT PRIMARY KEY,
    event_id TEXT,
    disposition TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- Failed captures are not acceptance receipts. Backoff lets newer healthy
-- jobs progress while unresolved sources remain retained and visible.
CREATE TABLE notification_source_repairs (
    job_id TEXT PRIMARY KEY,
    attempts INTEGER NOT NULL DEFAULT 1,
    next_capture_at TIMESTAMPTZ NOT NULL,
    last_code TEXT NOT NULL DEFAULT 'source_capture_unavailable',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE notification_deliveries (
    id TEXT PRIMARY KEY,
    event_id TEXT NOT NULL REFERENCES notification_events(id),
    target_id TEXT NOT NULL,
    target_version BIGINT NOT NULL,
    kind TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN
        ('pending','sending','retry_wait','delivered','failed','cancelled','needs_attention')),
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    lease_owner TEXT NOT NULL DEFAULT '',
    lease_until TIMESTAMPTZ,
    last_code TEXT NOT NULL DEFAULT '',
    last_http_status INTEGER NOT NULL DEFAULT 0,
    duplicate_possible BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(event_id, target_id)
);
CREATE INDEX notification_deliveries_pickup ON notification_deliveries(next_attempt_at)
WHERE state IN ('pending','retry_wait');
