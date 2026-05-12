# Changelog

All notable changes to the Cooker project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added — May 2026 W1 batch (PRs #31, #32, #33, #35, #36, #37, #38, #39, #40)

**First week of execution against the May-2026 30-day plan.** Three primary code PRs landed (CI, security, frontend perf); seven research audit docs landed and surfaced four production-shape bugs for fast-track in W2+.

#### Primary code (production-shape)

- **CI critical path → ~3 min on warm cache** (PR #35). Parallel `go test -race ./...` in one invocation (P26-05-34); drop `needs: [backend, frontend, helm]` serialisation on the docker job (P26-05-38); `docker/setup-buildx-action@v3` + `docker/build-push-action@v6` with `cache-from: type=gha`, `cache-to: type=gha,mode=max` (P26-05-39); `actions/cache@v4` for `~/.cache/go-build` keyed on `hashFiles('backend/go.sum')` (P26-05-35 bonus).
- **Frontend bundle split — entry chunk 490 KB → 59 KB (88% cut)** (PR #38). Route-level `React.lazy` + `<Suspense fallback={<SkeletonStack />}>` for all non-landing routes; Vite `manualChunks` splits `react`, `@xyflow/react`, `oidc-client-ts`, `zustand` into independent vendor chunks. `@xyflow/react` (~150 KB) only loads on canvas routes (PipelineEditor, RunPage, ComposePage). Closes P26-05-24 + P26-05-28.
- **S26-05 security quick wins (six fixes)** (PR #39).
  - **S26-05-04** — drop `/var/run/docker.sock` volume + volumeMount from `deploy/kubernetes/deployment.yaml`; inline warning comment.
  - **S26-05-13** — drop `postgresql.auth.password: cooker` default from `values.yaml`; `required`-guard `database.passwordSecretRef.name` in `_helpers.tpl`.
  - **S26-05-10** — `net/url`-based `sslmode` enforcement in `Config.Validate()`: non-localhost hosts must use `require` / `verify-ca` / `verify-full` in production. Four new tests.
  - **S26-05-01** — replace five reflected-error sites in `internal/auth/oidc.go` with generic `authentication failed` / `provider unavailable`; detail at `slog.Warn`/`slog.Error` server-side. `TestMiddleware_TamperedLocalTokenReturnsGenericBody` pins the contract.
  - **S26-05-19** — RBAC / rate-limiting / CORS wording updates in `SECURITY.md`; flip Postgres-SSL checklist line to checked.
  - **S26-05-23** — env-configurable `orphanThreshold` via `COOKER_ORPHAN_SWEEP_INTERVAL` (default 60s, rejects values ≤ `heartbeatInterval`). `TestOrphanThreshold_DefaultIsSafe` added. Note: the SQL-parameterisation half of S26-05-23 is **still open**.

#### Research audits (W1 idle-lane output)

Each lands as a doc only; the actionable findings are summarised inline.

- **`docs/audits/W11-followup-2026-05.md`** (PR #31, `cooker-planner`). 31/31 W11 gaps cross-reference clean. Two follow-ups: silent P1→P2 demotion of Kaniko/Buildah `nodeSelector` + `tolerations` (W11 §ML step 6), and single-persona tagging on first-run onboarding.
- **`docs/audits/2026-05-adapter-wiring.md`** (PR #32, `cooker-backend-adapters`). Five findings. **F-02 (High)**: missing `COOKER_PUSHER=docker` production gate in `Config.Validate` — silent regression of the existing `COOKER_BUILDER=docker` guard. F-01 (High): `selectBuilder` / `selectPusher` / `selectDeployer` default-fall-through to `Noop{}` on unknown values. Plus three Lower-severity findings.
- **`docs/audits/2026-05-deploy-parity.md`** + **`2026-05-store-parity.md`** (PR #33, `cooker-infra-deploy` + `cooker-backend-data`, stacked on one branch due to sandbox shared-cwd contamination). **F-01 (Production)**: raw-K8s manifests probe `/health` (10 s initial delay); chart probes `/health/live` + `/health/ready` (60 s). Raw-manifest install path → `CrashLoopBackOff`. **F-07 (Production)**: `RunCoordinator.Spawn` for app-deploy writes heartbeats to a row that doesn't exist yet (`handler/app.go:179-184`); OOM-killed pod leaves no orphan row for `SweepOrphans` → run lost silently.
- **`docs/audits/2026-05-frontend-hygiene.md`** (PR #40, replaces #34 after extracting unique file from contaminated commit). Seven findings. **FH-03 (High)**: `useWebSocket.connect()` races with `disconnect()` during the ticket fetch; rapid unmounts leak WebSocket connections up to the browser's 256-per-origin limit. One-line fix recommended.
- **`docs/audits/2026-05-half-shipped.md`** (PR #36, `cooker-feature-dev`). Five trust-of-tool gaps where UI claims success but backend does nothing. **HS26-05-01**: promotion + approval flow is theatre (handlers synthesise success). **HS26-05-02**: GitHub webhook deploy returns 202 but never enqueues. HS26-05-03 already closed by T1 (DAG plan). HS26-05-04: settings registry/cluster CRUD persists nothing. HS26-05-05: `/kubernetes/*` fully stubbed.
- **`docs/audits/2026-05-handler-layering.md`** (PR #37, `cooker-backend-api`). Three High findings. F1: duplicate DAG validator in `handler/pipeline.go:267-324` (57-line reimplementation of `service.ValidatePipelineDAG`). F2: run-status finalisation rule embedded in the `RunPipeline` goroutine closure. F3: compose-file parsing + graph construction (~200 LOC) in the docker handler.

#### Sandbox-isolation lesson

The W1 parallel-spawn run (10 background agents sharing one cwd) produced cross-branch contamination: PR #34's head commit bundled three audits, only one its own; PR #33's branch stacked three audits in sequence. PR #34 was closed and replaced by PR #40 (the unique frontend-hygiene file salvaged onto a fresh branch). W2+ spawns use `isolation: "worktree"` per the team plan.

### Added — `claude/project-audit-security-GKXzQ` (PR #29) — May 2026 audit week

**Seven-workstream audit run. Four waves of consolidation. No production code changes — this PR ships the planning + research surface that the 30-day execution plan runs against.** Detailed scope and decisions in [`docs/pm-brief-2026-05.md`](docs/pm-brief-2026-05.md).

#### Wave 1 — fresh audits

- **`docs/audits/2026-05-security-review.md`** (407 lines). Full-repo line-cited security pass against post-PR-#21 HEAD. Auth, secrets, container & supply chain, network, data, API surface, threat-model drift.
- **`docs/audits/2026-05-perf-and-optimization.md`** (445 lines). Allocations, latency, throughput, footprint, startup time.
- **`docs/audits/dag-performance.md`** (177 lines). Cache, job-queue/concurrency, fault tolerance, per-stage logging behaviour for `backend/pkg/dagrunner` + `internal/service` + `internal/builder` + `internal/deployer`.
- **`docs/shipping-go.md`**. Research: how mature OSS Go products release and operate; 0–180 day Cooker adoption plan. Gates the marketing launch.

#### Wave 2 — strategic planning

- **`docs/roadmap-2026.md`** (205 lines). 2026 themes + top-30. Strategic frame: between "Jenkins-is-too-much" and "GitHub-Actions-YAML-is-too-little".
- **`docs/protocols.md`** (699 lines). §3 **CKR-LOG/1** (length-prefixed binary log-stream framing). §4 **CKR-DSL** (pipeline DSL design surface, recommended syntax YAML — needs decision B).
- **`docs/marketing/strategy.md`**. OSS-adoption strategy, 90-day horizon. Blocked on `shipping-go.md` deliverables.

#### Wave 3 — user guide

34 files across `docs/user-guide/` — `index.md`, `concepts/`, `getting-started/`, `guides/`, `operations/`, `reference/`, `troubleshooting/`, `faq.md`. ~4,908 lines.

#### Wave 4 — PM brief + DAG plan

- **`docs/pm-brief-2026-05.md`** (183 lines). 15-item 90-day plan (Block 1), eight open decisions A–H, agent-delegation map.
- **`docs/dag-adaptation-2026.md`** (649 lines). Research from Jenkins / Dokploy / Dagger / Airflow. Output: **5 DAG primitives ranked**, **5 tidy-first refactors T1–T5**, **4 ADRs DR-1..DR-4**, **20-week implementation calendar**.

#### CI fix

- `fix(ci): unblock backend gofmt step — drop trailing blank lines` (commit `ed0a212`).

---

### Added — bridge entries (PRs #21, #23, #24, #25, #26, #27, #28)

Catching CHANGELOG up to where commits already landed. Each block summarises a merged PR; authoritative narrative is in `backlog.md` "Closed (recent)".

#### `claude/identify-failure-point-Duy02` (PR #21) — SPOF closeout

- **Graceful HTTP shutdown** on SIGTERM/SIGINT (30s drain). `terminationGracePeriodSeconds: 60` in chart.
- **Postgres reconnect-with-backoff at boot** — jittered exponential (500ms→30s, 5min budget). `livenessProbe.initialDelaySeconds: 60`.
- **`/health/live` + `/health/ready` split** with per-check breakdown. `/health` kept as back-compat alias.
- **Lazy OIDC discovery + JWKS-age signal** — atomic `verifier *atomic.Pointer[oidc.IDTokenVerifier]` with double-checked init.
- **`RunCoordinator` heartbeat + orphan sweep** — `internal/server/runs.go` tracks goroutines, drains 25s on shutdown. Migration `006_run_heartbeat.up.sql` adds `heartbeat_at` partial index.
- **`Config.Validate` multi-replica + builder guards** — refuses production `COOKER_BUILDER=docker`; refuses `replicaCount>1` + memory state without `COOKER_STICKY_SESSIONS=true`.
- **Helm defaults flipped to multi-replica safe** — kaniko + Redis WS hub + Redis WS tickets + Redis rate limit.
- **Redis pub/sub WS hub** — length-prefixed binary frame across replicas; jittered subscriber reconnect.
- **Resilience Prometheus counters** — `cooker_db_connection_errors_total`, `cooker_redis_connection_errors_total`, `cooker_jwks_fetch_failures_total`, `cooker_pipeline_runs_orphaned_total`. Alertmanager rules in `RUNBOOK.md`.
- **OCI distribution-spec conformance CI** — `registry:2` sidecar + upstream conformance binary.
- **Aegis "Workshop" frontend redesign** — full port: paper/coal/rust theme, shared atoms, Simple ⇄ Pro mode, every page re-laid.
- **`docs/ROLLOUT.md`** — operator UAT→production cutover playbook.

#### `claude/review-production-rollout-MT3YO` — P0 follow-up batch

- **P0.1** — OIDC lock-free fast path via `atomic.Pointer[oidc.IDTokenVerifier]`.
- **P0.2** — Redis WS hub subscriber resubscribe with backoff + 5s `Receive` timeout.
- **P0.3** — `time.NewTimer` + `Stop()` in DB backoff (replaces `time.After`).
- **P0.4** — parallel readiness checks via `errgroup`.
- **P0.5** — binary WS broadcast framing (~74 → 2 bytes of framing).
- **P0.6** — OCI conformance scope flipped to `workflow_dispatch` / `schedule` only.
- **P0.7** — OCI image-spec v1.1 structural schema validation.
- **P7** — `make uat-up-with-keycloak` (pre-seeded `cooker` realm) + `make test-e2e`.
- **P9.5 follow-up** — Buildah Helm chart wiring; CI matrix asserts docker-socket absent + buildah RBAC renders.

#### `claude/plan-weekly-features-WoB0S` (PR #25)

- **Per-role complexity + model frontmatter on `cooker-*` subagents.** Three Opus (planner, security, feature-dev), seven Sonnet.
- **Postgres retention CronJob (Helm)** — 90-day cutoff at 02:00 UTC daily; runs as UID 65532 with caps dropped; reuses `cooker.databaseUrlEnv` named template.

#### `claude/observability-week-1` (PR #26)

- **Per-stage live logs** — `model.Stage.Logs` populated by the executor, streamed over WebSocket.
- **App health** — `AppDetailPage` reads real status from `DeployTarget.Status`; deploy adapter surfaces `URL` on success.

#### `claude/docs-w10-w11` (PR #27) + bundled fixes (PR #28)

- **`docs/audits/W10-bug-and-chain-recheck.md`** — third pass at the bug + chain re-audit.
- **`docs/audits/W11-user-journeys.md`** (195 lines) — four-persona walkthrough; populated the "Discovered via user-journey W11" section in `backlog.md`.
- PR #28 ships bundled small fixes from the W10 audit.

#### Skills + agents harness

- `cooker-audit`, `cooker-find`, `cooker-improve`, `cooker-weekly`, `cooker-ci-debug`, `cooker-fix-bug`, `cooker-new-feature` skills under `.claude/skills/`.
- Per-role `cooker-*` subagents under `.claude/agents/` (planner, backend-api, backend-data, backend-adapters, frontend-ui, frontend-state, infra-ci, infra-deploy, security, feature-dev).

---

### Added — `claude/finish-backlog-priority-psf4D` (PR #19)

#### Toolchain (P6)

- **Go 1.22 → 1.25.** `backend/go.mod`, `deploy/docker/Dockerfile`, `.github/workflows/ci.yml` all moved together. `golang.org/x/time` unpinned to `v0.15.0`.
- **golangci-lint v1.59 → v2.0.** New v2-format `backend/.golangci.yml`. CI installs `golangci/golangci-lint-action@v6` with `version: v2.0.2`.
- **`gofmt -l` drift check** is now a CI step.

#### Observability (P4)

- **`log/slog` migration.** `cmd/cooker/main.go` installs a JSON `slog` handler as the default. Every `log.Print*` / `log.Fatal*` callsite across `backend/internal/server/`, `backend/internal/handler/`, `backend/internal/service/`, `backend/internal/config/`, `backend/internal/server/websocket.go` rewritten as structured `slog.Info|Warn|Error` calls.
- **Prometheus `/metrics`** via `internal/observability/observability.go` — exports `cooker_http_requests_total{method,route,status}` and `cooker_http_request_duration_seconds{method,route}`. Routes are labelled by Gin's matched template (e.g. `/api/v1/pipelines/:id`), not the concrete URL, to keep cardinality bounded. Opt in with `COOKER_METRICS_ENABLED=true`.
- **OpenTelemetry traces** via `otelgin` + OTLP/gRPC. Opt in with `COOKER_TRACING_ENABLED=true` + `COOKER_OTLP_ENDPOINT=host:port`. `Setup` returns a shutdown func that's invoked on `Server.Close`. Service-name and version are configurable via `COOKER_SERVICE_NAME` / `COOKER_SERVICE_VERSION`.

#### Multi-replica state (P3)

- **Redis-backed rate limiter** (`internal/server/ratelimit_redis.go`). GCRA via `github.com/go-redis/redis_rate/v10`; fail-open on Redis errors so a transient blip doesn't lock users out. Selectable via `COOKER_RATE_LIMIT_BACKEND=redis`.
- **Redis-backed WS ticket store** (`internal/server/wsticket_redis.go`). Atomic `GETDEL` (Redis 6.2+) so a single ticket can never be redeemed twice across cooker replicas. Selectable via `COOKER_WS_TICKET_BACKEND=redis`.

#### Secret backends (P2)

- **HashiCorp Vault** (`internal/secrets/vault/`). KV v2 mount + per-environment path. Auth via `VAULT_TOKEN` (works with Vault Agent injector). New env: `COOKER_SECRETS_VAULT_{ADDR,TOKEN,MOUNT,PREFIX}`.
- **AWS Secrets Manager** (`internal/secrets/awsm/`). One AWS secret per `<prefix>/<envID>/<key>`. Auth via the standard AWS chain (IRSA, instance profile, env vars). New env: `COOKER_SECRETS_AWS_{REGION,PREFIX}`.
- **GCP Secret Manager** (`internal/secrets/gcpsm/`). One GCP secret per `<prefix>__<envID>__<key>`. Auth via Application Default Credentials. New env: `COOKER_SECRETS_GCP_{PROJECT_ID,PREFIX}`.
- All three implement the `secrets.Manager` interface and slot into the existing `selectSecretsManager` via `COOKER_SECRETS_BACKEND={vault,aws,gcp}`. Production-mode `Validate()` enforces the required env per backend.

#### Native SDK adapters (P9.1)

- **BuildKit builder** (`internal/builder/buildkit.go`) — `github.com/moby/buildkit/client` v0.18.2 over gRPC. Drives `frontend=dockerfile.v0` solves; supports `BuildArgs`, `Platforms`, and progress streaming to `LogWriter`.
- **crane pusher** (`internal/pusher/crane.go`) — `go-containerregistry` `remote.Image` / `remote.Write` / `crane.Digest`. Auth keychain pulls from the request's `Auth` callback or falls back to `~/.docker/config.json` + cred helpers.
- **client-go deployer** (`internal/deployer/clientgo.go`) — k8s.io/client-go dynamic client + REST mapper + server-side apply with `FieldManager: cooker`. Handles multi-doc YAML.

#### Cloud deploy targets (P9.2)

- **Cloud Run** (`internal/deploytarget/cloudrun/`) — `cloud.google.com/go/run/apiv2` create/update + traffic-split rollback.
- **AWS ECS / Fargate** (`internal/deploytarget/ecs/`) — `aws-sdk-go-v2/service/ecs` register-task-def + create/update service + revision-based rollback.
- **Fly.io** (`internal/deploytarget/flyio/`) — REST against `api.machines.dev`. Auto-creates the fly app on first deploy.
- **Render** (`internal/deploytarget/render/`) — REST against `api.render.com/v1`. Triggers a deploy on an operator-created Render service.
- **Self-registration** in `internal/server/deploytargets.go` — each target only registers when its config block is non-empty so operators don't have to wire backends they don't use. New env vars: `COOKER_DEPLOY_CLOUDRUN_*`, `COOKER_DEPLOY_ECS_*`, `COOKER_DEPLOY_FLY_*`, `COOKER_DEPLOY_RENDER_*`.
- New `model.DeployTargetKind` values: `ecs`, `fly`, `render`.

#### GitOps + Buildah + OpenAPI

- **go-git GitOpsCommit** (`internal/gitops/gogit.go`) — full `github.com/go-git/go-git/v5` implementation. Auth resolution: SSH key path → ssh-agent → HTTPS basic. Each `Commit` clones to a temp dir, writes the file, commits with the configured author, and pushes.
- **Buildah builder** (`internal/builder/buildah.go`) — third in-cluster builder option alongside Kaniko and the docker.sock fallback. Submits a `batch/v1.Job` running `quay.io/buildah/stable`. Adds `CAP_SETUID` / `CAP_SETGID` for rootless user-namespace setup. Configurable storage driver (`vfs` | `overlay`). Selectable via `COOKER_BUILDER=buildah`.
- **swaggo/swag OpenAPI generation.** `make swagger` regenerates `backend/docs/api/swagger.{json,yaml,go}` from doc-comments. Flagship endpoints annotated; the full sweep is a low-friction follow-up.

### Notes for operators

- The new secret + deploy backends do **not** validate credentials at boot — they fail at first call. Watch for connection-error logs after switching backends.
- Cloud deploy targets (Cloud Run, ECS, Fly.io, Render) and secret backends (Vault, AWS, GCP) are unit-tested but have not been exercised against real cloud accounts in CI. End-to-end against a real provider is a follow-up.
- Tailscale `tsnet` transport (P9.4) remains build-tagged. `tailscale.com` v1.96+ requires Go ≥1.26 which isn't released stably; we pin to Go 1.25 to keep the runner image and module tooling in step. Revisit when Go 1.26 GAs.

### Added — `claude/finish-backlog-priority-psf4D` (PR #19, earlier commits)

**KeepSave follow-ups, OIDC, frontend UX, CI hygiene (P2/P3/P5/P6/P7).**

- **KeepSave Helm wiring** — `secrets.backend=keepsave` renders `COOKER_SECRETS_KEEPSAVE_{URL,PROJECT_ID,API_KEY}` (the API key via `secretKeyRef` into an operator-managed Secret); CI matrix asserts both happy-path and `apiKey-missing-fail`. Closes **P2.1** follow-up.
- **KeepSave secret promotion handler** — `POST /api/v1/environments/:id/secrets/promote` via the new `secrets.Promoter` interface; admin + MFA gated. Database backend returns 501 `ErrPromotionUnsupported`. Closes **P2.1** follow-up.
- **OIDC group-to-role mapping configurable** — `COOKER_OIDC_GROUP_MAP` (CSV `group:role,...`) overrides the default `cooker-{admins,operators,approvers,viewers}` mapping; surfaced as `oidc.groupRoleMap` in `values.yaml`. Closes **P3**.
- **Step-up MFA on destructive admin routes** — `auth.RequireMFA` middleware enforces a configured `acr`/`amr` claim on DELETE pipelines/envs/apps/hosts, secret reveal/put/delete/promote, and app webhook rotation. Empty `COOKER_OIDC_MFA_ACR_VALUES` disables the gate. Returns 403 `mfa_required` with `acr_values`; the frontend API client re-issues `signinRedirect({acr_values})` on the response. Closes **P3**.
- **Toast primitive + OIDC silent-renew toast** — Zustand-backed `toastStore` + `ToastViewport` mounted in `App.tsx`. `OIDCProvider` pushes a warning toast on `addSilentRenewError`. Closes **P5**.
- **WebSocket auto-reconnect with backoff** — `useWebSocket` exponential backoff (500ms → 30s) with fresh ticket fetch on each reconnect; opt-out via `reconnect.enabled=false`. Closes **P5**.
- **`gofmt -l` check + `golangci-lint` in CI** — repo-wide gofmt sweep + tuned `backend/.golangci.yml`. Closes **P6**.
- **`handler/network.go` and `handler/volume.go` cleanup** — write endpoints return HTTP 501 `{error,operation,hint}` instead of fake "pending" mock IDs; list endpoints return `[]` for empty-state UIs. Closes **P6**.
- **`docker-compose.uat.socketproxy.yml`** + `make uat-up-socketproxy` — opt-in `socketproxy` profile drops the host docker.sock bind mount and routes the cooker container at a hardened `tecnativa/docker-socket-proxy`. Closes **P7**.

### Added — earlier in `Unreleased`

- **Pluggable secrets backend** (`backend/internal/secrets/`). New `secrets.Manager` interface mirrors the existing builder/pusher/deployer strategy pattern; selectable at boot via `COOKER_SECRETS_BACKEND`. Closes backlog **P2.1**.
  - `database` adapter (default) wraps the historical AES-GCM + JSONB path; behavior is unchanged when this backend is selected.
  - `keepsave` adapter delegates storage to a [KeepSave](https://github.com/santapong/keepsave) server. Cooker's environment name maps to KeepSave's `environment` parameter; a single KeepSave project owns all of Cooker's secrets.
  - New env vars: `COOKER_SECRETS_BACKEND`, `COOKER_SECRETS_KEEPSAVE_URL`, `COOKER_SECRETS_KEEPSAVE_PROJECT_ID`, `COOKER_SECRETS_KEEPSAVE_API_KEY`.
  - Production startup validation extended to require KeepSave config when that backend is selected.
- **CI: `helm lint` + `helm template` + `kubeconform` job** in `.github/workflows/ci.yml`. Validates the chart against default and production-with-OIDC values on every push. Closes **P6.1**.
- **CI: `Register` returns error; `MustRegister` for init() callers** in `backend/internal/deploytarget/target.go`. Replaces the historical `panic` in `Register`. Tests cover both contracts. Closes the panic-removal item from **P6.2**.
- **Renovate config** at the repo root (`renovate.json`): weekly Mon-AM schedule, automerge minor/patch on green CI, major bumps gated on human review, custom regex manager for `KUBECTL_VERSION` ARG in the Dockerfile. Closes **P1.5**.
- **Helm chart values**: `ingress.tls`, `postgresql.sslMode`, and `secrets.backend` / `secrets.keepsave.*` blocks documented in `deploy/helm/cooker/values.yaml`. Chart-side rendering of `sslMode` and KeepSave env-var wiring are tracked as follow-ups.
- **Documentation:**
  - `docs/MULTI_REPLICA.md` — sticky-session + Redis-shared-state guide for multi-replica deploys, with NGINX/ALB/Traefik/HAProxy/Envoy examples. Closes the docs portion of **P3**.
  - `docs/RUNBOOK.md` — incident response runbook covering hung builds, Postgres down, OIDC unreachable, KeepSave outage, OOMKilled. Closes **P8** runbook.
  - `docs/adr/` — three accepted ADRs covering the strategy-pattern interfaces, the secrets-manager rationale, and the JSONB graph-storage decision. Closes **P8** ADRs.
  - `docs/openapi.yaml` — OpenAPI 3.1 sketch covering pipelines, runs, environments + secrets, apps + webhook, and the GitHub webhook entry point. Closes the OpenAPI sketch portion of **P8**; full generated spec via `swaggo/swag` remains a follow-up.
  - README §Deployment now documents TLS at ingress and Postgres SSL with concrete config snippets. Closes the docs portion of **P1.3** and **P1.4**.
  - README §Operations table indexes RUNBOOK, MULTI_REPLICA, SECURITY, and the backlog so operators land on the right doc faster.
- **Frontend `ErrorBoundary`** at the app root (`frontend/src/components/ErrorBoundary.tsx`, wired in `App.tsx`). Catches uncaught render errors so the React tree no longer crashes to a blank page; provides Try-again and Go-home recovery paths. Closes **P5** error-boundary item.
- **Frontend `Skeleton` + `SkeletonStack`** components (`frontend/src/components/Skeleton.tsx`). Shimmer-animated content placeholders. `ProtectedRoute` now uses a SkeletonStack while auth state restores instead of "Loading..." text. Closes the loading-skeletons portion of **P5**.

### Changed

- `handler.New(store, codec)` is now `handler.New(store, codec, secrets.Manager)`. Secret CRUD endpoints (`PutSecret`, `RevealSecret`, `DeleteSecret`) delegate to the configured Manager rather than touching `crypto.Codec` directly. Behavior on the wire is unchanged.
- The `requireCodec` middleware split into two gates: `requireSecrets` (Manager-presence check used by env-secret endpoints) and `requireCodec` (Codec-active check still used by App-webhook endpoints, which encrypt outside the Manager).

### Notes for operators

- Switching secrets backends does **not** auto-migrate existing secrets. Plan a one-shot copy step (read from old, write to new) before flipping `COOKER_SECRETS_BACKEND`.
- The `keepsave` adapter currently uses an internal HTTP client (`backend/internal/secrets/keepsave/client.go`) rather than the published Go SDK at `github.com/santapong/KeepSave/sdks/go`, because the SDK directory does not yet contain a `go.mod`. The client surface aligns with the SDK so a future swap is mechanical.
- Multi-replica deployments must apply sticky sessions (see `docs/MULTI_REPLICA.md`) until the Redis-backed rate limiter and ticket store land (open backlog item P3).

## [0.1.0] - 2026-03-21

### Added

#### Core Platform
- Initial project scaffolding with Go backend and React frontend
- `docker-compose.yml` for local development (frontend, backend, PostgreSQL, Redis)
- `Makefile` with build, test, lint, dev, and deploy targets
- GitHub Actions CI pipeline (backend test, frontend lint/build, Docker image build)

#### Backend (Go + Gin)
- HTTP API server with Gin framework and CORS middleware
- Pipeline CRUD endpoints (`/api/v1/pipelines`) with in-memory store (PostgreSQL-ready)
- DAG validation with cycle detection using Kahn's algorithm
- Pipeline execution engine with topological sort and parallel stage execution
- Reusable DAG runner package (`pkg/dagrunner`) with comprehensive tests
- Docker management endpoints (`/api/v1/docker/images`, `/api/v1/docker/containers`)
- Kubernetes management endpoints (`/api/v1/kubernetes/workloads`, namespaces, pods)
- OCI Registry endpoints following distribution-spec v1.1 (`/api/v1/registry`)
- Referrers API support for supply chain metadata (signatures, SBOMs)
- Multi-environment support (Dev/Staging/Production) with promotion API
- Environment CRUD endpoints with configurable auto/manual promotion policies
- SSO authentication via OIDC/OAuth 2.0 with PKCE flow
- RBAC middleware with admin, operator, viewer roles mapped from OIDC claims
- WebSocket hub for real-time streaming (pipeline runs, Docker builds, K8s watch)
- PostgreSQL schema with JSONB storage for pipeline graphs
- Database migrations (001_initial: pipelines, pipeline_runs, environments tables)
- Store interfaces and PostgreSQL implementation for pipeline persistence
- Health check endpoint (`/health`)

#### OCI Compliance
- OCI image-spec v1.1 types: Manifest, Index, Descriptor, Platform
- OCI media type constants with Docker compatibility types
- Manifest and Index validation functions
- Content-addressable digest computation (SHA-256)
- Helper functions for creating OCI Manifests and Image Indexes
- OCI utility package (`pkg/ociutil`) for parsing and inspecting manifests

#### Frontend (React + TypeScript + Vite)
- React Flow graph-based pipeline editor with drag-and-drop from toolbar
- Six custom node types: BuildNode, TestNode, DeployNode, PushNode, ApprovalNode, CustomNode
- ConditionalEdge component with visual labels (success/failure/always)
- Pipeline toolbar with draggable node palette and Run/Save/Validate actions
- Node configuration panel (slide-out form for editing stage config)
- Run history panel with status indicators
- Zustand stores for pipeline, Docker, Kubernetes, environment, and UI state
- Typed API client with `get`, `post`, `put`, `del` wrappers
- Separate API modules for pipelines, Docker, Kubernetes, and registry
- Pipelines list page with create and navigate to editor
- Pipeline editor page with React Flow integration
- Docker management page (images table, containers table)
- Kubernetes dashboard page (workloads table, namespace selector, scale/restart)
- Environments page with promotion flow visualization (Dev → Staging → Prod)
- OIDC authentication provider with React context
- Protected route component with role-based access checks
- WebSocket hooks (`useWebSocket`, `usePipelineExecution`, `useKubeWatch`)
- DAG validation utility (cycle detection, reference checking) on frontend
- OCI media type utilities with size formatting
- Dark theme UI with CSS custom properties
- Application layout with sidebar navigation and top bar
- Environment status badges in top bar (Dev/Staging/Production)
- React Router with page routing

#### Deployment
- Multi-stage Dockerfile (Node frontend build + Go backend build → Alpine runtime)
- Development Dockerfiles for frontend (Vite dev server) and backend (Go with air)
- Kubernetes manifests: Namespace, Deployment, Service, Ingress, ServiceAccount, RBAC
- Helm chart with Chart.yaml, values.yaml, and templates (deployment, service, helpers)
- Configurable Helm values for OIDC, Docker socket, K8s access, PostgreSQL, Redis

#### Documentation
- README.md with architecture overview, quick start, and feature list

### OCI Standards Referenced
- [OCI image-spec v1.1](https://github.com/opencontainers/image-spec) - Image Manifest, Image Index, Descriptors
- [OCI runtime-spec v1.2](https://github.com/opencontainers/runtime-spec) - Container runtime configuration
- [OCI distribution-spec v1.1](https://github.com/opencontainers/distribution-spec) - Registry API, referrers API

### Technical Notes
- Backend uses in-memory stores for MVP; PostgreSQL store layer is implemented and ready for wiring
- Docker, Kubernetes, and Registry handlers are structured with placeholder implementations; service layer integration with Docker SDK, client-go, and go-containerregistry is the next step
- OIDC token validation uses placeholder parsing in dev mode; production wiring with `go-oidc` is prepared

[Unreleased]: https://github.com/cooker-ci/cooker/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/cooker-ci/cooker/releases/tag/v0.1.0
