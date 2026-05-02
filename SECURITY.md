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

Cooker uses **OpenID Connect (OIDC)** with **OAuth 2.0 PKCE** flow for authentication:

- **Protocol**: OIDC Discovery + Authorization Code with PKCE (no client secrets in browser)
- **Token validation**: JWT access tokens validated server-side using the OIDC provider's JWKS endpoint
- **Session management**: Short-lived access tokens with refresh token rotation
- **Supported providers**: Keycloak, Okta, Azure AD, Google, GitHub

> **Default in local & UAT:** OIDC is **disabled** (`COOKER_OIDC_ENABLED=false`) and the backend injects a dev admin user so contributors and testers can exercise the API without an IdP. Production deployments **must** enable OIDC — see the checklist below and [docs/UAT.md](docs/UAT.md#enabling-oidc-sign-in-for-uat) for how to wire Google or another provider.

### Authorization (RBAC)

Role-based access control with three tiers:

| Role | Permissions |
|------|-------------|
| **admin** | Full access: manage pipelines, deploy to all environments, configure settings, manage users |
| **operator** | Run pipelines, approve environment promotions, manage environments, view all resources |
| **viewer** | Read-only access to all resources, view pipeline runs and logs |

Roles are mapped from OIDC group claims. The mapping is configurable via environment variables or Helm values.

### OCI Registry Security

- **Authentication**: Registry credentials stored server-side only, never exposed to the frontend
- **Transport**: All registry communication over TLS (HTTPS)
- **Content trust**: Images tracked by content-addressable digests (`sha256:...`) per OCI image-spec
- **Supply chain**: Referrers API support for attaching signatures, SBOMs, and build provenance to images

### Kubernetes Access

- **In-cluster**: Uses Kubernetes service account tokens with least-privilege RBAC
- **External**: Kubeconfig-based access with configurable contexts
- **RBAC**: Cooker's service account ClusterRole is scoped to required resources only (deployments, pods, services, configmaps, secrets, namespaces)

### Docker Socket Security

When Cooker mounts the Docker socket (`/var/run/docker.sock`):

- This grants the container root-equivalent access to the Docker daemon
- **Production recommendation**: Use Kaniko for in-cluster image builds instead of Docker socket mounting
- **Alternative**: Use a TCP Docker endpoint with TLS mutual authentication (`DOCKER_TLS_VERIFY=true`)

### Data Security

- **Database**: Pipeline definitions, run history, and environment configs stored in PostgreSQL
- **Secrets**: Database passwords, OIDC client secrets, and registry credentials should be managed via Kubernetes Secrets or an external secret manager (e.g., HashiCorp Vault, AWS Secrets Manager)
- **Environment variables**: Sensitive configuration injected at runtime, never baked into images
- **No secrets in pipelines**: Pipeline variable values are stored in the database; sensitive values should use secret references rather than plaintext

### Network Security

- **CORS**: Configurable allowed origins via `COOKER_ALLOWED_ORIGINS`. Defaults to `localhost:5173,localhost:3000` for `COOKER_ENV=dev|uat`; defaults to **deny-all** for `COOKER_ENV=production` so missing config is loud, not silent.
- **`Allow-Credentials`**: explicitly **off**. Cooker authenticates via `Authorization: Bearer <jwt>` headers, not cookies — credentials mode adds no value and would block wildcard reflection.
- **WebSocket**: two-layer auth — same-origin policy via `gorilla/websocket` `CheckOrigin` (sharing the CORS allowlist) **and** a single-use ticket. Clients `POST /api/v1/ws-tickets` over the authenticated API to obtain a 60-second ticket and open `/ws/...` with `?ticket=<value>`. Tickets are consumed on first use; replay is rejected.
- **Ingress**: TLS termination recommended at the ingress controller level.
- **Internal traffic**: Backend-to-database and backend-to-Redis communication should use encrypted connections in production.

### Rate limiting

Cooker applies a **per-user, in-memory rate limit** to the most expensive endpoints (pipeline runs, Docker image builds, App deploys) so a single user cannot accidentally fork-bomb builds. Defaults: 10 requests/minute, burst 3, keyed on the OIDC subject claim.

- Tunable via `COOKER_RATE_LIMIT_ENABLED` (default `true`), `COOKER_RATE_LIMIT_PER_MINUTE` (default `10`), `COOKER_RATE_LIMIT_BURST` (default `3`).
- **Multi-replica deployments must disable** this (`COOKER_RATE_LIMIT_ENABLED=false`) — the limiter is per-process and won't share state across replicas. Use ingress / WAF rate limiting instead.
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
- [ ] Use Kaniko instead of Docker socket for image builds
- [ ] Enable PostgreSQL SSL connections
- [ ] Set up audit logging
- [x] Run the container as non-root *(image runs as UID 65532 by default)*
- [x] Enable network policies to restrict pod-to-pod traffic *(NetworkPolicy ships with the Helm chart, gated by `networkPolicy.enabled`; raw manifest at `deploy/kubernetes/network-policy.yaml`)*
- [ ] Regularly update base images and dependencies
