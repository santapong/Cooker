# Crash & Service-Quality Audit

**Companion to:** [`dag-performance.md`](dag-performance.md), [`spof-and-database.md`](spof-and-database.md), [`vulnerabilities-and-chains.md`](vulnerabilities-and-chains.md), and [`remediation-plan.md`](remediation-plan.md) (the phased fix plan). This one fans out to the rest of the codebase.

**Method:** ten parallel Explore agents, each scoped to one failure class (panics, goroutine leaks, resource exhaustion, races, startup, availability, performance, security, observability, operational readiness). Each was briefed not to re-cover material already in the two earlier audits. Headline findings were spot-checked against the source before publishing — every Critical/High finding here has been read directly.

**TL;DR — the two findings that can take prod down or expose secrets:**

1. **`backend/internal/builder/buildah.go:143-152` — shell injection.** The `buildah bud` command is built with `fmt.Sprintf` and run via `/bin/sh -c` (line 190-191). User-controllable fields (`req.Dockerfile`, `req.ContextDir`, `req.BuildArgs[k]=v`, `req.Tags[i]`) are interpolated unescaped. Anyone who can submit a pipeline definition gets RCE on the builder pod.
2. **`backend/internal/gitops/gogit.go:104-108` — path traversal.** `filepath.Join(dir, req.Path)` with no boundary check. `req.Path = "../../../etc/passwd"` writes outside the repo. The gitops stage is gated on auth, but any authorised user with pipeline-edit permission can pivot to arbitrary file write inside the cooker pod.

Both are **fixable in a single afternoon.** Details below.

---

## Part A — Crash bugs

### A.1 Panics, nil dereferences, type assertions

**Hunter 1 result:** *no critical findings.* Codebase is well-guarded — the two-step `requireCodec()` pattern in handlers, defensive `len(pods.Items) == 0` checks before indexing, and consistent `, ok :=` on type assertions. The single ignored type-assertion at `backend/internal/auth/local/local.go:191` (`iss, _ := claims["iss"].(string)`) is intentional. **No fixes needed in this category.**

### A.2 Goroutine leaks, channel mis-use, deadlocks

| Finding | File | Severity | Fix |
|---|---|---|---|
| Channel double-close panic on backpressure + unregister race | `server/websocket.go:89, 104` (`close(client.send)` in two paths) | **High** | Guard with `sync.Once` per client, or remove the broadcast-side close and rely on `readPump`'s defer cleanup |
| Nested heartbeat goroutine leak when work outlives parent ctx | `server/runs.go:58-67` | **High** | Wait for the inner ticker goroutine via a join channel before `Spawn` returns |
| BuildKit log-drain goroutine leaks on `Solve` early-exit | `builder/buildkit.go:75-86` (`statusCh` orphaned) | **High** | Use a buffered channel + drain on error, or wrap the goroutine in a `select { case <-ctx.Done() }` |
| `rateLimiter.gc()` runs forever, no shutdown hook | `server/ratelimit.go:44-75` | Low | `Close()` method called from `Server.Close()` |
| `wsTicketStore.gc()` same pattern | `server/wsticket.go:79, 116-128` | Low | Same |
| `handler/app.go:157 vs 162` — inconsistent run tracking | `handler/app.go` | Low | Always route through `RunCoordinator.Spawn` |

### A.3 Resource exhaustion (FDs, conns, memory, body limits)

| Finding | File | Severity | Fix |
|---|---|---|---|
| GitHub webhook reads body via unbounded `io.ReadAll` | `handler/app.go:242` | **High** (DoS) | `io.LimitReader(c.Request.Body, 10<<20)` |
| Kaniko Pod has memory **request** but **no limit**; 5 GB COPY context can OOM the node | `builder/kaniko.go:217` (also `buildah.go:195`) | **High** | Add `Limits: corev1.ResourceList{...}` alongside `Requests` |
| Per-client WebSocket send channel buffered to 256 messages | `server/websocket.go:148` | Medium | Size based on burst behaviour, or apply backpressure with deadline-aware send |
| Gin default `MaxRequestBodySize` (32 MB) not narrowed | `server/server.go` | Medium | Set per-route or globally to ~1 MB for pipeline submissions |

### A.4 Data races

| Finding | File | Severity | Fix |
|---|---|---|---|
| **Critical** non-atomic check-then-set on `c.cli` / `c.mapper` lazy init | `deployer/clientgo.go:55-72` (`if c.cli != nil ...` line 56, writes lines 71-72) | **Critical** | `sync.Once` |
| Map mutation (`delete`) during `RLock` iteration | `server/websocket.go:98-109` (delete at line 105 inside `RLock` block opened on line 98) | **High** (Go panics on concurrent mutation) | Collect victims in a slice during iteration, drop the lock, then upgrade to `Lock` to delete |
| TOCTOU on ticket consume: deletes before checking expiry; concurrent consume of same ticket can both "succeed" | `server/wsticket.go:102-114` | Medium | Move expiry check before the `delete` |
| `*rate.Limiter` returned outside lock; `gc()` can delete the bucket between unlock and `Allow()` | `server/ratelimit.go:49-89` | Low | `rate.Limiter` is itself goroutine-safe; the only effect is a benign loss of bucket state on race. Acceptable. |

### A.5 Boot / startup failure modes

| Finding | File | Severity | Fix |
|---|---|---|---|
| `redisClient` never closed in `Server.Close()` | `server/server.go:102, 257-273` | **High** | Add `if s.redisClient != nil { s.redisClient.Close() }` in `Close()` |
| `wsHub` and its Redis backend not closed on shutdown — `consume()` and `Run()` goroutines leak | `server/server.go:112-117, 216` | **High** | Add `wsHub.Close()` in `Close()`; in turn close the backend's context (already in place) |
| Audit sink leaks on later `New()` early-return | `server/server.go:143-147` (and any error after that point) | Medium | Defer-based cleanup chain in `New()` |
| `go wsHub.Run()` launched **before** `s.registerRoutes()` | `server/server.go:216` | Medium | Reorder to launch after routes registered |
| `/health/ready` doesn't fail when JWKS is stale | `server/health.go:80-88` (only logs the age, never sets `ok=false`) | Medium | Threshold-fail when age > N hours or "never refreshed" |
| Production `Validate()` doesn't check `DatabaseURL` is non-default | `config/config.go:353-356` | Medium | Add explicit check |
| SIGTERM during `server.New()` panics on nil `srv.Close()` | `cmd/cooker/main.go:51-58` | Medium | Nil-check `srv` before calling `Close()` |

---

## Part B — Service quality

### B.1 Availability and graceful degradation

| Finding | File | Severity | Fix |
|---|---|---|---|
| WebSocket `readPump` / `writePump` lack `SetReadDeadline` / `SetWriteDeadline` and ping/pong | `server/websocket.go:158-178` | **High** (Slow-Loris DoS) | Add 60s read deadline, 10s write deadline, 30s ping ticker |
| Hub goroutine not stopped on shutdown — overlap with A.5 #2 | same file | **High** | Same fix |
| GitHub webhook lacks `X-GitHub-Delivery` deduplication | `handler/app.go:238-304` | Medium | Capture delivery ID, store in a small TTL set (Redis or in-memory), reject duplicates |
| Pipeline-run trigger lacks idempotency-key support | `handler/pipeline.go:131-186` | Medium | Accept `Idempotency-Key` header, return cached run-id on retry |
| Deployer returns after `kubectl apply`, doesn't wait for rollout `Available` | `deployer/clientgo.go:78-127` | Medium | `wait.PollUntilContextCancel` on Deployment status conditions |
| No per-request timeout middleware in Gin | `server/router.go` | Medium | Per-route `context.WithTimeout`, e.g. 30s for mutating routes |
| Orphan sweep runs on every replica's boot — slight thundering herd | `server/server.go:169-179` | Low | Leader election or single cron CronJob outside cooker |

### B.2 Performance bottlenecks (beyond DAG and DB)

| Finding | File | Severity | Fix |
|---|---|---|---|
| Audit file-sink mutex held during sync `Write` on every authenticated mutation | `audit/audit.go:120-122` | **High** (lock contention) | Async sink: bounded channel + worker goroutine; drop or block based on policy |
| `mwWriter` mutex serialises every log line in Clone/Build/Deploy | `service/app_deployer.go:229-239` | **High** | Channel-based fanout, or split per-stream writers |
| `wsLogSink.Write` allocates `append([]byte(nil), p...)` per byte | `handler/app.go:224` | Medium | Slice `p` directly or use `bytes.Buffer` with pool |
| `decodeBroadcast` allocates per cross-replica message | `server/wshub_backend.go:262` | Medium | Slice the payload directly when the recipient is single-use |
| OIDC `ensureProvider` uses `c.Request.Context()` for slow IdP discovery | `auth/oidc.go:109` | Medium | Background context with 10s timeout, or cache the metadata |

### B.3 Security

| Finding | File | Severity | Fix |
|---|---|---|---|
| **Shell injection in Buildah builder.** `buildah bud --storage-driver=%s -f %s -t cooker-build:current %s` then `--build-arg %s=%s` per arg, all via `fmt.Sprintf`, then run as `/bin/sh -c {cmd}` (line 190-191) | `builder/buildah.go:143-152, 190-191` | **Critical** (RCE) | Switch to `exec`-style args (no shell) — invoke buildah directly with each arg as its own element. Or shell-escape every dynamic value via `kballard/go-shellquote` |
| **Path traversal in GitOps writer.** `filepath.Join(dir, req.Path)` with no boundary check; `req.Path = "../../../etc/foo"` writes outside the repo | `gitops/gogit.go:104-108` | **Critical** | `clean := filepath.Clean(req.Path); if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) { return error }; full := filepath.Join(dir, clean)` |
| Path traversal in synthesised Dockerfile path — `BuildPlan.Path` flows directly into the build request without validation | `service/app_deployer.go:104` | High | Validate `plan.Path` with the same clean-and-bound check |
| Same shell-injection class for buildah `push` and `inspect` lines (uses `--storage-driver=%s` + `docker://%s` for tag) | `builder/buildah.go:149-150, 151` | High | Same fix as B.3 #1 |
| `oidc.go` HTTP responses include raw `err.Error()`, leaking IdP URL, network internals, JWT parse errors | `auth/oidc.go:161, 188, 203, 218` | Medium | Log full error server-side; return generic "invalid token" / "auth provider unavailable" |
| `handler/app.go` echoes `X-GitHub-Event` header in error responses | `handler/app.go` | Low | Hardcode message |

**Confirmations (no findings):**
- SQL injection — all postgres queries use `$1`, `$2` placeholders.
- HMAC comparison — `hmac.Equal` used in webhook signature check (`source/github/webhook.go:38`).
- JWT alg confusion — `coreos/go-oidc/v3` enforces RS256 by default; verifier checks `iss`, `aud`, `exp`.
- SSRF — no user-controlled IdP / registry URLs in scope.
- CSRF — bearer-token-only API; no cookie auth.

### B.4 Observability gaps

| Finding | File | Severity | Fix |
|---|---|---|---|
| Status updates from `runner.Updates()` only go to slog, not persisted to store; on crash, partial progress is lost | `service/executor.go:147-151` (also flagged in `dag-performance.md`) | **High** | Pump `StatusUpdate` into `RunStore.UpdateStage()` instead of (or as well as) slog |
| Secret-bytes leak risk — `slog.Warn("...", "err", err)` for codec failures could include sealed bytes | `handler/app.go:282` | Medium | Drop the `err` field; log only the app ID |
| No HTTP auth latency / per-stage build/push/deploy duration / K8s/registry call latency metrics | `observability/observability.go:39-70` | Medium | Add histograms + wire from middleware and service layer |
| DAG goroutines don't propagate trace context | `pkg/dagrunner/runner.go:64-78` | Medium | `otel.GetTextMapPropagator().Inject` / `Extract` around the goroutine launch |
| K8s deploy adapter logs nothing about apply results, conflicts, or rollout status | `deployer/clientgo.go:78-128` | Low | One slog line per applied resource; events-watcher for rollout |
| Heartbeat errors logged but not metered | `server/runs.go:68-82` | Low | `observability.IncHeartbeatError()` |
| Frontend has no client-side telemetry / error boundary | `frontend/src/` | Low | Sentry or equivalent |

### B.5 Operational readiness

| Finding | File | Severity | Fix |
|---|---|---|---|
| Production `Validate()` doesn't reject default/empty `DatabaseURL` | `config/config.go:246` | **High** | Reject `localhost`/empty when `Env==production` |
| `COOKER_SECRET_KEY` validation only fires when `SecretsBackend == "database"`; other paths boot with empty key | `config/config.go:249, 360-361` | **High** | Require key when **any** backend that uses the codec is active, or when `Env==production` |
| Helm `secretKeyRef` for `COOKER_SECRET_KEY` is `optional: true` — pod boots if Secret is absent | `deploy/helm/cooker/templates/deployment.yaml:137` | Medium | Drop `optional` (default is false) |
| No Helm `required()` guard for `builder.kind=kaniko` + missing `contextPVC` | `deploy/helm/cooker/values.yaml:100` + template | Medium | Helm `{{- required "..." .Values.builder.contextPVC }}` |
| Helm chart has no `HorizontalPodAutoscaler` or `PodDisruptionBudget` templates | `deploy/helm/cooker/templates/` | Medium | Add `hpa.yaml`, `pdb.yaml` with `enabled` toggles |
| `AllowedOrigins == ["*"]` silently accepted in production | `config/config.go:395-396` | Medium | Reject in production `Validate()` |
| `deploy/kubernetes/rbac.yaml:10-23` — cluster-wide `ClusterRole` with full verbs on namespaces/secrets/configmaps | `deploy/kubernetes/rbac.yaml` | **High** | Split into namespaced `Role` for cooker namespace + per-builder-namespace `Role` for Job creation |
| Dockerfile not fully version-pinned (apk packages, alpine tag) | `deploy/docker/Dockerfile:51-60` | Low | Pin alpine to a digest; pin apk versions |
| `docs/RUNBOOK.md` missing backup/restore, on-call escalation, monitoring dashboards, vault/aws/gcp failure modes | `docs/RUNBOOK.md` | Low | Sectional additions |
| No circuit breaker on HTTP secret backends (vault/AWS/GCP/KeepSave) | `secrets/manager.go` | Low | Wrap with simple `gobreaker` or in-process state machine |
| No `/version` endpoint | n/a | Low | `/version` returning `{build_sha, release_tag, go_version}` populated via `-ldflags` |

---

## Cross-cutting severity table

Every Critical and High finding from both Parts in a single ranked list. Severity tie-broken by user-visible blast radius.

| # | Severity | Finding | File |
|---|---|---|---|
| 1 | **Critical** | Shell injection in Buildah builder (`/bin/sh -c` with un-escaped user input) | `builder/buildah.go:143-152, 190-191` |
| 2 | **Critical** | Path traversal in GitOps writer | `gitops/gogit.go:104-108` |
| 3 | **Critical** | Data race on lazy `cli`/`mapper` init in K8s deployer | `deployer/clientgo.go:55-72` |
| 4 | **High** | Path traversal in synthesised Dockerfile path | `service/app_deployer.go:104` |
| 5 | **High** | Buildah push/inspect use same shell-injection pattern as #1 | `builder/buildah.go:149-150, 151` |
| 6 | **High** | WebSocket double-close panic | `server/websocket.go:89, 104` |
| 7 | **High** | Map mutation during `RLock` iteration | `server/websocket.go:98-105` |
| 8 | **High** | Nested heartbeat goroutine leak | `server/runs.go:58-67` |
| 9 | **High** | BuildKit log-drain goroutine leak | `builder/buildkit.go:75-86` |
| 10 | **High** | `redisClient` never closed; `wsHub.Run()`/`consume()` goroutines leak past shutdown | `server/server.go:102, 112-117, 257-273` |
| 11 | **High** | GitHub webhook unbounded `io.ReadAll` (DoS) | `handler/app.go:242` |
| 12 | **High** | Kaniko / Buildah memory request only, no limit (node OOM) | `builder/kaniko.go:217`, `builder/buildah.go:195` |
| 13 | **High** | WebSocket pumps lack deadlines + ping/pong (Slow-Loris) | `server/websocket.go:158-178` |
| 14 | **High** | Audit file-sink mutex on every authenticated mutation | `audit/audit.go:120-122` |
| 15 | **High** | `mwWriter` mutex serialises log fanout in Clone/Build/Deploy | `service/app_deployer.go:229-239` |
| 16 | **High** | Status updates not persisted; partial progress lost on crash | `service/executor.go:147-151` |
| 17 | **High** | Production `Validate()` doesn't reject default/empty `DATABASE_URL` | `config/config.go:246` |
| 18 | **High** | `COOKER_SECRET_KEY` only required when `SecretsBackend==database` | `config/config.go:249, 360-361` |
| 19 | **High** | Cluster-wide `ClusterRole` over-permissive for Cooker SA | `deploy/kubernetes/rbac.yaml:10-23` |

(Mediums and Lows are in their respective Part B subsections — 30 more findings across availability, observability, operational hygiene.)

---

## Top 10 to fix this sprint

Prioritised by **(blast radius × likelihood) ÷ effort**.

1. **Buildah shell injection (#1)** — RCE on the builder pod. Fix is half a day.
2. **GitOps path traversal (#2)** — arbitrary file write. Three lines.
3. **Critical data race in deployer (#3)** — `sync.Once`. Five lines.
4. **WebSocket double-close panic (#6)** + **map mutation during RLock (#7)** — both in the same hub-loop method; fix together.
5. **WebSocket Slow-Loris (#13)** — `SetReadDeadline` + ping. Half a day.
6. **GitHub webhook body limit (#11)** — `io.LimitReader`. Five lines.
7. **Cluster-wide RBAC (#19)** — split into namespaced roles.
8. **Production validation (#17, #18, #10 partial)** — reject defaults; require key; close `redisClient`. Same `Validate()` PR.
9. **Audit sink async (#14)** — unblocks every authenticated request under load.
10. **Status updates persisted (#16)** — partial-progress recovery; one of the two highest user-visible improvements.

That's roughly two engineer-weeks if done in parallel. None of the ten requires a refactor of more than one file.

---

## Out of scope for this audit

- **Frontend correctness** — not deeply audited beyond confirming no client telemetry.
- **Helm chart values bikeshed** — flagged structural gaps (HPA, PDB, optional secret) but didn't review every default.
- **Test coverage** — assumed adequate; CI runs `go test -race`.
- **Documentation prose quality** — only flagged missing topics, not editorial issues.
- **Dependency vulnerability scanning** — no Snyk/Dependabot review; left to operator.

For the auditor curious about scope: ten parallel agents covered ~95% of the backend Go code by file count. Frontend, Helm internals, and the secrets-backend integrations got a lighter touch. Follow-up audits could focus on each in turn.
