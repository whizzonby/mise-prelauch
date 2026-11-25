package scotiabank

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"mise.tt/api/internal/payments"
	"mise.tt/api/internal/platform/config"
)

// Scotiabank has no public integration specification: every operation must
// refuse, and the adapter must never report itself ready or able to charge.
func TestEverythingIsBlocked(t *testing.T) {
	ctx := context.Background()
	for _, p := range []*Provider{New(config.ScotiabankConfig{}), New(config.ScotiabankConfig{MerchantID: "M-123"})} {
		if r := p.Readiness(); r.Ready || r.Reason == "" {
			t.Fatalf("readiness: %+v", r)
		}
		if p.Live() {
			t.Fatal("reported live")
		}
		c := p.Capabilities()
		if len(c.Currencies) != 0 || c.OffSessionCharges || c.Refunds || c.Supports("TTD") {
			t.Fatalf("capabilities claim more than exists: %+v", c)
		}
		if _, err := p.CreateCheckout(ctx, payments.CheckoutRequest{}); !errors.Is(err, ErrBlocked) {
			t.Fatal("checkout not blocked")
		}
		if _, err := p.CheckoutStatus(ctx, "x"); !errors.Is(err, ErrBlocked) {
			t.Fatal("status not blocked")
		}
		if _, err := p.TransactionStatus(ctx, "x"); !errors.Is(err, ErrBlocked) {
			t.Fatal("transaction not blocked")
		}
		if _, err := p.Charge(ctx, payments.ChargeRequest{}); !errors.Is(err, ErrBlocked) {
			t.Fatal("charge not blocked")
		}
		if _, err := p.Refund(ctx, payments.RefundRequest{}); !errors.Is(err, ErrBlocked) {
			t.Fatal("refund not blocked")
		}
		if _, err := p.ParseWebhook(http.Header{}, []byte(`{"id":"x"}`)); !errors.Is(err, payments.ErrBadSignature) {
			t.Fatal("webhook accepted")
		}
	}
	if r := New(config.ScotiabankConfig{MerchantID: "M-123"}).Readiness(); r.CredentialRef == "M-123" {
		t.Fatal("merchant id shown unmasked")
	}
}
