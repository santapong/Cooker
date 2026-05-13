<div align="center">

# Cooker

**A web-based CI/CD platform with a visual DAG editor for building, pushing, and deploying OCI images to Kubernetes and cloud runtimes.**

[![Go Version](https://img.shields.io/badge/go-1.22+-00ADD8?logo=go&logoColor=white)](https://golang.org/dl/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![CI](https://github.com/santapong/Cooker/actions/workflows/ci.yml/badge.svg)](https://github.com/santapong/Cooker/actions/workflows/ci.yml)
[![OCI Conformance](https://github.com/santapong/Cooker/actions/workflows/oci-conformance.yml/badge.svg)](https://github.com/santapong/Cooker/actions/workflows/oci-conformance.yml)
[![GHCR](https://img.shields.io/badge/container-ghcr.io%2Fsantapong%2Fcooker-2496ED?logo=docker&logoColor=white)](https://github.com/santapong/Cooker/pkgs/container/cooker)
[![Helm](https://img.shields.io/badge/helm-oci%3A%2F%2Fghcr.io%2Fsantapong%2Fcharts%2Fcooker-0F1689?logo=helm&logoColor=white)](https://github.com/santapong/Cooker/pkgs/container/charts%2Fcooker)

[**Quick Start**](#-quick-start) • [**Documentation**](#-documentation) • [**Architecture**](#-architecture) • [**Contributing**](#-contributing)

</div>

---

## Overview

Cooker is a single-binary CI/CD platform that lets teams design pipelines visually and run them against real container infrastructure. Drag stages onto a canvas, wire them into a DAG, and Cooker handles building images, pushing them to OCI registries, and rolling them out across Dev / Staging / Production environments.

Unlike Jenkins (XML / pipeline scripts) or Argo CD (GitOps only), Cooker gives you a single pane of glass for the whole build-to-deploy lifecycle — with live build logs, approval gates, and a real audit trail.

> **Status:** production-ready on single-replica and multi-replica (Redis-backed) deployments. `Config.Validate` refuses unsafe boots in production. See [the rollout playbook](docs/ROLLOUT.md).

## ✨ Features

### Pipeline authoring

- **Visual DAG editor** powered by React Flow — drag, drop, connect stages
- **Six stage types**: Build, Test, Push, Deploy, Approval, Custom
- **Simple ⇄ Pro toggle** — beginners get guard rails, experts get raw access
- **Apps** abstraction — point at a GitHub repo, pick a deploy target, ship in one click

### Execution

- **Live WebSocket-streamed build logs** for every stage
- **Configurable retry, fan-out limits, and run deadlines** per pipeline
- **Auto-promotion or manual approval** gates between environments
- **GitHub webhook triggers** with per-app secrets
- **GitOps mode** — write rendered manifests back to a git repo (`go-git/v5`)

### Builders & registries

- **Four builders**: Kaniko (default), BuildKit, Buildah, Docker (dev only)
- **Crane-based push** via `go-containerregistry` — exercised against the upstream [OCI distribution-spec conformance suite](https://github.com/opencontainers/distribution-spec) in CI
- **Full OCI compliance**: image-spec v1.1, runtime-spec v1.2, distribution-spec v1.1

### Deploy targets

- **Kubernetes** (client-go + kubectl fallback)
- **AWS ECS / Fargate**
- **Google Cloud Run** with traffic-split rollback
- **Fly.io** Machines
- **Render**

### Authentication & authorization

- **OIDC / OAuth 2.0 with PKCE** — Keycloak, Okta, Azure AD, Google, GitHub
- **Four-role RBAC**: admin / operator / approver / viewer
- **Configurable group-to-role mapping** via `COOKER_OIDC_GROUP_MAP`
- **Step-up MFA** on destructive admin routes (opt-in)

### Secrets

Five pluggable backends, selectable at boot via `COOKER_SECRETS_BACKEND`:

| Backend | Best for |
|---------|----------|
| `database` | Simple single-Cooker installs (AES-GCM in Postgres) |
| `keepsave` | Multi-tenant or audit-heavy environments |
| `vault` | Teams with existing HashiCorp Vault |
| `aws` | AWS-native (EKS / ECS / Lambda) deployments |
| `gcp` | GCP-native (GKE / Cloud Run) deployments |

### Observability

- **Prometheus `/metrics`** with request counters, duration histograms, and four resilience metrics
- **OpenTelemetry / OTLP traces** for distributed request flows
- **Structured JSON logs** via `log/slog`
- **Per-route audit log** with configurable destination (stdout or file)
- **Split `/health/live` + `/health/ready`** with per-check breakdown

### Multi-replica & HA

- **Redis-backed shared state** (rate limiter, WebSocket tickets, broadcast hub) for horizontal scaling
- **Graceful 30 s shutdown** drains in-flight runs on SIGTERM
- **Orphan sweep** reaps stale runs after OOM kills
- **Helm chart** with `NetworkPolicy`, `securityContext`, sticky-session helpers

## 🚀 Quick Start

### Local development

```bash
git clone https://github.com/santapong/Cooker.git
cd Cooker
docker compose up
```

Then open:

- **Frontend** — http://localhost:5173
- **Backend API** — http://localhost:8080/api/v1

### Try the full stack (UAT mode)

```bash
make uat-up                  # Single binary serving the SPA on :8080
make uat-up-with-keycloak    # Same + Keycloak (alice/admin, bob/viewer)
make test-e2e                # End-to-end smoke: boots UAT, runs one pipeline
```

### Production install (Helm)

```bash
# 1. Provision your OIDC client secret and a 32-byte secret key
kubectl create secret generic cooker-oidc \
  --from-literal=client-secret=<value-from-idp>
kubectl create secret generic cooker-secret-key \
  --from-literal=key=$(head -c 32 /dev/urandom | base64)

# 2. Install via Helm OCI chart
helm install cooker oci://ghcr.io/santapong/charts/cooker \
  --version 0.1.0 \
  --set cookerEnv=production \
  --set oidc.enabled=true \
  --set oidc.issuerUrl=https://auth.example.com \
  --set oidc.clientId=cooker \
  --set oidc.clientSecretRef.name=cooker-oidc \
  --set oidc.redirectUrl=https://cooker.example.com/callback \
  --set secretKey.existingSecret=cooker-secret-key \
  --set 'ingress.tls[0].secretName=cooker-tls' \
  --set 'ingress.tls[0].hosts[0]=cooker.example.com'
```

Full installation guide: [docs/user-guide/getting-started/helm-install.md](docs/user-guide/getting-started/helm-install.md)

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

| Layer | Stack |
|-------|-------|
| **Frontend** | React 18 + TypeScript + React Flow + Zustand + Vite |
| **Backend** | Go 1.22+ · Gin · Docker SDK · client-go · go-containerregistry |
| **Storage** | PostgreSQL 14+ · Redis 7+ |
| **Container** | Multi-stage Alpine, non-root UID 65532 |
| **Distribution** | GHCR multi-arch (amd64 + arm64) · Helm OCI chart · cosign-signed |

See [docs/architecture.md](docs/architecture.md) for the full architecture document and [docs/adr/](docs/adr/) for design decisions.

## 📚 Documentation

| For | Start here |
|-----|------------|
| **Users** | [User Guide](docs/user-guide/index.md) — quickstart, concepts, common workflows |
| **Operators** | [Rollout Playbook](docs/ROLLOUT.md) — UAT → production cutover · [Runbook](docs/RUNBOOK.md) — incident response · [Security](SECURITY.md) — hardening checklist |
| **Contributors** | [Architecture](docs/architecture.md) · [Design patterns](docs/design.md) · [ADRs](docs/adr/) |
| **API** | [OpenAPI spec](docs/openapi.yaml) · `make swagger` for generated docs |
| **Release engineers** | [Release Playbook](docs/RELEASING.md) · [Publish Verification Checklist](docs/SECURITY-RELEASE-VERIFY.md) |

## 🔧 Configuration

Cooker is configured via environment variables. Production-mode settings are validated at boot — misconfiguration fails loudly rather than silently.

Common variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `COOKER_ENV` | `dev` | `dev` · `uat` · `production` |
| `COOKER_BUILDER` | `kaniko` | `kaniko` · `buildkit` · `buildah` · `docker` |
| `COOKER_SECRETS_BACKEND` | `database` | `database` · `keepsave` · `vault` · `aws` · `gcp` |
| `COOKER_OIDC_ENABLED` | `false` | Enable OIDC sign-in (required in production) |
| `COOKER_METRICS_ENABLED` | `false` | Expose Prometheus `/metrics` |
| `COOKER_TRACING_ENABLED` | `false` | Send OTLP traces |
| `COOKER_RATE_LIMIT_BACKEND` | `memory` | `memory` · `redis` (for multi-replica) |

Full reference: [docs/user-guide/reference/env-vars.md](docs/user-guide/reference/env-vars.md)

## 🛡️ OCI Compliance

Cooker is built on the three Open Container Initiative specifications:

| Specification | Version | Used for |
|--------------|---------|----------|
| [image-spec](https://github.com/opencontainers/image-spec) | v1.1 | Manifest + Image Index for multi-arch builds |
| [runtime-spec](https://github.com/opencontainers/runtime-spec) | v1.2 | Container runtime semantics surfaced in the UI |
| [distribution-spec](https://github.com/opencontainers/distribution-spec) | v1.1 | Registry operations including the referrers API |

The registry/pusher path is continuously verified against the upstream OCI conformance suite via the [`oci-conformance.yml`](.github/workflows/oci-conformance.yml) workflow.

## 🗺️ Project Structure

```
.
├── backend/                  Go API server
│   ├── cmd/cooker/           Entry point
│   ├── internal/
│   │   ├── server/           HTTP server, WebSocket hub, run coordinator
│   │   ├── handler/          HTTP handlers (thin layer)
│   │   ├── service/          Business logic (executor, promoter, deployer)
│   │   ├── store/            Postgres + memory store implementations
│   │   ├── auth/             OIDC, RBAC, MFA middleware
│   │   ├── builder/          Image builder adapters
│   │   ├── pusher/           Registry push adapters
│   │   ├── deployer/         Kubernetes deployer adapters
│   │   ├── deploytarget/     Cloud Run / ECS / Fly / Render adapters
│   │   ├── secrets/          Pluggable secret backends
│   │   └── ...
│   └── pkg/dagrunner/        Reusable DAG execution engine
├── frontend/                 React + TypeScript + Vite
│   └── src/
│       ├── pages/            Route components
│       ├── components/       Reusable UI + DAG editor
│       ├── stores/           Zustand state
│       ├── api/              Typed API client
│       └── auth/             OIDC provider
├── deploy/
│   ├── docker/               Multi-stage Dockerfile
│   ├── helm/cooker/          Helm chart
│   └── kubernetes/           Raw manifests
└── docs/                     All documentation
```

## 🤝 Contributing

Contributions are welcome — bug reports, feature requests, and pull requests.

1. **Fork** the repository
2. **Create a branch** — `git checkout -b feature/my-feature`
3. **Make your changes** following the [contributor checklist](docs/design.md)
4. **Run tests** — `make test`
5. **Push** and open a pull request

Read the [design conventions](docs/design.md) before larger changes — they cover the handler → service → store layering, error wrapping, test patterns, and the "adding a feature" checklist.

Found a security issue? Please follow the [responsible disclosure policy](SECURITY.md) rather than filing a public issue.

## 📋 Roadmap

The strategic plan is in [docs/roadmap-2026.md](docs/roadmap-2026.md). Active work and effort estimates live in [backlog.md](backlog.md). The DAG-primitives roadmap (retry policies, conditional edges, fan-out matrix, cache plumbing, stage outputs) is in [docs/dag-adaptation-2026.md](docs/dag-adaptation-2026.md).

For changes between releases, see [CHANGELOG.md](CHANGELOG.md).

## 📄 License

Released under the [MIT License](LICENSE).

---

<div align="center">

**Built with Go and TypeScript**

</div>
