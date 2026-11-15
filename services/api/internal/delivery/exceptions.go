package delivery

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/support"
)

// ExceptionInput raises a fulfillment or delivery exception.
type ExceptionInput struct {
	Stage        string `json:"stage"`
	Type         string `json:"type"`
	Severity     string `json:"severity"`
	Blocking     bool   `json:"blocking"`
	Summary      string `json:"summary"`
	Notes        string `json:"notes"`
	NextAction   string `json:"next_action"`
	OwnerUserID  string `json:"owner_user_id"`
	ObligationID string `json:"obligation_id"`
	DeliveryID   string `json:"delivery_id"`
	OrderID      string `json:"order_id"`
	CustomerID   string `json:"customer_id"`
	BatchID      string `json:"batch_id"`
	RunID        string `json:"run_id"`
	// DedupeKey keeps one exception per cause (a retried job, a double click).
	DedupeKey string `json:"-"`
	Actor     string `json:"-"`
}

var (
	exceptionTypes = map[string]bool{"eligibility": true, "missing_package": true, "incorrect_package": true, "kit_discrepancy": true,
		"batch_problem": true, "address_problem": true, "capacity_shortage": true, "partner_cancellation": true, "late_handoff": true,
		"handoff_dispute": true, "delivery_failure": true, "missing_proof": true, "damaged_package": true, "customer_complaint": true, "other": true}
	severities  = map[string]bool{"low": true, "medium": true, "high": true, "critical": true}
	nextActions = map[string]bool{"none": true, "reattempt": true, "reschedule": true, "return": true, "escalate": true, "refund_review": true}
)

// RaiseException records an exception in q's transaction and returns its id.
// With a dedupe key, an existing exception for the same cause is returned.
func (s *Service) RaiseException(ctx context.Context, q db.Querier, in ExceptionInput) (string, error) {
	in.Summary, in.Notes = strings.TrimSpace(in.Summary), strings.TrimSpace(in.Notes)
	if in.Stage == "" {
		in.Stage = "delivery"
	}
	if in.NextAction == "" {
		in.NextAction = "none"
	}
	var f []httpx.FieldError
	bad := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	if in.Stage != "fulfillment" && in.Stage != "delivery" {
		bad("stage", "Choose fulfillment or delivery.")
	}
	if !exceptionTypes[in.Type] {
		bad("type", "Choose an exception type.")
	}
	if !severities[in.Severity] {
		bad("severity", "Choose a severity.")
	}
	if !nextActions[in.NextAction] {
		bad("next_action", "Choose a next action.")
	}
	if n := runes(in.Summary); n < 3 || n > 300 {
		bad("summary", "Summarise the exception (3 to 300 characters).")
	}
	if runes(in.Notes) > 2000 {
		bad("notes", "Keep notes under 2,000 characters.")
	}
	for field, v := range map[string]string{"owner_user_id": in.OwnerUserID, "obligation_id": in.ObligationID, "delivery_id": in.DeliveryID,
		"order_id": in.OrderID, "customer_id": in.CustomerID, "batch_id": in.BatchID, "run_id": in.RunID} {
		if v != "" && !validID(v) {
			bad(field, "Not a valid id.")
		}
	}
	if len(f) > 0 {
		return "", httpx.ValidationError(f)
	}
	// A delivery or obligation fills in the order and customer.
	if in.DeliveryID != "" {
		if err := q.QueryRow(ctx, `SELECT obligation_id::text, order_id::text, customer_id::text FROM deliveries WHERE id = $1`, in.DeliveryID).
			Scan(&in.ObligationID, &in.OrderID, &in.CustomerID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return "", fieldErr("delivery_id", "Unknown delivery.")
			}
			return "", err
		}
	} else if in.ObligationID != "" {
		if err := q.QueryRow(ctx, `SELECT order_id::text, customer_id::text FROM fulfillment_obligations WHERE id = $1`, in.ObligationID).
			Scan(&in.OrderID, &in.CustomerID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return "", fieldErr("obligation_id", "Unknown fulfillment.")
			}
			return "", err
		}
	}
	if in.DedupeKey != "" {
		var id string
		err := q.QueryRow(ctx, `SELECT id::text FROM delivery_exceptions WHERE dedupe_key = $1`, in.DedupeKey).Scan(&id)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
	}
	number, err := NextNumber(ctx, q, "EXC", WeekPeriod(s.Now().In(s.Location)))
	if err != nil {
		return "", err
	}
	var id string
	err = q.QueryRow(ctx, `
		INSERT INTO delivery_exceptions (number, stage, type, severity, blocking, owner_user_id, obligation_id, delivery_id, order_id,
			customer_id, batch_id, run_id, summary, notes, next_action, dedupe_key, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		ON CONFLICT (dedupe_key) DO NOTHING RETURNING id::text`,
		number, in.Stage, in.Type, in.Severity, in.Blocking, nilIfEmpty(in.OwnerUserID), nilIfEmpty(in.ObligationID), nilIfEmpty(in.DeliveryID),
		nilIfEmpty(in.OrderID), nilIfEmpty(in.CustomerID), nilIfEmpty(in.BatchID), nilIfEmpty(in.RunID), in.Summary, in.Notes, in.NextAction,
		nilIfEmpty(in.DedupeKey), nilIfEmpty(in.Actor)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		// Raced with the same dedupe key.
		err = q.QueryRow(ctx, `SELECT id::text FROM delivery_exceptions WHERE dedupe_key = $1`, in.DedupeKey).Scan(&id)
		return id, err
	}
	if err != nil {
		return "", err
	}
	if _, err := q.Exec(ctx, `INSERT INTO delivery_exception_events (exception_id, type, body, actor_user_id) VALUES ($1, 'created', $2, $3)`,
		id, in.Summary, nilIfEmpty(in.Actor)); err != nil {
		return "", err
	}
	typ := "delivery.exception_raised"
	if in.Stage == "fulfillment" {
		typ = "fulfillment.exception_raised"
	}
	if err := event(ctx, q, typ, "delivery_exception", id, "", map[string]any{"type": in.Type, "severity": in.Severity,
		"blocking": in.Blocking, "delivery_id": in.DeliveryID, "obligation_id": in.ObligationID}); err != nil {
		return "", err
	}
	if s.Alerts != nil && (in.Severity == "high" || in.Severity == "critical") {
		title := "Exception " + number + ": " + in.Summary
		if runes(title) > 200 {
			title = string([]rune(title)[:197]) + "..."
		}
		if err := s.Alerts.Raise(ctx, q, support.Alert{Kind: "delivery_exception", Severity: alertSeverity(in.Severity),
			Title: title, Body: clip(in.Notes, 1000), ResourceType: "delivery_exception", ResourceID: id,
			Permission: "delivery_exceptions.manage", DedupeKey: "delivery_exception:" + id}); err != nil {
			return "", err
		}
	}
	return id, record(ctx, q, in.Actor, "delivery.exception_raised", "delivery_exception", id,
		map[string]any{"type": in.Type, "severity": in.Severity, "blocking": in.Blocking})
}

func alertSeverity(s string) string {
	if s == "critical" {
		return "critical"
	}
	return "warning"
}

// Exception is an exception with its history.
type Exception struct {
	ID           string           `json:"id"`
	Number       string           `json:"number"`
	Stage        string           `json:"stage"`
	Type         string           `json:"type"`
	Severity     string           `json:"severity"`
	Status       string           `json:"status"`
	Blocking     bool             `json:"blocking"`
	OwnerUserID  *string          `json:"owner_user_id"`
	OwnerName    *string          `json:"owner_name"`
	ObligationID *string          `json:"obligation_id"`
	DeliveryID   *string          `json:"delivery_id"`
	DeliveryNo   *string          `json:"delivery_number"`
	OrderID      *string          `json:"order_id"`
	OrderNumber  *string          `json:"order_number"`
	CustomerID   *string          `json:"customer_id"`
	BatchID      *string          `json:"batch_id"`
	RunID        *string          `json:"run_id"`
	Summary      string           `json:"summary"`
	Notes        string           `json:"notes"`
	NextAction   string           `json:"next_action"`
	Resolution   *string          `json:"resolution"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
	ResolvedAt   *time.Time       `json:"resolved_at"`
	Events       []ExceptionEvent `json:"events,omitempty"`
}

type ExceptionEvent struct {
	Type      string    `json:"type"`
	Body      *string   `json:"body"`
	ActorName *string   `json:"actor_name"`
	CreatedAt time.Time `json:"created_at"`
}

const exceptionCols = `e.id::text, e.number, e.stage, e.type, e.severity, e.status, e.blocking, e.owner_user_id::text,
	NULLIF(trim(COALESCE(ou.first_name, '') || ' ' || COALESCE(ou.last_name, '')), ''), e.obligation_id::text, e.delivery_id::text,
	d.number, e.order_id::text, o.number, e.customer_id::text, e.batch_id::text, e.run_id::text, e.summary, e.notes, e.next_action,
	e.resolution, e.created_at, e.updated_at, e.resolved_at`

const exceptionFrom = ` FROM delivery_exceptions e LEFT JOIN users ou ON ou.id = e.owner_user_id
	LEFT JOIN deliveries d ON d.id = e.delivery_id LEFT JOIN orders o ON o.id = e.order_id`

func scanException(row pgx.Row) (*Exception, error) {
	var e Exception
	err := row.Scan(&e.ID, &e.Number, &e.Stage, &e.Type, &e.Severity, &e.Status, &e.Blocking, &e.OwnerUserID, &e.OwnerName,
		&e.ObligationID, &e.DeliveryID, &e.DeliveryNo, &e.OrderID, &e.OrderNumber, &e.CustomerID, &e.BatchID, &e.RunID, &e.Summary,
		&e.Notes, &e.NextAction, &e.Resolution, &e.CreatedAt, &e.UpdatedAt, &e.ResolvedAt)
	return &e, err
}

// ExceptionFilter narrows the queue.
type ExceptionFilter struct {
	Status     string // open (default: open + in_progress), resolved, all
	Stage      string
	DeliveryID string
	CustomerID string
}

func (s *Service) Exceptions(ctx context.Context, f ExceptionFilter) ([]Exception, error) {
	where, args := []string{"true"}, []any{}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, strings.ReplaceAll(cond, "$?", "$"+itoa(len(args))))
	}
	switch f.Status {
	case "", "open":
		where = append(where, "e.status IN ('open', 'in_progress')")
	case "resolved":
		where = append(where, "e.status IN ('resolved', 'closed')")
	case "all":
	default:
		return nil, fieldErr("status", "Choose open, resolved or all.")
	}
	if f.Stage != "" {
		add("e.stage = $?", f.Stage)
	}
	if f.DeliveryID != "" {
		if !validID(f.DeliveryID) {
			return []Exception{}, nil
		}
		add("e.delivery_id = $?", f.DeliveryID)
	}
	if f.CustomerID != "" {
		if !validID(f.CustomerID) {
			return []Exception{}, nil
		}
		add("e.customer_id = $?", f.CustomerID)
	}
	rows, err := s.pool.Query(ctx, `SELECT `+exceptionCols+exceptionFrom+` WHERE `+strings.Join(where, " AND ")+`
		ORDER BY CASE e.severity WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END, e.created_at DESC LIMIT 300`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Exception{}
	for rows.Next() {
		e, err := scanException(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func (s *Service) Exception(ctx context.Context, id string) (*Exception, error) {
	if !validID(id) {
		return nil, httpx.ErrNotFound
	}
	e, err := scanException(s.pool.QueryRow(ctx, `SELECT `+exceptionCols+exceptionFrom+` WHERE e.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT x.type, x.body, NULLIF(trim(COALESCE(u.first_name, '') || ' ' || COALESCE(u.last_name, '')), ''), x.created_at
		FROM delivery_exception_events x LEFT JOIN users u ON u.id = x.actor_user_id WHERE x.exception_id = $1 ORDER BY x.id`, id)
	if err != nil {
		return nil, err
	}
	e.Events, err = pgx.CollectRows(rows, pgx.RowToStructByPos[ExceptionEvent])
	return e, err
}

// ExceptionUpdate works an exception. Resolution needs a resolution note;
// resolving the last blocking exception of a fulfillment resumes it.
type ExceptionUpdate struct {
	Status      *string `json:"status"`
	OwnerUserID *string `json:"owner_user_id"`
	NextAction  *string `json:"next_action"`
	Note        string  `json:"note"`
	Resolution  string  `json:"resolution"`
}

func (s *Service) UpdateException(ctx context.Context, actor, id string, in ExceptionUpdate) (*Exception, error) {
	if !validID(id) {
		return nil, httpx.ErrNotFound
	}
	in.Note, in.Resolution = strings.TrimSpace(in.Note), strings.TrimSpace(in.Resolution)
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var status, obligation string
		var blocking bool
		err := tx.QueryRow(ctx, `SELECT status, COALESCE(obligation_id::text, ''), blocking FROM delivery_exceptions WHERE id = $1 FOR UPDATE`, id).
			Scan(&status, &obligation, &blocking)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		if status == "closed" {
			return conflict("closed", "This exception is closed.")
		}
		logEvent := func(typ, body string) error {
			_, err := tx.Exec(ctx, `INSERT INTO delivery_exception_events (exception_id, type, body, actor_user_id) VALUES ($1, $2, $3, $4)`,
				id, typ, nilIfEmpty(body), nilIfEmpty(actor))
			return err
		}
		if in.Note != "" {
			if runes(in.Note) > 2000 {
				return fieldErr("note", "Keep notes under 2,000 characters.")
			}
			if err := logEvent("note", in.Note); err != nil {
				return err
			}
		}
		if in.OwnerUserID != nil {
			owner := *in.OwnerUserID
			if owner == "me" {
				owner = actor
			}
			if owner != "" && !validID(owner) {
				return fieldErr("owner_user_id", "Not a valid user.")
			}
			if _, err := tx.Exec(ctx, `UPDATE delivery_exceptions SET owner_user_id = $2, updated_at = now() WHERE id = $1`, id, nilIfEmpty(owner)); err != nil {
				return err
			}
			if err := logEvent("owner", owner); err != nil {
				return err
			}
		}
		if in.NextAction != nil {
			if !nextActions[*in.NextAction] {
				return fieldErr("next_action", "Choose a next action.")
			}
			if _, err := tx.Exec(ctx, `UPDATE delivery_exceptions SET next_action = $2, updated_at = now() WHERE id = $1`, id, *in.NextAction); err != nil {
				return err
			}
			if err := logEvent("next_action", *in.NextAction); err != nil {
				return err
			}
		}
		if in.Status != nil && *in.Status != status {
			to := *in.Status
			switch to {
			case "open", "in_progress":
				if status == "resolved" {
					return conflict("invalid_transition", "A resolved exception stays resolved; raise a new one.")
				}
			case "resolved", "closed":
				if in.Resolution == "" || runes(in.Resolution) > 2000 {
					return fieldErr("resolution", "Say how the exception was resolved.")
				}
			default:
				return fieldErr("status", "Choose open, in progress, resolved or closed.")
			}
			if to == "resolved" || to == "closed" {
				if _, err := tx.Exec(ctx, `UPDATE delivery_exceptions SET status = $2, resolution = $3, resolved_by = $4,
					resolved_at = COALESCE(resolved_at, now()), updated_at = now() WHERE id = $1`, id, to, in.Resolution, nilIfEmpty(actor)); err != nil {
					return err
				}
				if err := logEvent("resolution", in.Resolution); err != nil {
					return err
				}
				// The last blocking exception resolved: the fulfillment resumes.
				if blocking && obligation != "" && s.Fulfillment != nil {
					var open int
					if err := tx.QueryRow(ctx, `SELECT count(*) FROM delivery_exceptions WHERE obligation_id = $1 AND blocking AND status IN ('open', 'in_progress')`,
						obligation).Scan(&open); err != nil {
						return err
					}
					if open == 0 {
						if err := s.Fulfillment.Resume(ctx, tx, obligation, actor); err != nil {
							return err
						}
					}
				}
			} else {
				if _, err := tx.Exec(ctx, `UPDATE delivery_exceptions SET status = $2, updated_at = now() WHERE id = $1`, id, to); err != nil {
					return err
				}
				if err := logEvent("status", to); err != nil {
					return err
				}
			}
		}
		return record(ctx, tx, actor, "delivery.exception_updated", "delivery_exception", id, nil)
	})
	if err != nil {
		return nil, err
	}
	return s.Exception(ctx, id)
}

// BlockingExceptions counts open blocking exceptions naming a batch, an
// obligation or a delivery.
func BlockingExceptions(ctx context.Context, q db.Querier, column, id string) (int, error) {
	if column != "batch_id" && column != "obligation_id" && column != "delivery_id" {
		return 0, errors.New("delivery: bad column")
	}
	var n int
	err := q.QueryRow(ctx, `SELECT count(*) FROM delivery_exceptions WHERE `+column+` = $1 AND blocking AND status IN ('open', 'in_progress')`, id).Scan(&n)
	return n, err
}

func itoa(n int) string {
	const digits = "0123456789"
	if n < 10 {
		return digits[n : n+1]
	}
	return itoa(n/10) + digits[n%10:n%10+1]
}
