# Changelog

All notable changes to the Cooker project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed — harness: TheLoopSkill installed, `workflow` skill retired

- Vendored the 12 `loop-*` skills from
  [santapong/TheLoopSkill](https://github.com/santapong/TheLoopSkill)
  (v0.4.0) into `.claude/skills/` per its INSTALL.md — the commit-into-project
  path that works in Claude Code web sessions.
- Retired the repo-authored `workflow` skill: `loop-engine` does the same job
  (authoring/running multi-agent Workflow scripts) with a superset of the
  patterns and policies. Cooker's 10-phase AIDLC framework was ported to
  `loop-engine/frameworks/Cooker-AIDLC.md` (run `/loop-engine --framework
  Cooker-AIDLC`); `ORCHESTRATION.md` moved to `.claude/workflows/`; the
  `sdlc`/`lightweight` framework variants and `new-workflow.sh` were dropped
  (replaced by `loop-engine/templates/`).
- Kept the other `cooker-*` and `ponytail-*` skills after a per-skill duplicate
  review — they carry project protocol (audit-corpus routing, chain-ledger
  bookkeeping, the Monday cron contract) or a job `loop-review` deliberately
  refuses (aggressive deletion hunts). Each kept skill now routes the generic
  half of its job to its `loop-*` counterpart; the skill roster and division
  of labour are documented in `docs/engineering/harness-engineering.md`.
- Hardening follow-ups from the change audit: `.claude/skills/README.md` pins
  the vendored packs (TheLoopSkill 0.4.0 @ `9f03ad1`) with a re-sync procedure
  and a keep-verbatim rule; `scripts/check-doc-links.sh` + a `docs` CI job
  guard relative markdown links (offline, no network); CLAUDE.md gained a
  skills-routing bullet so `/loop-engine --framework Cooker-AIDLC` and the
  saved workflows stay discoverable in every session.

### Added — AWS/Vercel hosting design (IaC + overlays + guide)

- New hosting track: UAT = Cooker SPA on **Vercel** (split-origin via
  `VITE_API_BASE_URL`, per-PR previews with local auth, Deployment
  Protection off) + an AWS **Lightsail** backend behind Caddy;
  production = **EKS Auto Mode** across Starter/Team/Scale tiers.
- `deploy/vercel/`: `vercel.json` (SPA rewrites + 1y asset cache) and a
  setup README (project settings, env matrix, preview-auth recipe,
  Lightsail+Caddy sketch).
- `deploy/aws/terraform/`: a clean Terraform skeleton (no eksctl,
  single state, S3 native locking) — VPC, EKS Auto Mode, RDS, ElastiCache
  Serverless Valkey (Team+), ECR + pull-through cache, Pod Identity,
  Secrets Manager, CloudWatch + Budgets, per-tier tfvars. Spot NodePool
  and a dockerconfig-refresh CronJob ship as example k8s manifests.
- `deploy/aws/values/values-aws-{starter,team,scale}.yaml`: Helm overlays
  (`COOKER_SECRETS_BACKEND=aws`, OIDC scopes without `groups` for Cognito,
  `networkPolicy.enabled=false`, Pod-Identity `serviceAccount.annotations`
  `{}`, ALB ingress with mandatory `idle_timeout.timeout_seconds=300`).
- `.github/workflows/ci.yml` helm job now templates the chart against each
  AWS overlay (+ kubeconform) so the overlays can't rot.
- `docs/guides/DEPLOY-AWS-VERCEL.md`: the full advisory — topology, tiered
  cost tables (rough estimates, sourced, retrieved 2026-06-11), traps
  ledger, provisioning runbook, DR, and an OPEN-questions list (incl. the
  Cognito `aud` spike and the chart `extraVolumes` gap). Region
  recommendation: ap-southeast-1 (Bangkok not launch-ready). Cross-linked
  from the docs index, product-plan §6, system-design 08, UAT, ROLLOUT,
  and MULTI_REPLICA. Costs are estimates — re-verify at apply time.

### Added — SSH remote deploy target (Thread 1, Dokploy/Coolify model)

- New `deploytarget/ssh` adapter: SSH into a registered host
  (`HostKindSSHDocker = "ssh-docker"`), run `docker pull` then
  `docker run -d --restart=always`. No agent on the remote, no
  Kubernetes, no cloud API. Host-key TOFU pinning is mandatory
  (`golang.org/x/crypto/ssh` with a strict `HostKeyCallback`).
- New `DeployTargetSSH = "ssh"` constant on `model.DeployTargetKind`.
- New SSH fields on `model.Host`: `SSHEndpoint`, `SSHUser`,
  `SSHPort`, `SSHPrivateKeyRef` (write-only, never serialised),
  `SSHKnownHostKey` (TOFU-pinned), `SSHStrictHostKey`. PEM bodies
  flow through `secrets.Manager` via a new `service.HostService`.
- New migration `014_ssh_hosts` adds the columns idempotently
  (`ADD COLUMN IF NOT EXISTS`); down migration drops them.
- Hosts page extended: kind selector now includes
  `ssh-docker`; the form accepts a private-key PEM textarea and
  a strict-host-key toggle (default ON). `GET /hosts/:id`
  redacts the PEM ref and surfaces only `hasSSHPrivateKey`.
- `Config.ValidateSSHHosts` refuses production boot if any
  registered SSH host has `sshStrictHostKey=false`. Runs after
  the store is open but before serving traffic.

### Added — May 2026 W6 batch (PR #89: Phase 1 + Phase 2 Dokploy adaptation)

Closed in a 16-commit branch (`claude/analyze-dokploy-integration-NTrW3`).
All new subsystems are gated by default-off feature flags; merging
is a no-op for any operator who hasn't flipped `COOKER_JOBQUEUE_ENABLED`.
Full design rationale and "what we adapted from Dokploy" matrix in
[`docs/adapted-from-dokploy.md`](docs/proposals/adapted-from-dokploy.md) and
[`docs/architecture-phase1-phase2.md`](docs/reference/architecture-phase1-phase2.md).

#### Phase 1 — architectural primitives

- **A1: Durable async job queue** (`internal/jobqueue/`). Postgres-native with `FOR UPDATE SKIP LOCKED` for lock-free pickup and `NOTIFY cooker_jobs_new` for near-instant worker wake-up. `EnqueueOptions.ConcurrencyKey` enforces per-key serialisation via a `NOT EXISTS` guard inside the dequeue query. Worker pool with panic-recover, capped exponential backoff with jitter, atomic `Reschedule` that transitions to terminal `failed` when `attempts >= max_attempts`. Three new Prometheus series: `cooker_jobqueue_depth{status}`, `cooker_jobqueue_attempts_total{kind}`, `cooker_jobqueue_run_duration_seconds{kind,outcome}`. Migration `010_jobs.up.sql`. Gated by `COOKER_JOBQUEUE_ENABLED=false` by default; when off, `RunPipeline` keeps using the inline `Runs.Spawn` path. Pattern adapted from Dokploy's BullMQ + Inngest dual queue but reimplemented Postgres-native to avoid making Redis mandatory.

- **A2: Run + stage state machine** (`internal/runstate/`). Formal transition-table FSM with typed `ErrInvalidTransition`. State alphabet is pinned to `model.RunStatus` (`pending`, `running`, `success`, `failed`, `cancelled`) via a test assertion so a future rename can't drift the two. `TransitionRun(current, event)` adapter returns the input state unchanged on error. Terminal-sticky property covered by exhaustive tests. In-house 80-LOC FSM rather than `github.com/looplab/fsm` because `push_files` over the GitHub MCP can't run `go mod tidy`; semantic guarantee is identical and a swap is mechanical.

- **A3: Resource-action permission middleware** (`internal/auth/permission.go`). `Resource` and `Action` typed constants, role × resource × action matrix with deny-by-default for undeclared pairs. `RequirePermission(resource, action)` Gin middleware applied at route registration alongside the existing `RequireRole` / `RequireMFA`. Three sensitive routes adopted: `POST /pipelines/:id/run` (`(pipeline, invoke)`), `GET /environments/:id/secrets/:key` (`(secret, reveal)`), `PUT /apps/:id/webhook` (`(webhook, update)`). Remaining routes adopt incrementally without a flag day. Pattern adapted from Dokploy's tRPC `withPermission` middleware.

#### Phase 2 — feature gaps

- **F1: Multi-channel notification fan-out** (`internal/notifier/`). Four channel adapters — Slack (incoming-webhook), Discord (color-coded embed), generic Webhook (JSON POST with optional bearer token + arbitrary headers), Email (SMTP via stubbable `SMTPSender`). `Dispatcher` fans out concurrently with `errors.Join` for per-target failures and per-target `SendTimeout` (default 10s). Per-event-type `text/template` rendering. `Target` rows carry an `event_types` array filter (empty = all events) for per-target event scoping. Migration `011_notification_targets.up.sql`. Dispatched by `service.JobQueueRunner.Handle` on terminal run status so a slow Slack webhook can't slow run completion.

- **F2: Cron-triggered pipeline runs** (`internal/scheduler/`). In-house 5-field POSIX cron parser supporting `*`, lists, ranges, steps; operates in the schedule's IANA location for DST-correct firing. Search capped at 4 years to bail on pathological expressions. Runner uses `pg_try_advisory_lock` on a dedicated connection for continuous (not one-shot) leader election. Per-tick: scan `schedules` for `enabled AND next_run_at <= NOW()`, enqueue a pipeline-run job through the existing `JobQueueEnqueuer`, atomically `MarkFired` with computed `next_run_at`. Bad cron expressions disable the row rather than looping forever. Migration `012_schedules.up.sql`. Gated by `COOKER_SCHEDULER_ENABLED=false` by default; production `Config.Validate()` rejects `SCHEDULER_ENABLED=true` without `JOBQUEUE_ENABLED=true`.

- **F3: GitLab + Bitbucket Server + Gitea webhook receivers** (`internal/source/{gitlab,bitbucket,gitea}/`, handlers `internal/handler/webhook_{gitlab,bitbucket,gitea}.go`). Each package mirrors `internal/source/github` with provider-specific signature verifiers. GitLab uses a literal `X-Gitlab-Token` (no HMAC); Bitbucket Server and Gitea use HMAC-SHA256 with different header conventions (Bitbucket prefixes `sha256=`, Gitea sends raw hex). All comparisons use `subtle.ConstantTimeCompare` / `hmac.Equal`. Three new endpoints, `POST /webhooks/{gitlab,bitbucket,gitea}`, mirroring the existing `/webhooks/github` flow exactly. Bitbucket Cloud not supported in v1 (Atlassian doesn't sign Cloud webhooks).

- **F4: Pipeline templates v1** (`internal/templates/`). A `pipeline_templates` table carries reusable Pipeline-shaped JSONB schemas. New endpoints: `GET /api/v1/templates` (gallery), `GET /api/v1/templates/:id` (single template + schema), `POST /api/v1/pipelines/from-template/:id` (create-from-template). Create-from-template deep-copies the schema with fresh stage IDs (re-mapping edges accordingly) and re-validates through `service.ValidatePipelineDAG`. Operators seed templates via SQL in v1. Migration `013_pipeline_templates.up.sql`.

#### Integration + safety

- **Default-off feature flags throughout**. With both `COOKER_JOBQUEUE_ENABLED` and `COOKER_SCHEDULER_ENABLED` off, the runtime is byte-identical to pre-Phase-1 except for two no-op nil-checks.
- **Deterministic shutdown order**. HTTP drain → run coordinator drain → jobqueue pool drain → scheduler drain → health checker drain. Each step has its own timeout.
- **Migrations**: `010_jobs`, `011_notification_targets`, `012_schedules`, `013_pipeline_templates`.

### Added — May 2026 W5 batch (PRs #78, #79, #80, #81, #82, #83, #84)

**Fifth week of execution.** Closes the **handler-layering audit** in full (F1+F2+F3 all merged), finalises the **multi-tenancy ADR** as `Accepted (A3-defer)` per PM Decision A, lands the **CI cache quota fix**, **closes 6 SECURITY.md drift findings** from W4, and produces two forward-looking research docs.

#### Handler-layering closure — F2 + F3 (#82, #84)

- **F2** (#84) — `Executor.Execute` signature bumped to `(model.RunResult, error)`. `model.RunResult{Status, FinishedAt}` added in `internal/model/run.go`. The four-branch status-reconciliation block in `handler/pipeline.go RunPipeline` (which could flip `Cancelled → Success` on a nil error — silent bug) is deleted; handler persists what `Execute` returned. New private `Executor.finalize` owns the terminal-state machine: snapshots `startedCancelled` at entry so external pre-set Cancelled survives `Running→terminal`; `context.Canceled → Cancelled` (was `Failed` pre-F2, semantic correction); other errors → `Failed`; clean return → `Success`. New `TestExecutor_F2_RunResult` (4 cases) + `TestExecutor_F2_CancelledStaysCancelled` pin the regression. `service/app_deployer.go` updated to new signature. Closes handler-layering Finding 2.
- **F3** (#82) — `service.ParseComposeGraph(data []byte) (*model.ComposeGraph, error)` extracted from `handler/docker.go` (~200 LOC moved). Handler `ParseComposeFile` shrinks to disk + path-allowlist + HTTP framing only; `resolveComposePath` untouched; the three generic 400 strings byte-identical. Typed `service.ErrInvalidComposeYAML`. **11-case table-driven corpus** + dedicated `TestParseComposeGraph_ConnSetKeyFormat` byte-locking the `src->dst:type` dedup key shape. Closes handler-layering Finding 3.

**Combined with PR #64's F1, all three handler-layering findings are now closed.**

#### Multi-tenancy ADR — A3-defer (#79)

PM locked Decision A as **A3-defer** on 2026-05-13. `docs/adr/0004-multi-tenancy.md` flipped Proposed → Accepted. Appendix A rewritten as Q4-2026 execution playbook (migration `010_tenancy.up.sql` SQL, model field bumps, middleware composition rule, WS hub scope-key changes, OIDC tenancy claim mapping, trigger checklist). Appendix B (A2 alternative) collapsed. `docs/roadmap-2026.md` C1 row updated: "Decision: A3-defer. Unblocked."

#### CI cache quota fix (#78)

`.github/workflows/ci.yml` docker job: `cache-to: type=gha,mode=max` → `mode=min`. `mode=max` stored 2-4 GB of intermediate layers per push, crowding out go-build + npm caches within GHA's 10 GB per-repo quota. `mode=min` stores only the final stage (~50 MB). The "next quick win" identified in PR #69's CI baseline audit.

#### SECURITY.md drift bundle (#83)

Six fixes from PR #71's walk, doc-only:

1. **RBAC table** rewritten as four-row (admin / operator / approver / viewer); operator's "approve promotions" claim removed (`CanApprovePromotion` accepts only admin + approver).
2. **Secrets adapter inventory** expanded from "e.g., Vault, AWS Secrets Manager" to full five-row table (database / keepsave / vault / aws / gcp).
3. **Multi-replica Redis triad** enumerated (rate-limit + ws-ticket + ws-hub backends).
4. **`S26-05-15` action-pinning closure** honest: release.yml pinned, other workflows tracked as Known Gap with `cooker-weekly.yml`'s `claude-code-action@v1` (`contents:write` + `pull-requests:write`) called out.
5. **`SECURITY-RELEASE-VERIFY.md`** cross-linked from both `SECURITY.md` and `docs/RELEASING.md`.
6. **CLAUDE.md KeepSave** corrected: parked → shipped at HEAD.

#### Research audits (W5 idle lane)

- **`docs/audits/2026-05-p3-schema-sketch.md`** (#81) — P#3 outputs schema migration. Recommendation: DDL-additive `010_stage_outputs.up.sql` with CHECK constraint at 32 KiB (PR #58 §1 authoritative). One caveat: W6 implementation must wrap in `DO $$ BEGIN IF NOT EXISTS ... END $$` for idempotency.
- **`docs/audits/2026-05-usestagelogs-reconnect.md`** (#80) — **Real HIGH gap found.** `useStageLogs` REST backfill not re-issued on WebSocket reconnect; log lines during 0-30s reconnect window are permanently dropped. **Proposed fix (queued for W6):** expose `onReconnect`/`reconnectCount` from `useWebSocket`; `useStageLogs` re-fetches `getStageLogs` on each reconnect and merges. ~1 hour, frontend-only. Pairs with PR #72 (transport-layer audit which was clean).

### Added — May 2026 W4 batch (PRs #66, #67, #69, #70, #71, #72, #73, #75)

**Fourth week of execution.** Closes the **T-series tidy sprint** (T1+T3+F1 in W3, T4+T5+T2 in W4) — `dag-adaptation-2026.md` §6 is now fully resolved. Ships F-04 (final store-parity finding), the multi-cloud bug bundle (three production bugs across ECS/Fly.io/Render), the empty-state roll-out across five list pages, four forward-looking research audits, and the F2+F3 ready-to-fire prompts for W5.

#### T-series closure

- **T4 — `Edge.Condition` forward-compat refusal** (squashed into #75 as a stacked commit from the worktree sandbox; tracked as `claude/w4-t4-edge-condition-refuse`) — `ValidatePipelineDAG` rejects any edge whose `Condition` is non-empty and not `"success"`. Primitive #2 (W6, DR-4) replaces this refusal with real evaluation per `dag-adaptation-2026.md §7.2`. Three new sub-tests.
- **T5 — Batched `persistProgress` via drain goroutine** (squashed into #75 as a stacked commit; tracked as `claude/w4-t5-batched-persistprogress`) — `internal/service/executor.go` now drains `runner.Updates()` in a goroutine with `min(500ms, 10 transitions)` debounce. **Eager flush on terminal `failed`/`success`** preserves the final-outcome guarantee. Closes `dag-performance.md` Medium #10. **Prerequisite for Primitive #1 in W5** — without T5, P#1's retry attempts triple the JSONB write rate per stage.
- **T2 — LogWriter wired for push + deploy** (#73) — `pusher.Request` and `deployer.Request` expose `LogWriter io.Writer`. `executePush` and `executeDeploy` mirror `executeBuild`'s `cappedBuffer + io.MultiWriter + lineWriter` wiring. Every shipped adapter writes canonical lines: `Pushed image to <ref>` (Crane/Docker/Noop) and `Applied <kind>/<name>` (ClientGo/Kubectl/Noop). `RunPage`'s `LogsPanel` is already stage-agnostic — no frontend change needed. Closes `dag-performance.md` §4 High #2. **Unblocks Primitive #4 cache-flag visibility** — Kaniko's `5/10 layers cached` lines now reach RunPage.

#### Production bug bundle (#75)

Three real bugs from PR #59's deploytarget walk, in one PR:

- **E-2** — ECS: `errors.As(uerr, &ecstypes.ServiceNotFoundException{})` gating before `CreateService` fallthrough. Closes ghost-Fargate-service leak.
- **F-2 + F-3** — Fly.io: new `listMachines(ctx, appID)` reads existing machines so `Deploy` updates in place via `POST /apps/<id>/machines/<id>` (no more linear billing growth); `Rollback` calls the real per-machine restart URL via the same listing path.
- **R-2** — Render: replaced flat `json:"serviceDetails.url"` (which Go treats as a literal key) with a nested `ServiceDetails struct { URL string \`json:"url"\` }`. `TestRender_StatusURLDecodes` locks the JSON shape.

#### F-04 — pipeline_runs.created_at (#70)

Final closure of store-parity audit. `model.PipelineRun.CreatedAt time.Time` (`json:"createdAt"`) added; Postgres `Create` uses `RETURNING created_at`; memory `Create` stamps `time.Now()` when zero; both `List`s sort newest-first. Two new tests in `internal/store/memory/run_created_at_test.go`. **No new migration** — column pre-existed in `001_initial.up.sql`.

#### Empty-state roll-out (#67)

W11 §Indie step 2 (empty-state CTAs) extended from PR #62's Apps/Pipelines/Environments to **HostsPage, RegistryPage, KubernetesPage, DockerPage, ComposePage**. Per-page primary `Btn` + ghost user-guide anchor. `RegistryPage`'s CTA is **conditional** — empty-because-no-filter shows CTA; filtered-empty shows the existing "Try a different filter" text only.

#### Research audits (W4 idle lane)

- **`docs/audits/2026-05-f2-f3-prompts.md`** (#66) — Two ready-to-fire `cooker-feature-dev` delegation prompts for F2 (Executor → `(RunResult, error)`) and F3 (`service.ParseComposeGraph`). Confidence F2 lands cleanly in W5: **HIGH** (T4/T5 disjoint from F2's surface; T5's drain ordering is the one reviewer caveat).
- **`docs/audits/2026-05-ci-baseline.md`** (#69) — Static-analysis of the 4-job parallel CI structure. Theoretical warm-cache critical path: **~2–2.5 min** (under W1's ~3 min target). Live measurement deferred (`gh` unavailable in spawn sandbox). **Next quick win: change `cache-to: type=gha,mode=max` → `mode=min` in docker job.** 1-line, no logic risk, lands W5.
- **`docs/audits/2026-05-security-walk-post-w3.md`** (#71) — 6 drift findings (3 MEDIUM, 1 LOW, 2 INFO), **all documentation-only**: RBAC table omits `approver` role; `S26-05-15` action-pinning scope narrower than `SECURITY.md` implies (17 unpinned in `ci.yml`/`cooker-weekly.yml`/`oci-conformance.yml`); `docs/SECURITY-RELEASE-VERIFY.md` is orphaned (no link from `SECURITY.md`/`RELEASING.md`); secrets adapter inventory understates (5 backends, doc names only 2); multi-replica Redis triad mentions only rate-limit; CLAUDE.md KeepSave drift. **Zero code/threat-model regressions.** Bundle into a small `docs(security)` PR in W5 (~30 min).
- **`docs/audits/2026-05-reconnect-redis-failover.md`** (#72) — Audit of `useWebSocket` against four Redis-failover scenarios. **Zero real bugs found.** All four modes recover correctly. One MEDIUM UX-only improvement opportunity: `http.Server.Shutdown()` doesn't drain Gorilla WebSocket connections, so pod terminations send TCP RST instead of graceful close frame 1001. Reconnect loop recovers; UX is hard disconnect during rolling updates.

### Added — May 2026 W3 batch (PRs #55, #56, #57, #58, #59, #60, #62, #64)

**Third week of execution against the May-2026 30-day plan.** Ships the `v0.1.0` release engine end-to-end (the single highest-leverage item per pm-brief §2.1), the cross-stack AppDetailPage refresh (W11 quickwins + raw-WS migration + deployedURL plumbing), three small code fixes closing W1+W2 findings, and four forward-looking research docs that feed W4+ work.

#### Release engineering — v0.1.0 publish (PR #60)

- **`.goreleaser.yaml`** — full `dockers` block (linux/amd64 + linux/arm64), `docker_manifests` stitching, `signs` (cosign keyless on `checksums.txt`), `docker_signs` (cosign keyless on manifest digests), `snapshot` config.
- **`.github/workflows/release.yml`** — tag-triggered (`v*.*.*`); permissions exactly `contents: write, id-token: write, packages: write`; pipeline checks out → goes through `goreleaser/goreleaser-action` → packages + pushes Helm OCI chart to `oci://ghcr.io/santapong/charts/cooker`. All 8 third-party actions pinned to 40-char SHAs.
- **`Makefile`** — `VERSION` / `COMMIT` / `BUILD_DATE` vars; `build-backend` uses `-ldflags "$(GO_LDFLAGS)"`; new `make release-snapshot` and `make release` targets.
- **`docs/RELEASING.md`** — release runbook (prerequisites, pre-flight, tag-and-push, observation, verification, troubleshooting, patch-release flow).
- **`docs/SECURITY-RELEASE-VERIFY.md`** (PR #56) — 6-section publish-time verification checklist; highest-risk single item is `permissions:` block correctness (closes `S26-05-15`).
- **`SECURITY.md`** — new "Supply chain and release signing (v0.1.0+)" section.
- **`docs/user-guide/getting-started/helm-install.md`** — OCI install snippet (`helm install cooker oci://ghcr.io/santapong/charts/cooker --version 0.1.0`).
- **Reviewer-time gates** before tagging `v0.1.0`: (a) verify 8 action SHAs against upstream release pages, (b) set Settings → Actions → Workflow permissions to "Read and write", (c) `goreleaser check`.

#### Cross-stack — AppDetailPage refresh (PR #62)

- **Lazy-load `AppDetailPage`** — own 7.7 KB chunk; entry bundle drops a further ~7.7 KB.
- **Raw `new WebSocket(url)` migrated to `useWebSocket`** — closes a pre-existing CLAUDE.md hard-rule violation that **bypassed the FH-03 fix** from PR #49.
- **Webhook URL panel + copy** on AppDetailPage (W11 §Indie step 5).
- **Last-deploy summary card + Visit link** (W11 §Indie step 6) — surfaces `app.deployedURL`.
- **Backend schema change** to plumb `deployedURL`: migration `009_app_deployed_url.up.sql` adds `apps.deployed_url`; `model.App.DeployedURL` exposed via `GET /apps/:id`; `Prober` interface extended to return `(AppHealth, msg, url)`; `AppLister.UpdateHealth` extended with `deployedURL` (empty preserves prior via `CASE WHEN $5 <> '' THEN $5 ELSE deployed_url END`); memory + Postgres impls updated.
- **`EmptyState` two-CTA pattern** rolled to `AppsPage` + `PipelinesPage` + `EnvironmentsPage` (W11 §Indie step 2).

#### Backend fixes — T1 + T3 + handler F1 (PR #64, replaces #63)

- **T1** (`dag-adaptation` §6 T1) — `executeTest`/`executeApproval`/`executeCustom` now return `fmt.Errorf("stage type %q not implemented", ...)` instead of silently returning `nil`. Closes `dag-performance.md` Critical #1. Side-effect: pipelines using these stage types now fail loudly — they were silently broken before.
- **T3** (`dag-adaptation` §6 T3) — redundant status-drain goroutine in `executor.go:266-270` deleted. T5 in W4 will put a proper drain in.
- **Handler F1** (`docs/audits/2026-05-handler-layering.md` Finding 1) — `handler/pipeline.go`'s 57-line `validateDAG` deleted; both call sites delegate to `service.ValidatePipelineDAG`, which was extended with duplicate-stage-ID + dangling-edge checks.
- 4 test fixtures updated to use `push` instead of `test` for the loud-fail side-effect; 3 new tests pin the new contracts.

#### Frontend perf — P26-05-29 WS onMessage ref (PR #57)

- **Stable `onMessageRef`** in `useWebSocket.ts`. `useCallback` dep array trimmed from `[url, onMessage]` to `[url]`. Closes the reconnect storm that fired every time a caller (e.g. `useStageLogs`) passed a fresh arrow callback.
- Closes `P26-05-29` (deferred from W2 PR #52).

#### Research audits (W3 idle lane)

- **`docs/audits/2026-05-handler-f2-f3-extraction.md`** (PR #55) — service-layer extraction sketch for F2 (RunResult contract for terminal-status guarantee) + F3 (`service.ParseComposeGraph` extracting ~200 LOC from `handler/docker.go`). Sequence F2 first (smaller, mechanical, pattern-setting). Total ~1.5 engineering-days.
- **`docs/audits/2026-05-deploytarget-walk.md`** (PR #59) — **three real production bugs**:
  - **E-2** (High/Critical) — ECS `UpdateService` errors other than "service not found" silently fall through to `CreateService`, leaving ghost Fargate services.
  - **F-2 + F-3** (High) — Fly.io accumulates machines on every deploy (`Deploy` always creates, never updates); `Rollback` calls a non-existent URL (404 silently).
  - **R-2** (High) — Render `Status.URL` always empty because the Go JSON struct tag `"serviceDetails.url"` is treated as a literal key, not a dot-path.
- **`docs/audits/2026-05-p3-jsonb-cap-design.md`** (PR #58) — JSONB-cap enforcement for P#3 outputs. Recommendation: service-layer `ApplyOutputCap` in `internal/buildplan/outputcap.go`, mirroring the existing 1 MiB log cap pattern at `executor.go:344-373`.
- **`docs/audits/2026-05-tseries-w4-coordination.md`** (PR #61) — W4 T-series sequencing recommendation: **T4 → T2 → T5**. T2 and T5 are file-disjoint but should NOT parallelize on a single engineer. T5 is the prerequisite for Primitive #1 in W5. Flags 2-line drift in `dag-adaptation-2026.md` §6 T2 file:line citations (stale after T1's stub replacement shifted lines).

### Added — May 2026 W2 batch (PRs #42, #43, #44, #45, #46, #47, #48, #49, #50, #52, #53)

**Second week of execution against the May-2026 30-day plan.** W1 surfaced four production-shape bugs and one trust-of-tool category; W2 closes the production bugs as code PRs, ships v0.1.0 release scaffolding, drafts the multi-tenancy ADR, and produces six forward-looking research audits.

#### Production bug fixes (closes W1 findings)

- **F-01** (PR #43) — raw-K8s `deploy/kubernetes/deployment.yaml` probe paths flipped from `/health` (10s initial delay) to `/health/live` + `/health/ready` (60s initial delay, named `http` port, `timeoutSeconds: 5`, `failureThreshold: 5`). Closes the `CrashLoopBackOff` trap for operators using the raw-manifest install path. Chart/raw parity restored.
- **F-02** (PR #47) — `Config.Validate()` now refuses `COOKER_PUSHER=docker` in production with the same docker.sock RCE-to-host warning as the existing `COOKER_BUILDER=docker` guard. Mirrors the builder check; one peer test. Closes the silent regression where operators followed the builder advice but left the pusher path open. `SECURITY.md` "image build isolation" updated.
- **F-07** (PR #52) — `internal/handler/app.go` `DeployApp` now creates a stub `PipelineRun` row (`Status=running`, `StartedAt=now`) **before** calling `RunCoordinator.Spawn`. Closes the gap where Spawn's first heartbeat hit a non-existent row and `SweepOrphans` had no orphan to reap if the pod crashed mid-deploy. New `TestRunCoordinator_F07_HeartbeatSucceedsWhenRowCreatedBeforeSpawn` regression test.
- **FH-03** (PR #49) — `frontend/src/hooks/useWebSocket.ts` `connect()` now re-checks `closedByCallerRef.current` after `await fetchWSTicket()`. Closes the WebSocket-leak race where rapid unmounts during the ticket fetch accumulated dangling sockets up to the browser's 256-per-origin limit (logs view going silently dark). One-line fix.

#### Backend perf wins (closes May-2026 perf-audit findings)

- **P26-05-01** (PR #52) — `gin.Default()` → `gin.New() + gin.Recovery()`; `gin.SetMode(gin.ReleaseMode)` outside dev. Drops the verbose ANSI request logger that duplicated `observability.MetricsMiddleware`. Expected ~5–10% CPU reduction on hot HTTP paths.
- **P26-05-12** (PR #52) — `rateLimiter.mu` `sync.Mutex` → `sync.RWMutex` with separate `lastMu` for `lastSeen` writes so the lock domains never nest. Bucket-already-registered fast path now read-locked. GC loop collects-then-deletes.
- **P26-05-29** (deferred to W3) — WebSocket `onMessage` ref pattern. The riskier of the three perf wins; pulled out of the W2 PR for a separate review.

#### Release engineering — v0.1.0 scaffolding (PR #53)

- **Module path fixed.** `github.com/cooker-ci/cooker` → `github.com/santapong/cooker` across 78 Go files + `go.mod` + swagger doc + `.golangci.yml` errcheck exclusion + CHANGELOG footer links + image-ref docs (Dockerfile / Helm `values.yaml` / raw-K8s `deployment.yaml` / `docs/user-guide/operations/architecture.md`). Historical narrative references in `docs/pm-brief-2026-05.md` + `docs/shipping-go.md` intentionally preserved.
- **`cooker --version`** flag in `cmd/cooker/main.go`: three ldflags-populated package vars (`version`, `commit`, `date`); propagated into `server.BuildVersion`/`SHA`/`Time` so `GET /api/v1/version` reflects real release metadata.
- **Deferred to W3:** `.goreleaser.yaml` + `Makefile` ldflags + cosign keyless + GHCR multi-arch push + Helm OCI chart publish. The W2 agent's report claimed it shipped GoReleaser config; the actual branch did not contain it. W3 closes that.

#### Multi-tenancy ADR (PR #42)

- **`docs/adr/0004-multi-tenancy.md`** drafted with the **A3-defer** decision: ship the cheap `owner_team_id` ownership column now (closes `S26-05-09` IDOR); document the namespace-scoped `tenant_id` migration as Appendix A for if/when hosted Cooker Cloud is approved; Appendix B sketches the single-tenant-forever variant.
- **Status: Proposed** — awaiting Decision A on hosted Cloud (pm-brief §4 Q1 + Q7). Planner's deeper-read opinion: A3 still looks right; the hidden A1 advantage doesn't survive the cost math when Decision A is undecided.

#### Forward-looking research (W2 idle lane)

Each lands as one audit doc; the findings will be consumed by future PRs.

- **`docs/audits/2026-05-action-pinning.md`** (PR #44) — 17 action references across 3 workflow files; 0 currently pinned; pinning order recommended (`ci.yml` → `oci-conformance.yml` → `cooker-weekly.yml`, with `anthropics/claude-code-action@v1` FIRST within the weekly because it carries `contents: write` + `pull-requests: write`). Closes `S26-05-15`.
- **`docs/audits/2026-05-cache-plumb-sketch.md`** (PR #45) — minimal `CacheSpec` model + per-adapter integration sketches (kaniko / buildkit / buildah / docker), test strategy, Helm values shape, and 7 open questions for the future P#4 PR. **Effort confirmed at 10–11 engineering-days** for the narrow scope (matches §10 calendar's 3-week budget); 12–13 days if per-environment `cacheRepo` defaults are bundled.
- **`docs/audits/2026-05-p1-unmarshaller-corpus.md`** (PR #46) — five round-trip test cases for the `Retries int` → `Retry RetryPolicy` migration, plus the `UnmarshalJSON` skeleton and a risk register. **Design gap flagged:** `Exponential bool` is `omitempty` + "default true" — contradictory; three resolution options proposed. Implementer must pick one before P#1 ships in W5.
- **`docs/audits/2026-05-p1-context-pack.md`** (PR #48) — file:line context-pack for the three sub-agents that will sub-delegate Primitive #1 implementation (backend-api / frontend-ui / frontend-state). PR-T5 conflict check confirmed clean: T5's batched persistProgress absorbs P#1's extra status-transition emit volume. Three doc-drift findings in `dag-adaptation-2026.md` §7.1 worth a small follow-up PR.
- **`docs/audits/2026-05-w11-quickwin-wireframes.md`** (PR #50) — text wireframes for three W11 P2 items (webhook URL on AppDetailPage, deployed URL on AppDetailPage, empty-state CTAs on Apps/Pipelines/Environments). PR-#38 bundle-split scan confirmed no drift, but surfaced **two pre-existing hygiene issues**: `AppDetailPage` is eager-loaded despite never being a landing page, and `AppDetailPage.tsx:80` contains a raw `new WebSocket(url)` that violates the CLAUDE.md hard rule (and **bypasses the FH-03 fix** because it doesn't use the hook). W3 follow-up planned.

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

**Seven-workstream audit run. Four waves of consolidation. No production code changes — this PR ships the planning + research surface that the 30-day execution plan runs against.** Detailed scope and decisions in [`docs/pm-brief-2026-05.md`](docs/proposals/pm-brief-2026-05.md).

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

[Unreleased]: https://github.com/santapong/cooker/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/santapong/cooker/releases/tag/v0.1.0
