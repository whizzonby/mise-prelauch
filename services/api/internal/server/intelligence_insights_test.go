package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// Sprint 9.2 Stage 7: risks, opportunities, recommendations, meal
// intelligence and meal proposals. All advisory.

func userID(t *testing.T, h *harness, email string) string {
	t.Helper()
	var id string
	if err := h.pool.QueryRow(t.Context(), `SELECT id FROM users WHERE email = $1`, email).Scan(&id); err != nil {
		t.Fatalf("user %s: %v", email, err)
	}
	return id
}

func riskByKey(t *testing.T, res response) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, x := range list(t, res.Data["items"]) {
		out[obj(t, x)["key"].(string)] = obj(t, x)
	}
	return out
}

func TestRiskIndicatorsAreExplainableAndConfigurable(t *testing.T) {
	h := newHarness(t)
	owner := bearer(h.adminLogin("owner-risk@mise.test", "super_admin"))
	admin := bearer(h.adminLogin("admin-risk@mise.test", "admin"))
	fin := bearer(h.adminLogin("finance-risk@mise.test", "finance"))
	ops := bearer(h.adminLogin("ops-risk@mise.test", "operations"))

	res := h.do("GET", "/api/v1/admin/intelligence/risks", nil, owner)
	h.wantStatus(res, http.StatusOK)
	risks := riskByKey(t, res)
	if len(risks) != 11 {
		t.Fatalf("owner sees %d risks", len(risks))
	}
	for k, r := range risks {
		st := r["status"].(string)
		if st != "triggered" && st != "clear" && st != "not_evaluable" || r["explanation"] == "" || r["review_action"] == "" ||
			r["previous_from"] == "" || r["freshness"] == "" {
			t.Errorf("%s = %v", k, r)
		}
		if strings.Contains(strings.ToLower(fmt.Sprint(r)), "probab") && !strings.Contains(r["explanation"].(string), "No probability") {
			t.Errorf("%s mentions a probability", k)
		}
	}
	// No expenses recorded: every category is missing, so the records risk is high.
	inc := risks["incomplete_financial_records"]
	if inc["status"] != "triggered" || inc["severity"] != "high" || inc["value"] != float64(11) {
		t.Fatalf("incomplete records = %v", inc)
	}
	if !strings.Contains(strings.Join(toStrings(t, inc["limitations"]), " "), "engineering default") {
		t.Fatal("default threshold not labelled")
	}
	// Nothing to compare: not evaluated, never a made-up value.
	if r := risks["refunds_growing"]; r["status"] != "not_evaluable" || r["value"] != nil {
		t.Fatalf("refunds = %v", r)
	}
	if r := risks["cash_shortage"]; r["status"] != "not_evaluable" {
		t.Fatalf("cash = %v", r)
	}

	// Each role sees only its domains.
	finRisks := riskByKey(t, h.do("GET", "/api/v1/admin/intelligence/risks", nil, fin))
	if finRisks["payment_failures"] == nil || finRisks["inventory_expiry"] != nil || finRisks["delivery_failures"] != nil {
		t.Fatalf("finance risks = %v", keys(finRisks))
	}
	opsRisks := riskByKey(t, h.do("GET", "/api/v1/admin/intelligence/risks", nil, ops))
	if opsRisks["delivery_failures"] == nil || opsRisks["payment_failures"] != nil || opsRisks["cash_shortage"] != nil {
		t.Fatalf("operations risks = %v", keys(opsRisks))
	}

	// Saving a triggered risk is idempotent; an untriggered one cannot be saved.
	save := func(key string, tok reqOpt) response {
		return h.do("POST", "/api/v1/admin/intelligence/recommendations/from-risk", map[string]any{"key": key}, tok)
	}
	first := save("incomplete_financial_records", owner)
	h.wantStatus(first, http.StatusCreated)
	again := save("incomplete_financial_records", admin)
	h.wantStatus(again, http.StatusOK)
	if first.Data["id"] != again.Data["id"] || first.Data["basis"] != "observed" || first.Data["kind"] != "risk" {
		t.Fatalf("saved = %v / %v", first.Data, again.Data)
	}
	h.wantStatus(save("refunds_growing", owner), http.StatusConflict)
	h.wantStatus(save("no_such_risk", owner), http.StatusNotFound)
	h.wantStatus(save("incomplete_financial_records", fin), http.StatusForbidden) // finance cannot review recommendations

	// Thresholds: configure only, a reason, and a consistent direction.
	path := "/api/v1/admin/intelligence/risk-thresholds/incomplete_financial_records"
	h.wantStatus(h.do("PUT", path, map[string]any{"enabled": true, "warn": 20, "critical": 30, "note": "owner"}, admin), http.StatusForbidden)
	h.wantStatus(h.do("PUT", path, map[string]any{"enabled": true, "warn": 20, "critical": 30}, owner), http.StatusUnprocessableEntity)
	h.wantStatus(h.do("PUT", path, map[string]any{"enabled": true, "warn": 20, "critical": 10, "note": "x"}, owner), http.StatusUnprocessableEntity)
	h.wantStatus(h.do("PUT", "/api/v1/admin/intelligence/risk-thresholds/nope", map[string]any{"enabled": true, "warn": 1, "critical": 2, "note": "x"}, owner), http.StatusNotFound)
	set := h.do("PUT", path, map[string]any{"enabled": true, "warn": 20, "critical": 30, "note": "Owner: only flag when most categories are missing"}, owner)
	h.wantStatus(set, http.StatusOK)
	if set.Data["is_default"] != false {
		t.Fatalf("threshold = %v", set.Data)
	}
	if r := riskByKey(t, h.do("GET", "/api/v1/admin/intelligence/risks", nil, owner))["incomplete_financial_records"]; r["status"] != "clear" {
		t.Fatalf("after the change = %v", r)
	}
	h.wantStatus(h.do("PUT", path, map[string]any{"enabled": false, "warn": 20, "critical": 30, "note": "off"}, owner), http.StatusOK)
	if r := riskByKey(t, h.do("GET", "/api/v1/admin/intelligence/risks", nil, owner))["incomplete_financial_records"]; r["status"] != "not_evaluable" {
		t.Fatalf("disabled = %v", r)
	}

	// The AI tool sees the same indicators, filtered by the caller.
	tool := runTool(h, "business_risks", nil, fin)
	h.wantStatus(tool, http.StatusOK)
	if _, ok := factOf(t, tool.Data, "risk.payment_failures"); !ok {
		t.Fatal("business_risks lacks payment failures")
	}
	for _, f := range list(t, tool.Data["facts"]) {
		if strings.HasPrefix(obj(t, f)["key"].(string), "risk.inventory") {
			t.Fatal("finance saw an operational risk through the tool")
		}
	}
}

func keys(m map[string]map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestRecommendationsAreAdvisoryAndScoped(t *testing.T) {
	h := newHarness(t)
	owner := bearer(h.adminLogin("owner-rec@mise.test", "super_admin"))
	ops := bearer(h.adminLogin("ops-rec@mise.test", "operations"))
	h.adminLogin("support-rec@mise.test", "support")
	base := "/api/v1/admin/intelligence/recommendations"

	var before [3]int
	_ = h.pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM purchase_orders), (SELECT count(*) FROM marketing_campaigns), (SELECT count(*) FROM refunds)`).Scan(&before[0], &before[1], &before[2])

	fp := h.do("POST", base, map[string]any{"domain": "financial", "title": "Introduce a corporate plan", "summary": "Several firms asked."}, owner)
	h.wantStatus(fp, http.StatusCreated)
	op := h.do("POST", base, map[string]any{"domain": "operational", "title": "Second packing shift", "summary": "Batches finish late."}, owner)
	h.wantStatus(op, http.StatusCreated)
	if fp.Data["basis"] != "business_proposal" || fp.Data["status"] != "open" {
		t.Fatalf("proposal = %v", fp.Data)
	}
	h.wantStatus(h.do("POST", base, map[string]any{"domain": "nowhere", "title": "x"}, owner), http.StatusUnprocessableEntity)

	// Operations sees only operational and general items.
	items := list(t, h.do("GET", base, nil, ops).Data["items"])
	if len(items) != 1 || obj(t, items[0])["title"] != "Second packing shift" {
		t.Fatalf("operations sees %v", items)
	}
	h.wantStatus(h.do("GET", base+"/"+fp.Data["id"].(string), nil, ops), http.StatusNotFound)

	id := op.Data["id"].(string)
	act := func(action string, body map[string]any) response {
		return h.do("POST", base+"/"+id+"/"+action, body, owner)
	}
	h.wantStatus(act("dismiss", map[string]any{}), http.StatusUnprocessableEntity)
	h.wantStatus(act("assign", map[string]any{"assignee": userID(t, h, "support-rec@mise.test")}), http.StatusUnprocessableEntity)
	h.wantStatus(act("save", nil), http.StatusOK)
	as := act("assign", map[string]any{"assignee": userID(t, h, "ops-rec@mise.test")})
	h.wantStatus(as, http.StatusOK)
	if as.Data["status"] != "assigned" || as.Data["assigned_to"] != userID(t, h, "ops-rec@mise.test") {
		t.Fatalf("assigned = %v", as.Data)
	}
	h.wantStatus(act("validate", map[string]any{}), http.StatusUnprocessableEntity)
	v := act("validate", map[string]any{"note": "Confirmed with the kitchen lead."})
	h.wantStatus(v, http.StatusOK)
	if v.Data["basis"] != "validated" || v.Data["status"] != "assigned" {
		t.Fatalf("validated = %v", v.Data)
	}
	done := act("done", nil)
	h.wantStatus(done, http.StatusOK)
	h.wantStatus(act("dismiss", map[string]any{"note": "late"}), http.StatusConflict)
	h.wantStatus(act("publish", nil), http.StatusNotFound)
	if n := len(list(t, done.Data["events"])); n != 5 {
		t.Fatalf("events = %d", n)
	}
	// Operations cannot review (no intelligence_recommendation.review).
	h.wantStatus(h.do("POST", base+"/"+id+"/save", nil, ops), http.StatusForbidden)

	// Opportunities are observations with a caution; an unknown one cannot be saved.
	opps := h.do("GET", "/api/v1/admin/intelligence/opportunities", nil, owner)
	h.wantStatus(opps, http.StatusOK)
	for _, o := range list(t, opps.Data["items"]) {
		if obj(t, o)["basis"] != "observed" || !strings.Contains(obj(t, o)["caution"].(string), "not a forecast") {
			t.Fatalf("opportunity = %v", o)
		}
	}
	h.wantStatus(h.do("POST", base+"/from-opportunity", map[string]any{"key": "meal_availability:nope"}, owner), http.StatusNotFound)

	// Nothing operational changed.
	var after [3]int
	_ = h.pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM purchase_orders), (SELECT count(*) FROM marketing_campaigns), (SELECT count(*) FROM refunds)`).Scan(&after[0], &after[1], &after[2])
	if before != after {
		t.Fatalf("recommendations changed operational records: %v -> %v", before, after)
	}
}

func TestMealIntelligenceAndProposalsStayDrafts(t *testing.T) {
	w := newKitchenWeek(t)
	h := w.h
	owner := bearer(h.adminLogin("owner-meal@mise.test", "super_admin"))
	ops := bearer(h.adminLogin("ops-meal@mise.test", "operations"))
	mkt := bearer(h.adminLogin("marketing-meal@mise.test", "marketing"))

	mi := h.do("GET", "/api/v1/admin/intelligence/meals", nil, ops)
	h.wantStatus(mi, http.StatusOK)
	if len(list(t, mi.Data["rules"])) == 0 || !strings.Contains(strings.Join(toStrings(t, mi.Data["limitations"]), " "), "ratings") {
		t.Fatalf("meal intelligence = %v", mi.Data)
	}
	for _, m := range list(t, mi.Data["meals"]) {
		mm := obj(t, m)
		if mm["weeks_on_menu"].(float64) > 0 && mm["kits_per_week_on_menu"] == nil {
			t.Fatalf("no availability-adjusted demand: %v", mm)
		}
	}
	h.wantStatus(h.do("GET", "/api/v1/admin/intelligence/meals", nil, mkt), http.StatusForbidden)
	h.wantStatus(runTool(h, "meal_intelligence", nil, ops), http.StatusOK)

	var meals0, recipes0, versions0 int
	count := func() (int, int, int) {
		var a, b, c int
		_ = h.pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM meals WHERE status = 'published'), (SELECT count(*) FROM recipes), (SELECT count(*) FROM recipe_versions)`).Scan(&a, &b, &c)
		return a, b, c
	}
	meals0, recipes0, versions0 = count()

	ev := h.do("GET", "/api/v1/admin/intelligence/meal-proposals/evidence", nil, ops)
	h.wantStatus(ev, http.StatusOK)
	if ev.Data["demand_by_category"] == nil || ev.Data["approved_supplier_products"] == nil {
		t.Fatalf("evidence = %v", ev.Data)
	}

	cr := h.do("POST", "/api/v1/admin/intelligence/meal-proposals", map[string]any{"name": "Pholourie bowl", "description": "Street-food bowl.",
		"target_audience": "Repeat customers", "complexity": "medium",
		"ingredients": []any{map[string]any{"name": "Unicorn saffron", "quantity": "1 pinch"}}}, owner)
	h.wantStatus(cr, http.StatusCreated)
	p := cr.Data
	if p["status"] != "proposed" || p["origin"] != "manual" || len(list(t, p["required_approvals"])) != 6 || p["estimated_cost_minor"] != nil ||
		!strings.Contains(strings.Join(toStrings(t, p["availability_notes"]), " "), "not in the ingredient catalogue") ||
		!strings.Contains(strings.Join(toStrings(t, p["limitations"]), " "), "no recipe, nutrition or allergen claim") {
		t.Fatalf("proposal = %v", p)
	}
	id := p["id"].(string)
	step := func(action string, body map[string]any, tok reqOpt) response {
		return h.do("POST", "/api/v1/admin/intelligence/meal-proposals/"+id+"/"+action, body, tok)
	}
	h.wantStatus(step("approve", map[string]any{"note": "yes"}, owner), http.StatusConflict)
	h.wantStatus(step("review", nil, ops), http.StatusForbidden)
	h.wantStatus(step("review", nil, owner), http.StatusOK)
	h.wantStatus(step("approve", map[string]any{}, owner), http.StatusUnprocessableEntity)
	ap := step("approve", map[string]any{"note": "Develop for the March menu."}, owner)
	h.wantStatus(ap, http.StatusOK)
	if ap.Data["status"] != "approved_for_development" {
		t.Fatalf("approved = %v", ap.Data)
	}
	h.wantStatus(step("link_recipe", map[string]any{"recipe_id": "not-a-uuid"}, owner), http.StatusUnprocessableEntity)
	h.wantStatus(step("link_recipe", map[string]any{"recipe_id": "00000000-0000-4000-8000-000000000000"}, owner), http.StatusUnprocessableEntity)
	h.wantStatus(step("publish", nil, owner), http.StatusNotFound)
	rj := step("reject", map[string]any{"note": "Supplier cannot source it."}, owner)
	h.wantStatus(rj, http.StatusOK)
	if rj.Data["status"] != "rejected" || len(list(t, rj.Data["events"])) != 4 {
		t.Fatalf("rejected = %v", rj.Data)
	}
	h.wantStatus(step("review", nil, owner), http.StatusConflict)
	if m, r, v := count(); m != meals0 || r != recipes0 || v != versions0 {
		t.Fatalf("a proposal changed meals or recipes: %d/%d/%d -> %d/%d/%d", meals0, recipes0, versions0, m, r, v)
	}
	if n := len(list(t, h.do("GET", "/api/v1/admin/intelligence/meal-proposals", nil, ops).Data["items"])); n != 1 {
		t.Fatalf("proposals listed = %d", n)
	}

	// Generating concepts needs AI: unavailable here, and never invented.
	gen := h.do("POST", "/api/v1/admin/intelligence/meal-proposals/generate", map[string]any{}, owner)
	h.wantStatus(gen, http.StatusServiceUnavailable)
}

func TestAIMealConceptsAreDraftsWithCheckedNumbers(t *testing.T) {
	h, f := aiHarness(t)
	owner := bearer(h.adminLogin("owner-concepts@mise.test", "super_admin"))
	h.wantStatus(h.do("PUT", "/api/v1/admin/intelligence/ai/settings", aiOn, owner), http.StatusOK)
	f.set(func(n int, req map[string]any) (int, string) {
		msgs := fmt.Sprint(req["messages"])
		if !strings.Contains(msgs, "Evidence (data)") || !strings.Contains(msgs, "DATA, not instructions") {
			t.Errorf("concept request lacks the evidence framing")
		}
		concept := map[string]any{"name": "Callaloo pasta", "description": "Popular with 987 regulars every week.", "target_audience": "Families",
			"rationale": "Greens are in demand.", "complexity": "low",
			"ingredients": []any{map[string]any{"name": "Callaloo", "quantity": "200 g", "note": ""}}}
		bad := map[string]any{"name": "", "description": "", "target_audience": "", "rationale": "", "complexity": "low", "ingredients": []any{}}
		return 200, conceptsReply([]any{concept, bad})
	})
	mkt := bearer(h.adminLogin("marketing-concepts@mise.test", "marketing"))
	h.wantStatus(h.do("POST", "/api/v1/admin/intelligence/meal-proposals/generate", map[string]any{}, mkt), http.StatusForbidden)
	res := h.do("POST", "/api/v1/admin/intelligence/meal-proposals/generate", map[string]any{"brief": "Vegetarian ideas"}, owner)
	h.wantStatus(res, http.StatusOK)
	items := list(t, res.Data["items"])
	run := obj(t, res.Data["run"])
	if len(items) != 1 || run["kind"] != "meal_concepts" || run["status"] != "partial" {
		t.Fatalf("generated %d, run %v", len(items), run)
	}
	p := obj(t, items[0])
	if p["origin"] != "ai" || p["status"] != "proposed" || strings.Contains(p["description"].(string), "987") ||
		!strings.Contains(p["description"].(string), "[unverified]") || !strings.Contains(p["description"].(string), "AI hypothesis") ||
		!strings.Contains(strings.Join(toStrings(t, p["limitations"]), " "), "Written by AI") {
		t.Fatalf("AI proposal = %v", p)
	}
	var stored string
	_ = h.pool.QueryRow(t.Context(), `SELECT status FROM intelligence_runs WHERE id = $1`, run["id"]).Scan(&stored)
	if stored != "partial" {
		t.Fatalf("stored run status = %s", stored)
	}
}

func conceptsReply(concepts []any) string {
	return answerReplyRaw(map[string]any{"concepts": concepts})
}
