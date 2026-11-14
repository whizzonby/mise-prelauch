package inventory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/qty"
	"mise.tt/api/internal/production"
	"mise.tt/api/internal/suppliers"
)

// ---------------------------------------------------------------- counts

// Count is a physical stock count of one location.
type Count struct {
	ID              string      `json:"id"`
	Number          string      `json:"number"`
	LocationID      string      `json:"location_id"`
	LocationName    string      `json:"location_name"`
	Status          string      `json:"status"`
	Notes           string      `json:"notes"`
	StartedByName   string      `json:"started_by_name"`
	SubmittedByName *string     `json:"submitted_by_name"`
	PostedByName    *string     `json:"posted_by_name"`
	CreatedAt       time.Time   `json:"created_at"`
	SubmittedAt     *time.Time  `json:"submitted_at"`
	PostedAt        *time.Time  `json:"posted_at"`
	Lines           []CountLine `json:"lines"`
	// Lots in the location not yet counted.
	ToCount []LotRef `json:"to_count"`
}

// CountLine is one counted lot.
type CountLine struct {
	ID             string `json:"id"`
	LotID          string `json:"lot_id"`
	LotCode        string `json:"lot_code"`
	IngredientName string `json:"ingredient_name"`
	Unit           string `json:"unit"`
	// The ledger balance when this line was counted.
	Expected string `json:"expected"`
	Counted  string `json:"counted"`
	Variance string `json:"variance"`
	// Movements on the lot since it was counted (kept when the count is posted).
	MovedSince    bool      `json:"moved_since"`
	CountedByName string    `json:"counted_by_name"`
	CountedAt     time.Time `json:"counted_at"`
}

func (s *Service) Counts(ctx context.Context) ([]Count, error) {
	rows, err := s.pool.Query(ctx, `SELECT id FROM stock_counts ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	out := []Count{}
	for _, id := range ids {
		c, err := s.count(ctx, s.pool, id)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, nil
}

func (s *Service) Count(ctx context.Context, id string) (*Count, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	return s.count(ctx, s.pool, id)
}

func (s *Service) count(ctx context.Context, q db.Querier, id string) (*Count, error) {
	var c Count
	err := q.QueryRow(ctx, `SELECT c.id, c.number, c.location_id, l.name, c.status, c.notes, COALESCE(btrim(a.first_name || ' ' || a.last_name), ''),
		CASE WHEN c.submitted_by IS NULL THEN NULL ELSE COALESCE(btrim(b.first_name || ' ' || b.last_name), '') END,
		CASE WHEN c.posted_by IS NULL THEN NULL ELSE COALESCE(btrim(p.first_name || ' ' || p.last_name), '') END,
		c.created_at, c.submitted_at, c.posted_at
		FROM stock_counts c JOIN storage_locations l ON l.id = c.location_id LEFT JOIN users a ON a.id = c.started_by
		LEFT JOIN users b ON b.id = c.submitted_by LEFT JOIN users p ON p.id = c.posted_by WHERE c.id = $1`, id).Scan(
		&c.ID, &c.Number, &c.LocationID, &c.LocationName, &c.Status, &c.Notes, &c.StartedByName, &c.SubmittedByName, &c.PostedByName,
		&c.CreatedAt, &c.SubmittedAt, &c.PostedAt)
	if err != nil {
		return nil, notFoundIf(err)
	}
	rows, err := q.Query(ctx, `SELECT cl.id, cl.lot_id, l.code, i.name, l.unit, trim_scale(cl.expected)::text, trim_scale(cl.counted)::text,
		trim_scale(cl.counted - cl.expected)::text,
		EXISTS (SELECT 1 FROM inventory_movements m WHERE m.lot_id = cl.lot_id AND m.id > cl.last_movement_id AND m.on_hand_delta <> 0),
		COALESCE(btrim(u.first_name || ' ' || u.last_name), ''), cl.counted_at
		FROM stock_count_lines cl JOIN inventory_lots l ON l.id = cl.lot_id JOIN ingredients i ON i.id = l.ingredient_id
		LEFT JOIN users u ON u.id = cl.counted_by WHERE cl.count_id = $1 ORDER BY i.name, l.code`, id)
	if err != nil {
		return nil, err
	}
	if c.Lines, err = pgx.CollectRows(rows, pgx.RowToStructByPos[CountLine]); err != nil {
		return nil, err
	}
	rows, err = q.Query(ctx, `SELECT l.id, l.code, l.status FROM inventory_lots l WHERE l.location_id = $1 AND l.on_hand > 0
		AND NOT EXISTS (SELECT 1 FROM stock_count_lines cl WHERE cl.count_id = $2 AND cl.lot_id = l.id) ORDER BY l.code`, c.LocationID, id)
	if err != nil {
		return nil, err
	}
	c.ToCount, err = pgx.CollectRows(rows, pgx.RowToStructByPos[LotRef])
	return &c, err
}

// StartCount opens a count for a location (one open count per location).
func (s *Service) StartCount(ctx context.Context, actor, locationID, notes string) (*Count, error) {
	notes = strings.TrimSpace(notes)
	if runes(notes) > 1000 {
		return nil, fieldErr("notes", "At most 1000 characters.")
	}
	var id string
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if _, _, err := s.location(ctx, tx, locationID); err != nil {
			return err
		}
		n, err := suppliers.NextNumber(ctx, tx, "CNT", "")
		if err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `INSERT INTO stock_counts (number, location_id, notes, started_by) VALUES ($1, $2, $3, $4) RETURNING id`,
			fmt.Sprintf("CNT-%04d", n), locationID, notes, nilIfEmpty(actor)).Scan(&id)
		if db.IsUniqueViolation(err, "") {
			return conflict("count_open", "This location already has a count in progress.")
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "inventory.count_started", TargetType: "stock_count", TargetID: id})
	})
	if err != nil {
		return nil, err
	}
	return s.Count(ctx, id)
}

// CountLineInput records what was physically counted in one lot.
type CountLineInput struct {
	LotID   string `json:"lot_id"`
	Counted string `json:"counted"`
}

// RecordCounts records counted quantities. Each line keeps the ledger balance
// and the last movement at the moment it was counted, so stock moved during
// the count is not overwritten when the count is posted.
func (s *Service) RecordCounts(ctx context.Context, actor, id string, lines []CountLineInput) (*Count, error) {
	if len(lines) == 0 {
		return nil, fieldErr("lines", "Record at least one counted lot.")
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var status, location string
		if err := tx.QueryRow(ctx, `SELECT status, location_id FROM stock_counts WHERE id = $1 FOR UPDATE`, id).Scan(&status, &location); err != nil || !uuidPattern.MatchString(id) {
			return httpx.ErrNotFound
		}
		if status != "open" {
			return conflict("invalid_transition", "This count is "+status+".")
		}
		for i, l := range lines {
			field := fmt.Sprintf("lines[%d]", i)
			counted, err := qty.Parse(l.Counted)
			if err != nil || counted.Sign() < 0 {
				return fieldErr(field+".counted", "Enter the quantity counted (0 if none).")
			}
			lk, err := lockLot(ctx, tx, l.LotID)
			if err != nil {
				return fieldErr(field+".lot_id", "Lot not found.")
			}
			if lk.LocationID != location {
				return fieldErr(field+".lot_id", "Lot "+lk.Code+" is not in this location.")
			}
			var last int64
			if err := tx.QueryRow(ctx, `SELECT COALESCE(max(id), 0) FROM inventory_movements WHERE lot_id = $1`, lk.ID).Scan(&last); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO stock_count_lines (count_id, lot_id, expected, last_movement_id, counted, counted_by)
				VALUES ($1, $2, $3::numeric, $4, $5::numeric, $6)
				ON CONFLICT (count_id, lot_id) DO UPDATE SET expected = $3::numeric, last_movement_id = $4, counted = $5::numeric, counted_by = $6, counted_at = now()`,
				id, lk.ID, lk.OnHand.String(), last, counted.String(), nilIfEmpty(actor)); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "inventory.count_recorded", TargetType: "stock_count", TargetID: id,
			Metadata: map[string]any{"lines": len(lines)}})
	})
	if err != nil {
		return nil, err
	}
	return s.Count(ctx, id)
}

// SubmitCount hands a finished count for approval.
func (s *Service) SubmitCount(ctx context.Context, actor, id string) (*Count, error) {
	err := s.tx(ctx, func(tx pgx.Tx) error {
		c, err := s.count(ctx, tx, id)
		if err != nil {
			return err
		}
		if c.Status != "open" {
			return conflict("invalid_transition", "This count is "+c.Status+".")
		}
		if len(c.Lines) == 0 {
			return unprocessable("incomplete", "Count at least one lot.")
		}
		if len(c.ToCount) > 0 {
			return unprocessable("incomplete", fmt.Sprintf("%d lot(s) in this location are not counted yet.", len(c.ToCount)))
		}
		if _, err := tx.Exec(ctx, `UPDATE stock_counts SET status = 'submitted', submitted_by = $2, submitted_at = now() WHERE id = $1`, id, nilIfEmpty(actor)); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "inventory.count_submitted", TargetType: "stock_count", TargetID: id})
	})
	if err != nil {
		return nil, err
	}
	return s.Count(ctx, id)
}

// PostCount approves a submitted count and posts each variance as an
// adjustment: the lot moves by (counted − expected at count time), on top of
// whatever moved since. Someone other than the person who submitted it posts it.
func (s *Service) PostCount(ctx context.Context, actor, id string) (*Count, error) {
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM stock_counts WHERE id = $1 FOR UPDATE`, id); err != nil {
			return err
		}
		c, err := s.count(ctx, tx, id)
		if err != nil {
			return err
		}
		if c.Status != "submitted" {
			return conflict("invalid_transition", "Only a submitted count can be posted.")
		}
		var submitter *string
		if err := tx.QueryRow(ctx, `SELECT submitted_by::text FROM stock_counts WHERE id = $1`, id).Scan(&submitter); err != nil {
			return err
		}
		if submitter != nil && *submitter == actor {
			return forbidden("own_count", "You submitted this count, so someone else must approve and post it.")
		}
		for _, l := range c.Lines {
			variance := qty.MustParse(l.Variance)
			if variance.IsZero() {
				continue
			}
			lk, err := lockLot(ctx, tx, l.LotID)
			if err != nil {
				return err
			}
			target := lk.OnHand.Add(variance)
			reason := fmt.Sprintf("Count %s: counted %s, expected %s", c.Number, l.Counted, l.Expected)
			if l.MovedSince {
				reason += " (stock moved during the count; the variance is applied to the current balance)"
			}
			if target.Sign() < 0 {
				return unprocessable("negative_stock", "Lot "+l.LotCode+": posting the variance would take it below zero; recount it.")
			}
			if err := s.adjust(ctx, tx, actor, l.LotID, "count_discrepancy", target, reason, "", l.ID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE stock_counts SET status = 'posted', posted_by = $2, posted_at = now() WHERE id = $1`, id, nilIfEmpty(actor)); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "inventory.count_posted", TargetType: "stock_count", TargetID: id})
	})
	if err != nil {
		return nil, err
	}
	return s.Count(ctx, id)
}

// CancelCount abandons an open or submitted count; nothing is posted.
func (s *Service) CancelCount(ctx context.Context, actor, id string) (*Count, error) {
	err := s.tx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE stock_counts SET status = 'cancelled' WHERE id = $1 AND status IN ('open', 'submitted')`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return conflict("invalid_transition", "Only an open or submitted count can be cancelled.")
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "inventory.count_cancelled", TargetType: "stock_count", TargetID: id})
	})
	if err != nil {
		return nil, err
	}
	return s.Count(ctx, id)
}

// ---------------------------------------------------------------- traceability

// Trace follows lots from supplier to production batch and the orders
// behind each batch. It is batch-level: packing does not record which kit
// received which lot, so a customer order is linked to a lot only through
// the batch that made its meal.
type Trace struct {
	Scope  string     `json:"scope"`
	Lots   []TraceLot `json:"lots"`
	Notice string     `json:"notice"`
}

// TraceLot is one lot with where it came from and where it went.
type TraceLot struct {
	Lot     Lot          `json:"lot"`
	Batches []TraceBatch `json:"batches"`
}

// TraceBatch is a batch a lot went into.
type TraceBatch struct {
	Use          BatchUse             `json:"use"`
	Batch        *production.BatchRef `json:"batch"`
	OrderNumbers []string             `json:"order_numbers"`
}

const traceNotice = "Traceability is batch-level: a lot is linked to the batches it was issued to, and each batch to the orders in its plan that contained its meal. Packing does not record which kit received which lot."

// TraceFilter names what to trace from: one of them.
type TraceFilter struct {
	LotID, BatchID, SupplierID, POID string
}

func (s *Service) Trace(ctx context.Context, f TraceFilter) (*Trace, error) {
	var where, arg, scope string
	switch {
	case f.LotID != "":
		where, arg, scope = `l.id::text = $1`, f.LotID, "lot"
	case f.BatchID != "":
		where, arg, scope = `l.id IN (SELECT lot_id FROM batch_ingredient_lots WHERE batch_id::text = $1)`, f.BatchID, "batch"
	case f.SupplierID != "":
		where, arg, scope = `l.supplier_id::text = $1`, f.SupplierID, "supplier"
	case f.POID != "":
		where, arg, scope = `po.id::text = $1`, f.POID, "purchase_order"
	default:
		return nil, fieldErr("lot", "Trace from a lot, a batch, a supplier or a purchase order.")
	}
	if !uuidPattern.MatchString(arg) {
		return nil, httpx.ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `SELECT `+lotCols+lotFrom+` WHERE `+where+` ORDER BY l.received_at LIMIT 300`, arg)
	if err != nil {
		return nil, err
	}
	lots, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Lot, error) { return s.scanLot(r) })
	if err != nil {
		return nil, err
	}
	t := &Trace{Scope: scope, Lots: []TraceLot{}, Notice: traceNotice}
	orders := map[string][]string{}
	refs := map[string]*production.BatchRef{}
	for _, l := range lots {
		uses, err := s.batchUses(ctx, s.pool, "", l.ID)
		if err != nil {
			return nil, err
		}
		tl := TraceLot{Lot: l, Batches: []TraceBatch{}}
		for _, u := range uses {
			if f.BatchID != "" && u.BatchID != f.BatchID {
				continue
			}
			if _, ok := refs[u.BatchID]; !ok {
				if refs[u.BatchID], err = s.Production.BatchRef(ctx, s.pool, u.BatchID, false); err != nil {
					return nil, err
				}
				if orders[u.BatchID], err = s.Production.BatchOrders(ctx, s.pool, u.BatchID); err != nil {
					return nil, err
				}
			}
			tl.Batches = append(tl.Batches, TraceBatch{Use: u, Batch: refs[u.BatchID], OrderNumbers: orders[u.BatchID]})
		}
		t.Lots = append(t.Lots, tl)
	}
	return t, nil
}
