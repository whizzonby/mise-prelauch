package production

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/vocab"
)

// Batch is the production of one meal's kits for one plan, from one exact
// recipe version.
type Batch struct {
	ID              string     `json:"id"`
	Code            string     `json:"code"`
	PlanID          string     `json:"plan_id"`
	PlanNumber      string     `json:"plan_number"`
	Week            string     `json:"week"`
	MealID          string     `json:"meal_id"`
	MealName        string     `json:"meal_name"`
	RecipeID        string     `json:"recipe_id"`
	RecipeName      string     `json:"recipe_name"`
	RecipeVersionID string     `json:"recipe_version_id"`
	RecipeVersion   int        `json:"recipe_version"`
	ProductionDate  string     `json:"production_date"`
	PlannedKits     int        `json:"planned_kits"`
	ActualKits      *int       `json:"actual_kits"`
	AssignedTeam    string     `json:"assigned_team"`
	Status          string     `json:"status"`
	QualityStatus   string     `json:"quality_status"`
	PackagingStatus string     `json:"packaging_status"`
	StartedAt       *time.Time `json:"started_at"`
	CompletedAt     *time.Time `json:"completed_at"`
	CancelReason    *string    `json:"cancel_reason"`
	TasksOpen       int        `json:"tasks_open"`
	TasksDone       int        `json:"tasks_done"`
	// Detail only.
	Yield     *Yield         `json:"yield,omitempty"`
	Packaging *Packaging     `json:"packaging,omitempty"`
	Checks    []Check        `json:"checks,omitempty"`
	Holds     []Hold         `json:"holds,omitempty"`
	Tasks     []Task         `json:"tasks,omitempty"`
	Waste     []Waste        `json:"waste,omitempty"`
	History   []BatchEvent   `json:"history,omitempty"`
	Checklist []ChecklistRow `json:"checklist,omitempty"`
	// What still stands between the batch and completion.
	Remaining []string `json:"remaining,omitempty"`
}

type BatchEvent struct {
	From      *string   `json:"from"`
	To        string    `json:"to"`
	Note      *string   `json:"note"`
	ActorName string    `json:"actor_name"`
	CreatedAt time.Time `json:"created_at"`
}

type Yield struct {
	PlannedKits  int       `json:"planned_kits"`
	ProducedKits int       `json:"produced_kits"`
	RejectedKits int       `json:"rejected_kits"`
	ReworkedKits int       `json:"reworked_kits"`
	AcceptedKits int       `json:"accepted_kits"`
	Notes        string    `json:"notes"`
	RecordedAt   time.Time `json:"recorded_at"`
}

const batchCols = `b.id, b.code, b.plan_id, p.number, p.week_start::text, b.meal_id, b.meal_name, v.recipe_id, r.name, b.recipe_version_id, v.number,
	b.production_date::text, b.planned_kits, b.actual_kits, b.assigned_team, b.status, b.quality_status, b.packaging_status,
	b.started_at, b.completed_at, b.cancel_reason,
	(SELECT count(*) FROM production_tasks t WHERE t.batch_id = b.id AND t.status IN ('todo', 'in_progress', 'blocked'))::int,
	(SELECT count(*) FROM production_tasks t WHERE t.batch_id = b.id AND t.status = 'done')::int`

const batchFrom = ` FROM production_batches b JOIN production_plans p ON p.id = b.plan_id
	JOIN recipe_versions v ON v.id = b.recipe_version_id JOIN recipes r ON r.id = v.recipe_id`

func scanBatch(r pgx.Row) (Batch, error) {
	var b Batch
	err := r.Scan(&b.ID, &b.Code, &b.PlanID, &b.PlanNumber, &b.Week, &b.MealID, &b.MealName, &b.RecipeID, &b.RecipeName, &b.RecipeVersionID,
		&b.RecipeVersion, &b.ProductionDate, &b.PlannedKits, &b.ActualKits, &b.AssignedTeam, &b.Status, &b.QualityStatus, &b.PackagingStatus,
		&b.StartedAt, &b.CompletedAt, &b.CancelReason, &b.TasksOpen, &b.TasksDone)
	return b, err
}

// BatchFilter narrows the batch list.
type BatchFilter struct {
	PlanID   string
	Status   string
	From, To string
	// Open: not completed or cancelled.
	Open bool
}

func (s *Service) Batches(ctx context.Context, f BatchFilter) ([]Batch, error) {
	return s.batches(ctx, s.pool, f)
}

func (s *Service) batches(ctx context.Context, q db.Querier, f BatchFilter) ([]Batch, error) {
	var plan, from, to *string
	if uuidPattern.MatchString(f.PlanID) {
		plan = &f.PlanID
	}
	if _, ok := parseDay(f.From); ok {
		from = &f.From
	}
	if _, ok := parseDay(f.To); ok {
		to = &f.To
	}
	rows, err := q.Query(ctx, `SELECT `+batchCols+batchFrom+`
		WHERE ($1::uuid IS NULL OR b.plan_id = $1::uuid) AND ($2 = '' OR b.status = $2)
		  AND ($3::date IS NULL OR b.production_date >= $3::date) AND ($4::date IS NULL OR b.production_date <= $4::date)
		  AND (NOT $5 OR b.status NOT IN ('completed', 'cancelled'))
		ORDER BY b.production_date, b.code LIMIT 500`, plan, f.Status, from, to, f.Open)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Batch{}
	for rows.Next() {
		b, err := scanBatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Service) Batch(ctx context.Context, id string) (*Batch, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	return s.batch(ctx, s.pool, id)
}

func (s *Service) batch(ctx context.Context, q db.Querier, id string) (*Batch, error) {
	b, err := scanBatch(q.QueryRow(ctx, `SELECT `+batchCols+batchFrom+` WHERE b.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var y Yield
	err = q.QueryRow(ctx, `SELECT planned_kits, produced_kits, rejected_kits, reworked_kits, accepted_kits, notes, recorded_at
		FROM production_yield_records WHERE batch_id = $1`, id).Scan(&y.PlannedKits, &y.ProducedKits, &y.RejectedKits, &y.ReworkedKits, &y.AcceptedKits, &y.Notes, &y.RecordedAt)
	if err == nil {
		b.Yield = &y
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if b.Packaging, err = s.packaging(ctx, q, id); err != nil {
		return nil, err
	}
	if b.Checks, err = s.checks(ctx, q, id); err != nil {
		return nil, err
	}
	if b.Holds, err = s.holds(ctx, q, id, false); err != nil {
		return nil, err
	}
	if b.Tasks, err = s.tasks(ctx, q, TaskFilter{BatchID: id, All: true}); err != nil {
		return nil, err
	}
	if b.Waste, err = s.waste(ctx, q, WasteFilter{BatchID: id}); err != nil {
		return nil, err
	}
	if b.Checklist, err = s.checklist(ctx, q, id); err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `SELECT e.from_status, e.to_status, e.note, COALESCE(btrim(u.first_name || ' ' || u.last_name), ''), e.created_at
		FROM production_batch_events e LEFT JOIN users u ON u.id = e.actor_id WHERE e.batch_id = $1 ORDER BY e.id`, id)
	if err != nil {
		return nil, err
	}
	if b.History, err = pgx.CollectRows(rows, pgx.RowToStructByPos[BatchEvent]); err != nil {
		return nil, err
	}
	b.Remaining = remaining(&b)
	return &b, nil
}

// remaining lists what completion still needs, in the kitchen's words.
func remaining(b *Batch) []string {
	out := []string{}
	switch b.Status {
	case "completed", "cancelled":
		return out
	case "planned":
		return append(out, "The plan has not been released to the kitchen.")
	case "released":
		out = append(out, "Start the batch.")
	}
	if b.Yield == nil {
		out = append(out, "Record the kits produced.")
	}
	for _, h := range b.Holds {
		if h.Status == "open" {
			out = append(out, "Quality hold: "+h.Reason)
		}
	}
	if len(b.Checklist) == 0 {
		out = append(out, "No quality checklist is configured. A manager must add one from the approved procedure.")
	}
	for _, c := range b.Checklist {
		switch {
		case c.Result == nil:
			out = append(out, "Quality check not done: "+c.Name+".")
		case c.Critical && *c.Result != "pass":
			out = append(out, "Critical quality check failed: "+c.Name+". Re-inspect after the hold is resolved.")
		}
	}
	if b.Packaging == nil || b.Packaging.CompletedAt == nil {
		out = append(out, "Finish packaging and verify labels.")
	}
	return out
}

func (s *Service) addBatchEvent(ctx context.Context, q db.Querier, id string, from, to, note, actor string) error {
	_, err := q.Exec(ctx, `INSERT INTO production_batch_events (batch_id, from_status, to_status, note, actor_id) VALUES ($1, $2, $3, $4, $5)`,
		id, nilIfEmpty(from), to, nilIfEmpty(note), nilIfEmpty(actor))
	return err
}

// lockBatch loads and locks a batch, checking its status.
func (s *Service) lockBatch(ctx context.Context, tx pgx.Tx, id string, from ...string) (*Batch, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM production_batches WHERE id = $1 FOR UPDATE`, id).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, httpx.ErrNotFound
		}
		return nil, err
	}
	if len(from) > 0 {
		ok := false
		for _, f := range from {
			ok = ok || f == status
		}
		if !ok {
			return nil, conflict("invalid_transition", "This batch is "+human(status)+".")
		}
	}
	return s.batch(ctx, tx, id)
}

func (s *Service) setBatch(ctx context.Context, tx pgx.Tx, b *Batch, status, note, actor, extra string, args ...any) error {
	if _, err := tx.Exec(ctx, `UPDATE production_batches SET status = $2, updated_at = now()`+extra+` WHERE id = $1`, append([]any{b.ID, status}, args...)...); err != nil {
		return err
	}
	if status != b.Status {
		return s.addBatchEvent(ctx, tx, b.ID, b.Status, status, note, actor)
	}
	return nil
}

// StartInput optionally names the team.
type StartInput struct {
	AssignedTeam string `json:"assigned_team"`
}

// Start begins work on a released batch.
func (s *Service) Start(ctx context.Context, actor, id string, in StartInput) (*Batch, error) {
	team := strings.TrimSpace(in.AssignedTeam)
	if runes(team) > 60 {
		return nil, fieldErr("assigned_team", "Keep the team name under 60 characters.")
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		b, err := s.lockBatch(ctx, tx, id, "released")
		if err != nil {
			return err
		}
		extra := `, started_at = now()`
		args := []any{}
		if team != "" {
			extra += `, assigned_team = $3`
			args = append(args, team)
		}
		if err := s.setBatch(ctx, tx, b, "in_progress", "", actor, extra, args...); err != nil {
			return err
		}
		ids, err := s.planOrders(ctx, tx, b.PlanID, b.MealID)
		if err != nil {
			return err
		}
		if _, err := s.Orders.Advance(ctx, tx, ids, []string{"confirmed", "awaiting_production"}, "in_production", "Batch "+b.Code+" started", actor); err != nil {
			return err
		}
		if err := event(ctx, tx, "production.batch_started", "production_batch", id, "", map[string]any{"code": b.Code, "meal_id": b.MealID}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "production.batch_started", TargetType: "production_batch", TargetID: id})
	})
	if err != nil {
		return nil, err
	}
	return s.Batch(ctx, id)
}

// OutputInput records what the kitchen produced. Reworked kits are kits
// that failed and were fixed: they are part of produced and of accepted,
// counted once. Rejected kits are never available for fulfilment.
type OutputInput struct {
	ProducedKits int    `json:"produced_kits"`
	RejectedKits int    `json:"rejected_kits"`
	ReworkedKits int    `json:"reworked_kits"`
	Notes        string `json:"notes"`
}

// RecordOutput reconciles a batch's yield and sends it to quality checks.
// It can be corrected until the batch completes.
func (s *Service) RecordOutput(ctx context.Context, actor, id string, in OutputInput) (*Batch, error) {
	in.Notes = strings.TrimSpace(in.Notes)
	var f []httpx.FieldError
	bad := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	if in.ProducedKits < 0 || in.ProducedKits > 100000 {
		bad("produced_kits", "Enter how many kits were assembled.")
	}
	if in.RejectedKits < 0 || in.RejectedKits > in.ProducedKits {
		bad("rejected_kits", "Rejected kits cannot be more than the kits produced.")
	}
	if in.ReworkedKits < 0 || in.ReworkedKits > in.ProducedKits-in.RejectedKits {
		bad("reworked_kits", "Reworked kits are part of the accepted kits, so they cannot be more than produced minus rejected.")
	}
	if runes(in.Notes) > 1000 {
		bad("notes", "Keep notes under 1,000 characters.")
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	accepted := in.ProducedKits - in.RejectedKits
	err := s.tx(ctx, func(tx pgx.Tx) error {
		b, err := s.lockBatch(ctx, tx, id, "in_progress", "awaiting_quality_check", "quality_hold")
		if err != nil {
			return err
		}
		if b.Packaging != nil && b.Packaging.CompletedAt != nil {
			return conflict("packed", "Packaging is already complete for this batch; output can no longer change.")
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO production_yield_records (batch_id, planned_kits, produced_kits, rejected_kits, reworked_kits, accepted_kits, notes, recorded_by, recorded_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
			ON CONFLICT (batch_id) DO UPDATE SET produced_kits = EXCLUDED.produced_kits, rejected_kits = EXCLUDED.rejected_kits,
			  reworked_kits = EXCLUDED.reworked_kits, accepted_kits = EXCLUDED.accepted_kits, notes = EXCLUDED.notes,
			  recorded_by = EXCLUDED.recorded_by, recorded_at = now()`,
			id, b.PlannedKits, in.ProducedKits, in.RejectedKits, in.ReworkedKits, accepted, in.Notes, nilIfEmpty(actor)); err != nil {
			return err
		}
		next := b.Status
		if b.Status == "in_progress" {
			next = "awaiting_quality_check"
		}
		if err := s.setBatch(ctx, tx, b, next, "", actor, `, actual_kits = $3`, accepted); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE packaging_records SET required_kits = $2, updated_at = now() WHERE batch_id = $1`, id, accepted); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "production.output_recorded", TargetType: "production_batch", TargetID: id,
			Metadata: map[string]any{"produced": in.ProducedKits, "rejected": in.RejectedKits, "reworked": in.ReworkedKits, "accepted": accepted}})
	})
	if err != nil {
		return nil, err
	}
	return s.Batch(ctx, id)
}

// Complete marks a batch ready for fulfilment. It is refused while
// anything in remaining() is outstanding: a planned quantity alone never
// completes a batch.
func (s *Service) Complete(ctx context.Context, actor, id string) (*Batch, error) {
	err := s.tx(ctx, func(tx pgx.Tx) error {
		b, err := s.lockBatch(ctx, tx, id)
		if err != nil {
			return err
		}
		if b.Status == "quality_hold" {
			return conflict("quality_hold", "This batch is on quality hold and cannot be released for fulfilment until the hold is resolved.")
		}
		if b.Status != "awaiting_quality_check" {
			return conflict("invalid_transition", "This batch is "+human(b.Status)+".")
		}
		if rem := remaining(b); len(rem) > 0 {
			return &httpx.Error{Status: 422, Code: "batch_incomplete", Message: strings.Join(rem, " ")}
		}
		if b.Packaging.PackedKits != b.Yield.AcceptedKits {
			return &httpx.Error{Status: 422, Code: "batch_incomplete", Message: fmt.Sprintf("Packed kits (%d) must equal accepted kits (%d).", b.Packaging.PackedKits, b.Yield.AcceptedKits)}
		}
		if err := s.setBatch(ctx, tx, b, "completed", "", actor, `, completed_at = now(), quality_status = 'passed'`); err != nil {
			return err
		}
		payload := map[string]any{"code": b.Code, "meal_id": b.MealID, "recipe_version_id": b.RecipeVersionID, "accepted_kits": b.Yield.AcceptedKits,
			"production_date": b.ProductionDate, "plan_id": b.PlanID}
		if err := event(ctx, tx, "production.batch_completed", "production_batch", id, "", payload); err != nil {
			return err
		}
		if err := event(ctx, tx, "production.ready_for_fulfillment", "production_batch", id, "", payload); err != nil {
			return err
		}
		// An order is ready when every batch making its meals is complete.
		ids, err := s.readyOrders(ctx, tx, b.PlanID)
		if err != nil {
			return err
		}
		if _, err := s.Orders.Advance(ctx, tx, ids, []string{"awaiting_production", "in_production"}, "ready_for_fulfillment", "All batches for this order are complete", actor); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "production.batch_completed", TargetType: "production_batch", TargetID: id,
			Metadata: map[string]any{"accepted_kits": b.Yield.AcceptedKits}})
	})
	if err != nil {
		return nil, err
	}
	return s.Batch(ctx, id)
}

func (s *Service) readyOrders(ctx context.Context, q db.Querier, planID string) ([]string, error) {
	rows, err := q.Query(ctx, `
		SELECT l.order_id::text FROM production_demand_lines l
		JOIN production_plans p ON p.snapshot_id = l.snapshot_id
		WHERE p.id = $1
		GROUP BY l.order_id
		HAVING bool_and(EXISTS (SELECT 1 FROM production_batches x WHERE x.plan_id = p.id AND x.meal_id = l.meal_id AND x.status = 'completed'))`, planID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// CancelBatch stops a batch that has not completed. Its history is kept.
func (s *Service) CancelBatch(ctx context.Context, actor, id, why string) (*Batch, error) {
	why, err := reason(why, 3)
	if err != nil {
		return nil, err
	}
	err = s.tx(ctx, func(tx pgx.Tx) error {
		b, err := s.lockBatch(ctx, tx, id, "planned", "released", "in_progress", "awaiting_quality_check", "quality_hold")
		if err != nil {
			return err
		}
		if err := s.setBatch(ctx, tx, b, "cancelled", why, actor, `, cancelled_at = now(), cancel_reason = $3`, why); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE production_tasks SET status = 'cancelled', updated_at = now() WHERE batch_id = $1 AND status IN ('todo', 'in_progress', 'blocked')`, id); err != nil {
			return err
		}
		if err := s.raise(ctx, tx, alert("batch_cancelled:"+id, "production_batch_cancelled", "warning", "Batch "+b.Code+" cancelled: "+b.MealName, why,
			"production_batch", id, "production.approve")); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "production.batch_cancelled", TargetType: "production_batch", TargetID: id, Reason: why})
	})
	if err != nil {
		return nil, err
	}
	return s.Batch(ctx, id)
}

// Label is the data printed on a kit's label, all from the batch's exact
// approved recipe version. It is not a legally reviewed label.
type Label struct {
	MealName       string   `json:"meal_name"`
	BatchCode      string   `json:"batch_code"`
	RecipeCode     string   `json:"recipe_code"`
	RecipeVersion  int      `json:"recipe_version"`
	Ingredients    []string `json:"ingredients"`
	Contains       []string `json:"allergens_contains"`
	MayContain     []string `json:"allergens_may_contain"`
	Storage        string   `json:"storage_instructions"`
	Preparation    string   `json:"preparation_instructions"`
	ProductionDate string   `json:"production_date"`
	UseBy          *string  `json:"use_by"`
	UseByNote      string   `json:"use_by_note"`
	TraceabilityID string   `json:"traceability_id"`
	ComplianceNote string   `json:"compliance_note"`
}

func (s *Service) Label(ctx context.Context, id string) (*Label, error) {
	b, err := s.Batch(ctx, id)
	if err != nil {
		return nil, err
	}
	v, err := s.Culinary.VersionByID(ctx, s.pool, b.RecipeVersionID)
	if err != nil {
		return nil, err
	}
	var code string
	if err := s.pool.QueryRow(ctx, `SELECT code FROM recipes WHERE id = $1`, v.RecipeID).Scan(&code); err != nil {
		return nil, err
	}
	l := &Label{MealName: b.MealName, BatchCode: b.Code, RecipeCode: code, RecipeVersion: v.Number, Ingredients: []string{},
		Contains: []string{}, MayContain: []string{}, Storage: v.StorageInstructions, Preparation: v.CustomerInstructions,
		ProductionDate: b.ProductionDate, TraceabilityID: b.Code + "/" + code + "-v" + fmt.Sprint(v.Number),
		UseByNote:      "Not set: no approved shelf-life or use-by policy has been recorded.",
		ComplianceNote: "Generated from the approved recipe version. Not reviewed for labelling compliance."}
	// Ingredient declaration in recipe order: a descending-weight order needs
	// every line in a common mass unit, which the recipe may not provide.
	for _, line := range v.Lines {
		name := line.IngredientName
		if line.Preparation != "" {
			name += " (" + line.Preparation + ")"
		}
		l.Ingredients = append(l.Ingredients, name)
	}
	for _, a := range v.Contains {
		l.Contains = append(l.Contains, vocab.Allergens.Label(a))
	}
	for _, a := range v.MayContain {
		l.MayContain = append(l.MayContain, vocab.Allergens.Label(a))
	}
	return l, nil
}
