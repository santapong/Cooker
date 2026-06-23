---
title: "Cooker vs Woodpecker CI: visual pipeline editor vs YAML-first CI runner"
description: "Woodpecker CI is a clean OSS Drone fork with YAML pipelines and no enterprise tier. Cooker adds a visual DAG editor, multi-cloud deploy targets, and multi-environment promotion."
---

# Cooker vs Woodpecker CI: visual pipeline editor vs YAML-first CI runner

**Target keyword:** Woodpecker CI alternative

---

Woodpecker CI is a community-maintained, Apache 2.0-licensed fork of Drone CI that preserves the open-source CI runner model after Harness's BSL relicensing — it uses YAML pipelines, an agent-based runner architecture, and a minimal web UI for viewing run status. Cooker is a self-hosted CI/CD tool with a drag-drop visual pipeline editor, native Kubernetes and multi-cloud deploy targets, and built-in multi-environment promotion — features absent from Woodpecker.

---

## Feature comparison

| Feature | Cooker | Woodpecker CI |
|---|---|---|
| Licence | Apache-2.0 | Apache 2.0 |
| Visual DAG pipeline editor | Yes — drag-drop React Flow canvas | No — web UI shows run status only |
| Pipeline definition | Visual DAG (primary); CKR-DSL on roadmap | `.woodpecker.yml` or `.drone.yml` YAML |
| Agent-based runner architecture | No — single binary includes executor | Yes — separate server + one or more agents |
| OCI image builds (no docker.sock) | Yes — Kaniko, BuildKit, Buildah | Via Docker-in-Docker plugin (docker.sock required) |
| Deploy to Kubernetes | Yes — native client-go adapter, built-in Deploy stage | Via community plugins or shell |
| Deploy to ECS / Cloud Run / Fly / Render | Yes — native adapters | No |
| Multi-environment promotion + approval gates | Yes — Dev/Staging/Prod, RBAC-gated approvals | No |
| OIDC + RBAC | Yes — OIDC/PKCE, four roles (admin/operator/approver/viewer) | Partial — GitHub/GitLab/Bitbucket OAuth; two roles (admin/user) |
| Real-time streaming logs | Yes — WebSocket per stage | Yes |
| Pluggable secrets | Yes — 5 backends | Basic — secrets stored in Woodpecker's own DB |
| Cron-triggered runs | Yes (Phase 2, default-off) | Yes |
| Multi-provider webhooks (GitHub + GitLab + Bitbucket + Gitea) | Yes | Yes |
| Drone plugin compatibility | Partial (roadmap) | Yes — core design goal |
| Single binary | Yes | No — server + agent(s) are separate processes |
| Single-tenant today | Yes | Yes |

---

## Where Woodpecker wins

**Drone plugin compatibility.** Woodpecker is a Drone fork and intentionally maintains compatibility with the Drone plugin ecosystem. If you have custom Drone plugins or your team has invested in Drone-compatible tooling, migrating to Woodpecker is lower-friction than migrating to any other tool, including Cooker.

**YAML pipeline maturity.** Woodpecker's YAML pipeline format is well-documented, widely understood, and benefits from years of Drone community knowledge. For teams who have strong preferences for pipeline-as-code and already know Drone YAML, Woodpecker's syntax is familiar. Cooker's primary interface is the visual editor; YAML import is on the roadmap but not yet available.

**Agent-based horizontal scaling.** Woodpecker's separate agent model lets you add runner capacity independently of the server. You can run agents on different hardware types (arm64 builds on an Apple Silicon agent, GPU workloads on a GPU host). Cooker's single-binary model is simpler to operate but is less flexible for heterogeneous runner fleets.

**Established OSS community.** Woodpecker has an active contributor community, a Discord, and a known track record post-fork. If community continuity and the ability to read issues/PRs about your problems matter to your evaluation, Woodpecker has more of that history than Cooker does today.

---

## Where Cooker wins

**Visual pipeline authoring.** Cooker's drag-drop DAG editor is the feature with no equivalent in Woodpecker. You build a pipeline by dragging stage nodes (Build, Test, Push, Deploy, Approval, Custom) onto a canvas and connecting them with edges. The resulting graph is what runs — there is no YAML that the graph "compiles to" that you then have to keep in sync. For teams who find YAML CI maintenance painful, or who want to onboard operators without CI YAML expertise, the visual model is a material difference.

**First-class deploy story.** Woodpecker is a CI runner — it executes steps. Deploying to Kubernetes, ECS, or Cloud Run requires shell scripts or community plugins. Cooker treats Deploy as a first-class stage type with native adapters (Kubernetes via client-go, ECS via aws-sdk-go-v2, Cloud Run via GCP's Go SDK, Fly.io and Render via their REST APIs) and built-in rollback per target.

**Multi-environment promotion with approval gates.** Dev, Staging, and Production are modelled as first-class environments in Cooker. You draw a promotion edge in the pipeline DAG; if you attach an Approval stage to that edge, the pipeline pauses until an `approver`-role user signs off in the UI. Woodpecker has no equivalent concept.

**Stronger OIDC + RBAC.** Cooker ships with four named roles (admin, operator, approver, viewer), configurable OIDC group-to-role mapping, and step-up MFA on destructive admin routes. Woodpecker's auth is OAuth-based with admin/user distinctions but no role model for deployment approvals.

**No docker.sock in production.** Cooker defaults to Kaniko for builds (no host socket required). In production mode, the Docker builder is refused at startup. Woodpecker relies on Docker-in-Docker or host socket mounting for image builds.

---

## Honest caveats

If your question is "I want an open-source, YAML-based Drone replacement that my existing Drone plugins will work with," Woodpecker is a more direct answer than Cooker today. Cooker's Drone YAML import is on the roadmap but not shipped.

Both tools are single-tenant. Woodpecker's two-role model (admin/user) is coarser than Cooker's four roles, but both tools give all authenticated users broad visibility into all pipelines.

---

## Try Cooker

```bash
git clone https://github.com/santapong/Cooker.git
cd Cooker
docker compose up
```

Open `http://localhost:5173`. No OIDC required in dev mode — a dev admin user is injected automatically.

Full quickstart: [docs.cooker.dev/getting-started](https://docs.cooker.dev/getting-started/)
