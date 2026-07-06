# Cooker in the Three-Project Vercel Stack (KeepSave + Grovernance + Cooker)

**Scope:** the integration layer this repo's existing guides don't cover — running Cooker alongside **KeepSave** (secrets backend + token issuer) and **Grovernance-Platfrom** (preventive deploy gate), with all three frontends on Vercel. For the Cooker-only Vercel/Lightsail mechanics, [`deploy/vercel/README.md`](../../deploy/vercel/README.md) and [`DEPLOY-AWS-VERCEL.md`](DEPLOY-AWS-VERCEL.md) remain authoritative; this guide does not repeat them.

## Topology

Vercel hosts the SPA only (split-origin — Vercel cannot proxy Cooker's build-log WebSocket, per `deploy/vercel/README.md`). The backend runs on a container host (Lightsail per the existing guide, or Railway/Render/Fly/K8s) with Neon Postgres (Vercel Marketplace).

```
Vercel: cooker SPA ──fetch/wss──► cooker :8080 (container host) ──► Neon (db: cooker)
                                     │ secrets                │ deploy gate
                                     ▼                        ▼
                               KeepSave API            Grovernance POST /authorize
                          (COOKER_SECRETS_BACKEND)     (blocks deploys on DENY)
```

## Vercel project

Exactly per `deploy/vercel/README.md`: Root `frontend`, Vite, `npm run build` → `dist`, env `VITE_API_BASE_URL=https://<cooker-api>` (the WS URL derives from the same var via `frontend/src/api/origin.ts`), plus `VITE_OIDC_*` when OIDC is enabled.

## Backend env — production, three-project wiring

Per `Config.Validate()` (`backend/internal/config/config.go`); full reference in [`../user-guide/reference/env-vars.md`](../user-guide/reference/env-vars.md):

| Variable | Value / note |
|---|---|
| `COOKER_ENV` | `production` |
| `DATABASE_URL` | Neon, `sslmode=require` (enforced for non-localhost hosts) |
| `COOKER_ALLOWED_ORIGINS` | `https://<vercel-domain>` — wildcard rejected in production |
| `COOKER_SECRETS_BACKEND` | `keepsave` (then `COOKER_SECRET_KEY` is not required) |
| `COOKER_SECRETS_KEEPSAVE_URL` | `https://<keepsave-api>` |
| `COOKER_SECRETS_KEEPSAVE_PROJECT_ID` | KeepSave project for Cooker's secrets |
| `COOKER_SECRETS_KEEPSAVE_API_KEY` | project/environment-scoped KeepSave API key |
| `COOKER_GOVERNANCE_URL` | `https://<governanced>` — enables the pre-deploy gate |
| `COOKER_GOVERNANCE_CALLER_TOKEN` | KeepSave **service token** with scope `governance.authorize` (required once the URL is set) |
| `COOKER_BUILDER` / `COOKER_PUSHER` | must not be `docker` in production — start with `noop`, move to `kaniko`/`crane` once a K8s cluster exists |
| `COOKER_OIDC_*` | optional; any standard OIDC issuer |

Notes:

- **Single replica** to start: memory backends for rate-limit / WS-ticket / WS-hub are fine at 1 replica; `COOKER_REPLICA_COUNT>1` without sticky sessions requires the Redis backends (enforced at boot; see [`MULTI_REPLICA.md`](MULTI_REPLICA.md)).
- **WebSockets**: the browser connects `wss://<cooker-api>` directly — the container host must pass WebSockets through (Lightsail+Caddy, Railway, Render, and Fly all do).
- **Git-provider webhooks** point at the **backend** origin (`https://<cooker-api>/webhooks/...`), never the Vercel origin. The dedicated per-source-IP limiter on those unauthenticated receivers (`COOKER_WEBHOOK_RATE_LIMIT_*`, on by default — PR #122) is fine at defaults for a single replica.

## Bring-up order (whole stack)

1. Neon: databases keepsave / grovernance / cooker.
2. KeepSave backend first (issues the tokens and holds the secrets) — verify its JWKS endpoint.
3. Grovernance backend (verifies KeepSave JWTs) — verify `POST /authorize`.
4. **Cooker backend** — verify boot passes `Config.Validate()`, secrets resolve from KeepSave, and a deploy attempt hits the gate: a non-deployer targeting prod must be **blocked with a readable reason** (and an audit row on the Grovernance side); an authorized deployer succeeds.
5. Three Vercel frontend projects; add each Vercel origin to its backend's allowed-origins list.
6. Smoke: run a pipeline with the `noop` builder from the Vercel SPA and watch the live log stream over the direct WebSocket.
