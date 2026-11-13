package culinary

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/qty"
	"mise.tt/api/internal/vocab"
)

var (
	VersionStatuses  = []string{"draft", "submitted", "under_review", "changes_requested", "approved", "published", "archived"}
	Substitutions    = []string{"none", "equivalent_allowed", "approval_required"}
	NutritionSources = []string{"laboratory_analysis", "food_composition_database", "supplier_specification", "other"}
	editable         = []string{"draft", "changes_requested"}
)

// Recipe is a recipe with its versions.
type Recipe struct {
	ID          string    `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Category    string    `json:"category"`
	MealID      *string   `json:"meal_id"`
	MealName    *string   `json:"meal_name"`
	ChefID      *string   `json:"chef_id"`
	ChefName    *string   `json:"chef_name"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	// The latest version of any status, and the version production uses for
	// the current open week, if any.
	Latest        VersionSummary   `json:"latest"`
	InUse         *VersionSummary  `json:"in_use"`
	Versions      []VersionSummary `json:"versions,omitempty"`
	Collaborators []Person         `json:"collaborators,omitempty"`
	// What the caller may do now.
	CanEdit bool `json:"can_edit"`
}

type Person struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type VersionSummary struct {
	ID            string     `json:"id"`
	Number        int        `json:"number"`
	Status        string     `json:"status"`
	EffectiveFrom *string    `json:"effective_from"`
	ChangeSummary string     `json:"change_summary"`
	UpdatedAt     time.Time  `json:"updated_at"`
	PublishedAt   *time.Time `json:"published_at"`
}

// Version is one recipe version in full.
type Version struct {
	VersionSummary
	RecipeID              string     `json:"recipe_id"`
	YieldServings         int        `json:"yield_servings"`
	ServingSize           string     `json:"serving_size"`
	ServingSizeG          *string    `json:"serving_size_g"`
	KitchenPrepMinutes    *int       `json:"kitchen_prep_minutes"`
	CustomerCookMinutes   *int       `json:"customer_cook_minutes"`
	StorageInstructions   string     `json:"storage_instructions"`
	PackagingInstructions string     `json:"packaging_instructions"`
	CustomerInstructions  string     `json:"customer_instructions"`
	DietaryTags           []string   `json:"dietary_tags"`
	CrossContactNote      string     `json:"cross_contact_note"`
	AuthorID              *string    `json:"author_id"`
	AuthorName            string     `json:"author_name"`
	SubmittedAt           *time.Time `json:"submitted_at"`
	ApprovedAt            *time.Time `json:"approved_at"`
	ApprovedByName        string     `json:"approved_by_name"`
	Lines                 []Line     `json:"ingredients"`
	PreparationSteps      []string   `json:"preparation_steps"`
	AssemblySteps         []string   `json:"assembly_steps"`
	Contains              []string   `json:"allergens_contains"`
	MayContain            []string   `json:"allergens_may_contain"`
	// Allergens of the ingredients used, and those not yet declared.
	IngredientAllergens []string  `json:"ingredient_allergens"`
	MissingAllergens    []string  `json:"missing_allergens"`
	Nutrition           Nutrition `json:"nutrition"`
	Reviews             []Review  `json:"reviews"`
	Signoff             Signoff   `json:"signoff"`
	// What stops this version moving on, in plain words.
	Problems []string `json:"problems"`
}

type Line struct {
	ID                  string   `json:"id"`
	Position            int      `json:"position"`
	IngredientID        string   `json:"ingredient_id"`
	IngredientName      string   `json:"ingredient_name"`
	IngredientActive    bool     `json:"ingredient_active"`
	BaseUnit            string   `json:"base_unit"`
	Quantity            qty.Q    `json:"quantity"`
	Unit                string   `json:"unit"`
	Preparation         string   `json:"preparation"`
	YieldPercent        *string  `json:"yield_percent"`
	Optional            bool     `json:"optional"`
	Substitution        string   `json:"substitution"`
	Note                string   `json:"note"`
	IngredientAllergens []string `json:"ingredient_allergens"`
}

// Nutrition is per serving. Strings are decimals; nil is "not provided".
type Nutrition struct {
	EnergyKcal *string `json:"energy_kcal"`
	ProteinG   *string `json:"protein_g"`
	CarbsG     *string `json:"carbs_g"`
	FatG       *string `json:"fat_g"`
	SatFatG    *string `json:"sat_fat_g"`
	FibreG     *string `json:"fibre_g"`
	SugarG     *string `json:"sugar_g"`
	SodiumMg   *string `json:"sodium_mg"`
	Source     *string `json:"source"`
	Method     string  `json:"method"`
	Complete   bool    `json:"complete"`
	// Verified: signed off by a nutritionist on this version, and not since invalidated.
	Verified bool `json:"verified"`
}

func (n Nutrition) values() []*string {
	return []*string{n.EnergyKcal, n.ProteinG, n.CarbsG, n.FatG, n.SatFatG, n.FibreG, n.SugarG, n.SodiumMg}
}

type Review struct {
	ID           string     `json:"id"`
	Stage        string     `json:"stage"`
	Decision     string     `json:"decision"`
	Notes        string     `json:"notes"`
	ReviewerName string     `json:"reviewer_name"`
	CreatedAt    time.Time  `json:"created_at"`
	Invalidated  *time.Time `json:"invalidated_at"`
}

// Signoff says which valid approvals the version holds.
type Signoff struct {
	Culinary  bool `json:"culinary"`
	Nutrition bool `json:"nutrition"`
	Final     bool `json:"final"`
}

// ---------------------------------------------------------------- reading

const recipeCols = `r.id, r.code, r.name, r.description, r.category, r.meal_id, m.name, r.chef_id, c.display_name, r.created_at, r.updated_at`

func scanRecipe(row pgx.Row, extra ...any) (Recipe, error) {
	var r Recipe
	dest := append([]any{&r.ID, &r.Code, &r.Name, &r.Description, &r.Category, &r.MealID, &r.MealName, &r.ChefID, &r.ChefName, &r.CreatedAt, &r.UpdatedAt}, extra...)
	err := row.Scan(dest...)
	return r, err
}

func summaries(ctx context.Context, q db.Querier, recipeID string) ([]VersionSummary, error) {
	rows, err := q.Query(ctx, `SELECT id, number, status, effective_from::text, change_summary, updated_at, published_at
		FROM recipe_versions WHERE recipe_id = $1 ORDER BY number DESC`, recipeID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (VersionSummary, error) {
		var v VersionSummary
		err := r.Scan(&v.ID, &v.Number, &v.Status, &v.EffectiveFrom, &v.ChangeSummary, &v.UpdatedAt, &v.PublishedAt)
		return v, err
	})
}

// RecipeFilter narrows the recipe list.
type RecipeFilter struct {
	Query  string
	Status string
	// Mine: recipes the caller owns or collaborates on.
	Mine       bool
	ChefID     string
	Page, Size int
}

type RecipePage struct {
	Items []Recipe `json:"items"`
	Total int      `json:"total"`
	Page  int      `json:"page"`
}

func (s *Service) Recipes(ctx context.Context, p *identity.Principal, f RecipeFilter) (*RecipePage, error) {
	if f.Size < 1 || f.Size > 100 {
		f.Size = 50
	}
	if f.Page < 1 {
		f.Page = 1
	}
	var status, chef *string
	if slices.Contains(VersionStatuses, f.Status) {
		status = &f.Status
	}
	if uuidPattern.MatchString(f.ChefID) {
		chef = &f.ChefID
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+recipeCols+`, lv.id, lv.number, lv.status, lv.effective_from::text, lv.change_summary, lv.updated_at, lv.published_at,
		       count(*) OVER ()::int
		FROM recipes r
		LEFT JOIN meals m ON m.id = r.meal_id
		LEFT JOIN chef_profiles c ON c.id = r.chef_id
		JOIN LATERAL (SELECT * FROM recipe_versions v WHERE v.recipe_id = r.id ORDER BY v.number DESC LIMIT 1) lv ON true
		WHERE ($1 = '' OR r.name ILIKE '%' || $1 || '%' OR r.code ILIKE '%' || $1 || '%')
		  AND ($2::text IS NULL OR lv.status = $2)
		  AND ($3::uuid IS NULL OR r.chef_id = $3::uuid)
		  AND (NOT $4 OR r.created_by = $5::uuid OR c.user_id = $5::uuid
		       OR EXISTS (SELECT 1 FROM recipe_collaborators rc WHERE rc.recipe_id = r.id AND rc.user_id = $5::uuid))
		ORDER BY lv.updated_at DESC
		LIMIT $6 OFFSET $7`, escapeLike(strings.TrimSpace(f.Query)), status, chef, f.Mine, p.UserID, f.Size, (f.Page-1)*f.Size)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := &RecipePage{Items: []Recipe{}, Page: f.Page}
	for rows.Next() {
		var lv VersionSummary
		r, err := scanRecipe(rows, &lv.ID, &lv.Number, &lv.Status, &lv.EffectiveFrom, &lv.ChangeSummary, &lv.UpdatedAt, &lv.PublishedAt, &out.Total)
		if err != nil {
			return nil, err
		}
		r.Latest = lv
		out.Items = append(out.Items, r)
	}
	return out, rows.Err()
}

func (s *Service) Recipe(ctx context.Context, p *identity.Principal, id string) (*Recipe, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	r, err := scanRecipe(s.pool.QueryRow(ctx, `SELECT `+recipeCols+` FROM recipes r LEFT JOIN meals m ON m.id = r.meal_id
		LEFT JOIN chef_profiles c ON c.id = r.chef_id WHERE r.id = $1`, id))
	if err != nil {
		return nil, notFoundIf(err)
	}
	if r.Versions, err = summaries(ctx, s.pool, id); err != nil {
		return nil, err
	}
	if len(r.Versions) > 0 {
		r.Latest = r.Versions[0]
	}
	week := s.currentWeek()
	if vid, ok, err := RecipeVersionForRecipe(ctx, s.pool, id, week); err != nil {
		return nil, err
	} else if ok {
		for i := range r.Versions {
			if r.Versions[i].ID == vid {
				r.InUse = &r.Versions[i]
			}
		}
	}
	rows, err := s.pool.Query(ctx, `SELECT u.id, btrim(u.first_name || ' ' || u.last_name) FROM recipe_collaborators rc JOIN users u ON u.id = rc.user_id
		WHERE rc.recipe_id = $1 ORDER BY 2`, id)
	if err != nil {
		return nil, err
	}
	if r.Collaborators, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Person]); err != nil {
		return nil, err
	}
	r.CanEdit, err = s.canEdit(ctx, s.pool, p, id)
	return &r, err
}

func (s *Service) currentWeek() time.Time {
	if s.Weeks == nil {
		return s.Now()
	}
	return s.Weeks.NextOpenWeek(s.Now())
}

// canEdit: recipes.edit and the recipe's chef, creator or a collaborator;
// or recipes.approve (culinary direction may edit any recipe).
func (s *Service) canEdit(ctx context.Context, q db.Querier, p *identity.Principal, recipeID string) (bool, error) {
	if p.Can(identity.PermRecipesApprove) {
		return true, nil
	}
	if !p.Can(identity.PermRecipesEdit) {
		return false, nil
	}
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM recipes r LEFT JOIN chef_profiles c ON c.id = r.chef_id WHERE r.id = $1
		AND (r.created_by = $2 OR c.user_id = $2 OR EXISTS (SELECT 1 FROM recipe_collaborators rc WHERE rc.recipe_id = r.id AND rc.user_id = $2)))`,
		recipeID, p.UserID).Scan(&ok)
	return ok, err
}

var errNotYours = httpx.NewError(403, "not_your_recipe", "Only the recipe's chef, its collaborators or the culinary director can change it.")

// Version returns version number n of a recipe (0: the latest).
func (s *Service) Version(ctx context.Context, recipeID string, n int) (*Version, error) {
	if !uuidPattern.MatchString(recipeID) {
		return nil, httpx.ErrNotFound
	}
	return s.version(ctx, s.pool, recipeID, n)
}

func (s *Service) version(ctx context.Context, q db.Querier, recipeID string, n int) (*Version, error) {
	var v Version
	err := q.QueryRow(ctx, `
		SELECT v.id, v.number, v.status, v.effective_from::text, v.change_summary, v.updated_at, v.published_at, v.recipe_id,
		       v.yield_servings, v.serving_size, trim_scale(v.serving_size_g)::text, v.kitchen_prep_minutes, v.customer_cook_minutes,
		       v.storage_instructions, v.packaging_instructions, v.customer_instructions, v.dietary_tags, v.cross_contact_note,
		       v.author_id, COALESCE(btrim(a.first_name || ' ' || a.last_name), ''), v.submitted_at, v.approved_at,
		       COALESCE(btrim(ap.first_name || ' ' || ap.last_name), '')
		FROM recipe_versions v LEFT JOIN users a ON a.id = v.author_id LEFT JOIN users ap ON ap.id = v.approved_by
		WHERE v.recipe_id = $1 AND ($2 = 0 OR v.number = $2)
		ORDER BY v.number DESC LIMIT 1`, recipeID, n).Scan(
		&v.ID, &v.Number, &v.Status, &v.EffectiveFrom, &v.ChangeSummary, &v.UpdatedAt, &v.PublishedAt, &v.RecipeID,
		&v.YieldServings, &v.ServingSize, &v.ServingSizeG, &v.KitchenPrepMinutes, &v.CustomerCookMinutes,
		&v.StorageInstructions, &v.PackagingInstructions, &v.CustomerInstructions, &v.DietaryTags, &v.CrossContactNote,
		&v.AuthorID, &v.AuthorName, &v.SubmittedAt, &v.ApprovedAt, &v.ApprovedByName)
	if err != nil {
		return nil, notFoundIf(err)
	}
	if err := s.fill(ctx, q, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// VersionByID loads a version by its id (production uses it).
func (s *Service) VersionByID(ctx context.Context, q db.Querier, id string) (*Version, error) {
	var recipeID string
	var n int
	if err := q.QueryRow(ctx, `SELECT recipe_id, number FROM recipe_versions WHERE id = $1`, id).Scan(&recipeID, &n); err != nil {
		return nil, notFoundIf(err)
	}
	return s.version(ctx, q, recipeID, n)
}

func (s *Service) fill(ctx context.Context, q db.Querier, v *Version) error {
	rows, err := q.Query(ctx, `
		SELECT l.id, l.position, l.ingredient_id, i.name, i.active, i.base_unit, trim_scale(l.quantity)::text, l.unit, l.preparation,
		       trim_scale(l.yield_percent)::text, l.optional, l.substitution, l.note, i.allergens
		FROM recipe_version_ingredients l JOIN ingredients i ON i.id = l.ingredient_id
		WHERE l.version_id = $1 ORDER BY l.position`, v.ID)
	if err != nil {
		return err
	}
	v.Lines, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Line, error) {
		var l Line
		var amount string
		err := r.Scan(&l.ID, &l.Position, &l.IngredientID, &l.IngredientName, &l.IngredientActive, &l.BaseUnit, &amount, &l.Unit,
			&l.Preparation, &l.YieldPercent, &l.Optional, &l.Substitution, &l.Note, &l.IngredientAllergens)
		if err == nil {
			l.Quantity, err = qty.Parse(amount)
		}
		return l, err
	})
	if err != nil {
		return err
	}
	v.PreparationSteps, v.AssemblySteps = []string{}, []string{}
	rows, err = q.Query(ctx, `SELECT kind, body FROM recipe_steps WHERE version_id = $1 ORDER BY kind, position`, v.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var kind, body string
		if err := rows.Scan(&kind, &body); err != nil {
			rows.Close()
			return err
		}
		if kind == "preparation" {
			v.PreparationSteps = append(v.PreparationSteps, body)
		} else {
			v.AssemblySteps = append(v.AssemblySteps, body)
		}
	}
	rows.Close()
	v.Contains, v.MayContain = []string{}, []string{}
	rows, err = q.Query(ctx, `SELECT allergen, kind FROM recipe_allergens WHERE version_id = $1 ORDER BY allergen`, v.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var a, kind string
		if err := rows.Scan(&a, &kind); err != nil {
			rows.Close()
			return err
		}
		if kind == "contains" {
			v.Contains = append(v.Contains, a)
		} else {
			v.MayContain = append(v.MayContain, a)
		}
	}
	rows.Close()
	n := &v.Nutrition
	err = q.QueryRow(ctx, `SELECT trim_scale(energy_kcal)::text, trim_scale(protein_g)::text, trim_scale(carbs_g)::text, trim_scale(fat_g)::text, trim_scale(sat_fat_g)::text, trim_scale(fibre_g)::text,
		trim_scale(sugar_g)::text, trim_scale(sodium_mg)::text, source, method FROM recipe_nutrition WHERE version_id = $1`, v.ID).Scan(
		&n.EnergyKcal, &n.ProteinG, &n.CarbsG, &n.FatG, &n.SatFatG, &n.FibreG, &n.SugarG, &n.SodiumMg, &n.Source, &n.Method)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	n.Complete = n.Source != nil
	for _, x := range n.values() {
		if x == nil {
			n.Complete = false
		}
	}
	rows, err = q.Query(ctx, `SELECT r.id, r.stage, r.decision, r.notes, COALESCE(btrim(u.first_name || ' ' || u.last_name), ''), r.created_at, r.invalidated_at
		FROM recipe_reviews r LEFT JOIN users u ON u.id = r.reviewer_id WHERE r.version_id = $1 ORDER BY r.created_at, r.id`, v.ID)
	if err != nil {
		return err
	}
	if v.Reviews, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Review]); err != nil {
		return err
	}
	for _, r := range v.Reviews {
		if r.Invalidated != nil {
			continue
		}
		switch {
		case r.Stage == "culinary" && r.Decision == "approved":
			v.Signoff.Culinary = true
		case r.Stage == "nutrition" && r.Decision == "approved":
			v.Signoff.Nutrition = true
		case r.Stage == "final" && r.Decision == "approved":
			v.Signoff.Final = true
		}
	}
	v.Nutrition.Verified = v.Signoff.Nutrition && slices.Contains([]string{"under_review", "approved", "published", "archived"}, v.Status)
	v.IngredientAllergens = []string{}
	for _, l := range v.Lines {
		for _, a := range l.IngredientAllergens {
			if !slices.Contains(v.IngredientAllergens, a) {
				v.IngredientAllergens = append(v.IngredientAllergens, a)
			}
		}
	}
	slices.Sort(v.IngredientAllergens)
	v.MissingAllergens = []string{}
	for _, a := range v.IngredientAllergens {
		if !slices.Contains(v.Contains, a) {
			v.MissingAllergens = append(v.MissingAllergens, a)
		}
	}
	v.Problems = problems(v)
	return nil
}

// problems lists what stands between a version and approval.
func problems(v *Version) []string {
	out := []string{}
	if len(v.Lines) == 0 {
		out = append(out, "Add the ingredients and their quantities.")
	}
	for _, l := range v.Lines {
		if !l.IngredientActive {
			out = append(out, l.IngredientName+" is no longer an active ingredient.")
		}
	}
	if len(v.PreparationSteps) == 0 {
		out = append(out, "Add at least one preparation step.")
	}
	for _, a := range v.MissingAllergens {
		out = append(out, "An ingredient contains "+vocab.Allergens.Label(a)+", which is not declared.")
	}
	if !v.Nutrition.Complete {
		out = append(out, "Nutrition per serving is incomplete (every figure and its source are needed).")
	}
	return out
}

// submitProblems are the ones that block submission (nutrition and
// allergens may still be worked on with the nutritionist).
func submitProblems(v *Version) []string {
	out := []string{}
	if len(v.Lines) == 0 {
		out = append(out, "Add the ingredients and their quantities.")
	}
	for _, l := range v.Lines {
		if !l.IngredientActive {
			out = append(out, l.IngredientName+" is no longer an active ingredient.")
		}
	}
	if len(v.PreparationSteps) == 0 {
		out = append(out, "Add at least one preparation step.")
	}
	return out
}

// ---------------------------------------------------------------- writing

// RecipeInput creates a recipe or changes its header.
type RecipeInput struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Category    *string `json:"category"`
	// "" unlinks.
	MealID *string `json:"meal_id"`
	ChefID *string `json:"chef_id"`
	// Replaces the collaborator list when present (staff user ids).
	Collaborators *[]string `json:"collaborators"`
}

func (s *Service) nextCode(ctx context.Context, q db.Querier) (string, error) {
	var n int64
	if err := q.QueryRow(ctx, `UPDATE recipe_counters SET last = last + 1 RETURNING last`).Scan(&n); err != nil {
		return "", err
	}
	return fmt.Sprintf("RCP-%04d", n), nil
}

func (s *Service) CreateRecipe(ctx context.Context, p *identity.Principal, in RecipeInput) (*Recipe, error) {
	if in.Name == nil {
		in.Name = new(string)
	}
	var id string
	err := s.tx(ctx, func(tx pgx.Tx) error {
		code, err := s.nextCode(ctx, tx)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO recipes (code, name, created_by) VALUES ($1, 'New recipe', $2) RETURNING id`, code, p.UserID).Scan(&id); err != nil {
			return err
		}
		// A chef creating a recipe owns it by default.
		if in.ChefID == nil {
			var chef string
			if err := tx.QueryRow(ctx, `SELECT id FROM chef_profiles WHERE user_id = $1`, p.UserID).Scan(&chef); err == nil {
				in.ChefID = &chef
			}
		}
		if err := s.applyHeader(ctx, tx, id, in); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO recipe_versions (recipe_id, number, author_id) VALUES ($1, 1, $2)`, id, p.UserID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: "recipe.created", TargetType: "recipe", TargetID: id})
	})
	if err != nil {
		return nil, err
	}
	return s.Recipe(ctx, p, id)
}

func (s *Service) applyHeader(ctx context.Context, tx pgx.Tx, id string, in RecipeInput) error {
	var f []httpx.FieldError
	bad := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	if in.Name != nil {
		name := clean(*in.Name)
		if n := runes(name); n < 1 || n > 100 {
			bad("name", "Give the recipe a name (up to 100 characters).")
		} else if _, err := tx.Exec(ctx, `UPDATE recipes SET name = $2 WHERE id = $1`, id, name); err != nil {
			return err
		}
	}
	if in.Description != nil {
		d := strings.TrimSpace(*in.Description)
		if runes(d) > 2000 {
			bad("description", "Keep the description under 2,000 characters.")
		} else if _, err := tx.Exec(ctx, `UPDATE recipes SET description = $2 WHERE id = $1`, id, d); err != nil {
			return err
		}
	}
	if in.Category != nil {
		c := clean(*in.Category)
		if runes(c) > 60 {
			bad("category", "Keep the category under 60 characters.")
		} else if _, err := tx.Exec(ctx, `UPDATE recipes SET category = $2 WHERE id = $1`, id, c); err != nil {
			return err
		}
	}
	if in.MealID != nil {
		var published bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM recipe_versions WHERE recipe_id = $1 AND status IN ('published', 'archived'))`, id).Scan(&published); err != nil {
			return err
		}
		var cur *string
		if err := tx.QueryRow(ctx, `SELECT meal_id FROM recipes WHERE id = $1`, id).Scan(&cur); err != nil {
			return err
		}
		changed := (cur == nil) != (*in.MealID == "") || (cur != nil && *cur != *in.MealID)
		switch {
		case !changed:
		case published:
			bad("meal_id", "This recipe has been published for its meal; the link is fixed. Start a new recipe for another meal.")
		case *in.MealID == "":
			if _, err := tx.Exec(ctx, `UPDATE recipes SET meal_id = NULL WHERE id = $1`, id); err != nil {
				return err
			}
		case !uuidPattern.MatchString(*in.MealID):
			bad("meal_id", "Choose a meal from the catalogue.")
		default:
			if s.Catalog != nil {
				if _, ok, err := s.Catalog.MealName(ctx, tx, *in.MealID); err != nil {
					return err
				} else if !ok {
					bad("meal_id", "Choose a meal from the catalogue.")
					break
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE recipes SET meal_id = $2 WHERE id = $1`, id, *in.MealID); err != nil {
				if db.IsUniqueViolation(err, "recipes_meal_key") {
					bad("meal_id", "That meal already has a recipe.")
					break
				}
				return err
			}
		}
	}
	if in.ChefID != nil {
		var chef *string
		if *in.ChefID != "" {
			var ok bool
			if !uuidPattern.MatchString(*in.ChefID) {
				bad("chef_id", "Choose a chef.")
			} else if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM chef_profiles WHERE id = $1)`, *in.ChefID).Scan(&ok); err != nil {
				return err
			} else if !ok {
				bad("chef_id", "Choose a chef.")
			}
			chef = in.ChefID
		}
		if _, err := tx.Exec(ctx, `UPDATE recipes SET chef_id = $2 WHERE id = $1`, id, chef); err != nil {
			return err
		}
	}
	if in.Collaborators != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM recipe_collaborators WHERE recipe_id = $1`, id); err != nil {
			return err
		}
		for _, u := range *in.Collaborators {
			var ok bool
			if !uuidPattern.MatchString(u) {
				bad("collaborators", "Choose staff members.")
				continue
			}
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_key = ur.role_key
				JOIN users x ON x.id = ur.user_id WHERE ur.user_id = $1 AND rp.permission_key = 'recipes.edit' AND x.status = 'active')`, u).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				bad("collaborators", "Collaborators must be active staff who can edit recipes.")
				continue
			}
			if _, err := tx.Exec(ctx, `INSERT INTO recipe_collaborators (recipe_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, u); err != nil {
				return err
			}
		}
	}
	if len(f) > 0 {
		return httpx.ValidationError(f)
	}
	_, err := tx.Exec(ctx, `UPDATE recipes SET updated_at = now() WHERE id = $1`, id)
	return err
}

func (s *Service) UpdateRecipe(ctx context.Context, p *identity.Principal, id string, in RecipeInput) (*Recipe, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM recipes WHERE id = $1 FOR UPDATE`, id); err != nil {
			return err
		}
		ok, err := s.canEdit(ctx, tx, p, id)
		if err != nil {
			return err
		}
		if !ok {
			return errNotYours
		}
		if in.ChefID != nil && !p.Can(identity.PermRecipesApprove) {
			return httpx.NewError(403, "forbidden", "Only the culinary director reassigns a recipe's chef.")
		}
		if err := s.applyHeader(ctx, tx, id, in); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: "recipe.updated", TargetType: "recipe", TargetID: id})
	})
	if err != nil {
		return nil, err
	}
	return s.Recipe(ctx, p, id)
}

// DraftInput changes the open draft. Nil fields keep their value; a list
// that is present replaces the whole list.
type DraftInput struct {
	YieldServings         *int            `json:"yield_servings"`
	ServingSize           *string         `json:"serving_size"`
	ServingSizeG          *string         `json:"serving_size_g"`
	KitchenPrepMinutes    *int            `json:"kitchen_prep_minutes"`
	CustomerCookMinutes   *int            `json:"customer_cook_minutes"`
	StorageInstructions   *string         `json:"storage_instructions"`
	PackagingInstructions *string         `json:"packaging_instructions"`
	CustomerInstructions  *string         `json:"customer_instructions"`
	DietaryTags           *[]string       `json:"dietary_tags"`
	CrossContactNote      *string         `json:"cross_contact_note"`
	ChangeSummary         *string         `json:"change_summary"`
	Ingredients           *[]LineInput    `json:"ingredients"`
	PreparationSteps      *[]string       `json:"preparation_steps"`
	AssemblySteps         *[]string       `json:"assembly_steps"`
	Contains              *[]string       `json:"allergens_contains"`
	MayContain            *[]string       `json:"allergens_may_contain"`
	Nutrition             *NutritionInput `json:"nutrition"`
}

type LineInput struct {
	IngredientID string  `json:"ingredient_id"`
	Quantity     string  `json:"quantity"`
	Unit         string  `json:"unit"`
	Preparation  string  `json:"preparation"`
	YieldPercent *string `json:"yield_percent"`
	Optional     bool    `json:"optional"`
	Substitution string  `json:"substitution"`
	Note         string  `json:"note"`
}

type NutritionInput struct {
	EnergyKcal *string `json:"energy_kcal"`
	ProteinG   *string `json:"protein_g"`
	CarbsG     *string `json:"carbs_g"`
	FatG       *string `json:"fat_g"`
	SatFatG    *string `json:"sat_fat_g"`
	FibreG     *string `json:"fibre_g"`
	SugarG     *string `json:"sugar_g"`
	SodiumMg   *string `json:"sodium_mg"`
	Source     *string `json:"source"`
	Method     string  `json:"method"`
}

// SaveDraft edits the recipe's open version (draft or changes requested).
// Any edit invalidates the version's earlier reviews: they were given on
// different content, and nothing is carried forward.
func (s *Service) SaveDraft(ctx context.Context, p *identity.Principal, recipeID string, in DraftInput) (*Version, error) {
	if !uuidPattern.MatchString(recipeID) {
		return nil, httpx.ErrNotFound
	}
	var number int
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var vid, status string
		err := tx.QueryRow(ctx, `SELECT id, number, status FROM recipe_versions WHERE recipe_id = $1 ORDER BY number DESC LIMIT 1 FOR UPDATE`, recipeID).Scan(&vid, &number, &status)
		if err != nil {
			return notFoundIf(err)
		}
		ok, err := s.canEdit(ctx, tx, p, recipeID)
		if err != nil {
			return err
		}
		if !ok {
			return errNotYours
		}
		if !slices.Contains(editable, status) {
			return conflict("version_locked", "Version "+fmt.Sprint(number)+" is "+strings.ReplaceAll(status, "_", " ")+" and cannot be edited. Start a new revision to change it.")
		}
		if err := s.writeDraft(ctx, tx, vid, in); err != nil {
			return err
		}
		res, err := tx.Exec(ctx, `UPDATE recipe_reviews SET invalidated_at = now() WHERE version_id = $1 AND invalidated_at IS NULL`, vid)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE recipe_versions SET updated_at = now() WHERE id = $1`, vid); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE recipes SET updated_at = now() WHERE id = $1`, recipeID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: "recipe.draft_saved", TargetType: "recipe_version", TargetID: vid,
			Metadata: map[string]any{"version": number, "reviews_invalidated": res.RowsAffected()}})
	})
	if err != nil {
		return nil, err
	}
	return s.version(ctx, s.pool, recipeID, number)
}

func optDecimal(field string, v *string, max string, f *[]httpx.FieldError) *string {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil
	}
	q, err := qty.Parse(*v)
	if err != nil || q.Sign() < 0 || q.Cmp(qty.MustParse(max)) > 0 {
		*f = append(*f, httpx.FieldError{Field: field, Code: "invalid", Message: "Enter a number between 0 and " + max + "."})
		return nil
	}
	out := q.Format(1)
	return &out
}

func (s *Service) writeDraft(ctx context.Context, tx pgx.Tx, vid string, in DraftInput) error {
	var f []httpx.FieldError
	bad := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	text := func(field string, v *string, max int, col string) error {
		if v == nil {
			return nil
		}
		t := strings.TrimSpace(*v)
		if runes(t) > max {
			bad(field, fmt.Sprintf("Keep this under %d characters.", max))
			return nil
		}
		_, err := tx.Exec(ctx, `UPDATE recipe_versions SET `+col+` = $2 WHERE id = $1`, vid, t)
		return err
	}
	for _, t := range []struct {
		field string
		v     *string
		max   int
	}{{"serving_size", in.ServingSize, 60}, {"storage_instructions", in.StorageInstructions, 1000},
		{"packaging_instructions", in.PackagingInstructions, 1000}, {"customer_instructions", in.CustomerInstructions, 4000},
		{"cross_contact_note", in.CrossContactNote, 500}, {"change_summary", in.ChangeSummary, 500}} {
		if err := text(t.field, t.v, t.max, t.field); err != nil {
			return err
		}
	}
	if in.YieldServings != nil {
		if *in.YieldServings < 1 || *in.YieldServings > 100 {
			bad("yield_servings", "The recipe makes between 1 and 100 servings.")
		} else if _, err := tx.Exec(ctx, `UPDATE recipe_versions SET yield_servings = $2 WHERE id = $1`, vid, *in.YieldServings); err != nil {
			return err
		}
	}
	if in.ServingSizeG != nil {
		g := optDecimal("serving_size_g", in.ServingSizeG, "5000", &f)
		if _, err := tx.Exec(ctx, `UPDATE recipe_versions SET serving_size_g = $2::numeric WHERE id = $1`, vid, g); err != nil {
			return err
		}
	}
	for _, m := range []struct {
		field string
		v     *int
		max   int
	}{{"kitchen_prep_minutes", in.KitchenPrepMinutes, 1440}, {"customer_cook_minutes", in.CustomerCookMinutes, 480}} {
		if m.v == nil {
			continue
		}
		var val *int
		if *m.v >= 0 {
			if *m.v > m.max {
				bad(m.field, fmt.Sprintf("Up to %d minutes.", m.max))
				continue
			}
			val = m.v
		}
		if _, err := tx.Exec(ctx, `UPDATE recipe_versions SET `+m.field+` = $2 WHERE id = $1`, vid, val); err != nil {
			return err
		}
	}
	if in.DietaryTags != nil {
		tags := []string{}
		for _, t := range *in.DietaryTags {
			if !vocab.Dietary.Has(t) {
				bad("dietary_tags", "Unknown dietary tag: "+t+".")
			} else if !slices.Contains(tags, t) {
				tags = append(tags, t)
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE recipe_versions SET dietary_tags = $2 WHERE id = $1`, vid, tags); err != nil {
			return err
		}
	}
	if in.Ingredients != nil {
		if len(*in.Ingredients) > 200 {
			bad("ingredients", "Up to 200 ingredient lines.")
		}
		if _, err := tx.Exec(ctx, `DELETE FROM recipe_version_ingredients WHERE version_id = $1`, vid); err != nil {
			return err
		}
		conv, err := NewConverter(ctx, tx)
		if err != nil {
			return err
		}
		for i, l := range *in.Ingredients {
			field := fmt.Sprintf("ingredients[%d]", i)
			amount, err := qty.Parse(l.Quantity)
			if err != nil || amount.Sign() <= 0 || amount.Cmp(qty.Int(100_000_000)) >= 0 {
				bad(field+".quantity", "Enter a quantity greater than zero.")
				continue
			}
			if amount.Round(3).Cmp(amount) != 0 {
				bad(field+".quantity", "Quantities have at most 3 decimal places.")
				continue
			}
			if conv.Dimension(l.Unit) == "" {
				bad(field+".unit", "Choose a unit.")
				continue
			}
			var base string
			if !uuidPattern.MatchString(l.IngredientID) {
				bad(field+".ingredient_id", "Choose an ingredient from the catalogue.")
				continue
			}
			if err := tx.QueryRow(ctx, `SELECT base_unit FROM ingredients WHERE id = $1`, l.IngredientID).Scan(&base); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					bad(field+".ingredient_id", "Choose an ingredient from the catalogue.")
					continue
				}
				return err
			}
			// The line's unit must convert to the ingredient's base unit:
			// otherwise production could never total it.
			if _, _, err := conv.Convert(ctx, l.IngredientID, qty.Int(1), l.Unit, base); errors.Is(err, ErrIncompatible) {
				bad(field+".unit", "This ingredient is counted in "+base+" and has no recorded conversion from "+l.Unit+". Use "+base+", or record a conversion on the ingredient first.")
				continue
			} else if err != nil {
				return err
			}
			if l.Substitution == "" {
				l.Substitution = "none"
			}
			if !slices.Contains(Substitutions, l.Substitution) {
				bad(field+".substitution", "Choose a substitution policy.")
				continue
			}
			var yp *string
			if l.YieldPercent != nil && strings.TrimSpace(*l.YieldPercent) != "" {
				y, err := qty.Parse(*l.YieldPercent)
				if err != nil || y.Sign() <= 0 || y.Cmp(qty.Int(100)) > 0 {
					bad(field+".yield_percent", "Usable share is a percentage above 0 and at most 100.")
					continue
				}
				ys := y.Format(2)
				yp = &ys
			}
			prep, note := clean(l.Preparation), strings.TrimSpace(l.Note)
			if runes(prep) > 100 || runes(note) > 300 {
				bad(field+".preparation", "Keep the preparation under 100 characters and the note under 300.")
				continue
			}
			if _, err := tx.Exec(ctx, `INSERT INTO recipe_version_ingredients (version_id, position, ingredient_id, quantity, unit, preparation,
				yield_percent, optional, substitution, note) VALUES ($1, $2, $3, $4::numeric, $5, $6, $7::numeric, $8, $9, $10)`,
				vid, i, l.IngredientID, amount.String(), l.Unit, prep, yp, l.Optional, l.Substitution, note); err != nil {
				return err
			}
		}
	}
	for _, st := range []struct {
		kind  string
		steps *[]string
	}{{"preparation", in.PreparationSteps}, {"assembly", in.AssemblySteps}} {
		if st.steps == nil {
			continue
		}
		if _, err := tx.Exec(ctx, `DELETE FROM recipe_steps WHERE version_id = $1 AND kind = $2`, vid, st.kind); err != nil {
			return err
		}
		pos := 0
		for _, body := range *st.steps {
			body = strings.TrimSpace(body)
			if body == "" {
				continue
			}
			if runes(body) > 1000 || pos > 100 {
				bad(st.kind+"_steps", "Keep each step under 1,000 characters (up to 100 steps).")
				break
			}
			if _, err := tx.Exec(ctx, `INSERT INTO recipe_steps (version_id, kind, position, body) VALUES ($1, $2, $3, $4)`, vid, st.kind, pos, body); err != nil {
				return err
			}
			pos++
		}
	}
	if in.Contains != nil || in.MayContain != nil {
		var cur []struct{ a, k string }
		rows, err := tx.Query(ctx, `SELECT allergen, kind FROM recipe_allergens WHERE version_id = $1`, vid)
		if err != nil {
			return err
		}
		for rows.Next() {
			var a, k string
			if err := rows.Scan(&a, &k); err != nil {
				rows.Close()
				return err
			}
			cur = append(cur, struct{ a, k string }{a, k})
		}
		rows.Close()
		contains, may := []string{}, []string{}
		for _, c := range cur {
			if c.k == "contains" {
				contains = append(contains, c.a)
			} else {
				may = append(may, c.a)
			}
		}
		if in.Contains != nil {
			contains = *in.Contains
		}
		if in.MayContain != nil {
			may = *in.MayContain
		}
		if _, err := tx.Exec(ctx, `DELETE FROM recipe_allergens WHERE version_id = $1`, vid); err != nil {
			return err
		}
		seen := map[string]bool{}
		for kind, list := range map[string][]string{"contains": contains, "may_contain": may} {
			for _, a := range list {
				if !vocab.MealAllergens.Has(a) {
					bad("allergens_"+kind, "Unknown allergen: "+a+".")
					continue
				}
				if seen[a] {
					if kind == "may_contain" {
						continue // "contains" wins over "may contain"
					}
				}
				seen[a] = true
				if _, err := tx.Exec(ctx, `INSERT INTO recipe_allergens (version_id, allergen, kind) VALUES ($1, $2, $3)
					ON CONFLICT (version_id, allergen) DO UPDATE SET kind = 'contains'`, vid, a, kind); err != nil {
					return err
				}
			}
		}
	}
	if n := in.Nutrition; n != nil {
		vals := []*string{
			optDecimal("nutrition.energy_kcal", n.EnergyKcal, "5000", &f), optDecimal("nutrition.protein_g", n.ProteinG, "500", &f),
			optDecimal("nutrition.carbs_g", n.CarbsG, "500", &f), optDecimal("nutrition.fat_g", n.FatG, "500", &f),
			optDecimal("nutrition.sat_fat_g", n.SatFatG, "500", &f), optDecimal("nutrition.fibre_g", n.FibreG, "200", &f),
			optDecimal("nutrition.sugar_g", n.SugarG, "500", &f), optDecimal("nutrition.sodium_mg", n.SodiumMg, "20000", &f),
		}
		var source *string
		if n.Source != nil && *n.Source != "" {
			if !slices.Contains(NutritionSources, *n.Source) {
				bad("nutrition.source", "Choose where the figures come from.")
			} else {
				source = n.Source
			}
		}
		method := strings.TrimSpace(n.Method)
		if runes(method) > 500 {
			bad("nutrition.method", "Keep the method under 500 characters.")
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO recipe_nutrition (version_id, energy_kcal, protein_g, carbs_g, fat_g, sat_fat_g, fibre_g, sugar_g, sodium_mg, source, method, entered_by, updated_at)
			VALUES ($1, $2::numeric, $3::numeric, $4::numeric, $5::numeric, $6::numeric, $7::numeric, $8::numeric, $9::numeric, $10, $11, NULL, now())
			ON CONFLICT (version_id) DO UPDATE SET energy_kcal = EXCLUDED.energy_kcal, protein_g = EXCLUDED.protein_g, carbs_g = EXCLUDED.carbs_g,
			  fat_g = EXCLUDED.fat_g, sat_fat_g = EXCLUDED.sat_fat_g, fibre_g = EXCLUDED.fibre_g, sugar_g = EXCLUDED.sugar_g,
			  sodium_mg = EXCLUDED.sodium_mg, source = EXCLUDED.source, method = EXCLUDED.method, updated_at = now()`,
			vid, vals[0], vals[1], vals[2], vals[3], vals[4], vals[5], vals[6], vals[7], source, method); err != nil {
			return err
		}
	}
	if len(f) > 0 {
		return httpx.ValidationError(f)
	}
	return nil
}

// RecipeVersionForRecipe is the published version of a recipe in force for
// a delivery week: the latest effective week on or before it.
func RecipeVersionForRecipe(ctx context.Context, q db.Querier, recipeID string, week time.Time) (string, bool, error) {
	var id string
	err := q.QueryRow(ctx, `SELECT id FROM recipe_versions WHERE recipe_id = $1 AND status = 'published' AND effective_from <= $2
		ORDER BY effective_from DESC, number DESC LIMIT 1`, recipeID, week).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return id, err == nil, err
}

// RecipeVersionForMeal is the published recipe version production uses for
// a catalogue meal in a delivery week.
func RecipeVersionForMeal(ctx context.Context, q db.Querier, mealID string, week time.Time) (string, bool, error) {
	var id string
	err := q.QueryRow(ctx, `SELECT v.id FROM recipe_versions v JOIN recipes r ON r.id = v.recipe_id
		WHERE r.meal_id = $1 AND v.status = 'published' AND v.effective_from <= $2
		ORDER BY v.effective_from DESC, v.number DESC LIMIT 1`, mealID, week).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return id, err == nil, err
}
