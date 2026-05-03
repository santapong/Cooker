-- Local-auth users. Active when COOKER_LOCAL_AUTH_ENABLED=true. The
-- OIDC path stores no rows here — its identity lives at the IdP. The
-- two paths coexist in the auth middleware via a token-prefix probe.
--
-- password_hash holds a bcrypt hash (DefaultCost or higher). Empty
-- means "no local password" (slot reserved but never used).
-- role is a single string mirroring auth.Role. Cooker's RBAC reads
-- this directly when the user signs in via the local path.
CREATE TABLE IF NOT EXISTS users (
    id            TEXT PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL DEFAULT '',
    password_hash TEXT NOT NULL DEFAULT '',
    role          TEXT NOT NULL DEFAULT 'viewer',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
