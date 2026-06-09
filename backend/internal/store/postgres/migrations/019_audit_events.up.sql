-- 019_audit_events.up.sql
--
-- Queryable audit trail (roadmap M5). The file/stdout audit sinks are
-- append-only with no read path; the in-product audit viewer (W11
-- SaaS §6 / Enterprise §6) needs a store it can filter. Rows mirror
-- audit.Event exactly — request/response bodies are never captured.
--
-- Written by the async db sink (drop-on-full, like the file sink) so
-- a slow/down Postgres can never block request handling. Retention
-- is enforced by a daily sweep (COOKER_AUDIT_DB_RETENTION).

CREATE TABLE IF NOT EXISTS audit_events (
    id         BIGSERIAL PRIMARY KEY,
    time       TIMESTAMPTZ NOT NULL,
    user_sub   TEXT NOT NULL DEFAULT '',
    user_email TEXT NOT NULL DEFAULT '',
    method     TEXT NOT NULL,
    path       TEXT NOT NULL,
    status     INT NOT NULL,
    latency_ms BIGINT NOT NULL DEFAULT 0,
    client_ip  TEXT NOT NULL DEFAULT ''
);

-- The viewer's default query is "newest first, optionally for one
-- user"; both shapes are served by these.
CREATE INDEX IF NOT EXISTS idx_audit_events_time
    ON audit_events (time DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_user_time
    ON audit_events (user_sub, time DESC);
