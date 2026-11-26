package finance

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
)

// Recurring is a scheduled cost (rent, salaries, subscriptions). An approved
// schedule produces estimated occurrences; an actual expense recorded for an
// occurrence (same schedule and date) replaces that estimate.
type Recurring struct {
	ID          string     `json:"id"`
	Category    string     `json:"category"`
	Description string     `json:"description"`
	AmountMinor int64      `json:"amount_minor"`
	Currency    string     `json:"currency"`
	Cadence     string     `json:"cadence"`
	StartsOn    string     `json:"starts_on"`
	EndsOn      *string    `json:"ends_on"`
	Status      string     `json:"status"`
	Reference   string     `json:"reference"`
	ApprovedBy  *string    `json:"approved_by"`
	ApprovedAt  *time.Time `json:"approved_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

const recurringCols = `id, category_key, description, amount_minor, currency, cadence, to_char(starts_on, 'YYYY-MM-DD'),
	to_char(ends_on, 'YYYY-MM-DD'), status, reference, approved_by::text, approved_at, created_at`

func scanRecurring(row pgx.Row) (*Recurring, error) {
	var r Recurring
	err := row.Scan(&r.ID, &r.Category, &r.Description, &r.AmountMinor, &r.Currency, &r.Cadence, &r.StartsOn, &r.EndsOn, &r.Status,
		&r.Reference, &r.ApprovedBy, &r.ApprovedAt, &r.CreatedAt)
	return &r, err
}

func (s *Service) RecurringList(ctx context.Context) ([]Recurring, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+recurringCols+` FROM finance_recurring_expenses ORDER BY status, category_key, created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Recurring{}
	for rows.Next() {
		r, err := scanRecurring(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

type RecurringInput struct {
	Category    string  `json:"category"`
	Description string  `json:"description"`
	AmountMinor int64   `json:"amount_minor"`
	Currency    string  `json:"currency"`
	Cadence     string  `json:"cadence"`
	StartsOn    string  `json:"starts_on"`
	EndsOn      *string `json:"ends_on"`
	Reference   string  `json:"reference"`
}

func (s *Service) CreateRecurring(ctx context.Context, p *identity.Principal, in RecurringInput) (*Recurring, error) {
	var f []httpx.FieldError
	bad := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	in.Description = strings.TrimSpace(in.Description)
	if in.Currency == "" {
		in.Currency = Currency
	}
	var ok bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM finance_expense_categories WHERE key = $1)`, in.Category).Scan(&ok); err != nil {
		return nil, err
	}
	if !ok {
		bad("category", "Choose a category.")
	}
	if n := utf8.RuneCountInString(in.Description); n < 1 || n > 300 {
		bad("description", "Describe the cost (up to 300 characters).")
	}
	if in.AmountMinor <= 0 {
		bad("amount_minor", "Give the amount of each occurrence.")
	}
	if !regexp.MustCompile(`^[A-Z]{3}$`).MatchString(in.Currency) {
		bad("currency", "Use a three-letter currency code.")
	}
	switch in.Cadence {
	case "weekly", "monthly", "quarterly", "yearly":
	default:
		bad("cadence", "Choose weekly, monthly, quarterly or yearly.")
	}
	if !validDate(in.StartsOn) {
		bad("starts_on", "Give the first date (YYYY-MM-DD).")
	}
	if in.EndsOn != nil && *in.EndsOn != "" {
		if !validDate(*in.EndsOn) || *in.EndsOn < in.StartsOn {
			bad("ends_on", "The end date must be a date on or after the start.")
		}
	} else {
		in.EndsOn = nil
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	var id string
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO finance_recurring_expenses (category_key, description, amount_minor, currency, cadence, starts_on, ends_on,
			reference, created_by) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`, in.Category, in.Description, in.AmountMinor,
			in.Currency, in.Cadence, in.StartsOn, in.EndsOn, strings.TrimSpace(in.Reference), p.UserID).Scan(&id); err != nil {
			return err
		}
		return s.event(ctx, tx, "", id, "created", "", "draft", map[string]any{"amount_minor": in.AmountMinor, "cadence": in.Cadence}, "", p.UserID)
	})
	if err != nil {
		return nil, err
	}
	return scanRecurring(s.pool.QueryRow(ctx, `SELECT `+recurringCols+` FROM finance_recurring_expenses WHERE id = $1`, id))
}

// SetRecurringStatus approves (draft → approved; needs approve permission) or ends a schedule.
func (s *Service) SetRecurringStatus(ctx context.Context, p *identity.Principal, id, action, note string) (*Recurring, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	if action == "approve" && !p.Can(identity.PermFinanceExpensesApprove) {
		return nil, httpx.ErrForbidden
	}
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var status string
		err := tx.QueryRow(ctx, `SELECT status FROM finance_recurring_expenses WHERE id = $1 FOR UPDATE`, id).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		switch {
		case action == "approve" && status == "draft":
			_, err = tx.Exec(ctx, `UPDATE finance_recurring_expenses SET status = 'approved', approved_by = $2, approved_at = now(), updated_at = now() WHERE id = $1`, id, p.UserID)
			if err == nil {
				err = s.event(ctx, tx, "", id, "approve", status, "approved", map[string]any{}, note, p.UserID)
			}
		case action == "end" && status != "ended":
			_, err = tx.Exec(ctx, `UPDATE finance_recurring_expenses SET status = 'ended', approved_at = coalesce(approved_at, now()),
				ends_on = coalesce(ends_on, (now() AT TIME ZONE 'America/Port_of_Spain')::date), updated_at = now() WHERE id = $1`, id)
			if err == nil {
				err = s.event(ctx, tx, "", id, "end", status, "ended", map[string]any{}, note, p.UserID)
			}
		default:
			return httpx.NewError(http.StatusConflict, "invalid_transition", "This recurring cost is "+status+": that step does not apply.")
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return scanRecurring(s.pool.QueryRow(ctx, `SELECT `+recurringCols+` FROM finance_recurring_expenses WHERE id = $1`, id))
}

// Occurrences lists the dates a schedule falls on within [from, to].
// Monthly and longer cadences keep the start's day of month, moved to the
// month's last day when the month is shorter (31 Jan → 28/29 Feb).
func Occurrences(cadence string, start time.Time, end *time.Time, from, to time.Time) []time.Time {
	var out []time.Time
	add := func(n int) time.Time {
		switch cadence {
		case "weekly":
			return start.AddDate(0, 0, 7*n)
		case "monthly":
			return addMonthsClamped(start, n)
		case "quarterly":
			return addMonthsClamped(start, 3*n)
		case "yearly":
			return addMonthsClamped(start, 12*n)
		}
		return time.Time{}
	}
	for n := 0; n < 10000; n++ {
		d := add(n)
		if d.IsZero() || d.After(to) || (end != nil && d.After(*end)) {
			break
		}
		if !d.Before(from) {
			out = append(out, d)
		}
	}
	return out
}

func addMonthsClamped(t time.Time, months int) time.Time {
	y, m, d := t.Date()
	first := time.Date(y, m+time.Month(months), 1, 0, 0, 0, 0, t.Location())
	last := first.AddDate(0, 1, -1).Day()
	if d > last {
		d = last
	}
	return time.Date(first.Year(), first.Month(), d, 0, 0, 0, 0, t.Location())
}

// ---------------------------------------------------------------- coverage

// ConfirmCoverage records that a category has nothing to record for a month
// (the only way a missing category counts as zero). undo removes it.
func (s *Service) ConfirmCoverage(ctx context.Context, p *identity.Principal, category, month, note string, undo bool) error {
	t, err := time.Parse("2006-01", month)
	if err != nil {
		return httpx.ValidationError([]httpx.FieldError{{Field: "month", Code: "invalid", Message: "Give the month as YYYY-MM."}})
	}
	m := t.Format("2006-01-02")
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM finance_expense_categories WHERE key = $1)`, category).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return httpx.ValidationError([]httpx.FieldError{{Field: "category", Code: "invalid", Message: "Choose a category."}})
		}
		if undo {
			_, err = tx.Exec(ctx, `DELETE FROM finance_coverage_confirmations WHERE category_key = $1 AND month = $2`, category, m)
		} else {
			_, err = tx.Exec(ctx, `INSERT INTO finance_coverage_confirmations (category_key, month, note, confirmed_by) VALUES ($1, $2, $3, $4)
				ON CONFLICT (category_key, month) DO UPDATE SET note = EXCLUDED.note, confirmed_by = EXCLUDED.confirmed_by, confirmed_at = now()`,
				category, m, strings.TrimSpace(note), p.UserID)
		}
		if err != nil {
			return err
		}
		action := map[bool]string{false: "coverage_confirmed", true: "coverage_unconfirmed"}[undo]
		return auditCoverage(ctx, tx, p.UserID, action, category, month, note)
	})
}

// ---------------------------------------------------------------- cash balances

type CashBalance struct {
	ID           string    `json:"id"`
	AsOf         string    `json:"as_of"`
	AccountLabel string    `json:"account_label"`
	AmountMinor  int64     `json:"amount_minor"`
	Currency     string    `json:"currency"`
	Reference    string    `json:"reference"`
	RecordedBy   *string   `json:"recorded_by"`
	CreatedAt    time.Time `json:"created_at"`
}

type CashBalanceInput struct {
	AsOf         string `json:"as_of"`
	AccountLabel string `json:"account_label"`
	AmountMinor  int64  `json:"amount_minor"`
	Currency     string `json:"currency"`
	Reference    string `json:"reference"`
}

func (s *Service) CashBalances(ctx context.Context) ([]CashBalance, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, to_char(as_of, 'YYYY-MM-DD'), account_label, amount_minor, currency, reference, recorded_by::text, created_at
		FROM finance_cash_balances ORDER BY as_of DESC, account_label LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CashBalance{}
	for rows.Next() {
		var b CashBalance
		if err := rows.Scan(&b.ID, &b.AsOf, &b.AccountLabel, &b.AmountMinor, &b.Currency, &b.Reference, &b.RecordedBy, &b.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// RecordCashBalance records an approved balance (finance.expenses.approve) with the reference it was taken from.
func (s *Service) RecordCashBalance(ctx context.Context, p *identity.Principal, in CashBalanceInput) (*CashBalance, error) {
	var f []httpx.FieldError
	if in.Currency == "" {
		in.Currency = Currency
	}
	if !validDate(in.AsOf) {
		f = append(f, httpx.FieldError{Field: "as_of", Code: "invalid", Message: "Give the date of the balance (YYYY-MM-DD)."})
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(in.AccountLabel)); n < 1 || n > 100 {
		f = append(f, httpx.FieldError{Field: "account_label", Code: "invalid", Message: "Name the account."})
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(in.Reference)); n < 1 || n > 300 {
		f = append(f, httpx.FieldError{Field: "reference", Code: "required", Message: "Say where the balance comes from (statement, reconciliation)."})
	}
	if !regexp.MustCompile(`^[A-Z]{3}$`).MatchString(in.Currency) {
		f = append(f, httpx.FieldError{Field: "currency", Code: "invalid", Message: "Use a three-letter currency code."})
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	var b CashBalance
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO finance_cash_balances (as_of, account_label, amount_minor, currency, reference, recorded_by)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, to_char(as_of, 'YYYY-MM-DD'), account_label, amount_minor, currency, reference, recorded_by::text, created_at`,
			in.AsOf, strings.TrimSpace(in.AccountLabel), in.AmountMinor, in.Currency, strings.TrimSpace(in.Reference), p.UserID).
			Scan(&b.ID, &b.AsOf, &b.AccountLabel, &b.AmountMinor, &b.Currency, &b.Reference, &b.RecordedBy, &b.CreatedAt); err != nil {
			if strings.Contains(err.Error(), "finance_cash_balances_as_of_account_label_key") {
				return httpx.NewError(http.StatusConflict, "duplicate_balance", "A balance for this account and date is already recorded.")
			}
			return err
		}
		return auditCoverage(ctx, tx, p.UserID, "cash_balance_recorded", in.AccountLabel, in.AsOf, in.Reference)
	})
	return &b, err
}

func auditCoverage(ctx context.Context, q db.Querier, actor, action, target, period, note string) error {
	return auditRecord(ctx, q, actor, "finance."+action, "finance", target+"@"+period, map[string]any{"note": note})
}
