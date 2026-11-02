-- Refuse downgrade while unresolved external work exists. Operators must
-- reconcile it first; silently unlocking these keys would permit duplicates.
ALTER TABLE jobs DROP CONSTRAINT jobs_status_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_status_check CHECK (
    status IN ('pending','running','succeeded','failed','cancelled')
);
DROP INDEX jobs_attention_concurrency_idx;
