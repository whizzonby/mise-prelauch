package production

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
)

var CheckTypes = []string{"ingredient_verification", "quantity_verification", "packaging_integrity", "label_accuracy",
	"allergen_declaration", "storage_temperature", "general_inspection"}

// QualityTemplate is one check of the quality checklist, configured from an
// approved operating procedure. Criteria are what the inspector follows;
// no thresholds are invented here.
type QualityTemplate struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	CheckType    string    `json:"check_type"`
	Criteria     string    `json:"criteria"`
	Critical     bool      `json:"critical"`
	Position     int       `json:"position"`
	Active       bool      `json:"active"`
	ProcedureRef string    `json:"procedure_ref"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (s *Service) QualityTemplates(ctx context.Context) ([]QualityTemplate, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, check_type, criteria, critical, position, active, procedure_ref, updated_at
		FROM quality_check_templates ORDER BY active DESC, position, name`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[QualityTemplate])
}

type QualityTemplateInput struct {
	Name         *string `json:"name"`
	CheckType    *string `json:"check_type"`
	Criteria     *string `json:"criteria"`
	Critical     *bool   `json:"critical"`
	Position     *int    `json:"position"`
	Active       *bool   `json:"active"`
	ProcedureRef *string `json:"procedure_ref"`
}

func (s *Service) SaveQualityTemplate(ctx context.Context, actor, id string, in QualityTemplateInput) ([]QualityTemplate, error) {
	t := QualityTemplate{CheckType: "general_inspection", Active: true}
	if id != "" {
		if !uuidPattern.MatchString(id) {
			return nil, httpx.ErrNotFound
		}
		err := s.pool.QueryRow(ctx, `SELECT id, name, check_type, criteria, critical, position, active, procedure_ref, updated_at
			FROM quality_check_templates WHERE id = $1`, id).Scan(&t.ID, &t.Name, &t.CheckType, &t.Criteria, &t.Critical, &t.Position, &t.Active, &t.ProcedureRef, &t.UpdatedAt)
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
		bad("name", "Name the check (up to 100 characters).")
	}
	if in.CheckType != nil {
		t.CheckType = *in.CheckType
	}
	if !slices.Contains(CheckTypes, t.CheckType) {
		bad("check_type", "Choose the type of check.")
	}
	if in.Criteria != nil {
		t.Criteria = strings.TrimSpace(*in.Criteria)
	}
	if n := runes(t.Criteria); n < 1 || n > 2000 {
		bad("criteria", "Write what the inspector checks, from the approved procedure.")
	}
	if in.Critical != nil {
		t.Critical = *in.Critical
	}
	if in.Position != nil {
		t.Position = *in.Position
	}
	if in.Active != nil {
		t.Active = *in.Active
	}
	if in.ProcedureRef != nil {
		t.ProcedureRef = strings.TrimSpace(*in.ProcedureRef)
	}
	if runes(t.ProcedureRef) > 200 {
		bad("procedure_ref", "Keep the procedure reference under 200 characters.")
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if id == "" {
			if err := tx.QueryRow(ctx, `INSERT INTO quality_check_templates (name, check_type, criteria, critical, position, active, procedure_ref, created_by)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`, t.Name, t.CheckType, t.Criteria, t.Critical, t.Position, t.Active, t.ProcedureRef, nilIfEmpty(actor)).Scan(&id); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `UPDATE quality_check_templates SET name = $2, check_type = $3, criteria = $4, critical = $5, position = $6,
			active = $7, procedure_ref = $8, updated_at = now() WHERE id = $1`, id, t.Name, t.CheckType, t.Criteria, t.Critical, t.Position, t.Active, t.ProcedureRef); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "quality.template_saved", TargetType: "quality_template", TargetID: id,
			Metadata: map[string]any{"critical": t.Critical, "active": t.Active}})
	})
	if err != nil {
		return nil, err
	}
	return s.QualityTemplates(ctx)
}

// Check is one recorded inspection.
type Check struct {
	ID            string    `json:"id"`
	TemplateID    string    `json:"template_id"`
	Name          string    `json:"name"`
	CheckType     string    `json:"check_type"`
	Critical      bool      `json:"critical"`
	Result        string    `json:"result"`
	Notes         string    `json:"notes"`
	EvidenceRef   *string   `json:"evidence_ref"`
	InspectorName string    `json:"inspector_name"`
	CreatedAt     time.Time `json:"created_at"`
}

func (s *Service) checks(ctx context.Context, q db.Querier, batchID string) ([]Check, error) {
	rows, err := q.Query(ctx, `SELECT c.id, c.template_id, t.name, c.check_type, c.critical, c.result, c.notes, c.evidence_ref,
		COALESCE(btrim(u.first_name || ' ' || u.last_name), ''), c.created_at
		FROM quality_checks c JOIN quality_check_templates t ON t.id = c.template_id LEFT JOIN users u ON u.id = c.inspector_id
		WHERE c.batch_id = $1 ORDER BY c.created_at DESC`, batchID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Check])
}

// ChecklistRow is an active checklist item with its latest result for a
// batch: a result recorded before the batch's latest hold was resolved
// does not count, so a released hold always needs a fresh inspection.
type ChecklistRow struct {
	TemplateID string     `json:"template_id"`
	Name       string     `json:"name"`
	CheckType  string     `json:"check_type"`
	Criteria   string     `json:"criteria"`
	Critical   bool       `json:"critical"`
	Result     *string    `json:"result"`
	CheckedAt  *time.Time `json:"checked_at"`
}

func (s *Service) checklist(ctx context.Context, q db.Querier, batchID string) ([]ChecklistRow, error) {
	rows, err := q.Query(ctx, `
		SELECT t.id, t.name, t.check_type, t.criteria, t.critical, c.result, c.created_at
		FROM quality_check_templates t
		LEFT JOIN LATERAL (
			SELECT result, created_at FROM quality_checks c WHERE c.batch_id = $1 AND c.template_id = t.id
			  AND c.created_at > COALESCE((SELECT max(resolved_at) FROM quality_holds h WHERE h.batch_id = $1 AND h.status = 'released'), '-infinity')
			ORDER BY c.created_at DESC LIMIT 1) c ON true
		WHERE t.active ORDER BY t.position, t.name`, batchID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[ChecklistRow])
}

// CheckInput records an inspection.
type CheckInput struct {
	BatchID     string `json:"batch_id"`
	TemplateID  string `json:"template_id"`
	Result      string `json:"result"`
	Notes       string `json:"notes"`
	EvidenceRef string `json:"evidence_ref"`
}

// RecordCheck stores an inspection. A failed critical check puts the batch
// on quality hold at once and alerts the staff who may resolve it.
func (s *Service) RecordCheck(ctx context.Context, actor string, in CheckInput) (*Batch, error) {
	in.Notes, in.EvidenceRef = strings.TrimSpace(in.Notes), strings.TrimSpace(in.EvidenceRef)
	var f []httpx.FieldError
	bad := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	if in.Result != "pass" && in.Result != "fail" {
		bad("result", "Result is pass or fail.")
	}
	if in.Result == "fail" && runes(in.Notes) < 3 {
		bad("notes", "Describe what failed.")
	}
	if runes(in.Notes) > 2000 || runes(in.EvidenceRef) > 500 {
		bad("notes", "Keep notes under 2,000 characters and the evidence reference under 500.")
	}
	if !uuidPattern.MatchString(in.TemplateID) {
		bad("template_id", "Choose the check.")
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		b, err := s.lockBatch(ctx, tx, in.BatchID, "in_progress", "awaiting_quality_check", "quality_hold")
		if err != nil {
			return err
		}
		var name, typ string
		var critical, active bool
		if err := tx.QueryRow(ctx, `SELECT name, check_type, critical, active FROM quality_check_templates WHERE id = $1`, in.TemplateID).Scan(&name, &typ, &critical, &active); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fieldErr("template_id", "Choose the check.")
			}
			return err
		}
		if !active {
			return fieldErr("template_id", "That check is no longer on the checklist.")
		}
		var checkID string
		if err := tx.QueryRow(ctx, `INSERT INTO quality_checks (batch_id, template_id, check_type, critical, result, notes, evidence_ref, inspector_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`, b.ID, in.TemplateID, typ, critical, in.Result, in.Notes, nilIfEmpty(in.EvidenceRef), nilIfEmpty(actor)).Scan(&checkID); err != nil {
			return err
		}
		if in.Result == "fail" && critical && b.Status != "quality_hold" {
			if err := s.hold(ctx, tx, b, checkID, "Failed critical check \""+name+"\": "+in.Notes, actor); err != nil {
				return err
			}
		} else if in.Result == "fail" {
			if _, err := tx.Exec(ctx, `UPDATE production_batches SET quality_status = CASE WHEN status = 'quality_hold' THEN quality_status ELSE 'failed' END, updated_at = now() WHERE id = $1`, b.ID); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "quality.check_recorded", TargetType: "production_batch", TargetID: b.ID,
			Metadata: map[string]any{"check": name, "result": in.Result, "critical": critical}})
	})
	if err != nil {
		return nil, err
	}
	return s.Batch(ctx, in.BatchID)
}

func (s *Service) hold(ctx context.Context, tx pgx.Tx, b *Batch, checkID, why, actor string) error {
	var holdID string
	if err := tx.QueryRow(ctx, `INSERT INTO quality_holds (batch_id, check_id, reason, created_by) VALUES ($1, $2, $3, $4) RETURNING id`,
		b.ID, nilIfEmpty(checkID), why, nilIfEmpty(actor)).Scan(&holdID); err != nil {
		if db.IsUniqueViolation(err, "quality_holds_one_open") {
			return conflict("already_on_hold", "This batch is already on hold.")
		}
		return err
	}
	if err := s.setBatch(ctx, tx, b, "quality_hold", why, actor, `, quality_status = 'on_hold'`); err != nil {
		return err
	}
	if err := s.raise(ctx, tx, alert("quality_hold:"+holdID, "quality_hold", "critical", "Quality hold: "+b.Code+" "+b.MealName, why,
		"production_batch", b.ID, "quality.release_hold")); err != nil {
		return err
	}
	return event(ctx, tx, "quality.hold_created", "production_batch", b.ID, holdID, map[string]any{"hold_id": holdID, "code": b.Code, "reason": why})
}

// HoldInput puts a batch on hold by hand.
type HoldInput struct {
	BatchID string `json:"batch_id"`
	Reason  string `json:"reason"`
}

func (s *Service) PlaceHold(ctx context.Context, actor string, in HoldInput) (*Batch, error) {
	why, err := reason(in.Reason, 3)
	if err != nil {
		return nil, err
	}
	err = s.tx(ctx, func(tx pgx.Tx) error {
		b, err := s.lockBatch(ctx, tx, in.BatchID, "in_progress", "awaiting_quality_check")
		if err != nil {
			return err
		}
		if err := s.hold(ctx, tx, b, "", why, actor); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "quality.hold_created", TargetType: "production_batch", TargetID: b.ID, Reason: why})
	})
	if err != nil {
		return nil, err
	}
	return s.Batch(ctx, in.BatchID)
}

// Hold is a quality hold.
type Hold struct {
	ID              string     `json:"id"`
	BatchID         string     `json:"batch_id"`
	BatchCode       string     `json:"batch_code"`
	MealName        string     `json:"meal_name"`
	ProductionDate  string     `json:"production_date"`
	Reason          string     `json:"reason"`
	Status          string     `json:"status"`
	CreatedByName   string     `json:"created_by_name"`
	CreatedAt       time.Time  `json:"created_at"`
	ResolvedByName  string     `json:"resolved_by_name"`
	ResolvedAt      *time.Time `json:"resolved_at"`
	ResolutionNotes *string    `json:"resolution_notes"`
}

func (s *Service) Holds(ctx context.Context, openOnly bool) ([]Hold, error) {
	return s.holds(ctx, s.pool, "", openOnly)
}

func (s *Service) holds(ctx context.Context, q db.Querier, batchID string, openOnly bool) ([]Hold, error) {
	rows, err := q.Query(ctx, `
		SELECT h.id, h.batch_id, b.code, b.meal_name, b.production_date::text, h.reason, h.status,
		       COALESCE(btrim(c.first_name || ' ' || c.last_name), ''), h.created_at,
		       COALESCE(btrim(r.first_name || ' ' || r.last_name), ''), h.resolved_at, h.resolution_notes
		FROM quality_holds h JOIN production_batches b ON b.id = h.batch_id
		LEFT JOIN users c ON c.id = h.created_by LEFT JOIN users r ON r.id = h.resolved_by
		WHERE ($1 = '' OR h.batch_id::text = $1) AND (NOT $2 OR h.status = 'open')
		ORDER BY h.status = 'open' DESC, h.created_at DESC LIMIT 200`, batchID, openOnly)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Hold])
}

// ResolveInput releases or discards a held batch.
type ResolveInput struct {
	// release: the batch goes back for re-inspection; discard: it is cancelled.
	Decision string `json:"decision"`
	Notes    string `json:"notes"`
}

// ResolveHold needs quality.release_hold (checked at the route). Releasing
// does not pass the batch: the failed checks must be inspected again.
func (s *Service) ResolveHold(ctx context.Context, actor, holdID string, in ResolveInput) (*Batch, error) {
	if in.Decision == "" {
		in.Decision = "release"
	}
	if in.Decision != "release" && in.Decision != "discard" {
		return nil, fieldErr("decision", "Decision is release or discard.")
	}
	notes := strings.TrimSpace(in.Notes)
	if n := runes(notes); n < 3 || n > 1000 {
		return nil, fieldErr("notes", "Record how the hold was resolved.")
	}
	if !uuidPattern.MatchString(holdID) {
		return nil, httpx.ErrNotFound
	}
	var batchID string
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, `SELECT batch_id, status FROM quality_holds WHERE id = $1 FOR UPDATE`, holdID).Scan(&batchID, &status); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpx.ErrNotFound
			}
			return err
		}
		if status != "open" {
			return conflict("hold_resolved", "This hold is already "+status+".")
		}
		b, err := s.lockBatch(ctx, tx, batchID, "quality_hold")
		if err != nil {
			return err
		}
		final := "released"
		if in.Decision == "discard" {
			final = "discarded"
		}
		if _, err := tx.Exec(ctx, `UPDATE quality_holds SET status = $2, resolved_by = $3, resolved_at = now(), resolution_notes = $4 WHERE id = $1`,
			holdID, final, nilIfEmpty(actor), notes); err != nil {
			return err
		}
		if in.Decision == "discard" {
			if err := s.setBatch(ctx, tx, b, "cancelled", "Discarded after quality hold: "+notes, actor, `, cancelled_at = now(), cancel_reason = $3, quality_status = 'failed'`, "Discarded after quality hold"); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE production_tasks SET status = 'cancelled', updated_at = now() WHERE batch_id = $1 AND status IN ('todo', 'in_progress', 'blocked')`, b.ID); err != nil {
				return err
			}
		} else {
			next := "in_progress"
			if b.Yield != nil {
				next = "awaiting_quality_check"
			}
			if err := s.setBatch(ctx, tx, b, next, "Hold released: "+notes, actor, `, quality_status = 'pending'`); err != nil {
				return err
			}
		}
		if err := event(ctx, tx, "quality.hold_released", "production_batch", b.ID, holdID, map[string]any{"hold_id": holdID, "decision": in.Decision}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "quality.hold_" + final, TargetType: "production_batch", TargetID: b.ID, Reason: notes})
	})
	if err != nil {
		return nil, err
	}
	return s.Batch(ctx, batchID)
}
