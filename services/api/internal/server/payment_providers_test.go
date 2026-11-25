package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"mise.tt/api/internal/payments/stripe"
	"mise.tt/api/internal/payments/testpay"
	"mise.tt/api/internal/platform/config"
	"mise.tt/api/internal/server"
)

// Sprint 9.1 integration tests: several payment providers behind the one
// payments module. Stripe runs against a local server that follows Stripe's
// documented requests and responses (no sandbox credentials exist); PayPal is
// configured for USD only, as it cannot charge TTD; Scotiabank is blocked.

// fakeStripeAPI keeps Checkout Sessions in memory. Tests set what a
// session's payment intent looks like when the adapter reads it.
type fakeStripeAPI struct {
	mu       sync.Mutex
	next     int
	sessions map[string]*fakeSession
	expired  []string
}

type fakeSession struct {
	status string // open, complete, expired
	amount int64
	intent string // JSON of the payment intent, "" for none yet
}

func (f *fakeStripeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	path := r.URL.Path
	switch {
	case r.Method == http.MethodPost && path == "/v1/checkout/sessions":
		_ = r.ParseForm()
		var amount int64
		fmt.Sscan(r.PostForm.Get("line_items[0][price_data][unit_amount]"), &amount)
		f.next++
		id := fmt.Sprintf("cs_test_%d", f.next)
		f.sessions[id] = &fakeSession{status: "open", amount: amount}
		fmt.Fprintf(w, `{"id":%q,"url":"https://checkout.stripe.test/c/pay/%s","expires_at":%d}`, id, id, time.Now().Add(time.Hour).Unix())
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/v1/checkout/sessions/"):
		s := f.sessions[strings.TrimPrefix(path, "/v1/checkout/sessions/")]
		if s == nil {
			w.WriteHeader(404)
			fmt.Fprint(w, `{"error":{"type":"invalid_request_error","code":"resource_missing","message":"No such session"}}`)
			return
		}
		intent := "null"
		if s.intent != "" {
			intent = s.intent
		}
		fmt.Fprintf(w, `{"id":"x","status":%q,"amount_total":%d,"currency":"ttd","payment_intent":%s}`, s.status, s.amount, intent)
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/expire"):
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/v1/checkout/sessions/"), "/expire")
		s := f.sessions[id]
		if s == nil || s.status != "open" {
			w.WriteHeader(400)
			fmt.Fprint(w, `{"error":{"type":"invalid_request_error","code":"checkout_session_not_open","message":"Session is already complete"}}`)
			return
		}
		s.status = "expired"
		f.expired = append(f.expired, id)
		fmt.Fprintf(w, `{"id":%q,"status":"expired"}`, id)
	default:
		w.WriteHeader(404)
		fmt.Fprint(w, `{"error":{"type":"invalid_request_error","code":"resource_missing","message":"unknown"}}`)
	}
}

func (f *fakeStripeAPI) set(id string, fn func(*fakeSession)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f.sessions[id])
}

const stripeWebhookSecret = "whsec_test_mise"

func newProviderShop(t *testing.T) (*shop, *fakeStripeAPI) {
	t.Helper()
	fake := &fakeStripeAPI{sessions: map[string]*fakeSession{}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	s := newShop(t, func(c *config.Config, _ *server.Limits) {
		// TTD is set here only to exercise the flow: Stripe's TTD support is
		// unconfirmed, and production offers only confirmed currencies.
		c.Commerce.Stripe = config.StripeConfig{SecretKey: "sk_test_mise_0000abcd", WebhookSecret: stripeWebhookSecret,
			Currencies: []string{"TTD"}, APIBase: srv.URL}
		c.Commerce.PayPal = config.PayPalConfig{ClientID: "paypal-client-9876", ClientSecret: "secret", Env: "sandbox", WebhookID: "WH",
			Currencies: []string{"USD"}, APIBase: srv.URL}
	})
	return s, fake
}

func optionNames(t *testing.T, h *harness, currency string) []string {
	t.Helper()
	res := h.do("GET", "/api/v1/payment-options?currency="+currency, nil)
	h.wantStatus(res, http.StatusOK)
	var names []string
	for _, o := range list(t, res.Data["items"]) {
		names = append(names, obj(t, o)["provider"].(string))
	}
	return names
}

func TestPaymentOptionsAndProviderSettings(t *testing.T) {
	s, _ := newProviderShop(t)
	h := s.h
	super := bearer(h.adminLogin("owner@mise.test", "super_admin"))
	admin := bearer(h.adminLogin("ops-admin@mise.test", "admin"))
	finance := bearer(h.adminLogin("finance@mise.test", "finance"))
	support := bearer(h.adminLogin("support@mise.test", "support"))

	// As shipped: only the simulator is enabled.
	if got := optionNames(t, h, "TTD"); strings.Join(got, ",") != "testpay" {
		t.Fatalf("TTD options = %v", got)
	}
	h.wantStatus(h.do("GET", "/api/v1/payment-options?currency=ttd", nil), http.StatusUnprocessableEntity)

	// Permissions: support cannot see providers; finance can read but not change; admin cannot go live.
	h.wantStatus(h.do("GET", "/api/v1/admin/payment-providers", nil, support), http.StatusForbidden)
	h.wantStatus(h.do("GET", "/api/v1/admin/payment-providers", nil, finance), http.StatusOK)
	h.wantStatus(h.do("PATCH", "/api/v1/admin/payment-providers/stripe", map[string]any{"enabled": true}, finance), http.StatusForbidden)
	h.wantStatus(h.do("POST", "/api/v1/admin/payment-providers/stripe/mode", map[string]any{"mode": "live", "reference": "x"}, admin), http.StatusForbidden)

	// The admin list shows every provider, with masked credentials and why it is not taking payments.
	res := h.do("GET", "/api/v1/admin/payment-providers", nil, admin)
	h.wantStatus(res, http.StatusOK)
	byName := map[string]map[string]any{}
	for _, it := range list(t, res.Data["items"]) {
		byName[obj(t, it)["provider"].(string)] = obj(t, it)
	}
	for _, name := range []string{"testpay", "stripe", "paypal", "scotiabank"} {
		if byName[name] == nil {
			t.Fatalf("provider %s missing from %v", name, byName)
		}
	}
	raw, _ := json.Marshal(res.Data)
	if strings.Contains(string(raw), "sk_test_mise_0000abcd") || strings.Contains(string(raw), stripeWebhookSecret) {
		t.Fatal("a secret appears in the admin response")
	}
	if byName["stripe"]["credential_ref"] != "sk_test_…abcd" || byName["stripe"]["taking_payments"] != false ||
		byName["stripe"]["not_taking_reason"] != "Not enabled by staff." {
		t.Fatalf("stripe status = %v", byName["stripe"])
	}
	if byName["scotiabank"]["ready"] != false || !strings.Contains(byName["scotiabank"]["readiness_reason"].(string), "Blocked") {
		t.Fatalf("scotiabank status = %v", byName["scotiabank"])
	}

	// Scotiabank cannot be enabled: it has no integration specification.
	res = h.do("PATCH", "/api/v1/admin/payment-providers/scotiabank", map[string]any{"enabled": true}, admin)
	h.wantStatus(res, http.StatusConflict)
	h.wantStatus(h.do("PATCH", "/api/v1/admin/payment-providers/nope", map[string]any{"enabled": true}, admin), http.StatusNotFound)
	h.wantStatus(h.do("PATCH", "/api/v1/admin/payment-providers/stripe", map[string]any{"merchant_status": "approved-ish"}, admin), http.StatusUnprocessableEntity)

	// Enabling Stripe (test mode) offers it for TTD; PayPal enabled is offered for USD only, never TTD.
	h.wantStatus(h.do("PATCH", "/api/v1/admin/payment-providers/stripe", map[string]any{"enabled": true, "note": "sandbox trial"}, admin), http.StatusOK)
	h.wantStatus(h.do("PATCH", "/api/v1/admin/payment-providers/paypal", map[string]any{"enabled": true}, admin), http.StatusOK)
	if got := optionNames(t, h, "TTD"); strings.Join(got, ",") != "testpay,stripe" {
		t.Fatalf("TTD options = %v", got)
	}
	if got := optionNames(t, h, "USD"); strings.Join(got, ",") != "testpay,paypal" {
		t.Fatalf("USD options = %v", got)
	}
	if got := optionNames(t, h, "EUR"); len(got) != 0 {
		t.Fatalf("EUR options = %v", got)
	}

	// Live mode is refused with test credentials, even for the owner, and the merchant must be verified.
	res = h.do("POST", "/api/v1/admin/payment-providers/stripe/mode", map[string]any{"mode": "live", "reference": "Board minute 12"}, super)
	h.wantStatus(res, http.StatusConflict)
	if res.Error.Code != "test_credentials" {
		t.Fatalf("live refusal = %s", res.Raw)
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/payment-providers/stripe/mode", map[string]any{"mode": "sideways"}, super), http.StatusUnprocessableEntity)

	// Every change is in the append-only provider audit trail and the general audit log.
	h.wantStatus(h.do("PATCH", "/api/v1/admin/payment-providers/stripe", map[string]any{"merchant_status": "applied", "merchant_reference": "acct ref 1"}, admin), http.StatusOK)
	h.wantStatus(h.do("PATCH", "/api/v1/admin/payment-providers/stripe", map[string]any{"enabled": false}, admin), http.StatusOK)
	ctx := context.Background()
	var actions string
	if err := h.pool.QueryRow(ctx, `SELECT string_agg(action || ':' || to_value, ',' ORDER BY id) FROM payment_provider_events WHERE provider = 'stripe'`).Scan(&actions); err != nil {
		t.Fatal(err)
	}
	if actions != "enabled:enabled,merchant_status:applied,merchant_reference:acct ref 1,disabled:disabled" {
		t.Fatalf("provider events = %s", actions)
	}
	var audited int
	_ = h.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action LIKE 'payment_provider.%' AND target_id = 'stripe'`).Scan(&audited)
	if audited != 4 {
		t.Fatalf("audit entries = %d, want 4", audited)
	}
	if _, err := h.pool.Exec(ctx, `UPDATE payment_provider_events SET note = 'edited'`); err == nil {
		t.Fatal("provider events can be edited")
	}
	if _, err := h.pool.Exec(ctx, `UPDATE payment_provider_settings SET mode = 'live' WHERE provider = 'stripe'`); err == nil {
		t.Fatal("live mode without a recorded approval was stored")
	}
	if got := optionNames(t, h, "TTD"); strings.Join(got, ",") != "testpay" {
		t.Fatalf("after disabling Stripe, TTD options = %v", got)
	}
}

// declinedBill subscribes with the simulator, declines the first payment and
// returns the customer and the bill that is now open for another attempt.
func declinedBill(t *testing.T, s *shop, email string) (reqOpt, string) {
	t.Helper()
	tok, addr := s.customer(email)
	_, ref := s.subscribe(tok, addr, s.basic, nil)
	s.pay(ref, "decline", testpay.CardApprove)
	s.h.drainJobs()
	bill := obj(t, obj(t, s.subscription(tok)["outstanding"])["billing"])
	return tok, bill["id"].(string)
}

func stripeWebhook(t *testing.T, h *harness, eventID, sessionID string) response {
	t.Helper()
	body := []byte(fmt.Sprintf(`{"id":%q,"type":"checkout.session.completed","data":{"object":{"id":%q,"object":"checkout.session"}}}`, eventID, sessionID))
	hdr := http.Header{}
	hdr.Set("Stripe-Signature", stripe.SignForTest(stripeWebhookSecret, body, time.Now()))
	hdr.Set("Content-Type", "application/json")
	return h.raw("POST", "/api/v1/webhooks/payments/stripe", body, hdr)
}

func TestCheckoutWithChosenProvider(t *testing.T) {
	s, fake := newProviderShop(t)
	h := s.h
	ctx := context.Background()
	admin := bearer(h.adminLogin("ops-admin@mise.test", "admin"))
	tok, bill := declinedBill(t, s, "chooser@example.com")

	// A disabled or unknown provider is refused server-side, whatever the browser sends.
	for _, p := range []string{"stripe", "scotiabank", "paypal", "bogus"} {
		res := h.do("POST", "/api/v1/checkout/sessions", map[string]any{"billing_cycle_id": bill, "provider": p}, tok)
		h.wantStatus(res, http.StatusUnprocessableEntity)
	}
	h.wantStatus(h.do("PATCH", "/api/v1/admin/payment-providers/stripe", map[string]any{"enabled": true}, admin), http.StatusOK)

	// Pay with Stripe: the customer is sent to Stripe's hosted page.
	res := h.do("POST", "/api/v1/checkout/sessions", map[string]any{"billing_cycle_id": bill, "provider": "stripe"}, tok)
	h.wantStatus(res, http.StatusCreated)
	if !strings.HasPrefix(res.Data["redirect_url"].(string), "https://checkout.stripe.test/") {
		t.Fatalf("redirect = %v", res.Data["redirect_url"])
	}
	first := "cs_test_1"

	// Switching to the simulator while the Stripe page is still unpaid closes it at Stripe first.
	res = h.do("POST", "/api/v1/checkout/sessions", map[string]any{"billing_cycle_id": bill, "provider": "testpay"}, tok)
	h.wantStatus(res, http.StatusCreated)
	if len(fake.expired) != 1 || fake.expired[0] != first {
		t.Fatalf("expired at Stripe = %v", fake.expired)
	}
	var st string
	_ = h.pool.QueryRow(ctx, `SELECT status FROM checkout_sessions WHERE provider = 'stripe' AND provider_ref = $1`, first).Scan(&st)
	if st != "cancelled" {
		t.Fatalf("Stripe checkout = %s, want cancelled", st)
	}

	// Back to Stripe; this time the payment is still processing there when the customer tries the
	// simulator: refused, nothing is retried elsewhere while the outcome is open.
	res = h.do("POST", "/api/v1/checkout/sessions", map[string]any{"billing_cycle_id": bill, "provider": "stripe"}, tok)
	h.wantStatus(res, http.StatusCreated)
	second := "cs_test_2"
	fake.set(second, func(fs *fakeSession) {
		fs.status = "complete"
		fs.intent = `{"id":"pi_2","status":"processing","amount":33000,"currency":"ttd"}`
	})
	res = h.do("POST", "/api/v1/checkout/sessions", map[string]any{"billing_cycle_id": bill, "provider": "testpay"}, tok)
	h.wantStatus(res, http.StatusConflict)
	if res.Error.Code != "payment_in_progress" {
		t.Fatalf("conflict = %s", res.Raw)
	}

	// Stripe confirms by signed webhook; the status comes from Stripe, not the event body.
	fake.set(second, func(fs *fakeSession) {
		fs.intent = `{"id":"pi_2","status":"succeeded","amount":33000,"currency":"ttd","customer":"cus_1",
			"payment_method":{"id":"pm_1","card":{"brand":"visa","last4":"4242"}}}`
	})
	h.wantStatus(stripeWebhook(t, h, "evt_ok", second), http.StatusOK)
	h.drainJobs()
	if v := s.subscription(tok); v["status"] != "active" {
		t.Fatalf("status = %v", v["status"])
	}
	var billingProvider, authority string
	if err := h.pool.QueryRow(ctx, `SELECT coalesce(billing_provider, ''), billing_authority FROM subscriptions s
		JOIN customers c ON c.id = s.customer_id JOIN users u ON u.id = c.user_id WHERE u.email = 'chooser@example.com'`).Scan(&billingProvider, &authority); err != nil {
		t.Fatal(err)
	}
	if billingProvider != "stripe" || authority != "mise" {
		t.Fatalf("billing owner = %s/%s, want stripe/mise", billingProvider, authority)
	}

	// A replayed webhook changes nothing; a badly signed one is refused.
	h.wantStatus(stripeWebhook(t, h, "evt_ok", second), http.StatusOK)
	var succeeded int
	_ = h.pool.QueryRow(ctx, `SELECT count(*) FROM payments WHERE billing_cycle_id = $1 AND status = 'succeeded'`, bill).Scan(&succeeded)
	if succeeded != 1 {
		t.Fatalf("succeeded payments = %d", succeeded)
	}
	bad := http.Header{}
	bad.Set("Stripe-Signature", stripe.SignForTest("whsec_wrong", []byte(`{}`), time.Now()))
	if r := h.raw("POST", "/api/v1/webhooks/payments/stripe", []byte(`{"id":"evt_x"}`), bad); r.Status < 400 {
		t.Fatalf("bad signature status = %d", r.Status)
	}
}

func TestProviderAmountMismatchIsHeld(t *testing.T) {
	s, fake := newProviderShop(t)
	h := s.h
	admin := bearer(h.adminLogin("ops-admin@mise.test", "admin"))
	h.wantStatus(h.do("PATCH", "/api/v1/admin/payment-providers/stripe", map[string]any{"enabled": true}, admin), http.StatusOK)
	tok, bill := declinedBill(t, s, "mismatch@example.com")
	h.wantStatus(h.do("POST", "/api/v1/checkout/sessions", map[string]any{"billing_cycle_id": bill, "provider": "stripe"}, tok), http.StatusCreated)
	// Stripe reports less than the bill: recorded and flagged, never applied.
	fake.set("cs_test_1", func(fs *fakeSession) {
		fs.status = "complete"
		fs.intent = `{"id":"pi_9","status":"succeeded","amount":100,"currency":"ttd"}`
	})
	h.wantStatus(stripeWebhook(t, h, "evt_short", "cs_test_1"), http.StatusOK)
	h.drainJobs()
	if v := s.subscription(tok); v["status"] == "active" {
		t.Fatal("a short payment activated the subscription")
	}
	var attention bool
	var reason string
	if err := h.pool.QueryRow(context.Background(), `SELECT needs_attention, coalesce(attention_reason, '') FROM payments
		WHERE billing_cycle_id = $1 AND provider = 'stripe'`, bill).Scan(&attention, &reason); err != nil {
		t.Fatal(err)
	}
	if !attention || !strings.Contains(reason, "100 TTD") {
		t.Fatalf("flag = %v %q", attention, reason)
	}
}
