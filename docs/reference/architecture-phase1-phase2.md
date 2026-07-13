# Architecture: Phase 1 + Phase 2 additions (May 2026)

This document covers the five new subsystems landed in PR #89
(`claude/analyze-dokploy-integration-NTrW3`). It supplements — does
not replace — [`docs/architecture.md`](./architecture.md), which
describes the rest of Cooker.

If you want the high-level pitch ("what changed and why"), read
[`docs/adapted-from-dokploy.md`](../proposals/adapted-from-dokploy.md) first.

---

## Subsystem map

```
                       ┌───────────────────────────┐
                       │ internal/scheduler   │
                       │  pg_advisory_lock    │
                       │  cron parser         │
                       │  due-row scan + enq  │
                       └───────────┬────────────┘
                                   │ EnqueueRun
                                   ▼
HTTP RunPipeline ──▶ internal/service.JobQueueEnqueuer
                                   │
                                   ▼
                       ┌───────────────────────────┐
                       │ internal/jobqueue    │
                       │  postgres.Enqueue    │  INSERT + NOTIFY
                       │  postgres.Dequeue    │  FOR UPDATE SKIP LOCKED
                       │  worker pool         │  N workers, panic-recover
                       │  PqListener          │  wake on NOTIFY
                       │  exp. backoff        │  Reschedule → Failed cap
                       └───────────┬────────────┘
                                   │ Dispatch handler
                                   ▼
                       ┌───────────────────────────┐
                       │ service.JobQueueRunner  │
                       │  loads pipeline + run    │
                       │  calls Executor.Execute  │
                       │  persists run via store  │
                       │  dispatches notifications│
                       └─────────┬──────────────┘
                                 │
                  ┌────────────────┼────────────────┐
                  ▼                  ▼                ▼
     ┌────────────────┐  ┌──────────────┐  ┌─────────────────┐
     │ Executor       │  │ runstate    │  │ notifier         │
     │ (unchanged)    │  │ FSM         │  │ Dispatcher       │
     │                │  │ (transition │  │ fan-out per      │
     │                │  │  validation │  │ enabled Target   │
     │                │  │  layer)     │  │                  │
     └────────────────┘  └──────────────┘  └────────┬────────┘
                                                  │
              ┌────────────────┬───────────────┤
              ▼                ▼                ▼
        Slack webhook     Discord webhook    Email (SMTP)
        Generic JSON webhook (POST + bearer)

               ────────── across all routes ──────────
              ┌───────────────────────────────┐
              │  internal/auth.RequirePermission(r,a) │
              │  composes with RequireRole + RequireMFA│
              └───────────────────────────────┘
```

---

## 1. Job queue (`internal/jobqueue/`)

### Table

```sql
-- migrations/010_jobs.up.sql
CREATE TABLE jobs (
    id              TEXT PRIMARY KEY,
    kind            TEXT NOT NULL,                 -- e.g. "pipeline-run"
    payload         JSONB NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'pending',
    attempts        INTEGER NOT NULL DEFAULT 0,
    max_attempts    INTEGER NOT NULL DEFAULT 1,
    run_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_by       TEXT NOT NULL DEFAULT '',
    locked_at       TIMESTAMPTZ,
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ,
    last_error      TEXT NOT NULL DEFAULT '',
    concurrency_key TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX jobs_pickup_idx       ON jobs (run_at) WHERE status = 'pending';
CREATE INDEX jobs_running_concurrency_idx
    ON jobs (concurrency_key)
    WHERE status = 'running' AND concurrency_key <> '';
```

### Dequeue query (the heart)

```sql
WITH picked AS (
  SELECT id FROM jobs
   WHERE status = 'pending'
     AND run_at <= NOW()
     AND ($1::text[] = '{}' OR kind = ANY($1::text[]))
     AND (concurrency_key = ''
          OR NOT EXISTS (
             SELECT 1 FROM jobs j2
              WHERE j2.concurrency_key = jobs.concurrency_key
                AND j2.status = 'running'))
   ORDER BY run_at
   LIMIT 1
   FOR UPDATE SKIP LOCKED
)
UPDATE jobs SET status='running', locked_by=$2, locked_at=NOW(),
                started_at=NOW(), attempts=attempts+1, updated_at=NOW()
 WHERE id = (SELECT id FROM picked)
 RETURNING ...;
```

- `FOR UPDATE SKIP LOCKED` means N workers can call this in parallel
  without contention; each gets a different row.
- The `NOT EXISTS` guard enforces per-key serialisation. A blocked
  job stays `pending` rather than being skipped permanently.
- `attempts + 1` is atomic; `Reschedule` later branches on
  `attempts >= max_attempts` via a `CASE` so no read-modify-write
  race.

### Worker loop

```
for {
  job := store.Dequeue()
  if job == nil {
     wait for NOTIFY OR poll timeout
     continue
  }
  err := registry.Dispatch(ctx, job)   // panic-recover
  if err == nil {
     store.Complete()
  } else {
     nextRunAt := now + Backoff.NextDelay(attempts)
     store.Reschedule(nextRunAt, err)  // → 'pending' or terminal 'failed'
  }
}
```

### Concurrency key

Enqueue carries an opaque `ConcurrencyKey` (e.g.
`"pipeline:<id>"`). The dequeue guarantees at most one running job
per key. Different keys run in parallel. Setting `ConcurrencyKey=""`
opts out of the guard.

### Metrics

- `cooker_jobqueue_depth{status}` — gauge per status
- `cooker_jobqueue_attempts_total{kind}` — counter
- `cooker_jobqueue_run_duration_seconds{kind,outcome}` — histogram

### Feature flag

`COOKER_JOBQUEUE_ENABLED` (default `false`). When false,
`bootJobQueue` returns all-nil deps; `RunPipeline` falls through to
the inline `Runs.Spawn` path (pre-Phase-1 behaviour).

---

## 2. Run / stage state machine (`internal/runstate/`)

### Generic FSM

```go
type FSM struct {
    current State
    table   map[transitionKey]State
    names   string
}

type Builder struct { ... }       // shared table after Build()
func (FSM) Apply(event Event) (FSM, error)  // value receiver; assign back
func (FSM) Can(event Event) bool
```

- FSM is a **value type**. `Apply` returns a new FSM; the caller
  assigns back. Mutating the receiver wouldn't propagate.
- Builder shares its `table` by reference with every Build()'d
  FSM — cheap because the table is immutable after construction.

### Run alphabet

```
states:   pending, running, success, failed, cancelled
events:   start, succeed, fail, cancel

pending --start--> running --succeed--> success
                          --fail-----> failed
                          --cancel---> cancelled
pending --cancel--> cancelled
```

States **exactly match** `model.RunStatus` values; a test in
`run_test.go` pins each constant against `model.RunStatus.*` so any
future rename can't drift the alphabet.

### Adapter

```go
func TransitionRun(current model.RunStatus, event Event) (model.RunStatus, error)
func CanTransitionRun(current model.RunStatus, event Event) bool
```

Returns `current` unchanged on error so callers that ignore the
error (against advice) at least don't corrupt the column.

### Status of adoption

Primitive in place. `executor.go` still writes statuses directly
for now; the migration is mechanical (one Transition call per
assignment) and lands in a follow-up commit.

---

## 3. Permission middleware (`internal/auth/permission.go`)

### Matrix shape

```go
type Resource string  // ResourcePipeline, ResourceSecret, ResourceWebhook, ...
type Action   string  // ActionRead, ActionInvoke, ActionReveal, ActionUpdate, ...

func Permission(claims *Claims, resource Resource, action Action) bool {
    if claims == nil { return false }
    allowed, ok := policyMatrix[policyKey{resource, action}]
    if !ok { return false }   // deny by default for undeclared pairs
    for _, role := range claims.Roles {
        if allowed[Role(role)] { return true }
    }
    return false
}
```

### Middleware

```go
func RequirePermission(resource Resource, action Action) gin.HandlerFunc
```

Used at route registration. Composes cleanly with the existing
`RequireRole`, `RequireMFA`, rate-limiter, and idempotency
middlewares.

### Adopted routes

| Route | Permission required |
|---|---|
| `POST /api/v1/pipelines/:id/run` | `(pipeline, invoke)` |
| `GET /api/v1/environments/:id/secrets/:key` | `(secret, reveal)` |
| `PUT /api/v1/apps/:id/webhook` | `(webhook, update)` |

These three are highest-impact; the remaining routes can adopt
incrementally with no flag day.

### Defense in depth

The role gate (`adminRole` / `writeRole`) and the new permission
gate co-exist on each adopted route. Dropping one by accident still
leaves the other enforcing.

---

## 4. Notifications (`internal/notify/notifier/`)

### Table

```sql
-- migrations/011_notification_targets.up.sql
CREATE TABLE notification_targets (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    kind        TEXT NOT NULL CHECK (kind IN ('slack','discord','webhook','email')),
    config      JSONB NOT NULL DEFAULT '{}',
    event_types TEXT[] NOT NULL DEFAULT '{}',   -- empty = all events
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### Dispatcher

```go
func (d *Dispatcher) Dispatch(ctx context.Context, event Event) error {
    targets := store.ListEnabled(ctx)
    var wg sync.WaitGroup
    errCh := make(chan error, len(targets))
    for t := range targets matching event.Type {
        wg.Add(1)
        go func(t Target) {
            defer wg.Done()
            sendCtx, cancel := context.WithTimeout(ctx, d.SendTimeout)
            defer cancel()
            if err := registry.Lookup(t.Kind).Send(sendCtx, t, event); err != nil {
                errCh <- err
            }
        }(t)
    }
    wg.Wait(); close(errCh)
    return errors.Join(errors...)
}
```

- One goroutine per target. Per-target `SendTimeout` so a hung
  webhook can't block executor progress.
- `errors.Join` surfaces per-target failures together; the runner
  logs the joined error but does not fail the job.

### Channel adapters

| Kind | Wire shape |
|---|---|
| `slack` | POST to incoming-webhook URL, `{text, channel, username, icon_emoji}` |
| `discord` | POST to webhook URL, `{embeds: [{title, description, color, ...}]}` with color per event type |
| `webhook` | POST event JSON to arbitrary URL, optional `Authorization: Bearer <token>` + headers |
| `email` | SMTP via stubbable `SMTPSender` interface, plain text body, port 587 default |

Event types: `run.succeeded`, `run.failed`, `run.cancelled`,
`deploy.succeeded`, `deploy.failed`, `build.failed`.

---

## 5. Scheduler (`internal/scheduler/`)

### Table

```sql
-- migrations/012_schedules.up.sql
CREATE TABLE schedules (
    id           TEXT PRIMARY KEY,
    pipeline_id  TEXT NOT NULL,
    name         TEXT NOT NULL DEFAULT '',
    cron_expr    TEXT NOT NULL,
    timezone     TEXT NOT NULL DEFAULT 'UTC',
    last_run_at  TIMESTAMPTZ,
    last_run_id  TEXT NOT NULL DEFAULT '',
    next_run_at  TIMESTAMPTZ NOT NULL,
    enabled      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### Leader election

```go
func (r *Runner) tryAcquireLock(ctx context.Context) (*sql.Conn, bool, error) {
    conn, err := r.db.Conn(ctx)
    if err != nil { return nil, false, err }
    var got bool
    err = conn.QueryRowContext(ctx,
        `SELECT pg_try_advisory_lock($1)`, schedulerLockKey).Scan(&got)
    return conn, got, err
}
```

- Lock is **session-scoped** — held on a dedicated connection. When
  the leader exits or dies, the connection closes and Postgres
  releases the lock automatically.
- Each tick the leader's loop `Ping`s its connection. A dead
  connection means the lock was released; the runner exits the
  `leadLoop` and retries acquisition.
- Non-leader replicas sleep `tickEvery * 2` and retry. The wait
  time isn't latency-critical because the leader is fielding the
  fires anyway.

### Cron parser

In-house 5-field POSIX. Field bitmasks for fast `matches(v)`. POSIX
dom/dow OR rule: when both are restrictive, *either* matching is
sufficient. Search capped at 4 years per `Next()` to bail on
pathological expressions.

Timezone-aware: operates in the schedule's IANA location so
`"0 9 * * *"` fires at 9am local across spring-forward / fall-back.

Supported syntax:

```
*        any
5        exactly 5
1,3,5    list
1-5      range
*/5      step
1-10/2   range with step
```

Not supported: `@hourly`, `@daily`, `L`, `W`, `#`. Operators who
want `@hourly` can write `"0 * * * *"`. A follow-up can swap in
`robfig/cron/v3` if descriptor support becomes important.

### Tick

```go
func (r *Runner) tick(ctx context.Context) {
    now := r.nowFn()
    due := store.DueBefore(ctx, now)
    for _, s := range due {
        cron, err := Parse(s.CronExpr)
        if err != nil { store.Update(s.Enabled=false); continue }
        runID := newRunID()
        if err := enqueuer.EnqueueRun(ctx, s.PipelineID, runID); err != nil {
            continue  // retry on next tick
        }
        next := cron.Next(now, s.LoadLocation())
        store.MarkFired(s.ID, now, runID, next)  // atomic single UPDATE
    }
}
```

Key property: a bad cron expression disables the row rather than
looping forever. Enqueue failures retry on the next tick without
advancing `next_run_at`.

---

## End-to-end runtime path (everything enabled)

```
  Cron tick                          HTTP POST /api/v1/pipelines/:id/run
     │                                          │
     └─ Scheduler scans schedules ───┬        │
        enqueues pipeline-run job   │        │
                                    ▼        ▼
                       JobQueueEnqueuer.EnqueueRun(pipelineID, runID)
                                    │
                                    ▼
                  INSERT INTO jobs + NOTIFY cooker_jobs_new
                                    │
                                    ▼
              Worker Dequeue (FOR UPDATE SKIP LOCKED)
                                    │
                                    ▼
              JobQueueRunner.Handle
                  ├─ Load pipeline + run from store
                  ├─ Executor.Execute(ctx, p, run)
                  ├─ store.Runs.Update(run)
                  └─ Notifier.Dispatch(event)
                          ├─ Slack webhook
                          ├─ Discord webhook
                          ├─ Email (SMTP)
                          └─ Generic JSON webhook
```

Git provider webhooks (`POST /webhooks/{github,gitlab,bitbucket,gitea}`)
verify their respective signature, look up the App by repo+branch,
and follow the same path — enqueue a deploy job, worker picks it
up, dispatches notifications on terminal state.

Templates (`POST /api/v1/pipelines/from-template/:id`) materialise
a new Pipeline row by deep-copying the template's schema with
fresh IDs; subsequent runs go through the same path as any
user-authored pipeline.

---

## Shutdown order

The HTTP server, run coordinator, jobqueue pool, scheduler, and
health checker are drained deterministically on `SIGTERM`:

```
1. HTTP drain (default 30s)
2. RunCoordinator.Wait (default 25s)
3. Jobqueue pool drain (workers finish in-flight jobs)
4. Scheduler drain (lock released; loop returns on ctx)
5. Health checker cancel + 5s wait
```

A timed-out drain logs a `Warn` but does not block the next step;
the boot-time orphan sweep recovers any in-flight state on the
next start.

---

## Configuration

| Env var | Default | What |
|---|---|---|
| `COOKER_JOBQUEUE_ENABLED` | `false` | Spawn worker pool + switch RunPipeline to Enqueue |
| `COOKER_JOBQUEUE_WORKERS` | `4` | Worker count |
| `COOKER_JOBQUEUE_WORKER_PREFIX` | `cooker` | Prefix for worker IDs in audit/observability |
| `COOKER_SCHEDULER_ENABLED` | `false` | Spawn cron loop (requires jobqueue enabled) |
| `COOKER_SCHEDULER_TICK` | `30s` | Tick interval; smaller = faster overdue catch, more queries |

Production `Config.Validate()` rejects `COOKER_SCHEDULER_ENABLED=true`
without `COOKER_JOBQUEUE_ENABLED=true`.

---

## See also

- [`docs/adapted-from-dokploy.md`](../proposals/adapted-from-dokploy.md) — what came from Dokploy and what didn't
- [`docs/architecture.md`](./architecture.md) — the rest of Cooker's architecture
- [`CHANGELOG.md`](../../CHANGELOG.md) — the Phase 1 + Phase 2 changelog entries
