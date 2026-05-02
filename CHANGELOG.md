# Changelog

All notable changes to the Cooker project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Pluggable secrets backend** (`backend/internal/secrets/`). New `secrets.Manager` interface mirrors the existing builder/pusher/deployer strategy pattern; selectable at boot via `COOKER_SECRETS_BACKEND`. Closes backlog **P2.1**.
  - `database` adapter (default) wraps the historical AES-GCM + JSONB path; behavior is unchanged when this backend is selected.
  - `keepsave` adapter delegates storage to a [KeepSave](https://github.com/santapong/keepsave) server. Cooker's environment name maps to KeepSave's `environment` parameter; a single KeepSave project owns all of Cooker's secrets.
  - New env vars: `COOKER_SECRETS_BACKEND`, `COOKER_SECRETS_KEEPSAVE_URL`, `COOKER_SECRETS_KEEPSAVE_PROJECT_ID`, `COOKER_SECRETS_KEEPSAVE_API_KEY`.
  - Production startup validation extended to require KeepSave config when that backend is selected.
- **CI: `helm lint` + `helm template` + `kubeconform` job** in `.github/workflows/ci.yml`. Validates the chart against default and production-with-OIDC values on every push. Closes **P6.1**.
- **CI: `Register` returns error; `MustRegister` for init() callers** in `backend/internal/deploytarget/target.go`. Replaces the historical `panic` in `Register`. Tests cover both contracts. Closes the panic-removal item from **P6.2**.
- **Renovate config** at the repo root (`renovate.json`): weekly Mon-AM schedule, automerge minor/patch on green CI, major bumps gated on human review, custom regex manager for `KUBECTL_VERSION` ARG in the Dockerfile. Closes **P1.5**.
- **Helm chart values**: `ingress.tls`, `postgresql.sslMode`, and `secrets.backend` / `secrets.keepsave.*` blocks documented in `deploy/helm/cooker/values.yaml`. Chart-side rendering of `sslMode` and KeepSave env-var wiring are tracked as follow-ups.
- **Documentation:**
  - `docs/MULTI_REPLICA.md` — sticky-session + Redis-shared-state guide for multi-replica deploys, with NGINX/ALB/Traefik/HAProxy/Envoy examples. Closes the docs portion of **P3**.
  - `docs/RUNBOOK.md` — incident response runbook covering hung builds, Postgres down, OIDC unreachable, KeepSave outage, OOMKilled. Closes **P8** runbook.
  - `docs/adr/` — three accepted ADRs covering the strategy-pattern interfaces, the secrets-manager rationale, and the JSONB graph-storage decision. Closes **P8** ADRs.
  - README §Deployment now documents TLS at ingress and Postgres SSL with concrete config snippets. Closes the docs portion of **P1.3** and **P1.4**.
  - README §Operations table indexes RUNBOOK, MULTI_REPLICA, SECURITY, and the backlog so operators land on the right doc faster.
- **Frontend `ErrorBoundary`** at the app root (`frontend/src/components/ErrorBoundary.tsx`, wired in `App.tsx`). Catches uncaught render errors so the React tree no longer crashes to a blank page; provides Try-again and Go-home recovery paths. Closes **P5** error-boundary item.

### Changed

- `handler.New(store, codec)` is now `handler.New(store, codec, secrets.Manager)`. Secret CRUD endpoints (`PutSecret`, `RevealSecret`, `DeleteSecret`) delegate to the configured Manager rather than touching `crypto.Codec` directly. Behavior on the wire is unchanged.
- The `requireCodec` middleware split into two gates: `requireSecrets` (Manager-presence check used by env-secret endpoints) and `requireCodec` (Codec-active check still used by App-webhook endpoints, which encrypt outside the Manager).

### Notes for operators

- Switching secrets backends does **not** auto-migrate existing secrets. Plan a one-shot copy step (read from old, write to new) before flipping `COOKER_SECRETS_BACKEND`.
- The `keepsave` adapter currently uses an internal HTTP client (`backend/internal/secrets/keepsave/client.go`) rather than the published Go SDK at `github.com/santapong/KeepSave/sdks/go`, because the SDK directory does not yet contain a `go.mod`. The client surface aligns with the SDK so a future swap is mechanical.
- Multi-replica deployments must apply sticky sessions (see `docs/MULTI_REPLICA.md`) until the Redis-backed rate limiter and ticket store land (open backlog item P3).

## [0.1.0] - 2026-03-21

### Added

#### Core Platform
- Initial project scaffolding with Go backend and React frontend
- `docker-compose.yml` for local development (frontend, backend, PostgreSQL, Redis)
- `Makefile` with build, test, lint, dev, and deploy targets
- GitHub Actions CI pipeline (backend test, frontend lint/build, Docker image build)

#### Backend (Go + Gin)
- HTTP API server with Gin framework and CORS middleware
- Pipeline CRUD endpoints (`/api/v1/pipelines`) with in-memory store (PostgreSQL-ready)
- DAG validation with cycle detection using Kahn's algorithm
- Pipeline execution engine with topological sort and parallel stage execution
- Reusable DAG runner package (`pkg/dagrunner`) with comprehensive tests
- Docker management endpoints (`/api/v1/docker/images`, `/api/v1/docker/containers`)
- Kubernetes management endpoints (`/api/v1/kubernetes/workloads`, namespaces, pods)
- OCI Registry endpoints following distribution-spec v1.1 (`/api/v1/registry`)
- Referrers API support for supply chain metadata (signatures, SBOMs)
- Multi-environment support (Dev/Staging/Production) with promotion API
- Environment CRUD endpoints with configurable auto/manual promotion policies
- SSO authentication via OIDC/OAuth 2.0 with PKCE flow
- RBAC middleware with admin, operator, viewer roles mapped from OIDC claims
- WebSocket hub for real-time streaming (pipeline runs, Docker builds, K8s watch)
- PostgreSQL schema with JSONB storage for pipeline graphs
- Database migrations (001_initial: pipelines, pipeline_runs, environments tables)
- Store interfaces and PostgreSQL implementation for pipeline persistence
- Health check endpoint (`/health`)

#### OCI Compliance
- OCI image-spec v1.1 types: Manifest, Index, Descriptor, Platform
- OCI media type constants with Docker compatibility types
- Manifest and Index validation functions
- Content-addressable digest computation (SHA-256)
- Helper functions for creating OCI Manifests and Image Indexes
- OCI utility package (`pkg/ociutil`) for parsing and inspecting manifests

#### Frontend (React + TypeScript + Vite)
- React Flow graph-based pipeline editor with drag-and-drop from toolbar
- Six custom node types: BuildNode, TestNode, DeployNode, PushNode, ApprovalNode, CustomNode
- ConditionalEdge component with visual labels (success/failure/always)
- Pipeline toolbar with draggable node palette and Run/Save/Validate actions
- Node configuration panel (slide-out form for editing stage config)
- Run history panel with status indicators
- Zustand stores for pipeline, Docker, Kubernetes, environment, and UI state
- Typed API client with `get`, `post`, `put`, `del` wrappers
- Separate API modules for pipelines, Docker, Kubernetes, and registry
- Pipelines list page with create and navigate to editor
- Pipeline editor page with React Flow integration
- Docker management page (images table, containers table)
- Kubernetes dashboard page (workloads table, namespace selector, scale/restart)
- Environments page with promotion flow visualization (Dev → Staging → Prod)
- OIDC authentication provider with React context
- Protected route component with role-based access checks
- WebSocket hooks (`useWebSocket`, `usePipelineExecution`, `useKubeWatch`)
- DAG validation utility (cycle detection, reference checking) on frontend
- OCI media type utilities with size formatting
- Dark theme UI with CSS custom properties
- Application layout with sidebar navigation and top bar
- Environment status badges in top bar (Dev/Staging/Production)
- React Router with page routing

#### Deployment
- Multi-stage Dockerfile (Node frontend build + Go backend build → Alpine runtime)
- Development Dockerfiles for frontend (Vite dev server) and backend (Go with air)
- Kubernetes manifests: Namespace, Deployment, Service, Ingress, ServiceAccount, RBAC
- Helm chart with Chart.yaml, values.yaml, and templates (deployment, service, helpers)
- Configurable Helm values for OIDC, Docker socket, K8s access, PostgreSQL, Redis

#### Documentation
- README.md with architecture overview, quick start, and feature list

### OCI Standards Referenced
- [OCI image-spec v1.1](https://github.com/opencontainers/image-spec) - Image Manifest, Image Index, Descriptors
- [OCI runtime-spec v1.2](https://github.com/opencontainers/runtime-spec) - Container runtime configuration
- [OCI distribution-spec v1.1](https://github.com/opencontainers/distribution-spec) - Registry API, referrers API

### Technical Notes
- Backend uses in-memory stores for MVP; PostgreSQL store layer is implemented and ready for wiring
- Docker, Kubernetes, and Registry handlers are structured with placeholder implementations; service layer integration with Docker SDK, client-go, and go-containerregistry is the next step
- OIDC token validation uses placeholder parsing in dev mode; production wiring with `go-oidc` is prepared

[Unreleased]: https://github.com/cooker-ci/cooker/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/cooker-ci/cooker/releases/tag/v0.1.0
