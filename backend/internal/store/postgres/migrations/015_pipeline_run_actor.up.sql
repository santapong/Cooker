-- 015_pipeline_run_actor: capture the actor that started a pipeline run so
-- the deploy-stage executor can call the governance gate on their behalf
-- (Grovernance Milestone C — delegated actor). The token is hashed; the raw
-- token is never persisted.
--
-- Defaults are empty strings / empty arrays so the migration is backward-
-- compatible with rows started before this column existed. The executor hook
-- treats an empty StartedByUserSub as "skip governance for this run" — the
-- pre-Phase-3 path remains intact.
ALTER TABLE pipeline_runs
    ADD COLUMN started_by_user_sub    TEXT      NOT NULL DEFAULT '',
    ADD COLUMN started_by_email       TEXT      NOT NULL DEFAULT '',
    ADD COLUMN started_by_groups      JSONB     NOT NULL DEFAULT '[]',
    ADD COLUMN started_by_token_hash  TEXT      NOT NULL DEFAULT '';
