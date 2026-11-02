# Backend automation implementation record

User authorized implementation of the backend plan on 28 September 2026. Worktree: Cooker-backend; branch: feature/backend-automation-20260928; base: 949d4a9. Accepted frontend design is preserved.

## Priority change — 30 September 2026

The owner moved **Vercel to the first provider integration**, ahead of domains
and AWS/GCP expansion, with account connection and existing deployment sync as
the first outcome. [Vercel A](../backend/vercel-sync.md) now adds encrypted
personal/team access-token connections, explicit project selection, automatic
read-only polling, recent history and Settings/Cloud controls. Hosted Vercel
account UAT is pending. OAuth onboarding and native deployment controls are
separate later slices; the full roadmap is still unfinished.

## Milestone tracking

| Milestone | Implemented in this candidate | Acceptance status |
| --- | --- | --- |
| M0: truthful baseline | Configured source-build/service-log capabilities; DAG dangling-edge/cycle fix; real tiny Redis and n8n queue path; source-build conflict resolved with BuildKit/Crane; documented capability matrix | Local baseline established. Hosted source, cluster/cloud and human acceptance remain separate |
| M1: dependable deployment | Durable App plans/jobs, exact Git source, fenced run writes, atomic job/run lifecycle, lease recovery, cancellation, explicit Needs attention, isolated OCI handoff, service-scoped configuration, durable masked stage logs, immutable service images, exact-container Docker logs, recorded-service runtime reads, periodic local health | Substantial implementation and real local acceptance. Full M1 exit gate remains open; see limits below |
| M2: GitHub automation | Encrypted connections; branch/tag exact/glob rules; signed raw-body verification; durable inbox/deduplication; atomic intent/job receipt; server-derived policy preview; machine identity; stale-ref and configuration checks; review inbox; missed-ref check; connected controls | Contract, PostgreSQL and browser fixture checks pass. Hosted GitHub delivery/private checkout and combined production-build acceptance pending |
| M3: GitLab/Gitea | Shared secure HTTPS transport, private CA/host policy, stable repository IDs, typed refs, private checkout, explicit GitLab signing/legacy modes | Real private Gitea source and signed webhook-to-Docker acceptance pass. Real GitLab and hosted GitHub campaigns remain pending |
| M4: domains/tunnels | Existing base-domain routing retained | New domain ownership/TLS/tunnel management not started |
| M5: AWS/GCP operations | Existing configured targets retained | Cloud logs, complete health/URL/recovery and account acceptance not started |
| Vercel A: account and existing deployment sync — now first provider priority | Encrypted connections, project allowlist, durable snapshots, replica leases, polling/backoff and UI controls | Contract, PostgreSQL and browser fixture checks; hosted account acceptance pending |
| Vercel B/C: OAuth and native deployment controls | No native mutation adapter or OAuth client added | Later slices; no deploy/promotion/rollback claim |

The full M0–M6 plan is **not finished**. The first implementation backlog B01–B08
has a substantial local candidate, with release gates and remaining engineering
explicit below. No milestone is marked production-accepted from mocks or code
presence. The accepted frontend design remains preserved.

## Delivered behavior

- A manual deployment can enter the existing PostgreSQL queue. Its source SHA,
  App configuration, environment generation and actor are captured before
  acknowledgment. Generated run views can be reconstructed after a crash.
- Pipeline runs now admit their immutable planned run through the queue first,
  closing the run-row/queue gap. A real database concurrency test covers the
  serialization key and stale-worker write fence.
- Recovery, cancellation and operator resolution commit the job, corresponding
  run and App deployment history together. Normal execution saves its result
  before marking the queue handler complete. Uncertain outcomes hold the App
  lock as Needs attention until an
  administrator records a verified resolution; arbitrary work is not retried.
- BuildKit exports private local OCI layouts; Crane publishes and verifies the
  same image/index digest. Configured production TCP connections require mTLS.
  Existing Compose images resolve to immutable digests before queue admission;
  the accepted source/configuration binds each original image reference to its pin.
- Linked configuration keys have explicit per-service allowlists. Normal
  configuration remains readable while secret values are encrypted and masked
  before stage capture, durable replay and live broadcast. Edited environment
  generations cannot silently change an accepted deployment.
- GitHub/GitLab/Gitea connections use repository allowlists and encrypted
  credentials. Git checkout is exact-commit, isolated from host Git helpers and
  credentials. Full branch/tag refs remain distinct even with the same name.
- Connection webhooks authenticate before deduplication. Delivery metadata,
  per-rule decisions and intents are durable. Queue admission and its receipt
  commit atomically. Annotated tag objects and peeled commits are both checked.
- Review hashes and automation identity come from the server. Configuration,
  source connection and authorization are checked again before execution.
- Settings, App import, service permissions, rule preview/save, recent intents,
  review actions and deployment links are connected to their APIs.
- Legacy per-App webhook auto-deploy now returns an explicit migration conflict
  for eligible events. It cannot bypass the new authority/review boundary.
- Docker logs verify the executor-stamped run label, then follow the exact
  container ID using its recorded deployment configuration. Missing/rotated
  database generations and unverifiable external-secret history disable replay.
  Kubernetes logs explicitly remain unavailable until immutable pod sessions exist.
- Promotion approval no longer claims that it deployed an environment.

See [the capability matrix](../backend/README.md) and
[operator setup/migration/recovery](../backend/operator-guide.md).

## Evidence actually executed

Tests ran locally on 28 September 2026. The host has no Go executable; Go checks
use the official cached `golang:1.25` image (Go 1.25.14). Frozen copies and source
hash manifests identify tested snapshots while the two approved agents worked
in the shared worktree. Earlier successful snapshots do not automatically
validate later edits.

| Check | Observed result | Evidence |
| --- | --- | --- |
| Combined backend race suite after recovery fix | 1,604 test cases/subtests passed in 56 packages; 7 database-gated tests skipped in this invocation; go vet passed | `/mnt/data/cooker-backend-final-r3-20260928/race.jsonl`, `vet.log` |
| Final database/race campaign | 765 test cases/subtests passed across jobqueue/store/logstore/service/handler; the 7 database-gated checks ran here. Six opt-in fixture/helper tests skipped in this command; Compose, BuildKit, forced worker death, restore and Gitea flow were verified separately | `/mnt/data/cooker-backend-final-r3-20260928/integration.jsonl` |
| Final runtime race review | Service, handler and server suites passed against the source used for the real deployment rerun | `/mnt/data/cooker-runtime-n8n-acceptance-20260928/race.log` |
| PostgreSQL job claim/fencing/recovery | Passed real database tests | `backend/internal/jobqueue/*integration_test.go`, `store/postgres/run_claim_integration_test.go` |
| Durable logs and trigger inbox | Passed race and isolated PostgreSQL checks | `/mnt/data/cooker-environment-scope-check/final.log`, `/mnt/data/cooker-trigger-check/integration.log` |
| Atomic trigger admission | Passed injected rollback, receipt reconstruction, rule/identity guards | `/mnt/data/cooker-trigger-admission-review-20260928.log` |
| Automation through PostgreSQL | Passed durable intent/queue receipt/restart/stale revision/config and revocation tests | `/mnt/data/cooker-automation-integration-20260928.log` |
| Source refs and checkout | Passed full GitHub/service race suites with real local Git branch/tag collisions and annotated tags | `/mnt/data/cooker-typed-ref-review-20260928.log` |
| Full local Git automation | Real private Gitea sent a signed HTTPS webhook; preview/save → durable intent/job → healthy Docker Redis passed on final source in 19.07 seconds; review/signature/retry/log assertions passed; fixtures removed | `/mnt/data/cooker-gitea-flow-final-20260928/acceptance.json` |
| Private Gitea source | Passed on disposable Gitea 1.26.4; fixture removed | `/mnt/data/cooker-gitea-source-acceptance-20260928.log` |
| BuildKit/Crane publication | Passed real single/multi-platform OCI publication and mTLS reruns | `/mnt/data/cooker-build-handoff-final.log`, `/mnt/data/cooker-build-handoff-mtls.log` |
| Redis and n8n queue-mode deployment | Passed real App queue → Compose → readiness → service logs; exact source despite branch advance | `/mnt/data/cooker-app-acceptance-evidence-20260928/acceptance.json` |
| Final pinned-image / scoped n8n / runtime-log rerun | Redis + n8n passed in 38.48 seconds, including immutable images, exact-container logs, four-container credential isolation and stopped-worker → queued job → resumed success; fixtures removed | `/mnt/data/cooker-runtime-n8n-acceptance-20260928/acceptance.json` |
| Image admission | Mutable-tag retargeting cannot change the accepted image; service/server race checks passed | `/mnt/data/cooker-image-pins-tests-20260928.log` |
| Code quality | gofmt, go vet and frontend lint passed. All 27 introduced Go lint findings resolved; 31 pre-existing baseline findings remain unchanged, so global golangci-lint still exits nonzero | `/mnt/data/cooker-candidate-lint-comparison-20260928.json`, `/mnt/data/cooker-frontend-lint-final-20260928.log` |
| Frontend unit tests | 181 passed in 23 files | `/mnt/data/cooker-frontend-tests-final-20260928.log` |
| Chromium frontend acceptance | 39 passed, including 7 new automation-control checks; production build passed as prerequisite | `/mnt/data/cooker-frontend-e2e-20260928.log` |
| Connected Settings smoke check | Real local API/PostgreSQL connection save survived browser reload; test-only credentials stayed hidden | Disposable server `127.0.0.1:14642`, database `cooker_ui_acceptance_20260928`; this is not provider access proof |
| Packaged application | Combined image built, starts as UID65532, migrates disposable PostgreSQL and serves frontend + connections JSON API; Git/Compose/kubectl present | `/mnt/data/cooker-backend-final-r2-20260928/image-build.log`, `package-smoke.json` |
| Forced worker death and recovery | SIGKILL after real Redis readiness passed in 13.16 seconds; no repeated deployment, retained App lock, stale-worker fencing, atomic resolution/rollback and reconstructed history; 3 history regression cases passed | `/mnt/data/cooker-worker-recovery-acceptance-20260928/attempt-2/acceptance.json` |
| Backup/restore drill | Actual pg_dump/pg_restore passed in 3.00 seconds: 31 migrations, encrypted probes, same-key success, wrong-key rejection without ciphertext changes, durable job/run/history and log cursors | `/mnt/data/cooker-restore-drill-20260928/acceptance.json` |

The final r3 backend manifest matches the worktree. The packaged image used
the r2 snapshot with the recovery fix; later changes only made error prefixes
lowercase and cleanup discards explicit, and retained the artifact privacy
assertion with a narrow lint annotation. Those final changes passed r3 tests
and the final Gitea flow. The packaged image is a local check artifact.

The browser suite uses mock API fixtures except for the separate connected
Settings smoke check. The n8n acceptance uses an injected local Git source;
it does not prove hosted GitHub delivery or a production control-plane setup.

## Remaining first-release engineering and acceptance

1. Run one hosted disposable GitHub repository from signed public delivery
   through connected App APIs, the durable queue, isolated source build,
   digest artifact, actual runtime and logs. The requested repository URL and
   configured connection are not yet supplied. No live credentials were read
   from unrelated projects or Git credential files.
2. Extend the passed post-readiness worker-death test to other uncertain
   provider outcomes/cancellation boundaries, Kubernetes rollout/rotation, and older-schema
   upgrade restore. The same-version restore drill passed; existing database
   tests cover durable boundaries but
   do not substitute for all of these live cases.
3. External secret stores still resolve their current value; immutable secret
   version history and an online encryption-key rotation tool are unfinished.
   Runtime log replay rejects unversioned external secrets; explicit service
   permissions can prove that a deployment selected only plain configuration.
   Registry connections remain instance-scoped; per-App credential authority
   requires a further policy contract. Service-scope mode prevents linked
   runtime credentials from becoming build inputs; arbitrary build secrets
   are not added in this slice.
4. Actual promotion dispatch and durable rollback/canary orchestration are
   unfinished. Legacy manual execution remains process-bound when the queue is
   disabled. Scheduler enqueue/mark-fired has a separate crash window; terminal
   notifications are best effort.
5. Authored-pipeline Git triggers, optional periodic ref enumeration, a complete
   delivery-decision browser (including ignored events), source-token rotation
   controls, runtime replica/time-range/search/download controls, and measured
   queue/acknowledgment performance remain follow-up implementation. APIs and
   store capabilities do not imply every UI control exists.
6. Real GitLab acceptance, domain/DNS/ACME/tunnel lifecycle, and AWS/GCP operations plus later Vercel controls
   releases follow the original gates. They need concrete provider resources
   and scoped acceptance inputs; none were deployed or certified here.

## Integration state

All checks are stopped and their disposable resources were removed; the
loopback documentation preview remains available. All changes are local and
uncommitted in `Cooker-backend`. No push, merge, tag,
release or production deployment was performed. The original Cooker `develop`
checkout remains separate. The design/architecture preview ports are not proof
that this backend candidate has been integrated into the released application.


## Vercel A local validation — 30 September 2026

The final frozen backend passed **1,667 cases/subtests across 57 packages** with
race detection. Eight live-DB tests are skipped in that no-database invocation;
the affected Vercel/server/audit campaign passed **162 cases/subtests across
three packages** with actual PostgreSQL, including the Vercel persistence gate.
The older unrelated license/promotion DB gates retain their separately dated
28 September evidence and were not rerun for this isolated provider slice.
`go vet` passed; lint matches exactly the original 31 findings (zero added or
removed). Binary compilation and authenticated localhost readiness passed.

Frontend checks passed 194 unit tests and 47 Chromium regression checks, plus
separate scoped Vercel mobile/accessibility checks. The live preview revealed
and fixed a local-auth build configuration loop; a separate local-only browser
campaign passed five session/no-token/expiry/login/logout checks. Provider
replies remain fixtures. No hosted Vercel account acceptance or external
provider mutation is claimed.

Evidence: `/mnt/data/cooker-vercel-validation-20260930/results.json`, final
backend source manifest/logs, and the frontend evidence folders. Source remains
uncommitted in this candidate. The original Cooker checkout is still clean at
949d4a9. The local preview is available at `http://127.0.0.1:4644/signin`, with
its own persistent PostgreSQL volume and private generated encryption config.
