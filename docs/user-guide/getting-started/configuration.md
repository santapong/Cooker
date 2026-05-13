# Configuration

All Cooker configuration is environment-variable-based today. There is no config file yet — adding a YAML overlay is on the [roadmap](https://github.com/santapong/cooker/blob/main/docs/shipping-go.md#4-configuration-story) but not yet shipped.

For the full enumerated list, see [Reference: Environment variables](../reference/env-vars.md).

## `COOKER_ENV`

This single variable changes Cooker's defaults dramatically.

| Value | What changes |
|---|---|
| `dev` *(default)* | Lenient defaults: localhost CORS allowlist, no fatal startup checks, dev admin user injected when auth is off. |
| `uat` | Same as `dev` for defaults, used by the compose stack. Documented separately so testers can distinguish "I'm running UAT" from "I'm hacking locally." |
| `production` | Strict defaults: deny-all CORS, fatal startup checks for missing `COOKER_SECRET_KEY` / `COOKER_ALLOWED_ORIGINS`, refusal to boot with `COOKER_BUILDER=docker`. |

The Helm chart defaults to `cookerEnv: production`. The compose stack sets `COOKER_ENV=uat`.

## Dev mode (auth off)

When `COOKER_OIDC_ENABLED=false` AND `COOKER_LOCAL_AUTH_ENABLED=false`, the auth middleware injects a synthetic admin user (`dev-user`) on every request. This is intentional:

- It lets `make uat-up` boot end-to-end without configuring an IdP.
- It lets contributors hit the API from `curl` during development.

`Config.Validate()` emits a `slog.Warn` if BOTH are disabled in production. It does **not** refuse to boot today — operators who set `COOKER_ENV=production` but forget `COOKER_OIDC_ENABLED=true` ship an open-admin service. Closing this is `S26-05-07` in the [security review](../../audits/2026-05-security-review.md).

Never run dev mode on a network anyone else can reach.

## Validation (production)

When `COOKER_ENV=production`, `Config.Validate()` runs at boot and refuses to start if any of the following fail:

| Check | Source |
|---|---|
| `DATABASE_URL` is set and not the dev default | `config.go:367-371` |
| `COOKER_SECRET_KEY` (base64-decoded) is ≥ 32 bytes — unless `COOKER_SECRETS_BACKEND=keepsave` | `config.go:378-389` |
| `COOKER_ALLOWED_ORIGINS` is non-empty and not `*` | `config.go:420-424` |
| `COOKER_BUILDER != docker` | `config.go:452-454` |
| Secrets-backend-specific config is present (KeepSave URL, Vault addr, GCP project, etc.) | `config.go:391-419` |
| When `COOKER_REPLICA_COUNT > 1`: either `COOKER_STICKY_SESSIONS=true`, or rate limiter / WS ticket / WS hub backends are all `redis` | `config.go:459-475` |
| If `COOKER_LOCAL_AUTH_ENABLED=true`: `COOKER_LOCAL_AUTH_JWT_SIGNING_KEY` decodes to ≥ 32 bytes | `config.go:428-436` |
| If `COOKER_AUDIT_ENABLED=true` with `destination=file`: `COOKER_AUDIT_FILE_PATH` is set | `config.go:437-448` |

A typical first-boot failure looks like:

```text
{"time":"...","level":"ERROR","msg":"invalid configuration","err":"config: COOKER_SECRET_KEY is required in production; COOKER_ALLOWED_ORIGINS is required in production (no permissive default)"}
```

Set the missing variables and restart.

## Pluggable backends

Cooker picks adapters at boot from env vars. Unknown values fall back to `noop` with a log line (so a typo doesn't crash boot — but pipelines silently do nothing).

| Family | Env var | Values |
|---|---|---|
| Builder | `COOKER_BUILDER` | `noop`, `docker`, `kaniko`, `buildah`, `buildkit` |
| Pusher | `COOKER_PUSHER` | `noop`, `docker`, `crane` |
| Deployer | `COOKER_DEPLOYER` | `noop`, `kubectl`, `clientgo` |
| Secrets | `COOKER_SECRETS_BACKEND` | `database` *(default)*, `keepsave`, `vault`, `aws`, `gcp` |
| Rate limiter | `COOKER_RATE_LIMIT_BACKEND` | `memory` *(default)*, `redis` |
| WS ticket store | `COOKER_WS_TICKET_BACKEND` | `memory` *(default)*, `redis` |
| WS hub | `COOKER_WS_HUB_BACKEND` | `memory` *(default)*, `redis` |
| Audit destination | `COOKER_AUDIT_DESTINATION` | `stdout` *(default)*, `file` |

Stub-only at the moment: `buildkit` is partially implemented (not wired) per [`docs/UAT.md`](../../UAT.md#what-works-right-now).

## Secrets backend selection

The secrets backend determines where Cooker stores environment secret VALUES. Switching does not auto-migrate — plan a one-shot copy step.

| Backend | When | Storage | Required vars |
|---|---|---|---|
| `database` *(default)* | Single-node installs | `environments.secrets` JSONB column | `COOKER_SECRET_KEY` |
| `keepsave` | Multi-tenant / audit-heavy | KeepSave server | `COOKER_SECRETS_KEEPSAVE_URL` + `_PROJECT_ID` + `_API_KEY` |
| `vault` | Existing HashiCorp Vault | KV v2 mount | `COOKER_SECRETS_VAULT_ADDR` (+ `_TOKEN` unless Vault Agent injects) |
| `aws` | AWS-native (EKS / ECS) | Secrets Manager | `COOKER_SECRETS_AWS_REGION` (auto-discoverable on EC2) |
| `gcp` | GCP-native (GKE / Cloud Run) | Secret Manager | `COOKER_SECRETS_GCP_PROJECT_ID` |

See [Secrets](../guides/secrets.md) for the per-backend setup.

## Observability (opt-in)

Both off by default — set these to turn them on.

```bash
# Prometheus /metrics on the same port as the API.
COOKER_METRICS_ENABLED=true

# OpenTelemetry traces over OTLP/gRPC.
COOKER_TRACING_ENABLED=true
COOKER_OTLP_ENDPOINT=otel-collector.observability.svc.cluster.local:4317
COOKER_OTLP_INSECURE=true     # in-cluster OTLP rarely uses TLS
COOKER_SERVICE_NAME=cooker
COOKER_SERVICE_VERSION=v0.1.0
```

See [Observability](../operations/observability.md) for the metric and span names.

## Defaults you almost always want to change in production

| Variable | Default | Production guidance |
|---|---|---|
| `COOKER_ENV` | `dev` | `production` |
| `COOKER_OIDC_ENABLED` | `false` | `true` (and configure the rest of `COOKER_OIDC_*`) |
| `COOKER_BUILDER` | `noop` | `kaniko` or `buildah` |
| `COOKER_PUSHER` | `noop` | `crane` |
| `COOKER_DEPLOYER` | `noop` | `clientgo` |
| `COOKER_RATE_LIMIT_BACKEND` | `memory` | `redis` if `replicaCount>1` |
| `COOKER_AUDIT_ENABLED` | `true` in production, else `false` | leave on |

## Reload semantics

There is no hot reload. Every change to a `COOKER_*` env var requires a process restart (in K8s, a pod rollout). This is intentional — see [`docs/shipping-go.md` §4](../../shipping-go.md#4-configuration-story).

## Cross-references

- **[Reference: Environment variables](../reference/env-vars.md)** — every variable with default and source citation.
- **[Auth & RBAC](../operations/auth-and-rbac.md)** — OIDC setup specifics.
- **[Secrets](../guides/secrets.md)** — per-backend wiring.
