###############################################################################
# Monitoring Module - AWS resources for the observability stack
###############################################################################

terraform {
  required_version = ">= 1.5.0"
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 5.40" }
  }
}

locals {
  name_prefix = "${var.project}-${var.environment}"
}

# KMS key for encryption
resource "aws_kms_key" "monitoring" {
  description             = "KMS key for monitoring data encryption"
  deletion_window_in_days = 14
  enable_key_rotation     = true
  tags                    = { Name = "${local.name_prefix}-monitoring-kms" }
}

# S3 bucket for Loki log storage
resource "aws_s3_bucket" "loki" {
  bucket        = "${local.name_prefix}-loki-chunks"
  force_destroy = var.environment != "prod"
  tags          = { Name = "${local.name_prefix}-loki-chunks" }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "loki" {
  bucket = aws_s3_bucket.loki.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm     = "aws:kms"
      kms_master_key_id = aws_kms_key.monitoring.arn
    }
  }
}

resource "aws_s3_bucket_versioning" "loki" {
  bucket = aws_s3_bucket.loki.id
  versioning_configuration { status = "Enabled" }
}

# S3 bucket for Thanos long-term storage
resource "aws_s3_bucket" "thanos" {
  bucket        = "${local.name_prefix}-thanos-store"
  force_destroy = var.environment != "prod"
  tags          = { Name = "${local.name_prefix}-thanos-store" }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "thanos" {
  bucket = aws_s3_bucket.thanos.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm     = "aws:kms"
      kms_master_key_id = aws_kms_key.monitoring.arn
    }
  }
}

# SNS topic for alert routing
resource "aws_sns_topic" "alerts" {
  name              = "${local.name_prefix}-alerts"
  kms_master_key_id = aws_kms_key.monitoring.id
  tags              = { Name = "${local.name_prefix}-alerts" }
}

resource "aws_sns_topic_subscription" "email" {
  count     = var.alert_email != "" ? 1 : 0
  topic_arn = aws_sns_topic.alerts.arn
  protocol  = "email"
  endpoint  = var.alert_email
}

# IAM role for Prometheus (IRSA)
resource "aws_iam_role" "prometheus" {
  name = "${local.name_prefix}-prometheus"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Principal = { Federated = var.oidc_provider_arn }
      Action = "sts:AssumeRoleWithWebIdentity"
      Condition = {
        StringEquals = {
          "${var.oidc_provider_url}:sub" = "system:serviceaccount:monitoring:prometheus"
        }
      }
    }]
  })
}

resource "aws_iam_role_policy" "prometheus_thanos" {
  name = "thanos-s3-access"
  role = aws_iam_role.prometheus.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:ListBucket"]
      Resource = [aws_s3_bucket.thanos.arn, "${aws_s3_bucket.thanos.arn}/*"]
    }]
  })
}

# IAM role for Loki (IRSA)
resource "aws_iam_role" "loki" {
  name = "${local.name_prefix}-loki"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Principal = { Federated = var.oidc_provider_arn }
      Action = "sts:AssumeRoleWithWebIdentity"
      Condition = {
        StringEquals = {
          "${var.oidc_provider_url}:sub" = "system:serviceaccount:monitoring:loki"
        }
      }
    }]
  })
}

resource "aws_iam_role_policy" "loki_s3" {
  name = "loki-s3-access"
  role = aws_iam_role.loki.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:ListBucket"]
      Resource = [aws_s3_bucket.loki.arn, "${aws_s3_bucket.loki.arn}/*"]
    }, {
      Effect   = "Allow"
      Action   = ["kms:Decrypt", "kms:GenerateDataKey"]
      Resource = [aws_kms_key.monitoring.arn]
    }]
  })
}

# CloudWatch log group
resource "aws_cloudwatch_log_group" "monitoring" {
  name              = "/sre-platform/${var.environment}/monitoring"
  retention_in_days = var.environment == "prod" ? 90 : 14
  tags              = { Name = "${local.name_prefix}-monitoring-logs" }
}
