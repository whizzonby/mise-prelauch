package intelligence

import (
	"context"
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

	// Stage 7: meals, risks, opportunities, recommendations, meal proposals. All advisory.
	mux.Handle("GET /api/v1/admin/intelligence/meals", adm(identity.PermIntelligenceOperationalRead, h.meals))
	mux.Handle("GET /api/v1/admin/intelligence/risks", adm(identity.PermIntelligenceRead, h.risks))
	mux.Handle("GET /api/v1/admin/intelligence/risk-thresholds", adm(identity.PermIntelligenceRead, h.thresholds))
	mux.Handle("PUT /api/v1/admin/intelligence/risk-thresholds/{key}", adm(identity.PermIntelligenceConfigure, h.setThreshold))
	mux.Handle("GET /api/v1/admin/intelligence/opportunities", adm(identity.PermIntelligenceRead, h.opportunities))
	mux.Handle("GET /api/v1/admin/intelligence/recommendations", adm(identity.PermIntelligenceRead, h.recommendations))
	mux.Handle("POST /api/v1/admin/intelligence/recommendations", adm(identity.PermIntelligenceRecommendationReview, h.propose))
	mux.Handle("POST /api/v1/admin/intelligence/recommendations/from-risk", adm(identity.PermIntelligenceRecommendationReview, h.saveRisk))
	mux.Handle("POST /api/v1/admin/intelligence/recommendations/from-opportunity", adm(identity.PermIntelligenceRecommendationReview, h.saveOpportunity))
	mux.Handle("GET /api/v1/admin/intelligence/recommendations/{id}", adm(identity.PermIntelligenceRead, h.recommendation))
	mux.Handle("POST /api/v1/admin/intelligence/recommendations/{id}/{action}", adm(identity.PermIntelligenceRecommendationReview, h.reviewRecommendation))
	mux.Handle("GET /api/v1/admin/intelligence/meal-proposals", adm(identity.PermIntelligenceOperationalRead, h.proposals))
	mux.Handle("GET /api/v1/admin/intelligence/meal-proposals/evidence", adm(identity.PermIntelligenceOperationalRead, h.proposalEvidence))
	mux.Handle("POST /api/v1/admin/intelligence/meal-proposals", adm(identity.PermIntelligenceRecommendationReview, h.createProposal))
	mux.Handle("POST /api/v1/admin/intelligence/meal-proposals/generate", adm(identity.PermIntelligenceRecommendationReview, h.generateProposals))
	mux.Handle("GET /api/v1/admin/intelligence/meal-proposals/{id}", adm(identity.PermIntelligenceOperationalRead, h.proposal))
	mux.Handle("POST /api/v1/admin/intelligence/meal-proposals/{id}/{action}", adm(identity.PermIntelligenceRecommendationReview, h.transitionProposal))

	// Stage 8: executive briefings.
	mux.Handle("GET /api/v1/admin/intelligence/briefings", adm(identity.PermIntelligenceExecutiveRead, h.briefings))
	mux.Handle("POST /api/v1/admin/intelligence/briefings", adm(identity.PermIntelligenceExecutiveRead, h.generateBriefing))
	mux.Handle("GET /api/v1/admin/intelligence/briefings/{id}", adm(identity.PermIntelligenceExecutiveRead, h.briefing))
	mux.Handle("POST /api/v1/admin/intelligence/briefings/{id}/narrative", adm(identity.PermIntelligenceExecutiveRead, h.narrate))
	mux.Handle("GET /api/v1/admin/intelligence/briefing-schedules", adm(identity.PermIntelligenceExecutiveRead, h.schedules))
	mux.Handle("PUT /api/v1/admin/intelligence/briefing-schedules/{cadence}", adm(identity.PermIntelligenceConfigure, h.setSchedule))
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

func (h *Handler) meals(w http.ResponseWriter, r *http.Request) {
	if !identity.PrincipalFrom(r.Context()).Can(identity.PermAnalyticsProductionRead) {
		httpx.Fail(w, r, httpx.ErrForbidden)
		return
	}
	q := r.URL.Query()
	mi, err := h.Box.Meals(r.Context(), q.Get("from"), q.Get("to"))
	reply(w, r, mi, err)
}

func (h *Handler) risks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	risks, err := h.Box.Risks(r.Context(), identity.PrincipalFrom(r.Context()), q.Get("from"), q.Get("to"))
	reply(w, r, map[string]any{"items": risks, "note": "Deterministic indicators with thresholds. No probabilities are given."}, err)
}

func (h *Handler) thresholds(w http.ResponseWriter, r *http.Request) {
	items, err := h.Box.ThresholdList(r.Context())
	reply(w, r, map[string]any{"items": items}, err)
}

func (h *Handler) setThreshold(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Enabled  bool    `json:"enabled"`
		Warn     float64 `json:"warn"`
		Critical float64 `json:"critical"`
		Note     string  `json:"note"`
	}
	if err := httpx.Decode(w, r, &b, 4<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	t, err := h.Box.SetThreshold(r.Context(), identity.PrincipalFrom(r.Context()), r.PathValue("key"), b.Enabled, b.Warn, b.Critical, b.Note)
	reply(w, r, t, err)
}

func (h *Handler) opportunities(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ops, err := h.Box.Opportunities(r.Context(), identity.PrincipalFrom(r.Context()), q.Get("from"), q.Get("to"))
	reply(w, r, map[string]any{"items": ops, "note": caution}, err)
}

func (h *Handler) recommendations(w http.ResponseWriter, r *http.Request) {
	items, err := h.Box.Recommendations(r.Context(), identity.PrincipalFrom(r.Context()), r.URL.Query().Get("status"))
	reply(w, r, map[string]any{"items": items, "advisory": Advisory}, err)
}

func (h *Handler) recommendation(w http.ResponseWriter, r *http.Request) {
	rec, err := h.Box.Recommendation(r.Context(), identity.PrincipalFrom(r.Context()), r.PathValue("id"))
	reply(w, r, rec, err)
}

func (h *Handler) propose(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Domain  string `json:"domain"`
		Title   string `json:"title"`
		Summary string `json:"summary"`
	}
	if err := httpx.Decode(w, r, &b, 8<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	rec, err := h.Box.Propose(r.Context(), identity.PrincipalFrom(r.Context()), b.Domain, b.Title, b.Summary)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusCreated, rec)
}

type saveBody struct {
	Key  string `json:"key"`
	From string `json:"from"`
	To   string `json:"to"`
}

func (h *Handler) saveFrom(w http.ResponseWriter, r *http.Request, fn func(context.Context, *identity.Principal, string, string, string) (*Recommendation, bool, error)) {
	var b saveBody
	if err := httpx.Decode(w, r, &b, 4<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	rec, created, err := fn(r.Context(), identity.PrincipalFrom(r.Context()), b.Key, b.From, b.To)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.JSON(w, r, status, rec)
}

func (h *Handler) saveRisk(w http.ResponseWriter, r *http.Request) { h.saveFrom(w, r, h.Box.SaveRisk) }
func (h *Handler) saveOpportunity(w http.ResponseWriter, r *http.Request) {
	h.saveFrom(w, r, h.Box.SaveOpportunity)
}

func (h *Handler) reviewRecommendation(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Note     string `json:"note"`
		Assignee string `json:"assignee"`
	}
	if err := httpx.Decode(w, r, &b, 4<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	rec, err := h.Box.ReviewRecommendation(r.Context(), identity.PrincipalFrom(r.Context()), r.PathValue("id"), r.PathValue("action"), b.Note, b.Assignee)
	reply(w, r, rec, err)
}

func (h *Handler) proposals(w http.ResponseWriter, r *http.Request) {
	items, err := h.Box.Proposals(r.Context(), r.URL.Query().Get("status"))
	reply(w, r, map[string]any{"items": items, "required_approvals": RequiredApprovals}, err)
}

func (h *Handler) proposalEvidence(w http.ResponseWriter, r *http.Request) {
	ev, err := h.Box.Evidence(r.Context())
	reply(w, r, ev, err)
}

func (h *Handler) proposal(w http.ResponseWriter, r *http.Request) {
	x, err := h.Box.Proposal(r.Context(), r.PathValue("id"))
	reply(w, r, x, err)
}

func (h *Handler) createProposal(w http.ResponseWriter, r *http.Request) {
	var in ProposalInput
	if err := httpx.Decode(w, r, &in, 32<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	x, err := h.Box.CreateProposal(r.Context(), identity.PrincipalFrom(r.Context()), in)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusCreated, x)
}

// generateProposals also needs intelligence.ask (it calls the model).
func (h *Handler) generateProposals(w http.ResponseWriter, r *http.Request) {
	p := identity.PrincipalFrom(r.Context())
	if !p.Can(identity.PermIntelligenceAsk) {
		httpx.Fail(w, r, httpx.ErrForbidden)
		return
	}
	var b struct {
		Brief string `json:"brief"`
	}
	if err := httpx.Decode(w, r, &b, 8<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	items, run, err := h.Assistant.GenerateConcepts(r.Context(), p, b.Brief)
	reply(w, r, map[string]any{"items": items, "run": run}, err)
}

func (h *Handler) transitionProposal(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Note     string `json:"note"`
		RecipeID string `json:"recipe_id"`
	}
	if err := httpx.Decode(w, r, &b, 4<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	x, err := h.Box.TransitionProposal(r.Context(), identity.PrincipalFrom(r.Context()), r.PathValue("id"), r.PathValue("action"), b.Note, b.RecipeID)
	reply(w, r, x, err)
}

func (h *Handler) withAINote(ctx context.Context, b *Briefing) *Briefing {
	if b != nil && b.Narrative == nil {
		if st := h.Assistant.Status(ctx); st.Available {
			b.AINote = "No AI narrative yet for this version."
		} else {
			b.AINote = "AI unavailable: " + st.Reason + " The briefing is complete without it."
		}
	}
	return b
}

func (h *Handler) briefings(w http.ResponseWriter, r *http.Request) {
	items, err := h.Box.Briefings(r.Context(), r.URL.Query().Get("cadence"))
	for _, b := range items {
		h.withAINote(r.Context(), b)
	}
	reply(w, r, map[string]any{"items": items}, err)
}

func (h *Handler) generateBriefing(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Cadence string `json:"cadence"`
	}
	if err := httpx.Decode(w, r, &b, 1<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	br, err := h.Box.GenerateBriefing(r.Context(), identity.PrincipalFrom(r.Context()).UserID, b.Cadence, "manual")
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	status := http.StatusOK
	if br.Created {
		status = http.StatusCreated
	}
	httpx.JSON(w, r, status, h.withAINote(r.Context(), br))
}

func (h *Handler) briefing(w http.ResponseWriter, r *http.Request) {
	br, err := h.Box.GetBriefing(r.Context(), r.PathValue("id"))
	reply(w, r, h.withAINote(r.Context(), br), err)
}

// narrate also needs intelligence.ask (it calls the model).
func (h *Handler) narrate(w http.ResponseWriter, r *http.Request) {
	p := identity.PrincipalFrom(r.Context())
	if !p.Can(identity.PermIntelligenceAsk) {
		httpx.Fail(w, r, httpx.ErrForbidden)
		return
	}
	br, err := h.Assistant.Narrate(r.Context(), p, r.PathValue("id"))
	reply(w, r, br, err)
}

func (h *Handler) schedules(w http.ResponseWriter, r *http.Request) {
	items, err := h.Box.Schedules(r.Context())
	reply(w, r, map[string]any{"items": items, "note": "Schedules ship disabled. Recipients are staff accounts allowed to read executive intelligence, checked again before each email."}, err)
}

func (h *Handler) setSchedule(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Enabled     bool     `json:"enabled"`
		Recipients  []string `json:"recipients"`
		ApprovalRef string   `json:"approval_ref"`
	}
	if err := httpx.Decode(w, r, &b, 4<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	s, err := h.Box.SetSchedule(r.Context(), identity.PrincipalFrom(r.Context()), r.PathValue("cadence"), b.Enabled, b.Recipients, b.ApprovalRef)
	reply(w, r, s, err)
}
