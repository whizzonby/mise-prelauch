package stripe

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"mise.tt/api/internal/payments"
	"mise.tt/api/internal/platform/config"
)

// fakeStripe implements the documented subset of the Stripe API that the
// adapter uses. Responses are keyed by path; requests are recorded.
type fakeStripe struct {
	mu       sync.Mutex
	requests []recorded
	handle   map[string]func(w http.ResponseWriter, form url.Values)
}

type recorded struct {
	Method, Path, Auth, IdemKey, ContentType string
	Form                                     url.Values
}

func (f *fakeStripe) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	form, _ := url.ParseQuery(string(raw))
	f.mu.Lock()
	f.requests = append(f.requests, recorded{r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Idempotency-Key"),
		r.Header.Get("Content-Type"), form})
	h := f.handle[r.Method+" "+r.URL.Path]
	f.mu.Unlock()
	if h == nil {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","code":"resource_missing","message":"No such object"}}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	h(w, form)
}

func (f *fakeStripe) last() recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

func jsonOK(body string) func(http.ResponseWriter, url.Values) {
	return func(w http.ResponseWriter, _ url.Values) { _, _ = w.Write([]byte(body)) }
}

func jsonStatus(code int, body string) func(http.ResponseWriter, url.Values) {
	return func(w http.ResponseWriter, _ url.Values) { w.WriteHeader(code); _, _ = w.Write([]byte(body)) }
}

func newTest(t *testing.T) (*Provider, *fakeStripe) {
	t.Helper()
	fs := &fakeStripe{handle: map[string]func(http.ResponseWriter, url.Values){}}
	srv := httptest.NewServer(fs)
	t.Cleanup(srv.Close)
	p := New(config.StripeConfig{SecretKey: "sk_test_0123456789abcd", WebhookSecret: "whsec_test", Currencies: []string{"USD"},
		APIBase: srv.URL}, srv.Client())
	return p, fs
}

var ctx = context.Background()

func TestReadinessAndCapabilities(t *testing.T) {
	p := New(config.StripeConfig{}, nil)
	if r := p.Readiness(); r.Ready || !strings.Contains(r.Reason, "STRIPE_SECRET_KEY") {
		t.Fatalf("unconfigured: %+v", r)
	}
	p = New(config.StripeConfig{SecretKey: "sk_test_0123456789abcd", WebhookSecret: "whsec_x"}, nil)
	if r := p.Readiness(); r.Ready || !strings.Contains(r.Reason, "STRIPE_CURRENCIES") {
		t.Fatalf("no currencies: %+v", r)
	}
	p, _ = newTest(t)
	r := p.Readiness()
	if !r.Ready || r.CredentialRef != "sk_test_…abcd" || strings.Contains(r.CredentialRef, "0123456789") {
		t.Fatalf("ready/masked: %+v", r)
	}
	if p.Live() {
		t.Fatal("test key reported live")
	}
	c := p.Capabilities()
	if !c.Supports("USD") || c.Supports("TTD") || !c.OffSessionCharges || !c.Refunds {
		t.Fatalf("capabilities: %+v", c)
	}
	if !New(config.StripeConfig{SecretKey: "sk_live_x"}, nil).Live() {
		t.Fatal("live key not reported live")
	}
}

func TestCreateCheckout(t *testing.T) {
	p, fs := newTest(t)
	fs.handle["POST /v1/checkout/sessions"] = jsonOK(`{"id":"cs_test_1","url":"https://checkout.stripe.com/c/pay/cs_test_1","expires_at":1900000000}`)
	cs, err := p.CreateCheckout(ctx, payments.CheckoutRequest{Reference: "chk-1", AmountMinor: 12345, Currency: "USD",
		Description: "Week of 30 Nov", ReturnURL: "https://app.example/subscribe/result", SaveMethod: true})
	if err != nil {
		t.Fatal(err)
	}
	if cs.ProviderRef != "cs_test_1" || cs.RedirectURL == "" || cs.ExpiresAt.Unix() != 1900000000 {
		t.Fatalf("session: %+v", cs)
	}
	req := fs.last()
	if req.Auth != "Bearer sk_test_0123456789abcd" || req.IdemKey != "mise-checkout-chk-1" ||
		req.ContentType != "application/x-www-form-urlencoded" {
		t.Fatalf("headers: %+v", req)
	}
	want := map[string]string{"mode": "payment", "client_reference_id": "chk-1", "line_items[0][price_data][currency]": "usd",
		"line_items[0][price_data][unit_amount]": "12345", "payment_intent_data[setup_future_usage]": "off_session",
		"customer_creation": "always", "metadata[mise_checkout]": "chk-1"}
	for k, v := range want {
		if got := req.Form.Get(k); got != v {
			t.Errorf("form %s = %q, want %q", k, got, v)
		}
	}

	fs.handle["POST /v1/checkout/sessions"] = jsonStatus(400, `{"error":{"type":"invalid_request_error","code":"parameter_invalid","message":"bad currency"}}`)
	if _, err := p.CreateCheckout(ctx, payments.CheckoutRequest{Reference: "chk-2", AmountMinor: 1, Currency: "USD"}); err == nil ||
		!strings.Contains(err.Error(), "parameter_invalid") {
		t.Fatalf("api error not surfaced: %v", err)
	}
	fs.handle["POST /v1/checkout/sessions"] = jsonStatus(401, `not json`)
	if _, err := p.CreateCheckout(ctx, payments.CheckoutRequest{Reference: "chk-3"}); err == nil {
		t.Fatal("bad credentials accepted")
	}
}

func TestCheckoutStatus(t *testing.T) {
	p, fs := newTest(t)
	path := "GET /v1/checkout/sessions/cs_1"
	cases := []struct {
		name, body string
		want       payments.Status
		ref        string
	}{
		{"open, no attempt", `{"id":"cs_1","status":"open","amount_total":500,"currency":"usd","payment_intent":null}`, payments.StatusPending, ""},
		{"expired, no attempt", `{"id":"cs_1","status":"expired","amount_total":500,"currency":"usd"}`, payments.StatusCancelled, ""},
		{"paid", `{"id":"cs_1","status":"complete","payment_intent":{"id":"pi_1","status":"succeeded","amount":500,"currency":"usd",
			"customer":"cus_1","payment_method":{"id":"pm_1","card":{"brand":"visa","last4":"4242"}}}}`, payments.StatusSucceeded, "pi_1"},
		{"declined, can retry", `{"id":"cs_1","status":"open","payment_intent":{"id":"pi_2","status":"requires_payment_method","amount":500,
			"currency":"usd","last_payment_error":{"code":"card_declined","message":"declined"}}}`, payments.StatusFailed, "pi_2"},
		{"unpaid intent, expired", `{"id":"cs_1","status":"expired","payment_intent":{"id":"pi_3","status":"requires_payment_method",
			"amount":500,"currency":"usd"}}`, payments.StatusCancelled, "pi_3"},
		{"processing", `{"id":"cs_1","status":"complete","payment_intent":{"id":"pi_4","status":"processing","amount":500,"currency":"usd"}}`,
			payments.StatusProcessing, "pi_4"},
	}
	for _, c := range cases {
		fs.handle[path] = jsonOK(c.body)
		pp, err := p.CheckoutStatus(ctx, "cs_1")
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if pp.Status != c.want || pp.TransactionRef != c.ref || pp.AmountMinor != 500 || pp.Currency != "USD" {
			t.Errorf("%s: %+v", c.name, pp)
		}
	}
	fs.handle[path] = jsonOK(cases[2].body)
	pp, _ := p.CheckoutStatus(ctx, "cs_1")
	if pp.MethodRef != "cus_1|pm_1" || pp.MethodLabel != "Visa ending 4242" {
		t.Fatalf("saved method: %+v", pp)
	}
}

func TestChargeOffSession(t *testing.T) {
	p, fs := newTest(t)
	if _, err := p.Charge(ctx, payments.ChargeRequest{MethodRef: "pm_only"}); !errors.Is(err, payments.ErrCannotCharge) {
		t.Fatalf("malformed method ref: %v", err)
	}
	fs.handle["POST /v1/payment_intents"] = jsonOK(`{"id":"pi_9","status":"succeeded","amount":700,"currency":"usd","customer":"cus_1","payment_method":"pm_1"}`)
	pp, err := p.Charge(ctx, payments.ChargeRequest{MethodRef: "cus_1|pm_1", AmountMinor: 700, Currency: "USD", Reference: "pay-1", IdempotencyKey: "renew-1"})
	if err != nil || pp.Status != payments.StatusSucceeded || pp.TransactionRef != "pi_9" {
		t.Fatalf("charge: %+v %v", pp, err)
	}
	req := fs.last()
	if req.IdemKey != "renew-1" || req.Form.Get("off_session") != "true" || req.Form.Get("confirm") != "true" ||
		req.Form.Get("customer") != "cus_1" || req.Form.Get("payment_method") != "pm_1" || req.Form.Get("currency") != "usd" {
		t.Fatalf("charge request: %+v", req)
	}
	// Authentication required: a failure carrying the intent, never retried elsewhere.
	fs.handle["POST /v1/payment_intents"] = jsonStatus(402, `{"error":{"type":"card_error","code":"authentication_required",
		"message":"This payment requires authentication.","payment_intent":{"id":"pi_10","status":"requires_payment_method","amount":700,"currency":"usd"}}}`)
	pp, err = p.Charge(ctx, payments.ChargeRequest{MethodRef: "cus_1|pm_1", AmountMinor: 700, Currency: "USD", IdempotencyKey: "renew-2"})
	if err != nil || pp.Status != payments.StatusFailed || pp.FailureCode != "authentication_required" || pp.TransactionRef != "pi_10" {
		t.Fatalf("auth required: %+v %v", pp, err)
	}
	// Network-level failure: the outcome is unknown, so it is an error (retried with the same key), not a failure.
	fs.handle["POST /v1/payment_intents"] = jsonStatus(500, `oops`)
	if _, err := p.Charge(ctx, payments.ChargeRequest{MethodRef: "cus_1|pm_1", IdempotencyKey: "renew-3"}); err == nil {
		t.Fatal("unknown outcome reported as a result")
	}
}

func TestRefund(t *testing.T) {
	p, fs := newTest(t)
	fs.handle["POST /v1/refunds"] = jsonOK(`{"id":"re_1","status":"succeeded"}`)
	r, err := p.Refund(ctx, payments.RefundRequest{TransactionRef: "pi_1", AmountMinor: 200, Currency: "USD", Reason: "requested_by_customer", IdempotencyKey: "ref-1"})
	if err != nil || !r.Succeeded || r.ProviderRef != "re_1" {
		t.Fatalf("refund: %+v %v", r, err)
	}
	if req := fs.last(); req.IdemKey != "ref-1" || req.Form.Get("payment_intent") != "pi_1" || req.Form.Get("amount") != "200" ||
		req.Form.Get("reason") != "requested_by_customer" {
		t.Fatalf("refund request: %+v", req)
	}
	_, _ = p.Refund(ctx, payments.RefundRequest{TransactionRef: "pi_1", Reason: "made up", IdempotencyKey: "ref-x"})
	if fs.last().Form.Has("reason") {
		t.Fatal("unsupported reason sent to Stripe")
	}
	fs.handle["POST /v1/refunds"] = jsonOK(`{"id":"re_2","status":"pending"}`)
	if _, err := p.Refund(ctx, payments.RefundRequest{TransactionRef: "pi_1", IdempotencyKey: "ref-2"}); err == nil {
		t.Fatal("pending refund reported final")
	}
	fs.handle["POST /v1/refunds"] = jsonOK(`{"id":"re_3","status":"failed","failure_reason":"expired_or_canceled_card"}`)
	if r, err := p.Refund(ctx, payments.RefundRequest{TransactionRef: "pi_1", IdempotencyKey: "ref-3"}); err != nil || r.Succeeded ||
		!strings.Contains(r.FailureMessage, "expired_or_canceled_card") {
		t.Fatalf("failed refund: %+v %v", r, err)
	}
	fs.handle["POST /v1/refunds"] = jsonStatus(400, `{"error":{"type":"invalid_request_error","code":"charge_already_refunded","message":"already refunded"}}`)
	if r, err := p.Refund(ctx, payments.RefundRequest{TransactionRef: "pi_1", IdempotencyKey: "ref-4"}); err != nil || r.Succeeded {
		t.Fatalf("refused refund: %+v %v", r, err)
	}
}

func TestCancelCheckout(t *testing.T) {
	p, fs := newTest(t)
	fs.handle["POST /v1/checkout/sessions/cs_1/expire"] = jsonOK(`{"id":"cs_1","status":"expired"}`)
	if err := p.CancelCheckout(ctx, "cs_1"); err != nil {
		t.Fatal(err)
	}
	fs.handle["POST /v1/checkout/sessions/cs_2/expire"] = jsonStatus(400, `{"error":{"type":"invalid_request_error","code":"checkout_session_not_open","message":"Session is already complete"}}`)
	if err := p.CancelCheckout(ctx, "cs_2"); err != nil {
		t.Fatalf("already complete should be a no-op: %v", err)
	}
	fs.handle["POST /v1/checkout/sessions/cs_3/expire"] = jsonStatus(400, `{"error":{"type":"api_error","code":"other","message":"something else"}}`)
	if err := p.CancelCheckout(ctx, "cs_3"); err == nil {
		t.Fatal("unexpected error swallowed")
	}
}

func TestParseWebhook(t *testing.T) {
	p, _ := newTest(t)
	now := time.Unix(1_800_000_000, 0)
	p.Now = func() time.Time { return now }
	body, _ := json.Marshal(map[string]any{"id": "evt_1", "type": "checkout.session.completed",
		"data": map[string]any{"object": map[string]any{"id": "cs_1", "object": "checkout.session"}}})

	h := http.Header{}
	h.Set("Stripe-Signature", SignForTest("whsec_test", body, now))
	ev, err := p.ParseWebhook(h, body)
	if err != nil || ev.ID != "evt_1" || ev.SessionRef != "cs_1" || ev.TransactionRef != "" {
		t.Fatalf("valid: %+v %v", ev, err)
	}

	piBody := []byte(`{"id":"evt_2","type":"payment_intent.succeeded","data":{"object":{"id":"pi_1","object":"payment_intent"}}}`)
	h.Set("Stripe-Signature", SignForTest("whsec_test", piBody, now))
	if ev, err := p.ParseWebhook(h, piBody); err != nil || ev.TransactionRef != "pi_1" {
		t.Fatalf("payment intent event: %+v %v", ev, err)
	}

	// Several v1 signatures (secret rotation): any one matching is enough.
	good := SignForTest("whsec_test", body, now)
	bad := SignForTest("whsec_old", body, now)
	h.Set("Stripe-Signature", bad+","+strings.Split(good, ",")[1])
	if _, err := p.ParseWebhook(h, body); err != nil {
		t.Fatalf("rotated secret: %v", err)
	}

	reject := map[string]string{
		"wrong secret":   SignForTest("whsec_other", body, now),
		"stale (replay)": SignForTest("whsec_test", body, now.Add(-6*time.Minute)),
		"future":         SignForTest("whsec_test", body, now.Add(6*time.Minute)),
		"no v1":          "t=1800000000",
		"no timestamp":   "v1=abcd",
		"empty":          "",
		"not hex":        "t=1800000000,v1=zz",
	}
	for name, sig := range reject {
		h.Set("Stripe-Signature", sig)
		if _, err := p.ParseWebhook(h, body); !errors.Is(err, payments.ErrBadSignature) {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
	// Body changed after signing.
	h.Set("Stripe-Signature", SignForTest("whsec_test", body, now))
	if _, err := p.ParseWebhook(h, append([]byte(nil), append(body[:len(body)-1], ' ', '}')...)); !errors.Is(err, payments.ErrBadSignature) {
		t.Error("tampered body accepted")
	}
	// No webhook secret configured: nothing is accepted.
	p.cfg.WebhookSecret = ""
	h.Set("Stripe-Signature", SignForTest("", body, now))
	if _, err := p.ParseWebhook(h, body); !errors.Is(err, payments.ErrBadSignature) {
		t.Error("accepted without a configured secret")
	}
}
