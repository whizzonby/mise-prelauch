package scenario

import (
	"fmt"
	"math/big"
)

// Presets.
const (
	Base         = "base"
	Conservative = "conservative"
	Optimistic   = "optimistic"
	Custom       = "custom"
)

// presetRules are the multipliers each preset applies to observed values.
// They are engineering defaults chosen to show a range, not estimates of
// likelihood; the output lists each one as an assumption.
var presetRules = map[string][]struct {
	key  string
	num  int64 // multiplier num/den, or an added amount when den == 0
	den  int64
	text string
}{
	Conservative: {
		{"new_subscriptions_per_week", 3, 4, "New subscriptions 25 % below observed"},
		{"weekly_churn_bps", 5, 4, "Churn 25 % above observed"},
		{"renewal_success_bps", -200, 0, "Renewal success 2 percentage points below observed"},
		{"ingredient_cost_change_bps", 500, 0, "Ingredient costs 5 % higher"},
	},
	Optimistic: {
		{"new_subscriptions_per_week", 5, 4, "New subscriptions 25 % above observed"},
		{"weekly_churn_bps", 3, 4, "Churn 25 % below observed"},
		{"renewal_success_bps", 100, 0, "Renewal success 1 percentage point above observed"},
	},
}

// PresetNote is shown with every preset.
const PresetNote = "Presets are illustrative ranges set by engineering (multipliers shown as assumptions). They are not probabilities, " +
	"not statistically calibrated prediction intervals, and none is labelled likely."

// ApplyPreset adjusts values for a preset. Base changes nothing; price and
// ingredient-cost changes default to zero ("no change", an assumption).
func ApplyPreset(in *Inputs, preset string) error {
	switch preset {
	case Base, Custom, "":
	case Conservative, Optimistic:
	default:
		return fmt.Errorf("preset: use base, conservative, optimistic or custom")
	}
	for _, k := range []string{"price_change_bps", "ingredient_cost_change_bps"} {
		if v, ok := in.Values[k]; !ok || v.Value == nil {
			zero := int64(0)
			in.Values[k] = Value{Value: &zero, Source: Assumption, Basis: "No change assumed."}
		}
	}
	for _, r := range presetRules[preset] {
		v := in.Values[r.key]
		if v.Value == nil {
			continue
		}
		var n int64
		if r.den == 0 {
			n = *v.Value + r.num
		} else {
			n = Round(new(big.Rat).Mul(big.NewRat(*v.Value, 1), big.NewRat(r.num, r.den)))
		}
		def, _ := Find(r.key)
		n = max(def.Min, min(def.Max, n))
		in.Values[r.key] = Value{Value: &n, Source: Preset, Basis: r.text + " (" + preset + " preset)."}
		in.Assumptions = append(in.Assumptions, r.text+" ("+preset+" preset).")
	}
	return nil
}

// Change is one what-if: a named change to the inputs.
type Change struct {
	Kind   string           `json:"kind"`
	Params map[string]int64 `json:"params"`
	Label  string           `json:"label"`
}

// ChangeKinds and the parameters each takes.
var ChangeKinds = map[string][]string{
	"price_increase":     {"bps"},
	"customer_growth":    {"new_per_week"},
	"churn_change":       {"bps"},
	"ingredient_cost":    {"bps"},
	"hire":               {"count", "monthly_cost_minor"},
	"kitchen_expansion":  {"capacity_kits", "investment_minor", "monthly_cost_minor", "week"},
	"delivery_expansion": {"new_per_week", "delivery_cost_per_kit_minor"},
	"new_meal_kit":       {"new_per_week", "investment_minor", "week"},
	"marketing":          {"monthly_minor", "new_per_week"},
	"supplier_change":    {"ingredient_bps"},
}

// ApplyChanges applies what-if changes as assumptions over the inputs.
// Changes to an input that is missing are refused (nothing is assumed).
func ApplyChanges(in *Inputs, changes []Change) error {
	if len(changes) > 20 {
		return fmt.Errorf("changes: at most 20")
	}
	weeks := Weeks(in.HorizonDays)
	for i, c := range changes {
		allowed, ok := ChangeKinds[c.Kind]
		if !ok {
			return fmt.Errorf("changes[%d].kind: unknown change %q", i, c.Kind)
		}
		for p := range c.Params {
			found := false
			for _, a := range allowed {
				found = found || a == p
			}
			if !found {
				return fmt.Errorf("changes[%d].params.%s: %s takes %v", i, p, c.Kind, allowed)
			}
		}
		p := func(k string) int64 { return c.Params[k] }
		label := c.Label
		if len(label) > 100 {
			return fmt.Errorf("changes[%d].label: at most 100 characters", i)
		}
		add := func(key string, delta int64, text string) error {
			if delta == 0 {
				return nil
			}
			v := in.Values[key]
			if v.Value == nil {
				return fmt.Errorf("changes[%d]: %s needs %s, which is missing", i, c.Kind, key)
			}
			def, _ := Find(key)
			n := *v.Value + delta
			if n < def.Min || n > def.Max {
				return fmt.Errorf("changes[%d]: %s would be out of range", i, key)
			}
			in.Values[key] = Value{Value: &n, Source: WhatIf, Basis: v.Basis + " " + text}
			in.Assumptions = append(in.Assumptions, text)
			return nil
		}
		setOrAdd := func(key string, delta int64, text string) error {
			if v := in.Values[key]; v.Value == nil && delta != 0 {
				in.Values[key] = Value{Value: &delta, Source: WhatIf, Basis: text}
				in.Assumptions = append(in.Assumptions, text)
				return nil
			}
			return add(key, delta, text)
		}
		invest := func(amount, week int64, text string) error {
			if amount == 0 {
				return nil
			}
			if week == 0 {
				week = 1
			}
			if week < 1 || week > int64(weeks) || amount < 0 {
				return fmt.Errorf("changes[%d]: week 1 to %d, amount 0 or more", i, weeks)
			}
			if label == "" {
				label = c.Kind
			}
			in.Investments = append(in.Investments, Investment{Week: int(week), AmountMinor: amount, Label: label})
			in.Assumptions = append(in.Assumptions, fmt.Sprintf("%s: one-off %s in week %d.", text, money(amount), week))
			return nil
		}
		var err error
		switch c.Kind {
		case "price_increase":
			err = add("price_change_bps", p("bps"), fmt.Sprintf("What-if: price change of %s.", pct(p("bps"))))
		case "customer_growth":
			err = add("new_subscriptions_per_week", p("new_per_week"), fmt.Sprintf("What-if: %+d new subscriptions per week.", p("new_per_week")))
		case "churn_change":
			err = add("weekly_churn_bps", p("bps"), fmt.Sprintf("What-if: weekly churn changed by %s.", pct(p("bps"))))
		case "ingredient_cost", "supplier_change":
			b := p("bps") + p("ingredient_bps")
			err = add("ingredient_cost_change_bps", b, fmt.Sprintf("What-if (%s): ingredient cost change of %s.", c.Kind, pct(b)))
		case "hire":
			if p("count") < 0 || p("monthly_cost_minor") < 0 {
				return fmt.Errorf("changes[%d]: count and cost cannot be negative", i)
			}
			err = add("labour_monthly_minor", p("count")*p("monthly_cost_minor"),
				fmt.Sprintf("What-if: hire %d at %s per month each.", p("count"), money(p("monthly_cost_minor"))))
		case "kitchen_expansion":
			if err = setOrAdd("capacity_kits_per_week", p("capacity_kits"), fmt.Sprintf("What-if: kitchen capacity +%d kits per week.", p("capacity_kits"))); err == nil {
				if err = add("fixed_costs_monthly_minor", p("monthly_cost_minor"), fmt.Sprintf("What-if: kitchen running costs +%s per month.", money(p("monthly_cost_minor")))); err == nil {
					err = invest(p("investment_minor"), p("week"), "What-if: kitchen expansion")
				}
			}
		case "delivery_expansion":
			if err = add("new_subscriptions_per_week", p("new_per_week"), fmt.Sprintf("What-if: delivery expansion adds %d new subscriptions per week.", p("new_per_week"))); err == nil {
				err = add("delivery_cost_per_kit_minor", p("delivery_cost_per_kit_minor"), fmt.Sprintf("What-if: delivery cost per kit %+d minor units.", p("delivery_cost_per_kit_minor")))
			}
		case "new_meal_kit":
			if err = add("new_subscriptions_per_week", p("new_per_week"), fmt.Sprintf("What-if: a new meal kit adds %d new subscriptions per week.", p("new_per_week"))); err == nil {
				err = invest(p("investment_minor"), p("week"), "What-if: new meal kit launch")
			}
		case "marketing":
			if err = add("marketing_monthly_minor", p("monthly_minor"), fmt.Sprintf("What-if: marketing %+d minor units per month.", p("monthly_minor"))); err == nil {
				err = add("new_subscriptions_per_week", p("new_per_week"), fmt.Sprintf("What-if: marketing adds %d new subscriptions per week (assumed, not measured).", p("new_per_week")))
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func pct(bps int64) string {
	return new(big.Rat).SetFrac64(bps, 100).FloatString(2) + " %"
}

func money(minor int64) string {
	return "TTD " + new(big.Rat).SetFrac64(minor, 100).FloatString(2)
}
