// Package scotiabank registers Scotia eCom+ Hosted Payments (Scotiabank
// Trinidad and Tobago) as a provider, without processing anything.
//
// What is published (tt.scotiabank.com, read 25 November 2026): Scotia eCom+
// offers a Virtual Terminal and "Hosted Payments", where the merchant site
// redirects customers to Scotiabank's secure payment page. Onboarding is by
// email to merchant services. No integration specification is public: no
// endpoints, authentication, callback or signature format, transaction
// reference format, refund API, tokenisation or sandbox.
//
// So this adapter only declares the provider and its boundaries. Every
// payment operation is refused, and Readiness always says why. Nothing here
// is guessed. When Scotiabank supplies the merchant integration guide,
// implement CreateCheckout, CheckoutStatus, ParseWebhook and Refund from it
// (see docs/integrations/SCOTIABANK.md).
package scotiabank

import (
	"context"
	"errors"
	"net/http"

	"mise.tt/api/internal/payments"
	"mise.tt/api/internal/platform/config"
)

const Name = "scotiabank"

// ErrBlocked is returned by every payment operation.
var ErrBlocked = errors.New("scotiabank: payment processing is blocked until Scotiabank provides the merchant integration specification")

type Provider struct{ cfg config.ScotiabankConfig }

func New(cfg config.ScotiabankConfig) *Provider { return &Provider{cfg: cfg} }

func (p *Provider) Name() string        { return Name }
func (p *Provider) DisplayName() string { return "Card (Scotiabank)" }
func (p *Provider) Live() bool          { return false }

// Capabilities are unknown, so none are claimed.
func (p *Provider) Capabilities() payments.Capabilities {
	return payments.Capabilities{Method: "card"}
}

func (p *Provider) Readiness() payments.Readiness {
	ref := ""
	if p.cfg.MerchantID != "" {
		ref = "merchant id set"
	}
	return payments.Readiness{CredentialRef: ref,
		Reason: "Blocked: Scotiabank has not provided a merchant integration specification (endpoints, authentication, callback verification), so no payment can be processed."}
}

func (p *Provider) CreateCheckout(context.Context, payments.CheckoutRequest) (*payments.CheckoutSession, error) {
	return nil, ErrBlocked
}
func (p *Provider) CheckoutStatus(context.Context, string) (*payments.ProviderPayment, error) {
	return nil, ErrBlocked
}
func (p *Provider) TransactionStatus(context.Context, string) (*payments.ProviderPayment, error) {
	return nil, ErrBlocked
}
func (p *Provider) Charge(context.Context, payments.ChargeRequest) (*payments.ProviderPayment, error) {
	return nil, ErrBlocked
}
func (p *Provider) Refund(context.Context, payments.RefundRequest) (*payments.RefundResult, error) {
	return nil, ErrBlocked
}

// ParseWebhook refuses everything: there is no documented way to verify a
// Scotiabank callback, and an unverified callback must never move money.
func (p *Provider) ParseWebhook(http.Header, []byte) (*payments.WebhookEvent, error) {
	return nil, payments.ErrBadSignature
}
