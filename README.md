# Mise

Mise is a Caribbean meal-kit company based in Trinidad & Tobago. This repository is the
**Mise Digital Platform**: the public site and waitlist (pre-launch), the customer app,
the internal admin, and the Go API behind them. Sprint 1 laid the foundation: one
identity model with database RBAC, revocable sessions and admin MFA, the customer app
shell, an OpenAPI contract, and the link from waitlist leads to customer accounts.
Start with `docs/architecture/SYSTEM_OVERVIEW.md`.

## What is in the repository

| Path | What it is |
|---|---|
| `apps/marketing` | Public site (Next.js). Static-first. Waitlist, invitation links, personal welcome page. Port 3100. |
| `apps/admin` | Internal admin (Next.js, server-rendered). Sign-in with MFA, dashboard, leads, CSV export. Port 3101. |
| `apps/web` | Customer app (Next.js, server-rendered). Sprint 1: register, verify email, sign in, reset password, sessions. Port 3102. |
| `services/api` | The Mise API, worker and operator CLI (Go, one module, three binaries). Port 8090. |
| `packages/ui` | The Mise design system: tokens, primitives, motion. |
| `packages/validation` | Zod schemas for forms. |
| `packages/api-client` | Typed client for the API; types generated from `docs/api/openapi.yaml`. |
| `packages/analytics` | First-party event taxonomy, tracker and attribution capture. |
| `packages/config` | Shared TypeScript and ESLint configuration. |
| `e2e` | Playwright end-to-end tests. |
| `infrastructure` | Dockerfiles, Terraform for AWS, scripts. |
| `docs` | Architecture, API, database and decision records. Start with `docs/architecture/plan.md`. |

## Run it locally

You need Docker, Node 22 or newer with pnpm 11, and Go 1.27 or newer.

Mise uses its own port block so it can run beside other projects:
marketing **3100**, admin **3101**, customer app **3102**, API **8090**, PostgreSQL **5460**, Mailpit **1026** (SMTP) and **8026** (inbox).

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
pnpm dev                # terminal 3: marketing :3100, admin :3101, customer app :3102
```

### Create an admin

The password is read from an environment variable so it never lands in shell history.

```sh
MISE_ADMIN_PASSWORD='at-least-12-characters' pnpm misectl admin create -email you@example.com -name "Your Name" -role super_admin
```

In containers: `docker compose run --rm -e MISE_ADMIN_PASSWORD=… migrate /app/misectl admin create -email … -name … -role super_admin`.

Staff roles: `super_admin`, `admin`, `marketing`, `operations`, `support` (plus `finance`,
`chef`, `kitchen_staff`, `nutritionist`, `farmer`, which have no permissions yet). Roles and
permissions live in the database; see `docs/security/AUTHORIZATION.md`. Other operator commands:
`misectl admin grant|revoke|reset-mfa|disable|enable|signout -email …` and `misectl doctor`.

Outside local development every admin sets up an authenticator app at their first
sign-in. Set `ADMIN_MFA_REQUIRED=true` in `.env` to try that flow locally.

### Useful addresses

| | |
|---|---|
| Site | <http://localhost:3100> |
| Admin | <http://localhost:3101> |
| Customer app | <http://localhost:3102> |
| API health | <http://localhost:8090/api/v1/health> |
| Email inbox (every email the worker sends) | <http://localhost:8026> |

Demo data for the admin dashboard (invented `@example.com` leads):

```sh
docker compose exec -T postgres psql -U mise -d mise < infrastructure/scripts/seed-dev.sql
```

## Tests

```sh
pnpm lint && pnpm typecheck   # every workspace
pnpm test                     # frontend unit and component tests (Vitest; excludes Playwright)
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

## Generated code

Two kinds of code are generated and committed; CI fails if either is stale.

```sh
pnpm sqlc            # Go from services/api/internal/*/queries.sql (runs sqlc in Docker)
pnpm generate:api    # TypeScript types from docs/api/openapi.yaml
```

When you add or change an endpoint, update `docs/api/openapi.yaml` in the same change:
`TestRoutesMatchOpenAPI` fails otherwise.

## Troubleshooting

- **`pnpm typecheck` complains about image imports**: it now runs `next typegen` first; run `pnpm install` if `next` is missing.
- **Go is not on PATH**: everything Go can run in Docker, for example
  `docker run --rm -v "$PWD/services/api:/src" -w /src golang:1.27-alpine go test ./...`
  (database tests need `TEST_DATABASE_URL` and a network that reaches PostgreSQL).
- **The API refuses to start**: the message names the setting. Common ones outside local:
  `ADMIN_MFA_REQUIRED` cannot be false in production; `BFF_SHARED_KEY` must be 32+ characters.
- **Admin sign-in loops back to the code page**: the session is waiting for MFA; enter a code, or ask an operator for `misectl admin reset-mfa`.
- **No email arrives locally**: the worker must be running; every email is in Mailpit at <http://localhost:8026>.

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

- `docs/architecture/SYSTEM_OVERVIEW.md`: the platform, and where each topic is documented
- `docs/architecture/EXISTING_SYSTEM_AUDIT.md`: what Sprint 1 found and changed
- `docs/api/openapi.yaml`, `docs/api/API_STANDARDS.md`: the API contract and its rules
- `docs/security/`: authentication, authorisation, threat model
- `docs/database/`: conceptual ERD, Sprint 1 schema, migration strategy
- `docs/architecture/plan.md`: the pre-launch architecture and implementation plan
- `docs/architecture/design-system.md`: tokens, type, motion, components
- `docs/architecture/security.md`: threat-by-threat security and privacy approach
- `docs/architecture/analytics.md`: event taxonomy and attribution
- `docs/architecture/deployment.md`: environments, infrastructure, first deploy, rollback
- `docs/api/README.md`: the REST API
- `docs/database/schema.md`: tables, constraints, lifecycle
- `docs/decisions/`: architecture decision records (Sprint 1 ADR-001 to 005; pre-launch records in `prelaunch/`)
- `docs/status.md`: what has been verified, and what has not yet been exercised
