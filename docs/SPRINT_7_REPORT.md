# Sprint 7 report: Delivery, Fulfillment, Dispatch and Customer Delivery Experience

Branch `sprint-7/delivery-fulfillment` (from `sprint-6/procurement-inventory`, `e2a6416`). Brief:
`E:\FULL MISE DEV\SPRINT 7`. Audit: `docs/architecture/SPRINT_7_AUDIT.md`. Plan, state machines and
progress: `docs/architecture/SPRINT_7_PLAN.md`. Written 14 November 2026; every result below was
produced that day (§18).

Status words used throughout:
- **Verified**: exercised by an automated test against real PostgreSQL, or end to end in a browser.
- **Implemented, not verified**: built and type-checked, the screen renders (screenshot), no test drives it.
- **Blocked**: needs a business input (§20); the software ships it empty and says so on screen.
- **Deferred**: out of this sprint's scope.

**Review fixes, 15 November 2026.** Two issues raised in review were fixed before Sprint 8 (§22):
delivery dates are now bound to the order's week by an approved rule, and a corrected delivered outcome
is recorded by commerce as a dispute that order views, customer pages and reports all show.

Nothing here is live-delivery ready. No partner, agreement, zone, delivery day, window, fee, rate,
capacity, proof policy, failure policy, return procedure, email wording or cold-chain rule was
invented. All partners, couriers, zones, days and deliveries in the screenshots and the development
database are local demo or test records (marked `demo` or "LOCAL DEMO ONLY") and are not evidence of
real operations.

## 1. Repository audit findings

Full detail in `SPRINT_7_AUDIT.md`. Where reports and code disagreed, the code was taken as true:

- `production.ready_for_fulfillment` is emitted **per batch** (aggregate `production_batch`), not per
  order, with payload `{code, meal_id, recipe_version_id, accepted_kits, production_date, plan_id}` and
  no order ids. Commerce moves an order to `ready_for_fulfillment` in the same transaction, only when
  every batch for its meals is complete. Fulfillment therefore works out which orders a batch completes.
- Production cannot place a quality hold on a *completed* batch. Fulfillment re-checks holds anyway
  and adds its own **batch flag** (a blocking exception) for problems found at packing.
- There is no order-cancel workflow. Refunds act on payments only. `orders.delivery_window` is never written.
- The Sprint 6 "generic" consumer was procurement-specific code. Its claimed behaviour (durable
  offset, idempotent jobs, retries, visible failures, replay) is true, and it is now a shared
  `platform/consumer` used by both procurement and fulfillment.
- `00006` records the 11 Nov 2026 decision that all 15 regions are served. It is kept as
  customer-facing serviceability. Operational zones are new configuration, mapped to regions by
  explicit rules, and they ship empty.
- There is no file storage, so photo proof is blocked (brief §25 fallback).

## 2. Architecture decisions

- Two modules: `internal/fulfillment` (obligations, packages, verification, release, readiness
  consumer) and `internal/delivery` (everything after release). Fulfillment depends on delivery types.
  Delivery calls back through a `Fulfillment` interface, inside the caller's transaction.
- Commerce stays the owner of orders: delivery moves order status only through `orders.Advance`
  (`out_for_delivery` at handoff, `delivered` with proof). Production and inventory tables are read
  through `production.KitsForOrder/OrdersForBatch/BatchState`; nothing in them is written.
- One obligation per order (`UNIQUE(order_id)`, plus an advisory lock), one delivery per obligation,
  and one active stop per delivery (partial unique index). These database constraints are the
  final guard against duplicates.
- `deliveries.status` is the single delivery authority. Its state machine is in `delivery/state.go`.
  Undoing an outcome is a separate, permissioned, reasoned `correction` event.
- Confirmation codes are derived per delivery and attempt with an HMAC of the server key. They are
  never stored and match only that delivery.
- Capacity with no configured row is **unknown**, never unlimited. Going beyond known or unknown
  capacity needs an explicit, reasoned override from someone with `delivery.dispatch`.
- `delivery_updates` became a live email topic. It is a service topic: on until the customer turns
  it off, and sending is blocked until the wording is approved.

## 3. Implemented modules

| Area | Where | Status |
|---|---|---|
| Shared event consumer | `platform/consumer` | Verified |
| Readiness consumer, obligations, packages, verification, discrepancies, batch flags, release, labels, trace | `internal/fulfillment` | Verified |
| Settings with blockers, zones and coverage, calendar and windows, capacity | `delivery/config.go` | Verified |
| Partners, qualification, approval history, couriers | `delivery/partners.go` | Verified |
| Deliveries, scheduling, address correction, courier scope | `delivery/deliveries.go` | Verified |
| Runs, stops, sequence, manifests, release, custody handoffs | `delivery/dispatch.go` | Verified |
| Tracking, proof, completion, failure, reattempt, cancel, correction, returns | `delivery/tracking.go` | Verified |
| Reschedule, support links, customer view | `delivery/customer.go` | Verified |
| Customer emails (queued, sent, failed, skipped) | `delivery/notify.go`, `notifications/delivery.go` | Verified |
| Costs, reconciliation, overview, report | `delivery/reconcile.go` | Verified (costs: implemented, not verified by a test) |
| Carrier adapter + manual adapter + webhook verifier (not routed) | `delivery/carrier.go` | Verified (unit) |
| Demo seeding | `misectl delivery seed-demo [-ready]` | Used by the e2e and screenshots |

## 4. Database migrations

`00011_delivery_fulfillment.sql` is additive. It creates 34 tables (a numbering counter; fulfillment obligations, packages,
items, append-only verifications and events; settings; service zones and region rules; service days,
windows and capacity; partners, partner zones, criteria and append-only events; couriers; deliveries
with an address guard trigger; dispatch runs, stops and manifests; append-only handoffs and handoff
packages; append-only tracking events and proofs; exceptions, exception events and support-case
links; reschedule requests; returns and return packages; cost records; reconciliations; notification
records). It also adds the `fulfillment` consumer offset, 23 permissions and 6 roles. It does not alter
any commerce, production or inventory table. Up and down are written.

`00012_delivery_week_and_outcomes.sql` (review fixes, additive): the delivery-week rule columns on
`delivery_settings` (with a check that a rule has both ends and an approved reference),
`deliveries.order_week` (backfilled from the order, then NOT NULL), and the commerce-owned,
append-only `order_delivery_outcomes`. The development database is at version 12. Up was applied to the
development database (version 11) and runs on every test harness start.

## 5. API endpoints

OpenAPI 1.7.0 added 59 paths (70 operations) and 79 schemas, and the TypeScript client was
regenerated (`api.delivery.*` and three `api.commerce.*` customer methods). The contract test confirms
every served route is documented. Groups: `/admin/fulfillment` (queue, item, verify, release, labels,
trace, sync, consumer, batch flag); `/admin/delivery/{settings,zones,coverage,calendar,capacity,
partners,couriers,costs,exceptions,deliveries,reschedule-requests,returns,dispatch-runs,overview}`;
`/admin/reports/delivery`; and for customers `/me/orders/{id}/delivery` (+ `/support`,
`/reschedule-requests`). 1.7.1 (review fixes) adds `delivery_outcome` to `Order` and `OrderSummary`,
the delivery-week rule to the settings schemas, `order_week` to `Delivery` and `disputed_outcomes` to
the overview. Customer routes use the customer session and answer 404 for another
customer's order.

## 6. Fulfillment workflow

`ready_for_verification → verification_in_progress → verified → ready_for_dispatch → handed_over →
fulfilled`, with `exception` able to interrupt any active state (resume when its last blocking
exception is resolved) and `cancelled`. Staff verify **each item**: counted quantity, the batch code
scanned or typed from the kit label, recipe card, packaging and delivery label. The system also
re-checks the batch's status, holds, packing, recipe version and flags. Any mismatch is recorded as a
discrepancy and raises a blocking exception. Resolving it sends the item back to be checked again.
Two packers checking the same item: one wins, the other gets `already_verified`. A retried tap
(same key) is answered without a second record. **Verified.**

## 7. Production readiness integration

The `fulfillment` consumer reads `production.ready_for_fulfillment` after a settling delay and
enqueues `fulfillment.batch_ready`. The handler finds the orders in the batch's plan, and for each it
checks the commerce status, payment (`succeeded`, or nothing owed), every batch complete, no open
holds, and packed with verified labels. Not yet ready → no-op. Ready → obligation. Contradiction
(refunded, cancelled, missing batch) → a visible obligation in `exception` or `cancelled`, plus an
exception, never an active delivery. A batch not yet visible → error → retry. Replay from 0 and five
concurrent consumers produced one obligation per order. On the development database the consumer
processed all 10 historical events from Sprints 5–6 and created 4 obligations, with 0 failures.
**Verified.**

## 8. Delivery configuration

Zones (code, name, status, fee-policy reference, notes) cover regions through explicit rules, and a
region can belong to only one zone. A release whose address is in an active region with no zone is
flagged `unzoned` (a zone can be assigned by hand, with a reason). Inactive regions are flagged
`not_serviceable`, which blocks. Service days (all zones or one, open or closed), windows (draft or
approved; customers see only approved ones), and capacity by date, zone, window, partner and courier,
which cannot be set below what is assigned. No weekday pattern is assumed. **Verified.**

## 9. Partner management

Partners are carrier-neutral (manual, file, API or internal fleet). Approval needs a signed
agreement and all seven criteria met with evidence. Approval history is append-only. Withdrawing a
criterion or the agreement suspends an approved partner. Unapproved partners cannot take runs unless
a documented exception policy is set *and* a reason is given (recorded on the run and stop). Couriers
belong to a partner. A courier linked to a staff login with the `courier` role sees only their own
runs. **Verified.**

## 10. Dispatch and manifests

The planning screen schedules released deliveries onto approved dates, creates runs and assigns
deliveries, checking the partner's approval and zone, the schedule, blocking exceptions and capacity
under row locks. Concurrency tests: three dispatchers assigning one delivery gave one stop; two
deliveries into capacity 1 gave one success and one refusal. Stop order is manual (there is no
optimiser). Finalizing freezes a manifest (content, SHA-256, counts), and reopening supersedes it. The
manifest holds recipient, address, phone, instructions and packages, with no email, amounts or
account data. The CSV is served only through the permissioned API (audited, `no-store`, protected
against formula injection) and the admin's authenticated route. **Verified.**

## 11. Chain of custody

Release makes deliveries `ready_for_pickup`, but nothing is in custody until a handoff is recorded
per package (accepted, rejected or missing; who, when, verified location only, evidence reference).
A package is accepted once per attempt (unique index). Two couriers confirming at once: one record.
The same key resent: one record. A rejected or missing package marks the handoff disputed and raises
a blocking exception. When all of a delivery's packages are accepted it becomes `picked_up`, the
fulfillment `handed_over`, and the order `out_for_delivery`. **Verified.**

## 12. Tracking and proof

Tracking events are append-only (an update is refused by the database), carry a source
(staff/partner/customer/system) and an idempotency key, and keep a location only when marked
verified. These are milestones, and the customer view says it is not live tracking. Proof methods:
confirmation code, recipient acknowledgement, staff confirmation, partner confirmation. They are
accepted only if the approved policy lists them. With no policy configured, nothing can be delivered.
Photo proof is refused. A wrong code (including another delivery's code) is kept as a rejected
proof. Duplicate submissions give one accepted proof. `delivered` needs accepted proof for the
current attempt. **Verified.**

## 13. Customer experience

The order page has a mobile-first delivery card: a plain headline, the confirmed date and approved
window, any issue in customer words, the delivery code while the order is out, a timeline made only
of recorded milestones (order confirmed, kitchen preparing, packed and checked, ready for delivery,
out for delivery, delivered), the attempt history, a support request (opens a case linked to the
delivery) and a reschedule request (a request, never a promise). The test confirmed that no internal
status names or partner names appear, that another customer gets 404, and that there is no sideways
scroll at 390 px. Delivery emails: wording not approved → recorded "skipped"; customer opted out →
"skipped"; otherwise "sent" only when the mailer accepted the message. **Verified.**

## 14. Exceptions and reconciliation

One queue for fulfillment and delivery exceptions (type, severity, blocking, owner, links, next
action, resolution, append-only history). High and critical exceptions raise admin alerts.
Failed deliveries use configured reasons, never assume fault, and always raise an exception.
Next steps are reattempt (a new attempt, needing a new handoff), reschedule (checked against the
calendar and known capacity), or return. Returned kits are quarantined and need a recorded review;
nothing goes back to inventory (asserted: no inventory movements). Reconciliation compares planned,
handed over, delivered, failed, returned, missing proof, open exceptions and unreviewed returns, and
refuses to close while any discrepancy remains. Cancelling never refunds: it raises a refund-review
exception. Cancel and assign racing: the cancel wins and no stop is left. **Verified.**

## 15. Traceability

Supplier → ingredient lot → production batch (Sprint 6) → recipe version → order package item
(batch and recipe version) → delivery → proof and outcome. `GET /admin/fulfillment/{id}/trace` and
the fulfillment screen show this. It is explicitly **batch-level**: which lot portion went into
which kit is not recorded and not claimed. **Verified.**

## 16. RBAC and security

There are 23 new permissions (the brief's list plus `delivery.read_assigned` and
`delivery.reconcile`). Roles `fulfillment_manager`, `fulfillment_staff`, `dispatch_manager`,
`dispatcher`, `delivery_coordinator` and `courier` are added; super admin and admin get everything;
support and operations get read access. Every route checks the permission in the API, and courier
routes also check the courier's scope. Tests confirmed: packers cannot release, dispatchers cannot
approve partners or configure zones, support cannot record status, couriers cannot release runs or
see other runs (404), only `delivery_settings.manage` can correct an outcome, and customers see only
their own delivery. Addresses cannot change after dispatch (API and database trigger). Proofs are
hidden from roles without `delivery_proof.read`. Every privileged change is audited, and manifest
downloads are audited.

Change made because of a test: `GET /admin/delivery/settings` now needs only an admin session.
Couriers need the approved proof methods and failure reasons, and the settings hold policies, not
personal data.

## 17. UI screenshots

Folder: `docs/screenshots/sprint-7/`, desktop (1440) and phone (390) for 20 screens. Screens:
overview, fulfillment queue, a fulfilled fulfillment, calendar, dispatch planning, runs, a reconciled
run with its manifest, delivery detail (courier view), partners, a partner, couriers, zones,
exceptions, an exception, returns, reconciliation, report, settings, Customer 360 deliveries.
Test-only shots: `fulfillment-verify-phone`, `courier-delivery-out-phone`,
`customer-order-delivered-phone`. Every screen uses the existing admin shell and tokens, with no
gradients.

## 18. Automated test results (14 November 2026)

| Suite | Command | Result |
|---|---|---|
| Go vet | `go vet ./...` (golang:1.27-alpine) | clean |
| Go | `go test ./... -count=1 -p 1` against PostgreSQL 17 | **149 top-level tests passed, 0 failed, 0 skipped** (Sprint 7: 13 server integration tests + 4 delivery unit tests; 15 Nov, after the review fixes) |
| Vitest | `pnpm exec vitest run` per app | **43 passed** (admin 6, marketing 22, web 15) |
| TypeScript | `tsc --noEmit` in api-client, admin, web | clean |
| ESLint | admin and web Sprint 7 files | clean |
| Playwright | `playwright test --workers=1` in three batches | **55 passed, 0 failed** (Sprints 1–4: 38; Sprints 3/5/6: 13; Sprint 7: 4), on 14 Nov and again on 15 Nov after the review fixes |

What went wrong along the way, as it happened:
- The first full Playwright run with 2 workers: 51 passed, 4 failed. Two were 60-second timeouts
  and one a `/profile` sideways-scroll reading, all under memory pressure; all three passed when
  rerun with one worker. The fourth was a fragile Sprint 6 test that clicked the *first* "Receive"
  link, which was an older, deliberately partly received order. It now opens its own order.
- A one-worker full run was stopped by Claude Code when the PC ran critically low on memory. The
  suite was then run in three batches with one worker (results above).
- Two Sprint 2 tests asserted every email topic is off by default. They now exempt the
  `delivery_updates` service topic and still assert this for every marketing topic.

## 19. Known limitations

- Resolved on 15 Nov (§22): delivery dates are now bound to the order's week, and corrected outcomes
  are recorded as disputes in commerce.
- One package per order. Splitting an order into several bags is not built.
- Labels are data on screen, not a print layout, and are marked not certified.
- File and API partner modes have no adapter. The webhook verifier is unit-tested and deliberately
  not routed.
- Cost records are not covered by an integration test (implemented, not verified).
- Offline use: forms keep typed input after a failed save and say a connection is needed. There is no offline queue.
- Not done (debt carried from earlier sprints): Safari and Firefox runs, real devices, manual
  screen-reader testing, load testing. Accessibility checks are axe (wcag2a/aa) on the tested screens only.

## 20. Outstanding business decisions (blocked until decided)

Delivery partner agreements; operational zones; delivery days and windows; capacity figures; delivery
fees (zone fee-policy reference only); partner rates; proof-of-delivery policy (until set, nothing
can be completed); reschedule cutoff (requests collected, nothing promised); failure reasons (a
starting list ships and is editable); return and food-handling disposition (quarantine plus a
recorded review is all that is allowed); customer delivery email wording (emails skipped); package
label approval; cold-chain requirements; whether photo proof is wanted (needs secure storage first).

## 21. Sprint 8 readiness

Events for analytics: `fulfillment.created/verified/ready_for_dispatch/exception_raised` and
`delivery.assigned/dispatched/handed_over/in_transit/delivered/failed/rescheduled/returned/
exception_raised/reconciled`, each deduplicated per aggregate and attempt. Tracking events say who
reported each fact, so staff-recorded data can be told apart from partner data. The report endpoint
gives definitions per figure. Nothing in Sprint 8 has been started; it needs explicit authorization.

## 22. Review fixes (15 November 2026)

**Priority 1: delivery dates follow the order's week.** `delivery_settings` holds an approved
delivery-week rule: an order of the week starting Monday *W* may be delivered from *W*+min to *W*+max
days (0–27), with the reference of the decision that set it. Every delivery stores its order's week
(`order_week`). The rule is enforced when scheduling, when approving a reschedule, and again when
assigning to a run (so narrowing the rule later also stops assignments). **With no rule, nothing can be
scheduled** (`delivery_week_rule_missing`), and the settings and overview list this as a blocker.
Delivery days are still undecided, so the rule ships unset. Local demo data sets a demo rule (Monday
to Sunday of the order's week) and demo days inside each demo order's week. The planning screen shows
each delivery's order week. Test: `TestDeliveryDatesFollowTheOrderWeekRule` (no rule refused; a
half-set rule refused; an open day outside the week refused for scheduling and reschedule; narrowing
the rule refuses the assignment). **Verified.**

**Priority 2: one reconciliation contract between delivery and commerce.** Commerce owns a new
append-only record, `order_delivery_outcomes`, written only through `orders.FlagDeliveryOutcome`.
A delivered → failed correction:
- records the order's delivered outcome as **disputed** (with the reason), and never rewrites the
  order's status, its status history or any payment (asserted in the test);
- returns the fulfillment to "with partner" (`Reopen`), like an ordinary failed attempt, so the
  order can be redelivered. The first version of this fix put the fulfillment into an exception
  that a redelivery could not leave; the new test caught it;
- still raises a critical exception for staff.

A later attempt delivered with proof records the outcome as **confirmed**. Everything reads the same
record:
- customer order page and order list: "Delivery under review" while disputed;
- the customer delivery card: the corrected (failed) state;
- admin order detail: a "Delivery outcome disputed" tag and note; admin order list: a "Disputed" tag;
- delivery report: `corrected_outcomes` and `disputed_outcomes`, defined, and corrected deliveries
  count under their corrected status;
- operations overview: a "Disputed outcomes" count.

Test: `TestCorrectedOutcomeIsADisputeInCommerceAndReports`. **Verified.**

Other review items are tracked in §19 and remain open: one package per order, labels not a
certified print layout, no file or API carrier adapters, cost records without an integration test,
and outstanding Safari, Firefox, real-device, screen-reader and load testing. Multi-package support
and a delivery-cost test should be done before a live launch.

## Commit

Sprint 7 committed as `815af31` on `sprint-7/delivery-fulfillment` (local; not pushed), working tree clean after the commit. This hash note was added in a follow-up commit.
