# F2 + F3 sub-delegation prompts (2026-05 W4)

Read-only. Polishes the F2+F3 sketch in
`docs/audits/2026-05-handler-f2-f3-extraction.md` (W3 PR #55) into two
prompts the PM pastes verbatim into `cooker-feature-dev` Agent calls
in W5 — **F2 first, F3 after F2 merges**. F1 shipped via PR #64. Both
close `docs/design.md` §11 violations.

---

## 1. F2 prompt — `service.Executor` returns a terminal `RunResult`

**Title (PR):** `refactor(service): F2 — Executor returns terminal RunResult`
**Branch:** `claude/w5-f2-executor-runresult`
**Target agent:** `cooker-feature-dev`
**Source sketch:** `docs/audits/2026-05-handler-f2-f3-extraction.md` §F2 (PR #55).

### Prompt body (paste verbatim)

> Close audit finding **F2** from `docs/audits/2026-05-handler-layering.md`.
> The extraction sketch with file:line citations, the contract, and the
> migration plan are in `docs/audits/2026-05-handler-f2-f3-extraction.md`
> §F2 (PR #55). F1 has already shipped via PR #64; do not re-derive that
> work. Sequence after T4 + T5 (W4 batch) lands so `executor.go` is
> quiet.
>
> **Read first:** `CLAUDE.md`, `docs/design.md` §11, the F2 section of
> the sketch, and the dag-performance overlap note (sketch §F2 "Cross-
> reference").
>
> **Scope (in this PR only):**
>
> 1. Add `service.RunResult` and bump the `Executor` interface in
>    `backend/internal/service/executor.go` to
>    `Execute(ctx, *model.Pipeline, *model.PipelineRun) (RunResult, error)`.
>    `RunResult.Status` MUST be terminal (`Failed`, `Success`, or
>    forward-compat `Cancelled`); non-terminal on return is a programmer
>    error.
> 2. Move the if-err/if-still-running branch out of
>    `backend/internal/handler/pipeline.go:191-210` and into
>    `Execute`. The handler keeps `h.Store.Runs.Update` and an
>    `applyResult(run, result)` 6-line helper colocated with `RunResult`.
> 3. Update **both** call sites: `RunPipeline` (pipeline.go:191) and
>    `RunPipelineFromCanvas` (same file, ad-hoc-canvas variant — confirm
>    line range during read; sketch notes both share the pattern).
> 4. Update all in-tree `Executor` implementations and test doubles
>    (memory executor, pipeline executor, fakes in `service/` and
>    `handler/`).
> 5. **Out of scope:** mid-run progress persistence (stays with the
>    dag-performance workstream); the `RunSpawner` interface (leave
>    `h.Runs.Spawn` alone — F2 only changes what runs inside the
>    closure); any wire-format change to `model.RunStatus`.
>
> **Forward-compat test (required, table-driven, in
> `internal/service/executor_test.go`):**
>
> - Execute returns nil → `Status == RunStatusSuccess`.
> - Execute returns err, run still `Running` → `Status == Failed`,
>   `Error` populated from the underlying err.
> - **Execute returns nil, run already `Cancelled` → `Status` stays
>   `Cancelled`** (this is the silent-flip bug the sketch flags — the
>   today-handler checks only `RunStatusRunning` and would flip
>   `Cancelled` to `Success`).
> - Execute returns err, run already `Failed` with non-empty `Error` →
>   `Error` not overwritten.
>
> Add a `RunStatusCancelled` model constant **only if absent today**;
> if absent, ship it in the same PR as a forward-compat stub. No
> store migration is required — `runs.status` is already a free-form
> string in `internal/store/postgres/migrations/`.
>
> **Sub-delegation (per `docs/design.md` §11):**
>
> - `cooker-backend-api` owns: `internal/service/executor.go`,
>   `internal/service/executor_test.go`, `internal/handler/pipeline.go`,
>   `internal/handler/pipeline_test.go`, and any service-layer fakes.
> - `cooker-backend-data` is **not** required (no schema change). If
>   the status constant is missing and you choose to add it, the
>   `model` change is still API-owned (no migration).
> - `cooker-security` is not required.
>
> **Verification commands (run before declaring done):**
>
> ```
> cd backend && go vet ./...
> cd backend && go test ./... -race
> cd backend && go build ./...
> ```
>
> **PR body template:**
>
> ```
> ## Summary
> - Closes F2 from docs/audits/2026-05-handler-layering.md.
> - Executor.Execute now returns a terminal RunResult; handler closure
>   shrinks to persist-only and is no longer a hidden state machine.
> - Adds forward-compat test that catches the Cancelled-to-Success
>   silent flip the sketch in PR #55 flagged.
>
> ## Test plan
> - [ ] go vet ./... clean
> - [ ] go test ./... -race clean (including new table-driven
>       executor_test.go cases)
> - [ ] handler closure in pipeline.go no longer branches on
>       runCopy.Status
> - [ ] backlog.md F2 entry moved to Closed with this PR number
> ```
>
> **Done when:** the four verification commands are clean, both
> `RunPipeline` call sites are migrated, the four table-driven cases
> are in place, and `backlog.md` is updated.

---

## 2. F3 prompt — `service.ParseComposeGraph` extraction

**Title (PR):** `refactor(service): F3 — extract ParseComposeGraph from handler`
**Branch:** `claude/w5-f3-parse-compose-graph`
**Target agent:** `cooker-feature-dev`
**Source sketch:** `docs/audits/2026-05-handler-f2-f3-extraction.md` §F3 (PR #55).
**Sequence:** fire **after** F2 merges (per sketch §"Sequence recommendation").

### Prompt body (paste verbatim)

> Close audit finding **F3** from `docs/audits/2026-05-handler-layering.md`.
> The extraction sketch with file:line citations and the residue shape
> for the handler is in `docs/audits/2026-05-handler-f2-f3-extraction.md`
> §F3 (PR #55). F1 shipped in PR #64; F2 must be merged before this
> branch opens so the "service returns a built domain object; handler
> does I/O around it" pattern is established.
>
> **Read first:** `CLAUDE.md`, `docs/design.md` §11, the F3 section of
> the sketch, and `backend/internal/handler/docker.go:150-352`.
>
> **Scope (in this PR only):**
>
> 1. Create `backend/internal/service/compose.go` with public surface:
>
>    ```go
>    func ParseComposeGraph(data []byte) (*model.ComposeGraph, error)
>    ```
>
>    Input is raw YAML bytes (disk + path concerns stay in the
>    handler). Output is the built graph or a typed
>    `ErrInvalidComposeYAML` distinguishable from future validation
>    errors.
> 2. Move these helpers into `compose.go` as **private**:
>    - `parseEnvToMap` (handler/docker.go:268-293)
>    - `parseDependsOn` (handler/docker.go:295-316)
>    - `parseCommand` (handler/docker.go:318-335)
>    - `parseBuild` (handler/docker.go:337-352)
>    - The `composeFile` / `composeService` intermediate structs.
> 3. **Keep in the handler:** `resolveComposePath` (allowlist +
>    traversal guard — HTTP-input shaped, security-critical); the
>    generic 400 error strings ("invalid compose filename", "cannot
>    read compose file", "invalid YAML") verbatim — they are an
>    allowlist-mapping concern. Shrink `ParseComposeFile` to the
>    11-line residue shown in the sketch §F3 "The handler's residue".
> 4. **`connSet` dedup key format MUST be byte-preserved.** The key
>    format `src->dst:type` is implementation detail today but
>    frontend ordering / dedup behaviour may already depend on it.
>    Lock it in with a test that asserts a specific key shape.
> 5. **Out of scope:** any wire-format change to `model.ComposeGraph`
>    / `ComposeService` / `ComposeBuild` / `ComposeConnection` — they
>    stay in `model/` unchanged; `UpdateComposeService`
>    (handler/docker.go:250) — stub, no extraction; F4–F7 from the
>    same audit.
>
> **Test corpus — table-driven in `internal/service/compose_test.go`
> (all 10 + bonus from sketch §F3 "Test surface"):**
>
> 1. Empty file → empty graph, no error.
> 2. Single service, no deps → one node, zero edges.
> 3. `depends_on` as list → list-shaped edges.
> 4. `depends_on` as map (long form) → keys become edges.
> 5. `environment` as map with cross-service reference →
>    `env_reference` edge.
> 6. `environment` as list (`KEY=val`) with cross-service reference →
>    edge.
> 7. `environment` as list with bare key (no `=`) → empty string
>    value, no panic.
> 8. Named top-level `networks` and `volumes` appear in graph
>    aggregates.
> 9. `build` as string → `ComposeBuild{Context: s}`.
> 10. `build` as map with `context` + `dockerfile` → both populated.
> 11. (bonus) Duplicate edges via both `depends_on` and env reference
>     → deduped by `connSet` (assert key format byte-for-byte).
>
> Handler test surface in `handler/docker_test.go` shrinks to two
> cases: (a) `resolveComposePath` rejects absolute/traversal/separator
> inputs with the generic 400; (b) happy path returns 200 with a
> graph-shaped body. Delete handler tests that duplicate service
> coverage.
>
> **Sub-delegation:**
>
> - `cooker-backend-api` owns: `internal/service/compose.go`,
>   `internal/service/compose_test.go`, `internal/handler/docker.go`,
>   `internal/handler/docker_test.go`.
> - `cooker-backend-data` is **not** required. `model.ComposeGraph`
>   is the wire shape and stays in `model/`; the sketch §F3 confirms
>   no schema work.
> - `cooker-security` is not required (the allowlist guard
>   `resolveComposePath` stays in the handler unchanged).
>
> **Verification commands:**
>
> ```
> cd backend && go vet ./...
> cd backend && go test ./... -race
> cd backend && go build ./...
> ```
>
> **PR body template:**
>
> ```
> ## Summary
> - Closes F3 from docs/audits/2026-05-handler-layering.md.
> - Extracts compose-YAML graph construction (~70 lines + 4 helpers)
>   into service.ParseComposeGraph; handler ParseComposeFile shrinks
>   to ~11 lines of HTTP-layer I/O around the service call.
> - Adds 11-case table-driven test corpus including connSet dedup-key
>   byte-format lock-in.
>
> ## Test plan
> - [ ] go vet ./... clean
> - [ ] go test ./... -race clean
> - [ ] resolveComposePath untouched; error strings byte-identical
> - [ ] connSet key format asserted (src->dst:type)
> - [ ] backlog.md F3 entry moved to Closed with this PR number
> ```
>
> **Done when:** the four verification commands are clean, the 11
> service test cases pass, the handler residue matches the sketch's
> 11-line shape, and `backlog.md` is updated.

---

## Cross-references

- Source sketch: `docs/audits/2026-05-handler-f2-f3-extraction.md` (PR #55).
- F1 closure: PR #64.
- W4 prerequisites for F2: T4 (`feat(service): T4 — refuse non-success
  Edge.Condition`) and T5 (`feat(service): T5 — batched persistProgress`)
  both land in `internal/service/executor.go`; F2 rebases on top of them
  with no expected region overlap (T5 touches the drain comment block at
  executor.go:265-272; F2 changes the `Execute` signature at line 143).
