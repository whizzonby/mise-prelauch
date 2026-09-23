package admin

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/platform/db"
)

type Bucket struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

type DayCount struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

type Overview struct {
	TotalLeads          int        `json:"total_leads"`
	VerifiedLeads       int        `json:"verified_leads"`
	PendingLeads        int        `json:"pending_leads"`
	UnsubscribedLeads   int        `json:"unsubscribed_leads"`
	ReferralConversions int        `json:"referral_conversions"`
	ReferredSignups     int        `json:"referred_signups"`
	ProfilesCompleted   int        `json:"profiles_completed"`
	RangeDays           int        `json:"range_days"`
	SignupsByDay        []DayCount `json:"signups_by_day"`
	Sources             []Bucket   `json:"sources"`
	Dietary             []Bucket   `json:"dietary"`
	HouseholdSizes      []Bucket   `json:"household_sizes"`
	Locations           []Bucket   `json:"locations"`
}

// overview computes the dashboard. Blocked leads are excluded everywhere:
// they are abuse, not audience. "Verified" means on the list and reachable
// (VERIFIED, QUALIFIED or CONVERTED).
func overview(ctx context.Context, q db.Querier, days int) (*Overview, error) {
	o := &Overview{RangeDays: days}

	err := q.QueryRow(ctx, `
		SELECT count(*),
			count(*) FILTER (WHERE status IN ('VERIFIED', 'QUALIFIED', 'CONVERTED')),
			count(*) FILTER (WHERE status = 'PENDING'),
			count(*) FILTER (WHERE status = 'UNSUBSCRIBED'),
			count(*) FILTER (WHERE referred_by IS NOT NULL)
		FROM leads WHERE status <> 'BLOCKED'`,
	).Scan(&o.TotalLeads, &o.VerifiedLeads, &o.PendingLeads, &o.UnsubscribedLeads, &o.ReferredSignups)
	if err != nil {
		return nil, err
	}

	if err := q.QueryRow(ctx, `SELECT count(*) FROM referrals WHERE status = 'converted'`).Scan(&o.ReferralConversions); err != nil {
		return nil, err
	}
	if err := q.QueryRow(ctx, `
		SELECT count(*) FROM lead_preferences p JOIN leads l ON l.id = p.lead_id
		WHERE l.status <> 'BLOCKED'
		  AND (p.meals_per_week IS NOT NULL OR p.cooking_frequency IS NOT NULL OR p.metadata <> '{}')`,
	).Scan(&o.ProfilesCompleted); err != nil {
		return nil, err
	}

	// generate_series fills days with no signups, so the chart has no gaps.
	rows, err := q.Query(ctx, `
		SELECT d::date, count(l.id)
		FROM generate_series((now() AT TIME ZONE 'UTC')::date - ($1::int - 1), (now() AT TIME ZONE 'UTC')::date, interval '1 day') AS d
		LEFT JOIN leads l ON (l.created_at AT TIME ZONE 'UTC')::date = d::date AND l.status <> 'BLOCKED'
		GROUP BY d ORDER BY d`, days)
	if err != nil {
		return nil, err
	}
	o.SignupsByDay, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (DayCount, error) {
		var day time.Time
		var dc DayCount
		err := row.Scan(&day, &dc.Count)
		dc.Date = day.Format(time.DateOnly)
		return dc, err
	})
	if err != nil {
		return nil, err
	}

	if o.Sources, err = buckets(ctx, q, `
		SELECT CASE WHEN l.referred_by IS NOT NULL AND a.utm_source IS NULL THEN 'referral'
		            ELSE COALESCE(a.utm_source, 'direct') END AS label, count(*)
		FROM leads l LEFT JOIN lead_attribution a ON a.lead_id = l.id AND a.touch = 'first'
		WHERE l.status <> 'BLOCKED'
		GROUP BY label ORDER BY count(*) DESC, label LIMIT 10`); err != nil {
		return nil, err
	}
	if o.Dietary, err = buckets(ctx, q, `
		SELECT d AS label, count(*)
		FROM lead_preferences p JOIN leads l ON l.id = p.lead_id, unnest(p.dietary_preferences) AS d
		WHERE l.status <> 'BLOCKED'
		GROUP BY d ORDER BY count(*) DESC, d LIMIT 12`); err != nil {
		return nil, err
	}
	if o.HouseholdSizes, err = buckets(ctx, q, `
		SELECT CASE WHEN p.household_size IS NULL THEN 'Not given'
		            WHEN p.household_size >= 6 THEN '6+'
		            ELSE p.household_size::text END AS label, count(*)
		FROM leads l LEFT JOIN lead_preferences p ON p.lead_id = l.id
		WHERE l.status <> 'BLOCKED'
		GROUP BY label ORDER BY label`); err != nil {
		return nil, err
	}
	if o.Locations, err = buckets(ctx, q, `
		SELECT location AS label, count(*) FROM leads WHERE status <> 'BLOCKED'
		GROUP BY location ORDER BY count(*) DESC, location LIMIT 15`); err != nil {
		return nil, err
	}
	return o, nil
}

func buckets(ctx context.Context, q db.Querier, sql string) ([]Bucket, error) {
	rows, err := q.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Bucket, error) {
		var b Bucket
		err := row.Scan(&b.Label, &b.Count)
		return b, err
	})
	if out == nil {
		out = []Bucket{}
	}
	return out, err
}
