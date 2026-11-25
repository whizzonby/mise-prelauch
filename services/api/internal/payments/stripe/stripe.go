// Package stripe is the Stripe adapter: Stripe Checkout (hosted page) for
// the first payment, saving the card for later off-session charges that Mise
// itself makes for weekly renewals (Mise-managed billing; Stripe
// subscriptions are not used, so only one system ever charges a renewal).
//
// Built to Stripe's documentation (docs.stripe.com, read 25 November 2026):
//   - POST /v1/checkout/sessions (mode=payment, payment_intent_data[setup_future_usage]=off_session)
//   - GET  /v1/checkout/sessions/{id}, POST /v1/checkout/sessions/{id}/expire
//   - POST /v1/payment_intents (off_session=true, confirm=true), GET /v1/payment_intents/{id}
//   - POST /v1/refunds
//   - Webhooks: Stripe-Signature "t=…,v1=…", HMAC-SHA256 of "t.body" with the
//     endpoint secret, constant-time compared, five-minute tolerance.
//
// Status: not verified against Stripe. No test-mode keys exist yet, and a
// Stripe account needs a business in a supported country (Trinidad and
// Tobago is not one). Tests run against a local server that follows the
// documented requests and responses.
package stripe

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"mise.tt/api/internal/payments"
	"mise.tt/api/internal/platform/config"
)

const Name = "stripe"

// Tolerance is how old a signed webhook may be (Stripe's libraries use five minutes).
const Tolerance = 5 * time.Minute

type Provider struct {
	cfg    config.StripeConfig
	base   string
	client *http.Client
	Now    func() time.Time
}

func New(cfg config.StripeConfig, client *http.Client) *Provider {
	base := cfg.APIBase
	if base == "" {
		base = "https://api.stripe.com"
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &Provider{cfg: cfg, base: strings.TrimRight(base, "/"), client: client, Now: time.Now}
}

func (p *Provider) Name() string        { return Name }
func (p *Provider) DisplayName() string { return "Card (Stripe)" }

// Live reports live keys (sk_live_ or rk_live_).
func (p *Provider) Live() bool {
	return strings.HasPrefix(p.cfg.SecretKey, "sk_live_") || strings.HasPrefix(p.cfg.SecretKey, "rk_live_")
}

func (p *Provider) Capabilities() payments.Capabilities {
	return payments.Capabilities{OffSessionCharges: true, Refunds: true, Currencies: p.cfg.Currencies, Method: "card"}
}

func (p *Provider) Readiness() payments.Readiness {
	var missing []string
	if p.cfg.SecretKey == "" {
		missing = append(missing, "STRIPE_SECRET_KEY")
	}
	if p.cfg.WebhookSecret == "" {
		missing = append(missing, "STRIPE_WEBHOOK_SECRET")
	}
	if len(missing) > 0 {
		return payments.Readiness{Reason: "Not configured: " + strings.Join(missing, ", ") + " missing."}
	}
	if len(p.cfg.Currencies) == 0 {
		return payments.Readiness{Reason: "No currencies confirmed for this Stripe account (STRIPE_CURRENCIES).", CredentialRef: mask(p.cfg.SecretKey)}
	}
	return payments.Readiness{Ready: true, CredentialRef: mask(p.cfg.SecretKey)}
}

// mask shows the key type and its last four characters only.
func mask(key string) string {
	if len(key) < 12 {
		return "set"
	}
	return key[:8] + "…" + key[len(key)-4:]
}

// ---------------------------------------------------------------- HTTP

// apiError is Stripe's error object.
type apiError struct {
	Error struct {
		Type          string         `json:"type"`
		Code          string         `json:"code"`
		DeclineCode   string         `json:"decline_code"`
		Message       string         `json:"message"`
		PaymentIntent *paymentIntent `json:"payment_intent"`
	} `json:"error"`
}

func (p *Provider) call(ctx context.Context, method, path string, form url.Values, idempotencyKey string, out any) (*apiError, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, p.base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.cfg.SecretKey)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	res, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("stripe %s %s: %w", method, path, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 400 {
		var ae apiError
		if json.Unmarshal(raw, &ae) == nil && (ae.Error.Type != "" || ae.Error.Code != "") {
			return &ae, nil
		}
		return nil, fmt.Errorf("stripe %s %s: HTTP %d", method, path, res.StatusCode)
	}
	return nil, json.Unmarshal(raw, out)
}

// ---------------------------------------------------------------- objects

type paymentIntent struct {
	ID             string `json:"id"`
	Status         string `json:"status"`
	Amount         int64  `json:"amount"`
	AmountReceived int64  `json:"amount_received"`
	Currency       string `json:"currency"`
	Customer       any    `json:"customer"`
	PaymentMethod  any    `json:"payment_method"`
	LastError      *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"last_payment_error"`
}

type checkoutSession struct {
	ID            string          `json:"id"`
	URL           string          `json:"url"`
	Status        string          `json:"status"`
	PaymentStatus string          `json:"payment_status"`
	ExpiresAt     int64           `json:"expires_at"`
	AmountTotal   int64           `json:"amount_total"`
	Currency      string          `json:"currency"`
	Customer      any             `json:"customer"`
	PaymentIntent *paymentIntent  `json:"-"`
	RawIntent     json.RawMessage `json:"payment_intent"`
}

// idOf reads an expandable field: either an id string or an object with an id.
func idOf(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case map[string]any:
		if id, ok := x["id"].(string); ok {
			return id
		}
	}
	return ""
}

// cardLabel reads "Visa ending 4242" from an expanded payment method.
func cardLabel(v any) string {
	pm, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	card, ok := pm["card"].(map[string]any)
	if !ok {
		return ""
	}
	brand, _ := card["brand"].(string)
	last4, _ := card["last4"].(string)
	if brand == "" || last4 == "" {
		return ""
	}
	return strings.ToUpper(brand[:1]) + brand[1:] + " ending " + last4
}

func (pi *paymentIntent) toPayment() *payments.ProviderPayment {
	pp := &payments.ProviderPayment{TransactionRef: pi.ID, AmountMinor: pi.Amount, Currency: strings.ToUpper(pi.Currency)}
	switch pi.Status {
	case "succeeded":
		pp.Status = payments.StatusSucceeded
	case "processing":
		pp.Status = payments.StatusProcessing
	case "requires_action", "requires_confirmation":
		pp.Status = payments.StatusRequiresAction
	case "canceled":
		pp.Status = payments.StatusCancelled
	default: // requires_payment_method: not paid (yet)
		pp.Status = payments.StatusPending
		if pi.LastError != nil {
			pp.Status = payments.StatusFailed
		}
	}
	if pi.LastError != nil {
		pp.FailureCode, pp.FailureMessage = pi.LastError.Code, pi.LastError.Message
	}
	if cus, pm := idOf(pi.Customer), idOf(pi.PaymentMethod); cus != "" && pm != "" {
		// Both are needed to charge the method later.
		pp.MethodRef = cus + "|" + pm
		pp.MethodLabel = cardLabel(pi.PaymentMethod)
	}
	return pp
}

// ---------------------------------------------------------------- Provider

func (p *Provider) CreateCheckout(ctx context.Context, req payments.CheckoutRequest) (*payments.CheckoutSession, error) {
	f := url.Values{}
	f.Set("mode", "payment")
	f.Set("client_reference_id", req.Reference)
	f.Set("success_url", req.ReturnURL)
	f.Set("cancel_url", req.ReturnURL)
	f.Set("line_items[0][quantity]", "1")
	f.Set("line_items[0][price_data][currency]", strings.ToLower(req.Currency))
	f.Set("line_items[0][price_data][unit_amount]", strconv.FormatInt(req.AmountMinor, 10))
	f.Set("line_items[0][price_data][product_data][name]", req.Description)
	f.Set("metadata[mise_checkout]", req.Reference)
	f.Set("payment_intent_data[metadata][mise_checkout]", req.Reference)
	if req.SaveMethod {
		f.Set("customer_creation", "always")
		f.Set("payment_intent_data[setup_future_usage]", "off_session")
	}
	var cs checkoutSession
	ae, err := p.call(ctx, http.MethodPost, "/v1/checkout/sessions", f, "mise-checkout-"+req.Reference, &cs)
	if err != nil {
		return nil, err
	}
	if ae != nil {
		return nil, fmt.Errorf("stripe checkout: %s: %s", ae.Error.Code, ae.Error.Message)
	}
	if cs.ID == "" || cs.URL == "" {
		return nil, errors.New("stripe checkout: no session id or url")
	}
	out := &payments.CheckoutSession{ProviderRef: cs.ID, RedirectURL: cs.URL}
	if cs.ExpiresAt > 0 {
		out.ExpiresAt = time.Unix(cs.ExpiresAt, 0)
	}
	return out, nil
}

func (p *Provider) CheckoutStatus(ctx context.Context, sessionRef string) (*payments.ProviderPayment, error) {
	var cs checkoutSession
	ae, err := p.call(ctx, http.MethodGet, "/v1/checkout/sessions/"+url.PathEscape(sessionRef)+
		"?expand[]=payment_intent&expand[]=payment_intent.payment_method", nil, "", &cs)
	if err != nil {
		return nil, err
	}
	if ae != nil {
		return nil, fmt.Errorf("stripe session: %s: %s", ae.Error.Code, ae.Error.Message)
	}
	var pi paymentIntent
	if len(cs.RawIntent) > 0 && cs.RawIntent[0] == '{' {
		if err := json.Unmarshal(cs.RawIntent, &pi); err != nil {
			return nil, err
		}
	}
	if pi.ID == "" {
		// No payment attempt yet.
		st := payments.StatusPending
		if cs.Status == "expired" {
			st = payments.StatusCancelled
		}
		return &payments.ProviderPayment{Status: st, AmountMinor: cs.AmountTotal, Currency: strings.ToUpper(cs.Currency)}, nil
	}
	pp := pi.toPayment()
	if pp.Status == payments.StatusPending && cs.Status == "expired" {
		pp.Status = payments.StatusCancelled
	}
	if pp.Status == payments.StatusPending {
		pp.TransactionRef = "" // the customer can still try another card on Stripe's page
	}
	return pp, nil
}

func (p *Provider) TransactionStatus(ctx context.Context, transactionRef string) (*payments.ProviderPayment, error) {
	var pi paymentIntent
	ae, err := p.call(ctx, http.MethodGet, "/v1/payment_intents/"+url.PathEscape(transactionRef)+"?expand[]=payment_method", nil, "", &pi)
	if err != nil {
		return nil, err
	}
	if ae != nil {
		return nil, fmt.Errorf("stripe payment intent: %s: %s", ae.Error.Code, ae.Error.Message)
	}
	return pi.toPayment(), nil
}

// Charge makes an off-session charge on a saved card. A card that needs the
// customer to authenticate fails with code authentication_required: the
// customer then pays that week through checkout (never retried elsewhere).
func (p *Provider) Charge(ctx context.Context, req payments.ChargeRequest) (*payments.ProviderPayment, error) {
	cus, pm, ok := strings.Cut(req.MethodRef, "|")
	if !ok || cus == "" || pm == "" {
		return nil, payments.ErrCannotCharge
	}
	f := url.Values{}
	f.Set("amount", strconv.FormatInt(req.AmountMinor, 10))
	f.Set("currency", strings.ToLower(req.Currency))
	f.Set("customer", cus)
	f.Set("payment_method", pm)
	f.Set("off_session", "true")
	f.Set("confirm", "true")
	f.Set("description", req.Description)
	f.Set("metadata[mise_payment]", req.Reference)
	var pi paymentIntent
	ae, err := p.call(ctx, http.MethodPost, "/v1/payment_intents", f, req.IdempotencyKey, &pi)
	if err != nil {
		return nil, err
	}
	if ae != nil {
		if ae.Error.PaymentIntent != nil {
			pp := ae.Error.PaymentIntent.toPayment()
			pp.Status, pp.FailureCode, pp.FailureMessage = payments.StatusFailed, ae.Error.Code, ae.Error.Message
			return pp, nil
		}
		return &payments.ProviderPayment{Status: payments.StatusFailed, AmountMinor: req.AmountMinor, Currency: req.Currency,
			FailureCode: ae.Error.Code, FailureMessage: ae.Error.Message}, nil
	}
	return pi.toPayment(), nil
}

func (p *Provider) Refund(ctx context.Context, req payments.RefundRequest) (*payments.RefundResult, error) {
	f := url.Values{}
	f.Set("payment_intent", req.TransactionRef)
	f.Set("amount", strconv.FormatInt(req.AmountMinor, 10))
	switch req.Reason {
	case "duplicate", "fraudulent", "requested_by_customer":
		f.Set("reason", req.Reason)
	}
	f.Set("metadata[mise_refund]", req.IdempotencyKey)
	var r struct {
		ID            string `json:"id"`
		Status        string `json:"status"`
		FailureReason string `json:"failure_reason"`
	}
	ae, err := p.call(ctx, http.MethodPost, "/v1/refunds", f, req.IdempotencyKey, &r)
	if err != nil {
		return nil, err
	}
	if ae != nil {
		return &payments.RefundResult{FailureMessage: ae.Error.Message}, nil
	}
	switch r.Status {
	case "succeeded":
		return &payments.RefundResult{ProviderRef: r.ID, Succeeded: true}, nil
	case "pending", "requires_action":
		// Not final yet: left pending in Mise and retried with the same key.
		return nil, fmt.Errorf("stripe refund %s is %s", r.ID, r.Status)
	default:
		return &payments.RefundResult{ProviderRef: r.ID, FailureMessage: "Refund " + r.Status + " " + r.FailureReason}, nil
	}
}

// CancelCheckout expires an open Checkout Session so it can no longer be paid.
func (p *Provider) CancelCheckout(ctx context.Context, sessionRef string) error {
	var cs checkoutSession
	ae, err := p.call(ctx, http.MethodPost, "/v1/checkout/sessions/"+url.PathEscape(sessionRef)+"/expire", url.Values{}, "mise-expire-"+sessionRef, &cs)
	if err != nil {
		return err
	}
	if ae != nil && ae.Error.Code != "" {
		// Already complete or expired: nothing left to close.
		if strings.Contains(ae.Error.Message, "complete") || strings.Contains(ae.Error.Message, "expired") {
			return nil
		}
		return fmt.Errorf("stripe expire: %s: %s", ae.Error.Code, ae.Error.Message)
	}
	return nil
}

// ParseWebhook verifies the Stripe-Signature header, then reads which
// session or payment the event is about. The event's contents are not
// trusted beyond that: the status is fetched from Stripe.
func (p *Provider) ParseWebhook(header http.Header, body []byte) (*payments.WebhookEvent, error) {
	if p.cfg.WebhookSecret == "" {
		return nil, payments.ErrBadSignature
	}
	var ts string
	var sigs []string
	for _, part := range strings.Split(header.Get("Stripe-Signature"), ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "t":
			ts = v
		case "v1":
			sigs = append(sigs, v)
		}
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || len(sigs) == 0 {
		return nil, payments.ErrBadSignature
	}
	if d := p.Now().Sub(time.Unix(sec, 0)); d > Tolerance || d < -Tolerance {
		return nil, payments.ErrBadSignature
	}
	mac := hmac.New(sha256.New, []byte(p.cfg.WebhookSecret))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	want := mac.Sum(nil)
	ok := false
	for _, s := range sigs {
		got, err := hex.DecodeString(s)
		if err == nil && hmac.Equal(want, got) {
			ok = true
		}
	}
	if !ok {
		return nil, payments.ErrBadSignature
	}
	var ev struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Data struct {
			Object struct {
				ID     string `json:"id"`
				Object string `json:"object"`
			} `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &ev); err != nil || ev.ID == "" {
		return nil, payments.ErrBadSignature
	}
	out := &payments.WebhookEvent{ID: ev.ID, Type: ev.Type}
	switch ev.Data.Object.Object {
	case "checkout.session":
		out.SessionRef = ev.Data.Object.ID
	case "payment_intent":
		out.TransactionRef = ev.Data.Object.ID
	}
	return out, nil
}

// SignForTest builds a Stripe-Signature header (tests and local tools only).
func SignForTest(secret string, body []byte, at time.Time) string {
	ts := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	return "t=" + ts + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}
