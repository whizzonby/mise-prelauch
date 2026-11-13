package server_test

// Sprint 4 verification debt (Sprint 5 brief §43): staff subscription
// cancellation, staff "sign out everywhere" and the retention signals.

import (
	"net/http"
	"testing"
	"time"

	"mise.tt/api/internal/payments/testpay"
)

func TestStaffCancelsASubscriptionWithAReason(t *testing.T) {
	s := newShop(t)
	h := s.h
	tok, sub := s.active("leaving@example.com", testpay.CardApprove)
	s.tick("2026-11-13 00:30") // week of 16 Nov finalised; week of 23 Nov exists, unpaid
	support := bearer(h.adminLogin("help-cancel@mise.test", "support"))
	ops := bearer(h.adminLogin("ops-cancel@mise.test", "operations"))
	path := "/api/v1/admin/subscriptions/" + sub + "/cancel"

	// Permission, then a reason code, are required.
	h.wantStatus(h.do("POST", path, map[string]any{"reason": "customer_request"}, ops), http.StatusForbidden)
	h.wantStatus(h.do("POST", path, map[string]any{"cancel_reason": "moving"}, support), http.StatusUnprocessableEntity)
	h.wantStatus(h.do("POST", path, map[string]any{"reason": "because"}, support), http.StatusUnprocessableEntity)
	if n := h.count(`SELECT count(*) FROM subscriptions WHERE id = $1 AND status = 'active'`, sub); n != 1 {
		t.Fatal("a refused request changed the subscription")
	}

	// The same rule as the customer's own cancel: no paid week remains, so it ends now.
	res := h.do("POST", path, map[string]any{"reason": "customer_request", "note": "Called in", "cancel_reason": "moving"}, support)
	h.wantStatus(res, http.StatusOK)
	if st := obj(t, res.Data["subscription"])["status"]; st != "cancelled" {
		t.Fatalf("status = %v", st)
	}
	if s.subscription(tok) != nil {
		t.Error("the customer still sees a current subscription")
	}
	if n := h.count(`SELECT count(*) FROM subscriptions WHERE id = $1 AND cancel_reason = 'moving'`, sub); n != 1 {
		t.Error("cancel reason not stored")
	}
	if n := h.count(`SELECT count(*) FROM audit_log WHERE action LIKE 'subscription.%' AND reason = 'customer_request: Called in' AND actor_user_id = $1`,
		h.userIDOf("help-cancel@mise.test")); n == 0 {
		t.Error("staff cancel not audited with its reason and actor")
	}
	// Cancelling twice is refused, not repeated.
	h.wantStatus(h.do("POST", path, map[string]any{"reason": "customer_request"}, support), http.StatusConflict)
}

func TestStaffSignOutEverywhere(t *testing.T) {
	h := newHarness(t)
	root := bearer(h.adminLogin("root-so@mise.test", "super_admin"))
	admin := bearer(h.adminLogin("admin-so@mise.test", "admin"))
	support := bearer(h.adminLogin("support-so@mise.test", "support"))
	// Two sessions for the same staff member (two devices).
	cook1 := bearer(h.adminLogin("cook-so@mise.test", "operations"))
	login := h.do("POST", "/api/v1/admin/auth/login", map[string]string{"email": "cook-so@mise.test", "password": "correct horse battery staple"})
	h.wantStatus(login, http.StatusOK)
	cook2 := bearer(login.Data["token"].(string))
	cookID := h.userIDOf("cook-so@mise.test")
	path := "/api/v1/admin/staff/" + cookID + "/signout"

	h.wantStatus(h.do("POST", path, map[string]any{"reason": "Lost tablet"}, support), http.StatusForbidden)
	h.wantStatus(h.do("POST", path, map[string]any{}, root), http.StatusUnprocessableEntity)
	h.wantStatus(h.do("GET", "/api/v1/admin/orders", nil, cook1), http.StatusOK)

	res := h.do("POST", path, map[string]any{"reason": "Lost tablet"}, root)
	h.wantStatus(res, http.StatusOK)
	if n := res.Data["sessions_ended"]; n != float64(2) {
		t.Errorf("sessions_ended = %v, want 2", n)
	}
	for _, tok := range []reqOpt{cook1, cook2} {
		h.wantStatus(h.do("GET", "/api/v1/admin/orders", nil, tok), http.StatusUnauthorized)
	}
	if n := h.count(`SELECT count(*) FROM audit_log WHERE action = 'staff.signed_out' AND target_id = $1 AND reason = 'Lost tablet'`, cookID); n != 1 {
		t.Errorf("sign-out audit entries = %d", n)
	}
	// The account still works: signing in again is allowed.
	h.wantStatus(h.do("GET", "/api/v1/admin/orders", nil, bearer(h.adminLogin("cook-so@mise.test", "operations"))), http.StatusOK)

	// Admins do not manage staff (users.manage is super_admin only); a super
	// administrator can sign out an administrator and keeps their own session.
	adminID := h.userIDOf("admin-so@mise.test")
	h.wantError(h.do("POST", "/api/v1/admin/staff/"+h.userIDOf("root-so@mise.test")+"/signout", map[string]any{"reason": "Test"}, admin), http.StatusForbidden, "forbidden")
	h.wantStatus(h.do("POST", "/api/v1/admin/staff/"+adminID+"/signout", map[string]any{"reason": "Rotation"}, root), http.StatusOK)
	h.wantStatus(h.do("GET", "/api/v1/admin/orders", nil, admin), http.StatusUnauthorized)
	h.wantStatus(h.do("GET", "/api/v1/admin/orders", nil, root), http.StatusOK)
}

func TestRetentionSignals(t *testing.T) {
	s := newShop(t)
	h := s.h
	s.active("past-due@example.com", testpay.CardDecline)   // renewal will decline
	s.active("misses-weeks@example.com", testpay.CardApprove) // never chooses meals
	s.active("quiet@example.com", testpay.CardApprove)
	s.customer("stale-case@example.com")
	s.customer("fresh-case@example.com")
	goneTok, goneSub := s.active("gone@example.com", testpay.CardApprove)
	s.tick("2026-11-13 00:30") // week of 16 Nov closes without meals
	// No paid week remains, so this cancellation takes effect at once.
	h.wantStatus(h.do("POST", "/api/v1/me/subscriptions/"+goneSub+"/cancel", map[string]any{"reason": "too_expensive"}, goneTok), http.StatusOK)
	cancelledAt := h.clock.Now()
	s.tick("2026-11-18 00:30") // renewals: one declines
	s.tick("2026-11-20 00:30") // week of 23 Nov closes without meals
	now := h.clock.Now()

	// Facts set directly: a long-quiet subscriber and a case nobody touched.
	if _, err := h.pool.Exec(t.Context(), `UPDATE users SET last_login_at = $1 WHERE email = 'quiet@example.com'`, now.Add(-40*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	support := bearer(h.adminLogin("help-ret@mise.test", "support"))
	// Cases are stamped by the database clock; pin both to the test clock.
	for email, age := range map[string]time.Duration{"stale-case@example.com": 5 * 24 * time.Hour, "fresh-case@example.com": time.Hour} {
		res := h.do("POST", "/api/v1/admin/support/cases", map[string]any{"customer_id": h.customerIDOf(email), "subject": "Where is my box", "category": "delivery"}, support)
		h.wantStatus(res, http.StatusCreated)
		if _, err := h.pool.Exec(t.Context(), `UPDATE support_cases SET updated_at = $1 WHERE id = $2`, now.Add(-age), res.Data["id"]); err != nil {
			t.Fatal(err)
		}
	}

	h.wantStatus(h.do("GET", "/api/v1/admin/retention", nil, bearer(h.adminLogin("cook-ret@mise.test", "kitchen_staff"))), http.StatusForbidden)
	res := h.do("GET", "/api/v1/admin/retention", nil, support)
	h.wantStatus(res, http.StatusOK)
	if rules := obj(t, res.Data["rules"]); len(rules) != 5 {
		t.Errorf("rules = %v", rules)
	}
	got := map[string][]string{}
	for _, it := range list(t, res.Data["items"]) {
		row := obj(t, it)
		got[row["email"].(string)] = append(got[row["email"].(string)], row["signal"].(string))
	}
	want := map[string][]string{
		"past-due@example.com":     {"failed_renewal"},
		"misses-weeks@example.com": {"missed_selections"},
		"quiet@example.com":        {"inactive", "missed_selections"},
		"stale-case@example.com":   {"stale_case"},
	}
	for email, signals := range want {
		for _, sig := range signals {
			if !contains(got[email], sig) {
				t.Errorf("%s: want signal %s, got %v", email, sig, got[email])
			}
		}
	}
	if sigs, ok := got["fresh-case@example.com"]; ok {
		t.Errorf("fresh case flagged: %v", sigs)
	}
	if contains(got["past-due@example.com"], "missed_selections") {
		t.Error("an unpaid week was counted as a missed selection")
	}

	// Recently cancelled: listed for 30 days, then not.
	if !contains(got["gone@example.com"], "recently_cancelled") {
		t.Errorf("recent cancellation not listed: %v", got["gone@example.com"])
	}
	h.clock.Set(cancelledAt.Add(31 * 24 * time.Hour))
	if hasSignal(t, h, support, "gone@example.com", "recently_cancelled") {
		t.Error("cancellation older than 30 days still listed")
	}
}

func hasSignal(t *testing.T, h *harness, tok reqOpt, email, signal string) bool {
	t.Helper()
	res := h.do("GET", "/api/v1/admin/retention", nil, tok)
	h.wantStatus(res, http.StatusOK)
	for _, it := range list(t, res.Data["items"]) {
		row := obj(t, it)
		if row["email"] == email && row["signal"] == signal {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
