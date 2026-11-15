package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/jobs"
)

// JobEmail sends one customer delivery email (internal/notifications).
const JobEmail = "delivery.email"

// EmailJob is the job payload: the notification row to send.
type EmailJob struct {
	NotificationID string `json:"notification_id"`
}

type Notification struct {
	Kind      string    `json:"kind"`
	Channel   string    `json:"channel"`
	Status    string    `json:"status"`
	Reason    *string   `json:"reason"`
	Attempts  int       `json:"attempts"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (s *Service) notifications(ctx context.Context, id string) ([]Notification, error) {
	rows, err := s.pool.Query(ctx, `SELECT kind, channel, status, reason, attempts, created_at, updated_at
		FROM delivery_notifications WHERE delivery_id = $1 ORDER BY created_at`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Notification])
}

// notify records a customer message once per (delivery, kind, key) and
// queues it. It is "queued" until the mail provider accepts it; the job
// marks it sent, failed or skipped (with the reason).
func (s *Service) notify(ctx context.Context, tx pgx.Tx, r *row, kind, key string) error {
	var id string
	err := tx.QueryRow(ctx, `INSERT INTO delivery_notifications (delivery_id, customer_id, kind, dedupe_key, status)
		VALUES ($1, $2, $3, $4, 'queued') ON CONFLICT (dedupe_key) DO NOTHING RETURNING id::text`,
		r.ID, r.CustomerID, kind, fmt.Sprintf("%s:%s:%s", r.ID, kind, key)).Scan(&id)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	return jobs.Enqueue(ctx, tx, JobEmail, EmailJob{NotificationID: id})
}

// EmailFacts is what the email job needs to compose a message.
type EmailFacts struct {
	ID          string
	Kind        string
	Status      string
	CustomerID  string
	OrderNumber string
	OrderID     string
	ServiceDate *string
	Window      *string
	Approved    bool
	OptedIn     bool
}

// EmailFor loads a queued notification. Delivery emails respect the
// customer's delivery_updates preference (on unless they turned it off).
func EmailFor(ctx context.Context, q db.Querier, payload json.RawMessage) (*EmailFacts, error) {
	var job EmailJob
	if err := json.Unmarshal(payload, &job); err != nil {
		return nil, fmt.Errorf("delivery email: bad payload: %w", err)
	}
	var f EmailFacts
	err := q.QueryRow(ctx, `SELECT n.id::text, n.kind, n.status, n.customer_id::text, o.number, o.id::text,
		CASE WHEN d.schedule_confirmed_at IS NOT NULL THEN d.service_date::text END,
		CASE WHEN w.status = 'approved' THEN w.label || ' (' || to_char(w.starts_at, 'HH24:MI') || '–' || to_char(w.ends_at, 'HH24:MI') || ')' END,
		(SELECT customer_emails_approved FROM delivery_settings),
		COALESCE((SELECT enabled FROM notification_preferences p WHERE p.customer_id = n.customer_id AND p.topic = 'delivery_updates'), true)
		FROM delivery_notifications n JOIN deliveries d ON d.id = n.delivery_id JOIN orders o ON o.id = d.order_id
		LEFT JOIN delivery_windows w ON w.id = d.window_id WHERE n.id = $1`, job.NotificationID).
		Scan(&f.ID, &f.Kind, &f.Status, &f.CustomerID, &f.OrderNumber, &f.OrderID, &f.ServiceDate, &f.Window, &f.Approved, &f.OptedIn)
	return &f, err
}

// MarkEmail records the outcome of a send attempt.
func MarkEmail(ctx context.Context, q db.Querier, id, status, reason string) error {
	_, err := q.Exec(ctx, `UPDATE delivery_notifications SET status = $2, reason = $3, attempts = attempts + 1,
		last_error = CASE WHEN $2 = 'failed' THEN $3 END, updated_at = now() WHERE id = $1`, id, status, nilIfEmpty(clip(reason, 300)))
	return err
}
