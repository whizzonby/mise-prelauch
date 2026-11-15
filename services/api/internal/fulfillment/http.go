package fulfillment

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

// Routes registers the fulfillment API. Every route needs an admin session
// and the named permission, enforced here, not in the browser.
func (h *Handler) Routes(mux httpx.Router) {
	adm := h.Identity.RequireAdmin
	mux.Handle("GET /api/v1/admin/fulfillment", adm(identity.PermFulfillmentRead, h.list))
	mux.Handle("POST /api/v1/admin/fulfillment/sync", adm(identity.PermFulfillmentRelease, h.sync))
	mux.Handle("GET /api/v1/admin/fulfillment/consumer", adm(identity.PermFulfillmentRead, h.consumer))
	mux.Handle("POST /api/v1/admin/fulfillment/batches/{id}/flag", adm(identity.PermFulfillmentVerify, h.flag))
	mux.Handle("GET /api/v1/admin/fulfillment/{id}", adm(identity.PermFulfillmentRead, h.get))
	mux.Handle("POST /api/v1/admin/fulfillment/{id}/verify", adm(identity.PermFulfillmentVerify, h.verify))
	mux.Handle("POST /api/v1/admin/fulfillment/{id}/release", adm(identity.PermFulfillmentRelease, h.release))
	mux.Handle("GET /api/v1/admin/fulfillment/{id}/labels", adm(identity.PermFulfillmentRead, h.labels))
	mux.Handle("GET /api/v1/admin/fulfillment/{id}/trace", adm(identity.PermFulfillmentRead, h.trace))
}

func respond(w http.ResponseWriter, r *http.Request, status int, v any, err error) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, status, v)
}

func actor(r *http.Request) string { return identity.PrincipalFrom(r.Context()).UserID }

func id(r *http.Request) string { return strings.ToLower(r.PathValue("id")) }

func decode[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var v T
	if r.ContentLength == 0 && r.Header.Get("Content-Type") == "" {
		return v, true
	}
	if err := httpx.Decode(w, r, &v, 64<<10); err != nil {
		httpx.Fail(w, r, err)
		return v, false
	}
	return v, true
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	items, err := h.S.List(r.Context(), Filter{Status: q.Get("status"), Week: q.Get("week"), Query: q.Get("q")})
	respond(w, r, http.StatusOK, map[string]any{"items": items}, err)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	o, err := h.S.Get(r.Context(), id(r))
	respond(w, r, http.StatusOK, o, err)
}

func (h *Handler) sync(w http.ResponseWriter, r *http.Request) {
	out, err := h.S.Sync(r.Context(), actor(r))
	respond(w, r, http.StatusOK, map[string]any{"items": out}, err)
}

func (h *Handler) consumer(w http.ResponseWriter, r *http.Request) {
	st, err := h.S.ConsumerStatus(r.Context())
	respond(w, r, http.StatusOK, st, err)
}

func (h *Handler) verify(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[VerifyInput](w, r)
	if !ok {
		return
	}
	o, err := h.S.Verify(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusOK, o, err)
}

func (h *Handler) release(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[struct {
		Version *int `json:"version"`
	}](w, r)
	if !ok {
		return
	}
	o, err := h.S.Release(r.Context(), actor(r), id(r), in.Version)
	respond(w, r, http.StatusOK, o, err)
}

func (h *Handler) labels(w http.ResponseWriter, r *http.Request) {
	ls, err := h.S.Labels(r.Context(), id(r))
	respond(w, r, http.StatusOK, map[string]any{"items": ls}, err)
}

func (h *Handler) trace(w http.ResponseWriter, r *http.Request) {
	t, err := h.S.Trace(r.Context(), id(r))
	respond(w, r, http.StatusOK, t, err)
}

func (h *Handler) flag(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[struct {
		Reason string `json:"reason"`
	}](w, r)
	if !ok {
		return
	}
	exc, err := h.S.FlagBatch(r.Context(), actor(r), id(r), in.Reason)
	respond(w, r, http.StatusCreated, map[string]any{"exception_id": exc}, err)
}
