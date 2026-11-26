package server_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"mise.tt/api/internal/finance"
	"mise.tt/api/internal/payments/testpay"
)

// Sprint 9.2: management finance. Expenses with approval and audit,
// recurring costs, coverage, cash balances and the deterministic summary.

func monthPeriod() (from, to, month string) {
	now := time.Now().In(mustLocation("America/Port_of_Spain"))
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1)
	return first.Format("2006-01-02"), last.Format("2006-01-02"), first.Format("2006-01")
}

func mustLocation(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}

func summaryOf(t *testing.T, h *harness, tok reqOpt, from, to string) map[string]any {
	t.Helper()
	res := h.do("GET", "/api/v1/admin/finance/summary?from="+from+"&to="+to, nil, tok)
	h.wantStatus(res, http.StatusOK)
	return res.Data
}

func resultOf(t *testing.T, s map[string]any, key string) map[string]any {
	t.Helper()
	for _, r := range list(t, s["results"]) {
		if obj(t, r)["key"] == key {
			return obj(t, r)
		}
	}
	t.Fatalf("no result %s", key)
	return nil
}

func lineOf(t *testing.T, s map[string]any, key string) map[string]any {
	t.Helper()
	for _, l := range list(t, s["lines"]) {
		if obj(t, l)["key"] == key {
			return obj(t, l)
		}
	}
	t.Fatalf("no line %s", key)
	return nil
}

func categoryOf(t *testing.T, s map[string]any, key string) map[string]any {
	t.Helper()
	for _, c := range list(t, s["categories"]) {
		if obj(t, c)["key"] == key {
			return obj(t, c)
		}
	}
	t.Fatalf("no category %s", key)
	return nil
}

func TestManagementFinanceSummaryIsHonestAboutCoverage(t *testing.T) {
	s := newShop(t)
	h := s.h
	ctx := context.Background()
	owner := bearer(h.adminLogin("owner@mise.test", "super_admin"))
	fin := bearer(h.adminLogin("fin@mise.test", "finance"))
	from, to, month := monthPeriod()

	// Nothing recorded: collections are known; every expense category is missing, so no result is claimed.
	sum := summaryOf(t, h, owner, from, to)
	if !strings.Contains(sum["statement"].(string), "Not audited") {
		t.Fatalf("statement = %v", sum["statement"])
	}
	for _, key := range []string{"contribution_margin", "estimated_operating_result", "recognised_revenue"} {
		if r := resultOf(t, sum, key); r["withheld"] != true || r["amount_minor"] != nil {
			t.Fatalf("%s should be withheld: %v", key, r)
		}
	}
	if c := categoryOf(t, sum, "kitchen_rent"); c["coverage"] != finance.Missing {
		t.Fatalf("rent coverage = %v", c["coverage"])
	}
	if cash := obj(t, sum["cash"]); cash["complete"] != false {
		t.Fatalf("cash = %v", cash)
	}

	// Test-provider payments never count as real collections.
	s.active("finance-test-customer@example.com", testpay.CardApprove)
	sum = summaryOf(t, h, owner, from, to)
	if lineOf(t, sum, "gross_collections")["amount_minor"].(float64) != 0 || sum["test_payments"].(float64) != 1 || sum["test_collections_minor"].(float64) != 33000 {
		t.Fatalf("test payment leaked into real collections: gross %v, test %v/%v", lineOf(t, sum, "gross_collections")["amount_minor"], sum["test_payments"], sum["test_collections_minor"])
	}

	// A real payment (here: a payment row marked as another provider) with and without a reported fee.
	if _, err := h.pool.Exec(ctx, `UPDATE payments SET provider = 'stripe' WHERE customer_id = (SELECT c.id FROM customers c JOIN users u ON u.id = c.user_id
		WHERE u.email = 'finance-test-customer@example.com')`); err != nil {
		t.Fatal(err)
	}
	sum = summaryOf(t, h, owner, from, to)
	if lineOf(t, sum, "gross_collections")["amount_minor"].(float64) != 33000 || lineOf(t, sum, "provider_fees")["coverage"] != finance.Partial {
		t.Fatalf("real payment: gross %v fees %v", lineOf(t, sum, "gross_collections"), lineOf(t, sum, "provider_fees"))
	}
	if !strings.Contains(fmt.Sprint(resultOf(t, sum, "contribution_margin")["reasons"]), "Fees reported for 0 of 1") {
		t.Fatalf("contribution reasons = %v", resultOf(t, sum, "contribution_margin")["reasons"])
	}
	if cash := obj(t, sum["cash"]); cash["inflow_basis"] != "collections" {
		t.Fatalf("collections must not be presented as settled cash: %v", cash)
	}
	if _, err := h.pool.Exec(ctx, `UPDATE payments SET fee_minor = 990, fee_currency = 'TTD', settlement_amount_minor = 32010, settlement_currency = 'TTD'
		WHERE provider = 'stripe'`); err != nil {
		t.Fatal(err)
	}

	// Expenses: one per category this month, approved by someone other than the recorder.
	amounts := map[string]int64{"salaries_wages": 1200000, "kitchen_rent": 500000, "utilities": 80000, "packaging": 12000, "marketing": 60000,
		"insurance": 30000, "equipment_maintenance": 0, "transportation": 5000, "payment_processing_fees": 2500, "professional_services": 25000}
	date := from[:8] + "05"
	for cat, amount := range amounts {
		res := h.do("POST", "/api/v1/admin/finance/expenses", map[string]any{"category": cat, "description": "Test " + cat, "amount_minor": amount,
			"kind": "actual", "expense_date": date, "payment_date": date, "reference": "INV-" + cat}, fin)
		h.wantStatus(res, http.StatusCreated)
		id := res.Data["id"].(string)
		h.wantStatus(h.do("POST", "/api/v1/admin/finance/expenses/"+id+"/submit", nil, fin), http.StatusOK)
		// The finance user cannot approve their own expense.
		h.wantError(h.do("POST", "/api/v1/admin/finance/expenses/"+id+"/approve", nil, fin), http.StatusConflict, "own_expense")
		h.wantStatus(h.do("POST", "/api/v1/admin/finance/expenses/"+id+"/approve", nil, owner), http.StatusOK)
	}
	// "Other operating expenses": nothing this month, confirmed explicitly (the only way missing becomes zero).
	sum = summaryOf(t, h, owner, from, to)
	if r := resultOf(t, sum, "estimated_operating_result"); r["withheld"] != true || !strings.Contains(fmt.Sprint(r["reasons"]), "Other operating expenses") {
		t.Fatalf("before confirmation: %v", r)
	}
	h.wantStatus(h.do("PUT", "/api/v1/admin/finance/coverage", map[string]any{"category": "other_operating", "month": month, "confirmed": true, "note": "none this month"}, fin), http.StatusOK)

	// Exact, deterministic results (no inventory or delivery activity in this test).
	sum = summaryOf(t, h, owner, from, to)
	net := int64(33000)
	contribution := net - 990 - 12000 - 5000 - 2500
	if r := resultOf(t, sum, "contribution_margin"); r["withheld"] != false || int64(r["amount_minor"].(float64)) != contribution {
		t.Fatalf("contribution = %v, want %d", r, contribution)
	}
	fixed := int64(1200000 + 500000 + 80000 + 60000 + 30000 + 0 + 25000)
	if r := resultOf(t, sum, "estimated_operating_result"); r["withheld"] != false || int64(r["amount_minor"].(float64)) != contribution-fixed {
		t.Fatalf("operating result = %v, want %d", r, contribution-fixed)
	}
	if r := resultOf(t, sum, "estimated_gross_margin"); int64(r["amount_minor"].(float64)) != net-12000 {
		t.Fatalf("gross margin = %v", r)
	}
	again := summaryOf(t, h, owner, from, to)
	if fmt.Sprint(again["results"]) != fmt.Sprint(sum["results"]) {
		t.Fatal("the same period computed twice gave different results")
	}
	cash := obj(t, sum["cash"])
	if cash["inflow_basis"] != "settlement" || int64(cash["inflows_minor"].(float64)) != 32010 {
		t.Fatalf("settled inflows = %v", cash)
	}
	var paid int64
	for _, a := range amounts {
		paid += a
	}
	if int64(cash["outflows_minor"].(float64)) != paid || cash["recorded_balance_minor"] != nil {
		t.Fatalf("cash outflows = %v, want %d", cash["outflows_minor"], paid)
	}

	// A cash balance needs the approve permission and a reference; then it is shown, never derived from collections.
	h.wantStatus(h.do("POST", "/api/v1/admin/finance/cash-balances", map[string]any{"as_of": from, "account_label": "Operating", "amount_minor": 2500000, "reference": "Bank statement 1"}, bearer(h.adminLogin("ops-admin@mise.test", "admin"))), http.StatusForbidden)
	h.wantStatus(h.do("POST", "/api/v1/admin/finance/cash-balances", map[string]any{"as_of": from, "account_label": "Operating", "amount_minor": 2500000, "reference": "Bank statement 1"}, fin), http.StatusCreated)
	h.wantStatus(h.do("POST", "/api/v1/admin/finance/cash-balances", map[string]any{"as_of": from, "account_label": "Operating", "amount_minor": 1, "reference": "again"}, fin), http.StatusConflict)
	cash = obj(t, summaryOf(t, h, owner, from, to)["cash"])
	if int64(cash["recorded_balance_minor"].(float64)) != 2500000 {
		t.Fatalf("balance = %v", cash)
	}

	// Another currency is excluded and counted, never converted.
	res := h.do("POST", "/api/v1/admin/finance/expenses", map[string]any{"category": "marketing", "description": "US ad spend", "amount_minor": 10000,
		"currency": "USD", "kind": "actual", "expense_date": date}, fin)
	h.wantStatus(res, http.StatusCreated)
	usd := res.Data["id"].(string)
	h.wantStatus(h.do("POST", "/api/v1/admin/finance/expenses/"+usd+"/submit", nil, fin), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/finance/expenses/"+usd+"/approve", nil, owner), http.StatusOK)
	sum = summaryOf(t, h, owner, from, to)
	if sum["excluded_other_currency_records"].(float64) != 1 || int64(categoryOf(t, sum, "marketing")["actual_minor"].(float64)) != 60000 {
		t.Fatalf("USD expense: excluded %v, marketing %v", sum["excluded_other_currency_records"], categoryOf(t, sum, "marketing")["actual_minor"])
	}

	// Workflow guards and an immutable history.
	h.wantError(h.do("PATCH", "/api/v1/admin/finance/expenses/"+usd, map[string]any{"category": "marketing", "description": "x", "amount_minor": 1,
		"kind": "actual", "expense_date": date}, fin), http.StatusConflict, "not_editable")
	h.wantStatus(h.do("POST", "/api/v1/admin/finance/expenses/"+usd+"/void", nil, fin), http.StatusUnprocessableEntity) // a note is required
	h.wantStatus(h.do("POST", "/api/v1/admin/finance/expenses/"+usd+"/void", map[string]any{"note": "entered in error"}, fin), http.StatusOK)
	detail := h.do("GET", "/api/v1/admin/finance/expenses/"+usd, nil, fin)
	var actions []string
	for _, e := range list(t, detail.Data["history"]) {
		actions = append(actions, obj(t, e)["action"].(string))
	}
	if strings.Join(actions, ",") != "created,submit,approve,void" {
		t.Fatalf("history = %v", actions)
	}
	if _, err := h.pool.Exec(ctx, `UPDATE finance_expense_events SET note = 'x'`); err == nil {
		t.Fatal("expense history can be edited")
	}

	// Permissions: support staff and customers cannot read finance.
	support := bearer(h.adminLogin("support@mise.test", "support"))
	h.wantStatus(h.do("GET", "/api/v1/admin/finance/summary?from="+from+"&to="+to, nil, support), http.StatusForbidden)
	h.wantStatus(h.do("GET", "/api/v1/admin/finance/expenses", nil, support), http.StatusForbidden)
	cust := bearer(h.customer("finance-curious@example.com"))
	h.wantStatus(h.do("GET", "/api/v1/admin/finance/summary?from="+from+"&to="+to, nil, cust), http.StatusUnauthorized)
}

func TestRecurringCostsAreEstimatesUntilRecordedAndNeverCountedTwice(t *testing.T) {
	h := newHarness(t)
	owner := bearer(h.adminLogin("owner@mise.test", "super_admin"))
	from, to, _ := monthPeriod()
	start := from[:8] + "01"
	res := h.do("POST", "/api/v1/admin/finance/recurring-expenses", map[string]any{"category": "kitchen_rent", "description": "Kitchen lease",
		"amount_minor": 500000, "cadence": "monthly", "starts_on": start, "reference": "Lease 2026"}, owner)
	h.wantStatus(res, http.StatusCreated)
	id := res.Data["id"].(string)
	// A draft schedule does not count.
	if c := categoryOf(t, summaryOf(t, h, owner, from, to), "kitchen_rent"); c["coverage"] != finance.Missing {
		t.Fatalf("draft schedule counted: %v", c)
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/finance/recurring-expenses/"+id+"/approve", nil, owner), http.StatusOK)
	c := categoryOf(t, summaryOf(t, h, owner, from, to), "kitchen_rent")
	if c["coverage"] != finance.Estimated || int64(c["estimated_minor"].(float64)) != 500000 || int64(c["actual_minor"].(float64)) != 0 {
		t.Fatalf("approved schedule = %v", c)
	}
	// Recording the actual for that occurrence replaces the estimate (not added to it).
	res = h.do("POST", "/api/v1/admin/finance/expenses", map[string]any{"category": "kitchen_rent", "description": "Rent", "amount_minor": 510000,
		"kind": "actual", "expense_date": start, "recurring_id": id}, owner)
	h.wantStatus(res, http.StatusCreated)
	eid := res.Data["id"].(string)
	h.wantStatus(h.do("POST", "/api/v1/admin/finance/expenses/"+eid+"/submit", nil, owner), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/finance/expenses/"+eid+"/approve", nil, owner), http.StatusOK) // super admin may approve their own
	c = categoryOf(t, summaryOf(t, h, owner, from, to), "kitchen_rent")
	if c["coverage"] != finance.Complete || int64(c["actual_minor"].(float64)) != 510000 || int64(c["estimated_minor"].(float64)) != 0 {
		t.Fatalf("after recording the actual = %v", c)
	}
	// The same occurrence cannot be recorded twice.
	h.wantStatus(h.do("POST", "/api/v1/admin/finance/expenses", map[string]any{"category": "kitchen_rent", "description": "Rent again", "amount_minor": 510000,
		"kind": "actual", "expense_date": start, "recurring_id": id}, owner), http.StatusUnprocessableEntity)
}

func TestRecurringOccurrencesClampToMonthEnd(t *testing.T) {
	utc := time.UTC
	d := func(s string) time.Time { v, _ := time.ParseInLocation("2006-01-02", s, utc); return v }
	got := finance.Occurrences("monthly", d("2026-01-31"), nil, d("2026-01-01"), d("2026-04-30"))
	var s []string
	for _, x := range got {
		s = append(s, x.Format("2006-01-02"))
	}
	if strings.Join(s, ",") != "2026-01-31,2026-02-28,2026-03-31,2026-04-30" {
		t.Fatalf("monthly = %v", s)
	}
	end := d("2026-02-15")
	if n := len(finance.Occurrences("weekly", d("2026-02-01"), &end, d("2026-01-01"), d("2026-03-31"))); n != 3 {
		t.Fatalf("weekly with an end date = %d occurrences, want 3", n)
	}
	if n := len(finance.Occurrences("quarterly", d("2025-11-15"), nil, d("2026-01-01"), d("2026-12-31"))); n != 4 {
		t.Fatalf("quarterly in 2026 = %d, want 4", n)
	}
}
