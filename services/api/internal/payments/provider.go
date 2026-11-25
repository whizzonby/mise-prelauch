// Package payments takes money through a payment provider, behind an
// interface that says nothing about any one provider.
//
// Owner's decision (11 November 2026): no provider has been chosen. The only
// implementation is testpay, a local simulator that can never accept real
// money and is refused in production by configuration. A real provider is
// added by implementing Provider against its documented API; its actual
// capabilities (stored cards, off-session charges, refunds) are declared in
// Capabilities, never assumed.
//
// The rules this package keeps:
//   - Card details never reach Mise: payment is collected on the provider's
//     hosted page, and only an opaque token is stored.
//   - A payment's outcome is taken from the provider (a signed webhook, then
//     a status lookup), never from the browser coming back.
//   - Every provider call carries an idempotency key, and every webhook is
//     stored once per provider event id, so retries never charge twice.
//   - Payment status only moves forward (see advance), so events that arrive
//     late or out of order cannot undo a later state.
package payments

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// Status is the internal payment status. Provider statuses are mapped to it
// by each adapter.
type Status string

const (
	StatusPending           Status = "pending"
	StatusRequiresAction    Status = "requires_action"
	StatusProcessing        Status = "processing"
	StatusSucceeded         Status = "succeeded"
	StatusFailed            Status = "failed"
	StatusCancelled         Status = "cancelled"
	StatusRefunded          Status = "refunded"
	StatusPartiallyRefunded Status = "partially_refunded"
)

var rank = map[Status]int{
	StatusPending: 0, StatusRequiresAction: 1, StatusProcessing: 2,
	StatusFailed: 3, StatusCancelled: 3,
	StatusSucceeded: 4, StatusPartiallyRefunded: 5, StatusRefunded: 6,
}

// advance reports whether a payment may move from one status to another.
// Moves only go forward: a success is never undone by a late "processing"
// or "failed" event. Refund statuses are reached only through refunds.
func advance(from, to Status) bool {
	if from == to {
		return false
	}
	if to == StatusRefunded || to == StatusPartiallyRefunded {
		return false
	}
	if from == StatusFailed && to == StatusCancelled || from == StatusCancelled && to == StatusFailed {
		return false
	}
	return rank[to] > rank[from]
}

// Capabilities is what a provider actually supports.
type Capabilities struct {
	// OffSessionCharges: Mise can charge a saved method without the customer
	// present (needed for automatic weekly renewals).
	OffSessionCharges bool
	Refunds           bool
	// Currencies the provider can charge for this merchant account, as
	// confirmed by the provider (ISO 4217). A currency missing here is never
	// offered: Mise does not convert money to fit a provider.
	Currencies []string
	// Method is what the customer sees the provider as: "card" or "paypal".
	Method string
}

// Supports reports whether the provider can charge in currency.
func (c Capabilities) Supports(currency string) bool {
	for _, cur := range c.Currencies {
		if cur == currency {
			return true
		}
	}
	return false
}

// Readiness is whether an adapter can take payments at all, before any
// staff setting is considered: configured credentials, a published
// specification. Reason explains a "no" in plain words.
type Readiness struct {
	Ready  bool
	Reason string
	// CredentialRef is a masked reference to the credentials in use (never
	// the secret itself), for the admin page.
	CredentialRef string
}

// Describer is implemented by adapters that can say whether they are ready
// and how they present themselves. Adapters without it are ready.
type Describer interface {
	Readiness() Readiness
	DisplayName() string
}

type CheckoutRequest struct {
	// Reference is our checkout session id; also the idempotency key.
	Reference   string
	AmountMinor int64
	Currency    string
	Description string
	ReturnURL   string
	// SaveMethod asks the provider to keep the method for later charges.
	SaveMethod bool
}

type CheckoutSession struct {
	ProviderRef string
	RedirectURL string
	ExpiresAt   time.Time
}

// ProviderPayment is a payment as the provider reports it.
type ProviderPayment struct {
	// TransactionRef is the provider's id for the money movement; empty
	// while the customer has not paid yet.
	TransactionRef string
	Status         Status
	AmountMinor    int64
	Currency       string
	// MethodRef and MethodLabel describe a saved method (token), if any.
	MethodRef      string
	MethodLabel    string
	FailureCode    string
	FailureMessage string
	// Money detail, only when the provider reports it (Mise never converts):
	// what was actually charged, what settles to the merchant, the provider's
	// exchange rate (exact decimal string) and its fee.
	Money *MoneyDetail
}

// MoneyDetail is provider-reported money movement that can differ from the
// order amount. Empty fields are left unrecorded.
type MoneyDetail struct {
	ChargedCurrency       string
	ChargedAmountMinor    *int64
	SettlementCurrency    string
	SettlementAmountMinor *int64
	FXRate                string // decimal, e.g. "0.1472000000"; "" when none
	FXSource              string
	FXAt                  *time.Time
	FeeMinor              *int64
	FeeCurrency           string
}

type ChargeRequest struct {
	MethodRef      string
	AmountMinor    int64
	Currency       string
	Description    string
	Reference      string
	IdempotencyKey string
}

type RefundRequest struct {
	TransactionRef string
	AmountMinor    int64
	Currency       string
	Reason         string
	IdempotencyKey string
}

type RefundResult struct {
	ProviderRef    string
	Succeeded      bool
	FailureMessage string
}

// WebhookEvent is an authenticated notification from a provider.
type WebhookEvent struct {
	ID         string
	Type       string
	SessionRef string
	// TransactionRef is set for events about an off-session charge.
	TransactionRef string
}

// ErrBadSignature is returned by ParseWebhook for unauthenticated requests.
var ErrBadSignature = errors.New("payments: webhook signature is not valid")

// ErrUnsupported is returned for an operation the provider does not offer.
var ErrUnsupported = errors.New("payments: not supported by this provider")

// Provider is one payment provider.
// CheckoutCanceller is implemented by providers that can close a hosted
// checkout so it can no longer be paid. When Mise abandons a checkout (the
// customer started again, or the bill was voided) it closes the provider's
// session too; otherwise a second browser tab could still take the money.
// A real provider adapter must implement it if the provider allows it.
type CheckoutCanceller interface {
	CancelCheckout(ctx context.Context, sessionRef string) error
}

type Provider interface {
	Name() string
	// Live reports whether the provider moves real money.
	Live() bool
	Capabilities() Capabilities
	CreateCheckout(ctx context.Context, req CheckoutRequest) (*CheckoutSession, error)
	// CheckoutStatus asks the provider for the current state of a checkout.
	CheckoutStatus(ctx context.Context, sessionRef string) (*ProviderPayment, error)
	// TransactionStatus asks for the current state of a charge.
	TransactionStatus(ctx context.Context, transactionRef string) (*ProviderPayment, error)
	Charge(ctx context.Context, req ChargeRequest) (*ProviderPayment, error)
	Refund(ctx context.Context, req RefundRequest) (*RefundResult, error)
	// ParseWebhook authenticates a webhook request and reads it.
	ParseWebhook(header http.Header, body []byte) (*WebhookEvent, error)
}
