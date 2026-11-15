# Sprint 7 plan: delivery and fulfillment

Brief: `E:\FULL MISE DEV\SPRINT 7`. Audit: `SPRINT_7_AUDIT.md`. Branch `sprint-7/delivery-fulfillment`.

## 1. Existing contracts reused

- Orders: `orders`, `order_items`, `orders.address_snapshot` (`orders.Address`). Order status changes
  only through `orders.Advance`, to `out_for_delivery` and `delivered`.
- Production: batches, `quality_holds`, `packaging_records`, `production_demand_lines`. Read-only, through
  a `fulfillment.Production` interface.
- Events: `domain_events` through `outbox.Record`, and `event_consumer_offsets`. The procurement dispatcher loop
  becomes a shared `platform/consumer` helper, and procurement keeps its behaviour.
- Jobs, audit, alerts (`backoffice.Raise`), support cases (`support.Service.Create`), customer email
  jobs and `notification_deliveries`, the `vocab` topic `delivery_updates`.
- Region serviceability: `delivery_zones` and `delivery.Zones.Check`, unchanged.

## 2. Domain ownership

| Module | Owns |
|---|---|
| `fulfillment` | obligations, packages and items, verifications, release, readiness consumer, fulfillment events |
| `delivery` | service zones and region rules, service days, windows, capacity, partners and qualification, couriers, deliveries, dispatch runs and stops, manifests, handoffs, tracking events, proofs, exceptions (fulfillment and delivery stages), reschedule requests, returns, cost records, reconciliations, settings, carrier adapters, delivery reports |

Fulfillment releases an obligation through `delivery.CreateFromRelease`, called in the same
transaction (the service interface `fulfillment.Deliveries`). Delivery calls back through
`delivery.Fulfillment` (`MarkHandedOver`, `MarkFulfilled`, `MarkException`) and `delivery.Orders`
(`Advance`). No module writes another module's tables.

## 3. Database (`00011_delivery_fulfillment.sql`, additive)

Fulfillment:
- `fulfillment_obligations`: `order_id UNIQUE` (one per order, which is one per subscription cycle),
  customer, week_start, plan_id, number `FUL-…`, `status`, `version`, kits_required, timestamps.
- `fulfillment_packages`: code `PKG-<order>-<n>` UNIQUE, obligation, sequence, `verification_status`,
  `label_printed_at`, `handoff_status`.
- `fulfillment_package_items`: package, meal, meal_name, quantity, `batch_id` and `recipe_version_id`
  (from the demand line and batch), with `UNIQUE (obligation_id, meal_id)` through the package.
- `fulfillment_verifications`: append-only, one row per item check (counted quantity, scanned batch
  code, recipe confirmed, packaging and label checks, result, discrepancy note, actor).
- `fulfillment_events`: append-only transition history (from, to, reason, actor).

Delivery configuration:
- `delivery_service_zones` (code UNIQUE, name, status draft/active/inactive, notes, fee_policy_ref).
- `delivery_zone_regions` (zone, country, region → FK `delivery_zones (country, region)`,
  `UNIQUE (country, region)`). This is the coverage rule.
- `delivery_service_days` (date, zone nullable meaning all zones, status open/closed, cutoff_at, notes).
- `delivery_windows` (service day, label, start, end, status draft/approved).
- `delivery_capacity` (date, optional zone, window, partner and courier, capacity, notes). The scope is unique.
- `delivery_partners`, `delivery_partner_zones`, `delivery_partner_criteria` (qualification checks),
  `delivery_partner_events` (status and approval history, append-only).
- `delivery_couriers` (name, phone, partner, optional `user_id` for courier login scope, status).
- `delivery_settings` (singleton): accepted proof methods and whether the proof policy is approved,
  reschedule policy, failure reasons, return dispositions, whether the partner exception policy is allowed,
  whether the label is certified, whether delivery emails are approved.

Delivery operations:
- `deliveries`: `obligation_id UNIQUE`, order, customer, `status`, `version`, zone and zone_source
  (coverage_rule/manual/none), service_date, window, schedule_confirmed_at,
  **address snapshot (immutable trigger)**, address_validation, confirmation code hash, attempt.
- `dispatch_runs` (number, date, partner, courier, zone, status, version).
- `dispatch_stops`: run, delivery, sequence, status. A **partial unique index** allows one active
  stop per delivery, which is how duplicate assignments are prevented.
- `dispatch_manifests` (run, revision, content jsonb, sha256, current/superseded).
- `delivery_handoffs` and `delivery_handoff_packages` (accepted, rejected or missing per package).
- `delivery_tracking_events`: append-only, with `UNIQUE (delivery_id, idempotency_key)` and a source
  (staff/partner/customer/system).
- `delivery_proofs`: one accepted proof per delivery (partial unique), with an idempotency key.
- `delivery_exceptions` and `delivery_exception_events`. They cover the fulfillment stage too and are
  linked to `support_cases` through `delivery_case_links`.
- `delivery_reschedule_requests`, `delivery_returns`, `delivery_cost_records`, `delivery_reconciliations`.
- `delivery_notifications`: queued, sent, failed or skipped, with the provider outcome.

## 4. Fulfillment state machine

```text
ready_for_verification ─▶ verification_in_progress ─▶ verified ─▶ ready_for_dispatch ─▶ handed_over ─▶ fulfilled
        │                        │                        │               │                  │
        └──────────────┬─────────┴────────────────────────┴───────────────┴──────────────────┘
                       ▼
                   exception ─▶ (resolved) back to the state it left, or ─▶ cancelled
```

`awaiting_production` is not stored: an order without an obligation *is* awaiting production
(`orders.status` already says so). Every transition locks the row, checks the version and the
allowed `from` states, and writes `fulfillment_events` and audit. `fulfilled` is set only when the
delivery records `delivered` **with accepted proof**.

## 5. Delivery state machine (single authority: `deliveries.status`)

```text
pending_assignment → assigned → ready_for_pickup → picked_up → in_transit → arriving → delivered
                      ↑   │            │               │            │           │
                      │   └── unassign ┘               └────────────┴───────────┴─▶ delivery_failed
                      └──────────── reattempt / reschedule ◀──────────────────────── delivery_failed
delivery_failed → returned ;  pending_assignment|assigned|ready_for_pickup → cancelled
```

The transition table lives in `delivery/state.go`. Any other move is refused with `invalid_transition`.
`delivered` requires an accepted proof in the same transaction. A correction (for example a delivered
order that was not actually delivered) is its own `correction` event, needs `delivery_settings.manage`
and a reason, and is never an overwrite.

## 6. Readiness consumer

The consumer is `fulfillment`, with a row in `event_consumer_offsets` starting at 0. The shared dispatcher
enqueues `fulfillment.batch_ready {event_id, batch_id}` for `production.ready_for_fulfillment`. The
handler:
1. Loads the batch. If it is not visible yet, it returns an error, which means a retry.
2. Finds the orders in the batch's plan snapshot that contain its meal.
3. For each order, it confirms that the order status is `ready_for_fulfillment` (commerce agrees every
   batch is complete), that payment is eligible (`succeeded`, or no payment with a zero total), that
   every batch for its meals is `completed`, that no batch has an **open hold**, and that packaging is
   complete with labels verified.
4. It inserts the obligation, packages and items with `ON CONFLICT (order_id) DO NOTHING`. A replay
   therefore does nothing.
5. Contradictions (refunded payment, cancelled order, missing batch) create an obligation in
   `exception` together with a visible exception record. They are never skipped silently.
6. Orders that are not ready yet (other batches still open) are a no-op; their last batch's event will
   create them. A "sync ready orders" admin action covers a missed event.

## 7. Dispatch architecture

Release creates a `delivery` in `pending_assignment`. Its zone comes from the coverage rule if one
exists; otherwise there is no zone and the delivery is flagged `unzoned` until someone assigns one
manually. The planner chooses an **open service day** (and an approved window, if any), which confirms
the schedule. A run is (date, partner, optional courier and zone). Adding a stop locks the run and the
delivery, checks the partner's approval, coverage and capacity (all matching capacity rows are locked
`FOR UPDATE`), and counts existing active stops. If no capacity is configured, capacity is **unknown**
and the stop needs an explicit override with `delivery.dispatch` and a reason. Finalizing a run writes a
manifest (snapshot and hash). A stop change after that supersedes the manifest and returns the run to
draft. Releasing moves the stops to `ready_for_pickup`. A handoff, per package, moves them to `picked_up`.

## 8. Partner adapter

`delivery.Carrier` has `Capabilities()` and the methods `CreateShipment`, `CancelShipment`, `RequestStatus`,
`ReceiveTrackingUpdate`, `RetrieveProof` and `ReportException`, each returning `ErrUnsupported` when the
capability is missing. `ManualCarrier` is the working adapter: staff hand over the manifest, and status
is recorded by staff with source `partner` when the partner reports it. The partner webhook design
(HMAC signature, timestamp window, idempotency key, partner identity, rate limit) is a verifier that is
written and tested but **not routed**, because there is no provider specification.

## 9. Proof of delivery

Methods: `confirmation_code`, `recipient_acknowledgment`, `staff_confirmation`,
`partner_confirmation`, `photo`. `photo` is refused (there is no secure storage, audit §6).
`delivery_settings.proof_methods` starts **empty, so the policy is not configured and `delivered`
is blocked**. The block is visible as an operational blocker. A confirmation code is 6 digits, random per
delivery, stored as an HMAC hash keyed to the delivery id, shown to the customer, and checked against
that delivery only. Duplicate submissions with the same idempotency key return the first proof.

## 10. RBAC

New permissions: those in brief §45, plus `delivery.reconcile` and `delivery.read_assigned`
(couriers: only the stops of runs whose courier is linked to their user). New roles:
`fulfillment_manager`, `fulfillment_staff`, `dispatch_manager`, `dispatcher`,
`delivery_coordinator`, `courier`. `super_admin` and `admin` get all of them. Support roles gain
`delivery.read` and `delivery_exceptions.read`. Partners have no login: there is no partner portal,
so partners cannot see shipments, their own or anyone else's. A future API partner is scoped by
its partner id.

## 11. Customer web

Customer API (session, own orders only): `GET /api/v1/orders/{id}/delivery` returns the timeline,
the confirmed date and window, the address and instructions, the customer-safe exception summary and the
confirmation code. `POST /api/v1/orders/{id}/delivery/support` creates a support case (category `delivery`)
linked to the delivery, and `POST …/reschedule-requests` records a request (no promise unless a policy is
configured). The order page shows a mobile-first delivery card and timeline with friendly labels, and
never shows internal dispatch or partner names.

## 12. Admin UI

A "Delivery & fulfillment" nav group with pages for Overview, Fulfillment queue (and a mobile verify
screen), Delivery calendar, Dispatch planning, Dispatch runs (and a printable manifest), Partners,
Couriers, Zones, Exceptions, Returns, Reconciliation, Delivery reports and Settings. Customer 360 and the
order detail gain delivery panels. It reuses the existing admin shell, `supply-ui` components and design tokens.

## 13. Test strategy

Go integration tests against real Postgres (`delivery_*_test.go`) for the 30 brief items, the concurrency
cases in §49 (goroutines against one database), and RBAC. Playwright covers dispatch and delivery
end to end through the UI, the customer timeline, and the failed-delivery path. The full Sprint 1–6
suites are run again.

## 14. Sprint 8 integration contracts

Events: `fulfillment.*` and `delivery.*` (§48), each with aggregate = obligation or delivery id and
dedupe key = transition or event id. The report kind is `delivery`. Tracking events have explicit
sources, so analytics can tell staff-recorded data from partner-reported data.

## Progress

- [x] Stage 1: audit and plan
- [x] Stage 2: fulfillment foundation. Migration `00011_delivery_fulfillment.sql` (all Sprint 7 tables), shared
  `platform/consumer` (procurement moved onto it, its tests still pass), `orders.Facts`, `production.KitsForOrder/OrdersForBatch/
  BatchState`, `internal/fulfillment` (consumer, obligations, verification, discrepancies, batch flags, release, labels, trace),
  delivery exceptions and `CreateFromRelease`. Tests: 5 in `delivery_test.go` pass (14 Nov).
- [x] Stage 3: delivery configuration: zones + coverage rules, calendar and windows, capacity (unknown ≠ unlimited),
  partners with qualification criteria and approval history, couriers (login-scoped), settings with blockers.
- [x] Stage 4: dispatch: scheduling, address correction, runs, stops (row locks + one-active-stop index), manual sequence,
  manifests (frozen, superseded on reopen, CSV no-store), release, per-package custody handoffs.
- [x] Stage 5: tracking (append-only, sourced, verified-location only), proof (policy-gated, HMAC codes, idempotent),
  completion, customer view and routes, delivery emails recorded as queued/sent/failed/skipped; `delivery_updates` topic live.
- [x] Stage 6: failure + exception, reattempt, returns (quarantine + review), cancel, explicit corrections, reschedule
  requests and decisions, support case links, costs, reconciliation, overview, report. OpenAPI 1.7.0 (59 paths, 79 schemas)
  and the TS client regenerated. Go: 147 top-level tests (incl. 11 Sprint 7 server tests + 4 delivery unit tests) all pass (14 Nov). Two Sprint 2 notification tests updated for the delivery_updates service topic.
- [x] Stage 7: admin UI (Delivery & fulfillment nav: overview, fulfillment queue + phone verify screen, calendar, planning,
  runs + printable manifest + CSV route, delivery detail for couriers, partners, couriers, zones, exceptions, returns,
  reconciliation, reports, settings, Customer 360 deliveries tab) and customer order delivery card/timeline/support/reschedule.
  `misectl delivery seed-demo [-ready]`. Playwright `delivery-fulfillment.spec.ts` (4 tests) written; each passed (14 Nov).
  Fixes found by the browser test: couriers could not read the proof policy (settings now need only admin.access);
  an unlinked courier login crashed "My runs".
- [x] Stage 8 (14 Nov): Go 147/147, Vitest 43/43, Playwright 55/55 (one worker, three batches), screenshots in
  docs/screenshots/sprint-7, report docs/SPRINT_7_REPORT.md.
