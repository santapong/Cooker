-- 025_canary.up.sql
--
-- Canary deployments (OR-1). Two changes:
--
--   1. apps.canary_config — the per-app opt-in canary policy
--      (deploy strategy, traffic weight, auto-promote, health window).
--      Stored as JSONB alongside build_plan / deploy_target. Defaults to
--      '{}' so existing rows read back as a rolling deploy (the model's
--      CanaryConfig.Normalize turns an empty config into strategy=rolling).
--      Requires Postgres >= 12 for cheap ADD COLUMN ... DEFAULT (no full
--      table rewrite), matching migration 008/009.
--
--   2. app_canaries — the live state of an in-flight canary rollout
--      (one progressing row per app at a time, then a terminal history
--      row). Driven by service.CanaryService; never written by the user
--      directly. Keyed by id; app_id cascades on app delete.

ALTER TABLE apps
    ADD COLUMN IF NOT EXISTS canary_config JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE TABLE IF NOT EXISTS app_canaries (
    id                    TEXT PRIMARY KEY,
    app_id                TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    run_id                TEXT NOT NULL DEFAULT '',
    stable_image          TEXT NOT NULL DEFAULT '',
    canary_image          TEXT NOT NULL DEFAULT '',
    weight                INTEGER NOT NULL DEFAULT 0,
    status                TEXT NOT NULL,
    auto_promote          BOOLEAN NOT NULL DEFAULT FALSE,
    health_window_seconds INTEGER NOT NULL DEFAULT 0,
    healthy               BOOLEAN NOT NULL DEFAULT TRUE,
    message               TEXT NOT NULL DEFAULT '',
    promote_after         TIMESTAMPTZ,
    started_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at           TIMESTAMPTZ
);

-- At most one non-terminal canary per app: the service guards this too,
-- but a partial unique index makes a concurrent double-start impossible
-- at the database level (the loser gets a conflict, mapped to 409).
CREATE UNIQUE INDEX IF NOT EXISTS uq_app_canaries_active
    ON app_canaries (app_id)
    WHERE status = 'progressing';

-- History reads are "newest first for one app" (mirrors app_deploys).
CREATE INDEX IF NOT EXISTS idx_app_canaries_app_started
    ON app_canaries (app_id, started_at DESC);

-- The auto-promote sweep scans for progressing canaries whose window
-- has elapsed; a partial index keeps that scan narrow on installs with
-- many resolved rows.
CREATE INDEX IF NOT EXISTS idx_app_canaries_promote_after
    ON app_canaries (promote_after)
    WHERE status = 'progressing';
