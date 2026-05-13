# REST API reference

Cooker's API is documented as Go doc-comments and rendered as OpenAPI under `backend/docs/api/swagger.yaml`. There's also a hand-curated [`docs/openapi.yaml`](../../openapi.yaml). This page is the operator-readable cross-reference.

Routes are registered in `backend/internal/server/router.go`. All `/api/v1/*` routes require authentication except `/api/v1/auth/local/{signup,signin}` and `/api/v1/auth/methods`.

## Conventions

- **Base path:** `/api/v1`
- **Auth:** `Authorization: Bearer <jwt>` for OIDC and local auth.
- **Content type:** `application/json`.
- **Error format:** `{"error": "<message>"}` or `{"error": "<message>", "details": {...}}`.

## Status codes

| Code | When |
|---|---|
| 200 | OK with body. |
| 201 | Created with body. |
| 202 | Accepted (async work spawned; webhook receivers, deploy triggers). |
| 204 | Deleted; no body. |
| 400 | Validation error. |
| 401 | Missing or invalid auth. |
| 403 | Authenticated but insufficient role, OR MFA required (`{"error":"mfa_required","acr_values":[...]}`). |
| 404 | Resource doesn't exist. |
| 409 | Optimistic-concurrency conflict (stale `version`). |
| 429 | Rate-limited (`Retry-After` header set). |
| 501 | Backend doesn't implement the requested operation (e.g. promote on a non-Promoter secrets backend). |
| 503 | A dependency is unavailable (codec uninitialised, etc.). |

## Pipelines

| Method | Path | Role | Notes |
|---|---|---|---|
| GET | `/api/v1/pipelines` | any authed | List. |
| POST | `/api/v1/pipelines` | operator+ | Create. |
| GET | `/api/v1/pipelines/:id` | any authed | Get. |
| PUT | `/api/v1/pipelines/:id` | operator+ | Update (optimistic concurrency). |
| DELETE | `/api/v1/pipelines/:id` | admin (MFA) | Delete. |
| POST | `/api/v1/pipelines/:id/validate` | any authed | DAG validation; returns errors as 200 body. |
| POST | `/api/v1/pipelines/:id/run` | operator+ | Start a run. Rate-limited. Honours `Idempotency-Key`. |
| GET | `/api/v1/pipelines/:id/runs` | any authed | List runs. **Unpaginated** — see roadmap D2. |
| GET | `/api/v1/pipelines/:id/runs/:runId` | any authed | Get run. |
| POST | `/api/v1/pipelines/:id/runs/:runId/cancel` | operator+ | Cancel an in-flight run. |
| GET | `/api/v1/pipelines/:id/runs/:runId/logs/:stageId` | any authed | Stage log capture. |
| POST | `/api/v1/pipelines/:id/runs/:runId/promote` | operator+ | Promote to next env. |
| POST | `/api/v1/pipelines/:id/runs/:runId/approve` | approver/admin | Approve a manual promotion. |
| GET | `/api/v1/pipelines/:id/runs/:runId/env-status` | any authed | Per-env status. |

## Apps

| Method | Path | Role | Notes |
|---|---|---|---|
| GET | `/api/v1/apps` | any authed | List. |
| POST | `/api/v1/apps` | operator+ | Create. |
| GET | `/api/v1/apps/:id` | any authed | Get. |
| PUT | `/api/v1/apps/:id` | operator+ | Update (optimistic concurrency). |
| DELETE | `/api/v1/apps/:id` | admin (MFA) | Delete. |
| POST | `/api/v1/apps/:id/deploy` | operator+ | Trigger a Clone-Build-Push-Deploy run. Rate-limited. Honours `Idempotency-Key`. |
| PUT | `/api/v1/apps/:id/webhook` | admin (MFA) | Set / rotate the GitHub webhook secret. |

## Environments

| Method | Path | Role | Notes |
|---|---|---|---|
| GET | `/api/v1/environments` | any authed | List. Secret values are NOT returned; `secretKeys` is. |
| POST | `/api/v1/environments` | operator+ | Create. |
| PUT | `/api/v1/environments/:id` | operator+ | Update (optimistic concurrency). |
| DELETE | `/api/v1/environments/:id` | admin (MFA) | Delete. |
| GET | `/api/v1/environments/:id/secrets/:key` | admin (MFA) | Reveal plaintext. |
| PUT | `/api/v1/environments/:id/secrets/:key` | admin (MFA) | Set / update. Body: `{"value":"..."}`. |
| DELETE | `/api/v1/environments/:id/secrets/:key` | admin (MFA) | Delete. |
| POST | `/api/v1/environments/:id/secrets/promote` | admin (MFA) | Copy keys to another env. Body: `{"toEnvironmentId":"...","keys":["k1","k2"]}`. Returns 501 if backend has no Promoter. |

## Hosts

| Method | Path | Role | Notes |
|---|---|---|---|
| GET | `/api/v1/hosts` | any authed | List. |
| POST | `/api/v1/hosts` | operator+ | Create. |
| GET | `/api/v1/hosts/:id` | any authed | Get. |
| PUT | `/api/v1/hosts/:id` | operator+ | Update. |
| DELETE | `/api/v1/hosts/:id` | admin (MFA) | Delete. |

> **Partial.** The Hosts page in the frontend is not yet a menu item; the API works. See [Hosts and deploy targets](../concepts/hosts-and-targets.md).

## Docker

| Method | Path | Role | Notes |
|---|---|---|---|
| GET | `/api/v1/docker/images` | any authed | **Stubbed.** Returns empty list. |
| GET | `/api/v1/docker/images/:id` | any authed | **Stubbed.** |
| POST | `/api/v1/docker/images/build` | operator+ | Stubbed; rate-limited; placeholder for the manual-build path. |
| DELETE | `/api/v1/docker/images/:id` | admin | **Stubbed.** |
| GET | `/api/v1/docker/containers` | any authed | **Stubbed.** |
| POST | `/api/v1/docker/containers` | operator+ | **Stubbed.** |
| POST | `/api/v1/docker/containers/:id/stop` | operator+ | **Stubbed.** |
| DELETE | `/api/v1/docker/containers/:id` | admin | **Stubbed.** |
| GET | `/api/v1/docker/containers/:id/logs` | any authed | **Stubbed.** |
| POST | `/api/v1/docker/compose/parse` | any authed | Parse a docker-compose.yml. |
| PUT | `/api/v1/docker/compose/services/:name` | operator+ | Update a compose service. |
| (networks / volumes) | various | as above | Return `501 Not Implemented` per `S26-05-29` style. |

> **All Docker-resource handlers are stubs** in this release. They exist for the SPA's panel views; real implementations are roadmap items.

## Registry

| Method | Path | Role | Notes |
|---|---|---|---|
| GET | `/api/v1/registry/repositories` | any authed | **Stubbed.** Returns empty list. |
| GET | `/api/v1/registry/:name/tags` | any authed | List tags via go-containerregistry. |
| GET | `/api/v1/registry/:name/manifests/:ref` | any authed | Get manifest. |
| POST | `/api/v1/registry/push` | operator+ | **Stub** (no validation today; `S26-05-29`). |
| POST | `/api/v1/registry/pull` | operator+ | **Stub.** |
| GET | `/api/v1/registry/:name/referrers/:digest` | any authed | OCI referrers API. |

## Kubernetes

| Method | Path | Role | Notes |
|---|---|---|---|
| GET | `/api/v1/kubernetes/namespaces` | any authed | List. |
| GET | `/api/v1/kubernetes/workloads` | any authed | List Deployments/StatefulSets/DaemonSets. |
| GET | `/api/v1/kubernetes/workloads/:ns/:kind/:name` | any authed | Get. |
| POST | `/api/v1/kubernetes/workloads/:ns/:kind/:name/scale` | operator+ | Body: `{"replicas":N}`. |
| POST | `/api/v1/kubernetes/workloads/:ns/:kind/:name/restart` | operator+ | `kubectl rollout restart` equivalent. |
| GET | `/api/v1/kubernetes/pods/:ns/:name/logs` | any authed | Pod logs. |
| POST | `/api/v1/kubernetes/apply` | operator+ | Apply a manifest. |
| DELETE | `/api/v1/kubernetes/:ns/:kind/:name` | admin | Delete. |

> **Partial.** K8s handlers depend on a working kubeconfig / in-cluster credentials and the `client-go` runtime. Some are stubs in this release — `kubernetes` namespaces/workloads list returns empty unless your deployer is wired.

## Settings

| Method | Path | Role | Notes |
|---|---|---|---|
| GET | `/api/v1/settings/registries` | any authed | List configured registries (credentials redacted). |
| POST | `/api/v1/settings/registries` | admin | Add a registry. |
| DELETE | `/api/v1/settings/registries/:id` | admin | Delete. |
| GET | `/api/v1/settings/clusters` | any authed | List configured clusters. |
| POST | `/api/v1/settings/clusters` | admin | Add a cluster (kubeconfig sealed via Codec). |

## Auth

| Method | Path | Role | Notes |
|---|---|---|---|
| GET | `/api/v1/auth/methods` | public | Probe; tells the SPA which forms to render. |
| POST | `/api/v1/auth/local/signup` | public | First user becomes admin. Disabled when `COOKER_LOCAL_AUTH_ALLOW_SIGNUP=false`. |
| POST | `/api/v1/auth/local/signin` | public | Returns HS256 JWT. **Not rate-limited at app layer** (`S26-05-02`). |
| GET | `/api/v1/auth/local/me` | any authed (local OR oidc) | Returns the authenticated user. |

## WebSocket tickets

| Method | Path | Role | Notes |
|---|---|---|---|
| POST | `/api/v1/ws-tickets` | any authed | Returns `{ticket, expires_at}`. Single-use, 60s TTL. |

## WebSocket endpoints

All require a `?ticket=<value>` query parameter.

| Path | What |
|---|---|
| `/ws/pipeline-run/:runId` | Pipeline-run status changes. |
| `/ws/app-run/:runId` | App-deploy run events. |
| `/ws/docker/build/:buildId` | Live build output. |
| `/ws/runs/:runId/stages/:stageId/logs` | Per-stage live log tail. |
| `/ws/kubernetes/watch?namespace=...&resource=...` | K8s watch fan-out. |

## Webhooks

| Method | Path | Auth | Notes |
|---|---|---|---|
| POST | `/webhooks/github` | HMAC SHA-256 | Unauthenticated by JWT — HMAC is the auth. **Not rate-limited at app layer** (`S26-05-22`). Honours `X-GitHub-Delivery` for idempotency. |

See [GitHub webhooks](../guides/github-webhooks.md) and [Reference: Webhooks](webhooks.md).

## Health

| Method | Path | Auth | Notes |
|---|---|---|---|
| GET | `/health` | none | Legacy. Equivalent to `/health/live`. |
| GET | `/health/live` | none | Liveness probe. |
| GET | `/health/ready` | none | Readiness probe (store ping). |

## Stability

| Tier | Behaviour |
|---|---|
| **Stable** | Pipelines, Apps, Environments, Auth, Health, Webhooks. |
| **Experimental** | Hosts, Settings (chart UX is rudimentary). |
| **Stub** | Docker (images/containers/networks/volumes), some Registry methods, some Kubernetes methods. Return empty bodies or 501; do not rely on them. |

Tier changes are documented in `CHANGELOG.md`.

## OpenAPI

- `docs/openapi.yaml` — hand-curated sketch.
- `backend/docs/api/swagger.yaml` — generated from doc comments via `make swagger`.

The generated version is more complete and is the truth-source when they disagree.

## Cross-references

- **[Webhooks](webhooks.md)** — payload format.
- **[CLI](cli.md)** — what's wired today (very little) and what's roadmap.
- **[`backend/internal/server/router.go`](https://github.com/santapong/cooker/blob/main/backend/internal/server/router.go)** — the source.
