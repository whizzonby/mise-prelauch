package production

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
)

// DemandMeal is one meal's confirmed demand for a delivery week.
type DemandMeal struct {
	MealID            string         `json:"meal_id"`
	MealName          string         `json:"meal_name"`
	Kits              int            `json:"kits"`
	Servings          int            `json:"servings_per_kit"`
	Orders            int            `json:"orders"`
	OrderNumbers      []string       `json:"order_numbers"`
	RecipeVersionID   *string        `json:"recipe_version_id"`
	RecipeName        *string        `json:"recipe_name"`
	RecipeVersion     *int           `json:"recipe_version"`
	DietaryTags       []string       `json:"dietary_tags"`
	CustomerAllergies map[string]int `json:"customer_allergies"`
	// Plain-language warnings: no recipe, mixed servings.
	Notes []string `json:"notes"`
}

// Demand is a delivery week's confirmed demand: live from orders, or as
// frozen in a snapshot.
type Demand struct {
	Week     string    `json:"week"`
	CutoffAt time.Time `json:"cutoff_at"`
	// Final: the selection cutoff has passed, so the week's orders exist.
	Final    bool         `json:"final"`
	Kits     int          `json:"kits"`
	Orders   int          `json:"orders"`
	Meals    []DemandMeal `json:"meals"`
	Snapshot *Snapshot    `json:"snapshot"`
	// Changed: live orders differ from the current snapshot.
	Changed    bool   `json:"changed"`
	Definition string `json:"definition"`
}

const demandDefinition = "Kits in orders for the week that are not pending or cancelled. An order exists only for a paid (or promotion-free) week with confirmed meals, made at the selection cutoff; carts, draft selections and unpaid weeks are never counted."

type Snapshot struct {
	ID        string    `json:"id"`
	Week      string    `json:"week"`
	Revision  int       `json:"revision"`
	Status    string    `json:"status"`
	Kits      int       `json:"kits"`
	Orders    int       `json:"orders"`
	CutoffAt  time.Time `json:"cutoff_at"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by_name"`
	Hash      string    `json:"content_hash"`
}

type sourceLine struct {
	orderID, orderNumber, mealID, mealName string
	mealVersion, quantity, servings        int
	dietary, allergens                     []string
	recipeVersion                          *string
}

// live reads the week's orders and resolves each meal's recipe version.
func (s *Service) live(ctx context.Context, q db.Querier, week time.Time) ([]sourceLine, error) {
	src, err := s.Orders.DemandLines(ctx, q, week)
	if err != nil {
		return nil, err
	}
	recipes := map[string]*string{}
	out := make([]sourceLine, 0, len(src))
	for _, l := range src {
		rv, seen := recipes[l.MealID]
		if !seen {
			id, ok, err := culinary.RecipeVersionForMeal(ctx, q, l.MealID, week)
			if err != nil {
				return nil, err
			}
			if ok {
				rv = &id
			}
			recipes[l.MealID] = rv
		}
		out = append(out, sourceLine{orderID: l.OrderID, orderNumber: l.OrderNumber, mealID: l.MealID, mealName: l.MealName,
			mealVersion: l.MealVersion, quantity: l.Quantity, servings: l.Servings, dietary: l.DietaryTags, allergens: l.CustomerAllergens, recipeVersion: rv})
	}
	return out, nil
}

func hashLines(lines []sourceLine) string {
	keys := make([]string, len(lines))
	for i, l := range lines {
		rv := ""
		if l.recipeVersion != nil {
			rv = *l.recipeVersion
		}
		keys[i] = fmt.Sprintf("%s|%s|%d|%d|%s", l.orderID, l.mealID, l.quantity, l.servings, rv)
	}
	sort.Strings(keys)
	sum := sha256.Sum256([]byte(strings.Join(keys, "\n")))
	return hex.EncodeToString(sum[:])
}

func (s *Service) aggregate(ctx context.Context, q db.Querier, lines []sourceLine) ([]DemandMeal, int, int, error) {
	byMeal := map[string]*DemandMeal{}
	var order []string
	orders := map[string]bool{}
	kits := 0
	servingsSeen := map[string]map[int]bool{}
	for _, l := range lines {
		m := byMeal[l.mealID]
		if m == nil {
			m = &DemandMeal{MealID: l.mealID, MealName: l.mealName, Servings: l.servings, RecipeVersionID: l.recipeVersion,
				CustomerAllergies: map[string]int{}, DietaryTags: l.dietary, OrderNumbers: []string{}, Notes: []string{}}
			byMeal[l.mealID] = m
			order = append(order, l.mealID)
			servingsSeen[l.mealID] = map[int]bool{}
		}
		m.Kits += l.quantity
		m.Orders++
		m.OrderNumbers = append(m.OrderNumbers, l.orderNumber)
		servingsSeen[l.mealID][l.servings] = true
		for _, a := range l.allergens {
			m.CustomerAllergies[a]++
		}
		orders[l.orderNumber] = true
		kits += l.quantity
	}
	out := []DemandMeal{}
	for _, id := range order {
		m := byMeal[id]
		if len(servingsSeen[id]) > 1 {
			m.Notes = append(m.Notes, "Orders for this meal were placed at different serving sizes; check the recipe yield before planning.")
		}
		if m.RecipeVersionID == nil {
			m.Notes = append(m.Notes, "No published recipe covers this week: it cannot be planned until one is approved and published.")
		} else {
			var name string
			var n int
			if err := q.QueryRow(ctx, `SELECT r.name, v.number FROM recipe_versions v JOIN recipes r ON r.id = v.recipe_id WHERE v.id = $1`, *m.RecipeVersionID).Scan(&name, &n); err != nil {
				return nil, 0, 0, err
			}
			m.RecipeName, m.RecipeVersion = &name, &n
		}
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MealName < out[j].MealName })
	return out, kits, len(orders), nil
}

// Demand is the live confirmed demand for a week, compared with the
// current snapshot.
func (s *Service) Demand(ctx context.Context, weekStr string) (*Demand, error) {
	week, ok := parseWeek(weekStr)
	if !ok {
		week = s.Weeks.NextOpenWeek(s.Now()).AddDate(0, 0, -7)
	}
	lines, err := s.live(ctx, s.pool, week)
	if err != nil {
		return nil, err
	}
	d := &Demand{Week: week.Format(day), CutoffAt: s.Weeks.CutoffFor(week), Definition: demandDefinition}
	d.Final = !s.Now().Before(d.CutoffAt)
	if d.Meals, d.Kits, d.Orders, err = s.aggregate(ctx, s.pool, lines); err != nil {
		return nil, err
	}
	snap, err := s.currentSnapshot(ctx, s.pool, week)
	if err != nil {
		return nil, err
	}
	d.Snapshot = snap
	d.Changed = snap != nil && snap.Hash != hashLines(lines)
	return d, nil
}

func (s *Service) currentSnapshot(ctx context.Context, q db.Querier, week time.Time) (*Snapshot, error) {
	snap, err := scanSnapshot(q.QueryRow(ctx, snapshotSelect+` WHERE s.week_start = $1 AND s.status = 'current'`, week))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return snap, err
}

const snapshotSelect = `SELECT s.id, s.week_start::text, s.revision, s.status, s.kits, s.orders, s.cutoff_at, s.created_at,
	COALESCE(btrim(u.first_name || ' ' || u.last_name), ''), s.content_hash
	FROM production_demand_snapshots s LEFT JOIN users u ON u.id = s.created_by`

func scanSnapshot(r pgx.Row) (*Snapshot, error) {
	var x Snapshot
	err := r.Scan(&x.ID, &x.Week, &x.Revision, &x.Status, &x.Kits, &x.Orders, &x.CutoffAt, &x.CreatedAt, &x.CreatedBy, &x.Hash)
	if err != nil {
		return nil, err
	}
	return &x, nil
}

// TakeSnapshot freezes the week's confirmed demand. Only after the
// selection cutoff. If nothing changed since the current snapshot, that
// snapshot is returned and nothing is written (created=false), so a retry,
// a double click or a second manager never duplicates one.
func (s *Service) TakeSnapshot(ctx context.Context, actor, weekStr string) (*Snapshot, bool, error) {
	week, ok := parseWeek(weekStr)
	if !ok {
		return nil, false, fieldErr("week", "Choose a delivery week (a Monday).")
	}
	var snap *Snapshot
	created := false
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var err error
		snap, created, err = s.snapshot(ctx, tx, actor, week)
		return err
	})
	return snap, created, err
}

func (s *Service) snapshot(ctx context.Context, tx pgx.Tx, actor string, week time.Time) (*Snapshot, bool, error) {
	cutoff := s.Weeks.CutoffFor(week)
	if s.Now().Before(cutoff) {
		return nil, false, conflict("selection_open", "Meal selection for this week is still open until "+cutoff.In(s.Location).Format("Mon 2 Jan 15:04")+". Demand is final only after the cutoff.")
	}
	if err := lock(ctx, tx, "production_snapshot:"+week.Format(day)); err != nil {
		return nil, false, err
	}
	lines, err := s.live(ctx, tx, week)
	if err != nil {
		return nil, false, err
	}
	hash := hashLines(lines)
	cur, err := s.currentSnapshot(ctx, tx, week)
	if err != nil {
		return nil, false, err
	}
	if cur != nil && cur.Hash == hash {
		return cur, false, nil
	}
	kits, ordersSeen := 0, map[string]bool{}
	for _, l := range lines {
		kits += l.quantity
		ordersSeen[l.orderID] = true
	}
	revision := 1
	if cur != nil {
		revision = cur.Revision + 1
		if _, err := tx.Exec(ctx, `UPDATE production_demand_snapshots SET status = 'superseded' WHERE id = $1`, cur.ID); err != nil {
			return nil, false, err
		}
	}
	var id string
	if err := tx.QueryRow(ctx, `INSERT INTO production_demand_snapshots (week_start, revision, cutoff_at, content_hash, kits, orders, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`, week, revision, cutoff, hash, kits, len(ordersSeen), nilIfEmpty(actor)).Scan(&id); err != nil {
		return nil, false, err
	}
	for _, l := range lines {
		if _, err := tx.Exec(ctx, `INSERT INTO production_demand_lines (snapshot_id, order_id, order_number, meal_id, meal_version, meal_name, quantity,
			servings, dietary_tags, customer_allergens, recipe_version_id) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			id, l.orderID, l.orderNumber, l.mealID, l.mealVersion, l.mealName, l.quantity, l.servings, l.dietary, l.allergens, l.recipeVersion); err != nil {
			return nil, false, err
		}
	}
	if err := event(ctx, tx, "production.demand_snapshotted", "demand_snapshot", id, "", map[string]any{
		"week": week.Format(day), "revision": revision, "kits": kits, "orders": len(ordersSeen)}); err != nil {
		return nil, false, err
	}
	// A plan already made from an earlier snapshot is not changed: the
	// difference is raised for a manager to handle.
	if cur != nil {
		var planID, number, status string
		err := tx.QueryRow(ctx, `SELECT id, number, status FROM production_plans WHERE week_start = $1 AND status <> 'cancelled'`, week).Scan(&planID, &number, &status)
		if err == nil {
			if err := s.raise(ctx, tx, alert("demand_changed:"+id, "production_demand_changed", "warning",
				"Demand changed after plan "+number+" was made",
				fmt.Sprintf("Revision %d of the week's demand has %d kits (the plan used revision %d). The %s plan is not changed automatically; review the difference.", revision, kits, cur.Revision, status),
				"production_plan", planID, "production.approve")); err != nil {
				return nil, false, err
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, false, err
		}
	}
	if err := audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "production.demand_snapshotted", TargetType: "demand_snapshot", TargetID: id,
		Metadata: map[string]any{"week": week.Format(day), "revision": revision, "kits": kits}}); err != nil {
		return nil, false, err
	}
	snap, err := scanSnapshot(tx.QueryRow(ctx, snapshotSelect+` WHERE s.id = $1`, id))
	return snap, true, err
}

// SnapshotMeals is a snapshot's demand per meal.
func (s *Service) snapshotMeals(ctx context.Context, q db.Querier, snapshotID string) ([]sourceLine, error) {
	rows, err := q.Query(ctx, `SELECT order_id, order_number, meal_id, meal_name, meal_version, quantity, servings, dietary_tags,
		customer_allergens, recipe_version_id FROM production_demand_lines WHERE snapshot_id = $1 ORDER BY order_number, meal_id`, snapshotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []sourceLine
	for rows.Next() {
		var l sourceLine
		var mv int32
		var qn, sv int16
		if err := rows.Scan(&l.orderID, &l.orderNumber, &l.mealID, &l.mealName, &mv, &qn, &sv, &l.dietary, &l.allergens, &l.recipeVersion); err != nil {
			return nil, err
		}
		l.mealVersion, l.quantity, l.servings = int(mv), int(qn), int(sv)
		out = append(out, l)
	}
	return out, rows.Err()
}

// SnapshotDetail is a snapshot with its demand per meal.
type SnapshotDetail struct {
	Snapshot
	Meals []DemandMeal `json:"meals"`
}

func (s *Service) SnapshotDetail(ctx context.Context, id string) (*SnapshotDetail, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	snap, err := scanSnapshot(s.pool.QueryRow(ctx, snapshotSelect+` WHERE s.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	lines, err := s.snapshotMeals(ctx, s.pool, id)
	if err != nil {
		return nil, err
	}
	meals, _, _, err := s.aggregate(ctx, s.pool, lines)
	if err != nil {
		return nil, err
	}
	return &SnapshotDetail{Snapshot: *snap, Meals: meals}, nil
}
