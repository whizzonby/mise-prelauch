# Sprint 8 audit: analytics, automation and forecasting

Audited 15 November 2026 on `sprint-8/analytics-automation` (from Sprint 7 review fix `8edeac0`).
The repository is authoritative. **Repo differs** marks a disagreement with a report or the brief.

## 0. Sprint 7 corrections (brief §1)

| Check | Finding |
|---|---|
| Scheduling respects the order week | Yes. `delivery_settings` delivery-week rule; enforced in `Schedule`, `DecideReschedule`, `AddStop`; unset rule blocks scheduling. Test `TestDeliveryDatesFollowTheOrderWeekRule`. |
| Corrected outcomes reconcile with commerce and customer views | Yes. `order_delivery_outcomes` (append-only, commerce-owned) via `orders.FlagDeliveryOutcome`; order JSON `delivery_outcome`; delivery report and overview counts. Test `TestCorrectedOutcomeIsADisputeInCommerceAndReports`. |
| Regression tests pass | Yes on 15 Nov: Go 149, Vitest 43, Playwright 55. |
| Delivery cost records have integration coverage | **No.** `RecordCost`/`Costs` have no integration test. Fixed in Stage 1 of this sprint (`TestDeliveryCostRecords`) before analytics uses costs. |

## 1. Source systems

Commerce (plans, menus, subscriptions, billing cycles, payments, refunds, invoices, orders), customers
and identity, CRM/support (Sprint 4), culinary and production (Sprint 5), suppliers, procurement and
inventory (Sprint 6), fulfillment and delivery (Sprint 7), leads and marketing analytics (Sprint 1–2).
One PostgreSQL database; one Go API and worker; Next.js admin and customer apps.

## 2. Authoritative tables and services

| Domain | Authoritative records | Owner |
|---|---|---|
| Revenue | `payments` (status, amount, succeeded_at, provider), `refunds` (succeeded), `invoices`, `billing_cycles` | `payments`, `billing` |
| Orders | `orders`, `order_items` (immutable), `order_status_history`, `order_delivery_outcomes` | `orders` |
| Subscriptions | `subscriptions`, `subscription_events` (created, activated, cancel_scheduled, cancellation_withdrawn, cancelled, pause_scheduled, resumed, expired), `subscription_changes`, `subscription_pauses`, `subscription_cycles` | `subscriptions` |
| Customers | `customers`, `users`, `lead_attribution` (first/last touch for waitlist leads only) | `customers`, `leads` |
| Meals | `meals` (`is_demo`), `weekly_menus`, `weekly_menu_meals`, `meal_selections` | `catalog`, `menus` |
| Production | plans, batches, `production_yield_records` (produced, rejected, reworked, accepted), quality holds, packaging, waste | `production` |
| Procurement | runs, lines, RFQs, quotations, purchase orders and lines, revisions, events | `procurement` |
| Inventory | `inventory_lots`, `inventory_movements` (append-only ledger), reservations, counts, adjustments, `batch_ingredient_lots` | `inventory` |
| Fulfillment/delivery | obligations, packages, deliveries, runs, handoffs, tracking, proofs, exceptions, returns, cost records, reconciliations | `fulfillment`, `delivery` |

## 3. Available event types (outbox `domain_events`)

Emitted today: `production.{demand_snapshotted, plan_created, plan_approved, plan_released,
batch_started, batch_completed, ready_for_fulfillment}`, `quality.{hold_created, hold_released}`,
`packaging.completed`, `recipe.{submitted, approved, published}`, `nutrition.review_completed`,
`supplier.{approved, suspended}`, `procurement.{plan_approved, shortage_identified}`,
`purchase_order.{approved, issued}`, `receiving.inspection_failed`, `inventory.{stock_reserved,
stock_issued, stock_adjusted}`, `fulfillment.{created, verified, ready_for_dispatch, exception_raised}`,
`delivery.{assigned, dispatched, handed_over, in_transit, delivered, failed, rescheduled, returned,
exception_raised, reconciled}`.

**Repo differs:** commerce (subscriptions, payments, refunds, orders) emits **no** domain events; it
keeps its own history tables. Payment-failure and subscription triggers must therefore be evaluated
from those tables on a schedule, not from the outbox. Events exist only since Sprint 5 (production)
and later, and not every historical transition emitted one. Analytics are computed from authoritative
tables; events are used only to refresh sooner and to trigger automations.

## 4. Data ownership

Analytics reads every domain and writes only its own snapshot, refresh and data-quality tables.
Automation writes its own rules, executions and tasks, and creates admin alerts through
`backoffice.Raise`. It never writes another module's tables. Forecasting writes only its own runs and
values. Forecasts never touch production plans or purchase orders.

## 5. Historical data availability (development database, 15 Nov 2026)

77 orders over 35 order weeks (from 2026-11-16, created with moved clocks by demo and e2e seeding), 174
payments, all `provider = testpay`, 156 subscriptions, 690 domain events, 37 meals (6 `is_demo`).
**There is no real operating history.** Every order was paid with the test provider or created by
demo seeding. Real demand is zero by definition, so forecasting must report "not enough verified
historical demand".

## 6. Data quality limitations

- Test versus real is identifiable only through `payments.provider = 'testpay'` and `meals.is_demo`,
  plus demo partners and couriers (`demo`). There is no customer-level test flag. Free (promotion)
  orders have no payment, so they are classified by their customer's other payments when available
  and otherwise counted as unverified.
- Order weeks run far into 2027 because seeding moves the clock. They are real rows, not real history.
- Payment retries are separate `payments` rows. Collected value is `succeeded` (or later refunded)
  rows only, which cannot double-count retries because a failed retry never succeeds.
- `on-time` exists only for deliveries with a confirmed date (and window when promised).
- Acquisition channel is recorded only for waitlist leads (`lead_attribution`). There is none for
  customers who registered directly.
- Inventory value is supported only for lots with `unit_cost_minor`. Lots without it are reported
  as "value unknown".

## 7. Existing report implementations

`internal/reporting.Report` covers customers, subscriptions, orders, finance, marketing, production,
procurement, inventory and suppliers. `delivery.Report` covers delivery. Each figure carries a
definition. CSV exports go through `backoffice.RequestExport`/`HandleExport` (`report_exports`,
audited, formula-safe). **Risk:** the same figure (e.g. "collected") can be computed in two places.
Sprint 8 introduces a single metric registry, and the existing reports are checked against it by
test, so the definitions cannot drift.

## 8. Existing scheduling infrastructure

- `platform/jobs`: queue with retries, backoff and stuck-job recovery.
- `platform/consumer`: durable outbox consumers (procurement, fulfillment).
- Interval loops in the worker: commerce scheduler (advisory lock), `backoffice.RunAlerts`, consumer
  dispatchers.

Sprint 8 adds one worker loop, `automation.Run`, guarded by an advisory lock, for scheduled rule
evaluation, report schedules and analytics refresh. All work runs as jobs. No second scheduler.

## 9. Proposed analytics boundaries

`internal/analytics`: a metric registry (one definition and one SQL per metric), dashboards built
from it, daily snapshots for history and freshness, refresh and backfill jobs, reconciliation of
snapshots against live values, data-quality checks, and CSV exports through the existing export
pipeline.

## 10. Proposed automation boundaries

`internal/automation`: typed rule kinds only (no user code). Evaluation (find subjects) is separate
from execution (perform an action as a job). Allowed actions are alert, task, flag for review,
generate report and request approval. Refunds, charges, approvals, hold overrides, dispatch and plan
changes are not actions. Thresholds ship unset, so rules ship disabled.

## 11. ML readiness assessment

No real history, no labels for churn, and no supplier delay history beyond test data. A Python ML
service is not justified. Sprint 8 builds explainable baselines (naive, seasonal naive, moving
average, simple exponential smoothing) behind an interface, with rolling-origin backtesting and a
readiness gate. Activation criteria are a business decision and ship unset.

## 12. Privacy and security risks

Analytics endpoints return aggregates. Customer-level lists (retention signals) are limited to
`analytics_customers.read` plus the existing `customers.read`, and show no payment card or health
data. Exports run through the audited export pipeline and omit contact details unless the role has
`customers.sensitive`. Automation tasks name records by id and number, not personal details. Demo and
test data can be mistaken for real figures, so every dashboard shows a data-origin notice while test
payments dominate.
