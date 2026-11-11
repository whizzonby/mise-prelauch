package leads

import (
	"context"
	"encoding/json"

	"mise.tt/api/internal/platform/db"
)

// The functions below are the leads module's interface for customer accounts
// (internal/customers). Accounts never write to lead tables directly.

// FindForAccount returns the lead with this canonical email, locked for the
// rest of the transaction, or nil.
func FindForAccount(ctx context.Context, q db.Querier, emailCanonical string) (*Lead, error) {
	return getByCanonicalEmailForUpdate(ctx, q, emailCanonical)
}

// Lead event types written by the server rather than the browser.
const EventAccountLinked = "account.linked"

// RecordEvent appends a server-side event to a lead's history. History is
// never rewritten: linking an account adds an event and changes nothing else.
func RecordEvent(ctx context.Context, q db.Querier, leadID, eventType string, metadata map[string]any) error {
	if metadata == nil {
		metadata = map[string]any{}
	}
	body, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `INSERT INTO lead_events (lead_id, event_type, metadata) VALUES ($1, $2, $3::jsonb)`,
		leadID, eventType, string(body))
	return err
}
