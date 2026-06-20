# Cooker Launch — Event Log (current)

> Append-only. Rotated by `.agent/rotate.sh` when this file exceeds its cap.
> Snapshot/board lives in `../state.md`. Archives indexed in `index.md`.

## [2026-06-20 ~05:50] M0 SETUP — orchestration stood up
- Merged PR #116 (squash) → `main` now has `docs/launch/`.
- Cut working branch `claude/launch-execution` off updated `main`.
- Created `.agent/` scaffolding: `state.md`, `protocol.md`, `log/current.md`, `rotate.sh`.
- Next: launch M0 team (3 Opus agents, disjoint file sets).
