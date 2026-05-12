# Promotions (Dev -> Staging -> Prod)

Promotion is how a successful run moves a build from one environment to the next. The classic flow is Dev -> Staging -> Prod, with manual approval before Prod. Cooker supports this end-to-end.

## The mental model

```text
   Pipeline run
   │
   ├─► Dev stages run (auto)        ──► EnvironmentStatus[dev]    = deployed
   │      │
   │      ▼
   ├─► Staging stages run (auto)    ──► EnvironmentStatus[staging] = deployed
   │      │
   │      ▼
   └─► Production stages WAIT       ──► EnvironmentStatus[prod]   = awaiting_approval
          │
          │   POST /approve
          ▼
       Production stages run (auto) ──► EnvironmentStatus[prod]    = deployed
```

The Run-level status is `running` throughout this until the last environment hits `deployed`, then it flips to `success`.

## Configure the environments

For each environment (Dev, Staging, Production), set its `PromotionPolicy`:

| Field | Dev | Staging | Prod |
|---|---|---|---|
| `strategy` | `auto` | `auto` | `manual` |
| `requiredApprovers` | 0 | 0 | 1 |
| `autoPromoteOn` | `[]` | `[]` (or `["tests_pass"]`) | `[]` |

Via API:

```bash
curl -X PUT https://cooker.example.com/api/v1/environments/<PROD_ENV_ID> \
     -H 'Authorization: Bearer <jwt>' \
     -H 'Content-Type: application/json' \
     -d '{
       "name": "production",
       "order": 3,
       "promotion": {
         "strategy": "manual",
         "requiredApprovers": 1
       },
       "target": {
         "type": "namespace",
         "namespace": "cooker-prod"
       }
     }'
```

> **Partial.** `requiredApprovers > 1` is on the model but the approval handler short-circuits on the first approval. Multi-approver gating is not yet wired.

## Build the pipeline with swimlanes

In the pipeline editor:

1. Drop swimlane nodes for each environment.
2. Drop stages into the right swimlane — each stage's `environmentId` is set to the swimlane it's dropped into.
3. Connect Build / Push stages outside the swimlanes (they're "environment-agnostic"); connect Deploy stages inside the relevant swimlane.

<!-- SCREENSHOT: pipeline editor showing three swimlanes (Dev / Staging / Prod) with Deploy stages inside each, an Approval node before the Prod swimlane -->

A common shape:

```text
   Build  ──►  Push  ──►  Deploy-Dev  ──►  Deploy-Staging  ──►  Approval  ──►  Deploy-Prod
                          [Dev]            [Staging]                            [Prod]
```

The Approval stage is technically optional — Cooker pauses on the environment boundary itself when the downstream env is `strategy=manual`. The explicit Approval node is useful when you want a visible "this is where humans intervene" marker.

## Run and approve

Run the pipeline. It progresses through Dev and Staging automatically. When it hits the Prod boundary, the run pauses:

- Run status: `running`
- `EnvironmentStatus[prod].Status`: `awaiting_approval`
- The UI shows an "Approve" button on the run detail page.

Click **Approve**, or call the API:

```bash
curl -X POST https://cooker.example.com/api/v1/pipelines/<PIPELINE_ID>/runs/<RUN_ID>/approve \
     -H 'Authorization: Bearer <jwt>'
```

The handler `ApprovePromotion` checks `auth.CanApprovePromotion(claims)` which is satisfied by the `approver` or `admin` role. The approver's `Subject` claim is recorded in `EnvironmentStatus.ApprovedBy`.

The Prod stages then run.

## Approver UX

Approvers see runs with `awaiting_approval` status in their dashboard. Filter via:

```bash
GET /api/v1/pipelines/<ID>/runs?status=awaiting_approval
```

> **Partial.** A cross-pipeline "things waiting for me" view isn't yet wired in the frontend — today approvers need to navigate per-pipeline. Track in roadmap `D11`-ish (empty-state CTAs).

## Step-up MFA on approve

If your install has `COOKER_OIDC_MFA_ACR_VALUES` set, destructive admin routes require an MFA-bearing JWT. The Approve endpoint does NOT currently require MFA — approvals are a lesser-privileged operation than secret reveal. If you want MFA on approvals, add the route to the gated set in `internal/server/router.go` (a code change).

The W11 SaaS-team walkthrough flagged that approvers get an unexpected `403 mfa_required` on Approve only if they happen to also be `admin` and hit one of the MFA-gated routes during the same session. The remedy is to sign back in via the MFA flow; the IdP runs the second factor and the new JWT carries the required `acr` claim.

## Cancelling between environments

Cancel a run while it's `awaiting_approval`:

```bash
curl -X POST https://cooker.example.com/api/v1/pipelines/<ID>/runs/<RUN_ID>/cancel \
     -H 'Authorization: Bearer <jwt>'
```

Cancelled runs cannot be approved or re-resumed. To deploy the same commit again, start a new run from the pipeline.

## Promoting secrets between environments

Independent of promoting runs. `POST /api/v1/environments/:id/secrets/promote` copies a chosen set of secret keys from this environment to another. Requires admin role + MFA. Returns `501 Not Implemented` for secrets backends that don't implement `secrets.Promoter` (today: `database` doesn't; `keepsave` does).

See [Secrets](secrets.md#promotion).

## Multi-environment status in the run

`PipelineRun.EnvironmentStatuses` carries one entry per environment the run touched:

```json
{
  "environmentStatuses": [
    {"environmentId": "dev",     "status": "deployed", "promotedAt": "2026-05-12T..."},
    {"environmentId": "staging", "status": "deployed", "promotedAt": "2026-05-12T..."},
    {"environmentId": "prod",    "status": "awaiting_approval"}
  ]
}
```

The UI's EnvironmentBar reads this and updates as new statuses flow in over the run's WebSocket channel.

## Cross-references

- **[Environments](../concepts/environments.md)** — promotion policy fields.
- **[Stages: Approval](../concepts/stages.md#approval-stagetypeapproval)** — the explicit-approval-node shape.
- **[Auth & RBAC](../operations/auth-and-rbac.md)** — who can approve what.
