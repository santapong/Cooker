# Cooker — Production-Readiness & Launch Roadmap

**Date:** 2026-06-20 · **Status:** assessment + roadmap (no code changed) · **Audience:** maintainer / launch decision-maker

This folder is a five-part production-readiness assessment plus this consolidated roadmap. It was commissioned to answer: *what does it take to make Cooker production-grade and launchable* — across **SLA/reliability, Stripe billing, subscription plans, hosting, and the legal/compliance surface* — covering all three end-states the maintainer wants: **(A) harden & launch the current product now, (B) sell it self-hosted with licensing, (C) run it as a hosted SaaS (“Cooker Cloud”) with Stripe subscriptions.**

| # | Section | One-line takeaway |
|---|---------|-------------------|
| [00](00-sre-sla-readiness.md) | SRE / SLA readiness | Self-hosted best-effort can ship now; a *contractual* SLA needs ~1–1.5 wk of operator scaffolding (dashboards, alert rules, proven restore). |
| [01](01-billing-monetization.md) | Billing & monetization | Stripe Billing (Checkout + Portal + Meters); **self-hosted licensing has no blockers (~4.5 d)**; Cloud billing is gated on tenancy. |
| [02](02-multitenancy-saas.md) | Multi-tenancy / SaaS isolation | The **long pole**: `tenant_id` is unbuilt; honest effort **6–8 weeks**, and the build-farm (runs untrusted code) is the top risk. |
| [03](03-hosting-deploy.md) | Hosting & deploy | **Vercel = frontend only.** Backend → k3s/VPS (~$15–45/mo) to launch; AWS is the only cloud with complete IaC. |
| [04](04-security-compliance-legal.md) | Security / compliance / legal | Stripe keeps PCI at **SAQ-A**; `tenant_id` is a **GDPR prerequisite** (per-customer erasure), not just a feature; SOC 2 is premature. |

---

## TL;DR — the honest verdict

1. **You can launch a self-hosted product in ~2 weeks.** The engine is solid (the 2026-06 audit closed the CRITICALs; boot/crash resilience is the codebase’s strongest asset). What’s missing for launch is **operator scaffolding and paperwork**, not hardening: ship the Grafana dashboards + alert rules that the runbook already references but don’t exist on disk, *prove* a Postgres restore, fix the placeholder security-contact, and write ToS/Privacy/AUP.

2. **You can start charging without building a SaaS.** Self-hosted **licensing (offline Ed25519 keys, ~4.5 days)** has **zero dependency on multi-tenancy**. This is the fastest path to revenue and should ship first.

3. **Hosted SaaS with Stripe subscriptions is a real project, not a bolt-on.** Stripe itself is ~1–2 weeks. The blocker is everything underneath it: **`tenant_id` isolation is unbuilt (6–8 weeks)**, and a multi-tenant build farm that runs *untrusted customer build code* needs per-tenant namespaces + a runtime sandbox (gVisor/Kata) + a pen-test before you can open public signups. Stripe can’t bill a “customer” until that customer is an isolated tenant.

4. **The “test on Vercel first, then GCP/AWS/Azure” plan needs one correction:** Vercel can host the **React frontend** (with nice per-PR previews) but **not** the Go/WebSocket/Kaniko backend. The workable shape is **frontend-on-Vercel + backend-on-cloud**, and the split-origin config is *already coded* (`frontend/src/api/origin.ts`). Cheapest launch backend is **k3s on a single VPS**; AWS is the only target with turnkey Terraform today.

---

## The critical path (one picture)

```
                     ┌─────────────────────────────────────────────┐
LANE A  (now)        │ Harden & launch — self-hosted, best-effort   │  ~2 wks, no blockers
                     │ dashboards · alert rules · restore drill ·   │
                     │ ToS/Privacy/AUP · security.txt · status page │
                     └─────────────────────────────────────────────┘
                                        │ (independent)
LANE B  (now)        ┌─────────────────────────────────────────────┐
                     │ B0 Self-hosted licensing (Ed25519 keys)      │  ~4.5 d, NO tenancy dep
                     └─────────────────────────────────────────────┘
                                        │
                          ════════ CRITICAL PATH ════════
                                        ▼
LANE C  (the gate)   ┌─────────────────────────────────────────────┐
                     │ MULTI-TENANCY (tenant_id)  ── 6–8 weeks ──    │  THE long pole
                     │  Step1 024_owner_team (½ d, closes IDOR seed) │
                     │  data-plane scoping (~2 wk)                   │
                     │  build-farm isolation + pen-test (~2 wk)      │  ← top security risk
                     │  secrets / quota / WS / job-queue (~2 wk)     │
                     └─────────────────────────────────────────────┘
                                ▼                         ▼
                     ┌────────────────────┐   ┌──────────────────────────┐
                     │ B1 Cloud billing   │   │ SaaS hosting: per-tenant │
                     │ (Stripe, ~8 d)     │   │ build NS + sandbox + HA   │
                     │ B2 metering (~2 d) │   │ + GDPR erasure/DSAR       │
                     └────────────────────┘   └──────────────────────────┘
                                        ▼
                            Hosted “Cooker Cloud” launch
```

**Everything that says “Cloud/SaaS” converges on `tenant_id`.** Billing-cloud, SaaS hosting, and clean GDPR per-customer erasure are all blocked on it. That’s why Lanes A and B (which don’t touch tenancy) are the rational first moves.

---

## Recommended sequencing

### Phase 0 — This week (quick wins, mostly non-code)
- **Ship the missing observability artifacts.** `RUNBOOK.md` references `deploy/observability/dashboards/` (Grafana JSON) — **the directory doesn’t exist**. Convert the inline alert rules into a real `PrometheusRule`. *(00, P0)*
- **Flip the metrics footgun:** `COOKER_METRICS_ENABLED` defaults `false`; set it on in prod values. *(00)*
- **Fix the security contact:** `SECURITY.md` reporting email is a `.example.com` placeholder; add `/.well-known/security.txt`. *(04, S, cheap)*
- **Declare Helm the supported install path;** label `deploy/kubernetes/` “reference only” (it lags the chart). *(00, 03)*
- **Confirm on `main`** the two reliability CRITICALs (CR-1 hub deadlock, CR-3 build leak) are the merged fixes from PR #115. *(00)* — they are; merged.

### Phase 1 — Self-hosted launch (Lane A + B0, ~2–3 weeks, parallelizable, no tenancy)
- Prove backup/restore (define RPO ≤24h / RTO ≤1h; back up `COOKER_SECRET_KEY` **separately** — its loss is irrecoverable). *(00)*
- Write the legal must-haves: ToS, Privacy Policy, **AUP** (load-bearing — users run arbitrary build/deploy code), SLA doc (best-effort tier), status page. *(04)*
- **B0 self-hosted licensing**: `internal/billing/` license verifier, Ed25519 offline keys, degrade-to-Free-on-expiry, Settings UI. Same `Entitlements` struct the Cloud path will reuse. *(01)*
- Hosting: publish a **split-origin deploy guide** (Vercel SPA + k3s/VPS backend), and the k3s RWX-storage note for the Kaniko context PVC. *(03)*

### Phase 2 — The tenancy gate (Lane C, 6–8 weeks, 2 eng + infra)
- **Step 1 first: `024_owner_team` migration (½ day)** — additive, `DEFAULT 1` backfill, behind `COOKER_TENANCY_MODE=single|multi`; partially closes the still-open **HIGH IDOR `S26-05-09`**. *(02)*
- Data-plane scoping on every `List`/`Get`/write (~2 wk); **build-farm isolation**: per-tenant namespaces + gVisor/Kata + NetworkPolicy + quotas, then a **pen-test** (~2 wk); secrets/WS/job-queue scoping (~2 wk). *(02)*

### Phase 3 — Hosted SaaS (after the gate)
- **B1 Stripe Cloud billing** (Checkout + Customer Portal + webhooks/dunning, ~8 d) → **B2 usage metering** via Stripe Meters (~2 d). PCI SAQ-A maintained. *(01, 04)*
- HA hosting on managed K8s (Redis backends already supported), GDPR erasure/DSAR wired to `tenant_id`, DPA + sub-processor list. *(03, 04)*
- SOC 2 only when enterprise demand appears — Cooker already emits the evidence (audit trail, RBAC, OIDC, signed releases); the gap is org-process. *(04)*

---

## Open product decisions (need the maintainer, not engineering)

1. **Cooker Cloud — go / no-go?** This is the single biggest fork. ADR-0004 deferred it pending a “hosted-Cloud signal.” Lanes A+B don’t need the answer; Phase 2/3 fully depend on it.
2. **Pricing mockup — binding?** `cosmic-pricing.html` promises per-replica + unlimited seats (Explorer $0 / Crew $49-per-replica / Constellation custom). Doc 01 proposes honoring it self-hosted and metering builds in Cloud — confirm. *(01)*
3. **License expiry posture:** degrade-to-Free (recommended; never brick a paying customer’s prod) vs hard-stop. *(01)*
4. **Open-core source split:** which features are licensed/Enterprise vs OSS. *(01)*
5. **Primary cloud for managed hosting:** AWS (only complete IaC today) vs GCP/Azure (need ~1–2 d IaC each). *(03)*

---

## Effort summary

| Lane / phase | Effort | Blocked by |
|---|---|---|
| Phase 0 quick wins | days | — |
| Lane A: self-hosted launch readiness | ~1–1.5 wk | — |
| Lane B0: self-hosted licensing | ~4.5 d | — |
| Lane C: multi-tenancy (`tenant_id` + build isolation) | **6–8 wk** | Cloud go/no-go |
| Lane B1+B2: Stripe Cloud billing + metering | ~10 d | Lane C |
| SaaS hosting (per-tenant build farm, HA, GDPR) | ~2–3 wk | Lane C + pen-test |

**Bottom line:** ship Lane A + B0 now (revenue + a real launch in weeks), and treat the hosted-SaaS ambition as a deliberate Phase-2/3 program gated on the multi-tenancy decision — don’t let the Stripe ask pull you into the 6–8-week tenancy build before you’ve decided Cooker Cloud is actually happening.
