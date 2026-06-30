---
name: lightweight
description: Fast path for small, low-risk, reversible changes — skip the heavy gates, keep the safety floor. Typo fixes, doc edits, one-file changes, trivial refactors.
default: false
---

# Lightweight

## When to use
The change is small, low-risk, and easy to undo: a typo, a doc edit, a one-file tweak, a trivial
refactor with no behaviour change. If you find yourself touching crypto/auth/promotion, a schema,
a public API, or more than a couple of files — stop and switch to `aidlc`. When in doubt, it's not
lightweight.

## Stages

**1. Confirm it's small** — sanity-check the change really is low-risk and reversible.
- Exit gate: it touches a small, well-understood surface and is not on the hard-gate list below.
- Human-gated: no
- Harness: inline.

**2. Do it** — make the minimal change that works (pairs well with the `ponytail` skill).
- Exit gate: the edit is the smallest thing that solves the task; no scope creep.
- Human-gated: no
- Harness: inline (solo tools).

**3. Check** — lint/build/relevant test green; eyeball the diff.
- Exit gate: repo checks pass for the touched area; non-trivial logic has one runnable check.
- Human-gated: no
- Harness: inline.

**4. Land** — feature branch → draft PR → merge.
- Exit gate: CI green; PR merged.
- Human-gated: yes (merge).
- Harness: driver inline + GitHub MCP.

## Hard gates
This framework is ONLY valid while all of these hold — if any is false, switch to `aidlc`:
- Does NOT touch crypto, auth, secrets, promotion, or the security-sensitive paths the repo's
  `aidlc` framework lists.
- Does NOT change a database schema, a public API contract, or a wire format.
- Is reversible and small (rule of thumb: a couple of files, no new dependency).
- Feature branch only; CI must be green before merge.
- If the change turns out bigger or riskier than it looked, stop and re-run under `aidlc`.

## Tracker sync (optional)
Off by default. Usually skipped for lightweight work. If you want a trail, just link the PR to an
existing tracker item rather than opening a new task.
