package inventory

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/qty"
	"mise.tt/api/internal/procurement"
	"mise.tt/api/internal/support"
)

// usableSQL is the condition for stock that may be allocated today ($1 = today).
const usableSQL = `l.status = 'available' AND (l.expiry_status = 'none_applies' OR (l.expiry_status = 'recorded' AND l.expiry_date >= $1::date))`

// Definitions are shown beside the figures.
var Definitions = map[string]string{
	"on_hand":       "Physically held in the store, whatever its status.",
	"usable":        "Available lots, not expired, with an expiry date recorded or confirmed not to apply.",
	"reserved":      "Held for a production plan.",
	"available":     "Usable minus reserved: what can still be promised.",
	"quarantined":   "Received but not accepted, or put on hold: never allocated.",
	"rejected":      "Refused on receipt and held for return to the supplier: never usable.",
	"expired":       "Past its recorded expiry date.",
	"expiry_review": "Available lots with no expiry date recorded: not allocated until a person records one or confirms none applies.",
	"incoming":      "Still to receive on approved, issued or partly received purchase orders. Not stock.",
}

// Summary is the stock of one ingredient, in its base unit.
type Summary struct {
	IngredientID   string `json:"ingredient_id"`
	IngredientName string `json:"ingredient_name"`
	Category       string `json:"category"`
	Unit           string `json:"unit"`
	Method         string `json:"method"`
	OnHand         string `json:"on_hand"`
	Usable         string `json:"usable"`
	Reserved       string `json:"reserved"`
	Available      string `json:"available"`
	Quarantined    string `json:"quarantined"`
	Rejected       string `json:"rejected"`
	Expired        string `json:"expired"`
	ExpiryReview   string `json:"expiry_review"`
	Incoming       string `json:"incoming"`
	Lots           int    `json:"lots"`
}

// Stock lists stock per ingredient: every ingredient with a lot or incoming stock.
func (s *Service) Stock(ctx context.Context, query string) (*StockView, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT i.id, i.name, i.category, i.base_unit, COALESCE(p.method, 'fifo'),
			trim_scale(COALESCE(sum(l.on_hand), 0))::text,
			trim_scale(COALESCE(sum(l.on_hand) FILTER (WHERE `+usableSQL+`), 0))::text,
			trim_scale(COALESCE(sum(l.reserved), 0))::text,
			trim_scale(COALESCE(sum(l.on_hand) FILTER (WHERE l.status IN ('quarantined', 'on_hold')), 0))::text,
			trim_scale(COALESCE(sum(l.on_hand) FILTER (WHERE l.status = 'rejected'), 0))::text,
			trim_scale(COALESCE(sum(l.on_hand) FILTER (WHERE l.expiry_status = 'recorded' AND l.expiry_date < $1::date), 0))::text,
			trim_scale(COALESCE(sum(l.on_hand) FILTER (WHERE l.status = 'available' AND l.expiry_status = 'not_recorded'), 0))::text,
			count(l.id) FILTER (WHERE l.on_hand > 0)::int
		FROM ingredients i LEFT JOIN inventory_policies p ON p.category = i.category
		LEFT JOIN inventory_lots l ON l.ingredient_id = i.id
		WHERE ($2 = '' OR i.name ILIKE '%' || $2 || '%')
		GROUP BY i.id, p.method ORDER BY i.name`, s.today(), strings.TrimSpace(query))
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Summary, error) {
		var x Summary
		err := r.Scan(&x.IngredientID, &x.IngredientName, &x.Category, &x.Unit, &x.Method, &x.OnHand, &x.Usable, &x.Reserved, &x.Quarantined,
			&x.Rejected, &x.Expired, &x.ExpiryReview, &x.Lots)
		return x, err
	})
	if err != nil {
		return nil, err
	}
	incoming := map[string]qty.Q{}
	if s.Procurement != nil {
		if incoming, err = s.Procurement.Incoming(ctx, s.pool); err != nil {
			return nil, err
		}
	}
	out := []Summary{}
	for _, x := range list {
		in := incoming[x.IngredientID]
		x.Incoming = in.String()
		// Reserved stock is always usable (only available lots hold reservations).
		x.Available = qty.MustParse(x.Usable).Sub(qty.MustParse(x.Reserved)).String()
		if x.Lots == 0 && in.Sign() == 0 {
			continue
		}
		out = append(out, x)
	}
	return &StockView{Items: out, Definitions: Definitions}, nil
}

// StockView is the stock list with the definition of every figure.
type StockView struct {
	Items       []Summary         `json:"items"`
	Definitions map[string]string `json:"definitions"`
}

// PlanPosition implements procurement.Stock: per ingredient, what is
// reserved for the plan and what usable stock nobody has reserved.
func (s *Service) PlanPosition(ctx context.Context, q db.Querier, planID string, ingredientIDs []string) (map[string]procurement.StockPosition, error) {
	out := map[string]procurement.StockPosition{}
	rows, err := q.Query(ctx, `SELECT ingredient_id, trim_scale(sum(open_quantity))::text FROM inventory_reservations
		WHERE plan_id = $1 AND status = 'active' AND ingredient_id = ANY ($2) GROUP BY ingredient_id`, planID, ingredientIDs)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var ing, v string
		if err := rows.Scan(&ing, &v); err != nil {
			rows.Close()
			return nil, err
		}
		p := out[ing]
		p.Reserved = qty.MustParse(v)
		out[ing] = p
	}
	rows.Close()
	// What the plan's batches were already given, net of returns.
	rows, err = q.Query(ctx, `SELECT bl.ingredient_id, trim_scale(sum(bl.issued - bl.returned))::text FROM batch_ingredient_lots bl
		JOIN production_batches b ON b.id = bl.batch_id WHERE b.plan_id = $1 AND bl.ingredient_id = ANY ($2) GROUP BY bl.ingredient_id`, planID, ingredientIDs)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var ing, v string
		if err := rows.Scan(&ing, &v); err != nil {
			rows.Close()
			return nil, err
		}
		p := out[ing]
		p.Issued = qty.MustParse(v)
		out[ing] = p
	}
	rows.Close()
	rows, err = q.Query(ctx, `SELECT l.ingredient_id, trim_scale(sum(l.on_hand - l.reserved))::text FROM inventory_lots l
		WHERE `+usableSQL+` AND l.ingredient_id = ANY ($2) GROUP BY l.ingredient_id`, s.today(), ingredientIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ing, v string
		if err := rows.Scan(&ing, &v); err != nil {
			return nil, err
		}
		p := out[ing]
		p.Available = qty.MustParse(v)
		out[ing] = p
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- lots

// Lot is one lot of an ingredient with its ledger.
type Lot struct {
	ID               string    `json:"id"`
	Code             string    `json:"code"`
	IngredientID     string    `json:"ingredient_id"`
	IngredientName   string    `json:"ingredient_name"`
	SupplierID       string    `json:"supplier_id"`
	SupplierName     string    `json:"supplier_name"`
	POID             string    `json:"po_id"`
	PONumber         string    `json:"po_number"`
	ReceiptID        string    `json:"receipt_id"`
	ReceiptNumber    string    `json:"receipt_number"`
	SupplierLotRef   string    `json:"supplier_lot_ref"`
	ReceivedAt       time.Time `json:"received_at"`
	Unit             string    `json:"unit"`
	ReceivedQuantity string    `json:"received_quantity"`
	OnHand           string    `json:"on_hand"`
	Reserved         string    `json:"reserved"`
	Status           string    `json:"status"`
	ExpiryDate       *string   `json:"expiry_date"`
	ExpiryStatus     string    `json:"expiry_status"`
	ExpiryNote       string    `json:"expiry_note"`
	LocationID       string    `json:"location_id"`
	LocationName     string    `json:"location_name"`
	UnitCostMinor    *int64    `json:"unit_cost_minor"`
	Currency         *string   `json:"currency"`
	Expired          bool      `json:"expired"`
	Usable           bool      `json:"usable"`
	// Why it cannot be allocated, when it cannot.
	Blocked string `json:"blocked"`

	Movements    []Movement    `json:"movements,omitempty"`
	Reservations []Reservation `json:"reservations,omitempty"`
	Batches      []BatchUse    `json:"batches,omitempty"`
}

const lotCols = `l.id, l.code, l.ingredient_id, i.name, l.supplier_id, s.name, po.id, po.number, g.id, g.number, l.supplier_lot_ref, l.received_at, l.unit,
	trim_scale(l.received_quantity)::text, trim_scale(l.on_hand)::text, trim_scale(l.reserved)::text, l.status, l.expiry_date::text, l.expiry_status,
	l.expiry_note, l.location_id, loc.name, l.unit_cost_minor, l.currency`

const lotFrom = ` FROM inventory_lots l JOIN ingredients i ON i.id = l.ingredient_id JOIN suppliers s ON s.id = l.supplier_id
	JOIN purchase_order_lines pl ON pl.id = l.po_line_id JOIN purchase_orders po ON po.id = pl.po_id
	JOIN goods_receipt_lines gl ON gl.id = l.receipt_line_id JOIN goods_receipts g ON g.id = gl.receipt_id
	JOIN storage_locations loc ON loc.id = l.location_id`

func (s *Service) scanLot(r pgx.Row) (Lot, error) {
	var x Lot
	err := r.Scan(&x.ID, &x.Code, &x.IngredientID, &x.IngredientName, &x.SupplierID, &x.SupplierName, &x.POID, &x.PONumber, &x.ReceiptID, &x.ReceiptNumber,
		&x.SupplierLotRef, &x.ReceivedAt, &x.Unit, &x.ReceivedQuantity, &x.OnHand, &x.Reserved, &x.Status, &x.ExpiryDate, &x.ExpiryStatus, &x.ExpiryNote,
		&x.LocationID, &x.LocationName, &x.UnitCostMinor, &x.Currency)
	if err != nil {
		return x, err
	}
	today := s.today().Format(time.DateOnly)
	x.Expired = x.ExpiryDate != nil && *x.ExpiryDate < today
	switch {
	case x.Status == "rejected":
		x.Blocked = "Rejected on receipt; held for return to the supplier."
	case x.Status == "quarantined":
		x.Blocked = "Quarantined: not accepted for use."
	case x.Status == "on_hold":
		x.Blocked = "On hold."
	case x.Expired:
		x.Blocked = "Expired on " + *x.ExpiryDate + "."
	case x.ExpiryStatus == "not_recorded":
		x.Blocked = "No expiry date recorded: record one, or confirm none applies, before it is used."
	default:
		x.Usable = true
	}
	return x, nil
}

// LotFilter narrows the lot list.
type LotFilter struct {
	IngredientID, Status, LocationID, SupplierID, Query string
	// Held: only lots with stock on hand.
	Held bool
}

func (s *Service) Lots(ctx context.Context, f LotFilter) ([]Lot, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+lotCols+lotFrom+`
		WHERE ($1 = '' OR l.ingredient_id::text = $1) AND ($2 = '' OR l.status = $2) AND ($3 = '' OR l.location_id::text = $3)
		AND ($4 = '' OR l.supplier_id::text = $4) AND (NOT $5 OR l.on_hand > 0)
		AND ($6 = '' OR l.code ILIKE '%' || $6 || '%' OR i.name ILIKE '%' || $6 || '%' OR l.supplier_lot_ref ILIKE '%' || $6 || '%')
		ORDER BY i.name, l.received_at LIMIT 500`, f.IngredientID, f.Status, f.LocationID, f.SupplierID, f.Held, strings.TrimSpace(f.Query))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Lot, error) { return s.scanLot(r) })
}

// Movement is one ledger row.
type Movement struct {
	ID             int64     `json:"id"`
	LotID          string    `json:"lot_id"`
	LotCode        string    `json:"lot_code"`
	IngredientID   string    `json:"ingredient_id"`
	IngredientName string    `json:"ingredient_name"`
	Type           string    `json:"type"`
	OnHandDelta    string    `json:"on_hand_delta"`
	ReservedDelta  string    `json:"reserved_delta"`
	OnHandAfter    string    `json:"on_hand_after"`
	ReservedAfter  string    `json:"reserved_after"`
	Unit           string    `json:"unit"`
	Reason         string    `json:"reason"`
	FromLocation   *string   `json:"from_location"`
	ToLocation     *string   `json:"to_location"`
	FromStatus     *string   `json:"from_status"`
	ToStatus       *string   `json:"to_status"`
	BatchID        *string   `json:"batch_id"`
	BatchCode      *string   `json:"batch_code"`
	PlanID         *string   `json:"plan_id"`
	ActorName      string    `json:"actor_name"`
	CreatedAt      time.Time `json:"created_at"`
}

// MovementFilter narrows the ledger.
type MovementFilter struct {
	LotID, IngredientID, Type, BatchID string
	From, To                           string
	Limit                              int
}

func (s *Service) Movements(ctx context.Context, f MovementFilter) ([]Movement, error) {
	return s.movements(ctx, s.pool, f)
}

func (s *Service) movements(ctx context.Context, q db.Querier, f MovementFilter) ([]Movement, error) {
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 300
	}
	rows, err := q.Query(ctx, `SELECT m.id, m.lot_id, l.code, m.ingredient_id, i.name, m.type, trim_scale(m.on_hand_delta)::text, trim_scale(m.reserved_delta)::text,
		trim_scale(m.on_hand_after)::text, trim_scale(m.reserved_after)::text, m.unit, m.reason, fl.name, tl.name, m.from_status, m.to_status,
		m.batch_id::text, b.code, m.plan_id::text, COALESCE(btrim(u.first_name || ' ' || u.last_name), ''), m.created_at
		FROM inventory_movements m JOIN inventory_lots l ON l.id = m.lot_id JOIN ingredients i ON i.id = m.ingredient_id
		LEFT JOIN storage_locations fl ON fl.id = m.from_location LEFT JOIN storage_locations tl ON tl.id = m.to_location
		LEFT JOIN production_batches b ON b.id = m.batch_id LEFT JOIN users u ON u.id = m.actor_id
		WHERE ($1 = '' OR m.lot_id::text = $1) AND ($2 = '' OR m.ingredient_id::text = $2) AND ($3 = '' OR m.type = $3)
		AND ($4 = '' OR m.batch_id::text = $4) AND ($5 = '' OR m.created_at >= $5::date) AND ($6 = '' OR m.created_at < $6::date + 1)
		ORDER BY m.id DESC LIMIT $7`, f.LotID, f.IngredientID, f.Type, f.BatchID, f.From, f.To, f.Limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Movement])
}

// Lot returns a lot with its ledger, reservations and the batches it went into.
func (s *Service) Lot(ctx context.Context, id string) (*Lot, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	x, err := s.scanLot(s.pool.QueryRow(ctx, `SELECT `+lotCols+lotFrom+` WHERE l.id = $1`, id))
	if err != nil {
		return nil, notFoundIf(err)
	}
	if x.Movements, err = s.movements(ctx, s.pool, MovementFilter{LotID: id, Limit: 1000}); err != nil {
		return nil, err
	}
	if x.Reservations, err = s.reservations(ctx, s.pool, ReservationFilter{LotID: id}); err != nil {
		return nil, err
	}
	x.Batches, err = s.batchUses(ctx, s.pool, "", id)
	return &x, err
}

// ---------------------------------------------------------------- lot actions

// StatusInput changes a lot's status, with a reason.
type StatusInput struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

var statusMoves = map[string][]string{
	"quarantined": {"available", "rejected"},
	"available":   {"on_hold"},
	"on_hold":     {"available", "rejected"},
}

// SetStatus releases a quarantined or held lot for use, holds an available
// one, or rejects it. Stock reserved for production must be released first.
func (s *Service) SetStatus(ctx context.Context, actor, id string, in StatusInput) (*Lot, error) {
	in.Reason = clean(in.Reason)
	if runes(in.Reason) < 3 || runes(in.Reason) > 500 {
		return nil, fieldErr("reason", "Give the reason.")
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		l, err := lockLot(ctx, tx, id)
		if err != nil {
			return err
		}
		if !slices.Contains(statusMoves[l.Status], in.Status) {
			return conflict("invalid_transition", "A "+human(l.Status)+" lot cannot become "+human(in.Status)+".")
		}
		if l.Reserved.Sign() > 0 {
			return conflict("reserved_stock", fmt.Sprintf("%s %s of this lot is reserved for production; release it first.", l.Reserved.String(), l.Unit))
		}
		if err := apply(ctx, tx, l, movement{Type: "status", ToStatus: in.Status, Reason: in.Reason, Actor: actor}); err != nil {
			return err
		}
		typ := map[string]string{"available": "inventory.lot_released", "on_hold": "inventory.lot_quarantined", "rejected": "inventory.lot_quarantined"}[in.Status]
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM inventory_movements WHERE lot_id = $1`, id).Scan(&n); err != nil {
			return err
		}
		if err := event(ctx, tx, typ, "inventory_lot", id, fmt.Sprint(n), map[string]any{"code": l.Code, "status": in.Status, "reason": in.Reason}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "inventory.lot_status", TargetType: "inventory_lot", TargetID: id,
			Metadata: map[string]any{"status": in.Status, "reason": in.Reason}})
	})
	if err != nil {
		return nil, err
	}
	return s.Lot(ctx, id)
}

// ExpiryInput records the date printed on the product, or that a person
// checked and no date applies. Never a guess.
type ExpiryInput struct {
	ExpiryDate  string `json:"expiry_date"`
	NoneApplies bool   `json:"none_applies"`
	Note        string `json:"note"`
}

func (s *Service) SetExpiry(ctx context.Context, actor, id string, in ExpiryInput) (*Lot, error) {
	in.Note = clean(in.Note)
	var date *time.Time
	status := "recorded"
	switch {
	case in.NoneApplies && in.ExpiryDate != "":
		return nil, fieldErr("expiry_date", "Either a date or none applies, not both.")
	case in.NoneApplies:
		if runes(in.Note) < 3 {
			return nil, fieldErr("note", "Say why no date applies (for example, the supplier's specification).")
		}
		status = "none_applies"
	default:
		d, err := time.Parse(time.DateOnly, in.ExpiryDate)
		if err != nil {
			return nil, fieldErr("expiry_date", "Enter the date printed on the product or the supplier's documents.")
		}
		date = &d
	}
	if runes(in.Note) > 300 {
		return nil, fieldErr("note", "At most 300 characters.")
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		l, err := lockLot(ctx, tx, id)
		if err != nil {
			return err
		}
		if l.ExpiryStatus != "not_recorded" {
			return conflict("expiry_recorded", "This lot's expiry has already been reviewed and is not changed. If it is wrong, hold the lot and record why.")
		}
		if _, err := tx.Exec(ctx, `UPDATE inventory_lots SET expiry_date = $2, expiry_status = $3, expiry_note = $4, updated_at = now() WHERE id = $1`,
			id, date, status, in.Note); err != nil {
			return err
		}
		reason := "Expiry date recorded: " + in.ExpiryDate
		if in.NoneApplies {
			reason = "No expiry date applies: " + in.Note
		}
		if err := apply(ctx, tx, l, movement{Type: "status", Reason: reason, Actor: actor}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "inventory.lot_expiry", TargetType: "inventory_lot", TargetID: id,
			Metadata: map[string]any{"expiry_status": status, "expiry_date": in.ExpiryDate}})
	})
	if err != nil {
		return nil, err
	}
	return s.Lot(ctx, id)
}

// TransferInput moves a whole lot to another location.
type TransferInput struct {
	LocationID string `json:"location_id"`
	Reason     string `json:"reason"`
}

func (s *Service) Transfer(ctx context.Context, actor, id string, in TransferInput) (*Lot, error) {
	in.Reason = clean(in.Reason)
	if runes(in.Reason) > 500 {
		return nil, fieldErr("reason", "At most 500 characters.")
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		l, err := lockLot(ctx, tx, id)
		if err != nil {
			return err
		}
		typ, _, err := s.location(ctx, tx, in.LocationID)
		if err != nil {
			return err
		}
		if in.LocationID == l.LocationID {
			return fieldErr("location_id", "The lot is already there.")
		}
		if typ == "quarantine" && l.Status == "available" {
			return fieldErr("location_id", "Put the lot on hold before moving it to quarantine.")
		}
		if typ != "quarantine" && (l.Status == "quarantined" || l.Status == "rejected") {
			return fieldErr("location_id", "Quarantined and rejected stock stays in quarantine.")
		}
		if err := apply(ctx, tx, l, movement{Type: "transfer", ToLocation: in.LocationID, Reason: in.Reason, Actor: actor}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "inventory.lot_transferred", TargetType: "inventory_lot", TargetID: id})
	})
	if err != nil {
		return nil, err
	}
	return s.Lot(ctx, id)
}

// QuantityInput removes stock from a lot: store waste, or a return of
// rejected goods to the supplier.
type QuantityInput struct {
	Quantity string `json:"quantity"`
	Reason   string `json:"reason"`
}

// StoreWaste records stock spoiled or damaged while still in storage. Waste
// during production is the kitchen's record (Sprint 5) and is not posted here.
func (s *Service) StoreWaste(ctx context.Context, actor, id string, in QuantityInput) (*Lot, error) {
	return s.remove(ctx, actor, id, in, "waste")
}

// SupplierReturn returns rejected or quarantined stock to the supplier.
func (s *Service) SupplierReturn(ctx context.Context, actor, id string, in QuantityInput) (*Lot, error) {
	return s.remove(ctx, actor, id, in, "supplier_return")
}

func (s *Service) remove(ctx context.Context, actor, id string, in QuantityInput, typ string) (*Lot, error) {
	in.Reason = clean(in.Reason)
	if runes(in.Reason) < 3 || runes(in.Reason) > 500 {
		return nil, fieldErr("reason", "Give the reason.")
	}
	q, err := parseQty(in.Quantity, "quantity")
	if err != nil {
		return nil, err
	}
	err = s.tx(ctx, func(tx pgx.Tx) error {
		l, err := lockLot(ctx, tx, id)
		if err != nil {
			return err
		}
		if typ == "supplier_return" && l.Status != "rejected" && l.Status != "quarantined" {
			return conflict("invalid_transition", "Only rejected or quarantined stock is returned to the supplier; accepted stock is adjusted or wasted.")
		}
		if l.OnHand.Sub(q).Cmp(l.Reserved) < 0 {
			return unprocessable("reserved_stock", fmt.Sprintf("Only %s %s is not reserved for production.", l.OnHand.Sub(l.Reserved).String(), l.Unit))
		}
		if err := apply(ctx, tx, l, movement{Type: typ, OnHand: qty.Q{}.Sub(q), Reason: in.Reason, Actor: actor}); err != nil {
			return err
		}
		if err := event(ctx, tx, "inventory.stock_adjusted", "inventory_lot", id, fmt.Sprint(s.Now().UnixNano()), map[string]any{
			"code": l.Code, "type": typ, "quantity": q.String(), "unit": l.Unit, "reason": in.Reason}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "inventory." + typ, TargetType: "inventory_lot", TargetID: id,
			Metadata: map[string]any{"quantity": q.String(), "reason": in.Reason}})
	})
	if err != nil {
		return nil, err
	}
	return s.Lot(ctx, id)
}

// AdjustmentInput sets a lot's on-hand quantity to what is really there.
type AdjustmentInput struct {
	LotID     string `json:"lot_id"`
	Category  string `json:"category"`
	NewOnHand string `json:"new_on_hand"`
	Reason    string `json:"reason"`
	Evidence  string `json:"evidence"`
}

var adjustCategories = []string{"count_discrepancy", "damage", "spoilage", "measurement_correction", "other"}

// Adjust records an adjustment with its reason, the previous and the new
// balance. History is never edited: the adjustment is a new ledger row.
func (s *Service) Adjust(ctx context.Context, actor string, in AdjustmentInput) (*Lot, error) {
	in.Reason, in.Evidence = strings.TrimSpace(in.Reason), clean(in.Evidence)
	if !slices.Contains(adjustCategories, in.Category) {
		return nil, fieldErr("category", "Choose why the stock is being adjusted.")
	}
	if runes(in.Reason) < 3 || runes(in.Reason) > 1000 {
		return nil, fieldErr("reason", "Explain the adjustment.")
	}
	if runes(in.Evidence) > 500 {
		return nil, fieldErr("evidence", "At most 500 characters.")
	}
	n, err := qty.Parse(in.NewOnHand)
	if err != nil || n.Sign() < 0 {
		return nil, fieldErr("new_on_hand", "Enter the quantity actually there.")
	}
	err = s.tx(ctx, func(tx pgx.Tx) error {
		return s.adjust(ctx, tx, actor, in.LotID, in.Category, n, in.Reason, in.Evidence, "")
	})
	if err != nil {
		return nil, err
	}
	return s.Lot(ctx, in.LotID)
}

func (s *Service) adjust(ctx context.Context, tx pgx.Tx, actor, lotID, category string, newOnHand qty.Q, reason, evidence, countLine string) error {
	l, err := lockLot(ctx, tx, lotID)
	if err != nil {
		return err
	}
	if newOnHand.Cmp(l.Reserved) < 0 {
		return unprocessable("reserved_stock", fmt.Sprintf("Lot %s has %s %s reserved; release reservations before adjusting below it.", l.Code, l.Reserved.String(), l.Unit))
	}
	delta := newOnHand.Sub(l.OnHand)
	var adjID string
	if err := tx.QueryRow(ctx, `INSERT INTO stock_adjustments (lot_id, category, previous_on_hand, new_on_hand, reason, evidence, count_line_id, actor_id)
		VALUES ($1, $2, $3::numeric, $4::numeric, $5, $6, $7, $8) RETURNING id`, lotID, category, l.OnHand.String(), newOnHand.String(), reason, evidence,
		nilIfEmpty(countLine), nilIfEmpty(actor)).Scan(&adjID); err != nil {
		return err
	}
	if err := apply(ctx, tx, l, movement{Type: "adjustment", OnHand: delta, Reason: human(category) + ": " + reason, RefType: "stock_adjustment", RefID: adjID, Actor: actor}); err != nil {
		return err
	}
	if err := event(ctx, tx, "inventory.stock_adjusted", "inventory_lot", lotID, adjID, map[string]any{"code": l.Code, "category": category,
		"previous": l.OnHand.Sub(delta).String(), "new": newOnHand.String(), "unit": l.Unit}); err != nil {
		return err
	}
	if delta.Sign() != 0 && s.Alerts != nil && category == "count_discrepancy" {
		if err := s.raise(ctx, tx, support.Alert{DedupeKey: "count_variance:" + adjID, Kind: "stock_count_variance", Severity: "info",
			Title: fmt.Sprintf("Count variance on %s: %s %s", l.Code, delta.String(), l.Unit), Body: reason, ResourceType: "inventory_lot", ResourceID: lotID,
			Permission: "inventory.adjust"}); err != nil {
			return err
		}
	}
	return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "inventory.adjusted", TargetType: "inventory_lot", TargetID: lotID,
		Metadata: map[string]any{"category": category, "delta": delta.String(), "reason": reason}})
}
