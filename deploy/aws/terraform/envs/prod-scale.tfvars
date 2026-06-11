# skeleton — review before apply; prices/assumptions in docs/guides/DEPLOY-AWS-VERCEL.md
#
# Scale tier: ~$54.09/day us-east-1 (~$1,645/mo) / ~$64.71/day ap-southeast-1.
# Shape: 3+ replicas + HPA (autoscaling), larger Multi-AZ RDS db.r7g.large
# (200GB, storage billed x2), bigger Spot pool (m7g.2xlarge), WAF. Sustained
# load.
#
# RDS r7g.large Multi-AZ is RECOMMENDED over Aurora Serverless v2 at this tier:
# ASv2's 2-ACU floor billed 24/7 (~$7.09/day for 4GB no-standby) loses to a
# steady r7g for non-spiky load (Aurora is the right call only for spiky/idle
# workloads). See the guide §3 rationale + §4 cost note.

tier   = "scale"
region = "ap-southeast-1"

# REQUIRED — fill these in:
domain          = "REPLACE.example.com"
route53_zone_id = "REPLACE_ZONE_ID"

# Optionally widen the Spot pool's instance choices for bigger builds:
# spot_build_instance_types = ["m7g.2xlarge", "m7g.4xlarge"]
#
# Per-tier maps in variables.tf flip:
#   - nat_gateway_count_by_tier["scale"]   = 3
#   - db_instance_class_by_tier["scale"]   = db.r7g.large
#   - db_multi_az_by_tier["scale"]         = true
#   - elasticache_enabled_by_tier["scale"] = true
