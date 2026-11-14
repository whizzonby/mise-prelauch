package procurement

import (
	"net/http"
	"strings"

	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/httpx"
)

// purchasingRoutes registers RFQs, quotations and purchase orders. Receiving
// staff read orders (receiving.read) without prices; prices need
// purchase_orders.read.
func (h *Handler) purchasingRoutes(mux httpx.Router) {
	adm := h.Identity.RequireAdmin
	mux.Handle("GET /api/v1/admin/procurement/rfqs", adm(identity.PermProcurementRead, h.rfqs))
	mux.Handle("POST /api/v1/admin/procurement/rfqs", adm(identity.PermProcurementPlan, h.createRFQ))
	mux.Handle("GET /api/v1/admin/procurement/rfqs/{id}", adm(identity.PermProcurementRead, h.rfq))
	mux.Handle("POST /api/v1/admin/procurement/rfqs/{id}/invitations", adm(identity.PermProcurementPlan, h.invite))
	mux.Handle("POST /api/v1/admin/procurement/rfqs/{id}/close", adm(identity.PermProcurementPlan, h.closeRFQ))
	mux.Handle("POST /api/v1/admin/procurement/rfqs/{id}/cancel", adm(identity.PermProcurementPlan, h.cancelRFQ))
	mux.Handle("GET /api/v1/admin/procurement/quotations", adm(identity.PermProcurementRead, h.quotations))
	mux.Handle("POST /api/v1/admin/procurement/quotations", adm(identity.PermProcurementPlan, h.recordQuotation))
	mux.Handle("POST /api/v1/admin/procurement/runs/{id}/purchase-orders", adm(identity.PermPurchaseOrdersCreate, h.fromRun))

	mux.Handle("GET /api/v1/admin/purchase-orders", adm(identity.PermReceivingRead, h.pos))
	mux.Handle("POST /api/v1/admin/purchase-orders", adm(identity.PermPurchaseOrdersCreate, h.createPO))
	mux.Handle("GET /api/v1/admin/purchase-orders/{id}", adm(identity.PermReceivingRead, h.po))
	mux.Handle("PATCH /api/v1/admin/purchase-orders/{id}", adm(identity.PermPurchaseOrdersCreate, h.updatePO))
	mux.Handle("POST /api/v1/admin/purchase-orders/{id}/submit", adm(identity.PermPurchaseOrdersCreate, h.submitPO))
	mux.Handle("POST /api/v1/admin/purchase-orders/{id}/approve", adm(identity.PermAdminAccess, h.approvePO))
	mux.Handle("POST /api/v1/admin/purchase-orders/{id}/reject", adm(identity.PermPurchaseOrdersApprove, h.rejectPO))
	mux.Handle("POST /api/v1/admin/purchase-orders/{id}/issue", adm(identity.PermPurchaseOrdersIssue, h.issuePO))
	mux.Handle("POST /api/v1/admin/purchase-orders/{id}/cancel", adm(identity.PermPurchaseOrdersCreate, h.cancelPO))
	mux.Handle("POST /api/v1/admin/purchase-orders/{id}/close", adm(identity.PermPurchaseOrdersCreate, h.closePO))
}

func (h *Handler) rfqs(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.RFQs(r.Context(), r.URL.Query().Get("status"))
	respond(w, r, http.StatusOK, map[string]any{"items": x}, err)
}

func (h *Handler) createRFQ(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[RFQInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.CreateRFQ(r.Context(), actor(r), in)
	respond(w, r, http.StatusCreated, x, err)
}

func (h *Handler) rfq(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.RFQ(r.Context(), id(r))
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) invite(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[InviteInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.RecordInvitation(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) closeRFQ(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.EndRFQ(r.Context(), actor(r), id(r), "closed")
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) cancelRFQ(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.EndRFQ(r.Context(), actor(r), id(r), "cancelled")
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) quotations(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.Quotations(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": x}, err)
}

func (h *Handler) recordQuotation(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[QuotationInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.RecordQuotation(r.Context(), actor(r), in)
	respond(w, r, http.StatusCreated, x, err)
}

func (h *Handler) fromRun(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.FromRun(r.Context(), actor(r), id(r))
	respond(w, r, http.StatusCreated, map[string]any{"items": x}, err)
}

func (h *Handler) pos(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	x, err := h.S.PurchaseOrders(r.Context(), principal(r), POFilter{Status: q.Get("status"), SupplierID: q.Get("supplier_id"), RunID: q.Get("run_id"),
		Query: q.Get("q"), Receivable: q.Get("receivable") == "true"})
	respond(w, r, http.StatusOK, map[string]any{"items": x}, err)
}

func (h *Handler) createPO(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[POInput](w, r)
	if !ok {
		return
	}
	in.SupplierID = strings.ToLower(in.SupplierID)
	x, err := h.S.CreatePO(r.Context(), actor(r), in)
	respond(w, r, http.StatusCreated, x, err)
}

func (h *Handler) po(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.PurchaseOrder(r.Context(), principal(r), id(r))
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) updatePO(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[POInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.UpdatePO(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) submitPO(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.SubmitPO(r.Context(), actor(r), id(r))
	respond(w, r, http.StatusOK, x, err)
}

// approvePO checks permissions in the service: the first approval needs
// purchase_orders.approve, the above-limit approval approve_above_limit.
func (h *Handler) approvePO(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if !p.Can(identity.PermPurchaseOrdersApprove) && !p.Can(identity.PermPurchaseOrdersApproveLimit) {
		httpx.Fail(w, r, forbidden("forbidden", "Approving purchase orders needs purchase_orders.approve."))
		return
	}
	x, err := h.S.ApprovePO(r.Context(), p, id(r))
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) rejectPO(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[reasonInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.RejectPO(r.Context(), actor(r), id(r), in.Reason)
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) issuePO(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[IssueInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.IssuePO(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) cancelPO(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[reasonInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.CancelPO(r.Context(), actor(r), id(r), in.Reason)
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) closePO(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[reasonInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.ClosePO(r.Context(), actor(r), id(r), in.Reason)
	respond(w, r, http.StatusOK, x, err)
}
