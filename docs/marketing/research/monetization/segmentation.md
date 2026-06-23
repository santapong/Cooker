# Cooker — Customer Segmentation & ICP Analysis

> Round 1 draft. Author: segmentation analyst. Date: 2026-06-21.
> Spine: `docs/audits/W11-user-journeys.md` (canonical personas), `docs/marketing/strategy.md` §1, `docs/launch/01-billing-monetization.md` §1.
> Labels: `ASSUMPTION:` marks unverified claims. All external benchmarks are dated.

---

## 1. Four segments in one view

| # | Segment | Org size | Self-hosted infra | Decision-maker | Buying power | Tier mapping |
|---|---|---|---|---|---|---|
| **S1** | Indie / solo dev | 1 | VPS / k3s | Themselves | $0–$20/mo personal card | Explorer (free-forever) |
| **S2** | SMB SaaS platform team | 20–150 | k3s / EKS / GKE | Lead platform engineer | $500–$2 k/mo ops budget | Crew ($49/replica/mo) |
| **S3** | Growth-stage platform team | 100–400 | EKS / GKE multi-env | Platform engineering lead or VP Eng | $2 k–$10 k/mo | Crew → Constellation |
| **S4** | Enterprise SRE / platform eng | 500+ | Multi-cluster, air-gapped | Procurement + SRE lead | $10 k+/yr contract | Constellation (custom) |

**S4 is explicitly NOT the launch audience** (Cooker is single-tenant; `S26-05-09` IDOR is documented). Constellation is the eventual destination, not the day-0 ICP.

---

## 2. Segment profiles

### S1 — Indie / solo dev

**Size:** globally, hundreds of thousands of developers run side-projects on a single VPS or k3s node. The r/selfhosted community alone has 1.1M members (strategy.md §3).

**Pain:** "I've written this CI config four times already." Wants `git push → live URL` in five minutes, with zero YAML. No platform team behind them — they are the platform team. Time-cost is the scarce resource, not money.

**Buying power:** $0–$20/month total ops budget (W11 persona 1 confirms "$0–$10/month for everything"). The limiting factor is personal wallet tolerance, not company approval.

**Decision-maker:** themselves. No procurement cycle, no committee.

**What triggers evaluation:** a viral HN post, a Mastodon share, a r/selfhosted thread. They try it the same evening.

**Buyer vs. user:** they are the same person. No separation between the technical evaluator and the check-writer.

**Free-forever rationale:** S1 is the adoption engine. Stars, GHCR pulls, word-of-mouth, and OSS contributors come primarily from this segment. Monetizing S1 early collapses the flywheel. This is consistent with strategy.md §1 ("PRIMARY persona for launch — do NOT monetize hard") and product-plan §7 ("adoption → sponsorship → consulting → open-core").

---

### S2 — SMB SaaS platform team (the Crew buyer)

**Size:** ASSUMPTION: (owner: market-sizing). Rough proxy: ~300 k–600 k engineering orgs globally with 20–150 headcount; even a 0.1% addressable share is 300–600 potential paying instances.

**Pain:** multi-environment promotion is manual click-ops; approvals are ad-hoc; importing 8+ repos into GitHub Actions is a repeated YAML tax. The W11 persona-2 walkthrough names the specific friction points — no bulk import, unintuitive App-vs-Pipeline model, no secret diff view.

**Buying power:** $500–$2 k/month is within a lead platform engineer's discretionary budget or requires one Slack message to a manager. A single Crew replica at $49/month is trivially below this threshold.

**Decision-maker:** the lead platform engineer or a senior SRE who holds a small ops-tooling budget. They self-evaluate, run a proof-of-concept, and approve without going to procurement.

**What triggers purchase:** (a) the team is already using Explorer and hitting the single-replica or OIDC/RBAC wall; (b) a compliance review asks for auditable approval gates (SOC 2 Lite); (c) headcount passes ~20 and ad-hoc deploys become a coordination problem.

**Buyer vs. user:** same person evaluates and pays. The lead engineer installs Cooker, demos it to the team, and submits the credit card. This is the bottom-up OSS motion in its cleanest form.

**Tier:** Crew ($49/replica/mo). One replica is sufficient for this cohort; HA requires two replicas at $98/mo — still inside budget.

---

### S3 — Growth-stage platform team

**Size:** ASSUMPTION: (owner: market-sizing). Narrower than S2; perhaps 10–15% of the S2 pool that has crossed a growth inflection.

**Pain:** S2's problems at scale. Multi-cluster, multiple engineering squads, a first compliance audit. They need the full Crew feature set plus early-stage Constellation features (KeepSave multi-tenant secrets, SSO group→role map). They are the natural upgrade path from Crew.

**Buying power:** $2 k–$10 k/month ops tooling; company credit card; may need manager sign-off but not procurement.

**Decision-maker:** VP Engineering or Platform Engineering lead. More consensus required than S2 but still a fast cycle (days, not months).

**Tier:** Crew (2–4 replicas = $98–$196/mo) trending toward Constellation on annual contract. ASSUMPTION: (owner: pricing) the gap between Crew top-end and Constellation custom may need a bridging SKU at ~$200–$400/mo as the segment matures — hand to `cooker-mkt-pricing`.

---

### S4 — Enterprise SRE / platform engineering

**Size:** Hundreds of qualifying orgs globally. Most are already committed to Harness, Argo, or GitHub Enterprise. Entry requires multi-tenancy, SAML, and a pen-test report — none of which Cooker has today.

**Pain:** existing enterprise CI/CD tools are YAML-first, opaque, and require months of onboarding. Cooker's visual graph is genuinely differentiated here — but only matters after the compliance gates clear.

**Buying power:** $10 k–$100 k+/yr on annual contract. Procurement cycle 3–9 months.

**Decision-maker:** SRE lead + security team + procurement. The technical evaluator (platform engineer) is not the same person as the budget holder.

**Buyer vs. user distinction (critical):** this is where user ≠ buyer diverges most. The engineer who evaluates Cooker has no purchasing authority. The procurement manager who signs the contract has never seen the product. Marketing to enterprise means a two-track motion: technical credibility for the evaluator, compliance/SLA documentation for the buyer.

**Tier:** Constellation (custom pricing, annual contract).

**Current status:** gated on (a) multi-tenancy (`tenant_id`, ADR-0004, ~3 wk unbuilt), (b) external pen-test, (c) SAML support. **Do not build enterprise-specific features speculatively.** The right signal to invest in S4 is inbound interest after S2 traction exists.

---

## 3. Willingness-to-pay by segment

| Segment | WTP range | Evidence / grounding | Tier |
|---|---|---|---|
| **S1 indie** | $0/mo (firm ceiling) | W11 persona 1: "$0–$10/month for everything"; self-hosted CI/CD alternatives (Woodpecker, Drone) are free as software. Source: [CI/CD Pricing 2026](https://cicdcalculator.com/self-hosted) | Explorer |
| **S2 SMB team** | $49–$200/mo | A self-hosted VPS+CI stack costs ~$60/mo (source: [agentdeals.dev CI/CD pricing 2026](https://agentdeals.dev/ci-cd-pricing)); Crew at $49/replica positions below that baseline. B2B median ARR per customer $50–$249/mo (ChartMogul Jan 2026). | Crew |
| **S3 growth-stage** | $200–$600/mo | ASSUMPTION: (owner: pricing/market-sizing). Extrapolated from S2 ceiling × replica count + feature premium. | Crew top-end / Constellation entry |
| **S4 enterprise** | $10 k–$100 k/yr | ASSUMPTION: (owner: pricing). Comparable: Buildkite Enterprise, Harness CI, Drone Enterprise all land in this range at 50–500 seat equivalents. | Constellation custom |

**External benchmark note:** Free-to-paid conversion for devtools OSS with bottom-up motion runs 2–7%, with exceptional products hitting 7%+ (getmonetizely.com, 2025). At a projected S1 user base of 1 k–5 k active installs, a 3–5% conversion to S2/Crew implies 30–250 paying instances — a realistic early-revenue band. ASSUMPTION: (owner: market-sizing to validate the active-install projection).

**Per-replica billing note:** the $49/replica model matches the mockup's binding FAQ promise and avoids the "seats" argument that generates the most SaaS pricing complaints. It is the right mechanic for the S2 buyer who thinks in infrastructure units, not headcount.

---

## 4. Who NOT to monetize (the free-forever segments)

| Segment | Rationale |
|---|---|
| **S1 indie / solo** | The adoption engine. Stars, word-of-mouth, OSS contributors, and Reddit/HN credibility all flow from this cohort. Paywall risk: kills the flywheel before it starts. Free Explorer forever is the correct answer. |
| **Open-source contributors** | Contributors who self-host for development are S1-adjacent. Any friction to spinning up a dev instance reduces the contributor pipeline. |
| **Student / educational installs** | ASSUMPTION: low materiality at launch, but worth naming for the pricing team. Adds stars and long-tail goodwill; zero monetization upside at launch scale. |

Cooker's OSS ladder (product-plan §7): adoption → sponsorship → consulting → open-core. Sponsorship ($0–$200/mo signal, e.g. GitHub Sponsors) is the first monetization surface for S1 — not a paid tier. Do not gate core CI/CD loop features on any paid tier at any scale; the license design in `01-billing-monetization.md` §4.2 confirms this correctly.

---

## 5. Buyer lens on W11 personas (extension)

| W11 persona | W11 user goal | Buyer extension: who holds budget | Purchase trigger | Tier |
|---|---|---|---|---|
| **P1 Solo dev** | `git push → live URL` in 5 min | Themselves (personal card or nothing) | Never; Explorer free-forever | Explorer |
| **P2 SaaS platform team** | Dev→Staging→Prod with one approval click | Lead platform engineer; no procurement cycle | Replica cap hit; OIDC/RBAC wall; first SOC 2 audit | Crew |
| **P3 Enterprise SRE** | Hard tenant boundaries + Vault + multi-cluster | SRE lead + procurement committee | Compliance mandate; replacement of a legacy CI tool | Constellation (future) |
| **P4 ML engineer** | GPU build cache + long-deadline runs | ML platform engineer or VPE (shared with P2 org) | Build-time pain after S2 Crew adoption; not a standalone ICP | Crew (same org as P2) |

P4 (ML engineer) is not an independent segment at launch. They exist inside S2/S3 orgs. Market to the platform team (P2); the ML engineer is a user, not the buyer.

---

## Cross-team flags

- **`cooker-mkt-growth`:** S1 is the top-of-funnel. All growth funnels should optimize for S1 → active Explorer install → organic conversion to S2 (not for direct S1 monetization). The moment a S1 user adds a second team member or hits `max_replicas=1`, that is the S2 conversion trigger — consider surfacing a contextual upgrade CTA at that friction point.

- **`cooker-mkt-pricing`:** the S3 growth-stage segment creates a gap between Crew ceiling (~$200/mo) and Constellation (custom). ASSUMPTION: an optional bridging SKU or a "Crew HA" price point (~$250–$400/mo flat for 2–4 replicas) may reduce churn at S2-top before they are ready for a Constellation sales cycle. Hand this segment profile to pricing for WTP validation.

- **`cooker-mkt-seo` / `cooker-mkt-sem`:** S1 searches organically ("self-hosted CI/CD single binary", "Woodpecker alternative", "Coolify CI/CD"). S2 searches by symptom ("Kubernetes multi-environment promotion", "SOC 2 CI/CD approval gates"). Two distinct keyword clusters; content strategy should not conflate them. The S1 cluster drives stars; the S2 cluster drives Crew trials.

- **`cooker-mkt-geo`:** S4 has geography-specific compliance requirements (EU data residency, FedRAMP for US public sector). Do not market Constellation to regulated verticals until `tenant_id` + external pen-test + air-gap certification exist. Premature S4 marketing creates a support/credibility liability.

- **`cooker-mkt-announce`:** launch messaging must hit S1 and S2 simultaneously. The HN post and r/selfhosted angle targets S1; the r/devops and r/kubernetes angles target S2. Do not use enterprise language ("enterprise-ready", "team-isolated") in any launch copy — `S26-05-09` IDOR makes those claims false today.

- **`cooker-mkt-market-sizing`:** reconcile: (a) the S2 addressable pool (20–150-person orgs running self-hosted K8s); (b) the 0.1% penetration assumption that yields 300–600 paying instances; (c) the 3–5% free-to-paid conversion rate applied to the S1 installer base. These three inputs need to converge on an "obtainable segment" number before the pricing team commits to a revenue model. The S1 installer base projection is the most uncertain input — it depends entirely on launch channel performance.

- **`cooker-mkt-pricing`:** the per-replica billing axis is already locked by the pricing mockup FAQ ("You're billed per running Cooker process, not per user"). This document takes that as binding. Any deviation requires a change to the mockup copy and a reconciliation with the billing design in `01-billing-monetization.md` §1.2 before this segmentation can be updated.
