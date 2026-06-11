# 2026-05 Half-Shipped Feature Sweep

**Branch:** `claude/research-half-shipped`
**Scope:** Features that crossed only some of the four layers (handler → service → store → frontend). Three patterns:
- **(B-only)** Backend exists, frontend doesn't consume it.
- **(F-only)** Frontend references an endpoint that returns 501, a hard-coded mock, or a `TODO` that pretends to succeed.
- **(half-Zustand)** Store action exists with no UI consumer, or API client method exists with no store action.

**Method:** Read-only sweep against HEAD of `main`. `grep` for 501 / `TODO` / mock returns on the backend; route-by-route trace through `frontend/src/api/*`, `frontend/src/stores/*`, `frontend/src/pages/*` and `frontend/src/components/pipeline/**`.

**Severity ranks:** **User-facing bug** (a real action the UI exposes silently no-ops or errors) / **Inconsistency** (the wiring works but persistence or business logic is fake — refresh reveals the lie) / **Cosmetic** (dead code, never reachable).

**Stable IDs:** `HS26-05-NN`.

**Companion to:**
- [`backlog.md`](../../backlog.md) — many of these are already tracked; cross-refs in each finding.
- [`launch-readiness.md`](launch-readiness.md) — every **User-facing bug** below is a launch blocker for the persona that hits it.
- [`W11-user-journeys.md`](W11-user-journeys.md) — the persona friction this audit is graded against.

---

## TL;DR — top 5 highest-impact gaps (ranked by W11 persona friction)

1. **`HS26-05-01` (User-facing bug)** — **Promotion + Approval gates are decorative.** `RunPage.tsx` exposes "Promote to Staging" and "Approve" buttons that POST to `/pipelines/:id/runs/:runId/{promote,approve}`; the handlers return `{"message": "promotion initiated"}` without writing anything. `GetEnvStatus` always returns `statuses: []`. The W11 SaaS-team and Enterprise-SRE journeys both depend on this flow.
2. **`HS26-05-02` (User-facing bug)** — **GitHub webhook does not deploy.** ~~`handler/app.go:338` is a `TODO: enqueue a real deploy`.~~ **CLOSED** (`claude/jolly-knuth-j396dx`) — a signature-verified push to a matching branch of an auto-deploy app now spawns a real deploy via the shared `triggerWebhookDeploy` helper (converging GitHub/GitLab/Gitea/Bitbucket onto the manual `DeployApp` path). Closest W11 friction: Indie hacker §step 5–6.
3. **`HS26-05-03` (User-facing bug)** — **Three pipeline stage types are no-ops at runtime.** ~~`executeTest`, `executeApproval`, `executeCustom` log and return nil.~~ **CLOSED** (`claude/jolly-knuth-j396dx`) — Test/Custom now run user code in an isolated container via the `stagerunner` backend (`COOKER_STAGE_RUNNER`: kube Job / docker run / noop), pass/fail by exit code; Approval is a persisted pause-gate (`stage_approvals`, migration 022) the executor blocks on until approved/rejected, with admin-or-approver endpoints and a run-page Approve/Reject affordance. ML and Enterprise journeys depend on a real Test/Custom; SaaS journey depends on a real Approval.
4. **`HS26-05-04` (User-facing bug)** — **`SettingsPage` registry + cluster CRUD persists nothing.** ~~`handler/registry.go:95-132` and the matching frontend forms accept input, return a `201 {message: "registry added"}`, then `ListRegistryConfigs` returns `[]` on the next paint.~~ **CLOSED** (`claude/jolly-knuth-j396dx`) — the five Settings handlers now persist through real `RegistryConfigStore` / `ClusterConfigStore` (memory + Postgres, migration 021), mirroring the HostStore pattern; sensitive credentials are written to `secrets.Manager` and never returned. Operator-onboarding friction is severe — every Pro-tier persona hits this on first login.
5. **`HS26-05-05` (User-facing bug)** — **`/api/v1/kubernetes/*` is fully stubbed.** `KubernetesPage` lists namespaces and workloads — both come back empty — and never invokes the `scale` / `restart` / `apply` / `deleteResource` actions defined in `kubernetesApi` and `kubernetesStore`. The whole `Workloads` page renders an "All namespaces / No workloads found" empty state in every environment. Enterprise SRE journey §step 5 is gated by this.

---

## (B-only) Backend exists, no frontend consumer

### `HS26-05-06` — Pipeline Run cancel — **Inconsistency** (covered, but only via the run page; not in lists)

- **Backend:** `POST /pipelines/:id/runs/:runId/cancel` → `handler.CancelPipelineRun` (`handler/pipeline.go`), wired in `router.go:77`.
- **Frontend:** No `cancelRun` method exists in `frontend/src/api/pipelines.ts`. `RunPage.tsx` shows status pills but no Cancel button. The endpoint is fully functional and unreachable from the UI.
- **Symptom:** Operator who starts a stuck run has no graceful way to stop it from the UI; they must POST manually with curl or wait for `runDeadline`.
- **Backlog:** Not tracked. Suggested tier: **W11-medium** (Indie + ML journeys both hit it; ML's iterative-build loop especially).

### `HS26-05-07` — App update `PUT /apps/:id` — **Inconsistency**

- **Backend:** `PUT /apps/:id` → `handler.UpdateApp`, mutating any field on `model.App`.
- **Frontend:** `appsApi` has no `update` method; `AppDetailPage.tsx` exposes only the webhook-rotation flow and `delete`. There's no way from the UI to toggle `autoDeploy` after creation, change the source repo, or update `defaultBranch`.
- **Symptom:** Indie hacker creates an App with `autoDeploy: true`, later wants to turn it off, has to delete and recreate.
- **Backlog:** Closest is W11 §Indie step 5 ("Webhook URL surfaced on AppDetailPage" — `backlog.md:261`). Suggested addition: **W11-low**, add an "Edit App" UI alongside.

### `HS26-05-08` — `GET /pipelines/:id/runs/:runId/env-status` — **half-shipped (UI calls, backend stub)**

- **Backend:** Returns `{"statuses": []}` always (`handler/environment.go:299-308`). No iteration over `model.Pipeline.Environments`, no read from `Promotion`/`Approval` store (there is no such store).
- **Frontend:** `RunPage.tsx:77` calls `pipelineApi.getEnvStatus`, swallows errors `/* env-status is best-effort */`, and renders a "no environments" empty state.
- **Symptom:** Promotion lane in the run page is permanently blank. Pairs with `HS26-05-01` — together they make the whole promotion/approval surface theatre.
- **Backlog:** Not tracked. Suggested tier: **W11-P1**. Same scope as fixing `HS26-05-01`.

---

## (F-only) Frontend exists, backend returns 501 / mock / TODO

### `HS26-05-01` — Promotion + Approval flow is theatre — **User-facing bug (HIGH)**

- **Backend stubs:** `handler.PromoteRun` (`handler/environment.go:259-268`), `handler.ApprovePromotion` (270-297), `handler.GetEnvStatus` (299-308). None of them write to any store. No `Promotion` / `Approval` table exists in `internal/store/postgres/migrations/`.
- **Frontend:** `RunPage.tsx:599-616` calls `promoteRun` and `approvePromotion` with environmentId; on success pushes a green "Approved <env>" toast. The `environmentStore.approve` action sends `{approvedBy}` (line 55) — different field than the handler reads (`{note}` at handler/environment.go:286-289), so the field is silently ignored but the call "succeeds".
- **Symptom:** User clicks Promote → sees green toast → refreshes → run is still pending in the source environment. User clicks Approve → sees green toast → the next time RequireApprovers is checked, no approval is recorded.
- **Backlog:** Implicit in `W11-user-journeys.md` §SaaS step 3 / §Enterprise step 5 but no concrete backlog item. **Add as P1.** This single feature anchors the W11 SaaS persona narrative.
- Bonus: `environmentStore.approve` and the handler also disagree on the request body shape (`approvedBy` vs `note`). Even after the handler is built out the store will need updating.

### `HS26-05-02` — GitHub webhook deploy is a TODO — **User-facing bug (HIGH)** — CLOSED

- **Backend:** `handler/app.go:338` — `// TODO: enqueue a real deploy (synthesise a Clone→Build→Push→Deploy run).` HMAC signature is verified; if `autoDeploy=true` the response is `202 {"appId", "commit", "branch", "status": "deploy queued"}` — no run is created.
- **Frontend:** `AppDetailPage.tsx:181-200` shows the `webhook` and `auto-deploy` pills, the rotate-secret flow works, and the user can copy the webhook URL into GitHub (well — almost, see `HS26-05-09`). The end-to-end loop appears complete from the UI side.
- **Symptom:** Indie hacker wires up the webhook, pushes a commit, GitHub shows a green "delivered 202" — and Cooker never builds anything. The mismatch is invisible until they wonder why their changes aren't live.
- **Backlog:** Not directly tracked. Related: W11 §Indie step 5–6 (`backlog.md:261-262`). **Add as P1.**

**Status: CLOSED** — Fixed in `claude/jolly-knuth-j396dx`. The four webhook handlers (`GitHubWebhook`, `GitLabWebhook`, `GiteaWebhook`, `BitbucketWebhook`) all reached an identical TODO after signature verification + the `AutoDeploy` check; they now converge on a single `handler.triggerWebhookDeploy` helper that reuses the exact manual-deploy path (`DeployApp` → stub run row → `RunSpawner.Spawn` → `runAppDeployCtx` → `AppDeployer.Deploy`). A signature-verified push to a matching branch of an auto-deploy app now creates a real run, attributed to the webhook via `PipelineRun.StartedByEmail = "webhook:<source>"` (e.g. `webhook:github`), and lands in deploy history (`app_deploys`, migration 018) exactly like a manual deploy. GitHub redeliveries don't double-deploy: the response stays a 2xx so the existing idempotency middleware (keyed on `X-GitHub-Delivery`) replays it. Tests: `TestGitHubWebhook_Valid_HMAC_TriggersDeploy` (a verified push creates+dispatches a webhook-attributed run) and `TestGitHubWebhook_AutoDeployDisabled_NoRun` (no run/dispatch when `AutoDeploy=false`).

### `HS26-05-03` — Three pipeline stage types are no-ops — **User-facing bug (HIGH)** — CLOSED

**CLOSED** on `claude/jolly-knuth-j396dx`. The three stage runtimes are real:

- **Test + Custom (containerized runner):** a new `internal/stagerunner` package mirrors the builder adapter pattern — `Runner` interface with `Kube` (one-shot `batch/v1.Job`, reusing the Kaniko Job-watch/log-stream plumbing), `DockerRun` (`docker run --rm`), and `Noop` (dev/test) backends, selected by `COOKER_STAGE_RUNNER` in `selectStageRunner` (server.go). `executeTest`/`executeCustom` build a request from stage config (image + command/script + env), stream container logs into the existing stage-log path, and pass/fail by exit code. User script text is **never** exec'd on the Cooker process. A Test/Custom stage with no image fails loudly.
- **Approval (persisted pause-gate):** migration `022_stage_approvals` adds `stage_approvals` + `stage_approval_votes` (mirrors migration 020's run_promotions shape, with DB-level distinct-approver counting). `executeApproval` opens the gate, broadcasts the stage as `awaiting` over the existing status WS channel, and blocks — polling the gate — until approved (distinct-approver threshold met → stage succeeds), rejected (→ stage fails), or `ctx` is cancelled (run deadline / cancel / stage timeout → stage fails). Approve/reject is gated to admin-or-approver (`CanApprovePromotion`, identity from claims) via `POST /pipelines/:id/runs/:runId/stages/:stageId/{approve,reject}`; a `GET .../stage-approvals` list endpoint backs the run-page Approve/Reject affordance.
- **Tests:** executor success / nonzero-exit / ctx-cancel for the runtime stages (fake runner), approval gate approve-resumes / reject-fails / ctx-cancel / no-service-fails-loud, store parity for the new table, handler RBAC (viewer forbidden, admin/approver allowed, approve-after-reject = 409). Backend `go test ./... -race` green.
- **Original finding (for the record):** `executeTest`/`executeApproval`/`executeCustom` returned `nil` (later hardened to fail-loud stubs by the W3 fix). The dispatch table calls them for `StageTypeTest`/`StageTypeApproval`/`StageTypeCustom`. No script ran, no container started, no gate blocked.

### `HS26-05-04` — Settings registry + cluster CRUD is fake — **User-facing bug (MEDIUM, high-leakage)**

- **Backend:** `handler/registry.go:95-132` (`ListRegistryConfigs`, `AddRegistryConfig`, `DeleteRegistryConfig`, `ListClusterConfigs`, `AddClusterConfig`). All five return synthesized success responses with no store interaction.
- **Frontend:** `SettingsPage.tsx` (lines 81, 178, 230, 316) calls every one of them and renders the result. Empty list on load, "added" toast on submit, empty list again on next paint.
- **Symptom:** Operator adds a registry credential set on first run, sees the green toast, navigates away, comes back, registry is gone. Looks like a bug to them; in fact nothing was ever persisted.
- **Backlog:** Not tracked. **Add as P1.** The right shape is probably a small `RegistryConfigStore` / `ClusterConfigStore` reusing the same memory+postgres pattern as `HostStore`. The duplication with `/hosts` is worth resolving — these may collapse into one resource.

**Status: CLOSED** — Fixed in `claude/jolly-knuth-j396dx`, following the audit's recommended shape exactly. New `store.RegistryConfigStore` + `store.ClusterConfigStore` interfaces (List/Get/Create/Delete) with memory and Postgres implementations and migration `021_settings_configs` (two tables, idempotent up, reversible down) — modelled column-for-column on the `hosts` table. The five handlers (`ListRegistryConfigs`, `AddRegistryConfig`, `DeleteRegistryConfig`, `ListClusterConfigs`, `AddClusterConfig`, plus a new symmetric `DeleteClusterConfig`) are now `*Handler` methods that persist for real; the add→refresh round-trip the finding called out now survives a reload. Credentials are handled like the SSH-host private key: the registry password and cluster kubeconfig are written to `secrets.Manager` by a thin service layer (`RegistryConfigService` / `ClusterConfigService`) and only an opaque reference (`password_ref` / `kubeconfig_ref`) lands on the row — no new crypto scheme. List/Get return the `Redact()`ed view carrying a `hasPassword` / `hasCredentials` boolean only; the secret bytes never appear in any response. Nil-safe in dev: a config that carries a credential when no secrets backend is configured returns 503, while a credential-free (anonymous-registry / context-only-cluster) config persists regardless. Tests: store CRUD + not-found (memory), and handler create→list→delete round-trip plus credential-redaction assertions for both resources (`TestRegistryConfig_CreateListDelete_RoundTrip`, `TestRegistryConfig_CreateRedactsPassword`, `TestClusterConfig_CreateListDelete_RoundTrip`). The `/hosts`-collapse idea was left for a future pass — these stay distinct resources for now.

### `HS26-05-05` — Kubernetes endpoints all stubbed — **User-facing bug (MEDIUM)**

- **Backend:** `handler/kubernetes.go:11-80` (`ListNamespaces`, `ListWorkloads`, `GetWorkload`, `ScaleWorkload`, `RestartWorkload`, `GetPodLogs`, `ApplyManifest`, `DeleteResource`). Every one returns either an empty list or a literal `"message"` string. No `client-go` consumer despite `deployer/clientgo.go` already being wired.
- **Frontend:** `KubernetesPage.tsx` fetches namespaces + workloads, then renders an empty table. `kubernetesStore.scale` / `restart` exist but no UI invokes them. `useKubeWatch` and `kubernetesApi.apply` / `deleteResource` are defined but unused.
- **Symptom:** Enterprise SRE clicks the "Workloads" sidebar entry, sees "No workloads found in the selected namespace", concludes Cooker has no live cluster view. (And they're right.)
- **Backlog:** Not tracked. **Add as P1** — but worth noting that the deployer-layer `clientgo` package is in place, so the gap is reading not writing. ~1 day to plumb a read-only listing through.
- Note: `wshub.HandleKubeWatch` is registered at `router.go:249-251` and `useKubeWatch` consumes it, but no page in the app calls `useKubeWatch`. The whole watch path is reachable but unconsumed.
- **Status: PARTIALLY RESOLVED.** The read path (`ListNamespaces`/`ListWorkloads`/`GetWorkload`/`GetPodLogs`) was wired to a real read-only client-go client on `claude/fervent-sagan-q50XA` (see backlog "Closed") and is no longer stubbed. The **write** part (`ScaleWorkload`/`RestartWorkload`/`ApplyManifest`/`DeleteResource`) is now **MITIGATED (closed-by-501)** on `claude/jolly-knuth-j396dx`: these return `501 {error,operation,hint}` via `notImplementedKubeWrite` instead of the fake 2xx (`scaled`/`rolling restart initiated`/`manifest applied`/`resource deleted`). The 501 is deliberately distinct from the read path's 503 `kubeUnavailable` — the read client may be configured and reachable; the write path simply isn't wired. The `kubernetesStore.{scale,restart}` actions have no UI consumer (`HS26-05-13`), so no success toast had to be re-toned; the api client already throws `ApiError` on the 501. Building the real mutating path stays future work.

### `HS26-05-09` — Webhook URL is not exposed on `AppDetailPage` — **User-facing bug (LOW)**

- **Backend:** `POST /webhooks/github` is the documented URL; nothing returns it to a UI caller.
- **Frontend:** `AppDetailPage.tsx:181-200` renders the `webhook` pill and the "Rotate webhook secret" button but never tells the user **what URL to paste into GitHub**. They have to read `docs/openapi.yaml` or guess.
- **Symptom:** Indie hacker sets webhook secret, copies it, goes to GitHub → realises they don't know the URL. Bounces back to the docs.
- **Backlog:** Tracked — `backlog.md:261` "Webhook URL surfaced on `AppDetailPage` next to the AutoDeploy toggle, with a copy button". **Already P1**, just listing for completeness.

### `HS26-05-10` — Compose service edit pretends to persist — **User-facing bug (LOW)**

- **Backend:** `PUT /docker/compose/services/:name` → `UpdateComposeService` at `handler/docker.go:250-266`. Accepts a body, returns `200 {"message":"Service config updated"}` — does not touch the on-disk compose file, does not call the docker host transport, has no store.
- **Frontend:** `composeStore.ts:144` calls it from `ComposePage` mutations.
- **Symptom:** User edits a compose service in the Compose editor, sees the change in the local Zustand state, refreshes, the change is gone. (The compose file on disk is the source of truth and we never wrote to it.)
- **Backlog:** Not tracked. Suggested tier: **W11-low**. ComposePage is a Pro feature seen by Enterprise + Indie; data-loss UX is the concern.

### `HS26-05-11` — Network / Volume write endpoints — **Inconsistency (intentional, but UI doesn't differentiate)**

- **Backend:** `handler/network.go` + `handler/volume.go` correctly return **structured 501** with `{error, operation, hint}` after the `backlog.md:125` cleanup.
- **Frontend:** `DockerPage.tsx:272, 297, 339, 449` calls `createNetwork` / `createVolume` and on success pushes a green toast. **The frontend treats 501 as failure** (the `await` throws via `api/client.ts`), so the user actually sees the error — **but** the surrounding text in `DockerPage.tsx:272` reads "Network create requested" optimistically. The flow is technically honest (error toast fires) but the optimistic-success copy on the happy path will be a copy bug once the transport ships.
- **Symptom:** Today: user clicks Create Network, sees a red error toast with "docker host transport not configured". Acceptable. Future: when the transport ships, the success path is fine.
- **Backlog:** Tracked — `backlog.md:125` (closed) + P9.4 host transport (blocked on Go 1.26). Suggested: add an integration test that asserts the toast text matches the response.

---

## (half-Zustand) Half-wired store / API client / hook

### `HS26-05-12` — `dockerStore.{buildImage, deleteImage, stopContainer, deleteContainer}` — **Cosmetic**

- **Store:** `frontend/src/stores/dockerStore.ts:45-62`. Four actions defined.
- **Consumers:** Zero — `DockerPage` only uses `fetchImages` and `fetchContainers`. The Build / Stop / Delete buttons on the image and container tables either don't exist or call `dockerApi` directly.
- **Why it matters:** These actions also wouldn't work end-to-end (`handler/docker.go:73-130` is stubbed; see `HS26-05-15`), but the store skeleton being present implies they were once intended.
- **Backlog:** Not tracked. **Cosmetic** — fold into whichever PR brings the Docker SDK plumbing online.

### `HS26-05-13` — `kubernetesStore.{scale, restart}` + `kubernetesApi.{apply, deleteResource}` — **Cosmetic**

- **Store:** `kubernetesStore.ts:52-59`. Actions exist; no UI consumer (confirmed via `grep`).
- **API client:** `kubernetesApi.apply` and `kubernetesApi.deleteResource` (`api/kubernetes.ts:14-17`); no store action and no page calls them.
- **Hook:** `useKubeWatch` (`hooks/useKubeWatch.ts`) — no consumer.
- **Why it matters:** Whole "act on workload" surface is plumbed at the wire level (frontend → backend stub) but has no UI affordance.
- **Backlog:** Same fix as `HS26-05-05`.

### `HS26-05-14` — `environmentStore.approve` body shape mismatch — **User-facing bug (LOW)**

- **Store:** `environmentStore.ts:54-55` posts `{approvedBy}`.
- **Handler:** `ApprovePromotion` reads `claims.Email` (correctly — IDOR-safe) and accepts an optional `{note}` from the body (`handler/environment.go:286-289`).
- **Why it matters:** The `approvedBy` field is silently dropped. The bug is latent because the handler is itself a stub (`HS26-05-01`) — but it'll bite the day someone wires up the real approval persistence layer expecting the store to pass through the right shape.
- **Backlog:** Fold into `HS26-05-01`.

### `HS26-05-15` — Docker handler stubs that the UI calls — **half-Zustand siblings**

- **Backend:** `handler/docker.go:59-130` — `ListDockerImages`, `GetDockerImage`, `BuildDockerImage`, `DeleteDockerImage`, `ListContainers`, `CreateContainer`, `StopContainer`, `DeleteContainer`, `GetContainerLogs` all return placeholder responses (empty list, hard-coded `"build-placeholder"` IDs, `"container-placeholder"`, etc).
- **Frontend:** `DockerPage` fetches via `dockerStore` and renders empty tables. The "build" `buildId` returned is the literal string `"build-placeholder"`; if any UI ever tried to open `/ws/docker/build/build-placeholder` it would never get any messages.
- **Symptom:** Same as `HS26-05-05` — page renders empty, looks broken. Indie hacker + SaaS persona §step 1–2.
- **Backlog:** Implicit in P9.1 closures (which only addressed builder/pusher/deployer for the pipeline path, **not** the standalone `/docker/*` REST surface).
- **Status: MITIGATED (closed-by-501)** — `claude/jolly-knuth-j396dx`: the standalone docker write stubs (`BuildDockerImage`, `DeleteDockerImage`, `CreateContainer`, `StopContainer`, `DeleteContainer`) now return `501 {error,operation,hint}` via `notImplementedDockerHost` instead of fake 2xx (`build-placeholder`/`container-placeholder`/`stopped`/`removed`), so the api client throws and the UI shows an honest error rather than a green toast for an action that never ran. Real Docker host transport remains future work (P9.4). Reads were already honest (empty-200 list, 501 inspect).

### `HS26-05-16` — `RunHistoryPanel` is unimported — **Cosmetic**

- **Component:** `frontend/src/components/pipeline/panels/RunHistoryPanel.tsx`.
- **Consumers:** None.
- **Symptom:** Dead component; tree-shaken from the bundle but lingers in source.
- **Backlog:** Not tracked. **Cosmetic** — either wire into `PipelineEditorPage` (where it was presumably intended) or delete in the next frontend cleanup pass.

### `HS26-05-17` — `StageTypeGitOpsCommit` has no palette entry — **Inconsistency**

- **Backend:** Stage type fully implemented (`executor.go:472+`, `internal/gitops/gogit.go`). Validated via `validate.StageType`.
- **Frontend:** `PipelineToolbar.tsx:13-18` lists the six common stage types but **not** `gitops-commit`. `NodeConfigPanel.tsx` has no branch for it.
- **Symptom:** User can't add a GitOps stage from the UI palette. The only way to land one is to POST a pipeline JSON directly. Closes part of the GitOps story (P9.3 ✅) but the frontend never caught up.
- **Backlog:** Not tracked. Suggested tier: **W11-low** — add palette + inspector for `gitops-commit`. ~1 hour of frontend work.

---

## Cross-cutting observations

- The handler-layer stubs concentrate in three packages: **`registry.go`**, **`kubernetes.go`**, and **`docker.go`**. Each was scaffolded with "Placeholder: will use <SDK>" comments and never followed up. `network.go` + `volume.go` were correctly migrated to honest 501s in `backlog.md:125`. Doing the same triage pass on the other three would be cheap and would immediately turn a fleet of green toasts into red ones — a strict improvement in honesty.
- The **promotion / approval** surface is the single highest-leverage gap: it's referenced by three W11 personas and the entire feature is theatre.
- The **stage-type runtime no-ops** (`HS26-05-03`) are the most insidious because the UI shows the stage going green. A user could ship a pipeline they believe runs tests and approval gates, and have neither. This is a trust-of-the-tool issue.

## Recommended quick-win sequence (in order)

1. **`HS26-05-04` (settings persistence)** — wire `RegistryConfigStore` + `ClusterConfigStore`, mirror the `HostStore` pattern. ~½ day. Closes a glaring first-impression bug.
2. **`HS26-05-15` honest 501s** — apply the network/volume treatment to `docker.go` + `kubernetes.go` + `registry.go` stubs that the UI silently treats as success. ~1 hour. Buys a working error UX until the real SDK plumbing lands.
3. **`HS26-05-09` webhook URL surface** — already P1 in backlog. ~1 hour, all frontend.
4. **`HS26-05-17` gitops palette** — ~1 hour frontend; closes the loop on the otherwise-shipped P9.3.
5. **`HS26-05-01` + `HS26-05-08` promotion + approval persistence** — the substantive item. Needs an ADR for whether approvals are a `model.Approval` row (with foreign keys to run + env + user) or an array column on `PipelineRun`. ~1–2 days end-to-end.

Then `HS26-05-02` (webhook deploy) and `HS26-05-03` (stage-type runtimes) are the next tier — each a half-day to several days of substantive work.
