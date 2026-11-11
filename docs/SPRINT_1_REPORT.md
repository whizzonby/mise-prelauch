# Sprint 1 report: platform foundation

Written 11 November 2026, at the end of Sprint 1. Sprint 1's changes are **not yet
committed** (by decision); they sit in the working tree on top of commit `d70359c`.

Sprint 1's code, documentation and tests are finished, and every check below was run on
11 November 2026. The Next.js security upgrade (F1) is applied and verified. The production
dependency audit is clean, and the full test suite, including Playwright 24/24, passes on
it.

---

## A. Existing system findings

Full detail: `docs/architecture/EXISTING_SYSTEM_AUDIT.md`.

**Already implemented (pre-launch):** a pnpm workspace monorepo with two Next.js apps
(`apps/marketing`, `apps/admin`). Behind them, a Go modular monolith (`services/api`:
chi, pgx, Goose, slog), with PostgreSQL 17 and a PostgreSQL-backed job queue and worker.
Features: waitlist signup with email verification and bot checks, referral links counted
only after verification, preference profiling, unsubscribe, event analytics, an admin
console (dashboard, lead search, lead detail, status change, CSV export with audit, erasure),
and SMTP/SES email. Also present: Terraform for AWS (never applied), a single-server
Docker/native production path, CI and deploy workflows (never run), 27 Go tests, 28 Vitest
tests and 17 Playwright tests.

The existing stack already matched the target (Go, chi, pgx, Goose, PostgreSQL, Next.js,
Tailwind), so **no migration plan was needed. The work extended the system in place.**

**Reused unchanged:** the marketing site (pages, content, design), lead, referral and event
behaviour, the platform packages (`config`, `db`, `httpx`, `jobs`, `mail`, `ratelimit`,
`token`), the design tokens in `packages/ui`, and the Terraform module layout.

**Needed improvement:**
- admin auth was a separate `admin_users` table with a role map hard-coded in Go and no MFA
- there was no customer identity
- CI had three defects: typecheck failed on a clean checkout, `pnpm test` ran Playwright
  with no stack, and a gofmt failure (audit D1–D3)
- there was no OpenAPI contract

## B. Architecture

**Selected:** a modular monolith. There is one Go API binary plus one worker binary from the
same module, with domain packages under `internal/`. Three Next.js apps (marketing, customer
web, admin) share `packages/ui`, `validation`, `api-client` and `config`. PostgreSQL is the
system of record. ADRs 001–005 in `docs/decisions/` give the reasons. In short: a small
team, one deployable, strict package boundaries, and no infrastructure cost added before
there is demand.

Main decisions:

1. **One identity model** (`users`) for every person who signs in: customers now, and
   staff, chefs and farmers later. Sessions have an *audience* (`customer` or `admin`), so a
   customer session can never act as admin (ADR-003).
2. **Opaque, revocable, database-backed sessions** in httpOnly cookies, not JWTs. The
   customer app is a backend-for-frontend, so the browser never holds an API token, and
   nothing goes in `localStorage`. A separate token flow for Flutter is designed (the
   `client` column is reserved) but not built.
3. **Role-based access control in the database**, with explicit permission keys and role
   assignments that can be scoped to an organisation or context. There is no `is_admin`.
   A test fails if the code checks a permission the database does not define.
4. **TOTP MFA for admins.** It is required outside `local` (`ADMIN_MFA_REQUIRED`). The
   secret is encrypted with AES-256-GCM, and codes cannot be replayed.
5. **Leads are linked, never converted.** A `customers.lead_id` link is created only when
   a *verified* account matches a lead's canonical email. Blocked leads are not linked. The
   lead row, its referral attribution, its source and its consent stay untouched.
6. **The OpenAPI 3.1 contract is the source of truth.** The TypeScript types are generated
   from it, and a Go test fails if the routes and the contract disagree.
7. **No new paid services:** no Redis, no SQS, and no tracing backend. Tracing is prepared
   (W3C `traceparent` is read and logged), not deployed (ADR-005).

## C. Implementation

**New modules:**
- `services/api/internal/identity/`: service, HTTP handlers, password hashing (Argon2id),
  TOTP, the principal, queries written with sqlc
- `internal/customers/`: the customer record and the lead link
- `internal/audit/`
- `internal/platform/crypto/`: AES-GCM
- `internal/platform/email/`: canonical email
- `internal/leads/account.go`, `internal/notifications/account.go`, and 6 account email
  templates
- `sqlc.yaml`
- `apps/web/`: the customer app shell, with sign-in, register, check-email, verify-email,
  forgot and reset password, and a signed-in page with a sessions list
- `apps/admin/src/app/login/mfa/` and `mfa-form.tsx`
- `packages/api-client/src/schema.d.ts` and `contract-check.ts`
- `e2e/tests/accounts.spec.ts`
- all the documents the brief listed under `docs/architecture`, `docs/database`,
  `docs/api`, `docs/security` and `docs/decisions` (the pre-launch ADRs moved to
  `docs/decisions/prelaunch/`)

**Modified:**
- server wiring and tests
- admin handlers, which now use identity (`admin/auth.go` removed)
- config, httpx (request IDs, trace IDs, the forwarded client address for the BFF)
- `misectl` (admin create, grant, revoke, reset-mfa, disable, enable, signout; `doctor`)
- worker
- admin app login, session and permission names
- root scripts, `.env.example` files, Compose files, CI workflow
- Terraform `compute`, `secrets` and `variables` (not applied)
- README and deployment docs

**Migration:** `00004_identity.sql` is additive. It adds `users`, `password_credentials`,
`sessions`, `auth_tokens`, `mfa_totp`, `roles`, `permissions`, `role_permissions`,
`user_roles` and `customers`, plus two nullable `audit_log` columns. It copies admins from
`admin_users` with the same ids and password hashes. `admin_users` and `admin_sessions` are
kept for rollback. Its *down* section drops the new tables, and `misectl` allows `down`
only when `MISE_ENV=local`.

**Endpoints (all under `/api/v1`, all in `docs/api/openapi.yaml`):**
- **Customers:** `auth/register`, `auth/verify-email`, `auth/login`, `auth/logout`,
  `auth/password/forgot`, `auth/password/reset`, `me`, `me/sessions` (list, revoke the
  others), `me/sessions/{id}` (revoke)
- **Admin:** `admin/auth/login`, `logout`, `me`, `mfa/setup`, `mfa/confirm`, `mfa/verify`
- **Unchanged pre-launch endpoints:** `health`, `ready`, the leads, referrals and events
  endpoints, admin stats and leads

**Permissions implemented:** `admin.access`, `analytics.view`, `leads.read`,
`leads.update`, `leads.export`, `leads.erase`, `users.read`, `users.manage`.

## D. Verification (run 11 November 2026)

Go ran in `golang:1.27-alpine` (the local Go SDK is broken, risk R4). PostgreSQL 17 and
Mailpit ran in an isolated Compose project (`mise-s1`), so no development or production
data was touched.

| Check | Result |
|---|---|
| `gofmt -l`, `go vet`, `go build` | clean |
| Go tests, database tests run against real PostgreSQL | **pass**: 69 tests and subtests, 0 skipped. This includes registration, duplicate-account hiding, lead linking, blocked leads not linked, password reset ending every session, session revoke, idle and absolute expiry, separate audiences, database permissions, scoped roles, disabled accounts, MFA enrolment, challenge and lockout, copying the pre-launch admins, and routes matching OpenAPI |
| Migrations on an empty database: up → down → up, then `misectl doctor` | **pass** |
| sqlc generated code is current | **pass**: no diff |
| OpenAPI → TypeScript types are current | **pass**: no diff |
| ESLint, all workspaces | **pass** |
| `tsc` typecheck, all workspaces | **pass** after one fix: `accounts.spec.ts` had a possibly-undefined value, left from the session cut short by the power loss |
| Vitest | **pass**: web 3, admin 6, marketing 22 |
| `next build`, all three apps | **pass** |
| Playwright, against API and worker (Docker) plus the three built apps | **24/24 pass**: 7 new account tests (register → email → confirm → sign in → sessions → sign out, refusals, password reset, a lead linked to its account as seen in the admin, axe scans of 3 pages) and all 17 pre-launch tests (no regressions) |
| Dependency audit (`pnpm audit --prod --audit-level high`) | Failed on `next@16.3.5` and others. **Now clean** after the F1 upgrade |
| After the F1 upgrade (Next.js 16.4.6) | lint, typecheck, Vitest 31/31 and all three builds **pass**; a `--frozen-lockfile` install works. **Playwright 24/24 pass** on 16.4.6 |

**Not tested:**
- `docker compose up --build` this time: the image build failed because npm registry
  requests timed out from inside Docker (a network problem; the same images built
  before Sprint 1)
- the GitHub Actions workflows themselves, because there is no remote
- `govulncheck`
- anything on AWS, and SES
- Safari and Firefox, physical phones, and screen readers
- load and performance
- a backup restore
- the Flutter token flow, which is designed but not built

## E. Infrastructure

**Running:** nothing new in any cloud. Sprint 1 provisioned no resources and made no
production or DNS changes. Locally, it uses Docker Compose (PostgreSQL, Mailpit, API,
worker, migrate, and the marketing, admin and web apps).

**Deployment changes:** code only.
- Terraform gains two secrets (`DATA_ENCRYPTION_KEY`, `BFF_SHARED_KEY`) and three settings
  (`ADMIN_MFA_REQUIRED=true`, `CUSTOMER_REGISTRATION_ENABLED=false`, `CUSTOMER_APP_URL`).
- Compose gains a `web` service.
- CI gains checks that the OpenAPI types, the sqlc code and the migration round-trip are
  current, plus the audit and secret scans.

**Estimated ongoing cost:**
- Unchanged from pre-launch: staging at idle is about US$90–120 a month (see
  `deployment.md`).
- The two new secrets add about US$0.80 a month.
- Deploying the customer app later would be one more Fargate service, about US$9–12 a month
  per environment. Not approved, not provisioned.

## F. Risks and open items

| # | Item | Action |
|---|---|---|
| **F1** | **Fixed and verified (Playwright 24/24 on 16.4.6).** `next@16.3.5` had critical advisories (unauthenticated RCE in image optimisation and `use cache`, RCE in `next/og`), and there were high advisories for zod, sharp and source-map-js. These were already present in the **pre-launch apps**. Now: `next` and `eslint-config-next` 16.4.6, `sharp` 0.35.5, `zod` 4.6.7, and `source-map-js` 1.2.2 and `undici` 8.12.4 (test tooling only) held at the patched minimums by `overrides` in `pnpm-workspace.yaml`. One high advisory remains, in `vite` 8.3.0. Vite is used only by the unit-test runner, never shipped. Its update hits a pnpm bug (`depNode.fetching is not a function`) | **If the pre-launch site is live, deploy this upgrade first.** Retry the vite update when pnpm is updated |
| F2 | Where production runs (single server or AWS) is unknown from the repository | Tell us. Deploying migration 00004 and the admin MFA change needs your approval |
| F3 | Admin MFA becomes mandatory outside `local`; every admin enrols at their next sign-in, and existing admin sessions end | Expected. `misectl admin reset-mfa` recovers a lost authenticator |
| F4 | Docker image builds need registry access | Retry on a stable connection |
| F5 | Uncommitted work | Commit when you decide. The Sprint 1 changes exist only in this working tree |

**Business decisions needed:**
1. When to open customer registration. It is off outside `local`
   (`CUSTOMER_REGISTRATION_ENABLED`).
2. Whether to deploy the customer app (cost above).
3. The production domain (still the placeholder `mise.tt`).
4. Whether admins are restricted to given IP ranges.
5. Whether anything in the 1 October meeting notes affects identity. The PDF could not be
   read here.

## G. Sprint 2 readiness

**Ready for the customer web platform:**
- registration, email verification, sign-in and out, password reset, session list and
  revoke
- a `customers` record linked to a user and, when it matches, to the waitlist lead (with
  attribution preserved)
- RBAC to add customer-facing and staff permissions to
- the BFF pattern in `apps/web`
- the OpenAPI contract and generated client
- the shared design tokens and components
- the audit log
- the job queue for transactional email

**Before Sprint 2 starts:**
- commit the Sprint 1 work
- the business decisions above, chiefly whether registration opens and where production
  runs

**Not started, by design:** the customer dashboard, catalogue, subscriptions, checkout,
payments, orders, the chef and farmer tools, inventory, production, delivery, and mobile.
