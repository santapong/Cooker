# Cooker Documentation

Start here. The docs are organized by purpose:

| Folder | What's inside |
|---|---|
| **[system-design/](system-design/README.md)** | The consolidated, top-to-bottom system design — 17 chapters (overview → C4 model), with diagrams. **Read this to understand how Cooker works.** |
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
- **[`openapi.yaml`](openapi.yaml)** — the full OpenAPI 3.1 spec (load into Swagger UI / Redoc).

## guides/

Operator-facing how-tos. Read the one that matches what you're doing.

| Doc | Read when |
|---|---|
| [guides/UAT.md](guides/UAT.md) | Touching anything that affects `make uat-up` |
| [guides/MULTI_REPLICA.md](guides/MULTI_REPLICA.md) | Running Cooker HA (Redis-backed, multi-replica) |
| [guides/ROLLOUT.md](guides/ROLLOUT.md) | Doing a UAT → production cutover |
| [guides/RUNBOOK.md](guides/RUNBOOK.md) | On call — incident response & alert rules |
| [guides/RELEASING.md](guides/RELEASING.md) | Cutting a release |
| [guides/SECURITY-RELEASE-VERIFY.md](guides/SECURITY-RELEASE-VERIFY.md) | Verifying a signed release artifact |

## reference/

Canonical, authoritative detail. The system-design chapters summarize and link here.

| Doc | Authoritative for |
|---|---|
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
