<!--
  skeleton / advisory — review before apply.
  Prices and assumptions live in docs/guides/DEPLOY-AWS-VERCEL.md
  (date-stamped 2026-06-11; re-verify every price at apply time).
-->

# Cooker UAT frontend on Vercel

This directory holds the Vercel project config (`vercel.json`) and the
setup recipe for hosting **only the Cooker SPA** on Vercel, talking to a
Cooker backend that runs elsewhere (an AWS Lightsail instance — see
§Lightsail below). It is the UAT half of the hosted topology documented
in [`../../docs/guides/DEPLOY-AWS-VERCEL.md`](../../docs/guides/DEPLOY-AWS-VERCEL.md).

> **Why split-origin?** Vercel rewrites **cannot proxy WebSockets**, and
> Vercel Serverless Functions don't support WebSocket connections (source:
> vercel.com/kb "Do Vercel Serverless Functions support WebSocket
> connections?"; the HTTP proxy also caps responses at 120s per
> vercel.com/docs limits — retrieved 2026-06-11). Cooker's live build-log
> stream is a long-lived WebSocket, so the SPA must talk to the backend
> **origin directly**, not through a Vercel rewrite. The frontend already
> supports this: `VITE_API_BASE_URL` drives both `fetch` and the WS
> authority via `frontend/src/api/origin.ts` (landed in commit `cc5343e`).

## Project settings (Vercel dashboard → Project → Settings)

| Setting | Value | Notes |
|---|---|---|
| Root Directory | `frontend` | The SPA lives under `frontend/`, not repo root. |
| Framework Preset | `Vite` | Vercel autodetects; confirm it's Vite. |
| Build Command | `npm run build` | Vite build → `dist/`. |
| Output Directory | `dist` | Vite's default. |
| Install Command | `npm ci` (default) | Uses `frontend/package-lock.json`. |
| Node.js Version | `20.x` | Matches CI (`actions/setup-node` node-version `20`). |
| Deployment Protection | **Disabled** | See below — this is deliberate. |

`vercel.json` (this directory) is the SPA-routing + asset-caching config:
a catch-all rewrite to `/index.html` (client-side routing) and a
1-year immutable `Cache-Control` on `/assets/*` (Vite content-hashes
those filenames, so they're safe to cache forever).

> **Where does `vercel.json` go?** With Root Directory = `frontend`,
> Vercel reads `vercel.json` from `frontend/`. This file is kept here under
> `deploy/vercel/` as the source of truth; copy or symlink it to
> `frontend/vercel.json`, **or** set the project's Root Directory config to
> point Vercel at it. Keep the two in sync — CI does not template Vercel
> config.

### Deployment Protection → Disabled (and why)

Vercel's **Deployment Protection** (Vercel Authentication / password) puts
a Vercel-owned auth wall in front of every preview and (optionally)
production deployment. We turn it **off** because:

- Cooker has its **own** auth (OIDC on the fixed UAT domain; local
  email+password on previews — see below). A second, Vercel-owned wall is
  redundant and blocks automated smoke tests and shareable preview links.
- Preview URLs are per-PR and ephemeral; the backend they talk to is the
  shared UAT backend, which enforces its own `COOKER_ENV=uat` posture.

If you want to gate previews without Vercel Authentication, use Vercel
**Shareable Links** (a per-deployment unguessable URL) instead — that
keeps previews link-private without adding an auth wall the SPA's own
auth would fight with.

## Environment variable matrix

Set these in Vercel → Project → Settings → Environment Variables. Vercel
scopes vars per environment (Production / Preview / Development); the SPA
reads them at **build** time (Vite inlines `VITE_*` into the bundle), so a
change requires a redeploy.

| Variable | Production env | Preview env |
|---|---|---|
| `VITE_API_BASE_URL` | `https://uat-api.<domain>` | `https://uat-api.<domain>` (same shared backend) |
| `VITE_OIDC_ENABLED` | `true` | `false` |
| `VITE_OIDC_AUTHORITY` | `<issuer URL of the UAT IdP>` | _(unset — OIDC off on previews)_ |
| `VITE_OIDC_CLIENT_ID` | `<client id registered for the fixed UAT domain>` | _(unset)_ |
| `VITE_OIDC_REDIRECT_URI` | `https://<fixed-uat-domain>/auth/callback` | _(unset)_ |

> Replace `<domain>` / `<fixed-uat-domain>` with your real DNS names. The
> backend origin (`uat-api.<domain>`) is the Lightsail instance behind
> Caddy (see §Lightsail). The exact `VITE_OIDC_*` variable names must match
> what `frontend/src/auth/OIDCProvider.tsx` reads — confirm against the
> source before setting them.

### Why previews use local auth, not OIDC

Vercel mints a **new, unguessable hostname per preview deployment**
(`cooker-<hash>-<scope>.vercel.app`). Real OIDC can't be used on previews
because:

- **Google forbids wildcard redirect URIs** — every redirect URI must be
  registered exactly (source: developers.google.com/identity/protocols/oauth2/web-server,
  retrieved 2026-06-11). You cannot pre-register an infinite set of
  per-PR hostnames.
- **Keycloak ≥ 24.0.3** tightened wildcard redirect matching so a `*`
  no longer crosses hostnames (source: keycloak.org/docs upgrading guide,
  retrieved 2026-06-11). The old "register `https://*.vercel.app/*`" trick
  no longer authorizes arbitrary preview hosts.

So previews run with `VITE_OIDC_ENABLED=false` and use Cooker's **local
email+password auth** against the shared UAT backend. That is legal in
`COOKER_ENV=uat` with `COOKER_ALLOWED_ORIGINS=*` (a wildcard CORS origin
is permitted in `uat`, never in `production`). The **fixed** UAT domain
(stable hostname) uses real OIDC with an exactly-registered redirect URI.

## Preview-auth recipe

1. Backend `.env.uat` (on the Lightsail host) sets:
   ```dotenv
   COOKER_ENV=uat
   COOKER_ALLOWED_ORIGINS=*
   # Local auth on; OIDC stays available for the fixed-domain path.
   ```
   The wildcard origin lets any per-PR Vercel preview hostname call the
   shared backend. **Only legal in `uat`** — `Config.Validate` rejects a
   wildcard origin under `COOKER_ENV=production`.
2. Vercel **Preview** env: `VITE_OIDC_ENABLED=false`, `VITE_API_BASE_URL`
   pointing at the shared backend. Testers sign in with a local UAT account.
3. Vercel **Production** env (the fixed UAT domain): `VITE_OIDC_ENABLED=true`
   with the OIDC vars above; the backend's registered redirect URI is the
   fixed domain's `/auth/callback` exactly.

## Backend `.env.uat` lines (on the Lightsail host)

```dotenv
COOKER_ENV=uat
COOKER_ALLOWED_ORIGINS=*            # legal only in uat; enables per-PR previews
COOKER_OIDC_ENABLED=true           # for the fixed-domain OIDC round-trip
COOKER_OIDC_ISSUER_URL=<UAT IdP issuer>
COOKER_OIDC_CLIENT_ID=<client id>
COOKER_OIDC_REDIRECT_URL=https://<fixed-uat-domain>/auth/callback   # exact, no wildcard
```

> The redirect URL registered at the IdP must be the **fixed UAT domain**,
> exactly. Google rejects wildcards; Keycloak ≥ 24.0.3 won't cross
> hostnames with a wildcard. Previews do not OIDC — they use local auth.

## Webhook target

GitHub / GitLab / Bitbucket / Gitea webhooks must point at the **backend
origin** (`https://uat-api.<domain>/...`), **not** the Vercel SPA origin.
The SPA serves no API and cannot receive webhooks. (The backend also
serves its own embedded copy of the UI at the API origin — see the
duplicate-UI note below.)

## Duplicate-UI note (ops fallback)

The Cooker single binary **embeds the SPA** and serves it at the API
origin too. So `https://uat-api.<domain>` will render a working Cooker UI
on its own. This is an intentional, kept ops fallback: if Vercel is down
or a preview is misconfigured, testers can hit the backend origin directly
and get the bundled UI. The Vercel deployment is the *primary* SPA host
(it gives per-PR previews — the sole reason to use Vercel here); the
embedded copy is the *fallback*. They will drift in version unless you
rebuild the image; treat the embedded UI as "good enough for ops", not the
canonical preview surface.

## Lightsail + Caddy backend setup (10-step sketch)

The backend half is one **AWS Lightsail 4 GB instance** ($24/mo bundle:
2 vCPU / 4 GB RAM / 80 GB SSD / 4 TB transfer, IPv4 included — source:
aws.amazon.com/lightsail/pricing/, retrieved 2026-06-11) running the UAT
compose stack behind Caddy with automatic Let's Encrypt TLS.

1. Create a Lightsail instance: Linux/Unix, a current LTS blueprint (or
   OS-only + install Docker), the **4 GB** bundle.
2. Attach a **static IP** and point DNS `uat-api.<domain>` (A record) at it.
   (Caddy needs the hostname resolvable before it can issue a cert.)
3. Open the firewall (Lightsail networking) for **80** and **443** only.
   SSH (22) stays restricted to your IP.
4. Install Docker + the compose plugin on the instance.
5. Clone the repo (or copy `docker-compose.uat.yml` + needed files) onto
   the host. Bring the stack up once to generate `.env.uat`
   (`make uat-up`), then edit `.env.uat` per the lines above.
6. Install Caddy (the OS package or the official container).
7. `Caddyfile` (reverse-proxy + auto-TLS):
   ```
   uat-api.<domain> {
       reverse_proxy localhost:8080
   }
   ```
   Caddy provisions and renews a Let's Encrypt cert automatically. The
   single `reverse_proxy` directive forwards HTTP **and** upgrades
   WebSocket connections transparently (Caddy passes `Upgrade`/`Connection`
   through by default) — which is exactly why the backend lives behind
   Caddy and not behind a Vercel rewrite.
8. Start Caddy; confirm `https://uat-api.<domain>/health/live` returns 200
   with a valid cert.
9. In Vercel, set `VITE_API_BASE_URL=https://uat-api.<domain>` (Production
   + Preview) and redeploy the SPA.
10. Smoke test: load the Vercel URL, sign in, trigger a pipeline run, and
    confirm the live log stream (WebSocket) flows. If logs stall, check
    Caddy is forwarding the WS upgrade and the backend `COOKER_ALLOWED_ORIGINS`
    admits the SPA origin.

### Budget alternative (no Vercel)

A **Hetzner CX22** (€3.79–4.35/mo — source: hetzner.com/news/new-cx-plans/,
retrieved 2026-06-11) can host the whole UAT stack with the embedded UI and
no Vercel at all (≈ $0.15/day). You lose per-PR previews (the only reason
to add Vercel). Use this when previews aren't worth the Vercel Pro seat.
