# Local deployment acceptance — PostgreSQL queue and Docker Compose

Checked 28 September 2026. Both cases below passed through Cooker's execution services. This closes a local execution evidence gap; it does not establish live GitHub, authentication/UI, cloud-provider or production acceptance.

## What actually ran

| Case | Result |
|---|---|
| One Redis service | A PostgreSQL-backed App job deployed a real digest-pinned Redis container through the Compose adapter. The container became healthy and answered `PONG`. |
| n8n queue mode | The same App queue deployed PostgreSQL, Redis, n8n main and one worker. The saved DAG contained four deployment stages and five dependencies. All four services became healthy. |
| Immutable source | Both fixtures advanced their local Git branch after queue acceptance to a deliberately invalid Compose definition. The workers still checked out and deployed the accepted full commit. |
| Durable execution evidence | Jobs and runs reached success. Deployment logs were read through a fresh PostgreSQL connection/log-store instance, and service logs were read through Cooker's runtime service. |
| Credential boundary | n8n test credentials were generated for this fixture and stored using Cooker's encrypted database secret manager. Assertions found neither secret in the persisted job payload, run, or redacted graph. |
| n8n worker proof | Stopping only the test worker left a webhook request pending and a job waiting in Redis. Restarting the worker completed the request; PostgreSQL recorded execution 1 as success and worker logs showed completion. |
| Cleanup | All two App projects' containers, volumes and networks were removed. The separate acceptance PostgreSQL container and its anonymous volume were also removed. Existing services and the Pi were untouched. |

The Git clone is a real local Git checkout injected at the source boundary. Source commit resolution is fixture-controlled. This does not claim that GitHub inspection, App save/deploy HTTP handlers, provider webhooks, credentials for private source repositories or the accepted frontend were exercised. Runtime readiness is deployment evidence; it is separate from periodic App health reconciliation.

The n8n fixture used the cached **2.28.6** image, verified by its executable version and pinned repository digest. It has one worker with concurrency five, not five worker containers. It exposes only main on a temporary loopback port. External calls, Code nodes, binary data, high availability, upgrades and load testing are outside this smoke test.

## Repeatable test

Source: `backend/internal/service/app_queue_live_integration_test.go`. Supporting fixtures are `testdata/n8n-queue.compose.yaml` and `testdata/n8n_queue_smoke.py` in the same package. The source test is opt-in behind the `integration` build tag.

1. Start a **new disposable PostgreSQL instance** bound to loopback with a database whose name begins `cooker_acceptance`. Do not reuse an existing database: Cooker applies its embedded migrations and the fixture writes Apps, environments, encrypted test credentials, jobs, runs and logs.
2. Provide its connection string as `COOKER_APP_ACCEPTANCE_DATABASE_URL`. The test refuses other database-name prefixes or non-loopback hosts. The host must have Docker CLI/Compose v2, Git and Python 3, with cached `redis:7-alpine`, `postgres:16-alpine` and `n8nio/n8n:latest` images. The test resolves cached tags to digests before creating the source fixture; these cache tags are not a newest-version recommendation.
3. Run `go test -tags integration ./internal/service -run '^TestAppQueueLiveCompose$' -v -count=1` from `backend/`. Set `COOKER_APP_ACCEPTANCE_N8N=true` to include the n8n case. The Redis case runs without that flag. For a host without Go, compile a Linux test binary from a frozen source copy with `CGO_ENABLED=0`, then run it on the host from `backend/internal/service`; do not give the build container the host Docker socket.
4. The fixture owns unique `cooker-e2e-*` Compose projects and cleans them by their exact project labels. The operator who started the separate PostgreSQL fixture must remove that instance and its owned volume afterward. If the process is forcibly killed, reconcile only the project names from that run's output; never use a global prune command.

Both real cases were compiled from a frozen backend copy using Go 1.25.14. The saved source hash manifest distinguishes this tested snapshot from concurrent development changes. Recompile and rerun when the relevant queue, source pinning, Compose execution or log paths change.

## Evidence retained for this run

Local evidence directory: `/mnt/data/cooker-app-acceptance-evidence-20260928/`.

- `acceptance.json`: scoped pass results, run/commit identities, image digests and cleanup checks derived from the integration assertions/output.
- `cooker-app-acceptance-redis.log`: passed one-service execution, including the accepted source SHA and run ID.
- `cooker-app-acceptance-n8n.log`: passed four-service execution and the worker queue proof.
- `source-sha256.json`: hashes of the frozen source used to compile the live acceptance binary.
- BuildKit/Crane verification logs copied alongside these records; its separate [handoff contract](buildkit-handoff.md) describes that scope.

The fixture database was removed. These are test assertions and captured execution logs, not a retained database backup or a deployment that remains running.

## Explicit service permissions acceptance — 28 September 2026, 20:15–20:16 ICT

A fresh run additionally passed `TestAppQueueLiveCompose/n8n_scoped`, enabled
with `COOKER_APP_ACCEPTANCE_SCOPED_N8N=true`. The earlier legacy n8n case remains
separately selectable. Both modes reuse the same worker-stop/queued-job/resume
proof and exact-source-commit checks.

The scoped case expands the fixture's shared YAML environment alias into
service-local environment maps and sets `environmentScope: service`. Image and
published-port interpolation values move to `BuildPlan.Variables`. This follows
the [explicit environment contract](../plans/2026-09-28-environment-scope.md)
rather than allowing globally interpolated secret extension templates.

Actual container inspection verified the following without printing values:

| Container | Linked secret keys present |
| --- | --- |
| PostgreSQL | `POSTGRES_PASSWORD` |
| Redis | None |
| n8n main | `POSTGRES_PASSWORD`, `N8N_ENCRYPTION_KEY` |
| n8n worker | `POSTGRES_PASSWORD`, `N8N_ENCRYPTION_KEY` |

An extra unbound canary secret was absent from all four containers, including
aliased environment values. Image interpolation inputs were also absent from
the runtime environment. Job/run completion, healthy services, fresh-connection
durable log replay, service logs, secret-free persisted execution views and
successful worker execution all passed. The n8n case took 45.99 seconds; the
entire Redis plus scoped n8n test took 49.61 seconds.

Evidence: `/mnt/data/cooker-scoped-n8n-acceptance-20260928/acceptance.json`,
`run.log`, `compile.log` and `source-sha256.json`. The frozen test binary used
Go 1.25.14, Docker 29.8.0, Compose 2.39.4-desktop.1 and cached digest-pinned n8n
2.28.6. Both disposable projects, their networks/volumes, and the dedicated
acceptance PostgreSQL container/volume were verified removed. The source,
production, cloud, upgrade, authentication and frontend limits above remain.

## Immutable images and deployment-bound logs — 28 September 2026, 20:43–20:44 ICT

A fresh frozen-source run passed Redis and scoped n8n with
`RequireImagePins=true` and the admission image resolver enabled. Cached image
digests were validated before queue acceptance and used by the worker after the
source branch moved. The executor's run labels were present on the actual
containers. Runtime status required the matching run, and service log sessions
verified that label, pinned the exact Docker container ID, and loaded the same
recorded deployment's database environment generation before redaction.

All earlier service-permission, healthy-service, durable stage replay,
secret-free persisted view, and stopped-worker/queued-job/resumed-worker checks
passed. The complete test took 38.48 seconds (Redis 3.46; scoped n8n 34.72).
The service, handler and server race suites also passed on this frozen source.
Pure fixtures separately verify replacement during secret resolution,
unrecorded services, legacy or mismatched run labels, changed environment
versions, and rejection of unversioned external-secret replay.

Evidence: `/mnt/data/cooker-runtime-n8n-acceptance-20260928/acceptance.json`,
`run.log`, `race.log`, `compile.log` and `source-sha256.json`. Both projects and
their networks/volumes were verified absent after cleanup; the dedicated test
PostgreSQL container and its volume were removed. This is local Docker
acceptance. Kubernetes log sessions remain explicitly unavailable until an
immutable pod-session adapter exists; the earlier cloud/provider/upgrade gates
remain open.
