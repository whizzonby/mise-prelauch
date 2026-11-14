package server_test

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"mise.tt/api/internal/payments/testpay"
	"mise.tt/api/internal/procurement"
)

// releasedPlan publishes a curry recipe, takes paid orders for the week of
// 16 November, and releases the production plan, all through the real API.
// It returns the plan and the ingredient ids by short name.
func releasedPlan(t *testing.T) (*shop, culinaryTeam, string, map[string]string) {
	t.Helper()
	s := newShop(t)
	h := s.h
	team := newTeam(h)
	ing := map[string]string{
		"chicken": h.ingredient(team.director, map[string]any{"name": "Chicken thigh", "category": "poultry", "base_unit": "g"}),
		"rice":    h.ingredient(team.director, map[string]any{"name": "Parboiled rice", "category": "grains", "base_unit": "g"}),
		"coconut": h.ingredient(team.director, map[string]any{"name": "Coconut milk", "category": "packaged", "base_unit": "g"}),
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/ingredients/"+ing["coconut"]+"/conversions", map[string]any{"from_unit": "ml", "to_unit": "g", "factor": "1.03", "source": "Spec"}, team.director), http.StatusOK)
	h.publishRecipe(team, s.meals[0], "Curry chicken", []map[string]any{
		{"ingredient_id": ing["chicken"], "quantity": "400", "unit": "g", "preparation": "diced"},
		{"ingredient_id": ing["rice"], "quantity": "0.3", "unit": "kg"},
		{"ingredient_id": ing["coconut"], "quantity": "100", "unit": "ml"},
	}, nil)
	tok, _ := s.active("curry-fan@example.com", testpay.CardApprove)
	h.wantStatus(h.do("POST", "/api/v1/me/meal-selections/"+s.weekCycle(tok, "2026-11-16")["id"].(string)+"/finalize", map[string]any{"items": items(s.meals[0], 3)}, tok), http.StatusOK)
	s.tick("2026-11-13 00:30")
	h.wantStatus(h.do("POST", "/api/v1/admin/production/snapshots", map[string]any{"week": "2026-11-16"}, team.manager), http.StatusCreated)
	res := h.do("POST", "/api/v1/admin/production/plans", map[string]any{"week": "2026-11-16"}, team.manager)
	h.wantStatus(res, http.StatusCreated)
	plan := res.Data["id"].(string)
	h.wantStatus(h.do("POST", "/api/v1/admin/production/task-templates", map[string]any{"name": "Portion proteins", "kind": "portioning"}, team.manager), http.StatusCreated)
	h.wantStatus(h.do("POST", "/api/v1/admin/production/plans/"+plan+"/approve", nil, team.manager), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/production/plans/"+plan+"/release", nil, team.manager), http.StatusOK)
	return s, team, plan, ing
}

// product adds a confirmed, priced product for an ingredient.
func (h *harness) product(as reqOpt, supplier, ingredient, name, packQty, packUnit string, priceMinor int64, lead int) string {
	h.t.Helper()
	res := h.do("POST", "/api/v1/admin/suppliers/"+supplier+"/products", map[string]any{"ingredient_id": ingredient, "name": name, "pack_quantity": packQty,
		"pack_unit": packUnit, "lead_time_days": lead}, as)
	h.wantStatus(res, http.StatusCreated)
	id := res.Data["id"].(string)
	h.wantStatus(h.do("POST", "/api/v1/admin/supplier-products/"+id+"/mapping", map[string]any{"decision": "confirmed"}, as), http.StatusOK)
	if priceMinor > 0 {
		h.wantStatus(h.do("POST", "/api/v1/admin/supplier-products/"+id+"/prices", map[string]any{"amount_minor": priceMinor, "currency": "TTD", "valid_from": "2026-11-01"}, as), http.StatusCreated)
	}
	return id
}

func lineFor(t *testing.T, run map[string]any, ingredient string) map[string]any {
	t.Helper()
	for _, l := range list(t, run["plan_lines"]) {
		if obj(t, l)["ingredient_id"] == ingredient {
			return obj(t, l)
		}
	}
	t.Fatalf("no line for %s in %v", ingredient, run["plan_lines"])
	return nil
}

func TestProcurementConsumesReleasedPlansOnce(t *testing.T) {
	s, _, plan, ing := releasedPlan(t)
	h := s.h
	sup := newSupplyTeam(h)
	ctx := context.Background()

	// The release event becomes a run through the job queue.
	n, err := h.sc.Procurement.Dispatch(ctx)
	if err != nil || n != 1 {
		t.Fatalf("dispatch = %d, %v", n, err)
	}
	h.drainJobs()
	if n, _ := h.sc.Procurement.Dispatch(ctx); n != 0 {
		t.Errorf("second dispatch handed over %d events again", n)
	}
	// Replayed delivery and a planner clicking at the same moment still make one run.
	if err := procurement.Replay(ctx, h.pool, 0); err != nil {
		t.Fatal(err)
	}
	if n, _ := h.sc.Procurement.Dispatch(ctx); n != 1 {
		t.Errorf("replay dispatched %d", n)
	}
	h.drainJobs()
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.do("POST", "/api/v1/admin/procurement/runs", map[string]any{"plan_id": plan}, sup.officer)
		}()
	}
	wg.Wait()
	if n := h.count(`SELECT count(*) FROM procurement_runs`); n != 1 {
		t.Fatalf("runs = %d", n)
	}
	if n := h.count(`SELECT count(*) FROM jobs WHERE kind = 'procurement.plan_released' AND status = 'failed'`); n != 0 {
		t.Errorf("failed deliveries = %d", n)
	}
	res := h.do("GET", "/api/v1/admin/procurement/runs", nil, sup.officer)
	runs := list(t, res.Data["items"])
	run := obj(t, runs[0])
	if run["source_event_id"] == nil || run["required_by"] != "2026-11-16" || run["lines"] != float64(3) {
		t.Fatalf("run = %v", run)
	}
	res = h.do("GET", "/api/v1/admin/procurement/runs/"+run["id"].(string), nil, sup.officer)
	h.wantStatus(res, http.StatusOK)
	// Gross requirements as the plan states them; with no stock and nothing ordered, all short.
	if len(list(t, res.Data["requirements"])) != 3 {
		t.Errorf("requirements = %v", res.Data["requirements"])
	}
	chicken := lineFor(t, res.Data, ing["chicken"])
	if chicken["gross"] == nil || chicken["shortage"] != chicken["gross"] || chicken["reserved"] != "0" || chicken["inbound"] != "0" || len(list(t, chicken["explain"])) < 4 {
		t.Errorf("chicken line = %v", chicken)
	}
	// Unreleased plans and kitchen staff are refused.
	h.wantStatus(h.do("POST", "/api/v1/admin/procurement/runs", map[string]any{"plan_id": plan}, sup.cook), http.StatusForbidden)
	res = h.do("GET", "/api/v1/admin/procurement/pending-plans", nil, sup.officer)
	if len(list(t, res.Data["items"])) != 0 {
		t.Errorf("plans without a run = %v", res.Data["items"])
	}
	if n := h.count(`SELECT count(*) FROM domain_events WHERE type = 'procurement.shortage_identified'`); n != 1 {
		t.Errorf("shortage events = %d", n)
	}
	if n := h.count(`SELECT count(*) FROM admin_alerts WHERE kind = 'ingredient_shortage' AND permission = 'procurement.plan'`); n != 1 {
		t.Errorf("shortage alerts = %d", n)
	}
}

func TestPurchasingDecisionsOrdersApprovalsAndRevisions(t *testing.T) {
	s, _, plan, ing := releasedPlan(t)
	h := s.h
	sup := newSupplyTeam(h)
	boss2 := bearer(h.adminLogin("buyer-boss2@mise.test", "procurement_manager"))
	h.criterion(sup.manager, "Supply agreement signed")
	farm := h.approvedSupplier(sup.coordinator, sup.manager, "Hosein Family Farm", "farmer")
	dist := h.approvedSupplier(sup.coordinator, sup.manager, "Northern Distributors", "distributor")
	res := h.do("POST", "/api/v1/admin/suppliers", map[string]any{"name": "Unvetted Traders", "type": "distributor", "phone": "1"}, sup.coordinator)
	unvetted := res.Data["id"].(string)
	bag := h.product(sup.coordinator, farm, ing["chicken"], "Chicken thighs, 2 kg bag", "2", "kg", 4500, 2)
	h.product(sup.coordinator, dist, ing["chicken"], "Chicken thighs, 10 kg case", "10", "kg", 20000, 10)
	riceSack := h.product(sup.coordinator, dist, ing["rice"], "Parboiled rice, 10 kg sack", "10", "kg", 9000, 1)
	cheap := h.product(sup.coordinator, unvetted, ing["chicken"], "Cheap chicken", "1", "kg", 100, 1)

	run, _, err := h.sc.Procurement.CreateRun(context.Background(), "", plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	runURL := "/api/v1/admin/procurement/runs/" + run.ID
	res = h.do("GET", runURL, nil, sup.officer)
	chicken := lineFor(t, res.Data, ing["chicken"])
	// Two approved options, sorted by supplier, not by price; the unvetted supplier is not one.
	opts := list(t, chicken["options"])
	if len(opts) != 2 || chicken["suggested_product_id"] != nil {
		t.Fatalf("chicken options = %v", opts)
	}
	farmOpt, distOpt := obj(t, opts[0]), obj(t, opts[1])
	if farmOpt["supplier_name"] != "Hosein Family Farm" || distOpt["lead_time_risk"] != true || farmOpt["lead_time_risk"] != false {
		t.Errorf("options = %v / %v", farmOpt, distOpt)
	}
	// Whole packs, at least the shortage.
	if farmOpt["packs"] == "0" || farmOpt["cost_minor"] == nil {
		t.Errorf("farm option = %v", farmOpt)
	}
	rice := lineFor(t, res.Data, ing["rice"])
	if rice["suggested_product_id"] != riceSack {
		t.Errorf("single rice option not suggested: %v", rice["suggested_product_id"])
	}
	coconut := lineFor(t, res.Data, ing["coconut"])

	decide := func(line string, body map[string]any, as reqOpt) response {
		return h.do("POST", runURL+"/lines/"+line+"/decision", body, as)
	}
	h.wantStatus(decide(chicken["id"].(string), map[string]any{"decision": "buy", "product_id": bag, "packs": "0.5"}, sup.officer), http.StatusUnprocessableEntity)
	h.wantStatus(decide(chicken["id"].(string), map[string]any{"decision": "buy", "product_id": riceSack, "packs": "1"}, sup.officer), http.StatusUnprocessableEntity)
	h.wantStatus(decide(chicken["id"].(string), map[string]any{"decision": "buy", "product_id": bag, "packs": farmOpt["packs"], "note": "Closer, fresher."}, sup.officer), http.StatusOK)
	h.wantStatus(decide(rice["id"].(string), map[string]any{"decision": "no_purchase"}, sup.officer), http.StatusUnprocessableEntity)
	h.wantStatus(decide(rice["id"].(string), map[string]any{"decision": "buy", "product_id": riceSack, "packs": rice["options"].([]any)[0].(map[string]any)["packs"]}, sup.officer), http.StatusOK)
	// Approval needs every shortage decided.
	h.wantError(h.do("POST", runURL+"/approve", nil, sup.manager), http.StatusUnprocessableEntity, "decisions_pending")
	h.wantStatus(decide(coconut["id"].(string), map[string]any{"decision": "deferred", "note": "Asking two suppliers for quotes first."}, sup.officer), http.StatusOK)
	h.wantStatus(h.do("POST", runURL+"/approve", nil, sup.officer), http.StatusForbidden)
	res = h.do("POST", runURL+"/approve", nil, sup.manager)
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != "approved" {
		t.Fatalf("run = %v", res.Data["status"])
	}
	// Approved decisions do not change silently.
	h.wantError(decide(coconut["id"].(string), map[string]any{"decision": "pending"}, sup.officer), http.StatusConflict, "run_locked")

	// One draft per supplier from the buy decisions; asking again adds nothing.
	res = h.do("POST", runURL+"/purchase-orders", nil, sup.officer)
	h.wantStatus(res, http.StatusCreated)
	if len(list(t, res.Data["items"])) != 2 {
		t.Fatalf("drafted = %v", res.Data["items"])
	}
	res = h.do("POST", runURL+"/purchase-orders", nil, sup.officer)
	if len(list(t, res.Data["items"])) != 0 {
		t.Fatalf("second drafting = %v", res.Data["items"])
	}
	res = h.do("GET", "/api/v1/admin/purchase-orders?supplier_id="+farm, nil, sup.officer)
	po := obj(t, list(t, res.Data["items"])[0])
	poURL := "/api/v1/admin/purchase-orders/" + po["id"].(string)

	// Submitting needs a delivery date; the buyer (the officer) cannot approve.
	h.wantError(h.do("POST", poURL+"/submit", nil, sup.officer), http.StatusUnprocessableEntity, "incomplete")
	h.wantStatus(h.do("PATCH", poURL, map[string]any{"expected_delivery": "2026-11-15"}, sup.officer), http.StatusOK)
	h.wantStatus(h.do("POST", poURL+"/submit", nil, sup.officer), http.StatusOK)
	h.wantStatus(h.do("POST", poURL+"/approve", nil, sup.officer), http.StatusForbidden)
	h.wantStatus(h.do("POST", poURL+"/approve", nil, sup.receiver), http.StatusForbidden)

	// Two approvers at once: one approves, the other finds it approved.
	statuses := make([]int, 2)
	var wg sync.WaitGroup
	for i, as := range []reqOpt{sup.manager, boss2} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses[i] = h.do("POST", poURL+"/approve", nil, as).Status
		}()
	}
	wg.Wait()
	if !(statuses[0] == 200 && statuses[1] == 409) && !(statuses[0] == 409 && statuses[1] == 200) {
		t.Fatalf("concurrent approvals = %v", statuses)
	}
	res = h.do("GET", poURL, nil, sup.officer)
	if res.Data["status"] != "approved" || len(list(t, res.Data["revisions"])) != 1 {
		t.Fatalf("approved order = %v", res.Data)
	}
	if n := h.count(`SELECT count(*) FROM ingredient_cost_references WHERE origin = 'purchase_order'`); n != 1 {
		t.Errorf("ordered cost references = %d", n)
	}
	// Inbound now counts for the line (expected before production): no shortage.
	res = h.do("GET", runURL, nil, sup.officer)
	chicken = lineFor(t, res.Data, ing["chicken"])
	if chicken["shortage"] != "0" || chicken["inbound"] == "0" {
		t.Errorf("chicken after ordering = shortage %v inbound %v", chicken["shortage"], chicken["inbound"])
	}

	// Issuing is recorded, not sent; only issuers issue.
	h.wantStatus(h.do("POST", poURL+"/issue", map[string]any{"channel": "whatsapp"}, sup.officer), http.StatusForbidden)
	h.wantStatus(h.do("POST", poURL+"/issue", map[string]any{"channel": "carrier pigeon"}, sup.manager), http.StatusUnprocessableEntity)
	h.wantStatus(h.do("POST", poURL+"/issue", map[string]any{"channel": "whatsapp", "note": "Sent the PDF to Mr Hosein."}, sup.manager), http.StatusOK)
	if n := h.count(`SELECT count(*) FROM supplier_communications WHERE resource_type = 'purchase_order'`); n != 1 {
		t.Errorf("issue not logged as a contact: %d", n)
	}

	// A material change after approval needs a reason and a new approval; revision 1 stays as approved.
	newLines := []map[string]any{{"product_id": bag, "packs": "9", "unit_price_minor": 4400}}
	h.wantStatus(h.do("PATCH", poURL, map[string]any{"lines": newLines}, sup.officer), http.StatusUnprocessableEntity)
	res = h.do("PATCH", poURL, map[string]any{"lines": newLines, "reason": "Farmer can supply more at a better price."}, sup.officer)
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != "pending_approval" || res.Data["revision"] != float64(2) {
		t.Fatalf("after change = %v rev %v", res.Data["status"], res.Data["revision"])
	}
	rev1 := obj(t, list(t, res.Data["revisions"])[0])
	if rev1["revision"] != float64(1) || rev1["total_minor"] == float64(9*4400) {
		t.Errorf("revision 1 = %v", rev1)
	}
	if _, err := h.pool.Exec(context.Background(), `UPDATE purchase_order_revisions SET total_minor = 0`); err == nil {
		t.Error("an approved revision could be changed")
	}

	// An approval limit (when the business sets one) needs a second approver above it.
	h.wantStatus(h.do("PUT", "/api/v1/admin/procurement/settings", map[string]any{"approval_limit_minor": 10000}, sup.officer), http.StatusForbidden)
	res = h.do("PUT", "/api/v1/admin/procurement/settings", map[string]any{"approval_limit_minor": 10000}, sup.manager)
	h.wantStatus(res, http.StatusOK)
	res = h.do("POST", poURL+"/approve", nil, boss2)
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != "pending_approval" || res.Data["approved_by_name"] == nil || len(list(t, res.Data["blocking"])) != 1 {
		t.Fatalf("after first approval above the limit = %v", res.Data)
	}
	h.wantError(h.do("POST", poURL+"/approve", nil, boss2), http.StatusForbidden, "above_limit")
	res = h.do("POST", poURL+"/approve", nil, sup.finance)
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != "approved" || res.Data["limit_approved_by_name"] == nil || len(list(t, res.Data["revisions"])) != 2 {
		t.Fatalf("after the limit approval = %v", res.Data)
	}

	// Receiving staff see the order but not its prices.
	res = h.do("GET", poURL, nil, sup.receiver)
	h.wantStatus(res, http.StatusOK)
	if obj(t, list(t, res.Data["lines"])[0])["unit_price_minor"] != nil || res.Data["total_minor"] != float64(0) {
		t.Errorf("receiver sees prices: %v", res.Data["lines"])
	}

	// Unapproved suppliers get no orders.
	res = h.do("POST", "/api/v1/admin/purchase-orders", map[string]any{"supplier_id": unvetted, "expected_delivery": "2026-11-15",
		"lines": []map[string]any{{"product_id": cheap, "packs": "5"}}}, sup.officer)
	h.wantStatus(res, http.StatusCreated)
	if b := list(t, res.Data["blocking"]); len(b) != 1 || !strings.Contains(b[0].(string), "not an approved supplier") {
		t.Errorf("unvetted supplier order not blocked: %v", res.Data["blocking"])
	}
	h.wantError(h.do("POST", "/api/v1/admin/purchase-orders/"+res.Data["id"].(string)+"/submit", nil, sup.officer), http.StatusUnprocessableEntity, "incomplete")

	// Quotations: an RFQ to two suppliers, invitations recorded (nothing sent), a quote recorded as received.
	res = h.do("POST", "/api/v1/admin/procurement/rfqs", map[string]any{"run_id": run.ID, "required_by": "2026-11-15",
		"lines": []map[string]any{{"ingredient_id": ing["rice"], "quantity": "20", "unit": "kg"}}, "supplier_ids": []string{dist, farm}}, sup.officer)
	h.wantStatus(res, http.StatusCreated)
	rfq := res.Data["id"].(string)
	if res.Data["status"] != "draft" {
		t.Errorf("rfq = %v", res.Data["status"])
	}
	res = h.do("POST", "/api/v1/admin/procurement/rfqs/"+rfq+"/invitations", map[string]any{"supplier_id": dist, "channel": "phone"}, sup.officer)
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != "open" {
		t.Errorf("rfq after invitation = %v", res.Data["status"])
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/procurement/quotations", map[string]any{"rfq_id": rfq, "supplier_id": unvetted, "currency": "TTD", "received_via": "email",
		"lines": []map[string]any{{"product_id": riceSack, "packs": "2", "unit_price_minor": 8500}}}, sup.officer), http.StatusUnprocessableEntity)
	res = h.do("POST", "/api/v1/admin/procurement/quotations", map[string]any{"rfq_id": rfq, "supplier_id": dist, "reference": "Q-881", "currency": "TTD",
		"received_via": "email", "valid_until": "2026-11-30", "lead_time_days": 1, "lines": []map[string]any{{"product_id": riceSack, "packs": "2", "unit_price_minor": 8500}}}, sup.officer)
	h.wantStatus(res, http.StatusCreated)
	if n := h.count(`SELECT count(*) FROM supplier_prices WHERE origin = 'quotation' AND quotation_id IS NOT NULL`); n != 1 {
		t.Errorf("quoted prices = %d", n)
	}
	if _, err := h.pool.Exec(context.Background(), `UPDATE quotation_lines SET unit_price_minor = 1`); err == nil {
		t.Error("a recorded quotation could be changed")
	}
	// The approved order kept its own price when the quotation arrived.
	res = h.do("GET", poURL, nil, sup.officer)
	if obj(t, list(t, res.Data["lines"])[0])["unit_price_minor"] != float64(4400) {
		t.Errorf("order price changed: %v", res.Data["lines"])
	}
	if n := h.count(`SELECT count(*) FROM audit_log WHERE action LIKE 'purchase_order.%'`); n < 8 {
		t.Errorf("purchase order audit entries = %d", n)
	}
}
