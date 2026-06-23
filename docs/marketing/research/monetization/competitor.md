# Competitor Monetization Benchmark

> Research date: 2026-06-21. Prices sourced from live web searches; cited below each entry.
> Scope: revenue models and price points only. Feature parity is the README's job.
> Author: competitive-monetization analyst (Round 1).

---

## Core comparison table

| Tool | Category | Monetization model | Free tier | Paid entry point | Paid ceiling | Revenue source notes |
|---|---|---|---|---|---|---|
| **GitHub Actions** | SaaS CI | Platform lock-in; usage-based add-on on top of per-seat GitHub plan | 2,000 min/mo (Free); 3,000 (Pro); 50,000 (Team) | $4/user/mo (GitHub Pro, personal) / $4/user/mo (Team) | Enterprise: custom | Linux runner $0.006/min; Windows $0.010/min; macOS $0.062/min (post-Jan 2026 cut of up to 39%). Minutes/storage are upsell; the CI layer itself is not priced separately. Runners on public repos stay free. |
| **GitLab CI** | SaaS CI / SCM | Per-seat SaaS; CI minutes included in seat | 400 compute-min/mo, 5-user group cap | $29/user/mo (Premium, billed annually) | Ultimate: $99/user/mo | CI is bundled; the seat buys SCM + PM + CI/CD together. Self-managed CE is MIT-free. |
| **Buildkite** | SaaS CI (BYOC) | Per-seat; bring-your-own compute; OSS layer for agents | Free up to 5 users, 3 agents | $15/user/mo (Pro) | Enterprise: custom | "Pay for orchestration, run compute yourself." Agents are Apache-2.0 OSS; the coordination plane is the paid product. No minute meter — cost scales with seats, not builds. |
| **CircleCI** | SaaS CI | Credit/compute consumption + per-seat component | 6,000 credits/mo, 1 concurrent job | $15/mo base + $15/additional user (Performance) | Scale: $2,000/mo | Per-execution model; credits consumed by machine size × time. Scale tier adds audit logs, GPU classes, dedicated support. |
| **Harness** | Enterprise CI/CD platform | Per-developer SaaS + enterprise licence; modular (CI, CD, FF, CCM, STO each separately priced) | 2,000 cloud credits/mo (Free) | ~$50–100/developer/mo (Team/Essentials, CI module) | Enterprise: custom; median contract ~$48k/yr | Drone CI (acquired 2020) folded into Harness CI module. OSS Drone community edition still exists but has no active commercial path independent of Harness. "Developer 360" pricing launched 2025 to simplify per-developer billing. |
| **Drone CI** | OSS CI (now Harness-owned) | OSS core (Apache-2.0) + commercial via Harness acquisition; legacy Enterprise edition ~$299/mo was reported but is effectively superseded by Harness pricing | OSS self-host: free | Harness path above | — | Drone.io community edition is free/self-host. If you want support or enterprise features, you buy Harness. The independent Drone Enterprise pricing is stale post-acquisition. |
| **Woodpecker CI** | Pure OSS CI | Sponsorship-only (no paid product) | Fully free (MIT/Apache) | n/a | n/a | GitHub Sponsors + Open Collective. As of 2026-06: ~$506 from GitHub Sponsors; Open Collective total raised ~$6,100; current balance ~$4,100. No cloud product, no enterprise tier, no dual-license. Sustained by ~10 volunteer maintainers. |
| **Argo Workflows / CD** | OSS K8s CI/CD | CNCF OSS (Apache-2.0); monetised via Akuity managed cloud | Fully free (self-host) | Akuity: $0 (starter) up to ~100 apps; add-on packs $99/mo per 10 Argo CD apps + 10 Kargo stages + 5M AI tokens | Akuity Enterprise: custom | Akuity is the VC-backed commercial vehicle for Argo (founded by Argo creators). AWS/Azure/GCP Marketplace listings. No dual-license; OSS layer is untouched. |
| **Coolify** | Self-hosted PaaS | OSS (Apache-2.0) + paid cloud management plane + sponsorships | Self-host: free forever, unlimited servers/apps | Cloud: $5/mo (up to 2 servers); each additional server $3/mo | No upper tier published; multi-team = additional $5/mo subscriptions | MRR breakdown (per public transparency, ~2025): GitHub Sponsors ~$4,500/mo; Open Collective ~$1,200/mo; Coolify Cloud ~$15k+/mo (~3,000 cloud users). Cloud manages the control plane only; user brings own VPS. 20% annual discount (~$4/mo). |
| **Dokploy** | Self-hosted PaaS | OSS (Apache-2.0) + paid cloud (Dokploy Cloud); VC-backed (Polychrome) | Self-host: free | Cloud Hobby: $4.50/server/mo | Cloud Startup: $15/mo (3 servers); Enterprise/Agency: custom | Newer than Coolify (founded 2024); 26k+ GitHub stars. Cloud tier adds backups, scheduled jobs (Startup), SSO/SAML, audit logs (Enterprise). Annual billing saves 20%. |
| **CapRover** | Self-hosted PaaS | Pure OSS (MIT); no commercial offering | Fully free | n/a | n/a | Community PRs only; founding developer no longer active. No sponsorship platform found. Sustained purely by volunteer contributions. Slowest release cadence of the three PaaS tools. |

Sources (all accessed 2026-06-21):
- GitHub Actions: [GitHub changelog Dec 2025](https://github.blog/changelog/2025-12-16-coming-soon-simpler-pricing-and-a-better-experience-for-github-actions/); [CICDCalculator](https://cicdcalculator.com/github-actions)
- GitLab CI: [CostBench](https://costbench.com/software/developer-tools/gitlab/)
- Buildkite: [CICDCost.com](https://cicdcost.com/buildkite-pricing); [CICDCalculator](https://cicdcalculator.com/buildkite)
- CircleCI: [circleci.com/pricing](https://circleci.com/pricing/); [Vendr](https://www.vendr.com/marketplace/circleci)
- Harness: [harness.io/pricing](https://www.harness.io/pricing); [Harness Developer 360 blog](https://www.harness.io/blog/introducing-developer-360-pricing-by-harness); [Vendr](https://www.vendr.com/marketplace/harness)
- Drone: [docs.drone.io/enterprise](https://docs.drone.io/enterprise/)
- Woodpecker: [opencollective.com/woodpecker-ci](https://opencollective.com/woodpecker-ci)
- Argo / Akuity: [akuity.io/pricing](https://akuity.io/pricing)
- Coolify: [coolify.io/pricing](https://coolify.io/pricing); [coolify.io/sponsorships](https://coolify.io/sponsorships)
- Dokploy: [dokploy.com/pricing](https://dokploy.com/pricing); [Vyomcloud breakdown](https://www.vyomcloud.com/blog/dokploy-pricing-free-vs-paid-plans/)
- CapRover: [srvrlss.io/provider/dokploy](https://www.srvrlss.io/provider/dokploy/) (comparison context)

---

## Monetization model patterns (synthesised)

### Pattern A: Platform-lock SaaS (GitHub Actions, GitLab CI)

CI/CD is bundled with the SCM. The product is the developer platform; CI is included to prevent churn to competitors. Pricing is per-seat or per-minute; neither is particularly cheap at scale. These tools win on network-effects and ecosystem, not price or flexibility. They are structurally inaccessible to Cooker — we are not a code-hosting platform. The relevant lesson: free public-repo CI creates a massive top-of-funnel that normalises "CI should be free."

### Pattern B: Orchestration SaaS, bring-your-own compute (Buildkite)

The commercial product is the coordination plane; the agent layer is Apache-2.0 OSS. Per-seat billing decouples revenue from compute volatility. This is the cleanest structural parallel to a future Cooker Cloud offering: Cooker would sell the management plane, and users supply the build-pod infrastructure (Kaniko/BuildKit on their own cluster). Buildkite's Pro at $15/user/mo is a useful ceiling benchmark for "what teams accept for CI orchestration-only."

### Pattern C: Credit/usage metering (CircleCI)

Billing by compute credit consumed during runs. Revenue scales with usage but creates anxiety-inducing invoices. CircleCI has faced sustained user complaints about unpredictable bills; that dissatisfaction is an opening for fixed-price alternatives. Harness has moved away from this (Developer 360) partly for the same reason.

### Pattern D: Enterprise platform (Harness)

Full-platform per-developer pricing across CI, CD, feature flags, cost management, security testing. Typical buyer is 50–200-person eng org. Median contract ~$48k/yr. Effective floor for an enterprise decision is ~$50/developer/mo for a CI module only. This is the upper-end comps tier for Cooker's Constellation (custom), not Crew.

### Pattern E: OSS core + managed cloud plane (Coolify, Dokploy, Akuity)

The OSS layer is genuinely free and unlimited. The paid product is operational convenience: Coolify and Dokploy sell management of the control plane itself ($4.50–$5/server/mo); Akuity sells a managed Argo CD cluster ($99/10-app addon packs). None of these monetize the CI DAG or the build step — they monetize the deploy and operations layer. This is the closest structural analogue to Cooker's self-hosted + optional-cloud model.

### Pattern F: Pure-OSS sponsorship (Woodpecker, CapRover)

Sub-$10k/yr in total funding. Sustainable only if maintainers are volunteers; breaks instantly if they churn. Woodpecker is the healthiest of these (~$5,700/mo equivalent in combined sponsors if the balance figures are representative); CapRover has effectively stalled. This is the pre-revenue phase Cooker is in today.

---

## Positioning gaps: where Cooker's hybrid can price differently

**1. The visual DAG is not priced anywhere.**
No competitor charges for the pipeline *editor*. Buildkite, CircleCI and Harness bill for orchestration/compute; Coolify/Dokploy bill for server management. Cooker's graph editor is a differentiated UX that can justify a modest premium over a raw YAML CI tool, especially for the Crew persona (SMB platform teams) who would pay for reduced cognitive load on pipeline authoring.

**2. Per-replica is anomalous but genuinely defensible for self-hosted.**
Coolify's $5/server/mo and Dokploy's $4.50/server/mo are the market reference for "what does a control-plane subscription cost?" Cooker's proposed $49/replica/mo Crew tier is 10x the Coolify/Dokploy price point, but Cooker delivers significantly more (build, push, deploy, visual editor, OIDC/RBAC, secrets backends) versus PaaS-only tools that handle only deploy/hosting. The premium is justifiable if the value narrative lands; it is a risk if buyers compare shelf price without reading the feature delta.

**3. No seat tax creates a real SMB wedge.**
Every SaaS competitor (Buildkite $15/user, CircleCI $15/user, GitLab $29/user, Harness $50–100/developer) adds per-seat fees that compound painfully at 20–50 person teams. Cooker's mockup commits to unlimited seats on all paid tiers. A 30-person team on Buildkite Pro pays $450/mo (30 × $15) before touching compute; on Cooker Crew they pay $49/replica (likely one or two replicas). That is a concrete, quotable price difference for the Crew ICP.

ASSUMPTION (owned by cooker-mkt-pricing): the $49/replica/mo Crew price is treated as committed from the design mockup. If the pricing agent revises this figure, the competitive delta above must be recalculated.

**4. Coolify Cloud is the closest analogous product — and its ceiling is $5/server.**
Coolify's cloud offering generates ~$15k MRR from ~3,000 paying users. That implies an average of ~$5/user/mo — almost all on the base plan. This tells the market that self-hosted-PaaS buyers are extremely price-sensitive and that $5/server is near the ceiling of frictionless adoption. Cooker is positioned above this (Crew at $49/replica) which requires the Crew pitch to clearly articulate the CI+PaaS+visual stack as justification for the premium. If buyers evaluate Cooker as "a Coolify with CI bolted on," $49 will feel expensive. If they evaluate it as "a Buildkite with a deploy story and no seat fees," $49 feels cheap.

**5. The deploy story is a gap in every pure-CI competitor.**
Buildkite, CircleCI, and GitHub Actions all stop at the artifact boundary. You build the image; you figure out deployment yourself. Cooker's native K8s / ECS / Cloud Run / Fly / Render / SSH deploy targets — in the same DAG as the build — have no direct price comparison in the CI market. This is closer to Harness territory (CI+CD combined) but at a fraction of the enterprise price and with a self-host option Harness does not offer at the SMB level.

---

## Cross-team flags

- **cooker-mkt-pricing:** Coolify Cloud ($5/server/mo, ~$15k MRR) is the floor anchor for self-hosted-PaaS pricing; Buildkite Pro ($15/user/mo, no minute meter) is the floor anchor for CI-orchestration-only pricing. Crew at $49/replica has a 10x premium over Coolify on list price; the pricing narrative needs to justify this gap explicitly. Recommend the pricing agent model a 30-person team scenario comparing Cooker Crew vs. Buildkite Pro vs. CircleCI Performance to make the no-seat-tax argument concrete.

- **cooker-mkt-business-model:** The OSS+cloud pattern (Coolify MRR structure: sponsorship $5.7k/mo + cloud $15k/mo) is the most actionable analogue for Cooker's Phase B0 → sponsorship → B1 Cloud ladder from `01-billing-monetization.md`. Woodpecker's sub-$6k total-raised in several years is a cautionary floor — sponsorship alone does not fund a product. Coolify's ~$15k Cloud MRR from ~3,000 users demonstrates the self-hosted-PaaS market can generate real revenue, but at low ARPU. The business-model agent should flag that Coolify achieves $15k MRR on a $5/mo product (needing 3,000 paying users) whereas Cooker targets $49/mo (needing ~306 Crew replicas to match). Getting to 306 paying replicas requires substantially more adoption than 3,000 Coolify users paying $5.

- **cooker-mkt-market-sizing:** Traction comparables: Coolify ~65k GitHub stars (2026), 3,000+ cloud users; Dokploy ~26k stars (founded 2024, faster trajectory); CapRover ~13k stars (plateaued); Woodpecker ~4.5k stars; Buildkite and CircleCI are private (no star proxy). The self-hosted PaaS niche has demonstrated 10k–65k star acquisition potential for well-positioned tools. The market-sizing agent can use Coolify's $15k MRR / 3,000 users as a public data point for self-hosted-PaaS TAM calibration.
