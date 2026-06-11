# skeleton — review before apply; prices/assumptions in docs/guides/DEPLOY-AWS-VERCEL.md
#
# Data module: RDS Postgres, EFS (build contexts), ElastiCache Serverless
# Valkey (Team+ only), and Secrets Manager (escrow key + db password + oidc
# client secret).

variable "name_prefix" { type = string }
variable "vpc_id" { type = string }
variable "private_subnet_ids" { type = list(string) }
variable "db_instance_class" { type = string }
variable "db_multi_az" { type = bool }
variable "db_allocated_gb" { type = number }
variable "elasticache_enabled" { type = bool }
variable "allowed_source_security_group_id" {
  description = "EKS node security group allowed to reach RDS/ElastiCache/EFS."
  type        = string
}

# ---------------------------------------------------------------------------
# Security group: allow Postgres (5432), Redis/Valkey (6379), and NFS (2049)
# from the EKS node SG only. Nothing is internet-reachable.
# ---------------------------------------------------------------------------
resource "aws_security_group" "data" {
  name        = "${var.name_prefix}-data"
  description = "RDS + ElastiCache + EFS, reachable only from EKS nodes"
  vpc_id      = var.vpc_id

  ingress {
    description     = "Postgres from EKS nodes"
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [var.allowed_source_security_group_id]
  }
  ingress {
    description     = "Valkey/Redis from EKS nodes"
    from_port       = 6379
    to_port         = 6379
    protocol        = "tcp"
    security_groups = [var.allowed_source_security_group_id]
  }
  ingress {
    description     = "NFS (EFS) from EKS nodes"
    from_port       = 2049
    to_port         = 2049
    protocol        = "tcp"
    security_groups = [var.allowed_source_security_group_id]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
  tags = { Name = "${var.name_prefix}-data" }
}

# ---------------------------------------------------------------------------
# RDS Postgres. sslmode=require works with the default RDS cert (no CA bundle
# needed): lib/pq's `require` means TLS WITHOUT verification. For hardening,
# mount the RDS CA bundle and switch the chart's postgresql.sslMode to
# verify-full (documented in the guide). Multi-AZ doubles STORAGE billing too.
# ---------------------------------------------------------------------------
resource "aws_db_subnet_group" "this" {
  name       = "${var.name_prefix}-db"
  subnet_ids = var.private_subnet_ids
  tags       = { Name = "${var.name_prefix}-db" }
}

resource "random_password" "db" {
  length  = 32
  special = false # keep it URL-safe for DATABASE_URL
}

resource "aws_db_instance" "this" {
  identifier     = "${var.name_prefix}-pg"
  engine         = "postgres"
  instance_class = var.db_instance_class
  multi_az       = var.db_multi_az

  allocated_storage = var.db_allocated_gb
  storage_type      = "gp3"
  storage_encrypted = true

  db_name  = "cooker"
  username = "cooker"
  password = random_password.db.result

  db_subnet_group_name   = aws_db_subnet_group.this.name
  vpc_security_group_ids = [aws_security_group.data.id]

  # DR: automated snapshots. Tune the window per RPO needs (guide §7).
  backup_retention_period = 7
  skip_final_snapshot     = false
  final_snapshot_identifier = "${var.name_prefix}-pg-final"
  deletion_protection       = true

  # require TLS in transit; no public access.
  publicly_accessible = false
  tags                = { Name = "${var.name_prefix}-pg" }
}

# ---------------------------------------------------------------------------
# EFS for build contexts (ReadWriteMany shared between Cooker pod + Kaniko
# Jobs). Standard storage class, Elastic throughput, lifecycle -> IA after
# 14 days. A nightly GC CronJob (`find -mtime +3 -delete`) is the example in
# modules/registry / documented in the guide — NOTHING in the repo GCs
# contexts, so the operator must run it. EFS is NOT backed up (contexts are
# ephemeral — explicitly declined in the DR plan).
# ---------------------------------------------------------------------------
resource "aws_efs_file_system" "contexts" {
  creation_token = "${var.name_prefix}-contexts"
  encrypted      = true
  throughput_mode = "elastic"

  lifecycle_policy {
    transition_to_ia = "AFTER_14_DAYS"
  }
  tags = { Name = "${var.name_prefix}-contexts" }
}

resource "aws_efs_mount_target" "contexts" {
  count           = length(var.private_subnet_ids)
  file_system_id  = aws_efs_file_system.contexts.id
  subnet_id       = var.private_subnet_ids[count.index]
  security_groups = [aws_security_group.data.id]
}

# ---------------------------------------------------------------------------
# ElastiCache Serverless Valkey — Team+ only (count gated on tier).
# Cooker connects via rediss:// (TLS-only); go-redis ParseURL handles the
# rediss scheme. Pricing: storage $0.084/GB-hr (~$6.13/mo floor) + ECPU
# $0.0023/M. UNIT AMBIGUITY OPEN: if ECPU is billed per-1k not per-1M, the
# Team ECPU line jumps ~10x — verify before committing. A node-based
# cache.t4g.micro (~$0.31-0.38/day) is the steady-small-cache alternative.
# ---------------------------------------------------------------------------
resource "aws_elasticache_serverless_cache" "valkey" {
  count  = var.elasticache_enabled ? 1 : 0
  engine = "valkey"
  name   = "${var.name_prefix}-valkey"

  security_group_ids = [aws_security_group.data.id]
  subnet_ids         = var.private_subnet_ids

  # Cap storage/ECPU so a runaway can't bill unbounded. Tune per the guide.
  cache_usage_limits {
    data_storage {
      maximum = 5 # GB
      unit    = "GB"
    }
    ecpu_per_second {
      maximum = 5000
    }
  }
  tags = { Name = "${var.name_prefix}-valkey" }
}

# ---------------------------------------------------------------------------
# Secrets Manager. Three secrets:
#   - cooker-secret-key : the COOKER_SECRET_KEY escrow. LOSS BRICKS encrypted
#                         secret backups (database backend). Treat as the
#                         crown-jewel — DR plan keeps it here on purpose.
#   - db-password       : the RDS password (also fed into DATABASE_URL).
#   - oidc-client-secret: the IdP client secret (Cognito/Google).
# The cooker SA's Pod Identity role (modules/cluster) is scoped to read
# exactly these ARNs.
# ---------------------------------------------------------------------------
resource "random_password" "secret_key" {
  length  = 44 # base64-ish length for an AES-256 key seed; the app derives/uses it
  special = false
}

resource "aws_secretsmanager_secret" "secret_key" {
  name = "${var.name_prefix}/cooker-secret-key"
  tags = { Name = "${var.name_prefix}-secret-key" }
}

resource "aws_secretsmanager_secret_version" "secret_key" {
  secret_id     = aws_secretsmanager_secret.secret_key.id
  secret_string = random_password.secret_key.result
}

resource "aws_secretsmanager_secret" "db_password" {
  name = "${var.name_prefix}/db-password"
  tags = { Name = "${var.name_prefix}-db-password" }
}

resource "aws_secretsmanager_secret_version" "db_password" {
  secret_id     = aws_secretsmanager_secret.db_password.id
  secret_string = random_password.db.result
}

resource "aws_secretsmanager_secret" "oidc_client_secret" {
  name = "${var.name_prefix}/oidc-client-secret"
  tags = { Name = "${var.name_prefix}-oidc-client-secret" }
}
# NOTE: no secret_version for the OIDC client secret — it's populated out of
# band after the IdP (Cognito/Google) app client is created. Left as an empty
# secret so the ARN exists for the Pod Identity policy + a K8s ExternalSecret
# / manual sync. The Cognito IdP path is GATED on the aud-claim spike (OPEN-7).

output "db_endpoint" { value = aws_db_instance.this.address }
output "db_port" { value = aws_db_instance.this.port }
output "efs_id" { value = aws_efs_file_system.contexts.id }
output "valkey_endpoint" {
  value = var.elasticache_enabled ? aws_elasticache_serverless_cache.valkey[0].endpoint : null
}
# ARNs the cooker SA Pod Identity role is allowed to read.
output "secret_arns" {
  value = [
    aws_secretsmanager_secret.secret_key.arn,
    aws_secretsmanager_secret.db_password.arn,
    aws_secretsmanager_secret.oidc_client_secret.arn,
  ]
}
