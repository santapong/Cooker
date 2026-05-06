ALTER TABLE pipelines    DROP COLUMN IF EXISTS version;
ALTER TABLE environments DROP COLUMN IF EXISTS version;
ALTER TABLE apps         DROP COLUMN IF EXISTS version;
ALTER TABLE hosts        DROP COLUMN IF EXISTS version;
