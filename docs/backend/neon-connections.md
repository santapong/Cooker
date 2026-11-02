# Neon metadata connections

Cooker uses a separate Neon management key to read organization/project/branch/database/compute metadata. Vercel credentials do not grant Neon API access. A Neon key may inherently permit writes; Cooker performs GET metadata operations only and requires explicit acknowledgement. Account mode (unknown/neon_managed/vercel_managed) is operator-declared, not verified billing ownership. No SQL health, provider provisioning, deployments or Vercel environment bindings are inferred.

## Backend connect and inspect flow

1. An administrator supplies an existing organization ID and key through the authenticated secure configuration API. Do not put keys in chat, command arguments, logs or documents. The backend requires active storage encryption and PostgreSQL.
2. Create with an explicit projectIds array (empty is allowed), name/accountMode and acknowledgeKeyPermissions=true. Credentials are encrypted with purpose, connection ID and organization binding; the API only returns hasCredential.
3. If project IDs are unknown, inspect GET /api/v1/admin/integrations/neon/connections/:id/projects?version=N. It returns accessible projects in the configured organization without selecting them or advancing sync clocks.
4. PUT the exact projectIds (up to5) with the current version. Organization identity is immutable. Every configuration edit advances version; stale edits return conflict. Replacing a key requires acknowledgement and successful scope validation before committing.
5. POST /api/v1/admin/integrations/neon/connections/:id/sync. This manually refreshes selected project/branch metadata under a versioned durable lease. Inspect state/errorCode even on HTTP200. A failed read retains a labelled stale observation; an empty successful inventory differs from unavailable access.
6. Use the versioned admin database/endpoint reads listed in the frontend handoff. Database inspection requires a branch from a successful saved inventory. Provider lifecycle state is not SQL readiness or application health.
7. Pause by PUT enabled=false. Disconnect uses DELETE with version and removes only Cooker-held configuration/credentials, not the Neon key or hosted resources. Revoke provider keys through the provider when needed.

Authenticated readers can view saved connections through GET /api/v1/integrations/neon and /connections/:id. Administrator/MFA policy applies to configuration, refresh, discovery and fresh resource inspection. Only canonical JSON field names are accepted; null/duplicates/aliases/unknown fields are rejected. After an uncertain write response, read current state before retrying.

## Developers and operators

DTOs and HTTP envelopes: [backend/frontend handoff](../plans/2026-10-25-vercel-ecosystem/BACKEND-FRONTEND-HANDOFF.md). Safe fixed error codes distinguish invalid input, conflict, paused, denied provider access, incomplete metadata and unavailable storage. API keys, ciphertext, leases, provider bodies, connection strings and raw error text are excluded. Database names are intentionally normal metadata; they are not global resource identity.

Requests use a fixed Neon origin,15-second request and45-second inventory deadlines; projects/branches have bounded pagination, database/compute lists use the official full-list schemas. A client has at most4simultaneous connections. Manual selected-project sync is90seconds with a100second lease; there is no background poller. Selection/key changes invalidate incompatible data and in-flight results. There are at most100connections and5selected projects per connection; these limits are coverage bounds, not proof of provider entitlement.

Migration042 stores encrypted credentials separately and adds six-family offline key-rotation compatibility. Read [offline key rotation](offline-key-rotation.md) before recovery. Never remove the compatibility guard to run old five-family tooling; down042 refuses remaining local Neon rows. No owner database/key change is authorized by this guide.

## Verification state

The current default Neon package passed38 top-level tests plus88 subtests onARM64 Go1.25.14,CGO0, with zero failures/skips. The wrapper failed afterward; separate failure and cleanup evidence are retained. PostgreSQL/key-rotation16 and HTTP6 did not execute: finite runners stopped on temporary-container lifecycle and five-second disk-counter freshness during setup/dependency preparation. Both campaigns are stopped and independently cleaned. These are execution blockers, not passed or skipped integration tests; current task stage is Blocked.

SQL persistence/rotation, registered HTTP/MFA/audit, race, authorized live-account comparison, explicit Vercel bindings and release remain unproven. Source/fixtures do not establish an activated owner connection. Exact unit evidence: /mnt/data/cooker-ecosystem-20261026/neon-connections/postgres-run/full-after-qualification/default-accepted.json. Current failure/cleanup and next eligibility: [verification handoff](../plans/2026-10-25-vercel-ecosystem/VERIFICATION.md).
