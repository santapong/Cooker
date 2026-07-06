-- 025_canary.down.sql
--
-- Reverse 025: drop the canary state table (its indexes go with it)
-- and remove the apps.canary_config column.

DROP TABLE IF EXISTS app_canaries;

ALTER TABLE apps
    DROP COLUMN IF EXISTS canary_config;
