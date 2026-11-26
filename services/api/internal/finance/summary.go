package finance

import (
	"context"
	"fmt"
	"sort"
	"time"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
)

func auditRecord(ctx context.Context, q db.Querier, actor, action, targetType, targetID string, meta map[string]any) error {
	return audit.Record(ctx, q, audit.Entry{ActorUserID: actor, Action: action, TargetType: targetType, TargetID: targetID, Metadata: meta})
}

// Coverage states. "missing" never becomes zero.
const (
	Complete  = "complete"
	Estimated = "estimated" // only estimates cover some months
	Partial   = "partial"
	Missing   = "missing"
)

// Line is one figure with its sources and how complete it is.
type Line struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	AmountMinor *int64   `json:"amount_minor"`
	Coverage    string   `json:"coverage"`
	Notes       []string `json:"notes"`
	Sources     []string `json:"sources"`
}

// Result is a derived figure. Withheld results carry no amount, only the reasons.
type Result struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	AmountMinor *int64   `json:"amount_minor"`
	Withheld    bool     `json:"withheld"`
	Estimated   bool     `json:"estimated"`
	Formula     string   `json:"formula"`
	Reasons     []string `json:"reasons"`
}

// CategoryCoverage is one expense category over the period.
type CategoryCoverage struct {
	Key            string   `json:"key"`
	Name           string   `json:"name"`
	CostClass      string   `json:"cost_class"`
	ActualMinor    int64    `json:"actual_minor"`
	EstimatedMinor int64    `json:"estimated_minor"`
	Coverage       string   `json:"coverage"`
	MissingMonths  []string `json:"missing_months"`
	EstimateMonths []string `json:"estimate_months"`
}

// Cash is the cash view: never complete while supplier payments are not recorded.
type Cash struct {
	InflowsMinor    int64    `json:"inflows_minor"`
	InflowBasis     string   `json:"inflow_basis"` // settlement or collections
	OutflowsMinor   int64    `json:"outflows_minor"`
	Complete        bool     `json:"complete"`
	Reasons         []string `json:"reasons"`
	BalanceMinor    *int64   `json:"recorded_balance_minor"`
	BalanceAsOf     *string  `json:"recorded_balance_as_of"`
	BalanceAccounts int      `json:"recorded_balance_accounts"`
}

// Summary is the management financial summary for a period (TTD only).
type Summary struct {
	From             string             `json:"from"`
	To               string             `json:"to"`
	Currency         string             `json:"currency"`
	Lines            []Line             `json:"lines"`
	Results          []Result           `json:"results"`
	Categories       []CategoryCoverage `json:"categories"`
	Cash             Cash               `json:"cash"`
	TestCollections  int64              `json:"test_collections_minor"`
	TestPayments     int64              `json:"test_payments"`
	RealPayments     int64              `json:"real_payments"`
	ExcludedCurrency int64              `json:"excluded_other_currency_records"`
	Notes            []string           `json:"notes"`
	GeneratedAt      time.Time          `json:"generated_at"`
	Statement        string             `json:"statement"`
}

// ParsePeriod reads an inclusive YYYY-MM-DD range (at most two years).
func ParsePeriod(from, to string) (time.Time, time.Time, error) {
	f, err1 := time.Parse("2006-01-02", from)
	t, err2 := time.Parse("2006-01-02", to)
	if err1 != nil || err2 != nil || t.Before(f) || t.Sub(f) > 731*24*time.Hour {
		return time.Time{}, time.Time{}, httpx.ValidationError([]httpx.FieldError{{Field: "from", Code: "invalid",
			Message: "Give a period as from and to dates (YYYY-MM-DD), at most two years."}})
	}
	return f, t, nil
}

func i64(v int64) *int64 { return &v }

func months(from, to time.Time) []time.Time {
	var out []time.Time
	for m := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC); !m.After(to); m = m.AddDate(0, 1, 0) {
		out = append(out, m)
	}
	return out
}

// Summary computes the management summary for [from, to] (inclusive, local dates).
func (s *Service) Summary(ctx context.Context, fromS, toS string) (*Summary, error) {
	from, to, err := ParsePeriod(fromS, toS)
	if err != nil {
		return nil, err
	}
	fromTS := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, s.Loc)
	toTS := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, s.Loc).AddDate(0, 0, 1)
	out := &Summary{From: fromS, To: toS, Currency: Currency, Lines: []Line{}, Results: []Result{}, Notes: []string{},
		GeneratedAt: s.Now().UTC(),
		Statement: "Management figures from operational records and recorded expenses. Not audited financial statements; " +
			"revenue is not recognised (no approved recognition policy)."}
	q := s.pool

	// ---- collections and refunds (real payments only; the test provider is reported apart)
	var gross, realCount, feeSum, feeCount, settleSum, settleCount int64
	if err := q.QueryRow(ctx, `SELECT coalesce(sum(amount_minor), 0), count(*),
		coalesce(sum(fee_minor) FILTER (WHERE fee_currency = 'TTD'), 0), count(*) FILTER (WHERE fee_minor IS NOT NULL AND fee_currency = 'TTD'),
		coalesce(sum(settlement_amount_minor) FILTER (WHERE settlement_currency = 'TTD'), 0), count(*) FILTER (WHERE settlement_amount_minor IS NOT NULL AND settlement_currency = 'TTD')
		FROM payments WHERE succeeded_at >= $1 AND succeeded_at < $2 AND currency = 'TTD' AND provider <> 'testpay'`, fromTS, toTS).
		Scan(&gross, &realCount, &feeSum, &feeCount, &settleSum, &settleCount); err != nil {
		return nil, err
	}
	out.RealPayments = realCount
	if err := q.QueryRow(ctx, `SELECT coalesce(sum(amount_minor), 0), count(*) FROM payments
		WHERE succeeded_at >= $1 AND succeeded_at < $2 AND currency = 'TTD' AND provider = 'testpay'`, fromTS, toTS).
		Scan(&out.TestCollections, &out.TestPayments); err != nil {
		return nil, err
	}
	var refunds int64
	if err := q.QueryRow(ctx, `SELECT coalesce(sum(r.amount_minor), 0) FROM refunds r JOIN payments p ON p.id = r.payment_id
		WHERE r.status = 'succeeded' AND r.currency = 'TTD' AND p.provider <> 'testpay' AND r.completed_at >= $1 AND r.completed_at < $2`, fromTS, toTS).
		Scan(&refunds); err != nil {
		return nil, err
	}
	var otherCurrency int64
	if err := q.QueryRow(ctx, `SELECT (SELECT count(*) FROM payments WHERE succeeded_at >= $1 AND succeeded_at < $2 AND currency <> 'TTD')
		+ (SELECT count(*) FROM finance_expenses WHERE status = 'approved' AND currency <> 'TTD' AND expense_date BETWEEN $3 AND $4)`,
		fromTS, toTS, fromS, toS).Scan(&otherCurrency); err != nil {
		return nil, err
	}
	out.ExcludedCurrency = otherCurrency
	net := gross - refunds
	out.Lines = append(out.Lines,
		Line{Key: "gross_collections", Label: "Gross collections", AmountMinor: i64(gross), Coverage: Complete,
			Sources: []string{"payments.amount_minor (succeeded, real providers)"}, Notes: []string{fmt.Sprintf("%d real payments", realCount)}},
		Line{Key: "refunds", Label: "Refunds", AmountMinor: i64(refunds), Coverage: Complete, Sources: []string{"refunds.amount_minor (succeeded)"}, Notes: []string{}},
		Line{Key: "net_collections", Label: "Net collections", AmountMinor: i64(net), Coverage: Complete,
			Sources: []string{"gross collections − refunds"}, Notes: []string{}})
	if out.TestPayments > 0 {
		out.Notes = append(out.Notes, fmt.Sprintf("%d test-provider payments (%s) are excluded: no real money moved.", out.TestPayments, money(out.TestCollections)))
	}

	feeLine := Line{Key: "provider_fees", Label: "Payment provider fees", AmountMinor: i64(feeSum), Coverage: Complete,
		Sources: []string{"payments.fee_minor (reported by the provider)"}, Notes: []string{}}
	if realCount > 0 && feeCount < realCount {
		feeLine.Coverage = Partial
		feeLine.Notes = append(feeLine.Notes, fmt.Sprintf("Fees reported for %d of %d payments.", feeCount, realCount))
	}
	out.Lines = append(out.Lines, feeLine)

	// ---- ingredient consumption and waste (inventory movements × lot cost)
	consumption, consLine := s.movementCost(ctx, "issue", "ingredient_consumption", "Ingredient consumption", fromTS, toTS)
	waste, wasteLine := s.movementCost(ctx, "waste", "ingredient_waste", "Ingredient waste", fromTS, toTS)
	if consLine == nil || wasteLine == nil {
		return nil, fmt.Errorf("finance: movement costs")
	}
	out.Lines = append(out.Lines, *consLine, *wasteLine)

	// ---- delivery costs (as the analytics registry defines them)
	var delivery, deliveredNoCost int64
	if err := q.QueryRow(ctx, `SELECT coalesce(sum(c.amount_minor), 0) FROM delivery_cost_records c
		LEFT JOIN deliveries d ON d.id = c.delivery_id LEFT JOIN dispatch_runs r ON r.id = c.run_id
		WHERE c.kind IN ('actual', 'adjustment') AND c.currency = 'TTD' AND coalesce(d.service_date, r.service_date) BETWEEN $1 AND $2`, fromS, toS).
		Scan(&delivery); err != nil {
		return nil, err
	}
	if err := q.QueryRow(ctx, `SELECT count(*) FROM deliveries d WHERE d.status = 'delivered' AND d.service_date BETWEEN $1 AND $2
		AND NOT EXISTS (SELECT 1 FROM delivery_cost_records c WHERE c.kind IN ('actual', 'adjustment')
			AND (c.delivery_id = d.id OR c.run_id IN (SELECT st.run_id FROM dispatch_stops st WHERE st.delivery_id = d.id)))`, fromS, toS).Scan(&deliveredNoCost); err != nil {
		return nil, err
	}
	dl := Line{Key: "delivery_costs", Label: "Delivery costs", AmountMinor: i64(delivery), Coverage: Complete,
		Sources: []string{"delivery_cost_records (actual and adjustment)"}, Notes: []string{}}
	if deliveredNoCost > 0 {
		dl.Coverage = Partial
		dl.Notes = append(dl.Notes, fmt.Sprintf("%d delivered deliveries have no actual cost recorded.", deliveredNoCost))
	}
	out.Lines = append(out.Lines, dl)

	// ---- expense categories
	cats, err := s.categoryCoverage(ctx, from, to)
	if err != nil {
		return nil, err
	}
	out.Categories = cats

	// ---- results
	byClass := func(classes ...string) (sum int64, missing []string, estimated bool) {
		for _, c := range cats {
			for _, cl := range classes {
				if c.CostClass != cl {
					continue
				}
				sum += c.ActualMinor + c.EstimatedMinor
				if c.Coverage == Missing || c.Coverage == Partial {
					missing = append(missing, fmt.Sprintf("%s (no record or confirmation for %v)", c.Name, c.MissingMonths))
				}
				if c.Coverage == Estimated || c.EstimatedMinor > 0 {
					estimated = true
				}
			}
		}
		return
	}
	incomplete := func(lines ...Line) []string {
		var r []string
		for _, l := range lines {
			if l.Coverage == Partial || l.Coverage == Missing {
				r = append(r, l.Label+": "+joinNotes(l.Notes))
			}
		}
		return r
	}

	varCats, varMissing, varEst := byClass("variable")
	contribReasons := append(incomplete(feeLine, *consLine, *wasteLine, dl), varMissing...)
	contribution := net - feeSum - consumption - waste - delivery - varCats
	cr := Result{Key: "contribution_margin", Label: "Contribution margin", Formula: "net collections − provider fees − ingredient consumption − waste − delivery − variable expense categories (packaging, transportation, processing fees)", Reasons: contribReasons, Estimated: varEst}
	if len(contribReasons) > 0 {
		cr.Withheld = true
	} else {
		cr.AmountMinor = i64(contribution)
	}

	pack := catAmount(cats, "packaging")
	gmReasons := incomplete(*consLine, *wasteLine)
	if c := catByKey(cats, "packaging"); c != nil && (c.Coverage == Missing || c.Coverage == Partial) {
		gmReasons = append(gmReasons, fmt.Sprintf("Packaging (no record or confirmation for %v)", c.MissingMonths))
	}
	gm := Result{Key: "estimated_gross_margin", Label: "Estimated gross margin", Formula: "net collections − ingredient consumption − waste − packaging", Reasons: gmReasons, Estimated: true}
	if len(gmReasons) > 0 {
		gm.Withheld = true
	} else {
		gm.AmountMinor = i64(net - consumption - waste - pack)
	}

	fixed, fixedMissing, fixedEst := byClass("fixed", "labor", "marketing")
	orReasons := append(append([]string{}, contribReasons...), fixedMissing...)
	or := Result{Key: "estimated_operating_result", Label: "Estimated operating result", Formula: "contribution margin − labour − marketing − fixed expense categories", Reasons: orReasons, Estimated: true}
	if len(orReasons) > 0 {
		or.Withheld = true
	} else {
		or.AmountMinor = i64(contribution - fixed)
		or.Estimated = varEst || fixedEst
	}
	out.Results = append(out.Results,
		Result{Key: "net_collections", Label: "Net collections", AmountMinor: i64(net), Formula: "gross collections − refunds", Reasons: []string{}},
		gm, cr, or,
		Result{Key: "recognised_revenue", Label: "Recognised revenue", Withheld: true, Formula: "—",
			Reasons: []string{"No approved revenue recognition policy. Collections are cash received, not recognised revenue."}})

	// ---- cash
	cash, err := s.cash(ctx, fromS, toS, net, realCount, settleCount, settleSum, refunds, toTS)
	if err != nil {
		return nil, err
	}
	out.Cash = *cash
	return out, nil
}

func joinNotes(n []string) string {
	if len(n) == 0 {
		return "incomplete"
	}
	s := n[0]
	for _, x := range n[1:] {
		s += " " + x
	}
	return s
}

func money(minor int64) string {
	sign := ""
	if minor < 0 {
		sign, minor = "-", -minor
	}
	return fmt.Sprintf("%sTTD %d.%02d", sign, minor/100, minor%100)
}

func catAmount(cats []CategoryCoverage, key string) int64 {
	if c := catByKey(cats, key); c != nil {
		return c.ActualMinor + c.EstimatedMinor
	}
	return 0
}

func catByKey(cats []CategoryCoverage, key string) *CategoryCoverage {
	for i := range cats {
		if cats[i].Key == key {
			return &cats[i]
		}
	}
	return nil
}

// movementCost costs inventory movements of one type at their lot's unit
// cost (exact numeric arithmetic, rounded to the cent once at the end).
func (s *Service) movementCost(ctx context.Context, typ, key, label string, fromTS, toTS time.Time) (int64, *Line) {
	var cost int64
	var moves, noCost int64
	err := s.pool.QueryRow(ctx, `SELECT coalesce(round(sum(-m.on_hand_delta * l.unit_cost_minor) FILTER (WHERE l.unit_cost_minor IS NOT NULL AND l.currency = 'TTD')), 0)::bigint,
		count(*), count(*) FILTER (WHERE l.unit_cost_minor IS NULL OR l.currency IS DISTINCT FROM 'TTD')
		FROM inventory_movements m JOIN inventory_lots l ON l.id = m.lot_id
		WHERE m.type = $1 AND m.created_at >= $2 AND m.created_at < $3`, typ, fromTS, toTS).Scan(&cost, &moves, &noCost)
	if err != nil {
		return 0, nil
	}
	l := &Line{Key: key, Label: label, AmountMinor: i64(cost), Coverage: Complete,
		Sources: []string{"inventory_movements (" + typ + ") × inventory_lots.unit_cost_minor"}, Notes: []string{fmt.Sprintf("%d movements", moves)}}
	if noCost > 0 {
		l.Coverage = Partial
		l.Notes = append(l.Notes, fmt.Sprintf("%d of %d have no lot cost in TTD.", noCost, moves))
	}
	return cost, l
}

// categoryCoverage sums approved expenses and estimates per category and
// decides coverage month by month.
func (s *Service) categoryCoverage(ctx context.Context, from, to time.Time) ([]CategoryCoverage, error) {
	cats, err := s.Categories(ctx)
	if err != nil {
		return nil, err
	}
	fromS, toS := from.Format("2006-01-02"), to.Format("2006-01-02")
	type monthState struct{ actual, confirmed, estimate bool }
	state := map[string]map[string]*monthState{}
	sums := map[string]*CategoryCoverage{}
	for _, c := range cats {
		state[c.Key] = map[string]*monthState{}
		for _, m := range months(from, to) {
			state[c.Key][m.Format("2006-01")] = &monthState{}
		}
		sums[c.Key] = &CategoryCoverage{Key: c.Key, Name: c.Name, CostClass: c.CostClass, MissingMonths: []string{}, EstimateMonths: []string{}}
	}
	rows, err := s.pool.Query(ctx, `SELECT category_key, kind, to_char(expense_date, 'YYYY-MM'), sum(amount_minor)
		FROM finance_expenses WHERE status = 'approved' AND currency = 'TTD' AND expense_date BETWEEN $1 AND $2 GROUP BY 1, 2, 3`, fromS, toS)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var cat, kind, month string
		var amount int64
		if err := rows.Scan(&cat, &kind, &month, &amount); err != nil {
			rows.Close()
			return nil, err
		}
		if kind == "actual" {
			sums[cat].ActualMinor += amount
			if st := state[cat][month]; st != nil {
				st.actual = true
			}
		} else {
			sums[cat].EstimatedMinor += amount
			if st := state[cat][month]; st != nil {
				st.estimate = true
			}
		}
	}
	rows.Close()
	conf, err := s.pool.Query(ctx, `SELECT category_key, to_char(month, 'YYYY-MM') FROM finance_coverage_confirmations
		WHERE month BETWEEN date_trunc('month', $1::date) AND $2::date`, fromS, toS)
	if err != nil {
		return nil, err
	}
	for conf.Next() {
		var cat, month string
		if err := conf.Scan(&cat, &month); err != nil {
			conf.Close()
			return nil, err
		}
		if st := state[cat][month]; st != nil {
			st.confirmed = true
		}
	}
	conf.Close()
	// Approved recurring schedules: occurrences not yet replaced by a recorded expense are estimates.
	rec, err := s.pool.Query(ctx, `SELECT r.id, r.category_key, r.amount_minor, r.cadence, r.starts_on, r.ends_on,
		coalesce(array_agg(to_char(e.expense_date, 'YYYY-MM-DD')) FILTER (WHERE e.id IS NOT NULL), '{}')
		FROM finance_recurring_expenses r
		LEFT JOIN finance_expenses e ON e.recurring_id = r.id AND e.status <> 'void'
		WHERE r.status IN ('approved', 'ended') AND r.approved_at IS NOT NULL AND r.currency = 'TTD'
		GROUP BY r.id`)
	if err != nil {
		return nil, err
	}
	for rec.Next() {
		var id, cat, cadence string
		var amount int64
		var start time.Time
		var end *time.Time
		var recorded []string
		if err := rec.Scan(&id, &cat, &amount, &cadence, &start, &end, &recorded); err != nil {
			rec.Close()
			return nil, err
		}
		done := map[string]bool{}
		for _, d := range recorded {
			done[d] = true
		}
		for _, d := range Occurrences(cadence, start, end, from, to) {
			if done[d.Format("2006-01-02")] {
				continue
			}
			sums[cat].EstimatedMinor += amount
			if st := state[cat][d.Format("2006-01")]; st != nil {
				st.estimate = true
			}
		}
	}
	rec.Close()
	out := make([]CategoryCoverage, 0, len(cats))
	for _, c := range cats {
		cc := sums[c.Key]
		keys := make([]string, 0, len(state[c.Key]))
		for m := range state[c.Key] {
			keys = append(keys, m)
		}
		sort.Strings(keys)
		for _, m := range keys {
			st := state[c.Key][m]
			switch {
			case st.actual || st.confirmed:
			case st.estimate:
				cc.EstimateMonths = append(cc.EstimateMonths, m)
			default:
				cc.MissingMonths = append(cc.MissingMonths, m)
			}
		}
		switch {
		case len(cc.MissingMonths) == len(keys):
			cc.Coverage = Missing
		case len(cc.MissingMonths) > 0:
			cc.Coverage = Partial
		case len(cc.EstimateMonths) > 0:
			cc.Coverage = Estimated
		default:
			cc.Coverage = Complete
		}
		out = append(out, *cc)
	}
	return out, nil
}

// cash builds the cash view. Collections are not bank cash: settlement is
// used only when every payment reports it; supplier payments are not
// recorded in the platform, so cash flow is never complete while goods were received.
func (s *Service) cash(ctx context.Context, fromS, toS string, net, realCount, settleCount, settleSum, refunds int64, toTS time.Time) (*Cash, error) {
	c := &Cash{Reasons: []string{}}
	if realCount == 0 || settleCount == realCount {
		c.InflowsMinor, c.InflowBasis = settleSum-refunds, "settlement"
		if realCount == 0 {
			c.InflowsMinor = 0
		}
	} else {
		c.InflowsMinor, c.InflowBasis = net, "collections"
		c.Reasons = append(c.Reasons, fmt.Sprintf("Settlement reported for %d of %d payments: inflows are collections, not confirmed bank receipts.", settleCount, realCount))
	}
	if err := s.pool.QueryRow(ctx, `SELECT coalesce(sum(amount_minor), 0) FROM finance_expenses WHERE status = 'approved' AND kind = 'actual'
		AND currency = 'TTD' AND payment_date BETWEEN $1 AND $2`, fromS, toS).Scan(&c.OutflowsMinor); err != nil {
		return nil, err
	}
	var receipts int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM goods_receipt_lines g JOIN goods_receipts r ON r.id = g.receipt_id
		WHERE (r.received_at AT TIME ZONE 'America/Port_of_Spain')::date BETWEEN $1 AND $2`, fromS, toS).Scan(&receipts); err != nil {
		return nil, err
	}
	if receipts > 0 {
		c.Reasons = append(c.Reasons, fmt.Sprintf("Supplier payments are not recorded in the platform (%d goods-receipt lines in the period).", receipts))
	}
	var bal *int64
	var asOf *string
	if err := s.pool.QueryRow(ctx, `SELECT sum(b.amount_minor), max(to_char(b.as_of, 'YYYY-MM-DD')), count(*) FROM (
		SELECT DISTINCT ON (account_label) account_label, amount_minor, as_of FROM finance_cash_balances
		WHERE currency = 'TTD' AND as_of <= $1 ORDER BY account_label, as_of DESC) b`, toS).Scan(&bal, &asOf, &c.BalanceAccounts); err != nil {
		return nil, err
	}
	c.BalanceMinor, c.BalanceAsOf = bal, asOf
	if bal == nil {
		c.Reasons = append(c.Reasons, "No approved cash balance is recorded.")
	}
	c.Complete = len(c.Reasons) == 0
	_ = toTS
	return c, nil
}
