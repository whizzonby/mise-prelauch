package intelligence

import (
	"encoding/json"
	"net/http"
	"strconv"

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
