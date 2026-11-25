# Sprint 9 audit: web launch readiness

Audited 14 November 2026 on `sprint-9/launch-readiness` (cut from Sprint 8 tip `7e05db0`, which contains the Sprint 7 review fix
`8edeac0`). Brief: `E:\FULL MISE DEV\SPRINT 9`. The repository is authoritative; **Repo differs** marks a disagreement with a report.

Severity: **Critical** blocks a safe or truthful launch; **High** blocks a complete customer or operational journey; **Medium** is a
correctness, consistency or verification gap; **Low** is polish or debt.

## 1. What exists (confirmed in code, not just in reports)

| Area | Where | State |
|---|---|---|
| Public marketing site | `apps/marketing` (`/`, `/privacy`, `/terms`, `/credits`, waitlist `/join`, `/verify`, `/welcome`, `/unsubscribe`, `sitemap.ts`, `robots.ts`) | Pre-launch waitlist site. Meals and plans come from **static files** (`content/meals.ts`, `content/plans.ts`), labelled "SAMPLE MENU". |
| Customer app | `apps/web` (catalogue `/meals`, `/meals/[slug]`, `/chefs`, `/plans`, `/plans/[slug]`, `/subscribe` → `/review` → `/result`, `/menu/[week]/select`, `/orders`, `/billing`, `/subscription/*`, `/profile/*`, auth pages) | Complete signed-in journey. **Every catalogue and plan page is behind sign-in** (`proxy.ts` matcher, `(app)/layout.tsx` `requireCustomer`). `/` redirects to sign-in. |
| Public read APIs | `GET /api/v1/meals`, `/meals/{slug}`, `/meal-categories`, `/chefs`, `/reference`, `/plans`, `/plans/{slug}`, `/weekly-menus`, `/subscription-info` | Unauthenticated already. The public website can use them without backend change. |
| Commerce | `commerce` (Quote, Subscribe, Pay, CheckoutResult, ChangePlan, Pause, Resume, Cancel, SetAddress, selection, scheduler Tick), `payments` (provider-agnostic, `testpay` adapter, webhooks with dedupe, reconcile, refunds), `billing` (server amounts in integer cents, tax modes, invoices), `orders` | Verified in Sprint 3; server-authoritative pricing, idempotent charges/refunds, webhook-only payment confirmation. |
| Admin/CRM, production, supply chain, delivery, analytics/automation/forecasting | `apps/admin` (≈120 pages), Go modules per domain | Built and tested in Sprints 4–8. |
| Outbox | `platform/outbox.Record` (dedupe on `(type, aggregate_id, dedupe_key)`), `platform/consumer` (offsets per consumer: `analytics`, `automation`, delivery/procurement consumers) | Used by culinary, production, suppliers, procurement, inventory, fulfillment, delivery. |
| Email | `notifications` (account, commerce, delivery templates, `layout.html`), `platform/mail` (`smtp`/`log` driver), Mailpit locally | Templates exist for verification, password reset, email change, commerce notices, delivery. SES configured only in Terraform, never exercised. |
| Infrastructure | `docker-compose.prod.yml` + `Caddyfile` (single server), Terraform `modules/platform` (ECS, RDS, ALB, CloudFront, SES, Secrets Manager), `.github/workflows/ci.yml`, `deploy.yml` | Built for the **pre-launch** product only (see §5). Never applied or run against AWS. |
| Tests | Go 16 packages (integration against PostgreSQL), Vitest 43, Playwright 13 specs (Chromium only) | Passing at end of Sprint 8. |

## 2. Missing customer-facing workflows

| # | Finding | Severity |
|---|---|---|
| C1 | **No public catalogue.** A visitor cannot browse meal kits, open a meal or see plans without an account. The brief's primary journey starts at "browse" before "create an account". | Critical |
| C2 | **Marketing site shows hardcoded sample meals and draft plan names** (`Basic`, `Fitness`, `School Lunch`) that are not the configured plans in the database. Brief §5, §8: "use configured business values". | Critical |
| C3 | No `/how-it-works`, `/about`, `/faq`, `/contact` pages. Home has how-it-works and FAQ sections; no standalone pages and no contact page. | High |
| C4 | Visitor → plan → sign up handoff: after registering, the customer lands on onboarding/dashboard, not back on the plan they chose. | High |
| C5 | Web app `/` redirects visitors to sign-in (dead end for anyone arriving from a link). | Medium |
| C7 | **Customer support is partial.** A customer can raise a support request about an order's delivery (`POST /me/orders/{id}/delivery/support`, Sprint 7); anything else goes by email to the contact address. General self-service cases are a product decision. | Medium |
| C6 | No subscription/cancellation/refund terms page; terms are a waitlist draft. | Critical (content, blocker) |

## 3. Broken or incomplete integrations

| # | Finding | Severity |
|---|---|---|
| I1 | **Commerce emits no outbox events** (payments, subscriptions, orders, refunds). Analytics/automation rely on the hourly tick; `payment_failed` rule cannot react to events. Clean hook points exist: `subscriptions.record` (every status change and note), `payments.apply` (every payment transition), `payments.Refund`, `orders.Create`. | High |
| I2 | Earlier `/reports/*` pages (procurement, delivery, inventory, production, suppliers) compute figures independently of the Sprint 8 metric registry. | Medium |
| I3 | No figure-level reconciliation test for procurement and delivery dashboards. | Medium |
| I4 | The `automation` event consumer path and `misectl analytics backfill` are not covered end to end. | Medium |
| I5 | Demo data guards hold: `catalog`/`commerce seed-demo` need `CATALOG_DEMO_ENABLED` (default on only when `MISE_ENV=local`); `production`/`delivery seed-demo` refuse outside `local`; demo meals are hidden from customers when the flag is off. **Gap:** no test asserts the production configuration hides demo meals from the public catalogue and sitemap. | Low |

## 4. Security risks

| # | Finding | Severity |
|---|---|---|
| S1 | No real payment provider; `testpay` refused in production (good). Commercial launch blocked until a provider is approved (§11 of brief). | Critical (business blocker) |
| S2 | Session cookies: customer `httpOnly`, `Secure` unless `WEB_COOKIE_SECURE=false`, `SameSite=Lax`; admin `SameSite=Strict`. API uses bearer tokens via BFF, so classic CSRF does not reach the API; Next server actions check Origin. **Needs a test**, not a change. | Medium |
| S3 | RBAC tested per sprint, but no single matrix test across all roles (customer, support, finance, admin, super admin, production, procurement, inventory, dispatch, courier, analyst) hitting admin APIs directly. | High |
| S4 | No dependency scan, secret scan or `govulncheck` evidence in this repo's history (CI has the steps, CI has never run on GitHub). | High |
| S5 | Rate limits are in-process per API instance. Acceptable for one instance; document for scale-out. | Low |
| S6 | Security headers/CSP on the Next apps not audited since Sprint 1. | Medium |
| S7 | **Found by the Sprint 9 ownership test:** deleting another customer's address returned 409 "address in use" instead of 404, confirming the id exists and is delivered to. **Fixed** (ownership checked first). | Medium |

## 5. Operational and deployment blockers

| # | Finding | Severity |
|---|---|---|
| D1 | **The customer app is not deployable.** `docker-compose.prod.yml`, `Caddyfile`, Terraform (`compute.tf` services `api, worker, marketing, admin`) and `deploy.yml` omit `apps/web`. Caddy exposes only waitlist API paths publicly. | Critical |
| D2 | Backups: RDS retention in Terraform, single-server doc says "nightly pg_dump"; **no restore has ever been tested**. | Critical |
| D3 | Terraform never planned/applied; GitHub Actions never run. Staging does not exist. | Critical (cannot be resolved without AWS access and approval) |
| D4 | No production email provider credentials; SES never exercised. | High (business/credential blocker) |
| D5 | No version endpoint; health/ready exist; worker liveness and consumer lag are visible only in admin pages. | Medium |
| D6 | `gofmt` flags 15 older files → CI `gofmt -l` step would fail. **Fixed** (tree formatted). | Medium |
| D7 | **sqlc drift:** packages generated before Sprints 5–8 lacked the newer tables' models and columns, so CI's `sqlc diff` would fail. **Fixed** (regenerated; only additive model types and extra scanned columns). | Medium |
| D8 | Old `/reports/delivery` defined "delivered" and "on time" differently from the Sprint 8 registry (counted disputed deliveries as delivered; counted deliveries without a promised window as on time). **Fixed**: shared figures now come from the registry; breakdowns use the same definitions; a figure-level test reconciles report and dashboard. | High |

## 6. Design inconsistencies

| # | Finding | Severity |
|---|---|---|
| U1 | Marketing and customer apps share `@mise/ui` tokens but have different headers/footers; once visitors cross from site to catalogue the shell changes. A public shell for the web app is needed. | High |
| U2 | Customer app footer links to marketing legal pages by absolute URL; fine, but there is no contact/support link. | Medium |
| U3 | Admin uses its own shell and tables; consistent within itself. No change needed beyond defects found by checks. | Low |

## 7. Performance risks

| # | Finding | Severity |
|---|---|---|
| P1 | Nothing has been measured (no Lighthouse, no load test, no API latency distribution). | High (evidence gap) |
| P2 | Catalogue pages are dynamic (cookie-dependent); public pages should cache API reads (`revalidate`) so a visitor surge does not hit PostgreSQL per request. | Medium |
| P3 | The test PC is memory-constrained; full Playwright runs must be batched. Load tests must be modest and documented as such. | Low (environment) |

## 8. Test coverage gaps

| # | Finding | Severity |
|---|---|---|
| T1 | Playwright runs Chromium only; Firefox and WebKit never run. | High |
| T2 | No single end-to-end test of the whole chain landing → catalogue → subscribe → pay → order → production → fulfillment → delivery → proof → customer view → analytics (brief §38). Pieces exist across specs. | High |
| T3 | No migration test on an upgraded database beyond up/down/up. | Medium |
| T4 | Responsive checks only at 390px and 1366/1440px. | Medium |
| T5 | Accessibility: axe on selected screens; no keyboard-only checkout pass documented; no screen-reader pass. | Medium |

## 9. Launch-critical dependencies (outside engineering)

Approved payment provider and sandbox credentials; production domain; email provider; legal review of privacy, terms, subscription and
refund terms; real meal photography and approved recipes/nutrition; plan prices; delivery zones/windows/capacity/partners; proof-of-delivery
policy; cold-chain policy; support contacts; tax and delivery-fee decisions (Sprint 3); revenue recognition, automation thresholds, report
recipients, forecast criteria (Sprint 8 §25); AWS account access for staging.

## 10. Implementation and verification plan

See `SPRINT_9_PLAN.md` (stages, scope decisions and the Progress checklist used to resume after interruptions).
