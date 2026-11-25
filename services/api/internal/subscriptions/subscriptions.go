// Package subscriptions owns a customer's subscription, its weekly cycles
// and the meals chosen for each cycle.
//
// A subscription is the agreement (plan, address, status). A cycle is one
// delivery week of it, created once per (subscription, week) with its own
// copy of the plan and price. Changing the subscription never rewrites a
// cycle that has been paid or finalised.
//
// The functions here are the building blocks; each takes the caller's
// transaction (db.Querier). The workflows that also involve billing,
// payments and orders are in internal/commerce.
package subscriptions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/menus"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/outbox"
	"mise.tt/api/internal/subscriptions/store"
)

type Options struct {
	// RenewalLead: a week is billed this long before its cutoff.
	RenewalLead time.Duration
	// MaxPauseWeeks is the longest pause a customer can ask for.
	MaxPauseWeeks int
}

type Service struct {
	pool   *pgxpool.Pool
	Policy menus.Policy
	Opts   Options
	Now    func() time.Time
}

func NewService(pool *pgxpool.Pool, policy menus.Policy, opts Options) *Service {
	if opts.MaxPauseWeeks < 1 {
		opts.MaxPauseWeeks = 4
	}
	return &Service{pool: pool, Policy: policy, Opts: opts, Now: time.Now}
}

func (s *Service) Pool() *pgxpool.Pool { return s.pool }

// Subscription is the stored subscription.
type Subscription = store.Subscription

// Cycle is one stored delivery week.
type Cycle = store.SubscriptionCycle

// ---------------------------------------------------------------- reading

func (s *Service) Get(ctx context.Context, q db.Querier, id string, lock bool) (*Subscription, error) {
	var sub Subscription
	var err error
	if lock {
		sub, err = store.New(q).GetSubscriptionForUpdate(ctx, id)
	} else {
		sub, err = store.New(q).GetSubscription(ctx, id)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

// LiveFor returns the customer's subscription in progress, or nil.
func (s *Service) LiveFor(ctx context.Context, q db.Querier, customerID string) (*Subscription, error) {
	sub, err := store.New(q).LiveSubscription(ctx, customerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

// LatestFor returns the customer's most recent subscription, or nil.
func (s *Service) LatestFor(ctx context.Context, q db.Querier, customerID string) (*Subscription, error) {
	sub, err := store.New(q).LatestSubscription(ctx, customerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

// ForCustomer returns a subscription only if it belongs to the customer;
// otherwise not found, exactly like an unknown id. lock takes a row lock.
func (s *Service) ForCustomer(ctx context.Context, q db.Querier, customerID, id string, lock bool) (*Subscription, error) {
	sub, err := s.Get(ctx, q, id, lock)
	if err != nil {
		return nil, err
	}
	if sub.CustomerID != customerID {
		return nil, httpx.ErrNotFound
	}
	return sub, nil
}

func (s *Service) Cycle(ctx context.Context, q db.Querier, id string, lock bool) (*Cycle, error) {
	var c Cycle
	var err error
	if lock {
		c, err = store.New(q).GetCycleForUpdate(ctx, id)
	} else {
		c, err = store.New(q).GetCycle(ctx, id)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// CycleForCustomer returns a cycle only if it belongs to the customer.
func (s *Service) CycleForCustomer(ctx context.Context, q db.Querier, customerID, id string) (*Cycle, error) {
	c, err := store.New(q).CycleForCustomer(ctx, store.CycleForCustomerParams{ID: id, CustomerID: customerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// CycleForWeek returns the subscription's cycle for a week, or nil.
func (s *Service) CycleForWeek(ctx context.Context, q db.Querier, subscriptionID string, week time.Time) (*Cycle, error) {
	c, err := store.New(q).CycleForWeek(ctx, store.CycleForWeekParams{SubscriptionID: subscriptionID, WeekStart: week})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ScheduledCycles are the weeks of a subscription not yet finalised.
func (s *Service) ScheduledCycles(ctx context.Context, q db.Querier, subscriptionID string) ([]Cycle, error) {
	return store.New(q).ScheduledCycles(ctx, subscriptionID)
}

// PlanNameForCycle names the plan of a cycle (billing uses it on invoices).
func (s *Service) PlanNameForCycle(ctx context.Context, q db.Querier, cycleID string) (string, error) {
	return store.New(q).CyclePlanName(ctx, cycleID)
}

// AddressInUse reports whether a live subscription delivers to an address.
func (s *Service) AddressInUse(ctx context.Context, q db.Querier, addressID string) (bool, error) {
	return store.New(q).AddressInUse(ctx, &addressID)
}

// ---------------------------------------------------------------- writing

// Event is a history entry.
type Event struct {
	Type          string
	EffectiveWeek *time.Time
	Actor         string
	Metadata      map[string]any
}

func (s *Service) record(ctx context.Context, q db.Querier, sub *Subscription, from, to *string, e Event) error {
	if e.Metadata == nil {
		e.Metadata = map[string]any{}
	}
	meta, _ := json.Marshal(e.Metadata)
	var actor *string
	if e.Actor != "" {
		actor = &e.Actor
	}
	historyID, err := store.New(q).AddEvent(ctx, store.AddEventParams{SubscriptionID: sub.ID, Type: e.Type, FromStatus: from, ToStatus: to,
		EffectiveWeek: e.EffectiveWeek, Actor: actor, Metadata: meta})
	if err != nil {
		return err
	}
	// The same fact as a domain event, in the same transaction: analytics and
	// automation react to it. One event per history row (the row id is the
	// dedupe key), so a replayed or retried change never publishes twice.
	payload := map[string]any{"subscription_id": sub.ID, "customer_id": sub.CustomerID, "plan_id": sub.PlanID,
		"status": sub.Status, "history_id": historyID}
	if from != nil {
		payload["from_status"] = *from
	}
	if to != nil {
		payload["to_status"] = *to
	}
	if e.EffectiveWeek != nil {
		payload["effective_week"] = menus.FormatWeek(*e.EffectiveWeek)
	}
	if err := outbox.Record(ctx, q, outbox.Event{Type: "subscription." + e.Type, AggregateType: "subscription", AggregateID: sub.ID,
		DedupeKey: strconv.FormatInt(historyID, 10), Payload: payload}); err != nil {
		return err
	}
	auditMeta := map[string]any{}
	for k, v := range e.Metadata {
		auditMeta[k] = v
	}
	if from != nil {
		auditMeta["from"] = *from
	}
	if to != nil {
		auditMeta["to"] = *to
	}
	if e.EffectiveWeek != nil {
		auditMeta["effective_week"] = menus.FormatWeek(*e.EffectiveWeek)
	}
	return audit.Record(ctx, q, audit.Entry{ActorUserID: e.Actor, Action: "subscription." + e.Type, TargetType: "subscription",
		TargetID: sub.ID, Metadata: auditMeta})
}

// Note records an event that is not a status change.
func (s *Service) Note(ctx context.Context, q db.Querier, sub *Subscription, e Event) error {
	return s.record(ctx, q, sub, nil, nil, e)
}

var errTransition = func(from, to Status) error {
	return httpx.NewError(http.StatusConflict, "invalid_transition",
		fmt.Sprintf("A subscription that is %s cannot become %s.", from.Label(), to.Label()))
}

// Transition changes a subscription's status, if the change is legal, and
// records it. A change to the status it already has is a no-op (so a retried
// activation changes nothing).
func (s *Service) Transition(ctx context.Context, q db.Querier, sub *Subscription, to Status, e Event) error {
	from := Status(sub.Status)
	if from == to {
		return nil
	}
	if !CanTransition(from, to) {
		return errTransition(from, to)
	}
	updated, err := store.New(q).SetSubscriptionStatus(ctx, store.SetSubscriptionStatusParams{ID: sub.ID, Status: string(to), Now: s.Now()})
	if err != nil {
		return err
	}
	if e.Type == "" {
		e.Type = string(to)
	}
	f, t := string(from), string(to)
	*sub = updated
	return s.record(ctx, q, sub, &f, &t, e)
}

// NewSubscription is what a subscription is created from.
type NewSubscription struct {
	CustomerID   string
	PlanID       string
	PlanPriceID  string
	MealsPerWeek int
	AmountMinor  int64
	Currency     string
	AddressID    string
	Actor        string
}

// CreatePending creates a subscription waiting for its first payment, and
// its first week: the next week that is still open for selection.
func (s *Service) CreatePending(ctx context.Context, q db.Querier, in NewSubscription) (*Subscription, *Cycle, error) {
	week := s.Policy.NextOpenWeek(s.Now())
	sub, err := store.New(q).CreateSubscription(ctx, store.CreateSubscriptionParams{
		CustomerID: in.CustomerID, PlanID: in.PlanID, PlanPriceID: in.PlanPriceID, MealsPerWeek: int16(in.MealsPerWeek),
		AmountMinor: in.AmountMinor, Currency: in.Currency, DeliveryAddressID: &in.AddressID, FirstWeek: week,
	})
	if db.IsUniqueViolation(err, "subscriptions_one_live") {
		return nil, nil, httpx.NewError(http.StatusConflict, "subscription_exists", "You already have a subscription.")
	}
	if err != nil {
		return nil, nil, err
	}
	if err := s.record(ctx, q, &sub, nil, ptr(string(PendingPayment)), Event{Type: "created", Actor: in.Actor, EffectiveWeek: &week,
		Metadata: map[string]any{"plan_id": in.PlanID, "plan_price_id": in.PlanPriceID}}); err != nil {
		return nil, nil, err
	}
	c, _, err := s.EnsureCycle(ctx, q, &sub, week)
	if err != nil {
		return nil, nil, err
	}
	return &sub, c, nil
}

func ptr[T any](v T) *T { return &v }

// renewalAt is when a week is billed: RenewalLead before its cutoff, but
// never before the cycle exists.
func (s *Service) renewalAt(cutoff time.Time) time.Time {
	r := cutoff.Add(-s.Opts.RenewalLead)
	if now := s.Now(); r.Before(now) {
		r = now
	}
	if r.After(cutoff) {
		r = cutoff
	}
	return r
}

// InPause reports whether a week falls in the subscription's pause.
func InPause(sub *Subscription, week time.Time) bool {
	return sub.PauseFromWeek != nil && sub.PauseUntilWeek != nil &&
		!week.Before(*sub.PauseFromWeek) && week.Before(*sub.PauseUntilWeek)
}

// EnsureCycle creates the subscription's cycle for a week from its current
// plan, unless it exists. A week inside a pause is created as paused. It
// returns the cycle and whether it was created now.
func (s *Service) EnsureCycle(ctx context.Context, q db.Querier, sub *Subscription, week time.Time) (*Cycle, bool, error) {
	if existing, err := s.CycleForWeek(ctx, q, sub.ID, week); err != nil || existing != nil {
		return existing, false, err
	}
	cutoff := s.Policy.CutoffFor(week)
	state := "scheduled"
	var reason *string
	if InPause(sub, week) {
		state, reason = "paused", ptr("paused")
	}
	c, err := store.New(q).InsertCycle(ctx, store.InsertCycleParams{
		SubscriptionID: sub.ID, CustomerID: sub.CustomerID, WeekStart: week, CutoffAt: cutoff, RenewalAt: s.renewalAt(cutoff),
		PlanID: sub.PlanID, PlanPriceID: sub.PlanPriceID, MealsPerWeek: sub.MealsPerWeek, AmountMinor: sub.AmountMinor,
		Currency: sub.Currency, State: state, StateReason: reason,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, err := s.CycleForWeek(ctx, q, sub.ID, week)
		return existing, false, err
	}
	if err != nil {
		return nil, false, err
	}
	return &c, true, nil
}

// SetCycleState records how a week ended (or that it was paused or
// cancelled before it started).
func (s *Service) SetCycleState(ctx context.Context, q db.Querier, cycleID, state, reason string) (*Cycle, error) {
	var r *string
	if reason != "" {
		r = &reason
	}
	c, err := store.New(q).SetCycleState(ctx, store.SetCycleStateParams{ID: cycleID, State: state, StateReason: r, Now: s.Now()})
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// Plan is what a cycle's plan snapshot is set to.
type Plan struct {
	PlanID       string
	PlanPriceID  string
	MealsPerWeek int
	AmountMinor  int64
	Currency     string
}

// SetPlan changes the plan future weeks are created with.
func (s *Service) SetPlan(ctx context.Context, q db.Querier, sub *Subscription, p Plan) error {
	if err := store.New(q).SetSubscriptionPlan(ctx, store.SetSubscriptionPlanParams{ID: sub.ID, PlanID: p.PlanID,
		PlanPriceID: p.PlanPriceID, MealsPerWeek: int16(p.MealsPerWeek), AmountMinor: p.AmountMinor, Currency: p.Currency}); err != nil {
		return err
	}
	sub.PlanID, sub.PlanPriceID, sub.MealsPerWeek, sub.AmountMinor, sub.Currency = p.PlanID, p.PlanPriceID, int16(p.MealsPerWeek), p.AmountMinor, p.Currency
	return nil
}

// SetCyclePlan changes the plan of a week that has not been billed.
func (s *Service) SetCyclePlan(ctx context.Context, q db.Querier, cycleID string, p Plan) error {
	return store.New(q).SetCyclePlan(ctx, store.SetCyclePlanParams{ID: cycleID, PlanID: p.PlanID, PlanPriceID: p.PlanPriceID,
		MealsPerWeek: int16(p.MealsPerWeek), AmountMinor: p.AmountMinor, Currency: p.Currency})
}

// RecordChange records a plan change.
func (s *Service) RecordChange(ctx context.Context, q db.Querier, sub *Subscription, fromPrice, toPrice string, week time.Time, actor string) error {
	var a *string
	if actor != "" {
		a = &actor
	}
	if err := store.New(q).AddChange(ctx, store.AddChangeParams{SubscriptionID: sub.ID, FromPlanPriceID: fromPrice,
		ToPlanPriceID: toPrice, EffectiveWeek: week, RequestedBy: a}); err != nil {
		return err
	}
	return s.Note(ctx, q, sub, Event{Type: "plan_changed", EffectiveWeek: &week, Actor: actor,
		Metadata: map[string]any{"from_plan_price_id": fromPrice, "to_plan_price_id": toPrice}})
}

// SetPause records a pause [from, until), or clears it (from nil).
func (s *Service) SetPause(ctx context.Context, q db.Querier, sub *Subscription, from, until *time.Time, actor, pauseStatus string) error {
	st := store.New(q)
	if err := st.SetSubscriptionPause(ctx, store.SetSubscriptionPauseParams{ID: sub.ID, PauseFromWeek: from, PauseUntilWeek: until}); err != nil {
		return err
	}
	sub.PauseFromWeek, sub.PauseUntilWeek = from, until
	if from == nil {
		return st.EndPauses(ctx, store.EndPausesParams{SubscriptionID: sub.ID, Status: pauseStatus})
	}
	var a *string
	if actor != "" {
		a = &actor
	}
	return st.AddPause(ctx, store.AddPauseParams{SubscriptionID: sub.ID, FromWeek: *from, UntilWeek: *until, Status: pauseStatus, RequestedBy: a})
}

// ActivatePause marks a scheduled pause as running.
func (s *Service) ActivatePause(ctx context.Context, q db.Querier, sub *Subscription) error {
	return store.New(q).ActivatePauses(ctx, sub.ID)
}

// SetCancel records (or clears, with week nil) a cancellation request.
func (s *Service) SetCancel(ctx context.Context, q db.Querier, sub *Subscription, week *time.Time, reason string) error {
	var at *time.Time
	var r *string
	if week != nil {
		now := s.Now()
		at = &now
		if reason != "" {
			r = &reason
		}
	}
	if err := store.New(q).SetSubscriptionCancel(ctx, store.SetSubscriptionCancelParams{ID: sub.ID, RequestedAt: at, EffectiveWeek: week, Reason: r}); err != nil {
		return err
	}
	sub.CancelRequestedAt, sub.CancelEffectiveWeek, sub.CancelReason = at, week, r
	return nil
}

// SetAddress changes where future orders are delivered.
func (s *Service) SetAddress(ctx context.Context, q db.Querier, sub *Subscription, addressID string) error {
	if err := store.New(q).SetSubscriptionAddress(ctx, store.SetSubscriptionAddressParams{ID: sub.ID, AddressID: &addressID}); err != nil {
		return err
	}
	sub.DeliveryAddressID = &addressID
	return nil
}

// SetPaymentMethod records the saved method renewals are charged to.
func (s *Service) SetPaymentMethod(ctx context.Context, q db.Querier, sub *Subscription, methodID string) error {
	if err := store.New(q).SetSubscriptionPaymentMethod(ctx, store.SetSubscriptionPaymentMethodParams{ID: sub.ID, PaymentMethodID: &methodID}); err != nil {
		return err
	}
	// Who bills renewals: Mise, through the method's provider. Never changed
	// for a provider-run subscription, so two systems cannot both charge.
	if _, err := q.Exec(ctx, `UPDATE subscriptions SET billing_provider = pm.provider, billing_authority = 'mise'
		FROM payment_methods pm WHERE subscriptions.id = $1 AND pm.id = $2 AND subscriptions.billing_authority <> 'provider'`, sub.ID, methodID); err != nil {
		return err
	}
	sub.PaymentMethodID = &methodID
	return nil
}

// MiseBillsRenewals reports whether Mise may charge this subscription's
// renewals itself: it is the billing authority and renewals are automatic.
func (s *Service) MiseBillsRenewals(ctx context.Context, q db.Querier, subscriptionID string) (bool, error) {
	var authority, renewal string
	if err := q.QueryRow(ctx, `SELECT billing_authority, renewal_status FROM subscriptions WHERE id = $1`, subscriptionID).Scan(&authority, &renewal); err != nil {
		return false, err
	}
	return authority == "mise" && renewal == "automatic", nil
}

// ---------------------------------------------------------------- scheduler queries

func (s *Service) Delivering(ctx context.Context, q db.Querier) ([]string, error) {
	return store.New(q).SubscriptionsNeedingCycles(ctx)
}

func (s *Service) StalePending(ctx context.Context, q db.Querier, before time.Time) ([]string, error) {
	return store.New(q).StalePendingSubscriptions(ctx, before)
}

func (s *Service) CyclesPastCutoff(ctx context.Context, q db.Querier, now time.Time) ([]string, error) {
	return store.New(q).CyclesPastCutoff(ctx, now)
}

func (s *Service) CyclesDueForRenewal(ctx context.Context, q db.Querier, now time.Time) ([]string, error) {
	return store.New(q).CyclesDueForRenewal(ctx, now)
}

// Reminder is a week whose meals are not confirmed yet.
type Reminder struct {
	CycleID    string
	CustomerID string
	Week       time.Time
	CutoffAt   time.Time
}

func (s *Service) NearingCutoff(ctx context.Context, q db.Querier, now, until time.Time) ([]Reminder, error) {
	rows, err := store.New(q).CyclesNearingCutoff(ctx, store.CyclesNearingCutoffParams{Now: now, Until: until})
	if err != nil {
		return nil, err
	}
	out := make([]Reminder, len(rows))
	for i, r := range rows {
		out[i] = Reminder{CycleID: r.CycleID, CustomerID: r.CustomerID, Week: r.WeekStart, CutoffAt: r.CutoffAt}
	}
	return out, nil
}

// InWeek lists the scheduled cycles of a week (for "the menu is out" emails).
func (s *Service) InWeek(ctx context.Context, q db.Querier, week time.Time) ([]Reminder, error) {
	rows, err := store.New(q).CustomersWithCycleInWeek(ctx, store.CustomersWithCycleInWeekParams{WeekStart: week, UnconfirmedOnly: false})
	if err != nil {
		return nil, err
	}
	out := make([]Reminder, len(rows))
	for i, r := range rows {
		out[i] = Reminder{CycleID: r.CycleID, CustomerID: r.CustomerID, Week: week, CutoffAt: r.CutoffAt}
	}
	return out, nil
}

// ---------------------------------------------------------------- history

// HistoryEntry is one subscription event.
type HistoryEntry struct {
	Type          string         `json:"type"`
	FromStatus    *string        `json:"from_status"`
	ToStatus      *string        `json:"to_status"`
	EffectiveWeek *string        `json:"effective_week"`
	Metadata      map[string]any `json:"metadata"`
	CreatedAt     time.Time      `json:"created_at"`
}

func (s *Service) History(ctx context.Context, subscriptionID string) ([]HistoryEntry, error) {
	rows, err := store.New(s.pool).ListEvents(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	out := []HistoryEntry{}
	for _, r := range rows {
		e := HistoryEntry{Type: r.Type, FromStatus: r.FromStatus, ToStatus: r.ToStatus, CreatedAt: r.CreatedAt}
		if r.EffectiveWeek != nil {
			e.EffectiveWeek = ptr(menus.FormatWeek(*r.EffectiveWeek))
		}
		_ = json.Unmarshal(r.Metadata, &e.Metadata)
		out = append(out, e)
	}
	return out, nil
}

// CycleRow is a cycle with its plan's name.
type CycleRow struct {
	Cycle
	PlanName string
}

func (s *Service) CyclesOfCustomer(ctx context.Context, customerID string, limit int) ([]CycleRow, error) {
	rows, err := store.New(s.pool).CyclesForCustomer(ctx, store.CyclesForCustomerParams{CustomerID: customerID, Lim: int32(limit)})
	if err != nil {
		return nil, err
	}
	out := make([]CycleRow, len(rows))
	for i, r := range rows {
		out[i] = CycleRow{Cycle: Cycle{ID: r.ID, SubscriptionID: r.SubscriptionID, CustomerID: r.CustomerID, WeekStart: r.WeekStart,
			CutoffAt: r.CutoffAt, RenewalAt: r.RenewalAt, PlanID: r.PlanID, PlanPriceID: r.PlanPriceID, MealsPerWeek: r.MealsPerWeek,
			AmountMinor: r.AmountMinor, Currency: r.Currency, State: r.State, StateReason: r.StateReason, FinalizedAt: r.FinalizedAt,
			CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}, PlanName: r.PlanName}
	}
	return out, nil
}

func (s *Service) CyclesOfSubscription(ctx context.Context, subscriptionID string) ([]CycleRow, error) {
	rows, err := store.New(s.pool).CyclesForSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	out := make([]CycleRow, len(rows))
	for i, r := range rows {
		out[i] = CycleRow{Cycle: Cycle{ID: r.ID, SubscriptionID: r.SubscriptionID, CustomerID: r.CustomerID, WeekStart: r.WeekStart,
			CutoffAt: r.CutoffAt, RenewalAt: r.RenewalAt, PlanID: r.PlanID, PlanPriceID: r.PlanPriceID, MealsPerWeek: r.MealsPerWeek,
			AmountMinor: r.AmountMinor, Currency: r.Currency, State: r.State, StateReason: r.StateReason, FinalizedAt: r.FinalizedAt,
			CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}, PlanName: r.PlanName}
	}
	return out, nil
}

// AdminRow is a subscription in the staff list.
type AdminRow struct {
	Subscription
	PlanName string
}

func (s *Service) List(ctx context.Context, status, customerID string, page, size int) ([]AdminRow, error) {
	if size < 1 || size > 100 {
		size = 50
	}
	if page < 1 {
		page = 1
	}
	params := store.ListSubscriptionsParams{Lim: int32(size), Off: int32((page - 1) * size)}
	if status != "" {
		params.Status = &status
	}
	if customerID != "" {
		params.CustomerID = &customerID
	}
	rows, err := store.New(s.pool).ListSubscriptions(ctx, params)
	if err != nil {
		return nil, err
	}
	out := make([]AdminRow, len(rows))
	for i, r := range rows {
		out[i] = AdminRow{Subscription: Subscription{ID: r.ID, CustomerID: r.CustomerID, Status: r.Status, PlanID: r.PlanID,
			PlanPriceID: r.PlanPriceID, MealsPerWeek: r.MealsPerWeek, AmountMinor: r.AmountMinor, Currency: r.Currency,
			DeliveryAddressID: r.DeliveryAddressID, PaymentMethodID: r.PaymentMethodID, FirstWeek: r.FirstWeek,
			ActivatedAt: r.ActivatedAt, PauseFromWeek: r.PauseFromWeek, PauseUntilWeek: r.PauseUntilWeek,
			CancelRequestedAt: r.CancelRequestedAt, CancelEffectiveWeek: r.CancelEffectiveWeek, CancelReason: r.CancelReason,
			CancelledAt: r.CancelledAt, ExpiredAt: r.ExpiredAt, Version: r.Version, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt},
			PlanName: r.PlanName}
	}
	return out, nil
}

// AlignCutoffs moves the cutoff of every scheduled cycle of a week to the
// cutoff its menu was published with.
func (s *Service) AlignCutoffs(ctx context.Context, q db.Querier, week, cutoff time.Time) (int64, error) {
	return store.New(q).AlignCycleCutoffs(ctx, store.AlignCycleCutoffsParams{WeekStart: week, CutoffAt: cutoff})
}
