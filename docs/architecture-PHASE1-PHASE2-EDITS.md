# docs/architecture.md edits guide — Phase 1 + Phase 2

Same premise as [`README-PHASE1-PHASE2-EDITS.md`](./README-PHASE1-PHASE2-EDITS.md):
the main `architecture.md` is large (~22 KB) and `push_files` over
the MCP replaces full file contents, so a rewrite carries real risk
of accidentally dropping unrelated content. This file holds the
exact additions a maintainer applies manually — each is purely
additive, no existing line needs to move.

The full subsystem detail lives in
[`docs/architecture-phase1-phase2.md`](./architecture-phase1-phase2.md);
the edits below cross-link it from the main architecture document
so a reader landing on `docs/architecture.md` finds the new content.

---

## Edit 1 — add a top-level pointer to the Phase 1 + Phase 2 doc

**Anchor (find the table of contents / introduction at the top of `docs/architecture.md`):**

This edit goes at the **very top of the document, immediately after
the H1 heading and any subtitle line**. The exact location depends
on your existing introduction; the goal is to make this the first
thing a reader sees so they don't miss the Phase 1+2 work when
looking up subsystems.

**Insert:**

```markdown
> **Phase 1 + Phase 2 (May 2026 W6):** This document covers Cooker's
> stable architecture. Five new subsystems landed in PR #89 —
> durable job queue, run-state FSM, permission middleware, notifier,
> cron scheduler — and are documented separately in
> [`docs/architecture-phase1-phase2.md`](./architecture-phase1-phase2.md).
> They are gated by default-off feature flags
> (`COOKER_JOBQUEUE_ENABLED`, `COOKER_SCHEDULER_ENABLED`) so this
> document remains accurate for any deployment that hasn't opted
> in. The full Dokploy attribution matrix is in
> [`docs/adapted-from-dokploy.md`](./adapted-from-dokploy.md).
```

---

## Edit 2 — add a "New subsystems" section near the end

**Anchor (find this near the end of `docs/architecture.md`).** Pick
whichever of these you find first (different `architecture.md`
versions have different trailing sections):

```markdown
## Future work
```

or

```markdown
## See also
```

or the last section before any appendices.

**Insert a new section immediately BEFORE that anchor:**

```markdown
## New subsystems (Phase 1 + Phase 2)

Five subsystems were added in PR #89. Each is default-off and
additive — the existing components below this section behave
identically until an operator flips the feature flag.

| Subsystem | Package | Migration | Feature flag |
|---|---|---|---|
| Durable job queue | `internal/jobqueue/` | `010_jobs` | `COOKER_JOBQUEUE_ENABLED` |
| Run / stage FSM | `internal/runstate/` | — | (always loaded; only the executor migration is gated) |
| Resource-action permissions | `internal/auth/permission.go` | — | (always loaded; routes adopt incrementally) |
| Multi-channel notifier | `internal/notifier/` | `011_notification_targets` | (always loaded; off when no targets configured) |
| Cron scheduler | `internal/scheduler/` | `012_schedules` | `COOKER_SCHEDULER_ENABLED` |
| Pipeline templates | `internal/templates/` | `013_pipeline_templates` | (always loaded; endpoints 503 when no DB) |

The interaction shape:

```
cron tick OR HTTP /pipelines/:id/run
        │
        ▼
JobQueueEnqueuer.EnqueueRun(pipelineID, runID)
        │
        ▼                                  ┌─ worker pool (N goroutines)
INSERT jobs + NOTIFY cooker_jobs_new ──────▼
                                          worker.Dequeue()  (FOR UPDATE SKIP LOCKED)
                                          │
                                          ▼
                                  JobQueueRunner.Handle
                                    ├─ Executor.Execute (unchanged)
                                    ├─ Store.Runs.Update
                                    └─ Notifier.Dispatch ─────────────────▶ Slack / Discord / Email / Webhook
```

Deep-dive: [`docs/architecture-phase1-phase2.md`](./architecture-phase1-phase2.md).
Attribution: [`docs/adapted-from-dokploy.md`](./adapted-from-dokploy.md).
```

---

## Edit 3 — if `architecture.md` has a layering / component diagram, add the new packages

**Anchor (find a section like "Component map", "Internal layout",
"Package structure", or similar that lists `internal/` packages).**

**Append the following rows / lines** to whatever table or list
describes the `internal/` packages:

```markdown
| `internal/jobqueue/`   | Durable Postgres job queue: store, worker pool, `pq.NewListener`, exponential backoff |
| `internal/runstate/`   | Run + stage state machine (FSM with typed invalid-transition errors) |
| `internal/notifier/`   | Multi-channel dispatcher: Slack / Discord / Email / Webhook adapters |
| `internal/scheduler/`  | Leader-elected cron-triggered runs (`pg_advisory_lock` + in-house POSIX cron parser) |
| `internal/templates/`  | Pipeline-template catalog + create-from-template flow |
| `internal/source/gitlab/`    | GitLab Push Hook parser + `X-Gitlab-Token` verifier |
| `internal/source/bitbucket/` | Bitbucket Server `repo:refs_changed` parser + HMAC-SHA256 verifier |
| `internal/source/gitea/`     | Gitea push parser + raw-hex HMAC-SHA256 verifier |
```

If the existing diagram is text/ASCII rather than a table, insert
the corresponding lines into the diagram — the convention used by
the file is the right convention to follow.

---

## Edit 4 — if `architecture.md` has a sequence diagram for "running a pipeline", update it

This edit is **conditional** — only apply if such a sequence diagram
exists in the current document. If not, skip this edit.

**Anchor (find a section / diagram describing the
`RunPipeline` HTTP flow).**

**Add a parallel diagram (or annotation) showing the new branch:**

```
   Old path (still default):
   HTTP /pipelines/:id/run
        │
        ▼
   handler.RunPipeline
        │  store.Runs.Create
        ▼  RunCoordinator.Spawn(...)
        Executor.Execute (inline goroutine)

   New path (COOKER_JOBQUEUE_ENABLED=true):
   HTTP /pipelines/:id/run
        │
        ▼
   handler.RunPipeline
        │  store.Runs.Create
        ▼  h.Enqueuer.EnqueueRun(pipelineID, runID)
        │
        ▼
   INSERT INTO jobs + NOTIFY ──▶ worker pool ──▶ JobQueueRunner.Handle ──▶ Executor.Execute
                                                          └──▶ Notifier.Dispatch
```

The handler returns `202 Accepted` in both paths; the client
observes the eventual outcome via the existing
`GET /pipelines/:id/runs/:runId` endpoint or the WebSocket
`/ws/runs/:runId` channel — neither has changed.

---

## Edit 5 — add the deterministic shutdown order if the doc has a lifecycle section

This edit is **conditional** — only apply if `architecture.md`
documents the shutdown / drain order.

**Anchor (find the shutdown / drain order section).**

**Replace the existing drain order with:**

```markdown
On `SIGTERM`, `Server.RunContext` drains in this order:

1. **HTTP server** — `httpSrv.Shutdown(drainCtx)` with default 30s timeout
2. **Run coordinator** — waits for in-flight tracked goroutines (default 25s)
3. **Job queue pool** — workers complete their current job; new dequeues stop (Phase 1)
4. **Scheduler** — advisory lock released; loop exits on ctx cancel (Phase 2)
5. **App health checker** — cancel + 5s wait

Each step has its own timeout. A timed-out step logs `Warn` but
does not block subsequent steps; the boot-time orphan sweep
recovers any half-finished run rows on the next start.
```

---

## Verification

After applying the edits, check:

```bash
grep -c 'Phase 1 + Phase 2' docs/architecture.md
# Expect: at least 2 (the top pointer + the new subsystems section)

grep -c 'COOKER_JOBQUEUE_ENABLED' docs/architecture.md
# Expect: at least 2 (subsystems table + feature flag reference)
```

The new content should cross-link cleanly to
`docs/architecture-phase1-phase2.md` and
`docs/adapted-from-dokploy.md`. No existing section should have been
deleted.

Once applied, this edits-guide doc can be removed; the same content
lives in the linked companion docs.
