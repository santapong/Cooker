# Authentication and RBAC

Cooker offers two authentication paths. The default and recommended path is OpenID Connect (OIDC) with PKCE. A fallback **local email + password** path exists for homelab / single-user / "OIDC isn't available yet" cases.

This page covers operator setup. For the security architecture and threat model, see [`SECURITY.md`](../../../SECURITY.md).

## Auth methods

| Method | When | Status |
|---|---|---|
| **OIDC PKCE** | Teams, production. | Stable. |
| **Local email + password** | Homelab, single-user, OIDC not yet available. | Stable but minimal — see trade-offs below. |
| **Dev mode (auth off)** | UAT, local dev. | Defaults to injecting an admin user. NEVER in production. |

You can enable both OIDC and local at the same time. The middleware inspects the JWT's `iss` claim and dispatches to the right verifier.

## OIDC setup

### Step 1 — Register the client

Register Cooker as an OIDC client with your IdP:

| Field | Value |
|---|---|
| Application type | Web application (browser-based) |
| Grant type | Authorization code + PKCE |
| Redirect URI | `https://cooker.example.com/callback` |
| Logout redirect URI | `https://cooker.example.com` (optional) |
| Scopes | `openid profile email groups` |

PKCE means **no client secret is needed in the browser**. The backend may use the client secret during token introspection for some providers; the Helm chart accepts it via `oidc.clientSecretRef`.

### Step 2 — Configure Cooker

```bash
COOKER_OIDC_ENABLED=true
COOKER_OIDC_ISSUER_URL=https://auth.example.com    # No trailing slash unless your IdP's well-known says so
COOKER_OIDC_CLIENT_ID=cooker
COOKER_OIDC_CLIENT_SECRET=...                       # If your IdP requires it
COOKER_OIDC_REDIRECT_URL=https://cooker.example.com/callback
```

`go-oidc` discovers JWKS, issuer, and supported algorithms from `<issuer>/.well-known/openid-configuration`. The first request after boot triggers discovery; subsequent JWT validations use the cached JWKS.

### Step 3 — Configure the frontend

The frontend reads `VITE_OIDC_*` env vars at build time (baked into the JS bundle). For Helm, set them via `extraEnv` so they're available during the chart's build step. For non-Helm, rebuild the image with the right values.

| Variable | Value |
|---|---|
| `VITE_OIDC_AUTHORITY` | Same as `COOKER_OIDC_ISSUER_URL`. |
| `VITE_OIDC_CLIENT_ID` | Same as `COOKER_OIDC_CLIENT_ID`. |
| `VITE_OIDC_REDIRECT_URI` | Same as `COOKER_OIDC_REDIRECT_URL`. |
| `VITE_OIDC_ENABLED` | `true`. |

> **Gotcha.** Frontend OIDC env vars are baked at **build time**. Changing them in `values.yaml` after `helm install` doesn't take effect until you rebuild the image. UAT works around this by setting them in `.env.uat` before `make uat-up`.

### Provider examples

**Google.** Easiest for solo testing. https://console.cloud.google.com/apis/credentials -> Create Credentials -> OAuth client ID -> Web application. Authorized redirect: `https://cooker.example.com/callback`. Note: Google does not emit a `groups` claim by default, so all signed-in users fall back to `viewer`.

**Keycloak.** Realm -> Clients -> Create. `Client authentication: Off` (PKCE). `Valid redirect URIs: https://cooker.example.com/callback`. Configure a "Group Membership" mapper that adds groups to the ID token's `groups` claim.

**Okta.** Applications -> Create -> OIDC -> Web Application -> PKCE. Grant types: Authorization Code, Refresh Token. Redirect URI as above.

**Azure AD (Entra).** App registrations -> New registration -> Single tenant. Redirect URI: `https://cooker.example.com/callback`. Token configuration -> Add optional claim -> `groups`.

**KeepSave.** Register Cooker as an OIDC client with redirect URI `https://cooker.example.com/callback` and PKCE enabled. Configure KeepSave to emit `groups` matching the role mapping.

## Group-to-role mapping

Cooker has four roles:

| Role | What it can do |
|---|---|
| **admin** | Everything. Reveal secrets, delete resources, configure registries / clusters. |
| **operator** | Run pipelines, create / update environments, deploy apps. |
| **approver** | Approve environment promotions. |
| **viewer** | Read-only. |

Roles are derived from the IdP's `groups` claim by `MapGroupsToRoles` (`internal/auth/rbac.go`).

### Default mapping

| Group claim | Role |
|---|---|
| `cooker-admins` | admin |
| `cooker-operators` | operator |
| `cooker-approvers` | approver |
| `cooker-viewers` | viewer |
| *(none of the above)* | viewer |

### Custom mapping

Set `COOKER_OIDC_GROUP_MAP` to a CSV of `group:role` pairs:

```bash
COOKER_OIDC_GROUP_MAP="platform-admins:admin,sre-team:operator,security-team:approver"
```

The custom mapping REPLACES the default. If you want both, include the defaults explicitly.

> **Known limit.** The group-role map is read **once at boot**. Revoking a user's group membership at the IdP doesn't propagate until the user's JWT expires AND their session is refreshed. Cooker re-derives roles from the new JWT on the next request, but does not actively force re-evaluation. Tracked as `S26-05-03`.

## Step-up MFA on destructive routes

You can require MFA on the most dangerous endpoints:

```bash
COOKER_OIDC_MFA_ACR_VALUES="mfa,phr"     # CSV of acceptable acr values
```

A request to a gated route succeeds only when the token's `acr` claim — or any value in the `amr` array — matches the configured set. Otherwise:

```json
{
  "error": "mfa_required",
  "acr_values": ["mfa", "phr"]
}
```

The frontend recognises this response and re-issues the OIDC sign-in with `acr_values=<configured>`, prompting the IdP to run the second factor.

Gated routes today:

- `DELETE` on pipelines / envs / apps / hosts
- `PUT` / `GET` / `DELETE` on environment secrets
- `POST` on environment secrets promote
- `PUT` on app webhook secret

Empty config disables the gate.

## Local auth (homelab / fallback)

> **Trade-off-heavy path. Read this section in full before enabling.**

Set:

```bash
COOKER_LOCAL_AUTH_ENABLED=true
COOKER_LOCAL_AUTH_JWT_SIGNING_KEY=$(head -c 32 /dev/urandom | base64)
COOKER_LOCAL_AUTH_TOKEN_TTL=12h
COOKER_LOCAL_AUTH_ALLOW_SIGNUP=true     # default; close after first admin signs up
```

Endpoints exposed:

- `POST /api/v1/auth/local/signup` — bcrypt-hashes the password, creates a `users` row. **First signup is granted admin** (bootstrap). Subsequent signups default to `viewer` and must be promoted by an admin.
- `POST /api/v1/auth/local/signin` — verifies bcrypt, issues HS256 JWT.
- `GET /api/v1/auth/local/me` — returns the authenticated user (works for both local and OIDC sessions).
- `GET /api/v1/auth/methods` — public probe so the SPA knows which form to render.

### Trade-offs

| Property | Local auth | OIDC |
|---|---|---|
| MFA enforcement | Not applicable — local tokens have no `acr` claim. | `COOKER_OIDC_MFA_ACR_VALUES` works. |
| Rate limiting on signin | **None at app layer.** Operators MUST enforce at the edge (nginx `limit_req`, Traefik rate-limit, etc). | Per-user rate limit applies once authenticated. |
| Revocation | No revocation list. A leaked JWT stays valid until `exp`. | Refresh tokens and IdP-side revocation. |
| Signup closure | `COOKER_LOCAL_AUTH_ALLOW_SIGNUP=false` to require admin-created accounts. | Not applicable. |
| Token storage in browser | `localStorage[cooker.local.token]` | `oidc-client-ts` (also `localStorage`) |

### When to enable local auth

- Homelab Cooker. One user. OIDC overkill.
- Single-user contractor / consultant. Same.
- Bootstrap an OIDC migration: create the local admin, then enable OIDC and promote the OIDC equivalent.

### When NOT to enable local auth

- Team installs. Use OIDC.
- Anywhere on the public internet without IP-based rate limiting at the edge.

## Dev mode (auth off)

When `COOKER_OIDC_ENABLED=false` AND `COOKER_LOCAL_AUTH_ENABLED=false`, the middleware injects a synthetic admin user on every request:

```json
{
  "subject": "dev-user",
  "email":   "dev@cooker.local",
  "roles":   ["admin"]
}
```

This is for UAT and local dev. The compose stack uses it by default — see [`docs/UAT.md`](../../UAT.md#enabling-oidc-sign-in-for-uat) for how to switch UAT to real OIDC.

`Config.Validate()` emits a `slog.Warn` if both are disabled in production. **It does NOT refuse to boot.** Closing this loophole is `S26-05-07`.

## Audit logging

When `COOKER_AUDIT_ENABLED=true` (default in production), every authenticated mutating call (POST / PUT / PATCH / DELETE under `/api/v1`) produces one structured event. See [Observability](observability.md#audit-log) and [`SECURITY.md`](../../../SECURITY.md#audit-logging).

## Cross-references

- **[Configuration](../getting-started/configuration.md)** — every OIDC env var.
- **[`SECURITY.md`](../../../SECURITY.md)** — full threat model.
- **[`docs/audits/2026-05-security-review.md`](../../audits/2026-05-security-review.md)** — open findings and what's already closed.
- **[Reference: env vars](../reference/env-vars.md#oidc)** — `COOKER_OIDC_*` and `COOKER_LOCAL_AUTH_*` enumerated.
