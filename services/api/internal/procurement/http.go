package procurement

import (
	"net/http"
	"strings"

	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/httpx"
)

type Handler struct {
	S        *Service
	Identity *identity.Service
}

// Routes registers the procurement API. Every route needs an admin session
// and the named permission; separation of duties is checked in the service.
func (h *Handler) Routes(mux httpx.Router) {
	adm := h.Identity.RequireAdmin
	mux.Handle("GET /api/v1/admin/procurement/runs", adm(identity.PermProcurementRead, h.runs))
	mux.Handle("POST /api/v1/admin/procurement/runs", adm(identity.PermProcurementPlan, h.createRun))
	mux.Handle("GET /api/v1/admin/procurement/runs/{id}", adm(identity.PermProcurementRead, h.run))
	mux.Handle("POST /api/v1/admin/procurement/runs/{id}/lines/{line}/decision", adm(identity.PermProcurementPlan, h.decide))
	mux.Handle("POST /api/v1/admin/procurement/runs/{id}/approve", adm(identity.PermProcurementApprove, h.approveRun))
	mux.Handle("POST /api/v1/admin/procurement/runs/{id}/reopen", adm(identity.PermProcurementApprove, h.reopenRun))
	mux.Handle("GET /api/v1/admin/procurement/pending-plans", adm(identity.PermProcurementRead, h.pendingPlans))
	mux.Handle("GET /api/v1/admin/procurement/deliveries", adm(identity.PermProcurementRead, h.deliveries))
	mux.Handle("GET /api/v1/admin/procurement/settings", adm(identity.PermProcurementRead, h.settings))
	mux.Handle("PUT /api/v1/admin/procurement/settings", adm(identity.PermProcurementConfigure, h.saveSettings))
	h.purchasingRoutes(mux)
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

func decode[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var v T
	if r.ContentLength == 0 && r.Header.Get("Content-Type") == "" {
		return v, true
	}
	if err := httpx.Decode(w, r, &v, 256<<10); err != nil {
		httpx.Fail(w, r, err)
		return v, false
	}
	return v, true
}

func id(r *http.Request) string { return strings.ToLower(r.PathValue("id")) }

type reasonInput struct {
	Reason string `json:"reason"`
}

func (h *Handler) runs(w http.ResponseWriter, r *http.Request) {
	runs, err := h.S.Runs(r.Context(), r.URL.Query().Get("status"))
	respond(w, r, http.StatusOK, map[string]any{"items": runs}, err)
}

func (h *Handler) createRun(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[struct {
		PlanID string `json:"plan_id"`
	}](w, r)
	if !ok {
		return
	}
	run, created, err := h.S.CreateRun(r.Context(), actor(r), strings.ToLower(in.PlanID), nil)
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	respond(w, r, status, run, err)
}

func (h *Handler) run(w http.ResponseWriter, r *http.Request) {
	run, err := h.S.Run(r.Context(), id(r))
	respond(w, r, http.StatusOK, run, err)
}

func (h *Handler) decide(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[DecisionInput](w, r)
	if !ok {
		return
	}
	run, err := h.S.Decide(r.Context(), actor(r), id(r), strings.ToLower(r.PathValue("line")), in)
	respond(w, r, http.StatusOK, run, err)
}

func (h *Handler) approveRun(w http.ResponseWriter, r *http.Request) {
	run, err := h.S.ApproveRun(r.Context(), actor(r), id(r))
	respond(w, r, http.StatusOK, run, err)
}

func (h *Handler) reopenRun(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[reasonInput](w, r)
	if !ok {
		return
	}
	run, err := h.S.ReopenRun(r.Context(), actor(r), id(r), in.Reason)
	respond(w, r, http.StatusOK, run, err)
}

func (h *Handler) pendingPlans(w http.ResponseWriter, r *http.Request) {
	p, err := h.S.PendingPlans(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": p}, err)
}

func (h *Handler) deliveries(w http.ResponseWriter, r *http.Request) {
	d, err := h.S.Deliveries(r.Context())
	respond(w, r, http.StatusOK, d, err)
}

func (h *Handler) settings(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.Settings(r.Context())
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) saveSettings(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[SettingsInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.SaveSettings(r.Context(), actor(r), in)
	respond(w, r, http.StatusOK, x, err)
}
