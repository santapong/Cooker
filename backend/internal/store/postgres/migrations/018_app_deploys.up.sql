-- 018_app_deploys.up.sql
--
-- Per-app deploy history (roadmap M3). Before this table, "what did
-- this app last ship?" lived only in frontend component state —
-- rollback and drift detection need it server-side.
--
-- Numbered 018: 016 is reserved by the run-list-indexes migration
-- (PR #102) and 017 by pipeline_run_deadline (PR #104); the migration
-- runner tolerates gaps.
--
-- kind: 'deploy' (normal app deploy) or 'rollback' (re-deploy of an
-- earlier image). image_ref/digest are empty for compose multi-image
-- deploys, which v1 rollback does not support.

CREATE TABLE IF NOT EXISTS app_deploys (
    id          TEXT PRIMARY KEY,
    app_id      TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    run_id      TEXT NOT NULL,
    pipeline_id TEXT NOT NULL DEFAULT '',
    image_ref   TEXT NOT NULL DEFAULT '',
    digest      TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL,
    kind        TEXT NOT NULL DEFAULT 'deploy',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- History reads are always "newest first for one app".
CREATE INDEX IF NOT EXISTS idx_app_deploys_app_created
    ON app_deploys (app_id, created_at DESC);
