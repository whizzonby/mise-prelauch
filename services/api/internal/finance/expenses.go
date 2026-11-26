// Package finance is Mise's lightweight management finance: operating
// expenses the platform did not otherwise hold (with approval and an audit
// trail), recurring costs, coverage confirmations and recorded cash
// balances, plus deterministic summaries and scenarios built on the
// authoritative operational records.
//
// It is not an accounting system and produces no audited statements.
// Collections, refunds, provider fees, ingredient and delivery costs are
// read from their own tables (docs/finance/FINANCIAL_METRIC_DEFINITIONS.md)
// and are never re-entered here, so nothing is counted twice. A category
// with nothing recorded is missing, never zero.
package finance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
)

// Currency is the management currency. Records in other currencies are
// reported as excluded, never converted.
const Currency = "TTD"

type Service struct {
	pool *pgxpool.Pool
	Now  func() time.Time
	Loc  *time.Location
}

func NewService(pool *pgxpool.Pool) *Service {
	loc, err := time.LoadLocation("America/Port_of_Spain")
	if err != nil {
		loc = time.UTC
	}
	return &Service{pool: pool, Now: time.Now, Loc: loc}
}

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Category is an expense category.
type Category struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	CostClass   string `json:"cost_class"`
	Description string `json:"description"`
}

func (s *Service) Categories(ctx context.Context) ([]Category, error) {
	rows, err := s.pool.Query(ctx, `SELECT key, name, cost_class, description FROM finance_expense_categories ORDER BY position, key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Category{}
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.Key, &c.Name, &c.CostClass, &c.Description); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Expense is a management expense record.
type Expense struct {
	ID          string     `json:"id"`
	Category    string     `json:"category"`
	Description string     `json:"description"`
	AmountMinor int64      `json:"amount_minor"`
	Currency    string     `json:"currency"`
	Kind        string     `json:"kind"`
	ExpenseDate string     `json:"expense_date"`
	PaymentDate *string    `json:"payment_date"`
	Reference   string     `json:"reference"`
	Status      string     `json:"status"`
	RecurringID *string    `json:"recurring_id"`
	CreatedBy   *string    `json:"created_by"`
	ApprovedBy  *string    `json:"approved_by"`
	ApprovedAt  *time.Time `json:"approved_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	History     []Event    `json:"history,omitempty"`
}

// Event is one audited change to an expense or recurring cost.
type Event struct {
	Action    string          `json:"action"`
	From      string          `json:"from_status"`
	To        string          `json:"to_status"`
	Changes   json.RawMessage `json:"changes"`
	Note      string          `json:"note"`
	ActorID   *string         `json:"actor_id"`
	ActorName string          `json:"actor_name"`
	CreatedAt time.Time       `json:"created_at"`
}

const expenseCols = `id, category_key, description, amount_minor, currency, kind, to_char(expense_date, 'YYYY-MM-DD'),
	to_char(payment_date, 'YYYY-MM-DD'), reference, status, recurring_id::text, created_by::text, approved_by::text, approved_at, created_at, updated_at`

func scanExpense(row pgx.Row) (*Expense, error) {
	var e Expense
	err := row.Scan(&e.ID, &e.Category, &e.Description, &e.AmountMinor, &e.Currency, &e.Kind, &e.ExpenseDate, &e.PaymentDate,
		&e.Reference, &e.Status, &e.RecurringID, &e.CreatedBy, &e.ApprovedBy, &e.ApprovedAt, &e.CreatedAt, &e.UpdatedAt)
	return &e, err
}

// ExpenseFilter selects expenses for the list.
type ExpenseFilter struct {
	Status   string
	Category string
	From, To string // YYYY-MM-DD, inclusive
}

func (s *Service) Expenses(ctx context.Context, f ExpenseFilter) ([]Expense, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+expenseCols+` FROM finance_expenses
		WHERE ($1 = '' OR status = $1) AND ($2 = '' OR category_key = $2)
		AND ($3 = '' OR expense_date >= $3::date) AND ($4 = '' OR expense_date <= $4::date)
		ORDER BY expense_date DESC, created_at DESC LIMIT 500`, f.Status, f.Category, f.From, f.To)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Expense{}
	for rows.Next() {
		e, err := scanExpense(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func (s *Service) Expense(ctx context.Context, id string) (*Expense, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	e, err := scanExpense(s.pool.QueryRow(ctx, `SELECT `+expenseCols+` FROM finance_expenses WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	e.History, err = s.history(ctx, "expense_id", id)
	return e, err
}

func (s *Service) history(ctx context.Context, column, id string) ([]Event, error) {
	rows, err := s.pool.Query(ctx, `SELECT e.action, e.from_status, e.to_status, e.changes, e.note, e.actor_id::text,
		coalesce(nullif(trim(u.first_name || ' ' || u.last_name), ''), u.email, ''), e.created_at
		FROM finance_expense_events e LEFT JOIN users u ON u.id = e.actor_id WHERE e.`+column+` = $1 ORDER BY e.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.Action, &e.From, &e.To, &e.Changes, &e.Note, &e.ActorID, &e.ActorName, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ExpenseInput creates or edits an expense (edits only while draft or rejected).
type ExpenseInput struct {
	Category    string  `json:"category"`
	Description string  `json:"description"`
	AmountMinor int64   `json:"amount_minor"`
	Currency    string  `json:"currency"`
	Kind        string  `json:"kind"`
	ExpenseDate string  `json:"expense_date"`
	PaymentDate *string `json:"payment_date"`
	Reference   string  `json:"reference"`
	RecurringID *string `json:"recurring_id"`
	Note        string  `json:"note"`
}

func validDate(v string) bool {
	_, err := time.Parse("2006-01-02", v)
	return err == nil
}

func (s *Service) validate(ctx context.Context, q db.Querier, in *ExpenseInput) error {
	var f []httpx.FieldError
	bad := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	in.Description = strings.TrimSpace(in.Description)
	in.Reference = strings.TrimSpace(in.Reference)
	if in.Currency == "" {
		in.Currency = Currency
	}
	var ok bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM finance_expense_categories WHERE key = $1)`, in.Category).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		bad("category", "Choose a category.")
	}
	if n := utf8.RuneCountInString(in.Description); n < 1 || n > 300 {
		bad("description", "Describe the expense (up to 300 characters).")
	}
	if in.AmountMinor < 0 {
		bad("amount_minor", "The amount cannot be negative.")
	}
	if !regexp.MustCompile(`^[A-Z]{3}$`).MatchString(in.Currency) {
		bad("currency", "Use a three-letter currency code.")
	}
	if in.Kind != "actual" && in.Kind != "estimated" {
		bad("kind", "Choose actual or estimated.")
	}
	if !validDate(in.ExpenseDate) {
		bad("expense_date", "Give the date the expense was incurred (YYYY-MM-DD).")
	}
	if in.PaymentDate != nil && *in.PaymentDate != "" {
		if !validDate(*in.PaymentDate) {
			bad("payment_date", "Give the payment date as YYYY-MM-DD.")
		} else if in.Kind != "actual" {
			bad("payment_date", "Only an actual expense has a payment date.")
		}
	} else {
		in.PaymentDate = nil
	}
	if utf8.RuneCountInString(in.Reference) > 300 {
		bad("reference", "Keep the reference under 300 characters.")
	}
	if in.RecurringID != nil && *in.RecurringID != "" {
		if !uuidPattern.MatchString(*in.RecurringID) {
			bad("recurring_id", "Unknown recurring cost.")
		} else {
			var cat string
			err := q.QueryRow(ctx, `SELECT category_key FROM finance_recurring_expenses WHERE id = $1`, *in.RecurringID).Scan(&cat)
			if errors.Is(err, pgx.ErrNoRows) {
				bad("recurring_id", "Unknown recurring cost.")
			} else if err != nil {
				return err
			} else if cat != in.Category {
				bad("recurring_id", "The recurring cost is in another category.")
			}
		}
	} else {
		in.RecurringID = nil
	}
	if len(f) > 0 {
		return httpx.ValidationError(f)
	}
	return nil
}

func (s *Service) event(ctx context.Context, q db.Querier, expenseID, recurringID, action, from, to string, changes map[string]any, note, actor string) error {
	b, err := json.Marshal(changes)
	if err != nil {
		return err
	}
	var e, r, a *string
	if expenseID != "" {
		e = &expenseID
	}
	if recurringID != "" {
		r = &recurringID
	}
	if actor != "" {
		a = &actor
	}
	if _, err := q.Exec(ctx, `INSERT INTO finance_expense_events (expense_id, recurring_id, action, from_status, to_status, changes, note, actor_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, e, r, action, from, to, b, strings.TrimSpace(note), a); err != nil {
		return err
	}
	target, tid := "finance_expense", expenseID
	if recurringID != "" {
		target, tid = "finance_recurring_expense", recurringID
	}
	return audit.Record(ctx, q, audit.Entry{ActorUserID: actor, Action: target + "." + action, TargetType: target, TargetID: tid,
		Metadata: map[string]any{"from": from, "to": to, "changes": changes}})
}

func (s *Service) CreateExpense(ctx context.Context, p *identity.Principal, in ExpenseInput) (*Expense, error) {
	var id string
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.validate(ctx, tx, &in); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO finance_expenses (category_key, description, amount_minor, currency, kind, expense_date,
			payment_date, reference, recurring_id, created_by, updated_by) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10) RETURNING id`,
			in.Category, in.Description, in.AmountMinor, in.Currency, in.Kind, in.ExpenseDate, in.PaymentDate, in.Reference, in.RecurringID,
			p.UserID).Scan(&id); err != nil {
			if strings.Contains(err.Error(), "finance_expenses_recurring_occurrence") {
				return httpx.ValidationError([]httpx.FieldError{{Field: "expense_date", Code: "duplicate",
					Message: "This occurrence of the recurring cost is already recorded."}})
			}
			return err
		}
		return s.event(ctx, tx, id, "", "created", "", "draft", map[string]any{"amount_minor": in.AmountMinor, "category": in.Category,
			"kind": in.Kind}, in.Note, p.UserID)
	})
	if err != nil {
		return nil, err
	}
	return s.Expense(ctx, id)
}

func (s *Service) UpdateExpense(ctx context.Context, p *identity.Principal, id string, in ExpenseInput) (*Expense, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		cur, err := scanExpense(tx.QueryRow(ctx, `SELECT `+expenseCols+` FROM finance_expenses WHERE id = $1 FOR UPDATE`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		if cur.Status != "draft" && cur.Status != "rejected" {
			return httpx.NewError(http.StatusConflict, "not_editable", "Only draft or rejected expenses can be edited. Void it and record a new one.")
		}
		if err := s.validate(ctx, tx, &in); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE finance_expenses SET category_key = $2, description = $3, amount_minor = $4, currency = $5, kind = $6,
			expense_date = $7, payment_date = $8, reference = $9, recurring_id = $10, status = 'draft', updated_by = $11, updated_at = now()
			WHERE id = $1`, id, in.Category, in.Description, in.AmountMinor, in.Currency, in.Kind, in.ExpenseDate, in.PaymentDate, in.Reference,
			in.RecurringID, p.UserID); err != nil {
			return err
		}
		changes := map[string]any{}
		if cur.AmountMinor != in.AmountMinor {
			changes["amount_minor"] = []int64{cur.AmountMinor, in.AmountMinor}
		}
		if cur.Category != in.Category {
			changes["category"] = []string{cur.Category, in.Category}
		}
		if cur.ExpenseDate != in.ExpenseDate {
			changes["expense_date"] = []string{cur.ExpenseDate, in.ExpenseDate}
		}
		if cur.Kind != in.Kind {
			changes["kind"] = []string{cur.Kind, in.Kind}
		}
		return s.event(ctx, tx, id, "", "edited", cur.Status, "draft", changes, in.Note, p.UserID)
	})
	if err != nil {
		return nil, err
	}
	return s.Expense(ctx, id)
}

// Transition moves an expense through its workflow:
// submit (draft → submitted), approve and reject (submitted → approved or
// rejected; needs finance.expenses.approve; someone else's expense unless
// the approver is a super admin), void (approved → void, with a note).
func (s *Service) Transition(ctx context.Context, p *identity.Principal, id, action, note string) (*Expense, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	from := map[string][]string{"submit": {"draft", "rejected"}, "approve": {"submitted"}, "reject": {"submitted"}, "void": {"approved", "draft", "submitted"}}
	to := map[string]string{"submit": "submitted", "approve": "approved", "reject": "rejected", "void": "void"}
	allowed, ok := from[action]
	if !ok {
		return nil, httpx.ErrNotFound
	}
	if (action == "approve" || action == "reject") && !p.Can(identity.PermFinanceExpensesApprove) {
		return nil, httpx.ErrForbidden
	}
	if (action == "reject" || action == "void") && strings.TrimSpace(note) == "" {
		return nil, httpx.ValidationError([]httpx.FieldError{{Field: "note", Code: "required", Message: "Say why."}})
	}
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		cur, err := scanExpense(tx.QueryRow(ctx, `SELECT `+expenseCols+` FROM finance_expenses WHERE id = $1 FOR UPDATE`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		okFrom := false
		for _, f := range allowed {
			okFrom = okFrom || f == cur.Status
		}
		if !okFrom {
			return httpx.NewError(http.StatusConflict, "invalid_transition", "This expense is "+cur.Status+": that step does not apply.")
		}
		if action == "approve" && cur.CreatedBy != nil && *cur.CreatedBy == p.UserID && !p.HasRole("super_admin") {
			return httpx.NewError(http.StatusConflict, "own_expense", "Someone else must approve an expense you recorded.")
		}
		set := `status = $2, updated_by = $3, updated_at = now()`
		if action == "approve" {
			set += `, approved_by = $3, approved_at = now()`
		}
		if _, err := tx.Exec(ctx, `UPDATE finance_expenses SET `+set+` WHERE id = $1`, id, to[action], p.UserID); err != nil {
			return err
		}
		return s.event(ctx, tx, id, "", action, cur.Status, to[action], map[string]any{}, note, p.UserID)
	})
	if err != nil {
		return nil, err
	}
	return s.Expense(ctx, id)
}
