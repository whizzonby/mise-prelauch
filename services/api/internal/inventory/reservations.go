package inventory

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/culinary"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/qty"
	"mise.tt/api/internal/production"
)

// Reservation holds stock of one lot for a production plan.
type Reservation struct {
	ID             string    `json:"id"`
	PlanID         string    `json:"plan_id"`
	PlanNumber     string    `json:"plan_number"`
	IngredientID   string    `json:"ingredient_id"`
	IngredientName string    `json:"ingredient_name"`
	LotID          string    `json:"lot_id"`
	LotCode        string    `json:"lot_code"`
	Quantity       string    `json:"quantity"`
	OpenQuantity   string    `json:"open_quantity"`
	Unit           string    `json:"unit"`
	Status         string    `json:"status"`
	ReleaseReason  *string   `json:"release_reason"`
	CreatedByName  string    `json:"created_by_name"`
	CreatedAt      time.Time `json:"created_at"`
}

// ReservationFilter narrows the list.
type ReservationFilter struct {
	PlanID, IngredientID, LotID, Status string
}

func (s *Service) Reservations(ctx context.Context, f ReservationFilter) ([]Reservation, error) {
	return s.reservations(ctx, s.pool, f)
}

func (s *Service) reservations(ctx context.Context, q db.Querier, f ReservationFilter) ([]Reservation, error) {
	rows, err := q.Query(ctx, `SELECT r.id, r.plan_id, p.number, r.ingredient_id, i.name, r.lot_id, l.code, trim_scale(r.quantity)::text,
		trim_scale(r.open_quantity)::text, l.unit, r.status, r.release_reason, COALESCE(btrim(u.first_name || ' ' || u.last_name), ''), r.created_at
		FROM inventory_reservations r JOIN production_plans p ON p.id = r.plan_id JOIN ingredients i ON i.id = r.ingredient_id
		JOIN inventory_lots l ON l.id = r.lot_id LEFT JOIN users u ON u.id = r.created_by
		WHERE ($1 = '' OR r.plan_id::text = $1) AND ($2 = '' OR r.ingredient_id::text = $2) AND ($3 = '' OR r.lot_id::text = $3)
		AND ($4 = '' OR r.status = $4) ORDER BY r.created_at DESC LIMIT 500`, f.PlanID, f.IngredientID, f.LotID, f.Status)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Reservation])
}

// candidates locks the lots of an ingredient that may be allocated today, in
// the order the ingredient's policy gives: FEFO (earliest recorded expiry;
// lots with no date are not candidates) or FIFO (earliest received).
func (s *Service) candidates(ctx context.Context, tx pgx.Tx, ingredientID string, onlyIDs []string) ([]*lot, string, error) {
	method, err := methodFor(ctx, tx, ingredientID)
	if err != nil {
		return nil, "", err
	}
	order := `l.received_at, l.code`
	extra := ``
	if method == "fefo" {
		order = `l.expiry_date, l.received_at, l.code`
		extra = ` AND l.expiry_status = 'recorded'`
	}
	rows, err := tx.Query(ctx, `SELECT l.id FROM inventory_lots l WHERE l.ingredient_id = $2 AND `+usableSQL+extra+`
		AND l.on_hand > 0 AND (COALESCE(cardinality($3::uuid[]), 0) = 0 OR l.id = ANY ($3)) ORDER BY `+order+` FOR UPDATE`, s.today(), ingredientID, onlyIDs)
	if err != nil {
		return nil, "", err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, "", err
	}
	out := []*lot{}
	for _, id := range ids {
		l, err := lockLot(ctx, tx, id)
		if err != nil {
			return nil, "", err
		}
		out = append(out, l)
	}
	return out, method, nil
}

// ReserveInput reserves stock of an ingredient for a production plan.
type ReserveInput struct {
	PlanID       string `json:"plan_id"`
	IngredientID string `json:"ingredient_id"`
	Quantity     string `json:"quantity"`
}

// ReserveResult says what was reserved and what could not be.
type ReserveResult struct {
	Reservations []Reservation `json:"reservations"`
	Reserved     string        `json:"reserved"`
	Short        string        `json:"short"`
	Method       string        `json:"method"`
}

// Reserve holds usable stock for an approved or released plan, lot by lot in
// the ingredient's allocation order. Reservation of one ingredient is
// serialised, so two plans never reserve the same quantity: the second sees
// what the first left. What cannot be covered is reported, not invented.
func (s *Service) Reserve(ctx context.Context, actor string, in ReserveInput) (*ReserveResult, error) {
	want, err := parseQty(in.Quantity, "quantity")
	if err != nil {
		return nil, err
	}
	if !uuidPattern.MatchString(in.IngredientID) {
		return nil, fieldErr("ingredient_id", "Choose the ingredient.")
	}
	res := &ReserveResult{}
	err = s.tx(ctx, func(tx pgx.Tx) error {
		p, err := s.Production.ProcurementRequirements(ctx, tx, in.PlanID)
		if err != nil {
			if errors.Is(err, httpx.ErrNotFound) {
				return fieldErr("plan_id", "Production plan not found.")
			}
			return err
		}
		if p.Status != "approved" && p.Status != "released" {
			return conflict("plan_not_approved", "Stock is reserved only for approved or released production plans; plan "+p.Number+" is "+human(p.Status)+".")
		}
		if err := lockStock(ctx, tx, in.IngredientID); err != nil {
			return err
		}
		lots, method, err := s.candidates(ctx, tx, in.IngredientID, nil)
		if err != nil {
			return err
		}
		res.Method = method
		left := want
		for _, l := range lots {
			if left.Sign() <= 0 {
				break
			}
			free := l.OnHand.Sub(l.Reserved)
			if free.Sign() <= 0 {
				continue
			}
			take := free
			if left.Cmp(free) < 0 {
				take = left
			}
			var rid string
			if err := tx.QueryRow(ctx, `INSERT INTO inventory_reservations (plan_id, ingredient_id, lot_id, quantity, open_quantity, created_by)
				VALUES ($1, $2, $3, $4::numeric, $4::numeric, $5) RETURNING id`, p.ID, in.IngredientID, l.ID, take.String(), nilIfEmpty(actor)).Scan(&rid); err != nil {
				return err
			}
			if err := apply(ctx, tx, l, movement{Type: "reserve", Reserved: take, Reason: "Reserved for plan " + p.Number, PlanID: p.ID,
				RefType: "inventory_reservation", RefID: rid, Actor: actor}); err != nil {
				return err
			}
			left = left.Sub(take)
		}
		reserved := want.Sub(left)
		res.Reserved, res.Short = reserved.String(), left.String()
		if reserved.Sign() > 0 {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM inventory_reservations WHERE plan_id = $1 AND ingredient_id = $2`, p.ID, in.IngredientID).Scan(&n); err != nil {
				return err
			}
			if err := event(ctx, tx, "inventory.stock_reserved", "production_plan", p.ID, fmt.Sprintf("%s:%d", in.IngredientID, n), map[string]any{
				"ingredient_id": in.IngredientID, "quantity": reserved.String(), "method": method}); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "inventory.reserved", TargetType: "production_plan", TargetID: p.ID,
			Metadata: map[string]any{"ingredient_id": in.IngredientID, "asked": want.String(), "reserved": reserved.String()}})
	})
	if err != nil {
		return nil, err
	}
	res.Reservations, err = s.reservations(ctx, s.pool, ReservationFilter{PlanID: in.PlanID, IngredientID: in.IngredientID, Status: "active"})
	return res, err
}

// Release frees what is left of a reservation, with a reason (requirements
// changed, the plan was cancelled, the stock is needed elsewhere).
func (s *Service) Release(ctx context.Context, actor, id, reason string) (*Reservation, error) {
	reason = clean(reason)
	if runes(reason) < 3 || runes(reason) > 500 {
		return nil, fieldErr("reason", "Give the reason.")
	}
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var lotID, planID, status, open string
		if err := tx.QueryRow(ctx, `SELECT lot_id, plan_id, status, trim_scale(open_quantity)::text FROM inventory_reservations WHERE id = $1 FOR UPDATE`, id).
			Scan(&lotID, &planID, &status, &open); err != nil {
			return notFoundIf(err)
		}
		if status != "active" {
			return conflict("invalid_transition", "This reservation is "+status+".")
		}
		l, err := lockLot(ctx, tx, lotID)
		if err != nil {
			return err
		}
		q := qty.MustParse(open)
		if err := apply(ctx, tx, l, movement{Type: "release", Reserved: qty.Q{}.Sub(q), Reason: reason, PlanID: planID, RefType: "inventory_reservation", RefID: id, Actor: actor}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE inventory_reservations SET status = 'released', open_quantity = 0, release_reason = $2, updated_at = now() WHERE id = $1`, id, reason); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "inventory.reservation_released", TargetType: "inventory_reservation", TargetID: id,
			Metadata: map[string]any{"quantity": q.String(), "reason": reason}})
	})
	if err != nil {
		return nil, err
	}
	list, err := s.reservations(ctx, s.pool, ReservationFilter{})
	if err != nil {
		return nil, err
	}
	for _, r := range list {
		if r.ID == id {
			return &r, nil
		}
	}
	return nil, httpx.ErrNotFound
}

// IssueInput issues stock of an ingredient to a production batch.
type IssueInput struct {
	BatchID      string `json:"batch_id"`
	IngredientID string `json:"ingredient_id"`
	Quantity     string `json:"quantity"`
	// Optional: a specific lot (it must be usable).
	LotID string `json:"lot_id"`
}

// IssueResult lists what came from which lot.
type IssueResult struct {
	Batch  *production.BatchRef `json:"batch"`
	Issued []IssuedLot          `json:"issued"`
}

type IssuedLot struct {
	LotID        string `json:"lot_id"`
	LotCode      string `json:"lot_code"`
	Quantity     string `json:"quantity"`
	FromReserved string `json:"from_reserved"`
	Unit         string `json:"unit"`
}

// Issue gives stock to a released or started batch: first from the plan's
// reservations, then from unreserved usable stock, in allocation order. It
// is all or nothing for the quantity asked (issue less to issue partially).
// Two cooks issuing the same lot are serialised on the lot and the ingredient.
func (s *Service) Issue(ctx context.Context, actor string, in IssueInput) (*IssueResult, error) {
	want, err := parseQty(in.Quantity, "quantity")
	if err != nil {
		return nil, err
	}
	if !uuidPattern.MatchString(in.IngredientID) {
		return nil, fieldErr("ingredient_id", "Choose the ingredient.")
	}
	res := &IssueResult{Issued: []IssuedLot{}}
	err = s.tx(ctx, func(tx pgx.Tx) error {
		b, err := s.Production.BatchRef(ctx, tx, in.BatchID, true)
		if err != nil {
			if errors.Is(err, httpx.ErrNotFound) {
				return fieldErr("batch_id", "Batch not found.")
			}
			return err
		}
		res.Batch = b
		if b.Status != "released" && b.Status != "in_progress" {
			return conflict("batch_not_open", "Batch "+b.Code+" is "+human(b.Status)+": stock is issued to released or started batches.")
		}
		if err := lockStock(ctx, tx, in.IngredientID); err != nil {
			return err
		}
		var only []string
		if in.LotID != "" {
			if !uuidPattern.MatchString(in.LotID) {
				return fieldErr("lot_id", "Not a lot.")
			}
			only = []string{in.LotID}
		}
		lots, _, err := s.candidates(ctx, tx, in.IngredientID, only)
		if err != nil {
			return err
		}
		if in.LotID != "" && len(lots) == 0 {
			return unprocessable("lot_not_usable", "That lot cannot be issued: it is quarantined, held, rejected, expired, has no recorded expiry, or is empty.")
		}
		// The plan's open reservations on these lots, oldest first.
		type held struct {
			id   string
			open qty.Q
		}
		holds := map[string][]held{}
		rows, err := tx.Query(ctx, `SELECT id, lot_id, trim_scale(open_quantity)::text FROM inventory_reservations
			WHERE plan_id = $1 AND ingredient_id = $2 AND status = 'active' ORDER BY created_at FOR UPDATE`, b.PlanID, in.IngredientID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, lotID, open string
			if err := rows.Scan(&id, &lotID, &open); err != nil {
				rows.Close()
				return err
			}
			holds[lotID] = append(holds[lotID], held{id, qty.MustParse(open)})
		}
		rows.Close()
		// Reserved lots first, then the rest, each in allocation order.
		slices.SortStableFunc(lots, func(a, c *lot) int {
			ha, hc := len(holds[a.ID]) > 0, len(holds[c.ID]) > 0
			switch {
			case ha && !hc:
				return -1
			case hc && !ha:
				return 1
			}
			return 0
		})
		left := want
		for _, l := range lots {
			if left.Sign() <= 0 {
				break
			}
			var mine qty.Q
			for _, h := range holds[l.ID] {
				mine = mine.Add(h.open)
			}
			free := l.OnHand.Sub(l.Reserved) // unreserved
			canTake := free.Add(mine)
			if canTake.Sign() <= 0 {
				continue
			}
			take := canTake
			if left.Cmp(canTake) < 0 {
				take = left
			}
			fromRes := take
			if mine.Cmp(take) < 0 {
				fromRes = mine
			}
			// Draw down this plan's reservations on the lot.
			rest := fromRes
			for _, h := range holds[l.ID] {
				if rest.Sign() <= 0 {
					break
				}
				d := h.open
				if rest.Cmp(d) < 0 {
					d = rest
				}
				if _, err := tx.Exec(ctx, `UPDATE inventory_reservations SET open_quantity = open_quantity - $2::numeric,
					status = CASE WHEN open_quantity - $2::numeric = 0 THEN 'issued' ELSE status END, updated_at = now() WHERE id = $1`, h.id, d.String()); err != nil {
					return err
				}
				rest = rest.Sub(d)
			}
			if err := apply(ctx, tx, l, movement{Type: "issue", OnHand: qty.Q{}.Sub(take), Reserved: qty.Q{}.Sub(fromRes),
				Reason: "Issued to batch " + b.Code, BatchID: b.ID, PlanID: b.PlanID, Actor: actor}); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO batch_ingredient_lots (batch_id, lot_id, ingredient_id, unit, issued) VALUES ($1, $2, $3, $4, $5::numeric)
				ON CONFLICT (batch_id, lot_id) DO UPDATE SET issued = batch_ingredient_lots.issued + $5::numeric, updated_at = now()`,
				b.ID, l.ID, l.IngredientID, l.Unit, take.String()); err != nil {
				return err
			}
			res.Issued = append(res.Issued, IssuedLot{LotID: l.ID, LotCode: l.Code, Quantity: take.String(), FromReserved: fromRes.String(), Unit: l.Unit})
			left = left.Sub(take)
		}
		if left.Sign() > 0 {
			return unprocessable("insufficient_stock", fmt.Sprintf("Only %s of %s can be issued from usable stock; nothing was issued. Issue less, or receive more.",
				want.Sub(left).String(), want.String()))
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM inventory_movements WHERE batch_id = $1 AND type = 'issue'`, b.ID).Scan(&n); err != nil {
			return err
		}
		if err := event(ctx, tx, "inventory.stock_issued", "production_batch", b.ID, fmt.Sprint(n), map[string]any{"ingredient_id": in.IngredientID,
			"quantity": want.String(), "lots": res.Issued}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "inventory.issued", TargetType: "production_batch", TargetID: b.ID,
			Metadata: map[string]any{"ingredient_id": in.IngredientID, "quantity": want.String()}})
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// ReturnInput returns unused stock from a batch to its lot.
type ReturnInput struct {
	BatchID  string `json:"batch_id"`
	LotID    string `json:"lot_id"`
	Quantity string `json:"quantity"`
	Reason   string `json:"reason"`
}

// Return puts back what a batch did not use, up to what it was issued from
// that lot.
func (s *Service) Return(ctx context.Context, actor string, in ReturnInput) (*BatchUse, error) {
	q, err := parseQty(in.Quantity, "quantity")
	if err != nil {
		return nil, err
	}
	in.Reason = clean(in.Reason)
	if runes(in.Reason) > 500 {
		return nil, fieldErr("reason", "At most 500 characters.")
	}
	err = s.tx(ctx, func(tx pgx.Tx) error {
		b, err := s.Production.BatchRef(ctx, tx, in.BatchID, true)
		if err != nil {
			return fieldErr("batch_id", "Batch not found.")
		}
		var issued, returned string
		if err := tx.QueryRow(ctx, `SELECT trim_scale(issued)::text, trim_scale(returned)::text FROM batch_ingredient_lots WHERE batch_id = $1 AND lot_id = $2 FOR UPDATE`,
			b.ID, in.LotID).Scan(&issued, &returned); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fieldErr("lot_id", "Nothing from that lot was issued to batch "+b.Code+".")
			}
			return err
		}
		out := qty.MustParse(issued).Sub(qty.MustParse(returned))
		if q.Cmp(out) > 0 {
			return unprocessable("over_return", fmt.Sprintf("Batch %s has %s from this lot that is not yet returned.", b.Code, out.String()))
		}
		l, err := lockLot(ctx, tx, in.LotID)
		if err != nil {
			return err
		}
		reason := "Returned from batch " + b.Code
		if in.Reason != "" {
			reason += ": " + in.Reason
		}
		if err := apply(ctx, tx, l, movement{Type: "return", OnHand: q, Reason: reason, BatchID: b.ID, PlanID: b.PlanID, Actor: actor}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE batch_ingredient_lots SET returned = returned + $3::numeric, updated_at = now() WHERE batch_id = $1 AND lot_id = $2`,
			b.ID, in.LotID, q.String()); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "inventory.returned", TargetType: "production_batch", TargetID: b.ID,
			Metadata: map[string]any{"lot_id": in.LotID, "quantity": q.String()}})
	})
	if err != nil {
		return nil, err
	}
	uses, err := s.batchUses(ctx, s.pool, in.BatchID, in.LotID)
	if err != nil || len(uses) == 0 {
		return nil, err
	}
	return &uses[0], nil
}

// BatchUse is one lot's use in one batch.
type BatchUse struct {
	BatchID        string    `json:"batch_id"`
	BatchCode      string    `json:"batch_code"`
	MealName       string    `json:"meal_name"`
	BatchStatus    string    `json:"batch_status"`
	LotID          string    `json:"lot_id"`
	LotCode        string    `json:"lot_code"`
	IngredientID   string    `json:"ingredient_id"`
	IngredientName string    `json:"ingredient_name"`
	Issued         string    `json:"issued"`
	Returned       string    `json:"returned"`
	Consumed       string    `json:"consumed"`
	Unit           string    `json:"unit"`
	FirstIssuedAt  time.Time `json:"first_issued_at"`
}

func (s *Service) batchUses(ctx context.Context, q db.Querier, batchID, lotID string) ([]BatchUse, error) {
	rows, err := q.Query(ctx, `SELECT bl.batch_id, b.code, b.meal_name, b.status, bl.lot_id, l.code, bl.ingredient_id, i.name,
		trim_scale(bl.issued)::text, trim_scale(bl.returned)::text, trim_scale(bl.issued - bl.returned)::text, bl.unit, bl.first_issued_at
		FROM batch_ingredient_lots bl JOIN production_batches b ON b.id = bl.batch_id JOIN inventory_lots l ON l.id = bl.lot_id
		JOIN ingredients i ON i.id = bl.ingredient_id
		WHERE ($1 = '' OR bl.batch_id::text = $1) AND ($2 = '' OR bl.lot_id::text = $2) ORDER BY i.name, l.code`, batchID, lotID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[BatchUse])
}

// ReconcileLine compares what a batch was planned to use with what was
// issued, returned and recorded as waste.
type ReconcileLine struct {
	IngredientID   string  `json:"ingredient_id"`
	IngredientName string  `json:"ingredient_name"`
	Unit           string  `json:"unit"`
	Planned        *string `json:"planned"`
	Issued         string  `json:"issued"`
	Returned       string  `json:"returned"`
	Consumed       string  `json:"consumed"`
	// Consumed − planned; nil when planned is unknown.
	Variance *string `json:"variance"`
	// Production waste the kitchen recorded for this ingredient in this batch:
	// part of what was consumed, never a separate stock movement.
	RecordedWaste string   `json:"recorded_waste"`
	Notes         []string `json:"notes"`
}

// Reconciliation is a batch's ingredient use.
type Reconciliation struct {
	Batch       *production.BatchRef `json:"batch"`
	Lines       []ReconcileLine      `json:"lines"`
	Lots        []BatchUse           `json:"lots"`
	Definitions map[string]string    `json:"definitions"`
}

// Reconcile reports a batch's use of each ingredient. Consumption is issued
// minus returned: stock that left the store for the batch. The kitchen's
// waste records are shown inside it, so nothing is counted twice.
func (s *Service) Reconcile(ctx context.Context, batchID string) (*Reconciliation, error) {
	b, err := s.Production.BatchRef(ctx, s.pool, batchID, false)
	if err != nil {
		return nil, err
	}
	needs, err := s.Production.BatchRequirements(ctx, s.pool, batchID)
	if err != nil {
		return nil, err
	}
	uses, err := s.batchUses(ctx, s.pool, batchID, "")
	if err != nil {
		return nil, err
	}
	waste, err := s.Production.Waste(ctx, production.WasteFilter{BatchID: batchID})
	if err != nil {
		return nil, err
	}
	conv, err := culinary.NewConverter(ctx, s.pool)
	if err != nil {
		return nil, err
	}
	lines, idx := []ReconcileLine{}, map[string]int{}
	get := func(id, name, unit string) *ReconcileLine {
		if i, ok := idx[id]; ok {
			return &lines[i]
		}
		lines = append(lines, ReconcileLine{IngredientID: id, IngredientName: name, Unit: unit, Issued: "0", Returned: "0", Consumed: "0", RecordedWaste: "0", Notes: []string{}})
		idx[id] = len(lines) - 1
		return &lines[len(lines)-1]
	}
	for _, n := range needs {
		l := get(n.IngredientID, n.IngredientName, n.Unit)
		l.Planned = n.Quantity
		if n.Approximate {
			l.Notes = append(l.Notes, "The plan used an average conversion for this ingredient.")
		}
	}
	for _, u := range uses {
		l := get(u.IngredientID, u.IngredientName, u.Unit)
		l.Issued = qty.MustParse(l.Issued).Add(qty.MustParse(u.Issued)).String()
		l.Returned = qty.MustParse(l.Returned).Add(qty.MustParse(u.Returned)).String()
		l.Consumed = qty.MustParse(l.Consumed).Add(qty.MustParse(u.Consumed)).String()
	}
	for _, w := range waste {
		if w.IngredientID == nil {
			continue
		}
		i, ok := idx[*w.IngredientID]
		if !ok {
			continue
		}
		l := &lines[i]
		amount, _, err := conv.Convert(ctx, *w.IngredientID, qty.MustParse(w.Quantity), w.Unit, l.Unit)
		if err != nil {
			l.Notes = append(l.Notes, "Waste of "+w.Quantity+" "+w.Unit+" could not be converted to "+l.Unit+".")
			continue
		}
		l.RecordedWaste = qty.MustParse(l.RecordedWaste).Add(amount).Round(3).String()
	}
	for i := range lines {
		l := &lines[i]
		if l.Planned != nil {
			v := qty.MustParse(l.Consumed).Sub(qty.MustParse(*l.Planned)).String()
			l.Variance = &v
		}
		if qty.MustParse(l.Issued).IsZero() {
			l.Notes = append(l.Notes, "Nothing issued from stock yet.")
		}
		if qty.MustParse(l.RecordedWaste).Cmp(qty.MustParse(l.Consumed)) > 0 {
			l.Notes = append(l.Notes, "More waste recorded than was issued from stock: check the waste records or the issues.")
		}
	}
	return &Reconciliation{Batch: b, Lines: lines, Lots: uses, Definitions: map[string]string{
		"planned":  "What the plan's requirements give for this batch, in the base unit.",
		"issued":   "Stock issued from lots to this batch.",
		"returned": "Stock returned from the batch to its lots.",
		"consumed": "Issued minus returned: what left the store for this batch.",
		"variance": "Consumed minus planned.",
		"waste":    "The kitchen's waste records for this batch: part of consumption, not a separate stock movement.",
	}}, nil
}
