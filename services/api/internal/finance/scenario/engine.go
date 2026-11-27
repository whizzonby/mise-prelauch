// Package scenario is the deterministic financial scenario engine.
//
// It projects a subscription business week by week from a set of inputs.
// Every input says where it came from: observed (computed from records, with
// the period and sources), preset (an engineering multiplier over an observed
// value) or assumption (entered by a person). Nothing is a forecast: results
// are what follows arithmetically from the inputs, and presets are
// illustrative ranges, not probabilities or calibrated prediction intervals.
//
// Arithmetic is exact (math/big rationals); each weekly money figure is
// rounded half away from zero to a minor unit, and totals are sums of the
// rounded weekly figures, so tables always add up. The same inputs always
// give the same output; InputHash identifies them.
package scenario

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
)

// EngineVersion changes whenever the arithmetic changes.
const EngineVersion = "scenario-v1"

// Units.
const (
	Count = "count"
	Minor = "money_minor"
	BPS   = "basis_points" // 1/100 of a percent
)

// Variable describes one input.
type Variable struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Unit        string `json:"unit"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
	Min, Max    int64  `json:"-"`
}

// Variables is every input the engine reads, in display order.
var Variables = []Variable{
	{"active_subscriptions", "Active subscriptions", Count, true, "Subscriptions billed weekly at the start.", 0, 10_000_000},
	{"new_subscriptions_per_week", "New subscriptions per week", Count, true, "Subscriptions added each week.", 0, 1_000_000},
	{"weekly_churn_bps", "Weekly churn", BPS, true, "Share of active subscriptions that end each week.", 0, 10_000},
	{"renewal_success_bps", "Renewal payment success", BPS, true, "Share of weekly renewals that are paid (the rest bring no sale that week).", 0, 10_000},
	{"weekly_value_minor", "Average weekly order value", Minor, true, "Per paid subscription-week, before tax.", 0, 100_000_000},
	{"price_change_bps", "Price change", BPS, true, "Applied to the order value (e.g. 500 = +5 %).", -9_000, 20_000},
	{"ingredient_cost_per_kit_minor", "Ingredient cost per kit", Minor, true, "Ingredients consumed per kit.", 0, 100_000_000},
	{"ingredient_cost_change_bps", "Ingredient cost change", BPS, true, "Applied to the ingredient cost per kit.", -9_000, 20_000},
	{"packaging_cost_per_kit_minor", "Packaging cost per kit", Minor, true, "", 0, 100_000_000},
	{"delivery_cost_per_kit_minor", "Delivery cost per kit", Minor, true, "", 0, 100_000_000},
	{"provider_fee_bps", "Payment provider fees", BPS, true, "Share of sales charged by payment providers.", 0, 2_000},
	{"fixed_costs_monthly_minor", "Fixed operating costs per month", Minor, true, "Rent, utilities, insurance, maintenance, professional services, other.", 0, 10_000_000_000},
	{"labour_monthly_minor", "Labour per month", Minor, true, "Salaries and wages.", 0, 10_000_000_000},
	{"marketing_monthly_minor", "Marketing per month", Minor, true, "", 0, 10_000_000_000},
	{"capacity_kits_per_week", "Production capacity (kits per week)", Count, false, "Optional: weeks above it are flagged.", 0, 10_000_000},
	{"opening_cash_minor", "Opening cash", Minor, false, "Optional: enables the cash view.", -10_000_000_000, 10_000_000_000},
}

// Find returns a variable's description.
func Find(key string) (Variable, bool) {
	for _, v := range Variables {
		if v.Key == key {
			return v, true
		}
	}
	return Variable{}, false
}

// Sources.
const (
	Observed   = "observed"
	Preset     = "preset"
	Assumption = "assumption"
	WhatIf     = "what_if"
)

// Value is one resolved input.
type Value struct {
	Value  *int64 `json:"value"`
	Source string `json:"source"`
	Basis  string `json:"basis"`
}

// Investment is a one-off cost in a given week (1-based).
type Investment struct {
	Week        int    `json:"week"`
	AmountMinor int64  `json:"amount_minor"`
	Label       string `json:"label"`
}

// Inputs are everything a projection depends on.
type Inputs struct {
	HorizonDays int              `json:"horizon_days"`
	Values      map[string]Value `json:"values"`
	Investments []Investment     `json:"investments"`
	Assumptions []string         `json:"assumptions"`
}

// Horizons allowed (days): 30, 60, 90 days, six and twelve months.
var Horizons = []int{30, 60, 90, 182, 365}

// Week is one projected week. Money in minor units (TTD).
type Week struct {
	Week             int    `json:"week"`
	ActiveStart      int64  `json:"active_start"`
	New              int64  `json:"new"`
	Churned          int64  `json:"churned"`
	PaidKits         int64  `json:"paid_kits"`
	CapacityBreach   bool   `json:"capacity_breach"`
	Sales            int64  `json:"sales_minor"`
	ProviderFees     int64  `json:"provider_fees_minor"`
	Ingredients      int64  `json:"ingredients_minor"`
	Packaging        int64  `json:"packaging_minor"`
	Delivery         int64  `json:"delivery_minor"`
	VariableCosts    int64  `json:"variable_costs_minor"`
	Contribution     int64  `json:"contribution_minor"`
	FixedCosts       int64  `json:"fixed_costs_minor"`
	Investments      int64  `json:"investments_minor"`
	OperatingResult  int64  `json:"operating_result_minor"`
	CumulativeResult int64  `json:"cumulative_result_minor"`
	Cash             *int64 `json:"cash_minor"`
}

// Totals are the sums over the horizon.
type Totals struct {
	Sales           int64 `json:"sales_minor"`
	VariableCosts   int64 `json:"variable_costs_minor"`
	Contribution    int64 `json:"contribution_minor"`
	FixedCosts      int64 `json:"fixed_costs_minor"`
	Investments     int64 `json:"investments_minor"`
	OperatingResult int64 `json:"operating_result_minor"`
	PaidKits        int64 `json:"paid_kits"`
	EndActive       int64 `json:"end_active_subscriptions"`
	CapacityWeeks   int   `json:"capacity_breach_weeks"`
}

// BreakEven is the number of active subscriptions at which a week's
// contribution covers a week's fixed costs (labour, marketing, fixed; not
// one-off investments). Nil when each subscription loses money.
type BreakEven struct {
	Subscriptions          *int64 `json:"subscriptions"`
	ContributionPerSubWeek string `json:"contribution_per_subscription_week_minor"` // exact, as a decimal
	FixedCostsPerWeek      string `json:"fixed_costs_per_week_minor"`
	Formula                string `json:"formula"`
	Note                   string `json:"note"`
}

// CashView is shown only when an opening cash balance is given.
type CashView struct {
	PeakNeed         int64  `json:"peak_need_minor"` // the deepest cumulative operating loss
	LowestCash       *int64 `json:"lowest_cash_minor"`
	FirstNegativeWk  *int   `json:"first_negative_week"`
	EndingCash       *int64 `json:"ending_cash_minor"`
	Note             string `json:"note"`
	OpeningAvailable bool   `json:"opening_available"`
}

// Output is a projection.
type Output struct {
	EngineVersion string     `json:"engine_version"`
	InputHash     string     `json:"input_hash"`
	Complete      bool       `json:"complete"`
	Missing       []string   `json:"missing"`
	Weeks         []Week     `json:"weeks"`
	Totals        *Totals    `json:"totals"`
	BreakEven     *BreakEven `json:"break_even"`
	Cash          CashView   `json:"cash"`
	Limitations   []string   `json:"limitations"`
	Statement     string     `json:"statement"`
}

// Hash identifies inputs (engine version included).
func Hash(in Inputs) string {
	keys := make([]string, 0, len(in.Values))
	for k := range in.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	type kv struct {
		K string `json:"k"`
		V *int64 `json:"v"`
	}
	vals := make([]kv, 0, len(keys))
	for _, k := range keys {
		vals = append(vals, kv{k, in.Values[k].Value})
	}
	inv := append([]Investment{}, in.Investments...)
	sort.SliceStable(inv, func(i, j int) bool { return inv[i].Week < inv[j].Week })
	b, _ := json.Marshal(struct {
		E string       `json:"e"`
		H int          `json:"h"`
		V []kv         `json:"v"`
		I []Investment `json:"i"`
	}{EngineVersion, in.HorizonDays, vals, inv})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Validate checks ranges and the horizon. Missing values are not errors:
// Run reports them.
func Validate(in Inputs) error {
	ok := false
	for _, h := range Horizons {
		if in.HorizonDays == h {
			ok = true
		}
	}
	if !ok {
		return fmt.Errorf("horizon_days: use one of %v", Horizons)
	}
	for k, v := range in.Values {
		def, found := Find(k)
		if !found {
			return fmt.Errorf("values.%s: unknown input", k)
		}
		if v.Value != nil && (*v.Value < def.Min || *v.Value > def.Max) {
			return fmt.Errorf("values.%s: between %d and %d", k, def.Min, def.Max)
		}
	}
	weeks := Weeks(in.HorizonDays)
	if len(in.Investments) > 20 {
		return fmt.Errorf("investments: at most 20")
	}
	for i, x := range in.Investments {
		if x.Week < 1 || x.Week > weeks || x.AmountMinor < 0 || x.AmountMinor > 10_000_000_000 || len(x.Label) > 100 {
			return fmt.Errorf("investments[%d]: week 1 to %d, amount 0 or more, label at most 100 characters", i, weeks)
		}
	}
	return nil
}

// Weeks in a horizon (rounded up).
func Weeks(days int) int { return (days + 6) / 7 }

func rat(v int64) *big.Rat { return new(big.Rat).SetInt64(v) }

func bps(v int64) *big.Rat { return big.NewRat(v, 10_000) }

func onePlus(b int64) *big.Rat { return new(big.Rat).Add(rat(1), bps(b)) }

func mul(xs ...*big.Rat) *big.Rat {
	out := rat(1)
	for _, x := range xs {
		out.Mul(out, x)
	}
	return out
}

// Round is half away from zero.
func Round(r *big.Rat) int64 {
	num, den := new(big.Int).Set(r.Num()), r.Denom()
	neg := num.Sign() < 0
	num.Abs(num)
	q, m := new(big.Int).QuoRem(num, den, new(big.Int))
	if new(big.Int).Mul(m, big.NewInt(2)).Cmp(den) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if neg {
		q.Neg(q)
	}
	return q.Int64()
}

func ceil(r *big.Rat) int64 {
	q, m := new(big.Int).QuoRem(r.Num(), r.Denom(), new(big.Int))
	if m.Sign() > 0 {
		q.Add(q, big.NewInt(1))
	}
	return q.Int64()
}

// Run projects the inputs. It never fills a missing required input.
func Run(in Inputs) *Output {
	out := &Output{EngineVersion: EngineVersion, InputHash: Hash(in), Missing: []string{}, Weeks: []Week{}, Limitations: []string{},
		Statement: "A scenario: what follows arithmetically from the inputs shown. Observed inputs come from records for the stated period; " +
			"presets and assumptions are not verified forecasts, and preset ranges are illustrative, not probabilities or prediction intervals."}
	val := func(k string) *int64 { return in.Values[k].Value }
	for _, v := range Variables {
		if v.Required && val(v.Key) == nil {
			out.Missing = append(out.Missing, v.Key)
		}
	}
	if len(out.Missing) > 0 {
		out.Limitations = append(out.Limitations, "Not calculated: some required inputs are missing. Enter them as assumptions or record the data they come from.")
		return out
	}
	out.Complete = true
	g := func(k string) int64 { return *val(k) }

	priceF := onePlus(g("price_change_bps"))
	value := mul(rat(g("weekly_value_minor")), priceF)
	feeRate := bps(g("provider_fee_bps"))
	ingKit := mul(rat(g("ingredient_cost_per_kit_minor")), onePlus(g("ingredient_cost_change_bps")))
	packKit, delKit := rat(g("packaging_cost_per_kit_minor")), rat(g("delivery_cost_per_kit_minor"))
	monthlyFixed := g("fixed_costs_monthly_minor") + g("labour_monthly_minor") + g("marketing_monthly_minor")
	// A month is 52/12 weeks: weekly fixed = monthly × 12 / 52.
	fixedWeek := big.NewRat(monthlyFixed*12, 52)
	renewal := bps(g("renewal_success_bps"))
	churn := bps(g("weekly_churn_bps"))
	newPerWeek := g("new_subscriptions_per_week")
	invByWeek := map[int]int64{}
	for _, x := range in.Investments {
		invByWeek[x.Week] += x.AmountMinor
	}
	var capacity *int64
	if c := val("capacity_kits_per_week"); c != nil {
		capacity = c
	} else {
		out.Limitations = append(out.Limitations, "No production capacity given: weeks are not checked against capacity.")
	}
	opening := val("opening_cash_minor")

	active := g("active_subscriptions")
	t := &Totals{}
	var cum int64
	var cash *int64
	if opening != nil {
		c := *opening
		cash = &c
	}
	cv := CashView{OpeningAvailable: opening != nil}
	minCum := int64(0)
	for w := 1; w <= Weeks(in.HorizonDays); w++ {
		wk := Week{Week: w, ActiveStart: active}
		wk.PaidKits = Round(mul(rat(active), renewal))
		kits := rat(wk.PaidKits)
		sales := mul(kits, value)
		wk.Sales = Round(sales)
		wk.ProviderFees = Round(mul(sales, feeRate))
		wk.Ingredients = Round(mul(kits, ingKit))
		wk.Packaging = Round(mul(kits, packKit))
		wk.Delivery = Round(mul(kits, delKit))
		wk.VariableCosts = wk.ProviderFees + wk.Ingredients + wk.Packaging + wk.Delivery
		wk.Contribution = wk.Sales - wk.VariableCosts
		// Weekly fixed costs, without drift: cumulative to this week minus cumulative to last.
		wk.FixedCosts = Round(mul(fixedWeek, rat(int64(w)))) - Round(mul(fixedWeek, rat(int64(w-1))))
		wk.Investments = invByWeek[w]
		wk.OperatingResult = wk.Contribution - wk.FixedCosts - wk.Investments
		cum += wk.OperatingResult
		wk.CumulativeResult = cum
		minCum = min(minCum, cum)
		if capacity != nil && wk.PaidKits > *capacity {
			wk.CapacityBreach = true
			t.CapacityWeeks++
		}
		if cash != nil {
			c := *opening + cum
			wk.Cash = &c
			if cv.LowestCash == nil || c < *cv.LowestCash {
				lc := c
				cv.LowestCash = &lc
			}
			if c < 0 && cv.FirstNegativeWk == nil {
				ww := w
				cv.FirstNegativeWk = &ww
			}
		}
		wk.Churned = Round(mul(rat(active), churn))
		wk.New = newPerWeek
		active = active - wk.Churned + wk.New

		t.Sales += wk.Sales
		t.VariableCosts += wk.VariableCosts
		t.Contribution += wk.Contribution
		t.FixedCosts += wk.FixedCosts
		t.Investments += wk.Investments
		t.OperatingResult += wk.OperatingResult
		t.PaidKits += wk.PaidKits
		out.Weeks = append(out.Weeks, wk)
	}
	t.EndActive = active
	out.Totals = t
	cv.PeakNeed = -minCum
	if opening != nil {
		e := *opening + cum
		cv.EndingCash = &e
		cv.Note = "Cash here is opening cash plus the cumulative operating result: it assumes sales are collected and costs paid in the week they occur (no payment terms, settlement delays, tax or financing)."
	} else {
		cv.Note = "No opening cash given: only the peak cumulative loss (the cash the plan would need) is shown."
		out.Limitations = append(out.Limitations, "No opening cash balance: the cash view is limited to the peak cumulative loss.")
	}
	out.Cash = cv
	if t.CapacityWeeks > 0 {
		out.Limitations = append(out.Limitations, fmt.Sprintf("%d weeks need more kits than the capacity given; sales assume that demand is met.", t.CapacityWeeks))
	}

	// Break-even: contribution per active subscription per week.
	perPaid := new(big.Rat).Sub(mul(value, new(big.Rat).Sub(rat(1), feeRate)), new(big.Rat).Add(ingKit, new(big.Rat).Add(packKit, delKit)))
	perActive := mul(perPaid, renewal)
	be := &BreakEven{ContributionPerSubWeek: perActive.FloatString(4), FixedCostsPerWeek: fixedWeek.FloatString(4),
		Formula: "ceil(weekly fixed costs ÷ contribution per active subscription per week); contribution = renewal success × (value × (1 − fee rate) − ingredients − packaging − delivery)"}
	if perActive.Sign() > 0 {
		n := ceil(new(big.Rat).Quo(fixedWeek, perActive))
		be.Subscriptions = &n
		be.Note = "One-off investments are not included."
	} else {
		be.Note = "No break-even: each subscription's variable costs are at least its sales."
	}
	out.BreakEven = be
	return out
}
