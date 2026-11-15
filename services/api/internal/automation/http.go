package automation

import (
	"fmt"
	"net/http"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/httpx"
)

type Handler struct {
	S        *Service
	R        *Reports
	Identity *identity.Service
}

func (h *Handler) Routes(mux httpx.Router) {
	adm := h.Identity.RequireAdmin
	mux.Handle("GET /api/v1/admin/automation/overview", adm(identity.PermAutomationRead, h.overview))
	mux.Handle("GET /api/v1/admin/automation/rules", adm(identity.PermAutomationRead, h.rules))
	mux.Handle("PATCH /api/v1/admin/automation/rules/{id}", adm(identity.PermAutomationManage, h.saveRule))
	mux.Handle("POST /api/v1/admin/automation/rules/{id}/evaluate", adm(identity.PermAutomationExecute, h.evaluate))
	mux.Handle("GET /api/v1/admin/automation/executions", adm(identity.PermAutomationRead, h.executions))
	mux.Handle("GET /api/v1/admin/automation/executions/{id}", adm(identity.PermAutomationRead, h.execution))
	mux.Handle("POST /api/v1/admin/automation/executions/{id}/retry", adm(identity.PermAutomationExecute, h.retry))
	mux.Handle("POST /api/v1/admin/automation/executions/{id}/decision", adm(identity.PermAutomationApprove, h.decide))
	mux.Handle("GET /api/v1/admin/automation/tasks", adm(identity.PermAdminAccess, h.tasks))
	mux.Handle("POST /api/v1/admin/automation/tasks/{id}/close", adm(identity.PermAdminAccess, h.closeTask))
	mux.Handle("GET /api/v1/admin/reports/scheduled", adm(identity.PermAutomationRead, h.schedules))
	mux.Handle("PATCH /api/v1/admin/reports/scheduled/{id}", adm(identity.PermAutomationManage, h.saveSchedule))
	mux.Handle("POST /api/v1/admin/reports/scheduled/{id}/run", adm(identity.PermAutomationExecute, h.runNow))
	mux.Handle("GET /api/v1/admin/reports/runs", adm(identity.PermAutomationRead, h.runs))
	mux.Handle("GET /api/v1/admin/reports/runs/{id}", adm(identity.PermAutomationRead, h.run))
	mux.Handle("GET /api/v1/admin/reports/runs/{id}/csv", adm(identity.PermAnalyticsExport, h.runCSV))
}

func respond(w http.ResponseWriter, r *http.Request, status int, v any, err error) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, status, v)
}

func principal(r *http.Request) *identity.Principal { return identity.PrincipalFrom(r.Context()) }
func actor(r *http.Request) string                  { return principal(r).UserID }

func perms(p *identity.Principal) []string {
	out := make([]string, len(p.Permissions))
	for i, x := range p.Permissions {
		out[i] = string(x)
	}
	return out
}

func decode[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var v T
	if r.ContentLength == 0 && r.Header.Get("Content-Type") == "" {
		return v, true
	}
	if err := httpx.Decode(w, r, &v, 32<<10); err != nil {
		httpx.Fail(w, r, err)
		return v, false
	}
	return v, true
}

func (h *Handler) overview(w http.ResponseWriter, r *http.Request) {
	o, err := h.S.Overview(r.Context())
	respond(w, r, http.StatusOK, o, err)
}

func (h *Handler) rules(w http.ResponseWriter, r *http.Request) {
	rs, err := h.S.Rules(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": rs}, err)
}

func (h *Handler) saveRule(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[RuleInput](w, r)
	if !ok {
		return
	}
	rule, err := h.S.SaveRule(r.Context(), actor(r), r.PathValue("id"), in)
	respond(w, r, http.StatusOK, rule, err)
}

func (h *Handler) evaluate(w http.ResponseWriter, r *http.Request) {
	rule, err := h.S.Rule(r.Context(), h.S.pool, r.PathValue("id"))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if !rule.Enabled {
		httpx.Fail(w, r, conflict("disabled", "Enable the rule (with its approval) before running it."))
		return
	}
	n, err := h.S.Evaluate(r.Context(), rule.ID, "manual", "by "+actor(r))
	if err == nil {
		_ = audit.Record(r.Context(), h.S.pool, audit.Entry{ActorUserID: actor(r), Action: "automation.evaluated", TargetType: "automation_rule", TargetID: rule.ID})
	}
	respond(w, r, http.StatusOK, map[string]any{"executions_created": n}, err)
}

func (h *Handler) executions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	xs, err := h.S.Executions(r.Context(), ExecutionFilter{Status: q.Get("status"), RuleID: q.Get("rule_id")})
	respond(w, r, http.StatusOK, map[string]any{"items": xs}, err)
}

func (h *Handler) execution(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.Execution(r.Context(), r.PathValue("id"))
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) retry(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.Retry(r.Context(), actor(r), r.PathValue("id"))
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) decide(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[struct {
		Decision string `json:"decision"`
		Note     string `json:"note"`
	}](w, r)
	if !ok {
		return
	}
	if in.Decision != "approve" && in.Decision != "reject" {
		httpx.Fail(w, r, fieldErr("decision", "Choose approve or reject."))
		return
	}
	x, err := h.S.Decide(r.Context(), actor(r), r.PathValue("id"), in.Decision == "approve", in.Note)
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) tasks(w http.ResponseWriter, r *http.Request) {
	ts, err := h.S.Tasks(r.Context(), perms(principal(r)), r.URL.Query().Get("status"))
	respond(w, r, http.StatusOK, map[string]any{"items": ts}, err)
}

func (h *Handler) closeTask(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}](w, r)
	if !ok {
		return
	}
	t, err := h.S.CloseTask(r.Context(), actor(r), perms(principal(r)), r.PathValue("id"), in.Status, in.Note)
	respond(w, r, http.StatusOK, t, err)
}

func (h *Handler) schedules(w http.ResponseWriter, r *http.Request) {
	s, err := h.R.Schedules(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": s}, err)
}

func (h *Handler) saveSchedule(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[ScheduleInput](w, r)
	if !ok {
		return
	}
	if err := h.R.SaveSchedule(r.Context(), actor(r), r.PathValue("id"), in); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	s, err := h.R.Schedules(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": s}, err)
}

func (h *Handler) runNow(w http.ResponseWriter, r *http.Request) {
	run, err := h.R.RunNow(r.Context(), actor(r), r.PathValue("id"))
	respond(w, r, http.StatusCreated, run, err)
}

func (h *Handler) runs(w http.ResponseWriter, r *http.Request) {
	rs, err := h.R.Runs(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": rs}, err)
}

func (h *Handler) run(w http.ResponseWriter, r *http.Request) {
	x, err := h.R.Run(r.Context(), r.PathValue("id"), true)
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) runCSV(w http.ResponseWriter, r *http.Request) {
	name, body, err := h.R.CSV(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	_ = audit.Record(r.Context(), h.S.pool, audit.Entry{ActorUserID: actor(r), Action: "automation.report_downloaded", TargetType: "report_run", TargetID: r.PathValue("id")})
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, name))
	_, _ = w.Write([]byte(body))
}
