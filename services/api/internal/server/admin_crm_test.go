package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"mise.tt/api/internal/payments/testpay"
	"mise.tt/api/internal/platform/config"
	"mise.tt/api/internal/server"
)

// Sprint 4 integration tests: staff administration, RBAC, the customer
// list and Customer 360, CRM, support, staff subscription actions, reports,
// exports, alerts, settings and the audit log.

func (h *harness) leadID(email string) string {
	h.t.Helper()
	var id string
	if err := h.pool.QueryRow(context.Background(), `SELECT id FROM leads WHERE email = $1`, email).Scan(&id); err != nil {
		h.t.Fatal(err)
	}
	return id
}

func (h *harness) customerIDOf(email string) string {
	h.t.Helper()
	var id string
	if err := h.pool.QueryRow(context.Background(), `SELECT c.id FROM customers c JOIN users u ON u.id = c.user_id WHERE u.email = $1`, email).Scan(&id); err != nil {
		h.t.Fatal(err)
	}
	return id
}

func (h *harness) userIDOf(email string) string {
	h.t.Helper()
	var id string
	if err := h.pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&id); err != nil {
		h.t.Fatal(err)
	}
	return id
}

var inviteToken = regexp.MustCompile(`accept-invite\?token=([A-Za-z0-9_\-]+)`)

// ---------------------------------------------------------------- staff and RBAC

func TestStaffInvitationAndRoleRules(t *testing.T) {
	h := newHarness(t)
	root := bearer(h.adminLogin("root@mise.test", "super_admin"))
	admin := bearer(h.adminLogin("admin@mise.test", "admin"))
	support := bearer(h.adminLogin("help@mise.test", "support"))

	// Staff accounts are managed by super administrators only (users.manage);
	// admins can look but not invite.
	h.wantStatus(h.do("GET", "/api/v1/admin/staff", nil, admin), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/staff/invitations", map[string]any{"email": "boss@mise.test", "name": "Big Boss", "role": "super_admin"}, admin), http.StatusForbidden)
	h.wantStatus(h.do("POST", "/api/v1/admin/staff/invitations", map[string]any{"email": "x@mise.test", "name": "X", "role": "support"}, support), http.StatusForbidden)
	h.wantStatus(h.do("POST", "/api/v1/admin/staff/invitations", map[string]any{"email": "x@mise.test", "name": "X", "role": "customer"}, root), http.StatusUnprocessableEntity)

	res := h.do("POST", "/api/v1/admin/staff/invitations", map[string]any{"email": "Nadia.Ali@mise.test", "name": "Nadia Ali", "role": "operations"}, root)
	h.wantStatus(res, http.StatusCreated)
	// The same address twice is refused while the first is open.
	h.wantStatus(h.do("POST", "/api/v1/admin/staff/invitations", map[string]any{"email": "nadia.ali@mise.test", "name": "Nadia", "role": "support"}, root), http.StatusUnprocessableEntity)
	h.drainJobs()
	msg := h.mailer.lastTo(t, "nadia.ali@mise.test")
	m := inviteToken.FindStringSubmatch(msg.Text)
	if m == nil || !strings.Contains(msg.Text, "http://admin.test/accept-invite") {
		t.Fatalf("invitation email: %s", msg.Text)
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/auth/invitations/accept", map[string]any{"token": m[1], "password": "short"}), http.StatusUnprocessableEntity)
	h.wantStatus(h.do("POST", "/api/v1/admin/auth/invitations/accept", map[string]any{"token": m[1], "password": "a long staff password 2026"}), http.StatusOK)
	// Single use.
	h.wantError(h.do("POST", "/api/v1/admin/auth/invitations/accept", map[string]any{"token": m[1], "password": "a long staff password 2026"}), http.StatusBadRequest, "token_invalid")
	res = h.do("POST", "/api/v1/admin/auth/login", map[string]string{"email": "nadia.ali@mise.test", "password": "a long staff password 2026"})
	h.wantStatus(res, http.StatusOK)
	nadia := bearer(res.Data["token"].(string))
	h.wantStatus(h.do("GET", "/api/v1/admin/orders", nil, nadia), http.StatusOK)
	h.wantStatus(h.do("GET", "/api/v1/admin/staff", nil, nadia), http.StatusForbidden)

	nadiaID := h.userIDOf("nadia.ali@mise.test")
	reason := map[string]any{"role": "finance", "reason": "Covers month end"}
	// A reason is required; nobody changes their own roles.
	h.wantStatus(h.do("POST", "/api/v1/admin/staff/"+nadiaID+"/roles", map[string]any{"role": "finance"}, root), http.StatusUnprocessableEntity)
	h.wantError(h.do("POST", "/api/v1/admin/staff/"+h.userIDOf("root@mise.test")+"/roles", map[string]any{"role": "finance", "reason": "Myself"}, root), http.StatusForbidden, "not_on_yourself")
	h.wantStatus(h.do("POST", "/api/v1/admin/staff/"+nadiaID+"/roles", reason, admin), http.StatusForbidden)
	h.wantStatus(h.do("POST", "/api/v1/admin/staff/"+nadiaID+"/roles", reason, root), http.StatusOK)
	h.wantStatus(h.do("GET", "/api/v1/admin/payments", nil, nadia), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/staff/"+nadiaID+"/roles/revoke", reason, root), http.StatusOK)
	h.wantStatus(h.do("GET", "/api/v1/admin/payments", nil, nadia), http.StatusForbidden)

	// Disabling ends sessions at once.
	h.wantStatus(h.do("POST", "/api/v1/admin/staff/"+nadiaID+"/disable", map[string]any{"reason": "Left the company"}, root), http.StatusOK)
	h.wantStatus(h.do("GET", "/api/v1/admin/orders", nil, nadia), http.StatusUnauthorized)

	// A second super_admin can be stepped down; the last one cannot.
	rootID := h.userIDOf("root@mise.test")
	other := bearer(h.adminLogin("root2@mise.test", "super_admin"))
	h.wantStatus(h.do("POST", "/api/v1/admin/staff/"+h.userIDOf("root2@mise.test")+"/roles/revoke", map[string]any{"role": "super_admin", "reason": "Rotation"}, root), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/staff/"+rootID+"/roles/revoke", map[string]any{"role": "super_admin", "reason": "Rotation"}, other), http.StatusUnauthorized)
	if n := h.count(`SELECT count(*) FROM user_roles WHERE role_key = 'super_admin'`); n != 1 {
		t.Errorf("super admins = %d", n)
	}

	// Every change was audited, with the reason.
	if n := h.count(`SELECT count(*) FROM audit_log WHERE action LIKE 'staff.%' AND reason IS NOT NULL`); n < 4 {
		t.Errorf("audited staff changes with a reason = %d", n)
	}
	res = h.do("GET", "/api/v1/admin/roles", nil, admin)
	h.wantStatus(res, http.StatusOK)
	if len(list(t, res.Data["roles"])) < 10 {
		t.Errorf("roles = %v", res.Data["roles"])
	}
	// Customers never reach admin routes.
	cust := bearer(h.customer("not-staff-4@example.com"))
	for _, path := range []string{"/api/v1/admin/staff", "/api/v1/admin/dashboard", "/api/v1/admin/customers", "/api/v1/admin/audit-events"} {
		h.wantStatus(h.do("GET", path, nil, cust), http.StatusUnauthorized)
	}
}

func TestStaffMustFinishMFAWhereRequired(t *testing.T) {
	h := newHarness(t, func(c *config.Config, _ *server.Limits) { c.AdminMFARequired = true })
	tok := bearer(h.adminLogin("mfa-admin@mise.test", "admin"))
	h.wantError(h.do("GET", "/api/v1/admin/dashboard", nil, tok), http.StatusForbidden, "mfa_required")
	h.wantError(h.do("GET", "/api/v1/admin/customers", nil, tok), http.StatusForbidden, "mfa_required")
}

// ---------------------------------------------------------------- customers

func TestCustomerListSearchAnd360(t *testing.T) {
	s := newShop(t)
	h := s.h
	tok, sub := s.active("aaliyah@example.com", testpay.CardApprove)
	h.wantStatus(h.do("PUT", "/api/v1/me/preferences", map[string]any{"dietary": []string{"vegetarian"}, "allergies": []map[string]any{{"allergen": "fish"}}}, tok), http.StatusOK)
	s.customer("prospect@example.com")
	cid := h.customerIDOf("aaliyah@example.com")

	support := bearer(h.adminLogin("help4@mise.test", "support"))
	finance := bearer(h.adminLogin("money4@mise.test", "finance"))
	marketing := bearer(h.adminLogin("mkt4@mise.test", "marketing"))

	h.wantStatus(h.do("GET", "/api/v1/admin/customers", nil, marketing), http.StatusForbidden)
	res := h.do("GET", "/api/v1/admin/customers?q=AALIYAH", nil, support)
	h.wantStatus(res, http.StatusOK)
	items := list(t, res.Data["items"])
	if len(items) != 1 || obj(t, items[0])["id"] != cid || obj(t, items[0])["subscription_status"] != "active" {
		t.Fatalf("search = %v", res.Data)
	}
	res = h.do("GET", "/api/v1/admin/customers?subscription_status=none", nil, support)
	if res.Data["total"] != float64(1) {
		t.Errorf("customers without a subscription = %v", res.Data["total"])
	}
	res = h.do("GET", "/api/v1/admin/customers?page_size=1&page=2&sort=name", nil, support)
	if len(list(t, res.Data["items"])) != 1 || res.Data["total"] != float64(2) {
		t.Errorf("pagination = %v", res.Data)
	}

	res = h.do("GET", "/api/v1/admin/customers/"+cid, nil, finance)
	h.wantStatus(res, http.StatusOK)
	if len(list(t, res.Data["subscriptions"])) != 1 || res.Data["can_view_sensitive"] != false {
		t.Errorf("360 = %v", res.Data)
	}
	// Allergies, addresses and phone need customers.sensitive; viewing them is audited.
	h.wantStatus(h.do("GET", "/api/v1/admin/customers/"+cid+"/preferences", nil, finance), http.StatusForbidden)
	res = h.do("GET", "/api/v1/admin/customers/"+cid+"/preferences", nil, support)
	h.wantStatus(res, http.StatusOK)
	if len(list(t, obj(t, res.Data["preferences"])["allergies"])) != 1 || len(list(t, res.Data["addresses"])) != 1 {
		t.Errorf("preferences = %v", res.Data)
	}
	if n := h.count(`SELECT count(*) FROM audit_log WHERE action = 'customer.sensitive_viewed' AND target_id = $1`, cid); n != 1 {
		t.Errorf("sensitive views audited = %d", n)
	}
	res = h.do("GET", "/api/v1/admin/customers/"+cid+"/billing", nil, finance)
	h.wantStatus(res, http.StatusOK)
	if len(list(t, res.Data["payments"])) != 1 || len(list(t, res.Data["invoices"])) != 1 {
		t.Errorf("billing = %v", res.Data)
	}
	res = h.do("GET", "/api/v1/admin/customers/"+cid+"/activity", nil, support)
	if len(list(t, res.Data["items"])) == 0 {
		t.Error("no activity")
	}
	h.wantStatus(h.do("GET", "/api/v1/admin/customers/00000000-0000-0000-0000-000000000000", nil, support), http.StatusNotFound)

	// Notes: support may write, finance may only read.
	h.wantStatus(h.do("POST", "/api/v1/admin/customers/"+cid+"/notes", map[string]any{"body": "Prefers deliveries after 5pm."}, support), http.StatusCreated)
	h.wantStatus(h.do("POST", "/api/v1/admin/customers/"+cid+"/notes", map[string]any{"body": "x"}, finance), http.StatusForbidden)
	res = h.do("GET", "/api/v1/admin/customers/"+cid+"/notes", nil, finance)
	if len(list(t, res.Data["items"])) != 1 {
		t.Errorf("notes = %v", res.Data)
	}
	// Note bodies never go into the audit log.
	if n := h.count(`SELECT count(*) FROM audit_log WHERE metadata::text LIKE '%5pm%'`); n != 0 {
		t.Error("a note body reached the audit log")
	}

	// Staff act on the subscription through the same rules, with a reason code.
	h.wantStatus(h.do("POST", "/api/v1/admin/subscriptions/"+sub+"/pause", map[string]any{"weeks": 1}, support), http.StatusUnprocessableEntity)
	h.wantStatus(h.do("POST", "/api/v1/admin/subscriptions/"+sub+"/pause", map[string]any{"weeks": 1, "reason": "customer_request"}, marketing), http.StatusForbidden)
	res = h.do("POST", "/api/v1/admin/subscriptions/"+sub+"/pause", map[string]any{"weeks": 9, "reason": "customer_request"}, support)
	h.wantStatus(res, http.StatusUnprocessableEntity) // the customer's own limit applies
	res = h.do("POST", "/api/v1/admin/subscriptions/"+sub+"/pause", map[string]any{"weeks": 1, "reason": "customer_request", "note": "Called in"}, support)
	h.wantStatus(res, http.StatusOK)
	if st := obj(t, res.Data["subscription"])["status"]; st != "pause_scheduled" {
		t.Errorf("status = %v (the paid first week is still delivered)", st)
	}
	if n := h.count(`SELECT count(*) FROM audit_log WHERE action LIKE 'subscription.%' AND reason = 'customer_request: Called in' AND actor_user_id = $1`, h.userIDOf("help4@mise.test")); n == 0 {
		t.Error("staff action not audited with its reason")
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/subscriptions/"+sub+"/resume", map[string]any{"reason": "customer_request"}, support), http.StatusOK)
}

// ---------------------------------------------------------------- CRM

func TestLeadStagesNotesAndConversion(t *testing.T) {
	h := newHarness(t)
	h.join("lead4@example.com", nil)
	lead := h.leadID("lead4@example.com")
	mkt := bearer(h.adminLogin("mkt@mise.test", "marketing"))
	ops := bearer(h.adminLogin("ops4@mise.test", "operations"))
	mktID := h.userIDOf("mkt@mise.test")

	res := h.do("GET", "/api/v1/admin/leads/"+lead+"/crm", nil, mkt)
	h.wantStatus(res, http.StatusOK)
	if res.Data["stage"] != "new" {
		t.Errorf("default stage = %v", res.Data["stage"])
	}
	h.wantStatus(h.do("PUT", "/api/v1/admin/leads/"+lead+"/crm", map[string]any{"stage": "converted"}, mkt), http.StatusUnprocessableEntity)
	h.wantStatus(h.do("PUT", "/api/v1/admin/leads/"+lead+"/crm", map[string]any{"stage": "contacted", "assigned_to": "00000000-0000-0000-0000-000000000000"}, mkt), http.StatusUnprocessableEntity)
	h.wantStatus(h.do("PUT", "/api/v1/admin/leads/"+lead+"/crm", map[string]any{"stage": "qualified", "assigned_to": mktID}, mkt), http.StatusOK)
	h.wantStatus(h.do("PUT", "/api/v1/admin/leads/"+lead+"/crm", map[string]any{"stage": "contacted"}, ops), http.StatusForbidden)
	h.wantStatus(h.do("POST", "/api/v1/admin/leads/"+lead+"/notes", map[string]any{"body": "Asked about school lunches."}, mkt), http.StatusCreated)

	// Verification and qualification are separate: the lifecycle is untouched.
	if st := h.leadStatus("lead4@example.com"); st != "VERIFIED" {
		t.Errorf("lead status = %s", st)
	}
	admin := bearer(h.adminLogin("lead-admin@mise.test", "admin"))
	res = h.do("GET", "/api/v1/admin/leads?stage=qualified&assigned_to="+mktID, nil, admin)
	h.wantStatus(res, http.StatusOK)
	if res.Data["total"] != float64(1) {
		t.Fatalf("filtered leads = %v", res.Data)
	}
	// A stage never creates an account.
	if n := h.count(`SELECT count(*) FROM customers`); n != 0 {
		t.Fatal("a CRM stage created a customer")
	}
	// Conversion happens only by the person registering and verifying.
	h.customer("lead4@example.com")
	res = h.do("GET", "/api/v1/admin/leads?q=lead4", nil, admin)
	if stage := obj(t, list(t, res.Data["items"])[0])["stage"]; stage != "converted" {
		t.Errorf("stage after conversion = %v", stage)
	}
	// Attribution and history are preserved: the lead row, its notes and stage stay.
	if n := h.count(`SELECT count(*) FROM crm_notes WHERE lead_id = $1`, lead); n != 1 {
		t.Error("lead notes lost on conversion")
	}
	if n := h.count(`SELECT count(*) FROM customers WHERE lead_id = $1`, lead); n != 1 {
		t.Error("lead not linked to the customer")
	}
}

func TestSegmentsCampaignsAndReferralOversight(t *testing.T) {
	s := newShop(t)
	h := s.h
	s.active("seg-sub@example.com", testpay.CardApprove)
	s.customer("seg-prospect@example.com")
	mkt := bearer(h.adminLogin("seg-mkt@mise.test", "marketing"))

	res := h.do("POST", "/api/v1/admin/segments/preview", map[string]any{"lifecycle": []string{"subscriber"}, "plan_ids": []string{s.basic}}, mkt)
	h.wantStatus(res, http.StatusOK)
	if res.Data["total"] != float64(1) {
		t.Errorf("subscriber preview = %v", res.Data)
	}
	res = h.do("POST", "/api/v1/admin/segments/preview", map[string]any{"lifecycle": []string{"prospect"}}, mkt)
	if res.Data["total"] != float64(1) {
		t.Errorf("prospect preview = %v", res.Data)
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/segments/preview", map[string]any{"regions": []string{"atlantis"}}, mkt), http.StatusUnprocessableEntity)
	res = h.do("POST", "/api/v1/admin/segments", map[string]any{"name": "Basic subscribers", "rules": map[string]any{"plan_ids": []string{s.basic}}}, mkt)
	h.wantStatus(res, http.StatusCreated)
	segID := res.Data["id"].(string)
	res = h.do("GET", "/api/v1/admin/segments", nil, mkt)
	if obj(t, list(t, res.Data["items"])[0])["customers"] != float64(1) {
		t.Errorf("segment size = %v", res.Data)
	}

	// Campaigns are records; activating sends nothing.
	res = h.do("POST", "/api/v1/admin/campaigns", map[string]any{"name": "Divali launch", "objective": "acquisition", "channel": "social",
		"utm_campaign": "divali-2026", "segment_id": segID}, mkt)
	h.wantStatus(res, http.StatusCreated)
	camp := res.Data["id"].(string)
	h.wantStatus(h.do("POST", "/api/v1/admin/campaigns/"+camp+"/status", map[string]any{"status": "completed"}, mkt), http.StatusConflict)
	h.wantStatus(h.do("POST", "/api/v1/admin/campaigns/"+camp+"/status", map[string]any{"status": "active"}, mkt), http.StatusOK)
	sent := h.mailer.countTo("seg-sub@example.com")
	h.drainJobs()
	if h.mailer.countTo("seg-sub@example.com") != sent {
		t.Error("activating a campaign sent email")
	}
	// Attribution: a lead who arrived with the campaign's UTM value.
	h.join("from-campaign@example.com", map[string]any{"attribution": map[string]any{"first": map[string]any{"utm_source": "instagram", "utm_campaign": "divali-2026"}}})
	res = h.do("GET", "/api/v1/admin/campaigns/"+camp, nil, mkt)
	h.wantStatus(res, http.StatusOK)
	if a := obj(t, res.Data["attribution"]); a["leads"] != float64(1) || a["verified_leads"] != float64(1) {
		t.Errorf("attribution = %v", a)
	}
	if n := h.count(`SELECT count(*) FROM audit_log WHERE action = 'campaign.active'`); n != 1 {
		t.Error("campaign activation not audited")
	}

	// Referral oversight: same-network referrals are flagged for review.
	_, _ = h.join("referrer4@example.com", nil, fromIP("203.0.113.7"))
	var code string
	_ = h.pool.QueryRow(context.Background(), `SELECT referral_code FROM leads WHERE email = 'referrer4@example.com'`).Scan(&code)
	h.join("friend4@example.com", map[string]any{"referral_code": code}, fromIP("203.0.113.7"))
	res = h.do("GET", "/api/v1/admin/referrals?flagged=true", nil, mkt)
	h.wantStatus(res, http.StatusOK)
	found := false
	for _, it := range list(t, res.Data["items"]) {
		for _, f := range list(t, obj(t, it)["flags"]) {
			found = found || f == "same_network" || f == "flagged_at_signup"
		}
	}
	if !found {
		t.Errorf("same-network referral not flagged: %v", res.Data)
	}
}

// ---------------------------------------------------------------- support

func TestSupportCaseLifecycle(t *testing.T) {
	s := newShop(t)
	h := s.h
	s.customer("needs-help@example.com")
	cid := h.customerIDOf("needs-help@example.com")
	support := bearer(h.adminLogin("agent@mise.test", "support"))
	ops := bearer(h.adminLogin("ops-case@mise.test", "operations"))
	marketing := bearer(h.adminLogin("mkt-case@mise.test", "marketing"))

	h.wantStatus(h.do("POST", "/api/v1/admin/support/cases", map[string]any{"subject": "Box", "category": "delivery"}, ops), http.StatusForbidden)
	h.wantStatus(h.do("GET", "/api/v1/admin/support/cases", nil, marketing), http.StatusForbidden)
	h.wantStatus(h.do("POST", "/api/v1/admin/support/cases", map[string]any{"subject": "Box", "category": "weather"}, support), http.StatusUnprocessableEntity)
	res := h.do("POST", "/api/v1/admin/support/cases", map[string]any{"customer_id": cid, "subject": "Card declined twice", "category": "billing",
		"priority": "high", "note": "Customer called."}, support)
	h.wantStatus(res, http.StatusCreated)
	id := res.Data["id"].(string)
	if res.Data["status"] != "open" || len(list(t, res.Data["events"])) != 1 {
		t.Fatalf("new case = %v", res.Data)
	}
	// A new case raises one alert, visible to case readers only.
	if n := h.count(`SELECT count(*) FROM admin_alerts WHERE kind = 'support_case' AND resource_id = $1`, id); n != 1 {
		t.Errorf("case alerts = %d", n)
	}
	h.wantStatus(h.do("PATCH", "/api/v1/admin/support/cases/"+id, map[string]any{"status": "in_progress", "assigned_to": h.userIDOf("agent@mise.test")}, support), http.StatusOK)
	// The case page names the assignee, not only the list.
	if res = h.do("GET", "/api/v1/admin/support/cases/"+id, nil, support); res.Data["assignee_name"] != "Test Admin" {
		t.Errorf("case assignee_name = %v", res.Data["assignee_name"])
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/support/cases/"+id+"/notes", map[string]any{"body": "Asked them to try another card."}, support), http.StatusCreated)
	h.wantStatus(h.do("PATCH", "/api/v1/admin/support/cases/"+id, map[string]any{"status": "resolved"}, support), http.StatusUnprocessableEntity)
	res = h.do("PATCH", "/api/v1/admin/support/cases/"+id, map[string]any{"status": "resolved", "resolution": "Paid with another card."}, support)
	h.wantStatus(res, http.StatusOK)
	if res.Data["resolved_at"] == nil {
		t.Error("resolved_at not set")
	}
	h.wantStatus(h.do("PATCH", "/api/v1/admin/support/cases/"+id, map[string]any{"status": "closed"}, support), http.StatusOK)
	h.wantError(h.do("PATCH", "/api/v1/admin/support/cases/"+id, map[string]any{"status": "open"}, support), http.StatusConflict, "case_closed")
	res = h.do("GET", "/api/v1/admin/support/cases/"+id, nil, ops)
	h.wantStatus(res, http.StatusOK)
	if n := len(list(t, res.Data["events"])); n < 6 {
		t.Errorf("case history has %d events", n)
	}
	res = h.do("GET", "/api/v1/admin/customers/"+cid, nil, support)
	if len(list(t, res.Data["cases"])) != 1 {
		t.Error("case missing from the Customer 360")
	}
}

// ---------------------------------------------------------------- reports, exports, alerts, audit

func metric(t *testing.T, data map[string]any, key string) float64 {
	t.Helper()
	for _, field := range []string{"metrics", "totals"} {
		if data[field] == nil {
			continue
		}
		for _, m := range list(t, data[field]) {
			if obj(t, m)["key"] == key {
				return obj(t, m)["value"].(float64)
			}
		}
	}
	t.Fatalf("no metric %s", key)
	return 0
}

func TestReportFiguresMatchTheRecords(t *testing.T) {
	s := newShop(t)
	h := s.h
	s.active("rep-one@example.com", testpay.CardApprove)
	s.active("rep-two@example.com", testpay.CardApprove)
	s.customer("rep-three@example.com")
	finance := bearer(h.adminLogin("rep-fin@mise.test", "finance"))
	var paymentID string
	_ = h.pool.QueryRow(context.Background(), `SELECT id FROM payments LIMIT 1`).Scan(&paymentID)
	h.wantStatus(h.do("POST", "/api/v1/admin/payments/"+paymentID+"/refunds", map[string]any{"amount_minor": 10000, "reason": "Late box", "request_key": "rep-refund-1"}, finance), http.StatusCreated)
	// Payments and refunds are stamped with the database's now(), but the shop's
	// clock is pinned, and the reports measure periods against it. Put both on the
	// pinned clock, or this test depends on the real date.
	pinned := h.clock.Now()
	if _, err := h.pool.Exec(context.Background(), `UPDATE payments SET succeeded_at = $1 WHERE succeeded_at IS NOT NULL`, pinned); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(context.Background(), `UPDATE refunds SET completed_at = $1 WHERE completed_at IS NOT NULL`, pinned); err != nil {
		t.Fatal(err)
	}

	support := bearer(h.adminLogin("rep-sup@mise.test", "support"))
	h.wantStatus(h.do("GET", "/api/v1/admin/dashboard", nil, support), http.StatusForbidden)
	res := h.do("GET", "/api/v1/admin/dashboard", nil, finance)
	h.wantStatus(res, http.StatusOK)
	want := map[string]float64{"customers": 3, "active_subscriptions": 2, "new_subscriptions": 2, "collected": 66000, "invoiced": 66000,
		"refunded": 10000, "successful_payments": 2, "failed_payments": 0}
	for k, v := range want {
		if got := metric(t, res.Data, k); got != v {
			t.Errorf("dashboard %s = %v, want %v", k, got, v)
		}
	}
	// Refunds are reported apart: collected is not reduced by them.
	today := h.clock.Now().Format("2006-01-02")
	res = h.do("GET", "/api/v1/admin/reports/finance?from=2026-01-01&to="+today, nil, finance)
	if res.Status != http.StatusOK {
		t.Fatalf("finance report: %s", res.Raw)
	}
	if metric(t, res.Data, "collected") != 66000 || metric(t, res.Data, "refunded") != 10000 {
		t.Errorf("finance report = %v", res.Data["totals"])
	}
	var sum float64
	for _, p := range list(t, obj(t, list(t, res.Data["series"])[0])["points"]) {
		sum += obj(t, p)["value"].(float64)
	}
	if sum != 66000 {
		t.Errorf("collected series sums to %v", sum)
	}
	res = h.do("GET", "/api/v1/admin/reports/subscriptions?from=2026-01-01&to="+today, nil, finance)
	if metric(t, res.Data, "new") != 2 {
		t.Errorf("subscription report = %v", res.Data["totals"])
	}
	h.wantStatus(h.do("GET", "/api/v1/admin/reports/finance?from=2026-02-01&to=2026-01-01", nil, finance), http.StatusUnprocessableEntity)
	h.wantStatus(h.do("GET", "/api/v1/admin/reports/profit?from=2026-01-01&to=2026-02-01", nil, finance), http.StatusNotFound)
}

func TestExportsArePermissionedPrivateAndAudited(t *testing.T) {
	s := newShop(t)
	h := s.h
	s.active("export-me@example.com", testpay.CardApprove)
	finance := bearer(h.adminLogin("exp-fin@mise.test", "finance"))
	other := bearer(h.adminLogin("exp-other@mise.test", "admin"))
	marketing := bearer(h.adminLogin("exp-mkt@mise.test", "marketing"))

	h.wantStatus(h.do("POST", "/api/v1/admin/exports", map[string]any{"kind": "customers"}, marketing), http.StatusForbidden)
	h.wantStatus(h.do("POST", "/api/v1/admin/exports", map[string]any{"kind": "everything"}, finance), http.StatusUnprocessableEntity)
	res := h.do("POST", "/api/v1/admin/exports", map[string]any{"kind": "customers"}, finance)
	h.wantStatus(res, http.StatusAccepted)
	id := res.Data["id"].(string)
	h.drainJobs()
	res = h.do("GET", "/api/v1/admin/exports", nil, finance)
	if e := obj(t, list(t, res.Data["items"])[0]); e["status"] != "ready" || e["row_count"] != float64(1) {
		t.Fatalf("export = %v", e)
	}
	dl := h.do("GET", "/api/v1/admin/exports/"+id+"/download", nil, finance)
	h.wantStatus(dl, http.StatusOK)
	body := string(dl.Raw)
	if !strings.HasPrefix(body, "customer_id,first_name") || !strings.Contains(body, "export-me@example.com") {
		t.Errorf("csv = %s", body)
	}
	if strings.Contains(body, "fish") || strings.Contains(body, "Ariapita") {
		t.Error("the export holds sensitive fields")
	}
	// Only the requester can download it.
	h.wantStatus(h.do("GET", "/api/v1/admin/exports/"+id+"/download", nil, other), http.StatusNotFound)
	if n := h.count(`SELECT count(*) FROM audit_log WHERE action IN ('export.requested', 'export.downloaded')`); n != 2 {
		t.Errorf("export audit entries = %d", n)
	}
}

func TestAlertsAreDedupedAndScopedByPermission(t *testing.T) {
	s := newShop(t)
	h := s.h
	tok, addr := s.customer("declines@example.com")
	_, ref := s.subscribe(tok, addr, s.basic, nil)
	s.pay(ref, "decline", testpay.CardApprove)
	h.drainJobs()
	for i := 0; i < 2; i++ {
		if _, err := h.bo.ScanAlerts(context.Background(), h.clock.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if n := h.count(`SELECT count(*) FROM admin_alerts WHERE kind = 'payment_failed'`); n != 1 {
		t.Fatalf("payment alerts after two scans = %d", n)
	}
	finance := bearer(h.adminLogin("al-fin@mise.test", "finance"))
	marketing := bearer(h.adminLogin("al-mkt@mise.test", "marketing"))
	res := h.do("GET", "/api/v1/admin/alerts", nil, finance)
	h.wantStatus(res, http.StatusOK)
	if res.Data["unread"] != float64(1) {
		t.Errorf("finance alerts = %v", res.Data)
	}
	res = h.do("GET", "/api/v1/admin/alerts", nil, marketing)
	if len(list(t, res.Data["items"])) != 0 {
		t.Error("marketing sees payment alerts")
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/alerts/read", map[string]any{"ids": []int{}}, finance), http.StatusOK)
	res = h.do("GET", "/api/v1/admin/alerts", nil, finance)
	if res.Data["unread"] != float64(0) {
		t.Error("alerts still unread")
	}
}

func TestAuditLogIsAppendOnlyAndPermissioned(t *testing.T) {
	h := newHarness(t)
	admin := bearer(h.adminLogin("audit-admin@mise.test", "admin"))
	support := bearer(h.adminLogin("audit-sup@mise.test", "support"))
	root := bearer(h.adminLogin("audit-root@mise.test", "super_admin"))
	h.wantStatus(h.do("POST", "/api/v1/admin/staff/invitations", map[string]any{"email": "audited@mise.test", "name": "Audited Person", "role": "support"}, root), http.StatusCreated)
	ctx := context.Background()
	if _, err := h.pool.Exec(ctx, `UPDATE audit_log SET action = 'tampered'`); err == nil {
		t.Error("an audit entry was edited")
	}
	if _, err := h.pool.Exec(ctx, `DELETE FROM audit_log`); err == nil {
		t.Error("audit entries were deleted")
	}
	h.wantStatus(h.do("GET", "/api/v1/admin/audit-events", nil, support), http.StatusForbidden)
	res := h.do("GET", "/api/v1/admin/audit-events?action=staff.inv", nil, admin)
	h.wantStatus(res, http.StatusOK)
	items := list(t, res.Data["items"])
	if len(items) != 1 || obj(t, items[0])["action"] != "staff.invited" {
		t.Errorf("audit = %v", res.Data)
	}
	raw, _ := json.Marshal(items)
	if strings.Contains(string(raw), "audited@mise.test") || strings.Contains(strings.ToLower(string(raw)), "password") {
		t.Error("personal data or secrets in audit entries")
	}
	// Settings: everyone in the console can read; changing needs settings.manage.
	h.wantStatus(h.do("GET", "/api/v1/admin/settings", nil, support), http.StatusOK)
	h.wantStatus(h.do("PUT", "/api/v1/admin/settings", map[string]any{"business.support_email": "help@mise.tt"}, admin), http.StatusForbidden)
	h.wantStatus(h.do("PUT", "/api/v1/admin/settings", map[string]any{"business.secret_key": "x"}, root), http.StatusUnprocessableEntity)
	res = h.do("PUT", "/api/v1/admin/settings", map[string]any{"business.support_email": "help@mise.tt"}, root)
	h.wantStatus(res, http.StatusOK)
	if n := h.count(`SELECT count(*) FROM audit_log WHERE action = 'setting.changed'`); n != 1 {
		t.Error("setting change not audited")
	}
	res = h.do("GET", "/api/v1/admin/integrations", nil, root)
	h.wantStatus(res, http.StatusOK)
	for _, it := range list(t, res.Data["items"]) {
		i := obj(t, it)
		if i["key"] == "payments" && i["status"] != "test_mode" {
			t.Errorf("payments integration = %v", i)
		}
		if i["key"] == "hubspot" && i["status"] != "not_configured" {
			t.Errorf("hubspot claimed as %v", i["status"])
		}
	}
}
