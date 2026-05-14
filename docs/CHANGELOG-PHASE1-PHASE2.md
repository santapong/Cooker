# CHANGELOG snippet — Phase 1 + Phase 2

This is a ready-to-paste snippet for the main `CHANGELOG.md`. The
main file is large enough that an automated rewrite via
`push_files` (which replaces full contents) carries real risk of
content loss, so this lives as a companion file. Paste the
`### Added` block below into `## [Unreleased]` once the PR merges.

---

### Added — May 2026 W6 batch (PR #89: Phase 1 + Phase 2 Dokploy adaptation)

Closed in a 16-commit branch (`claude/analyze-dokploy-integration-NTrW3`).
All new subsystems are gated by default-off feature flags; merging
is a no-op for any operator who hasn't flipped `COOKER_JOBQUEUE_ENABLED`.
Full design rationale and "what we adapted from Dokploy" matrix in
[`docs/adapted-from-dokploy.md`](docs/adapted-from-dokploy.md) and
[`docs/architecture-phase1-phase2.md`](docs/architecture-phase1-phase2.md).

#### Phase 1 — architectural primitives

- **A1: Durable async job queue** (`internal/jobqueue/`). Postgres-native
  with `FOR UPDATE SKIP LOCKED` for lock-free pickup and `NOTIFY
  cooker_jobs_new` for near-instant worker wake-up. `EnqueueOptions.
  ConcurrencyKey` enforces per-key serialisation via a `NOT EXISTS`
  guard inside the dequeue query (contending jobs stay `pending`,
  not skipped permanently). Worker pool with panic-recover, capped
  exponential backoff with jitter, atomic `Reschedule` that
  transitions to terminal `failed` when `attempts >= max_attempts`.
  Three new Prometheus series: `cooker_jobqueue_depth{status}`,
  `cooker_jobqueue_attempts_total{kind}`,
  `cooker_jobqueue_run_duration_seconds{kind,outcome}`. Migration
  `010_jobs.up.sql`. Gated by `COOKER_JOBQUEUE_ENABLED=false` by
  default; when off, `RunPipeline` keeps using the inline
  `Runs.Spawn` path (pre-Phase-1 behaviour). Pattern adapted from
  Dokploy's BullMQ + Inngest dual queue but reimplemented
  Postgres-native to avoid making Redis mandatory.

- **A2: Run + stage state machine** (`internal/runstate/`). Formal
  transition-table FSM (`fsm.go`, `run.go`, `stage.go`,
  `transition.go`) with typed `ErrInvalidTransition`. State
  alphabet is pinned to `model.RunStatus` (`pending`, `running`,
  `success`, `failed`, `cancelled`) via a test assertion so a
  future rename can't drift the two. `TransitionRun(current,
  event)` adapter returns the input state unchanged on error so
  callers that ignore the error (against advice) don't corrupt the
  column. Terminal-sticky property covered by exhaustive tests.
  In-house 80-LOC FSM rather than `github.com/looplab/fsm` because
  `push_files` over the GitHub MCP can't run `go mod tidy` to
  populate `go.sum`; semantic guarantee is identical and a swap is
  mechanical.

- **A3: Resource-action permission middleware**
  (`internal/auth/permission.go`). `Resource` and `Action` typed
  constants, role × resource × action matrix with deny-by-default
  for undeclared pairs. `RequirePermission(resource, action)` Gin
  middleware applied at route registration alongside the existing
  `RequireRole` / `RequireMFA` (belt-and-braces: dropping one gate
  by accident still leaves the others enforcing). Three sensitive
  routes adopted: `POST /pipelines/:id/run` (`(pipeline, invoke)`),
  `GET /environments/:id/secrets/:key` (`(secret, reveal)`),
  `PUT /apps/:id/webhook` (`(webhook, update)`). Remaining routes
  adopt incrementally without a flag day. Pattern adapted from
  Dokploy's tRPC `withPermission` middleware.

#### Phase 2 — feature gaps

- **F1: Multi-channel notification fan-out** (`internal/notifier/`).
  Four channel adapters — Slack (incoming-webhook), Discord
  (color-coded embed), generic Webhook (JSON POST with optional
  bearer token + arbitrary headers), Email (SMTP via stubbable
  `SMTPSender`). `Dispatcher` fans out concurrently with
  `errors.Join` for per-target failures and per-target
  `SendTimeout` (default 10s) so a hung webhook can't stall the
  executor. Per-event-type `text/template` rendering. `Target`
  rows carry an `event_types` array filter (empty = all events)
  for per-target event scoping. Migration
  `011_notification_targets.up.sql`. Dispatched by
  `service.JobQueueRunner.Handle` on terminal run status, so a
  slow Slack webhook can't slow run completion. Adapted from
  Dokploy's `packages/server/src/utils/notifications/*.ts` but
  with an explicit `Notifier` interface + `Dispatcher` rather
  than Dokploy's chained if-else.

- **F2: Cron-triggered pipeline runs** (`internal/scheduler/`).
  In-house 5-field POSIX cron parser (`cron.go`) supporting `*`,
  lists, ranges, steps; operates in the schedule's IANA location
  for DST-correct firing. Search capped at 4 years to bail on
  pathological expressions like `"0 0 30 2 *"`. Runner uses
  `pg_try_advisory_lock` on a dedicated connection for continuous
  (not one-shot) leader election; non-leader replicas wait and
  take over if the leader exits. Per-tick: scan `schedules` for
  `enabled AND next_run_at <= NOW()`, enqueue a pipeline-run job
  through the existing `JobQueueEnqueuer`, atomically `MarkFired`
  with computed `next_run_at`. Bad cron expressions disable the
  row rather than looping forever; transient enqueue failures
  retry on the next tick. Migration `012_schedules.up.sql`. Gated
  by `COOKER_SCHEDULER_ENABLED=false` by default; production
  `Config.Validate()` rejects `SCHEDULER_ENABLED=true` without
  `JOBQUEUE_ENABLED=true`. Adapted from Dokploy's `apps/schedules`
  BullMQ repeatable jobs + leader-elected via Cooker's existing
  `pg_advisory_lock` primitive.

- **F3: GitLab + Bitbucket Server + Gitea webhook receivers**
  (`internal/source/{gitlab,bitbucket,gitea}/`, handlers
  `internal/handler/webhook_{gitlab,bitbucket,gitea}.go`). Each
  package mirrors the existing `internal/source/github` shape:
  `PushEvent` with `FullName()` / `Branch()` / `IsBranchDelete()`,
  plus the provider-specific signature verifier. GitLab uses a
  literal `X-Gitlab-Token` (no HMAC); Bitbucket Server and Gitea
  use HMAC-SHA256 with different header naming conventions
  (Bitbucket prefixes `sha256=`, Gitea sends a raw hex digest).
  All comparisons use `subtle.ConstantTimeCompare` / `hmac.Equal`
  for timing-safe verification. Three new endpoints,
  `POST /webhooks/{gitlab,bitbucket,gitea}`, mirroring the existing
  `/webhooks/github` flow exactly (size-bound body read, event-type
  header filter, repo+branch lookup, secret decryption via
  `Codec.Open`). Bitbucket Cloud not supported in v1 (Atlassian
  doesn't sign Cloud webhooks; IP allowlist expected). The App
  model's existing `GitHubRepo` field acts as a provider-agnostic
  repo identifier; renaming to `Repo` + adding a `Provider` enum
  is a v2 migration. Adapted from Dokploy's
  `services/{gitlab,bitbucket,gitea}.ts`.

- **F4: Pipeline templates v1** (`internal/templates/`). A
  `pipeline_templates` table carries reusable Pipeline-shaped JSONB
  schemas. New endpoints: `GET /api/v1/templates` (gallery, enabled
  only), `GET /api/v1/templates/:id` (single template + schema),
  `POST /api/v1/pipelines/from-template/:id` (create-from-template).
  The create-from-template flow deep-copies the template's schema
  with fresh stage IDs (re-mapping edges accordingly) and
  re-validates through the existing `service.ValidatePipelineDAG`
  seam so a broken template surfaces as `400 Bad Request` rather
  than a successful create of a non-runnable row. Operators seed
  templates via SQL in v1 (admin CRUD endpoints + frontend gallery
  are follow-ups). Migration `013_pipeline_templates.up.sql`.
  Pattern adapted from Dokploy's `apps/dokploy/templates/` but
  reshaped from Compose-stack-shaped to Pipeline-shaped (Cooker is
  CI/CD, not PaaS).

#### Integration + safety

- **Default-off feature flags throughout**. `COOKER_JOBQUEUE_ENABLED`
  and `COOKER_SCHEDULER_ENABLED` both default `false`. With both
  off, the runtime behaviour is byte-identical to pre-Phase-1
  except for two no-op nil-checks. With both on, the runtime path
  is: cron tick or HTTP RunPipeline → `JobQueueEnqueuer.EnqueueRun`
  → `INSERT INTO jobs` + `NOTIFY` → worker `Dequeue` →
  `JobQueueRunner.Handle` → `Executor.Execute` + `store.Runs.Update`
  → `Notifier.Dispatch` to enabled targets.

- **Deterministic shutdown order**. HTTP drain → run coordinator
  drain → jobqueue pool drain → scheduler drain → health checker
  drain. Each step has its own timeout; a timed-out drain logs
  `Warn` but does not block the next step.

- **Self-review pass** (commit `1570a79`). Caught one P0 bug (the
  runstate state alphabet originally included `queued` /
  `timed_out` that don't exist in `model.RunStatus`, fixed before
  merge) and one P1 timer leak in `PqListener.Wait`. Added
  benchmarks for the hot paths: jobqueue Enqueue / Dequeue,
  ExponentialBackoff.NextDelay, RunFSM.Apply, notifier template
  rendering.

#### Migrations added

- `010_jobs.up.sql` — durable async job queue
- `011_notification_targets.up.sql` — multi-channel notifier targets
- `012_schedules.up.sql` — cron schedules
- `013_pipeline_templates.up.sql` — template catalog

#### Files added (high-level)

```
backend/internal/jobqueue/         queue store + worker pool + NOTIFY + backoff + benchmarks
backend/internal/runstate/         FSM + run/stage builders + adapters
backend/internal/notifier/         dispatcher + 4 channel adapters + targets + templates
backend/internal/scheduler/        cron parser + leader-elected runner + schedules store
backend/internal/templates/        template catalog
backend/internal/source/gitlab/    GitLab Push Hook + token verify
backend/internal/source/bitbucket/ Bitbucket Server repo:refs_changed + HMAC verify
backend/internal/source/gitea/     Gitea push + HMAC verify
backend/internal/auth/permission.go             resource×action matrix + middleware
backend/internal/handler/webhook_{gitlab,bitbucket,gitea}.go   3 new webhook receivers
backend/internal/handler/templates.go           templates handler
backend/internal/server/jobqueue_boot.go        jobqueue + notifier boot helper
backend/internal/server/scheduler_boot.go       scheduler boot helper
backend/internal/server/templates_boot.go       templates boot helper
backend/internal/service/jobqueue_runner.go     pipeline-run worker handler
backend/internal/service/enqueuer.go            JobEnqueuer impl
docs/adapted-from-dokploy.md                    full pattern attribution
docs/architecture-phase1-phase2.md              new subsystem architecture
```

#### Files touched (additive only)

- `backend/internal/handler/handler.go` — added `Enqueuer` and
  `Templates` fields (both optional; nil falls back to pre-Phase-1
  behaviour)
- `backend/internal/handler/pipeline.go` — `RunPipeline` branches
  on `h.Enqueuer != nil`; inline path unchanged when nil
- `backend/internal/server/server.go` — calls the three new
  `boot*` helpers, attaches deps to the handler, spawns Pool.Run /
  Scheduler.Run alongside the HTTP server, drains them in order
- `backend/internal/server/router.go` — added 3 webhook routes
  + 2 templates routes + 1 from-template route; adopted
  `RequirePermission` on 3 sensitive routes
- `backend/internal/config/config.go` — added `JobQueueConfig` and
  `SchedulerConfig` with env-var loading and production-mode
  validation
- `backend/internal/observability/observability.go` — added 3
  jobqueue Prometheus series + `cooker_notifier_sent_total{channel,outcome}`
- `backend/internal/store/postgres/migrations/010..013_*.sql` —
  four new migrations

#### Tests added

Unit tests across every new package; benchmarks for hot paths.
Live-DB integration tests for jobqueue + scheduler + notifier
under multi-worker load deferred to a follow-up that comes with
the docker-compose CI changes.
