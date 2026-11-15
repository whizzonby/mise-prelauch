package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
)

// Delivery is the admin view of a delivery.
type Delivery struct {
	ID                  string          `json:"id"`
	Number              string          `json:"number"`
	ObligationID        string          `json:"obligation_id"`
	OrderID             string          `json:"order_id"`
	OrderNumber         string          `json:"order_number"`
	CustomerID          string          `json:"customer_id"`
	CustomerName        string          `json:"customer_name"`
	OrderWeek           string          `json:"order_week"`
	Status              string          `json:"status"`
	Version             int             `json:"version"`
	Attempt             int             `json:"attempt"`
	ZoneID              *string         `json:"zone_id"`
	ZoneCode            *string         `json:"zone_code"`
	ZoneSource          string          `json:"zone_source"`
	ServiceDate         *string         `json:"service_date"`
	WindowID            *string         `json:"window_id"`
	WindowLabel         *string         `json:"window_label"`
	ScheduleConfirmedAt *time.Time      `json:"schedule_confirmed_at"`
	Address             json.RawMessage `json:"address"`
	AddressValidation   string          `json:"address_validation"`
	PackageCount        int             `json:"package_count"`
	RunID               *string         `json:"run_id"`
	RunNumber           *string         `json:"run_number"`
	PartnerName         *string         `json:"partner_name"`
	Stop                *int            `json:"stop_sequence"`
	DeliveredAt         *time.Time      `json:"delivered_at"`
	FailedAt            *time.Time      `json:"failed_at"`
	OpenExceptions      int             `json:"open_exceptions"`
	BlockingExceptions  int             `json:"blocking_exceptions"`
	HasProof            bool            `json:"has_proof"`
	CreatedAt           time.Time       `json:"created_at"`
	Packages            []PackageRef    `json:"packages,omitempty"`
	Events              []Tracking      `json:"events,omitempty"`
	Proofs              []Proof         `json:"proofs,omitempty"`
	Reschedules         []Reschedule    `json:"reschedule_requests,omitempty"`
	Returns             []Return        `json:"returns,omitempty"`
	Notifications       []Notification  `json:"notifications,omitempty"`
	Cases               []CaseLink      `json:"support_cases,omitempty"`
}

const deliveryCols = `d.id::text, d.number, d.obligation_id::text, d.order_id::text, o.number, d.customer_id::text,
	trim(COALESCE(u.first_name, '') || ' ' || COALESCE(u.last_name, '')), d.order_week::text, d.status, d.version, d.attempt, d.zone_id::text, z.code, d.zone_source,
	d.service_date::text, d.window_id::text, w.label, d.schedule_confirmed_at, d.address, d.address_validation, d.package_count,
	r.id::text, r.number, p.name, st.sequence::int, d.delivered_at, d.failed_at,
	(SELECT count(*) FROM delivery_exceptions e WHERE e.delivery_id = d.id AND e.status IN ('open', 'in_progress'))::int,
	(SELECT count(*) FROM delivery_exceptions e WHERE e.delivery_id = d.id AND e.blocking AND e.status IN ('open', 'in_progress'))::int,
	EXISTS (SELECT 1 FROM delivery_proofs pf WHERE pf.delivery_id = d.id AND pf.attempt = d.attempt AND pf.status = 'accepted'),
	d.created_at`

const deliveryFrom = ` FROM deliveries d JOIN orders o ON o.id = d.order_id JOIN customers c ON c.id = d.customer_id JOIN users u ON u.id = c.user_id
	LEFT JOIN delivery_service_zones z ON z.id = d.zone_id LEFT JOIN delivery_windows w ON w.id = d.window_id
	LEFT JOIN dispatch_stops st ON st.delivery_id = d.id AND st.status = 'active'
	LEFT JOIN dispatch_runs r ON r.id = st.run_id LEFT JOIN delivery_partners p ON p.id = r.partner_id`

func scanDelivery(row pgx.Row) (*Delivery, error) {
	var d Delivery
	err := row.Scan(&d.ID, &d.Number, &d.ObligationID, &d.OrderID, &d.OrderNumber, &d.CustomerID, &d.CustomerName, &d.OrderWeek, &d.Status, &d.Version,
		&d.Attempt, &d.ZoneID, &d.ZoneCode, &d.ZoneSource, &d.ServiceDate, &d.WindowID, &d.WindowLabel, &d.ScheduleConfirmedAt, &d.Address,
		&d.AddressValidation, &d.PackageCount, &d.RunID, &d.RunNumber, &d.PartnerName, &d.Stop, &d.DeliveredAt, &d.FailedAt, &d.OpenExceptions,
		&d.BlockingExceptions, &d.HasProof, &d.CreatedAt)
	return &d, err
}

// Scope is what a staff member may see: everything, or (couriers) only
// deliveries on runs assigned to them.
type Scope struct {
	All       bool
	CourierID string
}

// ScopeFor works out a principal's delivery scope; ok is false when they
// may see no deliveries at all.
func (s *Service) ScopeFor(ctx context.Context, p *identity.Principal) (Scope, bool) {
	if p.Can(identity.PermDeliveryRead) {
		return Scope{All: true}, true
	}
	if p.Can(identity.PermDeliveryReadAssigned) {
		if c, ok := courierOf(ctx, s.pool, p.UserID); ok {
			return Scope{CourierID: c}, true
		}
	}
	return Scope{}, false
}

func (sc Scope) clause(n int) string {
	if sc.All {
		return "true"
	}
	return `EXISTS (SELECT 1 FROM dispatch_stops s2 JOIN dispatch_runs r2 ON r2.id = s2.run_id
		WHERE s2.delivery_id = d.id AND s2.status IN ('active', 'done') AND r2.courier_id = $` + itoa(n) + `)`
}

// DeliveryFilter narrows the delivery list.
type DeliveryFilter struct {
	Status     string // a status, "active" (default), "unassigned", "all"
	Date       string
	ZoneID     string
	CustomerID string
	OrderID    string
	Query      string
}

func (s *Service) Deliveries(ctx context.Context, sc Scope, f DeliveryFilter) ([]Delivery, error) {
	where, args := []string{}, []any{}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, strings.ReplaceAll(cond, "$?", "$"+itoa(len(args))))
	}
	if !sc.All {
		args = append(args, sc.CourierID)
		where = append(where, sc.clause(len(args)))
	}
	switch f.Status {
	case "", "active":
		where = append(where, "d.status NOT IN ('delivered', 'returned', 'cancelled')")
	case "unassigned":
		where = append(where, "d.status = 'pending_assignment'")
	case "all":
	default:
		if _, ok := transitions[f.Status]; !ok {
			return nil, fieldErr("status", "Unknown status.")
		}
		add("d.status = $?", f.Status)
	}
	if f.Date != "" {
		d, ok := parseDay(f.Date)
		if !ok {
			return nil, fieldErr("date", "Use a date like 2026-11-19.")
		}
		add("d.service_date = $?", d)
	}
	for col, v := range map[string]string{"d.zone_id": f.ZoneID, "d.customer_id": f.CustomerID, "d.order_id": f.OrderID} {
		if v != "" {
			if !validID(v) {
				return []Delivery{}, nil
			}
			add(col+" = $?", v)
		}
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		add("(lower(d.number) LIKE $? OR lower(o.number) LIKE $?)", "%"+strings.ToLower(q)+"%")
	}
	sql := `SELECT ` + deliveryCols + deliveryFrom
	if len(where) > 0 {
		sql += ` WHERE ` + strings.Join(where, " AND ")
	}
	rows, err := s.pool.Query(ctx, sql+` ORDER BY d.service_date NULLS FIRST, z.code NULLS FIRST, st.sequence NULLS LAST, d.number LIMIT 500`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Delivery{}
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// Delivery returns one delivery with its history, within the scope. A
// delivery outside the scope answers not found.
func (s *Service) Delivery(ctx context.Context, sc Scope, id string) (*Delivery, error) {
	if !validID(id) {
		return nil, httpx.ErrNotFound
	}
	args := []any{id}
	cond := "true"
	if !sc.All {
		args = append(args, sc.CourierID)
		cond = sc.clause(2)
	}
	d, err := scanDelivery(s.pool.QueryRow(ctx, `SELECT `+deliveryCols+deliveryFrom+` WHERE d.id = $1 AND `+cond, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if s.Fulfillment != nil {
		if d.Packages, err = s.Fulfillment.Packages(ctx, s.pool, d.ObligationID); err != nil {
			return nil, err
		}
	}
	if d.Events, err = s.tracking(ctx, s.pool, id); err != nil {
		return nil, err
	}
	if d.Proofs, err = s.proofs(ctx, id); err != nil {
		return nil, err
	}
	if d.Reschedules, err = s.reschedules(ctx, id); err != nil {
		return nil, err
	}
	if d.Returns, err = s.returns(ctx, "r.delivery_id = $1", id); err != nil {
		return nil, err
	}
	if d.Notifications, err = s.notifications(ctx, id); err != nil {
		return nil, err
	}
	if d.Cases, err = s.caseLinks(ctx, id); err != nil {
		return nil, err
	}
	return d, nil
}

// Tracking is one tracking event as staff see it.
type Tracking struct {
	ID         int64      `json:"id"`
	Type       string     `json:"type"`
	From       *string    `json:"from"`
	To         *string    `json:"to"`
	Attempt    int        `json:"attempt"`
	Source     string     `json:"source"`
	ActorName  *string    `json:"actor_name"`
	OccurredAt time.Time  `json:"occurred_at"`
	RecordedAt time.Time  `json:"recorded_at"`
	Location   *string    `json:"location"`
	Notes      *string    `json:"notes"`
	Details    json.RawMessage `json:"details"`
}

func (s *Service) tracking(ctx context.Context, q db.Querier, id string) ([]Tracking, error) {
	rows, err := q.Query(ctx, `SELECT t.id, t.type, t.from_status, t.to_status, t.attempt::int, t.source,
		NULLIF(trim(COALESCE(u.first_name, '') || ' ' || COALESCE(u.last_name, '')), ''), t.occurred_at, t.recorded_at, t.location, t.notes, t.details
		FROM delivery_tracking_events t LEFT JOIN users u ON u.id = t.actor_user_id WHERE t.delivery_id = $1 ORDER BY t.id`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Tracking])
}

// ---------------------------------------------------------------- scheduling

type ScheduleInput struct {
	ServiceDate string `json:"service_date"`
	WindowID    string `json:"window_id"`
	ZoneID      string `json:"zone_id"` // manual zone assignment
	Reason      string `json:"reason"`  // required with a manual zone
	Version     *int   `json:"version"`
}

// Schedule confirms a delivery's zone, date and (approved) window. The date
// must be an approved service day for the zone. Only an unassigned delivery
// can be rescheduled this way.
func (s *Service) Schedule(ctx context.Context, actor, id string, in ScheduleInput) (*Delivery, error) {
	date, ok := parseDay(in.ServiceDate)
	if !ok {
		return nil, fieldErr("service_date", "Use a date like 2026-11-19.")
	}
	if in.WindowID != "" && !validID(in.WindowID) {
		return nil, fieldErr("window_id", "Choose a window.")
	}
	if in.ZoneID != "" && !validID(in.ZoneID) {
		return nil, fieldErr("zone_id", "Choose a zone.")
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		r, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := expect(r, in.Version); err != nil {
			return err
		}
		if r.Status != "pending_assignment" {
			return conflict("invalid_transition", "Only an unassigned delivery can be scheduled; this one is "+Human(r.Status)+". Unassign it first.")
		}
		zone, source := r.ZoneID, ""
		if in.ZoneID != "" && (zone == nil || *zone != in.ZoneID) {
			why, err := reason(in.Reason, 3, 500)
			if err != nil {
				return err
			}
			var st string
			if err := tx.QueryRow(ctx, `SELECT status FROM delivery_service_zones WHERE id = $1`, in.ZoneID).Scan(&st); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return fieldErr("zone_id", "Unknown zone.")
				}
				return err
			}
			if st != "active" {
				return unprocessable("zone_inactive", "That zone is not active.")
			}
			zone, source = &in.ZoneID, "manual"
			in.Reason = why
		}
		if zone == nil {
			return unprocessable("no_zone", "Assign a delivery zone first: no coverage rule covers this address.")
		}
		var validation string
		if err := tx.QueryRow(ctx, `SELECT address_validation FROM deliveries WHERE id = $1`, id).Scan(&validation); err != nil {
			return err
		}
		if validation == "not_serviceable" {
			return unprocessable("not_serviceable", "Mise does not deliver to this address's region. Resolve the address exception first.")
		}
		st, err := s.settings(ctx, tx)
		if err != nil {
			return err
		}
		if err := eligibleDate(st, r.OrderWeek, date); err != nil {
			return err
		}
		open, why, err := serviceDayOpen(ctx, tx, date, zone)
		if err != nil {
			return err
		}
		if !open {
			return unprocessable("not_a_service_day", why)
		}
		if in.WindowID != "" {
			var st string
			err := tx.QueryRow(ctx, `SELECT w.status FROM delivery_windows w JOIN delivery_service_days d ON d.id = w.service_day_id
				WHERE w.id = $1 AND d.service_date = $2 AND (d.zone_id IS NULL OR d.zone_id = $3)`, in.WindowID, date, *zone).Scan(&st)
			if errors.Is(err, pgx.ErrNoRows) {
				return fieldErr("window_id", "That window is not on this service day.")
			}
			if err != nil {
				return err
			}
			if st != "approved" {
				return unprocessable("window_not_approved", "Only approved windows can be promised.")
			}
		}
		extra := `, service_date = $3, window_id = $4, schedule_confirmed_at = now(), version = version + 1, updated_at = now()`
		args := []any{id, r.Status, date, nilIfEmpty(in.WindowID)}
		if source != "" {
			extra += `, zone_id = $5, zone_source = 'manual'`
			args = append(args, *zone)
		}
		if _, err := tx.Exec(ctx, `UPDATE deliveries SET status = $2`+extra+` WHERE id = $1`, args...); err != nil {
			return err
		}
		details := map[string]any{"service_date": in.ServiceDate, "window_id": in.WindowID}
		if source != "" {
			details["zone_id"], details["zone_reason"] = *zone, in.Reason
		}
		if err := s.track(ctx, tx, trackInput{DeliveryID: id, Type: "scheduled", Attempt: r.Attempt, Source: "staff", Actor: actor,
			Details: details, Key: "scheduled:" + itoa(r.Version)}); err != nil && !errors.Is(err, errDuplicate) {
			return err
		}
		if err := s.notify(ctx, tx, r, "scheduled", in.ServiceDate+":"+in.WindowID); err != nil {
			return err
		}
		return record(ctx, tx, actor, "delivery.scheduled", "delivery", id, details)
	})
	if err != nil {
		return nil, err
	}
	return s.Delivery(ctx, Scope{All: true}, id)
}

type AddressInput struct {
	Address Address `json:"address"`
	Reason  string  `json:"reason"`
	Version *int    `json:"version"`
}

// CorrectAddress replaces the snapshot before dispatch, with a reason. The
// old and new addresses are kept in the tracking history; after dispatch
// the database refuses the change.
func (s *Service) CorrectAddress(ctx context.Context, actor, id string, in AddressInput) (*Delivery, error) {
	why, err := reason(in.Reason, 5, 500)
	if err != nil {
		return nil, err
	}
	a := in.Address
	a.RecipientName, a.Line1, a.City, a.Region = strings.TrimSpace(a.RecipientName), strings.TrimSpace(a.Line1), strings.TrimSpace(a.City), strings.TrimSpace(a.Region)
	if a.RecipientName == "" || a.Line1 == "" || a.City == "" || a.Region == "" {
		return nil, fieldErr("address", "Give the recipient, street, city and region.")
	}
	if a.Country == "" {
		a.Country = "TT"
	}
	err = s.tx(ctx, func(tx pgx.Tx) error {
		r, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := expect(r, in.Version); err != nil {
			return err
		}
		if r.Status != "pending_assignment" && r.Status != "assigned" {
			return conflict("dispatched", "The address cannot change once a delivery is dispatched.")
		}
		if a.RegionLabel == "" {
			var old Address
			_ = json.Unmarshal(r.Address, &old)
			if old.Region == a.Region {
				a.RegionLabel = old.RegionLabel
			} else {
				a.RegionLabel = Human(a.Region)
			}
		}
		next, err := json.Marshal(a)
		if err != nil {
			return err
		}
		cov, err := CoverageFor(ctx, tx, a.Country, a.Region)
		if err != nil {
			return err
		}
		zoneSource := "none"
		if cov.ZoneID != "" {
			zoneSource = "coverage_rule"
		}
		if _, err := tx.Exec(ctx, `UPDATE deliveries SET address = $2, address_validation = $3, zone_id = $4, zone_source = $5,
			version = version + 1, updated_at = now() WHERE id = $1`, id, next, cov.Validation, nilIfEmpty(cov.ZoneID), zoneSource); err != nil {
			return err
		}
		if err := s.track(ctx, tx, trackInput{DeliveryID: id, Type: "address_corrected", Attempt: r.Attempt, Source: "staff", Actor: actor, Notes: why,
			Details: map[string]any{"previous": json.RawMessage(r.Address), "next": json.RawMessage(next)}, Key: "address:" + itoa(r.Version)}); err != nil {
			return err
		}
		return record(ctx, tx, actor, "delivery.address_corrected", "delivery", id, map[string]any{"reason": why, "validation": cov.Validation})
	})
	if err != nil {
		return nil, err
	}
	return s.Delivery(ctx, Scope{All: true}, id)
}
