# skeleton — review before apply; prices/assumptions in docs/guides/DEPLOY-AWS-VERCEL.md
#
# Observability module: a CloudWatch log group (7-day retention), a
# Container-Insights toggle marker, an AWS Budget monthly alarm (tier total
# x 1.3), and a cost anomaly detector.
#
# COST TRAP (see traps ledger): CloudWatch INGESTION is $0.50/GB — that's the
# trap, NOT retention. A debug build dumping 20GB/day of Kaniko layer logs is
# $10/day. The Helm overlay pairs a Fluent Bit filter that DROPS Kaniko layer
# log lines before ingestion. Team+ may move metrics to AMP + Managed Grafana
# (~a wash on metrics) — it only wins if it also cuts log ingestion.

variable "name_prefix" { type = string }
variable "tier" { type = string }
variable "container_insights" { type = bool }
variable "log_retention_days" { type = number }
variable "monthly_budget_usd" {
  description = "Monthly budget threshold in USD. main.tf passes the tier's estimated total; the alarm fires at this value (the caller already applied any x1.3 headroom or applies it here)."
  type        = number
}

# --- CloudWatch log group -----------------------------------------------------
# 7-day retention. Container Insights + Fluent Bit (installed as an EKS add-on
# / DaemonSet out of band) ship pod logs here. Keep retention short; the cost
# lever is dropping debug log VOLUME (ingestion), not extending/shrinking
# retention.
resource "aws_cloudwatch_log_group" "cooker" {
  name              = "/cooker/${var.name_prefix}"
  retention_in_days = var.log_retention_days
  tags              = { Name = "${var.name_prefix}-logs" }
}

# Container Insights for EKS is enabled via the CloudWatch Observability
# EKS add-on (installed in a real apply). This marker output records the
# operator's intent so the toggle is visible in state; the add-on install
# itself is left to modules/cluster or an out-of-band step to avoid coupling.
output "container_insights_enabled" { value = var.container_insights }

# --- AWS Budget: monthly cost alarm ------------------------------------------
# Fires notifications at 80% (forecast) and 100% (actual) of the threshold.
# Wire notification_subscriber addresses at apply time.
resource "aws_budgets_budget" "monthly" {
  name         = "${var.name_prefix}-monthly"
  budget_type  = "COST"
  limit_amount = format("%.0f", var.monthly_budget_usd * 1.3)
  limit_unit   = "USD"
  time_unit    = "MONTHLY"

  # Filter to this tier's cost-allocation tag so the budget tracks only this
  # deployment's spend.
  cost_filter {
    name   = "TagKeyValue"
    values = ["user:CostCenter$cooker-${var.tier}"]
  }

  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 80
    threshold_type             = "PERCENTAGE"
    notification_type          = "FORECASTED"
    subscriber_email_addresses = [] # REPLACE: add operator email(s)
  }
  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 100
    threshold_type             = "PERCENTAGE"
    notification_type          = "ACTUAL"
    subscriber_email_addresses = [] # REPLACE: add operator email(s)
  }
}

# --- Cost anomaly detection ---------------------------------------------------
# Catches unexpected spend spikes (e.g. a runaway build queue scaling the Spot
# pool, or a debug-log flood) faster than the monthly budget.
resource "aws_ce_anomaly_monitor" "cooker" {
  name              = "${var.name_prefix}-anomaly"
  monitor_type      = "DIMENSIONAL"
  monitor_dimension = "SERVICE"
}

resource "aws_ce_anomaly_subscription" "cooker" {
  name      = "${var.name_prefix}-anomaly-sub"
  frequency = "DAILY"
  monitor_arn_list = [aws_ce_anomaly_monitor.cooker.arn]
  threshold_expression {
    dimension {
      key           = "ANOMALY_TOTAL_IMPACT_ABSOLUTE"
      values        = ["50"] # USD impact before alerting; tune per tier
      match_options = ["GREATER_THAN_OR_EQUAL"]
    }
  }
  subscriber {
    type    = "EMAIL"
    address = "REPLACE@example.com"
  }
}

output "log_group_name" { value = aws_cloudwatch_log_group.cooker.name }
