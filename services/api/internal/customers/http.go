package customers

import (
	"net/http"

	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/httpx"
)

const maxBody = 16 << 10

// Handler serves the customer's own records under /api/v1/me. The customer is
// always the signed-in one: no route takes a customer id.
type Handler struct{ Service *Service }

func (h *Handler) Routes(mux httpx.Router) {
	req := h.Service.Identity.RequireCustomer
	mux.Handle("GET /api/v1/me/profile", req(h.getProfile))
	mux.Handle("PATCH /api/v1/me/profile", req(h.patchProfile))
	mux.Handle("GET /api/v1/me/household", req(h.getHousehold))
	mux.Handle("PUT /api/v1/me/household", req(h.putHousehold))
	mux.Handle("GET /api/v1/me/preferences", req(h.getPreferences))
	mux.Handle("PUT /api/v1/me/preferences", req(h.putPreferences))
	mux.Handle("GET /api/v1/me/onboarding", req(h.getOnboarding))
	mux.Handle("PUT /api/v1/me/onboarding", req(h.putOnboarding))
	mux.Handle("GET /api/v1/me/notification-preferences", req(h.getNotifications))
	mux.Handle("PUT /api/v1/me/notification-preferences", req(h.putNotifications))
}

func respond[T any](w http.ResponseWriter, r *http.Request, v T, err error) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, v)
}

func decode[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var in T
	if err := httpx.Decode(w, r, &in, maxBody); err != nil {
		httpx.Fail(w, r, err)
		return in, false
	}
	return in, true
}

func principal(r *http.Request) *identity.Principal { return identity.PrincipalFrom(r.Context()) }

func (h *Handler) getProfile(w http.ResponseWriter, r *http.Request) {
	v, err := h.Service.GetProfile(r.Context(), principal(r))
	respond(w, r, v, err)
}

func (h *Handler) patchProfile(w http.ResponseWriter, r *http.Request) {
	if in, ok := decode[ProfileInput](w, r); ok {
		v, err := h.Service.UpdateProfile(r.Context(), principal(r), in)
		respond(w, r, v, err)
	}
}

func (h *Handler) getHousehold(w http.ResponseWriter, r *http.Request) {
	v, err := h.Service.GetHousehold(r.Context(), principal(r))
	respond(w, r, map[string]*Household{"household": v}, err)
}

func (h *Handler) putHousehold(w http.ResponseWriter, r *http.Request) {
	if in, ok := decode[HouseholdInput](w, r); ok {
		v, err := h.Service.PutHousehold(r.Context(), principal(r), in)
		respond(w, r, map[string]*Household{"household": v}, err)
	}
}

func (h *Handler) getPreferences(w http.ResponseWriter, r *http.Request) {
	v, err := h.Service.PreferencesFor(r.Context(), principal(r))
	respond(w, r, v, err)
}

func (h *Handler) putPreferences(w http.ResponseWriter, r *http.Request) {
	if in, ok := decode[PreferencesInput](w, r); ok {
		v, err := h.Service.PutPreferences(r.Context(), principal(r), in)
		respond(w, r, v, err)
	}
}

func (h *Handler) getOnboarding(w http.ResponseWriter, r *http.Request) {
	v, err := h.Service.GetOnboarding(r.Context(), principal(r))
	respond(w, r, v, err)
}

func (h *Handler) putOnboarding(w http.ResponseWriter, r *http.Request) {
	if in, ok := decode[OnboardingInput](w, r); ok {
		v, err := h.Service.PutOnboarding(r.Context(), principal(r), in)
		respond(w, r, v, err)
	}
}

func (h *Handler) getNotifications(w http.ResponseWriter, r *http.Request) {
	v, err := h.Service.GetNotificationPreferences(r.Context(), principal(r))
	respond(w, r, v, err)
}

func (h *Handler) putNotifications(w http.ResponseWriter, r *http.Request) {
	if in, ok := decode[NotificationInput](w, r); ok {
		v, err := h.Service.PutNotificationPreferences(r.Context(), principal(r), in)
		respond(w, r, v, err)
	}
}
