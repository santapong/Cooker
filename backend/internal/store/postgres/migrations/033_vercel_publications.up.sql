CREATE TABLE vercel_publications (
 id TEXT PRIMARY KEY,
 connection_id TEXT NOT NULL,
 connection_version INTEGER NOT NULL,
 actor TEXT NOT NULL,
 status TEXT NOT NULL,
 receipt JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 CHECK (status IN ('reviewed','queued','creating_project','submitting','checking','succeeded','failed','needs_attention'))
);
CREATE INDEX vercel_publications_history ON vercel_publications(created_at DESC);
