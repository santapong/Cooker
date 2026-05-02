# Cooker

A web-based CI/CD management tool with a **graph-based UI** for visually building and executing pipelines that build **OCI-compliant** Docker images, push to registries, and deploy to **Kubernetes** across Dev, Staging, and Production environments.

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
- **Auth**: OIDC/OAuth 2.0 with PKCE (Keycloak, Okta, Azure AD, Google, GitHub) — disabled by default in local/UAT (the backend injects a dev admin user); see [docs/UAT.md](docs/UAT.md#enabling-oidc-sign-in-for-uat) to enable.

See [docs/architecture.md](docs/architecture.md) for the full architecture document.

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
# Local development
docker compose up

# Frontend: http://localhost:5173
# Backend:  http://localhost:8080
# API:      http://localhost:8080/api/v1
```

## Features

- **Visual Pipeline Builder** - Drag-and-drop graph editor with React Flow, 6 node types (Build, Test, Deploy, Push, Approval, Custom)
- **Multi-Environment Deployment** - Dev → Staging → Production with configurable auto/manual promotion and approval gates
- **Docker Management** - Build, list, inspect OCI images and manage containers with manifest details
- **OCI Registry Integration** - Push/pull via distribution-spec, browse tags, inspect manifests, referrers API for supply chain metadata
- **Kubernetes Dashboard** - Manage workloads, scale, restart, view logs across clusters and namespaces
- **SSO Authentication** - OpenID Connect with PKCE, RBAC (admin, operator, viewer roles)
- **Live Execution** - WebSocket-powered real-time pipeline status, build logs, and K8s event streaming
- **Environment Swim Lanes** - Visual grouping of pipeline stages by deployment environment

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
| **Environments** | CRUD, promote, approve, per-env status |
| **WebSocket** | Pipeline run stream, Docker build stream, K8s watch events |

## Project Structure

```
├── frontend/              React + TypeScript + Vite
│   └── src/
│       ├── components/    React Flow nodes, edges, panels, layout
│       ├── stores/        Zustand (pipeline, Docker, K8s, environment)
│       ├── api/           Typed API client per domain
│       ├── auth/          OIDC provider + protected routes
│       ├── hooks/         WebSocket, pipeline execution, K8s watch
│       ├── types/         TypeScript types (pipeline, Docker, K8s, OCI)
│       └── pages/         Pipelines, Editor, Docker, K8s, Environments
├── backend/               Go API server
│   ├── cmd/cooker/        Entry point
│   ├── internal/
│   │   ├── server/        HTTP server, router, WebSocket hub
│   │   ├── auth/          OIDC middleware, RBAC
│   │   ├── handler/       HTTP handlers (pipeline, Docker, K8s, registry, env)
│   │   ├── service/       Business logic (executor, promoter)
│   │   ├── model/         Domain types
│   │   ├── oci/           OCI image-spec types, media types, validation
│   │   └── store/         PostgreSQL persistence + migrations
│   └── pkg/
│       ├── dagrunner/     Reusable DAG execution engine
│       └── ociutil/       OCI descriptor utilities
├── deploy/
│   ├── docker/            Dockerfiles (multi-stage, dev frontend, dev backend)
│   ├── kubernetes/        Raw manifests (namespace, deployment, service, ingress, RBAC)
│   └── helm/cooker/       Helm chart
├── docs/
│   └── architecture.md    Full architecture document
├── CHANGELOG.md           Version history
├── SECURITY.md            Security policy and guidelines
├── docker-compose.yml     Local development stack
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
  --set secretKey.existingSecret=cooker-secret-key
```

See [SECURITY.md](SECURITY.md) for the production deployment security checklist.

## Documentation

| Document | Description |
|----------|-------------|
| [docs/architecture.md](docs/architecture.md) | System architecture, component map, data flow, OCI integration |
| [docs/design.md](docs/design.md) | Design patterns, conventions, auth flow, testing strategy, contributor checklist |
| [docs/UAT.md](docs/UAT.md) | UAT runbook and how to enable OIDC sign-in for testers |
| [CHANGELOG.md](CHANGELOG.md) | Version history following Keep a Changelog format |
| [SECURITY.md](SECURITY.md) | Security policy, auth architecture, production hardening checklist |

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/my-feature`)
3. Run tests: `make test`
4. Commit changes
5. Push and open a pull request

## License

MIT
