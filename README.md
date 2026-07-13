<div align="center">

# 🍳 Cooker

**A web-based CI/CD platform with a visual DAG editor for building, pushing, and deploying OCI images to Kubernetes and cloud runtimes.**

*Drag stages onto a canvas. Wire them into a DAG. Ship to production.*

[![Go Version](https://img.shields.io/badge/go-1.22+-00ADD8?logo=go&logoColor=white)](https://golang.org/dl/)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![CI](https://github.com/santapong/Cooker/actions/workflows/ci.yml/badge.svg)](https://github.com/santapong/Cooker/actions/workflows/ci.yml)
[![OCI Conformance](https://github.com/santapong/Cooker/actions/workflows/oci-conformance.yml/badge.svg)](https://github.com/santapong/Cooker/actions/workflows/oci-conformance.yml)
[![GHCR](https://img.shields.io/badge/container-ghcr.io%2Fsantapong%2Fcooker-2496ED?logo=docker&logoColor=white)](https://github.com/santapong/Cooker/pkgs/container/cooker)
[![Helm](https://img.shields.io/badge/helm-oci%3A%2F%2Fghcr.io%2Fsantapong%2Fcharts%2Fcooker-0F1689?logo=helm&logoColor=white)](https://github.com/santapong/Cooker/pkgs/container/charts%2Fcooker)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](https://github.com/santapong/Cooker/pulls)

[Quick Start](#-quick-start) • [Documentation](#-documentation) • [Architecture](#-architecture) • [Configuration](#-configuration) • [API](#-api-reference) • [FAQ](#-faq)

</div>

---

## 📑 Table of Contents

<details>
<summary>Click to expand</summary>

- [Overview](#-overview)
- [Why Cooker?](#-why-cooker)
- [Comparison](#-comparison)
- [Features](#-features)
- [Screenshots](#-screenshots)
- [Quick Start](#-quick-start)
  - [Local development](#local-development-docker-compose)
  - [UAT mode](#uat-mode-single-binary)
  - [Production install (Helm)](#production-install-helm)
- [Architecture](#-architecture)
- [Configuration](#-configuration)
  - [Common environment variables](#common-environment-variables)
  - [Secrets backends](#secrets-backends)
  - [Multi-replica setup](#multi-replica-setup)
- [Builders & Deploy Targets](#-builders--deploy-targets)
- [API Reference](#-api-reference)
- [OCI Compliance](#-oci-compliance)
- [Project Structure](#-project-structure)
- [Development](#-development)
- [Documentation](#-documentation)
- [Observability](#-observability)
- [Security](#-security)
- [Troubleshooting](#-troubleshooting)
- [FAQ](#-faq)
- [Contributing](#-contributing)
- [What's not done yet](#-whats-not-done-yet)
- [Roadmap](#-roadmap)
- [License](#-license)

</details>

---

## 🌟 Overview

**Cooker** is a single-binary CI/CD platform that lets teams design pipelines visually and run them against real container infrastructure. Drag stages onto a canvas, wire them into a DAG, and Cooker handles building OCI-compliant images, pushing them to registries, and rolling them out across Dev, Staging, and Production environments.

Whether you're a solo developer deploying a side project to Fly.io or a platform team running multi-tenant Kubernetes clusters, Cooker scales with you — from a one-shot Docker Compose stack on your laptop to a HA Redis-backed multi-replica deployment in production.

> **Cooker is an open-source, self-hosted CI/CD tool with a drag-drop graph editor for building OCI images (Kaniko, BuildKit, Buildah) and deploying to Kubernetes, ECS, Cloud Run, Fly.io, and Render — single Go binary, Apache-2.0-licensed, no SaaS, no agents.**

> **Status:** production-ready on single-replica and multi-replica (Redis-backed) deployments. `Config.Validate` refuses unsafe boots in production. See the [rollout playbook](docs/guides/ROLLOUT.md) for the UAT → production cutover.

> **Recently shipped (May 2026 W6):** Phase 1 + Phase 2 of the Dokploy adaptation work landed in PR #89 — a durable Postgres-backed job queue, formal run/stage state machine, resource-action permission middleware, multi-channel notifications (Slack/Discord/Email/Webhook), cron-triggered runs, GitLab/Bitbucket/Gitea webhook receivers, and a pipeline templates catalog. All gated behind default-off feature flags; merging is a no-op until an operator opts in. See [`docs/adapted-from-dokploy.md`](docs/proposals/adapted-from-dokploy.md) for the attribution matrix and [`docs/architecture-phase1-phase2.md`](docs/reference/architecture-phase1-phase2.md) for the new subsystem deep-dive.

## 🤔 Why Cooker?

| Pain point | How Cooker solves it |
|------------|---------------------|
| **"Our CI uses XML / a custom DSL I don't understand"** | Visual DAG editor — drag, drop, connect. Pipeline-as-code is supported via the upcoming CKR-DSL but isn't required. |
| **"I want to see my pipelines, not edit YAML in a separate repo"** | Pipelines, runs, environments, and apps are all first-class entities in the UI. Live status. Real-time logs. |
| **"My CI builds the image but I need a separate tool to deploy it"** | Build, push, and deploy are stages in the same pipeline. Approval gates between environments. Rollback per deploy target. |
| **"Multi-environment deployments are a nightmare to coordinate"** | Native Dev / Staging / Production environments. Auto-promotion or manual approval gates per edge. RBAC-gated approval. |
| **"I want OCI compliance, not a vendor-locked registry"** | Built on go-containerregistry. Continuously verified against the upstream [OCI conformance suite](https://github.com/opencontainers/distribution-spec) in CI. |
| **"Secrets management is bolted on"** | Five pluggable backends: Postgres (AES-GCM), KeepSave, HashiCorp Vault, AWS Secrets Manager, GCP Secret Manager. |

## ⚔️ Comparison

| Feature | Cooker | Jenkins | Argo CD | Drone CI |
|---------|--------|---------|---------|----------|
| **Visual DAG editor** | ✅ | ❌ (Blue Ocean abandoned) | ❌ (status-only viz) | ❌ |
| **OCI-native builds** | ✅ (Kaniko / BuildKit / Buildah) | ⚠️ (plugins required) | ❌ (build-agnostic) | ✅ |
| **GitOps support** | ✅ (`internal/gitops/`) | ⚠️ (3rd-party) | ✅ (its core model) | ❌ |
| **Multi-environment promotion** | ✅ (first-class) | ⚠️ (manual) | ✅ (ApplicationSets) | ❌ |
| **Approval gates** | ✅ (RBAC-gated) | ✅ | ⚠️ (sync windows) | ❌ |
| **Pluggable secrets** | ✅ (5 backends) | ⚠️ (Credentials plugin) | ⚠️ (Vault plugin) | ⚠️ (plugins) |
| **Single binary** | ✅ | ❌ (JVM + .war) | ❌ (multi-service) | ✅ |
| **OIDC + PKCE** | ✅ (built-in) | ⚠️ (plugin) | ✅ | ⚠️ |
| **Real-time WebSocket logs** | ✅ | ⚠️ (polling) | ⚠️ | ✅ |
| **Cloud Run / ECS / Fly / Render targets** | ✅ | ⚠️ (plugins) | ❌ (K8s only) | ❌ |

> 📊 **In-depth, honest comparisons** (we list where they win, too): [vs GitHub Actions](docs/compare/cooker-vs-github-actions.md) · [vs Coolify](docs/compare/cooker-vs-coolify.md) · [vs Drone](docs/compare/cooker-vs-drone.md) · [vs Woodpecker](docs/compare/cooker-vs-woodpecker.md) · [vs Argo Workflows](docs/compare/cooker-vs-argo-workflows.md)

## ✨ Features

<details>
<summary><b>🎨 Pipeline Authoring</b></summary>

- **Visual DAG editor** powered by [React Flow](https://reactflow.dev/) — drag, drop, connect stages
- **Six stage types**: Build, Test, Push, Deploy, Approval, Custom
- **Simple ⇄ Pro toggle** — beginners get guard rails, experts get raw access to all knobs
- **Apps abstraction** — point at a GitHub repo, pick a deploy target, ship in one click
- **Forward-compat trigger conditions** — edges accept `success` (default), with `failure`/`always` arriving in the next primitive
- **Environment swim lanes** — visual grouping of stages by deployment environment

</details>

<details>
<summary><b>⚡ Execution</b></summary>

- **Live WebSocket-streamed build logs** for every stage (build, push, deploy)
- **Configurable retry, fan-out limits, and run deadlines** per pipeline
- **Auto-promotion or manual approval** gates between environments
- **GitHub webhook triggers** with per-app HMAC secrets
- **GitOps mode** — write rendered manifests back to a git repo via `go-git/v5` (SSH key, ssh-agent, or HTTPS basic auth)
- **Bounded fan-out** prevents thundering herd against your registry / cluster
- **Run deadlines** — circuit-break long-running stages automatically
- **Graceful 30 s shutdown** drains in-flight runs on SIGTERM
- **Orphan sweep** reaps stale runs after OOM kills
- **Durable async job queue** (Phase 1 / default-off) — runs survive restarts via Postgres `FOR UPDATE SKIP LOCKED` + `LISTEN/NOTIFY`; per-pipeline concurrency keys
- **Formal state machine** (Phase 1) for run / stage status — invalid transitions rejected at the API boundary, terminal-sticky
- **Cron-triggered runs** (Phase 2 / default-off) — leader-elected via `pg_advisory_lock`; in-house POSIX cron parser with IANA timezone support
- **Multi-provider git webhooks** — GitHub + GitLab + Bitbucket Server + Gitea, each with provider-correct signature verification (`hmac.Equal` / `subtle.ConstantTimeCompare`)
- **Multi-channel notifications** — Slack, Discord, Email (SMTP), generic JSON webhook; per-target event-type filters
- **Pipeline templates** — catalog + create-from-template with fresh-ID deep-copy + DAG re-validation

</details>

<details>
<summary><b>🛠️ Builders & Registries</b></summary>

Four builders selectable via `COOKER_BUILDER`:

| Builder | Use case |
|---------|----------|
| **Kaniko** (default) | In-cluster Job builds — no host docker.sock, secure by default |
| **BuildKit** | gRPC against an external buildkitd — fastest cold builds |
| **Buildah** | In-cluster Job — full Dockerfile parity, rootless |
| **Docker** | Local docker.sock — **dev only, refused at boot in production** |

Push path uses **`crane`** via [go-containerregistry](https://github.com/google/go-containerregistry) — continuously verified against the [upstream OCI distribution-spec conformance suite](https://github.com/opencontainers/distribution-spec/tree/main/conformance) in CI.

</details>

<details>
<summary><b>🌐 Deploy Targets</b></summary>

Cooker can deploy to multiple targets in one pipeline:

| Target | Implementation | What's special |
|--------|----------------|----------------|
| **Kubernetes** | client-go + kubectl fallback | Manifest apply, per-resource status, native log streaming |
| **AWS ECS / Fargate** | aws-sdk-go-v2 | Task-def registration + service update with rollback to previous revision |
| **Google Cloud Run** | cloud.google.com/go/run | Traffic-split rollback (route 0% to canary on failure) |
| **Fly.io Machines** | REST against `api.machines.dev` | Update-in-place per machine, per-machine restart for rollback |
| **Render** | REST against `api.render.com/v1` | Triggers deploy on operator-created Render service |

Targets without config aren't registered — operators don't have to wire backends they don't use.

</details>

<details>
<summary><b>🔐 Authentication & Authorization</b></summary>

- **OIDC / OAuth 2.0 with PKCE** — works with Keycloak, Okta, Azure AD, Google, GitHub, Auth0, any compliant IdP
- **Four-role RBAC**: `admin` / `operator` / `approver` / `viewer`
- **Configurable group-to-role mapping** via `COOKER_OIDC_GROUP_MAP` (e.g. `cooker-admins:admin,cooker-deploy:operator`)
- **Step-up MFA** on destructive admin routes — opt-in via `COOKER_OIDC_MFA_ACR_VALUES`
- **Generic auth errors** — server-side detail logged via slog; client sees only `authentication failed` / `provider unavailable`
- **Lazy OIDC discovery** — Cooker boots even if your IdP is unreachable; discovery retries with backoff

| Role | Can do |
|------|--------|
| `admin` | Everything (CRUD all resources, secrets, RBAC) |
| `operator` | Run pipelines, deploy apps, view secrets (read-only) |
| `approver` | Approve environment promotions (production gates) |
| `viewer` | Read-only view of all resources |

</details>

<details>
<summary><b>🔑 Secrets Management</b></summary>

Five pluggable backends, selectable at boot via `COOKER_SECRETS_BACKEND`. The API surface is identical across backends — switching is a config change.

| Backend | Best for | Storage | Encryption |
|---------|----------|---------|------------|
| **`database`** *(default)* | Simple single-Cooker installs | Postgres JSONB column | AES-GCM (`COOKER_SECRET_KEY`) |
| **`keepsave`** | Multi-tenant, audit-heavy | [KeepSave](https://github.com/santapong/keepsave) server | AES-256-GCM (KeepSave-managed) |
| **`vault`** | Teams with HashiCorp Vault | Vault KV v2 mount | Vault-managed |
| **`aws`** | AWS-native (EKS / ECS / Lambda) | AWS Secrets Manager | KMS |
| **`gcp`** | GCP-native (GKE / Cloud Run) | GCP Secret Manager | GCP-managed |

Plus `POST /api/v1/environments/:id/secrets/promote` for cross-env promotion via the `secrets.Promoter` interface.

</details>

<details>
<summary><b>📊 Observability</b></summary>

- **Prometheus `/metrics`** — opt-in via `COOKER_METRICS_ENABLED=true`. Exposes:
  - `cooker_http_requests_total{method,route,status}` (counter)
  - `cooker_http_request_duration_seconds{method,route}` (histogram)
  - Four resilience counters: `cooker_db_connection_errors_total`, `cooker_redis_connection_errors_total`, `cooker_jwks_fetch_failures_total`, `cooker_pipeline_runs_orphaned_total`
- **OpenTelemetry / OTLP traces** — opt-in via `COOKER_TRACING_ENABLED=true`
- **Structured JSON logs** via `log/slog` — every line carries `time`, `level`, `msg`, plus structured fields (`pipeline=<id>`, `stage=<name>`)
- **Per-route audit log** — on by default in production. Destination: `stdout` or `file` via `COOKER_AUDIT_DESTINATION` / `COOKER_AUDIT_FILE_PATH`
- **Split health probes**:
  - `/health/live` — pod is alive
  - `/health/ready` — DB ping + Redis ping + JWKS age (per-check breakdown in response body)
- **Recommended Alertmanager rules** in [`docs/RUNBOOK.md`](docs/guides/RUNBOOK.md)

</details>

<details>
<summary><b>🔄 Multi-replica & HA</b></summary>

Three pieces of internal state are per-process by default. Either pin sticky sessions at ingress (simpler) or back them with Redis (proper HA):

| State | Memory backend | Redis backend |
|-------|----------------|---------------|
| **Rate limiter** | `golang.org/x/time/rate` | GCRA via `go-redis/redis_rate/v10` |
| **WebSocket tickets** | sync.Map with TTL | Atomic `GETDEL` on Redis 6.2+ |
| **WebSocket broadcast hub** | in-process channels | `cooker:ws:broadcast` pub/sub with jittered reconnect |

Helm chart defaults to all three Redis-backed. Enable via:

```bash
COOKER_RATE_LIMIT_BACKEND=redis
COOKER_WS_TICKET_BACKEND=redis
COOKER_WS_HUB_BACKEND=redis
```

</details>

## 📸 Screenshots

> _Add your own screenshots to `docs/images/` and reference them here_

| Pipeline Editor | Run View | App Detail |
|-----------------|----------|------------|
| _Drag-and-drop DAG_ | _Live logs streaming_ | _Webhook + deploy URL_ |

## 🚀 Quick Start

### Local development (Docker Compose)

The fastest way to try Cooker locally:

```bash
git clone https://github.com/santapong/Cooker.git
cd Cooker
docker compose up
```

Then open:

| URL | What |
|-----|------|
| http://localhost:5173 | Frontend (Vite dev server with HMR) |
| http://localhost:8080/api/v1 | Backend API |
| http://localhost:8080/health/ready | Health probe |
| http://localhost:8080/metrics | Prometheus metrics (if enabled) |

By default OIDC is **off** in local mode — the backend injects a dev admin user so you can start building immediately.

### UAT mode (single binary)

Mimics production — single binary serving both API and SPA on port 8080:

```bash
# Single binary, OIDC off, dev admin injected
make uat-up

# Same + pre-seeded Keycloak realm
# Users: alice/admin (admin role), bob/viewer (viewer role)
make uat-up-with-keycloak

# Same + tecnativa/docker-socket-proxy
# Drops host socket bind mount for security testing
make uat-up-socketproxy

# End-to-end smoke test: boots UAT, runs one no-op pipeline, asserts success
make test-e2e
```

### Production install (Helm)

**Prerequisites:**

- Kubernetes 1.25+ cluster with an ingress controller (NGINX, ALB, Traefik, etc.)
- Postgres 14+ (managed or in-cluster)
- Redis 7+ (for multi-replica) — optional for single-replica
- cert-manager + Let's Encrypt (or equivalent) for TLS
- An OIDC provider (Keycloak, Okta, Auth0, Azure AD, Google Workspace, GitHub Enterprise)

**Step 1 — Provision secrets:**

```bash
# OIDC client secret (paste from your IdP's app config)
kubectl create secret generic cooker-oidc \
  --from-literal=client-secret=<paste-value>

# 32-byte AES key for env-secret encryption at rest
kubectl create secret generic cooker-secret-key \
  --from-literal=key=$(head -c 32 /dev/urandom | base64)
```

**Step 2 — Install via Helm OCI chart:**

```bash
helm install cooker oci://ghcr.io/santapong/charts/cooker \
  --version 0.2.0 \
  --namespace cooker --create-namespace \
  --set cookerEnv=production \
  --set oidc.enabled=true \
  --set oidc.issuerUrl=https://auth.example.com \
  --set oidc.clientId=cooker \
  --set oidc.clientSecretRef.name=cooker-oidc \
  --set oidc.redirectUrl=https://cooker.example.com/callback \
  --set secretKey.existingSecret=cooker-secret-key \
  --set 'ingress.tls[0].secretName=cooker-tls' \
  --set 'ingress.tls[0].hosts[0]=cooker.example.com' \
  --set 'ingress.hosts[0].host=cooker.example.com'
```

**Step 3 — Verify:**

```bash
kubectl -n cooker wait --for=condition=available deployment/cooker --timeout=120s
kubectl -n cooker port-forward svc/cooker 8080:80
curl localhost:8080/health/ready
```

📖 **Full installation guide:** [docs/user-guide/getting-started/helm-install.md](docs/user-guide/getting-started/helm-install.md)
📖 **TLS / cert-manager setup:** [docs/ROLLOUT.md](docs/guides/ROLLOUT.md)
📖 **Multi-replica configuration:** [docs/MULTI_REPLICA.md](docs/guides/MULTI_REPLICA.md)

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────────────┐
│  Browser: React + TypeScript + React Flow (graph UI)    │
│  Zustand state · WebSocket live updates                 │
└──────────────────────┬──────────────────────────────────┘
                       │ HTTPS / WSS
┌──────────────────────▼──────────────────────────────────┐
│              Go API Server (Gin)                        │
│  ┌──────────┬──────────┬──────────┬──────────────────┐  │
│  │ Pipeline │  Docker  │   K8s    │   Registry       │  │
│  │ Engine   │  Service │  Service │   Service        │  │
│  └────┬─────┴─────┬────┴────┬─────┴────────┬─────────┘  │
│       │           │         │              │            │
│  DAG Runner  Docker SDK  client-go  go-containerregistry│
│                                                         │
│  ┌────────────────────────────────────────────────────┐ │
│  │  PostgreSQL (state) + Redis (cache + pub/sub)      │ │
│  └────────────────────────────────────────────────────┘ │
└───────┬──────────┬──────────────┬───────────────────────┘
        ▼          ▼              ▼
   Docker Engine  Kubernetes API  OCI Registries / Cloud APIs
```

### Stack summary

| Layer | Tech |
|-------|------|
| **Frontend** | React 18 + TypeScript + React Flow + Zustand + Vite |
| **Backend** | Go 1.22+ · Gin · Docker SDK · client-go · go-containerregistry |
| **State** | PostgreSQL 14+ (pipelines, runs, envs, apps) |
| **Cache / pub-sub** | Redis 7+ (rate limit, WS tickets, broadcast hub) |
| **Container** | Multi-stage Alpine, non-root UID 65532, distroless-style |
| **Distribution** | GHCR multi-arch (amd64 + arm64) · Helm OCI chart · cosign-signed |
| **CI** | GitHub Actions — backend (race-test), frontend (build), docker, helm jobs |

### Layering rules

Cooker enforces strict layering:

```
HTTP request
    ↓
handler/     ← Thin HTTP parsing layer. No business logic. No DB calls.
    ↓
service/     ← Business logic. Returns domain types, never HTTP errors.
    ↓
store/       ← Postgres / memory adapters. Returns ErrNotFound etc.
strategy/    ← Pluggable backends (builder, pusher, deployer, secrets, ...)
```

See [`docs/design.md`](docs/reference/design.md) for the full design conventions.

📖 **Full architecture document:** [docs/architecture.md](docs/reference/architecture.md)
📖 **Design decisions (ADRs):** [docs/adr/](docs/adr/)

## 🔧 Configuration

Cooker is configured entirely via environment variables. Production-mode (`COOKER_ENV=production`) triggers strict `Config.Validate` at boot — misconfiguration fails loudly rather than silently.

### Common environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `COOKER_ENV` | `dev` | `dev` · `uat` · `production` |
| `COOKER_PORT` | `8080` | HTTP listen port |
| `COOKER_DATABASE_URL` | — | Postgres connection string (required in production) |
| `COOKER_REDIS_URL` | — | Redis connection string (required for multi-replica) |
| `COOKER_SECRET_KEY` | — | Base64-encoded 32-byte AES key (required for `database` secrets backend in production) |
| `COOKER_BUILDER` | `kaniko` | `kaniko` · `buildkit` · `buildah` · `docker` |
| `COOKER_PUSHER` | `crane` | `crane` · `docker` (docker refused in production) |
| `COOKER_SECRETS_BACKEND` | `database` | `database` · `keepsave` · `vault` · `aws` · `gcp` |
| `COOKER_RATE_LIMIT_BACKEND` | `memory` | `memory` · `redis` |
| `COOKER_WS_TICKET_BACKEND` | `memory` | `memory` · `redis` |
| `COOKER_WS_HUB_BACKEND` | `memory` | `memory` · `redis` |
| `COOKER_STICKY_SESSIONS` | `false` | Set `true` if your ingress pins clients to the same replica |
| `COOKER_JOBQUEUE_ENABLED` | `false` | Phase 1: spawn the durable job-queue worker pool and switch `RunPipeline` to async enqueue |
| `COOKER_JOBQUEUE_WORKERS` | `4` | Worker goroutine count (only when jobqueue enabled) |
| `COOKER_SCHEDULER_ENABLED` | `false` | Phase 2 F2: spawn the cron-triggered runs loop (requires `COOKER_JOBQUEUE_ENABLED=true`) |
| `COOKER_SCHEDULER_TICK` | `30s` | Scheduler tick interval; smaller = faster overdue catch, more queries |

<details>
<summary><b>Auth & OIDC variables</b></summary>

| Variable | Default | Description |
|----------|---------|-------------|
| `COOKER_OIDC_ENABLED` | `false` | Enable OIDC sign-in |
| `COOKER_OIDC_ISSUER_URL` | — | IdP base URL |
| `COOKER_OIDC_CLIENT_ID` | — | Your registered client ID |
| `COOKER_OIDC_CLIENT_SECRET` | — | Confidential client secret (Authorization Code flow) |
| `COOKER_OIDC_REDIRECT_URL` | — | Must match what's registered at the IdP |
| `COOKER_OIDC_ALLOWED_ORIGINS` | — | Comma-separated CORS origins |
| `COOKER_OIDC_GROUP_MAP` | — | CSV: `group:role` pairs (e.g. `cooker-admins:admin,cooker-deploy:operator`) |
| `COOKER_OIDC_MFA_ACR_VALUES` | — | Required `acr`/`amr` for destructive routes |

</details>

<details>
<summary><b>Observability variables</b></summary>

| Variable | Default | Description |
|----------|---------|-------------|
| `COOKER_METRICS_ENABLED` | `false` | Expose Prometheus `/metrics` |
| `COOKER_TRACING_ENABLED` | `false` | Enable OTLP traces |
| `COOKER_OTLP_ENDPOINT` | — | OTLP gRPC endpoint (e.g. `otel-collector:4317`) |
| `COOKER_OTLP_INSECURE` | `false` | Disable TLS for in-cluster OTLP |
| `COOKER_SERVICE_NAME` | `cooker` | Service name for traces |
| `COOKER_AUDIT_DESTINATION` | `stdout` | `stdout` · `file` |
| `COOKER_AUDIT_FILE_PATH` | — | Required if destination is `file` |

</details>

<details>
<summary><b>Resource limits & timeouts</b></summary>

| Variable | Default | Description |
|----------|---------|-------------|
| `COOKER_ORPHAN_SWEEP_INTERVAL` | `60s` | How often to reap stale runs |
| `COOKER_HEARTBEAT_INTERVAL` | `30s` | Run-coordinator heartbeat |
| `COOKER_MAX_FANOUT` | `8` | Bounded DAG concurrency |
| `COOKER_RUN_DEADLINE` | `30m` | Global timeout per pipeline run |
| `COOKER_SHUTDOWN_TIMEOUT` | `30s` | SIGTERM drain window |

</details>

📖 **Full reference:** [docs/user-guide/reference/env-vars.md](docs/user-guide/reference/env-vars.md)

### Secrets backends

Pick one at boot. The API and on-the-wire format are identical across backends.

<details>
<summary><b><code>database</code> — default, simple</b></summary>

```bash
COOKER_SECRETS_BACKEND=database
COOKER_SECRET_KEY=$(head -c 32 /dev/urandom | base64)
```

Stored in `environments.secrets` JSONB column, encrypted with AES-GCM. With no key set in production, the secret API returns `503` so misconfiguration is loud.

</details>

<details>
<summary><b><code>keepsave</code> — multi-tenant, audit-heavy</b></summary>

```bash
COOKER_SECRETS_BACKEND=keepsave
COOKER_SECRETS_KEEPSAVE_URL=http://keepsave:8080
COOKER_SECRETS_KEEPSAVE_PROJECT_ID=<cooker-project-uuid>
COOKER_SECRETS_KEEPSAVE_API_KEY=ks_xxxx
```

`COOKER_SECRET_KEY` not required — KeepSave handles encryption.

</details>

<details>
<summary><b><code>vault</code> — HashiCorp Vault</b></summary>

```bash
COOKER_SECRETS_BACKEND=vault
COOKER_SECRETS_VAULT_ADDR=https://vault.example.com:8200
COOKER_SECRETS_VAULT_MOUNT=secret
COOKER_SECRETS_VAULT_PREFIX=cooker
COOKER_SECRETS_VAULT_TOKEN=$(cat /vault/secrets/token)
```

Each environment maps to one Vault secret at `<mount>/data/<prefix>/<envID>`. Vault Agent injector compatible (leave `_TOKEN` empty).

</details>

<details>
<summary><b><code>aws</code> — AWS Secrets Manager</b></summary>

```bash
COOKER_SECRETS_BACKEND=aws
COOKER_SECRETS_AWS_REGION=us-east-1
COOKER_SECRETS_AWS_PREFIX=cooker
```

Auth via the standard AWS chain (IRSA on EKS, instance profile on EC2). One AWS secret per Cooker key for clean per-key versioning and IAM scoping.

</details>

<details>
<summary><b><code>gcp</code> — GCP Secret Manager</b></summary>

```bash
COOKER_SECRETS_BACKEND=gcp
COOKER_SECRETS_GCP_PROJECT_ID=my-gcp-project
COOKER_SECRETS_GCP_PREFIX=cooker
```

Auth via Application Default Credentials (Workload Identity on GKE).

</details>

**Switching backends:** secrets do not auto-migrate. Plan a one-shot copy step (read from old, write to new) before flipping the env var. There is no live dual-write.

📖 **Full secrets guide:** [docs/user-guide/guides/secrets.md](docs/user-guide/guides/secrets.md)
📖 **Design rationale:** [ADR-0002](docs/adr/0002-secrets-manager.md)

### Multi-replica setup

Three internal state pieces need shared storage to survive replica scaling. Either:

**Option A — Sticky sessions** (simpler, fine for typical workloads):

```yaml
# ingress annotations (NGINX example)
nginx.ingress.kubernetes.io/affinity: cookie
nginx.ingress.kubernetes.io/session-cookie-name: cooker-session
nginx.ingress.kubernetes.io/session-cookie-max-age: "172800"
```

Plus `COOKER_STICKY_SESSIONS=true` so `Config.Validate` permits multi-replica boots with memory backends.

**Option B — Redis-backed** (proper HA, chart default):

```bash
COOKER_RATE_LIMIT_BACKEND=redis
COOKER_WS_TICKET_BACKEND=redis
COOKER_WS_HUB_BACKEND=redis
COOKER_REDIS_URL=redis://cooker-redis:6379
```

📖 **Snippets for NGINX / ALB / Traefik / HAProxy / Envoy:** [docs/MULTI_REPLICA.md](docs/guides/MULTI_REPLICA.md)

## 🛠️ Builders & Deploy Targets

### Builders

```bash
# In production, set in Helm values or as env var
COOKER_BUILDER=kaniko  # default
```

| Builder | Pros | Cons |
|---------|------|------|
| **Kaniko** | No daemon, no socket. Runs as a Kubernetes Job per build. | Slower cold builds than BuildKit |
| **BuildKit** | Fastest. Supports cache mounts, multi-platform out of the box. | Requires an external buildkitd |
| **Buildah** | Rootless, daemonless, full Dockerfile parity. | Slower than BuildKit |
| **Docker** | Familiar. | **Refused at boot in production** — requires host socket, root-equivalent access |

### Deploy targets

Configure only the ones you need; unconfigured ones are skipped at boot.

<details>
<summary><b>Kubernetes</b></summary>

Default — uses client-go with kubectl fallback. Configure via Helm `kubeconfig` value or auto-detected ServiceAccount.

</details>

<details>
<summary><b>AWS ECS / Fargate</b></summary>

```bash
COOKER_DEPLOY_ECS_REGION=us-east-1
COOKER_DEPLOY_ECS_CLUSTER=cooker-prod
COOKER_DEPLOY_ECS_EXECUTION_ROLE=arn:aws:iam::123456789012:role/ecsTaskExecutionRole
COOKER_DEPLOY_ECS_TASK_ROLE=arn:aws:iam::123456789012:role/myAppTaskRole
COOKER_DEPLOY_ECS_SUBNETS=subnet-abc,subnet-def
COOKER_DEPLOY_ECS_SECURITY_GROUPS=sg-12345
```

Auth via standard AWS chain (IRSA on EKS, instance profile, env vars).

</details>

<details>
<summary><b>Google Cloud Run</b></summary>

```bash
COOKER_DEPLOY_CLOUDRUN_PROJECT=my-gcp-project
COOKER_DEPLOY_CLOUDRUN_REGION=us-central1
```

Auth via Application Default Credentials. Supports traffic-split rollback (route 0% to canary on failure).

</details>

<details>
<summary><b>Fly.io Machines</b></summary>

```bash
COOKER_DEPLOY_FLY_TOKEN=fly_xxxxxxxxxxxx
COOKER_DEPLOY_FLY_REGION=iad  # optional
```

Auto-creates the Fly app on first deploy. Updates machines in place; rollback restarts the previous machine config.

</details>

<details>
<summary><b>Render</b></summary>

```bash
COOKER_DEPLOY_RENDER_TOKEN=rnd_xxxxxxxxxxxx
COOKER_DEPLOY_RENDER_OWNER_ID=tea-xxx  # optional
```

Operator pre-creates the Render service; Cooker triggers deploys on it.

</details>

<details>
<summary><b>SSH remote (Dokploy / Coolify model)</b></summary>

No agent on the box, no Kubernetes, no cloud API — Cooker SSH'es into a registered host with the host's private key and runs `docker pull` + `docker run -d --restart=always` over the wire. Register the host through the **Hosts** page (kind `ssh-docker`), paste the PEM private key once (stored encrypted via the secrets manager, never echoed back), and point an App's deploy target at it.

Host-key verification is **mandatory** — Cooker pins the server's public key on first connect (TOFU) and refuses on key change. Production-mode boot refuses to serve if any registered SSH host has `sshStrictHostKey=false`.

```yaml
# App deploy target — pick "ssh" kind, point at a registered host
deployTarget:
  kind: ssh
  hostId: <host-uuid>
```

</details>

## 🌐 API Reference

Base path: `/api/v1`. All endpoints return JSON. Auth via `Authorization: Bearer <token>` (OIDC access token).

### Common endpoints

<details>
<summary><b>Pipelines</b></summary>

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/pipelines` | List pipelines |
| `POST` | `/pipelines` | Create pipeline |
| `GET` | `/pipelines/:id` | Get pipeline detail |
| `PUT` | `/pipelines/:id` | Update pipeline |
| `DELETE` | `/pipelines/:id` | Delete pipeline (admin, MFA) |
| `POST` | `/pipelines/:id/run` | Trigger a new run |
| `POST` | `/pipelines/:id/validate` | Validate DAG without running |
| `GET` | `/pipelines/:id/runs` | List runs for this pipeline |

</details>

<details>
<summary><b>Runs</b></summary>

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/runs` | List runs (paginated, sorted by createdAt DESC) |
| `GET` | `/runs/:id` | Get run detail (stages, status, logs reference) |
| `POST` | `/runs/:id/cancel` | Cancel a running run |

</details>

<details>
<summary><b>Apps</b></summary>

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/apps` | List apps |
| `POST` | `/apps` | Create app (clone-build-push-deploy from a repo) |
| `GET` | `/apps/:id` | Get app detail (deployed URL, last run, health) |
| `POST` | `/apps/:id/deploy` | Trigger a deploy |
| `POST` | `/apps/:id/webhook/rotate` | Rotate webhook secret (admin, MFA) |
| `POST` | `/webhooks/apps/:id` | GitHub webhook receiver (HMAC-verified) |

</details>

<details>
<summary><b>Environments</b></summary>

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/environments` | List environments |
| `POST` | `/environments` | Create environment |
| `GET` | `/environments/:id` | Get detail with secrets (admin only, MFA) |
| `POST` | `/environments/:id/promote` | Promote secrets to another env |
| `POST` | `/environments/:id/secrets` | Set secret (admin, MFA) |
| `DELETE` | `/environments/:id/secrets/:key` | Delete secret (admin, MFA) |

</details>

<details>
<summary><b>WebSocket</b></summary>

| Stream | Purpose |
|--------|---------|
| `wss://.../ws/runs/:id` | Live run status + stage progress |
| `wss://.../ws/stage-logs/:runId/:stageId` | Per-stage log lines |
| `wss://.../ws/k8s/watch` | K8s event watch |

Auth: get a 60-second ticket via `POST /api/v1/ws-tickets`, then connect with `?ticket=<value>`.

</details>

<details>
<summary><b>Health & metadata</b></summary>

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health/live` | Liveness probe |
| `GET` | `/health/ready` | Readiness (DB + Redis + JWKS check) |
| `GET` | `/metrics` | Prometheus metrics (if enabled) |
| `GET` | `/api/v1/version` | `{version, commit, date}` from ldflags |

</details>

📖 **OpenAPI spec:** [docs/openapi.yaml](docs/openapi.yaml) · [generated Swagger](backend/docs/api/swagger.yaml) (regenerate with `make swagger`)

## 🛡️ OCI Compliance

Cooker is built end-to-end on the three Open Container Initiative specifications:

| Specification | Version | Used for |
|---------------|---------|----------|
| [**image-spec**](https://github.com/opencontainers/image-spec) | v1.1 | Build output = OCI Manifest + Image Index. Multi-arch builds. Images tracked by `{mediaType, digest, size}` descriptors. |
| [**runtime-spec**](https://github.com/opencontainers/runtime-spec) | v1.2 | Container runtime semantics (mounts, env, cgroups, namespaces) surfaced in the UI. Docker Engine handles actual `config.json`. |
| [**distribution-spec**](https://github.com/opencontainers/distribution-spec) | v1.1 | Registry operations: `/v2/<name>/tags/list`, `/v2/<name>/manifests/<ref>`, `/v2/<name>/referrers/<digest>`. Referrers API for supply chain metadata. |

**Key libraries:** `github.com/opencontainers/image-spec/specs-go/v1`, `github.com/google/go-containerregistry`, `github.com/docker/docker/client`, `k8s.io/client-go`

The pusher path is continuously verified against the [upstream OCI conformance suite](https://github.com/opencontainers/distribution-spec/tree/main/conformance) via [`.github/workflows/oci-conformance.yml`](.github/workflows/oci-conformance.yml).

## 🗺️ Project Structure

```
Cooker/
├── backend/                  Go API server
│   ├── cmd/cooker/           Entry point: slog setup, signal handling, server.RunContext
│   ├── internal/
│   │   ├── server/           HTTP server · WebSocket hub · ticket store · rate limiter
│   │   ├── handler/          HTTP handlers (thin layer)
│   │   ├── service/          Business logic (executor, promoter, app deployer)
│   │   ├── store/            Postgres + memory store implementations
│   │   ├── auth/             OIDC, RBAC, MFA middleware
│   │   ├── audit/            Per-route audit log sink (stdout / file)
│   │   ├── observability/    Prometheus /metrics + OpenTelemetry OTLP/gRPC
│   │   ├── secrets/          Pluggable backends (database, keepsave, vault, awsm, gcpsm)
│   │   ├── builder/          Image builder adapters (docker, kaniko, buildah, buildkit)
│   │   ├── pusher/           Image push strategy + crane adapter
│   │   ├── deployer/         Kubernetes deployer (client-go + kubectl)
│   │   ├── deploytarget/     Cloud Run / ECS / Fly / Render adapters
│   │   ├── gitops/           GitOpsCommit (go-git/v5)
│   │   ├── transport/        Optional transports (tsnet, build-tagged)
│   │   ├── crypto/           AES-GCM codec for app webhook secrets
│   │   ├── retry/            Bounded retry helpers
│   │   ├── idempotency/      Run-launch dedupe + pg_advisory_lock
│   │   ├── jobqueue/         Phase 1 / A1: durable Postgres job queue + workers + NOTIFY
│   │   ├── runstate/         Phase 1 / A2: run + stage FSM with typed invalid-transition errors
│   │   ├── notifier/         Phase 2 / F1: Slack/Discord/Email/Webhook dispatcher
│   │   ├── scheduler/        Phase 2 / F2: leader-elected cron-triggered runs
│   │   ├── templates/        Phase 2 / F4: pipeline template catalog
│   │   ├── buildplan/        Clone→Build→Push→Deploy run synthesis
│   │   ├── source/           Repo clone helpers + Phase 2 / F3 webhook parsers (github, gitlab, bitbucket, gitea)
│   │   ├── validate/         Cross-cutting validation
│   │   ├── model/            Domain types
│   │   ├── oci/              OCI image-spec types, media types, validation
│   │   ├── config/           Env-var loading + production Validate()
│   │   └── store/postgres/   Migrations + per-store impl
│   └── pkg/
│       ├── dagrunner/        Reusable DAG execution engine
│       └── ociutil/          OCI descriptor utilities
├── frontend/                 React + TypeScript + Vite
│   └── src/
│       ├── pages/            Route components (PipelinesPage, RunPage, AppDetailPage, ...)
│       ├── components/       React Flow nodes/edges/panels, EmptyState, LogsPanel, ...
│       ├── stores/           Zustand stores (pipeline, docker, k8s, environment)
│       ├── api/              Typed API client per domain
│       ├── hooks/            useWebSocket, useStageLogs, useK8sWatch
│       ├── auth/             OIDC provider + protected routes
│       └── types/            Pipeline / Docker / K8s / OCI type definitions
├── deploy/
│   ├── docker/Dockerfile     Multi-stage: frontend build → backend build → Alpine runtime
│   ├── helm/cooker/          Helm chart (values.yaml + templates)
│   └── kubernetes/           Raw manifests (parity with Helm chart for non-Helm users)
├── docs/                     All documentation
│   ├── user-guide/           For end users
│   ├── adr/                  Architecture decision records
│   ├── audits/               Internal audit reports
│   ├── architecture.md       Full architecture document
│   ├── design.md             Design patterns, conventions, contributor checklist
│   ├── ROLLOUT.md            UAT → production cutover playbook
│   ├── RUNBOOK.md            Incident response
│   ├── MULTI_REPLICA.md      Sticky sessions + Redis-shared-state guide
│   ├── RELEASING.md          Release cutting playbook
│   └── SECURITY-RELEASE-VERIFY.md   Publish-time verification checklist
├── .github/workflows/        CI: ci.yml, release.yml, oci-conformance.yml, cooker-weekly.yml
├── Makefile                  Build orchestration
├── docker-compose.yml        Local development stack
├── CHANGELOG.md              Keep a Changelog format
├── SECURITY.md               Security policy + production hardening checklist
├── backlog.md                Open work + closed log
└── README.md                 This file
```

## 💻 Development

### Prerequisites

- **Go** 1.22+ (check: `go version`)
- **Node.js** 20+ and npm
- **Docker** + Docker Compose
- **PostgreSQL** 14+ (or use the compose stack)
- **golangci-lint** (optional for local linting)

### Build from source

```bash
git clone https://github.com/santapong/Cooker.git
cd Cooker

# Backend
cd backend
go build -o bin/cooker ./cmd/cooker
./bin/cooker --version

# Frontend
cd ../frontend
npm install
npm run dev          # Vite dev server with HMR
npm run build        # Production bundle to dist/
npm run test         # Vitest
npm run lint         # ESLint
```

### Make targets

```bash
make help              # Show all targets
make build             # Build the single binary
make test              # Run all tests (backend + frontend)
make test-race         # Backend tests with race detector
make uat-up            # Start UAT compose stack
make uat-down          # Stop UAT stack
make test-e2e          # End-to-end smoke
make docker-build      # Build Docker image
make swagger           # Regenerate OpenAPI from swag annotations
make release-snapshot  # Run goreleaser snapshot (no push)
make clean             # Remove build artifacts
```

### Testing

```bash
# Backend — race detector on by default in CI
cd backend && go test ./... -race -count=1

# Frontend
cd frontend && npm test

# Integration — boots UAT and asserts pipeline success
make test-e2e
```

📖 **Contributor checklist:** [docs/design.md](docs/reference/design.md#11-adding-a-new-feature)

## 📚 Documentation

### For users

| Document | Description |
|----------|-------------|
| [User Guide Index](docs/user-guide/index.md) | Landing page — concepts, getting started, guides |
| [Getting Started](docs/user-guide/getting-started/) | Quickstart · Helm install · upgrading |
| [Concepts](docs/user-guide/concepts/) | Pipelines · Apps · Stages · Runs · Environments · Targets |
| [Guides](docs/user-guide/guides/) | First pipeline · K8s deploy · registries · secrets · promotions · GitHub webhooks |
| [Reference](docs/user-guide/reference/) | Full API · CLI · env vars · webhook payloads |

### For operators

| Document | Description |
|----------|-------------|
| [Rollout Playbook](docs/guides/ROLLOUT.md) | UAT → production cutover (single source of truth) |
| [Runbook](docs/guides/RUNBOOK.md) | Incident response — symptom → checks → cause → mitigation |
| [Multi-Replica Guide](docs/guides/MULTI_REPLICA.md) | Sticky sessions + Redis-shared-state |
| [UAT Runbook](docs/guides/UAT.md) | How to enable OIDC for testers |
| [Security Policy](SECURITY.md) | Auth architecture · production hardening checklist |
| [Backlog](backlog.md) | Open work · effort estimates · readiness matrix |

### For contributors

| Document | Description |
|----------|-------------|
| [Architecture](docs/reference/architecture.md) | System architecture · component map · data flow · OCI integration |
| [Architecture — Phase 1 + Phase 2](docs/reference/architecture-phase1-phase2.md) | New subsystems: jobqueue, runstate FSM, permission middleware, notifier, scheduler |
| [Adapted from Dokploy](docs/proposals/adapted-from-dokploy.md) | What was adapted, what was skipped, what was deferred — with paths into both codebases |
| [Design Patterns](docs/reference/design.md) | Layering · error wrapping · test strategy · contributor checklist (§11) |
| [ADRs](docs/adr/) | Architecture decision records |
| [Roadmap 2026](docs/proposals/roadmap-2026.md) | Strategic themes for the year |
| [DAG Adaptation Plan](docs/proposals/dag-adaptation-2026.md) | 20-week primitives roadmap |
| [Protocols](docs/reference/protocols.md) | CKR-LOG/1 (binary log stream) + CKR-DSL (pipeline DSL) |
| [CHANGELOG](CHANGELOG.md) | Keep a Changelog format |

### For release engineers

| Document | Description |
|----------|-------------|
| [Release Playbook](docs/guides/RELEASING.md) | Tag → workflow → verify |
| [Publish Verification](docs/guides/SECURITY-RELEASE-VERIFY.md) | Cosign + permissions + non-root + port surface |

## 📊 Observability

Both metrics and traces are **off by default** — turn them on per deployment via env / Helm values.

### Prometheus metrics

```bash
COOKER_METRICS_ENABLED=true
# /metrics is now served on the same port as the API
```

Key metrics:

| Metric | Type | What |
|--------|------|------|
| `cooker_http_requests_total{method,route,status}` | Counter | Request count by route template |
| `cooker_http_request_duration_seconds{method,route}` | Histogram | Latency distribution |
| `cooker_db_connection_errors_total` | Counter | Postgres connection failures |
| `cooker_redis_connection_errors_total` | Counter | Redis connection failures |
| `cooker_jwks_fetch_failures_total` | Counter | OIDC JWKS fetch failures |
| `cooker_pipeline_runs_orphaned_total` | Counter | Stale runs reaped by orphan sweep |

📖 **Recommended Alertmanager rules:** [docs/RUNBOOK.md](docs/guides/RUNBOOK.md#alertmanager-rules)

### OpenTelemetry traces

```bash
COOKER_TRACING_ENABLED=true
COOKER_OTLP_ENDPOINT=otel-collector.observability.svc.cluster.local:4317
COOKER_OTLP_INSECURE=true  # in-cluster OTLP rarely uses TLS
COOKER_SERVICE_NAME=cooker
COOKER_SERVICE_VERSION=v0.1.0
```

### Structured logs

Every line is a JSON object via `log/slog`:

```json
{"time":"2026-05-13T12:34:56Z","level":"INFO","msg":"stage finished","pipeline":"abc-123","stage":"build","status":"success","duration_ms":42351}
```

## 🔒 Security

- **Non-root container** — UID 65532
- **No `docker.sock` mount** in raw K8s manifests
- **`Config.Validate`** refuses unsafe boots in production
- **OIDC + PKCE** — no client secret in browser
- **Step-up MFA** on destructive admin routes
- **Per-route audit log** — on by default in production
- **AES-GCM** encryption for env secrets at rest
- **cosign keyless signing** of release artifacts
- **NetworkPolicy** in Helm chart (gated by values)
- **`securityContext`** with `readOnlyRootFilesystem: true`

📖 **Full security policy:** [SECURITY.md](SECURITY.md)
📖 **Responsible disclosure:** [SECURITY.md#reporting-a-vulnerability](SECURITY.md#reporting-a-vulnerability)

## 🩺 Troubleshooting

<details>
<summary><b>Pipeline runs forever without finishing</b></summary>

- Check `/health/ready` — DB / Redis / JWKS issues will surface there
- Inspect the run's stage list via `GET /api/v1/runs/:id` — look for a stage stuck in `Running`
- Check the orphan sweep metric `cooker_pipeline_runs_orphaned_total` — if growing, your `COOKER_HEARTBEAT_INTERVAL` may be too short for your cluster
- Pod may be OOM-killed; orphan sweep reaps after `COOKER_ORPHAN_SWEEP_INTERVAL`

</details>

<details>
<summary><b>OIDC sign-in redirects to error page</b></summary>

- Verify your IdP redirect URL matches `COOKER_OIDC_REDIRECT_URL` exactly (including scheme + port + path)
- Most IdPs require HTTPS — check your ingress TLS config
- Check `cooker_jwks_fetch_failures_total` — non-zero means your IdP discovery endpoint is unreachable
- Server-side detail is logged at `slog.Warn` / `slog.Error` — `kubectl logs` will show the cause

</details>

<details>
<summary><b>WebSocket connects then disconnects immediately</b></summary>

- Tickets are single-use and expire after 60 s — make sure your client fetches a fresh one before each connect
- Check your ingress allows WebSocket upgrades (some ALB configs strip the Upgrade header)
- Multi-replica without sticky sessions or Redis WS hub will lose the ticket across replicas

</details>

<details>
<summary><b>Build hangs at "pushing image"</b></summary>

- Check that your registry credentials are configured for the registry you're pushing to
- Verify the registry isn't blocking your egress (some private registries require allow-listed IPs)
- For Kaniko: the in-cluster Job needs a registry pull secret on its ServiceAccount

</details>

<details>
<summary><b>Container won't start: <code>Config.Validate</code> error</b></summary>

Production mode (`COOKER_ENV=production`) runs strict validation at boot. Check the error message — it names the exact env var that's misconfigured. Common ones:

- `BUILDER=docker` refused in production (use kaniko/buildkit/buildah)
- Multi-replica without `COOKER_STICKY_SESSIONS=true` or Redis backends
- Missing `COOKER_SECRET_KEY` with `database` secrets backend
- Non-localhost Postgres without `sslmode>=require`
- OIDC enabled with non-HTTPS redirect URL

</details>

📖 **Full troubleshooting guide:** [docs/user-guide/operations/troubleshooting.md](docs/user-guide/operations/troubleshooting.md)
📖 **Incident response:** [docs/RUNBOOK.md](docs/guides/RUNBOOK.md)

## ❓ FAQ

<details>
<summary><b>Why another CI/CD tool?</b></summary>

Most CI/CD tools optimize for either (a) the build half (Drone, Buildkite) or (b) the deploy half (Argo CD, Flux). The handoff between them is usually a separate orchestrator. Cooker treats build, push, and deploy as stages in the same DAG — with a visual editor — so you don't need three tools and a glue script.

</details>

<details>
<summary><b>Does Cooker replace Jenkins?</b></summary>

For most container-based workflows, yes. Cooker covers what 90% of Jenkins pipelines actually do (build → test → push → deploy). What Cooker doesn't cover yet: non-container builds (Maven JARs going to Nexus, etc.), library plugins, and the deeply customizable Jenkins ecosystem. If your pipeline is Java-on-bare-metal with heavy plugin reliance, Jenkins is still the better fit today.

</details>

<details>
<summary><b>Can I use Cooker without Kubernetes?</b></summary>

Yes. Cooker runs as a single binary on any host with Docker installed. Deploy targets include Cloud Run, ECS, Fly.io, and Render — none of which require you to operate Kubernetes yourself. You only need Kubernetes if you're deploying *to* Kubernetes.

</details>

<details>
<summary><b>How do I migrate from Jenkins / Drone / GitHub Actions?</b></summary>

Pipeline YAML import is on the roadmap (D6 in [`backlog.md`](backlog.md) — Drone first, then GitHub Actions). For now, the visual editor + GitHub webhook trigger is the fastest path: point Cooker at your repo, draw the pipeline, switch your repo's webhook.

</details>

<details>
<summary><b>Is Cooker production-ready?</b></summary>

Yes for single-replica and multi-replica (Redis-backed) shapes. `Config.Validate` enforces the rest — see the deployment-shape readiness matrix in [`backlog.md`](backlog.md#production-readiness-summary). The honest verdict and any open caveats live there.

</details>

<details>
<summary><b>Does Cooker support pipeline-as-code?</b></summary>

Today the canonical representation is the visual DAG (stored as JSONB). A custom YAML DSL (CKR-DSL) is on the roadmap — see [`docs/protocols.md`](docs/reference/protocols.md) for the proposal.

</details>

<details>
<summary><b>How does Cooker handle secrets in pipelines?</b></summary>

Per-environment scoping. Each environment has its own secret namespace. Secrets are injected as env vars into build/test/deploy stages on a per-stage basis (you opt in per stage to avoid accidental leakage). Five pluggable backends — see [Secrets backends](#secrets-backends).

</details>

<details>
<summary><b>Can I self-host Cooker on a single VPS?</b></summary>

Yes. The `docker-compose.yml` in the repo runs everything on one box: backend + frontend + Postgres + Redis + an internal Docker daemon for builds. Suitable for solo developers and small teams.

</details>

<details>
<summary><b>What's the difference between an App and a Pipeline?</b></summary>

A **Pipeline** is a user-authored DAG of stages. An **App** is a higher-level shortcut: point at a GitHub repo, pick a deploy target, click Deploy. Cooker synthesises a Clone → Build → Push → Deploy run under the hood. Apps also have webhooks for auto-deploy on push.

</details>

## 🤝 Contributing

Contributions are welcome — bug reports, feature requests, documentation improvements, and pull requests. See **[CONTRIBUTING.md](CONTRIBUTING.md)** for the full guide (dev setup, layering rules, PR process, and the contributor licence agreement).

### Quick start for contributors

```bash
git clone https://github.com/santapong/Cooker.git
cd Cooker
make uat-up                  # Spin up the dev stack
git checkout -b feature/my-feature
# ... make changes ...
make test                    # Run tests
git commit -m "feat: my feature"
git push -u origin feature/my-feature
# Then open a PR on GitHub
```

### Before larger changes

1. **Read [docs/design.md](docs/reference/design.md)** — it covers the layering rules, error wrapping, test patterns, and the "adding a feature" checklist (§11)
2. **Open an issue** to discuss approach before writing significant code
3. **Follow the layering rules**: handler → service → store; no business logic in handlers, no HTTP types in services
4. **Add tests** — race detector is on in CI; every non-trivial change needs `*_test.go`

### Reporting security issues

Found a security issue? Please follow the [responsible disclosure policy](SECURITY.md) rather than filing a public issue.

## 🚧 What's not done yet

Honest up front, so you can decide (this is a young project with a bus factor of one):

- **Single-tenant.** Every authenticated user can see every pipeline, app, and environment. Multi-tenancy is on the roadmap; we don't pretend it ships today.
- **No in-product audit-log viewer** yet — the audit middleware writes structured JSON; query it from your log stack.
- **No first-class PR-preview environments** — assemble them manually in the pipeline editor for now.
- **Builder choice matters:** the `docker` builder mounts the host socket and is **dev-only**. Kaniko (default), BuildKit, or Buildah are the production options.
- **OIDC, not SAML.** Linux/macOS server (Docker Desktop works on Mac); not Windows-native.

If any of these is a blocker for you, wait. If not, `docker compose up` takes about 30 seconds.

## 🗺️ Roadmap

### ✅ Recently shipped — Phase 1 + Phase 2 (May 2026 W6)

| Theme | What |
|-------|------|
| **Durable async execution** | Postgres-backed job queue + `NOTIFY` wake-ups + `FOR UPDATE SKIP LOCKED` pickup + per-pipeline concurrency keys + exp. backoff |
| **Run state machine** | Formal FSM with typed `ErrInvalidTransition`; state alphabet pinned to `model.RunStatus` |
| **Permission middleware** | Resource × action matrix + `RequirePermission` Gin middleware (defense-in-depth alongside roles + MFA) |
| **Notifications** | Slack / Discord / Email / Webhook fan-out via `errors.Join`; per-target `SendTimeout` |
| **Cron triggers** | Leader-elected (`pg_advisory_lock`) scheduler with in-house POSIX cron parser; DST-correct IANA timezones |
| **Git providers** | GitLab + Bitbucket Server + Gitea webhook receivers alongside GitHub |
| **Templates v1** | Pipeline-template catalog + create-from-template with fresh stage IDs + DAG re-validation |

Full attribution + design rationale: [`docs/adapted-from-dokploy.md`](docs/proposals/adapted-from-dokploy.md)
Full Phase 1+2 architecture: [`docs/architecture-phase1-phase2.md`](docs/reference/architecture-phase1-phase2.md)
Ready-to-paste CHANGELOG entry: [`CHANGELOG.md`](CHANGELOG.md)

### 🚀 Upcoming

| Theme | What |
|-------|------|
| **Admin UI** | CRUD endpoints + frontend pages for templates / schedules / notification-targets |
| **DAG primitives** | Retry policies · conditional edges · fan-out matrix · cache plumbing · stage outputs |
| **Executor migration** | Switch `service/executor.go` status writes through `runstate.TransitionRun` (mechanical; primitive in place) |
| **More deploy targets** | Kamal · Cloud Run depth · HashiCorp Nomad |
| **Pipeline-as-code** | CKR-DSL parser · import from Drone / GitHub Actions YAML |
| **Marketplace** | Sharable pipeline templates · org-scoped catalog · frontend gallery |
| **AI assist** | Suggest stages · explain failures (local heuristics first, optional hosted LLM) |
| **Builder breadth** | Nixpacks · Railpack · Paketo · Heroku Buildpacks |

- 📋 **Active backlog** with effort estimates: [`backlog.md`](backlog.md)
- 🗓️ **Strategic plan:** [`docs/roadmap-2026.md`](docs/proposals/roadmap-2026.md)
- 🧱 **DAG primitives roadmap:** [`docs/dag-adaptation-2026.md`](docs/proposals/dag-adaptation-2026.md)
- 🤝 **PM brief + decisions:** [`docs/pm-brief-2026-05.md`](docs/proposals/pm-brief-2026-05.md)

## 💬 Community

- **Issues:** [GitHub Issues](https://github.com/santapong/Cooker/issues)
- **Discussions:** [GitHub Discussions](https://github.com/santapong/Cooker/discussions)
- **Security:** [Responsible disclosure](SECURITY.md)

## 🙏 Acknowledgments

Built on the shoulders of:

- [Gin](https://github.com/gin-gonic/gin) — HTTP framework
- [go-containerregistry](https://github.com/google/go-containerregistry) — OCI registry client
- [client-go](https://github.com/kubernetes/client-go) — Kubernetes client
- [React Flow](https://reactflow.dev/) — Visual DAG editor
- [Zustand](https://github.com/pmndrs/zustand) — Frontend state
- [oidc-client-ts](https://github.com/authts/oidc-client-ts) — Browser OIDC
- [coreos/go-oidc](https://github.com/coreos/go-oidc) — Backend OIDC verification
- [goreleaser](https://goreleaser.com/) — Release engineering
- [cosign](https://github.com/sigstore/cosign) — Supply chain signatures
- The [OCI](https://opencontainers.org/) community for the image / runtime / distribution specs

## 📄 License

Released under the [Apache License 2.0](LICENSE). See [`NOTICE`](NOTICE) for attribution.

---

<div align="center">

**CI/CD you can see.**

*Built with Go and TypeScript.*

</div>
