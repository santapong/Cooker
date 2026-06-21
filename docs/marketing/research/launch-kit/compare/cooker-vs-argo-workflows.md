<!-- DRAFT — for docs site /compare/ -->

---
title: "Cooker vs Argo Workflows: single-binary CI/CD vs Kubernetes-native workflow engine"
description: "Argo Workflows is the Kubernetes-native DAG engine for data and ML pipelines. Cooker is a single-binary CI/CD tool with a visual editor and multi-cloud deploy targets."
---

# Cooker vs Argo Workflows: single-binary CI/CD vs Kubernetes-native workflow engine

**Target keyword:** Argo Workflows alternative

---

Argo Workflows is a Kubernetes-native workflow engine from the Argo Project (CNCF) that expresses DAG-based workloads as CRDs — it is widely used for data pipelines, ML training jobs, and CI use cases that require deep Kubernetes integration. Cooker is a self-hosted CI/CD tool with a drag-drop visual pipeline editor, a single Go binary that runs without CRD installation, and native deploy targets spanning Kubernetes, ECS, Cloud Run, Fly.io, and Render.

---

## Feature comparison

| Feature | Cooker | Argo Workflows |
|---|---|---|
| Installation complexity | Single binary or `docker compose up`; Helm chart for production | Multiple controllers (workflow-controller, argo-server), CRD installation, RBAC cluster-wide |
| Visual DAG pipeline editor | Yes — drag-drop React Flow canvas | Partial — Argo UI shows DAG status; authoring is YAML CRDs only |
| Pipeline / workflow definition | Visual DAG (primary); CKR-DSL on roadmap | YAML Workflow CRDs |
| Kubernetes required to run | No — runs on any Docker host | Yes — CRDs require a Kubernetes cluster to run Argo itself |
| OCI image builds | Yes — Kaniko, BuildKit, Buildah | Not native — CI use case layered on top of workflow primitives |
| Deploy to Kubernetes | Yes — native client-go adapter | Yes (Argo CD is the deploy counterpart, separate install) |
| Deploy to ECS / Cloud Run / Fly / Render | Yes — native adapters | No — K8s only; cloud targets require custom steps |
| Multi-environment promotion + approval gates | Yes — Dev/Staging/Prod, RBAC-gated approvals | Partial — sync windows in Argo CD; no visual promotion DAG in Argo Workflows |
| OIDC + RBAC | Yes — OIDC/PKCE, four roles | Yes — OIDC supported; cluster RBAC manages access |
| Real-time streaming logs | Yes — WebSocket per stage | Yes — artifact log streaming |
| Secrets management | Yes — 5 pluggable backends | Via Kubernetes Secrets or external plugin (Vault, etc.) |
| Pluggable step executors | Yes — builder/deployer adapters | Yes — emissary, PNS, k8sapi executors |
| Scalability for large ML/data pipelines | Not the target use case | Yes — purpose-built for high-volume, long-running workflow orchestration |
| Single-tenant today | Yes | Namespace-isolated on Kubernetes RBAC |
| Licence | MIT | Apache 2.0 |
| CNCF backing | No | Yes — Argo is a CNCF graduated project |

---

## Where Argo Workflows wins

**Kubernetes-native depth.** Argo Workflows is designed from the ground up as a Kubernetes-first system. Every workflow is a CRD. You get Kubernetes-native RBAC, namespace isolation, resource quotas, pod-level scheduling control, node selectors, tolerations, and affinity rules on every step. If your workloads are deeply Kubernetes-specific — GPU allocation, spot-instance node pools, large PVC mounts — Argo Workflows exposes all of this directly. Cooker abstracts over these details.

**ML and data pipeline scale.** Argo Workflows is the de-facto standard for ML training pipelines, ETL jobs, and data-science workflows that run on Kubernetes. It supports recursive DAGs, parameter sweeps, artifact passing between steps, and long-running steps (hours to days). Cooker's execution model targets CI/CD pipeline durations (minutes), not ML training runs (hours).

**CNCF backing and ecosystem.** Argo Workflows is a CNCF graduated project with a large contributor base, enterprise adoption, and integration with the broader Argo family (Argo CD, Argo Events, Argo Rollouts). If you are building on a Kubernetes platform where Argo CD is already installed for GitOps delivery, adding Argo Workflows for CI is a natural extension that shares tooling, RBAC, and operational patterns.

**CRD-as-code workflow definition.** Argo Workflows are Kubernetes resources — they live in a git repo, can be applied with `kubectl apply`, and participate in your GitOps workflow. If your organisation treats everything as a Kubernetes manifest, this is the right shape.

---

## Where Cooker wins

**No Kubernetes required to run Cooker itself.** Argo Workflows requires a Kubernetes cluster for the Argo control plane — the workflow-controller, argo-server, and CRD installation. Cooker runs as a single binary on any Linux host with Docker, and optionally on Kubernetes via Helm. If your team does not already operate Kubernetes, Cooker's operational overhead is dramatically lower.

**Visual pipeline authoring.** Argo Workflows has no visual editor for authoring — you write YAML CRDs. The Argo UI shows the DAG status of a running workflow but is not an authoring surface. Cooker's drag-drop editor lets you build, wire, and modify a pipeline without writing YAML. For CI/CD workflows (build → test → push → deploy), the visual model is faster to author and easier to hand off to non-infrastructure engineers.

**Integrated build and deploy in one tool.** Argo Workflows handles workflow orchestration; you need Argo CD (separate installation, separate CRDs, separate learning curve) for application delivery. Cooker integrates build, push, and deploy in the same pipeline DAG. An Approval stage in the same graph can gate a production deploy without switching tools.

**Multi-cloud deploy targets.** Argo Workflows is Kubernetes-only. Cooker has native adapters for Kubernetes, AWS ECS/Fargate, Google Cloud Run, Fly.io, and Render. If your production workload does not run on Kubernetes — or runs on a mix — Cooker covers it; Argo Workflows does not.

**Simpler secrets model.** Cooker's five-backend secrets system (Postgres AES-GCM, KeepSave, Vault, AWS SM, GCP SM) gives you pluggable secret storage with a consistent API across all environments. Argo Workflows delegates to Kubernetes Secrets or external plugin integrations, which requires separate configuration and cluster permissions for each.

---

## Coexistence note

Argo Workflows and Cooker are not necessarily competing for the same slot. If you already use Argo CD for GitOps delivery, Cooker can sit alongside it — Cooker handles the CI half (build, test, push), and Argo CD handles the GitOps delivery half. Cooker has a native GitOps mode that writes rendered manifests back to a git repo for Argo CD to pick up. The two tools do not need to be mutually exclusive.

---

## Honest caveats

Cooker is not a replacement for Argo Workflows for ML or data-science workloads. If your use case involves parameter sweeps, artifact passing between long-running steps, or workflows that run for hours — Argo Workflows is purpose-built for that and Cooker is not. Cooker targets CI/CD pipelines that run in minutes, not workflow orchestration that runs for hours.

Cooker is single-tenant today. Argo Workflows provides namespace-level isolation via Kubernetes RBAC, which can serve as a form of team isolation in a shared cluster — Cooker does not have an equivalent isolation model yet.

---

## Try Cooker

```bash
git clone https://github.com/santapong/Cooker.git
cd Cooker
docker compose up
```

Open `http://localhost:5173`. No OIDC required in dev mode — a dev admin user is injected automatically.

Full quickstart: [docs.cooker.dev/getting-started](https://docs.cooker.dev/getting-started/)
