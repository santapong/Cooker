-- Cooker initial schema
-- Stores pipeline definitions, runs, and environments

CREATE TABLE IF NOT EXISTS pipelines (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT DEFAULT '',
    stages      JSONB NOT NULL DEFAULT '[]',
    edges       JSONB NOT NULL DEFAULT '[]',
    variables   JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS pipeline_runs (
    id          TEXT PRIMARY KEY,
    pipeline_id TEXT NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    status      TEXT NOT NULL DEFAULT 'pending',
    stage_runs  JSONB NOT NULL DEFAULT '[]',
    env_statuses JSONB NOT NULL DEFAULT '[]',
    variables   JSONB NOT NULL DEFAULT '{}',
    started_at  TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    error       TEXT DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS environments (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    sort_order  INT NOT NULL DEFAULT 0,
    target      JSONB NOT NULL DEFAULT '{}',
    promotion   JSONB NOT NULL DEFAULT '{}',
    variables   JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_pipeline_runs_pipeline_id ON pipeline_runs(pipeline_id);
CREATE INDEX IF NOT EXISTS idx_pipeline_runs_status ON pipeline_runs(status);
CREATE INDEX IF NOT EXISTS idx_environments_order ON environments(sort_order);
