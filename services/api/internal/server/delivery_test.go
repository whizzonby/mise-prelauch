package server_test

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"mise.tt/api/internal/payments/testpay"
	"mise.tt/api/internal/platform/consumer"
)

// ---------------------------------------------------------------- fixtures

// kitchenWeek is a week of real orders taken through production: two
// paying customers, curry and buljol, a released plan with one batch per
// meal. Nothing is inserted by hand: every record comes from the API.
type kitchenWeek struct {
	s       *shop
	h       *harness
	team    culinaryTeam
	plan    string
	qc      string
	curry   string // batch ids
	fish    string
	tokA    reqOpt
	tokB    reqOpt
	orderA  string
	orderB  string
	ops     deliveryTeam
	pending map[string]bool
}

type deliveryTeam struct {
	fmgr, fstaff, dmgr, dispatcher, coord, support, courier reqOpt
}

func newDeliveryTeam(h *harness) deliveryTeam {
	return deliveryTeam{
		fmgr:       bearer(h.adminLogin("fmgr@mise.test", "fulfillment_manager")),
		fstaff:     bearer(h.adminLogin("packer@mise.test", "fulfillment_staff")),
		dmgr:       bearer(h.adminLogin("dmgr@mise.test", "dispatch_manager")),
		dispatcher: bearer(h.adminLogin("dispatch@mise.test", "dispatcher")),
		coord:      bearer(h.adminLogin("coord@mise.test", "delivery_coordinator")),
		support:    bearer(h.adminLogin("help@mise.test", "support")),
		courier:    bearer(h.adminLogin("rider@mise.test", "courier")),
	}
}

func newKitchenWeek(t *testing.T) *kitchenWeek {
	t.Helper()
	s := newShop(t)
	h := s.h
	team := newTeam(h)
	w := &kitchenWeek{s: s, h: h, team: team, ops: newDeliveryTeam(h)}
	chicken := h.ingredient(team.director, map[string]any{"name": "Chicken thigh", "category": "poultry", "base_unit": "g"})
	rice := h.ingredient(team.director, map[string]any{"name": "Rice", "category": "grains", "base_unit": "g"})
	cod := h.ingredient(team.director, map[string]any{"name": "Salted cod", "category": "seafood", "base_unit": "g", "allergens": []string{"fish"}})
	h.publishRecipe(team, s.meals[0], "Curry chicken", []map[string]any{
		{"ingredient_id": chicken, "quantity": "400", "unit": "g"}, {"ingredient_id": rice, "quantity": "300", "unit": "g"}}, nil)
	h.publishRecipe(team, s.fishMeal, "Buljol", []map[string]any{
		{"ingredient_id": cod, "quantity": "200", "unit": "g"}, {"ingredient_id": rice, "quantity": "200", "unit": "g"}}, []string{"fish"})

	w.tokA, _ = s.active("ana@example.com", testpay.CardApprove)
	w.tokB, _ = s.active("ben@example.com", testpay.CardApprove)
	h.wantStatus(h.do("POST", "/api/v1/me/meal-selections/"+s.weekCycle(w.tokA, "2026-11-16")["id"].(string)+"/finalize",
		map[string]any{"items": items(s.meals[0], 3)}, w.tokA), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/me/meal-selections/"+s.weekCycle(w.tokB, "2026-11-16")["id"].(string)+"/finalize",
		map[string]any{"items": items(s.meals[0], 2, s.fishMeal, 1)}, w.tokB), http.StatusOK)
	s.tick("2026-11-13 00:30")
	w.orderA, w.orderB = w.orderOf("ana@example.com"), w.orderOf("ben@example.com")

	h.wantStatus(h.do("POST", "/api/v1/admin/production/snapshots", map[string]any{"week": "2026-11-16"}, team.manager), http.StatusCreated)
	res := h.do("POST", "/api/v1/admin/production/plans", map[string]any{"week": "2026-11-16"}, team.manager)
	h.wantStatus(res, http.StatusCreated)
	w.plan = res.Data["id"].(string)
	h.wantStatus(h.do("POST", "/api/v1/admin/production/task-templates", map[string]any{"name": "Assemble kits", "kind": "assembly"}, team.manager), http.StatusCreated)
	res = h.do("POST", "/api/v1/admin/quality/templates", map[string]any{"name": "Kit complete and sealed", "check_type": "packaging_integrity",
		"criteria": "Per SOP-QA-2.", "critical": true}, team.manager)
	h.wantStatus(res, http.StatusCreated)
	w.qc = obj(t, list(t, res.Data["items"])[0])["id"].(string)
	h.wantStatus(h.do("POST", "/api/v1/admin/production/plans/"+w.plan+"/approve", nil, team.manager), http.StatusOK)
	res = h.do("POST", "/api/v1/admin/production/plans/"+w.plan+"/release", nil, team.manager)
	h.wantStatus(res, http.StatusOK)
	for _, b := range list(t, res.Data["batches"]) {
		m := obj(t, b)
		if m["meal_id"] == s.meals[0] {
			w.curry = m["id"].(string)
		} else {
			w.fish = m["id"].(string)
		}
	}
	return w
}

func (w *kitchenWeek) orderOf(email string) string {
	w.h.t.Helper()
	var id string
	if err := w.h.pool.QueryRow(context.Background(), `SELECT o.id FROM orders o JOIN customers c ON c.id = o.customer_id
		JOIN users u ON u.id = c.user_id WHERE u.email = $1`, email).Scan(&id); err != nil {
		w.h.t.Fatalf("order of %s: %v", email, err)
	}
	return id
}

// cook takes a batch through the kitchen: start, output, quality, packing,
// tasks, complete. Packed kits equal accepted kits.
func (w *kitchenWeek) cook(batch string, kits int) {
	h, team := w.h, w.team
	h.t.Helper()
	h.wantStatus(h.do("POST", "/api/v1/admin/production/batches/"+batch+"/start", nil, team.kitchen), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/production/batches/"+batch+"/output", map[string]any{"produced_kits": kits}, team.kitchen), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/quality/checks", map[string]any{"batch_id": batch, "template_id": w.qc, "result": "pass"}, team.inspector), http.StatusCreated)
	h.wantStatus(h.do("PUT", "/api/v1/admin/production/batches/"+batch+"/packaging",
		map[string]any{"packaging_type": "Insulated bag", "packed_kits": kits, "labels_verified": true, "complete": true}, team.kitchen), http.StatusOK)
	h.finishTasks(team.kitchen, batch)
	h.wantStatus(h.do("POST", "/api/v1/admin/production/batches/"+batch+"/complete", nil, team.kitchen), http.StatusOK)
}

// consume runs the readiness consumer as the worker would.
func (w *kitchenWeek) consume() int {
	w.h.t.Helper()
	n, err := w.h.ff.Fulfillment.Dispatch(context.Background())
	if err != nil {
		w.h.t.Fatalf("dispatch: %v", err)
	}
	w.h.drainJobs()
	return n
}

func (w *kitchenWeek) obligationFor(order string) map[string]any {
	w.h.t.Helper()
	var id string
	if err := w.h.pool.QueryRow(context.Background(), `SELECT id FROM fulfillment_obligations WHERE order_id = $1`, order).Scan(&id); err != nil {
		w.h.t.Fatalf("obligation for %s: %v", order, err)
	}
	res := w.h.do("GET", "/api/v1/admin/fulfillment/"+id, nil, w.ops.fmgr)
	w.h.wantStatus(res, http.StatusOK)
	return res.Data
}

// verifyAll checks every item of an obligation as correct.
func (w *kitchenWeek) verifyAll(o map[string]any, as reqOpt) map[string]any {
	h := w.h
	h.t.Helper()
	var res response
	for _, p := range list(h.t, o["packages"]) {
		for _, it := range list(h.t, obj(h.t, p)["items"]) {
			item := obj(h.t, it)
			res = h.do("POST", "/api/v1/admin/fulfillment/"+o["id"].(string)+"/verify", map[string]any{
				"item_id": item["id"], "counted_quantity": item["quantity"], "scanned_batch_code": item["batch_code"],
				"recipe_confirmed": true, "packaging_ok": true, "label_ok": true, "idempotency_key": "ok-" + item["id"].(string)}, as)
			h.wantStatus(res, http.StatusOK)
		}
	}
	return res.Data
}

// ---------------------------------------------------------------- stage 2

func TestReadinessEventsMakeOneObligationPerReadyOrder(t *testing.T) {
	w := newKitchenWeek(t)
	h := w.h
	ctx := context.Background()

	// Curry completes: Ana's whole order is ready; Ben still waits for buljol.
	w.cook(w.curry, 5)
	if n := w.consume(); n != 1 {
		t.Fatalf("readiness events dispatched = %d", n)
	}
	if n := h.count(`SELECT count(*) FROM fulfillment_obligations`); n != 1 {
		t.Fatalf("obligations after curry = %d", n)
	}
	a := w.obligationFor(w.orderA)
	if a["status"] != "ready_for_verification" || a["kits_required"] != float64(3) {
		t.Fatalf("Ana's obligation = %v", a)
	}
	pkgs := list(t, a["packages"])
	if len(pkgs) != 1 || len(list(t, obj(t, pkgs[0])["items"])) != 1 {
		t.Fatalf("packages = %v", pkgs)
	}
	item := obj(t, list(t, obj(t, pkgs[0])["items"])[0])
	if item["batch_id"] != w.curry || item["quantity"] != float64(3) || item["recipe_version"] != float64(1) {
		t.Errorf("item = %v", item)
	}

	// Replaying every event from the start creates nothing new.
	if err := consumer.Replay(ctx, h.pool, "fulfillment", 0); err != nil {
		t.Fatal(err)
	}
	w.consume()
	// Five workers handling the same batch at once still make one obligation per order.
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := h.ff.Fulfillment.ConsumeBatch(ctx, 0, w.curry); err != nil {
				t.Errorf("concurrent consume: %v", err)
			}
		}()
	}
	wg.Wait()
	if n := h.count(`SELECT count(*) FROM fulfillment_obligations`); n != 1 {
		t.Fatalf("obligations after replay and concurrent consumption = %d", n)
	}

	// Buljol completes: Ben's order is now ready, with both meals and both batches.
	w.cook(w.fish, 1)
	w.consume()
	b := w.obligationFor(w.orderB)
	items := list(t, obj(t, list(t, b["packages"])[0])["items"])
	if b["status"] != "ready_for_verification" || len(items) != 2 || b["kits_required"] != float64(3) {
		t.Fatalf("Ben's obligation = %v", b)
	}
	// The consumer's state is visible, with nothing failed.
	res := h.do("GET", "/api/v1/admin/fulfillment/consumer", nil, w.ops.fmgr)
	h.wantStatus(res, http.StatusOK)
	if len(list(t, res.Data["failed"])) != 0 || res.Data["last_event_id"] == float64(0) {
		t.Errorf("consumer = %v", res.Data)
	}
	// Nothing in commerce or production was changed by fulfillment.
	if n := h.count(`SELECT count(*) FROM orders WHERE status = 'ready_for_fulfillment'`); n != 2 {
		t.Errorf("orders ready = %d", n)
	}
}

func TestIncompleteProductionAndHoldsNeverMakeAnObligation(t *testing.T) {
	w := newKitchenWeek(t)
	h := w.h
	// A batch on quality hold cannot complete, so there is no readiness event.
	h.wantStatus(h.do("POST", "/api/v1/admin/production/batches/"+w.curry+"/start", nil, w.team.kitchen), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/production/batches/"+w.curry+"/output", map[string]any{"produced_kits": 5}, w.team.kitchen), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/quality/checks", map[string]any{"batch_id": w.curry, "template_id": w.qc, "result": "fail",
		"notes": "Seal failed on several bags."}, w.team.inspector), http.StatusCreated)
	h.wantError(h.do("POST", "/api/v1/admin/production/batches/"+w.curry+"/complete", nil, w.team.kitchen), http.StatusConflict, "quality_hold")
	w.consume()
	// Even when asked directly, an order whose batches are incomplete is not ready.
	out, err := h.ff.Fulfillment.ConsumeBatch(context.Background(), 0, w.curry)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range out {
		if o.Result != "not_ready" {
			t.Errorf("outcome = %+v", o)
		}
	}
	res := h.do("POST", "/api/v1/admin/fulfillment/sync", nil, w.ops.fmgr)
	h.wantStatus(res, http.StatusOK)
	if n := h.count(`SELECT count(*) FROM fulfillment_obligations`); n != 0 {
		t.Fatalf("obligations with production incomplete = %d", n)
	}
}

func TestRefundedOrderIsNotFulfilledAutomatically(t *testing.T) {
	w := newKitchenWeek(t)
	h := w.h
	// Ana's payment is fully refunded before her kits are ready.
	var payment string
	if err := h.pool.QueryRow(context.Background(), `SELECT payment_id FROM orders WHERE id = $1`, w.orderA).Scan(&payment); err != nil {
		t.Fatal(err)
	}
	var amount float64
	if err := h.pool.QueryRow(context.Background(), `SELECT amount_minor FROM payments WHERE id = $1`, payment).Scan(&amount); err != nil {
		t.Fatal(err)
	}
	finance := bearer(h.adminLogin("finance@mise.test", "finance"))
	h.wantStatus(h.do("POST", "/api/v1/admin/payments/"+payment+"/refunds",
		map[string]any{"amount_minor": amount, "reason": "Customer moved abroad", "request_key": "refund-ana-1"}, finance), http.StatusCreated)
	w.cook(w.curry, 5)
	w.consume()
	a := w.obligationFor(w.orderA)
	if a["status"] != "exception" || a["open_blocking_exceptions"] != float64(1) {
		t.Fatalf("refunded order's obligation = %v", a)
	}
	// It cannot be verified or released, and no delivery exists.
	h.wantError(h.do("POST", "/api/v1/admin/fulfillment/"+a["id"].(string)+"/release", nil, w.ops.fmgr), http.StatusConflict, "invalid_transition")
	if n := h.count(`SELECT count(*) FROM deliveries`); n != 0 {
		t.Errorf("deliveries = %d", n)
	}
	res := h.do("GET", "/api/v1/admin/delivery/exceptions", nil, w.ops.fmgr)
	h.wantStatus(res, http.StatusOK)
	exc := list(t, res.Data["items"])
	if len(exc) != 1 || obj(t, exc[0])["type"] != "eligibility" || !strings.Contains(obj(t, exc[0])["notes"].(string), "refunded") {
		t.Fatalf("exceptions = %v", exc)
	}
	// Resolving the exception does not resume a refunded order.
	h.wantError(h.do("PATCH", "/api/v1/admin/delivery/exceptions/"+obj(t, exc[0])["id"].(string),
		map[string]any{"status": "resolved", "resolution": "Kits kept for staff meal"}, w.ops.fmgr), http.StatusUnprocessableEntity, "not_eligible")
}

func TestVerificationNeedsTheRightKitsAndRecordsDiscrepancies(t *testing.T) {
	w := newKitchenWeek(t)
	h := w.h
	w.cook(w.curry, 5)
	w.cook(w.fish, 1)
	w.consume()
	b := w.obligationFor(w.orderB)
	id := b["id"].(string)
	items := list(t, obj(t, list(t, b["packages"])[0])["items"])
	var curryItem, fishItem map[string]any
	for _, x := range items {
		if m := obj(t, x); m["batch_id"] == w.curry {
			curryItem = m
		} else {
			fishItem = m
		}
	}
	verify := func(as reqOpt, item map[string]any, counted any, code, key string) response {
		return h.do("POST", "/api/v1/admin/fulfillment/"+id+"/verify", map[string]any{"item_id": item["id"], "counted_quantity": counted,
			"scanned_batch_code": code, "recipe_confirmed": true, "packaging_ok": true, "label_ok": true, "idempotency_key": key}, as)
	}
	// Staff without verify permission cannot; support cannot.
	h.wantStatus(verify(w.ops.support, curryItem, 2, curryItem["batch_code"].(string), "k1"), http.StatusForbidden)
	h.wantStatus(verify(w.ops.courier, curryItem, 2, curryItem["batch_code"].(string), "k1"), http.StatusForbidden)
	// A quantity alone does not verify: the batch code must be scanned.
	h.wantStatus(verify(w.ops.fstaff, curryItem, 2, "", "k1"), http.StatusUnprocessableEntity)

	// Two packers verify the same item at once: one wins, the other is told.
	var wg sync.WaitGroup
	statuses := make([]int, 2)
	for i := range statuses {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			statuses[i] = verify(w.ops.fstaff, curryItem, 2, curryItem["batch_code"].(string), "packer-"+string(rune('a'+i))).Status
		}(i)
	}
	wg.Wait()
	if !(statuses[0] == 200 && statuses[1] == 409 || statuses[0] == 409 && statuses[1] == 200) {
		t.Fatalf("concurrent verification statuses = %v", statuses)
	}
	// A retried tap (same key) is answered, not counted twice.
	winner := "packer-a"
	if statuses[1] == 200 {
		winner = "packer-b"
	}
	h.wantStatus(verify(w.ops.fstaff, curryItem, 2, curryItem["batch_code"].(string), winner), http.StatusOK)
	if n := h.count(`SELECT count(*) FROM fulfillment_verifications WHERE obligation_id = $1`, id); n != 1 {
		t.Fatalf("verification rows = %d", n)
	}

	// The wrong kit (a curry kit scanned for the buljol line) is a discrepancy.
	res := verify(w.ops.fstaff, fishItem, 1, curryItem["batch_code"].(string), "k2")
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != "exception" || res.Data["resume_status"] != "verification_in_progress" || res.Data["open_blocking_exceptions"] != float64(1) {
		t.Fatalf("after wrong kit = %v", res.Data)
	}
	h.wantError(h.do("POST", "/api/v1/admin/fulfillment/"+id+"/release", nil, w.ops.fmgr), http.StatusConflict, "invalid_transition")
	// Resolving the discrepancy sends the item back to be checked again.
	exc := list(t, h.do("GET", "/api/v1/admin/delivery/exceptions", nil, w.ops.fmgr).Data["items"])
	if len(exc) != 1 || obj(t, exc[0])["type"] != "kit_discrepancy" {
		t.Fatalf("exceptions = %v", exc)
	}
	res = h.do("PATCH", "/api/v1/admin/delivery/exceptions/"+obj(t, exc[0])["id"].(string),
		map[string]any{"status": "resolved", "resolution": "Swapped in the buljol kit from batch."}, w.ops.fmgr)
	h.wantStatus(res, http.StatusOK)
	b = w.obligationFor(w.orderB)
	if b["status"] != "verification_in_progress" {
		t.Fatalf("after resolution = %v", b["status"])
	}
	res = verify(w.ops.fstaff, fishItem, 1, fishItem["batch_code"].(string), "k3")
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != "verified" {
		t.Fatalf("after re-check = %v", res.Data)
	}
	// Staff can verify but not release.
	h.wantStatus(h.do("POST", "/api/v1/admin/fulfillment/"+id+"/release", nil, w.ops.fstaff), http.StatusForbidden)
}

func TestReleaseSnapshotsTheAddressAndFlagsCoverage(t *testing.T) {
	w := newKitchenWeek(t)
	h := w.h
	ctx := context.Background()
	w.cook(w.curry, 5)
	w.consume()
	a := w.obligationFor(w.orderA)
	w.verifyAll(a, w.ops.fstaff)

	// A flagged batch blocks release until the problem is resolved.
	res := h.do("POST", "/api/v1/admin/fulfillment/batches/"+w.curry+"/flag", map[string]any{"reason": "Two bags found unsealed at packing."}, w.ops.fstaff)
	h.wantStatus(res, http.StatusCreated)
	flag := res.Data["exception_id"].(string)
	h.wantError(h.do("POST", "/api/v1/admin/fulfillment/"+a["id"].(string)+"/release", nil, w.ops.fmgr), http.StatusUnprocessableEntity, "batch_flagged")
	h.wantStatus(h.do("PATCH", "/api/v1/admin/delivery/exceptions/"+flag,
		map[string]any{"status": "resolved", "resolution": "Bags resealed and re-inspected by QA."}, w.ops.fmgr), http.StatusOK)

	// A stale version is refused; the right one releases.
	stale := 1
	h.wantError(h.do("POST", "/api/v1/admin/fulfillment/"+a["id"].(string)+"/release", map[string]any{"version": stale}, w.ops.fmgr), http.StatusConflict, "stale")
	res = h.do("POST", "/api/v1/admin/fulfillment/"+a["id"].(string)+"/release", map[string]any{"version": a["version"].(float64) + 1}, w.ops.fmgr)
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != "ready_for_dispatch" || res.Data["delivery_id"] == nil {
		t.Fatalf("released = %v", res.Data)
	}
	dl := res.Data["delivery_id"].(string)
	// Port of Spain is served but no zone covers it yet: flagged, not assumed.
	var validation, zoneSource, line1 string
	if err := h.pool.QueryRow(ctx, `SELECT address_validation, zone_source, address->>'line1' FROM deliveries WHERE id = $1`, dl).
		Scan(&validation, &zoneSource, &line1); err != nil {
		t.Fatal(err)
	}
	if validation != "unzoned" || zoneSource != "none" || line1 == "" {
		t.Errorf("delivery coverage = %s %s %q", validation, zoneSource, line1)
	}
	// The snapshot does not follow later address changes.
	if _, err := h.pool.Exec(ctx, `UPDATE customer_addresses SET line1 = 'Somewhere else' WHERE customer_id = (SELECT customer_id FROM orders WHERE id = $1)`, w.orderA); err != nil {
		t.Fatal(err)
	}
	var after string
	if err := h.pool.QueryRow(ctx, `SELECT address->>'line1' FROM deliveries WHERE id = $1`, dl).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != line1 {
		t.Errorf("address snapshot changed: %q → %q", line1, after)
	}
	// Labels name the package, never the full name, and say they are not certified.
	res = h.do("GET", "/api/v1/admin/fulfillment/"+a["id"].(string)+"/labels", nil, w.ops.fstaff)
	h.wantStatus(res, http.StatusOK)
	l := obj(t, list(t, res.Data["items"])[0])
	if l["certified"] != false || !strings.Contains(l["notice"].(string), "Not certified") || strings.Contains(l["recipient"].(string), "@") {
		t.Errorf("label = %v", l)
	}
	// Traceability stays at batch level.
	res = h.do("GET", "/api/v1/admin/fulfillment/"+a["id"].(string)+"/trace", nil, w.ops.fmgr)
	h.wantStatus(res, http.StatusOK)
	if res.Data["level"] != "batch" || obj(t, list(t, res.Data["items"])[0])["batch_id"] != w.curry {
		t.Errorf("trace = %v", res.Data)
	}
	// Releasing twice is refused, and there is one delivery.
	h.wantError(h.do("POST", "/api/v1/admin/fulfillment/"+a["id"].(string)+"/release", nil, w.ops.fmgr), http.StatusConflict, "invalid_transition")
	if n := h.count(`SELECT count(*) FROM deliveries`); n != 1 {
		t.Errorf("deliveries = %d", n)
	}
}
