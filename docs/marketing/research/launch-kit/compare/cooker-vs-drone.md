<!-- DRAFT — for docs site /compare/ -->

---
title: "Cooker vs Drone CI: open-source CI/CD without the commercial licence tier"
description: "Drone CI is mature and container-native but now under Harness's BSL licence. Cooker is MIT-licensed with a visual DAG editor and a built-in deploy story."
---

# Cooker vs Drone CI: open-source CI/CD without the commercial licence tier

**Target keyword:** Drone CI alternative

---

Drone CI is a container-native, YAML-based CI runner originally created by Harshil Agrawal and now owned by Harness, Inc., which relicensed the enterprise edition under the Business Source License (BSL) — meaning the source is available but commercial use above a threshold requires a paid licence. Cooker is a fully MIT-licensed, self-hosted CI/CD tool with a drag-drop visual pipeline editor, native multi-target deployment, and no enterprise edition split.

---

## Feature comparison

| Feature | Cooker | Drone CI |
|---|---|---|
| Licence | MIT — no EE tier, no usage gating | Community edition: Apache 2.0; Enterprise edition: BSL (Harness commercial terms) |
| Visual DAG pipeline editor | Yes — drag-drop React Flow canvas | No — YAML pipelines only |
| Pipeline definition | Visual DAG (primary); CKR-DSL on roadmap | `.drone.yml` YAML |
| OCI image builds | Yes — Kaniko, BuildKit, Buildah | Via Docker-in-Docker plugin (requires docker.sock) |
| Deploy to Kubernetes | Yes — native client-go adapter | Via community plugin (no first-class support) |
| Deploy to ECS / Cloud Run / Fly / Render | Yes — native adapters | No — requires shell scripts or third-party plugins |
| Multi-environment promotion + approval gates | Yes — Dev/Staging/Prod, RBAC-gated approvals | Partial — `promote` event exists; no visual promotion DAG |
| OIDC + RBAC | Yes — OIDC/PKCE, four roles | Partial — GitHub/GitLab/Bitbucket OAuth; no RBAC roles |
| Real-time streaming logs | Yes — WebSocket per stage | Yes |
| Pluggable secrets | Yes — 5 backends (Vault, AWS SM, GCP SM, KeepSave, Postgres) | Yes — Drone Secrets + Vault plugin |
| Pipeline-as-code (YAML import) | Roadmap (Drone YAML import listed in backlog) | Yes — native |
| Single binary | Yes | Yes (runner is separate from server) |
| Single-tenant today | Yes | Yes |
| Active upstream development | Yes | Slower — Harness focus has shifted to their SaaS platform |

---

## Where Drone wins

**Maturity and ecosystem.** Drone has been in production since 2014. Its plugin ecosystem is extensive — hundreds of community plugins cover cloud providers, testing frameworks, notification channels, and deployment targets. If you are running a well-established Drone install and it works, the migration cost to any alternative is real.

**YAML-as-code.** If your team already version-controls all CI configuration in `.drone.yml`, you have a known mental model, code review workflow, and rollback story. Cooker's primary interface is the visual editor; importing existing Drone YAML is on the roadmap but is not available today.

**Container-native maturity.** Drone pioneered the "every step runs in a container" model. The runtime semantics are well-documented and battle-tested. Cooker follows the same model but with less production-hours behind it.

**Runner architecture.** Drone's separate runner model lets you run runners on Linux, Windows, macOS, and arm64 hosts. Cooker targets Linux/Kubernetes-first; Windows is explicitly out of scope.

---

## Where Cooker wins

**MIT licence, no ambiguity.** Drone's enterprise edition is BSL-licensed. The Community Edition is Apache 2.0 but has capped features. If your organisation has a legal policy against BSL dependencies, or if you simply want a CI tool with no licence-tier anxiety, Cooker is MIT and there is no enterprise edition to worry about.

**Visual pipeline editor.** Cooker's drag-drop DAG editor is the primary authoring surface. Drone has no visual editor — pipelines are YAML only. For teams who find CI YAML maintenance a burden, or who want to onboard non-engineers to pipeline authoring, the visual model is a meaningful difference.

**First-class deploy story.** Drone is a CI runner — it runs steps. Deploying to Kubernetes, ECS, or Cloud Run requires community plugins or shell scripts. Cooker treats Deploy as a first-class stage type with native adapters and built-in rollback per target. Approval gates between environments are RBAC-enforced and wired into the same DAG as the build stages.

**Integrated multi-environment model.** Dev, Staging, and Production are first-class Cooker concepts. Promotion edges in the DAG carry the pipeline from one environment to the next; approval gates block promotion until an `approver`-role user signs off.

**No docker.sock in production.** Cooker's default builder is Kaniko (in-cluster Kubernetes Job, no host socket). In production mode, Cooker refuses to boot with the Docker-socket builder. Drone's Docker plugin requires mounting the host socket, which is a known container-escape risk.

---

## Honest caveats

Cooker is earlier-stage software. If your pipeline runs reliably on Drone today and the licence is acceptable to your organisation, the switching cost is not zero. The Drone YAML import that would lower that cost is on Cooker's roadmap but not yet shipped.

Both tools are single-tenant — Drone does not natively isolate pipeline visibility between different teams sharing one server, and neither does Cooker.

---

## Try Cooker

```bash
git clone https://github.com/santapong/Cooker.git
cd Cooker
docker compose up
```

Open `http://localhost:5173`. No OIDC required in dev mode — a dev admin user is injected automatically.

Full quickstart: [docs.cooker.dev/getting-started](https://docs.cooker.dev/getting-started/)
