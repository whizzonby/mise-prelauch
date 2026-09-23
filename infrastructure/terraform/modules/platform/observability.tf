# Metrics are derived from the structured logs the services already write, so
# the application needs no metrics agent. Alarms go to one SNS topic.

resource "aws_sns_topic" "alarms" {
  name = "${local.name}-alarms"
}

resource "aws_sns_topic_subscription" "alarm_email" {
  topic_arn = aws_sns_topic.alarms.arn
  protocol  = "email"
  endpoint  = var.alarm_email
}

locals {
  # metric name => { log group, filter pattern over the JSON log line }
  log_metrics = {
    LeadsCreated      = { group = "api", pattern = "{ $.event = \"lead.created\" }" }
    LeadsVerified     = { group = "api", pattern = "{ $.event = \"lead.verified\" }" }
    LeadsUnsubscribed = { group = "api", pattern = "{ $.event = \"lead.unsubscribed\" }" }
    AdminLoginsFailed = { group = "api", pattern = "{ $.event = \"admin.login_failed\" }" }
    ApiErrors         = { group = "api", pattern = "{ $.level = \"ERROR\" }" }
    EmailsSent        = { group = "worker", pattern = "{ $.event = \"email.sent\" }" }
    JobsFailed        = { group = "worker", pattern = "{ $.msg = \"job failed permanently\" }" }
  }
}

resource "aws_cloudwatch_log_metric_filter" "metric" {
  for_each       = local.log_metrics
  name           = each.key
  log_group_name = aws_cloudwatch_log_group.service[each.value.group].name
  pattern        = each.value.pattern

  metric_transformation {
    name          = each.key
    namespace     = "Mise/${var.environment}"
    value         = "1"
    default_value = "0"
  }
}

resource "aws_cloudwatch_metric_alarm" "api_errors" {
  alarm_name          = "${local.name}-api-errors"
  alarm_description   = "The API logged more than 5 errors in 5 minutes."
  namespace           = "Mise/${var.environment}"
  metric_name         = "ApiErrors"
  statistic           = "Sum"
  period              = 300
  evaluation_periods  = 1
  threshold           = 5
  comparison_operator = "GreaterThanThreshold"
  treat_missing_data  = "notBreaching"
  alarm_actions       = [aws_sns_topic.alarms.arn]
  ok_actions          = [aws_sns_topic.alarms.arn]
}

resource "aws_cloudwatch_metric_alarm" "jobs_failed" {
  alarm_name          = "${local.name}-jobs-failed"
  alarm_description   = "A background job (an email) failed after all retries."
  namespace           = "Mise/${var.environment}"
  metric_name         = "JobsFailed"
  statistic           = "Sum"
  period              = 300
  evaluation_periods  = 1
  threshold           = 0
  comparison_operator = "GreaterThanThreshold"
  treat_missing_data  = "notBreaching"
  alarm_actions       = [aws_sns_topic.alarms.arn]
}

resource "aws_cloudwatch_metric_alarm" "admin_logins_failed" {
  alarm_name          = "${local.name}-admin-logins-failed"
  alarm_description   = "More than 10 failed admin sign-ins in 15 minutes."
  namespace           = "Mise/${var.environment}"
  metric_name         = "AdminLoginsFailed"
  statistic           = "Sum"
  period              = 900
  evaluation_periods  = 1
  threshold           = 10
  comparison_operator = "GreaterThanThreshold"
  treat_missing_data  = "notBreaching"
  alarm_actions       = [aws_sns_topic.alarms.arn]
}

resource "aws_cloudwatch_metric_alarm" "alb_5xx" {
  alarm_name          = "${local.name}-alb-5xx"
  alarm_description   = "The load balancer's targets returned more than 10 server errors in 5 minutes."
  namespace           = "AWS/ApplicationELB"
  metric_name         = "HTTPCode_Target_5XX_Count"
  dimensions          = { LoadBalancer = aws_lb.main.arn_suffix }
  statistic           = "Sum"
  period              = 300
  evaluation_periods  = 1
  threshold           = 10
  comparison_operator = "GreaterThanThreshold"
  treat_missing_data  = "notBreaching"
  alarm_actions       = [aws_sns_topic.alarms.arn]
}

resource "aws_cloudwatch_metric_alarm" "unhealthy_targets" {
  for_each            = aws_lb_target_group.service
  alarm_name          = "${local.name}-${each.key}-unhealthy"
  alarm_description   = "No healthy ${each.key} task is behind the load balancer."
  namespace           = "AWS/ApplicationELB"
  metric_name         = "HealthyHostCount"
  dimensions          = { LoadBalancer = aws_lb.main.arn_suffix, TargetGroup = each.value.arn_suffix }
  statistic           = "Minimum"
  period              = 60
  evaluation_periods  = 3
  threshold           = 1
  comparison_operator = "LessThanThreshold"
  treat_missing_data  = "breaching"
  alarm_actions       = [aws_sns_topic.alarms.arn]
  ok_actions          = [aws_sns_topic.alarms.arn]
}

resource "aws_cloudwatch_metric_alarm" "database_storage" {
  alarm_name          = "${local.name}-db-storage-low"
  alarm_description   = "The database has less than 4 GB of free storage."
  namespace           = "AWS/RDS"
  metric_name         = "FreeStorageSpace"
  dimensions          = { DBInstanceIdentifier = aws_db_instance.main.identifier }
  statistic           = "Minimum"
  period              = 300
  evaluation_periods  = 2
  threshold           = 4 * 1024 * 1024 * 1024
  comparison_operator = "LessThanThreshold"
  alarm_actions       = [aws_sns_topic.alarms.arn]
}

resource "aws_cloudwatch_metric_alarm" "database_cpu" {
  alarm_name          = "${local.name}-db-cpu-high"
  alarm_description   = "Database CPU above 80% for 15 minutes."
  namespace           = "AWS/RDS"
  metric_name         = "CPUUtilization"
  dimensions          = { DBInstanceIdentifier = aws_db_instance.main.identifier }
  statistic           = "Average"
  period              = 300
  evaluation_periods  = 3
  threshold           = 80
  comparison_operator = "GreaterThanThreshold"
  alarm_actions       = [aws_sns_topic.alarms.arn]
}
