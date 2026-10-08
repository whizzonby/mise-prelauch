package leads

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/platform/db"
)

const leadColumns = `id, first_name, last_name, email, phone, location, status, referral_code, referred_by,
	email_verified_at, consent_at, created_at, updated_at`

func scanLead(row pgx.Row) (*Lead, error) {
	var l Lead
	err := row.Scan(&l.ID, &l.FirstName, &l.LastName, &l.Email, &l.Phone, &l.Location, &l.Status, &l.ReferralCode,
		&l.ReferredBy, &l.EmailVerifiedAt, &l.ConsentAt, &l.CreatedAt, &l.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// GetByID returns nil, nil when there is no such lead.
func GetByID(ctx context.Context, q db.Querier, id string) (*Lead, error) {
	return scanLead(q.QueryRow(ctx, `SELECT `+leadColumns+` FROM leads WHERE id = $1`, id))
}

func getForUpdate(ctx context.Context, q db.Querier, id string) (*Lead, error) {
	return scanLead(q.QueryRow(ctx, `SELECT `+leadColumns+` FROM leads WHERE id = $1 FOR UPDATE`, id))
}

func getByCanonicalEmailForUpdate(ctx context.Context, q db.Querier, canonical string) (*Lead, error) {
	return scanLead(q.QueryRow(ctx, `SELECT `+leadColumns+` FROM leads WHERE email_canonical = $1 FOR UPDATE`, canonical))
}

type insertLead struct {
	normalizedSignup
	Status         Status
	Code           string
	ReferredBy     *string
	ConsentVersion string
	IPHash         string
}

func insert(ctx context.Context, q db.Querier, in insertLead) (*Lead, error) {
	var ipHash *string
	if in.IPHash != "" {
		ipHash = &in.IPHash
	}
	lead, err := scanLead(q.QueryRow(ctx, `
		INSERT INTO leads (first_name, last_name, email, email_canonical, phone, location, status, referral_code,
			referred_by, consent_at, consent_version, signup_ip_hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now(), $10, $11)
		RETURNING `+leadColumns,
		in.FirstName, in.LastName, in.Email, in.EmailCanonical, in.Phone, in.Location, in.Status, in.Code,
		in.ReferredBy, in.ConsentVersion, ipHash))
	if err != nil {
		return nil, err
	}

	if _, err := q.Exec(ctx,
		`INSERT INTO lead_preferences (lead_id, household_size, dietary_preferences, packaging_preference) VALUES ($1, $2, $3, $4)`,
		lead.ID, in.HouseholdSize, in.Dietary, in.Packaging); err != nil {
		return nil, err
	}

	for touch, t := range map[string]*Touch{"first": in.Attribution.First, "latest": in.Attribution.Latest} {
		if t == nil {
			continue
		}
		if _, err := q.Exec(ctx, `
			INSERT INTO lead_attribution (lead_id, touch, utm_source, utm_medium, utm_campaign, utm_content,
				utm_term, landing_page, referrer_url)
			VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''),
				NULLIF($8, ''), NULLIF($9, ''))`,
			lead.ID, touch, t.UTMSource, t.UTMMedium, t.UTMCampaign, t.UTMContent, t.UTMTerm,
			t.LandingPage, t.ReferrerURL); err != nil {
			return nil, err
		}
	}

	// Join the visitor's earlier anonymous events to the lead they became.
	if in.AnonymousID != "" {
		if _, err := q.Exec(ctx,
			`UPDATE lead_events SET lead_id = $1 WHERE anonymous_id = $2 AND lead_id IS NULL`,
			lead.ID, in.AnonymousID); err != nil {
			return nil, err
		}
	}
	return lead, nil
}

func setStatus(ctx context.Context, q db.Querier, id string, status Status) error {
	_, err := q.Exec(ctx, `UPDATE leads SET status = $2, updated_at = now() WHERE id = $1`, id, status)
	return err
}

func markVerified(ctx context.Context, q db.Querier, id string, status Status) error {
	_, err := q.Exec(ctx,
		`UPDATE leads SET status = $2, email_verified_at = COALESCE(email_verified_at, now()), updated_at = now() WHERE id = $1`,
		id, status)
	return err
}

func renewConsent(ctx context.Context, q db.Querier, id string, status Status, version string) error {
	_, err := q.Exec(ctx,
		`UPDATE leads SET status = $2, consent_at = now(), consent_version = $3, updated_at = now() WHERE id = $1`,
		id, status, version)
	return err
}

// recentlyEmailed reports whether an email job of this kind was queued for the
// lead in the last ten minutes. It stops a repeated signup from being used to
// flood someone's inbox.
func recentlyEmailed(ctx context.Context, q db.Querier, leadID, kind string) (bool, error) {
	var exists bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM jobs
			WHERE kind = $1 AND payload->>'lead_id' = $2 AND created_at > now() - interval '10 minutes'
		)`, kind, leadID).Scan(&exists)
	return exists, err
}

func profileCompleted(ctx context.Context, q db.Querier, leadID string) (bool, error) {
	var done bool
	err := q.QueryRow(ctx, `
		SELECT COALESCE((
			SELECT meals_per_week IS NOT NULL OR cooking_frequency IS NOT NULL OR metadata <> '{}'
			FROM lead_preferences WHERE lead_id = $1
		), false)`, leadID).Scan(&done)
	return done, err
}

func upsertPreferences(ctx context.Context, q db.Querier, leadID string, p normalizedPreferences) error {
	metadata, err := json.Marshal(p.Metadata)
	if err != nil {
		return err
	}
	// COALESCE keeps the stored value for any answer the person skipped.
	_, err = q.Exec(ctx, `
		INSERT INTO lead_preferences (lead_id, household_size, meals_per_week, dietary_preferences,
			meal_interests, cooking_frequency, delivery_area, metadata, packaging_preference)
		VALUES ($1, $2, $3, COALESCE($4::text[], '{}'), COALESCE($5::text[], '{}'), $6, $7, $8::jsonb, $9)
		ON CONFLICT (lead_id) DO UPDATE SET
			packaging_preference = COALESCE(EXCLUDED.packaging_preference, lead_preferences.packaging_preference),
			household_size      = COALESCE(EXCLUDED.household_size, lead_preferences.household_size),
			meals_per_week      = COALESCE(EXCLUDED.meals_per_week, lead_preferences.meals_per_week),
			dietary_preferences = COALESCE($4::text[], lead_preferences.dietary_preferences),
			meal_interests      = COALESCE($5::text[], lead_preferences.meal_interests),
			cooking_frequency   = COALESCE(EXCLUDED.cooking_frequency, lead_preferences.cooking_frequency),
			delivery_area       = COALESCE(EXCLUDED.delivery_area, lead_preferences.delivery_area),
			metadata            = lead_preferences.metadata || EXCLUDED.metadata,
			updated_at          = now()`,
		leadID, p.HouseholdSize, p.MealsPerWeek, p.Dietary, p.MealInterests, p.CookingFrequency,
		p.DeliveryArea, metadata, p.Packaging)
	return err
}

// Erase permanently deletes a lead and everything attached to it. Related rows
// go with it through ON DELETE CASCADE. It reports whether a lead was deleted.
func Erase(ctx context.Context, q db.Querier, id string) (bool, error) {
	tag, err := q.Exec(ctx, `DELETE FROM leads WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	// Queued email jobs reference the lead by id only and would fail harmlessly,
	// but there is no reason to keep them.
	if _, err := q.Exec(ctx, `DELETE FROM jobs WHERE payload->>'lead_id' = $1`, id); err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
