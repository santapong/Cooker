-- 011_notification_targets.up.sql
-- Multi-channel notification targets. Rows represent destinations
-- (a Slack webhook URL, a Discord webhook, an SMTP mailbox, a
-- generic webhook URL) that the notifier package fans out to when
-- the executor emits an Event matching the target's event_types
-- filter.

CREATE TABLE IF NOT EXISTS notification_targets (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    kind        TEXT NOT NULL,
    config      JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- event_types empty means "fire on every event". Otherwise the
    -- dispatcher only delivers when the event's Type matches one
    -- of these strings.
    event_types TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT notification_targets_kind_check CHECK (
        kind IN ('slack','discord','webhook','email')
    )
);

-- The dispatcher hot path scans enabled rows. Partial index keeps
-- the scan size proportional to active configurations only.
CREATE INDEX IF NOT EXISTS notification_targets_enabled_idx
    ON notification_targets (kind)
    WHERE enabled = TRUE;
