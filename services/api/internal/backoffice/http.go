package backoffice

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"mise.tt/api/internal/crm"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/reporting"
	"mise.tt/api/internal/support"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type Handler struct{ S *Service }

// Routes registers the Sprint 4 administration API. Every route needs an
// admin session that has finished MFA (where required) and the named
// permission; the permission is checked on the server for every request.
func (h *Handler) Routes(mux httpx.Router) {
	adm := h.S.Identity.RequireAdmin
	P := func(s string) identity.Permission { return identity.Permission(s) }
	// Overview and reports.
	mux.Handle("GET /api/v1/admin/dashboard", adm(P("reports.read"), h.dashboard))
	mux.Handle("GET /api/v1/admin/reports/{kind}", adm(P("reports.read"), h.report))
	// Customers.
	mux.Handle("GET /api/v1/admin/customers", adm(P("customers.read"), h.customers))
	mux.Handle("GET /api/v1/admin/customers/{id}", adm(P("customers.read"), h.customer))
	mux.Handle("GET /api/v1/admin/customers/{id}/orders", adm(P("customers.read"), h.customerOrders))
	mux.Handle("GET /api/v1/admin/customers/{id}/billing", adm(P("customers.read"), h.customerBilling))
	mux.Handle("GET /api/v1/admin/customers/{id}/preferences", adm(P("customers.sensitive"), h.customerPreferences))
	mux.Handle("GET /api/v1/admin/customers/{id}/activity", adm(P("customers.read"), h.customerActivity))
	mux.Handle("GET /api/v1/admin/customers/{id}/notes", adm(P("customers.read"), h.notes("customer")))
	mux.Handle("POST /api/v1/admin/customers/{id}/notes", adm(P("leads.manage"), h.addNote("customer")))
	// CRM.
	mux.Handle("GET /api/v1/admin/leads/{id}/crm", adm(P("leads.read"), h.leadCRM))
	mux.Handle("PUT /api/v1/admin/leads/{id}/crm", adm(P("leads.manage"), h.setLeadCRM))
	mux.Handle("GET /api/v1/admin/leads/{id}/notes", adm(P("leads.read"), h.notes("lead")))
	mux.Handle("POST /api/v1/admin/leads/{id}/notes", adm(P("leads.manage"), h.addNote("lead")))
	mux.Handle("GET /api/v1/admin/segments", adm(P("campaigns.manage"), h.segments))
	mux.Handle("POST /api/v1/admin/segments", adm(P("campaigns.manage"), h.saveSegment))
	mux.Handle("POST /api/v1/admin/segments/preview", adm(P("campaigns.manage"), h.previewRules))
	mux.Handle("GET /api/v1/admin/segments/{id}", adm(P("campaigns.manage"), h.segment))
	mux.Handle("PUT /api/v1/admin/segments/{id}", adm(P("campaigns.manage"), h.saveSegment))
	mux.Handle("DELETE /api/v1/admin/segments/{id}", adm(P("campaigns.manage"), h.deleteSegment))
	mux.Handle("GET /api/v1/admin/campaigns", adm(P("campaigns.manage"), h.campaigns))
	mux.Handle("POST /api/v1/admin/campaigns", adm(P("campaigns.manage"), h.saveCampaign))
	mux.Handle("GET /api/v1/admin/campaigns/{id}", adm(P("campaigns.manage"), h.campaign))
	mux.Handle("PUT /api/v1/admin/campaigns/{id}", adm(P("campaigns.manage"), h.saveCampaign))
	mux.Handle("POST /api/v1/admin/campaigns/{id}/status", adm(P("campaigns.manage"), h.campaignStatus))
	mux.Handle("GET /api/v1/admin/referrals", adm(P("leads.read"), h.referrals))
	mux.Handle("GET /api/v1/admin/retention", adm(P("customers.read"), h.retention))
	// Support.
	mux.Handle("GET /api/v1/admin/support/cases", adm(P("cases.read"), h.cases))
	mux.Handle("POST /api/v1/admin/support/cases", adm(P("cases.manage"), h.createCase))
	mux.Handle("GET /api/v1/admin/support/cases/{id}", adm(P("cases.read"), h.getCase))
	mux.Handle("PATCH /api/v1/admin/support/cases/{id}", adm(P("cases.manage"), h.updateCase))
	mux.Handle("POST /api/v1/admin/support/cases/{id}/notes", adm(P("cases.manage"), h.caseNote))
	// Commerce administration.
	mux.Handle("POST /api/v1/admin/subscriptions/{id}/pause", adm(P("subscriptions.manage"), h.subAction("pause")))
	mux.Handle("POST /api/v1/admin/subscriptions/{id}/resume", adm(P("subscriptions.manage"), h.subAction("resume")))
	mux.Handle("POST /api/v1/admin/subscriptions/{id}/cancel", adm(P("subscriptions.manage"), h.subAction("cancel")))
	mux.Handle("GET /api/v1/admin/invoices", adm(P("payments.read"), h.invoices))
	// Exports, alerts, settings, integrations, audit.
	mux.Handle("GET /api/v1/admin/exports", adm(P("admin.access"), h.exports))
	mux.Handle("POST /api/v1/admin/exports", adm(P("admin.access"), h.requestExport))
	mux.Handle("GET /api/v1/admin/exports/{id}/download", adm(P("admin.access"), h.downloadExport))
	mux.Handle("GET /api/v1/admin/alerts", adm(P("admin.access"), h.alerts))
	mux.Handle("POST /api/v1/admin/alerts/read", adm(P("admin.access"), h.readAlerts))
	mux.Handle("GET /api/v1/admin/settings", adm(P("admin.access"), h.settings))
	mux.Handle("PUT /api/v1/admin/settings", adm(P("settings.manage"), h.putSettings))
	mux.Handle("GET /api/v1/admin/integrations", adm(P("settings.manage"), h.integrations))
	mux.Handle("GET /api/v1/admin/audit-events", adm(P("audit.read"), h.audit))
}

func respond(w http.ResponseWriter, r *http.Request, status int, v any, err error) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, status, v)
}

func principal(r *http.Request) *identity.Principal { return identity.PrincipalFrom(r.Context()) }

func pathID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := strings.ToLower(r.PathValue("id"))
	if !uuidPattern.MatchString(id) {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return "", false
	}
	return id, true
}

func decode[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var v T
	if err := httpx.Decode(w, r, &v, 32<<10); err != nil {
		httpx.Fail(w, r, err)
		return v, false
	}
	return v, true
}

func intParam(r *http.Request, key string) int {
	n, _ := strconv.Atoi(r.URL.Query().Get(key))
	return n
}

// ---------------------------------------------------------------- overview and reports

func (h *Handler) dashboard(w http.ResponseWriter, r *http.Request) {
	d, err := h.S.Reporting.Dashboard(r.Context())
	if err == nil {
		// Each attention item is shown only to staff who can act on it.
		p := principal(r)
		kept := d.Attention[:0]
		for _, a := range d.Attention {
			if p.Can(identity.Permission(a.Permission)) {
				kept = append(kept, a)
			}
		}
		d.Attention = kept
	}
	respond(w, r, http.StatusOK, d, err)
}

func (h *Handler) report(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rep, err := h.S.Reporting.Report(r.Context(), r.PathValue("kind"), q.Get("from"), q.Get("to"))
	respond(w, r, http.StatusOK, rep, err)
}

// ---------------------------------------------------------------- customers

func (h *Handler) customers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := reporting.CustomerFilter{Query: q.Get("q"), Status: q.Get("status"), SubscriptionStatus: q.Get("subscription_status"),
		Region: q.Get("region"), Sort: q.Get("sort"), Page: intParam(r, "page"), Size: intParam(r, "page_size")}
	if len(f.Query) > 100 {
		httpx.Fail(w, r, httpx.ValidationError([]httpx.FieldError{{Field: "q", Code: "length", Message: "Search text is too long."}}))
		return
	}
	if id := q.Get("plan_id"); uuidPattern.MatchString(id) {
		f.PlanID = id
	}
	if id := q.Get("segment_id"); id != "" {
		seg, err := h.S.CRM.Segment(r.Context(), id)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		f.Rules = &seg.Rules
	}
	switch f.SubscriptionStatus {
	case "", "none", "pending_payment", "active", "pause_scheduled", "paused", "past_due", "cancel_scheduled", "cancelled", "expired":
	default:
		f.SubscriptionStatus = ""
	}
	page, err := h.S.Reporting.Customers(r.Context(), f)
	respond(w, r, http.StatusOK, page, err)
}

func (h *Handler) customer(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	c, err := h.S.Customer(r.Context(), principal(r), id)
	respond(w, r, http.StatusOK, c, err)
}

func (h *Handler) customerOrders(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	list, err := h.S.CustomerOrders(r.Context(), id)
	respond(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

func (h *Handler) customerBilling(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	b, err := h.S.CustomerBilling(r.Context(), id)
	respond(w, r, http.StatusOK, b, err)
}

func (h *Handler) customerPreferences(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := h.S.CustomerPreferences(r.Context(), principal(r).UserID, id)
	respond(w, r, http.StatusOK, p, err)
}

func (h *Handler) customerActivity(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	list, err := h.S.Reporting.CustomerActivity(r.Context(), id, intParam(r, "limit"))
	respond(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

func (h *Handler) notes(subject string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		list, err := h.S.CRM.Notes(r.Context(), subject, id)
		respond(w, r, http.StatusOK, map[string]any{"items": list}, err)
	}
}

func (h *Handler) addNote(subject string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		in, ok := decode[struct {
			Body string `json:"body"`
		}](w, r)
		if !ok {
			return
		}
		n, err := h.S.CRM.AddNote(r.Context(), principal(r).UserID, subject, id, in.Body)
		respond(w, r, http.StatusCreated, n, err)
	}
}

// ---------------------------------------------------------------- CRM

func (h *Handler) leadCRM(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	c, err := h.S.CRM.LeadCRM(r.Context(), id)
	respond(w, r, http.StatusOK, c, err)
}

func (h *Handler) setLeadCRM(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	in, ok := decode[crm.LeadCRMInput](w, r)
	if !ok {
		return
	}
	c, err := h.S.CRM.SetLeadCRM(r.Context(), principal(r).UserID, id, in)
	respond(w, r, http.StatusOK, c, err)
}

func (h *Handler) segments(w http.ResponseWriter, r *http.Request) {
	list, err := h.S.CRM.Segments(r.Context())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	type withSize struct {
		crm.Segment
		Customers int `json:"customers"`
	}
	out := []withSize{}
	for _, s := range list {
		n, err := h.S.Reporting.SegmentCount(r.Context(), s.Rules)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		out = append(out, withSize{Segment: s, Customers: n})
	}
	httpx.JSON(w, r, http.StatusOK, map[string]any{"items": out})
}

func (h *Handler) segment(w http.ResponseWriter, r *http.Request) {
	s, err := h.S.CRM.Segment(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	preview, err := h.S.Reporting.SegmentPreview(r.Context(), s.Rules)
	respond(w, r, http.StatusOK, map[string]any{"segment": s, "preview": preview}, err)
}

func (h *Handler) previewRules(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[crm.Rules](w, r)
	if !ok {
		return
	}
	if f := in.Validate(); len(f) > 0 {
		httpx.Fail(w, r, httpx.ValidationError(f))
		return
	}
	preview, err := h.S.Reporting.SegmentPreview(r.Context(), in)
	respond(w, r, http.StatusOK, preview, err)
}

func (h *Handler) saveSegment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id != "" && !uuidPattern.MatchString(id) {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	in, ok := decode[crm.SegmentInput](w, r)
	if !ok {
		return
	}
	s, err := h.S.CRM.SaveSegment(r.Context(), principal(r).UserID, id, in)
	status := http.StatusOK
	if id == "" {
		status = http.StatusCreated
	}
	respond(w, r, status, s, err)
}

func (h *Handler) deleteSegment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := h.S.CRM.DeleteSegment(r.Context(), principal(r).UserID, id)
	respond(w, r, http.StatusOK, map[string]bool{"deleted": true}, err)
}

func (h *Handler) campaigns(w http.ResponseWriter, r *http.Request) {
	list, err := h.S.CRM.Campaigns(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

func (h *Handler) campaign(w http.ResponseWriter, r *http.Request) {
	c, err := h.S.CRM.Campaign(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	utm := ""
	if c.UTMCampaign != nil {
		utm = *c.UTMCampaign
	}
	a, err := h.S.Reporting.CampaignAttribution(r.Context(), utm)
	respond(w, r, http.StatusOK, map[string]any{"campaign": c, "attribution": a}, err)
}

func (h *Handler) saveCampaign(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id != "" && !uuidPattern.MatchString(id) {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	in, ok := decode[crm.CampaignInput](w, r)
	if !ok {
		return
	}
	c, err := h.S.CRM.SaveCampaign(r.Context(), principal(r).UserID, id, in)
	status := http.StatusOK
	if id == "" {
		status = http.StatusCreated
	}
	respond(w, r, status, c, err)
}

func (h *Handler) campaignStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	in, ok := decode[struct {
		Status string `json:"status"`
	}](w, r)
	if !ok {
		return
	}
	c, err := h.S.CRM.SetCampaignStatus(r.Context(), principal(r).UserID, id, in.Status)
	respond(w, r, http.StatusOK, c, err)
}

func (h *Handler) referrals(w http.ResponseWriter, r *http.Request) {
	list, total, err := h.S.Reporting.Referrals(r.Context(), r.URL.Query().Get("flagged") == "true", intParam(r, "page"), intParam(r, "page_size"))
	respond(w, r, http.StatusOK, map[string]any{"items": list, "total": total}, err)
}

func (h *Handler) retention(w http.ResponseWriter, r *http.Request) {
	list, err := h.S.Reporting.Retention(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": list, "rules": reporting.RetentionRules}, err)
}

// ---------------------------------------------------------------- support

func (h *Handler) cases(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := support.Filter{Status: q.Get("status"), OpenOnly: q.Get("open") == "true", CustomerID: q.Get("customer_id"),
		AssignedTo: q.Get("assigned_to"), Priority: q.Get("priority"), Page: intParam(r, "page"), Size: intParam(r, "page_size")}
	if f.AssignedTo == "me" {
		f.AssignedTo = principal(r).UserID
	}
	page, err := h.S.Support.List(r.Context(), f)
	respond(w, r, http.StatusOK, page, err)
}

func (h *Handler) createCase(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[support.CreateInput](w, r)
	if !ok {
		return
	}
	c, err := h.S.Support.Create(r.Context(), principal(r).UserID, in)
	respond(w, r, http.StatusCreated, c, err)
}

func (h *Handler) getCase(w http.ResponseWriter, r *http.Request) {
	c, err := h.S.Support.Get(r.Context(), r.PathValue("id"))
	respond(w, r, http.StatusOK, c, err)
}

func (h *Handler) updateCase(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	in, ok := decode[support.UpdateInput](w, r)
	if !ok {
		return
	}
	c, err := h.S.Support.Update(r.Context(), principal(r).UserID, id, in)
	respond(w, r, http.StatusOK, c, err)
}

func (h *Handler) caseNote(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	in, ok := decode[struct {
		Body string `json:"body"`
	}](w, r)
	if !ok {
		return
	}
	c, err := h.S.Support.AddNote(r.Context(), principal(r).UserID, id, in.Body)
	respond(w, r, http.StatusCreated, c, err)
}

// ---------------------------------------------------------------- commerce

// subAction acts on a subscription on the customer's behalf, through the
// same code and rules as the customer's own request, with a reason code.
func (h *Handler) subAction(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		in, ok := decode[struct {
			Reason       string `json:"reason"`
			Note         string `json:"note"`
			Weeks        int    `json:"weeks"`
			CancelReason string `json:"cancel_reason"`
		}](w, r)
		if !ok {
			return
		}
		actor := principal(r).UserID
		var v any
		var err error
		switch action {
		case "pause":
			v, err = h.S.Commerce.StaffPause(r.Context(), actor, id, in.Weeks, in.Reason, in.Note)
		case "resume":
			v, err = h.S.Commerce.StaffResume(r.Context(), actor, id, in.Reason, in.Note)
		case "cancel":
			v, err = h.S.Commerce.StaffCancel(r.Context(), actor, id, in.CancelReason, in.Reason, in.Note)
		}
		respond(w, r, http.StatusOK, map[string]any{"subscription": v}, err)
	}
}

func (h *Handler) invoices(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	status := q.Get("status")
	if status != "" && status != "open" && status != "paid" && status != "void" {
		status = ""
	}
	search := q.Get("q")
	if len(search) > 100 {
		search = search[:100]
	}
	list, total, err := h.S.Reporting.Invoices(r.Context(), status, search, intParam(r, "page"), intParam(r, "page_size"))
	respond(w, r, http.StatusOK, map[string]any{"items": list, "total": total}, err)
}

// ---------------------------------------------------------------- exports, alerts, settings, audit

func (h *Handler) exports(w http.ResponseWriter, r *http.Request) {
	list, err := h.S.Exports(r.Context(), principal(r))
	respond(w, r, http.StatusOK, map[string]any{"items": list, "limit": reporting.ExportLimit}, err)
}

func (h *Handler) requestExport(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[ExportInput](w, r)
	if !ok {
		return
	}
	e, err := h.S.RequestExport(r.Context(), principal(r), in)
	respond(w, r, http.StatusAccepted, e, err)
}

func (h *Handler) downloadExport(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	kind, content, err := h.S.DownloadExport(r.Context(), principal(r), id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="mise-`+kind+`.csv"`)
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func (h *Handler) alerts(w http.ResponseWriter, r *http.Request) {
	list, unread, err := h.S.Alerts(r.Context(), principal(r), r.URL.Query().Get("unread") == "true")
	respond(w, r, http.StatusOK, map[string]any{"items": list, "unread": unread}, err)
}

func (h *Handler) readAlerts(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[struct {
		IDs []int64 `json:"ids"`
	}](w, r)
	if !ok {
		return
	}
	if in.IDs == nil {
		in.IDs = []int64{}
	}
	err := h.S.MarkRead(r.Context(), principal(r), in.IDs)
	respond(w, r, http.StatusOK, map[string]bool{"read": true}, err)
}

func (h *Handler) settings(w http.ResponseWriter, r *http.Request) {
	s, err := h.S.Settings(r.Context())
	respond(w, r, http.StatusOK, s, err)
}

func (h *Handler) putSettings(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[map[string]string](w, r)
	if !ok {
		return
	}
	s, err := h.S.PutSettings(r.Context(), principal(r).UserID, in)
	respond(w, r, http.StatusOK, s, err)
}

func (h *Handler) integrations(w http.ResponseWriter, r *http.Request) {
	list, err := h.S.IntegrationStatuses(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

func (h *Handler) audit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := AuditFilter{Actor: q.Get("actor"), Action: q.Get("action"), TargetType: q.Get("target_type"), TargetID: q.Get("target_id"),
		From: q.Get("from"), To: q.Get("to"), Page: intParam(r, "page"), Size: intParam(r, "page_size")}
	if f.Actor != "" && !uuidPattern.MatchString(f.Actor) {
		f.Actor = "00000000-0000-0000-0000-000000000000"
	}
	for _, d := range []*string{&f.From, &f.To} {
		if *d != "" && len(*d) != 10 {
			*d = ""
		}
	}
	list, total, err := h.S.AuditLog(r.Context(), f)
	respond(w, r, http.StatusOK, map[string]any{"items": list, "total": total}, err)
}
