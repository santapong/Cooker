---
name: sdlc
description: Classic phase-gated SDLC (requirements → design → build → test → deploy → maintain). Reach for it when you want disciplined phase gates without the AI-governance specifics of aidlc.
default: false
---

# SDLC

## When to use
A medium-to-large change where you want the familiar waterfall-ish gates — clear requirements,
a design signed off before building, test before deploy — but you don't need the full
AI-governance ceremony (decision-class, threat-model delta, adversarial verify) that `aidlc`
adds. Not for one-line fixes (use `lightweight`) and not for security-sensitive crypto/auth work
(use `aidlc`, which has the gates that protect those).

## Stages

**1. Requirements** — state what must be true when done, and what is explicitly out of scope.
- Exit gate: a written, testable acceptance criterion exists.
- Human-gated: no
- Harness: inline (driver writes it down).

**2. Design** — decide the approach; name the files/components touched and the trade-off taken.
- Exit gate: approach written; an ADR exists if the choice is expensive to reverse.
- Human-gated: yes (sign off the approach before building anything non-trivial).
- Harness: one Plan agent.

**3. Build** — implement to the design and the repo's conventions, one logical unit at a time.
- Exit gate: code compiles/lints clean on a feature branch; no scope creep beyond the design.
- Human-gated: no
- Harness: pipeline over the work-list (one implementer per independent unit).

**4. Test** — prove it does what the requirements said and doesn't break neighbours.
- Exit gate: the repo's test command is green locally; new logic has a test.
- Human-gated: no
- Harness: inline or one verifier agent per area.

**5. Review** — a second pass for correctness and fit before it lands.
- Exit gate: `/code-review` findings triaged; required reviewers approve.
- Human-gated: yes (reviewer approval).
- Harness: `/code-review` phase (+ `/security-review` if the diff is sensitive).

**6. Deploy** — land and roll out behind the repo's release/boot guards.
- Exit gate: draft PR → CI green → merged; rollout steps followed.
- Human-gated: yes (merge authority).
- Harness: driver inline + GitHub MCP.

**7. Maintain** — watch it in the wild; file regressions back as new intake.
- Exit gate: docs updated; any follow-up filed in the repo tracker.
- Human-gated: no
- Harness: inline.

## Hard gates
- Design signed off before build for anything non-trivial — no building ahead of an undecided approach.
- Tests green before deploy — never merge red.
- Feature branch only; never edit the default branch directly.
- Repo conventions (layering, error handling, docs-in-same-PR) still apply — SDLC relaxes the
  AI-governance gates, not the codebase invariants.
- If a gate cannot be met, stop and report — do not route around it.

## Tracker sync (optional)
Off by default. To opt in: create the tracker task at **Requirements**, move it to In-Progress at
**Build**, to In-Review at **Review**, and Done at **Deploy** (ClickUp `clickup_update_task` or a
GitHub issue linked to the PR).
