package payments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/payments/store"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/jobs"
	"mise.tt/api/internal/platform/outbox"
)

// JobProcessWebhook processes one stored webhook event.
const JobProcessWebhook = "payments.process_webhook"

// Outcome is what the rest of the system hears about a payment.
type Outcome struct {
	PaymentID       string
	BillingCycleID  string
	CustomerID      string
	Kind            string // checkout or off_session
	PaymentMethodID string
	AmountMinor     int64
	Currency        string
	FailureMessage  string
}

// Hooks hears about payment outcomes, inside the transaction that records
// them, so the outcome and its consequences commit together. The commerce
// module implements it.
type Hooks interface {
	PaymentSucceeded(ctx context.Context, q db.Querier, o Outcome) error
	PaymentFailed(ctx context.Context, q db.Querier, o Outcome) error
	CheckoutExpired(ctx context.Context, q db.Querier, billingCycleID string) error
}

type Service struct {
	pool      *pgxpool.Pool
	providers map[string]Provider
	// Checkout is the provider new checkouts use ("" = none available).
	Checkout string
	Hooks    Hooks
	// CheckoutTTL is how long a checkout stays open.
	CheckoutTTL time.Duration
	Now         func() time.Time
}

func NewService(pool *pgxpool.Pool, providers ...Provider) *Service {
	s := &Service{pool: pool, providers: map[string]Provider{}, CheckoutTTL: time.Hour, Now: time.Now}
	for _, p := range providers {
		s.providers[p.Name()] = p
		if s.Checkout == "" {
			s.Checkout = p.Name()
		}
	}
	return s
}

// Provider returns a configured provider by name.
func (s *Service) Provider(name string) (Provider, bool) {
	p, ok := s.providers[name]
	return p, ok
}

// Available reports which provider takes new payments, if any, and whether
// it moves real money.
func (s *Service) Available() (name string, live bool, caps Capabilities, ok bool) {
	p, ok := s.providers[s.Checkout]
	if !ok {
		return "", false, Capabilities{}, false
	}
	return p.Name(), p.Live(), p.Capabilities(), true
}

var ErrNoProvider = httpx.NewError(http.StatusServiceUnavailable, "payments_unavailable",
	"Online payment is not available yet. Please try again later.")

// ---------------------------------------------------------------- checkout

// Checkout is a checkout session.
type Checkout struct {
	ID             string    `json:"id"`
	Status         string    `json:"status"`
	RedirectURL    string    `json:"redirect_url,omitempty"`
	ExpiresAt      time.Time `json:"expires_at"`
	BillingCycleID string    `json:"billing_cycle_id"`
	AmountMinor    int64     `json:"amount_minor"`
	Currency       string    `json:"currency"`
	Provider       string    `json:"provider"`
	Test           bool      `json:"test"`
}

func (s *Service) checkoutView(c store.CheckoutSession) Checkout {
	v := Checkout{ID: c.ID, Status: c.Status, ExpiresAt: c.ExpiresAt, BillingCycleID: c.BillingCycleID,
		AmountMinor: c.AmountMinor, Currency: c.Currency, Provider: c.Provider}
	if c.RedirectUrl != nil && c.Status == "open" {
		v.RedirectURL = *c.RedirectUrl
	}
	if p, ok := s.providers[c.Provider]; ok {
		v.Test = !p.Live()
	}
	return v
}

// CheckoutInput is a request to collect one billing cycle.
type CheckoutInput struct {
	CustomerID     string
	BillingCycleID string
	AmountMinor    int64
	Currency       string
	Description    string
	// ReturnURL gets "?checkout=<id>" appended.
	ReturnURL string
}

// StartCheckout opens (or returns the open) hosted checkout for a billing
// cycle. The caller has already checked the cycle is payable and the amount
// is the server-computed total. The provider is called outside any database
// transaction.
func (s *Service) StartCheckout(ctx context.Context, in CheckoutInput) (*Checkout, error) {
	p, ok := s.providers[s.Checkout]
	if !ok {
		return nil, ErrNoProvider
	}
	if in.AmountMinor <= 0 {
		return nil, httpx.NewError(http.StatusConflict, "nothing_to_pay", "There is nothing to pay.")
	}
	var session store.CheckoutSession
	reuse := false
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		open, err := q.OpenCheckoutForBillingCycle(ctx, in.BillingCycleID)
		if err == nil {
			if open.ExpiresAt.After(s.Now()) && open.AmountMinor == in.AmountMinor && open.RedirectUrl != nil {
				session, reuse = open, true
				return nil
			}
			// Stale or for a different amount: close it and start again.
			if err := q.SetCheckoutStatus(ctx, store.SetCheckoutStatusParams{ID: open.ID, Status: "cancelled"}); err != nil {
				return err
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		session, err = q.CreateCheckoutSession(ctx, store.CreateCheckoutSessionParams{
			CustomerID: in.CustomerID, BillingCycleID: in.BillingCycleID, Provider: p.Name(),
			AmountMinor: in.AmountMinor, Currency: in.Currency, ExpiresAt: s.Now().Add(s.CheckoutTTL),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if reuse {
		v := s.checkoutView(session)
		return &v, nil
	}
	ps, err := p.CreateCheckout(ctx, CheckoutRequest{
		Reference: session.ID, AmountMinor: in.AmountMinor, Currency: in.Currency, Description: in.Description,
		ReturnURL: in.ReturnURL + "?checkout=" + session.ID, SaveMethod: p.Capabilities().OffSessionCharges,
	})
	if err != nil {
		_ = store.New(s.pool).SetCheckoutStatus(context.WithoutCancel(ctx), store.SetCheckoutStatusParams{ID: session.ID, Status: "failed"})
		slog.ErrorContext(ctx, "create checkout failed", "provider", p.Name(), "error", err.Error())
		return nil, httpx.NewError(http.StatusBadGateway, "payment_provider_error", "The payment provider could not start checkout. Please try again.")
	}
	expires := session.ExpiresAt
	if !ps.ExpiresAt.IsZero() && ps.ExpiresAt.Before(expires) {
		expires = ps.ExpiresAt
	}
	if err := store.New(s.pool).SetCheckoutProviderRef(ctx, store.SetCheckoutProviderRefParams{
		ID: session.ID, ProviderRef: &ps.ProviderRef, RedirectUrl: &ps.RedirectURL, ExpiresAt: expires}); err != nil {
		return nil, err
	}
	session.ProviderRef, session.RedirectUrl, session.ExpiresAt = &ps.ProviderRef, &ps.RedirectURL, expires
	v := s.checkoutView(session)
	return &v, nil
}

// CheckoutForCustomer returns one of the customer's checkout sessions.
func (s *Service) CheckoutForCustomer(ctx context.Context, customerID, id string) (*Checkout, error) {
	c, err := store.New(s.pool).GetCheckoutSessionForCustomer(ctx, store.GetCheckoutSessionForCustomerParams{ID: id, CustomerID: customerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	v := s.checkoutView(c)
	return &v, nil
}

// CancelOpenCheckouts closes any open checkout for a billing cycle (it was
// voided, or paid another way).
func (s *Service) CancelOpenCheckouts(ctx context.Context, q db.Querier, billingCycleID string) error {
	return store.New(q).CancelOpenCheckouts(ctx, billingCycleID)
}

// ---------------------------------------------------------------- webhooks

// ReceiveWebhook authenticates and stores a provider notification, and
// queues it for processing in the same transaction. A repeated event id is
// accepted and ignored. It returns duplicate=true for a repeat.
func (s *Service) ReceiveWebhook(ctx context.Context, providerName string, header http.Header, body []byte) (duplicate bool, err error) {
	p, ok := s.providers[providerName]
	if !ok {
		return false, httpx.ErrNotFound
	}
	ev, err := p.ParseWebhook(header, body)
	if err != nil {
		slog.WarnContext(ctx, "payment webhook refused", "provider", providerName, "error", err.Error())
		return false, httpx.NewError(http.StatusBadRequest, "invalid_signature", "The webhook could not be verified.")
	}
	ref, kind := ev.SessionRef, "session"
	if ref == "" {
		ref, kind = ev.TransactionRef, "transaction"
	}
	if ref == "" {
		kind = "none"
	}
	payload := body
	if !json.Valid(payload) {
		payload, _ = json.Marshal(map[string]string{"raw": string(body)})
	}
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		id, err := store.New(tx).InsertWebhookEvent(ctx, store.InsertWebhookEventParams{
			Provider: providerName, EventID: ev.ID, EventType: ev.Type, RefKind: kind, ProviderRef: &ref, Payload: payload})
		if errors.Is(err, pgx.ErrNoRows) {
			duplicate = true
			return nil
		}
		if err != nil {
			return err
		}
		return jobs.Enqueue(ctx, tx, JobProcessWebhook, map[string]int64{"event_id": id})
	})
	if duplicate {
		slog.InfoContext(ctx, "payment webhook repeated", "provider", providerName, "event_type", ev.Type)
	}
	return duplicate, err
}

// ProcessWebhook is the job handler for a stored webhook event. It does not
// trust the event's contents beyond which payment it is about: it asks the
// provider for the current status and applies that.
func (s *Service) ProcessWebhook(ctx context.Context, payload json.RawMessage) error {
	var job struct {
		EventID int64 `json:"event_id"`
	}
	if err := json.Unmarshal(payload, &job); err != nil {
		return err
	}
	ev, err := store.New(s.pool).GetWebhookEvent(ctx, job.EventID)
	if err != nil {
		return err
	}
	if ev.Status == "processed" || ev.Status == "ignored" {
		return nil
	}
	ref := ""
	if ev.ProviderRef != nil {
		ref = *ev.ProviderRef
	}
	status := "processed"
	switch {
	case ev.RefKind == "session" && ref != "":
		err = s.ReconcileCheckout(ctx, ev.Provider, ref, "webhook", ev.EventID)
	case ev.RefKind == "transaction" && ref != "":
		err = s.ReconcileTransaction(ctx, ev.Provider, ref, "webhook", ev.EventID)
	default:
		status = "ignored"
	}
	if errors.Is(err, errUnknownRef) {
		status, err = "ignored", nil
	}
	saveCtx := context.WithoutCancel(ctx)
	if err != nil {
		msg := err.Error()
		_ = store.New(s.pool).SetWebhookEventStatus(saveCtx, store.SetWebhookEventStatusParams{ID: ev.ID, Status: "failed", Error: &msg})
		return err
	}
	return store.New(s.pool).SetWebhookEventStatus(saveCtx, store.SetWebhookEventStatusParams{ID: ev.ID, Status: status})
}

var errUnknownRef = errors.New("payments: no record for this provider reference")

// ReconcileCheckout brings a checkout session and its payment in line with
// what the provider reports now. Safe to call any number of times.
func (s *Service) ReconcileCheckout(ctx context.Context, providerName, sessionRef, source, eventID string) error {
	p, ok := s.providers[providerName]
	if !ok {
		return fmt.Errorf("payments: unknown provider %q", providerName)
	}
	pp, err := p.CheckoutStatus(ctx, sessionRef)
	if err != nil {
		return err
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		cs, err := q.CheckoutByProviderRef(ctx, store.CheckoutByProviderRefParams{Provider: providerName, ProviderRef: &sessionRef})
		if errors.Is(err, pgx.ErrNoRows) {
			return errUnknownRef
		}
		if err != nil {
			return err
		}
		if pp.Status == StatusPending || pp.TransactionRef == "" {
			if pp.Status == StatusCancelled {
				return q.SetCheckoutStatus(ctx, store.SetCheckoutStatusParams{ID: cs.ID, Status: "cancelled"})
			}
			return nil // the customer has not paid yet
		}
		if pp.AmountMinor != cs.AmountMinor || pp.Currency != cs.Currency {
			slog.ErrorContext(ctx, "provider amount does not match the checkout", "checkout_id", cs.ID)
		}
		var methodID *string
		if pp.MethodRef != "" {
			m, err := q.UpsertPaymentMethod(ctx, store.UpsertPaymentMethodParams{CustomerID: cs.CustomerID, Provider: providerName,
				ProviderRef: pp.MethodRef, Label: labelOr(pp.MethodLabel)})
			if err != nil {
				return err
			}
			methodID = &m.ID
		}
		key := "checkout:" + cs.ID + ":" + pp.TransactionRef
		pay, err := s.paymentFor(ctx, q, key, store.InsertPaymentParams{
			CustomerID: cs.CustomerID, BillingCycleID: cs.BillingCycleID, CheckoutSessionID: &cs.ID, PaymentMethodID: methodID,
			Provider: providerName, ProviderRef: &pp.TransactionRef, Kind: "checkout", AmountMinor: pp.AmountMinor,
			Currency: pp.Currency, Status: string(StatusPending), IdempotencyKey: key,
		})
		if err != nil {
			return err
		}
		switch pp.Status {
		case StatusSucceeded:
			if err := q.SetCheckoutStatus(ctx, store.SetCheckoutStatusParams{ID: cs.ID, Status: "completed"}); err != nil {
				return err
			}
		case StatusFailed, StatusCancelled:
			if err := q.SetCheckoutStatus(ctx, store.SetCheckoutStatusParams{ID: cs.ID, Status: "failed"}); err != nil {
				return err
			}
		}
		return s.apply(ctx, tx, pay, pp, methodID, source, eventID)
	})
}

// ReconcileTransaction does the same for an off-session charge.
func (s *Service) ReconcileTransaction(ctx context.Context, providerName, transactionRef, source, eventID string) error {
	p, ok := s.providers[providerName]
	if !ok {
		return fmt.Errorf("payments: unknown provider %q", providerName)
	}
	pp, err := p.TransactionStatus(ctx, transactionRef)
	if err != nil {
		return err
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		pay, err := store.New(tx).PaymentByProviderRef(ctx, store.PaymentByProviderRefParams{Provider: providerName, ProviderRef: &transactionRef})
		if errors.Is(err, pgx.ErrNoRows) {
			return errUnknownRef
		}
		if err != nil {
			return err
		}
		return s.apply(ctx, tx, pay, pp, nil, source, eventID)
	})
}

func labelOr(s string) string {
	if s == "" {
		return "Saved payment method"
	}
	if len(s) > 60 {
		return s[:60]
	}
	return s
}

// paymentFor returns the payment for an idempotency key, creating it.
func (s *Service) paymentFor(ctx context.Context, q *store.Queries, key string, p store.InsertPaymentParams) (store.Payment, error) {
	// An existing row (a retried request) is kept as it is. Either way the
	// row is then read with a lock for the rest of the transaction.
	if _, err := q.InsertPayment(ctx, p); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return store.Payment{}, err
	}
	return q.PaymentByIdempotencyKey(ctx, key)
}

// apply moves a payment to the provider's status if that is a forward move,
// records the history, and tells the hooks. A repeat of the same status
// does nothing, so a duplicate event never runs the hooks twice.
func (s *Service) apply(ctx context.Context, tx pgx.Tx, pay store.Payment, pp *ProviderPayment, methodID *string, source, eventID string) error {
	from := Status(pay.Status)
	to := pp.Status
	if !advance(from, to) {
		return nil
	}
	q := store.New(tx)
	var code, msg *string
	if pp.FailureCode != "" {
		code = &pp.FailureCode
	}
	if pp.FailureMessage != "" {
		msg = &pp.FailureMessage
	}
	var ref *string
	if pp.TransactionRef != "" {
		ref = &pp.TransactionRef
	}
	updated, err := q.UpdatePayment(ctx, store.UpdatePaymentParams{ID: pay.ID, Status: string(to), ProviderRef: ref,
		PaymentMethodID: methodID, FailureCode: code, FailureMessage: msg})
	if err != nil {
		return err
	}
	var evID *string
	if eventID != "" {
		evID = &eventID
	}
	fromStr := string(from)
	if err := q.AddPaymentHistory(ctx, store.AddPaymentHistoryParams{PaymentID: pay.ID, FromStatus: &fromStr, ToStatus: string(to),
		Source: source, ProviderEventID: evID}); err != nil {
		return err
	}
	if err := audit.Record(ctx, tx, audit.Entry{Action: "payment." + string(to), TargetType: "payment", TargetID: pay.ID,
		Metadata: map[string]any{"from": fromStr, "source": source, "amount_minor": pay.AmountMinor, "currency": pay.Currency}}); err != nil {
		return err
	}
	// A payment changes status at most once per status (advance never goes
	// back), so (payment, status) identifies the event: a duplicate webhook or
	// a reconcile racing a webhook publishes it once.
	test := true
	if p, ok := s.providers[pay.Provider]; ok {
		test = !p.Live()
	}
	if err := outbox.Record(ctx, tx, outbox.Event{Type: "payment." + string(to), AggregateType: "payment", AggregateID: pay.ID,
		DedupeKey: string(to), Payload: map[string]any{"payment_id": pay.ID, "billing_cycle_id": pay.BillingCycleID,
			"customer_id": pay.CustomerID, "kind": pay.Kind, "from_status": fromStr, "amount_minor": updated.AmountMinor,
			"currency": updated.Currency, "provider": pay.Provider, "test": test, "source": source}}); err != nil {
		return err
	}
	if s.Hooks == nil {
		return nil
	}
	o := Outcome{PaymentID: pay.ID, BillingCycleID: pay.BillingCycleID, CustomerID: pay.CustomerID, Kind: pay.Kind,
		AmountMinor: updated.AmountMinor, Currency: updated.Currency}
	if updated.PaymentMethodID != nil {
		o.PaymentMethodID = *updated.PaymentMethodID
	}
	if updated.FailureMessage != nil {
		o.FailureMessage = *updated.FailureMessage
	}
	switch to {
	case StatusSucceeded:
		return s.Hooks.PaymentSucceeded(ctx, tx, o)
	case StatusFailed, StatusCancelled:
		return s.Hooks.PaymentFailed(ctx, tx, o)
	}
	return nil
}

// Flag marks a payment for finance to review.
func (s *Service) Flag(ctx context.Context, q db.Querier, paymentID, reason string) error {
	return store.New(q).FlagPayment(ctx, store.FlagPaymentParams{ID: paymentID, Reason: &reason})
}

// ---------------------------------------------------------------- off-session charges

// ChargeInput is an automatic charge of a saved method.
type ChargeInput struct {
	CustomerID      string
	BillingCycleID  string
	PaymentMethodID string
	AmountMinor     int64
	Currency        string
	Description     string
	// Attempt numbers the attempts at one billing cycle; with the cycle it
	// makes the idempotency key, so a retried job reuses the same charge.
	Attempt int
}

// ErrCannotCharge means the provider or the customer's method cannot be
// charged without the customer: they need to pay through checkout.
var ErrCannotCharge = errors.New("payments: cannot charge without the customer")

// Charge charges a saved method for a billing cycle. The payment row is
// written before the provider is called, with the idempotency key the
// provider also receives, so a crash between the two can be retried safely.
func (s *Service) Charge(ctx context.Context, in ChargeInput) error {
	m, err := store.New(s.pool).GetPaymentMethod(ctx, in.PaymentMethodID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCannotCharge
	}
	if err != nil {
		return err
	}
	p, ok := s.providers[m.Provider]
	if !ok || !p.Capabilities().OffSessionCharges || m.Status != "active" {
		return ErrCannotCharge
	}
	key := fmt.Sprintf("charge:%s:%d", in.BillingCycleID, in.Attempt)
	var pay store.Payment
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		var err error
		pay, err = s.paymentFor(ctx, q, key, store.InsertPaymentParams{
			CustomerID: in.CustomerID, BillingCycleID: in.BillingCycleID, PaymentMethodID: &m.ID, Provider: p.Name(),
			Kind: "off_session", AmountMinor: in.AmountMinor, Currency: in.Currency, Status: string(StatusPending), IdempotencyKey: key,
		})
		return err
	})
	if err != nil {
		return err
	}
	if Status(pay.Status) != StatusPending {
		return nil // this attempt already has an outcome
	}
	pp, err := p.Charge(ctx, ChargeRequest{MethodRef: m.ProviderRef, AmountMinor: in.AmountMinor, Currency: in.Currency,
		Description: in.Description, Reference: pay.ID, IdempotencyKey: key})
	if err != nil {
		return fmt.Errorf("charge: %w", err)
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		locked, err := store.New(tx).GetPaymentForUpdate(ctx, pay.ID)
		if err != nil {
			return err
		}
		return s.apply(ctx, tx, locked, pp, nil, "charge", "")
	})
}

// ---------------------------------------------------------------- expiry

// ExpireCheckouts closes checkouts that ran out of time. Each is first
// reconciled with the provider, so a payment made at the last moment is
// recorded rather than lost.
func (s *Service) ExpireCheckouts(ctx context.Context, now time.Time) (int, error) {
	ids, err := store.New(s.pool).ExpiredOpenCheckouts(ctx, now)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		cs, err := store.New(s.pool).GetCheckoutSession(ctx, id)
		if err != nil {
			return n, err
		}
		if cs.ProviderRef != nil {
			if err := s.ReconcileCheckout(ctx, cs.Provider, *cs.ProviderRef, "reconcile", ""); err != nil && !errors.Is(err, errUnknownRef) {
				slog.WarnContext(ctx, "reconcile before expiry failed", "checkout_id", id, "error", err.Error())
				continue
			}
		}
		err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
			q := store.New(tx)
			cur, err := q.GetCheckoutSession(ctx, id)
			if err != nil || cur.Status != "open" {
				return err
			}
			if err := q.SetCheckoutStatus(ctx, store.SetCheckoutStatusParams{ID: id, Status: "expired"}); err != nil {
				return err
			}
			if s.Hooks != nil {
				return s.Hooks.CheckoutExpired(ctx, tx, cur.BillingCycleID)
			}
			return nil
		})
		if err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// ---------------------------------------------------------------- reading

// Payment is a payment as staff see it.
type Payment struct {
	ID              string     `json:"id"`
	CustomerID      string     `json:"customer_id"`
	BillingCycleID  string     `json:"billing_cycle_id"`
	Provider        string     `json:"provider"`
	ProviderRef     *string    `json:"provider_ref"`
	Kind            string     `json:"kind"`
	AmountMinor     int64      `json:"amount_minor"`
	RefundedMinor   int64      `json:"refunded_minor"`
	Currency        string     `json:"currency"`
	Status          string     `json:"status"`
	FailureMessage  *string    `json:"failure_message"`
	NeedsAttention  bool       `json:"needs_attention"`
	AttentionReason *string    `json:"attention_reason"`
	SucceededAt     *time.Time `json:"succeeded_at"`
	CreatedAt       time.Time  `json:"created_at"`
	Test            bool       `json:"test"`
}

type PaymentDetail struct {
	Payment
	History []HistoryEntry `json:"history"`
	Refunds []Refund       `json:"refunds"`
}

type HistoryEntry struct {
	From      *string   `json:"from"`
	To        string    `json:"to"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
}

type Refund struct {
	ID          string     `json:"id"`
	AmountMinor int64      `json:"amount_minor"`
	Currency    string     `json:"currency"`
	Reason      string     `json:"reason"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at"`
}

func (s *Service) paymentView(p store.Payment) Payment {
	v := Payment{ID: p.ID, CustomerID: p.CustomerID, BillingCycleID: p.BillingCycleID, Provider: p.Provider, ProviderRef: p.ProviderRef,
		Kind: p.Kind, AmountMinor: p.AmountMinor, RefundedMinor: p.RefundedMinor, Currency: p.Currency, Status: p.Status,
		FailureMessage: p.FailureMessage, NeedsAttention: p.NeedsAttention, AttentionReason: p.AttentionReason,
		SucceededAt: p.SucceededAt, CreatedAt: p.CreatedAt}
	if pr, ok := s.providers[p.Provider]; ok {
		v.Test = !pr.Live()
	} else {
		v.Test = p.Provider == "testpay"
	}
	return v
}

// PaymentsFilter narrows the staff list.
type PaymentsFilter struct {
	Status        string
	AttentionOnly bool
	CustomerID    string
}

func (s *Service) List(ctx context.Context, f PaymentsFilter) ([]Payment, error) {
	params := store.ListPaymentsParams{AttentionOnly: f.AttentionOnly}
	if f.Status != "" {
		params.Status = &f.Status
	}
	if f.CustomerID != "" {
		params.CustomerID = &f.CustomerID
	}
	rows, err := store.New(s.pool).ListPayments(ctx, params)
	if err != nil {
		return nil, err
	}
	out := []Payment{}
	for _, r := range rows {
		out = append(out, s.paymentView(r))
	}
	return out, nil
}

func (s *Service) Detail(ctx context.Context, id string) (*PaymentDetail, error) {
	p, err := store.New(s.pool).GetPayment(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	d := &PaymentDetail{Payment: s.paymentView(p), History: []HistoryEntry{}, Refunds: []Refund{}}
	hist, err := store.New(s.pool).PaymentHistory(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, h := range hist {
		d.History = append(d.History, HistoryEntry{From: h.FromStatus, To: h.ToStatus, Source: h.Source, CreatedAt: h.CreatedAt})
	}
	refunds, err := store.New(s.pool).RefundsForPayment(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, r := range refunds {
		d.Refunds = append(d.Refunds, Refund{ID: r.ID, AmountMinor: r.AmountMinor, Currency: r.Currency, Reason: r.Reason,
			Status: r.Status, CreatedAt: r.CreatedAt, CompletedAt: r.CompletedAt})
	}
	return d, nil
}

// SucceededFor returns the successful payment of a billing cycle, if any.
func (s *Service) SucceededFor(ctx context.Context, q db.Querier, billingCycleID string) (*Payment, error) {
	p, err := store.New(q).SucceededPaymentForBillingCycle(ctx, billingCycleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	v := s.paymentView(p)
	return &v, nil
}

// MethodLabel returns the label of a saved payment method.
func (s *Service) MethodLabel(ctx context.Context, id string) (string, bool) {
	m, err := store.New(s.pool).GetPaymentMethod(ctx, id)
	if err != nil || m.Status != "active" {
		return "", false
	}
	return m.Label, true
}

// CanChargeMethod reports whether a saved method can be charged automatically.
func (s *Service) CanChargeMethod(ctx context.Context, id string) bool {
	m, err := store.New(s.pool).GetPaymentMethod(ctx, id)
	if err != nil || m.Status != "active" {
		return false
	}
	p, ok := s.providers[m.Provider]
	return ok && p.Capabilities().OffSessionCharges
}

// StoredWebhook is a stored webhook as staff see it (without its payload).
type StoredWebhook struct {
	ID          int64      `json:"id"`
	Provider    string     `json:"provider"`
	EventID     string     `json:"event_id"`
	EventType   string     `json:"event_type"`
	ProviderRef *string    `json:"provider_ref"`
	Status      string     `json:"status"`
	Error       *string    `json:"error"`
	Attempts    int        `json:"attempts"`
	ReceivedAt  time.Time  `json:"received_at"`
	ProcessedAt *time.Time `json:"processed_at"`
}

func (s *Service) Webhooks(ctx context.Context, status string) ([]StoredWebhook, error) {
	var st *string
	if status != "" {
		st = &status
	}
	rows, err := store.New(s.pool).ListWebhookEvents(ctx, st)
	if err != nil {
		return nil, err
	}
	out := []StoredWebhook{}
	for _, r := range rows {
		out = append(out, StoredWebhook{ID: r.ID, Provider: r.Provider, EventID: r.EventID, EventType: r.EventType,
			ProviderRef: r.ProviderRef, Status: r.Status, Error: r.Error, Attempts: int(r.Attempts),
			ReceivedAt: r.ReceivedAt, ProcessedAt: r.ProcessedAt})
	}
	return out, nil
}
