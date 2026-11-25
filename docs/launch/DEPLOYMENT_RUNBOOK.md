# Deployment runbook

How a Mise release reaches staging and production. Background and one-time AWS setup: `docs/architecture/deployment.md` (AWS) and
`docs/architecture/deploy-single-server.md` (one server with Docker). This runbook is the step-by-step for a release.

> **Status (Sprint 9):** no AWS account, domain or production database has been provided, so **no step below has been run against
> real staging or production**. The procedure was rehearsed locally with the production compose file (§6). Going live needs the
> approvals in `GO_LIVE_CHECKLIST.md`; nothing here enables live payments.

## 1. What a release contains

| Artifact | Built from | Version shown |
|---|---|---|
| `api` image (also runs `worker` and `misectl`) | `infrastructure/docker/api.Dockerfile` | `GET /api/v1/version` → `{version, commit}`; worker heartbeat on admin `/system` |
| `marketing`, `admin`, `web` images | `infrastructure/docker/web.Dockerfile` with `APP=…` | image tag = commit SHA |

Images are tagged with the commit SHA. `RELEASE_VERSION` (a GitHub variable, or an environment variable for compose) labels the
release, e.g. `2026.12.1`.

Three public hosts: `<domain>` (marketing), `app.<domain>` (customer app: catalogue, plans, accounts, checkout), `admin.<domain>`.
The API is reached through those apps; only health, version, waitlist/referral endpoints and payment webhooks are routed to it
directly.

## 2. Before every release

1. CI green on the commit: Go tests, Vitest, typecheck, lint, Playwright.
2. Read the migrations in the release. They must be **compatible with the code already running** (expand first, contract in a later
   release): migrations run while the previous version still serves traffic.
3. New configuration? Add it to Secrets Manager / `.env.production` **before** deploying. The API refuses to start on invalid
   production configuration (for example `PAYMENTS_TEST_PROVIDER=true`, `MAIL_DRIVER=log`, MFA off).
4. Confirm the database has a recent automated backup (RDS console → Automated backups; single server: last `pg_dump`).
5. Write down the currently deployed commit (`curl https://<domain>/api/v1/version`). This is the rollback target.

## 3. AWS (ECS Fargate) — staging

Runs automatically: `.github/workflows/deploy.yml` on a green CI run on `main`.

1. The workflow assumes the staging deploy role (OIDC), builds and pushes the four images.
2. `ecs-deploy.sh migrate` runs `misectl migrate up` as a one-off task. **Failure stops the release; nothing is rolled out.**
3. `ecs-deploy.sh services` registers new task definitions for `api worker marketing admin web` and waits until all five are stable.
   The ECS circuit breaker rolls back tasks that fail health checks.
4. CloudFront is invalidated; the smoke test checks `/api/v1/ready`, the site, `app.<domain>/meals` and `/api/v1/version`.

## 4. AWS — production

1. GitHub → Actions → **Deploy** → *Run workflow* → environment `production`, from the commit already verified on staging.
2. A required reviewer approves the `production` environment.
3. Same steps as staging. Watch the run; it fails loudly at the first failed step.
4. Do the post-deploy checks (§7).

## 5. Single server (Docker Compose)

```sh
cd /opt/mise
git fetch && git checkout <release commit>
RELEASE_VERSION=<version> RELEASE_COMMIT=$(git rev-parse --short HEAD) \
  docker compose -f docker-compose.prod.yml --env-file .env.production --profile caddy up -d --build
```

`migrate` runs first; `api` and `worker` start only if it succeeds. Omit `--profile caddy` when another proxy terminates TLS
(point it at `127.0.0.1:3100` site, `3102` app, `3101` admin).

## 6. Local staging rehearsal (what was actually verified)

Recorded in `docs/testing/STAGING_REHEARSAL.md`: the unmodified `docker-compose.prod.yml` with `MISE_ENV=production` (without the Caddy
profile, ports on 127.0.0.1 only), a fresh database, throwaway secrets and Mailpit as SMTP. It verifies the images, migrations, the
production configuration checks, security headers, MFA enforcement and the worker. It does not verify TLS. It does **not** verify AWS, real DNS, real certificates, SES, RDS
backups or CloudFront.

## 7. Post-deployment checks (every release)

| Check | How | Expected |
|---|---|---|
| Version | `curl https://<domain>/api/v1/version` | the new commit |
| Readiness | `curl https://<domain>/api/v1/ready` | 200 |
| Site / app / admin | open `<domain>`, `app.<domain>/meals`, `admin.<domain>/login` | pages render over HTTPS |
| Worker | admin → **System** | worker heartbeat within the last minute, same version as the API |
| Errors | CloudWatch `ApiErrors`, `JobsFailed` alarms / `docker compose logs api worker` | no new errors |
| Launch gates | admin → **Launch** | unchanged unless the release meant to change them |
| Email | trigger a password reset to a staff address | arrives (provider evidence, not just "sent") |

If any check fails, follow `ROLLBACK_RUNBOOK.md`.

## 8. First production deployment only

1. Infrastructure: `terraform apply` of `infrastructure/terraform/environments/production` (needs AWS access, domain, SES
   verification out of sandbox).
2. Secrets in Secrets Manager: `TOKEN_SIGNING_KEY`, `DATA_ENCRYPTION_KEY`, `BFF_SHARED_KEY`, SMTP credentials. Generate fresh; never
   copy from staging.
3. Deploy (§4). The database is empty apart from roles and permissions created by migrations.
4. Create the first super admin: `misectl admin create` (one-off task), then enrol MFA.
5. `SITE_MODE` stays `waitlist`. Switching to `launch` is a separate, approved release (`GO_LIVE_CHECKLIST.md` §Business sign-off).
6. Never run `misectl … seed-demo` in production. Keep `CATALOG_DEMO_ENABLED=false`: the catalogue and commerce demo seeders refuse
   without it, and the production and delivery seeders refuse outside `MISE_ENV=local`.
