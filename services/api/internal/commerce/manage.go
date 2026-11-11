package commerce

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/addresses"
	"mise.tt/api/internal/billing"
	"mise.tt/api/internal/delivery"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/subscriptions"
)

// Current returns the customer's subscription in progress, or nil.
func (s *Service) Current(ctx context.Context, p *identity.Principal) (*SubscriptionView, error) {
	cid, err := s.customerID(ctx, p)
	if err != nil {
		return nil, err
	}
	sub, err := s.Subs.LiveFor(ctx, s.pool, cid)
	if err != nil || sub == nil {
		return nil, err
	}
	return s.subscriptionView(ctx, s.pool, sub)
}

// manage runs fn on the customer's subscription id, locked, in a
// transaction, and returns the updated view.
func (s *Service) manage(ctx context.Context, p *identity.Principal, id string, fn func(tx pgx.Tx, sub *subscriptions.Subscription) error) (*SubscriptionView, error) {
	cid, err := s.customerID(ctx, p)
	if err != nil {
		return nil, err
	}
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		sub, err := s.Subs.ForCustomer(ctx, tx, cid, id, true)
		if err != nil {
			return err
		}
		return fn(tx, sub)
	})
	if err != nil {
		return nil, err
	}
	sub, err := s.Subs.Get(ctx, s.pool, id, false)
	if err != nil {
		return nil, err
	}
	return s.subscriptionView(ctx, s.pool, sub)
}

func notAllowed(what string, st subscriptions.Status) error {
	return httpx.NewError(http.StatusConflict, "not_allowed", "You can't "+what+" while your subscription is "+strings.ToLower(st.Label())+".")
}

// openWeek is the subscription's next week that has not closed, and its
// billing, if those exist yet.
type openWeek struct {
	cycle *subscriptions.Cycle
	bill  *billing.Cycle
}

func (o openWeek) paid() bool   { return o.bill != nil && o.bill.Status == "paid" }
func (o openWeek) billed() bool { return o.bill != nil }

// upcoming returns the scheduled weeks whose selection is still open.
func (s *Service) upcoming(ctx context.Context, q db.Querier, sub *subscriptions.Subscription) ([]openWeek, error) {
	cycles, err := s.Subs.ScheduledCycles(ctx, q, sub.ID)
	if err != nil {
		return nil, err
	}
	now := s.Now()
	var out []openWeek
	for i := range cycles {
		c := cycles[i]
		if !now.Before(c.CutoffAt) {
			continue
		}
		b, err := s.Billing.ForSubscriptionCycle(ctx, q, c.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, openWeek{cycle: &c, bill: b})
	}
	return out, nil
}

// nextUncreatedWeek is the first week after every week that exists.
func (s *Service) nextUncreatedWeek(ctx context.Context, q db.Querier, sub *subscriptions.Subscription, weeks []openWeek) time.Time {
	w := s.Menus.Policy.NextOpenWeek(s.Now())
	for _, o := range weeks {
		if !o.cycle.WeekStart.Before(w) {
			w = o.cycle.WeekStart.AddDate(0, 0, 7)
		}
	}
	return w
}

// stopWeek takes an unpaid upcoming week out of delivery: its billing is
// voided (nothing is owed for a week that will not be delivered).
func (s *Service) stopWeek(ctx context.Context, q db.Querier, o openWeek, state, reason string) error {
	if o.bill != nil {
		if err := s.Billing.Void(ctx, q, o.bill.ID, reason); err != nil {
			return err
		}
		if err := s.Payments.CancelOpenCheckouts(ctx, q, o.bill.ID); err != nil {
			return err
		}
	}
	_, err := s.Subs.SetCycleState(ctx, q, o.cycle.ID, state, reason)
	return err
}

// ---------------------------------------------------------------- change plan

type ChangePlanInput struct {
	PlanID string `json:"plan_id"`
}

// ChangePlan moves the subscription to another plan from the first week not
// yet billed. There is no proration (none has been approved): weeks already
// billed keep their plan and price.
func (s *Service) ChangePlan(ctx context.Context, p *identity.Principal, id string, in ChangePlanInput) (*SubscriptionView, error) {
	return s.manage(ctx, p, id, func(tx pgx.Tx, sub *subscriptions.Subscription) error {
		st := subscriptions.Status(sub.Status)
		if st != subscriptions.Active && st != subscriptions.PauseScheduled && st != subscriptions.Paused && st != subscriptions.PastDue {
			return notAllowed("change plan", st)
		}
		selling, err := s.Plans.ForPurchase(ctx, tx, strings.ToLower(strings.TrimSpace(in.PlanID)))
		if err != nil {
			return err
		}
		if selling.PlanID == sub.PlanID {
			return httpx.NewError(http.StatusConflict, "same_plan", "You are already on this plan.")
		}
		weeks, err := s.upcoming(ctx, tx, sub)
		if err != nil {
			return err
		}
		plan := subscriptions.Plan{PlanID: selling.PlanID, PlanPriceID: selling.PriceID, MealsPerWeek: selling.MealsPerWeek,
			AmountMinor: selling.AmountMinor, Currency: selling.Currency}
		effective := s.nextUncreatedWeek(ctx, tx, sub, weeks)
		for _, o := range weeks {
			if o.billed() {
				continue
			}
			// Not billed yet: this week takes the new plan. If its meals were
			// confirmed for a different allowance, they need confirming again.
			if err := s.Subs.SetCyclePlan(ctx, tx, o.cycle.ID, plan); err != nil {
				return err
			}
			if int(o.cycle.MealsPerWeek) != selling.MealsPerWeek {
				if err := s.Subs.Unconfirm(ctx, tx, o.cycle.ID); err != nil {
					return err
				}
			}
			if o.cycle.WeekStart.Before(effective) {
				effective = o.cycle.WeekStart
			}
		}
		from := sub.PlanPriceID
		if err := s.Subs.SetPlan(ctx, tx, sub, plan); err != nil {
			return err
		}
		if err := s.Subs.RecordChange(ctx, tx, sub, from, selling.PriceID, effective, p.UserID); err != nil {
			return err
		}
		return s.notify(ctx, tx, "plan_changed:"+sub.ID+":"+selling.PriceID+":"+effective.Format("2006-01-02"), EmailPlanChanged, sub.CustomerID,
			map[string]string{"plan": selling.PlanName, "meals": itoa(selling.MealsPerWeek), "amount": formatMoney(selling.AmountMinor, selling.Currency),
				"week": weekLabel(effective), "link": s.link("/subscription")})
	})
}

// ---------------------------------------------------------------- pause

type PauseInput struct {
	Weeks int `json:"weeks"`
}

// Pause skips 1 to MaxPauseWeeks weeks, starting with the first week not
// yet paid. A paid week is never skipped: there are no automatic refunds.
func (s *Service) Pause(ctx context.Context, p *identity.Principal, id string, in PauseInput) (*SubscriptionView, error) {
	return s.manage(ctx, p, id, func(tx pgx.Tx, sub *subscriptions.Subscription) error {
		st := subscriptions.Status(sub.Status)
		if st != subscriptions.Active {
			return notAllowed("pause", st)
		}
		max := s.Subs.Opts.MaxPauseWeeks
		if in.Weeks < 1 || in.Weeks > max {
			return httpx.ValidationError([]httpx.FieldError{{Field: "weeks", Code: "range",
				Message: "Choose a pause of 1 to " + itoa(max) + " weeks."}})
		}
		weeks, err := s.upcoming(ctx, tx, sub)
		if err != nil {
			return err
		}
		from := s.nextUncreatedWeek(ctx, tx, sub, weeks)
		var stop []openWeek
		for _, o := range weeks {
			if o.paid() {
				continue
			}
			stop = append(stop, o)
			if o.cycle.WeekStart.Before(from) {
				from = o.cycle.WeekStart
			}
		}
		until := from.AddDate(0, 0, 7*in.Weeks)
		startsNow := len(stop) > 0
		pauseStatus := "scheduled"
		if startsNow {
			pauseStatus = "active"
		}
		if err := s.Subs.SetPause(ctx, tx, sub, &from, &until, p.UserID, pauseStatus); err != nil {
			return err
		}
		for _, o := range stop {
			if subscriptions.InPause(sub, o.cycle.WeekStart) {
				if err := s.stopWeek(ctx, tx, o, "paused", "paused"); err != nil {
					return err
				}
			}
		}
		to := subscriptions.PauseScheduled
		if startsNow {
			to = subscriptions.Paused
		}
		if err := s.Subs.Transition(ctx, tx, sub, to, subscriptions.Event{Type: string(to), EffectiveWeek: &from, Actor: p.UserID,
			Metadata: map[string]any{"weeks": in.Weeks}}); err != nil {
			return err
		}
		last := until.AddDate(0, 0, -1)
		return s.notify(ctx, tx, "paused:"+sub.ID+":"+from.Format("2006-01-02"), EmailPaused, sub.CustomerID, map[string]string{
			"from": from.Format("2 January"), "until": last.Format("2 January 2006"), "resume": weekLabel(until), "link": s.link("/subscription"),
		})
	})
}

// Resume ends a pause early, or withdraws a cancellation that has not
// taken effect yet.
func (s *Service) Resume(ctx context.Context, p *identity.Principal, id string) (*SubscriptionView, error) {
	return s.manage(ctx, p, id, func(tx pgx.Tx, sub *subscriptions.Subscription) error {
		st := subscriptions.Status(sub.Status)
		switch st {
		case subscriptions.PauseScheduled, subscriptions.Paused:
			if err := s.Subs.SetPause(ctx, tx, sub, nil, nil, p.UserID, "cancelled"); err != nil {
				return err
			}
			// Weeks paused but still open for selection come back; they are
			// billed at their usual time (or now, if that time has passed).
			cycles, err := s.Subs.CyclesOfSubscription(ctx, sub.ID)
			if err != nil {
				return err
			}
			now := s.Now()
			for _, c := range cycles {
				if c.State == "paused" && now.Before(c.CutoffAt) {
					if _, err := s.Subs.SetCycleState(ctx, tx, c.ID, "scheduled", ""); err != nil {
						return err
					}
				}
			}
			if err := s.Subs.Transition(ctx, tx, sub, subscriptions.Active, subscriptions.Event{Type: "resumed", Actor: p.UserID}); err != nil {
				return err
			}
			return s.notify(ctx, tx, "resumed:"+sub.ID+":"+now.Format(time.RFC3339), EmailResumed, sub.CustomerID, map[string]string{
				"week": weekLabel(s.Menus.Policy.NextOpenWeek(now)), "link": s.link("/menu"),
			})
		case subscriptions.CancelScheduled:
			if err := s.Subs.SetCancel(ctx, tx, sub, nil, ""); err != nil {
				return err
			}
			return s.Subs.Transition(ctx, tx, sub, subscriptions.Active, subscriptions.Event{Type: "cancellation_withdrawn", Actor: p.UserID})
		}
		return notAllowed("resume", st)
	})
}

// ---------------------------------------------------------------- cancel

type CancelInput struct {
	Reason string `json:"reason"`
}

// CancelReasons are the answers offered (optional, for the business).
var CancelReasons = []string{"too_expensive", "not_enough_time", "meals_not_for_me", "moving", "temporary", "other"}

// Cancel ends the subscription from the first week not yet paid. Weeks
// already paid are still delivered; nothing is refunded automatically.
// History (orders, invoices, payments) is kept.
func (s *Service) Cancel(ctx context.Context, p *identity.Principal, id string, in CancelInput) (*SubscriptionView, error) {
	reason := strings.TrimSpace(in.Reason)
	if reason != "" {
		ok := false
		for _, r := range CancelReasons {
			ok = ok || r == reason
		}
		if !ok {
			return nil, httpx.ValidationError([]httpx.FieldError{{Field: "reason", Code: "invalid", Message: "Choose a reason from the list."}})
		}
	}
	return s.manage(ctx, p, id, func(tx pgx.Tx, sub *subscriptions.Subscription) error {
		st := subscriptions.Status(sub.Status)
		if st != subscriptions.Active && st != subscriptions.PauseScheduled && st != subscriptions.Paused && st != subscriptions.PastDue {
			return notAllowed("cancel", st)
		}
		weeks, err := s.upcoming(ctx, tx, sub)
		if err != nil {
			return err
		}
		effective := s.nextUncreatedWeek(ctx, tx, sub, weeks)
		for _, o := range weeks {
			if o.paid() {
				continue
			}
			if err := s.stopWeek(ctx, tx, o, "cancelled", "cancelled"); err != nil {
				return err
			}
			if o.cycle.WeekStart.Before(effective) {
				effective = o.cycle.WeekStart
			}
		}
		if sub.PauseFromWeek != nil {
			if err := s.Subs.SetPause(ctx, tx, sub, nil, nil, p.UserID, "cancelled"); err != nil {
				return err
			}
		}
		if err := s.Subs.SetCancel(ctx, tx, sub, &effective, reason); err != nil {
			return err
		}
		pending, err := s.paidPending(ctx, tx, sub)
		if err != nil {
			return err
		}
		to := subscriptions.Cancelled
		if pending > 0 {
			to = subscriptions.CancelScheduled
		}
		meta := map[string]any{}
		if reason != "" {
			meta["reason"] = reason
		}
		if err := s.Subs.Transition(ctx, tx, sub, to, subscriptions.Event{Type: string(to), EffectiveWeek: &effective, Actor: p.UserID, Metadata: meta}); err != nil {
			return err
		}
		return s.notify(ctx, tx, "cancelled:"+sub.ID, EmailCancelled, sub.CustomerID, map[string]string{
			"week": weekLabel(effective), "paid_weeks": itoa(pending), "link": s.link("/plans"),
		})
	})
}

// paidPending counts paid weeks not yet finalised (still to be delivered).
func (s *Service) paidPending(ctx context.Context, q db.Querier, sub *subscriptions.Subscription) (int, error) {
	cycles, err := s.Subs.ScheduledCycles(ctx, q, sub.ID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, c := range cycles {
		b, err := s.Billing.ForSubscriptionCycle(ctx, q, c.ID)
		if err != nil {
			return 0, err
		}
		if b != nil && b.Status == "paid" {
			n++
		}
	}
	return n, nil
}

// ---------------------------------------------------------------- address

type AddressInput struct {
	AddressID string `json:"address_id"`
}

// SetAddress changes where future orders go. Orders already made keep the
// address they were made with.
func (s *Service) SetAddress(ctx context.Context, p *identity.Principal, id string, in AddressInput) (*SubscriptionView, error) {
	return s.manage(ctx, p, id, func(tx pgx.Tx, sub *subscriptions.Subscription) error {
		if !subscriptions.Status(sub.Status).Live() {
			return notAllowed("change the address", subscriptions.Status(sub.Status))
		}
		addr, err := addresses.Lookup(ctx, tx, s.Delivery, sub.CustomerID, strings.ToLower(strings.TrimSpace(in.AddressID)))
		if errors.Is(err, httpx.ErrNotFound) {
			return httpx.ValidationError([]httpx.FieldError{{Field: "address_id", Code: "required", Message: "Choose one of your addresses."}})
		}
		if err != nil {
			return err
		}
		if addr.DeliveryAvailability != delivery.Available {
			return httpx.ValidationError([]httpx.FieldError{{Field: "address_id", Code: "not_served",
				Message: "Mise does not deliver to " + addr.RegionLabel + " yet."}})
		}
		if err := s.Subs.SetAddress(ctx, tx, sub, addr.ID); err != nil {
			return err
		}
		return s.Subs.Note(ctx, tx, sub, subscriptions.Event{Type: "address_changed", Actor: p.UserID})
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
