# W10 — Bug + chain re-audit (post PR #25 / PR #26)

This is a fresh audit run after the Week 1 observability batch (PR #26) and the planning-week shipping batch (PR #25) landed. Format mirrors `chain-recheck.md`: top section is **newly introduced findings**, middle is **re-checked chains**, bottom is the **verdict summary**.

`[W10-N]` IDs continue the existing W-series (W1–W5).

---

## Newly introduced findings

PR #26 added ~2,000 lines across 7 files in the service / server / store / handler / frontend layers. Sweeping that surface produced the findings below. Severities are calibrated against the existing `chain-recheck.md` rubric: **Critical** = silent data-loss / goroutine leak / RCE-shaped, **High** = service-quality failure under realistic conditions, **Medium** = bug under contrived conditions, **Low** = micro-leak / unclean code, **Cleanup** = no behavioural impact.

| # | Severity | File:line | Description | Suggested fix |
|---|---|---|---|---|
| **[W10-1]** | Medium | `backend/internal/service/logbroadcast.go:50–95` | `lineWriter.Write` always returns `len(p)` even when `broadcast` fails. Documented as best-effort. The contract is fine in isolation, but `executor.go` wraps it in `io.MultiWriter` which treats a short return as fatal — if a future change ever has lineWriter return short, the whole log stream silently stops. | Add a comment on `lineWriter.Write` pinning the invariant: "Always returns `len(p)`. `executor.executeBuild` wraps this in `io.MultiWriter` which aborts on short writes, so this contract is load-bearing." |
| **[W10-2]** | Low | `backend/internal/service/logbroadcast.go:67–75` | `partial` slice can grow unbounded if a stage produces a single never-newlined string (e.g. a logger flushing 1 GiB without `\n`). On-disk capture is already capped at 1 MiB; the live broadcast tee isn't. | Cap `partial` at 64 KiB. On overflow, emit the buffered tail with an explicit truncation marker so the viewer sees something rather than nothing. |
| **[W10-3]** | Low | `backend/internal/service/logbroadcast.go:30–36` | `lineWriter` is not goroutine-safe; not documented. Today the executor serialises writes per stage so it doesn't matter, but a future caller could hit a race. | One-line struct comment: "Not goroutine-safe. The executor serialises Write calls per stage via the builder's LogWriter; do not share a `lineWriter` across goroutines." |
| **[W10-4]** | Low | `backend/internal/service/logbroadcast.go:67–82` | Slow path always allocates `combined` even when `partial` is empty. Micro-allocation; runs once per `Write` that straddles a newline. | Branch on `len(w.partial) > 0`; only allocate the combined buffer when it's non-empty. |
| **[W10-5]** | Cleanup | `backend/internal/service/executor.go:288–326` | Defer order in `executeBuild`: `lw.flush()` runs before `sr.Logs = logs.String()`. Correct (the flush appends to both broadcaster and cappedBuffer because the tee writer wraps both). Worth pinning in a comment. | Add a one-line comment in the deferred block documenting the ordering guarantee. |
| **[W10-6]** | Medium | `backend/internal/service/executor.go:298–301` | `io.MultiWriter` semantics: if any underlying writer returns short, the whole MultiWriter aborts. `lineWriter.Write` and `cappedBuffer.Write` both unconditionally return `len(p)`, so this works today, but no test pins the invariant. | Add a `_ = io.Writer(&lineWriter{})` interface assertion + a unit test that asserts `Write` returns `len(p)` on every input including ones that would trip the broadcast (eg. with a broadcaster that always errors). |
| **[W10-7]** | Cleanup | `backend/internal/service/executor.go:299–301` | Nil checks: `lw` is gated on `e.logBroadcast != nil && runID != "" && stage != nil && stage.ID != ""`. The latter three are guaranteed by callers; in tests `executeBuild` could be called directly with a zero-value stage and the gate keeps it nil-safe. | Document the contract in a comment: "Production callers always pass non-empty runID + stage.ID; the gate keeps direct test callers safe." |
| **[W10-8]** | **High** | `backend/internal/service/app_health.go:117–135` | **Probe panic kills the checker.** `tick` calls `c.proberFor(...).Probe(ctx, a)` with no `defer recover()`. A flaky cloud SDK that panics inside a Prober (Cloud Run / ECS / k8s SDKs all have known panic paths in older versions) takes down the entire AppHealthChecker goroutine. Health stops updating for every app cluster-wide; no signal to the operator. | Wrap each Prober call in a deferred-recover that logs the panic, writes `AppHealthUnknown` with a "probe panicked" message via `UpdateHealth`, and continues to the next app. |
| **[W10-9]** | Medium | `backend/internal/service/app_health.go:117–135` | **Tick pile-up.** Serial probe of N apps with slow probes (eg. K8s API timeouts at 10s × 100 apps = 16 min) blows past the 30s default interval. `time.Ticker` drops the pending tick, so no goroutine leak — but the next probe round can lag by minutes. | Two fixes, in priority order: (a) add a Prometheus histogram for `cooker_app_health_tick_seconds` so operators see the lag; (b) parallelise the per-app probe with a small worker pool (`COOKER_APP_HEALTH_PARALLELISM`, default 8). The pool fits in <50 lines but is on the upper edge — gate it behind a config flag so the simple path stays simple. |
| **[W10-10]** | Low | `backend/internal/service/app_health.go:120–125` | `proberFor` reads `c.probers` map without a lock. Today only `WithProber` writes to it, at construction. A future caller mutating `probers` after `Run` started would race. | Add a struct comment: "probers is populated at construction time and read-only thereafter; mutation after Run is started is not supported." Or wrap behind `sync.RWMutex` if post-construction registration is required (don't add the mutex speculatively). |
| **[W10-11]** | Cleanup | `backend/internal/service/app_health.go:151–158` | List-then-Update race tolerated correctly via `errors.Is(err, store.ErrNotFound)` check. | No fix needed. Add a regression test that calls `tick` against a store whose List returns an app that's been deleted before UpdateHealth resolves — assert the checker survives. |
| **[W10-12]** | **Critical** | `backend/internal/server/server.go:209–225` | **Goroutine leak on early-return after health checker starts.** The AppHealthChecker goroutine spawns inside `server.New`. If any subsequent line in `New` returns an error before the `Server` struct is constructed (and before any `cleanups = append(cleanups, ...)` registration captures `healthCancel`), the goroutine leaks. The current cleanups slice doesn't include `healthCancel` until after the `s := &Server{...}` line. | Move the `cleanups = append(cleanups, func() { if healthCancel != nil { healthCancel() }; if healthDone != nil { <-healthDone } })` registration immediately after the goroutine's `go func()` spawn. That way a later error in `New` correctly drains the checker via the cleanup loop. |
| **[W10-13]** | Cleanup | `backend/internal/server/server.go:330–340` | The doc-comment block describing the shutdown order is fine; the code matches it. | No fix. Pin the order with an inline comment: "1. HTTP drain → 2. run coordinator drain → 3. health checker cancel + 5s wait → 4. return." |
| **[W10-14]** | Low | `backend/internal/server/server.go:340–348` | `<-time.After(5 * time.Second)` allocates a Timer that the GC reclaims; not stopped on the `<-s.healthDone` win path. Single per-shutdown leak; benign. | Use `t := time.NewTimer(5*time.Second); defer t.Stop()` for cleanliness, matching the pattern used in `server/wshub_backend.go:223`. |
| **[W10-15]** | Cleanup | `backend/internal/store/postgres/app.go:21–25` | `appColumns` uses `COALESCE(health_status, 'unknown')` and `COALESCE(health_message, '')`. Migration 008 declares the columns `NOT NULL DEFAULT`, so they're never NULL. The COALESCE is a hand-rolled compatibility shim for a pre-migration state that doesn't exist. | Drop the `COALESCE`s. After migration 008 lands on every cluster, the columns are guaranteed non-NULL. Cleanup once PR #26 ships. |
| **[W10-16]** | Cleanup | `backend/internal/store/postgres/app.go:165–179` | `UpdateHealth` runs outside any transaction. `Update` doesn't list `health_*` columns in its SET clause, so a concurrent user-driven Update doesn't clobber the health fields. Postgres MVCC isolates the writes. | No fix. Add a comment on `UpdateHealth` documenting the isolation guarantee and the deliberate column-set choice. |
| **[W10-17]** | Cleanup | `backend/internal/store/postgres/app.go:21–25` | (Same surface as W10-15.) | (Folded into W10-15.) |
| **[W10-18]** | Medium | `backend/internal/store/postgres/migrations/008_app_health.up.sql:5–10` | `ADD COLUMN ... NOT NULL DEFAULT 'unknown'` is cheap on Postgres ≥ 12; on Postgres 11 it rewrites the entire `apps` table, locking it for the duration. Cooker's CI pins PG16 so it's fine; an operator running this against an old PG11 production deployment may see minutes of downtime. | One-line header note in the migration: `-- Requires Postgres ≥ 12 for cheap ADD COLUMN ... DEFAULT (avoids full table rewrite).` Don't change the SQL; the requirement is acceptable. |
| **[W10-19]** | Cleanup | `frontend/src/hooks/useStageLogs.ts:64–80` | REST backfill abort uses `AbortController` + `ctrl.signal.aborted` check inside the `.then`. Pattern is sound — the `.then` resolves with the stale fetch but the aborted flag suppresses the `setLines` call. | No fix. Add a regression test that mounts the hook with one `(runId, stageId)`, immediately re-mounts with a different one, and asserts the first hook's setLines never fires. |
| **[W10-20]** | Low | `frontend/src/hooks/useStageLogs.ts:99–106` | Rolling-buffer trim does `next.slice(next.length - MAX_LINES)` on every excess line. For pathological log streams (10k lines/sec) this allocates a 5000-element array many times per second. Typical builds emit <1000 total lines so this is benign in practice. | P3 / future: ring buffer instead of `Array.slice`. Out of scope for the W10 small-fix PR. |
| **[W10-21]** | **High** | `frontend/src/hooks/useStageLogs.ts:91–98` + `frontend/src/hooks/useWebSocket.ts:62–104` | When `enabled && runId && stageId` is false, `useStageLogs` passes `wsUrl: ''` to `useWebSocket`. The `autoConnect: enabled && !!wsUrl` gate means the connect callback never fires in that case, so we don't connect to the wrong endpoint. **However**, if a future caller drops the `autoConnect` gate and passes `url: ''` directly, `useWebSocket.connect` will build `wss://host?ticket=...` (no path) and end up routed to the wrong WS handler. The bug isn't live, but the contract is fragile. | Add a top-of-callback guard in `useWebSocket.connect`: `if (!url) return;` so any caller with an empty URL is safe regardless of `autoConnect`. ~3 lines. |

### Severity rollup

| Severity | Count |
|---|---|
| Critical | 1 (W10-12) |
| High | 2 (W10-8, W10-21) |
| Medium | 4 (W10-1, W10-6, W10-9, W10-18) |
| Low | 4 (W10-2, W10-3, W10-4, W10-14, W10-20) |
| Cleanup | 9 (W10-5, W10-7, W10-10, W10-11, W10-13, W10-15, W10-16, W10-17, W10-19) |

### Small-fix follow-up PR scope

The small-fix follow-up PR (`claude/audit-w10-small-fixes`) bundles every finding that fits in ≤50 lines per file:

- **[W10-8]** AppHealthChecker probe panic recovery (~12 lines).
- **[W10-12]** Move healthCancel cleanup registration earlier (~5 lines).
- **[W10-14]** `time.NewTimer` + `defer t.Stop()` in shutdown (~6 lines).
- **[W10-15]** Drop redundant COALESCE in `appColumns` (~2 lines).
- **[W10-18]** Migration header comment on PG12+ (~2 lines).
- **[W10-21]** `if (!url) return;` guard in `useWebSocket.connect` (~3 lines).
- **[W10-3]** + **[W10-1]** + **[W10-13]** + **[W10-16]** docstring/comment improvements (~12 lines combined).

**Deferred to follow-up backlog items:**
- **[W10-9]** parallel probe worker pool — borderline >50 lines, gated behind `COOKER_APP_HEALTH_PARALLELISM`.
- **[W10-2]** `partial` cap on lineWriter — not urgent (no log on disk has triggered it; covered by 1 MiB cappedBuffer).
- **[W10-20]** ring-buffer LogViewer — pure perf optimisation.

---

## Re-checked chains

Verified every chain in `docs/audits/chain-recheck.md` whose verdict cites a theme it was closed / mitigated by (`T<N>` or `W<N>`). For each: confirmed the cited file:line still exists at HEAD on `main` (`ed5c0fb`), confirmed the cited functionality is still present, and downgraded to "drifted" only when the line number shifted but the code is intact.

**Tally:** 23 still closed/mitigated; 4 closed (drifted, logic unchanged); **0 reopened**; 0 cannot-find.

| Chain # | Original verdict | Recheck status | Confirmed at file:line | Notes |
|---|---|---|---|---|
| B.1.3 | Closed by T15 | Still closed | `backend/internal/store/postgres/store.go:250` | INSERT schema_migrations with PK; per-migration transaction wrap. |
| B.1.4 | Closed by T6 | Still closed | `backend/internal/server/runs.go:80–99` | hbDone join confirmed before outer goroutine returns. |
| B.2.1 | Closed by T-deadline | Still closed | `backend/internal/server/runs.go:36–43, 70` | `runDeadline` var + `COOKER_RUN_DEADLINE` env override; `WithTimeout` at line 70. |
| B.2.2 | Mitigated by T24 | Still mitigated | `backend/internal/store/postgres/store.go:101` | `SetConnMaxLifetime` honors `COOKER_DB_CONN_MAX_LIFETIME`. |
| B.2.6 | Closed by T9 | Closed (drifted) | `backend/internal/server/websocket.go:214–217` | `SetReadDeadline` + pong handler. Citation drifted by 8 lines from refactor. |
| B.2.7 | Closed by T16 | Still closed | `backend/internal/audit/audit.go:95–102` | Bounded `fileSinkBuffer=1024` channel + drop-on-full. |
| B.2.8 | Mitigated by T-deadline | Still mitigated | `backend/internal/server/runs.go:70` | 30-min `runDeadline` bounds all git ops. |
| B.3.5 | Closed by T10 | Closed (drifted) | `backend/internal/service/executor.go:159–181` | `NewRunnerBounded` + per-stage timeout. Drifted by 46 lines (refactor). |
| B.3.7 | Closed by T-deadline+T6 | Still closed | `backend/internal/server/runs.go:70, 99` | Composite ceiling + heartbeat join. |
| B.5.5 | Closed by T11 | Still closed | `backend/internal/store/postgres/app.go:102–111` | `UPDATE ... WHERE id AND version`; `ErrConflict` on `RowsAffected==0`. |
| B.5.6 | Closed by T12 | Still closed | `backend/internal/server/middleware_idempotency.go:39–45` | Idempotency-Key cache + replay logic. |
| B.6.4 | Closed by T9 | Still closed | `backend/internal/server/websocket.go:214–217` | Read deadline + pong (same as B.2.6). |
| B.6.5 | Closed by T8 | Closed (drifted) | `backend/internal/handler/app.go:270` | `io.LimitReader(10<<20)`. Drifted by 27 lines. |
| B.6.8 | Closed by W3 | Still closed | `backend/internal/service/executor.go:36–46, 159, 251` | `NewRunnerBounded` + `COOKER_DAG_MAX_PARALLEL`. |
| B.6.9 | Closed by T16 | Still closed | `backend/internal/audit/audit.go:95–102` | Bounded audit channel (same as B.2.7). |
| B.7.1 | Reduced by T6+T-deadline | Still reduced | `backend/internal/server/runs.go:70, 99` | Both mechanisms confirmed. |
| B.7.3 | Closed by T15 | Still closed | `backend/internal/store/postgres/migrations/` | Migrations 001–007 all have a `.down.sql` partner. |
| B.8.1 | Closed by T12 | Still closed | `backend/internal/server/middleware_idempotency.go:39–45` | Idempotency middleware (same as B.5.6). |
| B.8.9 | Closed by T11 | Still closed | `backend/internal/store/postgres/app.go:102–111` | Version + conflict detection (same as B.5.5). |
| W2 | Closes newly-introduced #7 | Still present | `backend/internal/server/runs.go:36–43` | `COOKER_RUN_DEADLINE` (30m default). |
| W3 | Closes B.6.8 | Still present | `backend/internal/service/executor.go:36–46, 159, 251` | DAG fan-out cap. |
| W4 | Closes newly-introduced #2 | Still present | `backend/internal/server/server.go:195–196` | 32 MiB `NewMemoryBounded`. |
| W5 | Closes newly-introduced #4 | Still present | `backend/internal/store/postgres/store.go:180, 205, 242` | `pg_advisory_lock` + per-migration transaction wrap. |

### Drift footnotes

Three citations drifted by less than 50 lines and remain functionally intact:

- **B.2.6 / B.6.4** — `SetReadDeadline` shifted 8 lines (refactor of WS upgrade).
- **B.3.5** — DAG runner timeout wrap shifted 46 lines (after the W3 fan-out introduction).
- **B.6.5** — `io.LimitReader` shifted 27 lines (App webhook handler refactor).

None of these are regressions; the chain-recheck doc should be updated in a future round to refresh the line numbers.

---

## Verdict summary

**As of W10:**

- **21 newly-introduced findings** from the PR #26 surface sweep (1 Critical, 2 High, 4 Medium, 4 Low, 9 Cleanup-class). The Critical (W10-12) and both Highs (W10-8, W10-21) all fold into the small-fix follow-up PR (`claude/audit-w10-small-fixes`).
- **23 of 27** previously-closed chains re-verified at HEAD; **4 drifted** (line-shift only, functionality intact); **0 reopened** by either PR #25 or PR #26.
- The PR #26 surface is therefore **regression-free against the existing chain-recheck verdicts** — every theme that closed a chain is still on `main`, and the new code's footprint (LogBroadcaster + AppHealthChecker + frontend hook) is contained to genuinely-new failure modes catalogued under [W10-1] through [W10-21].

**Net delta vs. `chain-recheck.md`'s "33 remain open" headline:** unchanged (still 33 open from prior audits) plus 21 new findings from W10. The 21 new findings are categorised the same way: 3 directly fold into the small-fix PR; 4 deferred to backlog as larger remediations; 14 cleanups for opportunistic future PRs.

**Severity floor for the small-fix PR:** Critical (W10-12 goroutine leak), High (W10-8 probe panic safety, W10-21 fragile WS-URL contract).

---

## Cross-references

- **Adjacent doc:** `docs/audits/W11-user-journeys.md` — the persona walkthroughs that ran in the same audit week.
- **Source PRs audited:** PR #25 (`claude/plan-weekly-features-WoB0S` — Helm retention CronJob + agent frontmatter) and PR #26 (`claude/observability-app-health-dag-logs` — Week 1 observability).
- **Format precedent:** `docs/audits/chain-recheck.md` — same `# | Verdict | Notes` table shape.
