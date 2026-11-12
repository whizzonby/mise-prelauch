// Package reporting is the read-only view across modules for staff: the
// customer list and segments, the operational overview, reports, referral
// and retention oversight, campaign attribution, and CSV exports.
//
// It is the one place that reads several modules' tables in one query, so
// that totals are aggregated in PostgreSQL rather than added up from pages
// in a browser. It never writes. Every figure it returns has a written
// definition (docs/architecture/SPRINT_4_PLAN.md, "Metric definitions"),
// and periods are reckoned in Trinidad and Tobago time.
package reporting

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/crm"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/vocab"
)

// TimeZone is the zone periods are reckoned in.
const TimeZone = "America/Port_of_Spain"

// Live subscription statuses: still delivering or owing.
var liveStatuses = []string{"active", "pause_scheduled", "paused", "past_due", "cancel_scheduled"}

type Service struct {
	pool *pgxpool.Pool
	Now  func() time.Time
}

func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool, Now: time.Now} }

// queryTimeout bounds every report query.
const queryTimeout = 15 * time.Second

func (s *Service) ctx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, queryTimeout)
}

// args collects positional parameters for a dynamic query.
type args struct{ list []any }

func (a *args) add(v any) string {
	a.list = append(a.list, v)
	return fmt.Sprintf("$%d", len(a.list))
}

// ---------------------------------------------------------------- customers

// customerBase is every customer with the facts lists and segments filter
// on: identity, the most recent subscription, its plan, and the first-touch
// source of the waitlist lead they came from.
const customerBase = `
FROM customers c
JOIN users u ON u.id = c.user_id
LEFT JOIN LATERAL (
    SELECT s.* FROM subscriptions s WHERE s.customer_id = c.id ORDER BY s.created_at DESC LIMIT 1
) s ON true
LEFT JOIN subscription_plans p ON p.id = s.plan_id
LEFT JOIN lead_attribution la ON la.lead_id = c.lead_id AND la.touch = 'first'`

// RuleConditions turns segment rules into SQL conditions over customerBase.
// Each rule is a plain, explainable condition.
func RuleConditions(r crm.Rules, a *args) []string {
	var w []string
	if len(r.PlanIDs) > 0 {
		w = append(w, "s.plan_id = ANY("+a.add(r.PlanIDs)+"::uuid[])")
	}
	if len(r.SubscriptionStatuses) > 0 {
		w = append(w, "s.status = ANY("+a.add(r.SubscriptionStatuses)+"::text[])")
	}
	if len(r.Lifecycle) > 0 {
		var any []string
		for _, l := range r.Lifecycle {
			switch l {
			case "prospect":
				any = append(any, "NOT EXISTS (SELECT 1 FROM subscriptions x WHERE x.customer_id = c.id AND x.activated_at IS NOT NULL)")
			case "subscriber":
				any = append(any, "s.status = ANY("+a.add(liveStatuses)+"::text[])")
			case "lapsed":
				any = append(any, "(s.status IN ('cancelled', 'expired') AND EXISTS (SELECT 1 FROM subscriptions x WHERE x.customer_id = c.id AND x.activated_at IS NOT NULL))")
			}
		}
		w = append(w, "("+strings.Join(any, " OR ")+")")
	}
	if r.JoinedFrom != "" {
		w = append(w, "(c.created_at AT TIME ZONE '"+TimeZone+"')::date >= "+a.add(r.JoinedFrom)+"::date")
	}
	if r.JoinedTo != "" {
		w = append(w, "(c.created_at AT TIME ZONE '"+TimeZone+"')::date <= "+a.add(r.JoinedTo)+"::date")
	}
	if len(r.Regions) > 0 {
		p := a.add(r.Regions)
		w = append(w, "(c.delivery_region = ANY("+p+"::text[]) OR EXISTS (SELECT 1 FROM customer_addresses ad WHERE ad.customer_id = c.id AND ad.is_default AND ad.region = ANY("+p+"::text[])))")
	}
	if len(r.Dietary) > 0 {
		w = append(w, "EXISTS (SELECT 1 FROM customer_preferences cp WHERE cp.customer_id = c.id AND cp.dietary && "+a.add(r.Dietary)+"::text[])")
	}
	if len(r.Sources) > 0 {
		w = append(w, "coalesce(la.utm_source, 'direct') = ANY("+a.add(r.Sources)+"::text[])")
	}
	if r.MinOrders != nil {
		w = append(w, "(SELECT count(*) FROM orders o WHERE o.customer_id = c.id) >= "+a.add(*r.MinOrders)+"::int")
	}
	if r.MinTenureWeeks != nil {
		w = append(w, "EXISTS (SELECT 1 FROM subscriptions x WHERE x.customer_id = c.id AND x.activated_at <= now() - make_interval(weeks => "+a.add(*r.MinTenureWeeks)+"::int))")
	}
	if r.ConsentTopic != "" {
		w = append(w, "EXISTS (SELECT 1 FROM notification_preferences np WHERE np.customer_id = c.id AND np.topic = "+a.add(r.ConsentTopic)+" AND np.enabled)")
	}
	return w
}

// CustomerFilter narrows the customer list.
type CustomerFilter struct {
	Query              string
	Status             string // verified, unverified, disabled
	SubscriptionStatus string // a status, or "none"
	PlanID             string
	Region             string
	Rules              *crm.Rules
	Sort               string // joined, name, last_login
	Page, Size         int
}

// CustomerRow is one customer in the staff list.
type CustomerRow struct {
	ID                 string     `json:"id"`
	Name               string     `json:"name"`
	Email              string     `json:"email"`
	Status             string     `json:"status"`
	Segment            *string    `json:"segment"`
	SubscriptionStatus *string    `json:"subscription_status"`
	PlanName           *string    `json:"plan_name"`
	Region             *string    `json:"region"`
	Source             string     `json:"source"`
	JoinedAt           time.Time  `json:"joined_at"`
	LastLoginAt        *time.Time `json:"last_login_at"`
	Flags              []string   `json:"flags"`
}

type CustomerPage struct {
	Items []CustomerRow `json:"items"`
	Total int           `json:"total"`
	Page  int           `json:"page"`
	Size  int           `json:"page_size"`
}

func (f *CustomerFilter) where(a *args) []string {
	var w []string
	if q := strings.TrimSpace(f.Query); q != "" {
		p := a.add("%" + strings.ToLower(q) + "%")
		w = append(w, "(lower(u.email) LIKE "+p+" OR lower(u.first_name || ' ' || u.last_name) LIKE "+p+")")
	}
	switch f.Status {
	case "verified":
		w = append(w, "u.status = 'active' AND u.email_verified_at IS NOT NULL")
	case "unverified":
		w = append(w, "u.email_verified_at IS NULL")
	case "disabled":
		w = append(w, "u.status = 'disabled'")
	}
	switch {
	case f.SubscriptionStatus == "none":
		w = append(w, "s.id IS NULL")
	case f.SubscriptionStatus != "":
		w = append(w, "s.status = "+a.add(f.SubscriptionStatus))
	}
	if f.PlanID != "" {
		w = append(w, "s.plan_id = "+a.add(f.PlanID)+"::uuid")
	}
	if f.Region != "" && vocab.Regions.Has(f.Region) {
		w = append(w, "c.delivery_region = "+a.add(f.Region))
	}
	if f.Rules != nil {
		w = append(w, RuleConditions(*f.Rules, a)...)
	}
	return w
}

func (s *Service) Customers(ctx context.Context, f CustomerFilter) (*CustomerPage, error) {
	if f.Size < 1 || f.Size > 100 {
		f.Size = 25
	}
	if f.Page < 1 {
		f.Page = 1
	}
	order := map[string]string{
		"name":       "lower(u.first_name), lower(u.last_name), c.id",
		"last_login": "u.last_login_at DESC NULLS LAST, c.id",
	}[f.Sort]
	if order == "" {
		order = "c.created_at DESC, c.id"
	}
	a := &args{}
	w := f.where(a)
	cond := ""
	if len(w) > 0 {
		cond = "WHERE " + strings.Join(w, " AND ")
	}
	sql := `SELECT c.id, trim(u.first_name || ' ' || u.last_name), u.email,
	        CASE WHEN u.status = 'disabled' THEN 'disabled' WHEN u.email_verified_at IS NULL THEN 'unverified' ELSE 'verified' END,
	        p.segment, s.status, p.name, c.delivery_region, coalesce(la.utm_source, 'direct'), c.created_at, u.last_login_at,
	        EXISTS (SELECT 1 FROM support_cases sc WHERE sc.customer_id = c.id AND sc.status IN ('open', 'in_progress', 'waiting_for_customer')),
	        EXISTS (SELECT 1 FROM payments py WHERE py.customer_id = c.id AND py.needs_attention),
	        count(*) OVER ()` + customerBase + " " + cond + " ORDER BY " + order +
		" LIMIT " + a.add(f.Size) + " OFFSET " + a.add((f.Page-1)*f.Size)
	qctx, cancel := s.ctx(ctx)
	defer cancel()
	rows, err := s.pool.Query(qctx, sql, a.list...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := &CustomerPage{Items: []CustomerRow{}, Page: f.Page, Size: f.Size}
	for rows.Next() {
		var r CustomerRow
		var openCase, attention bool
		var total int
		if err := rows.Scan(&r.ID, &r.Name, &r.Email, &r.Status, &r.Segment, &r.SubscriptionStatus, &r.PlanName, &r.Region, &r.Source,
			&r.JoinedAt, &r.LastLoginAt, &openCase, &attention, &total); err != nil {
			return nil, err
		}
		r.Flags = []string{}
		if r.SubscriptionStatus != nil && *r.SubscriptionStatus == "past_due" {
			r.Flags = append(r.Flags, "payment_failed")
		}
		if openCase {
			r.Flags = append(r.Flags, "open_case")
		}
		if attention {
			r.Flags = append(r.Flags, "payment_review")
		}
		out.Items = append(out.Items, r)
		out.Total = total
	}
	return out, rows.Err()
}

// SegmentSize counts the customers a segment matches, with a sample.
func (s *Service) SegmentPreview(ctx context.Context, rules crm.Rules) (*CustomerPage, error) {
	return s.Customers(ctx, CustomerFilter{Rules: &rules, Size: 20})
}

// SegmentCustomerIDs is every customer id in a segment (capped), for exports
// and a future marketing provider.
func (s *Service) SegmentCount(ctx context.Context, rules crm.Rules) (int, error) {
	a := &args{}
	w := RuleConditions(rules, a)
	cond := ""
	if len(w) > 0 {
		cond = "WHERE " + strings.Join(w, " AND ")
	}
	var n int
	qctx, cancel := s.ctx(ctx)
	defer cancel()
	err := s.pool.QueryRow(qctx, "SELECT count(*)"+customerBase+" "+cond, a.list...).Scan(&n)
	return n, err
}

// CustomerOverview is the identity part of the Customer 360.
type CustomerOverview struct {
	ID              string     `json:"id"`
	UserID          string     `json:"user_id"`
	FirstName       string     `json:"first_name"`
	LastName        string     `json:"last_name"`
	Email           string     `json:"email"`
	Status          string     `json:"status"`
	EmailVerifiedAt *time.Time `json:"email_verified_at"`
	LastLoginAt     *time.Time `json:"last_login_at"`
	JoinedAt        time.Time  `json:"joined_at"`
	ContactChannel  string     `json:"contact_channel"`
	Region          *string    `json:"region"`
	RegionLabel     *string    `json:"region_label"`
	Dietary         []string   `json:"dietary"`
	HouseholdSize   *int16     `json:"household_size"`
	LeadID          *string    `json:"lead_id"`
	LeadLinkedAt    *time.Time `json:"lead_linked_at"`
	Source          string     `json:"source"`
	UTMCampaign     *string    `json:"utm_campaign"`
	ReferredBy      *string    `json:"referred_by_lead_id"`
	Segment         *string    `json:"segment"`
	Orders          int        `json:"orders"`
	OpenCases       int        `json:"open_cases"`
	Consents        []string   `json:"marketing_consents"`
}

// CustomerOverview reads the customer's identity, acquisition and summary.
// Allergies, addresses and phone numbers are not here (sensitive).
func (s *Service) CustomerOverview(ctx context.Context, id string) (*CustomerOverview, error) {
	o := CustomerOverview{}
	qctx, cancel := s.ctx(ctx)
	defer cancel()
	err := s.pool.QueryRow(qctx, `
		SELECT c.id, u.id, u.first_name, u.last_name, u.email,
		       CASE WHEN u.status = 'disabled' THEN 'disabled' WHEN u.email_verified_at IS NULL THEN 'unverified' ELSE 'verified' END,
		       u.email_verified_at, u.last_login_at, c.created_at, c.contact_channel, c.delivery_region,
		       coalesce(cp.dietary, '{}'), h.size, c.lead_id, c.lead_linked_at,
		       coalesce(la.utm_source, 'direct'), la.utm_campaign, l.referred_by, p.segment,
		       (SELECT count(*) FROM orders o WHERE o.customer_id = c.id)::int,
		       (SELECT count(*) FROM support_cases sc WHERE sc.customer_id = c.id AND sc.status IN ('open', 'in_progress', 'waiting_for_customer'))::int,
		       coalesce((SELECT array_agg(np.topic ORDER BY np.topic) FROM notification_preferences np WHERE np.customer_id = c.id AND np.enabled AND np.topic <> 'account_security'), '{}')
		`+customerBase+`
		LEFT JOIN customer_preferences cp ON cp.customer_id = c.id
		LEFT JOIN customer_households h ON h.customer_id = c.id
		LEFT JOIN leads l ON l.id = c.lead_id
		WHERE c.id = $1`, id).Scan(&o.ID, &o.UserID, &o.FirstName, &o.LastName, &o.Email, &o.Status, &o.EmailVerifiedAt, &o.LastLoginAt,
		&o.JoinedAt, &o.ContactChannel, &o.Region, &o.Dietary, &o.HouseholdSize, &o.LeadID, &o.LeadLinkedAt, &o.Source, &o.UTMCampaign,
		&o.ReferredBy, &o.Segment, &o.Orders, &o.OpenCases, &o.Consents)
	if err == pgx.ErrNoRows {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if o.Region != nil {
		l := vocab.Regions.Label(*o.Region)
		o.RegionLabel = &l
	}
	return &o, nil
}

// ActivityEntry is one thing that happened to or around a customer.
type ActivityEntry struct {
	At         time.Time      `json:"at"`
	Source     string         `json:"source"` // audit, subscription
	Action     string         `json:"action"`
	ActorName  *string        `json:"actor_name"`
	TargetType *string        `json:"target_type"`
	Reason     *string        `json:"reason"`
	Metadata   map[string]any `json:"metadata"`
}

// CustomerActivity merges audit entries about the customer, their account
// and their subscriptions, newest first. Note bodies are never in it.
func (s *Service) CustomerActivity(ctx context.Context, id string, limit int) ([]ActivityEntry, error) {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	qctx, cancel := s.ctx(ctx)
	defer cancel()
	rows, err := s.pool.Query(qctx, `
		WITH ids AS (
		    SELECT c.id::text AS id FROM customers c WHERE c.id = $1
		    UNION SELECT c.user_id::text FROM customers c WHERE c.id = $1
		    UNION SELECT s.id::text FROM subscriptions s WHERE s.customer_id = $1
		    UNION SELECT o.id::text FROM orders o WHERE o.customer_id = $1
		    UNION SELECT p.id::text FROM payments p WHERE p.customer_id = $1
		    UNION SELECT sc.id::text FROM subscription_cycles sc WHERE sc.customer_id = $1
		)
		SELECT a.created_at, 'audit', a.action, nullif(trim(u.first_name || ' ' || u.last_name), ''), a.target_type, a.reason, a.metadata
		FROM audit_log a LEFT JOIN users u ON u.id = a.actor_user_id
		WHERE a.target_id IN (SELECT id FROM ids)
		ORDER BY a.created_at DESC
		LIMIT $2`, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ActivityEntry{}
	for rows.Next() {
		var e ActivityEntry
		if err := rows.Scan(&e.At, &e.Source, &e.Action, &e.ActorName, &e.TargetType, &e.Reason, &e.Metadata); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
