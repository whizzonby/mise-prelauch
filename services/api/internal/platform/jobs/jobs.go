// Package jobs is a small transactional job queue on PostgreSQL. Enqueue takes
// the caller's transaction, so a job exists if and only if the work that
// requested it committed.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/platform/db"
)

// Handler processes one job payload. Returning an error schedules a retry.
type Handler func(ctx context.Context, payload json.RawMessage) error

func Enqueue(ctx context.Context, q db.Querier, kind string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal job payload: %w", err)
	}
	_, err = q.Exec(ctx, `INSERT INTO jobs (kind, payload) VALUES ($1, $2)`, kind, body)
	return err
}

type Runner struct {
	pool     *pgxpool.Pool
	handlers map[string]Handler
	// PollInterval is how long to sleep when the queue is empty.
	PollInterval time.Duration
	// JobTimeout bounds a single handler run.
	JobTimeout time.Duration
}

func NewRunner(pool *pgxpool.Pool) *Runner {
	return &Runner{
		pool:         pool,
		handlers:     make(map[string]Handler),
		PollInterval: 2 * time.Second,
		JobTimeout:   30 * time.Second,
	}
}

func (r *Runner) Register(kind string, h Handler) { r.handlers[kind] = h }

// Run processes jobs until ctx is cancelled.
func (r *Runner) Run(ctx context.Context) error {
	lastRequeue := time.Time{}
	for {
		if time.Since(lastRequeue) > time.Minute {
			r.requeueStuck(ctx)
			lastRequeue = time.Now()
		}
		worked, err := r.RunOne(ctx)
		if err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "job runner error", "error", err.Error())
		}
		if worked {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(r.PollInterval):
		}
	}
}

type job struct {
	id          int64
	kind        string
	payload     json.RawMessage
	attempts    int
	maxAttempts int
}

// RunOne claims and runs at most one ready job. It reports whether a job ran.
func (r *Runner) RunOne(ctx context.Context) (bool, error) {
	var j job
	err := r.pool.QueryRow(ctx, `
		UPDATE jobs SET status = 'running', locked_at = now(), attempts = attempts + 1
		WHERE id = (
			SELECT id FROM jobs
			WHERE status = 'queued' AND run_at <= now()
			ORDER BY run_at, id
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING id, kind, payload, attempts, max_attempts`,
	).Scan(&j.id, &j.kind, &j.payload, &j.attempts, &j.maxAttempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim job: %w", err)
	}

	runErr := r.execute(ctx, j)
	// Record the outcome even if the runner is shutting down.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if runErr == nil {
		_, err = r.pool.Exec(saveCtx,
			`UPDATE jobs SET status = 'done', finished_at = now(), locked_at = NULL, last_error = NULL WHERE id = $1`, j.id)
		slog.InfoContext(ctx, "job done", "job_id", j.id, "kind", j.kind, "attempt", j.attempts)
		return true, err
	}

	if j.attempts >= j.maxAttempts {
		_, err = r.pool.Exec(saveCtx,
			`UPDATE jobs SET status = 'failed', finished_at = now(), locked_at = NULL, last_error = $2 WHERE id = $1`,
			j.id, runErr.Error())
		slog.ErrorContext(ctx, "job failed permanently", "job_id", j.id, "kind", j.kind, "attempts", j.attempts, "error", runErr.Error())
		return true, err
	}

	_, err = r.pool.Exec(saveCtx,
		`UPDATE jobs SET status = 'queued', locked_at = NULL, last_error = $2, run_at = now() + $3 * interval '1 second' WHERE id = $1`,
		j.id, runErr.Error(), backoffSeconds(j.attempts))
	slog.WarnContext(ctx, "job failed, will retry", "job_id", j.id, "kind", j.kind, "attempt", j.attempts, "error", runErr.Error())
	return true, err
}

func (r *Runner) execute(ctx context.Context, j job) (err error) {
	h, ok := r.handlers[j.kind]
	if !ok {
		return fmt.Errorf("no handler registered for job kind %q", j.kind)
	}
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("handler panicked: %v", rec)
		}
	}()
	runCtx, cancel := context.WithTimeout(ctx, r.JobTimeout)
	defer cancel()
	return h(runCtx, j.payload)
}

// backoffSeconds: 30s, 2m, 8m, 32m.
func backoffSeconds(attempt int) int {
	return 30 << (2 * (attempt - 1))
}

// requeueStuck returns jobs to the queue when a worker died mid-run.
func (r *Runner) requeueStuck(ctx context.Context) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE jobs SET status = 'queued', locked_at = NULL
		 WHERE status = 'running' AND locked_at < now() - interval '10 minutes'`)
	if err != nil {
		slog.ErrorContext(ctx, "requeue stuck jobs", "error", err.Error())
		return
	}
	if tag.RowsAffected() > 0 {
		slog.WarnContext(ctx, "requeued stuck jobs", "count", tag.RowsAffected())
	}
}
