package subscriptions

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/catalog"
	"mise.tt/api/internal/menus"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/subscriptions/store"
)

// Item is one chosen meal and how many kits of it.
type Item struct {
	MealID   string `json:"meal_id"`
	Quantity int    `json:"quantity"`
}

// Selection is the meals chosen for one cycle.
type Selection struct {
	ID          string     `json:"id"`
	Status      string     `json:"status"` // draft, confirmed, locked
	Version     int        `json:"version"`
	Items       []Item     `json:"items"`
	Count       int        `json:"count"`
	ConfirmedAt *time.Time `json:"confirmed_at"`
	LockedAt    *time.Time `json:"locked_at"`
	// WasConfirmed, once locked: whether it was confirmed by the cutoff.
	WasConfirmed *bool     `json:"was_confirmed,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
	MenuID       string    `json:"menu_id"`
}

// SelectionFor returns the selection of a cycle, or nil if none was made.
func (s *Service) SelectionFor(ctx context.Context, q db.Querier, cycleID string) (*Selection, error) {
	row, err := store.New(q).GetSelectionForCycle(ctx, cycleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.selectionView(ctx, q, row)
}

func (s *Service) selectionView(ctx context.Context, q db.Querier, row store.MealSelection) (*Selection, error) {
	items, err := store.New(q).SelectionItems(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	out := &Selection{ID: row.ID, Status: row.Status, Version: int(row.Version), Items: []Item{}, ConfirmedAt: row.ConfirmedAt,
		LockedAt: row.LockedAt, WasConfirmed: row.WasConfirmed, UpdatedAt: row.UpdatedAt, MenuID: row.MenuID}
	for _, it := range items {
		out.Items = append(out.Items, Item{MealID: it.MealID, Quantity: int(it.Quantity)})
		out.Count += int(it.Quantity)
	}
	return out, nil
}

// SelectedMeals lists the meals customers have chosen from a menu. It
// implements menus.Usage.
func (s *Service) SelectedMeals(ctx context.Context, q db.Querier, menuID string) ([]string, error) {
	return store.New(q).SelectedMealsForMenu(ctx, menuID)
}

var (
	ErrCutoffPassed = httpx.NewError(http.StatusConflict, "selection_closed",
		"Meal selection for this week has closed. Your choices can no longer change.")
	errNoMenu = httpx.NewError(http.StatusConflict, "menu_not_available",
		"The menu for this week is not out yet. You can choose your meals once it is published.")
	errNotDelivering = httpx.NewError(http.StatusConflict, "week_not_delivered",
		"This week is not being delivered, so there is nothing to choose.")
	errSelectionChanged = httpx.NewError(http.StatusConflict, "version_conflict",
		"Your choices were changed somewhere else. We've loaded the latest; please check them and try again.")
)

// SaveInput is a customer's choice for a cycle.
type SaveInput struct {
	Items []Item
	// Version, if set, must match the stored selection (another device may
	// have changed it).
	Version *int
	// Confirm also confirms the choice; it must then use the whole allowance.
	Confirm bool
	Actor   string
}

// Save replaces a cycle's chosen meals. The caller has locked the cycle row
// (Cycle(..., lock=true)) in the same transaction, so this cannot interleave
// with the cutoff locking the selection. Every rule is checked here, on the
// server: the cutoff, the menu, the plan's allowance, per-meal limits.
func (s *Service) Save(ctx context.Context, q db.Querier, cycle *Cycle, menu *menus.Info, cat *catalog.Service, in SaveInput) (*Selection, error) {
	now := s.Now()
	if cycle.State != "scheduled" {
		if cycle.State == "paused" || cycle.State == "cancelled" {
			return nil, errNotDelivering
		}
		return nil, ErrCutoffPassed
	}
	if !now.Before(cycle.CutoffAt) {
		return nil, ErrCutoffPassed
	}
	if menu == nil || !menu.Published() {
		return nil, errNoMenu
	}
	if !menu.AllowsPlan(cycle.PlanID) {
		return nil, httpx.NewError(http.StatusConflict, "menu_not_for_plan", "This week's menu is not available on your plan.")
	}

	allowance := int(cycle.MealsPerWeek)
	merged := map[string]int{}
	var order []string
	var fields []httpx.FieldError
	add := func(code, msg string) {
		fields = append(fields, httpx.FieldError{Field: "items", Code: code, Message: msg})
	}
	for _, it := range in.Items {
		id := strings.ToLower(strings.TrimSpace(it.MealID))
		if it.Quantity < 0 || it.Quantity > 14 {
			add("quantity", "Choose between 0 and 14 of a meal.")
			break
		}
		if it.Quantity == 0 {
			continue
		}
		if _, ok := menu.Meals[id]; !ok {
			add("not_on_menu", "One of the meals is not on this week's menu.")
			break
		}
		if _, seen := merged[id]; !seen {
			order = append(order, id)
		}
		merged[id] += it.Quantity
	}
	total := 0
	for _, id := range order {
		total += merged[id]
		if limit := menu.Meals[id]; limit > 0 && merged[id] > limit {
			add("meal_limit", "You can choose at most "+itoa(limit)+" of one of these meals.")
		}
	}
	if total > allowance {
		add("allowance", "Your plan includes "+itoa(allowance)+" meals a week. Remove "+itoa(total-allowance)+" to continue.")
	}
	if in.Confirm && total < allowance {
		add("allowance", "Choose "+itoa(allowance-total)+" more to use your "+itoa(allowance)+" meals.")
	}
	if len(fields) > 0 {
		return nil, httpx.ValidationError(fields)
	}
	if len(order) > 0 {
		refs, err := cat.MealRefs(ctx, q, order)
		if err != nil {
			return nil, err
		}
		for _, id := range order {
			if !refs[id].Visible {
				return nil, httpx.ValidationError([]httpx.FieldError{{Field: "items", Code: "unavailable",
					Message: "One of the meals is no longer available. Please choose another."}})
			}
		}
	}

	st := store.New(q)
	row, err := st.GetSelectionForCycle(ctx, cycle.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		row, err = st.CreateSelection(ctx, store.CreateSelectionParams{SubscriptionCycleID: cycle.ID, CustomerID: cycle.CustomerID, MenuID: menu.ID})
		if errors.Is(err, pgx.ErrNoRows) {
			row, err = st.GetSelectionForCycle(ctx, cycle.ID)
		}
	} else if err == nil && in.Version != nil && int(row.Version) != *in.Version {
		return nil, errSelectionChanged
	}
	if err != nil {
		return nil, err
	}
	if row.Status == "locked" {
		return nil, ErrCutoffPassed
	}
	if err := st.ClearSelectionItems(ctx, row.ID); err != nil {
		return nil, err
	}
	for _, id := range order {
		if err := st.AddSelectionItem(ctx, store.AddSelectionItemParams{SelectionID: row.ID, MealID: id, Quantity: int16(merged[id])}); err != nil {
			return nil, err
		}
	}
	status := "draft"
	if in.Confirm {
		status = "confirmed"
	}
	row, err = st.SetSelectionStatus(ctx, store.SetSelectionStatusParams{ID: row.ID, Status: status, Now: now})
	if err != nil {
		return nil, err
	}
	action := "selection.saved"
	if in.Confirm {
		action = "selection.confirmed"
	}
	if err := audit.Record(ctx, q, audit.Entry{ActorUserID: in.Actor, Action: action, TargetType: "subscription_cycle", TargetID: cycle.ID,
		Metadata: map[string]any{"meals": total, "allowance": allowance, "week": menus.FormatWeek(cycle.WeekStart)}}); err != nil {
		return nil, err
	}
	return s.selectionView(ctx, q, row)
}

// Unconfirm returns a confirmed selection to draft (the week's plan changed,
// so the allowance may no longer match).
func (s *Service) Unconfirm(ctx context.Context, q db.Querier, cycleID string) error {
	row, err := store.New(q).GetSelectionForCycle(ctx, cycleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil || row.Status != "confirmed" {
		return err
	}
	_, err = store.New(q).SetSelectionStatus(ctx, store.SetSelectionStatusParams{ID: row.ID, Status: "draft", Now: s.Now()})
	return err
}

// Lock closes a cycle's selection at the cutoff and returns it (nil if the
// customer never chose). Locking twice is harmless.
func (s *Service) Lock(ctx context.Context, q db.Querier, cycleID string) (*Selection, error) {
	row, err := store.New(q).GetSelectionForCycle(ctx, cycleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if row.Status != "locked" {
		if row, err = store.New(q).SetSelectionStatus(ctx, store.SetSelectionStatusParams{ID: row.ID, Status: "locked", Now: s.Now()}); err != nil {
			return nil, err
		}
	}
	return s.selectionView(ctx, q, row)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
