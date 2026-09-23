# Mise

Mise is a Caribbean meal-kit company based in Trinidad & Tobago. This repository is the
**pre-launch platform**: the public site that introduces Mise and builds the waitlist, the
API that owns the lead data, and the internal admin the team uses to read it. It is
phase 1 of the full Mise platform, so the design system, API, database and infrastructure
here are the ones the customer product will grow from.

## What is in the repository

| Path | What it is |
|---|---|
| `apps/marketing` | Public site (Next.js). Static-first. Waitlist, invitation links, personal welcome page. Port 3100. |
| `apps/admin` | Internal admin (Next.js, server-rendered). Dashboard, leads, CSV export. Port 3101. |
| `services/api` | The Mise API, worker and operator CLI (Go, one module, three binaries). Port 8090. |
| `packages/ui` | The Mise design system: tokens, primitives, motion. |
| `packages/validation` | Zod schemas for forms. |
| `packages/api-client` | Typed client for the API. |
| `packages/analytics` | First-party event taxonomy, tracker and attribution capture. |
| `packages/config` | Shared TypeScript and ESLint configuration. |
| `e2e` | Playwright end-to-end tests. |
| `infrastructure` | Dockerfiles, Terraform for AWS, scripts. |
| `docs` | Architecture, API, database and decision records. Start with `docs/architecture/plan.md`. |

## Run it locally

You need Docker, Node 22 or newer with pnpm 11, and Go 1.27 or newer.

Mise uses its own port block so it can run beside other projects:
marketing **3100**, admin **3101**, API **8090**, PostgreSQL **5460**, Mailpit **1026** (SMTP) and **8026** (inbox).

### Everything in containers

```sh
docker compose up --build
```

That starts PostgreSQL, Mailpit, runs the migrations, and starts the API, the worker and
both apps. Then create an admin (see below) and open <http://localhost:3100>.

### Apps on the host, with hot reload

```sh
cp .env.example .env
pnpm install
pnpm services:up        # PostgreSQL and Mailpit in Docker
pnpm db:migrate         # apply migrations
pnpm dev:api            # terminal 1: API on :8090
pnpm dev:worker         # terminal 2: background jobs (email)
pnpm dev                # terminal 3: marketing on :3100, admin on :3101
```

### Create an admin

The password is read from an environment variable so it never lands in shell history.

```sh
MISE_ADMIN_PASSWORD='at-least-12-characters' pnpm misectl admin create -email you@example.com -name "Your Name" -role super_admin
```

In containers: `docker compose run --rm -e MISE_ADMIN_PASSWORD=… migrate /app/misectl admin create -email … -name … -role super_admin`.

Roles today: `super_admin`, `admin`, `marketing`, `operations`, `support`
(what each may do is in `services/api/internal/admin/auth.go`).

### Useful addresses

| | |
|---|---|
| Site | <http://localhost:3100> |
| Admin | <http://localhost:3101> |
| API health | <http://localhost:8090/api/v1/health> |
| Email inbox (every email the worker sends) | <http://localhost:8026> |

Demo data for the admin dashboard (invented `@example.com` leads):

```sh
docker compose exec -T postgres psql -U mise -d mise < infrastructure/scripts/seed-dev.sql
```

## Tests

```sh
pnpm lint && pnpm typecheck   # every workspace
pnpm test                     # frontend unit and component tests (Vitest)
pnpm test:go                  # Go tests; database tests run when TEST_DATABASE_URL is set (it is, in .env)
pnpm e2e                      # Playwright, against the running stack
```

### End-to-end tests

The Playwright suite drives the real stack: it needs the API, worker, both apps, PostgreSQL
and Mailpit running (either way of running locally works), and an admin whose credentials
are in `E2E_ADMIN_EMAIL` / `E2E_ADMIN_PASSWORD`. First run: `pnpm --filter @mise/e2e exec playwright install chromium`.

It covers: visitor → signup → email verification → welcome page; invitation link → signup →
referral credit; duplicate and invalid signups; admin sign-in → dashboard → lead → export;
mobile navigation; reduced motion; keyboard use; automated accessibility scans; SEO essentials.

Two local-only settings in `.env.example` exist for these tests: `RATE_LIMITS_DISABLED=true`
(the API refuses to start with it outside `MISE_ENV=local`) and `TRUSTED_PROXY_HOPS=1`.

## Changing content

All copy, meals, plans, form options and legal text live in `apps/marketing/src/content/`.
Components contain no copy. Three things there are placeholders and are marked as such in
the files:

- **Photography**: stock photographs from Pexels, credited at `/credits` and in `docs/content/image-credits.md`.
- **Sample menu**: `content/meals.ts`. Nutrition figures are intentionally absent until a nutritionist supplies them.
- **Legal text**: `content/legal.ts` describes what the platform does today but has not had legal review.

Plan prices are `null` in `content/plans.ts`, which shows "Price announced before launch".

## Deploying

Staging and production run on AWS (ECS Fargate, RDS PostgreSQL, SES, CloudFront), defined
in `infrastructure/terraform`. Deployments run only from GitHub Actions: a push to `main`
deploys staging after CI passes; production is a manual run that needs an approval.
See `docs/architecture/deployment.md`.

## Documentation

- `docs/architecture/plan.md`: the architecture and implementation plan
- `docs/architecture/design-system.md`: tokens, type, motion, components
- `docs/architecture/security.md`: threat-by-threat security and privacy approach
- `docs/architecture/analytics.md`: event taxonomy and attribution
- `docs/architecture/deployment.md`: environments, infrastructure, first deploy, rollback
- `docs/api/README.md`: the REST API
- `docs/database/schema.md`: tables, constraints, lifecycle
- `docs/decisions/`: architecture decision records
- `docs/status.md`: what has been verified, and what has not yet been exercised
