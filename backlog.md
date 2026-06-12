# Cooker Backlog

Tracks work that's been planned, scoped, or hinted at by the codebase but isn't shipped yet. Living document — when an item lands on `main`, remove it (or move it to the changelog).

Items are grouped by area and roughly prioritized within each group.

> Strategic framing — the feature roadmap for OSS adoption, monetization strategy, and UAT→production hosting recommendation — lives in [`docs/product-plan.md`](docs/product-plan.md).

---

## Production readiness summary

After PR #21 (the SPOF closeout) lands, Cooker is **production-quality with no known SPOF in the boot path**. Beyond what the previous round closed, the binary now: (a) survives Postgres being down at boot via jittered backoff, (b) survives the IdP being unreachable via lazy OIDC discovery, (c) drains in-flight HTTP and pipeline runs on SIGTERM, (d) sweeps orphaned `running` rows on every boot, (e) supports multi-replica via Redis-backed WS hub / ticket store / rate limiter (the chart defaults to all three), (f) refuses to start in production with `BUILDER=docker` or with multi-replica + memory backends without sticky sessions. The four resilience metrics needed to alert on degradation are exposed on `/metrics`.

The `Config.Validate` guards mean misconfigurations now fail loudly at boot rather than silently in production.

### Deployment-shape readiness matrix

| Shape | Verdict |
|---|---|
| **Single-replica + TLS ingress + Kaniko + Postgres SSL + edge rate limit** | ✅ Production-ready. Ship it. |
| **Single-replica + TLS ingress + still using `docker.sock`** | ❌ Refused at boot post-PR #21 (`Config.Validate`). Switch to Kaniko. |
| **Multi-replica + Redis-backed (default) + TLS + Kaniko** | ✅ Production-ready for HA. Default chart values shape. |
| **Multi-replica + sticky sessions + memory backends + TLS + Kaniko** | ✅ Production-ready for HA (sticky sessions satisfies the new `Config.Validate` guard). Less operationally clean than Redis. |
| **Multi-replica + memory backends without sticky sessions** | ❌ Refused at boot post-PR #21. Either set `COOKER_STICKY_SESSIONS=true` or flip the backends to redis. |
| **Anything without TLS + OIDC** | ❌ Sign-in flow won't work — most IdPs refuse non-HTTPS redirect URIs. |

### Operator-side concerns (still your call)

The chart can't make these decisions for you:

1. **TLS at ingress** is required for OIDC. Cooker doesn't terminate TLS itself; provision a cert with cert-manager (or equivalent) and reference it in `ingress.tls`. The chart now refuses to render if `cookerEnv=production AND oidc.enabled=true AND ingress.enabled=true AND ingress.tls is empty`.
2. **Builder choice** — set `builder.kind=kaniko` in production. The `docker` builder still ships for single-node test clusters but gives the Cooker container root-equivalent access to the host Docker daemon. The chart conditionally drops the `docker.sock` mount when `builder.kind != "docker"`.
3. **Multi-replica state** — rate limiter and WebSocket ticket store are per-process. Pin sticky sessions at ingress (works fine; documented in `docs/MULTI_REPLICA.md`) or implement Redis-backed versions (P3).
4. **Postgres SSL** — `?sslmode=` now renders into `DATABASE_URL` from `postgresql.sslMode` (default `require`). Set `database.host` to a TLS-capable Postgres for the chart to wire it through.
5. **Audit destination** — `COOKER_AUDIT_DESTINATION=stdout` (the default) routes via the cluster log stack. Set `COOKER_AUDIT_DESTINATION=file` + `COOKER_AUDIT_FILE_PATH` if you'd rather pair with a sidecar tail-shipper. The value is a comma list — add `db` (e.g. `db,stdout`) for the queryable `/admin/audit` viewer backed by Postgres (async drop-on-full writer; daily `COOKER_AUDIT_DB_RETENTION` sweep, default 90 days; production requires `DATABASE_URL`).

### What "OCI compliance" means here

`README.md` and `architecture.md` claim conformance with all three OCI specs. The pusher path (`internal/pusher/crane.go` via `go-containerregistry`) is now exercised against the upstream [OCI distribution-spec conformance suite](https://github.com/opencontainers/distribution-spec/tree/main/conformance) via `.github/workflows/oci-conformance.yml` — the workflow boots a `registry:2` sidecar, has Cooker push a freshly-built image to it, then runs the upstream conformance binary against the populated registry. The `/api/v1/registry/...` proxy endpoints in `internal/handler/registry.go` are still stubs and are NOT covered by conformance — they're a separate (smaller) workstream.

---

## Open items

What's left, organised by priority. All "blocked-on-bigger-PR" items have a one-line rationale for why they didn't ship in PR #17 and what unblocks them.

### Owner-requested (2026-06-11)

#### OR-1 — Pre-release / canary deploy mode (M, needs mini-ADR)

✅ **Shipped** on `claude/canary-deploys` — see "Closed (recent)". Mechanism chosen: K8s **replica-weighted** split (a `<name>-canary` Deployment sized to the traffic weight runs alongside the stable one behind a single Service); target scope Kubernetes-only (non-K8s → 422). Auto-promote-after-health-window and manual promote/abort both land; header-routed preview / Argo-Rollouts deferred.

#### OR-2 — Cloud-resource management around deployments (vision: cloud management platform; XL, ADR-gated)

When Cooker deploys to a target (EC2 host, ECS, EKS), surface and manage the cloud resources *related to that deployment* — instance state, load balancer/target health, attached storage, linked database, per-App cost via cost-allocation tags — so day-2 operations happen in Cooker instead of the AWS console. **Scope guard (from the 2026-06-11 assessment):** a general AWS-console replacement is a product pivot with a large credential blast radius and entrenched competitors; the defensible slice is *resources Cooker itself created or deploys onto*, read-only first. Suggested phasing: (1) read-only inventory + cost panel per App (builds on Pod Identity + the `awsm` adapter + DEPLOY-AWS-VERCEL groundwork), (2) safe lifecycle ops (restart/scale) behind the existing MFA step-up, (3) only then evaluate broader management. Each phase gets its own ADR.

### W6 carry-forward (queued)

Items deferred from the W2–W5 cycle. Sequenced for the next session.

#### W6.1 — Primitive #1: per-stage retry policies

✅ **Shipped** on `claude/feat-pipeline-power` (roadmap M2) — see "Closed (recent)". Field shape landed as `StageConfig.Retry {maxAttempts, initialMs, maxMs, exponential}` (JSONB — no migration needed; legacy `retries` int still honoured); `RetryOn` classification stayed with the executor's existing transient-vs-ctx classifier.

#### W6.2 — useStageLogs reconnect backfill (~1 hour, HIGH severity)

✅ **Shipped** on `claude/jolly-knuth-j396dx` (PR #108). `useWebSocket` exposes `onReconnect`/`reconnectCount`; `useStageLogs` re-issues `getStageLogs` on each reconnect and merges via the new seq-deduped `stageLogBuffer` module; vitest covers the drop-mid-run scenario from `docs/audits/2026-05-usestagelogs-reconnect.md` (no lines lost, none duplicated).

#### W6.3 — README screenshots (operator step)

- [ ] Capture three PNGs into `docs/images/`: pipeline-editor.png, run-view.png, app-detail.png.
- [ ] Update the placeholder Screenshots table in `README.md` to reference them.

#### W6.4 — Branch cleanup (operator step)

- [ ] ~80 stale branches on origin from merged PRs. Sandbox proxy refuses `git push --delete`. Run locally:
  ```bash
  gh pr list --state merged --limit 200 --json headRefName -q '.[].headRefName' \
    | xargs -I{} git push origin --delete {}
  ```
- [ ] Or enable Settings → General → Pull Requests → "Automatically delete head branches" for future PRs.

---

### P0 — SPOF closeout follow-ups (PR #21 spillover)

All P0.1–P0.7 items are now closed (see "Closed (recent)" below). P0.8 (operator rollout playbook) shipped in PR #21.

#### P0.8 — Operator rollout playbook

✅ **Shipped** as `docs/ROLLOUT.md` in PR #21. Single source of truth for UAT → production cutover; cross-references RUNBOOK.md, MULTI_REPLICA.md, SECURITY.md.

---

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
  - [x] Render KeepSave env-vars + `secretKeyRef` in the Helm `deployment.yaml` (with CI matrix asserting both happy-path and apiKey-missing-fail).
  - [ ] Swap the internal HTTP client for the published Go SDK (currently lacks a `go.mod`).
  - [x] Surface KeepSave's `/promote` endpoint as `POST /api/v1/environments/:id/secrets/promote` via the new `secrets.Promoter` interface.
- [x] **HashiCorp Vault adapter** — `internal/secrets/vault` against KV v2; selectable via `COOKER_SECRETS_BACKEND=vault`.
- [x] **AWS Secrets Manager adapter** — `internal/secrets/awsm` using `aws-sdk-go-v2`; one AWS secret per `<prefix>/<envID>/<key>`.
- [x] **GCP Secret Manager adapter** — `internal/secrets/gcpsm` using `cloud.google.com/go/secretmanager`; secrets named `<prefix>__<envID>__<key>`.

---

### P3 — Auth and authorization extensions

- [x] **Sticky-session docs** — `docs/MULTI_REPLICA.md` covers NGINX/ALB/Traefik/HAProxy/Envoy.
- [x] **Redis-backed rate limiter + WS ticket store** — `internal/server/ratelimit_redis.go` (GCRA via `go-redis/redis_rate/v10`) and `wsticket_redis.go` (atomic GETDEL on Redis 6.2+). Toggle via `COOKER_RATE_LIMIT_BACKEND=redis` and `COOKER_WS_TICKET_BACKEND=redis`.
- [x] **MFA / step-up auth at the IdP.** `auth.RequireMFA` middleware checks the token's `acr` (or `amr`) against `COOKER_OIDC_MFA_ACR_VALUES`; applied to admin destructive routes (DELETE pipelines/envs/apps/hosts, secret reveal/put/delete/promote, app webhook rotation). Frontend re-issues `signinRedirect({ acr_values })` on the 403 mfa_required response.
- [x] **OIDC group-to-role mapping configurable.** `COOKER_OIDC_GROUP_MAP` (CSV of `group:role` pairs) overrides the default `cooker-admins → admin` mapping; empty falls back to defaults. Surfaced in the Helm chart as `oidc.groupRoleMap`.

---

### P4 — Observability

- [x] **Prometheus `/metrics`** — `internal/observability` exposes `cooker_http_requests_total` + `cooker_http_request_duration_seconds`; opt in via `COOKER_METRICS_ENABLED=true`.
- [x] **OpenTelemetry traces** — same package configures an OTLP/gRPC exporter via `COOKER_OTLP_ENDPOINT` and wires `otelgin` middleware when `COOKER_TRACING_ENABLED=true`.
- [x] **`log/slog` migration** — `cmd/cooker/main.go` installs a JSON handler as the default; all `log.Print*`/`log.Fatal*` callers across `internal/server/`, `internal/handler/`, `internal/service/`, `internal/config/`, `internal/server/websocket.go` rewritten as structured `slog.Info|Warn|Error` calls.

---

### P5 — Frontend UX

- [x] **Sign-in landing page theme + full-app workshop redesign.** Closed by the Aegis frontend port — see "Closed (recent)" below.
- [x] **Loading skeletons** — `Skeleton` + `SkeletonStack` shipped. `ProtectedRoute` uses them during auth restore.
- [x] **App-root error boundary** — `ErrorBoundary` shipped.
- [x] **Toast primitive + OIDC silent renew toast.** `frontend/src/stores/toastStore.ts` (Zustand) + `components/Toast.tsx` viewport mounted in `App.tsx`. `OIDCProvider` pushes a warning toast on `addSilentRenewError`.
- [x] **WebSocket auto-reconnect with backoff.** `useWebSocket` exponential backoff (default 500ms → 30s) with fresh ticket fetch on each reconnect; opt-out via `reconnect.enabled=false`.

---

### P6 — Backend code quality and CI

- [x] **`helm lint` + `helm template` + `kubeconform`** — shipped.
- [x] **`deploytarget.Register` returns error; `MustRegister` for init() callers.**
- [x] **`gofmt -l` check in CI** + repo-wide gofmt sweep that normalised pre-existing drift.
- [x] **`golangci-lint` in CI** with a tuned `backend/.golangci.yml` (errcheck, govet, ineffassign, staticcheck, unused, gosimple, bodyclose, misspell, unconvert).
- [x] **Go version bump to 1.25** — `go.mod`, `deploy/docker/Dockerfile`, and `.github/workflows/ci.yml` all moved together. `golang.org/x/time` unpinned to `v0.15.0`. `golangci-lint` config migrated from v1.59 to v2.0 with a v2-format `.golangci.yml`.
- [x] **Replace `internal/handler/network.go` and `internal/handler/volume.go` placeholders.** Write endpoints now return HTTP 501 with a structured `{error,operation,hint}` payload instead of fake "pending" mock IDs; list endpoints return `[]` so empty-state UIs render. Tracked-forward note: full SDK wiring still needs the host transport (P9.4) before write paths can do real work.

---

### P7 — UAT and dev experience

- [x] **`tecnativa/docker-socket-proxy` overlay** at `docker-compose.uat.socketproxy.yml` + `make uat-up-socketproxy`. Opt-in via the `socketproxy` compose profile so the default `make uat-up` keeps working unchanged.
- [x] **`make uat-up-with-keycloak`** — Keycloak compose overlay (`docker-compose.uat.keycloak.yml`) with pre-seeded realm `cooker` (admins+viewers groups, alice/alice = admin, bob/bob = viewer). Uses `host.docker.internal:8081` so browser and backend share the same issuer URL.
- [x] **`make test-e2e`** — boots `make uat-up`, drives a deterministic pipeline (one no-op `custom` stage) through the API via curl/jq, asserts terminal status `success`, tears down on exit. Implementation: `scripts/e2e/run.sh`.

---

### P8 — Documentation

- [x] **OpenAPI spec** at `docs/openapi.yaml` — now **full route coverage** (all ~100 operations across pipelines, runs, apps, environments + secrets, docker, kubernetes, registry, hosts, templates, settings, admin, all four webhooks, and the WebSocket endpoints), hand-maintained in lockstep with `backend/internal/server/router.go` and validated against the OpenAPI 3.1 schema. Human-readable mirror at `docs/system-design/14-api-reference.md`.
- [x] **Generated OpenAPI** via `swaggo/swag` — `make swagger` regenerates `backend/docs/api/swagger.{json,yaml,go}` from doc-comments. Flagship endpoints (pipeline list / run, env list, secret put / promote) are annotated; the rest can be filled in incrementally as a low-friction follow-up.
- [x] **Incident runbook** at `docs/RUNBOOK.md`.
- [x] **ADRs 0001-0003** at `docs/adr/`.
- [x] **Run the OCI distribution-spec conformance suite** against Cooker-pushed images via a `registry:2` sidecar in CI. Re-framed from the original `/registry` proxy plan because those handlers are stubs; conformance against stubs is meaningless. The pusher path is the meaningful surface and is now covered. The `/registry` proxy story is tracked separately as a follow-up.

---

### P9 — Native SDK adapters and additional deploy targets (not blockers)

> Each item below has a working CLI fallback today (or is an additive new capability). Native rewrites give lower latency, fewer external CLI dependencies in the container, and richer error reporting — all nice-to-have, none required.

#### P9.1 — Replace CLI shell-outs with native Go SDKs

| File | Today | Status |
|---|---|---|
| `backend/internal/builder/buildkit.go` | `github.com/moby/buildkit/client` v0.18.2 | ✅ wired |
| `backend/internal/pusher/crane.go` | `github.com/google/go-containerregistry` (`remote.Image`/`remote.Write`/`crane.Digest`) | ✅ wired |
| `backend/internal/deployer/clientgo.go` | `k8s.io/client-go` dynamic client + server-side apply | ✅ wired |

All three use lazy initialisation so a process without registry / cluster reach still boots; errors surface at first call.

#### P9.2 — Additional deploy targets

| Adapter | Status | Underlying SDK |
|---|---|---|
| Cloud Run | ✅ wired (`internal/deploytarget/cloudrun/`) | `cloud.google.com/go/run/apiv2` |
| AWS ECS / Fargate | ✅ wired (`internal/deploytarget/ecs/`) | `github.com/aws/aws-sdk-go-v2/service/ecs` |
| Fly.io | ✅ wired (`internal/deploytarget/flyio/`) | REST API `https://api.machines.dev` |
| Render | ✅ wired (`internal/deploytarget/render/`) | REST API `https://api.render.com/v1/` |

Adapters self-register at boot only when their config block is non-empty. Operators don't need to wire backends they don't use.

**Caveats:** none of these have been exercised against real cloud accounts in CI — the unit tests assert the SDK calls fire correctly but expect transport errors when credentials are absent. End-to-end against a real GCP/AWS/Fly/Render project is a follow-up.

#### P9.3 — GitOpsCommit node

`backend/internal/gitops/gogit.go` — ✅ implemented via `github.com/go-git/go-git/v5`. Auth resolution: `SSHKeyPath` → ssh-agent → HTTPS basic. Each `Commit` clones to a temp dir, writes the file, commits, and pushes; conflict-retry is intentionally minimal (one fast-forward retry — anything more belongs in a controller layer).

#### P9.4 — Tailscale `tsnet` transport

`backend/internal/transport/tsnet/` is still build-tagged (`-tags tsnet`). **Blocker:** `tailscale.com` v1.96.x requires Go ≥ 1.26 which isn't released yet stably; the cooker build pins to Go 1.25 to keep the runner image (`golang:1.25-alpine`) and Go module tooling in step. Re-evaluate when Go 1.26 is GA, then either pin tailscale to a version compatible with Go 1.25 or do the bump in lockstep.

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

**Status:** ✅ shipped end-to-end. `backend/internal/builder/buildah.go` mirrors the Kaniko Job pattern, adds CAP_SETUID/CAP_SETGID and the storage-driver knob (`COOKER_BUILDAH_STORAGE_DRIVER`, default `vfs`). Helm chart wiring landed in `claude/review-production-rollout-MT3YO`: `builder.buildah.{image, namespace, serviceAccount, contextPVC, storageDriver}` values, `templates/rbac.yaml` gate extended to render a `cooker-buildah-builder` Role + RoleBinding when `builder.kind=buildah`, deployment template renders the `COOKER_BUILDAH_*` env-vars and the optional context PVC mount. CI matrix asserts (a) docker-socket is absent, (b) buildah RBAC + env-vars render, (c) kaniko env-vars don't leak. SECURITY.md "image build isolation" section updated with the PSA caveat.

**Original notes (kept for reference):**

**Files added:** `backend/internal/builder/buildah.go`.

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

## Discovered via user-journey W11

These items were surfaced by the persona walkthroughs in `docs/audits/W11-user-journeys.md`. Each one cites the persona who hit the friction so reviewers can re-read the original walkthrough. Tier guesses are conservative — promote / demote in a future planning round.

### P1 — high-leverage cross-persona

- [ ] **Tenant scoping** — design-doc gate first. Either data-scoped (`owner_team_id` on every Pipeline / App / Environment) or namespace-scoped (a "Cooker namespace" wrapping a slice of resources visible to a subset of OIDC groups). Multi-week feature; needs an ADR before code. Surfaced by Enterprise SRE. W11 §Enterprise step 4.
- [ ] **Per-App `runDeadline` override.** The per-Pipeline half shipped in roadmap M2 (`Pipeline.RunDeadline`, clamped [10s,24h], editor field). App deploys still use the cluster default — promote to `model.App` when a real app-deploy exceeds it. W11 §ML step 5.

### P2 — single-persona high-value

- [ ] **First-run empty-state CTAs** on Apps / Pipelines / Environments. Narrate the "create deploy target → import app → see deploy" sequence. Surfaced by Indie hacker (acute) + SaaS team (less acute). W11 §Indie step 2.
- [ ] **Bulk import** "import all repos from this GitHub org as Apps". Surfaced by SaaS team. W11 §SaaS step 4.
- [ ] **Per-environment secret diff view** (`Staging vs Prod`). Same UX shape as `git diff` for env-vars. Surfaced by SaaS team. W11 §SaaS step 7.
- [ ] **Approver pre-warning** for step-up MFA. Show a badge before they click "approve" so the 403 → re-auth round-trip isn't a surprise. Surfaced by SaaS team. W11 §SaaS step 3.
- [ ] **Production-readiness checklist surfaced in-product** on first boot (read from `launch-readiness.md` or hardcoded). Surfaced by Enterprise SRE. W11 §Enterprise step 1.
- [ ] **Per-team RBAC.** Extend `groupRoleMap` to allow scoped grants like `auth-admin: admin in tenant=auth-team`. Depends on the Tenant Scoping P1 above. Surfaced by Enterprise SRE. W11 §Enterprise step 4.
- [ ] **Surface "deployed to cluster X (namespace Y)"** prominently on AppDetailPage and Run page header. Surfaced by Enterprise SRE. W11 §Enterprise step 5.
- [ ] **Append-only / write-once audit-log adapter.** Eg. AWS CloudWatch with no-delete IAM policy, or a write-once S3 backend. New `audit.Sink` impl. Surfaced by Enterprise SRE. W11 §Enterprise step 6.
- [ ] **Kaniko / Buildah Job `nodeSelector` + `tolerations`** chart values, threaded through to the Job spec. Surfaced by AI/ML engineer. W11 §ML step 6.
- [ ] **`DeployTarget.NodeSelector` + `DeployTarget.Tolerations` model fields,** written through to Kubernetes deployer's Deployment spec. Surfaced by AI/ML engineer. W11 §ML step 7.

### P3 — speculative / low-leverage

- [ ] **"Use the bundled k3s" easy-button** on the New App wizard / Hosts page. Auto-wires Cooker's own cluster as a deploy target without operator setup. Surfaced by Indie hacker.
- [ ] **PR-preview environments.** Per-PR ephemeral environments with unique subdomains. Multi-week feature; needs design doc. Surfaced by Indie hacker.
- [ ] **Bulk webhook-secret rotation** across N selected apps. Surfaced by SaaS team.
- [ ] **Helm `groupRoleMap` schema validation** — refuse boot with typo'd role names. Surfaced by SaaS team.
- [ ] **Reduce conceptual cost of Apps-vs-Pipelines** for "I just want a per-repo pipeline" cases. A `make-pipeline-for-app` button on AppDetailPage. Surfaced by SaaS team.
- [ ] **SAML auth method** alongside the existing OIDC. Some legacy enterprise IdPs are SAML-only. Surfaced by Enterprise SRE.
- [ ] **`/me/admins` dashboard** listing every user with admin role today (sourced from OIDC groups + `groupRoleMap`). Surfaced by Enterprise SRE.
- [ ] **Document the `/health/ready` rate-limiting decision** in `SECURITY.md` to short-circuit pen-test reports. Surfaced by Enterprise SRE.
- [ ] **First-class ML stage type** (`StageTypeMLPull`?) with `dvc` / `huggingface` provider plugins. Surfaced by AI/ML engineer; gate on real demand.
- [ ] **Document the GitHub clone → PVC staging path** end-to-end in `docs/architecture.md`. Surfaced by AI/ML engineer.

---

## Closed (recent)

Items that landed in the `claude/uat-ready-*` PR series, PR #6, the `claude/cooker-backlog-readme-com8z` PR (#17), the `claude/complete-p1-backlog-qN4FP` PR, the `claude/finish-backlog-priority-psf4D` PR, the `claude/implement-frontend-design-XVxz2` PR (the Aegis frontend port), the `claude/identify-failure-point-Duy02` PR (#21, the SPOF closeout), the `claude/review-production-rollout-MT3YO` PR (P0 follow-up batch), the `claude/plan-weekly-features-WoB0S` PR (weekly: agent-team complexity + retention CronJob), the `claude/frontend-bundle-split` PR (route-level lazy-load + Vite manualChunks), the `claude/w3-t1-t3-handler-f1` PR, the `claude/w4-t4-edge-condition-refuse` PR, the `claude/w4-f04-created-at` PR, the `claude/w4-t2-logwriter-push-deploy` PR, the `claude/w5-ci-cache-mode-min` PR, the `claude/w5-adr-0004-finalize` PR, the `claude/w5-f3-parse-compose-graph` PR, the `claude/w5-security-drift-bundle` PR, the `claude/w5-f2-executor-runresult` PR, and the `claude/fervent-sagan-q50XA` branch:

### `claude/canary-deploys` — canary deployments (OR-1)

- ✅ **Canary deploy mode (opt-in per app)** — `model.CanaryConfig` (strategy `rolling`|`canary`, weight 1–99, auto-promote, health-window) on `App` + live `model.AppCanary` state; migration 024 adds `apps.canary_config` (JSONB) and the `app_canaries` table (partial unique index → one progressing canary per app, mapped to 409). `store.AppCanaryStore` in postgres + memory. `service.CanaryService`: `Start` builds+pushes the new image (new `AppDeployer.BuildAndPushImage`, a deploy-stage-stripped build) then establishes the split via `deployer.WeightedDeployer`; `Promote` → 100%, `Abort` → 0%, and a background `RunSweeper` auto-promotes a healthy canary past its window (or auto-rolls-back an unhealthy one via the app `Prober`); manual canaries wait for an operator.
- ✅ **Weighted-traffic deployer capability** — optional `deployer.WeightedDeployer` (embeds `Deployer`, adds `DeployWeighted`) implemented by `Kubectl` + `ClientGo` via replica weighting (a `<name>-canary` Deployment proportional to the weight behind one Service); `splitReplicas` keeps ≥1 pod on each side mid-rollout. Non-K8s backends don't implement it → typed `ErrCanaryUnsupported` → HTTP 422.
- ✅ **API + UI** — `DeployApp` branches to the canary path when the app opts in (response carries `"strategy"`); `GET /apps/:id/canary`, `POST /apps/:id/canary/{promote,abort}` (writeRole); live state embedded under `activeCanary` on `GET /apps/:id`. AppDetailPage gains a Deploy-strategy card (toggle + weight/auto-promote/window, saved via update; hint when target isn't K8s) and a Canary-rollout panel (traffic %, healthy/unhealthy, Promote/Abort), polling at 5s while progressing. Docs: design.md §2.1/§11 (optional-capability pattern) + UAT Scenario 1b. No new env var.

### `claude/feat-enterprise` — queryable audit trail + secrets connectivity test (roadmap M5)

- ✅ **Audit DB sink + admin viewer (W11 §SaaS step 6 + §Enterprise step 6)** — migration 019 `audit_events` (indexes `(time DESC)`, `(user_sub, time DESC)`); `store.AuditEventStore` in postgres + bounded-ring memory (~10k); `audit.NewStoreSink` (async, drop-on-full — same contract as the file sink; middleware and `Sink` untouched) + `NewMultiSink` so `COOKER_AUDIT_DESTINATION` takes comma lists (`db,stdout`); daily retention sweep via `COOKER_AUDIT_DB_RETENTION` (default 90 days, boot sweep + 24h ticker); `Config.Validate()` requires `DATABASE_URL` for `db` in production, memory-store fallback warns at boot. `GET /api/v1/admin/audit` (admin + MFA; from/to/user/method/path-prefix filters, limit ≤ 200) + `/admin/audit` viewer page (filter row, status-tone table, prev/next).
- ✅ **Secrets connectivity test (W11 §Enterprise step 2)** — `service.CheckSecrets` probes the configured backend with one `List` call (10s cap) and classifies auth-failed (KeepSave 401/403) / unreachable (timeout, net) / other; `POST /api/v1/settings/secrets/test` (adminRole + explicit MFA on the otherwise-MFA-less settings group; 409 with no environments, 503 with no backend; probe failure is 200-body data). Settings → Secrets tab with env select + result card. Reveals backend kind + reachability only — never key names/values.

### `claude/feat-reliability` — deploy history/rollback + drift + run diff (roadmap M3)

- ✅ **Deploy history + one-click rollback** — migration 018 `app_deploys` (append-only, `(app_id, created_at DESC)` index); `store.AppDeployStore` in postgres + memory; `AppDeployer` records terminal deploys/rollbacks best-effort and gains `DeployImage` (deploy-only single-stage pipeline via the existing synthesized manifest — no clone/build/push). `GET /apps/:id/deploys`; `POST /apps/:id/rollback` (writeRole + rate-limit + idempotency + governance — a rollback IS a deploy); default target = second most-recent successful single-image deploy, explicit `deployId` supported; non-k8s/compose → 409. AppDetailPage "Deploy history" card with per-row Roll back.
- ✅ **Drift detection (on-demand v1)** — `service.CheckDrift` compares the newest successful recorded image against the live Deployment image via the read-only kube client (exact-ref compare; `unknown` on missing history/unreachable cluster; `unsupported` off k8s). `GET /apps/:id/drift` (writeRole — live cluster state); AppDetailPage `in sync`/`drift` pill.
- ✅ **Run diff vs last green** — pure `service.BuildRunDiff` (per-stage status/duration/digest deltas + outputs-changed keys + variables diff + `pipeline_version` delta from M2's stamp); `GET /pipelines/:id/runs/:runId/diff?against=last-success|<runId>`; RunPage pro-mode "Diff vs last green" rail section. Per-stage configChanged deferred (runs don't snapshot stage config).

### `claude/feat-insight` — AI failure triage + stage-duration analytics (roadmap M4)

- ✅ **AI failure triage (opt-in)** — new `internal/triage` Anthropic Messages API client (keepsave-pattern: TLS≥1.2, 90s timeout, typed `APIError` + sentinels, one retry on 429/5xx; request omits sampling/thinking params). `BuildRequest` sends stage summary + error + last-32KiB log tail with env values/secret refs stripped. `POST /pipelines/:id/runs/:runId/stages/:stageId/triage` (writeRole + rate limit; failed stages only → 409 otherwise; 503 when disabled). Config `COOKER_AI_TRIAGE_ENABLED` / `COOKER_AI_TRIAGE_MODEL` (default `claude-fable-5`) / `ANTHROPIC_API_KEY` with a fail-fast Validate gate. `GET /api/v1/capabilities` advertises the feature; RunPage shows "Why did this fail?" on failed stages with a dismissible advisory card. SECURITY.md documents the egress posture.
- ✅ **Stage-duration analytics** — pure `service.ComputeAnalytics` (per-stage p50/p95/avg nearest-rank percentiles + success rates + per-run series; running/skipped excluded from samples); `GET /pipelines/:id/analytics?runs=N`; new dependency-free `Sparkline` SVG component + `/analytics` Insights page (pro-mode sidebar entry).

### `claude/feat-pipeline-power` — retry policies + run deadline + edge conditions (roadmap M2)

- ✅ **Primitive #1 — structured retry policies (W6.1)** — `StageConfig.Retry {maxAttempts, initialMs, maxMs, exponential}` (JSONB, no migration; legacy `retries` int still honoured, structured policy wins). Executor `policyFromStage` clamps to [1,10] attempts / [100ms,60s] initial / [initial,5m] max; `exponential=false` pins constant delay; approval/custom/test never retry. `validate.RetryPolicy` rejects out-of-range payloads at save. Editor: "Retry" section on build/push/deploy panels.
- ✅ **Per-Pipeline `runDeadline` override (W11 §ML step 5, pipeline half)** — migration 017 adds `pipelines.run_deadline` + `pipeline_runs.pipeline_version` (the version stamp feeds M3's run-diff). `validate.RunDeadline` ([10s,24h]); `service.PipelineRunDeadline` clamps; `RunCoordinator.SpawnWithDeadline` (additive — `Spawn` unchanged) applies it on the inline path and `jobqueue_runner` wraps Execute's ctx on the durable path. Editor toolbar "Run deadline" field.
- ✅ **Primitive #2 — edge conditions + skipped status** — `EdgeAllows`/`StageShouldRun` (AND-join; skip propagates through success/failure edges; only `always` passes a skipped upstream) in `internal/buildplan/edges.go`; `dagrunner.ErrSkipped` + `NewRunnerBoundedContinue` (continue-through-failure, first error returned at the end, ctx-cancel still aborts); executor gates each taskFunc and stamps terminal `RunStatusSkipped` (runstate gains Pending→Skipped). Validation now accepts success/failure/always and rejects unknowns (replaces T4's refusal). Editor: click an edge to cycle the condition; RunPage renders `skipped` neutral. Rollback knob `COOKER_EDGE_CONDITIONS_ENABLED=false` restores legacy abort-on-first-failure. **Behaviour change:** parallel branches now complete after an unrelated failure.

### `claude/feat-build-cache` — build layer cache + recipe auto-detect (roadmap M1)

- ✅ **Build-cache plumbing (W11 §ML step 4 + 9)** — `CacheSpec{mode,ref,inline}` on `StageConfig.Cache` + `builder.Request.Cache`; Kaniko appends `--cache=true --cache-repo=`, Buildah appends `--layers --cache-from/--cache-to` as discrete `$@` argv entries (a single env var would not word-split under the script's `IFS=$'\n'`), BuildKit sets registry `CacheImports/CacheExports` (`inline` → `mode=max` export; image-exporter `push` deliberately untouched), docker-sock logs "unsupported" and ignores. `validate.CacheSpec` enforces a strict registry-ref grammar (shell-safety gate, same class as T1). App-deploy synthesized build stages pick up `COOKER_BUILD_CACHE_REPO` (chart: `builder.cache.{enabled,ref}` with a `required` guard + CI render-matrix case). Editor: build-stage "Layer cache" section. Docs: `docs/build-cache.md`.
- ✅ **Build-recipe auto-detect (W11 §Indie step 3)** — `POST /api/v1/apps/detect-build` (writeRole + rate-limited): shallow clone via `internal/source/github` + the existing `buildplan.Detect`; `NewAppWizard` fires it when leaving the repo step and pre-selects the matching recipe with a "detected" badge; clone failures degrade to an info toast and the default recipe.
- ✅ **Webhook URL + deployed URL on `AppDetailPage` (W11 §Indie steps 5–6)** — verified already shipped at `AppDetailPage.tsx` (webhook row with copy button; deployed-URL chip fed by the app-health prober); backlog rows retired without code change.

### `claude/fervent-sagan-q50XA` — DAG Primitive #3 (inter-stage outputs) + log history/replay

> Landed on `claude/fervent-sagan-q50XA` (no PR number yet).

- ✅ **Inter-stage outputs (DAG Primitive #3)** — `dag-adaptation-2026.md` §7.3 / DR-2 / DR-3. A stage exposes `Outputs map[string]string` (`model.StageRun.Outputs`, persisted in the existing `stage_runs` JSONB column — **no migration**); a downstream stage references them via `${stages.<id>.<key>}` interpolation in its `StageConfig` string fields. New pure package file `internal/buildplan/interpolate.go` (`Interpolate` / `InterpolateSlice` / `InterpolateMap` / `References`, strict `${stages.<id>.<key>}` grammar — every other `${...}` token such as `${IMAGE}` passes through verbatim). `service.Executor` resolves the config into a **copy** before dispatch (shared stage map never mutated), records adapter outputs over an executor-derived baseline (build: `digest`/`tag`/`tags`; push: `digest`/`ref`; deploy: `resources`; gitops: `commit`/`ref`), and enforces caps (≤4 KiB/value, ≤32 KiB/stage) plus control-char rejection (`_invalid`) and truncation marker (`_truncated`). `Script` is intentionally not interpolated (exec'd, not data). `ValidatePipelineDAG` rejects references to unknown / non-ancestor stages at save time (key existence is runtime-only). Headline case: a Push stage with `repository: reg/app@${stages.<buildId>.digest}` receives the digest the Build stage produced. Builder/Pusher/Deployer `Result` types gained an optional `Outputs` field. Feature-flagged off via `COOKER_OUTPUTS_ENABLED=false` (short-circuits both interpolation and ingestion). Tests: full `buildplan` coverage; `executor_test.go` (digest→push flow under `-race`, runtime unknown-key failure, cap truncation, disabled passthrough); `pipeline_test.go` ancestor/self/unknown/slice-field validation.
- ✅ **K8s list/inspect REST endpoints wired to a real read-only client-go client; Docker reads made honest (no docker.sock)** — closes the P6 handler-placeholder gap for the Kubernetes read path and the related k8s-read item. Landed on `claude/fervent-sagan-q50XA` (no PR number yet). New read-only package `internal/kube` (`Client` with lazy `sync.Once` rest.Config init mirroring `deployer/clientgo.go` `restConfig()`; `New(kubeconfig)` for prod + `NewWithClientset(kubernetes.Interface)` for tests; init failure wrapped as `kube.ErrUnavailable`). Methods map to `model.Kube*`: `ListNamespaces`, `ListWorkloads` (Deployments+StatefulSets+DaemonSets, `""`→all namespaces), `GetWorkload` (kind case-insensitive; unknown kind → clear error), `GetPodLogs` (tailLines default 1000, clamped [1,10000], read into a 256 KiB-capped buffer so a huge log can't blow memory). Built **only** from the server's configured kubeconfig (`cfg.Kubernetes.Kubeconfig`, same source as the ClientGo deployer; empty→in-cluster) — no caller-supplied kubeconfig/URL, so no SSRF. `handler/kubernetes.go` list/inspect funcs are now thin `*Handler` methods (HTTP parsing only; all client-go logic in `internal/kube`): nil `Handler.Kube` or `kube.ErrUnavailable` → 503, unknown kind → 400. K8s **write** path (scale/restart/apply/delete) deliberately left as package-level stubs (separate work). `router.go` rebinds the four read routes to the `*Handler` methods; verbs/paths/role gates unchanged. `handler/docker.go` made honest: `GetDockerImage`/`GetContainerLogs` → `notImplementedDockerHost(c, …)` 501 (`image.inspect`/`container.logs`), `ListDockerImages`/`ListContainers` keep empty-200 with the placeholder comment replaced by the honest "no docker.sock until P9.4" contract. No Docker Engine SDK, no `go.mod` change (uses the existing `k8s.io/client-go v0.29.0`). Tests under `-race`: `internal/kube/client_test.go` (fake clientset; maps all three workload kinds, GetWorkload by kind, namespace status, fake pod logs, tailLines clamp, lazy-`ErrUnavailable`); `internal/handler/kubernetes_test.go` (nil-Kube 503 + fake-backed 200/JSON + unknown-kind 400); `internal/handler/docker_test.go` (`image.inspect`/`container.logs` 501, image/container list empty-200).
- ✅ **Log history & replay (execution-observability redesign Part A, Phases 1+2)** — `execution-observability-redesign-2026.md` §2. Fixes the one genuine logging defect: a WS subscriber that connected mid-run saw only *future* lines, lost the live stream after a stage ended, and had no backlog on reconnect. New package `internal/logstore` (`Store` interface + `Entry{seq,ts,line}` + bounded in-memory `Memory` backend: per-stage ring on `COOKER_LOGSTORE_MAX_BYTES` default 1 MiB, LRU stream eviction on `COOKER_LOGSTORE_MAX_STREAMS` default 256, thread-safe `Append`/`Read`, `FromEnv()` selecting `COOKER_LOGSTORE_BACKEND` default `memory`). Each stage-log frame is now the pinned JSON envelope `{"runId","stageId","seq","ts","line"}` (`logstore.EncodeFrame`, no trailing newline in `line`); `seq` is a monotonic per-stage cursor. `service.lineWriter` stamps `seq`+`ts`, appends to the store, and broadcasts the envelope (`newLineWriter(broadcast, store, runID, stageID)`); `Executor` gains `WithLogStore`. `server.WebSocketHub.HandleStageLogs` does replay-on-connect: parse `?since=<seq>` (clamp `<0`→0), register, replay the backlog directly on conn (single conn-writer, before `writePump` starts), then attach live. A slow client dropped on backpressure now receives one `{"control":"stream-truncated"}` frame (via `Client.truncated atomic.Bool` set on the hub drop path, emitted in `writePump`'s channel-closed branch) before close. Wired in `server.go` (one `logstore.FromEnv()` shared by hub + executor). **Memory backend only — single-replica, like the in-memory WS hub/rate limiter; `postgres`/`redis` backends (Part A Phase 3) remain future.** Tests: `logstore` contract suite + concurrent Append/Read under `-race`; `service` envelope/seq assertions; `server` mid-run-join, `since=` reconnect, and slow-client-truncation WS tests under `-race`. No migration; no LogBroadcaster signature change.

### `claude/w5-f2-executor-runresult` — F2 executor returns terminal RunResult

- ✅ **Handler-layering F2 — `Executor.Execute` returns terminal `RunResult`** — `model.RunResult` (`{Status, FinishedAt}`) added in `internal/model/run.go`; `service.Executor.Execute` now returns `(model.RunResult, error)`. The four-branch status-reconciliation block in `handler/pipeline.go` `RunPipeline` (the pre-F2 closure that could flip Cancelled→Success on a nil error) is deleted — the handler now persists what Execute returned verbatim. New private `Executor.finalize` owns the terminal-status state machine: a `startedCancelled` snapshot preserves an externally-set Cancelled across Running→terminal; `context.Canceled` maps to Cancelled (was Failed); other errors map to Failed; clean return maps to Success. New table-driven `TestExecutor_F2_RunResult` (success / failed / context cancelled / empty pipeline) plus `TestExecutor_F2_CancelledStaysCancelled` pin the silent-flip regression. `service/app_deployer.go` updated to the new signature. Closes handler-layering audit Finding 2.

### `claude/w5-adr-0004-finalize` — ADR-0004 multi-tenancy Proposed → Accepted (A3-defer)

- ✅ **Multi-tenancy ADR finalised** — PM locked Decision A as A3-defer on 2026-05-13. `docs/adr/0004-multi-tenancy.md` flipped Proposed → Accepted. Appendix A rewritten as a concrete Q4-2026 execution playbook: migration `010_tenancy.up.sql` SQL, model field bumps (`TenantID int64` on 8 entities), middleware injection point (`auth.RequireTenantBoundary` outer, existing `RequireTeamMember` inner), WS hub scope-key changes (`tenantID:runID` / `tenantID:userID`), OIDC tenancy claim mapping (`COOKER_OIDC_TENANT_CLAIM`), back-fill SQL via the existing `owner_team_id` linkage. Trigger checklist gates execution on (a) PM commits to hosted Cloud, or (b) customer contract, or (c) SAML greenlight. Appendix B (A2 rejected alternative) collapsed to a one-paragraph historical note. `docs/roadmap-2026.md` C1 row updated: "Decision: A3-defer. Unblocked." Closes roadmap C1's open-question gate; leaves C2 (SAML) and C3 (Cooker Cloud) speculative until a hosted-Cloud signal arrives.

### `claude/w5-f3-parse-compose-graph` — F3 extract ParseComposeGraph from handler

- ✅ **Handler-layering F3 — compose YAML graph construction moved to `service.ParseComposeGraph`** — the ~200-line graph-build loop (YAML unmarshal, four parse helpers `parseEnvToMap` / `parseDependsOn` / `parseCommand` / `parseBuild`, intermediate `composeFile` / `composeServiceDef` structs, and the `connSet` dedup loop) is now in `backend/internal/service/compose_graph.go`. The handler `ParseComposeFile` retains disk + path-allowlist + HTTP framing only — `resolveComposePath` stays unchanged and the three generic 400 strings ("invalid compose filename", "cannot read compose file", "invalid YAML") are byte-identical. Typed `service.ErrInvalidComposeYAML` distinguishes parser failure from future validation errors. 11-case table-driven corpus in `compose_graph_test.go`. Dedicated `TestParseComposeGraph_ConnSetKeyFormat` locks the dedup key shape `src->dst:type` byte-for-byte. Closes handler-layering audit Finding 3.

### `claude/w5-ci-cache-mode-min` — docker buildx cache-to mode=min

- ✅ **CI cache quota fix** — `cache-to: type=gha,mode=max` replaced with `cache-to: type=gha,mode=min` in the docker job of `.github/workflows/ci.yml`. `mode=max` was storing all intermediate build layers (~2–4 GB per push), crowding out the go-build and npm caches within GHA's 10 GB per-repo quota. `mode=min` stores only the final stage (~50 MB). Identified as the next quick win in `docs/audits/2026-05-ci-baseline.md` §6.

### `claude/w5-security-drift-bundle` — close 6 drift findings from W4 SECURITY.md walk

- ✅ **§1.2 RBAC table now lists four roles (MEDIUM)** — `SECURITY.md` "Authorization (RBAC)" rewritten as a four-row table (admin / operator / approver / viewer) keyed to `backend/internal/auth/rbac.go:12-17`. The operator row no longer claims promotion-approval rights — `CanApprovePromotion` (`rbac.go:92-102`) accepts only `admin` and `approver`.
- ✅ **§2.1 Secrets adapter inventory complete (LOW)** — `SECURITY.md` "Data Security" → "Secrets" replaced the `e.g.` hint with a five-row table listing every adapter that ships (`database` / `keepsave` / `vault` / `aws` / `gcp`).
- ✅ **§4.2 Multi-replica Redis triad documented (INFO)** — `SECURITY.md` enumerates all three per-process backends — `COOKER_RATE_LIMIT_BACKEND`, `COOKER_WS_TICKET_BACKEND`, `COOKER_WS_HUB_BACKEND` — each with the failure mode that flips when it isn't pointed at Redis.
- ✅ **§6.2 `S26-05-15` closure scope honest (MEDIUM)** — `SECURITY.md` "Pinned action SHAs" reworded to describe the release-workflow half as fully pinned (cosign trust chain) and introduce a "Known gap — non-release workflows" sub-paragraph naming `ci.yml`, `cooker-weekly.yml`, `oci-conformance.yml`.
- ✅ **§6.4 `SECURITY-RELEASE-VERIFY` cross-linked (MEDIUM)** — `SECURITY.md` "Verifying a release" now links to `docs/SECURITY-RELEASE-VERIFY.md`; `docs/RELEASING.md` "Step 4" carries a matching blockquote callout.
- ✅ **CLAUDE.md KeepSave drift corrected (INFO)** — bottom-of-file note rewritten from "parked pending walkthrough" to an accurate status: adapter ships at HEAD.
- ✅ **Audit doc resolution log** — all six findings in `docs/audits/2026-05-security-walk-post-w3.md` moved to a new "Closed" section.

### `claude/w4-t4-edge-condition-refuse` — T4 edge condition forward-compat guard

- ✅ **§6 T4 — `Edge.Condition` unsupported values refused at validation** — `ValidatePipelineDAG` in `internal/service/pipeline.go` now appends an error for any edge whose `Condition` is non-empty and not `"success"`. Empty-string and `"success"` conditions are unchanged (allowed). Primitive #2 (W6, DR-4) replaces this refusal with real evaluation per `dag-adaptation-2026.md §7.2`. Three new sub-tests in `TestValidatePipelineDAG_EdgeCondition`.

### `claude/w4-f04-created-at` — add `PipelineRun.CreatedAt` (store-parity F-04)

- ✅ **F-04 — `pipeline_runs.created_at` surfaced on `model.PipelineRun`** — `model.PipelineRun` gains `CreatedAt time.Time` (`json:"createdAt"`). Postgres `Get` and `List` now select the column; `Create` uses `RETURNING created_at` to populate the field. Memory `Create` stamps `time.Now()` when the caller omits the field; memory `List` sorts newest-first, matching `ORDER BY created_at DESC`. Tests `TestRunStore_ListOrderedByCreatedAt` and `TestRunStore_CreateSetsCreatedAt` added. No new migration needed. Closes store-parity audit F-04.

### `claude/w4-t2-logwriter-push-deploy` — LogWriter through push + deploy adapters

- ✅ **§6 T2 — LogWriter wired for push and deploy** — `pusher.Request` and `deployer.Request` now expose `LogWriter io.Writer`. `executePush` and `executeDeploy` mirror `executeBuild`'s wiring: a `cappedBuffer` (1 MiB cap) tee'd to a per-stage `lineWriter` via `io.MultiWriter` when a broadcaster is configured, with `defer sr.Logs = logs.String()`. Every shipped adapter writes the canonical lines: `pusher.{Crane,DockerSock,Noop}` write `Pushed image to <ref>`; `deployer.{ClientGo,Kubectl,Noop}` write `Applied <kind>/<name>`. Closes `dag-performance.md` §4 High #2. Unblocks Primitive #4 cache-flag visibility.
- ✅ **No frontend changes required** — `RunPage`'s `LogsPanel` is stage-agnostic; `useStageLogs` subscribes to `stage-logs:<runId>:<stageId>` for any selected stage. PR #61's coordination doc flagged this and the fix simply hands push + deploy stages a stream the panel already knew how to render.

### `claude/w3-t1-t3-handler-f1` — executor stubs + drain race + DAG validator dedup

- ✅ **§6 T1 — Executor stubs now fail loudly** — `executeTest`, `executeApproval`, and `executeCustom` now return `fmt.Errorf("stage type %q not implemented", stage.Type)` instead of `nil`. Pipelines that include these stage types will fail with a clear error rather than silently succeeding. Side-effect: any existing pipeline using test/approval/custom stages will start failing. Closes dag-performance.md Critical #1.
- ✅ **§6 T3 — Drain goroutine removed** — the goroutine at `executor.go` that ranged over `runner.Updates()` was removed. It had a race (channel may close before range starts if `runner.Run` completes synchronously) and duplicated the per-stage `slog.Info` already emitted in the stage loop. A debounced replacement that also persists progress mid-run will land in T5 (W4). Noted in dag-performance.md Medium #10.
- ✅ **Handler-layering F1 — `validateDAG` deleted from handler** — the 57-line Kahn's-algorithm re-implementation in `handler/pipeline.go` is deleted. Both `CreatePipeline` and `ValidatePipeline` now call `service.ValidatePipelineDAG`. The duplicate-ID and dangling-edge checks (previously handler-only) were moved into `service.ValidatePipelineDAG`. Closes handler-layering audit Finding 1.

### `claude/frontend-bundle-split` — frontend bundle performance

- ✅ **P26-05-24 — Route-level code splitting via `React.lazy` + `Suspense`** — `frontend/src/App.tsx` now lazy-loads every non-landing route. `AppsPage`, `AppDetailPage`, `SignInPage`, `SignUpPage`, and `Callback` remain eager (fast first paint). All other routes (`PipelineEditorPage`, `RunPage`, `ComposePage`, `NewAppWizard`, `PipelinesPage`, `DockerPage`, `KubernetesPage`, `EnvironmentsPage`, `HostsPage`, `SettingsPage`, `RegistryPage`) load on-demand. A single `<Suspense fallback={<SkeletonStack />}>` boundary at the route layer shows the shimmer skeleton while chunks download.
- ✅ **P26-05-28 — Vite `build.rollupOptions.output.manualChunks` for vendor splitting** — `frontend/vite.config.ts` splits vendors into stable, independently-cacheable chunks: `react` (react + react-dom + react-router-dom), `xyflow` (@xyflow/react), `oidc` (oidc-client-ts), `zustand`. The `xyflow` chunk is only downloaded when the user navigates to `PipelineEditorPage`, `RunPage`, or `ComposePage` — `PipelinesPage` (the list) does not pull it.

### `claude/plan-weekly-features-WoB0S` — weekly feature batch

- ✅ **Per-role complexity + model frontmatter on the 10 cooker-* subagents** — every `.claude/agents/cooker-*.md` now declares an explicit `model:` (sonnet for templated layer work, opus for cross-stack coordinators and the security curator), an inline `<!-- complexity: ... -->` rationale, a "When to escalate / demote" subsection with role-specific triggers, and a "Worked examples" subsection sized 2–3 examples per role. `cooker-feature-dev` and `cooker-security` move from sonnet → opus; the other eight reaffirm sonnet.
- ✅ **Postgres retention CronJob (Helm)** — closes the launch-readiness "pipeline_runs grows without bound" bullet. New `deploy/helm/cooker/templates/cronjob-retention.yaml` gated on `retention.enabled && database.host`, defaults to a 90-day cutoff at 02:00 UTC daily. Reuses the new `cooker.databaseUrlEnv` named template so deployment.yaml and the CronJob share the same `DATABASE_URL` construction. Pod runs as UID 65532 with caps dropped + readOnlyRootFilesystem when `securityContext.enabled=true`. Three new helm-template CI matrix rows assert (a) CronJob absent by default, (b) interpolation of `daysToKeep` + `schedule` + hardening on render, (c) Job is skipped when `database.host` is empty.

### `claude/review-production-rollout-MT3YO` — P0 follow-up batch (operator-independent)

- ✅ **P0.1 — OIDC lock-free fast path** — `internal/auth/oidc.go`. `Middleware.verifier` is now `atomic.Pointer[oidc.IDTokenVerifier]` and `lastJWKSRefresh` is `atomic.Int64` (UnixNano). The mutex serialises only slow-path provider discovery; `ensureProvider` uses double-checked init (load → if nil, lock → re-check → store). Hot path is lock-free. New concurrency test under `-race` exercises 32 writer + 32 reader goroutines through `recordJWKSRefresh` / `LastJWKSRefresh`.
- ✅ **P0.2 — Redis WS hub subscriber resubscribe with backoff** — `internal/server/wshub_backend.go`. `consume()` now owns the `*redis.PubSub` lifecycle and re-subscribes with jittered exponential backoff (500ms → 30s) on disconnect. `b.ch` only closes on context cancel, so the hub `Run()` survives Redis blips. Each iteration closes the previous `*redis.PubSub` before resubscribing to avoid leaked connections; post-resubscribe `Receive` is bounded by a 5s timeout to handle half-open TCP. `IncRedisConnectionError()` increments on every subscribe failure and reconnect, feeding `cooker_redis_connection_errors_total`. New unit tests for `sleepJitter` (timer + ctx-cancel paths) and `nextBackoff` (doubling and cap).
- ✅ **P0.3 — `time.NewTimer` in DB backoff** — `internal/store/postgres/store.go` `pingWithBackoff` swaps `time.After()` for `time.NewTimer` + `Stop()` on the ctx-cancel branch. No leaked timer channels per retry.
- ✅ **P0.4 — Parallel readiness checks** — `internal/server/health.go`. DB and Redis pings run concurrently via `errgroup.WithContext` against the shared 1s deadline. Worst-case probe latency is `max(db, redis)` instead of `db + redis`. `golang.org/x/sync` promoted from indirect to direct in `go.mod`.
- ✅ **P0.5 — Binary WS broadcast encoding** — `internal/server/wshub_backend.go`. Replaced `json.Marshal` of `BroadcastMessage` on the Redis pub/sub leg with a length-prefixed binary frame: `[channel-len: uint16 BE][channel][data]`. ~74 bytes of JSON framing per message replaced with 2. Browser-facing wire is unchanged — the hub still writes raw `data` as a `TextMessage`. Round-trip + truncation + oversized-channel tests added; documented in code that the format is internal and that a rolling upgrade across mixed-version replicas will see decode warnings during the upgrade window.
- ✅ **P0.6 — OCI conformance workflow scope** — `pull_request:` trigger removed in commit `a8aa68e` (already on `main` ahead of this branch). Workflow now only runs on `workflow_dispatch` and the weekly `schedule`, treating conformance as a tracked-but-non-blocking signal until a human can pull failing logs.
- ✅ **P0.7 — OCI image-spec schema validation** — `internal/pusher/conformance_test.go` adds `TestManifestSpecConformance`, which pulls the image pushed by `TestPushConformance` via `crane.Manifest` and validates structural requirements per OCI image-spec v1.1: `schemaVersion=2`, `mediaType=application/vnd.oci.image.manifest.v1+json`, descriptor digest/size/mediaType for both `config` and every `layer`. CI workflow + Makefile target updated to run both tests in one pass.
- ✅ **P7 — `make uat-up-with-keycloak`** — `docker-compose.uat.keycloak.yml` overlay + pre-seeded realm at `deploy/uat/keycloak-realm-cooker.json`. Realm contains the `cooker` public PKCE client, the three `cooker-*` groups, and two test users (alice/alice = admin, bob/bob = viewer). Both backend and browser use the same issuer URL `http://host.docker.internal:8081/realms/cooker` so issuer claim verification works without dual-URL configuration. Linux operators without Docker Desktop need a one-time `/etc/hosts` entry; documented in the Makefile output.
- ✅ **P7 — `make test-e2e`** — `scripts/e2e/run.sh` waits for `/health/ready`, creates a single-stage `custom` (no-op) pipeline via the API, triggers a run, polls until terminal, and asserts `success`. Uses dev-admin auto-injection (UAT default `COOKER_OIDC_ENABLED=false`). The Makefile target boots `make uat-up`, runs the harness, and tears down on exit via trap.
- ✅ **P9.5 follow-up — Buildah Helm chart wiring** — `templates/rbac.yaml` extended with a `cooker-buildah-builder` Role + RoleBinding gated on `builder.kind=buildah`. `templates/deployment.yaml` renders `COOKER_BUILDAH_{IMAGE,SERVICE_ACCOUNT,CONTEXT_PVC,STORAGE_DRIVER}` plus the optional context-PVC volume + mount when buildah is selected. `values.yaml` adds the `builder.buildah.{image, namespace, serviceAccount, contextPVC, storageDriver}` block alongside the existing kaniko block. CI matrix grew a `helm template (buildah builder)` job that asserts docker-socket is absent, buildah RBAC + env-vars render, and kaniko-named resources don't leak; the resulting render is also fed to kubeconform. `SECURITY.md` "image build isolation" updated with the PSA caveat.

### `claude/identify-failure-point-Duy02` (PR #21) — SPOF closeout

- ✅ **Graceful HTTP shutdown** — `cmd/cooker/main.go` installs SIGTERM/SIGINT handler; `Server.RunContext` wraps an explicit `http.Server` and drains in-flight requests for 30s on ctx cancel. `terminationGracePeriodSeconds: 60` in chart. Tests in `internal/server/server_shutdown_test.go`. — closes single-process abrupt-cut SPOF.
- ✅ **Postgres reconnect-with-backoff at boot** — `internal/store/postgres/store.go` `pingWithBackoff` uses jittered exponential backoff (500ms→30s, 5min budget) instead of crashing on the previous 5s ping timeout. `livenessProbe.initialDelaySeconds: 60` in chart. — pod no longer crash-loops through transient Postgres blips.
- ✅ **`/health/live` + `/health/ready` split** — `internal/server/health.go`. `/health/ready` returns 503 with per-check breakdown (DB ping + Redis ping + JWKS age). `/health` kept as alias for back-compat. Probes flipped in chart. — orchestrators can now distinguish "process up" from "ready to serve".
- ✅ **Lazy OIDC discovery + JWKS-age signal** — `internal/auth/oidc.go`. `NewMiddleware` no longer dials the IdP at construction; first authenticated request triggers discovery with a 30s retry-after cool-down. `LastJWKSRefresh()` feeds the readiness probe. — boot survives an unreachable IdP; transient blips self-heal.
- ✅ **Pipeline executor wiring + heartbeat + orphan sweep** — closed a latent correctness gap discovered during the audit: `RunPipeline` previously created a `pending` row and never spawned the executor. New `internal/server/runs.go` `RunCoordinator` (sync.WaitGroup + 30s heartbeat ticker) tracks goroutines and drains for 25s on shutdown. `runAppDeploy` migrated onto it. Migration `006_run_heartbeat.up.sql` adds `heartbeat_at` with a partial index. `SweepOrphans` runs at boot and marks rows with stale heartbeats as failed. — pipelines now actually execute; crashes leave clean state on next boot.
- ✅ **`Config.Validate` multi-replica + builder guards** — refuses production boot with `COOKER_BUILDER=docker` (closes the docker.sock RCE-to-host path); refuses `replicaCount>1` + memory-backed rate-limit/WS-ticket/WS-hub without `COOKER_STICKY_SESSIONS=true`. New env vars rendered from chart values. — eliminates entire classes of "works in dev, breaks in prod" misconfigs.
- ✅ **Helm defaults flipped to multi-replica safe** — `builder.kind=kaniko`, `wsHub.backend=redis`, `wsTicket.backend=redis`, `rateLimit.backend=redis`. `templates/deployment.yaml` renders the new env vars + `REDIS_URL`. `MULTI_REPLICA.md` re-framed around Redis-as-default with sticky sessions as fallback.
- ✅ **Redis pub/sub WS hub** — new `HubBackend` interface with memory + Redis pub/sub impls in `internal/server/wshub_backend.go`. Broadcasts cross replicas via `cooker:ws:broadcast`; per-client subscription map stays per-process. Hot path of WS upgrade unchanged. — closes "broadcast on replica A invisible to client on replica B" failure mode.
- ✅ **Resilience Prometheus counters** — `internal/observability` adds `cooker_db_connection_errors_total`, `cooker_redis_connection_errors_total`, `cooker_jwks_fetch_failures_total`, `cooker_pipeline_runs_orphaned_total`. Wired from each error path. `RUNBOOK.md` ships recommended Alertmanager rules. — operator gets paged on the right signals.
- ✅ **OCI distribution-spec conformance CI** — `.github/workflows/oci-conformance.yml` boots `registry:2`, has Cooker push via `pusher.NewCrane`, then runs the upstream `distribution-spec/conformance` binary. `make oci-conformance` mirrors locally. Re-framed from the original "test against `/registry` proxy stubs" plan because those are stubs. **Note:** workflow has been failing during the PR with the AI agent unable to reach logs; see P0.6 for the follow-up.
- ✅ **RUNBOOK + MULTI_REPLICA + SECURITY updates** — new sections covering probe semantics, rolling restart, recovery after restart, WS broadcast topology, Alertmanager rules, OCI compliance status.

- ✅ **Aegis "Workshop" frontend redesign** — port of the Claude-Design Aegis bundle into `frontend/src/`. Theme system (`theme/tokens.ts` paper / coal / rust), Fraunces serif + Inter Tight + JetBrains Mono, shared atoms (`Pill`, `StatusDot`, `Btn`, `Card`, `KindBadge`, `Toggle`, `HealthBar`, `DataTable`), Simple ⇄ Pro mode toggle persisted in `uiStore`, sidebar / topbar restyled, every page re-laid (Apps editorial dashboard, Pipelines list, PipelineEditor with palette + inspector, RunPage with step rail + logs + telemetry, NewAppWizard 4-step deploy wizard, Docker / Kubernetes / Environments tables, Compose graph, AppDetail). Sign-in landing + Callback + ErrorBoundary themed. Closes P5 sign-in landing.

- ✅ **Go 1.25 toolchain bump** — `go.mod`, `Dockerfile`, CI matrix all moved together; `golang.org/x/time` unpinned to v0.15.0; `golangci-lint` migrated v1.59 → v2.0 with a v2-format `.golangci.yml`. — P6
- ✅ **`log/slog` migration** — JSON handler installed in `cmd/cooker/main.go`; all `log` callers across the codebase rewritten as structured slog calls. — P4
- ✅ **Prometheus + OpenTelemetry** — `internal/observability` exposes `/metrics` (Gin middleware + counter/histogram) and an OTLP/gRPC TracerProvider via `otelgin`. Both opt-in via `COOKER_METRICS_ENABLED` / `COOKER_TRACING_ENABLED`. — P4
- ✅ **Redis-backed rate limiter + WS ticket store** — `redis_rate/v10` GCRA limiter and atomic GETDEL ticket store, selected via `COOKER_RATE_LIMIT_BACKEND=redis` / `COOKER_WS_TICKET_BACKEND=redis`. — P3
- ✅ **HashiCorp Vault, AWS Secrets Manager, GCP Secret Manager adapters** — `internal/secrets/{vault,awsm,gcpsm}` all implementing `secrets.Manager`; selectable via `COOKER_SECRETS_BACKEND={vault,aws,gcp}`. — P2
- ✅ **Native BuildKit / crane / client-go adapters** — replace the CLI shell-out stubs in `internal/builder/buildkit.go`, `internal/pusher/crane.go`, `internal/deployer/clientgo.go`. — P9.1
- ✅ **Cloud Run, ECS/Fargate, Fly.io, Render deploy targets** — `internal/deploytarget/{cloudrun,ecs,flyio,render}` with self-registration on non-empty config. — P9.2
- ✅ **go-git GitOpsCommit** — full SSH/HTTPS auth in `internal/gitops/gogit.go`. — P9.3
- ✅ **Buildah builder adapter** — third in-cluster builder Job pattern; storage-driver and SETUID/SETGID caps are configurable. — P9.5
- ✅ **swaggo/swag OpenAPI generation** — `make swagger` regenerates `backend/docs/api/swagger.{json,yaml,go}` from doc-comments; flagship endpoints annotated. — P8

- ✅ **KeepSave Helm wiring** — `secrets.backend=keepsave` renders `COOKER_SECRETS_KEEPSAVE_{URL,PROJECT_ID,API_KEY}` (the API key via `secretKeyRef` into an operator-managed Secret); CI matrix asserts both happy-path and apiKey-missing-fail. — P2.1 follow-up
- ✅ **KeepSave secret promotion handler** — `POST /api/v1/environments/:id/secrets/promote` via the new `secrets.Promoter` interface; admin+MFA gated; database backend returns 501. — P2.1 follow-up
- ✅ **OIDC group-to-role mapping configurable** — `COOKER_OIDC_GROUP_MAP` CSV of `group:role` pairs; chart value `oidc.groupRoleMap`. — P3
- ✅ **MFA / step-up auth** — `auth.RequireMFA` middleware applied to admin destructive routes; `COOKER_OIDC_MFA_ACR_VALUES` configures accepted `acr`/`amr`; frontend API client re-issues `signinRedirect({ acr_values })` on the 403 `mfa_required` response. — P3
- ✅ **Toast primitive + OIDC silent-renew toast** — Zustand store + `ToastViewport` mounted in `App.tsx`; `OIDCProvider` pushes a warning toast on `addSilentRenewError`. — P5
- ✅ **WebSocket auto-reconnect with backoff** — `useWebSocket` re-fetches a fresh ticket on each reconnect with exponential backoff (500ms → 30s default). — P5
- ✅ **`gofmt -l` + `golangci-lint` in CI** — repo-wide gofmt sweep + tuned `backend/.golangci.yml`. — P6
- ✅ **handler/network.go and handler/volume.go cleanup** — write endpoints return HTTP 501 with a structured `{error,operation,hint}` payload instead of mock IDs; list endpoints return `[]`. — P6
- ✅ **`docker-compose.uat.socketproxy.yml` overlay** + `make uat-up-socketproxy` — opt-in `socketproxy` profile that drops the host docker.sock bind mount and routes the cooker container at a hardened tecnativa/docker-socket-proxy. — P7
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
