package procurement

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
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/qty"
	"mise.tt/api/internal/suppliers"
	"mise.tt/api/internal/support"
)

var contactChannels = []string{"phone", "whatsapp", "email", "in_person", "document", "other"}

// ---------------------------------------------------------------- purchase orders

// PurchaseOrder is a PO with its lines, approvals and history.
type PurchaseOrder struct {
	ID                string     `json:"id"`
	Number            string     `json:"number"`
	SupplierID        string     `json:"supplier_id"`
	SupplierName      string     `json:"supplier_name"`
	SupplierQualified bool       `json:"supplier_qualified"`
	RunID             *string    `json:"run_id"`
	RunNumber         *string    `json:"run_number"`
	Status            string     `json:"status"`
	Revision          int        `json:"revision"`
	Currency          string     `json:"currency"`
	ExpectedDelivery  *string    `json:"expected_delivery"`
	LocationID        *string    `json:"location_id"`
	LocationName      *string    `json:"location_name"`
	DeliveryFeeMinor  int64      `json:"delivery_fee_minor"`
	TaxMinor          int64      `json:"tax_minor"`
	Notes             string     `json:"notes"`
	SupplierException *string    `json:"supplier_exception"`
	BuyerID           *string    `json:"buyer_id"`
	BuyerName         string     `json:"buyer_name"`
	ApprovedByName    *string    `json:"approved_by_name"`
	ApprovedAt        *time.Time `json:"approved_at"`
	LimitApprovedBy   *string    `json:"limit_approved_by_name"`
	IssuedByName      *string    `json:"issued_by_name"`
	IssuedAt          *time.Time `json:"issued_at"`
	IssuedVia         *string    `json:"issued_via"`
	CancelReason      *string    `json:"cancel_reason"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	SubtotalMinor     int64      `json:"subtotal_minor"`
	TotalMinor        int64      `json:"total_minor"`
	// The configured approval limit and whether this order is above it.
	AboveLimit bool `json:"above_limit"`

	Lines     []POLine     `json:"lines,omitempty"`
	History   []POEvent    `json:"history,omitempty"`
	Revisions []PORevision `json:"revisions,omitempty"`
	// What still blocks the next step, in plain words.
	Blocking []string `json:"blocking"`
}

// POLine is one order line, in packs. Prices are hidden from staff without
// purchase_orders.read (receiving sees quantities only).
type POLine struct {
	ID                string  `json:"id"`
	Position          int     `json:"position"`
	ProductID         string  `json:"product_id"`
	IngredientID      *string `json:"ingredient_id"`
	Description       string  `json:"description"`
	Packs             string  `json:"packs"`
	PackQuantity      string  `json:"pack_quantity"`
	PackUnit          string  `json:"pack_unit"`
	QuantityBase      *string `json:"quantity_base"`
	BaseUnit          *string `json:"base_unit"`
	UnitPriceMinor    *int64  `json:"unit_price_minor"`
	LineTotalMinor    *int64  `json:"line_total_minor"`
	ProcurementLineID *string `json:"procurement_line_id"`
	ReceivedPacks     string  `json:"received_packs"`
	AcceptedPacks     string  `json:"accepted_packs"`
	// Still expected, in packs.
	OpenPacks string `json:"open_packs"`
}

// POEvent is one status change.
type POEvent struct {
	From      *string   `json:"from"`
	To        string    `json:"to"`
	Note      *string   `json:"note"`
	ActorName string    `json:"actor_name"`
	CreatedAt time.Time `json:"created_at"`
}

// PORevision is the order as approved at one revision. Never changed.
type PORevision struct {
	Revision       int             `json:"revision"`
	TotalMinor     int64           `json:"total_minor"`
	Reason         *string         `json:"reason"`
	ApprovedByName string          `json:"approved_by_name"`
	ApprovedAt     time.Time       `json:"approved_at"`
	Snapshot       json.RawMessage `json:"snapshot"`
}

const poCols = `po.id, po.number, po.supplier_id, s.name, s.status, s.review_expires_on::text, po.run_id, r.number, po.status, po.revision, po.currency,
	po.expected_delivery::text, po.location_id, l.name, po.delivery_fee_minor, po.tax_minor, po.notes, po.supplier_exception, po.buyer_id,
	COALESCE(btrim(b.first_name || ' ' || b.last_name), ''),
	CASE WHEN po.approved_by IS NULL THEN NULL ELSE COALESCE(btrim(a.first_name || ' ' || a.last_name), '') END, po.approved_at,
	CASE WHEN po.limit_approved_by IS NULL THEN NULL ELSE COALESCE(btrim(la.first_name || ' ' || la.last_name), '') END,
	CASE WHEN po.issued_by IS NULL THEN NULL ELSE COALESCE(btrim(i.first_name || ' ' || i.last_name), '') END, po.issued_at, po.issued_via,
	po.cancel_reason, po.created_at, po.updated_at,
	COALESCE((SELECT sum(line_total_minor) FROM purchase_order_lines pl WHERE pl.po_id = po.id), 0)::bigint`

const poFrom = ` FROM purchase_orders po JOIN suppliers s ON s.id = po.supplier_id LEFT JOIN procurement_runs r ON r.id = po.run_id
	LEFT JOIN storage_locations l ON l.id = po.location_id LEFT JOIN users b ON b.id = po.buyer_id LEFT JOIN users a ON a.id = po.approved_by
	LEFT JOIN users la ON la.id = po.limit_approved_by LEFT JOIN users i ON i.id = po.issued_by`

func (s *Service) scanPO(r pgx.Row) (PurchaseOrder, error) {
	var x PurchaseOrder
	var supStatus string
	var expires *string
	err := r.Scan(&x.ID, &x.Number, &x.SupplierID, &x.SupplierName, &supStatus, &expires, &x.RunID, &x.RunNumber, &x.Status, &x.Revision, &x.Currency,
		&x.ExpectedDelivery, &x.LocationID, &x.LocationName, &x.DeliveryFeeMinor, &x.TaxMinor, &x.Notes, &x.SupplierException, &x.BuyerID, &x.BuyerName,
		&x.ApprovedByName, &x.ApprovedAt, &x.LimitApprovedBy, &x.IssuedByName, &x.IssuedAt, &x.IssuedVia, &x.CancelReason, &x.CreatedAt, &x.UpdatedAt,
		&x.SubtotalMinor)
	if err != nil {
		return x, err
	}
	x.SupplierQualified = supStatus == "approved" && (expires == nil || *expires >= s.today().Format(time.DateOnly))
	x.TotalMinor = x.SubtotalMinor + x.DeliveryFeeMinor + x.TaxMinor
	x.Blocking = []string{}
	return x, nil
}

// POFilter narrows the PO list.
type POFilter struct {
	Status, SupplierID, RunID, Query string
	// Receivable: approved, issued or partly received.
	Receivable bool
}

func (s *Service) PurchaseOrders(ctx context.Context, p *identity.Principal, f POFilter) ([]PurchaseOrder, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+poCols+poFrom+`
		WHERE ($1 = '' OR po.status = $1) AND ($2 = '' OR po.supplier_id::text = $2) AND ($3 = '' OR po.run_id::text = $3)
		AND ($4 = '' OR po.number ILIKE '%' || $4 || '%' OR s.name ILIKE '%' || $4 || '%')
		AND (NOT $5 OR po.status IN ('approved', 'issued', 'partially_received'))
		ORDER BY po.created_at DESC LIMIT 300`, f.Status, f.SupplierID, f.RunID, strings.TrimSpace(f.Query), f.Receivable)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (PurchaseOrder, error) { return s.scanPO(r) })
	if err != nil {
		return nil, err
	}
	if !canSeePrices(p) {
		for i := range out {
			out[i].SubtotalMinor, out[i].TotalMinor, out[i].DeliveryFeeMinor, out[i].TaxMinor = 0, 0, 0, 0
		}
	}
	return out, nil
}

func canSeePrices(p *identity.Principal) bool {
	return p == nil || p.Can(identity.PermPurchaseOrdersRead)
}

// PurchaseOrder returns one PO; prices only for purchase_orders.read.
func (s *Service) PurchaseOrder(ctx context.Context, p *identity.Principal, id string) (*PurchaseOrder, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	x, err := s.po(ctx, s.pool, id)
	if err != nil {
		return nil, err
	}
	if !canSeePrices(p) {
		x.SubtotalMinor, x.TotalMinor, x.DeliveryFeeMinor, x.TaxMinor = 0, 0, 0, 0
		for i := range x.Lines {
			x.Lines[i].UnitPriceMinor, x.Lines[i].LineTotalMinor = nil, nil
		}
		x.Revisions = nil
	}
	return x, nil
}

// POIn reads a PO in the caller's transaction (inventory's receiving uses it).
func (s *Service) POIn(ctx context.Context, q db.Querier, id string) (*PurchaseOrder, error) {
	return s.po(ctx, q, id)
}

func (s *Service) po(ctx context.Context, q db.Querier, id string) (*PurchaseOrder, error) {
	x, err := s.scanPO(q.QueryRow(ctx, `SELECT `+poCols+poFrom+` WHERE po.id = $1`, id))
	if err != nil {
		return nil, notFoundIf(err)
	}
	rows, err := q.Query(ctx, `SELECT id, position, product_id, ingredient_id, description, trim_scale(packs)::text, trim_scale(pack_quantity)::text, pack_unit,
		trim_scale(quantity_base)::text, base_unit, unit_price_minor, line_total_minor, procurement_line_id, trim_scale(received_packs)::text,
		trim_scale(accepted_packs)::text, trim_scale(greatest(packs - received_packs, 0))::text
		FROM purchase_order_lines WHERE po_id = $1 ORDER BY position`, id)
	if err != nil {
		return nil, err
	}
	if x.Lines, err = pgx.CollectRows(rows, pgx.RowToStructByPos[POLine]); err != nil {
		return nil, err
	}
	rows, err = q.Query(ctx, `SELECT e.from_status, e.to_status, e.note, COALESCE(btrim(u.first_name || ' ' || u.last_name), ''), e.created_at
		FROM purchase_order_events e LEFT JOIN users u ON u.id = e.actor_id WHERE e.po_id = $1 ORDER BY e.id`, id)
	if err != nil {
		return nil, err
	}
	if x.History, err = pgx.CollectRows(rows, pgx.RowToStructByPos[POEvent]); err != nil {
		return nil, err
	}
	rows, err = q.Query(ctx, `SELECT v.revision, v.total_minor, v.reason, COALESCE(btrim(u.first_name || ' ' || u.last_name), ''), v.approved_at, v.snapshot
		FROM purchase_order_revisions v LEFT JOIN users u ON u.id = v.approved_by WHERE v.po_id = $1 ORDER BY v.revision DESC`, id)
	if err != nil {
		return nil, err
	}
	if x.Revisions, err = pgx.CollectRows(rows, pgx.RowToStructByPos[PORevision]); err != nil {
		return nil, err
	}
	set, err := s.settings(ctx, q)
	if err != nil {
		return nil, err
	}
	x.AboveLimit = set.ApprovalLimitMinor != nil && x.TotalMinor > *set.ApprovalLimitMinor
	switch x.Status {
	case "draft", "pending_approval":
		if len(x.Lines) == 0 {
			x.Blocking = append(x.Blocking, "Add at least one line.")
		}
		if x.ExpectedDelivery == nil {
			x.Blocking = append(x.Blocking, "Set the expected delivery date.")
		}
		if !x.SupplierQualified && (!set.AllowUnapprovedSuppliers || x.SupplierException == nil) {
			x.Blocking = append(x.Blocking, x.SupplierName+" is not an approved supplier; unapproved suppliers get no purchase orders unless the exception policy allows it and a reason is recorded.")
		}
		if !slices.Contains(set.Currencies, x.Currency) {
			x.Blocking = append(x.Blocking, x.Currency+" is not an approved purchasing currency.")
		}
		if x.Status == "pending_approval" && x.AboveLimit && x.ApprovedByName != nil && x.LimitApprovedBy == nil {
			x.Blocking = append(x.Blocking, "Above the approval limit: a second approval by someone allowed to approve above it.")
		}
	}
	return &x, nil
}

// POInput creates or edits a draft purchase order.
type POInput struct {
	SupplierID        string         `json:"supplier_id"`
	RunID             string         `json:"run_id"`
	Currency          string         `json:"currency"`
	ExpectedDelivery  *string        `json:"expected_delivery"`
	LocationID        *string        `json:"location_id"`
	DeliveryFeeMinor  *int64         `json:"delivery_fee_minor"`
	Notes             *string        `json:"notes"`
	SupplierException *string        `json:"supplier_exception"`
	Lines             *[]POLineInput `json:"lines"`
	// Why an approved order is changing (material change → new revision).
	Reason string `json:"reason"`
}

// POLineInput is one line. Without a price, the price in force is used.
type POLineInput struct {
	ProductID         string `json:"product_id"`
	Packs             string `json:"packs"`
	UnitPriceMinor    *int64 `json:"unit_price_minor"`
	QuotationLineID   string `json:"quotation_line_id"`
	ProcurementLineID string `json:"procurement_line_id"`
}

// CreatePO makes a draft purchase order.
func (s *Service) CreatePO(ctx context.Context, actor string, in POInput) (*PurchaseOrder, error) {
	if !uuidPattern.MatchString(in.SupplierID) {
		return nil, fieldErr("supplier_id", "Choose the supplier.")
	}
	var id string
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var err error
		id, err = s.createPO(ctx, tx, actor, in)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.po(ctx, s.pool, id)
}

func (s *Service) createPO(ctx context.Context, tx pgx.Tx, actor string, in POInput) (string, error) {
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	if in.Currency == "" {
		in.Currency = "TTD"
	}
	set, err := s.settings(ctx, tx)
	if err != nil {
		return "", err
	}
	if !slices.Contains(set.Currencies, in.Currency) {
		return "", fieldErr("currency", in.Currency+" is not an approved purchasing currency.")
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM suppliers WHERE id = $1)`, in.SupplierID).Scan(&exists); err != nil {
		return "", err
	}
	if !exists {
		return "", fieldErr("supplier_id", "Supplier not found.")
	}
	var run *string
	if in.RunID != "" {
		if !uuidPattern.MatchString(in.RunID) {
			return "", fieldErr("run_id", "Not a procurement run.")
		}
		run = &in.RunID
	}
	period := s.today().Format("0601")
	n, err := suppliers.NextNumber(ctx, tx, "PO", period)
	if err != nil {
		return "", err
	}
	var id string
	if err := tx.QueryRow(ctx, `INSERT INTO purchase_orders (number, supplier_id, run_id, currency, buyer_id) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		fmt.Sprintf("PO-%s-%04d", period, n), in.SupplierID, run, in.Currency, nilIfEmpty(actor)).Scan(&id); err != nil {
		return "", err
	}
	if err := s.applyPO(ctx, tx, id, in.SupplierID, in.Currency, in); err != nil {
		return "", err
	}
	if err := s.poEvent(ctx, tx, id, "", "draft", "", actor); err != nil {
		return "", err
	}
	return id, audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "purchase_order.created", TargetType: "purchase_order", TargetID: id})
}

// applyPO writes header fields and, when given, replaces the lines.
func (s *Service) applyPO(ctx context.Context, tx pgx.Tx, id, supplierID, currency string, in POInput) error {
	var f []httpx.FieldError
	bad := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	var expected *time.Time
	if in.ExpectedDelivery != nil && *in.ExpectedDelivery != "" {
		d, err := time.Parse(time.DateOnly, *in.ExpectedDelivery)
		if err != nil {
			bad("expected_delivery", "Enter the expected delivery date.")
		}
		expected = &d
	}
	if in.LocationID != nil && *in.LocationID != "" && !uuidPattern.MatchString(*in.LocationID) {
		bad("location_id", "Choose a storage location.")
	}
	if in.DeliveryFeeMinor != nil && (*in.DeliveryFeeMinor < 0 || *in.DeliveryFeeMinor > 100_000_000) {
		bad("delivery_fee_minor", "Enter the delivery fee in cents.")
	}
	if in.Notes != nil && runes(*in.Notes) > 2000 {
		bad("notes", "At most 2000 characters.")
	}
	if in.SupplierException != nil {
		*in.SupplierException = clean(*in.SupplierException)
		if runes(*in.SupplierException) > 500 {
			bad("supplier_exception", "At most 500 characters.")
		}
	}
	if len(f) > 0 {
		return httpx.ValidationError(f)
	}
	var loc any
	if in.LocationID != nil {
		loc = nilIfEmpty(*in.LocationID)
	}
	if _, err := tx.Exec(ctx, `UPDATE purchase_orders SET
		expected_delivery = CASE WHEN $2::boolean THEN $3::date ELSE expected_delivery END,
		location_id = CASE WHEN $4::boolean THEN $5::uuid ELSE location_id END,
		delivery_fee_minor = COALESCE($6, delivery_fee_minor), notes = COALESCE($7, notes),
		supplier_exception = CASE WHEN $8::boolean THEN NULLIF($9, '') ELSE supplier_exception END, updated_at = now()
		WHERE id = $1`, id, in.ExpectedDelivery != nil, expected, in.LocationID != nil, loc, in.DeliveryFeeMinor, in.Notes,
		in.SupplierException != nil, in.SupplierException); err != nil {
		if db.IsForeignKeyViolation(err) {
			return fieldErr("location_id", "Storage location not found.")
		}
		return err
	}
	if in.Lines == nil {
		return nil
	}
	// Received lines keep their identity: they can grow, not shrink below what came.
	type keep struct {
		id       string
		received qty.Q
	}
	existing := map[string]keep{}
	rows, err := tx.Query(ctx, `SELECT id, product_id, trim_scale(received_packs)::text FROM purchase_order_lines WHERE po_id = $1`, id)
	if err != nil {
		return err
	}
	for rows.Next() {
		var lid, prod, recv string
		if err := rows.Scan(&lid, &prod, &recv); err != nil {
			rows.Close()
			return err
		}
		existing[prod] = keep{lid, qty.MustParse(recv)}
	}
	rows.Close()
	seen := map[string]bool{}
	conv, err := culinary.NewConverter(ctx, tx)
	if err != nil {
		return err
	}
	for i, l := range *in.Lines {
		field := fmt.Sprintf("lines[%d]", i)
		p, err := s.Suppliers.ProductIn(ctx, tx, l.ProductID)
		if err != nil || p.SupplierID != supplierID {
			return fieldErr(field+".product_id", "Choose one of this supplier's products.")
		}
		if seen[p.ID] {
			return fieldErr(field+".product_id", "Each product once per order; change its packs instead.")
		}
		seen[p.ID] = true
		if !p.Active || (p.Kind == "ingredient" && p.MappingStatus != "confirmed") {
			return fieldErr(field+".product_id", p.Name+" is not purchasable: its ingredient mapping is not confirmed, or it is inactive.")
		}
		packs, err := qty.Parse(l.Packs)
		if err != nil || packs.Sign() <= 0 {
			return fieldErr(field+".packs", "Enter the number of packs.")
		}
		var price int64
		switch {
		case l.UnitPriceMinor != nil:
			if *l.UnitPriceMinor < 0 {
				return fieldErr(field+".unit_price_minor", "Enter the price per pack in cents.")
			}
			price = *l.UnitPriceMinor
		case p.Price != nil && p.Price.Currency == currency:
			price = p.Price.AmountMinor
		default:
			return fieldErr(field+".unit_price_minor", "No "+currency+" price is in force for "+p.Name+": enter the agreed price per pack.")
		}
		var qline any
		if l.QuotationLineID != "" {
			var qp int64
			var qcur, qprod string
			if err := tx.QueryRow(ctx, `SELECT ql.unit_price_minor, q.currency, ql.product_id FROM quotation_lines ql JOIN quotations q ON q.id = ql.quotation_id
				WHERE ql.id = $1`, l.QuotationLineID).Scan(&qp, &qcur, &qprod); err != nil || qprod != p.ID {
				return fieldErr(field+".quotation_line_id", "That quotation line is for a different product.")
			}
			if qcur != currency {
				return fieldErr(field+".quotation_line_id", "The quotation is in "+qcur+"; the order is in "+currency+". No conversion is assumed.")
			}
			if l.UnitPriceMinor == nil {
				price = qp
			}
			qline = l.QuotationLineID
		}
		var pline any
		if l.ProcurementLineID != "" {
			var ing string
			if err := tx.QueryRow(ctx, `SELECT ingredient_id FROM procurement_lines WHERE id = $1`, l.ProcurementLineID).Scan(&ing); err != nil ||
				p.IngredientID == nil || *p.IngredientID != ing {
				return fieldErr(field+".procurement_line_id", "That planning line is for a different ingredient.")
			}
			pline = l.ProcurementLineID
		}
		var base, baseUnit *string
		if p.IngredientID != nil && p.BaseUnit != nil {
			q, _, err := conv.Convert(ctx, *p.IngredientID, packs.Mul(qty.MustParse(p.PackQuantity)), p.PackUnit, *p.BaseUnit)
			if err != nil {
				return errIncompatible(p.Name)
			}
			v := q.String()
			base, baseUnit = &v, p.BaseUnit
		}
		total := qty.Int(price).Mul(packs).Round(0).Rat().Num().Int64()
		desc := p.Name
		if p.PackDescription != "" {
			desc += " (" + p.PackDescription + ")"
		}
		if k, ok := existing[p.ID]; ok {
			if packs.Cmp(k.received) < 0 {
				return fieldErr(field+".packs", fmt.Sprintf("%s packs of %s were already received; the order cannot be smaller.", k.received.String(), p.Name))
			}
			if _, err := tx.Exec(ctx, `UPDATE purchase_order_lines SET position = $2, description = $3, packs = $4::numeric, pack_quantity = $5::numeric,
				pack_unit = $6, quantity_base = $7::numeric, base_unit = $8, unit_price_minor = $9, line_total_minor = $10, quotation_line_id = $11,
				procurement_line_id = $12 WHERE id = $1`, k.id, -(i + 1), desc, packs.String(), p.PackQuantity, p.PackUnit, base, baseUnit, price, total, qline, pline); err != nil {
				return err
			}
			delete(existing, p.ID)
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO purchase_order_lines (po_id, position, product_id, ingredient_id, description, packs, pack_quantity, pack_unit,
			quantity_base, base_unit, unit_price_minor, line_total_minor, quotation_line_id, procurement_line_id)
			VALUES ($1, $2, $3, $4, $5, $6::numeric, $7::numeric, $8, $9::numeric, $10, $11, $12, $13, $14)`,
			id, -(i + 1), p.ID, p.IngredientID, desc, packs.String(), p.PackQuantity, p.PackUnit, base, baseUnit, price, total, qline, pline); err != nil {
			return err
		}
	}
	for prod, k := range existing {
		if k.received.Sign() > 0 {
			return unprocessable("line_received", "A line already received cannot be removed (product "+prod+").")
		}
		if _, err := tx.Exec(ctx, `DELETE FROM purchase_order_lines WHERE id = $1`, k.id); err != nil {
			return err
		}
	}
	// Positions were written negative to dodge the unique index while reordering.
	_, err = tx.Exec(ctx, `UPDATE purchase_order_lines SET position = -position WHERE po_id = $1 AND position < 0`, id)
	return err
}

func errIncompatible(name string) error {
	return unprocessable("incompatible_mapping", name+" cannot be converted to its ingredient's base unit.")
}

// UpdatePO edits a draft, or makes a material change to an approved order:
// that needs a reason and starts a new revision, which needs approving again.
// The approved revision stays as it was.
func (s *Service) UpdatePO(ctx context.Context, actor, id string, in POInput) (*PurchaseOrder, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	in.Reason = clean(in.Reason)
	err := s.tx(ctx, func(tx pgx.Tx) error {
		x, err := s.lockPO(ctx, tx, id)
		if err != nil {
			return err
		}
		switch x.Status {
		case "draft":
		case "pending_approval":
			// Editing a submitted order takes it back to draft.
			if err := s.setStatus(ctx, tx, x, "draft", "Edited after submission", actor); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE purchase_orders SET approved_by = NULL, approved_at = NULL, limit_approved_by = NULL WHERE id = $1`, id); err != nil {
				return err
			}
		case "approved", "issued", "partially_received":
			if runes(in.Reason) < 3 || runes(in.Reason) > 500 {
				return fieldErr("reason", "An approved order changes only with a reason; it then needs approving again.")
			}
			if _, err := tx.Exec(ctx, `UPDATE purchase_orders SET revision = revision + 1, approved_by = NULL, approved_at = NULL, limit_approved_by = NULL WHERE id = $1`, id); err != nil {
				return err
			}
			if err := s.setStatus(ctx, tx, x, "pending_approval", fmt.Sprintf("Revision %d: %s", x.Revision+1, in.Reason), actor); err != nil {
				return err
			}
		default:
			return conflict("invalid_transition", "A "+human(x.Status)+" order cannot change.")
		}
		if err := s.applyPO(ctx, tx, id, x.SupplierID, x.Currency, in); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "purchase_order.updated", TargetType: "purchase_order", TargetID: id,
			Metadata: map[string]any{"reason": in.Reason, "from_status": x.Status}})
	})
	if err != nil {
		return nil, err
	}
	return s.po(ctx, s.pool, id)
}

func (s *Service) lockPO(ctx context.Context, tx pgx.Tx, id string) (*PurchaseOrder, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	if _, err := tx.Exec(ctx, `SELECT 1 FROM purchase_orders WHERE id = $1 FOR UPDATE`, id); err != nil {
		return nil, err
	}
	return s.po(ctx, tx, id)
}

func (s *Service) poEvent(ctx context.Context, q db.Querier, id, from, to, note, actor string) error {
	_, err := q.Exec(ctx, `INSERT INTO purchase_order_events (po_id, from_status, to_status, note, actor_id) VALUES ($1, $2, $3, $4, $5)`,
		id, nilIfEmpty(from), to, nilIfEmpty(note), nilIfEmpty(actor))
	return err
}

func (s *Service) setStatus(ctx context.Context, tx pgx.Tx, x *PurchaseOrder, to, note, actor string) error {
	if _, err := tx.Exec(ctx, `UPDATE purchase_orders SET status = $2, updated_at = now() WHERE id = $1`, x.ID, to); err != nil {
		return err
	}
	if err := s.poEvent(ctx, tx, x.ID, x.Status, to, note, actor); err != nil {
		return err
	}
	x.Status = to
	return nil
}

// SubmitPO sends a draft for approval.
func (s *Service) SubmitPO(ctx context.Context, actor, id string) (*PurchaseOrder, error) {
	err := s.tx(ctx, func(tx pgx.Tx) error {
		x, err := s.lockPO(ctx, tx, id)
		if err != nil {
			return err
		}
		if x.Status != "draft" {
			return conflict("invalid_transition", "Only a draft can be submitted; this order is "+human(x.Status)+".")
		}
		if len(x.Blocking) > 0 {
			return unprocessable("incomplete", strings.Join(x.Blocking, " "))
		}
		if err := s.setStatus(ctx, tx, x, "pending_approval", "", actor); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "purchase_order.submitted", TargetType: "purchase_order", TargetID: id})
	})
	if err != nil {
		return nil, err
	}
	return s.po(ctx, s.pool, id)
}

// ApprovePO approves an order someone else prepared. Above the configured
// limit it also needs a holder of approve_above_limit: one person holding
// both approves it in one step; otherwise it waits for the second approval.
// Two people approving at once: the row lock makes the second wait and then
// find the order already approved.
func (s *Service) ApprovePO(ctx context.Context, p *identity.Principal, id string) (*PurchaseOrder, error) {
	actor := p.UserID
	err := s.tx(ctx, func(tx pgx.Tx) error {
		x, err := s.lockPO(ctx, tx, id)
		if err != nil {
			return err
		}
		if x.Status != "pending_approval" {
			return conflict("invalid_transition", "Only an order waiting for approval can be approved; this one is "+human(x.Status)+".")
		}
		if x.BuyerID != nil && *x.BuyerID == actor {
			return forbidden("own_order", "You prepared this order, so someone else must approve it.")
		}
		for _, b := range x.Blocking {
			if !strings.HasPrefix(b, "Above the approval limit") {
				return unprocessable("incomplete", strings.Join(x.Blocking, " "))
			}
		}
		canPrimary, canLimit := p.Can(identity.PermPurchaseOrdersApprove), p.Can(identity.PermPurchaseOrdersApproveLimit)
		first := x.ApprovedByName == nil
		switch {
		case first && !canPrimary:
			return forbidden("forbidden", "Approving purchase orders needs purchase_orders.approve.")
		case first:
			if _, err := tx.Exec(ctx, `UPDATE purchase_orders SET approved_by = $2, approved_at = now() WHERE id = $1`, id, actor); err != nil {
				return err
			}
			if x.AboveLimit && !canLimit {
				if err := s.poEvent(ctx, tx, id, x.Status, x.Status, "First approval; above the limit, waiting for a second approval.", actor); err != nil {
					return err
				}
				if err := s.raise(ctx, tx, support.Alert{DedupeKey: fmt.Sprintf("po_limit:%s:%d", id, x.Revision), Kind: "purchase_order_above_limit",
					Severity: "info", Title: x.Number + " needs approval above the limit", Body: "First approval recorded; a holder of the above-limit approval must approve it.",
					ResourceType: "purchase_order", ResourceID: id, Permission: "purchase_orders.approve_above_limit"}); err != nil {
					return err
				}
				return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "purchase_order.approved_first", TargetType: "purchase_order", TargetID: id})
			}
			if x.AboveLimit {
				if _, err := tx.Exec(ctx, `UPDATE purchase_orders SET limit_approved_by = $2 WHERE id = $1`, id, actor); err != nil {
					return err
				}
			}
		default:
			// Second approval, above the limit.
			if !canLimit {
				return forbidden("above_limit", "This order is above the approval limit and needs someone allowed to approve above it.")
			}
			var first string
			if err := tx.QueryRow(ctx, `SELECT approved_by::text FROM purchase_orders WHERE id = $1`, id).Scan(&first); err != nil {
				return err
			}
			if first == actor {
				return forbidden("same_approver", "The second approval must be by a different person.")
			}
			if _, err := tx.Exec(ctx, `UPDATE purchase_orders SET limit_approved_by = $2 WHERE id = $1`, id, actor); err != nil {
				return err
			}
		}
		return s.completeApproval(ctx, tx, x, actor)
	})
	if err != nil {
		return nil, err
	}
	return s.po(ctx, s.pool, id)
}

// completeApproval freezes the revision, records costs and events.
func (s *Service) completeApproval(ctx context.Context, tx pgx.Tx, x *PurchaseOrder, actor string) error {
	var reason *string
	if x.Revision > 1 {
		if err := tx.QueryRow(ctx, `SELECT note FROM purchase_order_events WHERE po_id = $1 AND to_status = 'pending_approval' ORDER BY id DESC LIMIT 1`, x.ID).Scan(&reason); err != nil {
			return err
		}
	}
	snapshot, _ := json.Marshal(map[string]any{"number": x.Number, "supplier": x.SupplierName, "currency": x.Currency, "expected_delivery": x.ExpectedDelivery,
		"delivery_fee_minor": x.DeliveryFeeMinor, "tax_minor": x.TaxMinor, "lines": x.Lines, "total_minor": x.TotalMinor})
	if _, err := tx.Exec(ctx, `INSERT INTO purchase_order_revisions (po_id, revision, snapshot, total_minor, reason, approved_by) VALUES ($1, $2, $3, $4, $5, $6)`,
		x.ID, x.Revision, snapshot, x.TotalMinor, reason, nilIfEmpty(actor)); err != nil {
		return err
	}
	if err := s.setStatus(ctx, tx, x, "approved", "", actor); err != nil {
		return err
	}
	day := s.today()
	for _, l := range x.Lines {
		if l.IngredientID == nil || l.UnitPriceMinor == nil {
			continue
		}
		if err := culinary.RecordSupplyCost(ctx, tx, actor, culinary.SupplyCost{IngredientID: *l.IngredientID, AmountMinor: *l.UnitPriceMinor, Currency: x.Currency,
			PerQuantity: qty.MustParse(l.PackQuantity), Unit: l.PackUnit, Origin: "purchase_order", OriginID: l.ID,
			Source: fmt.Sprintf("%s revision %d, %s", x.Number, x.Revision, x.SupplierName), EffectiveFrom: day}); err != nil {
			return err
		}
	}
	if err := event(ctx, tx, "purchase_order.approved", "purchase_order", x.ID, fmt.Sprint(x.Revision), map[string]any{
		"number": x.Number, "supplier_id": x.SupplierID, "revision": x.Revision, "total_minor": x.TotalMinor, "currency": x.Currency}); err != nil {
		return err
	}
	return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "purchase_order.approved", TargetType: "purchase_order", TargetID: x.ID,
		Metadata: map[string]any{"revision": x.Revision, "total_minor": x.TotalMinor}})
}

// RejectPO returns an order to its buyer as a draft.
func (s *Service) RejectPO(ctx context.Context, actor, id, reason string) (*PurchaseOrder, error) {
	reason = clean(reason)
	if runes(reason) < 3 || runes(reason) > 500 {
		return nil, fieldErr("reason", "Say what needs to change.")
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		x, err := s.lockPO(ctx, tx, id)
		if err != nil {
			return err
		}
		if x.Status != "pending_approval" {
			return conflict("invalid_transition", "Only an order waiting for approval can be rejected.")
		}
		if _, err := tx.Exec(ctx, `UPDATE purchase_orders SET approved_by = NULL, approved_at = NULL, limit_approved_by = NULL WHERE id = $1`, id); err != nil {
			return err
		}
		if err := s.setStatus(ctx, tx, x, "draft", "Rejected: "+reason, actor); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "purchase_order.rejected", TargetType: "purchase_order", TargetID: id,
			Metadata: map[string]any{"reason": reason}})
	})
	if err != nil {
		return nil, err
	}
	return s.po(ctx, s.pool, id)
}

// IssueInput records how the approved order reached the supplier. Mise
// sends nothing itself: staff print or download the order and say how they
// sent it.
type IssueInput struct {
	Channel string `json:"channel"`
	Note    string `json:"note"`
}

func (s *Service) IssuePO(ctx context.Context, actor, id string, in IssueInput) (*PurchaseOrder, error) {
	in.Note = strings.TrimSpace(in.Note)
	if !slices.Contains(contactChannels, in.Channel) {
		return nil, fieldErr("channel", "Say how the order reached the supplier (you send it; Mise does not).")
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		x, err := s.lockPO(ctx, tx, id)
		if err != nil {
			return err
		}
		if x.Status != "approved" {
			return conflict("invalid_transition", "Only an approved order can be issued; this one is "+human(x.Status)+".")
		}
		ok, name, _, err := s.Suppliers.Qualified(ctx, tx, x.SupplierID)
		if err != nil {
			return err
		}
		if !ok {
			set, err := s.settings(ctx, tx)
			if err != nil {
				return err
			}
			if !set.AllowUnapprovedSuppliers || x.SupplierException == nil {
				return unprocessable("supplier_not_approved", name+" is no longer an approved supplier. The order cannot be issued.")
			}
		}
		if err := s.setStatus(ctx, tx, x, "issued", in.Note, actor); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE purchase_orders SET issued_via = $2, issued_by = $3, issued_at = now() WHERE id = $1`,
			id, in.Channel, nilIfEmpty(actor)); err != nil {
			return err
		}
		summary := fmt.Sprintf("Purchase order %s (revision %d) sent by %s.", x.Number, x.Revision, strings.ReplaceAll(in.Channel, "_", " "))
		if in.Note != "" {
			summary += " " + in.Note
		}
		if err := suppliers.LogCommunication(ctx, tx, actor, x.SupplierID, suppliers.CommunicationInput{Channel: in.Channel, Direction: "outbound",
			Summary: summary, ResourceType: "purchase_order", ResourceID: id}); err != nil {
			return err
		}
		if err := event(ctx, tx, "purchase_order.issued", "purchase_order", id, fmt.Sprint(x.Revision), map[string]any{
			"number": x.Number, "supplier_id": x.SupplierID, "expected_delivery": x.ExpectedDelivery, "revision": x.Revision}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "purchase_order.issued", TargetType: "purchase_order", TargetID: id,
			Metadata: map[string]any{"channel": in.Channel}})
	})
	if err != nil {
		return nil, err
	}
	return s.po(ctx, s.pool, id)
}

// CancelPO cancels an order nothing has been received against.
func (s *Service) CancelPO(ctx context.Context, actor, id, reason string) (*PurchaseOrder, error) {
	return s.endPO(ctx, actor, id, reason, "cancelled")
}

// ClosePO closes a partly received order (nothing more is expected) or a
// received one.
func (s *Service) ClosePO(ctx context.Context, actor, id, reason string) (*PurchaseOrder, error) {
	return s.endPO(ctx, actor, id, reason, "closed")
}

func (s *Service) endPO(ctx context.Context, actor, id, reason, to string) (*PurchaseOrder, error) {
	reason = clean(reason)
	if runes(reason) < 3 || runes(reason) > 500 {
		return nil, fieldErr("reason", "Give the reason.")
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		x, err := s.lockPO(ctx, tx, id)
		if err != nil {
			return err
		}
		received := false
		for _, l := range x.Lines {
			if qty.MustParse(l.ReceivedPacks).Sign() > 0 {
				received = true
			}
		}
		switch {
		case to == "cancelled" && (received || !slices.Contains([]string{"draft", "pending_approval", "approved", "issued"}, x.Status)):
			return conflict("invalid_transition", "Only an order with nothing received can be cancelled; close it instead.")
		case to == "closed" && !slices.Contains([]string{"partially_received", "received"}, x.Status):
			return conflict("invalid_transition", "Only a received or partly received order can be closed.")
		}
		if err := s.setStatus(ctx, tx, x, to, reason, actor); err != nil {
			return err
		}
		if to == "cancelled" {
			if _, err := tx.Exec(ctx, `UPDATE purchase_orders SET cancel_reason = $2 WHERE id = $1`, id, reason); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "purchase_order." + to, TargetType: "purchase_order", TargetID: id,
			Metadata: map[string]any{"reason": reason}})
	})
	if err != nil {
		return nil, err
	}
	return s.po(ctx, s.pool, id)
}

// FromRun drafts one purchase order per supplier for the buy decisions of an
// approved run. Lines already on a live order are skipped, so asking twice
// creates nothing new.
func (s *Service) FromRun(ctx context.Context, actor, runID string) ([]PurchaseOrder, error) {
	if !uuidPattern.MatchString(runID) {
		return nil, httpx.ErrNotFound
	}
	var ids []string
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if err := lock(ctx, tx, "procurement-run-po:"+runID); err != nil {
			return err
		}
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM procurement_runs WHERE id = $1`, runID).Scan(&status); err != nil {
			return notFoundIf(err)
		}
		if status != "approved" {
			return conflict("run_not_approved", "Purchasing decisions must be approved before orders are drafted from them.")
		}
		rows, err := tx.Query(ctx, `SELECT l.id, l.product_id, trim_scale(l.packs)::text, p.supplier_id FROM procurement_lines l
			JOIN supplier_products p ON p.id = l.product_id
			WHERE l.run_id = $1 AND l.decision = 'buy'
			AND NOT EXISTS (SELECT 1 FROM purchase_order_lines pl JOIN purchase_orders po ON po.id = pl.po_id
				WHERE pl.procurement_line_id = l.id AND po.status <> 'cancelled')
			ORDER BY p.supplier_id`, runID)
		if err != nil {
			return err
		}
		type pick struct{ line, product, packs, supplier string }
		var picks []pick
		for rows.Next() {
			var x pick
			if err := rows.Scan(&x.line, &x.product, &x.packs, &x.supplier); err != nil {
				rows.Close()
				return err
			}
			picks = append(picks, x)
		}
		rows.Close()
		var required string
		if err := tx.QueryRow(ctx, `SELECT required_by::text FROM procurement_runs WHERE id = $1`, runID).Scan(&required); err != nil {
			return err
		}
		bySupplier, order := map[string][]POLineInput{}, []string{}
		for _, p := range picks {
			if _, ok := bySupplier[p.supplier]; !ok {
				order = append(order, p.supplier)
			}
			bySupplier[p.supplier] = append(bySupplier[p.supplier], POLineInput{ProductID: p.product, Packs: p.packs, ProcurementLineID: p.line})
		}
		for _, sup := range order {
			lines := bySupplier[sup]
			id, err := s.createPO(ctx, tx, actor, POInput{SupplierID: sup, RunID: runID, Currency: "TTD", Lines: &lines,
				Notes: ptr("Drafted from the approved purchasing decisions; ingredients are needed by " + required + ".")})
			if err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := []PurchaseOrder{}
	for _, id := range ids {
		x, err := s.po(ctx, s.pool, id)
		if err != nil {
			return nil, err
		}
		out = append(out, *x)
	}
	return out, nil
}

func ptr[T any](v T) *T { return &v }

// ---------------------------------------------------------------- receiving contract

// ReceivedLine is what inventory reports for one PO line on a receipt, in packs.
type ReceivedLine struct {
	POLineID  string
	Delivered qty.Q
	Accepted  qty.Q
}

// RecordReceipt is called by inventory, inside its receiving transaction,
// after locking the order with LockForReceipt. It refuses over-receiving
// beyond the configured tolerance, updates the lines and the order status,
// and writes the events.
func (s *Service) RecordReceipt(ctx context.Context, tx pgx.Tx, actor, poID, receiptID string, lines []ReceivedLine) error {
	x, err := s.po(ctx, tx, poID)
	if err != nil {
		return err
	}
	if !slices.Contains([]string{"approved", "issued", "partially_received"}, x.Status) {
		return conflict("not_receivable", "Order "+x.Number+" is "+human(x.Status)+": nothing can be received against it.")
	}
	set, err := s.settings(ctx, tx)
	if err != nil {
		return err
	}
	tolerance := qty.Q{}
	if set.ReceivingTolerancePercent != nil {
		tolerance = qty.MustParse(*set.ReceivingTolerancePercent)
	}
	byID := map[string]POLine{}
	for _, l := range x.Lines {
		byID[l.ID] = l
	}
	for _, r := range lines {
		l, ok := byID[r.POLineID]
		if !ok {
			return fieldErr("lines", "A line is not on order "+x.Number+".")
		}
		ordered := qty.MustParse(l.Packs)
		limit := ordered.Add(ordered.Mul(tolerance).Div(qty.Int(100)))
		after := qty.MustParse(l.ReceivedPacks).Add(r.Delivered)
		if after.Cmp(limit) > 0 {
			msg := fmt.Sprintf("%s: %s packs ordered, %s already received; %s more would be over the order", l.Description, l.Packs, l.ReceivedPacks, r.Delivered.String())
			if set.ReceivingTolerancePercent == nil {
				msg += " and no receiving tolerance is configured."
			} else {
				msg += " beyond the " + *set.ReceivingTolerancePercent + "% tolerance."
			}
			return unprocessable("over_receipt", msg)
		}
		if _, err := tx.Exec(ctx, `UPDATE purchase_order_lines SET received_packs = received_packs + $2::numeric, accepted_packs = accepted_packs + $3::numeric
			WHERE id = $1`, l.ID, r.Delivered.String(), r.Accepted.String()); err != nil {
			return err
		}
	}
	var open int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM purchase_order_lines WHERE po_id = $1 AND received_packs < packs`, poID).Scan(&open); err != nil {
		return err
	}
	to := "received"
	if open > 0 {
		to = "partially_received"
	}
	if to != x.Status {
		if err := s.setStatus(ctx, tx, x, to, "Receipt "+receiptID, actor); err != nil {
			return err
		}
	}
	return event(ctx, tx, "purchase_order."+to, "purchase_order", poID, receiptID, map[string]any{"number": x.Number, "receipt_id": receiptID})
}

// LockForReceipt locks an order row so two clerks recording deliveries
// against it are serialised.
func (s *Service) LockForReceipt(ctx context.Context, tx pgx.Tx, poID string) (*PurchaseOrder, error) {
	return s.lockPO(ctx, tx, poID)
}

// ---------------------------------------------------------------- RFQs and quotations

// RFQ is a request for quotation and the quotations received for it.
type RFQ struct {
	ID         string      `json:"id"`
	Number     string      `json:"number"`
	Status     string      `json:"status"`
	RunID      *string     `json:"run_id"`
	RequiredBy *string     `json:"required_by"`
	Deadline   *time.Time  `json:"deadline"`
	Notes      string      `json:"notes"`
	BuyerName  string      `json:"buyer_name"`
	CreatedAt  time.Time   `json:"created_at"`
	Lines      []RFQLine   `json:"lines"`
	Suppliers  []RFQInvite `json:"suppliers"`
	Quotations []Quotation `json:"quotations"`
}

type RFQLine struct {
	ID             string  `json:"id"`
	IngredientID   *string `json:"ingredient_id"`
	IngredientName *string `json:"ingredient_name"`
	Description    string  `json:"description"`
	Quantity       string  `json:"quantity"`
	Unit           string  `json:"unit"`
}

// RFQInvite is a supplier asked, and how staff asked them.
type RFQInvite struct {
	SupplierID   string     `json:"supplier_id"`
	SupplierName string     `json:"supplier_name"`
	Channel      *string    `json:"channel"`
	InvitedAt    *time.Time `json:"invited_at"`
	Responded    bool       `json:"responded"`
}

// Quotation is a quotation as received and recorded.
type Quotation struct {
	ID             string          `json:"id"`
	RFQID          *string         `json:"rfq_id"`
	SupplierID     string          `json:"supplier_id"`
	SupplierName   string          `json:"supplier_name"`
	Reference      string          `json:"reference"`
	Currency       string          `json:"currency"`
	LeadTimeDays   *int            `json:"lead_time_days"`
	DeliveryTerms  string          `json:"delivery_terms"`
	ValidUntil     *string         `json:"valid_until"`
	ReceivedVia    string          `json:"received_via"`
	ReceivedAt     time.Time       `json:"received_at"`
	RecordedByName string          `json:"recorded_by_name"`
	Lines          []QuotationLine `json:"lines"`
}

type QuotationLine struct {
	ID             string  `json:"id"`
	ProductID      string  `json:"product_id"`
	ProductName    string  `json:"product_name"`
	IngredientID   *string `json:"ingredient_id"`
	Packs          string  `json:"packs"`
	PackQuantity   string  `json:"pack_quantity"`
	PackUnit       string  `json:"pack_unit"`
	UnitPriceMinor int64   `json:"unit_price_minor"`
}

func (s *Service) RFQs(ctx context.Context, status string) ([]RFQ, error) {
	rows, err := s.pool.Query(ctx, `SELECT id FROM rfqs WHERE ($1 = '' OR status = $1) ORDER BY created_at DESC LIMIT 200`, status)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	out := []RFQ{}
	for _, id := range ids {
		r, err := s.rfq(ctx, s.pool, id)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, nil
}

func (s *Service) RFQ(ctx context.Context, id string) (*RFQ, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	return s.rfq(ctx, s.pool, id)
}

func (s *Service) rfq(ctx context.Context, q db.Querier, id string) (*RFQ, error) {
	var r RFQ
	err := q.QueryRow(ctx, `SELECT r.id, r.number, r.status, r.run_id, r.required_by::text, r.deadline, r.notes,
		COALESCE(btrim(u.first_name || ' ' || u.last_name), ''), r.created_at FROM rfqs r LEFT JOIN users u ON u.id = r.buyer_id WHERE r.id = $1`, id).Scan(
		&r.ID, &r.Number, &r.Status, &r.RunID, &r.RequiredBy, &r.Deadline, &r.Notes, &r.BuyerName, &r.CreatedAt)
	if err != nil {
		return nil, notFoundIf(err)
	}
	rows, err := q.Query(ctx, `SELECT l.id, l.ingredient_id, i.name, l.description, trim_scale(l.quantity)::text, l.unit
		FROM rfq_lines l LEFT JOIN ingredients i ON i.id = l.ingredient_id WHERE l.rfq_id = $1 ORDER BY l.position`, id)
	if err != nil {
		return nil, err
	}
	if r.Lines, err = pgx.CollectRows(rows, pgx.RowToStructByPos[RFQLine]); err != nil {
		return nil, err
	}
	rows, err = q.Query(ctx, `SELECT rs.supplier_id, s.name, rs.channel, rs.invited_at,
		EXISTS (SELECT 1 FROM quotations qt WHERE qt.rfq_id = rs.rfq_id AND qt.supplier_id = rs.supplier_id)
		FROM rfq_suppliers rs JOIN suppliers s ON s.id = rs.supplier_id WHERE rs.rfq_id = $1 ORDER BY s.name`, id)
	if err != nil {
		return nil, err
	}
	if r.Suppliers, err = pgx.CollectRows(rows, pgx.RowToStructByPos[RFQInvite]); err != nil {
		return nil, err
	}
	r.Quotations, err = s.quotations(ctx, q, id)
	return &r, err
}

func (s *Service) quotations(ctx context.Context, q db.Querier, rfqID string) ([]Quotation, error) {
	rows, err := q.Query(ctx, `SELECT qt.id, qt.rfq_id, qt.supplier_id, s.name, qt.reference, qt.currency, qt.lead_time_days, qt.delivery_terms,
		qt.valid_until::text, qt.received_via, qt.received_at, COALESCE(btrim(u.first_name || ' ' || u.last_name), '')
		FROM quotations qt JOIN suppliers s ON s.id = qt.supplier_id LEFT JOIN users u ON u.id = qt.recorded_by
		WHERE ($1 = '' OR qt.rfq_id::text = $1) ORDER BY qt.received_at DESC LIMIT 200`, rfqID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Quotation, error) {
		var x Quotation
		var lead *int16
		err := r.Scan(&x.ID, &x.RFQID, &x.SupplierID, &x.SupplierName, &x.Reference, &x.Currency, &lead, &x.DeliveryTerms, &x.ValidUntil,
			&x.ReceivedVia, &x.ReceivedAt, &x.RecordedByName)
		if lead != nil {
			n := int(*lead)
			x.LeadTimeDays = &n
		}
		return x, err
	})
	if err != nil {
		return nil, err
	}
	for i := range out {
		rows, err := q.Query(ctx, `SELECT ql.id, ql.product_id, p.name, p.ingredient_id, trim_scale(ql.packs)::text, trim_scale(p.pack_quantity)::text, p.pack_unit,
			ql.unit_price_minor FROM quotation_lines ql JOIN supplier_products p ON p.id = ql.product_id WHERE ql.quotation_id = $1 ORDER BY ql.position`, out[i].ID)
		if err != nil {
			return nil, err
		}
		if out[i].Lines, err = pgx.CollectRows(rows, pgx.RowToStructByPos[QuotationLine]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// RFQInput creates a request for quotation.
type RFQInput struct {
	RunID       string         `json:"run_id"`
	RequiredBy  string         `json:"required_by"`
	Deadline    string         `json:"deadline"`
	Notes       string         `json:"notes"`
	Lines       []RFQLineInput `json:"lines"`
	SupplierIDs []string       `json:"supplier_ids"`
}

type RFQLineInput struct {
	IngredientID string `json:"ingredient_id"`
	Description  string `json:"description"`
	Quantity     string `json:"quantity"`
	Unit         string `json:"unit"`
}

func (s *Service) CreateRFQ(ctx context.Context, actor string, in RFQInput) (*RFQ, error) {
	in.Notes = strings.TrimSpace(in.Notes)
	var f []httpx.FieldError
	bad := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	if len(in.Lines) == 0 || len(in.Lines) > 50 {
		bad("lines", "Ask about at least one item.")
	}
	if len(in.SupplierIDs) == 0 {
		bad("supplier_ids", "Choose at least one supplier to ask.")
	}
	var required *time.Time
	if in.RequiredBy != "" {
		d, err := time.Parse(time.DateOnly, in.RequiredBy)
		if err != nil {
			bad("required_by", "Enter a date.")
		}
		required = &d
	}
	var deadline *time.Time
	if in.Deadline != "" {
		d, err := time.Parse(time.RFC3339, in.Deadline)
		if err != nil {
			if d2, err2 := time.ParseInLocation(time.DateOnly, in.Deadline, s.Location); err2 == nil {
				d = d2.Add(24*time.Hour - time.Minute)
			} else {
				bad("deadline", "Enter the date quotations are due.")
			}
		}
		deadline = &d
	}
	if runes(in.Notes) > 2000 {
		bad("notes", "At most 2000 characters.")
	}
	var run *string
	if in.RunID != "" {
		if !uuidPattern.MatchString(in.RunID) {
			bad("run_id", "Not a procurement run.")
		}
		run = &in.RunID
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	var id string
	err := s.tx(ctx, func(tx pgx.Tx) error {
		n, err := suppliers.NextNumber(ctx, tx, "RFQ", "")
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO rfqs (number, run_id, required_by, deadline, notes, buyer_id) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			fmt.Sprintf("RFQ-%04d", n), run, required, deadline, in.Notes, nilIfEmpty(actor)).Scan(&id); err != nil {
			return err
		}
		for i, l := range in.Lines {
			field := fmt.Sprintf("lines[%d]", i)
			l.Description = clean(l.Description)
			q, err := qty.Parse(l.Quantity)
			if err != nil || q.Sign() <= 0 {
				return fieldErr(field+".quantity", "Enter the quantity.")
			}
			var ing any
			if l.IngredientID != "" {
				if !uuidPattern.MatchString(l.IngredientID) {
					return fieldErr(field+".ingredient_id", "Not an ingredient.")
				}
				ing = l.IngredientID
				if l.Description == "" {
					if err := tx.QueryRow(ctx, `SELECT name FROM ingredients WHERE id = $1`, l.IngredientID).Scan(&l.Description); err != nil {
						return fieldErr(field+".ingredient_id", "Ingredient not found.")
					}
				}
			}
			if n := runes(l.Description); n < 1 || n > 200 {
				return fieldErr(field+".description", "Describe what is wanted.")
			}
			if _, err := tx.Exec(ctx, `INSERT INTO rfq_lines (rfq_id, ingredient_id, description, quantity, unit, position) VALUES ($1, $2, $3, $4::numeric, $5, $6)`,
				id, ing, l.Description, q.String(), l.Unit, i); err != nil {
				if db.IsForeignKeyViolation(err) {
					return fieldErr(field+".unit", "Choose a unit.")
				}
				return err
			}
		}
		for _, sup := range in.SupplierIDs {
			if !uuidPattern.MatchString(sup) {
				return fieldErr("supplier_ids", "Not a supplier.")
			}
			if _, err := tx.Exec(ctx, `INSERT INTO rfq_suppliers (rfq_id, supplier_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, sup); err != nil {
				if db.IsForeignKeyViolation(err) {
					return fieldErr("supplier_ids", "Supplier not found.")
				}
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "rfq.created", TargetType: "rfq", TargetID: id})
	})
	if err != nil {
		return nil, err
	}
	return s.RFQ(ctx, id)
}

// InviteInput records that staff asked a supplier for a quotation, and how.
type InviteInput struct {
	SupplierID string `json:"supplier_id"`
	Channel    string `json:"channel"`
	Note       string `json:"note"`
}

// RecordInvitation marks a supplier as asked (the RFQ opens on the first).
// It records what a person did; nothing is sent.
func (s *Service) RecordInvitation(ctx context.Context, actor, id string, in InviteInput) (*RFQ, error) {
	if !slices.Contains(contactChannels, in.Channel) {
		return nil, fieldErr("channel", "Say how you asked them.")
	}
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var status, number string
		if err := tx.QueryRow(ctx, `SELECT status, number FROM rfqs WHERE id = $1 FOR UPDATE`, id).Scan(&status, &number); err != nil {
			return notFoundIf(err)
		}
		if status != "draft" && status != "open" {
			return conflict("invalid_transition", "This request is "+human(status)+".")
		}
		tag, err := tx.Exec(ctx, `UPDATE rfq_suppliers SET channel = $3, invited_at = now(), invited_by = $4 WHERE rfq_id = $1 AND supplier_id = $2`,
			id, in.SupplierID, in.Channel, nilIfEmpty(actor))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fieldErr("supplier_id", "That supplier is not on this request.")
		}
		if status == "draft" {
			if _, err := tx.Exec(ctx, `UPDATE rfqs SET status = 'open', updated_at = now() WHERE id = $1`, id); err != nil {
				return err
			}
		}
		summary := "Asked for a quotation (" + number + ")."
		if n := strings.TrimSpace(in.Note); n != "" {
			summary += " " + n
		}
		if err := suppliers.LogCommunication(ctx, tx, actor, in.SupplierID, suppliers.CommunicationInput{Channel: in.Channel, Direction: "outbound",
			Summary: summary, ResourceType: "rfq", ResourceID: id}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "rfq.invitation_recorded", TargetType: "rfq", TargetID: id,
			Metadata: map[string]any{"supplier_id": in.SupplierID, "channel": in.Channel}})
	})
	if err != nil {
		return nil, err
	}
	return s.RFQ(ctx, id)
}

// EndRFQ closes or cancels a request.
func (s *Service) EndRFQ(ctx context.Context, actor, id, to string) (*RFQ, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE rfqs SET status = $2, updated_at = now() WHERE id = $1 AND status IN ('draft', 'open')`, id, to)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return conflict("invalid_transition", "Only a draft or open request can be "+to+".")
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "rfq." + to, TargetType: "rfq", TargetID: id})
	})
	if err != nil {
		return nil, err
	}
	return s.RFQ(ctx, id)
}

// QuotationInput records a quotation received outside the system.
type QuotationInput struct {
	RFQID         string               `json:"rfq_id"`
	SupplierID    string               `json:"supplier_id"`
	Reference     string               `json:"reference"`
	Currency      string               `json:"currency"`
	LeadTimeDays  *int                 `json:"lead_time_days"`
	DeliveryTerms string               `json:"delivery_terms"`
	ValidUntil    string               `json:"valid_until"`
	ReceivedVia   string               `json:"received_via"`
	Lines         []QuotationLineInput `json:"lines"`
}

type QuotationLineInput struct {
	ProductID      string `json:"product_id"`
	Packs          string `json:"packs"`
	UnitPriceMinor int64  `json:"unit_price_minor"`
}

// RecordQuotation stores a quotation as received. It never changes later:
// a revised quote is a new quotation. Each line also becomes a supplier price
// (origin quotation), valid until the quotation's validity.
func (s *Service) RecordQuotation(ctx context.Context, actor string, in QuotationInput) (*Quotation, error) {
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	in.Reference, in.DeliveryTerms = clean(in.Reference), strings.TrimSpace(in.DeliveryTerms)
	var f []httpx.FieldError
	bad := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	if !uuidPattern.MatchString(in.SupplierID) {
		bad("supplier_id", "Choose the supplier.")
	}
	if len(in.Currency) != 3 {
		bad("currency", "Enter the currency of the quotation.")
	}
	if !slices.Contains(contactChannels, in.ReceivedVia) {
		bad("received_via", "Say how the quotation arrived.")
	}
	if len(in.Lines) == 0 {
		bad("lines", "Record at least one quoted product.")
	}
	if in.LeadTimeDays != nil && (*in.LeadTimeDays < 0 || *in.LeadTimeDays > 365) {
		bad("lead_time_days", "Between 0 and 365 days.")
	}
	var until *time.Time
	if in.ValidUntil != "" {
		d, err := time.Parse(time.DateOnly, in.ValidUntil)
		if err != nil {
			bad("valid_until", "Enter a date, or leave it blank.")
		}
		until = &d
	}
	if runes(in.Reference) > 120 || runes(in.DeliveryTerms) > 500 {
		bad("reference", "Too long.")
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	var id string
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var rfq any
		if in.RFQID != "" {
			var status string
			if err := tx.QueryRow(ctx, `SELECT status FROM rfqs WHERE id = $1`, in.RFQID).Scan(&status); err != nil || !uuidPattern.MatchString(in.RFQID) {
				return fieldErr("rfq_id", "Request for quotation not found.")
			}
			if status == "cancelled" {
				return conflict("invalid_transition", "That request was cancelled.")
			}
			var asked bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM rfq_suppliers WHERE rfq_id = $1 AND supplier_id = $2)`, in.RFQID, in.SupplierID).Scan(&asked); err != nil {
				return err
			}
			if !asked {
				return fieldErr("supplier_id", "That supplier was not asked on this request.")
			}
			rfq = in.RFQID
		}
		var lead any
		if in.LeadTimeDays != nil {
			lead = *in.LeadTimeDays
		}
		if err := tx.QueryRow(ctx, `INSERT INTO quotations (rfq_id, supplier_id, reference, currency, lead_time_days, delivery_terms, valid_until, received_via, recorded_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`, rfq, in.SupplierID, in.Reference, in.Currency, lead, in.DeliveryTerms, until,
			in.ReceivedVia, nilIfEmpty(actor)).Scan(&id); err != nil {
			if db.IsForeignKeyViolation(err) {
				return fieldErr("supplier_id", "Supplier not found.")
			}
			return err
		}
		validUntil := ""
		if until != nil {
			validUntil = until.Format(time.DateOnly)
		}
		for i, l := range in.Lines {
			field := fmt.Sprintf("lines[%d]", i)
			p, err := s.Suppliers.ProductIn(ctx, tx, l.ProductID)
			if err != nil || p.SupplierID != in.SupplierID {
				return fieldErr(field+".product_id", "Choose one of this supplier's products.")
			}
			packs, err := qty.Parse(l.Packs)
			if err != nil || packs.Sign() <= 0 {
				return fieldErr(field+".packs", "Enter the quoted number of packs.")
			}
			if l.UnitPriceMinor < 0 {
				return fieldErr(field+".unit_price_minor", "Enter the price per pack in cents.")
			}
			if _, err := tx.Exec(ctx, `INSERT INTO quotation_lines (quotation_id, product_id, packs, unit_price_minor, position) VALUES ($1, $2, $3::numeric, $4, $5)`,
				id, p.ID, packs.String(), l.UnitPriceMinor, i); err != nil {
				return err
			}
			if err := suppliers.RecordPrice(ctx, tx, actor, p.ID, suppliers.PriceInput{AmountMinor: l.UnitPriceMinor, Currency: in.Currency,
				ValidFrom: s.today().Format(time.DateOnly), ValidUntil: validUntil, Origin: "quotation", Note: "Quotation " + in.Reference}, id); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "quotation.recorded", TargetType: "quotation", TargetID: id,
			Metadata: map[string]any{"supplier_id": in.SupplierID, "rfq_id": in.RFQID}})
	})
	if err != nil {
		return nil, err
	}
	list, err := s.quotations(ctx, s.pool, "")
	if err != nil {
		return nil, err
	}
	for _, q := range list {
		if q.ID == id {
			return &q, nil
		}
	}
	return nil, httpx.ErrNotFound
}

// Quotations lists recorded quotations, newest first.
func (s *Service) Quotations(ctx context.Context) ([]Quotation, error) {
	return s.quotations(ctx, s.pool, "")
}

// Incoming is, per ingredient, what is still to receive on approved, issued
// or partly received orders, in the base unit. Inventory shows it as
// confirmed incoming, never as stock.
func (s *Service) Incoming(ctx context.Context, q db.Querier) (map[string]qty.Q, error) {
	rows, err := q.Query(ctx, `SELECT pl.ingredient_id, trim_scale(pl.quantity_base)::text, trim_scale(pl.packs)::text, trim_scale(pl.received_packs)::text
		FROM purchase_order_lines pl JOIN purchase_orders po ON po.id = pl.po_id
		WHERE po.status IN ('approved', 'issued', 'partially_received') AND pl.ingredient_id IS NOT NULL AND pl.received_packs < pl.packs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]qty.Q{}
	for rows.Next() {
		var ing, base, packs, received string
		if err := rows.Scan(&ing, &base, &packs, &received); err != nil {
			return nil, err
		}
		p := qty.MustParse(packs)
		out[ing] = out[ing].Add(qty.MustParse(base).Mul(p.Sub(qty.MustParse(received))).Div(p))
	}
	return out, rows.Err()
}
