package customers

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/customers/store"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/vocab"
)

// ---------------------------------------------------------------- household

// Household is who the customer cooks for: counts only.
type Household struct {
	Name      *string    `json:"name"`
	Size      int16      `json:"size"`
	Adults    *int16     `json:"adults"`
	Children  *int16     `json:"children"`
	Source    string     `json:"source"`
	UpdatedAt *time.Time `json:"updated_at"`
}

func (s *Service) GetHousehold(ctx context.Context, p *identity.Principal) (*Household, error) {
	c, err := customer(ctx, s.Pool, p)
	if err != nil {
		return nil, err
	}
	h, err := store.New(s.Pool).GetHousehold(ctx, c.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return householdView(h), nil
}

func householdView(h store.CustomerHousehold) *Household {
	return &Household{Name: h.Name, Size: h.Size, Adults: h.Adults, Children: h.Children, Source: h.Source, UpdatedAt: &h.UpdatedAt}
}

type HouseholdInput struct {
	Name     *string `json:"name"`
	Size     int     `json:"size"`
	Adults   *int    `json:"adults"`
	Children *int    `json:"children"`
}

func (s *Service) PutHousehold(ctx context.Context, p *identity.Principal, in HouseholdInput) (*Household, error) {
	var fields []httpx.FieldError
	bad := func(field, code, msg string) {
		fields = append(fields, httpx.FieldError{Field: field, Code: code, Message: msg})
	}
	if in.Size < 1 || in.Size > 20 {
		bad("size", "out_of_range", "Enter between 1 and 20 people.")
	}
	var name *string
	if in.Name != nil {
		name = emptyToNil(*in.Name)
		if name != nil && len([]rune(*name)) > 60 {
			bad("name", "too_long", "Keep the household name to 60 characters.")
		}
	}
	adults, children := toInt16(in.Adults), toInt16(in.Children)
	if in.Adults != nil && (*in.Adults < 1 || *in.Adults > 20) {
		bad("adults", "out_of_range", "Enter between 1 and 20 adults.")
	}
	if in.Children != nil && (*in.Children < 0 || *in.Children > 19) {
		bad("children", "out_of_range", "Enter between 0 and 19 children.")
	}
	if deref(in.Adults)+deref(in.Children) > in.Size {
		bad("size", "too_small", "The household is smaller than the adults and children together.")
	}
	if len(fields) > 0 {
		return nil, httpx.ValidationError(fields)
	}
	var out *Household
	err := db.InTx(ctx, s.Pool, func(tx pgx.Tx) error {
		c, err := customer(ctx, tx, p)
		if err != nil {
			return err
		}
		h, err := store.New(tx).UpsertHousehold(ctx, store.UpsertHouseholdParams{
			CustomerID: c.ID, Name: name, Size: int16(in.Size), Adults: adults, Children: children, Source: "customer",
		})
		if err != nil {
			return err
		}
		out = householdView(h)
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: "customer.household_updated", TargetType: "customer", TargetID: c.ID})
	})
	return out, err
}

func toInt16(v *int) *int16 {
	if v == nil {
		return nil
	}
	n := int16(*v)
	return &n
}

func deref(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

// ---------------------------------------------------------------- preferences and allergies

// Allergy is one allergy the customer has told us about.
type Allergy struct {
	Allergen string  `json:"allergen"`
	Label    string  `json:"label"`
	Detail   *string `json:"detail"`
}

// Preferences are food preferences and, kept apart, allergies. A preference
// ("I prefer vegetarian meals") is not safety information; an allergy is.
type Preferences struct {
	Dietary             []string   `json:"dietary"`
	MealInterests       []string   `json:"meal_interests"`
	Proteins            []string   `json:"proteins"`
	MaxCookMinutes      *int16     `json:"max_cook_minutes"`
	OtherRestrictions   *string    `json:"other_restrictions"`
	Source              string     `json:"source"`
	Allergies           []Allergy  `json:"allergies"`
	AllergiesReviewedAt *time.Time `json:"allergies_reviewed_at"`
	UpdatedAt           *time.Time `json:"updated_at"`
}

// PreferencesFor returns a customer's preferences, empty if never set. The
// catalogue uses it for recommendations.
func (s *Service) PreferencesFor(ctx context.Context, p *identity.Principal) (*Preferences, error) {
	c, err := customer(ctx, s.Pool, p)
	if err != nil {
		return nil, err
	}
	return loadPreferences(ctx, s.Pool, c.ID)
}

func loadPreferences(ctx context.Context, q db.Querier, customerID string) (*Preferences, error) {
	st := store.New(q)
	out := &Preferences{Dietary: []string{}, MealInterests: []string{}, Proteins: []string{}, Source: "customer", Allergies: []Allergy{}}
	row, err := st.GetPreferences(ctx, customerID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, err
	default:
		out.Dietary, out.MealInterests, out.Proteins = row.Dietary, row.MealInterests, row.Proteins
		out.MaxCookMinutes, out.OtherRestrictions, out.Source = row.MaxCookMinutes, row.OtherRestrictions, row.Source
		out.AllergiesReviewedAt, out.UpdatedAt = row.AllergiesReviewedAt, &row.UpdatedAt
	}
	allergies, err := st.ListAllergies(ctx, customerID)
	if err != nil {
		return nil, err
	}
	for _, a := range allergies {
		out.Allergies = append(out.Allergies, Allergy{Allergen: a.Allergen, Label: vocab.Allergens.Label(a.Allergen), Detail: a.Detail})
	}
	return out, nil
}

// AllergyInput is one allergy as sent by the customer.
type AllergyInput struct {
	Allergen string  `json:"allergen"`
	Detail   *string `json:"detail"`
}

// PreferencesInput changes preferences. Fields left out are unchanged, so a
// step that asks only about diets cannot clear anything else. Allergies are
// replaced only when the allergies field is present; an empty list means "no
// allergies" and is recorded as an answer. max_cook_minutes 0 removes the
// limit; an empty other_restrictions clears it.
type PreferencesInput struct {
	Dietary           *[]string       `json:"dietary"`
	MealInterests     *[]string       `json:"meal_interests"`
	Proteins          *[]string       `json:"proteins"`
	MaxCookMinutes    *int            `json:"max_cook_minutes"`
	OtherRestrictions *string         `json:"other_restrictions"`
	Allergies         *[]AllergyInput `json:"allergies"`
}

func checkKeys(field string, keys []string, list vocab.List, fields *[]httpx.FieldError) []string {
	out := []string{}
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if !list.Has(k) {
			*fields = append(*fields, httpx.FieldError{Field: field, Code: "invalid", Message: "Choose from the list."})
			return nil
		}
		if !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	return out
}

func (s *Service) PutPreferences(ctx context.Context, p *identity.Principal, in PreferencesInput) (*Preferences, error) {
	var fields []httpx.FieldError
	var dietary, interests, proteins []string
	if in.Dietary != nil {
		raw := slices.Clone(*in.Dietary)
		noPref := slices.Contains(raw, vocab.NoDietaryPreference)
		raw = slices.DeleteFunc(raw, func(k string) bool { return k == vocab.NoDietaryPreference })
		dietary = checkKeys("dietary", raw, vocab.Dietary, &fields)
		if noPref && len(raw) > 0 {
			dietary = append(dietary, vocab.NoDietaryPreference)
		}
		if msg := vocab.Contradiction(dietary); msg != "" {
			fields = append(fields, httpx.FieldError{Field: "dietary", Code: "contradictory", Message: msg})
		}
		// "No specific preference" alone is stored as no dietary keys.
		dietary = slices.DeleteFunc(dietary, func(k string) bool { return k == vocab.NoDietaryPreference })
	}
	if in.MealInterests != nil {
		interests = checkKeys("meal_interests", *in.MealInterests, vocab.MealInterests, &fields)
	}
	if in.Proteins != nil {
		proteins = checkKeys("proteins", *in.Proteins, vocab.Proteins, &fields)
	}
	if in.MaxCookMinutes != nil && *in.MaxCookMinutes != 0 && (*in.MaxCookMinutes < 10 || *in.MaxCookMinutes > 240) {
		fields = append(fields, httpx.FieldError{Field: "max_cook_minutes", Code: "out_of_range", Message: "Choose between 10 and 240 minutes, or no limit."})
	}
	if in.OtherRestrictions != nil && len([]rune(strings.TrimSpace(*in.OtherRestrictions))) > 300 {
		fields = append(fields, httpx.FieldError{Field: "other_restrictions", Code: "too_long", Message: "Keep this to 300 characters."})
	}
	var allergies []AllergyInput
	if in.Allergies != nil {
		seen := map[string]bool{}
		for _, a := range *in.Allergies {
			if !vocab.Allergens.Has(a.Allergen) {
				fields = append(fields, httpx.FieldError{Field: "allergies", Code: "invalid", Message: "Choose allergies from the list."})
				break
			}
			if seen[a.Allergen] {
				continue
			}
			seen[a.Allergen] = true
			if a.Allergen == "other" {
				detail := emptyToNil(derefString(a.Detail))
				if detail == nil || len([]rune(*detail)) > 200 {
					fields = append(fields, httpx.FieldError{Field: "allergies.other", Code: "required", Message: "Tell us what the other allergy is, in up to 200 characters."})
					continue
				}
				allergies = append(allergies, AllergyInput{Allergen: "other", Detail: detail})
				continue
			}
			allergies = append(allergies, AllergyInput{Allergen: a.Allergen})
		}
	}
	if len(fields) > 0 {
		return nil, httpx.ValidationError(fields)
	}

	var out *Preferences
	err := db.InTx(ctx, s.Pool, func(tx pgx.Tx) error {
		c, err := customer(ctx, tx, p)
		if err != nil {
			return err
		}
		cur, err := loadPreferences(ctx, tx, c.ID)
		if err != nil {
			return err
		}
		params := store.UpsertPreferencesParams{
			CustomerID: c.ID, Dietary: cur.Dietary, MealInterests: cur.MealInterests, Proteins: cur.Proteins,
			MaxCookMinutes: cur.MaxCookMinutes, OtherRestrictions: cur.OtherRestrictions, Source: "customer",
			AllergiesReviewedAt: cur.AllergiesReviewedAt,
		}
		if in.Dietary != nil {
			params.Dietary = dietary
		}
		if in.MealInterests != nil {
			params.MealInterests = interests
		}
		if in.Proteins != nil {
			params.Proteins = proteins
		}
		if in.MaxCookMinutes != nil {
			params.MaxCookMinutes = nil
			if *in.MaxCookMinutes != 0 {
				n := int16(*in.MaxCookMinutes)
				params.MaxCookMinutes = &n
			}
		}
		if in.OtherRestrictions != nil {
			params.OtherRestrictions = emptyToNil(*in.OtherRestrictions)
		}
		st := store.New(tx)
		if in.Allergies != nil {
			now := s.now().UTC()
			params.AllergiesReviewedAt = &now
			if err := st.DeleteAllergies(ctx, c.ID); err != nil {
				return err
			}
			for _, a := range allergies {
				if err := st.InsertAllergy(ctx, store.InsertAllergyParams{CustomerID: c.ID, Allergen: a.Allergen, Detail: a.Detail}); err != nil {
					return err
				}
			}
			// Allergies are health information: the audit entry records that
			// they changed and how many there are, not which.
			if err := audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: "customer.allergies_updated", TargetType: "customer", TargetID: c.ID,
				Metadata: map[string]any{"count": len(allergies)}}); err != nil {
				return err
			}
		}
		if _, err := st.UpsertPreferences(ctx, params); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: "customer.preferences_updated", TargetType: "customer", TargetID: c.ID}); err != nil {
			return err
		}
		out, err = loadPreferences(ctx, tx, c.ID)
		return err
	})
	return out, err
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// PreferencesOf returns a customer's preferences and allergies by customer
// id, for work done without a signed-in person (an order made at the cutoff).
func PreferencesOf(ctx context.Context, q db.Querier, customerID string) (*Preferences, error) {
	return loadPreferences(ctx, q, customerID)
}
