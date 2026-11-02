# Durable scheduled occurrences

REL-02 scheduler admission uses the existing PostgreSQL pipeline queue. Migration
034 adds a schedule version and an occurrence journal. A due slot is identified
by the schedule ID and the stored `next_run_at`, independent of tick time or
retry. Its native run ID is a stable SHA-256 projection of that pair.

The scheduled payload pins the schedule version, pipeline ID, cron, timezone,
due slot, admission time and next slot. It also contains the existing immutable
native pipeline/run snapshot. In the queue's `OnEnqueue` transaction, Cooker
locks and compares the schedule, validates the next cron slot, inserts the
occurrence journal, and advances the schedule with a version check. The job,
journal and advancement commit together. Failed admission creates no runnable
job; a lost reply after commit cannot create another occurrence after restart.
The leader advisory lock reduces competing scans; correctness does not depend
on its lifetime.

Concurrent schedule edits use compare-and-set. API edits may include the
displayed `version`; stale versions return 409. Older request bodies remain
supported, while the handler's read is still fenced against concurrent firing.
Successful edits/fires increment the version. Disabling/editing a schedule
affects future admission and cannot rewrite the pipeline snapshot of a job
already accepted. Cancel that job through the existing explicit queue action
when needed; running external work still requires reconciliation.

Occurrence records survive terminal-job retention and schedule deletion. Even
when the job was cancelled or removed, reusing the same old slot cannot silently
create another run. This release does not add an automatic journal purge; an
operator must not delete admission evidence while it can still be replayed.

Migration 034 preserves legacy schedules with version 1. Old readers using the
previous column projection remain readable. Application rollback and schema
rollback differ: do not run an old scheduler against this database if doing so
would resume its non-atomic enqueue/advance behavior. Disable scheduled firing
before such a rollback, retain the journal, and reconcile any pending jobs.

The focused race/integration campaign is:

```sh
COOKER_INTEGRATION_DATABASE_URL='<disposable PostgreSQL URL>' \
  go test -race -tags=integration -count=1 -parallel=1 ./internal/scheduler
```

Each test uses an owned schema. Cases cover failed admission after insert and
advance, actual database update errors, context cancellation, parallel
admission, edits/disable, restart after a lost acknowledgement, cancelled-job
retention, immutable pipeline snapshots, manual admission and the actual
migration. Evidence: `/mnt/data/cooker-roadmap-20261002/scheduler/`.

This closes a scheduler admission boundary, not all REL-02. Terminal
notifications remain best effort through the current dispatcher; durable
notification retry/status is separate work. These checks do not prove hosted
Git, provider rollouts or the complete roadmap.
