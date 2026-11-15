// Package fulfillment turns production-ready, paid orders into verified
// packages ready for dispatch (Sprint 7).
//
// It consumes production.ready_for_fulfillment (one event per completed
// batch) and works out which orders are now wholly ready, the same way
// production does. For each it makes one obligation (unique per order), with
// packages whose items name the batch that made each meal. Staff verify the
// kits item by item; a verified obligation is released to delivery.
//
// Traceability is batch-level: a package item points at a batch, and Sprint
// 6 links batches to ingredient lots. Nothing here claims which lot portion
// went into which kit.
package fulfillment

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/delivery"
	"mise.tt/api/internal/orders"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/outbox"
	"mise.tt/api/internal/production"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Orders is what fulfillment reads from commerce.
type Orders interface {
	Facts(ctx context.Context, q db.Querier, orderID string) (*orders.FulfillmentFacts, error)
	ReadyOrderIDs(ctx context.Context, q db.Querier) ([]string, error)
}

// Production is what fulfillment reads from production.
type Production interface {
	KitsForOrder(ctx context.Context, q db.Querier, orderID string) ([]production.OrderKit, error)
	OrdersForBatch(ctx context.Context, q db.Querier, batchID string) (string, []string, error)
	BatchState(ctx context.Context, q db.Querier, batchID string) (*production.OrderKit, error)
}

// Delivery is what fulfillment asks of the delivery module.
type Delivery interface {
	RaiseException(ctx context.Context, q db.Querier, in delivery.ExceptionInput) (string, error)
	CreateFromRelease(ctx context.Context, tx pgx.Tx, in delivery.Release) (string, error)
	LabelCertified(ctx context.Context, q db.Querier) (bool, error)
}

type Service struct {
	pool        *pgxpool.Pool
	Orders      Orders
	Production  Production
	Delivery    Delivery
	Location    *time.Location
	Now         func() time.Time
	SettleDelay time.Duration
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, Now: time.Now, Location: time.UTC, SettleDelay: 30 * time.Second}
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

func human(s string) string { return strings.ReplaceAll(s, "_", " ") }

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func event(ctx context.Context, q db.Querier, typ, id, dedupe string, payload map[string]any) error {
	return outbox.Record(ctx, q, outbox.Event{Type: typ, AggregateType: "fulfillment_obligation", AggregateID: id, DedupeKey: dedupe, Payload: payload})
}

func record(ctx context.Context, q db.Querier, actor, action, id string, meta map[string]any) error {
	return audit.Record(ctx, q, audit.Entry{ActorUserID: actor, Action: action, TargetType: "fulfillment_obligation", TargetID: id, Metadata: meta})
}

// The fulfillment state machine. "exception" can interrupt any active state;
// resolving its blocking exceptions returns to the interrupted state.
var transitions = map[string][]string{
	"ready_for_verification":   {"verification_in_progress", "verified", "exception", "cancelled"},
	"verification_in_progress": {"verified", "exception", "cancelled"},
	"verified":                 {"ready_for_dispatch", "exception", "cancelled"},
	"ready_for_dispatch":       {"handed_over", "exception", "cancelled"},
	"handed_over":              {"fulfilled", "exception"},
	"exception":                {"cancelled"}, // plus Resume to resume_status
	"fulfilled":                {"handed_over"}, // only through an audited delivery correction (Reopen)
	"cancelled":                {},
}

type obligationRow struct {
	ID         string
	Number     string
	OrderID    string
	OrderNo    string
	CustomerID string
	Status     string
	Resume     *string
	Version    int
}

func (s *Service) lock(ctx context.Context, tx pgx.Tx, id string) (*obligationRow, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	var o obligationRow
	err := tx.QueryRow(ctx, `SELECT id::text, number, order_id::text, order_number, customer_id::text, status, resume_status, version
		FROM fulfillment_obligations WHERE id = $1 FOR UPDATE`, id).Scan(&o.ID, &o.Number, &o.OrderID, &o.OrderNo, &o.CustomerID, &o.Status, &o.Resume, &o.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	return &o, err
}

// move changes the state of a locked obligation and appends its history.
func (s *Service) move(ctx context.Context, tx pgx.Tx, o *obligationRow, to, why, actor string, extra string, args ...any) error {
	if !slices.Contains(transitions[o.Status], to) {
		return conflict("invalid_transition", "A fulfillment that is "+human(o.Status)+" cannot become "+human(to)+".")
	}
	if _, err := tx.Exec(ctx, `UPDATE fulfillment_obligations SET status = $2, version = version + 1, updated_at = now()`+extra+` WHERE id = $1`,
		append([]any{o.ID, to}, args...)...); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO fulfillment_events (obligation_id, from_status, to_status, reason, actor_user_id) VALUES ($1, $2, $3, $4, $5)`,
		o.ID, o.Status, to, nilIfEmpty(why), nilIfEmpty(actor)); err != nil {
		return err
	}
	o.Status = to
	o.Version++
	return nil
}

// ---------------------------------------------------------------- views

// Obligation is a fulfillment with its packages and history.
type Obligation struct {
	ID            string     `json:"id"`
	Number        string     `json:"number"`
	OrderID       string     `json:"order_id"`
	OrderNumber   string     `json:"order_number"`
	CustomerID    string     `json:"customer_id"`
	CustomerName  string     `json:"customer_name"`
	Week          string     `json:"week"`
	PlanID        *string    `json:"plan_id"`
	Status        string     `json:"status"`
	ResumeStatus  *string    `json:"resume_status"`
	Version       int        `json:"version"`
	KitsRequired  int        `json:"kits_required"`
	CreatedAt     time.Time  `json:"created_at"`
	VerifiedAt    *time.Time `json:"verified_at"`
	ReleasedAt    *time.Time `json:"released_at"`
	HandedOverAt  *time.Time `json:"handed_over_at"`
	FulfilledAt   *time.Time `json:"fulfilled_at"`
	DeliveryID    *string    `json:"delivery_id"`
	DeliveryNo    *string    `json:"delivery_number"`
	Region        string     `json:"region"`
	OpenBlocking  int        `json:"open_blocking_exceptions"`
	Packages      []Package  `json:"packages,omitempty"`
	History       []History  `json:"history,omitempty"`
	Verifications []Check    `json:"verifications,omitempty"`
}

type Package struct {
	ID                 string     `json:"id"`
	Code               string     `json:"code"`
	Sequence           int        `json:"sequence"`
	VerificationStatus string     `json:"verification_status"`
	VerifiedAt         *time.Time `json:"verified_at"`
	HandoffStatus      string     `json:"handoff_status"`
	Items              []Item     `json:"items"`
}

type Item struct {
	ID               string `json:"id"`
	MealID           string `json:"meal_id"`
	MealName         string `json:"meal_name"`
	Quantity         int    `json:"quantity"`
	BatchID          string `json:"batch_id"`
	BatchCode        string `json:"batch_code"`
	RecipeVersionID  string `json:"recipe_version_id"`
	RecipeVersion    int    `json:"recipe_version"`
	Status           string `json:"status"`
	VerifiedQuantity *int   `json:"verified_quantity"`
}

type History struct {
	From      *string   `json:"from"`
	To        string    `json:"to"`
	Reason    *string   `json:"reason"`
	ActorName *string   `json:"actor_name"`
	At        time.Time `json:"at"`
}

type Check struct {
	ItemID      string    `json:"item_id"`
	Counted     int       `json:"counted_quantity"`
	ScannedCode string    `json:"scanned_batch_code"`
	Result      string    `json:"result"`
	Discrepancy *string   `json:"discrepancy"`
	ActorName   *string   `json:"actor_name"`
	At          time.Time `json:"at"`
}

const obligationCols = `f.id::text, f.number, f.order_id::text, f.order_number, f.customer_id::text,
	trim(COALESCE(u.first_name, '') || ' ' || COALESCE(u.last_name, '')), f.week_start::text, f.plan_id::text, f.status, f.resume_status,
	f.version, f.kits_required, f.created_at, f.verified_at, f.released_at, f.handed_over_at, f.fulfilled_at, d.id::text, d.number,
	COALESCE(o.address_snapshot->>'region_label', ''),
	(SELECT count(*) FROM delivery_exceptions e WHERE e.obligation_id = f.id AND e.blocking AND e.status IN ('open', 'in_progress'))::int`

const obligationFrom = ` FROM fulfillment_obligations f JOIN orders o ON o.id = f.order_id
	JOIN customers c ON c.id = f.customer_id JOIN users u ON u.id = c.user_id LEFT JOIN deliveries d ON d.obligation_id = f.id`

func scanObligation(r pgx.Row) (*Obligation, error) {
	var o Obligation
	err := r.Scan(&o.ID, &o.Number, &o.OrderID, &o.OrderNumber, &o.CustomerID, &o.CustomerName, &o.Week, &o.PlanID, &o.Status, &o.ResumeStatus,
		&o.Version, &o.KitsRequired, &o.CreatedAt, &o.VerifiedAt, &o.ReleasedAt, &o.HandedOverAt, &o.FulfilledAt, &o.DeliveryID, &o.DeliveryNo,
		&o.Region, &o.OpenBlocking)
	return &o, err
}

// Filter narrows the fulfillment queue.
type Filter struct {
	Status string // a status, "active" (default) or "all"
	Week   string
	Query  string
}

func (s *Service) List(ctx context.Context, f Filter) ([]Obligation, error) {
	where, args := []string{"true"}, []any{}
	switch f.Status {
	case "", "active":
		where = append(where, "f.status NOT IN ('fulfilled', 'cancelled')")
	case "all":
	default:
		if _, ok := transitions[f.Status]; !ok {
			return nil, fieldErr("status", "Unknown status.")
		}
		args = append(args, f.Status)
		where = append(where, "f.status = $1")
	}
	if f.Week != "" {
		if _, err := time.Parse("2006-01-02", f.Week); err != nil {
			return nil, fieldErr("week", "Use a date like 2026-11-16.")
		}
		args = append(args, f.Week)
		where = append(where, "f.week_start = $"+itoa(len(args)))
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		args = append(args, "%"+strings.ToLower(q)+"%")
		n := itoa(len(args))
		where = append(where, "(lower(f.number) LIKE $"+n+" OR lower(f.order_number) LIKE $"+n+" OR lower(u.first_name || ' ' || u.last_name) LIKE $"+n+")")
	}
	rows, err := s.pool.Query(ctx, `SELECT `+obligationCols+obligationFrom+` WHERE `+strings.Join(where, " AND ")+`
		ORDER BY CASE f.status WHEN 'exception' THEN 0 WHEN 'ready_for_verification' THEN 1 WHEN 'verification_in_progress' THEN 1 ELSE 2 END,
		f.week_start, f.order_number LIMIT 500`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Obligation{}
	for rows.Next() {
		o, err := scanObligation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *o)
	}
	return out, rows.Err()
}

func (s *Service) Get(ctx context.Context, id string) (*Obligation, error) {
	return s.get(ctx, s.pool, id)
}

func (s *Service) get(ctx context.Context, q db.Querier, id string) (*Obligation, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	o, err := scanObligation(q.QueryRow(ctx, `SELECT `+obligationCols+obligationFrom+` WHERE f.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if o.Packages, err = s.packages(ctx, q, id); err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `SELECT e.from_status, e.to_status, e.reason, NULLIF(trim(COALESCE(u.first_name, '') || ' ' || COALESCE(u.last_name, '')), ''), e.created_at
		FROM fulfillment_events e LEFT JOIN users u ON u.id = e.actor_user_id WHERE e.obligation_id = $1 ORDER BY e.id`, id)
	if err != nil {
		return nil, err
	}
	if o.History, err = pgx.CollectRows(rows, pgx.RowToStructByPos[History]); err != nil {
		return nil, err
	}
	rows, err = q.Query(ctx, `SELECT v.item_id::text, v.counted_quantity, v.scanned_batch_code, v.result, v.discrepancy,
		NULLIF(trim(COALESCE(u.first_name, '') || ' ' || COALESCE(u.last_name, '')), ''), v.created_at
		FROM fulfillment_verifications v LEFT JOIN users u ON u.id = v.actor_user_id WHERE v.obligation_id = $1 ORDER BY v.id`, id)
	if err != nil {
		return nil, err
	}
	o.Verifications, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Check])
	return o, err
}

func (s *Service) packages(ctx context.Context, q db.Querier, obligationID string) ([]Package, error) {
	rows, err := q.Query(ctx, `SELECT id::text, code, sequence, verification_status, verified_at, handoff_status
		FROM fulfillment_packages WHERE obligation_id = $1 ORDER BY sequence`, obligationID)
	if err != nil {
		return nil, err
	}
	pkgs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Package, error) {
		var p Package
		var seq int16
		err := r.Scan(&p.ID, &p.Code, &seq, &p.VerificationStatus, &p.VerifiedAt, &p.HandoffStatus)
		p.Sequence, p.Items = int(seq), []Item{}
		return p, err
	})
	if err != nil {
		return nil, err
	}
	rows, err = q.Query(ctx, `SELECT i.package_id::text, i.id::text, i.meal_id::text, i.meal_name, i.quantity, i.batch_id::text, i.batch_code,
		i.recipe_version_id::text, v.number, i.status, i.verified_quantity
		FROM fulfillment_package_items i JOIN recipe_versions v ON v.id = i.recipe_version_id
		WHERE i.obligation_id = $1 ORDER BY i.meal_name, i.meal_id`, obligationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var pkg string
		var it Item
		var qty int16
		var vq *int16
		if err := rows.Scan(&pkg, &it.ID, &it.MealID, &it.MealName, &qty, &it.BatchID, &it.BatchCode, &it.RecipeVersionID, &it.RecipeVersion,
			&it.Status, &vq); err != nil {
			return nil, err
		}
		it.Quantity = int(qty)
		if vq != nil {
			n := int(*vq)
			it.VerifiedQuantity = &n
		}
		for i := range pkgs {
			if pkgs[i].ID == pkg {
				pkgs[i].Items = append(pkgs[i].Items, it)
			}
		}
	}
	return pkgs, rows.Err()
}

func itoa(n int) string {
	const digits = "0123456789"
	if n < 10 {
		return digits[n : n+1]
	}
	return itoa(n/10) + digits[n%10:n%10+1]
}
