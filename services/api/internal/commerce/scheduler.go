package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/addresses"
	"mise.tt/api/internal/billing"
	"mise.tt/api/internal/customers"
	"mise.tt/api/internal/menus"
	"mise.tt/api/internal/orders"
	"mise.tt/api/internal/payments"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/jobs"
	"mise.tt/api/internal/subscriptions"
)

// JobCharge charges a saved payment method for a renewal.
const JobCharge = "commerce.charge"

// schedulerLock is the advisory lock key that keeps one tick at a time
// across every worker.
const schedulerLock = 0x4d495345_0003 // "MISE", sprint 3

// TickReport says what one scheduler run did.
type TickReport struct {
	MenusPublished   int  `json:"menus_published"`
	MenusClosed      int  `json:"menus_closed"`
	CheckoutsExpired int  `json:"checkouts_expired"`
	PendingExpired   int  `json:"pending_expired"`
	CyclesCreated    int  `json:"cycles_created"`
	Renewals         int  `json:"renewals"`
	Retries          int  `json:"retries"`
	Finalized        int  `json:"finalized"`
	OrdersCreated    int  `json:"orders_created"`
	NeedsResolution  int  `json:"needs_resolution"`
	Unpaid           int  `json:"unpaid"`
	Reminders        int  `json:"reminders"`
	Skipped          bool `json:"skipped"`
}

// Tick runs every scheduled step once, as of now. Each step is idempotent
// (unique keys, state checks under row locks), so running it twice, or on
// two workers, does the work once. Steps that fail are logged and retried on
// the next tick; one bad record does not stop the others.
func (s *Service) Tick(ctx context.Context, now time.Time) (*TickReport, error) {
	r := &TickReport{}
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	var got bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, int64(schedulerLock)).Scan(&got); err != nil {
		return nil, err
	}
	if !got {
		r.Skipped = true
		return r, nil
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, int64(schedulerLock))
	}()

	logErr := func(step string, err error) {
		if err != nil {
			slog.ErrorContext(ctx, "scheduler step failed", "step", step, "error", err.Error())
		}
	}

	r.MenusPublished, r.MenusClosed, err = s.Menus.PublishDue(ctx, now)
	logErr("menus", err)
	r.CheckoutsExpired, err = s.Payments.ExpireCheckouts(ctx, now)
	logErr("checkouts", err)
	logErr("pending", s.expireStale(ctx, now, r))
	logErr("cycles", s.createCycles(ctx, now, r))
	logErr("renewals", s.renewals(ctx, now, r))
	logErr("retries", s.retries(ctx, now, r))
	logErr("cutoffs", s.cutoffs(ctx, now, r))
	logErr("reminders", s.reminders(ctx, now, r))
	return r, nil
}

// each runs fn for every id in its own transaction, logging failures.
func (s *Service) each(ctx context.Context, step string, ids []string, fn func(tx pgx.Tx, id string) error) {
	for _, id := range ids {
		if err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error { return fn(tx, id) }); err != nil {
			slog.ErrorContext(ctx, "scheduler item failed", "step", step, "id", id, "error", err.Error())
		}
	}
}

func (s *Service) expireStale(ctx context.Context, now time.Time, r *TickReport) error {
	ids, err := s.Subs.StalePending(ctx, s.pool, now.Add(-s.Opts.PendingTTL))
	if err != nil {
		return err
	}
	s.each(ctx, "pending", ids, func(tx pgx.Tx, id string) error {
		sub, err := s.Subs.Get(ctx, tx, id, true)
		if err != nil || subscriptions.Status(sub.Status) != subscriptions.PendingPayment {
			return err
		}
		r.PendingExpired++
		return s.expirePending(ctx, tx, sub, "", "payment_not_completed")
	})
	return nil
}

// createCycles makes sure every delivering subscription has its next open
// week, applies pauses that start or end, and completes cancellations
// whose last paid week is done.
func (s *Service) createCycles(ctx context.Context, now time.Time, r *TickReport) error {
	ids, err := s.Subs.Delivering(ctx, s.pool)
	if err != nil {
		return err
	}
	week := s.Menus.Policy.NextOpenWeek(now)
	s.each(ctx, "cycles", ids, func(tx pgx.Tx, id string) error {
		sub, err := s.Subs.Get(ctx, tx, id, true)
		if err != nil {
			return err
		}
		st := subscriptions.Status(sub.Status)
		if !st.Delivering() {
			return nil
		}
		if st == subscriptions.CancelScheduled {
			pending, err := s.paidPending(ctx, tx, sub)
			if err != nil {
				return err
			}
			if pending == 0 {
				return s.Subs.Transition(ctx, tx, sub, subscriptions.Cancelled, subscriptions.Event{Type: "cancelled", EffectiveWeek: sub.CancelEffectiveWeek})
			}
			return nil
		}
		if sub.CancelEffectiveWeek != nil && !week.Before(*sub.CancelEffectiveWeek) {
			return nil
		}
		// A pause that has run its course ends; one that has reached its
		// first week starts.
		if sub.PauseUntilWeek != nil && !week.Before(*sub.PauseUntilWeek) {
			if err := s.Subs.SetPause(ctx, tx, sub, nil, nil, "", "ended"); err != nil {
				return err
			}
			if st == subscriptions.Paused || st == subscriptions.PauseScheduled {
				if err := s.Subs.Transition(ctx, tx, sub, subscriptions.Active, subscriptions.Event{Type: "pause_ended", EffectiveWeek: &week}); err != nil {
					return err
				}
				if err := s.notify(ctx, tx, "resumed:"+sub.ID+":"+menus.FormatWeek(week), EmailResumed, sub.CustomerID, map[string]string{
					"week": weekLabel(week), "link": s.link("/menu"),
				}); err != nil {
					return err
				}
			}
		} else if st == subscriptions.PauseScheduled && subscriptions.InPause(sub, week) {
			if err := s.Subs.ActivatePause(ctx, tx, sub); err != nil {
				return err
			}
			if err := s.Subs.Transition(ctx, tx, sub, subscriptions.Paused, subscriptions.Event{Type: "paused", EffectiveWeek: &week}); err != nil {
				return err
			}
		}
		_, created, err := s.Subs.EnsureCycle(ctx, tx, sub, week)
		if created {
			r.CyclesCreated++
		}
		return err
	})
	return nil
}

// renewals bills each week that has reached its renewal time: a billing
// cycle and an invoice, then an automatic charge if the provider and the
// customer's saved method allow one, otherwise a "payment due" email.
func (s *Service) renewals(ctx context.Context, now time.Time, r *TickReport) error {
	ids, err := s.Subs.CyclesDueForRenewal(ctx, s.pool, now)
	if err != nil {
		return err
	}
	s.each(ctx, "renewal", ids, func(tx pgx.Tx, id string) error {
		c, err := s.Subs.Cycle(ctx, tx, id, true)
		if err != nil || c.State != "scheduled" {
			return err
		}
		if existing, err := s.Billing.ForSubscriptionCycle(ctx, tx, c.ID); err != nil || existing != nil {
			return err
		}
		sub, err := s.Subs.Get(ctx, tx, c.SubscriptionID, true)
		if err != nil {
			return err
		}
		if !subscriptions.Status(sub.Status).Delivering() || subscriptions.Status(sub.Status) == subscriptions.CancelScheduled {
			return nil
		}
		bc, created, err := s.Billing.CreateCycle(ctx, tx, billing.NewCycle{
			SubscriptionID: sub.ID, SubscriptionCycleID: c.ID, CustomerID: sub.CustomerID, Kind: "renewal", Week: c.WeekStart,
			DueAt: c.CutoffAt, SubtotalMinor: c.AmountMinor, Currency: c.Currency, Status: "due",
		})
		if err != nil || !created {
			return err
		}
		r.Renewals++
		if bc.TotalMinor == 0 {
			return s.paid(ctx, tx, payments.Outcome{BillingCycleID: bc.ID})
		}
		inv, err := s.Billing.IssueFor(ctx, tx, bc.ID, s.contactSnapshot(ctx, tx, sub))
		if err != nil {
			return err
		}
		if sub.PaymentMethodID != nil && s.Payments.CanChargeMethod(ctx, *sub.PaymentMethodID) {
			return jobs.Enqueue(ctx, tx, JobCharge, ChargeJob{BillingCycleID: bc.ID, Attempt: 1})
		}
		return s.notify(ctx, tx, "payment_due:"+bc.ID, EmailPaymentDue, sub.CustomerID, map[string]string{
			"week": weekLabel(c.WeekStart), "amount": formatMoney(bc.TotalMinor, bc.Currency), "invoice": inv.Number,
			"cutoff": localTime(c.CutoffAt), "link": s.link("/subscription"),
		})
	})
	return nil
}

// retries queues another charge for failed renewals whose retry time has
// come (only when BILLING_RENEWAL_RETRIES allows any).
func (s *Service) retries(ctx context.Context, now time.Time, r *TickReport) error {
	ids, err := s.Billing.RetryDue(ctx, s.pool, now)
	if err != nil {
		return err
	}
	s.each(ctx, "retry", ids, func(tx pgx.Tx, id string) error {
		bc, err := s.Billing.Get(ctx, tx, id, true)
		if err != nil || bc.Status != "failed" || bc.NextAttemptAt == nil {
			return err
		}
		if err := s.Billing.ClearRetry(ctx, tx, id); err != nil {
			return err
		}
		r.Retries++
		return jobs.Enqueue(ctx, tx, JobCharge, ChargeJob{BillingCycleID: id, Attempt: bc.Attempts + 1})
	})
	return nil
}

// ChargeJob is the payload of JobCharge.
type ChargeJob struct {
	BillingCycleID string `json:"billing_cycle_id"`
	Attempt        int    `json:"attempt"`
}

// HandleCharge is the JobCharge handler. A provider error returns an error,
// so the job is retried with the same idempotency key (never a second charge).
func (s *Service) HandleCharge(ctx context.Context, payload json.RawMessage) error {
	var job ChargeJob
	if err := json.Unmarshal(payload, &job); err != nil {
		return err
	}
	bc, err := s.Billing.Get(ctx, s.pool, job.BillingCycleID, false)
	if err != nil {
		return err
	}
	if !bc.Payable() {
		return nil
	}
	sub, err := s.Subs.Get(ctx, s.pool, bc.SubscriptionID, false)
	if err != nil {
		return err
	}
	cycle, err := s.Subs.Cycle(ctx, s.pool, bc.SubscriptionCycleID, false)
	if err != nil {
		return err
	}
	if cycle.State != "scheduled" || !s.Now().Before(cycle.CutoffAt) || sub.PaymentMethodID == nil {
		return nil
	}
	err = s.Payments.Charge(ctx, payments.ChargeInput{
		CustomerID: sub.CustomerID, BillingCycleID: bc.ID, PaymentMethodID: *sub.PaymentMethodID,
		AmountMinor: bc.TotalMinor, Currency: bc.Currency, Description: "Mise subscription, week of " + bc.PeriodStart,
		Attempt: job.Attempt,
	})
	if errors.Is(err, payments.ErrCannotCharge) {
		return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
			return s.notify(ctx, tx, "payment_due:"+bc.ID, EmailPaymentDue, sub.CustomerID, map[string]string{
				"week": weekLabel(cycle.WeekStart), "amount": formatMoney(bc.TotalMinor, bc.Currency),
				"cutoff": localTime(cycle.CutoffAt), "link": s.link("/subscription"),
			})
		})
	}
	return err
}

// cutoffs closes every week whose selection deadline has passed:
//   - paid, with confirmed meals and a published menu: an order is made
//   - paid, but no confirmed meals or no menu: needs_resolution (no meals
//     are invented; staff decide)
//   - not paid: unpaid (billing voided, no order)
func (s *Service) cutoffs(ctx context.Context, now time.Time, r *TickReport) error {
	ids, err := s.Subs.CyclesPastCutoff(ctx, s.pool, now)
	if err != nil {
		return err
	}
	s.each(ctx, "cutoff", ids, func(tx pgx.Tx, id string) error {
		state, created, err := s.finalize(ctx, tx, id, now)
		if err != nil {
			return err
		}
		switch state {
		case "finalized":
			r.Finalized++
			if created {
				r.OrdersCreated++
			}
		case "needs_resolution":
			r.NeedsResolution++
		case "unpaid":
			r.Unpaid++
		}
		return nil
	})
	return nil
}

// SettleCycles runs the cutoff step for the given cycles only, as of now.
// The scheduler settles every cycle; the local production demo seed uses
// this so that a moved clock touches nothing but its own demo customers.
func (s *Service) SettleCycles(ctx context.Context, ids []string, now time.Time) (*TickReport, error) {
	r := &TickReport{}
	for _, id := range ids {
		err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
			state, created, err := s.finalize(ctx, tx, id, now)
			if err != nil {
				return err
			}
			switch state {
			case "finalized":
				r.Finalized++
				if created {
					r.OrdersCreated++
				}
			case "needs_resolution":
				r.NeedsResolution++
			case "unpaid":
				r.Unpaid++
			}
			return nil
		})
		if err != nil {
			return r, err
		}
	}
	return r, nil
}

// finalize settles one week at its cutoff. It returns the new state and
// whether an order was created. Idempotent: a week already settled is left.
func (s *Service) finalize(ctx context.Context, tx pgx.Tx, cycleID string, now time.Time) (string, bool, error) {
	c, err := s.Subs.Cycle(ctx, tx, cycleID, true)
	if err != nil {
		return "", false, err
	}
	if c.State != "scheduled" || now.Before(c.CutoffAt) {
		return c.State, false, nil
	}
	sub, err := s.Subs.Get(ctx, tx, c.SubscriptionID, true)
	if err != nil {
		return "", false, err
	}
	sel, err := s.Subs.Lock(ctx, tx, c.ID)
	if err != nil {
		return "", false, err
	}
	bc, err := s.Billing.ForSubscriptionCycle(ctx, tx, c.ID)
	if err != nil {
		return "", false, err
	}
	resolve := func(reason string) (string, bool, error) {
		_, err := s.Subs.SetCycleState(ctx, tx, c.ID, "needs_resolution", reason)
		return "needs_resolution", false, err
	}
	if bc == nil || bc.Status != "paid" {
		if bc != nil {
			if err := s.Billing.Void(ctx, tx, bc.ID, "unpaid_at_cutoff"); err != nil {
				return "", false, err
			}
			if err := s.Payments.CancelOpenCheckouts(ctx, tx, bc.ID); err != nil {
				return "", false, err
			}
		}
		if _, err := s.Subs.SetCycleState(ctx, tx, c.ID, "unpaid", "unpaid_at_cutoff"); err != nil {
			return "", false, err
		}
		st := subscriptions.Status(sub.Status)
		if st == subscriptions.PendingPayment {
			// A first week that was never paid: the subscription ends.
			if err := s.expirePending(ctx, tx, sub, "", "first_week_unpaid"); err != nil {
				return "", false, err
			}
		} else if st == subscriptions.Active || st == subscriptions.PauseScheduled {
			if err := s.Subs.Transition(ctx, tx, sub, subscriptions.PastDue, subscriptions.Event{Type: "week_unpaid", EffectiveWeek: &c.WeekStart}); err != nil {
				return "", false, err
			}
		}
		return "unpaid", false, nil
	}
	menu, err := s.Menus.ForWeek(ctx, tx, c.WeekStart)
	if err != nil {
		return "", false, err
	}
	if menu == nil || (menu.Status != "published" && menu.Status != "closed") {
		return resolve("no_menu")
	}
	if sel == nil || sel.WasConfirmed == nil || !*sel.WasConfirmed || sel.Count != int(c.MealsPerWeek) {
		return resolve("no_confirmed_selection")
	}
	if sub.DeliveryAddressID == nil {
		return resolve("no_address")
	}
	addr, err := addresses.Lookup(ctx, tx, s.Delivery, sub.CustomerID, *sub.DeliveryAddressID)
	if err != nil {
		return resolve("no_address")
	}
	ids := make([]string, len(sel.Items))
	for i, it := range sel.Items {
		ids[i] = it.MealID
	}
	refs, err := s.Catalog.MealRefs(ctx, tx, ids)
	if err != nil {
		return "", false, err
	}
	var items []orders.ItemInput
	for _, it := range sel.Items {
		ref, ok := refs[it.MealID]
		if !ok {
			return resolve("meal_missing")
		}
		items = append(items, orders.ItemInput{MealID: ref.ID, MealVersion: ref.Version, MealName: ref.Name, MealSlug: ref.Slug,
			Quantity: it.Quantity, Servings: ref.Servings, Allergens: ref.Allergens, AllergenStatus: ref.AllergenStatus,
			DietaryTags: ref.DietaryTags})
	}
	prefs, err := customers.PreferencesOf(ctx, tx, sub.CustomerID)
	if err != nil {
		return "", false, err
	}
	allergens := []string{}
	for _, a := range prefs.Allergies {
		allergens = append(allergens, a.Allergen)
	}
	paymentID := ""
	if p, err := s.Payments.SucceededFor(ctx, tx, bc.ID); err != nil {
		return "", false, err
	} else if p != nil {
		paymentID = p.ID
	}
	planName, err := s.Subs.PlanNameForCycle(ctx, tx, c.ID)
	if err != nil {
		return "", false, err
	}
	_, created, err := s.Orders.Create(ctx, tx, orders.NewOrder{
		CustomerID: sub.CustomerID, SubscriptionID: sub.ID, SubscriptionCycleID: c.ID, BillingCycleID: bc.ID, PaymentID: paymentID,
		MenuID: menu.ID, Week: c.WeekStart, PlanName: planName, Currency: bc.Currency, SubtotalMinor: bc.SubtotalMinor,
		DiscountMinor: bc.DiscountMinor, DeliveryFeeMinor: bc.DeliveryFeeMinor, TaxMinor: bc.TaxMinor, TotalMinor: bc.TotalMinor,
		Address: orderAddress(addr), CustomerAllergens: allergens, Items: items,
	})
	if err != nil {
		return "", false, err
	}
	if _, err := s.Subs.SetCycleState(ctx, tx, c.ID, "finalized", ""); err != nil {
		return "", false, err
	}
	return "finalized", created, nil
}

// reminders sends one "choose your meals" email per week not yet confirmed,
// ReminderLead before the cutoff, when the menu is out.
func (s *Service) reminders(ctx context.Context, now time.Time, r *TickReport) error {
	due, err := s.Subs.NearingCutoff(ctx, s.pool, now, now.Add(s.Opts.ReminderLead))
	if err != nil {
		return err
	}
	for _, d := range due {
		menu, err := s.Menus.ForWeek(ctx, s.pool, d.Week)
		if err != nil || menu == nil || !menu.Published() {
			continue
		}
		err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
			return s.notify(ctx, tx, "reminder:"+d.CycleID, EmailSelectionRemind, d.CustomerID, map[string]string{
				"week": weekLabel(d.Week), "cutoff": localTime(d.CutoffAt), "link": s.link("/menu/" + menus.FormatWeek(d.Week) + "/select"),
			})
		})
		if err != nil {
			slog.ErrorContext(ctx, "reminder failed", "cycle_id", d.CycleID, "error", err.Error())
			continue
		}
		r.Reminders++
	}
	return nil
}

// MenuHooks adapts the service to menus.Hooks.
type MenuHooks struct{ S *Service }

// MenuPublished aligns the cutoff of the week's cycles with the menu's and
// tells the week's subscribers the menu is out.
func (h MenuHooks) MenuPublished(ctx context.Context, q db.Querier, menuID string, week time.Time) error {
	s := h.S
	info, err := s.Menus.ForWeek(ctx, q, week)
	if err != nil || info == nil {
		return err
	}
	if _, err := s.Subs.AlignCutoffs(ctx, q, week, info.CutoffAt); err != nil {
		return err
	}
	list, err := s.Subs.InWeek(ctx, q, week)
	if err != nil {
		return err
	}
	for _, d := range list {
		if err := s.notify(ctx, q, "menu:"+menuID+":"+d.CycleID, EmailMenuAvailable, d.CustomerID, map[string]string{
			"week": weekLabel(week), "cutoff": localTime(info.CutoffAt), "link": s.link("/menu/" + menus.FormatWeek(week) + "/select"),
		}); err != nil {
			return err
		}
	}
	return nil
}

// RunScheduler ticks every interval until ctx ends.
func (s *Service) RunScheduler(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if rep, err := s.Tick(ctx, s.Now()); err != nil {
			slog.ErrorContext(ctx, "scheduler tick failed", "error", err.Error())
		} else if !rep.Skipped && (rep.CyclesCreated+rep.Renewals+rep.Finalized+rep.Unpaid+rep.NeedsResolution+rep.Reminders+rep.PendingExpired) > 0 {
			slog.InfoContext(ctx, "scheduler tick", "report", fmt.Sprintf("%+v", *rep))
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
