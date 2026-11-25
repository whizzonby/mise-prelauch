package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/platform/consumer"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/jobs"
)

// ---------------------------------------------------------------- snapshots

// JobRefresh recomputes daily snapshots for a range of local days.
const JobRefresh = "analytics.refresh"

type RefreshJob struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason"`
	Actor  string `json:"actor,omitempty"`
}

// RefreshRun is one refresh as recorded.
type RefreshRun struct {
	ID            int64      `json:"id"`
	Reason        string     `json:"reason"`
	From          string     `json:"from"`
	To            string     `json:"to"`
	Status        string     `json:"status"`
	ValuesWritten int        `json:"values_written"`
	Error         *string    `json:"error"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at"`
}

// Refresh recomputes every snapshotted metric for each day in the range and
// upserts it. Idempotent: running it again (a replay, a backfill, a late
// event) replaces the same rows and never adds.
func (s *Service) Refresh(ctx context.Context, reason, actor string, r Range) (*RefreshRun, error) {
	if r.Days() > 731 {
		return nil, fieldErr("from", "Refresh at most two years at a time.")
	}
	var id int64
	if err := s.pool.QueryRow(ctx, `INSERT INTO analytics_refresh_runs (reason, from_day, to_day, actor_user_id) VALUES ($1, $2, $3, $4) RETURNING id`,
		reason, r.From, r.To, nilIfEmpty(actor)).Scan(&id); err != nil {
		return nil, err
	}
	written := 0
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = %d", (5*s.QueryTimeout).Milliseconds())); err != nil {
			return err
		}
		for d := r.From; !d.After(r.To); d = d.AddDate(0, 0, 1) {
			for i := range registry {
				m := &registry[i]
				if !m.Snapshot {
					continue
				}
				v, err := s.Value(ctx, tx, m, Range{From: d, To: d}, Filters{})
				if err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `INSERT INTO analytics_snapshots (metric_id, day, value, refresh_run_id, computed_at) VALUES ($1, $2, $3, $4, now())
					ON CONFLICT (metric_id, day) DO UPDATE SET value = EXCLUDED.value, refresh_run_id = EXCLUDED.refresh_run_id, computed_at = now()`,
					m.ID, d, v, id); err != nil {
					return err
				}
				written++
			}
		}
		return nil
	})
	status, msg := "succeeded", (*string)(nil)
	if err != nil {
		status = "failed"
		m := err.Error()
		if len(m) > 1000 {
			m = m[:1000]
		}
		msg = &m
	}
	// Recorded outside the work's transaction so a failure stays visible.
	if _, uerr := s.pool.Exec(context.WithoutCancel(ctx), `UPDATE analytics_refresh_runs SET status = $2, values_written = $3, error = $4, finished_at = now() WHERE id = $1`,
		id, status, written, msg); uerr != nil && err == nil {
		err = uerr
	}
	run, gerr := s.run(ctx, id)
	if err != nil {
		return run, err
	}
	return run, gerr
}

func (s *Service) run(ctx context.Context, id int64) (*RefreshRun, error) {
	var r RefreshRun
	var f, t time.Time
	err := s.pool.QueryRow(ctx, `SELECT id, reason, from_day, to_day, status, values_written, error, started_at, finished_at FROM analytics_refresh_runs WHERE id = $1`, id).
		Scan(&r.ID, &r.Reason, &f, &t, &r.Status, &r.ValuesWritten, &r.Error, &r.StartedAt, &r.FinishedAt)
	r.From, r.To = f.Format(day), t.Format(day)
	return &r, err
}

// HandleRefresh is the job handler (retried by the queue on error).
func (s *Service) HandleRefresh(ctx context.Context, payload json.RawMessage) error {
	var j RefreshJob
	if err := json.Unmarshal(payload, &j); err != nil {
		return fmt.Errorf("analytics: bad payload: %w", err)
	}
	r, err := s.ParseRange(j.From, j.To)
	if err != nil {
		return err
	}
	_, err = s.Refresh(ctx, j.Reason, j.Actor, r)
	return err
}

// EnqueueRefresh queues a refresh (scheduled or requested).
func EnqueueRefresh(ctx context.Context, q db.Querier, j RefreshJob) error {
	return jobs.Enqueue(ctx, q, JobRefresh, j)
}

// Snapshot is a stored daily value.
type Snapshot struct {
	Day   string   `json:"day"`
	Value *float64 `json:"value"`
}

// Series returns a metric's stored daily values in a range.
func (s *Service) Series(ctx context.Context, metricID string, r Range) ([]Snapshot, error) {
	if _, ok := Get(metricID); !ok {
		return nil, fieldErr("metric", "Unknown metric.")
	}
	rows, err := s.pool.Query(ctx, `SELECT to_char(day, 'YYYY-MM-DD'), value::float8 FROM analytics_snapshots WHERE metric_id = $1 AND day BETWEEN $2 AND $3 ORDER BY day`,
		metricID, r.From, r.To)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Snapshot])
}

// ---------------------------------------------------------------- event consumer

// JobEvent refreshes the day an operational event happened on, so late or
// replayed events correct the snapshots (refresh is idempotent).
const JobEvent = "analytics.event"

// ConsumerName is analytics' row in event_consumer_offsets.
const ConsumerName = "analytics"

type eventPayload struct {
	EventID int64 `json:"event_id"`
}

// Dispatcher routes every outbox event to the analytics job. A failed
// analytics job never blocks other consumers or core processing.
func (s *Service) Dispatcher(settle time.Duration) *consumer.Dispatcher {
	routes := map[string]consumer.Route{}
	for _, t := range EventTypes {
		routes[t] = consumer.Route{Kind: JobEvent, Payload: func(id int64, _ string) any { return eventPayload{EventID: id} }}
	}
	return &consumer.Dispatcher{Pool: s.pool, Name: ConsumerName, SettleDelay: settle, Routes: routes}
}

// EventTypes are the outbox events that change analytics figures.
var EventTypes = []string{"production.plan_released", "production.batch_completed", "quality.hold_created", "quality.hold_released",
	"procurement.shortage_identified", "purchase_order.approved", "purchase_order.issued", "receiving.inspection_failed", "inventory.stock_issued",
	"inventory.stock_adjusted", "fulfillment.verified", "delivery.assigned", "delivery.handed_over", "delivery.delivered", "delivery.failed",
	"delivery.returned", "delivery.reconciled",
	// Commerce (Sprint 9): money, orders and subscription movements refresh the day they happened.
	"payment.succeeded", "payment.failed", "payment.cancelled", "refund.succeeded", "order.confirmed", "order.status_changed",
	"subscription.created", "subscription.activated", "subscription.renewed", "subscription.renewal_failed", "subscription.week_unpaid",
	"subscription.payment_recovered", "subscription.pause_scheduled", "subscription.paused", "subscription.pause_ended",
	"subscription.resumed", "subscription.cancel_scheduled", "subscription.cancellation_withdrawn", "subscription.cancelled",
	"subscription.expired", "subscription.plan_changed"}

func (s *Service) HandleEvent(ctx context.Context, payload json.RawMessage) error {
	var p eventPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("analytics: bad payload: %w", err)
	}
	var at time.Time
	if err := s.pool.QueryRow(ctx, `SELECT occurred_at FROM domain_events WHERE id = $1`, p.EventID).Scan(&at); err != nil {
		return err
	}
	t := at.In(s.Location)
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	_, err := s.Refresh(ctx, "event", "", Range{From: d, To: d})
	return err
}

// ---------------------------------------------------------------- status

type Status struct {
	LastSuccess *time.Time       `json:"last_success"`
	Stale       bool             `json:"stale"`
	StaleAfter  string           `json:"stale_after"`
	Runs        []RefreshRun     `json:"runs"`
	Consumer    *consumer.Status `json:"consumer"`
	Snapshots   int64            `json:"snapshots"`
}

// StaleAfter: snapshots older than this are flagged (the worker refreshes hourly).
const StaleAfter = 3 * time.Hour

func (s *Service) Status(ctx context.Context) (*Status, error) {
	st := &Status{StaleAfter: StaleAfter.String(), Runs: []RefreshRun{}}
	if err := s.pool.QueryRow(ctx, `SELECT max(finished_at) FROM analytics_refresh_runs WHERE status = 'succeeded'`).Scan(&st.LastSuccess); err != nil {
		return nil, err
	}
	st.Stale = st.LastSuccess == nil || s.Now().Sub(*st.LastSuccess) > StaleAfter
	rows, err := s.pool.Query(ctx, `SELECT id FROM analytics_refresh_runs ORDER BY id DESC LIMIT 20`)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		r, err := s.run(ctx, id)
		if err != nil {
			return nil, err
		}
		st.Runs = append(st.Runs, *r)
	}
	if st.Consumer, err = consumer.StatusOf(ctx, s.pool, ConsumerName, JobEvent, JobRefresh); err != nil {
		return nil, err
	}
	err = s.pool.QueryRow(ctx, `SELECT count(*) FROM analytics_snapshots`).Scan(&st.Snapshots)
	return st, err
}

// ---------------------------------------------------------------- catalog

// Catalog renders the metric catalogue as Markdown (docs/analytics/METRIC_CATALOG.md).
func Catalog() string {
	var b strings.Builder
	b.WriteString("# Metric catalog\n\nGenerated from `internal/analytics/metrics.go` by `misectl analytics catalog`; a test fails if this file\n" +
		"is out of date. Every dashboard, export, snapshot and scheduled report uses these definitions and no other.\n\n" +
		"Time zone: " + tz + ". Reporting period: " + rangeP + ".\n\n")
	domain := ""
	for _, m := range Metrics() {
		if m.Domain != domain {
			domain = m.Domain
			b.WriteString("## " + strings.ToUpper(domain[:1]) + domain[1:] + "\n\n")
		}
		fmt.Fprintf(&b, "### `%s` — %s\n\n%s\n\n", m.ID, m.Name, m.Definition)
		filters := "none"
		if len(m.Filters) > 0 {
			filters = strings.Join(m.Filters, ", ")
		}
		currency := "not money"
		if m.Currency != "" {
			currency = m.Currency
		}
		update := live
		if m.Snapshot {
			update = onDemand
		}
		fmt.Fprintf(&b, "| | |\n|---|---|\n| Kind | %s |\n| Source fields | %s |\n| Calculation | %s |\n| Filters | %s |\n| Update | %s |\n| Missing data | %s |\n| Currency | %s |\n| Limitations | %s |\n| Owner | %s |\n| Permission | `%s` |\n\n",
			m.Kind, "`"+strings.Join(m.Sources, "`, `")+"`", m.Calculation, filters, update, m.Missing, currency, m.Limitations, m.Owner, m.Permission)
	}
	return b.String()
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Tick dispatches new outbox events to analytics jobs and, at most once an
// hour, queues a refresh of yesterday and today. Safe on several workers:
// the queued refresh is idempotent and the hour check is in the database.
func (s *Service) Tick(ctx context.Context, settle time.Duration) error {
	if _, err := s.Dispatcher(settle).Dispatch(ctx); err != nil {
		return err
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('analytics.schedule', 8))`); err != nil {
			return err
		}
		var recent int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM analytics_refresh_runs WHERE reason = 'schedule' AND started_at > now() - interval '1 hour'`).Scan(&recent); err != nil {
			return err
		}
		var queued int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE kind = $1 AND status IN ('queued', 'running') AND payload->>'reason' = 'schedule'`, JobRefresh).Scan(&queued); err != nil {
			return err
		}
		if recent > 0 || queued > 0 {
			return nil
		}
		today := s.today()
		return EnqueueRefresh(ctx, tx, RefreshJob{From: today.AddDate(0, 0, -1).Format(day), To: today.Format(day), Reason: "schedule"})
	})
}
