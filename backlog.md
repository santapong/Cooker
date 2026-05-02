# Cooker Backlog

Tracks work that's been planned, scoped, or hinted at by the codebase but isn't shipped yet. Living document — when an item lands on `main`, remove it (or move it to the changelog).

Items are grouped by area and roughly prioritized within each group.

---

## P1 — Production hardening (operator-side)

These can't ship inside the chart because they depend on the operator's environment.

- [ ] **TLS termination at ingress.** Cooker doesn't terminate TLS itself; the ingress controller (or cloud LB) must. Update `deploy/helm/cooker/values.yaml` `ingress.tls` and document in SECURITY.md.
- [ ] **Kaniko-based image builds (no docker.sock).** Today UAT and the Helm chart bind-mount `/var/run/docker.sock`, which gives the container root-equivalent host access even when the container itself runs non-root. Replace with Kaniko (in-cluster, rootless) for production. Wire up via the existing `builder.Builder` strategy interface — add `internal/builder/kaniko.go` and select with `COOKER_BUILDER=kaniko`.
- [ ] **PostgreSQL SSL.** Document the connection-string flag and provide a Helm value to flip `sslmode=require`.
- [ ] **Audit logging.** Capture authenticated mutations (CreatePipeline, RunPipeline, DeployApp, ApprovePromotion, RevealSecret) to a structured log so operators can answer "who did what". Suggest a `slog`-based audit logger; `RequireRole` is the natural place to instrument.
- [ ] **Base image / dependency rolling updates.** A renovate / dependabot config for `go.mod`, `package.json`, the Alpine + kubectl pins in the Dockerfile, and the Helm chart `Chart.yaml`.

## P2 — Secrets manager integration (planned)

- [ ] **PR G — KeepSave secrets manager.** **Backlog.** Will be planned in a separate session once the user walks through KeepSave's API. The integration is purely additive: define a `secrets.Manager` interface (`Get`, `Put`, `Delete`, `List`), add `internal/secrets/keepsave/` adapter, and switch `internal/handler/environment.go` to call it. The DB-backed encrypted column keeps working as the default and as a fallback. Helm values: `secrets.backend: keepsave|database`, `secrets.keepsave.{url,authMode,...}`.
- [ ] **HashiCorp Vault adapter.** Same `secrets.Manager` interface, second adapter. Pulls via Vault Agent injector pattern.
- [ ] **AWS Secrets Manager / GCP Secret Manager adapters.** Cloud-native deployments.

## P3 — Auth and authorization extensions

- [ ] **Sticky sessions documentation for WebSocket tickets.** PR F's ticket store is per-process. Multi-replica deployments need either sticky sessions at the ingress or a Redis-backed ticket store. Document the recommended ingress annotations; later, optionally add the Redis backend.
- [ ] **Distributed rate limiter.** PR H is per-process. For multi-replica, add a Redis-backed `rate.Limiter` (e.g. `go-redis/redis_rate`) selected by config, default off.
- [ ] **MFA / step-up auth at the IdP.** Cooker delegates auth to the IdP, but admin-only operations (DeleteApp, RevealSecret) could request a step-up via `acr_values=mfa` on the OIDC redirect. Per-route opt-in.
- [ ] **OIDC group-to-role mapping configurable.** Today `MapGroupsToRoles` (`backend/internal/auth/rbac.go:77`) hardcodes `cooker-admins → admin`, etc. Make the mapping a `map[string]string` from `COOKER_OIDC_GROUP_MAP` so deployments can integrate with whatever group naming they have.

## P4 — Observability

- [ ] **Prometheus metrics endpoint.** `/metrics` exposing Gin request counters/latency, executor stage outcomes, WebSocket connection counts, rate-limiter denials. Standard `prometheus/client_golang` instrumentation.
- [ ] **OpenTelemetry traces.** Trace pipeline runs end-to-end (handler → service.Executor → builder/pusher/deployer). Wire via `otelgin` middleware and propagate context through DAG runner.
- [ ] **Structured logging.** Migrate from `log` to `log/slog` with a JSON handler in production. Audit logging (P1) lands on top.

## P5 — Frontend UX

- [ ] **Theme the sign-in landing page** (the gate inside `frontend/src/auth/ProtectedRoute.tsx`). Currently inline-styled placeholder; user mentioned a Claude-generated design that may apply here.
- [ ] **Loading skeletons** instead of `Loading…` text for auth restoration and protected pages.
- [ ] **Error boundary** at the app root (currently uncaught render errors crash the React tree).
- [ ] **OIDC silent renew UI feedback** when `automaticSilentRenew` fails — surface a "session expired, please sign in again" toast instead of silently kicking to the IdP.
- [ ] **WebSocket auto-reconnect** with backoff on disconnect (PR F's tickets work for one connection; reconnects need fresh tickets).

## P6 — Backend code quality

- [ ] **Replace `panic(...)` in `internal/deploytarget/target.go:86`** with a returned error from `Register()` and a startup-time check. The current `MustRegister`-style is fine but doesn't compose well if we ever want plug-ins to register at runtime.
- [ ] **golangci-lint in CI.** `Makefile` already has a `lint-backend` target; CI doesn't invoke it. Add a step.
- [ ] **`gofmt -l` check in CI.** Catch style drift before merge.
- [ ] **Go version bump to 1.24+.** `golang.org/x/time@v0.5.0` is pinned because newer versions need 1.25. Update `go.mod`, `Dockerfile`, and `.github/workflows/ci.yml` together.
- [ ] **Replace `internal/handler/network.go` and `internal/handler/volume.go` placeholder responses** with real Docker SDK calls. Currently they return mock IDs.

## P7 — UAT and dev experience

- [ ] **`tecnativa/docker-socket-proxy`** as an alternative to `group_add` in `docker-compose.uat.yml`. The `group_add` workaround in PR E auto-detects the host docker GID, but operators on unusual hosts hit the fallback (999). Socket proxy avoids the GID problem entirely and exposes only the Docker API endpoints Cooker actually uses (read, build, push) — finer-grained capability surface than full socket access.
- [ ] **`helm lint` in CI.** PR D's templates were validated by hand because no `helm` binary in the dev environment. Add a job that lints the chart and runs `helm template` against a fixture values file.
- [ ] **`make uat-up-with-keycloak`** target that adds Keycloak as a compose service and pre-seeds a realm, so testers can exercise the full OIDC flow without an external IdP. Currently testers must use Google OIDC or a self-hosted IdP.
- [ ] **`make test-e2e`** that boots `make uat-up`, runs a deterministic pipeline through the API, and tears down. Currently UAT testing is manual per `docs/UAT.md`.

## P8 — Documentation

- [ ] **OpenAPI / Swagger spec** for `/api/v1`. Manually maintained today as a markdown table in README.md; tools like `swaggo/swag` can generate from Go source comments.
- [ ] **Runbook for incident response.** What to do when a build runs forever, when the DB goes down, when an OIDC issuer is unreachable.
- [ ] **Architecture Decision Records (ADR)** for the bigger decisions (JSONB graph storage, in-memory + Postgres dual store, single-binary deployment). `docs/architecture.md` mentions them in passing; full ADRs would help future contributors.

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
