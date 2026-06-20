# Security Policy

## Supported Versions

| Version | Supported          |
|---------|--------------------|
| 0.1.x   | :white_check_mark: |

## Reporting a Vulnerability

If you discover a security vulnerability in Cooker, please report it responsibly.

**Do not open a public GitHub issue for security vulnerabilities.**

Instead, please email: **santapongsondhi@gmail.com**

This disclosure contact is also advertised machine-readably at `/.well-known/security.txt` (RFC 9116).

Include the following in your report:
- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

We will acknowledge receipt within 48 hours and aim to provide a fix or mitigation within 7 days for critical issues.

## Security Architecture

### Authentication

Cooker offers two authentication paths. Operators can enable either or both:

#### Path 1: OpenID Connect (recommended for teams and production)

- **Protocol**: OIDC Discovery + Authorization Code with PKCE (no client secrets in browser)
- **Token validation**: JWT access tokens validated server-side using the OIDC provider's JWKS endpoint
- **Session management**: Short-lived access tokens with refresh token rotation
- **Supported providers**: Keycloak, Okta, Azure AD, Google, GitHub
- **Generic verify-failure response** (S26-05-01): on a verify error the API returns `{"error":"authentication failed"}` with `401`, and `{"error":"provider unavailable"}` with `503 + Retry-After` when the IdP itself is unreachable. The upstream library's diagnostic detail (`iss mismatch`, `kid not found`, signature parse errors, etc.) is logged at `slog.Warn` / `slog.Error` server-side only — clients do not get an oracle for crafting valid-shaped tokens or fingerprinting JWKS rotation cadence.

> **Default in local & UAT:** OIDC is **disabled** (`COOKER_OIDC_ENABLED=false`) and the backend injects a dev admin user so contributors and testers can exercise the API without an IdP. Production deployments should enable OIDC — see the checklist below and [docs/UAT.md](docs/guides/UAT.md#enabling-oidc-sign-in-for-uat) for how to wire Google or another provider.

#### Path 2: Local email + password (homelab / single-user / fallback)

When `COOKER_LOCAL_AUTH_ENABLED=true`, Cooker exposes:

- `POST /api/v1/auth/local/signup` — bcrypt-hashes the password (`DefaultCost`) and creates a row in the `users` table. The very first signup is granted `admin` (bootstrap pattern); subsequent signups default to `viewer` and must be promoted by an admin.
- `POST /api/v1/auth/local/signin` — verifies the bcrypt hash and issues an HS256-signed JWT (`iss=cooker-local`, `exp=COOKER_LOCAL_AUTH_TOKEN_TTL`, default 12h).
- `GET /api/v1/auth/local/me` — returns the authenticated user's profile (works for both local and OIDC sessions).
- `GET /api/v1/auth/methods` — public capability probe so the frontend knows which auth methods to render.

The same auth middleware accepts both kinds of bearer token: it inspects the JWT's `iss` claim and dispatches to the local issuer or the OIDC IDTokenVerifier as appropriate.

**Trade-offs and limits of the local path:**

- **MFA gating does not apply.** `COOKER_OIDC_MFA_ACR_VALUES` is checked against the OIDC token's `acr`/`amr` claims; local-auth tokens carry neither. If your environment requires MFA on destructive admin routes, those users must come in through the OIDC path.
- **Brute-force defence is rate-limit-only.** There's no account lockout, no CAPTCHA, no email verification, no password reset. The per-user rate limiter on `/api/v1` applies once a user is authenticated, but unauthenticated `/auth/local/signin` calls are not rate-limited at the application layer — operators should enforce that at the edge (NGINX `limit_req`, Traefik rate-limit middleware, etc.).
- **Signup can be closed.** Set `COOKER_LOCAL_AUTH_ALLOW_SIGNUP=false` to disable `/signup`; the UI hides the form and direct calls return 403. Use this when you want admin-created accounts only.
- **Signing key requirement.** `COOKER_LOCAL_AUTH_JWT_SIGNING_KEY` must decode to ≥ 32 bytes (base64-decoded; raw bytes also accepted). `Config.Validate()` refuses to start in production with a shorter key.
- **No revocation list.** A leaked JWT remains valid until its `exp` claim. Lower `COOKER_LOCAL_AUTH_TOKEN_TTL` (default `12h`) if that's a concern.
- **Token storage.** The frontend stores the JWT in `localStorage` under `cooker.local.token` — same XSS exposure as the OIDC path's `oidc-client-ts` storage. Both paths benefit from a strict CSP at the ingress.

This path is intentionally minimal — it's the homelab / single-user / OIDC-isn't-available-yet escape hatch, not a full IAM. For team or production use, OIDC is the recommended path.

#### Path 3: API tokens (personal access tokens / service accounts)

Scripts and external CI authenticate with a long-lived bearer token instead of a browser OIDC flow. This path is always available — the `api_tokens` table backs it regardless of which interactive auth method is configured.

- **Format.** A token is `ck_` + base64url(32 random bytes from `crypto/rand`) — 256 bits of entropy. The `ck_` prefix lets the bearer middleware route token auth vs JWT auth cheaply, before paying for any verify.
- **Hash-only storage.** Only the SHA-256 hash of the plaintext is persisted (hex), behind a unique index. The plaintext is shown **exactly once**, at creation (`POST /api/v1/tokens` → `{"token":"ck_…"}`), and is unrecoverable afterward. A short `displayPrefix` (first 12 chars, e.g. `ck_AbCd1234`) is kept so a token is identifiable in listings without revealing the secret; it is not sufficient to authenticate. The hash is never serialised to any client (`json:"-"`).
- **Verification.** On a `ck_`-prefixed bearer credential the middleware SHA-256-hashes it, looks it up by the hash index, and `crypto/subtle.ConstantTimeCompare`s the stored hash. An absent, revoked, or expired token is rejected with a generic `401 {"error":"authentication failed"}` — identical to a bad JWT, with no oracle (S26-05-01 posture). The token branch engages **only** on the `ck_` prefix and never alters the OIDC or local-JWT paths.
- **Identity.** A token-authenticated request carries `sub = token:<id>`, `name = <token name>`, and the token's single stored role. The role is snapshotted at creation.
- **Role cap.** Any authenticated user may mint a token, but only with a role **≤ their own** — a viewer cannot mint an admin token. An admin may mint any role; the orthogonal `approver` role may be minted only by an admin or an approver (an operator of the same numeric rank cannot escalate into promotion-approval rights via a token).
- **Ownership.** A user may list and delete the tokens they created; admins may list (`GET /api/v1/tokens?all=true`) and delete any token. A non-admin deleting another user's token gets `404` (not `403`) so token ids cannot be probed.
- **No self-replication.** A request **authenticated by a token** may **not** create or delete tokens. A leaked token therefore cannot mint fresh tokens or revoke others — it is bounded to its own role's API surface until revoked or expired.
- **Revocation = immediate delete.** `DELETE /api/v1/tokens/:id` removes the row; the next authenticated use fails. There is no soft-revoke / grace window.
- **Expiry.** `expiresAt` is optional. A token with no expiry never expires; expired tokens are rejected exactly like revoked ones. **Operators are strongly advised to set short expiries for CI tokens.**
- **MFA bypass — stated honestly.** API tokens carry no `acr`/`amr` claims, so they **never satisfy the step-up MFA gate** (`COOKER_OIDC_MFA_ACR_VALUES`) — the same limitation as local-auth JWTs. A token cannot drive an MFA-gated destructive route, and an admin deleting *another* user's token via the API is itself MFA-gated when MFA is configured (own-token deletion is not). **A leaked token is bearer access at its role until it is revoked or expires** — there is no per-request second factor. Treat tokens as secrets: scope them to the least role that works, set a short expiry for CI, store them in your CI's secret store (never in source), and revoke on suspected exposure.
- **`last_used_at`.** Each authenticated use stamps `last_used_at`, throttled to at most one write per minute per token to avoid hot-path write amplification. Use it to spot dormant tokens to prune.

### Authorization (RBAC)

Role-based access control with four tiers (`backend/internal/auth/rbac.go:12-17`):

| Role | Permissions |
|------|-------------|
| **admin** | Full access: manage pipelines, deploy to all environments, configure settings, manage users; can reveal secret values |
| **operator** | Run pipelines, manage environments, view all resources. **Cannot** approve environment promotions — that's gated to admin and approver only (`CanApprovePromotion`, `rbac.go:92-102`). |
| **approver** | Narrow role dedicated to environment promotion approval. No other write rights beyond the approval action itself. |
| **viewer** | Read-only access to all resources, view pipeline runs and logs |

Roles are mapped from OIDC group claims. The mapping is configurable via `COOKER_OIDC_GROUP_MAP` (CSV of `group:role` pairs) or the Helm chart value `oidc.groupRoleMap`. Empty falls back to the built-in `cooker-admins → admin`, `cooker-operators → operator`, `cooker-approvers → approver`, `cooker-viewers → viewer` (`DefaultGroupRoleMap`, `rbac.go:120-125`).

#### Step-up MFA on destructive admin routes

Operators can require a second factor on the most dangerous endpoints (DELETE pipelines/envs/apps/hosts, secret reveal/put/delete/promote, app webhook rotation) by setting `COOKER_OIDC_MFA_ACR_VALUES` (CSV — chart: `oidc.mfaAcrValues`). The middleware accepts a request only when the token's `acr` claim, or any value in the `amr` array, is in the configured set. Otherwise the route returns:

```
HTTP/1.1 403 Forbidden
{ "error": "mfa_required", "acr_values": ["mfa", ...] }
```

The frontend API client recognises this response and re-issues the OIDC sign-in redirect with `acr_values=<configured>` so the IdP runs the second factor and the user retries the action with a fresh, MFA-bearing token. Empty config disables the gate (current default).

### Supply chain and release signing (v0.1.0+)

Cooker releases are signed using **cosign keyless signing** via the Sigstore / Rekor transparency log. There are no long-lived signing keys stored in the repository or in CI secrets.

#### What is signed

| Artifact | Verification method |
|---|---|
| `checksums.txt` (SHA-256 of all binary archives) | `cosign verify-blob checksums.txt --signature checksums.txt.sig --certificate checksums.txt.pem` |
| `ghcr.io/santapong/cooker:<tag>` manifest digest | `cosign verify ghcr.io/santapong/cooker:<tag> --certificate-identity ...` |
| `oci://ghcr.io/santapong/charts/cooker` Helm chart | Not signed in v0.1.0; tracked as a follow-up (Helm OCI + Referrers API). |

#### How it works

1. The release workflow (`.github/workflows/release.yml`) requests a short-lived OIDC token from the GitHub Actions token endpoint (`id-token: write` permission).
2. `cosign` exchanges that token with the Sigstore Fulcio CA for a short-lived X.509 certificate whose Subject is bound to the workflow's run ID and the tag ref.
3. The certificate + signature are stored in the Sigstore Rekor transparency log, not in the repository.
4. Verifiers run `cosign verify-blob` or `cosign verify` with `--certificate-identity` set to the workflow path and `--certificate-oidc-issuer` set to `https://token.actions.githubusercontent.com`. Any signature that does NOT originate from the official workflow and tag will fail verification.

#### Pinned action SHAs

`.github/workflows/release.yml` is the release workflow that feeds the cosign trust chain, and every third-party `uses:` in it is pinned to a 40-character SHA per the supply-chain policy. The SHA comments in the workflow file record the action version at the time of pinning; reviewers must verify these SHAs against upstream release tags before merging updates.

**Non-release workflows — pinned (S26-05-15 closed).** `.github/workflows/ci.yml`, `.github/workflows/cooker-weekly.yml`, and `.github/workflows/oci-conformance.yml` are now pinned to 40-character SHAs as well, with `# <tag>` comments recording the version at pin time (annotated tags pinned to their peeled commits — including `anthropics/claude-code-action`, the highest-write-permission action in the set). Resolution history is in [`docs/audits/2026-05-action-pinning.md`](docs/audits/2026-05-action-pinning.md). When enabling Renovate/Dependabot (backlog P1.5), turn on digest-pinning updates so these SHAs stay current.

#### Verifying a release

See [`docs/RELEASING.md`](docs/guides/RELEASING.md#step-4--verify-the-release-artifacts) for the exact `cosign verify-blob` and `cosign verify` commands. For the security-side post-publish checklist (Rekor lookup, identity drift checks, expected workflow subjects), see [`docs/SECURITY-RELEASE-VERIFY.md`](docs/guides/SECURITY-RELEASE-VERIFY.md).

### OCI Registry Security

- **Authentication**: Registry credentials stored server-side only, never exposed to the frontend
- **Transport**: All registry communication over TLS (HTTPS)
- **Content trust**: Images tracked by content-addressable digests (`sha256:...`) per OCI image-spec
- **Conformance**: The image-push path is exercised against the upstream [OCI distribution-spec conformance suite](https://github.com/opencontainers/distribution-spec/tree/main/conformance) via `.github/workflows/oci-conformance.yml` (weekly + on pusher changes).
- **Supply chain**: Referrers API support for attaching signatures, SBOMs, and build provenance; keyless cosign signatures shipped with every release starting v0.1.0 (see "Supply chain and release signing" above).

### Kubernetes Access

- **In-cluster**: Uses Kubernetes service account tokens with least-privilege RBAC
- **External**: Kubeconfig-based access with configurable contexts
- **RBAC** (S26-05-19): Cooker's ServiceAccount holds a Role *or* ClusterRole, chart-selectable via `rbac.clusterWide`. The chart default is **cluster-wide** for compatibility with the v0.1 raw manifests (`deploy/kubernetes/rbac.yaml`); namespace-scoped is encouraged once your deploy targets are confined. The chart-rendered ClusterRole is scoped to the resources Cooker needs (deployments, pods, services, configmaps, secrets, namespaces) — but "scoped to resources" is not the same as "scoped to a namespace", which is the property operators reading this section usually care about.
- **Read API (list/inspect endpoints)**: `GET /api/v1/kubernetes/{namespaces,workloads,workloads/:ns/:kind/:name,pods/:ns/:name/logs}` perform **live, read-only** client-go reads against the server's configured cluster (the cluster target comes only from in-cluster config / `COOKER_KUBECONFIG` — never from the request, so there is no SSRF). These are gated at the **operator role** (`writeRole`), the same as the Kubernetes write path: a plain `viewer` cannot enumerate the cluster or read pod logs. This matters because pod logs can contain tokens/PII and — with the default `rbac.clusterWide: true` — span **every namespace** the ServiceAccount can see, not just Cooker's deploy targets. To narrow the blast radius, set `rbac.clusterWide: false` and confine the ServiceAccount to your deploy-target namespaces. Pod-log reads are bounded (`tailLines` clamped to ≤10000, response capped at 256 KiB). Write operations (scale/restart/apply/delete) remain unimplemented stubs.
- **The same operator-role gate now applies over WebSocket** (CR-6, fixed). The live cluster watch `GET /ws/kubernetes/watch` and the runtime-log stream `GET /ws/runtime/:appId/:serviceId/logs` are operator-gated to mirror their HTTP equivalents. The role is enforced **twice**: a `viewer` is refused a ticket for these paths at mint time (`POST /api/v1/ws-tickets` returns 403), and the WS upgrade gate independently rejects a ticket whose bound roles don't satisfy the route. Before this fix, the `/ws` group carried no role guard and the ticket's subject was discarded, so any authenticated user — including a `viewer` — could watch arbitrary namespaces and stream runtime logs. See "WebSocket" below for the ticket scoping model.

### Image build isolation

Cooker ships four image-build strategies; pick via `COOKER_BUILDER` (chart: `builder.kind`):

- **`kaniko`** *(production-recommended)*: each build runs as a one-shot `batch/v1.Job` inside the cluster. No host docker.sock, no node-root path. The Cooker pod and the Kaniko Job share a `PersistentVolumeClaim` (`builder.kaniko.contextPVC`) for the source tree. RBAC scoped to the build namespace ships in `templates/rbac.yaml` — Job + Pod create/get/watch/delete only, never cluster-wide.
- **`buildah`**: same shape as Kaniko (one `batch/v1.Job` per build, namespace-scoped RBAC, no host docker.sock) but with full Dockerfile feature parity — `RUN --mount=type=cache`, heredocs, etc. that Kaniko silently ignores. **PSA caveat**: rootless Buildah needs `CAP_SETUID` + `CAP_SETGID` for its user-namespace setup, so the build namespace must be PSA `baseline` or a custom profile permitting both — `restricted` drops them and the rootless setup fails. Storage driver is configurable via `builder.buildah.storageDriver` (`vfs` works without kernel modules; `overlay` is faster but needs fuse-overlayfs on the nodes).
- **`docker`**: shells out to the local Docker daemon via the bind-mounted host socket. Convenient on single-node test clusters; gives the Cooker container root-equivalent access to the host's Docker. An RCE in Cooker → full host control. Only use this on isolated dev hosts.
- **`buildkit`**: stub; not yet wired (backlog P9.1).

The Helm chart conditionally drops the `docker.sock` volume + mount when `builder.kind != "docker"`, so any of `kaniko` / `buildah` / `buildkit` carries no leftover host paths.

**Pusher gate (F-02):** The `COOKER_PUSHER=docker` adapter shells out to the Docker CLI for `docker push`, which uses the same bind-mounted host socket as the `docker` builder. `Config.Validate()` now refuses to boot in production when `COOKER_PUSHER=docker` is set, closing the gap where an operator who correctly switches `COOKER_BUILDER=kaniko` but leaves `COOKER_PUSHER=docker` would still expose the docker.sock surface via the push path. Use `COOKER_PUSHER=crane` in production. The **raw-Kubernetes parity manifest** at `deploy/kubernetes/deployment.yaml` does **not** mount the host docker socket either (S26-05-04, closed on `claude/sec-quickwins-2026-05`); operators who legitimately need the socket must author a deliberate variant and accept the RCE-to-host gap explicitly.

**Docker-host deploy runtime (compose deployment DAGs):** An App whose deploy target is `docker-host` runs its per-service deploy stages through the `docker-run` / `compose` deployers (`backend/internal/deployer/dockerrun.go`, `compose.go`), which shell out to the local `docker` CLI (`docker run` / `docker compose up`). These talk to the Docker daemon (`DOCKER_HOST`, default the host socket) and therefore carry the **same root-equivalent RCE-to-host exposure as the `docker` builder/pusher** — an RCE in Cooker becomes host control. They are intended for **single-node / dev hosts only**; for cluster deployments use a Kubernetes deploy target (the per-service manifests apply via client-go, no socket). The deployers fail closed with `ErrUnavailable` when the `docker` CLI is absent. Resource limits parsed from the compose file are applied as `docker run --memory/--cpus` (and as K8s `resources.limits` on the Kubernetes path).

### Test/Custom stage execution model

Pipeline **Test** and **Custom** stages run **user-supplied code** — a Test stage's command, a Custom stage's shell script. This is, by design, arbitrary code execution: a pipeline author can run anything their chosen image can run. Cooker confines it the same way it confines image builds — **never on the Cooker server process**, always inside a throwaway container — and the operator picks the confinement backend via `COOKER_STAGE_RUNNER` (default `noop`):

- **`kube`** *(production-recommended)*: each Test/Custom stage runs as a one-shot `batch/v1.Job` in the configured namespace, reusing the same Job-submit/watch/log-stream plumbing and namespace-scoped RBAC as the Kaniko/Buildah builders. The stage container drops `ALL` capabilities and disallows privilege escalation; the pod otherwise runs under whatever PodSecurityContext the cluster enforces. No host docker.sock, no node-root path. The container exit code is the stage's pass/fail.
- **`docker`**: shells out to the local Docker daemon (`docker run --rm <image> <cmd>`). This gives the script the **same root-equivalent RCE-to-host exposure as the `docker` builder/pusher** — an escape from the stage container is host control. No host paths are bind-mounted by Cooker, but the daemon itself is the privileged surface. Intended for **single-node / dev hosts only**; for clusters use `kube`.
- **`noop`** *(default)*: does **not** start a container. It logs the intended command and reports success. Safe for dev/CI where no container runtime is available — but it does **not** run the user's code, so do not rely on it to actually execute tests or scripts in any environment where the result matters. Production must set `kube` (or `docker` on an isolated host).

A Test/Custom stage with no `image` configured fails loudly at execute time rather than silently passing (the fail-loud contract from the HS26-05-03 fix). Stage stdout/stderr is streamed to the run page and persisted to the stage log (ANSI escapes stripped, capped at 1 MiB) — the same path build logs take, with the same caveat: **the logs contain whatever the user's script prints**, including any secret it chooses to echo. Stage environment variables (including resolved `secretRefs`) are injected into the container; treat the runner backend as part of your trust boundary and scope its namespace/host accordingly.

### Approval-gate stages (human-in-the-loop)

An **Approval** stage pauses the run until a distinct-approver threshold approves (or any approver rejects). The gate is a persisted row (`stage_approvals` / `stage_approval_votes`, migration 022) — it survives a server restart and the vote count is enforced at the DB level (one vote per identity via a `UNIQUE` constraint, so a double-click counts once). Approve/reject is gated to **admin or approver** roles (the same `CanApprovePromotion` RBAC as promotion approval), and the approver identity is taken from the OIDC claims, never the request body (IDOR-safe). A rejection settles the gate immediately. The blocked executor respects the run deadline / cancel / stage timeout via `ctx`, so an un-actioned gate fails the stage rather than hanging a worker.

### AI failure triage (opt-in data egress)

`COOKER_AI_TRIAGE_ENABLED=true` adds `POST /pipelines/:id/runs/:runId/stages/:stageId/triage` (operator role, rate-limited). On each **explicit operator click** — never automatically — Cooker sends the failed stage's sanitized config summary, its error string, and the last 32 KiB of its captured logs to the Anthropic Messages API and returns the model's advisory text.

Controls:

- **Off by default.** Disabled deployments return 503 and the frontend hides the button (`GET /api/v1/capabilities`).
- **Secrets stripped.** `Config.Env` values and `SecretRefs` values never enter the prompt (names only, for context). Pinned by `TestBuildRequest_StripsSecretsAndTailsLogs`.
- **Key custody.** `ANTHROPIC_API_KEY` lives server-side only; boot fails fast if triage is enabled without it (`Config.Validate`). The key is never echoed to clients or logs.
- **Advisory only.** The response is text for the operator; Cooker takes no automatic action on it.
- **Transport.** TLS ≥ 1.2 enforced on the outbound client; 90s timeout; one retry on 429/5xx.

Residual risk: stage logs can contain whatever the user's build prints. If your builds may log sensitive material, leave triage disabled or scrub at the build level — Cooker cannot distinguish a secret a build chose to print from ordinary output.

### Cloud inventory credentials (read-only, opt-in)

`COOKER_CLOUD_AWS_ENABLED` / `COOKER_CLOUD_GCP_ENABLED` add the read-only cloud inventory & cost panel (`GET /api/v1/cloud/{inventory,costs}`, `POST /api/v1/cloud/refresh`). When enabled, Cooker calls **only list/describe/cost APIs** — EC2 `DescribeInstances`, EKS `ListClusters`/`DescribeCluster`, ECR `DescribeRepositories`, Cost Explorer `GetCostAndUsage` on AWS; Compute `aggregatedList`, Container `clusters.list`, Artifact Registry `repositories.list` on GCP. There is **no mutating call path** in `internal/cloudinventory/` (the package and its providers expose `ListResources` + `CostSummary` only), so even a compromised Cooker cannot create, modify, or delete a cloud resource through this feature.

Controls:

- **Off by default.** With neither provider enabled the endpoints return `200 {"enabled":false}` and the SDK clients are never constructed (`GET /api/v1/capabilities` reports `cloudInventory:false`, hiding the page).
- **Least-privilege IAM is the operator's responsibility — and the primary control.** Grant a **read-only** identity:
  - **AWS**: the managed `ReadOnlyAccess` policy is sufficient but broad; prefer a scoped policy allowing only `ec2:DescribeInstances`, `eks:ListClusters`, `eks:DescribeCluster`, `ecr:DescribeRepositories`, and `ce:GetCostAndUsage`.
  - **GCP**: the predefined roles `roles/compute.viewer`, `roles/container.viewer`, and `roles/artifactregistry.reader` (Cost is not read via API — see below).
- **Prefer workload identity over static keys.** On EKS use **IRSA** (annotate the ServiceAccount with `eks.amazonaws.com/role-arn`); on GKE use **Workload Identity** (`iam.gke.io/gcp-service-account`). With either in place **no key env var or Secret is needed** — the Helm chart renders the enable flag + locator and nothing else. The static-credential fallback (`COOKER_CLOUD_AWS_SECRET_ACCESS_KEY`, `COOKER_CLOUD_GCP_CREDENTIALS_JSON`) is supported for non-managed runtimes and is wired via `secretKeyRef` into a **pre-created, operator-managed Secret** — never `values.yaml`, never a chart-created Secret. The keys are never logged (the providers log only the region/project on boot).
- **Read-role exposure.** The two `GET` endpoints are read-level (any authenticated user), exposing **resource metadata and aggregate cost only** — instance IDs, cluster names, repository URIs, and per-service spend. No credential material is ever placed on a response. `POST /cloud/refresh` is `writeRole` + rate-limited because it forces a synchronous fan-out to the cloud APIs (AWS Cost Explorer bills per request).
- **Caching bounds API spend.** Results are cached in memory for `COOKER_CLOUD_CACHE_TTL` (default 5m); a misconfigured non-positive TTL is ignored so the cost APIs can't be hammered.

GCP cost note: GCP exposes month-to-date spend only through the BigQuery billing export (or the Budgets API), **not** the Cloud Billing v1 API. To avoid fabricating a figure, the GCP provider returns a labelled **zero** cost summary; GCP resources are still real. Wiring the BigQuery export is tracked as follow-up.

Residual risk: the inventory reflects whatever the granted identity can see. Scope the IAM role to the accounts/projects you intend to surface; a broad `ReadOnlyAccess` role makes the panel enumerate everything in the account.

## Data Security

- **Database**: Pipeline definitions, run history, and environment configs stored in PostgreSQL
- **Secrets**: Database passwords, OIDC client secrets, and registry credentials should be managed via Kubernetes Secrets or an external secret manager. Cooker ships **five** secret-backend adapters, selected via `COOKER_SECRETS_BACKEND` (chart: `secrets.backend`); `Config.Validate()` (`backend/internal/config/config.go:411-433`) enforces the per-backend required env vars before boot:

    | `COOKER_SECRETS_BACKEND` | Adapter | Required env vars (in production) |
    |---|---|---|
    | `database` *(default)* | `internal/secrets/database` — envelope-encrypted rows in Postgres | `COOKER_SECRET_KEY` (≥ 32 bytes, base64) |
    | `keepsave` | `internal/secrets/keepsave` — KeepSave HTTPS API | `COOKER_SECRETS_KEEPSAVE_URL` (https://), `COOKER_SECRETS_KEEPSAVE_PROJECT_ID`, `COOKER_SECRETS_KEEPSAVE_API_KEY` |
    | `vault` | `internal/secrets/vault` — HashiCorp Vault KV v2 | `COOKER_SECRETS_VAULT_ADDR` |
    | `aws` | `internal/secrets/awsm` — AWS Secrets Manager | region auto-discovered from instance metadata if unset |
    | `gcp` | `internal/secrets/gcpsm` — GCP Secret Manager | `COOKER_SECRETS_GCP_PROJECT_ID` |

    The Helm chart **does not ship a default Postgres password** (S26-05-13); operators must pre-create a Secret and point `database.passwordSecretRef.name` at it. The chart `required`-guards this so a `helm install` without an override fails at render time rather than shipping a publicly-documented credential.
- **Postgres TLS** (S26-05-10): `Config.Validate()` rejects boot in production when `DATABASE_URL` points at a non-localhost host with `sslmode=disable` (or no `sslmode` parameter at all). The minimum acceptable value is `require`; `verify-ca` / `verify-full` are preferred when a CA bundle is mounted into the pod.
- **Environment variables**: Sensitive configuration injected at runtime, never baked into images
- **No secrets in pipelines**: Pipeline variable values are stored in the database; sensitive values should use secret references rather than plaintext

#### Credential handling — SSH remote hosts

When an operator registers a host with `kind=ssh-docker` (the
Dokploy/Coolify-style remote-deploy target), the PEM-encoded
private key body never lands on the `hosts` table. The flow is:

1. Handler binds `sshPrivateKeyPem` from the POST/PUT body. The
   field is **write-only**: it's not declared on `model.Host` and
   cannot be returned by any serialiser.
2. `service.HostService.maybeStoreKey` parses the PEM (rejecting
   malformed input with `ErrInvalidPrivateKey`), then writes it
   to `secrets.Manager.Put` under the synthetic envID `_hosts`,
   key `ssh_private_key.<host-id>`. The reference stored on the
   row is `host:<host-id>`; the bytes themselves live wherever
   the configured secrets backend keeps them.
3. `GET /api/v1/hosts/:id` (and `/hosts`) return a `HostResponse`
   that omits the ref and surfaces only `hasSSHPrivateKey: bool`.
   A regression test in `internal/handler/host_test.go` asserts
   no response body contains the substring `PRIVATE KEY` after a
   Create with a real PEM. Logs from the SSH adapter never echo
   the PEM either — the log writer is sanity-checked in
   `ssh_test.go`.
4. Host-key verification is **mandatory**. The SSH adapter's
   `HostKeyCallback` enforces a TOFU policy: pinned key required
   in strict mode, recorded on first connect in lax mode (lax
   mode is forbidden in production by `Config.ValidateSSHHosts`,
   which runs after store boot but before serving traffic). The
   `golang.org/x/crypto/ssh` "accept any host key" callback is
   forbidden in this codebase — a grep tripwire and a package-
   level test catch any reintroduction.
5. SSH connections are cached per host inside `Target` for the
   process lifetime and closed on shutdown via a `cleanup`
   registered with `server.New`. No global mutable state — the
   cache mutex is per-`Target` instance and the package is
   race-detector clean (`go test ./internal/deploytarget/ssh
   -race`).

### Network Security

- **CORS**: Configurable allowed origins via `COOKER_ALLOWED_ORIGINS`. Defaults to `localhost:5173,localhost:3000` for `COOKER_ENV=dev|uat`; defaults to **deny-all** for `COOKER_ENV=production` so missing config is loud, not silent. Boot **refuses to start** if `COOKER_ALLOWED_ORIGINS` is empty in production (`Config.Validate` — `backend/internal/config/config.go`); a wildcard `*` is also rejected (S26-05-19).
- **`Allow-Credentials`**: explicitly **off**. Cooker authenticates via `Authorization: Bearer <jwt>` headers, not cookies — credentials mode adds no value and would block wildcard reflection.
- **WebSocket**: three-layer auth — same-origin policy via `gorilla/websocket` `CheckOrigin` (sharing the CORS allowlist), a single-use ticket, **and** role + resource scoping bound into that ticket (CR-6). Clients `POST /api/v1/ws-tickets` over the authenticated API with a `{"path":"/ws/..."}` body naming the exact resource they will connect to. The server captures the caller's `auth.Claims` (subject + roles) and binds `{subject, roles, path}` into a 60-second, single-use ticket; the client then opens `/ws/...` with `?ticket=<value>`. The upgrade gate enforces **both** scopes: (1) **resource** — the ticket's bound path must equal the request path, so a ticket minted for `/ws/pipeline-run/A` cannot be replayed against `/ws/pipeline-run/B` or any other route (the resource id lives in the path, so binding the path binds the resource — the WS analogue of the HTTP `loadRunForPipeline` ownership check); and (2) **role** — routes that require operator+ over HTTP (`/ws/kubernetes/watch`, `/ws/runtime/:appId/:serviceId/logs`) require the same role over WS, refused at mint time *and* re-checked at connect time. Read-level log streams (`/ws/pipeline-run/:runId`, `/ws/app-run/:runId`, `/ws/docker/build/:buildId`, `/ws/runs/:runId/stages/:stageId/logs`) require only authentication, matching their HTTP `GET` peers. Tickets are consumed on first use; replay is rejected. The grant's subject + roles are placed in the request context (`ws-subject`, `ws-roles`) for handlers and the audit trail.
  - **Prior gap (now closed):** before CR-6, the `/ws` group had no `RequireRole` and the ticket subject set in context was never read by any handler, so any authenticated user (including a `viewer`) could stream any run/build/pod-log channel and watch arbitrary namespaces — bypassing the HTTP-layer IDOR and role checks. The ticket alone is no longer the only authz control; role and resource are bound at mint and enforced at connect.
- **App health probe**: `AppHealthChecker` runs in-process inside the cooker pod and reads from each deploy target via the existing in-cluster credentials (kubeconfig / SDK clients). It does NOT add an inbound network surface; egress is to the same target-backend endpoints the executor already talks to.
- **Ingress**: TLS termination recommended at the ingress controller level.
- **Internal traffic**: Backend-to-database and backend-to-Redis communication should use encrypted connections in production.

### Audit logging

Cooker emits a structured audit event for every authenticated mutating call (POST, PUT, PATCH, DELETE under `/api/v1`). Defaults: **enabled in production**, off elsewhere. Events are JSON, one per request, written to stdout (default), a file, a Postgres table, or any comma-separated combination.

- Tunable via `COOKER_AUDIT_ENABLED` (default `true` when `COOKER_ENV=production`, else `false`), `COOKER_AUDIT_DESTINATION` (comma list of `stdout`, `file`, `db` — e.g. `db,stdout`), and `COOKER_AUDIT_FILE_PATH`.
- Each event records: timestamp, OIDC subject, OIDC email, HTTP method, route template (e.g. `/api/v1/environments/:id/secrets/:key` — never the concrete `:id`), status code, latency, client IP.
- **Bodies are never captured.** The middleware does not read request or response bodies, so secret-bearing routes (`PUT /environments/:id/secrets/:key`, `PUT /apps/:id/webhook`) are safe by construction. Bearer tokens, OIDC raw JWTs, and secret values cannot appear in the trail.
- The redacted-route allowlist lives in `internal/audit/audit.go` (`IsRedacted`). Future changes that introduce body capture must consult it.
- Forward stdout to your SIEM via the cluster's logging stack (Loki, ELK, Datadog) or use the file sink with a sidecar tail-shipper.
- **`db` destination (queryable trail)**: events are also written asynchronously to the `audit_events` table and served back through `GET /api/v1/admin/audit` (admin + MFA) and the `/admin/audit` UI. The writer is drop-on-full (same contract as the file sink): a slow or down Postgres can lose audit events but can never block or slow request handling — pair `db` with `stdout`/`file` when you need a loss-resistant copy. Rows are swept daily after `COOKER_AUDIT_DB_RETENTION` (default `2160h` = 90 days; `0` disables). In production, `db` requires `DATABASE_URL` (`Config.Validate()` refuses to boot otherwise); with the in-memory store it degrades to a non-durable ~10k-event ring and logs a boot warning.

### Secrets-backend connectivity test

`POST /api/v1/settings/secrets/test` (admin + MFA) probes the configured secrets backend with a single authenticated `List` call. The response reveals **only** the backend kind (`database`, `keepsave`, …), reachability, latency, and a classified error (`authentication failed` / `backend unreachable` / raw message). Key names and values are never returned — the probe discards the list contents. The probe is rate-limited only by the admin+MFA gate; it makes one outbound request per click to the same endpoint the executor already talks to (no new egress class).

### Rate limiting

Cooker applies a **per-user, in-memory rate limit** to **three** specific endpoints (S26-05-19) so a single user cannot accidentally fork-bomb builds. Defaults: 10 requests/minute, burst 3, keyed on the OIDC subject claim. The covered routes are exactly:

- `POST /api/v1/pipelines/:id/run`
- `POST /api/v1/docker/images/build`
- `POST /api/v1/apps/:id/deploy`

**Every other route in `/api/v1` — including environment / app / pipeline CRUD, webhook secret rotation, `POST /api/v1/environments/:id/secrets/promote`, and the unauthenticated `/webhooks/github` receiver and `/api/v1/auth/local/{signup,signin}` paths — is unbounded at the application layer.** Operators must enforce edge rate-limiting (NGINX `limit_req`, Traefik middleware, Cloudflare, AWS WAF) for those.

- Tunable via `COOKER_RATE_LIMIT_ENABLED` (default `true`), `COOKER_RATE_LIMIT_PER_MINUTE` (default `10`), `COOKER_RATE_LIMIT_BURST` (default `3`).
- **Multi-replica deployments must back per-process state with Redis** — or pin clients with sticky sessions. `Config.Validate()` (`backend/internal/config/config.go:482-499`) refuses to boot in production when `COOKER_REPLICA_COUNT > 1 && COOKER_STICKY_SESSIONS=false` unless **all three** of the per-process subsystems are pointed at Redis:
    - `COOKER_RATE_LIMIT_BACKEND=redis` — otherwise a user can exhaust their quota on one replica and a fresh burst lands on the next.
    - `COOKER_WS_TICKET_BACKEND=redis` — otherwise a ticket minted on replica A is invisible to replica B and the WS upgrade is rejected.
    - `COOKER_WS_HUB_BACKEND=redis` — otherwise a broadcast emitted on replica A never reaches a client connected to replica B.

  Set all three to `redis` (and provide `REDIS_URL`), or set `COOKER_STICKY_SESSIONS=true` so the load balancer pins each client to one replica. Disabling the rate limiter entirely (`COOKER_RATE_LIMIT_ENABLED=false`) still leaves the WS-ticket and WS-hub backends to address.
- This middleware is **defense in depth**, not a substitute for edge limiting. Operators are still expected to configure IP-based rate limiting at the ingress controller (nginx-ingress, Traefik) or the cloud edge (Cloudflare, AWS WAF, GCP Cloud Armor) for unauthenticated paths and overall traffic shaping.

### CSRF

Cooker is **not vulnerable to classic CSRF** because the API is authenticated by the `Authorization` header, not by cookies:

- The browser does **not** auto-attach `Authorization` headers on cross-origin requests, so a malicious site cannot trigger an authenticated state-changing call.
- The OIDC sign-in redirect carries a `state` parameter validated by `oidc-client-ts` (RFC 6749 §10.12) — this guards the auth code exchange step.
- WebSocket upgrades are gated by the same origin allowlist as HTTP CORS.

**Why no CSRF tokens (synchronizer / double-submit-cookie):** they protect cookie-authenticated apps. Adding them to a Bearer-token API would be ceremony with no extra security and would not stop the attacks they're designed for. If session-cookie auth is ever added, CSRF tokens become required at the same time.

### Container Image Security

Cooker's own Docker image follows these practices:

- **Multi-stage build**: Build dependencies not included in the final image
- **Minimal base**: Alpine Linux base image with only `ca-certificates`
- **Non-root**: image runs as UID/GID 65532 (`cooker`), the standard "nonroot" UID aligned with distroless and K8s `runAsNonRoot: true`. UAT compose adds the host's docker group GID via `group_add` so the container can access the bind-mounted `docker.sock` without root.
- **OCI labels**: Image includes standard OCI annotations for traceability

### Security Headers

Production deployments should add these headers via the ingress controller or a reverse proxy:

```
Content-Security-Policy: default-src 'self'; connect-src 'self' wss:;
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
X-XSS-Protection: 1; mode=block
Strict-Transport-Security: max-age=31536000; includeSubDomains
Referrer-Policy: strict-origin-when-cross-origin
```

## Security Checklist for Production Deployment

- [ ] Enable OIDC authentication (`COOKER_OIDC_ENABLED=true`)
- [ ] Configure TLS on ingress
- [ ] Restrict CORS origins to your domain
- [ ] Use Kubernetes Secrets for database passwords and OIDC credentials
- [ ] Scope Cooker's ClusterRole to required namespaces if possible
- [ ] Use Kaniko instead of Docker socket for image builds *(set `builder.kind=kaniko` and `builder.kaniko.contextPVC=<your PVC>` in the chart, or `COOKER_BUILDER=kaniko` for non-Helm deploys)*; also set `COOKER_PUSHER=crane` — `Config.Validate()` refuses `COOKER_PUSHER=docker` in production (F-02)*
- [x] Enable PostgreSQL SSL connections *(enforced by `Config.Validate()` in production for non-localhost hosts — S26-05-10)*
- [x] Set up audit logging *(on by default when `COOKER_ENV=production`; emits one JSON event per mutating API call. Destination via `COOKER_AUDIT_DESTINATION`. See "Audit logging" above.)*
- [x] Run the container as non-root *(image runs as UID 65532 by default)*
- [x] Enable network policies to restrict pod-to-pod traffic *(NetworkPolicy ships with the Helm chart, gated by `networkPolicy.enabled`; raw manifest at `deploy/kubernetes/network-policy.yaml`)*
- [ ] Regularly update base images and dependencies
- [ ] If the cloud inventory panel is enabled (`COOKER_CLOUD_{AWS,GCP}_ENABLED`), bind a **read-only** identity — IRSA / Workload Identity over static keys — scoped to the minimum describe/list/cost permissions *(see "Cloud inventory credentials")*
- [x] Verify release artifact signatures *(cosign keyless signing ships with every release from v0.1.0; see "Supply chain and release signing" and `docs/RELEASING.md §Step 4`)*
