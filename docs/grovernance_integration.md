# Grovernance Platform integration (Phase 4)

Cooker delegates deploy authorisation to the Grovernance Platform's
`POST /authorize` endpoint when configured. Grovernance is a small Go service
that resolves the actor token (via KeepSave's `/userinfo`), looks up the
target service in its catalog, runs a fixed-rule decision (`prod` requires
membership in `ProdDeployerGroups` or in `AllowedServiceAccounts`), and
returns ALLOW or DENY with a reason and an audit id.

This document describes the Cooker side of the contract.

## Configuration

| Env var | Default | Meaning |
|---|---|---|
| `COOKER_GOVERNANCE_URL` | *(empty)* | Base URL for Grovernance. Empty means the integration is **disabled** — Cooker behaves as it did pre-Phase-4. |
| `COOKER_GOVERNANCE_FAIL_OPEN_ENVS` | `dev,staging` | Comma-separated list of envs that should proceed when Grovernance is unreachable (transport error / non-200). Production is implicitly fail-closed unless added here. |
| `COOKER_GOVERNANCE_BOOTSTRAP_SERVICES` | `governance` | Comma-separated list of service names that bypass the gate. Required so Grovernance itself can be deployed through Cooker. |

## How it gates a deploy

The middleware runs after `RequireRole(operator|admin)` and the rate
limiter, *before* the run is spawned. It extracts the actor's bearer token
from the `Authorization` header, looks up the App's name and Environment
name from the store, and calls `POST {COOKER_GOVERNANCE_URL}/authorize`:

```json
{
  "actor":    { "token": "<bearer-from-request>" },
  "action":   "deploy",
  "resource": { "service": "<app.name>", "env": "<environment.name>" },
  "context":  { "request_id": "<X-Request-ID>" }
}
```

Grovernance answers HTTP 200 with:

```json
{
  "decision": "allow" | "deny",
  "reason":   "...",
  "policy_id":"rule.prod.human",
  "audit_id": "<uuid>"
}
```

| Grovernance response | Cooker behaviour |
|---|---|
| `decision: allow` | request proceeds; `governance.audit_id` is stashed on the Gin context for downstream audit logging |
| `decision: deny` | 403 returned with `{error, reason, policy_id, audit_id, service, env}` |
| transport error AND env in `FAIL_OPEN_ENVS` | request proceeds (logged as warning) |
| transport error AND env NOT fail-open | 503 returned (fail-closed) |
| service in `BOOTSTRAP_SERVICES` | no HTTP call; request proceeds |

## Where it sits in the route

```
api.Group("/api/v1")
  -> OIDC middleware           (existing)
  -> audit middleware          (existing)
apps.POST("/:id/deploy",
  writeRole,                   // operator | admin
  expensive,                   // rate limit
  idempotencyMiddleware,
  RequireGovernanceAllow,      // <-- Phase 4
  h.DeployApp)
```

## Where it doesn't sit (yet)

`POST /pipelines/:id/run` is *not* gated by middleware in v1: a pipeline
can target multiple services / envs across its stages, and the natural
gate is a per-stage check inside the run executor at
`internal/service/executor.go::Execute` before each `StageTypeDeploy`.
That hook requires plumbing the actor token from the run starter into
stage context (a small store-side change), and is deferred to v1.1. App
deploys via `POST /apps/:id/deploy` synthesise a pipeline run but their
DENY is caught at the HTTP middleware *before* the run is spawned, so
that path is fully gated today.

## Test plan

- Unit tests: `internal/governance/client_test.go`,
  `internal/auth/governance_middleware_test.go`.
- E2E: see the verification section in
  `grovernance-platfrom/.claude/plans/so-now-can-you-sprightly-volcano.md`
  — boot Postgres + KeepSave + Grovernance + Cooker via docker-compose,
  drive a non-deployer prod deploy, expect 403.

## Operational notes

- The HTTP client uses a 2-second timeout. If Grovernance is slow under
  load, prod deploys fail closed.
- The middleware emits structured slog entries on DENY and on fail-closed
  503s. Wire them into your aggregator with the `governance.audit_id`
  field so a deny in Cooker can be cross-referenced to the audit row in
  Grovernance.
- The middleware is a *no-op* when `COOKER_GOVERNANCE_URL` is empty.
  Existing installs do not need to change anything to upgrade.
