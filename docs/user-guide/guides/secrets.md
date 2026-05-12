# Secrets

Cooker stores environment-scoped secrets and injects them into the runtime env of stages that reference them. Five storage backends are supported, picked at boot via `COOKER_SECRETS_BACKEND`.

The handler API is identical across backends — switching is a config change, not a code change. The storage layout, encryption, and trust model differ.

## Backend selection

| Backend | When | Storage | Encryption | Required vars |
|---|---|---|---|---|
| `database` *(default)* | Single-Cooker installs; no separate secrets infra | `environments.secrets` JSONB column | AES-GCM via `COOKER_SECRET_KEY` | `COOKER_SECRET_KEY` (base64 32 bytes) |
| `keepsave` | Multi-tenant; want a dedicated secrets server | KeepSave server | AES-256-GCM managed by KeepSave | `COOKER_SECRETS_KEEPSAVE_URL`, `_PROJECT_ID`, `_API_KEY` |
| `vault` | Existing HashiCorp Vault deployment | Vault KV v2 | Vault-managed | `COOKER_SECRETS_VAULT_ADDR` (+ `_TOKEN` unless Vault Agent injects) |
| `aws` | AWS-native deployment | AWS Secrets Manager | KMS | `COOKER_SECRETS_AWS_REGION` (auto-detected on EC2) |
| `gcp` | GCP-native deployment | GCP Secret Manager | Google-managed | `COOKER_SECRETS_GCP_PROJECT_ID` |

For backend-specific knobs see [Reference: env vars](../reference/env-vars.md#secrets-backend).

## `database` (default)

Secrets are AES-256-GCM-sealed before persisting to the `environments.secrets` JSONB column. The encryption key is `COOKER_SECRET_KEY`, base64-encoded 32 bytes.

Generate one:

```bash
head -c 32 /dev/urandom | base64
```

In production with this backend, `COOKER_SECRET_KEY` is required and validated at boot. With no key set, the secrets API returns `503 Service Unavailable` so the operator notices the gap rather than silently storing plaintext.

> **Known gap.** No dual-key rotation path. Rotating `COOKER_SECRET_KEY` invalidates every previously sealed secret. Plan a one-shot read-with-old-key, write-with-new-key step before changing the key. Tracked as `S26-05-08`.

## `keepsave`

A single KeepSave project owns all of Cooker's secrets. Cooker's environment **name** (`prod`, `uat`, etc.) maps to KeepSave's `environment` query parameter; per-environment isolation comes from KeepSave's per-env API-key scoping.

```bash
COOKER_SECRETS_BACKEND=keepsave
COOKER_SECRETS_KEEPSAVE_URL=https://keepsave:8080
COOKER_SECRETS_KEEPSAVE_PROJECT_ID=<cooker-project-uuid>
COOKER_SECRETS_KEEPSAVE_API_KEY=ks_xxxx
```

With this backend, `COOKER_SECRET_KEY` is not required — KeepSave handles encryption. `Config.Validate()` rejects partial KeepSave config in production (any one of the three vars missing is fatal).

KeepSave is a Cooker-team product. See [`docs/shipping-go.md` §4.5](../../shipping-go.md#4-configuration-story) for the candid take.

### Promotion

KeepSave's `/promote` endpoint maps to Cooker's `POST /api/v1/environments/:id/secrets/promote`, which copies a subset of keys from this environment to another. This is the path the SaaS-team persona uses to move secrets through Dev -> Staging -> Prod without re-typing them.

## `vault`

HashiCorp Vault KV v2:

```bash
COOKER_SECRETS_BACKEND=vault
COOKER_SECRETS_VAULT_ADDR=https://vault.example.com:8200
COOKER_SECRETS_VAULT_MOUNT=secret           # KV v2 mount path
COOKER_SECRETS_VAULT_PREFIX=cooker          # path under <mount>
COOKER_SECRETS_VAULT_TOKEN=$(cat /vault/secrets/token)
```

Each Cooker environment maps to one Vault secret at `<mount>/data/<prefix>/<envID>` with one field per Cooker key. Vault handles encryption and audit.

Vault Agent injector works: empty `_TOKEN` is allowed when the SDK's chain finds the token elsewhere.

## `aws`

AWS Secrets Manager:

```bash
COOKER_SECRETS_BACKEND=aws
COOKER_SECRETS_AWS_REGION=us-east-1
COOKER_SECRETS_AWS_PREFIX=cooker
```

Auth via the standard AWS chain: IRSA on EKS, instance profile on EC2, env vars locally. One AWS secret per Cooker key — keeps per-key versioning and IAM scoping clean. Secret IDs are `<prefix>/<envID>/<key>`.

## `gcp`

GCP Secret Manager:

```bash
COOKER_SECRETS_BACKEND=gcp
COOKER_SECRETS_GCP_PROJECT_ID=my-gcp-project
COOKER_SECRETS_GCP_PREFIX=cooker
```

Auth via Application Default Credentials (Workload Identity on GKE / `GOOGLE_APPLICATION_CREDENTIALS` elsewhere). Secrets are named `<prefix>__<envID>__<key>` — the double-underscore separator works around GCP's `[A-Za-z0-9_-]` naming rule.

## CRUD operations

All four are admin-only and gated by MFA (when configured):

```bash
# Set / update
curl -X PUT https://cooker.example.com/api/v1/environments/<ENV_ID>/secrets/DB_PASSWORD \
     -H 'Authorization: Bearer <jwt>' \
     -H 'Content-Type: application/json' \
     -d '{"value":"supersecret"}'

# Reveal (returns plaintext)
curl https://cooker.example.com/api/v1/environments/<ENV_ID>/secrets/DB_PASSWORD \
     -H 'Authorization: Bearer <jwt>'

# Delete
curl -X DELETE https://cooker.example.com/api/v1/environments/<ENV_ID>/secrets/DB_PASSWORD \
     -H 'Authorization: Bearer <jwt>'

# Promote a set of keys to another environment
curl -X POST https://cooker.example.com/api/v1/environments/<DEV_ENV_ID>/secrets/promote \
     -H 'Authorization: Bearer <jwt>' \
     -H 'Content-Type: application/json' \
     -d '{"toEnvironmentId":"<PROD_ENV_ID>","keys":["DB_PASSWORD","API_KEY"]}'
```

Listing environments NEVER returns secret values — only the `secretKeys` array.

## Injecting secrets into stages

In the stage's `StageConfig`:

```json
{
  "secretRefs": ["DB_PASSWORD", "API_KEY"]
}
```

The executor resolves these against the stage's `environmentId` Environment just before run and injects them as env vars in the stage's runtime. The stage never sees ciphertext.

## Audit

Every `PUT` / `DELETE` / `REVEAL` / `PROMOTE` of a secret produces an audit event when `COOKER_AUDIT_ENABLED=true`. The event records the timestamp, OIDC subject, route template (`/api/v1/environments/:id/secrets/:key` — never the concrete `:key`), status code, and latency. **Bodies are never captured** by audit middleware, so the secret value cannot end up in the audit log even by mistake. See [Audit logging](../../../SECURITY.md#audit-logging).

## Backend switching

Switching `COOKER_SECRETS_BACKEND` does **not** migrate existing secrets. Read-and-write at runtime use a single backend; there is no live dual-write.

To migrate, do this once before flipping the env var:

```bash
# Pseudocode — write your own script.
for env in $(curl ... | jq -r '.[].id'); do
  for key in $(curl ... | jq -r '.secretKeys[]'); do
    value=$(curl .../secrets/$key | jq -r '.value')  # via old backend
    # Save value somewhere, then restart Cooker with the new backend,
    # then PUT it back via the same endpoint.
  done
done
```

Test the migration in a non-prod environment first. Rotation accidents are recoverable from your DB backup; the secrets themselves usually aren't.

## Cross-references

- **[Environments](../concepts/environments.md)** — where secrets live conceptually.
- **[Auth & RBAC](../operations/auth-and-rbac.md)** — who can `PUT` / `REVEAL`.
- **[Reference: env vars](../reference/env-vars.md#secrets-backend)** — every backend-specific variable.
- **[`SECURITY.md` § Audit logging](../../../SECURITY.md#audit-logging)** — what's logged when secrets are touched.
