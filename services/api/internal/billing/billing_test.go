package billing

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestQuoteWithoutTaxOrFees(t *testing.T) {
	p := Policy{TaxMode: TaxNone}
	a := p.Quote(33000, "TTD", nil)
	if a.TotalMinor != 33000 || a.TaxMinor != 0 || a.DiscountMinor != 0 || a.Tax.Mode != TaxNone {
		t.Fatalf("quote = %+v", a)
	}
}

func TestQuoteDiscountsAndTax(t *testing.T) {
	pct := &DiscountSnapshot{Kind: "percent", PercentBps: 1500, Code: "WELCOME"}
	fixed := &DiscountSnapshot{Kind: "fixed", AmountMinor: 50000, Code: "BIG"}

	excl := Policy{TaxMode: TaxExclusive, TaxRateBps: 1250, DeliveryFeeMinor: 2500}
	a := excl.Quote(33000, "TTD", pct)
	// 330.00 - 49.50 + 25.00 = 305.50; tax 12.5% = 38.19 (38.1875 rounded half up); total 343.69
	if a.DiscountMinor != 4950 || a.TaxMinor != 3819 || a.TotalMinor != 34369 {
		t.Errorf("exclusive quote = %+v", a)
	}

	incl := Policy{TaxMode: TaxInclusive, TaxRateBps: 1250}
	b := incl.Quote(33000, "TTD", nil)
	if b.TotalMinor != 33000 || b.TaxMinor != 3667 {
		t.Errorf("inclusive quote = %+v", b)
	}

	// A fixed discount larger than the price stops at zero.
	c := Policy{TaxMode: TaxNone}.Quote(33000, "TTD", fixed)
	if c.DiscountMinor != 33000 || c.TotalMinor != 0 {
		t.Errorf("capped discount = %+v", c)
	}
}

func TestPolicyValidate(t *testing.T) {
	if err := (Policy{TaxMode: "vat"}).Validate(); err == nil {
		t.Error("unknown tax mode accepted")
	}
	if err := (Policy{TaxMode: TaxExclusive}).Validate(); err == nil {
		t.Error("tax without a rate accepted")
	}
	if err := (Policy{TaxMode: TaxNone}).Validate(); err != nil {
		t.Error(err)
	}
}

func sampleInvoice() *Invoice {
	paid := time.Date(2026, 11, 17, 4, 0, 0, 0, time.UTC)
	return &Invoice{
		Number: "MISE-2026-000042", Status: "paid", IssuedAt: paid, DueAt: paid, PaidAt: &paid,
		PeriodStart: "2026-11-23", PeriodEnd: "2026-11-29",
		Customer: CustomerSnapshot{Name: "Ana (Test)", Email: "ana@example.com", Address: []string{"1 Frederick Street", "Port of Spain"}},
		Business: BusinessSnapshot{Name: "Mise"},
		Amounts:  Amounts{Currency: "TTD", SubtotalMinor: 33000, DiscountMinor: 4950, TotalMinor: 28050, Tax: TaxSnapshot{Mode: TaxNone}},
		Lines: []InvoiceLine{
			{Kind: "plan", Description: "Basic plan, week of 23 November 2026", Quantity: 1, AmountMinor: 33000},
			{Kind: "discount", Description: "Discount: Welcome (WELCOME)", Quantity: 1, AmountMinor: -4950},
		},
		TaxNote: TaxNote(TaxSnapshot{Mode: TaxNone}, 0, "TTD"),
	}
}

func TestInvoicePDFIsDeterministicAndWellFormed(t *testing.T) {
	a, b := InvoicePDF(sampleInvoice()), InvoicePDF(sampleInvoice())
	if !bytes.Equal(a, b) {
		t.Fatal("the same invoice gave different PDF bytes")
	}
	s := string(a)
	if !strings.HasPrefix(s, "%PDF-1.4") || !strings.HasSuffix(s, "%%EOF\n") {
		t.Fatal("not a PDF")
	}
	for _, want := range []string{"MISE-2026-000042", "TTD 280.50", `Ana \(Test\)`, "No tax is charged", "Paid on 17 November 2026"} {
		if !strings.Contains(s, want) {
			t.Errorf("PDF is missing %q", want)
		}
	}
	// The xref offsets must point at the objects.
	idx := strings.LastIndex(s, "startxref\n")
	var xref int
	if _, err := fmtSscan(s[idx+len("startxref\n"):], &xref); err != nil || !strings.HasPrefix(s[xref:], "xref") {
		t.Fatalf("startxref does not point at the xref table (%d)", xref)
	}
}

func TestTaxNote(t *testing.T) {
	if got := TaxNote(TaxSnapshot{Mode: TaxInclusive, RateBps: 1250, Label: "VAT"}, 3667, "TTD"); got != "Prices include VAT at 12.50%: TTD 36.67." {
		t.Errorf("note = %q", got)
	}
}
