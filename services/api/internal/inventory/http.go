package inventory

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

// Routes registers the inventory API. Every route needs an admin session and
// the named permission.
func (h *Handler) Routes(mux httpx.Router) {
	adm := h.Identity.RequireAdmin
	mux.Handle("GET /api/v1/admin/inventory", adm(identity.PermInventoryRead, h.stock))
	mux.Handle("GET /api/v1/admin/inventory/lots", adm(identity.PermInventoryRead, h.lots))
	mux.Handle("GET /api/v1/admin/inventory/lots/{id}", adm(identity.PermInventoryRead, h.lot))
	mux.Handle("POST /api/v1/admin/inventory/lots/{id}/status", adm(identity.PermReceivingInspect, h.setStatus))
	mux.Handle("POST /api/v1/admin/inventory/lots/{id}/expiry", adm(identity.PermReceivingInspect, h.setExpiry))
	mux.Handle("POST /api/v1/admin/inventory/lots/{id}/transfer", adm(identity.PermInventoryAdjust, h.transfer))
	mux.Handle("POST /api/v1/admin/inventory/lots/{id}/waste", adm(identity.PermInventoryAdjust, h.storeWaste))
	mux.Handle("POST /api/v1/admin/inventory/lots/{id}/supplier-return", adm(identity.PermInventoryAdjust, h.supplierReturn))
	mux.Handle("POST /api/v1/admin/inventory/adjustments", adm(identity.PermInventoryAdjust, h.adjust))
	mux.Handle("GET /api/v1/admin/inventory/movements", adm(identity.PermInventoryRead, h.movements))
	mux.Handle("GET /api/v1/admin/inventory/reservations", adm(identity.PermInventoryRead, h.reservations))
	mux.Handle("POST /api/v1/admin/inventory/reservations", adm(identity.PermInventoryReserve, h.reserve))
	mux.Handle("POST /api/v1/admin/inventory/reservations/{id}/release", adm(identity.PermInventoryReserve, h.release))
	mux.Handle("POST /api/v1/admin/inventory/issues", adm(identity.PermInventoryIssue, h.issue))
	mux.Handle("POST /api/v1/admin/inventory/returns", adm(identity.PermInventoryIssue, h.returnStock))
	mux.Handle("GET /api/v1/admin/inventory/batches/{id}/reconciliation", adm(identity.PermInventoryRead, h.reconcile))
	mux.Handle("GET /api/v1/admin/inventory/counts", adm(identity.PermInventoryRead, h.counts))
	mux.Handle("POST /api/v1/admin/inventory/counts", adm(identity.PermInventoryCount, h.startCount))
	mux.Handle("GET /api/v1/admin/inventory/counts/{id}", adm(identity.PermInventoryRead, h.count))
	mux.Handle("PUT /api/v1/admin/inventory/counts/{id}/lines", adm(identity.PermInventoryCount, h.countLines))
	mux.Handle("POST /api/v1/admin/inventory/counts/{id}/submit", adm(identity.PermInventoryCount, h.submitCount))
	mux.Handle("POST /api/v1/admin/inventory/counts/{id}/post", adm(identity.PermInventoryAdjust, h.postCount))
	mux.Handle("POST /api/v1/admin/inventory/counts/{id}/cancel", adm(identity.PermInventoryCount, h.cancelCount))
	mux.Handle("GET /api/v1/admin/inventory/locations", adm(identity.PermAdminAccess, h.locations))
	mux.Handle("POST /api/v1/admin/inventory/locations", adm(identity.PermInventoryConfigure, h.createLocation))
	mux.Handle("PATCH /api/v1/admin/inventory/locations/{id}", adm(identity.PermInventoryConfigure, h.updateLocation))
	mux.Handle("GET /api/v1/admin/inventory/policies", adm(identity.PermInventoryRead, h.policies))
	mux.Handle("PUT /api/v1/admin/inventory/policies", adm(identity.PermInventoryConfigure, h.savePolicy))

	mux.Handle("GET /api/v1/admin/receiving", adm(identity.PermReceivingRead, h.receipts))
	mux.Handle("POST /api/v1/admin/receiving", adm(identity.PermReceivingManage, h.receive))
	mux.Handle("GET /api/v1/admin/receiving/{id}", adm(identity.PermReceivingRead, h.receipt))
	mux.Handle("GET /api/v1/admin/receiving/checklist", adm(identity.PermReceivingRead, h.checklist))
	mux.Handle("POST /api/v1/admin/receiving/checklist", adm(identity.PermInventoryConfigure, h.createChecklistItem))
	mux.Handle("PATCH /api/v1/admin/receiving/checklist/{id}", adm(identity.PermInventoryConfigure, h.updateChecklistItem))

	mux.Handle("GET /api/v1/admin/traceability", adm(identity.PermTraceabilityRead, h.trace))
}

func respond(w http.ResponseWriter, r *http.Request, status int, v any, err error) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, status, v)
}

func principal(r *http.Request) *identity.Principal { return identity.PrincipalFrom(r.Context()) }
func actor(r *http.Request) string                  { return principal(r).UserID }

func decode[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var v T
	if r.ContentLength == 0 && r.Header.Get("Content-Type") == "" {
		return v, true
	}
	if err := httpx.Decode(w, r, &v, 512<<10); err != nil {
		httpx.Fail(w, r, err)
		return v, false
	}
	return v, true
}

func id(r *http.Request) string { return strings.ToLower(r.PathValue("id")) }

type reasonInput struct {
	Reason string `json:"reason"`
	Notes  string `json:"notes"`
}

func (h *Handler) stock(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.Stock(r.Context(), r.URL.Query().Get("q"))
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) lots(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	x, err := h.S.Lots(r.Context(), LotFilter{IngredientID: q.Get("ingredient_id"), Status: q.Get("status"), LocationID: q.Get("location_id"),
		SupplierID: q.Get("supplier_id"), Query: q.Get("q"), Held: q.Get("held") == "true"})
	respond(w, r, http.StatusOK, map[string]any{"items": x}, err)
}

func (h *Handler) lot(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.Lot(r.Context(), id(r))
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) setStatus(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[StatusInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.SetStatus(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) setExpiry(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[ExpiryInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.SetExpiry(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) transfer(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[TransferInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.Transfer(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) storeWaste(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[QuantityInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.StoreWaste(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) supplierReturn(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[QuantityInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.SupplierReturn(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) adjust(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[AdjustmentInput](w, r)
	if !ok {
		return
	}
	in.LotID = strings.ToLower(in.LotID)
	x, err := h.S.Adjust(r.Context(), actor(r), in)
	respond(w, r, http.StatusCreated, x, err)
}

func (h *Handler) movements(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	x, err := h.S.Movements(r.Context(), MovementFilter{LotID: q.Get("lot_id"), IngredientID: q.Get("ingredient_id"), Type: q.Get("type"),
		BatchID: q.Get("batch_id"), From: q.Get("from"), To: q.Get("to")})
	respond(w, r, http.StatusOK, map[string]any{"items": x}, err)
}

func (h *Handler) reservations(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	x, err := h.S.Reservations(r.Context(), ReservationFilter{PlanID: q.Get("plan_id"), IngredientID: q.Get("ingredient_id"), LotID: q.Get("lot_id"), Status: q.Get("status")})
	respond(w, r, http.StatusOK, map[string]any{"items": x}, err)
}

func (h *Handler) reserve(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[ReserveInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.Reserve(r.Context(), actor(r), in)
	respond(w, r, http.StatusCreated, x, err)
}

func (h *Handler) release(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[reasonInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.Release(r.Context(), actor(r), id(r), in.Reason)
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) issue(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[IssueInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.Issue(r.Context(), actor(r), in)
	respond(w, r, http.StatusCreated, x, err)
}

func (h *Handler) returnStock(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[ReturnInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.Return(r.Context(), actor(r), in)
	respond(w, r, http.StatusCreated, x, err)
}

func (h *Handler) reconcile(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.Reconcile(r.Context(), id(r))
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) counts(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.Counts(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": x}, err)
}

func (h *Handler) startCount(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[struct {
		LocationID string `json:"location_id"`
		Notes      string `json:"notes"`
	}](w, r)
	if !ok {
		return
	}
	x, err := h.S.StartCount(r.Context(), actor(r), strings.ToLower(in.LocationID), in.Notes)
	respond(w, r, http.StatusCreated, x, err)
}

func (h *Handler) count(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.Count(r.Context(), id(r))
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) countLines(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[struct {
		Lines []CountLineInput `json:"lines"`
	}](w, r)
	if !ok {
		return
	}
	x, err := h.S.RecordCounts(r.Context(), actor(r), id(r), in.Lines)
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) submitCount(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.SubmitCount(r.Context(), actor(r), id(r))
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) postCount(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.PostCount(r.Context(), actor(r), id(r))
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) cancelCount(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.CancelCount(r.Context(), actor(r), id(r))
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) locations(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.Locations(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": x}, err)
}

func (h *Handler) createLocation(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[LocationInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.SaveLocation(r.Context(), actor(r), "", in)
	respond(w, r, http.StatusCreated, map[string]any{"items": x}, err)
}

func (h *Handler) updateLocation(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[LocationInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.SaveLocation(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusOK, map[string]any{"items": x}, err)
}

func (h *Handler) policies(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.Policies(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": x}, err)
}

func (h *Handler) savePolicy(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[PolicyInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.SavePolicy(r.Context(), actor(r), in)
	respond(w, r, http.StatusOK, map[string]any{"items": x}, err)
}

func (h *Handler) receipts(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.Receipts(r.Context(), r.URL.Query().Get("po_id"))
	respond(w, r, http.StatusOK, map[string]any{"items": x}, err)
}

// receive records a delivery; inspecting it (accepting, quarantining,
// rejecting) needs receiving.inspect too.
func (h *Handler) receive(w http.ResponseWriter, r *http.Request) {
	if !principal(r).Can(identity.PermReceivingInspect) {
		httpx.Fail(w, r, forbidden("forbidden", "Recording a delivery includes its inspection, which needs receiving.inspect."))
		return
	}
	in, ok := decode[ReceiptInput](w, r)
	if !ok {
		return
	}
	in.POID = strings.ToLower(in.POID)
	x, replayed, err := h.S.Receive(r.Context(), actor(r), in)
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	respond(w, r, status, x, err)
}

func (h *Handler) receipt(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.Receipt(r.Context(), id(r))
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) checklist(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.Checklist(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": x}, err)
}

func (h *Handler) createChecklistItem(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[ChecklistInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.SaveChecklistItem(r.Context(), actor(r), "", in)
	respond(w, r, http.StatusCreated, map[string]any{"items": x}, err)
}

func (h *Handler) updateChecklistItem(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[ChecklistInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.SaveChecklistItem(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusOK, map[string]any{"items": x}, err)
}

func (h *Handler) trace(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	x, err := h.S.Trace(r.Context(), TraceFilter{LotID: q.Get("lot"), BatchID: q.Get("batch"), SupplierID: q.Get("supplier"), POID: q.Get("purchase_order")})
	respond(w, r, http.StatusOK, x, err)
}
