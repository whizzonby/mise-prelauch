package commerce

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"mise.tt/api/internal/billing"
	"mise.tt/api/internal/customers"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/menus"
	"mise.tt/api/internal/orders"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/subscriptions"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type Handler struct {
	S *Service
	// Limit applies a per-client budget to the money-moving endpoints.
	Limit func(http.Handler) http.Handler
}

func (h *Handler) Routes(mux httpx.Router) {
	limit := h.Limit
	if limit == nil {
		limit = func(next http.Handler) http.Handler { return next }
	}
	req := h.S.Identity.RequireCustomer
	mux.Handle("GET /api/v1/subscription-info", http.HandlerFunc(h.info))

	mux.Handle("GET /api/v1/me/subscription", req(h.current))
	mux.Handle("POST /api/v1/me/subscriptions/quote", req(h.quote))
	mux.Handle("POST /api/v1/me/subscriptions", limit(req(h.subscribe)))
	mux.Handle("POST /api/v1/me/subscriptions/{id}/change-plan", req(h.changePlan))
	mux.Handle("POST /api/v1/me/subscriptions/{id}/pause", req(h.pause))
	mux.Handle("POST /api/v1/me/subscriptions/{id}/resume", req(h.resume))
	mux.Handle("POST /api/v1/me/subscriptions/{id}/cancel", req(h.cancel))
	mux.Handle("PUT /api/v1/me/subscriptions/{id}/address", req(h.address))
	mux.Handle("GET /api/v1/me/subscription-cycles", req(h.weeks))
	mux.Handle("GET /api/v1/me/meal-selections/{cycleId}", req(h.selection))
	mux.Handle("PUT /api/v1/me/meal-selections/{cycleId}", req(h.saveSelection))
	mux.Handle("POST /api/v1/me/meal-selections/{cycleId}/finalize", req(h.finalizeSelection))
	mux.Handle("GET /api/v1/me/orders", req(h.orders))
	mux.Handle("GET /api/v1/me/orders/{id}", req(h.order))
	mux.Handle("GET /api/v1/me/orders/{id}/meals/{mealId}/recipe", req(h.recipe))
	mux.Handle("GET /api/v1/me/billing", req(h.billing))
	mux.Handle("GET /api/v1/me/invoices", req(h.invoices))
	mux.Handle("GET /api/v1/me/invoices/{id}", req(h.invoice))
	mux.Handle("GET /api/v1/me/invoices/{id}/pdf", req(h.invoicePDF))
	mux.Handle("POST /api/v1/checkout/sessions", limit(req(h.pay)))
	mux.Handle("GET /api/v1/checkout/sessions/{id}", req(h.checkout))

	adm := h.S.Identity.RequireAdmin
	mux.Handle("GET /api/v1/admin/subscriptions", adm(identity.PermSubscriptionsRead, h.adminSubscriptions))
	mux.Handle("GET /api/v1/admin/subscriptions/{id}", adm(identity.PermSubscriptionsRead, h.adminSubscription))
	mux.Handle("GET /api/v1/admin/orders", adm(identity.PermOrdersRead, h.adminOrders))
	mux.Handle("GET /api/v1/admin/orders/{id}", adm(identity.PermOrdersRead, h.adminOrder))
	mux.Handle("GET /api/v1/admin/demand", adm(identity.PermOrdersRead, h.adminDemand))
	mux.Handle("GET /api/v1/admin/invoices/{id}", adm(identity.PermPaymentsRead, h.adminInvoice))
	mux.Handle("GET /api/v1/admin/promotions", adm(identity.PermPromotionsManage, h.adminPromotions))
	mux.Handle("POST /api/v1/admin/promotions", adm(identity.PermPromotionsManage, h.adminCreatePromotion))
	mux.Handle("POST /api/v1/admin/promotions/{id}/activate", adm(identity.PermPromotionsManage, h.adminPromotionStatus("active")))
	mux.Handle("POST /api/v1/admin/promotions/{id}/archive", adm(identity.PermPromotionsManage, h.adminPromotionStatus("archived")))
	mux.Handle("GET /api/v1/admin/commerce/jobs", adm(identity.PermPaymentsRead, h.adminJobs))
}

func respond[T any](w http.ResponseWriter, r *http.Request, status int, v T, err error) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, status, v)
}

func principal(r *http.Request) *identity.Principal { return identity.PrincipalFrom(r.Context()) }

func pathUUID(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	id := strings.ToLower(r.PathValue(name))
	if !uuidPattern.MatchString(id) {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return "", false
	}
	return id, true
}

func decode[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var v T
	if err := httpx.Decode(w, r, &v, 16<<10); err != nil {
		httpx.Fail(w, r, err)
		return v, false
	}
	return v, true
}

func pageParams(r *http.Request) (int, int) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	return page, size
}

// ---------------------------------------------------------------- public

// Info is what the apps need to describe subscribing before sign-in.
type Info struct {
	Payments      PaymentsInfo `json:"payments"`
	NextOpenWeek  string       `json:"next_open_week"`
	CutoffRule    string       `json:"cutoff_rule"`
	TimeZone      string       `json:"time_zone"`
	MaxPauseWeeks int          `json:"max_pause_weeks"`
	CancelReasons []string     `json:"cancel_reasons"`
}

func (h *Handler) info(w http.ResponseWriter, r *http.Request) {
	p := h.S.Menus.Policy
	cutoff := time.Date(2026, 1, 1, p.CutoffHour, p.CutoffMinute, 0, 0, time.UTC)
	httpx.JSON(w, r, http.StatusOK, Info{
		Payments: h.S.paymentsInfo(), NextOpenWeek: menus.FormatWeek(p.NextOpenWeek(h.S.Now())),
		CutoffRule: "Choose your meals by " + p.CutoffWeekday.String() + " at " + cutoff.Format("3:04 pm") + " for the following week.",
		TimeZone:   menus.TimeZone, MaxPauseWeeks: h.S.Subs.Opts.MaxPauseWeeks, CancelReasons: CancelReasons,
	})
}

// ---------------------------------------------------------------- subscription

func (h *Handler) current(w http.ResponseWriter, r *http.Request) {
	v, err := h.S.Current(r.Context(), principal(r))
	respond(w, r, http.StatusOK, map[string]any{"subscription": v}, err)
}

func (h *Handler) quote(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[QuoteInput](w, r)
	if !ok {
		return
	}
	q, err := h.S.Quote(r.Context(), principal(r), in)
	respond(w, r, http.StatusOK, q, err)
}

func (h *Handler) subscribe(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[SubscribeInput](w, r)
	if !ok {
		return
	}
	res, err := h.S.Subscribe(r.Context(), principal(r), in)
	respond(w, r, http.StatusCreated, res, err)
}

func (h *Handler) manage(w http.ResponseWriter, r *http.Request, fn func(id string) (*SubscriptionView, error)) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	v, err := fn(id)
	respond(w, r, http.StatusOK, map[string]any{"subscription": v}, err)
}

func (h *Handler) changePlan(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[ChangePlanInput](w, r)
	if !ok {
		return
	}
	h.manage(w, r, func(id string) (*SubscriptionView, error) { return h.S.ChangePlan(r.Context(), principal(r), id, in) })
}

func (h *Handler) pause(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[PauseInput](w, r)
	if !ok {
		return
	}
	h.manage(w, r, func(id string) (*SubscriptionView, error) { return h.S.Pause(r.Context(), principal(r), id, in) })
}

func (h *Handler) resume(w http.ResponseWriter, r *http.Request) {
	h.manage(w, r, func(id string) (*SubscriptionView, error) { return h.S.Resume(r.Context(), principal(r), id) })
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	var in CancelInput
	if r.ContentLength > 0 {
		var ok bool
		if in, ok = decode[CancelInput](w, r); !ok {
			return
		}
	}
	h.manage(w, r, func(id string) (*SubscriptionView, error) { return h.S.Cancel(r.Context(), principal(r), id, in) })
}

func (h *Handler) address(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[AddressInput](w, r)
	if !ok {
		return
	}
	h.manage(w, r, func(id string) (*SubscriptionView, error) { return h.S.SetAddress(r.Context(), principal(r), id, in) })
}

func (h *Handler) weeks(w http.ResponseWriter, r *http.Request) {
	list, err := h.S.Weeks(r.Context(), principal(r))
	respond(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

// ---------------------------------------------------------------- selection

func (h *Handler) selection(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "cycleId")
	if !ok {
		return
	}
	p, err := h.S.SelectionPage(r.Context(), principal(r), id)
	respond(w, r, http.StatusOK, p, err)
}

func (h *Handler) saveSelection(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "cycleId")
	if !ok {
		return
	}
	in, ok := decode[SaveSelectionInput](w, r)
	if !ok {
		return
	}
	p, err := h.S.SaveSelection(r.Context(), principal(r), id, in)
	respond(w, r, http.StatusOK, p, err)
}

func (h *Handler) finalizeSelection(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "cycleId")
	if !ok {
		return
	}
	var in SaveSelectionInput
	if r.ContentLength > 0 {
		if in, ok = decode[SaveSelectionInput](w, r); !ok {
			return
		}
	}
	p, err := h.S.FinalizeSelection(r.Context(), principal(r), id, in)
	respond(w, r, http.StatusOK, p, err)
}

// ---------------------------------------------------------------- orders and billing

func (h *Handler) customer(w http.ResponseWriter, r *http.Request) (string, bool) {
	cid, err := h.S.customerID(r.Context(), principal(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return "", false
	}
	return cid, true
}

func (h *Handler) orders(w http.ResponseWriter, r *http.Request) {
	cid, ok := h.customer(w, r)
	if !ok {
		return
	}
	page, size := pageParams(r)
	list, err := h.S.Orders.ListForCustomer(r.Context(), cid, page, size)
	respond(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

func (h *Handler) order(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	cid, ok := h.customer(w, r)
	if !ok {
		return
	}
	o, err := h.S.Orders.ForCustomer(r.Context(), cid, id)
	respond(w, r, http.StatusOK, o, err)
}

// BillingSummary is the customer's billing page.
type BillingSummary struct {
	PaymentMethod *string         `json:"payment_method"`
	AutoRenew     bool            `json:"auto_renew"`
	Outstanding   *CycleView      `json:"outstanding"`
	History       []billing.Cycle `json:"history"`
	Terms         *string         `json:"terms"`
	TestPayments  bool            `json:"test_payments"`
}

func (h *Handler) billing(w http.ResponseWriter, r *http.Request) {
	cid, ok := h.customer(w, r)
	if !ok {
		return
	}
	out := BillingSummary{History: []billing.Cycle{}}
	sub, err := h.S.Current(r.Context(), principal(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if sub != nil {
		out.PaymentMethod, out.AutoRenew, out.Outstanding, out.Terms, out.TestPayments = sub.PaymentMethod, sub.AutoRenew, sub.Outstanding, &sub.Terms, sub.Test
	}
	out.History, err = h.S.Billing.ForCustomer(r.Context(), cid)
	respond(w, r, http.StatusOK, out, err)
}

func (h *Handler) invoices(w http.ResponseWriter, r *http.Request) {
	cid, ok := h.customer(w, r)
	if !ok {
		return
	}
	page, size := pageParams(r)
	list, err := h.S.Billing.InvoicesForCustomer(r.Context(), cid, page, size)
	respond(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

func (h *Handler) invoice(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	cid, ok := h.customer(w, r)
	if !ok {
		return
	}
	inv, err := h.S.Billing.InvoiceForCustomer(r.Context(), cid, id)
	respond(w, r, http.StatusOK, inv, err)
}

func writePDF(w http.ResponseWriter, inv *billing.Invoice) {
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+inv.Number+`.pdf"`)
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(billing.InvoicePDF(inv))
}

func (h *Handler) invoicePDF(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	cid, ok := h.customer(w, r)
	if !ok {
		return
	}
	inv, err := h.S.Billing.InvoiceForCustomer(r.Context(), cid, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	writePDF(w, inv)
}

func (h *Handler) pay(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[PayInput](w, r)
	if !ok {
		return
	}
	c, err := h.S.Pay(r.Context(), principal(r), in)
	respond(w, r, http.StatusCreated, c, err)
}

func (h *Handler) checkout(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	res, err := h.S.CheckoutResult(r.Context(), principal(r), id)
	respond(w, r, http.StatusOK, res, err)
}

// ---------------------------------------------------------------- admin

// AdminSubscription is a subscription as staff see it.
type AdminSubscription struct {
	SubscriptionView
	CustomerID    string                       `json:"customer_id"`
	CustomerName  string                       `json:"customer_name"`
	CustomerEmail string                       `json:"customer_email"`
	CreatedAt     time.Time                    `json:"created_at"`
	History       []subscriptions.HistoryEntry `json:"history"`
	Billing       []billing.Cycle              `json:"billing"`
	AllCycles     []CycleView                  `json:"all_cycles"`
}

type AdminSubscriptionRow struct {
	ID          string     `json:"id"`
	CustomerID  string     `json:"customer_id"`
	Status      string     `json:"status"`
	PlanName    string     `json:"plan_name"`
	AmountMinor int64      `json:"amount_minor"`
	Currency    string     `json:"currency"`
	FirstWeek   string     `json:"first_week"`
	ActivatedAt *time.Time `json:"activated_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

func (h *Handler) adminSubscriptions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cust := ""
	if c := q.Get("customer_id"); uuidPattern.MatchString(c) {
		cust = c
	}
	page, size := pageParams(r)
	rows, err := h.S.Subs.List(r.Context(), q.Get("status"), cust, page, size)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out := []AdminSubscriptionRow{}
	for _, s := range rows {
		out = append(out, AdminSubscriptionRow{ID: s.ID, CustomerID: s.CustomerID, Status: s.Status, PlanName: s.PlanName,
			AmountMinor: s.AmountMinor, Currency: s.Currency, FirstWeek: menus.FormatWeek(s.FirstWeek), ActivatedAt: s.ActivatedAt,
			CreatedAt: s.CreatedAt})
	}
	httpx.JSON(w, r, http.StatusOK, map[string]any{"items": out})
}

func (h *Handler) adminSubscription(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	sub, err := h.S.Subs.Get(ctx, h.S.pool, id, false)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	view, err := h.S.subscriptionView(ctx, h.S.pool, sub)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out := AdminSubscription{SubscriptionView: *view, CustomerID: sub.CustomerID, CreatedAt: sub.CreatedAt, AllCycles: []CycleView{}}
	h.S.fillContact(ctx, sub.CustomerID, &out.CustomerName, &out.CustomerEmail)
	if out.History, err = h.S.Subs.History(ctx, id); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if out.Billing, err = h.S.Billing.ForSubscription(ctx, id); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	cycles, err := h.S.Subs.CyclesOfSubscription(ctx, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	for i := range cycles {
		cv, err := h.S.cycleView(ctx, h.S.pool, &cycles[i].Cycle, cycles[i].PlanName)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		out.AllCycles = append(out.AllCycles, *cv)
	}
	httpx.JSON(w, r, http.StatusOK, out)
}

func (s *Service) fillContact(ctx context.Context, customerID string, name, email *string) {
	uid, err := customers.UserIDOf(ctx, s.pool, customerID)
	if err != nil {
		return
	}
	if c, err := s.Identity.ContactOf(ctx, uid); err == nil {
		*name, *email = strings.TrimSpace(c.FirstName+" "+c.LastName), c.Email
	}
}

func (h *Handler) adminOrders(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := orders.Filter{Status: q.Get("status")}
	if wk, ok := menus.ParseWeek(q.Get("week")); ok {
		f.Week = &wk
	}
	if c := q.Get("customer_id"); uuidPattern.MatchString(c) {
		f.CustomerID = c
	}
	list, err := h.S.Orders.List(r.Context(), f)
	respond(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

func (h *Handler) adminOrder(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	o, err := h.S.Orders.Get(r.Context(), id)
	respond(w, r, http.StatusOK, o, err)
}

func (h *Handler) adminDemand(w http.ResponseWriter, r *http.Request) {
	week, ok := menus.ParseWeek(r.URL.Query().Get("week"))
	if !ok {
		week = h.S.Menus.Policy.NextOpenWeek(h.S.Now()).AddDate(0, 0, -7)
	}
	// Final once the week's cutoff has passed and every week is settled.
	final := !h.S.Now().Before(h.S.Menus.Policy.CutoffFor(week))
	d, err := h.S.Orders.Demand(r.Context(), week, final)
	respond(w, r, http.StatusOK, d, err)
}

func (h *Handler) adminInvoice(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	inv, err := h.S.Billing.Invoice(r.Context(), id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if r.URL.Query().Get("format") == "pdf" {
		writePDF(w, inv)
		return
	}
	httpx.JSON(w, r, http.StatusOK, inv)
}

func (h *Handler) adminPromotions(w http.ResponseWriter, r *http.Request) {
	list, err := h.S.Billing.Promotions(r.Context())
	respond(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

func (h *Handler) adminCreatePromotion(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[billing.PromotionInput](w, r)
	if !ok {
		return
	}
	p, err := h.S.Billing.CreatePromotion(r.Context(), principal(r).UserID, in)
	respond(w, r, http.StatusCreated, p, err)
}

func (h *Handler) adminPromotionStatus(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathUUID(w, r, "id")
		if !ok {
			return
		}
		p, err := h.S.Billing.SetPromotionStatus(r.Context(), principal(r).UserID, id, status)
		respond(w, r, http.StatusOK, p, err)
	}
}

// FailedJob is a background job that gave up (staff review these).
type FailedJob struct {
	ID         int64      `json:"id"`
	Kind       string     `json:"kind"`
	Status     string     `json:"status"`
	Attempts   int        `json:"attempts"`
	LastError  *string    `json:"last_error"`
	CreatedAt  time.Time  `json:"created_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

// adminJobs lists commerce and payment jobs that failed or are retrying.
// Payloads are not shown (they may name customers).
func (h *Handler) adminJobs(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.pool.Query(r.Context(), `
		SELECT id, kind, status, attempts, last_error, created_at, finished_at FROM jobs
		WHERE (kind LIKE 'commerce.%' OR kind LIKE 'payments.%')
		  AND (status = 'failed' OR (status = 'queued' AND attempts > 0))
		ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	defer rows.Close()
	out := []FailedJob{}
	for rows.Next() {
		var j FailedJob
		if err := rows.Scan(&j.ID, &j.Kind, &j.Status, &j.Attempts, &j.LastError, &j.CreatedAt, &j.FinishedAt); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		out = append(out, j)
	}
	respond(w, r, http.StatusOK, map[string]any{"items": out}, rows.Err())
}
