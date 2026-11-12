package reporting

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"strings"
	"time"

	"mise.tt/api/internal/crm"
)

// ExportLimit is the most rows one export holds; larger results are cut and
// marked truncated.
const ExportLimit = 50_000

// ExportKinds and the permission each needs. Columns are the minimum for the
// purpose: no allergies, addresses, phone numbers, notes or card details.
var ExportKinds = map[string]string{
	"customers":     "customers.export",
	"subscriptions": "customers.export",
	"orders":        "customers.export",
	"payments":      "payments.read",
	"leads":         "leads.export",
}

// ExportParams narrows an export.
type ExportParams struct {
	From      string     `json:"from,omitempty"`
	To        string     `json:"to,omitempty"`
	Status    string     `json:"status,omitempty"`
	SegmentID string     `json:"segment_id,omitempty"`
	Rules     *crm.Rules `json:"-"`
}

// csvCell stops spreadsheet formula injection: a cell that starts with =,
// +, - or @ (or a tab or carriage return) is prefixed with a quote.
func csvCell(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}

// BuildExport writes the CSV for an export. It returns the bytes, the row
// count and whether the limit cut it short.
func (s *Service) BuildExport(ctx context.Context, kind string, p ExportParams) ([]byte, int, bool, error) {
	a := &args{}
	var sql string
	var head []string
	dateRange := func(col string) string {
		var w []string
		if p.From != "" {
			w = append(w, "("+col+" AT TIME ZONE '"+TimeZone+"')::date >= "+a.add(p.From)+"::date")
		}
		if p.To != "" {
			w = append(w, "("+col+" AT TIME ZONE '"+TimeZone+"')::date <= "+a.add(p.To)+"::date")
		}
		if len(w) == 0 {
			return "true"
		}
		return strings.Join(w, " AND ")
	}
	switch kind {
	case "customers":
		head = []string{"customer_id", "first_name", "last_name", "email", "status", "joined", "subscription_status", "plan", "region", "source"}
		w := []string{dateRange("c.created_at")}
		if p.Rules != nil {
			w = append(w, RuleConditions(*p.Rules, a)...)
		}
		sql = `SELECT c.id::text, u.first_name, u.last_name, u.email,
		              CASE WHEN u.status = 'disabled' THEN 'disabled' WHEN u.email_verified_at IS NULL THEN 'unverified' ELSE 'verified' END,
		              to_char(c.created_at AT TIME ZONE '` + TimeZone + `', 'YYYY-MM-DD'), coalesce(s.status, ''), coalesce(p.name, ''),
		              coalesce(c.delivery_region, ''), coalesce(la.utm_source, 'direct')` + customerBase +
			` WHERE ` + strings.Join(w, " AND ") + ` ORDER BY c.created_at`
	case "subscriptions":
		head = []string{"subscription_id", "customer_id", "status", "plan", "amount_minor", "currency", "first_week", "activated_at", "cancelled_at"}
		w := dateRange("s.created_at")
		if p.Status != "" {
			w += " AND s.status = " + a.add(p.Status)
		}
		sql = `SELECT s.id::text, s.customer_id::text, s.status, p.name, s.amount_minor::text, s.currency, s.first_week::text,
		              coalesce(to_char(s.activated_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''), coalesce(to_char(s.cancelled_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), '')
		       FROM subscriptions s JOIN subscription_plans p ON p.id = s.plan_id WHERE ` + w + ` ORDER BY s.created_at`
	case "orders":
		head = []string{"order_number", "customer_id", "week", "status", "plan", "meals", "total_minor", "currency", "created_at"}
		w := "true"
		if p.From != "" {
			w += " AND o.week_start >= " + a.add(p.From) + "::date"
		}
		if p.To != "" {
			w += " AND o.week_start <= " + a.add(p.To) + "::date"
		}
		sql = `SELECT o.number, o.customer_id::text, o.week_start::text, o.status, o.plan_name, o.meals_count::text, o.total_minor::text, o.currency,
		              to_char(o.created_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		       FROM orders o WHERE ` + w + ` ORDER BY o.week_start, o.number`
	case "payments":
		head = []string{"payment_id", "customer_id", "created_at", "status", "kind", "amount_minor", "refunded_minor", "currency", "provider", "provider_ref"}
		w := dateRange("py.created_at")
		if p.Status != "" {
			w += " AND py.status = " + a.add(p.Status)
		}
		sql = `SELECT py.id::text, py.customer_id::text, to_char(py.created_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), py.status, py.kind,
		              py.amount_minor::text, py.refunded_minor::text, py.currency, py.provider, coalesce(py.provider_ref, '')
		       FROM payments py WHERE ` + w + ` ORDER BY py.created_at`
	case "leads":
		head = []string{"lead_id", "first_name", "last_name", "email", "status", "location", "source", "campaign", "created_at"}
		sql = `SELECT l.id::text, l.first_name, l.last_name, l.email, l.status, l.location, coalesce(la.utm_source, 'direct'), coalesce(la.utm_campaign, ''),
		              to_char(l.created_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		       FROM leads l LEFT JOIN lead_attribution la ON la.lead_id = l.id AND la.touch = 'first'
		       WHERE l.status <> 'BLOCKED' AND ` + dateRange("l.created_at") + ` ORDER BY l.created_at`
	default:
		return nil, 0, false, fmt.Errorf("unknown export %q", kind)
	}
	sql += " LIMIT " + a.add(ExportLimit+1)
	qctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	rows, err := s.pool.Query(qctx, sql, a.list...)
	if err != nil {
		return nil, 0, false, err
	}
	defer rows.Close()
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write(head)
	n, truncated := 0, false
	for rows.Next() {
		if n == ExportLimit {
			truncated = true
			break
		}
		vals, err := rows.Values()
		if err != nil {
			return nil, 0, false, err
		}
		rec := make([]string, len(vals))
		for i, v := range vals {
			if v != nil {
				rec[i] = csvCell(fmt.Sprint(v))
			}
		}
		_ = w.Write(rec)
		n++
	}
	if err := rows.Err(); err != nil {
		return nil, 0, false, err
	}
	w.Flush()
	return buf.Bytes(), n, truncated, w.Error()
}
