package server_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"mise.tt/api/internal/launch"
)

func gateOf(t *testing.T, data map[string]any, key string) map[string]any {
	t.Helper()
	for _, g := range list(t, data["gates"]) {
		if obj(t, g)["key"] == key {
			return obj(t, g)
		}
	}
	t.Fatalf("gate %s missing", key)
	return nil
}

// The launch control centre shows evidence and sign-offs separately, never
// lets a sign-off override contrary evidence, and is never "ready" while a
// critical gate is open.
func TestLaunchGatesAreEvidenceBackedAndSignedOff(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	owner := bearer(h.adminLogin("owner@mise.test", "super_admin"))
	admin := bearer(h.adminLogin("admin@mise.test", "admin"))
	cook := bearer(h.adminLogin("cook@mise.test", "kitchen_staff"))

	h.wantStatus(h.do("GET", "/api/v1/admin/launch", nil, cook), http.StatusForbidden)
	res := h.do("GET", "/api/v1/admin/launch", nil, admin)
	h.wantStatus(res, http.StatusOK)
	if res.Data["ready"] != false || res.Data["critical_blockers"].(float64) < 1 {
		t.Fatalf("a fresh system must not be ready: %v", res.Data["summary"])
	}
	pay := gateOf(t, res.Data, "payment_provider")
	if pay["status"] != launch.StatusBlocked || pay["evidence_ok"] != false {
		t.Fatalf("payment gate with only the simulator = %v", pay)
	}

	// Only launch.signoff may sign; approving against contrary evidence is refused.
	body := map[string]any{"decision": "approved", "reference": "Board minutes 2026-11-20"}
	h.wantStatus(h.do("POST", "/api/v1/admin/launch/gates/legal_policies/signoffs", body, admin), http.StatusForbidden)
	h.wantError(h.do("POST", "/api/v1/admin/launch/gates/payment_provider/signoffs", body, owner), http.StatusConflict, "evidence_missing")
	h.wantStatus(h.do("POST", "/api/v1/admin/launch/gates/no_such_gate/signoffs", body, owner), http.StatusNotFound)
	h.wantStatus(h.do("POST", "/api/v1/admin/launch/gates/legal_policies/signoffs", map[string]any{"decision": "approved", "reference": "x"}, owner), http.StatusUnprocessableEntity)

	// A gate the system cannot observe is verified only by a recorded sign-off.
	res = h.do("POST", "/api/v1/admin/launch/gates/legal_policies/signoffs", map[string]any{"decision": "approved",
		"reference": "Counsel review LR-17", "note": "Privacy, terms, subscription and refund terms reviewed."}, owner)
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != launch.StatusVerified || obj(t, res.Data["signoff"])["reference"] != "Counsel review LR-17" {
		t.Fatalf("after sign-off = %v", res.Data)
	}
	// Not applicable needs a reason.
	h.wantStatus(h.do("POST", "/api/v1/admin/launch/gates/inventory_readiness/signoffs", map[string]any{"decision": "not_applicable", "reference": "Ops call"}, owner), http.StatusUnprocessableEntity)
	res = h.do("POST", "/api/v1/admin/launch/gates/inventory_readiness/signoffs", map[string]any{"decision": "not_applicable",
		"reference": "Ops call 21 Nov", "note": "Launch week uses supplier deliveries only; no opening stock."}, owner)
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != launch.StatusNotApplicable {
		t.Fatalf("not applicable = %v", res.Data["status"])
	}
	// Revoking withdraws it; history keeps every decision.
	res = h.do("POST", "/api/v1/admin/launch/gates/legal_policies/signoffs", map[string]any{"decision": "revoked", "reference": "Terms changed after review"}, owner)
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != launch.StatusBlocked || res.Data["signoff"] != nil {
		t.Fatalf("after revoke = %v", res.Data)
	}
	if n := h.count(`SELECT count(*) FROM launch_signoffs WHERE gate = 'legal_policies'`); n != 2 {
		t.Errorf("legal sign-off history = %d rows, want 2", n)
	}
	if _, err := h.pool.Exec(ctx, `UPDATE launch_signoffs SET decision = 'approved'`); err == nil {
		t.Error("sign-offs must be append-only")
	}
	if n := h.count(`SELECT count(*) FROM audit_log WHERE action = 'launch.signoff'`); n != 3 {
		t.Errorf("audited sign-offs = %d, want 3", n)
	}
	if res := h.do("GET", "/api/v1/admin/launch", nil, admin); res.Data["ready"] != false {
		t.Error("ready while critical gates are open")
	}
}

func TestSystemStatusShowsWorkerQueuesAndConsumers(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	admin := bearer(h.adminLogin("admin@mise.test", "admin"))

	res := h.do("GET", "/api/v1/version", nil)
	h.wantStatus(res, http.StatusOK)
	if res.Data["version"] != "dev" {
		t.Errorf("an unstamped test build reports %v", res.Data["version"])
	}

	res = h.do("GET", "/api/v1/admin/system/status", nil, admin)
	h.wantStatus(res, http.StatusOK)
	if res.Data["worker_healthy"] != false {
		t.Error("no worker has beaten, yet the worker is reported healthy")
	}
	if err := launch.Beat(ctx, h.pool, "worker@test", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(ctx, `INSERT INTO jobs (kind, payload, status, attempts, last_error, finished_at) VALUES ('commerce.email', '{}', 'failed', 5, 'smtp refused', now())`); err != nil {
		t.Fatal(err)
	}
	res = h.do("GET", "/api/v1/admin/system/status", nil, admin)
	h.wantStatus(res, http.StatusOK)
	if res.Data["worker_healthy"] != true || res.Data["email_failures_24h"] != float64(1) || res.Data["failed_jobs_24h"] != float64(1) {
		t.Fatalf("status = %v", res.Data)
	}
	if len(list(t, res.Data["consumers"])) < 4 {
		t.Errorf("consumers = %v", res.Data["consumers"])
	}
	// A worker that stops beating is reported stopped.
	if _, err := h.pool.Exec(ctx, `UPDATE worker_heartbeats SET beat_at = now() - interval '10 minutes'`); err != nil {
		t.Fatal(err)
	}
	if res := h.do("GET", "/api/v1/admin/system/status", nil, admin); res.Data["worker_healthy"] != false {
		t.Error("a silent worker is still reported healthy")
	}
}
