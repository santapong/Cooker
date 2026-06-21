<!-- DRAFT dev.to article -->

# The Single-Binary Deploy Story: A Tour of Cooker's Helm Chart

*Tags: kubernetes, helm, devops, selfhosted*

---

Cooker is a CI/CD tool that ships as a single Go binary. That binary serves the HTTP API and the React frontend on port 8080, speaks WebSocket for live log streaming, and runs the DAG executor that builds OCI images and deploys to Kubernetes. It is self-hosted, Apache-2.0-licensed, and has no SaaS component.

This article walks through the Helm chart for operators who want to understand what they're deploying before they deploy it. The chart lives at `deploy/helm/cooker/` in the repo; the values discussed here are all from `values.yaml`.

---

## What the Chart Installs

A standard Cooker deployment with `helm install cooker oci://ghcr.io/santapong/charts/cooker` creates:

- One Deployment (Cooker binary, runs as UID 65532)
- One Service (ClusterIP, port 8080)
- One Ingress (disabled by default; you enable it with `ingress.enabled=true`)
- A NetworkPolicy (disabled by default; `networkPolicy.enabled=true`)
- A PodDisruptionBudget (disabled by default)
- An HPA (disabled by default)
- RBAC objects for the builder you've selected (Role + RoleBinding for Kaniko or Buildah Jobs)
- Optional: a ServiceMonitor for Prometheus scraping, PrometheusRules for alerting

No CRDs. No operators. No webhook admission controllers. The footprint is a Deployment and its supporting objects.

---

## The `cookerEnv` Value: Why It Matters at Boot

```yaml
cookerEnv: production
```

This maps to the `COOKER_ENV` environment variable. In production mode, `Config.Validate()` runs at startup and refuses to boot if the configuration is unsafe. Specifically, it rejects:

- `COOKER_BUILDER=docker` (the docker socket builder mounts the host socket; refused in production)
- Multi-replica configuration with memory backends and `COOKER_STICKY_SESSIONS=false`
- A database secrets backend without a `COOKER_SECRET_KEY`
- Non-localhost Postgres without `sslmode>=require`
- OIDC enabled with a non-HTTPS redirect URL

The chart defaults to `cookerEnv: production`. The effect is that misconfiguration fails loudly at boot — you see the error in `kubectl logs` — rather than silently misbehaving at runtime. This is the right default for a production deployment. If you're doing a quick test on a cluster that isn't fully wired up, set `cookerEnv: uat` to skip the strict checks.

---

## Secrets: Two Patterns

**Pattern 1: Reference a pre-created Secret (recommended)**

```yaml
secretKey:
  existingSecret: "cooker-secret-key"
  existingSecretKey: "key"
```

Create the secret before installing the chart:

```bash
kubectl create secret generic cooker-secret-key \
  --from-literal=key=$(head -c 32 /dev/urandom | base64)
```

The chart renders this as `secretKeyRef` in the Deployment's env block. The value never appears in the chart values, in `helm history`, or in any log.

**Pattern 2: Inline value (not recommended for production)**

```yaml
secretKey:
  value: "base64-encoded-32-byte-key-here"
```

This is convenient for development clusters. It stores the key in a Kubernetes Secret that the chart creates, but the value ends up in `helm history` and in any Secret export. The comment in `values.yaml` is explicit about this.

The same two-pattern design applies to the OIDC client secret (`oidc.clientSecretRef`) and the KeepSave API key (`secrets.keepsave.apiKey.existingSecret`).

---

## Builder Selection

```yaml
builder:
  kind: kaniko
```

The builder is selected at deploy time. Three production-viable options:

**Kaniko** (default): submits a Kubernetes Job per build that runs `gcr.io/kaniko-project/executor`. No host docker socket. The Cooker pod and the Kaniko Job share a PVC for the build context. The chart creates a Role and RoleBinding granting Cooker's ServiceAccount `create`/`get`/`watch` on Jobs and Pods in the builder namespace. If you don't set `builder.kaniko.contextPVC`, the chart falls back to `emptyDir`, which means the build context won't contain your source — useful only for chart smoke tests.

**Buildah**: same shape as Kaniko but runs `quay.io/buildah/buildah`. Full Dockerfile feature parity including `RUN --mount=type=cache` and heredoc syntax. Trade-off: rootless Buildah needs `CAP_SETUID` and `CAP_SETGID`, so the build namespace must be Pod Security Admission `baseline` or a custom profile. The `restricted` standard drops both capabilities and the build will fail.

**Docker**: uses the host docker socket. The chart renders the volume and mount only when `builder.kind=docker`. This is refused at boot when `cookerEnv=production`. Dev and UAT only.

Pin builder images to specific release tags:

```yaml
builder:
  kaniko:
    image: gcr.io/kaniko-project/executor:v1.23.2
  buildah:
    image: quay.io/buildah/buildah:v1.38.0
```

The chart comments flag `latest` as a risk for production: a new builder release can change build behaviour between deploys without any change on your side.

---

## Multi-Replica: What Needs to Be Shared

Cooker has three pieces of internal state that are per-process by default:

| State | Default | Multi-replica option |
|---|---|---|
| Rate limiter | In-memory (`golang.org/x/time/rate`) | Redis GCRA via `go-redis/redis_rate` |
| WebSocket tickets | `sync.Map` with TTL | Atomic Redis `GETDEL` |
| WebSocket broadcast hub | In-process channels | Redis pub/sub (`cooker:ws:broadcast`) |

The chart defaults all three to Redis-backed:

```yaml
wsHub:
  backend: redis
wsTicket:
  backend: redis
rateLimit:
  backend: redis
redis:
  enabled: true
  url: "redis://cooker-redis:6379"
```

With `redis.enabled: true`, the chart deploys a single-node Redis instance. For production, replace this with an external Redis (Elasticache, Cloud Memorystore, a managed instance) by setting `redis.enabled: false` and updating `redis.url`.

If you genuinely need single-replica only, set all three backends to `memory` and set `replicaCount: 1`. The chart will reject a multi-replica configuration with memory backends unless `stickySessions: true` is set, which tells `Config.Validate` that the ingress is pinning clients to one replica.

---

## Network Policy

```yaml
networkPolicy:
  enabled: false
```

Disabled by default because enabling it on a cluster without Calico, Cilium, or another CNI that enforces NetworkPolicy will silently do nothing — and operators should make that choice explicitly. When enabled, the generated policy permits:

- Ingress from the ingress controller namespace to port 8080
- Egress to Postgres and Redis on their respective ports
- Egress to the Kubernetes API server on 443 (for `client-go`)
- Egress to OCI registries on 443 (for push)

Any builder Jobs (Kaniko, Buildah) are launched in a separate namespace and are not covered by this policy — they have their own ServiceAccount and may need separate egress rules for registry push.

---

## Security Posture

The Deployment template sets:

```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 65532
  readOnlyRootFilesystem: true
  allowPrivilegeEscalation: false
```

UID 65532 is the non-root user baked into the multi-stage Dockerfile. The `readOnlyRootFilesystem: true` constraint means any file write at runtime (temporary files, build context staging) must go to a volume or an `emptyDir` — the chart mounts an `emptyDir` at `/tmp` for this purpose.

Container images are cosign-signed on release. The `SECURITY-RELEASE-VERIFY.md` playbook documents how to verify signatures before deploying.

---

## Operator Checklist Before Going Live

1. Pin `image.tag` to a specific release tag, not `latest`.
2. Create secrets out-of-band (`cooker-oidc`, `cooker-secret-key`) before `helm install`.
3. Set `cookerEnv: production` — this is already the default.
4. Enable ingress with TLS. OIDC won't work without HTTPS redirect URLs.
5. Set `networkPolicy.enabled: true` if your CNI supports it.
6. Check `kubectl -n cooker logs deploy/cooker` immediately after install — any `Config.Validate` failure surfaces here with an exact error message.
7. Run `curl https://cooker.example.com/health/ready` and confirm all three checks (DB, Redis, JWKS) return healthy.

---

## What the Chart Does Not Do

- It does not create Postgres. Bring your own.
- It does not configure your OIDC provider. Register the redirect URL and create the client secret yourself.
- It does not migrate existing pipelines from one secrets backend to another. Switching `secrets.backend` requires a manual copy step.
- It does not configure the ingress controller itself (NGINX, ALB, Traefik) — only the Ingress object.

The chart installs Cooker. The surrounding infrastructure is yours to manage.

Try it: `docker compose up` — repo at github.com/santapong/Cooker
