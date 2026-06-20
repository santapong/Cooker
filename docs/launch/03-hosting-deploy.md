# 03 — Hosting and Deployment Recommendation

> **Advisory, date-stamped 2026-06-20.** All prices are rough estimates with
> inline sources retrieved 2026-06-11 (from `docs/guides/DEPLOY-AWS-VERCEL.md`)
> unless noted. Re-verify at apply time. Engineering state of record:
> `backlog.md` and `docs/audits/launch-readiness.md`. This document interprets
> the stated intent ("test on Vercel first, then GCP/AWS/Azure and more") and
> gives an honest structural assessment. It does not modify any artifact.

---

## 1. The Vercel Reality

### What Vercel is and what Cooker needs

Vercel is a serverless/edge platform optimised for statically-built frontends
and short-lived serverless functions. Cooker's architecture is the opposite of
that shape:

| Cooker requirement | Vercel capability | Verdict |
|---|---|---|
| Long-running Go binary (persistent state: in-flight runs, WS hub) | No persistent process; functions time out at 120s | Cannot host |
| WebSocket connections (live build logs) | No WebSocket support in Serverless Functions; rewrites do not proxy WS upgrades (source: vercel.com/kb; vercel.com/docs limits) | Cannot host |
| In-cluster Kaniko Jobs (submits `batch/v1.Job` to Kubernetes) | No Kubernetes API access; no Job model | Cannot host |
| PostgreSQL connection (long-lived pool) | No persistent socket pool | Cannot host |
| Redis pub/sub (multi-replica WS hub, ticket store, rate limiter) | Not available | Cannot host |
| Vite SPA (static HTML/JS/CSS, all routing client-side) | Core use case; per-PR preview deployments; `vercel.json` SPA-routing config already ships at `deploy/vercel/vercel.json` | Works perfectly |

**Bottom line: Vercel can host the React frontend only.** The Go binary, every API route, every WebSocket path, all build Jobs, and all data must run elsewhere.

### The workable split-origin shape

This is not a workaround. The codebase already supports it:

- `frontend/src/api/origin.ts` exports `API_ORIGIN` (from `VITE_API_BASE_URL`)
  and `wsBase()`, which derive the correct `https://` fetch base and
  `wss://` WebSocket authority from a single build-time variable.
- `frontend/src/api/client.ts` computes `API_BASE = ${API_ORIGIN}/api/v1`.
  Every `fetch` call goes through this — no backend URL appears in any
  component, satisfying the "no backend URLs in components" rule in `CLAUDE.md`.
- When `VITE_API_BASE_URL` is unset the SPA falls back to same-origin
  (relative paths), so the single-binary default deployment is unaffected.

```
Browser
  │
  ├── HTTPS  GET /  →  Vercel (SPA assets, per-PR preview URLs)
  │
  ├── HTTPS  /api/v1/*  →  backend host (Caddy/ingress/ALB → Cooker :8080)
  │
  └── WSS    /ws/*      →  backend host (same reverse-proxy; Caddy passes
                           WS Upgrade transparently; Nginx / ALB need
                           explicit WS configuration)
```

### Required wiring for a split deploy

**Build-time Vercel environment variables** (set in Vercel → Project → Settings
→ Environment Variables; changes require a redeploy because Vite bakes them in):

| Variable | Value |
|---|---|
| `VITE_API_BASE_URL` | `https://<backend-hostname>` (e.g. `https://api.cooker.example.com`) |
| `VITE_OIDC_ENABLED` | `true` on the fixed/stable domain; `false` on per-PR previews |
| `VITE_OIDC_AUTHORITY` | IdP issuer URL (stable domain only) |
| `VITE_OIDC_CLIENT_ID` | client id registered at the IdP (stable domain only) |
| `VITE_OIDC_REDIRECT_URI` | `https://<stable-vercel-domain>/auth/callback` (exact, registered at IdP) |

**Backend env config** (on the host running Cooker):

| Variable | Value notes |
|---|---|
| `COOKER_ALLOWED_ORIGINS` | Exact Vercel production domain + stable UAT domain. In `COOKER_ENV=uat` only, wildcard `*` is permitted to serve per-PR preview hostnames (Vercel mints a new hostname per PR). Wildcard is rejected at boot under `COOKER_ENV=production`. |
| `COOKER_OIDC_REDIRECT_URL` | Must match the exactly-registered redirect URI at the IdP. Google and Keycloak ≥ 24.0.3 do not accept wildcard redirect URIs — per-PR preview hostnames cannot do OIDC. |

**Why previews cannot use OIDC**: Vercel mints a new unguessable hostname per
PR (e.g. `cooker-abc123-scope.vercel.app`). Neither Google nor Keycloak ≥ 24.0.3
accept a wildcard redirect URI across hostnames. The documented workaround
(see `deploy/vercel/README.md`) is to run previews with `VITE_OIDC_ENABLED=false`
against a shared UAT backend using local email+password auth, which is legal in
`COOKER_ENV=uat` but not in production.

**WebSocket proxy requirement**: any reverse proxy in front of the backend must
forward the `Upgrade: websocket` and `Connection: Upgrade` headers.

- Caddy: passes WS upgrades by default — no extra config needed.
- Nginx: requires `proxy_http_version 1.1; proxy_set_header Upgrade $http_upgrade; proxy_set_header Connection "upgrade";`
- AWS ALB: supports WebSockets but requires `idle_timeout.timeout_seconds` to
  exceed Cooker's WS ping interval (~54s). The guide documents `300s` as the
  safe value (`docs/guides/DEPLOY-AWS-VERCEL.md` §3.3).

---

## 2. Cloud Comparison

Cooker's architectural requirements set the baseline for any cloud deployment:

- Kubernetes (for in-cluster Kaniko/Buildah Jobs and the Kaniko context PVC)
- Managed PostgreSQL or a self-managed Postgres with SSL
- Redis (for multi-replica WS hub, ticket store, and rate limiter — required when `replicaCount > 1`)
- ReadWriteMany storage (EFS on AWS / Filestore on GCP / Azure Files) for the Kaniko build context PVC shared between the Cooker pod and Kaniko Jobs
- TLS ingress (OIDC IdPs require HTTPS redirect URIs; the chart refuses `production + oidc + ingress` without `ingress.tls`)
- A container registry (ECR / Artifact Registry / ACR or any OCI registry)

### Why Cloud Run cannot run the backend

Cloud Run is a managed serverless container platform with a request-driven model.
It does not support:

- Persistent WebSocket connections at scale (it can handle WS but instances
  scale to zero and connections are dropped)
- Submitting Kubernetes Jobs (there is no in-cluster Kubernetes API — Cloud Run
  has no concept of a shared Job namespace)
- A ReadWriteMany PVC for the Kaniko build context

Cloud Run could notionally serve the SPA (though Vercel is a better choice for
previews), but the Go backend must run on Kubernetes or a long-lived VM.

### Per-cloud assessment

| Dimension | AWS (EKS + existing Terraform) | GCP (GKE) | Azure (AKS) |
|---|---|---|---|
| **IaC status** | Complete — tiered Terraform at `deploy/aws/terraform/` (Starter/Team/Scale tfvars, modules for network/cluster/data/registry/observability) | Not present — no `deploy/gcp/` directory | Not present — no `deploy/azure/` directory |
| **Managed Postgres** | RDS PostgreSQL (gp3, multi-AZ at Team+; `sslmode=require` via the data module) | Cloud SQL for PostgreSQL | Azure Database for PostgreSQL |
| **Managed Redis/Valkey** | ElastiCache Serverless Valkey (Team+; `rediss://` TLS; unit-ambiguity OPEN for ECPU pricing — verify before committing) | Cloud Memorystore for Redis | Azure Cache for Redis |
| **Kubernetes** | EKS Auto Mode — manages compute (Karpenter), ALB controller, EBS CSI; EFS CSI installed as an add-on; 21-day max node lifetime (Bottlerocket) | GKE Autopilot or Standard; Workload Identity native | AKS with Managed Identity |
| **Workload identity (no static keys)** | Pod Identity (Terraform-wired in `modules/cluster/main.tf`; `serviceAccount.annotations` stays `{}`) | Workload Identity Federation | Azure Workload Identity |
| **Build context storage (RWX)** | EFS (Elastic throughput, lifecycle → IA 14d; provisioned in `modules/data/main.tf`) | Filestore (NFS) | Azure Files (SMB/NFS) |
| **Container registry** | ECR + pull-through cache for Docker Hub (provisioned in `modules/registry/main.tf`) | Artifact Registry | Azure Container Registry |
| **Ingress / TLS** | ALB via Auto Mode's load-balancer controller; ACM for certs; `idle_timeout=300s` required for WS | GKE Ingress / Gateway API; cert-manager or managed certs | Application Gateway Ingress Controller or nginx-ingress; cert-manager |
| **Secrets** | Secrets Manager (escrow key + DB password + OIDC client secret; Pod Identity read-scoped to exact ARNs) | Secret Manager | Azure Key Vault |
| **Cost ballpark (Kubernetes tier)** | Starter ~$292/mo us-east-1 / ~$335/mo ap-southeast-1; Team ~$977/mo; Scale ~$1,645/mo (see §4) | Comparable to AWS Team tier for a GKE Standard 3-node setup; Autopilot billing is per-pod so it depends heavily on actual load | Comparable to AWS; AKS control plane is free; node + storage costs track AWS |
| **Known gaps vs. chart** | `extraVolumes`/`extraVolumeMounts` missing (workaround: kustomize patch); `ingress.tls` guard does not account for ALB-terminated TLS (pass a dummy entry) | No IaC — operator must write from scratch | No IaC — operator must write from scratch |
| **Cognito/IdP caveat** | Cognito access tokens carry `client_id` not `aud`; Cooker's verifier enforces `aud == ClientID` — half-day spike required before committing (OPEN-7 in `docs/guides/DEPLOY-AWS-VERCEL.md`) | Standard OIDC — no known incompatibility; Google OAuth works (no `groups` claim → viewer-only unless a Lambda/custom claim is added) | Azure AD / Entra ID — standard OIDC; groups claim available via optional token configuration |

### Primary launch target recommendation

**Recommended: AWS (EKS Auto Mode) if managed Kubernetes is required; k3s on a VPS if cost is the priority.**

Rationale for AWS when choosing managed Kubernetes:
- The only cloud with complete, tested IaC at `deploy/aws/terraform/`. GCP and
  Azure have no corresponding `deploy/gcp/` or `deploy/azure/` directory — an
  operator starting there writes all infrastructure from scratch.
- Pod Identity wired in Terraform eliminates static credentials from the Helm
  chart (`serviceAccount.annotations: {}`).
- EFS as the Kaniko context PVC covers the ReadWriteMany requirement with
  lifecycle policies.
- Three-tier overlay system (Starter/Team/Scale tfvars) maps directly to the
  chart's scaling path: `values flip` for replica/Redis changes, `terraform
  apply` for data tier changes.

Rationale for k3s VPS when cost is the priority (the `product-plan.md` §6.3
recommendation):
- Estimated $15–45/mo vs $292+/mo for EKS Starter.
- k3s provides everything the Helm chart needs: ingress (Traefik or nginx),
  NetworkPolicy (Flannel with network-policy addon or Calico), PVC for Kaniko
  context, cert-manager for Let's Encrypt TLS.
- Lower operational complexity for a solo operator.
- Scaling path exists: when a single VPS is insufficient, migrate to managed K8s
  with the existing Helm chart (values are compatible).

---

## 3. Tiered Hosting Shapes

### Shape A — Cheapest viable launch (single-replica k3s / VPS)

**Cost:** ~$15–45/mo. Cited in `docs/product-plan.md` §6.3 as the documented
"Ship it" shape.

**Architecture:**

```
VPS (4 vCPU / 8 GB, e.g. Hetzner CPX31 ~$15/mo)
  └── k3s (single node)
       ├── Cooker pod (1 replica, UID 65532)
       ├── Redis pod (in-cluster; see deploy/kubernetes/redis.yaml)
       ├── Postgres (co-hosted or managed: Neon/Supabase ~$0-15/mo)
       └── cert-manager (Let's Encrypt TLS)
```

**Helm values alignment:**

```yaml
replicaCount: 1
cookerEnv: production
builder:
  kind: kaniko
  kaniko:
    contextPVC: cooker-build-context   # operator-provisioned PVC
wsHub:
  backend: memory                      # legal at replicaCount: 1
wsTicket:
  backend: memory
rateLimit:
  backend: memory
redis:
  enabled: true                        # still needed for wsHub if you want to scale later
securityContext:
  enabled: true                        # UID 65532, drop ALL caps, readOnlyRootFilesystem
networkPolicy:
  enabled: true
retention:
  enabled: true
  daysToKeep: 90
```

Note: with `replicaCount: 1` and all three backends set to `memory`, no Redis
is strictly required. The chart's default (`redis` backends) is correct for
forward-compatibility when you plan to scale. `Config.Validate` refuses to boot
with `memory` backends and `replicaCount > 1` unless `stickySessions: true`.

**Kaniko context PVC:** k3s includes local-path-provisioner (RWO only). For the
Kaniko PVC (ReadWriteMany — shared between Cooker pod and Kaniko Job), use
`nfs-subdir-external-provisioner` against the VPS NFS export, or Longhorn. EBS
(RWO) requires co-scheduling the Kaniko Job to the same node as Cooker, which
the chart does not currently stamp via `nodeSelector`/`tolerations` (noted open
in `docs/guides/DEPLOY-AWS-VERCEL.md` §8 "builder nodeSelector/tolerations").

**Verdict:** lowest cost, fully validated posture, sufficient for a solo
portfolio project or small team. This is the first recommended target.

---

### Shape B — HA multi-replica on managed Kubernetes

**Cost:** ~$50-100/mo (DOKS) or ~$292+/mo (EKS Starter, 3 tiers).

**Architecture:** 2+ Cooker replicas behind a managed load balancer, external
managed Postgres (Multi-AZ at Team+), ElastiCache/Memorystore/managed Redis,
EFS/Filestore for the Kaniko context PVC.

**Helm values alignment:**

```yaml
replicaCount: 2           # or driven by HPA
hpa:
  enabled: true
  minReplicas: 2
  maxReplicas: 6
pdb:
  enabled: true
  minAvailable: 1
wsHub:
  backend: redis           # required — memory per-process breaks WS broadcast across replicas
wsTicket:
  backend: redis           # required — tickets minted on replica A rejected by replica B
rateLimit:
  backend: redis           # required — per-user limits must be shared
redis:
  enabled: false           # point at external managed Redis
  url: "rediss://..."      # ElastiCache Serverless uses rediss:// (TLS)
database:
  host: "<rds-endpoint>"
  passwordSecretRef:
    name: cooker-db
    key: password
postgresql:
  sslMode: require
```

The chart's default `values.yaml` already ships this shape (`wsHub.backend:
redis`, `wsTicket.backend: redis`, `rateLimit.backend: redis`). The Starter
tfvars override to in-memory; Team+ use ElastiCache.

**Note on ALB / EKS:** the chart's `networkPolicy.enabled` uses pod-label
selectors. ALB target-group ENIs are not pods, so `networkPolicy.enabled: false`
is required on EKS with ALB ingress, replaced by a CIDR-based NetworkPolicy
alternative (documented in `docs/guides/DEPLOY-AWS-VERCEL.md` §3.4).

---

### Shape C — Hosted-SaaS-grade (multi-tenant build isolation)

This shape is not yet supported by the current codebase. Per `docs/product-plan.md`
§5 Tier 3 and the ADR series:

- Multi-tenancy / per-team RBAC requires an architectural ADR (deferred, no
  implementation exists).
- Build isolation across tenants requires namespace-per-tenant Kaniko Jobs with
  NetworkPolicy enforcement, which in turn requires the `builder nodeSelector/
  tolerations` follow-up and an RBAC model scoped to per-tenant namespaces.

At the infra layer, Shape C maps to the AWS Scale tier ($1,645/mo) with HPA
3..6 replicas, `r7g.large` Multi-AZ Postgres, the full Spot build pool, and WAF.
But the platform-layer work (multi-tenancy) is the true gate, not the
infrastructure cost.

**Do not plan for Shape C until `docs/adr/0004-multi-tenancy.md` is actioned
and the codebase supports it.**

---

## 4. "Test on Vercel First, then Cloud" as a Real Plan

The stated sequence maps to this concrete operational plan:

### Phase 1 — Frontend preview on Vercel + backend on a small cloud env

**Duration:** day 1–2.

1. **Deploy the Vercel project:**
   - Connect the GitHub repo to Vercel.
   - Set Root Directory = `frontend`, Framework = Vite, Node = 20.x.
   - Copy `deploy/vercel/vercel.json` → `frontend/vercel.json` (or configure
     Root Directory to point Vercel at `deploy/vercel/`).
   - Set `VITE_API_BASE_URL` = `https://<backend-hostname>` in Vercel
     environment variables (Production + Preview).
   - Set `VITE_OIDC_ENABLED=false` in the Preview environment.
   - Disable Deployment Protection (Cooker has its own auth).

2. **Stand up the backend on a lightweight host:**
   - Option A (cheapest, ~$0.15/day): Hetzner CX22 running the UAT compose stack
     (`make uat-up`) with Caddy for TLS. No Vercel Pro required; no per-PR
     previews.
   - Option B (~$1.45/day): AWS Lightsail 4 GB ($0.79/day) + Vercel Pro
     ($0.66/day). Gives per-PR previews. Backend setup documented step-by-step
     in `deploy/vercel/README.md` §"Lightsail + Caddy backend setup (10-step
     sketch)".

3. **Configure the backend for split-origin:**
   - Set `COOKER_ENV=uat`.
   - Set `COOKER_ALLOWED_ORIGINS=*` (legal only in `uat`; lets any per-PR Vercel
     preview hostname call the backend).
   - For the fixed/stable domain: set `COOKER_OIDC_ENABLED=true` and register
     the exact redirect URI at the IdP.

4. **Smoke test the WS stream**: load the Vercel URL, sign in with local auth,
   trigger a pipeline run, confirm live logs flow through the WebSocket. If logs
   stall, Caddy is not forwarding the WS upgrade or `COOKER_ALLOWED_ORIGINS`
   does not admit the Vercel origin.

**What is validated at this phase:** the split-origin wiring, the CORS/origin
configuration, the WebSocket path, and the OIDC flow on the fixed domain. This
is purely UAT posture — `Config.Validate` is lenient under `COOKER_ENV=uat`.

---

### Phase 2 — Promote backend to the chosen cloud (production posture)

**Duration:** 1–3 days depending on cloud familiarity.

**For AWS (EKS Auto Mode) — complete IaC exists:**

1. Copy `deploy/aws/terraform/backend.tf.example` → `backend.tf`; fill in the
   S3 state bucket (TF ≥ 1.10 native locking, no DynamoDB needed).
2. Fill in `deploy/aws/terraform/envs/prod-starter.tfvars` (domain,
   Route 53 zone id).
3. `terraform init && terraform apply -var-file=envs/prod-starter.tfvars`.
   This provisions VPC, EKS Auto Mode, RDS, EFS, ECR, Secrets Manager secrets,
   Pod Identity associations, CloudWatch, and a budget alarm.
4. Resolve the Cognito `aud` spike (OPEN-7) before committing to Cognito as the
   IdP. Alternatives: Google OAuth (viewer-only without groups) or in-cluster
   Keycloak (ops burden). See `docs/guides/DEPLOY-AWS-VERCEL.md` §5 Step 2.
5. `helm upgrade --install cooker deploy/helm/cooker -f deploy/aws/values/values-aws-starter.yaml`
   with per-cluster `--set` overrides (database.host, ACM cert ARN, OIDC vars,
   secretKey.existingSecret).
6. Run the pre-PROD checklist from `docs/audits/launch-readiness.md` §Pre-PROD.
7. Walk the cutover procedure in `docs/guides/ROLLOUT.md`.

**For GCP / Azure — no IaC exists (see §5 Gaps):**

An operator choosing GCP or Azure must write all infrastructure from scratch.
The Helm chart is cloud-agnostic and will work on GKE or AKS, but there is no
equivalent of `deploy/aws/terraform/` for those clouds. Effort estimate: 1–2
days for a GCP or Azure engineer familiar with their cloud's Terraform providers
(`google` or `azurerm`), covering VPC, GKE/AKS, Cloud SQL/Azure DB, Filestore/
Azure Files, Artifact Registry/ACR, Workload Identity, and TLS ingress.

**For VPS / k3s — no cloud IaC needed:**

Follow the `docs/product-plan.md` §6.3 shape: provision a 4 vCPU / 8 GB VPS,
install k3s, install cert-manager, provision the Kaniko context PVC (NFS or
Longhorn for RWX), create the Kubernetes Secrets for `COOKER_SECRET_KEY` and
`DATABASE_URL`, then `helm upgrade --install cooker deploy/helm/cooker` with
the production values.

---

### CORS / origins config across phases

| Phase | `COOKER_ALLOWED_ORIGINS` | `COOKER_ENV` | Notes |
|---|---|---|---|
| Phase 1 (UAT, preview) | `*` | `uat` | Wildcard permits per-PR Vercel hostnames. Rejected at boot in `production`. |
| Phase 1 (UAT, stable domain) | exact Vercel stable domain | `uat` | Single origin for the OIDC-enabled stable URL. |
| Phase 2 (production) | exact production domain(s), comma-separated | `production` | Wildcard rejected; empty rejected. Config.Validate enforces at boot. |

---

## 5. Gaps

These are missing artifacts or known issues that an operator will hit. Effort
estimates are for a single engineer.

| Gap | Severity | Effort | Notes |
|---|---|---|---|
| **GCP Terraform IaC** | High — no `deploy/gcp/terraform/` exists | 1–2 days | GKE cluster + VPC + Cloud SQL + Filestore + Artifact Registry + Workload Identity + Cloud Armor ingress. Mirrors the AWS module structure. |
| **Azure Terraform IaC** | High — no `deploy/azure/terraform/` exists | 1–2 days | AKS + VNet + Azure Database for PostgreSQL + Azure Files + ACR + Workload Identity + App Gateway ingress. |
| **Chart `extraVolumes` / `extraVolumeMounts`** | Medium — required for AWS dockerconfig refresh CronJob | 2–4 hours | The chart exposes `extraEnv` / `extraEnvFrom` but not volume mounts. Without this, the `dockerconfigjson` Secret for ECR refresh cannot be mounted at `/etc/cooker-docker` via Helm; a kustomize patch is the current workaround. Filed in `docs/guides/DEPLOY-AWS-VERCEL.md` §8. |
| **Chart `ingress.tls` guard vs ALB-terminated TLS** | Low — workaround exists | 1–2 hours | The `production + oidc + ingress` guard in `templates/ingress.yaml` requires `ingress.tls` even when TLS is terminated at the ALB via ACM. Pass a dummy `ingress.tls` entry as the workaround. |
| **Vercel project config placement** | Low | 30 minutes | `deploy/vercel/vercel.json` must be copied or symlinked to `frontend/vercel.json` for Vercel to read it (Vercel reads `vercel.json` relative to the Root Directory, which is `frontend`). No CI step syncs this. |
| **Split-deploy runbook** | Medium — currently fragmented across multiple files | 4–8 hours | A single `docs/guides/DEPLOY-SPLIT-ORIGIN.md` covering the Vercel + backend topology end-to-end (env matrix, CORS config, OIDC redirect, WS proxy requirements, smoke tests) would reduce operator error. Content exists in `deploy/vercel/README.md` and `docs/guides/DEPLOY-AWS-VERCEL.md` §2 but is split. |
| **Kaniko build context PVC guide for k3s** | Medium — RWX storage is non-obvious on k3s | 2–4 hours | The chart requires a RWX PVC for `builder.kaniko.contextPVC`. k3s local-path-provisioner is RWO only. A guide covering `nfs-subdir-external-provisioner` or Longhorn as the RWX option on k3s is absent. |
| **Builder `nodeSelector` / `tolerations`** | Medium — required for clean Spot targeting on EKS | 4–8 hours | Kaniko/Buildah Jobs do not inherit `nodeSelector` or `tolerations` from the chart, so Spot NodePool targeting and the EBS-RWO same-node context alternative are blocked. Filed in `docs/guides/DEPLOY-AWS-VERCEL.md` §8. |
| **Cognito `aud` claim spike (OPEN-7)** | High — gates the AWS IdP choice | 4 hours | Cooker's OIDC verifier enforces `aud == ClientID`; Cognito access tokens carry `client_id` not `aud`. A small configurable-audience code change or a pre-token-generation Lambda is required. Not an infra gap — a backend code change. |
| **EFS GC CronJob** | Medium — EFS grows without bound otherwise | 1–2 hours | The Terraform and guide document the need for a nightly `find -mtime +3 -delete` CronJob against EFS, but no CronJob manifest ships in the repo. Operators must write it. |
| **GCP and Azure cost tables** | Low — advisory only | 4–8 hours | `docs/guides/DEPLOY-AWS-VERCEL.md` has detailed AWS cost tables. GCP and Azure equivalents do not exist. |

---

## Summary

| Question | Answer |
|---|---|
| Can Vercel host the Cooker backend? | No. WebSockets, the in-cluster Kaniko Job model, Postgres connections, and long-running Go process state are all incompatible with Vercel's serverless model. |
| What can Vercel host? | The React frontend (SPA static assets + per-PR preview deployments). |
| What is the correct split? | Frontend on Vercel (or any static CDN). Backend on Kubernetes (k3s VPS or managed cloud K8s) with a reverse proxy (Caddy / Nginx / ALB) that forwards WebSocket upgrades. |
| Recommended primary launch target | k3s on a VPS (~$15–45/mo) for portfolio/solo use; AWS EKS (Starter tier ~$292/mo) when managed Kubernetes is required — it is the only cloud with complete IaC. |
| What is "test on Vercel first, then cloud" as a real plan? | Phase 1: Vercel SPA + Lightsail/Hetzner UAT backend (compose stack + Caddy). Phase 2: Helm install on the target cloud K8s cluster. The split-origin wiring is already in the codebase (`VITE_API_BASE_URL`, `origin.ts`, `client.ts`). |
| Biggest gaps | GCP and Azure have no Terraform IaC. Chart is missing `extraVolumes`/`extraVolumeMounts`. Cognito `aud` claim requires a code-level fix before EKS + Cognito is usable. |
