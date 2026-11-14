package server_test

import (
	"net/http"
	"testing"
)

// Sprint 6 integration tests: suppliers and farmers, qualification,
// products mapped to the ingredient catalogue, price history and
// availability; procurement, purchasing, receiving, inventory and
// traceability follow in supply_chain_test.go.

type supplyTeam struct {
	manager, officer, coordinator, stores, receiver, cook, finance, director reqOpt
}

func newSupplyTeam(h *harness) supplyTeam {
	return supplyTeam{
		manager:     bearer(h.adminLogin("buyer-boss@mise.test", "procurement_manager")),
		officer:     bearer(h.adminLogin("buyer@mise.test", "procurement_officer")),
		coordinator: bearer(h.adminLogin("suppliers@mise.test", "supplier_coordinator")),
		stores:      bearer(h.adminLogin("stores@mise.test", "inventory_manager")),
		receiver:    bearer(h.adminLogin("dock@mise.test", "receiving_staff")),
		cook:        bearer(h.adminLogin("line-cook@mise.test", "kitchen_staff")),
		finance:     bearer(h.adminLogin("money@mise.test", "finance")),
		director:    bearer(h.adminLogin("chef-director@mise.test", "culinary_director")),
	}
}

// criterion adds a qualification criterion from "the approved procedure".
func (h *harness) criterion(as reqOpt, name string, types ...string) string {
	h.t.Helper()
	if types == nil {
		types = []string{}
	}
	res := h.do("POST", "/api/v1/admin/supplier-criteria", map[string]any{"name": name, "supplier_types": types, "procedure_ref": "SOP-PUR-1"}, as)
	h.wantStatus(res, http.StatusCreated)
	for _, c := range list(h.t, res.Data["items"]) {
		if obj(h.t, c)["name"] == name {
			return obj(h.t, c)["id"].(string)
		}
	}
	h.t.Fatalf("criterion %q not saved", name)
	return ""
}

// approvedSupplier creates a supplier, submits it as one person and approves
// it as another, meeting every criterion that applies.
func (h *harness) approvedSupplier(submitter, approver reqOpt, name, typ string) string {
	h.t.Helper()
	res := h.do("POST", "/api/v1/admin/suppliers", map[string]any{"name": name, "type": typ, "phone": "+1 868 555 0100", "preferred_channel": "whatsapp"}, submitter)
	h.wantStatus(res, http.StatusCreated)
	id := res.Data["id"].(string)
	h.wantStatus(h.do("POST", "/api/v1/admin/suppliers/"+id+"/submit", nil, submitter), http.StatusOK)
	res = h.do("GET", "/api/v1/admin/suppliers/"+id, nil, approver)
	var results []map[string]any
	for _, c := range list(h.t, res.Data["criteria"]) {
		results = append(results, map[string]any{"criterion_id": obj(h.t, c)["id"], "met": true})
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/suppliers/"+id+"/review", map[string]any{"decision": "approved", "results": results}, approver), http.StatusOK)
	return id
}

func TestSupplierDirectoryQualificationAndFarms(t *testing.T) {
	h := newHarness(t)
	h.clock.Set(at("2026-11-11 10:00"))
	team := newSupplyTeam(h)

	// Informal farmers need little to be recorded.
	res := h.do("POST", "/api/v1/admin/suppliers", map[string]any{"name": "Hosein Family Farm", "type": "farmer"}, team.coordinator)
	h.wantStatus(res, http.StatusCreated)
	farmer := res.Data["id"].(string)
	if res.Data["status"] != "draft" || res.Data["qualified"] != false {
		t.Fatalf("new supplier = %v", res.Data)
	}
	h.wantError(h.do("POST", "/api/v1/admin/suppliers", map[string]any{"name": "  hosein family   FARM", "type": "farmer"}, team.coordinator), http.StatusConflict, "duplicate_supplier")
	// Receiving staff neither edit nor read the directory (contacts are protected).
	h.wantStatus(h.do("POST", "/api/v1/admin/suppliers", map[string]any{"name": "X", "type": "other"}, team.receiver), http.StatusForbidden)
	h.wantStatus(h.do("GET", "/api/v1/admin/suppliers/"+farmer, nil, team.receiver), http.StatusForbidden)

	// The farm profile, recorded on the farmer's behalf.
	res = h.do("PUT", "/api/v1/admin/suppliers/"+farmer+"/farm", map[string]any{"farm_name": "Hosein Garden", "farm_location": "Felicity, Chaguanas",
		"crops": []string{"Tomato", "Pimento", "tomato "}, "harvest_windows": "Tomatoes most weeks; pimento every second week",
		"information_source": "Told to us by phone"}, team.coordinator)
	h.wantStatus(res, http.StatusOK)
	farm := obj(t, res.Data["farm"])
	if len(list(t, farm["crops"])) != 2 || farm["recorded_by_name"] != "Test Admin" {
		t.Errorf("farm = %v", farm)
	}
	res = h.do("POST", "/api/v1/admin/suppliers", map[string]any{"name": "Island Packaging Ltd", "type": "packaging", "email": "orders@example.com"}, team.coordinator)
	packager := res.Data["id"].(string)
	h.wantError(h.do("PUT", "/api/v1/admin/suppliers/"+packager+"/farm", map[string]any{"farm_name": "x"}, team.coordinator), http.StatusUnprocessableEntity, "not_a_farm")

	// Submission needs a way to reach them.
	h.wantError(h.do("POST", "/api/v1/admin/suppliers/"+farmer+"/submit", nil, team.coordinator), http.StatusUnprocessableEntity, "incomplete")
	h.wantStatus(h.do("PATCH", "/api/v1/admin/suppliers/"+farmer, map[string]any{"phone": "+1 868 555 0101", "preferred_channel": "whatsapp"}, team.coordinator), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/suppliers/"+farmer+"/submit", nil, team.coordinator), http.StatusOK)
	// Coordinators do not approve.
	h.wantStatus(h.do("POST", "/api/v1/admin/suppliers/"+farmer+"/review", map[string]any{"decision": "approved"}, team.coordinator), http.StatusForbidden)
	// With no criteria from the business there is nothing to approve against.
	h.wantError(h.do("POST", "/api/v1/admin/suppliers/"+farmer+"/review", map[string]any{"decision": "approved"}, team.manager), http.StatusUnprocessableEntity, "no_criteria")

	// Criteria come from the approved procedure (procurement.configure).
	h.wantStatus(h.do("POST", "/api/v1/admin/supplier-criteria", map[string]any{"name": "x"}, team.coordinator), http.StatusForbidden)
	agreement := h.criterion(team.manager, "Supply agreement signed")
	visit := h.criterion(team.manager, "Farm visit completed", "farmer", "cooperative")
	res = h.do("GET", "/api/v1/admin/suppliers/"+farmer, nil, team.manager)
	if len(list(t, res.Data["criteria"])) != 2 {
		t.Fatalf("criteria for a farmer = %v", res.Data["criteria"])
	}
	res = h.do("GET", "/api/v1/admin/suppliers/"+packager, nil, team.manager)
	if len(list(t, res.Data["criteria"])) != 1 {
		t.Fatalf("criteria for packaging = %v", res.Data["criteria"])
	}

	// A required criterion not met refuses the approval; the supplier is then rejected.
	h.wantError(h.do("POST", "/api/v1/admin/suppliers/"+farmer+"/review", map[string]any{"decision": "approved",
		"results": []map[string]any{{"criterion_id": agreement, "met": true}, {"criterion_id": visit, "met": false}}}, team.manager), http.StatusUnprocessableEntity, "criteria_not_met")
	h.wantStatus(h.do("POST", "/api/v1/admin/suppliers/"+farmer+"/review", map[string]any{"decision": "rejected"}, team.manager), http.StatusUnprocessableEntity)
	res = h.do("POST", "/api/v1/admin/suppliers/"+farmer+"/review", map[string]any{"decision": "rejected", "notes": "Visit not yet done.",
		"results": []map[string]any{{"criterion_id": agreement, "met": true}, {"criterion_id": visit, "met": false, "note": "Booked for next week"}}}, team.manager)
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != "rejected" || res.Data["qualified"] != false {
		t.Fatalf("after rejection = %v", res.Data["status"])
	}

	// Resubmitted after the visit; whoever submits cannot approve.
	h.wantStatus(h.do("POST", "/api/v1/admin/suppliers/"+farmer+"/submit", map[string]any{"note": "Visit done 12 Nov"}, team.manager), http.StatusOK)
	approve := map[string]any{"decision": "approved", "expires_on": "2027-11-11",
		"results": []map[string]any{{"criterion_id": agreement, "met": true}, {"criterion_id": visit, "met": true}}}
	h.wantError(h.do("POST", "/api/v1/admin/suppliers/"+farmer+"/review", approve, team.manager), http.StatusForbidden, "own_submission")
	boss2 := bearer(h.adminLogin("buyer-boss2@mise.test", "procurement_manager"))
	res = h.do("POST", "/api/v1/admin/suppliers/"+farmer+"/review", approve, boss2)
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != "approved" || res.Data["qualified"] != true || res.Data["review_expires_on"] != "2027-11-11" {
		t.Fatalf("after approval = %v", res.Data)
	}
	if n := len(list(t, res.Data["history"])); n != 5 {
		t.Errorf("status history = %d entries", n)
	}
	if n := h.count(`SELECT count(*) FROM domain_events WHERE type = 'supplier.approved' AND aggregate_id = $1`, farmer); n != 1 {
		t.Errorf("approved events = %d", n)
	}

	// A certification counts only once someone verified it.
	res = h.do("POST", "/api/v1/admin/suppliers/"+farmer+"/documents", map[string]any{"kind": "certification", "title": "Good agricultural practice certificate",
		"issued_by": "Ministry of Agriculture", "expires_on": "2027-06-30"}, team.coordinator)
	h.wantStatus(res, http.StatusCreated)
	doc := obj(t, list(t, res.Data["documents"])[0])
	if doc["verified_by_name"] != nil {
		t.Errorf("document verified on entry: %v", doc)
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/supplier-documents/"+doc["id"].(string)+"/verify", nil, team.coordinator), http.StatusForbidden)
	res = h.do("POST", "/api/v1/admin/supplier-documents/"+doc["id"].(string)+"/verify", nil, team.manager)
	h.wantStatus(res, http.StatusOK)
	if obj(t, list(t, res.Data["documents"])[0])["verified_by_name"] == nil {
		t.Error("document not verified")
	}

	// What was said, through which channel. Nothing is sent.
	res = h.do("POST", "/api/v1/admin/suppliers/"+farmer+"/communications", map[string]any{"channel": "whatsapp", "direction": "outbound",
		"summary": "Asked about tomato volumes for December."}, team.coordinator)
	h.wantStatus(res, http.StatusCreated)
	if len(list(t, res.Data["communications"])) != 1 {
		t.Errorf("communications = %v", res.Data["communications"])
	}

	// Suspension stops ordering and tells purchasing.
	h.wantStatus(h.do("POST", "/api/v1/admin/suppliers/"+farmer+"/suspend", map[string]any{}, team.manager), http.StatusUnprocessableEntity)
	res = h.do("POST", "/api/v1/admin/suppliers/"+farmer+"/suspend", map[string]any{"reason": "Two late deliveries in a row."}, team.manager)
	h.wantStatus(res, http.StatusOK)
	if res.Data["qualified"] != false {
		t.Error("a suspended supplier is still qualified")
	}
	if n := h.count(`SELECT count(*) FROM admin_alerts WHERE kind = 'supplier_suspended'`); n != 1 {
		t.Errorf("suspension alerts = %d", n)
	}
	res = h.do("POST", "/api/v1/admin/suppliers/"+farmer+"/reactivate", map[string]any{"reason": "Delivery schedule agreed."}, team.manager)
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != "pending_review" {
		t.Errorf("reactivated = %v", res.Data["status"])
	}
	// Directory search and the farmers view.
	res = h.do("GET", "/api/v1/admin/suppliers?farmers=true", nil, team.officer)
	if res.Data["total"] != float64(1) {
		t.Errorf("farmers = %v", res.Data["total"])
	}
	res = h.do("GET", "/api/v1/admin/suppliers?q=island", nil, team.officer)
	if res.Data["total"] != float64(1) {
		t.Errorf("search = %v", res.Data["total"])
	}
	if n := h.count(`SELECT count(*) FROM audit_log WHERE action LIKE 'supplier.%'`); n < 10 {
		t.Errorf("supplier audit entries = %d", n)
	}
}

func TestSupplierProductsMappingPricesAndAvailability(t *testing.T) {
	h := newHarness(t)
	h.clock.Set(at("2026-11-11 10:00"))
	team := newSupplyTeam(h)
	h.criterion(team.manager, "Supply agreement signed")
	tomato := h.ingredient(team.director, map[string]any{"name": "Tomato", "category": "produce", "base_unit": "g"})
	onion := h.ingredient(team.director, map[string]any{"name": "Onion", "category": "produce", "base_unit": "g"})
	farm := h.approvedSupplier(team.coordinator, team.manager, "Hosein Family Farm", "farmer")
	res := h.do("POST", "/api/v1/admin/suppliers", map[string]any{"name": "Draft Distributors", "type": "distributor"}, team.coordinator)
	draft := res.Data["id"].(string)

	// A 10 kg crate of tomatoes is the catalogue's Tomato, in grams.
	res = h.do("POST", "/api/v1/admin/suppliers/"+farm+"/products", map[string]any{"ingredient_id": tomato, "name": "Roma tomatoes", "sku": "TOM-10",
		"grade": "Grade A", "pack_quantity": "10", "pack_unit": "kg", "pack_description": "10 kg crate", "min_order_packs": "2", "lead_time_days": 2}, team.coordinator)
	h.wantStatus(res, http.StatusCreated)
	crate := res.Data["id"].(string)
	if res.Data["pack_base"] != "10000" || res.Data["mapping_status"] != "proposed" || res.Data["price"] != nil {
		t.Fatalf("product = %v", res.Data)
	}
	// Incompatible mappings are refused: litres of tomato, onions by the piece without a recorded conversion.
	h.wantError(h.do("POST", "/api/v1/admin/suppliers/"+farm+"/products", map[string]any{"ingredient_id": tomato, "name": "Tomato juice", "pack_quantity": "1", "pack_unit": "l"}, team.coordinator),
		http.StatusUnprocessableEntity, "incompatible_mapping")
	h.wantError(h.do("POST", "/api/v1/admin/suppliers/"+farm+"/products", map[string]any{"ingredient_id": onion, "name": "Onions", "pack_quantity": "20", "pack_unit": "piece"}, team.coordinator),
		http.StatusUnprocessableEntity, "incompatible_mapping")
	h.wantError(h.do("POST", "/api/v1/admin/suppliers/"+farm+"/products", map[string]any{"ingredient_id": tomato, "name": "Dup", "sku": "tom-10", "pack_quantity": "1", "pack_unit": "kg"}, team.coordinator),
		http.StatusConflict, "duplicate_sku")
	// Packaging items have no ingredient.
	h.wantStatus(h.do("POST", "/api/v1/admin/suppliers/"+farm+"/products", map[string]any{"kind": "packaging", "name": "Insulated bag", "pack_quantity": "50", "pack_unit": "piece"}, team.coordinator),
		http.StatusCreated)
	// Products of unapproved suppliers are recorded but never purchasable.
	h.wantStatus(h.do("POST", "/api/v1/admin/suppliers/"+draft+"/products", map[string]any{"ingredient_id": tomato, "name": "Tomatoes", "pack_quantity": "5", "pack_unit": "kg"}, team.coordinator),
		http.StatusCreated)

	// Until someone confirms the mapping it is not a purchasing option.
	purchasable := func() int {
		res := h.do("GET", "/api/v1/admin/supplier-products?purchasable=true&ingredient_id="+tomato, nil, team.officer)
		h.wantStatus(res, http.StatusOK)
		return len(list(t, res.Data["items"]))
	}
	if n := purchasable(); n != 0 {
		t.Fatalf("purchasable before review = %d", n)
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/supplier-products/"+crate+"/mapping", map[string]any{"decision": "rejected"}, team.coordinator), http.StatusUnprocessableEntity)
	res = h.do("POST", "/api/v1/admin/supplier-products/"+crate+"/mapping", map[string]any{"decision": "confirmed", "note": "Same variety as the recipes use."}, team.coordinator)
	h.wantStatus(res, http.StatusOK)
	if res.Data["mapping_status"] != "confirmed" || res.Data["mapping_reviewer_name"] == nil {
		t.Fatalf("mapping = %v", res.Data)
	}
	for _, p := range list(t, h.do("GET", "/api/v1/admin/supplier-products?mapping=proposed", nil, team.officer).Data["items"]) {
		h.wantStatus(h.do("POST", "/api/v1/admin/supplier-products/"+obj(t, p)["id"].(string)+"/mapping", map[string]any{"decision": "confirmed"}, team.coordinator), http.StatusOK)
	}
	if n := purchasable(); n != 1 {
		t.Fatalf("purchasable after review = %d (the draft supplier's product must not count)", n)
	}

	// Prices are a history: a new price never overwrites an old one.
	h.wantStatus(h.do("POST", "/api/v1/admin/supplier-products/"+crate+"/prices", map[string]any{"amount_minor": 12000, "currency": "TTD", "valid_from": "2026-11-01", "origin": "price_list"}, team.receiver),
		http.StatusForbidden)
	h.wantStatus(h.do("POST", "/api/v1/admin/supplier-products/"+crate+"/prices", map[string]any{"amount_minor": 12000, "currency": "TTD", "valid_from": "2026-11-01", "origin": "price_list"}, team.coordinator),
		http.StatusCreated)
	res = h.do("POST", "/api/v1/admin/supplier-products/"+crate+"/prices", map[string]any{"amount_minor": 13500, "currency": "TTD", "valid_from": "2026-11-20",
		"origin": "verbal", "note": "Rainy season price"}, team.coordinator)
	h.wantStatus(res, http.StatusCreated)
	res = h.do("GET", "/api/v1/admin/supplier-products/"+crate, nil, team.officer)
	if obj(t, res.Data["price"])["amount_minor"] != float64(12000) || len(list(t, res.Data["prices"])) != 2 {
		t.Fatalf("price today = %v, history = %v", res.Data["price"], res.Data["prices"])
	}
	h.clock.Set(at("2026-11-21 10:00"))
	res = h.do("GET", "/api/v1/admin/supplier-products/"+crate, nil, team.officer)
	if obj(t, res.Data["price"])["amount_minor"] != float64(13500) {
		t.Fatalf("price on 21 Nov = %v", res.Data["price"])
	}
	// Each price is also a supplier cost reference on the ingredient, never overwritten.
	if n := h.count(`SELECT count(*) FROM ingredient_cost_references WHERE ingredient_id = $1 AND origin = 'supplier_quote'`, tomato); n != 2 {
		t.Errorf("supplier cost references = %d", n)
	}
	if _, err := h.pool.Exec(t.Context(), `UPDATE supplier_prices SET amount_minor = 1`); err == nil {
		t.Error("a price could be overwritten")
	}

	// Changing the pack sends the mapping back for review.
	res = h.do("PATCH", "/api/v1/admin/supplier-products/"+crate, map[string]any{"pack_quantity": "12"}, team.coordinator)
	h.wantStatus(res, http.StatusOK)
	if res.Data["mapping_status"] != "proposed" || res.Data["pack_base"] != "12000" {
		t.Errorf("after a pack change = %v %v", res.Data["mapping_status"], res.Data["pack_base"])
	}
	// Other edits keep it.
	h.wantStatus(h.do("POST", "/api/v1/admin/supplier-products/"+crate+"/mapping", map[string]any{"decision": "confirmed"}, team.coordinator), http.StatusOK)
	res = h.do("PATCH", "/api/v1/admin/supplier-products/"+crate, map[string]any{"availability": "limited", "lead_time_days": 3}, team.coordinator)
	if res.Data["mapping_status"] != "confirmed" || res.Data["lead_time_days"] != float64(3) {
		t.Errorf("after a plain edit = %v", res.Data)
	}

	// Availability is what the farmer said, not stock.
	res = h.do("POST", "/api/v1/admin/supplier-availability", map[string]any{"supplier_id": farm, "ingredient_id": tomato, "status": "expected_harvest",
		"estimated_quantity": "300", "unit": "kg", "available_from": "2026-12-01", "available_until": "2026-12-20", "confidence": "supplier_estimate",
		"source": "Phone call"}, team.coordinator)
	h.wantStatus(res, http.StatusCreated)
	h.wantStatus(h.do("POST", "/api/v1/admin/supplier-availability", map[string]any{"supplier_id": farm, "ingredient_id": tomato, "status": "expected_harvest",
		"estimated_quantity": "300", "confidence": "supplier_estimate"}, team.coordinator), http.StatusUnprocessableEntity)
	h.wantStatus(h.do("POST", "/api/v1/admin/supplier-availability", map[string]any{"supplier_id": farm, "ingredient_id": tomato, "status": "available_now",
		"confidence": "confirmed_by_supplier"}, team.coordinator), http.StatusCreated)
	res = h.do("GET", "/api/v1/admin/supplier-availability?ingredient_id="+tomato, nil, team.officer)
	if items := list(t, res.Data["items"]); len(items) != 1 || obj(t, items[0])["status"] != "available_now" {
		t.Errorf("current availability = %v", res.Data["items"])
	}
	res = h.do("GET", "/api/v1/admin/supplier-availability?current=false&ingredient_id="+tomato, nil, team.officer)
	if len(list(t, res.Data["items"])) != 2 {
		t.Errorf("availability history = %v", res.Data["items"])
	}
	if n := h.count(`SELECT count(*) FROM inventory_lots`); n != 0 {
		t.Errorf("availability created stock: %d lots", n)
	}
	// Receiving staff do not see prices.
	h.wantStatus(h.do("GET", "/api/v1/admin/supplier-products/"+crate, nil, team.receiver), http.StatusForbidden)
}
