# SEM / Paid Search — Cooker

> Status: Round 1 draft. Author: SEM specialist, 2026-06-21.
> Scope: strategy only. No product code touched.

---

## Executive position

Paid search stays OFF until at minimum day 180 post-launch, per `strategy.md §8`. This is not a placeholder — it reflects a real constraint: open-source adoption is earned through community trust, not bought through clicks; Cooker has no paid tier available to purchase at launch; and CAC has zero meaning when LTV is $0. This document designs paid search as a **conditional, phased experiment** to be activated only when the triggers in §1 are all met.

---

## 1. Activation triggers (the unlock conditions)

Paid search should only be considered when **all three gates** are simultaneously true:

| Gate | Required signal | Earliest realistic date |
|---|---|---|
| **G1 — Revenue surface exists** | Self-hosted Crew/Constellation licenses (B0) are live and have closed at least 5 paid deals. There is nothing to send paid traffic to if no one can pay. | Day 180+ (B0 ships ~4.5 d of build work, but commercial velocity takes months) |
| **G2 — Conversion baseline proven** | Organic free-to-paid conversion rate is measured and non-zero. ASSUMPTION: unit-economics specialist will supply the LTV/CAC ceiling; SEM cannot set a CAC target without it. | After G1 + 60 days of data |
| **G3 — SEO displacement confirmed** | The SEO specialist has audited which high-intent terms Cooker already ranks for organically. Do not pay for keywords we win for free. | Running SEO audit, likely day 90–120 |

**Kill-switch:** Pause all paid campaigns immediately if (a) ROAS drops below 0.8x for two consecutive 14-day periods, (b) CAC exceeds the ceiling set by unit-economics specialist, or (c) a new competitive offer changes the keyword landscape materially. Kill-switch is not a failure; it is the expected default if the market remains unconverted.

---

## 2. Campaign structure (design now, activate later)

### 2.1 Branded campaigns

| Campaign | Purpose | Budget priority |
|---|---|---|
| Brand: "cooker" + "cooker ci" + "cooker cd" | Defend brand name from competitor conquesting. Very low CPC ($0.50–$1.50 ASSUMPTION; branded terms are almost always cheapest). | Low — only activate if a competitor bids on "cooker" first |
| Brand: typos + variants | "cookerci", "cooker.dev", "cooker pipeline" | Same as above |

ASSUMPTION: branded CPC will be well below $2. If Coolify or another competitor begins bidding on "cooker", activate immediately with a $200/mo cap.

### 2.2 High-intent non-branded (the primary test campaign)

These are the terms that indicate someone is actively shopping for a solution. Defer to SEO for any term where Cooker achieves a first-page organic rank.

| Ad group | Example terms | Rationale |
|---|---|---|
| Self-hosted CI/CD | "self hosted ci cd tool", "open source cicd kubernetes", "self hosted pipeline builder" | Direct match to Cooker's positioning; likely low volume, high intent |
| Visual pipeline / graph CI | "visual ci cd pipeline", "drag drop pipeline editor", "pipeline dag ui" | Owns the differentiator; very low search volume but near-zero competition |
| Self-hosted PaaS comparison | "coolify alternative", "dokploy alternative", "capRover ci cd" | Adjacent audience already primed for self-hosting; Cooker adds CI they lack |
| Competitor + CI | "coolify ci pipeline", "woodpecker ci alternative" | Captures users frustrated by gap in competitor offerings |

ASSUMPTION: "coolify alternative" and similar terms likely sit at $2–$5 CPC in 2026 based on low advertiser density in the self-hosted PaaS niche. Cross-industry B2B SaaS non-branded median was $5.34 in mid-2025, rising to ~$8.50–$14 for more competitive tech verticals (WordStream 2025 benchmarks; Dreamdata B2B benchmark Q2 2025).

### 2.3 Competitor-conquesting campaigns (separate, optional)

Bidding on competitor brand names (Woodpecker CI, Drone CI, Coolify) is legal but carries ethical and cost considerations:

- **Ethics:** Acceptable practice if ad copy does not disparage or mislead. Cooker's brand rules prohibit "picking fights." Ad copy must be factual: "Open-source CI/CD with a visual graph editor — MIT licensed, single binary."
- **Cost:** Branded terms for established competitors carry significant quality score penalties (your ad is less relevant to "Woodpecker CI" than Woodpecker's own ad). Expect CPC 2–5x non-brand CPC, or $10–$25+ per click. ASSUMPTION: based on general competitor conquesting patterns; no Cooker-specific data exists.
- **Recommendation:** Treat conquesting as a Phase 2 test, after non-branded campaigns prove positive ROAS. Budget cap: $500/mo maximum. Pause immediately if CTR < 0.5%.

---

## 3. Budget and bidding

### 3.1 Starter scenario (Phase 1, month 7–9)

ASSUMPTION: LTV figures from the unit-economics specialist are required before these budgets are confirmed. The table below assumes a Crew self-hosted deal at $49/replica/mo, typical 12-month retention, and a 3-replica average deal size — yielding a rough LTV of ~$1,750. A CAC ceiling of 20–30% LTV (industry rule of thumb) implies maximum CAC of $350–$525.

| Campaign | Monthly budget | Target CPC | Est. clicks/mo | Target conversion to trial | Target CAC |
|---|---|---|---|---|---|
| Brand defense | $200 | $0.75 | ~267 | n/a — defense | n/a |
| High-intent non-branded | $1,500 | $5–$8 | ~188–300 | 3–5% to free install | Ceiling set by unit-economics |
| Competitor conquesting | $0 (Phase 2) | — | — | — | — |
| **Phase 1 total** | **$1,700/mo** | — | ~450–550 | — | — |

ASSUMPTION: "3–5% conversion to free install" follows B2B SaaS non-branded conversion benchmarks (Dreamdata 2025 data suggests ~2–4% non-branded CVR for infrastructure/DevTools). Free install is the first conversion event, not payment.

### 3.2 Bidding approach

- Start with **manual CPC** (not Target CPA/ROAS) for the first 60 days to accumulate conversion signal before handing to automation. Google's smart bidding requires 30–50 conversions/month in a campaign to work reliably.
- Use **phrase match** initially; avoid broad match until negative keyword list is mature. Developer-tools searches attract irrelevant query expansion rapidly.
- Key negatives from day one: "github", "gitlab", "jenkins", "azure devops", "bitbucket", "jenkins job", "free github actions".

### 3.3 CPC context (sourced, dated)

| Source | Date | Data point |
|---|---|---|
| WordStream 2025 Google Ads Benchmarks | 2025 | Avg CPC across all industries: $5.26; software/tech category: $5–$9 |
| Dreamdata B2B non-branded benchmark | Q2 2025 | Non-branded B2B SaaS median CPC rose to $5.34, up 29% YoY |
| GrowthSpree SaaS benchmarks 2026 | 2026 | DevTools SaaS non-branded median: $8.50–$14.00 |
| Cross-industry Q1 2026 (multiple sources) | Q1 2026 | Cross-industry average reached $2.96 for Search; tech B2B significantly above this |

Developer tooling keywords occupy a mid-range slot. "Self-hosted CI/CD" style terms are likely at the lower end ($4–$8) because the advertiser pool is thin. "Kubernetes CI/CD" terms may be at the higher end ($10–$15) due to cloud-vendor competition.

---

## 4. Landing pages and conversion tracking

### 4.1 Dedicated landing pages (one per campaign, not the homepage)

| Campaign | Landing page URL | Primary CTA | Messaging hook |
|---|---|---|---|
| High-intent non-branded | `/lp/self-hosted-cicd` | "Get started free — single binary install" | "CI/CD you can see — drag, drop, deploy. MIT-licensed, runs on your infra." |
| Coolify-adjacent | `/lp/coolify-alternative` | Same | "Self-hosted PaaS + a visual pipeline editor. What Coolify doesn't do — built in." |
| Brand defense | `/lp/cooker-home` (or redirect to homepage) | Same | Redundant since user is already searching for Cooker |

Each LP must: (a) show the hero cast above the fold, (b) have no site navigation (reduces distraction/bounce), (c) carry explicit "MIT-licensed, no hosted version, self-hosted only" language to filter non-ICP traffic before a click converts.

### 4.2 Conversion events (what we track)

| Event | Type | Value |
|---|---|---|
| `lp_cta_click` — "Get started free" | Micro | $0 — awareness only |
| `docs_quickstart_visit` | Micro | $0 — engagement signal |
| `ghcr_pull` (inferred via UTM → Plausible) | Primary | $0 — free install, the real intent signal |
| `license_inquiry_form` | Lead | ASSUMPTION: assign $50 estimated value until real conversion data exists |
| `crew_license_purchase` | Revenue | Actual Stripe revenue (once B0 ships) |

Track via Google Ads conversion tags + Plausible (already in the marketing stack) with parallel measurement. Do not rely on Google's modelled conversions for the first 90 days of paid activity.

### 4.3 UTM scheme

```
utm_source   = google
utm_medium   = cpc
utm_campaign = [branded|nonbrand-selfhosted-cicd|nonbrand-coolify-alt|conquesting-{competitor}]
utm_content  = [ad-variant-id]
utm_term     = {keyword}   ← auto-inserted by Google Ads ValueTrack
```

All UTM parameters flow into Plausible analytics. Map `utm_campaign` values to cost-centre codes so spend reconciles cleanly against Stripe revenue by campaign. ASSUMPTION: Plausible is the agreed analytics tool from strategy.md §8; if another tool is chosen, the UTM scheme is platform-agnostic.

---

## 5. ROAS / CAC targets

ASSUMPTION: this entire section is provisional pending input from the unit-economics specialist.

Using the indicative numbers from `01-billing-monetization.md`:
- Crew self-hosted: $49/replica/mo
- ASSUMPTION: avg 2 replicas, 14-month retention = LTV ~$1,372

A 12-month payback window (the CAC ceiling that is reasonable for a self-funded project) implies:
- **Maximum CAC: ~$343** (25% of LTV as a conservative bound)
- ROAS target is not meaningful for a free-trial → paid funnel; the correct metric is **cost-per-free-install** as the primary leading indicator, then **cost-per-paid-conversion** as the lagging indicator once B0 has been live for 60+ days.

**Minimum ROAS kill-switch:** if 90-day paid revenue / 90-day spend < 1.0 (i.e., gross negative), pause and reassess. Set this as an automated rule in Google Ads.

---

## Cross-team flags

- **Unit-economics specialist (cooker-mkt-unit-economics):** The CAC ceiling in §3.1 and §5 is a placeholder. SEM cannot set kill-switch thresholds or budget ceilings without LTV and payback-period inputs from unit-economics. This is the single most load-bearing dependency in this entire doc. Flag to reconcile in Round 2.

- **Pricing specialist (cooker-mkt-pricing):** Paid only makes sense if a purchasable offering exists. The B0 self-hosted license (shipping first, per `01-billing-monetization.md §6`) is the minimum viable commercial surface. Confirm B0 is live before G1 trigger is considered met.

- **SEO specialist (cooker-mkt-seo):** Before activating any non-branded campaign, the SEO team must supply a list of terms Cooker ranks for organically (page 1). We do not pay for what we win for free. Cross-reference the high-intent non-branded term list in §2.2 against organic rankings at activation time. Defer owned terms to SEO. This is a hard constraint, not a courtesy.

- **Segmentation specialist (cooker-mkt-segmentation):** Audience signals from segmentation should inform keyword grouping and LP messaging. The indie-hacker persona (Persona 1) is Explorer/free; paid spend targeting them yields no immediate revenue and should be excluded from paid campaigns. Paid should target Persona 2 (SMB platform team) who can justify a Crew license. Confirm persona-to-campaign mapping in Round 2.

- **CMO / orchestration:** Strategy.md §8 prohibits paid before day 180. This document honours that. If the CMO decides to move the trigger date earlier (e.g., because B0 ships fast and conversion proves quickly), the constraint is a policy call, not a SEM call — flag it explicitly before activating.
