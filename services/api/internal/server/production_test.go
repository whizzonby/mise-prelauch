package server_test

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"mise.tt/api/internal/payments/testpay"
)

// Sprint 5 integration tests: ingredients and units, recipes and their
// versions, the chef, culinary and nutrition workflow, catalogue claims,
// demand snapshots from real paid orders, production plans, ingredient
// requirements, batches, kitchen tasks, quality holds, packaging, yield,
// waste, labels, reports, events and RBAC.

type culinaryTeam struct {
	director, chef, chef2, nutri, manager, kitchen, inspector reqOpt
}

func newTeam(h *harness) culinaryTeam {
	return culinaryTeam{
		director:  bearer(h.adminLogin("director@mise.test", "culinary_director")),
		chef:      bearer(h.adminLogin("chef@mise.test", "chef")),
		chef2:     bearer(h.adminLogin("chef2@mise.test", "chef")),
		nutri:     bearer(h.adminLogin("nutri@mise.test", "nutritionist")),
		manager:   bearer(h.adminLogin("manager@mise.test", "production_manager")),
		kitchen:   bearer(h.adminLogin("cook@mise.test", "kitchen_staff")),
		inspector: bearer(h.adminLogin("qa@mise.test", "quality_inspector")),
	}
}

func (h *harness) ingredient(as reqOpt, body map[string]any) string {
	h.t.Helper()
	res := h.do("POST", "/api/v1/admin/ingredients", body, as)
	h.wantStatus(res, http.StatusCreated)
	return res.Data["id"].(string)
}

var completeNutrition = map[string]any{"energy_kcal": "540", "protein_g": "38.5", "carbs_g": "52", "fat_g": "18.2", "sat_fat_g": "6.1",
	"fibre_g": "4", "sugar_g": "5.5", "sodium_mg": "720", "source": "laboratory_analysis", "method": "Lab report MISE-TEST-1"}

// publishRecipe takes a recipe for a meal through the whole workflow.
func (h *harness) publishRecipe(t culinaryTeam, mealID, name string, lines []map[string]any, contains []string) string {
	h.t.Helper()
	res := h.do("POST", "/api/v1/admin/recipes", map[string]any{"name": name, "meal_id": mealID}, t.chef)
	h.wantStatus(res, http.StatusCreated)
	id := res.Data["id"].(string)
	h.wantStatus(h.do("PUT", "/api/v1/admin/recipes/"+id+"/draft", map[string]any{"yield_servings": 2, "ingredients": lines,
		"preparation_steps": []string{"Portion every ingredient into its bag."}, "allergens_contains": contains, "nutrition": completeNutrition,
		"storage_instructions": "Keep chilled."}, t.chef), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/recipes/"+id+"/submit", nil, t.chef), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/recipes/"+id+"/review", map[string]any{"decision": "approved"}, t.director), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/nutrition/reviews/"+id+"/approve", nil, t.nutri), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/recipes/"+id+"/approve", nil, t.director), http.StatusOK)
	res = h.do("POST", "/api/v1/admin/recipes/"+id+"/publish", nil, t.director)
	h.wantStatus(res, http.StatusOK)
	return id
}

// ---------------------------------------------------------------- culinary

func TestIngredientsUnitsAndConversions(t *testing.T) {
	h := newHarness(t)
	team := newTeam(h)
	chicken := h.ingredient(team.director, map[string]any{"name": "Chicken breast", "category": "poultry", "base_unit": "g", "aliases": []string{"Chicken fillet"}})
	// One ingredient per name or alias, whatever the case and spacing.
	h.wantError(h.do("POST", "/api/v1/admin/ingredients", map[string]any{"name": "chicken   BREAST", "category": "poultry", "base_unit": "g"}, team.director), http.StatusConflict, "duplicate_ingredient")
	h.wantError(h.do("POST", "/api/v1/admin/ingredients", map[string]any{"name": "Chicken fillet", "category": "poultry", "base_unit": "g"}, team.director), http.StatusConflict, "duplicate_ingredient")
	// Chefs read the catalogue but do not change it.
	h.wantStatus(h.do("POST", "/api/v1/admin/ingredients", map[string]any{"name": "Thyme", "category": "herbs_spices", "base_unit": "g"}, team.chef), http.StatusForbidden)
	h.wantStatus(h.do("GET", "/api/v1/admin/ingredients?q=fillet", nil, team.chef), http.StatusOK)
	res := h.do("GET", "/api/v1/admin/ingredients?q=fillet", nil, team.chef)
	if items := list(t, res.Data["items"]); len(items) != 1 || obj(t, items[0])["id"] != chicken {
		t.Errorf("search by alias = %v", res.Data["items"])
	}
	coconut := h.ingredient(team.director, map[string]any{"name": "Coconut milk", "category": "packaged", "base_unit": "g"})
	onion := h.ingredient(team.director, map[string]any{"name": "Onion", "category": "produce", "base_unit": "g"})
	// Same-dimension units already convert exactly; nothing to record.
	h.wantStatus(h.do("POST", "/api/v1/admin/ingredients/"+coconut+"/conversions", map[string]any{"from_unit": "ml", "to_unit": "l", "factor": "0.001", "source": "x"}, team.director), http.StatusUnprocessableEntity)
	// Volume to mass needs a sourced factor.
	h.wantStatus(h.do("POST", "/api/v1/admin/ingredients/"+coconut+"/conversions", map[string]any{"from_unit": "ml", "to_unit": "g", "factor": "1.03"}, team.director), http.StatusUnprocessableEntity)
	h.wantStatus(h.do("POST", "/api/v1/admin/ingredients/"+coconut+"/conversions", map[string]any{"from_unit": "ml", "to_unit": "g", "factor": "1.03", "source": "Supplier specification sheet"}, team.director), http.StatusOK)
	// Pieces vary: a piece conversion must say it is an average.
	h.wantStatus(h.do("POST", "/api/v1/admin/ingredients/"+onion+"/conversions", map[string]any{"from_unit": "piece", "to_unit": "g", "factor": "150", "source": "Kitchen weighing"}, team.director), http.StatusUnprocessableEntity)
	res = h.do("POST", "/api/v1/admin/ingredients/"+onion+"/conversions", map[string]any{"from_unit": "piece", "to_unit": "g", "factor": "150", "approximate": true, "source": "Average of 20 weighed onions"}, team.director)
	h.wantStatus(res, http.StatusOK)
	if c := list(t, res.Data["conversions"]); len(c) != 1 || obj(t, c[0])["approximate"] != true {
		t.Errorf("conversions = %v", res.Data["conversions"])
	}
	// Units are the reference list.
	res = h.do("GET", "/api/v1/admin/units", nil, team.chef)
	if len(list(t, res.Data["items"])) != 6 {
		t.Errorf("units = %v", res.Data["items"])
	}
	// Kitchen staff record waste in these units, without the ingredient catalogue.
	cook := bearer(h.adminLogin("units-cook@mise.test", "kitchen_staff"))
	h.wantStatus(h.do("GET", "/api/v1/admin/units", nil, cook), http.StatusOK)
	h.wantStatus(h.do("GET", "/api/v1/admin/ingredients", nil, cook), http.StatusForbidden)
	// No price is invented: costing says the estimate is incomplete.
	res = h.do("POST", "/api/v1/admin/recipes", map[string]any{"name": "Costing check"}, team.chef)
	h.wantStatus(res, http.StatusCreated)
	rid := res.Data["id"].(string)
	h.wantStatus(h.do("PUT", "/api/v1/admin/recipes/"+rid+"/draft", map[string]any{"ingredients": []map[string]any{{"ingredient_id": chicken, "quantity": "0.5", "unit": "kg"}}}, team.chef), http.StatusOK)
	res = h.do("GET", "/api/v1/admin/recipes/"+rid+"/costing", nil, team.chef)
	h.wantStatus(res, http.StatusOK)
	if res.Data["complete"] != false || obj(t, list(t, res.Data["lines"])[0])["status"] != "no_reference" {
		t.Errorf("costing without prices = %v", res.Data)
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/cost-references", map[string]any{"ingredient_id": chicken, "amount_minor": 4500, "per_quantity": "1", "unit": "kg",
		"source": "Test quote", "effective_from": "2026-01-01"}, team.director), http.StatusCreated)
	res = h.do("GET", "/api/v1/admin/recipes/"+rid+"/costing", nil, team.chef)
	// 0.5 kg at TTD 45.00 a kg; packaging still unknown, so still incomplete.
	if res.Data["ingredients_minor"] != float64(2250) || res.Data["complete"] != false {
		t.Errorf("costing = %v", res.Data)
	}
}

func TestRecipeWorkflowVersioningAndCatalogueClaims(t *testing.T) {
	s := newShop(t)
	h := s.h
	team := newTeam(h)
	chicken := h.ingredient(team.director, map[string]any{"name": "Chicken thigh", "category": "poultry", "base_unit": "g"})
	flour := h.ingredient(team.director, map[string]any{"name": "Wheat flour", "category": "baking", "base_unit": "g", "allergens": []string{"wheat"}})
	rice := h.ingredient(team.director, map[string]any{"name": "Parboiled rice", "category": "grains", "base_unit": "g"})
	meal := s.meals[0]

	res := h.do("POST", "/api/v1/admin/recipes", map[string]any{"name": "Curry chicken", "meal_id": meal}, team.chef)
	h.wantStatus(res, http.StatusCreated)
	id := res.Data["id"].(string)
	// A meal has one recipe.
	h.wantStatus(h.do("POST", "/api/v1/admin/recipes", map[string]any{"name": "Other curry", "meal_id": meal}, team.chef2), http.StatusUnprocessableEntity)
	// Another chef cannot edit it.
	h.wantError(h.do("PUT", "/api/v1/admin/recipes/"+id+"/draft", map[string]any{"yield_servings": 4}, team.chef2), http.StatusForbidden, "not_your_recipe")

	// Rice is counted in grams with no volume conversion: millilitres are refused.
	res = h.do("PUT", "/api/v1/admin/recipes/"+id+"/draft", map[string]any{"ingredients": []map[string]any{{"ingredient_id": rice, "quantity": "300", "unit": "ml"}}}, team.chef)
	h.wantStatus(res, http.StatusUnprocessableEntity)
	lines := []map[string]any{
		{"ingredient_id": chicken, "quantity": "500", "unit": "g", "preparation": "diced", "yield_percent": "75"},
		{"ingredient_id": rice, "quantity": "0.3", "unit": "kg"},
		{"ingredient_id": flour, "quantity": "20", "unit": "g"},
	}
	h.wantStatus(h.do("PUT", "/api/v1/admin/recipes/"+id+"/draft", map[string]any{"yield_servings": 2, "ingredients": lines}, team.chef), http.StatusOK)
	// No method yet: cannot submit.
	h.wantError(h.do("POST", "/api/v1/admin/recipes/"+id+"/submit", nil, team.chef), http.StatusUnprocessableEntity, "incomplete")
	h.wantStatus(h.do("PUT", "/api/v1/admin/recipes/"+id+"/draft", map[string]any{"preparation_steps": []string{"Dice and bag the chicken.", "Weigh the rice."}}, team.chef), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/recipes/"+id+"/submit", map[string]any{"notes": "First version"}, team.chef), http.StatusOK)
	// Submitted versions are locked for the chef.
	h.wantError(h.do("PUT", "/api/v1/admin/recipes/"+id+"/draft", map[string]any{"yield_servings": 3}, team.chef), http.StatusConflict, "version_locked")

	// Chefs cannot review or approve.
	h.wantStatus(h.do("POST", "/api/v1/admin/recipes/"+id+"/review", map[string]any{"decision": "approved"}, team.chef), http.StatusForbidden)
	h.wantStatus(h.do("POST", "/api/v1/admin/recipes/"+id+"/approve", nil, team.chef), http.StatusForbidden)
	h.wantStatus(h.do("POST", "/api/v1/admin/nutrition/reviews/"+id+"/approve", nil, team.chef), http.StatusForbidden)
	// Nutrition review starts after the culinary review.
	h.wantError(h.do("POST", "/api/v1/admin/nutrition/reviews/"+id+"/approve", nil, team.nutri), http.StatusConflict, "invalid_transition")
	h.wantStatus(h.do("POST", "/api/v1/admin/recipes/"+id+"/review", map[string]any{"decision": "approved", "notes": "Good balance"}, team.director), http.StatusOK)

	// Incomplete nutrition and an undeclared allergen block the sign-off.
	res = h.do("POST", "/api/v1/admin/nutrition/reviews/"+id+"/approve", nil, team.nutri)
	h.wantError(res, http.StatusUnprocessableEntity, "incomplete")
	h.wantStatus(h.do("POST", "/api/v1/admin/nutrition/reviews/"+id+"/request-changes", map[string]any{"notes": "Add nutrition and declare wheat."}, team.nutri), http.StatusOK)
	res = h.do("GET", "/api/v1/admin/recipes/"+id+"/versions/1", nil, team.chef)
	if res.Data["status"] != "changes_requested" {
		t.Fatalf("status = %v", res.Data["status"])
	}
	// The chef's edit invalidates the earlier culinary approval.
	res = h.do("PUT", "/api/v1/admin/recipes/"+id+"/draft", map[string]any{"nutrition": completeNutrition, "allergens_contains": []string{"wheat"}}, team.chef)
	h.wantStatus(res, http.StatusOK)
	if obj(t, res.Data["signoff"])["culinary"] != false {
		t.Errorf("culinary approval survived an edit: %v", res.Data["signoff"])
	}
	if n := h.count(`SELECT count(*) FROM recipe_reviews r JOIN recipe_versions v ON v.id = r.version_id WHERE v.recipe_id = $1 AND r.invalidated_at IS NULL`, id); n != 0 {
		t.Errorf("valid reviews after an edit = %d", n)
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/recipes/"+id+"/submit", nil, team.chef), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/recipes/"+id+"/review", map[string]any{"decision": "approved"}, team.director), http.StatusOK)
	// Final approval needs the nutrition sign-off first.
	h.wantError(h.do("POST", "/api/v1/admin/recipes/"+id+"/approve", nil, team.director), http.StatusUnprocessableEntity, "incomplete")
	h.wantStatus(h.do("POST", "/api/v1/admin/nutrition/reviews/"+id+"/approve", map[string]any{"notes": "Checked against the lab report."}, team.nutri), http.StatusOK)
	res = h.do("POST", "/api/v1/admin/recipes/"+id+"/approve", nil, team.director)
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != "approved" || obj(t, res.Data["nutrition"])["verified"] != true {
		t.Fatalf("approved version = %v", res.Data)
	}

	// Approved versions are immutable, in the API and in the database.
	h.wantError(h.do("PUT", "/api/v1/admin/recipes/"+id+"/draft", map[string]any{"yield_servings": 4}, team.director), http.StatusConflict, "version_locked")
	if _, err := h.pool.Exec(context.Background(), `UPDATE recipe_version_ingredients SET quantity = 1 WHERE version_id = $1`, res.Data["id"]); err == nil {
		t.Error("the database let an approved BOM change")
	}
	if _, err := h.pool.Exec(context.Background(), `UPDATE recipe_versions SET yield_servings = 9 WHERE id = $1`, res.Data["id"]); err == nil {
		t.Error("the database let an approved version change")
	}

	// Chefs cannot publish; the culinary director can.
	h.wantStatus(h.do("POST", "/api/v1/admin/recipes/"+id+"/publish", nil, team.chef), http.StatusForbidden)
	res = h.do("POST", "/api/v1/admin/recipes/"+id+"/publish", nil, team.director)
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != "published" || res.Data["effective_from"] != "2026-11-16" {
		t.Fatalf("published = %v", res.Data)
	}
	// The meal now carries the approved claims, verified.
	res = h.do("GET", "/api/v1/admin/catalog/meals/"+meal, nil, s.admin)
	if a := list(t, res.Data["allergens"]); len(a) != 1 || a[0] != "wheat" || res.Data["allergen_status"] != "declared" {
		t.Errorf("meal allergens = %v %v", res.Data["allergens"], res.Data["allergen_status"])
	}
	if obj(t, res.Data["nutrition"])["status"] != "verified" || obj(t, res.Data["nutrition"])["calories"] != float64(540) {
		t.Errorf("meal nutrition = %v", res.Data["nutrition"])
	}
	// ...and the catalogue editor can no longer contradict them.
	h.wantError(h.do("PUT", "/api/v1/admin/catalog/meals/"+meal+"/nutrition", map[string]any{"calories": 100, "protein_g": 1, "carbs_g": 1, "fat_g": 1, "verified": true}, team.nutri),
		http.StatusConflict, "recipe_controls_claims")

	// A revision is a new draft: content copied, no approval carried over.
	res = h.do("POST", "/api/v1/admin/recipes/"+id+"/revise", map[string]any{"change_summary": "Less rice"}, team.chef)
	h.wantStatus(res, http.StatusCreated)
	if res.Data["number"] != float64(2) || res.Data["status"] != "draft" || len(list(t, res.Data["ingredients"])) != 3 || len(list(t, res.Data["reviews"])) != 0 {
		t.Fatalf("revision = %v", res.Data)
	}
	if obj(t, res.Data["nutrition"])["verified"] != false {
		t.Error("nutrition approval was carried into the revision")
	}
	// Only one open revision at a time.
	h.wantError(h.do("POST", "/api/v1/admin/recipes/"+id+"/revise", nil, team.chef), http.StatusConflict, "revision_open")
	// Version 1 is still the one in use.
	res = h.do("GET", "/api/v1/admin/recipes/"+id, nil, team.chef)
	if obj(t, res.Data["in_use"])["number"] != float64(1) {
		t.Errorf("in use = %v", res.Data["in_use"])
	}
	// Every step was audited and evented.
	if n := h.count(`SELECT count(*) FROM audit_log WHERE action LIKE 'recipe.%'`); n < 10 {
		t.Errorf("recipe audit entries = %d", n)
	}
	if n := h.count(`SELECT count(DISTINCT type) FROM domain_events WHERE type IN ('recipe.submitted', 'recipe.approved', 'recipe.published', 'nutrition.review_completed')`); n != 4 {
		t.Errorf("recipe event types = %d", n)
	}
}

// ---------------------------------------------------------------- production

func TestProductionFromPaidOrdersToReadyKits(t *testing.T) {
	s := newShop(t)
	h := s.h
	ctx := context.Background()
	team := newTeam(h)
	curry, fish, pelau := s.meals[0], s.fishMeal, s.meals[1]

	chicken := h.ingredient(team.director, map[string]any{"name": "Chicken breast", "category": "poultry", "base_unit": "g"})
	rice := h.ingredient(team.director, map[string]any{"name": "Rice", "category": "grains", "base_unit": "g"})
	coconut := h.ingredient(team.director, map[string]any{"name": "Coconut milk", "category": "packaged", "base_unit": "g"})
	onion := h.ingredient(team.director, map[string]any{"name": "Onion", "category": "produce", "base_unit": "g"})
	saltfish := h.ingredient(team.director, map[string]any{"name": "Salted cod", "category": "seafood", "base_unit": "g", "allergens": []string{"fish"}})
	h.wantStatus(h.do("POST", "/api/v1/admin/ingredients/"+coconut+"/conversions", map[string]any{"from_unit": "ml", "to_unit": "g", "factor": "1.03", "source": "Spec"}, team.director), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/ingredients/"+onion+"/conversions", map[string]any{"from_unit": "piece", "to_unit": "g", "factor": "150", "approximate": true, "source": "Weighed"}, team.director), http.StatusOK)

	curryRecipe := h.publishRecipe(team, curry, "Curry chicken", []map[string]any{
		{"ingredient_id": chicken, "quantity": "500", "unit": "g", "yield_percent": "75"},
		{"ingredient_id": rice, "quantity": "0.3", "unit": "kg"},
		{"ingredient_id": coconut, "quantity": "100", "unit": "ml"},
		{"ingredient_id": onion, "quantity": "1", "unit": "piece", "preparation": "diced"},
	}, nil)
	h.publishRecipe(team, fish, "Buljol", []map[string]any{
		{"ingredient_id": saltfish, "quantity": "200", "unit": "g"},
		{"ingredient_id": rice, "quantity": "0.2", "unit": "kg"},
		{"ingredient_id": onion, "quantity": "1", "unit": "piece", "preparation": "sliced"},
	}, []string{"fish"})

	// Two paying customers; a third never pays.
	tok1, _ := s.active("cook1@example.com", testpay.CardApprove)
	tok2, _ := s.active("cook2@example.com", testpay.CardApprove)
	tok3, addr3 := s.customer("nopay@example.com")
	s.subscribe(tok3, addr3, s.basic, nil)
	h.wantStatus(h.do("POST", "/api/v1/me/meal-selections/"+s.weekCycle(tok1, "2026-11-16")["id"].(string)+"/finalize", map[string]any{"items": items(curry, 2, fish, 1)}, tok1), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/me/meal-selections/"+s.weekCycle(tok2, "2026-11-16")["id"].(string)+"/finalize", map[string]any{"items": items(curry, 1, fish, 1, pelau, 1)}, tok2), http.StatusOK)

	// Before the cutoff, demand is not final: no snapshot.
	h.wantError(h.do("POST", "/api/v1/admin/production/snapshots", map[string]any{"week": "2026-11-16"}, team.manager), http.StatusConflict, "selection_open")
	s.tick("2026-11-13 00:30")

	// Confirmed, paid demand only: 6 kits from 2 orders.
	res := h.do("GET", "/api/v1/admin/production/demand?week=2026-11-16", nil, team.manager)
	h.wantStatus(res, http.StatusOK)
	if res.Data["kits"] != float64(6) || res.Data["orders"] != float64(2) || res.Data["final"] != true {
		t.Fatalf("demand = %v", res.Data)
	}
	// A snapshot twice is one snapshot.
	res = h.do("POST", "/api/v1/admin/production/snapshots", map[string]any{"week": "2026-11-16"}, team.manager)
	h.wantStatus(res, http.StatusCreated)
	snapID := obj(t, res.Data["snapshot"])["id"]
	res = h.do("POST", "/api/v1/admin/production/snapshots", map[string]any{"week": "2026-11-16"}, team.manager)
	h.wantStatus(res, http.StatusOK)
	if obj(t, res.Data["snapshot"])["id"] != snapID || res.Data["created"] != false {
		t.Fatalf("second snapshot = %v", res.Data)
	}
	if n := h.count(`SELECT count(*) FROM production_demand_snapshots`); n != 1 {
		t.Fatalf("snapshots = %d", n)
	}
	if n := h.count(`SELECT count(*) FROM production_demand_lines WHERE order_number IN (SELECT number FROM orders o JOIN customers c ON c.id = o.customer_id JOIN users u ON u.id = c.user_id WHERE u.email = 'nopay@example.com')`); n != 0 {
		t.Errorf("unpaid lines in the snapshot = %d", n)
	}

	// Kitchen staff and chefs cannot plan.
	h.wantStatus(h.do("POST", "/api/v1/admin/production/plans", map[string]any{"week": "2026-11-16"}, team.kitchen), http.StatusForbidden)
	h.wantStatus(h.do("POST", "/api/v1/admin/production/plans", map[string]any{"week": "2026-11-16"}, team.chef), http.StatusForbidden)
	// Five managers clicking at once make one plan.
	var wg sync.WaitGroup
	statuses := make([]int, 5)
	for i := range statuses {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			statuses[i] = h.do("POST", "/api/v1/admin/production/plans", map[string]any{"week": "2026-11-16"}, team.manager).Status
		}(i)
	}
	wg.Wait()
	if n := h.count(`SELECT count(*) FROM production_plans`); n != 1 {
		t.Fatalf("plans after concurrent creates = %d (statuses %v)", n, statuses)
	}
	res = h.do("POST", "/api/v1/admin/production/plans", map[string]any{"week": "2026-11-16"}, team.manager)
	h.wantStatus(res, http.StatusOK)
	plan := res.Data
	planID := plan["id"].(string)
	if plan["production_date"] != "2026-11-16" || plan["status"] != "draft" || plan["planned_kits"] != float64(6) {
		t.Fatalf("plan = %v", plan)
	}

	// Requirements: exact, converted, traceable; different preparations never merged.
	want := map[string]string{"Chicken breast|": "2000", "Rice|": "1300", "Coconut milk|": "309", "Onion|diced": "450", "Onion|sliced": "300", "Salted cod|": "400"}
	reqs := list(t, plan["requirements"])
	if len(reqs) != len(want) {
		t.Fatalf("requirements = %v", reqs)
	}
	for _, r := range reqs {
		m := obj(t, r)
		key := m["ingredient_name"].(string) + "|" + m["preparation"].(string)
		if m["quantity"] != want[key] || m["unit"] != "g" {
			t.Errorf("%s = %v %v, want %s g", key, m["quantity"], m["unit"], want[key])
		}
		if strings.HasPrefix(key, "Onion") && m["approximate"] != true {
			t.Errorf("%s is not marked approximate", key)
		}
		if len(list(t, m["sources"])) == 0 {
			t.Errorf("%s has no sources", key)
		}
	}
	// Pelau has no recipe: the plan is blocked until a reasoned adjustment.
	h.wantError(h.do("POST", "/api/v1/admin/production/plans/"+planID+"/approve", nil, team.manager), http.StatusUnprocessableEntity, "plan_blocked")
	var pelauItem string
	for _, it := range list(t, plan["items"]) {
		if obj(t, it)["meal_id"] == pelau {
			pelauItem = obj(t, it)["id"].(string)
		}
	}
	h.wantStatus(h.do("PATCH", "/api/v1/admin/production/plans/"+planID+"/items/"+pelauItem, map[string]any{"planned_kits": 0}, team.manager), http.StatusUnprocessableEntity)
	h.wantStatus(h.do("PATCH", "/api/v1/admin/production/plans/"+planID+"/items/"+pelauItem, map[string]any{"planned_kits": 0, "reason": "No approved recipe yet; support is contacting the customer."}, team.manager), http.StatusOK)
	// The order itself is untouched.
	if n := h.count(`SELECT count(*) FROM order_items WHERE meal_id = $1`, pelau); n != 1 {
		t.Errorf("pelau order lines = %d", n)
	}

	// Checklists and task templates come from the business's procedures.
	h.wantStatus(h.do("POST", "/api/v1/admin/quality/templates", map[string]any{"name": "Allergen label matches recipe", "check_type": "allergen_declaration", "criteria": "Per SOP-QA-1.", "critical": true}, team.kitchen), http.StatusForbidden)
	res = h.do("POST", "/api/v1/admin/quality/templates", map[string]any{"name": "Allergen label matches recipe", "check_type": "allergen_declaration", "criteria": "Per SOP-QA-1.", "critical": true}, team.manager)
	h.wantStatus(res, http.StatusCreated)
	allergenCheck := obj(t, list(t, res.Data["items"])[0])["id"].(string)
	res = h.do("POST", "/api/v1/admin/quality/templates", map[string]any{"name": "Bags sealed", "check_type": "packaging_integrity", "criteria": "Per SOP-QA-2.", "position": 2}, team.manager)
	h.wantStatus(res, http.StatusCreated)
	var sealCheck string
	for _, x := range list(t, res.Data["items"]) {
		if obj(t, x)["name"] == "Bags sealed" {
			sealCheck = obj(t, x)["id"].(string)
		}
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/production/task-templates", map[string]any{"name": "Portion proteins", "kind": "portioning", "due_offset_minutes": 600}, team.manager), http.StatusCreated)
	h.wantStatus(h.do("POST", "/api/v1/admin/production/task-templates", map[string]any{"name": "Assemble kits", "kind": "assembly", "position": 2}, team.manager), http.StatusCreated)

	res = h.do("POST", "/api/v1/admin/production/plans/"+planID+"/approve", nil, team.manager)
	h.wantStatus(res, http.StatusOK)
	if len(list(t, res.Data["batches"])) != 2 {
		t.Fatalf("batches = %v", res.Data["batches"])
	}
	res = h.do("POST", "/api/v1/admin/production/plans/"+planID+"/release", nil, team.manager)
	h.wantStatus(res, http.StatusOK)
	h.wantError(h.do("POST", "/api/v1/admin/production/plans/"+planID+"/release", nil, team.manager), http.StatusConflict, "invalid_transition")
	if n := h.count(`SELECT count(*) FROM production_tasks`); n != 4 {
		t.Errorf("tasks = %d", n)
	}
	if n := h.count(`SELECT count(*) FROM orders WHERE status = 'awaiting_production'`); n != 2 {
		t.Errorf("orders awaiting production = %d", n)
	}
	var curryBatch, fishBatch string
	for _, b := range list(t, res.Data["batches"]) {
		m := obj(t, b)
		if m["status"] != "released" {
			t.Errorf("batch %v status %v", m["code"], m["status"])
		}
		if m["meal_id"] == curry {
			curryBatch = m["id"].(string)
		} else {
			fishBatch = m["id"].(string)
		}
	}

	// The kitchen: tasks wait for the batch to start.
	res = h.do("GET", "/api/v1/admin/production/tasks?batch_id="+curryBatch, nil, team.kitchen)
	tasks := list(t, res.Data["items"])
	task := obj(t, tasks[0])["id"].(string)
	h.wantError(h.do("PATCH", "/api/v1/admin/production/tasks/"+task, map[string]any{"status": "in_progress"}, team.kitchen), http.StatusConflict, "batch_not_started")
	h.wantStatus(h.do("POST", "/api/v1/admin/production/batches/"+curryBatch+"/start", map[string]any{"assigned_team": "Line A"}, team.kitchen), http.StatusOK)
	if n := h.count(`SELECT count(*) FROM orders WHERE status = 'in_production'`); n != 2 {
		t.Errorf("orders in production = %d", n)
	}
	// Staff take tasks themselves; only managers assign others.
	h.wantStatus(h.do("PATCH", "/api/v1/admin/production/tasks/"+task, map[string]any{"assigned_to": h.userIDOf("manager@mise.test")}, team.kitchen), http.StatusForbidden)
	res = h.do("PATCH", "/api/v1/admin/production/tasks/"+task, map[string]any{"assigned_to": "me", "status": "in_progress"}, team.kitchen)
	h.wantStatus(res, http.StatusOK)
	h.wantStatus(h.do("PATCH", "/api/v1/admin/production/tasks/"+task, map[string]any{"status": "blocked"}, team.kitchen), http.StatusUnprocessableEntity)
	h.wantStatus(h.do("PATCH", "/api/v1/admin/production/tasks/"+task, map[string]any{"status": "done", "note": "All portions weighed."}, team.kitchen), http.StatusOK)

	// Output: reworked kits count once; rejected kits are never available.
	h.wantStatus(h.do("POST", "/api/v1/admin/production/batches/"+curryBatch+"/output", map[string]any{"produced_kits": 3, "rejected_kits": 4}, team.kitchen), http.StatusUnprocessableEntity)
	res = h.do("POST", "/api/v1/admin/production/batches/"+curryBatch+"/output", map[string]any{"produced_kits": 3, "rejected_kits": 1, "reworked_kits": 1}, team.kitchen)
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != "awaiting_quality_check" || obj(t, res.Data["yield"])["accepted_kits"] != float64(2) {
		t.Fatalf("after output = %v", res.Data)
	}

	// Quality: kitchen staff cannot inspect; a failed critical check holds the batch.
	h.wantStatus(h.do("POST", "/api/v1/admin/quality/checks", map[string]any{"batch_id": curryBatch, "template_id": allergenCheck, "result": "pass"}, team.kitchen), http.StatusForbidden)
	res = h.do("POST", "/api/v1/admin/quality/checks", map[string]any{"batch_id": curryBatch, "template_id": allergenCheck, "result": "fail", "notes": "Label says no allergens; the recipe is approved without any, but the bag is missing its label."}, team.inspector)
	h.wantStatus(res, http.StatusCreated)
	if res.Data["status"] != "quality_hold" {
		t.Fatalf("after a failed critical check = %v", res.Data["status"])
	}
	holdID := obj(t, list(t, res.Data["holds"])[0])["id"].(string)
	if n := h.count(`SELECT count(*) FROM admin_alerts WHERE kind = 'quality_hold' AND permission = 'quality.release_hold'`); n != 1 {
		t.Errorf("hold alerts = %d", n)
	}
	// The hold blocks completion and packing, and kitchen staff cannot lift it.
	h.wantError(h.do("POST", "/api/v1/admin/production/batches/"+curryBatch+"/complete", nil, team.manager), http.StatusConflict, "quality_hold")
	h.wantError(h.do("PUT", "/api/v1/admin/production/batches/"+curryBatch+"/packaging", map[string]any{"packed_kits": 2}, team.kitchen), http.StatusConflict, "quality_hold")
	h.wantStatus(h.do("POST", "/api/v1/admin/quality/holds/"+holdID+"/release", map[string]any{"notes": "Fine"}, team.kitchen), http.StatusForbidden)
	h.wantStatus(h.do("POST", "/api/v1/admin/quality/holds/"+holdID+"/release", map[string]any{"notes": "Fine"}, team.manager), http.StatusForbidden)
	res = h.do("POST", "/api/v1/admin/quality/holds/"+holdID+"/release", map[string]any{"decision": "release", "notes": "Labels reprinted from the approved recipe."}, team.inspector)
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != "awaiting_quality_check" {
		t.Fatalf("after release = %v", res.Data["status"])
	}
	// Releasing a hold does not pass the batch: it needs a fresh inspection.
	h.wantError(h.do("POST", "/api/v1/admin/production/batches/"+curryBatch+"/complete", nil, team.kitchen), http.StatusUnprocessableEntity, "batch_incomplete")
	for _, c := range []string{allergenCheck, sealCheck} {
		h.wantStatus(h.do("POST", "/api/v1/admin/quality/checks", map[string]any{"batch_id": curryBatch, "template_id": c, "result": "pass"}, team.inspector), http.StatusCreated)
	}
	// Packing: packed must equal accepted, labels verified.
	h.wantError(h.do("PUT", "/api/v1/admin/production/batches/"+curryBatch+"/packaging", map[string]any{"packed_kits": 3, "labels_verified": true, "complete": true}, team.kitchen), http.StatusUnprocessableEntity, "packaging_incomplete")
	h.wantError(h.do("PUT", "/api/v1/admin/production/batches/"+curryBatch+"/packaging", map[string]any{"packed_kits": 2, "complete": true}, team.kitchen), http.StatusUnprocessableEntity, "packaging_incomplete")
	h.wantStatus(h.do("PUT", "/api/v1/admin/production/batches/"+curryBatch+"/packaging", map[string]any{"packaging_type": "Insulated bag", "packed_kits": 2, "labels_verified": true, "complete": true}, team.kitchen), http.StatusOK)
	res = h.do("POST", "/api/v1/admin/production/batches/"+curryBatch+"/complete", nil, team.kitchen)
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != "completed" || res.Data["quality_status"] != "passed" || res.Data["actual_kits"] != float64(2) {
		t.Fatalf("completed batch = %v", res.Data)
	}

	// Waste is recorded as weighed.
	h.wantStatus(h.do("POST", "/api/v1/admin/production/waste", map[string]any{"batch_id": curryBatch, "ingredient_id": chicken, "quantity": "250.5", "unit": "g",
		"category": "preparation_loss", "reason": "Trim"}, team.kitchen), http.StatusCreated)
	h.wantStatus(h.do("POST", "/api/v1/admin/production/waste", map[string]any{"batch_id": curryBatch, "item": "Curry chicken kit", "quantity": "1", "unit": "portion",
		"category": "quality_rejection", "reason": "Torn bag"}, team.kitchen), http.StatusCreated)
	h.wantStatus(h.do("POST", "/api/v1/admin/production/waste", map[string]any{"item": "x", "quantity": "1", "unit": "g", "category": "spoilage", "reason": "x"}, team.chef), http.StatusForbidden)

	// The fish batch, straight through.
	h.wantStatus(h.do("POST", "/api/v1/admin/production/batches/"+fishBatch+"/start", nil, team.kitchen), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/production/batches/"+fishBatch+"/output", map[string]any{"produced_kits": 2}, team.kitchen), http.StatusOK)
	for _, c := range []string{allergenCheck, sealCheck} {
		h.wantStatus(h.do("POST", "/api/v1/admin/quality/checks", map[string]any{"batch_id": fishBatch, "template_id": c, "result": "pass"}, team.inspector), http.StatusCreated)
	}
	h.wantStatus(h.do("PUT", "/api/v1/admin/production/batches/"+fishBatch+"/packaging", map[string]any{"packed_kits": 2, "labels_verified": true, "complete": true}, team.kitchen), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/production/batches/"+fishBatch+"/complete", nil, team.kitchen), http.StatusOK)
	// The first customer's whole order is ready; the second still waits for the pelau.
	var s1, s2 string
	if err := h.pool.QueryRow(ctx, `SELECT o.status FROM orders o JOIN customers c ON c.id = o.customer_id JOIN users u ON u.id = c.user_id WHERE u.email = 'cook1@example.com'`).Scan(&s1); err != nil {
		t.Fatal(err)
	}
	if err := h.pool.QueryRow(ctx, `SELECT o.status FROM orders o JOIN customers c ON c.id = o.customer_id JOIN users u ON u.id = c.user_id WHERE u.email = 'cook2@example.com'`).Scan(&s2); err != nil {
		t.Fatal(err)
	}
	if s1 != "ready_for_fulfillment" || s2 != "in_production" {
		t.Errorf("order statuses = %s, %s", s1, s2)
	}

	// The label comes from the batch's approved recipe version.
	res = h.do("GET", "/api/v1/admin/production/batches/"+fishBatch+"/label", nil, team.kitchen)
	h.wantStatus(res, http.StatusOK)
	if c := list(t, res.Data["allergens_contains"]); len(c) != 1 || c[0] != "Fish" || res.Data["use_by"] != nil || res.Data["recipe_version"] != float64(1) {
		t.Errorf("label = %v", res.Data)
	}

	// Historical production keeps its recipe version after a revision.
	h.wantStatus(h.do("POST", "/api/v1/admin/recipes/"+curryRecipe+"/revise", nil, team.chef), http.StatusCreated)
	res = h.do("GET", "/api/v1/admin/production/batches/"+curryBatch, nil, team.manager)
	if res.Data["recipe_version"] != float64(1) {
		t.Errorf("batch recipe version = %v", res.Data["recipe_version"])
	}

	// A change to orders after the plan makes a new snapshot revision; the
	// released plan is not changed, and managers are alerted.
	if _, err := h.pool.Exec(ctx, `UPDATE orders SET status = 'cancelled' WHERE id = (SELECT o.id FROM orders o JOIN customers c ON c.id = o.customer_id JOIN users u ON u.id = c.user_id WHERE u.email = 'cook2@example.com')`); err != nil {
		t.Fatal(err)
	}
	res = h.do("POST", "/api/v1/admin/production/snapshots", map[string]any{"week": "2026-11-16"}, team.manager)
	h.wantStatus(res, http.StatusCreated)
	if obj(t, res.Data["snapshot"])["revision"] != float64(2) || obj(t, res.Data["snapshot"])["kits"] != float64(3) {
		t.Errorf("revision 2 = %v", res.Data)
	}
	res = h.do("GET", "/api/v1/admin/production/plans/"+planID, nil, team.manager)
	if res.Data["snapshot_revision"] != float64(1) || res.Data["newer_snapshot"] == nil || res.Data["planned_kits"] != float64(5) {
		t.Errorf("plan after demand change = %v", res.Data)
	}
	if n := h.count(`SELECT count(*) FROM admin_alerts WHERE kind = 'production_demand_changed'`); n != 1 {
		t.Errorf("demand change alerts = %d", n)
	}

	// Requirements export for procurement.
	res = h.do("GET", "/api/v1/admin/production/requirements?plan_id="+planID+"&format=csv", nil, team.manager)
	if res.Status != http.StatusOK || !strings.Contains(string(res.Raw), "Chicken breast") || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/csv") {
		t.Errorf("csv = %d %s", res.Status, res.Raw)
	}

	// The production report matches the records.
	h.wantStatus(h.do("GET", "/api/v1/admin/reports/production?from=2026-11-09&to=2026-11-22", nil, team.kitchen), http.StatusForbidden)
	res = h.do("GET", "/api/v1/admin/reports/production?from=2026-11-09&to=2026-11-22", nil, team.manager)
	h.wantStatus(res, http.StatusOK)
	totals := map[string]float64{}
	for _, m := range list(t, res.Data["totals"]) {
		totals[obj(t, m)["key"].(string)] = obj(t, m)["value"].(float64)
	}
	for k, v := range map[string]float64{"planned_kits": 5, "completed_kits": 4, "batch_completion_rate": 100, "batches": 2, "quality_holds": 1,
		"rejected_kits": 1, "reworked_kits": 1, "yield_variance": -1, "waste_records": 2} {
		if totals[k] != v {
			t.Errorf("report %s = %v, want %v", k, totals[k], v)
		}
	}
	// The overview and the dashboard show production to the right people.
	h.wantStatus(h.do("GET", "/api/v1/admin/production/overview", nil, team.kitchen), http.StatusOK)
	h.wantStatus(h.do("GET", "/api/v1/admin/production/calendar?from=2026-11-16", nil, team.kitchen), http.StatusOK)

	// Every expected domain event was written once.
	for _, typ := range []string{"production.demand_snapshotted", "production.plan_created", "production.plan_approved", "production.plan_released",
		"production.batch_started", "production.batch_completed", "quality.hold_created", "quality.hold_released", "packaging.completed", "production.ready_for_fulfillment"} {
		if n := h.count(`SELECT count(*) FROM domain_events WHERE type = $1`, typ); n == 0 {
			t.Errorf("no %s event", typ)
		}
	}
	if n := h.count(`SELECT count(*) FROM domain_events WHERE type = 'production.plan_created'`); n != 1 {
		t.Errorf("plan_created events = %d", n)
	}
	// History is append-only.
	if _, err := h.pool.Exec(ctx, `DELETE FROM quality_checks`); err == nil {
		t.Error("quality checks could be deleted")
	}
	if _, err := h.pool.Exec(ctx, `UPDATE domain_events SET payload = '{}'`); err == nil {
		t.Error("domain events could be edited")
	}
}
