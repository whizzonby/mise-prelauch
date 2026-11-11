# ADR-005: Infrastructure

Status: accepted (Sprint 1). Extends `prelaunch/006-aws-deployment.md`.

## Context

The pre-launch Terraform describes AWS (ECS Fargate, RDS, ALB, CloudFront, WAF, SES,
Secrets Manager, CloudWatch) but **has never been applied**. A single-server production
path (`docker-compose.prod.yml`, `infrastructure/native/mise.sh`) also exists. Which one
is live is not known from the repository.

## Decision

1. **No new paid services and no `terraform apply` in Sprint 1.** Code changes only.
2. Terraform gains what the new code needs to start: two Secrets Manager secrets
   (`DATA_ENCRYPTION_KEY`, `BFF_SHARED_KEY`, about US$0.80 a month together) and settings
   (`ADMIN_MFA_REQUIRED=true`, `CUSTOMER_REGISTRATION_ENABLED=false`, `CUSTOMER_APP_URL`).
3. **The customer app is not added to AWS yet.** Deploying it means a fifth Fargate service
   (0.25 vCPU / 0.5 GB, about US$9 to 12 a month per environment), an ALB host rule and a
   DNS record. That is a cost and a business decision (registration is closed anyway), so
   it waits for Sprint 2 approval. It runs locally in Docker Compose today.
4. **Redis is not introduced.** Rate limits are per process with WAF as the global
   backstop; sessions are in PostgreSQL. Revisit when the API runs more than two tasks
   or needs shared, low-latency counters.
5. **Tracing is prepared, not deployed.** The API reads W3C `traceparent` and logs the
   trace id beside the request id. Adding the OpenTelemetry SDK and an ADOT collector
   sidecar is a later, costed step.
6. **SQS is not introduced.** The PostgreSQL job queue covers current volume
   (`prelaunch/008`). Move a job kind to SQS when it needs fan-out or throughput the
   database should not carry.

## Estimated recurring cost

Unchanged from `deployment.md` (staging at idle about US$90 to 120 a month) plus about
US$0.80 for the two new secrets. Adding the customer app later: about US$9 to 12 per
environment per month.

## Update, 11 November 2026 (Sprint 2)

- **Media storage approved:** a private S3 bucket for meal and chef photographs, served
  through the existing site distribution at `/media/*` (`storage.tf`).
- **Cost:** estimated under US$1 a month at launch volume.
- **Not applied yet.**
- **No service has write access** until admin uploads are built.
