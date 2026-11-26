package behavior

import (
	"context"
	"net/http"

	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/httpx"
)

// CustomerIDs resolves a signed-in customer's id.
type CustomerIDs interface {
	CustomerID(ctx context.Context, p *identity.Principal) (string, error)
}

type Handler struct {
	S         *Service
	Identity  *identity.Service
	Customers CustomerIDs
	// Limit guards event intake per client address.
	Limit func(http.Handler) http.Handler
}

func (h *Handler) Routes(mux httpx.Router) {
	limit := h.Limit
	if limit == nil {
		limit = func(n http.Handler) http.Handler { return n }
	}
	// The customer app (its server) sends events here; a customer session token attributes them.
	mux.Handle("POST /api/v1/behavior/events", limit(http.HandlerFunc(h.ingest)))

	adm := h.Identity.RequireAdmin
	mux.Handle("GET /api/v1/admin/intelligence/funnel", adm(identity.PermIntelligenceCustomerRead, h.funnel))
	mux.Handle("GET /api/v1/admin/intelligence/segments", adm(identity.PermIntelligenceCustomerRead, h.segments))
	mux.Handle("GET /api/v1/admin/intelligence/segments/{key}/members", adm(identity.PermIntelligenceCustomerRead, h.members))
	mux.Handle("GET /api/v1/admin/intelligence/cancellation-reasons", adm(identity.PermIntelligenceCustomerRead, h.reasons))
}

func reply(w http.ResponseWriter, r *http.Request, status int, v any, err error) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, status, v)
}

func (h *Handler) ingest(w http.ResponseWriter, r *http.Request) {
	// The apps never send under Global Privacy Control or Do Not Track; if a
	// request arrives with either signal anyway, nothing is stored.
	if r.Header.Get("Sec-GPC") == "1" || r.Header.Get("DNT") == "1" {
		httpx.JSON(w, r, http.StatusAccepted, map[string]int{"accepted": 0})
		return
	}
	var b Batch
	if err := httpx.Decode(w, r, &b, 32<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	customerID := ""
	if tok := identity.BearerToken(r); tok != "" {
		p, err := h.Identity.Authenticate(r.Context(), tok, identity.AudienceCustomer)
		if err != nil || p == nil {
			httpx.Fail(w, r, httpx.ErrUnauthorized)
			return
		}
		if customerID, err = h.Customers.CustomerID(r.Context(), p); err != nil {
			httpx.Fail(w, r, err)
			return
		}
	}
	n, err := h.S.Record(r.Context(), customerID, b)
	reply(w, r, http.StatusAccepted, map[string]int{"accepted": n}, err)
}

func (h *Handler) funnel(w http.ResponseWriter, r *http.Request) {
	f, err := h.S.Funnel(r.Context(), r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	reply(w, r, http.StatusOK, f, err)
}

func (h *Handler) segments(w http.ResponseWriter, r *http.Request) {
	list, err := h.S.Segments(r.Context())
	reply(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

// members names customers, so it also needs customers.read.
func (h *Handler) members(w http.ResponseWriter, r *http.Request) {
	if !identity.PrincipalFrom(r.Context()).Can(identity.PermCustomersRead) {
		httpx.Fail(w, r, httpx.ErrForbidden)
		return
	}
	list, err := h.S.Members(r.Context(), r.PathValue("key"))
	reply(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

func (h *Handler) reasons(w http.ResponseWriter, r *http.Request) {
	list, total, err := h.S.CancellationReasons(r.Context(), r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	reply(w, r, http.StatusOK, map[string]any{"items": list, "total": total,
		"note": "Only reasons customers chose are shown; cancellations without one are counted as not recorded. No reason is inferred."}, err)
}
