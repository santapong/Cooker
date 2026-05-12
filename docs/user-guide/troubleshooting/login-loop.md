# Sign-in loop

You click "Sign in", land on the IdP, sign in successfully, get redirected back to Cooker, and immediately get redirected to the IdP again. Loop.

This is the most common Cooker auth issue and almost always one of four root causes.

## 1. Redirect URI mismatch

**Check first:** the URI registered at the IdP must EXACTLY match `COOKER_OIDC_REDIRECT_URL`.

- Including trailing slash. `/callback` and `/callback/` are different.
- Including scheme. `http://` and `https://` are different.
- Including port if non-standard.

Open the IdP's console for the Cooker client; copy the registered Redirect URI. Compare against:

```sh
kubectl -n cooker exec deploy/cooker -- printenv COOKER_OIDC_REDIRECT_URL
```

Must match character-for-character.

## 2. Frontend vs backend mismatch

**Check:** `VITE_OIDC_*` (build-time, baked into the JS bundle) must agree with `COOKER_OIDC_*` (runtime).

The frontend reads its OIDC config from compile-time env vars. If you change `COOKER_OIDC_ISSUER_URL` in Helm and don't rebuild the image, the SPA still uses the old issuer in its sign-in redirect. The IdP sees an unknown `client_id` or `redirect_uri`, redirects back, the backend's middleware rejects the resulting token because the `iss` claim doesn't match, and you bounce.

In the browser DevTools, **Network -> XHR -> the `.well-known/openid-configuration` request** shows the issuer the SPA actually used. Compare that against `COOKER_OIDC_ISSUER_URL`.

**Fix:** rebuild the Cooker image with matching `VITE_OIDC_*` values, OR set the SPA values via a runtime injection (currently no first-party support; you'd patch `index.html`).

## 3. Clock skew

**Check:** the IdP issues JWTs with `iat` / `exp` based on its clock; Cooker rejects tokens issued in the future or expired in the past with a 5-minute leeway.

```sh
# Inside the Cooker container:
kubectl -n cooker exec deploy/cooker -- date

# Inside the IdP (if accessible) OR check the JWT.
```

JWT inspection (NOT in production — these are bearer tokens):

```sh
# Decode the access token (header.payload.signature, base64url).
echo '<jwt>' | cut -d. -f2 | base64 -d 2>/dev/null | jq
# Look at iat (issued at) and exp.
```

**Fix:** sync the Cooker node's clock (`chrony` or systemd-timesyncd). Cluster-wide NTP. Don't disable the clock skew tolerance — it's protective.

## 4. JWKS rotation issue

**Check:** the IdP rotated its signing key after Cooker had already cached the JWKS. The new JWT's `kid` isn't in Cooker's cache.

Today Cooker re-fetches JWKS lazily on `kid not found`, so this is usually self-healing. If it isn't, the IdP's JWKS endpoint might be unreachable from inside the cluster:

```sh
# From inside the Cooker pod:
kubectl -n cooker exec deploy/cooker -- wget -qO- https://auth.example.com/.well-known/openid-configuration
# Should return JSON with a "jwks_uri" field.

kubectl -n cooker exec deploy/cooker -- wget -qO- https://auth.example.com/<jwks_uri>
# Should return JSON with a "keys" array.
```

**Fix:** unblock the IdP from the cluster. Check egress NetworkPolicy. Check egress firewall.

## 5. Token verification reflected error

In `slog` logs, you may see:

```text
{"level":"WARN","msg":"oidc: token verify failed","err":"oidc: id token issued by a different provider, expected ..."}
```

This is informative. Match the error string against:

| Substring | Means |
|---|---|
| `iss mismatch` | IdP issuer URL ≠ `COOKER_OIDC_ISSUER_URL`. Often a trailing-slash bug. |
| `aud mismatch` | The token's `aud` doesn't contain `COOKER_OIDC_CLIENT_ID`. |
| `kid not found` | JWKS cache miss; see #4. |
| `token used before issued` | Clock skew; see #3. |
| `signature is invalid` | The IdP's signing key changed and the JWKS cache hasn't refreshed; see #4. |

> **Reflected to client today.** This error is currently reflected back to the browser in the response body. The [security review](../../audits/2026-05-security-review.md) tracks this as `S26-05-01`; future versions will return a generic `invalid token` and log the detail server-side only.

## When it works once and breaks on refresh

If the first sign-in succeeds but subsequent requests fail, the issue is the **refresh** flow:

- Refresh tokens are stored by `oidc-client-ts` in `localStorage`.
- The SPA calls the IdP's `/token` endpoint with the refresh token to get a new access token.
- If the refresh-token request fails (CORS issue, refresh token revoked, IdP returning the wrong scope), the SPA silently triggers `signinRedirect()` again — and you're in the loop.

Open DevTools -> Network and look for `POST` to `<issuer>/token`. The response tells you why.

Common cause: the IdP doesn't issue refresh tokens unless you request `offline_access` scope. Add it to the IdP's enabled scopes for the client.

## Last resort

Sign in with dev mode disabled (`COOKER_OIDC_ENABLED=false`, `COOKER_LOCAL_AUTH_ENABLED=false`) and access the API directly:

```sh
kubectl -n cooker port-forward deploy/cooker 8080:8080
curl http://localhost:8080/api/v1/auth/methods
# Should reflect what the SPA sees.
```

If this works, the bug is in the OIDC config; if it doesn't, the bug is bigger (DB, networking).

## Cross-references

- **[Auth & RBAC](../operations/auth-and-rbac.md)** — OIDC setup.
- **[Configuration](../getting-started/configuration.md)** — env vars.
- **[`docs/audits/2026-05-security-review.md`](../../audits/2026-05-security-review.md)** — `S26-05-01` (error reflection).
