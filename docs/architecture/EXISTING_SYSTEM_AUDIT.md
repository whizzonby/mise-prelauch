# Existing system audit (Sprint 1, step 1)

Audited: 11 November 2026, on commit `d70359c` of the pre-launch repository
(`github.com/whizzonby/mise-prelauch`, branch `main`). Sprint 1 work happens on the branch
`sprint-1/platform-foundation`, in a clone of that repository, so the pre-launch checkout
(`E:\MISE PRELAUNCH\mise`) is never modified.

Every finding below is marked:

- **[V] Verified**: read in the code or configuration, or observed by running it.
- **[A] Assumption**: inferred, not proven. Each one is worth confirming.

---

## 0. Summary

The pre-launch platform is **not** a prototype to replace. It is a well-built, documented
pnpm + Go monorepo that already follows most of the target architecture: a Go modular
monolith on PostgreSQL with pgx and Goose, three Next.js apps' worth of foundations (two
apps and a design-system package), a transactional job queue, Terraform for AWS, CI and
deploy workflows, and 27 Go and 28 TypeScript tests. Sprint 1 therefore **extends** it.
It does not rewrite it.

The main gaps against the Sprint 1 brief are:

1. **Identity.** Only staff can authenticate (`admin_users` / `admin_sessions`). There are
   no customer accounts, no email verification or password reset for accounts, no MFA,
   and roles map to permissions in a Go map rather than in the database.
2. **No OpenAPI contract.** The API is documented in Markdown, and the TypeScript client is
   written by hand.
3. **No customer web app** (`apps/web`).
4. **Three CI defects** that would fail the first real CI run (section 7).

---

## 1. Technology stack

| Layer | Found | Target | Gap |
|---|---|---|---|
| Backend language | Go 1.27.1 (`services/api/go.mod`) [V] | Go | none |
| HTTP router | Go standard library `net/http.ServeMux` with method patterns [V] | Chi | **Differs**: see ADR-001 |
| DB driver | pgx v5.11 (`pgxpool`) [V] | pgx | none |
| Queries | Hand-written SQL in repository functions [V] | sqlc | **Differs**: sqlc introduced for new modules (ADR-002) |
| Migrations | Goose v3.28, SQL files embedded in binaries, applied by `misectl migrate up` [V] | Goose | none |
| API contract | Markdown (`docs/api/README.md`) [V] | OpenAPI | **Missing**: added in Sprint 1 |
| Logging | `log/slog` JSON, request id on every record [V] | Structured logging | none |
| Tracing | Request ids only, no OpenTelemetry [V] | OpenTelemetry | **Missing**: trace-context propagation added; SDK deferred (ADR-005) |
| Jobs | PostgreSQL `jobs` table, `FOR UPDATE SKIP LOCKED`, retries with backoff, stuck-job requeue [V] | Durable jobs, outbox | Present. The enqueue-in-transaction design is already a transactional outbox |
| Cache | none [V] | Redis where justified | Not justified yet |
| Frontend | Next.js 16.3, React 19.3, TypeScript 6, Tailwind 4.3 [V] | same | none |
| UI primitives | Own `@mise/ui` package; Radix Accordion only; no shadcn/ui [V] | shadcn/ui + Radix | Partial: see section 6 |
| Forms | Plain React with Zod 4 schemas in `@mise/validation` [V] | React Hook Form + Zod | RHF not used; adopt when forms grow (Sprint 2) |
| Data fetching | Server components and server actions; no TanStack Query [V] | TanStack Query where appropriate | Not needed yet |
| Animation | CSS plus GSAP ScrollTrigger (lazy-loaded); Motion deliberately not installed [V] | Motion, GSAP | Deliberate, documented in `design-system.md` |
| Icons | lucide-react [V] | Lucide | none |
| Database | PostgreSQL 17 [V] | PostgreSQL | none |
| Infrastructure | Terraform for AWS (ECS Fargate, RDS, ALB, CloudFront, WAF, SES, Secrets Manager, CloudWatch) [V], **never applied** [V per `docs/status.md`]. A single-server production path also exists (`docker-compose.prod.yml`, `infrastructure/native/mise.sh`) [V] | AWS | Which path is live is **unknown** [A]: see risk R1 |
| Package manager | pnpm 11.9 workspaces, no task runner [V] | — | fine |

## 2. Application structure [V]

```
apps/marketing      public site, port 3100 (static-first; waitlist, /join/[code], /verify, /welcome, /unsubscribe, legal)
apps/admin          internal admin, port 3101 (server-rendered; dashboard, leads, lead detail, CSV export)
services/api        one Go module, three binaries: cmd/api, cmd/worker, cmd/misectl
  internal/platform   config, db, httpx (envelope, errors, middleware), jobs, mail, ratelimit, token
  internal/leads      signup, verification, preferences, unsubscribe, erasure
  internal/referrals  codes, referrer lookup, conversion, self-referral flagging
  internal/events     first-party analytics ingestion (allow-listed event types)
  internal/notifications  email templates and job handlers
  internal/admin      admin auth, sessions, permissions, stats, lead list/detail/export/erase, audit
  internal/server     composes modules into one http.Handler
  migrations          00001 initial schema, 00002 packaging preference, 00003 last name
packages/ui, validation, api-client, analytics, config
e2e                 Playwright (17 tests per docs/status.md)
infrastructure      docker/, terraform/, native/, scripts/
docs                architecture/, api/, database/, decisions/ (ADR 001 to 009), status.md
```

## 3. Existing functionality [V]

**API endpoints** (`internal/server/server.go`, `internal/admin/http.go`):

| Method and path | Auth | Purpose |
|---|---|---|
| `GET /api/v1/health`, `GET /api/v1/ready` | none | Liveness; readiness pings the database |
| `POST /api/v1/leads` | none, rate-limited | Waitlist signup (honeypot and timing bot checks) |
| `POST /api/v1/leads/verify` | signed token | Double opt-in |
| `POST /api/v1/leads/unsubscribe` | signed token | Unsubscribe |
| `GET /api/v1/leads/me`, `PATCH /api/v1/leads/preferences` | lead profile token | Welcome page, profiling |
| `GET /api/v1/referrals/{code}` | none | Invitation page lookup |
| `POST /api/v1/events` | optional | First-party analytics |
| `POST /api/v1/admin/auth/login`, `POST …/logout`, `GET …/me` | admin bearer | Admin session |
| `GET /api/v1/admin/stats/overview` | `stats:read` | Dashboard |
| `GET /api/v1/admin/leads`, `GET …/export.csv`, `GET/PATCH/DELETE …/leads/{id}` | `leads:*` | Lead management, audited export and erasure |

**Lead management**: canonical-email uniqueness (strips `+tag` and Gmail dots), status
lifecycle (`PENDING → VERIFIED → QUALIFIED → CONVERTED`, plus `UNSUBSCRIBED`, `BLOCKED`),
first/latest-touch UTM attribution, preferences, a lead event stream, and erasure.

**Referral system**: unique codes; a referral counts only after the referred lead verifies;
self-referral from the same network is flagged and never counted; optional milestone
emails (off by default).

**Admin**: Argon2id passwords (OWASP parameters); 32-byte random session tokens stored
as SHA-256 hashes; a constant-time dummy hash for unknown emails; per-IP and per-email
login limits; five roles (`super_admin`, `admin`, `marketing`, `operations`, `support`)
mapped to five permissions **in Go code**; audit entries for login, status change, export
and erasure. The admin Next.js app is a backend-for-frontend: the token lives in an
`httpOnly`, `SameSite=Strict` cookie, and the browser never calls the API.

**Email**: SMTP (Mailpit locally, SES in AWS) sent by the worker from jobs that carry
ids only, never personal data.

**Lead tokens** (ADR-009): stateless HMAC tokens bound to a purpose (`verify` 7 days,
`profile` 30 days, `unsub` no expiry).

## 4. Database structure [V]

Tables: `leads`, `lead_preferences`, `referrals`, `lead_attribution`, `lead_events`,
`jobs`, `admin_users`, `admin_sessions`, `audit_log`, plus Goose's `goose_db_version`.
UUID primary keys (`gen_random_uuid()`), identity bigints for append-only tables,
`timestamptz` throughout, CHECK constraints for enumerations, foreign keys with deliberate
`ON DELETE` behaviour, and partial and GIN indexes. Full description:
`docs/database/schema.md`. The data itself was not inspected: there is no access to any
production database [V], and the local `mise_postgres-data` Docker volume was
deliberately left untouched.

## 5. Infrastructure, environments, CI/CD

- **Local** [V]: `docker-compose.yml` runs PostgreSQL 17, Mailpit, migrate, api, worker
  and both apps on Mise's own port block (3100/3101/8090/5460/1026/8026). A separate
  `mise_test` database is created for Go integration tests.
- **AWS** [V code, not applied]: two Terraform roots (staging and production) over one
  module. ECS Fargate (api, worker, marketing, admin, one-off migrate), RDS PostgreSQL 17,
  ALB with host routing, CloudFront for the site, WAF, SES with DKIM, SPF and DMARC,
  Secrets Manager, CloudWatch metric filters and alarms. Estimated cost for staging at
  idle: about US$90 to 120 a month (`deployment.md`).
- **Single server** [V code]: `docker-compose.prod.yml` and `infrastructure/native/mise.sh`
  both run `misectl migrate up` on every deploy.
- **CI** [V file, never run]: `ci.yml` covers web lint, types, tests and build; Go
  formatting, vet, race tests and a migration up/down/up check; govulncheck, pnpm audit,
  gitleaks; and the end-to-end suite against `docker compose`. `deploy.yml`: staging on
  every push to `main`, production by manual dispatch with approval, through GitHub OIDC.

## 6. Reusable components [V]

- `@mise/ui`: tokens (cassava/coconut/dasheen palette, Young Serif and Instrument Sans,
  four radii, one shadow, motion tokens), `Container`, `Section`, `Heading`, `Text`,
  `MiseButton`, form controls, `FAQ`, `ImageReveal`, `Tag`, `Wordmark`. The default
  Tailwind palette, radii and shadows are removed, and a test fails the build if a
  gradient appears anywhere. **This already satisfies the brief's visual rules. It is
  reused as is, and the customer app is built on it.**
- `@mise/api-client`: typed fetch client with an `ApiError` envelope parser. Reused, with
  types now generated from OpenAPI.
- `@mise/validation`, `@mise/analytics`, `@mise/config`: reused unchanged.
- Go `platform/*` (config, httpx envelope and errors, db.InTx, jobs, mail, ratelimit):
  reused unchanged as the backend foundation.

shadcn/ui is **not** adopted wholesale. The brief asks for it as a foundation "not as the
finished visual identity", and `@mise/ui` already is a finished, accessible foundation on
Radix. New Radix-based primitives (dialog, dropdown, table) will be added to `@mise/ui` in
shadcn's copy-in style when a screen first needs them (Sprint 2 and later), rather than
installing components nothing uses yet.

## 7. Technical debt and defects found

| # | Finding | Evidence | Action in Sprint 1 |
|---|---|---|---|
| D1 | `pnpm typecheck` fails on a clean checkout: `next-env.d.ts` is gitignored and only generated by `next dev`/`next build`, and CI runs typecheck **before** build | Reproduced in the clean clone: 10 × TS2307 for image imports | Typecheck runs `next typegen` first |
| D2 | `pnpm test` runs the Playwright workspace too, so CI's `web` job would fail without a running stack | Reproduced | Root `test` excludes `@mise/e2e` |
| D3 | `internal/admin/http.go` is not gofmt-clean (last commit), so CI's formatting step would fail | `gofmt -l` | Reformatted |
| D4 | Permissions are a Go map, so roles cannot be changed without a deploy, and future scoped roles (farmer, chef) have nowhere to live | `internal/admin/auth.go` | Replaced by database RBAC |
| D5 | Admin session expiry is absolute only (12 h); there is no idle timeout and no way to revoke a staff member's sessions short of deleting rows | code | Idle timeout and revocation added |
| D6 | No MFA for admins | code, `status.md` | TOTP MFA, required outside local |
| D7 | No OpenAPI; the client types can drift from the server | — | OpenAPI contract, generated types, contract test |
| D8 | `mailpit:latest` is not pinned | compose | Left as is (local only); noted |
| D9 | The CI and Deploy workflows and the Terraform have never run | `status.md` | Still true. Nothing in this sprint applies infrastructure |

## 8. Security concerns

| # | Concern | Severity | Disposition |
|---|---|---|---|
| S1 | No MFA on admin accounts that can export the whole lead list | High | Fixed: TOTP MFA (D6) |
| S2 | In the AWS layout the admin API (`api.<domain>/api/v1/admin/*`) is publicly routable; only authentication protects it. `admin_allowed_cidrs` restricts the admin **app**, not the API path | Medium | Tracked (THREAT_MODEL T7). Recommendation: a WAF rule restricting `/api/v1/admin/*` to the admin app's egress, or serve the API privately |
| S3 | The lead `profile` token (30 days) is kept in `localStorage` | Low: scoped to one lead, unlocks only their referral page and preference answers | Kept (pre-launch behaviour preserved). It never grants account access; documented in AUTHENTICATION.md |
| S4 | Rate limits are per process | Low | Documented; WAF is the global backstop |
| S5 | `unsub` tokens never expire | Accepted by design (ADR-009) | — |
| S6 | `.env` with local defaults exists in the pre-launch working copy; it is gitignored and not committed [V] | None | — |

Strengths worth keeping: no secrets in the repository; strict JSON decoding with size
limits; CSV formula-injection guard; access logs that never record query strings; a
config loader that refuses unsafe settings outside `local`; erasure that removes the
lead's jobs and audit references.

## 9. Architectural inconsistencies

1. `admin` is both a domain (staff identity) and a feature area (lead management screens).
   Identity moves to a new `identity` module; `admin` keeps the back-office lead features.
2. `server.NewLeadService` is used by the worker as well, a small layering inversion.
   Left as is; noted.
3. ADR numbering: the pre-launch ADRs 001 to 009 overlap the ADR names the Sprint 1 brief
   asks for. The pre-launch ADRs move to `docs/decisions/prelaunch/` unchanged, and the
   new ADR-001 to 005 reference what they supersede.
4. The brief's target tree has `services/worker`. Here the worker is `services/api/cmd/worker`,
   which is deliberate (pre-launch ADR-002) and kept.

## 10. Current versus target, and the Sprint 1 plan

### Approach

Extend in place. No existing table is dropped or rewritten, and no existing endpoint
changes its URL or response shape, except that admin login gains an MFA step.

### Plan (in the brief's order)

| Step | Work |
|---|---|
| 1 Audit | This document; baseline test run (below) |
| 2 Architecture | SYSTEM_OVERVIEW, SYSTEM_CONTEXT, CONTAINER_ARCHITECTURE, MODULE_BOUNDARIES, DATA_FLOW; ADR-001 to 005 |
| 3 Repository | `apps/web` added; CI defects D1 to D3 fixed; pre-launch ADRs moved |
| 4 Backend | Reuse `platform/*`; add W3C `traceparent` correlation; `platform/crypto` (AES-GCM at-rest encryption); sqlc for new modules |
| 5 Identity | `internal/identity`: users, password credentials, sessions (web audience: customer or admin), single-use email tokens, TOTP MFA, DB-backed RBAC with scoped assignments. Customer register, verify, login, logout, forgot and reset password, me, session list and revoke. Admin login moves onto it, with an MFA challenge and enrolment |
| 6 Database | Migration 00004 (identity, RBAC, customers, audit columns). Admins are **copied** from `admin_users` with the same ids and password hashes, so their passwords keep working. `admin_users` and `admin_sessions` are kept, unused, for rollback |
| 7 API | `docs/api/openapi.yaml` (3.1) for every endpoint; generated TypeScript types; a Go test that fails when routes and the contract diverge |
| 8 Frontend | `apps/web` customer shell (sign-in, register, verify, forgot and reset password, signed-in placeholder, sessions); admin MFA screens; permission names updated |
| 9 Infra and CI | Compose service for `web`; CI generates and diffs the OpenAPI types; Terraform gains the new settings and secret (not applied) |
| 10 Verify | Go unit and integration tests, Vitest, typecheck, lint, builds, migration up/down/up, and a manual run of the stack |

### Files and modules expected to change

- **New**: `services/api/internal/identity/**`, `internal/customers/**`,
  `internal/platform/crypto/**`, `migrations/00004_identity.sql`, `sqlc.yaml`,
  `apps/web/**`, `packages/api-client/src/schema.d.ts`, `docs/**` (the files listed in
  the brief).
- **Modified**: `internal/server/server.go`, `internal/admin/{auth,http,leads}.go`,
  `internal/notifications/*`, `internal/platform/{config,httpx}`, `cmd/misectl`,
  `cmd/worker`, `apps/admin/src/**` (login, MFA, permission names),
  `packages/api-client/src/index.ts`, root `package.json`, `.env.example`,
  `docker-compose*.yml`, `.github/workflows/ci.yml`, `infrastructure/terraform/modules/platform/{compute,secrets}.tf`,
  `apps/marketing/package.json` (typecheck script only).
- **Unchanged**: the marketing site's pages, components, content and design; lead,
  referral and event behaviour.

## 11. Risks and blockers

| # | Risk or blocker | Impact | Mitigation |
|---|---|---|---|
| R1 | Whether a production deployment exists, and where (single server or AWS), is unknown [A] | Migration 00004 runs automatically on the next deploy of this branch | It is additive and copies admins without changing them. **Deploying it needs your approval** (it changes admin sign-in by adding MFA) |
| R2 | Admin MFA becomes mandatory outside `local`, so every admin must enrol at their next sign-in | Admins see an enrolment step | Enrolment is self-service at first sign-in; `misectl` can reset a lost authenticator |
| R3 | Existing admin sessions are not carried over | Admins sign in once more after deploy | Sessions last 12 h anyway |
| R4 | The Go toolchain on this machine is incomplete (`~/sdk/go1.27.1` is empty) | Go runs only in Docker here | All Go commands ran in `golang:1.27-alpine` |
| R5 | Customer registration opening publicly is a **business** decision | — | `CUSTOMER_REGISTRATION_ENABLED` defaults to **off** outside `local` |
| R6 | The meeting notes PDF (1 Oct 2026) could not be read here (no PDF tooling) [A: its decisions are reflected in commit `d7730f7`] | Possible missed decision | Confirm nothing in it affects Sprint 1 |
| R7 | The production domain is still the placeholder `mise.tt` | Email links and cookies | Configuration only; unchanged |

## 12. Baseline (before any change)

Run on the untouched clone, Go in `golang:1.27-alpine` (go1.27.4), PostgreSQL 17 in an
isolated compose project (`mise-s1`):

| Check | Result |
|---|---|
| `go vet ./...` | pass |
| `gofmt -l .` | **fail**: `internal/admin/http.go` (D3) |
| `go test ./...` with `TEST_DATABASE_URL` | **27/27 pass** (15 HTTP-level against PostgreSQL) |
| `pnpm lint` | pass |
| `pnpm typecheck` | **fail** on clean checkout (D1) |
| Vitest: marketing 22, admin 6 | **28/28 pass** |
| `pnpm test` (root) | fails because it runs Playwright without a stack (D2) |
| Playwright e2e | not run at baseline |
