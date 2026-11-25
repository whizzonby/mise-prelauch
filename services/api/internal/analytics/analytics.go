package analytics

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
)

type Service struct {
	pool     *pgxpool.Pool
	Location *time.Location
	Now      func() time.Time
	// QueryTimeout bounds every analytics query so analytics can never hold
	// up ordering, production or dispatch.
	QueryTimeout time.Duration
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, Location: time.UTC, Now: time.Now, QueryTimeout: 10 * time.Second}
}

const day = "2006-01-02"

// Range is an inclusive range of local dates.
type Range struct {
	From, To time.Time // local dates (midnight, UTC location)
}

func (r Range) Days() int { return int(r.To.Sub(r.From).Hours()/24) + 1 }

// Previous is the period of equal length just before.
func (r Range) Previous() Range {
	n := r.Days()
	return Range{From: r.From.AddDate(0, 0, -n), To: r.From.AddDate(0, 0, -1)}
}

func fieldErr(field, msg string) error {
	return httpx.ValidationError([]httpx.FieldError{{Field: field, Code: "invalid", Message: msg}})
}

// ParseRange reads from/to (YYYY-MM-DD). Defaults: the last 28 local days.
func (s *Service) ParseRange(from, to string) (Range, error) {
	today := s.today()
	r := Range{From: today.AddDate(0, 0, -27), To: today}
	if from != "" || to != "" {
		f, err1 := time.Parse(day, from)
		t, err2 := time.Parse(day, to)
		if err1 != nil || err2 != nil || t.Before(f) || t.Sub(f) > 731*24*time.Hour {
			return r, fieldErr("from", "Choose a range of 1 day to 2 years, with from before to (YYYY-MM-DD).")
		}
		r = Range{From: f, To: t}
	}
	return r, nil
}

func (s *Service) today() time.Time {
	t := s.Now().In(s.Location)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// bounds converts local dates to UTC instants: [from 00:00 local, to+1 00:00 local).
func (s *Service) bounds(r Range) (time.Time, time.Time) {
	f := time.Date(r.From.Year(), r.From.Month(), r.From.Day(), 0, 0, 0, 0, s.Location)
	t := time.Date(r.To.Year(), r.To.Month(), r.To.Day(), 0, 0, 0, 0, s.Location).AddDate(0, 0, 1)
	return f.UTC(), t.UTC()
}

// Filters narrow metrics that support them.
type Filters struct {
	Plan, Meal, Zone, Supplier string
}

func (f Filters) values() map[string]any {
	v := map[string]any{}
	for k, x := range map[string]string{"plan": f.Plan, "meal": f.Meal, "zone": f.Zone, "supplier": f.Supplier} {
		if x != "" {
			v[k] = x
		} else {
			v[k] = nil
		}
	}
	return v
}

func (f Filters) validate() error {
	for k, v := range map[string]string{"plan_id": f.Plan, "meal_id": f.Meal, "zone_id": f.Zone, "supplier_id": f.Supplier} {
		if v != "" && !uuidLike(v) {
			return fieldErr(k, "Not a valid id.")
		}
	}
	return nil
}

func uuidLike(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

// Value computes one metric for a range. nil means undefined (e.g. a rate
// with no denominator), never zero.
func (s *Service) Value(ctx context.Context, q db.Querier, m *Metric, r Range, f Filters) (*float64, error) {
	ft, tt := s.bounds(r)
	vals := f.values()
	vals["from_ts"], vals["to_ts"], vals["from"], vals["to"] = ft, tt, r.From, r.To
	sql, args := bind(m.SQL, vals)
	var v *float64
	if err := q.QueryRow(ctx, `SELECT (`+sql+`)::float8`, args...).Scan(&v); err != nil {
		return nil, fmt.Errorf("metric %s: %w", m.ID, err)
	}
	return v, nil
}

// readTx runs fn in a read-only transaction with a statement timeout.
func (s *Service) readTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = %d", s.QueryTimeout.Milliseconds())); err != nil {
		return err
	}
	return fn(tx)
}

// ---------------------------------------------------------------- dashboards

// MetricValue is a metric as shown on a dashboard.
type MetricValue struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Kind        Kind     `json:"kind"`
	Value       *float64 `json:"value"`
	Previous    *float64 `json:"previous"`
	Change      *float64 `json:"change"` // relative change; nil when undefined
	Definition  string   `json:"definition"`
	Limitations string   `json:"limitations"`
	Currency    string   `json:"currency"` // ISO code for money, empty otherwise
}

type Table struct {
	Title   string     `json:"title"`
	Note    string     `json:"note"`
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
	Link    string     `json:"link,omitempty"`
}

type Section struct {
	Title   string        `json:"title"`
	Note    string        `json:"note,omitempty"`
	Metrics []MetricValue `json:"metrics"`
	Tables  []Table       `json:"tables"`
}

type Dashboard struct {
	Kind         string     `json:"kind"`
	Title        string     `json:"title"`
	From         string     `json:"from"`
	To           string     `json:"to"`
	PreviousFrom string     `json:"previous_from"`
	PreviousTo   string     `json:"previous_to"`
	TimeZone     string     `json:"time_zone"`
	GeneratedAt  time.Time  `json:"generated_at"`
	LastRefresh  *time.Time `json:"last_refresh"`
	Notices      []string   `json:"notices"`
	Sections     []Section  `json:"sections"`
	Filters      []string   `json:"filters"`
}

type sectionSpec struct {
	title, note string
	metrics     []string
	tables      []func(ctx context.Context, s *Service, q db.Querier, r Range, f Filters) (Table, error)
}

type dashboardSpec struct {
	title      string
	permission identity.Permission
	filters    []string
	sections   []sectionSpec
}

// Dashboards is every dashboard: its permission and the registry metrics
// and tables it shows.
var dashboards = map[string]dashboardSpec{
	"overview": {"Executive overview", identity.PermAnalyticsExecutiveRead, nil, []sectionSpec{
		{"Money", "Cash view in TTD. There is no revenue recognition policy, so nothing here is recognised revenue.",
			[]string{"revenue.ordered_value", "revenue.collected", "revenue.refunded", "revenue.net_collected", "revenue.collected_test", "orders.average_value"}, nil},
		{"Customers and subscriptions", "", []string{"subscriptions.active", "subscriptions.new", "subscriptions.cancelled", "customers.repeat_rate"}, nil},
		{"Operations", "", []string{"meals.kits_ordered", "production.accepted_kits", "production.completion_rate", "delivery.delivered",
			"delivery.first_attempt_rate", "procurement.po_value", "inventory.value_on_hand", "operations.open_exceptions"}, nil},
		{"Across the business", "Facts placed side by side. A relationship between columns is not a cause.", nil,
			[]func(context.Context, *Service, db.Querier, Range, Filters) (Table, error){crossWeekly}},
	}},
	"revenue": {"Revenue and subscriptions", identity.PermAnalyticsRevenueRead, []string{"plan"}, []sectionSpec{
		{"Money", "Ordered, charged, collected and refunded are different things, never added together.",
			[]string{"revenue.ordered_value", "revenue.charged_value", "revenue.collected", "revenue.refunded", "revenue.net_collected",
				"revenue.collected_test", "revenue.outstanding", "orders.count", "orders.average_value"}, []func(context.Context, *Service, db.Querier, Range, Filters) (Table, error){collectedByPlan}},
		{"Payments", "", []string{"revenue.failed_payments", "revenue.cycles_failed"}, nil},
		{"Subscriptions", "From explicit subscription events; a failed payment alone is never counted as a cancellation.",
			[]string{"subscriptions.active", "subscriptions.new", "subscriptions.renewals", "subscriptions.pauses", "subscriptions.cancelled",
				"subscriptions.expired", "subscriptions.plan_changes"}, []func(context.Context, *Service, db.Querier, Range, Filters) (Table, error){activeByPlan}},
	}},
	"customers": {"Customers and retention", identity.PermAnalyticsCustomersRead, nil, []sectionSpec{
		{"Customers", "", []string{"customers.new", "customers.first_paid", "customers.repeat_rate"}, nil},
		{"Cohorts", "Customers grouped by the month of their first successful payment; the share with a paid order in each later month. Empty and small cohorts are shown as counts only.",
			nil, []func(context.Context, *Service, db.Querier, Range, Filters) (Table, error){cohortTable}},
		{"Retention signals", "Signals, not predictions: none of these proves a customer will leave, and nothing is changed for the customer because of them.",
			nil, []func(context.Context, *Service, db.Querier, Range, Filters) (Table, error){signalTable}},
	}},
	"meals": {"Meal performance", identity.PermAnalyticsProductionRead, []string{"plan", "meal"}, []sectionSpec{
		{"Demand", "Confirmed orders only; availability is shown beside demand so an unavailable meal is not read as unpopular.",
			[]string{"meals.kits_ordered"}, []func(context.Context, *Service, db.Querier, Range, Filters) (Table, error){mealTable}},
	}},
	"production": {"Production", identity.PermAnalyticsProductionRead, []string{"meal"}, []sectionSpec{
		{"Output", "Produced = accepted + rejected; reworked kits are inside accepted and are never added twice.",
			[]string{"production.planned_kits", "production.produced_kits", "production.accepted_kits", "production.rejected_kits",
				"production.reworked_kits", "production.completion_rate", "production.batch_hours"}, []func(context.Context, *Service, db.Querier, Range, Filters) (Table, error){productionByMeal}},
		{"Quality and waste", "", []string{"production.quality_holds", "production.waste_records"}, nil},
	}},
	"procurement": {"Procurement", identity.PermAnalyticsProcurementRead, []string{"supplier"}, []sectionSpec{
		{"Purchasing", "Factual measures per supplier; there are no supplier ratings.",
			[]string{"procurement.po_value", "procurement.po_count", "procurement.cycle_days", "procurement.on_time_rate", "procurement.rejected_packs",
				"procurement.overdue_orders"}, []func(context.Context, *Service, db.Querier, Range, Filters) (Table, error){supplierTable}},
	}},
	"inventory": {"Inventory", identity.PermAnalyticsInventoryRead, nil, []sectionSpec{
		{"Stock now", "From the inventory ledger; there is no second stock record.",
			[]string{"inventory.value_on_hand", "inventory.lots_without_cost", "inventory.lots_quarantined", "inventory.lots_expired"},
			[]func(context.Context, *Service, db.Querier, Range, Filters) (Table, error){ledgerCheck}},
		{"Movements", "", []string{"inventory.issue_movements", "inventory.waste_movements"}, nil},
	}},
	"support": {"Support chat", identity.PermSupportChatAnalytics, nil, []sectionSpec{
		{"Volume", "Counts only.", []string{"support.conversations_new", "support.conversations_resolved", "support.open_now",
			"support.unassigned_now", "support.backlog_now"}, nil},
		{"Speed", "Clock hours, including outside business hours.", []string{"support.first_response_hours", "support.resolution_hours"}, nil},
		{"Feedback", "Customers rate a conversation 1 to 5 if they choose to; shown as counts, never averaged into a score.",
			[]string{"support.feedback_responses", "support.feedback_low", "support.feedback_high"}, nil},
	}},
	"delivery": {"Delivery", identity.PermAnalyticsDeliveryRead, []string{"zone"}, []sectionSpec{
		{"Outcomes", "Corrected outcomes count under their corrected status; disputed deliveries are never counted as delivered.",
			[]string{"delivery.scheduled", "delivery.delivered", "delivery.failed_attempts", "delivery.first_attempt_rate", "delivery.on_time_rate",
				"delivery.returned", "delivery.disputed", "delivery.cost_actual"}, []func(context.Context, *Service, db.Querier, Range, Filters) (Table, error){zoneTable}},
	}},
}

// DashboardKinds lists the dashboards in navigation order.
var DashboardKinds = []string{"overview", "revenue", "customers", "meals", "production", "procurement", "inventory", "delivery", "support"}

// Permission returns the dashboard's permission.
func Permission(kind string) (identity.Permission, bool) {
	d, ok := dashboards[kind]
	return d.permission, ok
}

func (s *Service) Dashboard(ctx context.Context, kind string, r Range, f Filters) (*Dashboard, error) {
	spec, ok := dashboards[kind]
	if !ok {
		return nil, httpx.ErrNotFound
	}
	if err := f.validate(); err != nil {
		return nil, err
	}
	prev := r.Previous()
	out := &Dashboard{Kind: kind, Title: spec.title, From: r.From.Format(day), To: r.To.Format(day), PreviousFrom: prev.From.Format(day),
		PreviousTo: prev.To.Format(day), TimeZone: s.Location.String(), GeneratedAt: s.Now().UTC(), Notices: []string{}, Sections: []Section{},
		Filters: spec.filters}
	if out.Filters == nil {
		out.Filters = []string{}
	}
	err := s.readTx(ctx, func(tx pgx.Tx) error {
		for _, sec := range spec.sections {
			o := Section{Title: sec.title, Note: sec.note, Metrics: []MetricValue{}, Tables: []Table{}}
			for _, id := range sec.metrics {
				m, ok := Get(id)
				if !ok {
					return errors.New("analytics: unknown metric " + id)
				}
				mv, err := s.metricValue(ctx, tx, m, r, prev, f)
				if err != nil {
					return err
				}
				o.Metrics = append(o.Metrics, *mv)
			}
			for _, t := range sec.tables {
				tb, err := t(ctx, s, tx, r, f)
				if err != nil {
					return err
				}
				o.Tables = append(o.Tables, tb)
			}
			out.Sections = append(out.Sections, o)
		}
		notices, err := s.originNotices(ctx, tx, r)
		if err != nil {
			return err
		}
		out.Notices = notices
		var last *time.Time
		if err := tx.QueryRow(ctx, `SELECT max(finished_at) FROM analytics_refresh_runs WHERE status = 'succeeded'`).Scan(&last); err != nil {
			return err
		}
		out.LastRefresh = last
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) metricValue(ctx context.Context, q db.Querier, m *Metric, r, prev Range, f Filters) (*MetricValue, error) {
	mv := &MetricValue{ID: m.ID, Name: m.Name, Kind: m.Kind, Definition: m.Definition, Limitations: m.Limitations}
	if m.Kind == Money {
		mv.Currency = "TTD" // the code; the catalogue's Currency field explains the rule
	}
	// A metric ignores filters it does not support (the definition lists them).
	ff := Filters{}
	for _, name := range m.Filters {
		switch name {
		case "plan":
			ff.Plan = f.Plan
		case "meal":
			ff.Meal = f.Meal
		case "zone":
			ff.Zone = f.Zone
		case "supplier":
			ff.Supplier = f.Supplier
		}
	}
	var err error
	if mv.Value, err = s.Value(ctx, q, m, r, ff); err != nil {
		return nil, err
	}
	// Point-in-time figures ("Now: …") have no previous period to compare.
	if !strings.HasPrefix(m.Definition, "Now:") && m.ID != "procurement.overdue_orders" {
		if mv.Previous, err = s.Value(ctx, q, m, prev, ff); err != nil {
			return nil, err
		}
	}
	if mv.Value != nil && mv.Previous != nil && *mv.Previous != 0 {
		c := (*mv.Value - *mv.Previous) / math.Abs(*mv.Previous)
		mv.Change = &c
	}
	return mv, nil
}

// originNotices say when figures include test or demo records.
func (s *Service) originNotices(ctx context.Context, q db.Querier, r Range) ([]string, error) {
	ft, tt := s.bounds(r)
	var total, test int64
	if err := q.QueryRow(ctx, `SELECT coalesce(sum(amount_minor), 0), coalesce(sum(amount_minor) FILTER (WHERE provider = 'testpay'), 0)
		FROM payments WHERE succeeded_at >= $1 AND succeeded_at < $2`, ft, tt).Scan(&total, &test); err != nil {
		return nil, err
	}
	out := []string{}
	if test > 0 {
		out = append(out, fmt.Sprintf("%d%% of the money collected in this range went through the simulated test payment provider. These figures describe test and demo activity, not real business.", int(math.Round(float64(test)*100/float64(total)))))
	}
	var demoMeals int
	if err := q.QueryRow(ctx, `SELECT count(DISTINCT i.meal_id) FROM order_items i JOIN orders o ON o.id = i.order_id JOIN meals m ON m.id = i.meal_id
		WHERE m.is_demo AND o.created_at >= $1 AND o.created_at < $2`, ft, tt).Scan(&demoMeals); err != nil {
		return nil, err
	}
	if demoMeals > 0 {
		out = append(out, fmt.Sprintf("%d demo meal(s) were ordered in this range.", demoMeals))
	}
	return out, nil
}

// MetricsCSV renders a dashboard as CSV rows (aggregates only).
func (d *Dashboard) CSV() [][]string {
	rows := [][]string{{"section", "metric_id", "metric", "kind", "value", "previous", "from", "to", "currency"}}
	f := func(v *float64) string {
		if v == nil {
			return ""
		}
		return fmt.Sprintf("%.4f", *v)
	}
	for _, s := range d.Sections {
		for _, m := range s.Metrics {
			rows = append(rows, []string{s.Title, m.ID, m.Name, string(m.Kind), f(m.Value), f(m.Previous), d.From, d.To, m.Currency})
		}
		for _, t := range s.Tables {
			rows = append(rows, append([]string{s.Title, "table", t.Title}, t.Columns...))
			for _, r := range t.Rows {
				rows = append(rows, append([]string{s.Title, "row", t.Title}, r...))
			}
		}
	}
	return rows
}

// SafeCSV stops spreadsheet formula injection.
func SafeCSV(v string) string {
	if v != "" && strings.ContainsAny(v[:1], "=+-@\t\r") {
		// Negative numbers stay numbers.
		if _, err := fmt.Sscanf(v, "%f", new(float64)); err == nil && v[0] == '-' {
			return v
		}
		return "'" + v
	}
	return v
}

var errForbidden = httpx.NewError(http.StatusForbidden, "forbidden", "You do not have access to this.")

// Values computes registry metrics for an inclusive local date range, for
// reports outside the analytics dashboards that show the same figures. They
// read the registry instead of keeping their own SQL, so one figure has one
// definition everywhere. A nil value is undefined, never zero.
func (s *Service) Values(ctx context.Context, ids []string, from, to string) (map[string]*float64, error) {
	r, err := s.ParseRange(from, to)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*float64, len(ids))
	err = s.readTx(ctx, func(tx pgx.Tx) error {
		for _, id := range ids {
			m, ok := Get(id)
			if !ok {
				return fmt.Errorf("analytics: unknown metric %q", id)
			}
			v, err := s.Value(ctx, tx, m, r, Filters{})
			if err != nil {
				return err
			}
			out[id] = v
		}
		return nil
	})
	return out, err
}
