// Package vocab holds the fixed value lists that customers and the catalogue
// share: dietary keys, allergens, proteins, meal interests and Trinidad and
// Tobago regions. The database stores the keys; the labels are for API
// responses and documentation. Adding a value here needs no migration.
//
// Keys are stable. Never rename one: stored rows refer to it.
package vocab

import "slices"

// Term is one value: a stable key and its English label.
type Term struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type List []Term

func (l List) Has(key string) bool {
	return slices.ContainsFunc(l, func(t Term) bool { return t.Key == key })
}

func (l List) Label(key string) string {
	for _, t := range l {
		if t.Key == key {
			return t.Label
		}
	}
	return key
}

func (l List) Keys() []string {
	out := make([]string, len(l))
	for i, t := range l {
		out[i] = t.Key
	}
	return out
}

// Dietary preferences a customer may choose, and the matching tags a meal may
// carry. On a meal they are editorial tags set by staff, not certifications.
// The first six are "hard" for recommendations: a vegetarian is not
// recommended a meal that is not tagged vegetarian.
var Dietary = List{
	{"vegetarian", "Vegetarian"},
	{"vegan", "Vegan"},
	{"pescatarian", "Pescatarian"},
	{"halal", "Halal"},
	{"no-pork", "No pork"},
	{"no-beef", "No beef"},
	{"gluten-free", "Gluten-free"},
	{"dairy-free", "Dairy-free"},
	{"high-protein", "High protein"},
	{"low-carb", "Lower carb"},
}

// NoDietaryPreference is the explicit "no specific preference" answer. It
// cannot be combined with any dietary key.
const NoDietaryPreference = "no-preference"

// HardDietary are the dietary keys that exclude meals in recommendations.
var HardDietary = []string{"vegetarian", "vegan", "pescatarian", "halal", "no-pork", "no-beef"}

// Satisfies reports whether a meal's tags meet one dietary requirement. A
// vegan meal also suits a vegetarian or pescatarian; a vegetarian meal suits a
// pescatarian.
func Satisfies(mealTags []string, want string) bool {
	has := func(k string) bool { return slices.Contains(mealTags, k) }
	switch want {
	case "vegetarian":
		return has("vegetarian") || has("vegan")
	case "pescatarian":
		return has("pescatarian") || has("vegetarian") || has("vegan")
	default:
		return has(want)
	}
}

// Contradiction returns a message when dietary choices cannot all hold, or "".
func Contradiction(keys []string) string {
	has := func(k string) bool { return slices.Contains(keys, k) }
	switch {
	case has(NoDietaryPreference) && len(keys) > 1:
		return "\"No specific preference\" cannot be combined with other choices."
	case has("vegan") && has("pescatarian"):
		return "Vegan and pescatarian do not go together: pescatarian meals include fish. Choose one."
	case has("vegetarian") && has("pescatarian"):
		return "Vegetarian and pescatarian do not go together: pescatarian meals include fish. Choose one."
	}
	return ""
}

// Allergens follows the major food allergens commonly declared on labels.
// "other" takes a short description from the customer.
var Allergens = List{
	{"peanuts", "Peanuts"},
	{"tree-nuts", "Tree nuts"},
	{"milk", "Milk"},
	{"eggs", "Eggs"},
	{"fish", "Fish"},
	{"shellfish", "Shellfish"},
	{"wheat", "Wheat"},
	{"soy", "Soy"},
	{"sesame", "Sesame"},
	{"other", "Other"},
}

// MealAllergens are the allergens a meal can be declared to contain ("other"
// is a customer answer only).
var MealAllergens = Allergens[:len(Allergens)-1]

// Proteins is a meal's main protein, and a customer's protein preferences.
var Proteins = List{
	{"chicken", "Chicken"},
	{"beef", "Beef"},
	{"pork", "Pork"},
	{"goat", "Goat"},
	{"lamb", "Lamb"},
	{"fish", "Fish"},
	{"shellfish", "Shellfish"},
	{"egg", "Egg"},
	{"plant", "Plant-based"},
}

// MealInterests are the kinds of meals a customer says interest them in
// onboarding. They are preferences, not plans that exist.
var MealInterests = List{
	{"caribbean-classics", "Caribbean classics"},
	{"balanced-everyday", "Balanced everyday meals"},
	{"high-protein", "High-protein meals"},
	{"family-friendly", "Family-friendly meals"},
	{"vegetarian-options", "Vegetarian options"},
	{"quick-to-cook", "Quick-to-cook meals"},
}

// Countries Mise serves or may serve. Trinidad and Tobago is the first market.
var Countries = List{{"TT", "Trinidad and Tobago"}}

// Regions are the municipal corporations of Trinidad, and Tobago. An address
// in a region is not a promise that Mise delivers there.
var Regions = List{
	{"port-of-spain", "Port of Spain"},
	{"san-fernando", "San Fernando"},
	{"arima", "Arima"},
	{"chaguanas", "Chaguanas"},
	{"point-fortin", "Point Fortin"},
	{"diego-martin", "Diego Martin"},
	{"san-juan-laventille", "San Juan–Laventille"},
	{"tunapuna-piarco", "Tunapuna–Piarco"},
	{"sangre-grande", "Sangre Grande"},
	{"couva-tabaquite-talparo", "Couva–Tabaquite–Talparo"},
	{"mayaro-rio-claro", "Mayaro–Rio Claro"},
	{"princes-town", "Princes Town"},
	{"penal-debe", "Penal–Debe"},
	{"siparia", "Siparia"},
	{"tobago", "Tobago"},
}

// Topics are the optional email topics a customer can turn on or off. Account
// and security email is not a topic: it is always sent.
var Topics = List{
	{"meal_updates", "New meals and recipe updates"},
	{"weekly_menu", "Weekly menu announcements"},
	{"promotions", "Offers and promotions"},
	{"referral_updates", "Invitations and referral updates"},
}

// UpcomingTopics are listed so customers know they are coming; they cannot be
// set until the features that send them exist.
var UpcomingTopics = List{
	{"subscription_reminders", "Subscription reminders"},
	{"delivery_updates", "Delivery updates"},
}

// LaunchConsentTopics are the topics covered by the consent a waitlist lead
// gave ("Email me about the Mise launch, menus and offers").
var LaunchConsentTopics = []string{"weekly_menu", "promotions"}

// FromLeadDietary maps the waitlist's dietary keys onto these. They are the
// same keys today; the map exists so the two lists can drift safely.
var FromLeadDietary = map[string]string{
	"vegetarian": "vegetarian", "vegan": "vegan", "pescatarian": "pescatarian", "halal": "halal",
	"no-pork": "no-pork", "gluten-free": "gluten-free", "dairy-free": "dairy-free", "high-protein": "high-protein",
}

// FromLeadInterests maps the waitlist's menu interests onto MealInterests.
// Waitlist answers with no counterpart ("chef-series", "school-lunch") are
// not carried over.
var FromLeadInterests = map[string]string{
	"family": "family-friendly", "fitness": "high-protein", "caribbean-classics": "caribbean-classics",
}
