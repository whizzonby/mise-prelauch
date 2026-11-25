package server_test

import (
	"context"
	"net/http"
	"testing"

	"mise.tt/api/internal/analytics"
	"mise.tt/api/internal/automation"
	"mise.tt/api/internal/commerce"
	"mise.tt/api/internal/payments/testpay"
	"mise.tt/api/internal/platform/consumer"
)

// eventCount counts outbox events of a type, optionally for one aggregate.
func (h *harness) eventCount(typ string, aggregate ...string) int {
	if len(aggregate) > 0 {
		return h.count(`SELECT count(*) FROM domain_events WHERE type = $1 AND aggregate_id = $2`, typ, aggregate[0])
	}
	return h.count(`SELECT count(*) FROM domain_events WHERE type = $1`, typ)
}

// Commerce state changes publish outbox events in the same transaction:
// one per real transition, never twice for a duplicate webhook, a retried
// reconcile or a replayed backfill.
func TestCommerceEventsArePublishedOnceWithTheChange(t *testing.T) {
	s := newShop(t)
	h := s.h
	ctx := context.Background()

	// First week paid: created, activated, payment succeeded.
	tok, sub := s.active("events@example.com", testpay.CardApprove)
	for typ, want := range map[string]int{"subscription.created": 1, "subscription.activated": 1, "payment.succeeded": 1} {
		if got := h.eventCount(typ, sub); typ != "payment.succeeded" && got != want {
			t.Errorf("%s for the subscription = %d, want %d", typ, got, want)
		}
		if got := h.eventCount(typ); got != want {
			t.Errorf("%s = %d, want %d", typ, got, want)
		}
	}
	// Every subscription event matches a history row, and none was published
	// without the change it describes.
	if a, b := h.count(`SELECT count(*) FROM subscription_events`), h.count(`SELECT count(*) FROM domain_events WHERE type LIKE 'subscription.%' AND type <> 'subscription.renewed'`); a != b {
		t.Errorf("subscription history %d rows, events %d", a, b)
	}

	// The provider's webhook delivered again, and a reconcile, publish nothing new.
	var ref string
	if err := h.pool.QueryRow(ctx, `SELECT provider_ref FROM checkout_sessions LIMIT 1`).Scan(&ref); err != nil {
		t.Fatal(err)
	}
	var payload []byte
	if err := h.pool.QueryRow(ctx, `SELECT payload::text FROM payment_webhook_events LIMIT 1`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	h.wantStatus(h.raw("POST", "/api/v1/webhooks/payments/testpay", payload, h.com.Testpay.SignedHeader(payload, h.clock.Now())), http.StatusOK)
	h.drainJobs()
	if err := h.com.Payments.ReconcileCheckout(ctx, "testpay", ref, "reconcile", ""); err != nil {
		t.Fatal(err)
	}
	if n := h.eventCount("payment.succeeded"); n != 1 {
		t.Errorf("payment.succeeded after a duplicate webhook = %d, want 1", n)
	}
	var test bool
	if err := h.pool.QueryRow(ctx, `SELECT (payload->>'test')::bool FROM domain_events WHERE type = 'payment.succeeded'`).Scan(&test); err != nil || !test {
		t.Errorf("a test-provider payment must be labelled test (err %v)", err)
	}

	// The first week's meals are chosen, so the cutoff makes an order.
	week := s.weekCycle(tok, "2026-11-16")["id"].(string)
	h.wantStatus(h.do("POST", "/api/v1/me/meal-selections/"+week+"/finalize", map[string]any{"items": items(s.meals[0], 2, s.fishMeal, 1)}, tok), http.StatusOK)

	// A customer whose renewal is declined, and one whose renewal succeeds.
	s.active("declines@example.com", testpay.CardDecline)
	s.tick("2026-11-13 00:30") // cutoff for 16 Nov: orders made; week of 23 Nov created
	s.tick("2026-11-18 00:30") // renewals charged
	s.tick("2026-11-18 00:31")
	if n := h.eventCount("subscription.renewed", sub); n != 1 {
		t.Errorf("subscription.renewed = %d, want 1", n)
	}
	if n := h.eventCount("payment.failed"); n != 1 {
		t.Errorf("payment.failed = %d, want 1", n)
	}
	if n := h.eventCount("subscription.renewal_failed"); n != 1 {
		t.Errorf("subscription.renewal_failed = %d, want 1", n)
	}
	if a, b := h.count(`SELECT count(*) FROM orders`), h.eventCount("order.confirmed"); a == 0 || a != b {
		t.Errorf("orders %d, order.confirmed events %d", a, b)
	}

	// A refund publishes its final state once, even if the form is sent twice.
	var paymentID string
	if err := h.pool.QueryRow(ctx, `SELECT id FROM payments WHERE status = 'succeeded' ORDER BY created_at LIMIT 1`).Scan(&paymentID); err != nil {
		t.Fatal(err)
	}
	finance := bearer(h.adminLogin("finance@mise.test", "finance"))
	refund := map[string]any{"amount_minor": 5000, "reason": "Missing ingredient", "request_key": "events-refund-1"}
	h.wantStatus(h.do("POST", "/api/v1/admin/payments/"+paymentID+"/refunds", refund, finance), http.StatusCreated)
	h.wantStatus(h.do("POST", "/api/v1/admin/payments/"+paymentID+"/refunds", refund, finance), http.StatusCreated)
	if n := h.eventCount("refund.succeeded"); n != 1 {
		t.Errorf("refund.succeeded = %d, want 1", n)
	}

	// Pausing is a subscription event too.
	h.wantStatus(h.do("POST", "/api/v1/me/subscriptions/"+sub+"/pause", map[string]any{"weeks": 1}, tok), http.StatusOK)
	if n := h.count(`SELECT count(*) FROM domain_events WHERE aggregate_id = $1 AND type IN ('subscription.pause_scheduled', 'subscription.paused')`, sub); n != 1 {
		t.Errorf("pause events = %d, want 1", n)
	}

	// Backfill. Live events and backfilled ones use the same keys, so a
	// backfill after live publishing adds nothing...
	const commerceEvents = `SELECT count(*) FROM domain_events WHERE split_part(type, '.', 1) IN ('subscription', 'payment', 'refund', 'order')`
	before := h.count(commerceEvents)
	if n, err := commerce.BackfillEvents(ctx, h.pool); err != nil || n != 0 {
		t.Fatalf("backfill over live events added %d (err %v), want 0", n, err)
	}
	// ...and history recorded before commerce published events (simulated by
	// a history row written directly) gains its event, at its original time, once.
	var old int64
	if err := h.pool.QueryRow(ctx, `INSERT INTO subscription_events (subscription_id, type, to_status, created_at)
		VALUES ($1, 'address_changed', NULL, now() - interval '30 days') RETURNING id`, sub).Scan(&old); err != nil {
		t.Fatal(err)
	}
	if n, err := commerce.BackfillEvents(ctx, h.pool); err != nil || n != 1 {
		t.Fatalf("backfill of one old history row added %d (err %v), want 1", n, err)
	}
	if n, err := commerce.BackfillEvents(ctx, h.pool); err != nil || n != 0 {
		t.Errorf("second backfill added %d (err %v), want 0", n, err)
	}
	if h.count(commerceEvents) != before+1 {
		t.Errorf("commerce events = %d, want %d", h.count(commerceEvents), before+1)
	}
	if n := h.count(`SELECT count(*) FROM domain_events e JOIN subscription_events s ON s.id::text = e.dedupe_key
		WHERE e.type = 'subscription.address_changed' AND s.id = $1 AND e.occurred_at = s.created_at AND e.payload->>'backfilled' = 'true'`, old); n != 1 {
		t.Error("the backfilled event does not keep the time of the fact")
	}

	// The analytics consumer refreshes the days these events happened on.
	an := newAnalytics(h)
	h.registerAnalytics(an)
	if err := consumer.Replay(ctx, h.pool, analytics.ConsumerName, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := an.Dispatcher(0).Dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	h.drainJobs()
	if n := h.count(`SELECT count(*) FROM analytics_refresh_runs WHERE reason = 'event' AND status = 'succeeded'`); n == 0 {
		t.Error("commerce events did not refresh analytics")
	}
	if n := h.count(`SELECT count(*) FROM analytics_refresh_runs WHERE status <> 'succeeded'`); n != 0 {
		t.Errorf("%d analytics refreshes failed", n)
	}
}

// The payment_failed rule reacts to the payment.failed event: only while
// enabled, once per payment (dedupe), and an event replay changes nothing.
func TestPaymentFailedRuleReactsToTheEvent(t *testing.T) {
	s := newShop(t)
	h := s.h
	ctx := context.Background()
	au, _ := h.registerAutomation(false)
	admin := bearer(h.adminLogin("ops@mise.test", "admin"))
	rule := ruleID(t, h, "payment_failed")
	dispatch := func() {
		t.Helper()
		if _, err := au.Dispatcher(0).Dispatch(ctx); err != nil {
			t.Fatal(err)
		}
		h.drainJobs()
	}

	// A failure while the rule is off: the event is consumed, nothing happens.
	s.active("off@example.com", testpay.CardDecline)
	s.tick("2026-11-13 00:30")
	s.tick("2026-11-18 00:30")
	dispatch()
	if n := h.count(`SELECT count(*) FROM automation_executions`); n != 0 {
		t.Fatalf("a disabled rule ran %d times", n)
	}

	// Enabled (with the approval it needs), a new failure triggers it by event.
	h.wantStatus(h.do("PATCH", "/api/v1/admin/automation/rules/"+rule, map[string]any{"enabled": true, "action": "alert",
		"cooldown_minutes": 60, "approval_ref": "Ops review"}, admin), http.StatusOK)
	h.clock.Set(at("2026-11-18 01:00"))
	tok, addr := s.customer("on@example.com")
	_, ref := s.subscribe(tok, addr, s.basic, nil)
	var failedPayment string
	// The second customer's first payment is declined on the provider page.
	s.pay(ref, "decline", testpay.CardDecline)
	h.drainJobs()
	if err := h.pool.QueryRow(ctx, `SELECT id::text FROM payments WHERE status = 'failed' ORDER BY created_at DESC LIMIT 1`).Scan(&failedPayment); err != nil {
		t.Fatal(err)
	}
	dispatch()
	if n := h.count(`SELECT count(*) FROM automation_executions WHERE rule_id = $1 AND trigger = 'event' AND subject_id = $2`, rule, failedPayment); n != 1 {
		t.Fatalf("event-triggered executions for the failed payment = %d, want 1", n)
	}
	// Replaying every event evaluates again but the dedupe key holds.
	if err := consumer.Replay(ctx, h.pool, automation.ConsumerName, 0); err != nil {
		t.Fatal(err)
	}
	dispatch()
	if n := h.count(`SELECT count(*) FROM automation_executions WHERE subject_id = $1`, failedPayment); n != 1 {
		t.Errorf("after a replay, executions for the payment = %d, want 1", n)
	}
	// The failure from before the rule was enabled is never picked up.
	if n := h.count(`SELECT count(*) FROM automation_executions`); n != 1 {
		t.Errorf("executions = %d, want 1 (history must not flood)", n)
	}

	// Switched off again: a further failure does nothing.
	h.wantStatus(h.do("PATCH", "/api/v1/admin/automation/rules/"+rule, map[string]any{"enabled": false}, admin), http.StatusOK)
	tok3, addr3 := s.customer("later@example.com")
	_, ref3 := s.subscribe(tok3, addr3, s.basic, nil)
	s.pay(ref3, "decline", testpay.CardDecline)
	h.drainJobs()
	dispatch()
	if n := h.count(`SELECT count(*) FROM automation_executions`); n != 1 {
		t.Errorf("a disabled rule ran: executions = %d", n)
	}
}
