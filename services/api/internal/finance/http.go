package finance

import (
	"net/http"

	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/httpx"
)

type Handler struct {
	S        *Service
	Identity *identity.Service
}

func (h *Handler) Routes(mux httpx.Router) {
	adm := h.Identity.RequireAdmin
	mux.Handle("GET /api/v1/admin/finance/expense-categories", adm(identity.PermFinanceExpensesRead, h.categories))
	mux.Handle("GET /api/v1/admin/finance/expenses", adm(identity.PermFinanceExpensesRead, h.expenses))
	mux.Handle("POST /api/v1/admin/finance/expenses", adm(identity.PermFinanceExpensesManage, h.createExpense))
	mux.Handle("GET /api/v1/admin/finance/expenses/{id}", adm(identity.PermFinanceExpensesRead, h.expense))
	mux.Handle("PATCH /api/v1/admin/finance/expenses/{id}", adm(identity.PermFinanceExpensesManage, h.updateExpense))
	mux.Handle("POST /api/v1/admin/finance/expenses/{id}/{action}", adm(identity.PermFinanceExpensesManage, h.transition))
	mux.Handle("GET /api/v1/admin/finance/recurring-expenses", adm(identity.PermFinanceExpensesRead, h.recurring))
	mux.Handle("POST /api/v1/admin/finance/recurring-expenses", adm(identity.PermFinanceExpensesManage, h.createRecurring))
	mux.Handle("POST /api/v1/admin/finance/recurring-expenses/{id}/{action}", adm(identity.PermFinanceExpensesManage, h.recurringStatus))
	mux.Handle("PUT /api/v1/admin/finance/coverage", adm(identity.PermFinanceExpensesManage, h.coverage))
	mux.Handle("GET /api/v1/admin/finance/cash-balances", adm(identity.PermFinanceExpensesRead, h.cashBalances))
	mux.Handle("POST /api/v1/admin/finance/cash-balances", adm(identity.PermFinanceExpensesApprove, h.recordCashBalance))
	mux.Handle("GET /api/v1/admin/finance/summary", adm(identity.PermFinanceExpensesRead, h.summary))
}

func reply(w http.ResponseWriter, r *http.Request, status int, v any, err error) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, status, v)
}

func decode[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var in T
	if err := httpx.Decode(w, r, &in, 16<<10); err != nil {
		httpx.Fail(w, r, err)
		return in, false
	}
	return in, true
}

func principal(r *http.Request) *identity.Principal { return identity.PrincipalFrom(r.Context()) }

func (h *Handler) categories(w http.ResponseWriter, r *http.Request) {
	c, err := h.S.Categories(r.Context())
	reply(w, r, http.StatusOK, map[string]any{"items": c}, err)
}

func (h *Handler) expenses(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, err := h.S.Expenses(r.Context(), ExpenseFilter{Status: q.Get("status"), Category: q.Get("category"), From: q.Get("from"), To: q.Get("to")})
	reply(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

func (h *Handler) createExpense(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[ExpenseInput](w, r)
	if !ok {
		return
	}
	e, err := h.S.CreateExpense(r.Context(), principal(r), in)
	reply(w, r, http.StatusCreated, e, err)
}

func (h *Handler) expense(w http.ResponseWriter, r *http.Request) {
	e, err := h.S.Expense(r.Context(), r.PathValue("id"))
	reply(w, r, http.StatusOK, e, err)
}

func (h *Handler) updateExpense(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[ExpenseInput](w, r)
	if !ok {
		return
	}
	e, err := h.S.UpdateExpense(r.Context(), principal(r), r.PathValue("id"), in)
	reply(w, r, http.StatusOK, e, err)
}

type noteInput struct {
	Note string `json:"note"`
}

func readNote(w http.ResponseWriter, r *http.Request) (string, bool) {
	var in noteInput
	if r.ContentLength != 0 {
		if err := httpx.Decode(w, r, &in, 4<<10); err != nil {
			httpx.Fail(w, r, err)
			return "", false
		}
	}
	return in.Note, true
}

func (h *Handler) transition(w http.ResponseWriter, r *http.Request) {
	note, ok := readNote(w, r)
	if !ok {
		return
	}
	e, err := h.S.Transition(r.Context(), principal(r), r.PathValue("id"), r.PathValue("action"), note)
	reply(w, r, http.StatusOK, e, err)
}

func (h *Handler) recurring(w http.ResponseWriter, r *http.Request) {
	list, err := h.S.RecurringList(r.Context())
	reply(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

func (h *Handler) createRecurring(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[RecurringInput](w, r)
	if !ok {
		return
	}
	rc, err := h.S.CreateRecurring(r.Context(), principal(r), in)
	reply(w, r, http.StatusCreated, rc, err)
}

func (h *Handler) recurringStatus(w http.ResponseWriter, r *http.Request) {
	note, ok := readNote(w, r)
	if !ok {
		return
	}
	rc, err := h.S.SetRecurringStatus(r.Context(), principal(r), r.PathValue("id"), r.PathValue("action"), note)
	reply(w, r, http.StatusOK, rc, err)
}

type coverageInput struct {
	Category  string `json:"category"`
	Month     string `json:"month"`
	Note      string `json:"note"`
	Confirmed bool   `json:"confirmed"`
}

func (h *Handler) coverage(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[coverageInput](w, r)
	if !ok {
		return
	}
	err := h.S.ConfirmCoverage(r.Context(), principal(r), in.Category, in.Month, in.Note, !in.Confirmed)
	reply(w, r, http.StatusOK, map[string]any{"ok": true}, err)
}

func (h *Handler) cashBalances(w http.ResponseWriter, r *http.Request) {
	list, err := h.S.CashBalances(r.Context())
	reply(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

func (h *Handler) recordCashBalance(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[CashBalanceInput](w, r)
	if !ok {
		return
	}
	b, err := h.S.RecordCashBalance(r.Context(), principal(r), in)
	reply(w, r, http.StatusCreated, b, err)
}

func (h *Handler) summary(w http.ResponseWriter, r *http.Request) {
	s, err := h.S.Summary(r.Context(), r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	reply(w, r, http.StatusOK, s, err)
}
