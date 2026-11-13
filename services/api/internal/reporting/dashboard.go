package reporting

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// Metric is one figure with its definition, so nobody has to guess what it
// counts.
type Metric struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	Value      int64  `json:"value"`
	Kind       string `json:"kind"` // count or money
	Currency   string `json:"currency,omitempty"`
	Definition string `json:"definition"`
}

// Attention is something staff should act on, with how many and where.
type Attention struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	Count      int64  `json:"count"`
	Severity   string `json:"severity"`
	Link       string `json:"link"`
	Permission string `json:"permission"`
}

// Point is one bucket of a series.
type Point struct {
	Period string `json:"period"` // YYYY-MM-DD, the first day of the bucket
	Value  int64  `json:"value"`
}

type Series struct {
	Key        string  `json:"key"`
	Label      string  `json:"label"`
	Kind       string  `json:"kind"`
	Definition string  `json:"definition"`
	Points     []Point `json:"points"`
}

type Share struct {
	Label string `json:"label"`
	Value int64  `json:"value"`
}

type Dashboard struct {
	GeneratedAt  time.Time   `json:"generated_at"`
	TimeZone     string      `json:"time_zone"`
	PeriodLabel  string      `json:"period_label"`
	PeriodFrom   string      `json:"period_from"`
	PeriodTo     string      `json:"period_to"`
	CurrentWeek  string      `json:"current_week"`
	Metrics      []Metric    `json:"metrics"`
	Attention    []Attention `json:"attention"`
	Charts       []Series    `json:"charts"`
	Plans        []Share     `json:"plan_distribution"`
	NextCutoffAt *time.Time  `json:"next_cutoff_at"`
}

func one(ctx context.Context, q pgx.Tx, sql string, args ...any) (int64, error) {
	var n int64
	err := q.QueryRow(ctx, sql, args...).Scan(&n)
	return n, err
}

// Dashboard is the operational overview. "Period" figures cover the last 30
// days; "now" figures are the current state. Everything runs in one
// read-only transaction, so the figures agree with each other.
func (s *Service) Dashboard(ctx context.Context) (*Dashboard, error) {
	now := s.Now()
	from := now.AddDate(0, 0, -30)
	loc, _ := time.LoadLocation(TimeZone)
	if loc == nil {
		loc = time.FixedZone("AST", -4*3600)
	}
	d := &Dashboard{GeneratedAt: now, TimeZone: TimeZone, PeriodLabel: "Last 30 days",
		PeriodFrom: from.In(loc).Format("2006-01-02"), PeriodTo: now.In(loc).Format("2006-01-02")}
	qctx, cancel := s.ctx(ctx)
	defer cancel()
	tx, err := s.pool.BeginTx(qctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(qctx) //nolint:errcheck

	var currentWeek time.Time
	if err := tx.QueryRow(qctx, `SELECT date_trunc('week', ($1::timestamptz AT TIME ZONE '`+TimeZone+`'))::date`, now).Scan(&currentWeek); err != nil {
		return nil, err
	}
	d.CurrentWeek = currentWeek.Format("2006-01-02")

	type m struct {
		key, label, kind, def, sql string
		args                       []any
	}
	metrics := []m{
		{"customers", "Customers", "count", "Customer accounts with a confirmed email whose login is not disabled.",
			`SELECT count(*) FROM customers c JOIN users u ON u.id = c.user_id WHERE u.status = 'active' AND u.email_verified_at IS NOT NULL`, nil},
		{"active_subscriptions", "Active subscriptions", "count", "Subscriptions active, past due, or with a pause or cancellation scheduled but not yet in effect. Paused ones are not counted.",
			`SELECT count(*) FROM subscriptions WHERE status IN ('active', 'pause_scheduled', 'past_due', 'cancel_scheduled')`, nil},
		{"paused_subscriptions", "Paused", "count", "Subscriptions currently paused.",
			`SELECT count(*) FROM subscriptions WHERE status = 'paused'`, nil},
		{"new_subscriptions", "New subscriptions", "count", "Subscriptions first activated in the period.",
			`SELECT count(*) FROM subscriptions WHERE activated_at >= $1 AND activated_at <= $2`, []any{from, now}},
		{"cancellations", "Cancellations", "count", "Subscriptions that ended as cancelled in the period.",
			`SELECT count(*) FROM subscriptions WHERE cancelled_at >= $1 AND cancelled_at <= $2`, []any{from, now}},
		{"orders_this_week", "Orders this week", "count", "Orders (boxes) for the current delivery week, Monday to Sunday.",
			`SELECT count(*) FROM orders WHERE week_start = $1::date AND status <> 'cancelled'`, []any{currentWeek}},
		{"upcoming_boxes", "Upcoming paid boxes", "count", "Paid weeks whose meal selection is still open: future boxes, not subscriptions.",
			`SELECT count(*) FROM subscription_cycles sc JOIN billing_cycles b ON b.subscription_cycle_id = sc.id
			 WHERE sc.state = 'scheduled' AND sc.cutoff_at > $1 AND b.status = 'paid'`, []any{now}},
		{"successful_payments", "Successful payments", "count", "Payments that succeeded in the period.",
			`SELECT count(*) FROM payments WHERE succeeded_at >= $1 AND succeeded_at <= $2`, []any{from, now}},
		{"failed_payments", "Failed payments", "count", "Payment attempts that failed in the period.",
			`SELECT count(*) FROM payments WHERE status = 'failed' AND updated_at >= $1 AND updated_at <= $2`, []any{from, now}},
		{"invoiced", "Invoiced", "money", "Totals of invoices issued in the period, void invoices excluded. Not cash.",
			`SELECT coalesce(sum(total_minor), 0) FROM invoices WHERE issued_at >= $1 AND issued_at <= $2 AND status <> 'void'`, []any{from, now}},
		{"collected", "Collected", "money", "Successful payment amounts in the period, before refunds. Gross receipts, not profit.",
			`SELECT coalesce(sum(amount_minor), 0) FROM payments WHERE succeeded_at >= $1 AND succeeded_at <= $2`, []any{from, now}},
		{"refunded", "Refunded", "money", "Successful refunds in the period, shown separately from collected.",
			`SELECT coalesce(sum(amount_minor), 0) FROM refunds WHERE status = 'succeeded' AND completed_at >= $1 AND completed_at <= $2`, []any{from, now}},
		{"outstanding", "Outstanding", "money", "Weeks billed but not yet paid (due or failed) whose selection is still open.",
			`SELECT coalesce(sum(b.total_minor), 0) FROM billing_cycles b JOIN subscription_cycles sc ON sc.id = b.subscription_cycle_id
			 WHERE b.status IN ('due', 'failed') AND sc.cutoff_at > $1`, []any{now}},
		{"new_leads", "New leads", "count", "Waitlist signups created in the period.",
			`SELECT count(*) FROM leads WHERE created_at >= $1 AND created_at <= $2`, []any{from, now}},
		{"lead_conversions", "Lead conversions", "count", "Waitlist leads linked to a customer account in the period.",
			`SELECT count(*) FROM customers WHERE lead_linked_at >= $1 AND lead_linked_at <= $2`, []any{from, now}},
	}
	for _, x := range metrics {
		v, err := one(qctx, tx, x.sql, x.args...)
		if err != nil {
			return nil, err
		}
		mt := Metric{Key: x.key, Label: x.label, Value: v, Kind: x.kind, Definition: x.def}
		if x.kind == "money" {
			mt.Currency = "TTD"
		}
		d.Metrics = append(d.Metrics, mt)
	}

	attention := []struct {
		Attention
		sql  string
		args []any
	}{
		{Attention{Key: "failed_renewals", Label: "Unpaid weeks to chase before cutoff", Severity: "critical", Link: "/subscriptions?status=past_due", Permission: "subscriptions.read"},
			`SELECT count(*) FROM billing_cycles b JOIN subscription_cycles sc ON sc.id = b.subscription_cycle_id
			 WHERE b.status = 'failed' AND sc.cutoff_at > $1`, []any{now}},
		{Attention{Key: "payments_review", Label: "Payments needing review (possible refunds)", Severity: "critical", Link: "/payments?attention=true", Permission: "payments.read"},
			`SELECT count(*) FROM payments WHERE needs_attention`, nil},
		{Attention{Key: "needs_resolution", Label: "Paid weeks without confirmed meals", Severity: "warning", Link: "/orders", Permission: "orders.read"},
			`SELECT count(*) FROM subscription_cycles WHERE state = 'needs_resolution' AND week_start >= ($1::timestamptz AT TIME ZONE '` + TimeZone + `')::date - 7`, []any{now}},
		{Attention{Key: "unconfirmed_selections", Label: "Meal choices not confirmed, cutoff within 48 hours", Severity: "warning", Link: "/subscriptions", Permission: "subscriptions.read"},
			`SELECT count(*) FROM subscription_cycles sc LEFT JOIN meal_selections ms ON ms.subscription_cycle_id = sc.id
			 WHERE sc.state = 'scheduled' AND sc.cutoff_at > $1::timestamptz AND sc.cutoff_at <= $1::timestamptz + interval '48 hours'
			   AND (ms.id IS NULL OR ms.status = 'draft')`, []any{now}},
		{Attention{Key: "urgent_cases", Label: "Urgent or high-priority support cases open", Severity: "warning", Link: "/support?open=true", Permission: "cases.read"},
			`SELECT count(*) FROM support_cases WHERE status IN ('open', 'in_progress', 'waiting_for_customer') AND priority IN ('urgent', 'high')`, nil},
		{Attention{Key: "open_cases", Label: "Support cases open", Severity: "info", Link: "/support?open=true", Permission: "cases.read"},
			`SELECT count(*) FROM support_cases WHERE status IN ('open', 'in_progress', 'waiting_for_customer')`, nil},
		{Attention{Key: "failed_jobs", Label: "Background jobs that failed", Severity: "warning", Link: "/payments", Permission: "payments.read"},
			`SELECT count(*) FROM jobs WHERE status = 'failed' AND finished_at > $1::timestamptz - interval '7 days'`, []any{now}},
		// Sprint 5: production. Each is shown only to staff who act on it.
		{Attention{Key: "quality_holds", Label: "Batches on quality hold", Severity: "critical", Link: "/production/quality", Permission: "quality.read"},
			`SELECT count(*) FROM quality_holds WHERE status = 'open'`, nil},
		{Attention{Key: "production_delays", Label: "Batches past their production date and not complete", Severity: "critical", Link: "/production/batches?open=true", Permission: "production.read"},
			`SELECT count(*) FROM production_batches WHERE status NOT IN ('completed', 'cancelled') AND production_date < ($1::timestamptz AT TIME ZONE '` + TimeZone + `')::date`, []any{now}},
		{Attention{Key: "plans_awaiting_approval", Label: "Production plans awaiting approval", Severity: "warning", Link: "/production/plans?status=draft", Permission: "production.approve"},
			`SELECT count(*) FROM production_plans WHERE status = 'draft'`, nil},
		{Attention{Key: "requirement_exceptions", Label: "Ingredient requirements that cannot be calculated", Severity: "warning", Link: "/production/plans?status=draft", Permission: "production.plan"},
			`SELECT count(*) FROM ingredient_requirements r JOIN production_plans p ON p.id = r.plan_id WHERE p.status IN ('draft', 'approved') AND r.status <> 'ok'`, nil},
		{Attention{Key: "batches_due_today", Label: "Batches due today", Severity: "info", Link: "/production/batches?date=today", Permission: "production.read"},
			`SELECT count(*) FROM production_batches WHERE status NOT IN ('completed', 'cancelled') AND production_date = ($1::timestamptz AT TIME ZONE '` + TimeZone + `')::date`, []any{now}},
		{Attention{Key: "batches_in_progress", Label: "Batches in progress", Severity: "info", Link: "/production/batches?status=in_progress", Permission: "production.read"},
			`SELECT count(*) FROM production_batches WHERE status = 'in_progress'`, nil},
		{Attention{Key: "packaging_incomplete", Label: "Batches produced but not yet packed", Severity: "info", Link: "/production/packaging", Permission: "production.read"},
			`SELECT count(*) FROM production_batches WHERE status = 'awaiting_quality_check' AND packaging_status <> 'completed'`, nil},
	}
	for _, x := range attention {
		v, err := one(qctx, tx, x.sql, x.args...)
		if err != nil {
			return nil, err
		}
		a := x.Attention
		a.Count = v
		d.Attention = append(d.Attention, a)
	}
	var next time.Time
	if err := tx.QueryRow(qctx, `SELECT min(cutoff_at) FROM subscription_cycles WHERE state = 'scheduled' AND cutoff_at > $1`, now).Scan(&next); err == nil {
		d.NextCutoffAt = &next
	}

	// Twelve weeks of trends, by delivery or local calendar week.
	weeks := `SELECT g::date FROM generate_series(date_trunc('week', ($1::timestamptz AT TIME ZONE '` + TimeZone + `')) - interval '11 weeks',
	          date_trunc('week', ($1::timestamptz AT TIME ZONE '` + TimeZone + `')), interval '1 week') g`
	charts := []struct {
		key, label, kind, def, sql string
	}{
		{"collected_weekly", "Collected per week", "money", "Successful payments by the local week they succeeded in, before refunds.",
			`SELECT w.g, coalesce(sum(p.amount_minor), 0) FROM (` + weeks + `) w(g)
			 LEFT JOIN payments p ON p.succeeded_at IS NOT NULL AND date_trunc('week', p.succeeded_at AT TIME ZONE '` + TimeZone + `')::date = w.g
			 GROUP BY w.g ORDER BY w.g`},
		{"new_subscriptions_weekly", "New subscriptions per week", "count", "Subscriptions first activated in each local week.",
			`SELECT w.g, count(s.id) FROM (` + weeks + `) w(g)
			 LEFT JOIN subscriptions s ON s.activated_at IS NOT NULL AND date_trunc('week', s.activated_at AT TIME ZONE '` + TimeZone + `')::date = w.g
			 GROUP BY w.g ORDER BY w.g`},
		{"orders_weekly", "Orders per delivery week", "count", "Orders (boxes) by the delivery week they are for.",
			`SELECT w.g, count(o.id) FROM (` + weeks + `) w(g)
			 LEFT JOIN orders o ON o.week_start = w.g AND o.status <> 'cancelled'
			 GROUP BY w.g ORDER BY w.g`},
		{"failed_payments_weekly", "Failed payments per week", "count", "Payment attempts that failed, by local week.",
			`SELECT w.g, count(p.id) FROM (` + weeks + `) w(g)
			 LEFT JOIN payments p ON p.status = 'failed' AND date_trunc('week', p.updated_at AT TIME ZONE '` + TimeZone + `')::date = w.g
			 GROUP BY w.g ORDER BY w.g`},
	}
	for _, c := range charts {
		ser := Series{Key: c.key, Label: c.label, Kind: c.kind, Definition: c.def, Points: []Point{}}
		rows, err := tx.Query(qctx, c.sql, now)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var day time.Time
			var v int64
			if err := rows.Scan(&day, &v); err != nil {
				rows.Close()
				return nil, err
			}
			ser.Points = append(ser.Points, Point{Period: day.Format("2006-01-02"), Value: v})
		}
		rows.Close()
		d.Charts = append(d.Charts, ser)
	}
	rows, err := tx.Query(qctx, `SELECT p.name, count(*) FROM subscriptions s JOIN subscription_plans p ON p.id = s.plan_id
	                              WHERE s.status IN ('active', 'pause_scheduled', 'paused', 'past_due', 'cancel_scheduled')
	                              GROUP BY p.name ORDER BY count(*) DESC, p.name`)
	if err != nil {
		return nil, err
	}
	d.Plans = []Share{}
	for rows.Next() {
		var sh Share
		if err := rows.Scan(&sh.Label, &sh.Value); err != nil {
			rows.Close()
			return nil, err
		}
		d.Plans = append(d.Plans, sh)
	}
	rows.Close()
	return d, nil
}
