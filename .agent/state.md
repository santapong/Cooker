# Cooker Launch — Orchestration State

> **Living snapshot. Keep this file SMALL** — it is the rotating control surface the PM
> re-reads after every context compaction. Detailed history lives in `.agent/log/`.
> Rotation: when `.agent/log/current.md` exceeds its cap, `.agent/rotate.sh` archives it.
> This `state.md` only ever holds the *current* board + ledgers, not narrative history.

- **Project:** Cooker — full 3-lane launch (harden → license → SaaS)
- **Plan of record:** `docs/launch/README.md` + `/root/.claude/plans/can-you-give-me-swirling-hopper.md`
- **Working branch:** `claude/launch-execution` (off `main` @ #116 merged)
- **PM model:** Opus 4.8 (1M) · **Team policy:** Opus-only subagents
- **Last updated:** 2026-06-20 (M0 implemented + audited + remediated; ready to push/PR)
- **Phase:** EXECUTION → M0 Implement✅ Review✅ Verify✅ Audit✅ Research✅ Remediate✅ → **push + draft PR**

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
| M0 | Observability artifacts + metrics-on + security.txt + CI gate | A | — | 🟢 done → draft PR |
| M1 | DR drill + legal/status → self-hosted launch-ready | A | M0 | 🟢 done (committed `d21a55f`) |
| M2 | Self-hosted licensing (Ed25519 + entitlements + UI) | B0 | — | 🔵 T1✅T2✅T3✅ `49cd5e0` · docs✅ · audit running |
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
| M0-AUDIT independent audit + research | general-purpose (a228b2ce) | ✅ done | 8 findings + 3 research items (see registers) |
| M0-FIX remediation (M0-1..M0-5) | cooker-infra-deploy/ci/security | ✅ done | committed parts 1+2; gates green |

**M0 commits:** `e754698` (impl) · `<part1>` CI+security.txt · `<part2>` deploy mitigation · `.agent` records. Ready to push + open draft PR.

---

## Risk register (M0 audit — commit e754698)
| ID | Risk | Severity | Introduced by | Recommended change | Status |
|----|------|----------|---------------|--------------------|--------|
| M0-1 | Unauth `/metrics` on public app port. Served on bare router (`server.go:511`); M0 flips deploy default `metrics.enabled=true`. **Safe in default install (ingress.enabled=false → ClusterIP only)**, but if ingress is enabled its `/`-prefix routes `/metrics` publicly (route inventory, latencies, error counters). | **MED** (conditional) | M0 deploy-default flip | M0: keep enabled (safe by default) + values/RUNBOOK warning + ready nginx `configuration-snippet` to 403 `/metrics`; document ServiceMonitor internal-scrape path. **Permanent fix (follow-up): separate metrics port/listener** → tracked as R-FOLLOWUP-1. | ✅ done |
| M0-2 | `security.txt` `Expires` hardcoded `2027-06-20` (`securitytxt.go:11`); lapses → RFC9116-invalid. Test is a tripwire (CI reds on that date). | LOW | T2 | Compute `Expires` at startup (`time.Now().AddDate(1,0,0)`) so it never lapses; keep test. | ✅ done |
| M0-3 | Triple-copy alert-rule drift: standalone YAML has `runbook_url`+`description`, Helm & raw copies dropped them. No single generator. | MED | T1 | Restore annotation parity across the 3 copies; add note that Helm template is canonical. | ✅ done |
| M0-4 | Comment/metric mismatch `cooker-rules.yaml:84` (comment says `kube_pod_container_status_ready`, expr uses correct `kube_pod_status_ready`). | LOW | T1 | Fix comment. | ✅ done |
| M0-5 | CI "metrics-off" gate asserts NetworkPolicy absent but NOT ServiceMonitor/PrometheusRule absent, despite comment claiming both. No live bug (templates double-gated). | LOW | T3 | Add SM/PR absence greps to match the comment. | ✅ done |
| M0-6 | `/.well-known/security.txt` route precedence vs NoRoute/`/assets` — verified no shadowing. | INFO | T2 | none | ✅ accepted |
| M0-7 | Security-headers middleware avoids global `no-store` partly "so /metrics is cacheable" — mildly compounds M0-1 (intermediary caches). Pre-existing. | INFO | pre-existing | revisit if M0-1 permanent fix lands | ✅ accepted |
| M0-8 | No regression vs #115 (counters real, NetworkPolicy/securityContext/secretKeyRef untouched, no Allow-Credentials, no docker.sock). | INFO | — | none | ✅ accepted |
| R-FOLLOWUP-1 | Permanent M0-1 fix: bind `/metrics` to a dedicated internal port (`COOKER_METRICS_PORT`), ServiceMonitor scrapes it — never traverses public ingress. Backend+deploy change, out of M0 deploy-only scope. | — | roadmap | Schedule as a small standalone PR (post-M0). | ⚪ tracked |

---

## Research log (M0)
| Topic | Current approach | Alternative considered | Verdict | State |
|-------|------------------|------------------------|---------|-------|
| security.txt delivery | runtime Gin route + const body | `go:embed` static file | **Keep** — fine for a 4-line static doc; real defect was hardcoded Expires (M0-2), independent of delivery. | recorded |
| `/metrics` exposure | default-on, unauth, public app port (when ingress on) | (a) off-by-default; (b) **separate internal metrics port**; (c) auth/basic-auth on /metrics | **Change** — mature Go services expose /metrics on a separate internal port or require auth; k8s idiom = cluster-internal Service scraped by ServiceMonitor (never via public ingress). M0: mitigate via ingress block + docs; permanent = separate port (R-FOLLOWUP-1). | recorded |
| `CookerReadinessFailing` alert | depends on kube-state-metrics `kube_pod_status_ready` | `up{job="cooker"}==0` (Prometheus' own scrape via the shipped ServiceMonitor — no KSM dep) | **Change/document** — switch to `up==0` (no hidden KSM dependency) or document the KSM prerequisite. Folding into M0-3 fix. | recorded |

---

## Next actions (PM)
1. ~~Scaffolding + M0 team + review + verify + audit + remediation~~ ✅ all committed.
2. Push `claude/launch-execution`; open **M0 draft PR**.
3. Advance board → **M1** (DR drill + legal/status) and **M2** (Lane B0 licensing) — parallelizable.
4. Schedule **R-FOLLOWUP-1** (dedicated metrics port) as a standalone small PR.
5. Confirm **D4 (Cooker Cloud go/no-go)** with user before unblocking M3+.

## Log pointer
- Current log: `.agent/log/current.md`
- Archive index: `.agent/log/index.md`
