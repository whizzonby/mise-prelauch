# Sprint 3 plan: subscriptions, ordering and payments

Written 11 November 2026, on branch `sprint-3/subscription-commerce` from Sprint 2 (`834d541`).

## Step 1: audit of Sprint 1 and Sprint 2

Checked against the code, not the reports:

- **Identity and sessions:** customer sessions are httpOnly opaque tokens held by the
  `apps/web` server. `RequireCustomer` and `RequireAdmin(perm)` guard every route.
  Permissions are in the database, and a test ties the code's permission list to the
  migrations.
- **Customer records:** these are reused as they are:
  - profile, household (counts)
  - preferences and allergies, kept in separate tables
  - addresses (several, one default; region checked against `delivery_zones`, with all
    15 Trinidad and Tobago regions active)
  - notification topics
- **Catalogue:** meals have `version` (it goes up on every edit), a `status` and
  `is_demo`. Allergens are `declared` or `unconfirmed`, and nutrition is shown only once
  verified. `catalog.Service.Cards` and `Visible` are the read interface.
- **Infrastructure:**
  - The `jobs` table queue is transactional (`jobs.Enqueue` takes the caller's
    transaction), so it is already an outbox.
  - Retries with backoff and the requeue of stuck jobs are in place.
  - There is no scheduler yet.
- **Audit:** `audit.Record` takes the caller's transaction.
- **Conventions:**
  - Go `net/http` ServeMux with a `Router` recorder, checked against
    `docs/api/openapi.yaml`.
  - The `{data, error, meta}` envelope.
  - sqlc for new modules.
  - Goose migrations, additive and reversible.
- **Frontend:**
  - Server components and server actions.
  - Tokens stay on the Next.js server.
  - `packages/ui` primitives.
  - The `(app)` route group requires a session.
- **Tests:** Sprint 2 was green on 11 November 2026 (Go 86, Vitest 38, Playwright 31).

**Blockers found:** none for building. These are missing, and Sprint 3 adds them:

- a periodic scheduler in the worker
- an idempotency key on jobs (handlers must be idempotent instead)
- no `admin.access` for the `finance` role

**Kept from Sprint 2 (deviations from the brief's stack):** server actions instead of
React Hook Form and TanStack Query, no shadcn layer, and CSS motion. See ADR-006.

## Step 2: business rules

Answered by the owner on 11 November 2026:

| Question | Answer | How the code holds it |
|---|---|---|
| Payment provider | **Not chosen** | A provider-agnostic `payments.Provider` interface, plus `testpay`, a local test adapter. `testpay` is refused in production by configuration. There is no live provider, so real-money checkout is impossible |
| Billing model | **Prepaid weekly** | Each week is billed before its cutoff (`BILLING_RENEWAL_LEAD`, default 48 h before). An unpaid week at cutoff produces no order |
| Selection cutoff | **Thursday 23:59, Trinidad and Tobago time** | `MENU_CUTOFF_WEEKDAY=thursday`, `MENU_CUTOFF_TIME=23:59`. Each menu stores its own `cutoff_at` (UTC), which staff may adjust before publishing |
| Tax and delivery fees | **Not decided** | `BILLING_TAX_MODE` (`none`, `inclusive` or `exclusive`) and `BILLING_TAX_RATE_BPS`, default `none`. `BILLING_DELIVERY_FEE_MINOR` defaults to 0. Each invoice keeps a snapshot of the policy in force when it was issued |

Not answered, so they are configurable and marked **unresolved** in the report:

| Question | Behaviour until decided |
|---|---|
| Food Drop or Mise as the payment taker | Mise owns checkout in the design; any provider can sit behind the interface |
| Recurring payments | The provider declares `OffSessionCharges`. With it, renewals are charged automatically. Without it, the renewal invoice waits for the customer to pay through checkout |
| Missed cutoff | No default meals are invented. A paid week with no confirmed selection becomes `needs_resolution`, for staff to decide |
| Failed renewal | Subscription goes `past_due`, the customer is told, and can pay until the cutoff. Automatic retries: `BILLING_RENEWAL_RETRIES` (default **0**) after `BILLING_RENEWAL_RETRY_AFTER` |
| Pause | 1 to `SUBSCRIPTION_MAX_PAUSE_WEEKS` (default 4) weeks, from the first week not yet billed |
| Cancellation | Takes effect from the first week not yet billed. Weeks already paid are still delivered. **No automatic refunds**; refunds are a finance action |
| Proration | None. A plan change takes effect from the first week not yet billed |
| Plan segments | `segment` is descriptive. Only plans marked `self_serve` can be bought online. In the demo seed, School Lunch and Corporate Wellness are not self-serve (eligibility unknown) |
| Business-plan prices | Seeded only as **demo** plans (`misectl commerce seed-demo`), which are hidden outside local. The early-bird offer is not created |
| Invoice business details | `BILLING_BUSINESS_NAME` and `BILLING_BUSINESS_ADDRESS`. No tax number is shown unless `BILLING_TAX_REGISTRATION` is set |

## Domain model

Each lifecycle is its own record:

- **Plan:** what can be bought. Prices are separate immutable rows with an
  `effective_from`.
- **Subscription:** the customer's agreement. It keeps a snapshot of the plan and price it
  is on, plus its status.
- **Subscription cycle:** one delivery week of one subscription, unique per (subscription,
  week). It holds its own plan snapshot, cutoff and outcome.
- **Billing cycle:** the money side of a cycle, one per cycle. It has amounts, a status,
  attempts and an invoice.
- **Invoice:** issued with a gapless yearly number when a billing cycle is due. Its line
  items are immutable.
- **Payment:** one attempt to collect a billing cycle through a provider. It has a status
  history. Refunds are separate rows.
- **Meal selection:** the customer's meals for one cycle: draft, then confirmed, then
  locked at cutoff.
- **Order:** created once, at cutoff, from a paid cycle with a confirmed selection. It keeps
  a snapshot of the meals (with their version), quantities, amounts, address and allergy
  keys.

### Subscription statuses

```text
pending_payment ─▶ active ◀──▶ past_due
       │            │  ▲          │
       ▼            ▼  │          ▼
    expired   pause_scheduled ─▶ paused
                    │            │
                    ▼            ▼
             cancel_scheduled ─▶ cancelled
```

The allowed transitions are a table in `subscriptions/state.go`, with a test. Any other
transition is refused, and every transition writes a `subscription_events` row and an
audit entry.

### The weekly timeline (defaults)

For the week starting Monday 23 November 2026:

| When (Trinidad and Tobago time) | What happens |
|---|---|
| Thu 12 Nov 23:59 | The previous week's cutoff passes. The scheduler creates the cycle for the week of 23 Nov, with the plan snapshot taken now |
| From when staff publish the menu | The customer chooses meals: they save a draft, then confirm |
| Tue 17 Nov 23:59 (48 h before cutoff) | **Renewal:** the billing cycle and invoice are issued, and the card is charged if the provider allows it |
| Wed 18 Nov 23:59 | A reminder is sent if the selection is not confirmed |
| Thu 19 Nov 23:59 | **Cutoff:** the selection locks. The cycle finalises as one of: an order (paid and confirmed); `needs_resolution` (paid, no confirmed selection or no menu); `unpaid` (invoice voided, no order); or `paused` |

The scheduler is idempotent and safe to run twice. Every step has a unique key, and each
step runs in a transaction under a PostgreSQL advisory lock.

## Database: migration `00007_subscription_commerce.sql` (additive)

| Area | Tables |
|---|---|
| Plans | `subscription_plans`, `subscription_plan_prices` (immutable) |
| Subscriptions | `subscriptions`, `subscription_changes`, `subscription_pauses`, `subscription_events`, `subscription_cycles` |
| Menus and selection | `weekly_menus`, `weekly_menu_meals`, `meal_selections`, `meal_selection_items` |
| Orders | `orders`, `order_items` (immutable), `order_status_history` |
| Billing | `billing_cycles`, `invoices`, `invoice_items` (immutable), `invoice_counters` |
| Payments | `payment_methods` (provider tokens only), `checkout_sessions`, `payments`, `payment_status_history`, `payment_webhook_events`, `refunds` |
| Promotions | `promotions`, `promotion_redemptions` |
| Delivery of notifications | `notification_deliveries` (a dedupe key per email) |
| Test provider | `testpay_sessions`, `testpay_transactions`. These belong to the simulated provider and are used only when `PAYMENTS_TEST_PROVIDER=true` |
| Permissions | `plans.manage`, `menus.manage`, `menus.publish`, `subscriptions.read`, `orders.read`, `payments.read`, `payments.refund`, `promotions.manage` |

Money is `bigint` minor units (cents) with a `currency` column. Nothing uses floating
point.

## Backend modules

| Module | Owns |
|---|---|
| `plans` | plans and price versions; public read |
| `menus` | weekly menus and their meals; cutoff policy; publishing |
| `subscriptions` | subscriptions, cycles, changes, pauses, events, the state machine, meal selections |
| `billing` | billing cycles, invoices, invoice numbering and PDF, promotions, the tax and fee policy |
| `payments` | the `Provider` interface, `testpay`, checkout sessions, payments, webhooks, refunds, payment methods |
| `orders` | orders, items, status history, the demand read model |
| `commerce` | the workflows across them: checkout, payment outcome to activation, the scheduler tick, notifications. Each runs in one transaction |

Dependencies point into `commerce`. `payments` knows nothing about subscriptions: it
reports outcomes through a `Hooks` interface that `commerce` implements.

## Payments flow (test mode)

1. `POST /me/subscriptions` creates a `pending_payment` subscription, cycle 1 and its
   billing cycle. The amounts are computed on the server.
2. `POST /checkout/sessions` asks the provider for a hosted checkout and returns its URL.
3. **testpay:** the customer app hosts a clearly labelled "Test payment" page. It asks
   `testpay` to approve or decline, and `testpay` then sends a **signed webhook** to
   `POST /webhooks/payments/testpay`, through the same code a real provider would use.
4. The webhook handler:
   - checks the HMAC signature
   - stores the event (unique per provider event id, so a duplicate is a no-op)
   - queues processing in the same transaction
5. Processing asks the provider for the payment's **current status** (reconcile). Because
   it acts on the authoritative status, events that arrive out of order are harmless. The
   state machine never moves a payment backwards.
6. When a payment succeeds, one transaction:
   - marks the billing cycle paid
   - issues the invoice
   - activates the subscription (activation is idempotent)
   - queues the emails
7. `/subscribe/result` polls the API. It never decides on success itself.

## Customer app routes

| Area | Routes |
|---|---|
| Plans | `/plans`, `/plans/[slug]` |
| Subscribe | `/subscribe` (plan, address, code), `/subscribe/review` (server-computed totals and terms), checkout (a server action that redirects to the provider), `/subscribe/result` |
| Subscription | `/subscription`, `/subscription/manage` (delivery address), `/subscription/change-plan`, `/subscription/pause`, `/subscription/cancel` |
| Menu | `/menu` (redirects to the open week), `/menu/[week]`, `/menu/[week]/select` |
| Orders | `/orders`, `/orders/[id]` |
| Billing | `/billing`, `/billing/invoices/[id]` (and its PDF) |
| Test provider | `/test-checkout/[session]`, which exists only when the API reports `testpay` |

## Tests

- **Go, against PostgreSQL:** all 22 critical scenarios in the brief. The scheduler is
  driven with an explicit clock, so the cutoff, renewal and duplicate-run cases are exact.
- **Unit:** the state machine, the cutoff calculation, invoice totals (tax modes,
  discounts) and the order of the PDF bytes.
- **Vitest:** formatting money and weeks, and the selection counter logic.
- **Playwright:**
  - plans, then subscribe, then test payment, then active
  - select meals, confirm, edit
  - billing and invoice
  - pause, resume, cancel
  - a declined payment
  - admin menus and plans
  - the Sprint 1 and Sprint 2 suites as the regression check

## Progress

- [x] Step 3: database (migration 00007)
- [x] Steps 4 to 9: backend modules (plans, menus, subscriptions, billing, payments + testpay, orders, commerce).
  15 commerce integration tests pass; the full Go suite is green; OpenAPI is 1.3.0; the API client is regenerated
- [x] Step 10: customer UI (plans, subscribe, subscription, menu selection, orders, billing, test checkout) and admin (plans, menus, subscriptions, orders and demand, payments and refunds, promotions)
- [x] Step 11: notifications (11 commerce emails, deduplicated)
- [x] Step 12: verification and report. **Sprint 3 complete** (11 November 2026): Go 104,
  Vitest 43, Playwright 36/36. See `docs/SPRINT_3_REPORT.md`. Not committed yet.
