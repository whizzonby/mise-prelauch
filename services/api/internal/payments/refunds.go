package payments

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/payments/store"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
)

// RefundInput is a refund staff ask for. Refunds are never automatic: the
// pause, cancellation and refund policies are open business decisions, so a
// person with payments.refund decides each one.
type RefundInput struct {
	AmountMinor int64  `json:"amount_minor"`
	Reason      string `json:"reason"`
	// RequestKey makes a double-submitted form one refund. The admin app
	// sends a fresh random key with each form it shows.
	RequestKey string `json:"request_key"`
}

// Refund refunds part or all of a successful payment through its provider
// and records it. The refund row is written (pending) before the provider is
// called, with the idempotency key the provider also receives.
func (s *Service) Refund(ctx context.Context, actor, paymentID string, in RefundInput) (*PaymentDetail, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	in.RequestKey = strings.TrimSpace(in.RequestKey)
	var f []httpx.FieldError
	if in.AmountMinor <= 0 {
		f = append(f, httpx.FieldError{Field: "amount_minor", Code: "range", Message: "Enter an amount above zero."})
	}
	if n := len([]rune(in.Reason)); n < 3 || n > 300 {
		f = append(f, httpx.FieldError{Field: "reason", Code: "length", Message: "Say why, in 3 to 300 characters."})
	}
	if len(in.RequestKey) < 8 || len(in.RequestKey) > 100 {
		f = append(f, httpx.FieldError{Field: "request_key", Code: "required", Message: "Reload the form and try again."})
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	key := "refund:" + paymentID + ":" + in.RequestKey

	var refund store.Refund
	var pay store.Payment
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		var err error
		pay, err = q.GetPaymentForUpdate(ctx, paymentID)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		if existing, err := q.RefundByIdempotencyKey(ctx, key); err == nil {
			refund = existing
			return nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		st := Status(pay.Status)
		if st != StatusSucceeded && st != StatusPartiallyRefunded {
			return httpx.NewError(http.StatusConflict, "not_refundable", "Only a successful payment can be refunded.")
		}
		pending, err := q.PendingRefundTotal(ctx, paymentID)
		if err != nil {
			return err
		}
		if in.AmountMinor > pay.AmountMinor-pay.RefundedMinor-pending {
			return httpx.ValidationError([]httpx.FieldError{{Field: "amount_minor", Code: "too_much",
				Message: "That is more than is left to refund on this payment."}})
		}
		p, ok := s.providers[pay.Provider]
		if !ok || !p.Capabilities().Refunds {
			return httpx.NewError(http.StatusConflict, "refunds_unsupported", "This payment's provider cannot refund through Mise.")
		}
		refund, err = q.InsertRefund(ctx, store.InsertRefundParams{PaymentID: paymentID, AmountMinor: in.AmountMinor,
			Currency: pay.Currency, Reason: in.Reason, IdempotencyKey: key, RequestedBy: nilIfEmpty(actor)})
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "refund.requested", TargetType: "payment", TargetID: paymentID,
			Metadata: map[string]any{"refund_id": refund.ID, "amount_minor": in.AmountMinor}})
	})
	if err != nil {
		return nil, err
	}
	if refund.Status != "pending" {
		return s.Detail(ctx, paymentID)
	}
	if pay.ProviderRef == nil {
		return nil, fmt.Errorf("payment %s has no provider reference", paymentID)
	}
	p := s.providers[pay.Provider]
	res, err := p.Refund(ctx, RefundRequest{TransactionRef: *pay.ProviderRef, AmountMinor: refund.AmountMinor,
		Currency: refund.Currency, Reason: refund.Reason, IdempotencyKey: key})
	if err != nil {
		// Left pending: the same request key retries it safely.
		slog.ErrorContext(ctx, "refund call failed", "payment_id", paymentID, "error", err.Error())
		return nil, httpx.NewError(http.StatusBadGateway, "payment_provider_error", "The provider did not confirm the refund. Try again; it will not be refunded twice.")
	}
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		status := "failed"
		if res.Succeeded {
			status = "succeeded"
		}
		var ref *string
		if res.ProviderRef != "" {
			ref = &res.ProviderRef
		}
		if _, err := q.SetRefundResult(ctx, store.SetRefundResultParams{ID: refund.ID, Status: status, ProviderRef: ref}); errors.Is(err, pgx.ErrNoRows) {
			return nil // recorded by a concurrent retry
		} else if err != nil {
			return err
		}
		if !res.Succeeded {
			return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "refund.failed", TargetType: "payment", TargetID: paymentID,
				Metadata: map[string]any{"refund_id": refund.ID}})
		}
		updated, err := q.AddRefunded(ctx, store.AddRefundedParams{ID: paymentID, AmountMinor: refund.AmountMinor})
		if err != nil {
			return err
		}
		from := pay.Status
		if err := q.AddPaymentHistory(ctx, store.AddPaymentHistoryParams{PaymentID: paymentID, FromStatus: &from,
			ToStatus: updated.Status, Source: "refund"}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "refund.succeeded", TargetType: "payment", TargetID: paymentID,
			Metadata: map[string]any{"refund_id": refund.ID, "amount_minor": refund.AmountMinor, "status": updated.Status}})
	})
	if err != nil {
		return nil, err
	}
	return s.Detail(ctx, paymentID)
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
