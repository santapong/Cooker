# skeleton — review before apply; prices/assumptions in docs/guides/DEPLOY-AWS-VERCEL.md
#
# All account-specific values are variables. Nothing here hardcodes an
# account id, ARN, password, or hostname. Tier-varying infra (NAT count,
# RDS class, Multi-AZ, ElastiCache presence) is driven by per-tier maps
# keyed on var.tier.

variable "region" {
  description = "AWS region to deploy into. ap-southeast-1 is the recommended deploy region (full EKS Auto Mode + ElastiCache Serverless parity confirmed); us-east-1 is the cheapest baseline. ap-southeast-7 (Bangkok) is NOT launch-ready as of 2026-06-11 (Auto Mode + ElastiCache Serverless availability unverified, opt-in region)."
  type        = string
  default     = "ap-southeast-1"
}

variable "tier" {
  description = "Deployment tier: starter | team | scale. Selects NAT count, RDS class, Multi-AZ, ElastiCache presence, and node sizing via the maps below."
  type        = string
  default     = "starter"

  validation {
    condition     = contains(["starter", "team", "scale"], var.tier)
    error_message = "tier must be one of: starter, team, scale."
  }
}

variable "domain" {
  description = "The app hostname (e.g. cooker.example.com). Must have a Route 53 hosted zone you control for ACM DNS validation and the ALB alias record."
  type        = string
}

variable "route53_zone_id" {
  description = "Route 53 hosted zone id for var.domain. Used for ACM cert DNS validation and the ALB alias record."
  type        = string
}

variable "vpc_cidr" {
  description = "CIDR block for the VPC. Must be large enough for 3 public + 3 private subnets plus headroom for EKS Auto Mode pod IPs (VPC CNI assigns pod IPs from the subnet ranges)."
  type        = string
  default     = "10.0.0.0/16"
}

variable "az_count" {
  description = "Number of Availability Zones to spread subnets across. 3 is standard for HA; the network module derives subnet CIDRs from this."
  type        = number
  default     = 3
}

# ---------------------------------------------------------------------------
# Per-tier maps. var.tier indexes each of these. This is how one root config
# produces three meaningfully different topologies without duplicating modules.
# ---------------------------------------------------------------------------

variable "nat_gateway_count_by_tier" {
  description = "NAT gateways per tier. Starter uses 1 (cheapest; loses egress if that AZ dies). Team/Scale use 3 (one per AZ; survives an AZ loss). NAT is a top-3 cost line — see the traps ledger in the guide."
  type        = map(number)
  default = {
    starter = 1
    team    = 3
    scale   = 3
  }
}

variable "db_instance_class_by_tier" {
  description = "RDS Postgres instance class per tier. Graviton (t4g/m7g/r7g) chosen for ~19% price/perf. Starter t4g.micro single-AZ; Team m7g.large Multi-AZ; Scale r7g.large Multi-AZ. RDS over Aurora Serverless v2 at Scale (the 2-ACU floor billed 24/7 is more expensive than a steady r7g for non-spiky load — see the guide)."
  type        = map(string)
  default = {
    starter = "db.t4g.micro"
    team    = "db.m7g.large"
    scale   = "db.r7g.large"
  }
}

variable "db_multi_az_by_tier" {
  description = "Whether RDS runs Multi-AZ per tier. NOTE: Multi-AZ doubles STORAGE billing too, not just compute (see traps ledger). Starter single-AZ; Team/Scale Multi-AZ."
  type        = map(bool)
  default = {
    starter = false
    team    = true
    scale   = true
  }
}

variable "db_allocated_storage_gb_by_tier" {
  description = "RDS allocated gp3 storage (GB) per tier. Multi-AZ tiers bill this x2. Range 20-200GB across tiers."
  type        = map(number)
  default = {
    starter = 20
    team    = 100
    scale   = 200
  }
}

variable "elasticache_enabled_by_tier" {
  description = "Whether to provision ElastiCache Serverless Valkey per tier. Starter = false (in-memory WS hub/ticket/rate-limit backends are legal at replicaCount=1). Team/Scale = true (multi-replica needs shared state via rediss://)."
  type        = map(bool)
  default = {
    starter = false
    team    = true
    scale   = true
  }
}

variable "node_instance_types" {
  description = "Graviton instance types EKS Auto Mode's general-purpose NodePool may pick from. The Cooker image is multi-arch so arm64 (m7g) is safe. Auto Mode manages the actual provisioning (Karpenter-based); these constrain the pool."
  type        = list(string)
  default     = ["m7g.large", "m7g.xlarge"]
}

variable "spot_build_instance_types" {
  description = "Instance types for the Spot builder NodePool (Team+). Kaniko build Jobs land here. Kaniko is NOT checkpointable — a Spot interruption fails the run (jobqueue retries exist). capacity-optimized allocation + OD fallback. NOTE: EKS Auto Mode's per-instance fee stays OD-rate even on Spot (~30% effective overhead)."
  type        = list(string)
  default     = ["m7g.xlarge", "m7g.2xlarge"]
}

variable "eks_cluster_version" {
  description = "EKS control-plane version (e.g. \"1.31\"). Pin explicitly. Auto Mode recycles nodes on a 21-day max lifetime (Bottlerocket). Watch the EKS standard-support window: extended support bills ~6x past it (see traps ledger). Plan an upgrade before month 14."
  type        = string
  default     = "1.31"
}

variable "enable_ecr_interface_endpoints" {
  description = "Whether to create VPC interface endpoints for ECR (api + dkr). DEFAULT false: per the R3 cost bounce, 2 ECR endpoints x 3 AZ bill ~$1.44/day — roughly 6x the NAT data they'd avoid at 5GB/day. Treat endpoints as OPTIONAL security/rate-limit hardening (or scope to 1 AZ), NOT a cost saving. The ECR pull-through cache (registry module) is the recommended rate-limit fix and stays on regardless."
  type        = bool
  default     = false
}

variable "tags" {
  description = "Base tags applied to all resources. The cost-allocation tag CostCenter=cooker-<tier> is merged in by main.tf so AWS Cost Explorer can split spend per tier. Add your own org tags here."
  type        = map(string)
  default = {
    Project   = "cooker"
    ManagedBy = "terraform"
  }
}
