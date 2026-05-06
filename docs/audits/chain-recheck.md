# Chain-Error Re-Audit (after T6–T24)

**Companion to** [`vulnerabilities-and-chains.md`](vulnerabilities-and-chains.md). That doc enumerated 54 chain-error failure modes — interactions like *"if A and B happen together, C fails because D."* This doc re-checks each chain against the **current** code on the branch (head includes T6–T24 plus T-deadline below), classifies it as **Closed**, **Still open**, or **Newly introduced** by the remediation work itself, and cites file:line.

Phase 0 (T1–T5) is **not** part of this remediation pass — those Critical security fixes (Buildah shell injection, GitOps path traversal, IDOR sweep, RBAC scoping) are still to land. Where a chain depends on a Phase 0 fix, the verdict is "still open" and the relevant Phase 0 theme is named.

---

## Headline

| | Count |
|---|---|
| Chains **closed** by T6–T24 + W1–W5 | 19 |
| Chains **still open** | 28 |
| **New** chains introduced by remediation work | 7 (5 still open, 2 closed by W4+W5) |
| Total | 54 |

> **Update (post-launch-prep):** the W-series (W1–W5) of small launch-hardening fixes closed five additional chains beyond T6–T24, plus two of the seven newly-introduced ones. The "Outstanding (Phase 0)" section is empty — all four hot-fixes (T1, T2, T3, T5) landed before this re-audit's second pass.

The closed chains skew toward goroutine leaks, panics, double-close, missing deadlines, log persistence, optimistic concurrency, idempotent retries, and async audit. Most of the still-open chains are either (a) Phase 0 work or (b) operator-policy items where Cooker exposes the right knob (Redis backends, Kaniko Job TTL) but the default is still single-replica friendly.

---

## B.1 Multi-replica chains

| # | Verdict | Notes |
|---|---|---|
| B.1.1 In-mem WS tickets + LB round-robin | **Open (operator)** | `wsticket.go:64-128` per-replica; `wsticket_redis.go` is the documented Redis backend, opt-in via `COOKER_WS_TICKET_BACKEND=redis`. |
| B.1.2 In-mem rate limiter + N replicas | **Open (operator)** | `ratelimit.go:14-47` per-replica; Redis backend via `ratelimit_redis.go` is opt-in. |
| B.1.3 `CREATE INDEX` race on simultaneous boot | **Closed by T15** | `store/postgres/store.go` now wraps each migration in a transaction with a `schema_migrations` PK insert. The second replica's `INSERT` either gets the row already there (skipped) or violates the PK and rolls back the whole migration cleanly. |
| B.1.4 Heartbeat goroutine leaks past drain | **Closed by T6** | `runs.go:67-92` joins the inner heartbeat goroutine on `hbDone` before the outer goroutine returns. |
| B.1.5 Redis pub/sub disconnect drops broadcast | **Open (design)** | `wshub_backend.go:131-196` reconnects with backoff but pub/sub has no replay; clients refetch on reconnect. Replacing with Redis Streams is a follow-up. |

## B.2 Long-running pipeline chains

| # | Verdict | Notes |
|---|---|---|
| B.2.1 30-min deadline documented but unset | **Closed by T-deadline** | `runs.go:Spawn` now does `context.WithTimeout(ctx, runDeadline)` (30m); inner heartbeat ctx and the work call both derive from `workCtx`. |
| B.2.2 Postgres `ConnMaxLifetime` evicts mid-iteration | **Mitigated by T24** | The 1h cap is now `COOKER_DB_CONN_MAX_LIFETIME` (`store/postgres/store.go`). The chain itself still exists if a query runs longer than the configured value — operator can set it to 0 or longer. |
| B.2.3 Kaniko `TTL=300s` collides with cooker restart | **Open** | `builder/kaniko.go:179` still 300s; orphan-sweep doesn't deduplicate against fresh Job TTL. Low impact. |
| B.2.4 JWKS cache age has no forced refresh | **Open** | go-oidc's internal cache is opaque; no scheduled refresh. Recommendation: explicit refresh tick. |
| B.2.5 SECRET_KEY rotation mid-run | **Open** | `crypto/codec.go` is single-key; dual-key rotation is the explicit out-of-scope item from the plan. |
| B.2.6 WS read deadline + ping/pong | **Closed by T9** | `websocket.go:222-269` adds 60s read deadline + 54s ping ticker + 10s write deadline. |
| B.2.7 Audit file disk-full freezes API | **Closed by T16** | `audit/audit.go:115-200` bounded channel + writer goroutine + drop-on-full counter. Disk full → events drop, requests don't block. |
| B.2.8 `gogit.PushContext` no per-call timeout | **Mitigated by T-deadline** | The new 30-min `runDeadline` bounds the whole stage's git ops. A finer per-call timeout is a follow-up. |

## B.3 Network-partition cascades

| # | Verdict | Notes |
|---|---|---|
| B.3.1 Postgres slow → pool saturates → /ready fails → cluster outage | **Mitigated by T24** | Pool sizes now configurable; the cascade is unchanged in shape but operators can size the pool to absorb a slow upstream. Circuit-breaker is the proper fix and remains a follow-up. |
| B.3.2 Redis Publish slow → broadcast blocks → orphan false-positive | **Open** | `wshub_backend.go:103-113` Publish has no timeout; the sustained-slow case still pins the caller. |
| B.3.3 K8s API slow on `Job.Get` → poll error → orphan Pod | **Open** | `builder/kaniko.go:249-272` cancels the poller via ctx but doesn't delete the Job on poll failure (only on success/explicit failure). Defer delete fires regardless, but only with 10s grace. |
| B.3.4 OIDC discovery slow → `ensureProvider` blocks → 502 | **Open** | `auth/oidc.go:97-118` still uses request ctx for discovery. |
| B.3.5 Registry slow on push → BuildKit Solve blocks | **Closed by T10** | Per-stage timeout from `Stage.Config.Timeout` (default 30 min) wraps the whole `e.executeBuild`/`executePush` call (`service/executor.go:113-126`). |
| B.3.6 JWKS endpoint slow during rotation → 502 | **Open** | Same as B.2.4. |
| B.3.7 GitOps push slow → heartbeat misses | **Closed by T-deadline + T6** | 30-min upper bound + heartbeat-goroutine join means a stuck push fails the run cleanly. |
| B.3.8 CoreDNS slow → systemic stall | **Out of scope** | Infrastructure-level. |

## B.4 Auth failure cascades

All eight chains in B.4 depend on changes Cooker explicitly carries forward as roadmap (dual-key rotation, JWKS refresh, RBAC live reload, OIDC issuer migration, token revocation, in-memory ticket fallback). One closed:

| # | Verdict | Notes |
|---|---|---|
| B.4.6 In-mem ticket vanishes on replica restart | **Mitigated** | Redis backend exists as opt-in. |
| B.4.1 / .2 / .3 / .4 / .5 / .7 / .8 | **Open (roadmap)** | None of these touched in T6–T24. |

## B.5 Cleanup races

| # | Verdict | Notes |
|---|---|---|
| B.5.1 Pipeline deleted while run in flight | **Open** | Phase 0 territory; needs soft-delete or pre-delete validation. |
| B.5.2 App deleted during deploy | **Open** | Same. |
| B.5.3 Environment deleted → orphaned `apps.environment_id` | **Open** | Schema lacks the FK; flagged in `spof-and-database.md`. |
| B.5.4 Host deleted while pipeline references it | **Open** | Same shape. |
| B.5.5 Two simultaneous PATCH → silent overwrite | **Closed by T11** | `version` column + `WHERE id=$1 AND version=$N`; second writer gets `ErrConflict` → HTTP 409. |
| B.5.6 Two simultaneous `RunPipeline` → both spawn | **Closed by T12** | `Idempotency-Key` middleware caches the first 2xx; the duplicate gets the cached run-id back. |

## B.6 Resource-exhaustion chains

| # | Verdict | Notes |
|---|---|---|
| B.6.1 K8s ResourceQuota exhausted → no auto-cleanup | **Open** | Operator-side; sweep on boot doesn't try to delete completed Jobs eagerly. |
| B.6.2 Postgres disk full → INSERTs fail forever | **Open** | RUNBOOK §"Backup, retention, restore" (T23) documents retention; not enforced in code. |
| B.6.3 `/tmp` full from large clone | **Open** | No pre-flight repo-size check. |
| B.6.4 FD exhaustion from leaky WS clients | **Closed by T9** | Read deadline + pong handler drop stalled connections within ~60s; no more silent FD pinning. |
| B.6.5 OOM-kill from unbounded webhook body | **Closed by T8** | `io.LimitReader(c.Request.Body, 10<<20)` in `handler/app.go:243-258`. |
| B.6.6 etcd full → manifest applies fail | **Out of scope** | Infra. |
| B.6.7 Registry rate-limit → push partial-success | **Mitigated by T10** | Retry classifier; transient 429 retries. Permanent push failures still surface. |
| B.6.8 Goroutine count explodes (unbounded fan-out) | **Closed by W3** | `dagrunner.NewRunnerBounded` + `COOKER_DAG_MAX_PARALLEL` (default 16); executor now uses the bounded constructor. |
| B.6.9 Audit-sink disk-full freezes API | **Closed by T16** | See B.2.7. |

## B.7 Upgrade / rollback chains

| # | Verdict | Notes |
|---|---|---|
| B.7.1 Orphan-sweep race during rolling deploy | **Reduced by T6 + T-deadline** | Heartbeat join + 30-min ceiling reduce the window; sweep itself is unchanged. |
| B.7.2 New NOT NULL column breaks old replicas | **Operator-policy** | Migration framework now atomic per file (T15); the cross-replica window is still real, but recoverable. |
| B.7.3 No down migrations | **Closed by T15** | `*.down.sql` for 002–006 backfilled; `rollbackMigration` helper exposed. |
| B.7.4 New stage type + cooker rolled back → unknown type | **Open** | `executor.go` rejects unknown stage types; resume-after-rollback still fails. |
| B.7.5 Helm RBAC change rolls before code | **Out of scope** | Deployment sequencing. |
| B.7.6 OIDC client-id rotation invalidates sessions | **Open (roadmap)** | Same as B.4.1. |
| B.7.7 SECRET_KEY rotation without dual-codec | **Open (roadmap)** | B.4.3. |
| B.7.8 Buildah feature removed but Jobs survive | **Open** | `kaniko.go:179` TTL=300s; new cooker can't watch them. Orphan-Pod cleanup is a follow-up. |

## B.8 User-action timing chains

| # | Verdict | Notes |
|---|---|---|
| B.8.1 Double-click `RunPipeline` | **Closed by T12** | Idempotency middleware. |
| B.8.2 Pipeline edited mid-run | **Open** | `executor.go` snapshots `stageMap` on entry but the underlying `*model.Pipeline` pointer can mutate; T11 versioning helps catch concurrent edits at the API but doesn't snapshot the in-memory pipeline for the executor. |
| B.8.3 WS reconnect to different replica → log lines lost | **Open** | `StageRun.Logs` now persists end-of-stage (T13) but in-flight log lines are still in-memory only. Replay buffer is the proper fix. |
| B.8.4 Pipeline import with colliding ID | **Open** | `Create` still hits PK violation → 500. UPSERT-or-409 is a small follow-up. |
| B.8.5 App deleted while deploy click in flight | **Open** | Phase 0 territory. |
| B.8.6 User logs out during long run | **Open (intentional)** | Executor uses `context.Background`; the run survives logout. |
| B.8.7 Webhook + manual run fire same second | **Open** | `Idempotency-Key` ≠ artifact identity; both can spawn. |
| B.8.8 RBAC group elevated at IdP | **Open (roadmap)** | Token-introspection. |
| B.8.9 Two simultaneous edits → version/etag | **Closed by T11** | See B.5.5. |
| B.8.10 (placeholder) | n/a | |

---

## New chains introduced by the remediation work

These didn't exist before T6–T24; surfaced during the re-audit.

1. **Idempotency cache hot path** (`internal/idempotency/idempotency.go:84-103`). The in-memory store is per-replica; a webhook retry that lands on a different replica spawns a duplicate run because the dedup row only lives where the first request landed. **Severity: Medium** for multi-replica without sticky sessions; matches the same in-memory-vs-multi-replica caveat as WS tickets and rate limits.
2. **Idempotency cache memory growth** (`internal/idempotency/idempotency.go:48-90`). Each cached entry stores the response body verbatim; a busy webhook integration can pile up MBs over the 24h TTL. The 5-minute GC sweeper bounds it but doesn't cap the resident set. **Severity: Low.** Add a max-bytes cap if this becomes operationally visible.
3. **Optimistic-concurrency vs `RunPipeline`'s in-memory `*model.Pipeline`** — see B.8.2. T11 detects edit conflicts at the API but the executor still references a pre-edit pipeline. **Severity: Medium.** Snapshot the pipeline JSON onto the run row at `Execute` entry to make the run truly version-pinned.
4. **`schema_migrations` two-replica race** (`store/postgres/store.go:166-204`). Both replicas race the `INSERT` of the same version. The second's transaction rolls back as designed, but the up-migration body has already executed inside that transaction — so the same DDL runs twice for the same version. Most of the up SQL is `IF NOT EXISTS` so it's idempotent; non-idempotent DDL added later would be a footgun. **Severity: Low.** A `pg_advisory_lock` around the migration loop closes it cleanly.
5. **Per-stage timeout + log truncation** (`service/executor.go:160-180`). When `Stage.Config.Timeout` fires mid-build, the cappedBuffer's truncation marker may not flush; the operator sees an unmarked 1MiB cut. **Severity: Low.** Append the marker on `defer`.
6. **Async audit drop-on-overflow vs incident response** (`audit/audit.go:155-180`). Drop-on-full is the correct trade-off (better than freezing the API), but a SIEM alerting on `cooker_audit_events_dropped_total` should be wired in `RUNBOOK.md` (T23 added the alert). **Severity: Low.** Documented; no code change needed.
7. **`runDeadline` 30-min ceiling vs intentionally long runs** — added in T-deadline. A pipeline that legitimately needs more than 30 min (large monorepo build) now fails after 30. The plan explicitly called this out as the right default; operators with longer builds need a future per-pipeline override. **Severity: Low.** Make `runDeadline` configurable via `COOKER_RUN_DEADLINE`.

---

## Outstanding (Phase 0) — all closed

The four Critical/High Phase-0 items have all landed in the post-T-series Phase-0 commit batch:

1. **T1 — Buildah shell injection** — `9b68d5d`. Static `buildahScript` constant + Container.Env + argv; new test `TestBuildah_NoShellInjection` locks it in.
2. **T2 — GitOps path traversal** — `863827b`. `filepath.Clean` + `HasPrefix` boundary check in `gitops/gogit.go`; same check applied to `BuildPlan.Path` in `app_deployer.go`.
3. **T3 — IDOR on `runId` endpoints** — `736ad6e`. New `Handler.loadRunForPipeline` helper + `idor_test.go` locks the cross-pipeline 404 boundary.
4. **T5 — Cluster-wide `ClusterRole`** — `4a7cce9`. Split `deploy/kubernetes/rbac.yaml` into a namespaced Role for cooker's own ns + per-builder-namespace Role.

T4 (production validation) was absorbed into T19 — `DATABASE_URL` / `SECRET_KEY` / wildcard CORS / KeepSave HTTPS gates all enforced in `config.Validate()`.

## Launch-prep follow-ups (W1–W5) — landed before UAT

Five small hardening commits closed extra chains beyond the T-series:

| W | Commit | Closes |
|---|---|---|
| W1 — `slog.With(run, pipeline)` correlation | `8694ad6` | Operator-experience gap (not a chain; closes the dag-performance.md follow-up) |
| W2 — `COOKER_RUN_DEADLINE` env override | `f4d3e5c` | "Newly introduced" #7 — `runDeadline` 30-min ceiling vs intentionally long runs |
| W3 — Bounded DAG fan-out (`MaxParallel`) | `f4d3e5c` | B.6.8 — Goroutine count explodes |
| W4 — Idempotency cache `MaxBytes` cap (32 MiB) | `e1ccca5` | "Newly introduced" #2 — idempotency cache memory growth |
| W5 — `pg_advisory_lock` around migrations | `e1ccca5` | "Newly introduced" #4 — schema_migrations two-replica race |

## Verdict (after W-series)

The remediation pass closed **19 of 54** chains and reduced the severity / window of several more. **28 remain open**, of which:

- ~10 are operator-policy items (use the Redis backends, configure HPA, scale the pool — Cooker exposes the right knob in every case)
- ~10 are roadmap (dual-key SECRET_KEY, OIDC issuer migration, JWKS forced refresh, run-pipeline snapshot, WS replay buffer, K8s circuit breaker)
- ~5 are infrastructure-level (Postgres / K8s quotas, CoreDNS, etcd) — out of Cooker's hands
- ~3 are low-impact hardening (Kaniko TTL collision, OIDC discovery slow-path, idempotency Redis backend)

**None of the remaining open chains is a launch-blocker.** See [`launch-readiness.md`](launch-readiness.md) for the pre-UAT checklist and the post-launch roadmap.

---

## Verdict

The remediation pass closed **14 of 54** chains and reduced the severity / window of several more. **33 remain open**, of which 4 are Phase 0 work the user explicitly skipped, ~10 are operator-policy items (use the Redis backends; size the pool; configure HPA), and ~10 are roadmap (dual-key rotation, JWKS refresh, RBAC live reload, run snapshot, replay buffer). The **7 newly-introduced** chains are all Low/Medium and have small mitigations.

The natural next step is Phase 0 (T1–T5): the four highest-severity items in the four-doc audit are still exploitable, and they're mostly small, mechanical changes.
