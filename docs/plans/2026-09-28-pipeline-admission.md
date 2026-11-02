# Durable pipeline admission — 28 September 2026

The queued pipeline HTTP path persists an immutable pipeline and pending-run
snapshot in the durable job before creating the queryable run view. The
snapshot includes actor identity/groups, the audit token hash, definition
version, variables, stage identities and creation time. Raw bearer tokens are
not stored. Job identity remains `run:<runId>` and the HTTP receipt remains the
accepted PipelineRun, preserving existing clients.

A queue failure creates no pending run. If the queue accepts but the process
stops before the view exists, the worker recreates it without reading a newer
definition. A failed best-effort view insert after admission still returns
accepted; reporting failure could invite duplicate submission. Concurrent
creation cannot overwrite a cancellation or a newer view. A conflicting actor
or definition version fails before execution.

Cancellation and lease recovery can recreate the same view inside their queue
transition transaction. Deleting a pipeline does not resurrect it during
recovery: its run views may be gone, but the job can retain its explicit
cancellation or attention-needed outcome. A missing required pipeline prevents
normal PostgreSQL run creation/execution through the existing foreign key.

The configured scheduler enqueuer now also captures a pending-run snapshot;
it previously supplied an ID while the worker required an existing row. The
separate scheduler `MarkFired` write still has a possible duplicate-trigger
window and is not claimed as exactly-once scheduling. HTTP retries with a new
run ID similarly represent new requests; this is not a client idempotency API.

Local tests cover admission-process failure before view creation, actor and
definition preservation, a failed view store, concurrent cancellation, and
recovery in a disposable PostgreSQL schema. Notifications occur after durable
queue completion but remain best-effort; there is no notification outbox.
Deployment history recorded before queue completion represents an observed
attempt result until the queue settles, a separate remaining consistency issue.
