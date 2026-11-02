# Frontend authentication modes

Vite reads authentication flags when it builds the frontend. They must match the backend serving that bundle; changing only the backend environment does not update the JavaScript already built.

| Backend mode | Frontend build flags | Startup without a saved session |
| --- | --- | --- |
| Local email/password | `VITE_LOCAL_AUTH_ENABLED=true`, `VITE_OIDC_ENABLED=false` | Signed out; show the local sign-in form |
| OIDC | `VITE_OIDC_ENABLED=true` plus the issuer/client/redirect settings | Restore the OIDC session or sign in |
| Both methods | Both flags `true`, plus OIDC settings | Restore a valid session or show enabled sign-in methods |
| Development auth bypass | Both flags `false` or unset | Synthetic Developer identity; only for a backend also running without authentication |

For a local-only bundle:

```sh
VITE_LOCAL_AUTH_ENABLED=true VITE_OIDC_ENABLED=false npm run build
```

The combined Docker image accepts `--build-arg VITE_LOCAL_AUTH_ENABLED=true`. The UAT Compose build reads the same variable from its environment. The flag is public configuration; never place passwords, Vercel tokens or signing keys in any `VITE_*` variable. A local-only build does not create an OIDC client or require an issuer/client ID.

Stored local sessions are verified against `/api/v1/auth/local/me` before protected pages load. Invalid sessions are cleared. Local sign-out clears the token and returns to sign-in. `/api/v1/auth/methods` decides which sign-in controls the server offers; it does not override build-time authentication mode.

If `/signin` repeatedly bounces to `/apps` and API calls return 401, check that a local-auth backend is paired with a frontend built with `VITE_LOCAL_AUTH_ENABLED=true`. Rebuild the frontend; do not disable backend authentication to work around a mismatched bundle.

## Verification

GitHub and Google buttons use an explicitly configured Keycloak broker. See the
[registration and activation guide](../docs/user-guide/operations/social-sign-in.md).
The server publishes `oidc.broker` and `oidc.socialProviders` through
`/api/v1/auth/methods`. A successful older response without these fields leaves
social methods disabled; a failed discovery request shows Retry. A button only
works when both the server advertises that provider and this bundle has OIDC
configured. Redirect hints do not replace the SDK's code/PKCE/state checks.


`npm run test:e2e:local-auth` builds a separate local-only bundle in `node_modules/.cache/cooker-local-auth-dist`, serves it on loopback port 4621, and tests empty, valid and expired sessions, sign-in return navigation and sign-out. It leaves the ordinary `dist` untouched. API replies and credentials in this suite are browser fixtures, not real account verification.

`npm run test:e2e -- e2e/airlock.spec.ts e2e/vercel-connections.spec.ts --workers=2` checks the existing development-mode baseline on port 4620. Neither suite adopts an existing server on its test port.
