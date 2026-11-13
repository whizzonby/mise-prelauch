# Sprint 5 plan: Chef and Production ERP

Brief: `E:\FULL MISE DEV\SPRINT 5`. Audit: `docs/architecture/SPRINT_5_AUDIT.md`.
Branch: `sprint-5/chef-production`. Migration: `00009_chef_production.sql`.

## 1. Existing files reused

| File | Reuse |
|---|---|
| `internal/identity` (`RequireAdmin`, `Permission`, `AllPermissions`) | Every new route; new permission constants |
| `internal/audit` | Every state change, in the same transaction |
| `internal/platform/db` (`InTx`, `IsUniqueViolation`), `httpx` (errors, `Decode`, `JSON`) | All handlers |
| `internal/backoffice` (`Raise`) | Quality holds, plan exceptions |
| `internal/orders` | New `DemandSource` read (per order and meal line) |
| `internal/catalog` | New `ApplyRecipeClaims` (allergens and verified nutrition onto a meal) and recipe-lock checks |
| `internal/menus` (`Location`, `Policy.CutoffFor`) | Business time, "is the week final" |
| `internal/reporting` | Production report, dashboard attention items |
| `apps/admin` layout, `admin-ui.tsx`, `commerce-ui.tsx`, `catalog-form.tsx` | All screens |
| `docs/api/openapi.yaml`, `packages/api-client` | Contract and typed client |

## 2. Backend modules

Two modules, following ADR-001 (one module per bounded context, not per table):

- **`internal/culinary`**: ingredients, aliases, units and conversions, cost references,
  chefs (on `chef_profiles`), recipes, versions, BOM, steps, allergens, nutrition, reviews and
  approvals, costing estimate, catalogue sync.
- **`internal/production`**: schedule, demand snapshots, plans, requirements, batches, task
  templates and tasks, quality templates, checks and holds, packaging, yield, waste, labels,
  domain events, production report data.

Shared: **`internal/platform/qty`**, exact decimal quantities (`math/big.Rat`), parsed from and
written as decimal strings, rounded half-up to 3 places for storage.

SQL: hand-written pgx in both modules (like `backoffice` and `reporting`), because most queries
are aggregates and multi-table writes inside one transaction. ADR-002 is noted as amended:
sqlc stays the default for simple CRUD modules.

## 3. Database (migration 00009)

Additive. Down drops only what Up creates and removes the added columns and permissions.

| Area | Tables |
|---|---|
| Units | `units_of_measure` (code, dimension mass/volume/count, factor to the dimension's base: g, ml, piece; `portion` is its own dimension) |
| Ingredients | `ingredients` (name unique case-insensitively, category, base unit, purchasing unit, preparation state, allergens, storage, shelf-life note, active), `ingredient_aliases` (unique case-insensitively across names and aliases), `ingredient_unit_conversions` (ingredient-specific, across dimensions, with source and an `approximate` flag), `ingredient_cost_references` (amount per base-unit quantity, source, effective date; empty by default) |
| Chefs | `chef_profiles` + `user_id` (unique), `specialities`, `work_status`; `recipe_collaborators` |
| Recipes | `recipes` (code, name, meal link unique, owner chef, category), `recipe_versions` (number, status, yield servings, serving size, prep and customer cook minutes, storage, packaging and customer instructions, dietary tags, cross-contact note, effective week, author, approver, publish time), `recipe_version_ingredients` (BOM), `recipe_steps`, `recipe_allergens` (contains / may contain), `recipe_nutrition` (per serving, source, method), `recipe_reviews` (stage, decision, notes, reviewer, invalidated time) |
| Planning | `production_schedule_slots` (proposed prep and delivery weekdays), `production_settings` (capacity, one row), `production_demand_snapshots` (week, revision, content hash, cutoff, status), `production_demand_lines` (per order and meal, with recipe version), `production_plans`, `production_plan_items`, `ingredient_requirements`, `ingredient_requirement_sources` |
| Execution | `production_batches`, `production_task_templates`, `production_tasks`, `production_task_events` |
| Quality | `quality_check_templates`, `quality_checks`, `quality_holds` |
| Packing and output | `packaging_records`, `production_yield_records`, `production_waste` |
| Events | `domain_events` (append-only outbox) |

Integrity rules in the database:
- triggers refuse changes to the BOM, steps, allergens and nutrition of a version that is not
  `draft` or `changes_requested`, and to a version's content columns once it is approved;
- one open draft per recipe (partial unique index);
- one demand snapshot per (week, revision), and an unchanged snapshot is not written twice
  (content hash);
- one live plan per week (partial unique on `status <> 'cancelled'`);
- one batch per plan item; batch codes unique;
- `domain_events`, `production_task_events`, `quality_checks` and `recipe_reviews` refuse
  deletes; checks and reviews refuse edits except the review's `invalidated_at`.

## 4. State machines

**Recipe version**
```
draft ──submit──► submitted ──culinary approve──► under_review ──approve (needs culinary + nutrition sign-off)──► approved ──publish──► published ──archive──► archived
  ▲                   │                               │
  └──── edit ◄── changes_requested ◄── request changes / reject (culinary or nutrition)
```
- Submit: owner, collaborator, or `recipes.approve`; needs ingredients, yield, steps.
- Culinary review: `recipes.review`; nutrition sign-off and changes: `nutrition.review` /
  `nutrition.approve`; final approval: `recipes.approve`; publish: `recipes.publish`.
- Nobody reviews or approves a version they authored.
- Any edit in `changes_requested` invalidates every earlier review of that version.
- New revision: copies a published, approved or archived version into a new draft (number + 1)
  with **no** reviews carried over.
- Approval requires: an active ingredient on every line; every allergen of every ingredient
  declared as "contains"; complete nutrition (energy, protein, carbohydrate, fat, saturated
  fat, fibre, sugar, sodium) with a source; a valid culinary and nutrition sign-off.

**Production plan:** `draft ──approve──► approved ──release──► released`; `draft|approved ──cancel──► cancelled`.
Approve needs no blocking exception (a meal with no published recipe, an ingredient that cannot
be converted). Release creates the batches and their tasks.

**Batch:**
```
planned ──release──► released ──start──► in_progress ──record output──► awaiting_quality_check ──complete──► completed
                                                                │ failed critical check            ▲
                                                                ▼                                  │
                                                           quality_hold ──release hold (quality.release_hold)
planned|released|quality_hold ──cancel (production.approve, reason)──► cancelled
```
Complete needs: output recorded and reconciled, every active quality check passed since the
last hold, packaging completed with packed = accepted kits, no open hold.

**Task:** `todo → in_progress → done`, any open state `→ blocked` (with a note) `→ todo`.
**Hold:** `open → released` or `open → discarded` (batch cancelled).

## 5. Ingredient quantity and conversion strategy

- Canonical units: grams for mass, millilitres for volume, pieces for count, portions for
  portion. Each ingredient chooses one as its base unit.
- Same-dimension conversions use the unit table's exact factors (kg = 1000 g, l = 1000 ml).
- Across dimensions (ml → g, piece → g) only with an ingredient-specific conversion row.
  Piece conversions must be marked approximate and say so wherever they are used.
- No conversion → the requirement line is an **exception** (`unconvertible`), shown and blocking
  plan approval. Nothing is guessed.
- Requirement per BOM line = line quantity × planned servings ÷ recipe yield servings ÷
  (usable yield % ÷ 100 when set). Exact rational arithmetic; rounded half-up to 3 decimals
  once, after aggregation.
- Aggregation key: ingredient + preparation specification. "Diced" and "whole" never merge.
- Every requirement keeps its sources: plan item, recipe version, BOM line and quantity.

## 6. Domain events

Written to `domain_events` in the same transaction as the change: `recipe.submitted`,
`recipe.approved`, `recipe.published`, `nutrition.review_completed`,
`production.demand_snapshotted`, `production.plan_created`, `production.plan_approved`,
`production.plan_released`, `production.batch_started`, `production.batch_completed`,
`quality.hold_created`, `quality.hold_released`, `packaging.completed`,
`production.ready_for_fulfillment`. Unique (`type`, `aggregate_id`, `dedupe_key`) makes a retry
write nothing. Delivery to consumers is at least once; consumers must be idempotent.

## 7. API (all under `/api/v1/admin`, admin session + permission)

| Area | Routes |
|---|---|
| Units | `GET units` |
| Ingredients | `GET/POST ingredients`, `GET/PATCH ingredients/{id}`, `POST ingredients/{id}/aliases`, `POST ingredients/{id}/conversions`, `POST ingredients/{id}/costs` |
| Chefs | `GET chefs/team`, `PATCH chefs/{id}/team` |
| Recipes | `GET/POST recipes`, `GET recipes/{id}`, `PATCH recipes/{id}`, `GET recipes/{id}/versions/{n}`, `PUT recipes/{id}/draft` (whole draft: header, BOM, steps, allergens, nutrition), `POST recipes/{id}/submit`, `/review` (culinary), `/request-changes`, `/approve`, `/publish`, `/revise`, `/archive`, `GET recipes/{id}/costing` |
| Nutrition | `GET nutrition/reviews`, `POST nutrition/reviews/{id}/approve`, `POST nutrition/reviews/{id}/request-changes` (id = recipe id) |
| Planning | `GET production/overview`, `GET production/calendar`, `GET production/demand`, `POST production/snapshots`, `GET/POST production/plans`, `GET production/plans/{id}`, `PATCH production/plans/{id}/items/{item}`, `POST production/plans/{id}/approve`, `/release`, `/cancel`, `GET production/requirements` (+ `format=csv`) |
| Batches | `GET production/batches`, `GET production/batches/{id}`, `POST .../start`, `/output`, `/complete`, `/cancel`, `GET .../label` |
| Tasks | `GET production/tasks`, `PATCH production/tasks/{id}` |
| Settings | `GET/POST production/task-templates`, `PATCH production/task-templates/{id}`, `GET/POST quality/templates`, `PATCH quality/templates/{id}`, `GET/PUT production/settings` |
| Quality | `GET quality/holds`, `POST quality/checks`, `POST quality/holds/{id}/release` |
| Packing, waste | `PUT production/batches/{id}/packaging`, `GET/POST production/waste` |
| Reports | `GET reports/production` |

Idempotency: snapshots (content hash), plan creation (one live plan per week), release (one
batch per item, tasks unique per batch and template), events (dedupe key), checks (an
`Idempotency-Key` header is not needed: re-posting a check records a second inspection, which
is a real event).

## 8. UI pages (admin console)

Navigation groups, each item shown only with its permission:
- **Kitchen & Production:** Overview (`/production`), Calendar, Plans, Batches, Kitchen tasks
  (`/production/tasks`, tablet layout), Quality, Packaging, Waste & yield, Production settings.
- **Culinary:** Recipes (chef workspace: "My recipes" first), Ingredients, Chefs, Nutrition
  reviews, Recipe approvals.
- **Reports:** Production, Ingredient requirements.

The recipe editor is a stepped page (Basics → Ingredients → Method → Allergens & nutrition →
Review), each step its own form with a visible save state, not one long form.

## 9. RBAC

New roles: `culinary_director`, `production_manager`, `quality_inspector`. New permissions:
`ingredients.read`, `ingredients.manage`, `recipes.read`, `recipes.create`, `recipes.edit`,
`recipes.submit`, `recipes.review`, `recipes.approve`, `recipes.publish`, `nutrition.read`,
`nutrition.review`, `nutrition.approve`, `chefs.manage`, `production.read`, `production.plan`,
`production.approve`, `production.execute`, `production.configure`, `quality.read`,
`quality.inspect`, `quality.release_hold`, `packaging.manage`, `waste.record`,
`production_reports.read`.

| Role | Gets |
|---|---|
| super_admin, admin | all |
| culinary_director | ingredients.*, recipes.*, nutrition.read, chefs.manage, production.read, quality.read, production_reports.read |
| chef | ingredients.read, recipes.read/create/edit/submit, nutrition.read, production.read |
| nutritionist | ingredients.read, recipes.read, nutrition.read/review/approve (keeps nutrition.verify) |
| production_manager | ingredients.read, recipes.read, production.*, quality.read, quality.inspect, packaging.manage, waste.record, production_reports.read, orders.read |
| kitchen_staff | production.read, production.execute, packaging.manage, waste.record, quality.read |
| quality_inspector | recipes.read, production.read, quality.read, quality.inspect, quality.release_hold |
| operations | production.read, quality.read, production_reports.read |

Chefs edit only recipes they own or collaborate on, unless they hold `recipes.approve`.

## 10. Food-safety and business dependencies (not invented)

Each ships empty and blocks or labels what depends on it:

| Input | Without it |
|---|---|
| Approved recipes with real quantities | No plan can be approved for a meal without a published recipe |
| Verified nutrition per recipe | The recipe cannot be approved |
| Quality checklists from an approved procedure | No batch can complete |
| Kitchen task templates from an approved procedure | Released batches get no tasks (a warning shows) |
| Shelf lives and use-by policy | Labels say "Use-by: not set" |
| Ingredient prices | Costs show "incomplete" |
| Production schedule | Proposed Mon/Wed prep, Tue/Thu delivery, marked "proposed" |
| Kitchen capacity | "No capacity recorded"; no warning computed |
| Label legal review | Labels carry "Not reviewed for labelling compliance" |

## 11. Testing strategy

Go integration tests against real PostgreSQL (the `mise-s1` stack), one file
`server/production_test.go` reusing the Sprint 3 `newShop` harness so real paid orders feed
production. They cover the brief's 30 scenarios, including duplicate snapshot, concurrent plan
creation and repeated release. Unit tests for `qty` and the requirement calculation. Playwright:
chef draft to publish, kitchen plan to completed batch, quality hold blocks completion, RBAC
denials, and phone-width checks. Full earlier suites rerun.

## 12. Sprint 4 debt tracked

| # | Item | Plan |
|---|---|---|
| D1 | Staff subscription cancel has no test | Integration test in Stage 8 |
| D2 | "Sign out everywhere" has no test | Integration test in Stage 8 |
| D3 | Retention list has no test | Integration test in Stage 8 |
| D4 | Safari, Firefox, real phones, screen readers, load | Still open; needs people and devices |

## 13. Sprint 6 integration contracts

- **Ingredient catalogue** (`ingredients`, ids stable): procurement links supplier items to it.
- **Requirements**: `GET production/requirements?plan=` and its CSV; per ingredient, preparation,
  base unit, quantity, sources.
- **Cost references**: `ingredient_cost_references.source` will gain `supplier_price`.
- **Events**: `production.plan_released` (requirements fixed), `production.batch_completed`.
- **Lot traceability**: `production_batches.id` is ready to join a future `batch_ingredient_lots`.

## Progress

- [x] Stage 1: audit and plan
- [x] Stage 2: migration 00009, units, ingredients, recipes, versions, BOM (`internal/culinary`, `internal/platform/qty`)
- [x] Stage 3: chef and nutrition workflows, catalogue sync (`catalog/recipes.go`)
- [x] Stage 4: demand snapshots, plans, requirements, batches (`internal/production`)
- [x] Stage 5: tasks, kitchen execution
- [x] Stage 6: quality, packaging, yield, waste, labels
- [x] Stage 7: reports and dashboard (`reporting`), OpenAPI, API client, admin UI (all pages
  written; typecheck, lint and Go suite green 13 Nov after the power loss; screens not yet
  exercised in a browser, which Stage 8 covers)
- [ ] Stage 8: Sprint 4 debt tests, e2e, UI review, screenshots, report
  - [x] D1–D3: `server/sprint4_debt_test.go` (staff cancel, sign out everywhere, retention).
    Fix found on the way: the retention query read the database clock (`now()`,
    `current_date`); it now takes the service clock like the dashboard.
  - [ ] Playwright: chef draft → publish, plan → completed batch, quality hold, RBAC, phone width
  - [ ] UI review and screenshots
  - [ ] Full earlier suites rerun (Vitest, Playwright), `docs/SPRINT_5_REPORT.md`

Notes while building:
- The permission is `production_reports.read`, not `production.reports.read`: the
  permissions table only allows `resource.action` keys.
- Go integration tests: `server/production_test.go` (3 tests, passing 13 Nov).
  `TestScopedRolesDoNotGrantGlobalAccess` now uses the farmer role, since chefs
  have console access from Sprint 5.
- `domain_events` is truncated by the test harness (it has no foreign keys).
