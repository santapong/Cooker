# Installing Cooker

**Status:** install guide. **Grounding:** [`docs/launch/00-sre-sla-readiness.md`](../launch/00-sre-sla-readiness.md) §3.4 / §5 (INF-1), `CLAUDE.md` (project layout), the Helm chart at `deploy/helm/cooker/`.

---

## Supported install path: Helm (authoritative)

> **Helm is the supported, authoritative install path for Cooker.** The Helm chart at
> `deploy/helm/cooker/` is the source of truth for production-ready defaults — pod
> `securityContext`, non-root UID 65532, `secretKeyRef` for the OIDC client secret and
> `COOKER_SECRET_KEY`, `NetworkPolicy`, PodDisruptionBudget, probes, and environment
> wiring. **Install Cooker with Helm.**

The raw manifests under **`deploy/kubernetes/`** are **reference parity** — kept in sync
with the chart so non-Helm users have a worked example, but **Helm is authoritative**. The
raw-manifest path has historically lagged the chart's safety guards (missing
`startupProbe`, `COOKER_SECRET_KEY`, `COOKER_ENV`, PDB, image-tag pinning — see
`docs/launch/00-sre-sla-readiness.md` §3.4 / IN-H1..H6). If you deviate from Helm, you own
re-applying those guards yourself.

| Install path | Status | When to use |
|---|---|---|
| **Helm chart** (`deploy/helm/cooker/`) | **Supported & authoritative** | All production and most non-trivial deployments. |
| Raw manifests (`deploy/kubernetes/`) | **Reference parity** (kept in sync; Helm is authoritative) | Learning, air-gapped GitOps that templates its own values, or environments that cannot run Helm. You are responsible for parity with the chart's safety defaults. |
| Single binary / container (no orchestration) | Dev / evaluation only | Local trials, UAT (`make uat-up`). Not a production shape. |

## Quick start (Helm)

```sh
# From the repo root. Review and override values before a real install.
helm install cooker ./deploy/helm/cooker \
  --namespace cooker --create-namespace \
  --values my-values.yaml
```

At minimum, set in `my-values.yaml` (see the chart's `values.yaml` for the full surface):

- **Secrets via `secretKeyRef`** — `COOKER_SECRET_KEY` (envelope-encryption KEK) and, if
  OIDC is enabled, the OIDC client secret. **Never put secrets in `values.yaml`.**
- **`COOKER_ENV`** — `production` enables strict CORS defaults and `Config.Validate()`
  startup checks. Use `dev`/`uat` only for non-production.
- **OIDC** — issuer, client id, and the `secretKeyRef` for the client secret.
- **Licensing (paid tiers only)** — set the `license:` values block: `license.publicKey`
  (single base64 Ed25519 public key) or `license.publicKeys` (comma-separated list, for
  zero-downtime key rotation — see `docs/guides/UAT.md` → *Self-hosted licensing*). The
  license **token is sensitive**, so supply it via `license.tokenSecret.{name,key}`
  (an existing `Secret` referenced by `secretKeyRef`) rather than inline. **Never put the
  token in `values.yaml`.** With no license configured, Cooker runs on the Free tier.
- **Postgres** — connection settings; production enforces TLS (`sslmode=require`+).
- **HA (for any availability target)** — `≥ 2` replicas + PDB, and the Redis-backed
  rate-limit / WS-ticket / WS-hub backends (see `docs/guides/MULTI_REPLICA.md`).

## Before you go to production

This guide covers *install*; production readiness is broader. Pair it with:

- `SECURITY.md` — the production security checklist (OIDC on, TLS, scoped RBAC, secrets
  backend, container hardening).
- `docs/guides/RUNBOOK.md` — operational procedures and failure scenarios.
- `docs/guides/ROLLOUT.md` — phased rollout + smoke checks.
- `docs/guides/MULTI_REPLICA.md` — HA backends (required for any availability SLO).
- `docs/launch/00-sre-sla-readiness.md` — what a real availability target requires
  (dashboards, alerts, a *proven* restore drill, on-call).
- `docs/legal/sla.md` — the availability targets you can honestly claim per tier.

> **Note on `startupProbe` (INF-1):** the chart should carry a `startupProbe` so slow-boot
> clusters don't SIGKILL the pod before it's ready. If you use the raw manifests, confirm
> this guard is present; the chart is authoritative for it.

---

**Related:** `deploy/helm/cooker/` · `deploy/kubernetes/` (reference parity) · `SECURITY.md` · `docs/guides/RUNBOOK.md` · `docs/guides/ROLLOUT.md` · `docs/guides/MULTI_REPLICA.md`
