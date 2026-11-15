package fulfillment

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/delivery"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
)

// VerifyInput is one item checked at the packing table. Staff count the
// kits, scan (or type) the batch code from the kit label, and confirm the
// recipe card, packaging and delivery label. A quantity alone never
// verifies an item.
type VerifyInput struct {
	ItemID           string `json:"item_id"`
	CountedQuantity  *int   `json:"counted_quantity"`
	ScannedBatchCode string `json:"scanned_batch_code"`
	RecipeConfirmed  bool   `json:"recipe_confirmed"`
	PackagingOK      bool   `json:"packaging_ok"`
	LabelOK          bool   `json:"label_ok"`
	Discrepancy      string `json:"discrepancy"`
	IdempotencyKey   string `json:"idempotency_key"`
}

// Verify records one item check. A mismatch is a discrepancy: the item and
// package are marked, the fulfillment moves to exception and a blocking
// exception is raised. When every item is verified the fulfillment is.
func (s *Service) Verify(ctx context.Context, actor, id string, in VerifyInput) (*Obligation, error) {
	in.ScannedBatchCode = strings.ToUpper(strings.TrimSpace(in.ScannedBatchCode))
	in.Discrepancy = strings.TrimSpace(in.Discrepancy)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	var f []httpx.FieldError
	bad := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	if !uuidPattern.MatchString(in.ItemID) {
		bad("item_id", "Choose the item you checked.")
	}
	if in.CountedQuantity == nil || *in.CountedQuantity < 0 || *in.CountedQuantity > 500 {
		bad("counted_quantity", "Count the kits in the package.")
	}
	if in.ScannedBatchCode == "" || len(in.ScannedBatchCode) > 60 {
		bad("scanned_batch_code", "Scan or type the batch code from the kit label.")
	}
	if len([]rune(in.Discrepancy)) > 1000 {
		bad("discrepancy", "Keep the note under 1,000 characters.")
	}
	if in.IdempotencyKey == "" || len(in.IdempotencyKey) > 100 {
		bad("idempotency_key", "Missing request key.")
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		o, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		// The same submission twice (a retried tap) is answered with the current state.
		var seen int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM fulfillment_verifications WHERE item_id = $1 AND idempotency_key = $2`,
			in.ItemID, in.IdempotencyKey).Scan(&seen); err != nil {
			return err
		}
		if seen > 0 {
			return nil
		}
		if o.Status != "ready_for_verification" && o.Status != "verification_in_progress" {
			return conflict("invalid_transition", "This fulfillment is "+human(o.Status)+"; it cannot be verified now.")
		}
		var pkg, itemStatus, batchID, batchCode, recipe string
		var qty int16
		err = tx.QueryRow(ctx, `SELECT package_id::text, status, quantity, batch_id::text, batch_code, recipe_version_id::text
			FROM fulfillment_package_items WHERE id = $1 AND obligation_id = $2 FOR UPDATE`, in.ItemID, id).
			Scan(&pkg, &itemStatus, &qty, &batchID, &batchCode, &recipe)
		if errors.Is(err, pgx.ErrNoRows) {
			return fieldErr("item_id", "That item is not part of this order.")
		}
		if err != nil {
			return err
		}
		if itemStatus != "pending" {
			return conflict("already_verified", "This item was already checked ("+itemStatus+"). Resolve the exception to check it again.")
		}
		// What the system knows, checked again now (holds, packing, flags).
		var problems []string
		b, err := s.Production.BatchState(ctx, tx, batchID)
		if err != nil {
			return err
		}
		if ok, why := b.Ready(); !ok {
			problems = append(problems, why)
		}
		if b.RecipeVersionID != recipe {
			problems = append(problems, "The batch's recipe version no longer matches the order.")
		}
		if n, err := delivery.BlockingExceptions(ctx, tx, "batch_id", batchID); err != nil {
			return err
		} else if n > 0 {
			problems = append(problems, "Batch "+batchCode+" is flagged with an open problem.")
		}
		// What the person found.
		if *in.CountedQuantity != int(qty) {
			problems = append(problems, fmt.Sprintf("Counted %d kits; the order needs %d.", *in.CountedQuantity, qty))
		}
		if in.ScannedBatchCode != strings.ToUpper(batchCode) {
			problems = append(problems, "Scanned batch "+in.ScannedBatchCode+"; this item comes from "+batchCode+".")
		}
		if !in.RecipeConfirmed {
			problems = append(problems, "Recipe card not confirmed.")
		}
		if !in.PackagingOK {
			problems = append(problems, "Packaging not acceptable.")
		}
		if !in.LabelOK {
			problems = append(problems, "Delivery label not confirmed.")
		}
		if in.Discrepancy != "" {
			problems = append(problems, in.Discrepancy)
		}
		result := "verified"
		if len(problems) > 0 {
			result = "discrepancy"
		}
		note := strings.Join(problems, " ")
		if _, err := tx.Exec(ctx, `
			INSERT INTO fulfillment_verifications (obligation_id, item_id, counted_quantity, scanned_batch_code, recipe_confirmed, packaging_ok,
				label_ok, result, discrepancy, actor_user_id, idempotency_key)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			id, in.ItemID, *in.CountedQuantity, in.ScannedBatchCode, in.RecipeConfirmed, in.PackagingOK, in.LabelOK, result,
			nilIfEmpty(note), nilIfEmpty(actor), in.IdempotencyKey); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE fulfillment_package_items SET status = $2, verified_quantity = $3 WHERE id = $1`,
			in.ItemID, result, *in.CountedQuantity); err != nil {
			return err
		}
		if result == "discrepancy" {
			if _, err := tx.Exec(ctx, `UPDATE fulfillment_packages SET verification_status = 'discrepancy' WHERE id = $1`, pkg); err != nil {
				return err
			}
			from := o.Status
			if from == "ready_for_verification" {
				from = "verification_in_progress"
			}
			if err := s.move(ctx, tx, o, "exception", "Kit discrepancy: "+note, actor, `, resume_status = $3`, from); err != nil {
				return err
			}
			if _, err := s.Delivery.RaiseException(ctx, tx, delivery.ExceptionInput{Stage: "fulfillment", Type: "kit_discrepancy", Severity: "high",
				Blocking: true, Summary: "Kit discrepancy on " + o.OrderNo, Notes: note, ObligationID: id, BatchID: batchID,
				DedupeKey: "discrepancy:" + in.ItemID + ":" + in.IdempotencyKey, Actor: actor}); err != nil {
				return err
			}
			return record(ctx, tx, actor, "fulfillment.discrepancy", id, map[string]any{"item": in.ItemID, "problems": problems})
		}
		// Package verified when all its items are; the obligation when all packages are.
		if _, err := tx.Exec(ctx, `UPDATE fulfillment_packages p SET verification_status = 'verified', verified_by = $2, verified_at = now()
			WHERE p.id = $1 AND NOT EXISTS (SELECT 1 FROM fulfillment_package_items i WHERE i.package_id = p.id AND i.status <> 'verified')`,
			pkg, nilIfEmpty(actor)); err != nil {
			return err
		}
		var open int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM fulfillment_package_items WHERE obligation_id = $1 AND status <> 'verified'`, id).Scan(&open); err != nil {
			return err
		}
		if open == 0 {
			if err := s.move(ctx, tx, o, "verified", "Every kit checked", actor, `, verified_at = now()`); err != nil {
				return err
			}
			if err := event(ctx, tx, "fulfillment.verified", id, "", map[string]any{"order_id": o.OrderID}); err != nil {
				return err
			}
		} else if o.Status == "ready_for_verification" {
			if err := s.move(ctx, tx, o, "verification_in_progress", "", actor, ""); err != nil {
				return err
			}
		}
		return record(ctx, tx, actor, "fulfillment.item_verified", id, map[string]any{"item": in.ItemID})
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Release hands a verified fulfillment to delivery. Readiness is checked
// again (holds, batch flags, blocking exceptions) at this moment.
func (s *Service) Release(ctx context.Context, actor, id string, version *int) (*Obligation, error) {
	err := s.tx(ctx, func(tx pgx.Tx) error {
		o, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		if version != nil && *version != o.Version {
			return conflict("stale", "This fulfillment changed since you loaded it. Reload and try again.")
		}
		if o.Status != "verified" {
			return conflict("invalid_transition", "Only a verified fulfillment can be released; this one is "+human(o.Status)+".")
		}
		if n, err := delivery.BlockingExceptions(ctx, tx, "obligation_id", id); err != nil {
			return err
		} else if n > 0 {
			return unprocessable("blocked", "Resolve the open blocking exceptions first.")
		}
		var batches []string
		rows, err := tx.Query(ctx, `SELECT DISTINCT batch_id::text FROM fulfillment_package_items WHERE obligation_id = $1`, id)
		if err != nil {
			return err
		}
		if batches, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
			return err
		}
		for _, b := range batches {
			st, err := s.Production.BatchState(ctx, tx, b)
			if err != nil {
				return err
			}
			if ok, why := st.Ready(); !ok {
				return unprocessable("batch_not_ready", why)
			}
			if n, err := delivery.BlockingExceptions(ctx, tx, "batch_id", b); err != nil {
				return err
			} else if n > 0 {
				return unprocessable("batch_flagged", "Batch "+st.BatchCode+" is flagged with an open problem.")
			}
		}
		facts, err := s.Orders.Facts(ctx, tx, o.OrderID)
		if err != nil {
			return err
		}
		if ok, why := facts.PaymentEligible(); !ok {
			return unprocessable("not_eligible", why)
		}
		var pkgs int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM fulfillment_packages WHERE obligation_id = $1`, id).Scan(&pkgs); err != nil {
			return err
		}
		if _, err := s.Delivery.CreateFromRelease(ctx, tx, delivery.Release{ObligationID: id, OrderID: o.OrderID, OrderNumber: o.OrderNo,
			CustomerID: o.CustomerID, Week: facts.Week, Address: facts.Address, PackageCount: pkgs, Actor: actor}); err != nil {
			return err
		}
		if err := s.move(ctx, tx, o, "ready_for_dispatch", "Released to dispatch", actor, `, released_at = now()`); err != nil {
			return err
		}
		if err := event(ctx, tx, "fulfillment.ready_for_dispatch", id, "", map[string]any{"order_id": o.OrderID}); err != nil {
			return err
		}
		return record(ctx, tx, actor, "fulfillment.released", id, nil)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// FlagBatch records a problem found with a batch at packing time. It raises
// one blocking exception on the batch, which stops verification and release
// of every order holding its kits until resolved. Production's records are
// not touched.
func (s *Service) FlagBatch(ctx context.Context, actor, batchID, why string) (string, error) {
	why = strings.TrimSpace(why)
	if n := len([]rune(why)); n < 5 || n > 1000 {
		return "", fieldErr("reason", "Describe the problem (5 to 1,000 characters).")
	}
	var id string
	err := s.tx(ctx, func(tx pgx.Tx) error {
		b, err := s.Production.BatchState(ctx, tx, batchID)
		if err != nil {
			return err
		}
		id, err = s.Delivery.RaiseException(ctx, tx, delivery.ExceptionInput{Stage: "fulfillment", Type: "batch_problem", Severity: "critical",
			Blocking: true, Summary: "Problem with batch " + b.BatchCode, Notes: why, BatchID: batchID, NextAction: "escalate", Actor: actor})
		return err
	})
	return id, err
}

// Label is the fulfillment-facing package label. It carries no allergen,
// storage or shelf-life claims (those come from production's batch label)
// and only the recipient's first name and last initial.
type Label struct {
	PackageCode  string  `json:"package_code"`
	ScanCode     string  `json:"scan_code"`
	OrderNumber  string  `json:"order_number"`
	Recipient    string  `json:"recipient"`
	Region       string  `json:"region"`
	ZoneCode     *string `json:"zone_code"`
	DeliveryDate *string `json:"delivery_date"`
	Kits         int     `json:"kits"`
	Contents     []string `json:"contents"`
	Handling     string  `json:"handling"`
	Certified    bool    `json:"certified"`
	Notice       string  `json:"notice,omitempty"`
}

func (s *Service) Labels(ctx context.Context, id string) ([]Label, error) {
	o, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	var first, last string
	var zone, date *string
	if err := s.pool.QueryRow(ctx, `SELECT u.first_name, u.last_name, sz.code, d.service_date::text
		FROM fulfillment_obligations f JOIN customers c ON c.id = f.customer_id JOIN users u ON u.id = c.user_id
		LEFT JOIN deliveries d ON d.obligation_id = f.id LEFT JOIN delivery_service_zones sz ON sz.id = d.zone_id
		WHERE f.id = $1`, id).Scan(&first, &last, &zone, &date); err != nil {
		return nil, err
	}
	certified, err := s.Delivery.LabelCertified(ctx, s.pool)
	if err != nil {
		return nil, err
	}
	recipient := strings.TrimSpace(first)
	if r := []rune(strings.TrimSpace(last)); len(r) > 0 {
		recipient += " " + string(r[0]) + "."
	}
	out := []Label{}
	for _, p := range o.Packages {
		l := Label{PackageCode: p.Code, ScanCode: p.Code, OrderNumber: o.OrderNumber, Recipient: recipient, Region: o.Region, ZoneCode: zone,
			DeliveryDate: date, Certified: certified,
			Handling: "Meal-kit ingredients. Follow the storage instructions on each kit's production label."}
		for _, it := range p.Items {
			l.Kits += it.Quantity
			l.Contents = append(l.Contents, fmt.Sprintf("%d × %s (batch %s)", it.Quantity, it.MealName, it.BatchCode))
		}
		if !certified {
			l.Notice = "Not certified for live operational use: label approval is outstanding."
		}
		out = append(out, l)
	}
	return out, nil
}

// ---------------------------------------------------------------- called by delivery (in its transaction)

// Packages lists an obligation's packages for manifests and handoffs.
func (s *Service) Packages(ctx context.Context, q db.Querier, obligationID string) ([]delivery.PackageRef, error) {
	pkgs, err := s.packages(ctx, q, obligationID)
	if err != nil {
		return nil, err
	}
	out := make([]delivery.PackageRef, 0, len(pkgs))
	for _, p := range pkgs {
		ref := delivery.PackageRef{ID: p.ID, Code: p.Code}
		var parts []string
		for _, it := range p.Items {
			ref.Kits += it.Quantity
			parts = append(parts, fmt.Sprintf("%d × %s", it.Quantity, it.MealName))
		}
		ref.Contents = strings.Join(parts, ", ")
		out = append(out, ref)
	}
	return out, nil
}

func (s *Service) SetPackageHandoff(ctx context.Context, q db.Querier, packageIDs []string, status string) error {
	_, err := q.Exec(ctx, `UPDATE fulfillment_packages SET handoff_status = $2 WHERE id = ANY($1::uuid[])`, packageIDs, status)
	return err
}

func (s *Service) HandedOver(ctx context.Context, tx pgx.Tx, id, actor string) error {
	o, err := s.lock(ctx, tx, id)
	if err != nil || o.Status == "handed_over" {
		return err
	}
	return s.move(ctx, tx, o, "handed_over", "Handed to the delivery partner", actor, `, handed_over_at = now()`)
}

func (s *Service) Fulfilled(ctx context.Context, tx pgx.Tx, id, actor string) error {
	o, err := s.lock(ctx, tx, id)
	if err != nil || o.Status == "fulfilled" {
		return err
	}
	return s.move(ctx, tx, o, "fulfilled", "Delivered with accepted proof", actor, `, fulfilled_at = now()`)
}

func (s *Service) Interrupt(ctx context.Context, tx pgx.Tx, id, actor, why string) error {
	o, err := s.lock(ctx, tx, id)
	if err != nil || o.Status == "exception" || o.Status == "cancelled" || o.Status == "fulfilled" {
		return err
	}
	return s.move(ctx, tx, o, "exception", why, actor, `, resume_status = $3`, o.Status)
}

// Reopen returns a fulfilled obligation to handed over after an audited
// delivery correction (delivered → failed): the packages are treated as out
// with the partner again, exactly like a failed attempt, so a new attempt
// can be handed over and delivered.
func (s *Service) Reopen(ctx context.Context, tx pgx.Tx, id, actor, why string) error {
	o, err := s.lock(ctx, tx, id)
	if err != nil || o.Status != "fulfilled" {
		return err
	}
	return s.move(ctx, tx, o, "handed_over", why, actor, `, fulfilled_at = NULL`)
}

// Resume returns an obligation to the state its exception interrupted.
// Items in discrepancy go back to pending so they are checked again, and
// eligibility is re-checked before a fulfillment re-enters verification.
func (s *Service) Resume(ctx context.Context, tx pgx.Tx, id, actor string) error {
	o, err := s.lock(ctx, tx, id)
	if err != nil {
		return err
	}
	if o.Status != "exception" || o.Resume == nil {
		return nil
	}
	to := *o.Resume
	if to == "ready_for_verification" || to == "verification_in_progress" {
		if _, err := tx.Exec(ctx, `UPDATE fulfillment_package_items SET status = 'pending', verified_quantity = NULL
			WHERE obligation_id = $1 AND status = 'discrepancy'`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE fulfillment_packages SET verification_status = 'pending', verified_by = NULL, verified_at = NULL
			WHERE obligation_id = $1 AND verification_status = 'discrepancy'`, id); err != nil {
			return err
		}
		facts, err := s.Orders.Facts(ctx, tx, o.OrderID)
		if err != nil {
			return err
		}
		if ok, why := facts.PaymentEligible(); !ok {
			return unprocessable("not_eligible", "The fulfillment cannot resume: "+why)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE fulfillment_obligations SET status = $2, resume_status = NULL, version = version + 1, updated_at = now() WHERE id = $1`,
		id, to); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO fulfillment_events (obligation_id, from_status, to_status, reason, actor_user_id) VALUES ($1, 'exception', $2, $3, $4)`,
		id, to, "Exceptions resolved", nilIfEmpty(actor))
	return err
}

func (s *Service) Cancel(ctx context.Context, tx pgx.Tx, id, actor, why string) error {
	o, err := s.lock(ctx, tx, id)
	if err != nil || o.Status == "cancelled" {
		return err
	}
	return s.move(ctx, tx, o, "cancelled", why, actor, `, cancelled_at = now(), resume_status = NULL`)
}

// Trace is the batch-level chain for one order: package → item → batch,
// and the ingredient lots Sprint 6 linked to each batch.
type Trace struct {
	OrderNumber string       `json:"order_number"`
	Level       string       `json:"level"`
	Items       []TraceItem  `json:"items"`
	Delivery    *TraceOutcome `json:"delivery"`
}

type TraceItem struct {
	PackageCode   string     `json:"package_code"`
	MealName      string     `json:"meal_name"`
	Quantity      int        `json:"quantity"`
	BatchID       string     `json:"batch_id"`
	BatchCode     string     `json:"batch_code"`
	RecipeVersion int        `json:"recipe_version"`
	Lots          []TraceLot `json:"lots"`
}

type TraceLot struct {
	LotID        string `json:"lot_id"`
	LotCode      string `json:"lot_code"`
	Ingredient   string `json:"ingredient"`
	SupplierName string `json:"supplier_name"`
}

type TraceOutcome struct {
	Number  string     `json:"number"`
	Status  string     `json:"status"`
	Proof   *string    `json:"proof_method"`
	At      *time.Time `json:"delivered_at"`
}

func (s *Service) Trace(ctx context.Context, id string) (*Trace, error) {
	o, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	t := &Trace{OrderNumber: o.OrderNumber, Level: "batch", Items: []TraceItem{}}
	for _, p := range o.Packages {
		for _, it := range p.Items {
			ti := TraceItem{PackageCode: p.Code, MealName: it.MealName, Quantity: it.Quantity, BatchID: it.BatchID, BatchCode: it.BatchCode,
				RecipeVersion: it.RecipeVersion, Lots: []TraceLot{}}
			rows, err := s.pool.Query(ctx, `
				SELECT DISTINCT l.id::text, l.code, i.name, sp.name
				FROM batch_ingredient_lots bl JOIN inventory_lots l ON l.id = bl.lot_id JOIN ingredients i ON i.id = l.ingredient_id
				JOIN suppliers sp ON sp.id = l.supplier_id WHERE bl.batch_id = $1 ORDER BY 3, 2`, it.BatchID)
			if err != nil {
				return nil, err
			}
			if ti.Lots, err = pgx.CollectRows(rows, pgx.RowToStructByPos[TraceLot]); err != nil {
				return nil, err
			}
			t.Items = append(t.Items, ti)
		}
	}
	if o.DeliveryID != nil {
		var out TraceOutcome
		if err := s.pool.QueryRow(ctx, `SELECT d.number, d.status,
			(SELECT p.method FROM delivery_proofs p WHERE p.delivery_id = d.id AND p.status = 'accepted' ORDER BY p.created_at DESC LIMIT 1), d.delivered_at
			FROM deliveries d WHERE d.id = $1`, *o.DeliveryID).Scan(&out.Number, &out.Status, &out.Proof, &out.At); err != nil {
			return nil, err
		}
		t.Delivery = &out
	}
	return t, nil
}
