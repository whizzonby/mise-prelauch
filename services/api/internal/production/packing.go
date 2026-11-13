package production

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/qty"
)

// Packaging is a batch's packing record. Packaging stock is Sprint 6's;
// this records only what was packed, how and by whom.
type Packaging struct {
	PackagingType  string     `json:"packaging_type"`
	RequiredKits   int        `json:"required_kits"`
	PackedKits     int        `json:"packed_kits"`
	LabelsVerified bool       `json:"labels_verified"`
	AssignedTo     *string    `json:"assigned_to"`
	AssigneeName   string     `json:"assignee_name"`
	Exceptions     string     `json:"exceptions"`
	CompletedAt    *time.Time `json:"completed_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (s *Service) packaging(ctx context.Context, q db.Querier, batchID string) (*Packaging, error) {
	var p Packaging
	err := q.QueryRow(ctx, `SELECT k.packaging_type, k.required_kits, k.packed_kits, k.labels_verified, k.assigned_to,
		COALESCE(btrim(u.first_name || ' ' || u.last_name), ''), k.exceptions, k.completed_at, k.updated_at
		FROM packaging_records k LEFT JOIN users u ON u.id = k.assigned_to WHERE k.batch_id = $1`, batchID).Scan(
		&p.PackagingType, &p.RequiredKits, &p.PackedKits, &p.LabelsVerified, &p.AssignedTo, &p.AssigneeName, &p.Exceptions, &p.CompletedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &p, err
}

// PackagingInput records packing progress; Complete finishes it.
type PackagingInput struct {
	PackagingType  *string `json:"packaging_type"`
	PackedKits     *int    `json:"packed_kits"`
	LabelsVerified *bool   `json:"labels_verified"`
	Exceptions     *string `json:"exceptions"`
	// "me" or "" (unassign); assigning others needs production.plan (checked by the handler).
	AssignedTo *string `json:"assigned_to"`
	Complete   bool    `json:"complete"`
}

func (s *Service) SavePackaging(ctx context.Context, actor, id string, in PackagingInput) (*Batch, error) {
	err := s.tx(ctx, func(tx pgx.Tx) error {
		b, err := s.lockBatch(ctx, tx, id, "in_progress", "awaiting_quality_check", "quality_hold")
		if err != nil {
			return err
		}
		if b.Status == "quality_hold" {
			return conflict("quality_hold", "This batch is on quality hold. Packing waits until the hold is resolved.")
		}
		cur := b.Packaging
		if cur == nil {
			cur = &Packaging{RequiredKits: b.PlannedKits}
			if b.Yield != nil {
				cur.RequiredKits = b.Yield.AcceptedKits
			}
		}
		if cur.CompletedAt != nil {
			return conflict("packed", "Packaging is already complete for this batch.")
		}
		if in.PackagingType != nil {
			cur.PackagingType = strings.TrimSpace(*in.PackagingType)
			if runes(cur.PackagingType) > 100 {
				return fieldErr("packaging_type", "Keep the packaging type under 100 characters.")
			}
		}
		if in.PackedKits != nil {
			if *in.PackedKits < 0 || *in.PackedKits > 100000 {
				return fieldErr("packed_kits", "Enter the kits packed.")
			}
			cur.PackedKits = *in.PackedKits
		}
		if in.LabelsVerified != nil {
			cur.LabelsVerified = *in.LabelsVerified
		}
		if in.Exceptions != nil {
			cur.Exceptions = strings.TrimSpace(*in.Exceptions)
			if runes(cur.Exceptions) > 1000 {
				return fieldErr("exceptions", "Keep exceptions under 1,000 characters.")
			}
		}
		if in.AssignedTo != nil {
			switch *in.AssignedTo {
			case "":
				cur.AssignedTo = nil
			case "me":
				cur.AssignedTo = &actor
			default:
				if !uuidPattern.MatchString(*in.AssignedTo) {
					return fieldErr("assigned_to", "Choose a staff member.")
				}
				cur.AssignedTo = in.AssignedTo
			}
		}
		if in.Complete {
			var probs []string
			if b.Yield == nil {
				probs = append(probs, "Record the kits produced first.")
			} else if cur.PackedKits != b.Yield.AcceptedKits {
				probs = append(probs, "Packed kits must equal the accepted kits ("+strconv.Itoa(b.Yield.AcceptedKits)+"). Rejected kits are never packed for customers.")
			}
			if !cur.LabelsVerified {
				probs = append(probs, "Verify the labels against the approved recipe before finishing.")
			}
			if len(probs) > 0 {
				return &httpx.Error{Status: 422, Code: "packaging_incomplete", Message: strings.Join(probs, " ")}
			}
		}
		status := "in_progress"
		var completed any
		if in.Complete {
			status, completed = "completed", time.Now()
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO packaging_records (batch_id, packaging_type, required_kits, packed_kits, labels_verified, assigned_to, exceptions, completed_at, updated_by, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now())
			ON CONFLICT (batch_id) DO UPDATE SET packaging_type = EXCLUDED.packaging_type, required_kits = EXCLUDED.required_kits,
			  packed_kits = EXCLUDED.packed_kits, labels_verified = EXCLUDED.labels_verified, assigned_to = EXCLUDED.assigned_to,
			  exceptions = EXCLUDED.exceptions, completed_at = EXCLUDED.completed_at, updated_by = EXCLUDED.updated_by, updated_at = now()`,
			id, cur.PackagingType, cur.RequiredKits, cur.PackedKits, cur.LabelsVerified, cur.AssignedTo, cur.Exceptions, completed, nilIfEmpty(actor)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE production_batches SET packaging_status = $2, updated_at = now() WHERE id = $1`, id, status); err != nil {
			return err
		}
		if in.Complete {
			if err := event(ctx, tx, "packaging.completed", "production_batch", id, "", map[string]any{"code": b.Code, "packed_kits": cur.PackedKits}); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "production.packaging_" + map[bool]string{true: "completed", false: "updated"}[in.Complete],
			TargetType: "production_batch", TargetID: id, Metadata: map[string]any{"packed_kits": cur.PackedKits, "labels_verified": cur.LabelsVerified}})
	})
	if err != nil {
		return nil, err
	}
	return s.Batch(ctx, id)
}

var WasteCategories = []string{"preparation_loss", "spoilage", "quality_rejection", "packaging_damage", "overproduction", "other"}

// Waste is one waste record. Quantities are what staff weighed or counted.
type Waste struct {
	ID             string    `json:"id"`
	BatchID        *string   `json:"batch_id"`
	BatchCode      *string   `json:"batch_code"`
	IngredientID   *string   `json:"ingredient_id"`
	IngredientName *string   `json:"ingredient_name"`
	Item           string    `json:"item"`
	Quantity       string    `json:"quantity"`
	Unit           string    `json:"unit"`
	Category       string    `json:"category"`
	Reason         string    `json:"reason"`
	RecordedByName string    `json:"recorded_by_name"`
	CreatedAt      time.Time `json:"created_at"`
}

type WasteFilter struct {
	BatchID  string
	From, To string
}

func (s *Service) Waste(ctx context.Context, f WasteFilter) ([]Waste, error) {
	return s.waste(ctx, s.pool, f)
}

func (s *Service) waste(ctx context.Context, q db.Querier, f WasteFilter) ([]Waste, error) {
	var from, to *time.Time
	if d, ok := parseDay(f.From); ok {
		x := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, s.Location)
		from = &x
	}
	if d, ok := parseDay(f.To); ok {
		x := time.Date(d.Year(), d.Month(), d.Day()+1, 0, 0, 0, 0, s.Location)
		to = &x
	}
	rows, err := q.Query(ctx, `
		SELECT w.id, w.batch_id, b.code, w.ingredient_id, i.name, w.item, trim_scale(w.quantity)::text, w.unit, w.category, w.reason,
		       COALESCE(btrim(u.first_name || ' ' || u.last_name), ''), w.created_at
		FROM production_waste w LEFT JOIN production_batches b ON b.id = w.batch_id LEFT JOIN ingredients i ON i.id = w.ingredient_id
		LEFT JOIN users u ON u.id = w.recorded_by
		WHERE ($1 = '' OR w.batch_id::text = $1) AND ($2::timestamptz IS NULL OR w.created_at >= $2) AND ($3::timestamptz IS NULL OR w.created_at < $3)
		ORDER BY w.created_at DESC LIMIT 500`, f.BatchID, from, to)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Waste])
}

type WasteInput struct {
	BatchID      string `json:"batch_id"`
	IngredientID string `json:"ingredient_id"`
	Item         string `json:"item"`
	Quantity     string `json:"quantity"`
	Unit         string `json:"unit"`
	Category     string `json:"category"`
	Reason       string `json:"reason"`
}

func (s *Service) RecordWaste(ctx context.Context, actor string, in WasteInput) (*Waste, error) {
	in.Item, in.Reason = strings.TrimSpace(in.Item), strings.TrimSpace(in.Reason)
	var f []httpx.FieldError
	bad := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	amount, err := qty.Parse(in.Quantity)
	if err != nil || amount.Sign() <= 0 || amount.Cmp(qty.Int(10_000_000)) >= 0 {
		bad("quantity", "Enter the quantity wasted, as weighed or counted.")
	}
	if !slices.Contains(WasteCategories, in.Category) {
		bad("category", "Choose a category.")
	}
	if n := runes(in.Reason); n < 1 || n > 500 {
		bad("reason", "Say what happened.")
	}
	if in.IngredientID == "" && in.Item == "" {
		bad("item", "Choose an ingredient or name what was wasted.")
	}
	if runes(in.Item) > 100 {
		bad("item", "Keep the item under 100 characters.")
	}
	if in.IngredientID != "" && !uuidPattern.MatchString(in.IngredientID) {
		bad("ingredient_id", "Choose an ingredient.")
	}
	if in.BatchID != "" && !uuidPattern.MatchString(in.BatchID) {
		bad("batch_id", "Choose a batch.")
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	var id string
	err = s.tx(ctx, func(tx pgx.Tx) error {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM units_of_measure WHERE code = $1)`, in.Unit).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return fieldErr("unit", "Choose a unit.")
		}
		if in.BatchID != "" {
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM production_batches WHERE id = $1)`, in.BatchID).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return fieldErr("batch_id", "Batch not found.")
			}
		}
		if in.IngredientID != "" {
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM ingredients WHERE id = $1)`, in.IngredientID).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return fieldErr("ingredient_id", "Ingredient not found.")
			}
		}
		if err := tx.QueryRow(ctx, `INSERT INTO production_waste (batch_id, ingredient_id, item, quantity, unit, category, reason, recorded_by)
			VALUES ($1, $2, $3, $4::numeric, $5, $6, $7, $8) RETURNING id`, nilIfEmpty(in.BatchID), nilIfEmpty(in.IngredientID), in.Item,
			amount.String(), in.Unit, in.Category, in.Reason, nilIfEmpty(actor)).Scan(&id); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "production.waste_recorded", TargetType: "production_waste", TargetID: id,
			Metadata: map[string]any{"category": in.Category, "quantity": amount.String(), "unit": in.Unit}})
	})
	if err != nil {
		return nil, err
	}
	rows, err := s.waste(ctx, s.pool, WasteFilter{})
	if err != nil {
		return nil, err
	}
	for _, w := range rows {
		if w.ID == id {
			return &w, nil
		}
	}
	return nil, httpx.ErrNotFound
}
