// Package billing owns the money side of a subscription week: billing
// cycles (what is owed for one week and its state), invoices (issued once,
// with a gapless yearly number, never edited) and promotions.
//
// Amounts are computed here and only here, from server-side data: a plan
// price snapshot, an approved promotion, and the tax and delivery-fee policy
// in configuration. The browser never supplies an amount.
//
// Tax and delivery fees are an open business decision (11 November 2026).
// The default policy charges neither; invoices then say no tax is charged.
// Each billing cycle and invoice records the policy it was computed with.
package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/billing/store"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/money"
)

// Tax modes.
const (
	TaxNone      = "none"      // no tax charged
	TaxInclusive = "inclusive" // prices include tax; invoices show the tax part
	TaxExclusive = "exclusive" // tax is added on top
)

// Policy is the billing configuration in force.
type Policy struct {
	TaxMode          string
	TaxRateBps       int64
	TaxLabel         string
	TaxRegistration  string
	DeliveryFeeMinor int64
	BusinessName     string
	BusinessAddress  string
	// RenewalLead: a week is billed this long before its cutoff.
	RenewalLead time.Duration
	// RetryLimit failed renewal charges are retried this many times, each
	// RetryAfter later (default 0: no automatic retries).
	RetryLimit int
	RetryAfter time.Duration
}

// Validate checks a policy read from configuration.
func (p Policy) Validate() error {
	switch p.TaxMode {
	case TaxNone:
	case TaxInclusive, TaxExclusive:
		if p.TaxRateBps <= 0 || p.TaxRateBps > 5000 {
			return errors.New("BILLING_TAX_RATE_BPS must be between 1 and 5000 when tax is charged")
		}
	default:
		return fmt.Errorf("BILLING_TAX_MODE must be none, inclusive or exclusive, got %q", p.TaxMode)
	}
	if p.DeliveryFeeMinor < 0 {
		return errors.New("BILLING_DELIVERY_FEE_MINOR cannot be negative")
	}
	if p.RenewalLead < 0 || p.RetryLimit < 0 || p.RetryLimit > 5 {
		return errors.New("billing renewal settings are out of range")
	}
	return nil
}

// TaxSnapshot is the tax rule an amount was computed with.
type TaxSnapshot struct {
	Mode         string `json:"mode"`
	RateBps      int64  `json:"rate_bps"`
	Label        string `json:"label"`
	Registration string `json:"registration,omitempty"`
}

// DiscountSnapshot is the promotion an amount was computed with.
type DiscountSnapshot struct {
	PromotionID string `json:"promotion_id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	PercentBps  int64  `json:"percent_bps,omitempty"`
	AmountMinor int64  `json:"amount_minor,omitempty"`
}

// Amounts is a computed total, with how it was computed.
type Amounts struct {
	Currency         string            `json:"currency"`
	SubtotalMinor    int64             `json:"subtotal_minor"`
	DiscountMinor    int64             `json:"discount_minor"`
	DeliveryFeeMinor int64             `json:"delivery_fee_minor"`
	TaxMinor         int64             `json:"tax_minor"`
	TotalMinor       int64             `json:"total_minor"`
	Tax              TaxSnapshot       `json:"tax"`
	Discount         *DiscountSnapshot `json:"discount"`
}

// Quote computes what one week costs: the plan price, less any discount
// (never below zero), plus the delivery fee, with tax per the policy.
// Deterministic: the same inputs always give the same amounts.
func (p Policy) Quote(subtotal int64, currency string, d *DiscountSnapshot) Amounts {
	a := Amounts{Currency: currency, SubtotalMinor: subtotal, DeliveryFeeMinor: p.DeliveryFeeMinor,
		Tax: TaxSnapshot{Mode: p.TaxMode, RateBps: p.TaxRateBps, Label: p.TaxLabel, Registration: p.TaxRegistration}}
	if a.Tax.Mode == "" {
		a.Tax.Mode = TaxNone
	}
	if a.Tax.Mode == TaxNone {
		a.Tax.RateBps = 0
	}
	if d != nil {
		switch d.Kind {
		case "percent":
			a.DiscountMinor = money.Percent(subtotal, d.PercentBps)
		case "fixed":
			a.DiscountMinor = d.AmountMinor
		}
		if a.DiscountMinor > subtotal {
			a.DiscountMinor = subtotal
		}
		if a.DiscountMinor > 0 {
			a.Discount = d
		} else {
			a.DiscountMinor = 0
		}
	}
	base := subtotal - a.DiscountMinor + a.DeliveryFeeMinor
	switch a.Tax.Mode {
	case TaxExclusive:
		a.TaxMinor = money.Percent(base, a.Tax.RateBps)
		a.TotalMinor = base + a.TaxMinor
	case TaxInclusive:
		a.TaxMinor = money.IncludedTax(base, a.Tax.RateBps)
		a.TotalMinor = base
	default:
		a.TotalMinor = base
	}
	return a
}

type Service struct {
	pool   *pgxpool.Pool
	Policy Policy
	// Issuer names the plan on invoice lines (set by the server).
	Issuer Issuer
	Now    func() time.Time
}

func NewService(pool *pgxpool.Pool, policy Policy) *Service {
	return &Service{pool: pool, Policy: policy, Now: time.Now}
}

// Pool is the database the service writes to (for workflows that start
// their own transaction).
func (s *Service) Pool() *pgxpool.Pool { return s.pool }

// ---------------------------------------------------------------- billing cycles

// Cycle is a billing cycle.
type Cycle struct {
	ID                  string     `json:"id"`
	SubscriptionID      string     `json:"subscription_id"`
	SubscriptionCycleID string     `json:"subscription_cycle_id"`
	Kind                string     `json:"kind"`
	PeriodStart         string     `json:"period_start"`
	PeriodEnd           string     `json:"period_end"`
	DueAt               time.Time  `json:"due_at"`
	Status              string     `json:"status"`
	Attempts            int        `json:"attempts"`
	NextAttemptAt       *time.Time `json:"next_attempt_at"`
	PaidAt              *time.Time `json:"paid_at"`
	VoidReason          *string    `json:"void_reason"`
	Amounts
	InvoiceID     *string `json:"invoice_id,omitempty"`
	InvoiceNumber *string `json:"invoice_number,omitempty"`
	CustomerID    string  `json:"-"`
	PromotionID   *string `json:"-"`
}

func cycleView(b store.BillingCycle) Cycle {
	c := Cycle{
		ID: b.ID, SubscriptionID: b.SubscriptionID, SubscriptionCycleID: b.SubscriptionCycleID, Kind: b.Kind,
		PeriodStart: b.PeriodStart.Format("2006-01-02"), PeriodEnd: b.PeriodEnd.Format("2006-01-02"),
		DueAt: b.DueAt, Status: b.Status, Attempts: int(b.Attempts), NextAttemptAt: b.NextAttemptAt, PaidAt: b.PaidAt,
		VoidReason: b.VoidReason, CustomerID: b.CustomerID, PromotionID: b.PromotionID,
		Amounts: Amounts{Currency: b.Currency, SubtotalMinor: b.SubtotalMinor, DiscountMinor: b.DiscountMinor,
			DeliveryFeeMinor: b.DeliveryFeeMinor, TaxMinor: b.TaxMinor, TotalMinor: b.TotalMinor},
	}
	_ = json.Unmarshal(b.TaxSnapshot, &c.Tax)
	if len(b.DiscountSnapshot) > 0 && string(b.DiscountSnapshot) != "null" {
		var d DiscountSnapshot
		if json.Unmarshal(b.DiscountSnapshot, &d) == nil {
			c.Discount = &d
		}
	}
	return c
}

// NewCycle is what a billing cycle is created from.
type NewCycle struct {
	SubscriptionID      string
	SubscriptionCycleID string
	CustomerID          string
	Kind                string // initial or renewal
	Week                time.Time
	DueAt               time.Time
	SubtotalMinor       int64
	Currency            string
	Status              string // pending (initial, before checkout) or due
}

// CreateCycle creates the billing cycle for a subscription week, applying
// the subscription's promotion if it still has cycles left. It returns the
// existing one if the week was already billed (the scheduler may retry).
func (s *Service) CreateCycle(ctx context.Context, q db.Querier, in NewCycle) (*Cycle, bool, error) {
	if existing, err := store.New(q).BillingCycleForCycle(ctx, in.SubscriptionCycleID); err == nil {
		c := cycleView(existing)
		return &c, false, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, err
	}
	d, err := s.discountFor(ctx, q, in.SubscriptionID)
	if err != nil {
		return nil, false, err
	}
	a := s.Policy.Quote(in.SubtotalMinor, in.Currency, d)
	taxJSON, _ := json.Marshal(a.Tax)
	var discJSON []byte
	var promoID *string
	if a.Discount != nil {
		discJSON, _ = json.Marshal(a.Discount)
		promoID = &a.Discount.PromotionID
	}
	row, err := store.New(q).CreateBillingCycle(ctx, store.CreateBillingCycleParams{
		SubscriptionID: in.SubscriptionID, SubscriptionCycleID: in.SubscriptionCycleID, CustomerID: in.CustomerID,
		Kind: in.Kind, PeriodStart: in.Week, PeriodEnd: in.Week.AddDate(0, 0, 6), DueAt: in.DueAt,
		Currency: a.Currency, SubtotalMinor: a.SubtotalMinor, DiscountMinor: a.DiscountMinor,
		DeliveryFeeMinor: a.DeliveryFeeMinor, TaxMinor: a.TaxMinor, TotalMinor: a.TotalMinor,
		TaxSnapshot: taxJSON, DiscountSnapshot: discJSON, PromotionID: promoID, Status: in.Status,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Created by a concurrent run between the read and the insert.
		existing, err := store.New(q).BillingCycleForCycle(ctx, in.SubscriptionCycleID)
		if err != nil {
			return nil, false, err
		}
		c := cycleView(existing)
		return &c, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	c := cycleView(row)
	return &c, true, nil
}

// Get returns a billing cycle. lock takes a row lock (q must be a transaction).
func (s *Service) Get(ctx context.Context, q db.Querier, id string, lock bool) (*Cycle, error) {
	var row store.BillingCycle
	var err error
	if lock {
		row, err = store.New(q).GetBillingCycleForUpdate(ctx, id)
	} else {
		row, err = store.New(q).GetBillingCycle(ctx, id)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c := cycleView(row)
	return &c, nil
}

// ForSubscriptionCycle returns the billing cycle of a week, or nil.
func (s *Service) ForSubscriptionCycle(ctx context.Context, q db.Querier, cycleID string) (*Cycle, error) {
	row, err := store.New(q).BillingCycleForCycle(ctx, cycleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c := cycleView(row)
	return &c, nil
}

// Payable reports whether money can be taken for the cycle now.
func (c *Cycle) Payable() bool {
	return c.Status == "pending" || c.Status == "due" || c.Status == "failed"
}

// MarkDue makes a pending cycle payable (checkout started).
func (s *Service) MarkDue(ctx context.Context, q db.Querier, id string) error {
	c, err := s.Get(ctx, q, id, true)
	if err != nil {
		return err
	}
	if c.Status != "pending" {
		return nil
	}
	_, err = store.New(q).SetBillingStatus(ctx, store.SetBillingStatusParams{ID: id, Status: "due", Now: s.Now()})
	return err
}

// MarkPaid records that a cycle was paid, issues its invoice if it has none,
// marks the invoice paid and counts the promotion use. Idempotent: a cycle
// already paid is left as it is and changed=false.
func (s *Service) MarkPaid(ctx context.Context, q db.Querier, id string, customer CustomerSnapshot) (c *Cycle, changed bool, err error) {
	c, err = s.Get(ctx, q, id, true)
	if err != nil {
		return nil, false, err
	}
	if c.Status == "paid" {
		return c, false, nil
	}
	now := s.Now()
	row, err := store.New(q).SetBillingStatus(ctx, store.SetBillingStatusParams{ID: id, Status: "paid", Now: now})
	if err != nil {
		return nil, false, err
	}
	inv, err := s.ensureInvoice(ctx, q, row, customer, now)
	if err != nil {
		return nil, false, err
	}
	if inv.Status != "paid" {
		if err := store.New(q).SetInvoiceStatus(ctx, store.SetInvoiceStatusParams{ID: inv.ID, Status: "paid", Now: now}); err != nil {
			return nil, false, err
		}
	}
	if row.PromotionID != nil {
		if err := store.New(q).UseRedemptionCycle(ctx, row.SubscriptionID); err != nil {
			return nil, false, err
		}
	}
	paid := cycleView(row)
	paid.InvoiceID, paid.InvoiceNumber = &inv.ID, &inv.Number
	return &paid, true, nil
}

// MarkFailed records a failed collection attempt. retryAt schedules an
// automatic retry, if the policy allows one.
func (s *Service) MarkFailed(ctx context.Context, q db.Querier, id string) (*Cycle, error) {
	c, err := s.Get(ctx, q, id, true)
	if err != nil {
		return nil, err
	}
	if c.Status == "paid" || c.Status == "void" {
		return c, nil
	}
	var next *time.Time
	if c.Kind == "renewal" && c.Attempts+1 <= s.Policy.RetryLimit {
		t := s.Now().Add(s.Policy.RetryAfter)
		next = &t
	}
	row, err := store.New(q).SetBillingStatus(ctx, store.SetBillingStatusParams{ID: id, Status: "failed", CountAttempt: true, NextAttemptAt: next, Now: s.Now()})
	if err != nil {
		return nil, err
	}
	v := cycleView(row)
	return &v, nil
}

// ClearRetry takes a failed cycle off the retry list while it is retried,
// so a second scheduler run does not charge it again.
func (s *Service) ClearRetry(ctx context.Context, q db.Querier, id string) error {
	c, err := s.Get(ctx, q, id, true)
	if err != nil {
		return err
	}
	if c.Status != "failed" {
		return nil
	}
	_, err = store.New(q).SetBillingStatus(ctx, store.SetBillingStatusParams{ID: id, Status: "failed", Now: s.Now()})
	return err
}

// Void stops collection of a cycle (cutoff passed unpaid, checkout
// abandoned, week cancelled). Its invoice, if any, is voided too; nothing is
// deleted. A paid cycle is never voided: that would need a refund.
func (s *Service) Void(ctx context.Context, q db.Querier, id, reason string) error {
	c, err := s.Get(ctx, q, id, true)
	if err != nil {
		return err
	}
	if c.Status == "paid" || c.Status == "void" {
		return nil
	}
	now := s.Now()
	if _, err := store.New(q).SetBillingStatus(ctx, store.SetBillingStatusParams{ID: id, Status: "void", VoidReason: &reason, Now: now}); err != nil {
		return err
	}
	inv, err := store.New(q).InvoiceForBillingCycle(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return store.New(q).SetInvoiceStatus(ctx, store.SetInvoiceStatusParams{ID: inv.ID, Status: "void", Now: now})
}

// Move re-points a not-yet-paid initial billing cycle to another week.
func (s *Service) Move(ctx context.Context, q db.Querier, id, cycleID string, week time.Time) error {
	return store.New(q).MoveBillingCycle(ctx, store.MoveBillingCycleParams{ID: id, SubscriptionCycleID: cycleID,
		PeriodStart: week, PeriodEnd: week.AddDate(0, 0, 6)})
}

// RetryDue lists failed renewal cycles whose automatic retry is due.
func (s *Service) RetryDue(ctx context.Context, q db.Querier, now time.Time) ([]string, error) {
	return store.New(q).RetryDueBillingCycles(ctx, now)
}

// ForCustomer lists a customer's billing history (newest first).
func (s *Service) ForCustomer(ctx context.Context, customerID string) ([]Cycle, error) {
	rows, err := store.New(s.pool).ListBillingCyclesForCustomer(ctx, customerID)
	if err != nil {
		return nil, err
	}
	out := []Cycle{}
	for _, r := range rows {
		c := cycleView(store.BillingCycle{
			ID: r.ID, SubscriptionID: r.SubscriptionID, SubscriptionCycleID: r.SubscriptionCycleID, CustomerID: r.CustomerID,
			Kind: r.Kind, PeriodStart: r.PeriodStart, PeriodEnd: r.PeriodEnd, DueAt: r.DueAt, Currency: r.Currency,
			SubtotalMinor: r.SubtotalMinor, DiscountMinor: r.DiscountMinor, DeliveryFeeMinor: r.DeliveryFeeMinor,
			TaxMinor: r.TaxMinor, TotalMinor: r.TotalMinor, TaxSnapshot: r.TaxSnapshot, DiscountSnapshot: r.DiscountSnapshot,
			PromotionID: r.PromotionID, Status: r.Status, Attempts: r.Attempts, NextAttemptAt: r.NextAttemptAt,
			PaidAt: r.PaidAt, VoidedAt: r.VoidedAt, VoidReason: r.VoidReason,
		})
		c.InvoiceID, c.InvoiceNumber = r.InvoiceID, r.InvoiceNumber
		out = append(out, c)
	}
	return out, nil
}

// ForSubscription lists every billing cycle of a subscription (admin).
func (s *Service) ForSubscription(ctx context.Context, subscriptionID string) ([]Cycle, error) {
	rows, err := store.New(s.pool).ListBillingCyclesForSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	out := []Cycle{}
	for _, r := range rows {
		out = append(out, cycleView(r))
	}
	return out, nil
}

var errNotPayable = httpx.NewError(http.StatusConflict, "not_payable", "There is nothing to pay for this week.")

// ErrNotPayable is returned when a billing cycle cannot take a payment.
func ErrNotPayable() error { return errNotPayable }
