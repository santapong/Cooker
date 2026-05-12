# Kubernetes deploy target

The Kubernetes deploy target applies workloads to a cluster. The adapter is selected by `COOKER_DEPLOYER`:

| Value | Implementation | Status |
|---|---|---|
| `kubectl` | Shell out to the bundled `kubectl` binary against `KUBECONFIG`. | Stable, simple. |
| `clientgo` | Use `k8s.io/client-go`'s dynamic client + server-side apply. | Stable, production-recommended. |
| `noop` | Accept the deploy and do nothing. | Test only. |

`clientgo` is the production default in the Helm chart. `kubectl` is fine for UAT and dev.

## Three ways to wire credentials

### 1. In-cluster (Cooker pod's ServiceAccount)

The standard production pattern. Cooker reads `/var/run/secrets/kubernetes.io/serviceaccount/` and authenticates as its own pod.

Set:

```yaml
# values.yaml
extraEnv:
  - name: COOKER_K8S_IN_CLUSTER
    value: "true"
```

The chart provisions a `ServiceAccount`, `ClusterRole`, and `ClusterRoleBinding` for Cooker. Default RBAC permissions:

- `apps/v1` Deployments, StatefulSets, DaemonSets: get, list, watch, create, update, patch, delete
- `core/v1` Pods, Services, ConfigMaps, Secrets, Namespaces: get, list, watch, create, update, patch, delete
- `batch/v1` Jobs (for Kaniko / Buildah builders): create, get, list, watch, delete

> **Known finding.** The default RBAC is **cluster-wide**, not namespace-scoped. The Helm value `rbac.clusterWide: true` is the default for v0.1 compatibility. Tighten via `rbac.clusterWide: false` + a `Role`/`RoleBinding` per target namespace; this is `S26-05-19` in the [security review](../../audits/2026-05-security-review.md).

### 2. Kubeconfig file mounted into the pod

For deploys to a separate cluster:

```yaml
extraEnv:
  - name: KUBECONFIG
    value: /etc/kubeconfigs/prod
extraVolumes:
  - name: kubeconfigs
    secret:
      secretName: cooker-kubeconfigs
extraVolumeMounts:
  - name: kubeconfigs
    mountPath: /etc/kubeconfigs
    readOnly: true
```

The Secret keys are the context filenames. Switch contexts at the [Environment](../concepts/environments.md) level via `EnvironmentTarget.KubeContext`.

### 3. Per-host kubeconfig (managed Hosts)

For [Managed Hosts](../concepts/hosts-and-targets.md#hosts) of kind `kubernetes`, store the kubeconfig as a secret referenced by `Host.KubeconfigRef`. The deployer fetches and uses it per-deploy.

## Choosing namespace and context

Two layers:

- **Environment.Target** (`EnvironmentTarget`):
  - `type=namespace` + `namespace=cooker-staging` → deploys land in `cooker-staging` of the running cluster.
  - `type=cluster` + `clusterId=<id>` + `namespace=app-ns` → dial a separately-configured cluster, deploy to `app-ns`.
- **EnvironmentTarget.KubeContext** — optional kubeconfig context name. When set, overrides the default context.

For App deploys, `App.DeployTarget.Namespace` overrides the environment's namespace.

## What Deploy stages actually do

A Deploy stage runs one of:

- `kubectl apply -f <manifestPath>` if `COOKER_DEPLOYER=kubectl`. Server-side apply if your kubectl is 1.27+.
- A dynamic-client server-side apply if `COOKER_DEPLOYER=clientgo`.
- A `helm template <chart> | kubectl apply -f -` flow if `helmChart` is set.

The deployer waits for the workload to become Available (Deployment) or Ready (StatefulSet, DaemonSet, Pod). The wait timeout is the stage timeout (`StageConfig.Timeout`), defaulting to the global `COOKER_RUN_DEADLINE`.

Failure modes:

- The `apply` fails. Stage status = `failed`, error text = `kubectl`/API-server output.
- The apply succeeds but the workload never goes Ready. Stage hits its timeout, status = `failed`, error = `timed out waiting for rollout`.

## ImagePullSecrets

Cooker does **not** automatically create `imagePullSecrets` for the deployed workload. If your registry is private, you need to:

1. Create a `kubernetes.io/dockerconfigjson` Secret in the target namespace.
2. Reference it from the workload's `spec.template.spec.imagePullSecrets`.

For ECR / GCR / GAR, use the upstream solutions:

- ECR: [`amazon-ecr-credential-helper`](https://github.com/awslabs/amazon-ecr-credential-helper) or a CronJob that refreshes the secret.
- GCR / GAR: Workload Identity (one-time setup, no Cooker config).
- GHCR: A `kubernetes.io/dockerconfigjson` Secret created once per namespace.

> **Partial.** Roadmap `A5` / `A6` will make Cooker generate the appropriate secret for IRSA / Workload Identity automatically. Until then, manual creation is the path.

## Namespaces and RBAC

If you deploy across multiple namespaces, you need either:

- A `ClusterRole` that allows the verbs Cooker needs (cluster-wide default; see warning above), or
- A separate `Role` and `RoleBinding` in each target namespace.

The chart's `rbac.yaml` template renders the cluster-wide form by default. For namespace-scoped:

```yaml
# values.yaml
rbac:
  clusterWide: false
  namespaces:
    - cooker-dev
    - cooker-staging
    - cooker-prod
```

The chart then renders one `Role` + `RoleBinding` per listed namespace.

## Resource limits / requests on deployed workloads

These come from your manifests, not from Cooker. Cooker is a deployment tool, not a quota manager.

For Cooker's own pod limits, see [Self-hosting tips](self-hosting-tips.md#resource-requests).

## Health probes after deploy

Once the Deploy stage completes, `AppHealthChecker` (see [Apps](../concepts/apps.md#health-checks)) polls the deployed workload every `COOKER_APP_HEALTH_INTERVAL` (default 30s) and writes `App.HealthStatus`. The prober for Kubernetes reads Deployment status (replicas Available vs DesiredReplicas) — a Deployment with all replicas Ready is `healthy`, mid-rollout is `degraded`, no replicas serving is `failed`.

> **Partial.** Custom readiness checks (HTTP probe URL, expected status) are not yet wired into the App config. Today the prober's verdict is purely based on K8s Deployment status. Custom probes are a planned extension.

## Cross-references

- **[Hosts and deploy targets](../concepts/hosts-and-targets.md)** — when to use which target kind.
- **[Environments](../concepts/environments.md)** — target configuration.
- **[Docker builds](../operations/docker-builds.md)** — building the image you're about to deploy.
- **[Auth & RBAC](../operations/auth-and-rbac.md)** — Cooker's own RBAC (separate from K8s RBAC).
