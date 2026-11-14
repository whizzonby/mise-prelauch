# Sprint 6 report: Supplier, Procurement, Inventory and Traceability

Branch `sprint-6/procurement-inventory` (from `sprint-5/chef-production`, `cbee3c6`). Brief:
`E:\FULL MISE DEV\SPRINT 6`. Audit: `docs/architecture/SPRINT_6_AUDIT.md`. Plan, state machines
and progress: `docs/architecture/SPRINT_6_PLAN.md`. Written 14 November 2026; every test result
below was produced on 14 November (§16).

Status words used throughout:
- **Verified**: exercised by an automated test against real PostgreSQL, or end to end in a browser.
- **Implemented, not verified**: built and type-checked, the screen renders (screenshot), but no
  test drives it.
- **Blocked**: needs an input from the business (§18); the software ships it empty and says so.
- **Deferred**: out of this sprint's scope.

Nothing in this sprint is operationally certified. No supplier, price, certification,
qualification criterion, inspection rule, shelf life, tolerance, limit or stock figure was
invented: every one ships empty, and what depends on it is blocked or labelled (brief §47).
All suppliers, lots and orders in the screenshots and the development database are demo or
test content and are not evidence of real operations.

## 1. Repository audit findings

Full audit: `docs/architecture/SPRINT_6_AUDIT.md`. Every Sprint 5 claim was checked in the code,
and where the report and the code differed, the code was taken as correct. The main points:

- **One ingredient catalogue** (`culinary`), with one conversion engine (`culinary.Converter`).
  Supplier products map to `ingredients.id`; there is no second catalogue.
- **Requirement sources carried codes, not ids.** Production gained one read-only export,
  `production.Service.ProcurementRequirements(planID)`, with batch ids, plan, snapshot and
  production date. Production still owns its tables, and no other module writes them.
- **`ingredient_cost_references.source` is free text.** The column was not changed: a new
  `origin` column (`reference`, `supplier_quote`, `purchase_order`, `receipt`) and `origin_id`
  keep quoted, ordered and actual costs separate. No cost is ever overwritten.
- **Before this sprint, nothing consumed domain events.** Sprint 6 adds the first consumer
  (`event_consumer_offsets` → job queue).
- **Waste ownership:** `production_waste` is what the kitchen threw away from stock already
  issued. It is reported inside batch consumption and never posted again as a stock movement.
  Inventory owns store waste, which is stock thrown away while still in storage.
- `ingredients.purchasing_unit` and `shelf_life_note` are free text, so neither drives pack
  rounding or expiry. Supplier products carry their own pack, and a lot's expiry comes only from
  a recorded date.

## 2. Architecture decisions

- **Three new modules** (ADR-001, packages, not services): `internal/suppliers`,
  `internal/procurement`, `internal/inventory`. Dependencies point one way. Inventory gives
  procurement its stock position and inbound quantities through an interface that procurement
  defines, so there is no import cycle.
- **No module writes another's tables.** Costs go through `culinary.RecordSupplyCost`; batches
  and requirements are read through `production.Service`; orders are read for traceability.
- **One inventory truth:** the append-only `inventory_movements` ledger. Each lot's `on_hand`
  and `reserved` change in the same transaction as their movement row. CHECK constraints keep
  both from going negative and keep reserved ≤ on hand. A test checks that every lot equals the
  sums of its ledger rows.
- **Concurrency:** row locks (`FOR UPDATE`) on lots, ingredients and purchase orders, taken in
  allocation order. The idempotency key on receipts is unique, there is one run per plan, and
  two approvers acting at once still produce one approval.
- **Exact arithmetic** (`platform/qty`, `numeric`, decimal strings in JSON), as in Sprint 5.
  Money is integer minor units, in TTD only until other currencies are approved.
- **Event consumption:** `event_consumer_offsets` → one job per event → an idempotent handler,
  retried with backoff, ending in `failed` (dead letter). Events can be replayed with
  `misectl events replay -consumer procurement -from <id>`.
- **Nothing is sent to suppliers.** RFQ invitations and purchase-order issue are recorded by
  staff with the channel they used. Orders are printed or downloaded.

## 3. Implemented modules

| Area | What it does | Status |
|---|---|---|
| Supplier directory | Code, type, status, contacts, regions, payment terms (free text), search, farmers view | Verified (Go, browser) |
| Farmer profiles | One per supplier, recorded on the farmer's behalf, with attribution | Verified (Go, browser) |
| Qualification | Criteria per supplier type from the business's procedure; review against every criterion; submitter ≠ approver; suspend, reactivate, deactivate | Verified (Go, browser) |
| Documents, communication log | Metadata only (no file storage exists); a certification counts once verified; the log is append-only | Verified (Go); screens not driven |
| Supplier products and mapping | Pack and unit must convert to the ingredient's base unit; `proposed → confirmed / rejected`; a pack change sends the mapping back for review | Verified (Go, browser) |
| Price history | Append-only; each price is also a cost reference | Verified (Go, browser) |
| Availability | What the farmer said, never stock | Verified (Go); screen rendered only |
| Procurement runs | One per released plan, from the event or by hand; flagged when demand changes | Verified (Go, browser) |
| Net requirements and options | §7; no automatic choice of supplier | Verified (Go, browser) |
| RFQs and quotations | Invitations recorded, immutable quotations, quoted prices recorded as supplier prices | Verified (Go); screens rendered only |
| Purchase orders | Draft → approval → issue → receipt → close; revisions; approval limit | Verified (Go, browser up to issue) |
| Printable purchase order | `purchase-orders/{id}/print` | Implemented, not verified (rendered) |
| Receiving and inspection | Phone-first form; checklist; accepted, quarantined and rejected quantities; discrepancies | Verified (Go, browser at 390 px) |
| Lots | `LOT-YYMMDD-NNN` (labelled internal), supplier lot reference, status, expiry review | Verified (Go); status, expiry, transfer and waste forms not driven |
| Ledger, stock summary | §9 | Verified (Go) |
| Reservations and issues | FIFO / FEFO, reserve, release, issue, return | Verified (Go); reserve and issue in the browser |
| Adjustments and counts | Reasoned adjustments; counts that keep movements made during the count | Verified (Go); screens rendered only |
| Traceability | Supplier ↔ order ↔ receipt ↔ lot ↔ batch ↔ recipe ↔ customer orders (batch-level) | Verified (Go, browser) |
| Reports | Procurement, inventory, suppliers (performance) | Verified (Go); screens rendered only |
| Alerts | Shortage, overdue PO, expired lot, expiry review, plan without a run, PO above limit, count variance | Verified (Go) |

## 4. Database migrations

One migration, `00010_supply_chain.sql` (847 lines). It is additive and was tested
up / down / up. Its Down drops only what Up creates, plus the added columns and permissions.

- **37 tables:** suppliers (`supply_counters`, `suppliers`, `supplier_status_events`,
  `supplier_qualification_criteria`, `supplier_reviews`, `farmer_profiles`, `supplier_documents`,
  `supplier_communications`, `supplier_products`, `supplier_prices`, `supplier_availability`),
  procurement (`procurement_settings`, `procurement_runs`, `procurement_requirements`,
  `procurement_lines`, `rfqs`, `rfq_lines`, `rfq_suppliers`, `quotations`, `quotation_lines`,
  `purchase_orders`, `purchase_order_lines`, `purchase_order_revisions`, `purchase_order_events`),
  inventory (`storage_locations`, `inventory_policies`, `receiving_checklist_items`,
  `goods_receipts`, `goods_receipt_lines`, `inventory_lots`, `inventory_movements`,
  `inventory_reservations`, `batch_ingredient_lots`, `stock_adjustments`, `stock_counts`,
  `stock_count_lines`) and `event_consumer_offsets`.
- **Added columns:** `ingredient_cost_references.origin` (default `reference`, so existing rows
  keep their meaning) and `origin_id`.
- **12 triggers** make history unchangeable: `inventory_movements`, `supplier_status_events`,
  `supplier_reviews`, `supplier_communications`, `supplier_prices`, `purchase_order_events`,
  `purchase_order_revisions`, `quotations`, `quotation_lines`, `goods_receipts`,
  `goods_receipt_lines`, `stock_adjustments`.
- **Integrity in the database:** unique codes; one live run per plan; one receipt per
  idempotency key; delivered = accepted + quarantined + rejected; lot quantities never
  negative; reserved ≤ on hand.
- **5 roles and 25 permissions** (§14).

No production database was touched. The migration ran only on the local development and
`mise-s1` test databases.

## 5. API endpoints

91 new admin routes under `/api/v1/admin`:

| Area | Count |
|---|---|
| Suppliers, products, prices, availability, criteria | 24 |
| Procurement runs and settings | 10 |
| RFQs, quotations, purchase orders | 19 |
| Receiving, inventory, counts, locations, policies, traceability | 35 |
| Reports (`reports/procurement`, `reports/inventory`, `reports/suppliers`) | 3 |

They are listed in plan §8 and documented in `docs/api/openapi.yaml`, which was regenerated by
`gen_openapi6.py`. `TestRoutesMatchOpenAPI` checks that every registered route is in the
contract, and it passes. `packages/api-client` is regenerated with a `supply` namespace. Three
schema names clashed with existing ones and were renamed: `ProductionPlanRef`,
`RFQInviteInput`, `SupplierPriceInput`. Every route needs an admin session and a permission.
Pagination, validation and the error format follow Sprint 5.

**Events** are written with `outbox.Record` in the changing transaction:
- Suppliers: `supplier.approved`, `supplier.suspended`.
- Procurement: `procurement.shortage_identified`, `procurement.plan_approved`.
- Purchase orders: `purchase_order.approved`, `purchase_order.issued`,
  `purchase_order.partially_received`, `purchase_order.received`.
- Inventory: `inventory.lot_received`, `inventory.lot_quarantined`, `inventory.lot_released`,
  `inventory.stock_reserved`, `inventory.stock_issued`, `inventory.stock_adjusted`.
- Receiving: `receiving.inspection_failed`.

Each has a dedupe key wherever its aggregate can repeat (a revision number or a receipt id).

## 6. Supplier and farmer workflows

1. A supplier coordinator adds a supplier with only a name and type. For an informal farmer,
   little else is required.
2. The coordinator records the farm profile (location, practices, collection arrangements) on
   the farmer's behalf, attributed to themselves.
3. They submit it for review. Submission needs a way to reach the supplier.
4. Someone else, with `suppliers.approve`, reviews it against every criterion that applies to
   the supplier's type:
   - A required criterion that isn't met refuses the approval, and the supplier can then be
     rejected.
   - With no criteria configured, nothing can be approved, and the screen says why.
5. Documents are metadata only. A certification counts once someone has verified it.
6. The communication log records what was said and through which channel. Nothing is sent.
7. Suspension stops ordering and alerts purchasing. Reactivation needs a new review.
8. Products are mapped to catalogue ingredients:
   - Litres of tomato, or onions by the piece without a recorded conversion, are refused.
   - Until someone confirms the mapping, the product is not a purchasing option.
   - Products of unapproved suppliers are recorded but cannot be bought.

The browser test drives steps 1–4 and product mapping and pricing: it records a farmer, their
farm, submission, a review by another person, a product mapped and confirmed, and a price. A
coordinator cannot see the review form, and the API refuses their approval with 403.

## 7. Procurement planning methodology

A run is created once per released plan: from the `production.plan_released` event (through the
job queue) or by a planner, through the same function. A replayed delivery and a planner
clicking at the same moment still make one run. The run copies the gross requirements as
released, and they are never edited. A newer demand revision flags the run for review instead of
changing it.

For each ingredient, in its base unit:

```
gross     = the plan's requirements for the ingredient (all preparations)
buffer    = approved safety stock (none configured → 0, flagged on every line)
reserved  = active reservations for this plan
issued    = stock issued to the plan's batches, net of returns
inbound   = still-to-receive quantity on approved or issued PO lines linked to this line,
            expected by the required-by date (later ones shown as a lead-time risk, not counted)
shortage  = max(0, gross + buffer − reserved − issued − inbound)
```

Stock counts only once it is reserved for this plan, and inbound counts only through PO lines
linked to this line, so neither can be counted twice across plans. Usable unreserved stock is
shown next to a "reserve" action. Quarantined, rejected, held and expired stock, and stock whose
expiry hasn't been reviewed, never count.

**Options:** every confirmed product of an approved supplier is an option.
- Packs = the shortage divided by the pack size, rounded up, and at least the supplier's
  minimum.
- Each option shows its price per base unit, lead-time risk, availability and factual
  performance.
- Options are sorted by supplier, not by price, and the system never chooses one. When only one
  option exists, it is pre-selected, and the screen says so.

Every figure has a "Why this quantity" explanation. Approving the run needs a recorded decision
for every shortage (buy, defer with a note, or none needed). Once approved, decisions change only
if the run is reopened with a reason.

Verified by `TestProcurementConsumesReleasedPlansOnce` and
`TestPurchasingDecisionsOrdersApprovalsAndRevisions` (in Go), and in the browser on the run page.

## 8. Purchase order workflows

```
draft → pending_approval → approved → issued → partially_received → received → closed
          └ reject → draft      approved/issued + material change → new revision → pending_approval
draft | pending_approval | approved | issued (nothing received) → cancelled
```

- **Drafts:** buy decisions create one draft per supplier. Asking again adds nothing.
- **Submission** needs an expected delivery date.
- **Approval:**
  - The approver needs `purchase_orders.approve` and cannot be the order's creator.
  - Two approvers acting at once: one approves, the other finds it already approved (tested).
  - Above a configured approval limit, a second approval is needed, from a holder of
    `purchase_orders.approve_above_limit`. With no limit set, one approval is needed and the
    screen says "no limit configured".
- **Costs:** approval records each line's price as an ordered cost reference.
- **Issuing** needs `purchase_orders.issue` and is a record only. Nothing is sent.
- **Revisions:** a material change after approval needs a reason and a new approval. The
  approved revision stays as an immutable snapshot.
- **Unapproved suppliers** get no orders. The exception policy exists but is off.
- **Quotations:** an RFQ goes to several suppliers, with the invitations recorded. Quotations are
  immutable, and a quote received after approval does not change the approved order's price
  (tested).
- **Receiving staff** see orders without prices (Go and browser).

The browser test runs from the run page through drafting, a refused self-approval, approval by
another person and issue. The reject, cancel, close and revision screens are not driven in a
browser; their API paths are covered in Go.

## 9. Inventory ledger implementation

- Every change to a lot is one append-only row in `inventory_movements`. Movement kinds:
  receipt, reserve, release, issue, return, transfer, adjustment, store waste, supplier return,
  status change. Each row has on-hand and reserved deltas, the balance after, the actor, a reason
  and a reference.
- The lot's `on_hand` and `reserved` are kept in the same transaction.
  `TestStockFromPurchaseToBatchTraceAndReports` checks that every lot equals the sums of its
  ledger rows.
- **Quantities are defined on screen:**
  - on hand: physically held, in any status;
  - usable: available, not expired, with an expiry recorded or confirmed not to apply;
  - reserved: held for a plan;
  - available to promise: usable − reserved;
  - quarantined and expired are shown separately;
  - confirmed incoming is shown separately and never mixed with on hand.
- **Adjustments** need a reason and record the previous and new balance. Ordinary staff cannot
  make them, and an adjustment cannot take a lot below what is reserved on it.
- **Counts:** `open → submitted → posted | cancelled`. Each counted line records the expected
  quantity and the last movement id, and posting applies only the variance. Tested: counted 330
  against 350 expected, then 50 wasted afterwards, gives 350 − 50 − 20 = 280. The person who
  counted cannot post.

## 10. Receiving and quality controls

1. Receiving staff choose an issued order and enter only what is in front of them. The form is
   phone-first, with large targets and numeric inputs.
2. Each line records packs delivered, accepted, quarantined and rejected; the supplier's lot
   reference; the expiry as printed; storage locations; temperature; and each checklist item
   (pass, fail or not applicable; a fail needs a note). A discrepancy is recorded with what was
   wrong.
3. **Checklist required:** without receiving checklist items from the business's procedure,
   nothing can be accepted. Deliveries can only be quarantined or rejected.
4. **No tolerance:** more than ordered is refused unless a receiving tolerance is configured.
5. **Idempotency:** the same form sent twice records once, and two clerks recording the same
   delivery at once still record it once (PO row lock and idempotency key).
6. **Lots:** accepted stock becomes an available lot. Quarantined and rejected stock becomes lots
   that can never be allocated. Rejected stock can be returned to the supplier; accepted stock
   cannot. A lot without an expiry date waits for an expiry review and is never given a guessed
   date.
7. **Order status:** the order becomes `partially_received` or `received`, and rejected packs
   are not counted as received.

The browser test (390 px) receives two of three bags of chicken, with one torn bag rejected.
`TestReceivingInspectionLotsAndPartialDeliveries` covers the rest.

## 11. Lot allocation and reservations

- **Allocation order** comes from the policy for the ingredient's category:
  - FEFO: earliest recorded expiry first; lots without a date are excluded.
  - FIFO: earliest receipt first.
  - With no policy, FIFO applies, and it is labelled as the default.
- Held, quarantined, rejected and expired lots, and lots with an unreviewed expiry, are never
  allocated.
- **Reserve** locks the candidate lots in order and takes up to the plan's need, net of what is
  already reserved. It reports any shortfall. A concurrent request waits and sees the reduced
  figure, so nothing is reserved twice (tested with simultaneous requests).
- **Release** needs a reason.
- **Issue** to a released or in-progress batch takes the plan's reservations first, and is all or
  nothing: too much is refused whole. Two cooks issuing the last of a lot at once: one gets it.
- **Return** to the store is allowed up to issued − returned.
- **Consumption** per batch and ingredient is issued − returned. The kitchen's waste is shown
  inside it and never posted twice.
- A lot cannot be held while it is reserved.

Tested in the §45 scenario (`TestStockFromPurchaseToBatchTraceAndReports`): the poultry FEFO
policy, two deliveries with the second expiring sooner, and the sooner-expiring lot reserved
first. The browser test reserves from the run page and issues from the batch's ingredients page.

## 12. Production traceability

`batch_ingredient_lots` links each issue (and return) to `production_batches.id`. The trace
reads both ways: supplier → purchase order → receipt → lot → issue → batch → recipe version →
the customer orders that contained the batch's meal in its plan week. You can start from a lot,
a batch, a supplier or a purchase order (`GET traceability`, `traceability.read`).

**Traceability is batch-level**, and the screen says so. Packing does not record which kits
received which lots, so a lot leads to the orders a batch served, not to individual kits.

Verified in Go, and in the browser: "Trace this lot" shows the farm and the batch, and the
accessibility scan has no serious issues. The traceability screenshot shows the empty search
form, not a filled trace.

## 13. Costing integration

`culinary.RecordSupplyCost` writes an `ingredient_cost_references` row with an origin:
- `supplier_quote` when a supplier price or quotation is recorded;
- `purchase_order` when an order is approved (the ordered price);
- `receipt` when stock is accepted (the actual cost of what was received).

Rows are never overwritten, so quoted, ordered and received costs stay distinct and dated.
Existing rows keep `origin = reference`. Sprint 5's recipe costing reads these references, and
costs with no reference still show as incomplete estimates. Reports give TTD figures only and
never convert currencies. Supplier payments and accounting are out of scope (§19).

## 14. RBAC and security

**New roles:** `procurement_manager`, `procurement_officer`, `inventory_manager`,
`receiving_staff`, `supplier_coordinator`. Existing roles gained the following:
- `kitchen_staff`: `inventory.read` and `inventory.issue`;
- `finance`: PO read, above-limit approval, procurement read and reports;
- `quality_inspector`: receiving read and inspect, inventory read, traceability.

The full matrix is in plan §10.

**25 new permissions:**
- `suppliers.read/manage/approve`
- `supplier_products.read/manage`
- `procurement.read/plan/approve/configure`
- `purchase_orders.read/create/approve/issue/approve_above_limit`
- `receiving.read/manage/inspect`
- `inventory.read/reserve/issue/adjust/count/configure`
- `traceability.read`, `supplier_reports.read`

Handlers check permissions, never role names. The permissions are also added to the admin
client's `AdminPrincipal` enum.

| Risk | Handling | Evidence |
|---|---|---|
| Self-approval of a supplier, PO or count | Submitter / creator / counter cannot approve or post | Go; browser (supplier, PO) |
| Approval over a limit | Second approver with `approve_above_limit` | Go |
| Buying from an unvetted supplier | Unapproved suppliers get no orders; their products are not options | Go |
| Contacts and prices seen by the wrong people | Contacts need `suppliers.read`; prices need `supplier_products.read` or `purchase_orders.read`; receiving staff see orders without prices | Go; browser |
| Accepting goods without inspection | Accepting needs the checklist; a failed item cannot be accepted | Go |
| Double receipt, double reservation, double issue | Idempotency key, row locks, CHECK constraints | Go (concurrent requests) |
| Using unsafe stock | Quarantined, held, rejected, expired and unreviewed-expiry lots are never allocated | Go |
| Tampering with history | Append-only triggers; approved revisions immutable | Go |
| Screens hidden but reachable | Pages closed by permission, not only hidden from the menu | Browser (receiving staff, kitchen staff) |
| Audit | Every state change is audited in the same transaction | Go |

## 15. UI screenshots

`docs/screenshots/sprint-6/` holds 26 pages at 1440 px and 390 px (52 files), taken on 13
November with `node tools/admin-shots.mjs ../docs/screenshots/sprint-6 sprint-6` from `e2e/`
after the supply-chain tests ran:

- supply overview and supply settings;
- procurement runs and a run;
- purchase orders, an order and its print view;
- quotations;
- receiving, the phone receiving form and a receipt;
- inventory, lots, a lot, movements and stock counts;
- traceability;
- suppliers, a supplier and its farm tab;
- supplier products and a product;
- availability;
- the procurement, inventory and supplier reports.

**UI review** (14 November, from the screenshots and the browser tests):
- The receiving form at phone width shows one delivery per screen. Its fields are labelled and
  stacked, and it explains what an empty expiry means.
- The run page shows needed, reserved, issued, on-order, short and usable figures apart, with a
  "Why this quantity" explanation and the safety-stock warning.
- The phone test checks 12 supply screens for sideways scroll, and none has it.

Limits of the screenshots:
- Desktop full-page shots cut the sidebar at the viewport height. This comes from the capture,
  not the layout.
- The traceability screenshot shows the empty search form.
- The 91 on the alerts badge comes from accumulated test runs on the development database.

All supplier, lot and order content in the screenshots is test or demo data.

## 16. Automated test results (14 November 2026)

All results come from runs on 14 November against the `mise-s1` stack. The API image was built
after the last code change, and two demo plans were released with
`misectl production seed-demo` before each Playwright run.

| Suite | Command | Result |
|---|---|---|
| Go, all packages (real PostgreSQL) | `go test -count=1 -p 1 -v ./...` in `golang:1.27-alpine` | **All pass: 132 top-level tests** (125 after Sprint 5), 0 skipped, 0 failed |
| Typecheck | `pnpm typecheck` | Clean (exit 0) |
| Lint | `pnpm lint` | Clean (exit 0) |
| Vitest | `pnpm test` | **43 pass** (admin 6, web 15, marketing 22); unchanged |
| Playwright, whole suite | `playwright test --workers=2` | **51/51 pass** (47 earlier + 4 new), 3.4 min, on the second run |

**First Playwright run:** 50 passed and 1 failed. The failure was the Sprint 2 test
`customer pages have no automatically detectable accessibility problems`, which hit its
60-second timeout; no accessibility violation was reported. Run alone three times, it passed
each time (13.7 s, 12.6 s, 13.0 s). After reseeding, the whole suite passed 51/51. The test scans
10 pages one after another, and this PC is short of memory, so it is slow under load. Sprint 5
recorded a similar one-off failure (§17).

**New tests:**
- Go, `server/supply_test.go`: `TestSupplierDirectoryQualificationAndFarms`,
  `TestSupplierProductsMappingPricesAndAvailability`.
- Go, `server/supply_chain_test.go`: `TestProcurementConsumesReleasedPlansOnce`,
  `TestPurchasingDecisionsOrdersApprovalsAndRevisions`.
- Go, `server/supply_inventory_test.go`: `TestReceivingInspectionLotsAndPartialDeliveries`,
  `TestStockFromPurchaseToBatchTraceAndReports` (the §45 scenario, through the real services),
  `TestSupplyReportsAndAlertsMatchTheRecords`.
- Playwright, `e2e/tests/supply-chain.spec.ts`:
  - a farmer recorded, qualified, mapped and priced;
  - a released plan → an approved and issued order → a partial delivery at phone width with a
    rejected bag → reserve → issue to a batch → trace;
  - role limits on screens and the API;
  - 12 supply screens at phone width.

**Brief §44 scenarios:**
- 1–8: the supplier tests;
- 9–11 and 18–22: the procurement and purchasing tests, with duplicate and concurrent runs and
  approvals;
- 12–17 and 23–36: the receiving and §45 tests, with duplicate and concurrent receipts,
  reservations and issues;
- 37–39: the reports test, where every figure is checked against the records;
- 40: the full Go, Vitest and Playwright suites above.

**§45 end-to-end scenario:** all 15 steps run in `TestStockFromPurchaseToBatchTraceAndReports`,
and steps 1, 2, 4–11 and 14 also run in the browser. In the browser test, step 3 (the released
plan) comes from `seed-demo`, and steps 12, 13 and 15 are not driven.

**Found and fixed during Sprint 6:**
- The candidate-lot query failed on a NULL array.
- A reviewed expiry date could be overwritten.
- The test harness's `TRUNCATE users CASCADE` emptied the `procurement_settings` row.
- The supplier report crashed on a null `series`; report slices are now initialised.
- Two Sprint 5 browser tests run several staff browsers, so they are now marked `test.slow()`
  for this machine.

## 17. Known limitations

- **Screens not driven in a browser** (rendered and reviewed only, with their API paths covered
  in Go):
  - RFQs and quotations;
  - purchase order reject, cancel, close and revision, and the print view;
  - lot status, expiry review, transfer, store waste and supplier return;
  - returns to store and reservation release;
  - adjustments and stock counts;
  - supply settings forms (the browser tests configure the policies through the API);
  - availability, criteria, documents and the communication log;
  - the supply overview and the three report screens.
- **Plan §13 promised unit tests** for the net calculation and allocation order. They were not
  written as separate unit tests. Both are covered by the Go integration tests, with exact
  figures.
- **No barcode or QR scanning:** there is nothing reliable to test it with.
- **Documents are metadata only,** because no file storage exists.
- **Traceability is batch-level,** not per kit (§12).
- **The browser suite consumes released plans:** it needs two runs of
  `misectl production seed-demo` before every full run.
- **One timing-sensitive browser test** (§16).
- **Still open from earlier sprints:** Safari, Firefox, real phones, screen readers and load
  testing (Sprint 4 D4, Sprint 5 §17). The four Sprint 5 demo batches with open tasks are left
  as they were; historical records are not changed silently (brief §46).
- **The development PC's clock** ran about two hours behind on 13 November and about 16 hours
  behind on 14 November. Tests use the harness clock, and the times in the screenshots reflect
  the machine clock.
- **Not committed yet:** at the time of writing, the Sprint 6 work is not committed to git.

## 18. Outstanding business decisions

Each ships empty; what depends on it is blocked or labelled.

| Input | Until provided |
|---|---|
| Supplier qualification criteria | **No supplier can be approved** |
| Receiving inspection checklist | **Nothing can be accepted**; deliveries can only be quarantined or rejected |
| Storage locations | **Nothing can be received** |
| Purchase approval limit (and whether finance approves above it) | One approval for every PO; "no limit configured" |
| Receiving tolerance | Nothing above the ordered quantity is accepted |
| Approved purchasing currencies | TTD only; others refused |
| Unapproved-supplier exception | Off: unapproved suppliers get no orders |
| Safety stock | Zero, flagged on every procurement line |
| Allocation policy per category | FIFO, labelled as the default |
| Shelf-life rules | A lot without a recorded expiry cannot be allocated until someone reviews it |
| Lot numbering policy | Internal `LOT-YYMMDD-NNN`, labelled internal; the supplier's reference is kept separately |
| Payment terms | Free text per supplier; "not agreed" when empty |
| Storage capacity | Not recorded and not checked |
| Supplier messaging integrations | None; staff record contact, POs are printed |
| Real suppliers, prices, certifications, stock | None; the system invents none |

## 19. Sprint 7 readiness

**Contracts for delivery:**
- **Trigger:** `production.ready_for_fulfillment` (Sprint 5) stays the delivery trigger.
- **Recall look-ups:** a batch's lots come from `batch_ingredient_lots`, and a lot's batches and
  orders from `GET traceability?lot=`. Both are batch-level.
- **Farm collection:** farmer profiles hold collection arrangements as text, for delivery
  planning.
- **Event consumption:** `event_consumer_offsets` and the dispatcher are generic, so a Sprint 7
  consumer adds a row and a job kind.

**Deferred**, per the brief's boundaries (§51): delivery dispatch, drivers, routes, delivery
tracking, mobile apps, forecasting, supplier payments, accounting, external messaging and a
supplier portal (the §37 farmer portal is a future contract only).

Sprint 7 has not been started and needs explicit authorization.
