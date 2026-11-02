# M1, adoption and update-review verification handoff

**Current verification — 27 October 2026:** secure Neon connection, discovery, resource, HTTP and persistence source is prepared. The current default package passed **38 top-level tests plus 88 subtests**, with zero failures/skips; its wrapper failed afterward. SQL16 and HTTP6 remain **NOT RUN**: finite campaigns stopped on temporary-container lifecycle and disk-counter freshness guards, respectively. All owned work/staging was independently cleaned and the shared lane released. The existing Neon ClickUp task is freshly verified **Blocked** with original protected fields retained. See [verification](VERIFICATION.md), [resource tracking](RESOURCE-TRACKING.md) and [backend/frontend ownership](BACKEND-FRONTEND-HANDOFF.md). This is a runner-acceptance blocker despite Pi RAM headroom; waiting for RAM alone will not resolve it. All full product/live/release criteria remain open; resource automation stays PAUSED.

Initial handoff: 25 October 2026; scoped evidence updated 26 October, Asia/Bangkok. Candidate: `/mnt/data/company/apps/Cooker-backend`, branch `feature/backend-automation-20260928`, base `949d4a964abec603c9b0b82f6f8d8b7f9f23d73e`, with preserved pending changes. Proposed checks below remain unexecuted except for the separately recorded Neon fixture package.

The latest [update-review source checkpoint](M3-UPDATE-REVIEW-CHECKPOINT.md) adds preparation/read history only, with no deployment submission or job path. All 48 product tasks remain open and full completion is `not_proven`. VRC-04's 25 October confirmed remote stage was Blocked. Fresh26October readback verified the existing Neon andVRC-04 tasks Blocked for remaining full criteria, with protected fields retained; [scoped proof](/mnt/data/cooker-ecosystem-20261026/CLICKUP-VERIFICATION.json). No other task was freshly audited in this run. The chat resource automation remains paused.

## 26 October — executed Neon fixture scope

The [recorded outcome](/mnt/data/cooker-ecosystem-20261026/OUTCOME.json) and retained [attempt-3 test log](/mnt/data/cooker-ecosystem-20261026/attempt-3/evidence/test.log) establish **nine top-level tests plus twelve subtests passed, zero failures and zero skips**, package exit 0. The exact command was:

```sh
go test -json -count=1 -p=1 -parallel=1 -timeout=240s -mod=readonly ./internal/provider/neon
```

This ran on Linux/ARM64 Go 1.25.14 with `CGO_ENABLED=0`; **race coverage did not run**. The six pinned inputs—`go.mod`, `go.sum` and four Neon Go files—were unchanged. Synthetic metadata fixtures were isolated from live providers and SQL. This supplies a compile/unit result for that package, not Neon onboarding, credential versions, persistence/catalog wiring, live resources/bindings, application health or the M1/Vercel/frontend release gates.

Under the owner-approved soft guard, the three owned units had an aggregate one-CPU quota and 160-PID budget, with a 1 GiB sampled-RSS stop; hard RAM and swap caps were unavailable. Observed sampled summed RSS peaked at **312.94 MiB**, temperature at **60.05°C**, staged disk at **435.09 MiB**, and the shared lane was held **95.7 seconds**. Sampled RSS includes shared-page double counting and is not a kernel peak. Swap counters stayed unchanged; no guard stop occurred.

Two earlier 26 October executor attempts remain failed/stopped history: the socket-isolation gate rejected attempt 1, and stale disk-sample protection stopped attempt 2. Neither is counted as a test pass. The earlier 25 October queued/terminated attempt also remains historical. [Final cleanup](/mnt/data/cooker-ecosystem-20261026/FINAL-CLEANUP.json) verifies all owned units absent or stopped, all three 26 October staging roots removed, the lane released and post-run service/readiness checks retained. No watcher was reactivated.

## Evidence already obtained

- Coordinator and three independent reviewers inspected the provider, catalog and UI source. One P2 public-projection gap was fixed and rechecked in source.
- One coherent Go formatting/parse phase passed for 13 affected files. Its isolated formatter had effective CPU quota `25000/100000`, RAM `134217728`, swap `0`, PIDs `32`, no network and a 120-second deadline. The container finished and was removed. This does not prove compilation, tests or race safety.
- Plan links, all 45 original package IDs, 20 gates and 12 invariants are checked separately in [STATIC-CHECKS.json](/mnt/data/cooker-ecosystem-20261025/verification/STATIC-CHECKS.json). No original package/gate completion was promoted.
- [SOURCE-MANIFEST.json](/mnt/data/cooker-ecosystem-20261025/verification/SOURCE-MANIFEST.json) pins affected files and build inputs. It is a local checkpoint, not a whole-candidate release freeze. Earlier review hashes precede mechanical formatting; the final manifest records the formatted files and the coordinator's public-boundary fix.
- The later continuation [FORMAT.json](/mnt/data/cooker-ecosystem-20261025/continuation/FORMAT.json) records ten update-review/integration Go files passing formatting/parse with effective 0.25 CPU, 128 MiB RAM, no swap, 32 PIDs and a 120-second deadline. [TYPESCRIPT-SYNTAX.json](/mnt/data/cooker-ecosystem-20261025/continuation/TYPESCRIPT-SYNTAX.json) records twelve TS/TSX files parsed by TypeScript 5.5.4 with zero syntax diagnostics, effective 0.25 CPU, 256 MiB RAM, no swap, 32 PIDs and a 60-second deadline. Both owned containers were absent afterward; peak RAM was not recorded. Neither receipt proves compilation, semantic types, units, lint or build.
- The continuation [SOURCE-MANIFEST.json](/mnt/data/cooker-ecosystem-20261025/continuation/SOURCE-MANIFEST.json) is the newer 66-file affected checkpoint across M1, adoption, inactive Neon and update-review sources, with 55 Go top-level test declarations. Earlier exact-scope evidence remains historical; this is still not a whole-release identity or an executed test count.

## Admission and proposed executor

The workstation failed the RAM reserve requirement; earlier CPU samples also showed saturation. A later sample improved, but available RAM remained below the required reserve. The host Go binary is absent, but a fresh lookup confirmed the cached Linux/amd64 Go image `sha256:9bae33b05e4e159803d2ad714fecbda886969dce7ca369216dece578cad664c1`. This supersedes the first preflight's failed image lookup. No image pull is needed or performed for that image.

The historical 25 October Pi check at13:59 ICT found11GiB available RAM and63.7C, with other project builds running, no memory controller and no Go executable on PATH. It did not qualify a hard-RAM-bounded offload. The owner later explicitly approved the separate nine-test Neon soft-watchdog profile. That day's staged attempt was blocked behind Aegis and temperature68.85C; local control ended143 with unknown cause. Root cancelled and verified the matching remote waiter, all three units absent and staging removed. No toolchain, compiler or test started in that attempt. [Historical attempt and cleanup](/mnt/data/cooker-ecosystem-20261025/continuation/pi-test/OUTCOME.json). The 26 October result above supersedes only its Neon-fixture execution gap; it does not qualify the larger batch or supply hard memory containment.

Use one qualified x86 runner for the next batch. Before admission recheck CPU/RAM/swap/pressure, disk/inodes, actual cache/dependencies and other active jobs. Retain max(4 GiB, 25% host RAM) outside the workload; keep one heavy phase active. Proposed whole-workload ceiling: 1 CPU, 4 GiB hard RAM, no swap, 256 PIDs, one compiler/test worker, 10 minutes per phase / 30 minutes total, 2 GiB new disk including dependencies/caches and 250 MiB retained output. These are proposed limits, not measured requirements. Do not increase them silently after failure.

The runtime must contain compiler/test/browser children, not merely the Docker client. Verify effective cgroup values before execution, sample usage/headroom every five seconds, enforce the wall/output/disk boundaries, and stop only named task-owned work on cap exhaustion or reserve breach. Never stop another project's jobs to obtain capacity. Cache mounts must be explicitly reviewed and writable only if needed; no credentials, owner environment, real data, Docker control socket or provider network access belongs in fixture tests. Unknown dependency growth requires a separate bounded preparation phase.

## One coherent affected batch

After rechecking the source manifest and runner admission, run the following logical checks serially inside that bounded executor. Commands are package entry points, not a substitute for executor limits. No check below has run in this handoff.

From `backend`:

```sh
GOTOOLCHAIN=local GOMAXPROCS=1 go test -p=1 -race -count=1 -timeout=120s ./internal/provider/vercel ./internal/catalog
```

This includes 20 newly authored top-level ecosystem tests (13 provider, 7 catalog), plus the affected packages' existing regressions. Synthetic HTTP/memory fixtures need no real provider or database credentials. Do not interpret skipped PostgreSQL cases as persistence acceptance. If the exact pinned toolchain/cache cannot satisfy the module graph offline, record that failure before proposing a bounded preparation phase.

The coherent M3-A continuation adds 13 provider/read/store fixture declarations, including opt-in PostgreSQL cases, and three HTTP boundary declarations. The first command also discovers the new provider tests; run the handler boundary cases in the same serial backend phase:

```sh
GOTOOLCHAIN=local GOMAXPROCS=1 go test -p=1 -race -count=1 -timeout=120s -run '^(TestLandingAdoptionHTTP|TestLandingUpdateReviewHTTP)' ./internal/handler
```

These HTTP fixtures exercise handler authentication/role/parser boundaries; actual registered-router and successful actor persistence acceptance remain separate. The update-review continuation adds eight provider/store declarations, including two opt-in PostgreSQL cases, and two HTTP declarations. Its nil-alias fixture correction must execute alongside the authoritative-empty case; source recheck alone does not prove either result. For SQL tests supply only a newly created synthetic test database in the isolated executor, never inherited owner environment or production DSNs. Migrations 040 and 041, competing active bindings, token/selection/version fences, fresh-pool reads and the absence of legacy publications/jobs for review IDs must actually execute before persistence acceptance.

From `frontend`, with reviewed existing lockfile-matching dependencies:

```sh
node node_modules/vitest/vitest.mjs run src/components/catalog/IntegrationAccess.test.ts src/utils/vercelLandingAdoptions.test.ts src/api/vercelLandingAdoptions.test.ts src/api/vercelLandingAdoptionsTransport.test.ts src/utils/vercelLandingUpdateReviews.test.ts src/api/vercelLandingUpdateReviews.test.ts src/components/cloud/VercelLandingUpdateReviews.test.ts --maxWorkers=1 --minWorkers=1 --no-file-parallelism
node node_modules/typescript/bin/tsc --noEmit
node node_modules/eslint/bin/eslint.js \
  src/types/integrations.ts src/types/vercel.ts src/types/catalog.ts \
  src/components/catalog/IntegrationAccess.tsx src/components/catalog/IntegrationAccess.test.ts \
  src/pages/SyncedAppPage.tsx src/pages/SyncedPipelinePage.tsx \
  src/components/cloud/VercelDeployments.tsx src/components/settings/VercelConnections.tsx \
  src/types/vercelLandingAdoption.ts src/api/client.ts src/api/vercelLandingAdoptions.ts \
  src/utils/vercelLandingAdoptions.ts src/utils/vercelLandingAdoptions.test.ts \
  src/api/vercelLandingAdoptions.test.ts src/api/vercelLandingAdoptionsTransport.test.ts \
  src/components/cloud/VercelLandingAdoptions.tsx src/components/cloud/VercelLandingPublish.tsx \
  src/types/vercelLandingUpdateReview.ts src/api/vercelLandingUpdateReviews.ts \
  src/utils/vercelLandingUpdateReviews.ts src/components/cloud/VercelLandingUpdateReviews.tsx \
  src/test/fixtures/vercelLandingUpdateReviews.ts src/utils/vercelLandingUpdateReviews.test.ts \
  src/api/vercelLandingUpdateReviews.test.ts src/components/cloud/VercelLandingUpdateReviews.test.ts
```

The new component test declares 19 cases. Build once in a reviewed source-only staging area using `npm run build`, with owner `.env` files excluded. This avoids loading local environment values or replacing the previous preview/build. Node/image and dependency pins must be recorded before execution; do not install or pull automatically to make this proposal run.

M3-A adds 24 frontend test declarations across candidate/confirmation guards, route DTO construction and actual shared-client transport with synthetic fetch/auth fixtures. Add its nine changed frontend files to scoped lint. A lightweight parser passed 18 affected TypeScript/TSX files with TypeScript 5.5.4; this is syntax-only evidence, not the `tsc`, Vitest or build checks above. Twelve M3-A Go files passed the bounded formatter/parse phase. Receipts are under `/mnt/data/cooker-ecosystem-20261025/resume/`.

The update-review continuation authors 22 more frontend declarations across helper, API and SSR receipt files, including parameterized cases. They cover exact/current version selection, expiry, stale/unknown source and scope, safe observed destinations, empty/unavailable aliases, read/admin routes and the absence of deployment controls. They are **NOT RUN**; the later twelve-file syntax receipt above supplies only its declared parse scope.

Rerun only checks affected by a changed input, diagnosed failure or unresolved risk. No per-edit full-suite loop. A failed cap/timeout is a failed or stopped check, not a pass.

## Acceptance after local checks

1. Use a separate synthetic PostgreSQL database to verify absent old JSON and populated new JSON through persistence and a new pool/restart. Exercise retained observations, changed selection, cancellation, pause/disconnect and stale leases. Existing source/unit coverage alone does not establish that execution.
2. With a qualified browser budget, check authenticated navigation across synced Apps/Pipelines/Cloud/Settings, viewer/admin boundaries, narrow layouts, keyboard disclosures and both accepted themes. No deployment controls or executable resource records should appear from installation data.
3. A bounded read using the existing authorized Cooker connection must confirm exact selected account/team, actual permissions and reported installations. Compare allowed project access only. Neon resource identity, binding and SQL health require separate evidence. Do not use a different connector identity as proof of Cooker's connection.

For M3-A also verify viewer history versus admin configuration, version-bound confirmation invalidation, uncertain-save refresh, revocation after disconnect and preserved exact-project receipts. In a scoped live read, omitted project settings must remain ineligible/unknown. This stage creates no deployment; M3-B submission and current-production/alias acceptance are separate unfinished work.

For update reviews, verify authenticated list/detail and admin-only preparation against the registered router, session actor persistence, exact server artifact/baseline/alias clocks, nil versus empty metadata, revoked/changed adoption and connection fences, cancellation and fresh-pool storage. Confirm a `vlur_` review cannot reach the old publisher or create a job. In the UI, changed versions must clear selection; stale/expired/source-unknown receipts remain historical; an uncertain reply requires a successful read before another preparation request. Check both themes, narrow layout and keyboard access, with no deployment control for any role. No provider deployment should be created to demonstrate this review feature.

Record exact inputs/commands, pass/fail/skip counts, actual resource limits/peaks, stop events, retained outputs and owned cleanup. Recheck baseline host/service health. Reconcile the canonical ClickUp board and local [TASKS.json](TASKS.json) evidence mirror only against evidence actually obtained and verified remote readback. Local checks, current-account acceptance and release are separate states; M1 and the full roadmap remain unfinished here.


## 27 October — Neon connection backend and broader executor boundary

The backend now contains encrypted scoped connections, discovery/select/manual sync, pause/replacement/local disconnect, safe database/compute metadata, admin/MFA routes, migration042 and six-family recovery guards. [Backend/frontend handoff](BACKEND-FRONTEND-HANDOFF.md) keeps identity work in the separate owner session and preserves the palette. The proposed [next binding contract](M2-BINDING-CONTRACT.md) is not implemented or accepted. No frontend assets or owner services were edited by this phase.

The earlier frozen [34-test outcome](/mnt/data/cooker-ecosystem-20261026/neon-connections/UNIT-OUTCOME.json) passed34top-level+83subtests. Current discovery additions require38defaulttests; those,3NeonSQL and13keyrotation cases are **NOT RUN** in the broader trials. [Attempt1](/mnt/data/cooker-ecosystem-20261026/neon-connections/postgres-run/attempt-1/OUTCOME.json) failed during toolchain heartbeat checking; a read-order bug was fixed without claiming the unlogged actual age. [Attempt2](/mnt/data/cooker-ecosystem-20261026/neon-connections/postgres-run/attempt-2/OUTCOME.json) completed the pinned toolchain but failed before ungating Go version. Its original cleanup failed; a separate exact cleanup-only recovery verified all named resources/temp paths absent and released the shared lane. Both failures remain. HTTP/router, SQL, live account, race, bindings and release gates remain open.

The Pi now exposes a memory controller. Actual controller192MiB andwatch64MiB caps plusCPU/PID/zero-swap were read back; this does not qualify the still-unrunGo/PG containers. ControllerMemoryPeak192MiB is not anOOM verdict. The failedCLI diagnostic/pids.events were lost, so TasksMax16 contention remains a hypothesis. Next is one **qualification-only** concurrent attach/inspect/teardown campaign with total256PIDs redistributed64/32/32/128 and unchangedCPU/RAM/disk/time/heartbeat budgets, before any fullbatch. The existing Neon ClickUp task is verifiedBlocked, protectedcriteria/owners/dates/estimates/dependencies unchanged. Automation staysPAUSED; no credentials/providerresources/productionDB/key/domain, deployment/merge/push changed.


## Final foreground checkpoint — 27 October 2026

The current default Neon package remains a scoped **38 top-level + 88 subtest pass** (no failures/skips, ARM64 Go1.25.14, CGO0; no race). SQL acceptance remains NOT RUN after the SQL-only campaign exhausted its one changed retry during version-container teardown. Both native HTTP trials stopped during dependency preparation on five-second disk-counter freshness: 6.197376s then 5.239758s. The revised native DU scan itself completed in 1.833921s, but combined sample age still exceeded the limit; no particular scheduling cause or method benefit is established. No HTTP test phase began. Ten pure accounting/ownership/child-lifetime checks passed; these are runner checks, not product acceptance.

Failed outcomes and independent cleanup are retained at:

- [SQL-only outcome](/mnt/data/cooker-ecosystem-20261026/neon-connections/postgres-run/full-after-qualification/sql-only/attempt-2/OUTCOME.json).
- [First native HTTP outcome](/mnt/data/cooker-ecosystem-20261026/neon-connections/http-run/native/attempt-1/OUTCOME.json).
- [Revised native HTTP outcome](/mnt/data/cooker-ecosystem-20261026/neon-connections/http-run/native-du/attempt-1/OUTCOME.json).
- [Verified ClickUp update](/mnt/data/cooker-ecosystem-20261026/neon-connections/CLICKUP-CONTINUATION-RESULT.json).

All named units/processes/cgroups, SQL test containers/volume/network, caches and staging were independently stopped/removed; the same lane was released and core services/n8n remained healthy. No worker, waiter or automation remains active for these campaigns. Pi RAM/CPU headroom is available; integration execution is blocked by the recorded runner lifecycle/freshness boundaries, not an unchanged RAM shortage. The existing Neon task is now **Blocked**, with full criteria, owners, dates, estimates and dependencies retained; no other task or all48-state audit is implied.

Next eligibility requires a reviewed execution method with stable worker lifetimes and dependency/accounting/cancellation behavior qualified for the actual graph, within explicit effective limits. Resume only outstanding SQL16/HTTP6; do not repeat unchanged default38 or restart the exhausted campaigns unchanged. M1/M3 acceptance, production read attestation and explicit Neon bindings, current provider permissions, frontend-owned acceptance and release remain separate. No provider, owner DB/key, production service, port4644, deployment or Git publication changed. The other owner session controls frontend identity; palette remains fixed.
