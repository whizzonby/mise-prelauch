package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/payments/testpay"
	"mise.tt/api/internal/platform/config"
)

// DemoProduction is what SeedDemoProduction made.
type DemoProduction struct {
	Week    string   `json:"week"`
	Plan    string   `json:"plan"`
	PlanID  string   `json:"plan_id"`
	Batches []string `json:"batches"`
	Meals   []string `json:"meals"`
	Orders  int      `json:"orders"`
}

// SeedDemoProduction makes a released production plan for local
// development and the end-to-end tests. Local only, and only with demo
// content on.
//
// With the real clock, no week has both final orders and a published
// recipe: a recipe published today applies from the next open week, whose
// orders are not final until its cutoff. So this goes through the real API,
// in process, with the clock moved: just before a future week's cutoff two
// demo customers subscribe, pay with the test provider and choose meals,
// and demo recipes go through the chef, culinary, nutrition and publication
// workflow; just after the cutoff only those customers' weeks are settled
// (no general scheduler run), then demand is snapshotted and a plan is
// created, approved and released. Each run uses the next week that has no
// plan. The worker must be running: it processes the test payments.
func SeedDemoProduction(ctx context.Context, cfg config.Config, pool *pgxpool.Pool) (*DemoProduction, error) {
	if cfg.Env != "local" {
		return nil, errors.New("the production demo is only allowed when MISE_ENV=local")
	}
	if !cfg.CatalogDemoEnabled {
		return nil, errors.New("demo content is off (CATALOG_DEMO_ENABLED=false)")
	}
	// The in-process API: the demo's own requests are not rate limited.
	cfg.RateLimitsDisabled = true
	cfg.CustomerRegistrationEnabled = true
	ids, err := NewIdentityService(cfg, pool)
	if err != nil {
		return nil, err
	}
	com, err := NewCommerce(cfg, pool, ids)
	if err != nil {
		return nil, err
	}
	var clock time.Time
	com.SetClock(func() time.Time { return clock })
	handler, _, err := BuildWith(cfg, pool, LimitsFor(cfg), ids, com)
	if err != nil {
		return nil, err
	}
	api := &inproc{h: handler}
	policy := com.Menus.Policy

	// The week: the next open one without a live production plan.
	week := policy.NextOpenWeek(time.Now())
	for i := 0; ; i++ {
		if i == 52 {
			return nil, errors.New("no week without a production plan in the next year")
		}
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM production_plans WHERE week_start = $1 AND status <> 'cancelled'`, week).Scan(&n); err != nil {
			return nil, err
		}
		if n == 0 {
			break
		}
		week = week.AddDate(0, 0, 7)
	}
	weekStr := week.Format("2006-01-02")
	cutoff := policy.CutoffFor(week)
	clock = cutoff.Add(-24 * time.Hour) // selection still open; recipes published now apply from this week
	if _, err := SeedDemoMenus(ctx, cfg, pool, com); err != nil {
		return nil, err
	}

	// The demo team. Their passwords are random and never shown: the
	// accounts exist so that each step is done by a holder of the right role.
	staff := map[string]string{}
	for _, s := range []struct{ key, email, name, role string }{
		{"chef", "demo.chef@example.com", "Demo Chef", "chef"},
		{"director", "demo.culinary@example.com", "Demo Culinary Director", "culinary_director"},
		{"nutritionist", "demo.nutrition@example.com", "Demo Nutritionist", "nutritionist"},
		{"manager", "demo.production@example.com", "Demo Production Manager", "production_manager"},
	} {
		pw := randomSecret()
		if err := ids.UpsertStaff(ctx, s.email, s.name, s.role, pw); err != nil {
			return nil, err
		}
		res, err := api.do("POST", "/api/v1/admin/auth/login", map[string]any{"email": s.email, "password": pw}, "")
		if err != nil {
			return nil, fmt.Errorf("demo %s sign-in: %w", s.key, err)
		}
		if mfa, _ := res["mfa"].(string); mfa == "challenge" || mfa == "enrolment_required" {
			return nil, fmt.Errorf("demo %s sign-in needs MFA (%s); the demo needs ADMIN_MFA_REQUIRED=false", s.key, mfa)
		}
		staff[s.key] = str(res["token"])
	}

	// Two customers on the Basic demo plan.
	plans, err := api.do("GET", "/api/v1/plans", nil, "")
	if err != nil {
		return nil, err
	}
	var basic string
	for _, p := range items(plans) {
		if p["slug"] == "basic" {
			basic = str(p["id"])
		}
	}
	if basic == "" {
		return nil, errors.New("no Basic plan: run misectl commerce seed-demo first")
	}
	tag := week.Format("20060102") + "-" + randomSecret()[:6]
	type customer struct{ token, sub, cycle string }
	var customers []customer
	done := false
	defer func() {
		// A failed run must not leave demo customers renewing every week.
		if !done {
			for _, c := range customers {
				_, _ = api.do("POST", "/api/v1/me/subscriptions/"+c.sub+"/cancel", map[string]any{"reason": "other"}, c.token)
			}
		}
	}()
	for i := 1; i <= 2; i++ {
		email := fmt.Sprintf("demo-kitchen-%s-%d@example.com", tag, i)
		pw := randomSecret()
		if _, err := api.do("POST", "/api/v1/auth/register", map[string]any{"email": email, "password": pw, "first_name": "Demo", "last_name": fmt.Sprintf("Customer %d", i), "accept_terms": true}, ""); err != nil {
			return nil, err
		}
		// Demo customers are confirmed directly, not by the emailed link.
		if err := ids.MarkEmailVerifiedByOperator(ctx, email); err != nil {
			return nil, err
		}
		login, err := api.do("POST", "/api/v1/auth/login", map[string]any{"email": email, "password": pw}, "")
		if err != nil {
			return nil, err
		}
		tok := str(login["token"])
		addr, err := api.do("POST", "/api/v1/me/addresses", map[string]any{"recipient_name": "Demo Customer", "line1": "1 Demo Street", "city": "Woodbrook", "region": "port-of-spain"}, tok)
		if err != nil {
			return nil, err
		}
		sub, err := api.do("POST", "/api/v1/me/subscriptions", map[string]any{"plan_id": basic, "address_id": str(addr["id"])}, tok)
		if err != nil {
			return nil, err
		}
		redirect := str(obj(sub["checkout"])["redirect_url"])
		ref := redirect[bytes.LastIndexByte([]byte(redirect), '/')+1:]
		if _, err := api.do("POST", "/api/v1/test-payments/sessions/"+ref+"/complete", map[string]any{"outcome": "approve", "card": testpay.CardApprove}, ""); err != nil {
			return nil, err
		}
		customers = append(customers, customer{token: tok, sub: str(sub["subscription_id"])})
	}
	// The worker turns the provider's webhooks into paid weeks.
	for i := range customers {
		deadline := time.Now().Add(90 * time.Second)
		for {
			res, err := api.do("GET", "/api/v1/me/subscription", nil, customers[i].token)
			if err != nil {
				return nil, err
			}
			if s := obj(res["subscription"]); s != nil && s["status"] == "active" {
				break
			}
			if time.Now().After(deadline) {
				return nil, errors.New("the test payment was not processed within 90 seconds: is the worker running?")
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Second):
			}
		}
		cycles, err := api.do("GET", "/api/v1/me/subscription-cycles", nil, customers[i].token)
		if err != nil {
			return nil, err
		}
		for _, c := range items(cycles) {
			if c["week"] == weekStr {
				customers[i].cycle = str(c["id"])
			}
		}
		if customers[i].cycle == "" {
			return nil, fmt.Errorf("demo customer %d has no week of %s", i+1, weekStr)
		}
	}

	// The demo kitchen makes two named demo meals. Publishing a recipe sets
	// its meal's allergens and verified nutrition, so the other demo meals
	// keep their unconfirmed claims (an end-to-end test relies on that).
	// Other meals are used only if these two are not on the menu.
	page, err := api.do("GET", "/api/v1/me/meal-selections/"+customers[0].cycle, nil, customers[0].token)
	if err != nil {
		return nil, err
	}
	menu := obj(page["menu"])
	if menu == nil {
		return nil, fmt.Errorf("no menu for the week of %s", weekStr)
	}
	recipes, err := recipesByMeal(api, staff["director"])
	if err != nil {
		return nil, err
	}
	var named, withRecipe, without []map[string]any
	for _, m := range list(menu["meals"]) {
		meal := obj(m)
		r := recipes[str(meal["id"])]
		published := r != nil && (obj(r["in_use"]) != nil || obj(r["latest"])["status"] == "published")
		switch {
		case r != nil && !published:
			// An unfinished recipe: someone is working on it.
		case meal["name"] == "Curried channa and rice" || meal["name"] == "Stewed oxtail with thyme":
			named = append(named, meal)
		case published:
			// Published by an earlier run for an earlier week. Runs only
			// move forward, so it applies to this week too.
			withRecipe = append(withRecipe, meal)
		default:
			without = append(without, meal)
		}
	}
	chosen := append(append(named, withRecipe...), without...)
	if len(chosen) < 2 {
		return nil, errors.New("the menu needs two meals without an unfinished recipe")
	}
	chosen = chosen[:2]
	out := &DemoProduction{Week: weekStr}
	for _, meal := range chosen {
		out.Meals = append(out.Meals, str(meal["name"]))
		if recipes[str(meal["id"])] == nil {
			if err := demoRecipe(api, staff, meal); err != nil {
				return nil, fmt.Errorf("demo recipe for %s: %w", meal["name"], err)
			}
		}
	}
	if err := demoChecklists(api, staff["manager"]); err != nil {
		return nil, err
	}

	// Meals chosen: three each, six kits in all.
	a, b := str(chosen[0]["id"]), str(chosen[1]["id"])
	choices := [][]map[string]any{
		{{"meal_id": a, "quantity": 2}, {"meal_id": b, "quantity": 1}},
		{{"meal_id": a, "quantity": 1}, {"meal_id": b, "quantity": 2}},
	}
	var cycles []string
	for i, c := range customers {
		if _, err := api.do("POST", "/api/v1/me/meal-selections/"+c.cycle+"/finalize", map[string]any{"items": choices[i]}, c.token); err != nil {
			return nil, err
		}
		cycles = append(cycles, c.cycle)
	}

	// After the cutoff: settle only these weeks, then plan.
	clock = cutoff.Add(30 * time.Minute)
	rep, err := com.Service.SettleCycles(ctx, cycles, clock)
	if err != nil {
		return nil, err
	}
	out.Orders = rep.OrdersCreated
	mgr := staff["manager"]
	if _, err := api.do("POST", "/api/v1/admin/production/snapshots", map[string]any{"week": weekStr}, mgr); err != nil {
		return nil, err
	}
	plan, err := api.do("POST", "/api/v1/admin/production/plans", map[string]any{"week": weekStr}, mgr)
	if err != nil {
		return nil, err
	}
	out.PlanID, out.Plan = str(plan["id"]), str(plan["number"])
	if _, err := api.do("POST", "/api/v1/admin/production/plans/"+out.PlanID+"/approve", nil, mgr); err != nil {
		return nil, err
	}
	released, err := api.do("POST", "/api/v1/admin/production/plans/"+out.PlanID+"/release", nil, mgr)
	if err != nil {
		return nil, err
	}
	for _, bt := range list(released["batches"]) {
		out.Batches = append(out.Batches, str(obj(bt)["code"]))
	}
	// The demo customers stop after this week, so nothing renews them.
	for _, c := range customers {
		if _, err := api.do("POST", "/api/v1/me/subscriptions/"+c.sub+"/cancel", map[string]any{"reason": "other"}, c.token); err != nil {
			return nil, err
		}
	}
	done = true
	return out, nil
}

// recipesByMeal returns every recipe linked to a meal, by meal id.
func recipesByMeal(api *inproc, token string) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	for p := 1; ; p++ {
		res, err := api.do("GET", fmt.Sprintf("/api/v1/admin/recipes?page=%d", p), nil, token)
		if err != nil {
			return nil, err
		}
		page := items(res)
		for _, r := range page {
			if id := str(r["meal_id"]); id != "" {
				out[id] = r
			}
		}
		if len(page) == 0 || len(out) >= int(num(res["total"])) || p > 100 {
			return out, nil
		}
	}
}

// demoRecipe takes a recipe for the meal through the whole workflow, each
// step by the role that owns it. Its allergens are the meal's declared ones.
func demoRecipe(api *inproc, staff map[string]string, meal map[string]any) error {
	var lines []map[string]any
	for _, ing := range []struct{ name, category, qty, prep string }{
		{"Demo chicken thigh", "poultry", "500", "diced"},
		{"Demo long-grain rice", "grains", "300", ""},
		{"Demo onion", "produce", "150", "sliced"},
	} {
		id, err := demoIngredient(api, staff["director"], ing.name, ing.category)
		if err != nil {
			return err
		}
		lines = append(lines, map[string]any{"ingredient_id": id, "quantity": ing.qty, "unit": "g", "preparation": ing.prep})
	}
	contains := []string{}
	for _, t := range list(obj(meal["allergens"])["contains"]) {
		contains = append(contains, str(obj(t)["key"]))
	}
	res, err := api.do("POST", "/api/v1/admin/recipes", map[string]any{"name": str(meal["name"]) + " (demo recipe)", "meal_id": meal["id"]}, staff["chef"])
	if err != nil {
		return err
	}
	id := str(res["id"])
	draft := map[string]any{
		"yield_servings": 2, "ingredients": lines, "allergens_contains": contains,
		"preparation_steps":    []string{"Demo content: weigh and bag every ingredient."},
		"storage_instructions": "Demo content: not an approved storage procedure.",
		"nutrition": map[string]any{"energy_kcal": "540", "protein_g": "38", "carbs_g": "52", "fat_g": "18", "sat_fat_g": "6",
			"fibre_g": "4", "sugar_g": "5", "sodium_mg": "700", "source": "other", "method": "Demo figures, not measured"},
	}
	steps := []struct{ path, as string }{
		{"/submit", "chef"}, {"/review", "director"}, {"nutrition", "nutritionist"}, {"/approve", "director"}, {"/publish", "director"},
	}
	if _, err := api.do("PUT", "/api/v1/admin/recipes/"+id+"/draft", draft, staff["chef"]); err != nil {
		return err
	}
	for _, s := range steps {
		path, body := "/api/v1/admin/recipes/"+id+s.path, map[string]any{}
		if s.path == "nutrition" {
			path = "/api/v1/admin/nutrition/reviews/" + id + "/approve"
		}
		if s.path == "/review" {
			body["decision"] = "approved"
		}
		if _, err := api.do("POST", path, body, staff[s.as]); err != nil {
			return err
		}
	}
	return nil
}

func demoIngredient(api *inproc, token, name, category string) (string, error) {
	res, err := api.do("POST", "/api/v1/admin/ingredients", map[string]any{"name": name, "category": category, "base_unit": "g"}, token)
	if err == nil {
		return str(res["id"]), nil
	}
	found, err2 := api.do("GET", "/api/v1/admin/ingredients?q="+url.QueryEscape(name), nil, token)
	if err2 != nil {
		return "", err
	}
	for _, it := range items(found) {
		if it["name"] == name {
			return str(it["id"]), nil
		}
	}
	return "", err
}

// demoChecklists adds a demo quality checklist and kitchen task when none
// is configured: without them no batch can complete, and released batches
// get no tasks. Real ones come from the business's approved procedures.
func demoChecklists(api *inproc, token string) error {
	q, err := api.do("GET", "/api/v1/admin/quality/templates", nil, token)
	if err != nil {
		return err
	}
	if !anyActive(items(q)) {
		for _, t := range []map[string]any{
			{"name": "Demo check: label matches the approved recipe", "check_type": "allergen_declaration", "criteria": "Demo content, not an approved procedure.", "critical": true},
			{"name": "Demo check: bags sealed", "check_type": "packaging_integrity", "criteria": "Demo content, not an approved procedure.", "position": 2},
		} {
			if _, err := api.do("POST", "/api/v1/admin/quality/templates", t, token); err != nil {
				return err
			}
		}
	}
	tt, err := api.do("GET", "/api/v1/admin/production/task-templates", nil, token)
	if err != nil {
		return err
	}
	if !anyActive(items(tt)) {
		for _, t := range []map[string]any{
			{"name": "Demo task: portion ingredients", "kind": "portioning"},
			{"name": "Demo task: assemble kits", "kind": "assembly", "position": 2},
		} {
			if _, err := api.do("POST", "/api/v1/admin/production/task-templates", t, token); err != nil {
				return err
			}
		}
	}
	return nil
}

func anyActive(xs []map[string]any) bool {
	for _, x := range xs {
		if x["active"] == true {
			return true
		}
	}
	return false
}

// inproc calls the API handler in process, as the integration tests do.
type inproc struct{ h http.Handler }

func (c *inproc) do(method, path string, body any, token string) (map[string]any, error) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, err
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.RemoteAddr = "127.0.0.1:40000"
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	if rec.Code >= 300 {
		return nil, fmt.Errorf("%s %s: %d %s", method, path, rec.Code, bytes.TrimSpace(rec.Body.Bytes()))
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			return nil, fmt.Errorf("%s %s: %w", method, path, err)
		}
	}
	if env.Data == nil {
		env.Data = map[string]any{}
	}
	return env.Data, nil
}

func randomSecret() string {
	b := make([]byte, 18)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func num(v any) float64 {
	f, _ := v.(float64)
	return f
}

func obj(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func list(v any) []any {
	l, _ := v.([]any)
	return l
}

func items(m map[string]any) []map[string]any {
	var out []map[string]any
	for _, x := range list(m["items"]) {
		out = append(out, obj(x))
	}
	return out
}
