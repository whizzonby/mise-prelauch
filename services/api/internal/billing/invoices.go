package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/billing/store"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/money"
)

// CustomerSnapshot is who an invoice is addressed to, copied at issue time.
type CustomerSnapshot struct {
	Name    string   `json:"name"`
	Email   string   `json:"email"`
	Address []string `json:"address"`
}

// BusinessSnapshot is who issued it. No tax registration is printed unless
// one is configured: it is never invented.
type BusinessSnapshot struct {
	Name            string `json:"name"`
	Address         string `json:"address,omitempty"`
	TaxRegistration string `json:"tax_registration,omitempty"`
}

// InvoiceLine is one line of an invoice.
type InvoiceLine struct {
	Kind            string `json:"kind"`
	Description     string `json:"description"`
	Quantity        int    `json:"quantity"`
	UnitAmountMinor int64  `json:"unit_amount_minor"`
	AmountMinor     int64  `json:"amount_minor"`
}

// Invoice is an issued invoice.
type Invoice struct {
	ID          string           `json:"id"`
	Number      string           `json:"number"`
	Status      string           `json:"status"`
	IssuedAt    time.Time        `json:"issued_at"`
	DueAt       time.Time        `json:"due_at"`
	PaidAt      *time.Time       `json:"paid_at"`
	VoidedAt    *time.Time       `json:"voided_at"`
	PeriodStart string           `json:"period_start"`
	PeriodEnd   string           `json:"period_end"`
	Customer    CustomerSnapshot `json:"customer"`
	Business    BusinessSnapshot `json:"business"`
	Amounts
	Lines          []InvoiceLine `json:"lines"`
	TaxNote        string        `json:"tax_note"`
	SubscriptionID string        `json:"subscription_id"`
	BillingCycleID string        `json:"billing_cycle_id"`
	CustomerID     string        `json:"-"`
}

// lines builds the invoice lines from a billing cycle. Deterministic.
func lines(planName string, b store.BillingCycle, tax TaxSnapshot, disc *DiscountSnapshot) []InvoiceLine {
	out := []InvoiceLine{{
		Kind: "plan", Quantity: 1, UnitAmountMinor: b.SubtotalMinor, AmountMinor: b.SubtotalMinor,
		Description: fmt.Sprintf("%s plan, week of %s", planName, b.PeriodStart.Format("2 January 2006")),
	}}
	if b.DiscountMinor > 0 {
		desc := "Discount"
		if disc != nil {
			desc = fmt.Sprintf("Discount: %s (%s)", disc.Name, disc.Code)
		}
		out = append(out, InvoiceLine{Kind: "discount", Description: desc, Quantity: 1,
			UnitAmountMinor: -b.DiscountMinor, AmountMinor: -b.DiscountMinor})
	}
	if b.DeliveryFeeMinor > 0 {
		out = append(out, InvoiceLine{Kind: "delivery", Description: "Delivery", Quantity: 1,
			UnitAmountMinor: b.DeliveryFeeMinor, AmountMinor: b.DeliveryFeeMinor})
	}
	if tax.Mode == TaxExclusive && b.TaxMinor > 0 {
		out = append(out, InvoiceLine{Kind: "tax", Description: fmt.Sprintf("%s at %s", taxLabel(tax), percent(tax.RateBps)),
			Quantity: 1, UnitAmountMinor: b.TaxMinor, AmountMinor: b.TaxMinor})
	}
	return out
}

func taxLabel(t TaxSnapshot) string {
	if t.Label != "" {
		return t.Label
	}
	return "Tax"
}

func percent(bps int64) string {
	if bps%100 == 0 {
		return fmt.Sprintf("%d%%", bps/100)
	}
	return fmt.Sprintf("%d.%02d%%", bps/100, bps%100)
}

// TaxNote is the sentence that explains tax on a document.
func TaxNote(t TaxSnapshot, taxMinor int64, currency string) string {
	switch t.Mode {
	case TaxInclusive:
		return fmt.Sprintf("Prices include %s at %s: %s.", taxLabel(t), percent(t.RateBps), money.Format(taxMinor, currency))
	case TaxExclusive:
		return fmt.Sprintf("%s at %s is added to the price.", taxLabel(t), percent(t.RateBps))
	}
	return "No tax is charged on this invoice."
}

// Issuer tells billing which plan a billing cycle is for; the subscriptions
// module knows (the cycle's plan snapshot).
type Issuer interface {
	PlanNameForCycle(ctx context.Context, q db.Querier, subscriptionCycleID string) (string, error)
}

func (s *Service) business() BusinessSnapshot {
	name := s.Policy.BusinessName
	if name == "" {
		name = "Mise"
	}
	return BusinessSnapshot{Name: name, Address: s.Policy.BusinessAddress, TaxRegistration: s.Policy.TaxRegistration}
}

// IssueFor issues the invoice for a billing cycle if it has none yet (a
// renewal becoming due). Idempotent.
func (s *Service) IssueFor(ctx context.Context, q db.Querier, billingCycleID string, customer CustomerSnapshot) (*Invoice, error) {
	row, err := store.New(q).GetBillingCycleForUpdate(ctx, billingCycleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	inv, err := s.ensureInvoice(ctx, q, row, customer, s.Now())
	if err != nil {
		return nil, err
	}
	return s.invoiceView(ctx, q, *inv)
}

// ensureInvoice returns the cycle's invoice, issuing it first if needed.
// The number comes from a per-year counter row that is locked until the
// transaction commits, so numbers have no gaps and never repeat.
func (s *Service) ensureInvoice(ctx context.Context, q db.Querier, b store.BillingCycle, customer CustomerSnapshot, now time.Time) (*store.Invoice, error) {
	st := store.New(q)
	if inv, err := st.InvoiceForBillingCycle(ctx, b.ID); err == nil {
		return &inv, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if s.Issuer == nil {
		return nil, errors.New("billing: no issuer configured")
	}
	planName, err := s.Issuer.PlanNameForCycle(ctx, q, b.SubscriptionCycleID)
	if err != nil {
		return nil, err
	}
	year := now.In(location).Year()
	n, err := st.NextInvoiceNumber(ctx, int32(year))
	if err != nil {
		return nil, err
	}
	number := fmt.Sprintf("MISE-%d-%06d", year, n)
	view := cycleView(b)
	custJSON, _ := json.Marshal(customer)
	busJSON, _ := json.Marshal(s.business())
	status := "open"
	var paidAt *time.Time
	if b.Status == "paid" {
		status, paidAt = "paid", b.PaidAt
	}
	inv, err := st.CreateInvoice(ctx, store.CreateInvoiceParams{
		Number: number, CustomerID: b.CustomerID, SubscriptionID: b.SubscriptionID, BillingCycleID: b.ID, Status: status,
		Currency: b.Currency, SubtotalMinor: b.SubtotalMinor, DiscountMinor: b.DiscountMinor,
		DeliveryFeeMinor: b.DeliveryFeeMinor, TaxMinor: b.TaxMinor, TotalMinor: b.TotalMinor,
		PeriodStart: b.PeriodStart, PeriodEnd: b.PeriodEnd, IssuedAt: now, DueAt: b.DueAt, PaidAt: paidAt,
		CustomerSnapshot: custJSON, BusinessSnapshot: busJSON, TaxSnapshot: b.TaxSnapshot,
	})
	if err != nil {
		return nil, err
	}
	for i, l := range lines(planName, b, view.Tax, view.Discount) {
		if err := st.AddInvoiceItem(ctx, store.AddInvoiceItemParams{InvoiceID: inv.ID, Position: int16(i), Kind: l.Kind,
			Description: l.Description, Quantity: int32(l.Quantity), UnitAmountMinor: l.UnitAmountMinor, AmountMinor: l.AmountMinor}); err != nil {
			return nil, err
		}
	}
	return &inv, nil
}

func (s *Service) invoiceView(ctx context.Context, q db.Querier, inv store.Invoice) (*Invoice, error) {
	items, err := store.New(q).InvoiceItems(ctx, inv.ID)
	if err != nil {
		return nil, err
	}
	out := &Invoice{
		ID: inv.ID, Number: inv.Number, Status: inv.Status, IssuedAt: inv.IssuedAt, DueAt: inv.DueAt, PaidAt: inv.PaidAt,
		VoidedAt: inv.VoidedAt, PeriodStart: inv.PeriodStart.Format("2006-01-02"), PeriodEnd: inv.PeriodEnd.Format("2006-01-02"),
		Amounts: Amounts{Currency: inv.Currency, SubtotalMinor: inv.SubtotalMinor, DiscountMinor: inv.DiscountMinor,
			DeliveryFeeMinor: inv.DeliveryFeeMinor, TaxMinor: inv.TaxMinor, TotalMinor: inv.TotalMinor},
		Lines: []InvoiceLine{}, SubscriptionID: inv.SubscriptionID, BillingCycleID: inv.BillingCycleID, CustomerID: inv.CustomerID,
	}
	_ = json.Unmarshal(inv.CustomerSnapshot, &out.Customer)
	_ = json.Unmarshal(inv.BusinessSnapshot, &out.Business)
	_ = json.Unmarshal(inv.TaxSnapshot, &out.Tax)
	for _, it := range items {
		out.Lines = append(out.Lines, InvoiceLine{Kind: it.Kind, Description: it.Description, Quantity: int(it.Quantity),
			UnitAmountMinor: it.UnitAmountMinor, AmountMinor: it.AmountMinor})
	}
	out.TaxNote = TaxNote(out.Tax, out.TaxMinor, out.Currency)
	return out, nil
}

// InvoiceForCustomer returns one of the customer's invoices. Another
// customer's invoice id answers not found, exactly like an unknown one.
func (s *Service) InvoiceForCustomer(ctx context.Context, customerID, id string) (*Invoice, error) {
	inv, err := store.New(s.pool).GetInvoiceForCustomer(ctx, store.GetInvoiceForCustomerParams{ID: id, CustomerID: customerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.invoiceView(ctx, s.pool, inv)
}

// Invoice returns any invoice (staff).
func (s *Service) Invoice(ctx context.Context, id string) (*Invoice, error) {
	inv, err := store.New(s.pool).GetInvoice(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.invoiceView(ctx, s.pool, inv)
}

// InvoiceSummary is an invoice in a list.
type InvoiceSummary struct {
	ID          string    `json:"id"`
	Number      string    `json:"number"`
	Status      string    `json:"status"`
	IssuedAt    time.Time `json:"issued_at"`
	PeriodStart string    `json:"period_start"`
	TotalMinor  int64     `json:"total_minor"`
	Currency    string    `json:"currency"`
}

func (s *Service) InvoicesForCustomer(ctx context.Context, customerID string, page, size int) ([]InvoiceSummary, error) {
	if size < 1 || size > 50 {
		size = 20
	}
	if page < 1 {
		page = 1
	}
	rows, err := store.New(s.pool).ListInvoicesForCustomer(ctx, store.ListInvoicesForCustomerParams{
		CustomerID: customerID, Lim: int32(size), Off: int32((page - 1) * size)})
	if err != nil {
		return nil, err
	}
	out := []InvoiceSummary{}
	for _, r := range rows {
		out = append(out, InvoiceSummary{ID: r.ID, Number: r.Number, Status: r.Status, IssuedAt: r.IssuedAt,
			PeriodStart: r.PeriodStart.Format("2006-01-02"), TotalMinor: r.TotalMinor, Currency: r.Currency})
	}
	return out, nil
}

var location = mustLocation()

func mustLocation() *time.Location {
	if loc, err := time.LoadLocation("America/Port_of_Spain"); err == nil {
		return loc
	}
	return time.FixedZone("AST", -4*3600)
}

// InvoiceRef returns the id and number of a billing cycle's invoice, if issued.
func (s *Service) InvoiceRef(ctx context.Context, q db.Querier, billingCycleID string) (id, number string, ok bool) {
	inv, err := store.New(q).InvoiceForBillingCycle(ctx, billingCycleID)
	if err != nil {
		return "", "", false
	}
	return inv.ID, inv.Number, true
}
