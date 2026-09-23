# ADR-006: AWS deployment on ECS Fargate

Status: accepted

## Decision

Run on AWS with Terraform: ECS Fargate for the four services, RDS PostgreSQL, SES for
email, an Application Load Balancer with WAF, CloudFront in front of the marketing site,
Secrets Manager, CloudWatch. No Kubernetes. Detail in `docs/architecture/deployment.md`.

## Cost-conscious choices

- **No NAT gateway.** Tasks run in public subnets with public IPs, protected by a security
  group that accepts traffic only from the load balancer. Tasks still reach ECR, Secrets
  Manager, SES and CloudWatch. A NAT gateway would add roughly US$32 a month per zone to
  each environment. The database is in private subnets with no internet route. If a
  compliance requirement later demands private tasks, add NAT or VPC endpoints; nothing
  else changes.
- **One load balancer** for all three hosts, routed by host header.
- **The smallest sizes**: 0.25 vCPU tasks, `db.t4g.micro`, single-AZ database in staging.
- **No Redis, no SQS** (ADR-003, ADR-008).

## Why Fargate rather than the alternatives

- Lambda: the Go API would fit, but two Next.js servers and a long-polling worker would
  not fit as neatly, and one runtime model is simpler to operate than two.
- App Runner or Amplify: less control over networking and secrets, and a harder path to
  the private services the full platform will need.
- EC2: servers to patch.

## Deployments

Only from GitHub Actions, through OIDC, with a role scoped to one environment.
Production requires an approval. No long-lived AWS credentials exist.

## Consequences

- The first Terraform apply creates services before any image exists; they are unhealthy
  until the first deployment.
- The marketing image embeds its public URLs, so it is built once per environment.
