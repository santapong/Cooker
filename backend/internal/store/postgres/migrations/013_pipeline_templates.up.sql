-- 013_pipeline_templates.up.sql
-- Pipeline templates (Phase 2 / F4). A Template is a reusable
-- pipeline definition: a JSONB blob conforming to the Pipeline
-- schema (stages + edges + variables) plus light metadata. Operators
-- seed templates via SQL or a future CLI; users "Create from
-- template" through the UI.

CREATE TABLE IF NOT EXISTS pipeline_templates (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    category     TEXT NOT NULL DEFAULT '',
    schema       JSONB NOT NULL,
    icon_url     TEXT NOT NULL DEFAULT '',
    enabled      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Gallery page filters by enabled + category. Partial index keeps
-- the scan size proportional to live templates.
CREATE INDEX IF NOT EXISTS pipeline_templates_enabled_idx
    ON pipeline_templates (category, name)
    WHERE enabled = TRUE;
