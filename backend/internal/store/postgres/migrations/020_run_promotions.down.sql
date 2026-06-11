-- 020_run_promotions.down.sql
--
-- Reverse 020: drop the approval and promotion tables. Drop
-- promotion_approvals first (it FKs run_promotions); indexes go with
-- their tables.

DROP TABLE IF EXISTS promotion_approvals;
DROP TABLE IF EXISTS run_promotions;
