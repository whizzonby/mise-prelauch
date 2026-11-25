// Package paypal is the PayPal adapter: PayPal Checkout through the Orders
// v2 API, one-time payments only.
//
// Built to PayPal's developer documentation (developer.paypal.com, read
// 25 November 2026):
//   - OAuth: POST /v1/oauth2/token, HTTP Basic client id/secret,
//     grant_type=client_credentials.
//   - POST /v2/checkout/orders (intent CAPTURE, PayPal-Request-Id for
//     idempotency); the buyer approves on PayPal through the order's
//     approval link; POST /v2/checkout/orders/{id}/capture captures it.
//   - GET /v2/checkout/orders/{id}, GET /v2/payments/captures/{id}.
//   - POST /v2/payments/captures/{id}/refund.
//   - Webhooks verified by postback: POST /v1/notifications/verify-webhook-signature
//     with the PAYPAL-* transmission headers, the webhook id and the event,
//     answered verification_status SUCCESS.
//
// Not implemented on purpose:
//   - Saved PayPal accounts (vaulting) need separate PayPal approval, so
//     PayPal cannot charge later weeks off-session (OffSessionCharges false).
//   - PayPal Subscriptions would make PayPal bill renewals while Mise also
//     bills them; Mise stays the only billing authority.
//   - TTD: PayPal does not support it. PayPal is only offered for a
//     currency on its list that the business has configured, and Mise never
//     converts TTD (an approved conversion and disclosure policy would be
//     needed first).
//
// Status: not verified against PayPal (no sandbox credentials yet). The
// approval link's rel name could not be read from the documentation, so
// both documented forms ("approve" and "payer-action") are accepted; confirm
// in the sandbox.
package paypal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"mise.tt/api/internal/payments"
	"mise.tt/api/internal/platform/config"
)

const Name = "paypal"

type Provider struct {
	cfg    config.PayPalConfig
	base   string
	client *http.Client

	mu      sync.Mutex
	token   string
	expires time.Time
}

func New(cfg config.PayPalConfig, client *http.Client) *Provider {
	base := cfg.APIBase
	if base == "" {
		base = "https://api-m.sandbox.paypal.com"
		if cfg.Env == "live" {
			base = "https://api-m.paypal.com"
		}
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &Provider{cfg: cfg, base: strings.TrimRight(base, "/"), client: client}
}

func (p *Provider) Name() string        { return Name }
func (p *Provider) DisplayName() string { return "PayPal" }
func (p *Provider) Live() bool          { return p.cfg.Env == "live" }

func (p *Provider) Capabilities() payments.Capabilities {
	return payments.Capabilities{OffSessionCharges: false, Refunds: true, Currencies: p.cfg.Currencies, Method: "paypal"}
}

func (p *Provider) Readiness() payments.Readiness {
	if p.cfg.ClientID == "" || p.cfg.ClientSecret == "" {
		return payments.Readiness{Reason: "Not configured: PAYPAL_CLIENT_ID and PAYPAL_CLIENT_SECRET missing."}
	}
	ref := p.cfg.Env + " client …" + tail(p.cfg.ClientID)
	if p.cfg.WebhookID == "" {
		return payments.Readiness{Reason: "PAYPAL_WEBHOOK_ID missing: payments could not be confirmed.", CredentialRef: ref}
	}
	if len(p.cfg.Currencies) == 0 {
		return payments.Readiness{Reason: "No currencies configured (PAYPAL_CURRENCIES). PayPal does not support TTD.", CredentialRef: ref}
	}
	return payments.Readiness{Ready: true, CredentialRef: ref}
}

func tail(s string) string {
	if len(s) <= 4 {
		return "****"
	}
	return s[len(s)-4:]
}

// ---------------------------------------------------------------- money

// zeroDecimal are PayPal's "zero-digit" currencies.
var zeroDecimal = map[string]bool{"HUF": true, "JPY": true, "TWD": true}

// Value formats integer minor units as PayPal's decimal string, exactly (no floats).
func Value(minor int64, currency string) string {
	if zeroDecimal[currency] {
		return strconv.FormatInt(minor, 10)
	}
	sign := ""
	if minor < 0 {
		sign, minor = "-", -minor
	}
	return fmt.Sprintf("%s%d.%02d", sign, minor/100, minor%100)
}

// Minor parses PayPal's decimal string into integer minor units, exactly.
func Minor(value, currency string) (int64, error) {
	if zeroDecimal[currency] {
		return strconv.ParseInt(value, 10, 64)
	}
	sign := int64(1)
	if rest, neg := strings.CutPrefix(value, "-"); neg {
		sign, value = -1, rest
	}
	whole, frac, _ := strings.Cut(value, ".")
	if whole == "" || strings.HasPrefix(whole, "+") || strings.HasPrefix(whole, "-") {
		return 0, fmt.Errorf("paypal: %q is not an amount", value)
	}
	if len(frac) > 2 {
		return 0, fmt.Errorf("paypal: %q has more than two decimals", value)
	}
	frac += strings.Repeat("0", 2-len(frac))
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, err
	}
	f, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0, err
	}
	return sign * (w*100 + f), nil
}

type amount struct {
	CurrencyCode string `json:"currency_code"`
	Value        string `json:"value"`
}

// ---------------------------------------------------------------- HTTP

func (p *Provider) accessToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.token != "" && time.Now().Before(p.expires) {
		return p.token, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base+"/v1/oauth2/token", strings.NewReader("grant_type=client_credentials"))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(p.cfg.ClientID, p.cfg.ClientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("paypal token: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("paypal token: HTTP %d (check the client id and secret)", res.StatusCode)
	}
	var t struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&t); err != nil || t.AccessToken == "" {
		return "", errors.New("paypal token: no access token in the response")
	}
	p.token = t.AccessToken
	// Refresh a minute early.
	p.expires = time.Now().Add(time.Duration(max(t.ExpiresIn-60, 30)) * time.Second)
	return p.token, nil
}

type apiError struct {
	Status  int
	Name    string `json:"name"`
	Message string `json:"message"`
	Details []struct {
		Issue       string `json:"issue"`
		Description string `json:"description"`
	} `json:"details"`
}

func (e *apiError) Error() string {
	issue := ""
	if len(e.Details) > 0 {
		issue = " " + e.Details[0].Issue
	}
	return fmt.Sprintf("paypal: HTTP %d %s%s", e.Status, e.Name, issue)
}

func (p *Provider) call(ctx context.Context, method, path string, in any, requestID string, out any) error {
	tok, err := p.accessToken(ctx)
	if err != nil {
		return err
	}
	var body io.Reader
	switch v := in.(type) {
	case nil:
	case []byte: // already-encoded JSON, sent exactly as given
		body = bytes.NewReader(v)
	default:
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Prefer", "return=representation")
	if requestID != "" {
		req.Header.Set("PayPal-Request-Id", requestID)
	}
	res, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("paypal %s %s: %w", method, path, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return err
	}
	if res.StatusCode >= 400 {
		ae := &apiError{Status: res.StatusCode}
		_ = json.Unmarshal(raw, ae)
		return ae
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// ---------------------------------------------------------------- orders

type link struct {
	Href string `json:"href"`
	Rel  string `json:"rel"`
}

type capture struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Amount amount `json:"amount"`
	// SellerReceivable is PayPal's breakdown of what reaches the merchant
	// (gross, fee, net and, when currencies differ, the exchange rate).
	SellerReceivable *struct {
		GrossAmount  *amount `json:"gross_amount"`
		PayPalFee    *amount `json:"paypal_fee"`
		NetAmount    *amount `json:"net_amount"`
		ExchangeRate *struct {
			Value string `json:"value"`
		} `json:"exchange_rate"`
	} `json:"seller_receivable_breakdown"`
}

// money turns PayPal's reported breakdown into Mise's money detail. Nothing
// is computed: a figure PayPal did not send stays empty.
func (c capture) money() *payments.MoneyDetail {
	b := c.SellerReceivable
	if b == nil {
		return nil
	}
	m := &payments.MoneyDetail{}
	if b.GrossAmount != nil {
		if n, err := Minor(b.GrossAmount.Value, b.GrossAmount.CurrencyCode); err == nil {
			m.ChargedCurrency, m.ChargedAmountMinor = b.GrossAmount.CurrencyCode, &n
		}
	}
	if b.NetAmount != nil {
		if n, err := Minor(b.NetAmount.Value, b.NetAmount.CurrencyCode); err == nil {
			m.SettlementCurrency, m.SettlementAmountMinor = b.NetAmount.CurrencyCode, &n
		}
	}
	if b.PayPalFee != nil {
		if n, err := Minor(b.PayPalFee.Value, b.PayPalFee.CurrencyCode); err == nil {
			m.FeeCurrency, m.FeeMinor = b.PayPalFee.CurrencyCode, &n
		}
	}
	if b.ExchangeRate != nil && decimalRate.MatchString(b.ExchangeRate.Value) {
		m.FXRate, m.FXSource = b.ExchangeRate.Value, "paypal"
	}
	return m
}

var decimalRate = regexp.MustCompile(`^[0-9]{1,10}(\.[0-9]{1,10})?$`)

type order struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	Links         []link `json:"links"`
	PurchaseUnits []struct {
		ReferenceID string `json:"reference_id"`
		Amount      amount `json:"amount"`
		Payments    struct {
			Captures []capture `json:"captures"`
		} `json:"payments"`
	} `json:"purchase_units"`
}

// ApprovalRels are the link relations PayPal documents for the buyer's
// approval page; the first one present is used.
var ApprovalRels = []string{"payer-action", "approve"}

func (p *Provider) CreateCheckout(ctx context.Context, req payments.CheckoutRequest) (*payments.CheckoutSession, error) {
	in := map[string]any{
		"intent": "CAPTURE",
		"purchase_units": []map[string]any{{
			"reference_id": req.Reference,
			"custom_id":    req.Reference,
			"description":  req.Description,
			"amount":       amount{CurrencyCode: req.Currency, Value: Value(req.AmountMinor, req.Currency)},
		}},
		"payment_source": map[string]any{"paypal": map[string]any{"experience_context": map[string]any{
			"return_url": req.ReturnURL, "cancel_url": req.ReturnURL, "user_action": "PAY_NOW", "shipping_preference": "NO_SHIPPING",
		}}},
	}
	var o order
	if err := p.call(ctx, http.MethodPost, "/v2/checkout/orders", in, "mise-order-"+req.Reference, &o); err != nil {
		return nil, err
	}
	for _, rel := range ApprovalRels {
		for _, l := range o.Links {
			if l.Rel == rel && l.Href != "" {
				return &payments.CheckoutSession{ProviderRef: o.ID, RedirectURL: l.Href}, nil
			}
		}
	}
	return nil, errors.New("paypal: the order has no approval link")
}

// CheckoutStatus reads the order and, once the buyer has approved it,
// captures the payment (idempotently, by request id). The capture is what
// moves the money; until it completes nothing is recorded as paid.
func (p *Provider) CheckoutStatus(ctx context.Context, orderID string) (*payments.ProviderPayment, error) {
	var o order
	if err := p.call(ctx, http.MethodGet, "/v2/checkout/orders/"+url.PathEscape(orderID), nil, "", &o); err != nil {
		return nil, err
	}
	if o.Status == "APPROVED" {
		var captured order
		err := p.call(ctx, http.MethodPost, "/v2/checkout/orders/"+url.PathEscape(orderID)+"/capture", map[string]any{}, "mise-capture-"+orderID, &captured)
		var ae *apiError
		if errors.As(err, &ae) && ae.Status == http.StatusUnprocessableEntity {
			// For example INSTRUMENT_DECLINED: the buyer can choose another funding source on PayPal.
			return &payments.ProviderPayment{Status: payments.StatusFailed, FailureCode: detail(ae), FailureMessage: "PayPal could not capture the payment."}, nil
		}
		if err != nil {
			return nil, err
		}
		o = captured
	}
	return orderPayment(o)
}

func detail(ae *apiError) string {
	if len(ae.Details) > 0 {
		return ae.Details[0].Issue
	}
	return ae.Name
}

func orderPayment(o order) (*payments.ProviderPayment, error) {
	pp := &payments.ProviderPayment{Status: payments.StatusPending}
	if len(o.PurchaseUnits) > 0 {
		pu := o.PurchaseUnits[0]
		cur := pu.Amount.CurrencyCode
		if n, err := Minor(pu.Amount.Value, cur); err == nil {
			pp.AmountMinor, pp.Currency = n, cur
		}
		if len(pu.Payments.Captures) > 0 {
			c := pu.Payments.Captures[0]
			pp.TransactionRef = c.ID
			if n, err := Minor(c.Amount.Value, c.Amount.CurrencyCode); err == nil {
				pp.AmountMinor, pp.Currency = n, c.Amount.CurrencyCode
			}
			pp.Status = captureStatus(c.Status)
			pp.Money = c.money()
			return pp, nil
		}
	}
	switch o.Status {
	case "VOIDED":
		pp.Status = payments.StatusCancelled
	case "PAYER_ACTION_REQUIRED":
		pp.Status = payments.StatusPending // still on PayPal's page
	}
	return pp, nil
}

func captureStatus(s string) payments.Status {
	switch s {
	case "COMPLETED":
		return payments.StatusSucceeded
	case "PENDING":
		return payments.StatusProcessing
	case "DECLINED", "FAILED":
		return payments.StatusFailed
	case "REFUNDED":
		return payments.StatusSucceeded // refunds are tracked by Mise's own refund records
	}
	return payments.StatusPending
}

func (p *Provider) TransactionStatus(ctx context.Context, captureID string) (*payments.ProviderPayment, error) {
	var c capture
	if err := p.call(ctx, http.MethodGet, "/v2/payments/captures/"+url.PathEscape(captureID), nil, "", &c); err != nil {
		return nil, err
	}
	n, err := Minor(c.Amount.Value, c.Amount.CurrencyCode)
	if err != nil {
		return nil, err
	}
	return &payments.ProviderPayment{TransactionRef: c.ID, Status: captureStatus(c.Status), AmountMinor: n, Currency: c.Amount.CurrencyCode,
		Money: c.money()}, nil
}

// Charge is not available: saved PayPal accounts need PayPal's approval.
func (p *Provider) Charge(context.Context, payments.ChargeRequest) (*payments.ProviderPayment, error) {
	return nil, payments.ErrUnsupported
}

func (p *Provider) Refund(ctx context.Context, req payments.RefundRequest) (*payments.RefundResult, error) {
	in := map[string]any{"amount": amount{CurrencyCode: req.Currency, Value: Value(req.AmountMinor, req.Currency)}, "note_to_payer": "Refund from Mise"}
	var r struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	err := p.call(ctx, http.MethodPost, "/v2/payments/captures/"+url.PathEscape(req.TransactionRef)+"/refund", in, req.IdempotencyKey, &r)
	var ae *apiError
	if errors.As(err, &ae) && ae.Status == http.StatusUnprocessableEntity {
		return &payments.RefundResult{FailureMessage: "PayPal refused the refund: " + detail(ae)}, nil
	}
	if err != nil {
		return nil, err
	}
	switch r.Status {
	case "COMPLETED":
		return &payments.RefundResult{ProviderRef: r.ID, Succeeded: true}, nil
	case "PENDING":
		return nil, fmt.Errorf("paypal refund %s is pending", r.ID)
	}
	return &payments.RefundResult{ProviderRef: r.ID, FailureMessage: "Refund " + r.Status}, nil
}

// ---------------------------------------------------------------- webhooks

// ParseWebhook asks PayPal to verify the notification (postback), then reads
// which order or capture it is about. The status is fetched from PayPal.
func (p *Provider) ParseWebhook(header http.Header, body []byte) (*payments.WebhookEvent, error) {
	if p.cfg.WebhookID == "" {
		return nil, payments.ErrBadSignature
	}
	var ev struct {
		ID        string `json:"id"`
		EventType string `json:"event_type"`
		Resource  struct {
			ID                string `json:"id"`
			SupplementaryData struct {
				RelatedIDs struct {
					OrderID string `json:"order_id"`
				} `json:"related_ids"`
			} `json:"supplementary_data"`
		} `json:"resource"`
	}
	if err := json.Unmarshal(body, &ev); err != nil || ev.ID == "" {
		return nil, payments.ErrBadSignature
	}
	for _, h := range []string{"PAYPAL-TRANSMISSION-ID", "PAYPAL-TRANSMISSION-TIME", "PAYPAL-TRANSMISSION-SIG", "PAYPAL-CERT-URL", "PAYPAL-AUTH-ALGO"} {
		if header.Get(h) == "" {
			return nil, payments.ErrBadSignature
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	verify := map[string]any{
		"auth_algo":         header.Get("PAYPAL-AUTH-ALGO"),
		"cert_url":          header.Get("PAYPAL-CERT-URL"),
		"transmission_id":   header.Get("PAYPAL-TRANSMISSION-ID"),
		"transmission_sig":  header.Get("PAYPAL-TRANSMISSION-SIG"),
		"transmission_time": header.Get("PAYPAL-TRANSMISSION-TIME"),
		"webhook_id":        p.cfg.WebhookID,
	}
	// The event is posted back byte for byte as received: re-encoding it
	// (even only removing whitespace) would make PayPal's check fail.
	head, err := json.Marshal(verify)
	if err != nil {
		return nil, err
	}
	postback := make([]byte, 0, len(head)+len(body)+20)
	postback = append(postback, head[:len(head)-1]...)
	postback = append(postback, `,"webhook_event":`...)
	postback = append(postback, body...)
	postback = append(postback, '}')
	var res struct {
		VerificationStatus string `json:"verification_status"`
	}
	if err := p.call(ctx, http.MethodPost, "/v1/notifications/verify-webhook-signature", postback, "", &res); err != nil {
		return nil, fmt.Errorf("paypal webhook verification unavailable: %w", err)
	}
	if res.VerificationStatus != "SUCCESS" {
		return nil, payments.ErrBadSignature
	}
	out := &payments.WebhookEvent{ID: ev.ID, Type: ev.EventType}
	switch {
	case strings.HasPrefix(ev.EventType, "CHECKOUT.ORDER."):
		out.SessionRef = ev.Resource.ID
	case strings.HasPrefix(ev.EventType, "PAYMENT.CAPTURE."):
		if oid := ev.Resource.SupplementaryData.RelatedIDs.OrderID; oid != "" {
			out.SessionRef = oid
		} else {
			out.TransactionRef = ev.Resource.ID
		}
	}
	return out, nil
}
