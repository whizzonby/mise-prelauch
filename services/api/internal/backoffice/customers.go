package backoffice

import (
	"context"
	"time"

	"mise.tt/api/internal/addresses"
	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/billing"
	"mise.tt/api/internal/customers"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/orders"
	"mise.tt/api/internal/payments"
	"mise.tt/api/internal/reporting"
	"mise.tt/api/internal/support"
)

// The Customer 360 is assembled from each owning module; nothing here is a
// copy. Sensitive parts (allergies, addresses, phone) are only returned to
// staff with customers.sensitive.

// SubscriptionSummary is one of the customer's subscriptions.
type SubscriptionSummary struct {
	ID          string     `json:"id"`
	Status      string     `json:"status"`
	PlanName    string     `json:"plan_name"`
	AmountMinor int64      `json:"amount_minor"`
	Currency    string     `json:"currency"`
	FirstWeek   string     `json:"first_week"`
	ActivatedAt *time.Time `json:"activated_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// Customer360 is the overview tab plus everything small enough to load at once.
type Customer360 struct {
	reporting.CustomerOverview
	Subscriptions []SubscriptionSummary `json:"subscriptions"`
	Cases         []support.Case        `json:"cases"`
	Sensitive     bool                  `json:"can_view_sensitive"`
}

func (s *Service) Customer(ctx context.Context, p *identity.Principal, id string) (*Customer360, error) {
	o, err := s.Reporting.CustomerOverview(ctx, id)
	if err != nil {
		return nil, err
	}
	out := &Customer360{CustomerOverview: *o, Subscriptions: []SubscriptionSummary{}, Cases: []support.Case{},
		Sensitive: p.Can(identity.PermCustomersSensitive)}
	rows, err := s.Subs.List(ctx, "", id, 1, 50)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out.Subscriptions = append(out.Subscriptions, SubscriptionSummary{ID: r.ID, Status: r.Status, PlanName: r.PlanName,
			AmountMinor: r.AmountMinor, Currency: r.Currency, FirstWeek: r.FirstWeek.Format("2006-01-02"), ActivatedAt: r.ActivatedAt, CreatedAt: r.CreatedAt})
	}
	if p.Can(identity.PermCasesRead) {
		page, err := s.Support.List(ctx, support.Filter{CustomerID: id, Size: 50})
		if err != nil {
			return nil, err
		}
		out.Cases = page.Items
	}
	return out, nil
}

// CustomerOrders is the orders tab.
func (s *Service) CustomerOrders(ctx context.Context, id string) ([]orders.Summary, error) {
	return s.Orders.List(ctx, orders.Filter{CustomerID: id})
}

// CustomerBilling is the billing tab: weeks billed, invoices, payments
// (including failed attempts) and refunds.
type CustomerBilling struct {
	Cycles   []billing.Cycle          `json:"billing_cycles"`
	Invoices []billing.InvoiceSummary `json:"invoices"`
	Payments []payments.Payment       `json:"payments"`
}

func (s *Service) CustomerBilling(ctx context.Context, id string) (*CustomerBilling, error) {
	out := &CustomerBilling{}
	var err error
	if out.Cycles, err = s.Billing.ForCustomer(ctx, id); err != nil {
		return nil, err
	}
	if out.Invoices, err = s.Billing.InvoicesForCustomer(ctx, id, 1, 50); err != nil {
		return nil, err
	}
	if out.Payments, err = s.Payments.List(ctx, payments.PaymentsFilter{CustomerID: id}); err != nil {
		return nil, err
	}
	return out, nil
}

// CustomerPreferences is the preferences tab. Allergies, addresses and the
// phone number are sensitive; the caller has customers.sensitive.
type CustomerPreferences struct {
	Preferences    *customers.Preferences `json:"preferences"`
	Household      *customers.Household   `json:"household"`
	Addresses      []addresses.Address    `json:"addresses"`
	Phone          *string                `json:"phone"`
	ContactChannel string                 `json:"contact_channel"`
}

func (s *Service) CustomerPreferences(ctx context.Context, actor, id string) (*CustomerPreferences, error) {
	if _, err := s.Reporting.CustomerOverview(ctx, id); err != nil {
		return nil, err
	}
	out := &CustomerPreferences{}
	var err error
	if out.Preferences, err = customers.PreferencesOf(ctx, s.pool, id); err != nil {
		return nil, err
	}
	if out.Household, err = customers.HouseholdOf(ctx, s.pool, id); err != nil {
		return nil, err
	}
	if out.Addresses, err = addresses.ForCustomer(ctx, s.pool, s.Delivery, id); err != nil {
		return nil, err
	}
	if out.Phone, out.ContactChannel, err = customers.PhoneOf(ctx, s.pool, id); err != nil {
		return nil, err
	}
	// Looking at someone's health and contact details is itself recorded.
	return out, auditView(ctx, s, actor, id)
}

func auditView(ctx context.Context, s *Service, actor, id string) error {
	return audit.Record(ctx, s.pool, audit.Entry{ActorUserID: actor, Action: "customer.sensitive_viewed", TargetType: "customer", TargetID: id})
}
