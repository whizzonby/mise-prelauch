package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/leads"
	"mise.tt/api/internal/platform/db"
)

type LeadFilter struct {
	Query    string
	Status   string
	Location string
	Source   string
	// Packaging is a packaging option value, or "none" for leads who did not choose.
	Packaging string
	From      *time.Time
	To        *time.Time
	// Referral is "referrer" (has at least one converted referral) or
	// "referred" (was invited by someone).
	Referral string
	Page     int
	PageSize int
}

type LeadRow struct {
	ID                 string    `json:"id"`
	FirstName          string    `json:"first_name"`
	Email              string    `json:"email"`
	Status             string    `json:"status"`
	Location           string    `json:"location"`
	Source             string    `json:"source"`
	HouseholdSize      *int      `json:"household_size"`
	ReferralsConverted int       `json:"referrals_converted"`
	WasReferred        bool      `json:"was_referred"`
	CreatedAt          time.Time `json:"created_at"`
}

type LeadPage struct {
	Items    []LeadRow `json:"items"`
	Total    int       `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"page_size"`
}

const sourceExpr = `CASE WHEN l.referred_by IS NOT NULL AND a.utm_source IS NULL THEN 'referral'
	ELSE COALESCE(a.utm_source, 'direct') END`

const leadFrom = `FROM leads l
	LEFT JOIN lead_attribution a ON a.lead_id = l.id AND a.touch = 'first'
	LEFT JOIN lead_preferences p ON p.lead_id = l.id`

// where builds the WHERE clause for a filter. Every value is a bound
// parameter; no user input is concatenated into SQL.
func (f LeadFilter) where() (string, []any) {
	var clauses []string
	var args []any
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		// Escape LIKE wildcards so a search for "100%" means the literal text.
		pattern := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.ToLower(q)) + "%"
		p := arg(pattern)
		clauses = append(clauses, "(lower(l.first_name) LIKE "+p+" OR l.email LIKE "+p+")")
	}
	if f.Status != "" {
		clauses = append(clauses, "l.status = "+arg(f.Status))
	}
	if f.Location != "" {
		clauses = append(clauses, "l.location = "+arg(f.Location))
	}
	if f.Source != "" {
		clauses = append(clauses, sourceExpr+" = "+arg(f.Source))
	}
	switch f.Packaging {
	case "":
	case "none":
		clauses = append(clauses, "p.packaging_preference IS NULL")
	default:
		clauses = append(clauses, "p.packaging_preference = "+arg(f.Packaging))
	}
	if f.From != nil {
		clauses = append(clauses, "l.created_at >= "+arg(*f.From))
	}
	if f.To != nil {
		clauses = append(clauses, "l.created_at < "+arg(*f.To))
	}
	switch f.Referral {
	case "referrer":
		clauses = append(clauses, "EXISTS (SELECT 1 FROM referrals r WHERE r.referrer_lead_id = l.id AND r.status = 'converted')")
	case "referred":
		clauses = append(clauses, "l.referred_by IS NOT NULL")
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

const leadRowSelect = `SELECT l.id, l.first_name, l.email, l.status, l.location, ` + sourceExpr + `,
	p.household_size,
	(SELECT count(*) FROM referrals r WHERE r.referrer_lead_id = l.id AND r.status = 'converted'),
	l.referred_by IS NOT NULL, l.created_at `

func scanLeadRow(row pgx.CollectableRow) (LeadRow, error) {
	var r LeadRow
	err := row.Scan(&r.ID, &r.FirstName, &r.Email, &r.Status, &r.Location, &r.Source, &r.HouseholdSize,
		&r.ReferralsConverted, &r.WasReferred, &r.CreatedAt)
	return r, err
}

func listLeads(ctx context.Context, q db.Querier, f LeadFilter) (*LeadPage, error) {
	where, args := f.where()
	page := &LeadPage{Page: f.Page, PageSize: f.PageSize, Items: []LeadRow{}}

	if err := q.QueryRow(ctx, `SELECT count(*) `+leadFrom+where, args...).Scan(&page.Total); err != nil {
		return nil, err
	}
	args = append(args, f.PageSize, (f.Page-1)*f.PageSize)
	rows, err := q.Query(ctx, leadRowSelect+leadFrom+where+
		fmt.Sprintf(" ORDER BY l.created_at DESC, l.id LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	items, err := pgx.CollectRows(rows, scanLeadRow)
	if err != nil {
		return nil, err
	}
	if items != nil {
		page.Items = items
	}
	return page, nil
}

type Preferences struct {
	HouseholdSize       *int            `json:"household_size"`
	MealsPerWeek        *int            `json:"meals_per_week"`
	DietaryPreferences  []string        `json:"dietary_preferences"`
	MealInterests       []string        `json:"meal_interests"`
	CookingFrequency    *string         `json:"cooking_frequency"`
	DeliveryArea        *string         `json:"delivery_area"`
	PackagingPreference *string         `json:"packaging_preference"`
	Metadata            json.RawMessage `json:"metadata"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

type AttributionTouch struct {
	Touch       string    `json:"touch"`
	UTMSource   *string   `json:"utm_source"`
	UTMMedium   *string   `json:"utm_medium"`
	UTMCampaign *string   `json:"utm_campaign"`
	UTMContent  *string   `json:"utm_content"`
	UTMTerm     *string   `json:"utm_term"`
	LandingPage *string   `json:"landing_page"`
	ReferrerURL *string   `json:"referrer_url"`
	CreatedAt   time.Time `json:"created_at"`
}

type ReferralEntry struct {
	LeadID      string     `json:"lead_id"`
	FirstName   string     `json:"first_name"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	ConvertedAt *time.Time `json:"converted_at"`
}

type TimelineEntry struct {
	Kind      string          `json:"kind"` // event or audit
	Type      string          `json:"type"`
	Metadata  json.RawMessage `json:"metadata"`
	CreatedAt time.Time       `json:"created_at"`
}

type LeadDetail struct {
	ID              string             `json:"id"`
	FirstName       string             `json:"first_name"`
	Email           string             `json:"email"`
	Phone           *string            `json:"phone"`
	Location        string             `json:"location"`
	Status          string             `json:"status"`
	ReferralCode    string             `json:"referral_code"`
	EmailVerifiedAt *time.Time         `json:"email_verified_at"`
	ConsentAt       time.Time          `json:"consent_at"`
	ConsentVersion  string             `json:"consent_version"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
	Preferences     *Preferences       `json:"preferences"`
	Attribution     []AttributionTouch `json:"attribution"`
	ReferredBy      *ReferralEntry     `json:"referred_by"`
	Referrals       []ReferralEntry    `json:"referrals"`
	Timeline        []TimelineEntry    `json:"timeline"`
}

// leadDetail returns nil, nil when the lead does not exist.
func leadDetail(ctx context.Context, q db.Querier, id string) (*LeadDetail, error) {
	d := &LeadDetail{}
	err := q.QueryRow(ctx, `
		SELECT id, first_name, email, phone, location, status, referral_code, email_verified_at,
			consent_at, consent_version, created_at, updated_at
		FROM leads WHERE id = $1`, id,
	).Scan(&d.ID, &d.FirstName, &d.Email, &d.Phone, &d.Location, &d.Status, &d.ReferralCode,
		&d.EmailVerifiedAt, &d.ConsentAt, &d.ConsentVersion, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var p Preferences
	err = q.QueryRow(ctx, `
		SELECT household_size, meals_per_week, dietary_preferences, meal_interests, cooking_frequency,
			delivery_area, packaging_preference, metadata, updated_at
		FROM lead_preferences WHERE lead_id = $1`, id,
	).Scan(&p.HouseholdSize, &p.MealsPerWeek, &p.DietaryPreferences, &p.MealInterests, &p.CookingFrequency,
		&p.DeliveryArea, &p.PackagingPreference, &p.Metadata, &p.UpdatedAt)
	switch {
	case err == nil:
		d.Preferences = &p
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, err
	}

	rows, err := q.Query(ctx, `
		SELECT touch, utm_source, utm_medium, utm_campaign, utm_content, utm_term, landing_page, referrer_url, created_at
		FROM lead_attribution WHERE lead_id = $1 ORDER BY touch`, id)
	if err != nil {
		return nil, err
	}
	d.Attribution, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (AttributionTouch, error) {
		var a AttributionTouch
		err := row.Scan(&a.Touch, &a.UTMSource, &a.UTMMedium, &a.UTMCampaign, &a.UTMContent, &a.UTMTerm,
			&a.LandingPage, &a.ReferrerURL, &a.CreatedAt)
		return a, err
	})
	if err != nil {
		return nil, err
	}

	scanReferral := func(row pgx.CollectableRow) (ReferralEntry, error) {
		var e ReferralEntry
		err := row.Scan(&e.LeadID, &e.FirstName, &e.Status, &e.CreatedAt, &e.ConvertedAt)
		return e, err
	}
	rows, err = q.Query(ctx, `
		SELECT l.id, l.first_name, r.status, r.created_at, r.converted_at
		FROM referrals r JOIN leads l ON l.id = r.referred_lead_id
		WHERE r.referrer_lead_id = $1 ORDER BY r.created_at DESC LIMIT 200`, id)
	if err != nil {
		return nil, err
	}
	if d.Referrals, err = pgx.CollectRows(rows, scanReferral); err != nil {
		return nil, err
	}
	rows, err = q.Query(ctx, `
		SELECT l.id, l.first_name, r.status, r.created_at, r.converted_at
		FROM referrals r JOIN leads l ON l.id = r.referrer_lead_id
		WHERE r.referred_lead_id = $1`, id)
	if err != nil {
		return nil, err
	}
	referredBy, err := pgx.CollectRows(rows, scanReferral)
	if err != nil {
		return nil, err
	}
	if len(referredBy) > 0 {
		d.ReferredBy = &referredBy[0]
	}

	rows, err = q.Query(ctx, `
		SELECT kind, type, metadata, created_at FROM (
			SELECT 'event' AS kind, event_type AS type, metadata, created_at
			FROM lead_events WHERE lead_id = $1
			UNION ALL
			SELECT 'audit', action, metadata, created_at
			FROM audit_log WHERE target_type = 'lead' AND target_id = $1::text
		) t ORDER BY created_at DESC LIMIT 200`, id)
	if err != nil {
		return nil, err
	}
	d.Timeline, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (TimelineEntry, error) {
		var t TimelineEntry
		err := row.Scan(&t.Kind, &t.Type, &t.Metadata, &t.CreatedAt)
		return t, err
	})
	if err != nil {
		return nil, err
	}

	// Encode empty collections as [] rather than null.
	if d.Attribution == nil {
		d.Attribution = []AttributionTouch{}
	}
	if d.Referrals == nil {
		d.Referrals = []ReferralEntry{}
	}
	if d.Timeline == nil {
		d.Timeline = []TimelineEntry{}
	}
	return d, nil
}

// adminSettableStatuses are the transitions the team makes by hand. PENDING
// and VERIFIED are reached only by the lead's own actions; CONVERTED will be
// set by the customer migration at launch.
var adminSettableStatuses = map[leads.Status]bool{
	leads.StatusQualified:    true,
	leads.StatusBlocked:      true,
	leads.StatusUnsubscribed: true,
	leads.StatusVerified:     true, // undo a qualification or a block
}

// setLeadStatus changes a lead's status and returns the previous one. ok is
// false when the lead does not exist.
func setLeadStatus(ctx context.Context, q db.Querier, id string, status leads.Status) (previous string, ok bool, err error) {
	err = q.QueryRow(ctx, `
		WITH old AS (SELECT status FROM leads WHERE id = $1 FOR UPDATE)
		UPDATE leads SET status = $2, updated_at = now() FROM old WHERE leads.id = $1
		RETURNING old.status`, id, status).Scan(&previous)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return previous, err == nil, err
}

func audit(ctx context.Context, q db.Querier, actorID, action, targetType, targetID string, metadata map[string]any) error {
	if metadata == nil {
		metadata = map[string]any{}
	}
	body, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `
		INSERT INTO audit_log (actor_admin_id, action, target_type, target_id, metadata)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), $5::jsonb)`,
		nullIfEmpty(actorID), action, targetType, targetID, string(body))
	return err
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
