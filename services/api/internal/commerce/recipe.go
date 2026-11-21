package commerce

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/menus"
	"mise.tt/api/internal/platform/httpx"
)

// Sprint 10: approved cooking instructions for the customer app.
//
// A customer sees the cooking steps of a meal only for a box they have
// bought: the order must be theirs, confirmed (paid) and not cancelled, and
// contain the meal. The content is the customer-facing part of the recipe
// version production uses for that delivery week — `customer_instructions`
// and `customer_cook_minutes` of the *published* version. Kitchen steps,
// costs, suppliers, packaging and drafts are never included.

var errRecipeUnavailable = httpx.NewError(http.StatusNotFound, "recipe_unavailable",
	"Cooking steps for this meal are not published yet.")

// Orders whose box has been paid for (and not cancelled).
var cookableOrder = map[string]bool{
	"confirmed": true, "awaiting_production": true, "in_production": true,
	"ready_for_fulfillment": true, "out_for_delivery": true, "delivered": true,
}

type CustomerRecipe struct {
	OrderID       string   `json:"order_id"`
	MealID        string   `json:"meal_id"`
	MealName      string   `json:"meal_name"`
	MealSlug      string   `json:"meal_slug"`
	Week          string   `json:"week"`
	Version       int      `json:"version"`
	Servings      int      `json:"servings"`
	ServingSize   string   `json:"serving_size"`
	CookMinutes   *int     `json:"cook_minutes"`
	Ingredients   []string `json:"ingredients"`
	Steps         []string `json:"steps"`
	Storage       string   `json:"storage"`
	Allergens     []string `json:"allergens"`
	MayContain    []string `json:"may_contain"`
	CrossContact  string   `json:"cross_contact_note"`
	PublishedAt   *string  `json:"published_at"`
	EffectiveFrom string   `json:"effective_from"`
}

var stepNumber = regexp.MustCompile(`^\s*(\d{1,2}[.)]|[-•*])\s+`)

// SplitSteps turns approved instructions into steps: one per paragraph, or
// one per line when the text is a list. Leading "1." or "-" markers are
// dropped; the words are never changed.
func SplitSteps(text string) []string {
	text = strings.ReplaceAll(strings.TrimSpace(text), "\r\n", "\n")
	if text == "" {
		return []string{}
	}
	parts := strings.Split(text, "\n\n")
	if len(parts) == 1 {
		parts = strings.Split(text, "\n")
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(stepNumber.ReplaceAllString(strings.TrimSpace(p), ""))
		p = strings.Join(strings.Fields(p), " ")
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (s *Service) CustomerRecipe(ctx context.Context, customerID, orderID, mealID string) (*CustomerRecipe, error) {
	o, err := s.Orders.ForCustomer(ctx, customerID, orderID)
	if err != nil {
		return nil, err // 404 for anything not theirs
	}
	if !cookableOrder[o.Status] {
		return nil, httpx.NewError(http.StatusForbidden, "recipe_not_available_yet",
			"Cooking steps open once your box is confirmed.")
	}
	var name, slug string
	found := false
	for _, it := range o.Items {
		if it.MealID == mealID {
			name, slug, found = it.Name, it.Slug, true
			break
		}
	}
	if !found {
		return nil, httpx.ErrNotFound
	}
	week, ok := menus.ParseWeek(o.Week)
	if !ok {
		return nil, errRecipeUnavailable
	}

	r := &CustomerRecipe{OrderID: o.ID, MealID: mealID, MealName: name, MealSlug: slug, Week: o.Week}
	var instructions string
	var cook *int16
	var published *time.Time
	var effective time.Time
	var servings int16
	err = s.pool.QueryRow(ctx, `
		SELECT v.number, v.yield_servings, v.serving_size, v.customer_cook_minutes, v.customer_instructions,
		       v.storage_instructions, v.cross_contact_note, v.published_at, v.effective_from
		FROM recipe_versions v JOIN recipes rc ON rc.id = v.recipe_id
		WHERE rc.meal_id = $1 AND v.status = 'published' AND v.effective_from <= $2
		ORDER BY v.effective_from DESC, v.number DESC LIMIT 1`, mealID, week).
		Scan(&r.Version, &servings, &r.ServingSize, &cook, &instructions, &r.Storage, &r.CrossContact, &published, &effective)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errRecipeUnavailable
	}
	if err != nil {
		return nil, err
	}
	r.Steps = SplitSteps(instructions)
	if len(r.Steps) == 0 {
		return nil, errRecipeUnavailable
	}
	r.Servings = int(servings)
	if cook != nil {
		c := int(*cook)
		r.CookMinutes = &c
	}
	if published != nil {
		p := published.UTC().Format(time.RFC3339)
		r.PublishedAt = &p
	}
	r.EffectiveFrom = effective.Format("2006-01-02")

	rows, err := s.pool.Query(ctx, `
		SELECT a.allergen, a.kind FROM recipe_allergens a
		JOIN recipe_versions v ON v.id = a.version_id JOIN recipes rc ON rc.id = v.recipe_id
		WHERE rc.meal_id = $1 AND v.number = $2 ORDER BY a.kind, a.allergen`, mealID, r.Version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	r.Allergens, r.MayContain = []string{}, []string{}
	for rows.Next() {
		var a, kind string
		if err := rows.Scan(&a, &kind); err != nil {
			return nil, err
		}
		if kind == "contains" {
			r.Allergens = append(r.Allergens, a)
		} else {
			r.MayContain = append(r.MayContain, a)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// What is in the box for this meal, as the customer sees it: names and
	// quantities, never costs or suppliers.
	ing, err := s.pool.Query(ctx, `
		SELECT i.name, l.quantity::text, l.unit, l.preparation
		FROM recipe_version_ingredients l
		JOIN ingredients i ON i.id = l.ingredient_id
		JOIN recipe_versions v ON v.id = l.version_id JOIN recipes rc ON rc.id = v.recipe_id
		WHERE rc.meal_id = $1 AND v.number = $2 ORDER BY l.position`, mealID, r.Version)
	if err != nil {
		return nil, err
	}
	defer ing.Close()
	r.Ingredients = []string{}
	for ing.Next() {
		var n, q, u, prep string
		if err := ing.Scan(&n, &q, &u, &prep); err != nil {
			return nil, err
		}
		line := strings.TrimRight(strings.TrimRight(q, "0"), ".") + " " + u + " " + n
		if prep != "" {
			line += ", " + prep
		}
		r.Ingredients = append(r.Ingredients, line)
	}
	return r, ing.Err()
}

func (h *Handler) recipe(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	mealID, ok := pathUUID(w, r, "mealId")
	if !ok {
		return
	}
	cid, ok := h.customer(w, r)
	if !ok {
		return
	}
	res, err := h.S.CustomerRecipe(r.Context(), cid, id, mealID)
	respond(w, r, http.StatusOK, res, err)
}
