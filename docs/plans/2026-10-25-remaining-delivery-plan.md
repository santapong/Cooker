# Cooker — remaining delivery plan

**Planning review: 25 October 2026, Asia/Bangkok.** Audience: self-hosted users and small teams. Direction: reliable Git-to-deploy, Vercel first, then bounded AWS and GCP support. Preserve Ready Block, brackets, Open Workbench, charcoal/ember and Paper; the accepted landing, login, documentation and workspace remain the design baseline.

This is the current remaining-work view of the [45-package technical plan](2026-10-02-consolidated-feature-roadmap.md), [feature guide](2026-10-03-feature-roadmap-guide.md) and [requirement ledger](/mnt/data/cooker-roadmap-20261002/roadmap-audit/requirements.json). It preserves their scope and acceptance requirements. It does not mark packages complete or authorize provider changes.

**What is already here**

- Native/Vercel Apps, Services, production-first catalog, selected-project sync, read-only provider flows, release history and coverage/freshness presentation.
- Native editable DAGs, reviewed branch/tag rule authoring, approval/automatic modes, durable decisions/jobs and explicit single-ref recovery.
- Readable configuration, protected/versioned secrets, per-service bindings, registry authority and Docker runtime/log observation.
- Local/OIDC authentication, social-provider discovery/buttons and setup instructions; real GitHub/Google setup and round trips remain unverified.
- Bounded existing AWS/GCP deployment adapters and partial lifecycle controls. These do not establish complete ecosystem integration.

The primary checkout is clean on `develop` at `949d4a964abec603c9b0b82f6f8d8b7f9f23d73e`. Before this document, the isolated candidate was on `feature/backend-automation-20260928` with 669 pending paths. No remote fetch, build, test, browser, account, provider or running-service check was performed for this planning review. Source presence and recorded older tests are separate from current deployment/release acceptance.

**Latest evidence that changes the immediate backlog**

| Evidence | What it establishes | What remains |
| --- | --- | --- |
| [Manual native journey](/mnt/data/cooker-roadmap-20261002/environment-refusal-history-20261004/continuation-20261004/NATIVE-DAG-JOURNEY-ACCEPTANCE.md), 5 October | Real source → worker → build → registry → Docker, selected-secret redaction, three roles and ordinary restart; 10 browser checks on its recorded inputs | Combined current candidate, automatic/hosted Git and owner release acceptance |
| [Automatic Gitea V5 failed attempt](/mnt/data/cooker-roadmap-20261002/environment-refusal-history-20261004/continuation-20261004/native-gitea-driver-preparation-v5/trial-56f927f4-1cca-4445-a98d-de0c98a59147/ROOT-FAILED-ATTEMPT-REVIEW.json) | Signed callback, one queued native deployment, three successful stages, original signed receiver replay without additional source/registry effects | Overall campaign failed in its driver before SQL audit, browser and restart. The unstarted audit container was subsequently validated and removed; partial business success is not full acceptance |
| [V6 preparation](/mnt/data/cooker-roadmap-20261002/environment-refusal-history-20261004/continuation-20261004/native-gitea-driver-preparation-v6/SOURCE-PACKET.json) | Source-only variable-name repair and static regression | Pending source review/readiness; no actual V6 trial exists. Rebudget before execution under current resource rules |
| [Authenticated-attempts UI](/mnt/data/cooker-roadmap-20261002/environment-refusal-history-20261004/continuation-20261004/native-authenticated-attempts-ui-v1/HANDOFF.md) | New native source-connection attempt visibility; 21 unit/API-stub/SSR tests plus TypeScript, lint and build | Real signed-in integration. This newer 332-file frontend was not the 331-file frontend in the accepted manual journey |

The ledger still records all 45 package and 20 gate completion fields as `not_proven`. This means full acceptance is open; it does not mean implementation is absent. Its 12 shared rules remain binding. The 2 October 13-project Vercel comparison and 3 October owner readiness are historical, not fresh account coverage or uptime claims.

**How to deliver this**

Use small releases with explicit exit criteria and one main implementation slice at a time. Track **implemented → locally verified → live accepted → released** separately. Reuse valid unchanged-source evidence; repeat behavior checks when inputs changed, a check failed or a concern remains. Do not rebuild the existing editor, login buttons or log system merely because a whole-package field is unaccepted.

| Order | Outcome | Why now | Exit decision |
| --- | --- | --- | --- |
| 0 | Reconcile and freeze one candidate; prepare budgeted verification | Pending source and tested snapshots differ | Exact source/build inputs, current supported scope, test budget and rollback inputs recorded |
| 1 | Finish automatic native Git deployment and its workspace feedback | Joins already implemented components into the core user task | Real signed push → one intent/job/run → exact artifact/runtime/logs → duplicate no-op → audit/UI/restart |
| 2 | Complete trustworthy catalog and remaining R1 reliability; run auth setup in parallel | Users need correct inventory and recoverable deployment before broader writes | Relevant catalog, security, log, recovery and hosted-provider acceptance passes; unsupported capabilities stay unavailable |
| 3 | Adopt an existing Vercel project and update the existing landing | Highest-priority hosting integration; avoids duplicate projects/deployment owners | Same provider project, exact reviewed artifact, retained deployment/alias and reconciled operation |
| 4 | Extend Vercel onboarding, authored stage and independent operation controls | Reuses the accepted binding/operation contract | Each enabled action passes its own permissions, uncertainty and live-project gate |
| 5 | Complete supported promotion/rollback, domains and tunnels | Depends on reliable release and service identity | Correct target/traffic/address, recovery and DNS/TLS/access behavior |
| 6 | ECS/Fargate ecosystem slice, then Cloud Run | Validate the shared provider contract on one ecosystem at a time | Selected inventory → deployment → readiness/access/logs → failure/recovery → cleanup per provider |

Read-only catalog acceptance, provider registrations and documentation can proceed alongside local engineering. Vercel adoption/operation design can start before every independent R1 package closes, but write activation needs its relevant shared execution/security/recovery gates. No full R1 completion is claimed while any of its original requirements remains open.

**0 — Make the candidate reviewable and testable**

1. Reconcile pending files against the accepted snapshots; preserve unrelated work and failed-attempt evidence. Create a coherent source/build manifest and reviewable change set.
2. Recheck current host capacity before executing tests. Budget the entire compiler/database/daemon/browser stack, enforce CPU/RAM/PID/time/storage limits and run one heavy phase at a time. Old per-container limits do not prove a compliant aggregate budget.
3. Use the [service-test budget](/home/santapong/.codex/skills/service-test-budget/SKILL.md). Its numeric starting ceilings are trial guidance, not measured Cooker requirements. If local work is too heavy, check Pi capacity read-only, ARM64 compatibility and effective controls, then prepare a concrete scoped proposal; do not treat production services as spare test resources.
4. Complete the V6 source review and revise its resource contract before running it. Keep the source/function repair separate from actual acceptance. Integrate the newer attempts UI into the candidate intended for user testing.
5. Retain previous builds, database backup and original encryption/auth keys for supported rollback. Do not mix an application rollback with a database downgrade.

**R0 — Accurate Apps, Services and provider flows**

| Package | Remaining deliverable |
| --- | --- |
| CAT-01 | Establish a justified ordinary-application signal. Where complete service metadata is unavailable, keep the explained Application aggregate and unknown inventory/health; do not infer a database, queue or function from a framework name. |
| CAT-02 | Recheck aliases against the exact current production deployment. Keep unique deployment URLs and latest preview/activity separate. |
| CAT-03 | Revalidate the existing connection’s account/team and accessible/selected coverage; verify one app/flow per selected project, service/release links, rename/overlap/deselection behavior and landing receipt identity. |
| CAT-04 | Complete real signed-in Apps → Services → flow → release → provider dashboard acceptance, roles, keyboard access, narrow screens and both themes. |
| CAT-05 | Verify selection/discovery counts, pagination, freshness and partial/stale warnings in the actual workspace. New accessible projects are discoverable, not silently selected. |

**Done when:** selected inventory has no unexplained omission/duplicate; services and production destinations are truthful; the existing landing is traceable across views; imported flows remain read-only and native controls retain their permissions. An accessible project outside the connected team or current selection is not automatically missing sync data.

**R0a — GitHub and Google sign-in, parallel setup**

| Package | Remaining deliverable |
| --- | --- |
| AUTH-01 | Confirm current registrations; register GitHub first and Google next if still absent, with exact broker callbacks. |
| AUTH-02 | Configure the existing operator-managed Keycloak/OIDC path: issuer/audience, S256 PKCE, redirects/logout, allowed providers and trusted role groups. Keep secrets in secure broker configuration. |
| AUTH-03 | Test both real provider round trips, local administrator access, normal/admin roles, denial/cancellation/expiry/retry, deep-link return, logout and configured MFA. Do not silently merge accounts by email. |
| AUTH-04 | Verify activation and rollback instructions; advertise only working methods and preserve a usable local administrator path. |

Use the existing [social sign-in setup guide](../user-guide/operations/social-sign-in.md). Cooker sign-in, Git repository access and Vercel hosting access are three separate connections. User passwords/MFA remain manual steps. Registration status was not freshly checked in this review.

**R1 — Reliable Git-to-deploy, protected configuration and service logs**

| Package | Remaining deliverable |
| --- | --- |
| GIT-01 | Complete one permitted hosted private GitHub source → signed webhook → reviewed deployment → real image/digest → Docker readiness/logs campaign; measure callback acknowledgement against the original gate. |
| GIT-02 | Complete real GitLab acceptance, including nested/private repositories, typed branch/tag names, annotated tags and revoked access; retain Gitea regression. |
| GIT-03 | Finish the automatic Gitea audit/UI/restart journey; verify real rule setup, review-required approval and edit/revocation/version boundaries. Reuse existing exact/glob branch/tag authoring and the durable worker path. |
| GIT-04 | Integrate and verify delivery → decision → intent → job → native run navigation, all meaningful outcomes and pre-inbox authenticated failures/uncertainty. Source-connection history must not invent a pipeline assignment. |
| GIT-05 | Add the missing opt-in automatic missed-event sweep with finite page/request/time budgets, backoff, cancellation and progress/provenance feedback. Verify recovery followed by a genuine webhook creates one logical deployment. Finish hosted token replacement/revocation acceptance; retain existing secure replacement and explicit-ref recovery. |
| SEC-01 | Complete combined ordinary/secret editing and selected service bindings across the supported deployment paths and fallback. Plain settings stay readable; protected values remain masked; unrelated values never reach the workload. |
| SEC-02 | Accept actual external-store immutable versions, IAM and retention/replay behavior for the selected supported adapters. Missing historical versions fail closed. |
| SEC-03 | Finish offline key-rotation durability/process-loss/restore and compatible-executable recovery. Preserve original-key recovery; online keyring rotation remains separate. |
| SEC-04 | Complete remaining registry transports/base-image policy, in-flight revocation and compatible upgrade/fallback execution. Keep App/native per-stage credential grants and accepted versions pinned. |
| SEC-05 | Close remaining redaction boundaries: persisted/live/download logs, split chunks, errors, notifications, late credential resolution and recovery output. Use synthetic canaries. |
| LOG-01 | Verify the combined Docker App/native logs experience: immutable deployment/container identity, replicas, bounded search/time/page/download, expiry/reconnect, roles and both themes. Preserve already accepted manual/runtime evidence. |
| LOG-02 | Implement immutable Kubernetes log sessions bound to controller generation, pod UID, container and restart identity; complete a disposable-cluster campaign before enabling the capability. |
| REL-01 | Complete remaining worker-loss/cancellation/outage/result-persistence windows with stale-worker fencing and explicit uncertain outcomes; no blind repeat deployment. |
| REL-02 | Finish scheduler crash/restart no-double-fire and notification retry/recovery acceptance. Existing status/attempt visibility stays separate from delivery and human acknowledgement. |
| REL-03 | Complete supported executable/schema upgrade, rollback and older-schema restore; preserve encrypted authority, histories and cursors. Measure queue/API latency and capacity before making capacity claims. |

**Immediate implementation order inside R1:** finish the narrowly repaired automatic journey → integrate real rule/attempt feedback → add bounded automatic recovery → complete the remaining failure/upgrade/provider campaigns. Full R1 also retains its Kubernetes, security and operational requirements; a Docker-only milestone does not close them.

**Done when:** the hosted GitHub campaign and Gitea regression, authored native DAG, relevant GitLab acceptance, exact source/artifact/configuration authority, protected service logs and supported recovery/upgrade paths meet their original gates. Existing local or fixture results count only within their recorded scope.

**R2 — Vercel deployment control**

Imported Vercel projects already explain observed deployments. They are not executable Cooker DAGs. Keep them inspection-only unless an explicit deployment binding grants supported actions.

| Package | Remaining deliverable |
| --- | --- |
| VRC-01 | Add registered Vercel integration authorization/callback, account/team binding, selection, revocation and changed-scope handling. Retain encrypted token connection as an operator option; request write permission separately from read access. |
| VRC-02 | Optionally add authenticated/deduplicated provider events that schedule safe reads. Preserve polling/backoff as repair and explicit project selection. |
| VRC-03 | Add a reviewed **existing-project** update for the retained Cooker landing. The current publisher creates a dedicated new project; it must not be reused as an update by creating another project. |
| VRC-04 | Add explicit adoption bindings for provider project, deployment owner, source, environment, permitted actions and connection version. |
| VRC-05 | Add a bounded Vercel stage to authored native DAGs, using the binding and immutable revision/reviewed artifact. Link native run → operation → provider deployment → catalog history. |
| VRC-06 | Add permission/retention-aware provider build/runtime log inspection where tested support exists; otherwise show unavailable and link to the provider dashboard. |
| VRC-07 | Add reviewed promotion/rollback with exact deployment/current-production preconditions, operation/alias history and reconciliation of external changes or uncertain writes. |
| VRC-08 | Add a separately permitted project/environment configuration editor. Protect values, audit scope/names/versions and keep all raw environment values out of catalog snapshots. |

**Implementation order:** adoption/operation contracts → update the existing landing → authored Vercel stage → separately accepted logs/environment/lifecycle controls. OAuth onboarding can proceed in parallel; token access already exists as an operator route, subject to actual current scope. Optional faster-refresh events do not block the first safe existing-project update.

Choose whether Vercel Git integration or Cooker owns automatic deployment for each adopted project/environment. Prevent both from independently deploying the same change. The public landing artifact remains the reviewed seven-file allowlist, including the accepted icon; backend/auth/workspace/design-preview files are excluded.

**Done when:** one reviewed operation updates the same project/address, records the exact artifact/deployment, checks the resulting destination and reconciles uncertain results without blindly replaying a POST. Each extra action has its own live permission/behavior gate. Full R2 also requires its OAuth/selection/revocation criteria.

**R3 — Lifecycle controls and usable addresses**

| Package | Remaining deliverable |
| --- | --- |
| LIFE-01 | Connect promotion approval to real durable dispatch and record exact artifact/configuration/source/target plus queued/accepted/ready/failed/needs-attention results. |
| LIFE-02 | Complete supported prior-good application rollback with audited immutable artifact/configuration selection, readiness and reconciliation. Existing Kubernetes single-image rollback is partial; database/data rollback is separate. |
| LIFE-03 | Qualify canary/traffic controls only on supported adapters; observe actual versus desired traffic, restart and externally changed/uncertain state. |
| DOM-01 | Add exact service/environment/release domain bindings, ownership/DNS verification, routing and separate TLS issue/renew/expiry/restart states; start with manual DNS instructions. |
| DOM-02 | Add collision-safe operator subdomain reservations and observed routing/certificate policy. Each installation user need not buy a domain if its operator supplies a valid base zone. |
| TUN-01 | Add an explicitly configured persistent Cloudflare Tunnel adapter with protected credentials, exact service routing, access policy, reconnect/restart/revocation and health. |
| TUN-02 | Add optional temporary development addresses with explicit lifetime and cleanup. Keep ngrok as a later adapter option rather than implementing two tunnel providers together. |

**Done when:** supported promotion/rollback/traffic actions and real domain ownership, DNS/TLS renewal, collision, routing and tunnel recovery checks pass. Provider URLs, user domains, operator subdomains, persistent tunnels and temporary preview addresses remain distinct choices. Do not route databases, Redis, Docker control or Cooker administration implicitly, or assign the owned apex/www by assumption.

**R4 — AWS first, GCP next**

| Package | Remaining deliverable |
| --- | --- |
| AWS-01 | Add selected ECS/Fargate services/deployments to the shared catalog using account/region/resource identity and safe freshness/coverage. Existing EC2/EKS/ECR inventory is a different read model. |
| AWS-02 | Complete ECR digest → task/service operation receipts, permission preflight, readiness, endpoint/access and configured CloudWatch logs. Reuse existing bounded ECS deployment code. |
| AWS-03 | Replace a previous-number heuristic with an exact completed prior-good revision, durable failed/uncertain/cancel reconciliation and tested supported rollback. |
| GCP-01 | Add selected Cloud Run services/revisions to the shared catalog with project/region/resource identity. Existing Compute/GKE/registry inventory is separate. |
| GCP-02 | Complete Artifact Registry/Cloud Run operation and revision receipts, readiness/address, IAM/ingress/access and Cloud Logging. Reuse existing create/update adapter code. |
| GCP-03 | Replace list-order rollback with exact known-ready revision selection; wait for and reconcile desired/observed traffic and operation results. |

**Done per provider:** selected import → permitted disposable update → exact revision/readiness → endpoint/access/logs → failure/recovery/rollback → owned cleanup. Require selected account/project, region, infrastructure, permissions, cost ceiling and cleanup ownership first. Provisioning RDS/Cloud SQL, Redis services, VPCs and broad infrastructure is later scope, not implied by deploying an application that references them.

**Two acceptance examples**

1. **Cooker landing:** read-only Apps/Services/provider-flow attribution first; later review a new public bundle, update the existing adopted Vercel project and corroborate the resulting deployment/address/icon. A synced source → deployment → destination explanation is not an editable executable DAG.
2. **n8n queue mode:** deploy a reviewed, version-pinned PostgreSQL + Redis + n8n main/web + worker stack with correct scoped values and shared required n8n configuration. Verify service identity/readiness/logs, stop only its disposable worker, observe a waiting execution, resume it and corroborate completion in database/logs. Show deployment order separately from runtime database/queue communication. A worker does not inherently need the web process to start first. Do not run this against the existing production n8n installation or promise every cloud target fits long-running workers.

**Inputs needed at the relevant milestone**

- R0: current owner sign-in and existing scoped Vercel connection for read-only comparison; account/team and project-selection intent.
- R0a: GitHub/Google registrations, broker origin/redirects, role/MFA policy; secure credential entry outside chat.
- R1: permitted private GitHub/GitLab test repositories and owned reachable HTTPS callbacks; a disposable Kubernetes context for its log-session gate; selected external-secret/registry scopes when qualifying those adapters.
- R2: exact retained Vercel project, adoption/deployment-owner choice and scoped write permission; integration registration when enabling OAuth.
- R3: chosen operator zone/tunnel/hostname, routing and access policy.
- R4: selected cloud account/project/region, existing infrastructure, test resources, cost ceiling and cleanup scope.

Request these incrementally. Do not import another project's credentials or change a working connection just to fill a planning prerequisite.

**Release checklist and definition of done**

Keep the original 20 gates: seven milestone/provider gates; landing-read, landing-update and n8n journeys; contract, persistence, real-UI and live-provider verification; source/build freeze and rollback; user, developer, operator and status documentation; and coherent release integration.

For every shipped slice:

- Record exact reviewed source, dependencies/build inputs and supported capabilities; preserve failed checks and old snapshot compatibility.
- Pass affected Go/frontend/TypeScript/lint/build checks, actual disposable persistence/recovery checks and the relevant real UI/provider journey. Fixture success is labelled as fixture success.
- Verify roles, keyboard access, phone/desktop and both themes; keep graph and accessible step-list explanations equivalent and provider readiness separate from application health.
- Confirm no secret persistence/disclosure, duplicate mutation, stale authority or misleading success; retain explicit unavailable/needs-attention outcomes.
- Document actual user steps, developer APIs/identities and operator setup, rotation, migration, recovery and cleanup.
- Rehearse local activation and supported application/database recovery separately, then prepare the reviewable integration into `develop`. A merge, push, public deployment, DNS change or credential change is not implied by writing this plan.

No completion percentage or calendar promise is assigned from test counts or pending-file counts. Estimate each reviewable slice after its inputs and test budget are known. Broader multi-tenant hosting, extra providers, universal infrastructure provisioning, automatic database rollback and additional branding redesign are outside this delivery sequence.
