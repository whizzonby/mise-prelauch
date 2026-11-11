// Package customers owns the customer record: the commercial side of an
// account. Since Sprint 2 that is the profile (phone, contact channel,
// delivery area), the household, food preferences, allergies, onboarding
// progress and notification choices. Each concern has its own file.
//
// It also links an account to the waitlist lead with the same address once
// the customer has confirmed that address, so referral attribution, marketing
// source and consent history carry over without being copied, and copies the
// waitlist answers that the customer has not given yet.
package customers

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/customers/store"
	"mise.tt/api/internal/leads"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/vocab"
)

// Hooks implements identity.Hooks.
type Hooks struct{}

// CustomerRegistered creates the customer record for a new account.
func (Hooks) CustomerRegistered(ctx context.Context, q db.Querier, userID, termsVersion string) error {
	_, err := store.New(q).CreateCustomer(ctx, store.CreateCustomerParams{UserID: userID, TermsVersion: termsVersion})
	return err
}

// EmailVerified links the account to its lead. It runs only once the person
// has proved they own the inbox, and never for a blocked lead. The lead's own
// status and history are left as they are; an "account.linked" event is
// appended to its timeline.
//
// Then, once, it carries over what the person told the waitlist: dietary
// preferences, menu interests and household size go into the customer's
// records only where the customer has none yet, and launch-email consent
// turns on the matching topics only where the customer has made no choice.
// Allergies were never asked on the waitlist and are never written here.
func (Hooks) EmailVerified(ctx context.Context, q db.Querier, userID, emailCanonical string) error {
	lead, err := leads.FindForAccount(ctx, q, emailCanonical)
	if err != nil || lead == nil {
		return err
	}
	if lead.Status == leads.StatusBlocked {
		slog.InfoContext(ctx, "account not linked to a blocked lead", "event", "customer.link_skipped")
		return nil
	}
	s := store.New(q)
	n, err := s.LinkLead(ctx, store.LinkLeadParams{UserID: userID, LeadID: &lead.ID})
	if err != nil || n == 0 {
		return err
	}
	slog.InfoContext(ctx, "account linked to lead", "event", "customer.lead_linked")
	if err := leads.RecordEvent(ctx, q, lead.ID, leads.EventAccountLinked, nil); err != nil {
		return err
	}
	c, err := s.GetByUserID(ctx, userID)
	if err != nil {
		return err
	}
	return importFromLead(ctx, q, c, lead.ID)
}

func importFromLead(ctx context.Context, q db.Querier, c store.Customer, leadID string) error {
	in, err := leads.ImportableFor(ctx, q, leadID)
	if err != nil || in == nil {
		return err
	}
	s := store.New(q)
	imported := map[string]any{}

	dietary := mapKeys(in.DietaryPreferences, vocab.FromLeadDietary)
	interests := mapKeys(in.MealInterests, vocab.FromLeadInterests)
	if vocab.Contradiction(dietary) != "" {
		dietary = nil // the waitlist did not check combinations; ask again rather than guess
	}
	if len(dietary) > 0 || len(interests) > 0 {
		n, err := s.InsertWaitlistPreferencesIfMissing(ctx, store.InsertWaitlistPreferencesIfMissingParams{
			CustomerID: c.ID, Dietary: dietary, MealInterests: interests,
		})
		if err != nil {
			return err
		}
		if n > 0 {
			imported["preferences"] = map[string]int{"dietary": len(dietary), "meal_interests": len(interests)}
		}
	}
	if in.HouseholdSize != nil {
		n, err := s.InsertHouseholdIfMissing(ctx, store.InsertHouseholdIfMissingParams{CustomerID: c.ID, Size: *in.HouseholdSize})
		if err != nil {
			return err
		}
		if n > 0 {
			imported["household_size"] = true
		}
	}
	if in.ConsentActive {
		var topics []string
		for _, topic := range vocab.LaunchConsentTopics {
			n, err := s.InsertConsentTopicIfMissing(ctx, store.InsertConsentTopicIfMissingParams{CustomerID: c.ID, Topic: topic})
			if err != nil {
				return err
			}
			if n > 0 {
				topics = append(topics, topic)
			}
		}
		if len(topics) > 0 {
			imported["consent_topics"] = topics
		}
	}
	if len(imported) == 0 {
		return nil
	}
	return audit.Record(ctx, q, audit.Entry{
		ActorUserID: c.UserID, Action: "customer.waitlist_imported", TargetType: "customer", TargetID: c.ID, Metadata: imported,
	})
}

func mapKeys(in []string, mapping map[string]string) []string {
	out := []string{}
	for _, k := range in {
		if v, ok := mapping[k]; ok && !contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// LeadLink describes the account a lead became, for the admin lead page.
type LeadLink struct {
	CustomerID string    `json:"customer_id"`
	LinkedAt   time.Time `json:"linked_at"`
}

// LinkForLead returns the account linked to a lead, or nil.
func LinkForLead(ctx context.Context, q db.Querier, leadID string) (*LeadLink, error) {
	c, err := store.New(q).GetByLeadID(ctx, &leadID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	link := &LeadLink{CustomerID: c.ID}
	if c.LeadLinkedAt != nil {
		link.LinkedAt = *c.LeadLinkedAt
	}
	return link, nil
}
