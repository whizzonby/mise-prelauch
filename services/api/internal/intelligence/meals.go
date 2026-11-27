package intelligence

import (
	"context"
	"fmt"
	"math"
	"sort"

	"mise.tt/api/internal/analytics"
)

// MealRow is one meal's performance over a period.
type MealRow struct {
	MealID          string   `json:"meal_id"`
	Name            string   `json:"name"`
	Slug            string   `json:"slug"`
	Category        string   `json:"category"`
	Demo            bool     `json:"demo"`
	WeeksOnMenu     int64    `json:"weeks_on_menu"`
	KitsOrdered     int64    `json:"kits_ordered"`
	KitsPerWeekOn   *float64 `json:"kits_per_week_on_menu"` // availability-adjusted demand
	Customers       int64    `json:"customers"`
	RepeatCustomers int64    `json:"repeat_customers"`
	RepeatShare     *float64 `json:"repeat_share"`
	WasteRecords    int64    `json:"waste_records"`
	CostPerServing  *int64   `json:"cost_per_serving_minor"`
	CostComplete    bool     `json:"cost_complete"`
	CostNote        string   `json:"cost_note"`
	PrepMinutes     int      `json:"prep_minutes"`
	Labels          []string `json:"labels"`
}

// MealIntelligence is meal performance with availability taken into account.
type MealIntelligence struct {
	From          string    `json:"from"`
	To            string    `json:"to"`
	WeeksInPeriod int64     `json:"weeks_in_period"`
	Meals         []MealRow `json:"meals"`
	Rules         []string  `json:"rules"`
	Limitations   []string  `json:"limitations"`
}

// Labels.
const (
	LabelHighDemand  = "high_demand"
	LabelConstrained = "availability_constrained"
	LabelUnder       = "underperforming"
	LabelHighCost    = "high_cost"
)

var mealRules = []string{
	"Demand is kits ordered per week the meal was on a published menu, so a meal offered less often is not read as less popular.",
	"High demand: at least 1.5 × the median kits per week on menu (meals on 2 or more menus).",
	"Availability constrained: high demand, but on fewer than half of the period's published menus.",
	"Underperforming: on 4 or more menus and at most 0.5 × the median kits per week on menu.",
	"High cost: complete recipe costing per serving at least 1.5 × the median of complete costings.",
	"Engineering defaults; no rating, preference score or prediction is used.",
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64{}, xs...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// Meals computes meal intelligence for a period (default: the last 12 weeks).
func (b *Toolbox) Meals(ctx context.Context, from, to string) (*MealIntelligence, error) {
	if from == "" && to == "" {
		today := b.Now().In(ttLocation())
		from, to = today.AddDate(0, 0, -83).Format(day), today.Format(day)
	}
	r, err := b.Analytics.ParseRange(from, to)
	if err != nil {
		return nil, err
	}
	out := &MealIntelligence{From: r.From.Format(day), To: r.To.Format(day), Meals: []MealRow{}, Rules: mealRules, Limitations: []string{
		"No meal ratings are recorded, so ratings are not used.",
		"Meals are not priced individually (plans are), so a per-meal contribution margin is not calculated.",
		"Recipe costing is an estimate from reference costs, from the culinary workflow; incomplete costings are not compared.",
	}}
	if err := b.Pool.QueryRow(ctx, `SELECT count(DISTINCT week_start) FROM weekly_menus WHERE published_at IS NOT NULL AND week_start BETWEEN $1 AND $2`,
		r.From, r.To).Scan(&out.WeeksInPeriod); err != nil {
		return nil, err
	}
	rows, err := b.Pool.Query(ctx, `
		WITH ord AS (
		  SELECT i.meal_id, o.customer_id, o.week_start, i.quantity FROM order_items i JOIN orders o ON o.id = i.order_id
		  WHERE o.status NOT IN ('pending', 'cancelled') AND o.week_start BETWEEN $1 AND $2),
		per_cust AS (SELECT meal_id, customer_id, count(DISTINCT week_start) AS weeks FROM ord GROUP BY meal_id, customer_id)
		SELECT m.id, m.name, m.slug, coalesce(c.name, ''), m.is_demo, coalesce(m.prep_minutes, 0),
		  (SELECT count(DISTINCT wk.week_start) FROM weekly_menu_meals wm JOIN weekly_menus wk ON wk.id = wm.menu_id
		     WHERE wm.meal_id = m.id AND wk.published_at IS NOT NULL AND wk.week_start BETWEEN $1 AND $2),
		  coalesce((SELECT sum(quantity) FROM ord WHERE ord.meal_id = m.id), 0),
		  (SELECT count(*) FROM per_cust pc WHERE pc.meal_id = m.id),
		  (SELECT count(*) FROM per_cust pc WHERE pc.meal_id = m.id AND pc.weeks >= 2),
		  (SELECT count(*) FROM production_waste w JOIN production_batches pb ON pb.id = w.batch_id
		     WHERE pb.meal_id = m.id AND pb.production_date BETWEEN $1 AND $2),
		  (SELECT r.id::text FROM recipes r WHERE r.meal_id = m.id ORDER BY r.created_at LIMIT 1)
		FROM meals m LEFT JOIN meal_categories c ON c.id = m.category_id
		WHERE m.status = 'published' OR EXISTS (SELECT 1 FROM ord WHERE ord.meal_id = m.id)
		ORDER BY m.name LIMIT 300`, r.From, r.To)
	if err != nil {
		return nil, err
	}
	type rec struct {
		row    MealRow
		recipe *string
	}
	var recs []rec
	for rows.Next() {
		var x rec
		if err := rows.Scan(&x.row.MealID, &x.row.Name, &x.row.Slug, &x.row.Category, &x.row.Demo, &x.row.PrepMinutes, &x.row.WeeksOnMenu,
			&x.row.KitsOrdered, &x.row.Customers, &x.row.RepeatCustomers, &x.row.WasteRecords, &x.recipe); err != nil {
			rows.Close()
			return nil, err
		}
		recs = append(recs, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var demand, costs []float64
	for i := range recs {
		m := &recs[i].row
		m.Labels = []string{}
		if m.WeeksOnMenu > 0 {
			v := math.Round(float64(m.KitsOrdered)/float64(m.WeeksOnMenu)*100) / 100
			m.KitsPerWeekOn = &v
			if m.WeeksOnMenu >= 2 {
				demand = append(demand, v)
			}
		}
		if m.Customers > 0 {
			v := math.Round(float64(m.RepeatCustomers)/float64(m.Customers)*10000) / 10000
			m.RepeatShare = &v
		}
		m.CostNote = "No recipe is linked to this meal."
		if recs[i].recipe != nil && b.Culinary != nil {
			c, err := b.Culinary.RecipeCosting(ctx, *recs[i].recipe, 0)
			switch {
			case err != nil:
				m.CostNote = "The recipe has no version to cost."
			case c.YieldServings <= 0:
				m.CostNote = "The recipe has no yield."
			default:
				per := c.PerServingMinor
				m.CostPerServing, m.CostComplete = &per, c.Complete
				m.CostNote = fmt.Sprintf("Recipe version %d, reference costs.", c.Version)
				if !c.Complete {
					m.CostNote += " Incomplete: some ingredients or packaging have no reference cost."
				} else {
					costs = append(costs, float64(per))
				}
			}
		}
	}
	md, mc := median(demand), median(costs)
	for i := range recs {
		m := &recs[i].row
		if m.KitsPerWeekOn != nil && m.WeeksOnMenu >= 2 && md > 0 {
			if *m.KitsPerWeekOn >= 1.5*md {
				m.Labels = append(m.Labels, LabelHighDemand)
				if out.WeeksInPeriod > 0 && m.WeeksOnMenu*2 < out.WeeksInPeriod {
					m.Labels = append(m.Labels, LabelConstrained)
				}
			}
			if m.WeeksOnMenu >= 4 && *m.KitsPerWeekOn <= 0.5*md {
				m.Labels = append(m.Labels, LabelUnder)
			}
		}
		if m.CostComplete && m.CostPerServing != nil && mc > 0 && float64(*m.CostPerServing) >= 1.5*mc {
			m.Labels = append(m.Labels, LabelHighCost)
		}
		out.Meals = append(out.Meals, *m)
	}
	sort.SliceStable(out.Meals, func(i, j int) bool { return out.Meals[i].KitsOrdered > out.Meals[j].KitsOrdered })
	if len(demand) < 3 {
		out.Limitations = append(out.Limitations, "Fewer than 3 meals were on 2 or more menus: demand labels are not assigned.")
	}
	for _, m := range out.Meals {
		if m.Demo && m.KitsOrdered > 0 {
			out.Limitations = append(out.Limitations, "Some orders are for demo meals (labelled); demo content is not a real customer preference.")
			break
		}
	}
	return out, nil
}

// mealsAsTable is the meal intelligence as a tool table.
func mealsAsTable(mi *MealIntelligence) analytics.Table {
	t := analytics.Table{Title: "Meals", Note: "Kits per week on menu is availability-adjusted demand.",
		Columns: []string{"Meal", "Category", "Weeks on menu", "Kits ordered", "Kits per week on menu", "Repeat share", "Cost per serving (minor)", "Labels"}}
	for _, m := range mi.Meals {
		kpw, rep, cost := "n/a", "n/a", "n/a"
		if m.KitsPerWeekOn != nil {
			kpw = fmt.Sprintf("%.2f", *m.KitsPerWeekOn)
		}
		if m.RepeatShare != nil {
			rep = fmt.Sprintf("%.4f", *m.RepeatShare)
		}
		if m.CostPerServing != nil && m.CostComplete {
			cost = fmt.Sprint(*m.CostPerServing)
		}
		labels := ""
		for i, l := range m.Labels {
			if i > 0 {
				labels += ", "
			}
			labels += l
		}
		name := m.Name
		if m.Demo {
			name += " (demo)"
		}
		t.Rows = append(t.Rows, []string{name, m.Category, fmt.Sprint(m.WeeksOnMenu), fmt.Sprint(m.KitsOrdered), kpw, rep, cost, labels})
	}
	return t
}
