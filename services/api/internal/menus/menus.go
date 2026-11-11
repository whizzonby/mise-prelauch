// Package menus owns weekly menus: which meals customers may choose for a
// delivery week, and when choosing closes. Staff create and publish menus;
// customers see only published (and, after the cutoff, closed) menus.
//
// A menu's cutoff is stored on the menu (UTC) and defaults to the policy in
// week.go. The cutoff is enforced by the server when selections are saved
// (internal/subscriptions), never by a browser timer.
package menus

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/catalog"
	"mise.tt/api/internal/menus/store"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
)

// Usage tells the menu module which of a menu's meals customers have
// chosen. The subscriptions module owns selections and implements it.
type Usage interface {
	SelectedMeals(ctx context.Context, q db.Querier, menuID string) ([]string, error)
}

// Hooks hears about menu changes, in the transaction that made them.
type Hooks interface {
	MenuPublished(ctx context.Context, q db.Querier, menuID string, week time.Time) error
}

type Service struct {
	pool    *pgxpool.Pool
	catalog *catalog.Service
	Policy  Policy
	Usage   Usage
	Hooks   Hooks
	Now     func() time.Time
}

func NewService(pool *pgxpool.Pool, cat *catalog.Service, policy Policy) *Service {
	return &Service{pool: pool, catalog: cat, Policy: policy, Now: time.Now}
}

// ---------------------------------------------------------------- views

// Summary is a menu in a list.
type Summary struct {
	ID        string    `json:"id"`
	Week      string    `json:"week"`
	WeekLabel string    `json:"week_label"`
	Title     *string   `json:"title"`
	Status    string    `json:"status"`
	CutoffAt  time.Time `json:"cutoff_at"`
	IsOpen    bool      `json:"is_open"`
	MealCount int       `json:"meal_count"`
}

// MenuMeal is one meal on a menu as customers see it.
type MenuMeal struct {
	catalog.MealCard
	MaxPerCustomer *int `json:"max_per_customer"`
}

// Menu is a published menu with its meals.
type Menu struct {
	Summary
	Note            *string    `json:"note"`
	TimeZone        string     `json:"time_zone"`
	PublishedAt     *time.Time `json:"published_at"`
	EligiblePlanIDs []string   `json:"eligible_plan_ids"`
	Meals           []MenuMeal `json:"meals"`
}

func (s *Service) summary(m store.WeeklyMenu, mealCount int) Summary {
	return Summary{
		ID: m.ID, Week: FormatWeek(m.WeekStart), WeekLabel: WeekLabel(m.WeekStart), Title: m.Title, Status: m.Status,
		CutoffAt: m.CutoffAt, IsOpen: m.Status == "published" && s.Now().Before(m.CutoffAt), MealCount: mealCount,
	}
}

// Published lists the menus customers can see, from the current week on.
func (s *Service) Published(ctx context.Context) ([]Summary, error) {
	rows, err := store.New(s.pool).ListPublishedMenus(ctx, s.Policy.WeekOf(s.Now()))
	if err != nil {
		return nil, err
	}
	out := []Summary{}
	for _, m := range rows {
		meals, err := store.New(s.pool).MenuMeals(ctx, m.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, s.summary(m, len(meals)))
	}
	return out, nil
}

// PublishedWeek is the menu for a week as customers see it. A menu that is
// not published (or closed) is not found.
func (s *Service) PublishedWeek(ctx context.Context, week time.Time) (*Menu, error) {
	m, err := store.New(s.pool).GetMenuByWeek(ctx, week)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if m.Status != "published" && m.Status != "closed" {
		return nil, httpx.ErrNotFound
	}
	return s.customerMenu(ctx, m)
}

func (s *Service) customerMenu(ctx context.Context, m store.WeeklyMenu) (*Menu, error) {
	rows, err := store.New(s.pool).MenuMeals(ctx, m.ID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(rows))
	caps := map[string]*int{}
	for i, r := range rows {
		ids[i] = r.MealID
		if r.MaxPerCustomer != nil {
			v := int(*r.MaxPerCustomer)
			caps[r.MealID] = &v
		}
	}
	cards, err := s.catalog.Cards(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := &Menu{
		Summary: s.summary(m, len(cards)), Note: m.Note, TimeZone: m.TimeZone, PublishedAt: m.PublishedAt,
		EligiblePlanIDs: m.EligiblePlanIds, Meals: []MenuMeal{},
	}
	for _, c := range cards {
		out.Meals = append(out.Meals, MenuMeal{MealCard: c, MaxPerCustomer: caps[c.ID]})
	}
	return out, nil
}

// Info is what other modules need to check a choice against a menu.
type Info struct {
	ID              string
	Week            time.Time
	Status          string
	CutoffAt        time.Time
	EligiblePlanIDs []string
	// Meals on the menu, with the optional cap per customer (0 = none).
	Meals map[string]int
}

// Published reports whether customers may see and choose from the menu.
func (i *Info) Published() bool { return i.Status == "published" }

// AllowsPlan reports whether customers on planID may choose from it.
func (i *Info) AllowsPlan(planID string) bool {
	return len(i.EligiblePlanIDs) == 0 || slices.Contains(i.EligiblePlanIDs, planID)
}

// ForWeek returns the menu for a week, whatever its status, or nil if there
// is none. q may be a transaction.
func (s *Service) ForWeek(ctx context.Context, q db.Querier, week time.Time) (*Info, error) {
	m, err := store.New(q).GetMenuByWeek(ctx, week)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := store.New(q).MenuMeals(ctx, m.ID)
	if err != nil {
		return nil, err
	}
	info := &Info{ID: m.ID, Week: m.WeekStart, Status: m.Status, CutoffAt: m.CutoffAt, EligiblePlanIDs: m.EligiblePlanIds, Meals: map[string]int{}}
	for _, r := range rows {
		limit := 0
		if r.MaxPerCustomer != nil {
			limit = int(*r.MaxPerCustomer)
		}
		info.Meals[r.MealID] = limit
	}
	return info, nil
}

// ---------------------------------------------------------------- admin

// AdminMeal is a meal on a menu as staff see it.
type AdminMeal struct {
	MealID         string `json:"meal_id"`
	Name           string `json:"name"`
	Slug           string `json:"slug"`
	Status         string `json:"status"`
	Visible        bool   `json:"visible"`
	Selected       bool   `json:"selected"`
	MaxPerCustomer *int   `json:"max_per_customer"`
}

type AdminMenu struct {
	Summary
	Note            *string     `json:"note"`
	TimeZone        string      `json:"time_zone"`
	PublishAt       *time.Time  `json:"publish_at"`
	PublishedAt     *time.Time  `json:"published_at"`
	EligiblePlanIDs []string    `json:"eligible_plan_ids"`
	Version         int         `json:"version"`
	Meals           []AdminMeal `json:"meals"`
	DefaultCutoff   time.Time   `json:"default_cutoff_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
}

func (s *Service) AdminList(ctx context.Context, status string) ([]Summary, error) {
	params := store.ListMenusParams{}
	if status != "" {
		params.Status = &status
	}
	rows, err := store.New(s.pool).ListMenus(ctx, params)
	if err != nil {
		return nil, err
	}
	out := []Summary{}
	for _, r := range rows {
		out = append(out, s.summary(store.WeeklyMenu{
			ID: r.ID, WeekStart: r.WeekStart, Title: r.Title, Status: r.Status, CutoffAt: r.CutoffAt,
		}, int(r.MealCount)))
	}
	return out, nil
}

func (s *Service) AdminGet(ctx context.Context, id string) (*AdminMenu, error) {
	return s.adminMenu(ctx, s.pool, id)
}

func (s *Service) adminMenu(ctx context.Context, q db.Querier, id string) (*AdminMenu, error) {
	m, err := store.New(q).GetMenu(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := store.New(q).MenuMeals(ctx, id)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.MealID
	}
	refs, err := s.catalog.MealRefs(ctx, q, ids)
	if err != nil {
		return nil, err
	}
	var selected []string
	if s.Usage != nil {
		if selected, err = s.Usage.SelectedMeals(ctx, q, id); err != nil {
			return nil, err
		}
	}
	out := &AdminMenu{
		Summary: s.summary(m, len(rows)), Note: m.Note, TimeZone: m.TimeZone, PublishAt: m.PublishAt,
		PublishedAt: m.PublishedAt, EligiblePlanIDs: m.EligiblePlanIds, Version: int(m.Version), Meals: []AdminMeal{},
		DefaultCutoff: s.Policy.CutoffFor(m.WeekStart), UpdatedAt: m.UpdatedAt,
	}
	for _, r := range rows {
		ref := refs[r.MealID]
		am := AdminMeal{MealID: r.MealID, Name: ref.Name, Slug: ref.Slug, Status: ref.Status, Visible: ref.Visible,
			Selected: slices.Contains(selected, r.MealID)}
		if r.MaxPerCustomer != nil {
			v := int(*r.MaxPerCustomer)
			am.MaxPerCustomer = &v
		}
		out.Meals = append(out.Meals, am)
	}
	return out, nil
}

// MealInput is one meal on a menu.
type MealInput struct {
	MealID         string `json:"meal_id"`
	MaxPerCustomer *int   `json:"max_per_customer"`
}

// Input is a menu as staff edit it.
type Input struct {
	Week            string      `json:"week"`
	Title           string      `json:"title"`
	Note            string      `json:"note"`
	CutoffAt        *time.Time  `json:"cutoff_at"`
	EligiblePlanIDs []string    `json:"eligible_plan_ids"`
	Meals           []MealInput `json:"meals"`
	Version         int         `json:"version"`
}

type cleaned struct {
	week    time.Time
	title   *string
	note    *string
	cutoff  time.Time
	planIDs []string
	meals   []MealInput
	mealIDs []string
}

func (s *Service) clean(ctx context.Context, q db.Querier, in Input, week *time.Time) (*cleaned, error) {
	var f []httpx.FieldError
	add := func(field, code, msg string) { f = append(f, httpx.FieldError{Field: field, Code: code, Message: msg}) }
	c := &cleaned{planIDs: []string{}}
	if week != nil {
		c.week = *week
	} else if w, ok := ParseWeek(strings.TrimSpace(in.Week)); ok {
		c.week = w
	} else {
		add("week", "format", "Choose the Monday the delivery week starts on.")
	}
	if t := strings.TrimSpace(in.Title); t != "" {
		if len([]rune(t)) > 80 {
			add("title", "length", "Keep the title under 80 characters.")
		}
		c.title = &t
	}
	if n := strings.TrimSpace(in.Note); n != "" {
		if len([]rune(n)) > 500 {
			add("note", "length", "Keep the note under 500 characters.")
		}
		c.note = &n
	}
	for _, id := range in.EligiblePlanIDs {
		id = strings.ToLower(strings.TrimSpace(id))
		if !uuidPattern.MatchString(id) {
			add("eligible_plan_ids", "invalid", "Choose plans from the list.")
			break
		}
		if !slices.Contains(c.planIDs, id) {
			c.planIDs = append(c.planIDs, id)
		}
	}
	seen := map[string]bool{}
	for _, m := range in.Meals {
		id := strings.ToLower(strings.TrimSpace(m.MealID))
		if !uuidPattern.MatchString(id) {
			add("meals", "invalid", "Choose meals from the catalogue.")
			break
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		if m.MaxPerCustomer != nil && (*m.MaxPerCustomer < 1 || *m.MaxPerCustomer > 14) {
			add("meals", "range", "A limit per customer must be between 1 and 14.")
			break
		}
		c.meals = append(c.meals, MealInput{MealID: id, MaxPerCustomer: m.MaxPerCustomer})
		c.mealIDs = append(c.mealIDs, id)
	}
	if len(c.meals) > 40 {
		add("meals", "count", "A menu can have at most 40 meals.")
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	refs, err := s.catalog.MealRefs(ctx, q, c.mealIDs)
	if err != nil {
		return nil, err
	}
	for _, id := range c.mealIDs {
		if _, ok := refs[id]; !ok {
			add("meals", "unknown", "One of the meals no longer exists.")
			break
		}
	}
	c.cutoff = s.Policy.CutoffFor(c.week)
	if in.CutoffAt != nil {
		c.cutoff = in.CutoffAt.UTC()
	}
	weekStart := time.Date(c.week.Year(), c.week.Month(), c.week.Day(), 0, 0, 0, 0, s.Policy.Location)
	if !c.cutoff.Before(weekStart) {
		add("cutoff_at", "order", "Selection must close before the delivery week starts.")
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	return c, nil
}

func (s *Service) writeMeals(ctx context.Context, q *store.Queries, menuID string, meals []MealInput) error {
	if err := q.ClearMenuMeals(ctx, menuID); err != nil {
		return err
	}
	for i, m := range meals {
		var limit *int16
		if m.MaxPerCustomer != nil {
			v := int16(*m.MaxPerCustomer)
			limit = &v
		}
		if err := q.AddMenuMeal(ctx, store.AddMenuMealParams{MenuID: menuID, MealID: m.MealID, Position: int16(i), MaxPerCustomer: limit}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) Create(ctx context.Context, actor string, in Input) (*AdminMenu, error) {
	var id string
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		c, err := s.clean(ctx, tx, in, nil)
		if err != nil {
			return err
		}
		q := store.New(tx)
		id, err = q.CreateMenu(ctx, store.CreateMenuParams{
			WeekStart: c.week, Title: c.title, Note: c.note, CutoffAt: c.cutoff, EligiblePlanIds: c.planIDs, Actor: nilIfEmpty(actor),
		})
		if db.IsUniqueViolation(err, "weekly_menus_week_key") {
			return httpx.ValidationError([]httpx.FieldError{{Field: "week", Code: "taken", Message: "There is already a menu for this week."}})
		}
		if err != nil {
			return err
		}
		if err := s.writeMeals(ctx, q, id, c.meals); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "menu.created", TargetType: "weekly_menu", TargetID: id,
			Metadata: map[string]any{"week": FormatWeek(c.week), "meals": len(c.meals)}})
	})
	if err != nil {
		return nil, err
	}
	return s.AdminGet(ctx, id)
}

var errConflict = httpx.NewError(http.StatusConflict, "version_conflict", "Someone else changed this menu. Reload it and try again.")

// Update edits a menu. Once published, its cutoff is fixed (customers rely
// on it) and meals that someone has chosen cannot be taken off.
func (s *Service) Update(ctx context.Context, actor, id string, in Input) (*AdminMenu, error) {
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		m, err := q.GetMenuForUpdate(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		if int(m.Version) != in.Version {
			return errConflict
		}
		if m.Status == "closed" || m.Status == "archived" {
			return httpx.NewError(http.StatusConflict, "menu_closed", "This menu has closed and can no longer be changed.")
		}
		c, err := s.clean(ctx, tx, in, &m.WeekStart)
		if err != nil {
			return err
		}
		if m.Status == "published" {
			if !c.cutoff.Equal(m.CutoffAt) {
				return httpx.ValidationError([]httpx.FieldError{{Field: "cutoff_at", Code: "fixed", Message: "The cutoff of a published menu cannot change."}})
			}
			if err := s.checkStillOnMenu(ctx, tx, id, c.mealIDs); err != nil {
				return err
			}
			if err := s.checkVisible(ctx, tx, c.mealIDs); err != nil {
				return err
			}
		}
		if _, err := q.UpdateMenu(ctx, store.UpdateMenuParams{ID: id, Version: m.Version, Title: c.title, Note: c.note,
			CutoffAt: c.cutoff, EligiblePlanIds: c.planIDs, Actor: nilIfEmpty(actor)}); err != nil {
			return err
		}
		if err := s.writeMeals(ctx, q, id, c.meals); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "menu.updated", TargetType: "weekly_menu", TargetID: id,
			Metadata: map[string]any{"meals": len(c.meals)}})
	})
	if err != nil {
		return nil, err
	}
	return s.AdminGet(ctx, id)
}

func (s *Service) checkStillOnMenu(ctx context.Context, q db.Querier, menuID string, keep []string) error {
	if s.Usage == nil {
		return nil
	}
	selected, err := s.Usage.SelectedMeals(ctx, q, menuID)
	if err != nil {
		return err
	}
	for _, id := range selected {
		if !slices.Contains(keep, id) {
			return httpx.ValidationError([]httpx.FieldError{{Field: "meals", Code: "selected",
				Message: "Customers have chosen a meal you removed. It has to stay on this menu."}})
		}
	}
	return nil
}

func (s *Service) checkVisible(ctx context.Context, q db.Querier, ids []string) error {
	refs, err := s.catalog.MealRefs(ctx, q, ids)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if !refs[id].Visible {
			return httpx.NewError(http.StatusConflict, "meal_not_published",
				"Every meal on a published menu must itself be published. Publish "+refs[id].Name+" first, or take it off the menu.")
		}
	}
	return nil
}

// Publish makes a menu visible to customers now, or at publishAt.
func (s *Service) Publish(ctx context.Context, actor, id string, publishAt *time.Time) (*AdminMenu, error) {
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		return s.publish(ctx, tx, actor, id, publishAt)
	})
	if err != nil {
		return nil, err
	}
	return s.AdminGet(ctx, id)
}

func (s *Service) publish(ctx context.Context, tx pgx.Tx, actor, id string, publishAt *time.Time) error {
	q := store.New(tx)
	m, err := q.GetMenuForUpdate(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	now := s.Now()
	if m.Status != "draft" && m.Status != "scheduled" {
		return httpx.NewError(http.StatusConflict, "invalid_status", "Only a draft or scheduled menu can be published.")
	}
	if !m.CutoffAt.After(now) {
		return httpx.NewError(http.StatusConflict, "cutoff_passed", "This menu's cutoff has passed, so customers could no longer choose from it.")
	}
	rows, err := q.MenuMeals(ctx, id)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return httpx.NewError(http.StatusConflict, "no_meals", "Add meals before publishing the menu.")
	}
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.MealID
	}
	if err := s.checkVisible(ctx, tx, ids); err != nil {
		return err
	}
	status := "published"
	if publishAt != nil && publishAt.After(now) {
		if !publishAt.Before(m.CutoffAt) {
			return httpx.ValidationError([]httpx.FieldError{{Field: "publish_at", Code: "order", Message: "Publish the menu before its cutoff."}})
		}
		status = "scheduled"
	}
	if _, err := q.SetMenuStatus(ctx, store.SetMenuStatusParams{ID: id, Status: status, PublishAt: publishAt, Now: now, Actor: nilIfEmpty(actor)}); err != nil {
		return err
	}
	if err := audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "menu." + status, TargetType: "weekly_menu", TargetID: id,
		Metadata: map[string]any{"week": FormatWeek(m.WeekStart)}}); err != nil {
		return err
	}
	if status == "published" && s.Hooks != nil {
		return s.Hooks.MenuPublished(ctx, tx, id, m.WeekStart)
	}
	return nil
}

// Unpublish hides a menu again. It is refused once anyone has chosen from
// it: taking the menu away would strand their choices.
func (s *Service) Unpublish(ctx context.Context, actor, id string) (*AdminMenu, error) {
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		m, err := q.GetMenuForUpdate(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		if m.Status != "published" && m.Status != "scheduled" {
			return httpx.NewError(http.StatusConflict, "invalid_status", "Only a published or scheduled menu can be unpublished.")
		}
		if s.Usage != nil {
			selected, err := s.Usage.SelectedMeals(ctx, tx, id)
			if err != nil {
				return err
			}
			if len(selected) > 0 {
				return httpx.NewError(http.StatusConflict, "menu_in_use", "Customers have already chosen meals from this menu, so it stays published.")
			}
		}
		if _, err := q.SetMenuStatus(ctx, store.SetMenuStatusParams{ID: id, Status: "draft", Now: s.Now(), Actor: nilIfEmpty(actor)}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "menu.unpublished", TargetType: "weekly_menu", TargetID: id})
	})
	if err != nil {
		return nil, err
	}
	return s.AdminGet(ctx, id)
}

// ---------------------------------------------------------------- scheduler

// PublishDue publishes scheduled menus whose time has come, and closes
// published menus whose cutoff has passed. Safe to run any number of times.
func (s *Service) PublishDue(ctx context.Context, now time.Time) (published, closed int, err error) {
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		ids, err := q.DueScheduledMenus(ctx, now)
		if err != nil {
			return err
		}
		for _, id := range ids {
			m, err := q.GetMenu(ctx, id)
			if err != nil {
				return err
			}
			if !m.CutoffAt.After(now) {
				// Too late to be useful; leave it for staff.
				continue
			}
			if _, err := q.SetMenuStatus(ctx, store.SetMenuStatusParams{ID: id, Status: "published", Now: now}); err != nil {
				return err
			}
			if err := audit.Record(ctx, tx, audit.Entry{Action: "menu.published", TargetType: "weekly_menu", TargetID: id,
				Metadata: map[string]any{"scheduled": true}}); err != nil {
				return err
			}
			if s.Hooks != nil {
				if err := s.Hooks.MenuPublished(ctx, tx, id, m.WeekStart); err != nil {
					return err
				}
			}
			published++
		}
		done, err := q.CloseMenusPastCutoff(ctx, now)
		closed = len(done)
		return err
	})
	return published, closed, err
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
