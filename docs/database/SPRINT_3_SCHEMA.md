# Sprint 3 schema: subscription commerce

Migration `00007_subscription_commerce.sql`. It is additive: no existing table changes,
apart from new rows in `permissions` and `role_permissions`. It has been tested up, down
and up again, both in the migration rollback test and on the test stack.

## Conventions

- **Money:** `bigint` minor units (cents) beside a `currency` column. Nothing uses
  floating point.
- **Time:** `timestamptz` (UTC). A delivery week is a `date`, the Monday, as a local
  Trinidad and Tobago date. A check enforces that it is a Monday.
- **Financial history:**
  - `subscription_plan_prices`, `invoice_items` and `order_items` have a `forbid_change`
    trigger: an `UPDATE` or `DELETE` raises an error.
  - Corrections are new rows: a new price version, a refund.
- **Retry safety:**
  - Unique keys make every step safe to repeat: `subscription_cycles (subscription_id,
    week_start)`, `billing_cycles (subscription_cycle_id)`, `invoices (billing_cycle_id)`,
    `orders (subscription_cycle_id)`.
  - So do the idempotency keys on `payments`, `refunds` and `testpay_transactions`, the
    unique `(provider, event_id)` on webhooks, and the `notification_deliveries` key.

## Tables

| Area | Table | Notes |
|---|---|---|
| Plans | `subscription_plans` | `status` draft/active/retired; `self_serve`; availability window; `is_demo`; `version` |
| | `subscription_plan_prices` | Immutable; unique `(plan_id, effective_from)`; the current price is the latest one in effect |
| Subscriptions | `subscriptions` | 8 statuses; one live per customer (partial unique index); plan and price snapshot for future weeks; pause range; cancellation effective week |
| | `subscription_events` | Every status change and customer request |
| | `subscription_changes`, `subscription_pauses` | Plan changes and pauses, with their effective weeks |
| | `subscription_cycles` | One per (subscription, week). It holds its own plan snapshot, `cutoff_at` and `renewal_at`. `state` is scheduled, finalized, unpaid, needs_resolution, paused or cancelled |
| Menus | `weekly_menus` | Unique per week; draft/scheduled/published/closed/archived; `cutoff_at`; eligible plans |
| | `weekly_menu_meals` | Meals on a menu, with an optional per-customer cap |
| Selection | `meal_selections` | One per cycle: draft, confirmed or locked; `version` for edits from two devices; `was_confirmed` at lock |
| | `meal_selection_items` | Meal and quantity |
| Billing | `billing_cycles` | The money side of a cycle: amounts plus tax and discount snapshots, a CHECK that the total adds up, status pending/due/paid/failed/void, attempts, next retry |
| | `invoices`, `invoice_items`, `invoice_counters` | Gapless yearly numbers (`MISE-2026-000001`) from a counter row locked in the issuing transaction; customer, business and tax snapshots |
| | `promotions`, `promotion_redemptions` | Draft until activated; redemption cap enforced under a row lock; one use per customer |
| Payments | `payment_methods` | Provider token and display label only. Never card data |
| | `checkout_sessions` | At most one open per billing cycle |
| | `payments`, `payment_status_history` | One row per provider transaction; status only moves forward; `needs_attention` for money that could not be applied |
| | `payment_webhook_events` | Authenticated events only, once per provider event id; `ref_kind` and `provider_ref` say what to reconcile |
| | `refunds` | Idempotent per request key; capped at what is left to refund |
| Orders | `orders`, `order_items`, `order_status_history`, `order_counters` | A snapshot of meals with their versions, quantities, amounts, the address and the customer's allergy keys; `payment_id` is null only for a week a promotion made free |
| Email | `notification_deliveries` | A dedupe key per email that must go out at most once |
| Test provider | `testpay_sessions`, `testpay_transactions` | The simulator's own records; written only when `PAYMENTS_TEST_PROVIDER` is on |

## Permissions

| Permission | Granted to |
|---|---|
| `plans.manage` | super_admin, admin, finance |
| `menus.manage` | super_admin, admin, operations, marketing |
| `menus.publish` | super_admin, admin, operations |
| `subscriptions.read`, `orders.read` | super_admin, admin, finance, operations, support |
| `payments.read` | super_admin, admin, finance, support |
| `payments.refund` | super_admin, admin, finance |
| `promotions.manage` | super_admin, admin, finance |

`finance` now has `admin.access`.

## What later modules read

- **Demand:** `GET /admin/demand?week=` reads `orders` and `order_items`: kits per meal
  version, with order numbers and customer allergy keys. Production and procurement
  modules should read it, or the same query; they should not write to these tables.
- **Order status:** later statuses (awaiting_production onwards) are allowed by the CHECK
  constraint. The production and delivery modules will own those transitions and write
  `order_status_history`.
