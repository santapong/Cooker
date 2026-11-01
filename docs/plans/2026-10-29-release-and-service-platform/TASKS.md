# Cooker — complete remaining task register

Reviewed29October2026. **79 tasks:48 retained product IDs +31 new delivery/extension tasks.** All20 original gates and12 invariants are retained in [LEGACY-BASELINE.json](LEGACY-BASELINE.json).

This is the requested Git-versioned execution view. Existing ClickUp identities remain attached; no remote board was rewritten. Exact acceptance, dependencies and evidence live in [TASKS.json](TASKS.json). A task remains open until its complete acceptance is proven.

## R0

| ID | Task | State | Dependencies |
|---|---|---|---|
| [CAT-01](#cat-01) | Classify ordinary projects separately from unavailable multi-service metadata | open / not proven | Original stage gate / none for this slice |
| [CAT-02](#cat-02) | Enrich aliases for the exact current production deployment | open / not proven | Original stage gate / none for this slice |
| [CAT-03](#cat-03) | Reconcile selected inventory and navigation | open / not proven | Original stage gate / none for this slice |
| [CAT-04](#cat-04) | Complete actual workspace acceptance | open / not proven | Original stage gate / none for this slice |
| [CAT-05](#cat-05) | Expose coverage and operational limitations | open / not proven | Original stage gate / none for this slice |

## R0a

| ID | Task | State | Dependencies |
|---|---|---|---|
| [AUTH-01](#auth-01) | Register GitHub and Google sign-in providers | open / not proven | Original stage gate / none for this slice |
| [AUTH-02](#auth-02) | Configure the Keycloak sign-in broker | open / not proven | Original stage gate / none for this slice |
| [AUTH-03](#auth-03) | Verify sign-in, roles and account recovery | open / not proven | Original stage gate / none for this slice |
| [AUTH-04](#auth-04) | Document and verify sign-in activation and rollback | open / not proven | Original stage gate / none for this slice |

## R1

| ID | Task | State | Dependencies |
|---|---|---|---|
| [GIT-01](#git-01) | Hosted GitHub acceptance using one permitted disposable private repository | open / not proven | Original stage gate / none for this slice |
| [GIT-02](#git-02) | Real GitLab acceptance; retain Gitea regression | open / not proven | Original stage gate / none for this slice |
| [GIT-03](#git-03) | Bind authored DAGs to Git rules | open / not proven | Original stage gate / none for this slice |
| [GIT-04](#git-04) | Complete delivery/decision UI | open / not proven | Original stage gate / none for this slice |
| [GIT-05](#git-05) | Source credential rotation and missed-event recovery | open / not proven | Original stage gate / none for this slice |
| [SEC-01](#sec-01) | Improve ordinary/secret editing and service bindings | open / not proven | Original stage gate / none for this slice |
| [SEC-02](#sec-02) | Pin external-secret versions | open / not proven | Original stage gate / none for this slice |
| [SEC-03](#sec-03) | Add an encryption-key rotation procedure/tool | open / not proven | Original stage gate / none for this slice |
| [SEC-04](#sec-04) | Scope registry authority to each App and native Pipeline | open / not proven | Original stage gate / none for this slice |
| [SEC-05](#sec-05) | Verify redaction at every output boundary | open / not proven | Original stage gate / none for this slice |
| [LOG-01](#log-01) | Docker replica, time-range, bounded search and redacted download controls | open / not proven | Original stage gate / none for this slice |
| [LOG-02](#log-02) | Immutable Kubernetes log sessions | open / not proven | Original stage gate / none for this slice |
| [REL-01](#rel-01) | Extend queue crash/cancellation boundaries | open / not proven | Original stage gate / none for this slice |
| [REL-02](#rel-02) | Close scheduler admission crash windows and define notification delivery | open / not proven | Original stage gate / none for this slice |
| [REL-03](#rel-03) | Older-schema upgrade/restore and capacity checks | open / not proven | Original stage gate / none for this slice |

## R2

| ID | Task | State | Dependencies |
|---|---|---|---|
| [VRC-01](#vrc-01) | Connect Vercel through registered integration authorization | open / not proven | Original stage gate / none for this slice |
| [VRC-02](#vrc-02) | Refresh Vercel inventory from authenticated events | open / not proven | Original stage gate / none for this slice |
| [VRC-03](#vrc-03) | Update the existing Cooker landing project | open / not proven | Original stage gate / none for this slice |
| [VRC-04](#vrc-04) | Introduce explicit deployment bindings for adopted projects | open / not proven | Original stage gate / none for this slice |
| [VRC-05](#vrc-05) | Implement a bounded Vercel deployment stage | open / not proven | Original stage gate / none for this slice |
| [VRC-06](#vrc-06) | Add provider inspection/log adapter where supported | open / not proven | Original stage gate / none for this slice |
| [VRC-07](#vrc-07) | Promote/rollback supported provider releases | open / not proven | Original stage gate / none for this slice |
| [VRC-08](#vrc-08) | Add separately permitted provider environment editing | open / not proven | Original stage gate / none for this slice |

## R3

| ID | Task | State | Dependencies |
|---|---|---|---|
| [LIFE-01](#life-01) | Dispatch approved promotion and observe the result | open / not proven | Original stage gate / none for this slice |
| [LIFE-02](#life-02) | Roll back to a known prior application release | open / not proven | Original stage gate / none for this slice |
| [LIFE-03](#life-03) | Control gradual traffic where the provider supports it | open / not proven | Original stage gate / none for this slice |
| [DOM-01](#dom-01) | Bind custom domains to exact services and releases | open / not proven | Original stage gate / none for this slice |
| [DOM-02](#dom-02) | Provide collision-safe operator subdomains | open / not proven | Original stage gate / none for this slice |
| [TUN-01](#tun-01) | Connect approved services through persistent Cloudflare Tunnel | open / not proven | Original stage gate / none for this slice |
| [TUN-02](#tun-02) | Offer temporary development tunnel addresses | open / not proven | Original stage gate / none for this slice |

## R4

| ID | Task | State | Dependencies |
|---|---|---|---|
| [AWS-01](#aws-01) | Import selected ECS and Fargate services | open / not proven | Original stage gate / none for this slice |
| [AWS-02](#aws-02) | Deploy, inspect and diagnose ECS releases | open / not proven | Original stage gate / none for this slice |
| [AWS-03](#aws-03) | Reconcile and recover failed ECS rollouts | open / not proven | Original stage gate / none for this slice |
| [GCP-01](#gcp-01) | Import selected Cloud Run services and revisions | open / not proven | Original stage gate / none for this slice |
| [GCP-02](#gcp-02) | Deploy, inspect and diagnose Cloud Run revisions | open / not proven | Original stage gate / none for this slice |
| [GCP-03](#gcp-03) | Migrate and roll back Cloud Run traffic | open / not proven | Original stage gate / none for this slice |

## ecosystem

| ID | Task | State | Dependencies |
|---|---|---|---|
| [ECO-M1](#eco-m1) | Vercel installation visibility and allowed project access | open / not proven | Original stage gate / none for this slice |
| [ECO-M2-01](#eco-m2-01) | Secure Neon connections, discovery and selected metadata | open / not proven | Original stage gate / none for this slice |
| [ECO-M2-02](#eco-m2-02) | Explicit Neon resource bindings and live read attestation | open / not proven | Original stage gate / none for this slice |

## delivery

| ID | Task | State | Dependencies |
|---|---|---|---|
| [SHIP-01](#ship-01) | Freeze, sign off and push the landing plus complete repository plan | live verified / proven for feature publication slice | Original stage gate / none for this slice |
| [SHIP-02](#ship-02) | Verify B3/K1 story landing and actual OGL behavior | verified slice / proven for stated browser scope | Original stage gate / none for this slice |
| [SHIP-03](#ship-03) | Export the explicit safe public artifact | verified slice / proven for public export scope | Original stage gate / none for this slice |
| [SHIP-04](#ship-04) | Publish and verify the existing Cooker landing project | live verified / proven for production publication | SHIP-01, SHIP-02, SHIP-03 |
| [EXM-01](#exm-01) | Deploy the isolated health/log Vercel example | live verified / proven for direct provider slice | SHIP-01 |
| [EXM-02](#exm-02) | Execute the example through Cooker itself | blocked / not proven | VRC-01, VRC-04, VRC-05, INT-01, EXM-01 |

## integration

| ID | Task | State | Dependencies |
|---|---|---|---|
| [INT-01](#int-01) | Reconcile and integrate the isolated backend candidate | planned / not proven | Original stage gate / none for this slice |
| [INT-02](#int-02) | Finish B3 identity master and real-app frontend integration | planned / not proven | SHIP-02, INT-01 |

## service_groups

| ID | Task | State | Dependencies |
|---|---|---|---|
| [GRP-01](#grp-01) | Define typed environment, destination and service placement identities | planned / not proven | VRC-04 |
| [GRP-02](#grp-02) | Render collapsible destination and service groups in the DAG | planned / not proven | GRP-01, INT-02 |
| [GRP-03](#grp-03) | Bind each deployed node to its immutable runtime receipt | planned / not proven | GRP-01, VRC-05 |
| [GRP-04](#grp-04) | Gate mixed-target placement and node operations by capability | planned / not proven | GRP-02, GRP-03 |

## sandbox

| ID | Task | State | Dependencies |
|---|---|---|---|
| [SBX-01](#sbx-01) | Add Sandbox capability/account/region/cost discovery | planned / not proven | VRC-01 |
| [SBX-02](#sbx-02) | Implement an isolated Sandbox stage executor | planned / not proven | SBX-01, REL-01 |
| [SBX-03](#sbx-03) | Enforce Sandbox egress and credential boundaries | planned / not proven | SBX-02, SEC-05 |
| [SBX-04](#sbx-04) | Model persistent sessions, snapshots and resume semantics | planned / not proven | SBX-02, STO-01 |
| [SBX-05](#sbx-05) | Qualify Sandbox cleanup, retention and bounded billing | planned / not proven | SBX-03, SBX-04 |

## storage

| ID | Task | State | Dependencies |
|---|---|---|---|
| [STO-01](#sto-01) | Publish provider-specific durable-storage capabilities | planned / not proven | Original stage gate / none for this slice |
| [STO-02](#sto-02) | Add explicit service volume and Sandbox Drive bindings | planned / not proven | STO-01, GRP-01 |
| [STO-03](#sto-03) | Protect storage ownership, regions and concurrent access | planned / not proven | STO-02 |
| [STO-04](#sto-04) | Prove data survives stop, redeploy and restore | planned / not proven | STO-03, REL-03 |
| [STO-05](#sto-05) | Implement retention and separately authorized deletion | planned / not proven | STO-03, STO-04 |

## observability

| ID | Task | State | Dependencies |
|---|---|---|---|
| [OBS-01](#obs-01) | Define service telemetry scope, freshness and coverage | planned / not proven | GRP-01 |
| [OBS-02](#obs-02) | Unify bounded runtime log reads and downloads | planned / not proven | OBS-01, VRC-06, LOG-01, LOG-02 |
| [OBS-03](#obs-03) | Add bounded metric queries with honest unsupported states | planned / not proven | OBS-01 |
| [OBS-04](#obs-04) | Add node/group Logs, Metrics and Placement inspectors | planned / not proven | GRP-02, GRP-03, OBS-02, OBS-03 |
| [OBS-05](#obs-05) | Add optional reviewed telemetry collectors and drains | planned / not proven | OBS-01, OBS-03 |
| [OBS-06](#obs-06) | Add scoped alerts and operational recovery links | planned / not proven | OBS-04, REL-02 |

## release

| ID | Task | State | Dependencies |
|---|---|---|---|
| [REL-EXT-01](#rel-ext-01) | Apply authorization/audit/version gates to new operations | planned / not proven | SBX-02, STO-02, OBS-01, GRP-03 |
| [REL-EXT-02](#rel-ext-02) | Qualify the full grouped-service journey | planned / not proven | EXM-02, SBX-05, STO-05, OBS-04, REL-EXT-01 |
| [REL-EXT-03](#rel-ext-03) | Close self-hosting upgrade, recovery and operations delivery | planned / not proven | REL-EXT-02, REL-03 |

## Acceptance details

### CAT-01

A justified ordinary project displays one generic Application component; missing/permission-reduced multi-service records remain unavailable; no inferred DB/queue/function/runtime/health

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd5y](https://app.clickup.com/t/z8xxqwcd5y). Historical status is context, not a29October remote verification.

### CAT-02

Confirmed aliases match the dedicated alias API; deployment URLs remain separate; no aliases copied from unrelated or former deployments

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd5z](https://app.clickup.com/t/z8xxqwcd5z). Historical status is context, not a29October remote verification.

### CAT-03

Every successfully synced selected project has one app and provider flow, correctly linked services and exact release history; rename/overlap/token replacement cannot duplicate it

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd60](https://app.clickup.com/t/z8xxqwcd60). Historical status is context, not a29October remote verification.

### CAT-04

Owner follows Apps → Services → flow → release → provider dashboard; both themes, narrow screens, keyboard access and role restrictions pass

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd61](https://app.clickup.com/t/z8xxqwcd61). Historical status is context, not a29October remote verification.

### CAT-05

Show connected account/team, selected versus discovered counts, last success, stale/partial warnings and pagination; new accessible projects are discoverable but not silently selected

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd62](https://app.clickup.com/t/z8xxqwcd62). Historical status is context, not a29October remote verification.

### AUTH-01

See the full adjacent milestone section and exit gate; no acceptance narrowed to this sentence.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd63](https://app.clickup.com/t/z8xxqwcd63). Historical status is context, not a29October remote verification.

### AUTH-02

See the full adjacent milestone section and exit gate; no acceptance narrowed to this sentence.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd64](https://app.clickup.com/t/z8xxqwcd64). Historical status is context, not a29October remote verification.

### AUTH-03

See the full adjacent milestone section and exit gate; no acceptance narrowed to this sentence.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd65](https://app.clickup.com/t/z8xxqwcd65). Historical status is context, not a29October remote verification.

### AUTH-04

See the full adjacent milestone section and exit gate; no acceptance narrowed to this sentence.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd66](https://app.clickup.com/t/z8xxqwcd66). Historical status is context, not a29October remote verification.

### GIT-01

Signed delivery → exact branch/tag → server-reviewed App → durable job → production build/digest → Docker readiness and service logs

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd67](https://app.clickup.com/t/z8xxqwcd67). Historical status is context, not a29October remote verification.

### GIT-02

Private checkout, repository identity, nested paths, webhook mode, same-name branch/tag, annotated tags, revocation and missed delivery behavior verified per provider

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd68](https://app.clickup.com/t/z8xxqwcd68). Historical status is context, not a29October remote verification.

### GIT-03

Rule pins repository, typed ref, pipeline version, target/environment, actor and review policy; edits cannot silently change an admitted run

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd69](https://app.clickup.com/t/z8xxqwcd69). Historical status is context, not a29October remote verification.

### GIT-04

Show accepted, ignored, duplicate, stale, rejected, review-required and failed events with reasons and links to intent/job/run

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd6a](https://app.clickup.com/t/z8xxqwcd6a). Historical status is context, not a29October remote verification.

### GIT-05

Validate replacement in secure Settings; preserve unchanged webhook keys; old authority is revoked; bounded recovery cannot duplicate jobs or deploy obsolete refs

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd6b](https://app.clickup.com/t/z8xxqwcd6b). Historical status is context, not a29October remote verification.

### SEC-01

Database name/service settings remain readable; passwords/tokens remain protected; a service receives only its explicitly bound values

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd6c](https://app.clickup.com/t/z8xxqwcd6c). Historical status is context, not a29October remote verification.

### SEC-02

Accepted runs resolve an immutable version or fail closed; unverifiable historic versions cannot authorize log replay

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd6d](https://app.clickup.com/t/z8xxqwcd6d). Historical status is context, not a29October remote verification.

### SEC-03

Inventory every encrypted record, retain original key recovery, tag ciphertext key versions, test interrupted/partial rotation and restore; online rotation follows a separately validated multi-key transition

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd6e](https://app.clickup.com/t/z8xxqwcd6e). Historical status is context, not a29October remote verification.

### SEC-04

An App cannot use an unrelated registry credential; accepted build/push plans pin authorized connection/version; runtime secrets are not arbitrary build inputs. Native pipelines also pin exact per-stage selections; credential/grant changes around durable admission cannot silently change accepted authority

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd6f](https://app.clickup.com/t/z8xxqwcd6f). Historical status is context, not a29October remote verification.

### SEC-05

Canary values never enter persisted logs, broadcasts, error responses, notifications, downloads or fixtures; streaming chunk splits are covered

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd6g](https://app.clickup.com/t/z8xxqwcd6g). Historical status is context, not a29October remote verification.

### LOG-01

Every result binds to a concrete container/run identity; reconnect cursor ordering, permissions, limits and retention work

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd6h](https://app.clickup.com/t/z8xxqwcd6h). Historical status is context, not a29October remote verification.

### LOG-02

Capture deployment/controller generation and pod UID/container; rotation cannot attach a session to an unrelated new pod; real-cluster verification precedes availability claims

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd6j](https://app.clickup.com/t/z8xxqwcd6j). Historical status is context, not a29October remote verification.

### REL-01

Worker loss before/during/after mutation and during result persistence cannot cause blind redeployment; stale writes remain fenced and uncertain outcomes visible

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd6k](https://app.clickup.com/t/z8xxqwcd6k). Historical status is context, not a29October remote verification.

### REL-02

Restart cannot double-fire a schedule; notifications expose their retry/status policy and do not determine deployment success

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd6m](https://app.clickup.com/t/z8xxqwcd6m). Historical status is context, not a29October remote verification.

### REL-03

Encrypted records, histories, operation IDs and log cursors survive the tested upgrade path; measure queue delay/acknowledgement/API latency before setting capacity claims

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd6n](https://app.clickup.com/t/z8xxqwcd6n). Historical status is context, not a29October remote verification.

### VRC-01

See the full adjacent milestone section and exit gate; no acceptance narrowed to this sentence.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd6p](https://app.clickup.com/t/z8xxqwcd6p). Historical status is context, not a29October remote verification.

### VRC-02

See the full adjacent milestone section and exit gate; no acceptance narrowed to this sentence.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd6q](https://app.clickup.com/t/z8xxqwcd6q). Historical status is context, not a29October remote verification.

### VRC-03

Exact retained account/project + reviewed public bundle → one durable operation → retained deployment ID → readiness and production alias; no duplicate project

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd6r](https://app.clickup.com/t/z8xxqwcd6r). Historical status is context, not a29October remote verification.

### VRC-04

Admin/operator review establishes source, environment, permitted actions, provider project identity and deployment owner; imported records remain inspection-only by default

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd6t](https://app.clickup.com/t/z8xxqwcd6t). Historical status is context, not a29October remote verification.

### VRC-05

Authored Cooker DAG references the binding and immutable Git revision or reviewed artifact; result links native run to provider deployment/catalog history

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd6u](https://app.clickup.com/t/z8xxqwcd6u). Historical status is context, not a29October remote verification.

### VRC-06

Expose build/runtime streams only for tested API permissions and retention; missing provider access remains unavailable; redact before capture/download

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd6v](https://app.clickup.com/t/z8xxqwcd6v). Historical status is context, not a29October remote verification.

### VRC-07

Pin exact deployment and current production before change, preserve operation/alias history, reconcile external changes and uncertain writes

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd6w](https://app.clickup.com/t/z8xxqwcd6w). Historical status is context, not a29October remote verification.

### VRC-08

Scope by project/environment/service where supported; list names/targets with protected values masked; review writes and rotation, never import provider environment values into catalog snapshots

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd6x](https://app.clickup.com/t/z8xxqwcd6x). Historical status is context, not a29October remote verification.

### LIFE-01

See the full adjacent milestone section and exit gate; no acceptance narrowed to this sentence.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd6y](https://app.clickup.com/t/z8xxqwcd6y). Historical status is context, not a29October remote verification.

### LIFE-02

See the full adjacent milestone section and exit gate; no acceptance narrowed to this sentence.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd6z](https://app.clickup.com/t/z8xxqwcd6z). Historical status is context, not a29October remote verification.

### LIFE-03

See the full adjacent milestone section and exit gate; no acceptance narrowed to this sentence.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd70](https://app.clickup.com/t/z8xxqwcd70). Historical status is context, not a29October remote verification.

### DOM-01

See the full adjacent milestone section and exit gate; no acceptance narrowed to this sentence.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd71](https://app.clickup.com/t/z8xxqwcd71). Historical status is context, not a29October remote verification.

### DOM-02

See the full adjacent milestone section and exit gate; no acceptance narrowed to this sentence.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd72](https://app.clickup.com/t/z8xxqwcd72). Historical status is context, not a29October remote verification.

### TUN-01

See the full adjacent milestone section and exit gate; no acceptance narrowed to this sentence.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd73](https://app.clickup.com/t/z8xxqwcd73). Historical status is context, not a29October remote verification.

### TUN-02

See the full adjacent milestone section and exit gate; no acceptance narrowed to this sentence.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd74](https://app.clickup.com/t/z8xxqwcd74). Historical status is context, not a29October remote verification.

### AWS-01

See the full adjacent milestone section and exit gate; no acceptance narrowed to this sentence.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd75](https://app.clickup.com/t/z8xxqwcd75). Historical status is context, not a29October remote verification.

### AWS-02

See the full adjacent milestone section and exit gate; no acceptance narrowed to this sentence.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd76](https://app.clickup.com/t/z8xxqwcd76). Historical status is context, not a29October remote verification.

### AWS-03

See the full adjacent milestone section and exit gate; no acceptance narrowed to this sentence.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd77](https://app.clickup.com/t/z8xxqwcd77). Historical status is context, not a29October remote verification.

### GCP-01

See the full adjacent milestone section and exit gate; no acceptance narrowed to this sentence.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd78](https://app.clickup.com/t/z8xxqwcd78). Historical status is context, not a29October remote verification.

### GCP-02

See the full adjacent milestone section and exit gate; no acceptance narrowed to this sentence.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd79](https://app.clickup.com/t/z8xxqwcd79). Historical status is context, not a29October remote verification.

### GCP-03

See the full adjacent milestone section and exit gate; no acceptance narrowed to this sentence.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd7a](https://app.clickup.com/t/z8xxqwcd7a). Historical status is context, not a29October remote verification.

### ECO-M1

Observe optional installation metadata and exact selected-project access, independent empty/denied/stale/unavailable states and safe persisted compatibility; prove authenticated router and actual frontend behavior. Installation access must not imply resource binding or health.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd7b](https://app.clickup.com/t/z8xxqwcd7b). Historical status is context, not a29October remote verification.

### ECO-M2-01

Qualify encrypted connection lifecycle, exact account/organization scope, selected discovery/resources and safe API/UI behavior. Current38+88sub package pass remains distinct from failed wrapper; outstanding SQL16 and HTTP6 need a newly qualified runner, not an unchanged retry. No live onboarding or SQL health inferred.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd7c](https://app.clickup.com/t/z8xxqwcd7c). Historical status is context, not a29October remote verification.

### ECO-M2-02

Implement versioned project/environment/resource bindings using exact provider IDs and authorized customer access; prove configuration and application data path with isolated resources. Current binding contract is proposal-only; metadata visibility does not satisfy this task.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

Retained task: [z8xxqwcd7d](https://app.clickup.com/t/z8xxqwcd7d). Historical status is context, not a29October remote verification.

### SHIP-01

Review only owned frontend/example/plan files, scan the staged diff for private material, create a signed-off feature commit and verify the exact remote branch SHA. Preserve unrelated backend work and target develop for later integration.

Owner: current session. Evidence: [DELIVERY.md](DELIVERY.md), [https://github.com/santapong/Cooker/tree/feature/cooker-release-plan-20261029](https://github.com/santapong/Cooker/tree/feature/cooker-release-plan-20261029). Full scope: proven for feature publication slice.

### SHIP-02

Bounded Chromium: moving pixels and route registration, pause/resume, offscreen and reduced-motion stop, no-WebGL/vendor/no-JS fallback, keyboard join explanations, all six responsive widths and actual screenshot review. Store source/cap/cleanup receipts.

Owner: current session. Evidence: [evidence/landing-verification.json](evidence/landing-verification.json). Full scope: proven for stated browser scope.

### SHIP-03

Exclude login studies, backups, test/controller inputs, local notes, credentials and source maps; preserve working docs/demo links, CSP and pinned OGL hashes; verify final bytes and the static envelope of100files,4MiB/file,8MiB total.

Owner: current session. Evidence: [evidence/landing-artifact.json](evidence/landing-artifact.json), [evidence/landing-verification.json](evidence/landing-verification.json). Full scope: proven for public export scope.

### SHIP-04

Deploy exact reviewed commit/artifact to prj_oe2lsOOWP5p5AAafHtkjKsW77d6a in team_4xUt8aSlfT696WCBgbsjtJTV. Read back target, deployment ID/source and bytes/behavior, retain rollback deployment. Preserve existing protection; any protection/domain change requires its own explicit decision.

Owner: current session. Evidence: [evidence/vercel-landing.json](evidence/vercel-landing.json). Full scope: proven for production publication.

### EXM-01

One dependency-free Function and page, no secrets/data resources. Verify /api/health, no-store and structured runtime event against exact deployment. Record connector/direct deployment honestly; do not call it Cooker-managed.

Owner: current session. Evidence: [evidence/vercel-example.json](evidence/vercel-example.json), [evidence/public-access.json](evidence/public-access.json). Full scope: proven for direct provider slice.

### EXM-02

Run an available qualified Cooker instance, use its own authorized saved connection and immutable source/artifact binding, dispatch through durable worker, observe provider receipt and resulting health/logs. Connector team consent cannot be exported as a Cooker credential. No CLI/script bypass counts as native acceptance.

Owner: current_session. Evidence: not yet accepted for the full criterion.

### INT-01

Freeze the current backend candidate, review all pending changes and migration/worker compatibility, qualify scoped tests and stopped SQL/HTTP runner replacements, then integrate via develop. Preserve700-path pending work until reviewed; do not bulk-commit it with this landing release.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

### INT-02

Review reconstructed B3 geometry against the selected source; export clean small/reverse icons and apply to actual app/docs/login surfaces as authorized. Preserve charcoal/ember and existing auth/DAG semantics; prove real router/browser behavior, not only standalone examples.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

### GRP-01

Schema binds exact provider/account/team/project/environment/region/service/deployment IDs, run/stage/artifact provenance and observation clocks. Distinguish configured intent from runtime receipt and stale/unavailable/unsupported. Versioned reads/writes preserve old records.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

### GRP-02

Environment→destination→service grouping with stable IDs, separate branch/join ports, crossing/condition explanations, collapse/expand preserving external edges and keyboard step list. Check small/medium/large graphs, target collisions and phone navigation. Existing Stage.Group is only a starting point.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

### GRP-03

Successful dispatch records exact deployment/resource identity; retries/updates cannot silently rebind historical runs. Live observation has its own timestamp/coverage, and externally changed/deleted resources show drift. Resolve all actions from identity, never display name.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

### GRP-04

Validate service/target compatibility before dispatch. Current one-compute-target Compose limitation remains visible until a separately tested planner supports more. Read/refresh and any promote/rollback/restart actions require their actual scoped permissions and preconditions.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

### SBX-01

Distinguish offered, entitled, configured and verified Sandbox capability; inspect exact account/region/API and budgets without provisioning. Persist unsupported/denied/unknown distinctly. Prove current API/version contracts and safe request limits before execution.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

### SBX-02

Pin source and managed/custom image identity, create a bounded run, stream command outcomes, enforce deadline/cancel and reconcile uncertain create replies. Record sandbox/session IDs and actual exit status. Sandbox execution is not production deployment.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

### SBX-03

Default untrusted execution to deny-all or narrow declared egress; use supported credential brokering so secrets do not enter untrusted code. Test allowed/denied destinations, redaction, revocation and no ambient control-plane/Docker access. Do not label VM allocation as every per-process quota.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

### SBX-04

Track filesystem snapshots separately from running process state; version snapshot identity/retention and invoke reviewed startup on resume. Prove stop→resume file continuity, missing/expired snapshot and incompatible source/image behavior without replaying external deployment.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

### SBX-05

Actually observe timeout/cancel/provider-failure cleanup, retained/deleted state, permissions and budget stops. Use a reviewed disposable fixture and explicit account spend allowance before a live sandbox/drive run; no indefinite public port or auto-renewing session by default.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

### STO-01

Separate Function scratch /tmp, object/database storage, Docker named volumes, Kubernetes PVCs, Sandbox filesystem snapshots and Sandbox Drives. Current official Sandbox persistence/Drive offering is not Cooker support; ordinary Functions must not be offered as persistent-disk hosts.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

### STO-02

Typed exact resource owner/region/mount/mode/capacity/retention binding and versioned references; reject host-root paths, traversal, unsupported mounts and cross-tenant substitutions. Display configured and observed mounts independently.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

### STO-03

Enforce provider-supported single-writer/read-only-snapshot rules, same-region requirements and attachment leases. Test concurrent claims, stale versions, disconnect/revoke, old-schema readers and no silent data recreation.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

### STO-04

Use checksummed synthetic data across stop/resume/redeploy and reviewed snapshot/backup restore. Record actual data hashes, schema/image identities, failure and recovery. A mounted path or snapshot ID alone does not establish durability.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

### STO-05

Stopping a service preserves durable data unless a separately reviewed destructive action says otherwise. Audit retain/delete policy, unknown deletion outcome, recovery window and provider cleanup; test no orphan leakage or cross-service deletion.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

### OBS-01

Model runtime logs, build/stage logs, metrics and traces separately, bound to exact owner/resource/deployment/replica/time window. Unknown, unsupported, stale, empty and failed states differ. No health derived from deployment READY alone.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

### OBS-02

Use provider-supported adapters with cursors/time/line/byte limits, replica identity, cancellation, redaction and scoped retention. Prevent account/replica/cross-deployment leakage; keep pipeline logs separate from app runtime logs and exact snapshots immutable.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

### OBS-03

Show actual request/error/duration and provider-exposed CPU/RAM/restart data only when available. Bound labels/cardinality/range/resolution/page output and downsample. Account-plan/retention limits remain visible; absent measurements are not zero or healthy.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

### OBS-04

Selecting a node/group opens deployment-scoped tabs with timestamp/coverage, filters and copyable safe identity. Correct empty/loading/denied/stale/error behavior, keyboard access, mobile disclosure and exact selected-service/time semantics. No fake live charts.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

### OBS-05

Reuse configured OpenTelemetry/Prometheus or supported Vercel drains where appropriate; separately scope ingestion/auth/tenant labels/retention/cost and prove a real event/request traverses the selected path. No automatic second monitoring stack or broad log export.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

### OBS-06

Define threshold/window/missing-data and dedup/recovery behavior; configure notifications only with explicit recipients/channel authority. Link alert to exact run/service/log window and recovery action; test ordinary and unavailable-state cases.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

### REL-EXT-01

Real router tests for roles/tenant/resource ownership, conditional versions and configured MFA/step-up; audit sandbox/storage/provider mutations without secrets. Frontend buttons or provider connection badges cannot authorize actions.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

### REL-EXT-02

On coherent frozen inputs: source→parallel tasks→review→deployment→correct node group→actual scoped logs/metrics→sandbox persistence→cancel/restart/restore/rollback. Retain failures and prove owned cleanup; no full acceptance from unrelated passing slices.

Owner: unassigned. Evidence: not yet accepted for the full criterion.

### REL-EXT-03

Fresh install and supported old-schema upgrade/restore with operator docs, private/public access configuration, resource ceilings, monitoring/recovery and rehearsed rollback. Run artifact-matched dependency/risk review and required CI before main/tag/release; public landing is a separate deliverable.

Owner: unassigned. Evidence: not yet accepted for the full criterion.
