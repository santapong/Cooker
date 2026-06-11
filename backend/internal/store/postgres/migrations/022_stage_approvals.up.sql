-- 022_stage_approvals.up.sql
--
-- Real approval-gate persistence for StageTypeApproval stages (audit
-- finding HS26-05-03). Before this, executeApproval was a fail-loud stub:
-- an approval stage in a pipeline did not actually pause the run. This
-- adds a persisted gate so the run blocks until enough distinct approvers
-- approve (or one rejects), surviving a server restart.
--
-- Shape mirrors migration 020 (run_promotions / promotion_approvals):
--
-- stage_approvals: one row per (run, stage). required_approvers is
-- SNAPSHOTTED from the stage config at gate-creation time so editing the
-- pipeline later does not change an in-flight gate. UNIQUE (run_id,
-- stage_id) makes gate creation idempotent (a re-entered stage finds the
-- existing gate).
--
-- stage_approval_votes: append-only, one row per approval. UNIQUE
-- (stage_approval_id, approver_sub) enforces one approval per identity, so
-- COUNT(*) is the distinct-approver count that drives gate completion. A
-- rejection settles the gate directly (status='rejected') and is not
-- recorded as a vote.

CREATE TABLE IF NOT EXISTS stage_approvals (
    id                 TEXT PRIMARY KEY,
    run_id             TEXT NOT NULL,
    stage_id           TEXT NOT NULL,
    status             TEXT NOT NULL,
    required_approvers INT  NOT NULL DEFAULT 1,
    resolved_by        TEXT NOT NULL DEFAULT '',
    resolved_at        TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (run_id, stage_id)
);

-- Gate reads are always "the gate for one (run, stage)" or "all gates for
-- one run"; index the run for the latter.
CREATE INDEX IF NOT EXISTS idx_stage_approvals_run
    ON stage_approvals (run_id);

CREATE TABLE IF NOT EXISTS stage_approval_votes (
    id                TEXT PRIMARY KEY,
    stage_approval_id TEXT NOT NULL REFERENCES stage_approvals(id) ON DELETE CASCADE,
    approver_sub      TEXT NOT NULL DEFAULT '',
    approver_email    TEXT NOT NULL DEFAULT '',
    note              TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- One approval per identity per gate: COUNT(*) == distinct approver
    -- count, and a double-click can't inflate the tally.
    UNIQUE (stage_approval_id, approver_sub)
);

CREATE INDEX IF NOT EXISTS idx_stage_approval_votes_gate
    ON stage_approval_votes (stage_approval_id);
