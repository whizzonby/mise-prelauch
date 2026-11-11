# Sprint 3 report: subscriptions, ordering and payments

Written 11 November 2026. The work is on branch `sprint-3/subscription-commerce`, built
on Sprint 2 (`b34c80c`), and is **not yet committed**.
- The plan is in `docs/architecture/SPRINT_3_PLAN.md`.
- The decision record is ADR-007.
- The schema is in `docs/database/SPRINT_3_SCHEMA.md`.

Legend: **Verified** = built and covered by tests that pass. **Implemented, not verified**
= built but not exercised by an automated test or a person. **Blocked** = waiting on a
business decision or external input. **Deferred** = left for a later sprint on purpose.

## 1. Architecture summary

- Seven new Go modules in the modular monolith; no new framework, database or Go
  dependency:
  - `plans`
  - `menus`
  - `subscriptions`
  - `billing`
  - `payments` (with `payments/testpay`)
  - `orders`
  - `commerce`
- Every lifecycle is separate: plan price, subscription, cycle (one week), billing cycle,
  invoice, payment and order. A subscription is not an order, and a payment is not either.
- `commerce` runs the cross-module workflows. Each runs in one database transaction, with
  emails and renewal charges queued in the same transaction through the existing job
  table (outbox).
- **Scheduler:** the worker runs it every minute under a PostgreSQL advisory lock. It
  publishes scheduled menus, expires stale checkouts and unpaid sign-ups, creates the next
  week, bills renewals, retries, applies cutoffs and makes orders, and sends reminders.
  Every step is idempotent.
- **Payments:** a provider-agnostic `Provider` interface. The only implementation is
  `testpay`, a simulator that is refused in production.

## 2. Subscription features

| Feature | Status |
|---|---|
| Database-driven plans with immutable price versions; subscribers keep their price | Verified |
| Plan comparison, plan pages; non-self-serve plans show "talk to us" | Verified |
| Subscribe: address, code, review with server-computed totals and renewal terms, hosted checkout, result page that polls the server | Verified (Go and Playwright) |
| Activation only after the provider confirms (webhook, then status lookup) | Verified |
| State machine: 8 statuses with legal transitions; every change recorded and audited | Verified (unit and integration) |
| Change plan from the first week not yet billed (no proration) | Verified |
| Pause for 1 to 4 weeks from the first unpaid week; resume early | Verified |
| Cancel from the first unpaid week; paid weeks still delivered; withdraw before it takes effect | Verified |
| Change delivery address; an address in use cannot be deleted | Verified |
| Weekly cycles: unique per week, created once, plan snapshot per week | Verified |
| First week moves forward if paid after its cutoff | Implemented, not verified |

## 3. Database

Migration `00007_subscription_commerce.sql`, additive, with 30 tables. The details are in
`SPRINT_3_SCHEMA.md`.
- **Immutability:** price rows, invoice lines and order lines reject `UPDATE` and `DELETE`.
- **Rollback:** up, down and up are verified by the rollback test and on the test stack.
- **Live data:** dev and lead data are untouched. Tests use the separate test database and
  the `mise-s1` stack.

## 4. API endpoints

63 routes, all in `docs/api/openapi.yaml` (version 1.3.0). The contract test passes and
the TypeScript client is regenerated.

- **Public (6):**
  - `GET /plans`, `/plans/{slug}`
  - `GET /weekly-menus`, `/weekly-menus/{week}`
  - `GET /subscription-info`
  - `POST /webhooks/payments/{provider}`, authenticated by signature
- **Customer (21):**
  - `GET /me/subscription`
  - `POST /me/subscriptions/quote`, `POST /me/subscriptions`
  - `POST /me/subscriptions/{id}/change-plan|pause|resume|cancel`, `PUT .../address`
  - `GET /me/subscription-cycles`
  - `GET|PUT /me/meal-selections/{cycleId}`, `POST .../finalize`
  - `GET /me/orders[/{id}]`
  - `GET /me/billing`
  - `GET /me/invoices[/{id}[/pdf]]`
  - `POST /checkout/sessions`, `GET /checkout/sessions/{id}`
- **Admin (34):** plans and prices; menus (publish, schedule, unpublish); subscriptions;
  orders; weekly demand; invoices; payments and refunds; webhooks; failed jobs;
  promotions.
- **Test provider (2):** `/test-payments/sessions/{ref}[/complete]`, served only when
  `PAYMENTS_TEST_PROVIDER` is on.

**Deviations from the brief's route list:**
- `GET /me/meal-selections` (a list) is covered by `/me/subscription-cycles`. Each week
  carries its selection status.
- `POST /me/subscriptions/quote` and `GET /checkout/sessions/{id}` were added, so that
  every amount shown before payment, and every outcome after it, comes from the server.

## 5. Payment integration status

| Item | Status |
|---|---|
| Provider-agnostic layer: checkout, status lookup, off-session charge, refund, webhook parsing, declared capabilities | Verified |
| `testpay` simulator: hosted page, saved test cards (approve or decline), HMAC-signed webhooks with a 5-minute replay window | Verified |
| Duplicate webhooks acknowledged and ignored; out-of-order events harmless; repeated reconciliation harmless | Verified |
| Idempotency keys on every charge and refund; one open checkout per billing cycle; a double-submitted refund form makes one refund | Verified |
| Money that cannot be applied (paid after cutoff or after the subscription ended, or a second success) is recorded and flagged for finance, never lost and never auto-refunded | Implemented, not verified (flag paths) |
| **A real payment provider** | **Blocked**: no provider chosen; its docs and sandbox credentials are needed |
| Real-money checkout | **Blocked by design**: impossible with no live provider; the test provider is refused in production |

## 6. Billing and invoices

- **Prepaid weekly:** each week is billed 48 hours before its cutoff (configurable).
  - With a chargeable saved method, the card is charged automatically.
  - Otherwise the customer is emailed to pay.
  - Unpaid at the cutoff: the week is not delivered, the invoice is voided, and the
    subscription goes `past_due`.
- **Amounts:** computed only on the server, as integer cents, with three tax modes (none,
  inclusive, exclusive), an optional delivery fee and promotions. Each billing cycle stores
  the tax and discount rule it used.
- **Invoices:** gapless yearly numbers, with customer, business and tax snapshots and
  immutable lines. There is a deterministic PDF (a test checks identical bytes). The
  invoice says "No tax is charged" while tax is undecided, and no tax registration is
  printed unless one is configured.
- **Promotions:** percentage or fixed amount; first week or several weeks; a use limit;
  validity dates; plan eligibility. They start as drafts and need activating. The
  early-bird offer was **not** created.
- **Refunds:** finance only, through the provider, idempotent, capped at what is left to
  refund. A refund never changes the subscription.

## 7. Weekly menus

- Staff create a menu per week, add meals with optional per-customer limits, set eligible
  plans, then publish now or at a set time.
- **Cutoff:** defaults to Thursday 23:59 before the week. It can be adjusted before
  publishing, and is then fixed. Publishing aligns every subscriber's week to the menu's
  cutoff.
- Unpublishing is refused once customers have chosen from the menu, and a meal someone
  has chosen cannot be removed.
- Every meal on a published menu must itself be published. Customers see only published
  and closed menus.

## 8. Orders and meal selection

- **Selection:** the server enforces the cutoff, the menu, the plan's allowance, per-meal
  limits and a version check against edits from another device. Customers can save a
  draft, then confirm; they can edit until the cutoff, and the selection locks at the
  cutoff.
- **Allergies:** the selection screen marks meals that declare an allergen the customer
  listed, and meals whose allergens are not confirmed. Allergies and preferences are shown
  apart, and nothing is ever called allergen-free.
- **At the cutoff, each week ends one of three ways:**
  - **Order:** paid and confirmed. The order is a snapshot of meal versions, quantities,
    amounts, the address and allergy keys, created once per week.
  - **needs_resolution:** paid, but no confirmed meals or no menu. No meals are invented.
  - **unpaid:** not paid; no order.
- Orders are created as `confirmed`. Later statuses are left to the production and
  delivery modules.
- **Demand read model:** `GET /admin/demand?week=` gives kits per meal version, with order
  references and customer allergy counts. It does no procurement or production planning.

## 9. Customer screens

| Area | Routes |
|---|---|
| Plans | `/plans`, `/plans/[slug]` |
| Subscribe | `/subscribe`, `/subscribe/review`, `/subscribe/result` |
| Subscription | `/subscription`, `/subscription/manage`, `/subscription/change-plan`, `/subscription/pause`, `/subscription/cancel` |
| Menu | `/menu`, `/menu/[week]`, `/menu/[week]/select` |
| Orders | `/orders`, `/orders/[id]` |
| Billing | `/billing`, `/billing/invoices/[id]` (and `/pdf`) |
| Test provider | `/test-checkout/[ref]` |

- **Navigation:** "This week" and "Subscription" were added. The phone tab bar has five
  destinations, and the dashboard shows "your next week".
- **Admin:** Weekly menus, Plans, Subscriptions, Orders (with demand), Payments (with
  webhooks, failed jobs and refunds) and Promotions. Each section appears only for roles
  with the permission.
- **Screenshots** at 1440 px and 390 px are in `docs/screenshots/sprint-3/`. The review led
  to three fixes: smaller week and plan headings, and the phone button saying "Choose 1
  more" instead of a disabled "Confirm".

## 10. Tests executed (11 November 2026)

| Check | Result |
|---|---|
| Go gofmt, vet, build; sqlc output current | clean |
| Go tests against real PostgreSQL | **104 pass**, 0 fail. These include 15 commerce integration tests, plus unit tests for the cutoff calendar, state machine, quotes and tax modes, money and the PDF, and the production safety of the configuration |
| OpenAPI route contract | pass (all 63 new routes documented) |
| ESLint and `tsc`, all touched workspaces | pass |
| Vitest | **43 pass** (web 15, admin 6, marketing 22). New: selection counting, money formatting, time left |
| Next.js production builds (web, admin) | pass |
| Playwright against the running stack | **36/36 pass**: the 31 earlier tests plus 5 new journeys. The new journeys passed three times in a row |

**The brief's 22 critical scenarios:**
- 1 to 20: covered by the Go integration tests; 1, 2, 3, 9, 11, 14, 15 and 17 also in
  Playwright.
- 21 and 22: the Sprint 1 and 2 suites pass unchanged, apart from one assertion. That
  assertion said the dashboard never mentions a subscription, which no longer holds; it
  now checks that nothing about orders or deliveries is invented.

**Not tested:**
- a real provider sandbox (none exists)
- Docker image builds for the apps
- GitHub Actions (there is no remote)
- Safari, Firefox and real phones
- screen readers by hand
- load

## 11. Security review

- **Cards:** no card data is ever collected or stored, only provider tokens and display
  labels. Checkout is hosted by the provider.
- **Webhooks:** verified by HMAC in constant time, with a 5-minute replay window.
  Unauthenticated requests are refused and not stored. No customer session is used.
- **Ownership:** every customer route takes the customer from the session. Subscriptions,
  weeks, selections, orders, invoices, checkouts and PDFs answer 404 for another
  customer's id; tests cover this.
- **Amounts:** computed only on the server; the browser sends plan, address, code and
  meal ids, never an amount.
- **Admin:** every commerce route is permission-gated (8 new permissions). There is no
  endpoint that sets a payment's status. Refunds need `payments.refund`. Failed-job and
  webhook lists hide payloads.
- **Configuration:** the test provider is refused in production (tested). Subscribing and
  opening checkouts are rate-limited per IP. Server actions give the existing CSRF
  protection.
- **Audit:** every transition of subscriptions, payments, refunds, plans, menus,
  promotions and selections writes an audit entry, inside the same transaction.
- **PCI:** no PCI compliance is claimed. A scope review is needed once a provider is
  chosen.

## 12. Outstanding business decisions

| # | Question | Current behaviour |
|---|---|---|
| 1 | Payment provider | testpay only; **blocks real money** |
| 2 | Mise or Food Drop as the payment taker | Mise in the design; either can be a `Provider` |
| 3 | Recurring support | Follows the provider's declared capabilities |
| 4 | Tax (VAT registration, inclusive or exclusive) | None charged; configurable |
| 5 | Delivery fees and zones | Zero fee; all 15 regions active (Sprint 2) |
| 6 | Missed cutoff | `needs_resolution` for staff; no meals invented |
| 7 | Failed renewals: retries, when to stop | 0 retries; `past_due`; each later week is tried again |
| 8 | Refund policy | Manual, by finance only |
| 9 | Pause length | 1 to 4 weeks (configurable) |
| 10 | Eligibility for School Lunch and Corporate | Not sold online ("talk to us") |
| 11 | Approved prices | Business-plan figures exist only as demo plans |
| 12 | Early-bird offer | Not created |
| 13 | Invoice business details (legal name, address, tax number) | Name "Mise" only |

## 13. Known risks

- **Pending subscriptions expire on server time.** The expiry window is measured from the
  database time the row was created, not the scheduler's clock, so a local `-at` tick far
  in the future expires them. This does not affect production.
- **Reaching the provider:** provider calls run outside transactions. A crash between the
  provider call and recording the result is recovered by reconciliation (on expiry) or a
  job retry with the same idempotency key. Both paths are tested only with testpay.
- **Customers without a saved method:** with no off-session support, renewals depend on
  the customer paying by email link before each cutoff. That would need its own decision
  once a provider is chosen.
- **Admin meal list:** the menu editor lists the first page of published meals. This is
  enough for the demo catalogue, but needs search once the real menu grows.
- **Deployment:** the customer app and worker scheduler are not deployed to AWS (that
  needs the ADR-005 go-ahead).

## 14. Sprint 4 readiness

**Ready:**
- Admin and CRM can build on read-only subscription, order and payment views, and on
  `subscription_events` and `audit_log` for timelines.
- Finance has console access.
- Demand per meal version is available for the Chef ERP.

**Needed:**
- the decisions above
- a payment provider (sandbox first)
- delivery partner details for delivery windows
- the real menu and prices

**Deferred on purpose (brief §44):** full admin and CRM, production planning, procurement,
inventory, delivery dispatch, add-ons, and plans with per-customer eligibility.

Sprint 4 has not been started.
