# Cooker backend plan — dependable Git-to-deploy

**Decision date:** 28 September 2026 · **Audience:** self-hosted users and small teams · **Status:** authorized for implementation; local candidate in progress, full roadmap unfinished.

**Product promise:** connect a repository, choose a branch or release tag, supply configuration, and receive a working deployment with a clear explanation of what happened. Preserve the accepted Cooker workspace and DAG design while connecting it to real backend behavior.

This audit inspected the clean local `develop` checkout at `949d4a9`. It did not fetch remote state, run a new deployment, or repeat the full test suite. Source implementation and historical tests are distinct from live acceptance. The repository still marks human and live GitHub/cloud acceptance pending. The separate landing/login/workspace prototype is an accepted design reference, not a connected backend client.

Implementation progress and evidence: [current record](2026-09-28-backend-implementation.md). The audit below describes the pre-implementation baseline.

## 1. Answers to the five feature questions

| Requested capability | What exists in current code | What is missing or incomplete |
|---|---|---|
| GitHub, Gitea, GitLab and other webhooks; branch/tag auto-deploy | GitHub, GitLab, Gitea and Bitbucket Server/DC handlers; request authentication; rate limits and delivery-id idempotency middleware; exact repository + branch lookup; enabled App auto-deploy launches the App execution path | Checkout still assumes GitHub. No complete provider-neutral source connection, tag rules, branch patterns or trigger fan-out. The webhook commit is returned for diagnosis but does not pin execution. Reviewed Compose plans with a commit explicitly reject auto-deploy. These handlers do not trigger authored pipelines |
| Ordinary configuration and hidden credentials | `PlainVars` and encrypted `Secrets` are separate. Normal responses expose secret names, not values. Restricted secret write/reveal endpoints, database encryption and external secret-manager adapters exist | Complete secret editing experience, service-specific binding and versioned execution snapshots need work. Raw runtime/build output is not comprehensively scrubbed. Hidden UI values alone are not an end-to-end secrecy guarantee |
| Logs per service | Stage logs and WebSockets; Docker container and Kubernetes pod service-log streaming; final stage logs stored with run records | ECS CloudWatch and Cloud Run Cloud Logging streams explicitly return unavailable. Stage reconnect buffer is memory-only. No universal durable runtime-log archive or reliable cross-restart replay |
| Vercel, AWS and GCP integration | App deployment supports local Docker/Compose, Kubernetes, configured ECS/Fargate and Cloud Run subsets. AWS/GCP inventory and secret integrations exist | Vercel is not an App deployment target; existing Vercel guides host Cooker itself. Existing cloud targets need end-to-end logs, readiness, URL and recovery acceptance. Infrastructure provisioning is outside the current App flow |
| Domains or tunnel-provided addresses | Configured base-domain routing creates Docker Traefik labels or Kubernetes Ingress rules; local port fallback exists | No complete domain ownership, DNS, certificate lifecycle or tunnel-manager workflow. A self-hosted operator supplies DNS/TLS/network access today. Cooker does not currently supply a globally hosted domain service |

**Interpretation:** most areas have useful code foundations. None should be presented as a fully accepted, provider-independent product solely because its handler or adapter exists.

## 2. What to build first, and why

Build one trustworthy deployment path before adding more destinations. Automatic triggers amplify mistakes: a duplicated event, changed branch head, lost job, missing logs or leaked password becomes an unattended incident. Durable execution, exact source identity, protected configuration and useful logs are therefore part of the first release.

| Order | Milestone and user outcome | Prerequisite | Why this position |
|---|---|---|---|
| M0 | **Verified baseline and truthful capabilities.** Know exactly which path can deploy today | None | Prevents planning around disconnected adapters, demo responses or outdated documentation |
| M1 | **Recoverable manual deployment.** A pinned revision runs through durable jobs, isolated builds, protected configuration and usable service logs | M0 | Every later trigger and provider needs this execution contract |
| M2 | **Reliable GitHub auto-deploy.** Branch/tag rules deploy the intended revision and show why an event ran or was ignored | M1 | Delivers the selected primary outcome using the source integration closest to complete |
| M3 | **Complete GitLab and Gitea sources.** Same experience, including self-hosted instances | M2 contracts | Reuses a proven trigger engine instead of duplicating execution per provider |
| M4 | **Usable public addresses.** Operator subdomains, custom domains, TLS and managed tunnels | M1; M2 baseline release | Removes access/setup friction once deploys are trustworthy; independent work can start after M1 |
| M5 | **Complete AWS/GCP deployment operations.** Existing targets have logs, health, URL and recovery support | M1–M2; relevant M4 domain contract | Finishes targets already present before claiming a broad cloud ecosystem |
| M6 | **Vercel deployment destination.** Native web deployments through the same run history and policies | M1–M2 and provider capability contract | Requires a different artifact model; should not distort the Docker path prematurely |

M0–M2 form the first backend release. M3 and M4 form the next usability release. M5 and M6 are independent provider releases once core contracts stabilize. This is an order of acceptance gates, not a claim that all engineering must run serially. No calendar dates are promised before M0 measures the actual integration work.

**A public webhook URL is needed early.** M0 uses an operator-supplied HTTPS address or existing stable tunnel for the Cooker receiver. That setup does not depend on shipping the full M4 domain-management product.

## 3. Architecture direction

Keep the Go modular backend, React client, PostgreSQL and the existing DAG executor. Extend the PostgreSQL job queue for App deployments. Keep Redis optional for WebSocket/ticket/rate coordination; do not add a second job queue.

The proposed flow is:

`Verified event → trigger decision → immutable deployment plan → durable job → isolated build → DAG deployment → readiness result → URL + redacted logs`

Manual deploy and webhook deploy enter the same planner and execution service. Authored pipelines keep their own authoring model but share run, job, authorization, cancellation and evidence contracts. External cloud targets remain adapters; they do not each own a separate deployment history system.

Separate three responsibilities:

1. **Control:** authenticate people/events, store configuration, decide what may run, and record outcomes.
2. **Execution:** fetch the selected revision, build artifacts, execute permitted DAG stages, and report progress. Run repository-controlled work in an isolated execution boundary with bounded credentials and resources.
3. **Runtime:** Docker, Kubernetes or a provider runs deployed applications. Its service state is reconciled into Cooker; submitting a request does not by itself mean the application is healthy.

The six architecture views describe **current code**. This section and the milestones describe **proposed changes**. They must not be confused.

## 4. M0 — establish a verified baseline

### Work

- Publish an explicit capability table for each combination of source, build mode, destination, logs, URL, rollback and health. Derive advertised capability from configured and tested adapters, not just a registered target name.
- Trace one tiny GitHub source app and one image-only Compose application through inspect → review → save → deploy → service logs → terminal status. Saving configuration remains separate from deployment.
- Exercise the current n8n fixture as an additional multi-service acceptance case after the tiny app. Its prior standalone Docker success is useful evidence, but is not proof that Cooker deployed it.
- Reconcile the App source-build conflict: `app_execution.go` requires Docker builder/pusher while production validation forbids both. Write the isolated build-context/artifact contract and select the first compatible implementation for M1. Do not weaken production validation.
- Verify known DAG validation concerns using unknown endpoints, a valid DAG, self-loops, real cycles and mixed invalid edges. Fix confirmed defects inside this release scope.
- Mark unsupported UI actions honestly. Verify promotion semantics: an approval record must not be represented as a newly deployed environment without an actual deployment result. Preserve compatibility while correcting this behavior in M1.
- Record baseline tests, migrations, installation prerequisites, supported versions and remaining live acceptance. Keep design examples explicitly labeled until connected.

### Exit gate

A reproducible local acceptance report identifies which flows actually ran, which are simulated and which remain blocked. There is a reviewed design for production-compatible source builds and no unsupported target presented as ready. M0 does not require a cloud account.

## 5. M1 — make deployment execution dependable

### Durable work and recovery

Add an `app_deploy` job type to the existing PostgreSQL queue. Persist the run, immutable plan reference and job atomically, or use a transactional outbox when crossing store boundaries. Return an accepted response only after durable work exists. If PostgreSQL is unavailable, fail the request and allow retry; do not acknowledge a job that was never saved.

Use one active deployment lease per App/environment, with heartbeat, expiry and fencing against stale workers. Save provider deployment identifiers before advancing stages. On restart, reconcile an in-flight provider operation before retrying it. Delivery is at least once; the objective is one logical deployment intent, not an unsupported promise of exactly-once external side effects.

Cancellation stops new stages, propagates cancellation to supported running operations and preserves partial outcomes. Timeouts, retry ceilings and backoff are explicit. Network reads and idempotent reconciliation can retry; database migrations, arbitrary custom steps and uncertain external mutations cannot be blindly rerun. Ambiguous outcomes become **Needs attention**, with the last known provider state.

Expose queued, planning, building, awaiting approval, deploying, checking health, succeeded, failed, cancelled and needs-attention as understandable phases mapped to the existing run/stage state model. Do not introduce a competing state machine just for auto-deploy.

### Immutable source and build handoff

Every attempt records the full source commit, reviewed configuration version, plan hash, target identity and resulting image digest. A queued run is unaffected by later edits to the App or movement of the branch. A retry uses the same plan; “deploy latest” creates a new plan.

Use a versioned build context or an isolated checkout at the exact SHA. Bind inspection, selected Compose files and the build to the same source snapshot. Integrate one production-compatible builder and publisher through an explicit artifact handoff; favor the already supported BuildKit/Crane direction only after confirming its context and credential contracts in M0. A remote or Kubernetes executor may be required for the chosen self-hosted topology. Repository code must not inherit the control-plane database credentials or unrestricted host socket.

Image-only Compose follows the same plan/run contract and skips build/push stages. Record resolved image digests for repeatability. Retain provider-native revision IDs where an OCI digest is not the relevant artifact.

### Configuration and secrets — feature 2

Keep two explicit kinds of input:

| Input | Example | Read behavior | Delivery to workload |
|---|---|---|---|
| Ordinary configuration | `DB_NAME`, `LOG_LEVEL`, service port | Visible to permitted readers | Inject only into selected services |
| Secret | `DB_PASSWORD`, API token, encryption key | Name, version and state visible; value hidden | Resolve the approved reference just before execution |

Classification is an operator decision. A database name is often ordinary configuration but may be sensitive in a particular installation. Names can suggest a default; do not silently downgrade a secret. Default new credential fields to secret.

Add explicit environment and service bindings. Detect duplicate/conflicting keys during review instead of relying on surprising override order. Separate build secrets from runtime secrets; build secrets use the builder's secret mechanism, never a public build argument or an image layer. Ensure frontend-public variables carry a clear “will be included in client code” explanation.

Normal API reads, exports and persisted deployment views contain secret references only. Preserve the existing restricted reveal route and MFA boundary. Audit secret create/update/delete/reveal without logging values. Version references for deterministic reruns; if an external manager cannot supply an immutable version, disclose resolution-at-run-time and record version metadata when available. Revocation should stop unresolved future use, not reconstruct deleted secrets from run history.

Implement redaction before persistence, broadcast, notifications and error responses. Cover exact resolved values, common authorization formats and values split across streaming chunks. Never claim this can detect every transformed or deliberately encoded secret. Avoid unnecessary credential output at its source. Provide encryption-key backup/rotation and recovery instructions; losing the key must not cause silent replacement of existing secret data.

### Logs and status — feature 3

Distinguish **deployment logs** (build/test/deploy stages) from **service logs** (the running application). Offer App → service → instance/replica selection, time range, tail/follow, search within fetched results and an authorized redacted download.

Normalize log events with App, environment, service, run/stage where relevant, target, replica, timestamp, stream and cursor. Keep bounded durable stage chunks or equivalent append-only records in PostgreSQL for reconnect across restarts; start with configurable retention and byte limits. Runtime history initially comes from Docker/Kubernetes/provider log sources, with an explicit available time range. A central long-term runtime archive is a later optional backend, not an implied first-release feature.

Show reconnect state and a gap notice when a source has expired or rotated logs. A successful request to start a deployment is not a healthy service: run target readiness checks, then record an observed status and timestamp. Wire periodic health probes for supported targets; retain Unknown when no observation is possible.

If environment promotion is enabled, dispatch an actual target deployment and link its result. Approval and successful rollout are distinct events. Rollback references a previous immutable application artifact; database data or schema is never automatically downgraded.

### Exit gate

A pinned manual deploy survives a controlled worker restart; no duplicate logical attempt appears. The final revision, plan, artifact and service status agree. A test credential cannot be found in normal API output, exports, persisted logs or streamed logs. Docker/Kubernetes logs reconnect with honest retention limits. Production-mode source builds pass without bypassing existing safeguards. Unsupported operations remain unavailable with a reason.

## 6. M2 — reliable GitHub branches and tags

### User experience

Connect repository → choose environment → choose **branch** or **tag** → preview matching rules → test a delivery → enable auto-deploy. Examples: exact branch `main` to staging, `release/*` to a release environment, or tag `v*` to production. Production approval is configurable; a manual gate can pause an automatically prepared deployment.

Each received event has a plain-language result: **Queued**, **Ignored — branch does not match**, **Duplicate**, **Superseded**, **Awaiting review**, **Rejected — invalid signature**, or **Failed to prepare**. Link the decision to its run when one exists.

### Backend behavior

- Verify the raw request against the selected connection's secret before treating its payload as trusted. Keep size/rate limits and constant-time verification. Use a connection-scoped endpoint so repository names in an unverified body do not choose a different connection's authority. [GitHub signature guidance](https://docs.github.com/en/webhooks/using-webhooks/validating-webhook-deliveries).
- Normalize provider, connection, repository identity, delivery ID, event type, full ref, old/new revision and deletion flag. Store useful metadata and payload hash with bounded retention, not authorization headers or indiscriminate raw payloads.
- Match explicit branch/tag rules, with exclusions taking precedence. Use a documented glob syntax first; separate tag matching from semantic-version policy. Reject ambiguous patterns in a preview, not during deployment.
- Deduplicate with database uniqueness scoped to connection/provider delivery identity, plus a logical trigger/revision/plan identity. Keep the existing middleware as a compatibility layer, but prove behavior across concurrent deliveries and process restarts.
- Persist the delivery decision and job intent transactionally. Manual replay records a new replay request against the original delivery; it does not silently erase deduplication history.
- Resolve the exact event SHA. For annotated tags, resolve the tag object to its commit and record both. Treat a moved release tag as requiring review. Deletion events do not deploy or delete workloads.
- Serialize deployments per App/environment. Default branch policy: finish the active run and keep only the latest eligible pending revision. Check the provider's current branch head before deciding which delayed events are obsolete. Never infer event order from arrival time alone. Tag releases use an ordered, explicit release queue. Manual runs are not silently cancelled by pushes.
- Re-inspect every new Compose revision. Auto-run only inside an approved envelope of services, target, ports, mounts and secret bindings. Privileged settings, new exposed services or expanded credential use require renewed review. The present manually reviewed, SHA-pinned flow remains valid; migration must not silently enable automation on it.
- Apply authorization using a scoped deployment identity. Webhook-triggered execution must not bypass policy merely because no human user ID is attached to the run.
- Start with App triggers. Then permit an explicitly selected authored pipeline through the same normalized event contract and permissions, rather than assuming current App handlers already do this.

Add a **Check for missed changes** action and optional bounded periodic reconciliation for enabled rules. Events can be delayed or absent; polling must create the same deduplicated intent and respect approvals. GitLab, for example, documents circumstances where a multi-ref push produces no webhook. [GitLab webhook limits](https://docs.gitlab.com/user/project/integrations/webhooks/).

### Exit gate

Push to `main` deploys the exact expected SHA; another branch is ignored with a reason. A matching annotated tag deploys its commit. Repeated and concurrent delivery, queue restart, stale event, deleted branch and moved tag cases are deterministic. Configuration edits after queuing cannot alter the attempt. A changed privilege/secret envelope pauses for review. A missed-change check never creates a duplicate deployment.

## 7. M3 — make GitLab and Gitea complete source providers

Introduce a source adapter contract for connection validation, repository identity, ref listing, SHA resolution, source retrieval, webhook authentication and event normalization. Make provider host part of identity: `team/repo` on two Gitea instances is not the same repository.

Implement GitLab and Gitea checkout/inspection as well as their event handlers. Support configured HTTPS self-hosted base URLs and trusted internal CA configuration. Reject unsafe redirects, embedded URL credentials and unintended access to metadata/internal endpoints. For intentional private Git servers, require an operator-approved host policy; do not globally disable certificate or network checks. Test nested GitLab groups, pagination, branch names containing slashes, private repos and revoked credentials.

GitLab authentication is version-aware. Current official documentation recommends signing tokens for new webhooks; older servers still need explicit legacy secret-token compatibility. A connection configured for signatures must never downgrade automatically because a request omitted its signature. Gitea uses its documented signature and delivery headers. [GitLab webhook authentication](https://docs.gitlab.com/user/project/integrations/webhooks/), [Gitea webhooks](https://docs.gitea.com/usage/repository/webhooks/).

Preserve Bitbucket Server/DC under that exact name. Treat Bitbucket Cloud as a separate adapter with separate verification and payload fixtures. Add further providers through the same certification suite only when a user needs them. Do not offer an unauthenticated “any JSON webhook can deploy anything” endpoint.

**Exit gate:** the same branch/tag, pinned checkout, private credential, deduplication, missed-event and recovery suite passes for GitHub, GitLab and Gitea. Each provider has a real disposable-repository acceptance record; fixture-only support is labeled as such.

## 8. M4 — addresses, domains and tunnels

**Users do not necessarily need their own domain.** An installation operator can supply a base domain and give each App a subdomain. An individual self-hosted installation can instead use a supported provider address or tunnel. Cooker cannot create a stable globally reachable name without an operator/provider supplying that infrastructure.

| Mode | What the user supplies | Proposed Cooker responsibility |
|---|---|---|
| Operator domain | Operator configures `apps.example.com` once | Allocate `my-app.apps.example.com`, route it and report TLS status |
| Bring your own domain | User controls a domain and adds displayed DNS records | Validate ownership/DNS, bind one HTTP service, track certificate issuance/renewal |
| Stable tunnel | Operator connects a supported tunnel account and connector | Reconcile route, destination and connector health; show the provider-issued URL |
| Temporary demo | Developer explicitly starts a temporary tunnel | Show expiry/limitations clearly; no production uptime promise |

### Delivery steps

1. Add `DomainBinding` records: App/service/port, hostname, exposure type, ownership proof, desired/observed routing state, certificate state, provider reference and public URL. Normalize names, enforce unique claims and prevent cross-App reassignment without authorization.
2. Extend existing Traefik/Kubernetes route generation. Start with manual DNS instructions and verification. Issue certificates only after ownership and routing requirements pass; persist pending/error/active state. Exercise ACME staging, renewal and restart recovery before production certificate requests.
3. Add optional scoped DNS-provider credentials, beginning with a Cloudflare adapter if needed. Wildcard certificates require the appropriate DNS challenge; wildcard DNS alone does not provide TLS.
4. Add stable Cloudflare Tunnel connector integration; follow with ngrok through the same tunnel interface. Treat connector credentials as secrets. Reuse existing owned connectors where configured; manage only resources marked as belonging to Cooker. Reconcile crash/restart, lost provider access and orphaned routes.
5. Route only explicitly public HTTP services. Database/Redis ports remain private by default. Support streaming/WebSockets and configure upstream timeout behavior with the chosen provider. Removing an App route must not delete a shared domain or shared connector.

Quick Tunnels use temporary provider hostnames and are for testing. ngrok supplies account-dependent addresses and limits. Exact quotas, pricing and available names must be checked during integration; the plan does not promise a universal free permanent domain. [Cloudflare Quick Tunnels](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/do-more-with-tunnels/trycloudflare/), [ngrok plan limits](https://ngrok.com/docs/pricing-limits/free-plan-limits).

Separate **Cooker's webhook/control-plane URL** from **application URLs**. A tunnel to an App should not accidentally expose an unprotected administration endpoint. Use the existing authentication boundary for any exposed Cooker interface.

**Exit gate:** an ordinary team member can obtain an operator-provided HTTPS App URL without buying a domain; a custom hostname cannot activate without proof; TLS renews in the test scenario; a connector restart restores the same supported stable route; deleting an App leaves shared infrastructure intact.

## 9. M5 and M6 — provider ecosystems with explicit scope

Expose capability flags for deploy, source-build/image artifact, runtime logs, build logs, readiness, public URL, scaling, rollback, secrets and domains. Report **unsupported**, **unconfigured** and **temporarily unavailable** separately. Preflight permissions before the first mutation. Store external operation IDs and honor provider rate limits.

| Provider | First accepted integration scope | Later expansion |
|---|---|---|
| AWS | Existing ECS/Fargate deployment; ECR image access; CloudWatch service logs; Secrets Manager references; operator-supplied networking and database endpoints | Additional compute targets and reviewed infrastructure provisioning |
| GCP | Existing Cloud Run deployment; Artifact Registry access; Cloud Logging; Secret Manager and supported Cloud SQL connection references; provider URL | Additional compute, database provisioning and network automation |
| Vercel | Connect account/team and project; create native deployments; inspect build/result state; environment bindings; supported logs; deployment URL and domain association | Broader marketplace/storage integrations driven by actual use |

AWS log integration requires the task's logging configuration and access to its log group/streams. Cloud Run output is available through Cloud Logging; scope retrieval by project, service and revision. Plan those as runtime-log adapters, not as output from the deployment request itself. [ECS logging](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/using_awslogs.html), [Cloud Run logging](https://cloud.google.com/run/docs/logging).

Complete existing AWS/GCP target behavior first: accurate readiness, provider URL propagation, log access, version identifiers, cancellation/reconciliation and supported rollback. An external database binding does not mean Cooker creates the database or network. Unknown health remains Unknown. Existing Fly/Render/SSH adapters become selectable in the App flow only after their supported subset passes the same tests.

Vercel needs a native artifact path. Extend the deployment contract to distinguish an OCI image from a supported source/build output rather than pretending a Vercel deployment is a Docker container. Pin source to an immutable revision and store the Vercel deployment ID. Choose one automation owner for each project; avoid both Cooker and provider Git integration creating duplicate deployments from the same push. Vercel exposes deployment, environment and domain APIs, but the exact supported build/runtime log access and retention must be certified for the selected account. [Vercel REST API](https://vercel.com/docs/rest-api).

Credentials remain server-side references with the minimum scopes needed for the configured target. Validate team/project/account boundaries and redact provider errors. No account-wide infrastructure creation, billing control, arbitrary cross-cloud networking or automatic multi-cloud failover is included in this first ecosystem scope.

**Exit gate per provider:** one real disposable project completes create/update → readiness → URL → build/service logs as supported → failed rollout diagnosis → supported rollback → cleanup, with IDs and evidence. Live cloud tests require the relevant account and cost scope when implementation reaches this gate. An adapter unit test does not substitute for that record.

## 10. Data and API changes

Names below describe proposed contracts; finalize exact schemas against existing migrations in M0. Extend current run/deployment records wherever possible.

| Record | Important fields / constraints |
|---|---|
| SourceConnection | Provider, normalized base URL, provider repository ID, credential reference, allowed hosts, observed access state |
| TriggerRule | Source connection/repository, full ref kind/patterns, destination App or pipeline, environment, approval policy, enabled state, version |
| WebhookDelivery | Connection, delivery identity, event/ref/revision, authentication outcome, payload hash, decision/reason, timestamps; unique deduplication key |
| DeploymentPlan | Immutable source/config/Compose selection, DAG, target capabilities, permission envelope, secret references, plan hash |
| Existing run/deploy record extensions | Trigger + plan IDs, source SHA, image digest/native artifact ID, provider operation IDs, lease/fencing metadata, phase and observed outcome |
| Configuration bindings | Plain values or secret references; environment/service/build/runtime scope; version; no secret plaintext in plan JSON |
| Log chunks / cursors | Run/stage/service/replica identity, ordered cursor, redacted content, retention metadata; bounded size |
| DomainBinding / TunnelConnection | Hostname ownership, service/port, route/certificate state, connector/provider reference, secret reference |

Add resource families for source connections, trigger rules, delivery history/replay, deployment plans, target capabilities and domain/tunnel bindings under the current API versioning convention. Reuse existing App/deploy/run/environment/log endpoints where compatible. Return stable IDs and links for accepted asynchronous work; structured reason codes support clear UI text. Every endpoint must apply the same object-level permission checks as its underlying resource.

Use additive migrations. Map legacy GitHub repository/branch fields to a GitHub source and exact-branch rule while retaining read compatibility. Do not enable automation on previously manual reviewed plans. Keep old webhook routes working during an explicit connection migration window; show the new URL and test status before retiring old delivery paths. Deduplicate across the overlap window. Migration and rollback must preserve encrypted secrets and run history.

## 11. Acceptance matrix and release evidence

| Area | Required scenarios |
|---|---|
| Trigger integrity | Invalid/missing signature, oversized payload, disabled rule, duplicate/concurrent delivery, mismatched repository/host, unauthorized target, approved machine identity |
| Ref policy | Branch match/mismatch, glob exclusion, lightweight/annotated tag, deleted ref, moved tag, out-of-order event, missed event reconciliation |
| Execution | Crash before enqueue, after enqueue, during build, after provider mutation and before recording success; stale worker fencing; cancellation; bounded retry; queue backpressure |
| Review consistency | App edited after enqueue, config changed after approval, altered Compose mounts/ports/secrets, exact checkout and artifact proof, manual retry vs deploy latest |
| Secrets | Wrong-role read/reveal, MFA boundary, plain/secret collisions, service scope, build-layer exposure, split-chunk masking, exports, provider error and notification redaction |
| Logs and health | Multi-service/replica selection, reconnect, rotated/missing history, retention expiry, no cross-App leakage, health timeout and unknown state |
| Domains | Unproved ownership, duplicate hostname, bad DNS, certificate failure/renewal, tunnel restart, removed App, preserved shared connector |
| Providers | Capability rejection, insufficient/revoked credentials, rate limit, partial deployment, URL correctness, supported recovery and owned-resource cleanup |

Run pure contract tests first, then PostgreSQL integration/concurrency tests, isolated local Docker/Kubernetes tests where relevant, browser tests against the connected API, and finally scoped live provider acceptance. Use failure injection around actual durable boundaries; do not only mock every successful call. Preserve the accepted frontend layout and plain-language DAG explanations during wiring.

Proposed performance checks use declared test hardware and provider latency: webhook acknowledgment excludes build execution; the system remains responsive with at least ten queued intents; admission does not exceed configured worker concurrency; logs remain bounded under sustained output. Record measured p95 acknowledgment and queue delay before setting a public service-level promise. Correctness gates such as no lost acknowledged job and no duplicate logical intent apply independently of those timings.

Track delivery decisions, queue age/depth, lease recovery, build duration, deploy/readiness failures, observed health freshness, redaction events without values, and log gaps. One trace/run ID connects the receiver, planner, worker and adapter. Operator notifications should carry an actionable reason and run link.

Release in explicit opt-in slices. Stop rollout on lost jobs, mismatched source artifacts, secret exposure, misleading success states or unbounded retry. Preserve previous runnable artifacts and database backups; disable new trigger admission if necessary while allowing investigation of in-flight work. Database rollback is a migration procedure, never an automatic application rollback side effect.

## 12. First implementation backlog

1. **B01 · Capability and acceptance baseline:** reconcile documentation, enumerate supported combinations, capture the tiny App flow and current failures. Done when the baseline is reproducible and honest.
2. **B02 · Deployment plan contract:** immutable SHA/config/target plan and typed artifact identity; define machine actor authorization. Done when queued attempts cannot change under later edits.
3. **B03 · Production build handoff:** one isolated builder/publisher path using the B02 context. Done when production validation and an actual source-build acceptance both pass.
4. **B04 · Durable App jobs:** transactional intent, queue registration, leases, reconciliation and cancellation. Done when restart/crash-boundary tests preserve outcomes without blind re-execution.
5. **B05 · Protected config + logs:** scoped bindings, secret metadata/reveal flow, output scrubbing, durable stage replay and Docker/Kubernetes service log integration. Can progress alongside B03/B04 after B02.
6. **B06 · Health and truthful completion:** actual readiness/periodic probes; real promotion dispatch if exposed; provider URL/result mapping. Done when submission, approval, rollout and healthy are distinguishable.
7. **B07 · GitHub trigger vertical slice:** normalized delivery/rules, durable dedupe, exact SHA and review envelope. Start with exact branch, then tags/patterns and missed-event recovery.
8. **B08 · Connected frontend acceptance:** wire existing settings, run history, DAG and log views; verify the M0–M2 scenarios; publish a source-backed capability guide in the repository.

**Start with B01 and B02.** B01 tells us what actually runs; B02 defines the evidence that every later job must preserve. B03–B06 make unattended execution defensible. B07 then delivers useful automation without hiding foundational gaps.

After that release, M3–M6 each receive a similarly bounded implementation backlog using the contracts and exit gates above. Reprioritize only when real user adoption, setup friction or provider demand gives a reason. Multi-tenant SaaS, billing, a general infrastructure-provisioning engine, arbitrary plugins and a frontend redesign are outside this first program.

## 13. Source map for the audit

All source observations refer to local commit `949d4a9`, inspected 28 September 2026. Earlier proposals are context, not proof of completed behavior.

- [Current release and source-build limits](../../README.md:21), [current documentation status](../../docs/README.md:3).
- [Webhook routes](../../backend/internal/server/router.go:311), [GitHub event filtering](../../backend/internal/handler/app.go:417), [shared webhook deployment path](../../backend/internal/handler/app.go:526).
- [GitLab receiver](../../backend/internal/handler/webhook_gitlab.go), [Gitea receiver](../../backend/internal/handler/webhook_gitea.go), [Bitbucket Server receiver](../../backend/internal/handler/webhook_bitbucket.go).
- [GitHub checkout URL](../../backend/internal/source/github/clone.go:71), [repository/branch lookup](../../backend/internal/store/postgres/app.go:65), [App target and auto-deploy validation](../../backend/internal/service/app_targets.go:112).
- [App build preflight](../../backend/internal/server/app_execution.go), [production builder/pusher validation](../../backend/internal/config/validate.go:140).
- [Environment model and redaction](../../backend/internal/model/environment.go), [secret backend selection](../../backend/internal/server/factories.go:113), [environment handlers](../../backend/internal/handler/environment.go).
- [Service log reader](../../backend/internal/service/runtime.go), [stage log store interface](../../backend/internal/logstore/logstore.go), [ECS log limitation](../../backend/internal/deploy/deploytarget/ecs/ecs.go:282), [Cloud Run log limitation](../../backend/internal/deploy/deploytarget/cloudrun/cloudrun.go:190).
- [PostgreSQL queue registration](../../backend/internal/server/jobqueue_boot.go:80), [executor wiring](../../backend/internal/server/server.go:313), [promotion records](../../backend/internal/service/promoter.go:115).
- [Proxy route generation](../../backend/internal/service/proxy_manifest.go), [periodic health checker](../../backend/internal/service/app_health.go), [target contract](../../backend/internal/deploy/deploytarget/target.go).
- [Accepted design and backend handoff](/mnt/data/company/apps/Cooker-landing/docs/plans/2026-09-28-backend-handoff.md), [Cooker design specification](/mnt/data/company/apps/Cooker-landing/docs/design/COOKER-DESIGN-SPEC.md).
