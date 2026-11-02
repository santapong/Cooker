# Interrupted App worker acceptance

Verified 28 September 2026 at 20:57 ICT. A real worker process was killed after
Cooker deployed a healthy Redis container and before it saved the deployment
result. This verifies one local uncertain-outcome recovery boundary; it does not
prove complete M1 or every failure/restart scenario.

## Executed flow

1. The fixture started its own PostgreSQL instance, applied Cooker migrations,
   and created a tiny Compose source in a local Git repository. The normal
   durable App queue prepared the exact commit and mandatory image evidence
   before saving the deployment job.
2. A separate worker process used Cooker's real queue, App deployer, executor
   and Docker Compose adapter. After the adapter's real mutation/readiness
   call returned, a test wrapper paused before returning that result to the
   executor. The test independently observed `redis-cli ping` returning `PONG`
   and the container carrying the accepted run label.
3. The parent sent **SIGKILL** to that worker. A fresh store connection and
   restarted worker pool recovered its expired lease as **Needs attention**.
   The attempt count stayed at one. A second accepted job for the same App
   remained pending; the uncertain first deployment retained the App lock.
4. The revoked worker identity could neither complete its queue job nor write
   a successful run. The runtime container was not recreated.
5. The test recorded the checked external outcome through the same queue
   resolution operation used by the administrator handler. An injected error
   after the domain writes forced the entire transaction to roll back: job,
   run and history all stayed uncertain. Retrying without that injected error
   saved the verified success consistently, released the App lock, and rejected
   a competing stale administrator resolution. The newly unblocked second job
   was claimed only to prove lock release, then cancelled without dispatch.

The initial run exposed a real defect: lease recovery updated an existing
history row, but no row existed when the process died before the deployer's
terminal history write. `SyncJobRun` now creates that missing row inside the
same transaction. It carries the accepted App/run/pipeline identity and uncertain
status; it does not invent an image digest or infer successful rollout. Existing
artifact evidence remains intact. Deleted Apps are not recreated. Dedicated
PostgreSQL regressions cover those three cases and preservation of the accepted
actor and source/configuration payload.

## Evidence and cleanup

Successful evidence directory:
`/mnt/data/cooker-worker-recovery-acceptance-20260928/attempt-2/`.

- `acceptance.json`: asserted outcomes, exact source/run/image identities and
  cleanup results.
- `run.log`: full live acceptance passed in **13.16 seconds**.
- `history-regression.log`: missing-history, preserved-artifact and deleted-App
  cases passed in **2.23 seconds** using isolated schemas in the fixture DB.
- `worker.log`, `compile.log`, `source-sha256.json`: worker evidence and frozen
  source identity. The binary was compiled with Go 1.25.14 and CGO disabled.

`attempt-1/` retains the reproduced missing-history failure. Its resources were
also removed. Both runs removed their exact Compose project containers, volumes
and networks, and their separate PostgreSQL container and anonymous volume.
The killed worker's temporary checkout/runtime files lived in a parent-owned
fixture directory that was also removed. Existing services, private credentials,
production resources and the Pi were untouched.

## Repeat procedure

The integration test owns its disposable infrastructure. It needs local Docker
CLI/Compose, Git, and cached `redis:7-alpine` and `postgres:16-alpine` images; it
reads and uses their immutable repository digests. It binds only the fixture
PostgreSQL port to loopback. The Redis workload publishes no host port.

Run `TestAppQueueLiveInterruptedWorkerRecovery` from `backend/internal/service`
with the `integration` build tag and `COOKER_RECOVERY_ACCEPTANCE=true`.
`COOKER_RECOVERY_EVIDENCE_DIR` optionally retains the evidence outside temporary
storage. On a host without Go, compile a Linux service test binary from a frozen
backend copy with `CGO_ENABLED=0`, then execute it on the host; the build container
does not need a Docker socket. The test starts its worker child from that same
binary using `TestAppQueueRecoveryWorker`.

Optionally compile the PostgreSQL test package too and pass its binary path as
`COOKER_RECOVERY_HISTORY_TEST_BINARY`. The parent supplies only its generated
fixture database connection, and the regression test uses isolated temporary
schemas. Never point this test at an existing database or reuse existing
containers. Cleanup only exact names recorded for a forcibly stopped fixture;
never use a global prune operation.

## Limits

The pause after real Compose readiness is deliberate failure injection. The
workload and database are real; the source is a local Git fixture. There is no
hosted Git provider, public webhook, source build, private registry or cloud
provider in this test. It does not test failure during an unfinished Docker
operation, loss of the database, host/power loss, automatic provider reconciliation,
or recovery of arbitrary application migrations.

The administrator resolution store operation was executed directly with recorded
fixture evidence. HTTP administrator/MFA authentication, operator UI interaction
and authorization-provider behavior are separate acceptance gates. Recording a
verified outcome does not rerun the deployment or roll back application data.
