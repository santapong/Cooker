# README.md edits — Phase 1 + Phase 2

This file holds the exact additions to make to the main
[`README.md`](../README.md) for the Phase 1 + Phase 2 work. The
main README is 41 KB and `push_files` over the GitHub MCP replaces
*full* file contents — a rewrite carries real risk of accidentally
dropping unrelated content, so the safer path is to keep the diff
here and have a human apply it.

Every insertion below is purely additive: no existing line needs to
move or be deleted. Six anchor strings + six insertions, applied in
order.

---

## Edit 1 — add the "Recently shipped" callout to the Overview

**Anchor (find this existing line in `README.md`):**

```markdown
> **Status:** production-ready on single-replica and multi-replica (Redis-backed) deployments. `Config.Validate` refuses unsafe boots in production. See the [rollout playbook](docs/ROLLOUT.md) for the UAT → production cutover.
```

**Insert immediately after that line:**

```markdown

> **Recently shipped (May 2026 W6):** Phase 1 + Phase 2 of the Dokploy adaptation work landed in PR #89 — a durable Postgres-backed job queue, formal run/stage state machine, resource-action permission middleware, multi-channel notifications (Slack/Discord/Email/Webhook), cron-triggered runs, GitLab/Bitbucket/Gitea webhook receivers, and a pipeline templates catalog. All gated behind default-off feature flags; merging is a no-op until an operator opts in. See [`docs/adapted-from-dokploy.md`](docs/adapted-from-dokploy.md) for the attribution matrix and [`docs/architecture-phase1-phase2.md`](docs/architecture-phase1-phase2.md) for the new subsystem deep-dive.
```

---

## Edit 2 — add the new env vars to the Common Environment Variables table

**Anchor (find this existing row in `README.md`):**

```markdown
| `COOKER_STICKY_SESSIONS` | `false` | Set `true` if your ingress pins clients to the same replica |
```

**Append the following four rows immediately after it (still inside the same table):**

```markdown
| `COOKER_JOBQUEUE_ENABLED` | `false` | Phase 1: spawn the durable job-queue worker pool and switch `RunPipeline` to async enqueue |
| `COOKER_JOBQUEUE_WORKERS` | `4` | Worker goroutine count (only when jobqueue enabled) |
| `COOKER_SCHEDULER_ENABLED` | `false` | Phase 2 F2: spawn the cron-triggered runs loop (requires `COOKER_JOBQUEUE_ENABLED=true`) |
| `COOKER_SCHEDULER_TICK` | `30s` | Scheduler tick interval; smaller = faster overdue catch, more queries |
```

---

## Edit 3 — add a Documentation section pointer to the new docs

**Anchor (find this existing line in `README.md`'s "For contributors" subsection of the Documentation section):**

```markdown
| [Architecture](docs/architecture.md) | System architecture · component map · data flow · OCI integration |
```

**Insert immediately after that line:**

```markdown
| [Architecture — Phase 1 + Phase 2](docs/architecture-phase1-phase2.md) | New subsystems: jobqueue, runstate FSM, permission middleware, notifier, scheduler |
| [Adapted from Dokploy](docs/adapted-from-dokploy.md) | What was adapted, what was skipped, what was deferred — with paths into both codebases |
```

---

## Edit 4 — update the Features section with the new capabilities

**Anchor (find the existing closing `</details>` tag for the "⚡ Execution" details block, which currently ends with):**

```markdown
- **Orphan sweep** reaps stale runs after OOM kills

</details>
```

**Replace that closing `</details>` with the following so the new bullets land inside the existing details block:**

```markdown
- **Orphan sweep** reaps stale runs after OOM kills
- **Durable async job queue** (Phase 1 / default-off) — runs survive restarts via Postgres `FOR UPDATE SKIP LOCKED` + `LISTEN/NOTIFY`; per-pipeline concurrency keys
- **Formal state machine** (Phase 1) for run / stage status — invalid transitions rejected at the API boundary, terminal-sticky
- **Cron-triggered runs** (Phase 2 / default-off) — leader-elected via `pg_advisory_lock`; in-house POSIX cron parser with IANA timezone support
- **Multi-provider git webhooks** — GitHub + GitLab + Bitbucket Server + Gitea, each with provider-correct signature verification (`hmac.Equal` / `subtle.ConstantTimeCompare`)
- **Multi-channel notifications** — Slack, Discord, Email (SMTP), generic JSON webhook; per-target event-type filters
- **Pipeline templates** — catalog + create-from-template with fresh-ID deep-copy + DAG re-validation

</details>
```

---

## Edit 5 — update the Roadmap section

**Anchor (find the existing `## 🗺️ Roadmap` heading and its content). Replace the entire section** (from `## 🗺️ Roadmap` through the bullet list ending in `[`docs/pm-brief-2026-05.md`]`) **with:**

```markdown
## 🗺️ Roadmap

### ✅ Recently shipped — Phase 1 + Phase 2 (May 2026 W6)

| Theme | What |
|-------|------|
| **Durable async execution** | Postgres-backed job queue + `NOTIFY` wake-ups + `FOR UPDATE SKIP LOCKED` pickup + per-pipeline concurrency keys + exp. backoff |
| **Run state machine** | Formal FSM with typed `ErrInvalidTransition`; state alphabet pinned to `model.RunStatus` |
| **Permission middleware** | Resource × action matrix + `RequirePermission` Gin middleware (defense-in-depth alongside roles + MFA) |
| **Notifications** | Slack / Discord / Email / Webhook fan-out via `errors.Join`; per-target `SendTimeout` |
| **Cron triggers** | Leader-elected (`pg_advisory_lock`) scheduler with in-house POSIX cron parser; DST-correct IANA timezones |
| **Git providers** | GitLab + Bitbucket Server + Gitea webhook receivers alongside GitHub |
| **Templates v1** | Pipeline-template catalog + create-from-template with fresh stage IDs + DAG re-validation |

Full attribution + design rationale: [`docs/adapted-from-dokploy.md`](docs/adapted-from-dokploy.md)
Full Phase 1+2 architecture: [`docs/architecture-phase1-phase2.md`](docs/architecture-phase1-phase2.md)
Ready-to-paste CHANGELOG entry: [`docs/CHANGELOG-PHASE1-PHASE2.md`](docs/CHANGELOG-PHASE1-PHASE2.md)

### 🚀 Upcoming

| Theme | What |
|-------|------|
| **Admin UI** | CRUD endpoints + frontend pages for templates / schedules / notification-targets |
| **DAG primitives** | Retry policies · conditional edges · fan-out matrix · cache plumbing · stage outputs |
| **Executor migration** | Switch `service/executor.go` status writes through `runstate.TransitionRun` (mechanical; primitive in place) |
| **More deploy targets** | Kamal · Cloud Run depth · HashiCorp Nomad |
| **Pipeline-as-code** | CKR-DSL parser · import from Drone / GitHub Actions YAML |
| **Marketplace** | Sharable pipeline templates · org-scoped catalog · frontend gallery |
| **AI assist** | Suggest stages · explain failures (local heuristics first, optional hosted LLM) |
| **Builder breadth** | Nixpacks · Railpack · Paketo · Heroku Buildpacks |

- 📋 **Active backlog** with effort estimates: [`backlog.md`](backlog.md)
- 🗓️ **Strategic plan:** [`docs/roadmap-2026.md`](docs/roadmap-2026.md)
- 🧱 **DAG primitives roadmap:** [`docs/dag-adaptation-2026.md`](docs/dag-adaptation-2026.md)
- 🤝 **PM brief + decisions:** [`docs/pm-brief-2026-05.md`](docs/pm-brief-2026-05.md)
```

---

## Edit 6 — add the Phase 1 + Phase 2 packages to the Project Structure tree

**Anchor (find this existing line in the project tree):**

```markdown
│   │   ├── idempotency/      Run-launch dedupe + pg_advisory_lock
```

**Append the following lines immediately after it (mind the tree indentation):**

```markdown
│   │   ├── jobqueue/         Phase 1 / A1: durable Postgres job queue + workers + NOTIFY
│   │   ├── runstate/         Phase 1 / A2: run + stage FSM with typed invalid-transition errors
│   │   ├── notifier/         Phase 2 / F1: Slack/Discord/Email/Webhook dispatcher
│   │   ├── scheduler/        Phase 2 / F2: leader-elected cron-triggered runs
│   │   ├── templates/        Phase 2 / F4: pipeline template catalog
```

And in the `source/` line, you can optionally extend the comment to
list the new providers:

**Anchor:**

```markdown
│   │   ├── source/           Repo clone helpers
```

**Replace with:**

```markdown
│   │   ├── source/           Repo clone helpers + Phase 2 / F3 webhook parsers (github, gitlab, bitbucket, gitea)
```

---

## Verification

After applying all six edits, check:

1. The TOC at the top still resolves — no edit touches it.
2. The Configuration > Common Environment Variables table renders
   cleanly with the four new rows.
3. The new "Recently shipped" blockquote appears immediately after
   the existing Status blockquote in the Overview.
4. The Roadmap section has the "Recently shipped" + "Upcoming"
   split with the new tables.
5. The Project Structure tree lists the five new packages.
6. The Documentation section lists the two new docs under
   "For contributors".

`grep -n 'Phase 1 + Phase 2' README.md` should return at least the
status blockquote, the Documentation row, and the Roadmap heading
(three matches minimum).

Once applied, this diff doc can be deleted. The same content lives
in [`docs/adapted-from-dokploy.md`](./adapted-from-dokploy.md) and
[`docs/architecture-phase1-phase2.md`](./architecture-phase1-phase2.md)
for anyone reading the docs tree.
