# Production environment. It has its own state, network, database and secrets;
# nothing is shared with staging.
#
#   terraform init -backend-config=backend.hcl
#   terraform plan -var-file=production.tfvars
#
# State holds generated secrets (the token signing key and the SES SMTP
# credentials), so the state bucket must be private, encrypted and versioned.

terraform {
  required_version = ">= 1.9"

  backend "s3" {
    # Supplied by backend.hcl (see backend.hcl.example): bucket, region and lock table.
    key     = "production/terraform.tfstate"
    encrypt = true
  }

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.80"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }
}

provider "aws" {
  region = var.aws_region

  default_tags {
    tags = { Project = "mise", Environment = "production", ManagedBy = "terraform" }
  }
}

# CloudFront certificates must be issued in us-east-1, whatever region the rest runs in.
provider "aws" {
  alias  = "us_east_1"
  region = "us-east-1"

  default_tags {
    tags = { Project = "mise", Environment = "production", ManagedBy = "terraform" }
  }
}

variable "aws_region" {
  type    = string
  default = "us-east-1"
}

variable "domain" {
  description = "Domain for this environment, e.g. mise.tt."
  type        = string
}

variable "route53_zone_id" {
  type = string
}

variable "mail_from" {
  type = string
}

variable "github_repository" {
  type = string
}

variable "github_oidc_provider_arn" {
  type = string
}

variable "alarm_email" {
  type = string
}

variable "admin_allowed_cidrs" {
  type    = list(string)
  default = []
}

module "platform" {
  source = "../../modules/platform"

  providers = {
    aws           = aws
    aws.us_east_1 = aws.us_east_1
  }

  environment              = "production"
  domain                   = var.domain
  route53_zone_id          = var.route53_zone_id
  mail_from                = var.mail_from
  github_repository        = var.github_repository
  github_oidc_provider_arn = var.github_oidc_provider_arn
  alarm_email              = var.alarm_email
  admin_allowed_cidrs      = var.admin_allowed_cidrs
  vpc_cidr                 = "10.10.0.0/16"

  db_multi_az              = true
  db_backup_retention_days = 14
  log_retention_days       = 90
  desired_count            = { api = 2, worker = 1, marketing = 2, admin = 1 }
}

output "github_environment_variables" {
  description = "Copy these into the GitHub \"production\" environment (Settings -> Environments -> Variables)."
  value = {
    AWS_REGION                 = var.aws_region
    AWS_DEPLOY_ROLE_ARN        = module.platform.aws_deploy_role_arn
    ECR_REGISTRY               = module.platform.ecr_registry
    ECS_CLUSTER                = module.platform.ecs_cluster
    NAME_PREFIX                = module.platform.name_prefix
    CLOUDFRONT_DISTRIBUTION_ID = module.platform.cloudfront_distribution_id
    PUBLIC_SITE_URL            = module.platform.public_site_url
    PUBLIC_API_URL             = module.platform.public_api_url
  }
}

output "admin_url" {
  value = module.platform.admin_url
}
