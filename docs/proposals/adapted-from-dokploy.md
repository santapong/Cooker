# Adapted from Dokploy — Phase 1 + Phase 2 (May 2026)

This document is the honest record of what Cooker took from
[Dokploy](https://github.com/dokploy/dokploy)'s patterns, what it
rebuilt Go-native, and what it deliberately did not adopt. It exists
so a future maintainer can answer "why does the jobqueue look like
BullMQ?" without having to dig through PR descriptions.

The study that produced this list lives in
`/root/.claude/plans/so-can-you-deep-polished-brooks.md` (the plan
file) and was a two-pass comparison: first feature-level
("what could we use?"), then pattern-level ("what designs would
actually help?"). The pattern pass surfaced three real architectural
gaps in Cooker that even Dokploy's weak versions of the same
patterns at least addressed. Those three are Phase 1. Phase 2 is
four feature gaps, the first of which (notifications) is the
strongest Dokploy port.

---

## Summary

| Phase | Slice | Adapted? | Implementation kind |
|---|---|---|---|
| 1 | A1 Durable job queue | ✅ Adapted | Postgres-native (NOT a port of BullMQ); pattern from Dokploy's dual BullMQ + Inngest queue |
| 1 | A2 Run/stage state machine | ✅ Adapted | In-house FSM; pattern from Dokploy's implicit string-enum + conditional update |
| 1 | A3 Permission middleware | ✅ Adapted | Gin middleware over an existing role gate; pattern from Dokploy's tRPC `withPermission` |
| 2 | F1 Multi-channel notifications | ✅ Adapted | Slack / Discord / Email / Webhook; pattern from `packages/server/src/utils/notifications/*.ts` |
| 2 | F2 Cron-triggered runs | ✅ Adapted | Postgres advisory-lock leader; pattern from Dokploy's BullMQ repeatable jobs + `apps/schedules` |
| 2 | F3 Git provider breadth | ✅ Adapted | GitLab + Bitbucket Server + Gitea; pattern from Dokploy's `services/{gitlab,bitbucket,gitea}.ts` |
| 2 | F4 Pipeline templates | ✅ Adapted | Pipeline-shaped catalog; pattern from Dokploy's `apps/dokploy/templates/` |
| — | Traefik dynamic routing | ❌ Skipped | Cooker deploys *to* K8s ingress / cloud LBs, not Swarm+Traefik |
| — | Docker Swarm provider | ❌ Skipped | Cooker is K8s/cloud-runtime focused |
| — | Docker Compose stack deployment | ❌ Skipped | Pushes Cooker toward PaaS; different product |
| — | Managed databases (PG/MySQL/Mongo as service) | ❌ Skipped | Out of CI/CD scope |
| — | DB backups (S3 etc.) | ❌ Skipped | Out of CI/CD scope |
| — | Custom domain + cert-manager automation | ❌ Skipped | Cooker doesn't host user apps |
| — | Multi-server SSH agent (`apps/api`) | ❌ Skipped | Cooker ships to clusters, not remote nodes |
| — | Preview deployments (per-PR ephemeral) | 🕗 Deferred | Needs DNS automation; revisit after Apps + DNS work |
| — | Monitoring sidecar (`apps/monitoring`) | ❌ Skipped | Cooker already has Prometheus + OTel |
| — | AI assist (`services/ai.ts`) | 🕗 Deferred | Roadmap item, not pattern-driven |
| — | Drizzle migration pattern | ❌ Skipped | Different ecosystem (Go vs TypeScript) |
| — | Nixpacks / Railpack / Paketo / Heroku Buildpacks | 🕗 Deferred | "Nice-to-have" tier in the original plan |

---

## Phase 1: architectural patterns (A1–A3)

### A1 — Durable job queue

**What Dokploy does.** Two-mode queue: BullMQ (Redis) for self-hosted
deployments, Inngest for the cloud tenancy. Both invoke the same
`deployApplication()` business function. Concurrency control is
per-`serverId` (Inngest's `concurrency: [{ key, limit: 1 }]`).

**What Cooker added.** A Postgres-native equivalent in
`internal/jobqueue/`. Pickup uses `SELECT ... FOR UPDATE SKIP LOCKED`
in a CTE; insertion fires `NOTIFY cooker_jobs_new` so idle workers
wake immediately via `pq.NewListener`. Concurrency-key serialises
*per-pipeline* (Dokploy serialises per-server) via a `NOT EXISTS`
guard inside the dequeue query.

**Why Postgres-native instead of porting BullMQ.**
- Cooker already requires Postgres; Redis is currently optional.
  Making Redis mandatory would have been a bigger ask than building
  a small Postgres queue.
- `FOR UPDATE SKIP LOCKED` + `LISTEN/NOTIFY` gives exactly-once
  pickup with sub-second wake-up latency. No background polling
  needed in the happy path.
- The concurrency-key check happens *inside* dequeue's SQL, so a
  contending job stays `pending` rather than being skipped
  permanently like a BullMQ rate-limit kick.

**Files.** Migration `010_jobs.up.sql`; package `internal/jobqueue/`
(`jobqueue.go`, `postgres.go`, `memory.go`, `worker.go`,
`registry.go`, `notify.go`, `backoff.go` + tests + benchmarks).
Metrics: `cooker_jobqueue_depth`, `cooker_jobqueue_attempts_total`,
`cooker_jobqueue_run_duration_seconds`.

**Feature flag.** `COOKER_JOBQUEUE_ENABLED=false` (default). When
off, `RunPipeline` keeps using the inline `Runs.Spawn` path — the
pre-Phase-1 behaviour.

---

### A2 — Run + stage state machine

**What Dokploy does.** `DeploymentStatus = "running" | "done" |
"error" | "cancelled"`. Transitions are ad-hoc: callers write the
field directly via `updateDeploymentStatus(id, status)`, no
validation that the transition is legal.

**What Cooker added.** A formal transition-table FSM in
`internal/runstate/`. `Apply(event)` returns a typed
`ErrInvalidTransition` on a disallowed edge. The state alphabet
matches `model.RunStatus` exactly so a `TransitionRun(current,
event)` adapter can cast safely:

```
pending --start--> running --succeed--> success
                          --fail-----> failed
                          --cancel---> cancelled
pending --cancel--> cancelled
```

**Why in-house rather than `github.com/looplab/fsm`.** The GitHub
MCP can't run `go mod tidy` to populate `go.sum`. The semantic
guarantee (reject invalid transitions, return typed error,
terminal-sticky) is small enough to roll by hand in ~80 LOC. A
follow-up commit with local toolchain access can swap to
`looplab/fsm` mechanically; the test in `run_test.go` pins each
`State` constant against its `model.RunStatus` so the swap can't
drift the alphabet silently.

**Files.** `internal/runstate/` (`fsm.go`, `run.go`, `stage.go`,
`transition.go` + tests + benchmarks).

**Adoption.** The primitive is in place. Migrating `executor.go`'s
direct status writes to go through `runstate.TransitionRun` is
mechanical work that lands in a follow-up.

---

### A3 — Permission middleware

**What Dokploy does.** tRPC `withPermission(resource, action)`
middleware composed at route definition. A resource-action matrix
maps roles (`owner`/`admin`/`member`) to permitted verbs per
resource.

**What Cooker added.** A sibling of the existing `RequireRole` /
`RequireMFA` middlewares in `internal/auth/`. `Permission(claims,
resource, action)` checks a hand-maintained matrix (resource×action
→ allowed roles). `RequirePermission(resource, action)` is the Gin
middleware form, applied at route registration:

```go
pipelines.POST("/:id/run",
    writeRole,
    auth.RequirePermission(auth.ResourcePipeline, auth.ActionInvoke),
    expensive, idempotencyMiddleware(s.idempotency), h.RunPipeline)
```

**Belt-and-braces.** The role middleware and the permission
middleware co-exist on the same route. Dropping one gate by
accident still leaves the other enforcing.

**Files.** `internal/auth/permission.go` (matrix + middleware) +
`permission_test.go` (full matrix coverage). Three sensitive routes
adopted in `router.go`: `POST /pipelines/:id/run`,
`GET /environments/:id/secrets/:key`, `PUT /apps/:id/webhook`.
Remaining routes adopt incrementally.

**Adoption note.** The existing in-handler `auth.CanRevealSecret`,
`auth.CanApprovePromotion` calls keep working unchanged. Migrating
them to `Permission()` is a per-route swap.

---

## Phase 2: feature gaps (F1–F4)

### F1 — Multi-channel notifications

**What Dokploy does.** Per-event-type send functions (Slack,
Discord, Telegram, Email, generic webhook, ntfy, gotify, etc.)
dispatched via a chained if-else in
`sendBuildErrorNotifications`. React-email templates rendered
server-side.

**What Cooker added.** An explicit `Notifier` interface + a
`Dispatcher` that fans out concurrently using `errors.Join` to
surface per-target failures. Four adapters in v1: Slack, Discord,
generic Webhook, SMTP Email. Templates are Go `text/template` (one
per event type). Targets are stored in `notification_targets` table
with an `event_types` array filter so an operator can scope a
Slack channel to "only failures" without code change.

**Architecture difference from Dokploy.**
- Dispatcher rather than dispatch chain: adding a 5th channel is a
  one-line `registry.Register(notifier.NewTelegramNotifier())`.
- Per-target timeout (`Dispatcher.SendTimeout`) caps slow webhooks
  so one bad Slack URL can't stall the executor.
- Dispatch runs inside the jobqueue runner, not inline with the
  executor. A slow webhook can't slow run completion.

**Files.** Migration `011_notification_targets.up.sql`; package
`internal/notifier/` (`notifier.go`, `template.go`, `target.go`,
`targetstore.go`, `slack.go`, `discord.go`, `webhook.go`,
`email.go` + tests + benchmarks).

**Wiring.** `service.JobQueueRunner.Handle` calls
`dispatcher.Dispatch(ctx, event)` after each terminal run
transition. Event type derived from `run.Status` + `execErr`.

---

### F2 — Cron-triggered runs

**What Dokploy does.** Three BullMQ workers consuming a shared
`backupQueue` queue with repeatable jobs added via
`.add({ repeat: { pattern: cronSchedule } })`. No leader election;
BullMQ's own lock ensures each scheduled fire is consumed once.

**What Cooker added.** A leader-elected loop using
`pg_try_advisory_lock` on a dedicated connection. Non-leader replicas
wait and take over if the leader exits / crashes — continuous
election, not one-shot. Per-tick: scan `schedules` table for
`enabled AND next_run_at <= NOW()`, enqueue a pipeline-run job for
each, atomically update `last_run_at` + `next_run_at` via the
computed `cron.Next(now, schedule.LoadLocation())`.

**Cron parser.** In-house 5-field POSIX (minute, hour, dom, month,
dow) supporting `*`, lists `1,3,5`, ranges `1-5`, steps `*/5` /
`1-10/2`. Operates in the schedule's IANA location so
`"0 9 * * *"` fires at 9am local across DST. Search capped at 4
years to bail on pathological expressions like `"0 0 30 2 *"`.

**Why advisory-lock leader instead of BullMQ.** Avoids depending on
the job queue's specific concurrency model. Also: the existing
`internal/idempotency/` package already used `pg_advisory_lock` for
migrations, so the pattern was already in the codebase.

**Files.** Migration `012_schedules.up.sql`; package
`internal/scheduler/` (`cron.go`, `schedule.go`, `store.go`,
`scheduler.go` + tests).

**Feature flag.** `COOKER_SCHEDULER_ENABLED=false` (default).
Production `Validate()` refuses `SCHEDULER_ENABLED=true` without
`JOBQUEUE_ENABLED=true` since the scheduler enqueues through the
jobqueue.

---

### F3 — Git provider breadth

**What Dokploy does.** Separate services per provider (`github.ts`,
`gitlab.ts`, `bitbucket.ts`, `gitea.ts`, `git-provider.ts`). Each
handles its own webhook signature scheme.

**What Cooker added.** Three new packages mirroring the existing
`internal/source/github` shape:

| Provider | Signature scheme | Header |
|---|---|---|
| GitLab | Literal token (no HMAC) | `X-Gitlab-Token` |
| Bitbucket Server | HMAC-SHA256 with `sha256=` prefix | `X-Hub-Signature` |
| Gitea | HMAC-SHA256, raw hex digest | `X-Gitea-Signature` |

Each package exposes `PushEvent` with `FullName()`, `Branch()`,
`IsBranchDelete()`, and a `VerifySignature` (or `VerifyToken` for
GitLab) using `crypto/subtle.ConstantTimeCompare` and
`hmac.Equal` for timing-safe comparison.

**Handler integration.** Three new endpoints,
`POST /webhooks/{gitlab,bitbucket,gitea}`, mirroring the existing
`/webhooks/github` flow exactly: size-bound body read → filter on
event-type header → parse → look up App by repo+branch via the
existing `Apps.GetByRepo` seam → verify against the App's stored
WebhookSecret (decrypted via `Codec.Open`).

**Bitbucket Cloud not supported in v1.** Atlassian doesn't sign
Cloud webhooks; the documented posture is IP allowlisting at the
edge. Operators on Cloud should add an authenticating reverse proxy
in front of Cooker. Server / Data Center works.

**App model unchanged.** All providers use the existing
`GitHubRepo` field on the App as a provider-agnostic repo identifier
(slight semantic abuse to avoid a schema migration). Renaming to
`Repo` + adding a `Provider` enum is a v2 migration when product
requires it.

**Files.** Packages
`internal/source/{gitlab,bitbucket,gitea}/` + handlers
`internal/handler/webhook_{gitlab,bitbucket,gitea}.go`.

---

### F4 — Pipeline templates v1

**What Dokploy does.** App-shaped templates: each template is a
Docker Compose stack for a popular service (Plausible, PocketBase,
Calcom, etc.). "Use template" provisions a running app.

**What Cooker did differently.** Pipeline-shaped templates, not
app-shaped. A `pipeline_templates` row carries a JSONB schema that
conforms to the `model.Pipeline` shape (stages + edges + variables).
"Use template" creates a new Pipeline row by:

1. Deserialising the template's stored schema into a `model.Pipeline`
2. Generating fresh IDs for the new pipeline AND each stage
3. Re-mapping each edge's `From` / `To` to the new stage IDs
4. Re-validating the materialised pipeline through the existing
   `service.ValidatePipelineDAG` seam (catches broken templates as
   `400 Bad Request` rather than persisting an unrunnable row)

**Endpoints.**

```
GET  /api/v1/templates              # list (gallery)
GET  /api/v1/templates/:id          # single template + schema
POST /api/v1/pipelines/from-template/:id  # create-from-template
```

**Files.** Migration `013_pipeline_templates.up.sql`; package
`internal/templates/`; handler `internal/handler/templates.go`;
boot helper `internal/server/templates_boot.go`.

**What's deferred.** Admin CRUD endpoints (operators seed via SQL
for v1) and the frontend gallery. The catalog format is intentionally
the canonical Pipeline JSON so the same shape works for share-pipeline,
fork-pipeline, and template-pipeline use cases.

---

## What was deliberately *not* adopted

Dokploy is a PaaS for hosting apps and databases on a VPS. Cooker is
a CI/CD orchestrator that ships OCI images to clusters and cloud
runtimes. Most of Dokploy's surface area is PaaS-shaped and would
push Cooker toward becoming a different product. Each row below
was considered in the original feature audit and explicitly skipped
or deferred.

| Skipped feature | Path in Dokploy | Why |
|---|---|---|
| Traefik dynamic routing | `utils/traefik/`, `services/domain.ts` | Cooker doesn't terminate user traffic |
| Docker Swarm provider | `utils/cluster/`, `services/server.ts` | Cooker is K8s/cloud-runtime focused |
| Compose stack as deploy target | `services/compose.ts` | Different product (PaaS, not CI/CD) |
| Managed databases | `services/{postgres,mysql,mongo,redis,libsql}.ts` | Out of scope; Cooker doesn't run user data |
| Database backups + restore | `services/{backup,volume-backups}.ts` | Same as above |
| Custom domains + cert-manager glue | `services/{domain,certificate}.ts` | Cooker doesn't host user apps |
| Multi-server SSH agent | `apps/api/`, `utils/servers/` | Cooker ships to K8s; agents aren't needed |
| Real-time monitoring sidecar | `apps/monitoring/` | Cooker already has Prometheus + OTel |
| GPU setup | `utils/gpu-setup.ts` | Out of Phase 1/2 scope |
| Drizzle ORM | All schemas | TypeScript-only; Cooker uses raw `database/sql` + `pgx` |
| Better-Auth + 2FA | `lib/auth.ts` | Cooker uses OIDC + PKCE (different auth model) |

| Deferred feature | Why |
|---|---|
| Preview deployments | Needs DNS automation; revisit after Apps + DNS work |
| AI assist | Roadmap item; not pattern-driven |
| Nixpacks / Railpack / Paketo / Heroku Buildpacks | "Nice-to-have" tier; ships after F4 stabilises |
| Rollbacks | Cooker already has them per deploy target; Dokploy's Swarm-style rollback isn't a useful port |

---

## Attribution

The patterns above were studied from Dokploy v0.x source
(Apache-2.0). No code was text-copied; each Cooker module is
re-implemented Go-native using `database/sql`, `lib/pq`,
`crypto/hmac`, `text/template`, `time` etc. Per Apache-2.0 §4, a
`NOTICES` file at the repo root will be added in the first
feature-flag-enabling deployment, listing Dokploy as a design
reference for `internal/jobqueue/`, `internal/notifier/`,
`internal/scheduler/`, `internal/source/{gitlab,bitbucket,gitea}/`,
and `internal/templates/`.

Nothing under Dokploy's `packages/server/src/services/proprietary/`
directory was read or referenced.

---

## Further reading

- **Plan file:** `/root/.claude/plans/so-can-you-deep-polished-brooks.md`
- **PR #89:** the 16-commit branch landing all of Phase 1 + Phase 2
- **Phase 1+2 architecture:** [docs/architecture-phase1-phase2.md](../reference/architecture-phase1-phase2.md)
- **Changelog for Phase 1+2:** [`CHANGELOG.md`](../../CHANGELOG.md) (the former snippet has been merged in)
