# Apps, pipelines and environments

| Concept | Purpose |
|---|---|
| **App** | A repository, build plan and compute target, with optional Environment and database bindings. The reviewed Compose flow pins the source commit. |
| **Pipeline** | An explicitly authored DAG of build, test, push, deploy, approval or custom stages. |
| **Environment** | Named variables, secrets, target settings and promotion policy that Apps or pipeline stages can reference. |

## Choose the right entry point

Use **Apps → New app** to import and review a Compose stack. Use its **Use a single
Dockerfile** link for the simpler source layout. Use **Pipelines** when you need a
custom stage graph, parallel tests or explicit approval stages.

A Compose App can contain several services. It uses one compute target and one
replica per executable service; an external database binding can replace a local
Compose database. This does not make the App a general infrastructure provisioner.

## What a reviewed App saves

| Field | Meaning |
|---|---|
| `githubRepo`, `branch` | Repository identity and the branch/tag used for inspection |
| `buildPlan.commit` | Full 40-character Git SHA retained for deployment |
| `buildPlan.installationId` | Approved GitHub App installation, when used |
| `buildPlan.files` | Base Compose file followed by overrides in merge order |
| `buildPlan.profiles`, `buildPlan.variables` | Selected profiles and explicit interpolation inputs |
| `deployTarget.kind`, `deployTarget.prefix` | Compute target and scoped workload identity |
| `deployTarget.externalServices` | Existing database labels, consumers and Environment-key bindings |
| `environmentId` | Environment supplying variables and secrets |
| `registryRef` | Image registry/repository prefix used for built images |

See [the model](../../../backend/internal/model/app.go) for the complete schema.
The preview masks configuration values and shows Dockerfile/context/target and
service details. It performs no build or deployment.

**Save app** persists the reviewed configuration. **Deploy** on the App page
checks capabilities, retrieves the pinned source and executes the plan. Reopening
keeps the saved SHA even when its branch moves. Use **Inspect latest branch
revision** to review an update; saving a reviewed Compose App disables webhook
auto-deploy. Legacy unpinned App webhook behavior is a separate flow.

The graph saved for deployment display has redacted values and is **review-only**.
Use the App's Deploy action to run again; the displayed graph is not an executable
pipeline template.

## Prefixes and external databases

Use a prefix such as `shop-uat` to distinguish deployments. Prefixes accept 1–40
lowercase letters, digits or hyphens, with a letter/digit at each end. Generated
names are checked for collisions and target limits. Explicit prefixes are unique
within a target scope; see the [architecture reference](../../reference/github-compose-architecture.md#identity-and-persistence).

For an external database, create an Environment key such as `CLOUD_DB_URL`, then
bind the consumer's `DATABASE_URL` to that key. The binding overrides the local
Compose value for that consumer and excludes the database service from execution.
The external resource name is a label; it does not create connectivity,
credentials, a database instance or a proxy.

## Libraries and registries

The Compose page has two saved-stack surfaces: browser-local file references and
server-backed GitHub Apps. An image registry stores container images and is a
separate system. Read [Compose library and registries](../guides/compose-library.md)
for persistence, rename/forget and sharing behavior.

## Capability boundaries

The App flow supports Cooker-local Docker, Kubernetes, configured ECS Fargate and
Cloud Run for their supported subsets. Source builds require Docker builder +
Docker pusher and are currently dev/UAT-only. A registered adapter elsewhere in
the backend is not evidence that the App workflow supports it.

The App page provides deployment/stage logs and on-demand runtime status. Automatic
App health polling and cloud container log streaming are incomplete. Acceptance
should verify application health and external database access from the deployed
workload. See [setup and UAT](../../guides/GITHUB-COMPOSE-DEPLOYMENT.md).

## Candidate deployment-history behavior

The source/registry candidate is under local acceptance and is not yet activated
on the owner's localhost4644 build. Its App history distinguishes these cases:

- **Rejected before deployment:** the accepted registry permission or version
  changed before execution. The failed attempt has a run link but no image or
  deployment to roll back to. Review access and start a new deployment.
- **Needs attention:** Cooker cannot safely determine or acknowledge the outcome.
  Inspect the recorded run and actual workload before resolving it; this status
  is not permission to deploy again automatically.
- **History unavailable:** Cooker could not read the history. **Refresh history**
  retries the read; previously loaded attempts remain labelled as stale. Only a
  successful empty response shows **No deploys yet**.

A rejected attempt remains in history without hiding a previously recorded
runtime from service/log inspection. Runtime readiness and application health
are still separate observations; an earlier failed or uncertain deployment does
not become successful because it remains available to inspect.

For developers, queue acknowledgement and deployment outcome are distinct. A
handled rejection may complete its queue job while its deployment run stays
failed. If acknowledgement is lost after that failure was committed, the queue
can need attention while the exact failed attempt remains unchanged. This narrow
before-execution behavior does not establish recovery after runtime effects.
The [full roadmap](../../plans/2026-10-02-consolidated-feature-roadmap.md) records
the separate acceptance gates.

## Related guides

- [Pipelines](pipelines.md): DAG semantics and execution.
- [Environments](environments.md): variables, secrets and promotions.
- [Secrets](../guides/secrets.md): backend configuration.
- [GitHub webhooks](../guides/github-webhooks.md): unpinned/event-driven workflows;
  reviewed Compose Apps remain manual.
