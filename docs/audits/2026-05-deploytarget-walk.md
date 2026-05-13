# DeployTarget Adapter Walk — 2026-05 W3

> Status: **research deliverable; E-2, F-2, F-3, R-2 closed by PR `claude/w4-multicloud-bug-bundle`.**
> Original research branch: `claude/w3-research-deploytarget-walk`.
> Fix branch: `claude/w4-multicloud-bug-bundle`.
> Scope: `backend/internal/deploytarget/{cloudrun,ecs,flyio,render}/` plus the Kubernetes deploy
> path in `backend/internal/deployer/clientgo.go` (no `deploytarget/kubernetes/` subdirectory
> exists; `DeployTargetKubernetes` is served by `deployer.ClientGo` wired in
> `service/app_deployer.go:135`).
> Cross-references:
> - `dag-performance.md` §4 High finding #2 (LogWriter) — adapter-side feed for T2.
> - `dag-adaptation-2026.md` §6 T2 — LogWriter for push + deploy.
> - `2026-05-cache-plumb-sketch.md` — cache findings already covered there; not re-flagged.

---

## 1. Kubernetes (`deployer/clientgo.go`)

### K-1 — No LogWriter; K8s deploy events invisible to the run page

**Severity: High | File:line:** `clientgo.go:86`, `deployer/deployer.go:26-41`

`deployer.Request` has no `LogWriter io.Writer` field. `ClientGo.Deploy` appends applied
resource names to `Result.AppliedResources` (`clientgo.go:133`) but writes nothing to any
writer. Pod events (`ImagePullBackOff`, rollout progress, readiness failures) are invisible.
This is the deploy-half of `dag-performance.md` High #2 and the direct target of T2 in
`dag-adaptation-2026.md` §6 T2. Adding `LogWriter` to `deployer.Request` and emitting one
line per applied resource is the fix. Effort: ~2 hours, includes executor wiring analogous
to `executor.go:300-329`.

### K-2 — Discovery cache is per-adapter-instance, not shared

**Severity: Medium | File:line:** `clientgo.go:79`

`memcache.NewMemCacheClient(disc)` is constructed inside `sync.Once`, so it is shared within a
single `ClientGo` but not across instances. If the server creates multiple `ClientGo` values
(e.g. one per deploy call, which is how the current `server.go` `selectDeployer` path works),
each instance pays fresh HTTPS discovery on first `Deploy`. Fix: promote to a package-level
singleton keyed by kubeconfig path. Effort: ~1 hour.

### K-3 — Error wrapping uses `%v` instead of `%w` in `ensureClients`

**Severity: Low | File:line:** `clientgo.go:64, 75`

`fmt.Errorf("%w: rest config: %v", ErrUnavailable, err)` — the inner error is formatted with
`%v`, not `%w`, so `errors.Is(err, someK8sError)` cannot traverse the chain. CLAUDE.md
convention is `fmt.Errorf("<adapter>: <op>: %w", err)` throughout. Trivial fix.

---

## 2. Cloud Run (`deploytarget/cloudrun/cloudrun.go`)

### CR-1 — New gRPC client created on every call; no client reuse

**Severity: High | File:line:** `cloudrun.go:51, 102, 127, 133`

`Deploy`, `Status`, and `Rollback` each call `run.NewServicesClient(ctx)` or
`run.NewRevisionsClient(ctx)` and `defer c.Close()`. Cloud Run's Go client wraps a long-lived
gRPC connection pool; creating one per call pays TLS handshake + HTTP/2 stream setup every
operation. Under `AppHealthChecker`'s 30-second poll interval, a single Cloud Run target
creates ~120 gRPC connections per hour. Fix: promote clients to `Target` fields, initialize
via `sync.Once`. Effort: ~1 hour.

### CR-2 — No LogWriter; LRO wait is silent

**Severity: High | File:line:** `cloudrun.go:47`

`Deploy` calls `op.Wait(ctx)` — which can block for minutes on a cold-start Cloud Run revision
— with no progress written anywhere. This is the cloud-target-layer half of K-1. Requires
X-1 (add `LogWriter` to `Spec`) to fix. Effort: ~30 min per adapter once X-1 lands.

### CR-3 — Rollback error not wrapped

**Severity: Low | File:line:** `cloudrun.go:157`

`return err` on the final `UpdateService` call strips the "cloud-run: rollback" prefix. All
other error sites in this file wrap consistently. Trivial: `fmt.Errorf("cloud-run: rollback
%s: %w", appID, err)`.

### CR-4 — Non-404 `GetService` error swallowed in `Deploy`

**Severity: Medium | File:line:** `cloudrun.go:70-82`

```go
if _, gerr := c.GetService(ctx, ...); gerr == nil { /* update */ }
// falls through to Create on ANY gerr, not just NotFound
```

A permission-denied or quota-exceeded error causes silent fallthrough to `CreateService`,
which will fail with a confusing error or silently create a duplicate service. Fix: check
`status.Code(gerr) == codes.NotFound` before creating. Effort: ~30 minutes.

---

## 3. ECS / Fargate (`deploytarget/ecs/ecs.go`)

### E-1 — AWS SDK config loaded per call; no client reuse

**Severity: High | File:line:** `ecs.go:53-59`

`client()` calls `awsconfig.LoadDefaultConfig` on every `Deploy`, `Status`, and `Rollback`.
This performs file I/O, IMDS HTTP calls, and env-var parsing each time. Fix: cache
`*ecs.Client` in a `Target` field, initialize once via `sync.Once`. Effort: ~30 minutes.

### E-2 — `UpdateService` non-404 error swallowed; creates on any failure

**Severity: High (Critical for operators) | File:line:** `ecs.go:113-119`
**Status: CLOSED** — Fixed in `claude/w4-multicloud-bug-bundle`. `errors.As(uerr, &notFound)`
now gates the CreateService fallthrough; any non-ServiceNotFoundException is returned
immediately with context. Test: `TestECS_E2_UpdateServiceNonNotFoundErrorPropagates`.

```go
if _, uerr := c.UpdateService(ctx, ...); uerr == nil { return nil }
// falls through to CreateService on ANY uerr — including IAM deny, throttle
```

An IAM permission error or wrong cluster ARN on `UpdateService` silently triggered
`CreateService`, creating a ghost ECS service consuming Fargate capacity. This was the most
dangerous silent-data-corruption bug in the adapters: it burned money and was undiagnosable
from the Cooker UI.

### E-3 — No LogWriter; async deploy is fire-and-forget

**Severity: High | File:line:** `ecs.go:63`

After `CreateService`/`UpdateService` returns, ECS rolls out asynchronously. No progress
(task placement, image pull, health check) is visible. Requires X-1. Effort: ~2 hours
including a brief polling loop that emits `running=N/desired=M` lines.

### E-4 — CPU/Memory hardcoded at 256/512; not configurable

**Severity: Medium | File:line:** `ecs.go:91-92`

All task definitions use 256 CPU units and 512 MiB. Any non-trivial workload will OOM or be
CPU-throttled. Add `CPU`, `Memory` string fields to `Target`; expose via env-var; document in
`.env.uat.example` and `docs/UAT.md`. Effort: ~1 hour.

### E-5 — `aws.ToInt32` applied to value-type fields

**Severity: Low | File:line:** `ecs.go:158`

`RunningCount` and `DesiredCount` are `int32` value fields in the v2 SDK, not pointers.
Taking their address for `aws.ToInt32` is a no-op round-trip; code is correct but misleading.
Use the fields directly. Trivial.

---

## 4. Fly.io (`deploytarget/flyio/flyio.go`)

### F-1 — Hardcoded 30s `http.Client.Timeout`; context deadline ignored

**Severity: Medium | File:line:** `flyio.go:36`

`&http.Client{Timeout: 30 * time.Second}` acts as a separate wall-clock timer independent of
the request context's deadline. A 5-second stage timeout will not cancel the HTTP call until
30s have elapsed. Conversely, operators in high-latency regions cannot tune the timeout. Fix:
remove the client `Timeout`; wrap `ctx` with `context.WithTimeout` using a configurable
`FlyioConfig.RequestTimeout` default. Effort: ~30 minutes.

### F-2 — Deploy always creates a new Machine; no update path

**Severity: High | File:line:** `flyio.go:112-134`
**Status: CLOSED** — Fixed in `claude/w4-multicloud-bug-bundle`. `Deploy` now calls
`GET /apps/<id>/machines` first via `listMachines()`. When machines exist, it calls
`POST /apps/<id>/machines/<machine_id>` (update); otherwise it creates. Tests:
`TestFly_F2_DeployUpdatesExistingMachine`, `TestFly_F2_DeployCreatesOnFirstDeploy`.

`Deploy` previously always `POST /apps/<id>/machines` (create). Every subsequent deploy
appended a new machine alongside existing ones; billing grew linearly with deploy count.

### F-3 — Rollback calls a nonexistent bulk-restart endpoint

**Severity: High | File:line:** `flyio.go:180`
**Status: CLOSED** — Fixed in `claude/w4-multicloud-bug-bundle`. `Rollback` now calls
`listMachines()` then `POST /apps/<id>/machines/<machine_id>/restart` for each machine.
Returns a clear error when no machines are found. Test: `TestFly_F3_RollbackUsesPerMachineRestart`.

```go
t.do(ctx, http.MethodPost, "/apps/"+appID+"/machines/restart", nil)
```

The Fly Machines API requires a machine ID: `POST /apps/{app}/machines/{machine_id}/restart`.
The old URL returned `404` from the Fly API silently; operators could not roll back via Cooker.

### F-4 — No LogWriter; machine allocation details discarded

**Severity: High | File:line:** `flyio.go:79`

The JSON response from machine create/update contains machine ID, region, and state — all
discarded (`_, code, err := t.do(...)`). Requires X-1. Effort: ~1 hour once X-1 lands.

---

## 5. Render (`deploytarget/render/render.go`)

### R-1 — `findServiceID` called twice per `Status` and `Rollback`; no caching

**Severity: Medium | File:line:** `render.go:113, 136, 166, 181`

Each `Status` call makes two Render API requests: `GET /services?name=<name>` plus
`GET /services/<id>`. `AppHealthChecker` polls every 30 seconds; a workspace with many apps
can exhaust Render's per-account rate limits on health checks alone. Fix: cache
`appID → serviceID` in a `sync.Map` on `Target` with a 5-minute TTL. Effort: ~1 hour.

### R-2 — `Status` URL always empty; broken JSON struct tag

**Severity: High | File:line:** `render.go:143-156`
**Status: CLOSED** — Fixed in `claude/w4-multicloud-bug-bundle`. `renderServiceDetail` now
has a proper nested struct for `serviceDetails`; `Status()` reads
`resp.Service.ServiceDetails.URL`. Test: `TestRender_StatusURLDecodes` locks the JSON shape.

```go
ServiceURL string `json:"serviceDetails.url"`
```

`"serviceDetails.url"` was treated as a literal field name (not a dot-path). The Render API
returns the URL nested under `service.serviceDetails.url`; `Status.URL` was always `""` for
every Render service.

### R-3 — No LogWriter; async deploy is silent

**Severity: High | File:line:** `render.go:109`

After `POST /services/<id>/deploys`, Render builds asynchronously. The deploy ID in the
response is discarded; no polling, no progress. Requires X-1. Effort: ~2 hours including a
polling loop against `GET /deploys/<id>`.

### R-4 — Hardcoded 30s `http.Client.Timeout`; same as F-1

**Severity: Medium | File:line:** `render.go:36`

Identical issue to F-1. Render's `/services` list can exceed 30s on large workspaces.
Same fix. Effort: ~15 minutes.

---

## 6. Cross-cutting

### X-1 — `deploytarget.Spec` has no `LogWriter`; T2 has no adapter substrate

**Severity: High (blocks CR-2, E-3, F-4, R-3) | File:line:** `deploytarget/target.go:35-41`

All five cloud adapters share the root cause: `Spec` has no `LogWriter io.Writer` field.
This is the deploy-target-layer half of `dag-performance.md` High finding #2 and the direct
target of `dag-adaptation-2026.md` §6 T2 ("Wire LogWriter for push + deploy"). Adding the
field to `Spec` is a backward-compatible struct change; all five adapter constructors and test
doubles must be updated atomically. CLAUDE.md flags interface changes as escalation triggers
for opus — confirm this is a struct field, not a method signature change, before proceeding
on sonnet.

### X-2 — `Logs()` returns `ErrUnavailable` on all cloud adapters

**Severity: Medium | File:line:** `cloudrun.go:117`, `ecs.go:163`, `flyio.go:168`, `render.go:158`

All four cloud adapters stub `Logs()` with `ErrUnavailable`. The Logs tab on the App detail
page surfaces an error for every cloud runtime. Known stub; not a regression. Each adapter
needs a distinct streaming SDK (Cloud Logging, CloudWatch, Fly GraphQL, Render log endpoint).
Document as follow-on per-adapter feature, not in this PR.

### X-3 — No retry at adapter layer; `AppHealthChecker` intolerant of transients

**Severity: Low | File:line:** `service/app_health.go` (service layer, not adapter layer)**

Adapters correctly do not retry internally (per CLAUDE.md agent prompt). `executor.go:208-225`
handles retry for pipeline-stage deploy calls. However `AppHealthChecker.Status` does not pass
through the retry wrapper, so a single transient 503 marks the app unhealthy. Not a per-adapter
fix; flag for service-layer owners.

---

## 7. Severity summary

| # | Adapter | Finding | Severity |
|---|---------|---------|---------|
| K-1 | Kubernetes | No LogWriter; K8s events invisible | High |
| K-2 | Kubernetes | Discovery cache per-instance | Medium |
| K-3 | Kubernetes | `%v` instead of `%w` in ensureClients | Low |
| CR-1 | Cloud Run | New gRPC client per call | High |
| CR-2 | Cloud Run | No LogWriter; LRO wait silent | High |
| CR-3 | Cloud Run | Rollback error not wrapped | Low |
| CR-4 | Cloud Run | Non-404 GetService error swallowed | Medium |
| E-1 | ECS | AWS config loaded per call | High |
| E-2 | ECS | UpdateService non-404 swallowed → ghost service | **CLOSED** |
| E-3 | ECS | No LogWriter; async deploy invisible | High |
| E-4 | ECS | CPU/Memory hardcoded; not configurable | Medium |
| E-5 | ECS | `aws.ToInt32` on value-type field | Low |
| F-1 | Fly.io | Hardcoded timeout ignores ctx deadline | Medium |
| F-2 | Fly.io | Deploy always creates; accumulates machines | **CLOSED** |
| F-3 | Fly.io | Rollback calls nonexistent URL; silent 404 | **CLOSED** |
| F-4 | Fly.io | No LogWriter; machine details discarded | High |
| R-1 | Render | `findServiceID` called twice per poll | Medium |
| R-2 | Render | Status URL always empty; broken JSON tag | **CLOSED** |
| R-3 | Render | No LogWriter; async deploy silent | High |
| R-4 | Render | Hardcoded timeout ignores ctx deadline | Medium |
| X-1 | All | `Spec` has no `LogWriter`; T2 blocked | High |
| X-2 | All cloud | `Logs()` always ErrUnavailable | Medium |
| X-3 | All | AppHealthChecker intolerant of transients | Low |

---

## 8. Top 3 findings by operator impact (multi-cloud production)

All three have been closed by PR `claude/w4-multicloud-bug-bundle`.

**1. E-2 — ECS UpdateService non-404 swallowed → ghost Fargate service (`ecs.go:113-119`) — CLOSED**
An IAM permission error, API throttle, or wrong cluster ARN on `UpdateService` silently
triggered `CreateService`. The result was a ghost Fargate service consuming capacity and
incurring billing, with no Cooker-visible error. Fix: `errors.As` gate on
`ServiceNotFoundException`; non-404 errors returned immediately. Regression test added.

**2. F-2 + F-3 — Fly.io accumulates machines + Rollback is a silent 404 (`flyio.go:112-134, 180`) — CLOSED**
These two compound: every deploy appended a new Machine (unbounded billing growth), and the
Rollback button called `POST /apps/<id>/machines/restart` — a URL that does not exist on the
Fly API. Fix: `listMachines()` helper drives both; Deploy updates in-place when machines exist;
Rollback calls `POST /apps/<id>/machines/<id>/restart` per machine. Two test files added.

**3. R-2 — Render Status URL always empty due to malformed JSON tag (`render.go:143-156`) — CLOSED**
`json:"serviceDetails.url"` was treated by Go's JSON decoder as a literal key, not a dot-path.
Fix: `renderServiceDetail.ServiceDetails` is now a proper nested struct. `Status()` reads
`resp.Service.ServiceDetails.URL`. `TestRender_StatusURLDecodes` locks the JSON shape.
