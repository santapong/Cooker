# Cooker — Launch Kit

> Ready-to-use launch assets produced by the `cooker-mkt-*` team from the research in
> `../` (see `../PLAN.md` and `../monetization/monetization-plan.md`). Date: 2026-06-21.
>
> **These are DRAFTS.** Nothing here is live. "Promote" an asset = copy it to its real location
> once the relevant decision in `launch-readiness-tracker.md` §C is made. Until then your real
> `README.md`, docs site, and `.github/` are untouched.

## What's here

| Asset | File | Promote to | Blocked on (tracker §C) |
|---|---|---|---|
| **Project README rewrite** | `cooker-README.draft.md` | `/README.md` | license decision (#2) + hero cast (P3) |
| **Launch-readiness tracker** | `launch-readiness-tracker.md` | (keep here; it's the control doc) | — |
| **Show HN post + objections** | `show-hn-post.md` | post to HN on launch day | preconditions P1–P8 |
| **Product Hunt listing** | `product-hunt-listing.md` | Product Hunt (claim 30d early) | preconditions |
| **Comparison pages (×5)** | `compare/cooker-vs-*.md` | docs site `/compare/` | docs site live (P8) |
| **AI-citation: index** | `llms.txt` | `docs.cooker.dev/llms.txt` | canonical-sentence sign-off (#5) |
| **AI-citation: full** | `llms-full.txt` | `docs.cooker.dev/llms-full.txt` (auto-gen from README) | #5 |
| **"What is Cooker" page** | `what-is-cooker.md` | docs site | #5 |
| **Structured data** | `software-application-schema.json` | docs site `<head>` JSON-LD | — |
| **Pricing page** | `pricing-page.md` | docs/pricing page | OIDC sign-off (#3), Cloud price (#4) |
| **Sponsorship config** | `FUNDING.yml` | `/.github/FUNDING.yml` | create Open Collective (optional) |
| **Sponsor tiers** | `sponsors-tiers.md` | GitHub Sponsors profile | — |

## How to use it

1. Make the 5 maintainer decisions in `launch-readiness-tracker.md` §C (especially the license and the
   OIDC-gate — they unblock the most assets).
2. Close the 8 launch preconditions (§A) — the **hero cast (P3)** and **docs site (P8)** are the big ones
   the team can't produce for you.
3. Promote each asset to its real location (table above). Search every file for `{{...}}` placeholders and
   fill them (demo URL, Discord invite, license name, canonical sentence).
4. Follow the launch-week sequence in `../PLAN.md` §3.5 / `../channels/announce.md`.

## Honesty rules (inherited — non-negotiable)

Every asset already follows `../../strategy.md` §7: no astroturfing, no inflated numbers, no
"enterprise-ready"/multi-tenant claims while single-tenant, keep the core OSS, no star-begging. Keep it
that way when you promote them.

## Re-run / extend

Invoke `cooker-mkt-cmo` (or a specific specialist, e.g. `cooker-mkt-seo`) to refresh or extend any asset.
