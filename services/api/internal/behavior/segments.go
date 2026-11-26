package behavior

import (
	"context"
	"strings"
	"time"

	"mise.tt/api/internal/platform/httpx"
)

// Segment is a transparent, rule-based group of customers. Rules use only
// ordering, payment, delivery and selection records: no sensitive
// attributes, no scores, no predictions.
type Segment struct {
	Key        string    `json:"key"`
	Name       string    `json:"name"`
	Definition string    `json:"definition"`
	Sources    []string  `json:"sources"`
	Count      int64     `json:"count"`
	AsOf       time.Time `json:"as_of"`
}

// A paid week is a live order (made and not cancelled), as the analytics registry defines orders.
const liveOrders = `SELECT o.customer_id, o.week_start FROM orders o WHERE o.status NOT IN ('pending', 'cancelled')`

type segmentDef struct {
	key, name, definition string
	sources               []string
	members               string // SQL returning customer ids; $1 = now
}

var segments = []segmentDef{
	{"new", "New customers", "First paid week within the last 4 weeks.", []string{"orders"},
		`SELECT customer_id FROM (` + liveOrders + `) o GROUP BY customer_id HAVING min(week_start) >= ($1::timestamptz - interval '28 days')::date`},
	{"repeat", "Repeat customers", "Two or more paid weeks.", []string{"orders"},
		`SELECT customer_id FROM (` + liveOrders + `) o GROUP BY customer_id HAVING count(DISTINCT week_start) >= 2`},
	{"long_term", "Long-term subscribers", "Twelve or more paid weeks.", []string{"orders"},
		`SELECT customer_id FROM (` + liveOrders + `) o GROUP BY customer_id HAVING count(DISTINCT week_start) >= 12`},
	{"inactive", "Inactive customers", "At least one paid week, none in the last 4 weeks, and no live subscription.", []string{"orders", "subscriptions"},
		`SELECT customer_id FROM (` + liveOrders + `) o GROUP BY customer_id
		 HAVING max(week_start) < ($1::timestamptz - interval '28 days')::date
		 AND NOT EXISTS (SELECT 1 FROM subscriptions s WHERE s.customer_id = o.customer_id
			AND s.status IN ('active', 'pause_scheduled', 'paused', 'past_due', 'cancel_scheduled'))`},
	{"renewal_failures", "Customers with renewal failures", "A renewal charge failed in the last 60 days.", []string{"payments"},
		`SELECT DISTINCT customer_id FROM payments WHERE kind = 'off_session' AND status = 'failed' AND updated_at >= $1::timestamptz - interval '60 days'`},
	{"delivery_issues", "Customers with repeated delivery issues", "Two or more deliveries failed or returned in the last 90 days.", []string{"deliveries"},
		`SELECT customer_id FROM deliveries WHERE status IN ('delivery_failed', 'returned') AND service_date >= ($1::timestamptz - interval '90 days')::date
		 GROUP BY customer_id HAVING count(*) >= 2`},
}

// args passes the clock only to rules that use it ($1).
func (d segmentDef) args(now time.Time) []any {
	if strings.Contains(d.members, "$1") {
		return []any{now}
	}
	return nil
}

// categoryPreference: at least 3 kits chosen, half or more in one category.
const categoryPreference = `SELECT x.customer_id, c.slug, c.name FROM (
	SELECT o.customer_id, m.category_id, sum(i.quantity) AS kits, sum(sum(i.quantity)) OVER (PARTITION BY o.customer_id) AS total
	FROM order_items i JOIN orders o ON o.id = i.order_id JOIN meals m ON m.id = i.meal_id
	WHERE o.status NOT IN ('pending', 'cancelled') AND m.category_id IS NOT NULL GROUP BY o.customer_id, m.category_id) x
	JOIN meal_categories c ON c.id = x.category_id WHERE x.total >= 3 AND x.kits * 2 >= x.total`

func (s *Service) Segments(ctx context.Context) ([]Segment, error) {
	now := s.Now()
	out := []Segment{}
	for _, d := range segments {
		var n int64
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM (`+d.members+`) m`, d.args(now)...).Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, Segment{Key: d.key, Name: d.name, Definition: d.definition, Sources: d.sources, Count: n, AsOf: now})
	}
	rows, err := s.pool.Query(ctx, `SELECT slug, name, count(*) FROM (`+categoryPreference+`) p GROUP BY slug, name ORDER BY 3 DESC, 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var slug, name string
		var n int64
		if err := rows.Scan(&slug, &name, &n); err != nil {
			return nil, err
		}
		out = append(out, Segment{Key: "prefers_" + slug, Name: "Prefer " + name, Definition: "At least 3 kits chosen, half or more of them " + name + ".",
			Sources: []string{"order_items", "meals"}, Count: n, AsOf: now})
	}
	return out, rows.Err()
}

// Member is a customer in a segment (shown only to staff who may read customers).
type Member struct {
	CustomerID string `json:"customer_id"`
	Name       string `json:"name"`
	Email      string `json:"email"`
}

func (s *Service) Members(ctx context.Context, key string) ([]Member, error) {
	var sql string
	var args []any
	for _, d := range segments {
		if d.key == key {
			sql, args = d.members, d.args(s.Now())
		}
	}
	if sql == "" && len(key) > 8 && key[:8] == "prefers_" {
		sql, args = `SELECT customer_id FROM (`+categoryPreference+`) p WHERE slug = $1`, []any{key[8:]}
	}
	if sql == "" {
		return nil, httpx.ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `SELECT c.id, trim(u.first_name || ' ' || u.last_name), u.email FROM (`+sql+`) m
		JOIN customers c ON c.id = m.customer_id JOIN users u ON u.id = c.user_id ORDER BY 2 LIMIT 500`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.CustomerID, &m.Name, &m.Email); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
