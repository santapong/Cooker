DROP TABLE IF EXISTS notification_deliveries;
DROP TABLE IF EXISTS notification_job_receipts;
DROP TABLE IF EXISTS notification_source_repairs;
DROP TABLE IF EXISTS notification_events;
DROP TABLE IF EXISTS notification_policy;
DROP TRIGGER IF EXISTS notification_target_version ON notification_targets;
DROP FUNCTION IF EXISTS cooker_notification_target_version();
ALTER TABLE notification_targets DROP COLUMN IF EXISTS version;
DROP SEQUENCE IF EXISTS notification_target_versions;
