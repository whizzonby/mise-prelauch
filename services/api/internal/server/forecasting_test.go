package server_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// cloneHistory copies the fixture week's menu, cycles, orders and items into
// earlier weeks (test data only). Every third week drops one order so the
// series varies. Payments are moved to a non-test provider name so the
// history counts as verified for this test.
const cloneHistory = `
DO $$
DECLARE k int; src date := '2026-11-16'; wk date; m uuid; mid uuid; o record; c uuid; n int := 0; skip uuid;
BEGIN
  SELECT id INTO skip FROM orders WHERE week_start = src ORDER BY number DESC LIMIT 1;
  FOR k IN 1..%d LOOP
    wk := src - 7 * k;
    SELECT id INTO m FROM weekly_menus WHERE week_start = src;
    INSERT INTO weekly_menus SELECT (jsonb_populate_record(NULL::weekly_menus, to_jsonb(x) || jsonb_build_object('id', gen_random_uuid(), 'week_start', wk,
      'status', 'closed', 'published_at', coalesce(x.published_at, now())))).* FROM weekly_menus x WHERE x.id = m RETURNING id INTO mid;
    INSERT INTO weekly_menu_meals SELECT (jsonb_populate_record(NULL::weekly_menu_meals, to_jsonb(x) || jsonb_build_object('menu_id', mid))).*
      FROM weekly_menu_meals x WHERE x.menu_id = m;
    FOR o IN SELECT * FROM orders WHERE week_start = src AND status <> 'cancelled' LOOP
      CONTINUE WHEN k % 3 = 0 AND o.id = skip;
      n := n + 1;
      INSERT INTO subscription_cycles SELECT (jsonb_populate_record(NULL::subscription_cycles, to_jsonb(x) || jsonb_build_object('id', gen_random_uuid(),
        'week_start', wk))).* FROM subscription_cycles x WHERE x.id = o.subscription_cycle_id RETURNING id INTO c;
      INSERT INTO orders SELECT (jsonb_populate_record(NULL::orders, to_jsonb(x) || jsonb_build_object('id', gen_random_uuid(), 'number', x.number || '-H' || n,
        'subscription_cycle_id', c, 'menu_id', mid, 'week_start', wk, 'status', 'delivered'))).* FROM orders x WHERE x.id = o.id;
      INSERT INTO order_items SELECT (jsonb_populate_record(NULL::order_items, to_jsonb(i) || jsonb_build_object('id', gen_random_uuid(),
        'order_id', (SELECT id FROM orders WHERE number = o.number || '-H' || n)))).* FROM order_items i WHERE i.order_id = o.id;
    END LOOP;
  END LOOP;
END $$`

func TestForecastingIsGatedAdvisoryAndUsesApprovedRecipes(t *testing.T) {
	w := newKitchenWeek(t)
	h := w.h
	ctx := context.Background()
	h.clock.Set(at("2026-11-23 10:00")) // the fixture week is now complete history
	admin := bearer(h.adminLogin("chief@mise.test", "admin"))
	analyst := bearer(h.adminLogin("numbers@mise.test", "analyst"))

	// Only test-provider payments so far: refused, with nothing forecast.
	res := h.do("GET", "/api/v1/admin/forecasting/readiness", nil, analyst)
	h.wantStatus(res, http.StatusOK)
	if res.Data["ready"] != false || res.Data["verified_weeks"] != float64(0) || res.Data["test_paid_kits"].(float64) <= 0 ||
		res.Data["message"] != "Not enough verified historical demand to generate a reliable forecast." {
		t.Fatalf("readiness = %v", res.Data)
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/forecasting/runs", nil, analyst), http.StatusForbidden)
	res = h.do("POST", "/api/v1/admin/forecasting/runs", nil, admin)
	h.wantStatus(res, http.StatusCreated)
	if res.Data["status"] != "refused" || res.Data["values"] != nil {
		t.Fatalf("run = %v", res.Data)
	}

	// Real-provider history, but for demo meals only, is still refused.
	if _, err := h.pool.Exec(ctx, `UPDATE payments SET provider = 'fixture_card'`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(ctx, `UPDATE meals SET is_demo = true`); err != nil {
		t.Fatal(err)
	}
	res = h.do("GET", "/api/v1/admin/forecasting/readiness", nil, admin)
	if res.Data["verified_weeks"] != float64(0) || res.Data["demo_meal_kits"].(float64) <= 0 {
		t.Fatalf("demo readiness = %v", res.Data)
	}
	if _, err := h.pool.Exec(ctx, `UPDATE meals SET is_demo = false`); err != nil {
		t.Fatal(err)
	}

	// Criteria are the business's: they need an approval, and one verified week is not enough.
	h.wantStatus(h.do("PUT", "/api/v1/admin/forecasting/settings", map[string]any{"min_weeks": 8}, admin), 422)
	h.wantStatus(h.do("PUT", "/api/v1/admin/forecasting/settings", map[string]any{"min_weeks": 8, "approval_ref": "x"}, analyst), http.StatusForbidden)
	h.wantStatus(h.do("PUT", "/api/v1/admin/forecasting/settings", map[string]any{"min_weeks": 8, "max_missing_share": 0.2, "min_improvement": 0,
		"min_interval_residuals": 10, "approval_ref": "Planning review 20 Nov"}, admin), http.StatusOK)
	res = h.do("GET", "/api/v1/admin/forecasting/readiness", nil, admin)
	if res.Data["ready"] != false || res.Data["verified_weeks"] != float64(1) || !strings.Contains(strings.Join(reasons(t, res.Data["reasons"]), " "), "Weeks of verified history: 1, needs 8") {
		t.Fatalf("insufficient readiness = %v", res.Data)
	}

	// Ten weeks of verified history: a forecast is produced and versioned.
	if _, err := h.pool.Exec(ctx, strings.Replace(cloneHistory, "%d", "9", 1)); err != nil {
		t.Fatal(err)
	}
	if res := h.do("GET", "/api/v1/admin/forecasting/readiness", nil, admin); res.Data["ready"] != true {
		t.Fatalf("readiness with history = %v", res.Data)
	}
	counts := func() []int {
		return []int{h.count(`SELECT count(*) FROM production_plans`), h.count(`SELECT count(*) FROM production_plan_items`),
			h.count(`SELECT count(*) FROM ingredient_requirements`), h.count(`SELECT count(*) FROM procurement_runs`),
			h.count(`SELECT count(*) FROM purchase_orders`), h.count(`SELECT count(*) FROM orders`)}
	}
	before := counts()
	res = h.do("POST", "/api/v1/admin/forecasting/runs", nil, admin)
	h.wantStatus(res, http.StatusCreated)
	if res.Data["status"] != "completed" || res.Data["method"] == nil || res.Data["input_to"] != "2026-11-16" {
		t.Fatalf("run = %v", res.Data)
	}
	run := res.Data["id"].(string)
	values := list(t, res.Data["values"])
	if len(values) == 0 || obj(t, values[0])["week"] != "2026-11-23" {
		t.Fatalf("values = %v", values)
	}
	selected, naive := 0, false
	for _, b := range list(t, res.Data["backtests"]) {
		if obj(t, b)["selected"] == true {
			selected++
		}
		naive = naive || obj(t, b)["method"] == "naive"
	}
	if selected != 1 || !naive {
		t.Errorf("backtests = %v", res.Data["backtests"])
	}
	// Runs are append-only.
	if _, err := h.pool.Exec(ctx, `UPDATE forecast_values SET expected = 0 WHERE run_id = $1`, run); err == nil {
		t.Error("a forecast value was changed")
	}

	// Ingredients come from the published recipes through the production engine, and change nothing.
	res = h.do("GET", "/api/v1/admin/forecasting/runs/"+run+"/ingredients?week=2026-11-23", nil, analyst)
	h.wantStatus(res, http.StatusOK)
	needs := list(t, res.Data["needs"])
	if len(needs) == 0 || !strings.HasPrefix(res.Data["label"].(string), "Forecast, not demand") {
		t.Fatalf("projection = %v", res.Data)
	}
	want := map[string]bool{}
	rows, err := h.pool.Query(ctx, `SELECT DISTINCT ing.name FROM recipe_version_ingredients l JOIN recipe_versions v ON v.id = l.version_id
		JOIN ingredients ing ON ing.id = l.ingredient_id JOIN recipes r ON r.id = v.recipe_id
		WHERE v.status = 'published' AND r.meal_id IN (SELECT meal_id FROM forecast_values WHERE run_id = $1 AND week = '2026-11-23' AND expected > 0)`, run)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		want[n] = true
	}
	rows.Close()
	for _, n := range needs {
		if !want[obj(t, n)["ingredient_name"].(string)] {
			t.Errorf("%v is not in an approved recipe", n)
		}
	}
	if len(needs) < len(want) {
		t.Errorf("needs %d, recipes use %d ingredients", len(needs), len(want))
	}
	after := counts()
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("forecasting changed operational records: %v → %v", before, after)
		}
	}
	h.wantStatus(h.do("GET", "/api/v1/admin/forecasting/runs/"+run+"/ingredients?week=2026-11-23", nil, w.team.kitchen), http.StatusForbidden)
	if res := h.do("GET", "/api/v1/admin/forecasting/runs", nil, w.team.manager); res.Status != http.StatusOK || len(list(t, res.Data["items"])) != 2 {
		t.Errorf("production manager runs = %d %v", res.Status, res.Data)
	}
}

func reasons(t *testing.T, v any) []string {
	t.Helper()
	out := []string{}
	for _, x := range list(t, v) {
		out = append(out, x.(string))
	}
	return out
}
