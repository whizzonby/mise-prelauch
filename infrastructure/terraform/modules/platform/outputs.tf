# These map one-to-one onto the GitHub environment variables the deploy
# workflow reads (see docs/architecture/deployment.md).

output "aws_deploy_role_arn" {
  description = "GitHub variable AWS_DEPLOY_ROLE_ARN."
  value       = aws_iam_role.deploy.arn
}

output "ecr_registry" {
  description = "GitHub variable ECR_REGISTRY."
  value       = "${data.aws_caller_identity.current.account_id}.dkr.ecr.${data.aws_region.current.name}.amazonaws.com"
}

output "ecs_cluster" {
  description = "GitHub variable ECS_CLUSTER."
  value       = aws_ecs_cluster.main.name
}

output "name_prefix" {
  description = "GitHub variable NAME_PREFIX."
  value       = local.name
}

output "cloudfront_distribution_id" {
  description = "GitHub variable CLOUDFRONT_DISTRIBUTION_ID."
  value       = aws_cloudfront_distribution.site.id
}

output "public_site_url" {
  description = "GitHub variable PUBLIC_SITE_URL."
  value       = "https://${local.site_host}"
}

output "public_api_url" {
  description = "GitHub variable PUBLIC_API_URL."
  value       = "https://${local.api_host}"
}

output "admin_url" {
  value = "https://${local.admin_host}"
}

output "database_endpoint" {
  value = aws_db_instance.main.address
}
