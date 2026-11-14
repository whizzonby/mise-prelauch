# Sprint 6 plan: Supplier, Procurement, Inventory and Traceability

Brief: `E:\FULL MISE DEV\SPRINT 6`. Audit: `docs/architecture/SPRINT_6_AUDIT.md`.
Branch: `sprint-6/procurement-inventory`. Migration: `00010_supply_chain.sql`.

## 1. Existing components reused

| Component | Reuse |
|---|---|
| `culinary` ingredients, `Converter`, `AddCost` | The one ingredient catalogue and measurement engine; costs from quotes, POs and receipts |
| `production.Service` | `Plan`, new read `ProcurementRequirements(planID)`, `Batch(id)`, `Waste(filter)`; never written by Sprint 6 |
| `platform/qty`, `db.InTx`, row and advisory locks, `httpx` | Quantities, transactions, errors |
| `outbox`, `jobs` | Sprint 6 events; the first event consumer (§6) |
| `backoffice.Raise`, `audit.Record` | Alerts and audit, in the same transaction |
| `identity` RBAC, `misectl doctor` | New roles and permissions |
| `reporting` | Procurement, inventory and supplier reports, read-only |
| Admin shell, `KForm`, `kitchen-ui`, `commerce-ui` | All screens |
| Test harness (`newShop`, `newTeam`, `publishRecipe`), `misectl production seed-demo` | Real released plans and batches for tests |

## 2. New modules

| Module | Owns |
|---|---|
| `internal/suppliers` | suppliers, farmer profiles, qualification criteria and reviews, documents (metadata only), products (offers) and their ingredient mapping, price history, availability, communication log, performance figures |
| `internal/procurement` | procurement runs (requirement snapshots of released plans), lines and recommendations, RFQs, quotations, purchase orders, approvals, revisions, procurement settings |
| `internal/inventory` | storage locations, allocation policies, receiving checklist, goods receipts and inspections, lots, the movement ledger, reservations, issues and returns, `batch_ingredient_lots`, adjustments, counts |

Dependencies point one way: `suppliers` ← `procurement` ← `inventory` for PO lines; `inventory`
exposes stock position and inbound to `procurement` through an interface procurement defines
(no import cycle). None writes `culinary` or `production` tables; `culinary.AddCost` is called
for cost references.

## 3. Database migration (00010, additive)

One migration; Down drops only what Up creates, the added column and the permissions.

| Area | Tables |
|---|---|
| Suppliers | `suppliers` (code, name, trading name, type, status, contact, phone, email, preferred channel, region, operating regions, payment terms text, delivery arrangements, notes, review expiry), `supplier_status_events` (append-only approval history), `supplier_qualification_criteria` (configurable, by supplier type), `supplier_reviews` (criteria results, decision, notes, reviewer, expiry), `farmer_profiles` (one per supplier, `recorded_by` attribution), `supplier_documents` (title, kind, reference, `verified_by`, expiry; metadata only), `supplier_communications` (append-only) |
| Offers | `supplier_products` (supplier, ingredient or packaging item, SKU, name, grade, pack quantity and unit, minimum packs, lead days, availability, constraints, `mapping_status` proposed/confirmed/rejected with reviewer), `supplier_prices` (append-only: per pack, currency, valid from/until, origin, quote link), `supplier_availability` |
| Procurement | `procurement_settings` (one row: approval limit, receiving tolerance %, currencies, unapproved-supplier exception), `procurement_runs` (one per released plan, snapshot revision, source event id, status, review flag), `procurement_lines` (per ingredient: gross, buffer, reserved, inbound, shortage, recommendation, explanation, decision) |
| Purchasing | `rfqs`, `rfq_lines`, `rfq_suppliers`, `quotations`, `quotation_lines`, `purchase_orders`, `purchase_order_lines`, `purchase_order_revisions` (immutable approved snapshots), `purchase_order_events` (append-only) |
| Inventory | `storage_locations`, `inventory_policies` (per ingredient category: FIFO/FEFO), `receiving_checklist_items`, `goods_receipts` (idempotency key), `goods_receipt_lines` (delivered = accepted + quarantined + rejected, inspection results, discrepancy), `inventory_lots` (on hand and reserved maintained, CHECKs never negative, reserved ≤ on hand), `inventory_movements` (append-only ledger), `inventory_reservations`, `batch_ingredient_lots`, `stock_adjustments`, `stock_counts`, `stock_count_lines` |
| Costing | `ingredient_cost_references.origin` (`reference` default, `supplier_quote`, `purchase_order`, `receipt`) and `origin_id` |
| Events | `event_consumer_offsets` (consumer, last event id) |

Integrity in the database: append-only triggers on movements, status events, PO events,
revisions, prices and communications; unique codes; one live run per plan; one receipt per
idempotency key; lot quantities never negative; reserved ≤ on hand; a posted receipt and an
approved PO revision cannot change.

## 4. State machines

**Supplier:** `draft → pending_review → approved | rejected`; `approved → suspended → approved`
(re-review); any → `inactive`. Approval needs every required criterion for the supplier's
type recorded as met; the approver is not the person who submitted it. A review expiry, when
set, returns the supplier to `pending_review` for ordering purposes.

**Product mapping:** `proposed → confirmed | rejected`. The pack unit must convert to the
ingredient's base unit through `culinary.Converter`; otherwise the mapping is refused
(incompatible). Only confirmed products of approved suppliers are purchasing options.

**Procurement run:** `open → approved` (purchasing decisions approved) `→ closed`;
`flagged_for_review` is a flag, not a state, set when a newer demand revision exists.

**RFQ:** `draft → open` (invitations recorded by staff, with the channel used) `→ closed`;
`cancelled`. Nothing is "sent" by the system.

**Purchase order:**
```
draft ──submit──► pending_approval ──approve──► approved ──issue──► issued ──receive──► partially_received ──► received ──close──► closed
   ▲                    │ reject                   │ material change (reason) → new revision → pending_approval
   └────────────────────┘                          draft|pending_approval|approved|issued (nothing received) ──cancel──► cancelled
```
Approval: a holder of `purchase_orders.approve` who did not create it; above the configured
approval limit, also a holder of `purchase_orders.approve_above_limit`. With no limit set, one
approval is required and the screen says no limit is configured. Unapproved suppliers are
refused unless the exception policy is on and a reason is recorded.

**Lot:** `quarantined ⇄ available`, `→ on_hold`, `→ rejected` (never usable; returned or
written off). Expired is computed from a recorded expiry date, never a stored status.
Expiry status: `recorded`, `not_recorded` (cannot be allocated until reviewed), `none_applies`
(a person confirmed no date applies, with a note).

**Reservation:** `active → issued | released`. **Count:** `open → submitted → posted | cancelled`.

## 5. Inventory ledger, reservations and allocation

- Every change to a lot is a row in `inventory_movements` (receipt, reserve, release, issue,
  return, transfer, adjustment, store waste, supplier return, status change) with on-hand and
  reserved deltas, balance after, actor, reason and reference. The lot's `on_hand` and
  `reserved` are maintained in the same transaction; a test checks they equal the ledger sums.
- **Quantities defined** (shown on screen): on hand = physically held, any status; usable =
  available, not expired, expiry recorded or confirmed not applicable; reserved = held for a
  plan; available to promise = usable − reserved; quarantined; expired; confirmed incoming =
  ordered and not yet received on approved or issued POs (never mixed with on hand).
- **Reserve** for a plan: lock candidate lots (`FOR UPDATE`, in allocation order), take up to the
  plan's gross requirement net of what is already reserved; a concurrent request waits for the
  lock and sees the reduced availability, so the same quantity is never reserved twice.
- **Allocation order** per ingredient category policy: FEFO (earliest recorded expiry first;
  lots without a date excluded under FEFO) or FIFO (earliest receipt). No policy row → FIFO,
  labelled as the default. Held, quarantined, rejected, expired and unreviewed-expiry lots are
  never allocated.
- **Issue** to a batch (released or in progress, read through `production.Service.Batch`):
  consumes the plan's reservation on that lot first; writes the movement and
  `batch_ingredient_lots`. Partial issues allowed. **Return** up to issued − returned.
- **Consumption** per batch and ingredient = issued − returned. Production waste for that batch
  is reported inside consumption and never posted again (audit §3.5).
- **Adjustments** need a reason and record previous and new balance. **Counts** record the
  expected quantity and the last movement id when each line is counted; posting applies the
  variance (counted − expected at count time) as an adjustment, so movements made during the
  count are kept.

## 6. Event consumption

`event_consumer_offsets` holds, per consumer, the last event id handed to the job queue. The
worker's dispatcher (every scheduler interval) locks the offset row, reads newer events of the
types it consumes, enqueues one job per event (payload: event id) and moves the offset, in one
transaction. The job handler is idempotent (one run per plan, unique), retried with backoff,
and ends in `failed` (the dead-letter set) after its attempts; failures show on the supply chain
overview. Replay: `misectl events replay -consumer procurement -from <id>` moves the offset
back; idempotency makes re-delivery harmless. Planners can also create the run from a released
plan by hand, through the same function.

## 7. Net requirement methodology

Per procurement run (one released plan) and ingredient, in the ingredient's base unit:

```
gross     = sum of the plan's requirements for the ingredient (all preparations)
buffer    = approved safety stock (none configured → 0, flagged)
reserved  = active reservations for this plan
issued    = stock already issued to the plan's batches, net of returns
inbound   = still-to-receive quantity on approved or issued PO lines linked to this run line,
            expected by the required-by date (later ones shown as a lead-time risk, not counted)
shortage  = max(0, gross + buffer − reserved − issued − inbound)
```

Stock counts only once it is reserved for this plan, and inbound only through the PO lines
linked to this line, so neither can be counted twice across plans. Usable unreserved stock is
shown, with a "reserve" action. A null requirement (unconvertible) marks the line incomplete.
Recommendation: for each confirmed product of an approved supplier, packs = ceil(shortage ÷ pack
size in base units), at least the minimum; with price per base unit, lead-time risk against the
required-by date (production date), availability and factual performance. No supplier is
chosen automatically; when only one option exists it is pre-selected and says so. The buyer's
decision and note are recorded. Every figure carries its explanation.

## 8. API (under `/api/v1/admin`, admin session + permission)

| Area | Routes |
|---|---|
| Suppliers | `GET/POST suppliers`, `GET/PATCH suppliers/{id}`, `POST suppliers/{id}/submit`, `/review`, `/suspend`, `/reactivate`, `/deactivate`, `PUT suppliers/{id}/farm`, `POST suppliers/{id}/documents`, `POST suppliers/{id}/communications`, `GET/POST supplier-criteria`, `PATCH supplier-criteria/{id}` |
| Offers | `GET supplier-products`, `POST suppliers/{id}/products`, `PATCH supplier-products/{id}`, `POST supplier-products/{id}/mapping`, `POST supplier-products/{id}/prices`, `GET/POST supplier-availability` |
| Procurement | `GET procurement/runs`, `POST procurement/runs` (from a released plan), `GET procurement/runs/{id}`, `POST procurement/runs/{id}/reserve`, `POST procurement/runs/{id}/lines/{line}/decision`, `POST procurement/runs/{id}/approve`, `GET/PUT procurement/settings` |
| RFQs | `GET/POST procurement/rfqs`, `GET procurement/rfqs/{id}`, `POST .../open`, `/close`, `/cancel`, `POST procurement/quotations` |
| POs | `GET/POST purchase-orders`, `GET/PATCH purchase-orders/{id}`, `POST .../submit`, `/approve`, `/reject`, `/issue`, `/close`, `/cancel`, `GET .../document` (printable) |
| Receiving | `GET/POST receiving`, `GET receiving/{id}`, `GET/POST receiving/checklist`, `PATCH receiving/checklist/{id}` |
| Inventory | `GET inventory` (by ingredient), `GET inventory/lots`, `GET inventory/lots/{id}`, `POST inventory/lots/{id}/status`, `/transfer`, `/expiry`, `/waste`, `GET inventory/movements`, `GET/POST inventory/reservations`, `POST inventory/reservations/{id}/release`, `POST inventory/issues`, `POST inventory/returns`, `POST inventory/adjustments`, `GET/POST inventory/counts`, `GET inventory/counts/{id}`, `PUT inventory/counts/{id}/lines`, `POST .../submit`, `/post`, `GET/POST inventory/locations`, `PATCH inventory/locations/{id}`, `GET/PUT inventory/policies` |
| Traceability | `GET traceability?lot=|batch=|supplier=|purchase_order=`, `GET inventory/batches/{id}/reconciliation` |
| Reports | `GET reports/procurement`, `reports/inventory`, `reports/suppliers` |

Pagination, validation and errors as in Sprint 5. All routes in `docs/api/openapi.yaml`
(`TestRoutesMatchOpenAPI`); the TypeScript client is regenerated.

## 9. Events

`supplier.approved`, `supplier.suspended`, `procurement.shortage_identified`,
`procurement.plan_approved`, `purchase_order.approved`, `purchase_order.issued`,
`purchase_order.partially_received`, `purchase_order.received`, `inventory.lot_received`,
`inventory.lot_quarantined`, `inventory.lot_released`, `inventory.stock_reserved`,
`inventory.stock_issued`, `inventory.stock_adjusted`, `receiving.inspection_failed`. Written
with `outbox.Record` in the changing transaction; dedupe keys where an aggregate can repeat
(revision number, receipt id).

## 10. RBAC

Roles: `procurement_manager`, `procurement_officer`, `inventory_manager`, `receiving_staff`,
`supplier_coordinator`. Permissions (brief §38, plus three for policy and limits):
`suppliers.read/manage/approve`, `supplier_products.read/manage`,
`procurement.read/plan/approve/configure`, `purchase_orders.read/create/approve/issue/approve_above_limit`,
`receiving.read/manage/inspect`, `inventory.read/reserve/issue/adjust/count/configure`,
`traceability.read`, `supplier_reports.read`.

| Role | Gets |
|---|---|
| super_admin, admin | all |
| procurement_manager | suppliers.*, supplier_products.*, procurement.*, purchase_orders.* except approve_above_limit, receiving.read, inventory.read, inventory.reserve, traceability.read, supplier_reports.read, ingredients.read, production.read |
| procurement_officer | suppliers.read/manage, supplier_products.*, procurement.read/plan, purchase_orders.read/create, receiving.read, inventory.read, supplier_reports.read, ingredients.read, production.read |
| supplier_coordinator | suppliers.read/manage, supplier_products.read/manage, ingredients.read |
| inventory_manager | inventory.*, receiving.*, traceability.read, purchase_orders.read, suppliers.read, ingredients.read, production.read |
| receiving_staff | receiving.read/manage/inspect, inventory.read, ingredients.read (PO lines without prices) |
| kitchen_staff (existing) | + inventory.read, inventory.issue |
| finance (existing) | + purchase_orders.read, purchase_orders.approve_above_limit, procurement.read, supplier_reports.read |
| quality_inspector (existing) | + receiving.read, receiving.inspect, inventory.read, traceability.read |

Supplier contacts and payment terms need `suppliers.read`; prices need `supplier_products.read`
or `purchase_orders.read`. Separation of duties: nobody approves a supplier they submitted, a
PO they created, or a count they counted.

## 11. Business inputs (shipped empty; nothing invented)

| Input | Until provided |
|---|---|
| Supplier qualification criteria | No supplier can be approved (the review has nothing to check) |
| Purchase approval limit | One approval for every PO; screen says "no limit configured" |
| Receiving tolerance | Nothing above the ordered quantity can be received |
| Purchasing currencies | TTD only (the business currency); others refused |
| Unapproved-supplier exception | Off: unapproved suppliers get no POs |
| Storage locations | Nothing can be received (no place to put it) |
| Receiving inspection criteria | Deliveries can be quarantined or rejected, not accepted |
| Shelf-life rules, expiry | A lot without a recorded expiry cannot be allocated until someone confirms none applies |
| Safety stock | Zero, flagged on every line |
| Allocation policy per category | FIFO, labelled as the default |
| Lot numbering policy | Internal ids `LOT-YYMMDD-NNN`, labelled internal; supplier lot reference kept separately |
| Payment terms | Free text per supplier, "not agreed" when empty |
| Supplier messaging integrations | None; contact is recorded by staff, POs are printed or downloaded |

## 12. UI

Navigation: **Supply chain** (Overview, Procurement planning, Purchase orders, Quotations,
Receiving, Inventory, Lots, Movements, Stock counts, Traceability, Supply settings) and
**Suppliers** (Directory, Farmers, Products, Availability, Performance). Receiving, lot moves,
counts and issue to a batch are phone-first (large targets, one task per screen, numeric
input). A printable PO page. No scanning (nothing reliable to test it with).

## 13. Testing

Go integration tests on real PostgreSQL covering the brief's 40 scenarios, including concurrent
PO approval, duplicate receipt submission, concurrent reservation of one lot, adjustment during
a reservation, and a quotation change after approval. The §45 scenario runs through the real
services, from an approved supplier to verified reports, in one test. Unit tests for the net
calculation and allocation order. Playwright: supplier onboarding to approval, PO to partial
receipt with a rejected quantity, issue to a batch and traceability, role limits, phone width.
All earlier suites rerun.

## 14. Sprint 7 integration contracts

- `production.ready_for_fulfillment` stays the delivery trigger; Sprint 6 adds lot ids per batch
  (`batch_ingredient_lots`) for recall look-ups by delivery.
- Traceability is **batch-level**: a lot is linked to the batches it was issued to and, through
  the batch's meal and plan week, to the orders that contained that meal. Kit-level assignment
  would need packing to record which kits got which lots; it does not today.
- Supplier collection arrangements (farmer profiles) are recorded as text for delivery planning.

## Progress

- [x] Stage 1: audit and plan
- [x] Stage 2: supplier foundation (backend): migration `00010_supply_chain.sql` (all Sprint 6 tables,
  tested up/down/up), `internal/suppliers` (directory, farms, criteria, reviews with separation of
  duties, documents, communication log, products with reviewed ingredient mapping, price
  history, availability), `culinary.RecordSupplyCost` + `ingredient_cost_references.origin`,
  25 permissions, OpenAPI via scratchpad `gen_openapi6.py` (idempotent), tests
  `server/supply_test.go` (2 tests passing 13 Nov). Screens come with the rest in Stage 8.
- [x] Stage 3: procurement planning (backend): `production.ProcurementRequirements/BatchRef/ReleasedPlans`
  (read contracts), `internal/procurement` runs (idempotent per plan), net calculation with
  explanations, options (no automatic choice), decisions, approve/reopen, event consumer
  (`Dispatch` → job `procurement.plan_released`, settle delay, replay via
  `misectl events replay`), worker wiring. Harness builds the supply chain (`h.sc`).
- [x] Stage 4: purchasing (backend): RFQs, invitations (recorded, nothing sent), immutable
  quotations → supplier prices, POs (draft → approval with separation of duties and the
  approval limit → issue → receive contract `RecordReceipt` → close/cancel), material changes as
  revisions with immutable snapshots, costs at approval. Tests `server/supply_chain_test.go`
  (2 tests) + supplier tests: all passing 13 Nov. Harness fix: re-insert the
  `procurement_settings` row (TRUNCATE users CASCADE empties it).
- [x] Stage 5: inventory and receiving (backend): `internal/inventory` locations, policies,
  receiving checklist, `Receive` (idempotency key, PO lock, checklist required to accept,
  accepted/quarantined/rejected lots, tolerance via procurement), the ledger (`apply`), lot
  status/expiry review/transfer/store waste/supplier return, adjustments, stock summary with
  definitions, `PlanPosition` (procurement.Stock). Production read `BatchRequirements`,
  `BatchOrders`; procurement `Incoming`.
- [x] Stage 6: reservations and production issues (backend): FIFO/FEFO candidates with row and
  ingredient locks, `Reserve` (reports short), `Release`, `Issue` (reservations first, all or
  nothing), `Return`, `Reconcile`, counts (variance applied to the current balance), trace.
  Tests `server/supply_inventory_test.go` (2 tests, incl. the §45 scenario): all 6 Sprint 6
  tests passing 13 Nov. Fixes found: NULL array in the candidate query; reviewed expiry could
  be overwritten.
- [x] Stage 7: traceability and reporting (backend): reports `procurement`, `inventory`,
  `suppliers` (routes under `supplier_reports.read`; factual, defined, TTD-only money), alert
  scan additions (overdue PO, expired lot, expiry review, released plan without a run).
  Test `TestSupplyReportsAndAlertsMatchTheRecords` passing 13 Nov.
- [x] Stage 8: UI, verification, report
  - [x] API client `supply` namespace (scratchpad `client6.py`), permissions added to the
    `AdminPrincipal` enum; schema name clashes fixed (`PlanRef`, `InviteInput`, `PriceInput` →
    `ProductionPlanRef`, `RFQInviteInput`, `SupplierPriceInput`).
  - [x] Admin screens written (typecheck + lint clean 13 Nov): suppliers (directory, record with
    tabs, products, product, availability), procurement (runs, run), purchase orders (list,
    order, print), quotations (list, request), receiving (list, mobile form, receipt), inventory
    (stock, lots, lot, movements, counts, count, batch ingredients), traceability, supply
    overview, supply settings, reports (procurement, inventory, suppliers); nav groups; batch
    page link. Actions in `supply-actions.ts`.
  - [x] Build; every screen rendered (smoke check found the supplier report crashing on a null
    `series`: fixed by initialising report slices); `e2e/tests/supply-chain.spec.ts` (4 tests)
    passing 13 Nov. Needs `misectl production seed-demo` before each run.
  - [x] Full Playwright suite, screenshots, UI review, report (14 Nov: Go 132 pass, Vitest 43,
    Playwright 51/51 on the second run; first run 50/51, a load timeout in a Sprint 2 test that
    passed 3/3 alone). Report: `docs/SPRINT_6_REPORT.md`.
