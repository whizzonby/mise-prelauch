package culinary

import (
	"net/http"
	"strconv"
	"strings"

	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/httpx"
)

type Handler struct {
	S        *Service
	Identity *identity.Service
}

// Routes registers the culinary API. Every route needs an admin session
// that has finished MFA (where required) and the named permission; who may
// edit which recipe, and who may review whose work, is checked again in the
// service.
func (h *Handler) Routes(mux httpx.Router) {
	adm := h.Identity.RequireAdmin
	// Units are reference data every console user needs (kitchen staff record
	// waste in them), so any staff session may read them.
	mux.Handle("GET /api/v1/admin/units", adm(identity.PermAdminAccess, h.units))
	mux.Handle("GET /api/v1/admin/ingredients", adm(identity.PermIngredientsRead, h.ingredients))
	mux.Handle("POST /api/v1/admin/ingredients", adm(identity.PermIngredientsManage, h.createIngredient))
	mux.Handle("GET /api/v1/admin/ingredients/{id}", adm(identity.PermIngredientsRead, h.ingredient))
	mux.Handle("PATCH /api/v1/admin/ingredients/{id}", adm(identity.PermIngredientsManage, h.updateIngredient))
	mux.Handle("POST /api/v1/admin/ingredients/{id}/conversions", adm(identity.PermIngredientsManage, h.setConversion))
	mux.Handle("GET /api/v1/admin/cost-references", adm(identity.PermIngredientsRead, h.costRefs))
	mux.Handle("POST /api/v1/admin/cost-references", adm(identity.PermIngredientsManage, h.addCost))

	mux.Handle("GET /api/v1/admin/chefs/team", adm(identity.PermRecipesRead, h.chefs))
	mux.Handle("PATCH /api/v1/admin/chefs/{id}/team", adm(identity.PermChefsManage, h.updateChef))

	mux.Handle("GET /api/v1/admin/recipes", adm(identity.PermRecipesRead, h.recipes))
	mux.Handle("POST /api/v1/admin/recipes", adm(identity.PermRecipesCreate, h.createRecipe))
	mux.Handle("GET /api/v1/admin/recipes/queue", adm(identity.PermRecipesRead, h.queue))
	mux.Handle("GET /api/v1/admin/recipes/{id}", adm(identity.PermRecipesRead, h.recipe))
	mux.Handle("PATCH /api/v1/admin/recipes/{id}", adm(identity.PermRecipesEdit, h.updateRecipe))
	mux.Handle("GET /api/v1/admin/recipes/{id}/versions/{n}", adm(identity.PermRecipesRead, h.version))
	mux.Handle("PUT /api/v1/admin/recipes/{id}/draft", adm(identity.PermRecipesEdit, h.saveDraft))
	mux.Handle("POST /api/v1/admin/recipes/{id}/submit", adm(identity.PermRecipesSubmit, h.submit))
	mux.Handle("POST /api/v1/admin/recipes/{id}/review", adm(identity.PermRecipesReview, h.culinaryReview))
	mux.Handle("POST /api/v1/admin/recipes/{id}/request-changes", adm(identity.PermRecipesReview, h.requestChanges))
	mux.Handle("POST /api/v1/admin/recipes/{id}/approve", adm(identity.PermRecipesApprove, h.approve))
	mux.Handle("POST /api/v1/admin/recipes/{id}/publish", adm(identity.PermRecipesPublish, h.publish))
	mux.Handle("POST /api/v1/admin/recipes/{id}/archive", adm(identity.PermRecipesPublish, h.archive))
	mux.Handle("POST /api/v1/admin/recipes/{id}/revise", adm(identity.PermRecipesEdit, h.revise))
	mux.Handle("GET /api/v1/admin/recipes/{id}/costing", adm(identity.PermRecipesRead, h.costing))

	mux.Handle("GET /api/v1/admin/nutrition/reviews", adm(identity.PermNutritionRead, h.nutritionQueue))
	mux.Handle("POST /api/v1/admin/nutrition/reviews/{id}/approve", adm(identity.PermNutritionApprove, h.nutritionApprove))
	mux.Handle("POST /api/v1/admin/nutrition/reviews/{id}/request-changes", adm(identity.PermNutritionReview, h.nutritionChanges))
}

func respond(w http.ResponseWriter, r *http.Request, status int, v any, err error) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, status, v)
}

func principal(r *http.Request) *identity.Principal { return identity.PrincipalFrom(r.Context()) }

func decode[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var v T
	// An empty body is an empty object for action endpoints.
	if r.ContentLength == 0 && r.Header.Get("Content-Type") == "" {
		return v, true
	}
	if err := httpx.Decode(w, r, &v, 256<<10); err != nil {
		httpx.Fail(w, r, err)
		return v, false
	}
	return v, true
}

func id(r *http.Request) string { return strings.ToLower(r.PathValue("id")) }

func intQ(r *http.Request, key string) int {
	n, _ := strconv.Atoi(r.URL.Query().Get(key))
	return n
}

func (h *Handler) units(w http.ResponseWriter, r *http.Request) {
	u, err := h.S.Units(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": u}, err)
}

func (h *Handler) ingredients(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p, err := h.S.Ingredients(r.Context(), IngredientFilter{Query: q.Get("q"), Category: q.Get("category"), Active: q.Get("active"),
		Page: intQ(r, "page"), Size: intQ(r, "page_size")})
	respond(w, r, http.StatusOK, p, err)
}

func (h *Handler) ingredient(w http.ResponseWriter, r *http.Request) {
	i, err := h.S.Ingredient(r.Context(), id(r))
	respond(w, r, http.StatusOK, i, err)
}

func (h *Handler) createIngredient(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[IngredientInput](w, r)
	if !ok {
		return
	}
	i, err := h.S.CreateIngredient(r.Context(), principal(r).UserID, in)
	respond(w, r, http.StatusCreated, i, err)
}

func (h *Handler) updateIngredient(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[IngredientInput](w, r)
	if !ok {
		return
	}
	i, err := h.S.UpdateIngredient(r.Context(), principal(r).UserID, id(r), in)
	respond(w, r, http.StatusOK, i, err)
}

func (h *Handler) setConversion(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[ConversionInput](w, r)
	if !ok {
		return
	}
	i, err := h.S.SetConversion(r.Context(), principal(r).UserID, id(r), in)
	respond(w, r, http.StatusOK, i, err)
}

func (h *Handler) costRefs(w http.ResponseWriter, r *http.Request) {
	c, err := h.S.CostReferences(r.Context(), r.URL.Query().Get("kind"))
	respond(w, r, http.StatusOK, map[string]any{"items": c}, err)
}

func (h *Handler) addCost(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[CostInput](w, r)
	if !ok {
		return
	}
	c, err := h.S.AddCost(r.Context(), principal(r).UserID, in)
	respond(w, r, http.StatusCreated, c, err)
}

func (h *Handler) chefs(w http.ResponseWriter, r *http.Request) {
	c, err := h.S.Chefs(r.Context(), principal(r))
	respond(w, r, http.StatusOK, map[string]any{"items": c}, err)
}

func (h *Handler) updateChef(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[ChefInput](w, r)
	if !ok {
		return
	}
	c, err := h.S.UpdateChef(r.Context(), principal(r), id(r), in)
	respond(w, r, http.StatusOK, map[string]any{"items": c}, err)
}

func (h *Handler) recipes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p, err := h.S.Recipes(r.Context(), principal(r), RecipeFilter{Query: q.Get("q"), Status: q.Get("status"), Mine: q.Get("mine") == "true",
		ChefID: q.Get("chef_id"), Page: intQ(r, "page"), Size: intQ(r, "page_size")})
	respond(w, r, http.StatusOK, p, err)
}

func (h *Handler) createRecipe(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[RecipeInput](w, r)
	if !ok {
		return
	}
	rec, err := h.S.CreateRecipe(r.Context(), principal(r), in)
	respond(w, r, http.StatusCreated, rec, err)
}

func (h *Handler) recipe(w http.ResponseWriter, r *http.Request) {
	rec, err := h.S.Recipe(r.Context(), principal(r), id(r))
	respond(w, r, http.StatusOK, rec, err)
}

func (h *Handler) updateRecipe(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[RecipeInput](w, r)
	if !ok {
		return
	}
	rec, err := h.S.UpdateRecipe(r.Context(), principal(r), id(r), in)
	respond(w, r, http.StatusOK, rec, err)
}

func (h *Handler) version(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 0 {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	v, err := h.S.Version(r.Context(), id(r), n)
	respond(w, r, http.StatusOK, v, err)
}

func (h *Handler) saveDraft(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[DraftInput](w, r)
	if !ok {
		return
	}
	v, err := h.S.SaveDraft(r.Context(), principal(r), id(r), in)
	respond(w, r, http.StatusOK, v, err)
}

type notesBody struct {
	Notes string `json:"notes"`
}

func (h *Handler) submit(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[notesBody](w, r)
	if !ok {
		return
	}
	v, err := h.S.Submit(r.Context(), principal(r), id(r), in.Notes)
	respond(w, r, http.StatusOK, v, err)
}

func (h *Handler) culinaryReview(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[ReviewInput](w, r)
	if !ok {
		return
	}
	v, err := h.S.CulinaryReview(r.Context(), principal(r), id(r), in)
	respond(w, r, http.StatusOK, v, err)
}

func (h *Handler) requestChanges(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[ReviewInput](w, r)
	if !ok {
		return
	}
	v, err := h.S.RequestChanges(r.Context(), principal(r), id(r), "culinary", in)
	respond(w, r, http.StatusOK, v, err)
}

func (h *Handler) approve(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[notesBody](w, r)
	if !ok {
		return
	}
	v, err := h.S.Approve(r.Context(), principal(r), id(r), in.Notes)
	respond(w, r, http.StatusOK, v, err)
}

func (h *Handler) publish(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[PublishInput](w, r)
	if !ok {
		return
	}
	v, err := h.S.Publish(r.Context(), principal(r), id(r), in)
	respond(w, r, http.StatusOK, v, err)
}

func (h *Handler) archive(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[struct {
		Version int    `json:"version"`
		Reason  string `json:"reason"`
	}](w, r)
	if !ok {
		return
	}
	v, err := h.S.Archive(r.Context(), principal(r), id(r), in.Version, in.Reason)
	respond(w, r, http.StatusOK, v, err)
}

func (h *Handler) revise(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[struct {
		ChangeSummary string `json:"change_summary"`
	}](w, r)
	if !ok {
		return
	}
	v, err := h.S.Revise(r.Context(), principal(r), id(r), in.ChangeSummary)
	respond(w, r, http.StatusCreated, v, err)
}

func (h *Handler) costing(w http.ResponseWriter, r *http.Request) {
	c, err := h.S.RecipeCosting(r.Context(), id(r), intQ(r, "version"))
	respond(w, r, http.StatusOK, c, err)
}

func (h *Handler) queue(w http.ResponseWriter, r *http.Request) {
	stage := r.URL.Query().Get("stage")
	if stage == "" {
		stage = "culinary"
	}
	items, err := h.S.ReviewQueue(r.Context(), stage)
	respond(w, r, http.StatusOK, map[string]any{"items": items}, err)
}

func (h *Handler) nutritionQueue(w http.ResponseWriter, r *http.Request) {
	items, err := h.S.ReviewQueue(r.Context(), "nutrition")
	respond(w, r, http.StatusOK, map[string]any{"items": items}, err)
}

func (h *Handler) nutritionApprove(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[notesBody](w, r)
	if !ok {
		return
	}
	v, err := h.S.NutritionApprove(r.Context(), principal(r), id(r), in.Notes)
	respond(w, r, http.StatusOK, v, err)
}

func (h *Handler) nutritionChanges(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[ReviewInput](w, r)
	if !ok {
		return
	}
	v, err := h.S.RequestChanges(r.Context(), principal(r), id(r), "nutrition", in)
	respond(w, r, http.StatusOK, v, err)
}
