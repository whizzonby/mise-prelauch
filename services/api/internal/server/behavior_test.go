package server_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"mise.tt/api/internal/payments/testpay"
)

// Sprint 9.2: customer behaviour events (contract v1), funnel, abandonment,
// cancellation reasons and rule-based segments.

var eventSeq int

func evID() string {
	eventSeq++
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", eventSeq)
}

func ev(name string, props map[string]any) map[string]any {
	return map[string]any{"event_id": evID(), "name": name, "version": 1, "properties": props}
}

func header(k, v string) reqOpt { return func(r *http.Request) { r.Header.Set(k, v) } }

func TestBehaviorEventsFollowTheContract(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	anon := map[string]any{"anonymous_id": "anon_visitor_1", "session_id": "sess_visitor_1"}
	batch := func(base map[string]any, events ...map[string]any) map[string]any {
		b := map[string]any{"events": events}
		for k, v := range base {
			b[k] = v
		}
		return b
	}
	count := func() int {
		var n int
		if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM behavior_events`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// Refused: unknown events, properties outside the contract, values that are not identifiers, old versions, no actor.
	bad := []map[string]any{
		batch(anon, ev("page_scrolled", nil)),
		batch(anon, ev("meal_viewed", map[string]any{"meal": "jerk-chicken", "email": "a@b.c"})),
		batch(anon, ev("meal_viewed", map[string]any{"meal": "Someone typed this"})),
		batch(anon, ev("meal_viewed", map[string]any{})),
		batch(anon, ev("meal_selected", map[string]any{"meal": "jerk-chicken", "week": "2026-02-30"})),
		batch(anon, ev("checkout_started", map[string]any{"kind": "gift"})),
		batch(anon, map[string]any{"event_id": evID(), "name": "plan_viewed", "version": 2, "properties": map[string]any{"plan": "basic"}}),
		batch(anon, map[string]any{"event_id": "not-a-uuid", "name": "plan_viewed", "version": 1, "properties": map[string]any{"plan": "basic"}}),
		batch(nil, ev("plan_viewed", map[string]any{"plan": "basic"})),
	}
	for i, b := range bad {
		h.wantStatus(h.do("POST", "/api/v1/behavior/events", b), http.StatusUnprocessableEntity)
		if count() != 0 {
			t.Fatalf("bad batch %d stored something", i)
		}
	}

	// Accepted, and a retried event is stored once.
	view := ev("plan_viewed", map[string]any{"plan": "basic"})
	res := h.do("POST", "/api/v1/behavior/events", batch(anon, ev("catalogue_viewed", map[string]any{"filters": 2}), view))
	h.wantStatus(res, http.StatusAccepted)
	res = h.do("POST", "/api/v1/behavior/events", batch(anon, view))
	if res.Data["accepted"].(float64) != 0 || count() != 2 {
		t.Fatalf("retry stored again: accepted %v, rows %d", res.Data["accepted"], count())
	}

	// Global Privacy Control / Do Not Track: nothing is stored.
	for _, sig := range []reqOpt{header("Sec-GPC", "1"), header("DNT", "1")} {
		h.wantStatus(h.do("POST", "/api/v1/behavior/events", batch(anon, ev("plan_viewed", map[string]any{"plan": "basic"})), sig), http.StatusAccepted)
	}
	if count() != 2 {
		t.Fatalf("events stored despite a privacy signal: %d", count())
	}

	// A signed-in customer's events are attributed; a bad token is refused, not stored anonymously.
	cust := h.customer("behavior-customer@example.com")
	h.wantStatus(h.do("POST", "/api/v1/behavior/events", batch(nil, ev("meal_viewed", map[string]any{"meal": "jerk-chicken"})), bearer(cust)), http.StatusAccepted)
	h.wantStatus(h.do("POST", "/api/v1/behavior/events", batch(anon, ev("meal_viewed", map[string]any{"meal": "jerk-chicken"})), bearer("forged")), http.StatusUnauthorized)
	var attributed int
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM behavior_events WHERE customer_id IS NOT NULL`).Scan(&attributed); err != nil || attributed != 1 {
		t.Fatalf("attributed = %d (%v)", attributed, err)
	}
	if count() != 3 {
		t.Fatalf("rows = %d", count())
	}
}

func TestCustomerIntelligenceFunnelAndSegments(t *testing.T) {
	s := newShop(t)
	h := s.h
	ctx := context.Background()
	from, today := "2026-11-01", "2026-11-30"

	// Two visitors browse; one customer starts checkout and pays; another starts checkout and leaves.
	for _, a := range []string{"anon_funnel_1", "anon_funnel_2"} {
		h.wantStatus(h.do("POST", "/api/v1/behavior/events", map[string]any{"anonymous_id": a, "events": []map[string]any{
			ev("catalogue_viewed", nil), ev("plan_viewed", map[string]any{"plan": "basic"})}}), http.StatusAccepted)
	}
	payer, _ := s.customer("payer@example.com")
	h.wantStatus(h.do("POST", "/api/v1/behavior/events", map[string]any{"events": []map[string]any{
		ev("plan_selected", map[string]any{"plan": "basic"}), ev("checkout_started", map[string]any{"kind": "subscription", "plan": "basic"})}}, payer), http.StatusAccepted)
	paid, _ := s.active("payer-2@example.com", testpay.CardApprove)
	_ = paid
	leaver := h.customer("leaver@example.com")
	h.wantStatus(h.do("POST", "/api/v1/behavior/events", map[string]any{"events": []map[string]any{
		ev("checkout_started", map[string]any{"kind": "subscription"})}}, bearer(leaver)), http.StatusAccepted)
	// Backdate the leaver's checkout so the 24-hour rule can judge it.
	if _, err := h.pool.Exec(ctx, `UPDATE behavior_events SET occurred_at = occurred_at - interval '2 days' WHERE name = 'checkout_started'
		AND customer_id = (SELECT c.id FROM customers c JOIN users u ON u.id = c.user_id WHERE u.email = 'leaver@example.com')`); err != nil {
		t.Fatal(err)
	}

	owner := bearer(h.adminLogin("owner@mise.test", "super_admin"))
	res := h.do("GET", "/api/v1/admin/intelligence/funnel?from="+from+"&to="+today, nil, owner)
	h.wantStatus(res, http.StatusOK)
	steps := map[string]float64{}
	for _, st := range list(t, res.Data["steps"]) {
		steps[obj(t, st)["key"].(string)] = obj(t, st)["people"].(float64)
	}
	if steps["catalogue_viewed"] != 2 || steps["plan_viewed"] != 2 || steps["plan_selected"] != 1 || steps["checkout_started"] != 2 || steps["payment_completed"] != 1 {
		t.Fatalf("funnel = %v", steps)
	}
	if res.Data["checkouts_abandoned"].(float64) != 1 || res.Data["checkouts_pending"].(float64) != 1 || res.Data["includes_test_payments"] != true {
		t.Fatalf("abandonment = %v / pending %v / test %v", res.Data["checkouts_abandoned"], res.Data["checkouts_pending"], res.Data["includes_test_payments"])
	}

	// Cancellation reasons are the customer's own words; a missing reason is not inferred.
	if _, err := h.pool.Exec(ctx, `UPDATE subscriptions SET cancel_requested_at = now(), cancel_reason = NULL`); err != nil {
		t.Fatal(err)
	}
	res = h.do("GET", "/api/v1/admin/intelligence/cancellation-reasons?from="+from+"&to="+today, nil, owner)
	h.wantStatus(res, http.StatusOK)
	if r := list(t, res.Data["items"]); len(r) != 1 || obj(t, r[0])["reason"] != "not recorded" {
		t.Fatalf("reasons = %v", r)
	}

	// Access: finance and support have no customer intelligence; customers and guests are refused.
	for _, role := range []string{"finance", "support"} {
		tok := bearer(h.adminLogin(role+"-ci@mise.test", role))
		h.wantStatus(h.do("GET", "/api/v1/admin/intelligence/segments", nil, tok), http.StatusForbidden)
	}
	h.wantStatus(h.do("GET", "/api/v1/admin/intelligence/funnel?from="+from+"&to="+today, nil, bearer(leaver)), http.StatusUnauthorized)
	h.wantStatus(h.do("GET", "/api/v1/admin/intelligence/segments", nil), http.StatusUnauthorized)
	mkt := bearer(h.adminLogin("mkt-ci@mise.test", "marketing"))
	h.wantStatus(h.do("GET", "/api/v1/admin/intelligence/funnel?from="+from+"&to="+today, nil, mkt), http.StatusOK)
}

func TestCustomerSegmentsAreRuleBased(t *testing.T) {
	w := newKitchenWeek(t) // Ana and Ben: one paid week each (week of 16 Nov)
	h := w.h
	owner := bearer(h.adminLogin("owner@mise.test", "super_admin"))
	res := h.do("GET", "/api/v1/admin/intelligence/segments", nil, owner)
	h.wantStatus(res, http.StatusOK)
	seg := map[string]map[string]any{}
	for _, x := range list(t, res.Data["items"]) {
		seg[obj(t, x)["key"].(string)] = obj(t, x)
	}
	if seg["new"]["count"].(float64) != 2 || seg["repeat"]["count"].(float64) != 0 || seg["inactive"]["count"].(float64) != 0 || seg["new"]["definition"] == "" {
		t.Fatalf("segments = %v", seg)
	}
	// Category preference: each Basic week is at least 3 kits, so a preference can be shown, always with its rule.
	for k, v := range seg {
		if len(k) > 8 && k[:8] == "prefers_" {
			if c := v["count"].(float64); c < 1 || c > 2 || !strings.Contains(v["definition"].(string), "half or more") {
				t.Fatalf("preference segment %s = %v", k, v)
			}
		}
	}
	res = h.do("GET", "/api/v1/admin/intelligence/segments/new/members", nil, owner)
	h.wantStatus(res, http.StatusOK)
	emails := map[string]bool{}
	for _, m := range list(t, res.Data["items"]) {
		emails[obj(t, m)["email"].(string)] = true
	}
	if !emails["ana@example.com"] || !emails["ben@example.com"] || len(emails) != 2 {
		t.Fatalf("members = %v", emails)
	}
	h.wantStatus(h.do("GET", "/api/v1/admin/intelligence/segments/nope/members", nil, owner), http.StatusNotFound)

	// Marketing sees the counts; names need customers.read as well.
	mkt := bearer(h.adminLogin("mkt-seg@mise.test", "marketing"))
	h.wantStatus(h.do("GET", "/api/v1/admin/intelligence/segments", nil, mkt), http.StatusOK)
	var canRead bool
	if err := h.pool.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM role_permissions WHERE role_key = 'marketing' AND permission_key = 'customers.read')`).Scan(&canRead); err != nil {
		t.Fatal(err)
	}
	want := http.StatusForbidden
	if canRead {
		want = http.StatusOK
	}
	h.wantStatus(h.do("GET", "/api/v1/admin/intelligence/segments/new/members", nil, mkt), want)
	// Operations has no customer intelligence at all.
	h.wantStatus(h.do("GET", "/api/v1/admin/intelligence/segments", nil, bearer(h.adminLogin("ops-seg@mise.test", "operations"))), http.StatusForbidden)
}
