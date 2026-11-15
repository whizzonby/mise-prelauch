package production

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
)

// The read contract fulfillment (Sprint 7) uses. Production keeps its
// tables; fulfillment only reads what was made for an order through here.

// OrderKit is one meal line of an order and the batch that makes it, with
// the batch's readiness evidence: status, open holds and packaging.
type OrderKit struct {
	OrderID           string
	MealID            string
	MealName          string
	Quantity          int
	PlanID            string
	BatchID           string // "" when the plan has no batch for the meal
	BatchCode         string
	BatchStatus       string
	RecipeVersionID   string
	OpenHolds         int
	PackagingComplete bool
	LabelsVerified    bool
}

// Ready reports whether the kit is backed by a completed, unheld, packed batch.
func (k OrderKit) Ready() (bool, string) {
	switch {
	case k.BatchID == "":
		return false, k.MealName + " has no production batch."
	case k.BatchStatus != "completed":
		return false, k.MealName + " (" + k.BatchCode + ") is " + human(k.BatchStatus) + "."
	case k.OpenHolds > 0:
		return false, k.MealName + " (" + k.BatchCode + ") has an open quality hold."
	case !k.PackagingComplete || !k.LabelsVerified:
		return false, k.MealName + " (" + k.BatchCode + ") is not fully packed with verified labels."
	}
	return true, ""
}

// KitsForOrder returns an order's kits from the live (not cancelled) plan
// that includes it. ErrNotFound when no live plan includes the order.
func (s *Service) KitsForOrder(ctx context.Context, q db.Querier, orderID string) ([]OrderKit, error) {
	rows, err := q.Query(ctx, `
		SELECT l.order_id::text, l.meal_id::text, l.meal_name, l.quantity, p.id::text,
		       COALESCE(b.id::text, ''), COALESCE(b.code, ''), COALESCE(b.status, ''), COALESCE(b.recipe_version_id::text, ''),
		       (SELECT count(*) FROM quality_holds h WHERE h.batch_id = b.id AND h.status = 'open')::int,
		       COALESCE(pk.completed_at IS NOT NULL, false), COALESCE(pk.labels_verified, false)
		FROM production_plans p
		JOIN production_demand_lines l ON l.snapshot_id = p.snapshot_id
		LEFT JOIN production_batches b ON b.plan_id = p.id AND b.meal_id = l.meal_id AND b.status <> 'cancelled'
		LEFT JOIN packaging_records pk ON pk.batch_id = b.id
		WHERE l.order_id = $1 AND p.status <> 'cancelled'
		ORDER BY l.meal_name, l.meal_id`, orderID)
	if err != nil {
		return nil, err
	}
	kits, err := pgx.CollectRows(rows, pgx.RowToStructByPos[OrderKit])
	if err != nil {
		return nil, err
	}
	if len(kits) == 0 {
		return nil, httpx.ErrNotFound
	}
	return kits, nil
}

// OrdersForBatch returns the batch's plan and the orders in that plan that
// include the batch's meal. ErrNotFound when the batch is not visible.
func (s *Service) OrdersForBatch(ctx context.Context, q db.Querier, batchID string) (string, []string, error) {
	if !uuidPattern.MatchString(batchID) {
		return "", nil, httpx.ErrNotFound
	}
	var planID, mealID string
	err := q.QueryRow(ctx, `SELECT plan_id::text, meal_id::text FROM production_batches WHERE id = $1`, batchID).Scan(&planID, &mealID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, httpx.ErrNotFound
	}
	if err != nil {
		return "", nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT DISTINCT l.order_id::text FROM production_demand_lines l
		JOIN production_plans p ON p.snapshot_id = l.snapshot_id
		WHERE p.id = $1 AND l.meal_id = $2 ORDER BY 1`, planID, mealID)
	if err != nil {
		return "", nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	return planID, ids, err
}

// BatchState is a batch's current readiness evidence, for re-checks at
// verification and release.
func (s *Service) BatchState(ctx context.Context, q db.Querier, batchID string) (*OrderKit, error) {
	var k OrderKit
	err := q.QueryRow(ctx, `
		SELECT b.id::text, b.code, b.status, b.recipe_version_id::text, b.meal_name,
		       (SELECT count(*) FROM quality_holds h WHERE h.batch_id = b.id AND h.status = 'open')::int,
		       COALESCE(pk.completed_at IS NOT NULL, false), COALESCE(pk.labels_verified, false)
		FROM production_batches b LEFT JOIN packaging_records pk ON pk.batch_id = b.id WHERE b.id = $1`, batchID).
		Scan(&k.BatchID, &k.BatchCode, &k.BatchStatus, &k.RecipeVersionID, &k.MealName, &k.OpenHolds, &k.PackagingComplete, &k.LabelsVerified)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	return &k, err
}
