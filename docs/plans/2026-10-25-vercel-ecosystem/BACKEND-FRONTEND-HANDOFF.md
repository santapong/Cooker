# Backend and frontend coordination

Owner direction, current27October2026: continue the backend plan in this chat; a separate session may improve frontend identity. **The current color palette must not change.** Accepted ReadyBlock/OpenWorkbench work is the starting point, not a veto on the newly authorized identity work. This backend phase edits no frontend source, shared branding assets, theme CSS or embedded landing bundle.

Backend source: /mnt/data/company/apps/Cooker-backend, feature/backend-automation-20260928, base949d4a9 with preserved pending work. Primary checkout/owner server/public hosting remain separate. No cross-chat message, merge/push or public deployment is implied.

## Current execution checkpoint

**Current checkpoint — 27 October 2026:** secure Neon connection, discovery, resource, HTTP and persistence source is prepared. The current default package passed **38 top-level tests plus 88 subtests**, with zero failures/skips; its wrapper failed afterward. SQL16 and HTTP6 remain **NOT RUN**: finite campaigns stopped on temporary-container lifecycle and disk-counter freshness guards, respectively. All owned work/staging was independently cleaned and the shared lane released. The existing Neon ClickUp task is freshly verified **Blocked** with original protected fields retained. See [verification](VERIFICATION.md), [resource tracking](RESOURCE-TRACKING.md) and [backend/frontend ownership](BACKEND-FRONTEND-HANDOFF.md). This is a runner-acceptance blocker despite Pi RAM headroom; waiting for RAM alone will not resolve it. All full product/live/release criteria remain open; resource automation stays PAUSED.

Backend source/API remains additive and unchanged during these runtime trials. Identity/layout and frontend validation remain the other owner session’s work; no palette change is requested. The binding contract is proposed, not implemented.

## Additive Neon API contract —source prepared, runtime acceptance pending

Authenticated saved reads under /api/v1:

- GET /integrations/neon returns connections plus providerOperations=metadata_reads_only,databaseHealth=not_checked,managementModeEvidence=operator_declared.
- GET /integrations/neon/connections/:id returns {connection}.

Admin-only local configuration and bounded provider metadata reads under /api/v1/admin:

- POST /integrations/neon/connections: name,organizationId,accountMode (unknown/neon_managed/vercel_managed),apiKey,projectIds (explicit array,up to5),acknowledgeKeyPermissions=true.
- PUT /integrations/neon/connections/:id: required version; optional name,accountMode,enabled,projectIds,apiKey. A replacement key requires acknowledgeKeyPermissions=true. Organization is immutable.
- DELETE /integrations/neon/connections/:id?version=N disconnects locally; providerResourcesChanged=false.
- GET /integrations/neon/connections/:id/projects?version=N discovers accessible organization projects, including when projectIds=[], without selecting anything or changing snapshot clocks.
- POST /integrations/neon/connections/:id/sync manually refreshes selected project/branch snapshots. No background poller. Inspect returned state/errorCode; HTTP200 is not provider success.
- GET /integrations/neon/connections/:id/projects/:projectId/branches/:branchId/databases?version=N
- GET /integrations/neon/connections/:id/projects/:projectId/endpoints?version=N

Resource reads require a successful saved selected-project/branch inventory and return {inventory,databaseHealth:not_checked}. They do not store a Vercel project/environment binding or check SQL health. Start with an enabled connection and projectIds=[] if IDs are unknown; call project discovery, save explicit project selection through a versioned PUT, then Sync before resource inspection.

Connection fields are defined in backend/internal/provider/neon/connection_store.go; resources in model.go. Keys/ciphertext/leases are never returned. State is not_observed/unavailable/observed/stale/paused, with independent successful/attempt clocks. The API key may have provider write permissions even though Cooker only reads metadata. Account mode is operator-declared, not verified billing ownership. Backend enforces authentication/admin roles independently of frontend visibility. Invalid,null,duplicate or unknown request fields are rejected.

All routes and migration042 still require their actual HTTP/PostgreSQL acceptance; fixture success must remain separately labelled. Full prior APIs remain available unchanged. The frontend session can implement against this additive contract after checking the latest source; do not infer a running owner server from this file.
