-- 019_audit_events.down.sql
--
-- Reverse 019: drop the audit trail table (indexes go with it).

DROP TABLE IF EXISTS audit_events;
