output "loki_bucket_name" {
  value = aws_s3_bucket.loki.id
}

output "thanos_bucket_name" {
  value = aws_s3_bucket.thanos.id
}

output "prometheus_role_arn" {
  value = aws_iam_role.prometheus.arn
}

output "loki_role_arn" {
  value = aws_iam_role.loki.arn
}

output "sns_topic_arn" {
  value = aws_sns_topic.alerts.arn
}

output "kms_key_arn" {
  value = aws_kms_key.monitoring.arn
}
