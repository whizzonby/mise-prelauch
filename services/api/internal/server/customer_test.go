package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Sprint 2 integration tests: the customer's own records. Every test runs
// against PostgreSQL and goes through the HTTP handler, as the apps do.

func list(t *testing.T, v any) []any {
	t.Helper()
	l, ok := v.([]any)
	if !ok {
		t.Fatalf("not a list: %#v", v)
	}
	return l
}

func obj(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("not an object: %#v", v)
	}
	return m
}

func strs(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func TestCustomerRecordsNeedACustomerSession(t *testing.T) {
	h := newHarness(t)
	admin := bearer(h.adminLogin("admin@mise.test", "admin"))
	for _, path := range []string{"/api/v1/me/profile", "/api/v1/me/household", "/api/v1/me/preferences", "/api/v1/me/onboarding",
		"/api/v1/me/notification-preferences", "/api/v1/me/addresses", "/api/v1/me/favorites", "/api/v1/me/recommendations"} {
		h.wantStatus(h.do("GET", path, nil), http.StatusUnauthorized)
		h.wantStatus(h.do("GET", path, nil, bearer("not-a-session")), http.StatusUnauthorized)
		// A staff session is a different audience and opens nothing here.
		h.wantStatus(h.do("GET", path, nil, admin), http.StatusUnauthorized)
	}
}

func TestProfileUpdate(t *testing.T) {
	h := newHarness(t)
	me := bearer(h.customer("kavita@example.com"))

	res := h.do("GET", "/api/v1/me/profile", nil, me)
	h.wantStatus(res, http.StatusOK)
	if res.Data["email"] != "kavita@example.com" || res.Data["contact_channel"] != "email" || res.Data["delivery_availability"] != "unknown" {
		t.Fatalf("profile = %v", res.Data)
	}

	h.wantError(h.do("PATCH", "/api/v1/me/profile", map[string]any{"contact_channel": "whatsapp"}, me), http.StatusUnprocessableEntity, "validation_failed")
	h.wantError(h.do("PATCH", "/api/v1/me/profile", map[string]any{"phone": "call me"}, me), http.StatusUnprocessableEntity, "validation_failed")
	h.wantError(h.do("PATCH", "/api/v1/me/profile", map[string]any{"delivery_region": "atlantis"}, me), http.StatusUnprocessableEntity, "validation_failed")
	h.wantError(h.do("PATCH", "/api/v1/me/profile", map[string]any{"first_name": "  "}, me), http.StatusUnprocessableEntity, "validation_failed")
	// Privileged fields are not part of the profile at all.
	h.wantError(h.do("PATCH", "/api/v1/me/profile", map[string]any{"email_verified": true}, me), http.StatusBadRequest, "invalid_json")
	h.wantError(h.do("PATCH", "/api/v1/me/profile", map[string]any{"roles": []string{"admin"}}, me), http.StatusBadRequest, "invalid_json")

	res = h.do("PATCH", "/api/v1/me/profile", map[string]any{
		"first_name": "Kavi", "phone": "+1 (868) 555-0123", "contact_channel": "whatsapp",
		"delivery_country": "TT", "delivery_region": "chaguanas",
	}, me)
	h.wantStatus(res, http.StatusOK)
	if res.Data["first_name"] != "Kavi" || res.Data["last_name"] != "Ramdass" || res.Data["phone"] != "+18685550123" || res.Data["delivery_availability"] != "unknown" {
		t.Fatalf("updated profile = %v", res.Data)
	}
	// The name is the identity's, so the session's view changes too.
	if got := h.do("GET", "/api/v1/me", nil, me).Data["first_name"]; got != "Kavi" {
		t.Errorf("me.first_name = %v", got)
	}
	// With a zone configured, availability follows it. Without one, never "available".
	if _, err := h.pool.Exec(context.Background(), `INSERT INTO delivery_zones (country, region, status) VALUES ('TT', 'chaguanas', 'planned')`); err != nil {
		t.Fatal(err)
	}
	if got := h.do("GET", "/api/v1/me/profile", nil, me).Data["delivery_availability"]; got != "planned" {
		t.Errorf("availability = %v, want planned", got)
	}
	if n := h.count(`SELECT count(*) FROM audit_log WHERE action = 'customer.profile_updated' AND NOT metadata::text LIKE '%555%'`); n != 1 {
		t.Errorf("profile audit entries without the phone number = %d, want 1", n)
	}
}

func TestPasswordChange(t *testing.T) {
	h := newHarness(t)
	first := h.customer("asha@example.com")
	second := h.customerLogin("asha@example.com", customerPassword)

	h.wantError(h.do("PUT", "/api/v1/me/password", map[string]string{"current_password": "wrong one entirely", "new_password": "a brand new long password"}, bearer(first)),
		http.StatusUnprocessableEntity, "validation_failed")
	h.wantError(h.do("PUT", "/api/v1/me/password", map[string]string{"current_password": customerPassword, "new_password": "short"}, bearer(first)),
		http.StatusUnprocessableEntity, "validation_failed")
	h.wantStatus(h.do("PUT", "/api/v1/me/password", map[string]string{"current_password": customerPassword, "new_password": "a brand new long password"}, bearer(first)), http.StatusOK)

	// This session continues; every other one ends.
	h.wantStatus(h.do("GET", "/api/v1/me", nil, bearer(first)), http.StatusOK)
	h.wantStatus(h.do("GET", "/api/v1/me", nil, bearer(second)), http.StatusUnauthorized)
	h.wantStatus(h.do("POST", "/api/v1/auth/login", map[string]string{"email": "asha@example.com", "password": customerPassword}), http.StatusUnauthorized)
	h.customerLogin("asha@example.com", "a brand new long password")

	h.drainJobs()
	if msg := h.mailer.lastTo(t, "asha@example.com"); msg.Subject != "Your Mise password was changed" {
		t.Errorf("last email = %q", msg.Subject)
	}
}

func TestEmailChange(t *testing.T) {
	h := newHarness(t)
	me := bearer(h.customer("old@example.com"))
	h.customer("taken@example.com")

	h.wantError(h.do("POST", "/api/v1/me/email", map[string]string{"new_email": "new@example.com", "current_password": "not my password"}, me),
		http.StatusUnprocessableEntity, "validation_failed")
	h.wantError(h.do("POST", "/api/v1/me/email", map[string]string{"new_email": "old@example.com", "current_password": customerPassword}, me),
		http.StatusUnprocessableEntity, "validation_failed")

	// An address that belongs to someone else gets the same answer; that
	// address is told by email, and no link is sent anywhere.
	before := h.mailer.countTo("taken@example.com")
	h.wantStatus(h.do("POST", "/api/v1/me/email", map[string]string{"new_email": "taken@example.com", "current_password": customerPassword}, me), http.StatusAccepted)
	h.drainJobs()
	if h.mailer.countTo("taken@example.com") != before+1 || h.mailer.lastTo(t, "taken@example.com").Subject != "This address already has a Mise account" {
		t.Errorf("in-use notice not sent: %q", h.mailer.lastTo(t, "taken@example.com").Subject)
	}

	h.wantStatus(h.do("POST", "/api/v1/me/email", map[string]string{"new_email": "New@Example.com", "current_password": customerPassword}, me), http.StatusAccepted)
	h.drainJobs()
	notice := h.mailer.lastTo(t, "old@example.com")
	if notice.Subject != "Your Mise email address is being changed" || !strings.Contains(notice.Text, "n•••@example.com") || strings.Contains(notice.Text, "new@example.com") {
		t.Errorf("notice to the old address: %q\n%s", notice.Subject, notice.Text)
	}
	if got := h.do("GET", "/api/v1/me/profile", nil, me).Data["pending_email"]; got != "new@example.com" {
		t.Errorf("pending_email = %v", got)
	}
	// Nothing changes until the link is followed.
	h.customerLogin("old@example.com", customerPassword)

	tok := h.accountLink("new@example.com", "/confirm-email-change")
	h.wantStatus(h.do("POST", "/api/v1/auth/confirm-email-change", map[string]string{"token": tok}), http.StatusOK)
	h.wantError(h.do("POST", "/api/v1/auth/confirm-email-change", map[string]string{"token": tok}), http.StatusBadRequest, "token_invalid")

	res := h.do("GET", "/api/v1/me/profile", nil, me)
	if res.Data["email"] != "new@example.com" || res.Data["pending_email"] != nil || res.Data["email_verified"] != true {
		t.Errorf("profile after change = %v", res.Data)
	}
	h.wantStatus(h.do("POST", "/api/v1/auth/login", map[string]string{"email": "old@example.com", "password": customerPassword}), http.StatusUnauthorized)
	h.customerLogin("new@example.com", customerPassword)
}

func TestHouseholdPreferencesAndAllergies(t *testing.T) {
	h := newHarness(t)
	me := bearer(h.customer("family@example.com"))

	if res := h.do("GET", "/api/v1/me/household", nil, me); res.Data["household"] != nil {
		t.Fatalf("new household = %v", res.Data)
	}
	h.wantError(h.do("PUT", "/api/v1/me/household", map[string]any{"size": 0}, me), http.StatusUnprocessableEntity, "validation_failed")
	h.wantError(h.do("PUT", "/api/v1/me/household", map[string]any{"size": 2, "adults": 2, "children": 1}, me), http.StatusUnprocessableEntity, "validation_failed")
	res := h.do("PUT", "/api/v1/me/household", map[string]any{"name": "The Ramdass table", "size": 4, "adults": 2, "children": 2}, me)
	h.wantStatus(res, http.StatusOK)
	if hh := obj(t, res.Data["household"]); hh["size"] != float64(4) || hh["children"] != float64(2) {
		t.Errorf("household = %v", hh)
	}

	// Preferences that cannot all be true are refused, with a reason.
	for _, bad := range [][]string{{"vegan", "pescatarian"}, {"vegetarian", "pescatarian"}, {"no-preference", "vegan"}, {"carnivore"}} {
		res := h.do("PUT", "/api/v1/me/preferences", map[string]any{"dietary": bad}, me)
		h.wantError(res, http.StatusUnprocessableEntity, "validation_failed")
	}
	res = h.do("PUT", "/api/v1/me/preferences", map[string]any{"dietary": []string{"no-preference"}}, me)
	h.wantStatus(res, http.StatusOK)
	if len(list(t, res.Data["dietary"])) != 0 {
		t.Errorf("no-preference stored as %v", res.Data["dietary"])
	}

	// Allergies: "other" needs a description; nothing is answered until asked.
	if res := h.do("GET", "/api/v1/me/preferences", nil, me); res.Data["allergies_reviewed_at"] != nil {
		t.Errorf("allergies reviewed before being asked: %v", res.Data)
	}
	h.wantError(h.do("PUT", "/api/v1/me/preferences", map[string]any{"allergies": []map[string]any{{"allergen": "other"}}}, me), http.StatusUnprocessableEntity, "validation_failed")
	res = h.do("PUT", "/api/v1/me/preferences", map[string]any{
		"dietary":   []string{"vegetarian", "gluten-free"},
		"allergies": []map[string]any{{"allergen": "peanuts"}, {"allergen": "other", "detail": "Mango skin"}},
	}, me)
	h.wantStatus(res, http.StatusOK)
	if len(list(t, res.Data["allergies"])) != 2 || res.Data["allergies_reviewed_at"] == nil {
		t.Fatalf("allergies = %v", res.Data)
	}

	// A later step that only sends preferences never touches allergies.
	res = h.do("PUT", "/api/v1/me/preferences", map[string]any{"meal_interests": []string{"quick-to-cook"}, "max_cook_minutes": 30}, me)
	h.wantStatus(res, http.StatusOK)
	if len(list(t, res.Data["allergies"])) != 2 || strings.Join(strs(res.Data["dietary"]), ",") != "vegetarian,gluten-free" {
		t.Errorf("a partial update changed other answers: %v", res.Data)
	}

	// An empty list is an answer: no allergies.
	res = h.do("PUT", "/api/v1/me/preferences", map[string]any{"allergies": []any{}}, me)
	h.wantStatus(res, http.StatusOK)
	if len(list(t, res.Data["allergies"])) != 0 || res.Data["allergies_reviewed_at"] == nil {
		t.Errorf("cleared allergies = %v", res.Data)
	}

	// The audit log records that allergies changed, never which.
	if n := h.count(`SELECT count(*) FROM audit_log WHERE action = 'customer.allergies_updated'`); n != 2 {
		t.Errorf("allergy audit entries = %d, want 2", n)
	}
	if n := h.count(`SELECT count(*) FROM audit_log WHERE metadata::text ILIKE '%peanut%' OR metadata::text ILIKE '%mango%'`); n != 0 {
		t.Error("an allergy was written to the audit log")
	}
}

func TestOnboardingProgress(t *testing.T) {
	h := newHarness(t)
	me := bearer(h.customer("new@example.com"))

	res := h.do("GET", "/api/v1/me/onboarding", nil, me)
	if res.Data["step"] != "welcome" || res.Data["completed_at"] != nil {
		t.Fatalf("fresh onboarding = %v", res.Data)
	}
	h.wantError(h.do("PUT", "/api/v1/me/onboarding", map[string]any{"step": "dessert"}, me), http.StatusUnprocessableEntity, "validation_failed")
	h.wantError(h.do("PUT", "/api/v1/me/onboarding", map[string]any{"step": "dietary", "skipped": []string{"welcome"}}, me), http.StatusUnprocessableEntity, "validation_failed")

	h.wantStatus(h.do("PUT", "/api/v1/me/onboarding", map[string]any{"step": "allergies", "skipped": []string{"interests"}}, me), http.StatusOK)
	// Resuming, in another session, picks up where it was left.
	other := bearer(h.customerLogin("new@example.com", customerPassword))
	res = h.do("GET", "/api/v1/me/onboarding", nil, other)
	if res.Data["step"] != "allergies" || strings.Join(strs(res.Data["skipped"]), ",") != "interests" {
		t.Fatalf("resumed onboarding = %v", res.Data)
	}

	res = h.do("PUT", "/api/v1/me/onboarding", map[string]any{"step": "complete", "skipped": []string{"interests"}}, me)
	h.wantStatus(res, http.StatusOK)
	done := res.Data["completed_at"]
	if done == nil {
		t.Fatal("completing did not record a time")
	}
	// Going back over a step later keeps the completion.
	res = h.do("PUT", "/api/v1/me/onboarding", map[string]any{"step": "complete"}, me)
	if res.Data["completed_at"] != done {
		t.Errorf("completion moved: %v -> %v", done, res.Data["completed_at"])
	}
}

func TestNotificationPreferences(t *testing.T) {
	h := newHarness(t)
	me := bearer(h.customer("mail@example.com"))

	res := h.do("GET", "/api/v1/me/notification-preferences", nil, me)
	h.wantStatus(res, http.StatusOK)
	if sec := obj(t, res.Data["account_security"]); sec["enabled"] != true || sec["required"] != true {
		t.Errorf("account security = %v", sec)
	}
	for _, topic := range list(t, res.Data["topics"]) {
		if obj(t, topic)["enabled"] != false {
			t.Errorf("optional topic on by default without consent: %v", topic)
		}
	}
	h.wantError(h.do("PUT", "/api/v1/me/notification-preferences", map[string]any{"topics": map[string]bool{"account_security": false}}, me), http.StatusUnprocessableEntity, "validation_failed")
	h.wantError(h.do("PUT", "/api/v1/me/notification-preferences", map[string]any{"topics": map[string]bool{"delivery_updates": true}}, me), http.StatusUnprocessableEntity, "validation_failed")
	h.wantError(h.do("PUT", "/api/v1/me/notification-preferences", map[string]any{"topics": map[string]bool{"spam": true}}, me), http.StatusUnprocessableEntity, "validation_failed")

	res = h.do("PUT", "/api/v1/me/notification-preferences", map[string]any{"topics": map[string]bool{"promotions": true, "meal_updates": false}}, me)
	h.wantStatus(res, http.StatusOK)
	for _, topic := range list(t, res.Data["topics"]) {
		tp := obj(t, topic)
		if tp["topic"] == "promotions" && (tp["enabled"] != true || tp["source"] != "customer") {
			t.Errorf("promotions = %v", tp)
		}
	}
	// Each change is a consent record.
	var meta string
	if err := h.pool.QueryRow(context.Background(), `SELECT metadata::text FROM audit_log WHERE action = 'customer.consent_updated'`).Scan(&meta); err != nil {
		t.Fatalf("consent audit: %v", err)
	}
	if !strings.Contains(meta, `"promotions": true`) {
		t.Errorf("consent audit = %s", meta)
	}
}

func TestWaitlistAnswersCarryOverOnceAndNeverOverwrite(t *testing.T) {
	h := newHarness(t)
	// A lead who gave preferences and consent on the waitlist.
	_, profile := h.join("lead@example.com", map[string]any{"dietary_interests": []string{"vegetarian"}, "household_size": 3})
	h.wantStatus(h.do("PATCH", "/api/v1/leads/preferences", map[string]any{"meal_interests": []string{"family", "school-lunch"}}, bearer(profile)), http.StatusOK)

	me := bearer(h.customer("lead@example.com"))
	prefs := h.do("GET", "/api/v1/me/preferences", nil, me).Data
	if strings.Join(strs(prefs["dietary"]), ",") != "vegetarian" || strings.Join(strs(prefs["meal_interests"]), ",") != "family-friendly" || prefs["source"] != "waitlist" {
		t.Errorf("imported preferences = %v", prefs)
	}
	if len(list(t, prefs["allergies"])) != 0 || prefs["allergies_reviewed_at"] != nil {
		t.Errorf("allergies were set by the import: %v", prefs)
	}
	if hh := obj(t, h.do("GET", "/api/v1/me/household", nil, me).Data["household"]); hh["size"] != float64(3) || hh["source"] != "waitlist" {
		t.Errorf("imported household = %v", hh)
	}
	topics := map[string]any{}
	for _, tp := range list(t, h.do("GET", "/api/v1/me/notification-preferences", nil, me).Data["topics"]) {
		topics[obj(t, tp)["topic"].(string)] = obj(t, tp)
	}
	for _, k := range []string{"weekly_menu", "promotions"} {
		if tp := obj(t, topics[k]); tp["enabled"] != true || tp["source"] != "waitlist_consent" {
			t.Errorf("%s = %v, want on from waitlist consent", k, tp)
		}
	}
	if tp := obj(t, topics["meal_updates"]); tp["enabled"] != false {
		t.Errorf("meal_updates turned on without consent: %v", tp)
	}
	if n := h.count(`SELECT count(*) FROM audit_log WHERE action = 'customer.waitlist_imported'`); n != 1 {
		t.Errorf("import audit entries = %d", n)
	}
	// The lead itself is untouched.
	if h.leadStatus("lead@example.com") != "VERIFIED" {
		t.Errorf("lead status changed to %s", h.leadStatus("lead@example.com"))
	}

	// A customer who already answered keeps their answers when a lead is
	// linked later (here through an email change).
	h.join("later@example.com", map[string]any{"dietary_interests": []string{"pescatarian"}, "household_size": 5})
	other := bearer(h.customer("other@example.com"))
	h.wantStatus(h.do("PUT", "/api/v1/me/preferences", map[string]any{"dietary": []string{"vegan"}, "allergies": []map[string]any{{"allergen": "sesame"}}}, other), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/me/email", map[string]string{"new_email": "later@example.com", "current_password": customerPassword}, other), http.StatusAccepted)
	h.wantStatus(h.do("POST", "/api/v1/auth/confirm-email-change", map[string]string{"token": h.accountLink("later@example.com", "/confirm-email-change")}), http.StatusOK)
	prefs = h.do("GET", "/api/v1/me/preferences", nil, other).Data
	if strings.Join(strs(prefs["dietary"]), ",") != "vegan" || len(list(t, prefs["allergies"])) != 1 || prefs["source"] != "customer" {
		t.Errorf("linking overwrote the customer's answers: %v", prefs)
	}
	if hh := obj(t, h.do("GET", "/api/v1/me/household", nil, other).Data["household"]); hh["size"] != float64(5) {
		t.Errorf("missing household not filled from the lead: %v", hh)
	}
	if n := h.count(`SELECT count(*) FROM customers WHERE lead_id IS NOT NULL`); n != 2 {
		t.Errorf("linked customers = %d, want 2", n)
	}
}

func TestUnsubscribedLeadConsentIsNotCarriedOver(t *testing.T) {
	h := newHarness(t)
	h.join("quiet@example.com", nil)
	if _, err := h.pool.Exec(context.Background(), `UPDATE leads SET status = 'UNSUBSCRIBED' WHERE email = 'quiet@example.com'`); err != nil {
		t.Fatal(err)
	}
	me := bearer(h.customer("quiet@example.com"))
	for _, tp := range list(t, h.do("GET", "/api/v1/me/notification-preferences", nil, me).Data["topics"]) {
		if obj(t, tp)["enabled"] != false {
			t.Errorf("topic on for an unsubscribed lead: %v", tp)
		}
	}
}

func address(extra map[string]any) map[string]any {
	a := map[string]any{"recipient_name": "Kavita Ramdass", "line1": "12 Ariapita Avenue", "city": "Woodbrook", "region": "port-of-spain"}
	for k, v := range extra {
		a[k] = v
	}
	return a
}

func TestAddressesOwnershipAndDefault(t *testing.T) {
	h := newHarness(t)
	me := bearer(h.customer("home@example.com"))
	stranger := bearer(h.customer("stranger@example.com"))

	h.wantError(h.do("POST", "/api/v1/me/addresses", map[string]any{"line1": "x"}, me), http.StatusUnprocessableEntity, "validation_failed")
	h.wantError(h.do("POST", "/api/v1/me/addresses", address(map[string]any{"region": "narnia"}), me), http.StatusUnprocessableEntity, "validation_failed")
	h.wantError(h.do("POST", "/api/v1/me/addresses", address(map[string]any{"country": "US"}), me), http.StatusUnprocessableEntity, "validation_failed")

	// The first address is the default even if not asked.
	first := h.do("POST", "/api/v1/me/addresses", address(map[string]any{"label": "Home", "is_default": false}), me)
	h.wantStatus(first, http.StatusCreated)
	if first.Data["is_default"] != true || first.Data["delivery_availability"] != "unknown" || first.Data["region_label"] != "Port of Spain" {
		t.Fatalf("first address = %v", first.Data)
	}
	firstID := first.Data["id"].(string)
	second := h.do("POST", "/api/v1/me/addresses", address(map[string]any{"label": "Office", "region": "san-fernando", "city": "San Fernando", "is_default": true}), me)
	h.wantStatus(second, http.StatusCreated)
	secondID := second.Data["id"].(string)

	items := list(t, h.do("GET", "/api/v1/me/addresses", nil, me).Data["items"])
	if len(items) != 2 || obj(t, items[0])["id"] != secondID || obj(t, items[1])["is_default"] != false {
		t.Fatalf("addresses after a new default = %v", items)
	}
	if n := h.count(`SELECT count(*) FROM customer_addresses WHERE is_default`); n != 1 {
		t.Errorf("default addresses = %d", n)
	}

	// Another customer cannot see, change or delete them: same as not found.
	if items := list(t, h.do("GET", "/api/v1/me/addresses", nil, stranger).Data["items"]); len(items) != 0 {
		t.Errorf("stranger sees %d addresses", len(items))
	}
	h.wantError(h.do("PATCH", "/api/v1/me/addresses/"+firstID, map[string]any{"line1": "Hijacked"}, stranger), http.StatusNotFound, "not_found")
	h.wantError(h.do("DELETE", "/api/v1/me/addresses/"+firstID, nil, stranger), http.StatusNotFound, "not_found")
	h.wantError(h.do("PATCH", "/api/v1/me/addresses/not-an-id", map[string]any{"line1": "x"}, me), http.StatusNotFound, "not_found")

	// The default moves, it is not simply switched off.
	h.wantError(h.do("PATCH", "/api/v1/me/addresses/"+secondID, map[string]any{"is_default": false}, me), http.StatusUnprocessableEntity, "validation_failed")
	res := h.do("PATCH", "/api/v1/me/addresses/"+firstID, map[string]any{"is_default": true, "instructions": "Gate code 1234", "line2": ""}, me)
	h.wantStatus(res, http.StatusOK)
	if res.Data["is_default"] != true || res.Data["instructions"] != "Gate code 1234" || res.Data["line2"] != nil {
		t.Errorf("updated = %v", res.Data)
	}

	// Deleting the default hands it to the oldest remaining address.
	h.wantStatus(h.do("DELETE", "/api/v1/me/addresses/"+firstID, nil, me), http.StatusOK)
	items = list(t, h.do("GET", "/api/v1/me/addresses", nil, me).Data["items"])
	if len(items) != 1 || obj(t, items[0])["is_default"] != true {
		t.Errorf("after deleting the default = %v", items)
	}

	for i := 0; i < 9; i++ {
		h.wantStatus(h.do("POST", "/api/v1/me/addresses", address(nil), me), http.StatusCreated)
	}
	h.wantError(h.do("POST", "/api/v1/me/addresses", address(nil), me), http.StatusConflict, "too_many_addresses")
}

// rawJSON is for checks on a whole response body.
func rawJSON(t *testing.T, res response) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(res.Raw, &v); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(v)
	return string(b)
}
