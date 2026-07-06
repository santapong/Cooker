# Cooker — Master GTM & Monetization Plan

> **CMO synthesis (`cooker-mkt-cmo`)** integrating the marketing & monetization research team's
> 15 specialist docs into one plan. Date: 2026-06-21.
> This is the top-level read; the depth lives in `channels/*.md` and `monetization/*.md`
> (and the reconciled `monetization/monetization-plan.md`). It extends — never contradicts —
> `docs/marketing/strategy.md`, `docs/launch/*`, and `docs/product-plan.md` §7.
> **Posture: adoption first, honesty over hype, keep the core OSS.** Numbers below are research
> estimates; every churn/conversion figure is `ASSUMPTION:`-grade until real telemetry exists.

---

## 1. Executive summary

1. **The bet is one sentence:** *graph-first, self-hosted CI/CD with a real deploy story, in a single
   Go binary.* No competitor in the self-hosted-PaaS niche (Coolify/Dokploy/CapRover) has a pipeline
   DAG editor; no K8s-native tool (Argo/Tekton) has drag-drop. Lead every surface with the visual graph.
2. **Adoption is the product of the launch; revenue is a later, separate motion.** Goal: 1,000 stars +
   5 external contributors by day 90. The HN launch day is effectively make-or-break (the cold-traffic
   spike happens once).
3. **Channels, in priority order:** the **launch/announcement** is the spine; **SEO** wins the
   "alternative/vs" long-tail (but **activation content ships before comparison content**); **GEO** is a
   clean-baseline land-grab (Cooker appears in *zero* AI answers today); **SEM stays off** until
   day 180+ and free→paid is proven.
4. **Make money in this order:** **(B0) self-hosted Ed25519 licensing first** (no blockers, ~4.5 d,
   87–92% margin — the ARR spine), **consulting/support in parallel** (first real cash), **sponsorship
   = signal only**, and **Cooker Cloud (B1/B2) as gated bull-case upside**, not the plan.
5. **Cloud is hard-gated** on the unbuilt `tenant_id` (6–8 wk) + build-farm isolation + pen-test + an
   unmade go/no-go (ADR-0004). Do not let the Stripe ask pull tenancy work forward before the go/no-go.
6. **Two pricing reversals the whole team converged on:** move **basic OIDC** and **SSH deploy** into the
   free Explorer tier (gate only SSO group-mapping + MFA step-up). Gating OIDC would make the HN thread
   about the paywall, not the product (the "SSO tax" backlash is real and documented).
7. **Lead pricing with the no-seat-tax story:** a 30-person team pays ~$450/mo on Buildkite vs **$49**
   on Cooker Crew (unlimited seats). That framing — not the Coolify $5 anchor — justifies the price.
8. **Plan to the bear case (~$4K cash over 36 months).** The ~$78K base case is upside until cohort
   data exists. This is a portfolio/reputation play that *can* pay, not a revenue forecast to staff against.

---

## 2. Positioning & audience

- **One-liner / tagline (in use):** "CI/CD you can see." The single highest-leverage asset is the
  60-second hero cast — if it's bad, the differentiator collapses.
- **ICP (two segments that matter now):** **S1 indie/solo** (the adoption engine — free forever, do
  **not** monetize hard) and **S2 SMB SaaS platform team** (the Crew buyer; the lead engineer is both
  user and buyer — no procurement). S3 growth-stage is a 6–12 mo upgrade path; **S4 enterprise is gated
  on multi-tenancy** and must not be marketed to yet.
- **Honesty constraint that binds everything:** Cooker is single-tenant today. Never claim
  "enterprise-ready" / "team-isolated." This constrains channel copy (announce/GEO accuracy) *and*
  monetization (no enterprise claims; Cloud gated). It is a trust asset, not a weakness — say what's
  not done.

---

## 3. Go-to-market: the integrated channel plan

Full detail in `channels/`. The integration that matters: **the channels share one funnel** —
acquisition (announce) → activation (the first-green-run content SEO/GEO point at) → the free→paid
trigger that monetization depends on.

### 3.1 Launch & announcement — the spine (`channels/announce.md`)
- **Show HN** (Mon 09:00 ET) is the trajectory-setter; pre-empt objections with a "what's not done yet"
  comment (single-tenant, no audit-log viewer, builder-choice). Extended objection table incl. the
  "React Flow isn't a differentiator" attack (answer: graph is the *authoritative data model*, not a UI
  overlay).
- **Add Product Hunt** (Wed, 12:01 PT; listing claimed 30 days out), real newsletters (DevOps Weekly,
  Go Weekly, TLDR DevOps, Bret Fisher), 7 live podcasts (Changelog, Kubernetes Bytes, Ship It!, …),
  **Discord over Matrix** (persona-1 lives there), first-week support SLA (bugs ≤4 h, questions ≤24 h).
- **Hard gate:** do not announce before the launch preconditions (hero cast, GoReleaser binaries, Helm
  chart, docs site, security quick-wins) in strategy.md §4 are met.

### 3.2 SEO — organic (`channels/seo.md`)
- **Win the long-tail first** ("Drone/Woodpecker/Coolify alternative", "GitHub Actions self-hosted"),
  not the head term. The **Coolify-alternative-with-pipelines** page is uniquely ownable (it *is* the
  positioning).
- **Activation content ("first green run in 60s") is the #1 30-day priority — ahead of comparison
  pages** (agreed with growth: it converts at the highest-intent moment).
- Ship **SoftwareApplication JSON-LD** before launch (co-owned with GEO); set **dev.to canonical URLs to
  `docs.cooker.dev`**; all launch CTAs point at the docs domain so backlinks accrue to the owned property.

### 3.3 GEO — generative engine optimization (`channels/geo.md`)
- **Clean baseline:** Cooker is cited by no AI assistant today. The lever is a **canonical citable
  sentence** (CMO sign-off required) used verbatim everywhere + an **`llms.txt`/`llms-full.txt`**
  auto-generated from the README + accurate facts seeded into 8 corpora **after** launch readiness
  (seeding an "incomplete" project into training data is irreversible).
- Shares the `/compare/` pages + schema with SEO (coordinate, don't duplicate). Monthly 10-prompt test
  across 4 engines, incl. an explicit "is Cooker multi-tenant?" accuracy guardrail.

### 3.4 SEM — paid search (`channels/sem.md`)
- **Off until day 180+** and three gates clear (B0 has ≥5 deals; free→paid measured; SEO confirms which
  terms we already win). Campaigns are *designed now, activated later*. CAC ceilings come from
  unit-economics: **self-hosted $250 / Cloud $180**. Never bid on terms we rank for organically.

### 3.5 Unified channel sequencing
- **Now → launch:** hero cast, docs site, GitHub Topics, SoftwareApplication schema, claim Product Hunt,
  draft the canonical GEO sentence, write the activation quickstart.
- **Launch week:** HN → Mastodon → r/selfhosted → dev.to #1 → r/devops+r/kubernetes → Product Hunt +
  YouTube → r/golang → recap. Artifact Hub + awesome-selfhosted listings same week.
- **30/60/90:** activation content + the 5 `/compare/` pages (GitHub Actions & Coolify first); seed GEO
  corpora; newsletters/podcasts; Discord office hours. Paid stays off.

---

## 4. Monetization (summary — full reconciled plan in `monetization/monetization-plan.md`)

- **Model & sequence:** B0 self-hosted licensing first → consulting in parallel → sponsorship as signal
  → Cloud (B1/B2) as gated upside. Tie to launch-doc lanes; B0 has zero dependencies.
- **Reconciled tier table (changes from launch-doc §1.4 in bold):** Explorer $0 / Crew **$49/replica/mo**
  / Constellation custom; **unlimited seats** on all paid tiers. **Basic OIDC → Explorer** (only SSO
  group-map + MFA step-up at Crew). **SSH deploy → Explorer.** Cloud base **$39-vs-$49 unresolved**
  (unit-economics: $39 leaves only ~$9 margin buffer → prefer $49 or cut included build-minutes to 1,000).
- **Unit economics:** self-hosted Crew LTV:CAC ~8.1×, ~5.5 mo payback, 87–92% margin; Cloud thinner
  (~3.2×, 77%). **Each build is a pod that costs money** — the rate limiter is the entitlement enforcement
  point.
- **Forecast:** bear ~$4K / base ~$78K / bull (adds Cloud in Y3) — **bear is the planning baseline**;
  paid-conversion rate is the dominant swing variable (1.5%→0.5% cuts Y3 ARR ~60%).
- **Market context:** TAM (CI/CD) $13–17B; bottom-up Coolify comp (~$4.31 ARR/star) → conservative Y3
  anchor. Crew = 10× Coolify's list price → the Buildkite no-seat-tax narrative is mandatory on the page.

---

## 5. Unified roadmap (channels + monetization + gates)

| Horizon | Marketing | Monetization | Gates that must be true |
|---|---|---|---|
| **Now (pre-launch)** | Hero cast, docs site, schema, canonical GEO sentence, activation quickstart, claim Product Hunt | Decide license (MIT/Apache); start B0 licensing build (~4.5 d); write ToS/Privacy/**AUP** | G1 security.txt, G2 legal docs, G3 license consistent, G5 OIDC→Explorer, G6 SSH→Explorer |
| **Launch week** | HN + multi-channel sequence; Artifact Hub + awesome-selfhosted | Publish FUNDING.yml (sponsorship signal); state a consulting page | Launch preconditions (hero cast, binaries, chart, docs) |
| **30/60/90** | Activation content → `/compare/` pages; seed GEO corpora; podcasts/newsletters; Discord | B0 licensing live; first consulting engagement; **measure free→paid** | G4 CLA bot before first external PR; G7 no-seat-tax pricing page |
| **Day 180+** | Consider SEM (triggered) | SEM only if free→paid proven + LTV supports CAC | SEM gates (≥5 B0 deals, conversion measured) |
| **Gated / 2027** | Enterprise/compliance messaging *only after* tenancy | Cloud (B1/B2) build + Stripe | **G8 tenant_id, G9 build-farm isolation + pen-test, G10–G11 GDPR/DPA, G12 Cloud go/no-go, G13 Stripe SAQ-A** |

(Full 15-gate checklist in `monetization/risk.md` and `monetization/monetization-plan.md`.)

---

## 6. Decision log (cross-cutting conflicts the team resolved)

| # | Conflict | Resolution | Who flagged / concurred |
|---|---|---|---|
| 1 | Gate OIDC at Crew? | **No — move basic OIDC to Explorer**; gate only SSO group-map + MFA step-up | risk (ruling); pricing, segmentation, growth concurred |
| 2 | SSH deploy tier | **Move to Explorer** (simplest target; gating reads as punitive) | risk |
| 3 | Whose churn/conversion numbers? | **All labeled ASSUMPTION:; bear case = planning baseline** until cohort data | risk + forecast vs the optimistic base |
| 4 | Pricing anchor | **Lead with Buildkite no-seat-tax**, not Coolify $5 | competitor; pricing concurred |
| 5 | Turn on paid search? | **Deferred to day 180+, triggered**; CAC ceilings $250/$180 | sem ↔ unit-economics |
| 6 | Cloud in the forecast? | **Bull-only, gated**; never base case | business-model, forecast, risk aligned |
| 7 | SEO priority order | **Activation content before comparison pages** | growth ↔ seo |
| 8 | GEO vs SEO overlap | **Shared `/compare/` pages + one schema/sentence**, coordinated | geo ↔ seo |
| 9 | Channel→persona mix | S1 = HN/r-selfhosted/Discord (free); S2 = r/devops/newsletters/Product Hunt (Crew) | announce ↔ segmentation |

Unresolved (escalated to §7): Cloud base $39-vs-$49; license MIT-vs-Apache; an S3 "Crew HA" bridge SKU
between Crew (~$200/mo) and Constellation (custom).

---

## 7. Top open questions for the maintainer

1. **Cooker Cloud — go or no-go?** (ADR-0004 decision A.) The biggest fork. B0 + consulting don't need
   it; B1/B2 + the 6–8-wk `tenant_id` build fully depend on it. **Do not build tenancy speculatively.**
2. **OSS core license — MIT or Apache-2.0?** Inconsistent across docs; it's printed in the binary.
   **Apache-2.0 recommended** (patent grant) + a **CLA bot live before the first external PR merges**.
3. **OIDC-gate sign-off.** Confirm the team's reversal of launch-doc §1.4 (basic OIDC → Explorer) before
   B0 ships.
4. **Cloud base price** $39 vs $49 (only relevant if #1 is yes).
5. **S3 bridge SKU** — investigate a flat "Crew HA" tier to stop growth-stage churn in the price gap.

---

## 8. Research index

- Brief: `00-brief.md`
- Channels: `channels/{seo,sem,geo,announce}.md`
- Monetization analysts: `monetization/{pricing,market-sizing,competitor,business-model,segmentation,unit-economics,forecast,growth,partnerships,risk}.md`
- Reconciled monetization synthesis: `monetization/monetization-plan.md`
- Team definition & how to re-run: `README.md` (invoke `cooker-mkt-cmo`)

*Produced by the `cooker-mkt-*` research team via a brief → drafts → cross-critique → synthesis loop.
Every figure is a research estimate to validate with real telemetry, not a commitment.*
