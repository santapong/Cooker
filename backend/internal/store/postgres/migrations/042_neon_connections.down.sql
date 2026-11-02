DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM neon_connections) THEN
        RAISE EXCEPTION 'cannot remove Neon credential recovery support while connections remain';
    END IF;
END;
$$;
DROP TRIGGER vercel_credential_rewrap_guard ON vercel_connections;
DROP FUNCTION cooker_vercel_credential_rewrap_guard();
DROP TABLE neon_connections;
