-- 026_canary_pending.up.sql
--
-- PM26-07-06 (canary Start TOCTOU): Start now reserves the
-- one-canary-per-app slot with a 'pending' row BEFORE it builds the
-- image and establishes the weighted split, so the unique index — not
-- a check-then-act in application code — serializes concurrent Starts.
--
-- Widen the partial unique index to cover the pending state so a
-- second Start (pending or progressing) for the same app is rejected
-- at insert time (mapped to 409). Postgres can't ALTER a partial
-- index predicate in place, so drop and recreate.

DROP INDEX IF EXISTS uq_app_canaries_active;

CREATE UNIQUE INDEX IF NOT EXISTS uq_app_canaries_active
    ON app_canaries (app_id)
    WHERE status IN ('pending', 'progressing');
