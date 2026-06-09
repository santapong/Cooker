-- 017_pipeline_run_deadline.down.sql
--
-- Reverse 017: drop the two additive columns.

ALTER TABLE pipeline_runs DROP COLUMN IF EXISTS pipeline_version;
ALTER TABLE pipelines DROP COLUMN IF EXISTS run_deadline;
