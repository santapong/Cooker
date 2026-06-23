# Cooker — Business Model v1

**Author:** business-model analyst · **Round:** 1 (independent draft) · **Date:** 2026-06-21
**Spine doc:** `docs/product-plan.md` §7 · **Launch lanes:** `docs/launch/README.md` + `01-billing-monetization.md`

---

## 1. Model options weighed

| Revenue model | Fit | Effort | Blockers | Time to first dollar | Verdict |
|---|---|---|---|---|---|
| **B0 — Self-hosted open-core licensing** (offline Ed25519 keys) | High. Cooker's entire ICP is self-hosters; the Crew/Constellation feature set maps directly to what ops teams pay for. | ~4.5 d | None. No `tenant_id`, no Stripe, no pen-test required. | Weeks after launch if star traction arrives. | **Ship first. Highest near-term ROI.** |
| **Consulting / support** (deploy, operate, migrate) | High for a solo-maintainer. Runbooks (`ROLLOUT.md`, `RUNBOOK.md`, `MULTI_REPLICA.md`) already exist and are operator-facing; credibility is built in. | Near-zero product work; ~0.5 d to write a services page. | None — can offer today; billing is a bank transfer or Stripe Payment Link. | Immediate if one customer shows up. | **First real cash. Run in parallel with B0.** |
| **GitHub Sponsors / Open Collective** | Moderate. Signals community health; attracts contributors. Coolify, Woodpecker, and Woodpecker's foundation-level sponsors all demonstrate that self-hosted tools attract recurring $10–200/mo tiers. | ~0.5 d (`FUNDING.yml` + tiers text). | None. | $0–200/mo realistically in the first 90 days. | **Signal + contributor magnet. Not income.** |
| **B1/B2 — Hosted SaaS ("Cooker Cloud")** (Stripe Checkout + usage meters) | High long-term. Cloud removes the ops burden for SMB Crew buyers. | ~10 d billing work alone (B1: 8 d; B2: 2.25 d), but that is dwarfed by the 6–8-week `tenant_id` multi-tenancy build underneath it. | **HARD GATE: `tenant_id` (ADR-0004, deferred), per-tenant build-farm isolation, gVisor/Kata sandbox, external pen-test, Cloud go/no-go (ADR-0004 decision A, still unmade).** Also a GDPR prerequisite: per-customer erasure requires `tenant_id`. | 4–6 months minimum from today, assuming the Cloud go/no-go is made now. | **Do not start without the gates. Plan for it; do not build it yet.** |
| **Marketplace listings** (DigitalOcean 1-click, Linode, Artifact Hub Helm chart, `awesome-selfhosted`) | Moderate. Distribution multiplier; not a revenue stream, but feeds the self-hosted licensing funnel. | ~1 d each (DO/Linode app images are template submissions; Artifact Hub is a `annotations` addition to the Helm chart). | None. Artifact Hub requires a published OCI chart (GoReleaser/`helm push`). | Zero direct revenue; compounds B0 funnel over months. | **Do after v0.1.0 ships GoReleaser artifacts.** |
| **Dual licensing / relicense** | Low near-term. Requires consolidated copyright (CLA or full contributor copyright assignment) and a license switch decision. HashiCorp's BSL move and Elastic's oscillation between SSPL/AGPLv3/ELv2 warn that relicensing after external contributions lands badly with communities (see [Goodwin Law 2024](https://www.goodwinlaw.com/en/insights/publications/2024/09/insights-practices-moving-away-from-open-source-trends-in-licensing)). | Months of legal work + community cost. | Community backlash risk; difficult after external PRs land without a CLA already in place. | Speculative. | **Not recommended at this stage. Open-core (below) achieves the same commercial goal without relicensing the core.** |

---

## 2. Recommended mix and sequence

The sequence maps directly onto the launch lanes. No step requires the next step to begin.

```
NOW (0–3 months)
  Lane A   Harden & launch (OSS, best-effort) ──────────────────────────────▶
  Lane B0  Self-hosted licensing (4.5 d)        ──────────────────────────▶ first commercial revenue
  Parallel Consulting / support services        ──────────────────────────▶ first real cash
  Parallel GitHub Sponsors / Open Collective    ──────────────────────────▶ signal + contributor signal

LATER (3–6 months, gated)
  Lane B1  Stripe Cloud billing   ─┐
  Lane B2  Build-minute meters    ─┤── ALL blocked on tenant_id (6–8 wk) + pen-test + Cloud go/no-go
  SaaS     Cooker Cloud hosting   ─┘

DISTRIBUTION (after v0.1.0 artifacts)
  Marketplace listings, awesome-selfhosted, Artifact Hub, DO 1-click
```

### Why this sequence

**B0 before Cloud:** The launch doc states this plainly — B0 has zero dependency on `tenant_id` and can ship in 4.5 days. Cloud billing requires `tenant_id` (ADR-0004), a multi-tenant build farm isolated enough to run untrusted code, and an external pen-test. Those are 6–8 weeks of engineering before the first Stripe subscription can be safely issued. Building Cloud speculatively before the go/no-go decision (ADR-0004 decision A, currently unmade) violates the product-plan §7 anti-goal against "solo-operated paid SaaS on today's codebase."

**Consulting before licensing:** Consulting requires zero product code. If any enterprise or SMB team contacts us after the launch post, a consulting/support offer can go out the same day. It de-risks the period between launch and the first license sale.

**Sponsorship as signal:** Coolify runs GitHub Sponsors alongside a paid cloud tier; Dokploy has reached 31k stars (June 2026, per MassiveGRID comparison) partly through open-source goodwill. At Cooker's current scale, sponsorship income is immaterial but the sponsor count and tier mix are a leading indicator of paid conversion intent. ASSUMPTION: `cooker-mkt-partnerships` will size the realistic sponsor cohort.

**Marketplace after GoReleaser:** `awesome-selfhosted` and the Artifact Hub listing require published OCI/Helm artifacts. This is a launch-precondition already named in `docs/marketing/strategy.md` §4. Once that lands, marketplace listing is a half-day task with compounding funnel benefit.

---

## 3. Open-core line: OSS vs. commercial features

The core is and must stay OSS (MIT today; see §3.3 on license choice). The commercial features are exactly the §5 Tier 3 deferrals in `docs/product-plan.md` — features that have enterprise-sales value but do nothing for star count:

| Feature | OSS (Explorer / free) | Commercial (Crew/Constellation license) |
|---|---|---|
| Visual DAG pipeline editor | Yes | — |
| Single-replica CI/CD (build, push, deploy) | Yes | — |
| OIDC PKCE + 4-role RBAC | Yes | — |
| Postgres secrets backend (AES-GCM) | Yes | — |
| Webhook auto-deploy | Yes | — |
| Pipeline-as-code YAML | Yes | — |
| `cookerctl` CLI | Yes | — |
| Basic audit trail (stdout / DB) | Yes | — |
| Cron-triggered runs | No | Crew |
| Multi-replica HA (`max_replicas > 1`) | No | Crew (soft-warn, not hard-fail) |
| Vault / AWS / GCP / KeepSave secrets backends | No | Crew |
| OIDC MFA step-up | No | Crew |
| Full OTLP tracing + append-only audit export | No | Crew |
| 90-day run retention (vs 7-day OSS) | No | Crew |
| SSO group→role mapping | No | Constellation |
| Multi-tenant teams + per-team RBAC | No | Constellation (blocked on `tenant_id`) |
| Air-gapped deployment support | No | Constellation |
| SAML | No | Constellation (unbuilt; deferred) |
| SLA + priority support | No | Constellation |
| Image vulnerability scanning / SBOM | No | Constellation (unbuilt; deferred) |
| PR-preview environments | No | Constellation (unbuilt; deferred) |
| Compliance audit exports | No | Constellation (unbuilt; deferred) |

ASSUMPTION: final feature-gate list is owned by pricing (`cooker-mkt-pricing`) and the PM; this table is the starting point for their tier mapping, not the contract.

### 3.1 Source-tree split

The launch doc §7 names this as an open question. Current industry norm (as of 2025–2026): the cleanest open-core split keeps commercial features in a **separate directory or Go module** (`internal/billing/`, `internal/enterprise/`) with a build tag (e.g. `//go:build enterprise`) so the OSS binary compiles clean without them. This is the approach used by projects like Grafana (OSS vs. Grafana Enterprise) and is recommended by Open Core Ventures ([OCV Handbook](https://handbook.opencoreventures.com/startup-manual/fundamentals/licensing-and-distribution/)).

For Cooker, the `internal/billing/license/` path already scoped in `docs/launch/01-billing-monetization.md` §4 is the natural home. The entitlements engine (`internal/billing/entitlements.go`) can live in the OSS tree — it only maps plan IDs to feature structs; the commercial *enforcement* is in the same package but behind a build tag. The binary ships as either `cooker` (OSS, Explorer-only) or `cooker-enterprise` (all tiers); operators who buy a license run the enterprise binary.

ASSUMPTION: the exact build-tag vs. separate-module tradeoff is a risk/legal question; `cooker-mkt-risk` should confirm.

### 3.2 CLA requirement

Current norm (2025–2026): open-core does **not** require a CLA for the OSS core if the OSS license stays fixed (e.g. MIT stays MIT). A CLA is only required if the maintainer wants to relicense community contributions into the commercial tier — i.e., dual-license mode. Since Cooker's commercial features are **new code written by the maintainer** (not relicensed community PRs), no CLA is strictly required at B0 launch.

However: `docs/product-plan.md` §7 warns that "relicensing after outside contributions is painful" and recommends adding a CLA now if open-core is plausible. The practical recommendation is a **lightweight CLA** (Apache-style, or HashiCorp's model: contributor grants the maintainer a broad but non-exclusive license, contribution stays under MIT for community) applied from the first external PR. The [HashiCorp CLA model](https://www.hashicorp.com/en/blog/introducing-a-cla) is the cleanest precedent for this exact shape.

ASSUMPTION: `cooker-mkt-risk` owns the final CLA recommendation and any IP-ownership implications.

### 3.3 License choice (OSS core)

`docs/product-plan.md` §7 says "keep the core Apache-2.0." The current codebase is MIT (per `docs/marketing/strategy.md` HN draft: "MIT"). This needs resolution before B0 ships. Both work for open-core; Apache-2.0 has explicit patent grants that matter for enterprise buyers. ASSUMPTION: PM decides; this analyst notes that Apache-2.0 is the safer enterprise-facing choice (patent grant) and the one competitors like Woodpecker, Argo, and Tekton use.

---

## 4. Gated items (explicit flags)

Any recommendation that touches hosted Cloud revenue is **flagged as gated**:

- Cloud SaaS (B1/B2) is gated on: `tenant_id` unbuilt (~6–8 wk engineering), per-tenant build-farm isolation + gVisor/Kata runtime sandbox, external pen-test, and the unmade Cloud go/no-go (ADR-0004 decision A).
- The Constellation multi-tenant tier feature set (teams, per-team RBAC, SAML, PR-preview environments) is also gated on `tenant_id` even in the self-hosted licensing model.
- GDPR per-customer erasure is gated on `tenant_id` (it is the FK for the data boundary).

Do not open public Cloud signups before all four gates clear.

---

## Cross-team flags

- **`cooker-mkt-competitor`** (in): confirm whether Drone (Harness tier), Woodpecker, and Coolify Cloud use per-replica or per-seat billing, and whether any of them have a CLA in place. This directly informs the open-core line and the tier-price calibration.
- **`cooker-mkt-pricing`** (consumes this doc): the feature/tier table in §3 is the input for pricing tiers. The per-replica billing axis (Crew: $49/replica/mo; Cloud proposed: $39/mo + build-minutes) is the committed shape from the mockup — pricing should validate against willingness-to-pay data before locking it.
- **`cooker-mkt-forecast`** (consumes this doc): two distinct revenue lines to model — (1) self-hosted licensing (B0): unit = licenses sold × tier price; lags star count by a conversion rate; (2) Cloud SaaS (B1/B2): unit = Crew subscriptions × $39/mo + build-minute overages. Cloud line is a 2H 2026 or later scenario; label it clearly as gated/speculative in the forecast.
- **`cooker-mkt-risk`** (flag): (a) confirm CLA requirement and IP ownership structure before B0 ships; (b) confirm the build-tag vs. separate-module approach for the source-tree split satisfies the OSS-core promise under MIT/Apache-2.0; (c) size the pen-test cost and timeline dependency for Cloud go/no-go.
- **`cooker-mkt-partnerships`** (flag): (a) GitHub Sponsors / Open Collective tiers — what amounts and perks are realistic given the self-hosted-PaaS comp set (Coolify, Woodpecker)? (b) marketplace partnerships (DigitalOcean 1-click, Linode) — submission lead time and listing requirements; (c) any co-marketing or bundling with KeepSave (already a shipped secrets backend) that could be a sponsorship channel.
