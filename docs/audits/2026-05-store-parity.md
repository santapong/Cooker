# Postgres ↔ Memory Store Parity Audit (2026-05)

**Scope:** `backend/internal/store/store.go` (interfaces), `backend/internal/store/memory/memory.go` (memory impl), `backend/internal/store/postgres/{pipeline,run,environment,app,host,user}.go` (Postgres impl), all migrations in `backend/internal/store/postgres/migrations/`, and matching model structs in `backend/internal/model/`.

**Method:** Static analysis only — no code was modified. All citations are file:line using the branch state at the time of this audit.

**Verdict:** Interfaces are fully symmetric (no orphaned methods). ErrNotFound discipline is clean across all Get and mutation paths with one partial-coverage exception. Migration ↔ model alignment is solid with one silent divergence. The heartbeat write path has a documented gap in the app-deploy flow.

---

## Category 1: Method Signature Parity

All six store interfaces (`PipelineStore`, `RunStore`, `EnvironmentStore`, `AppStore`, `HostStore`, `UserStore`) are defined in `backend/internal/store/store.go:23–89`. Both impls satisfy every interface method. No divergence found.

| Interface method | Memory | Postgres |
|---|---|---|
| `PipelineStore.{List,Get,Create,Update,Delete}` | `memory.go:37–88` | `pipeline.go:24–121` |
| `RunStore.{List,Get,Create,Update,UpdateHeartbeat,SweepOrphans}` | `memory.go:95–164` | `run.go:25–149` |
| `EnvironmentStore.{List,Get,Create,Update,Delete}` | `memory.go:171–222` | `environment.go:24–126` |
| `AppStore.{List,Get,GetByRepo,Create,Update,Delete,UpdateHealth}` | `memory.go:229–309` | `app.go:32–150` |
| `HostStore.{List,Get,Create,Update,Delete}` | `memory.go:311–367` | `host.go:20–96` |
| `UserStore.{GetByEmail,GetByID,Create,Update,Count}` | `memory.go:369–429` | `user.go:21–75` |

**No findings in this category.**

---

## Category 2: ErrNotFound Discipline

### F-01 — UserStore.Create: memory returns a plain string error on duplicate; Postgres surfaces a DB-driver error (severity: medium)

**Memory** (`memory.go:402`):
```go
return fmt.Errorf("user %s: already exists", u.Email)
```
This is neither `store.ErrNotFound` nor `store.ErrConflict`. A caller doing `errors.Is(err, store.ErrConflict)` will get `false`. The handler (`auth_local.go:89–96`) guards against this by calling `GetByEmail` first, but the Create path itself leaks a raw string error that no sentinel covers.

**Postgres** (`user.go:43–52`): returns the `lib/pq` unique-constraint error unwrapped. The Signup handler (`auth_local.go:109–113`) treats any non-nil error from Create as HTTP 500, so the different surface from the two impls doesn't crash the caller — but the divergence means future callers cannot portably detect a duplicate-email attempt without an impl-specific check.

**Fix:** Define `store.ErrConflict` (already exists at `store.go:19`) wrapping in memory Create; in Postgres detect `pq.Error` code `23505` (unique_violation) and return `store.ErrConflict`.

**Effort:** ~30 min per impl + one test.

---

### F-02 — RunStore.Update: memory does not check for ErrConflict; Postgres does not either — both accept blind overwrites (severity: low, by design)

`RunStore.Update` in memory (`memory.go:124–130`) replaces the map entry without any version check. Postgres (`run.go:85–113`) does likewise — no `AND version=$N` clause, unlike `PipelineStore.Update`, `EnvironmentStore.Update`, `AppStore.Update`, and `HostStore.Update`.

This is intentional: `pipeline_runs` has no `version` column (confirmed: `007_versioning.up.sql` only adds `version` to `pipelines`, `environments`, `apps`, `hosts` — not `pipeline_runs`). However, the absence is undocumented in the interface comment at `store.go:33–44`. A future engineer adding a version column to `pipeline_runs` could diverge the two impls.

**Fix:** Add a comment to `store.go:35` noting that `RunStore.Update` intentionally skips optimistic concurrency; `pipeline_runs` is written by a single coordinator goroutine per run.

**Effort:** 5 min (comment only).

---

### F-03 — All ErrNotFound paths correctly wrap (no bare sql.ErrNoRows leaks)

Every `QueryRowContext` scan is wrapped correctly:

- `pipeline.go:47–48` — `errors.Is(err, sql.ErrNoRows)` → `store.ErrNotFound`
- `run.go:53–54` — same pattern
- `environment.go:47–48` — same
- `app.go:57–58`, `app.go:68–69` — same
- `host.go:47–48` — same
- `user.go:26–27`, `user.go:37–38` — same

All mutation paths (`Update`, `Delete`) check `RowsAffected == 0` and return a wrapped `store.ErrNotFound`. No bare `sql.ErrNoRows` escapes to callers. **This category is clean.**

---

## Category 3: Migration ↔ Go-Struct Field-Order Drift

### F-04 — `pipeline_runs.created_at` column exists in DB but has no corresponding field on `model.PipelineRun` (severity: medium)

Migration `001_initial.up.sql:25`:
```sql
created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
```

`model.PipelineRun` (`run.go:17–31`) has no `CreatedAt` field. The column is used only in the `ORDER BY created_at DESC` clause inside `RunStore.List` (`run.go:29`) and is never selected into the struct. This is functional (Postgres allows ORDER BY on non-selected columns) but:

1. Any future `SELECT *` query or ORM migration will silently include a column the struct cannot absorb.
2. The API response for a run has no `createdAt` field, even though the data exists. Clients who want to sort runs chronologically on the frontend receive only `startedAt` (which can be null for pending runs) and must infer order from the API's list order.
3. The memory impl (`runs.List`, `memory.go:95–105`) returns runs in map-iteration order (non-deterministic), while Postgres returns them newest-first by `created_at`. This is the only ordering divergence between the two impls.

**Fix (two options):** Add `CreatedAt time.Time` to `model.PipelineRun` and select it; or add a comment in `RunStore.List` that the memory impl ordering is deliberately unspecified.

**Effort:** ~1 hour (adding the field) or 5 min (comment).

---

### F-05 — `apps.has_webhook` does not exist as a column; the field lives only in Go (non-issue, by design)

`model.App.HasWebhook` (`app.go:91`) is a computed field populated by `App.Redact()` (`app.go:116`) based on whether `WebhookSecret` is non-empty. It is not persisted and correctly omitted from all SQL queries. Both impls are consistent. **No finding — documented for clarity.**

---

### F-06 — All other migration columns map 1-to-1 to model fields

| Migration | Table.Column | Model Field | Match |
|---|---|---|---|
| `001` | `pipelines.{id,name,description,stages,edges,variables,created_at,updated_at}` | `model.Pipeline` | OK |
| `001` | `pipeline_runs.{id,pipeline_id,status,stage_runs,env_statuses,variables,started_at,finished_at,error,created_at}` | `model.PipelineRun` | `created_at` missing (F-04) |
| `001` | `environments.{id,name,sort_order,target,promotion,variables,created_at}` | `model.Environment` | OK |
| `002` | `environments.secrets` | `model.Environment.Secrets` | OK |
| `003` | `apps.{id,name,description,github_repo,branch,build_plan,deploy_target,registry_ref,environment_id,webhook_secret,auto_deploy,created_at,updated_at}` | `model.App` | OK |
| `004` | `hosts.{id,name,kind,reachability,docker_endpoint,kubeconfig_ref,tailnet_ip,created_at,updated_at}` | `model.Host` | OK |
| `005` | `users.{id,email,name,password_hash,role,created_at,updated_at}` | `model.User` | OK |
| `006` | `pipeline_runs.heartbeat_at` | `model.PipelineRun.HeartbeatAt` | OK |
| `007` | `{pipelines,environments,apps,hosts}.version` | all four model structs `.Version` | OK |
| `008` | `apps.{health_status,health_checked_at,health_message}` | `model.App.{HealthStatus,HealthCheckedAt,HealthMessage}` | OK |

---

### F-07 — Memory `HealthStatus` default diverges from Postgres column default (severity: low)

Postgres `008_app_health.up.sql:11` gives `health_status` a column-level `DEFAULT 'unknown'`, so a freshly-created App read back from Postgres has `HealthStatus = "unknown"` (type `model.AppHealthUnknown`).

Memory `apps.Create` (`memory.go:261–265`) stores the struct as-is. If the caller does not set `HealthStatus`, the zero value (`""`) is stored and returned. The conformance test `app_health_test.go:79` explicitly pins this divergence:

```go
// Pin the memory behaviour here so any future drift between impls is caught.
if got.HealthStatus != "" {
    t.Errorf("expected zero-value HealthStatus on new memory App, got %q", got.HealthStatus)
}
```

The test acknowledges the divergence and locks it in rather than closing it. Callers must not rely on a consistent default across impls.

**Fix:** Either initialize `HealthStatus = model.AppHealthUnknown` in memory `Create`, or document the divergence in the interface comment on `AppStore.Create`.

**Effort:** 5 min.

---

## Bonus: Heartbeat Write Path Coverage

### F-08 — App-deploy synthesized runs: heartbeat fires before the run row exists (gap, not a bug) (severity: low)

`RunCoordinator.Spawn` (`server/runs.go:62–103`) writes the first heartbeat synchronously at line 78, before `work` executes. For pipeline-triggered runs the run row is created by the handler before `Spawn` is called, so `UpdateHeartbeat` lands on an existing row.

For app-deploy runs (`handler/app.go:179`), the synthesized `PipelineRun` is **not persisted before Spawn** — the run row is only written at the end of `runAppDeployCtx` (`app.go:226–229`). The first heartbeat therefore silently returns `store.ErrNotFound`, which `heartbeatBestEffort` (`runs.go:111`) explicitly tolerates:

```go
if err == nil || errors.Is(err, store.ErrNotFound) {
    return
}
```

Subsequent 30-second heartbeats also find no row. If the app-deploy takes longer than `orphanThreshold` (90s) and the process crashes mid-deploy, the boot-sweep will never mark the run orphaned — because no row exists to sweep. The run is simply lost.

The partial index from migration `006` (`idx_pipeline_runs_running_heartbeat WHERE status = 'running'`) correctly excludes non-existent rows, so the index itself is sound. The gap is in the lifecycle: the synthesized run row should be `Create`d before `Spawn` so the heartbeat mechanism can protect it.

**Fix:** In `handler/app.go`, create a stub `PipelineRun` row (with `Status = running`) before calling `Spawn`, then `Update` it at the end of the deploy. This matches the pipeline-run path.

**Effort:** ~1 hour (handler change + test).

---

### F-09 — `RunStore.Update` preserves `heartbeat_at` correctly in both impls (confirmed)

`run.go:101–104`: Postgres `Update` includes `heartbeat_at=$9`. Memory `Update` (`memory.go:124–130`) replaces the whole struct, preserving whatever `HeartbeatAt` the caller set. No drift here.

---

## Migration Completeness Check

All 8 migrations (`001_initial` through `008_app_health`) have matching `.up.sql` and `.down.sql` files. All `.up.sql` files use `IF NOT EXISTS` / `ADD COLUMN IF NOT EXISTS`. The migration runner (`store.go:applyMigrations`) holds `pg_advisory_lock` and records applied versions in `schema_migrations`, making re-runs idempotent. **No in-progress or orphaned migration found.**

---

## Severity Summary

| # | Finding | Severity | File:Line |
|---|---|---|---|
| F-01 | `UserStore.Create` memory returns raw string; Postgres surfaces driver error — neither returns `ErrConflict` | Medium | `memory.go:402`; `user.go:43–52` |
| F-02 | `RunStore.Update` skips optimistic concurrency — design intent undocumented | Low | `store.go:33–44`; `run.go:85–113` |
| F-03 | All ErrNotFound paths correct — no bare `sql.ErrNoRows` leaks | OK | — |
| F-04 | `pipeline_runs.created_at` exists in DB, absent from `model.PipelineRun`; list ordering diverges between impls | Medium | `001_initial.up.sql:25`; `model/run.go:17–31`; `memory.go:95–105`; `run.go:25–44` |
| F-05 | `App.HasWebhook` correctly computed, not persisted — no drift | OK | — |
| F-06 | All other migration columns match model fields | OK | — |
| F-07 | `HealthStatus` default is `"unknown"` in Postgres, `""` in memory — test pins the gap rather than closing it | Low | `008_app_health.up.sql:11`; `memory.go:261–265`; `app_health_test.go:79` |
| F-08 | App-deploy synthesized run has no row before `Spawn`; heartbeats fire against non-existent row; crash mid-deploy loses the run entirely | Medium | `handler/app.go:179–184`; `server/runs.go:78` |
| F-09 | `RunStore.Update` heartbeat field handled correctly in both impls | OK | `run.go:101–104`; `memory.go:124–130` |

---

## Top 3 by Production Blast Radius

**1. F-08 — App-deploy run row never exists during heartbeat window.**
A user clicks Deploy on an App; if the deploy takes more than 90 seconds and the pod restarts mid-deploy (OOM, rolling update, node eviction), the run is silently dropped. No row in `pipeline_runs`, no status in the UI, no orphan-sweep entry. The user sees the 202 response and a WebSocket channel that goes dark. The blast radius is every App deploy in production — the most common user action. Fix: create the stub run row before `Spawn`.

**2. F-04 — `pipeline_runs.created_at` absent from `model.PipelineRun`; list ordering is non-deterministic in memory.**
Callers of `RunStore.List` receive runs in `created_at DESC` order from Postgres but in hash-map iteration order from the memory impl. Any test or dev-mode code that asserts on list order will produce intermittent failures. The missing field also means the API never exposes a creation timestamp for a run, so clients cannot order or filter run history without relying on `startedAt` (which is null for pending runs). The blast radius is any feature that pages or sorts pipeline run history.

**3. F-01 — `UserStore.Create` duplicate-email error is not `store.ErrConflict` in either impl.**
The Signup handler guards with a pre-flight `GetByEmail`, but that check introduces a TOCTOU race: two concurrent signups with the same email can both pass the check and then race to `Create`. The memory impl returns a non-sentinel string error; the Postgres impl surfaces a `pq.Error` with code `23505`. Any future caller that checks `errors.Is(err, store.ErrConflict)` will silently mishandle the duplicate case in both impls. Blast radius is the signup endpoint under concurrent load, and any service code added in the future that calls `UserStore.Create` directly.
