# GitHub → Compose preview → deployment targets

**Status: IMPLEMENTED — ready for UAT, human/cloud acceptance pending.**

Requested and implemented for Santapong's 6 September 2026 Cooker change, based
on `develop` at `fb6faaa`. No GitHub account was connected and no cloud resources
were created or deployed during implementation.

The [setup and UAT guide](../guides/GITHUB-COMPOSE-DEPLOYMENT.md) describes the
current supported contract and configuration. The original audit below is
historical evidence of the baseline, not the current implementation status.

| Delivery | Local implementation status |
|---|---|
| P0 — explicit target support | Implemented. Capabilities, runtime/build prerequisites, explicit dispatch and failure propagation; no unsupported-target Kubernetes fallback. |
| P1 — GitHub source | Implemented. Administrator-approved GitHub App installations, public/manual source, repository picker, branch/tag input, recursive Compose discovery and immutable checkout. Live GitHub acceptance pending. |
| P2 — preview | Implemented with compose-go v2.11.0. Ordered files, profiles, explicit variables, Dockerfile/context/target preview, typed graph and target diagnostics. Preview is a masked summary, not an exported executable file. |
| P3 — prefix and bindings | Implemented. Persisted prefix, scoped runtime names, atomic prefix uniqueness, external database replacement and explicit connection-key precedence. |
| P4 — ECS execution | Implemented for existing Fargate infrastructure, supported resources/health checks, service stability and image verification. Live AWS-to-GCP acceptance and cloud runtime log streaming remain pending. No EC2 or Cloud SQL provisioning. |
| P5 — shared catalogue | Current reviewed Compose apps appear in the server-backed GitHub stacks catalogue. OCI distribution and immutable multi-revision history remain future work. |

## Verification record

- `go build ./...`, `go vet ./...`, whole-backend `gofmt` and
  `go test -race -timeout 120s ./...`: passed. The affected service, source,
  handler and server race suites passed again after final validation/lint fixes.
- Disposable PostgreSQL 16: all migrations applied and replayed, JSONB settings
  round-tripped, concurrent duplicate prefixes rejected, and legacy records
  preserved (`TestAppPrefixPostgresMigrationAndPersistence` passed). Its isolated
  schema and disposable container were removed. CI now supplies the integration
  database URL so this test also runs there.
- Frontend: 178 unit tests and the full 32-test Playwright suite passed, including
  import, ordered overrides/profiles, private-source fixtures, external database
  mapping, prefix-conflict recovery, cross-browser catalogue/reopen, deployment
  graph arrival during checkout, mobile overflow, accessibility and motion.
  All six import-flow checks passed again after fixing the direct-entry graph
  stylesheet. Production build passed as part of Playwright setup.
- Frontend lint: zero errors, five existing fast-refresh warnings. Pinned
  golangci-lint v2.5.0: new-code findings corrected; **31 findings remain in
  unchanged files** (13 unchecked errors, five static checks, 13 unused symbols).
  Those files were verified unchanged against HEAD. The full Go lint CI gate
  is therefore not green; this is a UAT candidate, not a release sign-off.
- GitHub and ECS automated tests use local HTTP/SDK fixtures. They verify code
  behavior, not actual account permissions, image registry access or cloud reachability.

## Intended experience

Connect GitHub → choose repository and revision → choose Compose file(s) →
preview services and Dockerfile builds → set deployment prefix → assign targets
and external dependencies → review the resolved plan → deploy and observe.

Working assumptions: “ECS” means AWS ECS; “prefix” means a deployment/project
name such as `shop-staging`. A filename-prefix search should also be available.
AWS EC2 VM creation or Alibaba Cloud ECS would require a separate provider scope.
Use an existing GCP Cloud SQL PostgreSQL instance as the first external-database
example; the user has not yet specified the database engine or existing resources.

## Historical baseline audit — before implementation

| Requirement | Current behavior and evidence |
|---|---|
| Connect a GitHub account and pick public/private repositories | Not implemented in this flow. The wizard accepts `owner/repo` and branch text. Cloning uses a public HTTPS URL; there is no installation/account picker or integrated private-repository authentication. [Wizard](../../frontend/src/pages/NewAppWizard.tsx), [clone](../../backend/internal/source/github/clone.go). |
| Find and choose Compose files | Partial. Detection returns one root build plan. Root `Dockerfile` takes precedence over four standard Compose filenames. No recursive candidate list, variant/prefix search, or override ordering. A Compose path can be entered manually. [Detector](../../backend/internal/build/buildplan/detect.go), [detection service](../../backend/internal/service/app_detect.go). |
| Render the selected repository's Compose before deploying | Missing. The standalone map reads files from the backend's configured Compose directory. Repository Compose parsing and pipeline synthesis occur inside `AppDeployer.Deploy`; the synthesized graph is persisted after execution returns. [Compose page](../../frontend/src/pages/ComposePage.tsx), [app deployer](../../backend/internal/service/app_deployer.go), [handler](../../backend/internal/handler/app.go). |
| Build services that refer to Dockerfiles | Partial implementation. A service with `build` produces Build → Push → Deploy; an image-only service produces Deploy. Basic context and Dockerfile fields are carried through. Nested Compose contexts are currently joined to the repository root rather than the Compose directory; some invalid/parent paths silently fall back to defaults. The YAML parser is a subset of Compose semantics. [Synthesis](../../backend/internal/service/app_deployer.go), [parser](../../backend/internal/service/compose_graph.go). |
| Isolate deployments using a prefix | Inconsistent. Built image names include the app name, but Docker container names and Kubernetes per-service resource names use the service name. Two stacks containing `web` can collide on a shared target. The low-level Compose runner supports `-p`, but the app path generates individual service stages. [Stage execution](../../backend/internal/service/stages.go), [Compose runner](../../backend/internal/deploy/deployer/compose.go). |
| Deploy the app to AWS ECS | Not integrated end to end. An adapter registers task definitions and creates/updates Fargate services in a configured cluster. Registration exists, but no production caller of `deploytarget.Lookup` was found. The app wizard offers Docker host, Kubernetes and Cloud Run only. The Compose synthesizer maps every target except Docker host to Kubernetes. Single-image synthesis adds a deploy stage only for Kubernetes. [ECS adapter](../../backend/internal/deploy/deploytarget/ecs/ecs.go), [registration](../../backend/internal/server/deploytargets.go), [synthesis](../../backend/internal/service/app_deployer.go). |
| Create a VM or a GCP managed database | No provisioning implementation found in the inspected application path. The ECS adapter expects cluster/network/IAM configuration; it does not create EC2 VMs. No Cloud SQL provisioning or database-service replacement workflow was found. |
| Connect an application to an existing external database | Environment/secret resolution exists, providing a building block for connection settings. There is no external-database binding UI, target-side connection check, or verified AWS-to-GCP deployment flow. Existing Compose environment literals override app environment entries, so merely attaching an environment may leave the original database endpoint in place. [Environment resolution and merging](../../backend/internal/service/app_deployer.go). |

**Historical deployment blocker (fixed locally):** selecting an unsupported target must never silently
produce Kubernetes stages or a successful build-only “deployment.” Existing cloud
adapter code and registration do not establish app-level deploy support. The old
“Stable when configured” cloud labels in the hosts/targets guide overstated this.

## Original delivery plan and acceptance criteria

### P0 — make target support explicit

Resolve target availability and supported build modes before cloning/building.
Reject unsupported target/build combinations with an actionable error. Replace
the default-to-Kubernetes branch with explicit dispatch. A deployment succeeds
only after the selected runtime reports the intended workload outcome.

Acceptance: selecting an unavailable ECS/Cloud Run target cannot invoke Kubernetes
or report build/push as a successful deployment. Existing supported paths retain
their behavior. Expose actual capabilities to the wizard.

### P1 — GitHub connection and Compose discovery

Add a GitHub App connection scoped to selected repositories, an accessible repo
and branch/tag picker, and an immutable commit SHA for each inspection. GitHub
supports granting an installation access to selected repositories; see its
[installation documentation](https://docs.github.com/en/apps/using-github-apps/installing-a-github-app-from-a-third-party).
Retain manual public repository input as a supported source.

Scan the selected revision for standard Compose filenames and variants such as
`compose.prod.yaml` and `docker-compose.staging.yml`, including subdirectories.
Show path, services and diagnostics for every candidate. Offer path/prefix search
and explicit manual paths. A root Dockerfile must not hide Compose candidates.
Let users select a base file and ordered overrides; never guess that every match
belongs in the same stack. Clear stale discovery when source or revision changes.

Acceptance: public/private permitted repos, multiple files, nested files, custom
prefix filters, denied access and changed branches all produce the correct list.
All previews and later builds use the selected SHA.

### P2 — preview the resolved stack and its build graph

Use a Compose-spec-aware loader with a pinned supported version and declared
feature support. Normalize the selected files, profiles and explicit environment
inputs before producing the graph. Docker's
[`compose config` semantics](https://docs.docker.com/reference/cli/docker/compose/config/)
provide the reference for merged files, interpolation and normalized rendering.
Do not import the Cooker host's environment into the preview.

Render services, dependency edges, ports, networks, volumes, required variables
and health checks before any build or deployment. Show source YAML and resolved
configuration with secrets masked. For each build service, expand Build → Push →
Deploy and show context, Dockerfile source, target stage, arguments and output
image. For image-only services, show the image source. Give external dependencies
a separate symbol and connection edge from executable stages.

Resolve relative build contexts from the Compose project directory, and Dockerfile
paths from their build contexts, following the
[Compose Build Specification](https://docs.docker.com/reference/compose-file/build/).
Support valid parent-relative paths that remain inside the repository. Reject
missing files, repository escapes and unsupported build features with their exact
locations instead of substituting a different Dockerfile. Preserve or explicitly
resolve `build`/`image`/`pull_policy` behavior in the reviewed plan.

Acceptance: preview performs no build/container/cloud mutation; nested contexts,
multiple Dockerfiles, overrides, missing variables and unsupported fields produce
accurate graphs or actionable diagnostics. Unsupported runtime semantics such as
storage or readiness dependencies cannot be silently discarded.

### P3 — deployment prefix and target mapping

Add an editable project prefix, suggested from repository and environment, with
an exact generated-name preview and collision validation. Example: `shop-staging`
produces scoped resources for `api` and `worker`. Preserve service keys for internal
references. Apply scope consistently to runtime resources, labels, routes, images,
and Cooker-owned networks/volumes, within each provider's naming rules.

On a native Compose target, use its project-name mechanism. Docker documents
[`-p`, `COMPOSE_PROJECT_NAME`, and top-level `name`](https://docs.docker.com/compose/how-tos/project-name/)
and their precedence. Show conflicts from explicit `container_name` or resource
names. References to existing external resources retain their identity.

Assign a runtime per deployable service and allow an explicit “Use existing
database” binding. Start with one compute target per stack and external database
bindings; several compute clouds in one stack require further network planning.
Example intended mapping:

| Compose component | Selected destination | Reviewed action |
|---|---|---|
| `api`, with `build: ./api` | AWS ECS | Build image, push to an accessible registry, deploy the ECS service. |
| `worker`, with its own Dockerfile | AWS ECS | Build and deploy independently, with its own resource settings. |
| `db`, originally a PostgreSQL image | Existing GCP Cloud SQL | Explicitly replace the local database dependency with a connection binding; do not launch the `db` container. |

Replacement must update the application's connection configuration and dependency
checks. It does not transfer existing database data or run migrations implicitly.
Show which configuration wins, especially where Compose literals would override
the linked environment. A simple `DATABASE_URL` substitution is insufficient when
the app expects separate fields, a local service hostname, or a proxy endpoint.

Acceptance: two identical stacks with different prefixes coexist; redeploy updates
only its own resources; an external database is neither created nor removed by
stack lifecycle actions; the preview and execution use the same resolved mapping.

### P4 — ECS execution and AWS-to-GCP validation

Wire the registered ECS adapter into the actual app/run execution path. Extend
the target model and wizard for supported cluster, network, identity, image-pull,
resource and service settings. The current adapter is a single-container Fargate
implementation with fixed task sizing; it is not a generic Compose converter.
Implement or explicitly reject required volumes, health checks, service discovery,
ingress and sidecars. Support a Cloud SQL proxy sidecar only with tested task
grouping/lifecycle behavior; a language connector is another supported design to
evaluate for the user's application.

Assuming AWS, ECS runs containers on compute such as Fargate or EC2; EC2 provides
VMs. See [AWS capacity documentation](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/capacity-launch-type-comparison.html).
Creating VM capacity is separate provisioning work. First support existing
infrastructure; any later create-infrastructure action needs its own reviewed
resource plan and explicit deployment authorization.

An AWS application can use GCP Cloud SQL with appropriate IP connectivity,
authentication and database configuration. Public-IP connections can use the
[Cloud SQL Auth Proxy](https://docs.cloud.google.com/sql/docs/postgres/sql-proxy)
or a supported connector. The proxy does not create a network route. For a private
instance, establish connectivity from AWS to the GCP VPC, such as a VPN; see
[Google's external connection guidance](https://docs.cloud.google.com/sql/docs/postgres/connect-to-instance-from-outside-vpc).
Test connectivity from the intended workload network, not only the Cooker server.

Acceptance: an explicitly authorized disposable ECS deployment builds the pinned
revision, starts healthy tasks, reports logs/status and redeploys predictably.
The application performs a bounded database round trip against a designated test
database. A failed target, denied image pull or unreachable database reports
failure. Verify cleanup affects only test workloads and preserves the external
database. No such cloud run has been performed in this audit.

### P5 — shared stack catalogue and OCI distribution

Persist the source, commit, ordered Compose files, masked summary, prefix and target
bindings as a server-backed stack revision. Keep secret references in the existing
secret system. Other authorized browsers can open the same revision and preview.
Build on the [shared registry proposal](2026-09-06-frontend-node-types-compose-registry.md#later-delivery-a-shared-versioned-compose-registry)
for OCI import/publish after the GitHub-first deployment flow works.

## UAT boundaries and follow-up

The local candidate is ready for human UAT under the setup guide's supported
subset. Existing infrastructure and operator credentials are required. Live
GitHub, Docker workload isolation and AWS-to-GCP database acceptance remain
unchecked. Cloud runtime logs, OCI distribution, infrastructure provisioning,
sidecar grouping and immutable revision history are not delivered by this change.

The historical audit exposed that adapter registration alone did not establish
working App dispatch. Future support claims must follow the production call path
and include observed execution evidence. Local SDK emulation is not live cloud UAT.
