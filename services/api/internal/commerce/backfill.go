package commerce

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/platform/db"
)

// BackfillEvents publishes, as domain events, the commerce facts recorded
// before commerce published events (Sprint 9). Every statement uses the same
// type, aggregate and dedupe key as the live code path and keeps the original
// time, so running it twice, or after live events exist, adds nothing, and
// analytics refreshes the day each fact happened. It returns how many events
// were added.
func BackfillEvents(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	statements := []string{
		// Subscription history: one event per history row.
		`INSERT INTO domain_events (type, aggregate_type, aggregate_id, dedupe_key, payload, occurred_at)
		 SELECT 'subscription.' || e.type, 'subscription', e.subscription_id, e.id::text,
		        jsonb_build_object('subscription_id', s.id, 'customer_id', s.customer_id, 'plan_id', s.plan_id, 'history_id', e.id,
		                           'from_status', e.from_status, 'to_status', e.to_status,
		                           'effective_week', to_char(e.effective_week, 'YYYY-MM-DD'), 'backfilled', true),
		        e.created_at
		 FROM subscription_events e JOIN subscriptions s ON s.id = e.subscription_id
		 WHERE e.type ~ '^[a-z_]+$'
		 ON CONFLICT (type, aggregate_id, dedupe_key) DO NOTHING`,
		// Renewals: further weeks paid on a running subscription.
		`INSERT INTO domain_events (type, aggregate_type, aggregate_id, dedupe_key, payload, occurred_at)
		 SELECT 'subscription.renewed', 'subscription', b.subscription_id, b.id::text,
		        jsonb_build_object('subscription_id', b.subscription_id, 'customer_id', b.customer_id, 'billing_cycle_id', b.id,
		                           'week', to_char(b.period_start, 'YYYY-MM-DD'), 'total_minor', b.total_minor, 'currency', b.currency,
		                           'backfilled', true),
		        b.paid_at
		 FROM billing_cycles b WHERE b.kind = 'renewal' AND b.status = 'paid' AND b.paid_at IS NOT NULL
		 ON CONFLICT (type, aggregate_id, dedupe_key) DO NOTHING`,
		// Payment outcomes (refund history rows are covered by refund events).
		`INSERT INTO domain_events (type, aggregate_type, aggregate_id, dedupe_key, payload, occurred_at)
		 SELECT 'payment.' || h.to_status, 'payment', p.id, h.to_status,
		        jsonb_build_object('payment_id', p.id, 'billing_cycle_id', p.billing_cycle_id, 'customer_id', p.customer_id, 'kind', p.kind,
		                           'from_status', h.from_status, 'amount_minor', p.amount_minor, 'currency', p.currency,
		                           'provider', p.provider, 'test', p.provider = 'testpay', 'source', h.source, 'backfilled', true),
		        h.created_at
		 FROM payment_status_history h JOIN payments p ON p.id = h.payment_id
		 WHERE h.to_status IN ('succeeded', 'failed', 'cancelled', 'pending', 'requires_action', 'processing') AND h.source <> 'refund'
		 ON CONFLICT (type, aggregate_id, dedupe_key) DO NOTHING`,
		// Refunds that reached a final state.
		`INSERT INTO domain_events (type, aggregate_type, aggregate_id, dedupe_key, payload, occurred_at)
		 SELECT 'refund.' || r.status, 'refund', r.id, 'final',
		        jsonb_build_object('refund_id', r.id, 'payment_id', r.payment_id, 'customer_id', p.customer_id, 'amount_minor', r.amount_minor,
		                           'currency', r.currency, 'provider', p.provider, 'backfilled', true),
		        coalesce(r.completed_at, r.created_at)
		 FROM refunds r JOIN payments p ON p.id = r.payment_id WHERE r.status IN ('succeeded', 'failed')
		 ON CONFLICT (type, aggregate_id, dedupe_key) DO NOTHING`,
		// Orders: created (confirmed) and every later status change.
		`INSERT INTO domain_events (type, aggregate_type, aggregate_id, dedupe_key, payload, occurred_at)
		 SELECT 'order.confirmed', 'order', o.id, 'confirmed',
		        jsonb_build_object('order_id', o.id, 'number', o.number, 'customer_id', o.customer_id, 'subscription_id', o.subscription_id,
		                           'billing_cycle_id', o.billing_cycle_id, 'week', to_char(o.week_start, 'YYYY-MM-DD'), 'meals', o.meals_count,
		                           'total_minor', o.total_minor, 'currency', o.currency, 'backfilled', true),
		        o.created_at
		 FROM orders o
		 ON CONFLICT (type, aggregate_id, dedupe_key) DO NOTHING`,
		`INSERT INTO domain_events (type, aggregate_type, aggregate_id, dedupe_key, payload, occurred_at)
		 SELECT 'order.status_changed', 'order', h.order_id, h.id::text,
		        jsonb_build_object('order_id', h.order_id, 'from_status', h.from_status, 'to_status', h.to_status, 'reason', h.reason,
		                           'backfilled', true),
		        h.created_at
		 FROM order_status_history h WHERE h.from_status IS NOT NULL
		 ON CONFLICT (type, aggregate_id, dedupe_key) DO NOTHING`,
	}
	var total int64
	err := db.InTx(ctx, pool, func(tx pgx.Tx) error {
		for _, sql := range statements {
			tag, err := tx.Exec(ctx, sql)
			if err != nil {
				return err
			}
			total += tag.RowsAffected()
		}
		return nil
	})
	return total, err
}
