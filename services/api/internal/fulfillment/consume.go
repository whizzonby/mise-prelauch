package fulfillment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/delivery"
	"mise.tt/api/internal/orders"
	"mise.tt/api/internal/platform/consumer"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/production"
)

// JobBatchReady turns a production.ready_for_fulfillment event (one per
// completed batch) into obligations for the orders it completed.
const JobBatchReady = "fulfillment.batch_ready"

// ConsumerName is this module's row in event_consumer_offsets.
const ConsumerName = "fulfillment"

type readyPayload struct {
	EventID int64  `json:"event_id"`
	BatchID string `json:"batch_id"`
}

func (s *Service) dispatcher() *consumer.Dispatcher {
	return &consumer.Dispatcher{Pool: s.pool, Name: ConsumerName, SettleDelay: s.SettleDelay, Routes: map[string]consumer.Route{
		"production.ready_for_fulfillment": {Kind: JobBatchReady, Payload: func(id int64, agg string) any { return readyPayload{EventID: id, BatchID: agg} }},
	}}
}

// Dispatch hands new readiness events to the job queue.
func (s *Service) Dispatch(ctx context.Context) (int, error) { return s.dispatcher().Dispatch(ctx) }

// RunDispatcher dispatches every interval until ctx ends (the worker runs it).
func (s *Service) RunDispatcher(ctx context.Context, interval time.Duration) {
	s.dispatcher().Run(ctx, interval)
}

// ConsumerStatus is the consumer's offset and failed or waiting jobs.
func (s *Service) ConsumerStatus(ctx context.Context) (*consumer.Status, error) {
	return consumer.StatusOf(ctx, s.pool, ConsumerName, JobBatchReady)
}

// HandleBatchReady is the job handler. It is idempotent (one obligation per
// order) and returns an error, so the job queue retries, when the batch is
// not visible yet.
func (s *Service) HandleBatchReady(ctx context.Context, payload json.RawMessage) error {
	var p readyPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("fulfillment: bad payload: %w", err)
	}
	_, err := s.ConsumeBatch(ctx, p.EventID, p.BatchID)
	return err
}

// Outcome is what consuming one order did.
type Outcome struct {
	OrderID string `json:"order_id"`
	Result  string `json:"result"` // created, exception, cancelled, exists, not_ready
	Detail  string `json:"detail,omitempty"`
}

// ConsumeBatch evaluates every order of the batch's plan that includes its meal.
func (s *Service) ConsumeBatch(ctx context.Context, eventID int64, batchID string) ([]Outcome, error) {
	var out []Outcome
	err := s.tx(ctx, func(tx pgx.Tx) error {
		_, orderIDs, err := s.Production.OrdersForBatch(ctx, tx, batchID)
		if errors.Is(err, httpx.ErrNotFound) {
			return fmt.Errorf("fulfillment: batch %s is not visible yet", batchID)
		}
		if err != nil {
			return err
		}
		for _, id := range orderIDs {
			o, err := s.evaluate(ctx, tx, id, eventID, "")
			if err != nil {
				return err
			}
			out = append(out, o)
		}
		return nil
	})
	return out, err
}

// Sync evaluates every order at ready_for_fulfillment that has no
// obligation: the safety net for a missed or failed event.
func (s *Service) Sync(ctx context.Context, actor string) ([]Outcome, error) {
	ids, err := s.Orders.ReadyOrderIDs(ctx, s.pool)
	if err != nil {
		return nil, err
	}
	out := []Outcome{}
	for _, id := range ids {
		err := s.tx(ctx, func(tx pgx.Tx) error {
			o, err := s.evaluate(ctx, tx, id, 0, actor)
			if err == nil && o.Result != "exists" {
				out = append(out, o)
			}
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// evaluate decides one order, in tx. It serialises on the order with an
// advisory lock so two workers cannot race to create the same obligation
// (the unique order_id constraint is the final guard).
func (s *Service) evaluate(ctx context.Context, tx pgx.Tx, orderID string, eventID int64, actor string) (Outcome, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('fulfillment:' || $1, 0))`, orderID); err != nil {
		return Outcome{}, err
	}
	var existing string
	err := tx.QueryRow(ctx, `SELECT id::text FROM fulfillment_obligations WHERE order_id = $1`, orderID).Scan(&existing)
	if err == nil {
		return Outcome{OrderID: orderID, Result: "exists"}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Outcome{}, err
	}
	facts, err := s.Orders.Facts(ctx, tx, orderID)
	if errors.Is(err, httpx.ErrNotFound) {
		return Outcome{}, fmt.Errorf("fulfillment: order %s is not visible yet", orderID)
	}
	if err != nil {
		return Outcome{}, err
	}
	switch facts.Status {
	case "awaiting_production", "in_production":
		// Other batches are still open; their completion will bring us back.
		return Outcome{OrderID: orderID, Result: "not_ready"}, nil
	case "ready_for_fulfillment":
	default:
		// Commerce and production disagree (cancelled, still pending, or
		// already past fulfillment with no obligation). Record it; never
		// create an active delivery from it.
		kits, _ := s.Production.KitsForOrder(ctx, tx, orderID)
		id, err := s.create(ctx, tx, facts, kits, eventID, actor, "cancelled",
			"The order is "+human(facts.Status)+" in commerce, but production made kits for it.")
		if err != nil {
			return Outcome{}, err
		}
		if _, err := s.Delivery.RaiseException(ctx, tx, delivery.ExceptionInput{Stage: "fulfillment", Type: "eligibility", Severity: "high",
			Summary: "Kits made for an order that is " + human(facts.Status), ObligationID: id,
			Notes:     "Commerce has this order as " + human(facts.Status) + ". No delivery was created. Decide what happens to the packed kits.",
			DedupeKey: "eligibility:" + orderID, Actor: actor}); err != nil {
			return Outcome{}, err
		}
		return Outcome{OrderID: orderID, Result: "cancelled", Detail: facts.Status}, nil
	}
	kits, err := s.Production.KitsForOrder(ctx, tx, orderID)
	if errors.Is(err, httpx.ErrNotFound) {
		return Outcome{}, fmt.Errorf("fulfillment: no live production plan includes order %s yet", facts.Number)
	}
	if err != nil {
		return Outcome{}, err
	}
	var problems []string
	if ok, why := facts.PaymentEligible(); !ok {
		problems = append(problems, why)
	}
	for _, k := range kits {
		if ok, why := k.Ready(); !ok {
			problems = append(problems, why)
		}
	}
	if len(problems) == 0 {
		if _, err := s.create(ctx, tx, facts, kits, eventID, actor, "ready_for_verification", ""); err != nil {
			return Outcome{}, err
		}
		return Outcome{OrderID: orderID, Result: "created"}, nil
	}
	// Contradiction: the order is marked ready but something says otherwise.
	// The obligation exists (so it is visible) but is blocked until resolved.
	id, err := s.create(ctx, tx, facts, kits, eventID, actor, "exception", strings.Join(problems, " "))
	if err != nil {
		return Outcome{}, err
	}
	if _, err := s.Delivery.RaiseException(ctx, tx, delivery.ExceptionInput{Stage: "fulfillment", Type: "eligibility", Severity: "high", Blocking: true,
		Summary: "Order " + facts.Number + " cannot be fulfilled yet", Notes: strings.Join(problems, " "), ObligationID: id,
		NextAction: "escalate", DedupeKey: "eligibility:" + orderID, Actor: actor}); err != nil {
		return Outcome{}, err
	}
	return Outcome{OrderID: orderID, Result: "exception", Detail: strings.Join(problems, " ")}, nil
}

// create inserts the obligation, one package and an item per meal line.
func (s *Service) create(ctx context.Context, tx pgx.Tx, f *orders.FulfillmentFacts, kits []production.OrderKit, eventID int64, actor, status, why string) (string, error) {
	number, err := delivery.NextNumber(ctx, tx, "FUL", delivery.WeekPeriod(f.Week))
	if err != nil {
		return "", err
	}
	var planID *string
	if len(kits) > 0 {
		planID = &kits[0].PlanID
	}
	var resume *string
	if status == "exception" {
		r := "ready_for_verification"
		resume = &r
	}
	var cancelledAt any
	if status == "cancelled" {
		cancelledAt = s.Now()
	}
	var ev *int64
	if eventID > 0 {
		ev = &eventID
	}
	kitsRequired := f.Kits
	if kitsRequired <= 0 {
		kitsRequired = 1
	}
	var id string
	if err := tx.QueryRow(ctx, `
		INSERT INTO fulfillment_obligations (number, order_id, order_number, customer_id, week_start, plan_id, status, resume_status, kits_required,
			source_event_id, cancelled_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING id::text`,
		number, f.OrderID, f.Number, f.CustomerID, f.Week, planID, status, resume, kitsRequired, ev, cancelledAt).Scan(&id); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO fulfillment_events (obligation_id, from_status, to_status, reason, actor_user_id) VALUES ($1, NULL, $2, $3, $4)`,
		id, status, nilIfEmpty(why), nilIfEmpty(actor)); err != nil {
		return "", err
	}
	var pkg string
	if err := tx.QueryRow(ctx, `INSERT INTO fulfillment_packages (code, obligation_id, sequence) VALUES ($1, $2, 1) RETURNING id::text`,
		number+"-P1", id).Scan(&pkg); err != nil {
		return "", err
	}
	for _, k := range kits {
		if k.BatchID == "" {
			continue // no batch: recorded as a problem above, nothing to pack
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO fulfillment_package_items (package_id, obligation_id, meal_id, meal_name, quantity, batch_id, batch_code, recipe_version_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, pkg, id, k.MealID, k.MealName, k.Quantity, k.BatchID, k.BatchCode, k.RecipeVersionID); err != nil {
			return "", err
		}
	}
	if err := event(ctx, tx, "fulfillment.created", id, "", map[string]any{"order_id": f.OrderID, "status": status, "source_event_id": eventID}); err != nil {
		return "", err
	}
	return id, record(ctx, tx, actor, "fulfillment.created", id, map[string]any{"order": f.Number, "status": status, "source_event_id": eventID})
}
