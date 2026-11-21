// Package appnotify is the Mise app's notification centre, its push device
// registry and customers' account deletion requests (Sprint 10).
//
// In-app notifications are written by the worker from the same events that
// send customer emails, so the app and email never disagree. They are the
// authority: a push message only points at one, and carries ids and a
// neutral sentence — never amounts, card details or message text (they show
// on a locked screen).
//
// SQL here is hand-written (pgx) rather than sqlc-generated: a small module,
// added late in the sprint; see docs/mobile/PUSH_NOTIFICATIONS.md.
package appnotify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/jobs"
)

// JobPush sends one notification to one device.
const JobPush = "app.push"

type Service struct {
	pool   *pgxpool.Pool
	Pusher Pusher // nil: push is not configured (no Firebase project yet)
	Now    func() time.Time
}

func New(pool *pgxpool.Pool, pusher Pusher) *Service {
	return &Service{pool: pool, Pusher: pusher, Now: time.Now}
}

// Note is one notification for the centre, and the lock-screen text of its push.
type Note struct {
	Type  string // e.g. payment.failed, delivery.update, chat.reply
	Title string
	Body  string
	// Link is an in-app path such as /orders/<id>/delivery.
	Link string
	// Push shows only this (no amounts, names or message text).
	PushTitle, PushBody string
	// Identity of the event; the same key twice records once.
	DedupKey string
}

var linkRe = regexp.MustCompile(`^/[A-Za-z0-9/_-]{0,199}$`)

// Key builds a stable dedup key from an event's kind and data.
func Key(kind string, data map[string]string) string {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	h.Write([]byte(kind))
	for _, k := range keys {
		fmt.Fprintf(h, "\x00%s=%s", k, data[k])
	}
	return kind + ":" + hex.EncodeToString(h.Sum(nil))[:40]
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// Record adds a notification for a user once, and queues a push to each of
// their registered devices. Safe to call again for the same event.
func (s *Service) Record(ctx context.Context, q db.Querier, userID string, n Note) error {
	if n.Link != "" && !linkRe.MatchString(n.Link) {
		return fmt.Errorf("appnotify: link %q is not an in-app path", n.Link)
	}
	var link *string
	if n.Link != "" {
		link = &n.Link
	}
	var id string
	err := q.QueryRow(ctx, `
		INSERT INTO customer_notifications (user_id, type, title, body, link, dedup_key)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (user_id, dedup_key) DO NOTHING
		RETURNING id`, userID, n.Type, clip(n.Title, 120), clip(n.Body, 500), link, n.DedupKey).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // already recorded
	}
	if err != nil {
		return err
	}
	rows, err := q.Query(ctx, `SELECT id FROM push_devices WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	if err != nil {
		return err
	}
	var devices []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			rows.Close()
			return err
		}
		devices = append(devices, d)
	}
	rows.Close()
	for _, d := range devices {
		if err := jobs.Enqueue(ctx, q, JobPush, PushJob{DeviceID: d, NotificationID: id, Title: n.PushTitle, Body: n.PushBody}); err != nil {
			return err
		}
	}
	return nil
}

type PushJob struct {
	DeviceID       string `json:"device_id"`
	NotificationID string `json:"notification_id"`
	Title          string `json:"title"`
	Body           string `json:"body"`
}

// Register adds the push job to the worker.
func (s *Service) Register(r *jobs.Runner) { r.Register(JobPush, s.handlePush) }

func (s *Service) handlePush(ctx context.Context, payload json.RawMessage) error {
	var job PushJob
	if err := json.Unmarshal(payload, &job); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	if s.Pusher == nil {
		slog.InfoContext(ctx, "push skipped: not configured", "event", "push.skipped")
		return nil
	}
	var token, platform, typ string
	var link *string
	err := s.pool.QueryRow(ctx, `
		SELECT d.push_token, d.platform, n.type, n.link
		FROM push_devices d
		JOIN customer_notifications n ON n.id = $2 AND n.user_id = d.user_id
		-- Only while the device's sign-in is live: "sign out everywhere" and a
		-- password reset end the session, and with it the pushes.
		JOIN sessions s ON s.id = d.session_id AND s.revoked_at IS NULL AND s.expires_at > now()
		WHERE d.id = $1 AND d.revoked_at IS NULL`, job.DeviceID, job.NotificationID).Scan(&token, &platform, &typ, &link)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // device signed out, or notification gone
	}
	if err != nil {
		return err
	}
	data := map[string]string{"type": typ, "notification_id": job.NotificationID}
	if link != nil {
		data["link"] = *link
	}
	err = s.Pusher.Send(ctx, PushMessage{Token: token, Platform: platform, Title: job.Title, Body: job.Body, Data: data, CollapseKey: job.NotificationID})
	switch {
	case errors.Is(err, ErrUnregistered), errors.Is(err, ErrInvalidToken):
		reason := "unregistered"
		if errors.Is(err, ErrInvalidToken) {
			reason = "invalid_token"
		}
		_, e := s.pool.Exec(ctx, `UPDATE push_devices SET revoked_at = now(), revoked_reason = $2 WHERE id = $1 AND revoked_at IS NULL`, job.DeviceID, reason)
		slog.InfoContext(ctx, "push token removed", "event", "push.token_removed", "reason", reason)
		return e
	case err != nil:
		return err // retried by the job runner
	}
	slog.InfoContext(ctx, "push sent", "event", "push.sent", "type", typ)
	return nil
}

// RevokeSessionDevices forgets the devices registered by a session that ended.
func (s *Service) RevokeSessionDevices(ctx context.Context, sessionID string) {
	if _, err := s.pool.Exec(ctx, `UPDATE push_devices SET revoked_at = now(), revoked_reason = 'logout'
		WHERE session_id = $1 AND revoked_at IS NULL`, sessionID); err != nil {
		slog.WarnContext(ctx, "revoke push devices", "error", err.Error())
	}
}
