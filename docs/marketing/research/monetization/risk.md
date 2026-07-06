# Cooker — Monetization Risk & Compliance Assessment v1

> Analyst: monetization risk, licensing & compliance.
> Round 3 (red-team), 2026-06-21.
> Spine: `docs/product-plan.md` §7, `docs/launch/01-billing-monetization.md` §6–§7,
> `docs/launch/04-security-compliance-legal.md`, `docs/marketing/strategy.md` §7.
> All nine peer drafts reviewed in full before this document was written.

---

## 1. OIDC Gate: Adjudication

Three peer drafts (pricing, segmentation, growth) all flagged gating OIDC behind Crew as the
highest OSS-backlash risk. Growth proposed the specific fix: move basic OIDC to Explorer and gate
only SSO group→role mapping behind Crew. This is the right call. Here is the ruling and the basis.

**Background.** The [SSO Wall of Shame](https://sso.tax/) and [SSOtax.org](https://ssotax.org/why)
document the established community norm: charging for OIDC/SSO is treated as a "security ransom,"
not an enterprise feature. Tailscale reversed their SSO paywall in 2024 after community pressure,
publicly admitting "the SSO tax felt like a mistake." CISA's "Secure by Design" whitepaper states
security features should not be paywalled. Every adjacent OSS tool (Gitea, Forgejo, Woodpecker CI)
ships OIDC free. This is a load-bearing community norm; violating it is the fastest way to make a
launch-week HN thread about the paywall rather than the product.

Gating OIDC behind Crew also directly contradicts `docs/product-plan.md` §7's ladder: "adoption
first" — the OIDC gate does not protect revenue, it protects against adoption by anyone who takes
auth seriously. The brand rule in `docs/marketing/strategy.md` §7 prohibits overselling
auth/security while simultaneously gating the auth feature that enables basic security hygiene.

**Recommendation (binding for the lead).** Move `feature.oidc_mfa` (basic OIDC login) to
Explorer. Gate `feature.sso_group_map` (SSO group→role mapping, the actual enterprise value that
requires the maintainer to maintain mapping logic and support) behind Crew. Gate step-up MFA behind
Crew. Update `docs/launch/01-billing-monetization.md` §1.4 entitlement table accordingly before
B0 ships.

**Conversion impact.** Mild but not zero. The second-strongest conversion trigger is now
"add Staging environment" (hits the `max_environments: 1` Explorer cap). That is a cleaner,
unambiguous upgrade trigger: a user who hits it has demonstrably graduated from a side project to
a production workload. It is also harder to resent than an auth paywall.

---

## 2. Other OSS/Community Risks

### 2.1 Source-tree split ambiguity

The business-model draft proposes a Go build-tag approach (`//go:build enterprise`) rather than a
separate module. ASSUMPTION: this is the right pragmatic choice for B0 scale, but it carries a
risk the draft does not fully name. A build tag means the enterprise features exist in the same
Git repository and are readable by anyone who clones the repo. For OSS-community trust this is
fine (it is the Grafana model). However, it means:

- A determined user can compile the enterprise binary without a license. The enforcement is the
  license check at runtime, not the source being hidden.
- Potential for "license-check bypass" patches circulating, which generates maintainer burden and
  adversarial community dynamics.

The mitigating factor is that Cooker's features (HA, SSO group mapping, secret backends) require
real infrastructure; bypassing a license check does not give you a Vault cluster. The risk is low
but should be named in the open-core source decision.

A separate module would be cleaner legally but adds build complexity for a solo maintainer at
this scale. The build-tag approach is accepted; note the runtime-only enforcement.

### 2.2 Entitlement table conflicts with "unlimited" brand promise

The pricing draft correctly identifies that the mockup FAQ promise ("unlimited pipelines and runs,
unlimited seats") is binding. Several entitlement rows in `01-billing-monetization.md` §1.4 are
not in conflict with this promise, but one is subtle: `allowed_deploy_targets` on Explorer is
`{k8s, fly, render}`, excluding SSH, ECS, and Cloud Run. This creates a scenario where a solo dev
on a VPS who wants SSH deploy (the most primitive deploy target) hits a Crew paywall. SSH deploy
is simpler to implement than K8s deploy; gating it behind Crew looks punitive and will generate
community criticism. This was not flagged by any peer.

**Recommendation.** Move SSH deploy target to Explorer. Keep ECS and Cloud Run behind Crew (they
signal a production AWS/GCP workload with budget). SSH is the cheapest deploy target; removing it
from the free tier will look like a cash grab to the self-hosted community.

---

## 3. Licensing and Legal Risk

### 3.1 CLA timing: this is urgent

The business-model draft correctly identifies that a CLA is required before the maintainer wants
to relicense community contributions into commercial tiers. What it under-emphasises is the
timing risk: **a CLA must be in place before the first external PR merges.** Strategy.md §6
targets "5 external contributors with merged PRs by d90." If that target is hit without a CLA,
retroactive CLA collection from those contributors is a manual, potentially contentious process.
Contributors who are unhappy with the commercial direction (and some will be) can simply refuse.
At that point the maintainer cannot safely incorporate their contributions into any commercial
feature without a legal opinion.

ASSUMPTION: the maintainer has not yet set up a CLA bot (no evidence in the repo).

**Gate.** Add a CLA (Apache-style or HashiCorp-model: contributor grants the maintainer a broad
non-exclusive license; contribution stays MIT for the community) AND a CLA bot (cla-assistant.io
or similar) to the repo BEFORE the launch post goes live. This is a one-day task. Not doing it is
a ticking clock against the d90 contributor target.

### 3.2 License inconsistency: MIT vs. Apache-2.0

The business-model draft notes a conflict: product-plan §7 says "keep the core Apache-2.0" while
strategy.md's HN draft says "MIT." The codebase must resolve this before B0 ships because the
license is printed in the binary and the README. Switching from MIT to Apache-2.0 after external
contributions land (even one PR) requires contributor sign-off or a CLA that pre-authorises the
switch. The recommendation is: decide now (Apache-2.0 is the stronger choice for enterprise
buyers due to patent grant), then use the CLA to cover future relicensing flexibility.

ASSUMPTION: this decision belongs to the PM/maintainer; the risk here is the window between first
external PR and decision being made.

### 3.3 AUP exposure: users run arbitrary build code

`docs/launch/04-security-compliance-legal.md` §5 correctly identifies the AUP as "load-bearing."
None of the nine peer drafts flags this as a risk except in passing. This analyst elevates it:
Cooker's Dockerfile build steps, Test stage runners, and Custom stage shell scripts execute
arbitrary user-authored code. In the self-hosted model this is the operator's risk. But once
Cooker is marketed as a commercial product with a paid support tier, aggressive AUP enforcement
becomes a legal necessity. The AUP must explicitly prohibit:

- Cryptomining in build stages.
- Building malware or scanners.
- Using the build farm (if/when Cloud launches) for DoS tooling.
- Exfiltrating another tenant's secrets via build output (Cloud-only risk).

For self-hosted B0 launch: the AUP still matters because consulting and support contracts create
a relationship. A paying consulting customer who uses Cooker to build and deploy malware creates
liability without an AUP. Write and publish it before charging any money.

---

## 4. Compliance Gates

### 4.1 tenant_id as GDPR prerequisite: non-negotiable

Every peer document that touches Cloud revenue correctly flags `tenant_id` as a hard gate. This
analyst states it flatly for the lead: **no Cloud revenue before `tenant_id` lands.** The reason
is not just technical (the `billing_subscriptions.tenant_id` FK requires `tenants(id)` — that is
the schema dependency). The reason is legal: without `tenant_id`, a hosted Cooker instance cannot
execute a GDPR Art. 17 right-to-erasure request for a single customer. "Erase this customer's
data" requires a tenant boundary to scope the delete. Today there is none. Charging a European
customer for a hosted service without the ability to honour a deletion request is a GDPR violation
with material fine risk (up to 4% of global annual revenue or EUR 20M, whichever is higher).

ASSUMPTION: the maintainer is based in Thailand but the addressable market is global; GDPR applies
to EU data subjects regardless of where Cooker operates.

**Gate.** Cloud public signups: BLOCKED until `tenant_id` + GDPR erasure tooling is operational.
Self-hosted B0 licensing: UNBLOCKED. The operator is the GDPR controller in the self-hosted
model; Cooker is the software vendor.

### 4.2 PCI SAQ-A boundary: hold the line

The launch docs correctly define the PCI posture as SAQ-A (Stripe-hosted Checkout; no card data
in Cooker). No peer draft violated this. The risk is implementation drift: a future contributor
adds a card field to Cooker's own frontend "for convenience," immediately pushing scope to
SAQ-A-EP or SAQ-D. Add a note to SECURITY.md and CONTRIBUTING.md: "We are PCI SAQ-A. Never
render a card input field in Cooker's own DOM. All card entry is Stripe-hosted Checkout only."
This is a cheap, permanent guard.

### 4.3 security.txt placeholder is a launch blocker

`docs/launch/04-security-compliance-legal.md` §5 flags this. The `SECURITY.md` reporting email
is `security@cooker-ci.example.com` (an example.com placeholder). A researcher who finds a
vulnerability cannot reach the maintainer. Under any paid-tier model this is a legal and
reputational liability. Fix before the HN post goes live.

---

## 5. Commercial Risk

### 5.1 Churn assumptions have no product-specific grounding

The unit-economics draft uses 2.0%/month churn for self-hosted Crew and 3.5%/month for Cloud.
The growth draft refines these to 1.5–2.0% steady-state with a 1.5× multiplier for the first 90
days. These are sourced from published SaaS benchmarks, not from Cooker data, because Cooker has
no paying customers yet.

The sensitivity this analyst flags that the unit-economics draft does not fully surface: the
1–3% paid conversion rate (from the market-sizing and forecast drafts) and the 2.0%/month churn
assumption are the two inputs that, if both land at their pessimistic ends, produce a Y3 base ARR
below $20K — which does not sustain a solo maintainer. The bear scenario in the forecast draft
($4K total cash over 36 months) is described as "not a tail risk — it is the median outcome for
a new OSS tool that fails to reach HN front page." This analyst agrees and elevates it: the bear
scenario must be the planning baseline, not the bear case. The base case should be treated as
the upside scenario until there is market data.

**Label all churn and conversion assumptions ASSUMPTION: with NO PRODUCT-SPECIFIC DATA as their
provenance** in any document shown to investors or used in spending decisions.

### 5.2 $49 = 10x Coolify: the positioning narrative must carry its weight

The competitor draft correctly identifies that Crew at $49/replica is 10x Coolify Cloud
($5/server). The draft concludes this is defensible if the value narrative lands. This analyst
adds: the pricing page and the upgrade prompt UX must do this work. An inline upgrade CTA that
says "upgrade to Crew" without explaining why $49 is reasonable will produce churn and negative
reviews. The pricing page must include a concrete comparison: "A 30-person team on Buildkite Pro
pays $450/mo (30 × $15); Cooker Crew pays $49 for unlimited seats." This is the no-seat-tax
argument, and it must be present before B0 launches. Without it, the 10x Coolify anchor wins.

### 5.3 Single-maintainer bus factor: consulting SLA risk

The forecast draft models consulting as a material cash contributor (up to $20K/yr in the base
case). A consulting engagement that implies a response-time commitment creates an SLA obligation.
The product-plan §7 anti-goal states "no solo-operated paid SaaS before fix-first + pen-test,"
but a consulting SLA has the same single-point-of-failure risk: if the maintainer is ill, on
holiday, or overwhelmed by a HN spike, a paying consulting customer with an SLA expectation has
a claim. Consulting contracts must explicitly disclaim SLA commitments in the solo-maintainer
phase, or they create unrealistic expectations that damage the "honest engineer voice" brand.

### 5.4 Buildkite framing dependency

The pricing draft uses Buildkite Pro ($15/user) as the "Crew at $49 feels cheap by comparison"
anchor. The competitor draft prices Buildkite at $15–30/user. This framing only works if the
buyer is evaluating Cooker as a Buildkite alternative (team of 30+). For the median early
adopter (S1 solo dev or small S2 team of 5), the Buildkite anchor does not compute. The 10x
Coolify anchor dominates. The pricing narrative needs to be tiered: for solo dev audiences,
emphasise free-forever; for team audiences, use the Buildkite comparison explicitly. One pricing
page copy cannot serve both anchors simultaneously.

---

## 6. Go/No-Go Gate Checklist

The monetization lead must verify all gates in the relevant column before any revenue action.

| # | Gate | Blocks | Status |
|---|---|---|---|
| G1 | `security@cooker-ci.example.com` replaced with a real monitored address + `/.well-known/security.txt` live | B0 self-hosted launch, any revenue | OPEN — cheap, do immediately |
| G2 | ToS, Privacy Policy, and AUP published (AUP must prohibit cryptomining, malware builds, DoS) | B0 self-hosted launch with any paid consulting/licensing | OPEN |
| G3 | OSS core license decided (MIT vs Apache-2.0) and consistent across README, binary, and LICENSE file | B0 | OPEN |
| G4 | CLA bot live on the repo before first external PR merges | B0 and all future tiers | OPEN — urgent; d90 contributor target accelerates this |
| G5 | OIDC gate revised: basic OIDC moved to Explorer; only `feature.sso_group_map` and MFA step-up gated at Crew | B0 (OSS reputation risk if OIDC is paywalled at launch) | OPEN |
| G6 | SSH deploy target moved to Explorer | B0 (community risk) | OPEN |
| G7 | Pricing page includes explicit no-seat-tax comparison (vs. Buildkite/CircleCI for team audiences) | B0 Crew conversion | OPEN |
| G8 | `tenant_id` data model (ADR-0004 Appendix A) merged | Cloud billing (B1), Cloud GDPR compliance | OPEN — 6–8 wk engineering |
| G9 | Per-tenant build-farm isolation (gVisor/Kata or per-tenant node pools) + external pen-test passed | Cloud public signups | OPEN — follows G8 |
| G10 | GDPR right-to-erasure tooling wired to `tenant_id` | Cloud revenue from EU data subjects | OPEN — follows G8 |
| G11 | DPA + SCCs offered to Cloud customers | Cloud revenue from EU customers | OPEN — legal work |
| G12 | Cloud go/no-go decision made (ADR-0004 decision A) | Cloud build starts (do not build B1 speculatively) | OPEN — unmade PM decision |
| G13 | Stripe Checkout confirmed SAQ-A (no card field in Cooker's DOM; Stripe-Signature webhook verification active; Stripe route on audit IsRedacted list) | Any Stripe billing | OPEN — billing not yet built |
| G14 | Consulting contracts explicitly disclaim SLA commitments | Consulting engagements | OPEN |
| G15 | All churn/conversion figures in any spend-authorising document carry the label "ASSUMPTION: no product-specific data" | Forecast credibility | OPEN |

---

## Cross-team flags

**pricing:** (1) Revise `feature.oidc_mfa` entitlement to give basic OIDC to Explorer; keep
`feature.sso_group_map` at Crew. Update the §1.4 table in `01-billing-monetization.md`. (2) Move
SSH deploy target to Explorer. (3) Add a concrete no-seat-tax comparison on the pricing page
(30-person team Buildkite vs. Cooker). These three changes are required before B0 ships.

**business-model:** (1) The CLA decision is urgent: state explicitly in your document that the
CLA bot must be live before the first external PR merges, not "before open-core is considered."
(2) The license inconsistency (MIT in strategy.md, Apache-2.0 in product-plan) must be resolved
and noted as a pre-B0 gate. (3) The AUP is a prerequisite for consulting, not just SaaS —
update your doc to reflect this.

**unit-economics:** Label the 2.0%/mo self-hosted churn and 3.5%/mo Cloud churn as "ASSUMPTION:
no product-specific data" wherever these numbers appear. The LTV and LTV:CAC outputs inherit this
uncertainty. The bear scenario ($4K/36 months) is a credible planning baseline; do not present
the base case as the plan.

**forecast:** (1) Elevate the bear scenario to the planning baseline. (2) Add a sensitivity row
for "OIDC-gate backlash causes negative HN launch" — this scenario reduces Y1 star count by
30–50% and is a real risk if the OIDC gate is not revised before launch. (3) Consulting contracts
must include an explicit disclaimer that there is no SLA obligation in the solo-maintainer phase.

**growth:** The OIDC gate recommendation aligns with your proposal. Consider modelling the
"OIDC stays gated" scenario as a separate sensitivity in your conversion estimates — it likely
reduces Explorer→Crew conversion through the auth trigger, but increases OSS community goodwill
and top-of-funnel adoption, which improves the star/install base that feeds the conversion
funnel.

**partnerships:** (1) The Hetzner referral sunset (August 2026) is correctly flagged. Action
by July 2026. (2) Any KeepSave co-blog post must not imply "we custody your secrets securely in
our shared Cloud" until `tenant_id` + pen-test gates are cleared. Co-blog the self-hosted
integration only.

**segmentation:** The S4 (Enterprise) segment description correctly flags the single-tenant IDOR
as blocking enterprise claims. Confirm that no marketing copy derived from segmentation uses
"team-isolated" or "enterprise-ready" language before those gates (G8–G11) clear.

**competitor:** The Coolify $5/server anchor and the Buildkite $15/user anchor serve different
buyer audiences. Your analysis correctly surfaces both but does not reconcile them into a single
pricing-page narrative. Flag to pricing: one pricing page cannot serve both anchors without
explicit audience segmentation in the copy.

**market-sizing:** The $165–345M SAM range has a 2x width that makes it nearly unusable for
planning. The more important number is the bottom-up SOM ($35–140K Y3 ARR). Lead with the
bottom-up number in any document used for decisions; the SAM is context, not a target.
