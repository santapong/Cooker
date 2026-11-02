# M2 Neon binding contract — proposed next slice

**27 October 2026. Proposal only: no binding implementation, passing acceptance or task completion is claimed.** Applies to [ECO-M2-02](https://app.clickup.com/t/z8xxqwcd7d), dependent on ECO-M1/ECO-M2-01 in [TASKS.json](TASKS.json). Existing ClickUp criteria and [PLAN.md](PLAN.md) remain authoritative. This document changes neither tracker state nor the other session's frontend identity/layout work; palette remains fixed.

## Outcome and evidence boundary

An administrator explicitly reviews: “this exact Neon database is mapped to this selected Vercel project's environment.” Cooker independently confirms both endpoints through authorized metadata reads, then stores the administrator's mapping. Resource existence is provider-observed; the relationship is **operator-reviewed**, not provider-discovered application use.

Installation access, matching names/frameworks, `DATABASE_URL`, a branch named main, READY and declared account mode establish none of that relationship. SQL/application health remains unknown. Provisioning, credentials/environment editing, migrations, deployment, deletion, tunnels and executable DAG entities are excluded.

Keep evidence dimensions separate: offered/installed, resource observed, operator mapping, application configuration and behavior verification. This slice can establish only observed resources and an operator mapping. Configuration/migrations/SQL checks remain **not checked** with independent future evidence; do not advance every dimension to “verified.”

## Source-backed starting point and gaps

- Neon `ConnectionService`, `DiscoverProjects`, versioned encrypted onboarding/manual sync and database/endpoint inspection exist in current source. Organization is immutable; selected projects and credential versions fence reads. Snapshot contains projects/branches; database/endpoint observations are currently returned on demand, not persisted as binding evidence.
- Vercel connection identity is personal/team account ID plus exact project ID. Catalog `vapp_`/`vflow_` IDs derive from provider identity, not names. M1 `IntegrationInventory` proves allowed project access only.
- Catalog currently has read-only App/Service/Pipeline projections and optional integration observations. It has no Neon adapter, binding record, relationship freshness or binding API. Existing landing adoption/update reviews concern deployment authority, **not database mappings**.
- Current secure-Neon verification is owned by root; earlier fixture/format results must not be substituted for its pending full evidence. No current-account resource relationship was checked while drafting.

## Minimal records and identity

Propose one narrow binding module, separate from executable stores and credential families:

| Record | Required safe fields |
| --- | --- |
| BindingReview | Server ID, actor, creation/10-minute expiry; both connection IDs/versions; exact endpoint tuples; environment/context; selected safe resource/target observations and times; reviewed declaration |
| ResourceBinding | Stable ID/version/enabled; confirmed review ID and actor/time; endpoint tuples; relationSource=`operator_reviewed`; independent observation/attempt/error clocks; retained safe evidence |

Neon identity: `(organizationID, projectID, branchID, databaseID)`. Database ID remains a canonical decimal string scoped by project/branch; names are display only. Vercel identity: `(accountScope, accountID, teamID, projectID)`. Connection IDs/versions pin authority but do not define provider identity. An active exact mapping is unique across overlapping connections; multiple genuinely different databases per target are allowed.

Environment is initially production or preview. Production stores the authoritative `targets.production.id` observed during review as `reviewedAgainstDeploymentID`. Preview requires an exact identity-verified preview deployment ID; never use branch names or “latest” as identity. Development/custom environments remain unsupported until a qualified exact-ID adapter exists.

A preview mapping also records a reviewed production-binding reference, distinct Neon branch/project tuple, explicit cleanup/lifecycle owner and declared account mode with source. If production separation is unknown, defer preview confirmation. Distinct IDs prove structural separation only; credentials, data isolation and runtime use remain unverified. Billing owner is unknown unless separately evidenced; a declaration is labelled as such.

## Proposed API and review flow

Authenticated reads: `GET /api/v1/resource-bindings/neon` and `/:id`, filtered by visible selected targets. Admin+existing MFA/rate limits:

1. `POST /api/v1/admin/resource-bindings/neon/reviews`: accept only endpoint IDs, expected connection versions, environment/exact preview context and explicit mapping declaration. No browser observation, actor, health, URL or secret is authoritative.
2. `POST .../reviews/:id/confirm`: same actor confirms the stored unexpired review; repeated confirmation returns the same binding.
3. `PUT .../bindings/:id`: expected binding version, supported enable/disable or a new reviewed replacement. Endpoint changes require a new review. Disable is local and possible after disconnect; retain history.
4. `POST .../bindings/:id/refresh`: bounded metadata reads only; refresh resource/target evidence, never infer a relationship from newly discovered names or transfer authority to a replacement token silently.

Review performs serial bounded reads under one overall90-second deadline, no retry: exact enabled selected Neon project/branch/database through existing safe APIs; Vercel identity/selected exact project; authoritative production or exact preview deployment context. Introduce a narrow Vercel **read attestation** adapter: current catalog snapshots alone cannot prove a fresh review, and binding code must not decrypt Vercel credentials itself. Exclude env endpoints/raw responses.

Only chosen allowlisted metadata, source and successful timestamps enter the review. Recheck both connection versions/enabled/selection and context after reads and before publishing it. Cancellation, incomplete/missing metadata, ambiguity or future/zero clocks cannot create a confirmed review. Provider reads confirm endpoints, not application wiring.

## Store, revocation and catalog projection

Proposed `BindingStore`: CreateReview/GetReview, ConfirmConditional, Get/List and DisableConditional; observation refresh uses version-fenced Finish. Add separate review/binding tables in the next available additive migration, chosen after root reconciles current migrations. No new secret storage. Index exact identities and active mapping uniqueness; bound review/list sizes and filter before pagination with source-availability metadata.

Confirmation is one short PostgreSQL transaction: lock Vercel connection, Neon connection, review, then binding in a fixed shared order; recheck versions, enabled state, immutable scope, selection, actor, expiry and consumed-review identity. Enforce uniqueness atomically, including concurrent overlapping connections. No HTTP under locks. Memory adapter must share authoritative locks; copied prechecks alone are insufficient. Persisted indexed fields must agree with receipt JSON.

Pause/token replacement/selection change invalidates fresh authority, even while last-reviewed evidence is retained. Disconnect/deselection hides mappings from ordinary catalog results immediately; historical admin inspection remains labelled revoked/unavailable. No cross-connection token substitution. Reauthorization needs a new review. Refresh failure preserves dated evidence as stale, does not advance successful clocks; successful confirmed resource removal marks resource missing without deleting anything remotely. Deployment changes retain environment-level operator intent but invalidate claims about the newly current deployment until reviewed; preview mappings never migrate to another deployment automatically.

Add optional `resourceBindings` inventory to catalog AppDetail/PipelineDetail, matched by exact Vercel provider tuple and current selected scope. A small `CatalogResourceBinding` carries identity, operator source/time, environment/deployment context, safe names and freshness; health=`unknown`, applicationConfigured=`not_checked`, inspect-only actions. No native App/Pipeline/Run/job or fabricated ServiceRecord is inserted. Existing service inspection can reference its app-level mappings with that scope explicit; global service rows remain actual deployment components. Inventory reads use saved evidence, never per-row provider calls. Partial source failure cannot look like confirmed empty. Frontend presentation/identity remains the other session's responsibility.

## Delivery and acceptance

First implement production-only review/confirmation, then read-only app/flow projection, then pinned preview context/lifecycle. No broad marketplace framework is needed. Full ECO-M2-02 remains open until its service/UI/live criteria also pass.

One coherent focused batch must cover: real registered admin/MFA/reader denial; exact ID/redaction/canary and strict-body rules; overlapping connections and atomic duplicate confirmation; connection edits/revocation/cancellation during reads; persisted PG restart/upgrade/JSON compatibility; stale versus authoritative removal; production changes versus pinned preview; missing separation/billing/health remaining unknown; selected-tenant fences; filter-before-pagination and source errors; zero executable entities/provider mutations. Reuse matching existing Neon credential evidence, not unrelated test passes.

Separately authorized live acceptance compares existing resource/deployment IDs and navigation across app/service/flow without creating resources or deployments. Freeze exact source/build inputs, document migration/retention/rollback and keep configured/locally verified/live accepted/released distinct. Down migration must refuse silent removal of retained history. This proposal closes none of those gates.
