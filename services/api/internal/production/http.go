package production

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/httpx"
)

type Handler struct {
	S        *Service
	Identity *identity.Service
}

// Routes registers the production API. Every route needs an admin session
// that has finished MFA (where required) and the named permission. Quality
// hold release has its own permission, so kitchen staff can never bypass a
// hold.
func (h *Handler) Routes(mux httpx.Router) {
	adm := h.Identity.RequireAdmin
	mux.Handle("GET /api/v1/admin/production/overview", adm(identity.PermProductionRead, h.overview))
	mux.Handle("GET /api/v1/admin/production/calendar", adm(identity.PermProductionRead, h.calendar))
	mux.Handle("GET /api/v1/admin/production/demand", adm(identity.PermProductionRead, h.demand))
	mux.Handle("POST /api/v1/admin/production/snapshots", adm(identity.PermProductionPlan, h.snapshot))
	mux.Handle("GET /api/v1/admin/production/snapshots/{id}", adm(identity.PermProductionRead, h.snapshotDetail))
	mux.Handle("GET /api/v1/admin/production/plans", adm(identity.PermProductionRead, h.plans))
	mux.Handle("POST /api/v1/admin/production/plans", adm(identity.PermProductionPlan, h.createPlan))
	mux.Handle("GET /api/v1/admin/production/plans/{id}", adm(identity.PermProductionRead, h.plan))
	mux.Handle("PATCH /api/v1/admin/production/plans/{id}/items/{item}", adm(identity.PermProductionPlan, h.adjust))
	mux.Handle("POST /api/v1/admin/production/plans/{id}/approve", adm(identity.PermProductionApprove, h.approve))
	mux.Handle("POST /api/v1/admin/production/plans/{id}/release", adm(identity.PermProductionApprove, h.release))
	mux.Handle("POST /api/v1/admin/production/plans/{id}/cancel", adm(identity.PermProductionApprove, h.cancel))
	mux.Handle("GET /api/v1/admin/production/requirements", adm(identity.PermProductionRead, h.requirements))

	mux.Handle("GET /api/v1/admin/production/batches", adm(identity.PermProductionRead, h.batches))
	mux.Handle("GET /api/v1/admin/production/batches/{id}", adm(identity.PermProductionRead, h.batch))
	mux.Handle("POST /api/v1/admin/production/batches/{id}/start", adm(identity.PermProductionExecute, h.start))
	mux.Handle("POST /api/v1/admin/production/batches/{id}/output", adm(identity.PermProductionExecute, h.output))
	mux.Handle("POST /api/v1/admin/production/batches/{id}/complete", adm(identity.PermProductionExecute, h.complete))
	mux.Handle("POST /api/v1/admin/production/batches/{id}/cancel", adm(identity.PermProductionApprove, h.cancelBatch))
	mux.Handle("PUT /api/v1/admin/production/batches/{id}/packaging", adm(identity.PermPackagingManage, h.packaging))
	mux.Handle("GET /api/v1/admin/production/batches/{id}/label", adm(identity.PermProductionRead, h.label))

	mux.Handle("GET /api/v1/admin/production/tasks", adm(identity.PermProductionRead, h.tasks))
	mux.Handle("PATCH /api/v1/admin/production/tasks/{id}", adm(identity.PermProductionExecute, h.updateTask))
	mux.Handle("GET /api/v1/admin/production/task-templates", adm(identity.PermProductionRead, h.taskTemplates))
	mux.Handle("POST /api/v1/admin/production/task-templates", adm(identity.PermProductionConfigure, h.saveTaskTemplate))
	mux.Handle("PATCH /api/v1/admin/production/task-templates/{id}", adm(identity.PermProductionConfigure, h.saveTaskTemplate))
	mux.Handle("GET /api/v1/admin/production/settings", adm(identity.PermProductionRead, h.settings))
	mux.Handle("PUT /api/v1/admin/production/settings", adm(identity.PermProductionConfigure, h.saveSettings))

	mux.Handle("GET /api/v1/admin/production/waste", adm(identity.PermProductionRead, h.waste))
	mux.Handle("POST /api/v1/admin/production/waste", adm(identity.PermWasteRecord, h.recordWaste))

	mux.Handle("GET /api/v1/admin/quality/templates", adm(identity.PermQualityRead, h.qualityTemplates))
	mux.Handle("POST /api/v1/admin/quality/templates", adm(identity.PermProductionConfigure, h.saveQualityTemplate))
	mux.Handle("PATCH /api/v1/admin/quality/templates/{id}", adm(identity.PermProductionConfigure, h.saveQualityTemplate))
	mux.Handle("POST /api/v1/admin/quality/checks", adm(identity.PermQualityInspect, h.check))
	mux.Handle("GET /api/v1/admin/quality/holds", adm(identity.PermQualityRead, h.holdsList))
	mux.Handle("POST /api/v1/admin/quality/holds", adm(identity.PermQualityInspect, h.placeHold))
	mux.Handle("POST /api/v1/admin/quality/holds/{id}/release", adm(identity.PermQualityReleaseHold, h.resolveHold))
}

func respond(w http.ResponseWriter, r *http.Request, status int, v any, err error) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, status, v)
}

func actor(r *http.Request) string { return identity.PrincipalFrom(r.Context()).UserID }

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

func id(r *http.Request) string { return strings.ToLower(r.PathValue("id")) }

type reasonBody struct {
	Reason string `json:"reason"`
}

func (h *Handler) overview(w http.ResponseWriter, r *http.Request) {
	o, err := h.S.Overview(r.Context())
	respond(w, r, http.StatusOK, o, err)
}

func (h *Handler) calendar(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	c, err := h.S.Calendar(r.Context(), r.URL.Query().Get("from"), days)
	respond(w, r, http.StatusOK, c, err)
}

func (h *Handler) demand(w http.ResponseWriter, r *http.Request) {
	d, err := h.S.Demand(r.Context(), r.URL.Query().Get("week"))
	respond(w, r, http.StatusOK, d, err)
}

func (h *Handler) snapshot(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[struct {
		Week string `json:"week"`
	}](w, r)
	if !ok {
		return
	}
	snap, created, err := h.S.TakeSnapshot(r.Context(), actor(r), in.Week)
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	respond(w, r, status, map[string]any{"snapshot": snap, "created": created}, err)
}

func (h *Handler) snapshotDetail(w http.ResponseWriter, r *http.Request) {
	d, err := h.S.SnapshotDetail(r.Context(), id(r))
	respond(w, r, http.StatusOK, d, err)
}

func (h *Handler) plans(w http.ResponseWriter, r *http.Request) {
	p, err := h.S.Plans(r.Context(), r.URL.Query().Get("status"))
	respond(w, r, http.StatusOK, map[string]any{"items": p}, err)
}

func (h *Handler) createPlan(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[PlanInput](w, r)
	if !ok {
		return
	}
	p, created, err := h.S.CreatePlan(r.Context(), actor(r), in)
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	respond(w, r, status, p, err)
}

func (h *Handler) plan(w http.ResponseWriter, r *http.Request) {
	p, err := h.S.Plan(r.Context(), id(r))
	respond(w, r, http.StatusOK, p, err)
}

func (h *Handler) adjust(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[AdjustInput](w, r)
	if !ok {
		return
	}
	p, err := h.S.AdjustItem(r.Context(), actor(r), id(r), strings.ToLower(r.PathValue("item")), in)
	respond(w, r, http.StatusOK, p, err)
}

func (h *Handler) approve(w http.ResponseWriter, r *http.Request) {
	p, err := h.S.Approve(r.Context(), actor(r), id(r))
	respond(w, r, http.StatusOK, p, err)
}

func (h *Handler) release(w http.ResponseWriter, r *http.Request) {
	p, err := h.S.Release(r.Context(), actor(r), id(r))
	respond(w, r, http.StatusOK, p, err)
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[reasonBody](w, r)
	if !ok {
		return
	}
	p, err := h.S.Cancel(r.Context(), actor(r), id(r), in.Reason)
	respond(w, r, http.StatusOK, p, err)
}

// requirements lists ingredient requirements; format=csv downloads them
// (the procurement input for Sprint 6), and the download is audited.
func (h *Handler) requirements(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rows, err := h.S.RequirementRows(r.Context(), q.Get("plan_id"), q.Get("from"), q.Get("to"))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if q.Get("format") != "csv" {
		httpx.JSON(w, r, http.StatusOK, map[string]any{"items": rows})
		return
	}
	if err := audit.Record(r.Context(), h.S.pool, audit.Entry{ActorUserID: actor(r), Action: "production.requirements_exported",
		TargetType: "production_plan", TargetID: q.Get("plan_id"), Metadata: map[string]any{"rows": len(rows), "from": q.Get("from"), "to": q.Get("to")}}); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="ingredient-requirements.csv"`)
	w.Header().Set("Cache-Control", "no-store")
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"production_date", "plan", "plan_status", "ingredient_id", "ingredient", "preparation", "quantity", "unit", "status", "approximate", "batches", "meals"})
	for _, x := range rows {
		quantity := ""
		if x.Quantity != nil {
			quantity = *x.Quantity
		}
		_ = cw.Write([]string{x.ProductionDate, x.PlanNumber, x.PlanStatus, x.IngredientID, safeCell(x.IngredientName), safeCell(x.Preparation), quantity, x.Unit,
			x.Status, strconv.FormatBool(x.Approximate), x.SourceBatches, safeCell(x.SourceMeals)})
	}
	cw.Flush()
}

// safeCell stops spreadsheets treating a cell as a formula.
func safeCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func (h *Handler) batches(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := BatchFilter{PlanID: q.Get("plan_id"), Status: q.Get("status"), From: q.Get("from"), To: q.Get("to"), Open: q.Get("open") == "true"}
	if d := q.Get("date"); d != "" {
		if d == "today" {
			d = h.S.today().Format(day)
		}
		f.From, f.To = d, d
	}
	b, err := h.S.Batches(r.Context(), f)
	respond(w, r, http.StatusOK, map[string]any{"items": b}, err)
}

func (h *Handler) batch(w http.ResponseWriter, r *http.Request) {
	b, err := h.S.Batch(r.Context(), id(r))
	respond(w, r, http.StatusOK, b, err)
}

func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[StartInput](w, r)
	if !ok {
		return
	}
	b, err := h.S.Start(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusOK, b, err)
}

func (h *Handler) output(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[OutputInput](w, r)
	if !ok {
		return
	}
	b, err := h.S.RecordOutput(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusOK, b, err)
}

func (h *Handler) complete(w http.ResponseWriter, r *http.Request) {
	b, err := h.S.Complete(r.Context(), actor(r), id(r))
	respond(w, r, http.StatusOK, b, err)
}

func (h *Handler) cancelBatch(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[reasonBody](w, r)
	if !ok {
		return
	}
	b, err := h.S.CancelBatch(r.Context(), actor(r), id(r), in.Reason)
	respond(w, r, http.StatusOK, b, err)
}

func (h *Handler) packaging(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[PackagingInput](w, r)
	if !ok {
		return
	}
	p := identity.PrincipalFrom(r.Context())
	if in.AssignedTo != nil && *in.AssignedTo != "" && *in.AssignedTo != "me" && !p.Can(identity.PermProductionPlan) {
		httpx.Fail(w, r, httpx.NewError(http.StatusForbidden, "forbidden", "Only a production manager assigns packing to someone else."))
		return
	}
	b, err := h.S.SavePackaging(r.Context(), p.UserID, id(r), in)
	respond(w, r, http.StatusOK, b, err)
}

func (h *Handler) label(w http.ResponseWriter, r *http.Request) {
	l, err := h.S.Label(r.Context(), id(r))
	respond(w, r, http.StatusOK, l, err)
}

func (h *Handler) tasks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := TaskFilter{BatchID: q.Get("batch_id"), Date: q.Get("date"), Status: q.Get("status"), AssignedTo: q.Get("assigned_to"), All: q.Get("all") == "true"}
	if f.Date == "today" {
		f.Date = h.S.today().Format(day)
	}
	if q.Get("mine") == "true" {
		f.AssignedTo = actor(r)
	}
	t, err := h.S.Tasks(r.Context(), f)
	respond(w, r, http.StatusOK, map[string]any{"items": t}, err)
}

func (h *Handler) updateTask(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[TaskInput](w, r)
	if !ok {
		return
	}
	t, err := h.S.UpdateTask(r.Context(), identity.PrincipalFrom(r.Context()), id(r), in)
	respond(w, r, http.StatusOK, t, err)
}

func (h *Handler) taskTemplates(w http.ResponseWriter, r *http.Request) {
	t, err := h.S.TaskTemplates(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": t}, err)
}

func (h *Handler) saveTaskTemplate(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[TaskTemplateInput](w, r)
	if !ok {
		return
	}
	t, err := h.S.SaveTaskTemplate(r.Context(), actor(r), id(r), in)
	status := http.StatusOK
	if r.Method == http.MethodPost {
		status = http.StatusCreated
	}
	respond(w, r, status, map[string]any{"items": t}, err)
}

func (h *Handler) settings(w http.ResponseWriter, r *http.Request) {
	s, err := h.S.Settings(r.Context())
	respond(w, r, http.StatusOK, s, err)
}

func (h *Handler) saveSettings(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[SettingsInput](w, r)
	if !ok {
		return
	}
	s, err := h.S.SaveSettings(r.Context(), actor(r), in)
	respond(w, r, http.StatusOK, s, err)
}

func (h *Handler) waste(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, err := h.S.Waste(r.Context(), WasteFilter{BatchID: q.Get("batch_id"), From: q.Get("from"), To: q.Get("to")})
	respond(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

func (h *Handler) recordWaste(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[WasteInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.RecordWaste(r.Context(), actor(r), in)
	respond(w, r, http.StatusCreated, x, err)
}

func (h *Handler) qualityTemplates(w http.ResponseWriter, r *http.Request) {
	t, err := h.S.QualityTemplates(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": t}, err)
}

func (h *Handler) saveQualityTemplate(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[QualityTemplateInput](w, r)
	if !ok {
		return
	}
	t, err := h.S.SaveQualityTemplate(r.Context(), actor(r), id(r), in)
	status := http.StatusOK
	if r.Method == http.MethodPost {
		status = http.StatusCreated
	}
	respond(w, r, status, map[string]any{"items": t}, err)
}

func (h *Handler) check(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[CheckInput](w, r)
	if !ok {
		return
	}
	b, err := h.S.RecordCheck(r.Context(), actor(r), in)
	respond(w, r, http.StatusCreated, b, err)
}

func (h *Handler) holdsList(w http.ResponseWriter, r *http.Request) {
	list, err := h.S.Holds(r.Context(), r.URL.Query().Get("open") == "true")
	respond(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

func (h *Handler) placeHold(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[HoldInput](w, r)
	if !ok {
		return
	}
	b, err := h.S.PlaceHold(r.Context(), actor(r), in)
	respond(w, r, http.StatusCreated, b, err)
}

func (h *Handler) resolveHold(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[ResolveInput](w, r)
	if !ok {
		return
	}
	b, err := h.S.ResolveHold(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusOK, b, err)
}
