---
name: Cooker-AIDLC
summary: Cooker's 10-phase AI-assisted development lifecycle (Intake → Plan → Design → Implement → Verify → Review → Land → Release/Deploy/Operate → Improve) with the repo's layering, store-parity, security, and docs-sync gates baked in.
when-to-use: Any non-trivial change in the Cooker repo — a new feature, a bug with blast radius, an adapter/stage type, anything touching auth/secrets/CORS/rate-limiter/NetworkPolicy/Dockerfile, or a change crossing the handler→service→store layers. Prefer it over the generic AIDLC whenever the task lives in this repo; use plain AIDLC only for work outside Cooker's governance.
---

# Cooker-AIDLC — Cooker's AI-Assisted Development Life Cycle

The 10-phase loop from `docs/engineering/ADLC.md`, made runnable as a loop-engine framework.
Phases 0, 2, 5, 6, and 7–9 are human-gated; trivial reversible edits may skip 2 and 7–9.
Canonical references: `docs/engineering/ADLC.md` (the lifecycle) and
`docs/engineering/harness-engineering.md` (how phases map to agents/skills/workflows) — this file
is the runnable summary; those docs are the source of truth.

## Phase: Intake & triage

- **Purpose**: Classify the change (bug / feature / audit / CI / chore) and name the branch.
- **Entry criteria**: A user request or scheduled trigger exists.
- **Agent activities**: Inline in the main loop; `/cooker-find` to locate the code the request touches.
- **Orchestration hint**: No fan-out — a single classification decision (loop-policy: don't spawn agents for work the main loop can settle in one step).
- **Exit gate**: (human) Type assigned and a `claude/<topic>` or `<area>/<topic>` branch chosen. Do not start security hot-fixes (themes T1/T2/T3/T5) or `roadmap`/`needs-design` items without asking the maintainer.

## Phase: Plan & scope

- **Purpose**: A written approach: critical files, sequencing, risks, verification plan.
- **Entry criteria**: Intake gate passed.
- **Agent activities**: One planning agent (the `cooker-planner` subagent for non-trivial work) reads CLAUDE.md, `backlog.md`, and the relevant `docs/audits/` entries, and returns the plan.
- **Orchestration hint**: Single agent — planning needs one coherent context, not a fan-out (harness policy: fan out for coverage, not for judgment).
- **Exit gate**: A written plan exists.

## Phase: Design / ADR

- **Purpose**: Record expensive-to-reverse choices as an ADR. Skip for reversible changes.
- **Entry criteria**: The plan identifies an expensive-to-reverse decision.
- **Agent activities**: `cooker-planner` drafts the ADR into `docs/adr/`, linked from `backlog.md`.
- **Orchestration hint**: Single agent; optionally a small judge panel over competing designs when the solution space is genuinely wide.
- **Exit gate**: (human) ADR accepted.

## Phase: Implement

- **Purpose**: Build the change layer by layer, respecting strict layering.
- **Entry criteria**: Plan (and ADR, if any) approved.
- **Agent activities**: `cooker-feature-dev` coordinating layer agents (`cooker-backend-api`, `cooker-backend-data`, `cooker-backend-adapters`, `cooker-frontend-*`, `cooker-infra-*`), or the `/cooker-new-feature`, `/cooker-improve`, `/cooker-fix-bug` skills for single-lane work.
- **Orchestration hint**: pipeline per file/layer for multi-file work (harness policy H1: pipeline is the default; barriers only when a stage needs all prior results).
- **Exit gate**: One commit per layer on the branch. No business logic in handlers, no HTTP types in services; new request fields have a matching store migration; memory & Postgres stores stay in parity.

## Phase: Verify

- **Purpose**: Prove it works and nothing broke.
- **Entry criteria**: Implementation committed on the branch.
- **Agent activities**: Run `make test` (race detector on) and the scoped gate `.claude/skills/cooker-improve/check-pkg.sh <pkg>`; drive the affected flow end-to-end (`/verify`, `/run`) when behaviour changed.
- **Orchestration hint**: Inline or a single verify agent; parallelize only independent test scopes.
- **Exit gate**: Tests green locally; manual E2E done if behaviour changed.

## Phase: Review

- **Purpose**: Correctness, complexity, and security review of the diff.
- **Entry criteria**: Verify gate passed.
- **Agent activities**: `/code-review` (or `/loop-review` for a security-focused pass); the `cooker-security` agent when the diff touches auth/secrets/CORS/rate-limiter/NetworkPolicy/Dockerfile; the saved `cooker-review` workflow for a diff-wide multi-dimension pass with adversarial verification.
- **Orchestration hint**: parallel finders per dimension, then per-finding adversarial verify (loop-policy: refute-first verification kills plausible-but-wrong findings).
- **Exit gate**: (human) Findings triaged; security sign-off when the diff touches the security surface, with `SECURITY.md` updated in the same PR.

## Phase: Land

- **Purpose**: Merge with docs and backlog updated together.
- **Entry criteria**: Review gate passed.
- **Agent activities**: Main loop drives: draft PR → CI green → squash-merge. `backlog.md` item moved to the Closed log and affected docs updated in the same PR.
- **Orchestration hint**: No fan-out — a sequenced checklist.
- **Exit gate**: (human) Merge approved and completed.

## Phase: Release, Deploy & Operate

- **Purpose**: Tag + signed artifact; roll Dev → Staging → Prod behind `Config.Validate`; wire alerts/runbook.
- **Entry criteria**: Change merged to `main`.
- **Agent activities**: Follow `RELEASING.md`, Helm boot guards, and `RUNBOOK.md`; deploy artifacts live under `deploy/`.
- **Orchestration hint**: No fan-out — human-paced promotion.
- **Exit gate**: (human) Release published and production rollout approved.

## Phase: Improve (feedback loop)

- **Purpose**: File findings back into intake so the audit corpus reflects reality.
- **Entry criteria**: Any time; scheduled weekly via `/cooker-weekly` (Monday cron in `.github/workflows/cooker-weekly.yml`).
- **Agent activities**: `/cooker-audit` for open-ended hunts; the saved `cooker-audit-sweep` / `cooker-health-sweep` workflows for repo-wide sweeps; new bugs and chains logged into `docs/audits/`.
- **Orchestration hint**: loop-until-dry for unknown-size discovery (loop-policy: simple counters miss the tail); multi-modal finder sweep with per-finding verify.
- **Exit gate**: New findings filed; `docs/audits/` chain rows flipped with commit SHAs.

## Hard gates (binding in every phase)

- **Layering:** handlers parse/respond only; services hold business logic and take no HTTP types; adapters implement narrow interfaces.
- **Store parity:** memory and Postgres stores never diverge; new request fields require a migration in `internal/store/postgres/migrations/`.
- **Security pass** is mandatory when the diff touches auth/secrets/CORS/rate-limiter/NetworkPolicy/Dockerfile, and `SECURITY.md` is updated in the same PR.
- **Docs-sync in the same PR** (UAT changes → `docs/guides/UAT.md`; backlog item → Closed log; harness changes → `docs/engineering/harness-engineering.md`).
- The CLAUDE.md "What NOT to do without asking" list is binding (no `Allow-Credentials: true`, no docker.sock bind-mount, no `go mod tidy` in backend, no Go-version bump, etc.).
- Feature branch only; never push to `main`. If a gate cannot be met, stop and report — do not route around it.

## Tracker sync (optional)

Off by default; `backlog.md` + GitHub Issues are the source of truth. To opt in: open/locate a
GitHub issue at **Intake**, link it on the PR at **Land**, and (if ClickUp is used) mirror the
phase as the ClickUp task status via `clickup_update_task`.
