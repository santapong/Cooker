# Cooker — Market Sizing v1

> Author: cooker-mkt-market-sizing · Round 1 draft · Date: 2026-06-21
> Inputs: docs/marketing/strategy.md §1 §6, docs/product-plan.md §7, docs/launch/01-billing-monetization.md, docs/marketing/research/00-brief.md, README.md
> Every figure is either SOURCE: (cited with date) or ASSUMPTION: (labelled explicitly).

---

## 1. TAM — Total Addressable Market

Cooker lives at the intersection of two named markets: CI/CD tooling and self-hosted PaaS/DevOps automation. Both are cited below with dates; figures go stale within 12–18 months.

| Market | 2025/2026 Value | CAGR | Source |
|---|---|---|---|
| CI/CD Tools (narrow) | USD 13.2 B (2026) | ~8.2% | Persistence Market Research, 2026 |
| CI/CD & DevOps Toolchain Platforms (broad) | USD 17.0 B (2025) → USD 44.1 B (2030) | ~21% | Virtue Market Research, 2025 |
| DevOps Market (broadest) | USD 16.1 B (2025) → USD 51.4 B (2031) | ~21% | Mordor Intelligence, 2026 |

SOURCE (2026-06): [Persistence Market Research — CI/CD Tools Market](https://www.persistencemarketresearch.com/market-research/continuous-integration-and-delivery-ci-cd-tools-market.asp)
SOURCE (2025): [Virtue Market Research — CI/CD & DevOps Toolchain Platforms](https://virtuemarketresearch.com/report/ci-cd-devops-toolchain-platforms-market)
SOURCE (2026): [Mordor Intelligence — DevOps Market](https://www.mordorintelligence.com/industry-reports/devops-market)

**TAM used for this analysis: USD 13–17 B (2025–2026), CI/CD + DevOps toolchain combined.**

Cooker is not attempting to address all of this. Most of that TAM belongs to GitHub Actions, GitLab CI, Jenkins, Harness, and enterprise toolchains. The figures are cited to establish ceiling; the SAM below is the honest number.

---

## 2. SAM — Serviceable Addressable Market

Cooker's SAM is defined by four simultaneous filters applied to the TAM:

1. **Self-hosted / on-prem preference** — excludes hosted-SaaS-only buyers.
2. **Container/Kubernetes workflows** — excludes legacy bare-metal or Windows-native shops.
3. **SMB and indie developer scale** — excludes enterprises that require multi-tenancy, SAML, or hard team isolation (not Cooker today).
4. **Willingness to pay for a self-hosted infra tool** — excludes the purely YAML-locked crowd who will not switch editors.

### Sizing the self-hosted slice

SOURCE (2025): [JetBrains — State of CI/CD 2025](https://blog.jetbrains.com/teamcity/2025/10/the-state-of-cicd/) reports 55% of developers regularly use CI/CD tools; GitHub Actions leads at 33%, Jenkins at 28%, GitLab CI at 19%. Self-hosted solutions (Drone, Woodpecker, Jenkins, Gitea-based) collectively hold an estimated 25–35% of CI/CD installs by count (ASSUMPTION: derived from survey distribution; no authoritative self-hosted share figure is publicly available).

ASSUMPTION: The self-hosted preference segment = ~25% of the CI/CD TAM = ~USD 3.3–4.3 B addressable by tools competing on "run it yourself."

ASSUMPTION: The visual / SMB / indie sub-slice (Cooker's actual fit) = ~5–8% of that self-hosted segment. Rationale: the Coolify/Dokploy/CapRover comps together have ~100,000+ GitHub stars (Coolify ~55k, Dokploy ~31k, CapRover ~14k) SOURCE (2025–2026): [Coolify GitHub](https://github.com/coollabsio/coolify), [Dokploy/CapRover via MassiveGRID comparison, March 2026](https://massivegrid.com/blog/dokploy-vs-coolify-vs-caprover/) — representing a real but not enormous installed base relative to the full CI/CD market.

**SAM estimate: USD 165–345 M** (5–8% of the self-hosted CI/CD slice, rough 2026 dollars).

This is the universe of potential license or SaaS revenue from self-hosted-preferring, container-native, SMB/indie shops in a world where Cooker is fully adopted by every suitable buyer — a ceiling, not a projection.

---

## 3. SOM — Serviceable Obtainable Market (Year 1–3)

SOM is derived bottom-up (see §4) and cross-checked against the strategy.md targets. The top-down derivation is shown here for completeness only; the bottom-up number is the operative figure.

**Top-down (illustrative only):** 0.1–0.5% of SAM USD 165–345 M = USD 165 K–1.7 M at year 3. This range is too wide to be useful; the bottom-up cross-check anchors it.

---

## 4. Bottom-Up Cross-Check: OSS Adoption Funnel

### 4.1 Comparable: Coolify

Coolify is the closest public comp — an OSS self-hosted PaaS that lacks a visual CI pipeline DAG (Cooker's differentiator) but targets the same indie/SMB self-hosted audience.

| Metric | Value | Source |
|---|---|---|
| GitHub stars (May 2026) | ~55,700 | [Coolify GitHub](https://github.com/coollabsio/coolify), verified May 2026 |
| Reported user base | 325,000+ | [Multiple reviews citing Coolify's own claims, March 2026](https://temps.sh/blog/coolify-review-2026) |
| Discord members | 20,375+ | [Coolify GitHub / community, May 2026](https://github.com/coollabsio/coolify) |
| Cloud MRR (hosted tier) | ~$15,000/mo | [Multiple review sources, 2025–2026](https://www.srvrlss.io/provider/coolify/) |
| GitHub Sponsors MRR | ~$4,500/mo | [Open Collective / review sources, 2025](https://opencollective.com/coollabsio) |
| Total monthly revenue (est.) | ~$20,000/mo (~$240 K ARR) | ASSUMPTION: GitHub Sponsors + Cloud MRR combined |

**Implied conversion ratios from Coolify:**
- Stars → reported users: ~325,000 / 55,700 = ~5.8x (ASSUMPTION: "users" includes anyone who has touched the tool, not just active installs; treat this as installs-ever, not active)
- Stars → active paying (Cloud): ~3,000 cloud users / 55,700 stars = ~5.4% (ASSUMPTION: "3,000 Cloud users" is an approximate figure from review sources)
- Active paying → MRR: 3,000 users × $5/mo avg (Cloud lowest tier) = $15K MRR — consistent with cited figures

### 4.2 Funnel stages applied to Cooker

Cooker's strategy.md §6 targets: 1,000 stars by day 90 post-launch. Below are bear/base/bull projections for years 1–3, keyed to star accumulation.

| Stage | Conversion | Bear | Base | Bull |
|---|---|---|---|---|
| **GitHub stars Y1** | — | 1,000 | 2,500 | 5,000 |
| **GitHub stars Y3** | — | 3,000 | 8,000 | 20,000 |
| **Installs (Y3)** | ASSUMPTION: 3–5x stars (vs. Coolify's ~6x, discounted for newer project) | 9,000 | 32,000 | 80,000 |
| **Active / production installs** | ASSUMPTION: 15–25% of installs are active (rest are "tried it once") | 1,350 | 8,000 | 20,000 |
| **Paying (self-hosted license, Crew @ $49/replica/mo)** | ASSUMPTION: 1–3% paid conversion of active installs; Coolify Cloud shows ~5% but Cooker's B0 license (self-hosted) faces higher friction than Coolify's $5/mo Cloud tier | 14 | 240 | 600 |
| **MRR @ $49/replica/mo** | — | $686 | $11,760 | $29,400 |
| **ARR Y3** | — | ~$8 K | ~$141 K | ~$353 K |

ASSUMPTION: average paying account = 1 replica (Explorer → Crew upgrade, single-replica production). SMB multi-replica accounts are the bull scenario tail.

ASSUMPTION: no Cooker Cloud revenue in Y1–Y2 (product-plan.md §7 and launch/01-billing-monetization.md are explicit: hosted billing requires tenant_id multi-tenancy, 6–8 weeks unbuilt, deferred to Q4 2026 at earliest). Cloud revenue excluded from Y1–Y2; included at small scale only in bull Y3.

ASSUMPTION: consulting/support (product-plan §7 rung 3) is excluded from this SOM; it is not recurring and cannot be forecast from a star funnel.

### 4.3 Cross-check: Coolify-comparable revenue trajectory

Coolify reached ~$240 K ARR with ~55,700 stars and ~5 years of runway. Scaled to Cooker's Y3 base-case star count of 8,000:

- Coolify ARR per star: $240K / 55,700 = ~$4.31/star
- Cooker Y3 base: 8,000 stars × $4.31 = ~$34 K ARR

This is more conservative than the $141 K base-case above because Coolify's monetization runs on a $5/mo Cloud tier (very low friction), while Cooker's B0 license is $49/replica/mo (higher ACV, lower volume). The two inputs bracket a plausible range.

**Composite SOM (Y3, base case): USD 35–140 K ARR.** The $35 K figure is the Coolify-trajectory extrapolation; the $141 K figure is the funnel model at 1–3% paid conversion. Both are plausible depending on how fast the self-hosted license gains distribution.

### 4.4 Summary table

| Scenario | Y1 ARR | Y3 ARR | Notes |
|---|---|---|---|
| Bear | <$1 K | ~$8 K | HN misses, slow distribution, no external contributors |
| Base | ~$5 K | ~$35–140 K | 1,000 Y1 stars per strategy.md; 1–2% paid conversion; B0 license only |
| Bull | ~$20 K | ~$350 K | Strong show-HN; consulting revenue layered in; early Crew customers; Cloud unlocked by Y2 |

These are pre-consulting, pre-sponsorship figures. Sponsorship (rung 2 of product-plan §7) could add $2–10 K/yr; consulting (rung 3) is uncapped but non-recurring.

---

## 5. Key Assumptions Register

| # | Assumption | Owner for validation |
|---|---|---|
| A1 | Self-hosted share of CI/CD market = 25–35% | cooker-mkt-segmentation (pull survey data) |
| A2 | Cooker's addressable visual/SMB slice = 5–8% of self-hosted segment | cooker-mkt-segmentation + cooker-mkt-competitor |
| A3 | Stars-to-installs multiplier = 3–5x for a pre-2-year project | cooker-mkt-competitor (check Dokploy, Woodpecker pull data) |
| A4 | Active / production install rate = 15–25% of total installs | cooker-mkt-growth (activation funnel) |
| A5 | Paid conversion rate = 1–3% of active installs (self-hosted license) | cooker-mkt-unit-economics + cooker-mkt-pricing (friction vs. Coolify's $5/mo) |
| A6 | No Cloud revenue Y1–Y2 (tenant_id gate) | product-plan.md §7 + launch/01-billing-monetization.md — hard constraint, not assumption |
| A7 | Average paying account = 1 replica ($49/mo) | cooker-mkt-pricing |
| A8 | Coolify ARR/star ratio is the right comp; Dokploy has no public revenue | cooker-mkt-competitor |

---

## Cross-team flags

- **cooker-mkt-forecast**: use the bear/base/bull ARR table in §4.4 as the Year 1–3 revenue inputs. Key sensitivity drivers are (A3) stars-to-installs and (A5) paid conversion — run a sensitivity table against both. The $49/replica ACV is committed (billing doc); do not vary it.
- **cooker-mkt-segmentation**: assumptions A1 and A2 need real survey/survey-proxy data to tighten the SAM range from USD 165–345 M. The current range is a 2x spread; tightening it to <1.5x would materially improve the forecast's credibility.
- **cooker-mkt-competitor**: validate A3 (stars-to-installs) and A8 (Coolify as the right comp) against Dokploy's pull/install data. Dokploy reached 31k stars with no reported public revenue — if it has zero paying users, the bull case above is too optimistic. Woodpecker CI (~4k stars) is another comp for the pure-CI niche but appears non-commercial.
- **cooker-mkt-unit-economics**: A5 (1–3% paid conversion) drives the entire SOM range. Sensitivity: if Crew adoption runs at 0.5% (friction from a $49 price on a young product), Y3 base ARR drops to ~$25 K; if it runs at 5% (strong product-market fit + consulting-led sales), Y3 exceeds $350 K. Payback and CAC are zero at OSS-led growth; margin analysis should focus on the consulting/support lane as the first real-cash rung.
- **cooker-mkt-business-model**: the Cloud gate (A6) is a hard binary. If tenant_id ships in Q4 2026, Cloud MRR could meaningfully shift Y2–Y3 projections upward; if it slips, B0 self-hosted license is the only Y1–Y2 revenue surface. Flag this dependency prominently in the business-model doc.
- **cooker-mkt-risk**: the bear scenario is not a tail risk — it is the median outcome for a new OSS tool that does not reach Show HN front page. The risk analyst should model the "failed launch" path as the starting point, not the exception.
