# Postgres ↔ Memory Store Parity Audit (2026-05)

**Scope:** `backend/internal/store/store.go` (interfaces), `backend/internal/store/memory/memory.go` (memory impl), `backend/internal/store/postgres/{pipeline,run,environment,app,host,user}.go` (Postgres impl), all migrations in `backend/internal/store/postgres/migrations/`, and matching model structs in `backend/internal/model/`.

**Method:** Static analysis only — no code was modified. All citations are file:line using the branch state at the time of this audit.

**Verdict:** Interfaces are fully symmetric. ErrNotFound discipline is clean across all Get and mutation paths with one partial-coverage exception. Migration ↔ model alignment is solid with one silent divergence. The heartbeat write path has a documented gap in the app-deploy flow.

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
This is neither `store.ErrNotFound` nor `store.ErrConflict`. A caller doing `errors.Is(err, store.ErrConflict)` gets `false`. The Signup handler (`auth_local.go:89–96`) guards against this by calling `GetByEmail` first, but that introduces a TOCTOU race: two concurrent signups with the same email can both pass the check and then race to `Create`.

**Postgres** (`user.go:43–52`): returns the `lib/pq` unique-constraint error unwrapped. Both impls diverge from the `store.ErrConflict` sentinel that the rest of the codebase uses.

**Fix:** In memory, return `fmt.Errorf("user %s: %w", u.Email, store.ErrConflict)`. In Postgres, detect `pq.Error` code `23505` and return `store.ErrConflict`.

**Effort:** ~30 min per impl + one test.

---

### F-02 — RunStore.Update skips optimistic concurrency — design intent undocumented (severity: low)

`RunStore.Update` in memory (`memory.go:124–130`) replaces the map entry with no version check. Postgres (`run.go:85–113`) uses no `AND version=$N` clause. This is by design: `pipeline_runs` has no `version` column (confirmed: `007_versioning.up.sql` only adds `version` to `pipelines`, `environments`, `apps`, `hosts`). The interface comment at `store.go:33–44` does not explain this, making the absence surprising to future contributors.

**Fix:** Add a comment to `store.go:35` noting that `RunStore.Update` intentionally skips optimistic concurrency; `pipeline_runs` is written by a single coordinator goroutine per run.

**Effort:** 5 min (comment only).

---

### F-03 — All ErrNotFound paths correctly wrap (no bare sql.ErrNoRows leaks)

Every `QueryRowContext` scan in Postgres is wrapped via `errors.Is(err, sql.ErrNoRows)` before mapping to `store.ErrNotFound`: `pipeline.go:47–48`, `run.go:53–54`, `environment.go:47–48`, `app.go:57–58` and `68–69`, `host.go:47–48`, `user.go:26–27` and `37–38`. All mutation paths (`Update`, `Delete`) check `RowsAffected == 0`. Memory mirrors the pattern. **This category is clean.**

---

## Category 3: Migration ↔ Go-Struct Field-Order Drift

### F-04 — `pipeline_runs.created_at` exists in DB but has no corresponding field on `model.PipelineRun` (severity: medium)

`001_initial.up.sql:25`:
```sql
created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
```

`model.PipelineRun` (`model/run.go:17–31`) has no `CreatedAt` field. The column drives `ORDER BY created_at DESC` in `RunStore.List` (`run.go:29`) and is never selected. Consequences:

1. The API response for a run exposes no creation timestamp. Clients cannot sort or filter run history without relying on `startedAt`, which is null for pending runs.
2. The memory impl (`memory.go:95–105`) returns runs in map-iteration order (non-deterministic). Postgres returns newest-first. This is the only ordering divergence between the two impls; no test catches it.

**Fix (two options):** Add `CreatedAt time.Time` to `model.PipelineRun` and select it in both queries; or add a comment that memory ordering is deliberately unspecified.

**Effort:** ~1 hour (adding the field) or 5 min (comment).

---

### F-05 — `HealthStatus` default is `"unknown"` in Postgres, `""` in memory (severity: low)

`008_app_health.up.sql:11` sets `DEFAULT 'unknown'` on the `health_status` column. A freshly-created App read back from Postgres has `HealthStatus = model.AppHealthUnknown`. Memory `apps.Create` (`memory.go:261–265`) stores the struct as-is; if the caller omits `HealthStatus`, the zero value `""` is stored. The parity test `app_health_test.go:79` locks in the divergence rather than closing it:

```go
if got.HealthStatus != "" {
    t.Errorf("expected zero-value HealthStatus on new memory App, got %q", got.HealthStatus)
}
```

**Fix:** Initialize `HealthStatus = model.AppHealthUnknown` in memory `Create`, or document the divergence in the interface comment on `AppStore.Create`.

**Effort:** 5 min.

---

### F-06 — All other migration columns map 1-to-1 to model fields (verified clean)

`001` pipelines, `002` env secrets, `003` apps, `004` hosts, `005` users, `006` run heartbeat, `007` versioning (pipelines/environments/apps/hosts), `008` app health — all columns present in migrations are represented in the corresponding model struct. **No additional drift.**

---

## Bonus: Heartbeat Write Path Coverage

### F-07 — App-deploy synthesized runs: heartbeat fires against a non-existent row; crash mid-deploy silently loses the run (severity: medium)

`RunCoordinator.Spawn` (`server/runs.go:78`) writes the first heartbeat synchronously before `work` executes. For pipeline-triggered runs, the run row is created before `Spawn` is called. For app-deploy runs (`handler/app.go:179`), the synthesized `PipelineRun` is **not persisted before Spawn** — the row is only written at the end of `runAppDeployCtx` (`app.go:226–229`). The first heartbeat returns `store.ErrNotFound`, tolerated at `runs.go:111`. Subsequent 30-second ticks also find no row.

If the process crashes during an app-deploy that runs longer than `orphanThreshold` (90s), the boot-sweep (`SweepOrphans`) will never mark the run orphaned — no row exists. The run is silently lost with no user-visible record. The partial index from `006_run_heartbeat.up.sql` (`WHERE status = 'running'`) is correct; the gap is in the app-deploy lifecycle.

**Fix:** In `handler/app.go`, `Create` a stub `PipelineRun` row with `Status = running` before calling `Spawn`, then `Update` it at the end of the deploy. Mirrors the pipeline-run path.

**Effort:** ~1 hour (handler change + conformance test).

---

### F-08 — `RunStore.Update` preserves `heartbeat_at` correctly in both impls (confirmed)

Postgres `Update` (`run.go:101–104`) includes `heartbeat_at=$9` from `nullTime(r.HeartbeatAt)`. Memory `Update` (`memory.go:124–130`) replaces the whole struct, preserving whatever `HeartbeatAt` the caller set. No drift. A full `Update` call will not silently zero out a previously-written heartbeat in either impl.

---

## Migration Completeness Check

All 8 migrations (`001_initial` through `008_app_health`) have matching `.up.sql` and `.down.sql` files. All `.up.sql` files use `IF NOT EXISTS` / `ADD COLUMN IF NOT EXISTS`. The migration runner (`store.go:applyMigrations`) holds `pg_advisory_lock` and records applied versions in `schema_migrations`, making re-runs idempotent. **No in-progress or orphaned migration found.**

---

## Severity Summary

| # | Finding | Severity | File:Line |
|---|---|---|---|
| F-01 | `UserStore.Create` duplicate-email error is not `store.ErrConflict` in either impl | Medium | `memory.go:402`; `user.go:43–52` |
| F-02 | `RunStore.Update` omits optimistic concurrency — intent undocumented | Low | `store.go:33–44`; `run.go:85–113` |
| F-03 | All ErrNotFound paths correctly wrap `sql.ErrNoRows` | OK | — |
| F-04 | `pipeline_runs.created_at` in DB, absent from `model.PipelineRun`; list ordering diverges | Medium | `001_initial.up.sql:25`; `model/run.go:17`; `memory.go:95` |
| F-05 | `HealthStatus` default `"unknown"` in Postgres, `""` in memory; test pins the gap | Low | `008_app_health.up.sql:11`; `memory.go:261`; `app_health_test.go:79` |
| F-06 | All other migration columns match model fields | OK | — |
| F-07 | App-deploy run row absent during heartbeat window; crash mid-deploy loses the run | Medium | `handler/app.go:179`; `server/runs.go:78` |
| F-08 | `RunStore.Update` correctly preserves `heartbeat_at` in both impls | OK | `run.go:101`; `memory.go:124` |

---

## Top 3 by Production Blast Radius

**1. F-07 — App-deploy run row never exists during heartbeat window.**
A user clicks Deploy on an App; if the deploy runs longer than 90 seconds and the pod restarts mid-deploy (OOM kill, rolling update, node eviction), the run is silently dropped. No row in `pipeline_runs`, no status in the UI, no orphan-sweep entry. The user sees a 202 and a WebSocket channel that goes dark. Blast radius: every App deploy in production — the most common user action after pipeline runs. Fix: create the stub run row before `Spawn`.

**2. F-04 — `pipeline_runs.created_at` absent from `model.PipelineRun`; list ordering non-deterministic in memory.**
`RunStore.List` returns runs newest-first from Postgres but in hash-map iteration order from the memory impl. Any test that asserts on list order is intermittently flaky in the memory backend. The missing field also means the API never exposes a creation timestamp for a run — clients cannot order or paginate run history without relying on `startedAt`, which is null for pending runs. Blast radius: any feature that pages or sorts pipeline run history.

**3. F-01 — `UserStore.Create` duplicate-email error is not `store.ErrConflict` in either impl.**
Memory returns a raw string; Postgres surfaces a `pq.Error` code `23505`. The Signup handler pre-flights with `GetByEmail` to work around this, but that introduces a TOCTOU race: two concurrent signups with the same email can both pass the guard and race to `Create`. Any future caller that checks `errors.Is(err, store.ErrConflict)` will silently mishandle the duplicate case. Blast radius: signup under concurrent load, and any service code added later that calls `UserStore.Create` directly.

---

## Closed findings

Findings moved here once the fix lands on `main`.

- **F-07** — App-deploy synthesised run row was never created before `RunCoordinator.Spawn`, so the coordinator's first heartbeat (`runs.go:78`) silently no-oped (`runs.go:111`) and a crash mid-deploy left no orphan row for the boot-sweep to reap. Fixed in `claude/w2-backend-perf-and-f07`: `handler/app.go:DeployApp` now creates a stub `PipelineRun` row (`Status=running`, `StartedAt=now`) before calling `Spawn`; `runAppDeployCtx` drops the `Create`-fallback and only calls `Update` at the end. A conformance test `TestRunCoordinator_F07_HeartbeatSucceedsWhenRowCreatedBeforeSpawn` in `internal/server/runs_test.go` asserts that `heartbeat_at` is populated when the row pre-exists Spawn.
