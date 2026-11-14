package production

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/culinary"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/qty"
)

// The read-only contracts the supply-chain modules (Sprint 6) use. Production
// keeps owning its tables: procurement and inventory read through these and
// never query or write production tables themselves.

// ProcurementPlan is a production plan as procurement consumes it.
type ProcurementPlan struct {
	ID               string
	Number           string
	Week             string
	ProductionDate   string
	Status           string
	SnapshotID       string
	SnapshotRevision int
	// The week's current demand revision; higher than SnapshotRevision when
	// orders changed after the plan was made.
	CurrentRevision int
	Requirements    []ProcurementRequirement
}

// ProcurementRequirement is one gross requirement with the batches it is for.
type ProcurementRequirement struct {
	ID             string
	IngredientID   string
	IngredientName string
	Preparation    string
	// Base-unit quantity; nil when a source could not be converted.
	Quantity    *string
	Unit        string
	Status      string
	Approximate bool
	BatchIDs    []string
}

// ProcurementRequirements reads a plan and its requirements with q (which
// may be the caller's transaction).
func (s *Service) ProcurementRequirements(ctx context.Context, q db.Querier, planID string) (*ProcurementPlan, error) {
	if !uuidPattern.MatchString(planID) {
		return nil, httpx.ErrNotFound
	}
	var p ProcurementPlan
	err := q.QueryRow(ctx, `SELECT p.id, p.number, p.week_start::text, p.production_date::text, p.status, p.snapshot_id, s.revision,
		COALESCE((SELECT max(c.revision) FROM production_demand_snapshots c WHERE c.week_start = p.week_start AND c.status = 'current'), s.revision)
		FROM production_plans p JOIN production_demand_snapshots s ON s.id = p.snapshot_id WHERE p.id = $1`, planID).Scan(
		&p.ID, &p.Number, &p.Week, &p.ProductionDate, &p.Status, &p.SnapshotID, &p.SnapshotRevision, &p.CurrentRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT r.id, r.ingredient_id, i.name, r.preparation, trim_scale(r.quantity)::text, r.unit, r.status, r.approximate,
			COALESCE(array_agg(DISTINCT b.id) FILTER (WHERE b.id IS NOT NULL), '{}')::text[]
		FROM ingredient_requirements r JOIN ingredients i ON i.id = r.ingredient_id
		LEFT JOIN ingredient_requirement_sources src ON src.requirement_id = r.id
		LEFT JOIN production_batches b ON b.plan_item_id = src.plan_item_id AND b.status <> 'cancelled'
		WHERE r.plan_id = $1
		GROUP BY r.id, i.name ORDER BY i.name, r.preparation`, planID)
	if err != nil {
		return nil, err
	}
	p.Requirements, err = pgx.CollectRows(rows, pgx.RowToStructByPos[ProcurementRequirement])
	return &p, err
}

// ReleasedPlans lists released plans from a delivery week on: procurement's
// safety net for plans whose release event was not (yet) consumed.
func (s *Service) ReleasedPlans(ctx context.Context, q db.Querier, fromWeek string) ([]PlanRef, error) {
	rows, err := q.Query(ctx, `SELECT id, number, week_start::text, production_date::text FROM production_plans
		WHERE status = 'released' AND week_start >= $1::date ORDER BY week_start, number`, fromWeek)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[PlanRef])
}

// PlanRef identifies a plan.
type PlanRef struct {
	ID             string `json:"id"`
	Number         string `json:"number"`
	Week           string `json:"week"`
	ProductionDate string `json:"production_date"`
}

// BatchRef is a batch as inventory needs it to issue stock.
type BatchRef struct {
	ID             string `json:"id"`
	Code           string `json:"code"`
	PlanID         string `json:"plan_id"`
	PlanNumber     string `json:"plan_number"`
	MealID         string `json:"meal_id"`
	MealName       string `json:"meal_name"`
	RecipeVersion  int    `json:"recipe_version"`
	RecipeName     string `json:"recipe_name"`
	Week           string `json:"week"`
	ProductionDate string `json:"production_date"`
	Status         string `json:"status"`
}

// BatchRef reads a batch with q; lock takes a share lock on the batch row so
// it cannot change status while the caller's transaction issues stock to it.
func (s *Service) BatchRef(ctx context.Context, q db.Querier, id string, lock bool) (*BatchRef, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	suffix := ""
	if lock {
		suffix = " FOR SHARE OF b"
	}
	var b BatchRef
	err := q.QueryRow(ctx, `SELECT b.id, b.code, b.plan_id, p.number, b.meal_id, b.meal_name, v.number, r.name, p.week_start::text,
		b.production_date::text, b.status
		FROM production_batches b JOIN production_plans p ON p.id = b.plan_id
		JOIN recipe_versions v ON v.id = b.recipe_version_id JOIN recipes r ON r.id = v.recipe_id
		WHERE b.id = $1`+suffix, id).Scan(&b.ID, &b.Code, &b.PlanID, &b.PlanNumber, &b.MealID, &b.MealName, &b.RecipeVersion, &b.RecipeName,
		&b.Week, &b.ProductionDate, &b.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	return &b, err
}

// BatchNeed is one ingredient a batch was planned to use, in the base unit.
type BatchNeed struct {
	IngredientID   string  `json:"ingredient_id"`
	IngredientName string  `json:"ingredient_name"`
	Quantity       *string `json:"quantity"`
	Unit           string  `json:"unit"`
	Approximate    bool    `json:"approximate"`
}

// BatchRequirements are a batch's planned ingredients: the plan's requirement
// sources for the batch's plan item (already scaled to its kits and usable
// share), converted to each ingredient's base unit and summed over
// preparations. Nil quantity when a source cannot be converted.
func (s *Service) BatchRequirements(ctx context.Context, q db.Querier, batchID string) ([]BatchNeed, error) {
	if !uuidPattern.MatchString(batchID) {
		return nil, httpx.ErrNotFound
	}
	rows, err := q.Query(ctx, `SELECT r.ingredient_id, i.name, i.base_unit, trim_scale(src.quantity)::text, src.unit
		FROM production_batches b JOIN ingredient_requirement_sources src ON src.plan_item_id = b.plan_item_id
		JOIN ingredient_requirements r ON r.id = src.requirement_id JOIN ingredients i ON i.id = r.ingredient_id
		WHERE b.id = $1 ORDER BY i.name`, batchID)
	if err != nil {
		return nil, err
	}
	type row struct{ ing, name, base, amount, unit string }
	var list []row
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.ing, &x.name, &x.base, &x.amount, &x.unit); err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	conv, err := culinary.NewConverter(ctx, q)
	if err != nil {
		return nil, err
	}
	out, idx := []BatchNeed{}, map[string]int{}
	totals := map[string]*qty.Q{}
	for _, x := range list {
		i, ok := idx[x.ing]
		if !ok {
			out = append(out, BatchNeed{IngredientID: x.ing, IngredientName: x.name, Unit: x.base})
			i = len(out) - 1
			idx[x.ing] = i
			zero := qty.Q{}
			totals[x.ing] = &zero
		}
		if totals[x.ing] == nil {
			continue
		}
		base, approx, err := conv.Convert(ctx, x.ing, qty.MustParse(x.amount), x.unit, x.base)
		if err != nil {
			if errors.Is(err, culinary.ErrIncompatible) {
				totals[x.ing] = nil
				continue
			}
			return nil, err
		}
		t := totals[x.ing].Add(base)
		totals[x.ing] = &t
		out[i].Approximate = out[i].Approximate || approx
	}
	for i := range out {
		if t := totals[out[i].IngredientID]; t != nil {
			v := t.Round(3).String()
			out[i].Quantity = &v
		}
	}
	return out, nil
}

// BatchOrders lists the customer orders a batch was made for: the orders in
// its plan's demand snapshot that contain the batch's meal. This is
// batch-level: packing does not record which kit went into which order.
func (s *Service) BatchOrders(ctx context.Context, q db.Querier, batchID string) ([]string, error) {
	if !uuidPattern.MatchString(batchID) {
		return nil, httpx.ErrNotFound
	}
	rows, err := q.Query(ctx, `SELECT DISTINCT dl.order_number FROM production_batches b
		JOIN production_plans p ON p.id = b.plan_id
		JOIN production_demand_lines dl ON dl.snapshot_id = p.snapshot_id AND dl.meal_id = b.meal_id
		WHERE b.id = $1 ORDER BY dl.order_number`, batchID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}
