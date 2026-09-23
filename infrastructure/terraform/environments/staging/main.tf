# Staging environment. It has its own state, network, database and secrets;
# nothing is shared with production.
#
#   terraform init -backend-config=backend.hcl
#   terraform plan -var-file=staging.tfvars
#
# State holds generated secrets (the token signing key and the SES SMTP
# credentials), so the state bucket must be private, encrypted and versioned.

terraform {
  required_version = ">= 1.9"

  backend "s3" {
    # Supplied by backend.hcl (see backend.hcl.example): bucket, region and lock table.
    key     = "staging/terraform.tfstate"
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
    tags = { Project = "mise", Environment = "staging", ManagedBy = "terraform" }
  }
}

# CloudFront certificates must be issued in us-east-1, whatever region the rest runs in.
provider "aws" {
  alias  = "us_east_1"
  region = "us-east-1"

  default_tags {
    tags = { Project = "mise", Environment = "staging", ManagedBy = "terraform" }
  }
}

variable "aws_region" {
  type    = string
  default = "us-east-1"
}

variable "domain" {
  description = "Domain for this environment, e.g. staging.mise.tt."
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

  environment              = "staging"
  domain                   = var.domain
  route53_zone_id          = var.route53_zone_id
  mail_from                = var.mail_from
  github_repository        = var.github_repository
  github_oidc_provider_arn = var.github_oidc_provider_arn
  alarm_email              = var.alarm_email
  admin_allowed_cidrs      = var.admin_allowed_cidrs
  vpc_cidr                 = "10.20.0.0/16"

  # Staging is deliberately small: one task per service, a single-zone database.
  db_multi_az              = false
  db_backup_retention_days = 7
  log_retention_days       = 14
}

output "github_environment_variables" {
  description = "Copy these into the GitHub \"staging\" environment (Settings -> Environments -> Variables)."
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
