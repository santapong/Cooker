# Cooker Documentation

The September 2026 GitHub Compose and frontend changes are implemented on
`develop` and ready for UAT. Human acceptance and live GitHub/cloud checks are
pending; older design chapters and proposals may describe a different baseline.
Start with the current guides below, then use the deeper references.

![Repository → Compose files → Preview → Destination → Review](images/compose-workflow.svg)

| You want to… | Start here |
|---|---|
| Try the current UI locally | [Quickstart](user-guide/getting-started/quickstart.md) |
| Import and review a GitHub Compose stack | [GitHub Compose deployment](guides/GITHUB-COMPOSE-DEPLOYMENT.md) |
| Understand file libraries, shared stacks and image registries | [Compose library and registries](user-guide/guides/compose-library.md) |
| Understand prefixes, SHA pins and external database execution | [GitHub Compose architecture](reference/github-compose-architecture.md) |
| Accept the current candidate | [UAT runbook](guides/UAT.md) and [implementation evidence](plans/2026-09-06-github-compose-deployment.md) |
| See what changed | [Changelog](../CHANGELOG.md) |

The rest of the docs are organized by purpose:

| Folder | What's inside |
|---|---|
| **[system-design/](system-design/README.md)** | The consolidated, top-to-bottom system design — 17 chapters (overview → C4 model), with diagrams. Read alongside the current GitHub Compose execution reference. |
| **[engineering/](engineering/README.md)** | Contributor process notes and historical development workflows. |
| **[guides/](#guides)** | Operator & how-to guides — UAT, multi-replica, rollout, runbook, releasing. |
| **[reference/](#reference)** | Canonical references — architecture, design conventions, protocols, the Go style guide. |
| **[adr/](adr/README.md)** | Architecture Decision Records (strategy pattern, secrets, JSONB, multi-tenancy). |
| **[proposals/](#proposals)** | Plans, roadmaps, and research — not current-state. |
| **[audits/](audits/)** | Bug / SPOF / security / performance audit findings. |
| **[user-guide/](user-guide/index.md)** | End-user getting-started material. |
| **[marketing/](marketing/strategy.md)** | Positioning & strategy. |

New to the project? Read **[`TUTORIAL.md`](TUTORIAL.md)** — a skippable, task-oriented walkthrough of every feature.

---

## Top-level entry points

- **[`TUTORIAL.md`](TUTORIAL.md)** — feature-by-feature walkthrough (skip what you don't need).
- **[`openapi.yaml`](openapi.yaml)** — the OpenAPI 3.1 reference (load into Swagger UI / Redoc). For the new App inspection flow, also use the source-linked [execution reference](reference/github-compose-architecture.md).

## guides/

Operator-facing how-tos. Read the one that matches what you're doing.

| Doc | Read when |
|---|---|
| [guides/GITHUB-COMPOSE-DEPLOYMENT.md](guides/GITHUB-COMPOSE-DEPLOYMENT.md) | Connecting GitHub, reviewing Compose and configuring existing compute/database targets |
| [guides/UAT.md](guides/UAT.md) | Touching anything that affects `make uat-up` |
| [guides/DEPLOY-AWS-VERCEL.md](guides/DEPLOY-AWS-VERCEL.md) | Hosting Cooker on AWS (EKS Auto Mode) + UAT SPA on Vercel — IaC, tiered cost, runbook |
| [guides/MULTI_REPLICA.md](guides/MULTI_REPLICA.md) | Running Cooker HA (Redis-backed, multi-replica) |
| [guides/ROLLOUT.md](guides/ROLLOUT.md) | Doing a UAT → production cutover |
| [guides/RUNBOOK.md](guides/RUNBOOK.md) | On call — incident response & alert rules |
| [guides/RELEASING.md](guides/RELEASING.md) | Cutting a release |
| [guides/SECURITY-RELEASE-VERIFY.md](guides/SECURITY-RELEASE-VERIFY.md) | Verifying a signed release artifact |

## reference/

Canonical, authoritative detail. The system-design chapters summarize and link here.

| Doc | Authoritative for |
|---|---|
| [reference/github-compose-architecture.md](reference/github-compose-architecture.md) | Current reviewed App import/execution path and its boundaries |
| [reference/architecture.md](reference/architecture.md) | The canonical system map (what calls what) |
| [reference/design.md](reference/design.md) | Feature patterns, conventions, the §11 "adding a feature" checklist |
| [reference/protocols.md](reference/protocols.md) | Wire protocols (CKR-LOG, CKR-DSL proposals) |
| [reference/shipping-go.md](reference/shipping-go.md) | Go style & shipping conventions |
| [reference/architecture-phase1-phase2.md](reference/architecture-phase1-phase2.md) | The feature-flagged platform subsystems (queue, scheduler, notifier, …) |

## proposals/

Plans and research — describe *intended* or *possible* future work, **not** current behaviour. When a proposal ships, its content moves into `reference/` or a system-design chapter.

| Doc | What it proposes |
|---|---|
| [proposals/dag-adaptation-2026.md](proposals/dag-adaptation-2026.md) | The 5 DAG primitives + 20-week roadmap |
| [proposals/execution-observability-redesign-2026.md](proposals/execution-observability-redesign-2026.md) | Log replay + execution tracing |
| [proposals/roadmap-2026.md](proposals/roadmap-2026.md) | 2026 roadmap |
| [proposals/pm-brief-2026-05.md](proposals/pm-brief-2026-05.md) | May 2026 PM brief |
| [proposals/adapted-from-dokploy.md](proposals/adapted-from-dokploy.md) | Dokploy adaptation attribution |
| [proposals/game-changer-ideas.md](proposals/game-changer-ideas.md) | Bigger-bet ideas |
| [proposals/grovernance_integration.md](proposals/grovernance_integration.md) | Governance integration notes |
| [proposals/claude-bug-routine-plan.md](proposals/claude-bug-routine-plan.md) | Weekly bug-hunt routine |

---

> **Doc conventions:** current-state lives in `system-design/` + `reference/`; future-state in
> `proposals/`. If you ship a proposal, update the relevant chapter and move/trim the proposal in the
> same PR so the docs never claim something that isn't real.
