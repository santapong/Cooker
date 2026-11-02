CREATE TABLE source_connections (
    id TEXT PRIMARY KEY,
    configuration JSONB NOT NULL,
    credential BYTEA NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
