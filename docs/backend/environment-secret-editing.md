# Environment configuration and protected-key editing

Ordinary values remain in `plainVars`; protected values are written separately and are never included in environment responses. A key cannot exist in both readable configuration and the database-backed secret map. Environment configuration writes use the existing `version` compare-and-swap contract.

Authenticated `GET /api/v1/environments/:id/secret-keys` is metadata-only. It returns:

```json
{"keys":["DATABASE_PASSWORD"],"sourceAvailability":"available","generation":7,"conditionalEditing":true}
```

Names are sorted and deduplicated. The endpoint calls only the configured manager's `List`, never `Get` or a reveal operation. Reads have a five-second context deadline and a 10,000-key response cap. A failed or malformed provider response is `503` with fixed text, `sourceAvailability:"unavailable"`, no keys or generation, and no raw provider error in logs. A concurrent environment-generation change is `409`; refresh metadata before editing. Credential-only registry environments remain unavailable through this API.

`generation` is the recorded Cooker `Environment.Version`. For the local database manager it fences both configuration and secret edits. External manager metadata can be available while `conditionalEditing` is false; this does not claim remote key/value consistency or remote compare-and-swap. Supported immutable deployment versions use the separate accepted-plan contract.

Administrator protected writes keep the existing MFA and role rules:

- `PUT /api/v1/environments/:id/secrets/:key` accepts `{"value":"new protected value","version":7}`.
- `DELETE /api/v1/environments/:id/secrets/:key?version=7` uses the same generation fence.
- Successful conditional edits return the committed `generation` with key and `stored` or `deleted` status.
- A stale generation is `409`; no write occurs. Invalid explicit generation, including null, is `400`.
- A manager without `secrets.ConditionalEditor` returns `501` before provider mutation. Never retry the request without its version.
- Legacy callers that omit version retain their old route behavior, without a client-generation or conditional-edit claim. They can still receive a store conflict; the server does not retry against newer configuration.

No secret value is needed to populate a draft. Keep failed protected-value drafts only in session memory, masked; refresh and review changed metadata rather than silently overwriting configuration or guessing an empty key inventory. A new environment starts at server-assigned generation 1; saved generation and fresh metadata are required before conditional secret editing.

## App and native pipeline bindings

New Apps return server-assigned version one, matching the persisted definition
and its first conditional edit. Client-supplied initial versions are overwritten.
Saving bindings updates an App or native pipeline definition; it does not deploy
or alter already accepted work. Compose uses its recorded service names. A
Dockerfile App uses the single `deploy` component and supports an explicit empty
selection; see [the Dockerfile binding contract](dockerfile-environment-bindings.md).
Supported native steps retain their existing selected-key contract. Buildpack
runtime selection is unavailable.

For `PUT /api/v1/pipelines/:id`, send the `version` returned by the latest
definition read. A matching version is checked against the database before the
write, and a successful edit increments it. Stale or future explicit versions,
including zero when the stored version is one, return `409` without writing.
Null, negative, noninteger and overflowing versions return `400` with fixed
text. New JSON/YAML-created pipelines return server-assigned version one,
matching the persisted definition and allowing its first conditional edit.
Legacy requests that omit `version` retain their existing behavior, including
the database race fence, but provide no client-version protection. The workspace
retains a conflicted draft for comparison and deliberate reapplication against
the latest version rather than automatically resubmitting it.

This API does not promote secret values, rotate the storage encryption key, manage external providers' keys, or establish full SEC-01 acceptance by source presence alone. Actual provider/version and deployment binding acceptance remain separately scoped.
