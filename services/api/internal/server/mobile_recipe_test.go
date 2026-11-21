package server_test

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"mise.tt/api/internal/commerce"
	"mise.tt/api/internal/payments/testpay"
)

// Sprint 10: approved cooking steps for the app, only for a bought box.

func (h *harness) publishRecipeWithSteps(t culinaryTeam, mealID, name, instructions string, lines []map[string]any) {
	h.t.Helper()
	res := h.do("POST", "/api/v1/admin/recipes", map[string]any{"name": name, "meal_id": mealID}, t.chef)
	h.wantStatus(res, http.StatusCreated)
	id := res.Data["id"].(string)
	h.wantStatus(h.do("PUT", "/api/v1/admin/recipes/"+id+"/draft", map[string]any{"yield_servings": 2, "ingredients": lines,
		"preparation_steps": []string{"KITCHEN ONLY: portion every ingredient into its bag."}, "nutrition": completeNutrition,
		"storage_instructions": "Keep chilled.", "customer_instructions": instructions, "customer_cook_minutes": 30}, t.chef), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/recipes/"+id+"/submit", nil, t.chef), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/recipes/"+id+"/review", map[string]any{"decision": "approved"}, t.director), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/nutrition/reviews/"+id+"/approve", nil, t.nutri), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/recipes/"+id+"/approve", nil, t.director), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/recipes/"+id+"/publish", nil, t.director), http.StatusOK)
}

func TestSplitSteps(t *testing.T) {
	for in, want := range map[string][]string{
		"1. Heat the oil.\n2. Brown the chicken.\n3) Simmer.": {"Heat the oil.", "Brown the chicken.", "Simmer."},
		"Heat the oil.\n\nBrown the chicken,\nthen add rice.":    {"Heat the oil.", "Brown the chicken, then add rice."},
		"  ":            {},
		"- One\n• Two": {"One", "Two"},
	} {
		if got := commerce.SplitSteps(in); !reflect.DeepEqual(got, want) {
			t.Errorf("SplitSteps(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCustomerRecipeOnlyForABoughtBox(t *testing.T) {
	s := newShop(t)
	h := s.h
	team := newTeam(h)
	curry, fish := s.meals[0], s.fishMeal

	chicken := h.ingredient(team.director, map[string]any{"name": "Chicken breast", "category": "poultry", "base_unit": "g"})
	h.publishRecipeWithSteps(team, curry, "Curry chicken", "1. Heat the oil in a deep pan.\n2. Brown the chicken.\n3. Add the curry paste and simmer for 20 minutes.",
		[]map[string]any{{"ingredient_id": chicken, "quantity": "500", "unit": "g"}})

	tok, _ := s.active("cook-at-home@example.com", testpay.CardApprove)
	other, _ := s.active("neighbour@example.com", testpay.CardApprove)
	cycle := s.weekCycle(tok, "2026-11-16")["id"].(string)
	h.wantStatus(h.do("POST", "/api/v1/me/meal-selections/"+cycle+"/finalize", map[string]any{"items": items(curry, 2, fish, 1)}, tok), http.StatusOK)
	s.tick("2026-11-13 00:30")

	res := h.do("GET", "/api/v1/me/orders", nil, tok)
	h.wantStatus(res, http.StatusOK)
	var orderID string
	for _, x := range list(t, res.Data["items"]) {
		if o := obj(t, x); o["week"] == "2026-11-16" {
			orderID = o["id"].(string)
		}
	}
	if orderID == "" {
		t.Fatalf("no order for the week: %s", res.Raw)
	}
	path := "/api/v1/me/orders/" + orderID + "/meals/" + curry + "/recipe"

	r := h.do("GET", path, nil, tok)
	h.wantStatus(r, http.StatusOK)
	steps := list(t, r.Data["steps"])
	if len(steps) != 3 || steps[0] != "Heat the oil in a deep pan." {
		t.Fatalf("steps = %v", steps)
	}
	if r.Data["cook_minutes"] != float64(30) || r.Data["servings"] != float64(2) {
		t.Fatalf("recipe = %v", r.Data)
	}
	if strings.Contains(string(r.Raw), "KITCHEN ONLY") || strings.Contains(string(r.Raw), "cost") {
		t.Fatalf("kitchen-only content leaked: %s", r.Raw)
	}

	// The mobile token works the same way.
	access, _ := tokens(h.mobileLogin("cook-at-home@example.com", "install-cook-0001"))
	h.wantStatus(h.do("GET", path, nil, bearer(access)), http.StatusOK)

	// Not theirs, not signed in, not in the box, or not published: refused.
	h.wantStatus(h.do("GET", path, nil, other), http.StatusNotFound)
	h.wantStatus(h.do("GET", path, nil), http.StatusUnauthorized)
	h.wantStatus(h.do("GET", "/api/v1/me/orders/"+orderID+"/meals/"+s.meals[1]+"/recipe", nil, tok), http.StatusNotFound)
	h.wantError(h.do("GET", "/api/v1/me/orders/"+orderID+"/meals/"+fish+"/recipe", nil, tok), http.StatusNotFound, "recipe_unavailable")
}
