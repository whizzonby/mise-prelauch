// Package inventory owns Mise's ingredient stock: storage locations,
// receiving and inspection, lots, the append-only movement ledger,
// reservations for production plans, issues to and returns from production
// batches, adjustments, counts, and lot-to-batch traceability.
//
// Stock is never a bare number: every change to a lot is a ledger row, and
// the lot's on-hand and reserved figures are kept in the same transaction
// (CHECK constraints stop them going negative or reserving more than is
// there). Rejected, quarantined, held and expired stock is never allocated;
// a lot without a recorded expiry date is not allocated until a person
// confirms none applies. Nothing here invents a location, a tolerance, an
// expiry date or a procedure.
package inventory

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
	"mise.tt/api/internal/platform/qty"
	"mise.tt/api/internal/procurement"
	"mise.tt/api/internal/production"
	"mise.tt/api/internal/suppliers"
	"mise.tt/api/internal/support"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Alerts raises operational alerts (internal/backoffice implements it).
type Alerts interface {
	Raise(ctx context.Context, q db.Querier, a support.Alert) error
}

type Service struct {
	pool        *pgxpool.Pool
	Procurement *procurement.Service
	Production  *production.Service
	Alerts      Alerts
	Now         func() time.Time
	Location    *time.Location
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
func clean(s string) string            { return strings.Join(strings.Fields(s), " ") }
func runes(s string) int               { return len([]rune(s)) }
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
func human(s string) string { return strings.ReplaceAll(s, "_", " ") }

func event(ctx context.Context, q db.Querier, typ, aggType, id, dedupe string, payload map[string]any) error {
	return outbox.Record(ctx, q, outbox.Event{Type: typ, AggregateType: aggType, AggregateID: id, DedupeKey: dedupe, Payload: payload})
}

func (s *Service) raise(ctx context.Context, q db.Querier, a support.Alert) error {
	if s.Alerts == nil {
		return nil
	}
	return s.Alerts.Raise(ctx, q, a)
}

// lockStock serialises allocation of one ingredient across requests, so two
// plans reserving (or two cooks issuing) never take the same quantity.
func lockStock(ctx context.Context, q db.Querier, ingredientID string) error {
	_, err := q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 7))`, "stock:"+ingredientID)
	return err
}

func parseQty(v, field string) (qty.Q, error) {
	q, err := qty.Parse(v)
	if err != nil || q.Sign() <= 0 {
		return qty.Q{}, fieldErr(field, "Enter a quantity above zero.")
	}
	return q, nil
}

// ---------------------------------------------------------------- locations

// Location is a storage location the business configured.
type Location struct {
	ID              string    `json:"id"`
	Code            string    `json:"code"`
	Name            string    `json:"name"`
	Type            string    `json:"type"`
	CapacityNote    string    `json:"capacity_note"`
	TemperatureNote string    `json:"temperature_note"`
	Active          bool      `json:"active"`
	Lots            int       `json:"lots"`
	UpdatedAt       time.Time `json:"updated_at"`
}

var locationTypes = []string{"receiving", "dry", "chilled", "frozen", "preparation", "quarantine", "other"}

func (s *Service) Locations(ctx context.Context) ([]Location, error) {
	rows, err := s.pool.Query(ctx, `SELECT l.id, l.code, l.name, l.type, l.capacity_note, l.temperature_note, l.active,
		(SELECT count(*) FROM inventory_lots x WHERE x.location_id = l.id AND x.on_hand > 0)::int, l.updated_at
		FROM storage_locations l ORDER BY l.active DESC, l.code`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Location])
}

// LocationInput adds or edits a location.
type LocationInput struct {
	Code            *string `json:"code"`
	Name            *string `json:"name"`
	Type            *string `json:"type"`
	CapacityNote    *string `json:"capacity_note"`
	TemperatureNote *string `json:"temperature_note"`
	Active          *bool   `json:"active"`
}

func (s *Service) SaveLocation(ctx context.Context, actor, id string, in LocationInput) ([]Location, error) {
	var f []httpx.FieldError
	bad := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	if in.Code != nil {
		*in.Code = strings.ToUpper(strings.TrimSpace(*in.Code))
		if !regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]{0,19}$`).MatchString(*in.Code) {
			bad("code", "A short code: letters, digits and hyphens, for example DRY-1.")
		}
	} else if id == "" {
		bad("code", "Give the location a short code.")
	}
	if in.Name != nil {
		*in.Name = clean(*in.Name)
		if n := runes(*in.Name); n < 1 || n > 120 {
			bad("name", "Name the location.")
		}
	} else if id == "" {
		bad("name", "Name the location.")
	}
	if in.Type != nil && !slices.Contains(locationTypes, *in.Type) {
		bad("type", "Choose the kind of storage.")
	} else if in.Type == nil && id == "" {
		bad("type", "Choose the kind of storage.")
	}
	for _, p := range []*string{in.CapacityNote, in.TemperatureNote} {
		if p != nil && runes(*p) > 300 {
			bad("capacity_note", "At most 300 characters.")
		}
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var err error
		if id == "" {
			err = tx.QueryRow(ctx, `INSERT INTO storage_locations (code, name, type, capacity_note, temperature_note) VALUES ($1, $2, $3, COALESCE($4, ''), COALESCE($5, '')) RETURNING id`,
				*in.Code, *in.Name, *in.Type, in.CapacityNote, in.TemperatureNote).Scan(&id)
		} else {
			if !uuidPattern.MatchString(id) {
				return httpx.ErrNotFound
			}
			if in.Active != nil && !*in.Active {
				var held int
				if err := tx.QueryRow(ctx, `SELECT count(*) FROM inventory_lots WHERE location_id = $1 AND on_hand > 0`, id).Scan(&held); err != nil {
					return err
				}
				if held > 0 {
					return conflict("location_in_use", fmt.Sprintf("%d lot(s) are stored here; move them before closing the location.", held))
				}
			}
			var tag interface{ RowsAffected() int64 }
			tag, err = tx.Exec(ctx, `UPDATE storage_locations SET code = COALESCE($2, code), name = COALESCE($3, name), type = COALESCE($4, type),
				capacity_note = COALESCE($5, capacity_note), temperature_note = COALESCE($6, temperature_note), active = COALESCE($7, active),
				updated_at = now() WHERE id = $1`, id, in.Code, in.Name, in.Type, in.CapacityNote, in.TemperatureNote, in.Active)
			if err == nil && tag.RowsAffected() == 0 {
				return httpx.ErrNotFound
			}
		}
		if db.IsUniqueViolation(err, "") {
			return conflict("duplicate_location", "Another location has that code.")
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "inventory.location_saved", TargetType: "storage_location", TargetID: id})
	})
	if err != nil {
		return nil, err
	}
	return s.Locations(ctx)
}

func (s *Service) location(ctx context.Context, q db.Querier, id string) (typ string, active bool, err error) {
	if !uuidPattern.MatchString(id) {
		return "", false, fieldErr("location_id", "Choose a storage location.")
	}
	err = q.QueryRow(ctx, `SELECT type, active FROM storage_locations WHERE id = $1`, id).Scan(&typ, &active)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, fieldErr("location_id", "Storage location not found.")
	}
	if err == nil && !active {
		return "", false, fieldErr("location_id", "That location is closed.")
	}
	return
}

// ---------------------------------------------------------------- policies

// Policy is the allocation method for an ingredient category.
type Policy struct {
	Category  string     `json:"category"`
	Method    string     `json:"method"`
	Note      string     `json:"note"`
	Default   bool       `json:"default"`
	UpdatedAt *time.Time `json:"updated_at"`
}

var categories = []string{"produce", "meat", "poultry", "seafood", "dairy", "eggs", "grains", "legumes", "herbs_spices", "condiments",
	"oils_fats", "baking", "beverages", "packaged", "other"}

// Policies lists every category; one without a configured policy uses FIFO
// and says so.
func (s *Service) Policies(ctx context.Context) ([]Policy, error) {
	rows, err := s.pool.Query(ctx, `SELECT category, method, note, updated_at FROM inventory_policies`)
	if err != nil {
		return nil, err
	}
	set := map[string]Policy{}
	for rows.Next() {
		var p Policy
		var at time.Time
		if err := rows.Scan(&p.Category, &p.Method, &p.Note, &at); err != nil {
			rows.Close()
			return nil, err
		}
		p.UpdatedAt = &at
		set[p.Category] = p
	}
	rows.Close()
	out := []Policy{}
	for _, c := range categories {
		p, ok := set[c]
		if !ok {
			p = Policy{Category: c, Method: "fifo", Note: "Default: no policy configured.", Default: true}
		}
		out = append(out, p)
	}
	return out, nil
}

// PolicyInput sets a category's method, from an approved operating policy.
type PolicyInput struct {
	Category string `json:"category"`
	Method   string `json:"method"`
	Note     string `json:"note"`
}

func (s *Service) SavePolicy(ctx context.Context, actor string, in PolicyInput) ([]Policy, error) {
	in.Note = clean(in.Note)
	if !slices.Contains(categories, in.Category) {
		return nil, fieldErr("category", "Choose an ingredient category.")
	}
	if in.Method != "fifo" && in.Method != "fefo" {
		return nil, fieldErr("method", "FIFO (first received) or FEFO (first expiring).")
	}
	if runes(in.Note) < 3 || runes(in.Note) > 300 {
		return nil, fieldErr("note", "Name the approved policy this comes from.")
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO inventory_policies (category, method, note, updated_by) VALUES ($1, $2, $3, $4)
			ON CONFLICT (category) DO UPDATE SET method = $2, note = $3, updated_by = $4, updated_at = now()`, in.Category, in.Method, in.Note, nilIfEmpty(actor)); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "inventory.policy_saved", TargetType: "inventory_policy", TargetID: "00000000-0000-0000-0000-000000000000",
			Metadata: map[string]any{"category": in.Category, "method": in.Method}})
	})
	if err != nil {
		return nil, err
	}
	return s.Policies(ctx)
}

// methodFor returns the allocation method for an ingredient.
func methodFor(ctx context.Context, q db.Querier, ingredientID string) (string, error) {
	var m *string
	err := q.QueryRow(ctx, `SELECT p.method FROM ingredients i LEFT JOIN inventory_policies p ON p.category = i.category WHERE i.id = $1`, ingredientID).Scan(&m)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fieldErr("ingredient_id", "Ingredient not found.")
	}
	if err != nil {
		return "", err
	}
	if m == nil {
		return "fifo", nil
	}
	return *m, nil
}

// ---------------------------------------------------------------- receiving checklist

// ChecklistItem is one receiving inspection criterion from the approved procedure.
type ChecklistItem struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Criteria     string    `json:"criteria"`
	Categories   []string  `json:"categories"`
	ProcedureRef string    `json:"procedure_ref"`
	Position     int       `json:"position"`
	Active       bool      `json:"active"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (s *Service) Checklist(ctx context.Context) ([]ChecklistItem, error) {
	return s.checklist(ctx, s.pool, "", false)
}

// checklist lists the items; for a category, the active items that apply to it.
func (s *Service) checklist(ctx context.Context, q db.Querier, category string, applicable bool) ([]ChecklistItem, error) {
	rows, err := q.Query(ctx, `SELECT id, name, criteria, categories, procedure_ref, position, active, updated_at FROM receiving_checklist_items
		WHERE NOT $1 OR (active AND (cardinality(categories) = 0 OR $2 = ANY (categories))) ORDER BY active DESC, position, name`, applicable, category)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[ChecklistItem])
}

// ChecklistInput adds or edits an item.
type ChecklistInput struct {
	Name         *string   `json:"name"`
	Criteria     *string   `json:"criteria"`
	Categories   *[]string `json:"categories"`
	ProcedureRef *string   `json:"procedure_ref"`
	Position     *int      `json:"position"`
	Active       *bool     `json:"active"`
}

func (s *Service) SaveChecklistItem(ctx context.Context, actor, id string, in ChecklistInput) ([]ChecklistItem, error) {
	var f []httpx.FieldError
	bad := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	if in.Name != nil {
		*in.Name = clean(*in.Name)
		if n := runes(*in.Name); n < 1 || n > 120 {
			bad("name", "Name the check.")
		}
	} else if id == "" {
		bad("name", "Name the check.")
	}
	if in.Criteria != nil {
		*in.Criteria = strings.TrimSpace(*in.Criteria)
		if n := runes(*in.Criteria); n < 1 || n > 1000 {
			bad("criteria", "State the pass criteria from the procedure.")
		}
	} else if id == "" {
		bad("criteria", "State the pass criteria from the procedure.")
	}
	if in.Categories != nil {
		for _, c := range *in.Categories {
			if !slices.Contains(categories, c) {
				bad("categories", "Choose ingredient categories from the list.")
				break
			}
		}
	}
	if in.ProcedureRef != nil && runes(*in.ProcedureRef) > 120 {
		bad("procedure_ref", "At most 120 characters.")
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if id == "" {
			cats := []string{}
			if in.Categories != nil {
				cats = *in.Categories
			}
			pos := 0
			if in.Position != nil {
				pos = *in.Position
			}
			if err := tx.QueryRow(ctx, `INSERT INTO receiving_checklist_items (name, criteria, categories, procedure_ref, position) VALUES ($1, $2, $3, COALESCE($4, ''), $5) RETURNING id`,
				*in.Name, *in.Criteria, cats, in.ProcedureRef, pos).Scan(&id); err != nil {
				return err
			}
		} else {
			if !uuidPattern.MatchString(id) {
				return httpx.ErrNotFound
			}
			tag, err := tx.Exec(ctx, `UPDATE receiving_checklist_items SET name = COALESCE($2, name), criteria = COALESCE($3, criteria),
				categories = COALESCE($4, categories), procedure_ref = COALESCE($5, procedure_ref), position = COALESCE($6, position),
				active = COALESCE($7, active), updated_at = now() WHERE id = $1`, id, in.Name, in.Criteria, in.Categories, in.ProcedureRef, in.Position, in.Active)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return httpx.ErrNotFound
			}
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "inventory.checklist_saved", TargetType: "receiving_checklist_item", TargetID: id})
	})
	if err != nil {
		return nil, err
	}
	return s.Checklist(ctx)
}

// ---------------------------------------------------------------- the ledger

// lot is a lot row locked for a change.
type lot struct {
	ID, Code, IngredientID, IngredientName, Unit, Status, LocationID, ExpiryStatus string
	ExpiryDate                                                                     *time.Time
	OnHand, Reserved                                                               qty.Q
}

// usable reports whether the lot may be allocated today.
func (l *lot) usable(today time.Time) bool {
	if l.Status != "available" {
		return false
	}
	switch l.ExpiryStatus {
	case "none_applies":
		return true
	case "recorded":
		return l.ExpiryDate != nil && !l.ExpiryDate.Before(today)
	}
	return false
}

func lockLot(ctx context.Context, tx pgx.Tx, id string) (*lot, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	var l lot
	var onHand, reserved string
	err := tx.QueryRow(ctx, `SELECT l.id, l.code, l.ingredient_id, i.name, l.unit, l.status, l.location_id, l.expiry_status, l.expiry_date,
		trim_scale(l.on_hand)::text, trim_scale(l.reserved)::text
		FROM inventory_lots l JOIN ingredients i ON i.id = l.ingredient_id WHERE l.id = $1 FOR UPDATE OF l`, id).Scan(
		&l.ID, &l.Code, &l.IngredientID, &l.IngredientName, &l.Unit, &l.Status, &l.LocationID, &l.ExpiryStatus, &l.ExpiryDate, &onHand, &reserved)
	if err != nil {
		return nil, notFoundIf(err)
	}
	l.OnHand, l.Reserved = qty.MustParse(onHand), qty.MustParse(reserved)
	return &l, nil
}

// movement is one ledger row to apply to a locked lot.
type movement struct {
	Type             string
	OnHand, Reserved qty.Q
	Reason           string
	ToLocation       string
	ToStatus         string
	BatchID, PlanID  string
	RefType, RefID   string
	Actor            string
}

// apply writes the movement and keeps the lot's figures, in one transaction.
func apply(ctx context.Context, tx pgx.Tx, l *lot, m movement) error {
	onHand, reserved := l.OnHand.Add(m.OnHand), l.Reserved.Add(m.Reserved)
	if onHand.Sign() < 0 {
		return unprocessable("insufficient_stock", fmt.Sprintf("Lot %s holds %s %s; that would take it below zero.", l.Code, l.OnHand.String(), l.Unit))
	}
	if reserved.Sign() < 0 || reserved.Cmp(onHand) > 0 {
		return unprocessable("reserved_stock", fmt.Sprintf("Lot %s has %s %s reserved for production; release the reservation first.", l.Code, l.Reserved.String(), l.Unit))
	}
	fromLoc, fromStatus := l.LocationID, l.Status
	toLoc, toStatus := l.LocationID, l.Status
	if m.ToLocation != "" {
		toLoc = m.ToLocation
	}
	if m.ToStatus != "" {
		toStatus = m.ToStatus
	}
	if _, err := tx.Exec(ctx, `UPDATE inventory_lots SET on_hand = $2::numeric, reserved = $3::numeric, location_id = $4, status = $5, updated_at = now() WHERE id = $1`,
		l.ID, onHand.String(), reserved.String(), toLoc, toStatus); err != nil {
		return err
	}
	var fl, tl, fs, ts any
	if toLoc != fromLoc {
		fl, tl = fromLoc, toLoc
	}
	if toStatus != fromStatus {
		fs, ts = fromStatus, toStatus
	}
	if _, err := tx.Exec(ctx, `INSERT INTO inventory_movements (lot_id, ingredient_id, type, on_hand_delta, reserved_delta, on_hand_after, reserved_after, unit,
		reason, from_location, to_location, from_status, to_status, batch_id, plan_id, reference_type, reference_id, actor_id)
		VALUES ($1, $2, $3, $4::numeric, $5::numeric, $6::numeric, $7::numeric, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`,
		l.ID, l.IngredientID, m.Type, m.OnHand.String(), m.Reserved.String(), onHand.String(), reserved.String(), l.Unit, m.Reason, fl, tl, fs, ts,
		nilIfEmpty(m.BatchID), nilIfEmpty(m.PlanID), nilIfEmpty(m.RefType), nilIfEmpty(m.RefID), nilIfEmpty(m.Actor)); err != nil {
		return err
	}
	l.OnHand, l.Reserved, l.LocationID, l.Status = onHand, reserved, toLoc, toStatus
	return nil
}

// nextLotCode returns the next internal lot code for today (LOT-YYMMDD-NNN).
// The format is internal, not a lot numbering policy: the supplier's own lot
// reference is kept beside it.
func (s *Service) nextLotCode(ctx context.Context, q db.Querier) (string, error) {
	day := s.today().Format("060102")
	n, err := suppliers.NextNumber(ctx, q, "LOT", day)
	return fmt.Sprintf("LOT-%s-%03d", day, n), err
}
