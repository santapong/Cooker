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
- `backend/internal/server/server.go` — extend `selectBuilder` switch to handle `case "kaniko"`.
- `backend/internal/config/config.go` — keep doc-string in sync (`"noop" | "docker" | "buildkit" | "kaniko"`).
- `deploy/helm/cooker/values.yaml` — new `builder.kind: docker|buildkit|kaniko` value, default `kaniko` for production. New `builder.kaniko.{image, registryAuthSecret, cachingEnabled}` sub-values.
- `deploy/helm/cooker/templates/deployment.yaml` — when `builder.kind=kaniko`, **drop the docker.sock volume mount entirely** and remove the `group_add` requirement. Conditional logic in the existing `{{- if .Values.docker.enabled }}` block.
- `deploy/helm/cooker/templates/rbac.yaml` — Cooker's ServiceAccount needs `pods` create/get/delete (or `Job` resources) in its own namespace if it spawns Kaniko pods directly, OR `secrets get` if Cooker passes registry credentials to a kaniko pod template.
- `SECURITY.md` Docker Socket Security section — mark Kaniko as the default in the chart, leave docker.sock as a UAT/dev fallback.
- `docs/architecture.md` builder section — add Kaniko as a first-class adapter.
- `backlog.md` — close this item; add to "Closed" log.

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

**Redaction rules (codified in tests):**
- Secret values never appear (only key names).
- OIDC tokens (raw JWT) never appear (only the `sub` claim).
- Request bodies that may contain credentials (`PUT /environments/:id/secrets/:key`, `PUT /apps/:id/webhook`) are not logged in the `extra` field.

**Risk:** low. Pure addition; no behavior change.

---

### P1.3 — TLS termination at ingress

- [x] `ingress.tls` value documented in `deploy/helm/cooker/values.yaml` with cert-manager example.
- [x] Helm install snippet in README updated to set `ingress.tls[0].secretName=cooker-tls`.
- [x] README `Deployment → TLS at ingress` section with cert-manager + Let's Encrypt walk-through.
- [ ] SECURITY.md production checklist still references this; add the explicit `--set ingress.tls[0].secretName=...` line to the install snippet.
- [ ] `deploy/helm/cooker/templates/ingress.yaml` should validate that `ingress.tls` is non-empty when `cookerEnv=production` and `oidc.enabled=true` (helper failure for misconfigured production).

### P1.4 — PostgreSQL SSL

- [x] `?sslmode=require` documented in the README and `values.yaml`.
- [x] `postgresql.sslMode` Helm value with default `require`.
- [ ] Render `?sslmode={{ .Values.postgresql.sslMode }}` into the constructed `DATABASE_URL` in `templates/deployment.yaml`.
- [ ] For the bundled `bitnami/postgresql` subchart, also flip `tls.enabled=true` and pass-through CA bundle config.

### P1.5 — Base image / dependency rolling updates

- [x] `renovate.json` at the repo root: weekly Mon-AM schedule, automerge minor/patch on green CI, major bumps gated on human review, custom regex manager for `KUBECTL_VERSION` ARG in the Dockerfile.
- [ ] Operator step: enable Renovate / Dependabot on the repo (one-time UI toggle).

---

## P2 — Secrets manager integration

- [x] **P2.1 — KeepSave secrets manager.** **Closed.** `secrets.Manager` interface lives at `backend/internal/secrets/manager.go`; `database` adapter wraps the existing AES-GCM logic and `keepsave` adapter delegates to a KeepSave server over HTTP. Selection via `COOKER_SECRETS_BACKEND`. KeepSave is system of record when selected. See [README §Secrets backends](README.md#secrets-backends). Follow-ups: swap the internal HTTP client for the published Go SDK (currently lacks a `go.mod`); render KeepSave env-vars + `secretKeyRef` in the Helm `deployment.yaml`; surface KeepSave's `/promote` endpoint as a Cooker secret-promotion handler.
- [ ] **HashiCorp Vault adapter.** Same `secrets.Manager` interface, third adapter. Pulls via Vault Agent injector pattern.
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

- [ ] **Theme the sign-in landing page** (the gate inside `frontend/src/auth/ProtectedRoute.tsx`). Currently inline-styled placeholder; user has a Claude-generated design that may apply here. **Awaiting design source** (file path / screenshot / external link) before scoping.
- [ ] **Loading skeletons** instead of `Loading…` text for auth restoration and protected pages.
- [ ] **Error boundary** at the app root (currently uncaught render errors crash the React tree).
- [ ] **OIDC silent renew UI feedback** when `automaticSilentRenew` fails — surface a "session expired, please sign in again" toast instead of silently kicking to the IdP.
- [ ] **WebSocket auto-reconnect** with backoff on disconnect (PR F's tickets work for one connection; reconnects need fresh tickets — fetch a new ticket on each reconnect attempt).

## P6 — Backend code quality and CI

- [x] **P6.1 — `helm lint` + `helm template` + `kubeconform` in CI.** New `helm` job in `.github/workflows/ci.yml` runs lint, templates default and production-with-OIDC values, and validates rendered manifests with kubeconform.
- [x] **`gofmt -l` check in CI.** Backend job fails when any Go file isn't gofmt'd.
- [x] **Replace `panic(...)` in `internal/deploytarget/target.go`** — `Register` now returns `ErrDuplicateKind`; `MustRegister` wraps the panic-on-error semantics for `init()` callers. Tests cover both contracts.
- [ ] **`golangci-lint` in CI.** `Makefile` already has a `lint-backend` target; CI doesn't invoke it. Add a step (use `golangci/golangci-lint-action@v6`). Skipped from the wave-1 sweep to avoid surfacing pre-existing lint debt in this PR; do as a follow-up with a tuned `.golangci.yml`.
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

## P9 — Native SDK adapters and additional deploy targets

> **Not blockers for production.** Each item below has a working CLI fallback that ships today (or is an additive new capability). The native-SDK rewrites give lower latency, fewer external CLI dependencies in the container, and richer error reporting — all nice-to-have, none required. Prioritize these only after P1–P4 land or if a specific user need surfaces.

### P9.1 — Replace CLI shell-outs with native Go SDKs

| File | Today | Replace with | Why bother |
|---|---|---|---|
| `backend/internal/builder/buildkit.go` | Stub; CLI fallback via `COOKER_BUILDER=docker` shells `docker build` | BuildKit gRPC client (`github.com/moby/buildkit/client`) | No `docker-cli` binary needed in the container; faster and correctly streams progress; no subprocess fan-out per build |
| `backend/internal/pusher/crane.go` | Stub; CLI fallback via `COOKER_PUSHER=docker` shells `docker push` | `github.com/google/go-containerregistry/pkg/crane` | No `docker-cli`; richer auth (OAuth flows, ECR/GCR token refresh); supports push by digest |
| `backend/internal/deployer/clientgo.go` | Stub; CLI fallback via `COOKER_DEPLOYER=kubectl` shells `kubectl apply` | `k8s.io/client-go` dynamic client | No `kubectl` binary needed; structured errors instead of stderr text parsing; can do partial-success rollback |

**Effort:** medium per adapter (~1 day each). All three plug into the existing strategy interfaces (`builder.Builder`, `pusher.Pusher`, `deployer.Deployer`). The `select<Kind>` switches in `internal/server/server.go` already have the case branches; you only need to fill in the constructor.

**Caveat:** swapping out the CLI fallbacks shrinks the Dockerfile attack surface (no more `docker-cli` in `apk add`) but requires bumping the Go module set (BuildKit pulls a lot). Check binary size impact before / after.

### P9.2 — Additional deploy targets

`internal/deploytarget/target.go` exposes a `Target` interface with one implementation today (`cloudrun/`, also stubbed). Adding a target = implement the interface + call `deploytarget.MustRegister(...)` in the package's `init()`. The strategy pattern is already wired.

| Adapter | Stub location | Underlying SDK | Notes |
|---|---|---|---|
| **Cloud Run** | `internal/deploytarget/cloudrun/` (returns `ErrUnavailable`) | `cloud.google.com/go/run/apiv2` | Needs GCP service-account credentials; expose via env var |
| **AWS ECS / Fargate** | not yet stubbed | `github.com/aws/aws-sdk-go-v2/service/ecs` | Tasks defined as JSON; map Pipeline stages → task definitions |
| **Fly.io** | not yet stubbed | `flyctl` SDK or REST API at `https://api.machines.dev` | Per-region machine deploy |
| **Render** | not yet stubbed | REST API `https://api.render.com/v1/` | Service-deploy POST |

**Effort:** ~1 day per target including basic e2e test. Each adapter is independent.

### P9.3 — GitOpsCommit node

- `backend/internal/gitops/gogit.go` — `go-git` writer is **stubbed** (Noop returns a deterministic fake SHA per `internal/gitops/noop.go`). When implemented, the GitOpsCommit pipeline node will commit a manifest change to a Git repo (e.g., for FluxCD / ArgoCD pull-based deploys).
- `internal/gitops/writer.go` defines the interface; `gogit.go` is the placeholder.
- **Effort:** medium (~half day). Need to handle SSH key auth, signed commits if requested, and conflict-retry on concurrent writes.

### P9.4 — Tailscale `tsnet` transport

- `backend/internal/transport/tsnet/` is **build-tagged**; default builds don't include it.
- Allows Cooker to join a Tailnet and reach private K8s API servers / private registries without a public VPN.
- **Effort:** small (~2 hours) to remove the build tag and document. Larger (~half day) if Cooker needs to provision its own Tailscale auth keys via OAuth.
- **Caveat:** adding `tailscale.com/tsnet` is a sizeable dependency; default-off is the right call until there's a user need.

---

## Closed (recent)

Items that landed in the `claude/uat-ready-*` PR series, PR #6, and the `claude/cooker-backlog-readme-com8z` PR (#17):

- ✅ **`helm lint` + `helm template` + kubeconform CI** — P6.1
- ✅ **`gofmt -l` CI check** — P6.2
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
