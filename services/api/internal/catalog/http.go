package catalog

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"mise.tt/api/internal/customers"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/vocab"
)

const maxBody = 64 << 10

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Tastes gives recommendations the customer's preferences.
type Tastes interface {
	PreferencesFor(ctx context.Context, p *identity.Principal) (*customers.Preferences, error)
}

type Handler struct {
	Service  *Service
	Identity *identity.Service
	Tastes   Tastes
}

func (h *Handler) Routes(mux httpx.Router) {
	// Public: the catalogue holds no personal data. Responses carry no
	// customer information, so they are the same for everyone.
	mux.Handle("GET /api/v1/meals", http.HandlerFunc(h.list))
	mux.Handle("GET /api/v1/meals/{slug}", http.HandlerFunc(h.detail))
	mux.Handle("GET /api/v1/meal-categories", http.HandlerFunc(h.categories))
	mux.Handle("GET /api/v1/chefs", http.HandlerFunc(h.chefs))
	mux.Handle("GET /api/v1/chefs/{slug}", http.HandlerFunc(h.chef))
	mux.Handle("GET /api/v1/reference", http.HandlerFunc(h.reference))

	mux.Handle("GET /api/v1/me/recommendations", h.Identity.RequireCustomer(h.recommendations))

	adm := h.Identity.RequireAdmin
	mux.Handle("GET /api/v1/admin/catalog/meals", adm(identity.PermCatalogRead, h.adminList))
	mux.Handle("POST /api/v1/admin/catalog/meals", adm(identity.PermCatalogManage, h.adminCreate))
	mux.Handle("GET /api/v1/admin/catalog/meals/{id}", adm(identity.PermCatalogRead, h.adminGet))
	mux.Handle("PUT /api/v1/admin/catalog/meals/{id}", adm(identity.PermCatalogManage, h.adminUpdate))
	mux.Handle("POST /api/v1/admin/catalog/meals/{id}/publish", adm(identity.PermCatalogPublish, h.adminStatus("published")))
	mux.Handle("POST /api/v1/admin/catalog/meals/{id}/unpublish", adm(identity.PermCatalogPublish, h.adminStatus("unpublished")))
	mux.Handle("PUT /api/v1/admin/catalog/meals/{id}/nutrition", adm(identity.PermNutritionVerify, h.adminNutrition))
	mux.Handle("GET /api/v1/admin/catalog/categories", adm(identity.PermCatalogRead, h.adminCategories))
	mux.Handle("POST /api/v1/admin/catalog/categories", adm(identity.PermCatalogManage, h.adminCreateCategory))
	mux.Handle("GET /api/v1/admin/catalog/chefs", adm(identity.PermCatalogRead, h.adminChefs))
	mux.Handle("POST /api/v1/admin/catalog/chefs", adm(identity.PermCatalogManage, h.adminSaveChef))
	mux.Handle("PUT /api/v1/admin/catalog/chefs/{id}", adm(identity.PermCatalogManage, h.adminSaveChef))
}

func respond[T any](w http.ResponseWriter, r *http.Request, status int, v T, err error) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, status, v)
}

func csv(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func atoi(v string) int {
	n, _ := strconv.Atoi(v)
	return n
}

// ---------------------------------------------------------------- public

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := Filter{
		Category: q.Get("category"), Diet: csv(q.Get("diet")), MaxMinutes: atoi(q.Get("max_minutes")),
		Protein: q.Get("protein"), Chef: q.Get("chef"), Query: q.Get("q"), ExcludeAllergens: csv(q.Get("exclude_allergens")),
		Sort: q.Get("sort"), Page: atoi(q.Get("page")), PageSize: atoi(q.Get("page_size")),
	}
	page, err := h.Service.List(r.Context(), f)
	respond(w, r, http.StatusOK, page, err)
}

func (h *Handler) detail(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if !slugPattern.MatchString(slug) {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	d, err := h.Service.Detail(r.Context(), slug)
	respond(w, r, http.StatusOK, d, err)
}

func (h *Handler) categories(w http.ResponseWriter, r *http.Request) {
	list, err := h.Service.Categories(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

func (h *Handler) chefs(w http.ResponseWriter, r *http.Request) {
	list, err := h.Service.Chefs(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

func (h *Handler) chef(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if !slugPattern.MatchString(slug) {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	c, err := h.Service.Chef(r.Context(), slug)
	respond(w, r, http.StatusOK, c, err)
}

// Reference is every fixed list the apps need labels for, from one source.
type Reference struct {
	Dietary            vocab.List `json:"dietary"`
	Allergens          vocab.List `json:"allergens"`
	MealAllergens      vocab.List `json:"meal_allergens"`
	Proteins           vocab.List `json:"proteins"`
	MealInterests      vocab.List `json:"meal_interests"`
	Countries          vocab.List `json:"countries"`
	Regions            vocab.List `json:"regions"`
	Topics             vocab.List `json:"topics"`
	CrossContactNotice string     `json:"cross_contact_notice"`
}

func (h *Handler) reference(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, r, http.StatusOK, Reference{
		Dietary: vocab.Dietary, Allergens: vocab.Allergens, MealAllergens: vocab.MealAllergens, Proteins: vocab.Proteins,
		MealInterests: vocab.MealInterests, Countries: vocab.Countries, Regions: vocab.Regions, Topics: vocab.Topics,
		CrossContactNotice: CrossContactNotice,
	})
}

func (h *Handler) recommendations(w http.ResponseWriter, r *http.Request) {
	p := identity.PrincipalFrom(r.Context())
	prefs, err := h.Tastes.PreferencesFor(r.Context(), p)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	t := Taste{Dietary: prefs.Dietary, MealInterests: prefs.MealInterests, Proteins: prefs.Proteins, MaxCookMinutes: prefs.MaxCookMinutes}
	for _, a := range prefs.Allergies {
		t.Allergies = append(t.Allergies, a.Allergen)
	}
	recs, err := h.Service.Recommend(r.Context(), t, atoi(r.URL.Query().Get("limit")))
	respond(w, r, http.StatusOK, recs, err)
}

// ---------------------------------------------------------------- admin

func actor(r *http.Request) *identity.Principal { return identity.PrincipalFrom(r.Context()) }

func pathID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := strings.ToLower(r.PathValue("id"))
	if !uuidPattern.MatchString(id) {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return "", false
	}
	return id, true
}

func decode[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var in T
	if err := httpx.Decode(w, r, &in, maxBody); err != nil {
		httpx.Fail(w, r, err)
		return in, false
	}
	return in, true
}

func (h *Handler) adminList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, err := h.Service.AdminList(r.Context(), q.Get("status"), q.Get("q"), atoi(q.Get("page")), atoi(q.Get("page_size")))
	respond(w, r, http.StatusOK, page, err)
}

func (h *Handler) adminGet(w http.ResponseWriter, r *http.Request) {
	if id, ok := pathID(w, r); ok {
		m, err := h.Service.AdminGet(r.Context(), id)
		respond(w, r, http.StatusOK, m, err)
	}
}

func (h *Handler) adminCreate(w http.ResponseWriter, r *http.Request) {
	if in, ok := decode[MealInput](w, r); ok {
		m, err := h.Service.CreateMeal(r.Context(), actor(r).UserID, in)
		respond(w, r, http.StatusCreated, m, err)
	}
}

func (h *Handler) adminUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if in, ok := decode[MealInput](w, r); ok {
		m, err := h.Service.UpdateMeal(r.Context(), actor(r).UserID, id, in)
		respond(w, r, http.StatusOK, m, err)
	}
}

func (h *Handler) adminStatus(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if id, ok := pathID(w, r); ok {
			m, err := h.Service.SetStatus(r.Context(), actor(r).UserID, id, status)
			respond(w, r, http.StatusOK, m, err)
		}
	}
}

func (h *Handler) adminNutrition(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if in, ok := decode[NutritionInput](w, r); ok {
		m, err := h.Service.SetNutrition(r.Context(), actor(r).UserID, id, in)
		respond(w, r, http.StatusOK, m, err)
	}
}

func (h *Handler) adminCategories(w http.ResponseWriter, r *http.Request) {
	list, err := h.Service.AdminCategories(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

func (h *Handler) adminCreateCategory(w http.ResponseWriter, r *http.Request) {
	if in, ok := decode[CategoryInput](w, r); ok {
		c, err := h.Service.CreateCategory(r.Context(), actor(r).UserID, in)
		respond(w, r, http.StatusCreated, c, err)
	}
}

func (h *Handler) adminChefs(w http.ResponseWriter, r *http.Request) {
	list, err := h.Service.AdminChefs(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

func (h *Handler) adminSaveChef(w http.ResponseWriter, r *http.Request) {
	id, status := "", http.StatusCreated
	if r.Method == http.MethodPut {
		var ok bool
		if id, ok = pathID(w, r); !ok {
			return
		}
		status = http.StatusOK
	}
	if in, ok := decode[ChefInput](w, r); ok {
		p := actor(r)
		c, err := h.Service.SaveChef(r.Context(), p.UserID, id, in, p.Can(identity.PermCatalogPublish))
		respond(w, r, status, c, err)
	}
}
