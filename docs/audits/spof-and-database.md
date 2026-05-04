# SPOF & Database Audit

**Companion to:** [`dag-performance.md`](dag-performance.md), [`crash-and-service-quality.md`](crash-and-service-quality.md), and [`vulnerabilities-and-chains.md`](vulnerabilities-and-chains.md). Together those cover the DAG executor, crash bugs and service-quality gaps in the rest of the codebase, and a 20-agent vulnerability-and-chain audit. This one zooms out to the rest of the stack:

- **Part A — SPOFs and crash blast radius.** What happens when a single component dies?
- **Part B — Database schema, indexing, migrations, rollback.** What does the schema cost at scale, and how do we get out of a bad migration?

**Method:** Static reading of `backend/internal/server/{server,health,wshub_backend,ratelimit*,wsticket*,runs}.go`, `backend/internal/store/postgres/`, `backend/internal/crypto/`, and the migrations directory. Every claim cites file and line.

---

## Part A — SPOFs and blast radius

### A.1 Redis: three consumers, three failure modes

Redis is created lazily, only if at least one of `COOKER_WSHUB_BACKEND`, `COOKER_WS_TICKET_BACKEND`, or `COOKER_RATE_LIMIT_BACKEND` is set to `redis` (`server.go:95-103`). The three consumers degrade differently.

| Consumer | File | On Redis outage |
|---|---|---|
| WebSocket hub | `wshub_backend.go:81-101, 131-169` | **Startup-gated** (the boot-time `Receive` test on line 87 fails the whole process if Redis is unreachable). At runtime, `consume()` reconnects with jittered backoff (500ms → 30s, line 152). Messages published during the disconnect window are **dropped** (Redis pub/sub has no replay; line 73). Local channel-full also drops silently (line 192). |
| Rate limiter | `ratelimit_redis.go:31-60` | **Fail-open** (line 42-44, comment "*Fail open: a redis blip should not lock users out.*"). Redis outage = rate limiting silently disabled until Redis recovers. |
| WS ticket store | `wsticket_redis.go:27-43` | **Fail-closed** (line 39-41 returns the Redis error; `Issue` callers cannot get a ticket; the WS upgrade returns 401 "missing ticket" via `wsticket.go:28-32`). No fallback to in-memory. |

The asymmetry is interesting: a Redis blip lets requests through (rate limiting is best-effort) but blocks new WebSocket connections (tickets must succeed). Either choice is defensible; the audit's point is that they should be a deliberate decision, not an accident — and they aren't documented anywhere as a pair.

### A.2 PostgreSQL: hard SPOF at boot, hard SPOF at runtime

`store/postgres/store.go:39-68` is the boot path:

- **Boot:** `pingWithBackoff` (line 73-111) retries with jittered exponential backoff up to a 5-minute budget (`pingBudget`, line 27). On exhaustion the process exits non-zero and Kubernetes restarts the pod; readiness probes ride out per-attempt failures.
- **Runtime:** every store call goes through the single `*sql.DB`. There's no retry-on-execute and no read-replica routing — a transient TCP reset surfaces as a 500 to the API caller.
- **Connection pool:** `MaxOpenConns=25`, `MaxIdleConns=5`, `ConnMaxLifetime=1h` (`store.go:44-46`). At three replicas that's ~75 connections to Postgres. Adequate for current load; will need pgbouncer or a higher cap once concurrent runs cross ~50.
- **Migration runner only embeds `*.up.sql`** (`store.go:32`: `//go:embed migrations/*.up.sql`). Down migrations are not in the binary, so even if you wrote them, there's no in-process way to apply them. Rollback is unavoidably out-of-band.

**Blast radius:** any Postgres outage takes the entire API down. Pipelines, apps, environments, runs, users — everything is gated on it. `/health/ready` (`health.go:29-98`) probes Postgres with a 1s timeout (line 15) and returns 503, which correctly drains the load balancer.

### A.3 Per-replica in-memory state (lost on restart, multiplied across replicas)

| State | File | Restart cost | Multi-replica cost |
|---|---|---|---|
| RunCoordinator goroutines | `runs.go:30-98` | All in-flight runs lose their heartbeat goroutine. Boot-time orphan sweep (`server.go:173`, threshold 90s — `runs.go:21`) marks them `failed`. **Users must re-run.** | Each replica tracks its own runs; no concern. |
| Rate-limit buckets (in-memory mode) | `ratelimit.go:19-47` | Burst budget resets — brief window of unenforced traffic. | **Per-replica buckets** (`ratelimit.go:14-18`); a 60/min limit becomes 60×N/min across N replicas. Use the Redis backend for multi-replica. |
| WS ticket store (in-memory mode) | `wsticket.go:64-128` | Issued tickets vanish; clients refetch (`Issue` is cheap). | **Tickets issued by replica A are unknown to replica B**; without sticky sessions, the upgrade can land on a different replica than the one that issued the ticket → 401. Use the Redis backend for multi-replica (the file's own comment on lines 56-58 says so). |
| WebSocket client map | `websocket.go` (per agent read) | All clients drop and reconnect. | Each replica maintains its own; cross-replica fan-out is via the hub backend (Redis pub/sub, see A.1). |

### A.4 External dependencies (no retry, no circuit breaker)

These all fail loudly the moment the dependency is unreachable:

- **Kubernetes API** — used by Kaniko, Buildah, and the clientgo deployer. Lazy client init in `NewKaniko` (`builder/kaniko.go:76-105`). No retry — already flagged in `dag-performance.md` §3.
- **BuildKit gRPC** — `builder/buildkit.go:42-46` dials on every `Build`; failure returns `ErrUnavailable`.
- **OIDC provider** — gated at startup (`server.go:54`). JWKS is cached, age tracked by `health.go:80-88`. Provider down = new logins fail; existing tokens validate against the cached JWKS until the cache is rotated.
- **Image registries** — push/pull errors propagate immediately; no per-stage retry.

A circuit breaker at the adapter layer (one per Builder/Pusher/Deployer endpoint) would prevent a downed dependency from spiking error rates and burning rate-limit budget.

### A.5 Panic blast radius

- **Stage goroutine has no `recover()`** (`runner.go:66-78`). Already flagged in `dag-performance.md` §3. Through the SPOF lens: a panic in one stage crashes the whole process (Go's default behaviour), which orphan-sweeps every other in-flight run on next boot. **One bad stage script = all in-flight pipelines fail.**
- **Background goroutines:** `rateLimiter.gc()` (`ratelimit.go:61-75`) has no shutdown hook — abandoned on replica termination. Negligible. `redisHubBackend.consume()` is closed via `Close()` (`wshub_backend.go:117-122`).

### A.6 Health endpoints — correct

`/health/ready` (`health.go:29-98`) probes Postgres + Redis (if configured) + JWKS age in parallel via `errgroup` (lines 42-61), with a shared 1s timeout (line 15). Any failure → 503 with a per-check breakdown so operators can see which dep tripped. **No issues here.**

`/health/live` (`health.go:19-23`) just answers 200 — correct, the process being able to serve at all is the answer.

### A.7 Configuration SPOFs

All config reads happen once at `config.Load()` (called from `cmd/cooker/main.go`). Notable consequences:

- **`COOKER_SECRET_KEY` rotation:** the codec is built once (`server.go:81-85`) and never refreshed. Rotating the key requires a rolling restart with overlapping windows; without dual-key support, secrets sealed under the old key become un-openable the moment the new key takes over. See remediation §A.7 below.
- **Rate-limit thresholds, Redis URL, OIDC issuer:** all startup-only. Restart required to change.

### A.8 SPOF summary matrix

| Component | SPOF? | Failure mode | Blast radius | Graceful degradation |
|---|---|---|---|---|
| Postgres | **Yes (boot + runtime)** | Boot exits non-zero; runtime returns 500s | Whole API | None (503 on `/ready`) |
| Redis (WS hub) | Yes at boot if `COOKER_WSHUB_BACKEND=redis` | Boot fails | All cross-replica broadcasts | Auto-reconnect at runtime |
| Redis (rate limit) | No | Limiter disabled | None — fail-open | Yes (silent passthrough) |
| Redis (WS tickets) | Yes if `redis` backend | Upgrades return 401 | New WS connections fail | None — no in-memory fallback |
| RunCoordinator goroutines | Per-replica | Restart orphans runs | In-flight runs marked `failed` | Orphan sweep on next boot |
| In-mem rate buckets | Per-replica | Multi-replica multiplies budget | Abuse window | Use Redis backend |
| In-mem WS tickets | Per-replica | Cross-replica 401s | New WS upgrades on wrong replica | Use Redis backend or sticky sessions |
| K8s API / BuildKit / Registry | Per call | Stage fails immediately | One stage | None (no retry, no circuit breaker) |
| OIDC provider | Boot + cache | Boot fails; new logins fail | Auth | JWKS cache survives short outages |
| `COOKER_SECRET_KEY` | Startup-only | No hot reload | New tokens unsigned | Rolling restart with overlap |

---

## Part B — Database schema, indexing, migrations

### B.1 Schema overview

| Table | Migration | PK | Notable columns |
|---|---|---|---|
| `pipelines` | `001_initial.up.sql:4-13` | `id TEXT` | `stages/edges/variables JSONB`, `created_at/updated_at TIMESTAMPTZ` |
| `pipeline_runs` | `001_initial.up.sql:15-26` | `id TEXT` | `pipeline_id TEXT NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE` (line 17), `stage_runs/env_statuses/variables JSONB`, `heartbeat_at TIMESTAMPTZ` (added in 006) |
| `environments` | `001_initial.up.sql:28-36` | `id TEXT` | `name TEXT NOT NULL UNIQUE`, `sort_order INT`, `secrets JSONB` (added in 002) |
| `apps` | `003_apps.up.sql` | `id TEXT` | `name UNIQUE`, `github_repo`, `branch`, `webhook_secret TEXT`, `auto_deploy BOOLEAN` |
| `hosts` | `004_hosts.up.sql` | `id TEXT` | `name UNIQUE`, `kind`, `reachability` |
| `users` | `005_users.up.sql` | `id TEXT` | `email UNIQUE`, `password_hash` (bcrypt), `role TEXT DEFAULT 'viewer'` |

All PKs are `TEXT` (random IDs generated in app code). All tables have `created_at` / `updated_at` defaulting to `NOW()`.

### B.2 Indexes — what's there, what's missing

**Existing:**
- `idx_pipeline_runs_pipeline_id` (`001_initial.up.sql:39`) — supports FK lookups.
- `idx_pipeline_runs_status` (`001_initial.up.sql:40`) — supports `WHERE status='running'`.
- `idx_environments_order` (`001_initial.up.sql:41`) — supports `ORDER BY sort_order`.
- `idx_apps_github_repo` (`003_apps.up.sql:23`) on `(github_repo, branch)` — webhook dispatch.
- `idx_users_email` (`005_users.up.sql:19`).
- `idx_pipeline_runs_running_heartbeat` (`006_run_heartbeat.up.sql:9-11`) — partial index on `(heartbeat_at)` `WHERE status='running'`. Sized to exactly the orphan-sweep query (`store/postgres/run.go:135-149`).

**Missing — every "list" query in the UI is unindexed:**

| Query | File:line | What's wrong |
|---|---|---|
| `pipeline_runs ORDER BY created_at DESC` | `store/postgres/run.go:29` | No index on `created_at`. Full-table scan + sort. With heartbeat updates churning the table at 1 row / 30s / running run, this gets expensive once history accumulates. |
| `pipelines ORDER BY updated_at DESC` | `store/postgres/pipeline.go:26` | No index on `updated_at`. |
| `apps ORDER BY updated_at DESC` | `store/postgres/app.go:28` | Same. |
| `users` lookup by `LOWER(email) = LOWER($1)` | `store/postgres/user.go:24` | `idx_users_email` is on the raw column → can't be used. Either add a functional index (`CREATE INDEX ... ON users (LOWER(email))`) or normalise on insert. |

**JSONB columns are never queried by content** (no `@>`, `?`, `->` in store code) — no GIN index needed. Correctly omitted.

**Composite uniqueness gap:** `apps(github_repo, branch)` is *indexed* but not declared `UNIQUE` (`003_apps.up.sql:23`). Webhook dispatch (`store.Apps.GetByRepo`) can in principle return ambiguous matches.

### B.3 Primary keys, foreign keys, cascade

- **All PKs are `TEXT`** with random values. Random TEXT PKs cause B-tree page splits; acceptable for current scale, but UUIDv7 / ULID would localise inserts and reduce write amplification.
- **Only one foreign key exists:** `pipeline_runs.pipeline_id → pipelines.id ON DELETE CASCADE` (`001_initial.up.sql:17`). Correctly prevents orphan runs.
- **Missing FK:** `apps` references an environment ID but has no FK to `environments.id`. Deleting an environment leaves orphaned apps. Severity: medium — visible only at admin time.

### B.4 Secrets at rest — encrypted (the sub-agent that helped me audit got this wrong)

**Important correction.** Environment and webhook secrets *are* encrypted at rest using AES-GCM via `crypto/codec.go:57-66`:

```go
func (c *Codec) Seal(plaintext []byte) ([]byte, error) {
    nonce := make([]byte, c.aead.NonceSize())
    io.ReadFull(rand.Reader, nonce)
    return c.aead.Seal(nonce, nonce, plaintext, nil), nil
}
```

The migration's own comment at `002_env_secrets.up.sql:1-4` confirms: "*base64-encoded AES-GCM sealed value (nonce ++ ciphertext ++ tag). Plaintext is never persisted; decryption happens only in the process memory of a Cooker instance that holds the COOKER_SECRET_KEY.*" The base64 wrapper is only there so the sealed bytes survive JSONB round-tripping.

**The real risks are operational, not cryptographic:**
1. Key rotation has no built-in dual-key support (see A.7). Rotating breaks every existing sealed value the moment the new key takes over.
2. `Codec.Active() == false` (no key set, `crypto/codec.go:30-33`) doesn't refuse to *handle* secrets — it returns `ErrNoKey` from `Seal/Open`, which the handler must handle. Worth a follow-up audit on whether all callers do.

### B.5 Migration plan and rollback

**Runner:** `applyMigrations` (`store/postgres/store.go:113-137`) reads every embedded `*.up.sql`, sorts by filename, and runs them in `db.ExecContext`. Statements use `IF NOT EXISTS`, so re-running on every boot is intentional and idempotent.

**Three structural problems:**

1. **No `schema_migrations` version table.** Postgres has no record of which migrations ran. The whole approach is "run them all every boot, idempotency carries the day." This works until:
   - A migration fails halfway through a multi-statement file. The next boot retries and the partial state can drift from the intended schema.
   - The file count grows (50+ migrations parsed and run on every boot is slow).
   - Two replicas boot simultaneously and race on a `CREATE INDEX CONCURRENTLY` (when those land).
2. **Only `001_initial.down.sql` exists.** Migrations 002–006 are forward-only. Rollback for any of them requires hand-written SQL coordinated with a code rollback. The down file directory listing:
   ```
   001_initial.down.sql      ← the only one
   001_initial.up.sql
   002_env_secrets.up.sql
   003_apps.up.sql
   004_hosts.up.sql
   005_users.up.sql
   006_run_heartbeat.up.sql
   ```
3. **Down files aren't even embedded.** `//go:embed migrations/*.up.sql` (`store.go:32`) only ships up files into the binary. So even `001_initial.down.sql` is decorative — there's no in-process path to apply it.

**Recommendation:** adopt `golang-migrate/migrate` (it supports embed.FS and tracks versions in `schema_migrations`) **or** write ~30 lines of version-table runner alongside the existing one. Either way, ship `.down.sql` for 002–006 and embed both directions.

### B.6 Performance smells

- **Write amplification on stage transitions.** `RunStore.Update` (`store/postgres/run.go:85-113`) re-marshals all three JSONB columns (`stage_runs`, `env_statuses`, `variables`) on every call, regardless of which one changed. A 10-stage pipeline with stage transitions every few seconds re-writes the whole row each time. Mitigation: a partial-update method (only re-marshal what changed) or move to an append-only event log with a denormalised projection.
- **Heartbeat hot row.** `UpdateHeartbeat` (`store/postgres/run.go:115-129`) fires every 30s per running run. The partial index `idx_pipeline_runs_running_heartbeat` is sized for this — it only contains running rows — so the index update cost stays small. But the row itself is the same row being touched repeatedly; under heavy concurrency, watch for HOT-update bloat and tune `autovacuum`.
- **No `LIMIT` on any list query.** `RunStore.List`, `PipelineStore.List`, `AppStore.List` all return everything. Once `pipeline_runs` crosses ~10k rows, the dashboard starts pulling MB-sized JSON responses. Add pagination at the API layer.
- **List queries hold the cursor open during scan iteration.** Standard `database/sql` behaviour, not a bug, but worth noting if a slow consumer ever holds the connection.

### B.7 Backup and retention

- **No documented Postgres backup procedure.** `RUNBOOK.md` mentions "plan a Postgres-side rotation policy for `pipeline_runs` if [space runs out]" but there's no procedure.
- **No automated retention.** `pipeline_runs` grows without bound; the orphan sweep marks rows `failed` but doesn't delete anything.
- **No restore drill.** Even if backups exist via the operator's external mechanism (pg_basebackup, WAL-G), nothing in the repo exercises the restore path.

Recommendation: add a `RUNBOOK.md` section covering backup cadence + retention policy + a quarterly restore drill, and ship a `pipeline_runs` rotation cron (delete-or-archive after N days).

---

## Severity table

| # | Issue | Severity | File:line | Mitigation |
|---|---|---|---|---|
| 1 | No down migrations for 002–006; migration runner doesn't embed `.down.sql` at all | **High** | `store/postgres/store.go:32`, `migrations/` | Add `*.down.sql`, change embed to `migrations/*.sql`, write down migrations |
| 2 | No `schema_migrations` version table — partial-failure replay is silent | **High** | `store/postgres/store.go:113-137` | Adopt `golang-migrate` or add version tracking |
| 3 | `pipeline_runs ORDER BY created_at DESC` unindexed | **High** | `store/postgres/run.go:29` | `CREATE INDEX idx_pipeline_runs_created_at_desc ON pipeline_runs (created_at DESC)` |
| 4 | `pipelines ORDER BY updated_at DESC` unindexed | **High** | `store/postgres/pipeline.go:26` | Add equivalent index |
| 5 | WS-ticket Redis backend has no fallback; outage = no new WS upgrades | **Medium** | `wsticket_redis.go:39-41` + `server.go:122-128` | Add in-memory fallback or document the trade-off |
| 6 | Per-replica in-memory rate buckets / WS tickets — multi-replica defeats them | **Medium** | `ratelimit.go:14-18`, `wsticket.go:56-58` | Use Redis backends in any deployment with `replicas > 1`; document explicitly |
| 7 | `apps.environment_id` has no FK; orphaned apps after env deletion | **Medium** | `003_apps.up.sql` | `ALTER TABLE apps ADD CONSTRAINT … FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE SET NULL` |
| 8 | `apps(github_repo, branch)` indexed but not `UNIQUE` | **Medium** | `003_apps.up.sql:23` | Add `UNIQUE` constraint or app-layer guard |
| 9 | `users` query uses `LOWER(email)` but index is on raw column | **Medium** | `store/postgres/user.go:24` | Functional index `(LOWER(email))` or normalise on insert |
| 10 | `apps ORDER BY updated_at DESC` unindexed | **Medium** | `store/postgres/app.go:28` | Add index |
| 11 | `RunStore.Update` re-marshals all 3 JSONB blobs on every stage transition | **Medium** | `store/postgres/run.go:85-113` | Partial-update method or event-log approach |
| 12 | List endpoints have no `LIMIT`/pagination | **Medium** | `run.go:25`, `pipeline.go:24`, `app.go:23`, `host.go`, `user.go` | Add cursor / offset pagination |
| 13 | `COOKER_SECRET_KEY` rotation has no dual-key window | **Medium** | `crypto/codec.go:30-50`, `server.go:81-85` | Accept two keys at once during rotation |
| 14 | No documented Postgres backup / restore / retention | **Medium** | `docs/RUNBOOK.md` | Add ops procedures |
| 15 | Connection pool fixed at 25/5 — no env-var override | **Low** | `store/postgres/store.go:44-46` | Expose as `COOKER_DB_MAX_OPEN_CONNS` etc. |
| 16 | Random `TEXT` PKs cause B-tree page splits | **Low** | All migrations | Switch to ULID/UUIDv7 over a future migration |
| 17 | No circuit breaker on K8s / BuildKit / Registry calls | **Low** (already in `dag-performance.md`) | builder/, deployer/, pusher/ | Adapter-level breakers |

---

## Remediation roadmap

### Quick wins (~1 day total)
- **Findings #3, #4, #10** — three `CREATE INDEX` statements in a single `007_list_indexes.up.sql`.
- **Finding #15** — three env vars + two `Set*` calls. Fifteen minutes.
- **Finding #9** — one functional index, one fewer false-negative on email lookup.

### Structural (~3-5 days total)
- **Findings #1 + #2** — adopt `golang-migrate` or write a small version-tracking runner; backfill `.down.sql` for 002–006.
- **Findings #7 + #8** — one migration adding the FK and the composite unique constraint. Test that webhook dispatch handles the now-enforced uniqueness.
- **Finding #11** — partial-update method on `RunStore`. Bigger change because every executor write site needs to declare what it's mutating.
- **Finding #12** — paginate every list endpoint. Frontend changes too.

### Architectural (~1-2 weeks)
- **Finding #5** — decide and document the Redis fail-mode contract for each consumer; add in-memory fallback for WS tickets if that's the chosen direction.
- **Finding #13** — dual-key support in `crypto.Codec`. Touches every secret read path.
- **Finding #14** — backup automation, retention cron, restore drill in CI.
- **Finding #16** — UUIDv7 migration. Coordinate with finding #1 (you'll need a real migration framework first).

The severity ordering reflects production-readiness *for the documented multi-replica use case*. For a single-replica deployment, finding #6 drops to "low" (in-memory works fine) and #5 stops mattering. Operators who deploy multi-replica without setting `WSHUB_BACKEND=redis`, `RATE_LIMIT_BACKEND=redis`, `WS_TICKET_BACKEND=redis` are in the worst spot — they'll see intermittent 401s and uneven rate limiting. Worth making this a Helm chart precondition.
