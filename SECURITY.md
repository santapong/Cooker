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

- **CORS**: Configurable allowed origins (defaults to permissive in dev, should be restricted in production)
- **WebSocket**: Same-origin policy enforced in production (upgrade check)
- **Ingress**: TLS termination recommended at the ingress controller level
- **Internal traffic**: Backend-to-database and backend-to-Redis communication should use encrypted connections in production

### Container Image Security

Cooker's own Docker image follows these practices:

- **Multi-stage build**: Build dependencies not included in the final image
- **Minimal base**: Alpine Linux base image with only `ca-certificates`
- **Non-root**: Production deployments should use a non-root user (configurable in Helm values)
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
- [ ] Run the container as non-root
- [ ] Enable network policies to restrict pod-to-pod traffic
- [ ] Regularly update base images and dependencies
