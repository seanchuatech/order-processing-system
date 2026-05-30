output "rds_endpoint" {
  description = "The database endpoint"
  value       = aws_db_instance.postgres.endpoint
}

output "kafka_endpoint" {
  description = "The self-hosted Kafka service endpoint inside EKS"
  value       = "kafka.default.svc.cluster.local:9092"
}

output "eso_iam_role_arn" {
  description = "The ARN of the IAM role assumed by External Secrets Operator"
  value       = aws_iam_role.eso.arn
}
