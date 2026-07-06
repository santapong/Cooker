# 00 — Shared brief (marketing & monetization research team)

> Written by the CMO orchestration for Round 1. Every specialist reads this first so we all reason
> from the same facts. Source docs: `docs/marketing/strategy.md`, `docs/product-plan.md` §7,
> `docs/launch/README.md` + `docs/launch/01-billing-monetization.md`, `docs/audits/W11-user-journeys.md`,
> `README.md`. Date: 2026-06-21.

## Objective
Produce a coherent, honest go-to-market + monetization plan for **Cooker** that extends (never
contradicts) the existing strategy/launch/product-plan docs. Output goes under
`docs/marketing/research/`. This is research/strategy only — no product code is touched.

## Product in one line
**Cooker is an open-source, single-Go-binary CI/CD tool with a graph-based (drag-drop) pipeline
editor that builds OCI images (Kaniko/BuildKit/Buildah), pushes to any registry, and deploys to
Kubernetes / ECS / Cloud Run / Fly / Render / SSH — with multi-environment promotion + approval
gates, OIDC/RBAC/MFA, and pluggable secrets backends.** MIT-licensed. Tagline in use: "CI/CD you can see."

## The one differentiator to lead with
**Graph-first UX for self-hosted CI/CD + a real deploy story.** The self-hosted-PaaS niche (Coolify,
Dokploy, CapRover) has proven star traction but **none have a real pipeline DAG editor**; the
K8s-native crowd (Argo, Tekton) has no drag-drop editor. Cooker's defensible position = "visual graph
CI/CD + Coolify-style self-hosted PaaS in one binary."

## ICP / personas (from W11 + strategy.md §1)
1. **Indie hacker / solo dev** on single-node k3s/VPS. Wants `git push → live URL` in 5 min. Pays
   $0–10/mo. **Primary launch persona; the adoption engine — do NOT monetize hard.**
2. **SMB SaaS platform team** (~50 people), one OIDC provider, Dev/Staging/Prod. **Secondary; the Crew
   buyer.**
3. **Enterprise SRE / platform eng** — compliance, multi-cluster, Vault. **NOT the launch audience**
   (Cooker is single-tenant today). Constellation tier, gated on multi-tenancy.
Explicitly NOT for: multi-tenant/hard-isolation needs, YAML-CI loyalists, Windows shops, SAML-only
shops, hosted-SaaS shoppers (no hosted offering exists yet).

## Competitive set
- **Self-hosted PaaS:** Coolify, Dokploy, CapRover (our closest traction comparables; no visual CI).
- **OSS CI:** Drone (Harness-owned, commercial tier), Woodpecker (YAML), Concourse, Jenkins X, Dagger (SDK), Tekton (plumbing), Argo Workflows/CD.
- **SaaS/incumbent:** GitHub Actions + GitLab CI (free-by-default), Buildkite, CircleCI, Harness (enterprise).

## Monetization reality (the spine — do not hand-wave around these)
- **Adoption-first ladder** (product-plan §7): adoption → sponsorship ($0–200/mo, signal only) →
  consulting/support (first real cash) → open-core (only after >1k stars + >10 prod deployments) →
  distribution. **Anti-goals:** no paywalls before traction; no speculative enterprise features; no
  solo-operated paid SaaS before a fix-first list + external pen-test.
- **Pricing mockup (treated as committed):** Explorer **$0** / Crew **$49 per replica/mo** /
  Constellation **custom**. **Per-replica billing, unlimited seats.** Cloud variant proposed at
  $39/mo base + build-minute usage.
- **Revenue lanes (launch docs):**
  - **B0 — self-hosted licensing** (offline Ed25519 keys, ~4.5 d, degrade-to-Free on expiry).
    **NO blockers — ships first. This is the fastest path to revenue.**
  - **B1/B2 — hosted "Cooker Cloud" billing + usage metering** (Stripe Checkout/Portal/Meters).
    **HARD-GATED on `tenant_id` multi-tenancy (6–8 weeks, unbuilt) + a build-farm sandbox + pen-test.**
    Cloud go/no-go is an **unmade** product decision (ADR-0004 deferred it).
- PCI stays **SAQ-A** (Stripe-hosted; no card data in Cooker). `tenant_id` is also a **GDPR**
  prerequisite for per-customer erasure.

## Hard constraints / brand rules (strategy.md §7 — non-negotiable)
No astroturfing / upvote rings / "smash that star button". No inflated numbers. No overselling
auth/multi-tenancy (there is an open single-tenant/IDOR posture — never claim "enterprise-ready" or
"team-isolated"). Keep the **core OSS**. No paid ads before ~day 180. Voice = senior engineer to
peers; no press-release tone, no "revolutionary/game-changing/AI-powered".

## Success metrics (strategy.md §6 targets)
Stars: 200 (d7) → 1000 (d90). GHCR pulls 200→5000. External contributors w/ merged PR 0→5 (the single
most important number). "Time to first run" median → 6 min. Honest failure definition exists; read §6.

## Instructions to every specialist (Round 1)
1. Read this brief + your agent file's "Required reading".
2. Use WebSearch/WebFetch for **current, cited** data where it adds value (date every source — prices
   and market figures go stale). If web tools are unavailable, reason from the docs + label clearly.
3. Label every assumption `ASSUMPTION:` and name the teammate who owns the real input.
4. Write ONE focused doc to your assigned output path (~500–900 words; tables welcome).
5. **End your file with a `## Cross-team flags` section** — bullets naming conflicts/overlaps/
   dependencies with specific teammates (by role) for the lead/CMO to reconcile. This is how we
   "discuss" in one pass.
6. Honesty over hype. Never contradict the brand rules above. Do not edit product code.
