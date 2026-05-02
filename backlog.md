# Cooker Backlog

Tracks work that's been planned, scoped, or hinted at by the codebase but isn't shipped yet. Living document — when an item lands on `main`, remove it (or move it to the changelog).

Items are grouped by area and roughly prioritized within each group.

---

## Production readiness summary

After the `claude/uat-ready-*` PR series, the `claude/cooker-backlog-readme-com8z` PR (#17), and the `claude/complete-p1-backlog-qN4FP` PR (closing all P1 code-side items) merged, Cooker is **production-quality.** The dangerous defaults are gone, Kaniko ships as an in-cluster builder, audit logging is on by default in production, the chart fails template-render if `production+OIDC+ingress` is missing TLS, and `DATABASE_URL` is rendered with `?sslmode=require`. Remaining production-readiness gaps are operator-side (TLS provisioning, OIDC IdP setup, Postgres backend choice).

### Deployment-shape readiness matrix

| Shape | Verdict |
|---|---|
| **Single-replica + TLS ingress + Kaniko + Postgres SSL + edge rate limit** | ✅ Production-ready. Ship it. |
| **Single-replica + TLS ingress + still using `docker.sock`** | ⚠️ Functionally works, but if the host runs other workloads, an RCE in Cooker compromises them all. |
| **Multi-replica + sticky sessions + TLS + Kaniko** | ✅ Production-ready for HA. See [docs/MULTI_REPLICA.md](docs/MULTI_REPLICA.md). |
| **Multi-replica without sticky sessions or Redis-backed limiter/tickets** | ❌ Will break: users get random 401s on WS reconnect, rate limits don't enforce correctly. |
| **Anything without TLS + OIDC** | ❌ Sign-in flow won't work — most IdPs refuse non-HTTPS redirect URIs. |

### Operator-side concerns (still your call)

The chart can't make these decisions for you:

1. **TLS at ingress** is required for OIDC. Cooker doesn't terminate TLS itself; provision a cert with cert-manager (or equivalent) and reference it in `ingress.tls`. The chart now refuses to render if `cookerEnv=production AND oidc.enabled=true AND ingress.enabled=true AND ingress.tls is empty`.
2. **Builder choice** — set `builder.kind=kaniko` in production. The `docker` builder still ships for single-node test clusters but gives the Cooker container root-equivalent access to the host Docker daemon. The chart conditionally drops the `docker.sock` mount when `builder.kind != "docker"`.
3. **Multi-replica state** — rate limiter and WebSocket ticket store are per-process. Pin sticky sessions at ingress (works fine; documented in `docs/MULTI_REPLICA.md`) or implement Redis-backed versions (P3).
4. **Postgres SSL** — `?sslmode=` now renders into `DATABASE_URL` from `postgresql.sslMode` (default `require`). Set `database.host` to a TLS-capable Postgres for the chart to wire it through.
5. **Audit destination** — `COOKER_AUDIT_DESTINATION=stdout` (the default) routes via the cluster log stack. Set `COOKER_AUDIT_DESTINATION=file` + `COOKER_AUDIT_FILE_PATH` if you'd rather pair with a sidecar tail-shipper.

### What "OCI compliance" means here

`README.md` and `architecture.md` claim conformance with all three OCI specs. The code uses OCI-compliant libraries (`go-containerregistry`, the official image-spec types) and produces OCI-compliant images, but **nothing has been formally tested against the [OCI distribution-spec conformance suite](https://github.com/opencontainers/distribution-spec/tree/main/conformance).** Until that runs, "OCI compliant" is a documented claim, not a certified one.

---

## Open items

What's left, organised by priority. All "blocked-on-bigger-PR" items have a one-line rationale for why they didn't ship in PR #17 and what unblocks them.

### P1 — Production hardening (operator-side)

All P1 code-side items are closed (see "Closed (recent)" below). The
remaining bullet is operator-side only:

#### P1.5 — Renovate (operator step)

- [x] `renovate.json` shipped.
- [ ] **Operator step (cannot be done in code):** enable Renovate or
      Dependabot on the repo via the GitHub UI — Settings → Code
      security and analysis → Dependabot, or install the Renovate
      GitHub App. One-time toggle.

#### P1.4 follow-up — bundled bitnami/postgresql TLS passthrough

- [ ] If bundling Postgres in-chart (currently no subchart in
      `Chart.yaml`), flip `tls.enabled=true` on the bitnami/postgresql
      subchart and pass through the CA bundle config. Deferred — bundling
      Postgres is a larger architectural decision; today operators bring
      their own Postgres and reference it via `database.host`.

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

#### P9.5 — Buildah builder adapter (alternative to Kaniko)

A third in-cluster builder alongside Kaniko, slotting into the same
`builder.Builder` interface and the same `batch/v1.Job` Pod pattern. Job
runs `quay.io/buildah/stable` instead of `gcr.io/kaniko-project/executor`.

**Why an operator would pick Buildah over Kaniko:**

- Full Dockerfile feature parity with BuildKit — `RUN --mount=type=cache`,
  `RUN --mount=type=secret`, `RUN --mount=type=ssh`, heredocs. Kaniko silently
  ignores these directives.
- Better layer cache when paired with `--layers --cache-to=registry://...`.
- Active maintenance pace (Red Hat / containers.org); Kaniko's release
  cadence has slowed.

**Why an operator would not:**

- Rootless Buildah needs `CAP_SETUID` + `CAP_SETGID` for its user-namespace
  setup. PodSecurityAdmission "restricted" drops both — operators must opt
  the build namespace into "baseline" or a custom profile. Kaniko avoids
  this with `runAsUser=0` inside the container only.
- Larger image (~150 MB vs Kaniko's ~50 MB).
- Storage driver choice: needs `overlay` (with fuse-overlayfs on the
  nodes) or `vfs` (slower, no kernel module). Kaniko bundles its own.

**Files to add:** `backend/internal/builder/buildah.go`,
`backend/internal/builder/buildah_test.go`.

**Files to modify:**
- `backend/internal/server/server.go` — `selectBuilder` add
  `case "buildah": return builder.NewBuildah(...)`.
- `backend/internal/config/config.go` — `KubernetesConfig.BuildahImage`,
  `BuildahServiceAccount`, `BuildahStorageDriver` (`overlay` | `vfs`).
- `deploy/helm/cooker/values.yaml` — `builder.buildah.{image, namespace,
  serviceAccount, contextPVC, storageDriver}`. Document the PSA story
  inline.
- `deploy/helm/cooker/templates/deployment.yaml` — extend the
  `COOKER_BUILDER=kaniko` env block to include buildah's env-vars when
  `builder.kind=buildah`.
- `deploy/helm/cooker/templates/rbac.yaml` — extend the gate from
  `eq .Values.builder.kind "kaniko"` to
  `or (eq .Values.builder.kind "kaniko") (eq .Values.builder.kind "buildah")`.
  Same Role + RoleBinding apply (Job + Pod create/get/delete/watch in
  the build namespace).
- `SECURITY.md` — add Buildah row to the "image build isolation" table
  with the PSA caveat called out.
- `.github/workflows/ci.yml` — extend the helm-template matrix with a
  `builder.kind=buildah` render that asserts (a) docker-socket is absent,
  (b) RBAC objects are present.

**CLI fallback option (lighter alt):** shell out to `buildah bud` from a
sidecar container in the cooker pod, no Job submission. Fewer moving
parts, but needs the Cooker container image to bundle buildah (~150 MB)
and the user-namespace capability on the cooker pod itself. Not
recommended for production.

**Effort:** ~1 day for the Job-based version (mostly the PSA story and
the `--cache-to` registry wiring); ~half day for the CLI shell-out.

---

## Closed (recent)

Items that landed in the `claude/uat-ready-*` PR series, PR #6, the `claude/cooker-backlog-readme-com8z` PR (#17), and the `claude/complete-p1-backlog-qN4FP` PR:

- ✅ **Kaniko builder adapter** (`internal/builder/kaniko.go` + tests, `selectBuilder` wiring, `builder.kind`/`builder.kaniko.*` chart values, `templates/rbac.yaml`, docker.sock conditionally dropped in deployment.yaml). Closes the docker.sock RCE-to-host gap. — P1.1
- ✅ **Audit logging middleware** (`internal/audit/`, `internal/server/middleware_audit.go`, `COOKER_AUDIT_*` config, on-by-default in production, redaction documented). — P1.2
- ✅ **Ingress TLS chart guard** — `templates/ingress.yaml` fails template-render when `cookerEnv=production` AND `oidc.enabled=true` AND `ingress.enabled=true` AND `ingress.tls` is empty; CI matrix asserts both pass and fail paths. SECURITY.md aligned. — P1.3
- ✅ **PostgreSQL `?sslmode=` rendering** — `database.{host,port,name,username,passwordSecretRef}` values block; `templates/deployment.yaml` constructs `DATABASE_URL` with `?sslmode={{ .Values.postgresql.sslMode }}`. — P1.4
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
