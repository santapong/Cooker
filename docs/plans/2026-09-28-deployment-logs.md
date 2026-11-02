# Deployment log foundation — 28 September 2026

This implements the log protection and durable stage-history portion of B05.
It does not establish the complete M1 acceptance gate or live cloud-log support.

## Operator behavior

With a configured main PostgreSQL database, stage replay history now defaults
to PostgreSQL. Migration `029_stage_logs` creates separate stream and entry
tables. An explicit `COOKER_LOGSTORE_BACKEND=memory` retains the disposable,
single-process option. An unknown backend or unavailable durable schema fails
startup instead of silently switching to volatile storage.

| Setting | Default | Meaning |
| --- | --- | --- |
| `COOKER_LOGSTORE_BACKEND` | PostgreSQL when the main database is configured, otherwise memory | Replay backend |
| `COOKER_LOGSTORE_MAX_BYTES` | 1 MiB | Retained line bytes per stage; oversize lines are shortened |
| `COOKER_LOGSTORE_MAX_STREAMS` | 256 | Retained streams; PostgreSQL enforces this at boot and each minute |
| `COOKER_LOGSTORE_RETENTION` | `168h` | PostgreSQL history lifetime |

Both stores additionally keep at most 4,096 entries per stage, including empty
lines. Stage capture in the run record retains its existing 1 MiB cap. Long
unterminated output is split into bounded 64 KiB frames. PostgreSQL serializes
cursor allocation under a stream-row lock, so stage retries and worker restarts
continue an existing retained stream instead of reusing its cursor.

Storage errors produce a visible history-unavailable signal and an operator
warning containing only run/stage IDs. A short timeout and five-second retry
cooldown prevent a failed database from blocking every output line. The final
bounded stage capture remains available if the run record can be saved. After
storage recovers, the next retained entry explains that earlier history was
unavailable. Expired or trimmed replay sends a gap signal before the retained
tail. The UI displays incomplete-history text without a reconnect loop.

## Secret boundary

`ResolveDetailed` separates ordinary environment values from the resolved
secret values. Both remain ephemeral and are excluded from JSON serialization.
The attempt carries its secret set in a context. `SecretLogWriter` masks known
values before the output reaches the final capture, replay store or WebSocket.
It handles secret values split across writes and newlines, and serializes
concurrent stdout/stderr writers. Adapters register credentials resolved later
with `RegisterLogSecrets` before using them. Public stage outputs and artifact
references containing a known secret are scrubbed and rejected before dependent
stages consume them. Provider error details from secret resolution are not
returned to the client.

Docker service logs capture one recorded deployment, verify the executor's
`io.cooker.run-id` label, pin the exact container ID, and resolve that same
deployment's environment generation before streaming. A rollout that reuses a
container name cannot redirect the stream or its secret mask. An edited App
cannot redirect the lookup to a different environment or external binding.
Changed or missing database configuration, retained job input, or legacy
containers without the run label fail closed. Runtime status probes likewise
use one deployment snapshot and require its run label before reporting ready.
Missing adapters, subprocess failures and scanner/write failures return errors.

Kubernetes generated pod templates receive the same run label, and readiness
requires the matching template plus current generation. Kubernetes service log
streaming is disabled until an adapter can bind the log request to an immutable
pod identity; a mutable pod name or selector plus a separate UID check is not an
atomic guarantee. The capability endpoint explicitly reports this limitation.

## Limits that remain explicit

- Exact known-value masking cannot recognize arbitrary encoded, transformed,
  hashed, application-generated or previously rotated secrets. Applications
  must avoid writing secrets to output. This is defense in depth.
- Explicit Compose service/runtime binding is described in
  [the environment scope contract](2026-09-28-environment-scope.md).
  Legacy manual plans preserve inherited behavior. External secret version
  pinning remains separate work. Runtime replay with an external manager is
  allowed only for unlinked environments or explicit service permissions whose
  keys are all in the recorded plain configuration. An empty external secret
  list cannot establish that an older inherited deployment received no secrets.
- Stage replay is durable. Runtime service streams still come from the runtime
  adapter and do not become an archival log aggregation service.
- AWS/GCP service log retrieval, replica selectors and cloud account acceptance
  are separate provider work. Unsupported adapters remain unsupported.
- Stream-count retention is periodic, not a global instantaneous database disk
  quota. PostgreSQL disk/index/vacuum capacity still needs operator monitoring.
- Retention deliberately deletes old streams. A stream recreated after expiry
  has a new cursor lifetime; history-gap/reset handling is required in clients.
- No throughput or public latency objective is claimed from unit tests.

## Verification

The implementation is exercised with pure tests for every two-chunk split of
a multiline secret, concurrent writes, late credentials, capture/replay/live
parity, missing runtime tools and subprocess errors. WebSocket tests cover
retention gaps and database read failures. A disposable PostgreSQL integration
test uses a unique schema and verifies 32 concurrent cursor allocations,
history across store reconstruction, stale cursor rejection, byte bounds,
stream-count pruning and retention cleanup. Race-enabled results and exact
commands are recorded in the implementation checkpoint rather than treating
this procedure as a permanent passing result.
