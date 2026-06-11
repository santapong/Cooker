# ADR-0005 — Persisted promotions and approvals as relational rows
Date: 2026-06-11
Status: accepted

## Context

The promotion/approval surface (`POST /pipelines/:id/runs/:runId/promote`,
`/approve`, and `GET …/env-status`) was theatre: the handlers returned
`{"message": "promotion initiated"}` and `{"statuses": []}` without writing
anything (audit findings HS26-05-01, HS26-05-08). `PromotionPolicy.RequiredApprovers`
existed on the model but was never enforced, and the frontend
`environmentStore.approve` posted `{approvedBy}` while the handler read `{note}`
(HS26-05-14).

A real flow has to answer three questions on any later read or restart:

1. Which environments has this run been promoted to, and what is each one's state?
2. Who approved a manual gate, when, and with what note?
3. Has a manual gate collected enough *distinct* approvers to complete
   (`>= RequiredApprovers`)?

`EnvironmentStatus` already lived inline on `PipelineRun` (a JSONB blob on the
`pipeline_runs.env_statuses` column), but that representation cannot express an
approval *set* — it has a single `approvedBy string`, so it can neither record
multiple approvers nor enforce a distinct-count threshold, and it offers no
queryable audit trail.

## Decision

Persist promotions and approvals as **two relational tables**, not as a wider
JSONB blob on the run:

- `run_promotions` — one row per `(run_id, environment_id)` target. Carries the
  promotion `status` (`pending` / `awaiting_approval` / `approved` / `deploying`
  / `deployed` / `failed`), the `strategy` and `required_approvers` **snapshotted**
  from the target environment's `PromotionPolicy` at promote time, the actor who
  initiated it, `promoted_at`, and timestamps. A `UNIQUE (run_id, environment_id)`
  constraint makes promote idempotent per target.
- `promotion_approvals` — append-only, one row per approval, FK to
  `run_promotions(id) ON DELETE CASCADE`. Records `approver_sub`, `approver_email`,
  `note`, and `created_at`. A `UNIQUE (promotion_id, approver_sub)` constraint
  enforces one approval per identity, so `COUNT(*)` *is* the distinct-approver
  count.

Enforcement lives in the `service.Promoter`:

- `strategy = "auto"` short-circuits: the promotion is created already terminal
  (`deployed`) with no approval required.
- `strategy = "manual"` creates the promotion `awaiting_approval`. Each approve
  inserts an approval row (re-approval by the same identity is a no-op, not an
  error). When the approval count reaches `required_approvers` the promotion
  advances to `approved`. `required_approvers <= 0` on a manual gate is treated
  as "one approval needed" so a misconfigured policy can't auto-complete with zero.

`GetEnvStatus` derives `[]model.EnvironmentStatus` from `run_promotions` joined to
its approval count, so the frontend promotion lane (already built, just starved of
data) renders real per-environment state.

### Why relational rows over a JSONB array on the promotion

This deliberately departs from ADR-0003's "JSONB for shapes that grow" default,
and the boundary is the same one ADR-0003 draws:

| | ADR-0003 JSONB case | This case |
|---|---|---|
| Shape churn | High (editor invents stage configs) | Fixed (approver, time, note) |
| Read pattern | By ID, whole document | Aggregated (`COUNT(DISTINCT)`), filtered by run/env |
| Integrity need | App-level only | DB-level: one-per-approver, distinct count |

Approvals are a set you aggregate and constrain, not an opaque document you
round-trip — so they earn real rows and real constraints. A JSONB array would
push the distinct-count and dedupe logic into Go and lose the `UNIQUE`
guarantee.

Alternatives considered:

| Option | Rejected because |
|---|---|
| Widen `EnvironmentStatus` on the run's `env_statuses` JSONB | Can't model an approver *set*; no DB-level distinct/dedupe; concurrent approves race on a read-modify-write of the whole run row. |
| JSONB `approvals[]` column on `run_promotions` | Loses `UNIQUE (promotion_id, approver_sub)`; distinct-count logic moves into Go. |
| Reuse `audit_events` as the approval log | Audit rows are drop-on-full and have no FK/uniqueness; they're a forensic trail, not a source of truth for gate completion. |

## Consequences

+ Promotions and approvals survive restarts; `env-status` reflects truth, not `[]`.
+ `RequiredApprovers` is enforced at the service layer with a DB-level distinct
  guarantee; two clicks by the same approver count once.
+ The approval rows are a natural, queryable audit trail (who/when/note).
+ Memory and Postgres stores stay at parity behind the new `PromotionStore`
  interface; handler stays thin and routes through `service.Promoter`.

− Two new tables and a migration (`020`). The `run_promotions` row snapshots the
  policy, so editing an environment's `RequiredApprovers` after a promotion is
  created does not retroactively change that in-flight gate — intentional, but a
  behaviour to document.
− `EnvironmentStatus` now has two sources of truth during the transition: the
  legacy inline `run.EnvironmentStatuses` (still written by the executor's
  in-memory `Promoter` chain helper) and the new store. `GetEnvStatus` reads the
  store; the inline field is left for the executor's existing auto-promote path
  and is not surfaced by the endpoint.
