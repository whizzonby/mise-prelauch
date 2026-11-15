package analytics

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strings"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/httpx"
)

type Handler struct {
	S        *Service
	Identity *identity.Service
}

// Routes registers the analytics API. Dashboards and exports check the
// dashboard's own permission in the handler (one route serves several
// dashboards); nothing relies on hidden navigation.
func (h *Handler) Routes(mux httpx.Router) {
	adm := h.Identity.RequireAdmin
	mux.Handle("GET /api/v1/admin/analytics/metrics", adm(identity.PermAdminAccess, h.metrics))
	mux.Handle("GET /api/v1/admin/analytics/status", adm(identity.PermAnalyticsExecutiveRead, h.status))
	mux.Handle("POST /api/v1/admin/analytics/refresh", adm(identity.PermAnalyticsExport, h.refresh))
	mux.Handle("GET /api/v1/admin/analytics/data-quality", adm(identity.PermAnalyticsExecutiveRead, h.quality))
	mux.Handle("POST /api/v1/admin/analytics/data-quality/run", adm(identity.PermAnalyticsExport, h.runQuality))
	mux.Handle("GET /api/v1/admin/analytics/series", adm(identity.PermAdminAccess, h.series))
	mux.Handle("GET /api/v1/admin/analytics/{kind}", adm(identity.PermAdminAccess, h.dashboard))
	mux.Handle("GET /api/v1/admin/analytics/{kind}/export", adm(identity.PermAnalyticsExport, h.export))
}

func respond(w http.ResponseWriter, r *http.Request, status int, v any, err error) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, status, v)
}

func principal(r *http.Request) *identity.Principal { return identity.PrincipalFrom(r.Context()) }

func (h *Handler) params(r *http.Request) (Range, Filters, error) {
	q := r.URL.Query()
	rg, err := h.S.ParseRange(q.Get("from"), q.Get("to"))
	return rg, Filters{Plan: q.Get("plan_id"), Meal: q.Get("meal_id"), Zone: q.Get("zone_id"), Supplier: q.Get("supplier_id")}, err
}

// metrics lists the catalogue entries the caller may see.
func (h *Handler) metrics(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	out := []Metric{}
	for _, m := range Metrics() {
		if p.Can(m.Permission) {
			out = append(out, m)
		}
	}
	httpx.JSON(w, r, http.StatusOK, map[string]any{"items": out})
}

func (h *Handler) dashboard(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	perm, ok := Permission(kind)
	if !ok {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	if !principal(r).Can(perm) {
		httpx.Fail(w, r, errForbidden)
		return
	}
	rg, f, err := h.params(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	d, err := h.S.Dashboard(r.Context(), kind, rg, f)
	respond(w, r, http.StatusOK, d, err)
}

// export serves a dashboard as CSV: aggregates only (no customer details),
// formula-safe, never cached, and audited.
func (h *Handler) export(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	perm, ok := Permission(kind)
	if !ok {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	p := principal(r)
	if !p.Can(perm) {
		httpx.Fail(w, r, errForbidden)
		return
	}
	rg, f, err := h.params(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	d, err := h.S.Dashboard(r.Context(), kind, rg, f)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if err := audit.Record(r.Context(), h.S.pool, audit.Entry{ActorUserID: p.UserID, Action: "analytics.exported", TargetType: "analytics_dashboard",
		TargetID: kind, Metadata: map[string]any{"from": d.From, "to": d.To}}); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="mise-%s-%s-%s.csv"`, kind, d.From, d.To))
	cw := csv.NewWriter(w)
	for _, row := range d.CSV() {
		for i := range row {
			row[i] = SafeCSV(row[i])
		}
		_ = cw.Write(row)
	}
	cw.Flush()
}

func (h *Handler) series(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("metric")
	m, ok := Get(id)
	if !ok {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	if !principal(r).Can(m.Permission) {
		httpx.Fail(w, r, errForbidden)
		return
	}
	rg, _, err := h.params(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	pts, err := h.S.Series(r.Context(), id, rg)
	respond(w, r, http.StatusOK, map[string]any{"metric": id, "points": pts}, err)
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	st, err := h.S.Status(r.Context())
	respond(w, r, http.StatusOK, st, err)
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	var in struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := httpx.Decode(w, r, &in, 4<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	rg, err := h.S.ParseRange(in.From, in.To)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	reason := "manual"
	if rg.Days() > 2 {
		reason = "backfill"
	}
	err = EnqueueRefresh(r.Context(), h.S.pool, RefreshJob{From: rg.From.Format(day), To: rg.To.Format(day), Reason: reason, Actor: principal(r).UserID})
	if err == nil {
		err = audit.Record(r.Context(), h.S.pool, audit.Entry{ActorUserID: principal(r).UserID, Action: "analytics.refresh_requested",
			TargetType: "analytics", Metadata: map[string]any{"from": in.From, "to": in.To, "reason": reason}})
	}
	respond(w, r, http.StatusAccepted, map[string]any{"queued": true, "from": rg.From.Format(day), "to": rg.To.Format(day)}, err)
}

func (h *Handler) quality(w http.ResponseWriter, r *http.Request) {
	c, err := h.S.LatestChecks(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": c}, err)
}

func (h *Handler) runQuality(w http.ResponseWriter, r *http.Request) {
	c, err := h.S.RunChecks(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": c}, err)
}

var _ = strings.TrimSpace
