CREATE TABLE neon_connections (
    id TEXT PRIMARY KEY,
    configuration JSONB NOT NULL,
    credential BYTEA NOT NULL CHECK (octet_length(credential) BETWEEN 1 AND 16384),
    version BIGINT NOT NULL CHECK (version > 0),
    snapshot JSONB NOT NULL DEFAULT 'null'::jsonb,
    last_attempt_at TIMESTAMPTZ,
    last_success_at TIMESTAMPTZ,
    error_code TEXT NOT NULL DEFAULT '',
    sync_lease TEXT,
    sync_lease_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CHECK ((sync_lease IS NULL) = (sync_lease_until IS NULL))
);

-- Rewrap changes ciphertext while preserving logical versions. Older five-
-- family tools refuse this second trigger during their existing schema check;
-- current tooling must explicitly qualify the sixth family before rewrapping.
CREATE FUNCTION cooker_vercel_credential_rewrap_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.credential IS DISTINCT FROM OLD.credential AND NEW.version = OLD.version
       AND current_setting('cooker.key_rotation_families', true) IS DISTINCT FROM '6' THEN
        RAISE EXCEPTION 'six-family offline credential rewrap required';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER vercel_credential_rewrap_guard
BEFORE UPDATE ON vercel_connections
FOR EACH ROW EXECUTE FUNCTION cooker_vercel_credential_rewrap_guard();
