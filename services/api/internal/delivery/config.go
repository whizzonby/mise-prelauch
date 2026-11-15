package delivery

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
)

// ---------------------------------------------------------------- settings

// Settings are the business policies. They ship unset, and the API says
// plainly which capabilities are blocked until they are configured.
type Settings struct {
	ProofMethods               []string  `json:"proof_methods"`
	ProofPolicyRef             string    `json:"proof_policy_ref"`
	RescheduleCutoffHours      *int      `json:"reschedule_cutoff_hours"`
	FailureReasons             []string  `json:"failure_reasons"`
	AllowUnapprovedPartners    bool      `json:"allow_unapproved_partners"`
	UnapprovedPartnerPolicyRef string    `json:"unapproved_partner_policy_ref"`
	LabelCertified             bool      `json:"label_certified"`
	CustomerEmailsApproved     bool      `json:"customer_emails_approved"`
	// Eligible delivery dates: order week start (Monday) + min..max days.
	DeliveryOffsetMinDays *int      `json:"delivery_offset_min_days"`
	DeliveryOffsetMaxDays *int      `json:"delivery_offset_max_days"`
	DeliveryWeekRuleRef   string    `json:"delivery_week_rule_ref"`
	UpdatedAt             time.Time `json:"updated_at"`
	Blockers                   []string  `json:"blockers"`
}

// ProofMethodsAvailable are the methods that can be enabled. Photo proof is
// not among them: there is no secure evidence storage yet.
var ProofMethodsAvailable = []string{"confirmation_code", "recipient_acknowledgment", "staff_confirmation", "partner_confirmation"}

func (s *Service) Settings(ctx context.Context) (*Settings, error) { return s.settings(ctx, s.pool) }

func (s *Service) settings(ctx context.Context, q db.Querier) (*Settings, error) {
	var st Settings
	err := q.QueryRow(ctx, `SELECT proof_methods, proof_policy_ref, reschedule_cutoff_hours, failure_reasons, allow_unapproved_partners,
		unapproved_partner_policy_ref, label_certified, customer_emails_approved, delivery_offset_min_days, delivery_offset_max_days,
		delivery_week_rule_ref, updated_at FROM delivery_settings`).
		Scan(&st.ProofMethods, &st.ProofPolicyRef, &st.RescheduleCutoffHours, &st.FailureReasons, &st.AllowUnapprovedPartners,
			&st.UnapprovedPartnerPolicyRef, &st.LabelCertified, &st.CustomerEmailsApproved, &st.DeliveryOffsetMinDays, &st.DeliveryOffsetMaxDays,
			&st.DeliveryWeekRuleRef, &st.UpdatedAt)
	if err != nil {
		return nil, err
	}
	st.Blockers = []string{}
	if st.DeliveryOffsetMinDays == nil {
		st.Blockers = append(st.Blockers, "No delivery-week rule is approved: no delivery can be scheduled (which days after an order's week start may deliver it).")
	}
	if len(st.ProofMethods) == 0 {
		st.Blockers = append(st.Blockers, "No proof-of-delivery policy is configured: deliveries cannot be completed.")
	}
	if st.RescheduleCutoffHours == nil {
		st.Blockers = append(st.Blockers, "No reschedule policy: customer requests are collected but no new date is promised.")
	}
	if !st.LabelCertified {
		st.Blockers = append(st.Blockers, "Package labels are not certified for live use.")
	}
	if !st.CustomerEmailsApproved {
		st.Blockers = append(st.Blockers, "Customer delivery email wording is not approved: delivery emails are recorded as skipped, not sent.")
	}
	st.Blockers = append(st.Blockers, "Photo proof is unavailable: there is no secure evidence storage.")
	return &st, nil
}

// LabelCertified tells fulfillment whether labels may be used live.
func (s *Service) LabelCertified(ctx context.Context, q db.Querier) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `SELECT label_certified FROM delivery_settings`).Scan(&ok)
	return ok, err
}

type SettingsInput struct {
	ProofMethods               *[]string `json:"proof_methods"`
	ProofPolicyRef             *string   `json:"proof_policy_ref"`
	RescheduleCutoffHours      *int      `json:"reschedule_cutoff_hours"`
	ClearReschedulePolicy      bool      `json:"clear_reschedule_policy"`
	FailureReasons             *[]string `json:"failure_reasons"`
	AllowUnapprovedPartners    *bool     `json:"allow_unapproved_partners"`
	UnapprovedPartnerPolicyRef *string   `json:"unapproved_partner_policy_ref"`
	LabelCertified             *bool     `json:"label_certified"`
	CustomerEmailsApproved     *bool     `json:"customer_emails_approved"`
	DeliveryOffsetMinDays      *int      `json:"delivery_offset_min_days"`
	DeliveryOffsetMaxDays      *int      `json:"delivery_offset_max_days"`
	DeliveryWeekRuleRef        *string   `json:"delivery_week_rule_ref"`
	ClearDeliveryWeekRule      bool      `json:"clear_delivery_week_rule"`
}

func (s *Service) PutSettings(ctx context.Context, actor string, in SettingsInput) (*Settings, error) {
	err := s.tx(ctx, func(tx pgx.Tx) error {
		cur, err := s.settings(ctx, tx)
		if err != nil {
			return err
		}
		next := *cur
		if in.ProofMethods != nil {
			for _, m := range *in.ProofMethods {
				if m == "photo" {
					return fieldErr("proof_methods", "Photo proof cannot be enabled: there is no secure evidence storage.")
				}
				if !slices.Contains(ProofMethodsAvailable, m) {
					return fieldErr("proof_methods", "Unknown proof method "+m+".")
				}
			}
			next.ProofMethods = dedupe(*in.ProofMethods)
		}
		if in.ProofPolicyRef != nil {
			next.ProofPolicyRef = strings.TrimSpace(*in.ProofPolicyRef)
		}
		if len(next.ProofMethods) > 0 && next.ProofPolicyRef == "" {
			return fieldErr("proof_policy_ref", "Name the approved policy (document or decision) that sets these proof methods.")
		}
		if in.ClearReschedulePolicy {
			next.RescheduleCutoffHours = nil
		} else if in.RescheduleCutoffHours != nil {
			if *in.RescheduleCutoffHours < 0 || *in.RescheduleCutoffHours > 336 {
				return fieldErr("reschedule_cutoff_hours", "Use 0 to 336 hours.")
			}
			next.RescheduleCutoffHours = in.RescheduleCutoffHours
		}
		if in.FailureReasons != nil {
			rs := dedupe(*in.FailureReasons)
			if len(rs) == 0 || len(rs) > 30 {
				return fieldErr("failure_reasons", "Keep 1 to 30 failure reasons.")
			}
			for _, r := range rs {
				if !keyPattern.MatchString(r) {
					return fieldErr("failure_reasons", "Use lowercase keys like recipient_unavailable.")
				}
			}
			next.FailureReasons = rs
		}
		if in.UnapprovedPartnerPolicyRef != nil {
			next.UnapprovedPartnerPolicyRef = strings.TrimSpace(*in.UnapprovedPartnerPolicyRef)
		}
		if in.AllowUnapprovedPartners != nil {
			next.AllowUnapprovedPartners = *in.AllowUnapprovedPartners
		}
		if next.AllowUnapprovedPartners && next.UnapprovedPartnerPolicyRef == "" {
			return fieldErr("unapproved_partner_policy_ref", "Name the documented exception policy that allows unapproved partners.")
		}
		if in.LabelCertified != nil {
			next.LabelCertified = *in.LabelCertified
		}
		if in.CustomerEmailsApproved != nil {
			next.CustomerEmailsApproved = *in.CustomerEmailsApproved
		}
		if in.DeliveryWeekRuleRef != nil {
			next.DeliveryWeekRuleRef = strings.TrimSpace(*in.DeliveryWeekRuleRef)
		}
		if in.ClearDeliveryWeekRule {
			next.DeliveryOffsetMinDays, next.DeliveryOffsetMaxDays, next.DeliveryWeekRuleRef = nil, nil, ""
		} else if in.DeliveryOffsetMinDays != nil || in.DeliveryOffsetMaxDays != nil {
			if in.DeliveryOffsetMinDays == nil || in.DeliveryOffsetMaxDays == nil {
				return fieldErr("delivery_offset_min_days", "Give both the first and the last eligible day.")
			}
			lo, hi := *in.DeliveryOffsetMinDays, *in.DeliveryOffsetMaxDays
			if lo < 0 || hi < lo || hi > 27 {
				return fieldErr("delivery_offset_max_days", "Use days 0 to 27 after the order's week start, the first no later than the last.")
			}
			next.DeliveryOffsetMinDays, next.DeliveryOffsetMaxDays = &lo, &hi
		}
		if next.DeliveryOffsetMinDays != nil && runes(next.DeliveryWeekRuleRef) < 2 {
			return fieldErr("delivery_week_rule_ref", "Name the approved decision that sets which days deliver an order's week.")
		}
		if _, err := tx.Exec(ctx, `UPDATE delivery_settings SET proof_methods = $1, proof_policy_ref = $2, reschedule_cutoff_hours = $3,
			failure_reasons = $4, allow_unapproved_partners = $5, unapproved_partner_policy_ref = $6, label_certified = $7,
			customer_emails_approved = $8, updated_by = $9, delivery_offset_min_days = $10, delivery_offset_max_days = $11,
			delivery_week_rule_ref = $12, updated_at = now()`,
			next.ProofMethods, next.ProofPolicyRef, next.RescheduleCutoffHours, next.FailureReasons, next.AllowUnapprovedPartners,
			next.UnapprovedPartnerPolicyRef, next.LabelCertified, next.CustomerEmailsApproved, nilIfEmpty(actor),
			next.DeliveryOffsetMinDays, next.DeliveryOffsetMaxDays, next.DeliveryWeekRuleRef); err != nil {
			return err
		}
		return record(ctx, tx, actor, "delivery.settings_updated", "delivery_settings", "", map[string]any{
			"proof_methods": next.ProofMethods, "reschedule_cutoff_hours": next.RescheduleCutoffHours,
			"allow_unapproved_partners": next.AllowUnapprovedPartners, "label_certified": next.LabelCertified,
			"customer_emails_approved": next.CustomerEmailsApproved, "delivery_offset_min_days": next.DeliveryOffsetMinDays,
			"delivery_offset_max_days": next.DeliveryOffsetMaxDays, "delivery_week_rule_ref": next.DeliveryWeekRuleRef})
	})
	if err != nil {
		return nil, err
	}
	return s.Settings(ctx)
}

func dedupe(in []string) []string {
	out := []string{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// ---------------------------------------------------------------- zones

type Zone struct {
	ID           string    `json:"id"`
	Code         string    `json:"code"`
	Name         string    `json:"name"`
	Status       string    `json:"status"`
	FeePolicyRef string    `json:"fee_policy_ref"`
	Notes        string    `json:"notes"`
	Regions      []Region  `json:"regions"`
	PartnerCount int       `json:"partner_count"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Region struct {
	Country string `json:"country"`
	Region  string `json:"region"`
	Status  string `json:"status"`
}

func (s *Service) Zones(ctx context.Context) ([]Zone, error) {
	rows, err := s.pool.Query(ctx, `SELECT z.id::text, z.code, z.name, z.status, z.fee_policy_ref, z.notes,
		(SELECT count(*) FROM delivery_partner_zones pz WHERE pz.zone_id = z.id)::int, z.updated_at
		FROM delivery_service_zones z ORDER BY z.code`)
	if err != nil {
		return nil, err
	}
	zones, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Zone, error) {
		var z Zone
		err := r.Scan(&z.ID, &z.Code, &z.Name, &z.Status, &z.FeePolicyRef, &z.Notes, &z.PartnerCount, &z.UpdatedAt)
		z.Regions = []Region{}
		return z, err
	})
	if err != nil {
		return nil, err
	}
	rows, err = s.pool.Query(ctx, `SELECT r.zone_id::text, r.country, r.region, z.status FROM delivery_zone_regions r
		JOIN delivery_zones z ON z.country = r.country AND z.region = r.region ORDER BY r.region`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var zid string
		var rg Region
		if err := rows.Scan(&zid, &rg.Country, &rg.Region, &rg.Status); err != nil {
			return nil, err
		}
		for i := range zones {
			if zones[i].ID == zid {
				zones[i].Regions = append(zones[i].Regions, rg)
			}
		}
	}
	return zones, rows.Err()
}

// Coverage lists every customer-facing region with the zone covering it.
type RegionCoverage struct {
	Country  string  `json:"country"`
	Region   string  `json:"region"`
	Status   string  `json:"status"`
	ZoneID   *string `json:"zone_id"`
	ZoneCode *string `json:"zone_code"`
}

func (s *Service) RegionCoverage(ctx context.Context) ([]RegionCoverage, error) {
	rows, err := s.pool.Query(ctx, `SELECT z.country, z.region, z.status, sz.id::text, sz.code FROM delivery_zones z
		LEFT JOIN delivery_zone_regions r ON r.country = z.country AND r.region = z.region
		LEFT JOIN delivery_service_zones sz ON sz.id = r.zone_id ORDER BY z.region`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[RegionCoverage])
}

type ZoneInput struct {
	Code         string   `json:"code"`
	Name         string   `json:"name"`
	Status       string   `json:"status"`
	FeePolicyRef string   `json:"fee_policy_ref"`
	Notes        string   `json:"notes"`
	Regions      []string `json:"regions"` // region keys in country TT
}

// SaveZone creates (id "") or updates a zone and replaces its coverage rules.
// A region already in another zone is refused rather than moved silently.
func (s *Service) SaveZone(ctx context.Context, actor, id string, in ZoneInput) (*Zone, error) {
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	in.Name, in.Notes, in.FeePolicyRef = strings.TrimSpace(in.Name), strings.TrimSpace(in.Notes), strings.TrimSpace(in.FeePolicyRef)
	if in.Status == "" {
		in.Status = "draft"
	}
	var f []httpx.FieldError
	bad := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	if !zoneCode.MatchString(in.Code) {
		bad("code", "Use 2 to 20 capital letters, digits or dashes.")
	}
	if n := runes(in.Name); n < 1 || n > 80 {
		bad("name", "Name the zone.")
	}
	if !slices.Contains([]string{"draft", "active", "inactive"}, in.Status) {
		bad("status", "Choose draft, active or inactive.")
	}
	if runes(in.Notes) > 1000 || runes(in.FeePolicyRef) > 200 {
		bad("notes", "Keep notes under 1,000 characters.")
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if id == "" {
			err := tx.QueryRow(ctx, `INSERT INTO delivery_service_zones (code, name, status, fee_policy_ref, notes) VALUES ($1, $2, $3, $4, $5)
				ON CONFLICT (code) DO NOTHING RETURNING id::text`, in.Code, in.Name, in.Status, in.FeePolicyRef, in.Notes).Scan(&id)
			if errors.Is(err, pgx.ErrNoRows) {
				return conflict("duplicate", "A zone with this code exists.")
			}
			if err != nil {
				return err
			}
		} else {
			if !validID(id) {
				return httpx.ErrNotFound
			}
			tag, err := tx.Exec(ctx, `UPDATE delivery_service_zones SET code = $2, name = $3, status = $4, fee_policy_ref = $5, notes = $6,
				updated_at = now() WHERE id = $1`, id, in.Code, in.Name, in.Status, in.FeePolicyRef, in.Notes)
			if err != nil {
				if db.IsUniqueViolation(err, "delivery_service_zones_code_key") {
					return conflict("duplicate", "A zone with this code exists.")
				}
				return err
			}
			if tag.RowsAffected() == 0 {
				return httpx.ErrNotFound
			}
		}
		if in.Regions != nil {
			if _, err := tx.Exec(ctx, `DELETE FROM delivery_zone_regions WHERE zone_id = $1`, id); err != nil {
				return err
			}
			for _, rg := range dedupe(in.Regions) {
				var other string
				err := tx.QueryRow(ctx, `SELECT z.code FROM delivery_zone_regions r JOIN delivery_service_zones z ON z.id = r.zone_id
					WHERE r.country = 'TT' AND r.region = $1`, rg).Scan(&other)
				if err == nil {
					return fieldErr("regions", "Region "+rg+" is already covered by zone "+other+".")
				}
				if !errors.Is(err, pgx.ErrNoRows) {
					return err
				}
				if _, err := tx.Exec(ctx, `INSERT INTO delivery_zone_regions (zone_id, country, region) VALUES ($1, 'TT', $2)`, id, rg); err != nil {
					if db.IsForeignKeyViolation(err) {
						return fieldErr("regions", "Region "+rg+" is not a delivery region.")
					}
					return err
				}
			}
		}
		return record(ctx, tx, actor, "delivery.zone_saved", "delivery_zone", id, map[string]any{"code": in.Code, "status": in.Status, "regions": in.Regions})
	})
	if err != nil {
		return nil, err
	}
	zones, err := s.Zones(ctx)
	if err != nil {
		return nil, err
	}
	for _, z := range zones {
		if z.ID == id {
			return &z, nil
		}
	}
	return nil, httpx.ErrNotFound
}

// ---------------------------------------------------------------- calendar

type ServiceDay struct {
	ID       string     `json:"id"`
	Date     string     `json:"date"`
	ZoneID   *string    `json:"zone_id"`
	ZoneCode *string    `json:"zone_code"`
	Status   string     `json:"status"`
	CutoffAt *time.Time `json:"cutoff_at"`
	Notes    string     `json:"notes"`
	Windows  []Window   `json:"windows"`
}

type Window struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	StartsAt string `json:"starts_at"`
	EndsAt   string `json:"ends_at"`
	Status   string `json:"status"`
}

// Calendar lists service days from..from+days.
func (s *Service) Calendar(ctx context.Context, from string, days int) ([]ServiceDay, error) {
	start := s.today()
	if from != "" {
		t, ok := parseDay(from)
		if !ok {
			return nil, fieldErr("from", "Use a date like 2026-11-16.")
		}
		start = t
	}
	if days <= 0 || days > 92 {
		days = 28
	}
	rows, err := s.pool.Query(ctx, `SELECT d.id::text, d.service_date::text, d.zone_id::text, z.code, d.status, d.cutoff_at, d.notes
		FROM delivery_service_days d LEFT JOIN delivery_service_zones z ON z.id = d.zone_id
		WHERE d.service_date BETWEEN $1 AND $2 ORDER BY d.service_date, z.code NULLS FIRST`, start, start.AddDate(0, 0, days-1))
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ServiceDay, error) {
		var d ServiceDay
		err := r.Scan(&d.ID, &d.Date, &d.ZoneID, &d.ZoneCode, &d.Status, &d.CutoffAt, &d.Notes)
		d.Windows = []Window{}
		return d, err
	})
	if err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Windows, err = s.windows(ctx, s.pool, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Service) windows(ctx context.Context, q db.Querier, dayID string) ([]Window, error) {
	rows, err := q.Query(ctx, `SELECT id::text, label, to_char(starts_at, 'HH24:MI'), to_char(ends_at, 'HH24:MI'), status
		FROM delivery_windows WHERE service_day_id = $1 ORDER BY starts_at`, dayID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Window])
}

type ServiceDayInput struct {
	Date     string        `json:"date"`
	ZoneID   string        `json:"zone_id"`
	Status   string        `json:"status"`
	CutoffAt *time.Time    `json:"cutoff_at"`
	Notes    string        `json:"notes"`
	Windows  []WindowInput `json:"windows"`
}

type WindowInput struct {
	Label    string `json:"label"`
	StartsAt string `json:"starts_at"`
	EndsAt   string `json:"ends_at"`
	Status   string `json:"status"`
}

// SaveServiceDay records an approved delivery date (or a closure) for every
// zone or one zone, with its windows. Windows already used by deliveries
// are kept.
func (s *Service) SaveServiceDay(ctx context.Context, actor string, in ServiceDayInput) (*ServiceDay, error) {
	d, ok := parseDay(in.Date)
	if !ok {
		return nil, fieldErr("date", "Use a date like 2026-11-19.")
	}
	if in.Status != "open" && in.Status != "closed" {
		return nil, fieldErr("status", "Choose open or closed.")
	}
	if in.ZoneID != "" && !validID(in.ZoneID) {
		return nil, fieldErr("zone_id", "Choose a zone.")
	}
	in.Notes = strings.TrimSpace(in.Notes)
	if runes(in.Notes) > 500 {
		return nil, fieldErr("notes", "Keep notes under 500 characters.")
	}
	for _, w := range in.Windows {
		st, err1 := time.Parse("15:04", w.StartsAt)
		en, err2 := time.Parse("15:04", w.EndsAt)
		if err1 != nil || err2 != nil || !en.After(st) {
			return nil, fieldErr("windows", "Each window needs a start and a later end, like 09:00 and 12:00.")
		}
		if l := runes(strings.TrimSpace(w.Label)); l < 1 || l > 60 {
			return nil, fieldErr("windows", "Label each window.")
		}
		if w.Status != "" && w.Status != "draft" && w.Status != "approved" {
			return nil, fieldErr("windows", "A window is draft or approved.")
		}
	}
	var id string
	err := s.tx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO delivery_service_days (service_date, zone_id, status, cutoff_at, notes, created_by) VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (service_date, zone_id) DO UPDATE SET status = EXCLUDED.status, cutoff_at = EXCLUDED.cutoff_at, notes = EXCLUDED.notes,
				updated_at = now()
			RETURNING id::text`, d, nilIfEmpty(in.ZoneID), in.Status, in.CutoffAt, in.Notes, nilIfEmpty(actor)).Scan(&id)
		if err != nil {
			if db.IsForeignKeyViolation(err) {
				return fieldErr("zone_id", "Unknown zone.")
			}
			return err
		}
		if in.Windows != nil {
			if _, err := tx.Exec(ctx, `DELETE FROM delivery_windows w WHERE w.service_day_id = $1
				AND NOT EXISTS (SELECT 1 FROM deliveries d WHERE d.window_id = w.id)`, id); err != nil {
				return err
			}
			for _, w := range in.Windows {
				st := w.Status
				if st == "" {
					st = "draft"
				}
				if _, err := tx.Exec(ctx, `INSERT INTO delivery_windows (service_day_id, label, starts_at, ends_at, status) VALUES ($1, $2, $3, $4, $5)
					ON CONFLICT (service_day_id, starts_at, ends_at) DO UPDATE SET label = EXCLUDED.label, status = EXCLUDED.status`,
					id, strings.TrimSpace(w.Label), w.StartsAt, w.EndsAt, st); err != nil {
					return err
				}
			}
		}
		return record(ctx, tx, actor, "delivery.service_day_saved", "delivery_service_day", id, map[string]any{"date": in.Date, "status": in.Status, "zone_id": in.ZoneID})
	})
	if err != nil {
		return nil, err
	}
	days, err := s.Calendar(ctx, in.Date, 1)
	if err != nil {
		return nil, err
	}
	for _, x := range days {
		if x.ID == id {
			return &x, nil
		}
	}
	return nil, httpx.ErrNotFound
}

// serviceDayOpen decides whether a zone delivers on a date: a closure for
// the zone or for all zones wins; otherwise an open day (zone or all) is
// needed. Without any configured day, the date is not a service day.
func serviceDayOpen(ctx context.Context, q db.Querier, date time.Time, zoneID *string) (bool, string, error) {
	rows, err := q.Query(ctx, `SELECT zone_id::text, status FROM delivery_service_days WHERE service_date = $1 AND (zone_id IS NULL OR zone_id = $2)`,
		date, zoneID)
	if err != nil {
		return false, "", err
	}
	defer rows.Close()
	open := false
	for rows.Next() {
		var z *string
		var st string
		if err := rows.Scan(&z, &st); err != nil {
			return false, "", err
		}
		if st == "closed" {
			return false, "Deliveries are closed on " + date.Format(day) + ".", nil
		}
		open = true
	}
	if err := rows.Err(); err != nil {
		return false, "", err
	}
	if !open {
		return false, date.Format(day) + " is not an approved delivery date.", nil
	}
	return true, "", nil
}

// ---------------------------------------------------------------- capacity

type Capacity struct {
	ID          string  `json:"id"`
	Date        string  `json:"date"`
	ZoneID      *string `json:"zone_id"`
	ZoneCode    *string `json:"zone_code"`
	WindowID    *string `json:"window_id"`
	WindowLabel *string `json:"window_label"`
	PartnerID   *string `json:"partner_id"`
	PartnerName *string `json:"partner_name"`
	CourierID   *string `json:"courier_id"`
	CourierName *string `json:"courier_name"`
	Capacity    int     `json:"capacity"`
	Assigned    int     `json:"assigned"`
	Remaining   int     `json:"remaining"`
	Notes       string  `json:"notes"`
}

// capacityUse counts active stops matching a capacity scope.
const capacityUse = `(SELECT count(*) FROM dispatch_stops st JOIN dispatch_runs r ON r.id = st.run_id JOIN deliveries d ON d.id = st.delivery_id
	WHERE st.status = 'active' AND r.service_date = c.service_date
	AND (c.zone_id IS NULL OR d.zone_id = c.zone_id) AND (c.window_id IS NULL OR d.window_id = c.window_id)
	AND (c.partner_id IS NULL OR r.partner_id = c.partner_id) AND (c.courier_id IS NULL OR r.courier_id = c.courier_id))::int`

func (s *Service) CapacityFor(ctx context.Context, date string) ([]Capacity, error) {
	d, ok := parseDay(date)
	if !ok {
		return nil, fieldErr("date", "Use a date like 2026-11-19.")
	}
	rows, err := s.pool.Query(ctx, `SELECT c.id::text, c.service_date::text, c.zone_id::text, z.code, c.window_id::text, w.label,
		c.partner_id::text, p.name, c.courier_id::text, co.name, c.capacity, `+capacityUse+`, 0, c.notes
		FROM delivery_capacity c LEFT JOIN delivery_service_zones z ON z.id = c.zone_id LEFT JOIN delivery_windows w ON w.id = c.window_id
		LEFT JOIN delivery_partners p ON p.id = c.partner_id LEFT JOIN delivery_couriers co ON co.id = c.courier_id
		WHERE c.service_date = $1 ORDER BY z.code NULLS FIRST, p.name NULLS FIRST`, d)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Capacity])
	for i := range out {
		out[i].Remaining = out[i].Capacity - out[i].Assigned
	}
	return out, err
}

type CapacityInput struct {
	Date      string `json:"date"`
	ZoneID    string `json:"zone_id"`
	WindowID  string `json:"window_id"`
	PartnerID string `json:"partner_id"`
	CourierID string `json:"courier_id"`
	Capacity  *int   `json:"capacity"`
	Notes     string `json:"notes"`
}

// SetCapacity sets capacity for one scope. Lowering it below what is
// already assigned is refused: unassign first.
func (s *Service) SetCapacity(ctx context.Context, actor string, in CapacityInput) ([]Capacity, error) {
	d, ok := parseDay(in.Date)
	if !ok {
		return nil, fieldErr("date", "Use a date like 2026-11-19.")
	}
	if in.Capacity == nil || *in.Capacity < 0 || *in.Capacity > 10000 {
		return nil, fieldErr("capacity", "Enter how many deliveries (0 to 10,000).")
	}
	for f, v := range map[string]string{"zone_id": in.ZoneID, "window_id": in.WindowID, "partner_id": in.PartnerID, "courier_id": in.CourierID} {
		if v != "" && !validID(v) {
			return nil, fieldErr(f, "Not a valid id.")
		}
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var id string
		err := tx.QueryRow(ctx, `
			INSERT INTO delivery_capacity (service_date, zone_id, window_id, partner_id, courier_id, capacity, notes, updated_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (service_date, zone_id, window_id, partner_id, courier_id) DO UPDATE SET capacity = EXCLUDED.capacity,
				notes = EXCLUDED.notes, updated_by = EXCLUDED.updated_by, updated_at = now()
			RETURNING id::text`, d, nilIfEmpty(in.ZoneID), nilIfEmpty(in.WindowID), nilIfEmpty(in.PartnerID), nilIfEmpty(in.CourierID),
			*in.Capacity, strings.TrimSpace(in.Notes), nilIfEmpty(actor)).Scan(&id)
		if err != nil {
			if db.IsForeignKeyViolation(err) {
				return fieldErr("scope", "Unknown zone, window, partner or courier.")
			}
			return err
		}
		var used int
		if err := tx.QueryRow(ctx, `SELECT `+capacityUse+` FROM delivery_capacity c WHERE c.id = $1`, id).Scan(&used); err != nil {
			return err
		}
		if used > *in.Capacity {
			return unprocessable("below_assigned", "Capacity cannot be lower than the deliveries already assigned in this scope.")
		}
		return record(ctx, tx, actor, "delivery.capacity_set", "delivery_capacity", id, map[string]any{"date": in.Date, "capacity": *in.Capacity})
	})
	if err != nil {
		return nil, err
	}
	return s.CapacityFor(ctx, in.Date)
}

// checkCapacity locks every capacity row matching the delivery and run and
// refuses one more stop where any is full. With no matching row, capacity
// is unknown, and the caller needs an explicit override.
func checkCapacity(ctx context.Context, tx pgx.Tx, date time.Time, zoneID, windowID *string, partnerID string, courierID *string) (known bool, full string, err error) {
	rows, err := tx.Query(ctx, `SELECT c.id::text, c.capacity, COALESCE(z.code, 'all zones') FROM delivery_capacity c
		LEFT JOIN delivery_service_zones z ON z.id = c.zone_id
		WHERE c.service_date = $1 AND (c.zone_id IS NULL OR c.zone_id = $2) AND (c.window_id IS NULL OR c.window_id = $3)
		AND (c.partner_id IS NULL OR c.partner_id = $4) AND (c.courier_id IS NULL OR c.courier_id = $5)
		ORDER BY c.id FOR UPDATE OF c`, date, zoneID, windowID, partnerID, courierID)
	if err != nil {
		return false, "", err
	}
	type scope struct {
		id   string
		cap  int
		zone string
	}
	var scopes []scope
	for rows.Next() {
		var sc scope
		if err := rows.Scan(&sc.id, &sc.cap, &sc.zone); err != nil {
			rows.Close()
			return false, "", err
		}
		scopes = append(scopes, sc)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, "", err
	}
	for _, sc := range scopes {
		var used int
		if err := tx.QueryRow(ctx, `SELECT `+capacityUse+` FROM delivery_capacity c WHERE c.id = $1`, sc.id).Scan(&used); err != nil {
			return false, "", err
		}
		if used >= sc.cap {
			return true, "Capacity is full for " + date.Format(day) + " (" + sc.zone + "): " + itoa(used) + " of " + itoa(sc.cap) + ".", nil
		}
	}
	return len(scopes) > 0, "", nil
}

// eligibleDate applies the approved delivery-week rule: an order of the week
// starting week may be delivered from week+min to week+max days. Without an
// approved rule nothing can be scheduled: delivery days are a business
// decision that is never assumed.
func eligibleDate(st *Settings, week, date time.Time) error {
	if st.DeliveryOffsetMinDays == nil || st.DeliveryOffsetMaxDays == nil {
		return unprocessable("delivery_week_rule_missing",
			"No delivery-week rule is approved, so deliveries cannot be scheduled. Set it in delivery settings once the business decides it.")
	}
	first, last := week.AddDate(0, 0, *st.DeliveryOffsetMinDays), week.AddDate(0, 0, *st.DeliveryOffsetMaxDays)
	if date.Before(first) || date.After(last) {
		return unprocessable("outside_delivery_week", "This order (week of "+week.Format(day)+") can only be delivered from "+
			first.Format(day)+" to "+last.Format(day)+" under rule "+st.DeliveryWeekRuleRef+".")
	}
	return nil
}
