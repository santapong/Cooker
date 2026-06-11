# skeleton — review before apply; prices/assumptions in docs/guides/DEPLOY-AWS-VERCEL.md
#
# Starter tier: ~$9.61/day us-east-1 (~$292/mo) / ~$11.00/day ap-southeast-1.
# Shape: 1 replica, memory WS/ticket/rate-limit backends, NO Redis, single-AZ
# RDS db.t4g.micro, 1 NAT, CloudWatch logs (7d). First-production footprint.

tier   = "starter"
region = "ap-southeast-1" # us-east-1 for the cheapest baseline

# REQUIRED — fill these in:
domain          = "REPLACE.example.com"
route53_zone_id = "REPLACE_ZONE_ID"

# Optional overrides (defaults in variables.tf already encode the starter
# shape via the per-tier maps; listed here for visibility):
# vpc_cidr                       = "10.0.0.0/16"
# eks_cluster_version            = "1.31"
# enable_ecr_interface_endpoints = false   # OPTIONAL hardening, not a saving
