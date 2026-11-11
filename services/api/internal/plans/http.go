package plans

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/plans/store"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type Handler struct {
	Service  *Service
	Identity *identity.Service
}

func (h *Handler) Routes(mux httpx.Router) {
	// Public: plans and prices are the same for everyone.
	mux.Handle("GET /api/v1/plans", http.HandlerFunc(h.list))
	mux.Handle("GET /api/v1/plans/{slug}", http.HandlerFunc(h.detail))

	adm := h.Identity.RequireAdmin
	mux.Handle("GET /api/v1/admin/plans", adm(identity.PermSubscriptionsRead, h.adminList))
	mux.Handle("POST /api/v1/admin/plans", adm(identity.PermPlansManage, h.adminCreate))
	mux.Handle("GET /api/v1/admin/plans/{id}", adm(identity.PermSubscriptionsRead, h.adminGet))
	mux.Handle("PUT /api/v1/admin/plans/{id}", adm(identity.PermPlansManage, h.adminUpdate))
	mux.Handle("POST /api/v1/admin/plans/{id}/prices", adm(identity.PermPlansManage, h.adminPrice))
	mux.Handle("POST /api/v1/admin/plans/{id}/activate", adm(identity.PermPlansManage, h.adminStatus("active")))
	mux.Handle("POST /api/v1/admin/plans/{id}/retire", adm(identity.PermPlansManage, h.adminStatus("retired")))
}

func respond[T any](w http.ResponseWriter, r *http.Request, status int, v T, err error) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, status, v)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	list, err := h.Service.Public(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

func (h *Handler) detail(w http.ResponseWriter, r *http.Request) {
	p, err := h.Service.PublicBySlug(r.Context(), r.PathValue("slug"))
	respond(w, r, http.StatusOK, p, err)
}

func actor(r *http.Request) string { return identity.PrincipalFrom(r.Context()).UserID }

func pathID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if !uuidPattern.MatchString(id) {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return "", false
	}
	return id, true
}

func (h *Handler) adminList(w http.ResponseWriter, r *http.Request) {
	list, err := h.Service.AdminList(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

func (h *Handler) adminGet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := h.Service.AdminGet(r.Context(), id)
	respond(w, r, http.StatusOK, p, err)
}

func (h *Handler) adminCreate(w http.ResponseWriter, r *http.Request) {
	var in PlanInput
	if err := httpx.Decode(w, r, &in, 16<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	p, err := h.Service.CreatePlan(r.Context(), actor(r), in, false)
	respond(w, r, http.StatusCreated, p, err)
}

func (h *Handler) adminUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in PlanInput
	if err := httpx.Decode(w, r, &in, 16<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	p, err := h.Service.UpdatePlan(r.Context(), actor(r), id, in)
	respond(w, r, http.StatusOK, p, err)
}

func (h *Handler) adminPrice(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in PriceInput
	if err := httpx.Decode(w, r, &in, 4<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	p, err := h.Service.AddPrice(r.Context(), actor(r), id, in)
	respond(w, r, http.StatusCreated, p, err)
}

func (h *Handler) adminStatus(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		p, err := h.Service.SetStatus(r.Context(), actor(r), id, status)
		respond(w, r, http.StatusOK, p, err)
	}
}

// ---------------------------------------------------------------- demo seed

// demoPlans are the draft figures from the business plan. They are not
// approved prices: they are loaded only as demo content, which is hidden and
// cannot be activated unless CATALOG_DEMO_ENABLED is on.
var demoPlans = []struct {
	in    PlanInput
	price int64
}{
	{PlanInput{Slug: "basic", Name: "Basic", MealsPerWeek: 3, Segment: "everyday", SelfServe: true, SortOrder: 1,
		Summary:  "Three dinners a week for one household. A good place to start.",
		Benefits: []string{"Three chef-designed meal kits each week", "Choose your meals before each Thursday's cutoff", "Pause or cancel from your account"}}, 33000},
	{PlanInput{Slug: "fitness", Name: "Fitness", MealsPerWeek: 4, Segment: "fitness", SelfServe: true, SortOrder: 2,
		Summary:  "Four protein-forward meals a week, built around training days.",
		Benefits: []string{"Four meal kits each week", "Meals marked high protein where verified", "Pause or cancel from your account"}}, 40000},
	{PlanInput{Slug: "premium-chef", Name: "Premium Chef", MealsPerWeek: 4, Segment: "premium", SelfServe: true, SortOrder: 3,
		Summary:  "Four of our most ambitious dishes each week, for cooks who like a project.",
		Benefits: []string{"Four premium meal kits each week", "Chef's notes with every kit", "Pause or cancel from your account"}}, 55000},
	{PlanInput{Slug: "school-lunch", Name: "School Lunch", MealsPerWeek: 5, Segment: "school", SelfServe: false, SortOrder: 4,
		Summary:  "Five school-day lunches. Arranged with our team while eligibility is confirmed.",
		Benefits: []string{"Five lunch kits each week"}}, 30000},
	{PlanInput{Slug: "corporate-wellness", Name: "Corporate Wellness", MealsPerWeek: 5, Segment: "corporate", SelfServe: false, SortOrder: 5,
		Summary:  "Five meals a week for teams. Arranged with your employer.",
		Benefits: []string{"Five meal kits each week"}}, 48000},
}

// SeedDemo loads the business-plan draft plans as demo content. It is
// idempotent: plans that already exist (by slug) are left alone.
func (s *Service) SeedDemo(ctx context.Context) (int, error) {
	created := 0
	for _, d := range demoPlans {
		_, err := store.New(s.pool).PlanIDBySlug(ctx, d.in.Slug)
		if err == nil {
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return created, err
		}
		in := d.in
		if f := in.clean(); len(f) > 0 {
			return created, httpx.ValidationError(f)
		}
		err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
			q := store.New(tx)
			id, err := q.CreatePlan(ctx, store.CreatePlanParams{
				Slug: in.Slug, Name: in.Name, Summary: in.Summary, Description: in.Description,
				MealsPerWeek: int16(in.MealsPerWeek), Segment: in.Segment, Benefits: in.Benefits, SelfServe: in.SelfServe,
				SortOrder: int16(in.SortOrder), IsDemo: true,
			})
			if err != nil {
				return err
			}
			if _, err := q.InsertPrice(ctx, store.InsertPriceParams{PlanID: id, AmountMinor: d.price, Currency: "TTD",
				EffectiveFrom: s.Now().Add(-time.Second), Note: ptr("Business-plan draft figure (demo)")}); err != nil {
				return err
			}
			_, err = q.SetPlanStatus(ctx, store.SetPlanStatusParams{ID: id, Status: "active"})
			return err
		})
		if err != nil {
			return created, err
		}
		created++
	}
	return created, nil
}

func ptr[T any](v T) *T { return &v }
