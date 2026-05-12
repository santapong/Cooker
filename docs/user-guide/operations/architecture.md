# Architecture (operator's view)

A condensed map of the system, for people running Cooker. For the full architecture document, see [`docs/architecture.md`](../../architecture.md).

## High-level

```text
┌────────────────────────────────────────────────────────────┐
│  Browser: React + TypeScript + React Flow                  │
│  Zustand state, WebSocket live updates, OIDC PKCE auth     │
└────────────────────────────┬───────────────────────────────┘
                             │ HTTPS / WSS
┌────────────────────────────▼───────────────────────────────┐
│         Go binary on :8080                                  │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐ │
│  │ OIDC + RBAC  │  │ CORS         │  │ WS Hub + Tickets │ │
│  │ middleware   │  │              │  │                  │ │
│  └──────┬───────┘  └──────┬───────┘  └────────┬─────────┘ │
│         │                 │                    │           │
│  ┌──────▼─────────────────▼────────────────────▼─────────┐│
│  │             Gin HTTP router                            ││
│  └──────┬─────────────────┬────────────────┬─────────────┘│
│         │                 │                │              │
│  ┌──────▼──────┐   ┌──────▼──────┐   ┌─────▼─────────┐  │
│  │  Handlers   │──►│   Services  │──►│  DAG Runner   │  │
│  │  (HTTP-only)│   │ (business   │   │ (pkg/dagrunner)│ │
│  │             │   │  logic)     │   │                │ │
│  └─────────────┘   └──────┬──────┘   └────────────────┘ │
│                           │                              │
│              ┌────────────┼────────────┐                 │
│       ┌──────▼──────┐ ┌───▼────┐ ┌─────▼─────┐          │
│       │  Builders   │ │Pushers │ │ Deployers │          │
│       │  (kaniko,   │ │(crane, │ │(clientgo, │          │
│       │   buildah)  │ │ docker)│ │  kubectl) │          │
│       └─────────────┘ └────────┘ └───────────┘          │
│                           │                              │
│                    ┌──────▼──────┐                      │
│                    │   Store     │                      │
│                    │ (Postgres   │                      │
│                    │  + Redis)   │                      │
│                    └─────────────┘                      │
└──────────────────────────────────────────────────────────┘
        │                        │                  │
   K8s API                  OCI registries     PostgreSQL + Redis
```

<!-- SCREENSHOT: a higher-fidelity rendered version of the architecture diagram -->

## What ships in one container

The Cooker container image (`ghcr.io/cooker-ci/cooker:<tag>`, once releases are wired up) contains:

| Component | Path inside container |
|---|---|
| Go binary | `/usr/local/bin/cooker` |
| React frontend (built) | `/usr/share/cooker/static/` |
| Embedded Postgres migrations | Inside the binary (`//go:embed`) |
| `kubectl` | `/usr/local/bin/kubectl` |
| `git` | `/usr/bin/git` |
| `docker` CLI | `/usr/local/bin/docker` |

The binary serves both the API and the static frontend on port 8080. Static asset paths under `/assets` go to the bundle; the SPA fallback (`NoRoute`) serves `index.html` for everything else, which makes client-side routing (including `/callback`) work without server-side handlers.

> **Note.** `kubectl` ships even when the `clientgo` deployer is the active one. This is a slight attack-surface increase; `S26-05-16` tracks making the kubectl install optional.

## Layered architecture (backend)

```text
┌────────────────────────────────────────────┐
│ Layer 1 — Transport   (Gin router, WS hub) │
├────────────────────────────────────────────┤
│ Layer 2 — Middleware  (CORS, OIDC, RBAC)   │
├────────────────────────────────────────────┤
│ Layer 3 — Handlers    (HTTP <-> services)  │
├────────────────────────────────────────────┤
│ Layer 4 — Services    (business logic)     │
├────────────────────────────────────────────┤
│ Layer 5 — Adapters    (Builder, Pusher,    │
│                        Deployer, Store)    │
└────────────────────────────────────────────┘
```

Higher layers depend on **interfaces** defined by lower layers, not on concrete types. Concrete adapters are wired together once at startup in `backend/cmd/cooker/main.go`.

This is documented in detail in [`docs/design.md`](../../design.md) — read that one if you're hacking on Cooker itself.

## Where state lives

| Where | What |
|---|---|
| **Postgres** | Pipelines, pipeline_runs, environments, apps, hosts, audit (when destination=file is unused). |
| **Redis** *(optional)* | Rate-limit buckets, WS tickets, WS hub fan-out. Only when corresponding `_BACKEND=redis`. |
| **In-memory** | Idempotency cache (32 MiB LRU). Per-process; rebuilt on restart. |
| **`oidc-client-ts` localStorage** | OIDC ID token + access token + refresh token. Browser-side only. |

## External dependencies

| Dependency | Role | Critical? |
|---|---|---|
| Postgres | Source of truth for all persistent state. | Yes — Cooker won't boot if it can't reach Postgres. |
| OIDC provider | JWT issuance + JWKS. | Yes when `COOKER_OIDC_ENABLED=true`. Lazy discovery means brief unreachability at boot is OK. |
| Docker daemon (host or remote) | When `COOKER_BUILDER=docker` or for legacy push paths. | Only if the corresponding backend is selected. |
| K8s cluster | Deploys, Kaniko / Buildah Jobs. | Only for K8s deploy targets. |
| OCI registry | Pushes. | Only if `COOKER_PUSHER != noop`. |
| Redis | Multi-replica shared state. | Only if `COOKER_REPLICA_COUNT > 1` without sticky sessions. |

## Concurrency model

- **HTTP requests** are handled per-goroutine by Gin.
- **Pipeline runs** run in a coordinator goroutine spawned by the `Executor` service. Each stage runs in its own goroutine; the DAG runner manages the wait-and-dispatch.
- **WebSocket connections** each have a reader goroutine and a writer goroutine.
- **AppHealthChecker** is a single goroutine that ticks every `COOKER_APP_HEALTH_INTERVAL` and probes apps sequentially.
- **The orphan sweep** runs once at boot inside `RunStore.SweepOrphans`.

There is no separate worker process. Everything runs in the Cooker pod.

## Network surfaces

Inbound:

| Port | Protocol | Listener |
|---|---|---|
| 8080 (default; `COOKER_PORT`) | HTTP / WS | The single Gin server. |

Outbound:

- Postgres (5432, configurable per `DATABASE_URL`).
- Redis (6379, configurable per `REDIS_URL`).
- OIDC provider (443 typically, on the issuer URL).
- K8s API server (configured via kubeconfig or in-cluster).
- OCI registries (443 typically).
- OTLP collector (4317 typically, when tracing is enabled).
- Docker daemon (Unix socket or TCP).

## Graceful shutdown

On `SIGTERM` / `SIGINT`:

1. The HTTP server stops accepting new connections.
2. In-flight HTTP requests get up to 30 seconds to complete (`terminationGracePeriodSeconds` -30).
3. Tracked pipeline runs get up to 25 seconds to finish (`Server.RunContext` waits for the run coordinator's drain channel).
4. The store closes; Postgres connections are returned to the pool.
5. Process exits.

A run that doesn't finish within the drain window has its `status` left as `running` with a stale `heartbeat_at`. The next boot's orphan sweep flips it to `failed`. See [Runs](../concepts/runs.md#the-heartbeat-and-orphan-sweep).

## Cross-references

- **Full architecture document:** [`docs/architecture.md`](../../architecture.md).
- **Design patterns and conventions:** [`docs/design.md`](../../design.md).
- **Auth flow specifics:** [Auth & RBAC](auth-and-rbac.md).
- **Observability hooks:** [Observability](observability.md).
