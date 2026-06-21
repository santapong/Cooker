# Cooker — Monetization Plan (synthesis)

> **Author:** monetization lead (Round 4 synthesis). **Date:** 2026-06-21.
> **Inputs:** the 10 analyst drafts under `docs/marketing/research/monetization/` (pricing,
> market-sizing, competitor, business-model, segmentation, unit-economics, forecast, growth,
> partnerships, risk) + the shared brief (`../00-brief.md`).
> **Grounding (referenced, never overwritten):** `docs/launch/01-billing-monetization.md`,
> `docs/launch/README.md`, `docs/product-plan.md` §7.
> This is the team's single voice. The decision log (§8) records who was on each side of every
> conflict resolved here. All figures carry `SOURCE:` or `ASSUMPTION:`; churn and conversion
> numbers have **no Cooker-specific data** and are labelled accordingly.

---

## 1. Executive summary

- **Cooker makes money by selling self-hosted commercial licenses first, funded in the gap by
  consulting/support — not by a hosted SaaS.** Self-hosted licensing (B0, offline Ed25519 keys)
  has **no blockers** and ships in ~4.5 days; it is the fastest, highest-margin (~87–92%) path to
  revenue.
- **Consulting/support runs in parallel and is the first *real* cash.** It needs zero product
  code, arrives when an SMB buyer asks for deployment help, and de-risks the months between launch
  and the first license sale. It is high-variance and capacity-gated — model it conservatively.
- **Sponsorship (GitHub Sponsors / Open Collective) is signal, not income** ($0–200/mo). It is a
  leading indicator of paid intent and a contributor magnet, nothing more.
- **Cooker Cloud (B1/B2) is gated upside, not the plan.** It is hard-blocked on the unbuilt
  `tenant_id` multi-tenancy (6–8 weeks), per-tenant build-farm isolation + pen-test, and an
  **unmade** go/no-go (ADR-0004). Cloud is bull-only and no earlier than 2027.
- **Pricing holds the committed mockup shape:** Explorer $0 / Crew **$49 per replica/mo** /
  Constellation custom, with **unlimited seats** (a binding FAQ promise). Two entitlement rows
  change from the launch doc: basic OIDC and SSH deploy both move *down* to Explorer.
- **Lead the price narrative with no-seat-tax, not the Coolify anchor:** a 30-person team pays
  ~$450/mo on Buildkite Pro (30 × $15) before any compute; Cooker Crew is $49 for unlimited seats.
  That is the quotable wedge for the team buyer.
- **The bear case is the planning baseline.** ~$4K total cash over 36 months is the median outcome
  for a new OSS tool that misses the HN front page — not a tail risk. Treat the ~$78K base case as
  upside until real cohort data exists.
- **Five decisions belong to the maintainer before B0 ships:** Cloud go/no-go, MIT-vs-Apache core
  license, the OIDC-gate sign-off, the $39-vs-$49 Cloud base, and a CLA bot live before the first
  external PR merges.

---

## 2. Recommended revenue model + sequence

The sequence maps one-to-one onto the launch-doc lanes (`docs/launch/README.md`). No step requires
the next to begin.

| Lane (launch doc) | Move | Blocker | Time to first $ | Role |
|---|---|---|---|---|
| **B0** (Lane B) | Self-hosted licensing — offline Ed25519 keys, degrade-to-Free on expiry | **None** | Weeks after launch if traction arrives | **Revenue spine. Ship first.** |
| **Parallel** | Consulting / support (deploy, operate, migrate) | None — invoice via Stripe Payment Link / bank transfer | Immediate, if one buyer appears | **First real cash; lumpy.** |
| **Parallel** | Sponsorship (GitHub Sponsors / Open Collective) | None | $0–200/mo | **Signal only.** |
| **Distribution** | Free marketplaces (Artifact Hub, awesome-selfhosted, DO/Linode 1-click) | v0.1.0 GoReleaser/Helm artifacts | $0 direct | Compounds the B0 funnel. |
| **B1 / B2** (Lane C → B1/B2) | Cooker Cloud billing + build-minute metering (Stripe) | **`tenant_id` (6–8 wk) + build-farm sandbox + pen-test + unmade go/no-go** | 4–6 months min, 2027 realistically | **Gated upside. Do not build speculatively.** |

**Why this order.** B0 has zero `tenant_id` dependency and validates the entitlements engine that
B1 later reuses. Cloud cannot bill a "customer" until that customer is an isolated tenant, and
building it speculatively violates the product-plan §7 anti-goal ("no solo-operated paid SaaS
before fix-first + pen-test"). Consulting precedes licensing because it requires no code and floors
the bear case. Sponsorship and marketplaces are adoption infrastructure, not revenue lines.

---

## 3. Tier / price / entitlement table (reconciled)

This is the launch-doc §1.4 table **with the Round 2–3 conflict resolutions applied**. Changes from
`docs/launch/01-billing-monetization.md` §1.4 are marked **CHANGED**; rows requiring a maintainer
decision are marked **DECISION**.

| Entitlement | Explorer ($0) | Crew ($49/replica/mo) | Constellation (custom) | Note |
|---|---|---|---|---|
| Self-hosted price | $0 | $49 / replica / mo | Custom annual | Committed mockup |
| Cloud price | $0 | **$49 base, or $39 + 1,000 included min** | Custom | **DECISION — see §3 note** |
| Seats | Unlimited | Unlimited | Unlimited | Binding FAQ promise |
| Replicas (self-hosted) | 1 | Unlimited (soft-warn, not hard-fail) | Unlimited | — |
| Pipelines / runs | Unlimited | Unlimited | Unlimited | Binding FAQ promise — do **not** gate |
| Concurrent builds | 1 | 3 included (Cloud meters overage) | Negotiated | — |
| Build-minutes/mo (Cloud) | 200 | 1,000–2,000 included (see §3 note) | Pooled | **DECISION** |
| Environments | 1 (Dev) | 3 (Dev/Staging/Prod) | Unlimited | Primary upgrade trigger |
| **Deploy targets** | **K8s, Fly, Render, SSH** | + ECS, Cloud Run | All + air-gapped | **CHANGED — SSH moved to Explorer** |
| Run retention | 7 days | 90 days | Configurable / export | — |
| Secrets backends | Postgres AES-GCM | + Vault / AWS / GCP | + KeepSave multi-tenant | — |
| **Basic OIDC login** | **Yes** | Yes | Yes | **CHANGED — moved to Explorer** |
| **SSO group→role map** | No | No | Yes | Unchanged — real enterprise value |
| **MFA step-up** | No | Yes | Yes | **CHANGED — explicit at Crew** |
| Cron triggers | No | Yes | Yes | — |
| Audit log + OTLP | No | Basic | Full + append-only export | — |
| API tokens / YAML export | Yes | Yes | Yes | Adoption drivers — never gate |
| Support | Community | Priority email | SLA + dedicated CSM | — |
| 14-day trial | n/a | Yes (no card) | Sales-assisted | Degrades to Explorer, never bricks |

**§3 note — Cloud base ($39 vs $49) [DECISION].** Unit-economics shows the $39 Cloud base yields
~$9 COGS = **77% gross margin**, only ~$9 above the 70%-margin floor of $30 — a thin buffer that
any Spot-scarcity event, support-load growth, or heavier-than-median build duration can erode below
70%. Two resolutions, either acceptable: **(a)** raise the Cloud base to **$49** (matching
self-hosted, widens headroom, simplifies the page), or **(b)** keep **$39 but cut included
build-minutes from 2,000 to 1,000**. Forecast confirms (a) lifts bull Cloud ARR ~25% with no volume
change. Self-hosted Crew at $49/replica is **unaffected** — its margin is 87–92% and needs no
change. This is moot until the Cloud go/no-go is "yes," but resolve it before any Cloud build.

**Why SSH and basic OIDC moved down** (full rationale in §8): both were flagged as punitive
free-tier exclusions that would dominate a launch-week HN thread. OIDC is the
[SSO-tax](https://ssotax.org/why) trap — charging for it reads as a security ransom and contradicts
product-plan §7's "adoption first." SSH is the cheapest deploy target; gating it looks like a cash
grab. Moving them down costs little conversion (the environment-limit trigger is cleaner) and buys
real OSS goodwill.

---

## 4. Unit economics summary

ASSUMPTION (no Cooker-specific data; provenance = published SaaS benchmarks, owner = risk/growth):
self-hosted churn 1.8–2.0%/mo steady-state (3.0%/mo for the first-90-day cohort), Cloud churn
3.0–3.5%/mo.

| Metric | Self-hosted Crew ($49/replica) | Cloud Crew ($39 base) |
|---|---|---|
| COGS / mo | ~$6 (support-dominated; license issuance ≈ $0) | ~$9 (build pods + Postgres + Stripe + support) |
| Gross margin | **87–92%** | **77%** (fragile; at the floor) |
| LTV (price × GM / churn) | **~$2,181** | **~$859** |
| Organic blended CAC | ~$268 (declines as base grows) | ~$268 |
| **LTV : CAC (organic)** | **8.1×** | **3.2×** |
| Payback | ~5.5 mo | ~8.3 mo |
| **Paid-CAC ceiling (SEM, post-day-180)** | **$250** | **$180** |

All four scenarios clear the SaaS bars (LTV:CAC ≥ 3×, payback < 12 mo) even under pessimistic churn.
**Self-hosted Crew is the strong unit-economics story.** Cloud is positive but thinner and depends
on holding churn < 3.5%/mo and not over-sizing included build-minutes. No paid acquisition before
day 180 (strategy.md hard rule); the CAC ceilings above are the upper bound when SEM eventually
activates. One Constellation deal ($5K–15K ARR, near-100% self-hosted margin) reshapes the whole
P&L — high upside, low launch-window probability, and gated on `tenant_id` for team features.

---

## 5. Forecast scenarios

ARR figures are B0 self-hosted licensing (ARR-equivalent). Consulting is one-time cash, shown
separately. Cloud is bull-only and post-2027. **The bear case is the credible planning baseline.**

| Scenario | Premise | Y1 ARR (lic) | Y1 consulting | Y1 total cash | Y3 ARR (lic) | Y3 consulting | Y3 total cash | Y3 Cloud ARR |
|---|---|---|---|---|---|---|---|---|
| **Bear (PLANNING BASELINE)** | HN misses; <500 stars Y1; 1% paid conv.; no consulting | ~$1,200 | $0 | **~$1,200** | ~$4,100 | $0 | **~$4,100** | $0 |
| **Base (upside until cohort data)** | 1,000 stars d90; 1.5% paid conv.; ~2 consulting/yr | ~$12,000 | ~$6,000 | ~$18,000 | ~$58,000 | ~$20,000 | ~$78,000 | $0 |
| **Bull** | Strong HN; 3,000 stars Y1; 3% paid conv.; 4 consulting/yr; Cloud unlocked H2'27 | ~$52,000 | ~$20,000 | ~$72,000 | ~$270,000 | ~$50,000 | ~$320,000 | ~$14–37K |

**Read this honestly.** The bear scenario (~$4K cash over 36 months) is *not* a tail — it is the
median outcome for an OSS tool that does not reach the HN front page (forecast + risk both rule
this). It is below any solo-maintainer sustainability threshold. The base case is the upside
scenario; do not present it as the plan, and do not authorise spending against it, until ≥1 real
cohort exists. The single largest swing variable is **paid conversion rate** (0.5% → bear-level
revenue even with bull-level stars); the second is the **stars-to-installs multiplier**. Cloud,
even in bull Y3, is ~$14–37K — material but never the spine. **Self-hosted B0 is the ARR spine in
every scenario.**

---

## 6. Pricing narrative

Lead with **no seat tax**, framed against Buildkite — *not* the Coolify $5 anchor (competitor's
recommended lead, endorsed by risk).

- **Team buyer (the Crew pitch):** "A 30-person team on Buildkite Pro pays **$450/mo** (30 × $15)
  before touching compute. CircleCI and GitLab compound the same per-seat tax. **Cooker Crew is $49
  for unlimited seats.**" This makes $49 feel *cheap* and is concrete and quotable. SOURCE
  (2026-06-21): Buildkite Pro $15/user, CircleCI $15/user, GitLab Premium $29/user (competitor.md).
- **Solo buyer:** lead with **free forever** — Explorer covers one replica, one environment, SSH +
  K8s deploy, basic OIDC, unlimited pipelines/runs/seats. Do not show the Buildkite math to this
  audience; it does not compute for a team of one.
- **Why the split matters:** the Coolify $5/server anchor is real and will be in the buyer's head
  ($49 is 10× it). One pricing page cannot serve both anchors — segment the copy by audience (risk
  §5.4, competitor flag). If the page lets the Coolify anchor win unanswered, conversion tracks the
  0.5–1% bear range, not the 1.5% base. The page **must** carry: "Coolify deploys; it doesn't build
  your images or run your CI." and the Buildkite seat-tax comparison, both before B0 launches
  (gate G7).

---

## 7. Go / No-Go gate checklist

Condensed from risk.md's 15 gates, grouped by what they block. The lead verifies the relevant group
before each revenue action.

| # | Gate | Status |
|---|---|---|
| **— B0 self-hosted (all must clear before launch) —** | | |
| G1 | Real monitored security contact + `/.well-known/security.txt` (replace `*.example.com` placeholder) | OPEN — cheap, do now |
| G2 | ToS, Privacy Policy, **AUP** published (AUP must prohibit cryptomining / malware / DoS builds) | OPEN |
| G3 | OSS core license decided (MIT vs Apache-2.0), consistent across README / binary / LICENSE | OPEN — **maintainer** |
| G4 | CLA bot live **before the first external PR merges** (d90 contributor target accelerates this) | OPEN — urgent |
| G5 | OIDC gate revised: basic OIDC → Explorer; only SSO group-map + MFA step-up at Crew | OPEN — **maintainer sign-off** |
| G6 | SSH deploy target moved to Explorer | OPEN |
| G7 | Pricing page carries the no-seat-tax (Buildkite) comparison + the Coolify-delta answer | OPEN |
| G14 | Consulting contracts explicitly disclaim SLA in the solo-maintainer phase | OPEN |
| G15 | All churn/conversion figures in spend-authorising docs labelled "ASSUMPTION: no product data" | OPEN |
| **— Cloud (B1/B2) — none may be skipped before public signups —** | | |
| G8 | `tenant_id` data model (ADR-0004 App. A) merged | OPEN — 6–8 wk |
| G9 | Per-tenant build-farm isolation (gVisor/Kata) + external pen-test passed | OPEN — follows G8 |
| G10 | GDPR right-to-erasure tooling wired to `tenant_id` | OPEN — follows G8 |
| G11 | DPA + SCCs offered to EU Cloud customers | OPEN — legal |
| G12 | **Cloud go/no-go decision made (ADR-0004 decision A) — do not build B1 speculatively** | OPEN — **maintainer** |
| G13 | Stripe Checkout confirmed SAQ-A (no card field in Cooker DOM; webhook sig verified) | OPEN — billing unbuilt |

---

## 8. Decision log (the record of the discussion)

Each cross-team conflict, its resolution, and which analysts were on each side.

1. **OIDC gate — paywall vs. free.** *Flagged by:* pricing (§5), growth, segmentation. *Adjudicated
   by:* risk (Round 3, binding). **Resolution: basic OIDC → Explorer; gate only SSO group→role map
   and MFA step-up at Crew.** Reverses the launch-doc §1.4 `feature.oidc_mfa` row. Basis: the
   [SSO-tax](https://ssotax.org/why) community norm, CISA "secure by design," every adjacent OSS
   tool ships OIDC free, and product-plan §7's "adoption first." The environment-limit trigger
   replaces it as the cleaner Explorer→Crew conversion event. *No dissent.*

2. **SSH deploy target — Explorer vs. Crew.** *Flagged by:* risk alone (§2.2); launch doc and
   pricing both had SSH at Crew. **Resolution: SSH → Explorer; keep ECS + Cloud Run at Crew.** SSH
   is the cheapest, most primitive deploy target; gating it from self-hosters reads as a cash grab.
   ECS/Cloud Run signal a budgeted cloud workload. *Accepted; no counter-argument raised.*

3. **Cloud base price — $39 vs $49.** *Flagged by:* unit-economics (§4, §8) → forecast confirmed
   the swing. *Pricing* set $39 originally (in line with CircleCI). **Resolution: leave open as a
   maintainer decision; recommend $49 OR keep $39 with included minutes cut to 1,000.** $39 leaves
   only ~$9 margin buffer (77% GM, floor is $30). Self-hosted $49/replica is untouched. Moot until
   Cloud go/no-go is "yes."

4. **Value metric — per-replica vs. per-seat vs. usage.** *Pricing, segmentation, business-model*
   all affirm the mockup. **Resolution: keep per-replica + unlimited seats for self-hosted (B0);
   add build-minute metering for Cloud only.** Unlimited seats is a binding FAQ promise and the
   no-seat-tax wedge; reversing it is a deliberate marketing change, not in scope. *Unanimous.*

5. **Planning baseline — bear vs. base.** *Flagged by:* forecast (bear ≈ median for a missed HN
   launch) → risk elevated it to binding (§5.1). **Resolution: bear (~$4K/36 mo) is the planning
   baseline; base is upside until cohort data exists.** Pricing's $49 narrative must land or
   conversion tracks the 0.5–1% bear range. *Agreed by forecast, risk, market-sizing.*

6. **Cloud in the forecast — base-case revenue vs. gated upside.** *Business-model, market-sizing,
   forecast, risk* align. **Resolution: Cloud is bull-only, post-2027, excluded from base.** It is
   hard-gated on `tenant_id` + isolation + pen-test + an unmade go/no-go (ADR-0004). *Unanimous;
   this is a launch-doc hard constraint, not a debate.*

7. **Pricing-page anchor — Buildkite no-seat-tax vs. Coolify $5.** *Competitor* surfaced both but
   did not reconcile; *risk* (§5.2, §5.4) ruled they serve different buyers. **Resolution: lead the
   team pitch with the Buildkite no-seat-tax math; lead the solo pitch with free-forever; never one
   page for both anchors.** *Resolved in favour of competitor's recommended lead.*

8. **Churn/conversion provenance.** *Unit-economics* used 2.0%/3.5%; *growth* refined to
   1.8%/3.0–3.5% with a 1.5× first-90-day multiplier; *risk* required the label. **Resolution: use
   growth's refined numbers; label every churn/conversion figure "ASSUMPTION: no product-specific
   data" in any spend-authorising document.** *Converged.*

9. **CLA + core license timing.** *Business-model* (§3.2–3.3) raised it; *risk* (§3.1–3.2) made it
   urgent. **Resolution: CLA bot live before the first external PR merges (G4); decide MIT vs
   Apache-2.0 before B0 (G3), Apache-2.0 recommended for its patent grant.** Hitting the d90
   contributor target without a CLA makes retroactive collection contentious. *Agreed.*

10. **Source-tree split.** *Business-model* proposed a `//go:build enterprise` build tag; *risk*
    (§2.1) accepted it with a caveat. **Resolution: build-tag approach (Grafana model) accepted;
    enforcement is runtime-only (license check), source is readable.** Mitigated because the gated
    features need real infrastructure a license bypass does not provide. *Accepted with caveat.*

---

## 9. Open questions for the maintainer

These are unmade decisions the team cannot make for you. They gate revenue actions above.

1. **Cooker Cloud — go or no-go?** (ADR-0004 decision A, deferred.) The single biggest fork. B0 +
   consulting do **not** need the answer; B1/B2 and the 6–8-week `tenant_id` investment fully
   depend on it. Do not let the Stripe ask pull you into tenancy work before this is "yes." (G12)

2. **OSS core license — MIT or Apache-2.0?** Strategy.md's HN draft says MIT; product-plan §7 says
   Apache-2.0. It is printed in the binary and README and must be consistent before B0. Apache-2.0
   is recommended (patent grant, matches Woodpecker/Argo/Tekton). Switching after the first
   external PR needs contributor sign-off — decide now, pair with the CLA. (G3, G4)

3. **OIDC-gate sign-off.** The team's binding recommendation moves basic OIDC to Explorer and gates
   only SSO group-map + MFA step-up at Crew — a deliberate change from launch-doc §1.4. Confirm
   before B0 ships. (G5)

4. **Cloud base price — $49, or $39 with 1,000 included build-minutes?** Resolve before any Cloud
   build (moot until Q1, gated on #1). $39 at 2,000 minutes leaves a thin 77% margin. (§3 note)

**ASSUMPTION (residual, owner = maintainer/ops):** active-install count (the conversion
denominator) requires telemetry opt-in or a Helm-pull proxy that does not exist yet — every
conversion-rate figure inherits that uncertainty until it is instrumented.
