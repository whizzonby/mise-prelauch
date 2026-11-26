package intelligence

import (
	"encoding/json"
	"net/http"

	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/httpx"
)

type Handler struct {
	Box      *Toolbox
	Identity *identity.Service
}

func (h *Handler) Routes(mux httpx.Router) {
	adm := h.Identity.RequireAdmin
	mux.Handle("GET /api/v1/admin/intelligence/tools", adm(identity.PermIntelligenceRead, h.tools))
	mux.Handle("POST /api/v1/admin/intelligence/tools/{name}", adm(identity.PermIntelligenceRead, h.run))
	mux.Handle("GET /api/v1/admin/intelligence/readiness", adm(identity.PermIntelligenceRead, h.readiness))
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
