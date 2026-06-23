# Cooker — Partnerships, Channels & Marketplaces (v1)

> Analyst: partnerships & channels. Round 1 draft. Date: 2026-06-21.
> Sources cited inline. All sponsorship figures treated as signal, not income (per product-plan §7).

---

## Framing: adoption-first, revenue later

Cooker has no paid SKU today. Every channel here is an **adoption channel** first; a revenue
channel only if/when a paid tier exists. The sequencing from product-plan §7 is the spine:
adoption → sponsorship (signal) → consulting → open-core. Marketplaces are listed under step 5
("distribution") — they compound adoption, not substitute for it.

Do not pursue paid marketplace listings (AWS/GCP/Azure commercial tiers) until the Crew ($49/replica)
or Constellation license exists. Running infrastructure for listing a free OSS binary through a paid
channel has a negative unit economics profile.

---

## 1. Free listing / adoption marketplaces

These are zero-cost to Cooker, carry no revenue share on the OSS tier, and each has a measurable
adoption audience.

### 1a. Artifact Hub (Helm chart)

| Dimension | Detail |
|---|---|
| What it is | CNCF-hosted registry for Helm charts, OCI artifacts, operators, plugins |
| Audience | Platform engineers and Kubernetes operators searching for ready-made charts |
| Revenue share | None. Artifact Hub is free to list on; it indexes any accessible Helm OCI repo |
| Effort | Low (~2 hours). Add `artifacthub-repo.yml` metadata file to the chart repo, register the OCI chart URL (`oci://ghcr.io/santapong/charts/cooker`) in Artifact Hub's web UI, request "Verified Publisher" badge |
| Gate | The Helm OCI chart must be published first (strategy.md §4 precondition: "Helm OCI chart published") |
| Adoption value | High. Helm is the standard install path for K8s-targeting users (persona 2); Artifact Hub is the first search destination. Comparable tools (Argo, Tekton, Woodpecker) are all listed there |
| Priority | **Ship immediately after v0.1.0 chart is published — same week** |

Source: [Artifact Hub](https://artifacthub.io/) / [CNCF project page](https://www.cncf.io/projects/artifact-hub/).

### 1b. awesome-selfhosted

| Dimension | Detail |
|---|---|
| What it is | Curated GitHub list (~200k stars). The primary discovery surface for persona 1 (indie hackers, r/selfhosted audience) |
| Audience | Solo devs and homelabbers — exactly the ICP launch target |
| Revenue share | None. PR to [awesome-selfhosted/awesome-selfhosted-data](https://github.com/awesome-selfhosted/awesome-selfhosted-data) |
| Effort | Low (~1 hour). Single PR, one item per submission. Category: "Software Development — Continuous Integration & Deployment" |
| Gate | Project must be actively maintained, have working install docs, and have been first released more than 4 months ago. Also requires a valid OSS license (MIT qualifies). Description under 250 characters. Language tag: Go |
| Adoption value | High. r/selfhosted (1.1M members) frequently links to and from this list; a listing here is persistent organic reach |
| Priority | **Ship at launch week (strategy.md §5 day 30 milestone: "awesome-list inclusion live")** |

Source: [awesome-selfhosted PR template](https://github.com/awesome-selfhosted/awesome-selfhosted/pulls).

### 1c. awesome-ci-cd / awesome-go

| Dimension | Detail |
|---|---|
| What they are | Curated lists targeting devops and Go developer audiences respectively |
| Effort | Low (1 hour each). separate PRs. awesome-go lists CLI tools and server projects |
| Adoption value | Medium. Lower traffic than awesome-selfhosted but reaches the Go contributor and DevOps communities — both relevant to persona 2 and contributor recruitment |
| Priority | **Week 3 post-launch (strategy.md §5 already lists these)** |

### 1d. DigitalOcean Marketplace — Kubernetes 1-Click App

| Dimension | Detail |
|---|---|
| What it is | DO Kubernetes 1-Click Apps are Helm charts submitted to DO's [marketplace-kubernetes](https://github.com/digitalocean/marketplace-kubernetes) GitHub repo, reviewed by DO's team, then listed with a "Deploy to DigitalOcean" button |
| Audience | DO's 600k+ developer customer base; DOKS (DigitalOcean Kubernetes Service) users |
| Revenue share | None on OSS listings. DO makes money on the DOKS cluster compute, not the app |
| Effort | Medium (~1–2 days). Submit PR to `digitalocean/marketplace-kubernetes` with a `src/<app-name>/` directory containing Helm values + install scripts + app metadata. DO team does a QA review pass. Contact: `one-clicks-team@digitalocean.com` |
| Gate | Cooker's Helm chart must be stable and chart+values must deploy cleanly on DOKS. The deploy guide (product-plan §6.3) already calls out DOKS as a supported target ("$50–75/mo" row). No Vendor Portal revenue share for free software |
| Adoption value | High. The "Deploy to DO" button is a meaningful conversion step for persona 1 users already on DO or shopping for a VPS. DOKS adoption is the persona-1 sweet spot |
| Priority | **Month 2 (after chart is stable on v0.1.x)** |

Source: [DO Marketplace docs](https://docs.digitalocean.com/products/marketplace/kubernetes-1-click-apps/), [marketplace-kubernetes repo](https://github.com/digitalocean/marketplace-kubernetes).

### 1e. Linode (Akamai) Marketplace — 1-Click App

| Dimension | Detail |
|---|---|
| What it is | Akamai/Linode's equivalent. Submission is a StackScript + metadata PR to [akamai-compute-marketplace/marketplace-apps](https://github.com/akamai-compute-marketplace/marketplace-apps) |
| Audience | Linode/Akamai's developer customer base (smaller than DO; cost-competitive VPS) |
| Revenue share | None on free listings |
| Effort | Medium (~1–2 days). Requires: StackScript or Helm chart, short description (100–125 words), support URL, technical docs, SVG logo assets and brand hex colours |
| Adoption value | Medium. Smaller reach than DO but incremental; Linode users skew to cost-conscious self-hosters matching persona 1 |
| Priority | **Month 3 (after DO listing is live and Cooker is stable)** |

Source: [Akamai Marketplace](https://www.linode.com/marketplace/), [GitHub repo](https://github.com/akamai-compute-marketplace/marketplace-apps).

### 1f. Vultr Marketplace — 1-Click App

| Dimension | Detail |
|---|---|
| What it is | Vultr's marketplace; vendor fills a verified-vendor form, Vultr contacts with next steps, image undergoes QA |
| Revenue share | Vultr explicitly does not require revenue sharing for OSS listings |
| Effort | Low-medium (~1 day) |
| Adoption value | Low-medium. Vultr's user base is smaller than DO/Linode; incremental reach |
| Priority | **Month 4+ (lowest priority of the VPS marketplaces)** |

Source: [Vultr Marketplace docs](https://docs.vultr.com/vultr-marketplace).

---

## 2. Paid cloud marketplaces — deferred

| Marketplace | Status | Gate |
|---|---|---|
| AWS Marketplace (AMI/container) | Deferred | AWS charges 20% revenue share on paid container/AMI listings. No fee for free/OSS listings, but there is no revenue to split yet. A free listing has negligible adoption value relative to effort (complex Seller registration, legal entity, bank details). Revisit when Crew ($49/replica) license key billing is live |
| GCP Marketplace | Deferred | Similar position. No IaC for GCP exists (product-plan §6.2 gap). Listing before GCP deployment story is documented is premature |
| Azure Marketplace | Deferred | Same reasoning. No Azure IaC exists |

ASSUMPTION (to unit-economics): AWS Marketplace commercial container listing rev-share is 20% of software charges for public offers, per Labra's 2025 cloud marketplace fee analysis. SaaS public offers are 3%. When Crew pricing is live, the effective take-rate significantly favours direct licensing (B0) over AWS Marketplace — flag to unit-economics for margin modelling.

Source: [Labra cloud marketplace fees 2025](https://labra.io/cloud-marketplace-fees-2025-aws-microsoft-azure-google-cloud-platform-revenue-shares-and-cost-saving-tips/).

---

## 3. Sponsorship and donations

Per product-plan §7: treat as **signal, not income**. Realistic early revenue $0–200/month.

| Platform | Tier structure | Effort | Notes |
|---|---|---|---|
| **GitHub Sponsors** | Up to 10 monthly tiers; suggested: $5 (supporter), $25 (backer), $100 (sponsor with README credit), $500 (partner with logo + priority issue label) | Low (~2 hours). Add `FUNDING.yml` | GitHub charges 0% fee for personal account sponsors; up to 6% for organizational sponsors. The README credit tier is the only one with a meaningful value exchange |
| **Open Collective** | Mirror tiers. OSC charges 10% fiscal hosting fee | Low (link from GitHub Sponsors once FUNDING.yml is set) | Useful for transparent financials if the project accepts corporate donors who need an invoice. Less relevant at launch |

ASSUMPTION: early-stage OSS CI tools with <1k stars typically receive $0–50/mo from GitHub Sponsors. The $0–200/mo ceiling from product-plan §7 is consistent with observed OSS funding patterns at this stage (sources: ry-ops.dev guide, December 2025; individual maintainer reports).

**Do not rely on sponsorship for any cash-flow planning. It is a signal of community health and nothing more.**

---

## 4. Affiliate and referral programs

Cooker's deploy guides recommend specific VPS providers. A referral link in the deploy docs is a
low-effort, no-ops way to recover some of the hosting cost of running a demo instance. It is not
a revenue channel — amounts are credits and small cash at early traffic volumes.

| Program | Structure | Notes |
|---|---|---|
| **DigitalOcean Affiliate** | 10% of referred user's monthly spend for up to 12 months, paid in cash via CJ Affiliate. Minimum $50 payout threshold. Example: referred user at $24/mo DOKS = $2.40/mo commission for up to 12 months ($28.80 total) | Embed referral link in `docs/launch/03-hosting-deploy.md` and the Helm quickstart. Track via CJ Affiliate dashboard. Apply at digitalocean.com/affiliates |
| **DigitalOcean Referral** (customer credit) | $25 credit to referrer after referred user spends their first $25 — account credit only, not cash | Lower value than affiliate; skip in favour of the affiliate program |
| **Hetzner Referral** | €10 credit to referrer once referee pays a ≥$10 invoice after using their €20 new-user credit. IMPORTANT: Hetzner has announced referral credits can only be accrued until 31 August 2026; redeemable until 31 December 2027. Program is sunsetting | Hetzner CX22/CPX31 is the recommended cost-optimal VPS in product-plan §6.3. Add referral link now before August 2026 sunset, but do not depend on it past 2026 |

ASSUMPTION (to forecast): at launch traffic levels (200–1k GHCR pulls/month), referral revenue will be approximately $0–30/month. Not material to any model; flag to forecast to zero this out.

Source: [DigitalOcean Affiliate Program](https://www.digitalocean.com/affiliates), [Hetzner Referral Program](https://www.hetzner.com/legal/referrals) (noting sunset date from [LowEndTalk thread](https://lowendtalk.com/discussion/216698/hetzner-stopts-with-their-referral-program)).

---

## 5. Ecosystem partnerships

These are alignment opportunities, not revenue channels. Each creates a distribution or credibility
signal.

| Partner type | Candidates | Value | Effort | Notes |
|---|---|---|---|---|
| **Registry vendors** | Docker Hub, Quay.io, GitHub Container Registry (GHCR), Harbor | Cooker already supports all OCI registries. No formal partnership needed; the integration is the partnership. A blog post or case study co-authored with the GHCR/Harbor team is plausible once usage is material | Low | GHCR is already the default in the docs; no action needed |
| **Secrets backends** | KeepSave (already integrated, SECURITY.md), HashiCorp Vault (integrated), AWS Secrets Manager (integrated), GCP Secret Manager (integrated) | KeepSave is a concrete OSS ecosystem partner — reach out post-launch for a co-blog post or "Cooker + KeepSave" quickstart. Vault has a large community blog audience | Low | ASSUMPTION: KeepSave team is reachable (their adapter is at `backend/internal/secrets/keepsave/`). No formal rev-share possible without a paid Cooker tier |
| **OIDC / IdP vendors** | Keycloak (OSS; UAT preset ships), Google OAuth (deploy guide), Okta/Auth0 (no integration yet) | Keycloak has an active ecosystem and conference presence (KubeCon). A "Cooker + Keycloak in 10 minutes" guide is useful content and earns inbound links from Keycloak's ecosystem page | Low | Defer Okta/Auth0 until persona 2 (SMB SaaS team) is the active focus |
| **Complementary OSS** | k3s/K3s docs, Coolify, Dokploy | Cooker coexists with (not replaces) these. Cross-linking in the `/compare/` docs earns reciprocal links. Strategy.md §1 is explicit: don't pick fights | Low | Coolify and Dokploy are the closest comparable traction stories; referencing them honestly is a brand asset |
| **KubeCon / CNCF ecosystem** | CNCF Sandbox application (long-term) | Artifact Hub listing puts Cooker in the CNCF orbit. Formal CNCF Sandbox application is a month-6+ decision, gated on contributor diversity and multi-tenant architecture (neither exists today) | Deferred | Do not apply to CNCF Sandbox until there are 3+ external contributors and the single-tenant IDOR is resolved |

---

## Priority sequencing (adoption value vs effort)

| Priority | Action | When | Effort |
|---|---|---|---|
| P1 | Artifact Hub listing | Launch week (after chart published) | 2 h |
| P2 | awesome-selfhosted PR | Launch week | 1 h |
| P3 | DigitalOcean Affiliate link in deploy docs | Launch week | 30 min |
| P4 | Hetzner referral link in deploy docs (before Aug 2026 sunset) | Launch week | 15 min |
| P5 | GitHub Sponsors / FUNDING.yml | Week 2 | 2 h |
| P6 | awesome-ci-cd / awesome-go PRs | Week 3 | 2 h |
| P7 | KeepSave co-blog post | Month 1–2 | 4 h |
| P8 | DigitalOcean Marketplace K8s 1-Click | Month 2 | 1–2 d |
| P9 | Linode Marketplace | Month 3 | 1–2 d |
| P10 | Vultr Marketplace | Month 4+ | 1 d |
| Deferred | AWS/GCP/Azure paid Marketplace listings | After Crew license billing is live | TBD |
| Deferred | CNCF Sandbox application | After 3+ external contributors + multi-tenant | TBD |

---

## Cross-team flags

- **To cooker-mkt-business-model**: the free-listing marketplaces (DO/Linode/Artifact Hub) are pure adoption channels with no revenue share. The first commercial marketplace listing opportunity (AWS/GCP/Azure) is gated on a paid SKU. AWS charges 20% on paid container/AMI listings vs 3% on SaaS — this makes per-replica licensing (B0 offline keys) more attractive than marketplace-distributed paid software. Flag this trade-off when sequencing B0 vs marketplace-commercial.

- **To cooker-mkt-announce**: the DO Marketplace "Deploy to DigitalOcean" button and Artifact Hub listing are **distribution** assets, not launch events. They should not be the headline of the Show HN post — they belong in the ecosystem/install section of the README and in month-2 follow-up content after the chart is stable.

- **To cooker-mkt-unit-economics**: sponsorship revenue is $0 at model time. Referral affiliate cash is $0–30/month at launch traffic — also zero for modelling. AWS Marketplace 20% rev-share vs direct B0 licensing economics should be modelled once Crew pricing is confirmed, since the delta is material at volume.

- **To cooker-mkt-forecast**: do not include marketplace revenue-share in any bear/base/bull scenario until a paid SKU is live. Sponsorship is a signal line, not a revenue line.

- **To cooker-mkt-risk**: Hetzner referral program sunsets August 2026. The dependency on Hetzner as the default recommended VPS (product-plan §6.3) is unchanged, but the referral link stops accruing credits in ~2 months. Flag this as a minor time-sensitive action, not a strategic risk.
