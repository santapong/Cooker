# Environment variables

Every Cooker configuration variable. Generated from `backend/internal/config/config.go`. The `Source` column cites the file:line where each default lives.

## General

| Variable | Default | Description | Source |
|---|---|---|---|
| `COOKER_ENV` | `dev` | Deployment environment. One of `dev`, `uat`, `production`. Production gates strict defaults. | `config.go:243` |
| `COOKER_PORT` | `8080` | HTTP port. | `config.go:250` |
| `DATABASE_URL` | `postgres://cooker:cooker@localhost:5432/cooker?sslmode=disable` | Postgres connection string. Empty = in-memory store (tests / dev only). | `config.go:251` |
| `REDIS_URL` | `redis://localhost:6379` | Redis connection string. Only used when any `*_BACKEND=redis`. | `config.go:252` |
| `COOKER_ALLOWED_ORIGINS` | `http://localhost:5173,http://localhost:3000` (dev/uat); empty (production) | CSV of allowed CORS origins. Production refuses empty AND refuses `*`. | `config.go:253` |
| `COOKER_REGISTRY` | `localhost:5000/cooker` | Default registry prefix when a Push stage doesn't override. | `config.go:255` |

## Secrets (AES key)

| Variable | Default | Description | Source |
|---|---|---|---|
| `COOKER_SECRET_KEY` | empty | Base64-encoded 32-byte AES-256 key. Required in production unless `COOKER_SECRETS_BACKEND=keepsave`. | `config.go:254` |

## Backend selectors

| Variable | Default | Values | Description | Source |
|---|---|---|---|---|
| `COOKER_BUILDER` | `noop` | `noop`, `docker`, `kaniko`, `buildah`, `buildkit` | Image builder. Production refuses `docker`. | `config.go:256` |
| `COOKER_PUSHER` | `noop` | `noop`, `docker`, `crane` | Registry pusher. | `config.go:257` |
| `COOKER_DEPLOYER` | `noop` | `noop`, `kubectl`, `clientgo` | K8s deployer. | `config.go:258` |
| `COOKER_SECRETS_BACKEND` | `database` | `database`, `keepsave`, `vault`, `aws`, `gcp` | Secrets storage backend. | `config.go:259` |

## Multi-replica

| Variable | Default | Description | Source |
|---|---|---|---|
| `COOKER_REPLICA_COUNT` | `1` | Replica count. Validated against per-process state. | `config.go:260` |
| `COOKER_STICKY_SESSIONS` | `false` | Operator confirms ingress affinity is configured. Allows memory-backed state at replica>1. | `config.go:261` |
| `COOKER_RATE_LIMIT_BACKEND` | `memory` | `memory` or `redis`. | `config.go:266` |
| `COOKER_WS_TICKET_BACKEND` | `memory` | `memory` or `redis`. | `config.go:269` |
| `COOKER_WS_HUB_BACKEND` | `memory` | `memory` or `redis`. Redis enables pub/sub fan-out across replicas. | `config.go:272` |

## Rate limit tuning

| Variable | Default | Description | Source |
|---|---|---|---|
| `COOKER_RATE_LIMIT_ENABLED` | `true` | Per-user rate limit on expensive endpoints. | `config.go:263` |
| `COOKER_RATE_LIMIT_PER_MINUTE` | `10` | Requests per minute per user. | `config.go:264` |
| `COOKER_RATE_LIMIT_BURST` | `3` | Bucket capacity for short bursts. | `config.go:265` |
| `COOKER_WEBHOOK_RATE_LIMIT_ENABLED` | `true` | Per-source-IP rate limit on the unauthenticated `/webhooks/*` receivers (independent of the per-user limiter). | `config.go` |
| `COOKER_WEBHOOK_RATE_LIMIT_PER_MINUTE` | `60` | Webhook requests per minute per source IP. | `config.go` |
| `COOKER_WEBHOOK_RATE_LIMIT_BURST` | `10` | Webhook bucket capacity for short bursts. | `config.go` |

> Today the per-user rate limiter only applies to three endpoints: `POST /pipelines/:id/run`, `POST /docker/images/build`, `POST /apps/:id/deploy`. The `/webhooks/*` receivers have their own per-source-IP limiter (`COOKER_WEBHOOK_RATE_LIMIT_*`). See [`SECURITY.md` § Rate limiting](../../../SECURITY.md#rate-limiting) and [§ Git-provider webhook receivers](../../../SECURITY.md#git-provider-webhook-receivers) for details.

## OIDC

| Variable | Default | Description | Source |
|---|---|---|---|
| `COOKER_OIDC_ENABLED` | `false` | Enable OIDC PKCE auth. | `config.go:275` |
| `COOKER_OIDC_ISSUER_URL` | empty | IdP issuer base URL — `<url>/.well-known/openid-configuration` must be reachable. | `config.go:276` |
| `COOKER_OIDC_CLIENT_ID` | empty | OIDC client ID. | `config.go:277` |
| `COOKER_OIDC_CLIENT_SECRET` | empty | OIDC client secret (PKCE doesn't need it; some IdPs require it for token introspection). | `config.go:278` |
| `COOKER_OIDC_REDIRECT_URL` | empty | Must exactly match what's registered with the IdP. | `config.go:279` |
| `COOKER_OIDC_GROUP_MAP` | empty (built-in defaults) | CSV of `group:role` pairs. See [Auth & RBAC](../operations/auth-and-rbac.md#group-to-role-mapping). | `config.go:281` |
| `COOKER_OIDC_MFA_ACR_VALUES` | empty (gate disabled) | CSV of acceptable `acr` values for step-up MFA gate. | `config.go:282` |

## Local auth

| Variable | Default | Description | Source |
|---|---|---|---|
| `COOKER_LOCAL_AUTH_ENABLED` | `false` | Enable `/api/v1/auth/local/*` endpoints. | `config.go:285` |
| `COOKER_LOCAL_AUTH_JWT_SIGNING_KEY` | empty | Base64 or raw; ≥32 bytes after decode. Required when local auth is enabled. | `config.go:286` |
| `COOKER_LOCAL_AUTH_TOKEN_TTL` | `12h` | Local auth JWT TTL. | `config.go:287` |
| `COOKER_LOCAL_AUTH_ALLOW_SIGNUP` | `true` | When false, `/signup` returns 403; UI hides the form. | `config.go:288` |

## Docker (host daemon)

| Variable | Default | Description | Source |
|---|---|---|---|
| `DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker daemon socket. Used by the `docker` builder/pusher. | `config.go:291` |
| `DOCKER_TLS_VERIFY` | `false` | Verify TLS when DOCKER_HOST is `tcp://`. | `config.go:292` |
| `DOCKER_CERT_PATH` | empty | TLS cert dir when DOCKER_TLS_VERIFY=true. | `config.go:293` |

## Kubernetes

| Variable | Default | Description | Source |
|---|---|---|---|
| `COOKER_K8S_IN_CLUSTER` | `false` | Use the pod's ServiceAccount token. | `config.go:296` |
| `KUBECONFIG` | empty | Path to kubeconfig file when not in-cluster. | `config.go:297` |
| `COOKER_K8S_NAMESPACE` | `cooker` | Namespace where Cooker creates Kaniko / Buildah build Jobs. | `config.go:298` |

## Kaniko builder

| Variable | Default | Description | Source |
|---|---|---|---|
| `COOKER_KANIKO_IMAGE` | `gcr.io/kaniko-project/executor:latest` | Kaniko executor image. | `config.go:299` |
| `COOKER_KANIKO_SERVICE_ACCOUNT` | empty | ServiceAccount for the Kaniko Job. Empty = namespace default. | `config.go:300` |
| `COOKER_KANIKO_CONTEXT_PVC` | empty | PVC mounted at the build-context path. Required in production. | `config.go:301` |

## Buildah builder

| Variable | Default | Description | Source |
|---|---|---|---|
| `COOKER_BUILDAH_IMAGE` | `quay.io/buildah/stable:latest` | Buildah image. | `config.go:302` |
| `COOKER_BUILDAH_SERVICE_ACCOUNT` | empty | ServiceAccount for the Buildah Job. | `config.go:303` |
| `COOKER_BUILDAH_CONTEXT_PVC` | empty | PVC mounted at the build-context path. | `config.go:304` |
| `COOKER_BUILDAH_STORAGE_DRIVER` | `vfs` | `vfs` (no kernel mods) or `overlay` (needs fuse-overlayfs). | `config.go:305` |

## Secrets backend

### `keepsave`

| Variable | Default | Description | Source |
|---|---|---|---|
| `COOKER_SECRETS_KEEPSAVE_URL` | empty | KeepSave server base URL. Must be `https://` in production. | `config.go:308` |
| `COOKER_SECRETS_KEEPSAVE_PROJECT_ID` | empty | UUID of the KeepSave project that owns Cooker's secrets. | `config.go:309` |
| `COOKER_SECRETS_KEEPSAVE_API_KEY` | empty | `X-API-Key` value. | `config.go:310` |

### `vault`

| Variable | Default | Description | Source |
|---|---|---|---|
| `COOKER_SECRETS_VAULT_ADDR` | empty | Vault address (HTTPS). | `config.go:313` |
| `COOKER_SECRETS_VAULT_TOKEN` | empty | Vault token. Can be empty when Vault Agent injects it via the SDK's chain. | `config.go:314` |
| `COOKER_SECRETS_VAULT_MOUNT` | `secret` | KV v2 mount path. | `config.go:315` |
| `COOKER_SECRETS_VAULT_PREFIX` | `cooker` | Path prefix under the mount. | `config.go:316` |

### `aws`

| Variable | Default | Description | Source |
|---|---|---|---|
| `COOKER_SECRETS_AWS_REGION` | empty | AWS region. Auto-discoverable from instance metadata. | `config.go:319` |
| `COOKER_SECRETS_AWS_PREFIX` | `cooker` | Secret ID prefix: `<prefix>/<envID>/<key>`. | `config.go:320` |

### `gcp`

| Variable | Default | Description | Source |
|---|---|---|---|
| `COOKER_SECRETS_GCP_PROJECT_ID` | empty | GCP project ID. Required when backend=gcp. | `config.go:323` |
| `COOKER_SECRETS_GCP_PREFIX` | `cooker` | Secret name prefix: `<prefix>__<envID>__<key>`. | `config.go:324` |

## Deploy targets

Each cloud deploy target self-registers when its required config is non-empty.

### Cloud Run

| Variable | Default | Description | Source |
|---|---|---|---|
| `COOKER_DEPLOY_CLOUDRUN_PROJECT` | empty | GCP project for Cloud Run. | `config.go:327` |
| `COOKER_DEPLOY_CLOUDRUN_REGION` | empty | GCP region. | `config.go:328` |

### ECS / Fargate

| Variable | Default | Description | Source |
|---|---|---|---|
| `COOKER_DEPLOY_ECS_REGION` | empty | AWS region. | `config.go:329` |
| `COOKER_DEPLOY_ECS_CLUSTER` | empty | ECS cluster name. | `config.go:330` |
| `COOKER_DEPLOY_ECS_EXECUTION_ROLE` | empty | Task execution role ARN. | `config.go:331` |
| `COOKER_DEPLOY_ECS_TASK_ROLE` | empty | Task role ARN. | `config.go:332` |
| `COOKER_DEPLOY_ECS_SUBNETS` | empty (CSV) | Subnet IDs for Fargate ENI. | `config.go:333` |
| `COOKER_DEPLOY_ECS_SECURITY_GROUPS` | empty (CSV) | Security group IDs. | `config.go:334` |

### Fly.io

| Variable | Default | Description | Source |
|---|---|---|---|
| `COOKER_DEPLOY_FLY_TOKEN` | empty | Fly API token. | `config.go:335` |
| `COOKER_DEPLOY_FLY_REGION` | empty | Default Fly region. | `config.go:336` |

### Render

| Variable | Default | Description | Source |
|---|---|---|---|
| `COOKER_DEPLOY_RENDER_TOKEN` | empty | Render API token. | `config.go:337` |
| `COOKER_DEPLOY_RENDER_OWNER_ID` | empty | Render account owner ID. | `config.go:338` |

## Audit log

| Variable | Default | Description | Source |
|---|---|---|---|
| `COOKER_AUDIT_ENABLED` | `true` in production, else `false` | Emit one structured event per authenticated mutating call. | `config.go:341` |
| `COOKER_AUDIT_DESTINATION` | `stdout` | `stdout` or `file`. | `config.go:342` |
| `COOKER_AUDIT_FILE_PATH` | empty | Required when destination=file. No rotation — use a sidecar log shipper. | `config.go:343` |

## Observability

| Variable | Default | Description | Source |
|---|---|---|---|
| `COOKER_METRICS_ENABLED` | `false` | Expose `/metrics` (Prometheus format). | `config.go:346` |
| `COOKER_TRACING_ENABLED` | `false` | Enable OTLP/gRPC trace export. | `config.go:347` |
| `COOKER_OTLP_ENDPOINT` | empty | OTLP collector address (`host:port`). | `config.go:348` |
| `COOKER_OTLP_INSECURE` | `false` | Use plaintext OTLP (in-cluster only). | `config.go:349` |
| `COOKER_SERVICE_NAME` | `cooker` | OTel `service.name`. | `config.go:350` |
| `COOKER_SERVICE_VERSION` | `dev` | OTel `service.version`. | `config.go:351` |

## App health

| Variable | Default | Description | Source |
|---|---|---|---|
| `COOKER_APP_HEALTH_INTERVAL` | `30s` | How often `AppHealthChecker` probes each App. `0` disables. | `config.go:353` |

## Feedback

| Variable | Default | Description | Source |
|---|---|---|---|
| `COOKER_FEEDBACK_GITHUB_TOKEN` | empty (feature off) | GitHub token used to file in-app feedback as issues. Use a fine-grained PAT scoped to Issues Read/Write on the one feedback repo. Server-side only — never sent to the browser. Empty keeps `POST /feedback` returning 503 and hides the frontend button. | `config.go` |
| `COOKER_FEEDBACK_GITHUB_REPO` | `santapong/Cooker` | `owner/repo` that receives feedback issues. | `config.go` |

## Frontend (build-time)

These are baked into the JS bundle at `npm run build`, not read at runtime. Setting them in Helm values requires rebuilding the image.

| Variable | Description |
|---|---|
| `VITE_OIDC_ENABLED` | `true` to render the OIDC sign-in flow. |
| `VITE_OIDC_AUTHORITY` | Issuer URL (same as `COOKER_OIDC_ISSUER_URL`). |
| `VITE_OIDC_CLIENT_ID` | Client ID. |
| `VITE_OIDC_REDIRECT_URI` | Redirect URI. |
| `VITE_OIDC_POST_LOGOUT_REDIRECT_URI` | Where to land after logout. |

> **TODO: verify** the full list of `VITE_OIDC_*` variables — they live in `frontend/src/vite-env.d.ts` and `frontend/src/auth/`. <!-- TODO: verify -->

## Validation (production)

`Config.Validate()` runs when `COOKER_ENV=production` and refuses to start if any of these fail. See [Configuration: Validation](../getting-started/configuration.md#validation-production).

| Check | Behaviour |
|---|---|
| `DATABASE_URL` non-empty and not the dev default | Fatal |
| `COOKER_SECRET_KEY` base64-decodes to ≥32 bytes (unless `COOKER_SECRETS_BACKEND=keepsave`) | Fatal |
| `COOKER_ALLOWED_ORIGINS` non-empty AND not `*` | Fatal |
| `COOKER_BUILDER != docker` | Fatal |
| Per-secrets-backend required fields | Fatal |
| Per-replica multi-replica safety (sticky sessions OR redis backends) | Fatal |
| `COOKER_OIDC_ENABLED=true` OR `COOKER_LOCAL_AUTH_ENABLED=true` | Warn-only (will become Fatal — `S26-05-07`) |
| Local auth signing key length | Fatal |
| Audit destination = file requires `COOKER_AUDIT_FILE_PATH` | Fatal |
| `DATABASE_URL` sslmode != disable | Should be Fatal but currently isn't (`S26-05-10`) |

## Cross-references

- **[Configuration](../getting-started/configuration.md)** — narrative version with examples.
- **[`backend/internal/config/config.go`](https://github.com/santapong/cooker/blob/main/backend/internal/config/config.go)** — the source.
