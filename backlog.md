# Cooker Backlog

Tracks work that's been planned, scoped, or hinted at by the codebase but isn't shipped yet. Living document — when an item lands on `main`, remove it (or move it to the changelog).

Items are grouped by area and roughly prioritized within each group.

---

## Production readiness summary

After the `claude/uat-ready-*` PR series merged, Cooker is **production-quality, not yet production-default.** The dangerous defaults are gone — but a usable production deployment depends on operator decisions that the chart can't make for you.

### Deployment-shape readiness matrix

| Shape | Verdict |
|---|---|
| **Single-replica + TLS ingress + Kaniko + Postgres SSL + edge rate limit** | ✅ Production-ready. Ship it. |
| **Single-replica + TLS ingress + still using `docker.sock`** | ⚠️ Functionally works, but if the host runs other workloads, an RCE in Cooker compromises them all. |
| **Multi-replica + sticky sessions + TLS + Kaniko** | ✅ Production-ready for HA. |
| **Multi-replica without sticky sessions or Redis-backed limiter/tickets** | ❌ Will break: users get random 401s on WS reconnect, rate limits don't enforce correctly. |
| **Anything without TLS + OIDC** | ❌ Sign-in flow won't work — most IdPs refuse non-HTTPS redirect URIs. |

### Why "not production-default"

Five operator-side concerns block a flat "yes" on production-readiness, in priority order:

1. **TLS at ingress** is required for OIDC. Cooker doesn't terminate TLS itself.
2. **`/var/run/docker.sock` bind-mount** still gives Cooker root-equivalent access to the host's Docker daemon, even with the container running as UID 65532. An RCE in Cooker → full host control. Fix: switch to **Kaniko** (in-cluster, rootless image builder); `builder.Builder` interface makes this clean. Detailed below.
3. **Multi-replica state** — rate limiter and WebSocket ticket store are per-process. Either pin sticky sessions at ingress (works fine) or implement Redis-backed versions (P3).
4. **Postgres SSL** — default `DATABASE_URL` doesn't set `sslmode=require`. Operator override.
5. **Audit logging** — no implementation. Mutations log to stdout but aren't structured for SIEM. Detailed below.

### What "OCI compliance" means here

`README.md` and `architecture.md` claim conformance with all three OCI specs. The code uses OCI-compliant libraries (`go-containerregistry`, the official image-spec types) and produces OCI-compliant images, but **nothing has been formally tested against the [OCI distribution-spec conformance suite](https://github.com/opencontainers/distribution-spec/tree/main/conformance).** Until that runs, "OCI compliant" is a documented claim, not a certified one.

---

## P1 — Production hardening (operator-side)

These can't ship inside the chart because they depend on the operator's environment.

### ⭐ P1.1 — Kaniko builder adapter (closes the docker.sock RCE-to-host gap)

**Why this is P1, not P3:** PR E made the container run as non-root (UID 65532), but the Helm chart (`deploy/helm/cooker/templates/deployment.yaml`) and UAT compose still bind-mount `/var/run/docker.sock`. The Docker daemon runs as root on the host. Anyone who gains code execution inside the Cooker container — through a vulnerability in dependencies, a leaked credential, a compromised webhook handler — can issue Docker API calls and run arbitrary containers as root on the host node. **The non-root container does not mitigate this**; it only protects the container's own filesystem. SECURITY.md acknowledges this gap; closing it requires not using `docker.sock` at all.

**Approach: in-cluster Kaniko.** [Kaniko](https://github.com/GoogleContainerTools/kaniko) builds OCI images **inside a container, without needing privileged access or a Docker daemon**. It reads the Dockerfile, executes the layers in user-space, and pushes directly to a registry. Standard CNCF pattern for in-cluster image builds.

**Files to add:**
- `backend/internal/builder/kaniko.go` — implements `builder.Builder` interface (defined at `backend/internal/builder/builder.go:49`).
- `backend/internal/builder/kaniko_test.go` — contract test against the `Builder` interface (smoke + error-path).

**Files to modify:**
- `backend/internal/server/server.go:152-161` — extend `selectBuilder` switch to handle `case "kaniko"`.
- `backend/internal/config/config.go:23` — keep doc-string in sync (`"noop" | "docker" | "buildkit" | "kaniko"`).
- `deploy/helm/cooker/values.yaml` — new `builder.kind: docker|buildkit|kaniko` value, default `kaniko` for production. New `builder.kaniko.{image, registryAuthSecret, cachingEnabled}` sub-values.
- `deploy/helm/cooker/templates/deployment.yaml` — when `builder.kind=kaniko`, **drop the docker.sock volume mount entirely** and remove the `group_add` requirement. Conditional logic in the existing `{{- if .Values.docker.enabled }}` block.
- `deploy/helm/cooker/templates/rbac.yaml` — Cooker's ServiceAccount needs `pods` create/get/delete (or `Job` resources) in its own namespace if it spawns Kaniko pods directly, OR `secrets get` if Cooker passes registry credentials to a kaniko pod template. Decide one of the two patterns below.
- `SECURITY.md` Docker Socket Security section — mark Kaniko as the default in the chart, leave docker.sock as a UAT/dev fallback.
- `docs/architecture.md` builder section — add Kaniko as a first-class adapter.
- `backlog.md` — close this item; add to "Closed" log.

**Two Kaniko integration patterns — pick one:**

| Pattern | Pros | Cons |
|---|---|---|
| **(a) Cooker spawns Kaniko Pods via client-go.** Cooker creates a `Job` per build with the Kaniko image, mounts the source as an emptyDir, points at the registry. Cooker watches the Job to completion, streams logs via `kubectl logs -f`. | Cooker owns the lifecycle; cleanup is straightforward. Build isolation per-pod. | Requires `Job` create/get/delete RBAC in the namespace. Latency per build (pod startup ~5s). |
| **(b) Cooker calls a Tekton / Argo Workflows pipeline.** Cooker just submits a CR and watches its status. Build happens in a fully separate operator's domain. | Industry-standard pattern; reuses existing CI/CD operator if installed. | Adds a hard dependency on Tekton or Argo. Operator must install one. |

Recommend **(a)** for the first iteration — fewer external dependencies, fits the existing strategy-pattern. Add (b) as a separate adapter later if there's demand.

**Implementation outline (~half a day):**
```go
// backend/internal/builder/kaniko.go
type Kaniko struct {
    Client      kubernetes.Interface
    Namespace   string  // where to spawn build pods
    Image       string  // gcr.io/kaniko-project/executor:v1.21.0
    PullSecrets []string
}

func (k *Kaniko) Build(ctx context.Context, spec builder.Spec, log io.Writer) (builder.Result, error) {
    job := buildJob(spec, k.Image, k.Namespace, k.PullSecrets)
    created, err := k.Client.BatchV1().Jobs(k.Namespace).Create(ctx, job, metav1.CreateOptions{})
    if err != nil { return builder.Result{}, err }
    defer cleanup(ctx, k.Client, created)
    return waitAndStreamLogs(ctx, k.Client, created, log)
}
```

**Verification:**
- Unit: contract test against the existing `TestBuilder*` patterns in `builder_test.go`.
- E2E: extend `make uat-up` (or a new `make uat-up-kaniko`) to use Kaniko instead of docker, and re-run the Apps Deploy test from `docs/UAT.md` Scenario 1.
- Helm: `helm template --set builder.kind=kaniko` produces a Deployment with **no** docker.sock volume.
- Manual: run a build, confirm the resulting image works (`crane manifest registry/cooker/demo:tag`).

**Risk:** medium. The biggest risk is Kaniko pod RBAC — if the namespace is restricted, the operator needs to widen it. Document the minimal RBAC clearly. UAT compose can keep using docker (faster local iteration) while production defaults to Kaniko.

---

### ⭐ P1.2 — Audit logging middleware (slog-based, hooks `RequireRole`)

**Why this is P1:** Cooker is a CI/CD tool. The audit trail of "who deployed what when" is **the** thing operators need during incidents. Today mutations log to stdout via the standard `log` package — non-structured, not parseable, missing the actor. SECURITY.md production checklist item "Set up audit logging" is unchecked; this is the implementation.

**Scope:** structured audit log entries for **authenticated mutating actions only**. Read endpoints don't need audit logging (would 10x the volume for no security value). Specifically:

| Endpoint | Why audit |
|---|---|
| `POST /api/v1/pipelines` and `PUT /:id` | Pipeline definition changes |
| `DELETE /api/v1/pipelines/:id` | Destructive |
| `POST /api/v1/pipelines/:id/run` | Triggers builds and deploys |
| `POST /api/v1/pipelines/:id/runs/:runId/cancel` | Aborts in-flight runs |
| `POST /api/v1/pipelines/:id/runs/:runId/promote` | Cross-environment promotion |
| `POST /api/v1/pipelines/:id/runs/:runId/approve` | Approval gate decisions |
| `POST /api/v1/apps` and `PUT /:id` | App definition changes |
| `DELETE /api/v1/apps/:id` | Destructive |
| `POST /api/v1/apps/:id/deploy` | Live deployment |
| `PUT /api/v1/apps/:id/webhook` | Credential rotation |
| `POST /api/v1/environments` and `PUT/DELETE` | Environment + variable changes |
| `PUT /api/v1/environments/:id/secrets/:key` | Secret writes |
| `GET /api/v1/environments/:id/secrets/:key` | **Reveals**, even though it's a GET |
| `DELETE /api/v1/environments/:id/secrets/:key` | Secret deletion |
| `POST /api/v1/settings/registries` and `DELETE` | Cross-tenant impact |
| `POST /api/v1/settings/clusters` | Cross-tenant impact |

**Schema (one JSON line per event):**
```json
{
  "ts":"2026-05-02T15:23:01.123Z",
  "actor":{"sub":"abc-123","email":"alice@example.com","roles":["operator"]},
  "action":"pipeline.run",
  "target":{"kind":"pipeline","id":"pl_42","extra":{"runId":"r_99"}},
  "result":"success",
  "request_id":"req_xyz",
  "remote_ip":"10.0.0.5",
  "duration_ms":142
}
```

**Files to add:**
- `backend/internal/audit/audit.go` — `Logger` type wrapping `*slog.Logger` with `LogAction(ctx, Event)`. Convenience helpers per action category.
- `backend/internal/audit/audit_test.go` — verify JSON shape, redaction (no token bodies, no secret values), required fields.
- `backend/internal/server/middleware_audit.go` — Gin middleware that wraps an audit-eligible handler: capture user from context, capture request URI for action mapping, capture status, emit event after handler completes.

**Files to modify:**
- `backend/cmd/cooker/main.go` — initialize `slog.New(slog.NewJSONHandler(os.Stdout, ...))` and wire into `server.New(cfg, auditLogger)`.
- `backend/internal/server/server.go` — accept `*audit.Logger`; pass into router setup.
- `backend/internal/server/router.go` — apply the audit middleware to each of the routes listed above. Stays explicit (per-route, not blanket) so the audit surface is reviewable in the router.
- `backend/internal/handler/environment.go` — `RevealSecret` already has admin gating; audit must include the *key* but **never** the *value*.
- `SECURITY.md` — update the "Set up audit logging" checklist item, document the schema and the redaction guarantees.
- `backlog.md` — close this item.

**Configuration:**
- `COOKER_AUDIT_ENABLED` (default `true` in production, `false` in dev/uat).
- `COOKER_AUDIT_DESTINATION` (default `stdout`; future: `file:/var/log/cooker/audit.jsonl`, `syslog://...`, etc.).

**Redaction rules (codified in tests):**
- Secret values never appear (only key names).
- OIDC tokens (raw JWT) never appear (only the `sub` claim).
- Request bodies that may contain credentials (`PUT /environments/:id/secrets/:key`, `PUT /apps/:id/webhook`) are not logged in the `extra` field.

**Implementation outline (~2 hours):**
```go
// backend/internal/audit/audit.go
type Event struct {
    Action    string                 `json:"action"`
    Target    Target                 `json:"target"`
    Result    string                 `json:"result"`
    Extra     map[string]any         `json:"extra,omitempty"`
}
type Target struct{ Kind, ID string; Extra map[string]any `json:",omitempty"` }

type Logger struct{ s *slog.Logger }

func (l *Logger) Log(ctx context.Context, e Event) {
    actor := auth.GetUser(ctx)  // *Claims or nil
    l.s.LogAttrs(ctx, slog.LevelInfo, "audit",
        slog.String("action", e.Action),
        slog.Any("actor", actor),
        slog.Any("target", e.Target),
        slog.String("result", e.Result),
        ...
    )
}
```

```go
// backend/internal/server/middleware_audit.go
func auditAction(logger *audit.Logger, action string) gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()
        c.Next()
        result := "success"
        if c.Writer.Status() >= 400 { result = "denied" }
        logger.Log(c.Request.Context(), audit.Event{
            Action: action,
            Target: audit.Target{Kind: kindFromRoute(c), ID: c.Param("id")},
            Result: result,
            Extra:  map[string]any{"duration_ms": time.Since(start).Milliseconds()},
        })
    }
}
```

**Verification:**
- Unit: emit one event per audit-eligible action, assert JSON shape and required fields.
- Unit: secret reveal logs the *key* but not the *value* — assert via golden test.
- Manual: trigger a deploy, confirm one `audit` line on stdout with the right shape; pipe into `jq` for sanity.

**Risk:** low. Pure addition; no behavior change. The biggest risk is volume — if a user deploys 1000 apps in a script, the audit log grows linearly. Mitigation: operators should ship logs to a real backend (Loki, Datadog, etc.) and rotate stdout; sampling is **not** appropriate for audit logs.

---

### P1.3 — TLS termination at ingress

Cooker doesn't terminate TLS itself; the ingress controller (or cloud LB) must. Tasks:

- Set `ingress.tls` in `deploy/helm/cooker/values.yaml` to a sane example (`secretName: cooker-tls`).
- Document cert-manager + Let's Encrypt in the README's Helm install example.
- SECURITY.md production checklist already references this; add the explicit `--set ingress.tls[0].secretName=...` line to the install snippet.

### P1.4 — PostgreSQL SSL

- Document the `?sslmode=require` flag (or `verify-full` for proper cert validation) in the README and `docs/UAT.md`.
- Add a Helm value `postgresql.sslMode` so the chart can render the `DATABASE_URL` query string for the operator.
- For the bundled `bitnami/postgresql` subchart (if used), also flip `tls.enabled=true`.

### P1.5 — Base image / dependency rolling updates

- Add a Renovate or Dependabot config covering: `backend/go.mod`, `frontend/package.json`, the `KUBECTL_VERSION` ARG in `deploy/docker/Dockerfile`, the Alpine base, the Helm `appVersion`, the Postgres + Redis chart versions.
- Auto-merge minor/patch on green CI; major bumps need a human PR review.

---

## P2 — Secrets manager integration (planned)

- [ ] **PR G — KeepSave secrets manager.** **Backlog.** Will be planned in a separate session once the user walks through KeepSave's API. The integration is purely additive: define a `secrets.Manager` interface (`Get`, `Put`, `Delete`, `List`), add `internal/secrets/keepsave/` adapter, and switch `internal/handler/environment.go` to call it. The DB-backed encrypted column keeps working as the default and as a fallback. Helm values: `secrets.backend: keepsave|database`, `secrets.keepsave.{url,authMode,...}`.
- [ ] **HashiCorp Vault adapter.** Same `secrets.Manager` interface, second adapter. Pulls via Vault Agent injector pattern.
- [ ] **AWS Secrets Manager / GCP Secret Manager adapters.** Cloud-native deployments.

## P3 — Auth and authorization extensions

- [ ] **Sticky sessions documentation for WebSocket tickets.** PR F's ticket store is per-process. Multi-replica deployments need either sticky sessions at the ingress or a Redis-backed ticket store. Document the recommended ingress annotations (`nginx.ingress.kubernetes.io/affinity: cookie`, etc.) and the cookie name; later, optionally add the Redis backend.
- [ ] **Distributed rate limiter.** PR H is per-process. For multi-replica, add a Redis-backed `rate.Limiter` (e.g. `go-redis/redis_rate`) selected by config, default off.
- [ ] **MFA / step-up auth at the IdP.** Cooker delegates auth to the IdP, but admin-only operations (`DeleteApp`, `RevealSecret`) could request a step-up via `acr_values=mfa` on the OIDC redirect. Per-route opt-in.
- [ ] **OIDC group-to-role mapping configurable.** Today `MapGroupsToRoles` (`backend/internal/auth/rbac.go:77`) hardcodes `cooker-admins → admin`, etc. Make the mapping a `map[string]string` from `COOKER_OIDC_GROUP_MAP` so deployments can integrate with whatever group naming they have.

## P4 — Observability

- [ ] **Prometheus metrics endpoint.** `/metrics` exposing Gin request counters/latency, executor stage outcomes, WebSocket connection counts, rate-limiter denials. Standard `prometheus/client_golang` instrumentation.
- [ ] **OpenTelemetry traces.** Trace pipeline runs end-to-end (handler → `service.Executor` → builder/pusher/deployer). Wire via `otelgin` middleware and propagate context through DAG runner.
- [ ] **Structured logging.** Migrate from `log` to `log/slog` with a JSON handler in production. Audit logging (P1.2) lands on top.

## P5 — Frontend UX

- [ ] **Theme the sign-in landing page** (the gate inside `frontend/src/auth/ProtectedRoute.tsx`). Currently inline-styled placeholder; user mentioned a Claude-generated design that may apply here.
- [ ] **Loading skeletons** instead of `Loading…` text for auth restoration and protected pages.
- [ ] **Error boundary** at the app root (currently uncaught render errors crash the React tree).
- [ ] **OIDC silent renew UI feedback** when `automaticSilentRenew` fails — surface a "session expired, please sign in again" toast instead of silently kicking to the IdP.
- [ ] **WebSocket auto-reconnect** with backoff on disconnect (PR F's tickets work for one connection; reconnects need fresh tickets — fetch a new ticket on each reconnect attempt).

## P6 — Backend code quality and CI

### ⭐ P6.1 — `helm lint` + `helm template` in CI

**Why:** PR D's chart templates were validated by hand (no `helm` binary in the dev environment). PR C added `oidc-secret.yaml` and `secret-key.yaml` templates with conditional rendering. A typo in `{{- if ... }}` blocks would silently break the chart and not be caught until someone tries to `helm install`.

**Implementation (~10 minutes):**

Add a new job to `.github/workflows/ci.yml`:

```yaml
helm:
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v4
    - uses: azure/setup-helm@v4
      with:
        version: v3.14.0
    - name: helm lint
      run: helm lint deploy/helm/cooker
    - name: helm template (default values)
      run: helm template cooker deploy/helm/cooker > /tmp/default.yaml
    - name: helm template (production-with-oidc)
      run: |
        helm template cooker deploy/helm/cooker \
          --set cookerEnv=production \
          --set 'oidc.allowedOrigins={https://cooker.example.com}' \
          --set oidc.enabled=true \
          --set oidc.issuerUrl=https://accounts.google.com \
          --set oidc.clientId=test-client \
          --set oidc.clientSecretRef.name=cooker-oidc \
          --set secretKey.existingSecret=cooker-secret-key \
          > /tmp/prod.yaml
    - name: kubeval the rendered manifests
      run: |
        wget -q https://github.com/yannh/kubeconform/releases/download/v0.6.4/kubeconform-linux-amd64.tar.gz
        tar -xzf kubeconform-linux-amd64.tar.gz
        ./kubeconform -strict -summary /tmp/default.yaml /tmp/prod.yaml
```

**Verification:** push to a branch; the new `helm` check should appear and pass.

### P6.2 — Other quality items

- [ ] **Replace `panic(...)` in `internal/deploytarget/target.go:86`** with a returned error from `Register()` and a startup-time check. The current `MustRegister`-style is fine but doesn't compose well if we ever want plug-ins to register at runtime.
- [ ] **`golangci-lint` in CI.** `Makefile` already has a `lint-backend` target; CI doesn't invoke it. Add a step (use `golangci/golangci-lint-action@v6`).
- [ ] **`gofmt -l` check in CI.** Catch style drift before merge. One-liner: `test -z "$(gofmt -l . | tee /dev/stderr)"`.
- [ ] **Go version bump to 1.24+.** `golang.org/x/time@v0.5.0` is pinned because newer versions need 1.25. Update `go.mod`, `Dockerfile`, and `.github/workflows/ci.yml` together; also unpin the `x/time` version.
- [ ] **Replace `internal/handler/network.go` and `internal/handler/volume.go` placeholder responses** with real Docker SDK calls. Currently they return mock IDs.

## P7 — UAT and dev experience

- [ ] **`tecnativa/docker-socket-proxy`** as an alternative to `group_add` in `docker-compose.uat.yml`. The `group_add` workaround in PR E auto-detects the host docker GID, but operators on unusual hosts hit the fallback (999). Socket proxy avoids the GID problem entirely and exposes only the Docker API endpoints Cooker actually uses (read, build, push) — finer-grained capability surface than full socket access.
- [ ] **`make uat-up-with-keycloak`** target that adds Keycloak as a compose service and pre-seeds a realm, so testers can exercise the full OIDC flow without an external IdP. Currently testers must use Google OIDC or a self-hosted IdP.
- [ ] **`make test-e2e`** that boots `make uat-up`, runs a deterministic pipeline through the API, and tears down. Currently UAT testing is manual per `docs/UAT.md`.

## P8 — Documentation

- [ ] **OpenAPI / Swagger spec** for `/api/v1`. Manually maintained today as a markdown table in README.md; tools like `swaggo/swag` can generate from Go source comments.
- [ ] **Runbook for incident response.** What to do when a build runs forever, when the DB goes down, when an OIDC issuer is unreachable.
- [ ] **Architecture Decision Records (ADR)** for the bigger decisions (JSONB graph storage, in-memory + Postgres dual store, single-binary deployment). `docs/architecture.md` mentions them in passing; full ADRs would help future contributors.
- [ ] **Run the OCI distribution-spec conformance suite** against Cooker's `/registry` proxy endpoints and publish the result. Until that runs, the "OCI compliant" claim in README is a documented intention, not a certified state.

---

## Closed (recent)

Items that landed in the `claude/uat-ready-*` PR series and PR #6:

- ✅ **OIDC PKCE wiring** (frontend + backend) — PR #6
- ✅ **kubectl SHA verification** in the Dockerfile — PR #6
- ✅ **HEALTHCHECK directive** — PR #6
- ✅ **Redis healthcheck + `service_healthy`** in dev compose — PR #6
- ✅ **`go vet ./...`** in CI — PR #6
- ✅ **eslint flat config** so frontend CI passes — PR #6
- ✅ **CORS hardening + `Allow-Credentials: false`** — PR A (#7)
- ✅ **`COOKER_ENV` foundation** — PR A (#7)
- ✅ **CSRF stance documented** — PR A (#7)
- ✅ **Production startup validation** (`Config.Validate()`) — PR B (#8)
- ✅ **Per-user rate limiting** on expensive endpoints — PR H (#9)
- ✅ **WebSocket single-use ticket auth** — PR F (#10)
- ✅ **Non-root container UID 65532** + UAT `group_add` — PR E (#11)
- ✅ **K8s pod securityContext + NetworkPolicy** (chart + raw manifests) — PR D (#12)
- ✅ **Helm chart OIDC `secretKeyRef` + `cookerEnv`** — PR C (#13)
