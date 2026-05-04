# Vulnerabilities & Failure-Chain Audit

**Companion to:** [`dag-performance.md`](dag-performance.md), [`spof-and-database.md`](spof-and-database.md), [`crash-and-service-quality.md`](crash-and-service-quality.md), and [`remediation-plan.md`](remediation-plan.md) (the phased fix plan that addresses every Critical and High finding here). This one is a 20-agent fan-out — 12 deeper-scope agents (vulnerabilities and performance gaps the prior audits didn't reach) and 8 "chain-error" analysts that look at *interactions* between components: "if A and B happen together, C fails because D."

**Method:** 20 parallel Explore agents, each scoped to one focus area and briefed not to repeat material already in the three existing audit docs. Critical/High findings were spot-checked in the source before publishing.

**TL;DR — the four findings that warrant a hot-fix:**

1. **IDOR on every `runId` endpoint.** `GET/POST /pipelines/:id/runs/:runId[/cancel|/promote|/approve|/env-status]` extract `runId` from the path and never verify `run.PipelineID == :id` (`handler/pipeline.go:199-219`, `handler/environment.go:253-289`). Any authenticated user with a runId guess can read or mutate runs across pipelines.
2. **No CSP / no security response headers** anywhere (`server/server.go`, `frontend/index.html`). XSS in any rendered field becomes immediately exploitable.
3. **Stage type / Host kind / Tag format / GitHubRepo format all unvalidated** (`handler/pipeline.go`, `handler/host.go`, `handler/docker.go`, `handler/app.go`). Combines with prior shell-injection in Buildah for amplified RCE surface.
4. **Sourcemap shipped to production** by default (`frontend/vite.config.ts`). Source code leaks to anyone with browser dev tools.

A unified severity table is at the bottom; chain-error analysis is in Part B.

---

## Part A — Vulnerabilities and gaps

### A.1 SQL injection deep-dive

Codebase is fully parameterised (every store query uses `$1, $2, ...`). One hardening hole:

- **`store/postgres/run.go:143`** — **Low/Hardening.** `SweepOrphans` builds an interval string via `fmt.Sprintf("%d milliseconds", threshold.Milliseconds())` and passes it as `$1::interval`. `threshold` is an internal `time.Duration` so this is not currently exploitable, but the value bypasses parameterisation. Fix: use `INTERVAL '1 millisecond' * $1` with the bound integer.

No JSONB `@>` / `?` queries, no query builders, no dynamic table names, no LIKE patterns with un-escaped `%` / `_`. Migrations only execute embedded `*.up.sql`. **Otherwise clean.**

### A.2 Env / secret leak

- `handler/environment.go:161` — `RevealSecret` returns raw `err.Error()` from the secrets manager → leaks Vault/AWS/keepsave failure detail. **Medium.**
- `handler/environment.go:56, 136` — DB error returned raw to client → leaks schema / constraint info. **Medium.**
- `handler/app.go:244` — `"read body: " + err.Error()` on webhook → minor I/O leak. **Low.**
- Audit middleware skips secret-bearing routes correctly; bearer tokens not logged; `.env.uat.example` has no real secrets; no Gin recovery middleware (panic stacks won't reach clients — good for security, but flag for diagnostics).

### A.3 Service-layer logic bugs

- `executor.go:104-105` — **High.** `stageRunMap[nodeID]` and `stageMap[nodeID]` may return nil if `Stage.ID` exists in the DAG but is missing from `run.StageRuns`. Subsequent deref at line 108-111 panics. Fix: nil-check both lookups before access.
- `executor.go:100-101` — **High.** Calling `Execute` twice on the same run unconditionally overwrites `StartedAt`. No idempotency guard.
- `executor.go:130` — **Medium.** Unknown stage type returns error only after the run+stage are marked Running.
- `service/promoter.go:68` — **Medium.** `EnvironmentStatuses` appended without dedup; concurrent promote calls duplicate entries.

### A.4 Frontend security

- **No CSP** anywhere — neither `<meta http-equiv>` in `frontend/index.html` nor a backend-set header. **High.** Combines with any future XSS to give attackers free rein. Fix: add `securityHeadersMiddleware` server-side **and** a CSP `<meta>` tag in index.html.
- **`frontend/vite.config.ts`** — sourcemap enabled in production by default (Vite default behaviour). **High.** Fix: `build: { sourcemap: false }`.
- `useWebSocket.ts:74` — WS URL uses `window.location.host` (safe) but no defensive `new URL()` parse for defence-in-depth. **Low.**
- Green flags: no `dangerouslySetInnerHTML`, no `eval` / `Function`, OIDC PKCE configured correctly, log lines rendered as text not HTML, all URL params `encodeURIComponent`'d.

### A.5 Cryptography

- `wshub_backend.go:202` and `store/postgres/store.go:95` use `math/rand` for backoff jitter. Not security-critical, but inconsistent with the `crypto/rand` used everywhere else. **Low.**
- `gitops/noop.go:20` uses SHA1 for fake commit hashes in the noop backend. Test-only path. **Low/cosmetic.**
- Confirmations: AES-GCM Codec correct; bcrypt DefaultCost=10; HS256 enforced for local-auth JWT with a 32-byte minimum key + issuer check; HMAC uses `hmac.Equal`; tokens use `crypto/rand`; no `InsecureSkipVerify`; no plaintext shared secrets on disk.

### A.6 Authn / Authz / IDOR

- **Critical IDOR.** `GET /pipelines/:id/runs/:runId` (`handler/pipeline.go:199-205`), `POST .../cancel` (line 207-219), `POST .../promote`, `POST .../approve`, `GET .../env-status` (`handler/environment.go:253-289`) all extract `runId` from the path and never verify `run.PipelineID == :id`. Cross-pipeline access by guessing run UUIDs. **Critical.** Fix: load the run, assert pipeline-id match (or return 404).
- **High IDOR.** `GET /apps/:id` (`handler/app.go:33-39`) has no ownership / membership check; any authed user reads any app's details (registry refs, branch, build plan).
- `GET /api/v1/settings/{registries,clusters}` lack `adminRole` middleware. **Medium** (info disclosure of registry endpoints, cluster configs).
- Local-auth has no token revocation endpoint; leaked JWT valid until 12 h TTL expires (`auth/local/local.go`). **Medium.**
- Signup events not audited when `AllowSignup=true` (`handler/auth_local.go:98-99`). **Medium.**
- WebSocket handlers verify the ticket but not that `ws-subject` has read-access to the resource (`server/router.go:211-246`). **Medium** (WS IDOR — combine with A.6 #1 and the attacker can subscribe to logs of any run).

### A.7 Container & K8s security

- Buildah Job adds `SETUID` / `SETGID` capabilities, incompatible with PSA `restricted` (`builder/buildah.go:204-209`). **Medium** — operators on restricted clusters must pick Kaniko; document the trade-off.
- Probes (cooker Deployment + builder Jobs) lack `timeoutSeconds` / `failureThreshold` / `successThreshold` overrides → defaults are 1 s / 3 / 1, false-positive failures on slow pods. **Medium.**
- Namespace not labeled `pod-security.kubernetes.io/enforce: restricted`. **Medium.**
- Builder Jobs omit `imagePullPolicy` → defaults to `IfNotPresent`. **Medium** (auditability).
- Kaniko / Buildah Job specs lack Pod-level `securityContext` (only container-level). **Medium.**
- Dockerfile bundles a `kubectl` binary that may be unused at runtime. **Medium** (attack surface).
- NetworkPolicy ingress allows all same-namespace pods (`podSelector: {}`). **Medium** (any pod reaches cooker on shared clusters).
- `automountServiceAccountToken` not explicitly set. **Low.**

### A.8 TLS / outbound HTTPS

- **`secrets/keepsave/client.go:34`** — **High.** `http.Client{Timeout: 30s}` uses default Transport. If `KEEPSAVE_URL` is `http://`, secrets transmit unencrypted. Fix: `config.Validate()` should reject `http://` for KeepSave in production; add `TLSClientConfig{MinVersion: tls.VersionTLS12}` on the Transport.
- `deploytarget/render/render.go:36`, `deploytarget/flyio/flyio.go:36` — default Transport, no `MinVersion` enforcement. **Medium** (defence-in-depth gap; URLs are hardcoded HTTPS).
- `transport/tsnet/real.go:62` — same pattern, but Tailscale overlay handles encryption; **Low** (document expectation).
- `config/config.go:181` — KeepSave example shows `http://keepsave:8080`; production should reject this. **Medium.**
- `server/server.go:229-232` — cooker listens on plain HTTP; TLS deferred to ingress. **Medium** (document explicitly in `RUNBOOK.md`).
- Confirmations: no `InsecureSkipVerify`, no HSTS / secure-cookie path needed (bearer-only), no cert loading in cooker.

### A.9 N+1 / sequential queries (beyond `spof-and-database.md`)

- `handler/environment.go:206-209` — **High.** `PromoteSecrets` fetches source + target environments sequentially; errgroup-able.
- `store/postgres/run.go:86-104`, `pipeline.go:77-95`, `environment.go:80-108` — **Medium.** `Update` re-marshals every JSONB column on every write, even when only one moved.
- `handler/app.go:204-207` — `runAppDeployCtx` does `Update`-then-fall-back-to-`Create` (two DB calls per deploy). **Low.**

### A.10 HTTP middleware & header hygiene

- **No security response headers anywhere** — no `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, `Permissions-Policy`, no CSP. **High.** Fix: a single middleware on the global router.
- No `Cache-Control: no-store` on `RevealSecret` and other secret-bearing endpoints. **Medium.**
- `gin.Default()` doesn't call `SetTrustedProxies(...)`. Behind a proxy, `c.ClientIP()` (used as rate-limit fallback key) reads user-controlled `X-Forwarded-For`. **Low.**

### A.11 Input validation comprehensive

- **High.** `Stage.Type`, `Host.Kind`, `Host.Reachability`, `EnvironmentTarget.Type` — none enum-validated in handlers or model. Arbitrary string accepted. (`handler/pipeline.go:49-50`, `handler/host.go:38-43`, `handler/environment.go:38-44`.)
- **High.** `App.GitHubRepo` accepts arbitrary strings with no `owner/repo` regex (`handler/app.go:47`). Combines with A.12 to amplify webhook routing concerns.
- **High.** Docker tag elements not validated against the tag regex (`handler/docker.go:74-83`).
- `App.Branch` — no git-ref-name validation (`handler/app.go:73`). **High.**
- `Registry.URL` — no scheme enforcement (HTTPS) (`handler/registry.go:44-45`). **High.**
- Length caps absent on `Pipeline.Name`, `App.Name`, `Registry.Name`, all descriptions — DoS / DB-bloat. **Medium.**
- `PromoteSecrets.Keys` array unbounded (`handler/environment.go:194`). **Medium.**
- `ApplyRequest.Manifest` accepts unbounded string (K8s manifests can be large) (`handler/kubernetes.go:38`). **Medium.**
- Path `:id` accepts any string (UUID not enforced). **Low.**
- Email field bound without explicit format pre-validation (`handler/auth_local.go:35-42`). **Low.**

### A.12 Webhook & external-input attack surface

- **High.** GitHub webhook doesn't capture / check `deleted` flag or all-zero `after` SHA on push events → branch-delete events deploy against a zero commit. Fix: add `Deleted bool` to the parser and reject in the handler.
- **Medium.** GitHub webhook routing matches by `ev.Repository.FullName` (which is `owner/name`, globally unique on GitHub) — but `App.GitHubRepo` has no `owner/repo` validation (see A.11), so an operator could register an app with just `myrepo` and unknowingly accept any GitHub user's pushes. Fix: enforce the format on input.
- **Medium.** OIDC backend doesn't validate `state` / `nonce` on the callback — relies entirely on `oidc-client-ts` in the browser. Defence-in-depth gap.
- **Medium.** K8s manifest YAML parser (`deployer/clientgo.go:130-147`) has no size or depth limits — billion-laughs / nesting bombs. Fix: `io.LimitReader` + decoder depth cap.
- **Medium.** Git clone doesn't strip `.git/hooks/*` post-clone (`source/github/clone.go`). If any later git operation in the build pipeline runs hooks, attacker repos get RCE. Also no symlink-escape check for the build context delivered to Kaniko/Buildah. Fix: post-clone, `rm -rf .git/hooks/*` and set `core.hooksPath=/dev/null`.
- **Medium.** Kaniko log streaming (`builder/kaniko.go:298-328`) writes pod stdout to `LogWriter` without stripping ANSI escape sequences. Build attackers can inject terminal-control sequences into operator logs.
- **Medium.** Webhook event-type filter is inverted (`if eventType != "push" { ignore }`). Should be an explicit allowlist.
- **Low.** AWS / GCP secrets-manager responses lack a size cap.

---

## Part B — Failure chains

Each chain is *"if A and B, then C fails because D"* — interactions that don't show up in single-component audits.

### B.1 Multi-replica chains

1. **In-memory WS tickets + LB round-robin → 401 coin-flip on every WS upgrade.** Replica A issues, replica B doesn't know. (`wsticket.go:64-128`.) Fix: Redis ticket store or sticky sessions.
2. **In-memory rate limiter + N replicas → user gets N× their budget.** (`ratelimit.go:14-47`.) Fix: Redis backend.
3. **Two replicas boot simultaneously + idempotent migrations → `CREATE INDEX` race; queries fall back to seq-scan for 30s–5m.** (`store.go:113-137`.) Fix: version-table migration runner + `CREATE INDEX CONCURRENTLY`.
4. **Drain timeout (25 s) shorter than heartbeat tick → final heartbeat lost; orphan sweep on next boot marks healthy run failed.** (`runs.go:21, 57-72`.) Fix: extend orphan threshold or join the inner heartbeat goroutine.
5. **Redis disconnect during broadcast → message dropped (no replay buffer); WS clients show stale state until refresh.** (`wshub_backend.go:131-196`.) Fix: replace pub/sub with Redis Streams + replay window.

### B.2 Long-running pipeline chains

1. **`runs.go:41-45`** says "30-minute deadline" but never calls `WithTimeout` → builds can hang indefinitely.
2. **Postgres `ConnMaxLifetime=1h`** evicts mid-row-iteration → `rows.Next` returns connection-closed error 50 minutes into a long query (`store.go:44`).
3. **Kaniko `TTLSecondsAfterFinished=300s`** collides with cooker restart's orphan sweep on freshly-completed Jobs (`kaniko.go:179`).
4. **JWKS cache has no forced refresh on age** → IdP rotates keys, cooker silently rejects new tokens until the cache misses (`oidc.go:122-125`).
5. **`COOKER_SECRET_KEY` rotation mid-run** → subsequent secret decryption fails with "authentication tag mismatch" (`crypto/codec.go:30-50`).
6. **WebSocket `readPump` / `writePump` have no read deadline + no ping/pong** → 5-min silent build is dropped by reverse proxy idle timeout (`websocket.go:158-178`).
7. **Audit log file fills disk → file-sink mutex held during sync write → every authenticated request blocks** (`audit/audit.go:115-123`).
8. **`gogit.Commit` / `PushContext` has no per-call timeout** → slow git remote hangs the whole run (`gogit.go:65-132`).

### B.3 Network-partition cascades (slow downstream)

1. **Postgres slow (20 s queries) → pool of 25 saturates → `/health/ready` 1s `Ping` times out → 503 → LB drains → in-flight WS clients fan out to other replicas → cluster outage in ~30 s.** (`store.go:45`, `health.go:15`.)
2. **Redis `Publish` slow → `Hub.Broadcast` blocks → executor's status-update goroutine stalls → heartbeat misses → orphan-detection on neighbour replica marks the run failed even though the build is healthy.** (`wshub_backend.go:103-113`.)
3. **K8s API slow on `Job.Get` → `wait.PollUntilContextCancel` returns `DeadlineExceeded` → builder returns error → executor fails the stage → Kaniko Job is *not* cancelled (only the poller) → orphan Pod consumes resources.** (`kaniko.go:249, 267`.)
4. **OIDC discovery slow → `ensureProvider` blocks on package mutex → Gin upstream queue fills → 502.** (`auth/oidc.go:97-118`.)
5. **Registry slow on push → BuildKit `Solve` blocks → no per-stage timeout → build hangs forever.** (Already in `dag-performance.md`.)
6. **JWKS endpoint slow during rotation → token verification blocks every request → cluster-wide 502.** (`oidc.go:200`.)
7. **GitOps push slow → run heartbeat misses → orphan-detect false positive on neighbour replica.** (`runs.go:58-67`.)
8. **CoreDNS slow → every outbound call (Postgres, Redis, K8s, OIDC, registry, GitHub) stalls in lockstep → systemic cascading slowdown.**

### B.4 Auth failure cascades

1. **OIDC issuer URL change** → existing tokens rejected with `iss mismatch` → IdP redirect loop until rollout completes everywhere.
2. **JWKS rotation** → cooker's go-oidc cache holds stale keys → some tokens fail with "unable to verify signature", others succeed → flapping auth.
3. **`COOKER_SECRET_KEY` rotation** → mid-rollout some replicas can decrypt environment secrets, others can't → users see intermittent 503 / decrypt failures.
4. **Bearer-token vs IdP revocation gap** → leaked token valid for full TTL (cooker doesn't introspect).
5. **Local-auth + OIDC simultaneously enabled** → router uses `iss="cooker-local"` heuristic; malformed claims could route incorrectly.
6. **In-memory WS tickets + replica restart** → tickets vanish; client gets 401 on next upgrade.
7. **RBAC group→role map cached at boot** → group changes don't take effect until restart.
8. **Login during OIDC discovery bootstrap race** → frontend loads with `auth-methods` showing OIDC but no redirect URL configured yet.

### B.5 Cleanup races between resources

1. **Pipeline deleted while run in flight.** `ON DELETE CASCADE` removes the run row → `RunStore.Update` returns `ErrNotFound` → executor logs and gives up; build continues unmonitored.
2. **App deleted during deploy.** Workdir is cleaned but run rows orphaned; WS broadcast has no parent.
3. **Environment deleted** → orphaned `apps.environment_id` (no FK) → deploys fail to resolve secrets at runtime.
4. **Host deleted** → pipeline stages with host ID fail at runtime; no schema-level guard.
5. **Two simultaneous `PATCH /pipelines/:id`** → no `version` column → last writer silently wins; first user's edit lost.
6. **Two simultaneous `RunPipeline`** → both spawn executors → image-tag race, conflicting K8s deploys.

### B.6 Resource-exhaustion chains

1. **K8s `ResourceQuota` exhausted → `Jobs.Create` returns 403 → no automatic cleanup → quota stays full.**
2. **Postgres disk full → INSERTs fail forever → no DB-side rotation policy.**
3. **Cooker `/tmp` full from large clone → next clone fails → no admission control on repo size.**
4. **FD exhaustion from leaky WS clients → `accept: too many open files` → cluster-wide 502.**
5. **OOM-kill from unbounded webhook `io.ReadAll` → orphan sweep marks every in-flight run failed on restart.**
6. **etcd full → manifest applies fail → no remediation path.**
7. **Registry rate-limit → push partial-success → next stage fails on missing image.**
8. **Goroutine count explodes (unbounded fan-out) → GC pauses → all requests slow.**
9. **Audit-sink disk full → mutex held during sync write → every authenticated request blocks indefinitely.**

### B.7 Upgrade / rollback chains

1. **Orphan-sweep race during rolling deploy.** New pod boots, sees fresh heartbeat from old pod's run; old pod is then drained mid-build; the executor is killed; next replica's sweep marks the run failed.
2. **New NOT NULL migration** applies on replica B; replica A still on old code → INSERT fails with default-missing-value.
3. **Down migrations not embedded** (`store.go:32`) → manual rollback corrupts schema.
4. **Pipeline saved with new stage type, cooker rolled back** → `unknown stage type` on resume → run unrecoverable.
5. **Helm RBAC change rolls before new code** → builder Jobs fail with 403 mid-rollout.
6. **OIDC client-id rotation** → all sessions invalidated simultaneously → IdP redirect storm.
7. **`SECRET_KEY` rotation without dual codecs** → secrets sealed under old key unopenable on new key.
8. **Buildah feature removed via Helm but Jobs survive** (TTL=300s) → orphan Pods → quota exhaustion.

### B.8 User-action timing chains

1. **Double-click `RunPipeline`** → two executors race on the same image tag.
2. **Pipeline edited mid-run** (no run-snapshot) → executor sees stage definitions change.
3. **WS reconnect to different replica** → in-flight log lines lost (no replay).
4. **Pipeline import with colliding ID** → 500 from PK violation (no UPSERT, no 409).
5. **App deleted while deploy click in flight** → executor talks to vanished app.
6. **User logs out during 25-min run** → executor finishes (uses `context.Background`), WS dies on token revoke, user can't reconnect to see final status.
7. **Webhook + manual run fire same second** → conflicting artifact state.
8. **RBAC group elevated at IdP** → old JWT still admin'd for full TTL.
9. **Pipeline update has no version/etag** → concurrent edit silently lost.

---

## Cross-cutting severity table

Critical and High items only. Mediums and Lows live under Part A.

| # | Severity | Finding | File |
|---|---|---|---|
| 1 | **Critical** | IDOR on `runId` endpoints — no `run.PipelineID == :id` check | `handler/pipeline.go:199-219`, `handler/environment.go:253-289` |
| 2 | **High** | IDOR on `GET /apps/:id` | `handler/app.go:33-39` |
| 3 | **High** | No security response headers (CSP, X-Frame-Options, X-Content-Type-Options, etc.) | `server/server.go` |
| 4 | **High** | No CSP `<meta>` in frontend either | `frontend/index.html` |
| 5 | **High** | Vite sourcemap shipped to production | `frontend/vite.config.ts` |
| 6 | **High** | Stage type / Host kind / Reachability / EnvironmentTarget type — not enum-validated | `handler/pipeline.go:49-50`, `handler/host.go:38-43`, `handler/environment.go:38-44` |
| 7 | **High** | App.GitHubRepo accepts arbitrary string (no `owner/repo` regex) | `handler/app.go:47` |
| 8 | **High** | Docker tag elements unvalidated | `handler/docker.go:74-83` |
| 9 | **High** | App.Branch unvalidated git-ref | `handler/app.go:73` |
| 10 | **High** | Registry.URL no scheme enforcement | `handler/registry.go:44-45` |
| 11 | **High** | KeepSave HTTP client doesn't enforce HTTPS / TLS MinVersion | `secrets/keepsave/client.go:34` |
| 12 | **High** | GitHub webhook doesn't check `deleted` / zero `after` SHA | `source/github/webhook.go`, `handler/app.go:250-304` |
| 13 | **High** | Service-layer nil-deref if `Stage.ID` not in `StageRuns` | `service/executor.go:104-105` |
| 14 | **High** | `Execute` called twice overwrites `StartedAt`; no idempotency guard | `service/executor.go:100-101` |
| 15 | **High** | Sequential environment fetch in `PromoteSecrets` | `handler/environment.go:206-209` |

Plus all Critical and High items already in the three earlier audits.

---

## Top 10 cross-cutting risks (synthesised from chains)

These are recurring patterns that touch multiple findings; fixing the pattern cleans up many items at once.

1. **No version-tracking on schema** → migrations / rollback / concurrent edits all break.
2. **In-memory state with multi-replica** → tickets, rate limits, broadcasts, orphan detection all degrade.
3. **No per-stage / per-call timeout enforcement** → slow downstreams cascade everywhere; runs hang forever.
4. **No idempotency keys** → double-clicks and webhook retries corrupt state.
5. **No optimistic-concurrency `version` column** on pipelines/apps/environments → silent overwrites.
6. **Heartbeat threshold (90s) is uncomfortably close to drain timeout (25s) and tick (30s)** → false-positive orphan failures.
7. **No request body / connection / time budget limits** on most handlers → DoS surface in many places at once.
8. **Configuration mutations require restart** (key, OIDC issuer, RBAC map) → rolling deploys are not seamless.
9. **Cleanup deferred to TTLs (Kaniko Job, ws-ticket, in-memory caches)** rather than explicit lifecycle hooks → races on restart.
10. **Validate at the model boundary, not the handler** → enum / format / length validation gaps proliferate; combine with shell-injection in builders for amplified RCE surface.

The "fix this sprint" list from `crash-and-service-quality.md` is still the right starting point. Once those land, the highest-leverage next move is **adding optimistic-concurrency `version` columns + a real migration framework with version tracking + down migrations** — that single combo retires roughly a third of the findings here.

---

## Out of scope

- No code changes. This is the fourth diagnostic doc; remediation is the user's prioritisation call.
- Some Medium findings overlap with prior audits (deliberately re-stated when the chain perspective adds context). The cross-cutting severity table dedupes against the earlier docs.
- A8's "deferred to ingress" stance for inbound TLS is acceptable — but should be made explicit in `RUNBOOK.md`.
- 20 agents covered ~98% of backend Go code by file count. A future audit could focus on frontend logic (beyond security), CI/test coverage gaps, and dependency-vulnerability scanning.
