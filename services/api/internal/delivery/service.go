package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
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

// The delivery operating system (Sprint 7): service zones and the calendar,
// capacity, partners and couriers, deliveries, dispatch runs, manifests,
// custody handoffs, tracking, proof, exceptions, reschedules, returns,
// costs, reconciliation and reports.
//
// Delivery owns deliveries.status, the single authority on where a delivery
// stands. It never edits an order: order statuses move through Orders
// (commerce), and fulfillment state through Fulfillment.

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Orders is what delivery asks of commerce.
type Orders interface {
	Advance(ctx context.Context, q db.Querier, orderIDs []string, from []string, to, reason, actor string) (int, error)
	// The reconciliation contract for corrected outcomes: commerce records
	// that a delivered outcome is disputed (or confirmed again) without
	// rewriting the order's status or payment history.
	FlagDeliveryOutcome(ctx context.Context, q db.Querier, orderID, state, reason, actor string) error
	OutcomeDisputed(ctx context.Context, q db.Querier, orderID string) (bool, error)
}

// PackageRef is a fulfillment package as delivery sees it.
type PackageRef struct {
	ID       string `json:"id"`
	Code     string `json:"code"`
	Contents string `json:"contents"`
	Kits     int    `json:"kits"`
}

// Fulfillment is what delivery asks of the fulfillment module, always in the
// caller's transaction.
type Fulfillment interface {
	Packages(ctx context.Context, q db.Querier, obligationID string) ([]PackageRef, error)
	SetPackageHandoff(ctx context.Context, q db.Querier, packageIDs []string, status string) error
	HandedOver(ctx context.Context, tx pgx.Tx, obligationID, actor string) error
	Fulfilled(ctx context.Context, tx pgx.Tx, obligationID, actor string) error
	Interrupt(ctx context.Context, tx pgx.Tx, obligationID, actor, reason string) error
	// Reopen undoes "fulfilled" after an audited delivery correction: back to handed over.
	Reopen(ctx context.Context, tx pgx.Tx, obligationID, actor, reason string) error
	Resume(ctx context.Context, tx pgx.Tx, obligationID, actor string) error
	Cancel(ctx context.Context, tx pgx.Tx, obligationID, actor, reason string) error
}

// Alerts raises staff alerts (internal/backoffice implements it).
type Alerts interface {
	Raise(ctx context.Context, q db.Querier, a support.Alert) error
}

// Cases opens support cases (internal/support implements it).
type Cases interface {
	Create(ctx context.Context, actor string, in support.CreateInput) (*support.Case, error)
}

type Service struct {
	pool        *pgxpool.Pool
	Fulfillment Fulfillment
	Orders      Orders
	Alerts      Alerts
	Cases       Cases
	// Metrics supplies the figures the delivery report shares with the
	// analytics registry (nil only in narrow unit tests).
	Metrics  MetricReader
	Location *time.Location
	Now      func() time.Time
	// CodeKey derives delivery confirmation codes.
	CodeKey []byte
	// Carriers by integration mode; "manual" is always present.
	Carriers map[string]Carrier
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, Now: time.Now, Location: time.UTC, Carriers: map[string]Carrier{"manual": ManualCarrier{}, "internal_fleet": ManualCarrier{}}}
}

// Pool exposes the pool to the HTTP layer of sibling modules' tests.
func (s *Service) tx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	return db.InTx(ctx, s.pool, fn)
}

// today is the business date.
func (s *Service) today() time.Time {
	t := s.Now().In(s.Location)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

const day = "2006-01-02"

func parseDay(v string) (time.Time, bool) {
	t, err := time.ParseInLocation(day, v, time.UTC)
	return t, err == nil
}

func fieldErr(field, msg string) error {
	return httpx.ValidationError([]httpx.FieldError{{Field: field, Code: "invalid", Message: msg}})
}

func conflict(code, msg string) error { return httpx.NewError(http.StatusConflict, code, msg) }

func unprocessable(code, msg string) error {
	return httpx.NewError(http.StatusUnprocessableEntity, code, msg)
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func runes(s string) int { return len([]rune(s)) }

// Human turns a status key into words.
func Human(s string) string { return strings.ReplaceAll(s, "_", " ") }

func reason(r string, min, max int) (string, error) {
	r = strings.TrimSpace(r)
	if n := runes(r); n < min || n > max {
		return "", fieldErr("reason", fmt.Sprintf("Give a reason (%d to %d characters).", min, max))
	}
	return r, nil
}

func validID(id string) bool { return uuidPattern.MatchString(id) }

func event(ctx context.Context, q db.Querier, typ, aggType, aggID, dedupe string, payload map[string]any) error {
	return outbox.Record(ctx, q, outbox.Event{Type: typ, AggregateType: aggType, AggregateID: aggID, DedupeKey: dedupe, Payload: payload})
}

func record(ctx context.Context, q db.Querier, actor, action, targetType, targetID string, meta map[string]any) error {
	return audit.Record(ctx, q, audit.Entry{ActorUserID: actor, Action: action, TargetType: targetType, TargetID: targetID, Metadata: meta})
}

// NextNumber returns the next number of a kind within a period, e.g.
// ("DLV", "2026W47") → "DLV-2026W47-0001". Shared with fulfillment.
func NextNumber(ctx context.Context, q db.Querier, kind, period string) (string, error) {
	var n int
	err := q.QueryRow(ctx, `
		INSERT INTO delivery_counters (kind, period, last) VALUES ($1, $2, 1)
		ON CONFLICT (kind, period) DO UPDATE SET last = delivery_counters.last + 1 RETURNING last`, kind, period).Scan(&n)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s-%04d", kind, period, n), nil
}

// WeekPeriod is the ISO week label used in numbers.
func WeekPeriod(t time.Time) string {
	y, w := t.ISOWeek()
	return fmt.Sprintf("%dW%02d", y, w)
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

var (
	zoneCode   = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]{1,19}$`)
	keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,40}$`)
)

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }
