# Hosts and deploy targets

Two different concepts. Both are about "where things run", but they live at different layers.

## Deploy targets

A **deploy target** selects the App's compute runtime. The GitHub Compose import
flow now dispatches explicitly to these targets:

| Kind | App fields | Current support |
|---|---|---|
| `docker-host` | `prefix`; no `hostId` | Docker on the Cooker host. Compose uses project-scoped native Compose; a single Dockerfile uses Docker Run. |
| `kubernetes` | `prefix`, `namespace` (default `default`) | Existing configured Kubernetes cluster, supported per-service manifests. |
| `ecs` | `prefix` | Existing globally configured AWS ECS Fargate cluster/network/roles. |
| `cloud-run` | `prefix` | Existing globally configured Google Cloud Run project and region. |

All source-build Apps require Docker builder + Docker pusher. The capability API
and import wizard report unavailable targets and unsupported Compose features.
Cloud region and resource names are derived from server target configuration and
the reviewed prefix; per-app `region`/`service` overrides are rejected. Fly,
Render, SSH and remote managed Docker hosts are not dispatched by this App flow.
Their adapter or Host records do not establish App support.

The local implementation fixes the `fb6faaa` baseline's cloud dispatch gap and
Kubernetes fallback. Cloud execution has automated SDK/HTTP fixture coverage;
live cloud acceptance remains pending. See the
[GitHub Compose setup and UAT guide](../../guides/GITHUB-COMPOSE-DEPLOYMENT.md)
for required configuration, exact limitations and acceptance steps.

### Existing databases in another cloud

A stack uses one compute target. `externalServices` may replace a Compose database
with an existing GCP Cloud SQL or external database binding. The binding names its
consumers and maps application variables to keys in a linked Cooker Environment.
These overrides take precedence over Compose connection literals. The database
is omitted from build/deploy and retains its independent lifecycle.

Cooker does not provision EC2 VMs, clusters, networking or managed databases, and
does not create cross-cloud connectivity or proxy sidecars. ECS support here means
Fargate containers. Workload-to-database connectivity requires live UAT.

## Hosts

A **Host** is a managed Docker daemon or Kubernetes cluster Cooker can dial. The model is `model.Host` (`backend/internal/model/host.go`):

| Field | Purpose |
|---|---|
| `name` | Display name. |
| `kind` | `docker` or `kubernetes`. |
| `reachability` | `direct` (plain TCP/HTTPS) or `tailnet` (Tailscale tsnet). |
| `dockerEndpoint` | Used when `kind=docker`, e.g. `tcp://10.0.0.3:2375`. |
| `kubeconfigRef` | Used when `kind=kubernetes`. Names a kubeconfig stored as a secret. |
| `tailnetIP` | Populated by the tsnet transport after first contact. |

### Direct vs tailnet hosts

| Reachability | When | Caveats |
|---|---|---|
| `direct` | Host is reachable on the cluster network. | Use TLS for any non-trivial deployment (`tcp://` over plaintext is dev-only). |
| `tailnet` | Host is only reachable over a Tailscale tailnet Cooker joins via `tsnet`. | **Build-tagged.** Default builds do NOT include the tsnet transport; you need `-tags tsnet` (see [`docs/UAT.md`](../../guides/UAT.md#what-works-right-now)). |

> Host management and App target dispatch are separate. Creating a Host record
> does not make it selectable in the GitHub Compose flow. The API can be exercised via:
>
> ```bash
> curl -X POST http://localhost:8080/api/v1/hosts \
>      -H 'Content-Type: application/json' \
>      -d '{"name":"prod-docker","kind":"docker","reachability":"direct","dockerEndpoint":"tcp://10.0.0.3:2375"}'
> ```
>
> Source: [`docs/UAT.md`](../../guides/UAT.md#scenario-5--managed-hosts).

## CRUD endpoints

| Operation | Endpoint | Role |
|---|---|---|
| List hosts | `GET /api/v1/hosts` | any authenticated |
| Create host | `POST /api/v1/hosts` | operator / admin |
| Get host | `GET /api/v1/hosts/:id` | any authenticated |
| Update host | `PUT /api/v1/hosts/:id` | operator / admin |
| Delete host | `DELETE /api/v1/hosts/:id` | admin (with MFA gate) |

## The relationship to Environments

[Environments](environments.md) name the deploy destination for stages assigned to them. For Kubernetes:

- `Environment.Target.Type = "namespace"` + `Namespace=cooker-staging` deploys to that namespace in the running cluster.
- `Environment.Target.Type = "cluster"` + `ClusterID=<id>` dials a separately-configured cluster (via `POST /api/v1/settings/clusters`).

For App deployments, the reviewed App `DeployTarget` selects compute; the linked
Environment supplies plain variables and secret values. Do not assume a generic
pipeline Environment's cluster selection changes an App's globally configured
cloud target.

## Cross-references

- **[Apps](apps.md)** — how `DeployTarget` is set on an App.
- **[Environments](environments.md)** — how K8s deploys are scoped.
- **[Kubernetes deploy](../guides/kubernetes-deploy.md)** — wiring kubeconfigs and RBAC.
- **[Reference: env vars](../reference/env-vars.md#deploy-targets)** — every cloud target's required config.
