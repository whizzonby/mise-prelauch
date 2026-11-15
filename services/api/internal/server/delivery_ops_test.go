package server_test

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// ---------------------------------------------------------------- configuration fixtures

type deliveryConfig struct {
	zone, partner, courier, window string
}

const serviceDay = "2026-11-19"

// configure sets up a zone covering Port of Spain, an approved service day
// with an approved window, a fully qualified manual partner covering the
// zone, a courier with a login, and a proof policy. Every step uses the API
// a manager would use.
func (w *kitchenWeek) configure() deliveryConfig {
	h, t := w.h, w.h.t
	t.Helper()
	var c deliveryConfig
	res := h.do("POST", "/api/v1/admin/delivery/zones", map[string]any{"code": "POS", "name": "Port of Spain", "status": "active",
		"regions": []string{"port-of-spain"}}, w.ops.dmgr)
	h.wantStatus(res, http.StatusCreated)
	c.zone = res.Data["id"].(string)
	res = h.do("PUT", "/api/v1/admin/delivery/calendar", map[string]any{"date": serviceDay, "status": "open",
		"windows": []map[string]any{{"label": "Morning", "starts_at": "09:00", "ends_at": "13:00", "status": "approved"}}}, w.ops.dmgr)
	h.wantStatus(res, http.StatusOK)
	c.window = obj(t, list(t, res.Data["windows"])[0])["id"].(string)
	c.partner = w.qualifiedPartner("Island Couriers (test)", c.zone)
	res = h.do("POST", "/api/v1/admin/delivery/couriers", map[string]any{"partner_id": c.partner, "name": "Rider One",
		"user_email": "rider@mise.test"}, w.ops.dmgr)
	h.wantStatus(res, http.StatusCreated)
	c.courier = res.Data["id"].(string)
	admin := bearer(h.adminLogin("boss@mise.test", "admin"))
	h.wantStatus(h.do("PUT", "/api/v1/admin/delivery/settings", map[string]any{"proof_methods": []string{"confirmation_code", "recipient_acknowledgment"},
		"proof_policy_ref": "OPS-POD-1 (test)", "delivery_offset_min_days": 0, "delivery_offset_max_days": 13, "delivery_week_rule_ref": "OPS-DAYS-1 (test)"}, admin), http.StatusOK)
	return c
}

func (w *kitchenWeek) qualifiedPartner(name, zone string) string {
	h, t := w.h, w.h.t
	t.Helper()
	res := h.do("POST", "/api/v1/admin/delivery/partners", map[string]any{"name": name, "integration_mode": "manual",
		"agreement_status": "signed", "agreement_ref": "AGR-TEST-1", "zone_ids": []string{zone}}, w.ops.dmgr)
	h.wantStatus(res, http.StatusCreated)
	id := res.Data["id"].(string)
	for _, c := range []string{"contract", "service_coverage", "handling_capability", "reliability", "escalation_process", "delivery_evidence", "handling_procedures"} {
		h.wantStatus(h.do("PUT", "/api/v1/admin/delivery/partners/"+id+"/criteria", map[string]any{"criterion": c, "met": true, "evidence": "Checked in test"}, w.ops.dmgr), http.StatusOK)
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/partners/"+id+"/status", map[string]any{"status": "pending_approval", "reason": "Ready"}, w.ops.dmgr), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/partners/"+id+"/status", map[string]any{"status": "approved", "reason": "All criteria met"}, w.ops.dmgr), http.StatusOK)
	return id
}

// released takes both orders through production, verification and release
// and returns their delivery ids (Ana, Ben).
func (w *kitchenWeek) released() (string, string) {
	w.h.t.Helper()
	w.cook(w.curry, 5)
	w.cook(w.fish, 1)
	w.consume()
	ids := []string{}
	for _, order := range []string{w.orderA, w.orderB} {
		o := w.verifyAll(w.obligationFor(order), w.ops.fstaff)
		res := w.h.do("POST", "/api/v1/admin/fulfillment/"+o["id"].(string)+"/release", nil, w.ops.fmgr)
		w.h.wantStatus(res, http.StatusOK)
		ids = append(ids, res.Data["delivery_id"].(string))
	}
	return ids[0], ids[1]
}

func (w *kitchenWeek) schedule(d string, c deliveryConfig) {
	w.h.t.Helper()
	w.h.wantStatus(w.h.do("POST", "/api/v1/admin/delivery/deliveries/"+d+"/schedule", map[string]any{"service_date": serviceDay, "window_id": c.window}, w.ops.dispatcher), http.StatusOK)
}

func (w *kitchenWeek) run(c deliveryConfig) string {
	w.h.t.Helper()
	res := w.h.do("POST", "/api/v1/admin/delivery/dispatch-runs", map[string]any{"service_date": serviceDay, "partner_id": c.partner, "courier_id": c.courier}, w.ops.dispatcher)
	w.h.wantStatus(res, http.StatusCreated)
	return res.Data["id"].(string)
}

func (w *kitchenWeek) capacity(n int) {
	w.h.t.Helper()
	w.h.wantStatus(w.h.do("PUT", "/api/v1/admin/delivery/capacity", map[string]any{"date": serviceDay, "capacity": n}, w.ops.dmgr), http.StatusOK)
}

func (w *kitchenWeek) handoverAll(run, key string, as reqOpt) response {
	h, t := w.h, w.h.t
	t.Helper()
	r := h.do("GET", "/api/v1/admin/delivery/dispatch-runs/"+run, nil, w.ops.dispatcher)
	pk := []map[string]any{}
	for _, s := range list(t, obj(t, r.Data["manifest"])["content"].(map[string]any)["stops"]) {
		for _, p := range list(t, obj(t, s)["packages"]) {
			pk = append(pk, map[string]any{"package_id": obj(t, p)["id"], "outcome": "accepted"})
		}
	}
	return h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/handoffs", map[string]any{"to_party": "Rider One", "packages": pk,
		"idempotency_key": key}, as)
}

func (w *kitchenWeek) deliveryData(id string, as reqOpt) map[string]any {
	w.h.t.Helper()
	res := w.h.do("GET", "/api/v1/admin/delivery/deliveries/"+id, nil, as)
	w.h.wantStatus(res, http.StatusOK)
	return res.Data
}

// ---------------------------------------------------------------- stage 3: configuration

func TestPartnersNeedQualificationAndApproval(t *testing.T) {
	w := newKitchenWeek(t)
	h := w.h
	res := h.do("POST", "/api/v1/admin/delivery/zones", map[string]any{"code": "POS", "name": "Port of Spain", "status": "active", "regions": []string{"port-of-spain"}}, w.ops.dmgr)
	h.wantStatus(res, http.StatusCreated)
	zone := res.Data["id"].(string)
	// A region belongs to one zone; a duplicate code is refused.
	h.wantError(h.do("POST", "/api/v1/admin/delivery/zones", map[string]any{"code": "POS", "name": "Again"}, w.ops.dmgr), http.StatusConflict, "duplicate")
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/zones", map[string]any{"code": "WEST", "name": "West", "regions": []string{"port-of-spain"}}, w.ops.dmgr), http.StatusUnprocessableEntity)
	// Dispatchers cannot configure zones or approve partners.
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/zones", map[string]any{"code": "X1", "name": "X"}, w.ops.dispatcher), http.StatusForbidden)

	res = h.do("POST", "/api/v1/admin/delivery/partners", map[string]any{"name": "Prospect Delivery Co (test)", "zone_ids": []string{zone}}, w.ops.dmgr)
	h.wantStatus(res, http.StatusCreated)
	p := res.Data["id"].(string)
	if res.Data["status"] != "draft" || res.Data["assignable"] != false || res.Data["agreement_status"] != "none" {
		t.Fatalf("new partner = %v", res.Data)
	}
	// Not approvable without a signed agreement and every criterion met.
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/partners/"+p+"/status", map[string]any{"status": "pending_approval", "reason": "Review"}, w.ops.dmgr), http.StatusOK)
	h.wantError(h.do("POST", "/api/v1/admin/delivery/partners/"+p+"/status", map[string]any{"status": "approved", "reason": "Looks fine"}, w.ops.dmgr), http.StatusUnprocessableEntity, "not_qualified")
	h.wantStatus(h.do("PATCH", "/api/v1/admin/delivery/partners/"+p, map[string]any{"name": "Prospect Delivery Co (test)", "agreement_status": "signed"}, w.ops.dmgr), http.StatusUnprocessableEntity)
	h.wantStatus(h.do("PATCH", "/api/v1/admin/delivery/partners/"+p, map[string]any{"name": "Prospect Delivery Co (test)", "agreement_status": "signed",
		"agreement_ref": "AGR-9", "zone_ids": []string{zone}}, w.ops.dmgr), http.StatusOK)
	h.wantError(h.do("POST", "/api/v1/admin/delivery/partners/"+p+"/status", map[string]any{"status": "approved", "reason": "Signed"}, w.ops.dmgr), http.StatusUnprocessableEntity, "not_qualified")
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/partners/"+p+"/status", map[string]any{"status": "approved", "reason": "x"}, w.ops.dispatcher), http.StatusForbidden)

	// An unapproved partner cannot take a live run.
	h.wantError(h.do("POST", "/api/v1/admin/delivery/dispatch-runs", map[string]any{"service_date": serviceDay, "partner_id": p}, w.ops.dispatcher),
		http.StatusUnprocessableEntity, "partner_not_approved")
	// Unless a documented exception policy allows it, with a reason.
	admin := bearer(h.adminLogin("boss@mise.test", "admin"))
	h.wantStatus(h.do("PUT", "/api/v1/admin/delivery/settings", map[string]any{"allow_unapproved_partners": true}, admin), http.StatusUnprocessableEntity)
	h.wantStatus(h.do("PUT", "/api/v1/admin/delivery/settings", map[string]any{"allow_unapproved_partners": true, "unapproved_partner_policy_ref": "OPS-EXC-2"}, admin), http.StatusOK)
	h.wantError(h.do("POST", "/api/v1/admin/delivery/dispatch-runs", map[string]any{"service_date": serviceDay, "partner_id": p}, w.ops.dispatcher),
		http.StatusUnprocessableEntity, "partner_not_approved")
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/dispatch-runs", map[string]any{"service_date": serviceDay, "partner_id": p,
		"partner_override": "Usual partner unavailable; ops lead approved by phone."}, w.ops.dispatcher), http.StatusCreated)

	// Fully qualified, approved, history kept; withdrawing a criterion suspends.
	q := w.qualifiedPartner("Qualified Co (test)", zone)
	res = h.do("GET", "/api/v1/admin/delivery/partners/"+q, nil, w.ops.dispatcher)
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != "approved" || len(list(t, res.Data["history"])) != 3 {
		t.Fatalf("approved partner = %v", res.Data)
	}
	res = h.do("PUT", "/api/v1/admin/delivery/partners/"+q+"/criteria", map[string]any{"criterion": "reliability", "met": false, "evidence": "Missed pickups"}, w.ops.dmgr)
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != "suspended" {
		t.Errorf("after withdrawal = %v", res.Data["status"])
	}
	// Photo proof cannot be enabled: there is no secure storage.
	h.wantStatus(h.do("PUT", "/api/v1/admin/delivery/settings", map[string]any{"proof_methods": []string{"photo"}, "proof_policy_ref": "x"}, admin), http.StatusUnprocessableEntity)
	res = h.do("GET", "/api/v1/admin/delivery/settings", nil, w.ops.dmgr)
	if !strings.Contains(string(res.Raw), "No proof-of-delivery policy") {
		t.Errorf("settings blockers = %s", res.Raw)
	}
}

func TestSchedulingCoverageAndCapacity(t *testing.T) {
	w := newKitchenWeek(t)
	h := w.h
	c := w.configure()
	a, b := w.released()
	// Released after zones exist: the coverage rule applied.
	if d := w.deliveryData(a, w.ops.dispatcher); d["address_validation"] != "serviceable" || d["zone_source"] != "coverage_rule" {
		t.Fatalf("coverage = %v %v", d["address_validation"], d["zone_source"])
	}
	// Not a service day; a closed day; an unapproved window.
	h.wantError(h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/schedule", map[string]any{"service_date": "2026-11-20"}, w.ops.dispatcher),
		http.StatusUnprocessableEntity, "not_a_service_day")
	res := h.do("PUT", "/api/v1/admin/delivery/calendar", map[string]any{"date": "2026-11-21", "status": "open",
		"windows": []map[string]any{{"label": "Evening", "starts_at": "17:00", "ends_at": "20:00"}}}, w.ops.dmgr)
	h.wantStatus(res, http.StatusOK)
	draft := obj(t, list(t, res.Data["windows"])[0])["id"].(string)
	h.wantError(h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/schedule", map[string]any{"service_date": "2026-11-21", "window_id": draft}, w.ops.dispatcher),
		http.StatusUnprocessableEntity, "window_not_approved")
	h.wantStatus(h.do("PUT", "/api/v1/admin/delivery/calendar", map[string]any{"date": "2026-11-21", "zone_id": c.zone, "status": "closed", "notes": "Holiday"}, w.ops.dmgr), http.StatusOK)
	h.wantError(h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/schedule", map[string]any{"service_date": "2026-11-21"}, w.ops.dispatcher),
		http.StatusUnprocessableEntity, "not_a_service_day")
	w.schedule(a, c)
	w.schedule(b, c)

	run := w.run(c)
	// Capacity not configured: unknown, never unlimited.
	h.wantError(h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/stops", map[string]any{"delivery_id": a}, w.ops.dispatcher), http.StatusUnprocessableEntity, "capacity")
	// Capacity 1: two dispatchers assigning both deliveries at once — one wins.
	w.capacity(1)
	var wg sync.WaitGroup
	st := make([]int, 2)
	for i, d := range []string{a, b} {
		wg.Add(1)
		go func(i int, d string) {
			defer wg.Done()
			st[i] = h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/stops", map[string]any{"delivery_id": d}, w.ops.dispatcher).Status
		}(i, d)
	}
	wg.Wait()
	if !(st[0] == 200 && st[1] == 422 || st[0] == 422 && st[1] == 200) {
		t.Fatalf("capacity race statuses = %v", st)
	}
	if n := h.count(`SELECT count(*) FROM dispatch_stops WHERE status = 'active'`); n != 1 {
		t.Fatalf("active stops = %d", n)
	}
	// Capacity cannot drop below what is assigned.
	h.wantError(h.do("PUT", "/api/v1/admin/delivery/capacity", map[string]any{"date": serviceDay, "capacity": 0}, w.ops.dmgr), http.StatusUnprocessableEntity, "below_assigned")
	// Going beyond known capacity needs an explicit, reasoned override.
	other := b
	if st[1] == 200 {
		other = a
	}
	res = h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/stops", map[string]any{"delivery_id": other, "capacity_override": "Rider confirmed an extra drop"}, w.ops.dispatcher)
	h.wantStatus(res, http.StatusOK)
	stops := list(t, res.Data["stops"])
	if len(stops) != 2 || obj(t, stops[1])["capacity_override"] == nil {
		t.Fatalf("stops = %v", stops)
	}
	// Assigning again (or by a second run) is refused.
	run2 := w.run(c)
	h.wantError(h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run2+"/stops", map[string]any{"delivery_id": a, "capacity_override": "x x x"}, w.ops.dispatcher),
		http.StatusConflict, "already_assigned")
}

// ---------------------------------------------------------------- stages 4–6: the end-to-end path

func TestDispatchCustodyTrackingProofAndCompletion(t *testing.T) {
	w := newKitchenWeek(t)
	h := w.h
	ctx := context.Background()
	c := w.configure()
	a, b := w.released()
	w.schedule(a, c)
	w.schedule(b, c)
	w.capacity(10)
	run := w.run(c)
	// Two dispatchers assign the same delivery at once: exactly one stop.
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/stops", map[string]any{"delivery_id": a}, w.ops.dispatcher)
		}()
	}
	wg.Wait()
	if n := h.count(`SELECT count(*) FROM dispatch_stops WHERE delivery_id = $1 AND status = 'active'`, a); n != 1 {
		t.Fatalf("active stops for one delivery = %d", n)
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/stops", map[string]any{"delivery_id": b}, w.ops.dispatcher), http.StatusOK)

	// Nothing is handed over before release, and no status can skip ahead.
	h.wantError(h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/events", map[string]any{"status": "in_transit", "idempotency_key": "early"}, w.ops.coord),
		http.StatusConflict, "invalid_transition")
	res := h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/finalize", nil, w.ops.dispatcher)
	h.wantStatus(res, http.StatusOK)
	m := obj(t, res.Data["manifest"])
	content := obj(t, m["content"])
	stops := list(t, content["stops"])
	if len(stops) != 2 || m["package_count"] != float64(2) || m["revision"] != float64(1) {
		t.Fatalf("manifest = %v", m)
	}
	// The manifest's packages are exactly the deliveries' packages.
	for _, s := range stops {
		st := obj(t, s)
		var want string
		if err := h.pool.QueryRow(ctx, `SELECT p.code FROM fulfillment_packages p JOIN deliveries d ON d.obligation_id = p.obligation_id WHERE d.number = $1`,
			st["delivery"]).Scan(&want); err != nil {
			t.Fatal(err)
		}
		if obj(t, list(t, st["packages"])[0])["code"] != want || strings.Contains(string(res.Raw), "@example.com") {
			t.Errorf("manifest stop = %v", st)
		}
	}
	// The CSV is served through the API, never cached, and the courier sees only their own run.
	res = h.do("GET", "/api/v1/admin/delivery/dispatch-runs/"+run+"/manifest?format=csv", nil, w.ops.courier)
	h.wantStatus(res, http.StatusOK)
	if res.Header.Get("Cache-Control") != "no-store, private" || !strings.Contains(string(res.Raw), "package_ids") {
		t.Errorf("manifest csv headers = %v", res.Header)
	}
	other := h.do("POST", "/api/v1/admin/delivery/dispatch-runs", map[string]any{"service_date": serviceDay, "partner_id": c.partner}, w.ops.dispatcher).Data["id"].(string)
	h.wantStatus(h.do("GET", "/api/v1/admin/delivery/dispatch-runs/"+other, nil, w.ops.courier), http.StatusNotFound)
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/release", nil, w.ops.courier), http.StatusForbidden)
	// Changing stops needs the run back in draft; the manifest is superseded, not lost.
	h.wantError(h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/stops", map[string]any{"delivery_id": a}, w.ops.dispatcher), http.StatusConflict, "invalid_transition")
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/reopen", map[string]any{"reason": "Swap stop order"}, w.ops.dispatcher), http.StatusOK)
	runData := h.do("GET", "/api/v1/admin/delivery/dispatch-runs/"+run, nil, w.ops.dispatcher).Data
	ids := []string{}
	for i := len(list(t, runData["stops"])) - 1; i >= 0; i-- {
		ids = append(ids, obj(t, list(t, runData["stops"])[i])["id"].(string))
	}
	h.wantStatus(h.do("PUT", "/api/v1/admin/delivery/dispatch-runs/"+run+"/sequence", map[string]any{"stop_ids": ids}, w.ops.dispatcher), http.StatusOK)
	res = h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/finalize", nil, w.ops.dispatcher)
	h.wantStatus(res, http.StatusOK)
	if obj(t, res.Data["manifest"])["revision"] != float64(2) || h.count(`SELECT count(*) FROM dispatch_manifests WHERE run_id = $1`, run) != 2 {
		t.Fatalf("manifest after reopen = %v", res.Data["manifest"])
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/release", nil, w.ops.dispatcher), http.StatusOK)
	if d := w.deliveryData(a, w.ops.dispatcher); d["status"] != "ready_for_pickup" {
		t.Fatalf("after release = %v", d["status"])
	}
	// Release alone is not custody.
	h.wantError(h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/events", map[string]any{"status": "in_transit", "idempotency_key": "e1"}, w.ops.coord),
		http.StatusConflict, "invalid_transition")
	// Two couriers confirm the same handoff at once (different keys): one record.
	st := make([]int, 2)
	for i := range st {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			st[i] = w.handoverAll(run, "handoff-"+string(rune('a'+i)), w.ops.dispatcher).Status
		}(i)
	}
	wg.Wait()
	if !(st[0] == 200 && st[1] == 409 || st[0] == 409 && st[1] == 200) {
		t.Fatalf("concurrent handoff statuses = %v", st)
	}
	// The same handoff resent (same key) is accepted and not duplicated.
	key := "handoff-a"
	if st[1] == 200 {
		key = "handoff-b"
	}
	h.wantStatus(w.handoverAll(run, key, w.ops.dispatcher), http.StatusOK)
	if n := h.count(`SELECT count(*) FROM delivery_handoffs WHERE run_id = $1`, run); n != 1 {
		t.Fatalf("handoffs = %d", n)
	}
	var orderStatus, fstatus string
	if err := h.pool.QueryRow(ctx, `SELECT o.status, f.status FROM deliveries d JOIN orders o ON o.id = d.order_id
		JOIN fulfillment_obligations f ON f.id = d.obligation_id WHERE d.id = $1`, a).Scan(&orderStatus, &fstatus); err != nil {
		t.Fatal(err)
	}
	if orderStatus != "out_for_delivery" || fstatus != "handed_over" {
		t.Fatalf("after handoff: order %s, fulfillment %s", orderStatus, fstatus)
	}

	// The courier records progress on their own delivery; a duplicate partner message is one event.
	for i := 0; i < 2; i++ {
		h.wantStatus(h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/events", map[string]any{"status": "in_transit", "source": "partner",
			"partner_reference": "IC-123", "idempotency_key": "partner-evt-1"}, w.ops.courier), http.StatusOK)
	}
	if n := h.count(`SELECT count(*) FROM delivery_tracking_events WHERE delivery_id = $1 AND to_status = 'in_transit'`, a); n != 1 {
		t.Fatalf("in transit events = %d", n)
	}
	// A location is only kept when verified.
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/events", map[string]any{"status": "note", "notes": "Traffic on the highway",
		"location": "Somewhere", "idempotency_key": "n1"}, w.ops.courier), http.StatusOK)
	if n := h.count(`SELECT count(*) FROM delivery_tracking_events WHERE location IS NOT NULL`); n != 0 {
		t.Errorf("unverified locations stored = %d", n)
	}
	// Tracking history is append-only in the database.
	if _, err := h.pool.Exec(ctx, `UPDATE delivery_tracking_events SET notes = 'edited' WHERE delivery_id = $1`, a); err == nil {
		t.Error("tracking events could be edited")
	}

	// Completing needs accepted proof; a wrong code is kept as rejected.
	h.wantError(h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/deliver", map[string]any{"idempotency_key": "d1"}, w.ops.courier),
		http.StatusUnprocessableEntity, "proof_required")
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/proof", map[string]any{"method": "photo", "idempotency_key": "p0"}, w.ops.courier), http.StatusUnprocessableEntity)
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/proof", map[string]any{"method": "confirmation_code", "code": "000000", "idempotency_key": "p1"}, w.ops.courier), http.StatusOK)
	h.wantError(h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/deliver", map[string]any{"idempotency_key": "d1"}, w.ops.courier),
		http.StatusUnprocessableEntity, "proof_required")
	// The customer has the right code in their account; Ben's code does not work for Ana.
	res = h.do("GET", "/api/v1/me/orders/"+w.orderA+"/delivery", nil, w.tokA)
	h.wantStatus(res, http.StatusOK)
	code := res.Data["confirmation_code"].(string)
	if res.Data["stage"] != "out_for_delivery" || res.Data["live_tracking"] != false || strings.Contains(string(res.Raw), "Island Couriers") ||
		strings.Contains(string(res.Raw), "in_transit") {
		t.Fatalf("customer view = %s", res.Raw)
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/deliveries/"+b+"/events", map[string]any{"status": "in_transit", "idempotency_key": "b1"}, w.ops.courier), http.StatusOK)
	bCode := h.do("GET", "/api/v1/me/orders/"+w.orderB+"/delivery", nil, w.tokB).Data["confirmation_code"].(string)
	if bCode == code {
		t.Fatal("two deliveries share a confirmation code")
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/proof", map[string]any{"method": "confirmation_code", "code": bCode, "idempotency_key": "p2"}, w.ops.courier), http.StatusOK)
	// Proof submitted twice is one accepted proof.
	for i := 0; i < 2; i++ {
		h.wantStatus(h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/proof", map[string]any{"method": "confirmation_code", "code": code, "idempotency_key": "p3"}, w.ops.courier), http.StatusOK)
	}
	if n := h.count(`SELECT count(*) FROM delivery_proofs WHERE delivery_id = $1 AND status = 'accepted'`, a); n != 1 {
		t.Fatalf("accepted proofs = %d", n)
	}
	if n := h.count(`SELECT count(*) FROM delivery_proofs WHERE delivery_id = $1 AND status = 'rejected'`, a); n != 2 {
		t.Errorf("rejected proofs = %d", n)
	}
	res = h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/deliver", map[string]any{"idempotency_key": "d2"}, w.ops.courier)
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != "delivered" {
		t.Fatalf("after deliver = %v", res.Data["status"])
	}
	if err := h.pool.QueryRow(ctx, `SELECT o.status, f.status FROM deliveries d JOIN orders o ON o.id = d.order_id
		JOIN fulfillment_obligations f ON f.id = d.obligation_id WHERE d.id = $1`, a).Scan(&orderStatus, &fstatus); err != nil {
		t.Fatal(err)
	}
	if orderStatus != "delivered" || fstatus != "fulfilled" {
		t.Fatalf("after delivery: order %s, fulfillment %s", orderStatus, fstatus)
	}
	// A delivered delivery cannot be silently undone.
	h.wantError(h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/fail", map[string]any{"reason": "refused", "idempotency_key": "f1"}, w.ops.coord),
		http.StatusConflict, "invalid_transition")
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/correct", map[string]any{"to": "delivery_failed", "reason": "Wrong house"}, w.ops.coord), http.StatusForbidden)

	// The customer timeline shows real milestones in friendly words.
	res = h.do("GET", "/api/v1/me/orders/"+w.orderA+"/delivery", nil, w.tokA)
	h.wantStatus(res, http.StatusOK)
	labels := []string{}
	for _, m := range list(t, res.Data["timeline"]) {
		if obj(t, m)["done"] == true {
			labels = append(labels, obj(t, m)["label"].(string))
		}
	}
	if strings.Join(labels, "|") != "Order confirmed|Kitchen preparing|Packed and checked|Ready for delivery|Out for delivery|Delivered" ||
		res.Data["confirmation_code"] != nil {
		t.Fatalf("timeline = %v (code %v)", labels, res.Data["confirmation_code"])
	}
	// Another customer cannot see it.
	h.wantStatus(h.do("GET", "/api/v1/me/orders/"+w.orderA+"/delivery", nil, w.tokB), http.StatusNotFound)
	// Support sees the history (but not proof details without permission).
	res = h.do("GET", "/api/v1/admin/delivery/deliveries?status=all&customer_id="+w.deliveryData(a, w.ops.dispatcher)["customer_id"].(string), nil, w.ops.support)
	h.wantStatus(res, http.StatusOK)
	if len(list(t, res.Data["items"])) != 1 {
		t.Fatalf("support list = %v", res.Data)
	}
	if d := w.deliveryData(a, w.ops.support); len(list(t, d["events"])) < 5 {
		t.Errorf("support history = %v", d["events"])
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/events", map[string]any{"status": "note", "notes": "x", "idempotency_key": "s"}, w.ops.support), http.StatusForbidden)

	// Ben's delivery completes by recipient acknowledgement; then the run reconciles.
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/deliveries/"+b+"/proof", map[string]any{"method": "recipient_acknowledgment", "recipient_name": "Ben", "idempotency_key": "bp"}, w.ops.courier), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/deliveries/"+b+"/deliver", map[string]any{"idempotency_key": "bd"}, w.ops.courier), http.StatusOK)
	res = h.do("GET", "/api/v1/admin/delivery/dispatch-runs/"+run+"/reconciliation", nil, w.ops.dmgr)
	h.wantStatus(res, http.StatusOK)
	if len(list(t, res.Data["discrepancies"])) != 0 || obj(t, res.Data["summary"])["delivered"] != float64(2) || res.Data["run_status"] != "completed" {
		t.Fatalf("reconciliation = %v", res.Data)
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/reconcile", nil, w.ops.dispatcher), http.StatusForbidden)
	res = h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/reconcile", map[string]any{"explanation": "All delivered"}, w.ops.dmgr)
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != "reconciled" {
		t.Fatalf("after reconcile = %v", res.Data)
	}
	// The report counts the records.
	res = h.do("GET", "/api/v1/admin/reports/delivery?from=2026-11-16&to=2026-11-22", nil, w.ops.dmgr)
	h.wantStatus(res, http.StatusOK)
	tot := obj(t, res.Data["totals"])
	if tot["deliveries"] != float64(2) || tot["delivered"] != float64(2) || tot["first_attempt_delivered"] != float64(2) || tot["proof_completeness"] != float64(1) {
		t.Fatalf("report totals = %v", tot)
	}
	// Traceability still runs from order package to batch, now with the outcome.
	res = h.do("GET", "/api/v1/admin/fulfillment/"+w.deliveryData(a, w.ops.dispatcher)["obligation_id"].(string)+"/trace", nil, w.ops.fmgr)
	h.wantStatus(res, http.StatusOK)
	if res.Data["level"] != "batch" || obj(t, res.Data["delivery"])["status"] != "delivered" || obj(t, res.Data["delivery"])["proof_method"] != "confirmation_code" {
		t.Errorf("trace = %v", res.Data)
	}
}

func TestFailedDeliveryExceptionReturnAndResolution(t *testing.T) {
	w := newKitchenWeek(t)
	h := w.h
	c := w.configure()
	a, b := w.released()
	w.schedule(a, c)
	w.schedule(b, c)
	w.capacity(10)
	run := w.run(c)
	for _, d := range []string{a, b} {
		h.wantStatus(h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/stops", map[string]any{"delivery_id": d}, w.ops.dispatcher), http.StatusOK)
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/finalize", nil, w.ops.dispatcher), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/release", nil, w.ops.dispatcher), http.StatusOK)
	h.wantStatus(w.handoverAll(run, "h1", w.ops.dispatcher), http.StatusOK)

	// Only configured reasons; the failure raises an exception, never assumes fault.
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/fail", map[string]any{"reason": "customer_fault", "idempotency_key": "f0"}, w.ops.courier), http.StatusUnprocessableEntity)
	res := h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/fail", map[string]any{"reason": "recipient_unavailable", "notes": "No answer at the gate", "idempotency_key": "f1"}, w.ops.courier)
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != "delivery_failed" || res.Data["open_exceptions"] != float64(1) {
		t.Fatalf("after failure = %v", res.Data)
	}
	if c := h.do("GET", "/api/v1/me/orders/"+w.orderA+"/delivery", nil, w.tokA).Data; c["stage"] != "issue" || c["issue"] == nil {
		t.Errorf("customer after failure = %v", c)
	}
	// Proof after a failure is refused; so is completion.
	h.wantError(h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/deliver", map[string]any{"idempotency_key": "x"}, w.ops.courier), http.StatusConflict, "invalid_transition")

	// Ben's delivery goes through; the run cannot reconcile while Ana's failure is open.
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/deliveries/"+b+"/events", map[string]any{"status": "in_transit", "idempotency_key": "t"}, w.ops.courier), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/deliveries/"+b+"/proof", map[string]any{"method": "recipient_acknowledgment", "recipient_name": "Ben", "idempotency_key": "bp"}, w.ops.courier), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/deliveries/"+b+"/deliver", map[string]any{"idempotency_key": "bd"}, w.ops.courier), http.StatusOK)
	h.wantError(h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/reconcile", nil, w.ops.dmgr), http.StatusUnprocessableEntity, "discrepancies")

	// The packages come back: quarantined for review, never into stock.
	res = h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/return", map[string]any{"reason": "Brought back by courier", "condition": "intact"}, w.ops.coord)
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != "returned" || obj(t, list(t, res.Data["returns"])[0])["disposition"] != "quarantined" {
		t.Fatalf("after return = %v", res.Data)
	}
	if n := h.count(`SELECT count(*) FROM inventory_movements`); n != 0 {
		t.Errorf("inventory movements after return = %d", n)
	}
	ret := obj(t, list(t, h.do("GET", "/api/v1/admin/delivery/returns", nil, w.ops.coord).Data["items"])[0])
	if ret["delivery_id"] != a || len(list(t, ret["packages"])) != 1 {
		t.Fatalf("returns = %v", ret)
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/returns/"+ret["id"].(string)+"/review", map[string]any{"disposition": "other_approved", "note": "x"}, w.ops.coord), http.StatusUnprocessableEntity)
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/returns/"+ret["id"].(string)+"/review", map[string]any{"disposition": "discarded", "note": "Discarded per food safety; out of temperature window."}, w.ops.coord), http.StatusOK)
	// Still the open failure exception: resolved with the decided next action.
	exc := list(t, h.do("GET", "/api/v1/admin/delivery/exceptions?delivery_id="+a, nil, w.ops.coord).Data["items"])
	if len(exc) != 1 || obj(t, exc[0])["type"] != "delivery_failure" {
		t.Fatalf("exceptions = %v", exc)
	}
	h.wantStatus(h.do("PATCH", "/api/v1/admin/delivery/exceptions/"+obj(t, exc[0])["id"].(string), map[string]any{"next_action": "refund_review",
		"status": "resolved", "resolution": "Kits discarded; refund review passed to finance."}, w.ops.coord), http.StatusOK)
	res = h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/reconcile", nil, w.ops.dmgr)
	h.wantStatus(res, http.StatusOK)
	sum := obj(t, res.Data["summary"])
	if sum["delivered"] != float64(1) || sum["returned"] != float64(1) || sum["returned_packages"] != float64(1) {
		t.Fatalf("reconciled summary = %v", sum)
	}
	// No refund was made automatically.
	if n := h.count(`SELECT count(*) FROM refunds`); n != 0 {
		t.Errorf("refunds = %d", n)
	}
}

func TestReschedulingCancellationAndCorrections(t *testing.T) {
	w := newKitchenWeek(t)
	h := w.h
	c := w.configure()
	a, b := w.released()
	w.schedule(a, c)
	w.capacity(1)
	run := w.run(c)
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/stops", map[string]any{"delivery_id": a}, w.ops.dispatcher), http.StatusOK)

	// With no reschedule policy, the customer's request is recorded, nothing promised.
	res := h.do("POST", "/api/v1/me/orders/"+w.orderA+"/delivery/reschedule-requests", map[string]any{"preferred_date": "2026-11-26", "reason": "Away that day"}, w.tokA)
	h.wantStatus(res, http.StatusCreated)
	if res.Data["open_request"] == nil || res.Data["service_date"] != serviceDay || !strings.Contains(res.Data["reschedule_policy"].(string), "isn't confirmed") {
		t.Fatalf("after request = %v", res.Data)
	}
	h.wantError(h.do("POST", "/api/v1/me/orders/"+w.orderA+"/delivery/reschedule-requests", map[string]any{"reason": "Again"}, w.tokA), http.StatusConflict, "already_requested")
	req := obj(t, list(t, h.do("GET", "/api/v1/admin/delivery/reschedule-requests", nil, w.ops.dmgr).Data["items"])[0])["id"].(string)
	// A date that is not a service day is refused; capacity is respected.
	h.wantError(h.do("POST", "/api/v1/admin/delivery/reschedule-requests/"+req+"/decide", map[string]any{"decision": "approve", "new_date": "2026-11-26"}, w.ops.dmgr),
		http.StatusUnprocessableEntity, "not_a_service_day")
	h.wantStatus(h.do("PUT", "/api/v1/admin/delivery/calendar", map[string]any{"date": "2026-11-26", "status": "open"}, w.ops.dmgr), http.StatusOK)
	h.wantStatus(h.do("PUT", "/api/v1/admin/delivery/capacity", map[string]any{"date": "2026-11-26", "capacity": 0}, w.ops.dmgr), http.StatusOK)
	h.wantError(h.do("POST", "/api/v1/admin/delivery/reschedule-requests/"+req+"/decide", map[string]any{"decision": "approve", "new_date": "2026-11-26"}, w.ops.dmgr),
		http.StatusUnprocessableEntity, "capacity")
	h.wantStatus(h.do("PUT", "/api/v1/admin/delivery/capacity", map[string]any{"date": "2026-11-26", "capacity": 5}, w.ops.dmgr), http.StatusOK)
	res = h.do("POST", "/api/v1/admin/delivery/reschedule-requests/"+req+"/decide", map[string]any{"decision": "approve", "new_date": "2026-11-26", "note": "Moved as asked"}, w.ops.dmgr)
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != "pending_assignment" || res.Data["service_date"] != "2026-11-26" || res.Data["run_id"] != nil {
		t.Fatalf("after approval = %v", res.Data)
	}

	// Cancelling while a dispatcher assigns: one of them wins, never both.
	w.schedule(b, c)
	var wg sync.WaitGroup
	var assign, cancel int
	wg.Add(2)
	go func() {
		defer wg.Done()
		assign = h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/stops", map[string]any{"delivery_id": b}, w.ops.dispatcher).Status
	}()
	go func() {
		defer wg.Done()
		cancel = h.do("POST", "/api/v1/admin/delivery/deliveries/"+b+"/cancel", map[string]any{"reason": "Customer asked to skip the week"}, w.ops.dmgr).Status
	}()
	wg.Wait()
	if cancel != 200 {
		t.Fatalf("cancel = %d (assign %d)", cancel, assign)
	}
	if n := h.count(`SELECT count(*) FROM dispatch_stops WHERE delivery_id = $1 AND status = 'active'`, b); n != 0 {
		t.Fatalf("a cancelled delivery kept an active stop")
	}
	// Confirming a cancelled delivery is refused.
	h.wantError(h.do("POST", "/api/v1/admin/delivery/deliveries/"+b+"/deliver", map[string]any{"idempotency_key": "late"}, w.ops.coord), http.StatusConflict, "invalid_transition")
	var fstatus string
	if err := h.pool.QueryRow(context.Background(), `SELECT status FROM fulfillment_obligations WHERE order_id = $1`, w.orderB).Scan(&fstatus); err != nil {
		t.Fatal(err)
	}
	if fstatus != "cancelled" {
		t.Errorf("fulfillment after cancel = %s", fstatus)
	}

	// Rescheduling during dispatch is refused: once picked up, only an outcome can be recorded.
	w.h.wantStatus(h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/schedule", map[string]any{"service_date": serviceDay}, w.ops.dispatcher), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/stops", map[string]any{"delivery_id": a}, w.ops.dispatcher), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/finalize", nil, w.ops.dispatcher), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/release", nil, w.ops.dispatcher), http.StatusOK)
	h.wantStatus(w.handoverAll(run, "hx", w.ops.dispatcher), http.StatusOK)
	h.wantError(h.do("POST", "/api/v1/me/orders/"+w.orderA+"/delivery/reschedule-requests", map[string]any{"reason": "Changed my mind"}, w.tokA), http.StatusConflict, "too_late")
	// The address cannot be changed after dispatch, by the API or in the database.
	h.wantError(h.do("PUT", "/api/v1/admin/delivery/deliveries/"+a+"/address", map[string]any{"reason": "Typo in street",
		"address": map[string]any{"recipient_name": "Ana", "line1": "1 New St", "city": "Woodbrook", "region": "port-of-spain"}}, w.ops.dispatcher), http.StatusConflict, "dispatched")
	if _, err := h.pool.Exec(context.Background(), `UPDATE deliveries SET address = '{"line1": "x"}' WHERE id = $1`, a); err == nil {
		t.Error("the database allowed an address change after dispatch")
	}

	// A wrong outcome is corrected explicitly, with a reason, by an authorised person.
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/fail", map[string]any{"reason": "address_not_found", "idempotency_key": "f"}, w.ops.courier), http.StatusOK)
	admin := bearer(h.adminLogin("boss@mise.test", "admin"))
	h.wantError(h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/correct", map[string]any{"to": "delivered", "reason": "Courier says it was delivered"}, admin),
		http.StatusConflict, "invalid_correction")
	res = h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/correct", map[string]any{"to": "in_transit", "reason": "Courier tapped fail by mistake; still on the road"}, admin)
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != "in_transit" {
		t.Fatalf("after correction = %v", res.Data["status"])
	}
	found := false
	for _, e := range list(t, res.Data["events"]) {
		if obj(t, e)["type"] == "correction" {
			found = true
		}
	}
	if !found {
		t.Error("the correction is not in the history")
	}
}

func TestDeliveryEmailsAreRecordedHonestly(t *testing.T) {
	w := newKitchenWeek(t)
	h := w.h
	c := w.configure()
	a, b := w.released()
	// Wording not approved: recorded as skipped, nothing sent.
	before := h.mailer.countTo("ana@example.com")
	w.schedule(a, c)
	h.drainJobs()
	if h.mailer.countTo("ana@example.com") != before {
		t.Fatal("an email was sent before the wording was approved")
	}
	n := list(t, w.deliveryData(a, w.ops.dispatcher)["notifications"])
	if len(n) != 1 || obj(t, n[0])["status"] != "skipped" {
		t.Fatalf("notifications = %v", n)
	}
	admin := bearer(h.adminLogin("boss@mise.test", "admin"))
	h.wantStatus(h.do("PUT", "/api/v1/admin/delivery/settings", map[string]any{"customer_emails_approved": true}, admin), http.StatusOK)
	// Ben turned delivery updates off; Ana did not (so hers are on).
	h.wantStatus(h.do("PUT", "/api/v1/me/notification-preferences", map[string]any{"topics": map[string]bool{"delivery_updates": false}}, w.tokB), http.StatusOK)
	w.schedule(b, c)
	w.capacity(5)
	run := w.run(c)
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/stops", map[string]any{"delivery_id": a}, w.ops.dispatcher), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/stops", map[string]any{"delivery_id": b}, w.ops.dispatcher), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/finalize", nil, w.ops.dispatcher), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/release", nil, w.ops.dispatcher), http.StatusOK)
	h.wantStatus(w.handoverAll(run, "h", w.ops.dispatcher), http.StatusOK)
	h.drainJobs()
	if m := h.mailer.lastTo(t, "ana@example.com"); !strings.Contains(m.Subject, "on its way") {
		t.Errorf("Ana's email = %q", m.Subject)
	}
	statuses := map[string]string{}
	for _, d := range []string{a, b} {
		for _, x := range list(t, w.deliveryData(d, w.ops.dispatcher)["notifications"]) {
			if obj(t, x)["kind"] == "dispatched" {
				statuses[d] = obj(t, x)["status"].(string)
			}
		}
	}
	if statuses[a] != "sent" || statuses[b] != "skipped" {
		t.Fatalf("dispatched notifications = %v", statuses)
	}
}

// ---------------------------------------------------------------- review fixes

func TestDeliveryDatesFollowTheOrderWeekRule(t *testing.T) {
	w := newKitchenWeek(t)
	h := w.h
	c := w.configure()
	a, _ := w.released()
	admin := bearer(h.adminLogin("boss@mise.test", "admin"))
	h.wantStatus(h.do("PUT", "/api/v1/admin/delivery/calendar", map[string]any{"date": "2026-12-03", "status": "open"}, w.ops.dmgr), http.StatusOK)

	// No approved rule: nothing can be scheduled, and the blocker says why.
	h.wantStatus(h.do("PUT", "/api/v1/admin/delivery/settings", map[string]any{"clear_delivery_week_rule": true}, admin), http.StatusOK)
	h.wantError(h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/schedule", map[string]any{"service_date": serviceDay}, w.ops.dispatcher),
		http.StatusUnprocessableEntity, "delivery_week_rule_missing")
	if res := h.do("GET", "/api/v1/admin/delivery/settings", nil, w.ops.dispatcher); !strings.Contains(string(res.Raw), "No delivery-week rule is approved") {
		t.Errorf("blockers = %s", res.Raw)
	}
	// A rule needs both ends and its approved reference.
	h.wantStatus(h.do("PUT", "/api/v1/admin/delivery/settings", map[string]any{"delivery_offset_min_days": 3}, admin), http.StatusUnprocessableEntity)
	h.wantStatus(h.do("PUT", "/api/v1/admin/delivery/settings", map[string]any{"delivery_offset_min_days": 3, "delivery_offset_max_days": 5}, admin), http.StatusUnprocessableEntity)
	// Week of 16 Nov: Thursday to Saturday only.
	h.wantStatus(h.do("PUT", "/api/v1/admin/delivery/settings", map[string]any{"delivery_offset_min_days": 3, "delivery_offset_max_days": 5,
		"delivery_week_rule_ref": "OPS-DAYS-2 (test)"}, admin), http.StatusOK)
	// An open service day outside the order's week is still refused.
	h.wantError(h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/schedule", map[string]any{"service_date": "2026-12-03"}, w.ops.dispatcher),
		http.StatusUnprocessableEntity, "outside_delivery_week")
	w.schedule(a, c) // 19 Nov: Thursday of the order's week
	// A reschedule is held to the same rule.
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/reschedule-requests", map[string]any{"reason": "Customer away"}, w.ops.coord), http.StatusCreated)
	req := obj(t, list(t, h.do("GET", "/api/v1/admin/delivery/reschedule-requests", nil, w.ops.dmgr).Data["items"])[0])["id"].(string)
	h.wantError(h.do("POST", "/api/v1/admin/delivery/reschedule-requests/"+req+"/decide", map[string]any{"decision": "approve", "new_date": "2026-12-03"}, w.ops.dmgr),
		http.StatusUnprocessableEntity, "outside_delivery_week")
	// Narrowing the rule after scheduling stops the assignment too.
	w.capacity(5)
	run := w.run(c)
	h.wantStatus(h.do("PUT", "/api/v1/admin/delivery/settings", map[string]any{"delivery_offset_min_days": 4, "delivery_offset_max_days": 5,
		"delivery_week_rule_ref": "OPS-DAYS-3 (test)"}, admin), http.StatusOK)
	h.wantError(h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/stops", map[string]any{"delivery_id": a}, w.ops.dispatcher),
		http.StatusUnprocessableEntity, "outside_delivery_week")
}

func TestCorrectedOutcomeIsADisputeInCommerceAndReports(t *testing.T) {
	w := newKitchenWeek(t)
	h := w.h
	ctx := context.Background()
	c := w.configure()
	a, _ := w.released()
	w.schedule(a, c)
	w.capacity(10)
	deliver := func(run, key string) {
		h.wantStatus(h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/stops", map[string]any{"delivery_id": a}, w.ops.dispatcher), http.StatusOK)
		h.wantStatus(h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/finalize", nil, w.ops.dispatcher), http.StatusOK)
		h.wantStatus(h.do("POST", "/api/v1/admin/delivery/dispatch-runs/"+run+"/release", nil, w.ops.dispatcher), http.StatusOK)
		h.wantStatus(w.handoverAll(run, "h-"+key, w.ops.dispatcher), http.StatusOK)
		h.wantStatus(h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/events", map[string]any{"status": "in_transit", "idempotency_key": "t-" + key}, w.ops.courier), http.StatusOK)
		h.wantStatus(h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/proof", map[string]any{"method": "recipient_acknowledgment", "recipient_name": "Ana",
			"idempotency_key": "p-" + key}, w.ops.courier), http.StatusOK)
		h.wantStatus(h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/deliver", map[string]any{"idempotency_key": "d-" + key}, w.ops.courier), http.StatusOK)
	}
	deliver(w.run(c), "1")
	order := func() map[string]any {
		res := h.do("GET", "/api/v1/me/orders/"+w.orderA, nil, w.tokA)
		h.wantStatus(res, http.StatusOK)
		return res.Data
	}
	if o := order(); o["status"] != "delivered" || o["delivery_outcome"] != nil {
		t.Fatalf("before correction = %v / %v", o["status"], o["delivery_outcome"])
	}
	payments := h.count(`SELECT count(*) FROM payment_status_history`)
	history := h.count(`SELECT count(*) FROM order_status_history WHERE order_id = $1`, w.orderA)

	// The delivered outcome is corrected: commerce records a dispute, rewrites nothing.
	admin := bearer(h.adminLogin("boss@mise.test", "admin"))
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/correct", map[string]any{"to": "delivery_failed",
		"reason": "Partner confirmed it went to the wrong house"}, admin), http.StatusOK)
	o := order()
	if o["status"] != "delivered" || obj(t, o["delivery_outcome"])["state"] != "disputed" {
		t.Fatalf("after correction = %v / %v", o["status"], o["delivery_outcome"])
	}
	if h.count(`SELECT count(*) FROM payment_status_history`) != payments || h.count(`SELECT count(*) FROM order_status_history WHERE order_id = $1`, w.orderA) != history {
		t.Error("the correction rewrote payment or order history")
	}
	if _, err := h.pool.Exec(ctx, `UPDATE order_delivery_outcomes SET state = 'confirmed'`); err == nil {
		t.Error("outcome entries can be edited")
	}
	// Staff lists, the customer's delivery card and the reports all agree.
	staffOrders := h.do("GET", "/api/v1/admin/orders?status=delivered", nil, admin)
	h.wantStatus(staffOrders, http.StatusOK)
	found := false
	for _, x := range list(t, staffOrders.Data["items"]) {
		if obj(t, x)["id"] == w.orderA {
			found = obj(t, x)["delivery_outcome"] == "disputed"
		}
	}
	if !found {
		t.Errorf("staff order list does not show the dispute: %s", staffOrders.Raw)
	}
	if cd := h.do("GET", "/api/v1/me/orders/"+w.orderA+"/delivery", nil, w.tokA).Data; cd["stage"] != "issue" {
		t.Errorf("customer delivery stage = %v", cd["stage"])
	}
	rep := obj(t, h.do("GET", "/api/v1/admin/reports/delivery?from=2026-11-16&to=2026-11-29", nil, w.ops.dmgr).Data["totals"])
	if rep["corrected_outcomes"] != float64(1) || rep["disputed_outcomes"] != float64(1) || rep["delivered"] != float64(0) {
		t.Fatalf("report after correction = %v", rep)
	}
	if ov := h.do("GET", "/api/v1/admin/delivery/overview", nil, w.ops.dmgr).Data; ov["disputed_outcomes"] != float64(1) {
		t.Errorf("overview disputed = %v", ov["disputed_outcomes"])
	}

	// A new attempt is delivered: commerce records the outcome as confirmed again.
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/reattempt", map[string]any{"reason": "Redeliver to the right house"}, w.ops.dmgr), http.StatusOK)
	h.wantStatus(h.do("PUT", "/api/v1/admin/delivery/calendar", map[string]any{"date": "2026-11-26", "status": "open"}, w.ops.dmgr), http.StatusOK)
	h.wantStatus(h.do("PUT", "/api/v1/admin/delivery/capacity", map[string]any{"date": "2026-11-26", "capacity": 5}, w.ops.dmgr), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/delivery/deliveries/"+a+"/schedule", map[string]any{"service_date": "2026-11-26"}, w.ops.dispatcher), http.StatusOK)
	run2 := h.do("POST", "/api/v1/admin/delivery/dispatch-runs", map[string]any{"service_date": "2026-11-26", "partner_id": c.partner, "courier_id": c.courier}, w.ops.dispatcher)
	h.wantStatus(run2, http.StatusCreated)
	deliver(run2.Data["id"].(string), "2")
	if o := order(); obj(t, o["delivery_outcome"])["state"] != "confirmed" {
		t.Fatalf("after redelivery = %v", o["delivery_outcome"])
	}
	rep = obj(t, h.do("GET", "/api/v1/admin/reports/delivery?from=2026-11-16&to=2026-11-29", nil, w.ops.dmgr).Data["totals"])
	if rep["disputed_outcomes"] != float64(0) || rep["delivered"] != float64(1) {
		t.Errorf("report after redelivery = %v", rep)
	}
}
