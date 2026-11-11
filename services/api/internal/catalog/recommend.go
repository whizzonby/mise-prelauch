package catalog

import (
	"context"
	"fmt"
	"slices"
	"sort"

	"mise.tt/api/internal/vocab"
)

// Recommendations are deterministic and explainable: every suggested meal
// carries the reasons it was chosen, built from the customer's own answers
// and the meal's stored data. There is no model, and none is implied.

// Taste is the part of a customer's preferences that recommendations use.
type Taste struct {
	Dietary        []string
	MealInterests  []string
	Proteins       []string
	MaxCookMinutes *int16
	Allergies      []string // allergen keys
}

type Recommendation struct {
	Meal    MealCard `json:"meal"`
	Reasons []string `json:"reasons"`
}

type Recommendations struct {
	Items []Recommendation `json:"items"`
	// Personalised is false when the customer has given no preferences yet;
	// the list is then simply the newest meals.
	Personalised bool   `json:"personalised"`
	Notice       string `json:"notice"`
}

// interestRules says how a meal interest from onboarding shows in a meal.
var interestRules = map[string]struct {
	categories []string
	tags       []string
	maxMinutes int
	reason     string
}{
	"caribbean-classics": {categories: []string{"caribbean-classics"}, reason: "A Caribbean classic"},
	"family-friendly":    {categories: []string{"family"}, reason: "Made for the family table"},
	"balanced-everyday":  {categories: []string{"lighter", "everyday"}, reason: "A balanced everyday meal"},
	"high-protein":       {tags: []string{"high-protein"}, reason: "High in protein"},
	"vegetarian-options": {tags: []string{"vegetarian", "vegan"}, reason: "A vegetarian option"},
	"quick-to-cook":      {maxMinutes: 30, reason: "Ready in 30 minutes or less"},
}

const candidateLimit = 200

// Recommend ranks published meals for a customer.
//
// Hard rules: a meal must satisfy every "hard" dietary choice (vegetarian,
// vegan, pescatarian, halal, no pork, no beef), and a meal that declares an
// allergen the customer listed is never suggested. A meal whose allergens are
// not yet declared can still be suggested to someone with allergies, ranked
// lower, and the card says its allergens are unconfirmed.
func (s *Service) Recommend(ctx context.Context, t Taste, limit int) (*Recommendations, error) {
	if limit < 1 || limit > 24 {
		limit = 6
	}
	var hard []string
	for _, d := range t.Dietary {
		if slices.Contains(vocab.HardDietary, d) {
			hard = append(hard, d)
		}
	}
	page, err := s.List(ctx, Filter{Diet: hard, PageSize: maxPageSize})
	if err != nil {
		return nil, err
	}
	candidates := page.Items
	// Fetch beyond the first page when the catalogue is large.
	for p := 2; len(candidates) < int(page.Total) && len(candidates) < candidateLimit; p++ {
		next, err := s.List(ctx, Filter{Diet: hard, PageSize: maxPageSize, Page: p})
		if err != nil {
			return nil, err
		}
		if len(next.Items) == 0 {
			break
		}
		candidates = append(candidates, next.Items...)
	}

	personalised := len(t.Dietary) > 0 || len(t.MealInterests) > 0 || len(t.Proteins) > 0 || t.MaxCookMinutes != nil
	type scored struct {
		rec   Recommendation
		score int
		order int
	}
	var list []scored
	for i, m := range candidates {
		if containsDeclared(m, t.Allergies) {
			continue
		}
		var reasons []string
		score := 0
		tags := termKeys(m.Dietary)
		for _, d := range hard {
			reasons = append(reasons, "Suits your "+lower(vocab.Dietary.Label(d))+" preference")
		}
		for _, d := range t.Dietary {
			if !slices.Contains(vocab.HardDietary, d) && slices.Contains(tags, d) {
				score += 3
				reasons = append(reasons, vocab.Dietary.Label(d))
			}
		}
		for _, in := range t.MealInterests {
			rule, ok := interestRules[in]
			if !ok {
				continue
			}
			match := slices.Contains(rule.categories, m.Category.Slug) ||
				slices.ContainsFunc(rule.tags, func(tag string) bool { return slices.Contains(tags, tag) }) ||
				(rule.maxMinutes > 0 && m.TotalMinutes <= rule.maxMinutes)
			if match {
				score += 3
				reasons = append(reasons, rule.reason)
			}
		}
		if m.Protein != nil && slices.Contains(t.Proteins, m.Protein.Key) {
			score += 2
			reasons = append(reasons, "Made with "+lower(m.Protein.Label))
		}
		if t.MaxCookMinutes != nil {
			if m.TotalMinutes <= int(*t.MaxCookMinutes) {
				score += 2
				reasons = append(reasons, fmt.Sprintf("Ready in %d minutes", m.TotalMinutes))
			} else {
				score -= 4
			}
		}
		if len(t.Allergies) > 0 && m.Allergens.Status != "declared" {
			score -= 3
		}
		list = append(list, scored{rec: Recommendation{Meal: m, Reasons: dedupe(reasons)}, score: score, order: i})
	}
	sort.SliceStable(list, func(a, b int) bool {
		if list[a].score != list[b].score {
			return list[a].score > list[b].score
		}
		return list[a].order < list[b].order // newest first, as listed
	})
	out := &Recommendations{Items: []Recommendation{}, Personalised: personalised,
		Notice: "Suggested from the preferences you gave us. " + CrossContactNotice}
	for i := 0; i < len(list) && i < limit; i++ {
		r := list[i].rec
		if r.Reasons == nil {
			r.Reasons = []string{}
		}
		out.Items = append(out.Items, r)
	}
	return out, nil
}

func containsDeclared(m MealCard, allergies []string) bool {
	if m.Allergens.Status != "declared" {
		return false
	}
	for _, a := range m.Allergens.Contains {
		if slices.Contains(allergies, a.Key) {
			return true
		}
	}
	return false
}

func termKeys(ts []vocab.Term) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Key
	}
	return out
}

func dedupe(in []string) []string {
	var out []string
	for _, s := range in {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

func lower(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'A' && r[0] <= 'Z' {
		r[0] += 'a' - 'A'
	}
	return string(r)
}
