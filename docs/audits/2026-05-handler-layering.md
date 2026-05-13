# Handler-Layering Audit (2026-05)

**Scope:** Every `.go` file under `backend/internal/handler/` plus the three service files that are the immediate callee targets (`service/executor.go`, `service/app_deployer.go`, `service/pipeline.go`, `service/promoter.go`).

**Method:** Static reading of all handler files. Three violation categories per the design contract in `docs/design.md` §11: (1) business logic in handlers, (2) HTTP types leaking into services, (3) bare error checks that should use `errors.Is`.

**Verdict:** Services are clean — no `gin.Context` or HTTP types leak into `internal/service/`. The main layering debt lives in handlers: two handlers contain non-trivial domain logic that belongs in the service tier, and one handler duplicates a DAG-validation algorithm that already exists in `service/pipeline.go`.

---

## Category 1 — Business Logic in Handlers

### ~~Finding 1 — HIGH~~ Finding 1 — CLOSED — Duplicate DAG Validator in `pipeline.go`

**CLOSED in `claude/w3-t1-t3-handler-f1`.**

**What was done:**
- `validateDAG` (57-line Kahn's algorithm) deleted from `handler/pipeline.go`.
- `service.ValidatePipelineDAG` extended with duplicate-stage-ID and dangling-edge checks (previously handler-only).
- `CreatePipeline` now calls `service.ValidatePipelineDAG` to reject cyclic pipelines at creation time.
- `ValidatePipeline` now delegates to `service.ValidatePipelineDAG` instead of the deleted private function.
- `handler/pipeline_test.go` updated: tests now call `service.ValidatePipelineDAG` directly; cycle-detection test uses `strings.Contains(e, "cycle")` because `dagrunner` returns `"DAG contains a cycle"` (not the old handler's `"pipeline contains a cycle"`).
- `service/pipeline_test.go` updated: added `TestValidatePipelineDAG_DuplicateStageID` and `TestValidatePipelineDAG_DanglingEdge` to pin the moved checks at the service layer.

~~**File:** `backend/internal/handler/pipeline.go:267–324`~~

~~**Current behaviour:** `validateDAG(p *model.Pipeline) []string` (57 lines) implements Kahn's topological-sort cycle detection, duplicate-stage-ID detection, and dangling-edge detection in full inside the handler file.~~

---

### Finding 2 — HIGH — Run Status Finalisation Logic in `RunPipeline` Closure

**File:** `backend/internal/handler/pipeline.go:191–210`

**Current behaviour:** The `h.Runs.Spawn` callback inside `RunPipeline` contains status-reconciliation logic after `h.Executor.Execute` returns:

```go
if execErr != nil {
    if runCopy.Status != model.RunStatusFailed {
        runCopy.Status = model.RunStatusFailed
    }
    if runCopy.Error == "" {
        runCopy.Error = execErr.Error()
    }
} else if runCopy.Status == model.RunStatusRunning {
    runCopy.Status = model.RunStatusSuccess
}
```

This is a domain rule ("if the executor returned an error and the status was not already set to failed, set it to failed; if it succeeded and the run is still showing running, advance to success") embedded in an HTTP handler goroutine. The `Executor.Execute` contract should own the terminal-status guarantee. The handler's job ends at "call Execute, persist the result."

**Impact:** The rule is now split: `Executor.Execute` sets `RunStatusFailed` on a stage error (line 278 of `executor.go`) and returns an error, but the handler additionally guards against the case where the executor set it to something else. This interplay is fragile — if `Execute` is extended to set `RunStatusCancelled` for context-cancelled runs, the handler's `else if runCopy.Status == model.RunStatusRunning` branch will incorrectly advance that to `Success`.

**Recommended fix:** Move the guard into `Executor.Execute` so it guarantees on return: run status is either `RunStatusSuccess` or `RunStatusFailed` (never `RunStatusRunning`). The handler closure becomes: call `Execute`, set `FinishedAt`, call `h.Store.Runs.Update`. See also `dag-performance.md` §3 ("No mid-run progress writes") — the same closure is flagged there for a related reason.

**Effort:** S (add a status-clamp at the end of `Executor.Execute`; simplify handler closure).

---

### Finding 3 — HIGH — Compose File Parsing and Graph Construction in `docker.go`

**File:** `backend/internal/handler/docker.go:150–352`

**Current behaviour:** `ParseComposeFile` (lines 150–248) reads a YAML file from disk, unmarshals it, walks the services to build a `model.ComposeGraph` complete with dependency edges inferred from `depends_on` and environment variable cross-references. Four private helper functions (`parseEnvToMap`, `parseDependsOn`, `parseCommand`, `parseBuild`) support the construction. This is ~200 lines of domain logic — YAML ingestion, graph building, edge inference — inside a handler file.

**Impact:** The logic is untestable without standing up an HTTP request. The file-path sanitisation (`resolveComposePath`) is correct and belongs in the handler; the graph construction does not.

**Recommended fix:** Extract a `service.ParseComposeGraph(data []byte) (*model.ComposeGraph, error)` function containing the unmarshal + graph-build loop. Move the four parse helpers there. The handler retains: read and size-limit the path, call `service.ParseComposeGraph`, write the response.

**Effort:** M (extract ~180 lines into a new service function; add a unit test).

---

### Finding 4 — MEDIUM — `bootstrapRole` Business Rule in `auth_local.go`

**File:** `backend/internal/handler/auth_local.go:197–209`

**Current behaviour:** `bootstrapRole` queries `store.UserStore.Count` and returns `RoleAdmin` if the count is zero, otherwise `RoleViewer`. This "first user becomes admin" rule is a business policy that belongs in a service or the local-auth package itself, not in a handler file.

**Impact:** The rule is invisible to service-level tests. Any future change to the bootstrap policy (e.g. "first three users become admin") requires editing a handler.

**Recommended fix:** Move `bootstrapRole` to `internal/auth/local` as `local.BootstrapRole(ctx, users store.UserStore) (string, error)`. The `Signup` handler calls it as before but via the auth package.

**Effort:** S (move function, update import).

---

### Finding 5 — MEDIUM — Variables/PlainVars Normalisation in `CreateEnvironment` and `UpdateEnvironment`

**File:** `backend/internal/handler/environment.go:44–57` (Create), `environment.go:75–80` (Update)

**Current behaviour:** Both handlers silently promote `env.Variables` to `env.PlainVars` and nil-out `env.Variables`. This is a domain mapping rule (the model distinguishes plain vars from sealed secrets; the API accepts them through either field for backward compatibility) that lives in the handler. A similar pattern is absent from the Update path's `env.Secrets = existing.Secrets` assignment, which is correct guard code; the vars normalisation is the policy part.

**Recommended fix:** Extract a `(*model.Environment).NormalisePlainVars()` method on the model, or a `service.NormaliseEnvironment(env *model.Environment)` function. Call it from both handler methods.

**Effort:** S (extract 4-line normalisation; no new test needed if the model method is trivial).

---

### Finding 6 — MEDIUM — Upsert Logic in `runAppDeployCtx`

**File:** `backend/internal/handler/app.go:222–231`

**Current behaviour:** After `AppDeployer.Deploy` returns, `runAppDeployCtx` implements an upsert: try `Store.Runs.Update`; if that fails (row not yet created by coordinator), fall back to `Store.Runs.Create`. This "update-or-insert" coordination policy belongs in a service or in the store, not scattered across a handler method.

**Impact:** Callers of `runAppDeployCtx` cannot easily test the create-fallback path without a full handler + store setup. If the store gains an explicit `Upsert` method (backlog item: mid-run progress, cross-referenced in `dag-performance.md` §3), this handler will not benefit unless someone remembers to update it.

**Recommended fix:** Add `store.RunStore.Upsert(ctx, run)` (or expose the upsert as a service helper), and call it from the handler. Alternatively, guarantee that the `RunCoordinator.Spawn` pre-creates the row before calling the work func, so the Update path is always safe.

**Effort:** S–M (depends on whether Upsert is added to the store interface, which requires `cooker-backend-data` coordination).

---

### Finding 7 — LOW — `CancelPipelineRun` Directly Mutates Run Status

**File:** `backend/internal/handler/pipeline.go:234–246`

**Current behaviour:** `CancelPipelineRun` sets `run.Status = model.RunStatusCancelled` inline then calls `Store.Runs.Update`. No validation that the run is in a cancellable state (e.g., not already `Failed` or `Success`).

**Impact:** Cancelling a completed run silently marks it `Cancelled`, corrupting history. The state-machine guard is a business rule.

**Recommended fix:** Add a `service.Executor.Cancel(ctx, run)` helper (or a pure `canCancel(status)` validator in the service) that validates the transition and returns an error for terminal states. The handler calls the service and maps the error to 409 Conflict.

**Effort:** S.

---

## Category 2 — HTTP Types in Services

No violations found. All files under `backend/internal/service/` import only domain and adapter packages. No `gin.Context`, `http.Request`, or `http.ResponseWriter` appears in any service file. `service/logbroadcast.go` and `service/runlog.go` are clean. The `wsLogSink` type used for WebSocket log fan-out lives in `handler/app.go` (correct layer), not in the service.

---

## Category 3 — Bare Error Checks

### Finding 8 — MEDIUM — `ErrPromotionUnsupported.Error()` Used as Message Instead of `errors.Is`

**File:** `backend/internal/handler/environment.go:220`

**Current behaviour:**

```go
c.JSON(http.StatusNotImplemented, gin.H{"error": secrets.ErrPromotionUnsupported.Error()})
```

This is not an `errors.Is` check (no check is performed — the type assertion `promoter, ok := h.Secrets.(secrets.Promoter)` is the gate), but the error value is converted to a string via `.Error()` to build the JSON body. This is fine for the message, but if a future path returned `ErrPromotionUnsupported` wrapped inside another error, a caller that checks it by string equality would break. The pattern sets a bad precedent immediately below code that correctly uses `errors.Is(err, secrets.ErrNotFound)` (line 224).

**Recommended fix:** Keep the type assertion gate; for the JSON body use a static string constant (`"secrets: promotion not supported by this backend"`) or continue calling `.Error()` on the sentinel — but add a comment that this is a static sentinel, not a wrapped error. Low priority but worth noting for consistency with the `errors.Is` pattern used two lines below.

**Effort:** Nit.

---

## Nits

The following are too small to warrant individual backlog items but are recorded for completeness.

- **`pipeline.go:64` / `app.go:74` / `environment.go:45` / `host.go:62`** — UUID generation and `time.Now()` for ID/timestamp fields occur in every Create handler rather than in a shared `store.Stamp(entity)` helper. Acceptable given the store owns persistence, but creates repetition. Not a layering violation per se.

- **`pipeline.go:47–77` (CreatePipeline)** — Nil-guarding `p.Variables`, `p.Stages`, `p.Edges` to empty-slice/map defaults. These defaults are model policy. Consider adding a `(*model.Pipeline).SetDefaults()` method so the logic is in one place.

- **`auth_local.go:79–86`** — `local.NormaliseEmail` and `local.HashPassword` are called inside the `Signup` handler, which is appropriate. The auth sub-package is doing the work; the handler is correctly thin here.

- **`docker.go:150–151` (`_ = c.ShouldBindJSON(&req)`)** — The return value of `ShouldBindJSON` is deliberately discarded; if it fails, `req.ComposePath` is empty, and `resolveComposePath("")` defaults to `docker-compose.yml`. This is a considered choice (comment explains it) but silently ignores any JSON parse error, which makes debugging unexpected file reads harder. Consider logging or responding 400 on malformed JSON.

---

## Severity Summary

| # | Finding | Severity | File:Line |
|---|---------|----------|-----------|
| 1 | ~~Duplicate DAG validator — 57 LoC of cycle-detection in handler~~ | ~~**High**~~ **Closed** in `claude/w3-t1-t3-handler-f1` | `handler/pipeline.go` |
| 2 | Run-status finalisation logic in `RunPipeline` closure | **High** | `handler/pipeline.go:191–210` |
| 3 | Compose file parsing + graph construction in handler (~200 LoC) | **High** | `handler/docker.go:150–352` |
| 4 | `bootstrapRole` business rule in auth handler | **Medium** | `handler/auth_local.go:197–209` |
| 5 | Variables/PlainVars normalisation in Create/UpdateEnvironment | **Medium** | `handler/environment.go:44–57, 75–80` |
| 6 | Upsert logic (Update→Create fallback) in `runAppDeployCtx` | **Medium** | `handler/app.go:222–231` |
| 7 | `CancelPipelineRun` mutates status without state-machine guard | **Low** | `handler/pipeline.go:234–246` |
| 8 | `ErrPromotionUnsupported.Error()` string expansion vs. sentinel | **Nit** | `handler/environment.go:220` |

---

## Cross-References

- **Finding 2** (run-status reconciliation) overlaps with `dag-performance.md` §3 "No mid-run progress persistence" — the same handler closure is cited there for not persisting progress writes mid-run.
- **Finding 6** (upsert logic) overlaps with `dag-performance.md` §3 recommendation to add `RunStore.Upsert` or pre-create the row in `RunCoordinator.Spawn`.
- **Finding 7** (cancel without guard) relates to the `2026-05-security-review.md` `S26-05-22` note on model-boundary updates — the same pattern of handlers directly mutating model fields without a service-layer guard is flagged there for the Update handler mass-assignment risk.
- **Category 2 verdict** (no HTTP leakage into services) is consistent with `2026-05-security-review.md` §4.2 observation that the service tier is correctly isolated; no new finding needed here.

---

*Audit performed 2026-05-12. No code was changed. All line numbers reference the `claude/research-handler-layering` branch at HEAD.*
