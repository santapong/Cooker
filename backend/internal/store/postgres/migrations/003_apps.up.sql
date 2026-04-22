-- Apps are the user-facing unit of deployment: one GitHub repo +
-- build plan + deploy target. The Deploy button on an App
-- synthesizes a Clone→Build→Push→Deploy run at request time.
CREATE TABLE IF NOT EXISTS apps (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL UNIQUE,
    description     TEXT DEFAULT '',
    github_repo     TEXT NOT NULL,
    branch          TEXT NOT NULL DEFAULT 'main',
    build_plan      JSONB,
    deploy_target   JSONB NOT NULL DEFAULT '{}',
    registry_ref    TEXT DEFAULT '',
    environment_id  TEXT DEFAULT '',
    -- webhook_secret is the base64 of a sealed (AES-GCM) value. The
    -- handler layer seals it with the operator-provided key before
    -- it lands in this column.
    webhook_secret  TEXT DEFAULT '',
    auto_deploy     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_apps_github_repo ON apps(github_repo, branch);
