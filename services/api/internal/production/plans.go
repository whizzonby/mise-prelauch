package production

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/culinary"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/qty"
	"mise.tt/api/internal/support"
)

func alert(dedupe, kind, severity, title, body, rtype, rid, perm string) support.Alert {
	return support.Alert{DedupeKey: dedupe, Kind: kind, Severity: severity, Title: title, Body: body, ResourceType: rtype, ResourceID: rid, Permission: perm}
}

// Plan is a production plan for one delivery week.
type Plan struct {
	ID               string        `json:"id"`
	Number           string        `json:"number"`
	Week             string        `json:"week"`
	ProductionDate   string        `json:"production_date"`
	Status           string        `json:"status"`
	Notes            string        `json:"notes"`
	SnapshotID       string        `json:"snapshot_id"`
	SnapshotRevision int           `json:"snapshot_revision"`
	CreatedByName    string        `json:"created_by_name"`
	ApprovedByName   string        `json:"approved_by_name"`
	ApprovedAt       *time.Time    `json:"approved_at"`
	ReleasedAt       *time.Time    `json:"released_at"`
	CancelReason     *string       `json:"cancel_reason"`
	CreatedAt        time.Time     `json:"created_at"`
	DemandKits       int           `json:"demand_kits"`
	PlannedKits      int           `json:"planned_kits"`
	Items            []PlanItem    `json:"items,omitempty"`
	Requirements     []Requirement `json:"requirements,omitempty"`
	Batches          []Batch       `json:"batches,omitempty"`
	// Blocking: must be fixed before approval. Warnings: for the manager's judgement.
	Blocking []string  `json:"blocking"`
	Warnings []string  `json:"warnings"`
	Capacity *Capacity `json:"capacity,omitempty"`
	// A newer demand snapshot exists for the week.
	NewerSnapshot *Snapshot `json:"newer_snapshot"`
}

type PlanItem struct {
	ID               string  `json:"id"`
	MealID           string  `json:"meal_id"`
	MealName         string  `json:"meal_name"`
	RecipeVersionID  *string `json:"recipe_version_id"`
	RecipeID         *string `json:"recipe_id"`
	RecipeName       *string `json:"recipe_name"`
	RecipeVersion    *int    `json:"recipe_version"`
	ServingsPerKit   int     `json:"servings_per_kit"`
	DemandKits       int     `json:"demand_kits"`
	PlannedKits      int     `json:"planned_kits"`
	AdjustmentReason *string `json:"adjustment_reason"`
}

type Capacity struct {
	KitsPerDay *int   `json:"kits_per_day"`
	Note       string `json:"note"`
}

// Requirement is one ingredient (with one preparation) the plan needs, in
// the ingredient's base unit, with where every part of it comes from.
type Requirement struct {
	ID             string              `json:"id"`
	IngredientID   string              `json:"ingredient_id"`
	IngredientName string              `json:"ingredient_name"`
	Preparation    string              `json:"preparation"`
	Quantity       *string             `json:"quantity"`
	Unit           string              `json:"unit"`
	Status         string              `json:"status"`
	Approximate    bool                `json:"approximate"`
	Note           string              `json:"note"`
	Sources        []RequirementSource `json:"sources"`
}

type RequirementSource struct {
	MealName      string  `json:"meal_name"`
	RecipeVersion int     `json:"recipe_version"`
	BatchCode     *string `json:"batch_code"`
	Quantity      string  `json:"quantity"`
	Unit          string  `json:"unit"`
}

const planCols = `p.id, p.number, p.week_start::text, p.production_date::text, p.status, p.notes, p.snapshot_id, s.revision,
	COALESCE(btrim(c.first_name || ' ' || c.last_name), ''), COALESCE(btrim(a.first_name || ' ' || a.last_name), ''),
	p.approved_at, p.released_at, p.cancel_reason, p.created_at,
	COALESCE((SELECT sum(demand_kits) FROM production_plan_items i WHERE i.plan_id = p.id), 0)::int,
	COALESCE((SELECT sum(planned_kits) FROM production_plan_items i WHERE i.plan_id = p.id), 0)::int`

const planFrom = ` FROM production_plans p JOIN production_demand_snapshots s ON s.id = p.snapshot_id
	LEFT JOIN users c ON c.id = p.created_by LEFT JOIN users a ON a.id = p.approved_by`

func scanPlan(r pgx.Row) (Plan, error) {
	var p Plan
	err := r.Scan(&p.ID, &p.Number, &p.Week, &p.ProductionDate, &p.Status, &p.Notes, &p.SnapshotID, &p.SnapshotRevision, &p.CreatedByName,
		&p.ApprovedByName, &p.ApprovedAt, &p.ReleasedAt, &p.CancelReason, &p.CreatedAt, &p.DemandKits, &p.PlannedKits)
	p.Blocking, p.Warnings = []string{}, []string{}
	return p, err
}

// Plans lists plans, newest week first.
func (s *Service) Plans(ctx context.Context, status string) ([]Plan, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+planCols+planFrom+` WHERE ($1 = '' OR p.status = $1) ORDER BY p.week_start DESC, p.created_at DESC LIMIT 100`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Plan{}
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Plan returns a plan with its items, requirements, batches and exceptions.
func (s *Service) Plan(ctx context.Context, id string) (*Plan, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	return s.plan(ctx, s.pool, id)
}

func (s *Service) plan(ctx context.Context, q db.Querier, id string) (*Plan, error) {
	p, err := scanPlan(q.QueryRow(ctx, `SELECT `+planCols+planFrom+` WHERE p.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT i.id, i.meal_id, i.meal_name, i.recipe_version_id, v.recipe_id, r.name, v.number, i.servings_per_kit, i.demand_kits, i.planned_kits, i.adjustment_reason
		FROM production_plan_items i LEFT JOIN recipe_versions v ON v.id = i.recipe_version_id LEFT JOIN recipes r ON r.id = v.recipe_id
		WHERE i.plan_id = $1 ORDER BY i.meal_name`, id)
	if err != nil {
		return nil, err
	}
	if p.Items, err = pgx.CollectRows(rows, pgx.RowToStructByPos[PlanItem]); err != nil {
		return nil, err
	}
	if p.Requirements, err = s.requirements(ctx, q, id); err != nil {
		return nil, err
	}
	if p.Batches, err = s.batches(ctx, q, BatchFilter{PlanID: id}); err != nil {
		return nil, err
	}
	var kitsPerDay *int
	var note string
	if err := q.QueryRow(ctx, `SELECT capacity_kits_per_day, capacity_note FROM production_settings`).Scan(&kitsPerDay, &note); err != nil {
		return nil, err
	}
	p.Capacity = &Capacity{KitsPerDay: kitsPerDay, Note: note}
	week, _ := parseWeek(p.Week)
	if cur, err := s.currentSnapshot(ctx, q, week); err != nil {
		return nil, err
	} else if cur != nil && cur.ID != p.SnapshotID {
		p.NewerSnapshot = cur
	}
	s.exceptions(&p)
	return &p, nil
}

func (s *Service) exceptions(p *Plan) {
	for _, it := range p.Items {
		if it.RecipeVersionID == nil && it.PlannedKits > 0 {
			p.Blocking = append(p.Blocking, it.MealName+" has no published recipe for this week.")
		}
	}
	for _, r := range p.Requirements {
		switch r.Status {
		case "unconvertible":
			p.Blocking = append(p.Blocking, r.IngredientName+": "+r.Note)
		case "inactive_ingredient":
			p.Blocking = append(p.Blocking, r.IngredientName+" is marked inactive in the ingredient catalogue.")
		}
		if r.Approximate {
			p.Warnings = append(p.Warnings, r.IngredientName+" uses an approximate (average) conversion: weigh before portioning.")
		}
	}
	if p.Capacity != nil && p.Capacity.KitsPerDay == nil {
		p.Warnings = append(p.Warnings, "No kitchen capacity is recorded, so this plan is not checked against one.")
	} else if p.Capacity != nil && p.PlannedKits > *p.Capacity.KitsPerDay {
		p.Warnings = append(p.Warnings, fmt.Sprintf("%d kits planned against a recorded capacity of %d a day.", p.PlannedKits, *p.Capacity.KitsPerDay))
	}
	if p.NewerSnapshot != nil && (p.Status == "draft" || p.Status == "approved" || p.Status == "released") {
		p.Warnings = append(p.Warnings, fmt.Sprintf("Demand changed after this plan was made: revision %d has %d kits. The plan was not changed.", p.NewerSnapshot.Revision, p.NewerSnapshot.Kits))
	}
}

// PlanInput creates a plan for a delivery week.
type PlanInput struct {
	Week           string `json:"week"`
	ProductionDate string `json:"production_date"`
	Notes          string `json:"notes"`
}

// CreatePlan makes the draft plan for a week from its current demand
// snapshot (taking one if the demand changed). There is one live plan per
// week: asking again returns the existing plan (created=false).
func (s *Service) CreatePlan(ctx context.Context, actor string, in PlanInput) (*Plan, bool, error) {
	week, ok := parseWeek(in.Week)
	if !ok {
		return nil, false, fieldErr("week", "Choose a delivery week (a Monday).")
	}
	in.Notes = strings.TrimSpace(in.Notes)
	if runes(in.Notes) > 2000 {
		return nil, false, fieldErr("notes", "Keep notes under 2,000 characters.")
	}
	var id string
	created := false
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if err := lock(ctx, tx, "production_plan:"+week.Format(day)); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT id FROM production_plans WHERE week_start = $1 AND status <> 'cancelled'`, week).Scan(&id); err == nil {
			return nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		snap, _, err := s.snapshot(ctx, tx, actor, week)
		if err != nil {
			return err
		}
		if snap.Kits == 0 {
			return conflict("no_demand", "There are no confirmed orders for this week, so there is nothing to plan.")
		}
		prod, err := s.productionDate(ctx, tx, week, in.ProductionDate)
		if err != nil {
			return err
		}
		var n int64
		if err := tx.QueryRow(ctx, `UPDATE production_plan_counters SET last = last + 1 RETURNING last`).Scan(&n); err != nil {
			return err
		}
		number := fmt.Sprintf("PP-%s-%03d", week.Format("060102"), n%1000)
		if err := tx.QueryRow(ctx, `INSERT INTO production_plans (number, week_start, snapshot_id, production_date, notes, created_by)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`, number, week, snap.ID, prod, in.Notes, nilIfEmpty(actor)).Scan(&id); err != nil {
			if db.IsUniqueViolation(err, "production_plans_one_live") {
				return conflict("plan_exists", "A plan for this week was just created. Open it instead.")
			}
			return err
		}
		lines, err := s.snapshotMeals(ctx, tx, snap.ID)
		if err != nil {
			return err
		}
		meals, _, _, err := s.aggregate(ctx, tx, lines)
		if err != nil {
			return err
		}
		for _, m := range meals {
			if _, err := tx.Exec(ctx, `INSERT INTO production_plan_items (plan_id, meal_id, meal_name, recipe_version_id, servings_per_kit, demand_kits, planned_kits)
				VALUES ($1, $2, $3, $4, $5, $6, $6)`, id, m.MealID, m.MealName, m.RecipeVersionID, m.Servings, m.Kits); err != nil {
				return err
			}
		}
		if err := s.calculate(ctx, tx, id); err != nil {
			return err
		}
		created = true
		if err := event(ctx, tx, "production.plan_created", "production_plan", id, "", map[string]any{"week": week.Format(day), "snapshot_id": snap.ID}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "production.plan_created", TargetType: "production_plan", TargetID: id,
			Metadata: map[string]any{"week": week.Format(day), "kits": snap.Kits}})
	})
	if err != nil {
		return nil, false, err
	}
	p, err := s.Plan(ctx, id)
	return p, created, err
}

// productionDate validates a chosen date, or proposes one from the first
// configured preparation day of the week.
func (s *Service) productionDate(ctx context.Context, q db.Querier, week time.Time, v string) (time.Time, error) {
	if v != "" {
		d, ok := parseDay(v)
		if !ok || d.Before(week.AddDate(0, 0, -7)) || d.After(week.AddDate(0, 0, 6)) {
			return time.Time{}, fieldErr("production_date", "Choose a production date in the delivery week or the week before.")
		}
		return d, nil
	}
	var wd int
	err := q.QueryRow(ctx, `SELECT prep_weekday FROM production_schedule_slots WHERE status <> 'retired' ORDER BY prep_weekday LIMIT 1`).Scan(&wd)
	if errors.Is(err, pgx.ErrNoRows) {
		return week, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return week.AddDate(0, 0, wd-1), nil
}

// calculate replaces a draft plan's ingredient requirements.
//
// For every plan item with a recipe version and each line of that
// version's bill of materials:
//
//	quantity = line quantity × planned kits × servings per kit ÷ recipe yield servings
//	           ÷ (usable % ÷ 100, when the line records one)
//
// converted to the ingredient's base unit, then summed per ingredient and
// preparation. Exact rational arithmetic; rounded to 3 decimals once.
func (s *Service) calculate(ctx context.Context, tx pgx.Tx, planID string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM ingredient_requirements WHERE plan_id = $1`, planID); err != nil {
		return err
	}
	conv, err := culinary.NewConverter(ctx, tx)
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `
		SELECT i.id, i.recipe_version_id, i.planned_kits, i.servings_per_kit, v.yield_servings,
		       l.id, l.ingredient_id, trim_scale(l.quantity)::text, l.unit, l.preparation, trim_scale(l.yield_percent)::text, ing.base_unit, ing.active, ing.name
		FROM production_plan_items i
		JOIN recipe_versions v ON v.id = i.recipe_version_id
		JOIN recipe_version_ingredients l ON l.version_id = v.id
		JOIN ingredients ing ON ing.id = l.ingredient_id
		WHERE i.plan_id = $1 AND i.planned_kits > 0
		ORDER BY ing.name, l.preparation, i.meal_name`, planID)
	if err != nil {
		return err
	}
	type src struct {
		itemID, versionID, lineID string
		amount                    qty.Q
		unit                      string
	}
	type agg struct {
		ingredient, name, prep, base string
		active, approximate, broken  bool
		total                        qty.Q
		note                         string
		sources                      []src
	}
	type rawLine struct {
		itemID, versionID, lineID, ingredient, unit, prep, base, name string
		kits, servings, yield                                         int
		amount                                                        string
		yieldPct                                                      *string
		active                                                        bool
	}
	var raws []rawLine
	for rows.Next() {
		var r rawLine
		var kits, servings, yield int32
		var srv, yl int16
		_ = srv
		_ = yl
		if err := rows.Scan(&r.itemID, &r.versionID, &kits, &servings, &yield, &r.lineID, &r.ingredient, &r.amount, &r.unit, &r.prep,
			&r.yieldPct, &r.base, &r.active, &r.name); err != nil {
			rows.Close()
			return err
		}
		r.kits, r.servings, r.yield = int(kits), int(servings), int(yield)
		raws = append(raws, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	byKey := map[string]*agg{}
	var keys []string
	for _, r := range raws {
		scaled := qty.MustParse(r.amount).Mul(qty.Int(int64(r.kits) * int64(r.servings))).Div(qty.Int(int64(r.yield)))
		if r.yieldPct != nil {
			scaled = scaled.Div(qty.MustParse(*r.yieldPct).Div(qty.Int(100)))
		}
		key := r.ingredient + "|" + r.prep
		a := byKey[key]
		if a == nil {
			a = &agg{ingredient: r.ingredient, name: r.name, prep: r.prep, base: r.base, active: r.active}
			byKey[key] = a
			keys = append(keys, key)
		}
		a.sources = append(a.sources, src{itemID: r.itemID, versionID: r.versionID, lineID: r.lineID, amount: scaled, unit: r.unit})
		inBase, approx, err := conv.Convert(ctx, r.ingredient, scaled, r.unit, r.base)
		if errors.Is(err, culinary.ErrIncompatible) {
			a.broken = true
			a.note = "No conversion from " + r.unit + " to " + r.base + " is recorded for this ingredient."
			continue
		}
		if err != nil {
			return err
		}
		a.total = a.total.Add(inBase)
		a.approximate = a.approximate || approx
	}
	sort.Strings(keys)
	for _, k := range keys {
		a := byKey[k]
		status := "ok"
		var amount *string
		switch {
		case a.broken:
			status = "unconvertible"
		case !a.active:
			status = "inactive_ingredient"
		}
		if !a.broken {
			v := a.total.String()
			amount = &v
		}
		var rid string
		if err := tx.QueryRow(ctx, `INSERT INTO ingredient_requirements (plan_id, ingredient_id, preparation, quantity, unit, status, approximate, note)
			VALUES ($1, $2, $3, $4::numeric, $5, $6, $7, $8) RETURNING id`, planID, a.ingredient, a.prep, amount, a.base, status, a.approximate, a.note).Scan(&rid); err != nil {
			return err
		}
		for _, x := range a.sources {
			if _, err := tx.Exec(ctx, `INSERT INTO ingredient_requirement_sources (requirement_id, plan_item_id, recipe_version_id, bom_line_id, quantity, unit)
				VALUES ($1, $2, $3, $4, $5::numeric, $6)`, rid, x.itemID, x.versionID, x.lineID, x.amount.String(), x.unit); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) requirements(ctx context.Context, q db.Querier, planID string) ([]Requirement, error) {
	rows, err := q.Query(ctx, `
		SELECT r.id, r.ingredient_id, i.name, r.preparation, trim_scale(r.quantity)::text, r.unit, r.status, r.approximate, r.note
		FROM ingredient_requirements r JOIN ingredients i ON i.id = r.ingredient_id
		WHERE r.plan_id = $1 ORDER BY i.name, r.preparation`, planID)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Requirement, error) {
		var x Requirement
		err := r.Scan(&x.ID, &x.IngredientID, &x.IngredientName, &x.Preparation, &x.Quantity, &x.Unit, &x.Status, &x.Approximate, &x.Note)
		x.Sources = []RequirementSource{}
		return x, err
	})
	if err != nil {
		return nil, err
	}
	idx := map[string]int{}
	for i, r := range list {
		idx[r.ID] = i
	}
	rows, err = q.Query(ctx, `
		SELECT s.requirement_id, i.meal_name, v.number, b.code, trim_scale(s.quantity)::text, s.unit
		FROM ingredient_requirement_sources s
		JOIN ingredient_requirements r ON r.id = s.requirement_id
		JOIN production_plan_items i ON i.id = s.plan_item_id
		JOIN recipe_versions v ON v.id = s.recipe_version_id
		LEFT JOIN production_batches b ON b.plan_item_id = i.id
		WHERE r.plan_id = $1 ORDER BY i.meal_name`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var rid string
		var src RequirementSource
		if err := rows.Scan(&rid, &src.MealName, &src.RecipeVersion, &src.BatchCode, &src.Quantity, &src.Unit); err != nil {
			return nil, err
		}
		if i, ok := idx[rid]; ok {
			list[i].Sources = append(list[i].Sources, src)
		}
	}
	return list, rows.Err()
}

// AdjustInput changes how many kits of one meal a draft plan makes.
type AdjustInput struct {
	PlannedKits int    `json:"planned_kits"`
	Reason      string `json:"reason"`
}

// AdjustItem changes the kits to make. It never changes an order: demand
// stays as ordered, and the difference needs a reason.
func (s *Service) AdjustItem(ctx context.Context, actor, planID, itemID string, in AdjustInput) (*Plan, error) {
	if !uuidPattern.MatchString(planID) || !uuidPattern.MatchString(itemID) {
		return nil, httpx.ErrNotFound
	}
	if in.PlannedKits < 0 || in.PlannedKits > 100000 {
		return nil, fieldErr("planned_kits", "Enter the number of kits to make.")
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM production_plans WHERE id = $1 FOR UPDATE`, planID).Scan(&status); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpx.ErrNotFound
			}
			return err
		}
		if status != "draft" {
			return conflict("plan_locked", "Only a draft plan can be adjusted. This plan is "+human(status)+".")
		}
		var demand, before int
		if err := tx.QueryRow(ctx, `SELECT demand_kits, planned_kits FROM production_plan_items WHERE id = $1 AND plan_id = $2`, itemID, planID).Scan(&demand, &before); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpx.ErrNotFound
			}
			return err
		}
		var why *string
		if in.PlannedKits != demand {
			r, err := reason(in.Reason, 3)
			if err != nil {
				return err
			}
			why = &r
		}
		if _, err := tx.Exec(ctx, `UPDATE production_plan_items SET planned_kits = $2, adjustment_reason = $3, adjusted_by = $4 WHERE id = $1`,
			itemID, in.PlannedKits, why, nilIfEmpty(actor)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE production_plans SET updated_at = now() WHERE id = $1`, planID); err != nil {
			return err
		}
		if err := s.calculate(ctx, tx, planID); err != nil {
			return err
		}
		r := ""
		if why != nil {
			r = *why
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "production.plan_adjusted", TargetType: "production_plan", TargetID: planID,
			Reason: r, Metadata: map[string]any{"item": itemID, "demand_kits": demand, "from": before, "to": in.PlannedKits}})
	})
	if err != nil {
		return nil, err
	}
	return s.Plan(ctx, planID)
}

// lockPlan loads and locks a plan for a transition.
func (s *Service) lockPlan(ctx context.Context, tx pgx.Tx, id string, from ...string) (*Plan, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM production_plans WHERE id = $1 FOR UPDATE`, id).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, httpx.ErrNotFound
		}
		return nil, err
	}
	ok := false
	for _, f := range from {
		ok = ok || f == status
	}
	if !ok {
		return nil, conflict("invalid_transition", "This plan is "+human(status)+".")
	}
	return s.plan(ctx, tx, id)
}

// Approve fixes a draft plan: its requirements are frozen and a batch is
// prepared for every meal with kits to make.
func (s *Service) Approve(ctx context.Context, actor, id string) (*Plan, error) {
	err := s.tx(ctx, func(tx pgx.Tx) error {
		p, err := s.lockPlan(ctx, tx, id, "draft")
		if err != nil {
			return err
		}
		if len(p.Blocking) > 0 {
			return &httpx.Error{Status: 422, Code: "plan_blocked", Message: "This plan cannot be approved yet: " + strings.Join(p.Blocking, " ")}
		}
		if p.PlannedKits == 0 {
			return &httpx.Error{Status: 422, Code: "plan_blocked", Message: "The plan makes no kits."}
		}
		prod, _ := parseDay(p.ProductionDate)
		var seq int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM production_batches WHERE production_date = $1`, prod).Scan(&seq); err != nil {
			return err
		}
		for _, it := range p.Items {
			if it.PlannedKits == 0 {
				continue
			}
			seq++
			code := fmt.Sprintf("B%s-%02d", prod.Format("20060102"), seq)
			if _, err := tx.Exec(ctx, `INSERT INTO production_batches (code, plan_id, plan_item_id, meal_id, meal_name, recipe_version_id, production_date, planned_kits)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT (plan_item_id) DO NOTHING`,
				code, id, it.ID, it.MealID, it.MealName, *it.RecipeVersionID, prod, it.PlannedKits); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE production_plans SET status = 'approved', approved_by = $2, approved_at = now(), updated_at = now() WHERE id = $1`, id, nilIfEmpty(actor)); err != nil {
			return err
		}
		if err := event(ctx, tx, "production.plan_approved", "production_plan", id, "", map[string]any{"week": p.Week, "kits": p.PlannedKits}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "production.plan_approved", TargetType: "production_plan", TargetID: id,
			Metadata: map[string]any{"kits": p.PlannedKits}})
	})
	if err != nil {
		return nil, err
	}
	return s.Plan(ctx, id)
}

// Release sends an approved plan to the kitchen: batches become released,
// kitchen tasks are made from the active templates, and the week's orders
// move to awaiting production. Releasing twice changes nothing.
func (s *Service) Release(ctx context.Context, actor, id string) (*Plan, error) {
	err := s.tx(ctx, func(tx pgx.Tx) error {
		p, err := s.lockPlan(ctx, tx, id, "approved")
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE production_batches SET status = 'released', updated_at = now() WHERE plan_id = $1 AND status = 'planned'`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO production_batch_events (batch_id, from_status, to_status, actor_id)
			SELECT id, 'planned', 'released', $2 FROM production_batches WHERE plan_id = $1 AND status = 'released'`, id, nilIfEmpty(actor)); err != nil {
			return err
		}
		tasks, err := s.generateTasks(ctx, tx, id, actor)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE production_plans SET status = 'released', released_by = $2, released_at = now(), updated_at = now() WHERE id = $1`, id, nilIfEmpty(actor)); err != nil {
			return err
		}
		ids, err := s.planOrders(ctx, tx, id, "")
		if err != nil {
			return err
		}
		if _, err := s.Orders.Advance(ctx, tx, ids, []string{"confirmed"}, "awaiting_production", "Production plan "+p.Number+" released", actor); err != nil {
			return err
		}
		reqs := []map[string]any{}
		for _, r := range p.Requirements {
			reqs = append(reqs, map[string]any{"ingredient_id": r.IngredientID, "preparation": r.Preparation, "quantity": r.Quantity, "unit": r.Unit})
		}
		if err := event(ctx, tx, "production.plan_released", "production_plan", id, "", map[string]any{
			"week": p.Week, "production_date": p.ProductionDate, "kits": p.PlannedKits, "requirements": reqs}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "production.plan_released", TargetType: "production_plan", TargetID: id,
			Metadata: map[string]any{"tasks": tasks, "orders": len(ids)}})
	})
	if err != nil {
		return nil, err
	}
	return s.Plan(ctx, id)
}

// planOrders lists the orders in a plan's snapshot, optionally only those
// containing one meal.
func (s *Service) planOrders(ctx context.Context, q db.Querier, planID, mealID string) ([]string, error) {
	rows, err := q.Query(ctx, `SELECT DISTINCT l.order_id::text FROM production_demand_lines l JOIN production_plans p ON p.snapshot_id = l.snapshot_id
		WHERE p.id = $1 AND ($2 = '' OR l.meal_id::text = $2)`, planID, mealID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// Cancel withdraws a plan that has not been released.
func (s *Service) Cancel(ctx context.Context, actor, id, why string) (*Plan, error) {
	why, err := reason(why, 3)
	if err != nil {
		return nil, err
	}
	err = s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.lockPlan(ctx, tx, id, "draft", "approved"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE production_batches SET status = 'cancelled', cancelled_at = now(), cancel_reason = $2, updated_at = now()
			WHERE plan_id = $1 AND status = 'planned'`, id, why); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE production_plans SET status = 'cancelled', cancelled_at = now(), cancel_reason = $2, updated_at = now() WHERE id = $1`, id, why); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "production.plan_cancelled", TargetType: "production_plan", TargetID: id, Reason: why})
	})
	if err != nil {
		return nil, err
	}
	return s.Plan(ctx, id)
}

// RequirementRows is the flat requirement list for a plan or for every live
// plan whose production date is in a range (the procurement input).
type RequirementRow struct {
	PlanID         string  `json:"plan_id"`
	PlanNumber     string  `json:"plan_number"`
	PlanStatus     string  `json:"plan_status"`
	ProductionDate string  `json:"production_date"`
	IngredientID   string  `json:"ingredient_id"`
	IngredientName string  `json:"ingredient_name"`
	Preparation    string  `json:"preparation"`
	Quantity       *string `json:"quantity"`
	Unit           string  `json:"unit"`
	Status         string  `json:"status"`
	Approximate    bool    `json:"approximate"`
	SourceBatches  string  `json:"source_batches"`
	SourceMeals    string  `json:"source_meals"`
}

func (s *Service) RequirementRows(ctx context.Context, planID, from, to string) ([]RequirementRow, error) {
	var pid *string
	if uuidPattern.MatchString(planID) {
		pid = &planID
	}
	f, okf := parseDay(from)
	t, okt := parseDay(to)
	if !okf {
		f = s.today().AddDate(0, 0, -7)
	}
	if !okt {
		t = f.AddDate(0, 0, 21)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT p.id, p.number, p.status, p.production_date::text, r.ingredient_id, i.name, r.preparation, trim_scale(r.quantity)::text, r.unit, r.status, r.approximate,
		       COALESCE((SELECT string_agg(DISTINCT b.code, ' ') FROM ingredient_requirement_sources x JOIN production_batches b ON b.plan_item_id = x.plan_item_id WHERE x.requirement_id = r.id), ''),
		       COALESCE((SELECT string_agg(DISTINCT it.meal_name, '; ') FROM ingredient_requirement_sources x JOIN production_plan_items it ON it.id = x.plan_item_id WHERE x.requirement_id = r.id), '')
		FROM ingredient_requirements r JOIN production_plans p ON p.id = r.plan_id JOIN ingredients i ON i.id = r.ingredient_id
		WHERE p.status <> 'cancelled' AND ($1::uuid IS NOT NULL AND p.id = $1::uuid OR $1::uuid IS NULL AND p.production_date BETWEEN $2 AND $3)
		ORDER BY p.production_date, i.name, r.preparation LIMIT 5000`, pid, f, t)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[RequirementRow])
}
