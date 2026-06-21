# Cooker — Revenue Forecast v1

> Author: revenue-forecasting analyst · Round 2 draft · Date: 2026-06-21
> Upstream inputs consumed: market-sizing, pricing, unit-economics, segmentation, business-model (all Round 1).
> Every driver is labelled ASSUMPTION: or SOURCE:. No hidden hockey-stick.

---

## 0. Constraints that bound every number in this document

Before any table: three hard facts from the launch docs that the forecast must not paper over.

1. **B0 (self-hosted licensing) is the only revenue lane with zero blockers today.** Cloud (B1/B2) is gated on `tenant_id` multi-tenancy (~6–8 weeks unbuilt), build-farm isolation, external pen-test, and an unmade go/no-go (ADR-0004). Cloud revenue is excluded from the base case and treated as a separate bull-only scenario starting no earlier than Q2 2027.
2. **The product has not launched yet.** Day-0 stars = 0. The strategy.md §6 targets (200 stars d7, 1,000 stars d90) are aspirations, not guarantees. The base case treats them as achievable; the bear case does not.
3. **Solo maintainer, ~16 hours/week marketing capacity** (strategy.md §8). Revenue actions that require more than that are unschedulable until a second contributor joins.

---

## 1. Funnel model — stars → installs → activated → paid

ASSUMPTION (A3 from market-sizing): stars-to-installs multiplier = 3–5x for a sub-2-year OSS project. ASSUMPTION (A4): active/production installs = 15–25% of total installs. ASSUMPTION (A5): paid conversion (Explorer → Crew self-hosted) = 1–3% of active installs at $49/replica/mo; segmentation supports 2–7% free-to-paid for devtools OSS, but $49 is higher friction than a $5/mo Cloud tier.

### Funnel by scenario at Year 1 (month 12 post-launch) and Year 3 (month 36)

| Stage | Conv. rate | Bear Y1 | Base Y1 | Bull Y1 | Bear Y3 | Base Y3 | Bull Y3 |
|---|---|---|---|---|---|---|---|
| GitHub stars | — | 400 | 1,000 | 3,000 | 1,500 | 5,000 | 15,000 |
| Total installs | 3–5x stars | 1,200 | 4,000 | 12,000 | 4,500 | 20,000 | 60,000 |
| Active/prod installs | 15–25% of installs | 180 | 800 | 3,000 | 675 | 4,000 | 15,000 |
| Paying (Crew self-hosted) | 1–3% of active | 2 | 16 | 90 | 7 | 80 | 450 |
| Crew Cloud paying | GATED — see §4 | 0 | 0 | 0 | 0 | 0 | bull-only |

ASSUMPTION: average paying account = 1 replica ($49/mo). Rationale: median Crew buyer is a single S2 team upgrading from Explorer single-replica; multi-replica HA is an S3-segment minority at launch scale. Bull Y3 assumes a mix of 1-replica and 2-replica accounts; blended ARPU modelled at $60/mo in that scenario.

---

## 2. Revenue lines by quarter — base scenario

All figures are ARR-equivalent (annualised from the quarter's MRR × 12). Consulting is modelled as one-time; it cannot be annualised without a backlog. See note below table.

| Quarter | B0 Licensing MRR | B0 Licensing ARR-eq | Consulting (one-time) | Sponsorship | Cloud MRR | Notes |
|---|---|---|---|---|---|---|
| **Q1 (M1–3)** | $49–$196 | $600–$2,400 | $0–$2,000 | $0 | $0 | Launch month; 2–4 first Crew converts; consulting possible if a SMB shows up |
| **Q2 (M4–6)** | $490–$784 | $5,900–$9,400 | $0–$3,000 | $0–$100 | $0 | HN aftermath conversions; sponsorship if >500 stars |
| **Q3 (M7–9)** | $784–$1,176 | $9,400–$14,100 | $1,000–$5,000 | $100–$200 | $0 | Comparison SEO content drives organic Crew trials |
| **Q4 (M10–12)** | $1,176–$1,960 | $14,100–$23,500 | $1,000–$5,000 | $100–$200 | $0 | Year-end; B0 only; Cloud gate still closed unless tenant_id ships in Q3 |
| **Y1 total** | — | ~$5K–$23K | ~$2K–$15K | ~$0–$500 | $0 | **Base: ~$12K combined; consulting is the near-term cash driver** |
| **Q5–Q8 (Y2)** | Grows to ~$3,920–$6,860 MRR | ~$47K–$82K | $5K–$20K | $200–$500 | GATED | Crew traction compounds; annual billing discounts improve cash flow |
| **Q9–Q12 (Y3)** | Grows to ~$3,920 MRR | ~$47K–$75K | $10K–$30K | $500–$2K | GATED | B0 is the revenue spine; consulting is lumpy but material |
| **Y3 total (B0 only)** | — | ~$47K–$75K | ~$10K–$30K | ~$500–$2K | — | **Base composite: ~$60–107K ARR-equivalent** |

ASSUMPTION: monthly churn = 2.0%/mo on self-hosted Crew (unit-economics input). Net customer retention modelled as ~79% annual. ASSUMPTION: no meaningful annual-contract uplift in Y1; introduce 20% annual discount promotion in Q3 (pricing recommendation).

Note on consulting: consulting revenue is uncorrelated with the star funnel. It arrives when an S2 buyer needs deployment help. One Crew customer who also buys a $3,000 consulting engagement doubles the Q1 cash. This is a high-variance, capacity-gated line — modelled conservatively; not included in ARR.

---

## 3. Bear / base / bull — three scenario summary

| Scenario | Premise | Y1 ARR-eq (B0 lic) | Y1 Consulting | Y1 Total cash | Y3 ARR-eq (B0 lic) | Y3 Consulting | Y3 Total cash | Y3 Cloud ARR |
|---|---|---|---|---|---|---|---|---|
| **Bear** | HN misses; <500 stars Y1; 1% paid conversion; no consulting | ~$1,200 | $0 | ~$1,200 | ~$4,100 | $0 | ~$4,100 | $0 |
| **Base** | 1,000 stars d90; 1.5% paid conv.; 2 consulting engagements/yr; no Cloud | ~$12,000 | ~$6,000 | ~$18,000 | ~$58,000 | ~$20,000 | ~$78,000 | $0 |
| **Bull** | Strong HN; 3,000 stars Y1; 3% paid conv.; 4 consulting/yr; Cloud unlocked H2 Y2 | ~$52,000 | ~$20,000 | ~$72,000 | ~$270,000 | ~$50,000 | ~$320,000 | ~$47K (see §4) |

ASSUMPTION: bull Y1 assumes HN front-page (estimated +400 stars on a good day, strategy.md §4) and immediate SMB Crew uptake from Day 30. This is the optimistic tail, not the planning number.

ASSUMPTION: base-case Y1 ARR of ~$12K is consistent with market-sizing's bottom-up SOM ($5K Y1 ARR, base) adjusted upward by consulting cash which market-sizing explicitly excluded. The composite figure ($18K) is the planning number for cashflow purposes.

---

## 4. Cloud scenario (GATED / speculative — not the base case)

Cloud revenue is modelled only under the bull scenario and only from Y2 Q3 onward (ASSUMPTION: `tenant_id` ships H1 2027; Cloud go/no-go made Q1 2027; launch Q2 2027). Treat this section as a sensitivity, not a forecast.

| Gate | Status | Earliest unblock |
|---|---|---|
| `tenant_id` multi-tenancy (ADR-0004) | Unbuilt; ~6–8 wk engineering | H1 2027 at earliest |
| Per-tenant build-farm isolation + gVisor/Kata | Unbuilt | After `tenant_id` |
| External pen-test | Not started | After build-farm |
| Cloud go/no-go (ADR-0004 decision A) | Unmade product decision | Maintainer decision point |
| GDPR per-customer erasure | Blocked on `tenant_id` | After `tenant_id` |

If all gates clear by Q2 2027 (bull only):

| Period | Cloud Crew paying accounts | Cloud MRR | Cloud ARR | GM (77%) |
|---|---|---|---|---|
| Y2 Q3–Q4 (first 6 months post-launch) | 10–30 | $390–$1,170 | $4,700–$14,000 | $3,600–$10,800 |
| Y3 full year | 30–80 | $1,170–$3,120 | $14,000–$37,400 | $10,800–$28,800 |

ASSUMPTION: Cloud Crew ARPU = $39 base + minimal overage = ~$45/mo blended (unit-economics input). ASSUMPTION: Cloud churn = 3.5%/mo (unit-economics input); higher than self-hosted because Cloud is easier to cancel.

Cloud does not change the base case. Cloud ARR in the bull Y3 is ~$14–37K — material but not dominant. Self-hosted B0 licensing remains the ARR spine through Y3 in every scenario.

---

## 5. Sensitivity table — three swing variables

| Variable | Bear value | Base value | Bull value | Impact on base Y3 ARR if swung to bear | Impact if swung to bull |
|---|---|---|---|---|---|
| **Paid conversion rate** (A5) | 0.5% of active installs | 1.5% | 3.5% | −60% ($23K) | +133% ($136K) |
| **Stars-to-installs multiplier** (A3) | 2x | 4x | 6x | −50% ($29K) | +50% ($87K) |
| **Monthly churn** | 4.0%/mo | 2.0%/mo | 1.2%/mo | LTV drops to $866; −37% Y3 ARR if same customers | LTV rises to $2,879; +32% |

The single largest swing variable is paid conversion rate. A 0.5% conversion scenario produces bear-level revenue even with bull-level star counts. The paid conversion rate is driven by: (a) friction at the Explorer→Crew upgrade trigger (OIDC gate, replica limit); (b) quality of the 14-day trial experience; (c) whether consulting/word-of-mouth creates S2 warm leads.

---

## 6. Solo-maintainer cash and effort reality check

ASSUMPTION: maintainer billing rate for consulting = $150/hr (market-sizing uses $30/hr blended for content time; consulting is specialist work at a different rate). One $3K engagement = 20 hours. At 16 hrs/week total marketing + community capacity (strategy.md §8), a single consulting engagement consumes 1.25 weeks.

| Revenue line | Maintainer hours/unit | Units per quarter (base) | Quarterly capacity cost | Cash generated |
|---|---|---|---|---|
| B0 license (automated) | ~0.5 hr/new customer (onboarding) | 4–6 new customers | ~3 hrs | ~$588–$882 net new MRR |
| Consulting engagement | 20–40 hrs | 0–1 | 20–40 hrs | $3K–$6K |
| Sponsorship | ~1 hr setup; recurring | 1 setup | ~1 hr | $0–$200/mo |
| Content (required for funnel) | 4 hrs/wk | 13 wks | 52 hrs/quarter | No direct cash; drives top-of-funnel |

Reality: in Q1–Q2, licensing revenue is too small to replace maintainer income. The base case generates ~$4–5K total cash in Q1-Q2 combined (licensing + one consulting engagement). This is supplemental income, not a salary. The crossover toward self-sustaining cash (>$5K/mo) requires ~100 active Crew accounts — achievable in base Y3, not Y1.

The bear scenario never reaches self-sustaining cash in a 36-month window without Cloud or a Constellation deal.

ASSUMPTION: one Constellation deal at $5K–$15K ARR (unit-economics note) would materially shift the bear case. At near-100% self-hosted margin, a single enterprise consulting-to-license conversion changes the Y3 P&L. However, Constellation is gated on `tenant_id` multi-tenancy for team features — the same gate as Cloud. Treat Constellation ARR as speculative until post-Y2.

---

## Cross-team flags

**To market-sizing analyst:** the base-case Y1 ARR of ~$12K (licensing) is structurally consistent with your $5K Y1 ARR bottom-up figure only if consulting revenue is added back. If the base-case licensing-only number is $5K and consulting is zero, Y1 cash is insufficient to cover even the domain + video editing budget. The forecast team flags: the market-sizing SOM needs a consulting-revenue component, not just a star-funnel ARR figure, to be actionable as a planning input.

**To unit-economics analyst:** the 2.0%/mo churn assumption on self-hosted Crew is the model's most optimistic single input. There is no churn history for Cooker. If early cohorts churn at 5%/mo (typical for any new SaaS product before product-market fit), Y3 base ARR drops from ~$58K to ~$23K and the bull case loses its upside. Recommend the unit-economics analyst establish a churn monitoring trigger: if 90-day cohort retention falls below 80%, revisit the LTV model before Y2 projections are used for any spending decision.

**To pricing analyst:** the $39 Cloud base price creates a 77% GM at 2,000 build-min/mo (unit-economics). The forecast models Cloud ARPU at $45 blended. If the pricing analyst accepts the unit-economics recommendation to raise Cloud base to $49 (or reduce included minutes), Cloud ARR in the bull scenario rises ~25% without volume change. Flag: this is a meaningful swing and should be resolved before Cloud go/no-go.

**To business-model analyst (Cloud gate dependency):** this forecast explicitly does not model Cloud revenue in the base case. The business-model doc correctly identifies the unmade go/no-go (ADR-0004 decision A) as a prerequisite. Forecasting Cloud as base-case revenue before that decision is made would be misleading. The current document treats Cloud as a bull-only, post-Y2 scenario. If the go/no-go is made affirmatively and `tenant_id` is prioritised now, the forecast should be re-run with Cloud entering at Y2 Q3.

**To risk analyst:** the bear scenario is not a tail risk — it is the median outcome for a new OSS tool that fails to reach HN front page or sustains fewer than 400 stars in Y1. The bear case produces ~$4K total cash over 36 months (licensing only; no consulting; no Cloud). This is below any plausible sustainability threshold. The risk analyst should model "failed HN launch" as the prior probability, not the exception, and evaluate whether an alternative distribution channel (r/selfhosted, marketplace listings, KeepSave co-marketing) can floor the bear case above zero.

**To segmentation analyst:** the base-case funnel assumes 1.5% paid conversion of active installs. The segmentation WTP data supports $49/mo for S2 buyers ($500–$2K/mo ops budget). However, the $49 price sits well above Coolify Cloud ($5/mo) in the same buyer's mental anchor set. The segmentation analyst should validate whether the pricing page can successfully reframe $49/replica as "below the cost of one VPS node" — if that story does not land on the pricing page, actual conversion is more likely to track the 0.5–1% bear range than the 1.5% base.
