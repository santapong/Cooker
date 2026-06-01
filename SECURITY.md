# Security Policy

## Supported Versions

| Version | Supported          |
|---------|--------------------|
| 0.1.x   | :white_check_mark: |

## Reporting a Vulnerability

If you discover a security vulnerability in Cooker, please report it responsibly.

**Do not open a public GitHub issue for security vulnerabilities.**

Instead, please email: **security@cooker-ci.example.com**

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

**Known gap — non-release workflows.** `.github/workflows/ci.yml`, `.github/workflows/cooker-weekly.yml`, and `.github/workflows/oci-conformance.yml` collectively contain 17 `uses:` entries that still reference floating major-version tags. Closure is tracked separately under follow-up `S26-05-15` and enumerated in [`docs/audits/2026-05-action-pinning.md`](docs/audits/2026-05-action-pinning.md). The highest-write-permission unpinned action in that set is `anthropics/claude-code-action@v1` in `cooker-weekly.yml`, which runs with `contents: write` + `pull-requests: write`; readers reasoning about the supply-chain blast radius should treat that workflow's trust boundary accordingly until it is SHA-pinned.

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

### Image build isolation

Cooker ships four image-build strategies; pick via `COOKER_BUILDER` (chart: `builder.kind`):

- **`kaniko`** *(production-recommended)*: each build runs as a one-shot `batch/v1.Job` inside the cluster. No host docker.sock, no node-root path. The Cooker pod and the Kaniko Job share a `PersistentVolumeClaim` (`builder.kaniko.contextPVC`) for the source tree. RBAC scoped to the build namespace ships in `templates/rbac.yaml` — Job + Pod create/get/watch/delete only, never cluster-wide.
- **`buildah`**: same shape as Kaniko (one `batch/v1.Job` per build, namespace-scoped RBAC, no host docker.sock) but with full Dockerfile feature parity — `RUN --mount=type=cache`, heredocs, etc. that Kaniko silently ignores. **PSA caveat**: rootless Buildah needs `CAP_SETUID` + `CAP_SETGID` for its user-namespace setup, so the build namespace must be PSA `baseline` or a custom profile permitting both — `restricted` drops them and the rootless setup fails. Storage driver is configurable via `builder.buildah.storageDriver` (`vfs` works without kernel modules; `overlay` is faster but needs fuse-overlayfs on the nodes).
- **`docker`**: shells out to the local Docker daemon via the bind-mounted host socket. Convenient on single-node test clusters; gives the Cooker container root-equivalent access to the host's Docker. An RCE in Cooker → full host control. Only use this on isolated dev hosts.
- **`buildkit`**: stub; not yet wired (backlog P9.1).

The Helm chart conditionally drops the `docker.sock` volume + mount when `builder.kind != "docker"`, so any of `kaniko` / `buildah` / `buildkit` carries no leftover host paths.

**Pusher gate (F-02):** The `COOKER_PUSHER=docker` adapter shells out to the Docker CLI for `docker push`, which uses the same bind-mounted host socket as the `docker` builder. `Config.Validate()` now refuses to boot in production when `COOKER_PUSHER=docker` is set, closing the gap where an operator who correctly switches `COOKER_BUILDER=kaniko` but leaves `COOKER_PUSHER=docker` would still expose the docker.sock surface via the push path. Use `COOKER_PUSHER=crane` in production. The **raw-Kubernetes parity manifest** at `deploy/kubernetes/deployment.yaml` does **not** mount the host docker socket either (S26-05-04, closed on `claude/sec-quickwins-2026-05`); operators who legitimately need the socket must author a deliberate variant and accept the RCE-to-host gap explicitly.

**Docker-host deploy runtime (compose deployment DAGs):** An App whose deploy target is `docker-host` runs its per-service deploy stages through the `docker-run` / `compose` deployers (`backend/internal/deployer/dockerrun.go`, `compose.go`), which shell out to the local `docker` CLI (`docker run` / `docker compose up`). These talk to the Docker daemon (`DOCKER_HOST`, default the host socket) and therefore carry the **same root-equivalent RCE-to-host exposure as the `docker` builder/pusher** — an RCE in Cooker becomes host control. They are intended for **single-node / dev hosts only**; for cluster deployments use a Kubernetes deploy target (the per-service manifests apply via client-go, no socket). The deployers fail closed with `ErrUnavailable` when the `docker` CLI is absent. Resource limits parsed from the compose file are applied as `docker run --memory/--cpus` (and as K8s `resources.limits` on the Kubernetes path).

### Data Security

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
- **WebSocket**: two-layer auth — same-origin policy via `gorilla/websocket` `CheckOrigin` (sharing the CORS allowlist) **and** a single-use ticket. Clients `POST /api/v1/ws-tickets` over the authenticated API to obtain a 60-second ticket and open `/ws/...` with `?ticket=<value>`. Tickets are consumed on first use; replay is rejected. The per-stage log channel `/ws/runs/:runId/stages/:stageId/logs` (added with the AppHealthChecker work) uses the same ticket gate; no new ingress.
- **App health probe**: `AppHealthChecker` runs in-process inside the cooker pod and reads from each deploy target via the existing in-cluster credentials (kubeconfig / SDK clients). It does NOT add an inbound network surface; egress is to the same target-backend endpoints the executor already talks to.
- **Ingress**: TLS termination recommended at the ingress controller level.
- **Internal traffic**: Backend-to-database and backend-to-Redis communication should use encrypted connections in production.

### Audit logging

Cooker emits a structured audit event for every authenticated mutating call (POST, PUT, PATCH, DELETE under `/api/v1`). Defaults: **enabled in production**, off elsewhere. Events are JSON, one per request, written to either stdout (default) or a file.

- Tunable via `COOKER_AUDIT_ENABLED` (default `true` when `COOKER_ENV=production`, else `false`), `COOKER_AUDIT_DESTINATION` (`stdout` or `file`), and `COOKER_AUDIT_FILE_PATH`.
- Each event records: timestamp, OIDC subject, OIDC email, HTTP method, route template (e.g. `/api/v1/environments/:id/secrets/:key` — never the concrete `:id`), status code, latency, client IP.
- **Bodies are never captured.** The middleware does not read request or response bodies, so secret-bearing routes (`PUT /environments/:id/secrets/:key`, `PUT /apps/:id/webhook`) are safe by construction. Bearer tokens, OIDC raw JWTs, and secret values cannot appear in the trail.
- The redacted-route allowlist lives in `internal/audit/audit.go` (`IsRedacted`). Future changes that introduce body capture must consult it.
- Forward stdout to your SIEM via the cluster's logging stack (Loki, ELK, Datadog) or use the file sink with a sidecar tail-shipper.

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
- [x] Verify release artifact signatures *(cosign keyless signing ships with every release from v0.1.0; see "Supply chain and release signing" and `docs/RELEASING.md §Step 4`)*
