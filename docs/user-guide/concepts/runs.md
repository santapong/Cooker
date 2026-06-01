# Runs

A **run** is a single execution of a pipeline (or a synthesised pipeline, for App deploys). Runs are the unit of observability — you watch a run live, you read its logs, you re-run on failure.

The model is `model.PipelineRun` (`backend/internal/model/run.go`).

## Lifecycle

```text
   pending  ──►  running  ──►  success
                    │
                    ├─► failed
                    └─► cancelled
```

| Status | Meaning |
|---|---|
| `pending` | Persisted, but the executor hasn't picked it up yet (queue, rate limiter, idempotency check). |
| `running` | Active. At least one stage is executing or waiting on a dependency. |
| `success` | All stages reached terminal `success` status. |
| `failed` | At least one stage failed and there's no `failure`-edge cleanup that recovered it. |
| `cancelled` | Explicitly cancelled via `POST /api/v1/pipelines/:id/runs/:runId/cancel`, OR force-cancelled when the run deadline expired. |

## Stage runs

Each stage in the pipeline produces a `StageRun`:

| Field | Purpose |
|---|---|
| `stageId` | The stage definition this run executed. |
| `status` | Same enum as the run; `success` once stage exits 0. |
| `startedAt`, `finishedAt` | Stage timings. |
| `logs` | Full stdout/stderr capture. Tee'd to the WebSocket log channel during execution. |
| `error` | Non-empty when the stage failed; explains why. |
| `artifacts` | OCI references produced by the stage (`{type, ref, digest}`). |

Successful Build stages produce an `artifact` with `type=oci-image` and the resulting image's digest. Push stages emit the registry-qualified ref.

## Run-level fields

| Field | Purpose |
|---|---|
| `id` | Run UUID. |
| `pipelineId` | Pipeline this run is for. For App deploys, this is the synthetic `app-<appId>`. |
| `status` | Current run status. |
| `stageRuns` | List of stage runs (see above). |
| `environmentStatuses` | Per-environment status; updated as the run promotes between envs. |
| `variables` | Resolved at run-start; pinned for the run (later edits to pipeline vars don't affect in-flight runs). |
| `startedAt`, `finishedAt` | Run timings. |
| `error` | Top-level error string when run failed during orchestration (not stage-level). |
| `heartbeatAt` | Updated periodically by the run coordinator while the run is in flight. |

## The heartbeat and orphan sweep

A run that crashes between `started_at` and `finished_at` without updating its status would otherwise sit as `status=running` forever. Cooker handles this with two mechanisms:

1. **Heartbeat.** While a run is in flight, the coordinator updates `heartbeat_at` periodically.
2. **Boot sweep.** On startup, `RunStore.SweepOrphans` flips any `status=running` row whose `heartbeat_at` is older than `COOKER_RUN_HEARTBEAT_THRESHOLD` to `failed` with `error=orphaned: heartbeat stale at boot`.

This means a Cooker pod that gets OOMKilled mid-run will have its in-flight runs marked failed on the next boot, not left hanging.

## Watching a run live

Two WebSocket channels:

- `/ws/pipeline-run/:runId` — run-level status changes.
- `/ws/runs/:runId/stages/:stageId/logs` — per-stage live log stream.

For App-deploy runs, use `/ws/app-run/:runId` instead.

All WebSocket endpoints require a single-use 60-second ticket. The frontend fetches one from `POST /api/v1/ws-tickets` and opens the WebSocket with `?ticket=<value>`. The ticket is consumed on first use; replay is rejected.

See [WS disconnects](../troubleshooting/ws-disconnects.md) for the failure modes.

## Cancelling a run

```bash
POST /api/v1/pipelines/:id/runs/:runId/cancel
```

Requires `operator` or `admin` role. Cancels in-flight stages by closing their executor contexts. Stages that have already completed are not rewound.

> **Behaviour note.** Cancellation is **cooperative**. A stage running an unkillable subprocess (rare but possible for native build tools) can ignore the context deadline and keep running. The run is marked `cancelled` regardless after the deadline; the subprocess simply leaks until it exits or is reaped by the container runtime.

## Run deadlines

Every run is subject to:

- `COOKER_RUN_DEADLINE` — global maximum run duration (default in code, varies by deployment). After this, the run is force-cancelled.
- Per-stage timeouts via `StageConfig.Timeout` — overrides the global default for that stage.

> **Known gap.** Per-Pipeline and per-App `runDeadline` overrides do NOT exist yet. ML workloads with 45+ minute builds run into this; see roadmap `D7` and the [W11 ML persona walkthrough](../../audits/W11-user-journeys.md#persona-4--ai--ml-engineer).

## Listing runs

```bash
GET /api/v1/pipelines/:id/runs
```

Returns all runs for a pipeline, newest first. The response is currently unpaginated — for pipelines with thousands of runs, this gets slow. Tracked as `D2` on the roadmap (run search + filters + pagination).

## Run logs

Two ways to read logs:

1. **Live.** WebSocket per-stage as described above.
2. **After the fact.** `GET /api/v1/pipelines/:id/runs/:runId/logs/:stageId` returns the captured stdout/stderr. Returns 404 once the executor has reaped old log buffers (today: never; logs persist for the run's lifetime in the DB).

## Idempotency

`POST /pipelines/:id/run` and `POST /apps/:id/deploy` support the `Idempotency-Key` header. Send the same key within 5 minutes and you get back the original run, not a duplicate. This is essential for retry safety — without it, a double-click can spawn two parallel builds against the same commit.

The middleware lives at `internal/idempotency` and runs against an in-memory bounded LRU (32 MiB resident set). See [`docs/RUNBOOK.md`](../../guides/RUNBOOK.md) for cache-miss diagnostics.

## Run statuses and the UI

In the UI:

- Run rows in the list view show the latest status.
- A run detail page renders the pipeline DAG with stages coloured by status.
- Stage clicks open the log panel.

<!-- SCREENSHOT: run detail page showing a 5-stage pipeline mid-execution, two stages green, one blue (running) -->

## Cross-references

- **[Pipelines](pipelines.md)** — what a run executes.
- **[Stages](stages.md)** — stage-level config that affects run behaviour.
- **[Troubleshooting: builds stuck](../troubleshooting/builds-stuck.md)** — when `status=running` doesn't progress.
- **[Troubleshooting: WS disconnects](../troubleshooting/ws-disconnects.md)** — the most common live-log frustration.
