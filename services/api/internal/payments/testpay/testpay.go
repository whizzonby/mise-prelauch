// Package testpay is a simulated payment provider for local development and
// staging. It stands in for a real provider's systems: a hosted checkout
// page (served by the customer app at /test-checkout/{ref}), saved test
// cards, off-session charges, refunds and HMAC-signed webhooks.
//
// It never touches real money and has no card fields: the test page offers
// buttons ("approve", "decline"). The API refuses to start with it in
// production (config: PAYMENTS_TEST_PROVIDER).
//
// Its tables (testpay_sessions, testpay_transactions) are the simulator's
// own records, as a real provider's would be; nothing else reads them. The
// SQL here is hand-written because this package plays an external system,
// not a Mise module.
package testpay

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/payments"
)

// Name is the provider name used in URLs and records.
const Name = "testpay"

// SignatureHeader carries "t=<unix seconds>,v1=<hex HMAC-SHA256>".
const SignatureHeader = "Testpay-Signature"

// Card outcomes for the saved test card.
const (
	CardApprove = "approve"
	CardDecline = "decline"
)

type Provider struct {
	pool *pgxpool.Pool
	// Secret signs webhooks (from configuration, never shown to browsers).
	secret []byte
	// PageURL is the customer app's test checkout page, e.g.
	// "http://localhost:3102/test-checkout".
	PageURL string
	// Deliver sends a signed webhook to Mise. The server wires it to the
	// webhook endpoint's code path.
	Deliver func(ctx context.Context, header http.Header, body []byte) error
	Now     func() time.Time
}

func New(pool *pgxpool.Pool, secret []byte, pageURL string) *Provider {
	return &Provider{pool: pool, secret: secret, PageURL: strings.TrimRight(pageURL, "/"), Now: time.Now}
}

func (p *Provider) Name() string { return Name }
func (p *Provider) Live() bool   { return false }
func (p *Provider) Capabilities() payments.Capabilities {
	return payments.Capabilities{OffSessionCharges: true, Refunds: true}
}

func token(prefix string) string {
	b := make([]byte, 18)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// refFor derives a stable reference from an idempotency key, so a repeated
// request finds the same record.
func refFor(prefix, key string) string {
	sum := sha256.Sum256([]byte(key))
	return prefix + hex.EncodeToString(sum[:15])
}

func (p *Provider) CreateCheckout(ctx context.Context, req payments.CheckoutRequest) (*payments.CheckoutSession, error) {
	ref := refFor("tps_", req.Reference)
	_, err := p.pool.Exec(ctx, `
		INSERT INTO testpay_sessions (ref, amount_minor, currency, description, return_url, reference)
		VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (ref) DO NOTHING`,
		ref, req.AmountMinor, req.Currency, req.Description, req.ReturnURL, req.Reference)
	if err != nil {
		return nil, err
	}
	return &payments.CheckoutSession{ProviderRef: ref, RedirectURL: p.PageURL + "/" + ref, ExpiresAt: p.Now().Add(time.Hour)}, nil
}

// Session is what the hosted test page shows.
type Session struct {
	Ref         string `json:"ref"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
	Description string `json:"description"`
	Status      string `json:"status"`
	ReturnURL   string `json:"return_url"`
}

var ErrNotFound = errors.New("testpay: no such session")

func (p *Provider) Session(ctx context.Context, ref string) (*Session, error) {
	var s Session
	err := p.pool.QueryRow(ctx, `SELECT ref, amount_minor, currency, description, status, return_url FROM testpay_sessions WHERE ref = $1`, ref).
		Scan(&s.Ref, &s.AmountMinor, &s.Currency, &s.Description, &s.Status, &s.ReturnURL)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &s, err
}

// Complete is the customer pressing a button on the test page. outcome is
// "approve", "decline" or "cancel"; card is what the saved test card does on
// later automatic charges. It then sends the signed webhook, as a provider
// would. Completing twice changes nothing and sends nothing new.
func (p *Provider) Complete(ctx context.Context, ref, outcome, card string) (*Session, error) {
	if card != CardDecline {
		card = CardApprove
	}
	status := map[string]string{"approve": "approved", "decline": "declined", "cancel": "cancelled"}[outcome]
	if status == "" {
		return nil, fmt.Errorf("testpay: unknown outcome %q", outcome)
	}
	var txRef string
	sent := false
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		var cur string
		var amount int64
		var currency string
		err := tx.QueryRow(ctx, `SELECT status, amount_minor, currency FROM testpay_sessions WHERE ref = $1 FOR UPDATE`, ref).Scan(&cur, &amount, &currency)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil || cur != "open" {
			return err
		}
		if status != "cancelled" {
			txRef = token("tpt_")
			txStatus := "succeeded"
			if status == "declined" {
				txStatus = "failed"
			}
			cardToken := token("tpc_") + ":" + card
			if _, err := tx.Exec(ctx, `
				INSERT INTO testpay_transactions (ref, kind, session_ref, card_token, amount_minor, currency, status, idempotency_key)
				VALUES ($1, 'checkout', $2, $3, $4, $5, $6, $1)`, txRef, ref, cardToken, amount, currency, txStatus); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE testpay_sessions SET status = $2, transaction_ref = NULLIF($3, ''), completed_at = now() WHERE ref = $1`, ref, status, txRef)
		sent = true
		return err
	})
	if err != nil {
		return nil, err
	}
	if sent {
		if err := p.send(ctx, "checkout."+status, map[string]string{"session_ref": ref}); err != nil {
			return nil, err
		}
	}
	return p.Session(ctx, ref)
}

// send signs and delivers one webhook event.
func (p *Provider) send(ctx context.Context, typ string, data map[string]string) error {
	body, _ := json.Marshal(map[string]any{"id": token("evt_"), "type": typ, "data": data})
	if p.Deliver == nil {
		return nil
	}
	return p.Deliver(ctx, p.SignedHeader(body, p.Now()), body)
}

// SignedHeader returns the header a webhook body is sent with.
func (p *Provider) SignedHeader(body []byte, at time.Time) http.Header {
	ts := strconv.FormatInt(at.Unix(), 10)
	h := http.Header{}
	h.Set(SignatureHeader, "t="+ts+",v1="+p.sign(ts, body))
	h.Set("Content-Type", "application/json")
	return h
}

func (p *Provider) sign(ts string, body []byte) string {
	mac := hmac.New(sha256.New, p.secret)
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// ParseWebhook checks the signature (constant-time) and a five-minute
// timestamp window, which stops old requests being replayed.
func (p *Provider) ParseWebhook(header http.Header, body []byte) (*payments.WebhookEvent, error) {
	var ts, sig string
	for _, part := range strings.Split(header.Get(SignatureHeader), ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "t":
			ts = v
		case "v1":
			sig = v
		}
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || sig == "" {
		return nil, payments.ErrBadSignature
	}
	if d := p.Now().Sub(time.Unix(sec, 0)); d > 5*time.Minute || d < -5*time.Minute {
		return nil, payments.ErrBadSignature
	}
	want, _ := hex.DecodeString(p.sign(ts, body))
	got, err := hex.DecodeString(sig)
	if err != nil || !hmac.Equal(want, got) {
		return nil, payments.ErrBadSignature
	}
	var ev struct {
		ID   string            `json:"id"`
		Type string            `json:"type"`
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(body, &ev); err != nil || ev.ID == "" || ev.Type == "" {
		return nil, fmt.Errorf("testpay: malformed event")
	}
	return &payments.WebhookEvent{ID: ev.ID, Type: ev.Type, SessionRef: ev.Data["session_ref"], TransactionRef: ev.Data["transaction_ref"]}, nil
}

func label(cardToken string) string {
	if strings.HasSuffix(cardToken, ":"+CardDecline) {
		return "Test card (declines later charges)"
	}
	return "Test card"
}

func (p *Provider) CheckoutStatus(ctx context.Context, ref string) (*payments.ProviderPayment, error) {
	var status, currency string
	var amount int64
	var txRef, card, txStatus *string
	err := p.pool.QueryRow(ctx, `
		SELECT s.status, s.amount_minor, s.currency, t.ref, t.card_token, t.status
		FROM testpay_sessions s LEFT JOIN testpay_transactions t ON t.ref = s.transaction_ref
		WHERE s.ref = $1`, ref).Scan(&status, &amount, &currency, &txRef, &card, &txStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	out := &payments.ProviderPayment{AmountMinor: amount, Currency: currency, Status: payments.StatusPending}
	switch status {
	case "approved":
		out.Status = payments.StatusSucceeded
	case "declined":
		out.Status, out.FailureCode, out.FailureMessage = payments.StatusFailed, "card_declined", "The test card was declined."
	case "cancelled":
		out.Status = payments.StatusCancelled
	}
	if txRef != nil {
		out.TransactionRef = *txRef
	}
	if card != nil && out.Status == payments.StatusSucceeded {
		out.MethodRef, out.MethodLabel = *card, label(*card)
	}
	return out, nil
}

func (p *Provider) TransactionStatus(ctx context.Context, ref string) (*payments.ProviderPayment, error) {
	var status, currency string
	var amount int64
	err := p.pool.QueryRow(ctx, `SELECT status, amount_minor, currency FROM testpay_transactions WHERE ref = $1`, ref).Scan(&status, &amount, &currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	out := &payments.ProviderPayment{TransactionRef: ref, AmountMinor: amount, Currency: currency, Status: payments.StatusSucceeded}
	if status == "failed" {
		out.Status, out.FailureCode, out.FailureMessage = payments.StatusFailed, "card_declined", "The saved test card was declined."
	}
	return out, nil
}

// Charge charges a saved test card. Cards saved as "decline" fail. The
// idempotency key is unique, so a repeated request returns the first result.
func (p *Provider) Charge(ctx context.Context, req payments.ChargeRequest) (*payments.ProviderPayment, error) {
	var exists bool
	if err := p.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM testpay_transactions WHERE card_token = $1)`, req.MethodRef).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("testpay: unknown card")
	}
	status := "succeeded"
	if strings.HasSuffix(req.MethodRef, ":"+CardDecline) {
		status = "failed"
	}
	ref := refFor("tpt_", req.IdempotencyKey)
	if _, err := p.pool.Exec(ctx, `
		INSERT INTO testpay_transactions (ref, kind, card_token, amount_minor, currency, status, idempotency_key)
		VALUES ($1, 'charge', $2, $3, $4, $5, $6) ON CONFLICT (idempotency_key) DO NOTHING`,
		ref, req.MethodRef, req.AmountMinor, req.Currency, status, req.IdempotencyKey); err != nil {
		return nil, err
	}
	return p.TransactionStatus(ctx, ref)
}

func (p *Provider) Refund(ctx context.Context, req payments.RefundRequest) (*payments.RefundResult, error) {
	ref := refFor("tpr_", req.IdempotencyKey)
	if _, err := p.pool.Exec(ctx, `
		INSERT INTO testpay_transactions (ref, kind, parent_ref, amount_minor, currency, status, idempotency_key)
		VALUES ($1, 'refund', $2, $3, $4, 'succeeded', $5) ON CONFLICT (idempotency_key) DO NOTHING`,
		ref, req.TransactionRef, req.AmountMinor, req.Currency, req.IdempotencyKey); err != nil {
		return nil, err
	}
	return &payments.RefundResult{ProviderRef: ref, Succeeded: true}, nil
}

// SendTransactionEvent re-sends the webhook for a charge (tests use it to
// deliver duplicates and out-of-order events).
func (p *Provider) SendTransactionEvent(ctx context.Context, txRef, typ string) error {
	return p.send(ctx, typ, map[string]string{"transaction_ref": txRef})
}
