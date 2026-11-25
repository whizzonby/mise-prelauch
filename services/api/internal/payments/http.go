package payments

import (
	"io"
	"net/http"
	"regexp"

	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/httpx"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

var providerPattern = regexp.MustCompile(`^[a-z0-9_-]{1,30}$`)

type Handler struct {
	Service  *Service
	Identity *identity.Service
}

func (h *Handler) Routes(mux httpx.Router) {
	// Authenticated by the provider's signature, not by a session.
	mux.Handle("POST /api/v1/webhooks/payments/{provider}", http.HandlerFunc(h.webhook))

	adm := h.Identity.RequireAdmin
	mux.Handle("GET /api/v1/admin/payments", adm(identity.PermPaymentsRead, h.adminList))
	mux.Handle("GET /api/v1/admin/payments/{id}", adm(identity.PermPaymentsRead, h.adminGet))
	mux.Handle("POST /api/v1/admin/payments/{id}/refunds", adm(identity.PermPaymentsRefund, h.adminRefund))
	mux.Handle("GET /api/v1/admin/payment-webhooks", adm(identity.PermPaymentsRead, h.adminWebhooks))

	// Sprint 9.1: providers. Live activation is a separate, stronger permission.
	mux.Handle("GET /api/v1/admin/payment-providers", adm(identity.PermPaymentProvidersRead, h.adminProviders))
	mux.Handle("PATCH /api/v1/admin/payment-providers/{provider}", adm(identity.PermPaymentProvidersManage, h.adminProviderUpdate))
	mux.Handle("POST /api/v1/admin/payment-providers/{provider}/mode", adm(identity.PermPaymentProvidersActivate, h.adminProviderMode))

	// Public: which ways to pay are open for a currency (no account data).
	mux.Handle("GET /api/v1/payment-options", http.HandlerFunc(h.options))
}

var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

func (h *Handler) options(w http.ResponseWriter, r *http.Request) {
	cur := r.URL.Query().Get("currency")
	if cur == "" {
		cur = "TTD"
	}
	if !currencyPattern.MatchString(cur) {
		httpx.Fail(w, r, httpx.ValidationError([]httpx.FieldError{{Field: "currency", Code: "invalid", Message: "Use a three-letter currency code."}}))
		return
	}
	opts, err := h.Service.Options(r.Context(), cur)
	if opts == nil {
		opts = []Option{}
	}
	respond(w, r, http.StatusOK, map[string]any{"currency": cur, "items": opts}, err)
}

func (h *Handler) adminProviders(w http.ResponseWriter, r *http.Request) {
	list, err := h.Service.Statuses(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

func (h *Handler) adminProviderUpdate(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("provider")
	if !providerPattern.MatchString(name) {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	var in SettingsInput
	if err := httpx.Decode(w, r, &in, 4<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	st, err := h.Service.UpdateSettings(r.Context(), identity.PrincipalFrom(r.Context()).UserID, name, in)
	respond(w, r, http.StatusOK, st, err)
}

func (h *Handler) adminProviderMode(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("provider")
	if !providerPattern.MatchString(name) {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	var in ActivateInput
	if err := httpx.Decode(w, r, &in, 4<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	st, err := h.Service.SetMode(r.Context(), identity.PrincipalFrom(r.Context()).UserID, name, in)
	respond(w, r, http.StatusOK, st, err)
}

func (h *Handler) webhook(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("provider")
	if !providerPattern.MatchString(name) {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		httpx.Fail(w, r, httpx.NewError(http.StatusRequestEntityTooLarge, "body_too_large", "Request body is too large."))
		return
	}
	dup, err := h.Service.ReceiveWebhook(r.Context(), name, r.Header, body)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// A repeat is acknowledged like the first, so the provider stops retrying.
	httpx.JSON(w, r, http.StatusOK, map[string]any{"received": true, "duplicate": dup})
}

func respond[T any](w http.ResponseWriter, r *http.Request, status int, v T, err error) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, status, v)
}

func (h *Handler) adminList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := PaymentsFilter{Status: q.Get("status"), AttentionOnly: q.Get("attention") == "true"}
	if c := q.Get("customer_id"); uuidPattern.MatchString(c) {
		f.CustomerID = c
	}
	list, err := h.Service.List(r.Context(), f)
	respond(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

func (h *Handler) adminGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !uuidPattern.MatchString(id) {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	d, err := h.Service.Detail(r.Context(), id)
	respond(w, r, http.StatusOK, d, err)
}

func (h *Handler) adminRefund(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !uuidPattern.MatchString(id) {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	var in RefundInput
	if err := httpx.Decode(w, r, &in, 4<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	d, err := h.Service.Refund(r.Context(), identity.PrincipalFrom(r.Context()).UserID, id, in)
	respond(w, r, http.StatusCreated, d, err)
}

func (h *Handler) adminWebhooks(w http.ResponseWriter, r *http.Request) {
	list, err := h.Service.Webhooks(r.Context(), r.URL.Query().Get("status"))
	respond(w, r, http.StatusOK, map[string]any{"items": list}, err)
}
