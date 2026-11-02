-- Preserve admitted source and verified checkout evidence during rollback.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pipeline_runs WHERE source IS NOT NULL)
 THEN RAISE EXCEPTION 'run source receipts require a data-preserving export before rollback'; END IF;
END $$;
ALTER TABLE pipeline_runs DROP COLUMN source;
