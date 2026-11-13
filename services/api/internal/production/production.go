// Package production is Mise's production operating system: it turns the
// finalised orders of a delivery week into an auditable demand snapshot, a
// production plan with traceable ingredient requirements, batches made from
// exact recipe versions, kitchen tasks, quality checks and holds, packaging,
// yield reconciliation and waste.
//
// Mise sends meal kits (pre-portioned ingredients and a recipe card), not
// cooked meals: batches are prepared, portioned, assembled, checked, packed
// and labelled, then handed to delivery.
//
// The module never edits an order's content. It moves an order through the
// production statuses Sprint 3 reserved for it (awaiting_production,
// in_production, ready_for_fulfillment) and nothing else.
package production

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/culinary"
	"mise.tt/api/internal/orders"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/outbox"
	"mise.tt/api/internal/support"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Orders is what production reads from and asks of the orders module.
type Orders interface {
	DemandLines(ctx context.Context, q db.Querier, week time.Time) ([]orders.SourceLine, error)
	Advance(ctx context.Context, q db.Querier, orderIDs []string, from []string, to, reason, actor string) (int, error)
}

// Alerts raises staff alerts (internal/backoffice implements it).
type Alerts interface {
	Raise(ctx context.Context, q db.Querier, a support.Alert) error
}

// Weeks is the business calendar (internal/menus Policy).
type Weeks interface {
	CutoffFor(week time.Time) time.Time
	NextOpenWeek(now time.Time) time.Time
	WeekOf(t time.Time) time.Time
}

type Service struct {
	pool     *pgxpool.Pool
	Culinary *culinary.Service
	Orders   Orders
	Alerts   Alerts
	Weeks    Weeks
	Location *time.Location
	Now      func() time.Time
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, Now: time.Now, Location: time.UTC}
}

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

func parseWeek(v string) (time.Time, bool) {
	t, ok := parseDay(v)
	return t, ok && t.Weekday() == time.Monday
}

func fieldErr(field, msg string) error {
	return httpx.ValidationError([]httpx.FieldError{{Field: field, Code: "invalid", Message: msg}})
}

func conflict(code, msg string) error { return httpx.NewError(http.StatusConflict, code, msg) }

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func runes(s string) int { return len([]rune(s)) }

func human(s string) string { return strings.ReplaceAll(s, "_", " ") }

func reason(r string, min int) (string, error) {
	r = strings.TrimSpace(r)
	if n := runes(r); n < min || n > 500 {
		return "", fieldErr("reason", "Give a reason (up to 500 characters).")
	}
	return r, nil
}

func event(ctx context.Context, q db.Querier, typ, aggType, aggID, dedupe string, payload map[string]any) error {
	return outbox.Record(ctx, q, outbox.Event{Type: typ, AggregateType: aggType, AggregateID: aggID, DedupeKey: dedupe, Payload: payload})
}

func (s *Service) raise(ctx context.Context, q db.Querier, a support.Alert) error {
	if s.Alerts == nil {
		return nil
	}
	return s.Alerts.Raise(ctx, q, a)
}

// lock serialises work on one key (a week, a plan) across requests.
func lock(ctx context.Context, q db.Querier, key string) error {
	_, err := q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, key)
	return err
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
