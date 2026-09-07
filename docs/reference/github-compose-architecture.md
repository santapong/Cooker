# Reviewed GitHub Compose execution

This reference describes the App import/execution path at `6d6ca01` on `develop`,
checked against repository code on 7 September 2026. The implementation is
unreleased and ready for UAT; it is not proof of live GitHub or cloud acceptance.
The broader pipeline adapter catalogue has separate execution contracts.

![Reviewed GitHub source, Docker image handoff, existing ECS Fargate and external Cloud SQL](../images/github-compose.svg)

[Interactive architecture](../images/github-compose.html) · [Operator setup](../guides/GITHUB-COMPOSE-DEPLOYMENT.md)

## Inspect, save, execute

| Boundary | Behavior | Source |
|---|---|---|
| Repository access | Administrator-approved installations; repository membership checks; short-lived contents-read checkout token | [GitHub App client](../../backend/internal/source/github/app.go) |
| Source identity | Resolve a branch/tag to a full SHA; fetch the saved SHA for later deployment | [Checkout](../../backend/internal/source/github/clone.go) |
| Compose inspection | Shared loader for ordered files, profiles, explicit variables and repository-contained paths; masked preview | [Inspection](../../backend/internal/service/app_inspection.go) |
| Compatibility | Reject unsupported fields for the selected destination rather than silently discard them | [Compatibility checks](../../backend/internal/service/app_compose_compatibility.go) |
| Save/review | Persist source SHA, build inputs, prefix and bindings; manual deploy | [App model](../../backend/internal/model/app.go), [wizard](../../frontend/src/pages/ComposeImportWizard.tsx) |
| Execution capability | Check actual builder/pusher and deployer prerequisites | [Execution checks](../../backend/internal/server/app_execution.go) |
| Deploy | Synthesize the executable stages, dispatch explicitly and surface failures | [App deployer](../../backend/internal/service/app_deployer.go), [targets](../../backend/internal/service/app_targets.go) |

The preview performs no build, push or deployment. Editing inspection inputs
invalidates the corresponding review. The App page opens the deployment view
during checkout and receives the generated graph as execution prepares it.

## Identity and persistence

A reviewed App stores the full Git SHA, ordered Compose files, profiles and
explicit interpolation inputs in its build plan. A moving branch does not change
that stored SHA. **Inspect latest branch revision** starts a new review; reviewed
Compose Apps cannot combine their pin with webhook auto-deploy.

Prefixes scope runtime identity. `RuntimeServiceName` combines the prefix and
sanitized service name; native Docker uses the Compose project prefix and Docker
adds its normal instance suffix. Validation rejects invalid names and generated
collisions. Migration [027_app_prefix](../../backend/internal/store/postgres/migrations/027_app_prefix.up.sql)
adds a unique index for explicit prefixes using target kind, host and namespace
scope. Legacy Apps without an explicit prefix retain their existing naming path.

PostgreSQL persists Apps and deployment graphs. The **GitHub stacks** catalogue
reads current saved Compose Apps from the server; it does not store an immutable
revision archive. Browser-local Compose entries only contain file references and
counts. See [library behavior](../user-guide/guides/compose-library.md).

## The image and runtime handoff

Source builds currently require Docker builder + Docker pusher so the inspected
checkout, built image and published image share a compatible execution path.
[Production validation](../../backend/internal/config/validate.go) refuses both
backends. This source-build flow remains dev/UAT-only until a compatible
production App build handoff is implemented and accepted.

| Runtime | Current translation |
|---|---|
| Native Docker Compose | Private runtime file outside build contexts; project prefix, service DNS, named volumes and readiness waiting |
| Kubernetes | Deployment and Service for each supported executable service; operator-selected namespace |
| ECS Fargate | Single-container ECS service per executable service, valid task resources, container health checks, service stability and image verification |
| Cloud Run | Explicit adapter dispatch for the supported single-container subset |

The App path supports one compute target and one replica per service. It does not
translate cloud volumes, Compose DNS between separate cloud services, ingress or
sidecar grouping. It does not select managed remote Docker hosts or provision
compute/database infrastructure. Capability and field diagnostics define whether
an individual plan is executable.

## External database substitution

A binding identifies the Compose database service, descriptive external resource,
consumer services and a map of application variable names to Environment key
names. For example, `DATABASE_URL=CLOUD_DB_URL` looks up `CLOUD_DB_URL` from the
linked Environment and assigns its value to the consumer's `DATABASE_URL`.

That value overrides the consumer's local Compose setting. The bound database
service is removed from build/runtime execution. The resource label does not
establish authentication, TLS, a network route, Cloud SQL proxy or data migration.
The application must implement the chosen connection method, and acceptance must
exercise the database from the deployed workload.

## Secrets and review graphs

Repository paths resolve through symlinks before containment checks. GitHub
installation tokens stay out of URLs, CLI arguments, checkout configuration and
browser responses. The generated runtime Compose file has private permissions,
lives outside every build context and is removed after use.

Persisted deployment graphs mask environment/build argument values, remove
executable details and carry `reviewOnly`. Pipeline reruns reject these graphs;
use App Deploy to resolve the real inputs again. Build arguments are not a secret
transport. Environment secrets should not be pasted into Compose source literals.

## Acceptance and limitations

Local fixture, browser, race and PostgreSQL checks are recorded in the
[verification record](../plans/2026-09-06-github-compose-deployment.md#verification-record).
Live GitHub installation permissions, registry access, AWS workload behavior and
AWS-to-GCP database connectivity still need UAT. Existing Go lint findings remain
recorded separately from passed checks.

ECS/native Docker readiness is a runtime check; it only proves database readiness
if application health checks exercise that dependency. Automatic App health
polling and cloud runtime log streaming are incomplete. App deletion does not
tear down cloud resources; use the operator/provider cleanup procedure for the
specific UAT resources.
