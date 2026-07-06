-- 024_licenses.up.sql
--
-- Installed self-hosted license (M2 self-hosted licensing — see
-- docs/launch/01-billing-monetization.md §4). Cooker's paid tiers are a
-- feature-flag-and-replica license, not a usage meter: a signed license
-- string (raw_token) encodes the plan, seat/replica allowance, the set
-- of feature flags it unlocks, and the licensee. An operator installs
-- exactly one license per instance.
--
-- Single-row semantics: there is at most one installed license. The
-- store keys every write on a fixed sentinel id ('active') and the
-- one_row CHECK below makes a second id impossible at the schema level,
-- so Set is an upsert on that single row and GetActive is a point read.
--
-- Signature verification of raw_token is the licensing service's job
-- (M2-T2); this table stores only the decoded claims plus the signed
-- artefact so the admin UI can render the current entitlement without
-- re-verifying on every read. features is a TEXT[] of flag names.
-- expires_at is nullable: NULL = perpetual (e.g. the free/explorer tier
-- never expires).

CREATE TABLE IF NOT EXISTS licenses (
    id                 TEXT PRIMARY KEY,
    raw_token          TEXT NOT NULL,
    plan               TEXT NOT NULL,
    seats              INTEGER NOT NULL DEFAULT 0,
    features           TEXT[] NOT NULL DEFAULT '{}',
    customer           TEXT NOT NULL DEFAULT '',
    issued_at          TIMESTAMPTZ NOT NULL,
    expires_at         TIMESTAMPTZ,
    installed_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    installed_by_sub   TEXT NOT NULL DEFAULT '',
    installed_by_email TEXT NOT NULL DEFAULT '',
    -- Enforce the single-active-license invariant in the schema: the only
    -- permitted id is the store's sentinel, so there can never be two rows.
    CONSTRAINT licenses_one_row CHECK (id = 'active')
);
