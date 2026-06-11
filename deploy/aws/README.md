<!--
  skeleton / advisory — review before apply.
  Prices and assumptions live in docs/guides/DEPLOY-AWS-VERCEL.md
  (date-stamped 2026-06-11; re-verify every price at apply time).
-->

# Cooker on AWS (EKS Auto Mode)

Terraform + Helm artifacts for running Cooker production on AWS using
**EKS Auto Mode**. This is the AWS half of the hosted topology; the full
design, cost tables, and rationale are in
[`../../docs/guides/DEPLOY-AWS-VERCEL.md`](../../docs/guides/DEPLOY-AWS-VERCEL.md).

> **These costs are rough 2026-06-11 estimates**, not quotes. Every price
> carries a source in the guide. Re-verify at apply time — AWS pricing
> changes and your usage will differ.

## Directory map

```
deploy/aws/
├── README.md                  ← you are here
├── terraform/                 single-state IaC (no eksctl)
│   ├── versions.tf            TF + provider version pins
│   ├── backend.tf.example     S3 state backend (native locking; copy → backend.tf)
│   ├── variables.tf           all account-specific values as variables
│   ├── main.tf                wires the modules together
│   ├── modules/
│   │   ├── network/           VPC, subnets, NAT (1 or 3 by tier), endpoints
│   │   ├── cluster/           EKS Auto Mode, add-ons, Pod Identity, NodePools
│   │   ├── data/              RDS Postgres, ElastiCache Valkey, Secrets Manager
│   │   ├── registry/          ECR repo + lifecycle + pull-through cache
│   │   └── observability/     CloudWatch log group, Container Insights, Budgets
│   └── envs/
│       ├── prod-starter.tfvars
│       ├── prod-team.tfvars
│       └── prod-scale.tfvars
└── values/                    Helm overlays (deployed from CI/operator, not TF)
    ├── values-aws-starter.yaml
    ├── values-aws-team.yaml
    └── values-aws-scale.yaml
```

## Tier picker

Pick the smallest tier that fits, then grow (see the guide §6 "Scaling
path"). Headline `$/day` is the **us-east-1** baseline; `ap-southeast-1`
(the deploy recommendation) runs ~15–22% higher.

| Tier | Shape | us-east-1 $/day † | When |
|---|---|---|---|
| **Starter** | 1 replica, memory backends, no Redis, single-AZ RDS, 1 NAT, CloudWatch logs | **~$9.61** (~$292/mo) | First production; solo/small team; can tolerate single-AZ DB. |
| **Team** | 2 replicas, Redis (ElastiCache Serverless), Multi-AZ RDS, Spot builders, 3 NAT | **~$32.11** (~$977/mo) | HA needed; real concurrent users; isolated build capacity. |
| **Scale** | 3+ replicas + HPA, larger Multi-AZ RDS, bigger Spot pool, WAF | **~$54.09** (~$1,645/mo) | Sustained load; autoscaling; security posture (WAF). |

† Rough estimates, retrieved 2026-06-11; sources in the guide. `ap-southeast-1`
columns are in the guide's full tables.

## Prerequisites

- An AWS account with permissions to create VPC / EKS / RDS / ElastiCache /
  ECR / Cognito / IAM (Pod Identity) / Secrets Manager / Budgets.
- A **domain** with a Route 53 hosted zone (for ACM DNS validation + the
  app hostname).
- **Terraform ≥ 1.10** (uses S3 native state locking — no DynamoDB table).
  OpenTofu ≥ 1.8 is a drop-in alternative.
- `kubectl` and `helm` (v3) on your workstation or CI runner.
- (Cognito IdP path) budget for the **aud-claim spike** — see the guide's
  OPEN-7. Cognito access tokens carry `client_id`, not `aud`; Cooker's
  verifier enforces `aud == ClientID`. A half-day spike decides Lambda-fix
  vs a small configurable-audience code change **before** you commit to
  Cognito as the IdP.

## Bootstrap order

1. **State bucket** — create the S3 bucket for Terraform state, then copy
   `terraform/backend.tf.example` → `terraform/backend.tf` and fill in the
   bucket name. (TF ≥ 1.10 native lockfile; no DynamoDB.)
2. **Apply infra** — `terraform apply -var-file=envs/prod-<tier>.tfvars`.
   This stands up VPC, EKS Auto Mode, RDS, ElastiCache (Team+), ECR,
   Cognito, Pod Identity associations, and Budgets.
3. **Pod Identity associations** — created by Terraform (`modules/cluster`),
   they map the `cooker` SA → Secrets Manager read + ECR pull, and the
   `cooker-kaniko` SA → ECR push. `serviceAccount.annotations` in the Helm
   overlay stays `{}` (Pod Identity uses the SA name, not an IRSA
   annotation).
4. **Helm install** — `helm upgrade --install cooker deploy/helm/cooker
   -f deploy/aws/values/values-aws-<tier>.yaml` plus the per-cluster
   placeholders the overlay leaves empty (`database.host`,
   `ingress.annotations` certificate ARN, `oidc.issuerUrl`, etc.). Deploy
   Helm from CI or an operator workstation — **not** via Terraform
   `helm_release` (keeps app rollout decoupled from infra state).
5. **DNS cutover** — point the app hostname at the ALB the chart's ingress
   provisions (the AWS Load Balancer Controller that Auto Mode manages
   reconciles the `Ingress` into an ALB). Validate, then flip DNS.

## Pointer

Full runbook, BOM tables, traffic-flow diagrams, the rejected-alternatives
rationale, DR procedures, and the complete OPEN-questions list are in
[`../../docs/guides/DEPLOY-AWS-VERCEL.md`](../../docs/guides/DEPLOY-AWS-VERCEL.md).
