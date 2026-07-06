-- 026_canary_pending.down.sql
--
-- Reverse 026: restore the progressing-only partial unique index.
-- Safe only when no 'pending' rows exist (a pending row would violate
-- nothing under the narrower predicate, but the app code that writes
-- 'pending' must be rolled back in lockstep).

DROP INDEX IF EXISTS uq_app_canaries_active;

CREATE UNIQUE INDEX IF NOT EXISTS uq_app_canaries_active
    ON app_canaries (app_id)
    WHERE status = 'progressing';
