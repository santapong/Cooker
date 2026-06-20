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

## [2026-06-20 ~07:00] M0 AUDIT+RESEARCH — 8 findings, 3 research items
- Audit (Opus a228b2ce) over e754698. Headline: M0-1 unauth /metrics exposure.
- PM correction: ingress.enabled=false by default → /metrics ClusterIP-only in default
  install → M0-1 is MED (conditional on enabling ingress), not HIGH. Verified values.yaml.
- Recorded full Risk register (M0-1..M0-8 + R-FOLLOWUP-1) + Research log in state.md.

## [2026-06-20 ~07:20] M0 REMEDIATE — 3 Opus fix agents, all green
- M0-5 (cooker-infra-ci): CI metrics-off gate now asserts SM/PR absent. ✅
- M0-2 (cooker-security): security.txt Expires computed dynamically (now+1y). ✅
- M0-1/M0-3/M0-4 (cooker-infra-deploy): /metrics ingress warning + nginx deny snippet +
  RUNBOOK note; restored rule-annotation parity across all 3 copies (verified identical);
  fixed comment typo + documented KSM prereq. ✅
- PM re-verify: all YAML parse; kubeconform deploy/kubernetes/ = 14/14; go vet+tests -race green.
- Committed in 2 parts (CI+security.txt; deploy). All M0 risks ✅ except R-FOLLOWUP-1 (tracked).
- Next: push + open M0 draft PR; advance to M1/M2.

## [2026-06-20 ~14:10] M0 SHIP — pushed; draft PR #117; watching
- Pushed claude/launch-execution; opened draft PR #117 (M0).
- CI #117: helm ✅ (real helm render validates M0 templates!), frontend ✅, backend+docker in progress.
- Subscribed to PR #117 activity. send_later NOT available → re-check CI on next agent completion.

## [2026-06-20 ~14:12] M1+M2 DELEGATE — 3 Opus agents launched (disjoint)
- M1-T1 cooker-infra-ci (a36d64c1): scripts/backup-restore-drill.sh + Makefile target + docs/guides/DR.md.
- M1-T2 general-purpose (add8633a): docs/legal/* (ToS/Privacy/AUP/SLA) + STATUS_PAGE.md + INSTALL.md.
- M2-T1 cooker-backend-data (af2ccc3c): license model + store (mem+pg parity) + migration 024_licenses.
- M2-T2 (logic) + M2-T3 (frontend) queued AFTER M2-T1 (dependency chain).

## [2026-06-20 ~14:40] M1✅ + M2-T1✅ committed; M2-T2 launched
- #117 CI (M0): helm✅ docker✅ frontend✅ backend (in progress, no failures).
- M1-T1 (DR drill) + M1-T2 (legal/status docs) done → reviewed (shellcheck clean, 5/5 legal
  banners) → committed `d21a55f` (M1 = Lane A finish ✅).
- M2-T1 (license data layer) done → PM re-verify go build/vet/test -race green; reviewed
  store.go interface + migration → committed `58260bd`.
- M2-T2 launched (cooker-backend-api ace59cdf): verify(Ed25519)/entitlements/config/service/
  handler/gate/CLI. Gate built+tested but NOT wired to existing routes (regression guard).
- Push HELD until M2-T2 lands → push M1+M2-T1+M2-T2 as one backend unit (single CI cycle).
- Branch model: single `claude/launch-execution` (per user) → #117 is the ROLLING execution PR.

## [2026-06-20 ~15:00] M2-T2✅ committed+pushed; M2-T3 launched
- M2-T2 (license backend) done → PM full `go build/vet/test ./... -race` green; reviewed
  verify.go (sig-before-unmarshal, secure) + entitlement.go (402, fails-open, NOT wired).
- Committed `920f2ad`; pushed M1+M2-T1+M2-T2 → #117 (renamed to rolling M0–M2 PR).
- #117 CI run 27874134638: helm✅ frontend✅, backend+docker building.
- M2-T3 (cooker-frontend-ui aa95e7b3) launched: api/license.ts + licenseStore + Settings
  License tab + tier cards (soft hints only, no hard-gating).
- After M2-T3: review+verify, commit+push, then M2 docs follow-up (env vars in UAT/SECURITY)
  + surface D5 (open-core split) to user. M3+ still gated on D4.
