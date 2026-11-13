# Sprint 5 report: Chef and Production ERP

Branch `sprint-5/chef-production` (from `sprint-4/admin-crm`, `04b091f`). Brief:
`E:\FULL MISE DEV\SPRINT 5`. Audit: `docs/architecture/SPRINT_5_AUDIT.md`. Plan, state machines
and progress: `docs/architecture/SPRINT_5_PLAN.md`. Written 13 November 2026.

Status words used throughout:
- **Verified**: exercised by an automated test against real PostgreSQL, or end to end in a browser.
- **Implemented, not verified**: built and type-checked, the screen renders (screenshot), but no
  test drives it.
- **Blocked**: needs an input from the business (§18); the software ships it empty and says so.
- **Deferred**: out of this sprint's scope.

Nothing in this sprint is operationally certified. No recipe, nutrition figure, allergen
declaration, checklist, shelf life or price was invented: every one ships empty, and what
depends on it is blocked or labelled (§17, §18).

## 1. Repository audit findings

Full audit: `docs/architecture/SPRINT_5_AUDIT.md`. Every Sprint 4 claim was checked in the code.
In short:

- **Production demand comes from orders**, not selections or carts. The scheduler makes an order
  at the cutoff only for a paid week with confirmed meals, one per subscription week. Sprint 5
  reads demand only from orders whose week has passed its cutoff.
- **Reused, not duplicated:** `chef_profiles` (extended, not a second chef table), the `chef`,
  `nutritionist` and `kitchen_staff` roles, `meal_nutrition` and `meals.allergens` (still the
  customer-facing record, now written from an approved recipe), the alerts, audit log,
  reporting module and admin shell.
- **Gaps filled:** no domain-event table (added `domain_events`, written in the same
  transaction); no exact quantity type (added `platform/qty`); no recipe link on meals (added
  `recipes.meal_id`); orders carry no delivery day (a plan covers a delivery week until Sprint 7).

## 2. Architecture decisions

- **Two modules** (ADR-001, one per bounded context): `internal/culinary` (ingredients, units,
  conversions, cost references, chefs, recipes, versions, reviews, costing, catalogue sync) and
  `internal/production` (demand, plans, requirements, batches, tasks, quality, packing, yield,
  waste, labels, events). `internal/reporting` gains the production report, read-only.
- **No module writes another's tables.** Culinary sets meal claims through the catalogue
  service; production reads demand through the orders service.
- **Exact arithmetic:** quantities are `math/big.Rat` in Go, `numeric` in PostgreSQL and decimal
  strings in JSON; rounded half-up to 3 places once, after aggregation.
- **Hand-written pgx** in both modules (aggregates and multi-table transactions); ADR-002 is
  amended: sqlc stays the default for simple CRUD.
- **Domain events** in an append-only outbox, unique per (type, aggregate, dedupe key), so a
  retry writes nothing. Nobody consumes them yet; Sprint 6 and 7 will.
- **One clock:** culinary and production read the commerce clock, so tests (and the local demo
  seed, §16) move all of them together.

## 3. Implemented modules

| Area | What it does | Status |
|---|---|---|
| Ingredient catalogue | Names and aliases unique case-insensitively, categories, base unit, allergens, storage, active flag | Verified (Go) |
| Units and conversions | Six units; exact same-dimension factors; cross-dimension only with a sourced, ingredient-specific conversion; piece conversions must be marked approximate | Verified (Go) |
| Cost references and costing | Sourced price per quantity; estimate marked incomplete without prices or packaging cost | Verified (Go) |
| Recipes and versions | Header, BOM, steps, allergens, nutrition, versions, revisions | Verified (Go, browser) |
| Chef workspace | "My recipes" first, stepped editor with a visible save state per step | Verified (browser) |
| Culinary, nutrition, final approval, publication | §6, §7 | Verified (Go, browser) |
| Catalogue sync | Publishing sets the meal's allergens and verified nutrition; the catalogue editor can no longer contradict them | Verified (Go) |
| Demand and snapshots | Final after the cutoff, paid orders only, idempotent, revisions when orders change | Verified (Go) |
| Plans and requirements | §9, §10 | Verified (Go); plan screen reviewed |
| Batches and kitchen tasks | §11 | Verified (Go, browser) |
| Quality, holds, packing, labels | §12 | Verified (Go, browser) |
| Yield and waste | §13 | Verified (Go, browser for waste) |
| Production overview, calendar, report | §13 | Verified (Go); screens reviewed |
| Kitchen settings (task and checklist templates, capacity) | Managers record procedures | Verified (Go); screen not driven in a browser |
| Requirements CSV export | For procurement (Sprint 6) | Verified (Go); download not clicked in a browser |

## 4. Database migrations

One migration, `00009_chef_production.sql` (849 lines), additive; its Down drops only what Up
creates and the added columns and permissions. Tables and integrity rules: plan §3. The rules
that matter most are in the database, not just the code:

- an approved version's BOM, steps, allergens, nutrition and content cannot be changed (triggers);
- one open draft per recipe; one live plan per week; one batch per plan item;
- an unchanged demand snapshot is not written twice (content hash);
- reviews, quality checks, task events and domain events cannot be deleted.

No migration was added in Stage 8.

## 5. API endpoints

65 new routes under `/api/v1/admin` (28 culinary, 37 production), listed in plan §7 and documented
in `docs/api/openapi.yaml`; `TestRoutesMatchOpenAPI` checks every registered route is in
the contract, and `packages/api-client` is regenerated from it. Every route needs an admin
session and a permission.

Changed in Stage 8: `GET units` now needs any staff session instead of `ingredients.read`
(see §17, "Found and fixed").

## 6. Chef workflow summary

1. A chef starts a recipe (optionally linked to a catalogue meal; one recipe per meal).
2. They fill it step by step: Details (yield, timing, dietary tags) → Ingredients (BOM, each
   line must convert to the ingredient's base unit) → Method (kitchen preparation required,
   assembly, customer card, storage, packing) → Allergens & nutrition → Review.
3. They submit; submission is refused, with the reason, while anything required is missing.
4. Submitted versions are locked to the chef. If changes are requested, editing reopens, and
   any edit invalidates every earlier review of that version.
5. Chefs edit only recipes they own or collaborate on; they never review, approve or publish.

Browser test: a chef drafts a recipe through every step, a submission without a method is
refused, the submitted version is locked, and chefs see no approval forms.

## 7. Nutritionist workflow summary

- Nutrition review opens only after the culinary review approves.
- Sign-off needs every figure (energy, protein, carbohydrate, fat, saturated fat, fibre,
  sugars, sodium) with a source, and every ingredient allergen declared as "contains".
- The nutritionist can request changes or reject with notes; the sign-off applies to that
  version only and never carries into a revision.
- Final approval (culinary director) needs both a valid culinary review and the nutrition
  sign-off. Nobody reviews or approves a version they wrote.

## 8. Recipe versioning approach

- Each recipe has numbered versions: `draft → submitted → under_review → approved → published
  → archived`, with `changes_requested` back to the chef (plan §4).
- Approved and published versions never change, enforced by the API and by database triggers.
- A change is a revision: a new draft copied from the version, with no review carried over;
  one open revision at a time; the published version stays in use until a new one is published.
- Publication sets the first delivery week (a week whose selection is still open).
- Plans and batches store the exact version used, so history keeps it after a revision
  (`TestProductionFromPaidOrdersToReadyKits`).

## 9. Production planning implementation

1. **Demand** for a delivery week: non-cancelled orders, final only after the cutoff.
2. **Snapshot**: a revision of demand per order and meal, with the recipe version in force for
   that week. Taking it twice is one snapshot. If orders change later, a new revision is raised
   as an exception and managers are alerted; the plan is not changed silently.
3. **Plan**: one live plan per week, from the snapshot. Five managers clicking at once make one
   plan (tested). Kits to make can differ from kits ordered only with a reason, which is audited;
   customer orders are never edited.
4. **Approve**: refused while anything blocks it (a meal without a published recipe, an
   ingredient that cannot be converted). Approving freezes requirements and prepares one batch
   per meal.
5. **Release**: creates the batches' tasks from the configured templates and moves the week's
   orders to `awaiting_production`. Releasing twice is refused.

## 10. Ingredient calculation methodology

Per BOM line: quantity (converted to the ingredient's base unit) × kits × servings per kit ÷
recipe yield servings, ÷ (usable % ÷ 100) where a verified usable share was recorded. Exact
rational arithmetic, aggregated per ingredient **and preparation** ("diced" and "sliced" never
merge), rounded half-up to 3 decimals once. Every requirement keeps its sources (meal, recipe
version, batch, quantity). A line that cannot be converted is an exception that blocks approval;
a requirement using an approximate piece conversion says "weigh before portioning".
`TestProductionFromPaidOrdersToReadyKits` checks six requirements to the gram, including a
ml→g conversion, an approximate piece conversion, a usable share and two preparations of one
ingredient.

## 11. Kitchen execution functionality

- Batch: `released → in_progress → awaiting_quality_check → completed`, with `quality_hold` and
  `cancelled` (plan §4). The batch page is laid out for a tablet: the next action first.
- Tasks wait until their batch starts. Staff take tasks themselves; only managers assign
  others. Blocking a task needs a note and alerts the manager.
- A batch cannot complete while any of its tasks is unfinished (or blocked): tasks are its
  work, and the brief says a batch is never complete merely because a quantity exists. The
  batch page lists each open task under "Still to do".
- Output: kits made, rejected and reworked (reworked kits count once, inside accepted).
- Orders follow production: `awaiting_production` → `in_production` → `ready_for_fulfillment`
  when every batch an order needs is complete.

Browser test: kitchen staff start a released batch, take and finish every task (an open one
is listed as still to do), and record output.

## 12. Quality control and packaging

- Checklists come from the business's procedures (templates); none exist by default, and **no
  batch can complete without one**.
- A failed critical check puts the batch on hold and alerts the holders of
  `quality.release_hold`. The hold blocks completion and packing.
- Only an inspector releases a hold, with a resolution; a released hold does not pass the
  batch: every check must be inspected again.
- Packing: packed kits must equal accepted kits, labels must be verified against the recipe.
- Labels come from the batch's approved recipe version and say "Not reviewed for labelling
  compliance" and "Use by: not set" until those policies exist.

Browser test: an inspector fails the critical check; the batch shows the hold; kitchen staff
cannot complete it, cannot release the hold, and their packing is refused; the inspector
releases the hold and passes every check; packing and completion then succeed.

## 13. Waste and yield reporting

- Yield per batch: planned, produced, rejected, reworked, accepted, variance.
- Waste: weighed or counted quantity, unit, category, reason, optionally an ingredient and a
  batch; recorded as it happens.
- The production report (`production_reports.read`) gives planned and completed kits,
  completion rate, holds, rejections, rework, yield variance and waste for a date range.
  `TestProductionFromPaidOrdersToReadyKits` checks nine report figures against the records.
- The production overview shows plans awaiting approval, batches due, in progress, on hold,
  waiting to be packed, delayed, and blocked tasks.

## 14. RBAC and security changes

New roles: `culinary_director`, `production_manager`, `quality_inspector`. 24 new permissions
(plan §9). Handlers check permissions, never role names. The permission is
`production_reports.read`, not the brief's `production.reports.read`: the permissions table only
allows `resource.action` keys.

| Risk | Handling | Evidence |
|---|---|---|
| Chef approves own or any recipe | Approve, review, publish need their own permissions; nobody reviews their own version | Go: `TestRecipeWorkflowVersioningAndCatalogueClaims`; browser |
| Another chef edits a recipe | Owner, collaborators or `recipes.approve` only | Go (`not_your_recipe`) |
| Approved content changed | API refuses; database triggers refuse | Go (direct `UPDATE` refused) |
| Kitchen staff plan, inspect or release holds | Refused by permission | Go; browser (screens and the API) |
| Quality bypass | Completion needs passed checks, no open hold, packing done | Go; browser |
| Unpaid demand produced | Only orders, which exist only for paid weeks | Go (unpaid customer contributes nothing) |
| Duplicate work | Snapshots, plans, releases, events idempotent; concurrent plan creation | Go |
| Tampering with history | Reviews, checks, task and domain events append-only | Go |
| Audit | Every state change audited in the same transaction | Go (count of `recipe.*` entries) |

## 15. Screenshots of major pages

`docs/screenshots/sprint-5/`, 24 pages at 1440 px and 390 px (48 files), taken with
`node tools/admin-shots.mjs ../docs/screenshots/sprint-5 sprint-5` from `e2e/` after the tests
ran: production overview, calendar, plans, plan, requirements, batches, a completed batch (with
its hold and release in the history), a released batch, kitchen tasks, quality, packaging,
waste, kitchen settings, recipes, recipe review, ingredients, nutrition and costing steps,
ingredients, an ingredient, chefs, nutrition reviews, recipe approvals, production report.

All demo content in them comes from `misectl production seed-demo` (§16) and is marked "Demo".

## 16. Automated test results (13 November 2026)

| Suite | Result |
|---|---|
| Go, all packages (real PostgreSQL, `mise-s1` stack) | All pass; 125 top-level tests (115 after Sprint 4) |
| Vitest | 43 pass (admin 6, web 15, marketing 22) |
| Playwright, whole suite | 47/47 pass (43 earlier + 4 new) |
| Typecheck and lint (all packages) | Clean |

New tests:
- `server/production_test.go`: `TestIngredientsUnitsAndConversions`,
  `TestRecipeWorkflowVersioningAndCatalogueClaims`, `TestProductionFromPaidOrdersToReadyKits`.
- `server/sprint4_debt_test.go`: staff cancel, sign out everywhere, retention (Sprint 4 debt
  D1–D3).
- `platform/qty`: exact quantity parsing, conversion and rounding.
- `e2e/tests/chef-production.spec.ts`: chef draft → publication; released batch → completed
  through a quality hold; role limits on screens and the API; kitchen screens at phone width.

Brief §44 scenarios: 1–8 and 10–11 in the two culinary tests; 9 and 12–28 in
`TestProductionFromPaidOrdersToReadyKits`; 14 and 15 include duplicate and concurrent requests;
1–5, 7, 18–25, 27 also end to end in the browser; 29–30 by the earlier suites, all passing.

**Released batches for the browser test.** With the real clock no week has both final orders
and a published recipe: a recipe published today applies from the next open week, whose orders
are not final until its cutoff. So `misectl production seed-demo` (new, local only) makes one
through the real API, in process, with the clock moved: two demo customers subscribe, pay with
the test provider and choose meals before a future week's cutoff; demo recipes go through the
real chef → culinary → nutrition → approval → publication workflow by demo staff; after the
cutoff only those customers' weeks are settled (not a general scheduler run), and a plan is
snapshotted, created, approved and released. It refuses to run outside `MISE_ENV=local` or with
demo content off, cancels its demo subscriptions so nothing renews, and uses the next week
without a plan each time. Each browser run uses up one released batch.

**Found and fixed in Stage 8:**
- The batch page crashed for kitchen staff and inspectors: it loads the unit list, which needed
  `ingredients.read`. Units are now readable by any staff session (Go regression check added).
- Six screens were wider than a phone (production overview, plans, plan, recipes, recipe,
  ingredients): grid columns grew to fit wide tables. All Sprint 5 grids now let their children
  shrink; the phone test covers eight kitchen screens.
- "1 open tasks" on batch cards.
- A batch could complete with tasks unfinished, leaving them open for good. Completion now
  needs every task done (`TestProductionFromPaidOrdersToReadyKits` checks the refusal). Four
  batches completed before this change on the development database still show open tasks.
- The retention query read the database clock instead of the service clock (Stage 8 D1–D3).

## 17. Known limitations

- **Demo meals carry demo claims locally.** The seed's recipes set allergens and "verified"
  nutrition (figures marked "Demo figures, not measured") on two named demo meals, Curried
  channa and Stewed oxtail; on this development database also on Fried ripe plantain and
  Pumpkin soup (from the first seed runs). Local only; demo meals are marked "Demo content". The
  Sprint 2 meal-detail test now opens Jerk chicken, which no recipe controls.
- Screens not driven in a browser (rendered and reviewed only): calendar, packaging and waste
  lists, kitchen settings forms, ingredient editing (aliases, conversions, prices), chef team,
  recipe revision and withdrawal, plan approval and item adjustment, requirements download,
  nutrition and approval queues. Their API paths are covered by the Go tests.
- One earlier test (`commerce pages are accessible and work on a phone`) failed once while
  Vitest and Playwright ran together, then passed in three runs. Not reproduced.
- Still open from Sprint 4 (D4): Safari, Firefox, real phones, screen readers, load testing.
- The development PC's clock ran about two hours behind on 13 November; times in the
  screenshots reflect it.

## 18. Outstanding business decisions

Each ships empty; what depends on it is blocked or labelled.

| Input | Until provided |
|---|---|
| Approved recipes with real quantities | No plan can be approved for a meal without one |
| Verified nutrition per recipe | The recipe cannot be approved |
| Quality checklists from an approved procedure | **No batch can complete** |
| Kitchen task templates from an approved procedure | Released batches get no tasks (a warning shows) |
| Shelf lives and use-by policy | Labels say "Use by: not set" |
| Label legal review | Labels say "Not reviewed for labelling compliance" |
| Ingredient prices and packaging costs | Costs show as incomplete estimates |
| Production schedule | Proposed Mon/Wed preparation, Tue/Thu delivery, marked "proposed" |
| Kitchen capacity | "No capacity recorded"; plans are not checked against it |

## 19. Sprint 6 readiness

Contracts for procurement (plan §13):
- **Ingredient catalogue** (`ingredients`, stable ids): supplier items link to it; no second
  catalogue.
- **Requirements**: `GET production/requirements?plan_id=` and its CSV, per ingredient,
  preparation, base unit, quantity and sources; fixed once a plan is released.
- **Cost references**: `ingredient_cost_references.source` will gain `supplier_price`.
- **Events**: `production.plan_released` (requirements fixed), `production.batch_completed`,
  `production.ready_for_fulfillment` (Sprint 7 delivery).
- **Lot traceability**: `production_batches.id` is ready for a future `batch_ingredient_lots`.

Not built, by the brief's boundaries: suppliers, purchase orders, inventory and stock
movements, receiving, dispatch, drivers, forecasting, mobile apps.
