# Cooker — Vercel ecosystem delivery plan

**Current checkpoint — 27 October 2026:** secure Neon connection, discovery, resource, HTTP and persistence source is prepared. The current default package passed **38 top-level tests plus 88 subtests**, with zero failures/skips; its wrapper failed afterward. SQL16 and HTTP6 remain **NOT RUN**: finite campaigns stopped on temporary-container lifecycle and disk-counter freshness guards, respectively. All owned work/staging was independently cleaned and the shared lane released. The existing Neon ClickUp task is freshly verified **Blocked** with original protected fields retained. See [verification](VERIFICATION.md), [resource tracking](RESOURCE-TRACKING.md) and [backend/frontend ownership](BACKEND-FRONTEND-HANDOFF.md). This is a runner-acceptance blocker despite Pi RAM headroom; waiting for RAM alone will not resolve it. All full product/live/release criteria remain open; resource automation stays PAUSED.

Planning date: **25 October 2026, Asia/Bangkok**. This plan extends the remaining delivery order; it does not certify implementation, account access, resources, deployment or release.

**26 October evidence update:** the isolated Neon metadata fixture package passed nine top-level tests and twelve subtests, with zero failures/skips, on ARM64 Go 1.25.14 (`CGO_ENABLED=0`, no race run). [Outcome and exact scope](/mnt/data/cooker-ecosystem-20261026/OUTCOME.json). This supersedes the earlier Neon-fixture NOT RUN checkpoint only; M1/Vercel, SQL, frontend/browser, live Neon onboarding/bindings and release acceptance remain pending. All 48 full product-task completions remain open/`not_proven`.

The next useful outcome is truthful visibility of installed Vercel integrations and their allowed access to selected projects. Then qualify Neon resources and explicit bindings, adopt the existing landing project, and add controlled operations. Catalog the wider ecosystem without provisioning every product.

## Authority, sources and current position

- The [Cooker ClickUp board](https://app.clickup.com/90181387221/v/l/li/1100540000004771) is now the canonical product task record, using the retained [Plan] Cooker project ID. On 25 October the owner selected Primary and approved the Cooker list and 48 roadmap titles. Persisted readback verified all 45 legacy packages plus M1 and two Neon slices. [TASKS.json](TASKS.json) retains local source/evidence and the remote-ID mapping; [TASKS.md](TASKS.md) is its generated handoff view. These local records are mirrors, not a second product backlog. Root owns integration and final acceptance.
- Preserve the [remaining delivery plan](../2026-10-25-remaining-delivery-plan.md), [45-package technical plan](../2026-10-02-consolidated-feature-roadmap.md) and [requirement ledger](/mnt/data/cooker-roadmap-20261002/roadmap-audit/requirements.json): all **45 packages, 20 gates and 12 invariants** retain their original acceptance. Their recorded completion remains `not_proven` until reconciled evidence changes it.
- Research inputs: [Marketplace](/mnt/data/cooker-ecosystem-20261025/research/01-marketplace.md), [Neon](/mnt/data/cooker-ecosystem-20261025/research/02-neon.md), [source seams](/mnt/data/cooker-ecosystem-20261025/research/03-code-seams.md), [data providers](/mnt/data/cooker-ecosystem-20261025/research/04-data-providers.md), [platform tools](/mnt/data/cooker-ecosystem-20261025/research/05-platform-tools.md), [delivery policy](/mnt/data/cooker-ecosystem-20261025/research/06-delivery-policy.md).
- The current source checkpoint includes M1 installation visibility, M3-A explicit landing adoption and [existing-project update review preparation](M3-UPDATE-REVIEW-CHECKPOINT.md). The review pins the server artifact, exact adoption/connection versions, observed production deployment and aliases, with ten-minute expiry and derived `submissionAvailable:false`. It has no submission or job path. M3-B deployment submission remains absent. Formatting and syntax checks are limited source evidence; M1/Vercel behavioral checks, semantic frontend types, builds, SQL, browser and current-account acceptance remain unproven. The separate 26 October Neon fixture pass does not clear those gates.
- All **48 approved product tasks remain open**, with full completion `not_proven`; completed agent reports or a focused test pass do not close them. The 25 October confirmed [VRC-04](https://app.clickup.com/t/z8xxqwcd6t) stage was **Blocked**, and that day's connector quota interrupted progress updates. Fresh26October readback verified the existing Neon andVRC-04 tasks Blocked for remaining full criteria, with original criteria, names, owners, dates, estimates and dependencies retained. [Scoped board proof](/mnt/data/cooker-ecosystem-20261026/CLICKUP-VERIFICATION.json). Only these two tasks were rechecked; this run closes no product task. The chat resource automation is **paused** and must not reactivate without a new explicit request.
- Provisioning, billing, credentials, DNS and deployment actions require their actual scoped authority and inputs. A plan or provider capability does not grant that authority. This documentation task performs none of them.

## Architecture and evidence contract

| Plane | Cooker responsibility | Boundary |
| --- | --- | --- |
| Native Vercel platform | Project/deployment/configuration/log/resource adapters with exact owner, project and environment identity. | Hosting, Functions, Blob and Global Config have product-specific controls; no universal integration inventory. |
| Marketplace | Definitions → installations → provider accounts → resource instances → explicit project/environment bindings. | A native Marketplace integration can use Vercel billing while remaining a third-party service. Installation access is not resource use. |
| Vercel Connect | Connector, subject, installation, project/environment link and granted runtime API authority. | Separate credential-broker plane; neither app login nor Marketplace resource provisioning. [Connect](https://vercel.com/docs/connect) |
| Application/SDK | Framework configuration, schema/migrations, authorization, instrumentation and actual application calls. | Injected keys or installed SDKs do not prove wiring, correct behavior or captured telemetry. |

Use `IntegrationDefinition`, `Installation`, `ProviderAccount`, `ProjectAccess`, `ResourceInstance`, `ResourceBinding`, `PreviewInstance`, `CredentialCapability`, `HealthObservation` and `BillingObservation` as proposed adapter concepts. Only M1 installation/access DTOs are frozen in [M1-CONTRACT.md](M1-CONTRACT.md); future concepts need a reviewed schema before implementation.

Every observation carries source, scope, successful observation time, attempt time and coverage. Preserve unsupported, not observed, requires provider authorization, denied, incomplete, stale and failed states. An authoritative empty result differs from an unavailable read. Store allowlisted metadata and credential references; exclude raw secrets, URLs containing credentials, arbitrary provider bodies and secret-bearing logs.

Readiness is evidence per capability, not one connection badge: **offered → installed → provisioned → bound → application-configured → verified**. Offered means catalog availability; installed means observed relationship; provisioned means an identified instance; bound means evidenced project/environment association; application-configured means required code/settings; verified means a dated, scoped behavior check. Paths may skip installation for manually configured services. Failure/revocation can invalidate later states without erasing historical evidence.

## Milestones and exit evidence

| Milestone | Deliverable and dependencies | Done evidence / limits |
| --- | --- | --- |
| M0 — coherent inputs | Reconcile current candidate/diff, versions, owners and changed contracts before the next check; preserve unrelated pending work. | Exact input/source manifest, reused evidence scope, finite next-check budget and conflicting contracts resolved. A full release freeze is distinct from an edit checkpoint. |
| M1 — installation access | M0 + research → optional sync → catalog projection → workspace. Reuse selected account/team and existing read routes. | [Frozen contract](M1-CONTRACT.md): installed integration metadata, **Allowed project access**, independent freshness, explicit empty/denied/stale/unavailable, selection/tenant fences, safe persistence and old-snapshot compatibility. Local fixtures/builds plus a separately labeled authorized live read establish their respective scopes. No resources, env endpoints, health, executable services or provisioning. |
| M2 — Neon resources and bindings | M1 identity + a documented supported customer resource interface and established caller authority. Park enumeration if authority/interface remains unknown. | Exact provider/resource IDs, ownership/billing mode, explicit project/environment binding and source/time. Separately authorized Neon metadata can qualify organization/project/branch/database/compute reads; SQL verification needs separate database credentials and budget. No synthetic binding from installation access. |
| M3 — adopt and update landing | M0 execution/security/fencing prerequisites + explicit existing-project adoption/operation contract. M2 supplies any needed data binding; a static landing need not await unused database work or unrelated Kubernetes gates. | Same Vercel project/account/address; reviewed seven-file landing allowlist including icon; immutable accepted artifact, durable operation/deployment receipt, observed result and uncertain-write reconciliation. No duplicate project creation. Choose Vercel Git or Cooker as automatic owner per environment. |
| M4 — independent Vercel controls | M3 binding/operation contract → native DAG stage, then separately scoped logs, lifecycle, env/configuration editing and onboarding. | Each enabled action proves preconditions, authority/version/revocation, durable history and failure/recovery on its target. OAuth/callback/selection retains full original acceptance even when an operator token works. Unsupported controls remain unavailable. |
| M5 — demand-driven ecosystem | Reuse qualified contracts: Upstash next, native Blob/Global Config when needed, then other data/auth/CMS/telemetry/AI tools. Continue retained lifecycle/domains/tunnels and AWS/GCP lanes. | One concrete capability at a time: inventory/binding, required application work, scoped verification and authorized cleanup. Provider availability or a Marketplace listing is not Cooker implementation or live acceptance. |

M1 uses one bounded `GET /v1/integrations/configurations?view=account` in the stored team context. The documented result is an array without pagination; omitted `projects` grants all-project access, while `[]` lists none. Neither proves a database connection. Follow the contract's 15-second/1 MiB/200-row limits, no retries, existing cooldown and lease/version fences. Exceeding a local cap means incomplete coverage. [Official configuration API](https://vercel.com/docs/rest-api/integrations/get-configurations-for-the-authenticated-user-or-team)

The Marketplace resource API is documented for integration-provider credentials; general customer PAT support is unqualified here. Do not call it solely because its route exists. M1 excludes even environment-name reads; a later env-metadata adapter must explicitly redact responses because requesting no decryption alone is insufficient. [Marketplace API authentication](https://vercel.com/docs/integrations/create-integration/marketplace-api)

For Neon, control-plane API credentials and PostgreSQL credentials are separate from Vercel access. Project keys are Editor-capable; organization keys carry broad administration. Do not market a GET-only client as a read-only credential. Upstash Developer API uses email/API-key Basic authentication and excludes Vercel/Fly.io-created accounts; Redis/Vector/QStash tokens are distinct data/product authority. [Neon key scopes](https://neon.com/docs/manage/api-keys), [Upstash limitation](https://upstash.com/docs/devops/developer-api/introduction), [authentication](https://upstash.com/docs/devops/developer-api/authentication)

Before a data-backed preview, record its exact branch/database/deployment, production separation, credential scope, lifecycle and cleanup owner. Environment labels alone do not prove isolation. Neon-managed and Vercel-managed preview cleanup differ; deployment-specific variables may be absent from project settings. Record billing owner (Vercel/provider/unknown), account mode, plan/region, metering period and observed versus estimated spend. [Neon integration comparison](https://neon.com/docs/guides/vercel-overview)

## Fifteen focused packages, small active waves

The user requested fifteen distinct focused agents. Run them as bounded packages, not fifteen concurrent writers: runtime maximum **root + three workers**; start implementation with two writers and pull UI when a slot clears. Keep one main implementation slice and one heavy execution phase active. Inherit the authorized model/effort; exact inherited model ID, tokens, money and productivity improvement are unknown unless exposed and measured.

| Packages | Outcome / dependencies | Source responsibility |
| --- | --- | --- |
| ECO-01, ECO-02, ECO-03 | Independent Marketplace, Neon and code-seam evidence. | Assigned research reports only; root resolves external-contract uncertainty. |
| ECO-04, ECO-05, ECO-06 | Data providers, platform coverage, delivery/test policy after required research. | Assigned reports only; research Done is not product Done. |
| ECO-07 | Optional installation sync after first three reports. | Vercel provider production files listed in the frozen contract. |
| ECO-08 | Catalog projection consuming ECO-07 contract. | Catalog production files only; no provider edits. |
| ECO-09 | Workspace consuming ECO-07/ECO-08. | Frontend types/API and read-only presentation; retain accepted Cooker identity. |
| ECO-10, ECO-11 | Focused backend/frontend regressions after coherent source. | Assigned test files/check receipts; root owns execution budget and batch timing. |
| ECO-12 | Optimized roadmap and capability map after ECO-04–06. | This PLAN.md and CAPABILITIES.md only. |
| ECO-13, ECO-14 | Independent privacy/authority and compatibility review after changed-source evidence. | Findings only; root assigns any repair to its source owner. |
| ECO-15 | Integrated acceptance/handoff after UI, checks, docs and reviews. | Findings/receipt; root accepts or retains pending gates. |

One writer owns each file at a time. Root serializes shared contracts, board updates and integration; workers return paths, source identity, checks actually performed and remaining limits. Budget: one attempt plus one diagnosis-driven revision, then re-scope/escalate; no unchanged failing retries. A textual budget is not an enforced cost cap.

Follow the board's workflow: `ready → in_progress → review → locally_verified → live_acceptance_pending → done`, with `blocked_external` for missing inputs. Implementation, local verification, live acceptance and release remain separate evidence. Root must inspect artifacts and acceptance before Done; no worker self-report alone closes a package or legacy gate.

## Risk-based verification after coherent edits

| Changed risk | Smallest meaningful batch | When to broaden |
| --- | --- | --- |
| Documentation only | Claims, links, contract consistency and 45-ID coverage review; no application reruns. | Prose reveals a behavior/authority change requiring implementation. |
| Pure presentation | Affected static/type checks and representative omitted/empty/stale/denied fixtures. | Shared navigation, interaction, accessibility or responsive behavior changes need affected browser checks. |
| Provider parsing/bounds | HTTP fixtures for exact scope/array/access semantics, unknown values, malformed/empty, caps/cancel and 401/403/429; synthetic secret canaries. | A new account permission assumption requires a separately authorized bounded live read. |
| Sync/catalog/persistence state | Focused clocks, authoritative empty, dedup, selected-only views, clone isolation, cooldown and lease/config/revocation fences. | Changed persistence compatibility requires a disposable database/restart/old-JSON batch; skipped database checks remain pending. |
| Security/authority, even a small diff | Targeted tenant/role denial, redaction, credential/version and serialized-response checks. | New writes need exact preconditions, revoked/changed scope, uncertain outcome reconciliation and a scoped real journey. |
| Integrated milestone | One affected backend batch, one frontend type/unit/build batch and one relevant authenticated end-to-end journey where inputs/capacity permit. | Major integration/release, migrations or shared auth/execution risk justify full applicable suites and original gate campaigns. |

Design checks during implementation; execute after the cross-layer batch is coherent. Reuse unchanged matching-scope evidence. Rerun affected checks only for changed code/tests/inputs, failure or unresolved risk; full suites belong at major release gates. Do not equate fewer reruns with measured savings or weaker acceptance.

Before heavy execution, root uses service-test-budget/resource-aware guidance, checks live capacity, declares and verifies aggregate CPU/RAM/PID/time/disk limits, cancellation and owned cleanup. Workers do not independently launch builds, browsers, services, account calls or database stacks. Missing authority/capacity leaves the dependent check pending.

## Stable legacy scope coverage

This delivery overlay changes order only. Every ID below retains its complete original acceptance, associated 20 gates and 12 invariants from the linked ledger; no row is a completion claim. Shared security/recovery gates apply where a milestone exercises their risk, without forcing unrelated Kubernetes/cloud acceptance ahead of the first safe Vercel update.

| Stable package IDs (45 total) | Retained scope | Delivery lane |
| --- | --- | --- |
| CAT-01, CAT-02, CAT-03, CAT-04, CAT-05 | Truthful app/service signals, production addresses, selected coverage, cross-view acceptance and freshness. | M1 supports catalog visibility; original full catalog acceptance remains. |
| AUTH-01, AUTH-02, AUTH-03, AUTH-04 | Real GitHub/Google registration, broker configuration, round trips and activation/rollback. | Parallel input-dependent auth lane; separate from Git/hosting authorization. |
| GIT-01, GIT-02, GIT-03, GIT-04, GIT-05 | Hosted GitHub/GitLab, Gitea automation, durable feedback, bounded missed-event repair and revocation. | Retained native Git/reliability lane. |
| SEC-01, SEC-02, SEC-03, SEC-04, SEC-05 | Settings/secret bindings, external immutable versions, key recovery, registry authority and every redaction boundary. | Relevant gates before affected M3–M5 writes; full acceptance retained. |
| LOG-01, LOG-02 | Docker logs plus immutable Kubernetes identity/session and disposable-cluster acceptance. | Native reliability lane; Kubernetes remains a separate explicit gate. |
| REL-01, REL-02, REL-03 | Worker uncertainty/fencing, scheduler/notification recovery and executable/schema rollback/restore. | Shared affected-action gates plus retained reliability campaigns. |
| VRC-01, VRC-02, VRC-03, VRC-04, VRC-05, VRC-06, VRC-07, VRC-08 | Onboarding/events, existing-project update/adoption, authored stage, logs, promotion/rollback and protected configuration editing. | M3 adoption/update; M4 independently accepted controls and onboarding. |
| LIFE-01, LIFE-02, LIFE-03 | Durable promotion, exact prior-good application rollback and supported observed traffic controls. | M4 Vercel actions and retained cross-provider lifecycle lane. |
| DOM-01, DOM-02 | Exact domain/release binding, ownership/DNS/TLS and collision-safe operator subdomains. | M5 retained addresses lane; external DNS remains valid. |
| TUN-01, TUN-02 | Authorized persistent Cloudflare routing/recovery and temporary-address expiry/cleanup. | M5 retained tunnel lane; no implicit database/admin exposure. |
| AWS-01, AWS-02, AWS-03 | Selected ECS catalog, exact ECR/task/service/log/readiness receipts and prior-good rollback. | Retained ECS/Fargate slice after its shared contract qualifies. |
| GCP-01, GCP-02, GCP-03 | Selected Cloud Run catalog, operation/revision/IAM/log readiness and observed traffic rollback. | Retained Cloud Run slice after ECS/shared-contract learning. |

Release closure requires exact candidate identity, relevant checks actually run, provider/UI/persistence evidence, operator/user docs, limits and recovery inputs. This plan alone closes no live gate and creates no resource.

## Current remaining work, grouped by outcome

The existing ClickUp tasks retain their IDs, dependencies and complete acceptance. These groups explain the next outcomes; they are not another backlog.

1. **Verify the coherent source.** Run the affected backend/frontend batch and actual migration/persistence checks for adoption and review receipts, then role/theme/keyboard/navigation acceptance. Confirm current selected-account reads separately. This comes first because the new authority and freshness contracts must work before a deployment action can depend on them. [Verification runbook](VERIFICATION.md)
2. **Update the same landing project safely.** Add a distinct update operation and queue kind, durable version/artifact/baseline admission, one provider submission, retained deployment identity and uncertain-write reconciliation. The prepared review cannot activate this path. Prove the same project/address and reviewed public artifact in a separately scoped actual update journey. A static landing does not wait for unused Neon work or unrelated Kubernetes acceptance.
3. **Connect confirmed data resources.** The inactive Neon metadata package now has a scoped ARM64 compile/fixture pass. Race coverage, supported account authority, secure onboarding, persistence, catalog presentation and explicit project/environment bindings remain. No live resource instance, binding or SQL health is inferred from that pass or from an installation.
4. **Finish the retained core and Vercel controls.** Continue hosted Git provider campaigns, authored DAG/recovery/secret/log gates, GitHub/Google sign-in, Vercel authorization/events, the authored Vercel stage and separately accepted logs/configuration/promotion/rollback. Keep each action's authority and acceptance independent.
5. **Expand after those contracts qualify.** Finish domain/TLS and tunnel behavior, then bounded ECS/Fargate and Cloud Run campaigns; select Upstash, Blob/Global Config and other ecosystem capabilities against an actual need. Broad provider provisioning remains later scope.
