package automation

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/jobs"
	"mise.tt/api/internal/support"
)

type Execution struct {
	ID           string           `json:"id"`
	RuleID       string           `json:"rule_id"`
	RuleName     string           `json:"rule_name"`
	RuleVersion  int              `json:"rule_version"`
	Trigger      string           `json:"trigger"`
	TriggerRef   string           `json:"trigger_ref"`
	SubjectType  string           `json:"subject_type"`
	SubjectID    string           `json:"subject_id"`
	SubjectLabel string           `json:"subject_label"`
	Action       string           `json:"action"`
	Status       string           `json:"status"`
	Attempts     int              `json:"attempts"`
	LastError    *string          `json:"last_error"`
	Detail       string           `json:"detail"`
	Link         string           `json:"link"`
	CreatedAt    time.Time        `json:"created_at"`
	FinishedAt   *time.Time       `json:"finished_at"`
	DecidedAt    *time.Time       `json:"decided_at"`
	DecisionNote *string          `json:"decision_note"`
	Events       []ExecutionEvent `json:"events,omitempty"`
}

type ExecutionEvent struct {
	Type      string    `json:"type"`
	Body      *string   `json:"body"`
	ActorName *string   `json:"actor_name"`
	At        time.Time `json:"at"`
}

const execCols = `e.id::text, e.rule_id::text, r.name, e.rule_version, e.trigger, e.trigger_ref, e.subject_type, e.subject_id, e.subject_label, e.action,
	e.status, e.attempts, e.last_error, coalesce(e.result->>'detail', ''), coalesce(e.result->>'link', ''), e.created_at, e.finished_at, e.decided_at, e.decision_note`

func scanExec(row pgx.Row) (*Execution, error) {
	var x Execution
	err := row.Scan(&x.ID, &x.RuleID, &x.RuleName, &x.RuleVersion, &x.Trigger, &x.TriggerRef, &x.SubjectType, &x.SubjectID, &x.SubjectLabel, &x.Action,
		&x.Status, &x.Attempts, &x.LastError, &x.Detail, &x.Link, &x.CreatedAt, &x.FinishedAt, &x.DecidedAt, &x.DecisionNote)
	return &x, err
}

type ExecutionFilter struct {
	Status string
	RuleID string
}

func (s *Service) Executions(ctx context.Context, f ExecutionFilter) ([]Execution, error) {
	where, args := []string{"true"}, []any{}
	if f.Status != "" && f.Status != "all" {
		args = append(args, f.Status)
		where = append(where, "e.status = $1")
	}
	if f.RuleID != "" {
		args = append(args, f.RuleID)
		where = append(where, "e.rule_id::text = $"+string(rune('0'+len(args))))
	}
	rows, err := s.pool.Query(ctx, `SELECT `+execCols+` FROM automation_executions e JOIN automation_rules r ON r.id = e.rule_id WHERE `+
		strings.Join(where, " AND ")+` ORDER BY e.created_at DESC LIMIT 200`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Execution{}
	for rows.Next() {
		x, err := scanExec(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *x)
	}
	return out, rows.Err()
}

func (s *Service) Execution(ctx context.Context, id string) (*Execution, error) {
	x, err := scanExec(s.pool.QueryRow(ctx, `SELECT `+execCols+` FROM automation_executions e JOIN automation_rules r ON r.id = e.rule_id WHERE e.id::text = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT v.type, v.body, NULLIF(trim(coalesce(u.first_name, '') || ' ' || coalesce(u.last_name, '')), ''), v.created_at
		FROM automation_execution_events v LEFT JOIN users u ON u.id = v.actor_user_id WHERE v.execution_id = $1 ORDER BY v.id`, x.ID)
	if err != nil {
		return nil, err
	}
	x.Events, err = pgx.CollectRows(rows, pgx.RowToStructByPos[ExecutionEvent])
	return x, err
}

// Retry requeues a failed execution. Safe: the action is idempotent per execution.
func (s *Service) Retry(ctx context.Context, actor, id string) (*Execution, error) {
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var status string
		err := tx.QueryRow(ctx, `SELECT status FROM automation_executions WHERE id::text = $1 FOR UPDATE`, id).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		if status != "failed" {
			return conflict("not_failed", "Only a failed execution can be retried.")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO automation_execution_events (execution_id, type, actor_user_id) VALUES ($1::uuid, 'retry_requested', $2)`,
			id, nilIfEmpty(actor)); err != nil {
			return err
		}
		if err := jobs.Enqueue(ctx, tx, JobExecute, executePayload{ExecutionID: id}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "automation.retry", TargetType: "automation_execution", TargetID: id})
	})
	if err != nil {
		return nil, err
	}
	return s.Execution(ctx, id)
}

// Decide approves or rejects a requested approval. Approving records the
// decision and tells the owning team; it never performs a financial or
// operational action itself.
func (s *Service) Decide(ctx context.Context, actor, id string, approve bool, note string) (*Execution, error) {
	note = strings.TrimSpace(note)
	if n := len([]rune(note)); n < 3 || n > 1000 {
		return nil, fieldErr("note", "Explain the decision (3 to 1,000 characters).")
	}
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var status, perm, label, ruleName string
		err := tx.QueryRow(ctx, `SELECT e.status, r.owner_permission, e.subject_label, r.name FROM automation_executions e JOIN automation_rules r ON r.id = e.rule_id
			WHERE e.id::text = $1 FOR UPDATE OF e`, id).Scan(&status, &perm, &label, &ruleName)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		if status != "awaiting_approval" {
			return conflict("not_awaiting", "This execution is not waiting for an approval.")
		}
		to, typ := "rejected", "rejected"
		if approve {
			to, typ = "approved", "approved"
		}
		if _, err := tx.Exec(ctx, `UPDATE automation_executions SET status = $2, decided_by = $3, decided_at = now(), decision_note = $4 WHERE id::text = $1`,
			id, to, nilIfEmpty(actor), note); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO automation_execution_events (execution_id, type, body, actor_user_id) VALUES ($1::uuid, $2, $3, $4)`,
			id, typ, note, nilIfEmpty(actor)); err != nil {
			return err
		}
		if approve && s.Alerts != nil {
			if err := s.Alerts.Raise(ctx, tx, support.Alert{DedupeKey: "automation-approved:" + id, Kind: "automation", Severity: "info",
				Title: clip("Approved follow-up: "+ruleName+": "+label, 200), Body: clip(note, 1000), ResourceType: "automation_execution", ResourceID: id,
				Permission: perm}); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "automation." + typ, TargetType: "automation_execution", TargetID: id, Reason: note})
	})
	if err != nil {
		return nil, err
	}
	return s.Execution(ctx, id)
}

// ---------------------------------------------------------------- tasks

type Task struct {
	ID          string     `json:"id"`
	ExecutionID *string    `json:"execution_id"`
	Title       string     `json:"title"`
	Detail      string     `json:"detail"`
	Permission  string     `json:"permission"`
	Link        string     `json:"link"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	ClosedAt    *time.Time `json:"closed_at"`
	CloseNote   *string    `json:"close_note"`
}

// Tasks lists tasks for the permissions the caller holds.
func (s *Service) Tasks(ctx context.Context, perms []string, status string) ([]Task, error) {
	if status == "" {
		status = "open"
	}
	rows, err := s.pool.Query(ctx, `SELECT id::text, execution_id::text, title, detail, permission, link, status, created_at, closed_at, close_note
		FROM automation_tasks WHERE permission = ANY($1) AND ($2 = 'all' OR status = $2) ORDER BY created_at DESC LIMIT 200`, perms, status)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Task])
}

func (s *Service) CloseTask(ctx context.Context, actor string, perms []string, id, status, note string) (*Task, error) {
	if status != "done" && status != "dismissed" {
		return nil, fieldErr("status", "Choose done or dismissed.")
	}
	note = strings.TrimSpace(note)
	if len([]rune(note)) > 1000 {
		return nil, fieldErr("note", "Keep the note under 1,000 characters.")
	}
	tag, err := s.pool.Exec(ctx, `UPDATE automation_tasks SET status = $2, closed_at = now(), closed_by = $3, close_note = $4
		WHERE id::text = $1 AND status = 'open' AND permission = ANY($5)`, id, status, nilIfEmpty(actor), nilIfEmpty(note), perms)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, httpx.ErrNotFound
	}
	_ = audit.Record(ctx, s.pool, audit.Entry{ActorUserID: actor, Action: "automation.task_closed", TargetType: "automation_task", TargetID: id})
	var t Task
	err = s.pool.QueryRow(ctx, `SELECT id::text, execution_id::text, title, detail, permission, link, status, created_at, closed_at, close_note
		FROM automation_tasks WHERE id::text = $1`, id).Scan(&t.ID, &t.ExecutionID, &t.Title, &t.Detail, &t.Permission, &t.Link, &t.Status, &t.CreatedAt,
		&t.ClosedAt, &t.CloseNote)
	return &t, err
}

// Overview summarises the control centre.
type Overview struct {
	Enabled          int        `json:"enabled"`
	Disabled         int        `json:"disabled"`
	Failed           int        `json:"failed_executions"`
	AwaitingApproval int        `json:"awaiting_approval"`
	OpenTasks        int        `json:"open_tasks"`
	LastSuccess      *time.Time `json:"last_success"`
	Recent           int        `json:"executions_last_7_days"`
}

func (s *Service) Overview(ctx context.Context) (*Overview, error) {
	var o Overview
	err := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM automation_rules WHERE enabled), (SELECT count(*) FROM automation_rules WHERE NOT enabled),
		(SELECT count(*) FROM automation_executions WHERE status = 'failed'), (SELECT count(*) FROM automation_executions WHERE status = 'awaiting_approval'),
		(SELECT count(*) FROM automation_tasks WHERE status = 'open'),
		(SELECT max(finished_at) FROM automation_executions WHERE status IN ('succeeded', 'approved', 'awaiting_approval')),
		(SELECT count(*) FROM automation_executions WHERE created_at > now() - interval '7 days')`).
		Scan(&o.Enabled, &o.Disabled, &o.Failed, &o.AwaitingApproval, &o.OpenTasks, &o.LastSuccess, &o.Recent)
	return &o, err
}
