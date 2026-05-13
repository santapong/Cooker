# Performance & Optimization Audit — 2026-05

**Scope:** allocations, latency, throughput, footprint, startup time. Read-only static audit.
**Method:** Source reading across `backend/`, `frontend/`, `deploy/`, `.github/workflows/`. Citations are `file:line` against `claude/project-audit-security-GKXzQ` at audit time.

**Out of scope** (sister audits this week):
- Auth, secrets handling, release engineering — see `2026-05-security-review.md`.
- Go shipping / build tooling — see `docs/shipping-go.md`.

**Builds on, does not duplicate:**
- `docs/audits/dag-performance.md` — already covers Kaniko cache wiring (T-5), bounded fan-out (W3), per-stage timeout (T10), `StageRun.Logs` wiring (T13), retry policy (T10).
- `docs/audits/spof-and-database.md` — already covers missing indexes (#3, #4, #10), `RunStore.Update` write amplification (#11), no `LIMIT` on lists (#12), pool sizing (#15).
- `docs/audits/crash-and-service-quality.md` — already covers audit-sink async (T16 / B.2.7), `mwWriter` mutex (B.2 — Open), `wsLogSink` copy cost (B.2 — Open), `decodeBroadcast` allocation (B.2 — Open).
- `docs/audits/W10-bug-and-chain-recheck.md` — covers lineWriter "always returns len(p)" invariant and `partial` cap.

Where this audit overlaps an open existing finding, the entry below references it instead of restating. New findings are everything not yet catalogued.

---

## Findings — backend

### P26-05-01 — Gin runs in debug mode in production
- **Area:** backend
- **Current behavior:** `internal/server/server.go:102` constructs the router with `gin.Default()`. No call to `gin.SetMode(gin.ReleaseMode)` anywhere in source; `GIN_MODE` is not set in `deploy/docker/Dockerfile` nor in `deploy/helm/cooker/templates/deployment.yaml`.
- **Why it's wasteful:** Debug mode (a) prints a per-route registration banner to stderr at boot (negligible), (b) installs the verbose `gin.Logger()` middleware that logs every request with ANSI colour codes and timestamp formatting — duplicating what `observability.MetricsMiddleware` already records structurally, (c) skips a fast-path in routing/recovery. At 1k req/s the duplicated logger costs ~5-10% CPU and inflates stderr volume ~3×.
- **Proposed fix:** In `main.go` before `server.New`, call `if cfg.Env == "production" || cfg.Env == "uat" { gin.SetMode(gin.ReleaseMode) }`. Replace `gin.Default()` with `gin.New()` + only the middleware Cooker actually wants (`securityHeaders`, `cors`, metrics, audit). The current `gin.Default()` is `gin.Logger()` + `gin.Recovery()`; replace with a slog-bridge or drop entirely since `observability.MetricsMiddleware` already records the per-request stats.
- **Expected win:** ~5-10% CPU on hot HTTP paths; cleaner production logs; ~50 ms shaved off boot output.
- **Risk:** Low. The logger output is duplicative.
- **Effort:** S.

### P26-05-02 — `BroadcastMessage` is allocated per WS message even on the local hot path
- **Area:** backend
- **Current behavior:** `internal/server/wshub_backend.go:79-82` — `memoryHubBackend.Publish` sends `BroadcastMessage{Channel: msg.Channel, Data: msg.Data}` (a value copy of a struct containing a `[]byte`). Every `lineWriter` flush in `executeBuild` calls `hub.Broadcast(channel, line)` → `backend.Publish(BroadcastMessage{...})`, which is then read in `Hub.Run` and iterated over the clients map. The `BroadcastMessage` struct lives only long enough for the receiver to fan it out.
- **Why it's wasteful:** For per-line build log streaming (the executor wraps the builder's stdio in a `lineWriter`), every newline-terminated log line creates: (1) a 2-byte+channel-string+payload slice in `lineWriter.Write` at `internal/service/logbroadcast.go:85-87` via `out := make([]byte, len(line)); copy(out, line)`, (2) a `BroadcastMessage{}` struct value pushed on the memory channel, (3) the receiver iterates clients and `select`-sends `msg.Data` to each `client.send`. A 1k-line Kaniko build hands the GC at minimum 1k slice allocations plus the channel pressure. Estimated allocs: 2-3 per log line × N lines = O(N) in the steady state.
- **Proposed fix:** (a) Pool the per-line `out` byte slice via `sync.Pool` keyed on small size class (64 / 256 / 1024 / 4096). lineWriter.Write copies into a pooled buffer, broadcasts, the client send path returns it. (b) Or: pass a single shared `[]byte` plus an explicit "owned/borrowed" flag and copy only on the slow (Redis) backend. (c) Cheapest: leave the in-memory backend as-is but skip the second copy when `msg.Channel` carries no remote-replica fan-out (single-replica deployments).
- **Expected win:** −40-60% allocations on the per-log-line path; lower GC pause variance during long builds.
- **Risk:** Medium. Pool reuse must respect: a client's `send` channel queues bytes for later consumption by `writePump`. If the slice is returned to the pool before `writePump` writes it, you corrupt the message. The pool should be keyed by writePump completion, not Broadcast return.
- **Effort:** M.

### P26-05-03 — Per-line log allocation in `lineWriter.Write`
- **Area:** backend
- **Current behavior:** `internal/service/logbroadcast.go:85-87` allocates a fresh `out` slice for every emitted line via `out := make([]byte, len(line)); copy(out, line); w.broadcast(...)`. `W10-4` flagged the unnecessary `combined` allocation on the slow path; this is the necessary one (the caller may reuse `p`) but it allocates *per line*, not per Write.
- **Why it's wasteful:** A typical builder writes 50-150 byte lines. At 1000 lines: 1000 × ~96 B = ~96 KB of garbage per build, but more importantly 1000 distinct allocations the GC must track. Combined with the `BroadcastMessage` copy in P26-05-02 and a possible Redis-side copy in `decodeBroadcast` (`wshub_backend.go:284`), a single build line is copied ≥3× before reaching the WebSocket pump.
- **Proposed fix:** sync.Pool of `[]byte`-backed buffers; lineWriter rents one per Write, fills it, hands it to `broadcast`, and the broadcast pipeline returns it after the underlying `writePump` has done its `WriteMessage`. Reference-count if multi-client. Cheapest local fix: enlarge `w.partial`'s reserve capacity, but the multi-client problem remains.
- **Expected win:** −30-50% allocations on log-streaming path under load.
- **Risk:** Medium — coordination with the broadcast lifecycle (see P26-05-02).
- **Effort:** M.

### P26-05-04 — DAG runner allocates a fresh OTel `MapCarrier` and `errCh` per level
- **Area:** backend
- **Current behavior:** `backend/pkg/dagrunner/runner.go:79, 85-86, 93` — every level inside `Run` allocates: `errCh := make(chan error, len(level))`, `carrier := propagation.MapCarrier{}` (a map), `otel.GetTextMapPropagator().Inject(ctx, carrier)` (populates the map), and (when bounded) `sem := make(chan struct{}, r.maxParallel)`. A pipeline with 5 levels means 5 errCh + 5 carriers + 5 semaphores.
- **Why it's wasteful:** Carriers are typically <5 keys — fine — but the `Inject` call always runs even when no tracer is configured (`OTel.GetTextMapPropagator()` returns the no-op propagator by default, but it still iterates and writes header keys). The `errCh` only ever holds at most one drained value because the loop reads it after `wg.Wait()` and returns on the first error. The semaphore is created+discarded per level.
- **Proposed fix:** (a) Hoist `carrier` outside the level loop — context doesn't change between levels. (b) Replace `errCh` with `var firstErr atomic.Value` (or a single `chan error` of size 1 with non-blocking sends). (c) Hoist the semaphore to a runner field. The runner already lives for one `Run` call; one allocation per Run, not per level.
- **Expected win:** −3 allocations × N levels per pipeline run; negligible CPU but tidier hot path. More importantly, fixes the latent issue that `Inject` calls the propagator unconditionally even for short test runs.
- **Risk:** Low.
- **Effort:** S.

### P26-05-05 — `persistProgress` on every stage transition re-marshals all three JSONB blobs
- **Area:** backend / db
- **Current behavior:** `internal/service/executor.go:199, 255, 261` — `e.persistProgress(ctx, run)` fires on every stage start, every stage finish, and every retry attempt. Each call routes to the configured `RunUpdater`, which for the Postgres store is `RunStore.Update` (`internal/store/postgres/run.go:85-113`). That method re-marshals `stage_runs`, `env_statuses`, and `variables` into JSON every time and rewrites the row.
- **Why it's wasteful:** A 10-stage pipeline produces 20+ Update calls (start + finish per stage). Each call serialises three JSONB blobs even though only `stage_runs` changes during normal progression. With `env_statuses` ~200 B and `variables` ~500 B, that's ~14 KB of redundant marshalling + 14 KB of Postgres write amplification per stage transition. At 100 concurrent pipelines × 1 transition/sec, this is ~1.4 MB/s of useless Postgres write traffic.
- **Proposed fix:** Already flagged as `spof-and-database.md` #11. Add `RunStore.UpdateStage(ctx, runID, stageIdx, stageRun)` that only re-marshals `stage_runs`. Or even better: write an append-only event log (`run_events(run_id, ts, kind, payload)`) and project on read; the row remains an immutable summary.
- **Expected win:** −66% serialisation cost per `persistProgress`; −2/3 row-write size; massively reduced WAL.
- **Risk:** Medium — every executor callsite that mutates run state needs to declare what changed. Concurrent `Update` (user PATCH) vs partial-update (executor) must not lose the user's edit.
- **Effort:** M.

### P26-05-06 — `RunStore.SweepOrphans` uses `fmt.Sprintf` to build an interval string passed as a query parameter
- **Area:** backend / db
- **Current behavior:** `internal/store/postgres/run.go:142-143` builds the SQL parameter via `fmt.Sprintf("%d milliseconds", threshold.Milliseconds())` and passes it as `$1::interval`. The fmt format runs on every boot.
- **Why it's wasteful:** Mostly aesthetic — the call only runs once per boot. But the `::interval` cast forces Postgres to parse a string at every execution. A typed-cast approach (`($1 || ' milliseconds')::interval`) or simpler `NOW() - make_interval(0, 0, 0, 0, 0, 0, $1::int)::interval` is cleaner. The real performance angle: the predicate `heartbeat_at IS NULL OR heartbeat_at < NOW() - $1::interval` cannot use `idx_pipeline_runs_running_heartbeat` for the `IS NULL` half because partial-index conditions don't cover null cases gracefully.
- **Proposed fix:** Pass threshold as seconds (`bigint`) and cast inside SQL with `NOW() - ($1 || ' seconds')::interval`. Also split the index into two: keep `idx_pipeline_runs_running_heartbeat (heartbeat_at) WHERE status='running'` for the time-based half, and let the orphan-sweep run two passes: one for null heartbeats (cheap because the partial index is keyed on rows that have one), one for stale. Or change the schema so `heartbeat_at` is NOT NULL with a sentinel value on insert.
- **Expected win:** Negligible CPU (boot-time only) but removes a footgun and clarifies the index design.
- **Risk:** Low.
- **Effort:** S.

### P26-05-07 — `pingWithBackoff` runs the migration loop after a successful ping even though the lock + version-table sequence is already its own retry-loop
- **Area:** backend
- **Current behavior:** `internal/store/postgres/store.go:105-113` — after `pingWithBackoff` returns, `applyMigrations` is called inline before `NewStore` returns. Migration applies under `pg_advisory_lock`, so only one replica's migration body runs at a time. The other replicas block in `pg_advisory_lock($1)` (`store.go:205`).
- **Why it's wasteful:** On a multi-replica boot, all N replicas race the advisory lock; the loser blocks until the winner finishes. At ~150ms for the cold migration loop on PG16 with 8 migrations, that's 150ms × N replicas = serialised boot. Worse: if a future migration is slow (a backfill, an index build), all replicas line up waiting and the readiness window inflates.
- **Proposed fix:** (a) Run `applyMigrations` from a dedicated migration Job/CronJob in the chart, not from the API pod's boot path. (b) Or short-circuit: check `schema_migrations` first; if all known versions are recorded, skip the advisory lock entirely. Reads the table with one query and returns immediately on the no-op path. Big win on the boot of replicas 2-N because the lock acquisition is also a network round-trip.
- **Expected win:** Multi-replica boot time `O(1)` instead of `O(N)`. For N=3 replicas, ~300ms shaved off rolling-restart latency.
- **Risk:** Medium — the fast-path read must be transactionally consistent or you race a migration in progress. A simple "is the highest known migration applied?" check is safe if the answer is yes.
- **Effort:** S.

### P26-05-08 — `applyMigrations` re-reads every `*.up.sql` from the embedded FS on every boot even when nothing's new
- **Area:** backend
- **Current behavior:** `internal/store/postgres/store.go:225-257` iterates every file in `migrations/`, reads it via `migrationsFS.ReadFile`, and only the `if applied[version] { continue }` check on line 231 skips already-applied ones — but `ReadDir` and the iteration happen unconditionally.
- **Why it's wasteful:** Negligible (the FS is embedded; reads are memory-to-memory). But this combined with P26-05-07 means every boot pays the full sweep cost. On 50+ migrations the file scan + applied-set lookup is still trivial; just noting it doesn't compound.
- **Proposed fix:** No action — leave the loop as-is. Listed for completeness because someone scanning for "hot startup paths" would notice it.
- **Risk:** —
- **Effort:** —

### P26-05-09 — `lineWriter.partial` slice can grow unbounded (W10-2 already raised)
- **Area:** backend
- **Current behavior:** `internal/service/logbroadcast.go:67-75`.
- See **`W10-2`** in `W10-bug-and-chain-recheck.md`. Not re-flagged — already in the backlog.

### P26-05-10 — `cappedBuffer.String()` returns a heap copy of the whole buffer on every stage finish
- **Area:** backend
- **Current behavior:** `internal/service/executor.go:316-321` — the deferred block in `executeBuild` calls `sr.Logs = logs.String()` exactly once per stage. `bytes.Buffer.String()` returns `string(b.buf[b.off:])`, which is a fresh allocation of `b.Len()` bytes (because Go's `string(bytes)` always copies unless the compiler can prove the slice is dead).
- **Why it's wasteful:** Up to 1 MiB allocation per build stage finish (the `stageLogCap`). The buffer is no longer needed after this assignment, so the copy is unavoidable with the current shape, but it doubles RSS temporarily during the assignment.
- **Proposed fix:** Use `unsafe.String(unsafe.SliceData(b.Bytes()), b.Len())` to alias the buffer's backing array as a string. Requires the buffer to be abandoned after the assignment (no `b.Reset()`, no further `Write`), which is true here. Or, change `StageRun.Logs` to `[]byte` and pass the buffer's bytes directly to the JSONB encoder (Postgres treats them identically).
- **Expected win:** −1 MiB allocation per stage finish; doubles available headroom during long pipelines.
- **Risk:** Medium — `unsafe.String` requires careful proof the underlying slice is immutable for the lifetime of the resulting string. The buffer must be unreachable after the cast.
- **Effort:** S (with unsafe) / M (with []byte refactor).

### P26-05-11 — Status-update drain goroutine in `Execute` does only slog logging (not WS or store)
- **Area:** backend
- **Current behavior:** `internal/service/executor.go:266-270` — `runner.Updates()` channel is drained in a goroutine that only emits `slog.Info("pipeline stage transition", ...)`. The status updates also fire inside `taskFunc` via `e.persistProgress` (P26-05-05), so the channel signal is double-counted.
- **Why it's wasteful:** The runner emits a `StatusUpdate` on every state change (running/success/failed), so for an N-stage pipeline this is `~3N` channel sends — each requires a `emitStatus` lock acquisition on `r.mu` (`runner.go:144-153`) and a goroutine wake on the receive side. The drain goroutine literally only logs them, which the executor's per-stage `slog.Info("pipeline executing stage", ...)` already covers more richly.
- **Proposed fix:** Remove the drain goroutine entirely OR drive it from the same path that does `persistProgress`. The `slog` line in the drain is strictly redundant with the per-stage logger inside `taskFunc`. Removing it also lets us simplify `Runner.Updates()` to fire-and-forget (no required reader).
- **Expected win:** −1 goroutine per pipeline run; −3N channel sends per run; modest CPU savings under high pipeline concurrency.
- **Risk:** Low — the channel currently must be drained (otherwise `runner.emitStatus`'s `r.updates <- ...` would block).
- **Effort:** S.

### P26-05-12 — `rateLimiter.limiterFor` takes the global mutex even for already-registered users
- **Area:** backend
- **Current behavior:** `internal/server/ratelimit.go:49-59` — every authenticated request through the expensive-routes middleware acquires `rl.mu.Lock()`, looks up the bucket, updates `lastSeen[key] = time.Now()`, and returns the limiter. The mutex is a write-lock, even on the common case where the bucket already exists.
- **Why it's wasteful:** With ~50 concurrent users hitting expensive endpoints (pipeline run / build / deploy), the limiter map is a single-point of serialisation. `rate.Limiter` is internally thread-safe; the only thing the mutex protects is the map. At ~1000 req/s through this middleware the lock becomes hot.
- **Proposed fix:** Use `sync.RWMutex`. Read-lock to look up; if missing, drop and re-acquire write-lock with double-check. `lastSeen` update can use a separate `atomic.Pointer[time.Time]` per bucket or be batched (the gc loop already tolerates approximate timestamps).
- **Expected win:** −90% lock contention on hot rate-limit paths; throughput uplift proportional to the number of users hitting expensive routes concurrently.
- **Risk:** Low. Race on `lastSeen` is benign — gc only deletes after `interval` of staleness.
- **Effort:** S.

### P26-05-13 — `rateLimitKey` does `c.ClientIP()` on every request (parses Forwarded headers)
- **Area:** backend
- **Current behavior:** `internal/server/ratelimit.go:100-105` — falls back to `c.ClientIP()` when no OIDC subject is present. The authenticated paths fall through to user-sub (good), but unauthenticated webhook / health paths still hit this.
- **Why it's wasteful:** `gin.Context.ClientIP()` walks `X-Forwarded-For`, `X-Real-IP`, and `RemoteAddr`. For authenticated routes (the common case), the IP path is dead code but the cost of dereferencing claims and the branch are still paid. Negligible per-request (~µs) but visible at 10k req/s.
- **Proposed fix:** Inline the user-sub fast path: `if claims := auth.GetUser(c); claims != nil && claims.Subject != "" { ... }` — already done; the dead branch is fine. No change needed unless we see this in a profile.
- **Risk:** —
- **Effort:** —

### P26-05-14 — `mwWriter` mutex serialises every log line through `AppDeployer.Deploy` (already in crash-and-service-quality B.2)
- **Area:** backend
- **Current behavior:** `internal/service/app_deployer.go:241-247` — `Write` holds `mu` across two underlying `Write` calls.
- See **B.2** in `crash-and-service-quality.md`. Not re-flagged.

### P26-05-15 — `wsLogSink.Write` allocates per byte slice (already in crash-and-service-quality B.2)
- **Area:** backend
- **Current behavior:** `internal/handler/app.go:244-249` does `append([]byte(nil), p...)`.
- See **B.2** in `crash-and-service-quality.md`. Not re-flagged.

### P26-05-16 — `encodeBroadcast`/`decodeBroadcast` allocate full payload + a Data copy per Redis message
- **Area:** backend
- **Current behavior:** `internal/server/wshub_backend.go:265, 284` — `encodeBroadcast` does `make([]byte, prefix+channel+data)`; `decodeBroadcast` does `append([]byte(nil), payload[end:]...)` to copy the Data out of the read buffer.
- **Why it's wasteful:** Every Redis broadcast in multi-replica deployments pays 2 allocations (encode + decode-copy). The decode copy is necessary because go-redis reuses its read buffer; the encode allocation could be pooled. At 1000 broadcast/sec across 3 replicas, that's 6000 alloc/s on this path.
- **Proposed fix:** sync.Pool the encode buffer keyed on size class. Decode-side is harder because the data slice escapes into the local channel and onto client send queues; tying the lifetime to "after all writePumps consume" is the same problem as P26-05-02. Land them together.
- **Expected win:** −50% allocations on Redis-broadcast path.
- **Risk:** Medium (lifetime tracking).
- **Effort:** M.

### P26-05-17 — Stage logs are kept in memory at full size (1 MiB) before persistence
- **Area:** backend
- **Current behavior:** `internal/service/executor.go:301, 316-321` — `cappedBuffer` accumulates the entire stage's output (up to 1 MiB) and writes it as a single `sr.Logs = logs.String()` at stage end.
- **Why it's wasteful:** For long-running pipelines, every concurrent build holds up to 1 MiB of log buffer in RAM. At 100 concurrent runs × 3 stages average = 300 MiB peak buffer footprint, on top of all other process memory. Combined with the goroutine fan-out cap of 16 in `defaultMaxParallel` (`executor.go:37`), that's ≤16 active stages × 1 MiB = 16 MiB per run × N concurrent runs. Manageable, but on a deep coordinator that's a real cost.
- **Proposed fix:** Stream-write to a temp file (or directly to Postgres in batches every 64 KiB) and persist the final reference instead of the bytes. Or: aggressively shrink the buffer's backing array on truncation (`bytes.Buffer` grows in 2× steps and never shrinks).
- **Expected win:** −16 MiB peak per concurrent run; allows raising `stageLogCap` without scaling the live footprint.
- **Risk:** Medium — changes the StageRun.Logs durability model.
- **Effort:** L.

### P26-05-18 — `bytes.IndexByte`-driven loop in `lineWriter.Write` rescans the same prefix after the slow-path prepend
- **Area:** backend
- **Current behavior:** `internal/service/logbroadcast.go:62-79` — on the slow path, `partial` is prepended to `p` into `combined`. The newline-scan loop on line 72 then `bytes.IndexByte(combined, '\n')` searches the whole concatenated buffer, including the prefix that was just buffered (and which by construction has no newline — `partial` is only ever the tail after the last newline of the previous Write).
- **Why it's wasteful:** Negligible CPU — the prefix is small — but redundant. The first IndexByte should start at `len(partial)` rather than the start.
- **Proposed fix:** Track an offset into `combined` and start `IndexByte` at `len(partial)` on the first iteration only. Or split the slow path: search the new `p` for the first newline; if found, the line is `partial + p[:firstNewline+1]`, no concatenation needed for the rest.
- **Expected win:** Marginal CPU savings; cleaner code.
- **Risk:** Low.
- **Effort:** S.

### P26-05-19 — `Postgres dialect` `database/sql` uses `lib/pq` (deprecated)
- **Area:** backend / db
- **Current behavior:** `internal/store/postgres/store.go:19` imports `_ "github.com/lib/pq"`. The Go ecosystem standard has shifted to `pgx` (which has both a `database/sql` shim and a native API). `lib/pq` is in maintenance mode — no new features, security fixes only.
- **Why it's wasteful:** `pgx` reports 2-3× faster parse/encode for JSONB-heavy workloads (which Cooker is — every Run row carries three JSONB blobs). Connection pool semantics are also better (true async work) and the prepared-statement cache is larger by default.
- **Proposed fix:** Migrate to `jackc/pgx/v5` via the `stdlib` shim. Drop-in for `database/sql` — change the import and the driver name from `"postgres"` to `"pgx"` in `sql.Open`. Phase 2: adopt `pgx` native for the high-throughput paths (run create / update).
- **Expected win:** 20-50% throughput uplift on JSONB-heavy paths.
- **Risk:** Medium — driver swap; CI tests should catch divergence but the pool tuning may need adjustment.
- **Effort:** S for the shim swap; M for native adoption.

### P26-05-20 — `MetricsMiddleware` calls `c.FullPath()` and `strconv.Itoa(status)` per request
- **Area:** backend
- **Current behavior:** `internal/observability/observability.go:135-147` — for every request: `c.FullPath()` walks the trie, `strconv.Itoa(c.Writer.Status())` allocates a string, then `WithLabelValues(method, route, status)` does a 3-string map lookup in the Prometheus collector.
- **Why it's wasteful:** At 5k req/s the `WithLabelValues` cost dominates (~500 ns per call internally because it has to hash three strings). `strconv.Itoa` for status codes 200-599 could be a pre-computed table.
- **Proposed fix:** (a) Cache the three commonly-seen statuses (200, 404, 500) as preallocated strings (Go does this for small ints already in some paths; check generated code). (b) For higher leverage: use `prometheus.GetMetricWith(prometheus.Labels{...})` once at handler-construction time and store the resulting `Counter`/`Histogram` directly on the route — eliminates the per-request map lookup. But that conflicts with Gin's runtime route matching.
- **Expected win:** ~5-10% reduction in per-request CPU at high throughput. Mostly profiler-noise below 1k req/s.
- **Risk:** Low.
- **Effort:** S (caching) / M (handler-bound metrics).

### P26-05-21 — Kaniko `streamLogs` uses `bufio.Scanner` with default 64 KiB max line buffer
- **Area:** backend
- **Current behavior:** `internal/builder/kaniko.go:343-349` — `scanner := bufio.NewScanner(stream)` with no `scanner.Buffer(...)` call, so the default `bufio.MaxScanTokenSize = 64 KiB` applies.
- **Why it's wasteful:** A Kaniko build that emits a single >64 KiB log line (e.g. a JSON blob from `npm ci --verbose`) causes the scanner to silently drop the line with `bufio.ErrTooLong`. The defer-close on line 342 means the stream itself stays attached, but logs go dark.
- **Proposed fix:** `scanner.Buffer(make([]byte, 64<<10), 1<<20)` to allow up to 1 MiB lines, or switch to `bufio.NewReader(stream).ReadBytes('\n')` which has no line cap. Allocations are similar; the difference is the no-cap fallback.
- **Expected win:** Reliability (no silent log loss); negligible perf.
- **Risk:** Low.
- **Effort:** S.

### P26-05-22 — `splitManifest` builds the docs slice eagerly even for the single-doc case
- **Area:** backend
- **Current behavior:** `internal/deployer/clientgo.go:154-177` — `splitManifest` always reads through `utilyaml.NewYAMLReader` (which scans for `---` separators) and accumulates into a `docs [][]byte`. For the single-doc case (the common one when Cooker synthesises a Deployment+Service), this is two reads and one append into a slice.
- **Why it's wasteful:** Minor; the YAML reader allocates intermediate buffers. Avoiding it for single-doc manifests would skip one allocation per Deploy.
- **Proposed fix:** Fast-path: if `bytes.Index(b, []byte("\n---"))` returns -1, treat `b` as a single doc and skip the reader.
- **Expected win:** Marginal — Deploy is called once per stage, not in a hot loop.
- **Risk:** Low.
- **Effort:** S. (Not worth doing in isolation.)

### P26-05-23 — `splitManifest` calls `bytes.TrimSpace` on the raw doc just to test emptiness
- **Area:** backend
- **Current behavior:** `internal/deployer/clientgo.go:168-170` — `if len(bytes.TrimSpace(raw)) == 0` allocates a trimmed copy (`bytes.TrimSpace` is in-place when the slice borders are already clean, but typically allocates).
- **Proposed fix:** `if isAllWhitespace(raw)` with a manual byte scan. Three lines.
- **Expected win:** Negligible.
- **Effort:** S. **Don't bother** in isolation.

---

## Findings — frontend

### P26-05-24 — No route-level code splitting; every page (including `@xyflow/react`) ships in the initial bundle
- **Area:** frontend
- **Current behavior:** `frontend/src/App.tsx:6-21` imports every page synchronously. `@xyflow/react` is imported transitively via `frontend/src/components/pipeline/PipelineCanvas.tsx:15` and `frontend/src/components/compose/ComposeCanvas.tsx:13` (with `@xyflow/react/dist/style.css`). Two pages need xyflow; the rest don't.
- **Why it's wasteful:** `@xyflow/react` is ~150 KiB minified+gz. A user on the Apps page (the default landing — `App.tsx:41-42`) downloads xyflow even though they never see a pipeline canvas. Same for `oidc-client-ts` (~30 KiB) and all the page chunks.
- **Proposed fix:** Use `React.lazy` + `Suspense` for each non-landing route. Critically split:
  - `PipelineEditorPage`, `ComposePage`, `RunPage` (xyflow-using) → separate chunk.
  - `NewAppWizard`, `SettingsPage`, `KubernetesPage`, `RegistryPage` → separate chunks.
  - Keep `AppsPage` + `AppDetailPage` in the main bundle (default landing route).
  - Configure Vite's `build.rollupOptions.output.manualChunks` to split vendor libraries: react, react-router, oidc, xyflow, zustand each into their own chunk for cache stability.
- **Expected win:** Initial bundle from ~600-800 KB to ~200-300 KB. Time-to-interactive on cold load roughly halved.
- **Risk:** Low — Suspense with a Skeleton fallback is already in use for `ProtectedRoute`.
- **Effort:** S.

### P26-05-25 — Multiple Zustand stores consumed without selectors → re-render storms
- **Area:** frontend
- **Current behavior:** Found via grep:
  - `frontend/src/pages/RunPage.tsx:590` — `const { environments, fetchEnvironments } = useEnvironmentStore();`
  - `frontend/src/pages/KubernetesPage.tsx:10` — `const { ... } = useKubernetesStore();`
  - `frontend/src/pages/PipelineEditorPage.tsx:19` — `const { pipeline, loadPipeline, savePipeline, selectedNodeId } = usePipelineStore();`
  - `frontend/src/pages/DockerPage.tsx:15` — `const { images, containers, loading, fetchImages, fetchContainers } = useDockerStore();`
  - `frontend/src/components/pipeline/PipelineCanvas.tsx:45` — `const store = usePipelineStore();`
  - `frontend/src/components/compose/ComposeCanvas.tsx:28` — `const store = useComposeStore();`
  - `frontend/src/pages/EnvironmentsPage.tsx:22` — `const { environments, loading, fetchEnvironments, createEnvironment } = useEnvironmentStore();`
  - `frontend/src/pages/ComposePage.tsx:11` — `const { fetchComposeGraph, selectedServiceName, loading, error, graph } = useComposeStore();`
  - `frontend/src/components/compose/panels/ServiceConfigPanel.tsx:4` — same pattern.
  - `frontend/src/components/pipeline/panels/NodeConfigPanel.tsx:9` — same pattern.
- **Why it's wasteful:** Zustand's `useStore()` without a selector subscribes the component to **every** state change. Every keystroke in the editor pane re-renders the canvas, every WebSocket log line re-renders the entire RunPage. With the pipeline editor, this means dragging a node through the xyflow canvas re-renders all the panels (and re-evaluates the node config form).
- **Proposed fix:** Replace each `const store = useFooStore()` with `const x = useFooStore(s => s.x)` and `const y = useFooStore(s => s.y)`, or use `useFooStore(useShallow(s => ({ x: s.x, y: s.y })))` with shallow equality. Hottest fix-first: `PipelineCanvas.tsx:45` and `ComposeCanvas.tsx:28` — those re-render xyflow on every input change in the editor panel.
- **Expected win:** −50-90% renders on the editor/canvas during interaction. Smoother drag, faster keystrokes.
- **Risk:** Low. Need to verify each component only uses the fields it actually reads.
- **Effort:** M.

### P26-05-26 — `useStageLogs` line-trim does `slice(next.length - MAX_LINES)` on every overflow message (W10-20)
- **Area:** frontend
- **Current behavior:** `frontend/src/hooks/useStageLogs.ts:99-112`.
- See **W10-20** in `W10-bug-and-chain-recheck.md`. Already deferred.

### P26-05-27 — `useStageLogs` does a separate REST backfill before WS attach
- **Area:** frontend
- **Current behavior:** `frontend/src/hooks/useStageLogs.ts:65-88` fetches `/pipelines/:id/runs/:runId/logs/:stageId` via REST, then `useWebSocket` connects separately. Two round-trips: one for backfill, one for the WS upgrade (plus the ticket fetch).
- **Why it's wasteful:** When a user navigates to a Run page, they wait for REST → render initial buffer → WS ticket fetch → WS upgrade. That's 3 sequential RTTs for the first paint of a stage log.
- **Proposed fix:** Have the WS endpoint emit a "history" message on connect containing the on-disk capture, then live-tail. One RTT (the WS upgrade), no REST. The ticket fetch is already separate; if we batch the ticket fetch with the auth-discovery probe at app boot we save another RTT.
- **Expected win:** First-paint of a log stream goes from ~3 RTTs to 1.
- **Risk:** Medium — the WS message protocol grows from a single "log line" frame to a discriminated union ("history" | "line"). Front and back must agree.
- **Effort:** M.

### P26-05-28 — Vite build doesn't configure `build.rollupOptions.output.manualChunks` for vendor splitting
- **Area:** frontend
- **Current behavior:** `frontend/vite.config.ts` only sets `sourcemap` conditionally. No `manualChunks` declaration, no `chunkSizeWarningLimit` raise. By default Vite/Rollup ships one giant vendor chunk.
- **Why it's wasteful:** A bump in any dependency invalidates the entire vendor chunk; users redownload ~600 KB to add 5 KB of patch. Cache hit ratio on push is essentially 0% across releases.
- **Proposed fix:** Pair with P26-05-24. Add `manualChunks: { react: ['react', 'react-dom', 'react-router-dom'], xyflow: ['@xyflow/react'], oidc: ['oidc-client-ts'], zustand: ['zustand'] }` to vite config.
- **Expected win:** Cache hit ratio across deploys jumps to ~80% (assuming most patches don't touch vendor deps).
- **Risk:** Low.
- **Effort:** S.

### P26-05-29 — `useWebSocket` recreates `ws.onmessage` closure on every render
- **Status:** CLOSED — see "Closed findings" section below.
- Fixed in `claude/w3-p26-05-29-onmessage-ref`.

### P26-05-30 — `JSON.parse(event.data)` on every WS log line in `useWebSocket`
- **Area:** frontend
- **Current behavior:** `useWebSocket.ts:99-105` — every WS message tries `JSON.parse` first, falls back to raw on parse failure.
- **Why it's wasteful:** Stage log lines are raw text (the executor's `lineWriter` broadcasts `[]byte` directly). `JSON.parse("hello world")` throws, the catch path runs, the raw is used. For 1000 log lines that's 1000 throws/catches. V8 deoptimises try/catch hot paths somewhat aggressively.
- **Proposed fix:** Make the protocol explicit: either always-JSON (executor wraps the line in a JSON envelope — costs an alloc on the backend) or always-raw (frontend skips `JSON.parse` for log channels and passes the string straight to `onMessage`). For now: peek at the first byte; only `JSON.parse` if it's `{` or `[`.
- **Expected win:** −1 throw per log line; possible V8 hot-path uplift on log-heavy run pages.
- **Risk:** Low.
- **Effort:** S.

---

## Findings — container

### P26-05-31 — Final image is `alpine:3.19` (~7 MB base) with `git`, `docker-cli`, `kubectl` (~150 MB total)
- **Area:** container
- **Current behavior:** `deploy/docker/Dockerfile:37-69` — installs `ca-certificates git docker-cli` via apk; downloads kubectl (~50 MB).
- **Why it's wasteful:** The runtime image carries `docker-cli` even in Kaniko/Buildah deployments where the cooker pod never shells out to it (the Kaniko adapter submits Jobs, not local docker). `git` is needed only by `internal/source/github/clone.go` (which has switched to `go-git/v5` per backlog P9.3) — let me check.
- **Cite for git dep:** `backlog.md:175` notes "go-git/v5 — gogit.go shipped"; if `internal/source/github/clone.go` still shells out to git, the binary is needed. **Recommend confirmation** that `internal/source/github/` is purely go-git, then drop `git` from the apk install.
- **Proposed fix:** (a) Make `docker-cli` install conditional on a build-arg (`INCLUDE_DOCKER_CLI=true`) so non-docker builders ship slimmer images. (b) If `internal/source/github` is pure-go, drop `git`. (c) Switch base to `gcr.io/distroless/static-debian12` since cooker runs CGO_ENABLED=0 — drops ~7 MB of alpine + adds a stronger security posture. Just keep kubectl as a separate copy from a build stage.
- **Expected win:** Image size ~150 MB → ~80 MB (drop docker-cli); ~80 MB → ~60 MB (distroless); ~60 MB → ~50 MB (drop git if unused). Pull time across 100 nodes saves ~10 GB transfer per rolling deploy.
- **Risk:** Medium — distroless has no shell, breaking `HEALTHCHECK` and any `kubectl exec` debugging. Mitigation: keep alpine for prod, ship a distroless variant tag for security-sensitive ops.
- **Effort:** S to drop the apk extras conditionally; M for the distroless variant.

### P26-05-32 — `HEALTHCHECK` uses `wget` (depends on alpine's wget; gone under distroless)
- **Area:** container
- **Current behavior:** `Dockerfile:78-79` — `HEALTHCHECK ... CMD wget -qO- http://localhost:8080/health > /dev/null || exit 1`.
- **Why it's wasteful:** Forks `/bin/sh` and `wget` every 30s. Negligible CPU but a per-pod 1 process/30s × N pods adds up; it's also dead weight if we move to distroless.
- **Proposed fix:** Add a `cooker healthcheck` subcommand to the binary that does the HTTP probe via `net/http`. `HEALTHCHECK CMD ["cooker", "healthcheck"]`. Saves the shell fork, works under distroless.
- **Expected win:** Minor CPU savings; enables distroless transition.
- **Risk:** Low.
- **Effort:** S.

### P26-05-33 — Frontend static files copied flat into `/usr/share/cooker/static`; not gzipped, no immutable headers
- **Area:** container / backend
- **Current behavior:** `Dockerfile:71` copies `frontend-build/dist` into `/usr/share/cooker/static`. `internal/server/router.go:262` uses `s.router.Static("/assets", ...)`, which serves files with Gin's default headers — no `Cache-Control: public, max-age=31536000, immutable` and no `Content-Encoding: gzip` even when a `.gz` sibling exists.
- **Why it's wasteful:** (a) Every page reload re-downloads the full bundle because nothing is cacheable past the default Gin headers. (b) Vite emits hashed filenames (already cache-busted), so they could be served with immutable; (c) gzip-compressing the bundle saves ~70% over the wire.
- **Proposed fix:** (a) In `Dockerfile`, after the `COPY --from=frontend-build`, add a `gzip -k -9` pass over `assets/*.js` and `assets/*.css`. (b) Replace `router.Static` with a custom handler that sets `Cache-Control: public, max-age=31536000, immutable` for `assets/` (hashed files) and serves the `.gz` variant when the client sends `Accept-Encoding: gzip`. (c) Optionally also pre-brotli with `brotli -k -q 11` — better compression for the same delivery.
- **Expected win:** ~70% bandwidth reduction on JS/CSS; ~95% cache hit rate on repeat visits.
- **Risk:** Low.
- **Effort:** S.

---

## Findings — CI

### P26-05-34 — Backend tests run packages one-at-a-time in a sequential `for` loop
- **Area:** CI
- **Current behavior:** `.github/workflows/ci.yml:64-79` — `for pkg in $(go list ./...); do go test -race -timeout 90s "$pkg"; done`. The comment says this is to surface failing packages clearly.
- **Why it's wasteful:** `go test -race ./...` runs packages in parallel by default (bounded by `GOMAXPROCS`). The per-package loop disables that parallelism. For ~30 backend packages × ~3s average test time, sequential is ~90s; parallel would be ~10-20s.
- **Proposed fix:** Run `go test -race -timeout 90s ./...` in one invocation. To still get per-package output on failure, use `-json | tparse` or `go test ... -v` + a parse step.
- **Expected win:** Backend test job ~90s → ~20s. Net CI wall-clock cut by ~70s.
- **Risk:** Low — the parallel run is what `go test` does for all other projects.
- **Effort:** S.

### P26-05-35 — No Go module cache or build cache in CI
- **Area:** CI
- **Current behavior:** `.github/workflows/ci.yml:31-35` uses `actions/setup-go@v5` with default settings. Setup-go enables module cache by default (good), but the build cache (`$GOCACHE`) is not preserved — every PR rebuilds from scratch.
- **Why it's wasteful:** `go build ./...` cold-builds the whole tree on every CI run. With kube-client-go and OTel pulled in, that's ~2 minutes of compilation.
- **Proposed fix:** Add an `actions/cache` step keyed on `go.sum` for `$GOCACHE` (typically `~/.cache/go-build` on Linux). Or: setup-go v5 has a `cache: true` flag (default true) that handles module cache but not GOCACHE — confirm via the action's docs and add an explicit step if needed.
- **Expected win:** Build phase from ~2min → ~30s on warm runs.
- **Risk:** Low — caches can be busted with new `go.sum`.
- **Effort:** S.

### P26-05-36 — No frontend lint/test/build parallelism within the frontend job
- **Area:** CI
- **Current behavior:** `.github/workflows/ci.yml:90-101` runs `npm ci → lint → build → test` sequentially.
- **Why it's wasteful:** `lint` and `test` are independent of `build`. Three sequential steps × ~20s each = ~60s; could be ~30s if split into two jobs (one for build+typecheck, one for lint+test).
- **Proposed fix:** Split into two parallel jobs that share an `npm ci` step via the `setup-node` action's cache.
- **Expected win:** Frontend job ~75s → ~45s.
- **Risk:** Low.
- **Effort:** S.

### P26-05-37 — Helm template jobs run 12 separate `helm template` invocations in sequence
- **Area:** CI
- **Current behavior:** `.github/workflows/ci.yml:103-296` — the helm job runs `helm lint`, then 10+ separate `helm template` renders + assertions.
- **Why it's wasteful:** Each `helm template` is ~1-2s; doing them in series = ~20-30s. Negligible in absolute terms but trivially parallelisable.
- **Proposed fix:** Either run them in a single bash loop with all the renders backgrounded (`& wait`) or split the helm job into a matrix.
- **Expected win:** Helm job ~30s → ~10s.
- **Risk:** Low.
- **Effort:** S. **Probably not worth doing** — the helm job isn't the critical path.

### P26-05-38 — Docker job has `needs: [backend, frontend, helm]` — serialises the most expensive job
- **Area:** CI
- **Current behavior:** `.github/workflows/ci.yml:298-304` — `docker` waits for all three preceding jobs.
- **Why it's wasteful:** Docker build is ~3-5 minutes (multi-stage Go + Node + alpine). Running it after the test jobs adds latency to the failure signal for any docker-only issue (Dockerfile syntax, base image fetch failures).
- **Proposed fix:** Drop `needs:`. Docker build is independent. Failing-fast on the test side stays; failing-fast on the docker side becomes possible.
- **Expected win:** Critical-path CI from ~8 min → ~5 min for the typical case (where test+build both pass).
- **Risk:** Low — wastes a few seconds of compute when tests fail, in exchange for parallel signal.
- **Effort:** S.

### P26-05-39 — Docker build has no `cache-from`/`cache-to` and no buildx layer cache
- **Area:** CI
- **Current behavior:** `.github/workflows/ci.yml:303-304` — `docker build -t cooker:ci -f deploy/docker/Dockerfile .`. No buildkit cache, no registry-backed cache.
- **Why it's wasteful:** Every CI run cold-builds the multi-stage image: full `npm ci`, full Go build, full kubectl fetch. ~3-5 minutes.
- **Proposed fix:** Switch to `docker/setup-buildx-action` + `docker/build-push-action@v5` with `cache-from: type=gha,cache-to: type=gha,mode=max`. The GitHub Actions cache backend persists layer cache across runs.
- **Expected win:** Docker job from ~4min → ~1min on warm cache; ~30s on perfect cache.
- **Risk:** Low.
- **Effort:** S.

---

## Top 10 wins (impact × ease)

Ranked by (user-visible perf improvement × ease of landing). Score is qualitative.

| # | ID | Win | Effort | Impact |
|---|---|---|---|---|
| 1 | **P26-05-24** + **P26-05-28** | Lazy-load routes + manualChunks vendor split | S | Initial bundle ~−50%; TTI ~halved |
| 2 | **P26-05-01** | Switch to `gin.ReleaseMode` / `gin.New()` | S | ~5-10% CPU on hot HTTP paths |
| 3 | **P26-05-34** | Run `go test -race ./...` in one invocation | S | CI ~−70s |
| 4 | **P26-05-39** | Docker layer cache via gha buildx | S | CI docker ~−3min |
| 5 | **P26-05-25** | Add selectors to all `useFooStore()` calls | M | Editor render storm −50-90% |
| 6 | **P26-05-33** | Gzip + immutable headers on static assets | S | ~70% bandwidth on JS/CSS |
| 7 | **P26-05-12** | RWMutex on rateLimiter buckets map | S | Throughput uplift under concurrency |
| 8 | **P26-05-07** | Skip migration loop when nothing new | S | Multi-replica boot O(1) instead of O(N) |
| 9 | **P26-05-29** | Stash `onMessage` in a ref in `useWebSocket` | S | Stops WS reconnect churn |
| 10 | **P26-05-19** | Migrate `lib/pq` → `pgx/v5` (stdlib shim) | S-M | 20-50% DB throughput on JSONB paths |

---

## Quick wins (≤30 min each)

Single-edit fixes a human can cherry-pick on a slow afternoon:

- **P26-05-01** — Add `gin.SetMode(gin.ReleaseMode)` in `main.go` if `cfg.Env != "dev"`. 3 lines.
- **P26-05-04** — Hoist `MapCarrier` and the OTel inject above the level loop in `runner.go:73`. 5 lines.
- **P26-05-11** — Delete the status-update drain goroutine in `executor.go:266-270` (or repurpose it). 5 lines.
- **P26-05-12** — Swap `rl.mu` from `Mutex` to `RWMutex`; use `RLock` in the fast path of `limiterFor`. ~15 lines.
- **P26-05-21** — `scanner.Buffer(...)` on `kaniko.go:343`. 1 line.
- **P26-05-28** — `manualChunks` in `vite.config.ts`. ~6 lines.
- **P26-05-29** — `onMessageRef` pattern in `useWebSocket.ts`. ~6 lines.
- **P26-05-30** — Peek-first-byte gate around `JSON.parse` in `useWebSocket.ts`. ~3 lines.
- **P26-05-34** — Replace the for-loop in `ci.yml:64-79` with `go test -race -timeout 90s ./...`. Optional `gotestsum` for pretty output. ~5 lines.
- **P26-05-38** — Drop `needs: [backend, frontend, helm]` from the docker job. 1 line.

That's 10 fixes totalling ~50 lines.

---

## "Don't bother" list

Things that look like wins on paper but aren't worth the effort:

- **`bytes.TrimSpace` in `splitManifest`** (P26-05-23) — Deploy is called ~once per stage. Optimising this is profiler noise.
- **`splitManifest` fast-path** (P26-05-22) — Same reason as above. Maybe ~10 µs saved per Deploy; meaningless.
- **`strconv.Itoa` in MetricsMiddleware** (P26-05-20) — Only matters at >5k req/s, which Cooker won't see in its target deployment. Premature.
- **`pingWithBackoff` `time.NewTimer`** — Already addressed in `P0.3`.
- **`applyMigrations` embed read on every boot** (P26-05-08) — In-memory FS reads. Optimising this is theatre.
- **Hand-pooling `BroadcastMessage` struct** — The struct itself is a value type; Go's escape analysis already keeps it on the stack in the common case. Pool the `Data` slice (P26-05-02), not the struct.
- **OTel propagator overhead** when no tracer is configured — `otel.GetTextMapPropagator()` returns the no-op composite by default. Cost is bounded. Don't bypass.
- **`rate.Limiter.Allow()` lock contention** — The standard library implementation is already lock-free on the fast path. Don't replace it.
- **`embed.FS` for migrations** — The whole migration set is <50 KB embedded. Replacing it with disk reads buys nothing.
- **Removing `bytes.Buffer` for `cappedBuffer`** — The buffer's growth strategy is reasonable. The 1 MiB cap is what matters; the underlying type doesn't.
- **CI helm matrix split** (P26-05-37) — Wall-clock saved ≤20s; not on the critical path.
- **`go vet`/`golangci-lint`/`gofmt` parallelism in CI** — These are <30s each. Splitting would shave seconds; not worth the YAML churn.

---

## Cross-references

- `docs/audits/dag-performance.md` §1 (Kaniko cache), §2 (bounded fan-out), §3 (retry), §4 (log persistence).
- `docs/audits/spof-and-database.md` Part B (indexes, write amplification, pagination).
- `docs/audits/crash-and-service-quality.md` Part B.2 (audit-sink async, mwWriter mutex, wsLogSink alloc, decodeBroadcast alloc).
- `docs/audits/W10-bug-and-chain-recheck.md` W10-2, W10-4, W10-20.
- `backlog.md` — closed items (P0.5 binary WS framing, T16 audit sink async, W3 DAG fan-out cap) are referenced where they overlap.

---

## Closed findings

Findings moved here once the fix lands on `main`.

- **P26-05-01** — Gin ran in debug mode in production, printing a per-route banner and a verbose ANSI request logger that duplicated `observability.MetricsMiddleware`. Fixed in `claude/w2-backend-perf-and-f07`: `internal/server/server.go` now calls `gin.SetMode(gin.ReleaseMode)` when `cfg.Env != "dev"` and switches from `gin.Default()` to `gin.New()` + `gin.Recovery()` so Cooker controls exactly which middleware runs. Expected: ~5-10% CPU savings on hot HTTP paths; cleaner production logs.
- **P26-05-12** — `rateLimiter.limiterFor` acquired a `sync.Mutex` write-lock on every request, even when the bucket already existed. Fixed in `claude/w2-backend-perf-and-f07`: `internal/server/ratelimit.go` now uses `sync.RWMutex` for buckets (read-lock on the fast path; write-lock only on first registration) plus a separate `sync.Mutex` (`lastMu`) for `lastSeen` so bucket reads never contend with gc writes. Expected: ~90% reduction in lock contention at 50+ concurrent users on expensive endpoints.
- **P26-05-34** — Backend test loop serialised packages one at a time. Fixed in `claude/ci-critical-path-3min`: replaced `for pkg in $(go list ./...); do go test -race ...; done` with a single `go test -race -timeout 120s ./...` invocation so Go's native cross-package parallelism applies. Expected: backend job ~90s → ~20s.
- **P26-05-38** — Docker job had `needs: [backend, frontend, helm]`, serialising it after the full test suite. Fixed in `claude/ci-critical-path-3min`: dropped `needs:` so all four jobs run in parallel. The docker job has no genuine data dependency on test output. Expected: critical-path CI ~8 min → ~3-5 min (docker build and tests run concurrently).
- **P26-05-39** — Docker build had no buildx layer cache. Fixed in `claude/ci-critical-path-3min`: added `docker/setup-buildx-action@v3` + `docker/build-push-action@v6` with `cache-from: type=gha` and `cache-to: type=gha,mode=max`. Expected: docker job ~4 min → ~1 min on warm cache.
- **P26-05-29** — `useWebSocket` included `onMessage` in the `useCallback([url, onMessage])` dep array for `connect`. Every parent re-render that passed a fresh arrow function (the common pattern in `useStageLogs`) gave `connect` a new identity, which triggered the `useEffect([autoConnect, connect, disconnect])` — causing a spurious disconnect+reconnect. Fixed in `claude/w3-p26-05-29-onmessage-ref`: `onMessage` is now stashed in `onMessageRef` (a `useRef`) updated via a bare `useEffect()` after every render; `ws.onmessage` reads `onMessageRef.current` at call time; `onMessage` is dropped from `connect`'s deps. The WS lifecycle now only reconnects when `url` actually changes. Expected: eliminates reconnect churn under re-render pressure; stable WS lifecycle during pipeline log streaming.
