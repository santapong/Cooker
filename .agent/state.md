# Cooker Launch — Orchestration State

> **Living snapshot. Keep this file SMALL** — it is the rotating control surface the PM
> re-reads after every context compaction. Detailed history lives in `.agent/log/`.
> Rotation: when `.agent/log/current.md` exceeds its cap, `.agent/rotate.sh` archives it.
> This `state.md` only ever holds the *current* board + ledgers, not narrative history.

- **Project:** Cooker — full 3-lane launch (harden → license → SaaS)
- **Plan of record:** `docs/launch/README.md` + `/root/.claude/plans/can-you-give-me-swirling-hopper.md`
- **Working branch:** `claude/launch-execution` (off `main` @ #116 merged)
- **PM model:** Opus 4.8 (1M) · **Team policy:** Opus-only subagents
- **Last updated:** 2026-06-20 (Lanes A+B0 shipped; wrap-up W2/W3 pushed; W1 final audit running)
- **Phase:** WRAP-UP (D4=pause SaaS, D5=gate dormant) → W2✅ W3✅ → **W1 final audit running** → W4 merge (await user)

---

## Decision ledger
| # | Decision | Value | Status |
|---|----------|-------|--------|
| D1 | Scope | Entire 3-lane roadmap | ✅ confirmed |
| D2 | Branch setup | Merge #116, new branch `claude/launch-execution` | ✅ confirmed |
| D3 | Team | Opus-only subagents; PM reviews every change | ✅ confirmed |
| D4 | Cooker Cloud go/no-go | **PAUSE SaaS, ship self-hosted first** — M3/M4/M5 parked | ✅ confirmed |
| D5 | Open-core split (wire the gate?) | **Keep entitlement gate DORMANT** — licensing ships, nothing hard-gated yet | ✅ confirmed |
| D6 | License expiry posture | Degrade-to-Free + warn | 🟡 assumed (built this way) |
| D7 | Primary managed cloud | AWS (only complete IaC) | 🟡 assumed (parked w/ M5) |

---

## Milestone board
| ID | Milestone | Lane | Gate | Status |
|----|-----------|------|------|--------|
| M0 | Observability artifacts + metrics-on + security.txt + CI gate | A | — | 🟢 done → draft PR |
| M1 | DR drill + legal/status → self-hosted launch-ready | A | M0 | 🟢 done (committed `d21a55f`) |
| M2 | Self-hosted licensing (Ed25519 + entitlements + UI) | B0 | — | 🟢 done `5eb8df5` (impl+docs+audit+remediation) |
| W | Wrap-up (W1 audit, W2 metrics-port, W3 key-rotation, W4 merge, W5 park) | A/B0 | — | 🔵 W2✅ W3✅ `5f34d7f` · W1 audit running · W4 await user |
| M3 | Multi-tenancy (tenant_id + build-farm isolation) | C | D4 flip | ⏸️ PARKED (D4: pause SaaS) |
| M4 | Stripe Cloud billing + metering | B1/B2 | M3 | ⏸️ PARKED |
| M5 | SaaS hosting (AWS) + GDPR + split-origin | SaaS | M3 | ⏸️ PARKED |

Legend: ⚪ pending · 🔵 in progress · 🟢 done · ⏸️ parked · 🔴 blocked/failing · 🟡 needs decision

**Resume pointer (if D4 → go):** start M3 = migration `024_owner_team` (closes IDOR S26-05-09)
→ data-plane `tenant_id` scoping → build-farm isolation+pen-test → M4 Stripe (dormant gate +
`internal/entitlements` are the seam) → M5 AWS SaaS. Full detail: plan file + docs/launch/.

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
| R-FOLLOWUP-1 | Permanent M0-1 fix: dedicated `/metrics` port (`COOKER_METRICS_PORT`) off the public ingress. | — | roadmap | Delivered in W2 (+ W1-01/02/03 hardening). | ✅ done |

### M2 audit (licensing — `58260bd`/`920f2ad`/`49cd5e0`) — no CRITICAL/HIGH; sec paths sound
| ID | Risk | Severity | Recommended change | Status |
|----|------|----------|--------------------|--------|
| M2-01 | Boot installer re-installs every boot (new UUID/InstalledAt) + clobbers UI-installed license | MED | idempotent on token identity (skip when stored RawToken == env) | ✅ fixed (Fix-A) |
| M2-02 | memory↔postgres ID parity break (pg forces id=`active`, memory keeps UUID) | MED | normalize memory ID to sentinel | ✅ fixed (Fix-B) |
| M2-03 | No postgres integration test for license store (CLAUDE.md parity rule) | MED | add `postgres/license_test.go` | ✅ fixed (Fix-B) |
| M2-04 | Install handler re-reads Current() — small inconsistency window | LOW | resolve entitlements from installed lic | ✅ fixed (Fix-A) |
| M2-05 | store error wraps drop `license:` prefix (cosmetic) | INFO | optional align | ✅ accepted |
| M2-06 | entitlement gate is dead code by design (dormant) | INFO | revisit at D5 wiring | ✅ accepted |
| M2-07 | tier-card CTAs presentational (no checkout) | INFO | B1 follow-up | ✅ accepted |
| R-FOLLOWUP-2 | Ed25519 key rotation: accept multiple pubkeys so the signing key can roll | — | fast-follow | Delivered in W3 (`COOKER_LICENSE_PUBLIC_KEYS` + `VerifyAny`). | ✅ done |

### W1 FINAL consolidated audit (whole launch delta `origin/main..HEAD`) — all triaged
| ID | Risk | Sev | Resolution | Status |
|----|------|-----|------------|--------|
| W1-01 | dedicated metrics server leaks on main-server-error exit path | **HIGH** | `metricsSrv.Close()` in errCh case | ✅ fixed |
| W1-02 | metrics binds all interfaces; NetworkPolicy didn't cover the port | MED | `COOKER_METRICS_HOST` + NetworkPolicy metrics-port rule | ✅ fixed |
| W1-03/04 | no `MetricsPort != Port` / range validation (silent metrics loss) | MED | `Validate()` checks | ✅ fixed |
| W1-06 | `handler/license` + `cmd/cooker-license` (root of trust) untested | MED | tests added; raw_token-never-in-body asserted on response | ✅ fixed |
| A-1 | CI metrics-enabled render gate could pass vacuously | MED | positive SM/PR render asserts | ✅ fixed |
| A-2 | `COOKER_LICENSE_PUBLIC_KEYS` (rotation) undocumented | MED | env docs + rotation runbook | ✅ fixed |
| A-3 | license not first-class in Helm | MED | `license:` values block (token via secretKeyRef) | ✅ fixed |
| W1-08 | GET /license leaked `installedByEmail` to any authed user | LOW | removed from response | ✅ fixed |
| A-4 | ingress `annotations:{}` uncomment trap | LOW | fixed comment/example | ✅ fixed |
| A-5 | `gt (int .port)` not nil-safe | LOW | `default 0 | int` | ✅ fixed |
| A-6 | `licenseStore.remove()` discarded server entitlements | LOW | trust response | ✅ fixed |
| A-7 | inert/overpromising tier CTAs | LOW | honest mailto CTAs | ✅ fixed |
| W1-05 | metrics bind error fire-and-forget | LOW | kept log-and-continue (deliberate; W1-03 catches common misconfig) | ✅ accepted |
| W1-07/09/10 | gate dormant by design / boot-install TOCTOU / no online revocation (offline licenses) | INFO | by design; documented (rotation = revocation) | ✅ accepted |

**W1 clean confirmations:** rule triple-copy parity holds, `/metrics` dedicated-port mitigation tested, store memory↔pg parity, migration 024 idempotent/reversible, all signature-change call-sites correct, DR drill accurate, legal templates safe, frontend solid, **no #115 regression.** W1 remediation commit: correct author (`Claude <noreply@anthropic.com>`).

---

## Research log (M0)
| Topic | Current approach | Alternative considered | Verdict | State |
|-------|------------------|------------------------|---------|-------|
| security.txt delivery | runtime Gin route + const body | `go:embed` static file | **Keep** — fine for a 4-line static doc; real defect was hardcoded Expires (M0-2), independent of delivery. | recorded |
| `/metrics` exposure | default-on, unauth, public app port (when ingress on) | (a) off-by-default; (b) **separate internal metrics port**; (c) auth/basic-auth on /metrics | **Change** — mature Go services expose /metrics on a separate internal port or require auth; k8s idiom = cluster-internal Service scraped by ServiceMonitor (never via public ingress). M0: mitigate via ingress block + docs; permanent = separate port (R-FOLLOWUP-1). | recorded |
| `CookerReadinessFailing` alert | depends on kube-state-metrics `kube_pod_status_ready` | `up{job="cooker"}==0` (Prometheus' own scrape via the shipped ServiceMonitor — no KSM dep) | **Change/document** — switch to `up==0` (no hidden KSM dependency) or document the KSM prerequisite. Folding into M0-3 fix. | recorded |
| **M2** license token format | custom `base64url(payload).base64url(ed25519 sig)`, single shared impl | PASETO v4.public / JWS-EdDSA | **Keep** — already has PASETO's key safety (no `alg` field → no alg-confusion). Do NOT adopt JWT here. PASETO only if ever standardizing. | recorded |
| **M2** entitlements model | hybrid: Go plan→feature map ∪ signed per-license `features[]` | pure claims-only (all features signed in) | **Keep** — signed features already allow one-off grants with no release; map gives a readable tier source-of-truth. | recorded |
| **M2** key management | single pubkey via `COOKER_LICENSE_PUBLIC_KEY` | multiple accepted pubkeys / `kid` rotation window | **Change (fast-follow)** — offline signing needs an operator rotation story (no JWKS). Low risk at launch (fresh key); add before key ages → R-FOLLOWUP-2. | recorded |

---

## Next actions (PM)
1. ~~M0+M1+M2 + audits/remediation + W2/W3 fast-follows + W1 final audit + remediation~~ ✅ all committed.
2. Push `claude/launch-execution` (W1 remediation) → #117; re-check CI.
3. **Await user: W4 merge call** for #117 (Lanes A+B0 self-hosted launch). Then move backlog items to Closed.
4. Optional (await user): authorize one-time force-push to clean ~20 "Unverified" historical commits (cosmetic; future commits already verified).
5. M3–M5 parked (D4). Resume pointer in milestone board if D4 flips to go.

## Log pointer
- Current log: `.agent/log/current.md`
- Archive index: `.agent/log/index.md`
