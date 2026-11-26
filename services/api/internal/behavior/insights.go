package behavior

import (
	"context"
	"time"

	"mise.tt/api/internal/platform/httpx"
)

// AbandonAfter is the inactivity rule: a checkout started with no
// successful payment by that customer within this time is abandoned.
const AbandonAfter = 24 * time.Hour

// Step is one funnel step: distinct people (signed-in customers or
// anonymous visitors) who did it in the period.
type Step struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	People int64  `json:"people"`
	Events int64  `json:"events"`
	Source string `json:"source"`
}

// Funnel is the customer-app conversion funnel for a period.
type Funnel struct {
	From             string     `json:"from"`
	To               string     `json:"to"`
	Steps            []Step     `json:"steps"`
	CheckoutsStarted int64      `json:"checkouts_started"`
	Abandoned        int64      `json:"checkouts_abandoned"`
	Pending          int64      `json:"checkouts_pending"`
	AbandonRule      string     `json:"abandon_rule"`
	TestPayments     bool       `json:"includes_test_payments"`
	Limitations      []string   `json:"limitations"`
	FirstEventAt     *time.Time `json:"first_event_at"`
}

func period(from, to string) (time.Time, time.Time, error) {
	loc, _ := time.LoadLocation("America/Port_of_Spain")
	f, e1 := time.ParseInLocation("2006-01-02", from, loc)
	t, e2 := time.ParseInLocation("2006-01-02", to, loc)
	if e1 != nil || e2 != nil || t.Before(f) || t.Sub(f) > 731*24*time.Hour {
		return f, t, httpx.ValidationError([]httpx.FieldError{{Field: "from", Code: "invalid", Message: "Give from and to dates (YYYY-MM-DD), at most two years."}})
	}
	return f, t.AddDate(0, 0, 1), nil
}

// Funnel counts each step by distinct actor. Payments come from the
// payment ledger (not from a behaviour event), so they cannot be inflated
// by the browser.
func (s *Service) Funnel(ctx context.Context, fromS, toS string) (*Funnel, error) {
	from, to, err := period(fromS, toS)
	if err != nil {
		return nil, err
	}
	f := &Funnel{From: fromS, To: toS, Steps: []Step{}, AbandonRule: "A checkout started with no successful payment by the same customer within 24 hours.",
		Limitations: []string{
			"Visitors are counted per browser (anonymous id); one person on two devices counts twice, and a visitor who signs in is counted again as a customer.",
			"Nothing is recorded for visitors whose browser sends Global Privacy Control or Do Not Track.",
		}}
	steps := []struct{ key, label string }{
		{"catalogue_viewed", "Viewed the meal catalogue"}, {"meal_viewed", "Viewed a meal"}, {"plan_viewed", "Viewed a plan"},
		{"plan_selected", "Chose a plan"}, {"checkout_started", "Started checkout"},
	}
	for _, st := range steps {
		var people, events int64
		if err := s.pool.QueryRow(ctx, `SELECT count(DISTINCT coalesce(customer_id::text, 'a:' || anonymous_id)), count(*) FROM behavior_events
			WHERE name = $1 AND occurred_at >= $2 AND occurred_at < $3`, st.key, from, to).Scan(&people, &events); err != nil {
			return nil, err
		}
		f.Steps = append(f.Steps, Step{Key: st.key, Label: st.label, People: people, Events: events, Source: "behavior_events"})
	}
	var paid, paidEvents int64
	var test bool
	if err := s.pool.QueryRow(ctx, `SELECT count(DISTINCT customer_id), count(*), coalesce(bool_or(provider = 'testpay'), false) FROM payments
		WHERE kind = 'checkout' AND succeeded_at >= $1 AND succeeded_at < $2`, from, to).Scan(&paid, &paidEvents, &test); err != nil {
		return nil, err
	}
	f.TestPayments = test
	f.Steps = append(f.Steps, Step{Key: "payment_completed", Label: "Paid", People: paid, Events: paidEvents, Source: "payments (succeeded checkout payments)"})
	if test {
		f.Limitations = append(f.Limitations, "Payments in this period include test-provider payments (no real money).")
	}
	// Abandonment: only checkouts old enough to judge.
	cut := s.Now().Add(-AbandonAfter)
	if err := s.pool.QueryRow(ctx, `SELECT count(*),
		count(*) FILTER (WHERE e.occurred_at <= $3 AND NOT EXISTS (SELECT 1 FROM payments p WHERE p.customer_id = e.customer_id
			AND p.succeeded_at >= e.occurred_at AND p.succeeded_at < e.occurred_at + interval '24 hours')),
		count(*) FILTER (WHERE e.occurred_at > $3)
		FROM behavior_events e WHERE e.name = 'checkout_started' AND e.customer_id IS NOT NULL AND e.occurred_at >= $1 AND e.occurred_at < $2`,
		from, to, cut).Scan(&f.CheckoutsStarted, &f.Abandoned, &f.Pending); err != nil {
		return nil, err
	}
	if err := s.pool.QueryRow(ctx, `SELECT min(occurred_at) FROM behavior_events`).Scan(&f.FirstEventAt); err != nil {
		return nil, err
	}
	if f.FirstEventAt == nil || f.FirstEventAt.After(from) {
		f.Limitations = append(f.Limitations, "Behaviour events were not recorded for the whole period (recording started with Sprint 9.2).")
	}
	return f, nil
}

// Reason is a recorded cancellation reason and how many cancellations gave it.
type Reason struct {
	Reason string `json:"reason"`
	Count  int64  `json:"count"`
}

// CancellationReasons counts the reasons customers chose. Cancellations
// with no reason are counted as "not recorded": no reason is inferred.
func (s *Service) CancellationReasons(ctx context.Context, fromS, toS string) ([]Reason, int64, error) {
	from, to, err := period(fromS, toS)
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT coalesce(nullif(cancel_reason, ''), 'not recorded'), count(*) FROM subscriptions
		WHERE cancel_requested_at >= $1 AND cancel_requested_at < $2 GROUP BY 1 ORDER BY 2 DESC, 1`, from, to)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Reason{}
	var total int64
	for rows.Next() {
		var r Reason
		if err := rows.Scan(&r.Reason, &r.Count); err != nil {
			return nil, 0, err
		}
		total += r.Count
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// Coverage describes how much behaviour data exists (for readiness).
type Coverage struct {
	Events       int64            `json:"events"`
	FirstEventAt *time.Time       `json:"first_event_at"`
	DaysCovered  int              `json:"days_covered"`
	ByName       map[string]int64 `json:"by_name"`
}

func (s *Service) Coverage(ctx context.Context) (*Coverage, error) {
	c := &Coverage{ByName: map[string]int64{}}
	if err := s.pool.QueryRow(ctx, `SELECT count(*), min(occurred_at), count(DISTINCT occurred_at::date) FROM behavior_events`).
		Scan(&c.Events, &c.FirstEventAt, &c.DaysCovered); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT name, count(*) FROM behavior_events GROUP BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		var k int64
		if err := rows.Scan(&n, &k); err != nil {
			return nil, err
		}
		c.ByName[n] = k
	}
	return c, rows.Err()
}
