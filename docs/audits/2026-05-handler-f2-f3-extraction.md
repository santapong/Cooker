# Handler-layering F2 + F3 extraction sketch (2026-05 W3)

Research deliverable. Scopes the service-layer extraction PRs for Findings
F2 and F3 from `docs/audits/2026-05-handler-layering.md`. F1 (duplicate DAG
validator) is closed by a parallel W3 spawn and is not addressed here.

Both findings violate `docs/design.md` §11: "handlers do HTTP parsing only;
business logic lives in services." Both are too large for fast-track. F2
should land first — it is smaller, mechanical, and establishes the
"Service returns terminal status" pattern that F3's tests assume.

---

## F2 — Run status finalisation logic in `RunPipeline` closure

### The current logic

`backend/internal/handler/pipeline.go:191-210` — the closure passed to
`h.Runs.Spawn`:

```go
h.Runs.Spawn(context.Background(), run.ID, func(ctx context.Context) error {
    runCopy := run
    execErr := h.Executor.Execute(ctx, p, runCopy)
    finished := time.Now()
    runCopy.FinishedAt = &finished
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
    if err := h.Store.Runs.Update(ctx, runCopy); err != nil {
        return err
    }
    return execErr
})
```

The state-machine rule "if Execute returned non-nil and the status is not
already Failed, force Failed; otherwise if still Running, mark Success" is
domain logic. Adding `model.RunStatusCancelled` today would silently flip
a cancelled run to Success on the no-error path because the closure
checks only `RunStatusRunning`. The handler is also untestable without
spinning up a `RunSpawner`.

Cross-reference: `dag-performance.md` §3 flags the same closure for not
persisting progress mid-run.

### The service-layer contract

Extend `service.Executor` (currently `Execute(ctx, pipeline, run) error`)
to return a `RunResult`:

```go
type RunResult struct {
    RunID      string
    Status     model.RunStatus  // guaranteed terminal
    FinishedAt time.Time
    Error      string           // populated iff Status != Success
}

type Executor interface {
    Execute(ctx context.Context, p *model.Pipeline, r model.Run) (RunResult, error)
}
```

Contract:

1. `Execute` returns a `RunResult` whose `Status` is one of the terminal
   states (`Failed`, `Success`, future `Cancelled`). Non-terminal status
   on return is a programmer error and `Execute` panics in tests.
2. The returned `error` is the underlying cause, surfaced for logging.
   The handler **must not** re-derive status from the error.
3. The service does not persist; the handler (or a small wrapper) calls
   `Store.Runs.Update` with the result.

### Migration steps

1. Add `service.RunResult` type and bump the `Executor` interface in
   `internal/service/executor.go`. Update all in-tree implementations
   (memory executor, pipeline executor, test doubles in `service/` and
   `handler/`).
2. Move the if-error/if-running branch into `Execute`. The implementation
   collapses what is today split across executor + handler closure into
   one place: build the terminal result, return it.
3. Reduce the handler closure to:

   ```go
   result, execErr := h.Executor.Execute(ctx, p, run)
   if err := h.Store.Runs.Update(ctx, applyResult(run, result)); err != nil {
       return err
   }
   return execErr
   ```

   `applyResult` is a 6-line pure helper colocated with `RunResult`.
4. Extend `internal/service/executor_test.go` with table-driven cases:
   - Execute returns nil → Status == Success
   - Execute returns err, run still Running → Status == Failed, Error set
   - Execute returns nil, run already Cancelled → Status stays Cancelled
     (forward-compat; this is the silent-flip bug)
   - Execute returns err, run already Failed → Error not overwritten if
     already set

### Risk + rollback

Risk: **low**. The change is mechanical; the call sites are limited to
`RunPipeline` and `RunPipelineFromCanvas` (verify both — same audit cites
only the first, but the second likely shares the pattern). Rollback:
revert the single PR commit; the interface bump is the only breaking
surface and the in-tree implementations are all updated together.

### Effort

~half day, dominated by writing the forward-compat `Cancelled` test.

---

## F3 — Compose file parsing in `docker.go`

### The current logic

`backend/internal/handler/docker.go` — five helpers plus the main entry:

| Symbol               | Lines      | Role                                       |
|----------------------|------------|--------------------------------------------|
| `ParseComposeFile`   | 150-248    | Handler + graph builder + cross-ref logic  |
| `parseEnvToMap`      | 268-293    | Env list/map normaliser                    |
| `parseDependsOn`     | 295-316    | depends_on list/map normaliser             |
| `parseCommand`       | 318-335    | Command string/list joiner                 |
| `parseBuild`         | 337-352    | Build string/struct → `*model.ComposeBuild`|

Of `ParseComposeFile`'s 99 lines, only lines 150-176 (request bind, path
resolve, disk read, YAML unmarshal error mapping) are HTTP-layer. Lines
178-247 — service list, network/volume collection, `connSet` dedup,
`depends_on` edges, env-var cross-reference edges — are domain logic.
None of it is reachable from a unit test without a `*gin.Context`.

### The service-layer extraction

New file: `internal/service/compose.go`. Public surface:

```go
func ParseComposeGraph(data []byte) (*model.ComposeGraph, error)
```

Takes raw YAML bytes (so disk and path concerns stay in the handler),
returns the built graph or an error distinguishing "invalid YAML"
(typed `ErrInvalidComposeYAML`) from any future validation errors.

The five helpers and the `composeFile`/`composeService` intermediate
structs move into `compose.go` as **private**.

`model.ComposeGraph`, `model.ComposeService`, `model.ComposeBuild`,
`model.ComposeConnection` stay in `model` — they are the wire shape.

### The handler's residue

`handler/docker.go` `ParseComposeFile` shrinks to:

```go
func (h *Handler) ParseComposeFile(c *gin.Context) {
    var req struct{ ComposePath string `json:"composePath"` }
    _ = c.ShouldBindJSON(&req)

    resolved, err := resolveComposePath(req.ComposePath)  // security-critical
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid compose filename"})
        return
    }
    data, err := os.ReadFile(resolved)
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read compose file"})
        return
    }
    graph, err := service.ParseComposeGraph(data)
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid YAML"})
        return
    }
    c.JSON(http.StatusOK, graph)
}
```

`resolveComposePath` stays in the handler package — it is the allowlist
+ traversal guard and is HTTP-input shaped. Do not move it. The generic
error messages stay verbatim (allowlist mapping concern).

### Test surface

New `internal/service/compose_test.go`, table-driven, 10 shapes:

1. Empty file → empty graph, no error.
2. Single service, no deps → one node, zero edges.
3. `depends_on` as list → list-shaped edges.
4. `depends_on` as map (long form) → keys become edges.
5. `environment` as map with cross-service reference → env_reference edge.
6. `environment` as list (`KEY=val`) with cross-service reference → edge.
7. `environment` as list with bare key (no `=`) → empty value, no panic.
8. Named `networks` and `volumes` top-level → appear in graph aggregates.
9. `build` as string → `ComposeBuild{Context: s}`.
10. `build` as map with `context` + `dockerfile` → both populated.
11. (bonus) Duplicate edges via both `depends_on` and env reference →
    deduped by `connSet`.

The handler test surface in `handler/docker_test.go` shrinks to:
"path-sanitisation rejects absolute/traversal/separator inputs with the
generic 400" and "happy path returns 200 with a graph-shaped body" — the
graph construction itself is now covered by the service tests.

### Risk + rollback

Risk: **medium**. Three sources of risk:

1. The `connSet` dedup key format (`src->dst:type`) is implementation
   detail today; preserve it byte-for-byte so any test or frontend that
   has begun to depend on edge ordering / dedup behaviour does not break.
2. The `interface{}` switch helpers have subtle nil/empty-string
   behaviours (`parseEnvToMap`'s "key without `=`" branch sets empty
   string, not nil) — tests must lock these in.
3. Lots of import churn; verify `go vet` and `go test ./... -race` clean
   before review.

Rollback: revert as one commit. No store migration, no interface bump,
no wire-format change.

### Effort

~1 day, mostly the table-driven test corpus.

---

## Sequence recommendation

**F2 first, then F3.**

1. **Size + risk asymmetry.** F2 is ~half a day, mechanical; F3 is ~1 day
   with subtle `interface{}`-switch invariants and a test corpus that
   needs careful construction. Land the small/safe one first.
2. **Pattern-setting.** F2 establishes "service returns a terminal result;
   handler persists" as the layering pattern. F3's tests assume the same
   shape (pure service function returns built domain object; handler does
   I/O around it). Reviewers seeing F2 first will not bikeshed F3's
   structure.
3. **Forward-compat win sooner.** F2 unblocks `RunStatusCancelled` without
   the silent-flip bug. F3 has no such latent footgun — correctness-
   equivalent today, just untestable.

Parallel is technically safe (no overlapping files: `pipeline.go` vs
`docker.go`, and the `service.Executor` interface bump does not touch
compose code), but the review-bandwidth cost of two simultaneous service-
layer extractions outweighs the calendar savings.

## Cross-references

- `docs/design.md` §11 — the layering rule both findings violate.
- `docs/audits/2026-05-handler-layering.md` — F2 at lines 27-55, F3 at
  lines 56-69.
- W3 `cooker-backend-api` spawn closing F1 — lands first; this PR's
  branch should rebase onto `main` after F1 merges, but has no code
  dependency on it (different file regions).
- `docs/audits/dag-performance.md` §3 — overlaps with F2 on the same
  closure; the mid-run progress write is **out of scope** here and stays
  with the perf workstream.

## Out of scope

- F4–F7 from the same audit (separate, lower-priority extractions).
- The `RunSpawner` interface itself — F2 leaves `h.Runs.Spawn` alone and
  only changes what runs inside the closure.
- `UpdateComposeService` (line 250) — stub, no extraction needed.
- Any wire-format change to `model.ComposeGraph`.

## Effort summary

- F2: ~0.5 engineering-day. Risk: low.
- F3: ~1.0 engineering-day. Risk: medium.
- Total: ~1.5 days, sequential. Sequence after T1+T3+F1 (W3 backend PR)
  lands so the layering pattern is fresh in reviewers' minds.
