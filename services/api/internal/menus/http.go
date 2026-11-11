package menus

import (
	"net/http"
	"regexp"
	"time"

	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/httpx"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type Handler struct {
	Service  *Service
	Identity *identity.Service
}

func (h *Handler) Routes(mux httpx.Router) {
	// Public: a published menu is the same for everyone.
	mux.Handle("GET /api/v1/weekly-menus", http.HandlerFunc(h.list))
	mux.Handle("GET /api/v1/weekly-menus/{week}", http.HandlerFunc(h.week))

	adm := h.Identity.RequireAdmin
	mux.Handle("GET /api/v1/admin/menus", adm(identity.PermMenusManage, h.adminList))
	mux.Handle("POST /api/v1/admin/menus", adm(identity.PermMenusManage, h.adminCreate))
	mux.Handle("GET /api/v1/admin/menus/{id}", adm(identity.PermMenusManage, h.adminGet))
	mux.Handle("PUT /api/v1/admin/menus/{id}", adm(identity.PermMenusManage, h.adminUpdate))
	mux.Handle("POST /api/v1/admin/menus/{id}/publish", adm(identity.PermMenusPublish, h.adminPublish))
	mux.Handle("POST /api/v1/admin/menus/{id}/unpublish", adm(identity.PermMenusPublish, h.adminUnpublish))
}

func respond[T any](w http.ResponseWriter, r *http.Request, status int, v T, err error) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, status, v)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	list, err := h.Service.Published(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

func (h *Handler) week(w http.ResponseWriter, r *http.Request) {
	week, ok := ParseWeek(r.PathValue("week"))
	if !ok {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	m, err := h.Service.PublishedWeek(r.Context(), week)
	respond(w, r, http.StatusOK, m, err)
}

func actor(r *http.Request) string { return identity.PrincipalFrom(r.Context()).UserID }

func pathID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if !uuidPattern.MatchString(id) {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return "", false
	}
	return id, true
}

func (h *Handler) adminList(w http.ResponseWriter, r *http.Request) {
	list, err := h.Service.AdminList(r.Context(), r.URL.Query().Get("status"))
	respond(w, r, http.StatusOK, map[string]any{
		"items":          list,
		"next_open_week": FormatWeek(h.Service.Policy.NextOpenWeek(h.Service.Now())),
		"time_zone":      TimeZone,
	}, err)
}

func (h *Handler) adminGet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	m, err := h.Service.AdminGet(r.Context(), id)
	respond(w, r, http.StatusOK, m, err)
}

func (h *Handler) adminCreate(w http.ResponseWriter, r *http.Request) {
	var in Input
	if err := httpx.Decode(w, r, &in, 32<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	m, err := h.Service.Create(r.Context(), actor(r), in)
	respond(w, r, http.StatusCreated, m, err)
}

func (h *Handler) adminUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in Input
	if err := httpx.Decode(w, r, &in, 32<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	m, err := h.Service.Update(r.Context(), actor(r), id, in)
	respond(w, r, http.StatusOK, m, err)
}

func (h *Handler) adminPublish(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		PublishAt *time.Time `json:"publish_at"`
	}
	if r.ContentLength > 0 {
		if err := httpx.Decode(w, r, &in, 1<<10); err != nil {
			httpx.Fail(w, r, err)
			return
		}
	}
	m, err := h.Service.Publish(r.Context(), actor(r), id, in.PublishAt)
	respond(w, r, http.StatusOK, m, err)
}

func (h *Handler) adminUnpublish(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	m, err := h.Service.Unpublish(r.Context(), actor(r), id)
	respond(w, r, http.StatusOK, m, err)
}
