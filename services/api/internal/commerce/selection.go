package commerce

import (
	"context"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/catalog"
	"mise.tt/api/internal/customers"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/menus"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/subscriptions"
	"mise.tt/api/internal/vocab"
)

// MealNote is what the customer should know about one meal on the menu,
// from their own records. Preferences and allergies are kept apart: a
// preference match is a convenience, an allergy note is a safety warning.
type MealNote struct {
	// ContainsYourAllergens lists allergens the meal is declared to contain
	// that the customer told us they are allergic to.
	ContainsYourAllergens []vocab.Term `json:"contains_your_allergens"`
	// AllergensUnconfirmed: the meal's allergens have not been declared, so
	// nothing can be said about it for an allergy.
	AllergensUnconfirmed bool `json:"allergens_unconfirmed"`
	// MatchesPreferences lists the customer's dietary preferences the meal
	// is tagged with (editorial tags, not certification).
	MatchesPreferences []vocab.Term `json:"matches_preferences"`
}

// SelectionPage is everything the meal-selection screen needs.
type SelectionPage struct {
	Cycle              CycleView                `json:"cycle"`
	Menu               *menus.Menu              `json:"menu"`
	Selection          *subscriptions.Selection `json:"selection"`
	Allowance          int                      `json:"allowance"`
	Selected           int                      `json:"selected"`
	Remaining          int                      `json:"remaining"`
	Open               bool                     `json:"open"`
	Confirmed          bool                     `json:"confirmed"`
	Notes              map[string]MealNote      `json:"notes"`
	HasAllergies       bool                     `json:"has_allergies"`
	AllergiesReviewed  bool                     `json:"allergies_reviewed"`
	CrossContactNotice string                   `json:"cross_contact_notice"`
	ServerTime         string                   `json:"server_time"`
}

// Weeks lists the customer's recent and upcoming weeks.
func (s *Service) Weeks(ctx context.Context, p *identity.Principal) ([]CycleView, error) {
	cid, err := s.customerID(ctx, p)
	if err != nil {
		return nil, err
	}
	rows, err := s.Subs.CyclesOfCustomer(ctx, cid, 12)
	if err != nil {
		return nil, err
	}
	out := []CycleView{}
	for i := range rows {
		v, err := s.cycleView(ctx, s.pool, &rows[i].Cycle, rows[i].PlanName)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, nil
}

// SelectionPage returns the meal-selection screen for one of the
// customer's weeks.
func (s *Service) SelectionPage(ctx context.Context, p *identity.Principal, cycleID string) (*SelectionPage, error) {
	cid, err := s.customerID(ctx, p)
	if err != nil {
		return nil, err
	}
	c, err := s.Subs.CycleForCustomer(ctx, s.pool, cid, cycleID)
	if err != nil {
		return nil, err
	}
	return s.selectionPage(ctx, s.pool, cid, c)
}

func (s *Service) selectionPage(ctx context.Context, q db.Querier, customerID string, c *subscriptions.Cycle) (*SelectionPage, error) {
	planName, err := s.Subs.PlanNameForCycle(ctx, q, c.ID)
	if err != nil {
		return nil, err
	}
	cv, err := s.cycleView(ctx, q, c, planName)
	if err != nil {
		return nil, err
	}
	page := &SelectionPage{Cycle: *cv, Allowance: int(c.MealsPerWeek), Open: cv.SelectionOpen, Notes: map[string]MealNote{},
		CrossContactNotice: catalog.CrossContactNotice, ServerTime: s.Now().UTC().Format("2006-01-02T15:04:05Z")}
	if cv.MenuPublished {
		if m, err := s.Menus.PublishedWeek(ctx, c.WeekStart); err == nil {
			page.Menu = m
		}
	}
	if page.Selection, err = s.Subs.SelectionFor(ctx, q, c.ID); err != nil {
		return nil, err
	}
	if page.Selection != nil {
		page.Selected = page.Selection.Count
		page.Confirmed = page.Selection.Status == "confirmed" || (page.Selection.WasConfirmed != nil && *page.Selection.WasConfirmed)
	}
	page.Remaining = max(page.Allowance-page.Selected, 0)

	prefs, err := customers.PreferencesOf(ctx, q, customerID)
	if err != nil {
		return nil, err
	}
	var allergies []string
	for _, a := range prefs.Allergies {
		allergies = append(allergies, a.Allergen)
	}
	page.HasAllergies = len(allergies) > 0
	page.AllergiesReviewed = prefs.AllergiesReviewedAt != nil
	if page.Menu != nil {
		for _, m := range page.Menu.Meals {
			note := MealNote{ContainsYourAllergens: []vocab.Term{}, MatchesPreferences: []vocab.Term{}}
			if m.Allergens.Status != "declared" {
				note.AllergensUnconfirmed = true
			} else {
				for _, t := range m.Allergens.Contains {
					if slices.Contains(allergies, t.Key) {
						note.ContainsYourAllergens = append(note.ContainsYourAllergens, t)
					}
				}
			}
			for _, t := range m.Dietary {
				if slices.Contains(prefs.Dietary, t.Key) {
					note.MatchesPreferences = append(note.MatchesPreferences, t)
				}
			}
			page.Notes[m.ID] = note
		}
	}
	return page, nil
}

// SaveSelectionInput is the customer's choice.
type SaveSelectionInput struct {
	Items   []subscriptions.Item `json:"items"`
	Version *int                 `json:"version"`
}

// SaveSelection stores a draft choice (it can be changed until the cutoff).
func (s *Service) SaveSelection(ctx context.Context, p *identity.Principal, cycleID string, in SaveSelectionInput) (*SelectionPage, error) {
	return s.saveSelection(ctx, p, cycleID, in, false)
}

// FinalizeSelection confirms the choice. With items, they are saved and
// confirmed in one step; without, the saved draft is confirmed. A confirmed
// choice can still be changed until the cutoff.
func (s *Service) FinalizeSelection(ctx context.Context, p *identity.Principal, cycleID string, in SaveSelectionInput) (*SelectionPage, error) {
	return s.saveSelection(ctx, p, cycleID, in, true)
}

func (s *Service) saveSelection(ctx context.Context, p *identity.Principal, cycleID string, in SaveSelectionInput, confirm bool) (*SelectionPage, error) {
	cid, err := s.customerID(ctx, p)
	if err != nil {
		return nil, err
	}
	cycleID = strings.ToLower(strings.TrimSpace(cycleID))
	var page *SelectionPage
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		owned, err := s.Subs.CycleForCustomer(ctx, tx, cid, cycleID)
		if err != nil {
			return err
		}
		// The row lock orders this save against the cutoff job.
		c, err := s.Subs.Cycle(ctx, tx, owned.ID, true)
		if err != nil {
			return err
		}
		items := in.Items
		if confirm && items == nil {
			sel, err := s.Subs.SelectionFor(ctx, tx, c.ID)
			if err != nil {
				return err
			}
			if sel != nil {
				items = sel.Items
			}
		}
		menu, err := s.Menus.ForWeek(ctx, tx, c.WeekStart)
		if err != nil {
			return err
		}
		sel, err := s.Subs.Save(ctx, tx, c, menu, s.Catalog, subscriptions.SaveInput{Items: items, Version: in.Version, Confirm: confirm, Actor: p.UserID})
		if err != nil {
			return err
		}
		if confirm {
			if err := s.notify(ctx, tx, "selection_confirmed:"+c.ID, EmailSelectionConfirm, cid, map[string]string{
				"week": weekLabel(c.WeekStart), "meals": itoa(sel.Count), "cutoff": localTime(c.CutoffAt),
				"link": s.link("/menu/" + menus.FormatWeek(c.WeekStart)),
			}); err != nil {
				return err
			}
		}
		page, err = s.selectionPage(ctx, tx, cid, c)
		return err
	})
	return page, err
}
