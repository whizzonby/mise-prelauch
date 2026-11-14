// Package suppliers owns Mise's supplier directory: suppliers and farmers,
// their qualification, documents, products mapped to the ingredient
// catalogue, price history, produce availability, the communication log and
// factual performance figures.
//
// Many suppliers are smallholder farmers who use the telephone or WhatsApp,
// not a portal, so staff keep these records on their behalf; every record
// says who entered it. Nothing here sends a message, invents a supplier, a
// price or a certification, or treats availability as stock.
package suppliers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/outbox"
	"mise.tt/api/internal/support"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

var (
	Types    = []string{"farmer", "cooperative", "producer", "distributor", "packaging", "other"}
	Channels = []string{"phone", "whatsapp", "email", "in_person", "other"}
	// Agricultural suppliers may have a farm profile.
	FarmTypes = []string{"farmer", "cooperative", "producer"}
)

// Alerts raises operational alerts (internal/backoffice implements it).
type Alerts interface {
	Raise(ctx context.Context, q db.Querier, a support.Alert) error
}

type Service struct {
	pool   *pgxpool.Pool
	Alerts Alerts
	Now    func() time.Time
	// Location is the business time zone, for "today".
	Location *time.Location
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, Now: time.Now, Location: time.UTC}
}

func (s *Service) today() time.Time {
	t := s.Now().In(s.Location)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func (s *Service) tx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	return db.InTx(ctx, s.pool, fn)
}

func fieldErr(field, msg string) error {
	return httpx.ValidationError([]httpx.FieldError{{Field: field, Code: "invalid", Message: msg}})
}
func conflict(code, msg string) error { return httpx.NewError(http.StatusConflict, code, msg) }
func unprocessable(code, msg string) error {
	return httpx.NewError(http.StatusUnprocessableEntity, code, msg)
}
func forbidden(code, msg string) error { return httpx.NewError(http.StatusForbidden, code, msg) }

func clean(s string) string { return strings.Join(strings.Fields(s), " ") }
func runes(s string) int    { return len([]rune(s)) }
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
func notFoundIf(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.ErrNotFound
	}
	return err
}

// NextNumber returns the next number for kind in period (SUP, PO-2611, ...).
// Shared by the supply-chain modules; it takes the caller's transaction.
func NextNumber(ctx context.Context, q db.Querier, kind, period string) (int, error) {
	var n int
	err := q.QueryRow(ctx, `
		INSERT INTO supply_counters (kind, period, last) VALUES ($1, $2, 1)
		ON CONFLICT (kind, period) DO UPDATE SET last = supply_counters.last + 1
		RETURNING last`, kind, period).Scan(&n)
	return n, err
}

func event(ctx context.Context, q db.Querier, typ, id, dedupe string, payload map[string]any) error {
	return outbox.Record(ctx, q, outbox.Event{Type: typ, AggregateType: "supplier", AggregateID: id, DedupeKey: dedupe, Payload: payload})
}

// ---------------------------------------------------------------- suppliers

// Supplier is one supplier record.
type Supplier struct {
	ID                   string    `json:"id"`
	Code                 string    `json:"code"`
	Name                 string    `json:"name"`
	TradingName          string    `json:"trading_name"`
	Type                 string    `json:"type"`
	Status               string    `json:"status"`
	ContactName          string    `json:"contact_name"`
	Phone                string    `json:"phone"`
	Email                string    `json:"email"`
	PreferredChannel     string    `json:"preferred_channel"`
	Region               string    `json:"region"`
	Location             string    `json:"location"`
	OperatingRegions     []string  `json:"operating_regions"`
	PaymentTerms         string    `json:"payment_terms"`
	DeliveryArrangements string    `json:"delivery_arrangements"`
	Notes                string    `json:"notes"`
	ReviewExpiresOn      *string   `json:"review_expires_on"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
	// Approved, and the approval has not lapsed: only these get live orders.
	Qualified bool `json:"qualified"`
	// How many products and whether a farm profile exists (directory columns).
	Products int  `json:"products"`
	HasFarm  bool `json:"has_farm"`

	Farm           *Farm           `json:"farm,omitempty"`
	Documents      []Document      `json:"documents,omitempty"`
	History        []StatusEvent   `json:"history,omitempty"`
	Reviews        []Review        `json:"reviews,omitempty"`
	Communications []Communication `json:"communications,omitempty"`
	// What the next review must check for this supplier's type.
	Criteria []Criterion `json:"criteria,omitempty"`
	// Why the supplier cannot be submitted or approved yet.
	Missing []string `json:"missing,omitempty"`
}

const supplierCols = `s.id, s.code, s.name, s.trading_name, s.type, s.status, s.contact_name, s.phone, s.email, s.preferred_channel,
	s.region, s.location, s.operating_regions, s.payment_terms, s.delivery_arrangements, s.notes, s.review_expires_on::text,
	s.created_at, s.updated_at,
	(SELECT count(*) FROM supplier_products p WHERE p.supplier_id = s.id AND p.active)::int,
	EXISTS (SELECT 1 FROM farmer_profiles f WHERE f.supplier_id = s.id)`

func (s *Service) scan(r pgx.Row) (Supplier, error) {
	var x Supplier
	err := r.Scan(&x.ID, &x.Code, &x.Name, &x.TradingName, &x.Type, &x.Status, &x.ContactName, &x.Phone, &x.Email, &x.PreferredChannel,
		&x.Region, &x.Location, &x.OperatingRegions, &x.PaymentTerms, &x.DeliveryArrangements, &x.Notes, &x.ReviewExpiresOn,
		&x.CreatedAt, &x.UpdatedAt, &x.Products, &x.HasFarm)
	if err == nil {
		x.Qualified = s.qualified(x.Status, x.ReviewExpiresOn)
	}
	return x, err
}

func (s *Service) qualified(status string, expires *string) bool {
	if status != "approved" {
		return false
	}
	if expires == nil {
		return true
	}
	d, err := time.Parse(time.DateOnly, *expires)
	return err == nil && !d.Before(s.today())
}

// Filter narrows the directory.
type Filter struct {
	Query, Type, Status string
	Farmers             bool
	Page, Size          int
}

// Page is a page of suppliers.
type Page struct {
	Items []Supplier `json:"items"`
	Total int        `json:"total"`
	Page  int        `json:"page"`
	Size  int        `json:"page_size"`
}

func (s *Service) List(ctx context.Context, f Filter) (*Page, error) {
	if f.Size <= 0 || f.Size > 200 {
		f.Size = 50
	}
	if f.Page < 1 {
		f.Page = 1
	}
	where := ` WHERE ($1 = '' OR s.name ILIKE '%' || $1 || '%' OR s.trading_name ILIKE '%' || $1 || '%' OR s.code ILIKE $1 || '%'
		OR s.contact_name ILIKE '%' || $1 || '%')
		AND ($2 = '' OR s.type = $2) AND ($3 = '' OR s.status = $3)
		AND (NOT $4 OR s.type IN ('farmer', 'cooperative', 'producer'))`
	args := []any{strings.TrimSpace(f.Query), f.Type, f.Status, f.Farmers}
	p := &Page{Page: f.Page, Size: f.Size, Items: []Supplier{}}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM suppliers s`+where, args...).Scan(&p.Total); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+supplierCols+` FROM suppliers s`+where+` ORDER BY s.name LIMIT $5 OFFSET $6`,
		append(args, f.Size, (f.Page-1)*f.Size)...)
	if err != nil {
		return nil, err
	}
	p.Items, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Supplier, error) { return s.scan(r) })
	return p, err
}

// Get returns a supplier with its farm, documents, history, reviews and
// communication log.
func (s *Service) Get(ctx context.Context, id string) (*Supplier, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	return s.get(ctx, s.pool, id)
}

func (s *Service) get(ctx context.Context, q db.Querier, id string) (*Supplier, error) {
	x, err := s.scan(q.QueryRow(ctx, `SELECT `+supplierCols+` FROM suppliers s WHERE s.id = $1`, id))
	if err != nil {
		return nil, notFoundIf(err)
	}
	if x.Farm, err = s.farm(ctx, q, id); err != nil {
		return nil, err
	}
	if x.Documents, err = s.documents(ctx, q, id); err != nil {
		return nil, err
	}
	if x.History, err = s.history(ctx, q, id); err != nil {
		return nil, err
	}
	if x.Reviews, err = s.reviews(ctx, q, id); err != nil {
		return nil, err
	}
	if x.Communications, err = s.communications(ctx, q, id); err != nil {
		return nil, err
	}
	if x.Criteria, err = s.criteriaFor(ctx, q, x.Type); err != nil {
		return nil, err
	}
	x.Missing = missing(&x)
	return &x, nil
}

// missing lists what a submission or an approval still needs.
func missing(x *Supplier) []string {
	out := []string{}
	if x.Phone == "" && x.Email == "" {
		out = append(out, "A phone number or email address, so purchasing can reach them.")
	}
	if len(x.Criteria) == 0 {
		out = append(out, "No qualification criteria are configured for "+strings.ReplaceAll(x.Type, "_", " ")+
			" suppliers. A procurement manager must add them from the approved procedure before anyone can be approved.")
	}
	return out
}

// Input creates or edits a supplier. Only name and type are required at
// first: informal farmers may have little else.
type Input struct {
	Name                 *string   `json:"name"`
	TradingName          *string   `json:"trading_name"`
	Type                 *string   `json:"type"`
	ContactName          *string   `json:"contact_name"`
	Phone                *string   `json:"phone"`
	Email                *string   `json:"email"`
	PreferredChannel     *string   `json:"preferred_channel"`
	Region               *string   `json:"region"`
	Location             *string   `json:"location"`
	OperatingRegions     *[]string `json:"operating_regions"`
	PaymentTerms         *string   `json:"payment_terms"`
	DeliveryArrangements *string   `json:"delivery_arrangements"`
	Notes                *string   `json:"notes"`
}

var emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

func (in *Input) validate(create bool) error {
	var f []httpx.FieldError
	bad := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	trim := func(p *string, field string, max int, collapse bool) {
		if p == nil {
			return
		}
		if collapse {
			*p = clean(*p)
		} else {
			*p = strings.TrimSpace(*p)
		}
		if runes(*p) > max {
			bad(field, fmt.Sprintf("At most %d characters.", max))
		}
	}
	if create && (in.Name == nil || clean(*in.Name) == "") {
		bad("name", "Enter the supplier's name.")
	}
	if in.Name != nil && clean(*in.Name) == "" && !create {
		bad("name", "The name cannot be empty.")
	}
	trim(in.Name, "name", 120, true)
	trim(in.TradingName, "trading_name", 120, true)
	trim(in.ContactName, "contact_name", 120, true)
	trim(in.Phone, "phone", 40, true)
	trim(in.Email, "email", 254, true)
	trim(in.Region, "region", 60, true)
	trim(in.Location, "location", 300, false)
	trim(in.PaymentTerms, "payment_terms", 300, false)
	trim(in.DeliveryArrangements, "delivery_arrangements", 500, false)
	trim(in.Notes, "notes", 2000, false)
	if create && in.Type == nil {
		bad("type", "Choose the type of supplier.")
	}
	if in.Type != nil && !slices.Contains(Types, *in.Type) {
		bad("type", "Choose the type of supplier.")
	}
	if in.PreferredChannel != nil && !slices.Contains(Channels, *in.PreferredChannel) {
		bad("preferred_channel", "Choose how they prefer to be contacted.")
	}
	if in.Email != nil && *in.Email != "" && !emailPattern.MatchString(*in.Email) {
		bad("email", "Enter a valid email address, or leave it blank.")
	}
	if in.OperatingRegions != nil {
		regions := []string{}
		for _, r := range *in.OperatingRegions {
			if r = clean(r); r != "" && !slices.ContainsFunc(regions, func(x string) bool { return strings.EqualFold(x, r) }) {
				regions = append(regions, r)
			}
		}
		if len(regions) > 30 {
			bad("operating_regions", "At most 30 regions.")
		}
		*in.OperatingRegions = regions
	}
	if len(f) > 0 {
		return httpx.ValidationError(f)
	}
	return nil
}

func (s *Service) Create(ctx context.Context, actor string, in Input) (*Supplier, error) {
	if err := in.validate(true); err != nil {
		return nil, err
	}
	var id string
	err := s.tx(ctx, func(tx pgx.Tx) error {
		n, err := NextNumber(ctx, tx, "SUP", "")
		if err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `INSERT INTO suppliers (code, name, type, created_by) VALUES ($1, $2, $3, $4) RETURNING id`,
			fmt.Sprintf("SUP-%04d", n), *in.Name, *in.Type, nilIfEmpty(actor)).Scan(&id)
		if db.IsUniqueViolation(err, "") {
			return conflict("duplicate_supplier", "A supplier with this name already exists. Open it instead of adding another.")
		}
		if err != nil {
			return err
		}
		in.Name, in.Type = nil, nil
		if err := s.apply(ctx, tx, id, in); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO supplier_status_events (supplier_id, to_status, actor_id) VALUES ($1, 'draft', $2)`, id, nilIfEmpty(actor)); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "supplier.created", TargetType: "supplier", TargetID: id})
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// apply writes the fields that are set.
func (s *Service) apply(ctx context.Context, tx pgx.Tx, id string, in Input) error {
	sets, args := []string{}, []any{id}
	set := func(col string, v any) {
		args = append(args, v)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	if in.Name != nil {
		set("name", *in.Name)
	}
	if in.TradingName != nil {
		set("trading_name", *in.TradingName)
	}
	if in.Type != nil {
		set("type", *in.Type)
	}
	if in.ContactName != nil {
		set("contact_name", *in.ContactName)
	}
	if in.Phone != nil {
		set("phone", *in.Phone)
	}
	if in.Email != nil {
		set("email", strings.ToLower(*in.Email))
	}
	if in.PreferredChannel != nil {
		set("preferred_channel", *in.PreferredChannel)
	}
	if in.Region != nil {
		set("region", *in.Region)
	}
	if in.Location != nil {
		set("location", *in.Location)
	}
	if in.OperatingRegions != nil {
		set("operating_regions", *in.OperatingRegions)
	}
	if in.PaymentTerms != nil {
		set("payment_terms", *in.PaymentTerms)
	}
	if in.DeliveryArrangements != nil {
		set("delivery_arrangements", *in.DeliveryArrangements)
	}
	if in.Notes != nil {
		set("notes", *in.Notes)
	}
	if len(sets) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `UPDATE suppliers SET `+strings.Join(sets, ", ")+`, updated_at = now() WHERE id = $1`, args...)
	if db.IsUniqueViolation(err, "") {
		return conflict("duplicate_supplier", "A supplier with this name already exists.")
	}
	return err
}

func (s *Service) Update(ctx context.Context, actor, id string, in Input) (*Supplier, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	if err := in.validate(false); err != nil {
		return nil, err
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var status, typ string
		if err := tx.QueryRow(ctx, `SELECT status, type FROM suppliers WHERE id = $1 FOR UPDATE`, id).Scan(&status, &typ); err != nil {
			return notFoundIf(err)
		}
		// A change of type changes what qualification means: an approved
		// supplier goes back for review rather than keep an approval for
		// something else.
		if in.Type != nil && *in.Type != typ && status == "approved" {
			if err := s.move(ctx, tx, id, status, "pending_review", "Supplier type changed from "+typ+" to "+*in.Type+"; needs a new review.", actor); err != nil {
				return err
			}
		}
		if err := s.apply(ctx, tx, id, in); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "supplier.updated", TargetType: "supplier", TargetID: id})
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// move changes a supplier's status and records why.
func (s *Service) move(ctx context.Context, tx pgx.Tx, id, from, to, note, actor string) error {
	if _, err := tx.Exec(ctx, `UPDATE suppliers SET status = $2, updated_at = now() WHERE id = $1`, id, to); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO supplier_status_events (supplier_id, from_status, to_status, note, actor_id) VALUES ($1, $2, $3, $4, $5)`,
		id, nilIfEmpty(from), to, nilIfEmpty(note), nilIfEmpty(actor))
	return err
}

// lockSupplier locks a supplier row and returns it.
func (s *Service) lockSupplier(ctx context.Context, tx pgx.Tx, id string) (*Supplier, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	if _, err := tx.Exec(ctx, `SELECT 1 FROM suppliers WHERE id = $1 FOR UPDATE`, id); err != nil {
		return nil, err
	}
	return s.get(ctx, tx, id)
}

// Submit sends a supplier for qualification review.
func (s *Service) Submit(ctx context.Context, actor, id, note string) (*Supplier, error) {
	err := s.tx(ctx, func(tx pgx.Tx) error {
		x, err := s.lockSupplier(ctx, tx, id)
		if err != nil {
			return err
		}
		if x.Status != "draft" && x.Status != "rejected" {
			return conflict("invalid_transition", "Only a draft or rejected supplier can be submitted for review; this one is "+human(x.Status)+".")
		}
		if x.Phone == "" && x.Email == "" {
			return unprocessable("incomplete", "Add a phone number or email address first, so purchasing can reach them.")
		}
		if _, err := tx.Exec(ctx, `UPDATE suppliers SET submitted_by = $2 WHERE id = $1`, id, nilIfEmpty(actor)); err != nil {
			return err
		}
		if err := s.move(ctx, tx, id, x.Status, "pending_review", clean(note), actor); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "supplier.submitted", TargetType: "supplier", TargetID: id})
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// CriterionResult is a reviewer's finding on one criterion.
type CriterionResult struct {
	CriterionID string `json:"criterion_id"`
	Name        string `json:"name"`
	Met         bool   `json:"met"`
	Note        string `json:"note"`
}

// ReviewInput is a qualification decision.
type ReviewInput struct {
	Decision  string            `json:"decision"`
	Results   []CriterionResult `json:"results"`
	Notes     string            `json:"notes"`
	ExpiresOn string            `json:"expires_on"`
}

// Review records a qualification decision. Approval needs every required
// criterion for the supplier's type recorded as met, by someone other than
// the person who submitted the supplier.
func (s *Service) Review(ctx context.Context, actor, id string, in ReviewInput) (*Supplier, error) {
	in.Notes = strings.TrimSpace(in.Notes)
	if !slices.Contains([]string{"approved", "rejected", "changes_requested"}, in.Decision) {
		return nil, fieldErr("decision", "Approve, reject or ask for changes.")
	}
	if in.Decision != "approved" && in.Notes == "" {
		return nil, fieldErr("notes", "Say why, so the record can be fixed.")
	}
	if runes(in.Notes) > 2000 {
		return nil, fieldErr("notes", "At most 2000 characters.")
	}
	var expires *time.Time
	if in.ExpiresOn != "" {
		d, err := time.Parse(time.DateOnly, in.ExpiresOn)
		if err != nil || !d.After(s.today()) {
			return nil, fieldErr("expires_on", "The review expiry must be a future date, or blank.")
		}
		expires = &d
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		x, err := s.lockSupplier(ctx, tx, id)
		if err != nil {
			return err
		}
		if x.Status != "pending_review" {
			return conflict("invalid_transition", "Only a supplier waiting for review can be reviewed; this one is "+human(x.Status)+".")
		}
		var submittedBy *string
		if err := tx.QueryRow(ctx, `SELECT submitted_by::text FROM suppliers WHERE id = $1`, id).Scan(&submittedBy); err != nil {
			return err
		}
		if submittedBy != nil && *submittedBy == actor {
			return forbidden("own_submission", "You submitted this supplier, so someone else must review it.")
		}
		results, err := checkResults(x.Criteria, in.Results, in.Decision == "approved")
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO supplier_reviews (supplier_id, decision, results, notes, expires_on, reviewer_id) VALUES ($1, $2, $3, $4, $5, $6)`,
			id, in.Decision, results, in.Notes, expires, nilIfEmpty(actor)); err != nil {
			return err
		}
		to := map[string]string{"approved": "approved", "rejected": "rejected", "changes_requested": "draft"}[in.Decision]
		if in.Decision == "approved" {
			if _, err := tx.Exec(ctx, `UPDATE suppliers SET review_expires_on = $2 WHERE id = $1`, id, expires); err != nil {
				return err
			}
		}
		if err := s.move(ctx, tx, id, x.Status, to, in.Notes, actor); err != nil {
			return err
		}
		if in.Decision == "approved" {
			if err := event(ctx, tx, "supplier.approved", id, fmt.Sprint(len(x.Reviews)+1), map[string]any{"code": x.Code, "type": x.Type, "expires_on": in.ExpiresOn}); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "supplier.reviewed", TargetType: "supplier", TargetID: id,
			Metadata: map[string]any{"decision": in.Decision}})
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// checkResults matches the reviewer's results to the criteria that apply.
// For an approval every criterion must have a result and every required one
// must be met; with no criteria configured there is nothing to approve against.
func checkResults(criteria []Criterion, given []CriterionResult, approving bool) ([]CriterionResult, error) {
	by := map[string]CriterionResult{}
	for _, r := range given {
		by[r.CriterionID] = r
	}
	out := []CriterionResult{}
	var problems []string
	for _, c := range criteria {
		r, ok := by[c.ID]
		r.Note = strings.TrimSpace(r.Note)
		if runes(r.Note) > 500 {
			return nil, fieldErr("results", "Each note is at most 500 characters.")
		}
		if !ok {
			if approving {
				problems = append(problems, "No result for: "+c.Name+".")
			}
			continue
		}
		if approving && c.Required && !r.Met {
			problems = append(problems, "Required and not met: "+c.Name+".")
		}
		out = append(out, CriterionResult{CriterionID: c.ID, Name: c.Name, Met: r.Met, Note: r.Note})
	}
	if approving && len(criteria) == 0 {
		return nil, unprocessable("no_criteria", "No qualification criteria are configured for this type of supplier. A procurement manager must add them from the approved procedure first.")
	}
	if len(problems) > 0 {
		return nil, unprocessable("criteria_not_met", strings.Join(problems, " "))
	}
	return out, nil
}

// Suspend stops ordering from an approved supplier.
func (s *Service) Suspend(ctx context.Context, actor, id, reason string) (*Supplier, error) {
	return s.transition(ctx, actor, id, reason, []string{"approved"}, "suspended", "supplier.suspended", true)
}

// Reactivate sends a suspended supplier for a new review, or reopens an
// inactive one as a draft.
func (s *Service) Reactivate(ctx context.Context, actor, id, reason string) (*Supplier, error) {
	var status string
	if err := s.pool.QueryRow(ctx, `SELECT status FROM suppliers WHERE id = $1`, id).Scan(&status); err != nil || !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	if status == "inactive" {
		return s.transition(ctx, actor, id, reason, []string{"inactive"}, "draft", "supplier.reopened", false)
	}
	return s.transition(ctx, actor, id, reason, []string{"suspended"}, "pending_review", "supplier.reactivated", true)
}

// Deactivate retires a supplier. History stays.
func (s *Service) Deactivate(ctx context.Context, actor, id, reason string) (*Supplier, error) {
	return s.transition(ctx, actor, id, reason, []string{"draft", "pending_review", "approved", "suspended", "rejected"}, "inactive", "supplier.deactivated", true)
}

func (s *Service) transition(ctx context.Context, actor, id, reason string, from []string, to, action string, needReason bool) (*Supplier, error) {
	reason = clean(reason)
	if needReason && runes(reason) < 3 {
		return nil, fieldErr("reason", "Give the reason.")
	}
	if runes(reason) > 500 {
		return nil, fieldErr("reason", "At most 500 characters.")
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		x, err := s.lockSupplier(ctx, tx, id)
		if err != nil {
			return err
		}
		if !slices.Contains(from, x.Status) {
			return conflict("invalid_transition", "This supplier is "+human(x.Status)+".")
		}
		if err := s.move(ctx, tx, id, x.Status, to, reason, actor); err != nil {
			return err
		}
		if to == "suspended" {
			if err := event(ctx, tx, "supplier.suspended", id, fmt.Sprint(len(x.History)), map[string]any{"code": x.Code, "reason": reason}); err != nil {
				return err
			}
			if s.Alerts != nil {
				if err := s.Alerts.Raise(ctx, tx, support.Alert{DedupeKey: "supplier_suspended:" + id + ":" + fmt.Sprint(len(x.History)), Kind: "supplier_suspended",
					Severity: "warning", Title: x.Name + " suspended", Body: reason + " Open purchase orders with this supplier need a decision.",
					ResourceType: "supplier", ResourceID: id, Permission: "purchase_orders.read"}); err != nil {
					return err
				}
			}
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: action, TargetType: "supplier", TargetID: id,
			Metadata: map[string]any{"reason": reason}})
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Qualified reports whether a supplier may receive live purchase orders
// (approved, approval not lapsed), with its name and status.
func (s *Service) Qualified(ctx context.Context, q db.Querier, id string) (ok bool, name, status string, err error) {
	var expires *string
	err = q.QueryRow(ctx, `SELECT name, status, review_expires_on::text FROM suppliers WHERE id = $1`, id).Scan(&name, &status, &expires)
	if err != nil {
		return false, "", "", notFoundIf(err)
	}
	return s.qualified(status, expires), name, status, nil
}

// StatusEvent is one entry in the approval history.
type StatusEvent struct {
	From      *string   `json:"from"`
	To        string    `json:"to"`
	Note      *string   `json:"note"`
	ActorName string    `json:"actor_name"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Service) history(ctx context.Context, q db.Querier, id string) ([]StatusEvent, error) {
	rows, err := q.Query(ctx, `SELECT e.from_status, e.to_status, e.note, COALESCE(btrim(u.first_name || ' ' || u.last_name), ''), e.created_at
		FROM supplier_status_events e LEFT JOIN users u ON u.id = e.actor_id WHERE e.supplier_id = $1 ORDER BY e.id`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[StatusEvent])
}

// Review is one qualification decision.
type Review struct {
	ID           string            `json:"id"`
	Decision     string            `json:"decision"`
	Results      []CriterionResult `json:"results"`
	Notes        string            `json:"notes"`
	ExpiresOn    *string           `json:"expires_on"`
	ReviewerName string            `json:"reviewer_name"`
	CreatedAt    time.Time         `json:"created_at"`
}

func (s *Service) reviews(ctx context.Context, q db.Querier, id string) ([]Review, error) {
	rows, err := q.Query(ctx, `SELECT r.id, r.decision, r.results, r.notes, r.expires_on::text, COALESCE(btrim(u.first_name || ' ' || u.last_name), ''), r.created_at
		FROM supplier_reviews r LEFT JOIN users u ON u.id = r.reviewer_id WHERE r.supplier_id = $1 ORDER BY r.created_at DESC`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Review])
}

func human(status string) string { return strings.ReplaceAll(status, "_", " ") }

// ---------------------------------------------------------------- criteria

// Criterion is one qualification check from the approved procedure.
type Criterion struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	SupplierTypes []string  `json:"supplier_types"`
	Required      bool      `json:"required"`
	ProcedureRef  string    `json:"procedure_ref"`
	Position      int       `json:"position"`
	Active        bool      `json:"active"`
	UpdatedAt     time.Time `json:"updated_at"`
}

const criterionCols = `id, name, description, supplier_types, required, procedure_ref, position, active, updated_at`

func (s *Service) Criteria(ctx context.Context) ([]Criterion, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+criterionCols+` FROM supplier_qualification_criteria ORDER BY active DESC, position, name`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Criterion])
}

func (s *Service) criteriaFor(ctx context.Context, q db.Querier, typ string) ([]Criterion, error) {
	rows, err := q.Query(ctx, `SELECT `+criterionCols+` FROM supplier_qualification_criteria
		WHERE active AND (cardinality(supplier_types) = 0 OR $1 = ANY (supplier_types)) ORDER BY position, name`, typ)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Criterion])
}

// CriterionInput adds or edits a criterion.
type CriterionInput struct {
	Name          *string   `json:"name"`
	Description   *string   `json:"description"`
	SupplierTypes *[]string `json:"supplier_types"`
	Required      *bool     `json:"required"`
	ProcedureRef  *string   `json:"procedure_ref"`
	Position      *int      `json:"position"`
	Active        *bool     `json:"active"`
}

func (s *Service) SaveCriterion(ctx context.Context, actor, id string, in CriterionInput) ([]Criterion, error) {
	var f []httpx.FieldError
	bad := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	if in.Name != nil {
		*in.Name = clean(*in.Name)
		if n := runes(*in.Name); n < 1 || n > 120 {
			bad("name", "Name the criterion (at most 120 characters).")
		}
	} else if id == "" {
		bad("name", "Name the criterion.")
	}
	if in.Description != nil && runes(*in.Description) > 1000 {
		bad("description", "At most 1000 characters.")
	}
	if in.ProcedureRef != nil {
		*in.ProcedureRef = clean(*in.ProcedureRef)
		if runes(*in.ProcedureRef) > 120 {
			bad("procedure_ref", "At most 120 characters.")
		}
	}
	if in.SupplierTypes != nil {
		for _, t := range *in.SupplierTypes {
			if !slices.Contains(Types, t) {
				bad("supplier_types", "Choose supplier types from the list.")
				break
			}
		}
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if id == "" {
			types := []string{}
			if in.SupplierTypes != nil {
				types = *in.SupplierTypes
			}
			pos, req := 0, true
			if in.Position != nil {
				pos = *in.Position
			}
			if in.Required != nil {
				req = *in.Required
			}
			desc, ref := "", ""
			if in.Description != nil {
				desc = strings.TrimSpace(*in.Description)
			}
			if in.ProcedureRef != nil {
				ref = *in.ProcedureRef
			}
			if err := tx.QueryRow(ctx, `INSERT INTO supplier_qualification_criteria (name, description, supplier_types, required, procedure_ref, position)
				VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`, *in.Name, desc, types, req, ref, pos).Scan(&id); err != nil {
				return err
			}
		} else {
			if !uuidPattern.MatchString(id) {
				return httpx.ErrNotFound
			}
			tag, err := tx.Exec(ctx, `UPDATE supplier_qualification_criteria SET
				name = COALESCE($2, name), description = COALESCE($3, description), supplier_types = COALESCE($4, supplier_types),
				required = COALESCE($5, required), procedure_ref = COALESCE($6, procedure_ref), position = COALESCE($7, position),
				active = COALESCE($8, active), updated_at = now() WHERE id = $1`,
				id, in.Name, in.Description, in.SupplierTypes, in.Required, in.ProcedureRef, in.Position, in.Active)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return httpx.ErrNotFound
			}
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "supplier_criterion.saved", TargetType: "supplier_criterion", TargetID: id})
	})
	if err != nil {
		return nil, err
	}
	return s.Criteria(ctx)
}

// ---------------------------------------------------------------- farms

// Farm is the farm profile of an agricultural supplier.
type Farm struct {
	FarmName               string    `json:"farm_name"`
	FarmLocation           string    `json:"farm_location"`
	Crops                  []string  `json:"crops"`
	GrowingSeasons         string    `json:"growing_seasons"`
	HarvestWindows         string    `json:"harvest_windows"`
	EstimatedCapacity      string    `json:"estimated_capacity"`
	CollectionArrangements string    `json:"collection_arrangements"`
	InformationSource      string    `json:"information_source"`
	RecordedByName         string    `json:"recorded_by_name"`
	UpdatedAt              time.Time `json:"updated_at"`
}

func (s *Service) farm(ctx context.Context, q db.Querier, id string) (*Farm, error) {
	var f Farm
	err := q.QueryRow(ctx, `SELECT f.farm_name, f.farm_location, f.crops, f.growing_seasons, f.harvest_windows, f.estimated_capacity,
		f.collection_arrangements, f.information_source, COALESCE(btrim(u.first_name || ' ' || u.last_name), ''), f.updated_at
		FROM farmer_profiles f LEFT JOIN users u ON u.id = f.recorded_by WHERE f.supplier_id = $1`, id).Scan(
		&f.FarmName, &f.FarmLocation, &f.Crops, &f.GrowingSeasons, &f.HarvestWindows, &f.EstimatedCapacity,
		&f.CollectionArrangements, &f.InformationSource, &f.RecordedByName, &f.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &f, err
}

// FarmInput records a farm profile. Every field is optional.
type FarmInput struct {
	FarmName               string   `json:"farm_name"`
	FarmLocation           string   `json:"farm_location"`
	Crops                  []string `json:"crops"`
	GrowingSeasons         string   `json:"growing_seasons"`
	HarvestWindows         string   `json:"harvest_windows"`
	EstimatedCapacity      string   `json:"estimated_capacity"`
	CollectionArrangements string   `json:"collection_arrangements"`
	InformationSource      string   `json:"information_source"`
}

func (s *Service) SaveFarm(ctx context.Context, actor, id string, in FarmInput) (*Supplier, error) {
	var f []httpx.FieldError
	limit := func(v *string, field string, max int) {
		*v = strings.TrimSpace(*v)
		if runes(*v) > max {
			f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: fmt.Sprintf("At most %d characters.", max)})
		}
	}
	limit(&in.FarmName, "farm_name", 120)
	limit(&in.FarmLocation, "farm_location", 300)
	limit(&in.GrowingSeasons, "growing_seasons", 500)
	limit(&in.HarvestWindows, "harvest_windows", 500)
	limit(&in.EstimatedCapacity, "estimated_capacity", 500)
	limit(&in.CollectionArrangements, "collection_arrangements", 500)
	limit(&in.InformationSource, "information_source", 300)
	crops := []string{}
	for _, c := range in.Crops {
		if c = clean(c); c != "" && !slices.ContainsFunc(crops, func(x string) bool { return strings.EqualFold(x, c) }) {
			crops = append(crops, c)
		}
	}
	if len(crops) > 50 {
		f = append(f, httpx.FieldError{Field: "crops", Code: "invalid", Message: "At most 50 crops."})
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		x, err := s.lockSupplier(ctx, tx, id)
		if err != nil {
			return err
		}
		if !slices.Contains(FarmTypes, x.Type) {
			return unprocessable("not_a_farm", "Farm profiles are for farmers, cooperatives and agricultural producers.")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO farmer_profiles (supplier_id, farm_name, farm_location, crops, growing_seasons, harvest_windows,
			estimated_capacity, collection_arrangements, information_source, recorded_by) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (supplier_id) DO UPDATE SET farm_name = $2, farm_location = $3, crops = $4, growing_seasons = $5, harvest_windows = $6,
			estimated_capacity = $7, collection_arrangements = $8, information_source = $9, recorded_by = $10, updated_at = now()`,
			id, in.FarmName, in.FarmLocation, crops, in.GrowingSeasons, in.HarvestWindows, in.EstimatedCapacity,
			in.CollectionArrangements, in.InformationSource, nilIfEmpty(actor)); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "supplier.farm_saved", TargetType: "supplier", TargetID: id})
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// ---------------------------------------------------------------- documents

// Document is a supplier document's details. A certification counts only once verified.
type Document struct {
	ID             string     `json:"id"`
	Kind           string     `json:"kind"`
	Title          string     `json:"title"`
	Reference      string     `json:"reference"`
	IssuedBy       string     `json:"issued_by"`
	ExpiresOn      *string    `json:"expires_on"`
	VerifiedByName *string    `json:"verified_by_name"`
	VerifiedAt     *time.Time `json:"verified_at"`
	Expired        bool       `json:"expired"`
	CreatedAt      time.Time  `json:"created_at"`
}

func (s *Service) documents(ctx context.Context, q db.Querier, id string) ([]Document, error) {
	rows, err := q.Query(ctx, `SELECT d.id, d.kind, d.title, d.reference, d.issued_by, d.expires_on::text,
		CASE WHEN d.verified_by IS NULL THEN NULL ELSE COALESCE(btrim(u.first_name || ' ' || u.last_name), '') END, d.verified_at,
		COALESCE(d.expires_on < $2, false), d.created_at
		FROM supplier_documents d LEFT JOIN users u ON u.id = d.verified_by WHERE d.supplier_id = $1 ORDER BY d.created_at`, id, s.today())
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Document])
}

// DocumentInput records a document the supplier provided (metadata only).
type DocumentInput struct {
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Reference string `json:"reference"`
	IssuedBy  string `json:"issued_by"`
	ExpiresOn string `json:"expires_on"`
}

func (s *Service) AddDocument(ctx context.Context, actor, id string, in DocumentInput) (*Supplier, error) {
	in.Title, in.Reference, in.IssuedBy = clean(in.Title), strings.TrimSpace(in.Reference), clean(in.IssuedBy)
	var f []httpx.FieldError
	if !slices.Contains([]string{"certification", "licence", "inspection_report", "agreement", "other"}, in.Kind) {
		f = append(f, httpx.FieldError{Field: "kind", Code: "invalid", Message: "Choose the kind of document."})
	}
	if n := runes(in.Title); n < 1 || n > 200 {
		f = append(f, httpx.FieldError{Field: "title", Code: "invalid", Message: "Give the document a title."})
	}
	if runes(in.Reference) > 300 || runes(in.IssuedBy) > 200 {
		f = append(f, httpx.FieldError{Field: "reference", Code: "invalid", Message: "Too long."})
	}
	var expires *time.Time
	if in.ExpiresOn != "" {
		d, err := time.Parse(time.DateOnly, in.ExpiresOn)
		if err != nil {
			f = append(f, httpx.FieldError{Field: "expires_on", Code: "invalid", Message: "Enter a date, or leave it blank."})
		}
		expires = &d
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.lockSupplier(ctx, tx, id); err != nil {
			return err
		}
		var docID string
		if err := tx.QueryRow(ctx, `INSERT INTO supplier_documents (supplier_id, kind, title, reference, issued_by, expires_on, added_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`, id, in.Kind, in.Title, in.Reference, in.IssuedBy, expires, nilIfEmpty(actor)).Scan(&docID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "supplier.document_added", TargetType: "supplier", TargetID: id,
			Metadata: map[string]any{"document_id": docID, "kind": in.Kind}})
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// VerifyDocument records that someone checked a document against its source.
func (s *Service) VerifyDocument(ctx context.Context, actor, docID string) (*Supplier, error) {
	if !uuidPattern.MatchString(docID) {
		return nil, httpx.ErrNotFound
	}
	var supplierID string
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var verified bool
		if err := tx.QueryRow(ctx, `SELECT supplier_id, verified_by IS NOT NULL FROM supplier_documents WHERE id = $1 FOR UPDATE`, docID).Scan(&supplierID, &verified); err != nil {
			return notFoundIf(err)
		}
		if verified {
			return conflict("already_verified", "This document is already verified.")
		}
		if _, err := tx.Exec(ctx, `UPDATE supplier_documents SET verified_by = $2, verified_at = now() WHERE id = $1`, docID, nilIfEmpty(actor)); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "supplier.document_verified", TargetType: "supplier", TargetID: supplierID,
			Metadata: map[string]any{"document_id": docID}})
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, supplierID)
}

// ---------------------------------------------------------------- communication

// Communication is one recorded contact with a supplier.
type Communication struct {
	ID             int64     `json:"id"`
	Channel        string    `json:"channel"`
	Direction      string    `json:"direction"`
	Summary        string    `json:"summary"`
	ResourceType   *string   `json:"resource_type"`
	ResourceID     *string   `json:"resource_id"`
	RecordedByName string    `json:"recorded_by_name"`
	CreatedAt      time.Time `json:"created_at"`
}

func (s *Service) communications(ctx context.Context, q db.Querier, id string) ([]Communication, error) {
	rows, err := q.Query(ctx, `SELECT c.id, c.channel, c.direction, c.summary, c.resource_type, c.resource_id::text,
		COALESCE(btrim(u.first_name || ' ' || u.last_name), ''), c.created_at
		FROM supplier_communications c LEFT JOIN users u ON u.id = c.recorded_by WHERE c.supplier_id = $1 ORDER BY c.id DESC LIMIT 100`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Communication])
}

// CommunicationInput records a contact that already happened.
type CommunicationInput struct {
	Channel      string `json:"channel"`
	Direction    string `json:"direction"`
	Summary      string `json:"summary"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
}

// LogCommunication records a contact, in the caller's transaction (other
// modules log the PO or RFQ they discussed).
func LogCommunication(ctx context.Context, q db.Querier, actor, supplierID string, in CommunicationInput) error {
	in.Summary = strings.TrimSpace(in.Summary)
	var f []httpx.FieldError
	if !slices.Contains([]string{"phone", "whatsapp", "email", "in_person", "document", "other"}, in.Channel) {
		f = append(f, httpx.FieldError{Field: "channel", Code: "invalid", Message: "Choose how you were in touch."})
	}
	if in.Direction != "inbound" && in.Direction != "outbound" {
		f = append(f, httpx.FieldError{Field: "direction", Code: "invalid", Message: "Say whether you contacted them or they contacted you."})
	}
	if n := runes(in.Summary); n < 1 || n > 2000 {
		f = append(f, httpx.FieldError{Field: "summary", Code: "invalid", Message: "Summarise what was said."})
	}
	if in.ResourceID != "" && !uuidPattern.MatchString(in.ResourceID) {
		f = append(f, httpx.FieldError{Field: "resource_id", Code: "invalid", Message: "Not a valid record."})
	}
	if len(f) > 0 {
		return httpx.ValidationError(f)
	}
	_, err := q.Exec(ctx, `INSERT INTO supplier_communications (supplier_id, channel, direction, summary, resource_type, resource_id, recorded_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, supplierID, in.Channel, in.Direction, in.Summary, nilIfEmpty(in.ResourceType), nilIfEmpty(in.ResourceID), nilIfEmpty(actor))
	return err
}

func (s *Service) AddCommunication(ctx context.Context, actor, id string, in CommunicationInput) (*Supplier, error) {
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.lockSupplier(ctx, tx, id); err != nil {
			return err
		}
		if err := LogCommunication(ctx, tx, actor, id, in); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "supplier.communication_logged", TargetType: "supplier", TargetID: id,
			Metadata: map[string]any{"channel": in.Channel}})
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}
