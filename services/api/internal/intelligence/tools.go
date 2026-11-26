// Package intelligence is Mise Intelligence: admin-only executive
// intelligence. Deterministic Go computes every number (through the
// existing analytics, finance, behaviour and forecasting services); the AI
// layer only explains tool results it is given. Missing data is reported as
// missing, never as zero; test-provider payments never count as revenue.
//
// Tools are typed and read-only. Each needs the intelligence permission for
// its domain AND the permission of the data it reads, so intelligence never
// shows anyone more than they could already see.
package intelligence

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/analytics"
	"mise.tt/api/internal/behavior"
	"mise.tt/api/internal/finance"
	"mise.tt/api/internal/forecasting"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/httpx"
)

// Domains a tool belongs to.
const (
	DomainExecutive   = "executive"
	DomainFinancial   = "financial"
	DomainCustomer    = "customer"
	DomainOperational = "operational"
	DomainGeneral     = "general"
)

// DomainPermission is the intelligence permission each domain needs.
var DomainPermission = map[string]identity.Permission{
	DomainExecutive:   identity.PermIntelligenceExecutiveRead,
	DomainFinancial:   identity.PermIntelligenceFinancialRead,
	DomainCustomer:    identity.PermIntelligenceCustomerRead,
	DomainOperational: identity.PermIntelligenceOperationalRead,
	DomainGeneral:     identity.PermIntelligenceRead,
}

// Fact is one number a tool returns. A nil Value means not available
// (missing or withheld, with the reason): never zero.
type Fact struct {
	Key        string   `json:"key"`
	Label      string   `json:"label"`
	Value      *float64 `json:"value"`
	Unit       string   `json:"unit"` // count, money_minor, ratio, number, hours, days
	Currency   string   `json:"currency,omitempty"`
	Definition string   `json:"definition,omitempty"`
	Note       string   `json:"note,omitempty"`
}

// Evidence says where a result came from.
type Evidence struct {
	Ref       string            `json:"ref"`
	Tool      string            `json:"tool"`
	Domain    string            `json:"domain"`
	From      string            `json:"from,omitempty"`
	To        string            `json:"to,omitempty"`
	Filters   map[string]string `json:"filters"`
	Freshness time.Time         `json:"freshness"`
	Sources   []string          `json:"sources"`
}

// Result is a tool's output.
type Result struct {
	Tool        string            `json:"tool"`
	Title       string            `json:"title"`
	Evidence    Evidence          `json:"evidence"`
	Facts       []Fact            `json:"facts"`
	Tables      []analytics.Table `json:"tables"`
	Limitations []string          `json:"limitations"`
	Notes       []string          `json:"notes"`
}

func (r *Result) fact(key, label string, v *float64, unit, currency, def string) {
	r.Facts = append(r.Facts, Fact{Key: key, Label: label, Value: v, Unit: unit, Currency: currency, Definition: def})
}

// Args are a tool's arguments. Each tool accepts only the ones it lists.
type Args struct {
	From     string `json:"from,omitempty"`
	To       string `json:"to,omitempty"`
	Plan     string `json:"plan,omitempty"`
	Meal     string `json:"meal,omitempty"`
	Zone     string `json:"zone,omitempty"`
	Supplier string `json:"supplier,omitempty"`
	Query    string `json:"query,omitempty"`
}

var argDescriptions = map[string]string{
	"from": "Start date YYYY-MM-DD (Trinidad and Tobago). Default: 27 days before today.",
	"to":   "End date YYYY-MM-DD, inclusive. Default: today. At most two years after from.",
	"plan": "Plan slug filter.", "meal": "Meal slug filter.", "zone": "Delivery zone filter.", "supplier": "Supplier filter.",
	"query": "Search words (3 to 200 characters).",
}

// Tool is one read-only tool.
type Tool struct {
	Name        string                `json:"name"`
	Title       string                `json:"title"`
	Description string                `json:"description"`
	Domain      string                `json:"domain"`
	Requires    []identity.Permission `json:"requires"`
	Args        []string              `json:"args"`
	Required    []string              `json:"required_args"`
	run         func(ctx context.Context, b *Toolbox, a Args) (*Result, error)
}

// Schema is the tool's argument JSON schema (for model tool calling).
func (t *Tool) Schema() map[string]any {
	props := map[string]any{}
	for _, a := range t.Args {
		p := map[string]any{"type": "string", "description": argDescriptions[a]}
		if a == "from" || a == "to" {
			p["pattern"] = `^\d{4}-\d{2}-\d{2}$`
		}
		props[a] = p
	}
	req := t.Required
	if req == nil {
		req = []string{}
	}
	return map[string]any{"type": "object", "properties": props, "required": req, "additionalProperties": false}
}

// Allowed reports whether a principal may use the tool.
func (t *Tool) Allowed(p *identity.Principal) bool {
	if p == nil || !p.Can(DomainPermission[t.Domain]) {
		return false
	}
	for _, perm := range t.Requires {
		if !p.Can(perm) {
			return false
		}
	}
	return true
}

// AIStatus is whether an AI provider is available (Stage 5 fills it in).
type AIStatus struct {
	Available bool   `json:"available"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Reason    string `json:"reason"`
}

// Toolbox runs tools over the authoritative services.
type Toolbox struct {
	Pool        *pgxpool.Pool
	Analytics   *analytics.Service
	Finance     *finance.Service
	Behavior    *behavior.Service
	Forecasting *forecasting.Service
	Now         func() time.Time
	AI          func() AIStatus
}

func (b *Toolbox) aiStatus() AIStatus {
	if b.AI == nil {
		return AIStatus{Reason: "No AI provider is configured."}
	}
	return b.AI()
}

func period(b *Toolbox, a Args) (analytics.Range, error) { return b.Analytics.ParseRange(a.From, a.To) }

const day = "2006-01-02"

func dashboardTool(name, title, desc, domain, kind string, perm identity.Permission, filters ...string) Tool {
	return Tool{Name: name, Title: title, Description: desc, Domain: domain, Requires: []identity.Permission{perm},
		Args: append([]string{"from", "to"}, filters...),
		run: func(ctx context.Context, b *Toolbox, a Args) (*Result, error) {
			r, err := period(b, a)
			if err != nil {
				return nil, err
			}
			f := analytics.Filters{Plan: a.Plan, Meal: a.Meal, Zone: a.Zone, Supplier: a.Supplier}
			d, err := b.Analytics.Dashboard(ctx, kind, r, f)
			if err != nil {
				return nil, err
			}
			res := &Result{Title: d.Title, Tables: []analytics.Table{}, Limitations: append([]string{}, d.Notices...), Notes: []string{}}
			res.Evidence = Evidence{From: d.From, To: d.To, Filters: filterMap(a), Freshness: d.GeneratedAt}
			if d.LastRefresh != nil {
				res.Evidence.Freshness = *d.LastRefresh
			}
			res.Notes = append(res.Notes, fmt.Sprintf("Previous period for comparison: %s to %s.", d.PreviousFrom, d.PreviousTo))
			for _, s := range d.Sections {
				if s.Note != "" {
					res.Notes = append(res.Notes, s.Title+": "+s.Note)
				}
				for _, m := range s.Metrics {
					unit := string(m.Kind)
					if m.Kind == analytics.Money {
						unit = "money_minor"
					}
					res.Facts = append(res.Facts, Fact{Key: m.ID, Label: m.Name, Value: m.Value, Unit: unit, Currency: m.Currency, Definition: m.Definition, Note: m.Limitations})
					if m.Previous != nil {
						res.Facts = append(res.Facts, Fact{Key: m.ID + ".previous", Label: m.Name + " (previous period)", Value: m.Previous, Unit: unit, Currency: m.Currency})
					}
					if m.Change != nil {
						res.Facts = append(res.Facts, Fact{Key: m.ID + ".change", Label: m.Name + " (relative change)", Value: m.Change, Unit: "ratio"})
					}
					res.Evidence.Sources = append(res.Evidence.Sources, "metric:"+m.ID)
				}
				for _, t := range s.Tables {
					res.Tables = append(res.Tables, t)
					res.Evidence.Sources = append(res.Evidence.Sources, "table:"+t.Title)
				}
			}
			return res, nil
		}}
}

func filterMap(a Args) map[string]string {
	m := map[string]string{}
	for k, v := range map[string]string{"plan": a.Plan, "meal": a.Meal, "zone": a.Zone, "supplier": a.Supplier} {
		if v != "" {
			m[k] = v
		}
	}
	return m
}

func f64(v int64) *float64 { x := float64(v); return &x }

func optI64(v *int64) *float64 {
	if v == nil {
		return nil
	}
	return f64(*v)
}

// Tools is every tool, in a stable order.
var Tools = []Tool{
	dashboardTool("business_overview", "Business overview", "Money (cash view), customers and subscriptions, and operations for a period, with the previous period for comparison.",
		DomainExecutive, "overview", identity.PermAnalyticsExecutiveRead),
	dashboardTool("revenue_and_subscriptions", "Revenue and subscriptions", "Ordered, charged, collected, refunded and outstanding money; failed payments; subscription starts, renewals, pauses, cancellations and plan changes.",
		DomainFinancial, "revenue", identity.PermAnalyticsRevenueRead, "plan"),
	{Name: "financial_summary", Title: "Management financial summary", Domain: DomainFinancial, Requires: []identity.Permission{identity.PermFinanceExpensesRead},
		Description: "Management (not audited) summary: collections, refunds, fees, ingredient and waste cost, delivery cost, expenses, contribution margin and operating result (withheld with reasons when inputs are missing), and the cash view.",
		Args:        []string{"from", "to"}, run: financialSummary},
	{Name: "expense_breakdown", Title: "Expenses by category", Domain: DomainFinancial, Requires: []identity.Permission{identity.PermFinanceExpensesRead},
		Description: "Approved actual and estimated expenses per category with coverage (complete, estimated, partial, missing) and the months missing.",
		Args:        []string{"from", "to"}, run: expenseBreakdown},
	dashboardTool("customer_retention", "Customers and retention", "New and first-paid customers, repeat rate, monthly cohorts and retention signals (signals, not predictions).",
		DomainCustomer, "customers", identity.PermAnalyticsCustomersRead),
	{Name: "customer_segments", Title: "Customer segments", Domain: DomainCustomer, Description: "Rule-based segments with their definitions and counts (no names).",
		run: segmentsTool},
	{Name: "conversion_funnel", Title: "Customer-app funnel", Domain: DomainCustomer, Description: "Distinct people at each step from catalogue to payment, checkout abandonment (24-hour rule) and the data's limitations.",
		Args: []string{"from", "to"}, run: funnelTool},
	{Name: "cancellation_reasons", Title: "Cancellation reasons", Domain: DomainCustomer, Description: "Reasons customers chose when cancelling; missing reasons are counted as not recorded.",
		Args: []string{"from", "to"}, run: reasonsTool},
	dashboardTool("meal_performance", "Meal performance", "Kits ordered per meal with availability beside demand.", DomainOperational, "meals", identity.PermAnalyticsProductionRead, "plan", "meal"),
	dashboardTool("production_metrics", "Production", "Planned, produced, accepted, rejected and reworked kits, completion rate, batch hours, quality holds and waste records.",
		DomainOperational, "production", identity.PermAnalyticsProductionRead, "meal"),
	dashboardTool("supplier_performance", "Procurement and suppliers", "Purchase order value and count, cycle time, on-time rate, rejected packs and overdue orders per supplier (no ratings).",
		DomainOperational, "procurement", identity.PermAnalyticsProcurementRead, "supplier"),
	dashboardTool("inventory_status", "Inventory", "Stock value on hand, lots without cost, quarantined and expired lots, issue and waste movements.",
		DomainOperational, "inventory", identity.PermAnalyticsInventoryRead),
	dashboardTool("delivery_metrics", "Delivery", "Scheduled, delivered, failed attempts, first-attempt and on-time rates, returned and disputed deliveries, delivery cost.",
		DomainOperational, "delivery", identity.PermAnalyticsDeliveryRead, "zone"),
	dashboardTool("support_trends", "Support chat", "Conversation volume, backlog, first-response and resolution hours, and feedback counts.",
		DomainOperational, "support", identity.PermSupportChatAnalytics),
	{Name: "demand_forecast", Title: "Demand forecast", Domain: DomainOperational, Requires: []identity.Permission{identity.PermForecastingRead},
		Description: "Forecast readiness against its criteria and, when a forecast has been run, the latest run's projected kits per week.",
		run:         forecastTool},
	{Name: "data_readiness", Title: "Data readiness", Domain: DomainGeneral, Description: "Which data Mise Intelligence can rely on: real vs test payments, expense coverage, demand history, behaviour data, supplier costs, delivery costs, forecasting, AI availability.",
		run: readinessTool},
	{Name: "knowledge_search", Title: "Knowledge search", Domain: DomainGeneral, Description: "Searches metric definitions and published help-centre articles.",
		Args: []string{"query"}, Required: []string{"query"}, run: knowledgeTool},
}

// Find returns a tool by name.
func Find(name string) (*Tool, bool) {
	for i := range Tools {
		if Tools[i].Name == name {
			return &Tools[i], true
		}
	}
	return nil, false
}

// Available is the tools a principal may use.
func Available(p *identity.Principal) []*Tool {
	out := []*Tool{}
	for i := range Tools {
		if Tools[i].Allowed(p) {
			out = append(out, &Tools[i])
		}
	}
	return out
}

func argErr(field, msg string) error {
	return httpx.ValidationError([]httpx.FieldError{{Field: field, Code: "invalid", Message: msg}})
}

// ParseArgs decodes arguments, refusing any the tool does not accept.
func (t *Tool) ParseArgs(raw json.RawMessage) (Args, error) {
	var a Args
	if len(raw) == 0 || string(raw) == "null" {
		raw = json.RawMessage("{}")
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return a, argErr("args", "Arguments must be a JSON object.")
	}
	allowed := map[string]bool{}
	for _, k := range t.Args {
		allowed[k] = true
	}
	for k, v := range m {
		if !allowed[k] {
			return a, argErr("args."+k, "Not an argument of "+t.Name+".")
		}
		s, ok := v.(string)
		if !ok || len(s) > 200 {
			return a, argErr("args."+k, "Must be a short string.")
		}
	}
	for _, k := range t.Required {
		if s, _ := m[k].(string); strings.TrimSpace(s) == "" {
			return a, argErr("args."+k, "Required.")
		}
	}
	_ = json.Unmarshal(raw, &a)
	return a, nil
}

// Run checks access and runs a tool. ref labels the result's evidence.
func (b *Toolbox) Run(ctx context.Context, p *identity.Principal, name string, raw json.RawMessage, ref string) (*Result, error) {
	t, ok := Find(name)
	if !ok {
		return nil, httpx.ErrNotFound
	}
	if !t.Allowed(p) {
		return nil, httpx.ErrForbidden
	}
	a, err := t.ParseArgs(raw)
	if err != nil {
		return nil, err
	}
	res, err := t.run(ctx, b, a)
	if err != nil {
		return nil, err
	}
	res.Tool = t.Name
	if res.Title == "" {
		res.Title = t.Title
	}
	res.Evidence.Ref, res.Evidence.Tool, res.Evidence.Domain = ref, t.Name, t.Domain
	if res.Evidence.Filters == nil {
		res.Evidence.Filters = map[string]string{}
	}
	if res.Evidence.Freshness.IsZero() {
		res.Evidence.Freshness = b.Now()
	}
	for _, x := range []*[]string{&res.Evidence.Sources, &res.Limitations, &res.Notes} {
		if *x == nil {
			*x = []string{}
		}
	}
	if res.Facts == nil {
		res.Facts = []Fact{}
	}
	if res.Tables == nil {
		res.Tables = []analytics.Table{}
	}
	return res, nil
}

func financialSummary(ctx context.Context, b *Toolbox, a Args) (*Result, error) {
	r, err := period(b, a)
	if err != nil {
		return nil, err
	}
	s, err := b.Finance.Summary(ctx, r.From.Format(day), r.To.Format(day))
	if err != nil {
		return nil, err
	}
	res := &Result{Evidence: Evidence{From: s.From, To: s.To, Freshness: s.GeneratedAt}, Notes: append([]string{s.Statement}, s.Notes...)}
	for _, l := range s.Lines {
		res.Facts = append(res.Facts, Fact{Key: "finance." + l.Key, Label: l.Label, Value: optI64(l.AmountMinor), Unit: "money_minor", Currency: s.Currency,
			Note: "Coverage: " + l.Coverage + noteJoin(l.Notes)})
		res.Evidence.Sources = append(res.Evidence.Sources, l.Sources...)
		if l.Coverage != finance.Complete {
			res.Limitations = append(res.Limitations, fmt.Sprintf("%s: coverage %s%s", l.Label, l.Coverage, noteJoin(l.Notes)))
		}
	}
	for _, x := range s.Results {
		f := Fact{Key: "finance." + x.Key, Label: x.Label, Value: optI64(x.AmountMinor), Unit: "money_minor", Currency: s.Currency, Definition: x.Formula}
		if x.Withheld {
			f.Note = "Withheld: " + strings.Join(x.Reasons, " ")
			res.Limitations = append(res.Limitations, x.Label+" is withheld: "+strings.Join(x.Reasons, " "))
		} else if x.Estimated {
			f.Note = "Includes estimates."
		}
		res.Facts = append(res.Facts, f)
	}
	res.fact("finance.cash.inflows", "Cash inflows ("+s.Cash.InflowBasis+" basis)", f64(s.Cash.InflowsMinor), "money_minor", s.Currency, "")
	res.fact("finance.cash.outflows", "Cash outflows (approved expenses paid)", f64(s.Cash.OutflowsMinor), "money_minor", s.Currency, "")
	res.fact("finance.cash.recorded_balance", "Latest recorded cash balance", optI64(s.Cash.BalanceMinor), "money_minor", s.Currency, "Entered from bank statements; never derived.")
	if !s.Cash.Complete {
		res.Limitations = append(res.Limitations, "Cash view incomplete: "+strings.Join(s.Cash.Reasons, " "))
	}
	res.fact("finance.test_collections", "Test-provider collections (not real money)", f64(s.TestCollections), "money_minor", s.Currency, "")
	res.fact("finance.real_payments", "Real payments", f64(int64(s.RealPayments)), "count", "", "")
	res.fact("finance.test_payments", "Test-provider payments", f64(int64(s.TestPayments)), "count", "", "")
	if s.RealPayments == 0 && s.TestPayments > 0 {
		res.Limitations = append(res.Limitations, "Every payment in this period is from the test provider: there is no real revenue to report.")
	}
	return res, nil
}

func noteJoin(n []string) string {
	if len(n) == 0 {
		return ""
	}
	return " (" + strings.Join(n, "; ") + ")"
}

func expenseBreakdown(ctx context.Context, b *Toolbox, a Args) (*Result, error) {
	r, err := period(b, a)
	if err != nil {
		return nil, err
	}
	s, err := b.Finance.Summary(ctx, r.From.Format(day), r.To.Format(day))
	if err != nil {
		return nil, err
	}
	res := &Result{Evidence: Evidence{From: s.From, To: s.To, Freshness: s.GeneratedAt, Sources: []string{"finance_expenses (approved)", "finance_recurring_expenses (approved)", "finance_coverage_confirmations"}}}
	t := analytics.Table{Title: "Expenses by category", Columns: []string{"Category", "Class", "Actual (TTD)", "Estimated (TTD)", "Coverage", "Missing months"}}
	for _, c := range s.Categories {
		res.fact("expense."+c.Key+".actual", c.Name+" (actual)", f64(c.ActualMinor), "money_minor", s.Currency, "")
		res.fact("expense."+c.Key+".estimated", c.Name+" (estimated)", f64(c.EstimatedMinor), "money_minor", s.Currency, "")
		t.Rows = append(t.Rows, []string{c.Name, c.CostClass, money(c.ActualMinor), money(c.EstimatedMinor), c.Coverage, strings.Join(c.MissingMonths, ", ")})
		if c.Coverage == finance.Missing || c.Coverage == finance.Partial {
			res.Limitations = append(res.Limitations, fmt.Sprintf("%s: %s (months with nothing recorded: %s).", c.Name, c.Coverage, strings.Join(c.MissingMonths, ", ")))
		}
	}
	res.Tables = []analytics.Table{t}
	return res, nil
}

func money(minor int64) string {
	sign := ""
	if minor < 0 {
		sign, minor = "-", -minor
	}
	return fmt.Sprintf("%s%d.%02d", sign, minor/100, minor%100)
}

func segmentsTool(ctx context.Context, b *Toolbox, _ Args) (*Result, error) {
	segs, err := b.Behavior.Segments(ctx)
	if err != nil {
		return nil, err
	}
	res := &Result{Evidence: Evidence{Freshness: b.Now()}}
	t := analytics.Table{Title: "Segments", Note: "Rules only: no sensitive attributes, no predictions.", Columns: []string{"Segment", "Customers", "Rule"}}
	for _, s := range segs {
		res.fact("segment."+s.Key, s.Name, f64(s.Count), "count", "", s.Definition)
		t.Rows = append(t.Rows, []string{s.Name, fmt.Sprint(s.Count), s.Definition})
		res.Evidence.Sources = append(res.Evidence.Sources, s.Sources...)
	}
	res.Evidence.Sources = dedupe(res.Evidence.Sources)
	res.Tables = []analytics.Table{t}
	return res, nil
}

func funnelTool(ctx context.Context, b *Toolbox, a Args) (*Result, error) {
	r, err := period(b, a)
	if err != nil {
		return nil, err
	}
	f, err := b.Behavior.Funnel(ctx, r.From.Format(day), r.To.Format(day))
	if err != nil {
		return nil, err
	}
	res := &Result{Evidence: Evidence{From: f.From, To: f.To, Freshness: b.Now(), Sources: []string{"behavior_events", "payments"}}, Limitations: f.Limitations,
		Notes: []string{"Abandonment rule: " + f.AbandonRule}}
	for _, s := range f.Steps {
		res.fact("funnel."+s.Key+".people", s.Label+" (people)", f64(s.People), "count", "", "Source: "+s.Source)
	}
	res.fact("funnel.checkouts_started", "Checkouts started by signed-in customers", f64(f.CheckoutsStarted), "count", "", "")
	res.fact("funnel.checkouts_abandoned", "Checkouts abandoned", f64(f.Abandoned), "count", "", f.AbandonRule)
	res.fact("funnel.checkouts_pending", "Checkouts younger than 24 hours (not judged)", f64(f.Pending), "count", "", "")
	return res, nil
}

func reasonsTool(ctx context.Context, b *Toolbox, a Args) (*Result, error) {
	r, err := period(b, a)
	if err != nil {
		return nil, err
	}
	list, total, err := b.Behavior.CancellationReasons(ctx, r.From.Format(day), r.To.Format(day))
	if err != nil {
		return nil, err
	}
	res := &Result{Evidence: Evidence{From: r.From.Format(day), To: r.To.Format(day), Freshness: b.Now(), Sources: []string{"subscriptions.cancel_reason"}},
		Notes: []string{"Only reasons customers chose; no reason is inferred."}}
	res.fact("cancellations.total", "Cancellations requested", f64(total), "count", "", "")
	t := analytics.Table{Title: "Cancellation reasons", Columns: []string{"Reason", "Cancellations"}}
	for _, x := range list {
		t.Rows = append(t.Rows, []string{x.Reason, fmt.Sprint(x.Count)})
	}
	res.Tables = []analytics.Table{t}
	if total > 0 && total < 10 {
		res.Limitations = append(res.Limitations, fmt.Sprintf("Only %d cancellations: too few to draw conclusions about reasons.", total))
	}
	return res, nil
}

func forecastTool(ctx context.Context, b *Toolbox, _ Args) (*Result, error) {
	rd, err := b.Forecasting.Readiness(ctx)
	if err != nil {
		return nil, err
	}
	res := &Result{Evidence: Evidence{Freshness: b.Now(), Sources: []string{"forecasting readiness", "forecast_runs"}}}
	ready := 0.0
	if rd.Ready {
		ready = 1
	}
	res.fact("forecast.ready", "Forecast criteria met (1 yes, 0 no)", &ready, "number", "", rd.Message)
	res.fact("forecast.verified_weeks", "Weeks of verified demand history", f64(int64(rd.VerifiedWeeks)), "count", "", "")
	res.fact("forecast.meals_with_history", "Meals with demand history", f64(int64(rd.MealsWithHistory)), "count", "", "")
	res.fact("forecast.test_paid_kits", "Kits paid with test payments (excluded)", &rd.TestPaidKits, "count", "", "")
	if !rd.Ready {
		res.Limitations = append(res.Limitations, "Forecasting is not ready: "+rd.Message)
		res.Limitations = append(res.Limitations, rd.Reasons...)
		return res, nil
	}
	runs, err := b.Forecasting.Runs(ctx)
	if err != nil {
		return nil, err
	}
	for _, run := range runs {
		if run.Status != "completed" {
			continue
		}
		full, err := b.Forecasting.Get(ctx, run.ID)
		if err != nil {
			return nil, err
		}
		res.Evidence.Freshness = full.CreatedAt
		res.Notes = append(res.Notes, fmt.Sprintf("Latest forecast run %s (%s), %d weeks ahead. A forecast is an estimate with a range, not a commitment.",
			full.CreatedAt.Format(day), strOr(full.Method), full.HorizonWeeks))
		weeks := map[string]float64{}
		var order []string
		for _, v := range full.Values {
			if _, ok := weeks[v.Week]; !ok {
				order = append(order, v.Week)
			}
			weeks[v.Week] += v.Expected
		}
		sort.Strings(order)
		t := analytics.Table{Title: "Expected kits per week", Columns: []string{"Week", "Expected kits (all meals)"}}
		for _, w := range order {
			x := weeks[w]
			res.fact("forecast.week."+w+".expected_kits", "Expected kits, week of "+w, &x, "count", "", "Sum of the per-meal expected values.")
			t.Rows = append(t.Rows, []string{w, fmt.Sprintf("%.1f", x)})
		}
		res.Tables = append(res.Tables, t)
		return res, nil
	}
	res.Limitations = append(res.Limitations, "Criteria are met but no forecast has been run yet.")
	return res, nil
}

func strOr(s *string) string {
	if s == nil {
		return "method not recorded"
	}
	return *s
}

func knowledgeTool(ctx context.Context, b *Toolbox, a Args) (*Result, error) {
	q := strings.TrimSpace(a.Query)
	if len(q) < 3 {
		return nil, argErr("args.query", "Search for at least 3 characters.")
	}
	res := &Result{Evidence: Evidence{Freshness: b.Now(), Sources: []string{"analytics metric registry", "help_articles (published)"}}}
	words := strings.Fields(strings.ToLower(q))
	type hit struct {
		score int
		row   []string
	}
	var hits []hit
	for _, m := range analytics.Metrics() {
		text := strings.ToLower(m.ID + " " + m.Name + " " + m.Definition + " " + m.Calculation)
		score := 0
		for _, w := range words {
			if strings.Contains(text, w) {
				score++
			}
		}
		if score > 0 {
			hits = append(hits, hit{score, []string{"metric", m.ID, m.Name, m.Definition, m.Limitations}})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if len(hits) > 8 {
		hits = hits[:8]
	}
	t := analytics.Table{Title: "Matches", Columns: []string{"Kind", "Id", "Title", "Definition or summary", "Limitations"}}
	for _, h := range hits {
		t.Rows = append(t.Rows, h.row)
	}
	rows, err := b.Pool.Query(ctx, `SELECT slug, title, summary FROM help_articles WHERE status = 'published' AND search @@ websearch_to_tsquery('english', $1)
		ORDER BY ts_rank(search, websearch_to_tsquery('english', $1)) DESC LIMIT 5`, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var slug, title, summary string
		if err := rows.Scan(&slug, &title, &summary); err != nil {
			return nil, err
		}
		t.Rows = append(t.Rows, []string{"help article", slug, title, summary, ""})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	res.Tables = []analytics.Table{t}
	if len(t.Rows) == 0 {
		res.Limitations = append(res.Limitations, "Nothing matched.")
	}
	return res, nil
}

func dedupe(s []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, x := range s {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
