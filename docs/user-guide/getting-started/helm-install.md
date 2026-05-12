# Helm install (production)

Cooker ships a Helm chart at `deploy/helm/cooker/`. This page covers a production install: real OIDC, TLS at ingress, the `kaniko` builder (no host docker.sock), and the production-mode config gates.

For local trial, use the [Quickstart](quickstart.md) compose stack instead.

## Prerequisites

- **Kubernetes 1.27+** with a working ingress controller (nginx, Traefik, or ALB). Cooker does not terminate TLS itself.
- **PostgreSQL 14+**, reachable from the cluster. Cooker does not bundle a Postgres subchart at the moment — bring your own (managed RDS, Cloud SQL, on-prem) or run one in-cluster separately.
- **OIDC provider** with PKCE support. Google, Keycloak, Okta, Azure AD (Entra), and KeepSave are tested.
- **An issued OIDC client** with redirect URI `https://<your-host>/callback`.
- **A registry** Cooker can push to. Anything OCI-compliant works: GHCR, ECR, Quay, Harbor, Docker Hub, a self-hosted Distribution.
- **`helm` 3.13+** and `kubectl` configured.

## Step 1 — Pre-create the Kubernetes Secrets

Cooker reads its OIDC client secret and the AES-256 secret key from Kubernetes Secrets, not from chart values. This keeps the chart usable in clusters that scan rendered manifests for sensitive strings.

```sh
kubectl create namespace cooker

# OIDC client secret (PKCE means the browser doesn't need this; the
# backend uses it during userinfo lookups for some providers).
kubectl -n cooker create secret generic cooker-oidc \
  --from-literal=client-secret=<value-from-idp>

# AES-256 key for the database-backed secrets backend. 32 bytes,
# base64-encoded.
kubectl -n cooker create secret generic cooker-secret-key \
  --from-literal=key=$(head -c 32 /dev/urandom | base64)

# TLS cert (skip if cert-manager will provision it automatically).
kubectl -n cooker create secret tls cooker-tls \
  --cert=path/to/fullchain.pem --key=path/to/privkey.pem
```

## Step 2 — Install the chart

```sh
helm install cooker deploy/helm/cooker/ \
  --namespace cooker \
  --set cookerEnv=production \
  --set 'oidc.allowedOrigins={https://cooker.example.com}' \
  --set oidc.enabled=true \
  --set oidc.issuerUrl=https://auth.example.com \
  --set oidc.clientId=cooker \
  --set oidc.clientSecretRef.name=cooker-oidc \
  --set oidc.redirectUrl=https://cooker.example.com/callback \
  --set secretKey.existingSecret=cooker-secret-key \
  --set 'ingress.tls[0].secretName=cooker-tls' \
  --set 'ingress.tls[0].hosts[0]=cooker.example.com'
```

This sets the production defaults you almost certainly want:

- `cookerEnv=production` — enables [`Config.Validate()`](../reference/env-vars.md#validation) startup checks (fails fast on missing CORS origins, weak keys, etc.).
- `builder.kind=kaniko` — chart default. Build Jobs run in-cluster; no `/var/run/docker.sock` mount.
- `oidc.enabled=true` — refuse to boot with auth off in production. (Today a stray `false` value emits `slog.Warn` and continues; closing-the-loop is finding `S26-05-07` in the [security review](../../audits/2026-05-security-review.md).)

## Step 3 — Verify

```sh
kubectl -n cooker get pods
# NAME                       READY   STATUS    RESTARTS   AGE
# cooker-67d77b4cf6-2zsx4    1/1     Running   0          1m

kubectl -n cooker logs -l app=cooker | jq -r '.msg'
# "cooker starting"
# "store: postgres migrations applied"

curl https://cooker.example.com/health/live
# {"status":"ok"}
```

Sign in as a member of one of the configured OIDC groups (see [Auth & RBAC](../operations/auth-and-rbac.md#group-to-role-mapping)).

## Mandatory production values

The chart defaults to `cookerEnv: production`. With that env, `Config.Validate()` will refuse to start unless ALL of the following are set:

| Setting | Why |
|---|---|
| `DATABASE_URL` (via `extraEnv` or a Secret) | No default in production; backend has no in-memory fallback when `cookerEnv=production`. |
| `COOKER_SECRET_KEY` (≥ 32 bytes, base64) | Required unless `secrets.backend=keepsave`. AES-256 key for secret-at-rest. |
| `COOKER_ALLOWED_ORIGINS` | No permissive default; missing config is a fatal startup error. |
| `COOKER_BUILDER != docker` | Production refuses the host docker.sock path. |

See [Validation](../reference/env-vars.md#validation) for the full list.

## TLS at ingress

OIDC sign-in **requires HTTPS** — most IdPs reject non-HTTPS redirect URIs. Cooker doesn't terminate TLS itself; the ingress controller (or cloud load balancer) does.

The canonical pattern is cert-manager + Let's Encrypt:

```yaml
# values.yaml
ingress:
  enabled: true
  className: nginx
  hosts:
    - host: cooker.example.com
      paths: [{ path: /, pathType: Prefix }]
  tls:
    - secretName: cooker-tls
      hosts: [cooker.example.com]
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
```

The chart does NOT install cert-manager for you; install it separately (`helm install cert-manager jetstack/cert-manager`).

## PostgreSQL SSL

Production must not connect to Postgres over plaintext.

| Connection style | How to enforce TLS |
|---|---|
| Managed (RDS, Cloud SQL, on-prem) — full `DATABASE_URL` | Append `?sslmode=require` (or `verify-full` if you mount a CA bundle). |
| In-cluster Postgres pod (bring-your-own) | Configure your Postgres pod with TLS, then `?sslmode=require` in `DATABASE_URL`. |

Example via `--set`:

```sh
helm install cooker deploy/helm/cooker/ \
  --set 'extraEnv[0].name=DATABASE_URL' \
  --set 'extraEnv[0].value=postgres://user:pass@db.internal:5432/cooker?sslmode=require'
```

> **Known gap.** `Config.Validate()` does NOT currently reject `?sslmode=disable` in production. The [security review](../../audits/2026-05-security-review.md) tracks this as `S26-05-10`; until it lands, the operator must set this correctly by hand.

## Multi-replica

Two pieces of state are per-process by default: the rate limiter and the WebSocket ticket store. Running two or more replicas requires either:

- **Sticky sessions** at the ingress (simpler) — set `COOKER_STICKY_SESSIONS=true`, add the ingress annotations from [`docs/MULTI_REPLICA.md`](../../MULTI_REPLICA.md), and you're done.
- **Redis-backed state** (proper HA) — set `rateLimit.backend=redis`, `wsTickets.backend=redis`, `wsHub.backend=redis`. The chart provisions a Redis sidecar by default; an external `REDIS_URL` works too.

`Config.Validate()` will refuse to start if `replicaCount>1` without one of these.

See [Self-hosting tips](../guides/self-hosting-tips.md#multi-replica) for the failure modes you'd see without either.

## Builder selection

The chart default is `builder.kind=kaniko`. The full table is in [Docker builds](../operations/docker-builds.md). The short version:

| Kind | When |
|---|---|
| `kaniko` *(default)* | Production. In-cluster Job, no docker.sock. |
| `buildah` | Production. Full Dockerfile parity (heredocs, `--mount=type=cache`). Needs PSA `baseline` or custom. |
| `buildkit` | **Stub** — not wired in this release. Tracked in roadmap `A12`. |
| `docker` | Dev only. Refused by `Config.Validate()` in production. |

## Networking

The chart ships a `NetworkPolicy` (gated by `networkPolicy.enabled`). Defaults allow ingress from any pod in Cooker's namespace; egress is 443 to anywhere except RFC1918 ranges (so outbound to OCI registries and IdPs works).

> **Known limitation.** The "any same-namespace pod" ingress default is intentionally loose for compatibility. Tighten via `networkPolicy.ingressNamespaceLabel` if you want to restrict to the ingress controller's namespace only. Tracked as `S26-05-20` in the [security review](../../audits/2026-05-security-review.md).

## Upgrading

See [Upgrading](upgrading.md).

## What you should reach for next

- **[Auth & RBAC](../operations/auth-and-rbac.md)** — wire your IdP groups to Cooker roles.
- **[Postgres](../operations/postgres.md)** — backup, migration semantics, the boot-time advisory lock.
- **[Observability](../operations/observability.md)** — `/metrics`, OTLP traces, `/healthz` vs `/health/ready`.
