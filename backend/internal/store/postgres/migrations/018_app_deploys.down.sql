-- 018_app_deploys.down.sql
--
-- Reverse 018: drop the deploy-history table (index goes with it).

DROP TABLE IF EXISTS app_deploys;
