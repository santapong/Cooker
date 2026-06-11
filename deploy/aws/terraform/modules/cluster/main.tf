# skeleton — review before apply; prices/assumptions in docs/guides/DEPLOY-AWS-VERCEL.md
#
# Cluster module: EKS Auto Mode + add-ons + Pod Identity associations.
#
# EKS Auto Mode manages compute (Karpenter-based), the AWS Load Balancer
# Controller, and the EBS CSI driver OFF-cluster (you don't run those
# controllers yourself). Nodes are Bottlerocket with a 21-day max lifetime
# (expect rolling node recycles). The Auto Mode management fee is ~12% of
# the on-demand instance price, charged PER INSTANCE and even on Spot.
#
# EFS CSI is NOT built into Auto Mode — it is installed here as an add-on so
# build contexts can live on an EFS volume (modules/data provisions the EFS;
# this wires the driver).

variable "name_prefix" { type = string }
variable "cluster_version" { type = string }
variable "vpc_id" { type = string }
variable "private_subnet_ids" { type = list(string) }
variable "node_instance_types" { type = list(string) }
variable "secrets_manager_arns" { type = list(string) }
variable "ecr_repository_arn" { type = string }

# terraform-aws-modules/eks >= v20.31 exposes cluster_compute_config for EKS
# Auto Mode (the general-purpose + system built-in NodePools). The exact
# minimum version that stabilized this argument is believed-OPEN — confirm
# against the module changelog at apply time.
module "eks" {
  source  = "terraform-aws-modules/eks/aws"
  version = ">= 20.31" # believed-OPEN: verify the cluster_compute_config arg shape

  cluster_name    = var.name_prefix
  cluster_version = var.cluster_version

  vpc_id     = var.vpc_id
  subnet_ids = var.private_subnet_ids

  # --- EKS Auto Mode ---------------------------------------------------------
  # Enables Auto Mode with the two built-in NodePools. Auto Mode runs the
  # ALB controller + EBS CSI for you; you do NOT manage node groups directly.
  cluster_compute_config = {
    enabled    = true
    node_pools = ["general-purpose", "system"]
  }

  # Cooker's operators reach the cluster via access entries (not the legacy
  # aws-auth configmap). Fill in your admin principal ARN at apply time.
  enable_cluster_creator_admin_permissions = true

  tags = { Name = var.name_prefix }
}

# --- EFS CSI driver add-on (NOT built into Auto Mode) -------------------------
# Installed so the Kaniko build-context PVC can bind an EFS-backed volume
# shared between the Cooker pod and Kaniko Jobs (ReadWriteMany). The EFS
# filesystem itself is created in modules/data.
resource "aws_eks_addon" "efs_csi" {
  cluster_name = module.eks.cluster_name
  addon_name   = "aws-efs-csi-driver"
  # addon_version intentionally omitted: defaults to the version EKS marks
  # default for the cluster version. Pin it explicitly in production.
}

# --- Pod Identity associations (NOT IRSA) -------------------------------------
# Pod Identity maps a (namespace, serviceaccount) pair to an IAM role using
# the SA NAME — so the Helm chart's serviceAccount.annotations stays {} (no
# eks.amazonaws.com/role-arn annotation needed, unlike IRSA).
#
# Two roles:
#   - cooker        SA -> read Secrets Manager + pull from ECR
#   - cooker-kaniko SA -> push to ECR (Kaniko Jobs use the built-in
#                         amazon-ecr-credential-helper; Pod Identity supplies
#                         the creds with zero in-pod config)

data "aws_iam_policy_document" "pod_identity_assume" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole", "sts:TagSession"]
    principals {
      type        = "Service"
      identifiers = ["pods.eks.amazonaws.com"]
    }
  }
}

# cooker SA role: Secrets Manager read + ECR pull
resource "aws_iam_role" "cooker" {
  name               = "${var.name_prefix}-cooker"
  assume_role_policy = data.aws_iam_policy_document.pod_identity_assume.json
}

data "aws_iam_policy_document" "cooker" {
  statement {
    sid     = "SecretsManagerRead"
    effect  = "Allow"
    actions = ["secretsmanager:GetSecretValue", "secretsmanager:DescribeSecret"]
    # Scoped to exactly the secrets this stack created (escrow key, db
    # password, oidc client secret) — not "*".
    resources = var.secrets_manager_arns
  }
  statement {
    sid    = "EcrPull"
    effect = "Allow"
    actions = [
      "ecr:GetDownloadUrlForLayer",
      "ecr:BatchGetImage",
      "ecr:BatchCheckLayerAvailability",
    ]
    resources = [var.ecr_repository_arn]
  }
  statement {
    sid       = "EcrAuthToken"
    effect    = "Allow"
    actions   = ["ecr:GetAuthorizationToken"]
    resources = ["*"] # GetAuthorizationToken is account-scoped; cannot be resource-narrowed
  }
}

resource "aws_iam_role_policy" "cooker" {
  name   = "${var.name_prefix}-cooker"
  role   = aws_iam_role.cooker.id
  policy = data.aws_iam_policy_document.cooker.json
}

resource "aws_eks_pod_identity_association" "cooker" {
  cluster_name    = module.eks.cluster_name
  namespace       = "cooker"
  service_account = "cooker"
  role_arn        = aws_iam_role.cooker.arn
}

# cooker-kaniko SA role: ECR push (+ pull/auth for cache layers)
resource "aws_iam_role" "kaniko" {
  name               = "${var.name_prefix}-kaniko"
  assume_role_policy = data.aws_iam_policy_document.pod_identity_assume.json
}

data "aws_iam_policy_document" "kaniko" {
  statement {
    sid    = "EcrPush"
    effect = "Allow"
    actions = [
      "ecr:GetDownloadUrlForLayer",
      "ecr:BatchGetImage",
      "ecr:BatchCheckLayerAvailability",
      "ecr:PutImage",
      "ecr:InitiateLayerUpload",
      "ecr:UploadLayerPart",
      "ecr:CompleteLayerUpload",
    ]
    resources = [var.ecr_repository_arn]
  }
  statement {
    sid       = "EcrAuthToken"
    effect    = "Allow"
    actions   = ["ecr:GetAuthorizationToken"]
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "kaniko" {
  name   = "${var.name_prefix}-kaniko"
  role   = aws_iam_role.kaniko.id
  policy = data.aws_iam_policy_document.kaniko.json
}

resource "aws_eks_pod_identity_association" "kaniko" {
  cluster_name    = module.eks.cluster_name
  namespace       = "cooker"
  service_account = "cooker-kaniko"
  role_arn        = aws_iam_role.kaniko.arn
}

output "cluster_name" { value = module.eks.cluster_name }
output "cluster_endpoint" { value = module.eks.cluster_endpoint }
# The EKS node security group — modules/data allows this SG to reach
# RDS/ElastiCache.
output "node_security_group_id" { value = module.eks.node_security_group_id }
