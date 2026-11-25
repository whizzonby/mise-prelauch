# Sprint 9 plan: web launch readiness

Brief: `E:\FULL MISE DEV\SPRINT 9`. Audit: `SPRINT_9_AUDIT.md`. Branch `sprint-9/launch-readiness` (from `7e05db0`).
Authorised 14 November 2026. Hard limits from the brief: no public deployment, no live payments, no invented policies or content, stop
after Sprint 9.

## 1. Critical launch blockers (cannot be closed by engineering)

| Blocker | Owner | Engineering response |
|---|---|---|
| No approved payment provider or sandbox credentials | Business | Keep `testpay` for development and e2e only (already refused in production). Commercial launch reported **blocked**. |
| Legal review: privacy, terms, subscription, cancellation and refund terms | Business/legal | Pages exist, marked draft; launch checklist shows **blocked**. No new binding policy text invented. |
| Real menu: approved recipes, nutrition, photography, plan prices | Culinary/business | Public site reads only published, non-demo meals and active plans with prices from the database. With none configured it shows honest empty states. |
| Delivery zones/windows/capacity/partners/proof policy | Operations | Shown only from configuration; checklist reads the configuration and marks it unverified until signed off. |
| Email provider, production domain, AWS account access | Business/ops | Configuration and runbooks prepared; staging marked **not verified** until it exists. |

## 2. Public website (decision)

Two public hosts already exist: the marketing site (root domain) and the customer app (`app.` host). To avoid a second catalogue:

1. **The customer app's catalogue and plan pages open to visitors.** `/meals`, `/meals/[slug]`, `/plans`, `/plans/[slug]`, `/chefs/*`
   render without a session in a public shell (site header with Sign in / Get started). Signed-in extras (favourites, recommendations,
   "your plan") appear only with a session. Everything else stays behind sign-in. The API endpoints are already public.
2. **Choosing a plan carries through sign-up.** A visitor's `Choose this plan` → `/register?next=/subscribe?plan=…` → after verification
   and onboarding the customer returns to subscribe with that plan.
3. **The marketing site becomes the launch landing page** using live data: featured meals from `GET /meals` (published, non-demo in
   production) and plans from `GET /plans`, cached with ISR. Static `content/meals.ts`/`plans.ts` samples are removed from the page.
   Adds `/how-it-works`, `/faq`, `/about`, `/contact`; `/meal-kits` and `/plans` on the root domain redirect to the customer app pages.
   The waitlist stays available behind a `SITE_MODE` switch (`waitlist` | `launch`), default `waitlist` in production until go-live is
   approved, so deploying the code does not announce a launch.
4. `/about` and `/contact` show only approved facts already in `content/site.ts` (name, contact email, origin); missing business content
   is flagged on the page source and in the launch checklist, never invented.
5. SEO: per-page metadata and canonical URLs, sitemaps on both hosts (customer app sitemap lists only published meals and active plans),
   `robots` disallows account, checkout and admin; `Product`-free structured data (no ratings, no reviews).

## 3. Commerce integration gaps

- **Outbox events** recorded in the same transaction as the state change:
  - `subscriptions.record` → `subscription.{created,activated,pause_scheduled,paused,resumed,cancel_scheduled,cancelled,plan_changed,
    renewed,expired,…}` (one event per real history entry; types come from the existing history types, not new lifecycle states).
  - `payments.apply` → `payment.succeeded`, `payment.failed`, `payment.cancelled`.
  - `payments.Refund` → `refund.succeeded` / `refund.failed` when the refund reaches a final state.
  - `orders.Create` → `order.confirmed`; order cancellation only if an existing transition exists.
  - Dedupe keys from the history row / payment status so retries and duplicate webhooks never duplicate events.
- **Consumers:** analytics refreshes the affected local day; automation's `payment_failed` rule gains an event trigger.
- **Backfill:** `misectl events backfill-commerce` replays history rows into events idempotently (same dedupe keys).
- Tests: transactional publish, duplicate webhook → one event, replay idempotent, backfill idempotent, consumer refresh.

## 4. Analytics and automation corrections

- Move earlier `/reports/procurement` and `/reports/delivery` figures onto registry metrics where equivalent ones exist; keep routes.
- Figure-level reconciliation tests for procurement and delivery dashboards against authoritative rows.
- End-to-end test of the `automation` consumer (event → rule → execution, dedupe, cooldown, disabled rule does nothing).

## 5. Security priorities

1. RBAC matrix test: every role × representative admin endpoints by direct API call; customer cross-account isolation on every `/me/*`
   resource with an id.
2. Session tests: cookie flags, logout invalidates the token server-side, password reset token single-use and expiry, login rate limit.
3. Payment webhook tests: bad signature refused, duplicate ignored, amounts server-side (already partly covered; extend).
4. Scans: `pnpm audit --prod`, `govulncheck`, gitleaks-style secret scan of history, `go vet`/`staticcheck` where runnable.
5. Security headers and CSP review for the three Next apps.
6. Documents: `docs/security/SECURITY_AUDIT.md`, `docs/security/THREAT_MODEL.md`.

## 6. Data integrity priorities

- Fresh-database migration 00001→00013 up, down to 0, up again; upgraded-database test: restore a dump of the current dev database
  into a scratch database and migrate.
- Backup and **restore rehearsal**: `pg_dump -Fc` → restore into a new database → row-count and checksum comparison. Documented with
  results; production backup verification remains a go-live item.
- Constraint audit query: every FK has an index, money columns are integer/numeric, append-only tables have no UPDATE paths.

## 7. UI/UX work

- Public shell for the customer app; consistent header/footer between marketing and customer app; contact/support link in both footers.
- Responsive checks at 320, 375, 390, 430, 768, 1024, 1440 for public pages, checkout and dashboard (automated horizontal-overflow test).
- Accessibility: axe on all public pages and the checkout journey, keyboard-only checkout run, focus and reduced motion; results in
  `docs/testing/ACCESSIBILITY_RESULTS.md`.
- Fix defects found; no visual redesign of working operational screens.

## 8. Infrastructure work

- Add the customer app to `docker-compose.prod.yml`, `Caddyfile` (`app.{$DOMAIN}`, customer API paths), Terraform `compute.tf`/`edge.tf`
  and `deploy.yml`. Validate with `docker compose config`, `terraform validate`/`fmt` (if Terraform is runnable here).
- `GET /api/v1/version` (build version and commit) and a worker heartbeat surfaced in `/ready` detail.
- Local **staging rehearsal**: build the production images and run the full production compose stack locally (`MISE_ENV=staging`),
  smoke-test it. Real AWS staging stays **not verified** (no account access).
- Runbooks: `docs/launch/DEPLOYMENT_RUNBOOK.md`, `ROLLBACK_RUNBOOK.md`, `INCIDENT_RESPONSE.md`, `OPERATIONAL_READINESS.md`.

## 9. Testing strategy

- Go: all packages, plus new integration tests above.
- Vitest: existing + public-catalogue helpers.
- Playwright: new `launch.spec.ts` (public journey, SEO, responsive widths, axe) and `journey.spec.ts` (brief §38 full chain using the
  domain APIs and demo fixtures, never direct status updates); negative paths (§39) where the domain supports them.
- Cross-browser: run the public and customer specs in Firefox and WebKit projects (memory permitting, batched).
- Performance: Lighthouse-style timings via Playwright navigation timing on production builds, API latency with a small Go/Node load
  script (documented concurrency, duration, p50/p95/p99, error rate) against the local stack; `docs/testing/PERFORMANCE_RESULTS.md`.
- `docs/testing/E2E_COVERAGE.md` maps brief §38/§39 steps to tests.

## 10. Release criteria

Engineering complete when every brief §50 item is Verified or explicitly Blocked with a reason. Launch control centre (admin
`/launch`, permission `launch.read`) shows each gate as Configured / Verified / Blocked / Not applicable from evidence: database
configuration reads (plans with prices, published non-demo meals, delivery zones, payment provider live flag, mail driver) plus a
recorded sign-off list. It never shows "Ready to launch" while any critical gate is blocked. `docs/launch/GO_LIVE_CHECKLIST.md` mirrors it.

Expected outcome given current business state: **Engineering complete; commercial launch blocked** (payment provider, legal, menu,
delivery policy, email, domain, AWS).

## Progress

Resume from the first unchecked item.

- [x] Stage 1 — Audit (`SPRINT_9_AUDIT.md`) and plan (this file)
- [x] Stage 2 — Customer website & commerce
  - [x] 2a Public catalogue/plans in the customer app (public shell, signed-in extras)
  - [x] 2b Plan choice carried through sign-up to subscribe
  - [x] 2c Marketing landing on live data, `SITE_MODE`, new pages, redirects
  - [x] 2d SEO: metadata, canonicals, sitemaps, robots (verify in browser with 2e)
  - [x] 2e Tests (Vitest, Playwright launch.spec) green — 25 Nov: Vitest 45, typecheck+lint clean, launch.spec 8/8, waitlist-mode regression 66/66 (experience.spec heading updated to the live-data section title)
- [x] Stage 3 — Integration corrections
  - [x] 3a Commerce outbox events + tests
  - [x] 3b Consumers (analytics refresh, automation payment_failed event trigger) + backfill command
  - [x] 3c Reports onto registry; procurement/delivery reconciliation tests; automation consumer e2e test
- [x] Stage 4 — Security & reliability
  - [x] 4a RBAC matrix + ownership + session tests
  - [x] 4b Scans (deps, vuln, secrets), headers/CSP
  - [x] 4c Migrations fresh/upgraded; backup + restore rehearsal
  - [x] 4d Version endpoint, worker heartbeat, system status, launch gates API + admin pages (security docs written in Stage 8)
- [x] Stage 5 — UI/UX refinement (responsive widths, a11y, fixes) — 25 Nov: responsive.spec 4/4 (keyboard checkout, 7 widths signed-in, admin tablet/phone, reduced motion); 3 overflow defects fixed; docs/testing/ACCESSIBILITY_RESULTS.md
- [x] Stage 6 — Production infrastructure (web app in compose/Caddy/Terraform/deploy; local staging rehearsal; runbooks) — 25 Nov: terraform fmt+validate ok; ecs-deploy waits for web; compose passes VERSION/COMMIT; local prod-compose rehearsal (no Caddy/TLS) docs/testing/STAGING_REHEARSAL.md; marketing build-time data defect fixed; docs/launch runbooks; SECURITY_AUDIT + THREAT_MODEL; job-failure test; supply report test made date-independent
- [x] Stage 7 — End-to-end verification (journey.spec, negative paths, cross-browser, performance, load) — 25 Nov: Chromium 71/71; cross-browser per report §21; perf/load/concurrency in docs/testing/PERFORMANCE_RESULTS.md (2 High concurrency defects fixed); E2E_COVERAGE.md
- [x] Stage 8 — Release assessment (launch control centre, GO_LIVE_CHECKLIST, report, screenshots, commit) — 25 Nov: docs/launch/GO_LIVE_CHECKLIST.md, docs/SPRINT_9_REPORT.md, docs/screenshots/sprint-9/visual
