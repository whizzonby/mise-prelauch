package delivery

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strings"

	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/httpx"
)

// routes registers partners, couriers, deliveries, dispatch, tracking,
// proof, returns, reconciliation, costs, the overview and the report.
func (h *Handler) routes(mux httpx.Router) {
	adm := h.Identity.RequireAdmin
	any := identity.PermAdminAccess // courier-reachable: scope checked in the handler

	mux.Handle("GET /api/v1/admin/delivery/overview", adm(identity.PermDeliveryReportsRead, h.overview))
	mux.Handle("GET /api/v1/admin/reports/delivery", adm(identity.PermDeliveryReportsRead, h.report))

	mux.Handle("GET /api/v1/admin/delivery/partners", adm(identity.PermDeliveryPartnersRead, h.partners))
	mux.Handle("POST /api/v1/admin/delivery/partners", adm(identity.PermDeliveryPartnersManage, h.savePartner))
	mux.Handle("GET /api/v1/admin/delivery/partners/{id}", adm(identity.PermDeliveryPartnersRead, h.partner))
	mux.Handle("PATCH /api/v1/admin/delivery/partners/{id}", adm(identity.PermDeliveryPartnersManage, h.savePartner))
	mux.Handle("PUT /api/v1/admin/delivery/partners/{id}/criteria", adm(identity.PermDeliveryPartnersManage, h.criterion))
	mux.Handle("POST /api/v1/admin/delivery/partners/{id}/status", adm(identity.PermDeliveryPartnersApprove, h.partnerStatus))
	mux.Handle("GET /api/v1/admin/delivery/couriers", adm(identity.PermDeliveryPartnersRead, h.couriers))
	mux.Handle("POST /api/v1/admin/delivery/couriers", adm(identity.PermDeliveryPartnersManage, h.saveCourier))
	mux.Handle("PATCH /api/v1/admin/delivery/couriers/{id}", adm(identity.PermDeliveryPartnersManage, h.saveCourier))
	mux.Handle("GET /api/v1/admin/delivery/costs", adm(identity.PermDeliveryPartnersRead, h.costs))
	mux.Handle("POST /api/v1/admin/delivery/costs", adm(identity.PermDeliveryPartnersManage, h.recordCost))

	mux.Handle("GET /api/v1/admin/delivery/deliveries", adm(any, h.deliveries))
	mux.Handle("GET /api/v1/admin/delivery/deliveries/{id}", adm(any, h.delivery))
	mux.Handle("POST /api/v1/admin/delivery/deliveries/{id}/schedule", adm(identity.PermDeliveryPlan, h.schedule))
	mux.Handle("PUT /api/v1/admin/delivery/deliveries/{id}/address", adm(identity.PermDeliveryPlan, h.address))
	mux.Handle("POST /api/v1/admin/delivery/deliveries/{id}/events", adm(identity.PermDeliveryUpdateStatus, h.status))
	mux.Handle("POST /api/v1/admin/delivery/deliveries/{id}/proof", adm(identity.PermDeliveryProofCreate, h.proof))
	mux.Handle("POST /api/v1/admin/delivery/deliveries/{id}/deliver", adm(identity.PermDeliveryConfirm, h.deliver))
	mux.Handle("POST /api/v1/admin/delivery/deliveries/{id}/fail", adm(identity.PermDeliveryConfirm, h.fail))
	mux.Handle("POST /api/v1/admin/delivery/deliveries/{id}/reattempt", adm(identity.PermDeliveryReschedule, h.reattempt))
	mux.Handle("POST /api/v1/admin/delivery/deliveries/{id}/return", adm(identity.PermDeliveryExceptionsManage, h.recordReturn))
	mux.Handle("POST /api/v1/admin/delivery/deliveries/{id}/cancel", adm(identity.PermDeliveryDispatch, h.cancel))
	mux.Handle("POST /api/v1/admin/delivery/deliveries/{id}/correct", adm(identity.PermDeliverySettingsManage, h.correct))
	mux.Handle("POST /api/v1/admin/delivery/deliveries/{id}/reschedule-requests", adm(identity.PermDeliveryReschedule, h.requestReschedule))
	mux.Handle("POST /api/v1/admin/delivery/deliveries/{id}/cases", adm(identity.PermDeliveryExceptionsManage, h.linkCase))
	mux.Handle("GET /api/v1/admin/delivery/reschedule-requests", adm(identity.PermDeliveryRead, h.pendingReschedules))
	mux.Handle("POST /api/v1/admin/delivery/reschedule-requests/{id}/decide", adm(identity.PermDeliveryReschedule, h.decide))

	mux.Handle("GET /api/v1/admin/delivery/returns", adm(identity.PermDeliveryExceptionsRead, h.returnsList))
	mux.Handle("POST /api/v1/admin/delivery/returns/{id}/review", adm(identity.PermDeliveryExceptionsManage, h.reviewReturn))

	mux.Handle("GET /api/v1/admin/delivery/dispatch-runs", adm(any, h.runs))
	mux.Handle("POST /api/v1/admin/delivery/dispatch-runs", adm(identity.PermDeliveryAssign, h.createRun))
	mux.Handle("GET /api/v1/admin/delivery/dispatch-runs/{id}", adm(any, h.run))
	mux.Handle("GET /api/v1/admin/delivery/dispatch-runs/{id}/manifest", adm(any, h.manifest))
	mux.Handle("POST /api/v1/admin/delivery/dispatch-runs/{id}/stops", adm(identity.PermDeliveryAssign, h.addStop))
	mux.Handle("POST /api/v1/admin/delivery/dispatch-runs/{id}/stops/{stop}/remove", adm(identity.PermDeliveryAssign, h.removeStop))
	mux.Handle("PUT /api/v1/admin/delivery/dispatch-runs/{id}/sequence", adm(identity.PermDeliveryAssign, h.sequence))
	mux.Handle("POST /api/v1/admin/delivery/dispatch-runs/{id}/finalize", adm(identity.PermDeliveryDispatch, h.finalize))
	mux.Handle("POST /api/v1/admin/delivery/dispatch-runs/{id}/reopen", adm(identity.PermDeliveryDispatch, h.reopen))
	mux.Handle("POST /api/v1/admin/delivery/dispatch-runs/{id}/release", adm(identity.PermDeliveryDispatch, h.releaseRun))
	mux.Handle("POST /api/v1/admin/delivery/dispatch-runs/{id}/handoffs", adm(identity.PermDeliveryDispatch, h.handoff))
	mux.Handle("GET /api/v1/admin/delivery/dispatch-runs/{id}/reconciliation", adm(identity.PermDeliveryRead, h.reconciliation))
	mux.Handle("POST /api/v1/admin/delivery/dispatch-runs/{id}/reconcile", adm(identity.PermDeliveryReconcile, h.reconcileRun))
}

// scope resolves what the caller may see; a staff member with neither
// delivery permission is refused.
func (h *Handler) scope(w http.ResponseWriter, r *http.Request) (Scope, bool) {
	sc, ok := h.S.ScopeFor(r.Context(), principal(r))
	if !ok {
		httpx.Fail(w, r, httpx.ErrForbidden)
	}
	return sc, ok
}

type reasonBody struct {
	Reason string `json:"reason"`
}

func (h *Handler) overview(w http.ResponseWriter, r *http.Request) {
	o, err := h.S.OverviewFor(r.Context(), r.URL.Query().Get("date"))
	respond(w, r, http.StatusOK, o, err)
}

func (h *Handler) report(w http.ResponseWriter, r *http.Request) {
	rep, err := h.S.Report(r.Context(), r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	respond(w, r, http.StatusOK, rep, err)
}

func (h *Handler) partners(w http.ResponseWriter, r *http.Request) {
	p, err := h.S.Partners(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": p}, err)
}

func (h *Handler) partner(w http.ResponseWriter, r *http.Request) {
	p, err := h.S.Partner(r.Context(), id(r))
	respond(w, r, http.StatusOK, p, err)
}

func (h *Handler) savePartner(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[PartnerInput](w, r)
	if !ok {
		return
	}
	status := http.StatusOK
	if r.Method == http.MethodPost {
		status = http.StatusCreated
	}
	p, err := h.S.SavePartner(r.Context(), actor(r), id(r), in)
	respond(w, r, status, p, err)
}

func (h *Handler) criterion(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[CriterionInput](w, r)
	if !ok {
		return
	}
	p, err := h.S.RecordCriterion(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusOK, p, err)
}

func (h *Handler) partnerStatus(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[PartnerStatusInput](w, r)
	if !ok {
		return
	}
	p, err := h.S.SetPartnerStatus(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusOK, p, err)
}

func (h *Handler) couriers(w http.ResponseWriter, r *http.Request) {
	c, err := h.S.Couriers(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": c}, err)
}

func (h *Handler) saveCourier(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[CourierInput](w, r)
	if !ok {
		return
	}
	status := http.StatusOK
	if r.Method == http.MethodPost {
		status = http.StatusCreated
	}
	c, err := h.S.SaveCourier(r.Context(), actor(r), id(r), in)
	respond(w, r, status, c, err)
}

func (h *Handler) costs(w http.ResponseWriter, r *http.Request) {
	c, err := h.S.Costs(r.Context(), r.URL.Query().Get("run_id"))
	respond(w, r, http.StatusOK, map[string]any{"items": c}, err)
}

func (h *Handler) recordCost(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[CostInput](w, r)
	if !ok {
		return
	}
	c, err := h.S.RecordCost(r.Context(), actor(r), in)
	respond(w, r, http.StatusCreated, c, err)
}

func (h *Handler) deliveries(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	d, err := h.S.Deliveries(r.Context(), sc, DeliveryFilter{Status: q.Get("status"), Date: q.Get("date"), ZoneID: q.Get("zone_id"),
		CustomerID: q.Get("customer_id"), OrderID: q.Get("order_id"), Query: q.Get("q")})
	respond(w, r, http.StatusOK, map[string]any{"items": d}, err)
}

func (h *Handler) delivery(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	d, err := h.S.Delivery(r.Context(), sc, id(r))
	if err == nil && !principal(r).Can(identity.PermDeliveryProofRead) {
		d.Proofs = nil
	}
	respond(w, r, http.StatusOK, d, err)
}

func (h *Handler) schedule(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[ScheduleInput](w, r)
	if !ok {
		return
	}
	d, err := h.S.Schedule(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusOK, d, err)
}

func (h *Handler) address(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[AddressInput](w, r)
	if !ok {
		return
	}
	d, err := h.S.CorrectAddress(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusOK, d, err)
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	in, ok := decode[StatusInput](w, r)
	if !ok {
		return
	}
	d, err := h.S.UpdateStatus(r.Context(), sc, actor(r), id(r), in)
	respond(w, r, http.StatusOK, d, err)
}

func (h *Handler) proof(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	in, ok := decode[ProofInput](w, r)
	if !ok {
		return
	}
	d, err := h.S.RecordProof(r.Context(), sc, actor(r), id(r), in)
	respond(w, r, http.StatusOK, d, err)
}

func (h *Handler) deliver(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	in, ok := decode[OutcomeInput](w, r)
	if !ok {
		return
	}
	d, err := h.S.Deliver(r.Context(), sc, actor(r), id(r), in)
	respond(w, r, http.StatusOK, d, err)
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	in, ok := decode[OutcomeInput](w, r)
	if !ok {
		return
	}
	d, err := h.S.Fail(r.Context(), sc, actor(r), id(r), in)
	respond(w, r, http.StatusOK, d, err)
}

func (h *Handler) reattempt(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[reasonBody](w, r)
	if !ok {
		return
	}
	d, err := h.S.Reattempt(r.Context(), actor(r), id(r), in.Reason)
	respond(w, r, http.StatusOK, d, err)
}

func (h *Handler) recordReturn(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[ReturnInput](w, r)
	if !ok {
		return
	}
	d, err := h.S.RecordReturn(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusOK, d, err)
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[reasonBody](w, r)
	if !ok {
		return
	}
	d, err := h.S.Cancel(r.Context(), actor(r), id(r), in.Reason)
	respond(w, r, http.StatusOK, d, err)
}

func (h *Handler) correct(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[struct {
		To     string `json:"to"`
		Reason string `json:"reason"`
	}](w, r)
	if !ok {
		return
	}
	d, err := h.S.Correct(r.Context(), actor(r), id(r), in.To, in.Reason)
	respond(w, r, http.StatusOK, d, err)
}

func (h *Handler) requestReschedule(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[RescheduleInput](w, r)
	if !ok {
		return
	}
	d, err := h.S.RequestReschedule(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusCreated, d, err)
}

func (h *Handler) linkCase(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[struct {
		CaseID string `json:"case_id"`
	}](w, r)
	if !ok {
		return
	}
	d, err := h.S.LinkCase(r.Context(), actor(r), id(r), strings.ToLower(in.CaseID))
	respond(w, r, http.StatusOK, d, err)
}

func (h *Handler) pendingReschedules(w http.ResponseWriter, r *http.Request) {
	p, err := h.S.PendingReschedules(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": p}, err)
}

func (h *Handler) decide(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[DecisionInput](w, r)
	if !ok {
		return
	}
	d, err := h.S.DecideReschedule(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusOK, d, err)
}

func (h *Handler) returnsList(w http.ResponseWriter, r *http.Request) {
	rs, err := h.S.Returns(r.Context(), r.URL.Query().Get("status"))
	respond(w, r, http.StatusOK, map[string]any{"items": rs}, err)
}

func (h *Handler) reviewReturn(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[ReviewInput](w, r)
	if !ok {
		return
	}
	rs, err := h.S.ReviewReturn(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusOK, map[string]any{"items": rs}, err)
}

func (h *Handler) runs(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	rs, err := h.S.Runs(r.Context(), sc, r.URL.Query().Get("date"), r.URL.Query().Get("status"))
	respond(w, r, http.StatusOK, map[string]any{"items": rs}, err)
}

func (h *Handler) run(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	run, err := h.S.Run(r.Context(), sc, id(r))
	respond(w, r, http.StatusOK, run, err)
}

func (h *Handler) createRun(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[struct {
		RunInput
		PartnerOverride string `json:"partner_override"`
	}](w, r)
	if !ok {
		return
	}
	run, err := h.S.CreateRun(r.Context(), actor(r), in.RunInput, in.PartnerOverride)
	respond(w, r, http.StatusCreated, run, err)
}

// manifest serves the current manifest as JSON or CSV. It is never a
// static file: every download goes through the permission check, is
// audited, and is marked not to be cached.
func (h *Handler) manifest(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.scope(w, r)
	if !ok {
		return
	}
	run, err := h.S.Run(r.Context(), sc, id(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if run.Manifest == nil {
		httpx.Fail(w, r, conflict("no_manifest", "The run has no manifest yet: finalize it first."))
		return
	}
	_ = record(r.Context(), h.S.pool, actor(r), "delivery.manifest_viewed", "dispatch_run", run.ID,
		map[string]any{"revision": run.Manifest.Revision, "format": r.URL.Query().Get("format")})
	w.Header().Set("Cache-Control", "no-store, private")
	if r.URL.Query().Get("format") != "csv" {
		httpx.JSON(w, r, http.StatusOK, run.Manifest)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="manifest-%s-r%d.csv"`, run.Number, run.Manifest.Revision))
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"stop", "delivery", "order", "recipient", "phone", "address", "region", "window", "instructions", "packages", "package_ids"})
	for _, s := range run.Manifest.Content.Stops {
		codes := []string{}
		for _, p := range s.Packages {
			codes = append(codes, p.Code)
		}
		_ = cw.Write([]string{fmt.Sprint(s.Sequence), s.Delivery, s.Order, safeCSV(s.Recipient), safeCSV(s.Phone), safeCSV(s.Address), s.Region,
			s.Window, safeCSV(s.Instructions), fmt.Sprint(len(s.Packages)), strings.Join(codes, " ")})
	}
	cw.Flush()
}

// safeCSV stops spreadsheet formula injection from customer-entered text.
func safeCSV(v string) string {
	if v != "" && strings.ContainsAny(v[:1], "=+-@\t\r") {
		return "'" + v
	}
	return v
}

func (h *Handler) addStop(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[StopInput](w, r)
	if !ok {
		return
	}
	run, err := h.S.AddStop(r.Context(), principal(r), id(r), in)
	respond(w, r, http.StatusOK, run, err)
}

func (h *Handler) removeStop(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[reasonBody](w, r)
	if !ok {
		return
	}
	run, err := h.S.RemoveStop(r.Context(), actor(r), id(r), strings.ToLower(r.PathValue("stop")), in.Reason)
	respond(w, r, http.StatusOK, run, err)
}

func (h *Handler) sequence(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[struct {
		StopIDs []string `json:"stop_ids"`
	}](w, r)
	if !ok {
		return
	}
	run, err := h.S.Sequence(r.Context(), actor(r), id(r), in.StopIDs)
	respond(w, r, http.StatusOK, run, err)
}

func (h *Handler) finalize(w http.ResponseWriter, r *http.Request) {
	run, err := h.S.Finalize(r.Context(), actor(r), id(r))
	respond(w, r, http.StatusOK, run, err)
}

func (h *Handler) reopen(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[reasonBody](w, r)
	if !ok {
		return
	}
	run, err := h.S.Reopen(r.Context(), actor(r), id(r), in.Reason)
	respond(w, r, http.StatusOK, run, err)
}

func (h *Handler) releaseRun(w http.ResponseWriter, r *http.Request) {
	run, err := h.S.Release(r.Context(), actor(r), id(r))
	respond(w, r, http.StatusOK, run, err)
}

func (h *Handler) handoff(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[HandoffInput](w, r)
	if !ok {
		return
	}
	run, err := h.S.RecordHandoff(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusOK, run, err)
}

func (h *Handler) reconciliation(w http.ResponseWriter, r *http.Request) {
	rec, err := h.S.Reconciliation(r.Context(), id(r))
	respond(w, r, http.StatusOK, rec, err)
}

func (h *Handler) reconcileRun(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[struct {
		Explanation string `json:"explanation"`
	}](w, r)
	if !ok {
		return
	}
	rec, err := h.S.CloseRun(r.Context(), actor(r), id(r), in.Explanation)
	respond(w, r, http.StatusOK, rec, err)
}
