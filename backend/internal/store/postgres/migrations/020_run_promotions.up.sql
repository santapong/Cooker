-- 020_run_promotions.up.sql
--
-- Real promotion + approval persistence (audit findings HS26-05-01,
-- HS26-05-08, HS26-05-14). Before this, PromoteRun / ApprovePromotion /
-- GetEnvStatus were stubs that wrote nothing and always returned an
-- empty status list — the whole promotion/approval surface was theatre.
-- See docs/adr/0005-promotion-approval-persistence.md for the choice of
-- relational rows over a JSONB array on the run.
--
-- run_promotions: one row per (run, target environment). strategy and
-- required_approvers are SNAPSHOTTED from the target environment's
-- PromotionPolicy at promote time so editing the policy later does not
-- retroactively change an in-flight gate. The UNIQUE (run_id,
-- environment_id) makes promote idempotent per target.
--
-- promotion_approvals: append-only, one row per approval. UNIQUE
-- (promotion_id, approver_sub) enforces one approval per identity, so
-- COUNT(*) is the distinct-approver count that drives gate completion.

CREATE TABLE IF NOT EXISTS run_promotions (
    id                 TEXT PRIMARY KEY,
    run_id             TEXT NOT NULL,
    pipeline_id        TEXT NOT NULL DEFAULT '',
    environment_id     TEXT NOT NULL,
    status             TEXT NOT NULL,
    strategy           TEXT NOT NULL DEFAULT 'manual',
    required_approvers INT  NOT NULL DEFAULT 0,
    requested_by       TEXT NOT NULL DEFAULT '',
    promoted_at        TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (run_id, environment_id)
);

-- env-status reads are always "all promotions for one run".
CREATE INDEX IF NOT EXISTS idx_run_promotions_run
    ON run_promotions (run_id);

CREATE TABLE IF NOT EXISTS promotion_approvals (
    id             TEXT PRIMARY KEY,
    promotion_id   TEXT NOT NULL REFERENCES run_promotions(id) ON DELETE CASCADE,
    approver_sub   TEXT NOT NULL DEFAULT '',
    approver_email TEXT NOT NULL DEFAULT '',
    note           TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- One approval per identity per promotion: COUNT(*) == distinct
    -- approver count, and a double-click can't inflate the tally.
    UNIQUE (promotion_id, approver_sub)
);

-- Approval reads are always "all approvals for one promotion".
CREATE INDEX IF NOT EXISTS idx_promotion_approvals_promotion
    ON promotion_approvals (promotion_id);
