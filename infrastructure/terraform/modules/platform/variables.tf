variable "environment" {
  description = "staging or production."
  type        = string
  validation {
    condition     = contains(["staging", "production"], var.environment)
    error_message = "environment must be staging or production."
  }
}

variable "domain" {
  description = "Domain this environment is served from, e.g. mise.tt or staging.mise.tt. The site is at the domain itself, the API at api.<domain>, the admin at admin.<domain>."
  type        = string
}

variable "route53_zone_id" {
  description = "Hosted zone that contains var.domain."
  type        = string
}

variable "mail_from" {
  description = "From header for transactional email, e.g. \"Mise <hello@mise.tt>\". The address must be on var.domain."
  type        = string
}

variable "github_repository" {
  description = "owner/name of the GitHub repository allowed to deploy to this environment."
  type        = string
}

variable "github_oidc_provider_arn" {
  description = "ARN of the account's GitHub OIDC provider (token.actions.githubusercontent.com). One per AWS account; created outside this module."
  type        = string
}

variable "alarm_email" {
  description = "Address that receives alarm notifications."
  type        = string
}

variable "vpc_cidr" {
  description = "CIDR block for the environment's VPC. Staging and production must not overlap if they are ever peered."
  type        = string
}

variable "db_instance_class" {
  type    = string
  default = "db.t4g.micro"
}

variable "db_multi_az" {
  description = "Run a standby database in a second availability zone."
  type        = bool
  default     = false
}

variable "db_backup_retention_days" {
  type    = number
  default = 7
}

variable "desired_count" {
  description = "Number of tasks per service."
  type = object({
    api       = number
    worker    = number
    marketing = number
    admin     = number
  })
  default = { api = 1, worker = 1, marketing = 1, admin = 1 }
}

variable "email_verification_required" {
  description = "Require leads to confirm their email address (double opt-in)."
  type        = bool
  default     = true
}

variable "referral_milestones" {
  description = "Comma-separated converted-referral counts that trigger an email. Empty disables them."
  type        = string
  default     = ""
}

variable "admin_allowed_cidrs" {
  description = "If set, only these CIDR ranges can reach the admin app. Empty leaves it open to the internet (it still requires sign-in)."
  type        = list(string)
  default     = []
}

variable "log_retention_days" {
  type    = number
  default = 30
}

locals {
  name       = "mise-${var.environment}"
  site_host  = var.domain
  api_host   = "api.${var.domain}"
  admin_host = "admin.${var.domain}"

  # The origin CloudFront uses to reach the marketing service through the load balancer.
  origin_host = "origin.${var.domain}"

  is_production = var.environment == "production"

  tags = {
    Project     = "mise"
    Environment = var.environment
    ManagedBy   = "terraform"
  }
}
