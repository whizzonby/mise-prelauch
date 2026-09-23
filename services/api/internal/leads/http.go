package leads

import (
	"net/http"
	"strings"

	"mise.tt/api/internal/platform/httpx"
)

const maxBody = 16 << 10

type Handler struct {
	Service *Service
}

// Create serves POST /api/v1/leads.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var in SignupInput
	if err := httpx.Decode(w, r, &in, maxBody); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	result, err := h.Service.Signup(r.Context(), in, httpx.ClientIP(r.Context()))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	status := http.StatusOK
	if result.Outcome == OutcomeCreated {
		status = http.StatusCreated
	}
	httpx.JSON(w, r, status, result)
}

type tokenBody struct {
	Token string `json:"token"`
}

// Verify serves POST /api/v1/leads/verify.
func (h *Handler) Verify(w http.ResponseWriter, r *http.Request) {
	var in tokenBody
	if err := httpx.Decode(w, r, &in, maxBody); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	result, err := h.Service.Verify(r.Context(), in.Token)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, result)
}

// Unsubscribe serves POST /api/v1/leads/unsubscribe.
func (h *Handler) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	var in tokenBody
	if err := httpx.Decode(w, r, &in, maxBody); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if err := h.Service.Unsubscribe(r.Context(), in.Token); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, map[string]bool{"unsubscribed": true})
}

// Me serves GET /api/v1/leads/me.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	leadID, err := h.Service.Authenticate(BearerToken(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	summary, err := h.Service.Me(r.Context(), leadID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, summary)
}

// UpdatePreferences serves PATCH /api/v1/leads/preferences.
func (h *Handler) UpdatePreferences(w http.ResponseWriter, r *http.Request) {
	leadID, err := h.Service.Authenticate(BearerToken(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	var in PreferencesInput
	if err := httpx.Decode(w, r, &in, maxBody); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	summary, err := h.Service.UpdatePreferences(r.Context(), leadID, in)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, summary)
}

// BearerToken extracts the token from an Authorization header.
func BearerToken(r *http.Request) string {
	scheme, tok, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(tok)
}
