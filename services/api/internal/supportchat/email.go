package supportchat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/platform/db"
)

// EmailFacts is what the reply email needs. No message text is included:
// the email only says there is a reply and links to read it signed in (or,
// for a guest, in the browser that holds the chat).
type EmailFacts struct {
	NotificationID int64
	Status         string
	ConversationID string
	Number         int64
	Seq            int64
	OwnerKind      string
	CustomerID     *string
	GuestName      string
	GuestEmail     string
	// ReadSeq is how far the owner has read now; at or past Seq the email is skipped.
	ReadSeq int64
}

func EmailFor(ctx context.Context, q db.Querier, payload json.RawMessage) (*EmailFacts, error) {
	var j EmailJob
	if err := json.Unmarshal(payload, &j); err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}
	f := &EmailFacts{NotificationID: j.NotificationID}
	var guestName, guestEmail *string
	err := q.QueryRow(ctx, `SELECT n.status, c.id, c.number, n.seq, CASE WHEN c.customer_id IS NOT NULL THEN 'customer' ELSE 'guest' END,
		c.customer_id, g.name, g.email,
		coalesce((SELECT max(last_read_seq) FROM support_read_positions r WHERE r.conversation_id = c.id AND r.reader_kind IN ('customer', 'guest')), 0)
		FROM support_chat_notifications n JOIN support_conversations c ON c.id = n.conversation_id
		LEFT JOIN support_guest_sessions g ON g.id = c.guest_id WHERE n.id = $1`, j.NotificationID).
		Scan(&f.Status, &f.ConversationID, &f.Number, &f.Seq, &f.OwnerKind, &f.CustomerID, &guestName, &guestEmail, &f.ReadSeq)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("support chat notification %d not found", j.NotificationID)
	}
	if guestName != nil {
		f.GuestName = *guestName
	}
	if guestEmail != nil {
		f.GuestEmail = *guestEmail
	}
	return f, err
}

// MarkEmail records what happened to a reply email.
func MarkEmail(ctx context.Context, q db.Querier, id int64, status, reason string) error {
	_, err := q.Exec(ctx, `UPDATE support_chat_notifications SET status = $2, reason = $3,
		sent_at = CASE WHEN $2 = 'sent' THEN now() ELSE sent_at END WHERE id = $1`, id, status, reason)
	return err
}
