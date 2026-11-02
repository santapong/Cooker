# Notification acceptance and delivery

Notifications are independent of deployment success. With PostgreSQL, Cooker
accepts safe event metadata and matching target versions in one transaction,
then sends from a separate outbox. A send failure never changes a run or job.
Without PostgreSQL, dispatch is process-local, bounded, and **best-effort**.

## Acceptance boundary and identity

An event is accepted only after its outbox transaction commits. Each terminal
run has one deterministic event identity; repeated callbacks and native job
reconciliation do not send it again. Canary state-change identities also pin
the canary ID and transition type. One delivery identity combines event ID and
target ID; target version is an immutable pin, not a new delivery identity.

Targets are selected when acceptance commits, using their current enabled
state and event filters. Configuration, access credentials, receiver URLs,
remote bodies, raw executor errors, operator-entered names, and namespaces are
never copied into event or delivery records. Notifications use safe native
IDs, status, observation time, and a fixed failure summary. Detailed errors
remain in Cooker's existing authorized run views.

Target versions come from a PostgreSQL sequence and a write trigger. Legacy
writers that do not know the version field still invalidate pending deliveries.
Deletion/recreation cannot reuse an old version. Sending resolves the current
encrypted configuration only when its version, kind, enabled state, and filter
still match the accepted pin. Edits cancel unsent old deliveries; they do not
reroute old messages to a replacement endpoint.

**An already in-flight request cannot be recalled by editing or disabling its
target.** No target-row lock is held across external I/O. The worker checks the
version immediately before sending; the old request may finish afterward.

## Recovery and its limits

Native terminal queue jobs are a retained repair source. Periodic reconciliation
captures success, failure, cancellation, and verified operator resolution even
if the process stopped between terminal persistence and the old completion
callback. The job retention transaction captures a receipt before deletion.
Capture errors retain the terminal job and leave its deployment outcome intact.
Receipts survive job deletion, so retention and restart cannot manufacture a
fresh event. Pre-migration terminal history is suppressed using a persisted
policy cutover time; installing this feature does not notify old deployments.

Malformed, missing, or nonterminal source views are **not** accepted receipts.
Safe repair records back off capture by 30 seconds, then 2 minutes, then at
most 10 minutes between attempts. Deferred sources do not starve newer jobs.
The delivery list reports all unresolved source jobs and the deferred repair
count, even when they have no delivery row yet.

Inline deployment and canary callbacks use the outbox after successful
acceptance. **If that initial transaction never commits, these paths currently
have no retained notification replay source.** Cooker logs a fixed acceptance
warning, and deployment success stays intact. A warning is not proof of durable
notification acceptance. Native terminal job repair is stronger than this
inline/canary boundary. Memory mode loses delivery state on restart.

## Delivery policy

| State | Meaning |
|---|---|
| `pending` | Accepted event waiting for a sender |
| `sending` | One fenced worker owns the attempt |
| `retry_wait` | Definite retryable rejection or target-read failure; waiting for backoff |
| `delivered` | Receiver accepted the request; this does not prove a human read it |
| `failed` | Permanent rejection, invalid configuration, missing adapter, or three attempts exhausted |
| `cancelled` | Accepted target was removed, edited, disabled, or filtered before sending |
| `needs_attention` | Worker loss, timeout, transport ambiguity, or an unconfirmed send result |

Each delivery allows at most **three** attempts. HTTP 429 and 5xx rejection
retries wait 30 seconds, then 2 minutes. A receiver may have performed work before
returning an error, so these retries can duplicate messages. Other HTTP
rejections and invalid configuration are terminal failures. Target-read errors
can retry within the same limit without an external send.

Transport failure and timeouts are conservative: the receiver may have accepted
the request. Expired sending leases and result-persistence failures also become
`needs_attention`; they are never automatically resent. Late workers cannot
overwrite a revoked claim. SMTP has a context/deadline-bound connection; all
adapters have a bounded send duration.

Generic webhooks receive `X-Cooker-Event-ID` and `Idempotency-Key`. Receivers may
use these to deduplicate, but Cooker does not assume they do. HTTP body replay
is disabled so Go's transport does not quietly repeat a webhook POST because
the idempotency header exists. This is **not an exactly-once external delivery
guarantee**. The current API exposes inspection only; it provides no automatic
resend or replacement-target adoption for terminal/uncertain deliveries.

## Safe status API

Existing authentication, admin role, and configured MFA protect both reads:

```text
GET /api/v1/admin/notification-deliveries
GET /api/v1/admin/notification-deliveries/:id
```

Lists support `state`, `targetId`, `runId`, `limit`, and `offset`. Filtering precedes
pagination; `total` counts the filtered deliveries. `pendingSourceJobs` and
`pendingSourceRepairJobs` describe the provider-independent native repair
backlog. `mode` states the durability limits. Responses contain fixed result
codes and HTTP status numbers, never target config, credentials, endpoint
bodies, lease ownership, or raw errors. PostgreSQL outage returns a fixed 503;
memory mode reports that durable history is unavailable.

## Local verification and rollback

Notification acceptance uses only an owned disposable PostgreSQL database and
local fake HTTP/SMTP receivers. Never use an owner database for destructive
tests or send test notifications to real configured endpoints. Evidence for
this implementation is recorded separately under
`/mnt/data/cooker-roadmap-20261002/notifications`.

Apply migration 035 through the normal migration runner. Keep the event,
delivery, receipt, repair, and policy tables for restart recovery. An older
binary can still read/update the target table, but its direct-send behavior
bypasses this policy; stop the notification sender before rollback and inspect
pending/uncertain deliveries. Do not drop the journal to retry messages. No
public release, credential change, actual receiver delivery, or whole-roadmap
completion is established by these fixture checks.
