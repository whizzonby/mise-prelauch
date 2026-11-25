package server_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"mise.tt/api/internal/analytics"
	"mise.tt/api/internal/payments/testpay"
	"mise.tt/api/internal/platform/consumer"
	"mise.tt/api/internal/server"
)

// metricOf finds a metric's value on a dashboard response.
// reconcileWithDashboard checks that a report figure equals the registry
// metric with the same definition on an analytics dashboard, for one range.
func (h *harness) reconcileWithDashboard(kind, from, to string, pairs map[string]float64) {
	h.t.Helper()
	viewer := bearer(h.adminLogin("reconcile-"+kind+"@mise.test", "analyst"))
	res := h.do("GET", "/api/v1/admin/analytics/"+kind+"?from="+from+"&to="+to, nil, viewer)
	h.wantStatus(res, http.StatusOK)
	for id, want := range pairs {
		got, _ := metricOf(h.t, res.Data, id)
		if got != want {
			h.t.Errorf("%s on the %s dashboard = %v, report says %v", id, kind, got, want)
		}
	}
}

func metricOf(t *testing.T, d map[string]any, id string) (value any, previous any) {
	t.Helper()
	for _, s := range list(t, d["sections"]) {
		for _, m := range list(t, obj(t, s)["metrics"]) {
			if obj(t, m)["id"] == id {
				return obj(t, m)["value"], obj(t, m)["previous"]
			}
		}
	}
	t.Fatalf("metric %s not on dashboard %v", id, d["kind"])
	return nil, nil
}

func tableOf(t *testing.T, d map[string]any, title string) [][]string {
	t.Helper()
	for _, s := range list(t, d["sections"]) {
		for _, tb := range list(t, obj(t, s)["tables"]) {
			if obj(t, tb)["title"] == title {
				out := [][]string{}
				for _, r := range list(t, obj(t, tb)["rows"]) {
					row := []string{}
					for _, c := range list(t, r) {
						row = append(row, c.(string))
					}
					out = append(out, row)
				}
				return out
			}
		}
	}
	t.Fatalf("table %q not found", title)
	return nil
}

func (h *harness) scalar(sql string, args ...any) float64 {
	h.t.Helper()
	var v float64
	if err := h.pool.QueryRow(context.Background(), sql, args...).Scan(&v); err != nil {
		h.t.Fatalf("scalar: %v", err)
	}
	return v
}

func TestAnalyticsReconcileWithAuthoritativeRecords(t *testing.T) {
	w := newKitchenWeek(t)
	h := w.h
	ctx := context.Background()
	s := w.s
	admin := bearer(h.adminLogin("chief@mise.test", "admin"))
	analyst := bearer(h.adminLogin("numbers@mise.test", "analyst"))
	// A third customer's first payment is declined: it must never count as collected.
	tok, addr := s.customer("declined@example.com")
	_, ref := s.subscribe(tok, addr, s.basic, nil)
	s.pay(ref, "decline", testpay.CardApprove)
	h.drainJobs()
	if n := h.count(`SELECT count(*) FROM payments WHERE status = 'failed'`); n < 1 {
		t.Fatalf("failed payments = %d", n)
	}
	// One payment is partly refunded; the same form sent twice is one refund.
	var payment string
	if err := h.pool.QueryRow(ctx, `SELECT payment_id FROM orders WHERE id = $1`, w.orderA).Scan(&payment); err != nil {
		t.Fatal(err)
	}
	finance := bearer(h.adminLogin("finance@mise.test", "finance"))
	for i := 0; i < 2; i++ {
		h.wantStatus(h.do("POST", "/api/v1/admin/payments/"+payment+"/refunds", map[string]any{"amount_minor": 5000, "reason": "Late delivery credit",
			"request_key": "credit-ana-1"}, finance), http.StatusCreated)
	}
	// Rework is part of accepted, never added twice.
	h.wantStatus(h.do("POST", "/api/v1/admin/production/batches/"+w.curry+"/start", nil, w.team.kitchen), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/production/batches/"+w.curry+"/output", map[string]any{"produced_kits": 6, "rejected_kits": 1, "reworked_kits": 2}, w.team.kitchen), http.StatusOK)

	from, to := "2026-11-01", "2026-11-30"
	get := func(kind string, as reqOpt) map[string]any {
		res := h.do("GET", "/api/v1/admin/analytics/"+kind+"?from="+from+"&to="+to, nil, as)
		h.wantStatus(res, http.StatusOK)
		return res.Data
	}
	rev := get("revenue", analyst)
	collected, _ := metricOf(t, rev, "revenue.collected")
	refunded, _ := metricOf(t, rev, "revenue.refunded")
	net, _ := metricOf(t, rev, "revenue.net_collected")
	wantCollected := h.scalar(`SELECT coalesce(sum(amount_minor), 0)::float8 FROM payments WHERE succeeded_at IS NOT NULL`)
	if collected != wantCollected || refunded != float64(5000) || net != wantCollected-5000 {
		t.Fatalf("collected %v (want %v), refunded %v, net %v", collected, wantCollected, refunded, net)
	}
	if test, _ := metricOf(t, rev, "revenue.collected_test"); test != wantCollected {
		t.Errorf("test-provider share = %v", test)
	}
	if !strings.Contains(strings.Join(func() []string {
		out := []string{}
		for _, n := range list(t, rev["notices"]) {
			out = append(out, n.(string))
		}
		return out
	}(), " "), "simulated test payment provider") {
		t.Errorf("no test-data notice: %v", rev["notices"])
	}
	if failed, _ := metricOf(t, rev, "revenue.failed_payments"); failed != h.scalar(`SELECT count(*)::float8 FROM payments WHERE status = 'failed'`) {
		t.Errorf("failed payments = %v", failed)
	}
	// Subscriptions agree with the authoritative records.
	if active, _ := metricOf(t, rev, "subscriptions.active"); active != h.scalar(`SELECT count(*)::float8 FROM subscriptions WHERE status IN ('active', 'pause_scheduled', 'paused', 'past_due', 'cancel_scheduled')`) {
		t.Errorf("active subscriptions = %v", active)
	}
	if n, _ := metricOf(t, rev, "subscriptions.new"); n != float64(2) {
		t.Errorf("new subscriptions = %v", n)
	}
	// Meal demand is confirmed orders only; production counts rework once.
	meals := get("meals", analyst)
	if kits, _ := metricOf(t, meals, "meals.kits_ordered"); kits != float64(6) {
		t.Errorf("kits ordered = %v", kits)
	}
	prod := get("production", analyst)
	acc, _ := metricOf(t, prod, "production.accepted_kits")
	rew, _ := metricOf(t, prod, "production.reworked_kits")
	pro, _ := metricOf(t, prod, "production.produced_kits")
	if acc != float64(5) || rew != float64(2) || pro != float64(6) {
		t.Errorf("produced %v accepted %v reworked %v", pro, acc, rew)
	}
	// Cross-domain rows use real references: the order week shows its kits.
	ov := get("overview", admin)
	weekly := tableOf(t, ov, "By order week")
	found := false
	for _, r := range weekly {
		if r[0] == "2026-11-16" {
			found = r[1] == "6"
		}
	}
	if !found {
		t.Errorf("weekly cross table = %v", weekly)
	}
	// Cohorts count customers by first payment month.
	cust := get("customers", analyst)
	if rows := tableOf(t, cust, "Cohorts by first payment month"); len(rows) != 1 || rows[0][1] != "2" {
		t.Errorf("cohorts = %v", rows)
	}

	// Permissions: kitchen staff see no revenue; unknown dashboards are not found.
	h.wantStatus(h.do("GET", "/api/v1/admin/analytics/revenue", nil, w.team.kitchen), http.StatusForbidden)
	h.wantStatus(h.do("GET", "/api/v1/admin/analytics/revenue/export", nil, w.team.manager), http.StatusForbidden)
	h.wantStatus(h.do("GET", "/api/v1/admin/analytics/nonsense", nil, admin), http.StatusNotFound)
	if res := h.do("GET", "/api/v1/admin/analytics/production", nil, w.team.manager); res.Status != http.StatusOK {
		t.Errorf("production manager on production analytics = %d", res.Status)
	}

	// Snapshots: refreshing twice, and replaying every event, changes nothing.
	an := newAnalytics(h)
	r, _ := an.ParseRange("2026-11-10", "2026-11-14")
	if _, err := an.Refresh(ctx, "backfill", "", r); err != nil {
		t.Fatal(err)
	}
	snap := h.count(`SELECT count(*) FROM analytics_snapshots`)
	sum := h.scalar(`SELECT coalesce(sum(value), 0)::float8 FROM analytics_snapshots`)
	if _, err := an.Refresh(ctx, "backfill", "", r); err != nil {
		t.Fatal(err)
	}
	if err := consumer.Replay(ctx, h.pool, "analytics", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := an.Dispatcher(0).Dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	h.registerAnalytics(an)
	h.drainJobs()
	if h.count(`SELECT count(*) FROM analytics_snapshots WHERE day BETWEEN '2026-11-10' AND '2026-11-14'`) != snap ||
		h.scalar(`SELECT coalesce(sum(value), 0)::float8 FROM analytics_snapshots WHERE day BETWEEN '2026-11-10' AND '2026-11-14'`) != sum {
		t.Fatal("a second refresh or an event replay changed the snapshots")
	}
	// Snapshots summed over days equal the live figure for the range.
	live := h.do("GET", "/api/v1/admin/analytics/revenue?from=2026-11-10&to=2026-11-14", nil, analyst).Data
	lc, _ := metricOf(t, live, "revenue.collected")
	if lc != h.scalar(`SELECT coalesce(sum(value), 0)::float8 FROM analytics_snapshots WHERE metric_id = 'revenue.collected' AND day BETWEEN '2026-11-10' AND '2026-11-14'`) {
		t.Errorf("snapshots do not sum to the live figure (%v)", lc)
	}

	// Exports: permissioned, audited, formula-safe.
	h.wantStatus(h.do("POST", "/api/v1/admin/plans", map[string]any{"name": "=HYPERLINK(\"x\")", "summary": "Bad name plan.", "meals_per_week": 3,
		"segment": "everyday", "self_serve": true, "benefits": []string{"x"}}, s.admin), http.StatusCreated)
	res := h.do("GET", "/api/v1/admin/analytics/revenue/export?from="+from+"&to="+to, nil, admin)
	h.wantStatus(res, http.StatusOK)
	if res.Header.Get("Cache-Control") != "no-store, private" || !strings.Contains(string(res.Raw), "revenue.collected") {
		t.Errorf("export = %s", res.Raw)
	}
	if n := h.count(`SELECT count(*) FROM audit_log WHERE action = 'analytics.exported'`); n != 1 {
		t.Errorf("export audits = %d", n)
	}

	// Data quality: a balance edited outside the ledger is caught, test payments are flagged.
	res = h.do("POST", "/api/v1/admin/analytics/data-quality/run", nil, admin)
	h.wantStatus(res, http.StatusOK)
	status := map[string]string{}
	for _, c := range list(t, res.Data["items"]) {
		status[obj(t, c)["id"].(string)] = obj(t, c)["status"].(string)
	}
	if status["test_payments"] != "warning" || status["inventory_ledger_mismatch"] != "ok" || status["payment_refund_mismatch"] != "ok" {
		t.Errorf("checks = %v", status)
	}
	if _, err := h.pool.Exec(ctx, `UPDATE payments SET refunded_minor = refunded_minor + 1 WHERE id = $1`, payment); err != nil {
		t.Fatal(err)
	}
	res = h.do("POST", "/api/v1/admin/analytics/data-quality/run", nil, admin)
	for _, c := range list(t, res.Data["items"]) {
		if obj(t, c)["id"] == "payment_refund_mismatch" && (obj(t, c)["status"] != "failed" || obj(t, c)["count"] != float64(1)) {
			t.Errorf("refund mismatch check = %v", c)
		}
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/analytics/data-quality/run", nil, analyst), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/admin/analytics/data-quality/run", nil, w.team.kitchen), http.StatusForbidden)
}

func newAnalytics(h *harness) *analytics.Service { return server.NewAnalytics(h.pool, h.com) }

// registerAnalytics wires the analytics jobs as the worker does.
func (h *harness) registerAnalytics(an *analytics.Service) {
	h.runner.Register(analytics.JobRefresh, an.HandleRefresh)
	h.runner.Register(analytics.JobEvent, an.HandleEvent)
}
