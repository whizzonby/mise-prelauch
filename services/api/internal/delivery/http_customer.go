package delivery

import (
	"context"
	"net/http"
	"strings"

	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/orders"
	"mise.tt/api/internal/platform/httpx"
)

// CustomerHandler serves a signed-in customer's own delivery information.
// It never uses admin routes or admin data: another customer's order
// answers not found.
type CustomerHandler struct {
	S         *Service
	Identity  *identity.Service
	Customers interface {
		CustomerID(ctx context.Context, p *identity.Principal) (string, error)
	}
	Orders interface {
		ForCustomer(ctx context.Context, customerID, id string) (*orders.Order, error)
	}
}

func (h *CustomerHandler) Routes(mux httpx.Router) {
	req := h.Identity.RequireCustomer
	mux.Handle("GET /api/v1/me/orders/{id}/delivery", req(h.get))
	mux.Handle("POST /api/v1/me/orders/{id}/delivery/support", req(h.support))
	mux.Handle("POST /api/v1/me/orders/{id}/delivery/reschedule-requests", req(h.reschedule))
}

func (h *CustomerHandler) order(w http.ResponseWriter, r *http.Request) (string, *orders.Order, bool) {
	cid, err := h.Customers.CustomerID(r.Context(), principal(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return "", nil, false
	}
	oid := strings.ToLower(r.PathValue("id"))
	if !validID(oid) {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return "", nil, false
	}
	o, err := h.Orders.ForCustomer(r.Context(), cid, oid)
	if err != nil {
		httpx.Fail(w, r, err)
		return "", nil, false
	}
	return cid, o, true
}

func (h *CustomerHandler) get(w http.ResponseWriter, r *http.Request) {
	cid, o, ok := h.order(w, r)
	if !ok {
		return
	}
	d, err := h.S.ForCustomer(r.Context(), cid, toFacts(o))
	respond(w, r, http.StatusOK, d, err)
}

func toFacts(o *orders.Order) OrderFacts {
	addr, _ := jsonMarshal(o.Address)
	return OrderFacts{OrderID: o.ID, Status: o.Status, CreatedAt: o.CreatedAt, Address: addr}
}

func (h *CustomerHandler) support(w http.ResponseWriter, r *http.Request) {
	cid, o, ok := h.order(w, r)
	if !ok {
		return
	}
	in, ok := decode[struct {
		Message string `json:"message"`
	}](w, r)
	if !ok {
		return
	}
	c, err := h.S.CustomerSupport(r.Context(), cid, o.ID, o.Number, in.Message)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusCreated, map[string]any{"case_number": c.Number, "status": "received"})
}

func (h *CustomerHandler) reschedule(w http.ResponseWriter, r *http.Request) {
	cid, o, ok := h.order(w, r)
	if !ok {
		return
	}
	in, ok := decode[RescheduleInput](w, r)
	if !ok {
		return
	}
	if err := h.S.CustomerReschedule(r.Context(), cid, o.ID, principal(r).UserID, in); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	d, err := h.S.ForCustomer(r.Context(), cid, toFacts(o))
	respond(w, r, http.StatusCreated, d, err)
}
