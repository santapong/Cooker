# Post-merge audit — 2026-07-06 (canary / license / cloud-inventory / feedback / route authz)

Targeted audit of the surfaces that landed after the four-doc audit series: canary
deployments (PR #113, merged `b7640fa`), the license store, the read-only cloud
inventory (OR-2 phase 1), the feedback→GitHub-issue relay (PR #123), and the
authz chains of all newly added routes.

**Method:** six scoped finder agents (exact file lists, the `cooker-audit`
anti-pattern catalogue), then an independent adversarial verification agent per
Medium+ finding, prompted to refute. Only findings that survived refutation are
listed as confirmed. IDs are `PM26-07-NN`, mirroring the `HS26-05-*` convention.

**Headline verdict:** the canary feature is **not usable in its merged state** —
PM26-07-01 (stable image always empty) and PM26-07-03 (immutable-selector
mutation) each independently make `Start` fail against a real cluster for any
previously-deployed app; the unit tests pass because the deployer is mocked.
Recommended order: fix 01+03 together (one PR, plus a fake-API-server or envtest
integration test), then 02+05+06 (sweeper/concurrency batch), then 04+07 and the
UAT Scenario 1b run to prove the feature end-to-end. Cloud-inventory (08/09/12),
feedback (10), and the authz gap (11) are independent one-PR fixes.

**Verification status of the codebase at audit time:** push-CI green on `main`
(`b7640fa`) across backend (race + Postgres), frontend, helm, docker, and docs
link-check; migration chain 001–025 applied clean in CI; `025_canary`
up/down reviewed statically (idempotent, reversible, defaulted NOT NULLs). The
live down/up drill was not run (no Docker daemon in the audit sandbox) and
remains a follow-up alongside UAT Scenario 1b.

## Confirmed findings (adversarially verified)

### [PM26-07-01] HIGH — stableImageFor always returns empty string, rendering the stable Deployment with `image:` blank — every canary Start fails and Abort has no rollback target

- **Area:** canary-concurrency · **Where:** `backend/internal/service/canary.go:387` · **Category:** data-integrity
- **Failure scenario:** GetActive only returns status='progressing' rows, and Start has already returned 409 if one exists — so at the point stableImageFor runs, GetActive is guaranteed ErrNotFound and the function always returns "". canaryManifest then emits the stable Deployment with `image: ` (empty), which the API server rejects (spec.containers[0].image: Required value). Every canary Start on Kubernetes fails at DeployWeighted and is recorded as CanaryFailed; the documented fallback ('fall back to the canary image') is never implemented — the code returns "" not canaryImage. Abort likewise deploys the full pool onto an empty stable image, so auto-rollback of an unhealthy canary would also fail.
- **Suggested fix:** Return the app's currently-deployed image (read the live Deployment or the most recent resolved AppCanary/AppDeploy row), and if truly unknown fall back to canaryImage as the comment promises — done in Start where canaryImage is in scope.
- **Adversarial verification:** CONFIRMED — Confirmed, not refuted. GetActive is literally `WHERE app_id=$1 AND status='progressing'` (postgres/app_canary.go:65-68; memory impl matches), and Start (canary.go:127-131) returns 409 ErrCanaryInFlight whenever GetActive succeeds, so when stableImageFor (canary.go:387-393) runs, GetActive is guaranteed ErrNotFound and the function always returns "". The doc-commented fallback "fall back to the ca
- **Status:** Closed by 3b06563 (PR #125)

### [PM26-07-02] HIGH — Sweeper runs on every replica with no leader election and Update has no status guard — replicaCount=3 can concurrently Promote and Abort the same canary, leaving cluster traffic and DB status contradictory

- **Area:** canary-concurrency · **Where:** `backend/internal/service/canary.go:273` · **Category:** multi-replica
- **Failure scenario:** server.go:574 starts RunSweeper unconditionally per pod. With replicaCount=3 all three sweepers list the same progressing canary in the same 15s tick. Replica A probes healthy and calls Promote (DeployWeighted weight=100); replica B probes moments later, sees the pods mid-rollout as degraded, and calls Abort — both loaded the row while still 'progressing' (loadActive is a plain read, and the UPDATE has no status predicate, so the second write silently overwrites the first transition). Ordering B-deploy-after-A-deploy leaves the cluster at weight 0 (all stable) or weight 100 (all canary) while the DB row says the opposite. Same race exists single-replica between the sweeper and an operator's manual Promote/Abort. Nothing in code or docs documents a single-replica restriction for the sweeper (only the rate limiter carries that caveat).
- **Suggested fix:** Make the terminal transition a compare-and-swap (UPDATE ... WHERE id=$1 AND status='progressing', 0 rows → someone else resolved it, skip the K8s call) and flip status BEFORE calling DeployWeighted, or gate the sweep with leader election / SELECT ... FOR UPDATE SKIP LOCKED.
- **Adversarial verification:** CONFIRMED — Verified against source. server.go:574-580 starts RunSweeper on every pod whenever a weighted deployer is configured — no leader election exists anywhere in backend/ (grep for leader/elect finds only Go select statements). postgres/app_canary.go:76-91 confirms the UPDATE is `WHERE id=$1` with no `AND status='progressing'` predicate and no version column, so a second terminal transition silently ov
- **Status:** Closed by 3b06563 (PR #125)

### [PM26-07-03] HIGH — Canary stable Deployment mutates the immutable spec.selector of the app's existing Deployment — Start fails on any previously-deployed app

- **Area:** canary-concurrency · **Where:** `backend/internal/deployer/weighted.go:82` · **Category:** correctness
- **Failure scenario:** The normal deploy path (defaultKubernetesManifest) creates Deployment <app> with selector {app: <app>}. canaryManifest re-applies the same-named Deployment with selector {app, track: stable}. Deployment spec.selector is immutable, so the API server rejects the apply with 'field is immutable' regardless of SSA Force — both kubectl and client-go backends fail. Result: canary Start returns 500/failed for every app that has ever been deployed the normal way, which is the only realistic precondition for wanting a canary.
- **Suggested fix:** Keep the stable Deployment's selector identical to the normal deploy ({app: name}) and put track only on pod labels, or delete+recreate, or name the stable track differently and drain the original Deployment.
- **Adversarial verification:** CONFIRMED — Confirmed, not refuted. canaryManifest (weighted.go:49) names the stable Deployment identically to the normal deploy's Deployment (stable := app; sanitizeName mirrors service.sanitize, and canary.go:142 passes app.Name), but writeDeployment (weighted.go:82) emits selector.matchLabels {app, track: stable} whereas defaultKubernetesManifest (app_deployer.go:457-458) created it with {app} only. apps/v
- **Status:** Closed by PR-2 (canary-batch2)

### [PM26-07-04] HIGH — Auto-promote health gate probes the app, not the canary workload — a crash-looping canary behind a healthy stable auto-promotes to 100%

- **Area:** canary-chains · **Where:** `backend/internal/service/canary.go:349` · **Category:** correctness
- **Failure scenario:** Prober's contract (service/app_health.go:37-39) is Probe(ctx, *model.App) — the interface carries no canary scoping, so the registered per-target prober checks the app's workload (the stable Deployment / ingress), not the -canary Deployment created by DeployWeighted. Scenario: canary image crash-loops at 10% weight while stable pods stay Ready; the window elapses; probeHealthy returns healthy from the stable workload; SweepAutoPromote calls Promote and shifts 100% of traffic onto the broken image — the exact outage canarying exists to prevent. (Conversely, if a selector-based prober does aggregate canary pods, an unhealthy CANARY marks the whole APP failed on the health badge — either behavior shows the probe isn't canary-scoped.)
- **Suggested fix:** Give the canary path a canary-aware probe: either extend Prober (or add a CanaryProber) that takes the canary workload name/namespace from the AppCanary row, or have the weighted deployer expose readiness of the canary ReplicaSet and gate promotion on that.
- **Adversarial verification:** CONFIRMED — The defect is real, and the shipped wiring is even weaker than the claim describes. Verified facts: (1) `probeHealthy` (backend/internal/service/canary.go:345-361) gates auto-promotion on `s.prober.Probe(ctx, app)`, and the `Prober` contract (backend/internal/service/app_health.go:37-39) takes only `*model.App` — there is no way to scope the probe to the `-canary` Deployment that `DeployWeighted` 
- **Status:** Closed by PR-3 (canary-batch3)

### [PM26-07-05] MEDIUM — Sweeper's K8s and DB calls run on the long-lived background context with no per-tick timeout

- **Area:** canary-concurrency · **Where:** `backend/internal/service/canary.go:302` · **Category:** timeout
- **Failure scenario:** SweepAutoPromote → evaluate → Promote/Abort → DeployWeighted all inherit the server-lifetime canaryCtx. A hung API server connection (client-go Patch with no deadline, or a wedged kubectl child process) blocks the sweep loop indefinitely — every other progressing canary's auto-promote/rollback stalls past its health window, and at shutdown the 2s drain in server.go:583-586 gives up and leaks the goroutine mid-mutation. Violates the repo invariant that every external I/O call carries a bounded context; the adjacent audit sweep (server.go:605) wraps each pass in context.WithTimeout(…, time.Minute) — the canary sweep does not.
- **Suggested fix:** Wrap each sweep pass (or each per-canary evaluate) in context.WithTimeout derived from canaryCtx, mirroring the audit retention sweep.
- **Adversarial verification:** CONFIRMED — Confirmed by direct reading. (1) backend/internal/service/canary.go:301-313 — RunSweeper passes its ctx (the server-lifetime canaryCtx from server.go:575) straight into SweepAutoPromote on every tick; there is no context.WithTimeout anywhere in the canary sweep path (grep of internal/service shows WithTimeout only in app_detect, executor, jobqueue_runner, secrets_check). (2) The sweep is strictly 
- **Status:** Closed by PR-2 (canary-batch2)

### [PM26-07-06] MEDIUM — Start TOCTOU: cluster is mutated (build + DeployWeighted) before the unique-index check, so the losing concurrent Start's image can be what actually runs

- **Area:** canary-concurrency · **Where:** `backend/internal/service/canary.go:135` · **Category:** concurrency
- **Failure scenario:** Two concurrent Starts for the same app both pass the GetActive pre-check (line 127), both build distinct images and both apply a weighted split to the cluster; only then does Create hit uq_app_canaries_active — the loser correctly gets 23505 → ErrConflict → 409 (mapping verified in postgres/app_canary.go:47-48 and the memory store, so parity holds). But if the loser's DeployWeighted landed second, the cluster is serving the loser's canary image while the persisted AppCanary row records the winner's image — the sweeper then promotes/aborts based on a row that doesn't describe what's deployed.
- **Suggested fix:** Create the progressing row (or a 'pending' row) BEFORE building/deploying so the unique index serializes Starts, then update it with the image after the split is established; delete/mark-failed on error.
- **Adversarial verification:** CONFIRMED — Confirmed by reading the code. In /home/user/Cooker/backend/internal/service/canary.go Start(), the only pre-check is the advisory GetActive at line 127; BuildAndPushImage (line 135) and DeployWeighted (line 140) both mutate the world before the authoritative uniqueness check at s.canaries.Create (line 176), and a Create ErrConflict (lines 177-179) returns ErrCanaryInFlight without rolling back or
- **Status:** Closed by PR-2 (canary-batch2)

### [PM26-07-07] MEDIUM — Deleting an app mid-canary orphans the live weighted split; sweeper marks the row aborted without tearing down traffic, and manual canaries are never reaped at all

- **Area:** canary-chains · **Where:** `backend/internal/service/canary.go:322` · **Category:** data-integrity
- **Failure scenario:** DeleteApp (handler/app.go:155-162) only deletes the apps row — it never checks h.Canary or calls Abort. The canary Deployment and weighted split established by DeployWeighted keep serving user traffic in the cluster. On the sweeper's next tick, evaluate's app-gone path flips the DB row to aborted but skips the DeployWeighted(Weight:0) teardown that real Abort performs — so the -canary workload serves N% of traffic forever with no API handle left (Promote/Abort now fail in loadActive at apps.Get → 500, not cleanup). Worse, that reap path only runs for AutoPromote canaries past their window: delete an app with a manual (AutoPromote=false) progressing canary and the app_canaries row stays 'progressing' forever, rescanned by every ListProgressing sweep, with the orphan pods still live.
- **Suggested fix:** In DeleteApp (or an app service), call Canary.Abort before deleting the app (tolerating ErrNoActiveCanary). In evaluate's app-gone branch, also attempt the weighted teardown using the row's images/namespace snapshot. Extend the sweep (or the app-gone check) to cover manual progressing canaries whose app no longer exists.
- **Adversarial verification:** CONFIRMED — The core defect is CONFIRMED. DeleteApp (backend/internal/handler/app.go:155-162) calls only h.Store.Apps.Delete — no reference to h.Canary, no Abort, no DeployWeighted(Weight:0). The route (backend/internal/server/router.go:252) wires the handler directly with no other cleanup hook, and no code anywhere else tears down a canary on app delete. Since real teardown only happens inside CanaryService.
- **Status:** Closed by PR-3 (canary-batch3)

### [PM26-07-08] MEDIUM — Cache-miss stampede: concurrent requests each fan out to billed Cost Explorer (no singleflight)

- **Area:** cloud-inventory · **Where:** `/home/user/Cooker/backend/internal/cloudinventory/cloudinventory.go:143` · **Category:** concurrency
- **Failure scenario:** At TTL expiry (or first load), N concurrent GET /api/v1/cloud/inventory or /cloud/costs requests all see an expired cache, all release the mutex, and all call fetch() independently — N full fan-outs including N billed CostExplorer:GetCostAndUsage calls ($0.01 each) plus N EC2/EKS/ECR sweeps. GET routes carry no rate limiter (router.go:198-199), and the POST /refresh limiter is per-user, so several distinct users pressing refresh simultaneously each trigger a separate billed fan-out. The cache exists specifically to protect the paid API (comment at line 42-46) but does not serialize misses.
- **Suggested fix:** Add singleflight around fetch: while a fetch is in flight, later callers wait on it (e.g. golang.org/x/sync/singleflight or a per-Service in-flight channel guarded by mu) instead of launching their own.
- **Adversarial verification:** CONFIRMED — Confirmed by direct read of backend/internal/cloudinventory/cloudinventory.go. Inventory() checks the TTL cache under s.mu, but on miss/expiry unlocks at line 143 and calls s.fetch(ctx) at line 145 with no singleflight, no in-flight marker, and no double-checked re-read after reacquiring the lock — so N concurrent goroutines that all observe an expired cache each perform a full provider fan-out. T
- **Status:** Closed by PR-4 (cloud-cache-batch4)

### [PM26-07-09] MEDIUM — Slow in-flight fetch can overwrite a newer Refresh snapshot (stale data re-stamped as fresh)

- **Area:** cloud-inventory · **Where:** `/home/user/Cooker/backend/internal/cloudinventory/cloudinventory.go:147` · **Category:** concurrency
- **Failure scenario:** T0: GET /inventory misses cache and starts fetch A (Cost Explorer is slow; fetchTimeout allows up to 30s). T0+2s: user changes something in AWS and POSTs /cloud/refresh; Refresh busts the cache, runs fetch B, caches the fresh snapshot. T0+20s: fetch A finally completes and unconditionally writes its older snapshot over B with a brand-new 5-minute expiry. The UI that just confirmed a refresh now serves pre-refresh data whose FetchedAt/expiry claim it is the freshest view — the exact 'stale presented as fresh' shape. There is no generation/epoch check on the cache write.
- **Suggested fix:** Guard the write with a generation counter: record gen under mu before fetching; on completion only install the result if s.gen is unchanged (Refresh increments gen). Falls out naturally if singleflight from finding 1 is added and Refresh joins/invalidate-then-joins the flight.
- **Adversarial verification:** CONFIRMED — Confirmed by reading /home/user/Cooker/backend/internal/cloudinventory/cloudinventory.go. Inventory() does the cache-miss check and the cache write in two separate mutex sections with s.fetch(ctx) running unlocked in between (lines 137-150), and the write is unconditional — no generation/epoch counter, no singleflight, no expiry comparison. Refresh() (lines 157-163) merely nils the cache and re-en
- **Status:** Closed by PR-4 (cloud-cache-batch4)

### [PM26-07-10] MEDIUM — Markdown/@-mention injection via OIDC identity claims in feedback issue body

- **Area:** feedback-relay · **Where:** `backend/internal/handler/feedback.go:132` · **Category:** injection
- **Failure scenario:** Message, PageURL and User-Agent are properly neutralized (dynamic fence / inlineCode), but UserID and UserEmail are only whitespace-collapsed. These come from OIDC token claims (Subject, Email), and many IdPs let end users self-edit profile email/name or admins federate untrusted IdPs. A user whose email claim is e.g. "@org/security-team see [x](https://evil)" gets live markdown and a team @-mention rendered on the User bullet of every issue they file, pinging the team and injecting attacker links into the maintainers' repo. The handler comment says identity is "derived server-side" but server-side derivation does not make the claim values trusted text.
- **Suggested fix:** Wrap UserID and UserEmail with the existing inlineCode(collapseSpace(...)) helper, same as PageURL and User-Agent.
- **Adversarial verification:** CONFIRMED — Confirmed real defect. At service/feedback.go:132, UserID and UserEmail are passed only through collapseSpace(), while the sibling metadata fields PageURL (line 133) and UserAgent (line 134) are wrapped in inlineCode(). The function's own doc comment (lines 121-127) explicitly states that "collapseSpace alone would leave @-mentions, #-references and `[links](…)` live on the bullet line" — so the asy
- **Status:** Closed by PR-5 (feedback-batch5)

### [PM26-07-11] MEDIUM — Canary promote/abort bypass the governance admission gate that deploy and rollback carry

- **Area:** routes-authz · **Where:** `backend/internal/server/router.go:268` · **Category:** authz
- **Failure scenario:** With COOKER_GOVERNANCE_URL set, an operator's initial canary deploy at 10% is admitted via govDeploy on /apps/:id/deploy, but promoting the canary to 100% of production traffic via POST /apps/:id/canary/promote never consults the governance service — a rollout that governance would deny at full weight goes live anyway. The inline comment documents skipping the rate limiter and idempotency (fast control-plane op) but says nothing about governance, while /rollback — also a traffic-state change, not a new build — explicitly carries govDeploy.
- **Suggested fix:** Add the govDeploy middleware (or a canary-specific governance extractor) to /canary/promote; abort is arguably safe to leave open as it reduces exposure, but document that decision in the route comment either way.
- **Adversarial verification:** CONFIRMED — Verified in source: router.go:253/259 attach govDeploy (RequireGovernanceAllow + AppDeployExtractor) to /apps/:id/deploy and /apps/:id/rollback, with the rollback comment explicitly stating rollbacks carry "the same ... governance gates" because a traffic-state change IS a deploy. router.go:268-269 give canary/promote and canary/abort only writeRole; the inline comment explains skipping the rate l
- **Status:** Closed by PR-3 (canary-batch3)

### [PM26-07-12] LOW — Failed/canceled fetch result is cached for the full TTL as if it were a good snapshot

- **Area:** cloud-inventory · **Where:** `/home/user/Cooker/backend/internal/cloudinventory/cloudinventory.go:148` · **Category:** data-integrity
- **Failure scenario:** fetch() runs under the HTTP request's context (handler passes c.Request.Context(), cloud.go:44). If the browser navigates away or the connection drops mid-fetch, every provider returns 'context canceled'; fetchOne records that as ProviderInventory.Error and Inventory() caches the all-errors snapshot with a fresh 5-minute expiry. Every user then sees 'aws: ... context canceled' error banners (with a current FetchedAt, so it looks authoritative) for up to 5 minutes, until TTL expiry or a write-role user hits POST /refresh. Same applies to any transient throttle/timeout: errors get the identical TTL as successes.
- **Suggested fix:** Detach the fan-out from the request context (context.Background()+fetchTimeout, since the result is cached for all users anyway), and/or skip caching — or use a short negative-TTL — when every provider (or any provider) errored.
- **Adversarial verification:** CONFIRMED — The defect is real; I could not refute it. Verified against the source: `handler/cloud.go:44` passes `c.Request.Context()` into `Service.Inventory()`, and Go's http.Server cancels that context when the client disconnects mid-request. In `cloudinventory.go`, `fetch()` (line 170) derives its 30s timeout context from the caller's context, so a client disconnect propagates `context.Canceled` into ever
- **Status:** Closed by PR-4 (cloud-cache-batch4)


## Low-severity leads (not adversarially verified)

- **[canary-concurrency]** splitReplicas with pool=1 yields zero canary pods for every weight 1-99, contradicting its own 'at least one pod each side' guarantee (`backend/internal/deployer/weighted.go:33`) — With WeightedRequest.Replicas=1 and weight 50 (or 1, or 99): canary is first clamped up to 1, then clamped down to pool-1 = 0 — the canary track gets zero pods while the AppCanary row reports 'canary at 50%'. If AutoPromote is on and no prober is wir
- **[license]** GetLicense/DeleteLicense return raw internal error strings to any authenticated user (`backend/internal/handler/license.go:60`) — A transient Postgres failure (e.g. connection refused with host details, or a pq error including SQL fragments) is wrapped by the service ("license: load active: ...") and echoed verbatim in the 500 body of GET /license — an endpoint readable by any 
- **[cloud-inventory]** Per-replica in-memory cache: POST /cloud/refresh only refreshes one replica; replicaCount>1 validation does not cover it (`/home/user/Cooker/backend/internal/cloudinventory/cloudinventory.go:62`) — With replicaCount=3 behind a non-sticky load balancer (a supported topology), POST /cloud/refresh busts and refreshes only the replica that served it; the next GET /inventory lands on another replica and returns the old snapshot — the refresh button 
- **[cloud-inventory]** AWS cost total silently sums mixed currencies and mislabels earlier service lines (`/home/user/Cooker/backend/internal/cloudinventory/aws/aws.go:327`) — Cost Explorer can return different Unit values per group (accounts billed in non-USD, or consolidated billing with mixed-currency line items). summary.Currency is overwritten by whichever group is processed last, each CostServiceLine captures the cur
- **[feedback-relay]** Created issue URL (private feedback repo owner/name) returned to every authenticated user including viewers (`backend/internal/handler/feedback.go:73`) — COOKER_FEEDBACK_GITHUB_REPO is typically a private internal repo; html_url discloses its owner/name (and issue numbering rate) to any authenticated user, viewers included — the frontend never uses issueUrl (it only shows a generic toast). An untruste

## Claims raised and refuted on verification

- **[license]** Memory LicenseStore desynchronizes entitlements across replicas (`backend/internal/store/memory/license.go:25`) — refuted: The memory LicenseStore is only selected when DatabaseURL is empty (backend/internal/server/server.go:967-970), and Config.Validate() (backend/internal/config/config.go:565-568) hard-fails production boot without a real DATABASE_URL — so a production
- **[feedback-relay]** Feedback spam control breaks under multi-replica: per-replica in-memory limiter, and docs say to disable it (`backend/internal/server/router.go:357`) — refuted: The claim's core premise — that the only multi-replica option is to disable the limiter, leaving /feedback unprotected — is false at HEAD; the code has a multi-replica-safe path plus a startup guard that blocks the claimed misconfiguration.

1. Redis
- **[routes-authz]** DELETE /license is a destructive admin action without the MFA step-up all other destructive admin routes carry (`backend/internal/server/router.go:336`) — refuted: The claim's premise — that every other destructive admin route carries the MFA step-up — is false in the same router file: docker.DELETE images/containers/networks/volumes (router.go:137,141,148,153), kubernetes.DELETE (line 186), and settings.DELETE

## Checked and found sound (finder notes)

- **canary-concurrency:** Checked and sound: sweeper goroutine is joined on shutdown via cancel+drain (server.go:574-588, 2s bound); memory and postgres AppCanaryStore are in behavioral parity (ErrConflict on second progressing, ErrNotFound mapping, copy-on-read); 025_canary.down.sql cleanly reverses the up (DROP TABLE removes the partial indexes; expected data loss only) — its header comment says "Reverse 024" instead of 025, cosmetic only; the 4 MiB / 64-doc manifest caps in clientgo.go splitManifest are a documented mitigation for parser bombs.
- **canary-chains:** Checked and sound: (1) promote/abort skipping the expensive rate limiter and idempotency middleware is deliberate and documented in router.go:261-266 ("fast control-plane ops, not new builds") — canary Start itself goes through POST /apps/:id/deploy which carries writeRole + expensive + idempotency + govDeploy, so build-triggering paths are all limited; promote/abort still require writeRole. (2) Double-start is guarded twice: service pre-check plus the partial unique index uq_app_canaries_active (migration 025) mapped to 409 via store.ErrConflict — memory and postgres stores are in parity on one-active-per-app. (3) The canary deploy path reuses the F-07 stub-run-row pattern and the coordinator/fallback-goroutine both bound the worker at 30 minutes.
- **license:** Verified sound: fail-closed degradation to Free on absent/expired/store-error; Ed25519 verify-before-parse with no alg-confusion surface; one-row invariant enforced identically in memory (mutex + id normalization) and Postgres (constant-id upsert + CHECK id='active'); no handler trusts client input for entitlement (plan/features come only from verified claims, installer identity from auth context). Read-path trust of stored decoded claims without re-verifying raw_token is explicitly documented in migration 024's header. Store I/O carries the request context.
- **cloud-inventory:** Checked and sound: (a) credential material — Config secrets never appear on returned types, slog lines log only region/project (server.go:1144,1155), and provider error strings wrap SDK errors that don't embed keys; (b) SDK-call contexts — all per-call I/O flows through the 30s fetchTimeout ctx (pagination loops included); construction touches no cloud API (SDK v2 creds resolve lazily); (c) config gap for non-production is mitigated: aws.New/gcp.New fail on empty region/project and newCloudInventory fails boot (server.go:1140-1152), so dev/uat can't boot half-configured; (d) fetchOne dropping resources on cost error, and GCP's zero CostSummary, are both explicitly documented design decisions — not flagged, though note the GCP "0.00" is shape-indistinguishable from a real zero-spend account (follow-up already tracked in the package doc).
- **feedback-relay:** Sound: message body injection is well-mitigated (dynamic backtick fence sized past the longest run, feedback.go:144; titles are plain text on GitHub); PageURL/User-Agent use inlineCode; GitHub error bodies are logged, never relayed (handler 502 generic); token is a documented fine-grained single-repo PAT, never echoed; the call is context-bound (request ctx + 10s client timeout); endpoint auth-required by the api group — right call, and viewer access is deliberate per the router comment.
- **routes-authz:** Checked all new routes: cloud (GET read-level, refresh writeRole+expensive — rationale documented inline), feedback (auth-only + expensive, viewer access documented), detect-build/triage (writeRole+expensive), canary GET, tokens (inline service RBAC documented), stage approvals (inline handler RBAC documented, matching /approve precedent). No new route bypasses oidcMW — feedback, license, canary, cloud, tokens are all inside the /api/v1 group with oidcMW.Handler(); only webhooks and local signup/signin sit outside, both with documented per-IP limiters.
