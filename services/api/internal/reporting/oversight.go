package reporting

import (
	"context"
	"time"
)

// ---------------------------------------------------------------- referrals

// ReferralRow is one referral with the facts used to judge it.
type ReferralRow struct {
	ID             string     `json:"id"`
	Status         string     `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	ConvertedAt    *time.Time `json:"converted_at"`
	ReferrerID     string     `json:"referrer_lead_id"`
	ReferrerName   string     `json:"referrer_name"`
	ReferrerEmail  string     `json:"referrer_email"`
	ReferredID     string     `json:"referred_lead_id"`
	ReferredName   string     `json:"referred_name"`
	ReferredEmail  string     `json:"referred_email"`
	ReferredState  string     `json:"referred_status"`
	BecameCustomer bool       `json:"referred_became_customer"`
	// Flags are the suspicious patterns that apply (see Referrals).
	Flags []string `json:"flags"`
}

// Referrals lists referrals, newest first, with three explainable flags:
//   - same_network: both signed up from the same network (keyed IP hash)
//   - burst: the referrer brought 5 or more people within 24 hours of this one
//   - blocked: the referred lead has been blocked
//
// Flags are for review only; nothing is changed or rewarded automatically.
// No rewards exist: none have been approved.
func (s *Service) Referrals(ctx context.Context, flaggedOnly bool, page, size int) ([]ReferralRow, int, error) {
	if size < 1 || size > 100 {
		size = 50
	}
	if page < 1 {
		page = 1
	}
	qctx, cancel := s.ctx(ctx)
	defer cancel()
	rows, err := s.pool.Query(qctx, `
		WITH r AS (
		    SELECT rf.*, a.first_name AS a_name, a.email AS a_email, b.first_name AS b_name, b.email AS b_email, b.status AS b_status,
		           EXISTS (SELECT 1 FROM customers c WHERE c.lead_id = b.id) AS became_customer,
		           (a.signup_ip_hash IS NOT NULL AND a.signup_ip_hash = b.signup_ip_hash) AS same_network,
		           (SELECT count(*) FROM referrals x WHERE x.referrer_lead_id = rf.referrer_lead_id
		              AND x.created_at BETWEEN rf.created_at - interval '24 hours' AND rf.created_at + interval '24 hours') >= 5 AS burst,
		           b.status = 'BLOCKED' AS blocked
		    FROM referrals rf JOIN leads a ON a.id = rf.referrer_lead_id JOIN leads b ON b.id = rf.referred_lead_id
		)
		SELECT id, status, created_at, converted_at, referrer_lead_id, a_name, a_email, referred_lead_id, b_name, b_email, b_status,
		       became_customer, same_network, burst, blocked, count(*) OVER ()
		FROM r
		WHERE NOT $1::bool OR same_network OR burst OR blocked OR status = 'flagged'
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`, flaggedOnly, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []ReferralRow{}
	total := 0
	for rows.Next() {
		var r ReferralRow
		var sameNet, burst, blocked bool
		if err := rows.Scan(&r.ID, &r.Status, &r.CreatedAt, &r.ConvertedAt, &r.ReferrerID, &r.ReferrerName, &r.ReferrerEmail,
			&r.ReferredID, &r.ReferredName, &r.ReferredEmail, &r.ReferredState, &r.BecameCustomer, &sameNet, &burst, &blocked, &total); err != nil {
			return nil, 0, err
		}
		r.Flags = []string{}
		for flag, on := range map[string]bool{"same_network": sameNet, "burst": burst, "blocked": blocked, "flagged_at_signup": r.Status == "flagged"} {
			if on {
				r.Flags = append(r.Flags, flag)
			}
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// ---------------------------------------------------------------- retention

// RetentionSignal is a rule-based reason to look at a customer. No
// prediction: each signal is a fact about the customer's records.
type RetentionSignal struct {
	CustomerID string    `json:"customer_id"`
	Name       string    `json:"name"`
	Email      string    `json:"email"`
	Signal     string    `json:"signal"`
	Detail     string    `json:"detail"`
	Since      time.Time `json:"since"`
}

// RetentionRules describes each signal, for the screen and the report.
var RetentionRules = map[string]string{
	"failed_renewal":     "A weekly payment failed and the subscription is past due.",
	"recently_cancelled": "The subscription was cancelled in the last 30 days.",
	"missed_selections":  "Two or more paid weeks in the last six weeks ended without confirmed meals.",
	"inactive":           "A live subscriber who has not signed in for 30 days.",
	"stale_case":         "A support case open for more than 3 days without an update.",
}

// Retention lists the customers who match each signal now.
func (s *Service) Retention(ctx context.Context) ([]RetentionSignal, error) {
	qctx, cancel := s.ctx(ctx)
	defer cancel()
	rows, err := s.pool.Query(qctx, `
		WITH who AS (SELECT c.id, trim(u.first_name || ' ' || u.last_name) AS name, u.email, u.last_login_at, c.created_at
		             FROM customers c JOIN users u ON u.id = c.user_id)
		SELECT w.id, w.name, w.email, 'failed_renewal', 'Past due', s.updated_at
		FROM subscriptions s JOIN who w ON w.id = s.customer_id WHERE s.status = 'past_due'
		UNION ALL
		SELECT w.id, w.name, w.email, 'recently_cancelled', coalesce(s.cancel_reason, 'No reason given'), s.cancelled_at
		FROM subscriptions s JOIN who w ON w.id = s.customer_id
		WHERE s.status = 'cancelled' AND s.cancelled_at > now() - interval '30 days'
		  AND NOT EXISTS (SELECT 1 FROM subscriptions x WHERE x.customer_id = s.customer_id AND x.status NOT IN ('cancelled', 'expired'))
		UNION ALL
		SELECT w.id, w.name, w.email, 'missed_selections', count(*)::text || ' weeks without confirmed meals', max(sc.finalized_at)
		FROM subscription_cycles sc JOIN who w ON w.id = sc.customer_id
		WHERE sc.state = 'needs_resolution' AND sc.state_reason = 'no_confirmed_selection' AND sc.week_start > current_date - 42
		GROUP BY w.id, w.name, w.email HAVING count(*) >= 2
		UNION ALL
		SELECT w.id, w.name, w.email, 'inactive', 'Last sign-in ' || coalesce(to_char(w.last_login_at, 'YYYY-MM-DD'), 'never'),
		       coalesce(w.last_login_at, w.created_at)
		FROM subscriptions s JOIN who w ON w.id = s.customer_id
		WHERE s.status IN ('active', 'pause_scheduled', 'past_due', 'cancel_scheduled')
		  AND coalesce(w.last_login_at, w.created_at) < now() - interval '30 days'
		UNION ALL
		SELECT w.id, w.name, w.email, 'stale_case', 'Case #' || sc.number || ': ' || sc.subject, sc.updated_at
		FROM support_cases sc JOIN who w ON w.id = sc.customer_id
		WHERE sc.status IN ('open', 'in_progress', 'waiting_for_customer') AND sc.updated_at < now() - interval '3 days'
		ORDER BY 6 DESC
		LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RetentionSignal{}
	for rows.Next() {
		var r RetentionSignal
		if err := rows.Scan(&r.CustomerID, &r.Name, &r.Email, &r.Signal, &r.Detail, &r.Since); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- campaigns

// Attribution is what a campaign's UTM value brought in, by first touch.
type Attribution struct {
	Leads         int    `json:"leads"`
	VerifiedLeads int    `json:"verified_leads"`
	Customers     int    `json:"customers"`
	Subscribers   int    `json:"subscribers"`
	Orders        int    `json:"orders"`
	Definition    string `json:"definition"`
}

// CampaignAttribution counts leads whose first visit carried utm_campaign,
// and what became of them. Customers who did not come through the
// waitlist are not attributed (no first-touch record exists for them).
func (s *Service) CampaignAttribution(ctx context.Context, utmCampaign string) (*Attribution, error) {
	a := &Attribution{Definition: "Waitlist leads whose first visit carried this utm_campaign; of those, how many confirmed, became customers, subscribed (ever activated) and how many orders they have."}
	if utmCampaign == "" {
		return a, nil
	}
	qctx, cancel := s.ctx(ctx)
	defer cancel()
	err := s.pool.QueryRow(qctx, `
		WITH l AS (SELECT l.id, l.email_verified_at FROM leads l JOIN lead_attribution la ON la.lead_id = l.id AND la.touch = 'first'
		           WHERE la.utm_campaign = $1),
		     c AS (SELECT c.id FROM customers c WHERE c.lead_id IN (SELECT id FROM l))
		SELECT (SELECT count(*) FROM l)::int, (SELECT count(*) FROM l WHERE email_verified_at IS NOT NULL)::int, (SELECT count(*) FROM c)::int,
		       (SELECT count(DISTINCT customer_id) FROM subscriptions WHERE activated_at IS NOT NULL AND customer_id IN (SELECT id FROM c))::int,
		       (SELECT count(*) FROM orders WHERE customer_id IN (SELECT id FROM c))::int`, utmCampaign).
		Scan(&a.Leads, &a.VerifiedLeads, &a.Customers, &a.Subscribers, &a.Orders)
	return a, err
}

// ---------------------------------------------------------------- invoices

type InvoiceRow struct {
	ID          string    `json:"id"`
	Number      string    `json:"number"`
	Status      string    `json:"status"`
	CustomerID  string    `json:"customer_id"`
	Customer    string    `json:"customer"`
	IssuedAt    time.Time `json:"issued_at"`
	PeriodStart string    `json:"period_start"`
	TotalMinor  int64     `json:"total_minor"`
	Currency    string    `json:"currency"`
}

// Invoices lists issued invoices for staff, newest first.
func (s *Service) Invoices(ctx context.Context, status, query string, page, size int) ([]InvoiceRow, int, error) {
	if size < 1 || size > 100 {
		size = 50
	}
	if page < 1 {
		page = 1
	}
	qctx, cancel := s.ctx(ctx)
	defer cancel()
	rows, err := s.pool.Query(qctx, `
		SELECT i.id, i.number, i.status, i.customer_id, coalesce(i.customer_snapshot ->> 'name', ''), i.issued_at, i.period_start, i.total_minor, i.currency,
		       count(*) OVER ()
		FROM invoices i
		WHERE ($1 = '' OR i.status = $1)
		  AND ($2 = '' OR i.number ILIKE '%' || $2 || '%' OR (i.customer_snapshot ->> 'email') ILIKE '%' || $2 || '%')
		ORDER BY i.issued_at DESC
		LIMIT $3 OFFSET $4`, status, query, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []InvoiceRow{}
	total := 0
	for rows.Next() {
		var r InvoiceRow
		var period time.Time
		if err := rows.Scan(&r.ID, &r.Number, &r.Status, &r.CustomerID, &r.Customer, &r.IssuedAt, &period, &r.TotalMinor, &r.Currency, &total); err != nil {
			return nil, 0, err
		}
		r.PeriodStart = period.Format("2006-01-02")
		out = append(out, r)
	}
	return out, total, rows.Err()
}
