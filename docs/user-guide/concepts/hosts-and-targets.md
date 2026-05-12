# Hosts and deploy targets

Two different concepts. Both are about "where things run", but they live at different layers.

## Deploy targets

A **deploy target** is the runtime an App deploys to. The model is `model.DeployTarget` (`backend/internal/model/app.go:52-58`); the kind is one of:

| Kind | Description | Status |
|---|---|---|
| `kubernetes` | Apply manifests to a K8s cluster. | Stable |
| `docker-host` | Deploy as a `docker run` on a managed Host. | Partial |
| `cloud-run` | Deploy a service to Google Cloud Run. | Stable when configured |
| `ecs` | Deploy a service to AWS ECS / Fargate. | Stable when configured |
| `fly` | Deploy a Fly.io machine. | Stable when configured |
| `render` | Trigger a deploy on a pre-created Render service. | Stable when configured |

Each cloud target self-registers at boot when its config block is non-empty. For example, the ECS target only registers when both `COOKER_DEPLOY_ECS_REGION` and `COOKER_DEPLOY_ECS_CLUSTER` are set.

See [Reference: env-vars](../reference/env-vars.md#deploy-targets) for the full list of required variables per target.

### Choosing a deploy target

Set `DeployTarget.Kind` on the App. The other fields depend on the kind:

| Kind | Fields you set on the App |
|---|---|
| `kubernetes` | `namespace` (defaults to `default`). |
| `docker-host` | `hostId` — references a managed [Host](#hosts) record. |
| `cloud-run` | `region`, `service` (the Cloud Run service name). |
| `ecs` | `service` (the ECS service name). |
| `fly` | `region`. |
| `render` | (no extra fields; the service is keyed by App name in the Render owner account). |

> **Partial.** The frontend wizard for non-Kubernetes targets is rudimentary. Today most operators wire cloud targets by `PUT /api/v1/apps/:id` with a hand-crafted JSON body. Tracked under W11 indie/SaaS gaps.

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
| `tailnet` | Host is only reachable over a Tailscale tailnet Cooker joins via `tsnet`. | **Build-tagged.** Default builds do NOT include the tsnet transport; you need `-tags tsnet` (see [`docs/UAT.md`](../../UAT.md#what-works-right-now)). |

> **Partial.** The Hosts page in the frontend is not yet a menu item — the API works but the UI is incomplete. Test via:
>
> ```bash
> curl -X POST http://localhost:8080/api/v1/hosts \
>      -H 'Content-Type: application/json' \
>      -d '{"name":"prod-docker","kind":"docker","reachability":"direct","dockerEndpoint":"tcp://10.0.0.3:2375"}'
> ```
>
> Source: [`docs/UAT.md`](../../UAT.md#scenario-5--managed-hosts).

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

For non-K8s targets, the App's own `DeployTarget` overrides this. An App with `DeployTarget.Kind = cloud-run` ignores its Environment's K8s target fields.

## Cross-references

- **[Apps](apps.md)** — how `DeployTarget` is set on an App.
- **[Environments](environments.md)** — how K8s deploys are scoped.
- **[Kubernetes deploy](../guides/kubernetes-deploy.md)** — wiring kubeconfigs and RBAC.
- **[Reference: env vars](../reference/env-vars.md#deploy-targets)** — every cloud target's required config.
