provider "aws" {
  region  = "us-east-1"
  profile = "personal-sandbox"
}

data "aws_caller_identity" "current" {}

data "terraform_remote_state" "bootstrap" {
  backend = "local"

  config = {
    path = "../bootstrap/terraform.tfstate"
  }
}

data "aws_eks_cluster" "cluster" {
  name = data.terraform_remote_state.bootstrap.outputs.cluster_name
}

data "aws_eks_cluster_auth" "cluster" {
  name = data.terraform_remote_state.bootstrap.outputs.cluster_name
}

provider "kubernetes" {
  host                   = data.aws_eks_cluster.cluster.endpoint
  cluster_ca_certificate = base64decode(data.aws_eks_cluster.cluster.certificate_authority[0].data)
  token                  = data.aws_eks_cluster_auth.cluster.token
}

provider "helm" {
  kubernetes {
    host                   = data.aws_eks_cluster.cluster.endpoint
    cluster_ca_certificate = base64decode(data.aws_eks_cluster.cluster.certificate_authority[0].data)
    token                  = data.aws_eks_cluster_auth.cluster.token
  }
}

# ==========================================
# 1. Database (RDS PostgreSQL)
# ==========================================

resource "aws_security_group" "rds" {
  name        = "ops-sandbox-rds-sg"
  description = "Allow EKS nodes to connect to RDS PostgreSQL"
  vpc_id      = data.terraform_remote_state.bootstrap.outputs.vpc_id

  ingress {
    description     = "PostgreSQL from EKS nodes"
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [data.terraform_remote_state.bootstrap.outputs.node_security_group_id]
  }

  # Egress block removed (fixing CKV_AWS_382) because the database does not need to initiate outbound internet connections.

  tags = {
    Name = "ops-sandbox-rds-sg"
  }
}

resource "aws_db_subnet_group" "rds" {
  name       = "ops-sandbox-rds-subnets"
  subnet_ids = data.terraform_remote_state.bootstrap.outputs.private_subnets

  tags = {
    Name = "ops-sandbox-rds-subnets"
  }
}

resource "aws_db_parameter_group" "postgres" {
  name        = "ops-sandbox-postgres-params"
  family      = "postgres16"
  description = "Custom parameter group for ops-sandbox postgres"

  parameter {
    name  = "log_statement"
    value = "all"
  }

  parameter {
    name  = "log_min_duration_statement"
    value = "1"
  }

  parameter {
    name  = "rds.force_ssl"
    value = "1"
  }
}

resource "aws_iam_role" "rds_monitoring" {
  name        = "ops-sandbox-rds-monitoring-role"
  description = "Role for RDS enhanced monitoring logs"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "monitoring.rds.amazonaws.com"
        }
      }
    ]
  })
}

resource "aws_iam_role_policy_attachment" "rds_monitoring" {
  role       = aws_iam_role.rds_monitoring.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonRDSEnhancedMonitoringRole"
}

resource "random_password" "db_password" {
  length           = 16
  special          = false
}

# tfsec:ignore:aws-rds-enable-initial-db-password-rotation
resource "aws_db_instance" "postgres" {
  #checkov:skip=CKV_AWS_157:Multi-AZ disabled for sandbox environment cost savings
  #checkov:skip=CKV_AWS_293:Deletion protection disabled to allow smooth sandbox terraform teardowns
  identifier             = "ops-sandbox-db"
  allocated_storage      = 20
  storage_type           = "gp2"
  engine                 = "postgres"
  engine_version         = "16.4"
  instance_class         = "db.t3.micro"
  db_name                = "orders"
  username               = "postgres"
  password               = random_password.db_password.result
  db_subnet_group_name   = aws_db_subnet_group.rds.name
  vpc_security_group_ids = [aws_security_group.rds.id]
  parameter_group_name   = aws_db_parameter_group.postgres.name
  skip_final_snapshot    = true
  multi_az               = false

  storage_encrypted                   = true
  iam_database_authentication_enabled = true
  enabled_cloudwatch_logs_exports     = ["postgresql", "upgrade"]
  auto_minor_version_upgrade          = true
  copy_tags_to_snapshot               = true

  monitoring_interval = 60
  monitoring_role_arn = aws_iam_role.rds_monitoring.arn

  performance_insights_enabled    = true
  performance_insights_kms_key_id = aws_kms_key.ssm.arn

  tags = {
    Name = "ops-sandbox-db"
  }
}

# ==========================================
# 2. AWS SQS Queues & SNS Topics
# ==========================================

resource "aws_sns_topic" "payment_processed" {
  name              = "ops-sandbox-payment-processed-topic"
  kms_master_key_id = aws_kms_key.ssm.arn
}

resource "aws_sqs_queue" "order_pending_dlq" {
  name                      = "ops-sandbox-order-pending-dlq"
  message_retention_seconds = 1209600 # 14 days
  kms_master_key_id         = aws_kms_key.ssm.arn
}

resource "aws_sqs_queue" "order_pending" {
  name                      = "ops-sandbox-order-pending"
  kms_master_key_id         = aws_kms_key.ssm.arn
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.order_pending_dlq.arn
    maxReceiveCount     = 3
  })
}

resource "aws_sqs_queue" "payment_processed_notification_dlq" {
  name                      = "ops-sandbox-payment-processed-notification-dlq"
  message_retention_seconds = 1209600
  kms_master_key_id         = aws_kms_key.ssm.arn
}

resource "aws_sqs_queue" "payment_processed_notification" {
  name                      = "ops-sandbox-payment-processed-notification"
  kms_master_key_id         = aws_kms_key.ssm.arn
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.payment_processed_notification_dlq.arn
    maxReceiveCount     = 3
  })
}

resource "aws_sqs_queue" "payment_processed_inventory_dlq" {
  name                      = "ops-sandbox-payment-processed-inventory-dlq"
  message_retention_seconds = 1209600
  kms_master_key_id         = aws_kms_key.ssm.arn
}

resource "aws_sqs_queue" "payment_processed_inventory" {
  name                      = "ops-sandbox-payment-processed-inventory"
  kms_master_key_id         = aws_kms_key.ssm.arn
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.payment_processed_inventory_dlq.arn
    maxReceiveCount     = 3
  })
}

resource "aws_sqs_queue" "payment_processed_analytics_dlq" {
  name                      = "ops-sandbox-payment-processed-analytics-dlq"
  message_retention_seconds = 1209600
  kms_master_key_id         = aws_kms_key.ssm.arn
}

resource "aws_sqs_queue" "payment_processed_analytics" {
  name                      = "ops-sandbox-payment-processed-analytics"
  kms_master_key_id         = aws_kms_key.ssm.arn
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.payment_processed_analytics_dlq.arn
    maxReceiveCount     = 3
  })
}

# CloudWatch Metric Alarms for all DLQs
locals {
  dlqs = {
    "order-pending" = {
      queue_name = aws_sqs_queue.order_pending_dlq.name
      desc       = "Order Pending Dead Letter Queue"
    }
    "payment-notification" = {
      queue_name = aws_sqs_queue.payment_processed_notification_dlq.name
      desc       = "Payment Processed Notification Dead Letter Queue"
    }
    "payment-inventory" = {
      queue_name = aws_sqs_queue.payment_processed_inventory_dlq.name
      desc       = "Payment Processed Inventory Dead Letter Queue"
    }
    "payment-analytics" = {
      queue_name = aws_sqs_queue.payment_processed_analytics_dlq.name
      desc       = "Payment Processed Analytics Dead Letter Queue"
    }
  }
}

resource "aws_cloudwatch_metric_alarm" "dlq_alarms" {
  for_each            = local.dlqs
  alarm_name          = "ops-sandbox-${each.key}-dlq-non-empty"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "ApproximateNumberOfMessagesVisible"
  namespace           = "AWS/SQS"
  period              = 60
  statistic           = "Sum"
  threshold           = 0
  alarm_description   = "Triggers when there are messages in the ${each.value.desc}"
  treat_missing_data  = "notBreaching"

  dimensions = {
    QueueName = each.value.queue_name
  }
}

# SNS Subscriptions
resource "aws_sns_topic_subscription" "notification" {
  topic_arn = aws_sns_topic.payment_processed.arn
  protocol  = "sqs"
  endpoint  = aws_sqs_queue.payment_processed_notification.arn
}

resource "aws_sns_topic_subscription" "inventory" {
  topic_arn = aws_sns_topic.payment_processed.arn
  protocol  = "sqs"
  endpoint  = aws_sqs_queue.payment_processed_inventory.arn
}

resource "aws_sns_topic_subscription" "analytics" {
  topic_arn = aws_sns_topic.payment_processed.arn
  protocol  = "sqs"
  endpoint  = aws_sqs_queue.payment_processed_analytics.arn
}

# SQS Queue Policies to allow SNS publishing
data "aws_iam_policy_document" "sqs_sns_policy_notification" {
  statement {
    effect = "Allow"
    principals {
      type        = "Service"
      identifiers = ["sns.amazonaws.com"]
    }
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.payment_processed_notification.arn]
    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_sns_topic.payment_processed.arn]
    }
  }
}

resource "aws_sqs_queue_policy" "notification" {
  queue_url = aws_sqs_queue.payment_processed_notification.id
  policy    = data.aws_iam_policy_document.sqs_sns_policy_notification.json
}

data "aws_iam_policy_document" "sqs_sns_policy_inventory" {
  statement {
    effect = "Allow"
    principals {
      type        = "Service"
      identifiers = ["sns.amazonaws.com"]
    }
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.payment_processed_inventory.arn]
    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_sns_topic.payment_processed.arn]
    }
  }
}

resource "aws_sqs_queue_policy" "inventory" {
  queue_url = aws_sqs_queue.payment_processed_inventory.id
  policy    = data.aws_iam_policy_document.sqs_sns_policy_inventory.json
}

data "aws_iam_policy_document" "sqs_sns_policy_analytics" {
  statement {
    effect = "Allow"
    principals {
      type        = "Service"
      identifiers = ["sns.amazonaws.com"]
    }
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.payment_processed_analytics.arn]
    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_sns_topic.payment_processed.arn]
    }
  }
}

resource "aws_sqs_queue_policy" "analytics" {
  queue_url = aws_sqs_queue.payment_processed_analytics.id
  policy    = data.aws_iam_policy_document.sqs_sns_policy_analytics.json
}

# ==========================================
# 3. AWS SSM Parameters (Secrets & Configs)
# ==========================================

resource "aws_kms_key" "ssm" {
  description             = "KMS key for SSM parameters and database encryption"
  deletion_window_in_days = 7
  enable_key_rotation     = true
  policy = jsonencode({
    Version = "2012-10-17"
    Id      = "ssm-key-policy"
    Statement = [
      {
        Sid    = "Enable IAM User Permissions"
        Effect = "Allow"
        Principal = {
          AWS = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:root"
        }
        Action   = "kms:*"
        Resource = "*"
      }
    ]
  })

  tags = {
    Name = "ops-sandbox-ssm-key"
  }
}

resource "aws_kms_alias" "ssm" {
  name          = "alias/ops-sandbox-ssm"
  target_key_id = aws_kms_key.ssm.key_id
}

resource "aws_ssm_parameter" "db_url" {
  name        = "/ops-sandbox/order-service/DATABASE_URL"
  description = "Database URL for the order service"
  type        = "SecureString"
  value       = "postgresql://postgres:${random_password.db_password.result}@${aws_db_instance.postgres.endpoint}/orders?sslmode=require"
  key_id      = aws_kms_key.ssm.arn
}

resource "aws_ssm_parameter" "inventory_db_url" {
  name        = "/ops-sandbox/inventory-service/DATABASE_URL"
  description = "Database URL for the inventory service"
  type        = "SecureString"
  value       = "postgresql://postgres:${random_password.db_password.result}@${aws_db_instance.postgres.endpoint}/orders?sslmode=require"
  key_id      = aws_kms_key.ssm.arn
}

# ==========================================
# 4. IAM Roles for Service Accounts (IRSA)
# ==========================================

locals {
  oidc_provider_url = replace(data.terraform_remote_state.bootstrap.outputs.oidc_provider, "https://", "")
}

data "aws_iam_policy_document" "eso_assume_role" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]
    effect  = "Allow"

    condition {
      test     = "StringEquals"
      variable = "${local.oidc_provider_url}:sub"
      values   = ["system:serviceaccount:external-secrets:external-secrets"]
    }

    principals {
      identifiers = [data.terraform_remote_state.bootstrap.outputs.oidc_provider_arn]
      type        = "Federated"
    }
  }
}

resource "aws_iam_role" "eso" {
  name               = "ops-sandbox-eso-role"
  assume_role_policy = data.aws_iam_policy_document.eso_assume_role.json
}

data "aws_iam_policy_document" "eso_ssm_access" {
  statement {
    actions = [
      "ssm:GetParameter",
      "ssm:GetParameters",
      "ssm:GetParametersByPath"
    ]
    effect    = "Allow"
    resources = ["arn:aws:ssm:us-east-1:${data.aws_caller_identity.current.account_id}:parameter/ops-sandbox/*"]
  }

  statement {
    actions   = ["kms:Decrypt"]
    effect    = "Allow"
    resources = [aws_kms_key.ssm.arn]
  }
}

resource "aws_iam_policy" "eso" {
  name        = "ops-sandbox-eso-policy"
  description = "Allow ESO to read SSM parameters"
  policy      = data.aws_iam_policy_document.eso_ssm_access.json
}

resource "aws_iam_role_policy_attachment" "eso" {
  role       = aws_iam_role.eso.name
  policy_arn = aws_iam_policy.eso.arn
}

# ==========================================
# 4a. Microservices IRSA Roles
# ==========================================

# Order Service IRSA Role
data "aws_iam_policy_document" "order_service_assume_role" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]
    effect  = "Allow"

    condition {
      test     = "StringEquals"
      variable = "${local.oidc_provider_url}:sub"
      values   = [
        "system:serviceaccount:default:order-service",
        "system:serviceaccount:default:outbox-relay"
      ]
    }

    principals {
      identifiers = [data.terraform_remote_state.bootstrap.outputs.oidc_provider_arn]
      type        = "Federated"
    }
  }
}

resource "aws_iam_role" "order_service" {
  name               = "ops-sandbox-order-service-role"
  assume_role_policy = data.aws_iam_policy_document.order_service_assume_role.json
}

data "aws_iam_policy_document" "order_service_sqs_access" {
  statement {
    actions   = ["sqs:SendMessage"]
    effect    = "Allow"
    resources = [aws_sqs_queue.order_pending.arn]
  }

  statement {
    actions = [
      "kms:Decrypt",
      "kms:GenerateDataKey"
    ]
    effect    = "Allow"
    resources = [aws_kms_key.ssm.arn]
  }
}

resource "aws_iam_policy" "order_service" {
  name        = "ops-sandbox-order-service-policy"
  description = "Allow order-service to publish SQS messages"
  policy      = data.aws_iam_policy_document.order_service_sqs_access.json
}

resource "aws_iam_role_policy_attachment" "order_service" {
  role       = aws_iam_role.order_service.name
  policy_arn = aws_iam_policy.order_service.arn
}

# Notification Service IRSA Role
data "aws_iam_policy_document" "notification_service_assume_role" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]
    effect  = "Allow"

    condition {
      test     = "StringEquals"
      variable = "${local.oidc_provider_url}:sub"
      values   = ["system:serviceaccount:default:notification-service"]
    }

    principals {
      identifiers = [data.terraform_remote_state.bootstrap.outputs.oidc_provider_arn]
      type        = "Federated"
    }
  }
}

resource "aws_iam_role" "notification_service" {
  name               = "ops-sandbox-notification-service-role"
  assume_role_policy = data.aws_iam_policy_document.notification_service_assume_role.json
}

data "aws_iam_policy_document" "notification_service_sqs_access" {
  statement {
    actions = [
      "sqs:ReceiveMessage",
      "sqs:DeleteMessage",
      "sqs:GetQueueAttributes"
    ]
    effect    = "Allow"
    resources = [aws_sqs_queue.payment_processed_notification.arn]
  }

  statement {
    actions = [
      "kms:Decrypt"
    ]
    effect    = "Allow"
    resources = [aws_kms_key.ssm.arn]
  }
}

resource "aws_iam_policy" "notification_service" {
  name        = "ops-sandbox-notification-service-policy"
  description = "Allow notification-service to consume SQS messages"
  policy      = data.aws_iam_policy_document.notification_service_sqs_access.json
}

resource "aws_iam_role_policy_attachment" "notification_service" {
  role       = aws_iam_role.notification_service.name
  policy_arn = aws_iam_policy.notification_service.arn
}

# Payment Service IRSA Role
data "aws_iam_policy_document" "payment_service_assume_role" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]
    effect  = "Allow"

    condition {
      test     = "StringEquals"
      variable = "${local.oidc_provider_url}:sub"
      values   = ["system:serviceaccount:default:payment-service"]
    }

    principals {
      identifiers = [data.terraform_remote_state.bootstrap.outputs.oidc_provider_arn]
      type        = "Federated"
    }
  }
}

resource "aws_iam_role" "payment_service" {
  name               = "ops-sandbox-payment-service-role"
  assume_role_policy = data.aws_iam_policy_document.payment_service_assume_role.json
}

data "aws_iam_policy_document" "payment_service_access" {
  statement {
    actions = [
      "sqs:ReceiveMessage",
      "sqs:DeleteMessage",
      "sqs:GetQueueAttributes"
    ]
    effect    = "Allow"
    resources = [aws_sqs_queue.order_pending.arn]
  }

  statement {
    actions   = ["sns:Publish"]
    effect    = "Allow"
    resources = [aws_sns_topic.payment_processed.arn]
  }

  statement {
    actions = [
      "kms:Decrypt",
      "kms:GenerateDataKey"
    ]
    effect    = "Allow"
    resources = [aws_kms_key.ssm.arn]
  }
}

resource "aws_iam_policy" "payment_service" {
  name        = "ops-sandbox-payment-service-policy"
  description = "Allow payment-service to consume SQS and publish to SNS"
  policy      = data.aws_iam_policy_document.payment_service_access.json
}

resource "aws_iam_role_policy_attachment" "payment_service" {
  role       = aws_iam_role.payment_service.name
  policy_arn = aws_iam_policy.payment_service.arn
}

# Inventory Service IRSA Role
data "aws_iam_policy_document" "inventory_service_assume_role" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]
    effect  = "Allow"

    condition {
      test     = "StringEquals"
      variable = "${local.oidc_provider_url}:sub"
      values   = ["system:serviceaccount:default:inventory-service"]
    }

    principals {
      identifiers = [data.terraform_remote_state.bootstrap.outputs.oidc_provider_arn]
      type        = "Federated"
    }
  }
}

resource "aws_iam_role" "inventory_service" {
  name               = "ops-sandbox-inventory-service-role"
  assume_role_policy = data.aws_iam_policy_document.inventory_service_assume_role.json
}

data "aws_iam_policy_document" "inventory_service_access" {
  statement {
    actions = [
      "sqs:ReceiveMessage",
      "sqs:DeleteMessage",
      "sqs:GetQueueAttributes"
    ]
    effect    = "Allow"
    resources = [aws_sqs_queue.payment_processed_inventory.arn]
  }

  statement {
    actions = [
      "kms:Decrypt"
    ]
    effect    = "Allow"
    resources = [aws_kms_key.ssm.arn]
  }
}

resource "aws_iam_policy" "inventory_service" {
  name        = "ops-sandbox-inventory-service-policy"
  description = "Allow inventory-service to consume SQS"
  policy      = data.aws_iam_policy_document.inventory_service_access.json
}

resource "aws_iam_role_policy_attachment" "inventory_service" {
  role       = aws_iam_role.inventory_service.name
  policy_arn = aws_iam_policy.inventory_service.arn
}

# Analytics Service IRSA Role
data "aws_iam_policy_document" "analytics_service_assume_role" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]
    effect  = "Allow"

    condition {
      test     = "StringEquals"
      variable = "${local.oidc_provider_url}:sub"
      values   = ["system:serviceaccount:default:analytics-service"]
    }

    principals {
      identifiers = [data.terraform_remote_state.bootstrap.outputs.oidc_provider_arn]
      type        = "Federated"
    }
  }
}

resource "aws_iam_role" "analytics_service" {
  name               = "ops-sandbox-analytics-service-role"
  assume_role_policy = data.aws_iam_policy_document.analytics_service_assume_role.json
}

data "aws_iam_policy_document" "analytics_service_access" {
  statement {
    actions = [
      "sqs:ReceiveMessage",
      "sqs:DeleteMessage",
      "sqs:GetQueueAttributes"
    ]
    effect    = "Allow"
    resources = [aws_sqs_queue.payment_processed_analytics.arn]
  }

  statement {
    actions = [
      "kms:Decrypt"
    ]
    effect    = "Allow"
    resources = [aws_kms_key.ssm.arn]
  }
}

resource "aws_iam_policy" "analytics_service" {
  name        = "ops-sandbox-analytics-service-policy"
  description = "Allow analytics-service to consume SQS"
  policy      = data.aws_iam_policy_document.analytics_service_access.json
}

resource "aws_iam_role_policy_attachment" "analytics_service" {
  role       = aws_iam_role.analytics_service.name
  policy_arn = aws_iam_policy.analytics_service.arn
}

# ==========================================
# 5. Helm Operator Deployments
# ==========================================

resource "helm_release" "external_secrets" {
  name             = "external-secrets"
  repository       = "https://charts.external-secrets.io"
  chart            = "external-secrets"
  namespace        = "external-secrets"
  create_namespace = true

  set {
    name  = "installCRDs"
    value = "true"
  }

  set {
    name  = "serviceAccount.annotations.eks\\.amazonaws\\.com/role-arn"
    value = aws_iam_role.eso.arn
  }
}

resource "helm_release" "argocd" {
  name             = "argocd"
  repository       = "https://argoproj.github.io/argo-helm"
  chart            = "argo-cd"
  version          = "7.1.3"
  namespace        = "argocd"
  create_namespace = true

  set {
    name  = "configs.params.server.insecure"
    value = "true"
  }
}


# ==========================================
# 6. Karpenter Autoscaling
# ==========================================

module "karpenter" {
  #checkov:skip=CKV_TF_1:Using public Terraform Registry modules with version constraints is standard practice
  source  = "terraform-aws-modules/eks/aws//modules/karpenter"
  version = "~> 20.0"

  cluster_name = data.terraform_remote_state.bootstrap.outputs.cluster_name

  enable_irsa                     = true
  irsa_oidc_provider_arn          = data.terraform_remote_state.bootstrap.outputs.oidc_provider_arn
  irsa_namespace_service_accounts = ["karpenter:karpenter"]
  create_access_entry             = true

  node_iam_role_additional_policies = {
    AmazonSSMManagedInstanceCore = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
  }
}

resource "helm_release" "karpenter" {
  namespace        = "karpenter"
  create_namespace = true

  name       = "karpenter"
  repository = "oci://public.ecr.aws/karpenter"
  chart      = "karpenter"
  version    = "1.0.0"

  depends_on = [module.karpenter]

  set {
    name  = "settings.clusterName"
    value = data.terraform_remote_state.bootstrap.outputs.cluster_name
  }

  set {
    name  = "settings.clusterEndpoint"
    value = data.terraform_remote_state.bootstrap.outputs.cluster_endpoint
  }

  set {
    name  = "serviceAccount.annotations.eks\\.amazonaws\\.com/role-arn"
    value = module.karpenter.iam_role_arn
  }

  set {
    name  = "settings.interruptionQueue"
    value = module.karpenter.queue_name
  }
}

# ==========================================
# 7. Karpenter Provisioner Configs
# ==========================================

resource "kubernetes_manifest" "karpenter_node_class" {
  manifest = {
    apiVersion = "karpenter.k8s.aws/v1"
    kind       = "EC2NodeClass"
    metadata = {
      name = "default"
    }
    spec = {
      amiFamily = "AL2023"
      amiSelectorTerms = [
        {
          alias = "al2023@latest"
        }
      ]
      role      = module.karpenter.node_iam_role_name
      subnetSelectorTerms = [
        {
          tags = {
            "karpenter.sh/discovery" = data.terraform_remote_state.bootstrap.outputs.cluster_name
          }
        }
      ]
      securityGroupSelectorTerms = [
        {
          tags = {
            "karpenter.sh/discovery" = data.terraform_remote_state.bootstrap.outputs.cluster_name
          }
        }
      ]
    }
  }

  depends_on = [helm_release.karpenter]
}

resource "kubernetes_manifest" "karpenter_node_pool" {
  manifest = {
    apiVersion = "karpenter.sh/v1"
    kind       = "NodePool"
    metadata = {
      name = "default"
    }
    spec = {
      template = {
        spec = {
          requirements = [
            {
              key      = "kubernetes.io/arch"
              operator = "In"
              values   = ["amd64"]
            },
            {
              key      = "kubernetes.io/os"
              operator = "In"
              values   = ["linux"]
            },
            {
              key      = "karpenter.sh/capacity-type"
              operator = "In"
              values   = ["on-demand"]
            },
            {
              key      = "node.kubernetes.io/instance-type"
              operator = "In"
              values   = ["t3.small", "t3.micro"]
            }
          ]
          nodeClassRef = {
            group = "karpenter.k8s.aws"
            kind  = "EC2NodeClass"
            name  = "default"
          }
        }
      }
      limits = {
        cpu = 20
      }
      disruption = {
        consolidationPolicy = "WhenEmptyOrUnderutilized"
        consolidateAfter    = "1m"
      }
    }
  }

  depends_on = [kubernetes_manifest.karpenter_node_class]
}

