-- 022_stage_approvals.down.sql
--
-- Reverse 022: drop the vote and gate tables. Drop stage_approval_votes
-- first (it FKs stage_approvals); indexes go with their tables.

DROP TABLE IF EXISTS stage_approval_votes;
DROP TABLE IF EXISTS stage_approvals;
