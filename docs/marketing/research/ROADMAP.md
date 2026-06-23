# Cooker — GTM & Monetization Roadmap

> The single board for taking Cooker from launch to revenue, synthesized from the marketing &
> monetization research (`PLAN.md`, `monetization/monetization-plan.md`, the 4 channel docs, and the
> 10 monetization analyses). Detailed gates live in `launch-kit/launch-readiness-tracker.md`.
> Status: 🔴 not started · 🟡 in progress / staged · 🟢 done. Last updated 2026-06-21.

## The thesis
Cooker wins a **niche, not a feature war**: *a visual graph CI/CD editor + a real deploy story in one
self-hosted Go binary.* Adoption first (reputation → stars → optional revenue), honesty as the brand,
**self-hosted licensing + consulting before any hosted SaaS.**

## Phase 0 — Pre-launch readiness  🟡  (gate: don't announce until all 🟢)
- [ ] 🔴 **60-second hero cast** recorded + embedded (script: `launch-kit/hero-cast-script.md`) — **hard gate**
- [ ] 🔴 **docs.cooker.dev** live (MkDocs) — hosts `/compare/`, `llms.txt`, schema
- [ ] 🔴 GoReleaser **v0.1.0** binaries + Helm OCI chart published
- [ ] 🔴 `docker compose up` verified clean by a non-maintainer
- [x] 🟢 README rewritten (visual-first + "What's not done yet")
- [ ] 🔴 Security quick-wins landed (docker.sock S26-05-04, sslmode, default PG password)
- [x] 🟢 License decided (**Apache-2.0**) · 🟢 canonical sentence approved · 🟢 CONTRIBUTING + DCO

## Phase 1 — Launch week  🔴  (one cold-traffic shot; all copy is written)
- [ ] Mon — Show HN (`launch-kit/show-hn-post.md`) + 30-min comment-watch SLA
- [ ] Week — Mastodon → r/selfhosted → dev.to #1 → r/devops + r/kubernetes → **Product Hunt** + YouTube → r/golang → recap
- [ ] Product Hunt listing (`launch-kit/product-hunt-listing.md`) — claim the account ~30 days early
- [ ] Newsletter + podcast outreach (lists + pitches in `channels/announce.md`)

## Phase 2 — Grow adoption  🔴  (30/60/90 — target 1,000 stars + 5 external contributors)
- [ ] Publish the 5 `/compare/` pages (staged in `docs/compare/`); ship activation content first
- [ ] Seed GEO corpora (canonical sentence + `llms.txt`) → get cited by ChatGPT/Claude/Perplexity (today: **zero** presence)
- [ ] awesome-selfhosted / awesome-go / Artifact Hub listings
- [ ] Open Discord (day 30); merge the first external PR; weekly content cadence
- [ ] Keep paid search (**SEM**) OFF until ~day 180 and only if free→paid is proven

## Phase 3 — Monetize  🔴  (only after traction; each step is independent)
- [ ] **B0 self-hosted licensing** (offline Ed25519 keys, ~4.5 d, no blockers) — **the revenue spine**
- [ ] **Consulting / support** (first real cash, zero code)
- [x] 🟢 Sponsorship live (`.github/FUNDING.yml`) — signal only, $0–200/mo
- [ ] Pricing page (`launch-kit/pricing-page.md`) — gated on the OIDC-tier sign-off + Cloud price
- [ ] **Cooker Cloud** (Stripe) — gated on `tenant_id` (6–8 wk) + build-farm sandbox + pen-test + go/no-go

## Money model (summary)
| Tier | Self-hosted price | Includes |
|---|---|---|
| **Explorer** | $0 | 1 replica, 1 env, K8s + Fly + Render + **SSH**, **basic OIDC**, unlimited pipelines/runs/seats |
| **Crew** | $49 / replica / mo · unlimited seats | HA, all deploy targets, SSO group-map + MFA, managed secrets, audit |
| **Constellation** | custom | air-gapped, SLA, multi-tenant secrets |

Wedge: **no seat tax** (30-person team ≈ $450/mo on Buildkite vs **$49** on Cooker Crew).
Forecast: **bear ≈ $4K / 36 mo** (the honest planning baseline) · base ≈ $78K Y3 · bull ≈ $320K Y3.
Biggest swing variable: the **free→paid rate** (2% base / 0.5% bear). Cloud is upside, never the spine.

## Open decisions (yours — these gate the items above)
- [ ] **Cooker Cloud — go or no-go?** (B0 + consulting do not need this)
- [ ] **OIDC-tier sign-off** — move basic OIDC to the free tier (team's strong recommendation)
- [ ] **Cloud base price** — $49, or $39 with 1,000 included build-minutes

## Honest risks
Launch day is make-or-break (cold traffic happens once) · OSS-paywall backlash if monetization gets greedy ·
bus factor of one · all churn/conversion numbers are **assumption-only** until real cohort data exists ·
don't build Cloud speculatively before the go/no-go.

## Sources
`PLAN.md` · `monetization/monetization-plan.md` · `channels/{seo,sem,geo,announce}.md` ·
`launch-kit/launch-readiness-tracker.md` · `../strategy.md` · `../../product-plan.md` §7 · `../../launch/`
