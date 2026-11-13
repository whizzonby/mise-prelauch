package production

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
)

var (
	TaskKinds  = []string{"preparation", "portioning", "sauce", "assembly", "labelling", "packaging", "inspection", "other"}
	Priorities = []string{"low", "normal", "high"}
	taskMoves  = map[string][]string{
		"todo":        {"in_progress", "done", "blocked"},
		"in_progress": {"todo", "done", "blocked"},
		"blocked":     {"todo", "in_progress"},
		"done":        {"in_progress"},
		"cancelled":   {},
	}
)

// TaskTemplate is a kitchen task a manager configured from an approved
// operating procedure. Releasing a plan makes one task per active template
// for each batch. None are shipped: Mise's procedures are not invented here.
type TaskTemplate struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Kind             string    `json:"kind"`
	Instructions     string    `json:"instructions"`
	Position         int       `json:"position"`
	Priority         string    `json:"priority"`
	DueOffsetMinutes *int      `json:"due_offset_minutes"`
	Active           bool      `json:"active"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (s *Service) TaskTemplates(ctx context.Context) ([]TaskTemplate, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, kind, instructions, position, priority, due_offset_minutes, active, updated_at
		FROM production_task_templates ORDER BY active DESC, position, name`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[TaskTemplate])
}

type TaskTemplateInput struct {
	Name             *string `json:"name"`
	Kind             *string `json:"kind"`
	Instructions     *string `json:"instructions"`
	Position         *int    `json:"position"`
	Priority         *string `json:"priority"`
	DueOffsetMinutes *int    `json:"due_offset_minutes"`
	Active           *bool   `json:"active"`
}

func (s *Service) SaveTaskTemplate(ctx context.Context, actor, id string, in TaskTemplateInput) ([]TaskTemplate, error) {
	t := TaskTemplate{Kind: "other", Priority: "normal", Active: true}
	if id != "" {
		if !uuidPattern.MatchString(id) {
			return nil, httpx.ErrNotFound
		}
		err := s.pool.QueryRow(ctx, `SELECT id, name, kind, instructions, position, priority, due_offset_minutes, active, updated_at
			FROM production_task_templates WHERE id = $1`, id).Scan(&t.ID, &t.Name, &t.Kind, &t.Instructions, &t.Position, &t.Priority, &t.DueOffsetMinutes, &t.Active, &t.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, httpx.ErrNotFound
		} else if err != nil {
			return nil, err
		}
	}
	var f []httpx.FieldError
	bad := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	if in.Name != nil {
		t.Name = strings.TrimSpace(*in.Name)
	}
	if n := runes(t.Name); n < 1 || n > 100 {
		bad("name", "Name the task (up to 100 characters).")
	}
	if in.Kind != nil {
		t.Kind = *in.Kind
	}
	if !slices.Contains(TaskKinds, t.Kind) {
		bad("kind", "Choose the kind of task.")
	}
	if in.Instructions != nil {
		t.Instructions = strings.TrimSpace(*in.Instructions)
	}
	if runes(t.Instructions) > 1000 {
		bad("instructions", "Keep instructions under 1,000 characters.")
	}
	if in.Position != nil {
		t.Position = *in.Position
	}
	if in.Priority != nil {
		t.Priority = *in.Priority
	}
	if !slices.Contains(Priorities, t.Priority) {
		bad("priority", "Priority is low, normal or high.")
	}
	if in.DueOffsetMinutes != nil {
		if *in.DueOffsetMinutes < 0 {
			t.DueOffsetMinutes = nil
		} else if *in.DueOffsetMinutes > 2880 {
			bad("due_offset_minutes", "Due within two days of the production day's start.")
		} else {
			t.DueOffsetMinutes = in.DueOffsetMinutes
		}
	}
	if in.Active != nil {
		t.Active = *in.Active
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if id == "" {
			if err := tx.QueryRow(ctx, `INSERT INTO production_task_templates (name, kind, instructions, position, priority, due_offset_minutes, active, created_by)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`, t.Name, t.Kind, t.Instructions, t.Position, t.Priority, t.DueOffsetMinutes, t.Active, nilIfEmpty(actor)).Scan(&id); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `UPDATE production_task_templates SET name = $2, kind = $3, instructions = $4, position = $5, priority = $6,
			due_offset_minutes = $7, active = $8, updated_at = now() WHERE id = $1`, id, t.Name, t.Kind, t.Instructions, t.Position, t.Priority, t.DueOffsetMinutes, t.Active); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "production.task_template_saved", TargetType: "task_template", TargetID: id})
	})
	if err != nil {
		return nil, err
	}
	return s.TaskTemplates(ctx)
}

// generateTasks makes the tasks of every released batch of a plan, once.
func (s *Service) generateTasks(ctx context.Context, tx pgx.Tx, planID, actor string) (int64, error) {
	tag, err := tx.Exec(ctx, `
		INSERT INTO production_tasks (batch_id, template_id, title, kind, instructions, position, priority, due_at)
		SELECT b.id, t.id, t.name, t.kind, t.instructions, t.position, t.priority,
		       CASE WHEN t.due_offset_minutes IS NULL THEN NULL
		            ELSE (b.production_date::timestamp + make_interval(mins => t.due_offset_minutes)) AT TIME ZONE $2 END
		FROM production_batches b CROSS JOIN production_task_templates t
		WHERE b.plan_id = $1 AND b.status = 'released' AND t.active
		ON CONFLICT (batch_id, template_id) WHERE template_id IS NOT NULL DO NOTHING`, planID, s.Location.String())
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO production_task_events (task_id, type, to_value, actor_id)
		SELECT t.id, 'created', 'todo', $2 FROM production_tasks t JOIN production_batches b ON b.id = t.batch_id
		WHERE b.plan_id = $1 AND NOT EXISTS (SELECT 1 FROM production_task_events e WHERE e.task_id = t.id)`, planID, nilIfEmpty(actor)); err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// Task is one kitchen task.
type Task struct {
	ID            string     `json:"id"`
	BatchID       string     `json:"batch_id"`
	BatchCode     string     `json:"batch_code"`
	BatchStatus   string     `json:"batch_status"`
	MealName      string     `json:"meal_name"`
	PlannedKits   int        `json:"planned_kits"`
	Title         string     `json:"title"`
	Kind          string     `json:"kind"`
	Instructions  string     `json:"instructions"`
	Priority      string     `json:"priority"`
	Status        string     `json:"status"`
	AssignedTo    *string    `json:"assigned_to"`
	AssigneeName  string     `json:"assignee_name"`
	DueAt         *time.Time `json:"due_at"`
	StartedAt     *time.Time `json:"started_at"`
	CompletedAt   *time.Time `json:"completed_at"`
	Notes         string     `json:"notes"`
	Exception     *string    `json:"exception"`
	ProductionDay string     `json:"production_date"`
}

type TaskFilter struct {
	BatchID string
	Date    string
	Status  string
	// AssignedTo: a user id; Mine sets it to the caller.
	AssignedTo string
	// All includes done and cancelled tasks.
	All bool
}

func (s *Service) Tasks(ctx context.Context, f TaskFilter) ([]Task, error) {
	return s.tasks(ctx, s.pool, f)
}

func (s *Service) tasks(ctx context.Context, q db.Querier, f TaskFilter) ([]Task, error) {
	var batch, date, who *string
	if uuidPattern.MatchString(f.BatchID) {
		batch = &f.BatchID
	}
	if _, ok := parseDay(f.Date); ok {
		date = &f.Date
	}
	if uuidPattern.MatchString(f.AssignedTo) {
		who = &f.AssignedTo
	}
	rows, err := q.Query(ctx, `
		SELECT t.id, t.batch_id, b.code, b.status, b.meal_name, b.planned_kits, t.title, t.kind, t.instructions, t.priority, t.status,
		       t.assigned_to, COALESCE(btrim(u.first_name || ' ' || u.last_name), ''), t.due_at, t.started_at, t.completed_at, t.notes, t.exception,
		       b.production_date::text
		FROM production_tasks t JOIN production_batches b ON b.id = t.batch_id LEFT JOIN users u ON u.id = t.assigned_to
		WHERE ($1::uuid IS NULL OR t.batch_id = $1::uuid) AND ($2::date IS NULL OR b.production_date = $2::date)
		  AND ($3 = '' OR t.status = $3) AND ($4::uuid IS NULL OR t.assigned_to = $4::uuid)
		  AND ($5 OR t.status IN ('todo', 'in_progress', 'blocked'))
		ORDER BY b.production_date, CASE t.status WHEN 'blocked' THEN 0 WHEN 'in_progress' THEN 1 WHEN 'todo' THEN 2 ELSE 3 END,
		         CASE t.priority WHEN 'high' THEN 0 WHEN 'normal' THEN 1 ELSE 2 END, t.due_at NULLS LAST, b.code, t.position
		LIMIT 500`, batch, date, f.Status, who, f.All || f.Status != "")
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Task])
}

// TaskInput changes a task. Nil fields keep their value.
type TaskInput struct {
	Status *string `json:"status"`
	// "" unassigns; "me" is the caller.
	AssignedTo *string `json:"assigned_to"`
	Note       *string `json:"note"`
	// Required when blocking a task: what is wrong.
	Exception *string `json:"exception"`
}

func (s *Service) UpdateTask(ctx context.Context, p *identity.Principal, id string, in TaskInput) (*Task, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	var batchID string
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var status, batchStatus string
		var assignee *string
		if err := tx.QueryRow(ctx, `SELECT t.status, t.assigned_to, t.batch_id, b.status FROM production_tasks t JOIN production_batches b ON b.id = t.batch_id
			WHERE t.id = $1 FOR UPDATE OF t`, id).Scan(&status, &assignee, &batchID, &batchStatus); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpx.ErrNotFound
			}
			return err
		}
		if batchStatus == "completed" || batchStatus == "cancelled" || batchStatus == "planned" {
			return conflict("batch_closed", "The batch is "+human(batchStatus)+"; its tasks cannot change.")
		}
		ev := func(typ string, from, to, body *string) error {
			_, err := tx.Exec(ctx, `INSERT INTO production_task_events (task_id, type, from_value, to_value, body, actor_id) VALUES ($1, $2, $3, $4, $5, $6)`,
				id, typ, from, to, body, p.UserID)
			return err
		}
		if in.AssignedTo != nil {
			next := *in.AssignedTo
			if next == "me" {
				next = p.UserID
			}
			if next != p.UserID && next != "" && !p.Can(identity.PermProductionPlan) {
				return httpx.NewError(403, "forbidden", "Only a production manager assigns tasks to someone else. You can take a task yourself.")
			}
			var to *string
			if next != "" {
				var ok bool
				if !uuidPattern.MatchString(next) {
					return fieldErr("assigned_to", "Choose a staff member.")
				}
				if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users u JOIN user_roles ur ON ur.user_id = u.id JOIN role_permissions rp ON rp.role_key = ur.role_key
					WHERE u.id = $1 AND u.status = 'active' AND rp.permission_key = 'production.execute')`, next).Scan(&ok); err != nil {
					return err
				}
				if !ok {
					return fieldErr("assigned_to", "Assign the task to active kitchen staff.")
				}
				to = &next
			}
			if (to == nil) != (assignee == nil) || (to != nil && *to != *assignee) {
				if _, err := tx.Exec(ctx, `UPDATE production_tasks SET assigned_to = $2, updated_at = now() WHERE id = $1`, id, to); err != nil {
					return err
				}
				if err := ev("assignment", assignee, to, nil); err != nil {
					return err
				}
			}
		}
		if in.Note != nil {
			note := strings.TrimSpace(*in.Note)
			if runes(note) > 500 {
				return fieldErr("note", "Keep a note under 500 characters.")
			}
			if note != "" {
				if _, err := tx.Exec(ctx, `UPDATE production_tasks SET notes = left(CASE WHEN notes = '' THEN $2 ELSE notes || E'\n' || $2 END, 2000), updated_at = now() WHERE id = $1`, id, note); err != nil {
					return err
				}
				if err := ev("note", nil, nil, &note); err != nil {
					return err
				}
			}
		}
		if in.Status != nil && *in.Status != status {
			next := *in.Status
			if !slices.Contains(taskMoves[status], next) {
				return conflict("invalid_transition", "A task that is "+human(status)+" cannot become "+human(next)+".")
			}
			if status == "done" && !p.Can(identity.PermProductionPlan) {
				return httpx.NewError(403, "forbidden", "Only a production manager reopens a finished task.")
			}
			if batchStatus == "released" && next != "todo" {
				return conflict("batch_not_started", "Start the batch before working its tasks.")
			}
			var exc *string
			if next == "blocked" {
				e := ""
				if in.Exception != nil {
					e = strings.TrimSpace(*in.Exception)
				}
				if n := runes(e); n < 3 || n > 1000 {
					return fieldErr("exception", "Say what is stopping the task.")
				}
				exc = &e
				if err := ev("exception", nil, nil, exc); err != nil {
					return err
				}
				if err := s.raise(ctx, tx, alert("task_blocked:"+id+":"+time.Now().Format(time.RFC3339Nano), "production_task_blocked", "warning",
					"Kitchen task blocked", e, "production_batch", batchID, "production.plan")); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE production_tasks SET status = $2, exception = COALESCE($3, exception), updated_at = now(),
				started_at = CASE WHEN $2 = 'in_progress' AND started_at IS NULL THEN now() ELSE started_at END,
				completed_at = CASE WHEN $2 = 'done' THEN now() ELSE NULL END,
				assigned_to = CASE WHEN $2 IN ('in_progress', 'done') AND assigned_to IS NULL THEN $4::uuid ELSE assigned_to END
				WHERE id = $1`, id, next, exc, p.UserID); err != nil {
				return err
			}
			if err := ev("status", &status, &next, nil); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: "production.task_updated", TargetType: "production_task", TargetID: id})
	})
	if err != nil {
		return nil, err
	}
	list, err := s.tasks(ctx, s.pool, TaskFilter{BatchID: batchID, All: true})
	if err != nil {
		return nil, err
	}
	for _, t := range list {
		if t.ID == id {
			return &t, nil
		}
	}
	return nil, httpx.ErrNotFound
}
