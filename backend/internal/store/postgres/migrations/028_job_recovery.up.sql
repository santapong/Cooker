ALTER TABLE jobs DROP CONSTRAINT jobs_status_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_status_check CHECK (
    status IN ('pending','running','succeeded','failed','cancelled','needs_attention')
);
CREATE INDEX jobs_attention_concurrency_idx ON jobs (concurrency_key)
    WHERE status IN ('running','needs_attention') AND concurrency_key <> '';
