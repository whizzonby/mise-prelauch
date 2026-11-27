package server_test

import (
	"net/http"
	"sort"
	"strings"
	"testing"

	"mise.tt/api/internal/payments/testpay"
)

// Sprint 9.2: Mise Intelligence tools, evidence and readiness.

func toolNames(t *testing.T, h *harness, tok reqOpt) []string {
	t.Helper()
	res := h.do("GET", "/api/v1/admin/intelligence/tools", nil, tok)
	h.wantStatus(res, http.StatusOK)
	var names []string
	for _, x := range list(t, res.Data["items"]) {
		names = append(names, obj(t, x)["name"].(string))
	}
	sort.Strings(names)
	return names
}

func runTool(h *harness, name string, args map[string]any, tok ...reqOpt) response {
	return h.do("POST", "/api/v1/admin/intelligence/tools/"+name, map[string]any{"args": args}, tok...)
}

func factOf(t *testing.T, res map[string]any, key string) (float64, bool) {
	t.Helper()
	for _, f := range list(t, res["facts"]) {
		if obj(t, f)["key"] == key {
			v, ok := obj(t, f)["value"].(float64)
			return v, ok
		}
	}
	t.Fatalf("no fact %s", key)
	return 0, false
}

func TestIntelligenceToolsFollowPermissions(t *testing.T) {
	h := newHarness(t)
	want := map[string]string{
		"finance":    "business_risks,data_readiness,expense_breakdown,financial_summary,knowledge_search,revenue_and_subscriptions,run_financial_scenario",
		"operations": "business_risks,data_readiness,delivery_metrics,knowledge_search,meal_intelligence,meal_performance,production_metrics,support_trends",
		"marketing":  "business_risks,cancellation_reasons,conversion_funnel,customer_segments,data_readiness,knowledge_search",
	}
	for role, names := range want {
		if got := strings.Join(toolNames(t, h, bearer(h.adminLogin(role+"-tools@mise.test", role))), ","); got != names {
			t.Errorf("%s tools = %s, want %s", role, got, names)
		}
	}
	owner := bearer(h.adminLogin("owner-tools@mise.test", "super_admin"))
	if n := len(toolNames(t, h, owner)); n != 20 {
		t.Errorf("super admin sees %d tools, want 20", n)
	}

	// Staff without intelligence, customers and guests are refused.
	support := bearer(h.adminLogin("support-tools@mise.test", "support"))
	h.wantStatus(h.do("GET", "/api/v1/admin/intelligence/tools", nil, support), http.StatusForbidden)
	h.wantStatus(runTool(h, "data_readiness", nil, support), http.StatusForbidden)
	h.wantStatus(h.do("GET", "/api/v1/admin/intelligence/readiness", nil, support), http.StatusForbidden)
	cust := bearer(h.customer("intel-curious@example.com"))
	h.wantStatus(runTool(h, "business_overview", nil, cust), http.StatusUnauthorized)
	h.wantStatus(runTool(h, "business_overview", nil), http.StatusUnauthorized)

	// A tool outside the caller's domains is refused even when called directly.
	fin := bearer(h.adminLogin("finance-tools2@mise.test", "finance"))
	h.wantStatus(runTool(h, "delivery_metrics", nil, fin), http.StatusForbidden)
	h.wantStatus(runTool(h, "customer_segments", nil, fin), http.StatusForbidden)
	h.wantStatus(runTool(h, "no_such_tool", nil, fin), http.StatusNotFound)
	// Arguments outside the tool's contract are refused.
	h.wantStatus(runTool(h, "financial_summary", map[string]any{"sql": "select 1"}, fin), http.StatusUnprocessableEntity)
	h.wantStatus(runTool(h, "financial_summary", map[string]any{"from": "2026-11-30", "to": "2026-11-01"}, fin), http.StatusUnprocessableEntity)
	h.wantStatus(runTool(h, "knowledge_search", map[string]any{}, fin), http.StatusUnprocessableEntity)
}

func TestIntelligenceToolsReturnTheAuthoritativeNumbers(t *testing.T) {
	w := newKitchenWeek(t)
	h := w.h
	owner := bearer(h.adminLogin("owner-intel@mise.test", "super_admin"))
	period := map[string]any{"from": "2026-11-01", "to": "2026-11-30"}

	// Every tool runs for a super admin and returns evidence.
	for _, name := range toolNames(t, h, owner) {
		args := period
		switch name {
		case "customer_segments", "demand_forecast", "data_readiness", "run_financial_scenario", "business_risks":
			args = nil
		case "knowledge_search":
			args = map[string]any{"query": "collected payments"}
		}
		res := runTool(h, name, args, owner)
		h.wantStatus(res, http.StatusOK)
		ev := obj(t, res.Data["evidence"])
		if ev["tool"] != name || ev["ref"] != "direct" || ev["domain"] == "" || ev["freshness"] == "" || res.Data["facts"] == nil || res.Data["limitations"] == nil {
			t.Fatalf("%s evidence = %v", name, ev)
		}
	}

	// The overview's numbers are the analytics dashboard's numbers.
	dash := h.do("GET", "/api/v1/admin/analytics/overview?from=2026-11-01&to=2026-11-30", nil, owner)
	h.wantStatus(dash, http.StatusOK)
	collected, _ := metricOf(t, dash.Data, "revenue.collected")
	ov := runTool(h, "business_overview", period, owner)
	if v, ok := factOf(t, ov.Data, "revenue.collected"); !ok || v != collected {
		t.Fatalf("overview revenue.collected = %v, dashboard %v", v, collected)
	}

	// The financial summary: test payments are not revenue, missing expenses withhold results.
	fs := runTool(h, "financial_summary", period, owner)
	h.wantStatus(fs, http.StatusOK)
	if v, _ := factOf(t, fs.Data, "finance.real_payments"); v != 0 {
		t.Fatalf("real payments = %v", v)
	}
	if v, _ := factOf(t, fs.Data, "finance.test_payments"); v < 2 {
		t.Fatalf("test payments = %v", v)
	}
	if v, _ := factOf(t, fs.Data, "finance.gross_collections"); v != 0 {
		t.Fatalf("test payments counted as collections: %v", v)
	}
	if _, ok := factOf(t, fs.Data, "finance.estimated_operating_result"); ok {
		t.Fatal("operating result given although expenses are missing")
	}
	lim := strings.Join(toStrings(t, fs.Data["limitations"]), " ")
	if !strings.Contains(lim, "test provider") || !strings.Contains(lim, "withheld") {
		t.Fatalf("limitations = %s", lim)
	}

	// Readiness says what cannot be relied on.
	rd := h.do("GET", "/api/v1/admin/intelligence/readiness", nil, owner)
	h.wantStatus(rd, http.StatusOK)
	status := map[string]string{}
	for _, c := range list(t, rd.Data["checks"]) {
		status[obj(t, c)["key"].(string)] = obj(t, c)["status"].(string)
	}
	if status["payments"] != "not_ready" || status["expenses"] != "not_ready" || status["behavior"] != "not_ready" || status["ai"] != "not_ready" ||
		rd.Data["overall"] != "not_ready" || obj(t, rd.Data["ai"])["available"] != false {
		t.Fatalf("readiness = %v (overall %v)", status, rd.Data["overall"])
	}

	// Knowledge search finds metric definitions.
	ks := runTool(h, "knowledge_search", map[string]any{"query": "collected"}, owner)
	if !strings.Contains(strings.Join(flatten(t, ks.Data["tables"]), " "), "revenue.collected") {
		t.Fatalf("knowledge search = %v", ks.Data["tables"])
	}

	// A real (non-test) payment appears as real once it exists.
	s := w.s
	s.active("real-intel@example.com", testpay.CardApprove)
	if _, err := h.pool.Exec(t.Context(), `UPDATE payments SET provider = 'stripe' WHERE customer_id = (SELECT c.id FROM customers c JOIN users u ON u.id = c.user_id WHERE u.email = 'real-intel@example.com')`); err != nil {
		t.Fatal(err)
	}
	fs = runTool(h, "financial_summary", period, owner)
	if v, _ := factOf(t, fs.Data, "finance.real_payments"); v != 1 {
		t.Fatalf("real payments after a real one = %v", v)
	}
}

func toStrings(t *testing.T, v any) []string {
	var out []string
	for _, x := range list(t, v) {
		out = append(out, x.(string))
	}
	return out
}

func flatten(t *testing.T, tables any) []string {
	var out []string
	for _, tb := range list(t, tables) {
		for _, row := range list(t, obj(t, tb)["rows"]) {
			out = append(out, toStrings(t, row)...)
		}
	}
	return out
}
