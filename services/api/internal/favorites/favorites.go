// Package favorites keeps the meals a customer has saved. It stores only the
// link (customer, meal); what a customer may see of a meal is the
// catalogue's decision, so a saved meal that is later unpublished drops out
// of the list without being deleted, and returns if it is republished.
package favorites

import (
	"context"
	"net/http"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/catalog"
	"mise.tt/api/internal/favorites/store"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/httpx"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Customers resolves the signed-in customer's id.
type Customers interface {
	CustomerID(ctx context.Context, p *identity.Principal) (string, error)
}

type Handler struct {
	Pool      *pgxpool.Pool
	Identity  *identity.Service
	Customers Customers
	Catalog   *catalog.Service
}

func (h *Handler) Routes(mux httpx.Router) {
	req := h.Identity.RequireCustomer
	mux.Handle("GET /api/v1/me/favorites", req(h.list))
	mux.Handle("POST /api/v1/me/favorites", req(h.add))
	mux.Handle("DELETE /api/v1/me/favorites/{mealId}", req(h.remove))
}

func (h *Handler) customerID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id, err := h.Customers.CustomerID(r.Context(), identity.PrincipalFrom(r.Context()))
	if err != nil {
		httpx.Fail(w, r, err)
		return "", false
	}
	return id, true
}

// List is the saved meals a customer can currently see, most recent first,
// and the ids of all of them (for marking cards as saved).
type List struct {
	Items   []catalog.MealCard `json:"items"`
	MealIDs []string           `json:"meal_ids"`
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	cid, ok := h.customerID(w, r)
	if !ok {
		return
	}
	ids, err := store.New(h.Pool).ListFavoriteMealIDs(r.Context(), cid)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	cards, err := h.Catalog.Cards(r.Context(), ids)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	visible := make([]string, 0, len(cards))
	for _, c := range cards {
		visible = append(visible, c.ID)
	}
	httpx.JSON(w, r, http.StatusOK, List{Items: cards, MealIDs: visible})
}

func (h *Handler) add(w http.ResponseWriter, r *http.Request) {
	var in struct {
		MealID string `json:"meal_id"`
	}
	if err := httpx.Decode(w, r, &in, 1<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	mealID := strings.ToLower(strings.TrimSpace(in.MealID))
	if !uuidPattern.MatchString(mealID) {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	// A draft or unpublished meal cannot be saved: to the customer it does
	// not exist, so the answer is the same as for an unknown id.
	visible, err := h.Catalog.Visible(r.Context(), mealID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if !visible {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	cid, ok := h.customerID(w, r)
	if !ok {
		return
	}
	n, err := store.New(h.Pool).AddFavorite(r.Context(), store.AddFavoriteParams{CustomerID: cid, MealID: mealID})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	status := http.StatusCreated
	if n == 0 {
		status = http.StatusOK // already saved; saving again changes nothing
	}
	httpx.JSON(w, r, status, map[string]any{"meal_id": mealID, "saved": true})
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	mealID := strings.ToLower(r.PathValue("mealId"))
	if !uuidPattern.MatchString(mealID) {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	cid, ok := h.customerID(w, r)
	if !ok {
		return
	}
	if _, err := store.New(h.Pool).RemoveFavorite(r.Context(), store.RemoveFavoriteParams{CustomerID: cid, MealID: mealID}); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// Removing a meal that was not saved is not an error: the end state is the same.
	httpx.JSON(w, r, http.StatusOK, map[string]any{"meal_id": mealID, "saved": false})
}
