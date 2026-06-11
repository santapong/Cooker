# skeleton — review before apply; prices/assumptions in docs/guides/DEPLOY-AWS-VERCEL.md
#
# Network module: VPC across az_count AZs, public + private subnets, NAT
# gateway(s) (1 for starter, 3 for team/scale), an S3 GATEWAY endpoint
# (free; keeps EFS/ECR layer pulls off NAT where possible), and OPTIONAL
# ECR interface endpoints (default off per the cost bounce).
#
# This is illustrative skeleton wiring. In a real apply, prefer the
# community VPC module (terraform-aws-modules/vpc/aws) which handles subnet
# math, route tables, and NAT placement robustly. The resources below are
# spelled out so the topology is reviewable, not because hand-rolling is
# recommended.

variable "name_prefix" { type = string }
variable "vpc_cidr" { type = string }
variable "az_count" { type = number }
variable "nat_gateway_count" { type = number }
variable "enable_ecr_interface_endpoints" { type = bool }
variable "region" { type = string }

data "aws_availability_zones" "available" {
  state = "available"
}

locals {
  azs = slice(data.aws_availability_zones.available.names, 0, var.az_count)
  # Derive /20 public and /20 private subnet CIDRs from the VPC CIDR.
  # cidrsubnet(vpc_cidr, 4, n) carves 16 /20s out of a /16.
  public_subnet_cidrs  = [for i in range(var.az_count) : cidrsubnet(var.vpc_cidr, 4, i)]
  private_subnet_cidrs = [for i in range(var.az_count) : cidrsubnet(var.vpc_cidr, 4, i + 8)]
}

resource "aws_vpc" "this" {
  cidr_block           = var.vpc_cidr
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = "${var.name_prefix}-vpc" }
}

resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id
  tags   = { Name = "${var.name_prefix}-igw" }
}

resource "aws_subnet" "public" {
  count                   = var.az_count
  vpc_id                  = aws_vpc.this.id
  cidr_block              = local.public_subnet_cidrs[count.index]
  availability_zone       = local.azs[count.index]
  map_public_ip_on_launch = true
  tags = {
    Name = "${var.name_prefix}-public-${local.azs[count.index]}"
    # ELB tag lets the AWS Load Balancer Controller place internet-facing
    # ALBs in these subnets.
    "kubernetes.io/role/elb" = "1"
  }
}

resource "aws_subnet" "private" {
  count             = var.az_count
  vpc_id            = aws_vpc.this.id
  cidr_block        = local.private_subnet_cidrs[count.index]
  availability_zone = local.azs[count.index]
  tags = {
    Name = "${var.name_prefix}-private-${local.azs[count.index]}"
    # internal-elb tag for internal LBs; nodes + pods live here.
    "kubernetes.io/role/internal-elb" = "1"
  }
}

# --- NAT: 1 gateway (starter) or one per AZ (team/scale) ----------------------
# NAT is a top-3 cost line. Single-NAT saves ~$2.16/day at Team but loses
# egress if that one AZ fails (private subnets in other AZs route through it).
resource "aws_eip" "nat" {
  count  = var.nat_gateway_count
  domain = "vpc"
  tags   = { Name = "${var.name_prefix}-nat-${count.index}" }
}

resource "aws_nat_gateway" "this" {
  count         = var.nat_gateway_count
  allocation_id = aws_eip.nat[count.index].id
  # Place each NAT in a public subnet. When count < az_count (starter=1),
  # all private subnets route through the single NAT in subnet 0.
  subnet_id  = aws_subnet.public[count.index].id
  tags       = { Name = "${var.name_prefix}-nat-${count.index}" }
  depends_on = [aws_internet_gateway.this]
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id
  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }
  tags = { Name = "${var.name_prefix}-public-rt" }
}

resource "aws_route_table_association" "public" {
  count          = var.az_count
  subnet_id      = aws_subnet.public[count.index].id
  route_table_id = aws_route_table.public.id
}

resource "aws_route_table" "private" {
  count  = var.az_count
  vpc_id = aws_vpc.this.id
  route {
    cidr_block = "0.0.0.0/0"
    # If nat_gateway_count == 1, every private RT points at NAT 0.
    # Otherwise each private subnet uses the NAT in its own AZ.
    nat_gateway_id = var.nat_gateway_count == 1 ? aws_nat_gateway.this[0].id : aws_nat_gateway.this[count.index].id
  }
  tags = { Name = "${var.name_prefix}-private-rt-${count.index}" }
}

resource "aws_route_table_association" "private" {
  count          = var.az_count
  subnet_id      = aws_subnet.private[count.index].id
  route_table_id = aws_route_table.private[count.index].id
}

# --- S3 gateway endpoint (free) -----------------------------------------------
# Gateway endpoints cost nothing and keep S3 traffic (ECR layer blobs are
# stored in S3) off the NAT path. Always worth having.
resource "aws_vpc_endpoint" "s3" {
  vpc_id            = aws_vpc.this.id
  service_name      = "com.amazonaws.${var.region}.s3"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = aws_route_table.private[*].id
  tags              = { Name = "${var.name_prefix}-s3-gw" }
}

# --- OPTIONAL ECR interface endpoints (default OFF) ---------------------------
# Per the R3 cost bounce: 2 ECR interface endpoints x 3 AZ bill ~$1.44/day,
# roughly 6x the NAT data they'd save at 5GB/day. These are OPTIONAL
# security/rate-limit hardening (or scope to 1 AZ), NOT a cost saving. The
# ECR pull-through cache (registry module) is the recommended rate-limit fix.
resource "aws_security_group" "vpc_endpoints" {
  count       = var.enable_ecr_interface_endpoints ? 1 : 0
  name        = "${var.name_prefix}-vpce"
  description = "Allow HTTPS from the VPC to interface endpoints"
  vpc_id      = aws_vpc.this.id
  ingress {
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = [var.vpc_cidr]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
  tags = { Name = "${var.name_prefix}-vpce" }
}

resource "aws_vpc_endpoint" "ecr_api" {
  count               = var.enable_ecr_interface_endpoints ? 1 : 0
  vpc_id              = aws_vpc.this.id
  service_name        = "com.amazonaws.${var.region}.ecr.api"
  vpc_endpoint_type   = "Interface"
  subnet_ids          = aws_subnet.private[*].id
  security_group_ids  = [aws_security_group.vpc_endpoints[0].id]
  private_dns_enabled = true
  tags                = { Name = "${var.name_prefix}-ecr-api" }
}

resource "aws_vpc_endpoint" "ecr_dkr" {
  count               = var.enable_ecr_interface_endpoints ? 1 : 0
  vpc_id              = aws_vpc.this.id
  service_name        = "com.amazonaws.${var.region}.ecr.dkr"
  vpc_endpoint_type   = "Interface"
  subnet_ids          = aws_subnet.private[*].id
  security_group_ids  = [aws_security_group.vpc_endpoints[0].id]
  private_dns_enabled = true
  tags                = { Name = "${var.name_prefix}-ecr-dkr" }
}

output "vpc_id" { value = aws_vpc.this.id }
output "private_subnet_ids" { value = aws_subnet.private[*].id }
output "public_subnet_ids" { value = aws_subnet.public[*].id }
