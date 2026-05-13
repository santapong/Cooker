# ADR-0004 — Multi-tenancy: data-scoped `owner_team_id`, with namespace-scoped `tenant_id` deferred
Date: 2026-05-12
Status: Accepted (PM decision 2026-05-13, A3-defer). Supersedes the "awaiting Decision A" gate. Revisit Q4 2026 if a hosted-Cloud signal arrives.

## Context

Cooker today has no resource-ownership model. Every authenticated user — including a `viewer` — can read every Pipeline, App, Environment, Host, and Run by ID. This is `S26-05-09` (HIGH) in the May 2026 security review:

- Finding header: `docs/audits/2026-05-security-review.md:113` (`#### S26-05-09 — HIGH — Authenticated authorization gap on app / environment / host reads (IDOR by-id)`).
- Affected handlers: `handler/app.go:51-57`, `handler/environment.go:26-36`, `handler/host.go:41-47`, `handler/pipeline.go:86-92` (enumerated at `docs/audits/2026-05-security-review.md:115`).
- Originally flagged: `vulnerabilities-and-chains.md` A.6 #2; remained open after the W10 recheck (`docs/audits/2026-05-security-review.md:23`).
- The review explicitly does **not** recommend a one-line fix: "adding a `created_by` / `team_id` column requires migration + a multi-tenant scoping decision" (`docs/audits/2026-05-security-review.md:118`). That decision is what this ADR makes.

Two user-journey audits independently surface the same gap:

- **SaaS persona, step 4** (`docs/audits/W11-user-journeys.md:71`) — bulk app import is painful but tractable; the deeper issue is that "team" has no meaning in Cooker.
- **Enterprise persona, step 4** (`docs/audits/W11-user-journeys.md:106`) — "Cooker is single-org. All 4 teams' pipelines / apps / environments live in one shared list. The `groupRoleMap` is a flat CSV: cannot say 'auth-admin: admin in `auth-team` namespace, viewer elsewhere.'"
- **W11 follow-up promotion** (`docs/audits/W11-followup-2026-05.md:18`) — Enterprise §4 "Tenant scoping (data-scoped or namespace-scoped)" promoted to **P1**, design-doc-gated.

The 2026 roadmap encodes the same dependency graph:

- **C1** (`docs/roadmap-2026.md:80`) — "Multi-tenancy v1 — ownership column (`owner_user_id` / `owner_team_id` on Pipeline / App / Environment / Host). Closes `S26-05-09` IDOR." Effort: XL. Open question: "Data-scoped (`tenant_id` everywhere) vs namespace-scoped (Cooker-Namespace wrapping). **Decision needed before code.**"
- **C2** (`docs/roadmap-2026.md:81`) — SAML auth. Depends on a coherent tenant story to be worth the lift.
- **C3** (`docs/roadmap-2026.md:82`) — Cooker Cloud free tier. Depends on **C1 (tenant isolation)**. "The roadmap's biggest open question: freemium tier yes/no?"

Decision A in the PM brief (`docs/pm-brief-2026-05.md:111` Q1, `docs/pm-brief-2026-05.md:117` Q7) is unanswered: we do not yet know whether hosted Cooker Cloud will be pursued. Both questions explicitly note that this ADR is gated on the answer.

PM locked **A3-defer** on 2026-05-13: ship `owner_team_id` now; do not ship `tenant_id` until a hosted-Cloud signal lands; revisit Q4 2026. This ADR documents the now-shape (Decision §1–§3) and the future-execution playbook (Appendix A).

## Decision

Adopt **A3 — defer the tenancy boundary; pick the cheap ownership-column shape now**, on the following terms.

### 1. Schema

Add one column to every Cooker-owned resource:

| Table | Column | Type | Constraint |
|---|---|---|---|
| `pipelines` | `owner_team_id` | `BIGINT` | `NOT NULL`, FK → `teams(id)` |
| `apps` | `owner_team_id` | `BIGINT` | `NOT NULL`, FK → `teams(id)` |
| `environments` | `owner_team_id` | `BIGINT` | `NOT NULL`, FK → `teams(id)` |
| `hosts` | `owner_team_id` | `BIGINT` | `NOT NULL`, FK → `teams(id)` |
| `runs` | `owner_team_id` | `BIGINT` | `NOT NULL`, FK → `teams(id)` |

A new `teams` table:

```sql
CREATE TABLE teams (
  id          BIGSERIAL PRIMARY KEY,
  slug        TEXT NOT NULL UNIQUE,
  display    TEXT NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE team_members (
  team_id  BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  user_id  TEXT   NOT NULL,
  role     TEXT   NOT NULL CHECK (role IN ('admin','operator','viewer')),
  PRIMARY KEY (team_id, user_id)
);
```

A seed `default` team (`id=1`) is created on migration; all pre-existing rows get `owner_team_id = 1`. The migration is therefore additive and back-compat: existing single-tenant installs keep behaving as before with no operator action required.

### 2. RBAC checks

Handlers gain a single helper, `auth.RequireTeamMember(ctx, teamID, minRole)`, called at the top of every handler that reads or writes a team-scoped resource. List endpoints filter by the caller's team membership set. The flat `groupRoleMap` continues to work — it maps OIDC group claims to roles in the `default` team — until C2 (SAML) or C3 (Cooker Cloud) forces a scoped grant model. The audit's `groupRoleMap` complaint (`docs/audits/W11-user-journeys.md:106`) becomes solvable but is **out of scope for this ADR** (it lives in C1's implementation ticket, roadmap row 26).

### 3. The `tenant_id` future-migration path

If hosted Cooker Cloud is later approved (Decision A → "yes", per `docs/pm-brief-2026-05.md:111`), `tenant_id` is introduced as a **strict superset** of `owner_team_id`:

- Add `tenants(id, slug, plan, created_at)` and `tenant_id BIGINT NOT NULL REFERENCES tenants(id)` on every resource AND on `teams`.
- One-to-one back-compat seed: each existing `team` gets exactly one `tenant`, `tenant_id` propagated to all resources.
- RBAC checks compose: `RequireTenantBoundary(ctx, tenantID) && RequireTeamMember(ctx, teamID, role)`.
- The migration ships as `internal/store/postgres/migrations/010_tenancy.up.sql`. No data loss; existing installs run as a single tenant indefinitely.

This means **picking A3 today does not foreclose A1 tomorrow**. The ownership-column shape becomes the inner-loop check; the tenant boundary becomes the outer-loop check. Both layers coexist cleanly.

## Consequences

+ Closes `S26-05-09`. Every read/write of a team-scoped resource is gated on team membership. Viewer-role users no longer enumerate other teams' app metadata.
+ One additive schema column per resource, one new `teams` table, one new `team_members` table. All NOT NULL with a default-team back-fill — no operator action on upgrade.
+ Estimated cost: ~half a day of handler RBAC wiring (5 handlers × ~30 lines each) + migration + tests. Tracks against C1 effort estimate "XL" in the roadmap (`docs/roadmap-2026.md:80`); the XL covers UI surfacing (team picker on Pipeline/App/Environment/Host create wizards, team-management page), which can land incrementally.
+ Does not foreclose Cooker Cloud. The `tenant_id` migration path above is documented and reversible.
+ Closes the W11 Enterprise §4 design-doc gate (`docs/audits/W11-followup-2026-05.md:18`). Implementation can then proceed under roadmap row 26 (`docs/roadmap-2026.md:146`).

− Until the team-picker UI ships, all resources default to `owner_team_id = 1`. Single-tenant installs see no behaviour change; multi-team installs see no enforcement gain until the wizards land. The schema column is the contract; the UX is the follow-up.
− `groupRoleMap` remains a flat CSV mapping OIDC groups to roles within the `default` team. Scoped grants ("admin in `auth-team`, viewer elsewhere") are still un-modelled. Tracked separately; not regressed by this ADR.
− Cross-team resource sharing (e.g. one Pipeline used by two teams) is **not supported** under this model. Each resource has exactly one owning team. If shared usage emerges as a real need, it will require either a `team_resource_grants` table or promotion to the full `tenant_id` model.
− The migration sets `NOT NULL` with a default-team back-fill, which means rollback is non-trivial. Mitigation: ship the migration as `up`/`down` pair; the `down` drops the columns and the `teams` tables. Tested in CI against an ephemeral Postgres.

## Alternatives considered

### A1 — Full multi-tenant `tenant_id` from day one

- Pros: ready for hosted Cooker Cloud; one schema migration instead of two; aligns with the audit's "namespace-scoped wrapping" branch (`docs/roadmap-2026.md:80`).
- Cons: scope explosion. Tenant lifecycle (create / suspend / delete), tenant-scoped quotas, tenant-scoped OIDC tenancy mapping, and tenant-scoped audit-log filtering all become same-PR concerns. Roadmap C1 sized this at XL — that's the A1 number. A3 is ~half a day plus the team-UX follow-up.
- Rejected because: hosted Cooker Cloud (`docs/roadmap-2026.md:82` C3 / `docs/pm-brief-2026-05.md:111` Q1) is undecided. Doing A1 now bets the half-day-vs-three-weeks budget on a hosted offering the user has not committed to.

### A2 — Single-tenant forever; document `S26-05-09` as a known property

- Pros: zero schema churn; matches the security review's "S (just document the property)" branch (`docs/audits/2026-05-security-review.md:119`).
- Cons: leaves `S26-05-09` open indefinitely; tombstones roadmap C2 (SAML — `docs/roadmap-2026.md:81`) and C3 (Cooker Cloud — `docs/roadmap-2026.md:82`); kills the Enterprise persona at W11 §Enterprise step 4 (`docs/audits/W11-user-journeys.md:106`).
- Rejected because: the marketing brief explicitly excluded the Enterprise persona from launch ICP because of `S26-05-09` (`docs/pm-brief-2026-05.md:47`). Closing this finding is a roadmap-level priority, not a documentation-level one.

### A3 — Defer the tenancy boundary; ship `owner_team_id` now (chosen)

- Pros: closes `S26-05-09`; matches the C1 description verbatim (`docs/roadmap-2026.md:80`); leaves C2/C3 open without committing to them; cheap.
- Cons: a future C3 commitment forces a second migration (`010_tenancy.up.sql`). Acceptable cost of optionality.

## Appendix A — Future `tenant_id` migration (revisit Q4 2026)

The Q4-2026 execution playbook for a hosted-Cloud commitment. **Do not execute** until at least one trigger fires: (a) PM commits to hosted Cooker Cloud, (b) a customer contract requires tenant isolation, (c) SAML (roadmap C2) is greenlit. None today → keep deferred.

### A.1 Migration #010 — `010_tenancy.up.sql`

```sql
CREATE TABLE tenants (
  id BIGSERIAL PRIMARY KEY,
  slug TEXT UNIQUE NOT NULL,
  plan TEXT NOT NULL DEFAULT 'free',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  suspended_at TIMESTAMPTZ NULL
);

INSERT INTO tenants (id, slug, plan) VALUES (1, 'default', 'self-hosted');

ALTER TABLE teams        ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 1 REFERENCES tenants(id);
ALTER TABLE pipelines    ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 1 REFERENCES tenants(id);
ALTER TABLE apps         ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 1 REFERENCES tenants(id);
ALTER TABLE environments ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 1 REFERENCES tenants(id);
ALTER TABLE hosts        ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 1 REFERENCES tenants(id);
ALTER TABLE runs         ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 1 REFERENCES tenants(id);
ALTER TABLE users        ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 1 REFERENCES tenants(id);
ALTER TABLE audit_log    ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 1 REFERENCES tenants(id);

-- Back-fill from team ownership; the DEFAULT 1 above handles fresh-install case
UPDATE pipelines    SET tenant_id = (SELECT tenant_id FROM teams WHERE teams.id = pipelines.owner_team_id);
UPDATE apps         SET tenant_id = (SELECT tenant_id FROM teams WHERE teams.id = apps.owner_team_id);
UPDATE environments SET tenant_id = (SELECT tenant_id FROM teams WHERE teams.id = environments.owner_team_id);
UPDATE hosts        SET tenant_id = (SELECT tenant_id FROM teams WHERE teams.id = hosts.owner_team_id);
UPDATE runs         SET tenant_id = (SELECT tenant_id FROM teams WHERE teams.id = runs.owner_team_id);
```

Paired `010_tenancy.down.sql` drops the columns and `DROP TABLE tenants`.

### A.2 Model field bumps

`Pipeline`, `App`, `Environment`, `Host`, `Run`, `User`, `Team`, `AuditLog` each gain `TenantID int64 \`json:"tenantId"\``. Store-interface `Get*ByID` and `List*` methods gain `tenantID int64` as the leftmost arg after `ctx`. Memory and Postgres impls add `WHERE tenant_id = $1` filtering.

### A.3 Middleware injection

New `auth.RequireTenantBoundary` registered in `internal/server/server.go` immediately **after** `auth.AuthMiddleware` on the `/api/v1` group. Reads `tenant_id` from the configured OIDC claim, sets `c.Set("tenantID", tid)`. Every handler that currently calls `auth.RequireTeamMember` gains a preceding `tid := auth.TenantFromContext(c)` and passes it to the store call. Composition: `RequireTenantBoundary` outer-loop, `RequireTeamMember` inner-loop.

### A.4 WS hub scope-key changes

`internal/server/ws_hub.go` currently keys subscriptions by `runID` and `userID`. Under tenancy: keys become `tenantID:runID` and `tenantID:userID`. The ticket store (`POST /api/v1/ws-tickets`) embeds `tenant_id` in the ticket payload. WS upgrade handler cross-validates ticket-tenant vs URL-path resource-tenant before joining the hub.

### A.5 OIDC tenancy claim mapping

`internal/auth/oidc.go` extracts `tenant_id` from a configured claim path. New env var `COOKER_OIDC_TENANT_CLAIM` (default `cooker_tenant_id`). Self-hosted installs continue to mint a synthetic claim `cooker_tenant_id=1` in the dev-admin injector path. Production-mode `Config.Validate()` requires `COOKER_OIDC_TENANT_CLAIM` when `COOKER_TENANCY_MODE=multi`.

### Estimate

~3 weeks of focused work (handler updates, store-layer scoping, OIDC tenancy claim, admin UI for tenant management). Matches the C3 "XL" sizing (`docs/roadmap-2026.md:82`).

## Appendix B — A2 (rejected by PM 2026-05-13)

A2 (single-tenant forever, document `S26-05-09` as a known property) was rejected. Retained for historical context only: the marketing brief excluded the Enterprise persona from launch ICP because of `S26-05-09` (`docs/pm-brief-2026-05.md:47`); closing it is a roadmap-level priority, not a documentation-level one. See "Alternatives Considered → A2" above for the original analysis.
