// Package catalog is the customer-facing meal catalogue: how a dish is
// presented (name, story, photographs, tags, the ingredients in the box,
// declared allergens, verified nutrition), who presents it (chef profiles) and
// how it is grouped (categories).
//
// It is deliberately not a recipe system. Production recipes, with
// quantities, yields, costing and versions, belong to the future Chef ERP,
// which will reference a meal by id. Nothing here should grow into that.
//
// Safety rules enforced here:
//   - Customers see only published meals, and demo content only when
//     CATALOG_DEMO_ENABLED is set.
//   - Nutrition is returned only once verified by someone with nutrition.verify.
//   - Allergens are reported as "contains", only when declared. Nothing says a
//     meal is free of an allergen, and cross-contact is always possible.
package catalog

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/catalog/store"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/vocab"
)

// CrossContactNotice is shown with every allergen statement and allergy filter.
const CrossContactNotice = "Mise kits are packed in a kitchen that handles all of the major allergens. " +
	"Filtering by allergy or preference does not guarantee a meal is free of an allergen or that cross-contact cannot happen. " +
	"If you have a severe allergy, check each ingredient list and contact us before you cook."

type Options struct {
	// AllowDemo shows demo content to customers and allows it to be published.
	AllowDemo bool
	// ImageHosts are the https hosts meal photographs may be served from,
	// besides the customer app's own /catalog/ folder.
	ImageHosts []string
}

type Service struct {
	pool *pgxpool.Pool
	opts Options
	now  func() time.Time
}

func NewService(pool *pgxpool.Pool, opts Options) *Service {
	return &Service{pool: pool, opts: opts, now: time.Now}
}

// ---------------------------------------------------------------- views

type Ref struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type Image struct {
	Src    string  `json:"src"`
	Alt    string  `json:"alt"`
	Width  *int32  `json:"width"`
	Height *int32  `json:"height"`
	Credit *string `json:"credit,omitempty"`
}

// AllergenInfo is what is known about a meal's allergens.
type AllergenInfo struct {
	// Status "declared": Contains lists what the meal contains.
	// "unconfirmed": nothing has been declared yet; do not rely on the meal for any allergy.
	Status   string       `json:"status"`
	Contains []vocab.Term `json:"contains"`
	Notice   string       `json:"notice"`
}

type NutritionSummary struct {
	Calories int16   `json:"calories"`
	ProteinG float64 `json:"protein_g"`
}

// MealCard is a meal in a list.
type MealCard struct {
	ID           string            `json:"id"`
	Slug         string            `json:"slug"`
	Name         string            `json:"name"`
	Summary      string            `json:"summary"`
	Category     Ref               `json:"category"`
	Chef         *Ref              `json:"chef"`
	PrepMinutes  int16             `json:"prep_minutes"`
	CookMinutes  int16             `json:"cook_minutes"`
	TotalMinutes int               `json:"total_minutes"`
	Servings     int16             `json:"servings"`
	Protein      *vocab.Term       `json:"protein"`
	Dietary      []vocab.Term      `json:"dietary"`
	Allergens    AllergenInfo      `json:"allergens"`
	Image        *Image            `json:"image"`
	Nutrition    *NutritionSummary `json:"nutrition"`
	IsDemo       bool              `json:"is_demo"`
}

func terms(keys []string, list vocab.List) []vocab.Term {
	out := []vocab.Term{}
	for _, k := range keys {
		out = append(out, vocab.Term{Key: k, Label: list.Label(k)})
	}
	return out
}

func allergenInfo(status string, keys []string) AllergenInfo {
	info := AllergenInfo{Status: status, Contains: []vocab.Term{}, Notice: CrossContactNotice}
	if status == "declared" {
		info.Contains = terms(keys, vocab.MealAllergens)
	}
	return info
}

func proteinTerm(key *string) *vocab.Term {
	if key == nil {
		return nil
	}
	return &vocab.Term{Key: *key, Label: vocab.Proteins.Label(*key)}
}

func cardFrom(r store.ListPublishedMealsRow) MealCard {
	c := MealCard{
		ID: r.ID, Slug: r.Slug, Name: r.Name, Summary: r.Summary,
		Category:    Ref{Slug: r.CategorySlug, Name: r.CategoryName},
		PrepMinutes: r.PrepMinutes, CookMinutes: r.CookMinutes, TotalMinutes: int(r.PrepMinutes) + int(r.CookMinutes),
		Servings: r.Servings, Protein: proteinTerm(r.Protein), Dietary: terms(r.DietaryTags, vocab.Dietary),
		Allergens: allergenInfo(r.AllergenStatus, r.Allergens), IsDemo: r.IsDemo,
	}
	if r.ChefSlug != nil && r.ChefName != nil {
		c.Chef = &Ref{Slug: *r.ChefSlug, Name: *r.ChefName}
	}
	if r.ImageSrc != nil && r.ImageAlt != nil {
		c.Image = &Image{Src: *r.ImageSrc, Alt: *r.ImageAlt, Width: r.ImageWidth, Height: r.ImageHeight}
	}
	if r.Calories != nil && r.ProteinG != nil {
		c.Nutrition = &NutritionSummary{Calories: *r.Calories, ProteinG: *r.ProteinG}
	}
	return c
}

// ---------------------------------------------------------------- listing

// Filter narrows the published meals. Every field is optional.
type Filter struct {
	Category         string
	Diet             []string
	MaxMinutes       int
	Protein          string
	Chef             string
	Query            string
	ExcludeAllergens []string
	IDs              []string
	Sort             string // "newest" (default), "quickest", "name"
	Page             int
	PageSize         int
}

type Page struct {
	Items    []MealCard `json:"items"`
	Total    int64      `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"page_size"`
}

const maxPageSize = 48

// dietGroup is the set of meal tags that satisfy one dietary requirement.
func dietGroup(key string) string {
	switch key {
	case "vegetarian":
		return "vegetarian|vegan"
	case "pescatarian":
		return "pescatarian|vegetarian|vegan"
	default:
		return key
	}
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func (f Filter) validate() error {
	var fields []httpx.FieldError
	bad := func(field, msg string) {
		fields = append(fields, httpx.FieldError{Field: field, Code: "invalid", Message: msg})
	}
	for _, d := range f.Diet {
		if !vocab.Dietary.Has(d) {
			bad("diet", "Unknown dietary preference.")
			break
		}
	}
	if f.Protein != "" && !vocab.Proteins.Has(f.Protein) {
		bad("protein", "Unknown protein.")
	}
	for _, a := range f.ExcludeAllergens {
		if !vocab.MealAllergens.Has(a) {
			bad("exclude_allergens", "Unknown allergen.")
			break
		}
	}
	if f.MaxMinutes < 0 || f.MaxMinutes > 480 {
		bad("max_minutes", "Choose a time between 1 and 480 minutes.")
	}
	switch f.Sort {
	case "", "newest", "quickest", "name":
	default:
		bad("sort", "Sort by newest, quickest or name.")
	}
	if len([]rune(f.Query)) > 100 {
		bad("q", "Search for up to 100 characters.")
	}
	if len(fields) > 0 {
		return httpx.ValidationError(fields)
	}
	return nil
}

func (s *Service) List(ctx context.Context, f Filter) (*Page, error) {
	if err := f.validate(); err != nil {
		return nil, err
	}
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 || f.PageSize > maxPageSize {
		f.PageSize = 24
	}
	params := store.ListPublishedMealsParams{
		AllowDemo: s.opts.AllowDemo, DietGroups: []string{}, ExcludeAllergens: []string{}, Ids: []string{},
		Sort: f.Sort, Lim: int32(f.PageSize), Off: int32((f.Page - 1) * f.PageSize),
	}
	if f.Category != "" {
		params.Category = &f.Category
	}
	for _, d := range f.Diet {
		params.DietGroups = append(params.DietGroups, dietGroup(d))
	}
	if f.MaxMinutes > 0 {
		n := int32(f.MaxMinutes)
		params.MaxMinutes = &n
	}
	if f.Protein != "" {
		params.Protein = &f.Protein
	}
	if f.Chef != "" {
		params.Chef = &f.Chef
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		like := likeEscaper.Replace(q)
		params.Search, params.SearchLike = &q, &like
	}
	if len(f.ExcludeAllergens) > 0 {
		params.ExcludeAllergens = f.ExcludeAllergens
	}
	if len(f.IDs) > 0 {
		params.Ids = f.IDs
	}
	rows, err := store.New(s.pool).ListPublishedMeals(ctx, params)
	if err != nil {
		return nil, err
	}
	out := &Page{Items: []MealCard{}, Page: f.Page, PageSize: f.PageSize}
	for _, r := range rows {
		out.Items = append(out.Items, cardFrom(r))
		out.Total = r.Total
	}
	if len(rows) == 0 && f.Page > 1 {
		// Past the end: report the total so the client can show the right pages.
		params.Off, params.Lim = 0, 1
		if first, err := store.New(s.pool).ListPublishedMeals(ctx, params); err == nil && len(first) > 0 {
			out.Total = first[0].Total
		}
	}
	return out, nil
}

// Cards returns the visible meals among ids, in the order given. Hidden or
// unknown ids are left out.
func (s *Service) Cards(ctx context.Context, ids []string) ([]MealCard, error) {
	if len(ids) == 0 {
		return []MealCard{}, nil
	}
	page, err := s.List(ctx, Filter{IDs: ids, PageSize: maxPageSize})
	if err != nil {
		return nil, err
	}
	byID := map[string]MealCard{}
	for _, c := range page.Items {
		byID[c.ID] = c
	}
	out := []MealCard{}
	for _, id := range ids {
		if c, ok := byID[id]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}

// Visible reports whether a customer may see a meal (published, and not demo
// unless demo content is enabled).
func (s *Service) Visible(ctx context.Context, mealID string) (bool, error) {
	return store.New(s.pool).IsMealVisible(ctx, store.IsMealVisibleParams{ID: mealID, AllowDemo: s.opts.AllowDemo})
}

// ---------------------------------------------------------------- detail

type Ingredient struct {
	Name     string  `json:"name"`
	Quantity *string `json:"quantity"`
}

type Nutrition struct {
	Calories   int16      `json:"calories"`
	ProteinG   float64    `json:"protein_g"`
	CarbsG     float64    `json:"carbs_g"`
	FatG       float64    `json:"fat_g"`
	FibreG     *float64   `json:"fibre_g"`
	SodiumMg   *int16     `json:"sodium_mg"`
	VerifiedAt *time.Time `json:"verified_at"`
}

type ChefSummary struct {
	Slug      string  `json:"slug"`
	Name      string  `json:"name"`
	RoleTitle *string `json:"role_title"`
	Bio       string  `json:"bio"`
	Image     *Image  `json:"image"`
}

type MealDetail struct {
	MealCard
	Description     string       `json:"description"`
	CookingOverview string       `json:"cooking_overview"`
	Images          []Image      `json:"images"`
	Ingredients     []Ingredient `json:"ingredients"`
	NutritionFull   *Nutrition   `json:"nutrition_detail"`
	ChefDetail      *ChefSummary `json:"chef_detail"`
	PublishedAt     *time.Time   `json:"published_at"`
}

func (s *Service) Detail(ctx context.Context, slug string) (*MealDetail, error) {
	q := store.New(s.pool)
	m, err := q.GetPublishedMealBySlug(ctx, store.GetPublishedMealBySlugParams{Slug: slug, AllowDemo: s.opts.AllowDemo})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	d := &MealDetail{
		MealCard: MealCard{
			ID: m.ID, Slug: m.Slug, Name: m.Name, Summary: m.Summary,
			Category:    Ref{Slug: m.CategorySlug, Name: m.CategoryName},
			PrepMinutes: m.PrepMinutes, CookMinutes: m.CookMinutes, TotalMinutes: int(m.PrepMinutes) + int(m.CookMinutes),
			Servings: m.Servings, Protein: proteinTerm(m.Protein), Dietary: terms(m.DietaryTags, vocab.Dietary),
			Allergens: allergenInfo(m.AllergenStatus, m.Allergens), IsDemo: m.IsDemo,
		},
		Description: m.Description, CookingOverview: m.CookingOverview, PublishedAt: m.PublishedAt,
		Images: []Image{}, Ingredients: []Ingredient{},
	}
	if m.ChefSlug != nil && m.ChefName != nil {
		d.Chef = &Ref{Slug: *m.ChefSlug, Name: *m.ChefName}
		d.ChefDetail = &ChefSummary{Slug: *m.ChefSlug, Name: *m.ChefName, RoleTitle: m.ChefRoleTitle, Bio: derefString(m.ChefBio)}
		if m.ChefImageSrc != nil && m.ChefImageAlt != nil {
			d.ChefDetail.Image = &Image{Src: *m.ChefImageSrc, Alt: *m.ChefImageAlt}
		}
	}
	images, err := q.ListImages(ctx, m.ID)
	if err != nil {
		return nil, err
	}
	for _, img := range images {
		d.Images = append(d.Images, Image{Src: img.Src, Alt: img.Alt, Width: img.Width, Height: img.Height, Credit: img.Credit})
	}
	if len(d.Images) > 0 {
		first := d.Images[0]
		d.Image = &first
	}
	ingredients, err := q.ListIngredients(ctx, m.ID)
	if err != nil {
		return nil, err
	}
	for _, in := range ingredients {
		d.Ingredients = append(d.Ingredients, Ingredient{Name: in.Name, Quantity: in.Quantity})
	}
	n, err := q.GetVerifiedNutrition(ctx, m.ID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, err
	default:
		d.NutritionFull = &Nutrition{Calories: n.Calories, ProteinG: n.ProteinG, CarbsG: n.CarbsG, FatG: n.FatG,
			FibreG: n.FibreG, SodiumMg: n.SodiumMg, VerifiedAt: n.VerifiedAt}
		d.Nutrition = &NutritionSummary{Calories: n.Calories, ProteinG: n.ProteinG}
	}
	return d, nil
}

// ---------------------------------------------------------------- categories and chefs

type Category struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MealCount   int64  `json:"meal_count"`
}

func (s *Service) Categories(ctx context.Context) ([]Category, error) {
	rows, err := store.New(s.pool).ListCategoriesWithCounts(ctx, s.opts.AllowDemo)
	if err != nil {
		return nil, err
	}
	out := []Category{}
	for _, r := range rows {
		out = append(out, Category{Slug: r.Slug, Name: r.Name, Description: r.Description, MealCount: r.MealCount})
	}
	return out, nil
}

func (s *Service) Chefs(ctx context.Context) ([]Ref, error) {
	rows, err := store.New(s.pool).ListPublishedChefs(ctx, s.opts.AllowDemo)
	if err != nil {
		return nil, err
	}
	out := []Ref{}
	for _, r := range rows {
		out = append(out, Ref{Slug: r.Slug, Name: r.DisplayName})
	}
	return out, nil
}

type ChefProfile struct {
	ChefSummary
	IsDemo bool       `json:"is_demo"`
	Meals  []MealCard `json:"meals"`
}

func (s *Service) Chef(ctx context.Context, slug string) (*ChefProfile, error) {
	c, err := store.New(s.pool).GetPublishedChefBySlug(ctx, store.GetPublishedChefBySlugParams{Slug: slug, AllowDemo: s.opts.AllowDemo})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	out := &ChefProfile{ChefSummary: ChefSummary{Slug: c.Slug, Name: c.DisplayName, RoleTitle: c.RoleTitle, Bio: c.Bio}, IsDemo: c.IsDemo}
	if c.ImageSrc != nil && c.ImageAlt != nil {
		out.Image = &Image{Src: *c.ImageSrc, Alt: *c.ImageAlt}
	}
	page, err := s.List(ctx, Filter{Chef: c.Slug, PageSize: 12})
	if err != nil {
		return nil, err
	}
	out.Meals = page.Items
	return out, nil
}

// ---------------------------------------------------------------- helpers

var slugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify makes a URL slug from a name: "Curry chicken & roti" -> "curry-chicken-roti".
func Slugify(name string) string {
	s := strings.Trim(slugUnsafe.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > 80 {
		s = strings.TrimRight(s[:80], "-")
	}
	return s
}

var (
	slugPattern     = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	localImageRegex = regexp.MustCompile(`^/catalog/[a-z0-9][a-z0-9\-/]*\.(jpg|jpeg|png|webp|avif)$`)
)

// imageAllowed accepts a file in the customer app's /catalog/ folder, or an
// https URL on a configured host.
func (s *Service) imageAllowed(src string) bool {
	if localImageRegex.MatchString(src) && !strings.Contains(src, "..") {
		return true
	}
	u, err := url.Parse(src)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return false
	}
	for _, h := range s.opts.ImageHosts {
		if strings.EqualFold(u.Host, h) {
			return true
		}
	}
	return false
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
