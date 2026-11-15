package delivery

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/httpx"
)

type Handler struct {
	S        *Service
	Identity *identity.Service
}

// Routes registers the delivery API. Every route needs an admin session and
// the named permission, checked here. Routes couriers may use take any admin
// session and check "all deliveries" or "my assigned runs" in the handler.
func (h *Handler) Routes(mux httpx.Router) {
	adm := h.Identity.RequireAdmin
	// Settings
	// Policies, not personal data: every staff role that records deliveries
	// (couriers included) needs the approved proof methods and failure reasons.
	mux.Handle("GET /api/v1/admin/delivery/settings", adm(identity.PermAdminAccess, h.settings))
	mux.Handle("PUT /api/v1/admin/delivery/settings", adm(identity.PermDeliverySettingsManage, h.putSettings))
	// Zones, coverage, calendar, capacity
	mux.Handle("GET /api/v1/admin/delivery/zones", adm(identity.PermDeliveryZonesRead, h.zones))
	mux.Handle("POST /api/v1/admin/delivery/zones", adm(identity.PermDeliveryZonesManage, h.saveZone))
	mux.Handle("PATCH /api/v1/admin/delivery/zones/{id}", adm(identity.PermDeliveryZonesManage, h.saveZone))
	mux.Handle("GET /api/v1/admin/delivery/coverage", adm(identity.PermDeliveryZonesRead, h.coverage))
	mux.Handle("GET /api/v1/admin/delivery/calendar", adm(identity.PermDeliveryZonesRead, h.calendar))
	mux.Handle("PUT /api/v1/admin/delivery/calendar", adm(identity.PermDeliveryZonesManage, h.saveDay))
	mux.Handle("GET /api/v1/admin/delivery/capacity", adm(identity.PermDeliveryZonesRead, h.capacity))
	mux.Handle("PUT /api/v1/admin/delivery/capacity", adm(identity.PermDeliveryZonesManage, h.setCapacity))
	// Exceptions
	mux.Handle("GET /api/v1/admin/delivery/exceptions", adm(identity.PermDeliveryExceptionsRead, h.exceptions))
	mux.Handle("POST /api/v1/admin/delivery/exceptions", adm(identity.PermDeliveryExceptionsManage, h.raise))
	mux.Handle("GET /api/v1/admin/delivery/exceptions/{id}", adm(identity.PermDeliveryExceptionsRead, h.exception))
	mux.Handle("PATCH /api/v1/admin/delivery/exceptions/{id}", adm(identity.PermDeliveryExceptionsManage, h.updateException))
	h.routes(mux)
}

func respond(w http.ResponseWriter, r *http.Request, status int, v any, err error) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, status, v)
}

func principal(r *http.Request) *identity.Principal { return identity.PrincipalFrom(r.Context()) }

func actor(r *http.Request) string { return principal(r).UserID }

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

func (h *Handler) settings(w http.ResponseWriter, r *http.Request) {
	st, err := h.S.Settings(r.Context())
	respond(w, r, http.StatusOK, st, err)
}

func (h *Handler) putSettings(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[SettingsInput](w, r)
	if !ok {
		return
	}
	st, err := h.S.PutSettings(r.Context(), actor(r), in)
	respond(w, r, http.StatusOK, st, err)
}

func (h *Handler) zones(w http.ResponseWriter, r *http.Request) {
	z, err := h.S.Zones(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": z}, err)
}

func (h *Handler) saveZone(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[ZoneInput](w, r)
	if !ok {
		return
	}
	status := http.StatusOK
	if r.Method == http.MethodPost {
		status = http.StatusCreated
	}
	z, err := h.S.SaveZone(r.Context(), actor(r), id(r), in)
	respond(w, r, status, z, err)
}

func (h *Handler) coverage(w http.ResponseWriter, r *http.Request) {
	c, err := h.S.RegionCoverage(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": c}, err)
}

func (h *Handler) calendar(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	c, err := h.S.Calendar(r.Context(), r.URL.Query().Get("from"), days)
	respond(w, r, http.StatusOK, map[string]any{"items": c}, err)
}

func (h *Handler) saveDay(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[ServiceDayInput](w, r)
	if !ok {
		return
	}
	d, err := h.S.SaveServiceDay(r.Context(), actor(r), in)
	respond(w, r, http.StatusOK, d, err)
}

func (h *Handler) capacity(w http.ResponseWriter, r *http.Request) {
	c, err := h.S.CapacityFor(r.Context(), r.URL.Query().Get("date"))
	respond(w, r, http.StatusOK, map[string]any{"items": c}, err)
}

func (h *Handler) setCapacity(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[CapacityInput](w, r)
	if !ok {
		return
	}
	c, err := h.S.SetCapacity(r.Context(), actor(r), in)
	respond(w, r, http.StatusOK, map[string]any{"items": c}, err)
}

func (h *Handler) exceptions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	items, err := h.S.Exceptions(r.Context(), ExceptionFilter{Status: q.Get("status"), Stage: q.Get("stage"), DeliveryID: q.Get("delivery_id"),
		CustomerID: q.Get("customer_id")})
	respond(w, r, http.StatusOK, map[string]any{"items": items}, err)
}

func (h *Handler) exception(w http.ResponseWriter, r *http.Request) {
	e, err := h.S.Exception(r.Context(), id(r))
	respond(w, r, http.StatusOK, e, err)
}

func (h *Handler) raise(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[ExceptionInput](w, r)
	if !ok {
		return
	}
	in.Actor = actor(r)
	var exID string
	err := h.S.tx(r.Context(), func(tx pgx.Tx) error {
		var err error
		exID, err = h.S.RaiseException(r.Context(), tx, in)
		if err == nil && in.Blocking && in.ObligationID != "" && h.S.Fulfillment != nil {
			err = h.S.Fulfillment.Interrupt(r.Context(), tx, in.ObligationID, in.Actor, "Exception raised: "+in.Summary)
		}
		return err
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	e, err := h.S.Exception(r.Context(), exID)
	respond(w, r, http.StatusCreated, e, err)
}

func (h *Handler) updateException(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[ExceptionUpdate](w, r)
	if !ok {
		return
	}
	e, err := h.S.UpdateException(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusOK, e, err)
}
