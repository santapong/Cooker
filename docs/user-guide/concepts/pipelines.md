# Pipelines

A pipeline in Cooker is a **DAG** (directed acyclic graph) of stages. Stages are the work units; edges are the dependency arrows. The graph editor in the browser is a 1:1 rendering of this model — what you see is what the executor runs.

## Mental model

```text
       ┌────────┐      ┌────────┐
       │ Build  │ ───► │ Test   │
       └────────┘      └────────┘
            │                │
            ▼                ▼
       ┌────────┐      ┌────────┐
       │ Push   │ ───► │ Deploy │
       └────────┘      └────────┘
```

- Each box is a **Stage**. Stage type determines what executes (build a Docker image, run tests, push to a registry, deploy to K8s, …).
- Each arrow is an **Edge**. Edges can carry a condition: `success` (default), `failure`, or `always`.
- Stages with no inbound dependencies run first. The executor walks the DAG level-by-level; within a level, stages run in parallel.

## The graph editor

The pipeline editor uses [React Flow](https://reactflow.dev/). It supports:

- Drag-and-drop from the **PipelineToolbar** (the node palette).
- Click-to-connect handles on each node.
- Slide-out **NodeConfigPanel** for editing a stage's config.
- Environment swimlanes — drop a stage into a swimlane to assign it to an environment (Dev / Staging / Prod).

<!-- SCREENSHOT: the pipeline editor with three stages connected, a swimlane visible -->

The editor saves the entire graph as a single atomic document when you click **Save** (a `POST /api/v1/pipelines` or `PUT /api/v1/pipelines/:id`). Stages and edges are stored as JSONB in Postgres — the React Flow data model and the database row are literally the same shape.

## Pipeline lifecycle

1. **Create** the pipeline. The editor opens with an empty canvas; you save when ready.
2. **Validate**. `POST /api/v1/pipelines/:id/validate` runs DAG validation (no orphan stages, no cycles, every edge references existing stages). The UI calls this for you on Save.
3. **Run** it. `POST /api/v1/pipelines/:id/run` starts a [Run](runs.md). Idempotency-Key headers are honoured for retry safety.
4. **Watch** it. WebSocket channel `/ws/pipeline-run/:runId` streams stage status changes. Per-stage logs stream over `/ws/runs/:runId/stages/:stageId/logs`.
5. **Promote** between environments — manual approval or auto-promote, configured per environment. See [Promotions](../guides/promotions.md).

## Stage types

See [Stages](stages.md) for the full catalog.

## DAG semantics

- **Topological execution.** Stages with no inbound `success` edges from non-completed stages can start. The executor batches them into "levels" and dispatches each level in parallel.
- **Edge conditions** gate transitions:
  - `success` *(default)* — target stage starts when source succeeds.
  - `failure` — target starts only on source failure (use for cleanup / notify).
  - `always` — target starts regardless.
- **No cycles.** `validatePipelineInput` and `validateDAG` reject pipelines with cycles before they hit the executor. Both live in `backend/internal/handler/pipeline.go`.
- **No orphan stages.** Every stage must be reachable from a starting node and must have at least one valid edge unless it's a terminal.

## Variables

Three precedence levels, lowest to highest:

1. **Pipeline.Variables** — set on the pipeline itself, available to every stage.
2. **Environment.PlainVars** — non-sensitive vars from the [Environment](environments.md) the stage runs in.
3. **StageConfig.Env** — per-stage override; wins over both above.

For sensitive values, use [Secrets](../guides/secrets.md) via `StageConfig.SecretRefs`. The executor resolves and injects them just before the stage runs; the stage never sees ciphertext.

## Pipelines vs Apps

If a pipeline is "I want full control over the DAG", an [App](apps.md) is "I have a repo, build it and deploy it." Apps synthesise a fixed Clone -> Build -> Push -> Deploy run at request time. Pipelines let you express custom shapes: parallel test matrices, fan-in / fan-out, approval gates between any stages.

You don't have to choose one. Many teams start with Apps for the common 80% and add a Pipeline for the awkward 20%.

## Optimistic concurrency

`Pipeline.Version` is the optimistic-concurrency token. Every successful Update increments it. PUT requests must echo the version they read; a stale version returns `409 Conflict`. This stops two operators silently clobbering each other's edits.

## Persistence

Stages and edges live in the `pipelines` table as JSONB:

```sql
pipelines (
  id TEXT PRIMARY KEY,
  name TEXT,
  description TEXT,
  stages JSONB,
  edges JSONB,
  variables JSONB,
  version INT,
  created_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ
)
```

This is intentional — pipelines are atomic documents. See [`docs/architecture.md` design decisions](../../reference/architecture.md#design-decisions) for the rationale.

## Limits

- **Pipeline submission size.** The handler doesn't currently enforce an `io.LimitReader` cap; Gin's `MaxMultipartMemory` (32 MiB) is the only protection. A pipeline JSON of ~30 MiB will load. There is no exploitable security issue here (admins only), but pipelines are normally a few KB. Tracked as `S26-05-30`.
- **DAG fan-out cap.** Per-run, there is a global parallelism cap so a pathologically wide graph cannot exhaust the executor.

## Cross-references

- **[Stages](stages.md)** — every stage type, what it does, what it configures.
- **[Runs](runs.md)** — lifecycle, statuses, logs, artifacts.
- **[Environments](environments.md)** — how swimlanes map to deploy targets.
- **[Your first pipeline](../guides/first-pipeline.md)** — walkthrough.
