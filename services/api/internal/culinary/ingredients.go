package culinary

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/qty"
	"mise.tt/api/internal/vocab"
)

var (
	IngredientCategories = []string{"produce", "meat", "poultry", "seafood", "dairy", "eggs", "grains", "legumes", "herbs_spices",
		"condiments", "oils_fats", "baking", "beverages", "packaged", "other"}
	PreparationStates = []string{"raw", "prepared", "cooked", "processed", "dried", "frozen", "canned"}
	StorageKinds      = []string{"unspecified", "ambient", "chilled", "frozen"}
)

// Ingredient is one entry of the shared ingredient catalogue.
type Ingredient struct {
	ID               string       `json:"id"`
	Name             string       `json:"name"`
	Description      string       `json:"description"`
	Category         string       `json:"category"`
	BaseUnit         string       `json:"base_unit"`
	PurchasingUnit   *string      `json:"purchasing_unit"`
	PreparationState string       `json:"preparation_state"`
	Allergens        []string     `json:"allergens"`
	Storage          string       `json:"storage"`
	StorageNotes     string       `json:"storage_notes"`
	ShelfLifeNote    *string      `json:"shelf_life_note"`
	Active           bool         `json:"active"`
	Aliases          []string     `json:"aliases"`
	CreatedAt        time.Time    `json:"created_at"`
	UpdatedAt        time.Time    `json:"updated_at"`
	Conversions      []Conversion `json:"conversions,omitempty"`
	Costs            []CostRef    `json:"costs,omitempty"`
	// Recipes using it in any version.
	UsedIn int `json:"used_in"`
}

const ingredientCols = `i.id, i.name, i.description, i.category, i.base_unit, i.purchasing_unit, i.preparation_state, i.allergens,
	i.storage, i.storage_notes, i.shelf_life_note, i.active, i.created_at, i.updated_at,
	COALESCE((SELECT array_agg(a.alias ORDER BY a.alias) FROM ingredient_aliases a WHERE a.ingredient_id = i.id), '{}'),
	(SELECT count(DISTINCT v.recipe_id) FROM recipe_version_ingredients l JOIN recipe_versions v ON v.id = l.version_id WHERE l.ingredient_id = i.id)::int`

func scanIngredient(r pgx.Row) (Ingredient, error) {
	var i Ingredient
	err := r.Scan(&i.ID, &i.Name, &i.Description, &i.Category, &i.BaseUnit, &i.PurchasingUnit, &i.PreparationState, &i.Allergens,
		&i.Storage, &i.StorageNotes, &i.ShelfLifeNote, &i.Active, &i.CreatedAt, &i.UpdatedAt, &i.Aliases, &i.UsedIn)
	return i, err
}

// IngredientFilter narrows the ingredient list.
type IngredientFilter struct {
	Query, Category string
	// Active: "", "true" or "false".
	Active     string
	Page, Size int
}

type IngredientPage struct {
	Items []Ingredient `json:"items"`
	Total int          `json:"total"`
	Page  int          `json:"page"`
}

func (s *Service) Ingredients(ctx context.Context, f IngredientFilter) (*IngredientPage, error) {
	if f.Size < 1 || f.Size > 200 {
		f.Size = 50
	}
	if f.Page < 1 {
		f.Page = 1
	}
	var cat, active *string
	if slices.Contains(IngredientCategories, f.Category) {
		cat = &f.Category
	}
	if f.Active == "true" || f.Active == "false" {
		active = &f.Active
	}
	q := strings.TrimSpace(f.Query)
	rows, err := s.pool.Query(ctx, `
		SELECT `+ingredientCols+`, count(*) OVER ()::int
		FROM ingredients i
		WHERE ($1::text IS NULL OR i.category = $1)
		  AND ($2::text IS NULL OR i.active = $2::boolean)
		  AND ($3 = '' OR i.name ILIKE '%' || $3 || '%'
		       OR EXISTS (SELECT 1 FROM ingredient_aliases a WHERE a.ingredient_id = i.id AND a.alias ILIKE '%' || $3 || '%'))
		ORDER BY lower(i.name)
		LIMIT $4 OFFSET $5`, cat, active, escapeLike(q), f.Size, (f.Page-1)*f.Size)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := &IngredientPage{Items: []Ingredient{}, Page: f.Page}
	for rows.Next() {
		var i Ingredient
		if err := rows.Scan(&i.ID, &i.Name, &i.Description, &i.Category, &i.BaseUnit, &i.PurchasingUnit, &i.PreparationState, &i.Allergens,
			&i.Storage, &i.StorageNotes, &i.ShelfLifeNote, &i.Active, &i.CreatedAt, &i.UpdatedAt, &i.Aliases, &i.UsedIn, &out.Total); err != nil {
			return nil, err
		}
		out.Items = append(out.Items, i)
	}
	return out, rows.Err()
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func (s *Service) Ingredient(ctx context.Context, id string) (*Ingredient, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	i, err := scanIngredient(s.pool.QueryRow(ctx, `SELECT `+ingredientCols+` FROM ingredients i WHERE i.id = $1`, id))
	if err != nil {
		return nil, notFoundIf(err)
	}
	conv, err := (&Converter{conversions: map[string][]Conversion{}, q: s.pool}).load(ctx, id)
	if err != nil {
		return nil, err
	}
	i.Conversions = conv
	costs, err := s.costRefs(ctx, s.pool, "ingredient", id)
	if err != nil {
		return nil, err
	}
	i.Costs = costs
	return &i, nil
}

// IngredientInput creates or (with pointers left nil) partly updates an ingredient.
type IngredientInput struct {
	Name             *string   `json:"name"`
	Description      *string   `json:"description"`
	Category         *string   `json:"category"`
	BaseUnit         *string   `json:"base_unit"`
	PurchasingUnit   *string   `json:"purchasing_unit"`
	PreparationState *string   `json:"preparation_state"`
	Allergens        *[]string `json:"allergens"`
	Storage          *string   `json:"storage"`
	StorageNotes     *string   `json:"storage_notes"`
	ShelfLifeNote    *string   `json:"shelf_life_note"`
	Active           *bool     `json:"active"`
	// Replaces the alias list when present.
	Aliases *[]string `json:"aliases"`
}

func (s *Service) validateIngredient(ctx context.Context, q db.Querier, cur *Ingredient, in IngredientInput) (Ingredient, []string, error) {
	v := Ingredient{Category: "other", PreparationState: "raw", Storage: "unspecified", Allergens: []string{}, Active: true}
	if cur != nil {
		v = *cur
	}
	var f []httpx.FieldError
	bad := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	if in.Name != nil {
		v.Name = clean(*in.Name)
	}
	if n := runes(v.Name); n < 1 || n > 100 {
		bad("name", "Give the ingredient a name (up to 100 characters).")
	}
	if in.Description != nil {
		v.Description = strings.TrimSpace(*in.Description)
	}
	if runes(v.Description) > 1000 {
		bad("description", "Keep the description under 1,000 characters.")
	}
	if in.Category != nil {
		v.Category = *in.Category
	}
	if !slices.Contains(IngredientCategories, v.Category) {
		bad("category", "Choose a category.")
	}
	if in.BaseUnit != nil {
		v.BaseUnit = *in.BaseUnit
	}
	var known bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM units_of_measure WHERE code = $1)`, v.BaseUnit).Scan(&known); err != nil {
		return v, nil, err
	}
	if !known {
		bad("base_unit", "Choose the unit requirements are counted in.")
	}
	if in.PurchasingUnit != nil {
		v.PurchasingUnit = nilIfEmpty(clean(*in.PurchasingUnit))
	}
	if v.PurchasingUnit != nil && runes(*v.PurchasingUnit) > 60 {
		bad("purchasing_unit", "Keep the purchasing unit under 60 characters.")
	}
	if in.PreparationState != nil {
		v.PreparationState = *in.PreparationState
	}
	if !slices.Contains(PreparationStates, v.PreparationState) {
		bad("preparation_state", "Choose a preparation state.")
	}
	if in.Allergens != nil {
		v.Allergens = []string{}
		for _, a := range *in.Allergens {
			if !vocab.MealAllergens.Has(a) {
				bad("allergens", "Unknown allergen: "+a+".")
				continue
			}
			if !slices.Contains(v.Allergens, a) {
				v.Allergens = append(v.Allergens, a)
			}
		}
		slices.Sort(v.Allergens)
	}
	if in.Storage != nil {
		v.Storage = *in.Storage
	}
	if !slices.Contains(StorageKinds, v.Storage) {
		bad("storage", "Choose how it is stored.")
	}
	if in.StorageNotes != nil {
		v.StorageNotes = strings.TrimSpace(*in.StorageNotes)
	}
	if runes(v.StorageNotes) > 500 {
		bad("storage_notes", "Keep storage notes under 500 characters.")
	}
	if in.ShelfLifeNote != nil {
		v.ShelfLifeNote = nilIfEmpty(strings.TrimSpace(*in.ShelfLifeNote))
	}
	if v.ShelfLifeNote != nil && runes(*v.ShelfLifeNote) > 300 {
		bad("shelf_life_note", "Keep the shelf-life note under 300 characters.")
	}
	if in.Active != nil {
		v.Active = *in.Active
	}
	var aliases []string
	if in.Aliases != nil {
		for _, a := range *in.Aliases {
			a = clean(a)
			if a == "" {
				continue
			}
			if runes(a) > 100 {
				bad("aliases", "Keep each alias under 100 characters.")
			}
			if strings.EqualFold(a, v.Name) {
				continue
			}
			if !slices.ContainsFunc(aliases, func(x string) bool { return strings.EqualFold(x, a) }) {
				aliases = append(aliases, a)
			}
		}
		if len(aliases) > 20 {
			bad("aliases", "Up to 20 aliases.")
		}
	}
	if len(f) > 0 {
		return v, nil, httpx.ValidationError(f)
	}
	return v, aliases, nil
}

// duplicate finds another ingredient already known by one of names.
func duplicate(ctx context.Context, q db.Querier, self string, names []string) (string, string, error) {
	var id, name string
	err := q.QueryRow(ctx, `
		SELECT i.id, i.name FROM ingredients i
		WHERE i.id IS DISTINCT FROM $1::uuid AND (
		      lower(regexp_replace(btrim(i.name), '\s+', ' ', 'g')) = ANY (SELECT lower(x) FROM unnest($2::text[]) x)
		   OR EXISTS (SELECT 1 FROM ingredient_aliases a WHERE a.ingredient_id = i.id
		              AND lower(regexp_replace(btrim(a.alias), '\s+', ' ', 'g')) = ANY (SELECT lower(x) FROM unnest($2::text[]) x)))
		LIMIT 1`, nilIfEmpty(self), names).Scan(&id, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil
	}
	return id, name, err
}

func (s *Service) CreateIngredient(ctx context.Context, actor string, in IngredientInput) (*Ingredient, error) {
	if in.BaseUnit == nil {
		in.BaseUnit = new(string)
	}
	var id string
	err := s.tx(ctx, func(tx pgx.Tx) error {
		v, aliases, err := s.validateIngredient(ctx, tx, nil, in)
		if err != nil {
			return err
		}
		if dupID, dupName, err := duplicate(ctx, tx, "", append([]string{v.Name}, aliases...)); err != nil {
			return err
		} else if dupID != "" {
			return conflict("duplicate_ingredient", "\""+dupName+"\" already covers this name. Add it as an alias there instead of a second ingredient.")
		}
		err = tx.QueryRow(ctx, `
			INSERT INTO ingredients (name, description, category, base_unit, purchasing_unit, preparation_state, allergens, storage,
			                         storage_notes, shelf_life_note, active, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) RETURNING id`,
			v.Name, v.Description, v.Category, v.BaseUnit, v.PurchasingUnit, v.PreparationState, v.Allergens, v.Storage,
			v.StorageNotes, v.ShelfLifeNote, v.Active, nilIfEmpty(actor)).Scan(&id)
		if err != nil {
			return err
		}
		if err := setAliases(ctx, tx, id, aliases); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "ingredient.created", TargetType: "ingredient", TargetID: id})
	})
	if err != nil {
		return nil, err
	}
	return s.Ingredient(ctx, id)
}

func setAliases(ctx context.Context, q db.Querier, id string, aliases []string) error {
	if _, err := q.Exec(ctx, `DELETE FROM ingredient_aliases WHERE ingredient_id = $1`, id); err != nil {
		return err
	}
	for _, a := range aliases {
		if _, err := q.Exec(ctx, `INSERT INTO ingredient_aliases (ingredient_id, alias) VALUES ($1, $2)`, id, a); err != nil {
			if db.IsUniqueViolation(err, "") {
				return conflict("duplicate_ingredient", "\""+a+"\" is already an alias of another ingredient.")
			}
			return err
		}
	}
	return nil
}

func (s *Service) UpdateIngredient(ctx context.Context, actor, id string, in IngredientInput) (*Ingredient, error) {
	cur, err := s.Ingredient(ctx, id)
	if err != nil {
		return nil, err
	}
	err = s.tx(ctx, func(tx pgx.Tx) error {
		v, aliases, err := s.validateIngredient(ctx, tx, cur, in)
		if err != nil {
			return err
		}
		names := []string{v.Name}
		if in.Aliases != nil {
			names = append(names, aliases...)
		}
		if dupID, dupName, err := duplicate(ctx, tx, id, names); err != nil {
			return err
		} else if dupID != "" {
			return conflict("duplicate_ingredient", "\""+dupName+"\" already covers this name.")
		}
		if v.BaseUnit != cur.BaseUnit {
			// Quantities already approved were reasoned about in the old unit.
			var used bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM recipe_version_ingredients l JOIN recipe_versions rv ON rv.id = l.version_id
				WHERE l.ingredient_id = $1 AND rv.status NOT IN ('draft', 'changes_requested'))`, id).Scan(&used); err != nil {
				return err
			}
			if used {
				return conflict("base_unit_in_use", "This ingredient is in submitted or approved recipes, so its base unit is fixed.")
			}
		}
		if _, err := tx.Exec(ctx, `
			UPDATE ingredients SET name = $2, description = $3, category = $4, base_unit = $5, purchasing_unit = $6, preparation_state = $7,
			       allergens = $8, storage = $9, storage_notes = $10, shelf_life_note = $11, active = $12, updated_at = now()
			WHERE id = $1`, id, v.Name, v.Description, v.Category, v.BaseUnit, v.PurchasingUnit, v.PreparationState, v.Allergens,
			v.Storage, v.StorageNotes, v.ShelfLifeNote, v.Active); err != nil {
			return err
		}
		if in.Aliases != nil {
			if err := setAliases(ctx, tx, id, aliases); err != nil {
				return err
			}
		}
		meta := map[string]any{}
		if !slices.Equal(v.Allergens, cur.Allergens) {
			meta["allergens_changed"] = true
		}
		if v.Active != cur.Active {
			meta["active"] = v.Active
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "ingredient.updated", TargetType: "ingredient", TargetID: id, Metadata: meta})
	})
	if err != nil {
		return nil, err
	}
	return s.Ingredient(ctx, id)
}

// ConversionInput records an ingredient-specific conversion.
type ConversionInput struct {
	From        string `json:"from_unit"`
	To          string `json:"to_unit"`
	Factor      string `json:"factor"`
	Approximate bool   `json:"approximate"`
	Source      string `json:"source"`
}

// SetConversion adds or replaces the conversion between two units of
// different dimensions. Within a dimension the exact unit factors apply and
// nothing is recorded.
func (s *Service) SetConversion(ctx context.Context, actor, id string, in ConversionInput) (*Ingredient, error) {
	if _, err := s.Ingredient(ctx, id); err != nil {
		return nil, err
	}
	in.Source = strings.TrimSpace(in.Source)
	factor, err := qty.Parse(in.Factor)
	if err != nil || factor.Sign() <= 0 {
		return nil, fieldErr("factor", "Enter how many of the second unit one of the first makes (a positive number).")
	}
	if n := runes(in.Source); n < 1 || n > 300 {
		return nil, fieldErr("source", "Say where the figure comes from (a supplier specification, a measurement).")
	}
	err = s.tx(ctx, func(tx pgx.Tx) error {
		c, err := NewConverter(ctx, tx)
		if err != nil {
			return err
		}
		fd, td := c.Dimension(in.From), c.Dimension(in.To)
		if fd == "" || td == "" {
			return fieldErr("from_unit", "Choose two known units.")
		}
		if fd == td {
			return fieldErr("to_unit", "These units already convert exactly; record conversions only between different kinds of measure.")
		}
		if (fd == "count" || td == "count") && !in.Approximate {
			return fieldErr("approximate", "Pieces vary in size, so a piece conversion is an average. Mark it approximate.")
		}
		if _, err := tx.Exec(ctx, `DELETE FROM ingredient_unit_conversions WHERE ingredient_id = $1
			AND ((from_unit = $2 AND to_unit = $3) OR (from_unit = $3 AND to_unit = $2))`, id, in.From, in.To); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO ingredient_unit_conversions (ingredient_id, from_unit, to_unit, factor, approximate, source, created_by)
			VALUES ($1, $2, $3, $4::numeric, $5, $6, $7)`, id, in.From, in.To, factor.Format(6), in.Approximate, in.Source, nilIfEmpty(actor)); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "ingredient.conversion_set", TargetType: "ingredient", TargetID: id,
			Metadata: map[string]any{"from": in.From, "to": in.To, "factor": factor.Format(6), "approximate": in.Approximate}})
	})
	if err != nil {
		return nil, err
	}
	return s.Ingredient(ctx, id)
}

// ---------------------------------------------------------------- cost references

// CostRef is a reference cost used for estimates only.
type CostRef struct {
	ID            string    `json:"id"`
	Kind          string    `json:"kind"`
	IngredientID  *string   `json:"ingredient_id"`
	Label         string    `json:"label"`
	AmountMinor   int64     `json:"amount_minor"`
	Currency      string    `json:"currency"`
	PerQuantity   string    `json:"per_quantity"`
	Unit          string    `json:"unit"`
	Source        string    `json:"source"`
	EffectiveFrom string    `json:"effective_from"`
	CreatedAt     time.Time `json:"created_at"`
}

func (s *Service) costRefs(ctx context.Context, q db.Querier, kind, ingredientID string) ([]CostRef, error) {
	rows, err := q.Query(ctx, `
		SELECT id, kind, ingredient_id, label, amount_minor, currency, trim_scale(per_quantity)::text, unit, source, effective_from::text, created_at
		FROM ingredient_cost_references
		WHERE kind = $1 AND ($2::uuid IS NULL OR ingredient_id = $2::uuid)
		ORDER BY effective_from DESC, created_at DESC LIMIT 50`, kind, nilIfEmpty(ingredientID))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (CostRef, error) {
		var c CostRef
		err := r.Scan(&c.ID, &c.Kind, &c.IngredientID, &c.Label, &c.AmountMinor, &c.Currency, &c.PerQuantity, &c.Unit, &c.Source, &c.EffectiveFrom, &c.CreatedAt)
		return c, err
	})
}

func (s *Service) CostReferences(ctx context.Context, kind string) ([]CostRef, error) {
	if kind != "ingredient" && kind != "packaging" {
		kind = "packaging"
	}
	return s.costRefs(ctx, s.pool, kind, "")
}

// CostInput records a reference cost.
type CostInput struct {
	Kind          string `json:"kind"`
	IngredientID  string `json:"ingredient_id"`
	Label         string `json:"label"`
	AmountMinor   int64  `json:"amount_minor"`
	PerQuantity   string `json:"per_quantity"`
	Unit          string `json:"unit"`
	Source        string `json:"source"`
	EffectiveFrom string `json:"effective_from"`
}

func (s *Service) AddCost(ctx context.Context, actor string, in CostInput) (*CostRef, error) {
	if in.Kind == "" {
		in.Kind = "ingredient"
	}
	var f []httpx.FieldError
	bad := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	if in.Kind != "ingredient" && in.Kind != "packaging" {
		bad("kind", "Kind is ingredient or packaging.")
	}
	if in.Kind == "ingredient" && !uuidPattern.MatchString(in.IngredientID) {
		bad("ingredient_id", "Choose the ingredient.")
	}
	if in.Kind == "packaging" {
		in.IngredientID = ""
		in.Unit = "portion"
		if in.Label = clean(in.Label); in.Label == "" {
			bad("label", "Name the packaging (for example, \"insulated box for a 2-person kit\").")
		}
	}
	if in.AmountMinor < 0 || in.AmountMinor > 100_000_000 {
		bad("amount_minor", "Enter the amount in cents.")
	}
	per, err := qty.Parse(in.PerQuantity)
	if err != nil || per.Sign() <= 0 {
		bad("per_quantity", "Enter the quantity the amount buys.")
	}
	in.Source = strings.TrimSpace(in.Source)
	if n := runes(in.Source); n < 1 || n > 300 {
		bad("source", "Say where the price comes from. Estimates need a source.")
	}
	day, err := time.Parse("2006-01-02", in.EffectiveFrom)
	if err != nil {
		bad("effective_from", "Enter the date the cost applies from.")
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	var id string
	err = s.tx(ctx, func(tx pgx.Tx) error {
		if in.Kind == "ingredient" {
			c, err := NewConverter(ctx, tx)
			if err != nil {
				return err
			}
			if c.Dimension(in.Unit) == "" {
				return fieldErr("unit", "Choose a unit.")
			}
			var ok bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM ingredients WHERE id = $1)`, in.IngredientID).Scan(&ok); err != nil || !ok {
				if err != nil {
					return err
				}
				return fieldErr("ingredient_id", "Ingredient not found.")
			}
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO ingredient_cost_references (ingredient_id, kind, label, amount_minor, per_quantity, unit, source, effective_from, created_by)
			VALUES ($1, $2, $3, $4, $5::numeric, $6, $7, $8, $9) RETURNING id`,
			nilIfEmpty(in.IngredientID), in.Kind, in.Label, in.AmountMinor, per.String(), in.Unit, in.Source, day, nilIfEmpty(actor)).Scan(&id); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "cost_reference.created", TargetType: "cost_reference", TargetID: id,
			Metadata: map[string]any{"kind": in.Kind, "amount_minor": in.AmountMinor}})
	})
	if err != nil {
		return nil, err
	}
	refs, err := s.costRefs(ctx, s.pool, in.Kind, in.IngredientID)
	if err != nil {
		return nil, err
	}
	for _, r := range refs {
		if r.ID == id {
			return &r, nil
		}
	}
	return nil, httpx.ErrNotFound
}
