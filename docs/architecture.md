# Cooker Architecture

## Overview

Cooker is a three-tier web application for CI/CD pipeline management with visual graph editing, Docker/Kubernetes operations, and full OCI standard compliance.

```
┌──────────────────────────────────────────────────────────────┐
│                     Browser (User)                            │
│  ┌────────────────────────────────────────────────────────┐  │
│  │  React + TypeScript Frontend                            │  │
│  │  ┌──────────────┐  ┌─────────────┐  ┌──────────────┐  │  │
│  │  │ React Flow   │  │  Dashboard  │  │  Env Manager │  │  │
│  │  │ Pipeline     │  │  Docker/K8s │  │  Dev/Stg/Prod│  │  │
│  │  │ Editor       │  │  Panels     │  │  Promotion   │  │  │
│  │  └──────────────┘  └─────────────┘  └──────────────┘  │  │
│  │  ┌──────────────────────────────────────────────────┐  │  │
│  │  │  Zustand State  │  WebSocket Client  │  OIDC Auth│  │  │
│  │  └──────────────────────────────────────────────────┘  │  │
│  └─────────────────────────┬──────────────────────────────┘  │
└────────────────────────────┼─────────────────────────────────┘
                             │ HTTPS / WSS
┌────────────────────────────▼─────────────────────────────────┐
│                   Go API Server (Gin)                         │
│                                                               │
│  ┌─────────────┐  ┌──────────────┐  ┌──────────────────────┐│
│  │ OIDC Auth   │  │ CORS         │  │ WebSocket Hub        ││
│  │ Middleware   │  │ Middleware   │  │ (fan-out broadcast)  ││
│  └──────┬──────┘  └──────┬───────┘  └──────────┬───────────┘│
│         │                │                      │            │
│  ┌──────▼────────────────▼──────────────────────▼──────────┐│
│  │                    HTTP Router                           ││
│  │  /api/v1/pipelines  /api/v1/docker  /api/v1/kubernetes  ││
│  │  /api/v1/registry   /api/v1/environments  /api/v1/settings│
│  │  /ws/pipeline-run   /ws/docker/build  /ws/kubernetes/watch│
│  └──────┬────────────────┬────────────────┬────────────────┘│
│         │                │                │                  │
│  ┌──────▼──────┐  ┌──────▼──────┐  ┌─────▼───────┐        │
│  │  Handlers   │  │  Services   │  │  DAG Runner  │        │
│  │  (thin HTTP │  │  (business  │  │  (topo sort  │        │
│  │   layer)    │  │   logic)    │  │   parallel)  │        │
│  └─────────────┘  └──────┬──────┘  └─────────────┘        │
│                          │                                   │
│         ┌────────────────┼────────────────┐                 │
│  ┌──────▼──────┐  ┌──────▼──────┐  ┌─────▼───────┐        │
│  │ OCI Package │  │ Store Layer │  │  Promoter   │        │
│  │ (manifest,  │  │ (PostgreSQL │  │  (env       │        │
│  │  index,     │  │  + Redis)   │  │   promotion)│        │
│  │  descriptor)│  │             │  │             │        │
│  └──────┬──────┘  └──────┬──────┘  └─────────────┘        │
│         │                │                                   │
└─────────┼────────────────┼───────────────────────────────────┘
          │                │
   ┌──────▼──────┐  ┌──────▼──────┐
   │ External    │  │ Persistence │
   │ Systems     │  │             │
   │             │  │ PostgreSQL  │
   │ Docker      │  │ Redis       │
   │ Kubernetes  │  │             │
   │ OCI Registries│             │
   └─────────────┘  └─────────────┘
```

## Component Details

### Frontend

| Component | Technology | Responsibility |
|-----------|------------|----------------|
| **PipelineCanvas** | React Flow (xyflow) | Visual DAG editor. Renders pipeline stages as nodes, dependencies as edges. Supports drag-and-drop node creation, connection drawing, and live execution visualization. |
| **Custom Nodes** | React + React Flow | BuildNode, TestNode, DeployNode, PushNode, ApprovalNode, CustomNode. Each renders an icon, label, config summary, status indicator, and connection handles. |
| **ConditionalEdge** | React Flow | Custom edge with labels for conditions (success/failure/always). Animated during execution. |
| **PipelineToolbar** | React | Draggable node palette for adding stages. Run/Save/Validate action buttons. |
| **NodeConfigPanel** | React | Slide-out panel for editing stage configuration when a node is selected. |
| **EnvironmentSwimlane** | React Flow Group Node | Visual boundary for Dev/Staging/Prod. Nodes dropped inside auto-assign to that environment. Collapsible. |
| **Zustand Stores** | Zustand | Decoupled state management: pipelineStore (graph state), dockerStore (images/containers), kubernetesStore (workloads), environmentStore (envs/promotion), uiStore (sidebar/tabs). |
| **API Client** | Fetch API | Typed wrappers (`get`, `post`, `put`, `del`) for all backend endpoints. Separate modules per domain. |
| **WebSocket Hooks** | React Hooks | `useWebSocket` (generic), `usePipelineExecution` (live run status), `useKubeWatch` (K8s events). |
| **OIDCProvider** | oidc-client-ts | OIDC PKCE authentication flow. React context for user state. |
| **ProtectedRoute** | React | Route guard checking authentication and RBAC roles. |

### Backend

| Component | Package | Responsibility |
|-----------|---------|----------------|
| **Server** | `internal/server` | Gin HTTP server setup, middleware registration (CORS, auth), route mounting. |
| **Router** | `internal/server/router.go` | Route registration for all API groups: pipelines, Docker, K8s, registry, environments, settings, WebSocket. |
| **WebSocket Hub** | `internal/server/websocket.go` | Fan-out message broadcasting. Manages client connections per channel. Channels: `pipeline-run:<id>`, `docker-build:<id>`, `kube-watch:<ns>:<resource>`, `stage-logs:<runId>:<stageId>` (per-stage live log tail; written by the executor's `service.LogBroadcaster` tee, consumed by the frontend `useStageLogs` hook). |
| **AppHealthChecker** | `internal/service/app_health.go` | Periodic background goroutine (default 30s, configurable via `COOKER_APP_HEALTH_INTERVAL`). For each App, dispatches to a per-deploy-target `Prober` and writes the verdict via `AppStore.UpdateHealth`. Health writes do NOT bump `App.Version` — observational state must not race against user-driven `Update` calls under optimistic concurrency. Cancelled by `Server.RunContext` after run-coordinator drain on SIGTERM. |
| **Handlers** | `internal/handler/` | Thin HTTP layer. Parses requests, calls services, returns JSON. One file per domain: pipeline, docker, kubernetes, registry, environment. |
| **Services** | `internal/service/` | Business logic. Pipeline CRUD + DAG validation, executor (orchestrates pipeline runs), promoter (environment promotion logic). |
| **Models** | `internal/model/` | Domain types: Pipeline, Stage, StageConfig, Edge, PipelineRun, StageRun, Artifact, Environment, EnvironmentTarget, PromotionPolicy, ContainerInfo, ImageInfo, KubeWorkload. |
| **OCI Package** | `internal/oci/` | OCI image-spec types (Manifest, Index, Descriptor, Platform), media type constants, validation functions, digest computation. |
| **Auth** | `internal/auth/` | OIDC middleware (token extraction + validation), RBAC middleware (role checking), configurable group-to-role mapping (`COOKER_OIDC_GROUP_MAP`), and `RequireMFA` step-up gate (`COOKER_OIDC_MFA_ACR_VALUES`). |
| **Store** | `internal/store/` | Data access interfaces (PipelineStore, RunStore, EnvironmentStore) with PostgreSQL implementation. |
| **Secrets** | `internal/secrets/` | `secrets.Manager` interface + adapters: `database` (default, AES-GCM), `keepsave`, `vault` (KV v2), `awsm` (AWS Secrets Manager), `gcpsm` (GCP Secret Manager). Optional `secrets.Promoter` interface for backends that support cross-environment promotion. |
| **Builders** | `internal/builder/` | `Builder` interface + adapters: `docker` (host-socket; dev only), `kaniko` (in-cluster Job), `buildah` (in-cluster Job, full Dockerfile parity), `buildkit` (gRPC against external buildkitd). |
| **Pusher** | `internal/pusher/` | `Pusher` interface + adapters: `crane` (`go-containerregistry`), `docker` (CLI shell-out), `noop`. |
| **Deployer** | `internal/deployer/` | `Deployer` interface + adapters: `kubectl` (CLI shell-out), `clientgo` (dynamic client + server-side apply), `noop`. |
| **DeployTarget** | `internal/deploytarget/` | App-deploy adapters per cloud runtime: `kubernetes`, `cloudrun`, `ecs` (Fargate), `flyio`, `render`. Self-register on non-empty config. |
| **GitOps** | `internal/gitops/` | `Writer` interface + `gogit` adapter (`go-git/v5`). Used by GitOpsCommit pipeline node. |
| **Observability** | `internal/observability/` | Prometheus `/metrics` (Gin middleware) + OpenTelemetry tracing (otelgin + OTLP/gRPC exporter). Both opt-in. |
| **DAG Runner** | `pkg/dagrunner/` | Reusable, standalone DAG execution engine. Topological sort into parallel levels, concurrent execution, status updates via channel. |
| **OCI Utilities** | `pkg/ociutil/` | Higher-level helpers: manifest parsing, index parsing, layer size calculation. |
| **Config** | `internal/config/` | Environment-variable-based configuration loading with defaults. |

### Data Flow

#### Pipeline Creation and Execution

```
1. User drags nodes onto React Flow canvas
2. Frontend: Zustand store updates Pipeline.stages[] and Pipeline.edges[]
3. User clicks "Save" → POST /api/v1/pipelines (or PUT for update)
4. Backend: Handler validates, stores in PostgreSQL (stages/edges as JSONB)
5. User clicks "Run" → POST /api/v1/pipelines/:id/run
6. Backend: Executor builds DAG from pipeline, runs topological sort
7. DAG Runner executes stages level-by-level (parallel within levels)
8. Each stage: Build → Docker SDK, Test → container run, Push → go-containerregistry, Deploy → client-go
9. Status updates broadcast via WebSocket → frontend updates node colors in real-time
```

#### Multi-Environment Promotion

```
1. Pipeline stages are assigned to environments (Dev/Staging/Prod)
2. Execution starts with Dev environment stages
3. After Dev stages succeed:
   a. Auto-promote: Promoter automatically triggers Staging stages
   b. Manual: Pipeline pauses, EnvironmentStatus = "awaiting_approval"
4. User clicks "Approve" → POST /api/v1/pipelines/:id/runs/:runId/approve
5. Promoter transitions to Deploying, triggers Production stages
6. EnvironmentBar in UI updates per-env status in real-time
```

## OCI Standards Integration

### image-spec v1.1

**Reference**: [github.com/opencontainers/image-spec](https://github.com/opencontainers/image-spec)

| Concept | Where in Cooker | Implementation |
|---------|-----------------|----------------|
| **Image Manifest** | `internal/oci/manifest.go` | Go struct mirroring the OCI manifest schema. Used when inspecting built images. |
| **Image Index** | `internal/oci/manifest.go` | Multi-platform manifest list. Created when BuildNode specifies multiple `platforms`. |
| **Descriptor** | `internal/oci/manifest.go` | `{mediaType, digest, size}` triple. Pipeline artifacts are stored as descriptors. |
| **Media Types** | `internal/oci/mediatype.go` | Constants for all OCI and Docker-compatible media types. |
| **Content Addressing** | `internal/oci/layer.go` | SHA-256 digest computation. All image references use `sha256:` digests. |

### runtime-spec v1.2

**Reference**: [github.com/opencontainers/runtime-spec](https://github.com/opencontainers/runtime-spec)

The runtime-spec is consumed indirectly through the Docker Engine SDK. When creating containers from the Docker management panel, Cooker exposes runtime-spec concepts in the UI:

- **Process**: command, args, environment variables, working directory
- **Mounts**: volume bindings
- **Resource limits**: CPU, memory (maps to cgroups)

### distribution-spec v1.1

**Reference**: [github.com/opencontainers/distribution-spec](https://github.com/opencontainers/distribution-spec)

| Endpoint | OCI Spec | Cooker API |
|----------|----------|------------|
| `GET /v2/<name>/tags/list` | Tag listing | `GET /api/v1/registry/:name/tags` |
| `GET /v2/<name>/manifests/<ref>` | Manifest retrieval | `GET /api/v1/registry/:name/manifests/:ref` |
| `POST /v2/<name>/blobs/uploads/` | Blob upload init | Internal (push flow) |
| `PUT /v2/<name>/manifests/<ref>` | Manifest push | `POST /api/v1/registry/push` |
| `GET /v2/<name>/referrers/<digest>` | Referrers API | `GET /api/v1/registry/:name/referrers/:digest` |

The backend Registry Service wraps `go-containerregistry` which natively implements these endpoints.

## Database Schema

```sql
pipelines (
  id TEXT PRIMARY KEY,
  name TEXT,
  description TEXT,
  stages JSONB,          -- Array of Stage objects (graph nodes)
  edges JSONB,           -- Array of Edge objects (graph connections)
  variables JSONB,       -- Pipeline-level key-value variables
  created_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ
)

pipeline_runs (
  id TEXT PRIMARY KEY,
  pipeline_id TEXT REFERENCES pipelines(id),
  status TEXT,           -- pending, running, success, failed, cancelled
  stage_runs JSONB,      -- Array of StageRun objects
  env_statuses JSONB,    -- Array of EnvironmentStatus objects
  variables JSONB,
  started_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ,
  error TEXT
)

environments (
  id TEXT PRIMARY KEY,
  name TEXT UNIQUE,      -- dev, staging, production
  sort_order INT,        -- Promotion order
  target JSONB,          -- {type, clusterId, namespace, kubeContext}
  promotion JSONB,       -- {strategy, requiredApprovers, autoPromoteOn}
  variables JSONB        -- Environment-specific overrides
)
```

Design decision: Stages and edges are stored as JSONB rather than normalized tables. This keeps the pipeline graph as a single atomic document, matching the React Flow data model and simplifying save/load operations.

## Key Libraries

| Library | Purpose |
|---------|---------|
| `github.com/gin-gonic/gin` | HTTP framework |
| `github.com/gorilla/websocket` | WebSocket connections |
| `github.com/google/uuid` | UUID generation |
| `github.com/lib/pq` | PostgreSQL driver |
| `github.com/coreos/go-oidc/v3` | OIDC token validation |
| `github.com/docker/docker/client` | Docker Engine SDK (planned) |
| `k8s.io/client-go` | Kubernetes dynamic client (server-side apply in `internal/deployer/clientgo.go`) |
| `github.com/google/go-containerregistry` | OCI image push/pull (`internal/pusher/crane.go`) |
| `github.com/moby/buildkit/client` | gRPC BuildKit driver (`internal/builder/buildkit.go`) |
| `github.com/go-git/go-git/v5` | GitOps commits (`internal/gitops/gogit.go`) |
| `github.com/redis/go-redis/v9` + `github.com/go-redis/redis_rate/v10` | Multi-replica rate limiter + WS ticket store |
| `github.com/prometheus/client_golang` | `/metrics` endpoint |
| `go.opentelemetry.io/otel` + `otelgin` + OTLP/gRPC exporter | Distributed tracing |
| `github.com/hashicorp/vault-client-go` | Vault KV v2 secrets backend |
| `github.com/aws/aws-sdk-go-v2` (config + secretsmanager + ecs) | AWS Secrets Manager + ECS deploy target |
| `cloud.google.com/go/secretmanager` + `cloud.google.com/go/run/apiv2` | GCP Secret Manager + Cloud Run deploy target |
| `github.com/swaggo/swag` | Generated OpenAPI from doc-comments |
| `@xyflow/react` | React Flow graph visualization |
| `zustand` | Frontend state management |
| `oidc-client-ts` | Browser OIDC client |
| `react-router-dom` | Client-side routing |

## Deployment Architecture

### Single Container

Cooker ships as a single container image. The Go binary serves both the API and the static frontend files on port 8080. This simplifies deployment -- one Deployment, one Service.

```
Alpine container
├── /usr/local/bin/cooker     (Go binary: API + static file server)
└── /usr/share/cooker/static/ (Built React frontend)
```

### Kubernetes Deployment

```
┌─ Namespace: cooker ─────────────────────────────────────────┐
│                                                              │
│  ┌─────────────┐  ┌────────────┐  ┌────────────┐           │
│  │   Cooker    │  │ PostgreSQL │  │   Redis    │           │
│  │ Deployment  │  │ StatefulSet│  │ Deployment │           │
│  │ (1 replica) │  │            │  │            │           │
│  └──────┬──────┘  └─────┬──────┘  └─────┬──────┘           │
│         │               │               │                   │
│  ┌──────▼──────┐  ┌─────▼──────┐  ┌─────▼──────┐           │
│  │   Service   │  │  Service   │  │  Service   │           │
│  │  :8080      │  │  :5432     │  │  :6379     │           │
│  └──────┬──────┘  └────────────┘  └────────────┘           │
│         │                                                    │
│  ┌──────▼──────┐                                            │
│  │   Ingress   │                                            │
│  │ cooker.example.com                                       │
│  └─────────────┘                                            │
│                                                              │
│  ServiceAccount + ClusterRole + ClusterRoleBinding           │
└──────────────────────────────────────────────────────────────┘
```

### Environment Targets

Cooker manages deployments to three environments. Each can point to a different infrastructure target:

| Environment | Target Options |
|-------------|----------------|
| **Dev** | Namespace `cooker-dev` in the same cluster, or a separate dev cluster |
| **Staging** | Namespace `cooker-staging` or a dedicated staging cluster |
| **Production** | Separate production cluster (recommended) or `cooker-prod` namespace |

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| **Go for backend** | Native Docker SDK, client-go, go-containerregistry. No FFI overhead. First-class container ecosystem support. |
| **Zustand over Redux** | Pipeline editor, Docker panel, K8s panel, and environment state are naturally independent slices. Zustand handles this with less boilerplate. |
| **JSONB for graph storage** | Pipeline graphs are loaded/saved as atomic documents. Normalizing stages and edges into separate tables adds complexity without benefit for the graph editing use case. |
| **Single container** | Go binary serves static files. Simplifies deployment, networking, and CORS. One port, one container, one Deployment. |
| **Reusable DAG runner** | Extracted to `pkg/dagrunner` as a standalone package. The scheduling algorithm (topological sort + parallel dispatch) is independent of pipeline-specific concerns and can be tested in isolation. |
| **Runtime-selectable store** | Both `internal/store/memory` and `internal/store/postgres` satisfy the same `store.Store` interfaces. Empty `DATABASE_URL` selects the in-memory backend (tests, hot-reload dev); a non-empty URL selects PostgreSQL with embedded migrations applied at boot. See [design.md §2.2](design.md#22-repository-pattern--pluggable-persistence). |
