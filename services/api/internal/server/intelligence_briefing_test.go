package server_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"mise.tt/api/internal/intelligence"
	"mise.tt/api/internal/server"
)

// Sprint 9.2 Stage 8: executive briefings.

func TestBriefingPeriodsAreCompletePeriods(t *testing.T) {
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC) // a Friday
	for cadence, want := range map[string][2]string{
		"daily": {"2026-11-26", "2026-11-26"}, "weekly": {"2026-11-16", "2026-11-22"}, "monthly": {"2026-10-01", "2026-10-31"},
	} {
		f, to, err := intelligence.BriefingPeriod(cadence, now)
		if err != nil || f.Format("2006-01-02") != want[0] || to.Format("2006-01-02") != want[1] {
			t.Errorf("%s = %s..%s, want %v", cadence, f.Format("2006-01-02"), to.Format("2006-01-02"), want)
		}
	}
	// On a Monday the last complete week ends the day before.
	f, to, _ := intelligence.BriefingPeriod("weekly", time.Date(2026, 11, 23, 8, 0, 0, 0, time.UTC))
	if f.Format("2006-01-02") != "2026-11-16" || to.Format("2006-01-02") != "2026-11-22" {
		t.Errorf("Monday weekly = %s..%s", f, to)
	}
}

func TestBriefingsAreDeterministicVersionedAndScoped(t *testing.T) {
	h := newHarness(t)
	owner := bearer(h.adminLogin("owner-brief@mise.test", "super_admin"))
	h.adminLogin("admin-brief@mise.test", "admin")
	admin := bearer(h.adminLogin("admin-brief@mise.test", "admin"))
	fin := bearer(h.adminLogin("finance-brief@mise.test", "finance"))
	base := "/api/v1/admin/intelligence/briefings"

	res := h.do("POST", base, map[string]any{"cadence": "weekly"}, owner)
	h.wantStatus(res, http.StatusCreated)
	c := obj(t, res.Data["content"])
	if res.Data["version"] != float64(1) || len(list(t, c["performance"])) != 10 || len(list(t, c["customer_trends"])) != 6 ||
		res.Data["narrative"] != nil || !strings.HasPrefix(res.Data["ai_note"].(string), "AI unavailable") {
		t.Fatalf("briefing = %v", res.Data)
	}
	if !strings.Contains(strings.Join(toStrings(t, c["data_limitations"]), " "), "Expense") && len(list(t, c["key_risks"])) == 0 {
		t.Fatalf("missing expenses neither a risk nor a limitation: %v", c)
	}
	fp := strings.Join(flattenItems(t, c["financial_position"]), " ")
	if !strings.Contains(fp, "withheld") || !strings.Contains(fp, "Cash position: not available") {
		t.Fatalf("financial position = %s", fp)
	}
	// Identical content: the same version, not a new one.
	again := h.do("POST", base, map[string]any{"cadence": "weekly"}, admin)
	h.wantStatus(again, http.StatusOK)
	if again.Data["id"] != res.Data["id"] {
		t.Fatal("identical briefing regenerated")
	}
	// Changed inputs (an indicator switched off): a new version.
	h.wantStatus(h.do("PUT", "/api/v1/admin/intelligence/risk-thresholds/incomplete_financial_records",
		map[string]any{"enabled": false, "warn": 1, "critical": 4, "note": "test"}, owner), http.StatusOK)
	v2 := h.do("POST", base, map[string]any{"cadence": "weekly"}, owner)
	h.wantStatus(v2, http.StatusCreated)
	if v2.Data["version"] != float64(2) || v2.Data["input_hash"] == res.Data["input_hash"] {
		t.Fatalf("v2 = %v", v2.Data["version"])
	}
	h.wantStatus(h.do("POST", base, map[string]any{"cadence": "hourly"}, owner), http.StatusUnprocessableEntity)
	h.wantStatus(h.do("POST", base, map[string]any{"cadence": "monthly"}, owner), http.StatusCreated)
	if n := len(list(t, h.do("GET", base+"?cadence=weekly", nil, owner).Data["items"])); n != 2 {
		t.Fatalf("weekly versions = %d", n)
	}
	// Executive readers only.
	h.wantStatus(h.do("GET", base, nil, fin), http.StatusForbidden)
	h.wantStatus(h.do("POST", base, map[string]any{"cadence": "daily"}, fin), http.StatusForbidden)
	// No AI: no narrative is invented.
	h.wantStatus(h.do("POST", base+"/"+res.Data["id"].(string)+"/narrative", nil, owner), http.StatusServiceUnavailable)

	// Schedules: disabled as shipped; recipients must be executive readers; configure only.
	sch := list(t, h.do("GET", "/api/v1/admin/intelligence/briefing-schedules", nil, owner).Data["items"])
	if len(sch) != 3 || obj(t, sch[0])["enabled"] != false {
		t.Fatalf("schedules = %v", sch)
	}
	path := "/api/v1/admin/intelligence/briefing-schedules/weekly"
	adminID, finID := userID(t, h, "admin-brief@mise.test"), userID(t, h, "finance-brief@mise.test")
	h.wantStatus(h.do("PUT", path, map[string]any{"enabled": true, "recipients": []any{}, "approval_ref": "x"}, owner), http.StatusUnprocessableEntity)
	h.wantStatus(h.do("PUT", path, map[string]any{"enabled": true, "recipients": []any{finID}, "approval_ref": "x"}, owner), http.StatusUnprocessableEntity)
	h.wantStatus(h.do("PUT", path, map[string]any{"enabled": true, "recipients": []any{adminID}}, owner), http.StatusUnprocessableEntity)
	h.wantStatus(h.do("PUT", path, map[string]any{"enabled": true, "recipients": []any{adminID}, "approval_ref": "x"}, admin), http.StatusForbidden)
	h.wantStatus(h.do("PUT", "/api/v1/admin/intelligence/briefing-schedules/hourly", map[string]any{"enabled": false}, owner), http.StatusNotFound)
	h.wantStatus(h.do("PUT", path, map[string]any{"enabled": true, "recipients": []any{adminID}, "approval_ref": "Owner approved 27 Nov"}, owner), http.StatusOK)

	// The worker: one briefing per period, one email per approved recipient, the real outcome recorded.
	sender := server.NewBriefings(h.cfg, h.pool, h.com, server.NewAnalytics(h.pool, h.com), h.ids, h.mailer)
	h.runner.Register(intelligence.JobBriefingEmail, sender.Handle)
	if n, err := sender.Box.DueBriefings(t.Context()); err != nil || n != 1 {
		t.Fatalf("due = %d, %v", n, err)
	}
	if n, _ := sender.Box.DueBriefings(t.Context()); n != 0 {
		t.Fatal("the same period was sent twice")
	}
	h.drainJobs()
	msg := h.mailer.lastTo(t, "admin-brief@mise.test")
	if !strings.Contains(msg.Subject, "weekly briefing") || !strings.Contains(msg.Text, "Data limitations") || !strings.Contains(msg.Text, "not audited") {
		t.Fatalf("email = %s / %.200s", msg.Subject, msg.Text)
	}
	var status string
	_ = h.pool.QueryRow(t.Context(), `SELECT status FROM intelligence_briefing_deliveries`).Scan(&status)
	if status != "sent" {
		t.Fatalf("delivery = %s", status)
	}
}

func flattenItems(t *testing.T, v any) []string {
	var out []string
	for _, x := range list(t, v) {
		out = append(out, obj(t, x)["title"].(string))
	}
	return out
}

func TestBriefingNarrativeIsCheckedAndNotRegenerated(t *testing.T) {
	h, f := aiHarness(t)
	owner := bearer(h.adminLogin("owner-narr@mise.test", "super_admin"))
	h.wantStatus(h.do("PUT", "/api/v1/admin/intelligence/ai/settings", aiOn, owner), http.StatusOK)
	br := h.do("POST", "/api/v1/admin/intelligence/briefings", map[string]any{"cadence": "monthly"}, owner)
	h.wantStatus(br, http.StatusCreated)
	f.set(func(n int, req map[string]any) (int, string) {
		if !strings.Contains(strings.ToLower(req["messages"].([]any)[1].(map[string]any)["content"].(string)), "briefing (data)") {
			t.Error("narrative request lacks the briefing data")
		}
		return 200, `{"model":"fake-model-1","choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"Revenue rose to 4242 this month. Profit is withheld."}}],"usage":{"prompt_tokens":900,"completion_tokens":40}}`
	})
	id := br.Data["id"].(string)
	res := h.do("POST", "/api/v1/admin/intelligence/briefings/"+id+"/narrative", nil, owner)
	h.wantStatus(res, http.StatusOK)
	n := obj(t, res.Data["narrative"])
	if strings.Contains(n["text"].(string), "4242") || !strings.Contains(n["text"].(string), "[unverified]") || n["prompt_version"] != "briefing-v1" {
		t.Fatalf("narrative = %v", n)
	}
	again := h.do("POST", "/api/v1/admin/intelligence/briefings/"+id+"/narrative", nil, owner)
	h.wantStatus(again, http.StatusOK)
	if len(f.requests()) != 1 {
		t.Fatalf("narrative regenerated (%d model calls)", len(f.requests()))
	}
	var kind string
	_ = h.pool.QueryRow(t.Context(), `SELECT kind FROM intelligence_runs`).Scan(&kind)
	if kind != "briefing" {
		t.Fatalf("run kind = %s", kind)
	}
}
