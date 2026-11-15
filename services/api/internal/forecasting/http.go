package forecasting

import (
	"net/http"

	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/httpx"
)

type Handler struct {
	S        *Service
	Identity *identity.Service
}

func (h *Handler) Routes(mux httpx.Router) {
	adm := h.Identity.RequireAdmin
	mux.Handle("GET /api/v1/admin/forecasting/readiness", adm(identity.PermForecastingRead, h.readiness))
	mux.Handle("GET /api/v1/admin/forecasting/settings", adm(identity.PermForecastingRead, h.settings))
	mux.Handle("PUT /api/v1/admin/forecasting/settings", adm(identity.PermForecastingConfigure, h.saveSettings))
	mux.Handle("GET /api/v1/admin/forecasting/runs", adm(identity.PermForecastingRead, h.runs))
	mux.Handle("POST /api/v1/admin/forecasting/runs", adm(identity.PermForecastingRun, h.run))
	mux.Handle("GET /api/v1/admin/forecasting/runs/{id}", adm(identity.PermForecastingRead, h.get))
	mux.Handle("GET /api/v1/admin/forecasting/runs/{id}/ingredients", adm(identity.PermForecastingRead, h.ingredients))
}

func respond(w http.ResponseWriter, r *http.Request, status int, v any, err error) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, status, v)
}

func actor(r *http.Request) string { return identity.PrincipalFrom(r.Context()).UserID }

func (h *Handler) readiness(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.Readiness(r.Context())
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) settings(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.Settings(r.Context(), h.S.pool)
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) saveSettings(w http.ResponseWriter, r *http.Request) {
	var in SettingsInput
	if err := httpx.Decode(w, r, &in, 8<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	x, err := h.S.SaveSettings(r.Context(), actor(r), in)
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) runs(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.Runs(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": x}, err)
}

// run always records the attempt: 201 with status "refused" when the gate is not passed.
func (h *Handler) run(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.Run(r.Context(), actor(r), "manual")
	respond(w, r, http.StatusCreated, x, err)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.Get(r.Context(), r.PathValue("id"))
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) ingredients(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.Ingredients(r.Context(), r.PathValue("id"), r.URL.Query().Get("week"))
	respond(w, r, http.StatusOK, x, err)
}
