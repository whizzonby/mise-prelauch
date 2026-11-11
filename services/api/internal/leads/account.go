package leads

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

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

// Importable is what a linked customer account may carry over from its lead:
// answers the person gave on the waitlist, and whether their launch-email
// consent still stands. Read-only; the lead is not changed.
type Importable struct {
	DietaryPreferences []string
	MealInterests      []string
	HouseholdSize      *int16
	// ConsentActive is true when the lead consented to launch email and has
	// not unsubscribed since.
	ConsentActive bool
}

// ImportableFor returns the carry-over answers of a lead, or nil if it is gone.
func ImportableFor(ctx context.Context, q db.Querier, leadID string) (*Importable, error) {
	var (
		out    Importable
		status Status
	)
	err := q.QueryRow(ctx, `
		SELECT l.status, p.household_size,
		       coalesce(p.dietary_preferences, '{}'), coalesce(p.meal_interests, '{}')
		FROM leads l
		LEFT JOIN lead_preferences p ON p.lead_id = l.id
		WHERE l.id = $1`, leadID).Scan(&status, &out.HouseholdSize, &out.DietaryPreferences, &out.MealInterests)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out.ConsentActive = status != StatusUnsubscribed && status != StatusBlocked
	return &out, nil
}
