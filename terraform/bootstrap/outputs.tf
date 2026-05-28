output "cluster_name" {
  description = "The EKS cluster name"
  value       = module.eks.cluster_name
}

output "cluster_endpoint" {
  description = "The EKS cluster endpoint URL"
  value       = module.eks.cluster_endpoint
}

output "cluster_certificate_authority_data" {
  description = "Base64 encoded certificate data required to communicate with the cluster"
  value       = module.eks.cluster_certificate_authority_data
}

output "oidc_provider_arn" {
  description = "The ARN of the OIDC Provider for EKS"
  value       = module.eks.oidc_provider_arn
}

output "oidc_provider" {
  description = "The OIDC Provider URL"
  value       = module.eks.oidc_provider
}

output "vpc_id" {
  description = "The ID of the VPC"
  value       = module.vpc.vpc_id
}

output "private_subnets" {
  description = "List of private subnet IDs"
  value       = module.vpc.private_subnets
}

output "public_subnets" {
  description = "List of public subnet IDs"
  value       = module.vpc.public_subnets
}

output "node_security_group_id" {
  description = "The security group ID associated with EKS worker nodes"
  value       = module.eks.node_security_group_id
}
