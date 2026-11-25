package commerce

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/addresses"
	"mise.tt/api/internal/billing"
	"mise.tt/api/internal/delivery"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/menus"
	"mise.tt/api/internal/payments"
	"mise.tt/api/internal/plans"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/outbox"
	"mise.tt/api/internal/subscriptions"
)

// ---------------------------------------------------------------- quote

type QuoteInput struct {
	PlanID        string `json:"plan_id"`
	PromotionCode string `json:"promotion_code"`
}

// Quote is everything shown before payment, all computed on the server.
type Quote struct {
	Plan           plans.Plan      `json:"plan"`
	FirstWeek      string          `json:"first_week"`
	FirstWeekLabel string          `json:"first_week_label"`
	CutoffAt       string          `json:"first_week_cutoff_at"`
	DueNow         billing.Amounts `json:"due_now"`
	Renewal        billing.Amounts `json:"renewal"`
	Promotion      *string         `json:"promotion"`
	Terms          string          `json:"terms"`
	TaxNote        string          `json:"tax_note"`
	Payments       PaymentsInfo    `json:"payments"`
}

type PaymentsInfo struct {
	Available bool   `json:"available"`
	Test      bool   `json:"test"`
	Provider  string `json:"provider,omitempty"`
}

func (s *Service) paymentsInfo() PaymentsInfo {
	name, live, _, ok := s.Payments.Available()
	return PaymentsInfo{Available: ok, Test: ok && !live, Provider: name}
}

func (s *Service) Quote(ctx context.Context, p *identity.Principal, in QuoteInput) (*Quote, error) {
	cid, err := s.customerID(ctx, p)
	if err != nil {
		return nil, err
	}
	in.PlanID = strings.ToLower(strings.TrimSpace(in.PlanID))
	selling, err := s.Plans.ForPurchase(ctx, s.pool, in.PlanID)
	if err != nil {
		return nil, err
	}
	plan, err := s.Plans.PublicByID(ctx, in.PlanID)
	if err != nil {
		return nil, err
	}
	var disc *billing.DiscountSnapshot
	var promo *string
	if strings.TrimSpace(in.PromotionCode) != "" {
		a, err := s.Billing.Check(ctx, s.pool, in.PromotionCode, selling.PlanID, cid)
		if err != nil {
			return nil, err
		}
		disc, promo = &a.Snapshot, &a.Description
	}
	week := s.Menus.Policy.NextOpenWeek(s.Now())
	due := s.Billing.Policy.Quote(selling.AmountMinor, selling.Currency, disc)
	return &Quote{
		Plan: *plan, FirstWeek: menus.FormatWeek(week), FirstWeekLabel: weekLabel(week),
		CutoffAt: s.Menus.Policy.CutoffFor(week).Format("2006-01-02T15:04:05Z07:00"),
		DueNow:   due, Renewal: s.Billing.Policy.Quote(selling.AmountMinor, selling.Currency, nil),
		Promotion: promo, Terms: s.Terms(selling.AmountMinor, selling.Currency),
		TaxNote: billing.TaxNote(due.Tax, due.TaxMinor, due.Currency), Payments: s.paymentsInfo(),
	}, nil
}

// ---------------------------------------------------------------- subscribe

type SubscribeInput struct {
	PlanID        string `json:"plan_id"`
	AddressID     string `json:"address_id"`
	PromotionCode string `json:"promotion_code"`
	// Provider is the payment method the customer chose ("" = the first available).
	Provider string `json:"provider"`
}

type SubscribeResult struct {
	SubscriptionID string             `json:"subscription_id"`
	Checkout       *payments.Checkout `json:"checkout"`
	// Activated: nothing was due (a promotion made the first week free).
	Activated bool `json:"activated"`
}

// Subscribe creates a subscription waiting for payment and opens the
// provider's checkout for its first week. The subscription becomes active
// only when the provider confirms the payment (PaymentSucceeded).
func (s *Service) Subscribe(ctx context.Context, p *identity.Principal, in SubscribeInput) (*SubscribeResult, error) {
	if _, _, _, ok := s.Payments.Available(); !ok {
		return nil, payments.ErrNoProvider
	}
	cid, err := s.customerID(ctx, p)
	if err != nil {
		return nil, err
	}
	in.PlanID = strings.ToLower(strings.TrimSpace(in.PlanID))
	in.AddressID = strings.ToLower(strings.TrimSpace(in.AddressID))
	var sub *subscriptions.Subscription
	var bc *billing.Cycle
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		live, err := s.Subs.LiveFor(ctx, tx, cid)
		if err != nil {
			return err
		}
		if live != nil {
			if subscriptions.Status(live.Status) != subscriptions.PendingPayment {
				return httpx.NewError(http.StatusConflict, "subscription_exists", "You already have a subscription. Manage it from your subscription page.")
			}
			// Starting again replaces an unfinished one.
			locked, err := s.Subs.Get(ctx, tx, live.ID, true)
			if err != nil {
				return err
			}
			if err := s.expirePending(ctx, tx, locked, p.UserID, "replaced"); err != nil {
				return err
			}
		}
		selling, err := s.Plans.ForPurchase(ctx, tx, in.PlanID)
		if err != nil {
			return err
		}
		addr, err := addresses.Lookup(ctx, tx, s.zones(tx), cid, in.AddressID)
		if errors.Is(err, httpx.ErrNotFound) {
			return httpx.ValidationError([]httpx.FieldError{{Field: "address_id", Code: "required", Message: "Choose a delivery address."}})
		}
		if err != nil {
			return err
		}
		if addr.DeliveryAvailability != delivery.Available {
			return httpx.ValidationError([]httpx.FieldError{{Field: "address_id", Code: "not_served",
				Message: "Mise does not deliver to " + addr.RegionLabel + " yet. Choose another address."}})
		}
		var promo *billing.Applicable
		if strings.TrimSpace(in.PromotionCode) != "" {
			if promo, err = s.Billing.Check(ctx, tx, in.PromotionCode, selling.PlanID, cid); err != nil {
				return err
			}
		}
		var cycle *subscriptions.Cycle
		sub, cycle, err = s.Subs.CreatePending(ctx, tx, subscriptions.NewSubscription{
			CustomerID: cid, PlanID: selling.PlanID, PlanPriceID: selling.PriceID, MealsPerWeek: selling.MealsPerWeek,
			AmountMinor: selling.AmountMinor, Currency: selling.Currency, AddressID: addr.ID, Actor: p.UserID,
		})
		if err != nil {
			return err
		}
		if promo != nil {
			if err := s.Billing.Reserve(ctx, tx, promo, sub.ID, cid); err != nil {
				return err
			}
		}
		bc, _, err = s.Billing.CreateCycle(ctx, tx, billing.NewCycle{
			SubscriptionID: sub.ID, SubscriptionCycleID: cycle.ID, CustomerID: cid, Kind: "initial", Week: cycle.WeekStart,
			DueAt: s.Now(), SubtotalMinor: cycle.AmountMinor, Currency: cycle.Currency, Status: "pending",
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	res := &SubscribeResult{SubscriptionID: sub.ID}
	if bc.TotalMinor == 0 {
		if err := s.settleFree(ctx, bc.ID); err != nil {
			return nil, err
		}
		res.Activated = true
		return res, nil
	}
	res.Checkout, err = s.startCheckout(ctx, cid, bc, in.Provider)
	var he *httpx.Error
	if errors.As(err, &he) && he.Code == "nothing_to_pay" {
		// A newer attempt (a second tab, a double click) replaced this one while it was starting.
		return nil, httpx.NewError(http.StatusConflict, "subscription_exists", "You already have a subscription. Manage it from your subscription page.")
	}
	return res, err
}

func (s *Service) startCheckout(ctx context.Context, customerID string, bc *billing.Cycle, provider string) (*payments.Checkout, error) {
	// In a transaction, so MarkDue's row lock holds: on the bare pool it read "pending", and a
	// replacement that voided the bill a moment later was overwritten back to "due".
	if err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error { return s.Billing.MarkDue(ctx, tx, bc.ID) }); err != nil {
		return nil, err
	}
	desc := "Mise subscription, week of " + bc.PeriodStart
	return s.Payments.StartCheckout(ctx, payments.CheckoutInput{
		CustomerID: customerID, BillingCycleID: bc.ID, AmountMinor: bc.TotalMinor, Currency: bc.Currency,
		Description: desc, ReturnURL: s.link("/subscribe/result"), Provider: provider,
	})
}

// settleFree completes a billing cycle that costs nothing (a promotion
// covered it) without a provider: there is no money to collect.
func (s *Service) settleFree(ctx context.Context, billingCycleID string) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		return s.paid(ctx, tx, payments.Outcome{BillingCycleID: billingCycleID})
	})
}

// PayInput asks to pay a billing cycle now.
type PayInput struct {
	BillingCycleID string `json:"billing_cycle_id"`
	Provider       string `json:"provider"`
}

// Pay opens a checkout for one of the customer's unpaid weeks: an
// unfinished first payment, or a renewal that is due or failed. Only weeks
// whose selection has not closed can be paid.
func (s *Service) Pay(ctx context.Context, p *identity.Principal, in PayInput) (*payments.Checkout, error) {
	cid, err := s.customerID(ctx, p)
	if err != nil {
		return nil, err
	}
	bc, err := s.Billing.Get(ctx, s.pool, strings.ToLower(strings.TrimSpace(in.BillingCycleID)), false)
	if err != nil || bc.CustomerID != cid {
		return nil, httpx.ErrNotFound
	}
	if !bc.Payable() {
		return nil, billing.ErrNotPayable()
	}
	cycle, err := s.Subs.Cycle(ctx, s.pool, bc.SubscriptionCycleID, false)
	if err != nil {
		return nil, err
	}
	sub, err := s.Subs.Get(ctx, s.pool, bc.SubscriptionID, false)
	if err != nil {
		return nil, err
	}
	if !subscriptions.Status(sub.Status).Live() || cycle.State != "scheduled" || !s.Now().Before(cycle.CutoffAt) {
		return nil, billing.ErrNotPayable()
	}
	return s.startCheckout(ctx, cid, bc, in.Provider)
}

// CheckoutResult is what the result page shows. It reports what the
// server knows; the page never decides on its own that a payment worked.
type CheckoutResult struct {
	Checkout           payments.Checkout `json:"checkout"`
	Outcome            string            `json:"outcome"` // pending, succeeded, failed, expired
	SubscriptionStatus string            `json:"subscription_status"`
	Week               string            `json:"week"`
	WeekLabel          string            `json:"week_label"`
	InvoiceID          *string           `json:"invoice_id"`
}

func (s *Service) CheckoutResult(ctx context.Context, p *identity.Principal, id string) (*CheckoutResult, error) {
	cid, err := s.customerID(ctx, p)
	if err != nil {
		return nil, err
	}
	c, err := s.Payments.CheckoutForCustomer(ctx, cid, id)
	if err != nil {
		return nil, err
	}
	bc, err := s.Billing.Get(ctx, s.pool, c.BillingCycleID, false)
	if err != nil {
		return nil, err
	}
	sub, err := s.Subs.Get(ctx, s.pool, bc.SubscriptionID, false)
	if err != nil {
		return nil, err
	}
	res := &CheckoutResult{Checkout: *c, Outcome: "pending", SubscriptionStatus: sub.Status, Week: bc.PeriodStart}
	if w, ok := menus.ParseWeek(bc.PeriodStart); ok {
		res.WeekLabel = weekLabel(w)
	}
	switch {
	case bc.Status == "paid":
		res.Outcome = "succeeded"
		if id, _, ok := s.Billing.InvoiceRef(ctx, s.pool, bc.ID); ok {
			res.InvoiceID = &id
		}
	case c.Status == "failed" || c.Status == "cancelled":
		res.Outcome = "failed"
	case c.Status == "expired":
		res.Outcome = "expired"
	}
	return res, nil
}

// ---------------------------------------------------------------- payment outcomes

// Hooks adapts the service to payments.Hooks.
type Hooks struct{ S *Service }

func (h Hooks) PaymentSucceeded(ctx context.Context, q db.Querier, o payments.Outcome) error {
	return h.S.paid(ctx, q, o)
}

func (h Hooks) PaymentFailed(ctx context.Context, q db.Querier, o payments.Outcome) error {
	return h.S.paymentFailed(ctx, q, o)
}

func (h Hooks) CheckoutExpired(ctx context.Context, q db.Querier, billingCycleID string) error {
	// Nothing changes yet: the customer can start another checkout, and an
	// unfinished subscription expires after PendingTTL (scheduler).
	return nil
}

// paid applies a successful payment (or a free week, o.PaymentID == "").
// Retried calls change nothing: the billing cycle is already paid.
func (s *Service) paid(ctx context.Context, q db.Querier, o payments.Outcome) error {
	bc, err := s.Billing.Get(ctx, q, o.BillingCycleID, true)
	if err != nil {
		return err
	}
	sub, err := s.Subs.Get(ctx, q, bc.SubscriptionID, true)
	if err != nil {
		return err
	}
	flag := func(reason string) error {
		slog.WarnContext(ctx, "payment needs attention", "payment_id", o.PaymentID, "reason", reason)
		if o.PaymentID == "" {
			return nil
		}
		return s.Payments.Flag(ctx, q, o.PaymentID, reason)
	}
	if bc.Status == "paid" {
		if existing, err := s.Payments.SucceededFor(ctx, q, bc.ID); err == nil && existing != nil && existing.ID != o.PaymentID && o.PaymentID != "" {
			return flag("This week was already paid by another payment. Refund this one.")
		}
		return nil
	}
	if bc.Status == "void" {
		return flag("Payment arrived after this week was closed or cancelled. Refund it, or contact the customer.")
	}
	cycle, err := s.Subs.Cycle(ctx, q, bc.SubscriptionCycleID, true)
	if err != nil {
		return err
	}
	status := subscriptions.Status(sub.Status)
	if !status.Live() {
		return flag("Payment arrived after the subscription had ended. Refund it.")
	}
	now := s.Now()
	if status == subscriptions.PendingPayment {
		// The first week moves forward if its menu closed while the customer
		// was paying. Nothing has been delivered or ordered for it.
		if !now.Before(cycle.CutoffAt) {
			if _, err := s.Subs.SetCycleState(ctx, q, cycle.ID, "cancelled", "paid_after_cutoff"); err != nil {
				return err
			}
			next, _, err := s.Subs.EnsureCycle(ctx, q, sub, s.Menus.Policy.NextOpenWeek(now))
			if err != nil {
				return err
			}
			if err := s.Billing.Move(ctx, q, bc.ID, next.ID, next.WeekStart); err != nil {
				return err
			}
			cycle = next
		}
	} else if cycle.State != "scheduled" || !now.Before(cycle.CutoffAt) {
		return flag("Payment arrived after this week's selection closed. Refund it, or contact the customer.")
	}
	paidCycle, _, err := s.Billing.MarkPaid(ctx, q, bc.ID, s.contactSnapshot(ctx, q, sub))
	if err != nil {
		return err
	}
	if status != subscriptions.PendingPayment {
		// A further week paid on a running subscription: a renewal. (The
		// first week is the activation, published by the status change.)
		if err := outbox.Record(ctx, q, outbox.Event{Type: "subscription.renewed", AggregateType: "subscription", AggregateID: sub.ID,
			DedupeKey: bc.ID, Payload: map[string]any{"subscription_id": sub.ID, "customer_id": sub.CustomerID, "billing_cycle_id": bc.ID,
				"week": menus.FormatWeek(cycle.WeekStart), "payment_id": o.PaymentID, "total_minor": paidCycle.TotalMinor,
				"currency": paidCycle.Currency}}); err != nil {
			return err
		}
	}
	if o.PaymentMethodID != "" && (sub.PaymentMethodID == nil || *sub.PaymentMethodID != o.PaymentMethodID) {
		if err := s.Subs.SetPaymentMethod(ctx, q, sub, o.PaymentMethodID); err != nil {
			return err
		}
	}
	if err := s.Payments.CancelOpenCheckouts(ctx, q, bc.ID); err != nil {
		return err
	}
	switch status {
	case subscriptions.PendingPayment:
		if err := s.Subs.Transition(ctx, q, sub, subscriptions.Active, subscriptions.Event{Type: "activated", EffectiveWeek: &cycle.WeekStart,
			Metadata: map[string]any{"payment_id": o.PaymentID}}); err != nil {
			return err
		}
		if err := s.notify(ctx, q, "activated:"+sub.ID, EmailActivated, sub.CustomerID, map[string]string{
			"week": weekLabel(cycle.WeekStart), "cutoff": localTime(cycle.CutoffAt), "link": s.link("/menu"),
		}); err != nil {
			return err
		}
	case subscriptions.PastDue:
		if err := s.Subs.Transition(ctx, q, sub, subscriptions.Active, subscriptions.Event{Type: "payment_recovered",
			EffectiveWeek: &cycle.WeekStart}); err != nil {
			return err
		}
	}
	data := map[string]string{"week": weekLabel(cycle.WeekStart), "amount": formatMoney(paidCycle.TotalMinor, paidCycle.Currency),
		"link": s.link("/billing")}
	if paidCycle.InvoiceNumber != nil {
		data["invoice"] = *paidCycle.InvoiceNumber
		data["link"] = s.link("/billing/invoices/" + *paidCycle.InvoiceID)
	}
	return s.notify(ctx, q, "receipt:"+bc.ID, EmailReceipt, sub.CustomerID, data)
}

func (s *Service) paymentFailed(ctx context.Context, q db.Querier, o payments.Outcome) error {
	bc, err := s.Billing.Get(ctx, q, o.BillingCycleID, true)
	if err != nil {
		return err
	}
	if bc.Status == "paid" || bc.Status == "void" {
		return nil
	}
	if _, err := s.Billing.MarkFailed(ctx, q, bc.ID); err != nil {
		return err
	}
	sub, err := s.Subs.Get(ctx, q, bc.SubscriptionID, true)
	if err != nil {
		return err
	}
	if bc.Kind == "initial" {
		// The customer is on the result page and can try again; no email.
		return nil
	}
	cycle, err := s.Subs.Cycle(ctx, q, bc.SubscriptionCycleID, false)
	if err != nil {
		return err
	}
	st := subscriptions.Status(sub.Status)
	if st == subscriptions.Active || st == subscriptions.PauseScheduled {
		if err := s.Subs.Transition(ctx, q, sub, subscriptions.PastDue, subscriptions.Event{Type: "renewal_failed",
			EffectiveWeek: &cycle.WeekStart, Metadata: map[string]any{"payment_id": o.PaymentID}}); err != nil {
			return err
		}
	}
	return s.notify(ctx, q, "payment_failed:"+o.PaymentID, EmailPaymentFailed, sub.CustomerID, map[string]string{
		"week": weekLabel(cycle.WeekStart), "amount": formatMoney(bc.TotalMinor, bc.Currency),
		"cutoff": localTime(cycle.CutoffAt), "link": s.link("/subscription"),
	})
}

// expirePending ends a subscription that never got its first payment:
// its first week is cancelled, the billing voided, any promotion use given
// back and open checkouts closed.
func (s *Service) expirePending(ctx context.Context, q db.Querier, sub *subscriptions.Subscription, actor, reason string) error {
	cycles, err := s.Subs.ScheduledCycles(ctx, q, sub.ID)
	if err != nil {
		return err
	}
	for _, c := range cycles {
		bc, err := s.Billing.ForSubscriptionCycle(ctx, q, c.ID)
		if err != nil {
			return err
		}
		if bc != nil {
			if bc.Status == "paid" {
				// Paid after all: not ours to expire.
				return nil
			}
			if err := s.Billing.Void(ctx, q, bc.ID, "not_started"); err != nil {
				return err
			}
			if err := s.Payments.CancelOpenCheckouts(ctx, q, bc.ID); err != nil {
				return err
			}
		}
		if _, err := s.Subs.SetCycleState(ctx, q, c.ID, "cancelled", "not_started"); err != nil {
			return err
		}
	}
	if err := s.Billing.Release(ctx, q, sub.ID); err != nil {
		return err
	}
	return s.Subs.Transition(ctx, q, sub, subscriptions.Expired, subscriptions.Event{Type: "expired", Actor: actor,
		Metadata: map[string]any{"reason": reason}})
}
