# Rejected App deployment attempts

An accepted App job can be refused before deployment because its accepted registry authority is unavailable or its linked environment generation changed. Cooker records a failed business run and a **Rejected before deployment** history entry. Queue `succeeded` means the worker acknowledged the handler outcome; it does not turn that rejected deployment into a successful application release.

The entry contains accepted App/run/pipeline identity, status and outcome time. Its image reference and digest stay empty. No source checkout, build, artifact publication or runtime is implied. The existing history links lead to the run's fixed explanation.

## Environment-generation decision

The worker compares the currently observed environment version with the version captured in the immutable accepted job. Only a successful metadata read with a different version produces `environment_generation` rejection. Missing, inaccessible, cancelled or failed metadata reads return a fixed uncertain outcome and require reconciliation; provider/database error text never becomes a rejection reason.

An environment rejection requires a captured, nonempty, non-credential-only environment ID and an explicitly present nonnegative generation. Explicit version zero remains supported. A missing or null generation cannot acquire that decision's authority.

## Persistence, retry and recovery

The same atomic recorder handles `registry_authority` and `environment_generation`. It checks the live worker claim, immutable accepted payload digest and actor/lineage, and requires an unstarted run without stage, artifact, environment or log effects. PostgreSQL locks job → App → run → history and commits both run and history together. A write failure, contradictory marker or stale claim cannot create a determinate success/refusal receipt.

History identity is deterministic per accepted run. Repeated recording preserves the first exact reason and timestamp. Registry and environment reasons cannot rewrite each other. Recovery derives only the two fixed reason summaries from the durable failed run, then validates the complete typed rejection marker. Arbitrary failed runs, empty images, partial markers and malformed accepted payloads are insufficient.

A lost queue acknowledgement can leave the job needing attention while the business rejection remains known. Suspending a job before a rejection is recorded follows the existing uncertain-attempt lifecycle; a later stale worker must preserve that prior outcome.

The latest attempt remains visible in history. Existing runtime inspection continues to use recorded runtime attempts, excluding the rejected kind. A rejected attempt does not create a deployed service, health verdict, log authority or rollback artifact.

## Verification boundary

See the [local repair handoff](/mnt/data/cooker-roadmap-20261002/environment-refusal-history-20261004/README.md) for exact source overlays, baseline reproduction, tests, retained failures and pending runtime/compatibility gates. The prior [eight-case campaign](/mnt/data/cooker-roadmap-20261002/dockerfile-environment-bindings/runtime-20261003/RUNTIME-HANDOFF-v5.md) observed the original missing-history behavior; those immutable results are not retroactively changed.

No migration is needed. New reason-aware marker recovery is not automatically supported by the previously prepared registry-only fallback. Combined official-server/worker, compatible fallback, owner UI and full SEC-01/R1 release acceptance remain separate gates.
