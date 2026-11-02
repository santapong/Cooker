# Existing landing adoption — source checkpoint

25 October 2026, Asia/Bangkok. This implements part of VRC-04 and prepares VRC-03. It does not complete either full package.

An administrator can record a fixed production/manual-Cooker/embedded-landing scope for an exact selected Vercel project. The server verifies the connection identity, checks a prior successful publication for the same account/project, fetches safe project-setting predicates and saves a versioned local adoption. No provider resource, project, deployment or update job is created by this step.

The [official project schema](https://vercel.com/docs/rest-api/projects/find-a-project-by-id-or-name) lists build/install/root, Git link and services as optional fields; services is an array. Omission is not established as proof of no Git/services or empty commands. The adapter therefore keeps missing settings unknown. It only retains explicit predicates, safe IDs/name and observation/reason fields. It discards command/root strings and all environment, metadata and arbitrary URL fields. Current account eligibility has not been checked.

Migration 040 creates `vercel_landing_adoptions`, separate from read-only catalog snapshots. Conditional writes check the current connection version, identity, enabled state and selected project in the same transaction as the binding update. An active unique index prevents overlapping connections from claiming the same exact account/project/environment. Explicit versioned revocation remains available after disconnect and retains history; the down migration refuses to erase a nonempty history table.

Authenticated reads:

- `GET /api/v1/integrations/vercel/landing/adoptions`
- `GET /api/v1/integrations/vercel/landing/adoptions/:id`

Admin-only configuration:

- `POST /api/v1/admin/integrations/vercel/landing/adoptions`
- `PUT /api/v1/admin/integrations/vercel/landing/adoptions/:id`

The HTTP boundary derives the actor from the session, checks the admin role, bounds the body to 4096 bytes and rejects unknown fields, non-object bodies and trailing JSON. Imported Apps and flows retain their read-only behavior. The Cloud panel uses existing publication/connection data, explains the fixed scope, retains revoked/changed-connection history and reconciles uncertain saves through reads.

Backend and HTTP source review found no remaining actionable P1/P2 in its finite scope. Fixtures are authored; tests/builds, actual migrations, restart/rollback, authenticated browser behavior and provider permissions/content have **not run**. The [verification handoff](VERIFICATION.md) remains subject to live runner admission. No owner server/database or hosted project was changed.

M3-B must use a distinct update queue kind. The old create-only worker ignores additive receipt fields, so reusing its kind could create a duplicate project. Update activation needs its own artifact/production/version fences, one-POST acknowledgement and uncertain-write reconciliation, plus local and live acceptance. This checkpoint adds no update execution controls.

The later [update-review checkpoint](M3-UPDATE-REVIEW-CHECKPOINT.md) adds server-issued preparation/read history for an existing-project update, with a pinned artifact and observed production/aliases. Submission and jobs remain unavailable. Its formatting/syntax evidence is separate from the unexecuted behavioral, persistence and live gates.

Source handoffs and risk/acceptance limits:

- [Backend](/mnt/data/cooker-ecosystem-20261025/resume/m3-a-implementation.md)
- [Cloud panel and frontend checks](/mnt/data/cooker-ecosystem-20261025/resume/m3-a-frontend.md)
- [Backend and HTTP review](/mnt/data/cooker-ecosystem-20261025/resume/m3-a-review.md)
- [ClickUp transfer verification](/mnt/data/cooker-ecosystem-20261025/resume/clickup-acceptance.json)
