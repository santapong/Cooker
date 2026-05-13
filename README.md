# Cooker

A web-based CI/CD management tool with a **graph-based UI** for visually building and executing pipelines that build **OCI-compliant** Docker images, push to registries, and deploy to **Kubernetes** (and Cloud Run / ECS / Fly / Render) across Dev, Staging, and Production environments.

> **Status (May 2026)**: production-quality on single-replica or multi-replica (Redis-backed) shapes. `Config.Validate` refuses unsafe boots (docker.sock builder in production, multi-replica + memory backends without sticky sessions, missing TLS for OIDC, non-localhost Postgres without `sslmode>=require`). See [`backlog.md`](backlog.md#production-readiness-summary) for the deployment-shape readiness matrix and [`docs/ROLLOUT.md`](docs/ROLLOUT.md) for the UAT→production cutover playbook.

## Architecture

```
┌─────────────────────────────────────────────────────┐
│  Browser: React + TypeScript + React Flow (graph UI) │
│  Zustand state | WebSocket live updates              │
└──────────────────────┬──────────────────────────────┘
                       │ HTTPS / WSS
┌──────────────────────▼──────────────────────────────┐
│          Go API Server (Gin framework)               │
│  ┌──────────┬───────────┬──────────┬─────────────┐  │
│  │ Pipeline │  Docker   │   K8s    │  Registry   │  │
│  │ Engine   │  Service  │  Service │  Service    │  │
│  └────┬─────┴─────┬─────┴────┬─────┴──────┬──────┘  │
│       │           │          │            │          │
│  DAG Runner  Docker SDK  client-go  go-container-   │
│                                     registry        │
│  ┌──────────────────────────────────────────────┐   │
│  │  PostgreSQL (pipelines, runs) + Redis (cache) │   │
│  └──────────────────────────────────────────────┘   │
└───────┬──────────┬──────────────┬───────────────────┘
   Docker Engine   Kubernetes API   OCI Registries
```

- **Frontend**: React + TypeScript + React Flow (visual DAG editor) + Zustand
- **Backend**: Go + Gin + Docker SDK + client-go + go-containerregistry
- **Database**: PostgreSQL + Redis
- **Auth**: OIDC/OAuth 2.0 with PKCE (Keycloak, Okta, Azure AD, Google, GitHub) — disabled by default in local/UAT (the backend injects a dev admin user); see [docs/UAT.md](docs/UAT.md#enabling-oidc-sign-in-for-uat) to enable. RBAC roles: **admin / operator / approver / viewer**. Group-to-role mapping is operator-configurable via `COOKER_OIDC_GROUP_MAP`; step-up MFA on destructive admin routes (DELETE pipelines/envs/apps/hosts, secret reveal/put/delete/promote, app webhook rotation) is opt-in via `COOKER_OIDC_MFA_ACR_VALUES`. OIDC verify errors are generic (`authentication failed` / `provider unavailable`); detail logged server-side at `slog.Warn`/`slog.Error`.
- **Secrets**: pluggable backends — AES-GCM at rest in Postgres (default), delegated to a [KeepSave](https://github.com/santapong/keepsave) server, HashiCorp Vault, AWS Secrets Manager, or GCP Secret Manager. See [Secrets backends](#secrets-backends).
- **Observability**: optional Prometheus `/metrics` and OpenTelemetry/OTLP traces (opt in via `COOKER_METRICS_ENABLED` / `COOKER_TRACING_ENABLED`). Structured `log/slog` JSON logs throughout. **`/health/live` + `/health/ready`** split with per-check breakdown (DB ping, Redis ping, JWKS age). Per-stage live build logs stream over WebSocket. Resilience counters (`cooker_db_connection_errors_total`, `cooker_redis_connection_errors_total`, `cooker_jwks_fetch_failures_total`, `cooker_pipeline_runs_orphaned_total`) ship recommended Alertmanager rules in [docs/RUNBOOK.md](docs/RUNBOOK.md).
- **Audit log**: per-route slog audit trail, on by default in production. Destination configurable (`stdout` or `file`) via `COOKER_AUDIT_DESTINATION` / `COOKER_AUDIT_FILE_PATH`.
- **Multi-replica state**: rate limiter, WebSocket ticket store, and WebSocket broadcast hub back onto Redis when `COOKER_RATE_LIMIT_BACKEND=redis` / `COOKER_WS_TICKET_BACKEND=redis` / `COOKER_WS_HUB_BACKEND=redis` (chart defaults to all three). See [docs/MULTI_REPLICA.md](docs/MULTI_REPLICA.md).
- **Builders**: `docker` (host socket — dev only; refused at boot in production), `kaniko` (in-cluster Job, default), `buildah` (in-cluster Job, full Dockerfile parity), or `buildkit` (gRPC against an external buildkitd). Selectable via `COOKER_BUILDER`. Raw-K8s manifests at `deploy/kubernetes/` no longer mount `/var/run/docker.sock` — closes S26-05-04.
- **Pushers**: `crane` (default; go-containerregistry) or `docker` (dev only; refused at boot in production alongside `COOKER_BUILDER=docker` — closes F-02).
- **Deploy targets**: Kubernetes, Cloud Run, AWS ECS / Fargate, Fly.io, Render. See [Cloud deploy targets](#cloud-deploy-targets).
- **Retention**: optional Helm-rendered CronJob prunes `pipeline_runs` older than `retention.daysToKeep` (default 90 days) at 02:00 UTC daily; gated on `retention.enabled && database.host`.
- **Orphan sweep**: stale-heartbeat runs are reaped on a configurable interval (`COOKER_ORPHAN_SWEEP_INTERVAL`, default 60s; must exceed `heartbeatInterval`). App-deploy runs create the `PipelineRun` row before heartbeat starts so OOM-killed pods leave an orphan row for the sweep to reap (closes F-07).
- **Version flag**: `cooker --version` prints `version`, `commit`, `date` (ldflags-populated); same metadata on `GET /api/v1/version`. Module path is `github.com/santapong/cooker`.
- **Raw-K8s liveness probes**: `deploy/kubernetes/deployment.yaml` now probes `/health/live` + `/health/ready` (matching the Helm chart) so the raw install path no longer crash-loops on cold boot (closes F-01).

See [docs/architecture.md](docs/architecture.md) for the full architecture document and [docs/adr/](docs/adr/) for the structural decisions and their rationale.

## OCI Compliance

All operations follow the three OCI (Open Container Initiative) specifications:

| Spec | Version | How Cooker Uses It |
|------|---------|-------------------|
| [**image-spec**](https://github.com/opencontainers/image-spec) | v1.1 | Build output = OCI Manifest + Image Index. Images tracked by `{mediaType, digest, size}` descriptors. Multi-arch via Image Index. |
| [**runtime-spec**](https://github.com/opencontainers/runtime-spec) | v1.2 | Container creation exposes runtime-spec concepts (mounts, env, cgroups, namespaces) in the UI. Docker Engine handles actual `config.json`. |
| [**distribution-spec**](https://github.com/opencontainers/distribution-spec) | v1.1 | Registry operations use standard endpoints: `/v2/<name>/tags/list`, `/v2/<name>/manifests/<ref>`, `/v2/<name>/referrers/<digest>`. Supports referrers API for supply chain metadata. |

**Key Go libraries**: `github.com/opencontainers/image-spec/specs-go/v1`, `github.com/google/go-containerregistry`, `github.com/docker/docker/client`, `k8s.io/client-go`

## Quick Start

```bash
# Local development (frontend + backend + Postgres + Redis)
docker compose up

# Frontend: http://localhost:5173
# Backend:  http://localhost:8080
# API:      http://localhost:8080/api/v1

# UAT stack (single binary serving the SPA on :8080, OIDC off)
make uat-up

# UAT + pre-seeded Keycloak realm (alice/admin, bob/viewer)
make uat-up-with-keycloak

# UAT + tecnativa/docker-socket-proxy (drops host socket bind mount)
make uat-up-socketproxy

# End-to-end smoke (boots UAT, runs one no-op pipeline, asserts success)
make test-e2e
```

New users: start at [docs/user-guide/index.md](docs/user-guide/index.md). Operators heading to production: [docs/ROLLOUT.md](docs/ROLLOUT.md). Frontend devs: initial JS entry chunk is 59 KB / 18 KB gzip; `@xyflow/react` only loads on canvas routes (post-PR-#38).

## Features

- **Visual Pipeline Builder** — drag-and-drop graph editor (React Flow); six node types (Build, Test, Deploy, Push, Approval, Custom); Simple ⇄ Pro mode toggle.
- **Apps** — higher-level shortcut: point at a GitHub repo, pick a deploy target, click Deploy. Cooker synthesises a Clone → Build → Push → Deploy run. AutoDeploy via per-app webhook secret.
- **Multi-Environment Deployment** — Dev → Staging → Production with configurable auto/manual promotion and approval gates; approver-role-gated approvals.
- **Docker Management** — build, list, inspect OCI images and manage containers with manifest details.
- **OCI Registry Integration** — push/pull via distribution-spec v1.1, browse tags, inspect manifests, referrers API for supply chain metadata. Pusher path exercised against the [upstream OCI distribution-spec conformance suite](https://github.com/opencontainers/distribution-spec/tree/main/conformance) in CI.
- **Kubernetes Dashboard** — manage workloads, scale, restart, view logs across clusters and namespaces.
- **SSO Authentication** — OpenID Connect with PKCE, RBAC (admin / operator / approver / viewer), configurable group-to-role mapping, optional step-up MFA on destructive admin routes.
- **Pluggable secrets backend** — AES-GCM in Postgres, [KeepSave](https://github.com/santapong/keepsave), HashiCorp Vault, AWS Secrets Manager, or GCP Secret Manager (per-project API keys, key rotation, env-to-env promotion).
- **GitOps commits** — `internal/gitops/gogit.go` writes manifests back to a git repo (`go-git/v5`, SSH-key / ssh-agent / HTTPS-basic auth chain).
- **Live Execution** — WebSocket-powered real-time pipeline status, per-stage build logs, and K8s event streaming. Redis pub/sub WS hub so broadcasts cross replicas.
- **Environment Swim Lanes** — visual grouping of pipeline stages by deployment environment.
- **Resilient by default** — SIGTERM-aware graceful shutdown (30s drain), Postgres reconnect-with-backoff at boot, lazy OIDC discovery, run-coordinator heartbeat + orphan sweep on every boot.
- **Fast CI, lean frontend** — CI critical path ~3 min on warm cache (parallel `go test ./...`, buildx GHA cache); frontend entry chunk 59 KB / 18 KB gzip (88% smaller than pre-PR-#38).

## Multi-Environment Pipeline

Cooker supports deploying across three environments with flexible infrastructure:

```
┌─── Dev ──────────┬─── Staging ──────┬─── Production ──────┐
│                  │                  │                      │
│ [Build] → [Test] │→ [Deploy-STG]   │→ [Approval] → [Deploy-PROD]
│     ↓            │      ↓           │                 ↓
│ [Push to Reg]    │  [Smoke Test]    │           [Health Check]
│                  │                  │                      │
└──────────────────┴──────────────────┴──────────────────────┘
```

- Each environment can target a **separate K8s cluster** or a **namespace** within a shared cluster
- Promotion between environments is **configurable per-edge**: auto-promote or manual approval
- Environment-specific variables override pipeline-level defaults (12-factor app style)

## API

Base path: `/api/v1`

| Area | Key Endpoints |
|------|---------------|
| **Pipelines** | CRUD, validate, run, list runs, get run details |
| **Docker** | List/inspect images (OCI manifest), build, list/manage containers |
| **Registry** | List repos/tags, get manifests, push/pull, referrers API |
| **Kubernetes** | List namespaces/workloads, scale, restart, pod logs, apply manifests |
| **Environments** | CRUD, promote, approve, per-env status, secret CRUD (admin-only) |
| **WebSocket** | Pipeline run stream, Docker build stream, K8s watch events |

## Project Structure

```
├── frontend/              React + TypeScript + Vite
│   └── src/
│       ├── components/    React Flow nodes, edges, panels, layout, ErrorBoundary
│       ├── stores/        Zustand (pipeline, Docker, K8s, environment)
│       ├── api/           Typed API client per domain
│       ├── auth/          OIDC provider + protected routes
│       ├── hooks/         WebSocket, pipeline execution, K8s watch
│       ├── types/         TypeScript types (pipeline, Docker, K8s, OCI)
│       └── pages/         Pipelines, Editor, Docker, K8s, Environments
├── backend/               Go API server
│   ├── cmd/cooker/        Entry point (slog handler install, signal handling, server.RunContext)
│   ├── internal/
│   │   ├── server/        HTTP server, router, WS hub (memory/redis), ticket store, rate limiter, RunCoordinator, health probes
│   │   ├── auth/          OIDC middleware (lazy discovery, atomic verifier), RBAC, RequireMFA
│   │   ├── handler/       HTTP handlers (pipeline, app, run, env, secret, docker, k8s, registry, webhook)
│   │   ├── service/       Business logic (executor, promoter, app deployer)
│   │   ├── audit/         Audit log sink (stdout / file) + middleware
│   │   ├── observability/ Prometheus /metrics + OpenTelemetry OTLP/gRPC setup
│   │   ├── secrets/       Secrets manager interface + adapters (database, keepsave, vault, awsm, gcpsm)
│   │   ├── builder/       Image builder strategy + adapters (docker, kaniko, buildah, buildkit)
│   │   ├── pusher/        Image push strategy + crane adapter
│   │   ├── deployer/      K8s deployer strategy + client-go adapter
│   │   ├── deploytarget/  Deploy target interface + adapters (kubernetes, cloudrun, ecs, flyio, render)
│   │   ├── gitops/        GitOpsCommit (go-git/v5)
│   │   ├── transport/     Optional transports (tsnet, build-tagged)
│   │   ├── crypto/        AES-GCM codec for app webhook secrets
│   │   ├── retry/         Bounded retry helpers
│   │   ├── idempotency/   Run-launch dedupe + pg_advisory_lock
│   │   ├── buildplan/     Clone→Build→Push→Deploy run synthesis from App
│   │   ├── source/        Repo clone helpers
│   │   ├── validate/      Cross-cutting validation
│   │   ├── model/         Domain types
│   │   ├── oci/           OCI image-spec types, media types, validation
│   │   ├── config/        Env-var loading + production Validate()
│   │   └── store/         Store interfaces + memory + PostgreSQL (with migrations)
│   └── pkg/
│       ├── dagrunner/     Reusable DAG execution engine (bounded fan-out, runDeadline)
│       └── ociutil/       OCI descriptor utilities
├── deploy/
│   ├── docker/            Dockerfiles (multi-stage, dev frontend, dev backend)
│   ├── kubernetes/        Raw manifests (namespace, deployment, service, ingress, RBAC)
│   └── helm/cooker/       Helm chart
├── docs/
│   ├── architecture.md    Full architecture document
│   ├── design.md          Design patterns, conventions, contributor checklist
│   ├── UAT.md             UAT runbook + how to enable OIDC for testers
│   ├── MULTI_REPLICA.md   Sticky-session + multi-replica gotchas
│   ├── RUNBOOK.md         Incident response — symptom-driven
│   └── adr/               Architecture decision records
├── CHANGELOG.md           Version history
├── SECURITY.md            Security policy and guidelines
├── backlog.md             Open work (planned, scoped, not shipped)
├── docker-compose.yml     Local development stack
├── renovate.json          Dependabot/Renovate configuration
└── Makefile               Build orchestration
```

## Deployment

```bash
# Build Docker image (multi-stage: frontend + backend → Alpine)
make docker-build

# Kubernetes (raw manifests)
kubectl apply -f deploy/kubernetes/

# Helm (recommended for production)
#
# Pre-create Kubernetes Secrets for the OIDC client secret and the
# COOKER_SECRET_KEY (AES-256 encryption key for env secrets at rest),
# then point the chart at them. Inline values work for testing but
# should not be used in production.
kubectl create secret generic cooker-oidc \
  --from-literal=client-secret=<value-from-idp>
kubectl create secret generic cooker-secret-key \
  --from-literal=key=$(head -c 32 /dev/urandom | base64)

helm install cooker deploy/helm/cooker/ \
  --set cookerEnv=production \
  --set 'oidc.allowedOrigins={https://cooker.example.com}' \
  --set oidc.enabled=true \
  --set oidc.issuerUrl=https://auth.example.com \
  --set oidc.clientId=cooker \
  --set oidc.clientSecretRef.name=cooker-oidc \
  --set oidc.redirectUrl=https://cooker.example.com/callback \
  --set secretKey.existingSecret=cooker-secret-key \
  --set 'ingress.tls[0].secretName=cooker-tls' \
  --set 'ingress.tls[0].hosts[0]=cooker.example.com'
```

See [SECURITY.md](SECURITY.md) for the production deployment security checklist.

### TLS at ingress

OIDC sign-in **requires HTTPS** — most IdPs reject non-HTTPS redirect URIs. Cooker doesn't terminate TLS itself; the ingress controller (or cloud LB) does. Pattern: provision the certificate with [cert-manager](https://cert-manager.io/) + Let's Encrypt and reference the resulting Secret in `values.yaml`:

```yaml
# values.yaml
ingress:
  enabled: true
  className: nginx
  hosts:
    - host: cooker.example.com
      paths: [{ path: /, pathType: Prefix }]
  tls:
    - secretName: cooker-tls
      hosts: [cooker.example.com]
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
```

The `--set` flags in the `helm install` snippet above set the same values from the command line.

### PostgreSQL SSL

Production should not connect to PostgreSQL over plaintext. Two paths, depending on how Postgres is provisioned:

1. **External PostgreSQL** (managed RDS / Cloud SQL / on-prem) — set the full `DATABASE_URL` with `?sslmode=require` (or `verify-full` if you mount a CA bundle into the pod):

   ```bash
   helm install cooker deploy/helm/cooker/ \
     --set 'env[0].name=DATABASE_URL' \
     --set 'env[0].value=postgres://user:pass@db.internal:5432/cooker?sslmode=require'
   ```

2. **Bundled `bitnami/postgresql` subchart** — set `postgresql.sslMode` (already documented in `values.yaml`) and the bitnami subchart's `tls.enabled=true` to provision a CA-signed server cert.

Valid `sslmode` values, in increasing strictness: `disable | allow | prefer | require | verify-ca | verify-full`. Use **`require`** as the floor for production and **`verify-full`** when you have a CA bundle.

> **Note:** chart-side rendering of `postgresql.sslMode` into the constructed `DATABASE_URL` is a follow-up. Today operators set the full URL via `env` overrides as shown above. Tracked in backlog `P1.4`.

### Multi-replica deployments

Two pieces of Cooker state are per-process today: the rate limiter and the WebSocket ticket store. Running 2+ replicas requires either sticky sessions at the ingress (recommended now) or Redis-backed shared state (open backlog item P3). Concrete ingress annotations for NGINX, ALB, Traefik, HAProxy, and Envoy + the failure modes you'd see without action are in [docs/MULTI_REPLICA.md](docs/MULTI_REPLICA.md).

## Secrets backends

Cooker supports five backends for environment-scoped secrets, selected at boot via `COOKER_SECRETS_BACKEND`. The `Manager` interface lives at `backend/internal/secrets/manager.go`; adapters are independent packages. Switching is purely a config change — handler logic and the on-the-wire API are identical between backends.

| Backend | When to use | Storage | Encryption |
|---|---|---|---|
| `database` *(default)* | Single-Cooker installs; no separate secrets infra | `environments.secrets` JSONB column | AES-GCM (`COOKER_SECRET_KEY`, base64-encoded 32 bytes) |
| `keepsave` | Multi-tenant or audit-heavy environments; dedicated secrets infra | [KeepSave](https://github.com/santapong/keepsave) server (system of record) | AES-256-GCM at rest, managed by KeepSave; per-project DEKs |
| `vault` | Existing HashiCorp Vault deployment; want PKI / dynamic secrets path | Vault KV v2 mount | Managed by Vault |
| `aws` | AWS-native deployment (EKS / ECS / Lambda) | AWS Secrets Manager (one secret per `<prefix>/<envID>/<key>`) | KMS (managed by AWS) |
| `gcp` | GCP-native deployment (GKE / Cloud Run) | GCP Secret Manager (one secret per `<prefix>__<envID>__<key>`) | Managed by GCP |

### `database` (default)

```bash
COOKER_SECRETS_BACKEND=database          # or unset
COOKER_SECRET_KEY=$(head -c 32 /dev/urandom | base64)
```

In production with this backend, `COOKER_SECRET_KEY` is required and validated at boot. With no key set, the secret API returns `503` so the operator notices the gap rather than silently storing plaintext.

### `keepsave`

A single KeepSave project owns all of Cooker's secrets. Cooker's environment **name** (`prod`, `uat`, etc.) maps to KeepSave's `environment` query parameter; per-environment isolation comes from KeepSave's per-env API-key scoping. KeepSave's `/promote` endpoint is available for future wiring of Cooker's secret-promotion flow.

```bash
COOKER_SECRETS_BACKEND=keepsave
COOKER_SECRETS_KEEPSAVE_URL=http://keepsave:8080
COOKER_SECRETS_KEEPSAVE_PROJECT_ID=<cooker-project-uuid>
COOKER_SECRETS_KEEPSAVE_API_KEY=ks_xxxx
```

With this backend, `COOKER_SECRET_KEY` is no longer required — KeepSave handles encryption. Production startup validation rejects partial config: any one of the three KeepSave variables missing is fatal.

### `vault` (HashiCorp Vault, KV v2)

```bash
COOKER_SECRETS_BACKEND=vault
COOKER_SECRETS_VAULT_ADDR=https://vault.example.com:8200
COOKER_SECRETS_VAULT_MOUNT=secret           # KV v2 mount path
COOKER_SECRETS_VAULT_PREFIX=cooker          # path under <mount>
# Token via env (works with Vault Agent injector that mounts the token):
COOKER_SECRETS_VAULT_TOKEN=$(cat /vault/secrets/token)
```

Each Cooker environment maps to one Vault secret at `<mount>/data/<prefix>/<envID>` with one field per Cooker key. Vault handles encryption and audit. Empty `_TOKEN` is allowed when Vault Agent provides the token at boot via the SDK's chain.

### `aws` (AWS Secrets Manager)

```bash
COOKER_SECRETS_BACKEND=aws
COOKER_SECRETS_AWS_REGION=us-east-1         # auto-discovered from instance metadata
COOKER_SECRETS_AWS_PREFIX=cooker            # AWS secret IDs become "<prefix>/<envID>/<key>"
```

Auth via the standard AWS chain: IRSA on EKS, instance profile on EC2, env vars locally. One AWS secret per Cooker key — keeps per-key versioning and IAM scoping clean.

### `gcp` (Google Cloud Secret Manager)

```bash
COOKER_SECRETS_BACKEND=gcp
COOKER_SECRETS_GCP_PROJECT_ID=my-gcp-project
COOKER_SECRETS_GCP_PREFIX=cooker            # secrets named "<prefix>__<envID>__<key>"
```

Auth via Application Default Credentials (Workload Identity on GKE / `GOOGLE_APPLICATION_CREDENTIALS` elsewhere). The double-underscore separator works around GCP Secret Manager's `[A-Za-z0-9_-]` naming rule.

**Switching backends:** secrets do not auto-migrate. Plan a one-shot copy step (read from old, write to new) before flipping the env var. Both reads and writes go to a single backend at runtime — there is no live dual-write.

See [ADR-0002](docs/adr/0002-secrets-manager.md) for the full rationale (tenancy, system-of-record, alternatives rejected).

## Cloud deploy targets

In addition to Kubernetes (the original target), Cooker can deploy Apps to managed cloud runtimes. Each target is opt-in: set the relevant config block and the adapter self-registers at boot.

| Target | Implementation | Required config |
|---|---|---|
| `cloud-run` | `cloud.google.com/go/run/apiv2` create/update + traffic-split rollback | `COOKER_DEPLOY_CLOUDRUN_PROJECT`, `COOKER_DEPLOY_CLOUDRUN_REGION` (ADC for auth) |
| `ecs` | `aws-sdk-go-v2/service/ecs` register-task-def + create/update service + revision-based rollback | `COOKER_DEPLOY_ECS_REGION`, `COOKER_DEPLOY_ECS_CLUSTER` (+ optional `_EXECUTION_ROLE`, `_TASK_ROLE`, `_SUBNETS`, `_SECURITY_GROUPS`) |
| `fly` | REST against `api.machines.dev`; auto-creates the fly app on first deploy | `COOKER_DEPLOY_FLY_TOKEN`, optional `COOKER_DEPLOY_FLY_REGION` |
| `render` | REST against `api.render.com/v1`; triggers a deploy on an operator-created Render service | `COOKER_DEPLOY_RENDER_TOKEN`, optional `COOKER_DEPLOY_RENDER_OWNER_ID` |

Targets without their config are not registered — operators don't have to wire backends they don't use.

## Observability (opt-in)

Both off by default. Turn them on in production via env / Helm.

```bash
# Prometheus /metrics endpoint exposed on the same port as the API.
COOKER_METRICS_ENABLED=true

# OpenTelemetry traces over OTLP/gRPC.
COOKER_TRACING_ENABLED=true
COOKER_OTLP_ENDPOINT=otel-collector.observability.svc.cluster.local:4317
COOKER_OTLP_INSECURE=true                   # in-cluster OTLP rarely uses TLS
COOKER_SERVICE_NAME=cooker
COOKER_SERVICE_VERSION=v0.1.0
```

Metrics: `cooker_http_requests_total{method,route,status}` (counter) and `cooker_http_request_duration_seconds{method,route}` (histogram). Routes are labelled by Gin's matched template (e.g. `/api/v1/pipelines/:id`), not the concrete URL, to keep cardinality bounded.

Logs: structured JSON via `log/slog` — every line carries `time`, `level`, `msg`, plus structured fields (e.g. `pipeline=<id>`, `stage=<name>`).

## Multi-replica deployments

Three cooker-internal pieces of state are per-process by default and need shared state to survive replica scaling: the **rate limiter**, the **WebSocket ticket store**, and the **WebSocket broadcast hub**. Either:

- **Sticky sessions at ingress** (simpler — works for typical workloads). See [docs/MULTI_REPLICA.md](docs/MULTI_REPLICA.md) for NGINX / ALB / Traefik / HAProxy / Envoy snippets. Requires `COOKER_STICKY_SESSIONS=true` for `Config.Validate` to permit multi-replica boots with memory backends.
- **Redis-backed state** (proper HA, chart default). Set `COOKER_RATE_LIMIT_BACKEND=redis`, `COOKER_WS_TICKET_BACKEND=redis`, and `COOKER_WS_HUB_BACKEND=redis`; all three consume the existing `REDIS_URL`. Rate limiting uses GCRA via `go-redis/redis_rate/v10`; WS tickets use atomic `GETDEL` so a single ticket can never be redeemed twice across replicas; broadcasts cross replicas via the `cooker:ws:broadcast` Redis pub/sub channel with a length-prefixed binary frame and jittered subscriber reconnect.

## Operations

| What you need | Where to look |
|---|---|
| TLS / cert-manager / production install | This README — *Deployment → TLS at ingress* |
| Multi-replica (sticky sessions or Redis) | This README — *Multi-replica deployments* + [docs/MULTI_REPLICA.md](docs/MULTI_REPLICA.md) |
| Prometheus + OpenTelemetry setup | This README — *Observability (opt-in)* |
| Incident response (build hung, DB down, OIDC unreachable, KeepSave outage, OOMKilled) | [docs/RUNBOOK.md](docs/RUNBOOK.md) |
| Secrets backend selection + switching | This README — *Secrets backends* |
| Cloud deploy targets (Cloud Run / ECS / Fly / Render) | This README — *Cloud deploy targets* |
| Production hardening checklist | [SECURITY.md](SECURITY.md) |
| Open work, sequencing, effort estimates | [backlog.md](backlog.md) |

## Documentation

### For users

| Document | Description |
|----------|-------------|
| [docs/user-guide/index.md](docs/user-guide/index.md) | User-guide landing — concepts, getting started, guides, operations, reference, troubleshooting, FAQ |
| [docs/user-guide/getting-started/](docs/user-guide/getting-started/) | Quickstart, Helm install, configuration, upgrading |
| [docs/user-guide/concepts/](docs/user-guide/concepts/) | Pipelines, Apps, Stages, Runs, Environments, Hosts & Targets |
| [docs/user-guide/guides/](docs/user-guide/guides/) | First pipeline, K8s deploy, registries, secrets, promotions, GitHub webhooks, notifications, self-hosting |
| [docs/user-guide/operations/](docs/user-guide/operations/) | Architecture, auth/RBAC, Docker builds, Postgres, observability, troubleshooting |
| [docs/user-guide/reference/](docs/user-guide/reference/) | API, CLI, env vars, webhooks |

### For operators

| Document | Description |
|----------|-------------|
| [docs/ROLLOUT.md](docs/ROLLOUT.md) | UAT → production cutover playbook (single source of truth for cutovers) |
| [docs/RUNBOOK.md](docs/RUNBOOK.md) | Incident response: symptom → checks → cause → mitigation; Alertmanager rules |
| [docs/UAT.md](docs/UAT.md) | UAT runbook + how to enable OIDC sign-in for testers |
| [docs/MULTI_REPLICA.md](docs/MULTI_REPLICA.md) | Sticky-session + Redis-shared-state guidance for multi-replica deploys |
| [SECURITY.md](SECURITY.md) | Security policy, auth architecture, production hardening checklist |
| [backlog.md](backlog.md) | Open work, sequencing, effort estimates; deployment-shape readiness matrix |

### For contributors

| Document | Description |
|----------|-------------|
| [docs/architecture.md](docs/architecture.md) | System architecture, component map, data flow, OCI integration |
| [docs/design.md](docs/design.md) | Design patterns, conventions, auth flow, testing strategy, contributor checklist (§11) |
| [docs/adr/](docs/adr/) | Architecture decision records (strategy interfaces, secrets manager, JSONB graph, **multi-tenancy A3-defer**) |
| [docs/openapi.yaml](docs/openapi.yaml) | Hand-curated OpenAPI 3.1 sketch |
| `backend/docs/api/swagger.yaml` | Generated OpenAPI from `swag` annotations — regenerate with `make swagger` |
| [CHANGELOG.md](CHANGELOG.md) | Version history following Keep a Changelog format |

### Planning + research (May 2026 audit week + W1 follow-up)

| Document | Description |
|----------|-------------|
| [docs/roadmap-2026.md](docs/roadmap-2026.md) | 2026 themes and top-30 — strategic frame for the year |
| [docs/pm-brief-2026-05.md](docs/pm-brief-2026-05.md) | 15-item 90-day plan + 8 open decisions gating work |
| [docs/dag-adaptation-2026.md](docs/dag-adaptation-2026.md) | 20-week DAG-primitives plan (5 primitives, 5 tidy-first refactors T1–T5, 4 ADRs) |
| [docs/protocols.md](docs/protocols.md) | Custom Cooker protocols proposal — CKR-LOG/1 (binary log stream) + CKR-DSL (pipeline DSL) |
| [docs/shipping-go.md](docs/shipping-go.md) | Research: how mature OSS Go products release and operate, applied to Cooker |
| [docs/marketing/strategy.md](docs/marketing/strategy.md) | OSS adoption marketing strategy (90-day post-launch horizon) |
| [docs/audits/](docs/audits/) | Audit series. May 2026: security review, perf/optimization, dag-performance, W11 personas, W11 follow-up, adapter-wiring, handler-layering, store-parity, deploy-parity, frontend-hygiene, half-shipped (W1); action-pinning, cache-plumb sketch, P#1 unmarshaller corpus, P#1 context-pack, W11 quickwin wireframes (W2); handler-F2+F3 extraction sketch, deploytarget walk, P#3 JSONB-cap design, T-series W4 coordination (W3); F2+F3 ready-to-fire prompts, CI baseline, SECURITY.md post-W3 walk, Redis-failover reconnect (W4); **P#3 schema sketch (W5)**, **useStageLogs reconnect (W5, HIGH gap)** |
| [docs/RELEASING.md](docs/RELEASING.md) | Release cutting playbook — tag → workflow → verify (added W3) |
| [docs/SECURITY-RELEASE-VERIFY.md](docs/SECURITY-RELEASE-VERIFY.md) | Publish-time verification checklist (cosign + permissions + non-root + port surface) (added W3) |

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/my-feature`)
3. Run tests: `make test`
4. Commit changes
5. Push and open a pull request

## License

MIT
