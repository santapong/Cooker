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

## Editions: LIGHT vs FULL

Two curated deploy postures, expressed purely as env/values presets (every Cooker
feature is env-toggled; the core CI/CD loop — pipelines, runs, apps, environments,
WebSockets — is always on and has no toggle):

| | **LIGHT** (main features) | **FULL** (everything) |
|---|---|---|
| Containers (docker) | cooker + postgres | cooker + postgres + **redis** |
| ws-hub / ws-ticket / rate-limit | memory (single replica) | redis (multi-replica-ready) |
| Job queue + cron scheduler | off | **on** (`COOKER_JOBQUEUE_ENABLED`, `COOKER_SCHEDULER_ENABLED`) |
| Metrics / tracing | off | **on** (`/metrics`, OTLP) |
| Audit trail | stdout | **stdout + db** with 90-day retention sweep |
| Build/push/deploy (k8s) | noop | **kaniko + crane + client-go** (canary-capable) |
| AI triage / feedback / cloud inventory | off | on once keys provided (documented in the preset) |

Commands:

```sh
make deploy-docker-light    # docker-compose.prod.yml only
make deploy-docker-full     # + docker-compose.full.yml overlay (adds Redis)
make deploy-k8s-light       # helm -f values-light.yaml
make deploy-k8s-full        # helm -f values-full.yaml
```

Presets live in `deploy/editions/{light,full}.env.example` (compose) and
`deploy/helm/cooker/values-{light,full}.yaml` (Helm). The compose targets keep the
preset between marker lines in `.env.prod`, so **switching editions in place** is just
running the other target — secrets and data volumes are preserved. On Helm,
`helm upgrade` with the other values file does the same.

Note: license tiers (`free`/`crew`/`constellation` in the entitlements system) are a
separate, currently-inert billing concept — editions here are purely about which
infrastructure/features run.

## Easy deploy (one command)

Two zero-wiring paths generate their own secrets and bundle their own datastores, so you can
stand Cooker up without pre-provisioning a database or crafting secrets. Both boot the strict
**production** posture (`COOKER_ENV=production`) with local username/password auth enabled — sign
up at `/signup` on first run.

**Docker (single host)** — splits the service into `cooker` + `postgres` + `redis` containers via
[`docker-compose.prod.yml`](../../docker-compose.prod.yml). First run writes `.env.prod` with fresh
random secrets (DB password, AES-256 secret key, local-auth JWT signing key):

```sh
make deploy-docker            # -> http://localhost:8080  (sign up at /signup)
make deploy-docker-logs
make deploy-docker-down
```

Postgres runs with a self-signed cert so the app connects over `sslmode=require` (mandatory for a
non-localhost DB host in production). Image builds are not wired by default (the docker-socket
builder is forbidden in production); point Cooker at a cluster with `COOKER_BUILDER=kaniko` or use
the UAT socket-proxy stack for non-production build testing.

**Kubernetes (one command)** — the `values-quickstart.yaml` preset bundles a Postgres StatefulSet +
Redis and autogenerates all secrets:

```sh
make deploy-k8s               # helm upgrade --install with the quickstart preset
kubectl port-forward svc/cooker 8080:8080
```

Both are convenience paths for evaluation and small installs. For production at scale, provision
Postgres/Redis externally, use a real IdP (OIDC), and manage secrets out-of-band via
`existingSecret` references — see the authoritative Helm section below.

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
