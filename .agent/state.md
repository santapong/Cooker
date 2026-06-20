# Cooker Launch — Orchestration State

> **Living snapshot. Keep this file SMALL** — it is the rotating control surface the PM
> re-reads after every context compaction. Detailed history lives in `.agent/log/`.
> Rotation: when `.agent/log/current.md` exceeds its cap, `.agent/rotate.sh` archives it.
> This `state.md` only ever holds the *current* board + ledgers, not narrative history.

- **Project:** Cooker — full 3-lane launch (harden → license → SaaS)
- **Plan of record:** `docs/launch/README.md` + `/root/.claude/plans/can-you-give-me-swirling-hopper.md`
- **Working branch:** `claude/launch-execution` (off `main` @ #116 merged)
- **PM model:** Opus 4.8 (1M) · **Team policy:** Opus-only subagents
- **Last updated:** 2026-06-20 (M0 implemented + committed `e754698`; audit in progress)
- **Phase:** EXECUTION → M0 Implement✅ Review✅ Verify✅ → **Audit (running)** → Research → push+PR

---

## Decision ledger
| # | Decision | Value | Status |
|---|----------|-------|--------|
| D1 | Scope | Entire 3-lane roadmap | ✅ confirmed |
| D2 | Branch setup | Merge #116, new branch `claude/launch-execution` | ✅ confirmed |
| D3 | Team | Opus-only subagents; PM reviews every change | ✅ confirmed |
| D4 | Cooker Cloud go/no-go | **Go**, but Lane C/B1/B2/SaaS only after A+B0 ship | 🟡 assumed |
| D5 | Pricing mockup binding | Yes (Explorer $0 / Crew $49-replica / Constellation custom) | 🟡 assumed |
| D6 | License expiry posture | Degrade-to-Free + warn | 🟡 assumed |
| D7 | Primary managed cloud | AWS (only complete IaC) | 🟡 assumed |

---

## Milestone board
| ID | Milestone | Lane | Gate | Status |
|----|-----------|------|------|--------|
| M0 | Observability artifacts + metrics-on + security.txt + CI gate | A | — | 🔵 IN PROGRESS |
| M1 | DR drill + legal/status → self-hosted launch-ready | A | M0 | ⚪ pending |
| M2 | Self-hosted licensing (Ed25519 + entitlements + UI) | B0 | — (parallel M1) | ⚪ pending |
| M3 | Multi-tenancy (tenant_id + build-farm isolation) | C | D4 confirm | ⚪ blocked on decision |
| M4 | Stripe Cloud billing + metering | B1/B2 | M3 | ⚪ blocked |
| M5 | SaaS hosting (AWS) + GDPR + split-origin | SaaS | M3 | ⚪ blocked |

Legend: ⚪ pending · 🔵 in progress · 🟢 done(merged) · 🔴 blocked/failing · 🟡 needs decision

---

## Active tasks (current milestone: M0)  →  committed `e754698`
| Task | Agent (Opus) | Status | Notes |
|------|--------------|--------|-------|
| M0-T1 observability bundle | cooker-infra-deploy | ✅ done | reviewed + gates green |
| M0-T2 security.txt + SECURITY.md contact | cooker-security | ✅ done | public Gin route (RFC 9116) + test |
| M0-T3 CI helm/kubeconform hardening (P6.1) | cooker-infra-ci | ✅ done | gate renders + Datree CRD + raw pass |
| M0-AUDIT independent audit + research | general-purpose (a228b2ce) | 🔵 running | incl. `/metrics` public-exposure check |

---

## Risk register (filled during AUDIT phase)
| ID | Risk | Severity | Introduced by | Recommended change | Status |
|----|------|----------|---------------|--------------------|--------|
| _none yet — audit runs after each milestone's implementation_ | | | | | |

---

## Research log (filled during RESEARCH phase)
| Topic | Current approach | Alternative considered | Verdict | State |
|-------|------------------|------------------------|---------|-------|
| _none yet — research runs after audit_ | | | | |

---

## Next actions (PM)
1. ~~Commit `.agent/` scaffolding~~ ✅ (`af3fc19`).
2. ~~Launch M0 team + review + verify~~ ✅ (committed `e754698`).
3. Review audit findings (agent a228b2ce) → populate Risk register; fix CRITICAL/HIGH before push.
4. Record research findings → Research log.
5. Update `.agent/state.md` board; commit `.agent` updates.
6. Push `claude/launch-execution`; open M0 draft PR; advance board → M1/M2.

## Log pointer
- Current log: `.agent/log/current.md`
- Archive index: `.agent/log/index.md`
