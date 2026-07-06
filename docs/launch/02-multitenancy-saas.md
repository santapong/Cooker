# 02 — Multi-tenancy and Tenant Isolation Design

**Series:** Cooker SaaS Launch Prerequisites
**Date:** 2026-06-20
**Status:** Design / gap-analysis — no code changes in this document.
**Depends on:** `docs/adr/0004-multi-tenancy.md` (A3-defer decision, 2026-05-13).
**Feeds into:** `docs/launch/01-billing.md` (billing needs a `tenant_id` FK), `docs/launch/03-saas-hosting.md` (SaaS hosting presupposes hard isolation boundaries).
**Effort:** XL (honest: ~3 calendar weeks of focused engineering, not counting build-farm isolation work which is a separate XL).

---

## 1. Current-state map

### 1.1 What exists today

| Primitive | Status | Location |
|---|---|---|
| `owner_team_id` column on resource tables | **Not shipped.** The ADR was accepted on 2026-05-13 but no migration implementing it exists in the codebase. The latest migration is `023_api_tokens.up.sql`. | `docs/adr/0004-multi-tenancy.md` §Decision §1 — intent only |
| `teams` table | **Not shipped.** No migration, no store interface, no Go model. | ADR §Decision §1 — intent only |
| `team_members` table | **Not shipped.** | ADR §Decision §1 — intent only |
| `tenant_id` column on resource tables | **Explicitly deferred.** ADR §Decision §3, Appendix A. | Revisit Q4 2026 |
| `tenants` table | **Explicitly deferred.** | ADR Appendix A.1 migration `010_tenancy.up.sql` (not yet written) |
| Instance-wide RBAC (`admin`/`operator`/`approver`/`viewer`) | Shipped. Flat, no team or tenant scope. | `backend/internal/auth/rbac.go`, `backend/internal/auth/permission.go` |
| OIDC group → role mapping (`groupRoleMap`) | Shipped. Flat CSV, maps OIDC group to instance-wide role. Cannot express "admin in team A, viewer in team B". | `backend/internal/auth/rbac.go:120-155` |
| Per-user rate limiting | Shipped, in-memory per-process. No per-tenant quota. | `backend/internal/server/ratelimit.go` |
| API tokens | Shipped. Instance-scoped, no team FK. | `backend/internal/store/postgres/migrations/023_api_tokens.up.sql`, `backend/internal/auth/apitoken.go` |

### 1.2 S26-05-09: IDOR / instance-wide reads — current state (confirmed in code)

The security finding from the May 2026 review (`docs/audits/2026-05-security-review.md:113`) is **still open**. All store `List` and `Get` calls are instance-wide:

- `PipelineStore.List(ctx)` — `SELECT ... FROM pipelines ORDER BY updated_at DESC` with no ownership filter (`backend/internal/store/postgres/pipeline.go:25`).
- `PipelineStore.Get(ctx, id)` — `SELECT ... FROM pipelines WHERE id = $1` with no ownership check (`backend/internal/store/postgres/pipeline.go:44`).
- `AppStore.List(ctx)` — `SELECT ... FROM apps ORDER BY updated_at DESC` (`backend/internal/store/postgres/app.go:33`).
- `AppStore.Get(ctx, id)` — `SELECT ... FROM apps WHERE id=$1` (`backend/internal/store/postgres/app.go:52`).
- `EnvironmentStore.List(ctx)`, `HostStore.List(ctx)` follow the same pattern.

The handler layer (`handler/pipeline.go:73`, `handler/app.go:42-58`, `handler/environment.go:29`) calls these without any team or tenant filtering. Any authenticated user — including a `viewer` — can call `GET /api/v1/pipelines` or `GET /api/v1/apps/<any-id>` and receive the full (webhook-redacted) record for any resource on the instance.

The `idor_test.go` in the handler package tests only one narrow case: cross-pipeline run access (`TestIDOR_RunIdMustMatchPipelineId`). It does not test cross-team or cross-tenant resource isolation because neither primitive exists yet.

### 1.3 Schema today: every resource table

No resource table carries any ownership column. The full schema as of migration 023:

| Table | Ownership columns | Notes |
|---|---|---|
| `pipelines` | None | Migration 001 |
| `pipeline_runs` | `started_by_user_sub`, `started_by_email`, `started_by_groups` (migration 015) | Actor capture only; no ownership FK |
| `environments` | None | Migration 001 |
| `apps` | None | Migration 003 |
| `hosts` | None | Migration 004 |
| `users` | `role TEXT` (instance-wide) | Migration 005 |
| `api_tokens` | `created_by_sub`, `created_by_email`, `role` | Migration 023; no team FK |
| `audit_events` | `user_sub`, `user_email` | Migration 019; no team FK |
| `jobs` | None | Migration 010 |
| `schedules` | None | Migration 012 |
| `pipeline_templates` | None | Migration 013 |
| `stage_approvals` | None | Migration 022 |
| `run_promotions` | None | Migration 020 |

**Summary: zero tenancy primitives are implemented in the actual codebase.** The ADR is accepted but unexecuted. The next migration to land would be `024_*`.

### 1.4 What is missing for true isolation

| Gap | Impact | Effort |
|---|---|---|
| `teams` + `team_members` tables | Cannot scope any resource to a team | S (migration, store, model) |
| `owner_team_id` on all 5 resource tables | Cannot filter reads or writes by team | M (migration per table, store interface changes, handler wiring) |
| `RequireTeamMember(ctx, teamID, minRole)` helper | Handlers have no team-membership check | M (auth package addition, all 5 handler files) |
| List endpoints: filter by caller's team set | `List` signatures gain `teamID` param or middleware pushes it via context | L (store interface change, both impls, all callers) |
| `tenant_id` boundary (deferred) | True SaaS isolation between paying customers | XL |
| Per-tenant rate limits and quotas | Noisy-neighbor protection | M (depends on tenant_id) |
| Per-tenant secrets namespace | Data isolation of env secrets between tenants | M (depends on tenant_id) |
| Per-tenant Kaniko/Buildah build namespace | Build-code execution isolation | XL (Kubernetes namespace-per-tenant or gVisor/Kata) |
| Tenant-scoped WS hub keys | WebSocket stream isolation between tenants | M (ADR Appendix A.4) |
| Tenant claim in OIDC token | IdP-issued `cooker_tenant_id` claim | M (ADR Appendix A.5) |
| Tenant lifecycle (create/suspend/delete) | SaaS onboarding flow | L |
| Billing FK (`tenant_id` → billing plan) | Meter usage per customer | Blocked on tenant_id existing |

---

## 2. Target tenant model for hosted Cooker Cloud

### 2.1 The two architectures

**Data-scoped (recommended):** every resource row carries `tenant_id BIGINT NOT NULL REFERENCES tenants(id)`. A single shared Postgres instance (potentially with multiple schemas or just row-level filtering) serves all tenants. Reads always `WHERE tenant_id = $<tid>`. The ADR calls this the "inner-loop check" shape — the same table, partitioned by column.

**Namespace-scoped:** a "Cooker namespace" is a slice of resources visible to one tenant. Could be implemented as a Kubernetes namespace per tenant (for the build cluster), a Postgres schema per tenant, or a fully separate Cooker instance per tenant.

### 2.2 Recommendation: data-scoped with per-tenant Kubernetes namespaces for build isolation

Recommendation: data-scoped `tenant_id` for all persistent data (Postgres), paired with per-tenant Kubernetes namespaces for build-job execution (§3 below). Rationale:

1. **Operational leverage.** One Postgres instance, one Cooker binary. A namespace-scoped model either requires one Cooker binary per tenant (operationally untenable at >10 tenants) or a complex namespace-routing layer that must be built from scratch.

2. **The ADR already chose this shape.** ADR-0004 Appendix A documents the exact migration (`010_tenancy.up.sql` — note: the number conflicts with the shipped `010_jobs.up.sql`; the actual migration would be `024_tenancy.up.sql`). The schema is well-specified. Deviating now discards that design work.

3. **Self-hosted compatibility.** Adding `WHERE tenant_id = $1` to every query is transparent to self-hosted users who run as a single tenant (id=1). A namespace-scoped model would require self-hosted operators to understand a new abstraction.

4. **Build-cluster isolation is orthogonal.** The build farm (Kaniko/Buildah jobs) must be namespace-isolated regardless of which persistence model is chosen (see §3). Mixing namespace-scoped persistence with per-tenant Kubernetes namespaces yields no additional isolation benefit but doubles the surface that must be maintained.

### 2.3 How tenants, teams, users, and RBAC compose

Under the target model:

```
tenants (id, slug, plan, suspended_at)
    └── teams (id, tenant_id FK, slug, display)
            └── team_members (team_id FK, user_id TEXT, role TEXT)
                    └── resources (pipelines, apps, environments, hosts, runs)
                                   each has: tenant_id FK + owner_team_id FK
```

Auth middleware chain for SaaS:
1. `auth.Middleware.Handler()` — validates the bearer token (OIDC / local / API token), extracts `Claims` including `tenant_id` from a configured OIDC claim (`COOKER_OIDC_TENANT_CLAIM`).
2. `auth.RequireTenantBoundary(ctx, tenantID)` — outer loop: reads `tenantID` from context set in step 1, aborts 403 if the resource's `tenant_id` does not match.
3. `auth.RequireTeamMember(ctx, teamID, minRole)` — inner loop: checks caller is a member of the resource's `owner_team_id` at or above `minRole`.
4. `auth.RequirePermission(resource, action)` — existing matrix check, unchanged.

For self-hosted installs, steps 1–2 are a no-op: all resources carry `tenant_id=1` and there is only one tenant. The middleware still runs but every check passes immediately.

RBAC scoped grants ("admin in team A, viewer in team B") become expressible through `team_members.role` per row rather than the flat `groupRoleMap`. The `groupRoleMap` continues to work for self-hosted installs by mapping OIDC groups to roles in the `default` team of the single tenant.

API tokens (`api_tokens` table, migration 023) would gain `tenant_id` and optionally `team_id` so service-account tokens are scoped to one tenant and optionally one team. A token created by a tenant admin can never be used to access another tenant's resources.

---

## 3. The hard isolation problems specific to Cooker

This section is the highest-risk section for a SaaS build. Cooker is not just a data platform — it **executes untrusted user code** inside Kubernetes Jobs. Every Kaniko and Buildah build is a Dockerfile authored by a tenant that runs with elevated privileges inside the build cluster. This is the primary attack surface for tenant escape.

### 3.1 Current build execution model

Kaniko creates a `batch/v1.Job` in the namespace configured by `KanikoConfig.Namespace` (`backend/internal/builder/kaniko.go:49-52`). Today there is one `Namespace` field per Cooker instance — all builds for all tenants land in the same Kubernetes namespace. The Buildah adapter follows the same pattern. This means:

- Tenant A's build job can read environment variables from Tenant B's build job if the Kubernetes API grants cross-pod inspect (it does, by default ClusterRole/ClusterRoleBinding without NetworkPolicy).
- A malicious Dockerfile that exfiltrates the node's service-account token can impersonate the Cooker service account, which has Job create/get/delete/watch across the build namespace — and therefore can create arbitrary pods in that namespace.
- There is no CPU/memory resource quota enforcement at the namespace level; a single runaway build can starve all other tenant builds on the node.

### 3.2 Required isolation layers for multi-tenant build farms

Each layer is independent; implement all of them before opening builds to untrusted tenants.

**Layer 1: Per-tenant Kubernetes namespaces for build jobs.**
`KanikoConfig.Namespace` must become `tenantBuildNamespace(tenantID)` (e.g. `cooker-builds-<tenant-slug>`). Each tenant namespace gets its own `ResourceQuota`, `LimitRange`, and `NetworkPolicy`. The Cooker service account requires RBAC in every tenant namespace it can create jobs in; this is best achieved with a per-namespace `RoleBinding` rather than a `ClusterRoleBinding`.

**Layer 2: NetworkPolicy blocking pod-to-pod cross-namespace traffic.**
Without this, a build pod can reach other pods in the cluster including the Cooker control-plane pod. The policy must:
- Allow egress to the container registry endpoint only (or an explicit allowlist).
- Allow egress to the Kubernetes API only for the specific calls needed (image pull secrets resolution).
- Block all pod-to-pod ingress from outside the namespace.
- Block all egress to the cluster metadata endpoint (169.254.169.254 on cloud providers — this is the IMDS/instance-role endpoint; a Dockerfile `curl 169.254.169.254` retrieves the node's cloud IAM role credentials).

**Layer 3: Pod Security Standards — Restricted level.**
All build jobs must run under the `restricted` Pod Security Standard: `runAsNonRoot: true`, `allowPrivilegeEscalation: false`, `seccompProfile: RuntimeDefault`, `capabilities: drop: ALL`. Kaniko executor can run rootless; Buildah requires rootless mode (`--storage-driver vfs`). This is not the same as the current production setup where Kaniko runs with the cluster's default PSA.

**Layer 4: Runtime sandbox (gVisor / Kata Containers) for the build namespace.**
The previous three layers reduce the blast radius but do not prevent kernel exploits from a malicious Dockerfile. A Dockerfile that exploits a Linux kernel CVE can escape a container on a shared node with ordinary PSA restrictions. gVisor (`runsc`) and Kata Containers provide stronger isolation:
- gVisor intercepts syscalls in a user-space kernel; overhead is ~10–30% on compute-heavy workloads, acceptable for most image builds.
- Kata Containers run each pod in a micro-VM; overhead is lower on compute, higher on startup latency (~1–2s cold start vs ~100ms for a gVisor pod).
For a hosted multi-tenant build farm, gVisor on GKE Sandbox or the equivalent (Azure ACI with Kata, AWS Firecracker on Fargate) is the minimum acceptable runtime for the build namespace. This is an infrastructure decision, not a Go code change — but Kaniko/Buildah must be validated against the chosen sandbox before shipping.

**Layer 5: Resource quotas and per-tenant build concurrency caps.**
`ResourceQuota` on CPU, memory, and storage per tenant namespace. Additionally, `KanikoConfig` must become per-tenant and include a `MaxConcurrentBuilds` field enforced by the job queue's `concurrency_key` (already available in `jobs` table migration 010). A tenant on a free plan might be capped at 1 concurrent build and 2 CPU / 4 GiB; a paid plan unlocks more.

**Layer 6: Build context isolation.**
Today, Cooker and Kaniko share a PVC (`KanikoConfig.ContextPVC`). A shared PVC mounted across all tenant builds leaks context files between tenants if the PVC claim allows cross-pod read. For multi-tenant SaaS: each build job must use either (a) a freshly-provisioned `emptyDir` or ephemeral volume populated at build time, or (b) a per-tenant PVC that only that tenant's build jobs can access. Option (a) is operationally simpler but requires the build context to be streamed at job start (OCI layer cache warms up per-job, not shared). Option (b) supports build cache re-use but requires per-tenant volume lifecycle management.

**Layer 7: Secrets isolation in the build environment.**
Build jobs receive environment variables including registry push credentials. These must be loaded from a per-tenant Kubernetes Secret rather than a shared Secret. The Cooker secrets backend (`secrets.Manager`) must scope lookups by `tenant_id` so `Get("registry-creds")` for Tenant A never returns Tenant B's credentials. The database secrets backend (AES-GCM, migration 002) stores secrets per-environment; adding `tenant_id` to the namespace key is sufficient for the database backend. Vault and cloud backends (AWS SM, GCP SM, KeepSave) need per-tenant path/project prefixes, which must be configurable per tenant at provisioning time.

### 3.3 Noisy-neighbor protection outside the build farm

- **WebSocket hub:** the in-memory hub backend (`server/wshub_backend.go`) already has CR-1 (blocking publish deadlock). Under multi-tenancy a slow consumer in Tenant A's WebSocket session should not block executor goroutines for Tenant B's run. WS keys must be scoped to `tenantID:runID` (ADR Appendix A.4) and the hub must drop-and-close on full buffer rather than blocking.
- **Rate limiting:** the current per-user in-memory limiter (`server/ratelimit.go`) has no tenant-level cap. A tenant with 50 users can consume 50x the quota of a tenant with 1 user at the same plan tier. The limiter must add a per-tenant aggregate bucket layered above the per-user bucket.
- **Postgres connection pool:** a long-running query from one tenant (e.g. a large `SELECT ... FROM pipeline_runs` with no `LIMIT`) can saturate connection pool slots. Row-level security (RLS) with `SET app.current_tenant = $1` on the connection is one approach; the simpler approach is `LIMIT` enforcement at the store layer and short query timeouts (`SetConnMaxLifetime`, `context` deadline per query) — the latter should be done regardless of multi-tenancy.
- **Job queue:** the `jobs` table has no `tenant_id` column. A free-tier tenant who enqueues 1000 jobs can starve paid-tier jobs in the queue. Add `tenant_id` to `jobs` and a per-tenant `MAX_PENDING_JOBS` soft limit enforced at enqueue time.

---

## 4. Migration and rollout path

### 4.1 Guiding constraint: the same binary must serve both self-hosted and SaaS

Cooker is and must remain an open-source, self-hosted tool. The tenancy migration must be additive: a self-hosted operator who runs `helm upgrade` should see no behaviour change. The strategy is:

- All tenancy columns carry `DEFAULT 1` (the pre-seeded `default` tenant / `default` team).
- A `COOKER_TENANCY_MODE` env var (values: `single` / `multi`, default `single`) gates multi-tenancy middleware. In `single` mode, `RequireTenantBoundary` is a no-op and `RequireTeamMember` is skipped.
- `Config.Validate()` requires `COOKER_OIDC_TENANT_CLAIM` to be non-empty only when `COOKER_TENANCY_MODE=multi`.

### 4.2 Step sequence

**Step 1 — Ship A3 (owner_team_id), closing S26-05-09.** This is the already-accepted ADR work, currently unexecuted. Migration number: `024_owner_team.up.sql`. Creates `teams`, `team_members`, and adds `owner_team_id` to `pipelines`, `apps`, `environments`, `hosts`, `pipeline_runs`. Back-fills all existing rows with `owner_team_id = 1` (the `default` team). Estimated effort: ~half a day of migration + store interface changes + handler wiring + conformance tests. This step is independent of SaaS and closes an open HIGH finding.

**Step 2 — Add `RequireTeamMember` and update handler reads.** The five handlers (`pipeline.go`, `app.go`, `environment.go`, `host.go`, `run`-related endpoints) gain `auth.RequireTeamMember` calls. List endpoints gain team-filtering. Store interface `List` methods gain a `teamID string` parameter (or team membership is pushed via context). Conformance tests updated. Effort: M (~2 days).

**Step 3 — Gate tenancy: `COOKER_TENANCY_MODE=multi` flag + team-picker UI.** Without a UI for team assignment, all resources stay in the `default` team. The team-picker wizard on Pipeline/App/Environment/Host create flows is the UX blocker. Effort: M (~1 day backend, ~2 days frontend).

**Step 4 — Ship `tenant_id` (migration `025_tenancy.up.sql`).** Creates `tenants` table, adds `tenant_id` to all resource tables and to `teams`, `api_tokens`, `audit_events`, `jobs`. Back-fills existing rows with `tenant_id = 1`. Ships `RequireTenantBoundary` middleware. Updates all `List`/`Get`/`Create` store calls to filter/set by `tenant_id`. Estimated effort: ~1 week.

**Step 5 — OIDC tenant claim mapping.** New env var `COOKER_OIDC_TENANT_CLAIM`. Self-hosted dev injector synthesises `cooker_tenant_id=1`. Effort: S (~half a day).

**Step 6 — API token scoping.** Migration `026_api_tokens_tenant.up.sql` adds `tenant_id NOT NULL DEFAULT 1` and optionally `team_id`. Effort: S.

**Step 7 — Per-tenant Kubernetes build namespaces.** Kaniko/Buildah builders must accept a `tenantNamespace` at job creation. Cooker must provision the namespace (with ResourceQuota, LimitRange, NetworkPolicy, RBAC RoleBinding) on first build for a new tenant. Effort: L (~3–4 days including infra testing).

**Step 8 — Secrets backend scoping.** Secrets Manager lookups gain `tenantID` prefix in the key path. Database backend needs migration. Vault/AWS/GCP/KeepSave adapters need per-tenant path configuration. Effort: M.

**Step 9 — Quota enforcement + billing FK.** Per-tenant quota rows (plan limits: concurrent builds, pipeline count, retention days). Billing integration (`docs/launch/01-billing.md`) reads quota from `tenants.plan`. Effort: M (depends on billing doc 01 design).

**Step 10 — WS hub tenant scoping.** Keys change from `runID` → `tenantID:runID`; ticket store embeds `tenant_id`; hub upgrade handler cross-validates. Effort: S.

### 4.3 Backfill strategy

The `DEFAULT 1` on every new column means no backfill query is needed beyond what the migration does automatically. The `default` team (id=1) and `default` tenant (id=1) are seeded in the same migration transaction. Existing Cooker installs see all their resources assigned to `default`/`default` with zero data change to existing rows' other columns. On a cold migration of a large instance, the `ALTER TABLE ... ADD COLUMN ... DEFAULT 1` is a metadata operation on Postgres 11+ (no table rewrite) — it is fast even on large tables.

### 4.4 Feature-flagging tenancy

- Steps 1–3: controlled by `COOKER_TENANCY_MODE=multi`. Default `single` changes nothing for self-hosted.
- Steps 4–6: same flag. Self-hosted users who never set `COOKER_TENANCY_MODE=multi` run indefinitely as a single tenant without any visible change.
- Steps 7–10: gated by both `COOKER_TENANCY_MODE=multi` and `COOKER_BUILD_ISOLATION=per-tenant-namespace` (a new flag). Self-hosted users who use a single build namespace are unaffected.

---

## 5. Dependency sequencing

### 5.1 What billing (doc 01) needs from this

- `tenants(id, slug, plan)` must exist before billing can attach a plan to a customer.
- `tenant_id` on `pipeline_runs` and `jobs` is the meter primitive: billing counts runs, build minutes, and deployed apps per tenant.
- Per-tenant quota rows must exist before billing can enforce plan limits at checkout.
- The billing webhook (e.g. Stripe `customer.subscription.updated`) must update `tenants.plan` and trigger quota recalculation.

**Sequencing:** Step 4 (`tenant_id` migration) must land before any billing integration work. The billing team can begin schema design in parallel but cannot wire Stripe webhooks until `tenants` is populated.

### 5.2 What SaaS hosting (doc 03) needs from this

- Per-tenant Kubernetes build namespaces (Step 7) must exist before the shared build cluster is opened to public tenants.
- gVisor / Kata runtime class must be configured on the build cluster before Step 7 is marked production-ready.
- `COOKER_TENANCY_MODE=multi` must be stable before onboarding the first paying customer.
- Tenant lifecycle (create/suspend/delete) — including the `suspended_at` column on `tenants` — must work before a free-tier abuse circuit-breaker can be implemented.

**Sequencing:** SaaS hosting cannot onboard its first paying (or even free) public tenant until Steps 1–7 are complete and the build farm isolation has passed a penetration test against the chosen runtime sandbox.

### 5.3 Full dependency graph

```
Step 1 (owner_team_id, ADR A3) ──► Step 2 (handler RBAC) ──► Step 3 (UI)
                                                                    │
Step 4 (tenant_id migration) ──────────────────────────────────────┤
        │                                                           │
        ├──► Step 5 (OIDC claim)                                    │
        ├──► Step 6 (API token scoping)                             │
        ├──► Step 8 (secrets backend scoping)                       │
        ├──► Step 9 (quota + billing FK) ──► billing (doc 01)       │
        └──► Step 10 (WS hub scoping)                               │
                                                                    ▼
Step 7 (per-tenant build namespaces) + runtime sandbox ──► SaaS hosting (doc 03)
```

### 5.4 Honest effort estimate

| Work stream | Effort | Owner |
|---|---|---|
| Step 1: migration + store changes for A3 | S (~half day) | backend-data (this agent) |
| Step 2: handler RBAC wiring + tests | M (~2 days) | backend-api agent |
| Step 3: team-picker UI | M (~2 days) | frontend agent |
| Step 4: `tenant_id` migration + store interface updates + all callers | L (~1 week) | backend-data + backend-api |
| Steps 5–6: OIDC claim + API token scoping | S (~1 day) | backend-api |
| Step 7: per-tenant build namespaces + infra | XL (~2 weeks including sandbox validation) | backend-adapters + infra |
| Step 8: secrets backend per-tenant scoping | M (~2 days) | backend-adapters |
| Step 9: quota + billing FK | M (~2 days, after billing doc 01 design) | backend-api + billing |
| Step 10: WS hub tenant scoping | S (~half day) | backend-api |
| **Total** | **~XL (5–6 calendar weeks, two engineers)** | |

The ~3-week figure in the ADR Appendix A covers steps 4–10 handler changes only. It does not include:
- The Step 7 build-farm isolation work (the largest single risk item).
- The upstream infrastructure work (gVisor/Kata cluster provisioning, per-tenant NetworkPolicy, RBAC).
- Any penetration testing of the isolation boundary.

With those included, the full scope is 6–8 calendar weeks for a team of two backend engineers plus infrastructure support. This is the honest XL.

---

## 6. Anti-patterns and risks

**Do not skip Step 7 before opening public tenants.** Shared Kubernetes build namespaces with a public SaaS are a direct path to cross-tenant container escape. The current Kaniko setup runs without a pod security sandbox. This is the highest-severity risk in this entire document — higher than the data isolation gaps.

**Do not add `tenant_id` columns to migrations that have already shipped.** The migration series is append-only. Adding `tenant_id` to `010_jobs.up.sql` (already shipped) is forbidden. The correct path is a new migration (`025_jobs_tenant.up.sql`) that adds the column with `IF NOT EXISTS` and `DEFAULT 1`.

**Do not implement tenant isolation only on Postgres and ignore the WS hub, job queue, and build cluster.** All four planes (data, message/stream, compute, secrets) must be isolated. Isolating only Postgres leaves three channels for cross-tenant data leakage.

**Do not store per-tenant Kubernetes kubeconfigs in the `cluster_configs` table without tenant scoping.** Once `tenant_id` lands on `cluster_configs`, each tenant can only read their own cluster connections. Until then, the Settings page exposes all cluster connections to all users — which is the same IDOR-by-id gap as S26-05-09.

**Do not use the database secrets backend with a shared AES key across tenants.** The current database secrets backend (`secrets/database`) encrypts all env secrets with the single `COOKER_SECRET_KEY`. Under multi-tenancy, this is acceptable as long as the key is rotated per-customer OR per-environment — but a single compromised key decrypts all tenants' secrets. Consider per-tenant derived keys (`HKDF(masterKey, tenantID)`) before opening the database backend to multi-tenant SaaS.

---

## 7. Summary table: what to build, in order

| # | Migration(s) | Store changes | Handler changes | Infra | Closes |
|---|---|---|---|---|---|
| 1 | `024_owner_team.up.sql` — `teams`, `team_members`, `owner_team_id` on 5 tables | `TeamStore` interface; `PipelineStore`/etc. `List` gain team filter | None yet | None | S26-05-09 (partially) |
| 2 | None | `List(ctx, teamID)` signatures | All 5 handlers add `RequireTeamMember` | None | S26-05-09 (fully) |
| 3 | None | None | None | None | W11 Enterprise §4 UX gate |
| 4 | `025_tenancy.up.sql` — `tenants`, `tenant_id` on all tables | All `Get`/`List`/`Create` gain `tenantID` | Add `RequireTenantBoundary` | None | True SaaS isolation (data plane) |
| 5 | None | None | OIDC claim extraction | None | Tenant identity |
| 6 | `026_api_tokens_tenant.up.sql` | `APITokenStore` gains `tenantID` filter | Token creation scopes to tenant | None | Token blast-radius |
| 7 | None | None | `KanikoConfig.TenantNamespace` | Per-tenant NS + quota + NetworkPolicy + gVisor/Kata | Build-plane isolation (the hard one) |
| 8 | None | Secrets path prefix per tenant | Secrets backends gain `tenantID` | None | Secrets plane isolation |
| 9 | `027_quotas.up.sql` | `QuotaStore` | Quota enforcement at enqueue | None | Billing FK, noisy-neighbor |
| 10 | None | None | WS hub key `tenantID:runID` | None | Stream plane isolation |
