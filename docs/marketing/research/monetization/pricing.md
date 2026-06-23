# Cooker — Pricing & Packaging v1

> Analyst: pricing & packaging. Round 1, 2026-06-21.
> Spine doc: `docs/launch/01-billing-monetization.md` §1 (Explorer $0 / Crew $49/replica / Constellation custom; unlimited seats).
> This document validates, refines, and flags risks against that committed mockup.

---

## 1. Value-metric analysis

### The three candidates

| Metric | How it works | Pros for Cooker | Cons for Cooker |
|---|---|---|---|
| **Per-replica** (mockup choice) | Bill by running Cooker process; seats unlimited | Maps to a real infra config (HA vs single-node); defensible offline; no usage pipeline needed at launch; matches the Ed25519 license model (B0 ships first) | Odd to buyers unused to it; hard to explain on a pricing page; meaningless for Cloud (we run the process) |
| **Per-seat** | Bill per authenticated user/developer | Familiar; scales with team size | Contradicts the mockup FAQ promise ("seats are unlimited"); penalises growing teams; bad OSS optics; most painful for the Indie-hacker ICP who uses Cooker solo |
| **Usage (build-minutes / concurrent builds)** | Bill by compute consumed | Aligns cost to value; natural for Cloud | Requires a metering pipeline; surprise invoices on spiky workloads; overkill for self-hosted with no telemetry path |

### Recommendation

**Keep per-replica for self-hosted (B0). Add build-minute metering only for Cloud (B2), gated on the tenant_id prerequisite.**

Rationale: the mockup's FAQ is a binding brand promise. Reversing unlimited seats before launch would require a deliberate, visible change, and there is no competitive pressure that makes per-seat more defensible right now (Woodpecker is fully free; the self-hosted-PaaS set charges per connected server, not per seat). Per-replica is the cheapest thing to build, verifiable offline, and the one axis that maps cleanly to the existing feature-flag gates (multi-replica HA requires Redis, which is already behind a config flag). For Cloud, concurrent builds is the scarce resource (each is a build pod we pay for) and is already the dimension the rate limiter throttles; build-minutes is the natural overage meter on top.

The hybrid model (`billing-monetization.md §1.2`) is the right shape and is confirmed here.

---

## 2. Tier design

The §1.4 entitlement table from the launch doc is reproduced and extended below. No commitments are reversed; two limits are sharpened, one gap is filled.

### Extended entitlement table

| Entitlement | Explorer (Free) | Crew (Team/Pro) | Constellation (Enterprise) | Change from §1.4? |
|---|---|---|---|---|
| **Self-hosted price** | $0 | $49 / replica / mo | Custom annual | No change |
| **Cloud price** | $0 | $39 / mo base + metered build-minutes | Custom | No change |
| **Seats** | Unlimited | Unlimited | Unlimited | No change — mockup promise |
| **Replicas (self-hosted)** | 1 | Unlimited | Unlimited | No change |
| **Concurrent builds** | 1 | 3 included, then metered (Cloud) | Negotiated | No change |
| **Build-minutes / mo** | 200 | 2,000 included, overage metered | Pooled / committed | No change |
| **Pipelines / runs** | Unlimited | Unlimited | Unlimited | No change — mockup promise; do NOT gate these |
| **Environments** | 1 (Dev only) | 3 (Dev / Staging / Prod) | Unlimited | No change |
| **Deploy targets** | K8s, Fly, Render | + ECS, Cloud Run, SSH | All + air-gapped | Sharpened: Explorer loses SSH (low-friction surface; keeps Cloud Run / ECS for Crew) |
| **Run retention** | 7 days | 90 days | Configurable / export | No change |
| **Secrets backends** | Postgres AES-GCM | + Vault / AWS / GCP | + KeepSave multi-tenant | No change |
| **OIDC + RBAC + MFA** | No (dev-admin only) | Yes | Yes + SSO group→role | **FLAG — see §5** |
| **Cron triggers** | No | Yes | Yes | No change |
| **Audit log + OTLP** | No | Basic (stdout / DB) | Full + append-only export | No change |
| **API tokens / service accounts** | Yes | Yes | Yes | **Added**: API tokens shipped (product-plan §5 Tier 1); gating them behind Crew would be hostile to OSS adoption and contradicts strategy.md §7 |
| **Pipeline-as-code (YAML export/import)** | Yes | Yes | Yes | **Added**: shipped feature; must not be gated — it's an adoption driver, not an enterprise feature |
| **Support** | Community | Priority email | SLA + dedicated CSM | No change |
| **14-day trial** | n/a | Yes (Crew features, no card required) | Yes (sales-assisted) | No change |

### Good-better-best logic

- **Explorer** is the adoption engine. Its limits (1 replica, 1 environment, 7-day retention, no OIDC, no cron) are real constraints that the Indie-hacker ICP will hit at scale, not on day one. The free tier is deliberately generous on the things that drive HN stars (unlimited pipelines, runs, seats, YAML export) and tight on the things that indicate production use (HA, multi-environment, OIDC).
- **Crew** is the upgrade trigger for a team that has graduated from a side project. The OIDC gate, multi-environment, and 90-day retention are the natural "we're using this for real now" signals. At $49/replica the economic ask is: "you already pay $15-45/mo for the VPS; now pay another $49 because your team needs OIDC and a Staging environment."
- **Constellation** is sales-assisted and gated on work that doesn't exist yet (multi-tenant isolation, air-gapped licensing, KeepSave). Don't market it at launch; list it on the pricing page as "contact us."

### Where exactly the free line sits

The free line is drawn at **production-team use**, not at power-user use. A solo dev running one replica with one environment and community secrets gets everything that matters to them for free. A team that needs: (a) their IdP for auth, or (b) a separate Staging env with approval gates, or (c) audit logs — pays. This is defensible and consistent with the launch doc.

ASSUMPTION (unit-economics team): the 200 build-minutes Cloud free tier assumes a cost floor that makes this breakeven or better. If a build-minute costs more than ~$0.0005 at scale, 200 free minutes per tenant may erode Cloud margins. Needs validation.

---

## 3. Price point defence

### Self-hosted: $49 / replica / month

Competitor anchors (all prices sourced 2026-06-21; see sources):

| Competitor | Model | Price | Notes |
|---|---|---|---|
| Coolify Cloud | Per connected server (control-plane SaaS) | $5 / mo (2 servers) + $3/server | Closest structural analogue; dramatically cheaper — different value prop (no CI, just PaaS) |
| Woodpecker CI | Free (Apache 2.0, YAML-only, no graph editor) | $0 | OSS reference; no commercial tier |
| Buildkite | Per user / orchestration only | ~$15-30 / user / mo (Pro) | Bring-your-own compute; SaaS orchestrator |
| CircleCI Performance | Per active user + build credits | $15 / active user / mo + $0.006/min | SaaS; credit model |
| Harness CI Team | Per developer | ~$57 / developer / mo | Enterprise-grade; complex module pricing |
| Drone Enterprise | Contact sales | Not published | Harness-owned; OSS base free |

**Verdict on $49/replica:** the price is not anchored to direct per-instance competitors because almost none exist — self-hosted CI tools charge per user (Buildkite, CircleCI) or are fully free (Woodpecker, Jenkins). Coolify's $5/server is the nearest structural match, but Coolify's cloud doesn't include CI/CD.

The $49 figure sits in a defensible position: it is well below the $57/dev Harness Team price (which assumes an enterprise buyer) and roughly equivalent to Buildkite Pro for a team of ~3 developers. The per-replica axis is unusual but coherent: the buyer already chose to run multi-replica, which signals a production workload with budget. One replica at $49 is a VPS-sized commitment.

ASSUMPTION (unit-economics team): $49/replica should yield positive margin after infrastructure costs (B0 licensing has near-zero variable cost). The Crew self-hosted tier's only ongoing cost is license-key issuance tooling and support. This should be comfortable, but unit-economics needs to confirm the cost floor and validate whether $49 is too conservative.

Risk: Coolify's $5/server creates a perception anchor. A buyer comparing "Coolify Cloud at $5 vs Cooker Crew at $49" will need a clear answer: "Coolify doesn't build your images or run your CI." That story needs to be on the pricing page.

### Cloud: $39 / month base

The $39 Cloud base (plus metered build-minutes) is approximately in line with CircleCI Performance ($15/active user for a small team of ~3) and is below Buildkite Pro for a comparable team. The base provides 2,000 build-minutes included, which at CircleCI's ~$0.006/minute would cost ~$12 in credits alone. The base rate is reasonable.

ASSUMPTION (unit-economics team): overage pricing for build-minutes (the $/min rate above the 2,000 included) is not set here. This rate needs a cost floor from unit-economics to avoid margin erosion. ASSUMPTION: suggest ~$0.008-0.012/minute overage (above CircleCI's $0.006 because we offer self-managed isolation; below $0.02 to stay accessible). Unit-economics must confirm.

---

## 4. Packaging

### Annual discount

Recommend a flat **20% annual discount** (equivalent to paying 10 months for 12). This is a standard SaaS offering and is easy to communicate: "$49/mo or $470/yr." Rationale: 20% is the Coolify Cloud rate for annual; CircleCI and Buildkite offer comparable or larger discounts at volume. 20% is enough to shift cash-flow-sensitive buyers to annual without destroying margin.

ASSUMPTION: unit-economics should validate that annual pre-payment covers the expected license-issuance + support cost for a Crew customer over 12 months.

### Trial design

The mockup commits to a **14-day trial** of Crew features. Recommended design:

- No credit card required on signup. Card collected at trial end (Stripe Checkout in trial mode).
- Trial starts on first license activation (self-hosted) or first tenant provision (Cloud). Not on account creation — give the buyer time to read the docs.
- Trial expiry: graceful degradation to Explorer (Free) limits, not a hard lockout. Running pipelines and replicas keep working; Crew features (OIDC, Staging env, extended retention) lock. This matches the §4.2 "degrade, never brick" posture.
- 3-day warning email via the existing Slack/Email notifier (the notifier is already shipped; billing-service just needs to hook `customer.subscription.trial_will_end`).
- Self-hosted trial: the Ed25519 license file carries `expires_at` = 14 days out; the license-issuance tool generates a trial license automatically on registration. No Stripe dependency.

### Bundling

No bundles at launch. The tier structure is clean enough that bundling (e.g., "Crew + 5,000 build-minutes") adds confusion without a demonstrated user-demand signal. Revisit if analytics show that Crew customers consistently burn over their included 2,000 minutes.

---

## 5. Brand-rule flags (gating contradictions)

One potential conflict with the "unlimited pipelines/runs/seats" promise in the mockup FAQ:

**OIDC gate (Crew only):** the §1.4 table gates `feature.oidc_mfa` behind Crew. The mockup FAQ says "unlimited seats on every paid tier" but is silent on auth method. Gating OIDC behind Crew means Explorer users can only use dev-admin mode or local auth. For a self-hosted OSS tool in 2026, this is likely to generate OSS backlash: OIDC is not a premium feature in any adjacent project (Gitea, Forgejo, Woodpecker all ship OIDC free). This is flagged to the risk analyst and growth analyst for a go/no-go call. Options:
  (a) Move OIDC to Explorer and gate SSO group-role-mapping (the actual enterprise value) behind Crew — lower backlash, still a real upgrade trigger.
  (b) Keep OIDC behind Crew and invest in explaining why (the billing-monetization doc's §4.2 rationale is solid but needs a pricing-page callout).

This is a deliberate change from §1.4 if (a) is chosen, and must be flagged as such.

---

## Cross-team flags

- **Competitor analyst (cooker-mkt-competitor):** price anchors above are from web searches dated 2026-06-21 and are reasonable for planning. The competitor analyst should validate Coolify Cloud's exact per-server rate and Buildkite's per-user Pro pricing; both are used directly to justify the $49 and $39 figures. Flag if either anchor moves materially.

- **Unit-economics (cooker-mkt-unit-economics):** two unset numbers this analysis depends on: (1) the overage build-minute rate for Cloud (I've assumed $0.008-0.012/min); (2) confirmation that $49/replica self-hosted yields positive margin (cost should be near-zero for a license-key model). Both affect whether the $39 Cloud base + meter model is profitable at the free tier's 200-minute inclusion.

- **Forecast (cooker-mkt-forecast):** the tier table in §2 is the input for conversion modeling. Key conversion assumption: Explorer→Crew trigger is the first time a user (a) adds a second team member with OIDC, or (b) creates a second environment. Forecast should model separately: Crew self-hosted ($49 flat) and Crew Cloud ($39 + overage), since the revenue profiles differ.

- **Growth (cooker-mkt-growth):** the free tier is deliberately generous (unlimited pipelines/runs/seats/API tokens/YAML export). This is intentional per product-plan §7 ("adoption first"). If growth models show that free-tier overgenerosity suppresses conversion, the levers are: (a) cut run retention from 7 to 3 days on Explorer, (b) reduce Explorer build-minutes from 200 to 50 on Cloud. Do not touch pipelines/runs/seats limits without a brand-promise audit.

- **Risk (cooker-mkt-risk):** the OIDC gate is the highest OSS-backlash risk in the current §1.4 table. The strategy.md §7 brand rules say "keep the core OSS." Gating OIDC (a standard infra auth protocol) behind a paid tier may be read as open-washing. Recommend the risk analyst evaluate option (a) from §5 above — moving OIDC to Explorer while keeping SSO group mapping behind Crew — against the conversion impact before launch.
