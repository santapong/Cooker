# Cooker Backlog

Tracks work that's been planned, scoped, or hinted at by the codebase but isn't shipped yet. Living document — when an item lands on `main`, remove it (or move it to the changelog).

Items are grouped by area and roughly prioritized within each group.

---

## Production readiness summary

After the `claude/uat-ready-*` PR series and the `claude/cooker-backlog-readme-com8z` PR (#17) merged, Cooker is **production-quality, not yet production-default.** The dangerous defaults are gone — but a usable production deployment still depends on operator decisions that the chart can't make for you.

### Deployment-shape readiness matrix

| Shape | Verdict |
|---|---|
| **Single-replica + TLS ingress + Kaniko + Postgres SSL + edge rate limit** | ✅ Production-ready. Ship it. |
| **Single-replica + TLS ingress + still using `docker.sock`** | ⚠️ Functionally works, but if the host runs other workloads, an RCE in Cooker compromises them all. |
| **Multi-replica + sticky sessions + TLS + Kaniko** | ✅ Production-ready for HA. See [docs/MULTI_REPLICA.md](docs/MULTI_REPLICA.md). |
| **Multi-replica without sticky sessions or Redis-backed limiter/tickets** | ❌ Will break: users get random 401s on WS reconnect, rate limits don't enforce correctly. |
| **Anything without TLS + OIDC** | ❌ Sign-in flow won't work — most IdPs refuse non-HTTPS redirect URIs. |

### Why "not production-default"

Five operator-side concerns block a flat "yes" on production-readiness, in priority order:

1. **TLS at ingress** is required for OIDC. Cooker doesn't terminate TLS itself.
2. **`/var/run/docker.sock` bind-mount** still gives Cooker root-equivalent access to the host's Docker daemon, even with the container running as UID 65532. An RCE in Cooker → full host control. Fix: switch to **Kaniko** (in-cluster, rootless image builder); `builder.Builder` interface makes this clean. Detailed below.
3. **Multi-replica state** — rate limiter and WebSocket ticket store are per-process. Pin sticky sessions at ingress (works fine; documented in `docs/MULTI_REPLICA.md`) or implement Redis-backed versions (P3).
4. **Postgres SSL** — default `DATABASE_URL` doesn't set `sslmode=require`. Operator override.
5. **Audit logging** — no implementation. Mutations log to stdout but aren't structured for SIEM. Detailed below.

### What "OCI compliance" means here

`README.md` and `architecture.md` claim conformance with all three OCI specs. The code uses OCI-compliant libraries (`go-containerregistry`, the official image-spec types) and produces OCI-compliant images, but **nothing has been formally tested against the [OCI distribution-spec conformance suite](https://github.com/opencontainers/distribution-spec/tree/main/conformance).** Until that runs, "OCI compliant" is a documented claim, not a certified one.

---

## P1 — Production hardening (operator-side)

These can't ship inside the chart because they depend on the operator's environment.

### ⭐ P1.1 — Kaniko builder adapter (closes the docker.sock RCE-to-host gap)

**Why this is P1, not P3:** The Helm chart and UAT compose still bind-mount `/var/run/docker.sock`. The Docker daemon runs as root on the host. Anyone who gains code execution inside the Cooker container can issue Docker API calls and run arbitrary containers as root on the host node. The non-root container does not mitigate this; it only protects the container's own filesystem.

**Approach: in-cluster Kaniko.** [Kaniko](https://github.com/GoogleContainerTools/kaniko) builds OCI images **inside a container, without needing privileged access or a Docker daemon**. Cooker spawns one Kaniko Pod per build via `client-go`, watches it to completion, and streams logs.

**Files to add:**
- `backend/internal/builder/kaniko.go` — implements `builder.Builder` interface.
- `backend/internal/builder/kaniko_test.go` — contract test against a fake Kubernetes client.

**Files to modify:**
- `backend/internal/server/server.go` — extend `selectBuilder` switch with `case "kaniko"`.
- `backend/internal/config/config.go` — keep doc-string in sync (`"noop" | "docker" | "buildkit" | "kaniko"`).
- `deploy/helm/cooker/values.yaml` — new `builder.kind` + `builder.kaniko.{image,registryAuthSecret,cachingEnabled}` block.
- `deploy/helm/cooker/templates/deployment.yaml` — when `builder.kind=kaniko`, drop the docker.sock volume mount entirely.
- `deploy/helm/cooker/templates/rbac.yaml` — Cooker's ServiceAccount needs `Job` create/get/delete in its own namespace.
- `SECURITY.md` Docker Socket Security section — mark Kaniko as the default in the chart, leave docker.sock as a UAT/dev fallback.
- `docs/architecture.md` builder section — add Kaniko as a first-class adapter.

**Risk:** medium. Biggest risk is namespace RBAC — operators on locked-down clusters need to widen it. Document the minimal RBAC clearly. UAT compose can keep using docker (faster local iteration) while production defaults to Kaniko. **Effort:** ~1 day.

---

### ⭐ P1.2 — Audit logging middleware (slog-based)

**Why this is P1:** Cooker is a CI/CD tool. The audit trail of "who deployed what when" is the thing operators need during incidents. Today mutations log to stdout via the standard `log` package — non-structured, not parseable, missing the actor.

**Scope:** structured `slog`-JSON entries for authenticated mutating actions only. Read endpoints don't need audit logging.

**Files to add:**
- `backend/internal/audit/audit.go` — `Logger` type wrapping `*slog.Logger` with `LogAction(ctx, Event)`.
- `backend/internal/audit/audit_test.go` — verify JSON shape, redaction (no token bodies, no secret values).
- `backend/internal/server/middleware_audit.go` — Gin middleware that wraps an audit-eligible handler.

**Files to modify:**
- `backend/cmd/cooker/main.go` — initialize `slog` JSON handler and wire into `server.New(cfg, auditLogger)`.
- `backend/internal/server/server.go` — accept `*audit.Logger`; pass into router setup.
- `backend/internal/server/router.go` — apply the audit middleware per-route (not blanket) so the surface is reviewable.
- `SECURITY.md` — mark "Set up audit logging" complete; document schema and redaction.

**Configuration:**
- `COOKER_AUDIT_ENABLED` (default `true` in production, `false` in dev/uat).
- `COOKER_AUDIT_DESTINATION` (default `stdout`; future: `file:`, `syslog:`).

**Redaction rules:**
- Secret values never appear (only key names).
- OIDC tokens (raw JWT) never appear (only the `sub` claim).
- Request bodies for `PUT /environments/:id/secrets/:key` and `PUT /apps/:id/webhook` are not logged in `extra`.

**Risk:** low. Pure addition; no behavior change. **Effort:** ~2 hours.

---

### P1.3 — TLS termination at ingress

- [x] `ingress.tls` value documented in `deploy/helm/cooker/values.yaml` with cert-manager example.
- [x] Helm install snippet in README sets `ingress.tls[0].secretName=cooker-tls`.
- [x] README §Deployment → TLS at ingress with cert-manager + Let's Encrypt walk-through.
- [ ] SECURITY.md production checklist still references this; align with the README snippet.
- [ ] `deploy/helm/cooker/templates/ingress.yaml` should fail-template when `cookerEnv=production` and `oidc.enabled=true` and `ingress.tls` is empty.

### P1.4 — PostgreSQL SSL

- [x] `?sslmode=require` documented in the README and `values.yaml`.
- [x] `postgresql.sslMode` Helm value with default `require`.
- [ ] Render `?sslmode={{ .Values.postgresql.sslMode }}` into the constructed `DATABASE_URL` in `templates/deployment.yaml`.
- [ ] For the bundled `bitnami/postgresql` subchart, also flip `tls.enabled=true` and pass-through CA bundle config.

### P1.5 — Base image / dependency rolling updates

- [x] `renovate.json` at the repo root: weekly Mon-AM schedule, automerge minor/patch on green CI, major bumps gated on human review, custom regex manager for `KUBECTL_VERSION`.
- [ ] Operator step: enable Renovate / Dependabot on the repo (one-time UI toggle).

---

## P2 — Secrets manager integration

- [x] **P2.1 — KeepSave secrets manager.** **Closed.** `secrets.Manager` interface lives at `backend/internal/secrets/manager.go`; `database` adapter wraps the existing AES-GCM logic and `keepsave` adapter delegates to a KeepSave server over HTTP. See [README §Secrets backends](README.md#secrets-backends) and [ADR-0002](docs/adr/0002-secrets-manager.md). Follow-ups:
  - [ ] Render KeepSave env-vars + `secretKeyRef` in the Helm `deployment.yaml`.
  - [ ] Swap the internal HTTP client for the published Go SDK (currently lacks a `go.mod`).
  - [ ] Surface KeepSave's `/promote` endpoint as a Cooker secret-promotion handler.
- [ ] **HashiCorp Vault adapter.** Same `secrets.Manager` interface, third adapter. Pulls via Vault Agent injector pattern.
- [ ] **AWS Secrets Manager / GCP Secret Manager adapters.** Cloud-native deployments.

## P3 — Auth and authorization extensions

- [x] **Sticky sessions documentation for WebSocket tickets.** `docs/MULTI_REPLICA.md` covers NGINX/ALB/Traefik/HAProxy/Envoy and the failure modes without action.
- [ ] **Distributed rate limiter + Redis-backed WS ticket store.** Per-process today. For multi-replica, add a Redis-backed `rate.Limiter` (`github.com/go-redis/redis_rate/v10`) and SETEX/GETDEL ticket store. Selected by config, default off.
- [ ] **MFA / step-up auth at the IdP.** Cooker delegates auth to the IdP, but admin-only operations could request a step-up via `acr_values=mfa` on the OIDC redirect. Per-route opt-in.
- [ ] **OIDC group-to-role mapping configurable.** Today `MapGroupsToRoles` (`backend/internal/auth/rbac.go:77`) hardcodes `cooker-admins → admin`. Make the mapping a `map[string]string` from `COOKER_OIDC_GROUP_MAP`.

## P4 — Observability

- [ ] **Prometheus metrics endpoint.** `/metrics` exposing Gin request counters/latency, executor stage outcomes, WebSocket connection counts, rate-limiter denials. Standard `prometheus/client_golang` instrumentation. Adds a Go dep — needs a `go.sum` regen step.
- [ ] **OpenTelemetry traces.** Trace pipeline runs end-to-end (handler → `service.Executor` → builder/pusher/deployer). Wire via `otelgin` middleware and propagate context through DAG runner.
- [ ] **Structured logging.** Migrate from `log` to `log/slog` with a JSON handler in production. Audit logging (P1.2) lands on top.

## P5 — Frontend UX

- [ ] **Theme the sign-in landing page** (the gate inside `frontend/src/auth/ProtectedRoute.tsx`). Currently inline-styled placeholder; user has a Claude-generated design that may apply here. **Awaiting design source** (file path / screenshot / external link) before scoping.
- [ ] **Loading skeletons** instead of `Loading…` text for auth restoration and protected pages.
- [x] **Error boundary at the app root.** `frontend/src/components/ErrorBoundary.tsx` wired in `App.tsx`. Catches uncaught render errors with a themed Try-again / Go-home fallback. Optional `fallback` prop for callers that want their own UI.
- [ ] **OIDC silent renew UI feedback** when `automaticSilentRenew` fails — surface a "session expired, please sign in again" toast instead of silently kicking to the IdP.
- [ ] **WebSocket auto-reconnect** with backoff on disconnect (PR F's tickets work for one connection; reconnects need fresh tickets — fetch a new ticket on each reconnect attempt).

## P6 — Backend code quality and CI

- [x] **P6.1 — `helm lint` + `helm template` + `kubeconform` in CI.** Validates default + production-with-OIDC values.
- [x] **`deploytarget.Register` returns error; `MustRegister` for init() callers** — replaces the historical `panic` in `Register`.
- [ ] **`gofmt -l` check in CI.** First attempt surfaced pre-existing formatting drift unrelated to this PR (no local Go toolchain available to fix). Pair this with the next item so a single tuned-config sweep can normalize everything.
- [ ] **`golangci-lint` in CI.** `Makefile` already has a `lint-backend` target; CI doesn't invoke it. Add a step (use `golangci/golangci-lint-action@v6`) and a tuned `.golangci.yml` that excludes generated files and any irrecoverable patterns.
- [ ] **Go version bump to 1.24+.** `golang.org/x/time@v0.5.0` is pinned because newer versions need 1.25. Update `go.mod`, `Dockerfile`, and `.github/workflows/ci.yml` together; also unpin the `x/time` version.
- [ ] **Replace `internal/handler/network.go` and `internal/handler/volume.go` placeholder responses** with real Docker SDK calls. Currently they return mock IDs.

## P7 — UAT and dev experience

- [ ] **`tecnativa/docker-socket-proxy`** as an alternative to `group_add` in `docker-compose.uat.yml`. The `group_add` workaround in PR E auto-detects the host docker GID, but operators on unusual hosts hit the fallback (999). Socket proxy avoids the GID problem entirely and exposes only the Docker API endpoints Cooker actually uses (read, build, push) — finer-grained capability surface than full socket access.
- [ ] **`make uat-up-with-keycloak`** target that adds Keycloak as a compose service and pre-seeds a realm, so testers can exercise the full OIDC flow without an external IdP. Currently testers must use Google OIDC or a self-hosted IdP.
- [ ] **`make test-e2e`** that boots `make uat-up`, runs a deterministic pipeline through the API, and tears down. Currently UAT testing is manual per `docs/UAT.md`.

## P8 — Documentation

- [ ] **OpenAPI / Swagger spec** for `/api/v1`. Manually maintained today as a markdown table in README.md; tools like `swaggo/swag` can generate from Go source comments.
- [x] **Runbook for incident response** — `docs/RUNBOOK.md` covers hung builds, Postgres down, OIDC unreachable, KeepSave outage, OOMKilled.
- [x] **Architecture Decision Records (ADR)** — three ADRs at `docs/adr/`: strategy-pattern interfaces, secrets manager, JSONB graph storage. More can be added as decisions land.
- [ ] **Run the OCI distribution-spec conformance suite** against Cooker's `/registry` proxy endpoints and publish the result. Until that runs, the "OCI compliant" claim in README is a documented intention, not a certified state.

## P9 — Native SDK adapters and additional deploy targets

> **Not blockers for production.** Each item below has a working CLI fallback that ships today (or is an additive new capability). The native-SDK rewrites give lower latency, fewer external CLI dependencies in the container, and richer error reporting — all nice-to-have, none required. Prioritize these only after P1–P4 land or if a specific user need surfaces.

### P9.1 — Replace CLI shell-outs with native Go SDKs

| File | Today | Replace with | Why bother |
|---|---|---|---|
| `backend/internal/builder/buildkit.go` | Stub; CLI fallback via `COOKER_BUILDER=docker` shells `docker build` | BuildKit gRPC client (`github.com/moby/buildkit/client`) | No `docker-cli` binary needed in the container; faster and correctly streams progress; no subprocess fan-out per build |
| `backend/internal/pusher/crane.go` | Stub; CLI fallback via `COOKER_PUSHER=docker` shells `docker push` | `github.com/google/go-containerregistry/pkg/crane` | No `docker-cli`; richer auth (OAuth flows, ECR/GCR token refresh); supports push by digest |
| `backend/internal/deployer/clientgo.go` | Stub; CLI fallback via `COOKER_DEPLOYER=kubectl` shells `kubectl apply` | `k8s.io/client-go` dynamic client | No `kubectl` binary needed; structured errors instead of stderr text parsing; can do partial-success rollback |

**Effort:** medium per adapter (~1 day each). All three plug into the existing strategy interfaces. The `select<Kind>` switches in `internal/server/server.go` already have the case branches; you only need to fill in the constructor.

**Caveat:** swapping out the CLI fallbacks shrinks the Dockerfile attack surface but requires bumping the Go module set (BuildKit pulls a lot). Check binary size impact before / after.

### P9.2 — Additional deploy targets

`internal/deploytarget/target.go` exposes a `Target` interface with one implementation today (`cloudrun/`, also stubbed). Adding a target = implement the interface + call `deploytarget.MustRegister(...)` in the package's `init()`.

| Adapter | Stub location | Underlying SDK | Notes |
|---|---|---|---|
| **Cloud Run** | `internal/deploytarget/cloudrun/` (returns `ErrUnavailable`) | `cloud.google.com/go/run/apiv2` | Needs GCP service-account credentials |
| **AWS ECS / Fargate** | not yet stubbed | `github.com/aws/aws-sdk-go-v2/service/ecs` | Tasks defined as JSON; map Pipeline stages → task definitions |
| **Fly.io** | not yet stubbed | `flyctl` SDK or REST API at `https://api.machines.dev` | Per-region machine deploy |
| **Render** | not yet stubbed | REST API `https://api.render.com/v1/` | Service-deploy POST |

**Effort:** ~1 day per target including basic e2e test. Each adapter is independent.

### P9.3 — GitOpsCommit node

- `backend/internal/gitops/gogit.go` — `go-git` writer is **stubbed** (Noop returns a deterministic fake SHA per `internal/gitops/noop.go`). When implemented, the GitOpsCommit pipeline node will commit a manifest change to a Git repo (e.g., for FluxCD / ArgoCD pull-based deploys).
- **Effort:** medium (~half day). Need to handle SSH key auth, signed commits if requested, and conflict-retry on concurrent writes.

### P9.4 — Tailscale `tsnet` transport

- `backend/internal/transport/tsnet/` is **build-tagged**; default builds don't include it.
- Allows Cooker to join a Tailnet and reach private K8s API servers / private registries without a public VPN.
- **Effort:** small (~2 hours) to remove the build tag and document. Larger (~half day) if Cooker needs to provision its own Tailscale auth keys via OAuth.
- **Caveat:** adding `tailscale.com/tsnet` is a sizeable dependency; default-off is the right call until there's a user need.

---

## Closed (recent)

Items that landed in the `claude/uat-ready-*` PR series, PR #6, and the `claude/cooker-backlog-readme-com8z` PR (#17):

- ✅ **App-root `ErrorBoundary`** (frontend) — P5 error-boundary item
- ✅ **Incident response runbook** — `docs/RUNBOOK.md` — P8
- ✅ **Architecture Decision Records (ADRs 0001-0003)** — `docs/adr/` — P8
- ✅ **Multi-replica + sticky-session guide** — `docs/MULTI_REPLICA.md` — P3 docs
- ✅ **`helm lint` + `helm template` + kubeconform CI** — P6.1
- ✅ **`deploytarget.Register` returns error; `MustRegister` for init() callers** — P6.2
- ✅ **Renovate config** — P1.5
- ✅ **TLS-at-ingress + Postgres `sslMode` documentation + values** — P1.3 / P1.4 (chart rendering still pending)
- ✅ **KeepSave secrets-manager backend** — `secrets.Manager` interface + `database`/`keepsave` adapters; selectable via `COOKER_SECRETS_BACKEND`. P2.1.
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
