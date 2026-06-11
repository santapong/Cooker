# skeleton — review before apply; prices/assumptions in docs/guides/DEPLOY-AWS-VERCEL.md
#
# Version pins for the Cooker-on-AWS Terraform. Single state covers
# VPC / EKS-AutoMode / RDS / ElastiCache / ECR / Cognito / PodIdentity / Budgets.
#
# OpenTofu drop-in: this configuration is OpenTofu-compatible (>= 1.8).
# Swap the `terraform` CLI for `tofu`; the S3 native-locking backend works
# in both. No provider-specific TF-only features are used here.

terraform {
  # TF >= 1.10 is REQUIRED: we use the S3 backend's *native* state locking
  # (use_lockfile) which removes the need for a DynamoDB lock table.
  # See backend.tf.example.
  required_version = ">= 1.10.0"

  required_providers {
    aws = {
      source = "hashicorp/aws"
      # OPEN-version: aws provider ~> 6.0 is the assumed major. Confirm the
      # current major at apply time — provider majors occasionally rename
      # arguments (notably around EKS Auto Mode and ElastiCache Serverless).
      version = "~> 6.0"
    }
    # The kubernetes/helm providers are intentionally NOT declared here.
    # Helm is deployed from CI/operator (`helm upgrade --install`), not via
    # terraform helm_release, to keep app rollout decoupled from infra state.
    # The Spot NodePool and dockerconfig-refresh CronJob ship as *example*
    # k8s manifests (modules/cluster, modules/registry) applied out-of-band.
  }
}
