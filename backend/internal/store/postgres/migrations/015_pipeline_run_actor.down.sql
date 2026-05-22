ALTER TABLE pipeline_runs
    DROP COLUMN IF EXISTS started_by_token_hash,
    DROP COLUMN IF EXISTS started_by_groups,
    DROP COLUMN IF EXISTS started_by_email,
    DROP COLUMN IF EXISTS started_by_user_sub;
