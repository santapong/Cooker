# M1 integration-installation contract

User outcome: see Vercel-reported installed integrations (including Neon when actually reported) and allowed access to selected projects, without inventing database resources, use, health or executable services.

Primary contract: GET https://api.vercel.com/v1/integrations/configurations?view=account, exact stored teamId when present. Official schema is a top-level array, no documented pagination. Missing projects means all-project access; [] means no allowed project IDs. This is authorization, not resource binding. Marketplace/native and external installations differ from Vercel Connect and from native Vercel products.

## Frozen cross-component Go/JSON shape

Add exported types in provider/vercel:

```go
type IntegrationInstallation struct {
 ID string `json:"id"`
 IntegrationID string `json:"integrationId"`
 Slug string `json:"slug,omitempty"`
 Name string `json:"name"`
 InstallationType string `json:"installationType"` // marketplace / external / unknown; omitted wire defaults external
 ProviderStatus string `json:"providerStatus"` // known reported installation state; unknown if unavailable, never resource health
 ProjectAccess string `json:"projectAccess"` // all_projects / selected_projects
 ProjectIDs []string `json:"projectIds"` // normalized allowed IDs, then intersected with selected IDs in public projection
}
type IntegrationInventory struct {
 Items []IntegrationInstallation `json:"items"`
 ObservedAt *time.Time `json:"observedAt,omitempty"` // latest successful inventory; does not advance on failure
 AttemptedAt *time.Time `json:"attemptedAt,omitempty"`
 State string `json:"state"` // available / stale / unavailable; missing observation is not observed
 ErrorCode string `json:"errorCode,omitempty"` // fixed safe code, no provider body
 Error string `json:"error,omitempty"` // Cooker-authored only
 Complete bool `json:"complete"`
}
```

Snapshot.Integrations and derived public Connection.Integrations are optional *IntegrationInventory. A separate optional IntegrationsAPI interface implements `Integrations(context.Context,string,string) ([]IntegrationInstallation,error)`; do not widen existing mandatory API. Database JSONB remains backward readable; no migration or new credential family.

Catalog AppDetail.Integrations and PipelineDetail.Integrations are optional *vercel.IntegrationInventory, filtered to the provider project. Complete describes coverage of the retained successful observation, even when State=stale; first unavailable has Complete=false. No new route is necessary: reuse existing catalog App/Pipeline detail plus Vercel View/connection APIs. Do not add integrations to executable records or ServiceRecord. Do not infer Neon identity from names as stable identity; installation ID+account remain identity, provider name/slug are display/search only.

## Sync and safety

- One installation GET per successful core sync, serial with optional detail work; <=15s installation request, <=1MiB decoded body, <=200 rows, no automatic retry. Existing core90s/lease/version/selection/pause/disconnect fences remain.
- Reuse exact origin and safe HTTP errors; no redirects/arbitrary hosts. Reject non-array/null/malformed identity/explicit cross-team data, duplicate/conflicting IDs and explicit null/malformed project access; unknown future enums remain unknown. Never truncate and claim complete. Success [] is authoritative empty.
- Optional error must retain core project/deployment success. Preserve prior inventory and successful clock as stale; first failure unavailable; unsupported optional API not observed/unavailable. Respect the existing connection's optional rate-limit cooldown and return max cooldown through existing Sync machinery; installation work follows the existing four-worker detail phase. This adds no global cross-connection semaphore.
- Deep clone all new slices/pointers in memory/filter/public projections; selection intersects authorized project IDs, preserves missing-vs-empty semantics and omits installations with no selected-project access. For all_projects, public ProjectIDs may represent only the selected subset and must retain ProjectAccess text; never expose unselected project IDs through the new field.
- At every public boundary, incomplete inventory or a missing/zero successful observation clock exposes no installation rows or successful clock. Normalize it to unavailable while retaining attempt/error context; do not mutate the saved input. Complete last-known successful observations remain eligible as stale.
- Catalog merges installation clocks independently from core/details clocks, scoped by same account/project. Newer successful empty supersedes older nonempty; another account is separate. Keep data/attempt/error paired to the chosen complete observation. Never graft another connection token failure onto a valid winner; per-connection warnings remain in provider View. If no success exists, choose newest failed AttemptedAt. Last-known data is not freshly observed.
- No environment endpoints, database queries, provider credential reads, raw JSON persistence, scopes-as-caller-permissions, arbitrary metadata/icon fetching, installation/provisioning/billing or provider mutations. Provider-resource and SQL-health discovery remain later separate adapters.

## Ownership

ECO-07 owns backend/internal/provider/vercel model.go, client.go, enrichment.go, service.go, memory.go, details.go (cloneSnapshot field only) and new integrations*.go production files; no tests yet. It may choose a wrapper around the existing optional details method so old DetailsAPI mocks remain compatible.
ECO-08 owns backend/internal/catalog model.go/service.go and new integration projection source files only; optional detail shape above; no handler/router additions unless coordinator agrees. No provider package edits.
ECO-09 owns frontend types/api optional integration fields and a shared IntegrationAccess component wired into synced App/Pipeline details, Cloud Vercel view and existing connection cards as appropriate. Preserve Cooker identity and read-only role behavior; no global CSS redesign, extra provider requests, provider-resource/health claims or independent services.

Workers return changed paths and rationale without running checks. Root coordinates one focused backend batch and one frontend TS/unit/build batch after source freeze, followed by independent security/compatibility review. Correctness of security/state changes still needs targeted tests even if small; documentation edits need no application suite.

## Done distinction

A reviewed implementation and passing fixture/unit/build evidence can establish local M1 verification. Actual current token/role permissions and installed-account content require a scoped live read, separately labelled. The existing45-package program and future ecosystem milestones remain open until their own criteria pass.
