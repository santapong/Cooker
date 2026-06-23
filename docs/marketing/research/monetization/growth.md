# Cooker — Growth & Funnel Analysis v1

> Author: growth / funnel analyst. Round 2, 2026-06-21.
> Spine docs: `docs/marketing/strategy.md` §5-§6, `docs/product-plan.md` §5,
> `docs/launch/01-billing-monetization.md` §3, `docs/marketing/research/monetization/segmentation.md`,
> `docs/marketing/research/monetization/pricing.md`, `docs/marketing/research/monetization/unit-economics.md`.
> Labels: `ASSUMPTION:` marks unverified claims. External benchmarks are dated.

---

## 1. The AARRR funnel

| Stage | Definition for Cooker | Key metric | Primary lever |
|---|---|---|---|
| **Acquisition** | GitHub star / GHCR pull / docs-site visit | Unique installs (GHCR pulls) | HN front page; r/selfhosted; README GIF above fold |
| **Activation** | First green pipeline run (all nodes complete, no failures) | Time-to-first-green-run (target: median ≤ 6 min — strategy.md §6) | `docker compose up` quickstart; empty-state CTAs (product-plan §5 Tier 2) |
| **Retention** | User still triggering runs at day 7, 30, 90 | D7 / D30 / D90 active-run rate | Webhook triggers; `cookerctl` CLI automation; git-push → pipeline (Tier 2) |
| **Revenue** | Explorer → Crew conversion (self-hosted license B0) | Free-to-paid rate; MRR | In-product upgrade prompts at real friction points (§2 below) |
| **Referral** | Organic word-of-mouth + OSS contributor path | External contributors with merged PR (strategy.md §6: target 5 by d90) | Public post-mortems; "good first issue" labels; honest docs |

### Why "first green run" is the right activation anchor

The strategy.md §6 survey metric is median time-to-first-run: 12 min at d30 → 6 min at d90. This is the product's own stated activation target, and it maps directly to the indie-hacker ICP's evaluation behaviour: they run `docker compose up`, drag two nodes, click Run, and either see green within minutes or they close the tab. No secondary onboarding event matters more than this one.

ASSUMPTION: "first green run" operationally means the first pipeline run that completes all stages with no failed nodes. This can be measured via run FSM terminal states in the Postgres `pipeline_runs` table without additional instrumentation.

---

## 2. Free-to-paid triggers — the actual gated features

The mockup FAQ binds two promises: unlimited pipelines/runs and unlimited seats on every paid tier. **These are inviolable; do not use them as upgrade triggers.** The real gates — confirmed by `01-billing-monetization.md §1.4` — are:

| Trigger moment | Gated entitlement | In-product signal | HTTP response |
|---|---|---|---|
| "Add Staging environment" (second env after Dev) | `max_environments: 1` on Explorer | 402 at `POST /environments` create path | "Upgrade to Crew to add Staging/Prod environments." |
| "Run multi-replica HA" (second Cooker process) | `max_replicas: 1` on Explorer | Soft dashboard warning on heartbeat; license check at startup | "A second replica requires a Crew license." |
| "Connect OIDC / enable MFA" | `feature.oidc_mfa: false` on Explorer | 402 when enabling OIDC in settings | "OIDC and MFA are available on Crew." [see §3 flag] |
| "Use Vault / AWS / GCP secrets backend" | `feature.managed_secrets: false` on Explorer | 402 at secrets-backend enable | "Managed secrets backends are a Crew feature." |
| "Extend run history beyond 7 days" | `retention_days: 7` on Explorer | Banner when user queries older runs | "Upgrade to Crew for 90-day run history." |
| "Enable cron triggers" | `feature.cron: false` on Explorer | 402 at schedule create | "Scheduled pipeline runs require Crew." |

**Upgrade prompt design (per launch doc §3 graceful over-limit UX):** Every prompt is a 402 `Payment Required` with a JSON body containing `upgrade_url` pointing to the Stripe Checkout session creation endpoint. The UI surfaces this as an inline CTA — never a modal blocker, never a lockout of existing runs. Existing pipelines keep executing; only the gated action is blocked. This matches §3's "degrade, never brick" posture and the brand rule against punitive UX.

The environment-limit gate is the highest-intent trigger. A user creating a second environment has demonstrably graduated from side-project to production workload. This is the segmentation analyst's S1→S2 conversion event.

---

## 3. Activation: reducing time-to-first-green-run

Current baseline: 12 min median at d30 (survey target). Target: 6 min at d90.

The gap lives in three places:

**Empty-state CTAs (product-plan §5 Tier 2, ~1 d effort):** A blank pipelines/apps/environments page is the highest-drop point. Replace every blank state with a "Create your first pipeline" CTA that pre-populates a two-node Build → Push template. The DAG editor opens with nodes already on canvas; user only sets the repo URL and registry. Eliminates the "where do I start" hesitation that costs 3–5 minutes.

**`docker compose up` quickstart:** Strategy.md §2 requires a working `docker compose up` quickstart tested on a clean machine before launch. This is the primary activation path for S1. Keep the compose file self-contained (no external dependencies beyond Docker); the goal is green terminal output in under 90 seconds, leaving the remaining time budget for the first pipeline run.

**`cookerctl` CLI (shipped — product-plan §5 Tier 2):** `cookerctl pipelines run --follow` exits with the run's exit code. This unlocks the "automate activation" path: a user can script their first run from a CI environment or a shell alias, removing the browser-first requirement. For S2 platform teams evaluating Cooker, this is the demo-to-adoption bridge.

**14-day Crew trial (pricing doc §4):** The trial starts on first license activation, not on account creation, giving the buyer time to reach the first green run before the clock starts. This is the right design for an infrastructure tool with a non-trivial setup surface.

ASSUMPTION: day-7 retention for dev tools running a PLG motion hovers around 30% (boldstart.vc devtools benchmark, 2023 — the most recent public benchmark for this category). That implies roughly 70% of users who install never return after a week. The empty-state CTAs and quickstart exist specifically to pull the median first-run time below the attention-span threshold that drives this drop.

---

## 4. Retention and the D7/D30/D90 cliff

Infrastructure SaaS has the lowest category churn — 1.8% monthly (Optif.ai B2B SaaS churn benchmark, 2026) — but 43% of all SMB losses concentrate in the first 90 days (mrrsaver.com, 2026). Cooker's retention architecture must address two distinct problems:

**Early-cohort survival (d0–d90):** The risk is not churn from a satisfied self-hosted install; it is abandonment before the first green run ever happens. The empty-state CTA and quickstart (§3) are the retention investment here.

**Post-activation stickiness (d90+):** Once a user has a pipeline running on a webhook trigger or cron schedule, switching cost is real. The `cookerctl` CLI, pipeline-as-code YAML export, and the AppDeployer webhook-to-deploy loop (product-plan §5 Tier 1, shipped) create the "it does my deploys automatically" habit that anchors retention. This is the infrastructure tool's natural flywheel: each automated deploy is a reminder that the tool is working without the user having to think about it.

**NRR loop:** Expansion within a Crew account comes from one of three vectors: (a) adding a second/third environment, (b) adding a second replica for HA, (c) a second Cooker instance for a different team within the same org. At $49/replica these are the natural expansion events. ASSUMPTION: SMB infrastructure SaaS median NRR runs 100–105% (digitalapplied.com NRR benchmarks, 2026); Cooker's per-replica billing structure should produce organic NRR in this range once a customer adds HA or a second environment server.

---

## 5. Conversion and churn estimates (for unit-economics and forecast)

These are the numbers unit-economics explicitly requested. Sources and assumption labels are attached to every figure.

### Free-to-paid conversion rate

| Scenario | Rate | Basis |
|---|---|---|
| OSS / self-hosted devtool, general | 0.5–3% | getmonetizely.com (2025): "open source SaaS companies typically see 0.5–3% conversion" |
| Devtool with freemium, bottom-up PLG | 2–7% | boldstart.vc devtools benchmark (2023); productmarketingalliance.com PLG article (2024) |
| Cooker conservative base case (d90) | **2%** | Lower bound of devtool PLG range; accounts for early-stage, low brand awareness, self-hosted friction |
| Cooker optimistic (d180, post-content flywheel) | **5%** | Mid-range of PLG devtool range, assuming quickstart reduces time-to-first-run to ≤6 min and SEO content compounds |

ASSUMPTION: conversion denominator is "active installs" — defined as a unique Cooker instance that has completed at least one successful pipeline run in the past 30 days. Raw GHCR pulls significantly overcount; ASSUMPTION (owned by product/ops): telemetry opt-in or a proxy metric (Helm pull rate × estimated deployment fraction) is needed to track active installs post-launch.

**Concrete implication for forecast:** At strategy.md's d90 target of 5,000 GHCR pulls and an assumed 20% pull-to-active-install rate (ASSUMPTION), the active base is ~1,000 installs. At 2% conversion: **20 paying Crew customers by d90**. At 5%: **50 customers**. These are the cohort-1 inputs for the forecast model.

### Churn rates

| Customer type | Monthly churn | Annual retention | Basis |
|---|---|---|---|
| Self-hosted Crew (infra tool, SMB) | **1.5–2.0%** | **79–84%** | Infrastructure SaaS median 1.8%/mo (Optif.ai, 2026); self-hosted binary has higher switching cost than SaaS → low end of range |
| Cloud Crew (if/when launched) | **3.0–3.5%** | **65–69%** | SMB SaaS median 3–5%/mo (mrrsaver.com, 2026); easier to cancel than a self-hosted binary |
| Early-cohort (d0–d90) survival adjustment | Apply a **1.5× churn multiplier** to first-90-day cohort | 43% of SMB churn in first 90 days (mrrsaver.com, 2026) | Use 3.0%/mo for d0–d90 self-hosted cohort; revert to 1.8%/mo after d90 |

These figures confirm unit-economics' provisional inputs (2.0% self-hosted, 3.5% Cloud) and sharpen them with the early-cohort adjustment. Hand 1.8%/mo steady-state to the LTV model; use 3.0%/mo for the first-90-day cohort survival curve.

### NRR estimate

ASSUMPTION: Cooker's per-replica expansion model should produce NRR in the range of 100–108% at steady state for self-hosted Crew. The expansion lever is replica count (HA adds a second replica = +$49/mo, +100% MRR per account). At an estimated 25% of Crew customers adding a second replica within 12 months, blended NRR ≈ 105–108%. ASSUMPTION (owner: unit-economics/forecast): validate this expansion rate assumption once first-cohort data exists; it is the key NRR driver given that seats are unlimited and cannot expand.

---

## 6. Referral and the OSS flywheel

Brand rules prohibit star-begging. The referral motion is organic and engineer-driven:

- **Contributors as the highest-value referral:** The strategy.md §6 metric of 5 external contributors with merged PRs by d90 is not vanity; each contributor represents a public endorsement in a community of peers who watch GitHub activity.
- **"Runs on Cooker" badges / blog posts:** When S2 teams adopt Cooker and write about it (engineering blog, r/devops post), this is the referral event. Provide a "Built with Cooker" badge in the docs for teams who want to display it — never mandate it.
- **Content virality path:** The strategy.md §3 comparison articles ("Cooker vs Woodpecker", "Cooker vs GitHub Actions for self-hosters") are the SEO-compounding referral surface. Each article captures a user mid-evaluation and converts them into an install, bypassing the HN spike dependency.

---

## Cross-team flags

- **unit-economics:** Use 1.8%/mo steady-state churn for self-hosted Crew LTV (replaces the provisional 2.0%); apply 3.0%/mo for the first-90-day cohort to model early abandonment. NRR of 105–108% is a reasonable planning assumption if replica expansion is tracked. Free-to-paid base rate of 2% conservative / 5% optimistic for d90 cohort-1 sizing. These are the numbers your LTV table needs.

- **forecast:** Conversion pipeline: 5,000 GHCR pulls × 20% active-install rate × 2–5% free-to-paid = 20–50 paying Crew customers by d90. At $49/mo and 87–92% gross margin these cohort-1 numbers produce low absolute MRR but healthy unit economics. Model two scenarios: 2% (conservative) and 5% (optimistic); do not blend them.

- **pricing:** The OIDC gate (Explorer: no OIDC) is the highest-risk free-to-paid trigger in the current entitlement table. It is a legitimate upgrade driver but generates OSS-backlash risk because OIDC is free in every adjacent tool (Gitea, Woodpecker, Forgejo). Recommendation: move basic OIDC to Explorer and gate SSO group→role mapping (the actual enterprise auth value) behind Crew. This lowers the conversion trigger intensity slightly but eliminates a PR-incident risk that could dominate the HN launch thread. The environment-limit gate is a cleaner, less controversial upgrade trigger that achieves the same S1→S2 inflection.

- **segmentation:** The S1→S2 conversion event is "creates a second environment." This is confirmed by the entitlement table and should be the primary in-product upgrade prompt. The secondary trigger is "enables OIDC" (subject to the OIDC-gate resolution above). The segmentation document's 3–5% conversion range from S1 base is consistent with this analysis (2–5% with early-stage discount applied).

- **seo/sem/geo/announce:** Activation content (the "docker compose up in 60 seconds" article, the quickstart walkthrough) is the highest-leverage SEO surface because it captures users at the exact moment they are deciding whether to continue. Prioritise this over comparison content in the first 30 days. Comparison content (Cooker vs GitHub Actions, vs Woodpecker) compounds in months 2–3. No paid acquisition before day 180 per strategy.md §8.

---

## Sources

- [B2B SaaS Churn Rate Benchmark — Optif.ai, 2026](https://optif.ai/learn/questions/b2b-saas-churn-rate-benchmark/)
- [SaaS Churn Rate Benchmarks 2026 — MRRSaver](https://www.mrrsaver.com/blog/saas-churn-rate-benchmarks)
- [Product benchmarks for dev tools — boldstart ventures, 2023](https://boldstart.vc/devtoolkit/an-alternative-to-nps-for-dev-tools/)
- [Open source to PLG: A winning strategy — Product Marketing Alliance, 2024](https://www.productmarketingalliance.com/developer-marketing/open-source-to-plg/)
- [SaaS Freemium Conversion Rates 2026 — First Page Sage](https://firstpagesage.com/seo-blog/saas-freemium-conversion-rates/)
- [Net Revenue Retention Benchmarks 2026 — Digital Applied](https://www.digitalapplied.com/blog/net-revenue-retention-benchmarks-2026-saas-expansion-data)
- [getmonetizely.com — Optimal conversion rate from free to paid in OSS SaaS, 2025](https://www.getmonetizely.com/articles/whats-the-optimal-conversion-rate-from-free-to-paid-in-open-source-saas)
- [B2B SaaS Trial-to-Paid Conversion Benchmarks 2026 — GrowthSpree](https://www.growthspreeofficial.com/blogs/b2b-saas-trial-to-paid-conversion-rate-benchmarks-2026-by-trial-type-acv-length-credit-card)
