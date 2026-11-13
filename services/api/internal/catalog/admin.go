package catalog

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/catalog/store"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/vocab"
)

// Staff-side catalogue management. Authorization is checked by the HTTP
// layer (catalog.read, catalog.manage, catalog.publish, nutrition.verify);
// these methods take the acting user's id for the audit log.

var (
	errVersionConflict = httpx.NewError(http.StatusConflict, "version_conflict",
		"Someone else saved this meal while you were editing. Reload it to see their changes, then make yours again.")
	errSlugTaken  = httpx.ValidationError([]httpx.FieldError{{Field: "slug", Code: "taken", Message: "Another item already uses this web address."}})
	errDemoLocked = httpx.NewError(http.StatusConflict, "demo_content",
		"This is demo content. It can be published only where demo content is enabled (CATALOG_DEMO_ENABLED).")
	errNoImage = httpx.NewError(http.StatusConflict, "image_required", "Add at least one photograph before publishing.")
)

type ImageInput struct {
	Src    string  `json:"src"`
	Alt    string  `json:"alt"`
	Width  *int32  `json:"width"`
	Height *int32  `json:"height"`
	Credit *string `json:"credit"`
}

type IngredientInput struct {
	Name     string  `json:"name"`
	Quantity *string `json:"quantity"`
}

// MealInput is a whole meal as the editor saves it. Version is the version
// the editor loaded; it is ignored on create.
type MealInput struct {
	Slug            string            `json:"slug"`
	Name            string            `json:"name"`
	Summary         string            `json:"summary"`
	Description     string            `json:"description"`
	CookingOverview string            `json:"cooking_overview"`
	CategoryID      string            `json:"category_id"`
	ChefID          *string           `json:"chef_id"`
	PrepMinutes     int               `json:"prep_minutes"`
	CookMinutes     int               `json:"cook_minutes"`
	Servings        int               `json:"servings"`
	Protein         *string           `json:"protein"`
	DietaryTags     []string          `json:"dietary_tags"`
	Allergens       []string          `json:"allergens"`
	AllergenStatus  string            `json:"allergen_status"`
	Images          []ImageInput      `json:"images"`
	Ingredients     []IngredientInput `json:"ingredients"`
	IsDemo          bool              `json:"is_demo"`
	Version         int32             `json:"version"`
}

type AdminNutrition struct {
	Calories   int16      `json:"calories"`
	ProteinG   float64    `json:"protein_g"`
	CarbsG     float64    `json:"carbs_g"`
	FatG       float64    `json:"fat_g"`
	FibreG     *float64   `json:"fibre_g"`
	SodiumMg   *int16     `json:"sodium_mg"`
	Status     string     `json:"status"`
	VerifiedAt *time.Time `json:"verified_at"`
}

// AdminMeal is a meal as staff see it, whatever its status.
type AdminMeal struct {
	ID              string            `json:"id"`
	Slug            string            `json:"slug"`
	Name            string            `json:"name"`
	Summary         string            `json:"summary"`
	Description     string            `json:"description"`
	CookingOverview string            `json:"cooking_overview"`
	CategoryID      string            `json:"category_id"`
	ChefID          *string           `json:"chef_id"`
	PrepMinutes     int16             `json:"prep_minutes"`
	CookMinutes     int16             `json:"cook_minutes"`
	Servings        int16             `json:"servings"`
	Protein         *string           `json:"protein"`
	DietaryTags     []string          `json:"dietary_tags"`
	Allergens       []string          `json:"allergens"`
	AllergenStatus  string            `json:"allergen_status"`
	Status          string            `json:"status"`
	PublishedAt     *time.Time        `json:"published_at"`
	IsDemo          bool              `json:"is_demo"`
	Version         int32             `json:"version"`
	Images          []ImageInput      `json:"images"`
	Ingredients     []IngredientInput `json:"ingredients"`
	Nutrition       *AdminNutrition   `json:"nutrition"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

type AdminMealRow struct {
	ID              string     `json:"id"`
	Slug            string     `json:"slug"`
	Name            string     `json:"name"`
	Status          string     `json:"status"`
	IsDemo          bool       `json:"is_demo"`
	CategoryName    string     `json:"category_name"`
	ChefName        *string    `json:"chef_name"`
	AllergenStatus  string     `json:"allergen_status"`
	NutritionStatus *string    `json:"nutrition_status"`
	PublishedAt     *time.Time `json:"published_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type AdminMealPage struct {
	Items    []AdminMealRow `json:"items"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}

func (s *Service) AdminList(ctx context.Context, status, query string, page, pageSize int) (*AdminMealPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}
	params := store.AdminListMealsParams{Lim: int32(pageSize), Off: int32((page - 1) * pageSize)}
	if status != "" {
		if !slices.Contains([]string{"draft", "published", "unpublished"}, status) {
			return nil, httpx.ValidationError([]httpx.FieldError{{Field: "status", Code: "invalid", Message: "Status is draft, published or unpublished."}})
		}
		params.Status = &status
	}
	if q := strings.TrimSpace(query); q != "" {
		like := likeEscaper.Replace(q)
		params.SearchLike = &like
	}
	rows, err := store.New(s.pool).AdminListMeals(ctx, params)
	if err != nil {
		return nil, err
	}
	out := &AdminMealPage{Items: []AdminMealRow{}, Page: page, PageSize: pageSize}
	for _, r := range rows {
		out.Total = r.Total
		out.Items = append(out.Items, AdminMealRow{ID: r.ID, Slug: r.Slug, Name: r.Name, Status: r.Status, IsDemo: r.IsDemo,
			CategoryName: r.CategoryName, ChefName: r.ChefName, AllergenStatus: r.AllergenStatus, NutritionStatus: r.NutritionStatus,
			PublishedAt: r.PublishedAt, UpdatedAt: r.UpdatedAt})
	}
	return out, nil
}

func (s *Service) AdminGet(ctx context.Context, id string) (*AdminMeal, error) {
	return s.adminMeal(ctx, s.pool, id)
}

func (s *Service) adminMeal(ctx context.Context, q db.Querier, id string) (*AdminMeal, error) {
	st := store.New(q)
	m, err := st.GetMeal(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	out := &AdminMeal{ID: m.ID, Slug: m.Slug, Name: m.Name, Summary: m.Summary, Description: m.Description,
		CookingOverview: m.CookingOverview, CategoryID: m.CategoryID, ChefID: m.ChefID, PrepMinutes: m.PrepMinutes,
		CookMinutes: m.CookMinutes, Servings: m.Servings, Protein: m.Protein, DietaryTags: m.DietaryTags,
		Allergens: m.Allergens, AllergenStatus: m.AllergenStatus, Status: m.Status, PublishedAt: m.PublishedAt,
		IsDemo: m.IsDemo, Version: m.Version, Images: []ImageInput{}, Ingredients: []IngredientInput{},
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
	images, err := st.ListImages(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, img := range images {
		out.Images = append(out.Images, ImageInput{Src: img.Src, Alt: img.Alt, Width: img.Width, Height: img.Height, Credit: img.Credit})
	}
	ingredients, err := st.ListIngredients(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, in := range ingredients {
		out.Ingredients = append(out.Ingredients, IngredientInput{Name: in.Name, Quantity: in.Quantity})
	}
	n, err := st.GetNutrition(ctx, id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, err
	default:
		out.Nutrition = &AdminNutrition{Calories: n.Calories, ProteinG: n.ProteinG, CarbsG: n.CarbsG, FatG: n.FatG,
			FibreG: n.FibreG, SodiumMg: n.SodiumMg, Status: n.Status, VerifiedAt: n.VerifiedAt}
	}
	return out, nil
}

// clean validates a meal and normalises it. It returns field errors.
func (s *Service) clean(in *MealInput) []httpx.FieldError {
	var f []httpx.FieldError
	bad := func(field, code, msg string) { f = append(f, httpx.FieldError{Field: field, Code: code, Message: msg}) }
	text := func(v *string, field string, min, max int, label string) {
		*v = strings.TrimSpace(*v)
		if n := len([]rune(*v)); n < min || n > max {
			if min > 0 {
				bad(field, "invalid", label+" must be "+itoa(min)+" to "+itoa(max)+" characters.")
			} else {
				bad(field, "too_long", label+" must be at most "+itoa(max)+" characters.")
			}
		}
	}
	text(&in.Name, "name", 1, 100, "The name")
	text(&in.Summary, "summary", 1, 200, "The summary")
	text(&in.Description, "description", 0, 3000, "The description")
	text(&in.CookingOverview, "cooking_overview", 0, 3000, "The cooking overview")
	in.Slug = strings.TrimSpace(in.Slug)
	if in.Slug == "" {
		in.Slug = Slugify(in.Name)
	}
	if !slugPattern.MatchString(in.Slug) || len(in.Slug) > 80 {
		bad("slug", "invalid", "Use lower-case letters, numbers and single hyphens.")
	}
	if in.CategoryID == "" {
		bad("category_id", "required", "Choose a category.")
	}
	if in.ChefID != nil && *in.ChefID == "" {
		in.ChefID = nil
	}
	if in.PrepMinutes < 0 || in.PrepMinutes > 240 {
		bad("prep_minutes", "out_of_range", "Preparation time must be 0 to 240 minutes.")
	}
	if in.CookMinutes < 0 || in.CookMinutes > 480 {
		bad("cook_minutes", "out_of_range", "Cooking time must be 0 to 480 minutes.")
	}
	if in.Servings < 1 || in.Servings > 12 {
		bad("servings", "out_of_range", "Servings must be 1 to 12.")
	}
	if in.Protein != nil && *in.Protein == "" {
		in.Protein = nil
	}
	if in.Protein != nil && !vocab.Proteins.Has(*in.Protein) {
		bad("protein", "invalid", "Choose a protein from the list.")
	}
	in.DietaryTags = uniq(in.DietaryTags)
	for _, t := range in.DietaryTags {
		if !vocab.Dietary.Has(t) {
			bad("dietary_tags", "invalid", "Choose dietary tags from the list.")
			break
		}
	}
	// A vegan meal is also vegetarian; store both so filters stay simple.
	if slices.Contains(in.DietaryTags, "vegan") && !slices.Contains(in.DietaryTags, "vegetarian") {
		in.DietaryTags = append(in.DietaryTags, "vegetarian")
	}
	in.Allergens = uniq(in.Allergens)
	for _, a := range in.Allergens {
		if !vocab.MealAllergens.Has(a) {
			bad("allergens", "invalid", "Choose allergens from the list.")
			break
		}
	}
	switch in.AllergenStatus {
	case "":
		in.AllergenStatus = "unconfirmed"
	case "unconfirmed", "declared":
	default:
		bad("allergen_status", "invalid", "Allergen status is unconfirmed or declared.")
	}
	if in.AllergenStatus == "unconfirmed" && len(in.Allergens) > 0 {
		bad("allergen_status", "inconsistent", "Mark the allergens as declared, or remove them until they are confirmed.")
	}
	if len(in.Images) > 12 {
		bad("images", "too_many", "Add up to 12 photographs.")
	}
	for i := range in.Images {
		img := &in.Images[i]
		img.Src, img.Alt = strings.TrimSpace(img.Src), strings.TrimSpace(img.Alt)
		if !s.imageAllowed(img.Src) {
			bad("images", "invalid_src", "Photographs must be in the customer app's /catalog/ folder or on an approved image host.")
			break
		}
		if img.Alt == "" || len([]rune(img.Alt)) > 200 {
			bad("images", "alt_required", "Describe every photograph for people who cannot see it (up to 200 characters).")
			break
		}
	}
	if len(in.Ingredients) > 60 {
		bad("ingredients", "too_many", "List up to 60 ingredients.")
	}
	for i := range in.Ingredients {
		ing := &in.Ingredients[i]
		ing.Name = strings.TrimSpace(ing.Name)
		if ing.Name == "" || len([]rune(ing.Name)) > 120 {
			bad("ingredients", "invalid", "Every ingredient needs a name of up to 120 characters.")
			break
		}
		if ing.Quantity != nil {
			q := strings.TrimSpace(*ing.Quantity)
			ing.Quantity = &q
			if q == "" {
				ing.Quantity = nil
			} else if len([]rune(q)) > 60 {
				bad("ingredients", "invalid", "Keep each quantity to 60 characters.")
				break
			}
		}
	}
	return f
}

func (s *Service) checkRefs(ctx context.Context, q *store.Queries, in MealInput) error {
	if _, err := q.GetCategory(ctx, in.CategoryID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) || db.IsInvalidText(err) {
			return httpx.ValidationError([]httpx.FieldError{{Field: "category_id", Code: "invalid", Message: "Choose a category from the list."}})
		}
		return err
	}
	if in.ChefID != nil {
		if _, err := q.GetChef(ctx, *in.ChefID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) || db.IsInvalidText(err) {
				return httpx.ValidationError([]httpx.FieldError{{Field: "chef_id", Code: "invalid", Message: "Choose a chef from the list."}})
			}
			return err
		}
	}
	return nil
}

func (s *Service) writeChildren(ctx context.Context, q *store.Queries, mealID string, in MealInput) error {
	if err := q.DeleteImages(ctx, mealID); err != nil {
		return err
	}
	for i, img := range in.Images {
		if err := q.InsertImage(ctx, store.InsertImageParams{MealID: mealID, Position: int16(i), Src: img.Src, Alt: img.Alt,
			Width: img.Width, Height: img.Height, Credit: img.Credit}); err != nil {
			return err
		}
	}
	if err := q.DeleteIngredients(ctx, mealID); err != nil {
		return err
	}
	for i, ing := range in.Ingredients {
		if err := q.InsertIngredient(ctx, store.InsertIngredientParams{MealID: mealID, Position: int16(i), Name: ing.Name, Quantity: ing.Quantity}); err != nil {
			return err
		}
	}
	return nil
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// CreateMeal saves a new draft.
func (s *Service) CreateMeal(ctx context.Context, actor string, in MealInput) (*AdminMeal, error) {
	if f := s.clean(&in); len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	var out *AdminMeal
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		if err := s.checkRefs(ctx, q, in); err != nil {
			return err
		}
		m, err := q.CreateMeal(ctx, store.CreateMealParams{Slug: in.Slug, Name: in.Name, Summary: in.Summary, Description: in.Description,
			CookingOverview: in.CookingOverview, CategoryID: in.CategoryID, ChefID: in.ChefID, PrepMinutes: int16(in.PrepMinutes),
			CookMinutes: int16(in.CookMinutes), Servings: int16(in.Servings), Protein: in.Protein, DietaryTags: in.DietaryTags,
			Allergens: in.Allergens, AllergenStatus: in.AllergenStatus, IsDemo: in.IsDemo, Actor: nilIfEmpty(actor)})
		if err != nil {
			return err
		}
		if err := s.writeChildren(ctx, q, m.ID, in); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "catalog.meal_created", TargetType: "meal", TargetID: m.ID}); err != nil {
			return err
		}
		out, err = s.adminMeal(ctx, tx, m.ID)
		return err
	})
	if db.IsUniqueViolation(err, "meals_slug_key") {
		return nil, errSlugTaken
	}
	return out, err
}

// UpdateMeal replaces a meal's content. A published meal stays published, so
// changes reach customers at once; unpublish first to rework it privately.
func (s *Service) UpdateMeal(ctx context.Context, actor, id string, in MealInput) (*AdminMeal, error) {
	if f := s.clean(&in); len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	var out *AdminMeal
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		cur, err := q.GetMeal(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := s.checkRefs(ctx, q, in); err != nil {
			return err
		}
		if !sameSet(in.Allergens, cur.Allergens) || in.AllergenStatus != cur.AllergenStatus {
			if locked, err := recipeLocked(ctx, tx, id); err != nil {
				return err
			} else if locked {
				return errRecipeControlsClaims
			}
		}
		if cur.Status == "published" {
			if in.IsDemo && !s.opts.AllowDemo {
				return errDemoLocked
			}
			if len(in.Images) == 0 {
				return errNoImage
			}
		}
		m, err := q.UpdateMeal(ctx, store.UpdateMealParams{ID: id, Version: in.Version, Slug: in.Slug, Name: in.Name, Summary: in.Summary,
			Description: in.Description, CookingOverview: in.CookingOverview, CategoryID: in.CategoryID, ChefID: in.ChefID,
			PrepMinutes: int16(in.PrepMinutes), CookMinutes: int16(in.CookMinutes), Servings: int16(in.Servings), Protein: in.Protein,
			DietaryTags: in.DietaryTags, Allergens: in.Allergens, AllergenStatus: in.AllergenStatus, IsDemo: in.IsDemo, Actor: nilIfEmpty(actor)})
		if errors.Is(err, pgx.ErrNoRows) {
			return errVersionConflict
		}
		if err != nil {
			return err
		}
		if err := s.writeChildren(ctx, q, m.ID, in); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "catalog.meal_updated", TargetType: "meal", TargetID: m.ID,
			Metadata: map[string]any{"version": m.Version}}); err != nil {
			return err
		}
		out, err = s.adminMeal(ctx, tx, m.ID)
		return err
	})
	if db.IsUniqueViolation(err, "meals_slug_key") {
		return nil, errSlugTaken
	}
	return out, err
}

// SetStatus publishes or unpublishes a meal.
func (s *Service) SetStatus(ctx context.Context, actor, id, status string) (*AdminMeal, error) {
	var out *AdminMeal
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		cur, err := q.GetMeal(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		if status == "published" {
			if cur.IsDemo && !s.opts.AllowDemo {
				return errDemoLocked
			}
			images, err := q.ListImages(ctx, id)
			if err != nil {
				return err
			}
			if len(images) == 0 {
				return errNoImage
			}
		}
		if _, err := q.SetMealStatus(ctx, store.SetMealStatusParams{ID: id, Status: status, Actor: nilIfEmpty(actor)}); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "catalog.meal_" + status, TargetType: "meal", TargetID: id}); err != nil {
			return err
		}
		out, err = s.adminMeal(ctx, tx, id)
		return err
	})
	return out, err
}

type NutritionInput struct {
	Calories int      `json:"calories"`
	ProteinG float64  `json:"protein_g"`
	CarbsG   float64  `json:"carbs_g"`
	FatG     float64  `json:"fat_g"`
	FibreG   *float64 `json:"fibre_g"`
	SodiumMg *int     `json:"sodium_mg"`
	// Verified publishes the figures to customers. Only someone holding
	// nutrition.verify can call this at all.
	Verified bool `json:"verified"`
}

func (s *Service) SetNutrition(ctx context.Context, actor, id string, in NutritionInput) (*AdminMeal, error) {
	var f []httpx.FieldError
	bad := func(field, msg string) {
		f = append(f, httpx.FieldError{Field: field, Code: "out_of_range", Message: msg})
	}
	if in.Calories < 0 || in.Calories > 5000 {
		bad("calories", "Calories must be 0 to 5000.")
	}
	for name, v := range map[string]float64{"protein_g": in.ProteinG, "carbs_g": in.CarbsG, "fat_g": in.FatG} {
		if v < 0 || v > 500 {
			bad(name, "Grams must be 0 to 500.")
		}
	}
	if in.FibreG != nil && (*in.FibreG < 0 || *in.FibreG > 200) {
		bad("fibre_g", "Fibre must be 0 to 200 g.")
	}
	if in.SodiumMg != nil && (*in.SodiumMg < 0 || *in.SodiumMg > 20000) {
		bad("sodium_mg", "Sodium must be 0 to 20000 mg.")
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	params := store.UpsertNutritionParams{MealID: id, Calories: int16(in.Calories), ProteinG: round1(in.ProteinG), CarbsG: round1(in.CarbsG),
		FatG: round1(in.FatG), FibreG: in.FibreG, Status: "pending"}
	if in.SodiumMg != nil {
		n := int16(*in.SodiumMg)
		params.SodiumMg = &n
	}
	if in.Verified {
		now := s.now().UTC()
		params.Status, params.VerifiedBy, params.VerifiedAt = "verified", nilIfEmpty(actor), &now
	}
	var out *AdminMeal
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		if _, err := q.GetMeal(ctx, id); errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		} else if err != nil {
			return err
		}
		if locked, err := recipeLocked(ctx, tx, id); err != nil {
			return err
		} else if locked {
			return errRecipeControlsClaims
		}
		if _, err := q.UpsertNutrition(ctx, params); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "catalog.nutrition_" + params.Status, TargetType: "meal", TargetID: id}); err != nil {
			return err
		}
		var err error
		out, err = s.adminMeal(ctx, tx, id)
		return err
	})
	return out, err
}

func round1(v float64) float64 { return float64(int64(v*10+0.5)) / 10 }

// ---------------------------------------------------------------- categories

type AdminCategory struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Position    int16  `json:"position"`
	IsDemo      bool   `json:"is_demo"`
}

type CategoryInput struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Position    int    `json:"position"`
	IsDemo      bool   `json:"is_demo"`
}

func (s *Service) AdminCategories(ctx context.Context) ([]AdminCategory, error) {
	rows, err := store.New(s.pool).ListCategories(ctx)
	if err != nil {
		return nil, err
	}
	out := []AdminCategory{}
	for _, c := range rows {
		out = append(out, AdminCategory{ID: c.ID, Slug: c.Slug, Name: c.Name, Description: c.Description, Position: c.Position, IsDemo: c.IsDemo})
	}
	return out, nil
}

func (s *Service) CreateCategory(ctx context.Context, actor string, in CategoryInput) (*AdminCategory, error) {
	in.Name, in.Description = strings.TrimSpace(in.Name), strings.TrimSpace(in.Description)
	if in.Slug == "" {
		in.Slug = Slugify(in.Name)
	}
	var f []httpx.FieldError
	if n := len([]rune(in.Name)); n < 1 || n > 60 {
		f = append(f, httpx.FieldError{Field: "name", Code: "invalid", Message: "The name must be 1 to 60 characters."})
	}
	if !slugPattern.MatchString(in.Slug) || len(in.Slug) > 60 {
		f = append(f, httpx.FieldError{Field: "slug", Code: "invalid", Message: "Use lower-case letters, numbers and single hyphens."})
	}
	if len([]rune(in.Description)) > 300 {
		f = append(f, httpx.FieldError{Field: "description", Code: "too_long", Message: "Keep the description to 300 characters."})
	}
	if in.Position < 0 || in.Position > 1000 {
		f = append(f, httpx.FieldError{Field: "position", Code: "out_of_range", Message: "Position must be 0 to 1000."})
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	var out *AdminCategory
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		c, err := store.New(tx).CreateCategory(ctx, store.CreateCategoryParams{Slug: in.Slug, Name: in.Name, Description: in.Description,
			Position: int16(in.Position), IsDemo: in.IsDemo})
		if err != nil {
			return err
		}
		out = &AdminCategory{ID: c.ID, Slug: c.Slug, Name: c.Name, Description: c.Description, Position: c.Position, IsDemo: c.IsDemo}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "catalog.category_created", TargetType: "meal_category", TargetID: c.ID})
	})
	if db.IsUniqueViolation(err, "meal_categories_slug_key") {
		return nil, errSlugTaken
	}
	return out, err
}

// ---------------------------------------------------------------- chefs

type AdminChef struct {
	ID          string    `json:"id"`
	Slug        string    `json:"slug"`
	DisplayName string    `json:"display_name"`
	RoleTitle   *string   `json:"role_title"`
	Bio         string    `json:"bio"`
	ImageSrc    *string   `json:"image_src"`
	ImageAlt    *string   `json:"image_alt"`
	Status      string    `json:"status"`
	IsDemo      bool      `json:"is_demo"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ChefInput struct {
	Slug        string  `json:"slug"`
	DisplayName string  `json:"display_name"`
	RoleTitle   *string `json:"role_title"`
	Bio         string  `json:"bio"`
	ImageSrc    *string `json:"image_src"`
	ImageAlt    *string `json:"image_alt"`
	Status      string  `json:"status"`
	IsDemo      bool    `json:"is_demo"`
}

func chefView(c store.ChefProfile) AdminChef {
	return AdminChef{ID: c.ID, Slug: c.Slug, DisplayName: c.DisplayName, RoleTitle: c.RoleTitle, Bio: c.Bio, ImageSrc: c.ImageSrc,
		ImageAlt: c.ImageAlt, Status: c.Status, IsDemo: c.IsDemo, UpdatedAt: c.UpdatedAt}
}

func (s *Service) AdminChefs(ctx context.Context) ([]AdminChef, error) {
	rows, err := store.New(s.pool).ListChefs(ctx)
	if err != nil {
		return nil, err
	}
	out := []AdminChef{}
	for _, c := range rows {
		out = append(out, chefView(c))
	}
	return out, nil
}

func (s *Service) cleanChef(in *ChefInput) []httpx.FieldError {
	var f []httpx.FieldError
	in.DisplayName, in.Bio = strings.TrimSpace(in.DisplayName), strings.TrimSpace(in.Bio)
	if in.Slug == "" {
		in.Slug = Slugify(in.DisplayName)
	}
	if n := len([]rune(in.DisplayName)); n < 1 || n > 80 {
		f = append(f, httpx.FieldError{Field: "display_name", Code: "invalid", Message: "The name must be 1 to 80 characters."})
	}
	if !slugPattern.MatchString(in.Slug) || len(in.Slug) > 80 {
		f = append(f, httpx.FieldError{Field: "slug", Code: "invalid", Message: "Use lower-case letters, numbers and single hyphens."})
	}
	if len([]rune(in.Bio)) > 2000 {
		f = append(f, httpx.FieldError{Field: "bio", Code: "too_long", Message: "Keep the biography to 2000 characters."})
	}
	if in.RoleTitle != nil {
		t := strings.TrimSpace(*in.RoleTitle)
		in.RoleTitle = nilIfEmpty(t)
		if len([]rune(t)) > 80 {
			f = append(f, httpx.FieldError{Field: "role_title", Code: "too_long", Message: "Keep the title to 80 characters."})
		}
	}
	if in.ImageSrc != nil && strings.TrimSpace(*in.ImageSrc) == "" {
		in.ImageSrc, in.ImageAlt = nil, nil
	}
	if in.ImageSrc != nil {
		if !s.imageAllowed(*in.ImageSrc) {
			f = append(f, httpx.FieldError{Field: "image_src", Code: "invalid_src", Message: "Photographs must be in /catalog/ or on an approved image host."})
		}
		if in.ImageAlt == nil || strings.TrimSpace(*in.ImageAlt) == "" {
			f = append(f, httpx.FieldError{Field: "image_alt", Code: "required", Message: "Describe the photograph."})
		}
	}
	switch in.Status {
	case "":
		in.Status = "draft"
	case "draft", "published", "unpublished":
	default:
		f = append(f, httpx.FieldError{Field: "status", Code: "invalid", Message: "Status is draft, published or unpublished."})
	}
	if in.Status == "published" && in.IsDemo && !s.opts.AllowDemo {
		f = append(f, httpx.FieldError{Field: "status", Code: "demo_content", Message: "Demo profiles can be published only where demo content is enabled."})
	}
	return f
}

// SaveChef creates (id empty) or updates a chef profile. canPublish says
// whether the caller holds catalog.publish; without it, the status cannot
// become or stop being "published".
func (s *Service) SaveChef(ctx context.Context, actor, id string, in ChefInput, canPublish bool) (*AdminChef, error) {
	if f := s.cleanChef(&in); len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	var out *AdminChef
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		before := "draft"
		if id != "" {
			cur, err := q.GetChef(ctx, id)
			if errors.Is(err, pgx.ErrNoRows) || db.IsInvalidText(err) {
				return httpx.ErrNotFound
			}
			if err != nil {
				return err
			}
			before = cur.Status
		}
		if !canPublish && (in.Status == "published") != (before == "published") {
			return httpx.ErrForbidden
		}
		var c store.ChefProfile
		var err error
		action := "catalog.chef_updated"
		if id == "" {
			action = "catalog.chef_created"
			c, err = q.CreateChef(ctx, store.CreateChefParams{Slug: in.Slug, DisplayName: in.DisplayName, RoleTitle: in.RoleTitle, Bio: in.Bio,
				ImageSrc: in.ImageSrc, ImageAlt: in.ImageAlt, Status: in.Status, IsDemo: in.IsDemo})
		} else {
			c, err = q.UpdateChef(ctx, store.UpdateChefParams{ID: id, Slug: in.Slug, DisplayName: in.DisplayName, RoleTitle: in.RoleTitle, Bio: in.Bio,
				ImageSrc: in.ImageSrc, ImageAlt: in.ImageAlt, Status: in.Status, IsDemo: in.IsDemo})
		}
		if err != nil {
			return err
		}
		v := chefView(c)
		out = &v
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: action, TargetType: "chef_profile", TargetID: c.ID,
			Metadata: map[string]any{"status": c.Status}})
	})
	if db.IsUniqueViolation(err, "chef_profiles_slug_key") {
		return nil, errSlugTaken
	}
	return out, err
}

func uniq(in []string) []string {
	out := []string{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
