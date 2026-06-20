# Cooker Launch — Event Log (current)

> Append-only. Rotated by `.agent/rotate.sh` when this file exceeds its cap.
> Snapshot/board lives in `../state.md`. Archives indexed in `index.md`.

## [2026-06-20 ~05:50] M0 SETUP — orchestration stood up
- Merged PR #116 (squash) → `main` now has `docs/launch/`.
- Cut working branch `claude/launch-execution` off updated `main`.
- Created `.agent/` scaffolding: `state.md`, `protocol.md`, `log/current.md`, `rotate.sh`.
- Next: launch M0 team (3 Opus agents, disjoint file sets).

## [2026-06-20 ~06:10] M0 DELEGATE — plan-mode blocker resolved
- First 2 rounds of Opus agents returned PLANS only — subagents inherited active
  plan mode and (correctly) refused to override a system reminder on prompt say-so.
- Resolution: PM re-called ExitPlanMode → "User approved… exited plan mode" cleared
  the session-wide state. Re-launched the team; agents then made REAL edits.
- Lesson (harness eng): subagents inherit plan mode; must exit at session level, not
  via prompt. Recorded in protocol.

## [2026-06-20 ~06:30] M0 IMPLEMENT+REVIEW+VERIFY — all 3 tasks green
- T1 cooker-infra-deploy: observability bundle + gated Helm SM/PR + metrics-on + RUNBOOK.
- T2 cooker-security: SECURITY.md contact + public /.well-known/security.txt route + test.
- T3 cooker-infra-ci: helm-job hardening (gate renders, Datree CRD kubeconform, raw pass),
  make helm-validate, backlog Closed entry.
- PM review: all diffs match plan + CLAUDE.md conventions; no re-kick.
- PM gates: go build/vet/test -race green; dashboard JSON + all YAML valid;
  kubeconform deploy/kubernetes/ = 14/14 (CRDs via Datree catalog). helm render → CI.
- Committed e754698.
- Next: AUDIT (agent a228b2ce running — incl /metrics public-exposure check) → RESEARCH → push + draft PR.
