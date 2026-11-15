package delivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
)

// Run is a dispatch run: the deliveries one partner (and optionally one
// courier) takes out on one date, in stop order.
type Run struct {
	ID          string     `json:"id"`
	Number      string     `json:"number"`
	ServiceDate string     `json:"service_date"`
	PartnerID   string     `json:"partner_id"`
	PartnerName string     `json:"partner_name"`
	PartnerMode string     `json:"partner_mode"`
	CourierID   *string    `json:"courier_id"`
	CourierName *string    `json:"courier_name"`
	ZoneID      *string    `json:"zone_id"`
	ZoneCode    *string    `json:"zone_code"`
	Status      string     `json:"status"`
	Version     int        `json:"version"`
	Notes       string     `json:"notes"`
	StopCount   int        `json:"stop_count"`
	Packages    int        `json:"package_count"`
	CreatedAt   time.Time  `json:"created_at"`
	FinalizedAt *time.Time `json:"finalized_at"`
	ReleasedAt  *time.Time `json:"released_at"`
	CompletedAt *time.Time `json:"completed_at"`
	Stops       []Stop     `json:"stops,omitempty"`
	Manifest    *Manifest  `json:"manifest,omitempty"`
	Handoffs    []Handoff  `json:"handoffs,omitempty"`
}

type Stop struct {
	ID               string  `json:"id"`
	Sequence         int     `json:"sequence"`
	DeliveryID       string  `json:"delivery_id"`
	DeliveryNumber   string  `json:"delivery_number"`
	DeliveryStatus   string  `json:"delivery_status"`
	OrderNumber      string  `json:"order_number"`
	Recipient        string  `json:"recipient"`
	AddressLine      string  `json:"address_line"`
	Region           string  `json:"region"`
	WindowLabel      *string `json:"window_label"`
	PackageCount     int     `json:"package_count"`
	Status           string  `json:"status"`
	CapacityOverride *string `json:"capacity_override"`
	PartnerOverride  *string `json:"partner_override"`
}

const runCols = `r.id::text, r.number, r.service_date::text, r.partner_id::text, p.name, p.integration_mode, r.courier_id::text, c.name,
	r.zone_id::text, z.code, r.status, r.version, r.notes,
	(SELECT count(*) FROM dispatch_stops s WHERE s.run_id = r.id AND s.status IN ('active', 'done'))::int,
	(SELECT COALESCE(sum(d.package_count), 0) FROM dispatch_stops s JOIN deliveries d ON d.id = s.delivery_id WHERE s.run_id = r.id AND s.status IN ('active', 'done'))::int,
	r.created_at, r.finalized_at, r.released_at, r.completed_at`

const runFrom = ` FROM dispatch_runs r JOIN delivery_partners p ON p.id = r.partner_id LEFT JOIN delivery_couriers c ON c.id = r.courier_id
	LEFT JOIN delivery_service_zones z ON z.id = r.zone_id`

func scanRun(row pgx.Row) (*Run, error) {
	var r Run
	err := row.Scan(&r.ID, &r.Number, &r.ServiceDate, &r.PartnerID, &r.PartnerName, &r.PartnerMode, &r.CourierID, &r.CourierName, &r.ZoneID,
		&r.ZoneCode, &r.Status, &r.Version, &r.Notes, &r.StopCount, &r.Packages, &r.CreatedAt, &r.FinalizedAt, &r.ReleasedAt, &r.CompletedAt)
	return &r, err
}

func (sc Scope) runClause(n int) string {
	if sc.All {
		return "true"
	}
	return "r.courier_id = $" + itoa(n)
}

func (s *Service) Runs(ctx context.Context, sc Scope, date, status string) ([]Run, error) {
	where, args := []string{}, []any{}
	if !sc.All {
		args = append(args, sc.CourierID)
		where = append(where, sc.runClause(len(args)))
	}
	if date != "" {
		d, ok := parseDay(date)
		if !ok {
			return nil, fieldErr("date", "Use a date like 2026-11-19.")
		}
		args = append(args, d)
		where = append(where, "r.service_date = $"+itoa(len(args)))
	}
	if status != "" && status != "all" {
		args = append(args, status)
		where = append(where, "r.status = $"+itoa(len(args)))
	}
	sql := `SELECT ` + runCols + runFrom
	if len(where) > 0 {
		sql += ` WHERE ` + strings.Join(where, " AND ")
	}
	rows, err := s.pool.Query(ctx, sql+` ORDER BY r.service_date DESC, r.number LIMIT 300`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func (s *Service) Run(ctx context.Context, sc Scope, id string) (*Run, error) {
	return s.run(ctx, s.pool, sc, id)
}

func (s *Service) run(ctx context.Context, q db.Querier, sc Scope, id string) (*Run, error) {
	if !validID(id) {
		return nil, httpx.ErrNotFound
	}
	args := []any{id}
	cond := "true"
	if !sc.All {
		args = append(args, sc.CourierID)
		cond = sc.runClause(2)
	}
	r, err := scanRun(q.QueryRow(ctx, `SELECT `+runCols+runFrom+` WHERE r.id = $1 AND `+cond, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if r.Stops, err = s.stops(ctx, q, id); err != nil {
		return nil, err
	}
	if r.Manifest, err = s.currentManifest(ctx, q, id); err != nil {
		return nil, err
	}
	r.Handoffs, err = s.handoffs(ctx, q, id)
	return r, err
}

func (s *Service) stops(ctx context.Context, q db.Querier, runID string) ([]Stop, error) {
	rows, err := q.Query(ctx, `SELECT s.id::text, s.sequence::int, d.id::text, d.number, d.status, o.number,
		COALESCE(d.address->>'recipient_name', ''), COALESCE(d.address->>'line1', ''), COALESCE(d.address->>'region_label', ''), w.label,
		d.package_count::int, s.status, s.capacity_override, s.partner_override
		FROM dispatch_stops s JOIN deliveries d ON d.id = s.delivery_id JOIN orders o ON o.id = d.order_id
		LEFT JOIN delivery_windows w ON w.id = d.window_id
		WHERE s.run_id = $1 AND s.status IN ('active', 'done') ORDER BY s.sequence`, runID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Stop])
}

type RunInput struct {
	ServiceDate string `json:"service_date"`
	PartnerID   string `json:"partner_id"`
	CourierID   string `json:"courier_id"`
	ZoneID      string `json:"zone_id"`
	Notes       string `json:"notes"`
}

// partnerUsable says whether a partner may take live assignments: approved,
// or (only where a documented exception policy allows it) with a reason.
func (s *Service) partnerUsable(ctx context.Context, q db.Querier, partnerID, override string) (string, error) {
	var status, name string
	err := q.QueryRow(ctx, `SELECT status, name FROM delivery_partners WHERE id = $1`, partnerID).Scan(&status, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fieldErr("partner_id", "Unknown partner.")
	}
	if err != nil {
		return "", err
	}
	if status == "approved" {
		return "", nil
	}
	if status == "suspended" || status == "retired" {
		return "", unprocessable("partner_not_approved", name+" is "+status+" and cannot take deliveries.")
	}
	st, err := s.settings(ctx, q)
	if err != nil {
		return "", err
	}
	if !st.AllowUnapprovedPartners {
		return "", unprocessable("partner_not_approved", name+" is not an approved delivery partner.")
	}
	why := strings.TrimSpace(override)
	if runes(why) < 3 {
		return "", unprocessable("partner_not_approved", name+" is not approved. Under policy "+st.UnapprovedPartnerPolicyRef+", give a reason to use it anyway.")
	}
	return "Policy " + st.UnapprovedPartnerPolicyRef + ": " + why, nil
}

func (s *Service) CreateRun(ctx context.Context, actor string, in RunInput, partnerOverride string) (*Run, error) {
	date, ok := parseDay(in.ServiceDate)
	if !ok {
		return nil, fieldErr("service_date", "Use a date like 2026-11-19.")
	}
	if !validID(in.PartnerID) {
		return nil, fieldErr("partner_id", "Choose a delivery partner.")
	}
	for f, v := range map[string]string{"courier_id": in.CourierID, "zone_id": in.ZoneID} {
		if v != "" && !validID(v) {
			return nil, fieldErr(f, "Not a valid id.")
		}
	}
	in.Notes = strings.TrimSpace(in.Notes)
	if runes(in.Notes) > 1000 {
		return nil, fieldErr("notes", "Keep notes under 1,000 characters.")
	}
	var id string
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.partnerUsable(ctx, tx, in.PartnerID, partnerOverride); err != nil {
			return err
		}
		if in.CourierID != "" {
			var partner, status string
			if err := tx.QueryRow(ctx, `SELECT partner_id::text, status FROM delivery_couriers WHERE id = $1`, in.CourierID).Scan(&partner, &status); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return fieldErr("courier_id", "Unknown courier.")
				}
				return err
			}
			if partner != in.PartnerID || status != "active" {
				return fieldErr("courier_id", "Choose an active courier of this partner.")
			}
		}
		number, err := NextNumber(ctx, tx, "RUN", date.Format("20060102"))
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO dispatch_runs (number, service_date, partner_id, courier_id, zone_id, notes, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id::text`, number, date, in.PartnerID, nilIfEmpty(in.CourierID), nilIfEmpty(in.ZoneID),
			in.Notes, nilIfEmpty(actor)).Scan(&id); err != nil {
			if db.IsForeignKeyViolation(err) {
				return fieldErr("zone_id", "Unknown zone.")
			}
			return err
		}
		return record(ctx, tx, actor, "delivery.run_created", "dispatch_run", id, map[string]any{"date": in.ServiceDate, "partner_id": in.PartnerID})
	})
	if err != nil {
		return nil, err
	}
	return s.Run(ctx, Scope{All: true}, id)
}

func (s *Service) lockRun(ctx context.Context, tx pgx.Tx, id string, from ...string) (*Run, error) {
	if !validID(id) {
		return nil, httpx.ErrNotFound
	}
	var r Run
	var date time.Time
	err := tx.QueryRow(ctx, `SELECT id::text, number, service_date, partner_id::text, courier_id::text, zone_id::text, status, version
		FROM dispatch_runs WHERE id = $1 FOR UPDATE`, id).Scan(&r.ID, &r.Number, &date, &r.PartnerID, &r.CourierID, &r.ZoneID, &r.Status, &r.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	r.ServiceDate = date.Format(day)
	if len(from) > 0 && !slices.Contains(from, r.Status) {
		return nil, conflict("invalid_transition", "Run "+r.Number+" is "+Human(r.Status)+".")
	}
	return &r, nil
}

type StopInput struct {
	DeliveryID       string `json:"delivery_id"`
	CapacityOverride string `json:"capacity_override"`
	PartnerOverride  string `json:"partner_override"`
}

// AddStop assigns a delivery to a draft run. It checks the partner, the
// partner's zones, the schedule and capacity, all under row locks; the
// database's one-active-stop index is the final guard against a delivery
// being assigned twice.
func (s *Service) AddStop(ctx context.Context, p *identity.Principal, runID string, in StopInput) (*Run, error) {
	if !validID(in.DeliveryID) {
		return nil, fieldErr("delivery_id", "Choose a delivery.")
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		run, err := s.lockRun(ctx, tx, runID, "draft")
		if err != nil {
			return err
		}
		partnerNote, err := s.partnerUsable(ctx, tx, run.PartnerID, in.PartnerOverride)
		if err != nil {
			return err
		}
		d, err := s.lock(ctx, tx, in.DeliveryID)
		if err != nil {
			return err
		}
		if d.Status != "pending_assignment" {
			if d.Status == "assigned" {
				return conflict("already_assigned", "Delivery "+d.Number+" is already assigned to a run.")
			}
			return conflict("invalid_transition", "Delivery "+d.Number+" is "+Human(d.Status)+".")
		}
		var confirmed *time.Time
		if err := tx.QueryRow(ctx, `SELECT schedule_confirmed_at FROM deliveries WHERE id = $1`, d.ID).Scan(&confirmed); err != nil {
			return err
		}
		if d.ServiceDate == nil || confirmed == nil || d.ServiceDate.Format(day) != run.ServiceDate {
			return unprocessable("not_scheduled", "Delivery "+d.Number+" is not scheduled for "+run.ServiceDate+".")
		}
		if d.ZoneID == nil {
			return unprocessable("no_zone", "Delivery "+d.Number+" has no zone.")
		}
		// The rule is checked again here: it may have changed since scheduling.
		st, err := s.settings(ctx, tx)
		if err != nil {
			return err
		}
		if err := eligibleDate(st, d.OrderWeek, *d.ServiceDate); err != nil {
			return err
		}
		if run.ZoneID != nil && *run.ZoneID != *d.ZoneID {
			return unprocessable("wrong_zone", "Delivery "+d.Number+" is in another zone than this run.")
		}
		var covers bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM delivery_partner_zones WHERE partner_id = $1 AND zone_id = $2)`, run.PartnerID, *d.ZoneID).Scan(&covers); err != nil {
			return err
		}
		if !covers {
			return unprocessable("partner_coverage", "The partner does not cover this delivery's zone.")
		}
		if n, err := BlockingExceptions(ctx, tx, "delivery_id", d.ID); err != nil {
			return err
		} else if n > 0 {
			return unprocessable("blocked", "Delivery "+d.Number+" has an open blocking exception.")
		}
		known, full, err := checkCapacity(ctx, tx, *d.ServiceDate, d.ZoneID, d.WindowID, run.PartnerID, run.CourierID)
		if err != nil {
			return err
		}
		override := strings.TrimSpace(in.CapacityOverride)
		if !known || full != "" {
			msg := full
			if !known {
				msg = "Capacity is not configured for this date, zone and partner, so it is unknown."
			}
			if override == "" {
				return unprocessable("capacity", msg+" An explicit override with a reason is needed.")
			}
			if !p.Can(identity.PermDeliveryDispatch) {
				return httpx.ErrForbidden
			}
			if runes(override) < 3 || runes(override) > 500 {
				return fieldErr("capacity_override", "Give a reason for going beyond known capacity (3 to 500 characters).")
			}
			override = msg + " Override: " + override
		} else {
			override = ""
		}
		var seq int
		if err := tx.QueryRow(ctx, `SELECT COALESCE(max(sequence), 0) + 1 FROM dispatch_stops WHERE run_id = $1 AND status IN ('active', 'done')`, runID).Scan(&seq); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO dispatch_stops (run_id, delivery_id, attempt, sequence, capacity_override, partner_override, added_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, runID, d.ID, d.Attempt, seq, nilIfEmpty(override), nilIfEmpty(partnerNote), nilIfEmpty(p.UserID)); err != nil {
			if db.IsUniqueViolation(err, "dispatch_stops_active_delivery") {
				return conflict("already_assigned", "Delivery "+d.Number+" is already assigned to a run.")
			}
			return err
		}
		if err := s.move(ctx, tx, d, "assigned", trackInput{Source: "staff", Actor: p.UserID, PartnerID: run.PartnerID,
			Notes: "Assigned to run " + run.Number + ".", Key: "assigned:" + runID + ":" + itoa(d.Version)}, ""); err != nil {
			return err
		}
		if err := s.bumpRun(ctx, tx, runID); err != nil {
			return err
		}
		if err := event(ctx, tx, "delivery.assigned", "delivery", d.ID, runID+":"+itoa(d.Attempt), map[string]any{"run_id": runID, "partner_id": run.PartnerID}); err != nil {
			return err
		}
		return record(ctx, tx, p.UserID, "delivery.assigned", "delivery", d.ID, map[string]any{"run": run.Number, "capacity_override": override, "partner_override": partnerNote})
	})
	if err != nil {
		return nil, err
	}
	return s.Run(ctx, Scope{All: true}, runID)
}

func (s *Service) bumpRun(ctx context.Context, tx pgx.Tx, runID string) error {
	_, err := tx.Exec(ctx, `UPDATE dispatch_runs SET version = version + 1, updated_at = now() WHERE id = $1`, runID)
	return err
}

// RemoveStop unassigns a delivery from a draft run.
func (s *Service) RemoveStop(ctx context.Context, actor, runID, stopID, why string) (*Run, error) {
	why, err := reason(why, 3, 500)
	if err != nil {
		return nil, err
	}
	err = s.tx(ctx, func(tx pgx.Tx) error {
		run, err := s.lockRun(ctx, tx, runID, "draft")
		if err != nil {
			return err
		}
		var deliveryID string
		err = tx.QueryRow(ctx, `SELECT delivery_id::text FROM dispatch_stops WHERE id = $1 AND run_id = $2 AND status = 'active'`, stopID, runID).Scan(&deliveryID)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		d, err := s.lock(ctx, tx, deliveryID)
		if err != nil {
			return err
		}
		if err := s.unassign(ctx, tx, actor, run, d, why); err != nil {
			return err
		}
		return s.resequence(ctx, tx, runID)
	})
	if err != nil {
		return nil, err
	}
	return s.Run(ctx, Scope{All: true}, runID)
}

// unassign removes the active stop of a locked delivery and returns it to
// pending_assignment (the caller holds the run lock).
func (s *Service) unassign(ctx context.Context, tx pgx.Tx, actor string, run *Run, d *row, why string) error {
	if _, err := tx.Exec(ctx, `UPDATE dispatch_stops SET status = 'removed', removed_by = $2, removed_at = now(), removal_reason = $3
		WHERE delivery_id = $1 AND status = 'active'`, d.ID, nilIfEmpty(actor), why); err != nil {
		return err
	}
	if err := s.move(ctx, tx, d, "pending_assignment", trackInput{Type: "unassigned", Source: "staff", Actor: actor,
		Notes: "Removed from run " + run.Number + ": " + why, Key: "unassigned:" + run.ID + ":" + itoa(d.Version)}, ""); err != nil {
		return err
	}
	return s.bumpRun(ctx, tx, run.ID)
}

func (s *Service) resequence(ctx context.Context, tx pgx.Tx, runID string) error {
	_, err := tx.Exec(ctx, `UPDATE dispatch_stops s SET sequence = n.seq FROM (
		SELECT id, row_number() OVER (ORDER BY sequence, added_at) AS seq FROM dispatch_stops WHERE run_id = $1 AND status IN ('active', 'done')
	) n WHERE s.id = n.id`, runID)
	return err
}

// Sequence sets the manual stop order of a draft run. Route optimisation is
// not offered: there is no routing engine.
func (s *Service) Sequence(ctx context.Context, actor, runID string, stopIDs []string) (*Run, error) {
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.lockRun(ctx, tx, runID, "draft"); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id::text FROM dispatch_stops WHERE run_id = $1 AND status = 'active' ORDER BY sequence`, runID)
		if err != nil {
			return err
		}
		current, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		if len(current) != len(stopIDs) {
			return fieldErr("stop_ids", "List every stop of the run exactly once.")
		}
		for _, id := range stopIDs {
			if !slices.Contains(current, strings.ToLower(id)) {
				return fieldErr("stop_ids", "List every stop of the run exactly once.")
			}
		}
		if len(dedupe(stopIDs)) != len(stopIDs) {
			return fieldErr("stop_ids", "List every stop of the run exactly once.")
		}
		for i, id := range stopIDs {
			if _, err := tx.Exec(ctx, `UPDATE dispatch_stops SET sequence = $2 WHERE id = $1`, id, i+1); err != nil {
				return err
			}
		}
		if err := s.bumpRun(ctx, tx, runID); err != nil {
			return err
		}
		return record(ctx, tx, actor, "delivery.run_sequenced", "dispatch_run", runID, map[string]any{"stops": stopIDs})
	})
	if err != nil {
		return nil, err
	}
	return s.Run(ctx, Scope{All: true}, runID)
}

// ---------------------------------------------------------------- manifests

type Manifest struct {
	ID           string          `json:"id"`
	Revision     int             `json:"revision"`
	Status       string          `json:"status"`
	ContentHash  string          `json:"content_hash"`
	StopCount    int             `json:"stop_count"`
	PackageCount int             `json:"package_count"`
	GeneratedAt  time.Time       `json:"generated_at"`
	Content      ManifestContent `json:"content"`
}

// ManifestContent is the frozen run sheet. It holds what a courier needs to
// deliver (recipient, address, phone, instructions, packages) and nothing
// else: no email, no order amounts, no allergen or account details.
type ManifestContent struct {
	RunNumber   string         `json:"run_number"`
	ServiceDate string         `json:"service_date"`
	Partner     string         `json:"partner"`
	Courier     *string        `json:"courier"`
	Zone        *string        `json:"zone"`
	Stops       []ManifestStop `json:"stops"`
	Handling    string         `json:"handling"`
	Generated   time.Time      `json:"generated_at"`
}

type ManifestStop struct {
	Sequence     int          `json:"sequence"`
	Delivery     string       `json:"delivery"`
	Order        string       `json:"order"`
	Recipient    string       `json:"recipient"`
	Phone        string       `json:"phone"`
	Address      string       `json:"address"`
	Region       string       `json:"region"`
	Instructions string       `json:"instructions"`
	Window       string       `json:"window"`
	Packages     []PackageRef `json:"packages"`
}

func (s *Service) currentManifest(ctx context.Context, q db.Querier, runID string) (*Manifest, error) {
	var m Manifest
	var content []byte
	err := q.QueryRow(ctx, `SELECT id::text, revision, status, content_hash, stop_count, package_count, generated_at, content
		FROM dispatch_manifests WHERE run_id = $1 AND status = 'current'`, runID).
		Scan(&m.ID, &m.Revision, &m.Status, &m.ContentHash, &m.StopCount, &m.PackageCount, &m.GeneratedAt, &content)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, json.Unmarshal(content, &m.Content)
}

// Finalize freezes a draft run's manifest. The run stays locked while the
// manifest is written, so no stop can change underneath it.
func (s *Service) Finalize(ctx context.Context, actor, runID string) (*Run, error) {
	err := s.tx(ctx, func(tx pgx.Tx) error {
		run, err := s.lockRun(ctx, tx, runID, "draft")
		if err != nil {
			return err
		}
		// Approval was checked per stop (with any recorded override); a
		// partner suspended since then cannot take the run.
		var pst string
		if err := tx.QueryRow(ctx, `SELECT status FROM delivery_partners WHERE id = $1`, run.PartnerID).Scan(&pst); err != nil {
			return err
		}
		if pst == "suspended" || pst == "retired" {
			return unprocessable("partner_not_approved", "The run's partner is "+pst+".")
		}
		full, err := s.run(ctx, tx, Scope{All: true}, runID)
		if err != nil {
			return err
		}
		if len(full.Stops) == 0 {
			return unprocessable("empty_run", "Add at least one delivery before finalizing.")
		}
		content := ManifestContent{RunNumber: full.Number, ServiceDate: full.ServiceDate, Partner: full.PartnerName, Courier: full.CourierName,
			Zone: full.ZoneCode, Generated: s.Now().UTC().Truncate(time.Second),
			Handling: "Meal-kit ingredients. Keep packages upright and out of direct sun. Hand over only to the recipient or as instructed."}
		packages := 0
		for _, st := range full.Stops {
			var a Address
			var addr []byte
			var obligation string
			if err := tx.QueryRow(ctx, `SELECT address, obligation_id::text FROM deliveries WHERE id = $1`, st.DeliveryID).Scan(&addr, &obligation); err != nil {
				return err
			}
			_ = json.Unmarshal(addr, &a)
			pkgs, err := s.Fulfillment.Packages(ctx, tx, obligation)
			if err != nil {
				return err
			}
			packages += len(pkgs)
			ms := ManifestStop{Sequence: st.Sequence, Delivery: st.DeliveryNumber, Order: st.OrderNumber, Recipient: a.RecipientName,
				Address: joinAddress(a), Region: a.RegionLabel, Packages: pkgs}
			if a.Phone != nil {
				ms.Phone = *a.Phone
			}
			if a.Instructions != nil {
				ms.Instructions = *a.Instructions
			}
			if st.WindowLabel != nil {
				ms.Window = *st.WindowLabel
			}
			content.Stops = append(content.Stops, ms)
		}
		raw, err := json.Marshal(content)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(raw)
		if _, err := tx.Exec(ctx, `UPDATE dispatch_manifests SET status = 'superseded' WHERE run_id = $1 AND status = 'current'`, runID); err != nil {
			return err
		}
		var rev int
		if err := tx.QueryRow(ctx, `SELECT COALESCE(max(revision), 0) + 1 FROM dispatch_manifests WHERE run_id = $1`, runID).Scan(&rev); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO dispatch_manifests (run_id, revision, content, content_hash, stop_count, package_count, generated_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, runID, rev, raw, hex.EncodeToString(sum[:]), len(content.Stops), packages, nilIfEmpty(actor)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE dispatch_runs SET status = 'finalized', finalized_at = now(), version = version + 1, updated_at = now() WHERE id = $1`, runID); err != nil {
			return err
		}
		return record(ctx, tx, actor, "delivery.run_finalized", "dispatch_run", runID, map[string]any{"revision": rev, "stops": len(content.Stops), "packages": packages})
	})
	if err != nil {
		return nil, err
	}
	return s.Run(ctx, Scope{All: true}, runID)
}

func joinAddress(a Address) string {
	parts := []string{a.Line1}
	for _, p := range []*string{a.Line2, a.Area} {
		if p != nil && strings.TrimSpace(*p) != "" {
			parts = append(parts, *p)
		}
	}
	parts = append(parts, a.City)
	return strings.Join(parts, ", ")
}

// Reopen returns a finalized run to draft; its manifest is superseded.
func (s *Service) Reopen(ctx context.Context, actor, runID, why string) (*Run, error) {
	why, err := reason(why, 3, 500)
	if err != nil {
		return nil, err
	}
	err = s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.lockRun(ctx, tx, runID, "finalized"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE dispatch_manifests SET status = 'superseded' WHERE run_id = $1 AND status = 'current'`, runID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE dispatch_runs SET status = 'draft', finalized_at = NULL, version = version + 1, updated_at = now() WHERE id = $1`, runID); err != nil {
			return err
		}
		return record(ctx, tx, actor, "delivery.run_reopened", "dispatch_run", runID, map[string]any{"reason": why})
	})
	if err != nil {
		return nil, err
	}
	return s.Run(ctx, Scope{All: true}, runID)
}

// Release sends a finalized run out: its deliveries become ready for pickup
// and the partner's adapter is told (a manual partner works from the
// manifest). Nothing is handed over until a handoff is recorded.
func (s *Service) Release(ctx context.Context, actor, runID string) (*Run, error) {
	err := s.tx(ctx, func(tx pgx.Tx) error {
		run, err := s.lockRun(ctx, tx, runID, "finalized")
		if err != nil {
			return err
		}
		if _, err := s.partnerUsable(ctx, tx, run.PartnerID, "released under the recorded stop overrides"); err != nil {
			return err
		}
		var mode string
		if err := tx.QueryRow(ctx, `SELECT integration_mode FROM delivery_partners WHERE id = $1`, run.PartnerID).Scan(&mode); err != nil {
			return err
		}
		carrier := s.adapterFor(mode)
		if carrier == nil {
			return unprocessable("no_adapter", "No working adapter exists for "+Human(mode)+" partners; use a manual partner.")
		}
		rows, err := tx.Query(ctx, `SELECT delivery_id::text FROM dispatch_stops WHERE run_id = $1 AND status = 'active' ORDER BY sequence`, runID)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		for _, id := range ids {
			d, err := s.lock(ctx, tx, id)
			if err != nil {
				return err
			}
			if d.Status == "cancelled" {
				continue
			}
			ref, err := carrier.CreateShipment(ctx, Shipment{DeliveryID: d.ID, Number: d.Number})
			if err != nil {
				return err
			}
			if err := s.move(ctx, tx, d, "ready_for_pickup", trackInput{Source: "system", Actor: actor, PartnerID: run.PartnerID,
				Notes: "Run " + run.Number + " released.", Details: map[string]any{"shipment": ref.Reference}, Key: "released:" + runID + ":" + itoa(d.Attempt)}, ""); err != nil {
				return err
			}
			if err := event(ctx, tx, "delivery.dispatched", "delivery", d.ID, runID+":"+itoa(d.Attempt), map[string]any{"run_id": runID}); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE dispatch_runs SET status = 'released', released_by = $2, released_at = now(), version = version + 1,
			updated_at = now() WHERE id = $1`, runID, nilIfEmpty(actor)); err != nil {
			return err
		}
		return record(ctx, tx, actor, "delivery.run_released", "dispatch_run", runID, map[string]any{"deliveries": len(ids)})
	})
	if err != nil {
		return nil, err
	}
	return s.Run(ctx, Scope{All: true}, runID)
}

// ---------------------------------------------------------------- custody

type Handoff struct {
	ID            string    `json:"id"`
	FromParty     string    `json:"from_party"`
	ToParty       string    `json:"to_party"`
	HandedAt      time.Time `json:"handed_at"`
	Location      *string   `json:"location"`
	Accepted      int       `json:"accepted_count"`
	Rejected      int       `json:"rejected_count"`
	Missing       int       `json:"missing_count"`
	Disputed      bool      `json:"disputed"`
	EvidenceRef   string    `json:"evidence_ref"`
	Notes         string    `json:"notes"`
	ActorName     *string   `json:"actor_name"`
	RecordedAt    time.Time `json:"recorded_at"`
}

func (s *Service) handoffs(ctx context.Context, q db.Querier, runID string) ([]Handoff, error) {
	rows, err := q.Query(ctx, `SELECT h.id::text, h.from_party, h.to_party, h.handed_at, h.location, h.accepted_count, h.rejected_count,
		h.missing_count, h.disputed, h.evidence_ref, h.notes, NULLIF(trim(COALESCE(u.first_name, '') || ' ' || COALESCE(u.last_name, '')), ''), h.created_at
		FROM delivery_handoffs h LEFT JOIN users u ON u.id = h.actor_user_id WHERE h.run_id = $1 ORDER BY h.created_at`, runID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Handoff])
}

type HandoffInput struct {
	FromParty        string           `json:"from_party"`
	ToParty          string           `json:"to_party"`
	HandedAt         *time.Time       `json:"handed_at"`
	Location         string           `json:"location"`
	LocationVerified bool             `json:"location_verified"`
	Packages         []PackageOutcome `json:"packages"`
	EvidenceRef      string           `json:"evidence_ref"`
	Notes            string           `json:"notes"`
	IdempotencyKey   string           `json:"idempotency_key"`
}

type PackageOutcome struct {
	PackageID string `json:"package_id"`
	Outcome   string `json:"outcome"` // accepted, rejected, missing
	Note      string `json:"note"`
}

// RecordHandoff records custody of packages passing to the partner or
// courier. A delivery is picked up only when all its packages are accepted.
// A rejected or missing package makes the handoff disputed and raises a
// blocking exception on its delivery. The same handoff sent twice (same
// key) is one record; a package already in custody cannot be accepted again.
func (s *Service) RecordHandoff(ctx context.Context, actor, runID string, in HandoffInput) (*Run, error) {
	in.FromParty, in.ToParty = strings.TrimSpace(in.FromParty), strings.TrimSpace(in.ToParty)
	in.IdempotencyKey, in.Notes, in.EvidenceRef = strings.TrimSpace(in.IdempotencyKey), strings.TrimSpace(in.Notes), strings.TrimSpace(in.EvidenceRef)
	if in.FromParty == "" {
		in.FromParty = "Mise kitchen"
	}
	var f []httpx.FieldError
	bad := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	if n := runes(in.ToParty); n < 1 || n > 120 {
		bad("to_party", "Who received the packages?")
	}
	if runes(in.FromParty) > 120 || runes(in.Location) > 200 || runes(in.EvidenceRef) > 200 || runes(in.Notes) > 1000 {
		bad("notes", "A field is too long.")
	}
	if len(in.Packages) == 0 {
		bad("packages", "List each package and whether it was accepted.")
	}
	for _, p := range in.Packages {
		if !validID(p.PackageID) || !slices.Contains([]string{"accepted", "rejected", "missing"}, p.Outcome) {
			bad("packages", "Each package needs an id and accepted, rejected or missing.")
			break
		}
	}
	if in.IdempotencyKey == "" || len(in.IdempotencyKey) > 100 {
		bad("idempotency_key", "Missing request key.")
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	handed := s.Now()
	if in.HandedAt != nil {
		if in.HandedAt.After(s.Now().Add(5 * time.Minute)) {
			return nil, fieldErr("handed_at", "A handoff cannot be in the future.")
		}
		handed = *in.HandedAt
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		run, err := s.lockRun(ctx, tx, runID, "released", "completed")
		if err != nil {
			return err
		}
		var seen int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM delivery_handoffs WHERE run_id = $1 AND idempotency_key = $2`, runID, in.IdempotencyKey).Scan(&seen); err != nil {
			return err
		}
		if seen > 0 {
			return nil
		}
		// Every package must belong to an active stop of this run.
		owner := map[string]string{}
		rows, err := tx.Query(ctx, `SELECT d.id::text, d.obligation_id::text FROM dispatch_stops s JOIN deliveries d ON d.id = s.delivery_id
			WHERE s.run_id = $1 AND s.status = 'active'`, runID)
		if err != nil {
			return err
		}
		type stopRef struct{ delivery, obligation string }
		var refs []stopRef
		for rows.Next() {
			var r stopRef
			if err := rows.Scan(&r.delivery, &r.obligation); err != nil {
				rows.Close()
				return err
			}
			refs = append(refs, r)
		}
		rows.Close()
		pkgsOf := map[string][]string{}
		for _, r := range refs {
			pkgs, err := s.Fulfillment.Packages(ctx, tx, r.obligation)
			if err != nil {
				return err
			}
			for _, p := range pkgs {
				owner[p.ID] = r.delivery
				pkgsOf[r.delivery] = append(pkgsOf[r.delivery], p.ID)
			}
		}
		counts := map[string]int{}
		outcome := map[string]string{}
		for _, p := range in.Packages {
			if _, ok := owner[p.PackageID]; !ok {
				return fieldErr("packages", "A package is not on this run.")
			}
			if _, dup := outcome[p.PackageID]; dup {
				return fieldErr("packages", "A package is listed twice.")
			}
			outcome[p.PackageID] = p.Outcome
			counts[p.Outcome]++
		}
		disputed := counts["rejected"]+counts["missing"] > 0
		var loc *string
		if in.LocationVerified && in.Location != "" {
			loc = &in.Location
		}
		var hid string
		if err := tx.QueryRow(ctx, `INSERT INTO delivery_handoffs (run_id, from_party, to_party, partner_id, courier_id, handed_at, location,
			accepted_count, rejected_count, missing_count, disputed, evidence_ref, notes, actor_user_id, idempotency_key)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15) RETURNING id::text`,
			runID, in.FromParty, in.ToParty, run.PartnerID, run.CourierID, handed, loc, counts["accepted"], counts["rejected"], counts["missing"],
			disputed, in.EvidenceRef, in.Notes, nilIfEmpty(actor), in.IdempotencyKey).Scan(&hid); err != nil {
			return err
		}
		touched := map[string]bool{}
		for _, p := range in.Packages {
			d := owner[p.PackageID]
			touched[d] = true
			var attempt int
			if err := tx.QueryRow(ctx, `SELECT attempt FROM deliveries WHERE id = $1`, d).Scan(&attempt); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO delivery_handoff_packages (handoff_id, package_id, delivery_id, attempt, outcome) VALUES ($1, $2, $3, $4, $5)`,
				hid, p.PackageID, d, attempt, p.Outcome); err != nil {
				if db.IsUniqueViolation(err, "delivery_handoff_packages_once") {
					return conflict("already_handed_over", "A package is already in the partner's custody.")
				}
				return err
			}
			if p.Outcome == "accepted" {
				if err := s.Fulfillment.SetPackageHandoff(ctx, tx, []string{p.PackageID}, "handed_over"); err != nil {
					return err
				}
			} else if p.Outcome == "missing" {
				if err := s.Fulfillment.SetPackageHandoff(ctx, tx, []string{p.PackageID}, "missing"); err != nil {
					return err
				}
			}
		}
		for d := range touched {
			r, err := s.lock(ctx, tx, d)
			if err != nil {
				return err
			}
			var accepted, problems int
			if err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE outcome = 'accepted'), count(*) FILTER (WHERE outcome <> 'accepted')
				FROM delivery_handoff_packages WHERE delivery_id = $1 AND attempt = $2`, d, r.Attempt).Scan(&accepted, &problems); err != nil {
				return err
			}
			var notes []string
			for _, p := range in.Packages {
				if owner[p.PackageID] == d && p.Outcome != "accepted" {
					notes = append(notes, p.Outcome+": "+p.PackageID+" "+strings.TrimSpace(p.Note))
				}
			}
			if len(notes) > 0 {
				typ := "handoff_dispute"
				if counts["missing"] > 0 {
					typ = "missing_package"
				}
				if _, err := s.RaiseException(ctx, tx, ExceptionInput{Stage: "delivery", Type: typ, Severity: "high", Blocking: true,
					Summary: "Handoff of " + r.Number + " disputed", Notes: strings.Join(notes, "; "), DeliveryID: d, RunID: runID,
					NextAction: "escalate", DedupeKey: "handoff:" + hid + ":" + d, Actor: actor}); err != nil {
					return err
				}
			}
			if r.Status == "ready_for_pickup" && problems == 0 && accepted == len(pkgsOf[d]) {
				if err := s.move(ctx, tx, r, "picked_up", trackInput{Source: "staff", Actor: actor, PartnerID: run.PartnerID, OccurredAt: handed,
					Location: in.Location, Verified: in.LocationVerified, Notes: "Handed to " + in.ToParty + ".", Details: map[string]any{"handoff_id": hid},
					Key: "picked_up:" + itoa(r.Attempt)}, ""); err != nil {
					return err
				}
				if err := s.Fulfillment.HandedOver(ctx, tx, r.ObligationID, actor); err != nil {
					return err
				}
				if _, err := s.Orders.Advance(ctx, tx, []string{r.OrderID}, []string{"ready_for_fulfillment"}, "out_for_delivery",
					"Handed to the delivery partner", actor); err != nil {
					return err
				}
				if err := event(ctx, tx, "delivery.handed_over", "delivery", d, itoa(r.Attempt), map[string]any{"handoff_id": hid}); err != nil {
					return err
				}
				if err := s.notify(ctx, tx, r, "dispatched", itoa(r.Attempt)); err != nil {
					return err
				}
			}
		}
		return record(ctx, tx, actor, "delivery.handoff_recorded", "dispatch_run", runID, map[string]any{"handoff_id": hid,
			"accepted": counts["accepted"], "rejected": counts["rejected"], "missing": counts["missing"]})
	})
	if err != nil {
		return nil, err
	}
	return s.Run(ctx, Scope{All: true}, runID)
}
