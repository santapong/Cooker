# Existing-project update review — source checkpoint

Source checkpoint: 25 October 2026; scoped evidence update: 26 October, Asia/Bangkok. This extends VRC-04 and prepares VRC-03. All 48 approved product tasks remain open; full completion is `not_proven`. The 25 October confirmed [VRC-04 ClickUp stage](https://app.clickup.com/t/z8xxqwcd6t) was **Blocked**. Fresh26October readback verified VRC-04 still Blocked for its remaining full criteria, with original criteria and protected fields retained. [Scoped board proof](/mnt/data/cooker-ecosystem-20261026/CLICKUP-VERIFICATION.json). The Neon fixture result does not close this task.

Source now defines preparation and inspection of an immutable, ten-minute review for the **same existing Cooker landing project**. It binds the exact enabled adoption and connection versions, account/project identity, current embedded landing artifact, observed production deployment and confirmed alias set. The server supplies those facts; the browser sends only `adoptionId`, `adoptionVersion` and `connectionVersion`.

This slice performs provider reads and a local review-receipt insert only. Public JSON always derives `submissionAvailable:false`. There is no provider mutation, project creation, submission endpoint, deployment job, promotion, domain change or editable imported DAG. A prepared review does not establish Vercel write permission, current application health or public accessibility.

## Stored review and source wiring

Migration 041 defines a separate `vercel_landing_update_reviews` table. Review IDs use `vlur_` and never enter the legacy create-only publication table. Conditional storage checks the current connection first and then the enabled adoption/version/identity/selection before insertion; provider I/O stays outside database locks. The down migration refuses nonempty review history. These are implemented source contracts, not executed migration or race evidence.

Review preparation checks prior successful publication provenance, safe static project predicates, exact reported current production and its deployment identity. It observes aliases for that exact deployment and repeats the project read to detect observed target drift. A nil alias set remains unknown; an explicitly observed empty set is valid. The final read and local receipt commit are not atomic with Vercel, so any later submission must revalidate the baseline.

Authenticated reads:

- `GET /api/v1/integrations/vercel/landing/update-reviews` returns `{reviews, historyLimit:100}`.
- `GET /api/v1/integrations/vercel/landing/update-reviews/:id` returns `{review}`.

Admin-only preparation:

- `POST /api/v1/admin/integrations/vercel/landing/update-reviews` returns `{review}` and derives its actor from the authenticated session.

The Cloud **Landing update reviews** panel reuses loaded adoptions, connections and artifact metadata. It reads review history on mount/session/role change or manual refresh, without API polling. Selection is bound to adoption and connection versions. Unknown, stale and expired records remain visibly historical; an uncertain preparation reply requires a successful history read before another preparation request. Readers have no authority-edit controls. Destination links are safe HTTPS origins labelled as observed at review time, with no access or health claim. There are no deployment controls, even if malformed receipt data advertises submission availability.

## Evidence obtained and limits

| Evidence | Observed result | What it does not establish |
| --- | --- | --- |
| Independent finite backend/HTTP source review | One nil-versus-empty fixture defect was corrected and rechecked in source; no remaining actionable P1/P2 in that reviewed slice. | Executed tests, frontend interaction or runtime permission/persistence. |
| [Go formatter/parse receipt](/mnt/data/cooker-ecosystem-20261025/continuation/FORMAT.json) | Ten affected Go files passed; effective 0.25 CPU / 128 MiB RAM / no swap / 32 PIDs, 120-second deadline; owned container absent afterward. | Compilation, unit/race checks, SQL or server execution. Peak memory was not recorded. |
| [TypeScript syntax receipt](/mnt/data/cooker-ecosystem-20261025/continuation/TYPESCRIPT-SYNTAX.json) | TypeScript 5.5.4 `createSourceFile` parsed twelve TS/TSX files with zero syntax diagnostics; effective 0.25 CPU / 256 MiB RAM / no swap / 32 PIDs, 60-second deadline; owned container absent afterward. | Semantic types, units, lint, build or browser behavior. Peak memory was not recorded. |
| [Affected source checkpoint](/mnt/data/cooker-ecosystem-20261025/continuation/SOURCE-MANIFEST.json) | Pins 66 affected files and records 55 Go top-level test declarations across M1, adoption, Neon and update-review sources. | Test execution, a full-candidate release freeze or a deployable build. |

The review slice authors eight provider/store test declarations, including two opt-in PostgreSQL cases, two HTTP boundary declarations and 22 frontend declarations with parameterized cases. **All remain NOT RUN.** Semantic TypeScript, units/race, lint/build, migrations/fresh-pool persistence, real registered HTTP/admin behavior, browser roles/themes/keyboard/layouts and live provider/account acceptance remain open. The source-review and formatting receipts do not close VRC-03/04 or any original gate.

**26 October scoped supersession:** the separate inactive Neon metadata package passed nine top-level tests and twelve subtests, with zero failures/skips, on ARM64 Go 1.25.14 with CGO disabled and no race run. Its six source/build-input pins were unchanged. [Outcome](/mnt/data/cooker-ecosystem-20261026/OUTCOME.json) and [final cleanup](/mnt/data/cooker-ecosystem-20261026/FINAL-CLEANUP.json) retain the exact scope, sampled 312.94 MiB RSS / 60.05°C / 435.09 MiB disk, 95.7-second lane use, and verified removal of all three owned staging roots. This was a soft-guard run without hard RAM/swap containment, isolated from live providers and SQL. It does not execute or qualify this Vercel update-review slice.

The [resource runbook](RESOURCE-TRACKING.md) keeps the chat automation paused. The 25 October queued/control-exit attempt and the first two 26 October executor failures remain preserved; neither is counted as a test pass. The successful Neon-only run does not close M1/Vercel, SQL, frontend/browser, race, live Neon onboarding/bindings or full-release gates. No hosted project, owner server/database, token, domain or existing service was changed.

## What follows

First execute the [affected verification batch](VERIFICATION.md) when a runner qualifies. Then implement the distinct existing-project update operation/queue with current artifact, production, version and authority checks; retain uncertain external IDs instead of blindly repeating a provider POST. Actual same-project publication and recovery acceptance remain separate. Neon bindings, hosted Git/OAuth, lifecycle, domains/tunnels and broader providers remain grouped in the [current plan](PLAN.md#current-remaining-work-grouped-by-outcome) and the existing ClickUp tasks.

Source and review handoffs:

- [Backend implementation](/mnt/data/cooker-ecosystem-20261025/continuation/update-review-backend.md)
- [Frontend and authored fixtures](/mnt/data/cooker-ecosystem-20261025/continuation/update-review-frontend.md)
- [Independent backend/HTTP review and narrow correction](/mnt/data/cooker-ecosystem-20261025/continuation/update-review-review.md)
