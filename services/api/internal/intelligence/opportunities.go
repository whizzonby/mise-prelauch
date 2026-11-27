package intelligence

import (
	"context"
	"fmt"
	"time"

	"mise.tt/api/internal/identity"
)

// Bases of an opportunity or recommendation.
const (
	BasisObserved  = "observed"
	BasisAI        = "ai_hypothesis"
	BasisProposal  = "business_proposal"
	BasisValidated = "validated"
)

// Opportunity is an evidence-backed observation worth reviewing. It is never
// an outcome: it says what the records show and what a person could review.
type Opportunity struct {
	Key         string            `json:"key"`
	Title       string            `json:"title"`
	Domain      string            `json:"domain"`
	Basis       string            `json:"basis"`
	Summary     string            `json:"summary"`
	Evidence    map[string]string `json:"evidence"`
	Review      string            `json:"review"`
	ReviewHref  string            `json:"review_href"`
	Caution     string            `json:"caution"`
	From        string            `json:"from"`
	To          string            `json:"to"`
	GeneratedAt time.Time         `json:"generated_at"`
}

const caution = "An observation from records, not a forecast or a guaranteed outcome. Any change goes through the normal workflow and its approvals."

// Opportunities lists the observed opportunities the caller may see.
func (b *Toolbox) Opportunities(ctx context.Context, p *identity.Principal, from, to string) ([]Opportunity, error) {
	out := []Opportunity{}
	add := func(o Opportunity) {
		o.Basis, o.Caution, o.GeneratedAt = BasisObserved, caution, b.Now()
		out = append(out, o)
	}
	ops := p.Can(identity.PermIntelligenceOperationalRead)
	if ops && p.Can(identity.PermAnalyticsProductionRead) {
		mi, err := b.Meals(ctx, from, to)
		if err != nil {
			return nil, err
		}
		for _, m := range mi.Meals {
			if m.Demo {
				continue
			}
			ev := map[string]string{"meal": m.Name, "weeks_on_menu": fmt.Sprint(m.WeeksOnMenu), "weeks_in_period": fmt.Sprint(mi.WeeksInPeriod),
				"kits_ordered": fmt.Sprint(m.KitsOrdered), "rule_source": "meal intelligence " + mi.From + " to " + mi.To}
			if m.KitsPerWeekOn != nil {
				ev["kits_per_week_on_menu"] = fmt.Sprintf("%.2f", *m.KitsPerWeekOn)
			}
			for _, l := range m.Labels {
				switch l {
				case LabelConstrained:
					add(Opportunity{Key: "meal_availability:" + m.MealID, Title: "Offer " + m.Name + " more often", Domain: DomainOperational,
						Summary:  fmt.Sprintf("%s sells %s kits per week when on the menu (high demand) but was on %d of %d menus.", m.Name, ev["kits_per_week_on_menu"], m.WeeksOnMenu, mi.WeeksInPeriod),
						Evidence: ev, Review: "Menu planning: consider scheduling it more often, within production and supply limits.", ReviewHref: "/menus", From: mi.From, To: mi.To})
				case LabelUnder:
					add(Opportunity{Key: "meal_review:" + m.MealID, Title: "Review " + m.Name, Domain: DomainOperational,
						Summary:  fmt.Sprintf("%s was on %d menus and sold %s kits per week on menu, half the median or less.", m.Name, m.WeeksOnMenu, ev["kits_per_week_on_menu"]),
						Evidence: ev, Review: "Culinary and menu review: description, photography, recipe or rotation.", ReviewHref: "/catalog/meals", From: mi.From, To: mi.To})
				case LabelHighCost:
					ev["cost_per_serving_minor"] = fmt.Sprint(*m.CostPerServing)
					add(Opportunity{Key: "meal_cost:" + m.MealID, Title: "Review the cost of " + m.Name, Domain: DomainOperational,
						Summary:  fmt.Sprintf("%s costs %s per serving by reference costs, 1.5 × the median or more.", m.Name, money(*m.CostPerServing)),
						Evidence: ev, Review: "Culinary costing review: ingredient choices and supplier products, through recipe versioning.", ReviewHref: "/culinary/recipes", From: mi.From, To: mi.To})
				}
			}
		}
	}
	if ops && p.Can(identity.PermSupplierProductsRead) {
		rows, err := b.Pool.Query(ctx, `WITH ranked AS (
			SELECT sp.product_id, sp.amount_minor, sp.valid_from, row_number() OVER (PARTITION BY sp.product_id ORDER BY sp.valid_from DESC, sp.created_at DESC) AS n
			FROM supplier_prices sp WHERE sp.currency = 'TTD')
			SELECT p.id, p.name, s.name, cur.amount_minor, prev.amount_minor, cur.valid_from::text
			FROM ranked cur JOIN ranked prev ON prev.product_id = cur.product_id AND prev.n = 2
			JOIN supplier_products p ON p.id = cur.product_id AND p.active JOIN suppliers s ON s.id = p.supplier_id
			WHERE cur.n = 1 AND prev.amount_minor > 0 AND cur.amount_minor * 100 >= prev.amount_minor * 110 AND cur.valid_from >= $1::date - 90
			ORDER BY (cur.amount_minor::numeric / prev.amount_minor) DESC LIMIT 20`, b.Now().In(ttLocation()).Format(day))
		if err != nil {
			return nil, err
		}
		type pr struct {
			id, name, supplier, since string
			cur, prev                 int64
		}
		var list []pr
		for rows.Next() {
			var x pr
			if err := rows.Scan(&x.id, &x.name, &x.supplier, &x.cur, &x.prev, &x.since); err != nil {
				rows.Close()
				return nil, err
			}
			list = append(list, x)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		for _, x := range list {
			add(Opportunity{Key: "supplier_price:" + x.id, Title: "Review the price of " + x.name, Domain: DomainOperational,
				Summary:  fmt.Sprintf("%s from %s went from %s to %s (from %s), an increase of 10 %% or more.", x.name, x.supplier, money(x.prev), money(x.cur), x.since),
				Evidence: map[string]string{"product": x.name, "supplier": x.supplier, "previous_price_minor": fmt.Sprint(x.prev), "current_price_minor": fmt.Sprint(x.cur), "since": x.since, "source": "supplier_prices"},
				Review:   "Procurement review: compare quotations from approved suppliers. Substitutions go through recipe approval.", ReviewHref: "/procurement/suppliers"})
		}
	}
	if p.Can(identity.PermIntelligenceCustomerRead) && b.Behavior != nil {
		segs, err := b.Behavior.Segments(ctx)
		if err != nil {
			return nil, err
		}
		for _, s := range segs {
			if s.Count == 0 {
				continue
			}
			ev := map[string]string{"segment": s.Name, "customers": fmt.Sprint(s.Count), "rule": s.Definition}
			switch s.Key {
			case "renewal_failures":
				add(Opportunity{Key: "retention:renewal_failures", Title: "Follow up failed renewals", Domain: DomainCustomer,
					Summary: fmt.Sprintf("%d customers had a renewal charge fail in the last 60 days.", s.Count), Evidence: ev,
					Review: "Retention review: payment-method reminders through the existing customer emails.", ReviewHref: "/customers"})
			case "inactive":
				add(Opportunity{Key: "retention:inactive", Title: "Review inactive customers", Domain: DomainCustomer,
					Summary: fmt.Sprintf("%d customers paid before but have no live subscription and no paid week in the last 4 weeks.", s.Count), Evidence: ev,
					Review: "Retention review: any win-back campaign needs marketing approval and consent.", ReviewHref: "/customers"})
			case "delivery_issues":
				add(Opportunity{Key: "retention:delivery_issues", Title: "Contact customers with repeated delivery issues", Domain: DomainCustomer,
					Summary: fmt.Sprintf("%d customers had two or more failed or returned deliveries in 90 days.", s.Count), Evidence: ev,
					Review: "Support review of their delivery history.", ReviewHref: "/support"})
			}
		}
	}
	return out, nil
}
