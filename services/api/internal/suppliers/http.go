package suppliers

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

// Routes registers the supplier API. Every route needs an admin session and
// the named permission. Contacts and payment terms need suppliers.read;
// prices need supplier_products.read.
func (h *Handler) Routes(mux httpx.Router) {
	adm := h.Identity.RequireAdmin
	mux.Handle("GET /api/v1/admin/suppliers", adm(identity.PermSuppliersRead, h.list))
	mux.Handle("POST /api/v1/admin/suppliers", adm(identity.PermSuppliersManage, h.create))
	mux.Handle("GET /api/v1/admin/suppliers/{id}", adm(identity.PermSuppliersRead, h.get))
	mux.Handle("PATCH /api/v1/admin/suppliers/{id}", adm(identity.PermSuppliersManage, h.update))
	mux.Handle("POST /api/v1/admin/suppliers/{id}/submit", adm(identity.PermSuppliersManage, h.submit))
	mux.Handle("POST /api/v1/admin/suppliers/{id}/review", adm(identity.PermSuppliersApprove, h.review))
	mux.Handle("POST /api/v1/admin/suppliers/{id}/suspend", adm(identity.PermSuppliersApprove, h.suspend))
	mux.Handle("POST /api/v1/admin/suppliers/{id}/reactivate", adm(identity.PermSuppliersApprove, h.reactivate))
	mux.Handle("POST /api/v1/admin/suppliers/{id}/deactivate", adm(identity.PermSuppliersApprove, h.deactivate))
	mux.Handle("PUT /api/v1/admin/suppliers/{id}/farm", adm(identity.PermSuppliersManage, h.farm))
	mux.Handle("POST /api/v1/admin/suppliers/{id}/documents", adm(identity.PermSuppliersManage, h.addDocument))
	mux.Handle("POST /api/v1/admin/supplier-documents/{id}/verify", adm(identity.PermSuppliersApprove, h.verifyDocument))
	mux.Handle("POST /api/v1/admin/suppliers/{id}/communications", adm(identity.PermSuppliersManage, h.communicate))
	mux.Handle("GET /api/v1/admin/supplier-criteria", adm(identity.PermSuppliersRead, h.criteria))
	mux.Handle("POST /api/v1/admin/supplier-criteria", adm(identity.PermProcurementConfigure, h.createCriterion))
	mux.Handle("PATCH /api/v1/admin/supplier-criteria/{id}", adm(identity.PermProcurementConfigure, h.updateCriterion))

	mux.Handle("GET /api/v1/admin/supplier-products", adm(identity.PermSupplierProductsRead, h.products))
	mux.Handle("POST /api/v1/admin/suppliers/{id}/products", adm(identity.PermSupplierProductsManage, h.createProduct))
	mux.Handle("GET /api/v1/admin/supplier-products/{id}", adm(identity.PermSupplierProductsRead, h.product))
	mux.Handle("PATCH /api/v1/admin/supplier-products/{id}", adm(identity.PermSupplierProductsManage, h.updateProduct))
	mux.Handle("POST /api/v1/admin/supplier-products/{id}/mapping", adm(identity.PermSupplierProductsManage, h.mapping))
	mux.Handle("POST /api/v1/admin/supplier-products/{id}/prices", adm(identity.PermSupplierProductsManage, h.addPrice))
	mux.Handle("GET /api/v1/admin/supplier-availability", adm(identity.PermSupplierProductsRead, h.availability))
	mux.Handle("POST /api/v1/admin/supplier-availability", adm(identity.PermSupplierProductsManage, h.recordAvailability))
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
	if err := httpx.Decode(w, r, &v, 256<<10); err != nil {
		httpx.Fail(w, r, err)
		return v, false
	}
	return v, true
}

func id(r *http.Request) string { return strings.ToLower(r.PathValue("id")) }

type reasonInput struct {
	Reason string `json:"reason"`
	Note   string `json:"note"`
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("page_size"))
	p, err := h.S.List(r.Context(), Filter{Query: q.Get("q"), Type: q.Get("type"), Status: q.Get("status"), Farmers: q.Get("farmers") == "true", Page: page, Size: size})
	respond(w, r, http.StatusOK, p, err)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[Input](w, r)
	if !ok {
		return
	}
	x, err := h.S.Create(r.Context(), actor(r), in)
	respond(w, r, http.StatusCreated, x, err)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.Get(r.Context(), id(r))
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[Input](w, r)
	if !ok {
		return
	}
	x, err := h.S.Update(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) submit(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[reasonInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.Submit(r.Context(), actor(r), id(r), in.Note)
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) review(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[ReviewInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.Review(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) suspend(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[reasonInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.Suspend(r.Context(), actor(r), id(r), in.Reason)
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) reactivate(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[reasonInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.Reactivate(r.Context(), actor(r), id(r), in.Reason)
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) deactivate(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[reasonInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.Deactivate(r.Context(), actor(r), id(r), in.Reason)
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) farm(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[FarmInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.SaveFarm(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) addDocument(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[DocumentInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.AddDocument(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusCreated, x, err)
}

func (h *Handler) verifyDocument(w http.ResponseWriter, r *http.Request) {
	x, err := h.S.VerifyDocument(r.Context(), actor(r), id(r))
	respond(w, r, http.StatusOK, x, err)
}

func (h *Handler) communicate(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[CommunicationInput](w, r)
	if !ok {
		return
	}
	x, err := h.S.AddCommunication(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusCreated, x, err)
}

func (h *Handler) criteria(w http.ResponseWriter, r *http.Request) {
	c, err := h.S.Criteria(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": c}, err)
}

func (h *Handler) createCriterion(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[CriterionInput](w, r)
	if !ok {
		return
	}
	c, err := h.S.SaveCriterion(r.Context(), actor(r), "", in)
	respond(w, r, http.StatusCreated, map[string]any{"items": c}, err)
}

func (h *Handler) updateCriterion(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[CriterionInput](w, r)
	if !ok {
		return
	}
	c, err := h.S.SaveCriterion(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusOK, map[string]any{"items": c}, err)
}

func (h *Handler) products(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p, err := h.S.Products(r.Context(), ProductFilter{SupplierID: q.Get("supplier_id"), IngredientID: q.Get("ingredient_id"),
		Mapping: q.Get("mapping"), Query: q.Get("q"), Purchasable: q.Get("purchasable") == "true"})
	respond(w, r, http.StatusOK, map[string]any{"items": p}, err)
}

func (h *Handler) createProduct(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[ProductInput](w, r)
	if !ok {
		return
	}
	p, err := h.S.CreateProduct(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusCreated, p, err)
}

func (h *Handler) product(w http.ResponseWriter, r *http.Request) {
	p, err := h.S.Product(r.Context(), id(r))
	respond(w, r, http.StatusOK, p, err)
}

func (h *Handler) updateProduct(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[ProductInput](w, r)
	if !ok {
		return
	}
	p, err := h.S.UpdateProduct(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusOK, p, err)
}

func (h *Handler) mapping(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[MappingInput](w, r)
	if !ok {
		return
	}
	p, err := h.S.ReviewMapping(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusOK, p, err)
}

func (h *Handler) addPrice(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[PriceInput](w, r)
	if !ok {
		return
	}
	p, err := h.S.AddPrice(r.Context(), actor(r), id(r), in)
	respond(w, r, http.StatusCreated, p, err)
}

func (h *Handler) availability(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	a, err := h.S.Availability(r.Context(), AvailabilityFilter{SupplierID: q.Get("supplier_id"), IngredientID: q.Get("ingredient_id"), Current: q.Get("current") != "false"})
	respond(w, r, http.StatusOK, map[string]any{"items": a}, err)
}

func (h *Handler) recordAvailability(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[AvailabilityInput](w, r)
	if !ok {
		return
	}
	a, err := h.S.RecordAvailability(r.Context(), actor(r), in)
	respond(w, r, http.StatusCreated, map[string]any{"items": a}, err)
}
