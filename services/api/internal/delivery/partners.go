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

// Criteria a partner is qualified against. Each is recorded as met or not
// with evidence; approval needs all of them and a signed agreement. The
// criteria name what must be shown, not the business's contractual terms.
var Criteria = []string{"contract", "service_coverage", "handling_capability", "reliability", "escalation_process",
	"delivery_evidence", "handling_procedures"}

var integrationModes = []string{"manual", "file", "api", "internal_fleet"}
var agreementStatuses = []string{"none", "negotiating", "signed", "expired", "terminated"}

type Partner struct {
	ID                string      `json:"id"`
	Name              string      `json:"name"`
	ContactName       string      `json:"contact_name"`
	Phone             string      `json:"phone"`
	Email             string      `json:"email"`
	EscalationContact string      `json:"escalation_contact"`
	IntegrationMode   string      `json:"integration_mode"`
	AgreementStatus   string      `json:"agreement_status"`
	AgreementRef      string      `json:"agreement_ref"`
	Status            string      `json:"status"`
	Capabilities      []string    `json:"capabilities"`
	SupportedWindows  string      `json:"supported_windows"`
	CapacityNote      string      `json:"capacity_note"`
	BillingReference  string      `json:"billing_reference"`
	Notes             string      `json:"notes"`
	Demo              bool        `json:"demo"`
	Version           int         `json:"version"`
	ApprovedAt        *time.Time  `json:"approved_at"`
	ZoneIDs           []string    `json:"zone_ids"`
	CourierCount      int         `json:"courier_count"`
	Assignable        bool        `json:"assignable"`
	Criteria          []Criterion `json:"criteria,omitempty"`
	History           []StatusEvt `json:"history,omitempty"`
	AdapterSupports   []string    `json:"adapter_supports"`
}

type Criterion struct {
	Criterion  string     `json:"criterion"`
	Met        *bool      `json:"met"`
	Evidence   string     `json:"evidence"`
	RecordedAt *time.Time `json:"recorded_at"`
}

type StatusEvt struct {
	From      *string   `json:"from"`
	To        string    `json:"to"`
	Reason    *string   `json:"reason"`
	ActorName *string   `json:"actor_name"`
	At        time.Time `json:"at"`
}

const partnerCols = `p.id::text, p.name, p.contact_name, p.phone, p.email, p.escalation_contact, p.integration_mode, p.agreement_status,
	p.agreement_ref, p.status, p.capabilities, p.supported_windows, p.capacity_note, p.billing_reference, p.notes, p.demo, p.version,
	p.approved_at, COALESCE((SELECT array_agg(zone_id::text ORDER BY zone_id) FROM delivery_partner_zones z WHERE z.partner_id = p.id), '{}'),
	(SELECT count(*) FROM delivery_couriers c WHERE c.partner_id = p.id AND c.status = 'active')::int`

func scanPartner(r pgx.Row) (*Partner, error) {
	var p Partner
	err := r.Scan(&p.ID, &p.Name, &p.ContactName, &p.Phone, &p.Email, &p.EscalationContact, &p.IntegrationMode, &p.AgreementStatus,
		&p.AgreementRef, &p.Status, &p.Capabilities, &p.SupportedWindows, &p.CapacityNote, &p.BillingReference, &p.Notes, &p.Demo, &p.Version,
		&p.ApprovedAt, &p.ZoneIDs, &p.CourierCount)
	p.Assignable = p.Status == "approved"
	return &p, err
}

func (s *Service) adapterFor(mode string) Carrier {
	if c, ok := s.Carriers[mode]; ok {
		return c
	}
	return nil
}

func (s *Service) decorate(p *Partner) {
	p.AdapterSupports = []string{}
	if c := s.adapterFor(p.IntegrationMode); c != nil {
		for _, cp := range c.Capabilities() {
			p.AdapterSupports = append(p.AdapterSupports, string(cp))
		}
	}
}

func (s *Service) Partners(ctx context.Context) ([]Partner, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+partnerCols+` FROM delivery_partners p ORDER BY p.status = 'retired', p.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Partner{}
	for rows.Next() {
		p, err := scanPartner(rows)
		if err != nil {
			return nil, err
		}
		s.decorate(p)
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *Service) Partner(ctx context.Context, id string) (*Partner, error) {
	if !validID(id) {
		return nil, httpx.ErrNotFound
	}
	p, err := scanPartner(s.pool.QueryRow(ctx, `SELECT `+partnerCols+` FROM delivery_partners p WHERE p.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	s.decorate(p)
	met := map[string]Criterion{}
	rows, err := s.pool.Query(ctx, `SELECT criterion, met, evidence, recorded_at FROM delivery_partner_criteria WHERE partner_id = $1`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c Criterion
		var m bool
		var at time.Time
		if err := rows.Scan(&c.Criterion, &m, &c.Evidence, &at); err != nil {
			rows.Close()
			return nil, err
		}
		c.Met, c.RecordedAt = &m, &at
		met[c.Criterion] = c
	}
	rows.Close()
	for _, k := range Criteria {
		c, ok := met[k]
		if !ok {
			c = Criterion{Criterion: k}
		}
		p.Criteria = append(p.Criteria, c)
	}
	rows, err = s.pool.Query(ctx, `SELECT e.from_status, e.to_status, e.reason, NULLIF(trim(COALESCE(u.first_name, '') || ' ' || COALESCE(u.last_name, '')), ''), e.created_at
		FROM delivery_partner_events e LEFT JOIN users u ON u.id = e.actor_user_id WHERE e.partner_id = $1 ORDER BY e.id`, id)
	if err != nil {
		return nil, err
	}
	p.History, err = pgx.CollectRows(rows, pgx.RowToStructByPos[StatusEvt])
	return p, err
}

type PartnerInput struct {
	Name              string   `json:"name"`
	ContactName       string   `json:"contact_name"`
	Phone             string   `json:"phone"`
	Email             string   `json:"email"`
	EscalationContact string   `json:"escalation_contact"`
	IntegrationMode   string   `json:"integration_mode"`
	AgreementStatus   string   `json:"agreement_status"`
	AgreementRef      string   `json:"agreement_ref"`
	Capabilities      []string `json:"capabilities"`
	SupportedWindows  string   `json:"supported_windows"`
	CapacityNote      string   `json:"capacity_note"`
	BillingReference  string   `json:"billing_reference"`
	Notes             string   `json:"notes"`
	ZoneIDs           []string `json:"zone_ids"`
	Version           *int     `json:"version"`
	Demo              bool     `json:"-"`
}

func (in *PartnerInput) validate() error {
	for _, p := range []*string{&in.Name, &in.ContactName, &in.Phone, &in.Email, &in.EscalationContact, &in.AgreementRef, &in.SupportedWindows,
		&in.CapacityNote, &in.BillingReference, &in.Notes} {
		*p = strings.TrimSpace(*p)
	}
	if in.IntegrationMode == "" {
		in.IntegrationMode = "manual"
	}
	if in.AgreementStatus == "" {
		in.AgreementStatus = "none"
	}
	var f []httpx.FieldError
	bad := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	if n := runes(in.Name); n < 1 || n > 120 {
		bad("name", "Name the partner.")
	}
	if !slices.Contains(integrationModes, in.IntegrationMode) {
		bad("integration_mode", "Choose manual, file, API or internal fleet.")
	}
	if !slices.Contains(agreementStatuses, in.AgreementStatus) {
		bad("agreement_status", "Choose an agreement status.")
	}
	if in.AgreementStatus == "signed" && in.AgreementRef == "" {
		bad("agreement_ref", "Reference the signed agreement.")
	}
	if runes(in.Notes) > 2000 || runes(in.EscalationContact) > 300 || runes(in.SupportedWindows) > 300 || runes(in.CapacityNote) > 300 {
		bad("notes", "Keep notes shorter.")
	}
	if runes(in.Phone) > 40 || runes(in.Email) > 200 || runes(in.ContactName) > 120 || runes(in.AgreementRef) > 200 || runes(in.BillingReference) > 120 {
		bad("contact", "A contact field is too long.")
	}
	for _, z := range in.ZoneIDs {
		if !validID(z) {
			bad("zone_ids", "Choose zones from the list.")
		}
	}
	if len(f) > 0 {
		return httpx.ValidationError(f)
	}
	in.Capabilities = dedupe(in.Capabilities)
	return nil
}

// SavePartner creates (id "") or edits a partner. Changing the agreement
// away from signed suspends an approved partner: it is no longer assignable.
func (s *Service) SavePartner(ctx context.Context, actor, id string, in PartnerInput) (*Partner, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if id == "" {
			if err := tx.QueryRow(ctx, `
				INSERT INTO delivery_partners (name, contact_name, phone, email, escalation_contact, integration_mode, agreement_status, agreement_ref,
					capabilities, supported_windows, capacity_note, billing_reference, notes, demo, created_by)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15) RETURNING id::text`,
				in.Name, in.ContactName, in.Phone, in.Email, in.EscalationContact, in.IntegrationMode, in.AgreementStatus, in.AgreementRef,
				in.Capabilities, in.SupportedWindows, in.CapacityNote, in.BillingReference, in.Notes, in.Demo, nilIfEmpty(actor)).Scan(&id); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO delivery_partner_events (partner_id, from_status, to_status, reason, actor_user_id) VALUES ($1, NULL, 'draft', 'Created', $2)`,
				id, nilIfEmpty(actor)); err != nil {
				return err
			}
		} else {
			if !validID(id) {
				return httpx.ErrNotFound
			}
			var status string
			var version int
			err := tx.QueryRow(ctx, `SELECT status, version FROM delivery_partners WHERE id = $1 FOR UPDATE`, id).Scan(&status, &version)
			if errors.Is(err, pgx.ErrNoRows) {
				return httpx.ErrNotFound
			}
			if err != nil {
				return err
			}
			if in.Version != nil && *in.Version != version {
				return conflict("stale", "This partner changed since you loaded it. Reload and try again.")
			}
			next := status
			if status == "approved" && in.AgreementStatus != "signed" {
				next = "suspended"
			}
			if _, err := tx.Exec(ctx, `UPDATE delivery_partners SET name = $2, contact_name = $3, phone = $4, email = $5, escalation_contact = $6,
				integration_mode = $7, agreement_status = $8, agreement_ref = $9, capabilities = $10, supported_windows = $11, capacity_note = $12,
				billing_reference = $13, notes = $14, status = $15, version = version + 1, updated_at = now() WHERE id = $1`,
				id, in.Name, in.ContactName, in.Phone, in.Email, in.EscalationContact, in.IntegrationMode, in.AgreementStatus, in.AgreementRef,
				in.Capabilities, in.SupportedWindows, in.CapacityNote, in.BillingReference, in.Notes, next); err != nil {
				return err
			}
			if next != status {
				if _, err := tx.Exec(ctx, `INSERT INTO delivery_partner_events (partner_id, from_status, to_status, reason, actor_user_id) VALUES ($1, $2, $3, $4, $5)`,
					id, status, next, "The agreement is no longer signed.", nilIfEmpty(actor)); err != nil {
					return err
				}
			}
		}
		if in.ZoneIDs != nil {
			if _, err := tx.Exec(ctx, `DELETE FROM delivery_partner_zones WHERE partner_id = $1`, id); err != nil {
				return err
			}
			for _, z := range dedupe(in.ZoneIDs) {
				if _, err := tx.Exec(ctx, `INSERT INTO delivery_partner_zones (partner_id, zone_id) VALUES ($1, $2)`, id, z); err != nil {
					if db.IsForeignKeyViolation(err) {
						return fieldErr("zone_ids", "Unknown zone.")
					}
					return err
				}
			}
		}
		return record(ctx, tx, actor, "delivery.partner_saved", "delivery_partner", id, map[string]any{"name": in.Name,
			"agreement_status": in.AgreementStatus, "integration_mode": in.IntegrationMode})
	})
	if err != nil {
		return nil, err
	}
	return s.Partner(ctx, id)
}

type CriterionInput struct {
	Criterion string `json:"criterion"`
	Met       bool   `json:"met"`
	Evidence  string `json:"evidence"`
}

// RecordCriterion records qualification evidence for one criterion.
func (s *Service) RecordCriterion(ctx context.Context, actor, id string, in CriterionInput) (*Partner, error) {
	in.Evidence = strings.TrimSpace(in.Evidence)
	if !slices.Contains(Criteria, in.Criterion) {
		return nil, fieldErr("criterion", "Unknown criterion.")
	}
	if in.Met && runes(in.Evidence) < 3 || runes(in.Evidence) > 1000 {
		return nil, fieldErr("evidence", "Describe the evidence (3 to 1,000 characters).")
	}
	if !validID(id) {
		return nil, httpx.ErrNotFound
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO delivery_partner_criteria (partner_id, criterion, met, evidence, recorded_by)
			SELECT id, $2, $3, $4, $5 FROM delivery_partners WHERE id = $1
			ON CONFLICT (partner_id, criterion) DO UPDATE SET met = EXCLUDED.met, evidence = EXCLUDED.evidence, recorded_by = EXCLUDED.recorded_by,
				recorded_at = now()`, id, in.Criterion, in.Met, in.Evidence, nilIfEmpty(actor))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		// Withdrawing a criterion from an approved partner suspends it.
		if !in.Met {
			if _, err := s.setPartnerStatus(ctx, tx, actor, id, "suspended", "Qualification withdrawn: "+Human(in.Criterion), true); err != nil {
				return err
			}
		}
		return record(ctx, tx, actor, "delivery.partner_criterion", "delivery_partner", id, map[string]any{"criterion": in.Criterion, "met": in.Met})
	})
	if err != nil {
		return nil, err
	}
	return s.Partner(ctx, id)
}

// PartnerStatusInput moves a partner through its approval workflow.
type PartnerStatusInput struct {
	Status string `json:"status"` // pending_approval, approved, suspended, retired, draft
	Reason string `json:"reason"`
}

var partnerMoves = map[string][]string{
	"draft":            {"pending_approval", "retired"},
	"pending_approval": {"approved", "draft", "retired"},
	"approved":         {"suspended", "retired"},
	"suspended":        {"pending_approval", "retired"},
	"retired":          {},
}

// SetPartnerStatus applies an approval decision. Approval needs a signed
// agreement and every criterion met; nothing is assumed about a company.
func (s *Service) SetPartnerStatus(ctx context.Context, actor, id string, in PartnerStatusInput) (*Partner, error) {
	why, err := reason(in.Reason, 3, 1000)
	if err != nil {
		return nil, err
	}
	err = s.tx(ctx, func(tx pgx.Tx) error {
		_, err := s.setPartnerStatus(ctx, tx, actor, id, in.Status, why, false)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.Partner(ctx, id)
}

func (s *Service) setPartnerStatus(ctx context.Context, tx pgx.Tx, actor, id, to, why string, onlyIfApproved bool) (bool, error) {
	var status, agreement string
	err := tx.QueryRow(ctx, `SELECT status, agreement_status FROM delivery_partners WHERE id = $1 FOR UPDATE`, id).Scan(&status, &agreement)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, httpx.ErrNotFound
	}
	if err != nil {
		return false, err
	}
	if onlyIfApproved && status != "approved" {
		return false, nil
	}
	if !slices.Contains(partnerMoves[status], to) {
		return false, conflict("invalid_transition", "A partner that is "+Human(status)+" cannot become "+Human(to)+".")
	}
	if to == "approved" {
		if agreement != "signed" {
			return false, unprocessable("not_qualified", "A partner needs a signed agreement before approval.")
		}
		var met int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM delivery_partner_criteria WHERE partner_id = $1 AND met`, id).Scan(&met); err != nil {
			return false, err
		}
		if met < len(Criteria) {
			return false, unprocessable("not_qualified", "Every qualification criterion must be met with evidence before approval.")
		}
	}
	extra := ""
	if to == "approved" {
		extra = `, approved_by = $3, approved_at = now()`
	}
	args := []any{id, to}
	if extra != "" {
		args = append(args, nilIfEmpty(actor))
	}
	if _, err := tx.Exec(ctx, `UPDATE delivery_partners SET status = $2, version = version + 1, updated_at = now()`+extra+` WHERE id = $1`, args...); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO delivery_partner_events (partner_id, from_status, to_status, reason, actor_user_id) VALUES ($1, $2, $3, $4, $5)`,
		id, status, to, why, nilIfEmpty(actor)); err != nil {
		return false, err
	}
	return true, record(ctx, tx, actor, "delivery.partner_status", "delivery_partner", id, map[string]any{"from": status, "to": to, "reason": why})
}

// ---------------------------------------------------------------- couriers

type Courier struct {
	ID          string    `json:"id"`
	PartnerID   string    `json:"partner_id"`
	PartnerName string    `json:"partner_name"`
	Name        string    `json:"name"`
	Phone       string    `json:"phone"`
	UserID      *string   `json:"user_id"`
	UserEmail   *string   `json:"user_email"`
	ZoneIDs     []string  `json:"zone_ids"`
	Status      string    `json:"status"`
	Notes       string    `json:"notes"`
	Demo        bool      `json:"demo"`
	Runs        int       `json:"runs"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (s *Service) Couriers(ctx context.Context) ([]Courier, error) {
	rows, err := s.pool.Query(ctx, `SELECT c.id::text, c.partner_id::text, p.name, c.name, c.phone, c.user_id::text, u.email,
		(SELECT COALESCE(array_agg(z::text), '{}') FROM unnest(c.zone_ids) z), c.status, c.notes, c.demo,
		(SELECT count(*) FROM dispatch_runs r WHERE r.courier_id = c.id)::int, c.updated_at
		FROM delivery_couriers c JOIN delivery_partners p ON p.id = c.partner_id LEFT JOIN users u ON u.id = c.user_id
		ORDER BY c.status, c.name`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Courier])
}

type CourierInput struct {
	PartnerID string   `json:"partner_id"`
	Name      string   `json:"name"`
	Phone     string   `json:"phone"`
	UserEmail string   `json:"user_email"` // links a staff login with the courier role
	ZoneIDs   []string `json:"zone_ids"`
	Status    string   `json:"status"`
	Notes     string   `json:"notes"`
	Demo      bool     `json:"-"`
}

// SaveCourier creates (id "") or edits a courier. A linked login must hold
// the courier role, so a courier sees only their own runs.
func (s *Service) SaveCourier(ctx context.Context, actor, id string, in CourierInput) (*Courier, error) {
	in.Name, in.Phone, in.Notes = strings.TrimSpace(in.Name), strings.TrimSpace(in.Phone), strings.TrimSpace(in.Notes)
	in.UserEmail = strings.ToLower(strings.TrimSpace(in.UserEmail))
	if in.Status == "" {
		in.Status = "active"
	}
	if n := runes(in.Name); n < 1 || n > 120 {
		return nil, fieldErr("name", "Name the courier.")
	}
	if !validID(in.PartnerID) {
		return nil, fieldErr("partner_id", "Choose the partner the courier works for.")
	}
	if in.Status != "active" && in.Status != "inactive" {
		return nil, fieldErr("status", "Choose active or inactive.")
	}
	if runes(in.Phone) > 40 || runes(in.Notes) > 1000 {
		return nil, fieldErr("notes", "A field is too long.")
	}
	for _, z := range in.ZoneIDs {
		if !validID(z) {
			return nil, fieldErr("zone_ids", "Choose zones from the list.")
		}
	}
	if in.ZoneIDs == nil {
		in.ZoneIDs = []string{}
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var userID *string
		if in.UserEmail != "" {
			var uid string
			err := tx.QueryRow(ctx, `SELECT u.id::text FROM users u JOIN user_roles r ON r.user_id = u.id
				WHERE u.email_canonical = $1 AND r.role_key = 'courier'`, in.UserEmail).Scan(&uid)
			if errors.Is(err, pgx.ErrNoRows) {
				return fieldErr("user_email", "No staff account with the courier role has this email.")
			}
			if err != nil {
				return err
			}
			userID = &uid
		}
		var err error
		if id == "" {
			err = tx.QueryRow(ctx, `INSERT INTO delivery_couriers (partner_id, name, phone, user_id, zone_ids, status, notes, demo)
				VALUES ($1, $2, $3, $4, $5::uuid[], $6, $7, $8) RETURNING id::text`, in.PartnerID, in.Name, in.Phone, userID, in.ZoneIDs, in.Status, in.Notes, in.Demo).Scan(&id)
		} else {
			if !validID(id) {
				return httpx.ErrNotFound
			}
			var tag interface{ RowsAffected() int64 }
			tag, err = tx.Exec(ctx, `UPDATE delivery_couriers SET partner_id = $2, name = $3, phone = $4, user_id = $5, zone_ids = $6::uuid[], status = $7,
				notes = $8, updated_at = now() WHERE id = $1`, id, in.PartnerID, in.Name, in.Phone, userID, in.ZoneIDs, in.Status, in.Notes)
			if err == nil && tag.RowsAffected() == 0 {
				return httpx.ErrNotFound
			}
		}
		if err != nil {
			if db.IsForeignKeyViolation(err) {
				return fieldErr("partner_id", "Unknown partner.")
			}
			if db.IsUniqueViolation(err, "delivery_couriers_user_key") {
				return fieldErr("user_email", "That login is already linked to another courier.")
			}
			return err
		}
		return record(ctx, tx, actor, "delivery.courier_saved", "delivery_courier", id, map[string]any{"name": in.Name, "partner_id": in.PartnerID})
	})
	if err != nil {
		return nil, err
	}
	all, err := s.Couriers(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range all {
		if c.ID == id {
			return &c, nil
		}
	}
	return nil, httpx.ErrNotFound
}

// courierOf returns the courier linked to a staff login, if any.
func courierOf(ctx context.Context, q db.Querier, userID string) (string, bool) {
	var id string
	if err := q.QueryRow(ctx, `SELECT id::text FROM delivery_couriers WHERE user_id = $1 AND status = 'active'`, userID).Scan(&id); err != nil {
		return "", false
	}
	return id, true
}
