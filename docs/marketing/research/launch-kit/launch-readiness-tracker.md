# Cooker — Launch-Readiness Tracker

> **CMO-owned.** One place to track everything that must be true before Cooker launches and before
> each revenue action. Consolidates: the **8 launch preconditions** (`docs/marketing/strategy.md` §4 /
> `channels/announce.md`), the **15 go/no-go gates** (`monetization/monetization-plan.md` §7 +
> `monetization/risk.md`), and the **open maintainer decisions**. Date: 2026-06-21.
> Status legend: 🔴 open · 🟡 draft ready in launch-kit (needs promote/decision) · 🟢 done.

---

## A. Launch preconditions — ALL must be 🟢 before announcing (strategy.md §4)

The hero cast is the single hard gate; no channel plan compensates for its absence.

| # | Precondition | Owner | Status | Notes |
|---|---|---|---|---|
| P1 | GoReleaser v0.1.0 binaries (linux/darwin × amd64/arm64, multi-arch image, checksums) | maintainer | 🔴 | `docs/shipping-go.md` 0–30d plan |
| P2 | Helm OCI chart published (`oci://ghcr.io/santapong/charts/cooker`) | maintainer | 🔴 | documented in README |
| P3 | **60-second hero cast** (recorded, captioned, embedded) | maintainer | 🔴 | **hard gate** — can't be produced by this team (needs recording) |
| P4 | `docker compose up` quickstart verified on a clean machine by a non-maintainer | maintainer | 🔴 | — |
| P5 | README rewritten (visual-first; cast/GIF in first viewport; demo URL above install) | CMO/maintainer | 🟡 | **draft ready:** `cooker-README.draft.md` |
| P6 | "What's not done yet" section in README | CMO/maintainer | 🟡 | in the README draft + `show-hn-post.md` |
| P7 | Security review Quick-Wins 1–6 landed (S26-05-04 docker.sock, S26-05-10 sslmode, S26-05-13 default PG pw) | eng | 🔴 | engineering, not marketing |
| P8 | Docs site live at `docs.cooker.dev` (MkDocs Material) | maintainer | 🔴 | hosts the `/compare/` pages, `llms.txt`, schema |

---

## B. Monetization go/no-go gates (monetization-plan §7 / risk.md)

### B0 self-hosted licensing + consulting — clear before launch / first revenue

| Gate | Requirement | Status | Launch-kit asset that moves it |
|---|---|---|---|
| G1 | Real monitored security contact + `/.well-known/security.txt` (replace `*.example.com`) | 🔴 | — (1-hour maintainer task) |
| G2 | ToS, Privacy Policy, **AUP** published (AUP must prohibit cryptomining/malware/DoS builds) | 🔴 | partly in `docs/legal/` already; AUP gap |
| G3 | OSS core license decided + consistent (README/binary/LICENSE) | 🔴 | **DECISION** — see §C |
| G4 | CLA bot live **before the first external PR merges** | 🔴 | urgent (d90 contributor target) |
| G5 | OIDC gate revised: basic OIDC → Explorer; only SSO group-map + MFA step-up at Crew | 🟡 | reflected in `pricing-page.md` (pending sign-off) |
| G6 | SSH deploy target moved to Explorer | 🟡 | reflected in `pricing-page.md` |
| G7 | Pricing page carries the no-seat-tax (Buildkite) comparison + the Coolify-delta answer | 🟡 | **draft ready:** `pricing-page.md` |
| G14 | Consulting contracts explicitly disclaim SLA in the solo phase | 🔴 | — |
| G15 | All churn/conversion figures in spend docs labelled "ASSUMPTION: no product data" | 🟢 | done throughout the research |

### Cloud (B1/B2) — none may be skipped before public signups (all gated on the §C #1 decision)

| Gate | Requirement | Status |
|---|---|---|
| G8 | `tenant_id` data model (ADR-0004 App. A) merged | 🔴 (6–8 wk) |
| G9 | Per-tenant build-farm isolation (gVisor/Kata) + external pen-test passed | 🔴 |
| G10 | GDPR right-to-erasure wired to `tenant_id` | 🔴 |
| G11 | DPA + SCCs offered to EU Cloud customers | 🔴 |
| G12 | **Cloud go/no-go decision made (ADR-0004 decision A)** | 🔴 **DECISION** |
| G13 | Stripe Checkout confirmed SAQ-A (no card field in DOM; webhook sig verified) | 🔴 |

---

## C. Open maintainer decisions (the team can't make these for you)

| # | Decision | Team recommendation | Gates it unblocks |
|---|---|---|---|
| 1 | **Cooker Cloud — go or no-go?** | Defer; ship B0 + consulting first. Don't build tenancy speculatively. | G8–G13, the whole Cloud lane |
| 2 | **Core license — MIT or Apache-2.0?** | **Apache-2.0** (patent grant; matches Woodpecker/Argo/Tekton) + CLA | G3, G4, README, `llms.txt`, all launch copy |
| 3 | **OIDC-gate sign-off** (move basic OIDC to Explorer) | **Approve** (avoids the SSO-tax backlash) | G5, finalizes `pricing-page.md` |
| 4 | **Cloud base price** — $49, or $39 with 1,000 included build-minutes | $49 (wider margin) — moot until #1 is "yes" | finalizes Cloud pricing |
| 5 | **Canonical citable sentence sign-off** (GEO) | Approve the drafted sentence (hard to retract once seeded) | unblocks GEO corpora seeding |

---

## D. First-30-day execution order (from PLAN.md §3.5 / strategy.md §5)

1. **Now (pre-launch):** decide license (#2) + OIDC gate (#3); promote the README draft; add SoftwareApplication
   schema + `llms.txt`; claim Product Hunt; record the hero cast (P3); write/verify the quickstart (P4).
2. **Launch week:** Show HN (Mon) → Mastodon → r/selfhosted → dev.to #1 → r/devops + r/kubernetes →
   Product Hunt + YouTube (Wed) → r/golang (Thu) → recap (Fri). Ship `cooker-vs-github-actions` compare page;
   PR to awesome-selfhosted + awesome-go + Artifact Hub.
3. **30/60/90:** activation content first, then the remaining 4 compare pages; seed GEO corpora; newsletters +
   podcasts; open Discord (day 30); measure free→paid. **Paid search stays off until day 180+.**

---

## E. What the launch-kit now provides (drafts in this folder)

| Need | Draft | Status |
|---|---|---|
| README rewrite (P5/P6) | `cooker-README.draft.md` | 🟡 promote to `/README.md` after license decision |
| Show HN + objections | `show-hn-post.md` | 🟡 |
| Product Hunt listing | `product-hunt-listing.md` | 🟡 |
| 5 `/compare/` pages (G7-adjacent, SEO/GEO) | `compare/*.md` | 🟡 promote to docs site `/compare/` |
| AI-citation assets | `llms.txt`, `llms-full.txt`, `what-is-cooker.md`, `software-application-schema.json` | 🟡 |
| Pricing page (G7) | `pricing-page.md` | 🟡 |
| Sponsorship | `FUNDING.yml`, `sponsors-tiers.md` | 🟡 promote `FUNDING.yml` to `.github/` |

> Promotion = copy the draft to its real location once the relevant §C decision is made. Nothing here
> overwrites the live README/docs until you choose to.
