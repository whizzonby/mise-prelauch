package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mise.tt/api/internal/commerce"
	"mise.tt/api/internal/menus"
	"mise.tt/api/internal/payments/testpay"
	"mise.tt/api/internal/platform/config"
	"mise.tt/api/internal/server"
)

// Sprint 3 integration tests: plans, subscriptions, weekly menus, meal
// selection, the scheduler (cycles, renewals, cutoffs), payments through the
// test provider and its signed webhooks, billing, invoices, orders, refunds.
// The commerce clock is set explicitly, so cutoffs and renewals are exact.
//
// The calendar used (Trinidad and Tobago time, cutoff Thursday 23:59,
// renewal 48 hours before the cutoff):
//
//	Wed 11 Nov 10:00   start; the open week is 16 Nov (cutoff Thu 12 Nov 23:59)
//	Fri 13 Nov 00:30   week of 16 Nov closed; the week of 23 Nov is created
//	Wed 18 Nov 00:30   week of 23 Nov billed (renewal was Tue 17 Nov 23:59)
//	Fri 20 Nov 00:30   week of 23 Nov closed

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, menus.Location())
	if err != nil {
		panic(err)
	}
	return t
}

type shop struct {
	h        *harness
	k        *kitchen
	admin    reqOpt
	basic    string // 3 meals, TTD 330
	family   string // 4 meals, TTD 400
	meals    []string
	fishMeal string // declares fish
	unknown  string // allergens not declared
}

func newShop(t *testing.T, mutate ...func(*config.Config, *server.Limits)) *shop {
	t.Helper()
	h := newHarness(t, mutate...)
	h.clock.Set(at("2026-11-11 10:00"))
	if _, err := h.pool.Exec(context.Background(), `INSERT INTO delivery_zones (country, region, status) VALUES ('TT', 'port-of-spain', 'active')`); err != nil {
		t.Fatal(err)
	}
	s := &shop{h: h, k: newKitchen(t, h)}
	s.admin = s.k.admin
	for i, name := range []string{"Curry chicken", "Pelau", "Callaloo soup", "Stew chicken"} {
		id, _ := s.k.published(name, map[string]any{"allergens": []string{}, "allergen_status": "declared", "dietary_tags": []string{}})
		if i == 2 {
			// Callaloo is tagged vegetarian.
			id2, _ := s.k.published("Doubles bowl", map[string]any{"allergens": []string{"wheat"}, "allergen_status": "declared", "dietary_tags": []string{"vegetarian"}})
			s.meals = append(s.meals, id2)
		}
		s.meals = append(s.meals, id)
	}
	s.fishMeal, _ = s.k.published("Buljol", map[string]any{"allergens": []string{"fish"}, "allergen_status": "declared"})
	s.unknown, _ = s.k.published("Mystery stew", map[string]any{"allergen_status": "unconfirmed"})
	s.meals = append(s.meals, s.fishMeal, s.unknown)
	s.basic = s.plan("Basic", 3, 33000)
	s.family = s.plan("Family", 4, 40000)
	s.menu("2026-11-16")
	s.menu("2026-11-23")
	return s
}

func (s *shop) plan(name string, meals int, price int64) string {
	h := s.h
	h.t.Helper()
	res := h.do("POST", "/api/v1/admin/plans", map[string]any{"name": name, "summary": name + " plan.", "meals_per_week": meals,
		"segment": "everyday", "self_serve": true, "benefits": []string{"Fresh kits"}}, s.admin)
	h.wantStatus(res, http.StatusCreated)
	id := res.Data["id"].(string)
	h.wantStatus(h.do("POST", "/api/v1/admin/plans/"+id+"/prices", map[string]any{"amount_minor": price}, s.admin), http.StatusCreated)
	h.wantStatus(h.do("POST", "/api/v1/admin/plans/"+id+"/activate", nil, s.admin), http.StatusOK)
	return id
}

func (s *shop) menu(week string) string {
	h := s.h
	h.t.Helper()
	var meals []map[string]any
	for _, id := range s.meals {
		meals = append(meals, map[string]any{"meal_id": id})
	}
	res := h.do("POST", "/api/v1/admin/menus", map[string]any{"week": week, "meals": meals}, s.admin)
	h.wantStatus(res, http.StatusCreated)
	id := res.Data["id"].(string)
	h.wantStatus(h.do("POST", "/api/v1/admin/menus/"+id+"/publish", nil, s.admin), http.StatusOK)
	return id
}

// customer signs up with a delivery address and returns the session.
func (s *shop) customer(email string) (reqOpt, string) {
	h := s.h
	h.t.Helper()
	tok := bearer(h.customer(email))
	res := h.do("POST", "/api/v1/me/addresses", address(nil), tok)
	h.wantStatus(res, http.StatusCreated)
	return tok, res.Data["id"].(string)
}

// subscribe starts a subscription and returns it with the test checkout ref.
func (s *shop) subscribe(tok reqOpt, addressID, planID string, extra map[string]any) (string, string) {
	h := s.h
	h.t.Helper()
	body := map[string]any{"plan_id": planID, "address_id": addressID}
	for k, v := range extra {
		body[k] = v
	}
	res := h.do("POST", "/api/v1/me/subscriptions", body, tok)
	h.wantStatus(res, http.StatusCreated)
	co := obj(h.t, res.Data["checkout"])
	if co["test"] != true {
		h.t.Fatalf("checkout is not marked as a test: %v", co)
	}
	redirect := co["redirect_url"].(string)
	if !strings.HasPrefix(redirect, "http://app.test/test-checkout/tps_") {
		h.t.Fatalf("redirect = %q", redirect)
	}
	return res.Data["subscription_id"].(string), redirect[strings.LastIndex(redirect, "/")+1:]
}

// pay presses a button on the test provider's page.
func (s *shop) pay(ref, outcome, card string) {
	s.h.t.Helper()
	s.h.wantStatus(s.h.do("POST", "/api/v1/test-payments/sessions/"+ref+"/complete", map[string]any{"outcome": outcome, "card": card}), http.StatusOK)
}

// active subscribes and pays with a test card that approves (or declines
// later charges, with card "decline").
func (s *shop) active(email, card string) (reqOpt, string) {
	tok, addr := s.customer(email)
	sub, ref := s.subscribe(tok, addr, s.basic, nil)
	s.pay(ref, "approve", card)
	s.h.drainJobs()
	return tok, sub
}

func (s *shop) subscription(tok reqOpt) map[string]any {
	s.h.t.Helper()
	res := s.h.do("GET", "/api/v1/me/subscription", nil, tok)
	s.h.wantStatus(res, http.StatusOK)
	if res.Data["subscription"] == nil {
		return nil
	}
	return obj(s.h.t, res.Data["subscription"])
}

func (s *shop) tick(when string) *commerce.TickReport {
	s.h.t.Helper()
	s.h.clock.Set(at(when))
	rep, err := s.h.com.Service.Tick(context.Background(), at(when))
	if err != nil {
		s.h.t.Fatalf("tick: %v", err)
	}
	s.h.drainJobs()
	return rep
}

// weekCycle returns the customer's cycle for a week.
func (s *shop) weekCycle(tok reqOpt, week string) map[string]any {
	s.h.t.Helper()
	res := s.h.do("GET", "/api/v1/me/subscription-cycles", nil, tok)
	s.h.wantStatus(res, http.StatusOK)
	for _, c := range list(s.h.t, res.Data["items"]) {
		if obj(s.h.t, c)["week"] == week {
			return obj(s.h.t, c)
		}
	}
	s.h.t.Fatalf("no cycle for week %s", week)
	return nil
}

func items(pairs ...any) []map[string]any {
	var out []map[string]any
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, map[string]any{"meal_id": pairs[i], "quantity": pairs[i+1]})
	}
	return out
}

// raw sends a body as given, with the given headers (webhooks).
func (h *harness) raw(method, path string, body []byte, header http.Header) response {
	h.t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	for k, v := range header {
		req.Header[k] = v
	}
	req.RemoteAddr = "198.51.100.9:40000"
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	res := response{Status: rec.Code, Header: rec.Header(), Raw: rec.Body.Bytes()}
	var env struct {
		Data  map[string]any `json:"data"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(res.Raw, &env)
	res.Data, res.Error.Code = env.Data, env.Error.Code
	return res
}

// ---------------------------------------------------------------- plans

func TestPlansAreDatabaseDrivenAndPricesAreVersioned(t *testing.T) {
	s := newShop(t)
	h := s.h
	res := h.do("GET", "/api/v1/plans", nil)
	h.wantStatus(res, http.StatusOK)
	plans := list(t, res.Data["items"])
	if len(plans) != 2 {
		t.Fatalf("public plans = %d, want 2", len(plans))
	}
	basic := obj(t, plans[0])
	if obj(t, basic["price"])["amount_minor"] != float64(33000) || basic["per_meal_minor"] != float64(11000) || basic["purchasable"] != true {
		t.Errorf("basic = %v", basic)
	}

	// A draft plan is not shown; a plan without a price cannot be activated.
	res = h.do("POST", "/api/v1/admin/plans", map[string]any{"name": "Draft", "summary": "Not yet.", "meals_per_week": 2, "segment": "everyday"}, s.admin)
	h.wantStatus(res, http.StatusCreated)
	draft := res.Data["id"].(string)
	h.wantError(h.do("POST", "/api/v1/admin/plans/"+draft+"/activate", nil, s.admin), http.StatusConflict, "no_price")
	h.wantStatus(h.do("GET", "/api/v1/plans/draft", nil), http.StatusNotFound)

	// A new price applies to new sales; a subscriber keeps the price they bought at.
	tok, _ := s.active("keeps-price@example.com", testpay.CardApprove)
	h.clock.Set(h.clock.Now().Add(time.Minute))
	h.wantStatus(h.do("POST", "/api/v1/admin/plans/"+s.basic+"/prices", map[string]any{"amount_minor": 36000}, s.admin), http.StatusCreated)
	if got := s.subscription(tok)["amount_minor"]; got != float64(33000) {
		t.Errorf("subscriber's price changed to %v", got)
	}
	res = h.do("GET", "/api/v1/plans/basic", nil)
	if obj(t, res.Data["price"])["amount_minor"] != float64(36000) {
		t.Errorf("new price not current: %v", res.Data["price"])
	}
	// Price rows cannot be edited, even directly in the database.
	if _, err := h.pool.Exec(context.Background(), `UPDATE subscription_plan_prices SET amount_minor = 1`); err == nil {
		t.Error("a price row was updated")
	}
	// Prices cannot start in the past.
	h.wantStatus(h.do("POST", "/api/v1/admin/plans/"+s.basic+"/prices",
		map[string]any{"amount_minor": 30000, "effective_from": at("2026-01-01 00:00")}, s.admin), http.StatusUnprocessableEntity)
}

// ---------------------------------------------------------------- subscribe and pay

func TestSubscriptionActivatesOnlyWhenTheProviderConfirms(t *testing.T) {
	s := newShop(t)
	h := s.h
	tok, addr := s.customer("first@example.com")

	res := h.do("POST", "/api/v1/me/subscriptions/quote", map[string]any{"plan_id": s.basic}, tok)
	h.wantStatus(res, http.StatusOK)
	if res.Data["first_week"] != "2026-11-16" || obj(t, res.Data["due_now"])["total_minor"] != float64(33000) {
		t.Fatalf("quote = %v", res.Data)
	}
	if !strings.Contains(res.Data["terms"].(string), "TTD 330.00") || !strings.Contains(res.Data["tax_note"].(string), "No tax") {
		t.Errorf("terms = %q / %q", res.Data["terms"], res.Data["tax_note"])
	}

	sub, ref := s.subscribe(tok, addr, s.basic, nil)
	if st := s.subscription(tok)["status"]; st != "pending_payment" {
		t.Fatalf("status before paying = %v", st)
	}
	// The customer returning from the provider proves nothing by itself.
	s.pay(ref, "approve", testpay.CardApprove)
	// The signed webhook is stored, but not yet processed: still pending.
	if st := s.subscription(tok)["status"]; st != "pending_payment" {
		t.Fatalf("status before the webhook was processed = %v", st)
	}
	h.drainJobs()
	v := s.subscription(tok)
	if v["status"] != "active" || v["id"] != sub {
		t.Fatalf("after payment: %v", v)
	}
	if v["payment_method"] != "Test card" || v["auto_renew"] != true {
		t.Errorf("saved method = %v / %v", v["payment_method"], v["auto_renew"])
	}
	// One paid billing cycle, one invoice with a number, one receipt email.
	if n := h.count(`SELECT count(*) FROM billing_cycles WHERE subscription_id = $1 AND status = 'paid'`, sub); n != 1 {
		t.Errorf("paid billing cycles = %d", n)
	}
	var number string
	if err := h.pool.QueryRow(context.Background(), `SELECT number FROM invoices WHERE subscription_id = $1 AND status = 'paid'`, sub).Scan(&number); err != nil || !strings.HasPrefix(number, "MISE-2026-") {
		t.Errorf("invoice number = %q (%v)", number, err)
	}
	if msg := h.mailer.lastTo(t, "first@example.com"); !strings.Contains(msg.Subject, "receipt") && !strings.Contains(msg.Subject, "started") {
		t.Errorf("last email = %q", msg.Subject)
	}
	if n := h.mailer.countTo("first@example.com"); n != 3 { // verify, activated, receipt
		t.Errorf("emails = %d, want 3", n)
	}
	// A second subscription is refused while one is live.
	h.wantError(h.do("POST", "/api/v1/me/subscriptions", map[string]any{"plan_id": s.basic, "address_id": addr}, tok), http.StatusConflict, "subscription_exists")
}

func TestFailedFirstPaymentCanBeRetried(t *testing.T) {
	s := newShop(t)
	h := s.h
	tok, addr := s.customer("declined@example.com")
	_, ref := s.subscribe(tok, addr, s.basic, nil)
	s.pay(ref, "decline", testpay.CardApprove)
	h.drainJobs()
	v := s.subscription(tok)
	if v["status"] != "pending_payment" {
		t.Fatalf("status after decline = %v", v["status"])
	}
	out := obj(t, v["outstanding"])
	bill := obj(t, out["billing"])
	if bill["status"] != "failed" {
		t.Fatalf("billing after decline = %v", bill)
	}
	// Pay again from the subscription page.
	res := h.do("POST", "/api/v1/checkout/sessions", map[string]any{"billing_cycle_id": bill["id"]}, tok)
	h.wantStatus(res, http.StatusCreated)
	redirect := res.Data["redirect_url"].(string)
	s.pay(redirect[strings.LastIndex(redirect, "/")+1:], "approve", testpay.CardApprove)
	h.drainJobs()
	if st := s.subscription(tok)["status"]; st != "active" {
		t.Fatalf("status after retry = %v", st)
	}
	// The result endpoint reports what the server knows.
	co := res.Data["id"].(string)
	res = h.do("GET", "/api/v1/checkout/sessions/"+co, nil, tok)
	h.wantStatus(res, http.StatusOK)
	if res.Data["outcome"] != "succeeded" || res.Data["invoice_id"] == nil {
		t.Errorf("result = %v", res.Data)
	}
	// Someone else's checkout is not found.
	other, _ := s.customer("nosy@example.com")
	h.wantStatus(h.do("GET", "/api/v1/checkout/sessions/"+co, nil, other), http.StatusNotFound)
}

func TestWebhooksAreAuthenticatedIdempotentAndOrderIndependent(t *testing.T) {
	s := newShop(t)
	h := s.h
	tok, addr := s.customer("hooks@example.com")
	sub, ref := s.subscribe(tok, addr, s.basic, nil)

	// A forged webhook is refused and not stored.
	body := []byte(`{"id":"evt_forged","type":"checkout.approved","data":{"session_ref":"` + ref + `"}}`)
	req := func(b []byte, header http.Header) response {
		return h.raw("POST", "/api/v1/webhooks/payments/testpay", b, header)
	}
	h.wantError(req(body, http.Header{"Testpay-Signature": {"t=1,v1=00"}, "Content-Type": {"application/json"}}), http.StatusBadRequest, "invalid_signature")
	// An old, correctly signed request (a replay) is refused too.
	h.wantError(req(body, h.com.Testpay.SignedHeader(body, h.clock.Now().Add(-time.Hour))), http.StatusBadRequest, "invalid_signature")
	if n := h.count(`SELECT count(*) FROM payment_webhook_events`); n != 0 {
		t.Fatalf("stored %d unauthenticated events", n)
	}

	s.pay(ref, "approve", testpay.CardApprove)
	h.drainJobs()

	// The same event delivered again is acknowledged and ignored.
	var payload []byte
	if err := h.pool.QueryRow(context.Background(), `SELECT payload::text FROM payment_webhook_events LIMIT 1`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	res := req(payload, h.com.Testpay.SignedHeader(payload, h.clock.Now()))
	h.wantStatus(res, http.StatusOK)
	if res.Data["duplicate"] != true {
		t.Errorf("repeat not recognised: %s", res.Raw)
	}
	h.drainJobs()

	// A late, out-of-order event (a new id saying "processing") changes nothing:
	// processing reads the provider's current status.
	late := []byte(`{"id":"evt_late","type":"checkout.processing","data":{"session_ref":"` + ref + `"}}`)
	h.wantStatus(req(late, h.com.Testpay.SignedHeader(late, h.clock.Now())), http.StatusOK)
	h.drainJobs()

	// Reconciling again (a retried activation) changes nothing either.
	if err := h.com.Payments.ReconcileCheckout(context.Background(), "testpay", ref, "reconcile", ""); err != nil {
		t.Fatal(err)
	}
	if st := s.subscription(tok)["status"]; st != "active" {
		t.Fatalf("status = %v", st)
	}
	checks := map[string]int{
		`SELECT count(*) FROM payments WHERE status = 'succeeded'`:                    1,
		`SELECT count(*) FROM payment_status_history`:                                 1,
		`SELECT count(*) FROM invoices`:                                               1,
		`SELECT count(*) FROM subscription_events WHERE type = 'activated'`:           1,
		`SELECT count(*) FROM notification_deliveries WHERE kind = 'payment_receipt'`: 1,
		`SELECT count(*) FROM payment_webhook_events WHERE status = 'processed'`:      2,
	}
	for q, want := range checks {
		if got := h.count(q); got != want {
			t.Errorf("%s = %d, want %d", q, got, want)
		}
	}
	if n := h.count(`SELECT count(*) FROM subscriptions WHERE id = $1 AND status = 'active'`, sub); n != 1 {
		t.Error("subscription not active")
	}
}

// ---------------------------------------------------------------- selection

func TestMealSelectionAllowanceAndCutoff(t *testing.T) {
	s := newShop(t)
	h := s.h
	tok, _ := s.active("chooser@example.com", testpay.CardApprove)
	// The customer is allergic to fish.
	h.wantStatus(h.do("PUT", "/api/v1/me/preferences", map[string]any{"dietary": []string{"vegetarian"}, "allergies": []map[string]any{{"allergen": "fish"}}}, tok), http.StatusOK)

	cycle := s.weekCycle(tok, "2026-11-16")
	id := cycle["id"].(string)
	if cycle["selection_open"] != true {
		t.Fatalf("selection not open: %v", cycle)
	}
	res := h.do("GET", "/api/v1/me/meal-selections/"+id, nil, tok)
	h.wantStatus(res, http.StatusOK)
	notes := obj(t, res.Data["notes"])
	fish := obj(t, notes[s.fishMeal])
	if len(list(t, fish["contains_your_allergens"])) != 1 {
		t.Errorf("fish allergy not flagged: %v", fish)
	}
	if obj(t, notes[s.unknown])["allergens_unconfirmed"] != true {
		t.Error("undeclared allergens not flagged")
	}
	if res.Data["allowance"] != float64(3) || res.Data["remaining"] != float64(3) {
		t.Errorf("allowance = %v remaining = %v", res.Data["allowance"], res.Data["remaining"])
	}

	// Over the allowance.
	h.wantStatus(h.do("PUT", "/api/v1/me/meal-selections/"+id, map[string]any{"items": items(s.meals[0], 2, s.meals[1], 2)}, tok), http.StatusUnprocessableEntity)
	// A meal that is not on the menu.
	h.wantStatus(h.do("PUT", "/api/v1/me/meal-selections/"+id, map[string]any{"items": items("00000000-0000-0000-0000-000000000000", 1)}, tok), http.StatusUnprocessableEntity)
	// Saving fewer is fine as a draft, but cannot be confirmed.
	res = h.do("PUT", "/api/v1/me/meal-selections/"+id, map[string]any{"items": items(s.meals[0], 2)}, tok)
	h.wantStatus(res, http.StatusOK)
	sel := obj(t, res.Data["selection"])
	if sel["status"] != "draft" || res.Data["remaining"] != float64(1) {
		t.Fatalf("draft = %v remaining %v", sel, res.Data["remaining"])
	}
	h.wantStatus(h.do("POST", "/api/v1/me/meal-selections/"+id+"/finalize", nil, tok), http.StatusUnprocessableEntity)
	// A stale version from another device is refused.
	h.wantError(h.do("PUT", "/api/v1/me/meal-selections/"+id, map[string]any{"items": items(s.meals[0], 3), "version": 0}, tok), http.StatusConflict, "version_conflict")
	// Confirm the full allowance.
	res = h.do("POST", "/api/v1/me/meal-selections/"+id+"/finalize", map[string]any{"items": items(s.meals[0], 2, s.meals[1], 1)}, tok)
	h.wantStatus(res, http.StatusOK)
	if res.Data["confirmed"] != true {
		t.Fatalf("not confirmed: %v", res.Data)
	}
	// Edit before the cutoff: allowed (it goes back to draft until confirmed again).
	h.clock.Set(at("2026-11-12 23:58"))
	h.wantStatus(h.do("POST", "/api/v1/me/meal-selections/"+id+"/finalize", map[string]any{"items": items(s.meals[1], 3)}, tok), http.StatusOK)
	// After the cutoff: refused by the server, whatever the browser shows.
	h.clock.Set(at("2026-11-12 23:59"))
	h.wantError(h.do("PUT", "/api/v1/me/meal-selections/"+id, map[string]any{"items": items(s.meals[0], 3)}, tok), http.StatusConflict, "selection_closed")
	// Another customer cannot see or change this week.
	other, _ := s.customer("other-chooser@example.com")
	h.wantStatus(h.do("GET", "/api/v1/me/meal-selections/"+id, nil, other), http.StatusNotFound)
	h.wantStatus(h.do("PUT", "/api/v1/me/meal-selections/"+id, map[string]any{"items": items(s.meals[0], 1)}, other), http.StatusNotFound)
	if n := h.count(`SELECT count(*) FROM notification_deliveries WHERE kind = 'selection_confirmed'`); n != 1 {
		t.Errorf("confirmation emails = %d, want 1", n)
	}
}

// ---------------------------------------------------------------- scheduler

func TestCutoffMakesOneOrderAndTheNextWeek(t *testing.T) {
	s := newShop(t)
	h := s.h
	tok, sub := s.active("orders@example.com", testpay.CardApprove)
	id := s.weekCycle(tok, "2026-11-16")["id"].(string)
	h.wantStatus(h.do("POST", "/api/v1/me/meal-selections/"+id+"/finalize", map[string]any{"items": items(s.meals[0], 2, s.fishMeal, 1)}, tok), http.StatusOK)

	rep := s.tick("2026-11-13 00:30")
	if rep.OrdersCreated != 1 || rep.CyclesCreated != 1 {
		t.Fatalf("first tick = %+v", rep)
	}
	// Running again (a retry, or a second worker) changes nothing.
	rep = s.tick("2026-11-13 00:31")
	if rep.OrdersCreated != 0 || rep.CyclesCreated != 0 || rep.Finalized != 0 {
		t.Fatalf("second tick = %+v", rep)
	}
	if n := h.count(`SELECT count(*) FROM orders WHERE subscription_id = $1`, sub); n != 1 {
		t.Fatalf("orders = %d", n)
	}
	if n := h.count(`SELECT count(*) FROM subscription_cycles WHERE subscription_id = $1`, sub); n != 2 {
		t.Fatalf("cycles = %d", n)
	}

	// The order is a snapshot: meal versions, quantities, amounts, address.
	res := h.do("GET", "/api/v1/me/orders", nil, tok)
	h.wantStatus(res, http.StatusOK)
	orders := list(t, res.Data["items"])
	if len(orders) != 1 {
		t.Fatalf("orders = %v", orders)
	}
	oid := obj(t, orders[0])["id"].(string)
	res = h.do("GET", "/api/v1/me/orders/"+oid, nil, tok)
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != "confirmed" || res.Data["meals_count"] != float64(3) || res.Data["total_minor"] != float64(33000) {
		t.Errorf("order = %v", res.Data)
	}
	if obj(t, res.Data["address"])["line1"] != "12 Ariapita Avenue" {
		t.Errorf("address snapshot = %v", res.Data["address"])
	}
	for _, it := range list(t, res.Data["items"]) {
		if obj(t, it)["meal_version"].(float64) < 1 {
			t.Errorf("item without a meal version: %v", it)
		}
	}
	// Editing the meal or the address later does not change the order.
	if _, err := h.pool.Exec(context.Background(), `UPDATE customer_addresses SET line1 = 'Moved'`); err != nil {
		t.Fatal(err)
	}
	res = h.do("GET", "/api/v1/me/orders/"+oid, nil, tok)
	if obj(t, res.Data["address"])["line1"] != "12 Ariapita Avenue" {
		t.Error("the order's address changed with the customer's")
	}
	// Another customer cannot see the order.
	other, _ := s.customer("orders-other@example.com")
	h.wantStatus(h.do("GET", "/api/v1/me/orders/"+oid, nil, other), http.StatusNotFound)

	// Staff see the week's demand per meal version, for later production modules.
	res = h.do("GET", "/api/v1/admin/demand?week=2026-11-16", nil, s.admin)
	h.wantStatus(res, http.StatusOK)
	if res.Data["kits"] != float64(3) || res.Data["orders"] != float64(1) || res.Data["status"] != "final" {
		t.Errorf("demand = %v", res.Data)
	}
}

func TestMissedSelectionNeedsResolutionAndUnpaidWeekMakesNoOrder(t *testing.T) {
	s := newShop(t)
	h := s.h
	// Paid but never confirmed: no meals are invented.
	tok, _ := s.active("forgetful@example.com", testpay.CardApprove)
	// This customer's saved card declines later charges.
	tok2, _ := s.active("never-pays@example.com", testpay.CardDecline)
	rep := s.tick("2026-11-13 00:30")
	if rep.NeedsResolution != 2 || rep.OrdersCreated != 0 {
		t.Fatalf("tick = %+v", rep)
	}
	if c := s.weekCycle(tok, "2026-11-16"); c["state"] != "needs_resolution" {
		t.Errorf("cycle = %v", c["state"])
	}

	// A renewal that is never paid: no order, the invoice is voided.
	rep = s.tick("2026-11-18 00:30") // the renewal charge declines
	v := s.subscription(tok2)
	if v["status"] != "past_due" {
		t.Fatalf("after declined renewal = %v", v["status"])
	}
	if n := h.count(`SELECT count(*) FROM notification_deliveries WHERE kind = 'payment_failed'`); n != 1 {
		t.Errorf("payment failed emails = %d", n)
	}
	rep = s.tick("2026-11-20 00:30")
	if rep.Unpaid != 1 {
		t.Fatalf("cutoff tick = %+v", rep)
	}
	if c := s.weekCycle(tok2, "2026-11-23"); c["state"] != "unpaid" {
		t.Errorf("unpaid week = %v", c["state"])
	}
	if n := h.count(`SELECT count(*) FROM invoices i JOIN billing_cycles b ON b.id = i.billing_cycle_id WHERE b.kind = 'renewal' AND i.status = 'void'`); n != 1 {
		t.Errorf("void renewal invoices = %d", n)
	}
	if n := h.count(`SELECT count(*) FROM orders WHERE week_start = '2026-11-23'`); n != 0 {
		t.Errorf("orders for an unpaid week = %d", n)
	}
}

func TestRenewalIsChargedOnceAndFailedRenewalCanBePaid(t *testing.T) {
	s := newShop(t)
	h := s.h
	tok, sub := s.active("renews@example.com", testpay.CardApprove)
	tok2, _ := s.active("recovers@example.com", testpay.CardDecline)
	s.tick("2026-11-13 00:30") // week of 23 Nov created
	rep := s.tick("2026-11-18 00:30")
	if rep.Renewals != 2 {
		t.Fatalf("renewal tick = %+v", rep)
	}
	s.tick("2026-11-18 00:31")
	if n := h.count(`SELECT count(*) FROM payments WHERE customer_id = (SELECT customer_id FROM subscriptions WHERE id = $1) AND kind = 'off_session'`, sub); n != 1 {
		t.Fatalf("renewal charges = %d, want exactly 1", n)
	}
	if c := s.weekCycle(tok, "2026-11-23"); obj(t, c["billing"])["status"] != "paid" {
		t.Fatalf("renewal billing = %v", c["billing"])
	}

	// A card that declines: past due, then paid by the customer before the cutoff.
	v := s.subscription(tok2)
	if v["status"] != "past_due" {
		t.Fatalf("status = %v", v["status"])
	}
	bill := obj(t, obj(t, v["outstanding"])["billing"])
	res := h.do("POST", "/api/v1/checkout/sessions", map[string]any{"billing_cycle_id": bill["id"]}, tok2)
	h.wantStatus(res, http.StatusCreated)
	redirect := res.Data["redirect_url"].(string)
	s.pay(redirect[strings.LastIndex(redirect, "/")+1:], "approve", testpay.CardApprove)
	h.drainJobs()
	if st := s.subscription(tok2)["status"]; st != "active" {
		t.Fatalf("after paying = %v", st)
	}
	// After the cutoff the week can no longer be paid.
	h.clock.Set(at("2026-11-20 00:00"))
	h.wantStatus(h.do("POST", "/api/v1/checkout/sessions", map[string]any{"billing_cycle_id": bill["id"]}, tok2), http.StatusConflict)
}

// ---------------------------------------------------------------- manage

func TestPlanChangePauseResumeAndCancel(t *testing.T) {
	s := newShop(t)
	h := s.h
	tok, sub := s.active("manager@example.com", testpay.CardApprove)
	s.tick("2026-11-13 00:30") // week of 23 Nov exists, not billed yet

	// Change plan: the unbilled week of 23 Nov takes the new plan; the paid
	// week of 16 Nov keeps the old one.
	res := h.do("POST", "/api/v1/me/subscriptions/"+sub+"/change-plan", map[string]any{"plan_id": s.family}, tok)
	h.wantStatus(res, http.StatusOK)
	if s.weekCycle(tok, "2026-11-23")["plan"].(map[string]any)["meals_per_week"] != float64(4) {
		t.Error("unbilled week did not take the new plan")
	}
	if s.weekCycle(tok, "2026-11-16")["plan"].(map[string]any)["meals_per_week"] != float64(3) {
		t.Error("finalised week changed plan")
	}
	h.wantError(h.do("POST", "/api/v1/me/subscriptions/"+sub+"/change-plan", map[string]any{"plan_id": s.family}, tok), http.StatusConflict, "same_plan")

	// Pause two weeks: starts with the unpaid week of 23 Nov.
	res = h.do("POST", "/api/v1/me/subscriptions/"+sub+"/pause", map[string]any{"weeks": 2}, tok)
	h.wantStatus(res, http.StatusOK)
	v := obj(t, res.Data["subscription"])
	if v["status"] != "paused" || obj(t, v["pause"])["from_week"] != "2026-11-23" || obj(t, v["pause"])["until_week"] != "2026-12-07" {
		t.Fatalf("paused = %v", v)
	}
	h.wantStatus(h.do("POST", "/api/v1/me/subscriptions/"+sub+"/pause", map[string]any{"weeks": 9}, tok), http.StatusConflict)
	// Nothing is billed for a paused week.
	if rep := s.tick("2026-11-18 00:30"); rep.Renewals != 0 {
		t.Errorf("billed a paused week: %+v", rep)
	}
	// Resume early: the week of 23 Nov comes back and is billed right away
	// (its renewal time has passed).
	h.wantStatus(h.do("POST", "/api/v1/me/subscriptions/"+sub+"/resume", nil, tok), http.StatusOK)
	if rep := s.tick("2026-11-18 00:35"); rep.Renewals != 1 {
		t.Errorf("resumed week not billed: %+v", rep)
	}

	// Cancel: the week of 23 Nov is paid, so it is still delivered.
	res = h.do("POST", "/api/v1/me/subscriptions/"+sub+"/cancel", map[string]any{"reason": "moving"}, tok)
	h.wantStatus(res, http.StatusOK)
	v = obj(t, res.Data["subscription"])
	if v["status"] != "cancel_scheduled" || obj(t, v["cancel"])["effective_week"] != "2026-11-30" {
		t.Fatalf("cancel = %v", v)
	}
	s.tick("2026-11-20 00:30") // week of 23 Nov closes (needs resolution: no meals chosen); no week of 30 Nov
	if n := h.count(`SELECT count(*) FROM subscription_cycles WHERE subscription_id = $1 AND week_start = '2026-11-30'`, sub); n != 0 {
		t.Errorf("a week was created after the cancellation")
	}
	s.tick("2026-11-20 00:31")
	if n := h.count(`SELECT count(*) FROM subscriptions WHERE id = $1 AND status = 'cancelled'`, sub); n != 1 {
		t.Error("subscription not cancelled after its last paid week")
	}
	// History stays: payments, invoices, events.
	if n := h.count(`SELECT count(*) FROM invoices WHERE subscription_id = $1`, sub); n != 2 {
		t.Errorf("invoices after cancelling = %d", n)
	}
	if n := h.count(`SELECT count(*) FROM subscription_events WHERE subscription_id = $1`, sub); n < 6 {
		t.Errorf("events = %d", n)
	}
	// A cancelled subscription cannot be paused or changed.
	h.wantStatus(h.do("POST", "/api/v1/me/subscriptions/"+sub+"/pause", map[string]any{"weeks": 1}, tok), http.StatusConflict)
}

func TestCancellingAnUnpaidWeekTakesEffectAtOnce(t *testing.T) {
	s := newShop(t)
	h := s.h
	tok, sub := s.active("quick-cancel@example.com", testpay.CardApprove)
	s.tick("2026-11-13 00:30")
	res := h.do("POST", "/api/v1/me/subscriptions/"+sub+"/cancel", nil, tok)
	h.wantStatus(res, http.StatusOK)
	if st := obj(t, res.Data["subscription"])["status"]; st != "cancelled" {
		t.Fatalf("status = %v (no paid week remained)", st)
	}
	if s.subscription(tok) != nil {
		t.Error("a cancelled subscription is still current")
	}
}

// ---------------------------------------------------------------- billing, invoices, refunds, promotions

func TestInvoicePDFAndOwnership(t *testing.T) {
	s := newShop(t)
	h := s.h
	tok, _ := s.active("invoiced@example.com", testpay.CardApprove)
	res := h.do("GET", "/api/v1/me/invoices", nil, tok)
	h.wantStatus(res, http.StatusOK)
	inv := obj(t, list(t, res.Data["items"])[0])
	id := inv["id"].(string)
	res = h.do("GET", "/api/v1/me/invoices/"+id, nil, tok)
	h.wantStatus(res, http.StatusOK)
	if res.Data["total_minor"] != float64(33000) || len(list(t, res.Data["lines"])) != 1 || !strings.Contains(res.Data["tax_note"].(string), "No tax") {
		t.Errorf("invoice = %v", res.Data)
	}
	pdf := h.do("GET", "/api/v1/me/invoices/"+id+"/pdf", nil, tok)
	h.wantStatus(pdf, http.StatusOK)
	if pdf.Header.Get("Content-Type") != "application/pdf" || !strings.HasPrefix(string(pdf.Raw), "%PDF-") {
		t.Errorf("pdf: %s", pdf.Header.Get("Content-Type"))
	}
	other, _ := s.customer("invoice-thief@example.com")
	h.wantStatus(h.do("GET", "/api/v1/me/invoices/"+id, nil, other), http.StatusNotFound)
	h.wantStatus(h.do("GET", "/api/v1/me/invoices/"+id+"/pdf", nil, other), http.StatusNotFound)
	res = h.do("GET", "/api/v1/me/billing", nil, tok)
	h.wantStatus(res, http.StatusOK)
	if len(list(t, res.Data["history"])) != 1 || res.Data["payment_method"] != "Test card" {
		t.Errorf("billing = %v", res.Data)
	}
}

func TestRefundsAreRecordedOnceAndNeedPermission(t *testing.T) {
	s := newShop(t)
	h := s.h
	s.active("refund-me@example.com", testpay.CardApprove)
	var paymentID string
	if err := h.pool.QueryRow(context.Background(), `SELECT id FROM payments WHERE status = 'succeeded'`).Scan(&paymentID); err != nil {
		t.Fatal(err)
	}
	support := bearer(h.adminLogin("support@mise.test", "support"))
	finance := bearer(h.adminLogin("finance@mise.test", "finance"))
	refund := map[string]any{"amount_minor": 10000, "reason": "Missing ingredient", "request_key": "form-1234567"}
	h.wantStatus(h.do("GET", "/api/v1/admin/payments/"+paymentID, nil, support), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/payments/"+paymentID+"/refunds", refund, support), http.StatusForbidden)

	res := h.do("POST", "/api/v1/admin/payments/"+paymentID+"/refunds", refund, finance)
	h.wantStatus(res, http.StatusCreated)
	if res.Data["status"] != "partially_refunded" || res.Data["refunded_minor"] != float64(10000) {
		t.Fatalf("after refund = %v", res.Data)
	}
	// The same form submitted twice is one refund.
	h.wantStatus(h.do("POST", "/api/v1/admin/payments/"+paymentID+"/refunds", refund, finance), http.StatusCreated)
	if n := h.count(`SELECT count(*) FROM refunds WHERE status = 'succeeded'`); n != 1 {
		t.Errorf("refunds = %d", n)
	}
	// More than is left is refused.
	h.wantStatus(h.do("POST", "/api/v1/admin/payments/"+paymentID+"/refunds",
		map[string]any{"amount_minor": 30000, "reason": "Too much", "request_key": "form-7654321"}, finance), http.StatusUnprocessableEntity)
	// Payment statuses cannot be set by hand: there is no endpoint for it,
	// and the customer is not touched by the refund (no automatic cancellation).
	if n := h.count(`SELECT count(*) FROM subscriptions WHERE status = 'active'`); n != 1 {
		t.Error("refund changed the subscription")
	}
}

func TestPromotionsAreApprovedAndComputedOnTheServer(t *testing.T) {
	s := newShop(t)
	h := s.h
	finance := bearer(h.adminLogin("promos@mise.test", "finance"))
	res := h.do("POST", "/api/v1/admin/promotions", map[string]any{"code": "welcome15", "name": "Welcome", "kind": "percent", "percent_bps": 1500, "cycles": 1, "max_redemptions": 1}, finance)
	h.wantStatus(res, http.StatusCreated)
	promo := res.Data["id"].(string)
	tok, addr := s.customer("promo@example.com")
	// A draft code does not work.
	h.wantStatus(h.do("POST", "/api/v1/me/subscriptions/quote", map[string]any{"plan_id": s.basic, "promotion_code": "WELCOME15"}, tok), http.StatusUnprocessableEntity)
	h.wantStatus(h.do("POST", "/api/v1/admin/promotions/"+promo+"/activate", nil, finance), http.StatusOK)
	res = h.do("POST", "/api/v1/me/subscriptions/quote", map[string]any{"plan_id": s.basic, "promotion_code": " welcome15 "}, tok)
	h.wantStatus(res, http.StatusOK)
	due := obj(t, res.Data["due_now"])
	if due["discount_minor"] != float64(4950) || due["total_minor"] != float64(28050) {
		t.Fatalf("discounted quote = %v", due)
	}
	if obj(t, res.Data["renewal"])["total_minor"] != float64(33000) {
		t.Error("a first-week code changed the renewal price")
	}
	sub, ref := s.subscribe(tok, addr, s.basic, map[string]any{"promotion_code": "WELCOME15"})
	s.pay(ref, "approve", testpay.CardApprove)
	h.drainJobs()
	var total int64
	if err := h.pool.QueryRow(context.Background(), `SELECT total_minor FROM invoices WHERE subscription_id = $1`, sub).Scan(&total); err != nil || total != 28050 {
		t.Errorf("invoice total = %d (%v)", total, err)
	}
	// Limited to one use.
	tok2, _ := s.customer("promo-two@example.com")
	h.wantStatus(h.do("POST", "/api/v1/me/subscriptions/quote", map[string]any{"plan_id": s.basic, "promotion_code": "WELCOME15"}, tok2), http.StatusUnprocessableEntity)
	// The renewal is charged at full price.
	s.tick("2026-11-13 00:30")
	s.tick("2026-11-18 00:30")
	if n := h.count(`SELECT count(*) FROM billing_cycles WHERE subscription_id = $1 AND kind = 'renewal' AND total_minor = 33000`, sub); n != 1 {
		t.Errorf("renewal not at full price")
	}
}

// ---------------------------------------------------------------- access

func TestCommerceAdministrationIsProtected(t *testing.T) {
	s := newShop(t)
	h := s.h
	tok, _ := s.customer("not-staff@example.com")
	for _, path := range []string{"/api/v1/admin/subscriptions", "/api/v1/admin/orders", "/api/v1/admin/payments", "/api/v1/admin/menus", "/api/v1/admin/plans", "/api/v1/admin/promotions"} {
		h.wantStatus(h.do("GET", path, nil), http.StatusUnauthorized)
		h.wantStatus(h.do("GET", path, nil, tok), http.StatusUnauthorized)
	}
	marketing := bearer(h.adminLogin("menus@mise.test", "marketing"))
	res := h.do("POST", "/api/v1/admin/menus", map[string]any{"week": "2026-11-30", "meals": []map[string]any{{"meal_id": s.meals[0]}}}, marketing)
	h.wantStatus(res, http.StatusCreated)
	h.wantStatus(h.do("POST", "/api/v1/admin/menus/"+res.Data["id"].(string)+"/publish", nil, marketing), http.StatusForbidden)
	h.wantStatus(h.do("GET", "/api/v1/admin/payments", nil, marketing), http.StatusForbidden)
	ops := bearer(h.adminLogin("ops@mise.test", "operations"))
	h.wantStatus(h.do("GET", "/api/v1/admin/orders", nil, ops), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/plans", map[string]any{}, ops), http.StatusForbidden)

	// A published menu with choices cannot be unpublished; one meal chosen cannot be removed.
	ctok, _ := s.active("locks-menu@example.com", testpay.CardApprove)
	id := s.weekCycle(ctok, "2026-11-16")["id"].(string)
	h.wantStatus(h.do("PUT", "/api/v1/me/meal-selections/"+id, map[string]any{"items": items(s.meals[0], 1)}, ctok), http.StatusOK)
	var menuID string
	if err := h.pool.QueryRow(context.Background(), `SELECT id FROM weekly_menus WHERE week_start = '2026-11-16'`).Scan(&menuID); err != nil {
		t.Fatal(err)
	}
	h.wantError(h.do("POST", "/api/v1/admin/menus/"+menuID+"/unpublish", nil, s.admin), http.StatusConflict, "menu_in_use")
	// Unpublished menus are not visible to customers.
	h.wantStatus(h.do("GET", "/api/v1/weekly-menus/2026-11-30", nil), http.StatusNotFound)
	h.wantStatus(h.do("GET", "/api/v1/weekly-menus/2026-11-16", nil), http.StatusOK)
}

func TestAddressUsedBySubscriptionCannotBeRemoved(t *testing.T) {
	s := newShop(t)
	h := s.h
	tok, addr := s.customer("keeps-address@example.com")
	sub, ref := s.subscribe(tok, addr, s.basic, nil)
	s.pay(ref, "approve", testpay.CardApprove)
	h.drainJobs()
	h.wantError(h.do("DELETE", "/api/v1/me/addresses/"+addr, nil, tok), http.StatusConflict, "address_in_use")
	_ = sub
}

// Starting checkout again (a second tab, a double click, two requests at
// once) replaces the unfinished subscription. Its checkout is closed at the
// provider too, so the abandoned one cannot still take the customer's money;
// only the newest can be paid, and the subscription activates once.
func TestReplacedCheckoutCannotBePaidAtTheProvider(t *testing.T) {
	s := newShop(t)
	h := s.h
	tok, addr := s.customer("two-tabs@example.com")
	_, first := s.subscribe(tok, addr, s.basic, nil)
	_, second := s.subscribe(tok, addr, s.basic, nil)
	if first == second {
		t.Fatal("starting again reused the same checkout")
	}
	h.drainJobs() // the provider is told to close the abandoned checkout by a queued job
	var status string
	if err := h.pool.QueryRow(context.Background(), `SELECT status FROM testpay_sessions WHERE ref = $1`, first).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" {
		t.Fatalf("the replaced checkout is %q at the provider, want cancelled", status)
	}
	// Paying the old tab does nothing; paying the new one activates the subscription.
	s.pay(first, "approve", testpay.CardApprove)
	s.pay(second, "approve", testpay.CardApprove)
	h.drainJobs()
	if st := s.subscription(tok)["status"]; st != "active" {
		t.Fatalf("status = %v", st)
	}
	if n := h.count(`SELECT count(*) FROM payments WHERE status = 'succeeded'`); n != 1 {
		t.Fatalf("%d succeeded payments, want 1", n)
	}
	if n := h.count(`SELECT count(*) FROM payments WHERE needs_attention`); n != 0 {
		t.Fatalf("%d payments need attention, want 0", n)
	}
}
