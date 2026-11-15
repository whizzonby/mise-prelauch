// Package analytics is Mise's metric registry and the analytics built on it
// (Sprint 8). Every figure shown anywhere in Intelligence, in exports, in
// snapshots and in scheduled reports is computed by exactly one Metric here,
// so no dashboard can calculate the same figure differently.
//
// Metrics read the authoritative domain tables (commerce, production,
// procurement, inventory, delivery). Analytics writes only its own tables.
package analytics

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"mise.tt/api/internal/identity"
)

// Kind is how a value is read: a count, money in minor units (TTD), a ratio
// in [0, 1], a plain number, or a duration.
type Kind string

const (
	Count Kind = "count"
	Money Kind = "money"
	Ratio Kind = "ratio"
	Num   Kind = "number"
	Hours Kind = "hours"
	Days  Kind = "days"
)

// Metric is one catalogue entry. SQL returns a single numeric (NULL when the
// figure is undefined, e.g. a rate with no denominator). Placeholders:
// {from_ts} {to_ts} (UTC bounds of the local range), {from} {to} (local
// dates, inclusive), {plan} {meal} {zone} {supplier} (optional filters,
// NULL when not applied). Only the placeholders a metric uses are bound.
type Metric struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Definition  string              `json:"definition"`
	Domain      string              `json:"domain"`
	Sources     []string            `json:"source_fields"`
	Calculation string              `json:"calculation"`
	Filters     []string            `json:"filters"`
	Period      string              `json:"reporting_period"`
	Update      string              `json:"update_frequency"`
	Missing     string              `json:"missing_data"`
	Currency    string              `json:"currency"`
	Limitations string              `json:"limitations"`
	Owner       string              `json:"owner"`
	Kind        Kind                `json:"kind"`
	Permission  identity.Permission `json:"permission"`
	// Snapshot: stored daily (additive per day, so a range is a sum of days).
	Snapshot bool   `json:"snapshotted"`
	SQL      string `json:"-"`
}

const (
	tz       = "America/Port_of_Spain (all dates are local to Trinidad and Tobago)"
	ttd      = "TTD minor units (cents). Other currencies are excluded, never converted."
	onDemand = "Computed on request from the authoritative tables; daily values snapshotted hourly"
	live     = "Computed on request from the authoritative tables"
	rangeP   = "Any range of 1 day to 2 years; compared with the previous period of equal length"
)

// liveOrder is an order that counts as demand: made and not cancelled.
const liveOrder = `o.status NOT IN ('pending', 'cancelled')`

var registry = []Metric{
	// ---------------------------------------------------------------- revenue
	{ID: "revenue.ordered_value", Name: "Ordered value", Domain: "commerce", Kind: Money, Permission: identity.PermAnalyticsRevenueRead,
		Definition:  "Total of orders made in the range (paid weeks turned into orders), not cancelled. What customers ordered, not what was collected.",
		Sources:     []string{"orders.total_minor", "orders.created_at", "orders.status", "orders.currency"},
		Calculation: "sum(total_minor) over orders created in the range, status not pending or cancelled, currency TTD",
		Filters:     []string{"plan"}, Missing: "0 when there are no orders", Currency: ttd, Owner: "Finance", Snapshot: true,
		Limitations: "Includes test-provider and free (promotion) orders; see revenue.collected_test for the test share.",
		SQL: `SELECT coalesce(sum(o.total_minor), 0) FROM orders o JOIN subscriptions s ON s.id = o.subscription_id
			WHERE ` + liveOrder + ` AND o.currency = 'TTD' AND o.created_at >= {from_ts} AND o.created_at < {to_ts} AND ({plan} IS NULL OR s.plan_id = {plan})`},
	{ID: "revenue.charged_value", Name: "Charged value", Domain: "commerce", Kind: Money, Permission: identity.PermAnalyticsRevenueRead,
		Definition:  "Amount asked of customers: billing cycles with at least one payment attempt started in the range, each counted once however many retries it took.",
		Sources:     []string{"billing_cycles.total_minor", "payments.billing_cycle_id", "payments.created_at"},
		Calculation: "sum(total_minor) over distinct billing cycles having a payment created in the range",
		Filters:     []string{"plan"}, Missing: "0", Currency: ttd, Owner: "Finance", Snapshot: false,
		Limitations: "A cycle whose attempts span two ranges counts in each range it was attempted.",
		SQL: `SELECT coalesce(sum(b.total_minor), 0) FROM billing_cycles b JOIN subscriptions s ON s.id = b.subscription_id
			WHERE b.currency = 'TTD' AND ({plan} IS NULL OR s.plan_id = {plan}) AND EXISTS (SELECT 1 FROM payments p WHERE p.billing_cycle_id = b.id
			AND p.created_at >= {from_ts} AND p.created_at < {to_ts})`},
	{ID: "revenue.collected", Name: "Collected", Domain: "commerce", Kind: Money, Permission: identity.PermAnalyticsRevenueRead,
		Definition:  "Successful payments in the range, before refunds. Money received, not profit and not recognised revenue.",
		Sources:     []string{"payments.amount_minor", "payments.succeeded_at", "payments.currency"},
		Calculation: "sum(amount_minor) where succeeded_at in the range (later refunds do not change when it was collected)",
		Filters:     []string{"plan"}, Missing: "0", Currency: ttd, Owner: "Finance", Snapshot: true,
		Limitations: "Retries cannot double-count: only an attempt that succeeded has succeeded_at. Includes test-provider payments.",
		SQL: `SELECT coalesce(sum(p.amount_minor), 0) FROM payments p JOIN billing_cycles pb ON pb.id = p.billing_cycle_id JOIN subscriptions s ON s.id = pb.subscription_id
			WHERE p.succeeded_at >= {from_ts} AND p.succeeded_at < {to_ts} AND p.currency = 'TTD' AND ({plan} IS NULL OR s.plan_id = {plan})`},
	{ID: "revenue.collected_test", Name: "Collected through the test provider", Domain: "commerce", Kind: Money, Permission: identity.PermAnalyticsRevenueRead,
		Definition:  "The part of Collected taken by the simulated test payment provider. Not real money.",
		Sources:     []string{"payments.provider", "payments.amount_minor", "payments.succeeded_at"},
		Calculation: "Collected restricted to provider = 'testpay'", Filters: []string{"plan"}, Missing: "0", Currency: ttd, Owner: "Finance", Snapshot: true,
		Limitations: "None: the provider is recorded on every payment.",
		SQL: `SELECT coalesce(sum(p.amount_minor), 0) FROM payments p JOIN billing_cycles pb ON pb.id = p.billing_cycle_id JOIN subscriptions s ON s.id = pb.subscription_id
			WHERE p.provider = 'testpay' AND p.succeeded_at >= {from_ts} AND p.succeeded_at < {to_ts} AND p.currency = 'TTD' AND ({plan} IS NULL OR s.plan_id = {plan})`},
	{ID: "revenue.refunded", Name: "Refunded", Domain: "commerce", Kind: Money, Permission: identity.PermAnalyticsRevenueRead,
		Definition: "Refunds that succeeded in the range.", Sources: []string{"refunds.amount_minor", "refunds.status", "refunds.completed_at"},
		Calculation: "sum(amount_minor) where status = succeeded and completed_at in the range", Filters: []string{"plan"}, Missing: "0", Currency: ttd,
		Owner: "Finance", Snapshot: true, Limitations: "A refund is one row per request key, so a resubmitted form cannot double-count.",
		SQL: `SELECT coalesce(sum(r.amount_minor), 0) FROM refunds r JOIN payments p ON p.id = r.payment_id JOIN billing_cycles pb ON pb.id = p.billing_cycle_id JOIN subscriptions s ON s.id = pb.subscription_id
			WHERE r.status = 'succeeded' AND r.currency = 'TTD' AND r.completed_at >= {from_ts} AND r.completed_at < {to_ts} AND ({plan} IS NULL OR s.plan_id = {plan})`},
	{ID: "revenue.net_collected", Name: "Net collected", Domain: "commerce", Kind: Money, Permission: identity.PermAnalyticsRevenueRead,
		Definition: "Collected minus Refunded in the range. Cash view only: no revenue recognition policy exists.",
		Sources:    []string{"payments", "refunds"}, Calculation: "revenue.collected − revenue.refunded", Filters: []string{"plan"}, Missing: "0",
		Currency: ttd, Owner: "Finance", Snapshot: true, Limitations: "A refund in this range can relate to a payment collected earlier.",
		SQL: `SELECT (SELECT coalesce(sum(p.amount_minor), 0) FROM payments p JOIN billing_cycles pb ON pb.id = p.billing_cycle_id JOIN subscriptions s ON s.id = pb.subscription_id
			WHERE p.succeeded_at >= {from_ts} AND p.succeeded_at < {to_ts} AND p.currency = 'TTD' AND ({plan} IS NULL OR s.plan_id = {plan}))
			- (SELECT coalesce(sum(r.amount_minor), 0) FROM refunds r JOIN payments p ON p.id = r.payment_id JOIN billing_cycles pb ON pb.id = p.billing_cycle_id JOIN subscriptions s ON s.id = pb.subscription_id
			WHERE r.status = 'succeeded' AND r.currency = 'TTD' AND r.completed_at >= {from_ts} AND r.completed_at < {to_ts} AND ({plan} IS NULL OR s.plan_id = {plan}))`},
	{ID: "revenue.failed_payments", Name: "Failed payment attempts", Domain: "commerce", Kind: Count, Permission: identity.PermAnalyticsRevenueRead,
		Definition: "Payment attempts that failed, by when they were last updated in the range.", Sources: []string{"payments.status", "payments.updated_at"},
		Calculation: "count where status = failed", Filters: []string{"plan"}, Missing: "0", Owner: "Finance", Snapshot: true,
		Limitations: "Each retry is its own attempt; see revenue.cycles_failed for weeks that ended unpaid.",
		SQL: `SELECT count(*) FROM payments p JOIN billing_cycles pb ON pb.id = p.billing_cycle_id JOIN subscriptions s ON s.id = pb.subscription_id WHERE p.status = 'failed'
			AND p.updated_at >= {from_ts} AND p.updated_at < {to_ts} AND ({plan} IS NULL OR s.plan_id = {plan})`},
	{ID: "revenue.cycles_failed", Name: "Weeks left unpaid", Domain: "commerce", Kind: Count, Permission: identity.PermAnalyticsRevenueRead,
		Definition: "Billing cycles (weeks) whose payment finally failed, by due date in the range.", Sources: []string{"billing_cycles.status", "billing_cycles.due_at"},
		Calculation: "count where status = failed and due_at in the range", Filters: []string{"plan"}, Missing: "0", Owner: "Finance", Snapshot: false,
		Limitations: "A week the customer later paid moves to paid and leaves this count.",
		SQL: `SELECT count(*) FROM billing_cycles b JOIN subscriptions s ON s.id = b.subscription_id WHERE b.status = 'failed'
			AND b.due_at >= {from_ts} AND b.due_at < {to_ts} AND ({plan} IS NULL OR s.plan_id = {plan})`},
	{ID: "revenue.outstanding", Name: "Outstanding", Domain: "commerce", Kind: Money, Permission: identity.PermAnalyticsRevenueRead,
		Definition: "Billing cycles due in the range and not paid now (due or failed), in TTD.", Sources: []string{"billing_cycles.status", "billing_cycles.total_minor"},
		Calculation: "sum(total_minor) where status in (due, failed) and due_at in the range", Filters: []string{"plan"}, Missing: "0", Currency: ttd,
		Owner: "Finance", Snapshot: false, Limitations: "Point-in-time: computed from current statuses.",
		SQL: `SELECT coalesce(sum(b.total_minor), 0) FROM billing_cycles b JOIN subscriptions s ON s.id = b.subscription_id
			WHERE b.status IN ('due', 'failed') AND b.currency = 'TTD' AND b.due_at >= {from_ts} AND b.due_at < {to_ts} AND ({plan} IS NULL OR s.plan_id = {plan})`},
	{ID: "orders.count", Name: "Orders", Domain: "commerce", Kind: Count, Permission: identity.PermAnalyticsRevenueRead,
		Definition: "Orders made in the range, not cancelled.", Sources: []string{"orders.created_at", "orders.status"},
		Calculation: "count of live orders created in the range", Filters: []string{"plan"}, Missing: "0", Owner: "Commerce", Snapshot: true,
		Limitations: "Includes test-provider orders.",
		SQL: `SELECT count(*) FROM orders o JOIN subscriptions s ON s.id = o.subscription_id WHERE ` + liveOrder + `
			AND o.created_at >= {from_ts} AND o.created_at < {to_ts} AND ({plan} IS NULL OR s.plan_id = {plan})`},
	{ID: "orders.average_value", Name: "Average order value", Domain: "commerce", Kind: Money, Permission: identity.PermAnalyticsRevenueRead,
		Definition: "Ordered value divided by orders, in the range.", Sources: []string{"orders.total_minor"}, Calculation: "revenue.ordered_value / orders.count",
		Filters: []string{"plan"}, Missing: "Undefined (no value) when there are no orders", Currency: ttd, Owner: "Commerce", Snapshot: false,
		Limitations: "Rounded to the cent.",
		SQL: `SELECT round(avg(o.total_minor)) FROM orders o JOIN subscriptions s ON s.id = o.subscription_id WHERE ` + liveOrder + `
			AND o.currency = 'TTD' AND o.created_at >= {from_ts} AND o.created_at < {to_ts} AND ({plan} IS NULL OR s.plan_id = {plan})`},

	// ---------------------------------------------------------------- subscriptions
	{ID: "subscriptions.active", Name: "Active subscriptions", Domain: "subscriptions", Kind: Count, Permission: identity.PermAnalyticsRevenueRead,
		Definition:  "Subscriptions activated on or before the end of the range and not cancelled or expired by then (paused ones count as active).",
		Sources:     []string{"subscription_events.type", "subscription_events.created_at"},
		Calculation: "count of subscriptions with an 'activated' event ≤ end and no 'cancelled'/'expired' event ≤ end", Filters: []string{"plan"},
		Missing: "0", Owner: "Commerce", Snapshot: false, Limitations: "Reconstructed from the subscription event history, which exists for every subscription since Sprint 3.",
		SQL: `SELECT count(*) FROM subscriptions s WHERE ({plan} IS NULL OR s.plan_id = {plan})
			AND EXISTS (SELECT 1 FROM subscription_events e WHERE e.subscription_id = s.id AND e.type = 'activated' AND e.created_at < {to_ts})
			AND NOT EXISTS (SELECT 1 FROM subscription_events e WHERE e.subscription_id = s.id AND e.type IN ('cancelled', 'expired') AND e.created_at < {to_ts})`},
	{ID: "subscriptions.new", Name: "New subscriptions", Domain: "subscriptions", Kind: Count, Permission: identity.PermAnalyticsRevenueRead,
		Definition: "Subscriptions activated (first payment confirmed) in the range.", Sources: []string{"subscription_events"},
		Calculation: "count of 'activated' events in the range", Filters: []string{"plan"}, Missing: "0", Owner: "Commerce", Snapshot: true,
		Limitations: "A subscription that never paid is not counted.",
		SQL: `SELECT count(*) FROM subscription_events e JOIN subscriptions s ON s.id = e.subscription_id WHERE e.type = 'activated'
			AND e.created_at >= {from_ts} AND e.created_at < {to_ts} AND ({plan} IS NULL OR s.plan_id = {plan})`},
	{ID: "subscriptions.cancelled", Name: "Cancellations", Domain: "subscriptions", Kind: Count, Permission: identity.PermAnalyticsRevenueRead,
		Definition: "Subscriptions whose cancellation took effect in the range (explicit cancellation events; payment failure alone is never a cancellation).",
		Sources:    []string{"subscription_events"}, Calculation: "count of 'cancelled' events in the range", Filters: []string{"plan"}, Missing: "0",
		Owner: "Commerce", Snapshot: true, Limitations: "Scheduled cancellations that were withdrawn are not counted.",
		SQL: `SELECT count(*) FROM subscription_events e JOIN subscriptions s ON s.id = e.subscription_id WHERE e.type = 'cancelled'
			AND e.created_at >= {from_ts} AND e.created_at < {to_ts} AND ({plan} IS NULL OR s.plan_id = {plan})`},
	{ID: "subscriptions.expired", Name: "Expirations", Domain: "subscriptions", Kind: Count, Permission: identity.PermAnalyticsRevenueRead,
		Definition: "Subscriptions that expired (ended without payment) in the range.", Sources: []string{"subscription_events"},
		Calculation: "count of 'expired' events", Filters: []string{"plan"}, Missing: "0", Owner: "Commerce", Snapshot: true, Limitations: "None known.",
		SQL: `SELECT count(*) FROM subscription_events e JOIN subscriptions s ON s.id = e.subscription_id WHERE e.type = 'expired'
			AND e.created_at >= {from_ts} AND e.created_at < {to_ts} AND ({plan} IS NULL OR s.plan_id = {plan})`},
	{ID: "subscriptions.pauses", Name: "Pauses scheduled", Domain: "subscriptions", Kind: Count, Permission: identity.PermAnalyticsRevenueRead,
		Definition: "Pauses customers scheduled in the range.", Sources: []string{"subscription_events"}, Calculation: "count of 'pause_scheduled' events",
		Filters: []string{"plan"}, Missing: "0", Owner: "Commerce", Snapshot: true, Limitations: "None known.",
		SQL: `SELECT count(*) FROM subscription_events e JOIN subscriptions s ON s.id = e.subscription_id WHERE e.type = 'pause_scheduled'
			AND e.created_at >= {from_ts} AND e.created_at < {to_ts} AND ({plan} IS NULL OR s.plan_id = {plan})`},
	{ID: "subscriptions.renewals", Name: "Renewals paid", Domain: "subscriptions", Kind: Count, Permission: identity.PermAnalyticsRevenueRead,
		Definition: "Renewal weeks paid in the range (a paid renewal billing cycle).", Sources: []string{"billing_cycles.kind", "billing_cycles.status", "payments.succeeded_at"},
		Calculation: "count of renewal cycles with a payment that succeeded in the range", Filters: []string{"plan"}, Missing: "0", Owner: "Commerce", Snapshot: true,
		Limitations: "Free (promotion) renewal weeks have no payment and are not counted.",
		SQL: `SELECT count(DISTINCT b.id) FROM billing_cycles b JOIN payments p ON p.billing_cycle_id = b.id JOIN subscriptions s ON s.id = b.subscription_id
			WHERE b.kind = 'renewal' AND p.succeeded_at >= {from_ts} AND p.succeeded_at < {to_ts} AND ({plan} IS NULL OR s.plan_id = {plan})`},
	{ID: "subscriptions.plan_changes", Name: "Plan changes", Domain: "subscriptions", Kind: Count, Permission: identity.PermAnalyticsRevenueRead,
		Definition: "Plan changes customers made in the range.", Sources: []string{"subscription_changes.created_at"}, Calculation: "count",
		Filters: []string{}, Missing: "0", Owner: "Commerce", Snapshot: true, Limitations: "Counts requests, by when they were made.",
		SQL: `SELECT count(*) FROM subscription_changes c WHERE c.created_at >= {from_ts} AND c.created_at < {to_ts}`},

	// ---------------------------------------------------------------- customers
	{ID: "customers.new", Name: "New customer accounts", Domain: "customers", Kind: Count, Permission: identity.PermAnalyticsCustomersRead,
		Definition: "Customer accounts created in the range.", Sources: []string{"customers.created_at"}, Calculation: "count", Filters: []string{},
		Missing: "0", Owner: "Growth", Snapshot: true, Limitations: "Includes demo and test accounts.",
		SQL: `SELECT count(*) FROM customers c WHERE c.created_at >= {from_ts} AND c.created_at < {to_ts}`},
	{ID: "customers.first_paid", Name: "First-time payers", Domain: "customers", Kind: Count, Permission: identity.PermAnalyticsCustomersRead,
		Definition: "Customers whose first successful payment was in the range.", Sources: []string{"payments.customer_id", "payments.succeeded_at"},
		Calculation: "count of customers with min(succeeded_at) in the range", Filters: []string{}, Missing: "0", Owner: "Growth", Snapshot: true,
		Limitations: "Includes test-provider payments.",
		SQL: `SELECT count(*) FROM (SELECT customer_id, min(succeeded_at) AS first FROM payments WHERE succeeded_at IS NOT NULL GROUP BY customer_id) f
			WHERE f.first >= {from_ts} AND f.first < {to_ts}`},
	{ID: "customers.repeat_rate", Name: "Repeat ordering", Domain: "customers", Kind: Ratio, Permission: identity.PermAnalyticsCustomersRead,
		Definition: "Of customers with an order in the range, the share who also had an earlier order.", Sources: []string{"orders.customer_id", "orders.created_at"},
		Calculation: "customers with an order in range and an order before it ÷ customers with an order in range", Filters: []string{},
		Missing: "Undefined when no customer ordered in the range", Owner: "Growth", Snapshot: false, Limitations: "Cancelled and pending orders excluded.",
		SQL: `WITH c AS (SELECT DISTINCT o.customer_id FROM orders o WHERE ` + liveOrder + ` AND o.created_at >= {from_ts} AND o.created_at < {to_ts})
			SELECT CASE WHEN count(*) = 0 THEN NULL ELSE count(*) FILTER (WHERE EXISTS (SELECT 1 FROM orders o WHERE o.customer_id = c.customer_id
			AND ` + liveOrder + ` AND o.created_at < {from_ts}))::numeric / count(*) END FROM c`},

	// ---------------------------------------------------------------- meals
	{ID: "meals.kits_ordered", Name: "Meal kits ordered", Domain: "orders", Kind: Count, Permission: identity.PermAnalyticsProductionRead,
		Definition: "Kits in live orders for order weeks starting in the range (confirmed demand; carts and drafts never become orders).",
		Sources:    []string{"order_items.quantity", "orders.week_start", "orders.status"}, Calculation: "sum(quantity) over live orders by week_start",
		Filters: []string{"plan", "meal"}, Missing: "0", Owner: "Culinary", Snapshot: false,
		Limitations: "Counted by order week, not by order date. Includes test-provider orders.",
		SQL: `SELECT coalesce(sum(i.quantity), 0) FROM order_items i JOIN orders o ON o.id = i.order_id JOIN subscriptions s ON s.id = o.subscription_id
			WHERE ` + liveOrder + ` AND o.week_start BETWEEN {from} AND {to} AND ({plan} IS NULL OR s.plan_id = {plan}) AND ({meal} IS NULL OR i.meal_id = {meal})`},

	// ---------------------------------------------------------------- production
	{ID: "production.planned_kits", Name: "Kits planned", Domain: "production", Kind: Count, Permission: identity.PermAnalyticsProductionRead,
		Definition: "Kits planned for batches with a production date in the range (cancelled batches excluded).", Sources: []string{"production_batches.planned_kits"},
		Calculation: "sum(planned_kits)", Filters: []string{"meal"}, Missing: "0", Owner: "Kitchen", Snapshot: true, Limitations: "None known.",
		SQL: `SELECT coalesce(sum(b.planned_kits), 0) FROM production_batches b WHERE b.status <> 'cancelled' AND b.production_date BETWEEN {from} AND {to}
			AND ({meal} IS NULL OR b.meal_id = {meal})`},
	{ID: "production.produced_kits", Name: "Kits produced", Domain: "production", Kind: Count, Permission: identity.PermAnalyticsProductionRead,
		Definition: "Kits assembled (before rejection) for batches in the range.", Sources: []string{"production_yield_records.produced_kits"},
		Calculation: "sum(produced_kits)", Filters: []string{"meal"}, Missing: "0 for batches without recorded output", Owner: "Kitchen", Snapshot: true,
		Limitations: "Batches without recorded output contribute nothing.",
		SQL: `SELECT coalesce(sum(y.produced_kits), 0) FROM production_yield_records y JOIN production_batches b ON b.id = y.batch_id
			WHERE b.status <> 'cancelled' AND b.production_date BETWEEN {from} AND {to} AND ({meal} IS NULL OR b.meal_id = {meal})`},
	{ID: "production.accepted_kits", Name: "Kits accepted", Domain: "production", Kind: Count, Permission: identity.PermAnalyticsProductionRead,
		Definition: "Produced minus rejected. Reworked kits are part of accepted and are not added again.", Sources: []string{"production_yield_records.accepted_kits"},
		Calculation: "sum(accepted_kits)", Filters: []string{"meal"}, Missing: "0", Owner: "Kitchen", Snapshot: true, Limitations: "None known.",
		SQL: `SELECT coalesce(sum(y.accepted_kits), 0) FROM production_yield_records y JOIN production_batches b ON b.id = y.batch_id
			WHERE b.status <> 'cancelled' AND b.production_date BETWEEN {from} AND {to} AND ({meal} IS NULL OR b.meal_id = {meal})`},
	{ID: "production.rejected_kits", Name: "Kits rejected", Domain: "production", Kind: Count, Permission: identity.PermAnalyticsProductionRead,
		Definition: "Kits rejected at output.", Sources: []string{"production_yield_records.rejected_kits"}, Calculation: "sum(rejected_kits)",
		Filters: []string{"meal"}, Missing: "0", Owner: "Kitchen", Snapshot: true, Limitations: "None known.",
		SQL: `SELECT coalesce(sum(y.rejected_kits), 0) FROM production_yield_records y JOIN production_batches b ON b.id = y.batch_id
			WHERE b.status <> 'cancelled' AND b.production_date BETWEEN {from} AND {to} AND ({meal} IS NULL OR b.meal_id = {meal})`},
	{ID: "production.reworked_kits", Name: "Kits reworked", Domain: "production", Kind: Count, Permission: identity.PermAnalyticsProductionRead,
		Definition: "Kits fixed and accepted after rework (already inside Kits accepted).", Sources: []string{"production_yield_records.reworked_kits"},
		Calculation: "sum(reworked_kits)", Filters: []string{"meal"}, Missing: "0", Owner: "Kitchen", Snapshot: true, Limitations: "Never added to accepted.",
		SQL: `SELECT coalesce(sum(y.reworked_kits), 0) FROM production_yield_records y JOIN production_batches b ON b.id = y.batch_id
			WHERE b.status <> 'cancelled' AND b.production_date BETWEEN {from} AND {to} AND ({meal} IS NULL OR b.meal_id = {meal})`},
	{ID: "production.completion_rate", Name: "Batch completion", Domain: "production", Kind: Ratio, Permission: identity.PermAnalyticsProductionRead,
		Definition: "Completed batches ÷ batches (not cancelled) with a production date in the range.", Sources: []string{"production_batches.status"},
		Calculation: "completed / not cancelled", Filters: []string{"meal"}, Missing: "Undefined with no batches", Owner: "Kitchen", Snapshot: false,
		Limitations: "Batches still in progress count against completion until they complete.",
		SQL: `SELECT CASE WHEN count(*) = 0 THEN NULL ELSE count(*) FILTER (WHERE b.status = 'completed')::numeric / count(*) END
			FROM production_batches b WHERE b.status <> 'cancelled' AND b.production_date BETWEEN {from} AND {to} AND ({meal} IS NULL OR b.meal_id = {meal})`},
	{ID: "production.batch_hours", Name: "Batch completion time", Domain: "production", Kind: Hours, Permission: identity.PermAnalyticsProductionRead,
		Definition: "Average hours from a batch starting to completing, for batches completed in the range.", Sources: []string{"production_batches.started_at", "production_batches.completed_at"},
		Calculation: "avg(completed_at − started_at)", Filters: []string{"meal"}, Missing: "Undefined with no completed batches", Owner: "Kitchen", Snapshot: false,
		Limitations: "Demo batches are completed within minutes and distort this figure.",
		SQL: `SELECT round((avg(extract(epoch FROM b.completed_at - b.started_at)) / 3600)::numeric, 2) FROM production_batches b
			WHERE b.completed_at >= {from_ts} AND b.completed_at < {to_ts} AND b.started_at IS NOT NULL AND ({meal} IS NULL OR b.meal_id = {meal})`},
	{ID: "production.quality_holds", Name: "Quality holds", Domain: "production", Kind: Count, Permission: identity.PermAnalyticsProductionRead,
		Definition: "Quality holds placed in the range.", Sources: []string{"quality_holds.created_at"}, Calculation: "count", Filters: []string{},
		Missing: "0", Owner: "Quality", Snapshot: true, Limitations: "None known.",
		SQL: `SELECT count(*) FROM quality_holds h WHERE h.created_at >= {from_ts} AND h.created_at < {to_ts}`},
	{ID: "production.waste_records", Name: "Waste records", Domain: "production", Kind: Count, Permission: identity.PermAnalyticsProductionRead,
		Definition: "Kitchen waste records made in the range (quantities have different units and are not added).", Sources: []string{"production_waste.created_at"},
		Calculation: "count", Filters: []string{}, Missing: "0", Owner: "Kitchen", Snapshot: true, Limitations: "Units differ by record; see the production report for quantities.",
		SQL: `SELECT count(*) FROM production_waste w WHERE w.created_at >= {from_ts} AND w.created_at < {to_ts}`},

	// ---------------------------------------------------------------- procurement
	{ID: "procurement.po_value", Name: "Purchase orders issued", Domain: "procurement", Kind: Money, Permission: identity.PermAnalyticsProcurementRead,
		Definition:  "Value of purchase orders issued to suppliers in the range (lines plus delivery fee and tax), TTD only.",
		Sources:     []string{"purchase_orders.issued_at", "purchase_order_lines.line_total_minor", "purchase_orders.delivery_fee_minor", "purchase_orders.tax_minor"},
		Calculation: "sum over issued POs", Filters: []string{"supplier"}, Missing: "0", Currency: ttd, Owner: "Procurement", Snapshot: true,
		Limitations: "Spend committed, not paid: supplier invoices are not recorded.",
		SQL: `SELECT coalesce(sum((SELECT coalesce(sum(l.line_total_minor), 0) FROM purchase_order_lines l WHERE l.po_id = p.id) + p.delivery_fee_minor + p.tax_minor), 0)
			FROM purchase_orders p WHERE p.issued_at >= {from_ts} AND p.issued_at < {to_ts} AND p.currency = 'TTD' AND ({supplier} IS NULL OR p.supplier_id = {supplier})`},
	{ID: "procurement.po_count", Name: "Purchase orders issued (count)", Domain: "procurement", Kind: Count, Permission: identity.PermAnalyticsProcurementRead,
		Definition: "Purchase orders issued in the range.", Sources: []string{"purchase_orders.issued_at"}, Calculation: "count", Filters: []string{"supplier"},
		Missing: "0", Owner: "Procurement", Snapshot: true, Limitations: "None known.",
		SQL: `SELECT count(*) FROM purchase_orders p WHERE p.issued_at >= {from_ts} AND p.issued_at < {to_ts} AND ({supplier} IS NULL OR p.supplier_id = {supplier})`},
	{ID: "procurement.cycle_days", Name: "Order to first delivery", Domain: "procurement", Kind: Days, Permission: identity.PermAnalyticsProcurementRead,
		Definition: "Average days from issuing a purchase order to its first receipt, for orders first received in the range.",
		Sources:    []string{"purchase_orders.issued_at", "goods_receipts.received_at"}, Calculation: "avg(first received_at − issued_at)", Filters: []string{"supplier"},
		Missing: "Undefined with no deliveries", Owner: "Procurement", Snapshot: false, Limitations: "Supplier lead time as observed, not as quoted.",
		SQL: `SELECT round((avg(extract(epoch FROM f.first - p.issued_at)) / 86400)::numeric, 1) FROM purchase_orders p
			JOIN (SELECT po_id, min(received_at) AS first FROM goods_receipts GROUP BY po_id) f ON f.po_id = p.id
			WHERE f.first >= {from_ts} AND f.first < {to_ts} AND p.issued_at IS NOT NULL AND ({supplier} IS NULL OR p.supplier_id = {supplier})`},
	{ID: "procurement.on_time_rate", Name: "Received on time", Domain: "procurement", Kind: Ratio, Permission: identity.PermAnalyticsProcurementRead,
		Definition: "Of orders first received in the range that had an expected date, the share first received on or before it (local date).",
		Sources:    []string{"purchase_orders.expected_delivery", "goods_receipts.received_at"}, Calculation: "on time ÷ received with an expected date",
		Filters: []string{"supplier"}, Missing: "Undefined when no received order had an expected date", Owner: "Procurement", Snapshot: false,
		Limitations: "Orders without an expected date are not measurable and are excluded.",
		SQL: `SELECT CASE WHEN count(*) = 0 THEN NULL ELSE count(*) FILTER (WHERE (f.first AT TIME ZONE 'America/Port_of_Spain')::date <= p.expected_delivery)::numeric / count(*) END
			FROM purchase_orders p JOIN (SELECT po_id, min(received_at) AS first FROM goods_receipts GROUP BY po_id) f ON f.po_id = p.id
			WHERE f.first >= {from_ts} AND f.first < {to_ts} AND p.expected_delivery IS NOT NULL AND ({supplier} IS NULL OR p.supplier_id = {supplier})`},
	{ID: "procurement.rejected_packs", Name: "Packs rejected at receiving", Domain: "procurement", Kind: Num, Permission: identity.PermAnalyticsProcurementRead,
		Definition: "Packs rejected on delivery in the range.", Sources: []string{"goods_receipt_lines.rejected_packs"}, Calculation: "sum(rejected_packs)",
		Filters: []string{"supplier"}, Missing: "0", Owner: "Procurement", Snapshot: true, Limitations: "Packs of different products are added as counts of packs.",
		SQL: `SELECT coalesce(sum(l.rejected_packs), 0) FROM goods_receipt_lines l JOIN goods_receipts g ON g.id = l.receipt_id JOIN purchase_orders p ON p.id = g.po_id
			WHERE g.received_at >= {from_ts} AND g.received_at < {to_ts} AND ({supplier} IS NULL OR p.supplier_id = {supplier})`},
	{ID: "procurement.overdue_orders", Name: "Overdue purchase orders", Domain: "procurement", Kind: Count, Permission: identity.PermAnalyticsProcurementRead,
		Definition: "Issued or partly received orders whose expected date is before the end of the range and that are not fully received now.",
		Sources:    []string{"purchase_orders.status", "purchase_orders.expected_delivery"}, Calculation: "count", Filters: []string{"supplier"}, Missing: "0",
		Owner: "Procurement", Snapshot: false, Limitations: "Point in time.",
		SQL: `SELECT count(*) FROM purchase_orders p WHERE p.status IN ('issued', 'partially_received') AND p.expected_delivery < {to}
			AND ({supplier} IS NULL OR p.supplier_id = {supplier})`},

	// ---------------------------------------------------------------- inventory
	{ID: "inventory.value_on_hand", Name: "Stock value on hand", Domain: "inventory", Kind: Money, Permission: identity.PermAnalyticsInventoryRead,
		Definition:  "Now: on-hand quantity × recorded unit cost for usable or held lots with a cost, TTD only. A current figure, not for the range.",
		Sources:     []string{"inventory_lots.on_hand", "inventory_lots.unit_cost_minor", "inventory_lots.received_quantity"},
		Calculation: "sum(on_hand × unit_cost_minor) — the unit cost is per base unit as recorded at receiving", Filters: []string{}, Missing: "0; lots without cost are counted in inventory.lots_without_cost",
		Currency: ttd, Owner: "Stores", Snapshot: false, Limitations: "Lots without a recorded cost are excluded, so this understates value when costs are missing.",
		SQL: `SELECT coalesce(round(sum(l.on_hand * l.unit_cost_minor)), 0) FROM inventory_lots l WHERE l.unit_cost_minor IS NOT NULL
			AND l.currency = 'TTD' AND l.on_hand > 0 AND l.status <> 'rejected'`},
	{ID: "inventory.lots_without_cost", Name: "Lots without a cost", Domain: "inventory", Kind: Count, Permission: identity.PermAnalyticsInventoryRead,
		Definition: "Now: lots with stock on hand and no recorded unit cost.", Sources: []string{"inventory_lots"}, Calculation: "count", Filters: []string{},
		Missing: "0", Owner: "Stores", Snapshot: false, Limitations: "Point in time.",
		SQL: `SELECT count(*) FROM inventory_lots l WHERE l.on_hand > 0 AND l.unit_cost_minor IS NULL`},
	{ID: "inventory.lots_quarantined", Name: "Lots quarantined or on hold", Domain: "inventory", Kind: Count, Permission: identity.PermAnalyticsInventoryRead,
		Definition: "Now: lots with stock that cannot be used (quarantined or on hold).", Sources: []string{"inventory_lots.status"}, Calculation: "count",
		Filters: []string{}, Missing: "0", Owner: "Stores", Snapshot: false, Limitations: "Point in time.",
		SQL: `SELECT count(*) FROM inventory_lots l WHERE l.on_hand > 0 AND l.status IN ('quarantined', 'on_hold')`},
	{ID: "inventory.lots_expired", Name: "Lots expired", Domain: "inventory", Kind: Count, Permission: identity.PermAnalyticsInventoryRead,
		Definition: "Now: lots with stock whose recorded expiry date has passed (local date).", Sources: []string{"inventory_lots.expiry_date"}, Calculation: "count",
		Filters: []string{}, Missing: "0", Owner: "Stores", Snapshot: false, Limitations: "Lots without a recorded expiry are not counted here.",
		SQL: `SELECT count(*) FROM inventory_lots l WHERE l.on_hand > 0 AND l.expiry_date < (now() AT TIME ZONE 'America/Port_of_Spain')::date`},
	{ID: "inventory.issue_movements", Name: "Issues to production", Domain: "inventory", Kind: Count, Permission: identity.PermAnalyticsInventoryRead,
		Definition: "Ledger issue movements in the range.", Sources: []string{"inventory_movements.type", "inventory_movements.created_at"}, Calculation: "count where type = issue",
		Filters: []string{}, Missing: "0", Owner: "Stores", Snapshot: true, Limitations: "Counts movements; quantities differ by unit.",
		SQL: `SELECT count(*) FROM inventory_movements m WHERE m.type = 'issue' AND m.created_at >= {from_ts} AND m.created_at < {to_ts}`},
	{ID: "inventory.waste_movements", Name: "Store waste", Domain: "inventory", Kind: Count, Permission: identity.PermAnalyticsInventoryRead,
		Definition: "Ledger waste movements (spoiled or damaged in storage) in the range.", Sources: []string{"inventory_movements"}, Calculation: "count where type = waste",
		Filters: []string{}, Missing: "0", Owner: "Stores", Snapshot: true, Limitations: "Kitchen waste is a production record, not counted here.",
		SQL: `SELECT count(*) FROM inventory_movements m WHERE m.type = 'waste' AND m.created_at >= {from_ts} AND m.created_at < {to_ts}`},

	// ---------------------------------------------------------------- delivery
	{ID: "delivery.scheduled", Name: "Deliveries scheduled", Domain: "delivery", Kind: Count, Permission: identity.PermAnalyticsDeliveryRead,
		Definition: "Deliveries with a confirmed service date in the range, not cancelled.", Sources: []string{"deliveries.service_date", "deliveries.schedule_confirmed_at"},
		Calculation: "count", Filters: []string{"zone"}, Missing: "0", Owner: "Dispatch", Snapshot: false, Limitations: "None known.",
		SQL: `SELECT count(*) FROM deliveries d WHERE d.schedule_confirmed_at IS NOT NULL AND d.status <> 'cancelled' AND d.service_date BETWEEN {from} AND {to}
			AND ({zone} IS NULL OR d.zone_id = {zone})`},
	{ID: "delivery.delivered", Name: "Delivered", Domain: "delivery", Kind: Count, Permission: identity.PermAnalyticsDeliveryRead,
		Definition: "Of those, deliveries completed with accepted proof and not disputed (corrected outcomes count under their corrected status).",
		Sources:    []string{"deliveries.status", "order_delivery_outcomes.state"}, Calculation: "count where status = delivered and the order's latest outcome is not disputed",
		Filters: []string{"zone"}, Missing: "0", Owner: "Dispatch", Snapshot: false, Limitations: "None known.",
		SQL: `SELECT count(*) FROM deliveries d WHERE d.schedule_confirmed_at IS NOT NULL AND d.status = 'delivered' AND d.service_date BETWEEN {from} AND {to}
			AND ({zone} IS NULL OR d.zone_id = {zone}) AND coalesce((SELECT x.state FROM order_delivery_outcomes x WHERE x.order_id = d.order_id ORDER BY x.id DESC LIMIT 1), '') <> 'disputed'`},
	{ID: "delivery.failed_attempts", Name: "Failed attempts", Domain: "delivery", Kind: Count, Permission: identity.PermAnalyticsDeliveryRead,
		Definition: "Recorded failed attempts for deliveries scheduled in the range (a delivery can fail more than once).", Sources: []string{"delivery_tracking_events.to_status"},
		Calculation: "count of tracking events to delivery_failed", Filters: []string{"zone"}, Missing: "0", Owner: "Dispatch", Snapshot: false, Limitations: "None known.",
		SQL: `SELECT count(*) FROM delivery_tracking_events t JOIN deliveries d ON d.id = t.delivery_id WHERE t.to_status = 'delivery_failed'
			AND d.service_date BETWEEN {from} AND {to} AND ({zone} IS NULL OR d.zone_id = {zone})`},
	{ID: "delivery.first_attempt_rate", Name: "First-attempt success", Domain: "delivery", Kind: Ratio, Permission: identity.PermAnalyticsDeliveryRead,
		Definition: "Delivered on attempt 1 (and not disputed) ÷ deliveries scheduled in the range.", Sources: []string{"deliveries.attempt", "deliveries.status"},
		Calculation: "first-attempt delivered / scheduled", Filters: []string{"zone"}, Missing: "Undefined with no scheduled deliveries", Owner: "Dispatch", Snapshot: false,
		Limitations: "Deliveries not yet attempted count against the rate until they are.",
		SQL: `SELECT CASE WHEN count(*) = 0 THEN NULL ELSE count(*) FILTER (WHERE d.status = 'delivered' AND d.attempt = 1
			AND coalesce((SELECT x.state FROM order_delivery_outcomes x WHERE x.order_id = d.order_id ORDER BY x.id DESC LIMIT 1), '') <> 'disputed')::numeric / count(*) END
			FROM deliveries d WHERE d.schedule_confirmed_at IS NOT NULL AND d.status <> 'cancelled' AND d.service_date BETWEEN {from} AND {to} AND ({zone} IS NULL OR d.zone_id = {zone})`},
	{ID: "delivery.on_time_rate", Name: "On time", Domain: "delivery", Kind: Ratio, Permission: identity.PermAnalyticsDeliveryRead,
		Definition: "Of delivered deliveries with an approved promised window, the share delivered on the date and inside the window. Without a promised window it is not measurable.",
		Sources:    []string{"deliveries.delivered_at", "deliveries.service_date", "delivery_windows"}, Calculation: "on time ÷ delivered with an approved window",
		Filters: []string{"zone"}, Missing: "Undefined when no delivered delivery had a promised window", Owner: "Dispatch", Snapshot: false,
		Limitations: "Deliveries with only a date and no window are excluded.",
		SQL: `SELECT CASE WHEN count(*) = 0 THEN NULL ELSE count(*) FILTER (WHERE (d.delivered_at AT TIME ZONE 'America/Port_of_Spain')::date = d.service_date
			AND (d.delivered_at AT TIME ZONE 'America/Port_of_Spain')::time BETWEEN w.starts_at AND w.ends_at)::numeric / count(*) END
			FROM deliveries d JOIN delivery_windows w ON w.id = d.window_id AND w.status = 'approved'
			WHERE d.status = 'delivered' AND d.service_date BETWEEN {from} AND {to} AND ({zone} IS NULL OR d.zone_id = {zone})`},
	{ID: "delivery.returned", Name: "Returned", Domain: "delivery", Kind: Count, Permission: identity.PermAnalyticsDeliveryRead,
		Definition: "Deliveries scheduled in the range whose packages came back.", Sources: []string{"deliveries.status"}, Calculation: "count where status = returned",
		Filters: []string{"zone"}, Missing: "0", Owner: "Dispatch", Snapshot: false, Limitations: "None known.",
		SQL: `SELECT count(*) FROM deliveries d WHERE d.status = 'returned' AND d.service_date BETWEEN {from} AND {to} AND ({zone} IS NULL OR d.zone_id = {zone})`},
	{ID: "delivery.disputed", Name: "Disputed outcomes", Domain: "delivery", Kind: Count, Permission: identity.PermAnalyticsDeliveryRead,
		Definition: "Now: orders whose delivered outcome commerce records as disputed (corrected and not delivered again).", Sources: []string{"order_delivery_outcomes"},
		Calculation: "count of orders whose latest outcome entry is disputed", Filters: []string{}, Missing: "0", Owner: "Dispatch", Snapshot: false, Limitations: "Point in time.",
		SQL: `SELECT count(*) FROM (SELECT DISTINCT ON (order_id) state FROM order_delivery_outcomes ORDER BY order_id, id DESC) x WHERE x.state = 'disputed'`},
	{ID: "delivery.cost_actual", Name: "Actual delivery cost", Domain: "delivery", Kind: Money, Permission: identity.PermAnalyticsDeliveryRead,
		Definition: "Recorded actual partner costs plus adjustments for deliveries or runs in the range. Estimated and contracted figures are never mixed in.",
		Sources:    []string{"delivery_cost_records.kind", "delivery_cost_records.amount_minor"}, Calculation: "sum where kind in (actual, adjustment), currency TTD",
		Filters: []string{"zone"}, Missing: "0 when no costs were recorded (which is not the same as no cost)", Currency: ttd, Owner: "Dispatch", Snapshot: false,
		Limitations: "Only what staff recorded; there is no carrier billing feed.",
		SQL: `SELECT coalesce(sum(c.amount_minor), 0) FROM delivery_cost_records c LEFT JOIN deliveries d ON d.id = c.delivery_id LEFT JOIN dispatch_runs r ON r.id = c.run_id
			WHERE c.kind IN ('actual', 'adjustment') AND c.currency = 'TTD' AND coalesce(d.service_date, r.service_date) BETWEEN {from} AND {to}
			AND ({zone} IS NULL OR d.zone_id = {zone} OR r.zone_id = {zone})`},
	{ID: "operations.open_exceptions", Name: "Open operational exceptions", Domain: "operations", Kind: Count, Permission: identity.PermAnalyticsExecutiveRead,
		Definition: "Now: open fulfillment and delivery exceptions, open quality holds, and open support cases.",
		Sources:    []string{"delivery_exceptions.status", "quality_holds.status", "support_cases.status"}, Calculation: "sum of the three counts", Filters: []string{},
		Missing: "0", Owner: "Operations", Snapshot: false, Limitations: "Point in time.",
		SQL: `SELECT (SELECT count(*) FROM delivery_exceptions WHERE status IN ('open', 'in_progress')) + (SELECT count(*) FROM quality_holds WHERE status = 'open')
			+ (SELECT count(*) FROM support_cases WHERE status IN ('open', 'in_progress', 'waiting_for_customer'))`},
}

// Metrics returns the registry in id order.
func Metrics() []Metric {
	out := append([]Metric(nil), registry...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

var byID = func() map[string]*Metric {
	m := map[string]*Metric{}
	for i := range registry {
		if _, dup := m[registry[i].ID]; dup {
			panic("analytics: duplicate metric " + registry[i].ID)
		}
		m[registry[i].ID] = &registry[i]
	}
	return m
}()

// Get returns a metric by id.
func Get(id string) (*Metric, bool) {
	m, ok := byID[id]
	return m, ok
}

var placeholder = regexp.MustCompile(`\{(from_ts|to_ts|from|to|plan|meal|zone|supplier)\}`)

var casts = map[string]string{"from_ts": "timestamptz", "to_ts": "timestamptz", "from": "date", "to": "date",
	"plan": "uuid", "meal": "uuid", "zone": "uuid", "supplier": "uuid"}

// bind turns placeholders into positional parameters, binding only the
// ones the metric uses (so an unused filter never needs a type).
func bind(sql string, values map[string]any) (string, []any) {
	pos := map[string]int{}
	var args []any
	out := placeholder.ReplaceAllStringFunc(sql, func(p string) string {
		name := strings.Trim(p, "{}")
		n, ok := pos[name]
		if !ok {
			args = append(args, values[name])
			n = len(args)
			pos[name] = n
		}
		return fmt.Sprintf("$%d::%s", n, casts[name])
	})
	return out, args
}
