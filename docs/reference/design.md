# Cooker — Design Document

This document describes the architectural style, design patterns, and engineering conventions used in Cooker. It complements [architecture.md](architecture.md) (which covers what the system *is*) by describing **how it is built and why**.

Audience: contributors, reviewers, and anyone integrating with or extending Cooker.

---

## 1. Architectural style

Cooker follows a **layered architecture** with **dependency inversion at the boundaries**. The backend is a single Go binary that also serves the static frontend bundle; the frontend is a single-page React application.

```
Frontend (React SPA)  ──────────────►  Backend (Go binary)
                                          │
                                          ▼
              ┌────────────────────────────────────────────────┐
              │  Layer 1 — Transport       (Gin router, WS hub) │
              ├────────────────────────────────────────────────┤
              │  Layer 2 — Middleware       (CORS, OIDC, RBAC)  │
              ├────────────────────────────────────────────────┤
              │  Layer 3 — Handlers         (HTTP <-> services) │
              ├────────────────────────────────────────────────┤
              │  Layer 4 — Services         (domain logic)      │
              ├────────────────────────────────────────────────┤
              │  Layer 5 — Adapters         (Builder, Pusher,   │
              │                              Deployer, Store)   │
              └────────────────────────────────────────────────┘
                                          │
                                          ▼
                            External: Docker, K8s, OCI registry, Postgres
```

Higher layers depend on **interfaces** defined by lower layers, not on concrete types. Concrete adapters (PostgreSQL store, Docker builder, kubectl deployer) are wired together once at startup in `backend/cmd/cooker/main.go` → `backend/internal/server/server.go:New`.

This produces a **hexagonal-flavoured** dependency graph: domain logic in `internal/service` and `pkg/dagrunner` knows nothing about Gin, Postgres, Docker, or Kubernetes — those are plug-in adapters.

---

## 2. Backend design patterns

### 2.1 Strategy pattern — pluggable execution backends

The pipeline executor doesn't know how to build, push, or deploy — it delegates to interface-typed backends. Concrete implementations are selected at runtime from `COOKER_BUILDER` / `COOKER_PUSHER` / `COOKER_DEPLOYER` env vars in `selectBuilder` / `selectPusher` / `selectDeployer` (`backend/internal/server/server.go:152-185`).

| Interface | File | Implementations |
|---|---|---|
| `builder.Builder` | `internal/build/builder/builder.go:49` | `DockerSock`, `Kaniko` (in-cluster Job), `Buildah` (in-cluster Job, full Dockerfile parity), `BuildKit` (gRPC), `Noop` |
| `pusher.Pusher` | `internal/build/pusher/pusher.go:37` | `DockerSock`, `Crane` (`go-containerregistry`), `Noop` |
| `deployer.Deployer` | `internal/deploy/deployer/deployer.go:54` | `Kubectl`, `ClientGo` (dynamic client + server-side apply), `Noop` |
| `deployer.WeightedDeployer` (optional) | `internal/deploy/deployer/deployer.go` | `Kubectl`, `ClientGo` — canary traffic split via replica weighting (OR-1). Backends that can't split traffic simply don't implement it; the service returns 422. |
| `secrets.Manager` | `internal/secrets/manager.go` | `database` (AES-GCM), `keepsave`, `vault`, `awsm` (AWS Secrets Manager), `gcpsm` (GCP Secret Manager) |
| `deploytarget.Target` | `internal/deploy/deploytarget/target.go` | `kubernetes`, `cloudrun`, `ecs` (Fargate), `flyio`, `render`. Self-register on non-empty config. |
| `gitops.Writer` | `internal/gitops/writer.go` | `gogit` (`go-git/v5`), `noop` |

**Why:** lets us run UAT end-to-end against `docker` + `kubectl` (operationally simple) and ship a production deploy with `buildkit` + `client-go` (no shell-out, no docker-cli dependency) without changing handler or service code. Unknown values fall back to `Noop` with a log line so a typo'd env var doesn't crash boot.

**Optional capabilities.** When only *some* implementations of an interface can do a thing, model it as a second, narrow interface that embeds the base one rather than widening the base (which would force every backend to stub a method it can't honour). The consumer type-asserts for the capability and degrades gracefully when it's absent. Canary deployments (OR-1) use this: `deployer.WeightedDeployer` embeds `Deployer` and adds `DeployWeighted`. Only the Kubernetes-backed deployers implement it; `service.CanaryService` type-asserts at construction and returns `ErrCanaryUnsupported` (→ HTTP 422) when the configured deployer can't split traffic. The same shape backs the per-target-kind `service.Prober` registry for app health.

### 2.2 Repository pattern — pluggable persistence

`internal/store/store.go` defines interfaces (`PipelineStore`, `RunStore`, `EnvironmentStore`, `AppStore`, `HostStore`) and an aggregate `Store` struct. Implementations live under `internal/store/memory/` and `internal/store/postgres/`.

Selection happens in `newStore` (`server.go:117`): empty `DATABASE_URL` → in-memory backend; otherwise → Postgres + embedded migrations.

**Why:** the in-memory backend is great for unit tests and hot-reload local dev. The Postgres backend uses `//go:embed migrations/*.up.sql` so migrations are sealed into the binary and apply automatically at boot. Both backends satisfy the same interfaces, so handlers and services don't change.

### 2.3 Functional options — variadic constructor configuration

Service-layer constructors take options instead of long parameter lists or builder structs.

```go
exec := service.NewExecutor(
    service.WithBuilder(...),
    service.WithPusher(...),
    service.WithDeployer(...),
)
```

**Why:** new dependencies can be added without breaking call sites, and tests can inject fakes one option at a time.

### 2.4 Middleware pattern — cross-cutting HTTP concerns

CORS, authentication, and authorization are plain `gin.HandlerFunc`s composed onto router groups:

- **CORS** — `corsMiddleware` (`server.go:128`) on the root engine.
- **OIDC** — `auth.Middleware.Handler()` (`internal/auth/oidc.go:61`) on the `/api/v1` group only. Validates the bearer JWT against the issuer's JWKS; injects `*Claims` into the Gin context. When OIDC is disabled (UAT/dev), `devHandler` injects a static admin user instead.
- **RBAC** — `auth.RequireRole(...)` (`internal/auth/rbac.go:20`) on individual mutating endpoints. Reads `*Claims` from context and 403s on insufficient role.

The `/health` endpoint is mounted **outside** any auth middleware on purpose — orchestrators must reach it without credentials.

### 2.5 Observer / pub-sub — WebSocket fan-out

`internal/server/websocket.go` runs a hub goroutine. Handlers call `h.WSBroadcast(channel, message)`; subscribers (browsers connected to `/ws/...`) receive only the channels they registered for. Channels are namespaced by entity ID: `pipeline-run:<id>`, `docker-build:<id>`, `kube-watch:<ns>:<resource>`.

**Why:** decouples writers (executor, K8s watcher) from readers (browser tabs). A run can have N browser observers without the executor knowing.

### 2.6 Fail-fast initialization

Configuration errors and unreachable dependencies are surfaced at boot, not on the first user request:

- OIDC provider discovery happens in `auth.NewMiddleware` (`oidc.go:41`) — a wrong issuer URL fails `make uat-up` immediately.
- Postgres connection is tested in `postgres.NewStore`; if it fails, `server.New` returns an error and `main.go` exits.
- The crypto codec validates `COOKER_SECRET_KEY` length and base64-decodes it before any handler runs.

**Why:** UAT noise is highest when failures appear hours into a session. Boot-time errors are obvious and reproducible.

### 2.7 Embedded resources — migrations & static frontend

- SQL migrations: `//go:embed migrations/*.up.sql` in `internal/store/postgres`. Migrations run on every boot; idempotent.
- Frontend bundle: copied into `/usr/share/cooker/static` in the Dockerfile and served by `s.router.NoRoute(...)` + `s.router.Static("/assets", ...)`.

**Why:** one Go binary contains everything needed to run the app. Deployment is `docker run cooker:latest` — no separate frontend deploy, no migration runner.

### 2.8 Read-only provider aggregation — cloud inventory

`internal/cloud/cloudinventory` (OR-2) is a second strategy-style extension point, distinct from the deploy/build adapters in §2.1 because it is **strictly read-only** and **cache-fronted** rather than action-oriented. A narrow `Provider` interface (`Name() / ListResources(ctx) / CostSummary(ctx)`) has `aws/` and `gcp/` implementations; a `Service` fans out to the enabled providers concurrently, aggregates into `model.CloudInventory`, and caches the result in memory with a TTL. Per-provider failures are isolated — one cloud being unreachable yields partial results with a per-provider `Error` rather than failing the whole request.

It mirrors `internal/kube`'s posture (lazy construction, nil-safe at the handler boundary, no mutation surface) and wires the same way the secrets/deploy adapters do: `newCloudInventory` in `server.go` constructs a provider per enabled cloud from `COOKER_CLOUD_*` config, and `h.CloudInventory` is the injected `CloudInventoryService` the three `GET/POST /cloud/*` handlers consume.

**Adding a cloud provider:** implement `cloudinventory.Provider` in a new `internal/cloud/cloudinventory/<cloud>/` package (list/describe/cost APIs only — never a mutation), add an `if cfg.<Cloud>.Enabled` branch to `newCloudInventory` in `server.go`, extend `config.CloudInventoryConfig` + `Validate()`, and render the env vars (credentials via `secretKeyRef`) in the Helm chart and raw manifests. Cover the adapter with tests against fake SDK clients / an httptest endpoint — no real cloud calls in CI.

---

## 3. Frontend design patterns

### 3.1 Provider pattern — auth context

`OIDCProvider` (`frontend/src/auth/OIDCProvider.tsx`) wraps the entire app in `main.tsx`. It exposes `useAuth()` which returns `{ user, isAuthenticated, isLoading, login, logout }`.

The provider also exports module-level helpers (`getAccessToken`, `getUserManager`, `triggerSignIn`) so non-React code (the API client) can read auth state without going through React context.

### 3.2 Higher-order route guard — `ProtectedRoute`

Wraps any route subtree. Behaviour:

1. `isLoading` → show "Loading…" (avoids flash of sign-in prompt during token restoration).
2. Not authenticated → render the IdP "Sign In" gate.
3. Authenticated but missing required role → "Access Denied".
4. Otherwise render children.

Used in `App.tsx`: the `/callback` route is public; everything else is wrapped in `<ProtectedRoute>`.

### 3.3 Domain-isolated state — Zustand stores

Each domain has its own store: `pipelineStore`, `dockerStore`, `kubernetesStore`, `environmentStore`, `uiStore`. Stores never import each other. Cross-store coordination happens at the page or hook level.

**Why:** the pipeline editor, K8s panel, and Docker panel are independently developed and tested. Zustand's per-store subscriptions also prevent unrelated re-renders.

### 3.4 Custom hooks — encapsulated side effects

WebSocket subscriptions, polling, and animation effects are each hidden behind a hook (`useWebSocket`, `usePipelineExecution`, `useKubeWatch`). Components stay declarative.

### 3.5 Typed API client per domain

`frontend/src/api/client.ts` exports `get<T>`, `post<T>`, `put<T>`, `del<T>` with shared logic (Bearer token, 401 handling, JSON parsing). Each domain gets its own typed wrapper module (e.g., `api/pipelines.ts`).

**Why:** central place to add headers/error handling once; types stay close to the endpoint that returns them.

---

## 4. Authentication & Authorization design

### 4.1 OIDC PKCE flow (frontend → IdP → frontend)

```
                  ┌────────────┐
                  │  Browser   │
                  │ (Cooker UI)│
                  └──────┬─────┘
                         │ 1. click "Sign In"
                         │ 2. window.location → IdP /authorize?code_challenge=...
                         ▼
                  ┌────────────┐
                  │    IdP     │  (Google / KeepSave / Keycloak)
                  └──────┬─────┘
                         │ 3. user authenticates
                         │ 4. redirect → http://localhost:8080/callback?code=...
                         ▼
                  ┌────────────┐
                  │  Browser   │  Callback.tsx runs signinRedirectCallback()
                  │            │  5. POST IdP /token  (code + verifier)
                  │            │  6. receive JWT access_token + id_token
                  │            │  7. store in localStorage via UserManager
                  └──────┬─────┘
                         │ 8. navigate to "/"
                         ▼
                  ┌────────────┐
                  │  API call  │  api/client.ts attaches Authorization: Bearer <jwt>
                  └──────┬─────┘
                         ▼
                  ┌────────────┐
                  │  Backend   │  auth.Middleware verifies signature against IdP JWKS,
                  │ (Cooker)   │  extracts groups → roles, calls handler.
                  └────────────┘
```

Key properties:
- **PKCE, no client secret** — the browser is a public client. Even if the JS bundle is intercepted, no static secret leaks.
- **Backend never sees the auth code** — the code → token exchange happens in the browser. There is no `/auth/callback` server-side handler. The static-file `NoRoute` serves the SPA, which then completes the flow client-side.
- **Provider-agnostic** — `coreos/go-oidc/v3` discovers JWKS, issuer, and supported algorithms from `<issuer>/.well-known/openid-configuration`. Works with Google, Keycloak, KeepSave, Okta, Azure AD without code changes.
- **Auth-off mode** — `COOKER_OIDC_ENABLED=false` swaps in `devHandler`, which injects a static admin claim. UAT defaults to this so smoke tests don't need an IdP. Documented in [UAT.md](../guides/UAT.md#enabling-oidc-sign-in-for-uat) and [SECURITY.md](../../SECURITY.md).

### 4.2 RBAC

Four roles: **admin**, **operator**, **approver**, **viewer**.

Role assignment is **derived from the IdP's `groups` claim** by `MapGroupsToRoles` (`internal/auth/rbac.go:77`):

| Group | Role |
|---|---|
| `cooker-admins` | admin |
| `cooker-operators` | operator |
| `cooker-approvers` | approver |
| `cooker-viewers` | viewer |
| *(none of the above)* | viewer (least privilege fallback) |

`RequireRole(...)` is applied per-endpoint:

| Action | Required role |
|---|---|
| Read pipelines / runs / environments | any authenticated |
| Create / update pipeline | operator or admin |
| Delete pipeline | admin |
| Approve environment promotion | approver or admin |
| Reveal a secret value | admin |

**Trust boundary:** the **backend** is the security authority. The frontend `roles` array is for UI gating only; even if a malicious user spoofs the frontend role check, the backend re-derives roles from the signed JWT and rejects unauthorized API calls.

---

## 5. Configuration

All runtime config comes from environment variables, loaded once into `config.Config` in `internal/config/config.go`. Defaults are coded; validation is at boot.

| Concern | Variable | Default |
|---|---|---|
| HTTP port | `COOKER_PORT` | `8080` |
| Postgres | `DATABASE_URL` | empty → in-memory store |
| OIDC | `COOKER_OIDC_ENABLED` + 4 more | `false` (dev admin user) |
| OIDC group→role mapping | `COOKER_OIDC_GROUP_MAP` | empty → built-in `cooker-{admins,operators,approvers,viewers}` |
| OIDC step-up MFA | `COOKER_OIDC_MFA_ACR_VALUES` | empty → MFA gate disabled |
| CORS | `COOKER_ALLOWED_ORIGINS` | localhost dev defaults |
| Builder backend | `COOKER_BUILDER` | `noop` (also: `docker`, `kaniko`, `buildah`, `buildkit`) |
| Pusher backend | `COOKER_PUSHER` | `noop` (also: `docker`, `crane`) |
| Deployer backend | `COOKER_DEPLOYER` | `noop` (also: `kubectl`, `clientgo`) |
| Secrets backend | `COOKER_SECRETS_BACKEND` | `database` (also: `keepsave`, `vault`, `aws`, `gcp`) |
| Secret encryption key | `COOKER_SECRET_KEY` | empty → secrets API disabled |
| Rate-limit backend | `COOKER_RATE_LIMIT_BACKEND` | `memory` (also: `redis`) |
| WS-ticket backend | `COOKER_WS_TICKET_BACKEND` | `memory` (also: `redis`) |
| Prometheus `/metrics` | `COOKER_METRICS_ENABLED` | `false` |
| OpenTelemetry traces | `COOKER_TRACING_ENABLED` + `COOKER_OTLP_ENDPOINT` | `false` / empty |
| Registry prefix | `COOKER_REGISTRY` | empty |

UAT injects values via `.env.uat` (template at `.env.uat.example`). Helm values map 1:1 onto the same env vars in `deploy/helm/cooker/templates/deployment.yaml`.

**Why env vars only:** twelve-factor compliance; works the same in `docker run`, Compose, K8s, and CI.

---

## 6. Real-time updates

Pipeline runs, Docker builds, and K8s events all stream over WebSocket (`/ws/pipeline-run/:id`, `/ws/docker/build/:id`, `/ws/kubernetes/watch?namespace=...`).

Authentication today: WebSocket handlers are **outside** the OIDC middleware because browsers cannot attach an `Authorization` header to a WS upgrade. Channels are gated by UUIDv4 unguessability of the run/build ID. SECURITY.md proposes a future ticket-exchange flow (HTTP `POST /api/v1/ws-ticket` exchanges the JWT for a short-lived single-use token included in the WS query string). This is tracked as a follow-up.

---

## 7. Testing strategy

| Layer | Style | Where |
|---|---|---|
| **Pure functions** | Table-driven Go tests | `*_test.go` next to source (e.g., `rbac_test.go`, `oidc_test.go`) |
| **Service / handler** | In-memory store + httptest | `internal/handler/*_test.go` |
| **DAG runner** | Standalone, no other deps | `pkg/dagrunner/*_test.go` |
| **Frontend types** | TypeScript compile-time | `tsc --noEmit` in CI |
| **Frontend logic** | Vitest | `frontend/**/*.test.ts(x)` |
| **End-to-end** | UAT compose stack | `make uat-up` + manual playbook in `docs/UAT.md` |

Race detector is on in CI: `go test ./... -race`. `go vet ./...` runs before tests.

**Convention:** every `internal/<package>/<file>.go` should ship with `<file>_test.go` for any non-trivial logic. New strategy implementations (a new builder/pusher/deployer) must include a smoke test against the interface contract.

---

## 8. CI/CD

Workflow at `.github/workflows/ci.yml`:

| Job | Steps |
|---|---|
| `backend` | checkout → setup Go 1.22 → `go build` → `go vet` → `go test -race` (against a Postgres service container) |
| `frontend` | checkout → setup Node 20 → `npm ci` → `npm run lint` → `npm run build` (includes `tsc -b`) → `npm test` |
| `docker` | needs both above → `docker build -f deploy/docker/Dockerfile .` |

The Dockerfile is the single source of truth for production: same multi-stage build runs in CI, in `make docker-build`, and in `make uat-up`. There is no separate "production build" step.

---

## 9. Code organization conventions

### Backend (Go)

```
backend/
├── cmd/cooker/         entry point — only main.go, calls config.Load + server.New + Run
├── internal/           private packages, NEVER imported by other modules
│   ├── auth/           OIDC middleware, RBAC
│   ├── builder/        image builders (strategy pattern)
│   ├── buildplan/      build-time planning
│   ├── config/         env-var config loading
│   ├── crypto/         AES-GCM codec for secrets-at-rest
│   ├── deployer/       deploy adapters (kubectl, client-go)
│   ├── deploytarget/   per-target adapters (cluster, cloud-run, ...)
│   ├── gitops/         git ops helpers
│   ├── handler/        thin HTTP layer
│   ├── model/          domain types — no behaviour, just data
│   ├── oci/            OCI image-spec types and helpers
│   ├── pusher/         registry push adapters (strategy)
│   ├── server/         HTTP/WS routing and dependency wiring
│   ├── service/        business logic — does NOT import handler
│   ├── source/github/  Git source providers
│   ├── store/          persistence interfaces + memory + postgres impls
│   └── transport/tsnet/ Tailscale-style overlay (planned)
└── pkg/                public packages — reusable, importable
    ├── dagrunner/      generic DAG executor (used by service.Executor)
    └── ociutil/        higher-level OCI helpers
```

Rules:
- **`internal/` is private**; nothing outside `backend/` may import it.
- **`pkg/` is public**; treat it as you would any open-source library — stable APIs, doc comments, tests.
- **No circular imports** — handlers depend on services, services depend on stores/strategies; never the reverse.
- **No business logic in handlers.** Handlers parse, call a service, return a JSON response.
- **No HTTP types in services.** Services take and return domain types from `internal/model`.
- **Errors are values.** Wrap with `fmt.Errorf("layer: %w", err)`. The store package exposes a typed `ErrNotFound` for callers to check via `errors.Is`.

### Frontend (TypeScript)

```
frontend/src/
├── App.tsx            top-level routes
├── main.tsx           entry — wraps App in BrowserRouter + OIDCProvider
├── api/               typed fetch wrappers — one module per backend domain
├── auth/              OIDCProvider, ProtectedRoute, Callback
├── components/        presentational + container components, organised by feature
├── hooks/             reusable hooks (useWebSocket, usePipelineExecution, ...)
├── pages/             route-level components
├── stores/            Zustand stores — one per domain
├── types/             shared TypeScript types
├── utils/             pure helpers
└── vite-env.d.ts      VITE_OIDC_* and other build-time env types
```

Rules:
- **No backend URLs in components.** All HTTP goes through `api/`.
- **No `localStorage` outside `auth/`.** Token storage is owned by `oidc-client-ts`; everything else uses Zustand.
- **No `any`.** TypeScript `strict` is on; `tsc --noEmit` runs in CI.

---

## 10. Coding style

### Go
- `gofmt` enforced (CI step planned).
- Package-level doc comment on every package (`// Package foo ...`).
- Doc comments on every exported symbol.
- Errors wrapped with package-prefixed context: `"oidc: discover provider %q: %w"`.
- `panic` only at startup for programmer errors (e.g., `deploytarget.Register` on duplicate Kind). Never for control flow.
- Comments explain **why**, not what. The code shows what.

### TypeScript
- `strict: true`, `noUnusedLocals: true`, `noUnusedParameters: true`.
- No semicolons in `.tsx` (matches existing style); `tsc` enforces consistency.
- Functional components only; no class components.
- Props typed inline or via local `interface`.

### Documentation
- Markdown lives under `docs/`.
- READMEs are short — link to docs/, don't inline.
- Every long-lived flag, config, or known limitation gets a paragraph in either `docs/UAT.md` or `SECURITY.md`.

---

## 11. Adding a new feature — checklist

For a new backend endpoint:

1. Add the model in `internal/model/`.
2. Add the store interface method (+ memory + postgres impls + migration).
3. Add the service function (business logic).
4. Add the handler function and route in `internal/server/router.go`.
5. Choose the right `RequireRole(...)` for the route.
6. Add tests at every layer.
7. Document the endpoint in `README.md` if user-facing.

For a new pluggable backend (e.g., a new pusher):

1. Implement the interface in a new file under `internal/<kind>/`.
2. Add the constructor case to the `select<Kind>` switch in `server.go`.
3. Document the env-var value in `.env.uat.example` and `docs/UAT.md`.
4. Add a contract test against the interface.

For a new *optional capability* on an existing adapter interface (e.g., the canary `WeightedDeployer`):

1. Declare a narrow interface that embeds the base (`type WeightedDeployer interface { Deployer; DeployWeighted(...) }`) plus a typed sentinel error for "unsupported".
2. Implement it only on the adapters that can honour it; assert `var _ Capability = (*Impl)(nil)` so a missing method is a compile error there.
3. In `server.go`, type-assert the selected adapter for the capability and pass the result (possibly nil) to the consuming service.
4. The service checks for nil / asserts and returns the typed "unsupported" error, which the handler maps to 422.

---

## 12. References

- [architecture.md](architecture.md) — system architecture and component map
- [../SECURITY.md](../../SECURITY.md) — security policy and threat model
- [UAT.md](../guides/UAT.md) — UAT runbook and OIDC enablement
- [OCI image-spec v1.1](https://github.com/opencontainers/image-spec)
- [OCI distribution-spec v1.1](https://github.com/opencontainers/distribution-spec)
- [OAuth 2.0 PKCE — RFC 7636](https://datatracker.ietf.org/doc/html/rfc7636)
- [OpenID Connect Core 1.0](https://openid.net/specs/openid-connect-core-1_0.html)
