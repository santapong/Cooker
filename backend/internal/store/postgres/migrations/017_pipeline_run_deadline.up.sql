-- 017_pipeline_run_deadline.up.sql
--
-- Per-pipeline run deadline (W11 §ML step 5) + the pipeline-version
-- stamp on runs that the run-diff endpoint (M3) reads.
--
-- Numbered 017: 016 is reserved by the in-flight run-list-indexes
-- migration on the perf branch (PR #102); the runner tolerates gaps.
--
-- Both columns are additive with DEFAULTs, so replicas on the old
-- code keep INSERTing cleanly during a rolling deploy, and the down
-- migration is a plain column drop.

-- Go duration string ("45m", "2h"); '' means "no override — use
-- COOKER_RUN_DEADLINE". Validated at the API layer ([10s, 24h]).
ALTER TABLE pipelines
    ADD COLUMN IF NOT EXISTS run_deadline TEXT NOT NULL DEFAULT '';

-- Pipeline.Version at the moment the run was created. 0 = unknown
-- (rows that predate this migration). Lets run-diff report whether
-- the definition changed between two runs without snapshotting the
-- whole pipeline JSON.
ALTER TABLE pipeline_runs
    ADD COLUMN IF NOT EXISTS pipeline_version INT NOT NULL DEFAULT 0;
