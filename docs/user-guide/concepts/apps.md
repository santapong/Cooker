# Apps vs Pipelines vs Environments

Cooker has three top-level user concepts. This page explains how they relate so you pick the right one for the job.

## The three nouns

| Noun | One-line definition | Source of truth |
|---|---|---|
| **Pipeline** | A DAG of stages, authored visually. Run on demand. | `model.Pipeline` |
| **App** | A repo + build plan + deploy target. Clone -> Build -> Push -> Deploy at the click of a button. | `model.App` |
| **Environment** | A named deploy destination (Dev / Staging / Prod) plus its variables and secrets. | `model.Environment` |

## When to use which

| You want to… | Use |
|---|---|
| Deploy a single repo on push to `main` | **App** with `autoDeploy=true` and a GitHub webhook. |
| Run a build + multiple parallel test suites + manual prod approval | **Pipeline**. |
| Share secrets across multiple Apps that deploy to the same cluster namespace | **Environment**. |
| Configure who can approve promotions to production | **Environment**'s `PromotionPolicy`. |
| Hand a non-engineer "click here to deploy" | **App**. |

## Apps in detail

An App is a higher-level shortcut around the Clone -> Build -> Push -> Deploy chain. The model is `model.App` (`backend/internal/model/app.go`):

| Field | Purpose |
|---|---|
| `name`, `description` | Identification. |
| `githubRepo` | `owner/name`. Cloned over HTTPS (deploy-key override is a backlog item). |
| `branch` | Default `main`. |
| `buildPlan` | Optional override. Nil means "detect at deploy time" (`buildplan` package looks for `Dockerfile`, `docker-compose.yml`, or buildpack-compatible source). |
| `deployTarget` | One of `kubernetes`, `cloud-run`, `ecs`, `fly`, `render`, `docker-host`. |
| `environmentId` | Links to an [Environment](environments.md) for plainVars + secrets. |
| `webhookSecret` | HMAC secret for the GitHub webhook (sealed with `Codec`). |
| `autoDeploy` | When true, every push event matching `branch` triggers a deploy. |
| `healthStatus` | Live health verdict from `AppHealthChecker` — `unknown` / `healthy` / `degraded` / `failed`. |

Clicking **Deploy** in the UI calls `POST /api/v1/apps/:id/deploy`. The handler synthesises a run, kicks it off in a goroutine, and streams logs over WebSocket channel `app-run:<runId>`.

### Build plan auto-detection

When `buildPlan` is nil, `internal/buildplan` inspects the cloned source and picks:

| Detected | Plan |
|---|---|
| `Dockerfile` at root | `kind=dockerfile`, path=`Dockerfile` |
| `docker-compose.yml` at root | `kind=compose`, path=`docker-compose.yml` |
| Otherwise | `kind=buildpack` (Paketo buildpacks) |

> **Partial.** The detector is not yet exposed in the New App wizard — operators can't see "we think your repo is a `dockerfile` build" before they click Deploy. Tracked as a W11 indie-persona gap.

### Health checks

`AppHealthChecker` runs every `COOKER_APP_HEALTH_INTERVAL` (default 30s) and dispatches to a per-deploy-target `Prober`. The verdict goes into `App.HealthStatus`. Health writes use a dedicated `AppStore.UpdateHealth` method (not `Update`) so they don't bump `App.Version` and race with user edits.

### App-run vs pipeline-run

Runs created via `POST /apps/:id/deploy` are stored under a synthetic pipeline ID `app-<appId>`. This means they don't appear in the Pipelines list. Intentional today; a proper "App runs" view is a follow-up. See [`docs/UAT.md`](../../UAT.md#known-limitations-uat-compose).

## Pipelines in detail

See [Pipelines](pipelines.md). The short version:

- Visual DAG authoring with arbitrary fan-out / fan-in.
- Per-edge conditions (`success` / `failure` / `always`).
- Environment swimlanes for stage-to-environment assignment.
- Optimistic concurrency on update (`Pipeline.Version`).

A pipeline is the right tool when you need anything more complex than "build then deploy" — parallel test matrices, conditional cleanup, multi-stage approval flows.

## Environments in detail

See [Environments](environments.md). An Environment is a named deploy destination with variables and secrets:

- `name` — `dev`, `staging`, `production`, or anything you choose.
- `order` — promotion order (lower → upstream).
- `target` — where to deploy (`type=cluster|namespace`, `clusterId`, `namespace`).
- `plainVars` — non-sensitive map; visible to any authenticated user.
- `secrets` — encrypted at rest via the configured [Secrets backend](../guides/secrets.md). Never serialised.
- `promotion` — `strategy=auto|manual`, `requiredApprovers`, `autoPromoteOn=[…]`.

An App or a Pipeline stage references an Environment to inherit its variables and secrets. Promotion between environments is configured here, not on the App or Pipeline.

## The relationship in one diagram

```text
   ┌──────────────┐         ┌──────────────────┐
   │     App      │────────►│  Environment     │
   │ (one repo,   │  uses   │  (vars,          │
   │  one target) │         │   secrets,       │
   └──────┬───────┘         │   promotion)     │
          │                 └────────┬─────────┘
          │ synthesises              │
          ▼                          │ referenced by
   ┌──────────────┐                  │
   │   Run        │                  │
   │ (Clone→Build │                  │
   │  →Push→Deploy)                  │
   └──────────────┘                  │
                                     │
   ┌──────────────┐                  │
   │   Pipeline   │──────────────────┘
   │ (DAG of      │  stage.environmentId
   │  stages)     │
   └──────┬───────┘
          │ runs
          ▼
   ┌──────────────┐
   │     Run      │
   └──────────────┘
```

## Common shapes

### Indie / single repo

One App per repo, `autoDeploy=true`, one Environment per target. No pipelines needed.

### Small team / 5-10 services

One App per service. Per-app Environments share secrets via `environmentId` reuse where appropriate (e.g. all apps point to `prod-environment` for production deploys).

### Platform team / 25+ services

A small Pipeline per shape ("Go service", "Node service", "static frontend"), with the App used only as a "Deploy" trigger. The Pipeline does the test-matrix work the App can't model on its own.

> **Known gap.** Cooker is single-tenant. All Apps / Pipelines / Environments live in one shared list visible to every authenticated user (RBAC gates writes, not reads). For multi-team isolation, see roadmap `C1` (multi-tenancy ADR is pending) and `S26-05-09` in the [security review](../../audits/2026-05-security-review.md).

## Cross-references

- **[Pipelines](pipelines.md)** — DAG semantics.
- **[Environments](environments.md)** — variables, secrets, promotion.
- **[GitHub webhooks](../guides/github-webhooks.md)** — wire up auto-deploy.
- **[First pipeline](../guides/first-pipeline.md)** — end-to-end walkthrough.
