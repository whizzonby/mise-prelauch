package server_test

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"mise.tt/api/internal/server"
)

// Sprint 2 integration tests: the catalogue, its administration,
// favourites and recommendations.

type kitchen struct {
	h       *harness
	admin   reqOpt
	catID   string
	otherID string
	chefID  string
}

func newKitchen(t *testing.T, h *harness) *kitchen {
	t.Helper()
	k := &kitchen{h: h, admin: bearer(h.adminLogin("admin@mise.test", "admin"))}
	res := h.do("POST", "/api/v1/admin/catalog/categories", map[string]any{"name": "Caribbean classics", "slug": "caribbean-classics"}, k.admin)
	h.wantStatus(res, http.StatusCreated)
	k.catID = res.Data["id"].(string)
	res = h.do("POST", "/api/v1/admin/catalog/categories", map[string]any{"name": "Lighter and balanced", "slug": "lighter"}, k.admin)
	h.wantStatus(res, http.StatusCreated)
	k.otherID = res.Data["id"].(string)
	res = h.do("POST", "/api/v1/admin/catalog/chefs", map[string]any{"display_name": "Test Kitchen", "bio": "Writes the test recipes.", "status": "published"}, k.admin)
	h.wantStatus(res, http.StatusCreated)
	k.chefID = res.Data["id"].(string)
	return k
}

func (k *kitchen) meal(name string, extra map[string]any) map[string]any {
	m := map[string]any{
		"name": name, "summary": "A summary of " + name + ".", "description": "Slow-cooked " + name + " with thyme.",
		"category_id": k.catID, "chef_id": k.chefID, "prep_minutes": 10, "cook_minutes": 20, "servings": 2,
		"images":      []map[string]any{{"src": "/catalog/test/" + strings.ReplaceAll(strings.ToLower(name), " ", "-") + ".jpg", "alt": "A plate of " + name}},
		"ingredients": []map[string]any{{"name": "Thyme", "quantity": "4 sprigs"}},
	}
	for key, v := range extra {
		m[key] = v
	}
	return m
}

// create saves a draft and returns its id and slug.
func (k *kitchen) create(name string, extra map[string]any) (string, string) {
	k.h.t.Helper()
	res := k.h.do("POST", "/api/v1/admin/catalog/meals", k.meal(name, extra), k.admin)
	k.h.wantStatus(res, http.StatusCreated)
	return res.Data["id"].(string), res.Data["slug"].(string)
}

func (k *kitchen) publish(id string) {
	k.h.t.Helper()
	k.h.wantStatus(k.h.do("POST", "/api/v1/admin/catalog/meals/"+id+"/publish", nil, k.admin), http.StatusOK)
}

func (k *kitchen) published(name string, extra map[string]any) (string, string) {
	k.h.t.Helper()
	id, slug := k.create(name, extra)
	k.publish(id)
	return id, slug
}

func names(t *testing.T, res response) []string {
	t.Helper()
	var out []string
	for _, it := range list(t, res.Data["items"]) {
		out = append(out, obj(t, it)["name"].(string))
	}
	slices.Sort(out)
	return out
}

func TestCatalogShowsOnlyPublishedMeals(t *testing.T) {
	h := newHarness(t)
	k := newKitchen(t, h)
	draftID, draftSlug := k.create("Curry chicken", nil)

	if got := names(t, h.do("GET", "/api/v1/meals", nil)); len(got) != 0 {
		t.Fatalf("drafts listed: %v", got)
	}
	h.wantError(h.do("GET", "/api/v1/meals/"+draftSlug, nil), http.StatusNotFound, "not_found")

	// Publishing needs a photograph.
	noPhoto, _ := k.create("Plain rice", map[string]any{"images": []any{}})
	h.wantError(h.do("POST", "/api/v1/admin/catalog/meals/"+noPhoto+"/publish", nil, k.admin), http.StatusConflict, "image_required")

	k.publish(draftID)
	res := h.do("GET", "/api/v1/meals/"+draftSlug, nil)
	h.wantStatus(res, http.StatusOK)
	if res.Data["name"] != "Curry chicken" || obj(t, res.Data["chef"])["name"] != "Test Kitchen" || len(list(t, res.Data["ingredients"])) != 1 {
		t.Fatalf("detail = %v", res.Data)
	}
	// Allergens unconfirmed by default; never "free from"; notice always present.
	al := obj(t, res.Data["allergens"])
	if al["status"] != "unconfirmed" || len(list(t, al["contains"])) != 0 || !strings.Contains(al["notice"].(string), "cross-contact") {
		t.Errorf("allergens = %v", al)
	}
	if res.Data["nutrition"] != nil || res.Data["nutrition_detail"] != nil {
		t.Errorf("unverified nutrition shown: %v", res.Data["nutrition"])
	}

	// Unpublishing hides it again.
	h.wantStatus(h.do("POST", "/api/v1/admin/catalog/meals/"+draftID+"/unpublish", nil, k.admin), http.StatusOK)
	h.wantError(h.do("GET", "/api/v1/meals/"+draftSlug, nil), http.StatusNotFound, "not_found")

	// Public responses carry no staff or customer data.
	k.publish(draftID)
	body := rawJSON(t, h.do("GET", "/api/v1/meals", nil))
	for _, secret := range []string{"admin@mise.test", "created_by", "updated_by", "version"} {
		if strings.Contains(body, secret) {
			t.Errorf("public catalogue contains %q", secret)
		}
	}
}

func TestDemoContentStaysOutOfProduction(t *testing.T) {
	h := newHarness(t) // demo disabled, as in production
	k := newKitchen(t, h)
	id, slug := k.create("Demo oxtail", map[string]any{"is_demo": true})
	h.wantError(h.do("POST", "/api/v1/admin/catalog/meals/"+id+"/publish", nil, k.admin), http.StatusConflict, "demo_content")

	// Even if one was published where demo was on, it stays hidden here.
	if _, err := h.pool.Exec(t.Context(), `UPDATE meals SET status = 'published', published_at = now() WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	h.wantError(h.do("GET", "/api/v1/meals/"+slug, nil), http.StatusNotFound, "not_found")
	if got := names(t, h.do("GET", "/api/v1/meals", nil)); len(got) != 0 {
		t.Errorf("demo meal listed: %v", got)
	}

	// The same database served with demo content enabled (local, staging).
	cfg := h.cfg
	cfg.CatalogDemoEnabled = true
	handler, _, err := server.Build(cfg, h.pool, openLimits())
	if err != nil {
		t.Fatal(err)
	}
	demo := *h
	demo.handler = handler
	demo.wantStatus(demo.do("GET", "/api/v1/meals/"+slug, nil), http.StatusOK)
}

func TestCatalogFiltersAndSearch(t *testing.T) {
	h := newHarness(t)
	k := newKitchen(t, h)
	k.published("Curried channa", map[string]any{"dietary_tags": []string{"vegan"}, "protein": "plant", "category_id": k.otherID,
		"allergen_status": "declared", "allergens": []string{}, "prep_minutes": 5, "cook_minutes": 15})
	k.published("Pumpkin soup", map[string]any{"dietary_tags": []string{"vegetarian"}, "protein": "plant", "category_id": k.otherID,
		"allergen_status": "declared", "allergens": []string{"milk"}, "cook_minutes": 40})
	k.published("Jerk chicken", map[string]any{"protein": "chicken", "allergen_status": "declared", "allergens": []string{"peanuts"}, "cook_minutes": 50})
	k.published("Stewed oxtail", map[string]any{"protein": "beef", "cook_minutes": 150, "description": "Browned in burnt sugar and braised."})
	k.create("Draft fish", map[string]any{"protein": "fish", "dietary_tags": []string{"pescatarian"}})

	cases := []struct {
		query string
		want  string
	}{
		{"", "Curried channa,Jerk chicken,Pumpkin soup,Stewed oxtail"},
		// A vegan meal suits a vegetarian; a vegetarian one suits a pescatarian.
		{"diet=vegetarian", "Curried channa,Pumpkin soup"},
		{"diet=vegan", "Curried channa"},
		{"diet=pescatarian", "Curried channa,Pumpkin soup"},
		{"category=lighter", "Curried channa,Pumpkin soup"},
		{"max_minutes=30", "Curried channa"},
		{"protein=chicken", "Jerk chicken"},
		{"chef=test-kitchen", "Curried channa,Jerk chicken,Pumpkin soup,Stewed oxtail"},
		{"q=pumpkin", "Pumpkin soup"},
		{"q=burnt+sugar", "Stewed oxtail"},
		{"q=" + url.QueryEscape("100%_"), ""},
		// Excluding an allergen also drops meals whose allergens are not declared.
		{"exclude_allergens=peanuts", "Curried channa,Pumpkin soup"},
		{"exclude_allergens=peanuts,milk", "Curried channa"},
		{"diet=vegetarian&max_minutes=30", "Curried channa"},
	}
	for _, c := range cases {
		res := h.do("GET", "/api/v1/meals?"+c.query, nil)
		h.wantStatus(res, http.StatusOK)
		if got := strings.Join(names(t, res), ","); got != c.want {
			t.Errorf("?%s = %q, want %q", c.query, got, c.want)
		}
	}
	for _, bad := range []string{"diet=carnivore", "protein=unicorn", "exclude_allergens=gluten", "sort=random", "max_minutes=-5"} {
		h.wantError(h.do("GET", "/api/v1/meals?"+bad, nil), http.StatusUnprocessableEntity, "validation_failed")
	}

	res := h.do("GET", "/api/v1/meals?page_size=2&sort=quickest", nil)
	if res.Data["total"] != float64(4) || len(list(t, res.Data["items"])) != 2 || obj(t, list(t, res.Data["items"])[0])["name"] != "Curried channa" {
		t.Errorf("page 1 = %v", res.Data)
	}
	res = h.do("GET", "/api/v1/meals?page_size=2&page=3", nil)
	if res.Data["total"] != float64(4) || len(list(t, res.Data["items"])) != 0 {
		t.Errorf("past the end = %v", res.Data)
	}

	cats := list(t, h.do("GET", "/api/v1/meal-categories", nil).Data["items"])
	for _, c := range cats {
		if cc := obj(t, c); cc["slug"] == "lighter" && cc["meal_count"] != float64(2) {
			t.Errorf("lighter count = %v", cc["meal_count"])
		}
	}
	chef := h.do("GET", "/api/v1/chefs/test-kitchen", nil)
	h.wantStatus(chef, http.StatusOK)
	if len(list(t, chef.Data["meals"])) != 4 {
		t.Errorf("chef meals = %d", len(list(t, chef.Data["meals"])))
	}
	h.wantStatus(h.do("GET", "/api/v1/reference", nil), http.StatusOK)
}

func TestNutritionIsShownOnlyOnceVerified(t *testing.T) {
	h := newHarness(t)
	k := newKitchen(t, h)
	id, slug := k.published("Curry chicken", nil)
	nutrition := map[string]any{"calories": 640, "protein_g": 42.25, "carbs_g": 55, "fat_g": 21, "verified": false}

	// Only nutrition.verify may enter figures; an admin without it cannot.
	h.wantStatus(h.do("PUT", "/api/v1/admin/catalog/meals/"+id+"/nutrition", nutrition, k.admin), http.StatusForbidden)
	nutritionist := bearer(h.adminLogin("nutrition@mise.test", "nutritionist"))
	h.wantStatus(h.do("PUT", "/api/v1/admin/catalog/meals/"+id+"/nutrition", nutrition, nutritionist), http.StatusOK)
	if res := h.do("GET", "/api/v1/meals/"+slug, nil); res.Data["nutrition"] != nil {
		t.Errorf("pending nutrition shown: %v", res.Data["nutrition"])
	}
	// A nutritionist can verify, not write meals.
	h.wantStatus(h.do("POST", "/api/v1/admin/catalog/meals", k.meal("Sneaky", nil), nutritionist), http.StatusForbidden)

	nutrition["verified"] = true
	h.wantStatus(h.do("PUT", "/api/v1/admin/catalog/meals/"+id+"/nutrition", nutrition, nutritionist), http.StatusOK)
	res := h.do("GET", "/api/v1/meals/"+slug, nil)
	if n := obj(t, res.Data["nutrition"]); n["calories"] != float64(640) || n["protein_g"] != 42.3 {
		t.Errorf("verified nutrition = %v", n)
	}
	if obj(t, res.Data["nutrition_detail"])["verified_at"] == nil {
		t.Error("verified nutrition has no verification time")
	}
}

func TestCatalogAdministrationPermissions(t *testing.T) {
	h := newHarness(t)
	k := newKitchen(t, h)
	customer := bearer(h.customer("eater@example.com"))
	h.wantStatus(h.do("GET", "/api/v1/admin/catalog/meals", nil), http.StatusUnauthorized)
	h.wantStatus(h.do("GET", "/api/v1/admin/catalog/meals", nil, customer), http.StatusUnauthorized)
	h.wantStatus(h.do("POST", "/api/v1/admin/catalog/meals", k.meal("Hack", nil), customer), http.StatusUnauthorized)

	// Marketing writes drafts but does not publish; support sees nothing.
	marketing := bearer(h.adminLogin("marketing@mise.test", "marketing"))
	res := h.do("POST", "/api/v1/admin/catalog/meals", k.meal("Marketing draft", nil), marketing)
	h.wantStatus(res, http.StatusCreated)
	id := res.Data["id"].(string)
	h.wantStatus(h.do("POST", "/api/v1/admin/catalog/meals/"+id+"/publish", nil, marketing), http.StatusForbidden)
	h.wantStatus(h.do("POST", "/api/v1/admin/catalog/chefs", map[string]any{"display_name": "New chef", "status": "published"}, marketing), http.StatusForbidden)
	support := bearer(h.adminLogin("support@mise.test", "support"))
	h.wantStatus(h.do("GET", "/api/v1/admin/catalog/meals", nil, support), http.StatusForbidden)

	// Two editors: the second save of the same version is refused.
	m := h.do("GET", "/api/v1/admin/catalog/meals/"+id, nil, k.admin).Data
	edit := k.meal("Marketing draft", map[string]any{"version": m["version"], "summary": "First edit."})
	h.wantStatus(h.do("PUT", "/api/v1/admin/catalog/meals/"+id, edit, k.admin), http.StatusOK)
	edit["summary"] = "Second edit, from a stale page."
	h.wantError(h.do("PUT", "/api/v1/admin/catalog/meals/"+id, edit, marketing), http.StatusConflict, "version_conflict")

	// Validation: images must be local or on an approved host, with alt text;
	// declared allergens must be marked as declared.
	h.wantError(h.do("POST", "/api/v1/admin/catalog/meals", k.meal("Bad image", map[string]any{"images": []map[string]any{{"src": "http://evil.example/x.jpg", "alt": "x"}}}), k.admin),
		http.StatusUnprocessableEntity, "validation_failed")
	h.wantError(h.do("POST", "/api/v1/admin/catalog/meals", k.meal("No alt", map[string]any{"images": []map[string]any{{"src": "/catalog/x.jpg", "alt": ""}}}), k.admin),
		http.StatusUnprocessableEntity, "validation_failed")
	h.wantError(h.do("POST", "/api/v1/admin/catalog/meals", k.meal("Unsure", map[string]any{"allergens": []string{"milk"}}), k.admin),
		http.StatusUnprocessableEntity, "validation_failed")
	h.wantError(h.do("POST", "/api/v1/admin/catalog/meals", k.meal("Marketing draft", nil), k.admin), http.StatusUnprocessableEntity, "validation_failed")

	if n := h.count(`SELECT count(*) FROM audit_log WHERE action LIKE 'catalog.%'`); n < 5 {
		t.Errorf("catalogue audit entries = %d", n)
	}
}

func TestFavorites(t *testing.T) {
	h := newHarness(t)
	k := newKitchen(t, h)
	soupID, _ := k.published("Pumpkin soup", nil)
	draftID, _ := k.create("Secret draft", nil)
	me := bearer(h.customer("saver@example.com"))
	other := bearer(h.customer("someone@example.com"))

	h.wantStatus(h.do("POST", "/api/v1/me/favorites", map[string]string{"meal_id": soupID}), http.StatusUnauthorized)
	h.wantStatus(h.do("POST", "/api/v1/me/favorites", map[string]string{"meal_id": soupID}, me), http.StatusCreated)
	h.wantStatus(h.do("POST", "/api/v1/me/favorites", map[string]string{"meal_id": soupID}, me), http.StatusOK)
	if n := h.count(`SELECT count(*) FROM customer_favorites`); n != 1 {
		t.Errorf("favourites = %d, want 1 (no duplicates)", n)
	}
	// A draft cannot be saved: to a customer it does not exist.
	h.wantError(h.do("POST", "/api/v1/me/favorites", map[string]string{"meal_id": draftID}, me), http.StatusNotFound, "not_found")
	h.wantError(h.do("POST", "/api/v1/me/favorites", map[string]string{"meal_id": "00000000-0000-0000-0000-000000000000"}, me), http.StatusNotFound, "not_found")

	res := h.do("GET", "/api/v1/me/favorites", nil, me)
	if got := names(t, res); strings.Join(got, ",") != "Pumpkin soup" {
		t.Errorf("favourites = %v", got)
	}
	if got := names(t, h.do("GET", "/api/v1/me/favorites", nil, other)); len(got) != 0 {
		t.Errorf("another customer sees %v", got)
	}

	// Unpublished: hidden from the list, but remembered.
	h.wantStatus(h.do("POST", "/api/v1/admin/catalog/meals/"+soupID+"/unpublish", nil, k.admin), http.StatusOK)
	if got := names(t, h.do("GET", "/api/v1/me/favorites", nil, me)); len(got) != 0 {
		t.Errorf("unpublished favourite listed: %v", got)
	}
	k.publish(soupID)
	if got := names(t, h.do("GET", "/api/v1/me/favorites", nil, me)); len(got) != 1 {
		t.Errorf("republished favourite missing: %v", got)
	}

	// Removing someone else's favourite is impossible: it removes only your own.
	h.wantStatus(h.do("DELETE", "/api/v1/me/favorites/"+soupID, nil, other), http.StatusOK)
	if n := h.count(`SELECT count(*) FROM customer_favorites`); n != 1 {
		t.Errorf("another customer removed a favourite")
	}
	h.wantStatus(h.do("DELETE", "/api/v1/me/favorites/"+soupID, nil, me), http.StatusOK)
	h.wantStatus(h.do("DELETE", "/api/v1/me/favorites/"+soupID, nil, me), http.StatusOK)
	if n := h.count(`SELECT count(*) FROM customer_favorites`); n != 0 {
		t.Errorf("favourites after removal = %d", n)
	}
}

func TestRecommendationsFollowPreferencesAndAllergies(t *testing.T) {
	h := newHarness(t)
	k := newKitchen(t, h)
	k.published("Curried channa", map[string]any{"dietary_tags": []string{"vegan"}, "protein": "plant", "category_id": k.otherID,
		"allergen_status": "declared", "allergens": []string{}, "cook_minutes": 15})
	k.published("Peanut stew", map[string]any{"dietary_tags": []string{"vegan"}, "protein": "plant",
		"allergen_status": "declared", "allergens": []string{"peanuts"}})
	k.published("Pumpkin soup", map[string]any{"dietary_tags": []string{"vegetarian"}, "protein": "plant", "cook_minutes": 40})
	k.published("Jerk chicken", map[string]any{"protein": "chicken", "allergen_status": "declared", "allergens": []string{}})

	me := bearer(h.customer("veg@example.com"))
	res := h.do("GET", "/api/v1/me/recommendations", nil, me)
	h.wantStatus(res, http.StatusOK)
	if res.Data["personalised"] != false || len(list(t, res.Data["items"])) != 4 {
		t.Errorf("before preferences = %v", res.Data)
	}

	h.wantStatus(h.do("PUT", "/api/v1/me/preferences", map[string]any{
		"dietary": []string{"vegetarian"}, "meal_interests": []string{"quick-to-cook"}, "max_cook_minutes": 30,
		"allergies": []map[string]any{{"allergen": "peanuts"}},
	}, me), http.StatusOK)
	res = h.do("GET", "/api/v1/me/recommendations", nil, me)
	var got []string
	for _, it := range list(t, res.Data["items"]) {
		got = append(got, obj(t, obj(t, it)["meal"])["name"].(string))
	}
	// Vegetarian only; the peanut meal never appears; the quick one leads.
	if strings.Join(got, ",") != "Curried channa,Pumpkin soup" {
		t.Errorf("recommendations = %v", got)
	}
	reasons := strs(obj(t, list(t, res.Data["items"])[0])["reasons"])
	if !slices.Contains(reasons, "Suits your vegetarian preference") || !slices.Contains(reasons, "Ready in 30 minutes or less") {
		t.Errorf("reasons = %v", reasons)
	}
	if res.Data["personalised"] != true || !strings.Contains(res.Data["notice"].(string), "does not guarantee") {
		t.Errorf("recommendation notice = %v", res.Data["notice"])
	}
}
