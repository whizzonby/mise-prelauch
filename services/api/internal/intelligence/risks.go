package intelligence

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"mise.tt/api/internal/analytics"
	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/finance"
	"mise.tt/api/internal/finance/scenario"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/httpx"
)

// Risk indicators are deterministic and explainable: a measure, its value
// in the period and the comparison period, a threshold and the resulting
// severity. They never carry a probability.

// Severities and statuses.
const (
	SevNone   = "none"
	SevWatch  = "watch"
	SevHigh   = "high"
	Triggered = "triggered"
	Clear     = "clear"
	NotEval   = "not_evaluable"
)

// RiskMetric is one figure behind a risk.
type RiskMetric struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Current  *float64 `json:"current"`
	Previous *float64 `json:"previous"`
	Unit     string   `json:"unit"`
}

// Threshold is a risk's configured threshold.
type Threshold struct {
	Key       string    `json:"key"`
	Enabled   bool      `json:"enabled"`
	Warn      float64   `json:"warn"`
	Critical  float64   `json:"critical"`
	Direction string    `json:"direction"` // above or below: which side is worse
	Unit      string    `json:"unit"`
	IsDefault bool      `json:"is_default"`
	Note      string    `json:"note"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Risk is one evaluated indicator.
type Risk struct {
	Key          string       `json:"key"`
	Title        string       `json:"title"`
	Domain       string       `json:"domain"`
	Status       string       `json:"status"`
	Severity     string       `json:"severity"`
	Measure      string       `json:"measure"`
	Value        *float64     `json:"value"`
	Threshold    Threshold    `json:"threshold"`
	Metrics      []RiskMetric `json:"metrics"`
	From         string       `json:"from"`
	To           string       `json:"to"`
	PreviousFrom string       `json:"previous_from"`
	PreviousTo   string       `json:"previous_to"`
	Explanation  string       `json:"explanation"`
	ReviewAction string       `json:"review_action"`
	ReviewHref   string       `json:"review_href"`
	Freshness    time.Time    `json:"freshness"`
	Limitations  []string     `json:"limitations"`
}

type riskDef struct {
	key, title, domain, measure, unit, direction, action, href string
	requires                                                    []identity.Permission
	eval                                                        func(ctx context.Context, b *Toolbox, c *riskCtx) (value *float64, metrics []RiskMetric, why string, err error)
}

type riskCtx struct {
	cur, prev     analytics.Range
	curV, prevV   map[string]*float64
	curF, prevF   *finance.Summary
	fromS, toS    string
	pFromS, pToS  string
}

func pct(v float64) *float64 { x := math.Round(v*100) / 100; return &x }

func change(cur, prev *float64) *float64 {
	if cur == nil || prev == nil || *prev == 0 {
		return nil
	}
	return pct((*cur - *prev) / math.Abs(*prev) * 100)
}

func m(id, label, unit string, c *riskCtx) RiskMetric {
	return RiskMetric{ID: id, Label: label, Current: c.curV[id], Previous: c.prevV[id], Unit: unit}
}

func lineAmount(s *finance.Summary, key string) (*float64, bool) {
	for _, l := range s.Lines {
		if l.Key == key {
			if l.AmountMinor == nil {
				return nil, false
			}
			v := float64(*l.AmountMinor)
			return &v, l.Coverage == finance.Complete
		}
	}
	return nil, false
}

func resultAmount(s *finance.Summary, key string) *float64 {
	for _, r := range s.Results {
		if r.Key == key && r.AmountMinor != nil {
			v := float64(*r.AmountMinor)
			return &v
		}
	}
	return nil
}

var riskDefs = []riskDef{
	{key: "renewals_declining", title: "Declining subscription renewals", domain: DomainExecutive, measure: "Change in successful renewals vs the previous period", unit: "percent", direction: "below",
		action: "Review renewal failures and cancellations.", href: "/subscriptions", requires: []identity.Permission{identity.PermAnalyticsRevenueRead},
		eval: func(_ context.Context, _ *Toolbox, c *riskCtx) (*float64, []RiskMetric, string, error) {
			mm := []RiskMetric{m("subscriptions.renewals", "Successful renewals", "count", c)}
			if p := c.prevV["subscriptions.renewals"]; p == nil || *p < 5 {
				return nil, mm, "Fewer than 5 renewals in the previous period: too few to compare.", nil
			}
			return change(c.curV["subscriptions.renewals"], c.prevV["subscriptions.renewals"]), mm, "", nil
		}},
	{key: "payment_failures", title: "Repeated payment failures", domain: DomainFinancial, measure: "Failed payments in the period", unit: "count", direction: "above",
		action: "Review failed payments and past-due subscriptions.", href: "/payments", requires: []identity.Permission{identity.PermAnalyticsRevenueRead},
		eval: func(_ context.Context, _ *Toolbox, c *riskCtx) (*float64, []RiskMetric, string, error) {
			return c.curV["revenue.failed_payments"], []RiskMetric{m("revenue.failed_payments", "Failed payments", "count", c)}, "", nil
		}},
	{key: "refunds_growing", title: "Growing refund volume", domain: DomainFinancial, measure: "Refunds as a share of collections", unit: "percent", direction: "above",
		action: "Review refunds and their reasons.", href: "/payments/refunds", requires: []identity.Permission{identity.PermAnalyticsRevenueRead},
		eval: func(_ context.Context, _ *Toolbox, c *riskCtx) (*float64, []RiskMetric, string, error) {
			mm := []RiskMetric{m("revenue.refunded", "Refunded", "money_minor", c), m("revenue.collected", "Collected", "money_minor", c)}
			col, ref := c.curV["revenue.collected"], c.curV["revenue.refunded"]
			if col == nil || *col == 0 || ref == nil {
				return nil, mm, "No collections in the period.", nil
			}
			return pct(*ref / *col * 100), mm, "", nil
		}},
	{key: "ingredient_costs_rising", title: "Increasing ingredient costs", domain: DomainOperational, measure: "Average change in received unit cost, per ingredient received in both periods", unit: "percent", direction: "above",
		action: "Review supplier prices and alternatives through procurement.", href: "/procurement/suppliers", requires: []identity.Permission{identity.PermAnalyticsInventoryRead},
		eval: func(ctx context.Context, b *Toolbox, c *riskCtx) (*float64, []RiskMetric, string, error) {
			var avg *float64
			var n int64
			err := b.Pool.QueryRow(ctx, `WITH p AS (
				SELECT ingredient_id, unit, avg(unit_cost_minor) FILTER (WHERE (received_at AT TIME ZONE 'America/Port_of_Spain')::date BETWEEN $1 AND $2) AS cur,
				  avg(unit_cost_minor) FILTER (WHERE (received_at AT TIME ZONE 'America/Port_of_Spain')::date BETWEEN $3 AND $4) AS prev
				FROM inventory_lots WHERE unit_cost_minor IS NOT NULL AND currency = 'TTD' GROUP BY ingredient_id, unit)
				SELECT (avg((cur - prev) / prev * 100))::float8, count(*) FROM p WHERE cur IS NOT NULL AND prev > 0`, c.cur.From, c.cur.To, c.prev.From, c.prev.To).Scan(&avg, &n)
			if err != nil {
				return nil, nil, "", err
			}
			cnt := float64(n)
			mm := []RiskMetric{{ID: "inventory_lots.unit_cost_minor", Label: "Ingredients received in both periods", Current: &cnt, Unit: "count"}}
			if n == 0 {
				return nil, mm, "No ingredient was received with a cost in both periods.", nil
			}
			return pct(*avg), mm, "", nil
		}},
	{key: "contribution_falling", title: "Falling contribution margin", domain: DomainFinancial, measure: "Change in contribution margin as a share of net collections (percentage points)", unit: "percentage_points", direction: "below",
		action: "Review the financial summary and the cost lines behind it.", href: "/finance/summary", requires: []identity.Permission{identity.PermFinanceExpensesRead},
		eval: func(_ context.Context, _ *Toolbox, c *riskCtx) (*float64, []RiskMetric, string, error) {
			share := func(s *finance.Summary) *float64 {
				cm, net := resultAmount(s, "contribution_margin"), resultAmount(s, "net_collections")
				if cm == nil || net == nil || *net == 0 {
					return nil
				}
				return pct(*cm / *net * 100)
			}
			cur, prev := share(c.curF), share(c.prevF)
			mm := []RiskMetric{{ID: "finance.contribution_margin_share", Label: "Contribution margin share of net collections", Current: cur, Previous: prev, Unit: "percent"}}
			if cur == nil || prev == nil {
				return nil, mm, "The contribution margin is withheld (incomplete cost records) or there are no net collections in one of the periods.", nil
			}
			return pct(*cur - *prev), mm, "", nil
		}},
	{key: "inventory_expiry", title: "Expired stock on hand", domain: DomainOperational, measure: "Lots past expiry that still have stock", unit: "count", direction: "above",
		action: "Review expired lots and dispose or adjust through inventory.", href: "/inventory/lots", requires: []identity.Permission{identity.PermAnalyticsInventoryRead},
		eval: func(_ context.Context, _ *Toolbox, c *riskCtx) (*float64, []RiskMetric, string, error) {
			return c.curV["inventory.lots_expired"], []RiskMetric{m("inventory.lots_expired", "Expired lots with stock (now)", "count", c)}, "", nil
		}},
	{key: "supplier_delays", title: "Supplier delivery delays", domain: DomainOperational, measure: "Purchase orders received on time", unit: "percent", direction: "below",
		action: "Review late purchase orders with the suppliers concerned.", href: "/procurement/purchase-orders", requires: []identity.Permission{identity.PermAnalyticsProcurementRead},
		eval: func(_ context.Context, _ *Toolbox, c *riskCtx) (*float64, []RiskMetric, string, error) {
			mm := []RiskMetric{m("procurement.on_time_rate", "On-time rate", "ratio", c), m("procurement.overdue_orders", "Overdue orders", "count", c)}
			v := c.curV["procurement.on_time_rate"]
			if v == nil {
				return nil, mm, "No purchase orders were received in the period.", nil
			}
			return pct(*v * 100), mm, "", nil
		}},
	{key: "excessive_waste", title: "Excessive waste", domain: DomainOperational, measure: "Ingredient waste cost as a share of consumption cost", unit: "percent", direction: "above",
		action: "Review waste records by reason and batch.", href: "/production/waste", requires: []identity.Permission{identity.PermFinanceExpensesRead},
		eval: func(_ context.Context, _ *Toolbox, c *riskCtx) (*float64, []RiskMetric, string, error) {
			waste, wOK := lineAmount(c.curF, "ingredient_waste")
			cons, cOK := lineAmount(c.curF, "ingredient_consumption")
			mm := []RiskMetric{{ID: "finance.ingredient_waste", Label: "Ingredient waste cost", Current: waste, Unit: "money_minor"},
				{ID: "finance.ingredient_consumption", Label: "Ingredient consumption cost", Current: cons, Unit: "money_minor"}}
			if !wOK || !cOK || cons == nil || *cons == 0 {
				return nil, mm, "Waste or consumption cost is incomplete (lots without cost) or zero.", nil
			}
			return pct(*waste / *cons * 100), mm, "", nil
		}},
	{key: "delivery_failures", title: "Delivery failure increase", domain: DomainOperational, measure: "First-attempt delivery success", unit: "percent", direction: "below",
		action: "Review failed deliveries by zone and partner.", href: "/delivery/exceptions", requires: []identity.Permission{identity.PermAnalyticsDeliveryRead},
		eval: func(_ context.Context, _ *Toolbox, c *riskCtx) (*float64, []RiskMetric, string, error) {
			mm := []RiskMetric{m("delivery.first_attempt_rate", "First-attempt rate", "ratio", c), m("delivery.failed_attempts", "Failed attempts", "count", c)}
			v := c.curV["delivery.first_attempt_rate"]
			if v == nil {
				return nil, mm, "No deliveries were completed in the period.", nil
			}
			return pct(*v * 100), mm, "", nil
		}},
	{key: "incomplete_financial_records", title: "Incomplete financial records", domain: DomainFinancial, measure: "Expense categories not recorded or confirmed for every month", unit: "count", direction: "above",
		action: "Record the missing expenses, or confirm there was nothing to record.", href: "/finance/expenses", requires: []identity.Permission{identity.PermFinanceExpensesRead},
		eval: func(_ context.Context, _ *Toolbox, c *riskCtx) (*float64, []RiskMetric, string, error) {
			n := 0.0
			for _, cat := range c.curF.Categories {
				if cat.Coverage == finance.Missing || cat.Coverage == finance.Partial {
					n++
				}
			}
			total := float64(len(c.curF.Categories))
			return &n, []RiskMetric{{ID: "finance.categories_missing", Label: "Categories missing or partial", Current: &n, Unit: "count"},
				{ID: "finance.categories", Label: "Categories", Current: &total, Unit: "count"}}, "", nil
		}},
	{key: "cash_shortage", title: "Potential cash shortage", domain: DomainFinancial, measure: "Weeks until projected cash turns negative (base scenario, 90 days)", unit: "weeks", direction: "below",
		action: "Review the base scenario and the cash balance with the owner.", href: "/intelligence/command-center?tab=outlook", requires: []identity.Permission{identity.PermIntelligenceScenarioRun},
		eval: func(ctx context.Context, b *Toolbox, _ *riskCtx) (*float64, []RiskMetric, string, error) {
			if b.Scenarios == nil {
				return nil, nil, "The scenario engine is not available.", nil
			}
			proj, _, err := b.Scenarios.Resolve(ctx, scenario.Request{HorizonDays: 90, Preset: scenario.Base})
			if err != nil {
				return nil, nil, "", err
			}
			o := proj.Output
			if !o.Complete {
				var names []string
				for _, k := range o.Missing {
					if v, ok := scenario.Find(k); ok {
						names = append(names, strings.ToLower(v.Label))
					}
				}
				return nil, nil, "The base scenario cannot be calculated: " + strings.Join(names, ", ") + " are not recorded.", nil
			}
			if !o.Cash.OpeningAvailable {
				return nil, nil, "No approved cash balance is recorded.", nil
			}
			mm := []RiskMetric{{ID: "scenario.lowest_cash_minor", Label: "Lowest projected cash", Unit: "money_minor"}}
			if o.Cash.LowestCash != nil {
				v := float64(*o.Cash.LowestCash)
				mm[0].Current = &v
			}
			if o.Cash.FirstNegativeWk == nil {
				big := 99.0 // never negative within the horizon
				return &big, mm, "", nil
			}
			w := float64(*o.Cash.FirstNegativeWk)
			return &w, mm, "", nil
		}},
}

var riskMetricIDs = []string{"subscriptions.renewals", "revenue.failed_payments", "revenue.refunded", "revenue.collected", "inventory.lots_expired",
	"procurement.on_time_rate", "procurement.overdue_orders", "delivery.first_attempt_rate", "delivery.failed_attempts"}

func (d *riskDef) allowed(p *identity.Principal) bool {
	if p == nil || !p.Can(DomainPermission[d.domain]) {
		return false
	}
	for _, perm := range d.requires {
		if !p.Can(perm) {
			return false
		}
	}
	return true
}

// Thresholds returns every risk threshold.
func (b *Toolbox) Thresholds(ctx context.Context) (map[string]Threshold, error) {
	rows, err := b.Pool.Query(ctx, `SELECT key, enabled, warn::float8, critical::float8, is_default, note, updated_at FROM intelligence_risk_thresholds`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Threshold{}
	for rows.Next() {
		var t Threshold
		if err := rows.Scan(&t.Key, &t.Enabled, &t.Warn, &t.Critical, &t.IsDefault, &t.Note, &t.UpdatedAt); err != nil {
			return nil, err
		}
		for _, d := range riskDefs {
			if d.key == t.Key {
				t.Direction, t.Unit = d.direction, d.unit
			}
		}
		out[t.Key] = t
	}
	return out, rows.Err()
}

// ThresholdList is the thresholds in indicator order.
func (b *Toolbox) ThresholdList(ctx context.Context) ([]Threshold, error) {
	th, err := b.Thresholds(ctx)
	if err != nil {
		return nil, err
	}
	out := []Threshold{}
	for _, d := range riskDefs {
		out = append(out, th[d.key])
	}
	return out, nil
}

// SetThreshold changes one threshold (audited). Critical must be at least
// as bad as warn in the indicator's direction.
func (b *Toolbox) SetThreshold(ctx context.Context, p *identity.Principal, key string, enabled bool, warn, critical float64, note string) (*Threshold, error) {
	var def *riskDef
	for i := range riskDefs {
		if riskDefs[i].key == key {
			def = &riskDefs[i]
		}
	}
	if def == nil {
		return nil, httpx.ErrNotFound
	}
	note = strings.TrimSpace(note)
	if note == "" || len(note) > 300 {
		return nil, fieldErr("note", "Say why the threshold changes (at most 300 characters).")
	}
	if math.IsNaN(warn) || math.IsNaN(critical) || math.Abs(warn) > 1e9 || math.Abs(critical) > 1e9 {
		return nil, fieldErr("warn", "Out of range.")
	}
	if def.direction == "above" && critical < warn || def.direction == "below" && critical > warn {
		return nil, fieldErr("critical", fmt.Sprintf("For this indicator a lower value is %s: critical must be at least as severe as warn.", map[string]string{"above": "better", "below": "worse"}[def.direction]))
	}
	tx, err := b.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE intelligence_risk_thresholds SET enabled = $2, warn = $3, critical = $4, is_default = false, note = $5, updated_by = $6, updated_at = now() WHERE key = $1`,
		key, enabled, warn, critical, note, p.UserID); err != nil {
		return nil, err
	}
	if err := audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: "intelligence.risk_threshold_updated", TargetType: "intelligence_risk_threshold", TargetID: key,
		Metadata: map[string]any{"enabled": enabled, "warn": warn, "critical": critical, "note": note}}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	th, err := b.Thresholds(ctx)
	if err != nil {
		return nil, err
	}
	t := th[key]
	return &t, nil
}

func severity(v float64, t Threshold) string {
	worse := func(x, lim float64) bool {
		if t.Direction == "above" {
			return x >= lim
		}
		return x <= lim
	}
	switch {
	case worse(v, t.Critical):
		return SevHigh
	case worse(v, t.Warn):
		return SevWatch
	}
	return SevNone
}

// Risks evaluates the indicators the caller may see for a period (default:
// the last 28 days) against the previous period of the same length.
func (b *Toolbox) Risks(ctx context.Context, p *identity.Principal, from, to string) ([]Risk, error) {
	cur, err := b.Analytics.ParseRange(from, to)
	if err != nil {
		return nil, err
	}
	prev := cur.Previous()
	c := &riskCtx{cur: cur, prev: prev, fromS: cur.From.Format(day), toS: cur.To.Format(day), pFromS: prev.From.Format(day), pToS: prev.To.Format(day)}
	if c.curV, err = b.Analytics.Values(ctx, riskMetricIDs, c.fromS, c.toS); err != nil {
		return nil, err
	}
	if c.prevV, err = b.Analytics.Values(ctx, riskMetricIDs, c.pFromS, c.pToS); err != nil {
		return nil, err
	}
	if c.curF, err = b.Finance.Summary(ctx, c.fromS, c.toS); err != nil {
		return nil, err
	}
	if c.prevF, err = b.Finance.Summary(ctx, c.pFromS, c.pToS); err != nil {
		return nil, err
	}
	th, err := b.Thresholds(ctx)
	if err != nil {
		return nil, err
	}
	out := []Risk{}
	for i := range riskDefs {
		d := &riskDefs[i]
		if !d.allowed(p) {
			continue
		}
		t := th[d.key]
		r := Risk{Key: d.key, Title: d.title, Domain: d.domain, Measure: d.measure, Threshold: t, From: c.fromS, To: c.toS,
			PreviousFrom: c.pFromS, PreviousTo: c.pToS, ReviewAction: d.action, ReviewHref: d.href, Freshness: b.Now(), Severity: SevNone,
			Metrics: []RiskMetric{}, Limitations: []string{}}
		if t.IsDefault {
			r.Limitations = append(r.Limitations, "Threshold is an engineering default, not a business-approved value.")
		}
		if !t.Enabled {
			r.Status, r.Explanation = NotEval, "This indicator is switched off."
			out = append(out, r)
			continue
		}
		v, mm, why, err := d.eval(ctx, b, c)
		if err != nil {
			return nil, err
		}
		if mm != nil {
			r.Metrics = mm
		}
		r.Value = v
		if v == nil {
			r.Status, r.Explanation = NotEval, "Not evaluated: "+why
			out = append(out, r)
			continue
		}
		r.Severity = severity(*v, t)
		side := map[string]string{"above": "at or above", "below": "at or below"}[t.Direction]
		val := withUnit(*v, d.unit)
		if d.key == "cash_shortage" && *v == 99 {
			val = "not within the horizon"
		}
		compared := ""
		for _, m := range r.Metrics {
			if m.Previous != nil {
				compared = fmt.Sprintf(" The previous period (%s to %s) is under Evidence.", c.pFromS, c.pToS)
				break
			}
		}
		if r.Severity == SevNone {
			r.Status = Clear
			r.Explanation = fmt.Sprintf("%s: %s. Watch level %s %s, high level %s %s.%s", d.measure, val, side, withUnit(t.Warn, d.unit), side, withUnit(t.Critical, d.unit), compared)
		} else {
			r.Status = Triggered
			level := map[string]float64{SevWatch: t.Warn, SevHigh: t.Critical}[r.Severity]
			r.Explanation = fmt.Sprintf("%s: %s, %s the %s level of %s.%s No probability is implied.",
				d.measure, val, side, map[string]string{SevWatch: "watch", SevHigh: "high"}[r.Severity], withUnit(level, d.unit), compared)
		}
		out = append(out, r)
	}
	return out, nil
}

// withUnit writes a value in an indicator's unit for an explanation.
func withUnit(v float64, unit string) string {
	switch unit {
	case "percent":
		return fmt.Sprintf("%g%%", v)
	case "percentage_points":
		return fmt.Sprintf("%+g points", v)
	case "weeks":
		return fmt.Sprintf("%g weeks", v)
	}
	return fmt.Sprintf("%g", v)
}
