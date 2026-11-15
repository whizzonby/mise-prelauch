package server_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"mise.tt/api/internal/automation"
	"mise.tt/api/internal/payments/testpay"
	"mise.tt/api/internal/server"
)

func (h *harness) registerAutomation(withMail bool) (*automation.Service, *automation.Reports) {
	var m automation.Mailer
	if withMail {
		m = h.mailer
	}
	au, rep := server.NewAutomation(h.cfg, h.pool, h.com, h.bo, h.ids, newAnalytics(h), m)
	h.runner.Register(automation.JobExecute, au.HandleExecute)
	h.runner.Register(automation.JobEvent, au.HandleEvent)
	h.runner.Register(automation.JobReportEmail, rep.HandleEmail)
	return au, rep
}

func ruleID(t *testing.T, h *harness, kind string) string {
	t.Helper()
	var id string
	if err := h.pool.QueryRow(context.Background(), `SELECT id::text FROM automation_rules WHERE kind = $1`, kind).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestAutomationRulesAreApprovedDedupedAndVisible(t *testing.T) {
	s := newShop(t)
	h := s.h
	ctx := context.Background()
	au, _ := h.registerAutomation(false)
	admin := bearer(h.adminLogin("ops@mise.test", "admin"))
	analyst := bearer(h.adminLogin("numbers@mise.test", "analyst"))
	rule := ruleID(t, h, "payment_failed")

	// Shipped disabled; enabling needs a cooldown and an approval reference.
	res := h.do("GET", "/api/v1/admin/automation/rules", nil, admin)
	h.wantStatus(res, http.StatusOK)
	for _, r := range list(t, res.Data["items"]) {
		if obj(t, r)["enabled"] != false || len(list(t, obj(t, r)["missing"])) == 0 {
			t.Fatalf("rule shipped enabled or configured: %v", r)
		}
	}
	h.wantStatus(h.do("PATCH", "/api/v1/admin/automation/rules/"+rule, map[string]any{"enabled": true}, admin), 422)
	h.wantStatus(h.do("PATCH", "/api/v1/admin/automation/rules/"+rule, map[string]any{"action": "refund"}, admin), 422)
	h.wantStatus(h.do("PATCH", "/api/v1/admin/automation/rules/"+ruleID(t, h, "overdue_purchase_order"), map[string]any{"params": map[string]any{"grace_hours": 99999}}, admin), 422)
	h.wantStatus(h.do("PATCH", "/api/v1/admin/automation/rules/"+rule, map[string]any{"enabled": true, "cooldown_minutes": 60, "approval_ref": "Ops review 12 Nov"}, analyst), http.StatusForbidden)
	h.wantStatus(h.do("PATCH", "/api/v1/admin/automation/rules/"+rule, map[string]any{"enabled": true, "action": "alert", "cooldown_minutes": 60, "approval_ref": "Ops review 12 Nov"}, admin), http.StatusOK)
	if n := h.count(`SELECT count(*) FROM automation_rule_versions WHERE rule_id = $1`, rule); n != 1 {
		t.Errorf("versions = %d", n)
	}

	// A failed payment after enabling creates one execution and one alert.
	tok, addr := s.customer("declined@example.com")
	_, ref := s.subscribe(tok, addr, s.basic, nil)
	s.pay(ref, "decline", testpay.CardApprove)
	h.drainJobs()
	for i := 0; i < 2; i++ { // a second worker tick, or a repeat, is a no-op
		if _, err := au.EvaluateAll(ctx); err != nil {
			t.Fatal(err)
		}
	}
	h.drainJobs()
	if n := h.count(`SELECT count(*) FROM automation_executions WHERE rule_id = $1 AND status = 'succeeded'`, rule); n != 1 {
		t.Fatalf("executions = %d", n)
	}
	if n := h.count(`SELECT count(*) FROM admin_alerts WHERE kind = 'automation'`); n != 1 {
		t.Errorf("alerts = %d", n)
	}
	// Still within the cooldown an hour-boundary later? No: after the cooldown it may fire again, once.
	h.clock.Set(h.clock.Now().Add(61 * time.Minute))
	if n, _ := au.EvaluateAll(ctx); n != 1 {
		t.Errorf("after cooldown created %d", n)
	}
	h.drainJobs()

	// A failing action stays visible and a retry is safe.
	h.wantStatus(h.do("PATCH", "/api/v1/admin/automation/rules/"+rule, map[string]any{"action": "task"}, admin), http.StatusOK)
	automation.FailNext = func(string) error { return errors.New("task store unavailable") }
	h.clock.Set(h.clock.Now().Add(61 * time.Minute))
	if _, err := au.EvaluateAll(ctx); err != nil {
		t.Fatal(err)
	}
	var exec string
	if err := h.pool.QueryRow(ctx, `SELECT id::text FROM automation_executions WHERE action = 'task'`).Scan(&exec); err != nil {
		t.Fatal(err)
	}
	if err := au.HandleExecute(ctx, []byte(`{"execution_id":"`+exec+`"}`)); err == nil {
		t.Fatal("failing action reported success")
	}
	automation.FailNext = nil
	res = h.do("GET", "/api/v1/admin/automation/overview", nil, analyst)
	if res.Data["failed_executions"] != float64(1) {
		t.Errorf("overview = %v", res.Data)
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/automation/executions/"+exec+"/retry", nil, analyst), http.StatusForbidden)
	h.wantStatus(h.do("POST", "/api/v1/admin/automation/executions/"+exec+"/retry", nil, admin), http.StatusOK)
	h.drainJobs()
	h.wantStatus(h.do("POST", "/api/v1/admin/automation/executions/"+exec+"/retry", nil, admin), http.StatusConflict)
	_ = au.HandleExecute(ctx, []byte(`{"execution_id":"`+exec+`"}`)) // a duplicate job delivery
	if n := h.count(`SELECT count(*) FROM automation_tasks WHERE execution_id = $1`, exec); n != 1 {
		t.Errorf("tasks = %d", n)
	}
	res = h.do("GET", "/api/v1/admin/automation/executions/"+exec, nil, admin)
	types := []string{}
	for _, e := range list(t, res.Data["events"]) {
		types = append(types, obj(t, e)["type"].(string))
	}
	if strings.Join(types, ",") != "created,failed,retry_requested,succeeded" {
		t.Errorf("history = %v", types)
	}
	// The task is for finance (who own payments); the admin holds it too.
	res = h.do("GET", "/api/v1/admin/automation/tasks", nil, admin)
	tasks := list(t, res.Data["items"])
	if len(tasks) != 1 {
		t.Fatalf("tasks = %v", tasks)
	}
	kitchen := bearer(h.adminLogin("cook@mise.test", "kitchen_staff"))
	if res := h.do("GET", "/api/v1/admin/automation/tasks", nil, kitchen); len(list(t, res.Data["items"])) != 0 {
		t.Error("kitchen sees a finance task")
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/automation/tasks/"+obj(t, tasks[0])["id"].(string)+"/close", map[string]any{"status": "done", "note": "Called the customer"}, admin), http.StatusOK)

	// Approval requests are decided by a person and never act themselves.
	h.wantStatus(h.do("PATCH", "/api/v1/admin/automation/rules/"+rule, map[string]any{"action": "request_approval"}, admin), http.StatusOK)
	h.clock.Set(h.clock.Now().Add(61 * time.Minute))
	if _, err := au.EvaluateAll(ctx); err != nil {
		t.Fatal(err)
	}
	h.drainJobs()
	if err := h.pool.QueryRow(ctx, `SELECT id::text FROM automation_executions WHERE status = 'awaiting_approval'`).Scan(&exec); err != nil {
		t.Fatal(err)
	}
	payments := h.count(`SELECT count(*) FROM payments`)
	h.wantStatus(h.do("POST", "/api/v1/admin/automation/executions/"+exec+"/decision", map[string]any{"decision": "approve", "note": "ok"}, analyst), http.StatusForbidden)
	h.wantStatus(h.do("POST", "/api/v1/admin/automation/executions/"+exec+"/decision", map[string]any{"decision": "approve"}, admin), 422)
	h.wantStatus(h.do("POST", "/api/v1/admin/automation/executions/"+exec+"/decision", map[string]any{"decision": "approve", "note": "Retry the card with the customer"}, admin), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/automation/executions/"+exec+"/decision", map[string]any{"decision": "reject", "note": "Changed mind"}, admin), http.StatusConflict)
	if h.count(`SELECT count(*) FROM payments`) != payments || h.count(`SELECT count(*) FROM refunds`) != 0 {
		t.Error("an approval changed payments")
	}
	// Disabled rules never run.
	h.wantStatus(h.do("PATCH", "/api/v1/admin/automation/rules/"+rule, map[string]any{"enabled": false}, admin), http.StatusOK)
	h.clock.Set(h.clock.Now().Add(61 * time.Minute))
	if n, _ := au.EvaluateAll(ctx); n != 0 {
		t.Errorf("disabled rule created %d", n)
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/automation/rules/"+rule+"/evaluate", nil, admin), http.StatusConflict)
}

func TestScheduledReportsRunOncePerOccurrence(t *testing.T) {
	s := newShop(t)
	h := s.h
	ctx := context.Background()
	_, rep := h.registerAutomation(true)
	admin := bearer(h.adminLogin("ops@mise.test", "admin"))
	h.adminLogin("numbers@mise.test", "analyst")
	h.adminLogin("cook@mise.test", "kitchen_staff")
	var id string
	if err := h.pool.QueryRow(ctx, `SELECT id::text FROM scheduled_reports WHERE kind = 'weekly_business'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	// Recipients must be staff allowed to see the report.
	h.wantStatus(h.do("PATCH", "/api/v1/admin/reports/scheduled/"+id, map[string]any{"recipients": []string{"cook@mise.test"}}, admin), 422)
	h.wantStatus(h.do("PATCH", "/api/v1/admin/reports/scheduled/"+id, map[string]any{"recipients": []string{"customer@example.com"}}, admin), 422)
	h.wantStatus(h.do("PATCH", "/api/v1/admin/reports/scheduled/"+id, map[string]any{"enabled": true}, admin), 422)
	h.wantStatus(h.do("PATCH", "/api/v1/admin/reports/scheduled/"+id, map[string]any{"enabled": true, "approval_ref": "Board pack 12 Nov",
		"recipients": []string{"numbers@mise.test"}}, admin), http.StatusOK)
	// Two ticks (or two workers) generate it once.
	for i := 0; i < 2; i++ {
		if _, err := rep.Due(ctx); err != nil {
			t.Fatal(err)
		}
	}
	h.drainJobs()
	if n := h.count(`SELECT count(*) FROM report_runs WHERE schedule_id = $1`, id); n != 1 {
		t.Fatalf("runs = %d", n)
	}
	if n := h.mailer.countTo("numbers@mise.test"); n != 1 {
		t.Errorf("emails = %d", n)
	}
	res := h.do("GET", "/api/v1/admin/reports/runs", nil, admin)
	run := obj(t, list(t, res.Data["items"])[0])
	if run["period_start"] != "2026-11-02" || run["period_end"] != "2026-11-08" || obj(t, list(t, run["deliveries"])[0])["status"] != "sent" {
		t.Errorf("run = %v", run)
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/reports/scheduled/"+id+"/run", nil, admin), http.StatusConflict)
	res = h.do("GET", "/api/v1/admin/reports/runs/"+run["id"].(string)+"/csv", nil, admin)
	h.wantStatus(res, http.StatusOK)
	if !strings.Contains(string(res.Raw), "revenue.collected") {
		t.Errorf("csv = %s", res.Raw)
	}
	// Without an email service the outcome is recorded as skipped, never sent.
	_, rep = h.registerAutomation(false)
	var daily string
	_ = h.pool.QueryRow(ctx, `SELECT id::text FROM scheduled_reports WHERE kind = 'daily_operations'`).Scan(&daily)
	h.wantStatus(h.do("PATCH", "/api/v1/admin/reports/scheduled/"+daily, map[string]any{"enabled": true, "approval_ref": "Ops 12 Nov",
		"recipients": []string{"ops@mise.test"}}, admin), http.StatusOK)
	if _, err := rep.Due(ctx); err != nil {
		t.Fatal(err)
	}
	if n := h.count(`SELECT count(*) FROM report_run_deliveries d JOIN report_runs r ON r.id = d.run_id WHERE r.schedule_id = $1 AND d.status = 'skipped'`, daily); n != 1 {
		t.Errorf("skipped deliveries = %d", n)
	}
}
