// Package plans owns subscription plans and their price versions.
//
// Prices are immutable rows with an effective date. A subscription records
// the price row it was sold at, so publishing a new price never changes what
// an existing subscriber pays for a week already created. Nothing here hard-
// codes a price: the business-plan figures exist only as demo content
// (misectl commerce seed-demo), hidden outside local development.
package plans

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/plans/store"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/money"
	"mise.tt/api/internal/vocab"
)

// Segments describe who a plan is meant for. Descriptive only: which
// customers may buy which plan is an open business decision, so the only
// purchase rule is self_serve.
var Segments = vocab.List{
	{Key: "everyday", Label: "Everyday cooking"},
	{Key: "fitness", Label: "Active lifestyles"},
	{Key: "family", Label: "Families"},
	{Key: "school", Label: "School lunches"},
	{Key: "premium", Label: "Food lovers"},
	{Key: "corporate", Label: "Workplaces"},
}

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

type Options struct {
	// AllowDemo shows demo plans to customers and lets them be bought.
	AllowDemo bool
}

type Service struct {
	pool *pgxpool.Pool
	opts Options
	Now  func() time.Time
}

func NewService(pool *pgxpool.Pool, opts Options) *Service {
	return &Service{pool: pool, opts: opts, Now: time.Now}
}

// Price is one price version.
type Price struct {
	ID            string    `json:"id"`
	AmountMinor   int64     `json:"amount_minor"`
	Currency      string    `json:"currency"`
	EffectiveFrom time.Time `json:"effective_from"`
	Formatted     string    `json:"formatted"`
}

// Plan is a plan as customers see it.
type Plan struct {
	ID              string     `json:"id"`
	Slug            string     `json:"slug"`
	Name            string     `json:"name"`
	Summary         string     `json:"summary"`
	Description     string     `json:"description"`
	MealsPerWeek    int        `json:"meals_per_week"`
	BillingInterval string     `json:"billing_interval"`
	Segment         vocab.Term `json:"segment"`
	Benefits        []string   `json:"benefits"`
	SelfServe       bool       `json:"self_serve"`
	Price           *Price     `json:"price"`
	PerMealMinor    int64      `json:"per_meal_minor"`
	NextPrice       *Price     `json:"next_price"`
	Purchasable     bool       `json:"purchasable"`
	// Why it cannot be bought online, when it cannot.
	Unavailable string `json:"unavailable_reason,omitempty"`
	IsDemo      bool   `json:"is_demo"`
}

// AdminPlan adds what staff need.
type AdminPlan struct {
	Plan
	Status         string     `json:"status"`
	AvailableFrom  *time.Time `json:"available_from"`
	AvailableUntil *time.Time `json:"available_until"`
	SortOrder      int        `json:"sort_order"`
	Version        int        `json:"version"`
	Prices         []Price    `json:"prices,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func priceOf(id string, amount int64, currency string, from time.Time) *Price {
	if amount <= 0 {
		return nil
	}
	return &Price{ID: id, AmountMinor: amount, Currency: currency, EffectiveFrom: from, Formatted: money.Format(amount, currency)}
}

func (s *Service) view(r store.ListPlansWithPriceRow, now time.Time) AdminPlan {
	p := AdminPlan{
		Plan: Plan{
			ID: r.ID, Slug: r.Slug, Name: r.Name, Summary: r.Summary, Description: r.Description,
			MealsPerWeek: int(r.MealsPerWeek), BillingInterval: r.BillingInterval,
			Segment:  vocab.Term{Key: r.Segment, Label: Segments.Label(r.Segment)},
			Benefits: r.Benefits, SelfServe: r.SelfServe, IsDemo: r.IsDemo,
			Price:     priceOf(r.PriceID, r.PriceAmountMinor, r.PriceCurrency, r.PriceEffectiveFrom),
			NextPrice: priceOf("", r.NextAmountMinor, r.PriceCurrency, r.NextEffectiveFrom),
		},
		Status: r.Status, AvailableFrom: r.AvailableFrom, AvailableUntil: r.AvailableUntil,
		SortOrder: int(r.SortOrder), Version: int(r.Version), UpdatedAt: r.UpdatedAt,
	}
	if p.NextPrice != nil && p.NextPrice.Currency == "" {
		p.NextPrice.Currency = money.Currency
		p.NextPrice.Formatted = money.Format(p.NextPrice.AmountMinor, money.Currency)
	}
	if p.Price != nil {
		p.PerMealMinor = money.Divide(p.Price.AmountMinor, int64(p.MealsPerWeek))
	}
	p.Unavailable = s.unavailable(r, now)
	p.Purchasable = p.Unavailable == ""
	return p
}

// unavailable explains why a plan cannot be bought now, or "" if it can.
func (s *Service) unavailable(r store.ListPlansWithPriceRow, now time.Time) string {
	switch {
	case r.Status != "active":
		return "This plan is not available."
	case r.IsDemo && !s.opts.AllowDemo:
		return "This plan is not available."
	case r.AvailableFrom != nil && now.Before(*r.AvailableFrom):
		return "This plan opens on " + r.AvailableFrom.In(tt).Format("2 January 2006") + "."
	case r.AvailableUntil != nil && !now.Before(*r.AvailableUntil):
		return "This plan is no longer available."
	case r.PriceAmountMinor <= 0:
		return "This plan has no price yet."
	case !r.SelfServe:
		return "This plan is arranged with our team. Contact us to join."
	}
	return ""
}

var tt = mustLoad("America/Port_of_Spain")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.FixedZone("AST", -4*3600)
	}
	return loc
}

// visible: what customers may see listed (active, and demo only where allowed,
// and not past its availability window).
func (s *Service) visible(r store.ListPlansWithPriceRow, now time.Time) bool {
	if r.Status != "active" || (r.IsDemo && !s.opts.AllowDemo) {
		return false
	}
	return r.AvailableUntil == nil || now.Before(*r.AvailableUntil)
}

// Public lists the plans customers can see.
func (s *Service) Public(ctx context.Context) ([]Plan, error) {
	now := s.Now()
	active := "active"
	rows, err := store.New(s.pool).ListPlansWithPrice(ctx, store.ListPlansWithPriceParams{Now: now, Status: &active})
	if err != nil {
		return nil, err
	}
	out := []Plan{}
	for _, r := range rows {
		if s.visible(r, now) {
			out = append(out, s.view(r, now).Plan)
		}
	}
	return out, nil
}

func (s *Service) get(ctx context.Context, q db.Querier, id, slug string) (*store.ListPlansWithPriceRow, error) {
	params := store.GetPlanWithPriceParams{Now: s.Now()}
	if id != "" && !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	if id != "" {
		params.ID = &id
	} else {
		params.Slug = &slug
	}
	r, err := store.New(q).GetPlanWithPrice(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	row := store.ListPlansWithPriceRow(r)
	return &row, nil
}

// PublicBySlug is one plan as customers see it; hidden plans are not found.
func (s *Service) PublicBySlug(ctx context.Context, slug string) (*Plan, error) {
	if !slugPattern.MatchString(slug) {
		return nil, httpx.ErrNotFound
	}
	r, err := s.get(ctx, s.pool, "", slug)
	if err != nil {
		return nil, err
	}
	now := s.Now()
	if !s.visible(*r, now) {
		return nil, httpx.ErrNotFound
	}
	p := s.view(*r, now).Plan
	return &p, nil
}

// Selling is what a subscription is sold at: the plan and the exact price row.
type Selling struct {
	PlanID       string
	PlanName     string
	PriceID      string
	MealsPerWeek int
	AmountMinor  int64
	Currency     string
}

// ErrNotPurchasable is returned when a plan cannot be bought online now.
func notPurchasable(reason string) error {
	return httpx.NewError(http.StatusConflict, "plan_unavailable", reason)
}

// ForPurchase returns what plan id sells at now, or an error saying why it
// cannot be bought. q may be a transaction.
func (s *Service) ForPurchase(ctx context.Context, q db.Querier, planID string) (*Selling, error) {
	r, err := s.get(ctx, q, planID, "")
	if err != nil {
		return nil, err
	}
	now := s.Now()
	if !s.visible(*r, now) {
		return nil, httpx.ErrNotFound
	}
	if reason := s.unavailable(*r, now); reason != "" {
		return nil, notPurchasable(reason)
	}
	return &Selling{PlanID: r.ID, PlanName: r.Name, PriceID: r.PriceID, MealsPerWeek: int(r.MealsPerWeek),
		AmountMinor: r.PriceAmountMinor, Currency: r.PriceCurrency}, nil
}

// NameOf returns a plan's name (for invoices and orders).
func (s *Service) NameOf(ctx context.Context, q db.Querier, planID string) (string, error) {
	r, err := s.get(ctx, q, planID, "")
	if err != nil {
		return "", err
	}
	return r.Name, nil
}

// ---------------------------------------------------------------- admin

func (s *Service) AdminList(ctx context.Context) ([]AdminPlan, error) {
	now := s.Now()
	rows, err := store.New(s.pool).ListPlansWithPrice(ctx, store.ListPlansWithPriceParams{Now: now})
	if err != nil {
		return nil, err
	}
	out := []AdminPlan{}
	for _, r := range rows {
		out = append(out, s.view(r, now))
	}
	return out, nil
}

func (s *Service) AdminGet(ctx context.Context, id string) (*AdminPlan, error) {
	r, err := s.get(ctx, s.pool, id, "")
	if err != nil {
		return nil, err
	}
	p := s.view(*r, s.Now())
	prices, err := store.New(s.pool).ListPrices(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, pr := range prices {
		p.Prices = append(p.Prices, *priceOf(pr.ID, pr.AmountMinor, pr.Currency, pr.EffectiveFrom))
	}
	return &p, nil
}

// PlanInput is the editable part of a plan.
type PlanInput struct {
	Slug           string     `json:"slug"`
	Name           string     `json:"name"`
	Summary        string     `json:"summary"`
	Description    string     `json:"description"`
	MealsPerWeek   int        `json:"meals_per_week"`
	Segment        string     `json:"segment"`
	Benefits       []string   `json:"benefits"`
	SelfServe      bool       `json:"self_serve"`
	AvailableFrom  *time.Time `json:"available_from"`
	AvailableUntil *time.Time `json:"available_until"`
	SortOrder      int        `json:"sort_order"`
	Version        int        `json:"version"`
}

func (in *PlanInput) clean() []httpx.FieldError {
	var f []httpx.FieldError
	add := func(field, code, msg string) { f = append(f, httpx.FieldError{Field: field, Code: code, Message: msg}) }
	in.Name = strings.TrimSpace(in.Name)
	in.Summary = strings.TrimSpace(in.Summary)
	in.Description = strings.TrimSpace(in.Description)
	in.Slug = strings.TrimSpace(strings.ToLower(in.Slug))
	if in.Slug == "" {
		in.Slug = slugify(in.Name)
	}
	if n := len([]rune(in.Name)); n < 1 || n > 60 {
		add("name", "length", "Give the plan a name of up to 60 characters.")
	}
	if n := len([]rune(in.Summary)); n < 1 || n > 200 {
		add("summary", "length", "Write a summary of up to 200 characters.")
	}
	if len([]rune(in.Description)) > 2000 {
		add("description", "length", "Keep the description under 2,000 characters.")
	}
	if !slugPattern.MatchString(in.Slug) || len(in.Slug) > 60 {
		add("slug", "format", "Use lowercase letters, numbers and hyphens.")
	}
	if in.MealsPerWeek < 1 || in.MealsPerWeek > 14 {
		add("meals_per_week", "range", "Meals per week must be between 1 and 14.")
	}
	if !Segments.Has(in.Segment) {
		add("segment", "invalid", "Choose who the plan is for.")
	}
	var benefits []string
	for _, b := range in.Benefits {
		if b = strings.TrimSpace(b); b != "" {
			benefits = append(benefits, b)
		}
	}
	in.Benefits = benefits
	if in.Benefits == nil {
		in.Benefits = []string{}
	}
	if len(in.Benefits) > 8 {
		add("benefits", "count", "List at most 8 benefits.")
	}
	for _, b := range in.Benefits {
		if len([]rune(b)) > 120 {
			add("benefits", "length", "Keep each benefit under 120 characters.")
			break
		}
	}
	if in.AvailableFrom != nil && in.AvailableUntil != nil && !in.AvailableUntil.After(*in.AvailableFrom) {
		add("available_until", "order", "The end must be after the start.")
	}
	return f
}

func slugify(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func (s *Service) CreatePlan(ctx context.Context, actor string, in PlanInput, isDemo bool) (*AdminPlan, error) {
	if f := in.clean(); len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	var id string
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		id, err = store.New(tx).CreatePlan(ctx, store.CreatePlanParams{
			Slug: in.Slug, Name: in.Name, Summary: in.Summary, Description: in.Description,
			MealsPerWeek: int16(in.MealsPerWeek), Segment: in.Segment, Benefits: in.Benefits, SelfServe: in.SelfServe,
			AvailableFrom: in.AvailableFrom, AvailableUntil: in.AvailableUntil, SortOrder: int16(in.SortOrder),
			IsDemo: isDemo, Actor: nilIfEmpty(actor),
		})
		if db.IsUniqueViolation(err, "subscription_plans_slug_key") {
			return httpx.ValidationError([]httpx.FieldError{{Field: "slug", Code: "taken", Message: "Another plan uses this web address."}})
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "plan.created", TargetType: "subscription_plan", TargetID: id})
	})
	if err != nil {
		return nil, err
	}
	return s.AdminGet(ctx, id)
}

var errConflict = httpx.NewError(http.StatusConflict, "version_conflict", "Someone else changed this plan. Reload it and try again.")

func (s *Service) UpdatePlan(ctx context.Context, actor, id string, in PlanInput) (*AdminPlan, error) {
	if f := in.clean(); len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		n, err := store.New(tx).UpdatePlan(ctx, store.UpdatePlanParams{
			ID: id, Version: int32(in.Version), Name: in.Name, Summary: in.Summary, Description: in.Description,
			MealsPerWeek: int16(in.MealsPerWeek), Segment: in.Segment, Benefits: in.Benefits, SelfServe: in.SelfServe,
			AvailableFrom: in.AvailableFrom, AvailableUntil: in.AvailableUntil, SortOrder: int16(in.SortOrder),
			Actor: nilIfEmpty(actor),
		})
		if err != nil {
			return err
		}
		if n == 0 {
			if _, err := s.get(ctx, tx, id, ""); err != nil {
				return err
			}
			return errConflict
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "plan.updated", TargetType: "subscription_plan", TargetID: id})
	})
	if err != nil {
		return nil, err
	}
	return s.AdminGet(ctx, id)
}

// PriceInput is a new price version.
type PriceInput struct {
	AmountMinor   int64      `json:"amount_minor"`
	EffectiveFrom *time.Time `json:"effective_from"`
	Note          string     `json:"note"`
}

// AddPrice records a new price version. It can start now or later, never in
// the past: past prices are history.
func (s *Service) AddPrice(ctx context.Context, actor, planID string, in PriceInput) (*AdminPlan, error) {
	now := s.Now()
	var f []httpx.FieldError
	if in.AmountMinor <= 0 || in.AmountMinor >= 100_000_000 {
		f = append(f, httpx.FieldError{Field: "amount_minor", Code: "range", Message: "Enter a price above zero."})
	}
	from := now
	if in.EffectiveFrom != nil {
		if in.EffectiveFrom.Before(now.Add(-time.Minute)) {
			f = append(f, httpx.FieldError{Field: "effective_from", Code: "past", Message: "A new price cannot start in the past."})
		} else if in.EffectiveFrom.After(now) {
			from = *in.EffectiveFrom
		}
	}
	note := strings.TrimSpace(in.Note)
	if len([]rune(note)) > 200 {
		f = append(f, httpx.FieldError{Field: "note", Code: "length", Message: "Keep the note under 200 characters."})
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.get(ctx, tx, planID, ""); err != nil {
			return err
		}
		pr, err := store.New(tx).InsertPrice(ctx, store.InsertPriceParams{
			PlanID: planID, AmountMinor: in.AmountMinor, Currency: money.Currency, EffectiveFrom: from,
			Note: nilIfEmpty(note), Actor: nilIfEmpty(actor),
		})
		if db.IsUniqueViolation(err, "subscription_plan_prices_effective_key") {
			return httpx.ValidationError([]httpx.FieldError{{Field: "effective_from", Code: "taken", Message: "A price already starts at that moment."}})
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "plan.price_added", TargetType: "subscription_plan", TargetID: planID,
			Metadata: map[string]any{"price_id": pr.ID, "amount_minor": pr.AmountMinor, "currency": pr.Currency, "effective_from": pr.EffectiveFrom}})
	})
	if err != nil {
		return nil, err
	}
	return s.AdminGet(ctx, planID)
}

// SetStatus activates or retires a plan. Retiring stops new sales; existing
// subscriptions keep their plan.
func (s *Service) SetStatus(ctx context.Context, actor, id, status string) (*AdminPlan, error) {
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		r, err := s.get(ctx, tx, id, "")
		if err != nil {
			return err
		}
		if status == "active" {
			if r.PriceAmountMinor <= 0 && r.NextAmountMinor <= 0 {
				return httpx.NewError(http.StatusConflict, "no_price", "Add a price before activating the plan.")
			}
			if r.IsDemo && !s.opts.AllowDemo {
				return httpx.NewError(http.StatusConflict, "demo_content", "Demo plans cannot be activated here.")
			}
		}
		if _, err := store.New(tx).SetPlanStatus(ctx, store.SetPlanStatusParams{ID: id, Status: status, Actor: nilIfEmpty(actor)}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "plan." + status, TargetType: "subscription_plan", TargetID: id,
			Metadata: map[string]any{"from": r.Status}})
	})
	if err != nil {
		return nil, err
	}
	return s.AdminGet(ctx, id)
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// PublicByID is one plan as customers see it, by id.
func (s *Service) PublicByID(ctx context.Context, id string) (*Plan, error) {
	r, err := s.get(ctx, s.pool, id, "")
	if err != nil {
		return nil, err
	}
	now := s.Now()
	if !s.visible(*r, now) {
		return nil, httpx.ErrNotFound
	}
	p := s.view(*r, now).Plan
	return &p, nil
}
