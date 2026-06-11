-- 023_api_tokens.up.sql
--
-- Personal access tokens / service-account tokens (product-plan Tier 1).
-- Scripts and external CI authenticate with a long-lived bearer token
-- (`ck_<base64url>`) instead of the browser OIDC flow. The auth
-- middleware routes a credential to this table by its `ck_` prefix,
-- SHA-256-hashes it, and looks it up via the hash index below.
--
-- Only the SHA-256 hash is stored — the plaintext is shown exactly once
-- at creation and is unrecoverable afterwards (revocation = delete the
-- row). display_prefix keeps the first 12 plaintext chars so a token is
-- identifiable in lists without revealing the secret. role is the single
-- RBAC role the token authenticates as (capped at creation to <= the
-- creator's own role). expires_at and last_used_at are nullable: a NULL
-- expiry never expires (operators are advised to set short expiries for
-- CI), a NULL last_used_at means "never used".

CREATE TABLE IF NOT EXISTS api_tokens (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL,
    display_prefix   TEXT NOT NULL,
    hash             TEXT NOT NULL,
    role             TEXT NOT NULL,
    created_by_sub   TEXT NOT NULL DEFAULT '',
    created_by_email TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at       TIMESTAMPTZ,
    last_used_at     TIMESTAMPTZ
);

-- The hot-path lookup is "find the token for this SHA-256 hash" on every
-- ck_-prefixed request. UNIQUE both enforces no two tokens share a hash
-- and serves as the lookup index.
CREATE UNIQUE INDEX IF NOT EXISTS idx_api_tokens_hash
    ON api_tokens (hash);

-- "List my tokens" filters by creator; index it for the common path.
CREATE INDEX IF NOT EXISTS idx_api_tokens_created_by_sub
    ON api_tokens (created_by_sub);
