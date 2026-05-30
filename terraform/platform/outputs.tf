output "rds_endpoint" {
  description = "The database endpoint"
  value       = aws_db_instance.postgres.endpoint
}

output "sqs_queue_url" {
  description = "URL of the provisioned AWS SQS queue"
  value       = aws_sqs_queue.orders_created.id
}

output "sqs_queue_arn" {
  description = "ARN of the provisioned AWS SQS queue"
  value       = aws_sqs_queue.orders_created.arn
}

output "eso_iam_role_arn" {
  description = "The ARN of the IAM role assumed by External Secrets Operator"
  value       = aws_iam_role.eso.arn
}
