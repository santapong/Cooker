# Repository token replacement and recovery

Checked against the local backend candidate and scoped verification on 3 October
2026. This implements the credential-replacement part of GIT-05. Hosted-provider
acceptance, bounded missed-event recovery and release activation remain separate
gates.

## What changes

An administrator can replace an existing GitHub, GitLab or Gitea connection's
repository token in **Settings → Git connections → Replace repository token**.
The saved token is never displayed. The form checks access before saving and uses
the connection's current version to prevent overwriting a concurrent edit.

Replacement accepts only a token and an optional Git username. It retains the
connection ID, provider, Git/API origins, allowed repositories, enabled state,
webhook mode and secret value, private-network/CA configuration and creation
identity. Re-encryption can change ciphertext bytes without changing the webhook
secret. It does not create or modify provider resources or change the webhook URL.

Anonymous connections have no token to replace. GitHub App installation
credentials remain operator-managed; this endpoint cannot switch authentication
mode. A different provider or origin requires a new connection. Creation and
general connection editing retain their separate contracts; this runbook does
not claim every connection mutation performs the replacement check.

## What the access check proves

Cooker requests repository metadata for **every allowed repository** using the
candidate token on the connection's approved origin. Each result must contain a
valid provider repository ID and match the requested repository path, including
GitLab nested groups. A response for another allowed repository is insufficient.
The check uses the existing redirect, TLS, private-network and destination rules
in [the source-adapter contract](source-adapters.md).

The check runs under a shared 20-second context with at most four concurrent
repository requests. A failed or cancelled check prevents the versioned save.
Inspecting a disabled connection does not enable its saved record. The final
store write also compares the version, so two successful checks cannot overwrite
one another: one write succeeds and the stale writer receives a conflict.

This is **repository metadata access**, not checkout certification. It does not
clone, list refs, verify Git content permissions, build or deploy. Successful
metadata access cannot guarantee that a later Git checkout will succeed or that
the credential will remain valid. Accepted source execution still checks the
recorded connection version, repository identity and exact commit.

## Operator sequence

1. Obtain a replacement repository-read token through the provider's normal
   account administration. Keep the existing token available during review.
2. Open the saved connection as a Cooker administrator, enter the replacement in
   the masked form and confirm its Git username. Complete configured MFA or
   step-up authentication when required.
3. Select **Check and replace token**. Wait for the explicit success notice. Do
   not treat a timeout, closed connection or generic server error as success or
   as proof that the old value is still stored.
4. Re-inspect and re-review affected Apps and native Git deployment rules against
   the new connection version before admitting new work. Verify the needed
   source workflow under its separate acceptance procedure.
5. Revoke the old token at the Git provider after confirming the replacement and
   reviewing affected work. **Cooker does not revoke it at the provider.**

A confirmed save increments `Connection.Version`. Previously accepted plans are
not rewritten to use the new credential. Fresh source access using an old pinned
generation refuses instead of silently rebinding to the replacement. Retained
completed receipts remain historical evidence; replacement does not replay them
or certify ongoing work that already passed a source-access boundary.

## API and permissions

Authenticated `POST /api/v1/admin/source-connections/:id/token-replacement`
belongs to the existing administrator and MFA middleware group. Viewer and
operator roles cannot use it. MFA enforcement depends on the configured allowed
ACR/AMR values; an unconfigured gate does not establish MFA acceptance.

The JSON body is capped at 32 KiB and has these fields:

| Field | Meaning |
| --- | --- |
| `version` | Current positive integer connection version from metadata |
| `token` | Nonempty replacement value, submitted through the secure form |
| `username` | Optional explicit Git username; omitted means retain it |

Unrelated request fields cannot change the provider, origins, webhook or
repository allowlist through this endpoint. A successful `200` response contains
`connection` metadata, `checkedRepositories` and
`check: "repository_metadata_access"`. The returned connection has the committed
next version and `hasToken`/`hasWebhookSecret` flags, never decrypted credentials.

Durable-queue configuration supplies the source-connection store and access
checker. An unavailable store/checker refuses replacement. The legacy
token-bearing connection `PUT` also uses fixed replacement failure summaries;
its broader connection-edit semantics are not a token-only operation.

| Result | Meaning and next action |
| --- | --- |
| `400` | Malformed request or missing token/current positive version; correct the input |
| `401` / `403` | Sign in with the required administrator authentication; configured MFA can require step-up |
| `404` | Connection unavailable; reload inventory |
| `409` | A concurrent connection edit won; reload and review before another replacement |
| `422` | Input or repository access check failed; replacement was not saved |
| `503`, explicitly unavailable checker | The check could not run; saved credentials were not changed |
| `503`, unconfirmed result; transport failure; malformed acknowledgement | The result is uncertain; reload validated metadata before deciding what to submit next |

Unexpected persistence failures can occur after an applied write whose
acknowledgement was lost. The API therefore uses a fixed unknown-result message
and does not claim the old token remains saved. Provider bodies, credential
values and arbitrary store/probe error text are excluded from replacement
failure responses. Normal audit middleware records identity, route and status,
without reading request or response bodies.

## Uncertain-save recovery and draft privacy

The workspace validates required connection metadata, the requested connection
ID, the exact next version, the check label and both repository counts before
showing a replacement acknowledgement. A partial or malformed response is
uncertain even if its HTTP status is `200`.

On conflict or uncertainty, the masked draft is retained in component memory,
submission is disabled, and **Reload connection and clear draft** is the explicit
recovery action. No automatic replacement retry occurs. Reload must return a
valid metadata inventory containing the edited connection. Failed, null, partial
or missing-connection reloads retain the disabled draft and fixed recovery error.
A validated reload closes the editor and clears the draft; it does not reveal the
stored token or prove which token was applied after a lost acknowledgement.

Closing or leaving the editor, signing out or changing the session owner clears
the draft. Token inputs are masked and are not saved in local/session storage.
Route, mount and session fences prevent late replies from changing a departed
editor. Fields and duplicate submission are blocked while a save is in flight.
The connection-creation form uses the same busy-state protection, without
claiming the replacement endpoint's access preflight for creation.

## Recorded local verification

The retained evidence is under
`/mnt/data/cooker-roadmap-20261002/source-credential-replacement-20261003/`.
[The verification record](/mnt/data/cooker-roadmap-20261002/source-credential-replacement-20261003/verification.json)
pins source manifests, commands, results and cleanup. These checks used owned
fixtures, not existing provider accounts or the owner's Cooker instance.

| Check | Observed scope |
| --- | --- |
| Affected Go race tests | 1,364 passing test entries, 693 top-level tests, four packages, no skips/failures on the 871-file freeze |
| Actual PostgreSQL | Three subcases plus parent passed: persisted replacement through a fresh SQL pool, failed-probe exact-row retention, and one winner/one conflict from two concurrent writers |
| Backend final inputs | The 872-file freeze adds only the PostgreSQL integration test to the verified 871-file source; default/integration vet and static build/export passed |
| Frontend | 375 unit tests in 44 files, scoped lint and 16 mocked browser cases on the final 319-file freeze |
| Browser coverage | Three provider forms, denied access, stale/unknown/partial saves, invalid reloads, roles, delayed route/sign-out replies and both themes; no live provider or official-server browser acceptance |

The exported static binary is pinned by
[the final build result](/mnt/data/cooker-roadmap-20261002/source-credential-replacement-20261003/static-build-result-v3.json).
It was not launched for this acceptance slice. Earlier test-runtime/export
failures remain in the artifact directory; they are not provider failures.
The PostgreSQL test used an injected metadata checker, so it proves persistence
and version fencing rather than network access. A fresh SQL pool is not a
database or application process-restart test.

Relevant implementation and tests:

- [Token-only service contract](../../backend/internal/source/connection/token_replacement.go)
  and [replacement unit tests](../../backend/internal/source/connection/token_replacement_test.go).
- [Repository metadata preflight](../../backend/internal/service/source_credential_check.go)
  and [PostgreSQL persistence/CAS test](../../backend/internal/source/connection/token_replacement_integration_test.go).
- [API failure classification](../../backend/internal/handler/source_connections.go)
  and [masked replacement editor](../../frontend/src/components/app/SourceTokenEditor.tsx).

## Remaining GIT-05 release gates

GIT-05 combines token replacement with missed-event recovery. This slice does
not complete that package. Remaining acceptance includes:

- Authorized real GitHub/GitLab/Gitea replacement checks, subsequent checkout
  and provider-side old-token revocation. Prior local Gitea source acceptance is
  separate evidence, not acceptance of this replacement flow.
- A genuine App/native admitted-job journey through replacement and fresh-worker
  source-version refusal, with retained receipts and no unapproved replay.
- Bounded missed-event recovery and its user journey: exact typed branch/tag
  selection, current reviewed authority, duplicate prevention, obsolete-ref
  refusal and explicit conflicting/paused/deleted-rule behavior. Replacement
  alone neither catches up deliveries nor dispatches jobs.
- An authenticated official-server API/workspace journey and coherent local UAT
  rollout, with recovery and compatible fallback checked against the intended
  build. Static compilation, mocked browser tests and source presence do not
  establish owner activation or full release acceptance.

See [the technical roadmap](../plans/2026-10-02-consolidated-feature-roadmap.md)
for package and release gates, and [delivery decision history](git-delivery-history.md)
for the existing event/intent/run inspection boundary.
