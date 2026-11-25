package paypal

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"mise.tt/api/internal/payments"
	"mise.tt/api/internal/platform/config"
)

// fakePayPal implements the documented subset of the PayPal REST API the
// adapter uses: OAuth client credentials, Orders v2, captures, refunds and
// the webhook verification postback.
type fakePayPal struct {
	mu         sync.Mutex
	tokenCalls int
	requests   []recorded
	handle     map[string]func(w http.ResponseWriter, body []byte)
}

type recorded struct {
	Method, Path, Auth, RequestID string
	Body                          []byte
}

func (f *fakePayPal) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path == "/v1/oauth2/token" {
		id, secret, ok := r.BasicAuth()
		if !ok || id != "client-id-1234" || secret != "client-secret" || string(body) != "grant_type=client_credentials" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
			return
		}
		f.tokenCalls++
		_, _ = w.Write([]byte(`{"access_token":"A21AA-token","token_type":"Bearer","expires_in":32400}`))
		return
	}
	f.requests = append(f.requests, recorded{r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("PayPal-Request-Id"), body})
	if r.Header.Get("Authorization") != "Bearer A21AA-token" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	h := f.handle[r.Method+" "+r.URL.Path]
	if h == nil {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"name":"RESOURCE_NOT_FOUND","message":"not found"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	h(w, body)
}

func (f *fakePayPal) last() recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

func ok(body string) func(http.ResponseWriter, []byte) {
	return func(w http.ResponseWriter, _ []byte) { _, _ = w.Write([]byte(body)) }
}

func status(code int, body string) func(http.ResponseWriter, []byte) {
	return func(w http.ResponseWriter, _ []byte) { w.WriteHeader(code); _, _ = w.Write([]byte(body)) }
}

func newTest(t *testing.T) (*Provider, *fakePayPal) {
	t.Helper()
	fp := &fakePayPal{handle: map[string]func(http.ResponseWriter, []byte){}}
	srv := httptest.NewServer(fp)
	t.Cleanup(srv.Close)
	p := New(config.PayPalConfig{ClientID: "client-id-1234", ClientSecret: "client-secret", Env: "sandbox", WebhookID: "WH-1",
		Currencies: []string{"USD"}, APIBase: srv.URL}, srv.Client())
	return p, fp
}

var ctx = context.Background()

func TestMoneyConversionIsExact(t *testing.T) {
	cases := []struct {
		minor int64
		cur   string
		val   string
	}{{12345, "USD", "123.45"}, {5, "USD", "0.05"}, {0, "USD", "0.00"}, {100, "EUR", "1.00"}, {1500, "JPY", "1500"}, {-250, "USD", "-2.50"}}
	for _, c := range cases {
		if got := Value(c.minor, c.cur); got != c.val {
			t.Errorf("Value(%d, %s) = %s, want %s", c.minor, c.cur, got, c.val)
		}
		if got, err := Minor(c.val, c.cur); err != nil || got != c.minor {
			t.Errorf("Minor(%s, %s) = %d %v, want %d", c.val, c.cur, got, err, c.minor)
		}
	}
	if n, _ := Minor("10.5", "USD"); n != 1050 {
		t.Errorf("one decimal: %d", n)
	}
	if n, _ := Minor("7", "USD"); n != 700 {
		t.Errorf("no decimals: %d", n)
	}
	for _, bad := range []string{"1.234", "abc", "1.x", ""} {
		if _, err := Minor(bad, "USD"); err == nil {
			t.Errorf("Minor(%q) accepted", bad)
		}
	}
}

func TestReadinessAndCapabilities(t *testing.T) {
	if r := New(config.PayPalConfig{}, nil).Readiness(); r.Ready {
		t.Fatal("unconfigured ready")
	}
	if r := New(config.PayPalConfig{ClientID: "abcdefgh", ClientSecret: "s"}, nil).Readiness(); r.Ready || !strings.Contains(r.Reason, "WEBHOOK") {
		t.Fatalf("no webhook id: %+v", r)
	}
	if r := New(config.PayPalConfig{ClientID: "abcdefgh", ClientSecret: "s", WebhookID: "W"}, nil).Readiness(); r.Ready ||
		!strings.Contains(r.Reason, "TTD") {
		t.Fatalf("no currencies: %+v", r)
	}
	p, _ := newTest(t)
	r := p.Readiness()
	if !r.Ready || strings.Contains(r.CredentialRef, "client-secret") || !strings.HasSuffix(r.CredentialRef, "1234") {
		t.Fatalf("ready: %+v", r)
	}
	c := p.Capabilities()
	if c.OffSessionCharges || !c.Refunds || c.Supports("TTD") || !c.Supports("USD") || c.Method != "paypal" {
		t.Fatalf("capabilities: %+v", c)
	}
	if p.Live() || !New(config.PayPalConfig{Env: "live"}, nil).Live() {
		t.Fatal("live flag")
	}
	if _, err := p.Charge(ctx, payments.ChargeRequest{}); !errors.Is(err, payments.ErrUnsupported) {
		t.Fatalf("off-session charge: %v", err)
	}
}

func TestBadCredentials(t *testing.T) {
	_, fp := newTest(t)
	srv := httptest.NewServer(fp)
	defer srv.Close()
	p := New(config.PayPalConfig{ClientID: "client-id-1234", ClientSecret: "wrong", APIBase: srv.URL}, srv.Client())
	_, err := p.CreateCheckout(ctx, payments.CheckoutRequest{Reference: "r", AmountMinor: 100, Currency: "USD"})
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("bad credentials: %v", err)
	}
}

func TestCreateCheckoutAndTokenCache(t *testing.T) {
	p, fp := newTest(t)
	fp.handle["POST /v2/checkout/orders"] = ok(`{"id":"5O190127TN364715T","status":"PAYER_ACTION_REQUIRED","links":[
		{"href":"https://api-m.sandbox.paypal.com/v2/checkout/orders/5O190127TN364715T","rel":"self"},
		{"href":"https://www.sandbox.paypal.com/checkoutnow?token=5O190127TN364715T","rel":"payer-action"}]}`)
	cs, err := p.CreateCheckout(ctx, payments.CheckoutRequest{Reference: "chk-1", AmountMinor: 4599, Currency: "USD",
		Description: "Mise week", ReturnURL: "https://app.example/subscribe/result"})
	if err != nil {
		t.Fatal(err)
	}
	if cs.ProviderRef != "5O190127TN364715T" || !strings.Contains(cs.RedirectURL, "checkoutnow") {
		t.Fatalf("session: %+v", cs)
	}
	req := fp.last()
	if req.RequestID != "mise-order-chk-1" {
		t.Fatalf("idempotency header: %q", req.RequestID)
	}
	var sent struct {
		Intent        string `json:"intent"`
		PurchaseUnits []struct {
			ReferenceID string `json:"reference_id"`
			Amount      amount `json:"amount"`
		} `json:"purchase_units"`
	}
	_ = json.Unmarshal(req.Body, &sent)
	if sent.Intent != "CAPTURE" || len(sent.PurchaseUnits) != 1 || sent.PurchaseUnits[0].Amount != (amount{"USD", "45.99"}) ||
		sent.PurchaseUnits[0].ReferenceID != "chk-1" {
		t.Fatalf("order body: %s", req.Body)
	}

	// The older "approve" relation is accepted too; the token is reused.
	fp.handle["POST /v2/checkout/orders"] = ok(`{"id":"O2","links":[{"href":"https://www.sandbox.paypal.com/checkoutnow?token=O2","rel":"approve"}]}`)
	if cs, err := p.CreateCheckout(ctx, payments.CheckoutRequest{Reference: "chk-2", AmountMinor: 1, Currency: "USD"}); err != nil || cs.ProviderRef != "O2" {
		t.Fatalf("approve rel: %+v %v", cs, err)
	}
	if fp.tokenCalls != 1 {
		t.Fatalf("token fetched %d times, want 1 (cached)", fp.tokenCalls)
	}

	fp.handle["POST /v2/checkout/orders"] = ok(`{"id":"O3","links":[{"href":"x","rel":"self"}]}`)
	if _, err := p.CreateCheckout(ctx, payments.CheckoutRequest{Reference: "chk-3"}); err == nil {
		t.Fatal("order without approval link accepted")
	}
	fp.handle["POST /v2/checkout/orders"] = status(422, `{"name":"UNPROCESSABLE_ENTITY","details":[{"issue":"CURRENCY_NOT_SUPPORTED"}]}`)
	if _, err := p.CreateCheckout(ctx, payments.CheckoutRequest{Reference: "chk-4", Currency: "TTD"}); err == nil ||
		!strings.Contains(err.Error(), "CURRENCY_NOT_SUPPORTED") {
		t.Fatalf("unsupported currency: %v", err)
	}
}

const capturedOrder = `{"id":"O1","status":"COMPLETED","purchase_units":[{"reference_id":"chk-1","amount":{"currency_code":"USD","value":"45.99"},
	"payments":{"captures":[{"id":"CAP1","status":"COMPLETED","amount":{"currency_code":"USD","value":"45.99"},
	"seller_receivable_breakdown":{"gross_amount":{"currency_code":"USD","value":"45.99"},"paypal_fee":{"currency_code":"USD","value":"1.84"},
	"net_amount":{"currency_code":"USD","value":"44.15"}}}]}}]}`

func TestCheckoutStatusCapturesOnce(t *testing.T) {
	p, fp := newTest(t)
	fp.handle["GET /v2/checkout/orders/O1"] = ok(`{"id":"O1","status":"APPROVED","purchase_units":[{"amount":{"currency_code":"USD","value":"45.99"}}]}`)
	fp.handle["POST /v2/checkout/orders/O1/capture"] = ok(capturedOrder)
	pp, err := p.CheckoutStatus(ctx, "O1")
	if err != nil {
		t.Fatal(err)
	}
	if pp.Status != payments.StatusSucceeded || pp.TransactionRef != "CAP1" || pp.AmountMinor != 4599 || pp.Currency != "USD" {
		t.Fatalf("captured: %+v", pp)
	}
	if req := fp.last(); req.RequestID != "mise-capture-O1" {
		t.Fatalf("capture idempotency: %q", req.RequestID)
	}
	m := pp.Money
	if m == nil || m.FeeCurrency != "USD" || *m.FeeMinor != 184 || *m.SettlementAmountMinor != 4415 || *m.ChargedAmountMinor != 4599 || m.FXRate != "" {
		t.Fatalf("money detail: %+v", m)
	}
}

func TestCheckoutStatusStates(t *testing.T) {
	p, fp := newTest(t)
	cases := []struct {
		name, body string
		want       payments.Status
	}{
		{"waiting for buyer", `{"id":"O1","status":"PAYER_ACTION_REQUIRED","purchase_units":[{"amount":{"currency_code":"USD","value":"1.00"}}]}`, payments.StatusPending},
		{"voided", `{"id":"O1","status":"VOIDED","purchase_units":[{"amount":{"currency_code":"USD","value":"1.00"}}]}`, payments.StatusCancelled},
		{"capture pending", `{"id":"O1","status":"COMPLETED","purchase_units":[{"amount":{"currency_code":"USD","value":"1.00"},
			"payments":{"captures":[{"id":"C","status":"PENDING","amount":{"currency_code":"USD","value":"1.00"}}]}}]}`, payments.StatusProcessing},
		{"capture declined", `{"id":"O1","status":"COMPLETED","purchase_units":[{"amount":{"currency_code":"USD","value":"1.00"},
			"payments":{"captures":[{"id":"C","status":"DECLINED","amount":{"currency_code":"USD","value":"1.00"}}]}}]}`, payments.StatusFailed},
	}
	for _, c := range cases {
		fp.handle["GET /v2/checkout/orders/O1"] = ok(c.body)
		pp, err := p.CheckoutStatus(ctx, "O1")
		if err != nil || pp.Status != c.want {
			t.Errorf("%s: %+v %v", c.name, pp, err)
		}
	}
	// Declined at capture (422 INSTRUMENT_DECLINED): a failure, not an error.
	fp.handle["GET /v2/checkout/orders/O1"] = ok(`{"id":"O1","status":"APPROVED"}`)
	fp.handle["POST /v2/checkout/orders/O1/capture"] = status(422, `{"name":"UNPROCESSABLE_ENTITY","details":[{"issue":"INSTRUMENT_DECLINED"}]}`)
	pp, err := p.CheckoutStatus(ctx, "O1")
	if err != nil || pp.Status != payments.StatusFailed || pp.FailureCode != "INSTRUMENT_DECLINED" {
		t.Fatalf("declined capture: %+v %v", pp, err)
	}
	// A server error at capture is an unknown outcome: an error, retried later with the same request id.
	fp.handle["POST /v2/checkout/orders/O1/capture"] = status(500, `{"name":"INTERNAL_SERVER_ERROR"}`)
	if _, err := p.CheckoutStatus(ctx, "O1"); err == nil {
		t.Fatal("unknown capture outcome reported as a result")
	}
}

func TestTransactionStatusWithExchangeRate(t *testing.T) {
	p, fp := newTest(t)
	fp.handle["GET /v2/payments/captures/CAP1"] = ok(`{"id":"CAP1","status":"COMPLETED","amount":{"currency_code":"USD","value":"10.00"},
		"seller_receivable_breakdown":{"gross_amount":{"currency_code":"USD","value":"10.00"},"paypal_fee":{"currency_code":"USD","value":"0.70"},
		"net_amount":{"currency_code":"USD","value":"9.30"},"exchange_rate":{"value":"0.9123"}}}`)
	pp, err := p.TransactionStatus(ctx, "CAP1")
	if err != nil || pp.Status != payments.StatusSucceeded || pp.AmountMinor != 1000 {
		t.Fatalf("capture: %+v %v", pp, err)
	}
	if pp.Money == nil || pp.Money.FXRate != "0.9123" || pp.Money.FXSource != "paypal" {
		t.Fatalf("exchange rate: %+v", pp.Money)
	}
	fp.handle["GET /v2/payments/captures/CAP2"] = ok(`{"id":"CAP2","status":"COMPLETED","amount":{"currency_code":"USD","value":"1.00"},
		"seller_receivable_breakdown":{"exchange_rate":{"value":"1e5; DROP"}}}`)
	if pp, _ := p.TransactionStatus(ctx, "CAP2"); pp.Money.FXRate != "" {
		t.Fatalf("malformed rate recorded: %q", pp.Money.FXRate)
	}
}

func TestRefund(t *testing.T) {
	p, fp := newTest(t)
	fp.handle["POST /v2/payments/captures/CAP1/refund"] = ok(`{"id":"R1","status":"COMPLETED"}`)
	r, err := p.Refund(ctx, payments.RefundRequest{TransactionRef: "CAP1", AmountMinor: 500, Currency: "USD", IdempotencyKey: "ref-1"})
	if err != nil || !r.Succeeded || r.ProviderRef != "R1" {
		t.Fatalf("refund: %+v %v", r, err)
	}
	req := fp.last()
	if req.RequestID != "ref-1" || !strings.Contains(string(req.Body), `"value":"5.00"`) {
		t.Fatalf("refund request: %s %s", req.RequestID, req.Body)
	}
	fp.handle["POST /v2/payments/captures/CAP1/refund"] = ok(`{"id":"R2","status":"PENDING"}`)
	if _, err := p.Refund(ctx, payments.RefundRequest{TransactionRef: "CAP1", IdempotencyKey: "ref-2"}); err == nil {
		t.Fatal("pending refund reported final")
	}
	fp.handle["POST /v2/payments/captures/CAP1/refund"] = status(422, `{"name":"UNPROCESSABLE_ENTITY","details":[{"issue":"REFUND_AMOUNT_EXCEEDED"}]}`)
	if r, err := p.Refund(ctx, payments.RefundRequest{TransactionRef: "CAP1", IdempotencyKey: "ref-3"}); err != nil || r.Succeeded ||
		!strings.Contains(r.FailureMessage, "REFUND_AMOUNT_EXCEEDED") {
		t.Fatalf("refused refund: %+v %v", r, err)
	}
}

func webhookHeaders() http.Header {
	h := http.Header{}
	h.Set("PAYPAL-TRANSMISSION-ID", "69cd13f0-d67a-11e5-baa3-778b53f4ae55")
	h.Set("PAYPAL-TRANSMISSION-TIME", "2026-11-25T16:00:00Z")
	h.Set("PAYPAL-TRANSMISSION-SIG", "sig")
	h.Set("PAYPAL-CERT-URL", "https://api-m.sandbox.paypal.com/v1/notifications/certs/CERT-1")
	h.Set("PAYPAL-AUTH-ALGO", "SHA256withRSA")
	return h
}

func TestParseWebhook(t *testing.T) {
	p, fp := newTest(t)
	var posted map[string]json.RawMessage
	var postedRaw []byte
	fp.handle["POST /v1/notifications/verify-webhook-signature"] = func(w http.ResponseWriter, body []byte) {
		postedRaw = append([]byte(nil), body...)
		_ = json.Unmarshal(body, &posted)
		_, _ = w.Write([]byte(`{"verification_status":"SUCCESS"}`))
	}
	body := []byte(`{"id":"WH-EVT-1","event_type":"PAYMENT.CAPTURE.COMPLETED","resource":{"id":"CAP1",
		"supplementary_data":{"related_ids":{"order_id":"O1"}}}}`)
	ev, err := p.ParseWebhook(webhookHeaders(), body)
	if err != nil || ev.ID != "WH-EVT-1" || ev.SessionRef != "O1" {
		t.Fatalf("capture event: %+v %v", ev, err)
	}
	if string(posted["webhook_id"]) != `"WH-1"` || !strings.HasSuffix(string(postedRaw), `"webhook_event":`+string(body)+"}") ||
		string(posted["auth_algo"]) != `"SHA256withRSA"` {
		t.Fatalf("postback body: %v", posted)
	}

	orderBody := []byte(`{"id":"WH-EVT-2","event_type":"CHECKOUT.ORDER.APPROVED","resource":{"id":"O9"}}`)
	if ev, err := p.ParseWebhook(webhookHeaders(), orderBody); err != nil || ev.SessionRef != "O9" {
		t.Fatalf("order event: %+v %v", ev, err)
	}
	refundOnly := []byte(`{"id":"WH-EVT-3","event_type":"PAYMENT.CAPTURE.REFUNDED","resource":{"id":"CAP7"}}`)
	if ev, err := p.ParseWebhook(webhookHeaders(), refundOnly); err != nil || ev.TransactionRef != "CAP7" || ev.SessionRef != "" {
		t.Fatalf("capture without order id: %+v %v", ev, err)
	}

	// PayPal says the signature is not valid.
	fp.handle["POST /v1/notifications/verify-webhook-signature"] = ok(`{"verification_status":"FAILURE"}`)
	if _, err := p.ParseWebhook(webhookHeaders(), body); !errors.Is(err, payments.ErrBadSignature) {
		t.Fatalf("failed verification accepted: %v", err)
	}
	// Missing transmission headers never reach PayPal.
	h := webhookHeaders()
	h.Del("PAYPAL-TRANSMISSION-SIG")
	if _, err := p.ParseWebhook(h, body); !errors.Is(err, payments.ErrBadSignature) {
		t.Fatalf("missing header accepted: %v", err)
	}
	if _, err := p.ParseWebhook(webhookHeaders(), []byte(`not json`)); !errors.Is(err, payments.ErrBadSignature) {
		t.Fatal("malformed body accepted")
	}
	// Verification service down: an error (PayPal retries), not acceptance and not a bad signature.
	fp.handle["POST /v1/notifications/verify-webhook-signature"] = status(503, `{}`)
	if _, err := p.ParseWebhook(webhookHeaders(), body); err == nil || errors.Is(err, payments.ErrBadSignature) {
		t.Fatalf("verification outage: %v", err)
	}
	p.cfg.WebhookID = ""
	if _, err := p.ParseWebhook(webhookHeaders(), body); !errors.Is(err, payments.ErrBadSignature) {
		t.Fatal("accepted without a webhook id")
	}
}
