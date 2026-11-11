package billing

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"mise.tt/api/internal/platform/money"
)

// InvoicePDF renders an invoice as a one-page A4 PDF. It is written by hand
// (no library) with the standard Helvetica fonts, so the output depends only
// on the invoice: the same invoice always gives the same bytes.
func InvoicePDF(inv *Invoice) []byte {
	p := &page{}
	const left, right = 56.0, 539.0
	green := [3]float64{0.118, 0.227, 0.169} // #1E3A2B
	muted := [3]float64{0.38, 0.37, 0.34}

	title := "Invoice"
	switch inv.Status {
	case "paid":
		title = "Invoice and receipt"
	case "void":
		title = "Invoice (void)"
	}

	// Header.
	p.color(green)
	p.text(left, 770, "F2", 26, "Mise.")
	p.textRight(right, 776, "F2", 16, title)
	p.color([3]float64{0, 0, 0})
	p.textRight(right, 758, "F1", 10, inv.Number)
	p.rule(left, 742, right, green, 1.5)

	// Who and when.
	y := 718.0
	p.color(muted)
	p.text(left, y, "F2", 8, "FROM")
	p.text(300, y, "F2", 8, "BILLED TO")
	p.color([3]float64{0, 0, 0})
	from := []string{inv.Business.Name}
	from = append(from, splitLines(inv.Business.Address)...)
	if inv.Business.TaxRegistration != "" {
		from = append(from, "Tax registration: "+inv.Business.TaxRegistration)
	}
	to := []string{inv.Customer.Name, inv.Customer.Email}
	to = append(to, inv.Customer.Address...)
	for i := 0; i < max(len(from), len(to)); i++ {
		y -= 14
		if i < len(from) {
			p.text(left, y, "F1", 10, fit(from[i], 40))
		}
		if i < len(to) {
			p.text(300, y, "F1", 10, fit(to[i], 44))
		}
	}

	y -= 30
	facts := [][2]string{
		{"Issued", date(inv.IssuedAt)},
		{"Delivery week", weekRange(inv.PeriodStart, inv.PeriodEnd)},
		{"Status", status(inv)},
	}
	for _, f := range facts {
		p.color(muted)
		p.text(left, y, "F1", 9, f[0])
		p.color([3]float64{0, 0, 0})
		p.text(150, y, "F1", 10, f[1])
		y -= 15
	}

	// Lines.
	y -= 16
	p.color(muted)
	p.text(left, y, "F2", 8, "DESCRIPTION")
	p.textRight(430, y, "F2", 8, "QTY")
	p.textRight(right, y, "F2", 8, "AMOUNT")
	y -= 8
	p.rule(left, y, right, muted, 0.5)
	p.color([3]float64{0, 0, 0})
	for _, l := range inv.Lines {
		y -= 18
		p.text(left, y, "F1", 10, fit(l.Description, 62))
		p.textRight(430, y, "F1", 10, fmt.Sprint(l.Quantity))
		p.textRight(right, y, "F1", 10, money.Format(l.AmountMinor, inv.Currency))
	}
	y -= 10
	p.rule(left, y, right, muted, 0.5)

	// Totals.
	totals := [][2]string{{"Subtotal", money.Format(inv.SubtotalMinor, inv.Currency)}}
	if inv.DiscountMinor > 0 {
		totals = append(totals, [2]string{"Discount", money.Format(-inv.DiscountMinor, inv.Currency)})
	}
	totals = append(totals, [2]string{"Delivery", money.Format(inv.DeliveryFeeMinor, inv.Currency)})
	switch inv.Tax.Mode {
	case TaxExclusive:
		totals = append(totals, [2]string{taxLabel(inv.Tax), money.Format(inv.TaxMinor, inv.Currency)})
	case TaxInclusive:
		totals = append(totals, [2]string{taxLabel(inv.Tax) + " included", money.Format(inv.TaxMinor, inv.Currency)})
	}
	for _, t := range totals {
		y -= 16
		p.textRight(430, y, "F1", 10, t[0])
		p.textRight(right, y, "F1", 10, t[1])
	}
	y -= 22
	p.color(green)
	p.textRight(430, y, "F2", 12, "Total")
	p.textRight(right, y, "F2", 12, money.Format(inv.TotalMinor, inv.Currency))

	// Notes.
	y -= 40
	p.color([3]float64{0, 0, 0})
	p.text(left, y, "F1", 9, inv.TaxNote)
	if inv.Status == "void" {
		y -= 14
		p.text(left, y, "F1", 9, "This invoice was voided and is not payable.")
	}
	p.color(muted)
	p.text(left, 60, "F1", 8, "Mise makes Caribbean meal kits in Trinidad & Tobago.")
	p.textRight(right, 60, "F1", 8, inv.Number)

	return p.document()
}

func date(t time.Time) string { return t.In(location).Format("2 January 2006") }

func weekRange(start, end string) string {
	s, err1 := time.Parse("2006-01-02", start)
	e, err2 := time.Parse("2006-01-02", end)
	if err1 != nil || err2 != nil {
		return start
	}
	return s.Format("2 January") + " – " + e.Format("2 January 2006")
}

func status(inv *Invoice) string {
	switch inv.Status {
	case "paid":
		if inv.PaidAt != nil {
			return "Paid on " + date(*inv.PaidAt)
		}
		return "Paid"
	case "void":
		return "Void"
	}
	return "Due by " + date(inv.DueAt)
}

func splitLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func fit(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// ---------------------------------------------------------------- PDF writer

type page struct{ ops bytes.Buffer }

func (p *page) color(c [3]float64) {
	fmt.Fprintf(&p.ops, "%.3f %.3f %.3f rg %.3f %.3f %.3f RG\n", c[0], c[1], c[2], c[0], c[1], c[2])
}

func (p *page) text(x, y float64, font string, size float64, s string) {
	fmt.Fprintf(&p.ops, "BT /%s %.1f Tf 1 0 0 1 %.2f %.2f Tm (%s) Tj ET\n", font, size, x, y, escape(s))
}

func (p *page) textRight(x, y float64, font string, size float64, s string) {
	p.text(x-width(s, font)*size/1000, y, font, size, s)
}

func (p *page) rule(x1, y, x2 float64, c [3]float64, w float64) {
	p.color(c)
	fmt.Fprintf(&p.ops, "%.2f w %.2f %.2f m %.2f %.2f l S\n", w, x1, y, x2, y)
}

func (p *page) document() []byte {
	var b bytes.Buffer
	var offsets []int
	obj := func(body string) {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", len(offsets), body)
	}
	b.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj("<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	obj("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Contents 4 0 R " +
		"/Resources << /Font << /F1 5 0 R /F2 6 0 R >> >> >>")
	stream := p.ops.Bytes()
	obj(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream)+1, stream))
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>")
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>")
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, o := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	return b.Bytes()
}

// escape writes s as a PDF string in WinAnsi encoding. Characters outside
// it become "?".
func escape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '(' || r == ')' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r >= 32 && r < 127:
			b.WriteRune(r)
		case winAnsi[r] != 0:
			fmt.Fprintf(&b, "\\%03o", winAnsi[r])
		case r >= 160 && r <= 255:
			fmt.Fprintf(&b, "\\%03o", r)
		default:
			b.WriteByte('?')
		}
	}
	return b.String()
}

var winAnsi = map[rune]byte{'–': 0x96, '—': 0x97, '…': 0x85, '‘': 0x91, '’': 0x92, '“': 0x93, '”': 0x94, '•': 0x95, '€': 0x80}

// width is the advance width of s in a standard font, in 1/1000 em, for
// right-aligned figures. Unlisted characters count as 556 (a digit).
func width(s, font string) float64 {
	w := 0.0
	for _, r := range s {
		if v, ok := glyphWidths[r]; ok {
			w += v
			continue
		}
		switch {
		case r >= 'a' && r <= 'z':
			w += 500
		case r >= 'A' && r <= 'Z':
			w += 667
		default:
			w += 556
		}
	}
	if font == "F2" {
		w *= 1.04
	}
	return w
}

var glyphWidths = map[rune]float64{
	' ': 278, ',': 278, '.': 278, '-': 333, '(': 333, ')': 333, ':': 278, '%': 889,
	'0': 556, '1': 556, '2': 556, '3': 556, '4': 556, '5': 556, '6': 556, '7': 556, '8': 556, '9': 556,
	'T': 611, 'D': 722, 'I': 278, 'M': 833, 'S': 667, 'E': 667, 'Q': 778, 'A': 667, 'Y': 667, 'U': 722, 'N': 722,
	'a': 556, 'b': 556, 'c': 500, 'd': 556, 'e': 556, 'f': 278, 'i': 222, 'j': 222, 'l': 222, 'm': 833,
	'n': 556, 'o': 556, 'p': 556, 'r': 333, 's': 500, 't': 278, 'u': 556, 'v': 500, 'w': 722, 'x': 500, 'y': 500,
}
