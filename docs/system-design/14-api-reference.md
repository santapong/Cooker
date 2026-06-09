# 14 · API Reference

> **Purpose:** the complete HTTP + WebSocket endpoint reference, generated from the authoritative route
> table in `backend/internal/server/router.go`. **See also:** [02-backend.md](02-backend.md) for "how
> the API is managed" (middleware, idempotency, rate limiting, error envelope).

> **On the OpenAPI spec:** [`../openapi.yaml`](../openapi.yaml) is a **hand-maintained** OpenAPI 3.1
> spec that now covers **all** of these routes (kept in sync with `router.go`). It is not
> auto-generated; the `swaggo/swag`-generated `backend/docs/api/swagger.*` annotates flagship
> endpoints only. **`router.go` remains the source of truth** and both this page and `openapi.yaml`
> mirror it.

## Conventions

- **Base path:** all authenticated endpoints are under `/api/v1`. WebSocket endpoints are under `/ws`.
  Webhooks and health/version/metrics are at the root.
- **Auth column:**
  | Symbol | Meaning |
  |---|---|
  | 🔓 | Unauthenticated (webhooks self-authenticate via HMAC) |
  | 🔑 | Valid Bearer token required (any authenticated user / `viewer`+) |
  | ✏️ | **writeRole** — `operator` or `admin` |
  | 🛡️ | **admin + MFA** (`adminRole` + `RequireMFA`) |
  | 🛠️ | **admin** only (no MFA) |
- **Extra gates:** ⏱️ = rate-limited (expensive route) · ♻️ = honors `Idempotency-Key` · 🔐 = additional
  fine-grained permission check (`RequirePermission`) · 🚦 = governance admission hook · ‡ = role
  enforced **in-handler**, not by route middleware.
- **Error envelope:** failures return a flat `{"error": "<message>"}`. `404` for not-found, `409`
  `{"error":"version conflict; refetch and retry"}` for optimistic-concurrency conflicts, `429` +
  `Retry-After: 60` when rate-limited, `503` when a backend (secrets/store) is unconfigured.
- **Audit:** every `/api/v1` route passes through the audit middleware.

---

## Health, version & metrics

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/health` | 🔓 | Liveness (alias) |
| GET | `/health/live` | 🔓 | Liveness probe |
| GET | `/health/ready` | 🔓 | Readiness probe (checks store/redis) |
| GET | `/version` | 🔓 | Build version / SHA / time |
| GET | `/metrics` | 🔓 | Prometheus metrics (currently unauthenticated) |

## Authentication

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/api/v1/auth/methods` | 🔓 | Which auth modes are enabled (OIDC / local) |
| GET | `/api/v1/capabilities` | 🔑 | Optional-feature discovery (`{aiTriage}`) |
| POST | `/api/v1/auth/local/signup` | 🔓 | Local-auth signup (only if local auth enabled) |
| POST | `/api/v1/auth/local/signin` | 🔓 | Local-auth signin → JWT |
| GET | `/api/v1/auth/local/me` | 🔑 | Current local-auth user (only if local auth enabled) |

## Git provider webhooks

All 🔓 but **HMAC/token-verified** per provider; all ♻️ idempotent (`X-GitHub-Delivery` fallback).

| Method | Path | Description |
|---|---|---|
| POST | `/webhooks/github` | GitHub push/PR events (`X-Hub-Signature-256`) |
| POST | `/webhooks/gitlab` | GitLab events (`X-Gitlab-Token`) |
| POST | `/webhooks/bitbucket` | Bitbucket events |
| POST | `/webhooks/gitea` | Gitea events |

## Pipelines & runs

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/api/v1/pipelines` | 🔑 | List pipelines |
| POST | `/api/v1/pipelines` | ✏️ | Create pipeline |
| GET | `/api/v1/pipelines/:id` | 🔑 | Get one pipeline |
| PUT | `/api/v1/pipelines/:id` | ✏️ | Update pipeline (optimistic-concurrency) |
| DELETE | `/api/v1/pipelines/:id` | 🛡️ | Delete pipeline |
| POST | `/api/v1/pipelines/:id/validate` | 🔑 | Validate the DAG (cycle/type check) |
| POST | `/api/v1/pipelines/:id/run` | ✏️ ⏱️ ♻️ 🔐 | Start a run (returns 202 + runId) |
| GET | `/api/v1/pipelines/:id/runs` | 🔑 | List runs, newest first. `?limit=` (default 50, max 200) + `?offset=` paginate; per-stage `logs` omitted from list rows — fetch a single run for logs |
| GET | `/api/v1/pipelines/:id/runs/:runId` | 🔑 | Get a run (with stage runs) |
| POST | `/api/v1/pipelines/:id/runs/:runId/cancel` | ✏️ | Cancel a running run |
| GET | `/api/v1/pipelines/:id/runs/:runId/logs/:stageId` | 🔑 | Final logs for a stage (REST) |
| GET | `/api/v1/pipelines/:id/runs/:runId/diff` | 🔑 | Diff a run vs last-success (or `?against=<runId>`) |
| POST | `/api/v1/pipelines/:id/runs/:runId/stages/:stageId/triage` | ✏️ ⏱️ | AI failure triage (opt-in; advisory only; 503 when disabled) |
| GET | `/api/v1/pipelines/:id/analytics` | 🔑 | Stage-duration + success-rate analytics (`?runs=`, default 30) |
| POST | `/api/v1/pipelines/from-template/:id` | ✏️ | Create a pipeline from a catalog template |
| POST | `/api/v1/pipelines/:id/runs/:runId/promote` | ✏️ | Promote a run to the next environment |
| POST | `/api/v1/pipelines/:id/runs/:runId/approve` | 🔑 ‡ | Approve a manual promotion gate. **No route-level role gate**; the handler requires **admin or approver** via `CanApprovePromotion` |
| GET | `/api/v1/pipelines/:id/runs/:runId/env-status` | 🔑 | Per-environment status of a run |

## Apps

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/api/v1/apps` | 🔑 | List apps |
| POST | `/api/v1/apps` | ✏️ | Create app |
| POST | `/api/v1/apps/detect-build` | ✏️ ⏱️ | Pre-import build detection: shallow-clone `{githubRepo, branch}` → `{plan, suggestedRecipe}` (New-App wizard) |
| GET | `/api/v1/apps/:id` | 🔑 | Get app |
| PUT | `/api/v1/apps/:id` | ✏️ | Update app |
| DELETE | `/api/v1/apps/:id` | 🛡️ | Delete app |
| POST | `/api/v1/apps/:id/deploy` | ✏️ ⏱️ ♻️ 🚦 | Build → push → deploy the app |
| GET | `/api/v1/apps/:id/deploys` | 🔑 | Deploy/rollback history (newest first, `?limit=`) |
| POST | `/api/v1/apps/:id/rollback` | ✏️ ⏱️ ♻️ 🚦 | Re-deploy a previous image (deploy-only; k8s targets) |
| GET | `/api/v1/apps/:id/drift` | ✏️ | Live-cluster image vs last shipped (on-demand) |
| PUT | `/api/v1/apps/:id/webhook` | 🛡️ 🔐 | Set the app's webhook secret |

## Environments & secrets

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/api/v1/environments` | 🔑 | List environments |
| POST | `/api/v1/environments` | ✏️ | Create environment |
| PUT | `/api/v1/environments/:id` | ✏️ | Update environment |
| DELETE | `/api/v1/environments/:id` | 🛡️ | Delete environment |
| GET | `/api/v1/environments/:id/secrets/:key` | 🛡️ 🔐 | Reveal a secret value |
| PUT | `/api/v1/environments/:id/secrets/:key` | 🛡️ | Set a secret |
| DELETE | `/api/v1/environments/:id/secrets/:key` | 🛡️ | Delete a secret |
| POST | `/api/v1/environments/:id/secrets/promote` | 🛡️ | Promote secrets to the next env (KeepSave; DB backend → 501) |

## Docker

> **Note:** image/container **list/inspect** handlers are placeholder stubs today (return empty/sample
> data). The real Docker work happens in the build/push/deploy executor path — see
> [05-extension-points.md](05-extension-points.md).

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/api/v1/docker/images` | 🔑 | List images (stub) |
| GET | `/api/v1/docker/images/:id` | 🔑 | Inspect image (stub) |
| POST | `/api/v1/docker/images/build` | ✏️ ⏱️ | Build an image |
| DELETE | `/api/v1/docker/images/:id` | 🛠️ | Remove an image |
| GET | `/api/v1/docker/containers` | 🔑 | List containers |
| POST | `/api/v1/docker/containers` | ✏️ | Create a container |
| POST | `/api/v1/docker/containers/:id/stop` | ✏️ | Stop a container |
| DELETE | `/api/v1/docker/containers/:id` | 🛠️ | Remove a container |
| GET | `/api/v1/docker/containers/:id/logs` | 🔑 | Container logs |
| POST | `/api/v1/docker/compose/parse` | 🔑 | Parse a compose file → topology graph |
| PUT | `/api/v1/docker/compose/services/:name` | ✏️ | Update a compose service |
| GET | `/api/v1/docker/networks` | 🔑 | List networks |
| POST | `/api/v1/docker/networks` | ✏️ | Create network |
| GET | `/api/v1/docker/networks/:id` | 🔑 | Get network |
| DELETE | `/api/v1/docker/networks/:id` | 🛠️ | Delete network |
| POST | `/api/v1/docker/networks/:id/connect` | ✏️ | Connect a container to a network |
| GET | `/api/v1/docker/volumes` | 🔑 | List volumes |
| POST | `/api/v1/docker/volumes` | ✏️ | Create volume |
| GET | `/api/v1/docker/volumes/:name` | 🔑 | Get volume |
| DELETE | `/api/v1/docker/volumes/:name` | 🛠️ | Delete volume |

## Kubernetes

> **Note:** these handlers are placeholder stubs today (return empty/sample data). Actual cluster
> deploys go through the `clientgo`/`kubectl` deployer in the executor path; live watch streams over the
> WebSocket channel below.

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/api/v1/kubernetes/namespaces` | 🔑 | List namespaces (stub) |
| GET | `/api/v1/kubernetes/workloads` | 🔑 | List workloads `?namespace=` (stub) |
| GET | `/api/v1/kubernetes/workloads/:ns/:kind/:name` | 🔑 | Get a workload (stub) |
| POST | `/api/v1/kubernetes/workloads/:ns/:kind/:name/scale` | ✏️ | Scale a workload |
| POST | `/api/v1/kubernetes/workloads/:ns/:kind/:name/restart` | ✏️ | Rolling restart |
| GET | `/api/v1/kubernetes/pods/:ns/:name/logs` | 🔑 | Pod logs |
| POST | `/api/v1/kubernetes/apply` | ✏️ | Apply a manifest |
| DELETE | `/api/v1/kubernetes/:ns/:kind/:name` | 🛠️ | Delete a resource |

## Registry

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/api/v1/registry/repositories` | 🔑 | List repositories |
| GET | `/api/v1/registry/:name/tags` | 🔑 | List tags for a repo |
| GET | `/api/v1/registry/:name/manifests/:ref` | 🔑 | Get a manifest |
| GET | `/api/v1/registry/:name/referrers/:digest` | 🔑 | OCI referrers for a digest |
| POST | `/api/v1/registry/push` | ✏️ | Push an image |
| POST | `/api/v1/registry/pull` | ✏️ | Pull an image |

## Hosts

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/api/v1/hosts` | 🔑 | List hosts |
| POST | `/api/v1/hosts` | ✏️ | Register a host |
| GET | `/api/v1/hosts/:id` | 🔑 | Get host |
| PUT | `/api/v1/hosts/:id` | ✏️ | Update host |
| DELETE | `/api/v1/hosts/:id` | 🛡️ | Delete host |

## Templates (catalog)

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/api/v1/templates` | 🔑 | List pipeline templates |
| GET | `/api/v1/templates/:id` | 🔑 | Get a template |

## Settings

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/api/v1/settings/registries` | 🔑 | List registry configs |
| POST | `/api/v1/settings/registries` | 🛠️ | Add registry config |
| DELETE | `/api/v1/settings/registries/:id` | 🛠️ | Delete registry config |
| GET | `/api/v1/settings/clusters` | 🔑 | List cluster configs |
| POST | `/api/v1/settings/clusters` | 🛠️ | Add cluster config |

## Admin catalogs

All under `/api/v1/admin`, all 🛡️ (admin + MFA).

| Method | Path | Description |
|---|---|---|
| POST | `/api/v1/admin/templates` | Create template |
| PUT | `/api/v1/admin/templates/:id` | Update template |
| DELETE | `/api/v1/admin/templates/:id` | Delete template |
| GET | `/api/v1/admin/schedules` | List schedules |
| GET | `/api/v1/admin/schedules/:id` | Get schedule |
| POST | `/api/v1/admin/schedules` | Create schedule |
| PUT | `/api/v1/admin/schedules/:id` | Update schedule |
| DELETE | `/api/v1/admin/schedules/:id` | Delete schedule |
| GET | `/api/v1/admin/notification-targets` | List notification targets |
| GET | `/api/v1/admin/notification-targets/:id` | Get notification target |
| POST | `/api/v1/admin/notification-targets` | Create notification target |
| PUT | `/api/v1/admin/notification-targets/:id` | Update notification target |
| DELETE | `/api/v1/admin/notification-targets/:id` | Delete notification target |

## Real-time (WebSocket)

First obtain a single-use 60s ticket, then connect with `?ticket=<value>` (see
[07-realtime-and-concurrency.md](07-realtime-and-concurrency.md)).

| Method | Path | Auth | Description |
|---|---|---|---|
| POST | `/api/v1/ws-tickets` | 🔑 | Issue a single-use WebSocket ticket (`{ticket, expires_at}`) |
| GET | `/ws/pipeline-run/:runId` | 🎫 ticket | Stream a pipeline run's status |
| GET | `/ws/app-run/:runId` | 🎫 ticket | Stream an app-deploy run |
| GET | `/ws/docker/build/:buildId` | 🎫 ticket | Stream a standalone image build |
| GET | `/ws/runs/:runId/stages/:stageId/logs` | 🎫 ticket | Stream live stage logs |
| GET | `/ws/kubernetes/watch?namespace=&resource=` | 🎫 ticket | Stream K8s resource changes |

## Static / SPA

| Method | Path | Description |
|---|---|---|
| GET | `/assets/*` | Embedded SPA static assets |
| (any) | `*` (NoRoute) | Serves the SPA `index.html` (client-side routing) |

---

## Summary by area

| Area | Routes | Notes |
|---|---|---|
| Health/version/metrics | 5 | All public |
| Auth | 4 | OIDC + optional local |
| Webhooks | 4 | HMAC-verified, idempotent |
| Pipelines & runs | 18 | Core surface + run diff + triage + analytics |
| Apps | 11 | Build→push→deploy + webhook + detect-build + history/rollback/drift |
| Environments & secrets | 8 | Secret reveal/promote gated 🛡️ |
| Docker | 20 | List/inspect are stubs |
| Kubernetes | 8 | Stubs; real deploy via executor |
| Registry | 6 | OCI distribution reads + push/pull |
| Hosts | 5 | |
| Templates + Settings | 7 | |
| Admin catalogs | 13 | All 🛡️ |
| WebSocket | 6 | Ticket-gated |

---

> _Verified against `main` @ `dd93402` on 2026-05-30. If you change the described behaviour, update this chapter in the same PR._
