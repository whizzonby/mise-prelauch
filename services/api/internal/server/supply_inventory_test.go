package server_test

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"mise.tt/api/internal/platform/qty"
)

// stockSetup is a released plan with an approved supplier, an issued
// purchase order for its three ingredients, storage locations and the plan's
// batch: everything receiving needs, made through the real API.
type stockSetup struct {
	s                      *shop
	h                      *harness
	team                   supplyTeam
	plan, batch, run, po   string
	ing                    map[string]string
	lines                  map[string]string // PO line by ingredient short name
	packs                  map[string]string // ordered packs by ingredient short name
	dry, chill, quarantine string
	farm                   string
}

func newStockSetup(t *testing.T) *stockSetup {
	t.Helper()
	s, _, plan, ing := releasedPlan(t)
	h := s.h
	st := &stockSetup{s: s, h: h, plan: plan, ing: ing, team: newSupplyTeam(h), lines: map[string]string{}, packs: map[string]string{}}
	team := st.team
	boss2 := bearer(h.adminLogin("buyer-boss2@mise.test", "procurement_manager"))
	h.criterion(team.manager, "Supply agreement signed")
	st.farm = h.approvedSupplier(team.coordinator, team.manager, "Hosein Family Farm", "farmer")
	products := map[string]string{
		"chicken": h.product(team.coordinator, st.farm, ing["chicken"], "Chicken thighs, 2 kg bag", "2", "kg", 4500, 1),
		"rice":    h.product(team.coordinator, st.farm, ing["rice"], "Parboiled rice, 10 kg sack", "10", "kg", 9000, 1),
		"coconut": h.product(team.coordinator, st.farm, ing["coconut"], "Coconut milk, 1 l carton", "1", "l", 1500, 1),
	}
	res := h.do("GET", "/api/v1/admin/production/batches?plan_id="+plan, nil, team.manager)
	st.batch = obj(t, list(t, res.Data["items"])[0])["id"].(string)

	// Storage as the business set it up (inventory.configure).
	loc := func(code, name, typ string) string {
		res := h.do("POST", "/api/v1/admin/inventory/locations", map[string]any{"code": code, "name": name, "type": typ}, team.stores)
		h.wantStatus(res, http.StatusCreated)
		for _, x := range list(t, res.Data["items"]) {
			if obj(t, x)["code"] == code {
				return obj(t, x)["id"].(string)
			}
		}
		t.Fatalf("location %s not saved", code)
		return ""
	}
	st.dry, st.chill, st.quarantine = loc("DRY-1", "Dry store", "dry"), loc("CHILL-1", "Walk-in chiller", "chilled"), loc("QUAR-1", "Quarantine shelf", "quarantine")

	// Purchasing: one order for all three, approved by someone other than its buyer, issued.
	run, _, err := h.sc.Procurement.CreateRun(context.Background(), "", plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	st.run = run.ID
	for _, l := range run.PlanLines {
		short := map[string]string{ing["chicken"]: "chicken", ing["rice"]: "rice", ing["coconut"]: "coconut"}[l.IngredientID]
		packs := l.Options[0].Packs
		if short == "chicken" {
			packs = "5" // room for the partial deliveries the tests make
		}
		st.packs[short] = packs
		h.wantStatus(h.do("POST", "/api/v1/admin/procurement/runs/"+run.ID+"/lines/"+l.ID+"/decision",
			map[string]any{"decision": "buy", "product_id": products[short], "packs": packs}, team.officer), http.StatusOK)
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/procurement/runs/"+run.ID+"/approve", nil, team.manager), http.StatusOK)
	res = h.do("POST", "/api/v1/admin/procurement/runs/"+run.ID+"/purchase-orders", nil, team.officer)
	st.po = obj(t, list(t, res.Data["items"])[0])["id"].(string)
	poURL := "/api/v1/admin/purchase-orders/" + st.po
	h.wantStatus(h.do("PATCH", poURL, map[string]any{"expected_delivery": "2026-11-14", "location_id": st.dry}, team.officer), http.StatusOK)
	h.wantStatus(h.do("POST", poURL+"/submit", nil, team.officer), http.StatusOK)
	h.wantStatus(h.do("POST", poURL+"/approve", nil, boss2), http.StatusOK)
	res = h.do("POST", poURL+"/issue", map[string]any{"channel": "whatsapp"}, team.manager)
	h.wantStatus(res, http.StatusOK)
	for _, l := range list(t, res.Data["lines"]) {
		line := obj(t, l)
		short := map[string]string{ing["chicken"]: "chicken", ing["rice"]: "rice", ing["coconut"]: "coconut"}[line["ingredient_id"].(string)]
		st.lines[short] = line["id"].(string)
	}
	return st
}

func (st *stockSetup) checklist(t *testing.T) (all, cold string) {
	t.Helper()
	h := st.h
	res := h.do("POST", "/api/v1/admin/receiving/checklist", map[string]any{"name": "Packaging intact", "criteria": "No tears, leaks or crushing (SOP-REC-1).",
		"procedure_ref": "SOP-REC-1"}, st.team.stores)
	h.wantStatus(res, http.StatusCreated)
	all = obj(t, list(t, res.Data["items"])[0])["id"].(string)
	res = h.do("POST", "/api/v1/admin/receiving/checklist", map[string]any{"name": "Delivered chilled", "criteria": "As SOP-REC-2 states for chilled poultry.",
		"categories": []string{"poultry"}, "position": 2}, st.team.stores)
	h.wantStatus(res, http.StatusCreated)
	for _, x := range list(t, res.Data["items"]) {
		if obj(t, x)["name"] == "Delivered chilled" {
			cold = obj(t, x)["id"].(string)
		}
	}
	return
}

func pass(ids ...string) []map[string]any {
	out := []map[string]any{}
	for _, id := range ids {
		out = append(out, map[string]any{"item_id": id, "result": "pass"})
	}
	return out
}

// lotsOf returns the lots of an ingredient by status.
func (st *stockSetup) lotsOf(t *testing.T, ingredient string) map[string][]map[string]any {
	t.Helper()
	res := st.h.do("GET", "/api/v1/admin/inventory/lots?ingredient_id="+ingredient, nil, st.team.stores)
	st.h.wantStatus(res, http.StatusOK)
	out := map[string][]map[string]any{}
	for _, x := range list(t, res.Data["items"]) {
		l := obj(t, x)
		out[l["status"].(string)] = append(out[l["status"].(string)], l)
	}
	return out
}

// ledgerBalances checks every lot's figures equal the sums of its ledger rows.
func (h *harness) ledgerBalances() {
	h.t.Helper()
	if n := h.count(`SELECT count(*) FROM inventory_lots l WHERE l.on_hand <> (SELECT COALESCE(sum(on_hand_delta), 0) FROM inventory_movements m WHERE m.lot_id = l.id)
		OR l.reserved <> (SELECT COALESCE(sum(reserved_delta), 0) FROM inventory_movements m WHERE m.lot_id = l.id)`); n != 0 {
		h.t.Errorf("%d lot(s) differ from their ledger", n)
	}
	if n := h.count(`SELECT count(*) FROM inventory_lots l WHERE l.reserved <> (SELECT COALESCE(sum(open_quantity), 0) FROM inventory_reservations r
		WHERE r.lot_id = l.id AND r.status = 'active')`); n != 0 {
		h.t.Errorf("%d lot(s) differ from their reservations", n)
	}
}

func TestReceivingInspectionLotsAndPartialDeliveries(t *testing.T) {
	st := newStockSetup(t)
	h, team := st.h, st.team
	key := func(n int) string { return fmt.Sprintf("receipt-form-%d-%s", n, st.po[:8]) }
	chickenLine := func(acc, rej string, insp []map[string]any) map[string]any {
		return map[string]any{"po_line_id": st.lines["chicken"], "delivered_packs": qty.MustParse(acc).Add(qty.MustParse(rej)).String(), "accepted_packs": acc,
			"rejected_packs": rej, "supplier_lot_ref": "HF-1113", "expiry_date": "2026-11-20", "location_id": st.chill, "quarantine_location_id": st.quarantine,
			"inspection": insp, "temperature_c": "3.5", "discrepancy": "damaged", "discrepancy_note": "One bag torn in transit."}
	}

	// Kitchen staff do not receive; receiving staff do.
	h.wantStatus(h.do("POST", "/api/v1/admin/receiving", map[string]any{"po_id": st.po}, team.cook), http.StatusForbidden)
	// Without the checklist from the approved procedure nothing can be accepted.
	h.wantError(h.do("POST", "/api/v1/admin/receiving", map[string]any{"po_id": st.po, "idempotency_key": key(1),
		"lines": []map[string]any{chickenLine("2", "1", nil)}}, team.receiver), http.StatusUnprocessableEntity, "no_checklist")
	all, cold := st.checklist(t)
	// Every item recorded; a failed one cannot be accepted.
	h.wantError(h.do("POST", "/api/v1/admin/receiving", map[string]any{"po_id": st.po, "idempotency_key": key(1),
		"lines": []map[string]any{chickenLine("2", "1", pass(all))}}, team.receiver), http.StatusUnprocessableEntity, "inspection_incomplete")
	h.wantError(h.do("POST", "/api/v1/admin/receiving", map[string]any{"po_id": st.po, "idempotency_key": key(1),
		"lines": []map[string]any{chickenLine("2", "1", []map[string]any{{"item_id": all, "result": "pass"}, {"item_id": cold, "result": "fail", "note": "Arrived at 9 °C"}})}},
		team.receiver), http.StatusUnprocessableEntity, "inspection_failed")
	// More rice than ordered: no tolerance is configured.
	over := qty.MustParse(st.packs["rice"]).Add(qty.Int(1)).String()
	h.wantError(h.do("POST", "/api/v1/admin/receiving", map[string]any{"po_id": st.po, "idempotency_key": key(1), "lines": []map[string]any{
		{"po_line_id": st.lines["rice"], "delivered_packs": over, "accepted_packs": over, "location_id": st.dry, "inspection": pass(all)}}}, team.receiver),
		http.StatusUnprocessableEntity, "over_receipt")

	// A partial delivery: two good bags of chicken and a torn one, and all the rice (no date on the sacks).
	body := map[string]any{"po_id": st.po, "idempotency_key": key(1), "delivery_ref": "Delivery note 4471", "lines": []map[string]any{
		chickenLine("2", "1", pass(all, cold)),
		{"po_line_id": st.lines["rice"], "delivered_packs": st.packs["rice"], "accepted_packs": st.packs["rice"], "location_id": st.dry, "inspection": pass(all)},
	}}
	res := h.do("POST", "/api/v1/admin/receiving", body, team.receiver)
	h.wantStatus(res, http.StatusCreated)
	receipt := res.Data["id"].(string)
	// The same form sent again records nothing more.
	res = h.do("POST", "/api/v1/admin/receiving", body, team.receiver)
	h.wantStatus(res, http.StatusOK)
	if res.Data["id"] != receipt || res.Data["replayed"] != true {
		t.Fatalf("replayed receipt = %v", res.Data)
	}
	if n := h.count(`SELECT count(*) FROM goods_receipts`); n != 1 {
		t.Fatalf("receipts = %d", n)
	}
	// Two clerks recording the same delivery at once still record it once.
	var wg sync.WaitGroup
	statuses := make([]int, 2)
	same := map[string]any{"po_id": st.po, "idempotency_key": key(2), "lines": []map[string]any{
		{"po_line_id": st.lines["coconut"], "delivered_packs": "1", "accepted_packs": "1", "expiry_date": "2026-11-10", "location_id": st.dry, "inspection": pass(all)}}}
	for i := range statuses {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses[i] = h.do("POST", "/api/v1/admin/receiving", same, team.receiver).Status
		}()
	}
	wg.Wait()
	if n := h.count(`SELECT count(*) FROM goods_receipts`); n != 2 || !(statuses[0]+statuses[1] == 201+200) {
		t.Fatalf("concurrent receipts = %d (statuses %v)", n, statuses)
	}

	// The order knows what came: partly received, rejected packs not accepted.
	res = h.do("GET", "/api/v1/admin/purchase-orders/"+st.po, nil, team.officer)
	if res.Data["status"] != "partially_received" {
		t.Fatalf("order = %v", res.Data["status"])
	}
	for _, l := range list(t, res.Data["lines"]) {
		line := obj(t, l)
		if line["id"] == st.lines["chicken"] && (line["received_packs"] != "3" || line["accepted_packs"] != "2") {
			t.Errorf("chicken line = %v", line)
		}
	}
	// Lots: accepted chicken usable; the torn bag rejected, in quarantine, never usable; rice waiting for an expiry review.
	chicken := st.lotsOf(t, st.ing["chicken"])
	if len(chicken["available"]) != 1 || chicken["available"][0]["on_hand"] != "4000" || chicken["available"][0]["usable"] != true ||
		chicken["available"][0]["supplier_lot_ref"] != "HF-1113" {
		t.Fatalf("chicken lots = %v", chicken)
	}
	if len(chicken["rejected"]) != 1 || chicken["rejected"][0]["usable"] != false || chicken["rejected"][0]["location_name"] != "Quarantine shelf" {
		t.Fatalf("rejected chicken = %v", chicken["rejected"])
	}
	rice := st.lotsOf(t, st.ing["rice"])["available"][0]
	if rice["usable"] != false || rice["expiry_status"] != "not_recorded" {
		t.Errorf("rice lot = %v", rice)
	}
	coconut := st.lotsOf(t, st.ing["coconut"])["available"][0]
	if coconut["expired"] != true || coconut["usable"] != false || coconut["on_hand"] != "1030" {
		t.Errorf("expired coconut lot = %v", coconut)
	}
	// Stock: every figure apart, incoming never counted as stock.
	res = h.do("GET", "/api/v1/admin/inventory", nil, team.stores)
	var sum map[string]any
	for _, x := range list(t, res.Data["items"]) {
		if obj(t, x)["ingredient_id"] == st.ing["chicken"] {
			sum = obj(t, x)
		}
	}
	if sum["on_hand"] != "6000" || sum["usable"] != "4000" || sum["rejected"] != "2000" || sum["available"] != "4000" || sum["incoming"] == "0" {
		t.Errorf("chicken stock = %v", sum)
	}
	if n := h.count(`SELECT count(*) FROM domain_events WHERE type = 'receiving.inspection_failed'`); n != 1 {
		t.Errorf("inspection failed events = %d", n)
	}
	if n := h.count(`SELECT count(*) FROM admin_alerts WHERE kind = 'receiving_discrepancy'`); n != 1 {
		t.Errorf("discrepancy alerts = %d", n)
	}
	if n := h.count(`SELECT count(*) FROM ingredient_cost_references WHERE origin = 'receipt'`); n != 3 {
		t.Errorf("received cost references = %d", n)
	}
	// Rejected goods go back to the supplier; accepted stock does not.
	h.wantStatus(h.do("POST", "/api/v1/admin/inventory/lots/"+chicken["available"][0]["id"].(string)+"/supplier-return", map[string]any{"quantity": "100", "reason": "x x x"}, team.stores), http.StatusConflict)
	res = h.do("POST", "/api/v1/admin/inventory/lots/"+chicken["rejected"][0]["id"].(string)+"/supplier-return", map[string]any{"quantity": "2000", "reason": "Collected by the farm."}, team.stores)
	h.wantStatus(res, http.StatusOK)
	if res.Data["on_hand"] != "0" {
		t.Errorf("after the return = %v", res.Data["on_hand"])
	}
	// Quarantined stock released only after review; the expiry is recorded, never guessed.
	h.wantStatus(h.do("POST", "/api/v1/admin/inventory/lots/"+rice["id"].(string)+"/expiry", map[string]any{"none_applies": true}, team.receiver), http.StatusUnprocessableEntity)
	res = h.do("POST", "/api/v1/admin/inventory/lots/"+rice["id"].(string)+"/expiry", map[string]any{"none_applies": true,
		"note": "Supplier specification: dry goods, best-before only on the outer sack."}, team.receiver)
	h.wantStatus(res, http.StatusOK)
	if res.Data["usable"] != true {
		t.Errorf("rice after review = %v", res.Data["blocked"])
	}
	h.wantError(h.do("POST", "/api/v1/admin/inventory/lots/"+rice["id"].(string)+"/expiry", map[string]any{"expiry_date": "2027-01-01"}, team.receiver), http.StatusConflict, "expiry_recorded")
	h.ledgerBalances()
	if _, err := h.pool.Exec(context.Background(), `DELETE FROM inventory_movements`); err == nil {
		t.Error("the ledger could be deleted")
	}
	res = h.do("GET", "/api/v1/admin/receiving/"+receipt, nil, team.receiver)
	if len(list(t, res.Data["lines"])) != 2 || len(list(t, obj(t, list(t, res.Data["lines"])[0])["lots"])) == 0 {
		t.Errorf("receipt = %v", res.Data["lines"])
	}
}

func TestStockFromPurchaseToBatchTraceAndReports(t *testing.T) {
	st := newStockSetup(t)
	h, team := st.h, st.team
	all, cold := st.checklist(t)
	// Poultry is allocated first-expiring-first, by an approved policy.
	h.wantStatus(h.do("PUT", "/api/v1/admin/inventory/policies", map[string]any{"category": "poultry", "method": "fefo", "note": "SOP-INV-2"}, team.officer), http.StatusForbidden)
	h.wantStatus(h.do("PUT", "/api/v1/admin/inventory/policies", map[string]any{"category": "poultry", "method": "fefo", "note": "SOP-INV-2"}, team.stores), http.StatusOK)

	// Two chicken deliveries: the second expires sooner. Rice in full.
	receive := func(k string, lines ...map[string]any) {
		h.wantStatus(h.do("POST", "/api/v1/admin/receiving", map[string]any{"po_id": st.po, "idempotency_key": "form-" + k + "-" + st.po[:8], "lines": lines}, team.receiver), http.StatusCreated)
	}
	receive("a", map[string]any{"po_line_id": st.lines["chicken"], "delivered_packs": "2", "accepted_packs": "2", "expiry_date": "2026-11-22", "location_id": st.chill,
		"inspection": pass(all, cold)}, map[string]any{"po_line_id": st.lines["rice"], "delivered_packs": st.packs["rice"], "accepted_packs": st.packs["rice"],
		"expiry_date": "2027-03-01", "location_id": st.dry, "inspection": pass(all)})
	receive("b", map[string]any{"po_line_id": st.lines["chicken"], "delivered_packs": "2", "accepted_packs": "2", "expiry_date": "2026-11-18", "location_id": st.chill,
		"inspection": pass(all, cold)})
	lots := st.lotsOf(t, st.ing["chicken"])["available"]
	byExpiry := map[string]string{}
	for _, l := range lots {
		byExpiry[l["expiry_date"].(string)] = l["id"].(string)
	}
	soon, later := byExpiry["2026-11-18"], byExpiry["2026-11-22"]

	// Reserving: the soonest-expiring lot first; nothing reserved twice, even at once.
	res := h.do("GET", "/api/v1/admin/procurement/runs/"+st.run, nil, team.officer)
	chicken := lineFor(t, res.Data, st.ing["chicken"])
	gross := chicken["gross"].(string)
	if chicken["available"] != "8000" || chicken["reserved"] != "0" {
		t.Fatalf("before reserving = %v", chicken)
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/inventory/reservations", map[string]any{"plan_id": st.plan, "ingredient_id": st.ing["chicken"], "quantity": "1000"}, team.cook), http.StatusForbidden)
	var wg sync.WaitGroup
	results := make([]map[string]any, 3)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := h.do("POST", "/api/v1/admin/inventory/reservations", map[string]any{"plan_id": st.plan, "ingredient_id": st.ing["chicken"], "quantity": "3000"}, team.stores)
			results[i] = r.Data
		}()
	}
	wg.Wait()
	total := qty.Q{}
	for _, r := range results {
		total = total.Add(qty.MustParse(r["reserved"].(string)))
	}
	if total.String() != "8000" {
		t.Fatalf("reserved in total = %s (8000 g usable); results %v", total.String(), results)
	}
	res = h.do("GET", "/api/v1/admin/inventory/reservations?plan_id="+st.plan+"&ingredient_id="+st.ing["chicken"], nil, team.stores)
	firstRes := obj(t, list(t, res.Data["items"])[len(list(t, res.Data["items"]))-1])
	if firstRes["lot_id"] != soon || firstRes["status"] != "active" {
		t.Errorf("first reservation = %v (FEFO wants the lot expiring 18 Nov)", firstRes)
	}
	// Release what the plan does not need, with a reason.
	for _, x := range list(t, res.Data["items"]) {
		r := obj(t, x)
		if r["lot_id"] == later {
			h.wantStatus(h.do("POST", "/api/v1/admin/inventory/reservations/"+r["id"].(string)+"/release", map[string]any{"reason": "More than the plan needs."}, team.stores), http.StatusOK)
		}
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/inventory/reservations", map[string]any{"plan_id": st.plan, "ingredient_id": st.ing["rice"],
		"quantity": lineFor(t, h.do("GET", "/api/v1/admin/procurement/runs/"+st.run, nil, team.officer).Data, st.ing["rice"])["gross"]}, team.stores), http.StatusCreated)
	// Reserved stock counts once for the plan's net requirement.
	res = h.do("GET", "/api/v1/admin/procurement/runs/"+st.run, nil, team.officer)
	chicken = lineFor(t, res.Data, st.ing["chicken"])
	if chicken["reserved"] != "4000" {
		t.Errorf("reserved for the plan = %v", chicken["reserved"])
	}
	// A held lot cannot be held while reserved; an adjustment during a reservation cannot go below it.
	h.wantError(h.do("POST", "/api/v1/admin/inventory/lots/"+soon+"/status", map[string]any{"status": "on_hold", "reason": "Check smell"}, team.receiver), http.StatusConflict, "reserved_stock")
	h.wantError(h.do("POST", "/api/v1/admin/inventory/adjustments", map[string]any{"lot_id": soon, "category": "damage", "new_on_hand": "100", "reason": "Dropped a bag."}, team.stores),
		http.StatusUnprocessableEntity, "reserved_stock")

	// Issuing to the batch: the plan's reservation first. Too much is refused whole.
	h.wantStatus(h.do("POST", "/api/v1/admin/inventory/issues", map[string]any{"batch_id": st.batch, "ingredient_id": st.ing["chicken"], "quantity": "100"}, team.receiver), http.StatusForbidden)
	h.wantError(h.do("POST", "/api/v1/admin/inventory/issues", map[string]any{"batch_id": st.batch, "ingredient_id": st.ing["chicken"], "quantity": "9000"}, team.cook),
		http.StatusUnprocessableEntity, "insufficient_stock")
	if n := h.count(`SELECT count(*) FROM inventory_movements WHERE type = 'issue'`); n != 0 {
		t.Fatalf("a refused issue moved stock: %d", n)
	}
	res = h.do("POST", "/api/v1/admin/inventory/issues", map[string]any{"batch_id": st.batch, "ingredient_id": st.ing["chicken"], "quantity": gross}, team.cook)
	h.wantStatus(res, http.StatusCreated)
	issued := list(t, res.Data["issued"])
	if obj(t, issued[0])["lot_id"] != soon || obj(t, issued[0])["from_reserved"] == "0" {
		t.Errorf("issued = %v", issued)
	}
	// Two cooks issuing the last of a lot at once: one gets it.
	remaining := qty.MustParse("8000").Sub(qty.MustParse(gross)).String()
	codes := make([]int, 2)
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = h.do("POST", "/api/v1/admin/inventory/issues", map[string]any{"batch_id": st.batch, "ingredient_id": st.ing["chicken"], "quantity": remaining}, team.cook).Status
		}()
	}
	wg.Wait()
	if !(codes[0]+codes[1] == 201+422) {
		t.Fatalf("concurrent issues = %v", codes)
	}
	// Back to the store: what the batch did not use.
	h.wantError(h.do("POST", "/api/v1/admin/inventory/returns", map[string]any{"batch_id": st.batch, "lot_id": soon, "quantity": "99999"}, team.cook), http.StatusUnprocessableEntity, "over_return")
	h.wantStatus(h.do("POST", "/api/v1/admin/inventory/returns", map[string]any{"batch_id": st.batch, "lot_id": later, "quantity": "500", "reason": "Not needed"}, team.cook), http.StatusCreated)
	// The kitchen records its waste (Sprint 5); it is never posted again as stock.
	h.wantStatus(h.do("POST", "/api/v1/admin/production/waste", map[string]any{"batch_id": st.batch, "ingredient_id": st.ing["chicken"], "quantity": "0.25", "unit": "kg",
		"category": "preparation_loss", "reason": "Trim"}, team.cook), http.StatusCreated)
	if n := h.count(`SELECT count(*) FROM inventory_movements WHERE type = 'waste'`); n != 0 {
		t.Errorf("production waste posted as stock movements: %d", n)
	}
	res = h.do("GET", "/api/v1/admin/inventory/batches/"+st.batch+"/reconciliation", nil, team.stores)
	h.wantStatus(res, http.StatusOK)
	var rc map[string]any
	for _, l := range list(t, res.Data["lines"]) {
		if obj(t, l)["ingredient_id"] == st.ing["chicken"] {
			rc = obj(t, l)
		}
	}
	consumed := qty.MustParse("8000").Sub(qty.MustParse("500")).String()
	if rc["planned"] != gross || rc["issued"] != "8000" || rc["returned"] != "500" || rc["consumed"] != consumed || rc["recorded_waste"] != "250" {
		t.Errorf("chicken reconciliation = %v", rc)
	}
	if rc["variance"] != qty.MustParse(consumed).Sub(qty.MustParse(gross)).String() {
		t.Errorf("variance = %v", rc["variance"])
	}

	// Stock issued to the plan's batches still serves the plan: it counts toward the requirement.
	res = h.do("GET", "/api/v1/admin/procurement/runs/"+st.run, nil, team.officer)
	chicken = lineFor(t, res.Data, st.ing["chicken"])
	if chicken["issued"] != consumed || chicken["reserved"] != "0" || chicken["shortage"] != "0" {
		t.Errorf("chicken after issuing = issued %v reserved %v shortage %v", chicken["issued"], chicken["reserved"], chicken["shortage"])
	}

	// Store waste and adjustments are reasoned rows; ordinary staff cannot make them.
	h.wantStatus(h.do("POST", "/api/v1/admin/inventory/adjustments", map[string]any{"lot_id": later, "category": "spoilage", "new_on_hand": "0", "reason": "x x"}, team.cook), http.StatusForbidden)
	res = h.do("POST", "/api/v1/admin/inventory/lots/"+later+"/waste", map[string]any{"quantity": "100", "reason": "Bag split in the chiller."}, team.stores)
	h.wantStatus(res, http.StatusOK)
	res = h.do("POST", "/api/v1/admin/inventory/adjustments", map[string]any{"lot_id": later, "category": "measurement_correction", "new_on_hand": "350",
		"reason": "Re-weighed on the calibrated scale.", "evidence": "Scale log 13 Nov"}, team.stores)
	h.wantStatus(res, http.StatusCreated)
	if n := h.count(`SELECT count(*) FROM stock_adjustments WHERE previous_on_hand = 400 AND new_on_hand = 350`); n != 1 {
		t.Errorf("adjustment record = %d", n)
	}

	// A count: a movement after a lot is counted is kept when the count posts.
	res = h.do("POST", "/api/v1/admin/inventory/counts", map[string]any{"location_id": st.chill}, team.stores)
	h.wantStatus(res, http.StatusCreated)
	count := res.Data["id"].(string)
	h.wantError(h.do("POST", "/api/v1/admin/inventory/counts", map[string]any{"location_id": st.chill}, team.stores), http.StatusConflict, "count_open")
	h.wantError(h.do("POST", "/api/v1/admin/inventory/counts/"+count+"/submit", nil, team.stores), http.StatusUnprocessableEntity, "incomplete")
	h.wantStatus(h.do("PUT", "/api/v1/admin/inventory/counts/"+count+"/lines", map[string]any{"lines": []map[string]any{{"lot_id": later, "counted": "330"}}}, team.stores), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/inventory/lots/"+later+"/waste", map[string]any{"quantity": "50", "reason": "Trimmed during the count."}, team.stores), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/inventory/counts/"+count+"/submit", nil, team.stores), http.StatusOK)
	h.wantError(h.do("POST", "/api/v1/admin/inventory/counts/"+count+"/post", nil, team.stores), http.StatusForbidden, "own_count")
	stores2 := bearer(h.adminLogin("stores2@mise.test", "inventory_manager"))
	res = h.do("POST", "/api/v1/admin/inventory/counts/"+count+"/post", nil, stores2)
	h.wantStatus(res, http.StatusOK)
	// Counted 330 against 350 expected (−20); the 50 wasted afterwards stays: 350 − 50 − 20 = 280.
	res = h.do("GET", "/api/v1/admin/inventory/lots/"+later, nil, team.stores)
	if res.Data["on_hand"] != "280" {
		t.Errorf("after the count = %v", res.Data["on_hand"])
	}
	if n := h.count(`SELECT count(*) FROM admin_alerts WHERE kind = 'stock_count_variance'`); n != 1 {
		t.Errorf("count variance alerts = %d", n)
	}
	h.ledgerBalances()

	// Traceability: supplier → order → receipt → lot → issue → batch → recipe → customer orders (batch-level).
	h.wantStatus(h.do("GET", "/api/v1/admin/traceability?lot="+soon, nil, team.cook), http.StatusForbidden)
	res = h.do("GET", "/api/v1/admin/traceability?lot="+soon, nil, team.stores)
	h.wantStatus(res, http.StatusOK)
	tl := obj(t, list(t, res.Data["lots"])[0])
	lot := obj(t, tl["lot"])
	tb := obj(t, list(t, tl["batches"])[0])
	if lot["supplier_name"] != "Hosein Family Farm" || lot["po_number"] == "" || lot["receipt_number"] == "" ||
		obj(t, tb["batch"])["recipe_version"] != float64(1) || len(list(t, tb["order_numbers"])) != 1 || res.Data["notice"] == "" {
		t.Errorf("trace = %v", res.Data)
	}
	// Only chicken was issued to the batch (two lots); rice was reserved, not issued.
	res = h.do("GET", "/api/v1/admin/traceability?batch="+st.batch, nil, team.stores)
	if n := len(list(t, res.Data["lots"])); n != 2 {
		t.Errorf("lots traced to the batch = %d", n)
	}
	res = h.do("GET", "/api/v1/admin/traceability?supplier="+st.farm, nil, team.stores)
	if n := len(list(t, res.Data["lots"])); n != 3 {
		t.Errorf("lots traced to the supplier = %d", n)
	}
	for _, typ := range []string{"inventory.lot_received", "inventory.stock_reserved", "inventory.stock_issued", "inventory.stock_adjusted",
		"purchase_order.partially_received"} {
		if n := h.count(`SELECT count(*) FROM domain_events WHERE type = $1`, typ); n == 0 {
			t.Errorf("no %s event", typ)
		}
	}
}

func reportTotals(t *testing.T, res response) map[string]float64 {
	t.Helper()
	out := map[string]float64{}
	for _, m := range list(t, res.Data["totals"]) {
		out[obj(t, m)["key"].(string)] = obj(t, m)["value"].(float64)
	}
	return out
}

func reportBreakdown(t *testing.T, res response, key string) map[string]float64 {
	t.Helper()
	out := map[string]float64{}
	for _, x := range list(t, obj(t, res.Data["breakdowns"])[key]) {
		out[obj(t, x)["label"].(string)] = obj(t, x)["value"].(float64)
	}
	return out
}

func TestSupplyReportsAndAlertsMatchTheRecords(t *testing.T) {
	st := newStockSetup(t)
	h, team := st.h, st.team
	all, cold := st.checklist(t)
	// One delivery on time (13 Nov, expected 14 Nov): 2 good bags of chicken, one torn; all the rice; one carton of coconut milk past its date.
	h.wantStatus(h.do("POST", "/api/v1/admin/receiving", map[string]any{"po_id": st.po, "idempotency_key": "report-form-1-" + st.po[:8], "lines": []map[string]any{
		{"po_line_id": st.lines["chicken"], "delivered_packs": "3", "accepted_packs": "2", "rejected_packs": "1", "expiry_date": "2026-11-20", "location_id": st.chill,
			"quarantine_location_id": st.quarantine, "inspection": pass(all, cold), "discrepancy": "damaged", "discrepancy_note": "Torn bag"},
		{"po_line_id": st.lines["rice"], "delivered_packs": st.packs["rice"], "accepted_packs": st.packs["rice"], "expiry_date": "2027-03-01", "location_id": st.dry, "inspection": pass(all)},
		{"po_line_id": st.lines["coconut"], "delivered_packs": "1", "accepted_packs": "1", "location_id": st.dry, "inspection": pass(all)},
	}}, team.receiver), http.StatusCreated)
	h.wantStatus(h.do("POST", "/api/v1/admin/inventory/issues", map[string]any{"batch_id": st.batch, "ingredient_id": st.ing["chicken"], "quantity": "1500"}, team.cook), http.StatusCreated)

	// Reports are for supplier_reports.read.
	h.wantStatus(h.do("GET", "/api/v1/admin/reports/procurement?from=2026-11-01&to=2026-11-30", nil, team.receiver), http.StatusForbidden)
	res := h.do("GET", "/api/v1/admin/reports/procurement?from=2026-11-01&to=2026-11-30", nil, team.manager)
	h.wantStatus(res, http.StatusOK)
	var poTotal float64
	po := h.do("GET", "/api/v1/admin/purchase-orders/"+st.po, nil, team.manager)
	poTotal = po.Data["total_minor"].(float64)
	tot := reportTotals(t, res)
	for k, v := range map[string]float64{"runs": 1, "shortages": 1, "pos_approved": 1, "po_value": poTotal, "pos_issued": 1, "receipts": 1, "open_pos": 1, "rfqs": 0} {
		if tot[k] != v {
			t.Errorf("procurement %s = %v, want %v", k, tot[k], v)
		}
	}
	if reportBreakdown(t, res, "value_by_supplier")["Hosein Family Farm"] != poTotal {
		t.Errorf("value by supplier = %v", reportBreakdown(t, res, "value_by_supplier"))
	}
	res = h.do("GET", "/api/v1/admin/reports/inventory?from=2026-11-01&to=2026-11-30", nil, team.stores)
	h.wantStatus(res, http.StatusOK)
	tot = reportTotals(t, res)
	riceValue := qty.MustParse(st.packs["rice"]).Mul(qty.Int(9000))
	wantValue, _ := riceValue.Add(qty.Int(2 * 4500)).Add(qty.Int(1500)).Rat().Float64()
	for k, v := range map[string]float64{"lots_received": 4, "lots_rejected": 1, "received_value": wantValue, "issues": 1, "returns": 0, "expiry_review": 1} {
		if tot[k] != v {
			t.Errorf("inventory %s = %v, want %v", k, tot[k], v)
		}
	}
	onHand := reportBreakdown(t, res, "on_hand")
	if onHand["Chicken thigh (g)"] != 4500 { // 4000 accepted − 1500 issued + 2000 rejected still held
		t.Errorf("chicken on hand = %v", onHand)
	}
	if reportBreakdown(t, res, "issued")["Chicken thigh (g)"] != 1500 {
		t.Errorf("issued = %v", reportBreakdown(t, res, "issued"))
	}
	res = h.do("GET", "/api/v1/admin/reports/suppliers?from=2026-11-01&to=2026-11-30", nil, team.finance)
	h.wantStatus(res, http.StatusOK)
	tot = reportTotals(t, res)
	if tot["deliveries"] != 1 || tot["on_time"] != 1 || tot["suppliers_approved"] != 1 {
		t.Errorf("supplier totals = %v", tot)
	}
	if reportBreakdown(t, res, "rejected_packs")["Hosein Family Farm"] != 1 || reportBreakdown(t, res, "orders_delivered")["Hosein Family Farm"] != 1 {
		t.Errorf("supplier breakdowns = %v / %v", reportBreakdown(t, res, "rejected_packs"), reportBreakdown(t, res, "orders_delivered"))
	}
	if _, ok := res.Data["definitions"].(map[string]any)["lead_time_days"]; !ok {
		t.Error("lead time is not defined")
	}

	// Alerts: the order is overdue after 14 Nov (still partly received), the coconut milk awaits an expiry review.
	if _, err := h.bo.ScanAlerts(context.Background(), at("2026-11-16 12:00")); err != nil {
		t.Fatal(err)
	}
	for kind, perm := range map[string]string{"purchase_order_overdue": "purchase_orders.read", "lot_expiry_review": "receiving.inspect"} {
		if n := h.count(`SELECT count(*) FROM admin_alerts WHERE kind = $1 AND permission = $2`, kind, perm); n != 1 {
			t.Errorf("%s alerts = %d", kind, n)
		}
	}
	// Scanning again raises nothing new.
	if n, _ := h.bo.ScanAlerts(context.Background(), at("2026-11-16 12:01")); n != 0 {
		t.Errorf("second scan raised %d", n)
	}
}
