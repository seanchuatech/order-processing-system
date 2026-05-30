provider "aws" {
  region  = "us-east-1"
  profile = "personal-sandbox"
}

data "aws_availability_zones" "available" {
  state = "available"
}

locals {
  cluster_name = "ops-sandbox"
}

module "vpc" {
  #checkov:skip=CKV_TF_1:Using public Terraform Registry modules with version constraints is standard practice
  source  = "terraform-aws-modules/vpc/aws"
  version = "~> 5.0"

  name = "ops-sandbox-vpc"
  cidr = "10.0.0.0/16"

  azs             = slice(data.aws_availability_zones.available.names, 0, 3)
  private_subnets = ["10.0.1.0/24", "10.0.2.0/24", "10.0.3.0/24"]
  public_subnets  = ["10.0.101.0/24", "10.0.102.0/24", "10.0.103.0/24"]

  enable_nat_gateway     = true
  single_nat_gateway     = true
  one_nat_gateway_per_az = false

  public_subnet_tags = {
    "kubernetes.io/role/elb"                      = "1"
    "kubernetes.io/cluster/${local.cluster_name}" = "shared"
  }

  private_subnet_tags = {
    "kubernetes.io/role/internal-elb"             = "1"
    "kubernetes.io/cluster/${local.cluster_name}" = "shared"
    "karpenter.sh/discovery"                      = local.cluster_name
  }
}

module "eks" {
  #checkov:skip=CKV_TF_1:Using public Terraform Registry modules with version constraints is standard practice
  source  = "terraform-aws-modules/eks/aws"
  version = "~> 20.0"

  cluster_name    = local.cluster_name
  cluster_version = "1.32"

  vpc_id     = module.vpc.vpc_id
  subnet_ids = module.vpc.private_subnets

  cluster_endpoint_public_access = true

  enable_cluster_creator_admin_permissions = true

  eks_managed_node_groups = {
    primary = {
      instance_types = ["t3.small"]
      min_size       = 1
      max_size       = 3
      desired_size   = 2

      # Needed for Karpenter discovery and security group management
      labels = {
        role = "primary"
      }
    }
  }

  # Cluster security group rules to allow Karpenter node-to-node communication
  node_security_group_tags = {
    "karpenter.sh/discovery" = local.cluster_name
  }

  cluster_addons = {
    coredns            = {}
    kube-proxy         = {}
    vpc-cni            = {}
    aws-ebs-csi-driver = {
      most_recent              = true
      service_account_role_arn = module.ebs_csi_irsa_role.iam_role_arn
    }
  }
}

module "ebs_csi_irsa_role" {
  #checkov:skip=CKV_TF_1:Using public Terraform Registry modules with version constraints is standard practice
  source  = "terraform-aws-modules/iam/aws//modules/iam-role-for-service-accounts-eks"
  version = "~> 5.0"

  role_name             = "${local.cluster_name}-ebs-csi"
  attach_ebs_csi_policy = true

  oidc_providers = {
    ex = {
      provider_arn               = module.eks.oidc_provider_arn
      namespace_service_accounts = ["kube-system:ebs-csi-controller-sa"]
    }
  }
}
