-- Review preparation is deliberately separate from executable publications.
-- Legacy publication lookup/deploy cannot read these IDs or queue these rows.
CREATE TABLE vercel_landing_update_reviews (
 id TEXT PRIMARY KEY,
 adoption_id TEXT NOT NULL,
 adoption_version INTEGER NOT NULL CHECK (adoption_version > 0),
 connection_id TEXT NOT NULL,
 connection_version INTEGER NOT NULL CHECK (connection_version > 0),
 account_scope TEXT NOT NULL CHECK (account_scope IN ('personal','team')),
 account_id TEXT NOT NULL,
 team_id TEXT NOT NULL DEFAULT '',
 project_id TEXT NOT NULL,
 environment TEXT NOT NULL CHECK (environment='production'),
 actor TEXT NOT NULL,
 receipt JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL,
 CHECK (expires_at=created_at+INTERVAL '10 minutes'),
 CHECK ((account_scope='personal' AND team_id='') OR (account_scope='team' AND team_id=account_id AND team_id<>''))
);
CREATE INDEX vercel_landing_update_reviews_history ON vercel_landing_update_reviews(created_at DESC,id);
CREATE INDEX vercel_landing_update_reviews_adoption ON vercel_landing_update_reviews(adoption_id);
