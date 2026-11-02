-- Local manual landing authority is separate from read-only provider snapshots.
-- No cascading foreign key: disconnect preserves historical evidence.
CREATE TABLE vercel_landing_adoptions (
 id TEXT PRIMARY KEY,
 connection_id TEXT NOT NULL,
 connection_version INTEGER NOT NULL CHECK (connection_version > 0),
 version INTEGER NOT NULL CHECK (version > 0),
 account_scope TEXT NOT NULL CHECK (account_scope IN ('personal','team')),
 account_id TEXT NOT NULL,
 team_id TEXT NOT NULL DEFAULT '',
 project_id TEXT NOT NULL,
 environment TEXT NOT NULL CHECK (environment = 'production'),
 enabled BOOLEAN NOT NULL,
 binding JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL,
 CHECK ((account_scope='personal' AND team_id='') OR (account_scope='team' AND team_id=account_id AND team_id<>''))
);
CREATE UNIQUE INDEX vercel_landing_adoptions_active_destination
 ON vercel_landing_adoptions(account_scope,account_id,team_id,project_id,environment)
 WHERE enabled;
CREATE INDEX vercel_landing_adoptions_connection ON vercel_landing_adoptions(connection_id);
