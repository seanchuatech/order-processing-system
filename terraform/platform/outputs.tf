output "rds_endpoint" {
  description = "The database endpoint"
  value       = aws_db_instance.postgres.endpoint
}

output "msk_bootstrap_brokers_plaintext" {
  description = "Plaintext connection host:port pairs for Kafka brokers"
  value       = aws_msk_cluster.kafka.bootstrap_brokers
}

output "msk_bootstrap_brokers_tls" {
  description = "TLS connection host:port pairs for Kafka brokers"
  value       = aws_msk_cluster.kafka.bootstrap_brokers_tls
}

output "eso_iam_role_arn" {
  description = "The ARN of the IAM role assumed by External Secrets Operator"
  value       = aws_iam_role.eso.arn
}
