# Git trigger inbox foundation — 28 September 2026

This implements M2's durable decision/intent boundary. It is separate from
provider authentication, source checkout, the approved deployment envelope,
queue admission and live GitHub acceptance.

## Contract

`TriggerService.AcceptVerified` accepts only metadata normalized by an already
authenticated provider adapter. Its source identity includes provider,
connection ID, canonical host and immutable repository ID. Display repository
paths do not substitute for that identity. Stored data excludes request bodies,
authorization headers and credentials. Delivery IDs are scoped by provider and
connection; reuse with a different authenticated event or payload hash fails.

All matching rules receive a decision. A rule selects an App/environment,
source/config generations, a branch or tag, include/exclude patterns, an actor
and an approval envelope. Exact matching compares the entire short ref name.
Globs use Go's `path.Match` syntax: `*`, `?` and character classes. `*` stays
within one slash-separated segment; `**` is rejected. Exclusions win. Neither
host nor repository identity is silently case-folded by this layer.

The inbox, decisions and candidate intents commit in one transaction. A rule
edited during that transaction causes a conflict. Logical identity incorporates
the rule snapshot, target App/environment and available configuration/envelope
versions, full ref, commit and original ref object. Concurrent deliveries for
the same logical intent reuse its ID rather than creating another attempt.

Intent run IDs match `AppDeployQueue.EnqueueIntent`: UUID SHA-1 namespace OID
over `cooker-intent:` plus the server-issued intent ID. An outbox drain can
therefore retry queue admission after a crash without inventing a second run.
The returned admission receipt is recorded using a version compare-and-swap;
queued status requires a receipt for that exact run.

`pending` means **awaiting validation/admission**, not deployed or queued.
Rules without a verified actor and approved envelope, or rules configured for
review, enter `awaiting_review`. The consumer must revalidate connection,
target/config generations, actor authority, source revision and the current
inspection envelope before enqueueing. This store alone never authorizes a
deployment.

Tag observations retain both the original object and peeled commit when
available. A changed observation requires review. A signed nonzero prior SHA
that differs from the signed new object also requires review even on the first
delivery observed by this installation. Deletion events only record an ignored
decision; they never deploy or remove workloads.

## Storage and retention

Migration `030_trigger_inbox` adds rule, delivery, intent and tag-observation
tables. Memory and PostgreSQL implementations share the contract. PostgreSQL
uses unique indexes for delivery and logical intent identities, row locks for
rule generations and tag observations, and compare-and-swap intent transitions.

`ListIntents` returns recent intents of all statuses for the review inbox,
optionally filtered by App and capped at 100 records. Pending admission scans
remain a separate oldest-first API.

`PruneTriggers` compacts completed audit detail. It keeps authenticated event
identities, logical keys and admission receipts as dedupe records. It preserves
pending/review work. This intentionally does not promise a fixed total row
count: erasing all old dedupe evidence would allow an old delivery to create a
new deployment. Installation-wide archival/export policy remains an operator
capacity decision.

## Verification and remaining acceptance

Pure tests cover exact/glob/exclusion rules, wrong-host/ref-kind boundaries,
32 concurrent copies of a delivery, separate deliveries for the same logical
intent, three App matches, changed configuration, moved/deleted tags, default
review, receipt identity, stale transitions and post-compaction dedupe.
PostgreSQL tests use disposable schemas and cover concurrent uniqueness,
restart reads, conflicting payload identity, logical dedupe with a different
proposed ID, rule-change rollback, moved tags and retained dedupe/receipts.

These tests do not certify GitHub account setup, signed webhook delivery over
a public URL, annotated tag peeling, branch-head reconciliation, changed Compose
privilege detection or a real automated deployment. Those are consumer/provider
acceptance gates and must be reported separately.
