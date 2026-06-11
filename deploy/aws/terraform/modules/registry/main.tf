# skeleton — review before apply; prices/assumptions in docs/guides/DEPLOY-AWS-VERCEL.md
#
# Registry module: ECR repository + lifecycle policy + a pull-through cache
# rule for Docker Hub (the rate-limit fix). The dockerconfig-refresh CronJob
# (for the crane in-process pusher) ships as an EXAMPLE k8s manifest, not a
# TF-managed resource.

variable "name_prefix" { type = string }

# --- ECR repository -----------------------------------------------------------
resource "aws_ecr_repository" "cooker" {
  name                 = "cooker"
  image_tag_mutability = "IMMUTABLE" # content-addressable tags; avoid silent overwrites
  image_scanning_configuration {
    scan_on_push = true
  }
  encryption_configuration {
    encryption_type = "AES256"
  }
  tags = { Name = "${var.name_prefix}-ecr" }
}

# --- Lifecycle policy: expire untagged + cap tagged history -------------------
resource "aws_ecr_lifecycle_policy" "cooker" {
  repository = aws_ecr_repository.cooker.name
  policy = jsonencode({
    rules = [
      {
        rulePriority = 1
        description  = "Expire untagged images after 7 days"
        selection = {
          tagStatus   = "untagged"
          countType   = "sinceImagePushed"
          countUnit   = "days"
          countNumber = 7
        }
        action = { type = "expire" }
      },
      {
        rulePriority = 2
        description  = "Keep only the last 30 tagged images"
        selection = {
          tagStatus     = "tagged"
          tagPrefixList = ["v", "sha-", "main-"]
          countType     = "imageCountMoreThan"
          countNumber   = 30
        }
        action = { type = "expire" }
      },
    ]
  })
}

# --- Pull-through cache for Docker Hub (rate-limit fix) -----------------------
# Caches docker.io images in ECR so build Jobs don't hit Docker Hub's
# anonymous pull rate limit. This is the RECOMMENDED rate-limit fix and stays
# on regardless of the (optional, default-off) ECR interface endpoints.
#
# Docker Hub requires credentials for the pull-through rule; store them in
# Secrets Manager and reference the secret ARN here. Left as a variable-less
# placeholder secret so the skeleton stays account-agnostic.
resource "aws_secretsmanager_secret" "dockerhub" {
  # ECR pull-through for an authenticated upstream expects a secret named with
  # the ecr-pullthroughcache/ prefix.
  name = "ecr-pullthroughcache/${var.name_prefix}-dockerhub"
  tags = { Name = "${var.name_prefix}-dockerhub" }
}
# NOTE: populate this secret out of band with {"username","accessToken"} for a
# Docker Hub account. No secret_version here on purpose (keeps creds out of state/skeleton).

resource "aws_ecr_pull_through_cache_rule" "dockerhub" {
  ecr_repository_prefix = "docker-hub"
  upstream_registry_url = "registry-1.docker.io"
  credential_arn        = aws_secretsmanager_secret.dockerhub.arn
}

output "repository_url" { value = aws_ecr_repository.cooker.repository_url }
output "repository_arn" { value = aws_ecr_repository.cooker.arn }
