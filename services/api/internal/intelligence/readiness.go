package intelligence

import (
	"context"
	"fmt"
	"time"

	"mise.tt/api/internal/finance"
)

// Readiness statuses.
const (
	Ready    = "ready"
	Limited  = "limited"
	NotReady = "not_ready"
)

// Check is one readiness check.
type Check struct {
	Key    string   `json:"key"`
	Label  string   `json:"label"`
	Status string   `json:"status"`
	Detail string   `json:"detail"`
	Value  *float64 `json:"value"`
	Unit   string   `json:"unit"`
}

// Readiness says which data Mise Intelligence can rely on.
type Readiness struct {
	Overall     string    `json:"overall"`
	Checks      []Check   `json:"checks"`
	AI          AIStatus  `json:"ai"`
	GeneratedAt time.Time `json:"generated_at"`
}

// Readiness runs every check. The overall status is the worst check
// (AI availability excluded: the deterministic views work without it).
func (b *Toolbox) Readiness(ctx context.Context) (*Readiness, error) {
	out := &Readiness{Checks: []Check{}, AI: b.aiStatus(ctx), GeneratedAt: b.Now()}
	add := func(c Check) { out.Checks = append(out.Checks, c) }

	// Real vs test payments (all time).
	var real, test int64
	if err := b.Pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE provider <> 'testpay'), count(*) FILTER (WHERE provider = 'testpay')
		FROM payments WHERE status = 'succeeded'`).Scan(&real, &test); err != nil {
		return nil, err
	}
	c := Check{Key: "payments", Label: "Real payments", Value: f64(real), Unit: "count", Status: Ready,
		Detail: fmt.Sprintf("%d real and %d test-provider payments have succeeded. Test payments never count as revenue.", real, test)}
	if real == 0 {
		c.Status, c.Detail = NotReady, fmt.Sprintf("No real payments yet: all %d succeeded payments are from the test provider, so there is no real revenue to analyse.", test)
	} else if test > 0 {
		c.Status = Limited
	}
	add(c)

	// Expense coverage for the last full month.
	now := b.Now().In(b.Finance.Loc)
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0)
	last := first.AddDate(0, 1, -1)
	s, err := b.Finance.Summary(ctx, first.Format(day), last.Format(day))
	if err != nil {
		return nil, err
	}
	var complete, estimated, missing int64
	for _, cat := range s.Categories {
		switch cat.Coverage {
		case finance.Complete:
			complete++
		case finance.Estimated:
			estimated++
		default:
			missing++
		}
	}
	c = Check{Key: "expenses", Label: "Expense coverage (" + first.Format("January 2006") + ")", Value: f64(complete), Unit: "count", Status: Ready,
		Detail: fmt.Sprintf("%d of %d categories complete, %d estimated, %d missing or partial.", complete, len(s.Categories), estimated, missing)}
	if missing > 0 {
		c.Status, c.Detail = NotReady, c.Detail+" Profit and operating results are withheld until every category is recorded or confirmed."
	} else if estimated > 0 {
		c.Status = Limited
	}
	add(c)

	// Demand history and forecasting.
	rd, err := b.Forecasting.Readiness(ctx)
	if err != nil {
		return nil, err
	}
	c = Check{Key: "demand_history", Label: "Demand history", Value: f64(int64(rd.VerifiedWeeks)), Unit: "weeks", Status: Ready,
		Detail: fmt.Sprintf("%d weeks of verified (real, paid, non-demo) demand across %d meals.", rd.VerifiedWeeks, rd.MealsWithHistory)}
	if rd.VerifiedWeeks == 0 {
		c.Status = NotReady
	} else if !rd.Ready {
		c.Status = Limited
	}
	add(c)
	c = Check{Key: "forecasting", Label: "Demand forecasting", Status: Ready, Detail: rd.Message}
	if !rd.Ready {
		c.Status = NotReady
	}
	add(c)

	// Behaviour data.
	cov, err := b.Behavior.Coverage(ctx)
	if err != nil {
		return nil, err
	}
	c = Check{Key: "behavior", Label: "Customer-app behaviour data", Value: f64(int64(cov.DaysCovered)), Unit: "days", Status: Ready,
		Detail: fmt.Sprintf("%d events over %d days.", cov.Events, cov.DaysCovered)}
	if cov.Events == 0 {
		c.Status, c.Detail = NotReady, "No behaviour events recorded yet: funnel and abandonment figures are empty."
	} else if cov.DaysCovered < 28 {
		c.Status, c.Detail = Limited, c.Detail+" Fewer than 28 days: too short for trends."
	}
	add(c)

	// Supplier costs on stock.
	var lots, noCost int64
	if err := b.Pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE unit_cost_minor IS NULL) FROM inventory_lots`).Scan(&lots, &noCost); err != nil {
		return nil, err
	}
	c = Check{Key: "supplier_costs", Label: "Ingredient costs on stock", Value: f64(noCost), Unit: "count", Status: Ready,
		Detail: fmt.Sprintf("%d of %d inventory lots have no unit cost.", noCost, lots)}
	if lots == 0 {
		c.Status, c.Detail = NotReady, "No inventory lots recorded: ingredient cost cannot be computed."
	} else if noCost > 0 {
		c.Status = Limited
	}
	add(c)

	// Delivery costs (last 90 days).
	var delivered, withoutCost int64
	if err := b.Pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE NOT EXISTS (SELECT 1 FROM delivery_cost_records c
		WHERE c.kind IN ('actual', 'adjustment') AND c.run_id IN (SELECT st.run_id FROM dispatch_stops st WHERE st.delivery_id = d.id)))
		FROM deliveries d WHERE d.status = 'delivered' AND d.service_date >= ($1::timestamptz - interval '90 days')::date`, b.Now()).Scan(&delivered, &withoutCost); err != nil {
		return nil, err
	}
	c = Check{Key: "delivery_costs", Label: "Delivery costs (last 90 days)", Value: f64(withoutCost), Unit: "count", Status: Ready,
		Detail: fmt.Sprintf("%d of %d delivered deliveries have no recorded run cost.", withoutCost, delivered)}
	if delivered == 0 {
		c.Status, c.Detail = NotReady, "No deliveries completed in the last 90 days."
	} else if withoutCost > 0 {
		c.Status = Limited
	}
	add(c)

	// AI.
	c = Check{Key: "ai", Label: "AI assistant", Status: Ready, Detail: fmt.Sprintf("%s %s", out.AI.Provider, out.AI.Model)}
	if !out.AI.Available {
		c.Status, c.Detail = NotReady, "AI unavailable: "+out.AI.Reason+" Every number and view still works; only the written explanations are off."
	}
	add(c)

	out.Overall = Ready
	for _, ch := range out.Checks {
		if ch.Key == "ai" {
			continue
		}
		if ch.Status == NotReady {
			out.Overall = NotReady
		} else if ch.Status == Limited && out.Overall == Ready {
			out.Overall = Limited
		}
	}
	return out, nil
}

func readinessTool(ctx context.Context, b *Toolbox, _ Args) (*Result, error) {
	rd, err := b.Readiness(ctx)
	if err != nil {
		return nil, err
	}
	res := &Result{Evidence: Evidence{Freshness: rd.GeneratedAt, Sources: []string{"payments", "finance", "orders", "behavior_events", "inventory_lots", "deliveries", "forecasting"}},
		Notes: []string{"Overall readiness: " + rd.Overall}}
	for _, c := range rd.Checks {
		if c.Value != nil {
			res.fact("readiness."+c.Key, c.Label, c.Value, c.Unit, "", c.Detail)
		}
		if c.Status != Ready {
			res.Limitations = append(res.Limitations, c.Label+": "+c.Detail)
		}
	}
	return res, nil
}
