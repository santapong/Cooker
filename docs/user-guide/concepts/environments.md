# Environments

An Environment in Cooker is a named deploy destination plus the variables and secrets that target uses. The model is `model.Environment` (`backend/internal/model/environment.go`).

Cooker doesn't assume how many environments you have or what they're called. Common shapes are 3 (Dev / Staging / Prod) or 2 (Staging / Prod). One is fine for indie projects.

## Fields

| Field | Purpose |
|---|---|
| `id`, `name` | Identity. Name should be unique within your install (no DB constraint, but the UI assumes uniqueness). |
| `order` | Promotion order. Lower numbers are upstream. By convention Dev=1, Staging=2, Prod=3. |
| `target` | `EnvironmentTarget` — where deploys land. |
| `plainVars` | Non-sensitive `{key: value}` map. Visible to any authenticated user. |
| `secrets` | Sealed values. Never serialised; redacted to `secretKeys` for client responses. |
| `promotion` | `PromotionPolicy` — how runs move from this env to the next. |
| `version` | Optimistic concurrency token. |

## Targets

`EnvironmentTarget` (`model.EnvironmentTarget`):

| Field | Purpose |
|---|---|
| `type` | `"cluster"` or `"namespace"`. Cluster targets dial a separate kubeconfig context; namespace targets reuse the running cluster. |
| `clusterId` | References a `RegistryConfig` cluster ID configured via the Settings page (`POST /api/v1/settings/clusters`). |
| `namespace` | K8s namespace where Deploy stages will apply. |
| `kubeContext` | Optional kubeconfig context name override. |

The deploy adapter (`COOKER_DEPLOYER`) is global per Cooker install, not per environment. The per-environment knob is *where* it deploys, not *how*. For non-K8s deploy targets (Cloud Run, ECS, Fly, Render) the App's `DeployTarget.Kind` overrides this.

## Variables and secrets

Two distinct namespaces:

- **`plainVars`** — non-sensitive. Stored as plain text in the JSONB column. Anyone with read access to the environment sees these. Use for things like `LOG_LEVEL=info`, `NODE_ENV=production`, image tag pins.
- **`secrets`** — sensitive. Stored according to the configured [Secrets backend](../guides/secrets.md). Never serialised back to clients — only the *key names* are returned in `secretKeys`.

Both are merged into a stage's runtime env at execution time. Precedence:

```text
StageConfig.Env  >  Environment.PlainVars / Secrets  >  Pipeline.Variables
```

`StageConfig.SecretRefs` names which secret keys from the stage's `EnvironmentID` to inject; the executor decrypts and adds them to the stage's env before run.

## Promotion policy

`PromotionPolicy` controls how a run moves from one environment to the next:

| Field | Purpose |
|---|---|
| `strategy` | `"auto"` — promote automatically when upstream succeeds. `"manual"` — pause and wait for approval. |
| `requiredApprovers` | Manual strategy only. Today: 1 approver. Multi-approver gating is partial. |
| `autoPromoteOn` | List of conditions that must hold for `auto` (e.g. `["tests_pass", "health_check"]`). |

> **Partial.** `requiredApprovers > 1` is on the model but not yet wired in the approver flow — approval from one approver always proceeds. Track as a feature gap; raise an issue if you hit it.

Approval API:

```bash
POST /api/v1/pipelines/:id/runs/:runId/approve
```

The handler requires `approver` or `admin` role. The approving user's `Subject` claim is recorded in `EnvironmentStatus.ApprovedBy` for audit.

See [Promotions](../guides/promotions.md) for the walkthrough.

## Status during a run

Each environment in a run has an `EnvironmentStatus` (`model.EnvironmentStatus`):

| Status | Meaning |
|---|---|
| `pending` | Not yet reached. |
| `deploying` | Stages assigned to this env are running. |
| `deployed` | All stages in this env succeeded. |
| `failed` | At least one stage failed. |
| `awaiting_approval` | Manual promotion gate; waiting for `POST /approve`. |

The frontend's EnvironmentBar shows these live via the run's WebSocket channel.

## CRUD endpoints

| Operation | Endpoint | Role |
|---|---|---|
| List | `GET /api/v1/environments` | any authenticated |
| Create | `POST /api/v1/environments` | operator / admin |
| Update | `PUT /api/v1/environments/:id` | operator / admin |
| Delete | `DELETE /api/v1/environments/:id` | admin (with MFA gate if configured) |
| Put secret | `PUT /api/v1/environments/:id/secrets/:key` | admin (with MFA gate) |
| Reveal secret | `GET /api/v1/environments/:id/secrets/:key` | admin (with MFA gate) |
| Delete secret | `DELETE /api/v1/environments/:id/secrets/:key` | admin (with MFA gate) |
| Promote secrets | `POST /api/v1/environments/:id/secrets/promote` | admin (with MFA gate) |

Reveal returns the plaintext. Listing environments NEVER returns secret values, only `secretKeys`.

## Multi-tenancy

Today, every authenticated user can read every environment's metadata (target, namespace, plainVars, secretKeys — but NOT secret values). The redaction is one-deep: secret values are never serialised, but the rest is exposed.

This is intentional but visible: `S26-05-09` in the [security review](../../audits/2026-05-security-review.md) documents it and `C1` on the [roadmap](https://github.com/santapong/cooker/blob/main/docs/roadmap-2026.md) tracks the multi-tenancy ADR.

## Cross-references

- **[Apps](apps.md)** — how Apps reference Environments.
- **[Promotions](../guides/promotions.md)** — manual approval flow walkthrough.
- **[Secrets](../guides/secrets.md)** — backend selection and rotation.
