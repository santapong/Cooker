# skeleton — review before apply; prices/assumptions in docs/guides/DEPLOY-AWS-VERCEL.md
#
# Root module: wires network -> cluster -> data/registry/observability.
# Single state. Helm is NOT deployed here (CI/operator runs helm upgrade
# --install with the per-tier overlay in deploy/aws/values/).

provider "aws" {
  region = var.region
  default_tags {
    # CostCenter is the cost-allocation tag; activate it in Billing ->
    # Cost allocation tags so Cost Explorer can break spend down per tier.
    tags = merge(var.tags, { CostCenter = "cooker-${var.tier}" })
  }
}

locals {
  # Resolve the per-tier values once so module blocks stay readable.
  nat_gateway_count   = var.nat_gateway_count_by_tier[var.tier]
  db_instance_class   = var.db_instance_class_by_tier[var.tier]
  db_multi_az         = var.db_multi_az_by_tier[var.tier]
  db_allocated_gb     = var.db_allocated_storage_gb_by_tier[var.tier]
  elasticache_enabled = var.elasticache_enabled_by_tier[var.tier]
  name_prefix         = "cooker-${var.tier}"
}

# --- Network: VPC, public/private subnets, NAT (1 or 3 by tier), endpoints ---
module "network" {
  source = "./modules/network"

  name_prefix                    = local.name_prefix
  vpc_cidr                       = var.vpc_cidr
  az_count                       = var.az_count
  nat_gateway_count              = local.nat_gateway_count
  enable_ecr_interface_endpoints = var.enable_ecr_interface_endpoints
  region                         = var.region
}

# --- Cluster: EKS Auto Mode, EFS CSI add-on, Pod Identity, NodePools ----------
module "cluster" {
  source = "./modules/cluster"

  name_prefix         = local.name_prefix
  cluster_version     = var.eks_cluster_version
  vpc_id              = module.network.vpc_id
  private_subnet_ids  = module.network.private_subnet_ids
  node_instance_types = var.node_instance_types

  # Pod Identity targets — ARNs come from the data + registry modules so the
  # cooker SA can read Secrets Manager + pull from ECR, and the kaniko SA can
  # push to ECR. serviceAccount.annotations in Helm stays {} (Pod Identity,
  # not IRSA).
  secrets_manager_arns = module.data.secret_arns
  ecr_repository_arn   = module.registry.repository_arn
}

# --- Data: RDS Postgres, ElastiCache Valkey (Team+), Secrets Manager ----------
module "data" {
  source = "./modules/data"

  name_prefix         = local.name_prefix
  vpc_id              = module.network.vpc_id
  private_subnet_ids  = module.network.private_subnet_ids
  db_instance_class   = local.db_instance_class
  db_multi_az         = local.db_multi_az
  db_allocated_gb     = local.db_allocated_gb
  elasticache_enabled = local.elasticache_enabled

  # Source security group allowed to reach RDS/ElastiCache: the EKS node SG.
  allowed_source_security_group_id = module.cluster.node_security_group_id
}

# --- Registry: ECR repo + lifecycle + pull-through cache rule -----------------
module "registry" {
  source = "./modules/registry"

  name_prefix = local.name_prefix
}

# --- Observability: CW log group, Container Insights toggle, Budget alarm ------
module "observability" {
  source = "./modules/observability"

  name_prefix              = local.name_prefix
  tier                     = var.tier
  container_insights       = true
  log_retention_days       = 7

  # Budget alarm at the tier's estimated monthly total x 1.3. The estimates
  # below are us-east-1 rough figures (2026-06-11); ap-southeast-1 runs higher
  # so the x1.3 headroom partly absorbs the regional premium. Re-verify and
  # adjust per the guide's cost tables before relying on the alarm.
  monthly_budget_usd = lookup({
    starter = 292
    team    = 977
    scale   = 1645
  }, var.tier, 292)
}
