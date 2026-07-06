# Run-state concurrency — design note (2026-06)

**Status:** Proposed / in progress. Landing across a short PR series (below).
**Origin:** Findings F2, F9, F15, F16, F18 from [`docs/audits/2026-06-health-sweep.md`](../audits/2026-06-health-sweep.md), plus the related perf item F7.

## Problem

A `*model.PipelineRun` is **shared mutable state** with no synchronization between its writer and its readers:

- **Writer:** during `Executor.Execute`, each stage runs in its own goroutine (`pkg/dagrunner` `go func(id)`, bounded by `maxParallel`) and mutates **its `StageRun` element in place** — `run.StageRuns[i].Status`, `.StartedAt`, `.FinishedAt`, `.Error`, `.Logs`, `.Artifacts`, `.Outputs`. There is no mutex around these writes.
- **Readers of the same pointer:**
  - the progress-persistence **drain goroutine** (`persistProgress` → `RunUpdater` → `RunStore.Update` → `json.Marshal(run)`),
  - the **HTTP response encoder** (`RunPipeline` does `c.JSON(202, run)` *after* spawning the executor on the same pointer),
  - the **in-memory store** (`runs.Get` returns `cp := *r`, a shallow copy that still aliases the `StageRuns` backing array).

Because the store holds the *same pointer* the executor mutates, every one of those reads can observe a half-written `StageRun`.

### Proven, not theoretical

Wiring a real (marshaling) `RunUpdater` and running 8 parallel stages under `-race` fails immediately:

```
WARNING: DATA RACE
Read  at executor.go:983  persistProgress → json.Marshal(run)     [drain goroutine]
Write at executor.go:831  stageRun.Status = next ("success")       [stage goroutine]
```

This is why `WithRunUpdater` is wired only in tests today (with a non-marshaling fake counter): wiring the real `RunStore.Update` (finding **F2**) would expose the race. The same shared-pointer hazard underlies **F15** (response encoder) and **F16** (store `Get`).

## The contract

> **The executor is the sole owner of the live `*PipelineRun` for the duration of a run. It mutates run state only under a per-run mutex. Every other party reads a snapshot, never the live pointer.**

Two primitives implement it:

1. **`PipelineRun.Clone()`** — a deep copy (StageRuns + their Artifacts/Outputs/time pointers, EnvironmentStatuses, Variables) that is safe to read while the original is mutated *once the original is no longer being concurrently written, or while holding the owning lock*. This is the single snapshot primitive all readers use.
2. **A per-run mutex inside `Execute`** — guards every `StageRun` write in the stage taskFunc and the `execute*` finalizers, and is held while `persistProgress` takes its `Clone()` snapshot. The marshal/DB-write then happens on the private snapshot, outside the lock.

With those, the store never holds the executor's live pointer: `persistProgress` publishes **snapshots** via `Update`, so anything the store hands out (`Get`) is an inert copy.

## Finding → fix

| Finding | Fix | PR |
|---|---|---|
| **F7** — `collectStageOutputs` scans the whole stage map per stage (O(stages²)) | Iterate the `allowed` (ancestor) set and look up in the map — O(ancestors), preserving the documented ancestor-only race-safety | **1** |
| **F15** — `RunPipeline` encodes the live run after spawning the executor | Take a `Clone()` snapshot **before** `SpawnWithDeadline` (run is still single-threaded there) and respond with the snapshot | **1** |
| **F18 / F10** — `RunStore.Update` re-marshals all JSONB (incl. full logs) on every call; status-only writers (cancel) pay a full-row rewrite | Add `RunStore.UpdateStatus` (status/finished_at/error only) and use it from cancel | **2** ✅ |
| **F16** — memory `runs.Get` shallow copy aliases the `StageRuns` backing array | `Get` returns `run.Clone()` | **3** |
| **F2 / F9** — mid-run persistence (`WithRunUpdater`) is never wired in prod; the drain goroutine is therefore inert overhead | Per-run mutex; persist a `Clone()` snapshot under the lock via a **heartbeat-safe** `UpdateProgress` (see below) | **3** |

## Heartbeat constraint (discovered during F2 design)

`persistProgress → RunStore.Update` cannot be wired naively: postgres `RunStore.Update` writes **`heartbeat_at` from `run.HeartbeatAt`** (`internal/store/postgres/run.go`), but the run coordinator maintains the heartbeat via a *separate* targeted `UpdateHeartbeat` column write — the executor's in-memory `run.HeartbeatAt` is stale. A drain flush through `Update` would therefore **overwrite `heartbeat_at` with a stale value on every flush**, defeating the boot orphan-sweep. This — on top of the data race — is why `WithRunUpdater` was never wired.

**Consequence:** F2 must persist through a method that does **not** touch `heartbeat_at`. PR 2 added `RunStore.UpdateStatus` (lifecycle columns only); PR 3 adds the drain's `UpdateProgress` (status + `stage_runs` + env/vars, **no** `heartbeat_at`) plus the snapshot-under-lock.

## PR sequence (revised)

1. **Run-state read-safety + perf** — `Clone()` + F15 + F7 + this note. *(landed)*
2. **Store targeted updates** — `RunStore.UpdateStatus` (F10) with memory/postgres parity; cancel switched off the full-row `Update` (F18). The heartbeat-safe foundation for F2. *(landed)*
3. **Executor run-state ownership** — per-run mutex + `UpdateProgress` (heartbeat-safe) + wire mid-run persistence (F2/F9) + memory `Get` deep-copy (F16), with the `TestExecutor_RunUpdaterMarshalRace` parallel-stages `-race` guard.

## Test strategy

- `Clone()` gets a unit test asserting deep independence (mutating the original's nested slices/maps doesn't touch the clone).
- PR 2 adds `TestExecutor_RunUpdaterMarshalRace` — N parallel stages + a marshaling `RunUpdater`, which must pass under `-race`. This is the standing guard that the ownership contract holds.
- Existing `TestExecutor_Outputs_ParallelSiblingsNoRace` must stay green through the F7 change (the ancestor-only filter is the invariant it protects).

## Out of scope

- `pipeline_runs`-row optimistic concurrency (single writer per run by design — see remediation-plan T11).
- The job-queue reliability items (F11/F12/F13) and F1/F5 — separate workstreams.
