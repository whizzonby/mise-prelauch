package inventory

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/culinary"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/qty"
	"mise.tt/api/internal/procurement"
	"mise.tt/api/internal/suppliers"
	"mise.tt/api/internal/support"
)

// Receipt is one delivery recorded against a purchase order.
type Receipt struct {
	ID             string        `json:"id"`
	Number         string        `json:"number"`
	POID           string        `json:"po_id"`
	PONumber       string        `json:"po_number"`
	SupplierID     string        `json:"supplier_id"`
	SupplierName   string        `json:"supplier_name"`
	DeliveryRef    string        `json:"delivery_ref"`
	Notes          string        `json:"notes"`
	ReceivedByName string        `json:"received_by_name"`
	ReceivedAt     time.Time     `json:"received_at"`
	Lines          []ReceiptLine `json:"lines"`
	// True when this submission was already recorded (same idempotency key).
	Replayed bool `json:"replayed"`
}

// ReceiptLine is what came for one PO line and what happened to it.
type ReceiptLine struct {
	ID               string             `json:"id"`
	POLineID         string             `json:"po_line_id"`
	Description      string             `json:"description"`
	IngredientID     *string            `json:"ingredient_id"`
	DeliveredPacks   string             `json:"delivered_packs"`
	AcceptedPacks    string             `json:"accepted_packs"`
	QuarantinedPacks string             `json:"quarantined_packs"`
	RejectedPacks    string             `json:"rejected_packs"`
	SupplierLotRef   string             `json:"supplier_lot_ref"`
	ExpiryDate       *string            `json:"expiry_date"`
	LocationName     *string            `json:"location_name"`
	Inspection       []InspectionResult `json:"inspection"`
	TemperatureC     *string            `json:"temperature_c"`
	Discrepancy      string             `json:"discrepancy"`
	DiscrepancyNote  string             `json:"discrepancy_note"`
	Lots             []LotRef           `json:"lots"`
}

// InspectionResult is the finding on one checklist item.
type InspectionResult struct {
	ItemID string `json:"item_id"`
	Name   string `json:"name"`
	Result string `json:"result"`
	Note   string `json:"note"`
}

// LotRef names a lot.
type LotRef struct {
	ID     string `json:"id"`
	Code   string `json:"code"`
	Status string `json:"status"`
}

// ReceiptInput records a delivery. The idempotency key comes from the form:
// sending the same form twice records one receipt.
type ReceiptInput struct {
	POID           string             `json:"po_id"`
	IdempotencyKey string             `json:"idempotency_key"`
	DeliveryRef    string             `json:"delivery_ref"`
	Notes          string             `json:"notes"`
	Lines          []ReceiptLineInput `json:"lines"`
}

// ReceiptLineInput is one delivered PO line, in packs:
// delivered = accepted + quarantined + rejected.
type ReceiptLineInput struct {
	POLineID             string             `json:"po_line_id"`
	DeliveredPacks       string             `json:"delivered_packs"`
	AcceptedPacks        string             `json:"accepted_packs"`
	QuarantinedPacks     string             `json:"quarantined_packs"`
	RejectedPacks        string             `json:"rejected_packs"`
	SupplierLotRef       string             `json:"supplier_lot_ref"`
	ExpiryDate           string             `json:"expiry_date"`
	LocationID           string             `json:"location_id"`
	QuarantineLocationID string             `json:"quarantine_location_id"`
	Inspection           []InspectionResult `json:"inspection"`
	TemperatureC         *string            `json:"temperature_c"`
	Discrepancy          string             `json:"discrepancy"`
	DiscrepancyNote      string             `json:"discrepancy_note"`
}

var discrepancies = []string{"none", "short", "over", "damaged", "wrong_item", "specification", "expiry", "temperature", "documentation", "other"}

func zeroIfEmpty(v string) string {
	if strings.TrimSpace(v) == "" {
		return "0"
	}
	return v
}

// Receive records a delivery: what came, the inspection, and what was
// accepted (an available lot), quarantined (a quarantined lot, never
// allocated) or rejected (a rejected lot, held for return, never usable).
// Accepting needs the receiving checklist from the approved procedure, every
// item recorded and none failed. The order's received quantities and status
// are updated by procurement in the same transaction, which refuses
// over-receiving beyond the configured tolerance.
func (s *Service) Receive(ctx context.Context, actor string, in ReceiptInput) (*Receipt, bool, error) {
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	if n := len(in.IdempotencyKey); n < 8 || n > 100 {
		return nil, false, fieldErr("idempotency_key", "The form is missing its submission key; reload the page.")
	}
	if !uuidPattern.MatchString(in.POID) {
		return nil, false, fieldErr("po_id", "Choose the purchase order.")
	}
	if len(in.Lines) == 0 {
		return nil, false, fieldErr("lines", "Record at least one delivered line.")
	}
	in.DeliveryRef, in.Notes = clean(in.DeliveryRef), strings.TrimSpace(in.Notes)
	if runes(in.DeliveryRef) > 120 || runes(in.Notes) > 2000 {
		return nil, false, fieldErr("notes", "Too long.")
	}
	var id string
	replayed := false
	err := s.tx(ctx, func(tx pgx.Tx) error {
		// Two clerks recording the same delivery: the order lock serialises
		// them, and the key catches the same form sent twice.
		po, err := s.Procurement.LockForReceipt(ctx, tx, in.POID)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT id FROM goods_receipts WHERE idempotency_key = $1`, in.IdempotencyKey).Scan(&id); err == nil {
			replayed = true
			return nil
		}
		n, err := suppliers.NextNumber(ctx, tx, "GR", s.today().Format("0601"))
		if err != nil {
			return err
		}
		number := fmt.Sprintf("GR-%s-%04d", s.today().Format("0601"), n)
		if err := tx.QueryRow(ctx, `INSERT INTO goods_receipts (number, po_id, supplier_id, idempotency_key, delivery_ref, notes, received_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`, number, po.ID, po.SupplierID, in.IdempotencyKey, in.DeliveryRef, in.Notes, nilIfEmpty(actor)).Scan(&id); err != nil {
			return err
		}
		byLine := map[string]procurement.POLine{}
		for _, l := range po.Lines {
			byLine[l.ID] = l
		}
		conv, err := culinary.NewConverter(ctx, tx)
		if err != nil {
			return err
		}
		var received []procurement.ReceivedLine
		var failedChecks, discrepancyNotes []string
		seen := map[string]bool{}
		for i, l := range in.Lines {
			field := fmt.Sprintf("lines[%d]", i)
			pl, ok := byLine[l.POLineID]
			if !ok {
				return fieldErr(field+".po_line_id", "That line is not on order "+po.Number+".")
			}
			if seen[pl.ID] {
				return fieldErr(field+".po_line_id", "Each order line once per receipt.")
			}
			seen[pl.ID] = true
			delivered, err := parseQty(l.DeliveredPacks, field+".delivered_packs")
			if err != nil {
				return err
			}
			acc, err1 := qty.Parse(zeroIfEmpty(l.AcceptedPacks))
			quar, err2 := qty.Parse(zeroIfEmpty(l.QuarantinedPacks))
			rej, err3 := qty.Parse(zeroIfEmpty(l.RejectedPacks))
			if err1 != nil || err2 != nil || err3 != nil || acc.Sign() < 0 || quar.Sign() < 0 || rej.Sign() < 0 {
				return fieldErr(field+".accepted_packs", "Accepted, quarantined and rejected are numbers of packs.")
			}
			if acc.Add(quar).Add(rej).Cmp(delivered) != 0 {
				return fieldErr(field+".accepted_packs", "Accepted, quarantined and rejected must add up to what was delivered.")
			}
			if l.Discrepancy == "" {
				l.Discrepancy = "none"
			}
			l.DiscrepancyNote = strings.TrimSpace(l.DiscrepancyNote)
			if !slices.Contains(discrepancies, l.Discrepancy) {
				return fieldErr(field+".discrepancy", "Choose the discrepancy.")
			}
			if (quar.Sign() > 0 || rej.Sign() > 0) && l.Discrepancy == "none" {
				return fieldErr(field+".discrepancy", "Say what was wrong with what was quarantined or rejected.")
			}
			if l.Discrepancy != "none" && runes(l.DiscrepancyNote) < 3 {
				return fieldErr(field+".discrepancy_note", "Describe the discrepancy.")
			}
			if runes(l.DiscrepancyNote) > 1000 || runes(l.SupplierLotRef) > 120 {
				return fieldErr(field+".discrepancy_note", "Too long.")
			}
			// The inspection, against the checklist that applies.
			var category string
			if pl.IngredientID != nil {
				if err := tx.QueryRow(ctx, `SELECT category FROM ingredients WHERE id = $1`, *pl.IngredientID).Scan(&category); err != nil {
					return err
				}
			}
			items, err := s.checklist(ctx, tx, category, true)
			if err != nil {
				return err
			}
			results, failed, missing := []InspectionResult{}, false, []string{}
			given := map[string]InspectionResult{}
			for _, r := range l.Inspection {
				given[r.ItemID] = r
			}
			for _, it := range items {
				r, ok := given[it.ID]
				if !ok || !slices.Contains([]string{"pass", "fail", "not_applicable"}, r.Result) {
					missing = append(missing, it.Name)
					continue
				}
				r.Name, r.Note = it.Name, strings.TrimSpace(r.Note)
				if r.Result == "fail" {
					failed = true
					if r.Note == "" {
						return fieldErr(field+".inspection", "Say what failed: "+it.Name+".")
					}
				}
				results = append(results, r)
			}
			if acc.Sign() > 0 {
				switch {
				case len(items) == 0:
					return unprocessable("no_checklist", "No receiving inspection checklist is configured from the approved procedure, so stock cannot be accepted. Quarantine or reject it, and ask an inventory manager to add the checklist.")
				case len(missing) > 0:
					return unprocessable("inspection_incomplete", pl.Description+": record a result for "+strings.Join(missing, ", ")+".")
				case failed:
					return unprocessable("inspection_failed", pl.Description+" failed inspection: quarantine or reject it, it cannot be accepted.")
				}
			}
			if failed || rej.Sign() > 0 {
				failedChecks = append(failedChecks, pl.Description)
			}
			if l.Discrepancy != "none" {
				discrepancyNotes = append(discrepancyNotes, pl.Description+": "+human(l.Discrepancy)+" ("+l.DiscrepancyNote+")")
			}
			var expiry *time.Time
			if strings.TrimSpace(l.ExpiryDate) != "" {
				d, err := time.Parse(time.DateOnly, l.ExpiryDate)
				if err != nil {
					return fieldErr(field+".expiry_date", "Enter the expiry or use-by date printed on the product, or leave it blank.")
				}
				expiry = &d
			}
			var temp any
			if l.TemperatureC != nil && strings.TrimSpace(*l.TemperatureC) != "" {
				t, err := qty.Parse(*l.TemperatureC)
				if err != nil || t.Cmp(qty.Int(-100)) < 0 || t.Cmp(qty.Int(100)) > 0 {
					return fieldErr(field+".temperature_c", "Enter the temperature in °C.")
				}
				temp = t.Format(1)
			}
			// Where it goes: accepted stock to a usable location, quarantined
			// and rejected stock to a quarantine location.
			var accLoc, quarLoc string
			if pl.IngredientID != nil && acc.Sign() > 0 {
				typ, _, err := s.location(ctx, tx, l.LocationID)
				if err != nil {
					return err
				}
				if typ == "quarantine" {
					return fieldErr(field+".location_id", "Accepted stock goes to a storage location, not quarantine.")
				}
				accLoc = l.LocationID
			}
			if pl.IngredientID != nil && (quar.Sign() > 0 || rej.Sign() > 0) {
				if l.QuarantineLocationID == "" {
					l.QuarantineLocationID = l.LocationID
				}
				typ, _, err := s.location(ctx, tx, l.QuarantineLocationID)
				if err != nil {
					return fieldErr(field+".quarantine_location_id", "Choose a quarantine location for stock that is not accepted.")
				}
				if typ != "quarantine" {
					return fieldErr(field+".quarantine_location_id", "Quarantined and rejected stock goes to a quarantine location.")
				}
				quarLoc = l.QuarantineLocationID
			}
			inspection, _ := json.Marshal(results)
			var lineID string
			price := int64(0)
			if pl.UnitPriceMinor != nil {
				price = *pl.UnitPriceMinor
			}
			var mainLoc any
			if accLoc != "" {
				mainLoc = accLoc
			} else if quarLoc != "" {
				mainLoc = quarLoc
			}
			if err := tx.QueryRow(ctx, `INSERT INTO goods_receipt_lines (receipt_id, po_line_id, ingredient_id, delivered_packs, accepted_packs, quarantined_packs,
				rejected_packs, supplier_lot_ref, expiry_date, location_id, inspection, temperature_c, discrepancy, discrepancy_note, unit_cost_minor, currency)
				VALUES ($1, $2, $3, $4::numeric, $5::numeric, $6::numeric, $7::numeric, $8, $9, $10, $11, $12::numeric, $13, $14, $15, $16) RETURNING id`,
				id, pl.ID, pl.IngredientID, delivered.String(), acc.String(), quar.String(), rej.String(), clean(l.SupplierLotRef), expiry, mainLoc,
				inspection, temp, l.Discrepancy, l.DiscrepancyNote, price, po.Currency).Scan(&lineID); err != nil {
				return err
			}
			received = append(received, procurement.ReceivedLine{POLineID: pl.ID, Delivered: delivered, Accepted: acc})
			if pl.IngredientID == nil {
				continue // packaging: received against the order, not ingredient stock
			}
			packBase, _, err := conv.Convert(ctx, *pl.IngredientID, qty.MustParse(pl.PackQuantity), pl.PackUnit, *pl.BaseUnit)
			if err != nil {
				return unprocessable("incompatible_mapping", pl.Description+" cannot be converted to its ingredient's base unit.")
			}
			for _, part := range []struct {
				packs  qty.Q
				status string
				loc    string
			}{{acc, "available", accLoc}, {quar, "quarantined", quarLoc}, {rej, "rejected", quarLoc}} {
				if part.packs.Sign() <= 0 {
					continue
				}
				code, err := s.nextLotCode(ctx, tx)
				if err != nil {
					return err
				}
				amount := part.packs.Mul(packBase).Round(3)
				expiryStatus := "not_recorded"
				if expiry != nil {
					expiryStatus = "recorded"
				}
				var lotID string
				if err := tx.QueryRow(ctx, `INSERT INTO inventory_lots (code, ingredient_id, supplier_id, po_line_id, receipt_line_id, supplier_lot_ref, received_at,
					unit, received_quantity, on_hand, status, expiry_date, expiry_status, location_id, unit_cost_minor, currency)
					VALUES ($1, $2, $3, $4, $5, $6, now(), $7, $8::numeric, 0, $9, $10, $11, $12, $13, $14) RETURNING id`,
					code, *pl.IngredientID, po.SupplierID, pl.ID, lineID, clean(l.SupplierLotRef), *pl.BaseUnit, amount.String(), part.status,
					expiry, expiryStatus, part.loc, price, po.Currency).Scan(&lotID); err != nil {
					return err
				}
				lk, err := lockLot(ctx, tx, lotID)
				if err != nil {
					return err
				}
				packs := part.packs.String() + " packs"
				if part.packs.Cmp(qty.Int(1)) == 0 {
					packs = "1 pack"
				}
				if err := apply(ctx, tx, lk, movement{Type: "receipt", OnHand: amount, Reason: fmt.Sprintf("Received on %s against %s (%s, %s)", number, po.Number,
					packs, part.status), RefType: "goods_receipt_line", RefID: lineID, Actor: actor}); err != nil {
					return err
				}
				typ := map[string]string{"available": "inventory.lot_received", "quarantined": "inventory.lot_quarantined", "rejected": "inventory.lot_quarantined"}[part.status]
				if err := event(ctx, tx, typ, "inventory_lot", lotID, "", map[string]any{"code": code, "ingredient_id": *pl.IngredientID, "quantity": amount.String(),
					"unit": *pl.BaseUnit, "status": part.status, "po_id": po.ID, "receipt_id": id}); err != nil {
					return err
				}
			}
			// The actual cost of what was accepted, kept apart from quoted and ordered costs.
			if acc.Sign() > 0 && pl.UnitPriceMinor != nil {
				if err := culinary.RecordSupplyCost(ctx, tx, actor, culinary.SupplyCost{IngredientID: *pl.IngredientID, AmountMinor: *pl.UnitPriceMinor,
					Currency: po.Currency, PerQuantity: qty.MustParse(pl.PackQuantity), Unit: pl.PackUnit, Origin: "receipt", OriginID: lineID,
					Source: "Received on " + number + " (" + po.Number + ", " + po.SupplierName + ")", EffectiveFrom: s.today()}); err != nil {
					return err
				}
			}
		}
		if err := s.Procurement.RecordReceipt(ctx, tx, actor, po.ID, id, received); err != nil {
			return err
		}
		if len(failedChecks) > 0 {
			if err := event(ctx, tx, "receiving.inspection_failed", "goods_receipt", id, "", map[string]any{"po_id": po.ID, "lines": failedChecks}); err != nil {
				return err
			}
		}
		if len(discrepancyNotes) > 0 {
			if err := s.raise(ctx, tx, support.Alert{DedupeKey: "receiving_discrepancy:" + id, Kind: "receiving_discrepancy", Severity: "warning",
				Title: "Delivery discrepancy on " + po.Number, Body: strings.Join(discrepancyNotes, " "), ResourceType: "goods_receipt", ResourceID: id,
				Permission: "receiving.inspect"}); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "receiving.recorded", TargetType: "goods_receipt", TargetID: id,
			Metadata: map[string]any{"po_id": po.ID, "lines": len(in.Lines)}})
	})
	if err != nil {
		return nil, false, err
	}
	r, err := s.Receipt(ctx, id)
	if r != nil {
		r.Replayed = replayed
	}
	return r, replayed, err
}

const receiptCols = `g.id, g.number, g.po_id, po.number, g.supplier_id, s.name, g.delivery_ref, g.notes,
	COALESCE(btrim(u.first_name || ' ' || u.last_name), ''), g.received_at`

const receiptFrom = ` FROM goods_receipts g JOIN purchase_orders po ON po.id = g.po_id JOIN suppliers s ON s.id = g.supplier_id
	LEFT JOIN users u ON u.id = g.received_by`

func scanReceipt(r pgx.Row) (Receipt, error) {
	var x Receipt
	err := r.Scan(&x.ID, &x.Number, &x.POID, &x.PONumber, &x.SupplierID, &x.SupplierName, &x.DeliveryRef, &x.Notes, &x.ReceivedByName, &x.ReceivedAt)
	x.Lines = []ReceiptLine{}
	return x, err
}

// Receipts lists deliveries, newest first.
func (s *Service) Receipts(ctx context.Context, poID string) ([]Receipt, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+receiptCols+receiptFrom+` WHERE ($1 = '' OR g.po_id::text = $1) ORDER BY g.received_at DESC LIMIT 200`, poID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Receipt, error) { return scanReceipt(r) })
}

func (s *Service) Receipt(ctx context.Context, id string) (*Receipt, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	x, err := scanReceipt(s.pool.QueryRow(ctx, `SELECT `+receiptCols+receiptFrom+` WHERE g.id = $1`, id))
	if err != nil {
		return nil, notFoundIf(err)
	}
	rows, err := s.pool.Query(ctx, `SELECT gl.id, gl.po_line_id, pl.description, gl.ingredient_id, trim_scale(gl.delivered_packs)::text,
		trim_scale(gl.accepted_packs)::text, trim_scale(gl.quarantined_packs)::text, trim_scale(gl.rejected_packs)::text, gl.supplier_lot_ref,
		gl.expiry_date::text, loc.name, gl.inspection, trim_scale(gl.temperature_c)::text, gl.discrepancy, gl.discrepancy_note
		FROM goods_receipt_lines gl JOIN purchase_order_lines pl ON pl.id = gl.po_line_id LEFT JOIN storage_locations loc ON loc.id = gl.location_id
		WHERE gl.receipt_id = $1 ORDER BY pl.position`, id)
	if err != nil {
		return nil, err
	}
	x.Lines, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (ReceiptLine, error) {
		var l ReceiptLine
		var insp []byte
		err := r.Scan(&l.ID, &l.POLineID, &l.Description, &l.IngredientID, &l.DeliveredPacks, &l.AcceptedPacks, &l.QuarantinedPacks, &l.RejectedPacks,
			&l.SupplierLotRef, &l.ExpiryDate, &l.LocationName, &insp, &l.TemperatureC, &l.Discrepancy, &l.DiscrepancyNote)
		l.Inspection = []InspectionResult{}
		_ = json.Unmarshal(insp, &l.Inspection)
		return l, err
	})
	if err != nil {
		return nil, err
	}
	for i := range x.Lines {
		rows, err := s.pool.Query(ctx, `SELECT id, code, status FROM inventory_lots WHERE receipt_line_id = $1 ORDER BY code`, x.Lines[i].ID)
		if err != nil {
			return nil, err
		}
		if x.Lines[i].Lots, err = pgx.CollectRows(rows, pgx.RowToStructByPos[LotRef]); err != nil {
			return nil, err
		}
	}
	return &x, nil
}
