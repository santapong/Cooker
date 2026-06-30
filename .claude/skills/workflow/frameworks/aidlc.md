---
name: aidlc
description: Cooker's default AI-Assisted Development Life Cycle — the 10-phase loop from docs/engineering/ADLC.md, with Cooker's layering, security, store-parity, and docs-sync gates baked in.
default: true
---

# AIDLC (Cooker)

## When to use
The default for any non-trivial change in Cooker: a new feature, a bug with blast radius, an
adapter/stage type, anything touching auth/secrets/CORS/rate-limiter/NetworkPolicy/Dockerfile, or
a change that crosses the handler→service→store layers. Trivial, reversible edits can use
`lightweight`; a plain phase-gated pass without the Cooker-specific governance can use `sdlc`.

Canonical reference: `docs/engineering/ADLC.md` (the 10-phase loop) and
`docs/engineering/harness-engineering.md` (how each phase maps to agents/skills/workflows). This
framework is the runnable summary; those docs are the source of truth.

## Stages

**0. Intake & triage** — give the change a type (bug/feature/audit/CI/chore) and a branch name.
- Exit gate: type assigned; `claude/<topic>` or `<area>/<topic>` branch chosen.
- Human-gated: yes (don't start security hot-fixes T1/T2/T3/T5 or `roadmap`/`needs-design` items without asking the maintainer).
- Harness: inline; `cooker-find` to locate code.

**1. Plan & scope** — write the approach: critical files, sequencing, risks, verification.
- Exit gate: a written plan exists (use the `cooker-planner` agent for non-trivial work).
- Human-gated: no
- Harness: one Plan agent / `cooker-planner`.

**2. Design / ADR** — only for expensive-to-reverse choices.
- Exit gate: ADR accepted in `docs/adr/` and linked from `backlog.md`.
- Human-gated: yes (ADR acceptance).
- Harness: `cooker-planner` → `docs/adr/`.

**3. Implement** — build per layer; respect strict layering.
- Exit gate: one commit per layer on the branch; **no business logic in handlers, no HTTP types in services**; new request fields have a matching store migration; memory & Postgres stores stay in parity.
- Human-gated: no
- Harness: `cooker-feature-dev` + layer agents, or `cooker-new-feature`/`cooker-improve`/`cooker-fix-bug`; `pipeline` for multi-file.

**4. Verify** — prove it works and nothing broke.
- Exit gate: `make test` (race on) green locally; manual E2E if behaviour changed.
- Human-gated: no
- Harness: `verify`/`run` skills, `check-pkg.sh`.

**5. Review** — correctness + complexity + security.
- Exit gate: `/code-review` findings triaged; **security sign-off if the diff touches auth/secrets/CORS/rate-limiter/NetworkPolicy/Dockerfile**.
- Human-gated: yes (security sign-off when applicable).
- Harness: `code-review` skill, `cooker-security` agent, `cooker-review` workflow.

**6. Land** — merge with docs and backlog updated together.
- Exit gate: draft PR → CI green → squash-merge; **`backlog.md` item moved to Closed and docs updated in the same PR**.
- Human-gated: yes (merge).
- Harness: driver + GitHub MCP.

**7–9. Release / Deploy / Operate** — tag + signed artifact; roll Dev→Staging→Prod behind `Config.Validate`; wire alerts/runbook.
- Exit gate: per `RELEASING.md` / Helm boot guards / `RUNBOOK.md`.
- Human-gated: yes (release + prod rollout).
- Harness: inline + deploy artifacts.

**10. Improve (feedback loop)** — file findings back into intake.
- Exit gate: new bugs/findings filed; audit docs reflect reality.
- Human-gated: no
- Harness: `cooker-audit`/`cooker-weekly` skills, `cooker-audit-sweep` workflow.

## Hard gates
- **Layering:** handlers parse/respond only; services hold business logic and take no HTTP types; adapters implement narrow interfaces.
- **Store parity:** memory and Postgres stores never diverge; new request fields require a migration in `internal/store/postgres/migrations/`.
- **Security pass** is mandatory when the diff touches auth/secrets/CORS/rate-limiter/NetworkPolicy/Dockerfile, and `SECURITY.md` is updated in the same PR.
- **Docs-sync in the same PR** (UAT changes → `docs/guides/UAT.md`; backlog item → Closed log).
- The CLAUDE.md "What NOT to do without asking" list is binding (no `Allow-Credentials: true`, no docker.sock bind-mount, no `go mod tidy` in backend, no Go-version bump, etc.).
- Feature branch only; never push to `main`. If a gate cannot be met, stop and report — do not route around it.

## Tracker sync (optional)
Off by default; `backlog.md` + GitHub Issues are the source of truth. To opt in: open/locate a
GitHub issue at **Intake**, link it on the PR at **Land**, and (if ClickUp is used) mirror the
phase as the ClickUp task status via `clickup_update_task`.
