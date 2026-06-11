# skeleton — review before apply; prices/assumptions in docs/guides/DEPLOY-AWS-VERCEL.md
#
# Team tier: ~$32.11/day us-east-1 (~$977/mo) / ~$38.53/day ap-southeast-1.
# Shape: 2 replicas, Redis (ElastiCache Serverless Valkey, rediss://),
# Multi-AZ RDS db.m7g.large (100GB, storage billed x2), Spot builder pool,
# 3 NAT. HA + real concurrent users.

tier   = "team"
region = "ap-southeast-1"

# REQUIRED — fill these in:
domain          = "REPLACE.example.com"
route53_zone_id = "REPLACE_ZONE_ID"

# Team adds an isolated Spot build pool (apply the example NodePool out of band
# from modules/cluster/spot-nodepool.example.yaml). Per-tier maps in
# variables.tf already flip:
#   - nat_gateway_count_by_tier["team"]   = 3
#   - db_instance_class_by_tier["team"]   = db.m7g.large
#   - db_multi_az_by_tier["team"]         = true
#   - elasticache_enabled_by_tier["team"] = true
#
# enable_ecr_interface_endpoints stays false by default — per the cost bounce,
# 2 endpoints x 3 AZ (~$1.44/day) cost ~6x the NAT data they'd save. Flip to
# true ONLY for security/rate-limit hardening, or scope to 1 AZ.
# enable_ecr_interface_endpoints = false
