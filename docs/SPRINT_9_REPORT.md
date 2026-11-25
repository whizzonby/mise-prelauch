# Sprint 9 report: web launch readiness

Branch `sprint-9/launch-readiness` (from Sprint 8 tip `7e05db0`). Brief: `E:\FULL MISE DEV\SPRINT 9`. Audit and plan:
`docs/architecture/SPRINT_9_AUDIT.md`, `SPRINT_9_PLAN.md`. Work ran 14 and 25 November 2026, interrupted by a power loss (nothing lost;
local checkpoint commits were made after it). Commit hash: see §29.

## Outcome

| Milestone | Status |
|---|---|
| **Engineering complete** | **Yes**, with the open items in §25 explicitly listed. Every brief §50 item is Verified or Blocked with a reason. |
| **Staging ready** | **No.** The production compose stack ran locally with production settings (`docs/testing/STAGING_REHEARSAL.md`), but no AWS staging exists and TLS was not tested. |
| **Operationally ready** | **No.** Payment provider, menu, prices, delivery policy, email provider, legal review, support contacts and incident contacts are business decisions not yet made. |
| **Launch ready** | **No.** The launch control centre reports "Not ready to launch: 25 critical gate(s) are not verified" (§26). |
| **Publicly launched** | **No.** Nothing was deployed; live payments are not enabled. |

**Engineering complete; commercial launch blocked.**

Status words in this report: **Verified** (test or measurement evidence named), **Implemented, not verified**, **Blocked** (needs
someone outside engineering), **Deferred**.

## 1. Repository audit findings

`SPRINT_9_AUDIT.md` recorded 40 findings. The launch-critical ones and their resolution:

| Finding | Severity | Resolution |
|---|---|---|
| C1 No public catalogue (meals and plans behind sign-in) | Critical | Verified fixed (§3, §4) |
| C2 Marketing site showed hard-coded sample meals and draft plans | Critical | Verified fixed: live data only (§3) |
| D1 Customer app not deployable (missing from compose, Caddy, Terraform, deploy) | Critical | Implemented; compose rehearsed locally; Terraform validates; AWS not verified (§16) |
| D2 No restore ever tested | Critical | Verified locally (§15); production restore Blocked |
| I1 Commerce emitted no outbox events | High | Verified fixed (§10) |
| S3 No RBAC matrix across roles | High | Verified (§13) |
| S4 No scan evidence | High | Verified: scans run (§12) |
| T1 Chromium only | High | Verified: Firefox, WebKit, iPhone-sized WebKit (§21) |
| T2 No whole-chain journey | High | Verified in two halves (§22) |
| P1 Nothing measured | High | Verified: page timings, load, concurrency (§20) |
| D8 Old delivery report defined figures differently | High | Fixed in Stage 3, reconciliation test |
| S7 Cross-customer address delete leaked existence (409 not 404) | Medium | Fixed, tested |
| D6/D7 gofmt and sqlc drift would fail CI | Medium | Fixed |

Found and fixed during Sprint 9 verification (not in the original audit):

| Defect | Severity | Found by | Fix |
|---|---|---|---|
| Simultaneous checkouts or meal confirmations hung forever (DB pool deadlock: a second pool connection taken inside a transaction holding row locks) | High | `e2e/tools/concurrency.mjs` | Coverage checked through the caller's transaction; selection page read after commit; scan of every transaction closure |
| A replaced checkout (second tab, double click) could still take money: provider session stayed open, and `MarkDue` outside a transaction could overwrite "void" with "due" | High | concurrency tool + database inspection | Provider sessions closed by a queued job (`CheckoutCanceller`), `MarkDue` in a transaction, `StartCheckout` locks the bill and refuses an unpayable one; test `TestReplacedCheckoutCannotBePaidAtTheProvider` |
| Marketing image could not be built, and would have shown "menu could not be loaded" after every deploy (live data read at build time) | High | local staging rehearsal | Live-data pages render per request; API reads cached 5 min with a 4 s timeout |
| Dead end: "Add an address" during checkout left the customer on the address page | Medium | `journey.spec.ts` design | Safe `next` return link back to the plan |
| Profile pages and two admin pages scrolled sideways on phones | Medium | `responsive.spec.ts` | `minmax(0,1fr)` grid tracks |
| `ecs-deploy.sh` did not wait for the customer app to be stable | Medium | review | Added `web` to the wait list |
| Single-server compose did not stamp release version/commit | Low | review | `RELEASE_VERSION`/`RELEASE_COMMIT` build args |
| Supply report test depended on the calendar date | Low | full Go run | Dates relative to the run day |

## 2. Architecture and integration changes

- **No new services or frameworks.** One migration, `00014_launch_operations.sql` (`worker_heartbeats`; append-only `launch_signoffs`).
- New Go package `launch` (gates, sign-offs, system status) and `platform/buildinfo` (`GET /api/v1/version`).
- Commerce outbox events and consumers (§10). `misectl events backfill-commerce`.
- Payments: optional `CheckoutCanceller` provider interface and `payments.cancel_checkout` job (§6).
- Customer app route groups: `(store)` public shell, `(app)` signed-in.

## 3. Public website

**Verified.** Decision (plan §2): the customer app hosts the one catalogue; the marketing site is the landing page and links into it.

- Marketing `/` reads featured meals (`GET /meals`) and plans (`GET /plans`) live; static sample files deleted. Honest empty and error
  states ("The first menu is being written…", "Plans are being finalised…"), verified in the production rehearsal with an empty database.
- New pages `/how-it-works`, `/faq`, `/about`, `/contact`; redirects `/meal-kits`, `/meal-kits/:slug`, `/plans`, `/login`, `/signup`,
  `/checkout` → the customer app.
- `SITE_MODE` (`waitlist` default in production, `launch` after approval) switches the calls to action, so deploying does not announce a
  launch. Both modes tested (`launch.spec.ts`, `waitlist.spec.ts`, `experience.spec.ts`).
- No testimonials, ratings, statistics or invented business facts. About and contact show only approved facts (§24).

## 4. Meal-kit catalogue

**Verified** (`launch.spec.ts`, `journey.spec.ts`, Go `TestCatalogShowsOnlyPublishedMeals`). Visitors browse `/meals` with search and
filters, without an account; only published meals appear (demo meals only when `CATALOG_DEMO_ENABLED`, which production refuses);
loading, empty and error states (`(store)/loading.tsx`, `error.tsx`). Signed-in extras (favourites, "your plan") only with a session.

## 5. Meal detail

**Verified.** Photograph, description, servings, cooking time, ingredients, declared allergens (or "not yet confirmed"), nutrition only
when approved, plan eligibility; no costing or supplier data. Accessibility: axe clean in all browsers tested.

## 6. Subscription and checkout

**Verified with the test provider.** Plans from the database with server prices; plan choice survives registration and email
confirmation (`launch.spec.ts`); address added mid-checkout returns to the plan (`journey.spec.ts`); review shows every amount and the
renewal terms before payment; the result page waits for the webhook-confirmed payment; never marked paid from a redirect.
Concurrency hardened (§1, §20). Tax and delivery fee are zero by configuration pending a decision.

## 7. Payment integration

**Blocked.** No payment provider has been approved and no sandbox credentials exist (brief §11: "do not invent a provider
selection"). The provider-agnostic `payments` module and the `testpay` simulator remain; production refuses the simulator (verified in
the rehearsal). Launch gate `payment_provider` is Blocked. When a provider is chosen: implement its adapter (checkout, webhooks,
refunds, `CheckoutCanceller`), test against its sandbox, then request approval for live keys.

## 8. Customer dashboard

**Verified** (customer-platform, subscription-commerce, journey, responsive specs): overview, orders, subscription and its management,
meal selection, billing and invoices (PDF), addresses, profile, preferences, household, notifications, security (sessions, sign out
everywhere), at seven widths, in Chromium, Firefox and desktop WebKit, and on iPhone-sized WebKit except two desktop-only tests (§21). No internal production or supplier data shown.

## 9. Admin and operational workflows

**Verified by the existing sprint suites, all re-run** (admin, admin-crm, chef-production, supply-chain, delivery-fulfillment,
intelligence; Go integration tests). New: admin **Launch** (`/launch`, gates, evidence, sign-offs) and **System** (`/system`: version,
worker heartbeat, outbox backlog, consumer lag, failed jobs/emails/webhooks). Every admin page checked at 390 and 768 px.

## 10. Commerce domain events

**Verified** (Go `TestCommerceEventsArePublishedOnceWithTheChange` and related). Recorded in the same transaction as the state
change, from real transitions only: `order.confirmed`, `order.status_changed`; `payment.succeeded`, `payment.failed`,
`payment.cancelled`; `refund.requested`, `refund.succeeded`, `refund.failed`; `subscription.created`, `activated`, `renewed`,
`renewal_failed`, `payment_recovered`, `week_unpaid`, `plan_changed`, `pause_scheduled`, `paused`, `pause_ended`, `resumed`,
`cancel_scheduled`, `cancellation_withdrawn`, `cancelled`, `expired`. Dedupe keys make retries and duplicate webhooks publish once.
Backfill rehearsed on a restored copy: 978 events added, re-run adds 0.

## 11. Analytics and automation corrections

**Verified.** Analytics consumer refreshes the affected day on commerce events; the `payment_failed` automation rule reacts to the event
(`TestPaymentFailedRuleReactsToTheEvent`); earlier `/reports/*` figures moved onto the metric registry where equivalent, with
figure-level reconciliation tests for procurement and delivery. Consumer failure handling tested (`TestFailingEmailAndConsumerJobsRetryThenStayVisible`).
Forecasting readiness gates unchanged (not weakened).

## 12. Security audit

**Verified scans; open findings listed.** `docs/security/SECURITY_AUDIT.md`, `THREAT_MODEL.md`.

| Scan | Result |
|---|---|
| `go vet ./...` | clean |
| `govulncheck ./...` | 0 reachable; 1 unreachable advisory (GO-2026-5932, `x/crypto/openpgp`, not imported) |
| `pnpm audit --prod` | no known vulnerabilities |
| `gitleaks git` (36 commits) | 4 findings, all false positives (test keys, a `curl -fsS`) |

Open: **F1 High** — on AWS the API host forwards customer and admin API paths to the internet (every route still authenticated,
permissioned, MFA for staff; single-server Caddy already restricts). Fix needs AWS to test. F2 breached-password check, F3 CAPTCHA before
public registration (Medium). No PCI DSS or other compliance is claimed.

## 13. RBAC and session testing

**Verified.** `TestEveryRoleReachesExactlyTheAdminEndpointsItIsGranted` (every role × admin endpoints by direct API call),
`TestCustomersCannotReachEachOthersRecords` (every id-bearing `/me/*` route), session expiry, idle timeout, revocation, audience separation,
password reset ends sessions, staff MFA lockout; production refuses API access before MFA (rehearsal: `403 mfa_required`).

## 14. Database integrity

**Verified locally.** Migrations up/down/up on a fresh database and on a restored copy of the development database (all sprints'
data), now at version 14; down sections fixed where an open invitation blocked rollback. Money in integer minor units; append-only tables
(audit, ledger, sign-offs, receipts) refuse updates (a receipt-update attempt during this sprint was refused by the database).
`docs/testing/MIGRATION_AND_RESTORE_RESULTS.md`.

## 15. Backup and recovery

**Verified locally; production Blocked.** `pg_dump -Fc` → restore → identical row counts across 197 tables and identical financial and
ledger checksums. Production: RDS encrypted automated backups with point-in-time recovery are defined in Terraform but not provisioned;
retention and recovery objectives need business approval (`OPERATIONAL_READINESS.md` §3).

## 16. Infrastructure and staging

- Customer app added to `docker-compose.prod.yml`, `Caddyfile`, Terraform (`compute.tf`, `edge.tf`, outputs), `deploy.yml`, `ecs-deploy.sh`.
- `terraform fmt -check` and `terraform validate`: pass (Terraform 1.9, providers downloaded; no plan or apply — no AWS access).
- `docker compose config`: valid. **Local production rehearsal**: images built, migrations, health/ready/version, worker heartbeat,
  pages, security headers, production refusals (test payments, log mailer, `migrate down`, demo seed), first admin + MFA enforcement —
  all verified. **TLS not tested** (running the Caddy profile on ports 80/443 was not permitted on this workstation).
- Runbooks: `docs/launch/DEPLOYMENT_RUNBOOK.md`, `ROLLBACK_RUNBOOK.md`, `INCIDENT_RESPONSE.md`, `OPERATIONAL_READINESS.md`.

## 17. UI/UX improvements

Public shell for the customer app (header and footer consistent with the site, contact link), plan-to-checkout handoff, address return
link, responsive fixes on profile and admin pages, Chromium-only evidence screenshots. No redesign of working operational screens.
Screenshots: `docs/screenshots/sprint-9/` and `docs/screenshots/sprint-9/visual/` (desktop 1440 and phone 390). They show the local
development database (clearly labelled demo meals and plans), and dates shown inside them come from this PC's clock, which ran about five
days slow (20 November instead of 25 November). Screenshots are evidence of layout only.

## 18. Accessibility

**Verified (automated) / not verified (manual).** `docs/testing/ACCESSIBILITY_RESULTS.md`: axe-core 0 violations (WCAG 2.0/2.1/2.2 A
and AA tags) on public, account, customer and commerce pages; keyboard-only checkout with visible focus; reduced motion; seven widths.
WebKit cannot Tab to links (platform setting), so the two link-keyboard tests are skipped there. **No screen-reader or real-device
audit; WCAG 2.2 AA compliance is not claimed.**

## 19. SEO

**Verified** (`launch.spec.ts`, `experience.spec.ts`): unique titles and descriptions, canonical URLs, Open Graph, `FAQPage` and
`Organization` structured data without ratings or reviews, sitemaps on both hosts listing only published meals and active plans,
`robots` disallowing account, checkout and admin; private pages `noindex`.

## 20. Performance and load testing

**Verified locally** (`docs/testing/PERFORMANCE_RESULTS.md`). Cold-load medians on production builds: landing LCP 284 ms, catalogue 156
ms, meal detail 148 ms, checkout review 140 ms, dashboard 176 ms. Catalogue API ~1,300 req/s with 0 errors (p95 25 ms at 20 users, 53 ms at
50); server-rendered pages ~70–78 req/s at 10 users (p95 ≈ 215–236 ms); admin analytics 168 req/s (p95 59 ms). Concurrency: one live
subscription, one payment and one invoice under simultaneous checkouts and payment callbacks; meal confirmations within the allowance.
**Not measured:** production hardware, mobile networks, production data volumes.

## 21. Cross-browser results

See §27 for the commands. Chromium: full suite. Firefox, desktop WebKit and iPhone-sized WebKit: customer-facing specs (accounts,
customer platform, subscription commerce, launch, responsive, journey).

| Engine (Playwright 1.63) | Passed | Failed | Skipped | Notes |
|---|---|---|---|---|
| Chromium (full suite, 13 specs) | 71 | 0 | 0 | three batches, `--workers=1` |
| Firefox | 32 | 0 | 0 | 30 in the combined run; the 2 others failed only because a second Playwright run deleted their trace files mid-run, and passed when rerun alone |
| WebKit, desktop | 30 | 0 | 2 | skipped: the two Tab-to-link keyboard tests (WebKit does not Tab to links) |
| WebKit, iPhone 13 size | 28 | 2 | 2 | failed: two older tests that click desktop-only controls (the desktop "Main" navigation, the desktop "Your box" sidebar); the same specs' phone tests passed on this engine. Test scope, not app defects. Skipped: as desktop WebKit. |

Test fixes made for other engines (none were app defects): screenshots taken only in Chromium (WebKit's 32,767 px limit on tall phone
pages); axe waits for entrance animations to finish (WebKit measured contrast mid fade-in); meal confirmation uses whichever control the
layout shows (phone action bar or box summary). **Not done:** real Safari, real iOS/Android devices.

## 22. End-to-end test results

**Verified.** `docs/testing/E2E_COVERAGE.md` maps every brief §38 step and §39 negative path to a passing test. The full chain runs in two
halves because orders exist only after a week's cutoff: `journey.spec.ts` (one visitor: landing → catalogue → meal → plan → register →
confirm email → address → review → sandbox payment → webhook-confirmed → meals → dashboard → subscription → staff view of payment and
history) and the production/delivery specs over orders placed through the same API with the clock moved (production → fulfillment →
dispatch → proof of delivery → customer sees "Delivered").

## 23. Production readiness

Ready in code and procedure; not provisioned. See the go-live checklist (§26) for each item.

## 24. Business configuration blockers

Needed from the business, none invented by engineering: payment provider and merchant account; plan prices; tax and delivery-fee
decisions; approved recipes, nutrition and allergen sign-off; licensed meal photography; delivery zones, windows, capacity, partner
agreements, proof-of-delivery policy; cold-chain requirements; production schedule; real suppliers and opening stock; support email,
hours and response promise; legal review of privacy, terms, subscription, cancellation and refund terms; business address and
other about-page facts; production domain; email provider; AWS account; incident contacts; backup retention and recovery objectives;
Sprint 8 §25 items (revenue recognition, automation thresholds, report recipients, forecast criteria).

## 25. Known limitations

- Payment: simulator only (Blocked). Email: Mailpit only. AWS: nothing provisioned; TLS untested.
- Security F1 (High), F2, F3 open (§12). Error pages do not show the request id.
- Residual payment window: a replaced checkout is closed at the provider by a job within seconds; a payment in that window is flagged
  on the void bill for a staff refund (never kept silently).
- The §38 chain runs in two halves (different customers).
- Accessibility: automated and keyboard only. Cross-browser: no real Safari or devices. Performance: one laptop.
- Admin detail pages (`/…/[id]`) were not part of the width sweep.
- Rate limits are per process (documented).

## 26. Launch checklist

`docs/launch/GO_LIVE_CHECKLIST.md` (30 items with Pass / Partial / Blocker; no Fail). Live view: admin `/launch` — at the end of the
sprint against the development database: **not ready, 25 critical gates not verified**; it never shows "ready" while a critical gate is
open (`TestLaunchGatesAreEvidenceBackedAndSignedOff`, `launch.spec.ts`).

## 27. Test commands and results

Run 25 November 2026 on the final tree.

| Suite | Command | Result |
|---|---|---|
| Go (integration on PostgreSQL) | `go test ./... -count=1 -p 1` (golang:1.27-alpine, `TEST_DATABASE_URL`) | 16 packages ok; 170 test functions |
| Go static | `go vet ./...`; `govulncheck ./...` | clean; 0 reachable |
| TypeScript unit | `pnpm test` | Vitest 45 (admin 7, web 15, marketing 23) |
| Typecheck / lint | `pnpm typecheck`; `pnpm lint` | clean |
| API contract | Go `TestRoutesMatchOpenAPI` (every route in `docs/api/openapi.yaml` and vice versa) | pass |
| Playwright, Chromium | `npx playwright test <specs> --project=chromium --workers=1` in 3 batches | 71 passed (13 specs) |
| Playwright, cross-browser | `E2E_ALL_BROWSERS=1 npx playwright test journey launch responsive accounts customer-platform subscription-commerce --project=firefox --project=webkit --project=mobile-webkit --workers=1` | see §21 |
| Performance | `E2E_PERF=1 npx playwright test performance --workers=1` | §20 |
| Load | `node e2e/tools/load.mjs …` | §20 |
| Concurrency | `node e2e/tools/concurrency.mjs` | 3/3 scenarios ok, 4 consecutive runs |
| Migrations / restore | `misectl migrate up/down`, `pg_dump`/`pg_restore` | §14, §15 |
| Staging rehearsal | `docker compose -p mise-staging -f docker-compose.prod.yml --env-file .env.production up -d` | §16 |
| Terraform | `terraform fmt -check -recursive`; `terraform validate` | pass |
| Security | `pnpm audit --prod`; `gitleaks git` | §12 |
| Payment sandbox | — | **Not verified** (no provider) |
| Email integration | Mailpit (account, password reset, commerce) | Verified locally; real provider **not verified** |

## 28. Sprint 10 readiness

The web platform is usable on mobile browsers (seven widths, iPhone-sized WebKit) and its APIs are the contract Flutter will use. Before
Sprint 10: decide mobile authentication (token flow for native apps was deliberately not built), and resolve F1 because native apps will
call the API host directly. **Sprint 10 has not been started and needs explicit authorization.**

## 29. Commit

Sprint 9 is committed as `37b700d` on `sprint-9/launch-readiness` (on top of local checkpoint commits `f0886f4`, `694b82d`,
`02d5388`, `2b6c35e`), followed by this hash note. **Not pushed**: pushing needs your go-ahead.
