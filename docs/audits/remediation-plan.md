# Remediation Plan

**Companion to:** [`dag-performance.md`](dag-performance.md), [`spof-and-database.md`](spof-and-database.md), [`crash-and-service-quality.md`](crash-and-service-quality.md), [`vulnerabilities-and-chains.md`](vulnerabilities-and-chains.md). Every finding referenced by `[Hn-N]` (hunter), `[An-N]` (auditor), `[Cn]` (chain analyst), or section number maps back to those docs.

**Method:** group findings by *remediation theme* rather than by audit. One PR per theme retires multiple findings at once. Themes are sequenced into five phases by blast-radius and dependency order. Effort estimates assume one engineer familiar with the codebase.

**Total findings across the four audits:** ~150 distinct items (4 Critical, ~30 High, ~50 Medium, ~70 Low). This plan covers Critical and High in detail; Mediums and Lows roll up into theme PRs or follow-ups.

---

## At-a-glance roadmap

| Phase | Goal | Wall-clock | Themes |
|---|---|---|---|
| **0 — Hot-fix** | Stop the security bleed | ~3 days | T1 Buildah injection · T2 GitOps traversal · T3 IDOR sweep · T4 Production validation · T5 RBAC scoping |
| **1 — Stability (P0)** | No more crashes / leaks / DoS | ~1 week | T6 Panic & race fixes · T7 Resource leaks on shutdown · T8 Body / memory limits · T9 WebSocket deadlines |
| **2 — Reliability (P1)** | Survive operational reality | ~2 weeks | T10 Per-stage timeout + retry · T11 Optimistic concurrency · T12 Idempotency keys · T13 Log persistence · T14 Schema-validation pass |
| **3 — Production hardening (P2)** | Multi-replica and key rotation | ~2 weeks | T15 golang-migrate · T16 Async audit sink · T17 HPA + PDB + probe tuning · T18 Trace propagation + metrics · T19 KeepSave HTTPS |
| **4 — Polish (P3)** | Hygiene and ops ergonomics | ~1 week | T20 CSP + security headers · T21 Sourcemap off · T22 /version endpoint · T23 RUNBOOK gaps · T24 Hardening misc |

Each theme below is sized as its own PR. **Phase 0 should land before any Phase 1 work** — these are exploitable today.

---

## Phase 0 — Hot-fix (~3 days)

### T1 — Buildah shell injection [Critical]

- **Findings:** `crash-and-service-quality.md` Critical #1; covers `[A8-1, A8-4]` from PR #23 commit 3.
- **File:** `backend/internal/builder/buildah.go:135-152, 190-191`
- **Fix:** drop the `/bin/sh -c` wrapper. Build the buildah invocation as `[]string` args directly:
  ```go
  args := []string{"bud", "--storage-driver=" + b.cfg.StorageDriver, "-f", dockerfile, "-t", "cooker-build:current", req.ContextDir}
  for k, v := range req.BuildArgs {
      args = append(args, "--build-arg", k+"="+v)
  }
  // Run `buildah` directly via Container.Command/Args; pipe to push and inspect via separate Containers in an initContainer chain, or write a tiny Go shim that loops through the args.
  ```
  If a single shell command is unavoidable, use `kballard/go-shellquote` to escape every dynamic value.
- **Verification:** unit test with `req.BuildArgs = {"X": "; touch /tmp/owned ;"}` should not produce `;` in the rendered command.
- **Effort:** ~half a day.

### T2 — GitOps path traversal [Critical]

- **Findings:** `crash-and-service-quality.md` Critical #2.
- **File:** `backend/internal/gitops/gogit.go:104-108`
- **Fix:**
  ```go
  clean := filepath.Clean(req.Path)
  if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") || strings.Contains(clean, "../") {
      return Result{}, fmt.Errorf("gogit: invalid path %q", req.Path)
  }
  full := filepath.Join(dir, clean)
  if !strings.HasPrefix(full, dir+string(filepath.Separator)) && full != dir {
      return Result{}, fmt.Errorf("gogit: path escapes repo")
  }
  ```
  Apply the same check in `service/app_deployer.go:104` (synthesised Dockerfile path) — that's `[A8-3]`.
- **Verification:** unit tests for `../etc/passwd`, `/etc/passwd`, `subdir/../../etc/passwd`, all rejected.
- **Effort:** ~1 hour.

### T3 — IDOR sweep on `runId` and `apps/:id` [Critical / High]

- **Findings:** `vulnerabilities-and-chains.md` Critical #1 + High #2; `[A6-1, A6-2]`.
- **Files:**
  - `backend/internal/handler/pipeline.go:199-205` (`GetPipelineRun`)
  - `backend/internal/handler/pipeline.go:207-219` (`CancelPipelineRun`)
  - `backend/internal/handler/environment.go:253-289` (`PromoteRun`, `ApprovePromotion`, `GetEnvStatus`)
  - `backend/internal/handler/app.go:33-39` (`GetApp`)
- **Fix:** add a small helper in `handler/handler.go`:
  ```go
  func (h *Handler) loadRunForPipeline(c *gin.Context, runID, pipelineID string) (*model.PipelineRun, bool) {
      run, err := h.Store.Runs.Get(c.Request.Context(), runID)
      if abortStoreErr(c, err, "run not found") { return nil, false }
      if run.PipelineID != pipelineID {
          c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "run not found"})
          return nil, false
      }
      return run, true
  }
  ```
  Call from each handler. For `GetApp`, decide whether multi-tenant read-all is intentional; if not, add an ownership / membership check.
- **Verification:** integration tests issuing one user's runId against another user's pipeline `:id` must return 404.
- **Effort:** ~half a day including tests.

### T4 — Production validation gaps [High]

- **Findings:** `crash-and-service-quality.md` High #17 #18; `[A10-1, A10-2, A10-6]`.
- **File:** `backend/internal/config/config.go:246, 249, 360-361, 395-396`
- **Fix:** in `Validate()`, when `Env == EnvProduction`:
  - Reject `DatabaseURL == ""` or any `localhost`/`127.0.0.1`/default-credential URL.
  - Require `SecretKey != ""` (regardless of `SecretsBackend`) — every backend that stores anything via `Codec` needs the key.
  - Reject `AllowedOrigins == []string{"*"}`.
  - Require `KeepSave.URL` to be `https://...` if `SecretsBackend == "keepsave"` (this is `[A8-1]`'s config-side fix).
- **Verification:** unit tests for each rejection case.
- **Effort:** ~2 hours.

### T5 — RBAC scoping [High]

- **Findings:** `crash-and-service-quality.md` High #19; `[A7-7]`.
- **File:** `deploy/kubernetes/rbac.yaml:10-23`, `deploy/helm/cooker/templates/rbac.yaml`
- **Fix:** split the `ClusterRole` into:
  1. A namespaced `Role` for the cooker namespace covering pipelines/runs/apps reads.
  2. Per-builder-namespace `Role`s scoped to `Jobs/Pods` create/get/list/delete (no namespace verbs).
  3. Drop `update,patch,delete` on `secrets` and `configmaps` cluster-wide; keep them only where the deployer adapter actually needs them.
  Add `automountServiceAccountToken: true` explicitly on the cooker pod (`[A7-8]`).
- **Verification:** apply the new RBAC into a kind cluster; confirm Cooker can build via Kaniko in a different namespace; confirm cluster-wide `kubectl auth can-i delete secrets --as=system:serviceaccount:cooker:cooker` returns `no`.
- **Effort:** ~half a day plus test in kind.

---

## Phase 1 — Stability (~1 week)

### T6 — Panic recovery + race fixes in concurrency code [Critical / High]

- **Findings:** `crash-and-service-quality.md` Critical #3, High #6 #7 #8 #9.
- **Files:**
  - `backend/pkg/dagrunner/runner.go:66-78` — wrap the goroutine body in `defer func() { if r := recover(); r != nil { errCh <- fmt.Errorf("node %s panic: %v", id, r) } }()`.
  - `backend/internal/deployer/clientgo.go:55-72` — replace lazy check-then-set with `sync.Once`.
  - `backend/internal/server/websocket.go:89, 98-105` — fix double-close (use `sync.Once` per client) and map-mutation-under-RLock (collect-victims-then-delete-after-Lock).
  - `backend/internal/server/runs.go:58-67` — join the inner heartbeat goroutine via a `done chan struct{}` before the outer defer fires.
  - `backend/internal/builder/buildkit.go:75-86` — wrap log-drain goroutine in `select { case <-ctx.Done(): }` and ensure `statusCh` is drained on error.
- **Verification:** `go test ./... -race -count=10`; add a stress test that triggers each code path 1000 times.
- **Effort:** ~2 days. Five files but each fix is small.

### T7 — Resource leaks on shutdown [High]

- **Findings:** `crash-and-service-quality.md` High #10; `[H5-1, H5-2, H5-3, H5-7, A6-2]`.
- **Files:**
  - `backend/internal/server/server.go:257-273` (`Server.Close`) — close `redisClient`, `wsHub`, audit sink in dependency order; nil-guard each.
  - `backend/internal/server/server.go:216` — reorder so `go wsHub.Run()` fires *after* `s.registerRoutes()`.
  - `backend/cmd/cooker/main.go:51-58` — nil-check `srv` before `srv.Close()` on early SIGTERM.
- **Verification:** `server_shutdown_test.go` already exists; extend it to cover Redis, hub, and pre-route SIGTERM cases.
- **Effort:** ~half a day.

### T8 — Body and memory limits [High]

- **Findings:** `crash-and-service-quality.md` High #11 #12.
- **Fix:**
  - `backend/internal/handler/app.go:242` — wrap with `io.LimitReader(c.Request.Body, 10 << 20)`.
  - Apply `MaxRequestBodySize` middleware globally (~1 MB) for non-webhook routes, ~10 MB for webhook routes that can be larger (manifest apply).
  - `backend/internal/builder/kaniko.go:217`, `backend/internal/builder/buildah.go:195` — add `Limits` alongside `Requests`. Suggested: `Memory: "2Gi"`, `CPU: "2"` (configurable via `KanikoConfig` / `BuildahConfig`).
- **Verification:** unit test posts a 50 MB body and expects 413. Kaniko unit test asserts `Limits` is set.
- **Effort:** ~half a day.

### T9 — WebSocket deadlines + ping/pong [High]

- **Findings:** `crash-and-service-quality.md` High #13.
- **File:** `backend/internal/server/websocket.go:158-178`
- **Fix:**
  ```go
  const (
      pongWait   = 60 * time.Second
      pingPeriod = (pongWait * 9) / 10
      writeWait  = 10 * time.Second
  )
  func (c *Client) readPump() {
      defer func() { c.hub.unregister <- c; c.conn.Close() }()
      c.conn.SetReadDeadline(time.Now().Add(pongWait))
      c.conn.SetPongHandler(func(string) error {
          c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil
      })
      for { _, _, err := c.conn.ReadMessage(); if err != nil { break } }
  }
  func (c *Client) writePump() {
      ticker := time.NewTicker(pingPeriod); defer func() { ticker.Stop(); c.conn.Close() }()
      for {
          select {
          case msg, ok := <-c.send:
              c.conn.SetWriteDeadline(time.Now().Add(writeWait))
              if !ok { c.conn.WriteMessage(websocket.CloseMessage, nil); return }
              if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil { return }
          case <-ticker.C:
              c.conn.SetWriteDeadline(time.Now().Add(writeWait))
              if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil { return }
          }
      }
  }
  ```
- **Verification:** unit test with a stalled fake client; the connection should drop after `pongWait`.
- **Effort:** ~half a day.

---

## Phase 2 — Reliability (~2 weeks)

### T10 — Per-stage timeout + retry [High]

- **Findings:** `dag-performance.md` items 3, 6.
- **Files:** `backend/internal/service/executor.go:168-254`, all builder/pusher/deployer adapters.
- **Fix:**
  1. Honour `Stage.Config.Timeout` for every stage type via `context.WithTimeout`. Default to a sane upper bound (e.g., 30 min) when unset.
  2. Add a small `retry` package (~50 lines) with exponential backoff + jitter and an error classifier (transient vs. permanent).
  3. Add `Stage.Config.Retries` (default 0) and wrap each builder/pusher/deployer call.
- **Verification:** unit tests with mock builders that fail N times then succeed.
- **Effort:** ~3 days including error classification.

### T11 — Optimistic concurrency [High]

- **Findings:** `vulnerabilities-and-chains.md` § B.5 #5; `[C17-5]`.
- **Files:**
  - New migration `007_versioning.up.sql` adding `version INT NOT NULL DEFAULT 1` to `pipelines`, `apps`, `environments`, `hosts`.
  - `backend/internal/store/postgres/pipeline.go:85-104` and friends — `UPDATE ... SET ... version=version+1 WHERE id=$1 AND version=$N`. Return `ErrConflict` (new typed error) when `RowsAffected == 0` and the row exists.
  - Handlers: read the version on GET, accept it on PATCH/PUT, return 409 on conflict.
- **Verification:** integration test with two simultaneous PATCHes; one should win, one should 409.
- **Effort:** ~2 days.

### T12 — Idempotency keys [Medium]

- **Findings:** `crash-and-service-quality.md` Medium (Part B.1 #3 #4); `[A6-3, A6-4]`.
- **Fix:**
  1. Accept an `Idempotency-Key` header on `POST /pipelines/:id/run`, `POST /apps/:id/deploy`, and the GitHub webhook (use `X-GitHub-Delivery`).
  2. New table `idempotency_keys (key TEXT PK, run_id TEXT, expires_at TIMESTAMPTZ)` with a 24 h TTL.
  3. Middleware checks the table before spawning; returns the existing run-id on hit.
- **Verification:** integration test issuing two requests with the same key returns the same run-id.
- **Effort:** ~1 day.

### T13 — Log persistence (wire `LogWriter` through executor) [High]

- **Findings:** `dag-performance.md` item 2; `[A9-4]`.
- **Files:** `backend/internal/service/executor.go:168-254`, all builder/pusher/deployer adapters.
- **Fix:**
  ```go
  func (e *Executor) executeBuild(ctx context.Context, stage *model.Stage, sr *model.StageRun) error {
      buf := newCappedBuffer(1 << 20) // 1 MiB cap
      defer func() { sr.Logs = buf.String() }()
      req := builder.Request{ ..., LogWriter: io.MultiWriter(buf, e.wsBroadcastWriter(sr.StageID)) }
      ...
  }
  ```
  Also: wire `runner.Updates()` into `RunStore.UpdateStage()` so partial progress survives crashes (`[A9-4]`).
- **Verification:** integration test runs a build, asserts `run.StageRuns[0].Logs` contains expected output.
- **Effort:** ~1 day for log persistence; +1 day for partial-progress writes.

### T14 — Schema-validation pass [High]

- **Findings:** `vulnerabilities-and-chains.md` High #6 #7 #8 #9 #10.
- **Files:** `backend/internal/handler/pipeline.go`, `host.go`, `app.go`, `docker.go`, `registry.go`, `environment.go`, `auth_local.go`.
- **Fix:** define one tiny `validate` package that exposes:
  ```go
  func StageType(s string) error
  func HostKind(s string) error
  func GitHubRepo(s string) error          // ^[\w.-]+/[\w.-]+$
  func DockerTag(s string) error            // ^[a-z0-9]+([._-][a-z0-9]+)*$
  func GitRefName(s string) error           // git's ref-format rules
  func RegistryURL(s string) error          // requires https:// in production
  func MaxLen(s string, n int) error
  ```
  Call from every handler post-bind. For polymorphic stage `Config`, validate per `Stage.Type`.
- **Verification:** table-driven unit tests on the validators; integration tests that send each invalid input and expect 400.
- **Effort:** ~2 days (lots of small surface area).

---

## Phase 3 — Production hardening (~2 weeks)

### T15 — Adopt `golang-migrate` (or equivalent) [High]

- **Findings:** `spof-and-database.md` items 1, 2; chain `[C13-3]`.
- **Files:** `backend/internal/store/postgres/store.go:32, 113-137`; `backend/internal/store/postgres/migrations/`.
- **Fix:**
  1. Add `golang-migrate/migrate` as a dependency.
  2. Embed `migrations/*.sql` (both up and down).
  3. Replace `applyMigrations` with `migrate.New(...).Up(...)`.
  4. Backfill `*.down.sql` for migrations 002–006.
  5. Document rollback procedure in `RUNBOOK.md`.
- **Verification:** integration test against a fresh Postgres applies all migrations, then rolls back to 001, then re-applies, leaving the schema identical.
- **Effort:** ~1 day for the swap, +1 day for backfilling down migrations and documenting.

### T16 — Async audit sink [High]

- **Findings:** `crash-and-service-quality.md` High #14.
- **File:** `backend/internal/audit/audit.go:39-122`
- **Fix:** replace synchronous `Write` under `Mutex` with a buffered channel (size 1024) + worker goroutine. Drop-on-full with a counter when overflow occurs; expose the drop-count metric. Rotate the file at a configurable size.
- **Verification:** stress test with 10k concurrent mutating requests; `Emit` latency should stay under 1 ms.
- **Effort:** ~1 day.

### T17 — HPA + PDB + probe tuning [Medium]

- **Findings:** `crash-and-service-quality.md` Medium (B.5 chart gaps); `[A7-2, A10-5]`.
- **Files:** `deploy/helm/cooker/templates/{hpa.yaml,pdb.yaml}` (new), `deploy/helm/cooker/templates/deployment.yaml:192-202`.
- **Fix:** templates with `enabled` toggles in `values.yaml`. Probe overrides: `timeoutSeconds: 5`, `failureThreshold: 5`, `successThreshold: 1`.
- **Verification:** `helm lint` + `helm template` (the P6.1 backlog item — wire into CI at the same time).
- **Effort:** ~half a day.

### T18 — Trace propagation + missing metrics [Medium]

- **Findings:** `crash-and-service-quality.md` Medium (B.4 #1 #2); `[A9-1, A9-2]`.
- **Files:** `backend/pkg/dagrunner/runner.go:64-78`, `backend/internal/observability/observability.go:39-70`, executor / builder / deployer adapters.
- **Fix:**
  1. Inject `otel.GetTextMapPropagator().Inject` around each goroutine launch in `runner.go`.
  2. Add histograms for HTTP auth latency, per-stage build/push/deploy duration, K8s/registry call latency.
  3. Attach `slog.With("run", run.ID)` at the top of `Execute` and pass via context.
- **Verification:** trace one pipeline run end-to-end; trace-ID present on every span.
- **Effort:** ~1.5 days.

### T19 — KeepSave HTTPS enforcement + outbound TLS hardening [High]

- **Findings:** `vulnerabilities-and-chains.md` High #11; `[A8-1]`. `[A8-2, A8-3, A8-4]`.
- **Files:** `backend/internal/secrets/keepsave/client.go:34`, `backend/internal/deploytarget/render/render.go:36`, `backend/internal/deploytarget/flyio/flyio.go:36`, `backend/internal/transport/tsnet/real.go:62`.
- **Fix:** every outbound `http.Client` constructed with an explicit `&http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, ...}`. Reject `http://` for KeepSave in `config.Validate()` (already in T4).
- **Verification:** unit tests with a TLS 1.0/1.1 server should fail; HTTP-scheme KeepSave URL should be rejected at boot.
- **Effort:** ~half a day.

---

## Phase 4 — Polish (~1 week)

### T20 — CSP + security response headers [High]

- **Findings:** `vulnerabilities-and-chains.md` High #3 #4; `[A4-1, A10-1]`.
- **Files:**
  - `backend/internal/server/server.go` — new `securityHeadersMiddleware`:
    ```go
    c.Header("X-Content-Type-Options", "nosniff")
    c.Header("X-Frame-Options", "DENY")
    c.Header("Referrer-Policy", "no-referrer")
    c.Header("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
    c.Header("Content-Security-Policy", "default-src 'self'; ...")
    ```
  - Backend secret-bearing routes — `Cache-Control: no-store`.
  - `frontend/index.html` — `<meta http-equiv="Content-Security-Policy" ...>` as fallback.
- **Verification:** `curl -I` against a representative endpoint shows all five headers.
- **Effort:** ~half a day; the CSP policy itself takes a couple of iterations to tighten.

### T21 — Vite sourcemap off in prod [High]

- **Findings:** `vulnerabilities-and-chains.md` High #5; `[A4-2]`.
- **File:** `frontend/vite.config.ts`
- **Fix:** `build: { sourcemap: false }`. Confirm CI build artifacts contain no `.map` files.
- **Effort:** ~5 minutes.

### T22 — `/version` endpoint [Low]

- **Findings:** `crash-and-service-quality.md` Medium #11; `[A10-11]`.
- **Files:** `backend/cmd/cooker/main.go`, `backend/internal/server/router.go`.
- **Fix:** populate `var Version, Commit, BuildTime string` via `-ldflags`; expose `GET /version` returning JSON. Update Helm values to set the build SHA.
- **Effort:** ~1 hour.

### T23 — RUNBOOK.md gaps [Medium]

- **Findings:** `spof-and-database.md` item 14; `[A10-9]`.
- **File:** `docs/RUNBOOK.md`
- **Fix:** sections for backup/restore (pg_basebackup + WAL archiving + restore drill), on-call escalation, monitoring dashboard pointers, secrets-backend (vault/aws/gcp/keepsave) failure modes.
- **Effort:** ~half a day of writing.

### T24 — Hardening miscellany [Mixed]

A bundle of small items that don't justify their own PRs:

- `math/rand` → `crypto/rand` for backoff jitter (`wshub_backend.go:202`, `store/postgres/store.go:95`). 30 minutes.
- Strip ANSI escapes from Kaniko log stream (`builder/kaniko.go:298-328`). 1 hour.
- Strip `.git/hooks/*` post-clone in `source/github/clone.go`. 30 minutes.
- Add `deleted` flag check to GitHub webhook parser. 30 minutes.
- K8s manifest YAML reader wrapped with `io.LimitReader` + depth cap (`deployer/clientgo.go:130-147`). 1 hour.
- Local-auth signup events emitted to slog at INFO. 15 minutes.
- Drop `optional: true` on `COOKER_SECRET_KEY` Helm `secretKeyRef`. 5 minutes.
- Helm `required()` guard for `builder.kind=kaniko` + missing `contextPVC`. 15 minutes.
- Pin alpine base + apk versions in `deploy/docker/Dockerfile`. 15 minutes.
- Configurable `MaxOpenConns` etc. via env vars. 30 minutes.

**Effort total:** ~half a day.

---

## Cross-cutting items (synthesised top-10 from `vulnerabilities-and-chains.md`)

These are root causes that several themes share. Phase 2's T11 + T12 + Phase 3's T15 retire most of them:

| Root cause | Retired by |
|---|---|
| 1. No version tracking on schema | T11 + T15 |
| 2. In-memory state with multi-replica | T16 (audit) + T15 (migrations) + (existing Redis backends — operators flip the env vars) |
| 3. No per-stage / per-call timeout | T10 |
| 4. No idempotency keys | T12 |
| 5. No optimistic concurrency | T11 |
| 6. Heartbeat / drain timing too tight | T6 (heartbeat join) + values tuning in T17 |
| 7. No request body / connection / time budget limits | T8 + T9 + T10 |
| 8. Configuration mutations require restart | (out of scope for this plan; needs `Codec` dual-key support and live reload — separate design effort) |
| 9. Cleanup deferred to TTLs | T6 + T15 (lifecycle hooks) |
| 10. Validate at handler not model | T14 |

---

## What's intentionally out of scope

The following appear in the audits but aren't addressed by this plan, by design:

- **Frontend feature work** (e.g., Sentry telemetry, error boundary). Recommend a follow-up plan focused on frontend.
- **Live config reload** (OIDC issuer, group→role map, `COOKER_SECRET_KEY` dual-key). Needs design discussion; not a one-PR change.
- **Bottom-half service-quality items** (most Lows in `crash-and-service-quality.md` and `vulnerabilities-and-chains.md`). They're listed in their respective audits; pick them up after Phase 4 lands.
- **Test-coverage uplift.** Each theme above adds tests for the changed behaviour, but a broader coverage drive is its own effort.
- **Dependency vulnerability scanning** (Dependabot, govulncheck wiring into CI). Recommend a separate, small CI PR.
- **Frontend bundle-size optimisation.** Out of scope for security/reliability remediation.

---

## How to use this document

1. **Open issues / tickets** for each Theme (T1–T24). Issue title = theme title; body = the relevant section here.
2. **Branch per theme** (`fix/T1-buildah-injection`, etc.); one PR per theme.
3. **Land Phase 0 first**, in any order — they're independent of each other.
4. **Phase 1 themes can mostly run in parallel**, but T6's runner panic-recovery should land before any throughput-heavy work.
5. **Phase 2 has a soft order:** T11 (versioning) → T12 (idempotency) → T13 (log persistence) → T14 (validation). T10 (timeouts/retry) can run in parallel.
6. **Phases 3 and 4** are independent and can be picked up by anyone.

Each PR description should reference the audit doc + finding ID it closes (e.g., "Closes `[A6-1]` from `vulnerabilities-and-chains.md`"), so the audits stay live tracking documents.
