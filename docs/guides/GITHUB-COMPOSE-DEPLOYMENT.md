# GitHub Compose deployment — UAT candidate

Implemented locally for the 6 September 2026 Cooker change. Human acceptance,
connection to a real GitHub App installation, and live cloud deployment remain
pending. The automated evidence below uses repository fixtures, local service
emulation and disposable PostgreSQL.

## What you can do

Open **Apps → New app**, connect an approved GitHub installation or enter a public
`owner/repo`, and select a branch or tag. Cooker discovers Compose files throughout
that revision. Choose the base file and ordered overrides, set profiles and
interpolation inputs, then render the service graph and Dockerfile details.

Choose a prefix and compute destination, optionally replace a Compose database
with an existing database connection, and review the resulting names and actions.
**Save app** stores the configuration; **Deploy** on the app page starts execution.
The deployment view opens during checkout and updates with the generated graph.

The **Compose registry → GitHub stacks** section lists these server-stored apps
across browsers. It stores the current reviewed source revision, files, profiles,
prefix and bindings. Browser-local file references remain available beside it.
This is not an OCI Compose artifact publisher or an immutable revision archive.

## Operator setup

### Source builds and persistence

Use PostgreSQL for persistent shared apps. Migration `027_app_prefix` runs with the
normal startup migrations; it prevents duplicate explicit prefixes within a target
scope. Existing apps without explicit prefixes retain their legacy identity.

GitHub source builds currently require this image handoff:

```dotenv
COOKER_BUILDER=docker
COOKER_PUSHER=docker
COOKER_REGISTRY=registry.example.com/team
```

Install Git, Docker CLI and Compose v2 in the backend environment, with access to
the intended Docker daemon. Cooker's Docker CLI must already have registry push
access. The deployment runtime must have pull access to the resulting images.
For ECR, provision repositories such as `shop-uat-api` and configure the execution
role's pull permissions before deployment. The local UAT registry is not an AWS
image registry.

The wizard rejects no-op build/push backends and unsupported image handoffs for
source builds. BuildKit/Crane remain available to separately configured pipeline
workflows; their presence does not establish a working App build handoff.

### GitHub App

Create an administrator-managed GitHub App with **Contents: read-only** and the
required repository metadata permission. Install it on the repositories to make
available to this Cooker workspace. Configure the backend:

```dotenv
COOKER_GITHUB_APP_ID=123456
COOKER_GITHUB_APP_SLUG=your-cooker-app
COOKER_GITHUB_APP_PRIVATE_KEY_FILE=/run/secrets/cooker-github.pem
COOKER_GITHUB_INSTALLATION_IDS=12345678,23456789
```

Mount the PEM key read-only and make it readable by the backend user. These are
server settings, not Vite variables. With `docker-compose.uat.yml`, add these
environment entries and the key mount to a local Compose override; `.env.uat`
alone does not automatically pass arbitrary settings into the container.

After installing through **Connect GitHub**, an administrator must add the
installation ID to the allowlist and restart the configured backend. Use
**Refresh connection** to load the enabled account. The picker is shared with
authorized workspace operators; it is not a personal OAuth credential store.
Public repository entry works without an App connection.

Cooker requests short-lived installation tokens with contents-read permission,
verifies that the repository belongs to the approved installation, and limits
checkout tokens to that repository. Tokens stay out of URLs, Git arguments,
checkout configuration and browser responses. GitHub documents these
[repository and permission restrictions](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-an-installation-access-token-for-a-github-app).

### Compute destination

| Destination | Required existing setup | Current App behavior |
|---|---|---|
| Docker on Cooker host | Docker daemon and Compose v2 | Project-scoped native Compose, service DNS, local named volumes, per-service readiness. No managed remote-host selection. |
| Kubernetes | `COOKER_DEPLOYER=kubectl` or `clientgo`, working cluster credentials | One Deployment and Service per supported Compose service in the selected namespace. |
| AWS ECS Fargate | Settings below, AWS identity and image access | One single-container ECS service per executable Compose service; resource sizing and container health checks. |
| Google Cloud Run | `COOKER_DEPLOY_CLOUDRUN_PROJECT`, `COOKER_DEPLOY_CLOUDRUN_REGION`, application default credentials | Explicit Cloud Run adapter dispatch for the supported single-container service subset. |

Unavailable targets show a setup reason. Fly, Render, SSH and unrecognized App
target kinds fail before checkout instead of falling back to another runtime.

```dotenv
COOKER_DEPLOY_ECS_REGION=ap-southeast-1
COOKER_DEPLOY_ECS_CLUSTER=your-existing-cluster
COOKER_DEPLOY_ECS_EXECUTION_ROLE=arn:aws:iam::123456789012:role/your-execution-role
COOKER_DEPLOY_ECS_TASK_ROLE=arn:aws:iam::123456789012:role/your-application-role
COOKER_DEPLOY_ECS_SUBNETS=subnet-example1,subnet-example2
COOKER_DEPLOY_ECS_SECURITY_GROUPS=sg-example
```

Supply AWS credentials through the backend's normal AWS SDK credential chain.
The operator identity needs ECS task/service operations and permission to pass
the configured roles. Cluster, subnets, security groups, image registry and IAM
roles must already exist. The current adapter uses Fargate with public-IP
assignment; routing, egress and inbound rules must fit that network choice.
It does not create a load balancer, DNS record, EC2 VM or Cloud SQL instance.
AWS distinguishes [ECS compute options](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/capacity-launch-type-comparison.html);
this release implements Fargate containers rather than VM provisioning.

## Example: GitHub API on ECS, database on GCP

For a repository with `deploy/compose.yaml`, `api/`, and
`docker/api.Dockerfile`, a starting configuration is:

```yaml
services:
  api:
    build:
      context: ../api
      dockerfile: ../docker/api.Dockerfile
      target: runtime
    ports:
      - "8080:8080"
    environment:
      DATABASE_URL: postgres://local:local-uat-only@db:5432/shop
    depends_on:
      db:
        condition: service_healthy
    deploy:
      resources:
        limits:
          cpus: "0.75"
          memory: 768M
  db:
    image: postgres:16
    environment:
      POSTGRES_USER: local
      POSTGRES_PASSWORD: local-uat-only
      POSTGRES_DB: shop
    volumes:
      - data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U local -d shop"]
      interval: 10s
      timeout: 5s
      retries: 5
volumes:
  data:
```

The Dockerfile must contain the selected `runtime` build stage and start the
application on port 8080. Contexts resolve from the base Compose directory;
Dockerfiles resolve from their build context, including parent paths that remain
inside the repository. This follows Docker's
[build-path semantics](https://docs.docker.com/reference/compose-file/build/).

1. Create a Cooker Environment with a secret key such as `CLOUD_DB_URL`, holding
   your application's valid Cloud SQL connection value. Add any other TLS or
   connector settings your application requires.
2. Import the repository and render `deploy/compose.yaml`. Choose prefix
   `shop-uat`, the configured ECS target, the environment, and an accessible image
   registry.
3. Under database `db`, select **Use an existing database**, provider **GCP Cloud
   SQL**, resource `your-project:your-region:your-instance`, and consumer `api`.
   Enter the binding `DATABASE_URL=CLOUD_DB_URL`.
4. Review `shop-uat-api` as the ECS workload and `db` as an external dependency.
   The binding replaces the Compose `DATABASE_URL` literal for `api`. Cooker does
   not build or deploy `db`, and does not create its Docker volume on ECS.
5. Save, then deploy when the destination is configured. Verify the deployed
   application performs a bounded database write/read against a designated UAT
   database. Configure an application health check that exercises the required
   dependency if database readiness must gate rollout success.

The resource label is descriptive. It does not establish credentials, TLS,
network access, a Cloud SQL proxy or data migration. A Cloud SQL language connector
must be integrated into your image/application if that is your chosen connection
method. Proxy sidecar/task grouping is outside this implementation. For a private
Cloud SQL address, provide routing from the AWS workload network to its GCP VPC.
Google's [outside-VPC connection guidance](https://docs.cloud.google.com/sql/docs/postgres/connect-to-instance-from-outside-vpc)
describes the network prerequisites. Test from the workload, not just Cooker.

## Review and execution contract

- The full source commit is saved. Reopening retains that SHA even if its branch
  moves; **Inspect latest branch revision** starts a new review. Reviewed Compose
  apps use manual deployment, with webhook auto-deploy disabled on save.
- The same Compose loader resolves review and deployment. Ordered overrides,
  profiles and explicit inputs are supported. Host process environment and root
  `.env` files are not implicitly imported; select an Environment or supply
  non-secret interpolation values. Explicit repository `env_file` is supported.
- Preview includes service/build dependencies, ports, networks, volumes, resource
  requests, Dockerfile source and a masked configuration summary. It performs no
  build, push or deployment. The summary is not an executable exported Compose
  file. Source literals should never contain production credentials.
- Prefixes contain 1–40 lowercase letters, digits or hyphens and start/end with a
  letter or digit. Generated names are checked for length/collisions. Docker uses
  the Compose project prefix; its default container name includes a `-1` suffix.
- Build supports context, Dockerfile, target and arguments. If both `image` and
  `build` are present, explicitly choose `pull_policy: build`. Other build modes
  receive diagnostics. Build arguments are not a secret-delivery mechanism.
- One compute target and one replica per service are supported. Cloud service
  dependencies need reachable endpoints or explicit external bindings; Compose
  DNS is not synthesized between separate cloud services. Bind mounts, config/
  secret mounts, privileged host settings and unsupported target fields block
  deployment. Native Docker additionally rejects provider/model/development
  actions. Cloud volume translation, ingress, sidecars and service discovery are
  not implemented. Diagnostics are authoritative for the selected input/target.
- The runtime Compose file is private, outside the build context, and removed
  after use. This prevents `COPY .` from packaging generated runtime secrets.
  Persisted deployment graphs contain masked values and cannot be rerun as
  executable pipelines; use the App's Deploy action.
- ECS waits for a stable service and verifies the task definition image. Native
  Docker Compose waits for service readiness. This is not independent database
  connectivity proof. Cloud runtime log streaming and automatic App health
  polling are not complete; build/stage logs and on-demand runtime status remain
  available. Use the provider console for cloud container logs.

## UAT acceptance

Automated local checks cover GitHub permission/token/pin behavior, Compose merge
and path boundaries, environment substitution, target dispatch/readiness errors,
redacted graphs, unique prefixes, PostgreSQL migration/persistence and browser
import/save/reopen/error recovery. Exact final check results are recorded in the
[implementation plan](../plans/2026-09-06-github-compose-deployment.md).

- [ ] Connect an approved real GitHub App installation; verify allowed private
  repositories appear and unapproved repositories are inaccessible.
- [ ] Render a representative repository, choose overrides/profiles, inspect
  every Dockerfile, and correct any target diagnostics.
- [ ] Save/reopen from another browser; move the branch and confirm the saved SHA
  remains fixed until explicitly reinspected.
- [ ] Deploy two prefixes to a disposable Docker target; verify service DNS,
  named volume isolation, redeploy identity and failure reporting.
- [ ] Deploy the approved ECS/Cloud SQL test mapping; verify image pull, container
  health, application database round trip and redeploy to a new reviewed SHA.
- [ ] Verify denied image pulls, unhealthy tasks and unreachable database are
  observable. A database failure only gates rollout when the app health check
  reports it.
- [ ] Remove only the designated test compute resources and confirm the external
  database/data remain intact. App deletion is not cloud resource teardown.

No live cloud deployment or infrastructure cleanup is implied by automated tests.

## Repeatable local verification

From `backend`, use Go 1.25 and the pinned module set:

```sh
go build ./...
go vet ./...
go test -race -timeout 120s ./...
```

Set `COOKER_INTEGRATION_DATABASE_URL` to a disposable PostgreSQL connection to run
`TestAppPrefixPostgresMigrationAndPersistence`. The role must be able to create a
schema. The test isolates all tables in its own schema and drops that schema
through cleanup. It does not require resetting the database's public schema.
From `frontend`, run `npm test`, `npm run lint`, and `npm run test:e2e`.

Verified implementation lessons from this change:

- Keep generated runtime Compose outside every build context; otherwise `COPY .`
  can package runtime secrets into the image. Resolve paths through symlinks
  before checking repository containment.
- Empty linked-environment/variable input is valid: initialize the loader map
  before inserting diagnostic placeholders. Distinguish empty interpolation
  defaults from missing required values.
- Verify the actual builder → image pusher → target caller, not just registered
  adapters. A build-only or no-op run cannot establish deployment support.
- Test direct entry to each lazy-loaded page. The import graph must load shared
  node/edge CSS without visiting a pipeline page first. Inspect screenshots as
  well as DOM assertions; the import regression now checks the rendered node
  dimensions and transparent edge hit paths.
- Keep cloud fixtures and live account/network acceptance separate in test reports.
