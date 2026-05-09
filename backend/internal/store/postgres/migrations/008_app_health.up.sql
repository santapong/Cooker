-- Cooker app post-deploy health probe
-- Powers AppHealthChecker (Week 1 observability): a per-app
-- background probe writes the latest readiness verdict so the UI
-- can show whether the app is actually serving after a deploy.
-- Probe writes go through AppStore.UpdateHealth and intentionally
-- do not bump the apps.version column (health is observational
-- state, not user-visible configuration).

ALTER TABLE apps
    ADD COLUMN IF NOT EXISTS health_status     TEXT        NOT NULL DEFAULT 'unknown',
    ADD COLUMN IF NOT EXISTS health_checked_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS health_message    TEXT        NOT NULL DEFAULT '';

-- Partial index — only non-healthy apps need fast lookups for the
-- alerting / dashboard "what's broken" query. Keeps the index narrow
-- on installs with thousands of healthy apps.
CREATE INDEX IF NOT EXISTS idx_apps_unhealthy
    ON apps (health_checked_at)
    WHERE health_status <> 'healthy';
