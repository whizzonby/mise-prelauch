// Package outbox records domain events in the same transaction as the change
// that caused them (a transactional outbox). Later modules (procurement,
// delivery) read domain_events in id order and must be idempotent: delivery
// is at least once, never exactly once.
package outbox

import (
	"context"
	"encoding/json"

	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
)

// Event is one domain event. DedupeKey makes a retried write a no-op: the
// same (Type, AggregateID, DedupeKey) is stored once.
type Event struct {
	Type          string
	AggregateType string
	AggregateID   string
	DedupeKey     string
	Payload       map[string]any
}

// Record writes the event with q, which should be the transaction making
// the change. A duplicate is ignored.
func Record(ctx context.Context, q db.Querier, e Event) error {
	if e.Payload == nil {
		e.Payload = map[string]any{}
	}
	body, err := json.Marshal(e.Payload)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `
		INSERT INTO domain_events (type, aggregate_type, aggregate_id, dedupe_key, payload, request_id)
		VALUES ($1, $2, $3, $4, $5::jsonb, NULLIF($6, ''))
		ON CONFLICT (type, aggregate_id, dedupe_key) DO NOTHING`,
		e.Type, e.AggregateType, e.AggregateID, e.DedupeKey, string(body), httpx.RequestID(ctx))
	return err
}
