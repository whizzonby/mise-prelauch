// Package customers owns the customer record: the commercial side of an
// account (terms accepted, and later addresses, households, dietary
// preferences and subscriptions). In Sprint 1 it does one more thing: when a
// customer confirms their email, it links the account to the waitlist lead
// with the same address, so referral attribution, marketing source and
// consent history carry over without being copied.
package customers

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/customers/store"
	"mise.tt/api/internal/leads"
	"mise.tt/api/internal/platform/db"
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
func (Hooks) EmailVerified(ctx context.Context, q db.Querier, userID, emailCanonical string) error {
	lead, err := leads.FindForAccount(ctx, q, emailCanonical)
	if err != nil || lead == nil {
		return err
	}
	if lead.Status == leads.StatusBlocked {
		slog.InfoContext(ctx, "account not linked to a blocked lead", "event", "customer.link_skipped")
		return nil
	}
	n, err := store.New(q).LinkLead(ctx, store.LinkLeadParams{UserID: userID, LeadID: &lead.ID})
	if err != nil || n == 0 {
		return err
	}
	slog.InfoContext(ctx, "account linked to lead", "event", "customer.lead_linked")
	return leads.RecordEvent(ctx, q, lead.ID, leads.EventAccountLinked, nil)
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
