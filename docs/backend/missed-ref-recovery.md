# Manual missed-ref recovery

The 3 October backend candidate hardens App recovery before the shared Git inbox.
This is separate from repository-token replacement, automatic recovery sweeps,
hosted Git acceptance and the full GIT-05 release gate.

`POST /api/v1/admin/trigger-rules/:id/check` uses the existing authenticated automation
policy and a body containing one full ref, for example `{"ref":"refs/heads/main"}`.
The route returns a delivery and its duplicate flag with HTTP 202. It does not
claim that a worker or deployment started. Unavailable review/source inputs return
the existing fixed 422 response; provider/store diagnostics are not exposed.
The route is wired in `backend/internal/server/router.go` and used by
`frontend/src/api/automation.ts`.

## Authority before source access

App recovery requires an enabled current saved rule, exact metadata/version
equality, its server-derived machine actor, allowed repository, App/environment/
connection versions, a matching branch/tag namespace and the reviewed plan hash.
The hash is reconstructed from metadata using the same normalized source fields
as Preview. This requires no token opening, provider request or value resolution.
Legacy reviewed App version 0 remains valid when the stored App still has that
exact version; requiring a positive version would reject existing reviewed work.

After these checks, verify actual repository identity and the exact selected ref.
Validate the full commit and optional tag object, inspect the accepted source
against the reviewed envelope, then repeat metadata/actor and registry preflight
before recording the event. A changed envelope requires another review. Fixed
errors suppress arbitrary source/inspection/actor callback text.

These final metadata checks are not a transaction with inbox insertion. Dispatch,
the atomic queue admission hooks and worker validation remain independent fences.
This slice does not establish exactly-once external execution or close every
concurrent edit/revocation window.

## Shared inbox and deduplication

The selected rule authorizes checking its source/ref. The resulting normalized
event enters the existing shared inbox, which evaluates **all matching rules** on
that connection/repository/ref. Explain this behavior in the workspace: an enabled
automatic matching rule may queue work after final dispatch checks. Do not present
the action as checking only one rule or as a read-only source probe.

The recovery delivery identity includes the checked rule/version, typed ref,
commit and tag object. Logical intent identity is shared with webhook admission.
Repeating a check or receiving a later webhook retains its intent and queue job.
Branch movement before dispatch records superseded work; changed tags require
review. Receipt recovery reuses the accepted job instead of repinning it.

## Verification scope and remaining work

The artifact handoff is
[missed-ref recovery evidence](/mnt/data/cooker-roadmap-20261002/missed-ref-recovery-20261003/README.md).
It retains negative reproductions, exact source manifests, affected checks,
independent source review and actual disposable PostgreSQL tests. The PostgreSQL
fixture uses the real shared inbox and App producer with registry, scheduler,
trigger and publication admission hooks. Jobs remain pending and unclaimed;
provider identity/revision/inspection are contract fixtures. Fresh SQL pools do
not constitute a database-process restart, official API/UI or hosted acceptance.

Before full acceptance, complete actual response/decision navigation, truthful
shared-rule explanations and recovery provenance, cancellation and finite
request/page/time budgets for automatic sweeps, rate-limit/provider failure cases,
hosted missed events and genuine worker/revocation journeys. Reuse valid named
evidence on its exact inputs; do not interpret code or adjacent tests as release
acceptance.
