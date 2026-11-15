# Metric catalog

Generated from `internal/analytics/metrics.go` by `misectl analytics catalog`; a test fails if this file
is out of date. Every dashboard, export, snapshot and scheduled report uses these definitions and no other.

Time zone: America/Port_of_Spain (all dates are local to Trinidad and Tobago). Reporting period: Any range of 1 day to 2 years; compared with the previous period of equal length.

## Customers

### `customers.first_paid` — First-time payers

Customers whose first successful payment was in the range.

| | |
|---|---|
| Kind | count |
| Source fields | `payments.customer_id`, `payments.succeeded_at` |
| Calculation | count of customers with min(succeeded_at) in the range |
| Filters | none |
| Update | Computed on request from the authoritative tables; daily values snapshotted hourly |
| Missing data | 0 |
| Currency | not money |
| Limitations | Includes test-provider payments. |
| Owner | Growth |
| Permission | `analytics_customers.read` |

### `customers.new` — New customer accounts

Customer accounts created in the range.

| | |
|---|---|
| Kind | count |
| Source fields | `customers.created_at` |
| Calculation | count |
| Filters | none |
| Update | Computed on request from the authoritative tables; daily values snapshotted hourly |
| Missing data | 0 |
| Currency | not money |
| Limitations | Includes demo and test accounts. |
| Owner | Growth |
| Permission | `analytics_customers.read` |

### `customers.repeat_rate` — Repeat ordering

Of customers with an order in the range, the share who also had an earlier order.

| | |
|---|---|
| Kind | ratio |
| Source fields | `orders.customer_id`, `orders.created_at` |
| Calculation | customers with an order in range and an order before it ÷ customers with an order in range |
| Filters | none |
| Update | Computed on request from the authoritative tables |
| Missing data | Undefined when no customer ordered in the range |
| Currency | not money |
| Limitations | Cancelled and pending orders excluded. |
| Owner | Growth |
| Permission | `analytics_customers.read` |

## Delivery

### `delivery.cost_actual` — Actual delivery cost

Recorded actual partner costs plus adjustments for deliveries or runs in the range. Estimated and contracted figures are never mixed in.

| | |
|---|---|
| Kind | money |
| Source fields | `delivery_cost_records.kind`, `delivery_cost_records.amount_minor` |
| Calculation | sum where kind in (actual, adjustment), currency TTD |
| Filters | zone |
| Update | Computed on request from the authoritative tables |
| Missing data | 0 when no costs were recorded (which is not the same as no cost) |
| Currency | TTD minor units (cents). Other currencies are excluded, never converted. |
| Limitations | Only what staff recorded; there is no carrier billing feed. |
| Owner | Dispatch |
| Permission | `analytics_delivery.read` |

### `delivery.delivered` — Delivered

Of those, deliveries completed with accepted proof and not disputed (corrected outcomes count under their corrected status).

| | |
|---|---|
| Kind | count |
| Source fields | `deliveries.status`, `order_delivery_outcomes.state` |
| Calculation | count where status = delivered and the order's latest outcome is not disputed |
| Filters | zone |
| Update | Computed on request from the authoritative tables |
| Missing data | 0 |
| Currency | not money |
| Limitations | None known. |
| Owner | Dispatch |
| Permission | `analytics_delivery.read` |

### `delivery.disputed` — Disputed outcomes

Now: orders whose delivered outcome commerce records as disputed (corrected and not delivered again).

| | |
|---|---|
| Kind | count |
| Source fields | `order_delivery_outcomes` |
| Calculation | count of orders whose latest outcome entry is disputed |
| Filters | none |
| Update | Computed on request from the authoritative tables |
| Missing data | 0 |
| Currency | not money |
| Limitations | Point in time. |
| Owner | Dispatch |
| Permission | `analytics_delivery.read` |

### `delivery.failed_attempts` — Failed attempts

Recorded failed attempts for deliveries scheduled in the range (a delivery can fail more than once).

| | |
|---|---|
| Kind | count |
| Source fields | `delivery_tracking_events.to_status` |
| Calculation | count of tracking events to delivery_failed |
| Filters | zone |
| Update | Computed on request from the authoritative tables |
| Missing data | 0 |
| Currency | not money |
| Limitations | None known. |
| Owner | Dispatch |
| Permission | `analytics_delivery.read` |

### `delivery.first_attempt_rate` — First-attempt success

Delivered on attempt 1 (and not disputed) ÷ deliveries scheduled in the range.

| | |
|---|---|
| Kind | ratio |
| Source fields | `deliveries.attempt`, `deliveries.status` |
| Calculation | first-attempt delivered / scheduled |
| Filters | zone |
| Update | Computed on request from the authoritative tables |
| Missing data | Undefined with no scheduled deliveries |
| Currency | not money |
| Limitations | Deliveries not yet attempted count against the rate until they are. |
| Owner | Dispatch |
| Permission | `analytics_delivery.read` |

### `delivery.on_time_rate` — On time

Of delivered deliveries with an approved promised window, the share delivered on the date and inside the window. Without a promised window it is not measurable.

| | |
|---|---|
| Kind | ratio |
| Source fields | `deliveries.delivered_at`, `deliveries.service_date`, `delivery_windows` |
| Calculation | on time ÷ delivered with an approved window |
| Filters | zone |
| Update | Computed on request from the authoritative tables |
| Missing data | Undefined when no delivered delivery had a promised window |
| Currency | not money |
| Limitations | Deliveries with only a date and no window are excluded. |
| Owner | Dispatch |
| Permission | `analytics_delivery.read` |

### `delivery.returned` — Returned

Deliveries scheduled in the range whose packages came back.

| | |
|---|---|
| Kind | count |
| Source fields | `deliveries.status` |
| Calculation | count where status = returned |
| Filters | zone |
| Update | Computed on request from the authoritative tables |
| Missing data | 0 |
| Currency | not money |
| Limitations | None known. |
| Owner | Dispatch |
| Permission | `analytics_delivery.read` |

### `delivery.scheduled` — Deliveries scheduled

Deliveries with a confirmed service date in the range, not cancelled.

| | |
|---|---|
| Kind | count |
| Source fields | `deliveries.service_date`, `deliveries.schedule_confirmed_at` |
| Calculation | count |
| Filters | zone |
| Update | Computed on request from the authoritative tables |
| Missing data | 0 |
| Currency | not money |
| Limitations | None known. |
| Owner | Dispatch |
| Permission | `analytics_delivery.read` |

## Inventory

### `inventory.issue_movements` — Issues to production

Ledger issue movements in the range.

| | |
|---|---|
| Kind | count |
| Source fields | `inventory_movements.type`, `inventory_movements.created_at` |
| Calculation | count where type = issue |
| Filters | none |
| Update | Computed on request from the authoritative tables; daily values snapshotted hourly |
| Missing data | 0 |
| Currency | not money |
| Limitations | Counts movements; quantities differ by unit. |
| Owner | Stores |
| Permission | `analytics_inventory.read` |

### `inventory.lots_expired` — Lots expired

Now: lots with stock whose recorded expiry date has passed (local date).

| | |
|---|---|
| Kind | count |
| Source fields | `inventory_lots.expiry_date` |
| Calculation | count |
| Filters | none |
| Update | Computed on request from the authoritative tables |
| Missing data | 0 |
| Currency | not money |
| Limitations | Lots without a recorded expiry are not counted here. |
| Owner | Stores |
| Permission | `analytics_inventory.read` |

### `inventory.lots_quarantined` — Lots quarantined or on hold

Now: lots with stock that cannot be used (quarantined or on hold).

| | |
|---|---|
| Kind | count |
| Source fields | `inventory_lots.status` |
| Calculation | count |
| Filters | none |
| Update | Computed on request from the authoritative tables |
| Missing data | 0 |
| Currency | not money |
| Limitations | Point in time. |
| Owner | Stores |
| Permission | `analytics_inventory.read` |

### `inventory.lots_without_cost` — Lots without a cost

Now: lots with stock on hand and no recorded unit cost.

| | |
|---|---|
| Kind | count |
| Source fields | `inventory_lots` |
| Calculation | count |
| Filters | none |
| Update | Computed on request from the authoritative tables |
| Missing data | 0 |
| Currency | not money |
| Limitations | Point in time. |
| Owner | Stores |
| Permission | `analytics_inventory.read` |

### `inventory.value_on_hand` — Stock value on hand

Now: on-hand quantity × recorded unit cost for usable or held lots with a cost, TTD only. A current figure, not for the range.

| | |
|---|---|
| Kind | money |
| Source fields | `inventory_lots.on_hand`, `inventory_lots.unit_cost_minor`, `inventory_lots.received_quantity` |
| Calculation | sum(on_hand × unit_cost_minor) — the unit cost is per base unit as recorded at receiving |
| Filters | none |
| Update | Computed on request from the authoritative tables |
| Missing data | 0; lots without cost are counted in inventory.lots_without_cost |
| Currency | TTD minor units (cents). Other currencies are excluded, never converted. |
| Limitations | Lots without a recorded cost are excluded, so this understates value when costs are missing. |
| Owner | Stores |
| Permission | `analytics_inventory.read` |

### `inventory.waste_movements` — Store waste

Ledger waste movements (spoiled or damaged in storage) in the range.

| | |
|---|---|
| Kind | count |
| Source fields | `inventory_movements` |
| Calculation | count where type = waste |
| Filters | none |
| Update | Computed on request from the authoritative tables; daily values snapshotted hourly |
| Missing data | 0 |
| Currency | not money |
| Limitations | Kitchen waste is a production record, not counted here. |
| Owner | Stores |
| Permission | `analytics_inventory.read` |

## Orders

### `meals.kits_ordered` — Meal kits ordered

Kits in live orders for order weeks starting in the range (confirmed demand; carts and drafts never become orders).

| | |
|---|---|
| Kind | count |
| Source fields | `order_items.quantity`, `orders.week_start`, `orders.status` |
| Calculation | sum(quantity) over live orders by week_start |
| Filters | plan, meal |
| Update | Computed on request from the authoritative tables |
| Missing data | 0 |
| Currency | not money |
| Limitations | Counted by order week, not by order date. Includes test-provider orders. |
| Owner | Culinary |
| Permission | `analytics_production.read` |

## Operations

### `operations.open_exceptions` — Open operational exceptions

Now: open fulfillment and delivery exceptions, open quality holds, and open support cases.

| | |
|---|---|
| Kind | count |
| Source fields | `delivery_exceptions.status`, `quality_holds.status`, `support_cases.status` |
| Calculation | sum of the three counts |
| Filters | none |
| Update | Computed on request from the authoritative tables |
| Missing data | 0 |
| Currency | not money |
| Limitations | Point in time. |
| Owner | Operations |
| Permission | `analytics_executive.read` |

## Commerce

### `orders.average_value` — Average order value

Ordered value divided by orders, in the range.

| | |
|---|---|
| Kind | money |
| Source fields | `orders.total_minor` |
| Calculation | revenue.ordered_value / orders.count |
| Filters | plan |
| Update | Computed on request from the authoritative tables |
| Missing data | Undefined (no value) when there are no orders |
| Currency | TTD minor units (cents). Other currencies are excluded, never converted. |
| Limitations | Rounded to the cent. |
| Owner | Commerce |
| Permission | `analytics_revenue.read` |

### `orders.count` — Orders

Orders made in the range, not cancelled.

| | |
|---|---|
| Kind | count |
| Source fields | `orders.created_at`, `orders.status` |
| Calculation | count of live orders created in the range |
| Filters | plan |
| Update | Computed on request from the authoritative tables; daily values snapshotted hourly |
| Missing data | 0 |
| Currency | not money |
| Limitations | Includes test-provider orders. |
| Owner | Commerce |
| Permission | `analytics_revenue.read` |

## Procurement

### `procurement.cycle_days` — Order to first delivery

Average days from issuing a purchase order to its first receipt, for orders first received in the range.

| | |
|---|---|
| Kind | days |
| Source fields | `purchase_orders.issued_at`, `goods_receipts.received_at` |
| Calculation | avg(first received_at − issued_at) |
| Filters | supplier |
| Update | Computed on request from the authoritative tables |
| Missing data | Undefined with no deliveries |
| Currency | not money |
| Limitations | Supplier lead time as observed, not as quoted. |
| Owner | Procurement |
| Permission | `analytics_procurement.read` |

### `procurement.on_time_rate` — Received on time

Of orders first received in the range that had an expected date, the share first received on or before it (local date).

| | |
|---|---|
| Kind | ratio |
| Source fields | `purchase_orders.expected_delivery`, `goods_receipts.received_at` |
| Calculation | on time ÷ received with an expected date |
| Filters | supplier |
| Update | Computed on request from the authoritative tables |
| Missing data | Undefined when no received order had an expected date |
| Currency | not money |
| Limitations | Orders without an expected date are not measurable and are excluded. |
| Owner | Procurement |
| Permission | `analytics_procurement.read` |

### `procurement.overdue_orders` — Overdue purchase orders

Issued or partly received orders whose expected date is before the end of the range and that are not fully received now.

| | |
|---|---|
| Kind | count |
| Source fields | `purchase_orders.status`, `purchase_orders.expected_delivery` |
| Calculation | count |
| Filters | supplier |
| Update | Computed on request from the authoritative tables |
| Missing data | 0 |
| Currency | not money |
| Limitations | Point in time. |
| Owner | Procurement |
| Permission | `analytics_procurement.read` |

### `procurement.po_count` — Purchase orders issued (count)

Purchase orders issued in the range.

| | |
|---|---|
| Kind | count |
| Source fields | `purchase_orders.issued_at` |
| Calculation | count |
| Filters | supplier |
| Update | Computed on request from the authoritative tables; daily values snapshotted hourly |
| Missing data | 0 |
| Currency | not money |
| Limitations | None known. |
| Owner | Procurement |
| Permission | `analytics_procurement.read` |

### `procurement.po_value` — Purchase orders issued

Value of purchase orders issued to suppliers in the range (lines plus delivery fee and tax), TTD only.

| | |
|---|---|
| Kind | money |
| Source fields | `purchase_orders.issued_at`, `purchase_order_lines.line_total_minor`, `purchase_orders.delivery_fee_minor`, `purchase_orders.tax_minor` |
| Calculation | sum over issued POs |
| Filters | supplier |
| Update | Computed on request from the authoritative tables; daily values snapshotted hourly |
| Missing data | 0 |
| Currency | TTD minor units (cents). Other currencies are excluded, never converted. |
| Limitations | Spend committed, not paid: supplier invoices are not recorded. |
| Owner | Procurement |
| Permission | `analytics_procurement.read` |

### `procurement.rejected_packs` — Packs rejected at receiving

Packs rejected on delivery in the range.

| | |
|---|---|
| Kind | number |
| Source fields | `goods_receipt_lines.rejected_packs` |
| Calculation | sum(rejected_packs) |
| Filters | supplier |
| Update | Computed on request from the authoritative tables; daily values snapshotted hourly |
| Missing data | 0 |
| Currency | not money |
| Limitations | Packs of different products are added as counts of packs. |
| Owner | Procurement |
| Permission | `analytics_procurement.read` |

## Production

### `production.accepted_kits` — Kits accepted

Produced minus rejected. Reworked kits are part of accepted and are not added again.

| | |
|---|---|
| Kind | count |
| Source fields | `production_yield_records.accepted_kits` |
| Calculation | sum(accepted_kits) |
| Filters | meal |
| Update | Computed on request from the authoritative tables; daily values snapshotted hourly |
| Missing data | 0 |
| Currency | not money |
| Limitations | None known. |
| Owner | Kitchen |
| Permission | `analytics_production.read` |

### `production.batch_hours` — Batch completion time

Average hours from a batch starting to completing, for batches completed in the range.

| | |
|---|---|
| Kind | hours |
| Source fields | `production_batches.started_at`, `production_batches.completed_at` |
| Calculation | avg(completed_at − started_at) |
| Filters | meal |
| Update | Computed on request from the authoritative tables |
| Missing data | Undefined with no completed batches |
| Currency | not money |
| Limitations | Demo batches are completed within minutes and distort this figure. |
| Owner | Kitchen |
| Permission | `analytics_production.read` |

### `production.completion_rate` — Batch completion

Completed batches ÷ batches (not cancelled) with a production date in the range.

| | |
|---|---|
| Kind | ratio |
| Source fields | `production_batches.status` |
| Calculation | completed / not cancelled |
| Filters | meal |
| Update | Computed on request from the authoritative tables |
| Missing data | Undefined with no batches |
| Currency | not money |
| Limitations | Batches still in progress count against completion until they complete. |
| Owner | Kitchen |
| Permission | `analytics_production.read` |

### `production.planned_kits` — Kits planned

Kits planned for batches with a production date in the range (cancelled batches excluded).

| | |
|---|---|
| Kind | count |
| Source fields | `production_batches.planned_kits` |
| Calculation | sum(planned_kits) |
| Filters | meal |
| Update | Computed on request from the authoritative tables; daily values snapshotted hourly |
| Missing data | 0 |
| Currency | not money |
| Limitations | None known. |
| Owner | Kitchen |
| Permission | `analytics_production.read` |

### `production.produced_kits` — Kits produced

Kits assembled (before rejection) for batches in the range.

| | |
|---|---|
| Kind | count |
| Source fields | `production_yield_records.produced_kits` |
| Calculation | sum(produced_kits) |
| Filters | meal |
| Update | Computed on request from the authoritative tables; daily values snapshotted hourly |
| Missing data | 0 for batches without recorded output |
| Currency | not money |
| Limitations | Batches without recorded output contribute nothing. |
| Owner | Kitchen |
| Permission | `analytics_production.read` |

### `production.quality_holds` — Quality holds

Quality holds placed in the range.

| | |
|---|---|
| Kind | count |
| Source fields | `quality_holds.created_at` |
| Calculation | count |
| Filters | none |
| Update | Computed on request from the authoritative tables; daily values snapshotted hourly |
| Missing data | 0 |
| Currency | not money |
| Limitations | None known. |
| Owner | Quality |
| Permission | `analytics_production.read` |

### `production.rejected_kits` — Kits rejected

Kits rejected at output.

| | |
|---|---|
| Kind | count |
| Source fields | `production_yield_records.rejected_kits` |
| Calculation | sum(rejected_kits) |
| Filters | meal |
| Update | Computed on request from the authoritative tables; daily values snapshotted hourly |
| Missing data | 0 |
| Currency | not money |
| Limitations | None known. |
| Owner | Kitchen |
| Permission | `analytics_production.read` |

### `production.reworked_kits` — Kits reworked

Kits fixed and accepted after rework (already inside Kits accepted).

| | |
|---|---|
| Kind | count |
| Source fields | `production_yield_records.reworked_kits` |
| Calculation | sum(reworked_kits) |
| Filters | meal |
| Update | Computed on request from the authoritative tables; daily values snapshotted hourly |
| Missing data | 0 |
| Currency | not money |
| Limitations | Never added to accepted. |
| Owner | Kitchen |
| Permission | `analytics_production.read` |

### `production.waste_records` — Waste records

Kitchen waste records made in the range (quantities have different units and are not added).

| | |
|---|---|
| Kind | count |
| Source fields | `production_waste.created_at` |
| Calculation | count |
| Filters | none |
| Update | Computed on request from the authoritative tables; daily values snapshotted hourly |
| Missing data | 0 |
| Currency | not money |
| Limitations | Units differ by record; see the production report for quantities. |
| Owner | Kitchen |
| Permission | `analytics_production.read` |

## Commerce

### `revenue.charged_value` — Charged value

Amount asked of customers: billing cycles with at least one payment attempt started in the range, each counted once however many retries it took.

| | |
|---|---|
| Kind | money |
| Source fields | `billing_cycles.total_minor`, `payments.billing_cycle_id`, `payments.created_at` |
| Calculation | sum(total_minor) over distinct billing cycles having a payment created in the range |
| Filters | plan |
| Update | Computed on request from the authoritative tables |
| Missing data | 0 |
| Currency | TTD minor units (cents). Other currencies are excluded, never converted. |
| Limitations | A cycle whose attempts span two ranges counts in each range it was attempted. |
| Owner | Finance |
| Permission | `analytics_revenue.read` |

### `revenue.collected` — Collected

Successful payments in the range, before refunds. Money received, not profit and not recognised revenue.

| | |
|---|---|
| Kind | money |
| Source fields | `payments.amount_minor`, `payments.succeeded_at`, `payments.currency` |
| Calculation | sum(amount_minor) where succeeded_at in the range (later refunds do not change when it was collected) |
| Filters | plan |
| Update | Computed on request from the authoritative tables; daily values snapshotted hourly |
| Missing data | 0 |
| Currency | TTD minor units (cents). Other currencies are excluded, never converted. |
| Limitations | Retries cannot double-count: only an attempt that succeeded has succeeded_at. Includes test-provider payments. |
| Owner | Finance |
| Permission | `analytics_revenue.read` |

### `revenue.collected_test` — Collected through the test provider

The part of Collected taken by the simulated test payment provider. Not real money.

| | |
|---|---|
| Kind | money |
| Source fields | `payments.provider`, `payments.amount_minor`, `payments.succeeded_at` |
| Calculation | Collected restricted to provider = 'testpay' |
| Filters | plan |
| Update | Computed on request from the authoritative tables; daily values snapshotted hourly |
| Missing data | 0 |
| Currency | TTD minor units (cents). Other currencies are excluded, never converted. |
| Limitations | None: the provider is recorded on every payment. |
| Owner | Finance |
| Permission | `analytics_revenue.read` |

### `revenue.cycles_failed` — Weeks left unpaid

Billing cycles (weeks) whose payment finally failed, by due date in the range.

| | |
|---|---|
| Kind | count |
| Source fields | `billing_cycles.status`, `billing_cycles.due_at` |
| Calculation | count where status = failed and due_at in the range |
| Filters | plan |
| Update | Computed on request from the authoritative tables |
| Missing data | 0 |
| Currency | not money |
| Limitations | A week the customer later paid moves to paid and leaves this count. |
| Owner | Finance |
| Permission | `analytics_revenue.read` |

### `revenue.failed_payments` — Failed payment attempts

Payment attempts that failed, by when they were last updated in the range.

| | |
|---|---|
| Kind | count |
| Source fields | `payments.status`, `payments.updated_at` |
| Calculation | count where status = failed |
| Filters | plan |
| Update | Computed on request from the authoritative tables; daily values snapshotted hourly |
| Missing data | 0 |
| Currency | not money |
| Limitations | Each retry is its own attempt; see revenue.cycles_failed for weeks that ended unpaid. |
| Owner | Finance |
| Permission | `analytics_revenue.read` |

### `revenue.net_collected` — Net collected

Collected minus Refunded in the range. Cash view only: no revenue recognition policy exists.

| | |
|---|---|
| Kind | money |
| Source fields | `payments`, `refunds` |
| Calculation | revenue.collected − revenue.refunded |
| Filters | plan |
| Update | Computed on request from the authoritative tables; daily values snapshotted hourly |
| Missing data | 0 |
| Currency | TTD minor units (cents). Other currencies are excluded, never converted. |
| Limitations | A refund in this range can relate to a payment collected earlier. |
| Owner | Finance |
| Permission | `analytics_revenue.read` |

### `revenue.ordered_value` — Ordered value

Total of orders made in the range (paid weeks turned into orders), not cancelled. What customers ordered, not what was collected.

| | |
|---|---|
| Kind | money |
| Source fields | `orders.total_minor`, `orders.created_at`, `orders.status`, `orders.currency` |
| Calculation | sum(total_minor) over orders created in the range, status not pending or cancelled, currency TTD |
| Filters | plan |
| Update | Computed on request from the authoritative tables; daily values snapshotted hourly |
| Missing data | 0 when there are no orders |
| Currency | TTD minor units (cents). Other currencies are excluded, never converted. |
| Limitations | Includes test-provider and free (promotion) orders; see revenue.collected_test for the test share. |
| Owner | Finance |
| Permission | `analytics_revenue.read` |

### `revenue.outstanding` — Outstanding

Billing cycles due in the range and not paid now (due or failed), in TTD.

| | |
|---|---|
| Kind | money |
| Source fields | `billing_cycles.status`, `billing_cycles.total_minor` |
| Calculation | sum(total_minor) where status in (due, failed) and due_at in the range |
| Filters | plan |
| Update | Computed on request from the authoritative tables |
| Missing data | 0 |
| Currency | TTD minor units (cents). Other currencies are excluded, never converted. |
| Limitations | Point-in-time: computed from current statuses. |
| Owner | Finance |
| Permission | `analytics_revenue.read` |

### `revenue.refunded` — Refunded

Refunds that succeeded in the range.

| | |
|---|---|
| Kind | money |
| Source fields | `refunds.amount_minor`, `refunds.status`, `refunds.completed_at` |
| Calculation | sum(amount_minor) where status = succeeded and completed_at in the range |
| Filters | plan |
| Update | Computed on request from the authoritative tables; daily values snapshotted hourly |
| Missing data | 0 |
| Currency | TTD minor units (cents). Other currencies are excluded, never converted. |
| Limitations | A refund is one row per request key, so a resubmitted form cannot double-count. |
| Owner | Finance |
| Permission | `analytics_revenue.read` |

## Subscriptions

### `subscriptions.active` — Active subscriptions

Subscriptions activated on or before the end of the range and not cancelled or expired by then (paused ones count as active).

| | |
|---|---|
| Kind | count |
| Source fields | `subscription_events.type`, `subscription_events.created_at` |
| Calculation | count of subscriptions with an 'activated' event ≤ end and no 'cancelled'/'expired' event ≤ end |
| Filters | plan |
| Update | Computed on request from the authoritative tables |
| Missing data | 0 |
| Currency | not money |
| Limitations | Reconstructed from the subscription event history, which exists for every subscription since Sprint 3. |
| Owner | Commerce |
| Permission | `analytics_revenue.read` |

### `subscriptions.cancelled` — Cancellations

Subscriptions whose cancellation took effect in the range (explicit cancellation events; payment failure alone is never a cancellation).

| | |
|---|---|
| Kind | count |
| Source fields | `subscription_events` |
| Calculation | count of 'cancelled' events in the range |
| Filters | plan |
| Update | Computed on request from the authoritative tables; daily values snapshotted hourly |
| Missing data | 0 |
| Currency | not money |
| Limitations | Scheduled cancellations that were withdrawn are not counted. |
| Owner | Commerce |
| Permission | `analytics_revenue.read` |

### `subscriptions.expired` — Expirations

Subscriptions that expired (ended without payment) in the range.

| | |
|---|---|
| Kind | count |
| Source fields | `subscription_events` |
| Calculation | count of 'expired' events |
| Filters | plan |
| Update | Computed on request from the authoritative tables; daily values snapshotted hourly |
| Missing data | 0 |
| Currency | not money |
| Limitations | None known. |
| Owner | Commerce |
| Permission | `analytics_revenue.read` |

### `subscriptions.new` — New subscriptions

Subscriptions activated (first payment confirmed) in the range.

| | |
|---|---|
| Kind | count |
| Source fields | `subscription_events` |
| Calculation | count of 'activated' events in the range |
| Filters | plan |
| Update | Computed on request from the authoritative tables; daily values snapshotted hourly |
| Missing data | 0 |
| Currency | not money |
| Limitations | A subscription that never paid is not counted. |
| Owner | Commerce |
| Permission | `analytics_revenue.read` |

### `subscriptions.pauses` — Pauses scheduled

Pauses customers scheduled in the range.

| | |
|---|---|
| Kind | count |
| Source fields | `subscription_events` |
| Calculation | count of 'pause_scheduled' events |
| Filters | plan |
| Update | Computed on request from the authoritative tables; daily values snapshotted hourly |
| Missing data | 0 |
| Currency | not money |
| Limitations | None known. |
| Owner | Commerce |
| Permission | `analytics_revenue.read` |

### `subscriptions.plan_changes` — Plan changes

Plan changes customers made in the range.

| | |
|---|---|
| Kind | count |
| Source fields | `subscription_changes.created_at` |
| Calculation | count |
| Filters | none |
| Update | Computed on request from the authoritative tables; daily values snapshotted hourly |
| Missing data | 0 |
| Currency | not money |
| Limitations | Counts requests, by when they were made. |
| Owner | Commerce |
| Permission | `analytics_revenue.read` |

### `subscriptions.renewals` — Renewals paid

Renewal weeks paid in the range (a paid renewal billing cycle).

| | |
|---|---|
| Kind | count |
| Source fields | `billing_cycles.kind`, `billing_cycles.status`, `payments.succeeded_at` |
| Calculation | count of renewal cycles with a payment that succeeded in the range |
| Filters | plan |
| Update | Computed on request from the authoritative tables; daily values snapshotted hourly |
| Missing data | 0 |
| Currency | not money |
| Limitations | Free (promotion) renewal weeks have no payment and are not counted. |
| Owner | Commerce |
| Permission | `analytics_revenue.read` |

