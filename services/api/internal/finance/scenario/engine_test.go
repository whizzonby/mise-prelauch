package scenario

import (
	"math/big"
	"strings"
	"testing"
)

func iv(v int64) *int64 { return &v }

func sample(horizon int) Inputs {
	vals := map[string]int64{
		"active_subscriptions": 100, "new_subscriptions_per_week": 10, "weekly_churn_bps": 500, "renewal_success_bps": 10_000,
		"weekly_value_minor": 20_000, "price_change_bps": 0, "ingredient_cost_per_kit_minor": 6_000, "ingredient_cost_change_bps": 0,
		"packaging_cost_per_kit_minor": 500, "delivery_cost_per_kit_minor": 1_500, "provider_fee_bps": 300,
		"fixed_costs_monthly_minor": 520_000, "labour_monthly_minor": 0, "marketing_monthly_minor": 0,
	}
	in := Inputs{HorizonDays: horizon, Values: map[string]Value{}}
	for k, v := range vals {
		in.Values[k] = Value{Value: iv(v), Source: Assumption}
	}
	return in
}

func TestRunMatchesHandArithmetic(t *testing.T) {
	out := Run(sample(30))
	if !out.Complete || len(out.Weeks) != 5 {
		t.Fatalf("complete=%v weeks=%d missing=%v", out.Complete, len(out.Weeks), out.Missing)
	}
	w1, w2 := out.Weeks[0], out.Weeks[1]
	// Week 1: 100 kits × 200.00 = 2,000,000; fees 3 % = 60,000; ingredients 600,000; packaging 50,000; delivery 150,000.
	if w1.Sales != 2_000_000 || w1.ProviderFees != 60_000 || w1.VariableCosts != 860_000 || w1.Contribution != 1_140_000 ||
		w1.FixedCosts != 120_000 || w1.OperatingResult != 1_020_000 || w1.Churned != 5 || w1.New != 10 {
		t.Fatalf("week 1 = %+v", w1)
	}
	// Week 2 starts with 100 − 5 + 10 = 105; churn 5.25 rounds to 5.
	if w2.ActiveStart != 105 || w2.Sales != 2_100_000 || w2.VariableCosts != 903_000 || w2.OperatingResult != 1_077_000 || w2.Churned != 5 {
		t.Fatalf("week 2 = %+v", w2)
	}
	var sum int64
	for _, w := range out.Weeks {
		sum += w.OperatingResult
	}
	if out.Totals.OperatingResult != sum || out.Weeks[4].CumulativeResult != sum {
		t.Fatalf("totals do not add up: %d vs %d", out.Totals.OperatingResult, sum)
	}
	// Break-even: 120,000 ÷ (200.00 × 0.97 − 80.00 = 11,400) = 10.53 → 11.
	if out.BreakEven.Subscriptions == nil || *out.BreakEven.Subscriptions != 11 {
		t.Fatalf("break-even = %+v", out.BreakEven)
	}
	// No capacity or opening cash: limitations say so, cash view limited.
	if lim := strings.Join(out.Limitations, " "); !strings.Contains(lim, "capacity") || !strings.Contains(lim, "opening cash") || out.Cash.EndingCash != nil {
		t.Fatalf("limitations = %s", lim)
	}
}

func TestRunIsReproducibleAndHashed(t *testing.T) {
	a, b := Run(sample(90)), Run(sample(90))
	if a.InputHash != b.InputHash || a.Totals.OperatingResult != b.Totals.OperatingResult {
		t.Fatal("same inputs gave different results")
	}
	c := sample(90)
	c.Values["weekly_churn_bps"] = Value{Value: iv(501), Source: Assumption}
	if Hash(c) == a.InputHash {
		t.Fatal("hash ignores a changed input")
	}
	// The source label does not change the numbers or the hash.
	d := sample(90)
	d.Values["weekly_churn_bps"] = Value{Value: iv(500), Source: Observed, Basis: "x"}
	if Hash(d) != a.InputHash {
		t.Fatal("hash depends on labels")
	}
}

func TestMissingInputsAreNeverFilled(t *testing.T) {
	in := sample(30)
	delete(in.Values, "weekly_value_minor")
	in.Values["provider_fee_bps"] = Value{}
	out := Run(in)
	if out.Complete || out.Totals != nil || len(out.Weeks) != 0 || strings.Join(out.Missing, ",") != "weekly_value_minor,provider_fee_bps" {
		t.Fatalf("missing inputs: complete=%v missing=%v", out.Complete, out.Missing)
	}
}

func TestFixedCostsDoNotDriftAndCashView(t *testing.T) {
	in := sample(365)
	in.Values["fixed_costs_monthly_minor"] = Value{Value: iv(100)}
	in.Values["opening_cash_minor"] = Value{Value: iv(-5_000_000)}
	in.Values["capacity_kits_per_week"] = Value{Value: iv(110)}
	in.Investments = []Investment{{Week: 2, AmountMinor: 3_000_000, Label: "oven"}}
	out := Run(in)
	if len(out.Weeks) != 53 || out.Totals.FixedCosts != 1223 { // 100 × 12 × 53 / 52 = 1223.08
		t.Fatalf("weeks %d fixed %d", len(out.Weeks), out.Totals.FixedCosts)
	}
	if out.Weeks[1].Investments != 3_000_000 || out.Totals.Investments != 3_000_000 {
		t.Fatal("investment not in week 2")
	}
	if out.Cash.FirstNegativeWk == nil || *out.Cash.FirstNegativeWk != 1 || *out.Cash.EndingCash != -5_000_000+out.Totals.OperatingResult {
		t.Fatalf("cash = %+v", out.Cash)
	}
	if out.Totals.CapacityWeeks == 0 || !out.Weeks[len(out.Weeks)-1].CapacityBreach || out.Weeks[0].CapacityBreach {
		t.Fatal("capacity breaches not flagged")
	}
}

func TestNoBreakEvenWhenEachSaleLosesMoney(t *testing.T) {
	in := sample(30)
	in.Values["ingredient_cost_per_kit_minor"] = Value{Value: iv(30_000)}
	if be := Run(in).BreakEven; be.Subscriptions != nil || !strings.Contains(be.Note, "No break-even") {
		t.Fatalf("break-even = %+v", be)
	}
}

func TestRoundHalfAwayFromZero(t *testing.T) {
	for _, c := range []struct {
		n, d, want int64
	}{{5, 2, 3}, {-5, 2, -3}, {7, 3, 2}, {-7, 3, -2}, {1, 2, 1}, {0, 1, 0}} {
		if got := Round(big.NewRat(c.n, c.d)); got != c.want {
			t.Errorf("Round(%d/%d) = %d, want %d", c.n, c.d, got, c.want)
		}
	}
}

func TestValidate(t *testing.T) {
	in := sample(45)
	if Validate(in) == nil {
		t.Fatal("45-day horizon accepted")
	}
	in = sample(30)
	in.Values["weekly_churn_bps"] = Value{Value: iv(20_000)}
	if Validate(in) == nil {
		t.Fatal("churn over 100 % accepted")
	}
	in = sample(30)
	in.Values["sql"] = Value{Value: iv(1)}
	if Validate(in) == nil {
		t.Fatal("unknown input accepted")
	}
	in = sample(30)
	in.Investments = []Investment{{Week: 9, AmountMinor: 1}}
	if Validate(in) == nil {
		t.Fatal("investment past the horizon accepted")
	}
}

func TestPresetsAreLabelledMultipliers(t *testing.T) {
	in := sample(30)
	delete(in.Values, "price_change_bps")
	if err := ApplyPreset(&in, Conservative); err != nil {
		t.Fatal(err)
	}
	g := func(k string) int64 { return *in.Values[k].Value }
	// 10 × 3/4 = 7.5 → 8; 500 × 5/4 = 625; 10,000 − 200; +500 ingredient; price defaults to no change.
	if g("new_subscriptions_per_week") != 8 || g("weekly_churn_bps") != 625 || g("renewal_success_bps") != 9_800 ||
		g("ingredient_cost_change_bps") != 500 || g("price_change_bps") != 0 || in.Values["weekly_churn_bps"].Source != Preset || len(in.Assumptions) != 4 {
		t.Fatalf("conservative = %+v", in.Values)
	}
	opt := sample(30)
	_ = ApplyPreset(&opt, Optimistic)
	if *opt.Values["renewal_success_bps"].Value != 10_000 { // capped at 100 %
		t.Fatal("renewal success above 100 %")
	}
	if ApplyPreset(&opt, "likely") == nil {
		t.Fatal("unknown preset accepted")
	}
}

func TestWhatIfChanges(t *testing.T) {
	in := sample(90)
	err := ApplyChanges(&in, []Change{
		{Kind: "price_increase", Params: map[string]int64{"bps": 500}},
		{Kind: "hire", Params: map[string]int64{"count": 2, "monthly_cost_minor": 500_000}},
		{Kind: "kitchen_expansion", Params: map[string]int64{"capacity_kits": 200, "investment_minor": 9_000_000, "monthly_cost_minor": 100_000, "week": 3}, Label: "Second kitchen"},
		{Kind: "supplier_change", Params: map[string]int64{"ingredient_bps": -300}},
	})
	if err != nil {
		t.Fatal(err)
	}
	g := func(k string) int64 { return *in.Values[k].Value }
	if g("price_change_bps") != 500 || g("labour_monthly_minor") != 1_000_000 || g("capacity_kits_per_week") != 200 ||
		g("fixed_costs_monthly_minor") != 620_000 || g("ingredient_cost_change_bps") != -300 || len(in.Investments) != 1 ||
		in.Investments[0].Week != 3 || in.Investments[0].Label != "Second kitchen" || in.Values["price_change_bps"].Source != WhatIf || len(in.Assumptions) != 6 {
		t.Fatalf("what-if = %+v %+v", in.Values, in.Investments)
	}
	base, changed := Run(sample(90)), Run(in)
	if changed.Totals.Sales <= base.Totals.Sales || changed.Totals.Investments != 9_000_000 {
		t.Fatal("what-if did not change the projection")
	}

	bad := sample(30)
	for _, c := range []Change{
		{Kind: "fire_everyone"},
		{Kind: "hire", Params: map[string]int64{"salary": 1}},
		{Kind: "churn_change", Params: map[string]int64{"bps": 20_000}},
		{Kind: "new_meal_kit", Params: map[string]int64{"new_per_week": 1, "investment_minor": 5, "week": 99}},
	} {
		x := sample(30)
		if ApplyChanges(&x, []Change{c}) == nil {
			t.Errorf("%+v accepted", c)
		}
	}
	delete(bad.Values, "marketing_monthly_minor")
	if ApplyChanges(&bad, []Change{{Kind: "marketing", Params: map[string]int64{"monthly_minor": 1000}}}) == nil {
		t.Fatal("a change on a missing input was accepted")
	}
}
