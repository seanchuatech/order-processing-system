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

  egress {
    from_port        = 0
    to_port          = 0
    protocol         = "-1"
    cidr_blocks      = ["0.0.0.0/0"]
    ipv6_cidr_blocks = ["::/0"]
  }

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

resource "random_password" "db_password" {
  length           = 16
  special          = true
  override_special = "!#$%&*()-_=+[]{}<>:?"
}

resource "aws_db_instance" "postgres" {
  identifier           = "ops-sandbox-db"
  allocated_storage    = 20
  storage_type         = "gp2"
  engine               = "postgres"
  engine_version       = "16.4"
  instance_class       = "db.t3.micro"
  db_name              = "orders"
  username             = "postgres"
  password             = random_password.db_password.result
  db_subnet_group_name = aws_db_subnet_group.rds.name
  vpc_security_group_ids = [aws_security_group.rds.id]
  skip_final_snapshot  = true
  multi_az             = false

  tags = {
    Name = "ops-sandbox-db"
  }
}

# ==========================================
# 2. Apache Kafka (Self-Hosted in EKS)
# ==========================================

resource "helm_release" "kafka" {
  name             = "kafka"
  repository       = "https://charts.bitnami.com/bitnami"
  chart            = "kafka"
  version          = "29.3.7"
  namespace        = "default"
  create_namespace = false

  set {
    name  = "broker.replicaCount"
    value = "1"
  }

  set {
    name  = "controller.replicaCount"
    value = "1"
  }

  set {
    name  = "persistence.enabled"
    value = "false"
  }

  set {
    name  = "listeners.client.protocol"
    value = "PLAINTEXT"
  }
}

# ==========================================
# 3. AWS SSM Parameters (Secrets & Configs)
# ==========================================

resource "aws_ssm_parameter" "db_url" {
  name        = "/ops-sandbox/order-service/DATABASE_URL"
  description = "Database URL for the order service"
  type        = "SecureString"
  value       = "postgresql://postgres:${random_password.db_password.result}@${aws_db_instance.postgres.endpoint}/orders?sslmode=disable"
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

resource "helm_release" "flux2" {
  name             = "flux2"
  repository       = "https://fluxcd-community.github.io/helm-charts"
  chart            = "flux2"
  namespace        = "flux-system"
  create_namespace = true
}

# ==========================================
# 6. Karpenter Autoscaling
# ==========================================

module "karpenter" {
  source  = "terraform-aws-modules/eks/aws//modules/karpenter"
  version = "~> 20.0"

  cluster_name = data.terraform_remote_state.bootstrap.outputs.cluster_name

  enable_irsa                     = true
  irsa_oidc_provider_arn          = data.terraform_remote_state.bootstrap.outputs.oidc_provider_arn
  irsa_namespace_service_accounts = ["karpenter:karpenter"]

  node_iam_role_additional_policies = {
    AmazonSSMManagedInstanceCore = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
  }
}

resource "aws_eks_access_entry" "karpenter_nodes" {
  cluster_name  = data.terraform_remote_state.bootstrap.outputs.cluster_name
  principal_arn = module.karpenter.node_iam_role_arn
  type          = "EC2_LINUX"
}

resource "helm_release" "karpenter" {
  namespace        = "karpenter"
  create_namespace = true

  name       = "karpenter"
  repository = "oci://public.ecr.aws/karpenter"
  chart      = "karpenter"
  version    = "1.0.0"

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

