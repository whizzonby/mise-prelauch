# Sprint 6 audit: what the supplier, procurement and inventory work builds on

Written 13 November 2026 on branch `sprint-6/procurement-inventory` (from
`sprint-5/chef-production`, `cbee3c6`). Every finding was checked in the code, not taken from
the Sprint 5 report. Where the report and the code differ, the code wins and the difference is
listed in §5.

## 1. Existing module inventory

| Module | Owns | Relevant to Sprint 6 |
|---|---|---|
| `internal/culinary` | Ingredients, aliases, units, ingredient conversions, cost references, chefs, recipes, versions, reviews, costing | **The ingredient catalogue.** `culinary.NewConverter(ctx, q)` and `Converter.Convert(ctx, ingredientID, amount, from, to)` are the one conversion engine; `Service.Ingredient`, `Ingredients`, `Units`, `AddCost`, `CostReferences` are exported |
| `internal/production` | Demand snapshots, plans, items, ingredient requirements and their sources, batches, tasks, quality, packing, yield, waste, labels | **Requirements and batches.** `Service.Plan(id)` (with `Requirements`), `RequirementRows`, `Batch(id)`, `Batches(filter)`, `Waste(filter)` are exported |
| `internal/platform/qty` | Exact decimal quantities (`math/big.Rat`), parse, format, half-up rounding to 3 places | Reused for every Sprint 6 quantity |
| `internal/platform/outbox` | `outbox.Record` writes `domain_events` in the caller's transaction, ignoring duplicates | Reused for Sprint 6 events |
| `internal/platform/jobs` | PostgreSQL job queue: transactional enqueue, retries with backoff, permanent `failed` state, stuck-job recovery | Reused for event consumption (§3.4) |
| `internal/backoffice` | Alerts (`Raise`, dedupe key, permission per alert), staff, exports | Reused for stock and purchasing alerts |
| `internal/audit` | `audit.Record` in the caller's transaction; append-only trigger | Every Sprint 6 state change |
| `internal/identity` | Database-backed roles and permissions, `RequireAdmin(perm, h)`, `AllPermissions` checked by `misectl doctor` | New roles and permissions |
| `internal/reporting` | Read-only reports (`/reports/{kind}`, `/reports/production`), dashboard, exports | Procurement, inventory and supplier reports |
| `internal/orders` | Orders and order items | Read only, for the batch → order traceability step |
| `apps/admin` | Console shell, permission-filtered navigation, `KForm` (server-action forms with visible save state), `admin-ui`, `commerce-ui`, `kitchen-ui` | All new screens |

ADR-001 (modular monolith, standard-library router, modules call each other through exported
functions), ADR-002 (pgx, additive Goose migrations, constraints in the database; sqlc for
simple CRUD, hand-written pgx where aggregates and multi-table transactions dominate, as
amended in Sprint 5) and ADR-006 (server actions with `useActionState`, no client data
library) all still hold.

## 2. Existing database tables Sprint 6 touches

Migrations run `00001` … `00009`; the next is **`00010`**.

| Table (migration) | Columns that matter | Sprint 6 use |
|---|---|---|
| `ingredients` (00009) | `id`, `name`, `category` (15 values), `base_unit`, `purchasing_unit` (free text), `storage` (`unspecified/ambient/chilled/frozen`), `shelf_life_note` (free text), `active` | Supplier products map to `ingredients.id`. No second catalogue |
| `units_of_measure`, `ingredient_unit_conversions` (00009) | Six units; ingredient-specific cross-dimension factors, `approximate` flag | Purchase units convert to the base unit through `culinary.Converter` |
| `ingredient_cost_references` (00009) | `ingredient_id`, `kind`, `amount_minor`, `currency`, `per_quantity`, `unit`, **`source` (free text, 1–300 chars)**, `effective_from` | Gains an origin column (§5.1) |
| `production_plans` (00009) | `id`, `number`, `week_start`, `production_date`, `status` (`draft/approved/released/cancelled`), `snapshot_id` | Procurement reads released plans only |
| `ingredient_requirements` (00009) | `plan_id`, `ingredient_id`, `preparation`, `quantity numeric(16,3)` (NULL when unconvertible), `unit`, `status` (`ok/unconvertible/inactive_ingredient`), `approximate`; unique (plan, ingredient, preparation) | Gross requirements |
| `ingredient_requirement_sources` (00009) | `requirement_id`, `plan_item_id`, `recipe_version_id`, `bom_line_id`, `quantity`, `unit` | Source batches (through `production_batches.plan_item_id`) |
| `production_batches` (00009) | `id`, `code`, `plan_id`, `plan_item_id`, `meal_id`, `recipe_version_id`, `production_date`, `status` | `batch_ingredient_lots` references `id` |
| `production_waste` (00009) | `batch_id`, `ingredient_id` or `item`, `quantity`, `unit`, `category`, `reason` | Owned by production; reconciliation reads it (§3.5) |
| `domain_events` (00009) | `id bigint`, `type`, `aggregate_type`, `aggregate_id`, `dedupe_key`, `payload jsonb`, `occurred_at`; unique (type, aggregate_id, dedupe_key); append-only trigger | Sprint 6 events; the consumer reads by `id` |
| `jobs` (pre-launch) | `kind`, `payload`, `status` (`queued/running/done/failed`), `attempts`, `max_attempts`, `run_at`, `last_error` | Event delivery retries and dead letters |
| `admin_alerts` (00008) | dedupe key, kind, severity, permission | Stock and purchasing alerts |
| `roles`, `permissions`, `role_permissions` (00004) | `resource.action` keys only | New roles and permissions |

## 3. Integration contracts (as they are in the code)

### 3.1 Production requirements

`GET /api/v1/admin/production/requirements?plan_id=` (`production.read`), and
`production.Service.Plan(id).Requirements` in Go. Each `Requirement`:

| Field | Type | Note |
|---|---|---|
| `id` | uuid | requirement row |
| `ingredient_id`, `ingredient_name` | uuid, string | |
| `preparation` | string | `""` when none; part of the identity |
| `quantity` | decimal string or **null** | null when a source could not be converted (`status` says why) |
| `unit` | string | the ingredient's base unit |
| `status` | `ok` / `unconvertible` / `inactive_ingredient` | |
| `approximate` | bool | an average piece conversion was used |
| `sources[]` | `meal_name`, `recipe_version` (**number**), `batch_code` (**code, not id**, null before approval), `quantity`, `unit` | |

The required-by date is not on the requirement: it is the plan's `production_date`. The plan
carries `snapshot_id` and `snapshot_revision`.

**Gap:** the HTTP sources carry codes and numbers, not ids, and there is no Go read that returns
batch ids per requirement. Sprint 6 needs ids for traceability, so production gains one
read-only export (`production.Service.ProcurementRequirements(planID)`): per requirement, the
batch ids, plan id, snapshot id and production date. Production keeps owning the tables; nothing
outside production writes them.

### 3.2 Events

`outbox.Record` inserts with `ON CONFLICT (type, aggregate_id, dedupe_key) DO NOTHING`, in the
caller's transaction. Payloads as written:

| Event | Aggregate | Payload |
|---|---|---|
| `production.plan_released` | `production_plan`, plan id | `week`, `production_date`, `kits`, `requirements[]` of `ingredient_id`, `preparation`, `quantity`, `unit` |
| `production.batch_completed` | `production_batch`, batch id | `code`, `meal_id`, `recipe_version_id`, `accepted_kits`, `production_date`, `plan_id` |
| `production.ready_for_fulfillment` | `production_batch`, batch id | same as `batch_completed` |

Dedupe keys are `""` for all three: one event per plan or batch.

### 3.3 Nothing consumes events yet

There is no consumer, cursor or delivery table. Sprint 6 adds the first consumer
(`production.plan_released` → a procurement requirement snapshot). See plan §6.

### 3.4 Jobs

`jobs.Runner` claims with `FOR UPDATE SKIP LOCKED`, retries with backoff, marks `failed` after
`max_attempts` (the dead-letter set), recovers stuck jobs, and **fails a job whose kind has no
handler** (so only the worker, which registers every kind, may run the queue). Enqueue takes
the caller's transaction.

### 3.5 Waste ownership

`production_waste` records what the kitchen threw away during a batch, as weighed. It is not a
stock movement: stock issued to a batch has already left inventory. Sprint 6 rule: inventory
owns stock movements (receipts, issues, returns, transfers, adjustments, **store waste** of stock
still in storage). Batch consumption = issued − returned. Production waste for that batch is
shown inside consumption, never posted again as a stock movement. No double count.

### 3.6 Alerts

`backoffice.Service.Raise(ctx, q, support.Alert{DedupeKey, Kind, Severity, Title, Body,
ResourceType, ResourceID, Permission})`: deduplicated, shown only to holders of the permission.

## 4. Reusable infrastructure

- Exact quantities and conversions: `qty`, `culinary.Converter`. No second measurement engine.
- Transactions, locks and errors: `db.InTx`, `pg_advisory_xact_lock` helper pattern
  (`production.lock`), `httpx` errors, `FOR UPDATE` row locks.
- Outbox, jobs, alerts, audit, as above.
- Admin UI: `KForm`, `Panel`, `DataTable`, `StatusTag`, `Notice`, `Steps`, `Timeline`,
  permission-filtered navigation groups.
- Test harness: `newHarness`, `newShop` (real paid orders), `newTeam` and `publishRecipe`
  (Sprint 5), `misectl production seed-demo` for released demo batches.

## 5. Differences between the Sprint 5 report and the code, and risks

1. **`ingredient_cost_references.source` is free text**, not an enum. "Adding `supplier_price`"
   means a new column (`origin`: `reference` for existing rows, `supplier_quote`,
   `purchase_order`, `receipt`) and links to the quote, PO line or receipt line, so quoted,
   ordered and actual costs stay distinct and none is overwritten.
2. **Requirement sources have no ids** (§3.1); fixed by a production read, not by reading
   production tables from procurement.
3. **Requirements can be null** (`unconvertible`). Procurement must mark such a line incomplete
   rather than treat it as zero. (Approval of a plan with one is blocked, so a released plan
   should not have them; the code still handles it.)
4. **Released plans cannot change**; a new demand revision raises
   `production_demand_changed` and leaves the plan. Procurement must snapshot what it consumed
   and flag a newer revision for review rather than recalculate silently.
5. **`ingredients.purchasing_unit` is free text** and `shelf_life_note` is free text. Neither can
   drive pack rounding or expiry. Supplier products carry their own purchase unit and pack size;
   lot expiry comes only from a recorded date.
6. **Concurrency risk:** reservations and issues contend for the same lot. Plan: lock the lot
   row (`FOR UPDATE`) and keep the lot's reserved and remaining quantities in the same
   transaction as the movement, with CHECK constraints that they never go negative.
7. **Clock drift** on the development PC (about two hours on 13 November): date-sensitive tests
   use the harness clock, never the machine clock.
8. **Development demo data** (`misectl production seed-demo`) is demo content and never evidence
   of real operations; four completed demo batches still show open tasks from before the
   Sprint 5 completion rule and are left unchanged.

## 6. Missing prerequisites (business decisions)

None of these exist in the code or the brief. Each becomes a configurable policy that ships
empty and blocks or labels what depends on it (plan §11):

supplier qualification criteria; payment terms; purchase approval thresholds (and whether
finance approves above one); receiving tolerances; safety stock; storage locations and
capacity; receiving inspection criteria; shelf-life rules; lot numbering policy; approved
purchasing currencies (TTD is the business currency from Sprint 3; no other is assumed);
supplier communication integrations (none: email exists for customers and staff only).

## 7. Proposed Sprint 6 boundaries

Three new modules, following ADR-001 (packages, not services):

```
culinary (ingredients, conversions, costs) ◄── reads/AddCost ── suppliers, procurement, inventory
production (plans, requirements, batches, waste) ── ProcurementRequirements(plan), Batch(id) ──► procurement, inventory
production.plan_released ── outbox → jobs ──► procurement (requirement snapshot, idempotent)
suppliers ── offers, prices, qualification ──► procurement
procurement ── approved PO lines ──► inventory (receiving)
inventory ── stock position, inbound ──► procurement (net requirements)
inventory ── batch_ingredient_lots ──► traceability (production_batches.id)
all three ── backoffice.Raise, audit.Record, outbox.Record
reporting ── read-only SQL ──► procurement, inventory, supplier reports
```

- **`internal/suppliers`**: suppliers, contacts, farmer profiles, qualification, documents
  (metadata only: no file storage exists), products (offers), price history, availability,
  communication log, performance figures.
- **`internal/procurement`**: requirement snapshots from released plans, net requirement
  calculation, recommendations, RFQs, quotations, purchase orders, approvals, revisions.
- **`internal/inventory`**: storage locations, receipts, inspections, lots, the movement
  ledger, reservations, issues, returns, adjustments, counts, `batch_ingredient_lots`.

Out of scope (brief §51): dispatch, drivers, routes, delivery tracking, mobile apps, forecasting,
supplier payments, accounting, external messaging, a supplier portal.
