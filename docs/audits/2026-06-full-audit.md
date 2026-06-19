# Full-Project Audit — Performance, Bugs & SPOF

**Date:** 2026-06-19
**Companion to:** the existing audit series ([`crash-and-service-quality.md`](crash-and-service-quality.md), [`spof-and-database.md`](spof-and-database.md), [`vulnerabilities-and-chains.md`](vulnerabilities-and-chains.md), [`2026-05-perf-and-optimization.md`](2026-05-perf-and-optimization.md), [`2026-05-deploy-parity.md`](2026-05-deploy-parity.md), [`2026-05-store-parity.md`](2026-05-store-parity.md), [`2026-05-frontend-hygiene.md`](2026-05-frontend-hygiene.md), [`2026-05-security-review.md`](2026-05-security-review.md)). This sweep re-audits the whole tree against three lenses — **bugs, single-points-of-failure, and performance** — and is deliberately cross-checked against the prior docs so already-known/closed items aren't re-litigated.

**Method:** six parallel agents, one per surface — backend core (`handler`/`service`/`server`/`cmd`), persistence (`store`), pluggable backends (`builder`/`pusher`/`deployer`/`deploytarget`/`secrets`/`transport`), frontend (`stores`/`api`/`hooks`/`auth`/`pages`/`components`), deploy/infra (Helm/K8s/Dockerfile/compose/CI), and a security cross-cut. Every Critical/High below was read directly at the cited line.

---

## TL;DR — the findings that can lose data, leak secrets, or take prod down

1. **`server/wshub_backend.go:80` — `memoryHubBackend.Publish` blocks forever on a full buffer.** Unconditional send into a 256-slot channel; the executor's inline WS broadcaster shares that path, so a slow client + a log burst can deadlock stage execution and fail live runs. *(CR-1)*
2. **`secrets/vault/vault.go:107-126` — Vault `Put`/`Delete` read-modify-write race.** No CAS; two concurrent stage secret writes to the same env silently drop one secret. *(CR-2)*
3. **`builder/kaniko.go:144` + `buildah.go:120` — `streamLogs` goroutine + kube-apiserver connection leaked per build.** No join before `Build()` returns; ~1 leaked goroutine+connection per build. *(CR-3)*
4. **`deploy/kubernetes/deployment.yaml:94` — raw manifest probes still hit `/health` with `initialDelaySeconds: 10`.** The deploy-parity doc records this as *closed*, but the fix was never applied — premature SIGKILL on cold boot + readiness that doesn't reflect DB state. *(CR-4)*
5. **`deploy/kubernetes/deployment.yaml` — `COOKER_SECRET_KEY` absent from raw manifest.** Binary boots with a zero AES key; every at-rest secret is encrypted with an empty key. *(CR-5)*
6. **WebSocket authorization gap (`server/wsticket.go:41` + `router.go:325-351`).** The ticket subject is set into context but **never read** by any WS handler, and the `/ws` group has no `RequireRole` — any authenticated user (incl. `viewer`) can stream any run/build/pod logs and watch arbitrary namespaces, bypassing the HTTP-layer IDOR and role checks. *(CR-6 / SEC-H1+H2)*

Most of the rest are safe, mechanical fixes (missing timeouts, client reuse, goroutine-shutdown hooks, unbounded reads). The two that need a design call before touching are CR-6 (WS authz model) and CR-2 (Vault path model) — flagged in §Remediation.

---

## Severity index

| ID | Sev | Area | File:line | One-liner |
|----|-----|------|-----------|-----------|
| CR-1 | **CRIT** | backend-core | `server/wshub_backend.go:80` | `memoryHubBackend.Publish` blocking send deadlocks executor on full buffer |
| CR-2 | **CRIT** | secrets | `secrets/vault/vault.go:107-126` | Vault `Put`/`Delete` read-modify-write race, no CAS → silent secret loss |
| CR-3 | **CRIT** | builder | `builder/kaniko.go:144`, `buildah.go:120` | `streamLogs` goroutine + kube conn leaked per build |
| CR-4 | **CRIT** | infra | `deploy/kubernetes/deployment.yaml:94` | Raw probes hit `/health` w/ `initialDelaySeconds:10` (parity "closed" but unapplied) |
| CR-5 | **CRIT** | infra | `deploy/kubernetes/deployment.yaml` | `COOKER_SECRET_KEY` absent → zero AES key in raw-manifest deploys |
| CR-6 | **CRIT** | security | `server/wsticket.go:41`, `router.go:325-351` | WS streams do no authz beyond "authenticated"; ticket subject discarded |
| BC-2 | **CRIT** | backend-core | `handler/app.go:244,641` | Nested 30-min timeout overrides `COOKER_RUN_DEADLINE` on app deploy/rollback |
| BC-H2 | HIGH | backend-core | `handler/pipeline.go:261` | `UpdatePipeline` doesn't map `ErrConflict`→409; silent empty 200 |
| BC-H4 | HIGH | backend-core | `handler/environment.go:253`, `:136` | `DeleteSecret`/`PutSecret` leak backend URLs/ARNs via `err.Error()` |
| BC-H5 | HIGH | backend-core | `handler/app.go:250` | `Deploy`→`(nil,nil,err)` leaves stub run stuck `Running` |
| BC-H3 | HIGH | backend-core | `server/wshub_logs.go:71` | `h.register<-client` blocks on hub exit; wedges HTTP drain on shutdown |
| BC-H1 | HIGH | backend-core | `server/ratelimit.go:54` | `rateLimiter.gc` goroutine has no shutdown hook |
| DA-H1 | HIGH | store | `store/memory/memory.go:143` | `runs.Get` returns live pointer; races heartbeat ticker + executor |
| DA-H2 | HIGH | store | `store/postgres/promotion.go:124`, `stageapproval.go:121` | INSERT+COUNT not atomic; concurrent approvals can stall a gate forever |
| DA-H3 | HIGH | store | `store/postgres/promotion.go:95`, `stageapproval.go:92` | N+1 on `ListPromotions`/`ListGates` |
| AD-H1 | HIGH | deploytarget | `deploytarget/ecs/ecs.go:54` | New AWS client (IMDS roundtrip) every Deploy/Status/Rollback |
| AD-H2 | HIGH | deploytarget | `deploytarget/cloudrun/cloudrun.go:51,101,127` | New gRPC clients every call; up to 4 per Rollback |
| AD-H3 | HIGH | deploytarget | `flyio/flyio.go:72`, `render/render.go:70` | `io.ReadAll` on API responses with no size cap |
| AD-H4 | HIGH | secrets | `secrets/keepsave/keepsave.go:42-69` | `Get`/`Put` list all secrets per call (N+1 HTTP) |
| AD-H5 | HIGH | deploytarget | `deploytarget/ssh/ssh.go:207-229` | Cached SSH client never health-checked; dead conn fails forever |
| AD-H6 | HIGH | builder | `builder/buildah.go:331` | Buildah log stream doesn't strip ANSI (Kaniko does) |
| IN-H1..H6 | HIGH | infra | various | Raw-manifest env-var/PDB/grace gaps; `:latest` image pins; no startupProbe |
| FE-H1 | HIGH | frontend | `hooks/usePipelineExecution.ts:6` | Full-store subscription → stale `nodes` snapshot in WS handler |
| FE-H2 | HIGH | frontend | `pages/RunPage.tsx:152` | `run` in dep array restarts approval poll on every `getRun` |
| FE-H3 | HIGH | frontend | `hooks/useWebSocket.ts:41,150` | 401 on ticket fetch silently backoffs, never `triggerSignIn` |
| SEC-M1 | MED | security | `config/config.go:510` | Prod with OIDC+local both off only *warns*; boots unauthenticated admin-as-everyone |

Full MEDIUM/LOW catalogue per surface is in the appendices below.

---

## Cross-cutting theme: the in-memory state SPOF surface

Three independent agents converged on the same structural risk: **per-process in-memory state that is correct on one replica but a data-loss / availability SPOF the moment you scale out or restart.** This is partially gated (`Config.Validate()` blocks multi-replica unless Redis backends are selected — `config.go:542-558`), but the gating has holes:

- **CR-5 / IN-H1:** the raw K8s manifests omit `COOKER_ENV` and the WS/rate-limit backend vars entirely, so a raw-manifest operator silently gets dev defaults + per-process state with no validation firing.
- **CR-1 / BC-L1:** the in-memory hub backend both deadlocks on overflow (CR-1) and never exits its `Run()` goroutine on clean shutdown (`wshub_backend.go:86` no-op `Close()`).
- **DA-H1 / DA-L1:** the memory store returns live pointers (only `apiTokens` clones), so the "dev/test only" store has real data races under the heartbeat ticker.

**Recommendation:** treat the memory backends as strictly dev/test, make `Config.Validate()` the single chokepoint that the raw manifests cannot bypass (give them production env defaults — IN-H1), and fix the two memory-backend correctness bugs (CR-1, BC-L1) so the dev loop stays race-clean under `-race`.

---

## Appendix A — Backend core (`handler`/`service`/`server`/`cmd`)

CRITICAL: CR-1 (`wshub_backend.go:80`), BC-2 (`app.go:244,641` nested deadline overrides `COOKER_RUN_DEADLINE`).

HIGH: BC-H1 `ratelimit.go:54` (gc no shutdown hook) · BC-H2 `pipeline.go:261` (no `ErrConflict`→409) · BC-H3 `wshub_logs.go:71` (`register` blocks on hub exit) · BC-H4 `environment.go:253,136` (secret-backend error leak) · BC-H5 `app.go:250` (stub run stuck `Running`).

MEDIUM: `pipeline.go:499` `GetRunDiff` unbounded `List(0,0)` for last-success · `ratelimit.go:142` `SetTrustedProxies` not set (XFF rate-limit bypass on IP fallback) · `middleware_audit.go:40` `Path=""` events from SPA fallback · `middleware_idempotency.go:68` `captureWriter` misses `WriteHeaderNow()` → idempotency cache never populates for `c.JSON` · `wshub_logs.go:78` replay uses `r.Context()` (cancels mid-replay) · `runs.go:148` `RunCoordinator.Wait` leaks goroutine on drain-timeout.

LOW: `wshub_backend.go:86` memory `Close()` no-op leaks `Run()` goroutine · `server.go:614` `Close()` skips `auditSweepCancel`/`healthCancel` · `pipeline.go:257` pipeline OCC version not read from existing · `wshub_logs.go:97` long replay can fill `client.send` then drop+close · `pipeline.go:270` Delete/Update/Cancel return 200 on non-ErrNotFound store errors.

**Verified-fixed (prior audits):** WS double-close `sync.Once`, map-mutation-under-RLock two-pass, heartbeat join, WS read deadline+ping/pong, webhook body limit, runId IDOR `loadRunForPipeline`, stage-type/repo/branch validation, security headers+CSP, health-probe panic recover, redis/wsHub close registration, bounded DAG fan-out, async audit sink. (~17 items confirmed in place.)

## Appendix B — Persistence (`store`)

HIGH: DA-H1 `memory/memory.go:143` (live-pointer race) · DA-H2 `postgres/promotion.go:124` + `stageapproval.go:121` (non-atomic INSERT+COUNT) · DA-H3 `postgres/promotion.go:95` + `stageapproval.go:92` (N+1).

MEDIUM: `postgres/promotion.go:51` / `stageapproval.go:49` two-phase SELECT TOCTOU · `memory/memory.go:783` `DeleteOlderThan` retains backing array (~5 MB pin) · `migrations/015_pipeline_run_actor.up.sql:10` missing `ADD COLUMN IF NOT EXISTS` · `postgres/store.go:101` `SetConnMaxIdleTime` never set (LB kills idle conns → TCP-reset 500s).

LOW: non-apiToken memory `Get` return raw pointers · `postgres/audit_event.go:75` dynamic LIMIT/OFFSET index arithmetic trap · `memory/memory.go:275` O(n) `findByRunEnvLocked`.

**Already catalogued (still open):** F-01 `UserStore.Create` no `ErrConflict`, F-05 `HealthStatus` default parity, B.2 `apps(github_repo,branch)` not UNIQUE, B.3 `apps.environment_id` no FK, B.6 `RunStore.Update` re-marshals all JSONB. **Closed & verified:** schema_migrations table, down-migrations 002-009.

## Appendix C — Pluggable backends

CRITICAL: CR-2 (Vault RMW race), CR-3 (Kaniko/Buildah `streamLogs` leak).

HIGH: AD-H1 ECS client per-call · AD-H2 Cloud Run gRPC per-call · AD-H3 Fly/Render unbounded `io.ReadAll` · AD-H4 KeepSave list-per-op N+1 · AD-H5 SSH dead-conn cache · AD-H6 Buildah ANSI not stripped.

MEDIUM: `gitops/gogit.go:86,138` no per-call git deadline · `pusher/crane.go:82` post-push digest can return empty silently · `vault/vault.go` two serialised calls no per-call timeout · `ecs/ecs.go:193` rollback by ARN string-decrement (skips deleted revisions) · `buildkit.go:42` new gRPC conn per Build · `gcpsm/gcpsm.go:128` lists all project secrets client-side (set `PageSize`) · `clientgo.go:138` swallows `AlreadyExists` on server-side apply · `render/render.go:143` two API calls per health tick, no service-id cache · `ssh/ssh.go:373` no per-command timeout (`docker pull` can hang).

LOW: `crane.go:38` auth "no creds" vs error not distinguished · `vault/vault.go:39` no response-body cap · `dockerrun.go:63` swallows `docker rm` error.

**Verified-fixed:** BuildKit log-drain leak, K8s Job poller uses `context.Background()` for delete, Kaniko/Buildah scanner buffer cap.

## Appendix D — Frontend

HIGH: FE-H1 `usePipelineExecution.ts:6` (full-store sub, stale nodes — but currently dead code, FE-L5) · FE-H2 `RunPage.tsx:152` (`run` in dep array restarts poll) · FE-H3 `useWebSocket.ts:41` (401 silent backoff, no `triggerSignIn`).

MEDIUM: `useKubeWatch.ts:5` onEvent dep churn · `useWebSocket.ts:228` scheduleReconnect deps · `useRuntimeLogs.ts:46` inline onMessage + `concat`/msg · `SettingsPage.tsx:528` `setTimeout` no cleanup · `AppDetailPage.tsx:143` orphaned `setTimeout` · `useWebSocket.ts:41` raw fetch outside `api/` [FH-02].

LOW: `uiStore.ts:19` persist→localStorage implicit [FH-01] · `RunPage.tsx:92` env poll never stops at terminal · `AppDetailPage.tsx:92` latent reconnect spinner-clear · `RuntimeInfoPanel.tsx:39` non-`window.` interval · `usePipelineExecution.ts` dead code · `OIDCProvider.tsx:211` stale local token + no OIDC session → `isLoading` deadlock.

PERF: `useRuntimeLogs.ts:49` array alloc per msg (adopt `useStageLogs` ring) · `RunPage.tsx:63` `join('\n')` O(N)/msg · `RuntimeInfoPanel.tsx:113` 5000 DOM nodes, `key={i}` (virtualise).

## Appendix E — Deploy / Infra (SPOF lens)

CRITICAL: CR-4 (raw probes `/health`+`initialDelaySeconds:10`, parity mis-closed), CR-5 (`COOKER_SECRET_KEY` absent from raw manifest).

HIGH: IN-H1 raw manifest missing `COOKER_ENV` + WS/RL backend vars (silent dev defaults) [F-02] · IN-H2 `terminationGracePeriodSeconds` absent from raw manifest [F-07] · IN-H3 no PDB/HPA in raw manifests · IN-H4 `image.tag: latest` chart default + raw manifest · IN-H5 no `startupProbe` · IN-H6 Kaniko/Buildah images `:latest`.

MEDIUM: raw manifest missing `COOKER_BUILDER`/`COOKER_SECRETS_BACKEND` [F-05/06] · OIDC block + client-secret `secretKeyRef` absent from raw manifest [F-03] · HPA-enabled-without-PDB not guarded · **retention CronJob has `readOnlyRootFilesystem:true` but no `/tmp` emptyDir → `psql` fails at runtime** · **NetworkPolicy K8s-API egress targets `default` namespace, not control-plane → breaks in-cluster client-go** · UAT postgres password `cooker` undocumented as dev-only · `alpine:3.19` not digest-pinned [R9] · dev compose binds docker.sock, no limits, postgres on `0.0.0.0` · no Redis manifest in `deploy/kubernetes/` despite `REDIS_URL`.

LOW: raw ingress WS annotations absent from chart default [F-08] · **`docker-compose.yml` references non-existent `Dockerfile.frontend`/`.backend` → `make dev` fails** · HPA CPU-only (memory is OOM vector) · `DATABASE_URL` bypasses `sslmode` on raw path [F-09] · Dockerfile `kubectl` hardcoded `amd64` (breaks multi-arch).

## Appendix F — Security cross-cut

CRITICAL: CR-6 (WS authz gap — ticket subject discarded; `/ws` group unroled).

HIGH (sub-findings of CR-6): SEC-H1 `wsticket.go:41` ws-subject set, never read → any authenticated user streams any run/build/pod logs (incl. echoed secrets) · SEC-H2 `router.go:340,349` `/ws/kubernetes/watch` + `/ws/runtime/.../logs` reachable by `viewer` though HTTP equivalents are operator-gated.

MEDIUM: SEC-M1 `config/config.go:510` prod no-auth only warns (admin-as-everyone if both OIDC+local off) · SEC-M2 `middleware_security.go:34` CSP `connect-src 'self'` vs SECURITY.md `'self' wss:` drift.

LOW: `keepsave/client.go:170` raw upstream body in error message.

**Verified-sound:** CORS (no Allow-Credentials, `*` rejected by Validate), OIDC verifier (lock-free JWKS cache, generic errors), local issuer HS256 alg pin, RBAC default-deny matrix, audit body-free redaction. The `ip:` rate-limit fallback is effectively dead code (limiter mounts only behind `writeRole`) — **not** a memory-exhaustion vector.

---

## Remediation sequencing

**Tier 0 — needs a design decision before code (flagged for sign-off):**
- **CR-6 (WS authz):** bind role + resource into the ticket at mint time and enforce per-route in `wsTicketGate`, then update SECURITY.md. Touches the auth model — not auto-applied.
- **CR-2 (Vault RMW race):** correct fix is per-key Vault paths (vs one path per env) or CAS with retry; the per-key change alters the on-Vault layout. Not auto-applied.

**Tier 1 — safe, mechanical, landing in this PR series:** CR-1, CR-3, CR-4, CR-5, BC-2, BC-H1/H4/H5, DA-H1, DA-H2, DA-H3 (batch query), AD-H1/H2/H3/H4/H5/H6, IN-H1..H6, FE-H2/H3, plus the bulk of MEDIUM/LOW timeouts, client-reuse, goroutine-shutdown hooks, and unbounded-read caps. These are behaviour-preserving or strictly-more-correct.

**Tier 2 — quality/perf, opportunistic:** frontend virtualisation (FE-PERF-02/03), memory-store clone convention (DA-L1), GCP/Render caching, dead-code removal (FE-L5).
