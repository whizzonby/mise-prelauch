package analytics

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"mise.tt/api/internal/platform/db"
)

func money(minor int64) string {
	neg := minor < 0
	if neg {
		minor = -minor
	}
	whole, cents := minor/100, minor%100
	s := strconv.FormatInt(whole, 10)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	out := fmt.Sprintf("TTD %s.%02d", b.String(), cents)
	if neg {
		return "−" + out
	}
	return out
}

func pct(num, den int64) string {
	if den == 0 {
		return "—"
	}
	return fmt.Sprintf("%d%%", (num*100+den/2)/den)
}

// rows collects string rows from a query whose columns are all text.
func rows(ctx context.Context, q db.Querier, sql string, args ...any) ([][]string, error) {
	r, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	out := [][]string{}
	for r.Next() {
		vals, err := r.Values()
		if err != nil {
			return nil, err
		}
		row := make([]string, len(vals))
		for i, v := range vals {
			if v == nil {
				row[i] = "—"
			} else {
				row[i] = fmt.Sprint(v)
			}
		}
		out = append(out, row)
	}
	return out, r.Err()
}

// crossWeekly places weekly facts from each domain side by side.
func crossWeekly(ctx context.Context, s *Service, q db.Querier, r Range, _ Filters) (Table, error) {
	rs, err := rows(ctx, q, `
		WITH w AS (SELECT generate_series(date_trunc('week', $1::date)::date, $2::date, interval '7 days')::date AS week)
		SELECT to_char(w.week, 'YYYY-MM-DD'),
		  (SELECT coalesce(sum(i.quantity), 0) FROM order_items i JOIN orders o ON o.id = i.order_id WHERE o.week_start = w.week AND o.status NOT IN ('pending', 'cancelled'))::text,
		  (SELECT coalesce(sum(y.accepted_kits), 0) FROM production_yield_records y JOIN production_batches b ON b.id = y.batch_id JOIN production_plans p ON p.id = b.plan_id
		     WHERE p.week_start = w.week AND b.status <> 'cancelled')::text,
		  (SELECT coalesce(sum(jsonb_array_length(e.payload->'shortages')), 0) FROM domain_events e JOIN production_plans p ON p.id::text = e.payload->>'plan_id'
		     WHERE e.type = 'procurement.shortage_identified' AND p.week_start = w.week)::text,
		  (SELECT count(*) FROM deliveries d WHERE d.order_week = w.week AND d.status = 'delivered')::text,
		  (SELECT count(*) FROM deliveries d WHERE d.order_week = w.week AND d.status IN ('delivery_failed', 'returned'))::text,
		  (SELECT count(*) FROM support_cases c WHERE c.category = 'delivery' AND (c.created_at AT TIME ZONE 'America/Port_of_Spain')::date BETWEEN w.week AND w.week + 6)::text
		FROM w ORDER BY w.week DESC LIMIT 26`, r.From, r.To)
	if err != nil {
		return Table{}, err
	}
	return Table{Title: "By order week", Note: "Kits ordered (confirmed), kits accepted in production, ingredient shortages identified when procurement ran, deliveries made and failed or returned, and delivery support cases opened that week. Side by side, not cause and effect.",
		Columns: []string{"Week", "Kits ordered", "Kits accepted", "Shortages identified", "Delivered", "Failed or returned", "Delivery cases"}, Rows: rs}, nil
}

func collectedByPlan(ctx context.Context, s *Service, q db.Querier, r Range, f Filters) (Table, error) {
	ft, tt := s.bounds(r)
	rs, err := rows(ctx, q, `SELECT pl.name, count(*)::text, sum(p.amount_minor)::bigint FROM payments p
		JOIN billing_cycles b ON b.id = p.billing_cycle_id JOIN subscriptions s ON s.id = b.subscription_id JOIN subscription_plans pl ON pl.id = s.plan_id
		WHERE p.succeeded_at >= $1 AND p.succeeded_at < $2 AND p.currency = 'TTD' GROUP BY pl.name ORDER BY 3 DESC`, ft, tt)
	if err != nil {
		return Table{}, err
	}
	for _, row := range rs {
		n, _ := strconv.ParseInt(row[2], 10, 64)
		row[2] = money(n)
	}
	return Table{Title: "Collected by plan", Note: "Same definition as Collected.", Columns: []string{"Plan", "Payments", "Collected"}, Rows: rs}, nil
}

func activeByPlan(ctx context.Context, s *Service, q db.Querier, r Range, _ Filters) (Table, error) {
	_, tt := s.bounds(r)
	rs, err := rows(ctx, q, `SELECT pl.name, count(*)::text FROM subscriptions s JOIN subscription_plans pl ON pl.id = s.plan_id
		WHERE EXISTS (SELECT 1 FROM subscription_events e WHERE e.subscription_id = s.id AND e.type = 'activated' AND e.created_at < $1)
		AND NOT EXISTS (SELECT 1 FROM subscription_events e WHERE e.subscription_id = s.id AND e.type IN ('cancelled', 'expired') AND e.created_at < $1)
		GROUP BY pl.name ORDER BY 2 DESC`, tt)
	if err != nil {
		return Table{}, err
	}
	return Table{Title: "Active subscriptions by plan", Note: "At the end of the range; same definition as Active subscriptions.", Columns: []string{"Plan", "Active"}, Rows: rs}, nil
}

// cohortTable: customers by month of first successful payment, and the
// share with a paid order (an order for a week paid in that month) in each
// of the following months. Rates are shown only for cohorts of at least five.
func cohortTable(ctx context.Context, s *Service, q db.Querier, r Range, _ Filters) (Table, error) {
	rr, err := q.Query(ctx, `
		WITH first AS (SELECT customer_id, date_trunc('month', min(succeeded_at) AT TIME ZONE 'America/Port_of_Spain')::date AS cohort
		               FROM payments WHERE succeeded_at IS NOT NULL GROUP BY customer_id),
		     active AS (SELECT DISTINCT o.customer_id, date_trunc('month', o.week_start)::date AS m FROM orders o WHERE o.status NOT IN ('pending', 'cancelled'))
		SELECT f.cohort, count(DISTINCT f.customer_id)::int,
		       count(DISTINCT a.customer_id) FILTER (WHERE a.m = f.cohort)::int,
		       count(DISTINCT a.customer_id) FILTER (WHERE a.m = (f.cohort + interval '1 month')::date)::int,
		       count(DISTINCT a.customer_id) FILTER (WHERE a.m = (f.cohort + interval '2 months')::date)::int,
		       count(DISTINCT a.customer_id) FILTER (WHERE a.m = (f.cohort + interval '3 months')::date)::int
		FROM first f LEFT JOIN active a ON a.customer_id = f.customer_id
		WHERE f.cohort BETWEEN date_trunc('month', $1::date)::date AND $2::date
		GROUP BY f.cohort ORDER BY f.cohort DESC LIMIT 24`, r.From, r.To)
	if err != nil {
		return Table{}, err
	}
	defer rr.Close()
	t := Table{Title: "Cohorts by first payment month", Note: "Months +0 to +3: share of the cohort with an order for a week in that month. Fewer than 5 customers: counts only, no rate.",
		Columns: []string{"Cohort", "Customers", "Month +0", "Month +1", "Month +2", "Month +3"}, Rows: [][]string{}}
	for rr.Next() {
		var c time.Time
		var n, m0, m1, m2, m3 int64
		if err := rr.Scan(&c, &n, &m0, &m1, &m2, &m3); err != nil {
			return Table{}, err
		}
		cell := func(k int64) string {
			if n < 5 {
				return strconv.FormatInt(k, 10)
			}
			return fmt.Sprintf("%s (%d)", pct(k, n), k)
		}
		t.Rows = append(t.Rows, []string{c.Format("2006-01"), strconv.FormatInt(n, 10), cell(m0), cell(m1), cell(m2), cell(m3)})
	}
	return t, rr.Err()
}

// signalTable counts explainable retention signals. Customer-level detail
// stays in Customer 360; analytics shows counts only.
func signalTable(ctx context.Context, s *Service, q db.Querier, r Range, _ Filters) (Table, error) {
	ft, tt := s.bounds(r)
	var failed, quiet, paused, failures, cases int64
	err := q.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM (SELECT b.subscription_id FROM payments p JOIN billing_cycles b ON b.id = p.billing_cycle_id WHERE p.status = 'failed'
			AND p.updated_at >= $1 AND p.updated_at < $2 GROUP BY b.subscription_id HAVING count(*) >= 2) x),
		(SELECT count(*) FROM subscriptions s WHERE s.status IN ('active', 'pause_scheduled') AND NOT EXISTS (SELECT 1 FROM orders o WHERE o.subscription_id = s.id
			AND o.created_at >= $1 AND o.created_at < $2)),
		(SELECT count(*) FROM subscriptions s WHERE s.status = 'paused'),
		(SELECT count(*) FROM (SELECT customer_id FROM deliveries WHERE status IN ('delivery_failed', 'returned') OR attempt > 1 GROUP BY customer_id HAVING count(*) >= 2) x),
		(SELECT count(*) FROM (SELECT customer_id FROM support_cases WHERE customer_id IS NOT NULL AND created_at >= $1 AND created_at < $2 GROUP BY customer_id HAVING count(*) >= 2) x)`,
		ft, tt).Scan(&failed, &quiet, &paused, &failures, &cases)
	if err != nil {
		return Table{}, err
	}
	return Table{Title: "Signals in the range", Note: "Counts of subscriptions or customers. No score is computed and nothing changes for the customer.",
		Columns: []string{"Signal", "Count", "How it is counted"}, Rows: [][]string{
			{"Repeated failed payments", strconv.FormatInt(failed, 10), "Subscriptions with 2 or more failed payment attempts in the range"},
			{"No order in the range", strconv.FormatInt(quiet, 10), "Active subscriptions with no order made in the range"},
			{"Paused now", strconv.FormatInt(paused, 10), "Subscriptions currently paused"},
			{"Repeated delivery problems", strconv.FormatInt(failures, 10), "Customers with 2 or more failed, returned or reattempted deliveries (all time)"},
			{"Frequent support contact", strconv.FormatInt(cases, 10), "Customers with 2 or more support cases opened in the range"},
		}}, nil
}

func mealTable(ctx context.Context, s *Service, q db.Querier, r Range, f Filters) (Table, error) {
	rs, err := rows(ctx, q, `
		SELECT m.name,
		  (SELECT count(DISTINCT wm.menu_id) FROM weekly_menu_meals wm JOIN weekly_menus wk ON wk.id = wm.menu_id WHERE wm.meal_id = m.id AND wk.week_start BETWEEN $1 AND $2)::text,
		  coalesce(sum(i.quantity), 0)::text,
		  (SELECT coalesce(sum(y.accepted_kits), 0) FROM production_yield_records y JOIN production_batches b ON b.id = y.batch_id
		     WHERE b.meal_id = m.id AND b.status <> 'cancelled' AND b.production_date BETWEEN $1 AND $2)::text,
		  CASE WHEN m.is_demo THEN 'demo' ELSE '' END
		FROM meals m LEFT JOIN order_items i ON i.meal_id = m.id AND EXISTS (SELECT 1 FROM orders o WHERE o.id = i.order_id AND o.status NOT IN ('pending', 'cancelled')
		     AND o.week_start BETWEEN $1 AND $2)
		WHERE ($3::uuid IS NULL OR m.id = $3)
		GROUP BY m.id, m.name, m.is_demo
		HAVING coalesce(sum(i.quantity), 0) > 0 OR (SELECT count(*) FROM weekly_menu_meals wm JOIN weekly_menus wk ON wk.id = wm.menu_id WHERE wm.meal_id = m.id AND wk.week_start BETWEEN $1 AND $2) > 0
		ORDER BY coalesce(sum(i.quantity), 0) DESC, m.name LIMIT 100`, r.From, r.To, nilIf(f.Meal))
	if err != nil {
		return Table{}, err
	}
	return Table{Title: "Meals", Note: "Weeks on the menu is availability: a meal on few menus cannot be compared with one always offered. Kits accepted is production output for the same dates.",
		Columns: []string{"Meal", "Weeks on menu", "Kits ordered", "Kits accepted", "Origin"}, Rows: rs}, nil
}

func productionByMeal(ctx context.Context, s *Service, q db.Querier, r Range, f Filters) (Table, error) {
	rs, err := rows(ctx, q, `SELECT b.meal_name, count(*)::text, sum(b.planned_kits)::text, coalesce(sum(y.accepted_kits), 0)::text, coalesce(sum(y.rejected_kits), 0)::text,
		(coalesce(sum(y.accepted_kits), 0) - sum(b.planned_kits))::text
		FROM production_batches b LEFT JOIN production_yield_records y ON y.batch_id = b.id
		WHERE b.status <> 'cancelled' AND b.production_date BETWEEN $1 AND $2 AND ($3::uuid IS NULL OR b.meal_id = $3)
		GROUP BY b.meal_name ORDER BY 3 DESC LIMIT 100`, r.From, r.To, nilIf(f.Meal))
	if err != nil {
		return Table{}, err
	}
	return Table{Title: "By meal", Note: "Variance = accepted − planned (negative means fewer kits than planned).",
		Columns: []string{"Meal", "Batches", "Planned", "Accepted", "Rejected", "Variance"}, Rows: rs}, nil
}

func supplierTable(ctx context.Context, s *Service, q db.Querier, r Range, f Filters) (Table, error) {
	ft, tt := s.bounds(r)
	rs, err := rows(ctx, q, `SELECT sp.name, count(DISTINCT p.id)::text,
		coalesce(sum((SELECT sum(l.line_total_minor) FROM purchase_order_lines l WHERE l.po_id = p.id)), 0)::bigint,
		(SELECT coalesce(sum(gl.rejected_packs), 0) FROM goods_receipt_lines gl JOIN goods_receipts g ON g.id = gl.receipt_id JOIN purchase_orders p2 ON p2.id = g.po_id
		   WHERE p2.supplier_id = sp.id AND g.received_at >= $1 AND g.received_at < $2)::text
		FROM suppliers sp JOIN purchase_orders p ON p.supplier_id = sp.id AND p.issued_at >= $1 AND p.issued_at < $2 AND p.currency = 'TTD'
		WHERE ($3::uuid IS NULL OR sp.id = $3) GROUP BY sp.id, sp.name ORDER BY 3 DESC LIMIT 100`, ft, tt, nilIf(f.Supplier))
	if err != nil {
		return Table{}, err
	}
	for _, row := range rs {
		n, _ := strconv.ParseInt(row[2], 10, 64)
		row[2] = money(n)
	}
	return Table{Title: "By supplier", Note: "Orders issued in the range and packs rejected on deliveries received in the range. Facts, not ratings.",
		Columns: []string{"Supplier", "Orders issued", "Lines value", "Packs rejected"}, Rows: rs}, nil
}

// ledgerCheck compares each lot's balance with the sum of its ledger movements.
func ledgerCheck(ctx context.Context, s *Service, q db.Querier, _ Range, _ Filters) (Table, error) {
	var lots, mismatched int64
	if err := q.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE l.on_hand <> coalesce(m.total, 0))
		FROM inventory_lots l LEFT JOIN (SELECT lot_id, sum(on_hand_delta) AS total FROM inventory_movements GROUP BY lot_id) m ON m.lot_id = l.id`).Scan(&lots, &mismatched); err != nil {
		return Table{}, err
	}
	return Table{Title: "Ledger reconciliation", Note: "Every lot's on-hand balance against the sum of its ledger movements. Any mismatch is a data-quality issue.",
		Columns: []string{"Lots checked", "Balances that differ from the ledger"}, Rows: [][]string{{strconv.FormatInt(lots, 10), strconv.FormatInt(mismatched, 10)}}}, nil
}

func zoneTable(ctx context.Context, s *Service, q db.Querier, r Range, f Filters) (Table, error) {
	rs, err := rows(ctx, q, `SELECT coalesce(z.code, 'No zone'), count(*)::text,
		count(*) FILTER (WHERE d.status = 'delivered' AND coalesce((SELECT x.state FROM order_delivery_outcomes x WHERE x.order_id = d.order_id ORDER BY x.id DESC LIMIT 1), '') <> 'disputed')::text,
		count(*) FILTER (WHERE d.status IN ('delivery_failed', 'returned'))::text, count(*) FILTER (WHERE d.attempt > 1)::text
		FROM deliveries d LEFT JOIN delivery_service_zones z ON z.id = d.zone_id
		WHERE d.schedule_confirmed_at IS NOT NULL AND d.status <> 'cancelled' AND d.service_date BETWEEN $1 AND $2 AND ($3::uuid IS NULL OR d.zone_id = $3)
		GROUP BY 1 ORDER BY 2 DESC`, r.From, r.To, nilIf(f.Zone))
	if err != nil {
		return Table{}, err
	}
	return Table{Title: "By zone", Note: "Same definitions as above.", Columns: []string{"Zone", "Scheduled", "Delivered", "Failed or returned", "Reattempted"}, Rows: rs}, nil
}

func nilIf(s string) any {
	if s == "" {
		return nil
	}
	return s
}
