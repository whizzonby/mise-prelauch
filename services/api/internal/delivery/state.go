package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/platform/httpx"
)

// The delivery state machine: the only moves deliveries.status can make.
// Anything else is refused. A correction is a separate, permissioned and
// audited operation (Correct), never an overwrite.
var transitions = map[string][]string{
	"pending_assignment": {"assigned", "cancelled"},
	"assigned":           {"pending_assignment", "ready_for_pickup", "cancelled"},
	"ready_for_pickup":   {"assigned", "picked_up", "cancelled"},
	"picked_up":          {"in_transit", "delivery_failed"},
	"in_transit":         {"arriving", "delivered", "delivery_failed"},
	"arriving":           {"delivered", "delivery_failed"},
	"delivery_failed":    {"pending_assignment", "returned"},
	"delivered":          {},
	"returned":           {},
	"cancelled":          {},
}

// Statuses lists every delivery status in lifecycle order.
var Statuses = []string{"pending_assignment", "assigned", "ready_for_pickup", "picked_up", "in_transit", "arriving",
	"delivered", "delivery_failed", "returned", "cancelled"}

// CanMove reports whether from → to is allowed.
func CanMove(from, to string) bool { return slices.Contains(transitions[from], to) }

// Active statuses: the delivery is somewhere between release and an outcome.
func active(status string) bool {
	return status != "delivered" && status != "returned" && status != "cancelled"
}

// row is a locked delivery.
type row struct {
	ID           string
	Number       string
	ObligationID string
	OrderID      string
	CustomerID   string
	Status       string
	Version      int
	Attempt      int
	ZoneID       *string
	ServiceDate  *time.Time
	WindowID     *string
	PackageCount int
	Address      json.RawMessage
	OrderWeek    time.Time
}

func (s *Service) lock(ctx context.Context, tx pgx.Tx, id string) (*row, error) {
	if !validID(id) {
		return nil, httpx.ErrNotFound
	}
	var r row
	err := tx.QueryRow(ctx, `SELECT id::text, number, obligation_id::text, order_id::text, customer_id::text, status, version, attempt,
		zone_id::text, service_date, window_id::text, package_count, address, order_week FROM deliveries WHERE id = $1 FOR UPDATE`, id).
		Scan(&r.ID, &r.Number, &r.ObligationID, &r.OrderID, &r.CustomerID, &r.Status, &r.Version, &r.Attempt, &r.ZoneID, &r.ServiceDate,
			&r.WindowID, &r.PackageCount, &r.Address, &r.OrderWeek)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	return &r, err
}

// expect checks an optimistic version when the caller sent one.
func expect(r *row, version *int) error {
	if version != nil && *version != r.Version {
		return conflict("stale", "This delivery changed since you loaded it. Reload and try again.")
	}
	return nil
}

// trackInput is one tracking event.
type trackInput struct {
	DeliveryID string
	Type       string
	From, To   string
	Attempt    int
	Source     string
	Actor      string
	PartnerID  string
	OccurredAt time.Time
	Location   string
	Verified   bool
	Notes      string
	Details    map[string]any
	Key        string
}

// track appends a tracking event. A repeated idempotency key is ignored
// (returns errDuplicate so callers can answer with the current state).
func (s *Service) track(ctx context.Context, tx pgx.Tx, in trackInput) error {
	if in.OccurredAt.IsZero() {
		in.OccurredAt = s.Now()
	}
	if in.Details == nil {
		in.Details = map[string]any{}
	}
	var loc *string
	if in.Verified && in.Location != "" {
		loc = &in.Location
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO delivery_tracking_events (delivery_id, type, from_status, to_status, attempt, source, actor_user_id, partner_id,
			occurred_at, location, location_verified, notes, details, idempotency_key)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (delivery_id, idempotency_key) DO NOTHING`,
		in.DeliveryID, in.Type, nilIfEmpty(in.From), nilIfEmpty(in.To), in.Attempt, in.Source, nilIfEmpty(in.Actor), nilIfEmpty(in.PartnerID),
		in.OccurredAt, loc, loc != nil, nilIfEmpty(in.Notes), in.Details, in.Key)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errDuplicate
	}
	return nil
}

var errDuplicate = errors.New("delivery: duplicate tracking event")

// move changes a locked delivery's status after checking the state machine,
// bumps its version and appends the tracking event.
func (s *Service) move(ctx context.Context, tx pgx.Tx, r *row, to string, in trackInput, extra string, args ...any) error {
	if !CanMove(r.Status, to) {
		return conflict("invalid_transition", "A delivery that is "+Human(r.Status)+" cannot become "+Human(to)+".")
	}
	in.DeliveryID, in.From, in.To = r.ID, r.Status, to
	if in.Attempt == 0 {
		in.Attempt = r.Attempt
	}
	if in.Type == "" {
		in.Type = to
	}
	if err := s.track(ctx, tx, in); err != nil {
		if errors.Is(err, errDuplicate) {
			return conflict("duplicate", "This update was already recorded.")
		}
		return err
	}
	q := `UPDATE deliveries SET status = $2, version = version + 1, updated_at = now()` + extra + ` WHERE id = $1`
	if _, err := tx.Exec(ctx, q, append([]any{r.ID, to}, args...)...); err != nil {
		return err
	}
	r.Status = to
	r.Version++
	return nil
}
