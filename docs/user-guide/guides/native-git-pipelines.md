# Deploy a connected Git source through a native pipeline

Candidate documentation checked 3 October 2026. These controls are under local
acceptance and are not included in the owner’s current localhost4644 build.
Hosted Git, private registry runtime and complete process-loss acceptance are
separate gates in the [full roadmap](../../plans/2026-10-02-consolidated-feature-roadmap.md).

A native pipeline is your executable deployment DAG. A synced Vercel flow is a
read-only explanation of Vercel deployments. Choose a native pipeline when you
want Cooker to build, push and deploy a connected repository.

## Prepare source and registry access

An administrator adds an enabled Git connection with an explicit repository
allowlist in **Settings → Git connections**. Source access and GitHub/Google
sign-in are separate connections. The connection’s provider webhook endpoint is
`/webhooks/connections/<connection-id>`; configure the provider’s supported
signature mode using the saved connection. A signed delivery does not itself
prove a deployment was accepted or succeeded.

An administrator adds an OCI registry connection in **Registry**, then grants the
saved native pipeline’s exact ID. An operator selects that connection under each
stage’s **Registry access for this step**. Empty selection uses anonymous access.
A grant alone does not select a credential; a selected ID alone does not grant it.
See [Registry access](registries.md).

## Save the deployment DAG

1. Open your native pipeline in **Pipelines**.
2. Expand **Source**. Choose **Connected Git repository**, the approved connection,
   repository and a full ref: `refs/heads/main` or `refs/tags/v1.0.0`.
3. Configure Build → Push → Deploy and connect the dependency arrows. Build paths
   stay relative to the checked-out repository. For example, context `.` and
   Dockerfile `Dockerfile`.
4. Choose **Docker container** for the Deploy runtime. Set a unique runtime name
   and required port mappings. Use the Push stage’s immutable image output, such
   as `${stages.push.image}`, where `push` is that stage’s exact ID.
5. Choose permitted registry connections separately on the Build, Push and Deploy
   steps. Save and validate the pipeline before reviewing a Git rule.

Connected native source requires a Build stage and currently supports Docker
deployment. Host-context Compose remains a separate supported registry transport;
source-bound Compose, Kubernetes/Helm and cloud runtime adapters are later work.
The server rejects unsupported scoped transports before checkout/build. Stage
outputs must stay enabled (`COOKER_OUTPUTS_ENABLED`, default enabled) for the
immutable Push→Deploy image handoff. Configuring a stage does not verify that the
operator’s builder, registry or Docker target is available.

## Review a Git rule

An administrator opens **Git deployment rules** in the native editor:

1. Choose branch updates or release tags, exact names or patterns, and optional
   exclusions. Rule names omit `refs/heads/` and `refs/tags/`.
2. Keep **Review each commit**, or choose **Automatic within reviewed
   configuration** when that machine actor is authorized.
3. Enter a real matching branch/tag name in **Concrete ref to inspect**, then
   select **Preview Git rule**.
4. Review the exact commit, saved pipeline version and source input paths. Select
   **Approve configuration and save Git rule**.

The server records reviewer authority and pins repository identity, source
connection, saved pipeline/environment versions and configuration hashes. The
browser cannot manufacture approval evidence. Editing source inputs, the saved
pipeline or permissions requires another configuration review; automatic does
not mean unrestricted. A changed ref between acceptance and execution can fail
freshness checks before the build.

**Check ref and evaluate rule** handles a missed delivery through the same rule
checks. It records a delivery decision and can queue an enabled automatic rule;
it is an execution action. **Disable rule** stops new matching admissions.

## Follow the result

Use **Git delivery decisions** to filter this pipeline or all incoming deliveries,
provider, decision status and exact ref. Open the linked pipeline/run. Accepted
means recorded or queued; use the run and stage results to see deployment outcome.
Missing historical decisions remain unknown rather than being recreated from
current rules. Delivery history is administrator-scoped under existing policy.

The run’s **Source receipt** shows accepted repository/ref/commit and source-plan
digest. **Verified checkout** appears only after the worker records that exact
checkout. Older or host-context runs may have no receipt. If the pipeline was
edited later, the run page labels that its graph shows the current definition;
it does not reconstruct the historical graph.

Pipeline stage logs show build/push/deploy output. Native Docker runtime readiness
and logs still need their own observation binding; App service log APIs are not
claimed for arbitrary native workloads. An uncertain interrupted deployment
requires operator reconciliation before a new effect is attempted. Retrying an
already admitted logical run recovers its retained receipt rather than repinning
current source or credentials.

## Developer interfaces

| Purpose | Interface |
| --- | --- |
| Read approved source choices without credentials | `GET /api/v1/apps/source-connections` |
| Save a connected native source | Pipeline `source: {connectionId, repository, fullRef}` on the existing versioned PUT |
| Request stage registry access | Stage `config.registryConnectionIds` |
| Preview/save a native rule | `POST /api/v1/admin/trigger-rules/preview` and `/trigger-rules` with `targetKind: pipeline`, `targetId`, empty `appId` and typed ref |
| Save reviewed configuration | Return preview’s rule, `expectedEnvelopeHash` and `expectedPlanHash`; server independently rechecks them |
| Inspect native decisions | `GET /api/v1/admin/trigger-deliveries?targetKind=pipeline&targetId=<id>` |
| Inspect accepted source | Existing Run response `source`, with separate optional `checkedOutAt` |

See [Registry authority](../../backend/registry-authority.md),
[automation review](../../backend/automation-review.md) and
[Git delivery history](../../backend/git-delivery-history.md) for admission,
credential/version, role/MFA and audit boundaries. Local fixture results are
recorded in the [coherent integration evidence](/mnt/data/cooker-roadmap-20261002/coherent-source-registry/README.md).
