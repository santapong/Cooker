# GitHub and Google sign-in

Cooker can offer **Continue with GitHub** and **Continue with Google** alongside
local email/password and organization single sign-on. These buttons use the
existing OIDC session through a Keycloak identity broker:

```text
Cooker → Keycloak → GitHub or Google → Keycloak → Cooker workspace
```

This is login authentication. GitHub repository access and the Vercel connection
are separate integrations. The new buttons do not grant deployment permissions.

## Current local status

The implementation is available in the backend candidate. The local server on
`http://127.0.0.1:4644` still uses its existing local accounts. Neither OAuth app
has been registered, and no broker has been activated for that server. The
buttons show **Setup needed** until both server and frontend are configured.
Your current Cooker account and connected Vercel projects remain available
through the existing local sign-in method.

## 1. Prepare Keycloak

Use an operator-managed Keycloak instance and a `cooker` realm. Both the browser
and Cooker backend must reach the **same issuer URL**. Use HTTPS for hosted
environments. The seeded users/passwords in Cooker's Keycloak UAT fixture are
for disposable tests; use your own instance and accounts here.

Create a public OIDC client with ID `cooker`, Standard Flow enabled and PKCE
method **S256**. Disable password/direct-access grants. For this local UI,
register the exact client redirect `http://127.0.0.1:4644/callback` and its origin
`http://127.0.0.1:4644`; register the exact post-logout destination too. Hosted
deployments need their own exact HTTPS URLs.

Add a `groups` client scope with a Group Membership mapper, claim name `groups`,
Full group path disabled, included in access and ID tokens. Add an audience
mapper for `cooker` to the access token: Cooker's existing verifier requires
that audience. Keep `profile`, `email` and `groups` available to the client.

Keep the default first-broker-login ownership verification. Do not enable
automatic account linking by matching email alone. If your instance requires
MFA, configure the provider Post Login Flow and ACR/LoA policy, then verify the
existing Cooker step-up gate with a real user; a social login alone is not MFA
evidence. See [Keycloak's administration guide](https://www.keycloak.org/docs/latest/server_admin/).

## 2. Register GitHub

1. In the realm, add the built-in GitHub identity provider with alias `github`.
   Copy its displayed **Redirect URI**.
2. Open [GitHub OAuth app registration](https://github.com/settings/applications/new).
   Name it Cooker, set your Cooker homepage, and paste the copied URI into
   Authorization callback URL. A typical broker URI is
   `https://<identity-host>/realms/cooker/broker/github/endpoint`.
3. Generate the client secret and enter it, with the Client ID, in Keycloak's
   GitHub provider form. Save and enable the provider. Keep external-token
   storage off unless a separately reviewed feature needs it. Login only needs
   identity/email access; do not request repository deployment scopes here.

Follow the current [GitHub registration instructions](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/creating-an-oauth-app).
The copied broker URI is authoritative, including any Keycloak path prefix.

## 3. Register Google

1. In [Google Cloud Console](https://console.cloud.google.com/), select/create the
   project for Cooker. Configure Google Auth Platform branding, audience and
   contact details. For External/Testing, add the intended test users.
2. Create an OAuth client of type **Web application**.
3. Add the Google identity provider in Keycloak with alias `google`. Copy its
   Redirect URI into the Google client's Authorized redirect URIs. It typically
   ends with `/realms/cooker/broker/google/endpoint`.
4. Enter that Client ID and secret in the Keycloak Google provider form, save,
   and enable. Use identity scopes (`openid`, `profile`, `email`). Do not add
   Drive, Gmail or other application scopes for login.

Google requires the redirect URI to match its registration. Follow its
[web application OAuth guide](https://developers.google.com/identity/protocols/oauth2/web-server).

**The provider callbacks go to Keycloak, not Cooker's `/callback`.** Keycloak
completes the broker round trip and sends its verified OIDC response to Cooker.

## 4. Enable the configured methods in Cooker

Only advertise providers after they work in the broker. Backend public settings:

```dotenv
COOKER_OIDC_ENABLED=true
COOKER_OIDC_BROKER=keycloak
COOKER_OIDC_SOCIAL_PROVIDERS=github,google
COOKER_OIDC_ISSUER_URL=https://<identity-host>/realms/cooker
COOKER_OIDC_CLIENT_ID=cooker
COOKER_OIDC_REDIRECT_URL=http://127.0.0.1:4644/callback
COOKER_LOCAL_AUTH_ENABLED=true
```

Use `github` or `google` alone if only one is ready. Leave the provider list
empty to disable both buttons. Generic OIDC providers still work without a
broker or social-provider list. Invalid/duplicate provider names and incomplete
social configuration fail server startup.

Rebuild the frontend using the same issuer and client:

```dotenv
VITE_OIDC_ENABLED=true
VITE_LOCAL_AUTH_ENABLED=true
VITE_OIDC_AUTHORITY=https://<identity-host>/realms/cooker
VITE_OIDC_CLIENT_ID=cooker
VITE_OIDC_REDIRECT_URI=http://127.0.0.1:4644/callback
VITE_OIDC_POST_LOGOUT_REDIRECT_URI=http://127.0.0.1:4644
VITE_OIDC_SCOPE=openid profile email groups
```

These values are public configuration. **GitHub and Google secrets belong only
in Keycloak**, never in `VITE_*`, chat, Git, screenshots or the browser bundle.
The public Cooker PKCE client does not need a client secret. Preserve the
existing database, local signing key and Cooker encryption key. Do not generate
new keys or reset accounts while enabling this feature.

The UAT Compose service forwards the two new backend variables. Other runtimes
must pass them explicitly in their deployment environment. Vite values are
build inputs; changing backend variables alone cannot update a shipped bundle.
See [frontend authentication modes](../../../frontend/AUTHENTICATION.md).

## 5. Check accounts and permissions

Broker users have their own issuer/subject identity. Cooker does not merge an
existing local account based on email. Keep your local administrator available.
Use trusted Keycloak groups for roles: `cooker-viewers`, `cooker-operators`,
`cooker-approvers`, and `cooker-admins`. Assign administrative membership
explicitly to the intended user; never make it the default for social users.
No matching group means viewer access. Review [Auth & RBAC](auth-and-rbac.md).

Acceptance after registering the apps:

- Each button opens the intended provider and returns to the requested local
  workspace page. Cancel/retry, expired callbacks and reloads behave sensibly.
- An ordinary social user cannot edit a DAG or perform administrative actions.
  An explicitly authorized admin satisfies the configured MFA requirement.
- Existing local sign-in/sign-out and the current Vercel connection still work.
- Provider denial, missing email and account linking require broker handling;
  they must not create an automatic local administrator or bypass verification.

Implemented local tests cover provider allowlisting, configuration discovery,
redirect failure and safe return navigation. Full GitHub/Google login, broker
account linking and MFA acceptance require the registered apps and running
broker; those have not been live-verified yet.
