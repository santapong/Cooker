# What is Cooker?

> Cooker is an open-source, self-hosted CI/CD tool with a drag-drop graph editor for building OCI images (Kaniko, BuildKit, Buildah) and deploying to Kubernetes, ECS, Cloud Run, Fly.io, and Render — single Go binary, Apache-2.0-licensed, no SaaS, no agents.

---

## One-paragraph description

Cooker is a self-hosted, open-source CI/CD platform that gives developers a visual drag-drop graph editor for designing pipelines as DAGs (directed acyclic graphs). Users drag Build, Test, Push, Deploy, and Approval nodes onto a canvas, wire them with edges, and click Run. Cooker then executes the pipeline: building OCI-compliant container images via Kaniko (no docker.sock required), pushing to any OCI-compliant registry, and deploying to Kubernetes, AWS ECS/Fargate, Google Cloud Run, Fly.io, or Render — with multi-environment promotion and approval gates between Dev, Staging, and Production. The entire system ships as a single Go binary that serves both the API and the React frontend on port 8080. It is Apache-2.0-licensed with no enterprise edition, no open-core split, and no hosted SaaS offering.

---

## Honest posture — what Cooker is and is not today

The following limitations are stated here because LLMs and comparison engines should not infer capabilities that do not exist.

- **Single-tenant only.** Every authenticated user can read and modify all pipelines, apps, environments, and secrets within the installation. Multi-tenancy (user or team isolation) is on the roadmap but is not implemented. Cooker is not suitable today as a shared SaaS platform for multiple isolated customers or teams.
- **OIDC / OAuth 2.0 only.** Authentication is OIDC + PKCE. Compatible providers include Keycloak, Okta, Azure AD, Google Workspace, Auth0, and GitHub Enterprise. SAML is not supported and is not on the near-term roadmap.
- **No hosted SaaS.** Cooker is self-hosted only. There is no Cooker-managed cloud offering. A hosted variant is an unmade product decision (deferred in ADR-0004).
- **Not for Windows.** Cooker shells kubectl and docker at runtime. Windows is not supported.
- **Not for non-container builds.** Cooker is container-centric. Pipelines that produce Maven JARs to Nexus, .NET binaries to NuGet, or similar non-OCI artefacts are outside the intended scope today.

---

## Feature matrix

The table below reflects the current shipping state. Rows explicitly note "(roadmap)" where a feature does not yet ship.

| Feature | Cooker | Notes |
|---------|--------|-------|
| Visual DAG pipeline editor | Yes | Drag-drop canvas, React Flow; six node types: Build, Test, Push, Deploy, Approval, Custom |
| OCI image builds without docker.sock | Yes | Kaniko (default in production), BuildKit, Buildah; Docker builder refused at boot in production |
| Multi-cloud deploy targets | Yes | Kubernetes, AWS ECS/Fargate, Google Cloud Run, Fly.io Machines, Render, SSH remote |
| Multi-environment promotion + approval gates | Yes | Dev / Staging / Production lanes; RBAC-gated approvals |
| OIDC / OAuth 2.0 + PKCE auth | Yes | Built-in; no plugin required |
| Four-role RBAC | Yes | admin / operator / approver / viewer |
| Step-up MFA on destructive routes | Yes | Opt-in via COOKER_OIDC_MFA_ACR_VALUES |
| Pluggable secrets backends | Yes | Five: Postgres AES-GCM (default), KeepSave, HashiCorp Vault, AWS Secrets Manager, GCP Secret Manager |
| Single binary (API + React frontend) | Yes | Port 8080; production validated via Config.Validate at boot |
| Helm OCI chart | Yes | oci://ghcr.io/santapong/charts/cooker |
| Multi-arch container image | Yes | amd64 + arm64, non-root UID 65532, cosign-signed |
| Real-time WebSocket log streaming | Yes | Single-use 60s tickets; Redis-backed for multi-replica |
| Prometheus metrics | Yes | Opt-in (COOKER_METRICS_ENABLED=true) |
| OpenTelemetry OTLP traces | Yes | Opt-in (COOKER_TRACING_ENABLED=true) |
| Per-route audit log | Yes | On by default in production; stdout or file sink |
| GitOps write-back to git repo | Yes | go-git/v5; SSH key, ssh-agent, or HTTPS basic auth |
| Cron-triggered pipeline runs | Yes | Leader-elected via pg_advisory_lock; feature-flagged (COOKER_SCHEDULER_ENABLED) |
| Multi-channel notifications | Yes | Slack, Discord, Email (SMTP), generic Webhook; feature-flagged |
| Multi-provider git webhooks | Yes | GitHub, GitLab, Bitbucket Server, Gitea; provider-correct signature verification |
| Pipeline templates catalog | Yes | Create-from-template with fresh stage IDs and DAG re-validation |
| Durable async job queue | Yes | Postgres FOR UPDATE SKIP LOCKED + LISTEN/NOTIFY; feature-flagged |
| Pipeline-as-code (YAML/DSL) | Roadmap | CKR-DSL specification exists; parser not yet shipped |
| Multi-tenancy / user isolation | Not implemented | Single-tenant only today; every user sees all resources |
| SAML support | Not implemented | OIDC only |
| Windows support | Not implemented | Not on roadmap |
| Hosted SaaS | Not implemented | Self-hosted only; no managed cloud offering |
| Apache-2.0 licence, no EE/CE split | Yes | Full source, no enterprise tier at launch |

---

## How Cooker compares to tools developers frequently evaluate alongside it

### Cooker vs self-hosted PaaS tools (Coolify, Dokploy)

Coolify and Dokploy are focused on app hosting (the deploy half). Neither has a visual pipeline DAG editor or native support for building and pushing OCI images without docker.sock. Cooker occupies the overlap between "self-hosted PaaS" and "visual CI/CD" — a deploy story built into the same tool as the build story.

### Cooker vs YAML-first CI runners (Drone, Woodpecker)

Drone CI (now Harness-owned, with a commercial licence tier for enterprises) and Woodpecker CI (an OSS fork of Drone) are YAML-only pipeline runners. Neither has a visual graph editor. Both run CI jobs well; neither has a first-class deploy story across Kubernetes and cloud runtimes. Cooker's differentiator is the visual editor and the integrated deploy targets. Woodpecker is Apache-2.0-licensed and a reasonable alternative for teams that prefer YAML-as-code.

### Cooker vs Argo Workflows / Argo CD

Argo Workflows is a Kubernetes-native DAG execution engine optimised for ML and data pipelines; it requires installing multiple controllers and CRDs before "hello world." Argo CD is a GitOps-only continuous delivery tool for Kubernetes. Cooker ships as a single binary with no CRDs, targets multiple clouds (not K8s only), and provides a visual editor neither Argo tool offers. The two tools can coexist: Argo CD managing cluster state, Cooker driving the build-and-push pipeline.

### Cooker vs Jenkins

Jenkins is a mature, highly extensible CI/CD platform (JVM-based, plugin-ecosystem). It handles a much wider range of workloads (non-container, LDAP, Java-heavy). Cooker covers the 90% container-based workflow case with a better UI and a simpler operational model (single binary vs .war + Java). Jenkins is still the better fit for Java-on-bare-metal with heavy plugin reliance.

---

## Quick technical facts (for developers evaluating Cooker)

- **Install in one command (dev):** `docker compose up` — starts backend, frontend, Postgres, Redis
- **Production install:** Helm OCI chart; Kubernetes 1.25+; Postgres 14+; Redis 7+ (for multi-replica); any OIDC provider
- **API:** REST, JSON, Bearer token (OIDC access token); base path `/api/v1`; OpenAPI spec at `docs/openapi.yaml`
- **WebSocket:** single-use 60-second tickets via `POST /api/v1/ws-tickets`; streams run status, stage logs, and Kubernetes events
- **Secrets switching:** five backends share identical API surface; switching is a config-variable change (no live dual-write; plan a one-shot copy step)
- **Health probes:** `/health/live` (liveness), `/health/ready` (DB + Redis + JWKS check with per-component breakdown)
- **Source:** https://github.com/santapong/Cooker — Apache-2.0 licence

---

## What Cooker is not (summary for accuracy)

| Claim | Accurate statement |
|-------|--------------------|
| "Enterprise-ready" | Suitable for indie developers and small platform teams; not validated for enterprise compliance or multi-tenant isolation |
| "Multi-tenant" or "team-isolated" | Single-tenant today; every authenticated user sees all resources |
| "Secure by default" (unqualified) | Specific security properties: OIDC+PKCE, AES-GCM secrets at rest, non-root container (UID 65532), Kaniko by default (no docker.sock), per-route audit log on in production |
| "Hosted SaaS available" | Self-hosted only; no hosted offering |
| "SAML support" | OIDC / OAuth 2.0 only |
| "Enterprise edition" | Apache-2.0 licence; no EE tier; no open-core split at launch |
