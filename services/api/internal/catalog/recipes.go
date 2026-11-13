package catalog

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/culinary"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
)

// A meal whose recipe has a published version takes its allergen
// declaration and verified nutrition from that approved version (Sprint 5).
// The catalogue editor can no longer set them by hand: the customer-facing
// claims must match the approved recipe.

var errRecipeControlsClaims = httpx.NewError(http.StatusConflict, "recipe_controls_claims",
	"This meal's allergens and nutrition come from its approved recipe. Change the recipe (a new revision) instead.")

// recipeLocked reports whether the meal has a published recipe version.
func recipeLocked(ctx context.Context, q db.Querier, mealID string) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM recipes r JOIN recipe_versions v ON v.recipe_id = r.id
		WHERE r.meal_id = $1 AND v.status IN ('published', 'archived'))`, mealID).Scan(&ok)
	return ok, err
}

// ApplyRecipeClaims sets a meal's declared allergens and verified nutrition
// from a published recipe version (culinary calls it in its transaction).
// The meal's version is bumped, so later orders record the change.
func (s *Service) ApplyRecipeClaims(ctx context.Context, q db.Querier, actor string, c culinary.MealClaims) error {
	allergens := slices.Clone(c.Allergens)
	if allergens == nil {
		allergens = []string{}
	}
	slices.Sort(allergens)
	tag, err := q.Exec(ctx, `UPDATE meals SET allergens = $2, allergen_status = 'declared', version = version + 1, updated_at = now(),
		updated_by = $3 WHERE id = $1`, c.MealID, allergens, nilIfEmpty(actor))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.ErrNotFound
	}
	now := s.now().UTC()
	if _, err := q.Exec(ctx, `
		INSERT INTO meal_nutrition (meal_id, calories, protein_g, carbs_g, fat_g, fibre_g, sodium_mg, status, verified_by, verified_at, updated_at)
		VALUES ($1, $2, $3::numeric, $4::numeric, $5::numeric, $6::numeric, $7, 'verified', $8, $9, now())
		ON CONFLICT (meal_id) DO UPDATE SET calories = EXCLUDED.calories, protein_g = EXCLUDED.protein_g, carbs_g = EXCLUDED.carbs_g,
		  fat_g = EXCLUDED.fat_g, fibre_g = EXCLUDED.fibre_g, sodium_mg = EXCLUDED.sodium_mg, status = 'verified',
		  verified_by = EXCLUDED.verified_by, verified_at = EXCLUDED.verified_at, updated_at = now()`,
		c.MealID, c.EnergyKcal, c.ProteinG, c.CarbsG, c.FatG, c.FibreG, c.SodiumMg, nilIfEmpty(c.NutritionByUser), now); err != nil {
		return err
	}
	return audit.Record(ctx, q, audit.Entry{ActorUserID: actor, Action: "catalog.claims_from_recipe", TargetType: "meal", TargetID: c.MealID,
		Metadata: map[string]any{"allergens": allergens}})
}

// MealName returns a meal's name, and whether it exists.
func (s *Service) MealName(ctx context.Context, q db.Querier, id string) (string, bool, error) {
	var name string
	err := q.QueryRow(ctx, `SELECT name FROM meals WHERE id = $1`, id).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return name, err == nil, err
}

func sameSet(a, b []string) bool {
	x, y := uniq(a), uniq(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}
