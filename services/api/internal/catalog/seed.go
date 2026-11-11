package catalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/catalog/store"
)

// Demo content, for local development and staging only.
//
// Everything here is marked is_demo and is refused unless
// CATALOG_DEMO_ENABLED is on. It invents no people: the "chef" is the Mise
// development kitchen, labelled as a demo profile. The photographs are the
// pre-launch stock images from Pexels (credited in docs/content/image-credits.md),
// not Mise's cooking. Times, servings and ingredients are illustrative. No
// nutrition is given and no allergens are declared, because both are facts
// about a real recipe that must come from the kitchen and the nutritionist.

type demoMeal struct {
	in       MealInput
	category string
	chef     bool
	image    string
	alt      string
	w, h     int32
	credit   string
}

var demoCategories = []CategoryInput{
	{Slug: "caribbean-classics", Name: "Caribbean classics", Description: "The dishes people grow up with, cooked the way they remember.", Position: 1, IsDemo: true},
	{Slug: "family", Name: "Family table", Description: "Generous meals that please a table of different tastes.", Position: 2, IsDemo: true},
	{Slug: "lighter", Name: "Lighter and balanced", Description: "Everyday meals built around vegetables, pulses and lean protein.", Position: 3, IsDemo: true},
	{Slug: "chef-series", Name: "Chef series", Description: "Occasional dishes from guest cooks.", Position: 4, IsDemo: true},
}

var demoChef = ChefInput{
	Slug: "mise-kitchen", DisplayName: "Mise Kitchen (demo)", RoleTitle: ptr("Demo profile for the development kitchen"),
	Bio: "This is placeholder text for development. Mise's recipes are written and tested in its kitchen in Trinidad. " +
		"Real chef profiles will appear here once the people behind each recipe have approved what is said about them.",
	ImageSrc: ptr("/catalog/demo/story-kitchen.jpg"), ImageAlt: ptr("Hands chopping garlic on a wooden board"),
	Status: "published", IsDemo: true,
}

func ptr[T any](v T) *T { return &v }

func ing(pairs ...string) []IngredientInput {
	var out []IngredientInput
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, IngredientInput{Name: pairs[i], Quantity: ptr(pairs[i+1])})
	}
	return out
}

var demoMeals = []demoMeal{
	{
		in: MealInput{Slug: "curry-chicken-and-potato", Name: "Curry chicken and potato",
			Summary:         "Chicken and potato cooked down in green seasoning and Trinidad curry, made for rice or roti.",
			Description:     "Demo content. A weeknight curry the way many Trinidadian kitchens make it: the chicken marinated in green seasoning, the curry powder cooked out in oil before anything else goes in, and potatoes that thicken the sauce as they soften.",
			CookingOverview: "Season the chicken with the green seasoning. Toast the curry in oil, add the chicken and brown it. Add potatoes and water, cover, and simmer until the potatoes break at the edges. Finish with chadon beni.",
			PrepMinutes:     15, CookMinutes: 30, Servings: 2, Protein: ptr("chicken"), IsDemo: true,
			Ingredients: ing("Chicken thighs, bone-in", "500 g", "Potatoes", "2 medium", "Green seasoning", "3 tbsp", "Trinidad curry powder", "2 tbsp", "Onion", "1", "Garlic", "4 cloves", "Chadon beni", "small bunch")},
		category: "caribbean-classics", chef: true, image: "/catalog/demo/meal-curry-chicken.jpg",
		alt: "Curry chicken with chunks of potato in a white bowl", w: 1800, h: 1201, credit: "Snappr, Pexels",
	},
	{
		in: MealInput{Slug: "jerk-chicken-rice-and-peas", Name: "Jerk chicken, rice and peas",
			Summary:         "Chicken in a pimento and scotch bonnet rub, with coconut rice and peas and fried ripe plantain.",
			Description:     "Demo content. A table-filling plate: chicken rubbed with pimento, thyme and scotch bonnet, rice and peas cooked in coconut milk, and plantain fried until the edges catch.",
			CookingOverview: "Rub the chicken and leave it while the rice cooks. Simmer rice with peas and coconut milk. Roast or grill the chicken, then fry the plantain in the last few minutes.",
			PrepMinutes:     20, CookMinutes: 35, Servings: 2, Protein: ptr("chicken"), IsDemo: true,
			Ingredients: ing("Chicken legs", "4", "Jerk rub", "3 tbsp", "Long-grain rice", "1 cup", "Red kidney beans", "1 can", "Coconut milk", "200 ml", "Ripe plantain", "1", "Thyme", "4 sprigs")},
		category: "family", chef: true, image: "/catalog/demo/meal-jerk-chicken.jpg",
		alt: "Jerk chicken with rice and peas and fried plantain on a white plate", w: 1800, h: 1201, credit: "Snappr, Pexels",
	},
	{
		in: MealInput{Slug: "stewed-oxtail-with-thyme", Name: "Stewed oxtail with thyme",
			Summary:         "Oxtail browned in burnt sugar and braised until it gives. A slow Sunday dish.",
			Description:     "Demo content. Oxtail browned in caramelised sugar, the Caribbean way, then braised with thyme, pimento and butter beans until it falls from the bone.",
			CookingOverview: "Brown the sugar until dark, sear the oxtail in it, add aromatics and stock, and braise covered for most of two hours. Add butter beans near the end.",
			PrepMinutes:     20, CookMinutes: 150, Servings: 2, Protein: ptr("beef"), IsDemo: true,
			Ingredients: ing("Oxtail", "800 g", "Brown sugar", "2 tbsp", "Butter beans", "1 can", "Thyme", "6 sprigs", "Pimento berries", "6", "Carrot", "1")},
		category: "chef-series", chef: false, image: "/catalog/demo/meal-oxtail.jpg",
		alt: "Dark, glossy stewed oxtail topped with a sprig of thyme", w: 1800, h: 1201, credit: "Snappr, Pexels",
	},
	{
		in: MealInput{Slug: "curried-channa-and-rice", Name: "Curried channa and rice",
			Summary:         "Chickpeas simmered with geera, garlic and tomato, served beside steamed rice.",
			Description:     "Demo content. A quick, filling curry of chickpeas with roasted geera and tomato, the kind that is better the next day.",
			CookingOverview: "Toast the geera and curry, add garlic and tomato, then the chickpeas and a little water. Simmer while the rice steams, crushing some chickpeas to thicken the sauce.",
			PrepMinutes:     10, CookMinutes: 20, Servings: 2, Protein: ptr("plant"), DietaryTags: []string{"vegan"}, IsDemo: true,
			Ingredients: ing("Chickpeas", "2 cans", "Geera (cumin)", "1 tsp", "Curry powder", "1 tbsp", "Tomato", "2", "Garlic", "3 cloves", "Basmati rice", "1 cup")},
		category: "lighter", chef: true, image: "/catalog/demo/meal-channa.jpg",
		alt: "A white plate, half steamed rice and half curried chickpeas", w: 1775, h: 1800, credit: "I Own My Food Art, Pexels",
	},
	{
		in: MealInput{Slug: "pumpkin-soup", Name: "Pumpkin soup",
			Summary:         "Pumpkin and split peas blended smooth with coconut milk, ginger and a little pepper.",
			Description:     "Demo content. A Saturday-soup kind of comfort, made lighter: pumpkin and split peas cooked soft, blended with coconut milk and lifted with ginger.",
			CookingOverview: "Simmer split peas until soft, add pumpkin, onion and ginger, then coconut milk. Blend until smooth and season with a touch of pepper.",
			PrepMinutes:     15, CookMinutes: 35, Servings: 2, Protein: ptr("plant"), DietaryTags: []string{"vegetarian"}, IsDemo: true,
			Ingredients: ing("Pumpkin", "600 g", "Yellow split peas", "1/2 cup", "Coconut milk", "200 ml", "Ginger", "thumb-sized piece", "Onion", "1", "Pimento pepper", "1")},
		category: "lighter", chef: true, image: "/catalog/demo/meal-pumpkin-soup.jpg",
		alt: "A terracotta pot of smooth yellow soup", w: 1800, h: 1202, credit: "Snappr, Pexels",
	},
	{
		in: MealInput{Slug: "fried-ripe-plantain", Name: "Fried ripe plantain",
			Summary:         "Ripe plantain, sliced long and fried until the edges caramelise. The side that goes with everything.",
			Description:     "Demo content. The simplest dish here and one of the most loved: plantain left to ripen until black-spotted, sliced on the diagonal and fried.",
			CookingOverview: "Slice the plantain on a long diagonal and fry in shallow oil, turning once, until deep gold at the edges.",
			PrepMinutes:     5, CookMinutes: 10, Servings: 2, Protein: ptr("plant"), DietaryTags: []string{"vegan"}, IsDemo: true,
			Ingredients: ing("Ripe plantain", "2", "Vegetable oil", "for frying", "Salt", "a pinch")},
		category: "caribbean-classics", chef: true, image: "/catalog/demo/meal-plantain.jpg",
		alt: "Slices of fried ripe plantain on a white plate", w: 1800, h: 1201, credit: "a Pexels contributor",
	},
}

// SeedDemo loads the demo catalogue and publishes it. It is idempotent:
// items that already exist (by slug) are left alone. It returns how many
// items were created.
func (s *Service) SeedDemo(ctx context.Context) (int, error) {
	if !s.opts.AllowDemo {
		return 0, errors.New("refusing to load demo content: CATALOG_DEMO_ENABLED is off")
	}
	q := store.New(s.pool)
	created := 0
	categoryIDs := map[string]string{}
	for _, c := range demoCategories {
		existing, err := q.GetCategoryBySlug(ctx, c.Slug)
		if err == nil {
			categoryIDs[c.Slug] = existing.ID
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return created, err
		}
		cat, err := s.CreateCategory(ctx, "", c)
		if err != nil {
			return created, fmt.Errorf("category %s: %w", c.Slug, err)
		}
		categoryIDs[c.Slug] = cat.ID
		created++
	}
	var chefID string
	if existing, err := q.GetChefBySlug(ctx, demoChef.Slug); err == nil {
		chefID = existing.ID
	} else if errors.Is(err, pgx.ErrNoRows) {
		chef, err := s.SaveChef(ctx, "", "", demoChef, true)
		if err != nil {
			return created, fmt.Errorf("chef: %w", err)
		}
		chefID = chef.ID
		created++
	} else {
		return created, err
	}
	for _, d := range demoMeals {
		if _, err := q.GetMealBySlug(ctx, d.in.Slug); err == nil {
			continue
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return created, err
		}
		in := d.in
		in.CategoryID = categoryIDs[d.category]
		if d.chef {
			in.ChefID = &chefID
		}
		in.Images = []ImageInput{{Src: d.image, Alt: d.alt, Width: &d.w, Height: &d.h, Credit: ptr(d.credit)}}
		m, err := s.CreateMeal(ctx, "", in)
		if err != nil {
			return created, fmt.Errorf("meal %s: %w", in.Slug, err)
		}
		if _, err := s.SetStatus(ctx, "", m.ID, "published"); err != nil {
			return created, fmt.Errorf("publish %s: %w", in.Slug, err)
		}
		created++
	}
	return created, nil
}
