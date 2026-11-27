package server_test

import (
	"net/http"
	"strings"
	"testing"

	"mise.tt/api/internal/payments/testpay"
)

// Sprint 9.2 Stage 6: the deterministic scenario engine over HTTP.

var fullAssumptions = map[string]any{
	"active_subscriptions": 100, "new_subscriptions_per_week": 10, "weekly_churn_bps": 500, "renewal_success_bps": 10000,
	"weekly_value_minor": 20000, "price_change_bps": 0, "ingredient_cost_per_kit_minor": 6000, "ingredient_cost_change_bps": 0,
	"packaging_cost_per_kit_minor": 500, "delivery_cost_per_kit_minor": 1500, "provider_fee_bps": 300,
	"fixed_costs_monthly_minor": 520000, "labour_monthly_minor": 0, "marketing_monthly_minor": 0,
}

func inputOf(t *testing.T, proj map[string]any, key string) map[string]any {
	t.Helper()
	for _, x := range list(t, proj["inputs"]) {
		if obj(t, x)["key"] == key {
			return obj(t, x)
		}
	}
	t.Fatalf("no input %s", key)
	return nil
}

func TestScenarioEngineOverHTTP(t *testing.T) {
	h := newHarness(t)
	owner := bearer(h.adminLogin("owner-scn@mise.test", "super_admin"))
	fin := bearer(h.adminLogin("finance-scn@mise.test", "finance"))
	const base = "/api/v1/admin/intelligence/scenarios"

	// Observed inputs: nothing real is recorded, so most are missing (never zero).
	obs := h.do("GET", base+"/observed", nil, fin)
	h.wantStatus(obs, http.StatusOK)
	values := map[string]map[string]any{}
	for _, v := range list(t, obj(t, obs.Data["observed"])["values"]) {
		values[obj(t, v)["key"].(string)] = obj(t, v)
	}
	for _, k := range []string{"weekly_value_minor", "weekly_churn_bps", "renewal_success_bps", "provider_fee_bps", "fixed_costs_monthly_minor", "capacity_kits_per_week", "opening_cash_minor"} {
		if values[k]["value"] != nil || values[k]["basis"] == "" {
			t.Errorf("%s observed = %v", k, values[k])
		}
	}
	if values["active_subscriptions"]["value"] != float64(0) {
		t.Errorf("active observed = %v", values["active_subscriptions"])
	}
	if len(list(t, obs.Data["variables"])) != 16 || len(list(t, obs.Data["change_kinds"])) != 10 {
		t.Fatal("model description incomplete")
	}

	// The base preset with no assumptions projects nothing and says what is missing.
	res := h.do("POST", base+"/run", map[string]any{"horizon_days": 90}, fin)
	h.wantStatus(res, http.StatusOK)
	out := obj(t, res.Data["output"])
	if out["complete"] != false || out["totals"] != nil || !strings.Contains(strings.Join(toStrings(t, out["missing"]), ","), "weekly_value_minor") {
		t.Fatalf("incomplete run = %v", out)
	}

	// With every input entered: the engine's numbers, labelled as assumptions.
	res = h.do("POST", base+"/run", map[string]any{"horizon_days": 30, "values": fullAssumptions}, fin)
	h.wantStatus(res, http.StatusOK)
	out = obj(t, res.Data["output"])
	w1 := obj(t, list(t, out["weeks"])[0])
	if out["complete"] != true || w1["sales_minor"] != float64(2_000_000) || w1["operating_result_minor"] != float64(1_020_000) ||
		obj(t, out["break_even"])["subscriptions"] != float64(11) || len(out["input_hash"].(string)) != 64 {
		t.Fatalf("complete run = %v", out)
	}
	if src := inputOf(t, res.Data, "weekly_value_minor")["source"]; src != "assumption" {
		t.Fatalf("source = %v", src)
	}
	if !strings.Contains(res.Data["preset_note"].(string), "not probabilities") {
		t.Fatal("preset note missing")
	}

	// Refusals: horizon, unknown inputs and changes, out-of-range values.
	for _, body := range []map[string]any{
		{"horizon_days": 45},
		{"horizon_days": 30, "values": map[string]any{"sql": 1}},
		{"horizon_days": 30, "values": map[string]any{"weekly_churn_bps": 20000}},
		{"horizon_days": 30, "values": fullAssumptions, "changes": []any{map[string]any{"kind": "charge_customers"}}},
		{"horizon_days": 30, "preset": "likely"},
	} {
		h.wantStatus(h.do("POST", base+"/run", body, fin), http.StatusUnprocessableEntity)
	}

	// Save, version, dedupe, reproduce, compare.
	created := h.do("POST", base, map[string]any{"name": "Base plan", "horizon_days": 90, "values": fullAssumptions}, fin)
	h.wantStatus(created, http.StatusCreated)
	id := created.Data["id"].(string)
	v1 := obj(t, list(t, created.Data["versions"])[0])
	same := h.do("POST", base+"/"+id+"/versions", map[string]any{"horizon_days": 90, "values": fullAssumptions}, fin)
	h.wantStatus(same, http.StatusOK)
	if same.Data["created"] != false || same.Data["id"] != v1["id"] {
		t.Fatalf("identical inputs made a new version: %v", same.Data["version"])
	}
	v2 := h.do("POST", base+"/"+id+"/versions", map[string]any{"horizon_days": 90, "values": fullAssumptions, "note": "5 % price rise",
		"changes": []any{map[string]any{"kind": "price_increase", "params": map[string]any{"bps": 500}},
			map[string]any{"kind": "kitchen_expansion", "params": map[string]any{"capacity_kits": 150, "investment_minor": 5000000, "week": 4}}}}, fin)
	h.wantStatus(v2, http.StatusOK)
	if v2.Data["created"] != true || v2.Data["version"] != float64(2) {
		t.Fatalf("v2 = %v", v2.Data)
	}
	proj2 := obj(t, v2.Data["projection"])
	if src := inputOf(t, proj2, "price_change_bps")["source"]; src != "what_if" || len(list(t, proj2["assumptions"])) == 0 {
		t.Fatalf("what-if source = %v", src)
	}
	got := h.do("GET", "/api/v1/admin/intelligence/scenario-versions/"+v2.Data["id"].(string), nil, owner)
	h.wantStatus(got, http.StatusOK)
	if got.Data["reproduced"] != true {
		t.Fatal("a stored version does not reproduce")
	}
	cmp := h.do("POST", base+"/compare", map[string]any{"version_ids": []any{v1["id"], v2.Data["id"]}}, fin)
	h.wantStatus(cmp, http.StatusOK)
	cols := list(t, cmp.Data["columns"])
	s1, s2 := obj(t, obj(t, cols[0])["totals"])["sales_minor"].(float64), obj(t, obj(t, cols[1])["totals"])["sales_minor"].(float64)
	if len(cols) != 2 || s2 <= s1 || obj(t, obj(t, cols[1])["totals"])["investments_minor"] != float64(5000000) {
		t.Fatalf("compare = %v vs %v", s1, s2)
	}
	h.wantStatus(h.do("POST", base+"/compare", map[string]any{"version_ids": []any{v1["id"]}}, fin), http.StatusUnprocessableEntity)
	h.wantStatus(h.do("POST", base+"/compare", map[string]any{"version_ids": []any{v1["id"], "00000000-0000-4000-8000-000000000000"}}, fin), http.StatusNotFound)

	// Archive: hidden from the list, no new versions.
	h.wantStatus(h.do("POST", base+"/"+id+"/archive", nil, fin), http.StatusOK)
	if n := len(list(t, h.do("GET", base, nil, fin).Data["items"])); n != 0 {
		t.Fatalf("archived scenario still listed (%d)", n)
	}
	if n := len(list(t, h.do("GET", base+"?archived=true", nil, fin).Data["items"])); n != 1 {
		t.Fatalf("archived list = %d", n)
	}
	h.wantStatus(h.do("POST", base+"/"+id+"/versions", map[string]any{"horizon_days": 30, "values": fullAssumptions}, fin), http.StatusConflict)
	var audits int
	_ = h.pool.QueryRow(t.Context(), `SELECT count(*) FROM audit_log WHERE action LIKE 'intelligence.scenario_%'`).Scan(&audits)
	if audits != 3 { // created, version 2 saved, archived
		t.Fatalf("audit entries = %d", audits)
	}

	// Access: only intelligence_scenario.run.
	for _, role := range []string{"operations", "marketing", "analyst", "support"} {
		tok := bearer(h.adminLogin(role+"-scn@mise.test", role))
		h.wantStatus(h.do("POST", base+"/run", map[string]any{"horizon_days": 30}, tok), http.StatusForbidden)
	}
	h.wantStatus(h.do("GET", base+"/observed", nil), http.StatusUnauthorized)

	// The AI tool runs the same engine and reports missing inputs.
	tool := runTool(h, "run_financial_scenario", map[string]any{"preset": "conservative", "horizon_days": "182"}, fin)
	h.wantStatus(tool, http.StatusOK)
	if _, ok := factOf(t, tool.Data, "scenario.operating_result"); ok {
		t.Fatal("tool projected with missing inputs")
	}
	if lim := strings.Join(toStrings(t, tool.Data["limitations"]), " "); !strings.Contains(lim, "Missing inputs") || !strings.Contains(lim, "not probabilities") {
		t.Fatalf("tool limitations = %s", lim)
	}
	if obj(t, tool.Data["evidence"])["filters"].(map[string]any)["preset"] != "conservative" {
		t.Fatal("tool evidence lacks the preset")
	}
	h.wantStatus(runTool(h, "run_financial_scenario", map[string]any{"horizon_days": "45"}, fin), http.StatusUnprocessableEntity)
	h.wantStatus(runTool(h, "run_financial_scenario", map[string]any{"preset": "custom"}, fin), http.StatusUnprocessableEntity)
}

func TestScenarioObservedCountsOnlyRealSubscriptions(t *testing.T) {
	w := newKitchenWeek(t)
	h, s := w.h, w.s
	fin := bearer(h.adminLogin("finance-obs@mise.test", "finance"))
	s.active("test-only@example.com", testpay.CardApprove)
	s.active("real-sub@example.com", testpay.CardApprove)
	if _, err := h.pool.Exec(t.Context(), `UPDATE payments SET provider = 'stripe' WHERE customer_id = (SELECT c.id FROM customers c JOIN users u ON u.id = c.user_id WHERE u.email = 'real-sub@example.com')`); err != nil {
		t.Fatal(err)
	}
	obs := h.do("GET", "/api/v1/admin/intelligence/scenarios/observed?months=2", nil, fin)
	h.wantStatus(obs, http.StatusOK)
	o := obj(t, obs.Data["observed"])
	for _, v := range list(t, o["values"]) {
		if obj(t, v)["key"] == "active_subscriptions" && obj(t, v)["value"] != float64(1) {
			t.Fatalf("active = %v (test-paid subscriptions must not count)", v)
		}
	}
	if o["months"] != float64(2) {
		t.Fatalf("months = %v", o["months"])
	}
	h.wantStatus(h.do("GET", "/api/v1/admin/intelligence/scenarios/observed?months=13", nil, fin), http.StatusUnprocessableEntity)
}
