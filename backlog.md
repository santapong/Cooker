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

## Open items

What's left, organised by priority. All "blocked-on-bigger-PR" items have a one-line rationale for why they didn't ship in PR #17 and what unblocks them.

### P1 — Production hardening (operator-side)

#### ⭐ P1.1 — Kaniko builder adapter

Closes the docker.sock RCE-to-host gap. Highest-priority remaining item.

**Why not in PR #17:** ~1 day of focused work. Pulls `client-go` Job APIs, needs a fake-K8s test, requires Helm-template conditionals to drop the docker.sock mount. Best as its own focused PR so reviewers can audit the RBAC story in isolation.

**Files to add:** `backend/internal/builder/kaniko.go`, `backend/internal/builder/kaniko_test.go`.
**Files to modify:** `backend/internal/server/server.go` (`selectBuilder` switch), `backend/internal/config/config.go` (doc-string), `deploy/helm/cooker/values.yaml` (`builder.kind` + `builder.kaniko.*`), `deploy/helm/cooker/templates/deployment.yaml` (drop docker.sock when `builder.kind=kaniko`), `deploy/helm/cooker/templates/rbac.yaml` (`Job` create/get/delete in own namespace), `SECURITY.md`, `docs/architecture.md`.

#### ⭐ P1.2 — Audit logging middleware (slog-based)

**Why not in PR #17:** ~2 hours of focused work. Best as its own PR so the redaction rules and the per-route audit-eligible list can be reviewed cleanly.

**Files to add:** `backend/internal/audit/audit.go`, `backend/internal/audit/audit_test.go`, `backend/internal/server/middleware_audit.go`.
**Files to modify:** `backend/cmd/cooker/main.go`, `backend/internal/server/server.go`, `backend/internal/server/router.go`, `SECURITY.md`.
**Config:** `COOKER_AUDIT_ENABLED` (default `true` in production), `COOKER_AUDIT_DESTINATION` (default `stdout`).
**Redaction:** secret values never appear; OIDC raw JWTs never appear; bodies of `PUT /environments/:id/secrets/:key` and `PUT /apps/:id/webhook` are not logged in `extra`.

#### P1.3 — TLS at ingress (chart hardening)

- [x] `ingress.tls` value documented + cert-manager example.
- [x] README §Deployment → TLS at ingress.
- [ ] SECURITY.md production checklist alignment.
- [ ] `templates/ingress.yaml` should fail-template when `cookerEnv=production` and `oidc.enabled=true` and `ingress.tls` is empty.

#### P1.4 — PostgreSQL SSL (chart hardening)

- [x] Documented in README + values.yaml.
- [x] `postgresql.sslMode` Helm value, default `require`.
- [ ] Render `?sslmode={{ .Values.postgresql.sslMode }}` into `DATABASE_URL` in `templates/deployment.yaml`.
- [ ] For bundled `bitnami/postgresql`, flip `tls.enabled=true` and pass-through CA bundle config.

#### P1.5 — Renovate

- [x] `renovate.json` shipped.
- [ ] Operator step: enable Renovate / Dependabot on the repo (one-time UI toggle).

---

### P2 — Secrets manager integration

- [x] **P2.1 — KeepSave secrets manager** — see [README §Secrets backends](README.md#secrets-backends) and [ADR-0002](docs/adr/0002-secrets-manager.md). Follow-ups:
  - [ ] Render KeepSave env-vars + `secretKeyRef` in the Helm `deployment.yaml`.
  - [ ] Swap the internal HTTP client for the published Go SDK (currently lacks a `go.mod`).
  - [ ] Surface KeepSave's `/promote` endpoint as a Cooker secret-promotion handler.
- [ ] **HashiCorp Vault adapter.** Same `secrets.Manager` interface, third adapter. Pulls via Vault Agent injector pattern. ~half day once the interface is in place.
- [ ] **AWS Secrets Manager / GCP Secret Manager adapters.** Cloud-native deployments. ~half day each.

---

### P3 — Auth and authorization extensions

- [x] **Sticky-session docs** — `docs/MULTI_REPLICA.md` covers NGINX/ALB/Traefik/HAProxy/Envoy.
- [ ] **Redis-backed rate limiter + WS ticket store.** **Why not in PR #17:** new Go dep (`github.com/go-redis/redis_rate/v10` + `redis/go-redis`); adding deps requires a `go.sum` regen step that needs a local Go toolchain. Sticky sessions are the supported multi-replica path until then.
- [ ] **MFA / step-up auth at the IdP.** Admin-only operations could request `acr_values=mfa` on the OIDC redirect. Per-route opt-in. ~half day.
- [ ] **OIDC group-to-role mapping configurable.** Today `MapGroupsToRoles` (`backend/internal/auth/rbac.go`) hardcodes `cooker-admins → admin`, etc. Make the mapping a `map[string]string` from `COOKER_OIDC_GROUP_MAP`. ~2 hours.

---

### P4 — Observability

All three items below add Go module deps; they share a single follow-up PR that also handles the `go.sum` regen.

- [ ] **Prometheus `/metrics`.** `prometheus/client_golang`, Gin middleware. ~half day.
- [ ] **OpenTelemetry traces.** `go.opentelemetry.io/otel`, `otelgin`. Trace pipeline runs end-to-end. ~1 day.
- [ ] **`log/slog` migration.** Replace remaining `log` calls with structured slog handlers. Lands on top of P1.2 audit logger.

---

### P5 — Frontend UX

- [ ] **Sign-in landing page theme.** **Blocked on Claude-generated design source** (file path / screenshot / external link).
- [x] **Loading skeletons** — `Skeleton` + `SkeletonStack` shipped. `ProtectedRoute` uses them during auth restore.
- [x] **App-root error boundary** — `ErrorBoundary` shipped.
- [ ] **OIDC silent renew toast.** When `automaticSilentRenew` fails, show a "session expired" toast instead of silently kicking to the IdP. **Why not in PR #17:** Cooker has no app-wide toast/notification primitive yet; introducing one is a small but separate concern (~half day for the toast hook + the silent-renew hookup).
- [ ] **WebSocket auto-reconnect with backoff.** PR F's tickets work for one connection; reconnects need fresh tickets. ~half day.

---

### P6 — Backend code quality and CI

- [x] **`helm lint` + `helm template` + `kubeconform`** — shipped.
- [x] **`deploytarget.Register` returns error; `MustRegister` for init() callers.**
- [ ] **`gofmt -l` check in CI.** First attempt surfaced pre-existing drift unrelated to this PR (no local Go toolchain). Pair with the next item so a single tuned-config sweep can normalize everything.
- [ ] **`golangci-lint` in CI.** Add `golangci/golangci-lint-action@v6` + a tuned `.golangci.yml`. ~half day to settle the exclude list.
- [ ] **Go version bump to 1.24+.** `golang.org/x/time@v0.5.0` is pinned because newer versions need 1.25. Update `go.mod`, `Dockerfile`, and `.github/workflows/ci.yml` together; unpin `x/time`.
- [ ] **Replace `internal/handler/network.go` and `internal/handler/volume.go` placeholder responses** with real Docker SDK calls. Currently they return mock IDs.

---

### P7 — UAT and dev experience

- [ ] **`tecnativa/docker-socket-proxy`** as an alternative to `group_add` in `docker-compose.uat.yml`. **Why not in PR #17:** real behavior change requiring verification — better as a focused PR with an opt-in `socketproxy` compose profile so the default `make uat-up` keeps working.
- [ ] **`make uat-up-with-keycloak`** target that adds Keycloak as a compose service and pre-seeds a realm. **Why not in PR #17:** realm pre-seed is environment-specific; needs a working Keycloak start-realm JSON checked in. ~half day.
- [ ] **`make test-e2e`** that boots `make uat-up`, runs a deterministic pipeline through the API, and tears down. ~1 day.

---

### P8 — Documentation

- [x] **OpenAPI sketch** at `docs/openapi.yaml` covering pipelines, runs, environments + secrets, apps + webhook, and the GitHub webhook entry point.
- [ ] **Generated OpenAPI** via `swaggo/swag` from Go source comments. ~half day to annotate handlers + wire `swag init` into the build.
- [x] **Incident runbook** at `docs/RUNBOOK.md`.
- [x] **ADRs 0001-0003** at `docs/adr/`.
- [ ] **Run the OCI distribution-spec conformance suite** against Cooker's `/registry` proxy endpoints and publish the result. ~half day.

---

### P9 — Native SDK adapters and additional deploy targets (not blockers)

> Each item below has a working CLI fallback today (or is an additive new capability). Native rewrites give lower latency, fewer external CLI dependencies in the container, and richer error reporting — all nice-to-have, none required.

#### P9.1 — Replace CLI shell-outs with native Go SDKs

| File | Today | Replace with |
|---|---|---|
| `backend/internal/builder/buildkit.go` | Stub; CLI fallback shells `docker build` | `github.com/moby/buildkit/client` (gRPC) |
| `backend/internal/pusher/crane.go` | Stub; CLI fallback shells `docker push` | `github.com/google/go-containerregistry/pkg/crane` |
| `backend/internal/deployer/clientgo.go` | Stub; CLI fallback shells `kubectl apply` | `k8s.io/client-go` dynamic client |

**Why not in PR #17:** each adds a heavy Go dep; needs a `go.sum` regen and binary-size verification. ~1 day per adapter.

#### P9.2 — Additional deploy targets

| Adapter | Status | Underlying SDK |
|---|---|---|
| Cloud Run | stubbed (`internal/deploytarget/cloudrun/`) | `cloud.google.com/go/run/apiv2` |
| AWS ECS / Fargate | not stubbed | `github.com/aws/aws-sdk-go-v2/service/ecs` |
| Fly.io | not stubbed | REST API `https://api.machines.dev` |
| Render | not stubbed | REST API `https://api.render.com/v1/` |

**Why not in PR #17:** each adapter is a new SDK dep + ~1 day of contract tests + e2e against a real account. Independent of each other.

#### P9.3 — GitOpsCommit node

`backend/internal/gitops/gogit.go` — `go-git` writer is **stubbed**. Implementing it is ~half day (SSH key auth, signed commits, conflict-retry).

#### P9.4 — Tailscale `tsnet` transport

`backend/internal/transport/tsnet/` is build-tagged. Removing the build tag is ~2 hours; provisioning own auth keys via OAuth is ~half day. Adds a sizeable dep — default-off is the right call until there's a user need.

---

## Closed (recent)

Items that landed in the `claude/uat-ready-*` PR series, PR #6, and the `claude/cooker-backlog-readme-com8z` PR (#17):

- ✅ **Skeleton + SkeletonStack components** + ProtectedRoute integration — P5 loading-skeletons
- ✅ **OpenAPI 3.1 sketch** at `docs/openapi.yaml` — P8
- ✅ **App-root `ErrorBoundary`** (frontend) — P5 error-boundary
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
