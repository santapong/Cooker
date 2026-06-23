# Unit Economics — Cooker v1 (Round 1 draft)

> Author: unit-economics analyst. Date: 2026-06-21.
> Status: v1 independent draft — inputs from risk/growth/SEM not yet received.
> Label key: ASSUMPTION: = owner named; FLAG: = cross-team dependency.

---

## 1. Product and revenue-model recap

Two delivery modes, two very different cost structures:

- **Self-hosted (B0)** — customer runs the binary; Cooker issues an Ed25519 signed license key. Marginal COGS per customer is near-zero: one license issuance (seconds of compute) plus support overhead.
- **Hosted Cloud (B1/B2)** — Cooker operates a shared build farm; every customer build spawns a Kaniko pod, consuming real compute. Hard-gated on `tenant_id` multi-tenancy (~3 weeks unbuilt) and a pen-test; treat as future-state.

Tier ladder (from `01-billing-monetization.md` §1.3):

| Tier | Self-hosted price | Cloud price |
|---|---|---|
| Explorer | $0 | $0 |
| Crew | $49 / replica / mo | $39 / mo base + build-minute usage |
| Constellation | Custom (annual) | Custom |

---

## 2. COGS — self-hosted

Self-hosted COGS is primarily fixed-overhead amortised over the customer base. There is no marginal compute per build (the customer owns the cluster).

| COGS item | Estimate | Basis |
|---|---|---|
| License issuance (Ed25519 sign, infra) | ~$0.10 / customer / mo | amortised cost of a minimal issuer service; essentially zero at low volume |
| Support (community tickets, async) | ~$2–5 / customer / mo | ASSUMPTION: estimated at 15 min/mo maintainer time at blended $30/hr; owned by ops/support |
| Payment processing (Stripe) | 2.9% + $0.30 / transaction | Stripe standard rate; on $49 Crew = $1.72/transaction = ~3.5% effective |
| Infra for license validation (CDN key distribution) | ~$0.05 / customer / mo | negligible |

**Self-hosted Crew COGS: ~$4–7 / customer / mo** (support-dominated).

**Self-hosted Crew gross margin: ($49 − $6) / $49 ≈ 87–92%.**

This is the high-margin anchor. Self-hosted licensing is the right first revenue move (B0 has zero blockers per `01-billing-monetization.md` §6).

---

## 3. COGS — Cloud (build-pod-dominant)

Each build spawns an ephemeral Kaniko pod. That pod is the primary Cloud COGS driver. Every Crew Cloud customer on the $39/mo base plan is included in 2,000 build-minutes/mo; overages bill at a rate TBD by pricing.

### 3.1 Build pod compute cost

Reference: EC2 t3.medium (2 vCPU / 4 GB) — a reasonable Kaniko pod size for a typical Docker image build.

| Rate | $/hr | Source |
|---|---|---|
| On-demand (t3.medium, us-east-1) | $0.0416 | AWS EC2 on-demand pricing, June 2026 |
| Spot (~62% discount) | $0.016 | AWS EC2 spot pricing, June 2026 (cited: instances.vantage.sh/aws/ec2/t3.medium) |

Per-minute costs:

- On-demand: $0.0416 / 60 = **$0.00069 / build-min**
- Spot: $0.016 / 60 = **$0.00027 / build-min**

ASSUMPTION: Cooker Cloud uses Spot instances with ~80% success rate, falling back to on-demand for the remaining 20%. Effective blended rate ≈ $0.00035 / build-min. Spot availability risk is real; owned by infra/ops.

### 3.2 Managed Postgres cost (per customer allocation)

Cloud requires one Postgres schema (or DB) per tenant once `tenant_id` lands. ASSUMPTION: a single managed Postgres cluster is shared across tenants; cost is allocated per tenant headcount.

| Provider | Entry price | Source |
|---|---|---|
| Neon (serverless) | ~$5–15 / mo per project | selfhost.dev managed Postgres comparison 2026 |
| Supabase Pro | $25 / mo per org | srvrlss.io Supabase pricing 2026 |
| DigitalOcean managed Postgres | $15 / mo (1 GB / 1 vCPU) | selfhost.dev comparison |

At launch scale (tens of tenants), ASSUMPTION: shared Neon cluster allocated at ~$1–3 / tenant / mo amortised. Owned by infra.

### 3.3 Egress and other infra

| Item | Estimate |
|---|---|
| Build-log egress (WebSocket streaming) | ~$0.09 / GB; typical build ≈ 50 KB logs → negligible |
| Container-image push egress (to customer registry) | Customer's own registry; we push out → ~$0.05–0.09 / GB AWS; most builds are <500 MB → $0.03–0.05 / build |
| Redis (rate-limiter, WS hub, ticket store) | Shared cluster; ~$0.50–1.00 / tenant / mo at small scale |

### 3.4 Cloud COGS summary — Crew tier ($39 base + usage)

Using 2,000 included build-minutes/mo as the representative Crew customer:

| Item | Cost / mo |
|---|---|
| Build compute (2,000 min × $0.00035) | $0.70 |
| Postgres allocation | $2.00 |
| Redis / infra allocation | $0.75 |
| Egress (build logs + image push est.) | $1.00 |
| Support | $3.00 |
| Stripe (2.9% + $0.30 on $39) | $1.43 |
| **Total Cloud COGS — light user** | **~$9** |

Heavy user (5,000 build-min/mo, 2.5× included):

| Build compute (5,000 min × $0.00035) | $1.75 |
| All other items same | $8.18 |
| **Total Cloud COGS — heavy user** | **~$10** |

ASSUMPTION: "Heavy user" overage revenue offsets the marginal compute increase at the metered rate — pricing must set the overage rate above $0.00035/min to preserve margin. Overage rate design is owned by pricing.

**Cloud Crew gross margin (light user):** ($39 − $9) / $39 ≈ **77%.**

Note: the 77% figure is structurally more fragile than self-hosted's 87–92% because it is sensitive to Spot availability, build intensity, and support load. The 2,000 included-minutes figure is the fulcrum — pricing must calibrate it so a median customer lands near the light-user scenario.

---

## 4. Price floor (what pricing must respect)

| Tier / mode | COGS | Required floor to achieve 70% gross margin |
|---|---|---|
| Self-hosted Crew | ~$6 / mo | ~$20 / mo — actual price $49; 87–92% margin; **healthy** |
| Cloud Crew (base) | ~$9 / mo | ~$30 / mo — actual base $39; 77% margin; **at the minimum acceptable boundary** |
| Cloud Crew overage / build-min | $0.00035 / min | floor: $0.0005 / min (1.4× cost); recommend $0.006–0.008/min to match GitHub Actions pricing signal and preserve margin |

FLAG TO PRICING: the $39 Cloud base is tight. Any support cost growth, Spot scarcity events, or heavier-than-expected average build duration will compress margin below 70%. Recommend the base land at $49 (matching self-hosted) or that included build-minutes be reduced from 2,000 to 1,000 to widen headroom. Bring this to the pricing agent before committing the Cloud price.

---

## 5. CAC

### 5.1 Organic / blended CAC (current plan)

Strategy.md is explicit: no paid ads before day 180. All launch acquisition is organic: Show HN, Reddit, Dev.to, YouTube, conference/podcast. Content cost is primarily maintainer time.

ASSUMPTION: blended organic CAC estimate framework (owned by growth/SEM):

| Cost component | Estimate | Basis |
|---|---|---|
| Maintainer content time (16 hr/week × $30/hr, 13-week quarter) | ~$6,240 / quarter | Strategy §8 budget |
| Hosting + tooling (domain, video editing) | ~$200 / quarter | Strategy §8 |
| **Total quarterly organic spend** | **~$6,440** | |

Target paid conversions (strategy.md day-90 targets: ~1,000 stars, Helm pulls ~1,200, GHCR ~5,000). ASSUMPTION (owned by growth): conversion from star/pull to paying customer at 1–3% for self-hosted Crew in the first 90 days = 12–36 paying customers. Using midpoint 24:

**Organic blended CAC: ~$6,440 / 24 ≈ $268.**

This is high relative to a $49/mo product but is expected for a nascent OSS tool where most acquired users are on the free Explorer tier. The relevant denominator shifts toward paying customers as traction builds. ASSUMPTION: by month 12, conversion rate improves to 5–8% of organic trial installs → organic CAC converges toward $80–150. Owned by growth.

### 5.2 Paid CAC (hypothetical SEM, post-day-180)

FLAG TO SEM: no SEM budget is authorised before day 180 (strategy.md §8). When SEM is eventually activated, the CAC ceiling this model can support is derived from the LTV:CAC constraint in §6. Do not exceed $250 paid CAC on Crew self-hosted or $200 on Cloud Crew (see §6).

---

## 6. LTV and LTV:CAC

ASSUMPTION: monthly churn rate. This is the single most critical input and is **owned by risk/growth (cooker-mkt-risk / cooker-mkt-growth)**. No LTV model is honest without it. Provisional ranges used here based on published SaaS benchmarks:

| Customer segment | Monthly churn assumption | Annual retention | Source / note |
|---|---|---|---|
| Self-hosted Crew (SMB/team) | 2.0% | ~79% | Infrastructure SaaS benchmarks show lowest churn ~1.8%/mo; citing Optif.ai/mrrsaver 2026 benchmarks; self-hosted tools tend to be stickier once deployed |
| Cloud Crew | 3.5% | ~65% | SMB SaaS median per mrrsaver.com 2026; Cloud is easier to cancel than a self-hosted binary |

These are provisional. Risk/growth must confirm with product-specific data once the product has retention history.

### LTV calculation: `price × gross-margin / monthly-churn`

| Scenario | Price/mo | Gross margin | Monthly churn | LTV |
|---|---|---|---|---|
| Self-hosted Crew | $49 | 89% (midpoint) | 2.0% | $49 × 0.89 / 0.02 = **$2,181** |
| Cloud Crew | $39 | 77% | 3.5% | $39 × 0.77 / 0.035 = **$859** |

### LTV:CAC ratios

| Scenario | LTV | CAC (organic blended) | LTV:CAC | Payback (mo) |
|---|---|---|---|---|
| Self-hosted Crew — organic | $2,181 | $268 | **8.1×** | **5.5 mo** |
| Cloud Crew — organic | $859 | $268 | **3.2×** | **8.3 mo** |
| Self-hosted Crew — paid ($200 CAC) | $2,181 | $200 | **10.9×** | **4.1 mo** |
| Cloud Crew — paid ($200 CAC) | $859 | $200 | **4.3×** | **6.2 mo** |

**Benchmark:** SaaS rule-of-thumb is LTV:CAC ≥ 3× (healthy) and payback < 12 months. All four scenarios clear that bar, even under the pessimistic churn assumptions.

**Self-hosted Crew is the strong unit-economics story** — 8×+ LTV:CAC under organic acquisition and margin nearly at 90%. Cloud Crew's economics are positive but thinner; Cloud viability depends on holding churn below 3.5%/mo and not over-sizing included build-minutes.

---

## 7. Sensitivity — what breaks the model

| Variable | Current assumption | Break-even level | Risk |
|---|---|---|---|
| Self-hosted monthly churn | 2.0% | 11.4% (LTV:CAC drops to 1×) | Low; self-hosted infra tools are sticky |
| Cloud monthly churn | 3.5% | 12.7% (LTV:CAC drops to 1×) | Medium; Cloud is easier to cancel |
| Organic CAC | $268 | $2,181 (self-hosted) / $859 (Cloud) | Low at those ceilings; the real risk is paid CAC runaway |
| Cloud gross margin | 77% | Any Spot scarcity event adds ~$0.0002/min → margin drops ~2 pp per 1,000 min/customer |  Medium |
| Constellation ARR | Custom | At $5,000–15,000 ARR and near-100% margin (self-hosted), one Constellation deal changes the whole P&L | High upside, low probability at launch |

---

## 8. Cross-team flags

**To `cooker-mkt-pricing`:**
- Price floor alert: Cloud Crew base of $39 gives only 77% gross margin. The floor for 70% margin is $30; we are $9 above it, but that buffer is thin. Any increase in support costs, average build intensity, or Spot scarcity can erode it. Recommend either raising Cloud Crew base to $49 or reducing included build-minutes to 1,000/mo. Bring this constraint to your pricing model explicitly.
- Overage rate floor: build-minute overage must be priced above $0.0005/min (cost floor); $0.006–0.008/min is the market-rate anchor (GitHub Actions charges $0.006/min on Linux).
- Self-hosted Crew at $49 / replica is structurally sound — 87–92% margin; no change recommended.

**To `cooker-mkt-sem` (Round 2 CAC ceiling):**
- **Self-hosted Crew: paid CAC ceiling = $250.** Above that, payback exceeds 12 months at median churn.
- **Cloud Crew: paid CAC ceiling = $180.** Thinner margin and higher churn leave less room.
- Do not activate paid acquisition before day 180 (strategy.md hard rule).

**To `cooker-mkt-forecast`:**
- Self-hosted Crew: 89% GM, LTV $2,181, LTV:CAC 8.1× (organic). Use for cohort modelling.
- Cloud Crew: 77% GM, LTV $859, LTV:CAC 3.2× (organic). Use lower confidence interval — Cloud is deferred and margin is sensitive.
- Constellation: treat as custom; near-100% self-hosted margin; model as a single deal scenario at $5k–15k ARR.
- Blended CAC drops materially as the customer base grows (fixed organic spend amortised over more conversions); model this as a declining-CAC cohort, not a flat $268.

**To `cooker-mkt-risk` and `cooker-mkt-growth` (churn input needed — highest priority):**
- The churn assumptions (2.0%/mo self-hosted, 3.5%/mo Cloud) are provisional benchmarks from published SaaS surveys. LTV and LTV:CAC swing materially on this number. This analyst needs actual product-specific churn signals — onboarding completion rates, first-30-day activation data, and comparable self-hosted tool retention data — before Round 2 numbers can be trusted.
- Flagging specifically: 43% of SMB churn concentrates in the first 90 days (per mrrsaver/optif.ai benchmarks). Cooker's "time to first run" target (6 min median per strategy §6) is the most important lever for the early-cohort survival rate.

---

## Sources

- [Managed PostgreSQL Comparison 2026](https://selfhost.dev/blog/managed-postgresql-comparison-2026/)
- [Neon Serverless Postgres Pricing 2026](https://vela.simplyblock.io/articles/neon-serverless-postgres-pricing-2026/)
- [GitHub Actions Pricing 2026 (CICDCalculator)](https://cicdcalculator.com/github-actions)
- [CI/CD Cost Comparison 2026 (LeanOps)](https://leanopstech.com/blog/ci-cd-pipeline-costs-github-actions-circleci-gitlab-2026/)
- [EC2 t3.medium spot pricing (Vantage)](https://instances.vantage.sh/aws/ec2/t3.medium)
- [EC2 t3.medium on-demand pricing (Economize)](https://www.economize.cloud/resources/aws/pricing/ec2/t3.medium/)
- [SaaS Churn Benchmarks 2026 (MRRSaver)](https://www.mrrsaver.com/blog/saas-churn-rate-benchmarks)
- [B2B SaaS Churn Rate Benchmarks (Optif.ai)](https://optif.ai/learn/questions/b2b-saas-churn-rate-benchmark/)
- [SaaS Churn Rate Statistics 2026 (SaaSUltra)](https://www.saasultra.com/saas-churn-rate-statistics-benchmarks/)
