package culinary

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/qty"
)

// Costing is an estimate from reference costs. It is never a procurement
// cost and is marked incomplete whenever any line has no usable reference.
type Costing struct {
	RecipeID      string        `json:"recipe_id"`
	Version       int           `json:"version"`
	YieldServings int           `json:"yield_servings"`
	Currency      string        `json:"currency"`
	Lines         []CostingLine `json:"lines"`
	// Sum of the lines that have a cost, in cents.
	IngredientsMinor int64 `json:"ingredients_minor"`
	// Latest packaging reference per kit, if any.
	PackagingMinor  *int64 `json:"packaging_minor"`
	PerServingMinor int64  `json:"per_serving_minor"`
	// Complete: every line and the packaging have a reference cost.
	Complete bool   `json:"complete"`
	Note     string `json:"note"`
}

type CostingLine struct {
	IngredientName string `json:"ingredient_name"`
	Quantity       qty.Q  `json:"quantity"`
	Unit           string `json:"unit"`
	CostMinor      *int64 `json:"cost_minor"`
	// ok, no_reference, unconvertible
	Status string  `json:"status"`
	Source *string `json:"source"`
	AsOf   *string `json:"as_of"`
}

// RecipeCosting estimates version n (0: latest) as of today.
func (s *Service) RecipeCosting(ctx context.Context, recipeID string, n int) (*Costing, error) {
	v, err := s.Version(ctx, recipeID, n)
	if err != nil {
		return nil, err
	}
	conv, err := NewConverter(ctx, s.pool)
	if err != nil {
		return nil, err
	}
	today := s.Now()
	out := &Costing{RecipeID: recipeID, Version: v.Number, YieldServings: v.YieldServings, Currency: "TTD", Lines: []CostingLine{}, Complete: true,
		Note: "An estimate from reference costs, not a purchase cost. Sprint 6 procurement will record actual prices."}
	total := qty.Int(0)
	for _, l := range v.Lines {
		cl := CostingLine{IngredientName: l.IngredientName, Quantity: l.Quantity, Unit: l.Unit, Status: "no_reference"}
		ref, err := latestCost(ctx, s.pool, l.IngredientID, today)
		if err != nil {
			return nil, err
		}
		if ref != nil {
			amount, _, err := conv.Convert(ctx, l.IngredientID, l.Quantity, l.Unit, ref.Unit)
			if errors.Is(err, ErrIncompatible) {
				cl.Status = "unconvertible"
			} else if err != nil {
				return nil, err
			} else {
				cost := amount.Mul(qty.Int(ref.AmountMinor)).Div(qty.MustParse(ref.PerQuantity))
				total = total.Add(cost)
				c := toMinor(cost)
				cl.CostMinor, cl.Status, cl.Source, cl.AsOf = &c, "ok", &ref.Source, &ref.EffectiveFrom
			}
		}
		if cl.Status != "ok" {
			out.Complete = false
		}
		out.Lines = append(out.Lines, cl)
	}
	out.IngredientsMinor = toMinor(total)
	if v.YieldServings > 0 {
		out.PerServingMinor = toMinor(total.Div(qty.Int(int64(v.YieldServings))))
	}
	pack, err := latestCost(ctx, s.pool, "", today)
	if err != nil {
		return nil, err
	}
	if pack != nil {
		m := pack.AmountMinor
		out.PackagingMinor = &m
	} else {
		out.Complete = false
	}
	if len(v.Lines) == 0 {
		out.Complete = false
	}
	return out, nil
}

func toMinor(q qty.Q) int64 {
	f, _ := q.Round(0).Rat().Float64()
	return int64(f)
}

// latestCost is the newest reference in force on day for an ingredient
// ("" for packaging).
func latestCost(ctx context.Context, q db.Querier, ingredientID string, day time.Time) (*CostRef, error) {
	var c CostRef
	err := q.QueryRow(ctx, `
		SELECT id, kind, ingredient_id, label, amount_minor, currency, trim_scale(per_quantity)::text, unit, source, effective_from::text, created_at
		FROM ingredient_cost_references
		WHERE (($1::uuid IS NULL AND kind = 'packaging') OR ingredient_id = $1::uuid) AND effective_from <= $2::date
		ORDER BY effective_from DESC, created_at DESC LIMIT 1`, nilIfEmpty(ingredientID), day.Format("2006-01-02")).Scan(
		&c.ID, &c.Kind, &c.IngredientID, &c.Label, &c.AmountMinor, &c.Currency, &c.PerQuantity, &c.Unit, &c.Source, &c.EffectiveFrom, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &c, err
}
