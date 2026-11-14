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

// The production report has its own route and permission (production_reports.read).

var errRange = httpx.ValidationError([]httpx.FieldError{{Field: "from", Code: "range",
	Message: "Choose a range of 1 day to 2 years, with from before to (YYYY-MM-DD)."}})

// Report builds one report. from and to are local dates, both included.
func (s *Service) Report(ctx context.Context, kind, from, to string) (*Report, error) {
	f, err1 := time.Parse("2006-01-02", from)
	t, err2 := time.Parse("2006-01-02", to)
	if err1 != nil || err2 != nil || t.Before(f) || t.Sub(f) > 731*24*time.Hour {
		return nil, errRange
	}
	r := &Report{Kind: kind, From: from, To: to, TimeZone: TimeZone, Totals: []Metric{}, Series: []Series{}, Breakdowns: map[string][]Share{}, Definitions: map[string]string{}}
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
	case "production":
		// Batches by production date. Cancelled batches count nowhere except
		// the cancelled total.
		live := `b.production_date BETWEEN $1::date AND $2::date AND b.status <> 'cancelled'`
		add(series("planned_kits", "Planned kits", "count", "Kits planned in batches, by production date (cancelled batches excluded).",
			`coalesce(sum(pb.planned_kits), 0)::bigint FROM (`+buckets+`) b(g) LEFT JOIN production_batches pb ON date_trunc($3, pb.production_date::timestamp)::date = b.g
			 AND pb.production_date BETWEEN $1::date AND $2::date AND pb.status <> 'cancelled' GROUP BY b.g ORDER BY b.g`))
		add(series("accepted_kits", "Completed kits", "count", "Accepted kits of completed batches, by production date: produced minus rejected, reworked kits counted once.",
			`coalesce(sum(y.accepted_kits), 0)::bigint FROM (`+buckets+`) b(g) LEFT JOIN production_batches pb ON date_trunc($3, pb.production_date::timestamp)::date = b.g
			 AND pb.production_date BETWEEN $1::date AND $2::date AND pb.status = 'completed' LEFT JOIN production_yield_records y ON y.batch_id = pb.id GROUP BY b.g ORDER BY b.g`))
		add(total("planned_kits", "Planned kits", "count", "Kits planned in batches with a production date in the range (cancelled batches excluded).",
			`SELECT coalesce(sum(planned_kits), 0) FROM production_batches b WHERE `+live, rng...))
		add(total("completed_kits", "Completed kits", "count", "Accepted kits of batches completed (ready for fulfilment) with a production date in the range. Rejected kits are never counted.",
			`SELECT coalesce(sum(y.accepted_kits), 0) FROM production_batches b JOIN production_yield_records y ON y.batch_id = b.id
			 WHERE b.production_date BETWEEN $1::date AND $2::date AND b.status = 'completed'`, rng...))
		add(total("batch_completion_rate", "Batch completion rate (%)", "percent", "Completed batches as a whole percentage of batches due in the range (cancelled batches excluded). 0 when none were due.",
			`SELECT CASE WHEN count(*) = 0 THEN 0 ELSE round(100.0 * count(*) FILTER (WHERE b.status = 'completed') / count(*)) END FROM production_batches b WHERE `+live, rng...))
		add(total("batches", "Batches", "count", "Batches with a production date in the range (cancelled excluded).",
			`SELECT count(*) FROM production_batches b WHERE `+live, rng...))
		add(total("cancelled_batches", "Cancelled batches", "count", "Batches with a production date in the range that were cancelled or discarded.",
			`SELECT count(*) FROM production_batches b WHERE b.production_date BETWEEN $1::date AND $2::date AND b.status = 'cancelled'`, rng...))
		add(total("quality_holds", "Quality holds", "count", "Holds placed on batches with a production date in the range, whatever their outcome.",
			`SELECT count(*) FROM quality_holds h JOIN production_batches b ON b.id = h.batch_id WHERE b.production_date BETWEEN $1::date AND $2::date`, rng...))
		add(total("rejected_kits", "Rejected kits", "count", "Kits rejected during production (never available for fulfilment).",
			`SELECT coalesce(sum(y.rejected_kits), 0) FROM production_yield_records y JOIN production_batches b ON b.id = y.batch_id WHERE b.production_date BETWEEN $1::date AND $2::date`, rng...))
		add(total("reworked_kits", "Reworked kits", "count", "Kits that failed and were fixed; already inside the completed kits, not added again.",
			`SELECT coalesce(sum(y.reworked_kits), 0) FROM production_yield_records y JOIN production_batches b ON b.id = y.batch_id WHERE b.production_date BETWEEN $1::date AND $2::date`, rng...))
		add(total("yield_variance", "Yield variance (kits)", "count", "Accepted minus planned kits, over batches whose output was recorded. Negative means fewer kits than planned.",
			`SELECT coalesce(sum(y.accepted_kits - y.planned_kits), 0) FROM production_yield_records y JOIN production_batches b ON b.id = y.batch_id WHERE b.production_date BETWEEN $1::date AND $2::date`, rng...))
		add(total("waste_records", "Waste records", "count", "Waste entries recorded in the range.",
			`SELECT count(*) FROM production_waste w WHERE `+inRange("w.created_at"), rng...))
		add(breakdown("by_meal", "Accepted kits of completed batches in the range, by meal.",
			`SELECT b.meal_name, coalesce(sum(y.accepted_kits), 0)::bigint FROM production_batches b JOIN production_yield_records y ON y.batch_id = b.id
			 WHERE b.production_date BETWEEN $1::date AND $2::date AND b.status = 'completed' GROUP BY 1 ORDER BY 2 DESC, 1`, rng...))
		add(breakdown("planned_by_meal", "Planned kits in the range, by meal.",
			`SELECT b.meal_name, sum(b.planned_kits)::bigint FROM production_batches b WHERE `+live+` GROUP BY 1 ORDER BY 2 DESC, 1`, rng...))
		add(breakdown("waste", "Waste recorded in the range by category and unit, rounded to whole units (quantities as weighed or counted).",
			`SELECT replace(w.category, '_', ' ') || ' (' || w.unit || ')', round(sum(w.quantity))::bigint FROM production_waste w WHERE `+inRange("w.created_at")+`
			 GROUP BY w.category, w.unit ORDER BY 2 DESC, 1`, rng...))
		add(breakdown("requirements", "Ingredient requirements of live plans with a production date in the range, in each ingredient's base unit, rounded to whole units. Lines that could not be calculated are left out.",
			`SELECT i.name || CASE WHEN r.preparation <> '' THEN ', ' || r.preparation ELSE '' END || ' (' || r.unit || ')', round(sum(r.quantity))::bigint
			 FROM ingredient_requirements r JOIN production_plans p ON p.id = r.plan_id JOIN ingredients i ON i.id = r.ingredient_id
			 WHERE p.status <> 'cancelled' AND p.production_date BETWEEN $1::date AND $2::date AND r.status = 'ok'
			 GROUP BY i.name, r.preparation, r.unit ORDER BY 1`, rng...))
	case "procurement":
		// Purchase orders count once, at their latest revision approved in
		// the range. Money is TTD only: no conversion rate is configured, so
		// orders in another currency are left out of value totals.
		approvedInRange := `SELECT DISTINCT ON (v.po_id) v.po_id, v.total_minor, v.approved_at FROM purchase_order_revisions v JOIN purchase_orders po ON po.id = v.po_id
			WHERE po.currency = 'TTD' AND ` + inRange("v.approved_at") + ` ORDER BY v.po_id, v.revision DESC`
		add(series("po_value", "Purchase orders approved (TTD)", "money", "Value of purchase orders approved, by local day or week of approval; each order once, at its latest revision approved in the range.",
			`coalesce(sum(a.total_minor), 0)::bigint FROM (`+buckets+`) b(g) LEFT JOIN (`+approvedInRange+`) a ON `+local("a.approved_at")+` = b.g GROUP BY b.g ORDER BY b.g`))
		add(total("runs", "Procurement runs", "count", "Released production plans taken into procurement in the range.",
			`SELECT count(*) FROM procurement_runs r WHERE `+inRange("r.created_at"), rng...))
		add(total("shortages", "Runs with shortages", "count", "Procurement runs created in the range that found at least one ingredient short or incomplete when created.",
			`SELECT count(*) FROM domain_events e WHERE e.type = 'procurement.shortage_identified' AND `+inRange("e.occurred_at"), rng...))
		add(total("pos_approved", "Orders approved", "count", "Purchase orders approved in the range (each order once).",
			`SELECT count(*) FROM (`+approvedInRange+`) a`, rng...))
		add(total("po_value", "Value approved (TTD)", "money", "Total of purchase orders approved in the range, each at its latest revision approved in the range, TTD only.",
			`SELECT coalesce(sum(total_minor), 0) FROM (`+approvedInRange+`) a`, rng...))
		add(total("pos_issued", "Orders issued", "count", "Purchase orders issued to suppliers in the range.",
			`SELECT count(*) FROM purchase_orders po WHERE `+inRange("po.issued_at"), rng...))
		add(total("receipts", "Deliveries received", "count", "Goods receipts recorded in the range.",
			`SELECT count(*) FROM goods_receipts g WHERE `+inRange("g.received_at"), rng...))
		add(total("open_pos", "Open orders (now)", "count", "Approved, issued or partly received orders not yet fully received, today.",
			`SELECT count(*) FROM purchase_orders WHERE status IN ('approved', 'issued', 'partially_received')`))
		add(total("overdue_pos", "Overdue orders (now)", "count", "Open orders whose expected delivery date has passed, today (Trinidad and Tobago).",
			`SELECT count(*) FROM purchase_orders WHERE status IN ('approved', 'issued', 'partially_received') AND expected_delivery < (now() AT TIME ZONE '`+TimeZone+`')::date`))
		add(total("rfqs", "Requests for quotation", "count", "Requests for quotation created in the range.",
			`SELECT count(*) FROM rfqs WHERE `+inRange("created_at"), rng...))
		add(total("quotations", "Quotations recorded", "count", "Quotations recorded as received in the range.",
			`SELECT count(*) FROM quotations WHERE `+inRange("received_at"), rng...))
		add(breakdown("value_by_supplier", "Value of orders approved in the range by supplier (TTD, each order once).",
			`SELECT s.name, sum(a.total_minor)::bigint FROM (`+approvedInRange+`) a JOIN purchase_orders po ON po.id = a.po_id JOIN suppliers s ON s.id = po.supplier_id
			 GROUP BY s.name ORDER BY 2 DESC, 1`, rng...))
		add(breakdown("decisions", "Purchasing decisions recorded on runs created in the range, by decision.",
			`SELECT replace(l.decision, '_', ' '), count(*) FROM procurement_lines l JOIN procurement_runs r ON r.id = l.run_id WHERE `+inRange("r.created_at")+`
			 GROUP BY l.decision ORDER BY 2 DESC, 1`, rng...))
	case "inventory":
		add(series("receipts", "Lots received", "count", "Lots created on receipt (accepted, quarantined or rejected), by local day or week.",
			`count(l.id)::bigint FROM (`+buckets+`) b(g) LEFT JOIN inventory_lots l ON `+local("l.received_at")+` = b.g AND `+inRange("l.received_at")+` GROUP BY b.g ORDER BY b.g`))
		add(series("issues", "Issues to production", "count", "Stock issue movements to production batches, by local day or week.",
			`count(m.id)::bigint FROM (`+buckets+`) b(g) LEFT JOIN inventory_movements m ON m.type = 'issue' AND `+local("m.created_at")+` = b.g AND `+inRange("m.created_at")+` GROUP BY b.g ORDER BY b.g`))
		add(total("lots_received", "Lots received", "count", "Lots created on receipt in the range, whatever their status.",
			`SELECT count(*) FROM inventory_lots l WHERE `+inRange("l.received_at"), rng...))
		add(total("lots_rejected", "Lots rejected or quarantined on receipt", "count", "Lots created in the range that were not accepted on receipt.",
			`SELECT count(*) FROM inventory_movements m JOIN inventory_lots l ON l.id = m.lot_id WHERE m.type = 'receipt' AND l.status IN ('rejected', 'quarantined')
			 AND `+inRange("m.created_at"), rng...))
		add(total("received_value", "Accepted stock value (TTD)", "money", "Accepted packs × the order's price per pack, for deliveries received in the range (TTD only).",
			`SELECT coalesce(round(sum(gl.accepted_packs * gl.unit_cost_minor)), 0)::bigint FROM goods_receipt_lines gl JOIN goods_receipts g ON g.id = gl.receipt_id
			 WHERE gl.currency = 'TTD' AND `+inRange("g.received_at"), rng...))
		add(total("issues", "Issue movements", "count", "Stock issued to production batches in the range (one per lot per issue).",
			`SELECT count(*) FROM inventory_movements m WHERE m.type = 'issue' AND `+inRange("m.created_at"), rng...))
		add(total("returns", "Returns from production", "count", "Stock returned from batches to the store in the range.",
			`SELECT count(*) FROM inventory_movements m WHERE m.type = 'return' AND `+inRange("m.created_at"), rng...))
		add(total("adjustments", "Adjustments", "count", "Stock adjustments (including posted count variances) in the range.",
			`SELECT count(*) FROM stock_adjustments a WHERE `+inRange("a.created_at"), rng...))
		add(total("store_waste", "Store waste records", "count", "Stock wasted in storage in the range (production waste is in the production report).",
			`SELECT count(*) FROM inventory_movements m WHERE m.type = 'waste' AND `+inRange("m.created_at"), rng...))
		add(total("counts_posted", "Counts posted", "count", "Stock counts approved and posted in the range.",
			`SELECT count(*) FROM stock_counts c WHERE c.status = 'posted' AND `+inRange("c.posted_at"), rng...))
		add(total("expired_lots", "Expired lots with stock (now)", "count", "Lots past their recorded expiry date still holding stock, today.",
			`SELECT count(*) FROM inventory_lots WHERE on_hand > 0 AND expiry_status = 'recorded' AND expiry_date < (now() AT TIME ZONE '`+TimeZone+`')::date`))
		add(total("expiry_review", "Lots awaiting an expiry review (now)", "count", "Available lots with stock and no expiry recorded or confirmed, today.",
			`SELECT count(*) FROM inventory_lots WHERE on_hand > 0 AND status = 'available' AND expiry_status = 'not_recorded'`))
		add(breakdown("on_hand", "Stock on hand now by ingredient, in its base unit, rounded to whole units.",
			`SELECT i.name || ' (' || l.unit || ')', round(sum(l.on_hand))::bigint FROM inventory_lots l JOIN ingredients i ON i.id = l.ingredient_id
			 WHERE l.on_hand > 0 GROUP BY i.name, l.unit ORDER BY 1`))
		add(breakdown("issued", "Stock issued to production in the range by ingredient, net of returns, in its base unit, rounded to whole units.",
			`SELECT i.name || ' (' || m.unit || ')', round(-sum(m.on_hand_delta))::bigint FROM inventory_movements m JOIN ingredients i ON i.id = m.ingredient_id
			 WHERE m.type IN ('issue', 'return') AND `+inRange("m.created_at")+` GROUP BY i.name, m.unit ORDER BY 1`, rng...))
		add(breakdown("movements", "Ledger movements in the range by type.",
			`SELECT replace(m.type, '_', ' '), count(*) FROM inventory_movements m WHERE `+inRange("m.created_at")+` GROUP BY m.type ORDER BY 2 DESC, 1`, rng...))
	case "suppliers":
		// Factual figures per supplier: no ratings, no scores.
		recv := `SELECT po.id, po.supplier_id, po.expected_delivery, po.issued_at, min(g.received_at) AS first_receipt, count(g.id) AS receipts
			FROM purchase_orders po JOIN goods_receipts g ON g.po_id = po.id GROUP BY po.id HAVING ` + inRange("min(g.received_at)")
		add(total("suppliers_approved", "Approved suppliers (now)", "count", "Suppliers approved and not lapsed, today.",
			`SELECT count(*) FROM suppliers WHERE status = 'approved' AND (review_expires_on IS NULL OR review_expires_on >= (now() AT TIME ZONE '`+TimeZone+`')::date)`))
		add(total("deliveries", "Orders first delivered", "count", "Purchase orders whose first delivery was received in the range.",
			`SELECT count(*) FROM (`+recv+`) d`, rng...))
		add(total("on_time", "Delivered on time", "count", "Of those, orders first delivered on or before their expected date (Trinidad and Tobago).",
			`SELECT count(*) FROM (`+recv+`) d WHERE (d.first_receipt AT TIME ZONE '`+TimeZone+`')::date <= d.expected_delivery`, rng...))
		add(breakdown("orders_placed", "Purchase orders approved in the range, by supplier (each order once).",
			`SELECT s.name, count(DISTINCT v.po_id) FROM purchase_order_revisions v JOIN purchase_orders po ON po.id = v.po_id JOIN suppliers s ON s.id = po.supplier_id
			 WHERE `+inRange("v.approved_at")+` GROUP BY s.name ORDER BY 2 DESC, 1`, rng...))
		add(breakdown("orders_delivered", "Orders first delivered in the range, by supplier.",
			`SELECT s.name, count(*) FROM (`+recv+`) d JOIN suppliers s ON s.id = d.supplier_id GROUP BY s.name ORDER BY 2 DESC, 1`, rng...))
		add(breakdown("on_time", "Orders first delivered on or before the expected date, by supplier.",
			`SELECT s.name, count(*) FROM (`+recv+`) d JOIN suppliers s ON s.id = d.supplier_id
			 WHERE (d.first_receipt AT TIME ZONE '`+TimeZone+`')::date <= d.expected_delivery GROUP BY s.name ORDER BY 2 DESC, 1`, rng...))
		add(breakdown("partial_deliveries", "Orders delivered in more than one receipt, or closed short, with a receipt in the range, by supplier.",
			`SELECT s.name, count(DISTINCT po.id) FROM purchase_orders po JOIN suppliers s ON s.id = po.supplier_id
			 WHERE EXISTS (SELECT 1 FROM goods_receipts g WHERE g.po_id = po.id AND `+inRange("g.received_at")+`)
			 AND ((SELECT count(*) FROM goods_receipts g WHERE g.po_id = po.id) > 1
			      OR EXISTS (SELECT 1 FROM purchase_order_lines pl WHERE pl.po_id = po.id AND pl.received_packs < pl.packs AND po.status = 'closed'))
			 GROUP BY s.name ORDER BY 2 DESC, 1`, rng...))
		add(breakdown("rejected_packs", "Packs rejected or quarantined on receipt in the range, by supplier (rounded).",
			`SELECT s.name, round(sum(gl.rejected_packs + gl.quarantined_packs))::bigint FROM goods_receipt_lines gl JOIN goods_receipts g ON g.id = gl.receipt_id
			 JOIN suppliers s ON s.id = g.supplier_id WHERE `+inRange("g.received_at")+` GROUP BY s.name HAVING sum(gl.rejected_packs + gl.quarantined_packs) > 0 ORDER BY 2 DESC, 1`, rng...))
		add(breakdown("lead_time_days", "Average days from issuing an order to its first delivery, for orders first delivered in the range, by supplier (rounded).",
			`SELECT s.name, round(avg(EXTRACT(EPOCH FROM d.first_receipt - d.issued_at) / 86400))::bigint FROM (`+recv+`) d JOIN suppliers s ON s.id = d.supplier_id
			 WHERE d.issued_at IS NOT NULL GROUP BY s.name ORDER BY 1`, rng...))
		add(breakdown("purchase_value", "Value of orders approved in the range by supplier (TTD, each order at its latest revision in the range).",
			`SELECT s.name, sum(a.total_minor)::bigint FROM (SELECT DISTINCT ON (v.po_id) v.po_id, v.total_minor FROM purchase_order_revisions v
			 JOIN purchase_orders po ON po.id = v.po_id WHERE po.currency = 'TTD' AND `+inRange("v.approved_at")+` ORDER BY v.po_id, v.revision DESC) a
			 JOIN purchase_orders po ON po.id = a.po_id JOIN suppliers s ON s.id = po.supplier_id GROUP BY s.name ORDER BY 2 DESC, 1`, rng...))
		add(breakdown("price_changes", "Prices recorded in the range, by supplier (each new price for a product counts once).",
			`SELECT s.name, count(*) FROM supplier_prices pr JOIN supplier_products p ON p.id = pr.product_id JOIN suppliers s ON s.id = p.supplier_id
			 WHERE `+inRange("pr.created_at")+` GROUP BY s.name ORDER BY 2 DESC, 1`, rng...))
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
