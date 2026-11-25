package launch

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
	// Public: which build is serving, for deployment checks and support.
	mux.Handle("GET /api/v1/version", http.HandlerFunc(h.version))

	adm := h.Identity.RequireAdmin
	mux.Handle("GET /api/v1/admin/system/status", adm(identity.PermLaunchRead, h.system))
	mux.Handle("GET /api/v1/admin/launch", adm(identity.PermLaunchRead, h.gates))
	mux.Handle("POST /api/v1/admin/launch/gates/{gate}/signoffs", adm(identity.PermLaunchSignoff, h.signoff))
}

func (h *Handler) version(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, r, http.StatusOK, BuildVersion())
}

func (h *Handler) system(w http.ResponseWriter, r *http.Request) {
	st, err := h.S.SystemStatus(r.Context())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, st)
}

func (h *Handler) gates(w http.ResponseWriter, r *http.Request) {
	ov, err := h.S.Gates(r.Context())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, ov)
}

func (h *Handler) signoff(w http.ResponseWriter, r *http.Request) {
	var in SignoffInput
	if err := httpx.Decode(w, r, &in, 16<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	g, err := h.S.SignOff(r.Context(), identity.PrincipalFrom(r.Context()), r.PathValue("gate"), in)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, g)
}
