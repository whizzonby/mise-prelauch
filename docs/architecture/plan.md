# Mise Pre-Launch Platform: architecture and implementation plan

Status: accepted for Phase 1, and revised after the build so it matches what was built. Source brief: `# MISE PRE-LAUNCH PLATFORM.md` (kept beside the repo).

This document covers the twelve planning items the brief asks for, then reviews the plan
against the brief. Deeper detail lives in the sibling docs (`docs/database`, `docs/api`,
`docs/decisions`).

## 1. Repository architecture

```
mise/
  apps/
    marketing/        Next.js public site (static-first), waitlist, welcome, verify, unsubscribe
    admin/            Next.js internal admin (server-rendered, talks to the API only)
  services/
    api/              Go module: one codebase, three binaries
      cmd/api         HTTP API
      cmd/worker      background job runner (email)
      cmd/misectl     migrations and operator commands (create admin)
      internal/
        platform/     config, httpx, db, jobs, mail, token, ratelimit
        leads/        signup, verification, preferences, unsubscribe, erasure
        referrals/    codes, attribution of referrer, conversion, abuse rules
        events/       first-party analytics ingestion
        notifications/ email templates and job handlers
        admin/        admin auth, sessions, RBAC permissions, stats, export, audit
      migrations/     SQL, embedded into the binaries
  packages/
    ui/               Mise design system: tokens, primitives, motion helpers
    validation/       Zod schemas shared by forms and the API client
    api-client/       typed fetch client for the Mise API
    analytics/        event taxonomy and the browser tracker
    config/           shared tsconfig and eslint config
  e2e/                Playwright end-to-end tests
  infrastructure/
    docker/           Dockerfiles, compose for local dev
    terraform/        AWS (modules + staging/production environments)
    scripts/          dev helpers
  docs/               architecture, api, database, decisions (ADRs)
```

Deviations from the brief's suggested tree, each with a reason:

- `services/worker` is a second binary inside `services/api`, not a second Go module. The
  worker shares every domain package with the API; two modules would mean a third shared
  module and version juggling for no benefit (ADR-002).
- `e2e/` is a top-level workspace because the tests span both apps and the API.

Tooling: pnpm workspaces, no task-runner layer. Packages are consumed as TypeScript source
(`transpilePackages`), so there is no package build step to keep in sync.

## 2. System architecture

```
 Browser ──► marketing (Next.js, static/ISR) ──┐
                                               ├──► Mise API (Go, REST /api/v1) ──► PostgreSQL
 Admin  ──► admin (Next.js, server-rendered) ──┘            │
 Flutter (future) ─────────────────────────────►            └─ jobs table ◄── worker (Go) ──► SMTP (SES / Mailpit)
```

- The API is the system of record. Next.js holds no business rules: forms validate for
  feedback, the API validates for truth.
- Marketing calls the API from the browser (CORS allow-list). Admin calls it server-side
  with a session token held in an httpOnly cookie, so no admin credential ever reaches
  client JavaScript.
- Background work uses a PostgreSQL job table (`FOR UPDATE SKIP LOCKED`). A job is enqueued
  in the same transaction that creates the lead, so an email can never be lost or sent for
  a rolled-back signup. SQS is deferred until volume justifies it (ADR-008).
- No Redis. Rate limiting is in-process per instance, backed by a WAF rate rule in AWS.

## 3. Database schema

Detail and rationale in `docs/database/schema.md`. Tables:

| Table | Purpose |
|---|---|
| `leads` | one row per person; explicit `status` lifecycle; unique `email_canonical`; unique `referral_code` |
| `lead_preferences` | 1:1 with lead; signup and progressive-profiling answers |
| `referrals` | referrer → referred, `pending` / `converted` / `flagged` |
| `lead_attribution` | first-touch and latest-touch UTM, landing page, referrer URL |
| `lead_events` | first-party analytics events (lead or anonymous) |
| `jobs` | transactional background job queue |
| `admin_users`, `admin_sessions` | admin authentication, role per user |
| `audit_log` | admin-sensitive operations (login, export, status change, erasure) |

Lead status: `PENDING → VERIFIED → QUALIFIED → CONVERTED`, plus `UNSUBSCRIBED` and
`BLOCKED`. Stored explicitly with a CHECK constraint, changed only by named service
operations.

## 4. API surface

Envelope: `{ "data": …, "meta": { "request_id" } }` or
`{ "error": { "code", "message", "fields"? }, "meta": { "request_id" } }`.

Public:

| Method | Path | Notes |
|---|---|---|
| GET | `/api/v1/health` | liveness |
| GET | `/api/v1/ready` | readiness (database reachable) |
| POST | `/api/v1/leads` | create lead; naturally idempotent on email |
| POST | `/api/v1/leads/verify` | confirm email with signed token |
| GET | `/api/v1/leads/me` | lead's own summary and referral counts (profile token) |
| PATCH | `/api/v1/leads/preferences` | progressive profiling (profile token) |
| POST | `/api/v1/leads/unsubscribe` | signed token from email |
| GET | `/api/v1/referrals/{code}` | validate a code, return referrer's first name |
| POST | `/api/v1/events` | batch of analytics events |

Admin (session bearer token, permission-checked): `auth/login`, `auth/logout`, `auth/me`,
`stats/overview`, `leads` (list, detail, status change, erasure), `leads/export.csv`.

## 5. Design system specification

Full spec in `docs/architecture/design-system.md`. Summary:

- Palette named for what is on a Trinidadian counter, exposed as semantic tokens:
  dasheen green (primary), chadon beni green (secondary), roucou red-orange (accent),
  coal-pot charcoal (foreground), cassava cream (background), coconut white (surface).
- Type: Young Serif for display, Instrument Sans for text. Sentence case throughout.
- No gradients, no glass, no glow. One shadow token, used only for overlays.
- Radii are small (2–4px), like a printed label. Photography is never rounded.
- Signature: the hero is a literal mise en place. Ingredient photographs sit on a strict
  grid and settle into position on load. That is the one orchestrated moment.

## 6. Site map

Marketing: `/` (hero, supply-chain story, how it works, meals, plans, chefs,
sustainability, FAQ, waitlist), `/join/[code]` (referral landing), `/welcome` (success,
referral panel, optional profiling), `/verify`, `/unsubscribe`, `/privacy`, `/terms`,
`sitemap.xml`, `robots.txt`, Open Graph image.

Admin: `/login`, `/` (dashboard), `/leads`, `/leads/[id]`.

## 7. Component architecture

`packages/ui` holds brand primitives with no data fetching: `MiseButton`, `Container`,
`Section`, `Heading`, `Text`, `MediaFrame`, `Field` controls, `FAQItem`, `ProcessStep`,
`ImageReveal`. Feature components that need data or content (`WaitlistForm`, `MealPreview`,
`ChefFeature`, `IngredientStory`, `ReferralPanel`) live in `apps/marketing` and compose the
primitives. All copy comes from `apps/marketing/src/content/*.ts`, typed, so a CMS can
replace the files without touching components.

## 8. Animation strategy

- Motion tokens: `fast` 150ms, `standard` 300ms, `editorial` 900ms; one easing for
  entrances, one for interactions.
- CSS handles the hero settle, image reveals, the FAQ and the mobile menu. The Motion
  library named in the brief was dropped during the build: CSS covers every case, runs
  before hydration and ships no JavaScript (ADR-007).
- GSAP ScrollTrigger is used once, for the pinned Farm → Dinner sequence, and is loaded
  lazily when that section approaches the viewport on wide screens only.
- Reduced motion: the hero renders settled, the story renders as a static illustrated
  sequence, and nothing is pinned. Small screens get the unpinned layout too, by design.

## 9. Security approach

Detail in `docs/architecture/security.md`. Server-side validation on every input;
parameterised SQL only; strict security headers and CSP; CORS allow-list; per-IP rate
limits; honeypot and minimum-fill-time bot checks; signed, expiring HMAC tokens for
verification, profile access and unsubscribe; Argon2id admin passwords; hashed session
tokens; permission checks per admin route; audit log for exports, status changes and
erasure; no PII in logs (emails are never logged, IPs are stored only as keyed hashes).

## 10. Analytics event taxonomy

Defined once in `packages/analytics` and mirrored by an allow-list in the API:
`page_view`, `section_viewed`, `hero_cta_clicked`, `waitlist_started`,
`waitlist_completed`, `preferences_started`, `preferences_completed`,
`referral_link_copied`, `referral_shared`, `faq_opened`, `meal_viewed`, `plan_viewed`.
Metadata is a small flat object of identifiers; form values are never included. The tracker
honours Global Privacy Control and Do Not Track, and uses no cookies.

## 11. Deployment architecture

AWS, Terraform, no Kubernetes (ADR-006):

- ECS Fargate services: `marketing`, `admin`, `api`, `worker`, behind one ALB with
  host-based routing; CloudFront in front of marketing; WAF on the ALB/CloudFront.
- RDS PostgreSQL (single-AZ in staging, Multi-AZ in production), automated backups.
- SES for email over SMTP; Secrets Manager for secrets; CloudWatch logs, metric filters
  and alarms; Route 53 and ACM.
- Staging and production are separate Terraform environments with separate state,
  networks and databases. Deploys run only from CI (GitHub Actions with OIDC).

## 12. Implementation phases

Milestones 0–7 exactly as in the brief (foundation, marketing, motion, lead platform,
referral engine, lead intelligence, admin, production readiness). Each ends with: run,
test, check responsive/accessibility/performance, update docs, commit.

## Plan review

Unnecessary complexity removed:

- No Redis, no SQS, no task runner, no chart library, no global client state library.
- No separate newsletter module: a waitlist lead with marketing consent is the newsletter
  subscriber. A module is added when there is a second audience.
- Stateless signed tokens instead of a token table.

Missing requirements found and added: unsubscribe endpoint and page, erasure endpoint,
readiness probe, `/join/[code]` landing, audit log, consent version stored with the
consent timestamp.

Security review: duplicate signups return a distinct "already on the list" outcome. That
confirms list membership to whoever submits an address, which is a deliberate, documented
trade-off for a waitlist (low sensitivity, rate limited); no lead data is returned for a
duplicate, the existing lead is emailed instead.

Future-platform review: identity is `leads` now and migrates to `customers` later via
`leads.status = CONVERTED` and a stable lead id; admin roles are data, permissions are a
code map, so new roles do not need schema changes; all rules live behind REST, so Flutter
gets the same behaviour.

## Blocking business decisions

None block the build; all are configuration:

1. Production domain (placeholder `mise.tt`) and sending domain for email.
2. Whether verification (double opt-in) is required. Default: on.
3. Referral rewards. Default: none promised; copy says early access is prioritised by
   list order only if that is later configured.
4. Real photography, chef names, meals and plan pricing. All placeholder content is
   flagged in `src/content`; the stock photographs are listed in `docs/content/image-credits.md`.
5. Legal review of the privacy policy and terms (placeholder text is marked as draft).
