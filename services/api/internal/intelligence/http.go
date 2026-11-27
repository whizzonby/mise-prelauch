package intelligence

import (
	"encoding/json"
	"net/http"
	"strconv"

	"mise.tt/api/internal/finance/scenario"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/httpx"
)

type Handler struct {
	Box       *Toolbox
	Assistant *Assistant
	Identity  *identity.Service
}

func (h *Handler) Routes(mux httpx.Router) {
	adm := h.Identity.RequireAdmin
	mux.Handle("GET /api/v1/admin/intelligence/tools", adm(identity.PermIntelligenceRead, h.tools))
	mux.Handle("POST /api/v1/admin/intelligence/tools/{name}", adm(identity.PermIntelligenceRead, h.run))
	mux.Handle("GET /api/v1/admin/intelligence/readiness", adm(identity.PermIntelligenceRead, h.readiness))

	// Ask Mise AI (Stage 5).
	mux.Handle("GET /api/v1/admin/intelligence/ai", adm(identity.PermIntelligenceRead, h.aiInfo))
	mux.Handle("PUT /api/v1/admin/intelligence/ai/settings", adm(identity.PermIntelligenceConfigure, h.aiSettings))
	mux.Handle("POST /api/v1/admin/intelligence/ask", adm(identity.PermIntelligenceAsk, h.ask))
	mux.Handle("GET /api/v1/admin/intelligence/runs", adm(identity.PermIntelligenceRead, h.runs))
	mux.Handle("GET /api/v1/admin/intelligence/runs/{id}", adm(identity.PermIntelligenceRead, h.getRun))
	mux.Handle("POST /api/v1/admin/intelligence/runs/{id}/review", adm(identity.PermIntelligenceRead, h.reviewRun))

	// Scenarios (Stage 6): deterministic and advisory; they change no record but their own.
	mux.Handle("GET /api/v1/admin/intelligence/scenarios/observed", adm(identity.PermIntelligenceScenarioRun, h.scenarioObserved))
	mux.Handle("POST /api/v1/admin/intelligence/scenarios/run", adm(identity.PermIntelligenceScenarioRun, h.scenarioRun))
	mux.Handle("POST /api/v1/admin/intelligence/scenarios/compare", adm(identity.PermIntelligenceScenarioRun, h.scenarioCompare))
	mux.Handle("GET /api/v1/admin/intelligence/scenarios", adm(identity.PermIntelligenceScenarioRun, h.scenarioList))
	mux.Handle("POST /api/v1/admin/intelligence/scenarios", adm(identity.PermIntelligenceScenarioRun, h.scenarioCreate))
	mux.Handle("GET /api/v1/admin/intelligence/scenarios/{id}", adm(identity.PermIntelligenceScenarioRun, h.scenarioGet))
	mux.Handle("POST /api/v1/admin/intelligence/scenarios/{id}/versions", adm(identity.PermIntelligenceScenarioRun, h.scenarioVersion))
	mux.Handle("POST /api/v1/admin/intelligence/scenarios/{id}/archive", adm(identity.PermIntelligenceScenarioRun, h.scenarioArchive))
	mux.Handle("GET /api/v1/admin/intelligence/scenario-versions/{id}", adm(identity.PermIntelligenceScenarioRun, h.scenarioVersionGet))
}

// aiInfo: availability, the caller's usage and (for configure) the settings.
func (h *Handler) aiInfo(w http.ResponseWriter, r *http.Request) {
	p := identity.PrincipalFrom(r.Context())
	u, err := h.Assistant.Usage(r.Context(), p)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out := map[string]any{"status": h.Assistant.Status(r.Context()), "usage": u, "prompt_version": PromptVersion,
		"can_ask": p.Can(identity.PermIntelligenceAsk), "settings": nil}
	if p.Can(identity.PermIntelligenceConfigure) {
		s, err := h.Assistant.Settings(r.Context())
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		out["settings"] = s
	}
	reply(w, r, out, nil)
}

func (h *Handler) aiSettings(w http.ResponseWriter, r *http.Request) {
	var s Settings
	if err := httpx.Decode(w, r, &s, 8<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out, err := h.Assistant.UpdateSettings(r.Context(), identity.PrincipalFrom(r.Context()), s)
	reply(w, r, out, err)
}

func (h *Handler) ask(w http.ResponseWriter, r *http.Request) {
	var in AskInput
	if err := httpx.Decode(w, r, &in, 16<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	run, err := h.Assistant.Ask(r.Context(), identity.PrincipalFrom(r.Context()), in)
	reply(w, r, run, err)
}

func (h *Handler) runs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.Assistant.Runs(r.Context(), identity.PrincipalFrom(r.Context()), limit)
	reply(w, r, map[string]any{"items": items}, err)
}

func (h *Handler) getRun(w http.ResponseWriter, r *http.Request) {
	run, err := h.Assistant.GetRun(r.Context(), identity.PrincipalFrom(r.Context()), r.PathValue("id"))
	reply(w, r, run, err)
}

func (h *Handler) reviewRun(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if err := httpx.Decode(w, r, &body, 4<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	run, err := h.Assistant.Review(r.Context(), identity.PrincipalFrom(r.Context()), r.PathValue("id"), body.Status, body.Note)
	reply(w, r, run, err)
}

func reply(w http.ResponseWriter, r *http.Request, v any, err error) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, v)
}

type toolInfo struct {
	Name        string         `json:"name"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Domain      string         `json:"domain"`
	Requires    []string       `json:"requires"`
	Schema      map[string]any `json:"schema"`
}

// tools lists only the tools the caller may use.
func (h *Handler) tools(w http.ResponseWriter, r *http.Request) {
	out := []toolInfo{}
	for _, t := range Available(identity.PrincipalFrom(r.Context())) {
		req := []string{string(DomainPermission[t.Domain])}
		for _, p := range t.Requires {
			req = append(req, string(p))
		}
		out = append(out, toolInfo{Name: t.Name, Title: t.Title, Description: t.Description, Domain: t.Domain, Requires: req, Schema: t.Schema()})
	}
	reply(w, r, map[string]any{"items": out}, nil)
}

// run runs one tool. Its own permissions are checked by the toolbox, so a
// caller with intelligence.read alone reaches only the general tools.
func (h *Handler) run(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Args json.RawMessage `json:"args"`
	}
	if r.ContentLength != 0 {
		if err := httpx.Decode(w, r, &body, 8<<10); err != nil {
			httpx.Fail(w, r, err)
			return
		}
	}
	res, err := h.Box.Run(r.Context(), identity.PrincipalFrom(r.Context()), r.PathValue("name"), body.Args, "direct")
	reply(w, r, res, err)
}

func (h *Handler) readiness(w http.ResponseWriter, r *http.Request) {
	rd, err := h.Box.Readiness(r.Context())
	reply(w, r, rd, err)
}

func (h *Handler) scenarioObserved(w http.ResponseWriter, r *http.Request) {
	months, _ := strconv.Atoi(r.URL.Query().Get("months"))
	obs, err := h.Box.Scenarios.Observe(r.Context(), months)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	kinds := []map[string]any{}
	for _, k := range []string{"price_increase", "customer_growth", "churn_change", "ingredient_cost", "hire", "kitchen_expansion",
		"delivery_expansion", "new_meal_kit", "marketing", "supplier_change"} {
		kinds = append(kinds, map[string]any{"kind": k, "params": scenario.ChangeKinds[k]})
	}
	vars := []map[string]any{}
	for _, v := range scenario.Variables {
		vars = append(vars, map[string]any{"key": v.Key, "label": v.Label, "unit": v.Unit, "required": v.Required, "description": v.Description, "min": v.Min, "max": v.Max})
	}
	reply(w, r, map[string]any{"observed": obs, "variables": vars, "horizons": scenario.Horizons, "change_kinds": kinds,
		"presets": []string{scenario.Base, scenario.Conservative, scenario.Optimistic, scenario.Custom}, "preset_note": scenario.PresetNote,
		"engine_version": scenario.EngineVersion}, nil)
}

type scenarioBody struct {
	scenario.Request
	Name        string `json:"name"`
	Description string `json:"description"`
	Note        string `json:"note"`
}

func (h *Handler) scenarioRun(w http.ResponseWriter, r *http.Request) {
	var b scenario.Request
	if err := httpx.Decode(w, r, &b, 32<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	p, _, err := h.Box.Scenarios.Resolve(r.Context(), b)
	reply(w, r, p, err)
}

func (h *Handler) scenarioCreate(w http.ResponseWriter, r *http.Request) {
	var b scenarioBody
	if err := httpx.Decode(w, r, &b, 32<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	sc, err := h.Box.Scenarios.Create(r.Context(), identity.PrincipalFrom(r.Context()), b.Name, b.Description, b.Request)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusCreated, sc)
}

func (h *Handler) scenarioVersion(w http.ResponseWriter, r *http.Request) {
	var b scenarioBody
	if err := httpx.Decode(w, r, &b, 32<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	v, err := h.Box.Scenarios.AddVersion(r.Context(), identity.PrincipalFrom(r.Context()), r.PathValue("id"), b.Request, b.Note)
	reply(w, r, v, err)
}

func (h *Handler) scenarioList(w http.ResponseWriter, r *http.Request) {
	items, err := h.Box.Scenarios.List(r.Context(), r.URL.Query().Get("archived") == "true")
	reply(w, r, map[string]any{"items": items}, err)
}

func (h *Handler) scenarioGet(w http.ResponseWriter, r *http.Request) {
	sc, err := h.Box.Scenarios.Get(r.Context(), r.PathValue("id"))
	reply(w, r, sc, err)
}

func (h *Handler) scenarioArchive(w http.ResponseWriter, r *http.Request) {
	sc, err := h.Box.Scenarios.Archive(r.Context(), identity.PrincipalFrom(r.Context()), r.PathValue("id"))
	reply(w, r, sc, err)
}

// scenarioVersionGet returns a stored version and whether recomputing it
// from its stored inputs gives the stored output (reproducibility).
func (h *Handler) scenarioVersionGet(w http.ResponseWriter, r *http.Request) {
	v, err := h.Box.Scenarios.GetVersion(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	again, _ := json.Marshal(h.Box.Scenarios.Rerun(v))
	stored, _ := json.Marshal(v.Projection.Output)
	reply(w, r, map[string]any{"version": v, "reproduced": v.EngineVersion == scenario.EngineVersion && string(again) == string(stored)}, nil)
}

func (h *Handler) scenarioCompare(w http.ResponseWriter, r *http.Request) {
	var b struct {
		VersionIDs []string `json:"version_ids"`
	}
	if err := httpx.Decode(w, r, &b, 4<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	c, err := h.Box.Scenarios.Compare(r.Context(), b.VersionIDs)
	reply(w, r, c, err)
}
