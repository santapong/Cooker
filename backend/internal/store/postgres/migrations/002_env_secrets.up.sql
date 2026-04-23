-- Environment secrets. Each entry in the JSONB is a base64-encoded
-- AES-GCM sealed value (nonce ++ ciphertext ++ tag). Plaintext is
-- never persisted; decryption happens only in the process memory of
-- a Cooker instance that holds the COOKER_SECRET_KEY.
ALTER TABLE environments
    ADD COLUMN IF NOT EXISTS secrets JSONB NOT NULL DEFAULT '{}';
