# Automation boundary review — 28 September 2026

Reviewed the new automation preparation/policy, source access, connection webhook and PostgreSQL trigger admission code in the isolated backend worktree. This is bounded implementation/test evidence, not hosted-provider UAT or completion of all backend milestones.

## Corrections made

1. **Annotated tag identity:** authenticated tag deliveries resolve their typed ref through the approved connection and verify that the signed `after` identifies the resolved commit or object. The durable event retains both the peeled commit and original tag object. A different current revision cannot replace the pushed source.
2. **Retry stability:** authentication precedes durable lookup. A repeated delivery with the same authenticated body returns its saved result without consulting a tag that may have moved. Reusing an identity with a different body conflicts. Concurrent normalizers also defer to the first committed matching-body result.
3. **Webhook/reconciliation deduplication:** manual missed-change checks retain the same tag object as webhook normalization, so they find one logical intent for the same rule/configuration/ref/object/commit.
4. **Same-commit tag moves:** Dispatch compares the original object as well as the peeled commit. Re-annotating a release tag at the same commit still requires review.
5. **Queued source freshness:** worker validation rechecks the typed ref after inspecting the configuration envelope. A branch that advanced while waiting, a changed tag object, or unavailable source stops that queued deployment. Accepted source remains immutable; no new commit is substituted.
6. **Machine authority:** queued actor groups and user token authority are rejected. Handler tests confirm client-supplied actor/groups/approval hashes cannot survive preview/save, and machine authority is checked again before save.
7. **Admission identity:** the PostgreSQL admission callback requires the expected deterministic job ID and App concurrency key. A mismatched job cannot acquire an intent receipt.

The reviewed policy hashes raw selected Compose, referenced env-file and Dockerfile bytes, rather than only a redacted graph. App/environment/connection generations and current machine authorization are rechecked. Administrator and configured MFA middleware wrap source/automation control routes. This source review is not a separate end-to-end authentication-provider test.

## Verified checks

- Focused service/handler tests passed with `-race`: unchanged approved plans, queued branch advancement, same-commit tag object changes, unavailable provider, revoked machine authorization, disabled connection, injected actor groups/token hash, Dispatch supersession/review, tag reconciliation deduplication, authenticated retry stability, signature/body conflict checks, and forged preview/save authority.
- A new disposable PostgreSQL database passed the admission tests: injected pre-commit failure leaves neither runnable job nor receipt; successful admission survives fresh store handles with the same canonical plan digest; disabled rules and mismatched job identity/target scope reject atomically. The fixture and its storage were removed.
- The full service and handler regression suites passed on the same frozen review snapshot.
- Source provider evidence is separate in [source-adapters.md](source-adapters.md), including a real disposable Gitea source check. No hosted GitHub/GitLab account was used for this review.

Evidence logs:

- `/mnt/data/cooker-automation-review-tests-20260928.log`
- `/mnt/data/cooker-trigger-admission-review-20260928.log`
- `/mnt/data/cooker-automation-review-regression-20260928.log` (full service/handler suite passed)

Tests use frozen backend copies to avoid compiling files while other agents edit the shared worktree. Live webhook-to-Docker deployment, full application integration, hosted accounts and the remaining milestone exit criteria are separate acceptance gates.

## Legacy GitHub import reference review

The legacy source resolver now preserves full `refs/heads/...` and
`refs/tags/...` inputs instead of prefixing a tag with `refs/heads/`. Bare
names remain branch names. Unsupported ref namespaces fail before API access.
The legacy clone used by repository inspection fetches that exact namespace,
peels annotated tags to a commit, and checks out the resulting full SHA with a
detached HEAD. It never falls back from a missing branch to a same-named tag.
An already reviewed commit still takes precedence over a moving branch.

The full GitHub source and service packages passed with `-race` on a frozen
copy after this change. The real local Git fixture verifies same-named branch
and annotated tag selection, missing-branch rejection, default-branch
compatibility and immutable reviewed checkout. An HTTP provider fixture checks
the legacy API resolver selects the corresponding typed GitHub endpoint. This
is local Git and API contract evidence, not a hosted GitHub account test.
Evidence: `/mnt/data/cooker-typed-ref-review-20260928.log`.
