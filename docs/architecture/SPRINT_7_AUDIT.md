# Sprint 7 audit: delivery and fulfillment

Audited 14 November 2026 on branch `sprint-7/delivery-fulfillment` (from Sprint 6
commit `e2a6416`). Where the earlier reports and the code disagree, the code is
taken as true; each such case is marked **Repo differs**.

## 1. Commerce → production → fulfillment, as built

1. The commerce scheduler makes **one order per subscription cycle**
   (`orders_cycle_key UNIQUE (subscription_cycle_id)`), at the selection cutoff, and only for a
   week that is paid or free under a promotion (`orders.payment_id` is NULL only then).
   Unpaid weeks, carts and drafts never become orders (`orders.SourceLine` comment, `DemandLines`).
2. Orders start as `confirmed`. The allowed statuses are already defined in `00007`:
   `pending, confirmed, awaiting_production, in_production, ready_for_fulfillment,
   out_for_delivery, delivered, cancelled`.
3. Production takes a **demand snapshot** per week (`production_demand_lines`: order, meal,
   quantity, recipe version). A plan is built from a snapshot, and its items become **batches**
   (one batch per plan item and meal).
4. `production.Complete(batch)` is refused unless the batch is `awaiting_quality_check`, nothing in
   `remaining()` is outstanding, and `packed_kits = accepted_kits`. In one transaction it then:
   - sets the batch to `completed` / `quality_status = passed`;
   - writes `production.batch_completed` and **`production.ready_for_fulfillment`**;
   - calls `orders.Advance(..., from {awaiting_production, in_production} → ready_for_fulfillment)`
     for every order in the plan whose meals' batches are **all** complete (`readyOrders`).
5. Nothing later than `ready_for_fulfillment` exists today. `out_for_delivery` and `delivered` are
   defined but never written.

The only code that changes `orders.status` is `orders.Advance`. It moves an order forward, and only from
the statuses the caller lists, and it writes `order_status_history`. Sprint 7 uses the same function.
Delivery never updates `orders` directly.

## 2. Order and address contracts

| Need (brief) | What exists |
|---|---|
| Order, lines | `orders`, `order_items` (immutable: `forbid_change` trigger), meal version, quantity, servings |
| Subscription week | `orders.week_start` (Monday), `subscription_cycle_id`, `billing_cycle_id` |
| Paid eligibility | An order exists only for a paid or free week. The payment is `orders.payment_id → payments.status` (`succeeded`, `refunded`, `partially_refunded` …) |
| Delivery address | `orders.address_snapshot` jsonb, copied when the order is made (`orders.Address`: recipient_name, phone, line1/2, area, city, region, region_label, country, instructions). There are no coordinates. |
| Delivery preferences | Only `instructions` on the address. No delivery-day or window preference exists. |
| Delivery window | `orders.delivery_window` jsonb exists but is **never written** (always NULL). |
| Delivery fee | `orders.delivery_fee_minor` is charged at checkout. Tax and delivery are zero and configurable (Sprint 3 decision). |
| Cancellation | **Repo differs:** there is no order-cancel workflow. `cancelled` is a valid status that nothing sets. |
| Refunds | `payments.Refund` (admin, `payments.refund` permission) refunds a **payment**. It does not change the order. |

**Missing:** delivery days, windows, and a customer reschedule preference. Sprint 7 must not invent them. They become configuration that ships empty.

## 3. Production readiness guarantees

- **Event:** `production.ready_for_fulfillment`. **Aggregate: the batch** (`aggregate_type
  production_batch`, `aggregate_id = batch id`), `dedupe_key ''`. The payload is `{code, meal_id,
  recipe_version_id, accepted_kits, production_date, plan_id}`. **It carries no order ids.**
  The brief speaks of order readiness; in the code, the event means *one batch* is ready. An
  order is ready only when every batch for its meals is complete. Fulfillment must work that out
  itself from `production_demand_lines`, the same way `readyOrders` does.
- **Dedupe:** `UNIQUE (type, aggregate_id, dedupe_key)`. A batch can complete only once, so there is
  one event per batch. Replays through the consumer offset can still deliver it twice.
- **Emitted when:** the quality check has passed, the batch is not on hold, nothing is outstanding,
  and packed kits equal accepted kits.
- **Quality holds:** `quality_holds` rows (`open/released/discarded`). A batch on hold cannot
  complete. **Repo differs:** `PlaceHold` accepts only `in_progress` or `awaiting_quality_check`
  batches, so production has no way to hold a batch *after* it completes. Fulfillment still checks
  for open holds at readiness, at verification and at release, which is cheap and protects against
  a future production change. A problem found at packing time becomes a **fulfillment batch flag**
  (an exception that blocks every obligation holding kits from that batch) rather than a write into
  production's tables.
- **Packaging:** `packaging_records` per batch: `packed_kits`, `labels_verified`, `completed_at`.
  Packaging is counted **per batch, not per order**. Nothing yet says which packed kit goes to
  which customer.

## 4. Traceability limits (kept as they are)

- Sprint 6 `batch_ingredient_lots` links ingredient lots to **production batches**.
- Sprint 7 links order packages to batches (one package item per meal, pointing at the batch that
  made it). Traceability therefore runs Supplier → Lot → **Batch** → Order package → Delivery. It is
  **batch-level**. Nothing records which lot portion went into which kit, and nothing in Sprint 7
  claims it does.

## 5. Event and job infrastructure (checked in code)

- `domain_events` is an append-only transactional outbox (`forbid_change` trigger), written through
  `platform/outbox.Record`.
- `event_consumer_offsets(consumer, last_event_id)` holds one row per consumer, and only
  `procurement` exists today. `procurement.Dispatch` locks its row `FOR UPDATE`, reads events after the
  offset that are older than a **settle delay** (an earlier transaction can commit later), enqueues jobs,
  and moves the offset in the same transaction. `Replay(fromID)` moves the offset back. `Deliveries()`
  reports waiting and failed jobs. **Verified:** the Sprint 6 report's claims (durable offsets,
  idempotent jobs, retries with backoff in `platform/jobs`, failures kept visible, replay) are true.
  The dispatcher, though, is **procurement-specific** code rather than a generic consumer. Sprint 7 will
  factor the loop into a shared helper rather than copy it.
- `platform/jobs`: `Enqueue` and `Runner`, with retries, backoff and requeue of stuck jobs.

## 6. Authorization and notifications

- RBAC: `permissions.key ~ '^[a-z][a-z_]*\.[a-z][a-z_]*$'` (resource.action), `roles`,
  `role_permissions`, seeded in migrations. Handlers check permissions through `identity.Principal`.
  `super_admin` and `admin` get every new permission (Sprint 6 pattern).
- Audit: `audit.Record(tx, Entry)` runs in the same transaction as the change.
- Alerts: `backoffice.Service.Raise` (deduplicated `admin_alerts`). Support cases: `support_cases`
  already has the category `delivery` but **no order or delivery link**.
- Email: commerce emails are a job (`commerce.JobEmail`) handled by `notifications.CommerceSender`,
  deduplicated through `notification_deliveries`. A send is logged but its result is **not stored**.
  `delivery_updates` is already declared as an **upcoming** customer topic (`vocab.UpcomingTopics`),
  so it cannot be set yet. There is no SMS or WhatsApp provider.
- **File storage: none.** Sprint 6 supplier documents store only metadata. Photo proof of delivery
  therefore stays **blocked** this sprint (brief §25 fallback).

## 7. Missing dependencies (business inputs)

None of the following exists, and Sprint 7 will not invent any of it: delivery partners or agreements, service days,
windows, capacities, delivery fees beyond the existing zero setting, partner rates, the proof-of-delivery
policy, the failed-delivery policy, the reschedule policy, return and food-handling procedures, approved
customer delivery email wording, cold-chain rules, label certification, and any geocoding or GPS provider.

**Repo differs (zones):** `00006_delivery_zones_tt.sql` records the business decision of
11 November 2026 that Mise delivers to all 15 Trinidad and Tobago regions. These are **customer-facing
serviceability** rows (`country, region, status`), not operational dispatch zones. They stay as they
are. Operational zones (code, capacity, service days) are new configuration, they ship empty, and they
are mapped onto these regions by explicit coverage rules.

## 8. Domain boundaries

- `internal/fulfillment` (new): obligations, packages, verification, release, and the readiness consumer.
- `internal/delivery` (extended; it already holds the zone-eligibility check): service zones, calendar,
  capacity, partners, couriers, deliveries, dispatch runs and stops, manifests, handoffs, tracking,
  proof, exceptions, reschedules, returns, costs, reconciliation, the carrier adapter, and reports.
- Commerce keeps orders. Changes go only through `orders.Advance`, called through an interface.
- Production keeps batches, holds and packaging. Fulfillment reads them through a `production` read
  interface and writes nothing in production tables.
- Inventory is untouched. Returned kits are **never** put back into stock.

## 9. Data-migration risks

- The migration is additive only (`00011_delivery_fulfillment.sql`). It adds no columns to
  commerce, production or inventory tables.
- Existing demo orders already at `ready_for_fulfillment` have no event history for fulfillment. The
  consumer starts its offset at **0**, so earlier readiness events are processed once (idempotent), and
  a manual "sync ready orders" action covers anything missed.
- `orders.delivery_window` stays unwritten. Sprint 7 keeps its own approved window on the delivery.
- Adding `delivery_updates` to customer topics changes the preferences screen. These are service
  messages about an order the customer has already paid for, so a customer with no saved choice counts
  as **on**, and they can turn it off. Sending is still blocked until the business approves the email wording (§7).

## 10. Sprint 7 plan

See `SPRINT_7_PLAN.md`.
