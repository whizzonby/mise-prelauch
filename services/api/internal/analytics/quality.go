package analytics

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Check is one data-quality check. Inconsistent records are flagged and
// counted, never silently discarded.
type Check struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Status      string    `json:"status"` // ok, warning, failed
	Count       int       `json:"count"`
	Detail      string    `json:"detail"`
	CheckedAt   time.Time `json:"checked_at"`
}

type checkSpec struct {
	id, name, description string
	// failed: any count is a defect; warning: worth investigating.
	severity string
	sql      string
}

var checks = []checkSpec{
	{"orders_without_items", "Orders without meals", "Live orders that have no order items.", "failed",
		`SELECT count(*) FROM orders o WHERE o.status NOT IN ('pending', 'cancelled') AND NOT EXISTS (SELECT 1 FROM order_items i WHERE i.order_id = o.id)`},
	{"payment_refund_mismatch", "Payment and refund totals differ", "Payments whose refunded amount differs from the sum of their succeeded refunds.", "failed",
		`SELECT count(*) FROM payments p WHERE p.refunded_minor <> coalesce((SELECT sum(amount_minor) FROM refunds r WHERE r.payment_id = p.id AND r.status = 'succeeded'), 0)`},
	{"succeeded_without_time", "Succeeded payments without a time", "Payments marked succeeded (or refunded) that have no succeeded_at, so they cannot be dated.", "failed",
		`SELECT count(*) FROM payments WHERE status IN ('succeeded', 'refunded', 'partially_refunded') AND succeeded_at IS NULL`},
	{"inventory_ledger_mismatch", "Stock balance differs from the ledger", "Lots whose on-hand balance is not the sum of their ledger movements.", "failed",
		`SELECT count(*) FROM inventory_lots l WHERE l.on_hand <> coalesce((SELECT sum(on_hand_delta) FROM inventory_movements m WHERE m.lot_id = l.id), 0)`},
	{"order_delivery_state", "Order and delivery disagree", "Orders marked delivered whose delivery is not delivered and not recorded as disputed, or orders out for delivery with no delivery out.", "failed",
		`SELECT count(*) FROM orders o JOIN deliveries d ON d.order_id = o.id WHERE (o.status = 'delivered' AND d.status <> 'delivered'
			AND coalesce((SELECT x.state FROM order_delivery_outcomes x WHERE x.order_id = o.id ORDER BY x.id DESC LIMIT 1), '') <> 'disputed')
			OR (o.status = 'out_for_delivery' AND d.status NOT IN ('picked_up', 'in_transit', 'arriving', 'delivery_failed', 'pending_assignment', 'assigned', 'ready_for_pickup'))`},
	{"missing_delivery_outcome", "Deliveries past their date with no outcome", "Deliveries whose confirmed date has passed and that are still out or waiting.", "warning",
		`SELECT count(*) FROM deliveries WHERE schedule_confirmed_at IS NOT NULL AND service_date < (now() AT TIME ZONE 'America/Port_of_Spain')::date
			AND status IN ('pending_assignment', 'assigned', 'ready_for_pickup', 'picked_up', 'in_transit', 'arriving')`},
	{"unreconciled_runs", "Completed runs not reconciled", "Dispatch runs with every delivery finished that have not been reconciled.", "warning",
		`SELECT count(*) FROM dispatch_runs WHERE status = 'completed'`},
	{"missing_source_references", "Fulfillment without a production plan", "Fulfillments that do not name the production plan their kits came from.", "warning",
		`SELECT count(*) FROM fulfillment_obligations WHERE plan_id IS NULL AND status <> 'cancelled'`},
	{"duplicate_readiness_events", "Duplicate readiness events", "Batches with more than one production.ready_for_fulfillment event (the outbox key should prevent this).", "failed",
		`SELECT count(*) FROM (SELECT aggregate_id FROM domain_events WHERE type = 'production.ready_for_fulfillment' GROUP BY aggregate_id HAVING count(*) > 1) x`},
	{"failed_consumer_jobs", "Failed event processing", "Event-consumer jobs (procurement, fulfillment, analytics, automation) that ran out of attempts.", "warning",
		`SELECT count(*) FROM jobs WHERE status = 'failed' AND kind IN ('procurement.plan_released', 'fulfillment.batch_ready', 'analytics.event', 'analytics.refresh', 'automation.execute', 'automation.event')`},
	{"test_payments", "Test-provider payments", "Payments taken by the simulated test provider. They must not be read as real revenue (dashboards say so).", "warning",
		`SELECT count(*) FROM payments WHERE provider = 'testpay'`},
	{"demo_records", "Demo records in operations", "Orders including demo meals, and demo delivery partners or couriers.", "warning",
		`SELECT (SELECT count(DISTINCT i.order_id) FROM order_items i JOIN meals m ON m.id = i.meal_id WHERE m.is_demo)
			+ (SELECT count(*) FROM delivery_partners WHERE demo) + (SELECT count(*) FROM delivery_couriers WHERE demo)`},
}

// RunChecks runs every check, stores the results and returns them.
func (s *Service) RunChecks(ctx context.Context) ([]Check, error) {
	out := []Check{}
	err := s.readTx(ctx, func(tx pgx.Tx) error {
		for _, c := range checks {
			var n int
			if err := tx.QueryRow(ctx, c.sql).Scan(&n); err != nil {
				return fmt.Errorf("check %s: %w", c.id, err)
			}
			st := "ok"
			if n > 0 {
				st = c.severity
			}
			out = append(out, Check{ID: c.id, Name: c.name, Description: c.description, Status: st, Count: n, CheckedAt: s.Now().UTC()})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Snapshot freshness and reconciliation of stored values against live ones.
	st, err := s.Status(ctx)
	if err != nil {
		return nil, err
	}
	fresh := Check{ID: "stale_analytics", Name: "Analytics snapshots stale", Description: "No successful refresh in the last " + StaleAfter.String() + ".",
		Status: "ok", CheckedAt: s.Now().UTC()}
	if st.Stale {
		fresh.Status, fresh.Count = "warning", 1
	}
	out = append(out, fresh)
	recon, err := s.reconcileSnapshots(ctx, 7)
	if err != nil {
		return nil, err
	}
	out = append(out, recon)
	for _, c := range out {
		if _, err := s.pool.Exec(ctx, `INSERT INTO analytics_quality_results (check_id, status, count, detail) VALUES ($1, $2, $3, $4)`,
			c.ID, c.Status, c.Count, c.Detail); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// reconcileSnapshots compares stored daily values with live values for the
// last days; any difference means a refresh is missing or failed.
func (s *Service) reconcileSnapshots(ctx context.Context, days int) (Check, error) {
	c := Check{ID: "snapshot_reconciliation", Name: "Snapshots match live figures", Status: "ok", CheckedAt: s.Now().UTC(),
		Description: fmt.Sprintf("Stored daily values for the last %d days, against the same metrics computed live now (missing days count as differences).", days)}
	today := s.today()
	diffs := 0
	var first string
	err := s.readTx(ctx, func(tx pgx.Tx) error {
		for d := today.AddDate(0, 0, -(days - 1)); !d.After(today); d = d.AddDate(0, 0, 1) {
			for i := range registry {
				m := &registry[i]
				if !m.Snapshot {
					continue
				}
				live, err := s.Value(ctx, tx, m, Range{From: d, To: d}, Filters{})
				if err != nil {
					return err
				}
				var stored *float64
				var found bool
				err = tx.QueryRow(ctx, `SELECT value::float8, true FROM analytics_snapshots WHERE metric_id = $1 AND day = $2`, m.ID, d).Scan(&stored, &found)
				if err != nil && err != pgx.ErrNoRows {
					return err
				}
				if !found || !same(stored, live) {
					diffs++
					if first == "" {
						first = m.ID + " on " + d.Format(day)
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return c, err
	}
	if diffs > 0 {
		c.Status, c.Count, c.Detail = "warning", diffs, "First difference: "+first+". Run a refresh for the range."
	}
	return c, nil
}

func same(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	d := *a - *b
	return d < 1e-6 && d > -1e-6
}

// LatestChecks returns the most recent result of each check.
func (s *Service) LatestChecks(ctx context.Context) ([]Check, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT ON (check_id) check_id, status, count, detail, checked_at FROM analytics_quality_results ORDER BY check_id, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := map[string]checkSpec{}
	for _, c := range checks {
		names[c.id] = c
	}
	out := []Check{}
	for rows.Next() {
		var c Check
		if err := rows.Scan(&c.ID, &c.Status, &c.Count, &c.Detail, &c.CheckedAt); err != nil {
			return nil, err
		}
		if spec, ok := names[c.ID]; ok {
			c.Name, c.Description = spec.name, spec.description
		} else if c.ID == "stale_analytics" {
			c.Name = "Analytics snapshots stale"
		} else if c.ID == "snapshot_reconciliation" {
			c.Name = "Snapshots match live figures"
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
