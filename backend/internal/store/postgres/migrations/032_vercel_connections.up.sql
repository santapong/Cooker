-- Credentials stay encrypted; provider snapshots are read-only imports.
CREATE TABLE vercel_connections (
 id TEXT PRIMARY KEY,
 configuration JSONB NOT NULL,
 credential BYTEA NOT NULL,
 version INTEGER NOT NULL DEFAULT 1,
 snapshot JSONB NOT NULL DEFAULT '{"projects":[],"deployments":[]}'::jsonb,
 last_attempt_at TIMESTAMPTZ,
 last_success_at TIMESTAMPTZ,
 next_sync_at TIMESTAMPTZ,
 retry_at TIMESTAMPTZ,
 sync_error TEXT NOT NULL DEFAULT '',
 sync_lease UUID,
 sync_lease_until TIMESTAMPTZ,
 failures INTEGER NOT NULL DEFAULT 0,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
