# Deployment

## Environments

| | Local | Staging | Production |
|---|---|---|---|
| Where | Docker Compose on a laptop | AWS | AWS |
| Site | `localhost:3100` | `staging.<domain>` | `<domain>` |
| Database | Compose PostgreSQL | Its own RDS instance | Its own RDS instance, Multi-AZ |
| Email | Mailpit (nothing leaves the machine) | SES | SES |
| Configuration | `.env` | Terraform + Secrets Manager | Terraform + Secrets Manager |
| Deployed by | you | GitHub Actions, on every push to `main` | GitHub Actions, by hand, with approval |

Staging and production are separate Terraform roots with separate state, VPCs, databases,
secrets and deploy roles. They share nothing, and a staging credential cannot reach
production. `MISE_ENV` tells the application which it is in; development-only settings
(`MAIL_DRIVER=log`, `RATE_LIMITS_DISABLED`, `migrate down`) are refused outside `local`.

## What runs in AWS

```
                      Route 53
                         │
        ┌────────────────┴───────────────────┐
   <domain>                        api.<domain>, admin.<domain>
        │                                    │
   CloudFront ── origin.<domain> ──►  Application Load Balancer  ◄── WAF
                                             │  (host-based routing)
                 ┌───────────────┬───────────┴────┬──────────────┐
            marketing           api            admin          worker     ECS Fargate
                                 │                               │
                                 └────────── RDS PostgreSQL ─────┘
                                                                 │
                                                          SES (SMTP)
   Secrets Manager → injected into tasks        CloudWatch logs → metric filters → alarms → SNS
```

- **Compute**: ECS on Fargate. Four services (`api`, `worker`, `marketing`, `admin`) and a
  one-off `migrate` task. No Kubernetes, no servers to patch.
- **Network**: tasks run in public subnets behind a security group that accepts traffic
  only from the load balancer; the database is in private subnets. This avoids a NAT
  gateway. See ADR-006.
- **Database**: RDS PostgreSQL 17, encrypted, TLS required, automated backups.
- **Email**: SES over SMTP, with DKIM, a custom MAIL FROM domain, SPF and DMARC records.
- **Edge**: CloudFront in front of the marketing site; WAF (rate rule plus AWS managed
  rules) on the load balancer.
- **Observability**: JSON logs in CloudWatch; metric filters turn log events into metrics
  (`LeadsCreated`, `LeadsVerified`, `EmailsSent`, `JobsFailed`, `ApiErrors`,
  `AdminLoginsFailed`); alarms email the address in `alarm_email`.

Rough monthly cost for staging at idle, before traffic: four small Fargate tasks, one
`db.t4g.micro`, a load balancer and WAF. Expect on the order of US$90 to US$120. The load
balancer and Fargate tasks are most of it.

## Setting up an environment (once)

Prerequisites, done by hand once per AWS account:

1. A Route 53 hosted zone for the domain.
2. An S3 bucket (private, versioned, encrypted) and a DynamoDB table for Terraform state.
3. The GitHub OIDC identity provider (`token.actions.githubusercontent.com`).
4. SES production access for the account (new accounts are in the sandbox and can email
   only verified addresses).

Then, for each environment:

```sh
cd infrastructure/terraform/environments/staging
cp backend.hcl.example backend.hcl          # fill in
cp staging.tfvars.example staging.tfvars    # fill in
terraform init -backend-config=backend.hcl
terraform apply -var-file=staging.tfvars
terraform output github_environment_variables
```

5. In GitHub, create an environment named `staging` (and `production`), and add each value
   from `github_environment_variables` as an environment **variable**. On `production`,
   add a required reviewer.
6. Confirm the SNS email subscription for alarms.

The first `apply` creates services that point at an image tag that does not exist yet, so
they will show as unhealthy until the first deployment pushes real images. That is expected.

7. Run the Deploy workflow. Then create the first admin:

```sh
aws ecs run-task --cluster mise-staging --launch-type FARGATE \
  --task-definition mise-staging-migrate \
  --network-configuration "$(aws ecs describe-services --cluster mise-staging --services mise-staging-api --query 'services[0].networkConfiguration' --output json)" \
  --overrides '{"containerOverrides":[{"name":"migrate","command":["/app/misectl","admin","create","-email","you@example.com","-name","Your Name","-role","super_admin"],"environment":[{"name":"MISE_ADMIN_PASSWORD","value":"…"}]}]}'
```

(The password is visible in the task's override record to anyone with ECS read access.
Change it after first sign-in once a "change password" screen exists, or pass it from a
short-lived Secrets Manager secret.)

## How a deployment works

`.github/workflows/deploy.yml`, and nothing else, deploys.

1. CI passes on `main` (staging), or someone runs the workflow for `production` and a
   reviewer approves it.
2. GitHub Actions assumes the environment's deploy role through OIDC. There are no
   long-lived AWS keys anywhere. The role trusts only this repository's jobs that declare
   that environment.
3. Three images are built and pushed, tagged with the commit SHA. The marketing image is
   built per environment because its public URLs are compiled in.
4. **Migrations run first**, as a one-off task. If they fail, the workflow stops and
   nothing is rolled out.
5. Each service gets a new task definition revision with the new image. ECS replaces tasks
   with no downtime (new tasks must pass health checks before old ones stop).
6. The CloudFront cache is invalidated and the workflow smoke-tests the site and
   `/api/v1/ready`.

Because migrations run while the previous release is still serving, a migration must be
compatible with the code that is already running: add columns and tables first, remove
them in a later release.

## Rollback

- **A bad release that fails health checks** is rolled back automatically by the ECS
  deployment circuit breaker.
- **A bad release that passes health checks**: re-run the Deploy workflow from the last
  good commit.
- **A bad migration**: migrations are forward-only outside local development. Write a new
  migration that undoes it. For data loss, restore the database to a point in time from
  RDS backups.

## Configuration reference

Every variable is documented in `.env.example`. In AWS the same variables are set by
`infrastructure/terraform/modules/platform/compute.tf`; secrets come from Secrets Manager.
