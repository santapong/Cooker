# DAG Performance Audit

**Scope:** Cache, job-queue/concurrency, fault tolerance, and system logging behaviour for the DAG executor that drives Cooker's Docker-build / Kubernetes-deploy pipelines.
**Method:** Static reading of `backend/pkg/dagrunner`, `backend/internal/service`, `backend/internal/builder`, `backend/internal/deployer`, `backend/internal/server`, and `backend/internal/store/postgres`. Every finding cites the file and line where the behaviour lives.
**Verdict:** Functional for small / single-replica deployments. Several gaps will hurt at scale: unbounded fan-out, no retry, no cache reuse, and per-stage logs that are wired up at the model layer but never populated.

---

## 1. Cache

### What exists

| Mechanism | Location | Notes |
|---|---|---|
| BuildKit layer cache | `backend/internal/builder/buildkit.go:63-74` | Uses default in-process BuildKit cache. **No cache import / export configured** in `SolveOpt` — comment at lines 14-19 explicitly defers cache features. |
| Kaniko build cache | `backend/internal/builder/kaniko.go:159-242` | **No `--cache=true`, no `--cache-dir` flag, no PVC for `/kaniko/cache`.** `ContextPVC` (lines 58-61) is for the *source tree*, not the layer cache. Comment at lines 33-35 admits `emptyDir` is "unit-test only" but real-build wiring isn't there. |
| Kubernetes API discovery cache | `backend/internal/deployer/clientgo.go:72` | `memcache.NewMemCacheClient(disc)` — scoped to a single `Deploy` call. No reuse across deploys. |
| WebSocket Redis pub/sub buffer | `backend/internal/server/wshub_backend.go:74-96` | 256-element per-replica buffer. Drops on backpressure (line ~192). |

### Concerning

- **Every Kaniko build is a cold start.** `BackoffLimit=0`, `TTLSecondsAfterFinished=300` (`kaniko.go:178-179`), no cache volume, no registry cache (`--cache-repo`). For a typical 8-step Dockerfile, that means re-pulling every base layer on every build. **Impact:** N×slowdown proportional to cache-miss rate.
- **BuildKit cache is opportunistic.** It will reuse cache only if the same `buildkitd` instance handles consecutive builds. Multi-replica BuildKit deployments will not share cache without `Cache{Imports,Exports}` exported via a registry — the code never sets these (`buildkit.go:63-74`).
- **No artifact / digest cache.** `BuildKit.Solve` returns `ExporterResponse["containerimage.digest"]` (`buildkit.go:91-93`); `executor.executeBuild` (`executor.go:180-186`) records it on the StageRun but the Pusher stage re-resolves the source by tag string (`executor.go:196-215`) instead of by digest. Tag mutations between Build and Push aren't detected.
- **No HTTP / handler cache.** Pipeline definitions are fetched from PostgreSQL on every run trigger. No in-memory caching layer between handlers and the store.

### Recommendation

1. Wire Kaniko cache: add `KanikoConfig.CacheRepo` and `CachePVC`, append `--cache=true --cache-repo=<repo>` (or `--cache-dir=/cache`) to the Job args in `kaniko.go:160`.
2. Add BuildKit cache imports/exports keyed by `pipeline_id` so cross-replica builds share layers via the registry.
3. Pass image **digests** (not tags) from Build → Push artifacts so the push stage is content-addressed.

---

## 2. Job queue & concurrency

### What exists

| Mechanism | Location | Notes |
|---|---|---|
| Topological sort + level-based parallelism | `backend/pkg/dagrunner/dag.go:29-76` | Kahn's algorithm; nodes at the same level returned as a slice. |
| Goroutine-per-node within a level | `backend/pkg/dagrunner/runner.go:64-79` | `for _, nodeID := range level { go func() { ... }() }` — **no semaphore, no worker pool, no upper bound.** |
| WaitGroup barrier between levels | `backend/pkg/dagrunner/runner.go:81-88` | Each level fully drains before the next begins. |
| Per-user token-bucket rate limiter | `backend/internal/server/ratelimit.go:19-106` | In-memory; gated by `COOKER_RATE_LIMIT_ENABLED`. Comment (lines 14-18) admits this is **per-process** — multi-replica defeats it. |
| RunCoordinator (heartbeat tracker) | `backend/internal/server/runs.go:30-98` | Tracks in-flight runs, 30s heartbeat ticker, 25s drain timeout, no concurrency cap. |
| Runs-update channel | `backend/pkg/dagrunner/runner.go:37` | Buffer = 100 status updates. The Executor drains in a goroutine (`executor.go:147-151`). |

### Concerning

- **Unbounded goroutine fan-out** (`runner.go:64-79`). A pipeline with 100 independent stages spawns 100 goroutines simultaneously. Each goroutine does network I/O against BuildKit / Kaniko / kubectl. **Risk:** registry rate-limit hits, K8s API throttling, OOM under heavy fan-out.
- **No global concurrency limit** across pipelines. The `RunCoordinator` happily spawns 1000 simultaneous `Spawn(...)` calls. Combined with point above, a small number of complex pipelines can exhaust ephemeral ports / file descriptors.
- **Rate-limit bypass on multi-replica deploys** (`ratelimit.go:14-18`, `35-46`). Each replica keeps its own bucket map. A user spamming behind a load balancer with N replicas gets ~N× the configured budget. Disable it and rely on edge limiting, as the comment suggests, or move to Redis-backed token buckets (the file already imports `golang.org/x/time/rate` — a `redis_rate` adapter would slot in).
- **The 30-minute deadline is documented but missing.** `runs.go:41-42` says "extended with a 30-minute deadline (matching the existing app-deploy behaviour)" but `Spawn` (line 45) passes `ctx` straight through with no `context.WithTimeout`. A hung stage (especially any of the unimplemented stubs — see §3) can pin the goroutine until pod restart.
- **`errCh` buffer = `len(level)`** (`runner.go:62`). Functionally fine, but if the level has 1000 nodes the channel allocates 1000 slots regardless of failures.

### Recommendation

1. Add a configurable semaphore inside `dagrunner.Runner` (e.g. `MaxParallel int` at construction, `chan struct{}` of that size). Acquire before launching each node goroutine.
2. Add a global concurrency cap at `RunCoordinator` level — token bucket of "max concurrent runs" — to protect the API server itself.
3. Implement the documented 30-minute deadline in `RunCoordinator.Spawn` (`runs.go:45`): `ctx, cancel := context.WithTimeout(ctx, 30*time.Minute); defer cancel()`.
4. Move rate limiting to Redis (see `wshub_backend.go` for the existing Redis client pattern) or make it explicit that the in-memory limiter is single-replica only.

---

## 3. Fault tolerance

### What exists

| Mechanism | Location | Notes |
|---|---|---|
| Context propagation | `runner.go:48-92`, `executor.go:83-166` | `ctx` flows from runner → executor → builder/pusher/deployer. |
| Per-level cancellation check | `runner.go:57-59` | Checks `ctx.Err()` before each level. |
| Fail-stop on first error | `runner.go:84-87` | First error from any goroutine in a level returns from `Run`; subsequent levels never start. |
| Kaniko Job cleanup | `kaniko.go:134-140` | `defer Delete(...)` with `PropagationBackground` and 10s grace. Survives ctx cancel. |
| Kaniko BackoffLimit = 0 | `kaniko.go:178` | **One attempt only** — no Kubernetes-level retry of the build pod. |
| Kaniko Timeout (optional) | `kaniko.go:120-124` | Honoured if `KanikoConfig.Timeout > 0`; otherwise unbounded. |
| Run heartbeat + boot-time orphan sweep | `runs.go:13-26`, `store/postgres/run.go:135-149` | `heartbeat_at` updated every 30s; rows stale > 90s at boot get `status='failed'`, `error='orphaned: heartbeat stale at boot'`. |
| WebSocket hub Redis reconnect | `wshub_backend.go:131-169` | Jittered exponential backoff (500ms → 30s). |

### Concerning

- **No retry logic on stage failures** (`executor.go:136-140`). A 500 from a registry push, a transient `kubectl` connection reset, or a race with PVC binding fails the whole pipeline. There is no `Stage.Config.Retries`, no exponential backoff in any builder/pusher/deployer.
- **No skip-downstream / partial-success policy** (`runner.go:84-87`). A failed `test` stage stops the deploy even if the user wanted "best effort". No `continue-on-error` flag on the stage.
- **Stage timeouts are advertised, not enforced.** `model.StageConfig.Timeout` is read into `executor.go:281` for the `custom` stage type and **logged** — never wired into `context.WithTimeout`. Build/Push/Deploy stages have no per-stage timeout knob at all.
- **Stub stages silently succeed.**
  - `executeTest` (`executor.go:190-193`): logs the request, returns `nil`. **Test gates do nothing.**
  - `executeApproval` (`executor.go:273-276`): logs and returns `nil`. **Approval gates auto-approve.**
  - `executeCustom` (`executor.go:279-282`): logs and returns `nil`. **Custom scripts never run.**
  Each of these is a fault-tolerance issue *because* they create false confidence — a green pipeline does not mean the gates passed.
- **No panic recovery anywhere in the executor / runner / app_deployer.** A panic inside `taskFunc` (`runner.go:71`) crashes the goroutine with no `recover()`. `wg.Done()` is deferred (line 67) so the WaitGroup unblocks, but the panic propagates to the runtime and kills the process. Combined with multi-replica + drain-timeout (25s), one malformed input can rolling-crash the whole API tier.
- **No transactional run-state writes.** `RunStore.Update` (`store/postgres/run.go:85-113`) is a single `UPDATE` of all JSONB blobs, but each `Executor` callback mutates the in-memory `stageRunMap` and there's no write-back inside the executor. **The executor never persists progress mid-run** — `Update` is presumably called by the handler after `Execute` returns. If the process dies mid-run, all stage transitions are lost; only the boot orphan sweep marks the run failed.
- **The 30-minute "app-deploy deadline" is documented but absent** (see §2).
- **No circuit breaker.** If BuildKit is unavailable, every queued build hammers it; if a registry is down, every push retries (well, fails once and returns immediately — see point above) without throttling.

### Recommendation

1. Add `Stage.Config.Retries` + `Stage.Config.RetryBackoff` and wrap each stage's builder/pusher/deployer call in a backoff loop. Classify errors first — only retry transient ones (network, 5xx, `context.DeadlineExceeded` if the per-stage timeout fired).
2. Honour `Stage.Config.Timeout` for **all** stage types via `context.WithTimeout`.
3. Add `recover()` at the top of the `taskFunc` goroutine (`runner.go:66`); convert panic into stage failure so the run fails cleanly and other goroutines drain.
4. Persist stage-run progress mid-run. Either pass the `RunStore` into the executor and update after each level, or pump status updates from `runner.Updates()` into the store (the goroutine at `executor.go:147-151` is exactly the right hook — currently it only emits to slog).
5. Implement test/approval/custom stages, **or** explicitly fail them with `errors.New("not implemented")` so users don't ship pipelines that silently auto-pass.
6. Add a circuit-breaker around BuildKit / Kaniko / Pusher calls keyed by endpoint.

---

## 4. System logging

### What exists

| Mechanism | Location | Notes |
|---|---|---|
| Global slog JSON handler | `backend/cmd/cooker/main.go:31` (per agent's read) | `slog.NewJSONHandler(os.Stderr, nil)` — no level filter, no trace-id. |
| Structured stage transitions | `executor.go:111`, `executor.go:149` | `slog.Info("pipeline stage transition", ...)` — fields `pipeline`, `stage`, `status`. |
| Builder log streaming | `kaniko.go:298-328`, `buildah.go:262-280`, `buildkit.go:75-85` | All three honour `Request.LogWriter`. Kaniko and Buildah follow the K8s pod log stream; BuildKit copies the gRPC `SolveStatus.Logs`. |
| App-deploy log fan-out | `app_deployer.go:51, 214-239` | `mwWriter` multiplexes the caller's writer + `LogSink`. Mutex-guarded. |
| StageRun.Logs field exists | `model/run.go:39` | `Logs string \`json:"logs,omitempty"\`` is part of the JSONB stage_runs payload — **the persistence path exists.** |
| Audit logging | `backend/internal/audit/audit.go`, `internal/server/middleware_audit.go` | JSON Lines audit trail for authenticated mutating requests. |

### Concerning

- **The `StageRun.Logs` field is never populated.** `executor.executeBuild` builds `builder.Request` without setting `LogWriter` (`executor.go:168-188`). Same for executePush, executeDeploy. Builder logs therefore go to **nowhere** in the pipeline-run path. The only code path that captures builder logs is `AppDeployer.Deploy` (`app_deployer.go:91`), and even there, the captured stream goes to the *caller's* `logW` — never written into `run.StageRuns[i].Logs`. The misleading comment at `app_deployer.go:90` ("available via the stage runs' Logs field after Execute returns") is **false**.
- **Builder logs are lost on WebSocket disconnect.** With no persistence, a client that disconnects mid-build cannot reattach and read what they missed. There is no replay buffer.
- **Kubernetes deploy events are not captured.** `clientgo.go` (Deploy path) applies manifests but does not stream Pod events / Deployment status — users don't see "ImagePullBackOff" or rollout progress in run logs.
- **No correlation IDs.** `slog` calls in the executor / runner don't attach `run_id` to a context-derived logger; multiple concurrent runs interleave in stderr with only `pipeline` + `stage` fields. Filtering "all logs from run X" is not possible without per-run grep.
- **No log level config.** `main.go:31` (per agent read) hard-codes the JSON handler with default `LevelInfo`. There's no `COOKER_LOG_LEVEL`. Debugging in prod requires a redeploy.
- **No structured "stage started/finished" lifecycle events** — the runner emits `StatusUpdate` over a channel (`runner.go:43-45`) but the only consumer drains them to slog (`executor.go:147-151`). They are not pushed to the WebSocket hub for the frontend graph view, and not persisted.
- **Audit log scope is HTTP-only.** `middleware_audit` covers POST/PUT/PATCH/DELETE — pipeline stage start/finish, build success/failure, and deploy events are not in the audit trail.

### Recommendation

1. **Wire LogWriter through the executor.** In `executor.go:168` add:
   ```go
   logBuf := &bytes.Buffer{}
   req := builder.Request{ ..., LogWriter: logBuf }
   defer func() { sr.Logs = logBuf.String() }()
   ```
   Same pattern for push and deploy. Test that `run.StageRuns[i].Logs` is populated end-to-end.
2. **Stream stage logs to the WebSocket hub** as they're produced (per-line, not at the end). The hub already exists (`wshub_backend.go`); add a per-run channel.
3. **Cap and truncate logs.** A runaway build can fill JSONB. Truncate per-stage log to e.g. 1 MiB and write a marker.
4. **Capture `kubectl apply` events** in the deployer adapter (kubectl events / Deployment.Status conditions) and append to `StageRun.Logs`.
5. **Attach `run_id` to a context-derived logger** at the top of `Execute` (`executor.go:83`), e.g. `logger := slog.With("run", run.ID)`; pass via context to all stage handlers.
6. **Make log level configurable** via `COOKER_LOG_LEVEL`.
7. **Audit pipeline lifecycle events** (run started / failed / deployed) to the same audit sink.

---

## Severity summary

| # | Issue | Severity | File |
|---|---|---|---|
| 1 | Test / Approval / Custom stages silently succeed | **Critical** (false confidence) | `executor.go:190-193, 273-276, 279-282` |
| 2 | `StageRun.Logs` never populated by executor | **High** (lost build/deploy logs) | `executor.go:168-254`, `model/run.go:39` |
| 3 | No retry on transient failures | **High** | `executor.go:136-140`, `runner.go:84-87` |
| 4 | Unbounded goroutine fan-out | **High** at scale | `runner.go:64-79` |
| 5 | Kaniko has no layer-cache wiring | **High** for build perf | `kaniko.go:159-242` |
| 6 | Stage `Timeout` field documented but unenforced | **Medium** | `executor.go:281` |
| 7 | No panic recovery in stage goroutine | **Medium** (process crash) | `runner.go:66-78` |
| 8 | "30-minute deadline" missing from `Spawn` | **Medium** | `runs.go:41-45` |
| 9 | Rate limiter is per-replica, no Redis backend | **Medium** | `ratelimit.go:14-18` |
| 10 | No mid-run progress persistence | **Medium** (orphan-sweep recovers state but loses partial progress) | `executor.go`, `store/postgres/run.go:85-113` |
| 11 | No deploy event / kubectl status capture | **Low/Medium** | `internal/deployer/clientgo.go` |
| 12 | No log-level config / no `run_id` correlation | **Low** | `cmd/cooker/main.go:31` |

---

## Suggested next steps (ordered)

1. **Quick wins (≈1 day combined):** items 6, 7, 8, 12 — small, mechanical edits with high diagnostic value.
2. **Wire log persistence (≈1 day):** item 2 — one of the two highest user-visible improvements.
3. **Stub-out or remove unimplemented stages (≈half day):** item 1 — fail loudly until the stages are real.
4. **Bounded fan-out + global cap (≈1 day):** items 4 + a small chunk of 9 — the right time to add a `MaxParallel` knob to `dagrunner.Runner`.
5. **Retry + per-stage timeout (≈2 days):** items 3 + 6 (full version) — needs error classification (transient vs permanent) and probably a small `retry` package.
6. **Build-cache wiring (≈1-2 days):** item 5 — landing this alongside the in-progress P1.1 (Kaniko adapter, see `backlog.md`) is the natural pairing.

The remaining items (multi-replica rate limit, mid-run progress writes, deploy event capture) are larger and should land after the items above are stable.
