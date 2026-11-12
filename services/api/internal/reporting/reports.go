package reporting

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/platform/httpx"
)

// Report is one report for a date range: series bucketed by day (ranges of
// up to 62 days) or by week, totals, and breakdowns, each with a definition.
type Report struct {
	Kind        string             `json:"kind"`
	From        string             `json:"from"`
	To          string             `json:"to"`
	Interval    string             `json:"interval"`
	TimeZone    string             `json:"time_zone"`
	Totals      []Metric           `json:"totals"`
	Series      []Series           `json:"series"`
	Breakdowns  map[string][]Share `json:"breakdowns"`
	Definitions map[string]string  `json:"definitions"`
}

// ReportKinds are the reports available.
var ReportKinds = []string{"customers", "subscriptions", "orders", "finance", "marketing"}

var errRange = httpx.ValidationError([]httpx.FieldError{{Field: "from", Code: "range",
	Message: "Choose a range of 1 day to 2 years, with from before to (YYYY-MM-DD)."}})

// Report builds one report. from and to are local dates, both included.
func (s *Service) Report(ctx context.Context, kind, from, to string) (*Report, error) {
	f, err1 := time.Parse("2006-01-02", from)
	t, err2 := time.Parse("2006-01-02", to)
	if err1 != nil || err2 != nil || t.Before(f) || t.Sub(f) > 731*24*time.Hour {
		return nil, errRange
	}
	r := &Report{Kind: kind, From: from, To: to, TimeZone: TimeZone, Breakdowns: map[string][]Share{}, Definitions: map[string]string{}}
	r.Interval = "week"
	if t.Sub(f) <= 62*24*time.Hour {
		r.Interval = "day"
	}
	qctx, cancel := s.ctx(ctx)
	defer cancel()
	tx, err := s.pool.BeginTx(qctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(qctx) //nolint:errcheck

	// $1 from date, $2 to date (inclusive), $3 interval ('day' or 'week').
	// Bounds as instants: local midnight at the start of from, and after to.
	lo := `(($1::date)::timestamp AT TIME ZONE '` + TimeZone + `')`
	hi := `((($2::date) + 1)::timestamp AT TIME ZONE '` + TimeZone + `')`
	buckets := `SELECT g::date FROM generate_series(date_trunc($3, $1::date::timestamp), $2::date::timestamp, ('1 ' || $3)::interval) g`
	local := func(col string) string { return `date_trunc($3, ` + col + ` AT TIME ZONE '` + TimeZone + `')::date` }

	series := func(key, label, kind, def, join string) error {
		ser := Series{Key: key, Label: label, Kind: kind, Definition: def, Points: []Point{}}
		rows, err := tx.Query(qctx, `SELECT b.g, `+join, from, to, r.Interval)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var day time.Time
			var v int64
			if err := rows.Scan(&day, &v); err != nil {
				return err
			}
			ser.Points = append(ser.Points, Point{Period: day.Format("2006-01-02"), Value: v})
		}
		r.Series = append(r.Series, ser)
		r.Definitions[key] = def
		return rows.Err()
	}
	total := func(key, label, kind, def, sql string, args ...any) error {
		var v int64
		if err := tx.QueryRow(qctx, sql, args...).Scan(&v); err != nil {
			return err
		}
		m := Metric{Key: key, Label: label, Value: v, Kind: kind, Definition: def}
		if kind == "money" {
			m.Currency = "TTD"
		}
		r.Totals = append(r.Totals, m)
		r.Definitions[key] = def
		return nil
	}
	breakdown := func(key, def, sql string, args ...any) error {
		rows, err := tx.Query(qctx, sql, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		out := []Share{}
		for rows.Next() {
			var sh Share
			if err := rows.Scan(&sh.Label, &sh.Value); err != nil {
				return err
			}
			out = append(out, sh)
		}
		r.Breakdowns[key] = out
		r.Definitions[key] = def
		return rows.Err()
	}
	rng := []any{from, to}
	inRange := func(col string) string { return col + " >= " + lo + " AND " + col + " < " + hi }

	var steps []error
	add := func(err error) { steps = append(steps, err) }
	switch kind {
	case "customers":
		add(series("registrations", "New registrations", "count", "Customer accounts created, by local day or week.",
			`count(c.id) FROM (`+buckets+`) b(g) LEFT JOIN customers c ON `+local("c.created_at")+` = b.g AND `+inRange("c.created_at")+` GROUP BY b.g ORDER BY b.g`))
		add(total("registrations", "New registrations", "count", "Customer accounts created in the range.",
			`SELECT count(*) FROM customers c WHERE `+inRange("c.created_at"), rng...))
		add(total("active_customers", "Active customers (now)", "count", "Customers with a live subscription today.",
			`SELECT count(DISTINCT customer_id) FROM subscriptions WHERE status IN ('active', 'pause_scheduled', 'paused', 'past_due', 'cancel_scheduled')`))
		add(total("lead_conversions", "Lead conversions", "count", "Waitlist leads linked to a customer account in the range.",
			`SELECT count(*) FROM customers c WHERE `+inRange("c.lead_linked_at"), rng...))
		add(breakdown("by_source", "Customers created in the range, by the first-touch source of their waitlist lead ('direct' when none).",
			`SELECT coalesce(la.utm_source, 'direct'), count(*) FROM customers c LEFT JOIN lead_attribution la ON la.lead_id = c.lead_id AND la.touch = 'first'
			 WHERE `+inRange("c.created_at")+` GROUP BY 1 ORDER BY 2 DESC, 1`, rng...))
	case "subscriptions":
		add(series("new", "New subscriptions", "count", "Subscriptions first activated, by local day or week.",
			`count(s.id) FROM (`+buckets+`) b(g) LEFT JOIN subscriptions s ON s.activated_at IS NOT NULL AND `+local("s.activated_at")+` = b.g AND `+inRange("s.activated_at")+` GROUP BY b.g ORDER BY b.g`))
		add(series("cancellations", "Cancellations", "count", "Subscriptions that reached cancelled, by local day or week.",
			`count(s.id) FROM (`+buckets+`) b(g) LEFT JOIN subscriptions s ON s.cancelled_at IS NOT NULL AND `+local("s.cancelled_at")+` = b.g AND `+inRange("s.cancelled_at")+` GROUP BY b.g ORDER BY b.g`))
		add(total("new", "New subscriptions", "count", "Subscriptions first activated in the range.",
			`SELECT count(*) FROM subscriptions s WHERE `+inRange("s.activated_at"), rng...))
		add(total("cancellations", "Cancellations", "count", "Subscriptions that reached cancelled in the range.",
			`SELECT count(*) FROM subscriptions s WHERE `+inRange("s.cancelled_at"), rng...))
		add(total("pauses", "Pauses requested", "count", "Pause requests made in the range (each pause counted once).",
			`SELECT count(*) FROM subscription_pauses p WHERE `+inRange("p.created_at"), rng...))
		add(total("active_now", "Active now", "count", "Subscriptions active, past due, or with a pause or cancellation scheduled (paused excluded).",
			`SELECT count(*) FROM subscriptions WHERE status IN ('active', 'pause_scheduled', 'past_due', 'cancel_scheduled')`))
		add(breakdown("plans", "Live subscriptions (including paused) by plan, now.",
			`SELECT p.name, count(*) FROM subscriptions s JOIN subscription_plans p ON p.id = s.plan_id
			 WHERE s.status IN ('active', 'pause_scheduled', 'paused', 'past_due', 'cancel_scheduled') GROUP BY p.name ORDER BY 2 DESC, 1`))
		add(breakdown("statuses", "Every subscription by status, now.",
			`SELECT status, count(*) FROM subscriptions GROUP BY status ORDER BY 2 DESC, 1`))
	case "orders":
		add(series("orders", "Orders", "count", "Orders by delivery week (or day of the delivery week's Monday).",
			`count(o.id) FROM (`+buckets+`) b(g) LEFT JOIN orders o ON date_trunc($3, o.week_start::timestamp)::date = b.g
			 AND o.week_start BETWEEN $1::date AND $2::date AND o.status <> 'cancelled' GROUP BY b.g ORDER BY b.g`))
		add(total("orders", "Orders", "count", "Orders for delivery weeks starting in the range.",
			`SELECT count(*) FROM orders WHERE week_start BETWEEN $1::date AND $2::date AND status <> 'cancelled'`, rng...))
		add(total("kits", "Meal kits", "count", "Meal kits in those orders.",
			`SELECT coalesce(sum(meals_count), 0) FROM orders WHERE week_start BETWEEN $1::date AND $2::date AND status <> 'cancelled'`, rng...))
		add(breakdown("by_plan", "Orders in the range by plan.",
			`SELECT plan_name, count(*) FROM orders WHERE week_start BETWEEN $1::date AND $2::date GROUP BY plan_name ORDER BY 2 DESC, 1`, rng...))
		add(breakdown("by_status", "Orders in the range by status.",
			`SELECT status, count(*) FROM orders WHERE week_start BETWEEN $1::date AND $2::date GROUP BY status ORDER BY 2 DESC, 1`, rng...))
		add(breakdown("upcoming_demand", "Confirmed meal choices for paid weeks whose selection is still open: provisional demand, not orders.",
			`SELECT m.name, sum(i.quantity)::bigint FROM subscription_cycles sc
			 JOIN billing_cycles b ON b.subscription_cycle_id = sc.id AND b.status = 'paid'
			 JOIN meal_selections ms ON ms.subscription_cycle_id = sc.id AND ms.status = 'confirmed'
			 JOIN meal_selection_items i ON i.selection_id = ms.id JOIN meals m ON m.id = i.meal_id
			 WHERE sc.state = 'scheduled' AND sc.cutoff_at > now() GROUP BY m.name ORDER BY 2 DESC, 1`))
	case "finance":
		add(series("collected", "Collected", "money", "Successful payments before refunds, by local day or week. Gross receipts, not profit.",
			`coalesce(sum(p.amount_minor), 0) FROM (`+buckets+`) b(g) LEFT JOIN payments p ON p.succeeded_at IS NOT NULL AND `+local("p.succeeded_at")+` = b.g AND `+inRange("p.succeeded_at")+` GROUP BY b.g ORDER BY b.g`))
		add(series("invoiced", "Invoiced", "money", "Invoices issued, void excluded, by local day or week.",
			`coalesce(sum(i.total_minor), 0) FROM (`+buckets+`) b(g) LEFT JOIN invoices i ON i.status <> 'void' AND `+local("i.issued_at")+` = b.g AND `+inRange("i.issued_at")+` GROUP BY b.g ORDER BY b.g`))
		add(total("invoiced", "Invoiced", "money", "Totals of invoices issued in the range, void excluded. Billed, not necessarily paid.",
			`SELECT coalesce(sum(total_minor), 0) FROM invoices WHERE status <> 'void' AND `+inRange("issued_at"), rng...))
		add(total("collected", "Collected", "money", "Successful payment amounts in the range, before refunds.",
			`SELECT coalesce(sum(amount_minor), 0) FROM payments WHERE `+inRange("succeeded_at"), rng...))
		add(total("refunded", "Refunded", "money", "Successful refunds completed in the range.",
			`SELECT coalesce(sum(amount_minor), 0) FROM refunds WHERE status = 'succeeded' AND `+inRange("completed_at"), rng...))
		add(total("successful_payments", "Successful payments", "count", "Payments that succeeded in the range.",
			`SELECT count(*) FROM payments WHERE `+inRange("succeeded_at"), rng...))
		add(total("failed_payments", "Failed payments", "count", "Payment attempts that failed, by when they were last updated in the range.",
			`SELECT count(*) FROM payments WHERE status = 'failed' AND `+inRange("updated_at"), rng...))
		add(breakdown("by_kind", "Collected in the range by how it was paid (minor units).",
			`SELECT CASE kind WHEN 'checkout' THEN 'Checkout' ELSE 'Automatic renewal' END, coalesce(sum(amount_minor), 0)::bigint
			 FROM payments WHERE `+inRange("succeeded_at")+` GROUP BY 1 ORDER BY 2 DESC`, rng...))
	case "marketing":
		add(series("leads", "New leads", "count", "Waitlist signups, by local day or week.",
			`count(l.id) FROM (`+buckets+`) b(g) LEFT JOIN leads l ON `+local("l.created_at")+` = b.g AND `+inRange("l.created_at")+` GROUP BY b.g ORDER BY b.g`))
		add(total("leads", "New leads", "count", "Waitlist signups in the range.",
			`SELECT count(*) FROM leads WHERE `+inRange("created_at"), rng...))
		add(total("verified_leads", "Verified leads", "count", "Leads from the range who have confirmed their email.",
			`SELECT count(*) FROM leads WHERE email_verified_at IS NOT NULL AND `+inRange("created_at"), rng...))
		add(total("referral_conversions", "Referral conversions", "count", "Referrals that converted (the friend confirmed) in the range.",
			`SELECT count(*) FROM referrals WHERE status = 'converted' AND `+inRange("converted_at"), rng...))
		add(breakdown("leads_by_source", "Leads from the range by first-touch utm_source ('direct' when none).",
			`SELECT coalesce(la.utm_source, 'direct'), count(*) FROM leads l LEFT JOIN lead_attribution la ON la.lead_id = l.id AND la.touch = 'first'
			 WHERE `+inRange("l.created_at")+` GROUP BY 1 ORDER BY 2 DESC, 1`, rng...))
		add(breakdown("leads_by_campaign", "Leads from the range by first-touch utm_campaign (none when blank).",
			`SELECT coalesce(la.utm_campaign, 'none'), count(*) FROM leads l LEFT JOIN lead_attribution la ON la.lead_id = l.id AND la.touch = 'first'
			 WHERE `+inRange("l.created_at")+` GROUP BY 1 ORDER BY 2 DESC, 1`, rng...))
	default:
		return nil, httpx.NewError(http.StatusNotFound, "not_found", "Unknown report.")
	}
	for _, err := range steps {
		if err != nil {
			return nil, err
		}
	}
	return r, nil
}
