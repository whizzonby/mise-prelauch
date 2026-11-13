# Sprint 5 audit: what the Chef and Production ERP builds on

Written 13 November 2026, on branch `sprint-5/chef-production` (from `sprint-4/admin-crm`,
`04b091f`). Every finding below was checked in the code, not taken from the Sprint 4 report.

## 1. Sprint 4 claims, verified

| Claim in the Sprint 4 report | Checked in | Result |
|---|---|---|
| Migration `00008_admin_crm.sql`, additive and reversible | `services/api/migrations/00008_admin_crm.sql` | Present. Has a `-- +goose Down` that drops only Sprint 4 tables and permissions |
| 43 new admin paths | `internal/backoffice/http.go`, `internal/identity/staff_http.go` | Present; the route contract test (`TestOpenAPICoversEveryRoute`) compares registered patterns with `docs/api/openapi.yaml` |
| Customer 360, CRM, support | `internal/backoffice`, `internal/crm`, `internal/support` | Present |
| Staff permissions in the database | `roles`, `permissions`, `role_permissions`, `user_roles` (00004) | Present. Handlers check permissions, never role names (`identity.RequireAdmin(perm, ...)`) |
| Append-only audit log | `audit_log_append_only` trigger (00008), `internal/audit` | Present. `audit.Record` takes the caller's transaction |
| Operational alerts | `admin_alerts`, `backoffice.Service.Raise` (dedupe key, permission per alert) | Present |
| Reports and exports | `internal/reporting` (read-only), `report_exports`, job `backoffice.export` | Present |

## 2. The authoritative source of production demand

**Orders, not selections or carts.**

- `internal/commerce/scheduler.go` (`Tick`) makes an order at the selection cutoff only for a
  subscription week that is **paid** (or free through a promotion) and has **confirmed** meals.
  An unpaid week becomes `unpaid` and makes no order; a week without confirmed meals becomes
  `needs_resolution` and makes no order (`TestMissedSelectionNeedsResolutionAndUnpaidWeekMakesNoOrder`).
- `orders` is unique per `subscription_cycle_id`, so a repeated scheduler run makes no second order.
- `order_items` is a snapshot (meal id, `meal_version`, name, servings, quantity, declared
  allergens) and has an immutability trigger.
- `orders.customer_allergens` copies the customer's declared allergy keys (keys only).
- `orders.status` already lists `awaiting_production`, `in_production` and
  `ready_for_fulfillment`; nothing sets them yet.
- **Existing interface:** `orders.Service.Demand(week)` and `GET /api/v1/admin/demand?week=`
  (`orders.read`) count kits per meal version for non-cancelled orders of a delivery week, with
  order numbers and customer allergy counts. It is **final** only once the week's cutoff has
  passed (`commerce/http.go adminDemand`).

**Decision:** Sprint 5 reads demand only through a new `orders.Service.DemandSnapshotSource`
(per order and meal, non-cancelled orders of a week whose cutoff has passed). It never reads
`meal_selections`, `meal_selection_items` or `checkout_sessions`. Paid-only is guaranteed by how
orders are made, and a test proves an unpaid week contributes nothing.

## 3. What already exists that Sprint 5 must reuse, not duplicate

| Existing | Where | How Sprint 5 uses it |
|---|---|---|
| `chef_profiles` (public chef card: name, bio, photo, status) | 00005 | **Extended** with a link to a staff user, specialities and a working status. No second chef table |
| Roles `chef`, `nutritionist`, `kitchen_staff` | 00004 | Reused. Sprint 5 adds `culinary_director`, `production_manager`, `quality_inspector` |
| `nutrition.verify` and `meal_nutrition` (verified per-serving figures shown to customers) | 00005, `catalog/admin.go SetNutrition` | Stays the customer-facing record. Publishing a recipe version linked to a meal writes the approved figures and allergens to the meal through the catalogue service |
| `meals.allergens`, `meals.allergen_status` | 00005 | As above: set from the approved recipe version when one is published |
| `meal_ingredients` (display text for customers) | 00005 | Left alone. The recipe BOM is not copied into the catalogue |
| `meals.version` (an edit counter) and `order_items.meal_version` | 00005, 00007 | Not a recipe version. Production resolves the recipe version from the meal and the delivery week |
| Job queue (transactional outbox) | `internal/platform/jobs`, ADR-001 | Domain events are written in the same transaction as the change (§6) |
| Alerts | `backoffice.Raise` | Quality holds and plan exceptions raise alerts to the permissions that act on them |
| Reports module and `Report` shape | `internal/reporting`, `GET /admin/reports/{kind}` | A `production` report in the same shape |
| Admin shell, permission-filtered nav, `admin-ui.tsx`, `commerce-ui`, server actions | `apps/admin` | All new screens live in the admin console |
| Business time zone | `menus.Location()` (America/Port_of_Spain) | Production dates and "due today" use it |

## 4. Gaps found

| Gap | Consequence for Sprint 5 |
|---|---|
| There is no domain-event table; the job queue needs a handler for every kind | Add an append-only `domain_events` outbox table written in the same transaction. Sprint 6 and 7 consumers read it by id; no handler is needed now, so nothing fails while nobody consumes |
| No unit or quantity type anywhere; catalogue nutrition uses `float64` via sqlc | Add an exact decimal quantity type (`math/big.Rat`, `numeric` in PostgreSQL, strings over JSON) |
| `meals` has no recipe | Add `recipes.meal_id` (one recipe per meal) |
| Orders carry no delivery day inside the week (`delivery_window` is always null) | A production plan covers a delivery week. The "required delivery date" is the week until Sprint 7 assigns days |
| The proposed Mon/Wed preparation, Tue/Thu delivery schedule is not approved | Stored as configurable schedule rows marked `proposed`; nothing depends on it being right |
| No approved food-safety procedures, quality checklists, task templates, shelf lives, prices or nutrition data | All are configuration with empty defaults. Production cannot complete a batch until a quality checklist exists; costs show as incomplete; labels show "use-by not set" |

## 5. Sprint 4 verification debt (brief §43)

Tracked in `docs/architecture/SPRINT_5_PLAN.md` §12. Integration tests for staff cancel, "sign
out everywhere" and the retention list are added in Sprint 5 Stage 8; manual browser, device,
screen reader and load checks remain open and are listed in the report.

## 6. Integration boundaries

```
orders (Sprint 3) ── DemandSnapshotSource(week) ──► production
catalog (Sprint 2) ◄── ApplyRecipeClaims(meal, allergens, nutrition) ── culinary (on publish)
culinary ── RecipeForMeal(meal, week) / VersionBOM(version) ──► production
production ── backoffice.Raise ──► alerts
culinary, production ── domain_events (same transaction) ──► Sprint 6 procurement, Sprint 7 delivery
reporting ── read-only SQL over production tables ──► reports, dashboard
```

No module writes another module's tables. Cross-module reads that are pure reporting
(`reporting`) follow the Sprint 4 rule that the reporting module reads but never writes.
