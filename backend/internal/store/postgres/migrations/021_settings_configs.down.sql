-- 021_settings_configs.down.sql
--
-- Reverse 021: drop the Settings registry + cluster config tables.
-- No FKs between them, so order is irrelevant.

DROP TABLE IF EXISTS cluster_configs;
DROP TABLE IF EXISTS registry_configs;
