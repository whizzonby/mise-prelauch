// Package commerce runs the subscription workflows that span modules:
// subscribing and paying, what a payment outcome does to a subscription,
// pausing, cancelling and changing plan, choosing meals, and the weekly
// scheduler that bills weeks, closes selection and makes orders.
//
// It owns no business records of its own beyond notification_deliveries
// (one row per email that must be sent at most once). Each workflow runs in
// one database transaction, calling the owning modules' functions with that
// transaction, so a step and its consequences commit together. Emails and
// provider calls go through the job queue in the same transaction (outbox).
package commerce

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/addresses"
	"mise.tt/api/internal/billing"
	"mise.tt/api/internal/catalog"
	"mise.tt/api/internal/customers"
	"mise.tt/api/internal/delivery"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/menus"
	"mise.tt/api/internal/orders"
	"mise.tt/api/internal/payments"
	"mise.tt/api/internal/plans"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/jobs"
	"mise.tt/api/internal/platform/money"
	"mise.tt/api/internal/subscriptions"
)

type Options struct {
	// CustomerAppURL builds the links in emails and the checkout return URL.
	CustomerAppURL string
	// PendingTTL: a subscription still waiting for its first payment after
	// this long is expired.
	PendingTTL time.Duration
	// ReminderLead: the "choose your meals" reminder goes this long before
	// the cutoff.
	ReminderLead time.Duration
}

// Customers resolves the signed-in customer.
type Customers interface {
	CustomerID(ctx context.Context, p *identity.Principal) (string, error)
}

type Service struct {
	pool      *pgxpool.Pool
	Plans     *plans.Service
	Menus     *menus.Service
	Subs      *subscriptions.Service
	Billing   *billing.Service
	Payments  *payments.Service
	Orders    *orders.Service
	Catalog   *catalog.Service
	Customers Customers
	Identity  *identity.Service
	Delivery  delivery.Eligibility
	Opts      Options
	Now       func() time.Time
}

func New(pool *pgxpool.Pool, opts Options) *Service {
	if opts.PendingTTL <= 0 {
		opts.PendingTTL = 24 * time.Hour
	}
	if opts.ReminderLead <= 0 {
		opts.ReminderLead = 24 * time.Hour
	}
	return &Service{pool: pool, Opts: opts, Now: time.Now}
}

func (s *Service) customerID(ctx context.Context, p *identity.Principal) (string, error) {
	return s.Customers.CustomerID(ctx, p)
}

// ---------------------------------------------------------------- notifications

// JobEmail sends one commerce email (internal/notifications renders it).
const JobEmail = "commerce.email"

// Email kinds.
const (
	EmailActivated        = "subscription_activated"
	EmailReceipt          = "payment_receipt"
	EmailPaymentFailed    = "payment_failed"
	EmailPaymentDue       = "payment_due"
	EmailMenuAvailable    = "menu_available"
	EmailSelectionRemind  = "selection_reminder"
	EmailSelectionConfirm = "selection_confirmed"
	EmailPaused           = "subscription_paused"
	EmailResumed          = "subscription_resumed"
	EmailCancelled        = "subscription_cancelled"
	EmailPlanChanged      = "plan_changed"
)

// EmailJob is the payload of a commerce email. Values are prepared when the
// email is queued; the recipient is looked up when it is sent.
type EmailJob struct {
	Kind       string            `json:"kind"`
	CustomerID string            `json:"customer_id"`
	Data       map[string]string `json:"data"`
}

// notify queues an email once per dedupeKey: a retried event or a second
// scheduler run finds the key taken and queues nothing.
func (s *Service) notify(ctx context.Context, q db.Querier, dedupeKey, kind, customerID string, data map[string]string) error {
	tag, err := q.Exec(ctx, `INSERT INTO notification_deliveries (dedupe_key, kind, customer_id) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		dedupeKey, kind, customerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	if data == nil {
		data = map[string]string{}
	}
	return jobs.Enqueue(ctx, q, JobEmail, EmailJob{Kind: kind, CustomerID: customerID, Data: data})
}

func (s *Service) link(path string) string {
	return strings.TrimRight(s.Opts.CustomerAppURL, "/") + path
}

func weekLabel(week time.Time) string { return menus.WeekLabel(week) }

func localTime(t time.Time) string {
	return t.In(menus.Location()).Format("Monday 2 January at 3:04 pm")
}

// ---------------------------------------------------------------- views

type PlanRef struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	MealsPerWeek int    `json:"meals_per_week"`
}

// CycleView is one week of a subscription as the customer sees it.
type CycleView struct {
	ID              string      `json:"id"`
	Week            string      `json:"week"`
	WeekLabel       string      `json:"week_label"`
	CutoffAt        time.Time   `json:"cutoff_at"`
	RenewalAt       time.Time   `json:"renewal_at"`
	State           string      `json:"state"`
	StateLabel      string      `json:"state_label"`
	Plan            PlanRef     `json:"plan"`
	AmountMinor     int64       `json:"amount_minor"`
	Currency        string      `json:"currency"`
	SelectionOpen   bool        `json:"selection_open"`
	MenuPublished   bool        `json:"menu_published"`
	SelectionStatus *string     `json:"selection_status"`
	SelectedCount   int         `json:"selected_count"`
	Billing         *BillingRef `json:"billing"`
	OrderID         *string     `json:"order_id"`
}

type BillingRef struct {
	ID            string  `json:"id"`
	Status        string  `json:"status"`
	TotalMinor    int64   `json:"total_minor"`
	Currency      string  `json:"currency"`
	InvoiceID     *string `json:"invoice_id"`
	InvoiceNumber *string `json:"invoice_number"`
}

func cycleStateLabel(c *subscriptions.Cycle, b *billing.Cycle) string {
	switch c.State {
	case "finalized":
		return "Order confirmed"
	case "unpaid":
		return "Not delivered: unpaid"
	case "needs_resolution":
		return "We'll be in touch"
	case "paused":
		return "Paused"
	case "cancelled":
		return "Cancelled"
	}
	if b == nil {
		return "Upcoming"
	}
	switch b.Status {
	case "paid":
		return "Paid"
	case "failed":
		return "Payment failed"
	case "due":
		return "Payment due"
	}
	return "Upcoming"
}

func (s *Service) cycleView(ctx context.Context, q db.Querier, c *subscriptions.Cycle, planName string) (*CycleView, error) {
	b, err := s.Billing.ForSubscriptionCycle(ctx, q, c.ID)
	if err != nil {
		return nil, err
	}
	now := s.Now()
	v := &CycleView{ID: c.ID, Week: menus.FormatWeek(c.WeekStart), WeekLabel: weekLabel(c.WeekStart), CutoffAt: c.CutoffAt,
		RenewalAt: c.RenewalAt, State: c.State, StateLabel: cycleStateLabel(c, b),
		Plan:        PlanRef{ID: c.PlanID, Name: planName, MealsPerWeek: int(c.MealsPerWeek)},
		AmountMinor: c.AmountMinor, Currency: c.Currency}
	if b != nil {
		v.Billing = &BillingRef{ID: b.ID, Status: b.Status, TotalMinor: b.TotalMinor, Currency: b.Currency}
		if id, number, ok := s.Billing.InvoiceRef(ctx, q, b.ID); ok {
			v.Billing.InvoiceID, v.Billing.InvoiceNumber = &id, &number
		}
		v.AmountMinor = b.TotalMinor
	}
	if c.State == "finalized" {
		if id, ok := s.Orders.IDForCycle(ctx, q, c.ID); ok {
			v.OrderID = &id
		}
	}
	menu, err := s.Menus.ForWeek(ctx, q, c.WeekStart)
	if err != nil {
		return nil, err
	}
	v.MenuPublished = menu != nil && (menu.Status == "published" || menu.Status == "closed")
	v.SelectionOpen = c.State == "scheduled" && now.Before(c.CutoffAt) && menu != nil && menu.Published() && menu.AllowsPlan(c.PlanID)
	sel, err := s.Subs.SelectionFor(ctx, q, c.ID)
	if err != nil {
		return nil, err
	}
	if sel != nil {
		v.SelectionStatus, v.SelectedCount = &sel.Status, sel.Count
	}
	return v, nil
}

// SubscriptionView is the customer's subscription.
type SubscriptionView struct {
	ID             string             `json:"id"`
	Status         string             `json:"status"`
	StatusLabel    string             `json:"status_label"`
	Plan           PlanRef            `json:"plan"`
	AmountMinor    int64              `json:"amount_minor"`
	Currency       string             `json:"currency"`
	PriceFormatted string             `json:"price_formatted"`
	Address        *addresses.Address `json:"address"`
	PaymentMethod  *string            `json:"payment_method"`
	AutoRenew      bool               `json:"auto_renew"`
	FirstWeek      string             `json:"first_week"`
	ActivatedAt    *time.Time         `json:"activated_at"`
	Pause          *WeekRange         `json:"pause"`
	Cancel         *CancelInfo        `json:"cancel"`
	Cycles         []CycleView        `json:"cycles"`
	Actions        Actions            `json:"actions"`
	// Outstanding is a week that can be paid now (failed or due renewal, or
	// an unfinished first payment).
	Outstanding *CycleView `json:"outstanding"`
	Terms       string     `json:"terms"`
	Test        bool       `json:"test_payments"`
}

type WeekRange struct {
	FromWeek  string `json:"from_week"`
	UntilWeek string `json:"until_week"`
	Label     string `json:"label"`
}

type CancelInfo struct {
	EffectiveWeek string     `json:"effective_week"`
	RequestedAt   *time.Time `json:"requested_at"`
	Label         string     `json:"label"`
}

type Actions struct {
	ChangePlan    bool `json:"change_plan"`
	Pause         bool `json:"pause"`
	Resume        bool `json:"resume"`
	Cancel        bool `json:"cancel"`
	UndoCancel    bool `json:"undo_cancel"`
	ChangeAddress bool `json:"change_address"`
	MaxPauseWeeks int  `json:"max_pause_weeks"`
}

// Terms is the renewal wording shown before payment and on the
// subscription page. It states when and how much, and how to stop.
func (s *Service) Terms(amount int64, currency string) string {
	lead := s.Billing.Policy.RenewalLead
	p := s.Menus.Policy
	example := p.CutoffFor(p.NextOpenWeek(s.Now()))
	when := example.Add(-lead).In(p.Location).Format("Monday at 3:04 pm")
	return fmt.Sprintf("After your first week, %s is charged for each week on %s (Trinidad and Tobago time), before that week's menu closes. "+
		"Pause or cancel at any time before a week is charged; weeks already paid are still delivered. Payments are not refunded automatically.",
		money.Format(amount, currency), when)
}

func (s *Service) subscriptionView(ctx context.Context, q db.Querier, sub *subscriptions.Subscription) (*SubscriptionView, error) {
	planName, err := s.Plans.NameOf(ctx, q, sub.PlanID)
	if err != nil {
		return nil, err
	}
	st := subscriptions.Status(sub.Status)
	v := &SubscriptionView{ID: sub.ID, Status: sub.Status, StatusLabel: st.Label(),
		Plan:        PlanRef{ID: sub.PlanID, Name: planName, MealsPerWeek: int(sub.MealsPerWeek)},
		AmountMinor: sub.AmountMinor, Currency: sub.Currency, PriceFormatted: money.Format(sub.AmountMinor, sub.Currency),
		FirstWeek: menus.FormatWeek(sub.FirstWeek), ActivatedAt: sub.ActivatedAt, Cycles: []CycleView{},
		Terms: s.Terms(sub.AmountMinor, sub.Currency)}
	if _, live, _, ok := s.Payments.Available(); ok {
		v.Test = !live
	}
	if sub.DeliveryAddressID != nil {
		if a, err := addresses.Lookup(ctx, q, s.Delivery, sub.CustomerID, *sub.DeliveryAddressID); err == nil {
			v.Address = a
		}
	}
	if sub.PaymentMethodID != nil {
		if label, ok := s.Payments.MethodLabel(ctx, *sub.PaymentMethodID); ok {
			v.PaymentMethod = &label
			v.AutoRenew = s.Payments.CanChargeMethod(ctx, *sub.PaymentMethodID)
		}
	}
	if sub.PauseFromWeek != nil && sub.PauseUntilWeek != nil {
		last := sub.PauseUntilWeek.AddDate(0, 0, -1)
		v.Pause = &WeekRange{FromWeek: menus.FormatWeek(*sub.PauseFromWeek), UntilWeek: menus.FormatWeek(*sub.PauseUntilWeek),
			Label: sub.PauseFromWeek.Format("2 January") + " – " + last.Format("2 January 2006")}
	}
	if sub.CancelEffectiveWeek != nil {
		v.Cancel = &CancelInfo{EffectiveWeek: menus.FormatWeek(*sub.CancelEffectiveWeek), RequestedAt: sub.CancelRequestedAt,
			Label: "No deliveries from the week of " + sub.CancelEffectiveWeek.Format("2 January 2006")}
	}
	cycles, err := s.Subs.CyclesOfSubscription(ctx, sub.ID)
	if err != nil {
		return nil, err
	}
	now := s.Now()
	for i := range cycles {
		c := cycles[i]
		// Upcoming weeks and the last few past ones.
		if len(v.Cycles) >= 6 {
			break
		}
		cv, err := s.cycleView(ctx, q, &c.Cycle, c.PlanName)
		if err != nil {
			return nil, err
		}
		v.Cycles = append(v.Cycles, *cv)
		if c.State == "scheduled" && now.Before(c.CutoffAt) && cv.Billing != nil &&
			(cv.Billing.Status == "due" || cv.Billing.Status == "failed" || cv.Billing.Status == "pending") {
			copy := *cv
			v.Outstanding = &copy
		}
	}
	v.Actions = Actions{
		ChangePlan:    st == subscriptions.Active || st == subscriptions.PauseScheduled || st == subscriptions.Paused || st == subscriptions.PastDue,
		Pause:         st == subscriptions.Active,
		Resume:        st == subscriptions.Paused || st == subscriptions.PauseScheduled,
		Cancel:        st == subscriptions.Active || st == subscriptions.PauseScheduled || st == subscriptions.Paused || st == subscriptions.PastDue,
		UndoCancel:    st == subscriptions.CancelScheduled,
		ChangeAddress: st.Live(),
		MaxPauseWeeks: s.Subs.Opts.MaxPauseWeeks,
	}
	return v, nil
}

// errNoSubscription is the answer to an action when there is none.
var errNoSubscription = httpx.NewError(http.StatusNotFound, "no_subscription", "You don't have a subscription.")

// contactSnapshot is who an invoice is addressed to.
func (s *Service) contactSnapshot(ctx context.Context, q db.Querier, sub *subscriptions.Subscription) billing.CustomerSnapshot {
	snap := billing.CustomerSnapshot{Address: []string{}}
	if uid, err := customers.UserIDOf(ctx, q, sub.CustomerID); err == nil {
		if c, err := s.Identity.ContactOf(ctx, uid); err == nil {
			snap.Name = strings.TrimSpace(c.FirstName + " " + c.LastName)
			snap.Email = c.Email
		}
	}
	if sub.DeliveryAddressID != nil {
		if a, err := addresses.Lookup(ctx, q, s.Delivery, sub.CustomerID, *sub.DeliveryAddressID); err == nil {
			snap.Address = addressLines(a)
		}
	}
	return snap
}

func addressLines(a *addresses.Address) []string {
	lines := []string{a.Line1}
	if a.Line2 != nil {
		lines = append(lines, *a.Line2)
	}
	if a.Area != nil {
		lines = append(lines, *a.Area)
	}
	lines = append(lines, a.City+", "+a.RegionLabel)
	return lines
}

func orderAddress(a *addresses.Address) orders.Address {
	return orders.Address{RecipientName: a.RecipientName, Phone: a.Phone, Line1: a.Line1, Line2: a.Line2, Area: a.Area,
		City: a.City, Region: a.Region, RegionLabel: a.RegionLabel, Country: a.Country, Instructions: a.Instructions}
}

func formatMoney(minor int64, currency string) string { return money.Format(minor, currency) }

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
