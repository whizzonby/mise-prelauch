// Package consumer hands domain events to the job queue for one named
// consumer, with a durable offset in event_consumer_offsets.
//
// Delivery is at least once: the offset moves in the same transaction that
// enqueues the jobs, a job is retried with backoff and stays visible when it
// fails, and Replay moves the offset back. Handlers must be idempotent.
//
// Events are read only after a short settling delay. An event id is assigned
// when its row is written, but a transaction that started earlier can commit
// later; reading too soon could move the offset past an event not yet visible.
package consumer

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/jobs"
)

// Route turns one event type into a job.
type Route struct {
	Kind    string
	Payload func(eventID int64, aggregateID string) any
}

// Dispatcher is one consumer.
type Dispatcher struct {
	Pool        *pgxpool.Pool
	Name        string
	Routes      map[string]Route
	SettleDelay time.Duration
}

// Dispatch enqueues jobs for new events and returns how many it enqueued.
func (d *Dispatcher) Dispatch(ctx context.Context) (int, error) {
	n := 0
	err := db.InTx(ctx, d.Pool, func(tx pgx.Tx) error {
		var last int64
		if err := tx.QueryRow(ctx, `SELECT last_event_id FROM event_consumer_offsets WHERE consumer = $1 FOR UPDATE`, d.Name).Scan(&last); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id, type, aggregate_id::text FROM domain_events
			WHERE id > $1 AND occurred_at <= now() - make_interval(secs => $2) ORDER BY id LIMIT 500`, last, d.SettleDelay.Seconds())
		if err != nil {
			return err
		}
		type ev struct {
			id  int64
			typ string
			agg string
		}
		var events []ev
		for rows.Next() {
			var e ev
			if err := rows.Scan(&e.id, &e.typ, &e.agg); err != nil {
				rows.Close()
				return err
			}
			events = append(events, e)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, e := range events {
			if r, ok := d.Routes[e.typ]; ok {
				if err := jobs.Enqueue(ctx, tx, r.Kind, r.Payload(e.id, e.agg)); err != nil {
					return err
				}
				n++
			}
			last = e.id
		}
		_, err = tx.Exec(ctx, `UPDATE event_consumer_offsets SET last_event_id = $2, updated_at = now() WHERE consumer = $1`, d.Name, last)
		return err
	})
	return n, err
}

// Run dispatches every interval until ctx ends (the worker runs it).
func (d *Dispatcher) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if _, err := d.Dispatch(ctx); err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "event dispatch failed", "consumer", d.Name, "error", err.Error())
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Replay moves a consumer back so events after fromID are delivered again.
func Replay(ctx context.Context, q db.Querier, name string, fromID int64) error {
	if fromID < 0 {
		return errors.New("consumer: replay from a positive event id")
	}
	tag, err := q.Exec(ctx, `UPDATE event_consumer_offsets SET last_event_id = $2, updated_at = now() WHERE consumer = $1`, name, fromID)
	if err == nil && tag.RowsAffected() == 0 {
		return errors.New("consumer: unknown consumer " + name)
	}
	return err
}

// Status is a consumer's state for an overview: its offset and any
// waiting or failed jobs.
type Status struct {
	LastEventID int64     `json:"last_event_id"`
	UpdatedAt   time.Time `json:"updated_at"`
	Waiting     int       `json:"waiting"`
	Failed      []Failed  `json:"failed"`
}

// Failed is a delivery that ran out of attempts.
type Failed struct {
	JobID     int64     `json:"job_id"`
	Payload   string    `json:"payload"`
	Error     string    `json:"error"`
	Attempts  int       `json:"attempts"`
	UpdatedAt time.Time `json:"failed_at"`
}

// StatusOf reports the consumer and the jobs of the given kinds.
func StatusOf(ctx context.Context, q db.Querier, name string, kinds ...string) (*Status, error) {
	var s Status
	if err := q.QueryRow(ctx, `SELECT last_event_id, updated_at FROM event_consumer_offsets WHERE consumer = $1`, name).Scan(&s.LastEventID, &s.UpdatedAt); err != nil {
		return nil, err
	}
	if err := q.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE kind = ANY($1) AND status IN ('queued', 'running')`, kinds).Scan(&s.Waiting); err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `SELECT id, payload::text, COALESCE(last_error, ''), attempts, COALESCE(finished_at, created_at)
		FROM jobs WHERE kind = ANY($1) AND status = 'failed' ORDER BY id DESC LIMIT 20`, kinds)
	if err != nil {
		return nil, err
	}
	s.Failed, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Failed])
	return &s, err
}
