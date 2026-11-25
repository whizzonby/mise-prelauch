// Package supportchat is Mise Chat: conversations between customers (or
// guests) and the support team, with live delivery over Server-Sent Events.
//
// Rules this package keeps:
//   - A message is stored before it is acknowledged, numbered per
//     conversation (seq), and never edited. "Delivered" means stored.
//   - Sending is idempotent by the sender's client_message_id, so a resend
//     after a dropped connection is the same message.
//   - Internal notes live in their own table and are only read by staff
//     endpoints that check support_chat.internal_notes.
//   - A guest is a name, an email and an opaque token (hashed at rest). A
//     guest conversation joins an account only when a signed-in customer
//     with that verified email presents the guest token; never by email
//     match alone.
//   - Nothing promises a reply time: availability and expectation text are
//     whatever the business configured, empty by default.
package supportchat

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/ratelimit"
	"mise.tt/api/internal/support"
)

// Channel is the PostgreSQL NOTIFY channel live events travel on.
const Channel = "mise_support_chat"

// JobEmail sends a "you have a reply" email (notifications.ChatSender).
const JobEmail = "support_chat.email"

// GuestTTL is how long a guest token stays valid after its last use.
const GuestTTL = 30 * 24 * time.Hour

var (
	uuidPattern     = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	clientIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
	Priorities      = []string{"low", "normal", "high", "urgent"}
	Sources         = []string{"site", "app", "help"}

	ErrUnavailable = httpx.NewError(http.StatusServiceUnavailable, "chat_unavailable", "Chat is not available right now. Please email us instead.")
	errGuestToken  = httpx.NewError(http.StatusUnauthorized, "guest_session_invalid", "Your chat session has ended. Start a new conversation.")
)

// Service is Mise Chat.
type Service struct {
	pool   *pgxpool.Pool
	Hub    *Hub
	Alerts support.Alerts
	Cases  *support.Service
	Now    func() time.Time
	typing *throttle
	// SendLimit bounds messages per customer or guest (message flooding).
	// Generous for a person typing; it stops scripts. Per API instance.
	SendLimit *ratelimit.Limiter
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, Hub: NewHub(pool), Now: time.Now, typing: newThrottle(3 * time.Second),
		SendLimit: ratelimit.New(20, time.Minute, 10)}
}

var errFlooding = httpx.NewError(http.StatusTooManyRequests, "rate_limited", "You're sending messages very quickly. Wait a moment, then try again.")

func (s *Service) allowSend(o Owner) error {
	if s.SendLimit != nil && !s.SendLimit.Allow(o.key()) {
		return errFlooding
	}
	return nil
}

// Owner is who a conversation belongs to: a customer or a guest.
type Owner struct {
	Kind string // "customer" or "guest"
	ID   string // customers.id or support_guest_sessions.id
}

func (o Owner) key() string { return o.Kind + ":" + o.ID }

func (o Owner) column() string {
	if o.Kind == "guest" {
		return "guest_id"
	}
	return "customer_id"
}

// Conversation is a conversation as its owner sees it.
type Conversation struct {
	ID             string     `json:"id"`
	Number         int64      `json:"number"`
	Status         string     `json:"status"`
	Subject        string     `json:"subject"`
	LastSeq        int64      `json:"last_seq"`
	LastMessage    string     `json:"last_message"`
	LastMessageAt  *time.Time `json:"last_message_at"`
	Unread         int64      `json:"unread"`
	TeamReadSeq    int64      `json:"team_read_seq"`
	FeedbackRating *int       `json:"feedback_rating"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// Message is one chat message. AuthorName is the agent's first name (or
// "Mise team") for agent messages, empty otherwise.
type Message struct {
	ConversationID  string    `json:"conversation_id"`
	Seq             int64     `json:"seq"`
	Author          string    `json:"author"`
	AuthorName      string    `json:"author_name"`
	Body            string    `json:"body"`
	ClientMessageID *string   `json:"client_message_id"`
	CreatedAt       time.Time `json:"created_at"`
}

// ---------------------------------------------------------------- validation

func cleanBody(body string) (string, error) {
	body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
	if n := utf8.RuneCountInString(body); n < 1 || n > 4000 {
		return "", httpx.ValidationError([]httpx.FieldError{{Field: "body", Code: "invalid", Message: "Write a message (up to 4,000 characters)."}})
	}
	return body, nil
}

func clientID(v string) (*string, error) {
	if v == "" {
		return nil, nil
	}
	if !clientIDPattern.MatchString(v) {
		return nil, httpx.ValidationError([]httpx.FieldError{{Field: "client_message_id", Code: "invalid", Message: "Use 8 to 64 letters, digits, - or _."}})
	}
	return &v, nil
}

func validUUID(id string) bool { return uuidPattern.MatchString(id) }

// ---------------------------------------------------------------- guests

// Guest is a guest session (the token is returned only when created).
type Guest struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Token string `json:"token,omitempty"`
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "mgt_" + base64.RawURLEncoding.EncodeToString(b), nil
}

// GuestFromToken returns the guest session for an opaque token, extending it.
func (s *Service) GuestFromToken(ctx context.Context, token string) (*Guest, error) {
	if !strings.HasPrefix(token, "mgt_") || len(token) > 100 {
		return nil, errGuestToken
	}
	var g Guest
	err := s.pool.QueryRow(ctx, `UPDATE support_guest_sessions SET last_seen_at = now(), expires_at = greatest(expires_at, $2)
		WHERE token_hash = $1 AND expires_at > now() AND linked_customer_id IS NULL RETURNING id, name, email`,
		hashToken(token), s.Now().Add(GuestTTL)).Scan(&g.ID, &g.Name, &g.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errGuestToken
	}
	return &g, err
}

// GuestInput starts a guest conversation.
type GuestInput struct {
	Name            string `json:"name"`
	Email           string `json:"email"`
	Body            string `json:"body"`
	ClientMessageID string `json:"client_message_id"`
	Source          string `json:"source"`
}

// maxGuestStartsPerEmail bounds new guest sessions for one address per hour.
const maxGuestStartsPerEmail = 5

// StartGuest creates a guest session and its first conversation.
func (s *Service) StartGuest(ctx context.Context, in GuestInput) (*Guest, *Conversation, *Message, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return nil, nil, nil, err
	}
	var f []httpx.FieldError
	in.Name = strings.TrimSpace(in.Name)
	in.Email = strings.TrimSpace(in.Email)
	if n := utf8.RuneCountInString(in.Name); n < 1 || n > 100 {
		f = append(f, httpx.FieldError{Field: "name", Code: "invalid", Message: "Tell us your name."})
	}
	if a, err := mail.ParseAddress(in.Email); err != nil || a.Address != in.Email || len(in.Email) > 254 {
		f = append(f, httpx.FieldError{Field: "email", Code: "invalid", Message: "Enter an email address we can reply to."})
	}
	body, err := cleanBody(in.Body)
	if err != nil {
		var he *httpx.Error
		if errors.As(err, &he) {
			f = append(f, he.Fields...)
		}
	}
	cid, err := clientID(in.ClientMessageID)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(f) > 0 {
		return nil, nil, nil, httpx.ValidationError(f)
	}
	if !slices.Contains(Sources, in.Source) {
		in.Source = "site"
	}
	var recent int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM support_guest_sessions WHERE lower(email) = lower($1) AND created_at > now() - interval '1 hour'`,
		in.Email).Scan(&recent); err != nil {
		return nil, nil, nil, err
	}
	if recent >= maxGuestStartsPerEmail {
		return nil, nil, nil, httpx.NewError(http.StatusTooManyRequests, "rate_limited", "Too many new conversations for this email address. Try again later.")
	}
	token, err := newToken()
	if err != nil {
		return nil, nil, nil, err
	}
	g := &Guest{Name: in.Name, Email: in.Email, Token: token}
	var conv *Conversation
	var msg *Message
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO support_guest_sessions (token_hash, name, email, expires_at) VALUES ($1, $2, $3, $4) RETURNING id`,
			hashToken(token), in.Name, in.Email, s.Now().Add(GuestTTL)).Scan(&g.ID); err != nil {
			return err
		}
		var err error
		conv, msg, err = s.create(ctx, tx, Owner{Kind: "guest", ID: g.ID}, in.Source, "", body, cid)
		return err
	})
	if err != nil {
		return nil, nil, nil, err
	}
	return g, conv, msg, nil
}

// LinkGuest moves a guest's conversations to the signed-in customer. The
// caller must hold the guest token (same browser) and a verified email that
// matches the guest's email.
func (s *Service) LinkGuest(ctx context.Context, customerID, email string, emailVerified bool, token string) (int, error) {
	g, err := s.GuestFromToken(ctx, token)
	if err != nil {
		return 0, err
	}
	if !emailVerified {
		return 0, httpx.NewError(http.StatusForbidden, "email_not_verified", "Verify your email address first, then your chat history can join your account.")
	}
	if !strings.EqualFold(strings.TrimSpace(g.Email), strings.TrimSpace(email)) {
		return 0, httpx.NewError(http.StatusForbidden, "email_mismatch", "This chat was started with a different email address.")
	}
	moved := 0
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `UPDATE support_conversations SET customer_id = $2, guest_id = NULL, updated_at = now()
			WHERE guest_id = $1 RETURNING id`, g.ID, customerID)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		moved = len(ids)
		for _, id := range ids {
			if err := addEvent(ctx, tx, id, "guest_linked", nil, ""); err != nil {
				return err
			}
			// The guest's read position becomes the customer's.
			if _, err := tx.Exec(ctx, `INSERT INTO support_read_positions (conversation_id, reader_kind, reader_id, last_read_seq)
				SELECT conversation_id, 'customer', $2, last_read_seq FROM support_read_positions
				WHERE conversation_id = $1 AND reader_kind = 'guest' AND reader_id = $3
				ON CONFLICT DO NOTHING`, id, customerID, g.ID); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE support_guest_sessions SET linked_customer_id = $2, linked_at = now() WHERE id = $1`, g.ID, customerID)
		return err
	})
	return moved, err
}

// ---------------------------------------------------------------- owner actions

// StartInput starts a conversation for a signed-in customer.
type StartInput struct {
	Body            string `json:"body"`
	ClientMessageID string `json:"client_message_id"`
	Subject         string `json:"subject"`
	Source          string `json:"source"`
}

func (s *Service) Start(ctx context.Context, o Owner, in StartInput) (*Conversation, *Message, error) {
	if err := s.allowSend(o); err != nil {
		return nil, nil, err
	}
	if err := s.requireEnabled(ctx); err != nil {
		return nil, nil, err
	}
	body, err := cleanBody(in.Body)
	if err != nil {
		return nil, nil, err
	}
	cid, err := clientID(in.ClientMessageID)
	if err != nil {
		return nil, nil, err
	}
	if !slices.Contains(Sources, in.Source) {
		in.Source = "app"
	}
	subject := strings.TrimSpace(in.Subject)
	if utf8.RuneCountInString(subject) > 160 {
		return nil, nil, httpx.ValidationError([]httpx.FieldError{{Field: "subject", Code: "invalid", Message: "Keep the subject under 160 characters."}})
	}
	var conv *Conversation
	var msg *Message
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		// A retried start (same client message id) returns the conversation it already made.
		if cid != nil {
			var existing string
			err := tx.QueryRow(ctx, `SELECT c.id FROM support_messages m JOIN support_conversations c ON c.id = m.conversation_id
				WHERE m.client_message_id = $1 AND m.seq = 1 AND c.`+o.column()+` = $2`, *cid, o.ID).Scan(&existing)
			if err == nil {
				conv, err = s.ownerConversation(ctx, tx, o, existing)
				if err != nil {
					return err
				}
				msg, err = messageBySeq(ctx, tx, existing, 1)
				return err
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		var err error
		conv, msg, err = s.create(ctx, tx, o, in.Source, subject, body, cid)
		return err
	})
	return conv, msg, err
}

func (s *Service) create(ctx context.Context, tx pgx.Tx, o Owner, source, subject, body string, cid *string) (*Conversation, *Message, error) {
	var id string
	if err := tx.QueryRow(ctx, `INSERT INTO support_conversations (`+o.column()+`, source, subject) VALUES ($1, $2, $3) RETURNING id`,
		o.ID, source, subject).Scan(&id); err != nil {
		return nil, nil, err
	}
	if err := addEvent(ctx, tx, id, "created", map[string]any{"source": source}, ""); err != nil {
		return nil, nil, err
	}
	msg, err := s.insertMessage(ctx, tx, id, o.Kind, "", body, cid)
	if err != nil {
		return nil, nil, err
	}
	if err := s.afterOwnerMessage(ctx, tx, id, o, msg.Seq); err != nil {
		return nil, nil, err
	}
	conv, err := s.ownerConversation(ctx, tx, o, id)
	return conv, msg, err
}

// SendInput is a new message in an existing conversation.
type SendInput struct {
	Body            string `json:"body"`
	ClientMessageID string `json:"client_message_id"`
}

// Send adds the owner's message. A resolved conversation reopens.
func (s *Service) Send(ctx context.Context, o Owner, conversationID string, in SendInput) (*Message, error) {
	if err := s.allowSend(o); err != nil {
		return nil, err
	}
	body, err := cleanBody(in.Body)
	if err != nil {
		return nil, err
	}
	cid, err := clientID(in.ClientMessageID)
	if err != nil {
		return nil, err
	}
	var msg *Message
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var status string
		err := tx.QueryRow(ctx, `SELECT status FROM support_conversations WHERE id = $1 AND `+o.column()+` = $2 FOR UPDATE`,
			conversationID, o.ID).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		if cid != nil {
			if m, err := messageByClientID(ctx, tx, conversationID, *cid); err == nil {
				msg = m
				return nil
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		if msg, err = s.insertMessage(ctx, tx, conversationID, o.Kind, "", body, cid); err != nil {
			return err
		}
		if status == "resolved" {
			if err := addEvent(ctx, tx, conversationID, "reopened", map[string]any{"by": o.Kind}, ""); err != nil {
				return err
			}
		}
		return s.afterOwnerMessage(ctx, tx, conversationID, o, msg.Seq)
	})
	return msg, err
}

// afterOwnerMessage: the conversation needs the team again, the owner has
// read their own message, and the team is alerted if nobody has it.
func (s *Service) afterOwnerMessage(ctx context.Context, tx pgx.Tx, conversationID string, o Owner, seq int64) error {
	var number int64
	var assignee *string
	if err := tx.QueryRow(ctx, `UPDATE support_conversations SET status = 'open', resolved_at = NULL, last_customer_message_at = now(), updated_at = now()
		WHERE id = $1 RETURNING number, assignee_id`, conversationID).Scan(&number, &assignee); err != nil {
		return err
	}
	if err := markRead(ctx, tx, conversationID, o.Kind, o.ID, seq); err != nil {
		return err
	}
	if assignee == nil && s.Alerts != nil {
		return s.Alerts.Raise(ctx, tx, support.Alert{
			DedupeKey: "support_chat.unassigned:" + conversationID, Kind: "support_chat", Severity: "info",
			Title: "Chat waiting for a reply", Body: "Conversation " + itoa(number) + " has a message and nobody is assigned.",
			ResourceType: "support_conversation", ResourceID: conversationID, Permission: "support_chat.read",
		})
	}
	return nil
}

// insertMessage stores a message with the next seq and announces it.
func (s *Service) insertMessage(ctx context.Context, tx pgx.Tx, conversationID, author, authorUserID, body string, cid *string) (*Message, error) {
	var seq int64
	if err := tx.QueryRow(ctx, `UPDATE support_conversations SET last_seq = last_seq + 1, last_message_at = now(), updated_at = now()
		WHERE id = $1 RETURNING last_seq`, conversationID).Scan(&seq); err != nil {
		return nil, err
	}
	var uid *string
	if authorUserID != "" {
		uid = &authorUserID
	}
	m := &Message{ConversationID: conversationID, Seq: seq, Author: author, Body: body, ClientMessageID: cid}
	if err := tx.QueryRow(ctx, `INSERT INTO support_messages (conversation_id, seq, author_kind, author_user_id, body, client_message_id)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING created_at`, conversationID, seq, author, uid, body, cid).Scan(&m.CreatedAt); err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" {
			return nil, httpx.NewError(http.StatusConflict, "duplicate_message", "That message was already sent.")
		}
		return nil, err
	}
	if author == "agent" {
		m.AuthorName = agentName(ctx, tx, authorUserID)
	}
	return m, notify(ctx, tx, Event{Kind: "message", Conversation: conversationID, Seq: seq, Actor: author})
}

func agentName(ctx context.Context, q db.Querier, userID string) string {
	var first string
	if userID != "" {
		_ = q.QueryRow(ctx, `SELECT first_name FROM users WHERE id = $1`, userID).Scan(&first)
	}
	if strings.TrimSpace(first) == "" {
		return "Mise team"
	}
	return first
}

const messageCols = `m.conversation_id, m.seq, m.author_kind, CASE WHEN m.author_kind = 'agent' THEN coalesce(nullif(trim(u.first_name), ''), 'Mise team') ELSE '' END,
	m.body, m.client_message_id, m.created_at`

func scanMessages(rows pgx.Rows) ([]Message, error) {
	defer rows.Close()
	out := []Message{}
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ConversationID, &m.Seq, &m.Author, &m.AuthorName, &m.Body, &m.ClientMessageID, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func queryMessages(ctx context.Context, q db.Querier, sql string, args ...any) ([]Message, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return scanMessages(rows)
}

func oneMessage(ctx context.Context, q db.Querier, where string, args ...any) (*Message, error) {
	ms, err := queryMessages(ctx, q, `SELECT `+messageCols+` FROM support_messages m LEFT JOIN users u ON u.id = m.author_user_id WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	if len(ms) == 0 {
		return nil, pgx.ErrNoRows
	}
	return &ms[0], nil
}

func messageBySeq(ctx context.Context, q db.Querier, conversationID string, seq int64) (*Message, error) {
	return oneMessage(ctx, q, `m.conversation_id = $1 AND m.seq = $2`, conversationID, seq)
}

func messageByClientID(ctx context.Context, q db.Querier, conversationID, cid string) (*Message, error) {
	return oneMessage(ctx, q, `m.conversation_id = $1 AND m.client_message_id = $2`, conversationID, cid)
}

// Messages lists messages after seq (at most limit), oldest first.
func Messages(ctx context.Context, q db.Querier, conversationID string, after int64, limit int) ([]Message, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	return queryMessages(ctx, q, `SELECT `+messageCols+` FROM support_messages m LEFT JOIN users u ON u.id = m.author_user_id
		WHERE m.conversation_id = $1 AND m.seq > $2 ORDER BY m.seq LIMIT $3`, conversationID, after, limit)
}

// OwnerMessages lists the owner's messages after seq.
func (s *Service) OwnerMessages(ctx context.Context, o Owner, conversationID string, after int64) ([]Message, error) {
	if _, err := s.ownerConversation(ctx, s.pool, o, conversationID); err != nil {
		return nil, err
	}
	return Messages(ctx, s.pool, conversationID, after, 200)
}

const ownerConversationSQL = `SELECT c.id, c.number, c.status, c.subject, c.last_seq,
	coalesce((SELECT body FROM support_messages WHERE conversation_id = c.id ORDER BY seq DESC LIMIT 1), ''),
	c.last_message_at,
	(SELECT count(*) FROM support_messages m WHERE m.conversation_id = c.id AND m.author_kind = 'agent'
		AND m.seq > coalesce((SELECT last_read_seq FROM support_read_positions r WHERE r.conversation_id = c.id AND r.reader_kind = $3 AND r.reader_id = $2), 0)),
	coalesce((SELECT max(last_read_seq) FROM support_read_positions r WHERE r.conversation_id = c.id AND r.reader_kind = 'agent'), 0),
	(SELECT rating FROM support_feedback f WHERE f.conversation_id = c.id),
	c.created_at, c.updated_at
	FROM support_conversations c`

func scanOwnerConversation(row pgx.Row) (*Conversation, error) {
	var c Conversation
	var rating *int16
	if err := row.Scan(&c.ID, &c.Number, &c.Status, &c.Subject, &c.LastSeq, &c.LastMessage, &c.LastMessageAt, &c.Unread, &c.TeamReadSeq,
		&rating, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	if rating != nil {
		r := int(*rating)
		c.FeedbackRating = &r
	}
	if utf8.RuneCountInString(c.LastMessage) > 140 {
		c.LastMessage = string([]rune(c.LastMessage)[:140]) + "…"
	}
	return &c, nil
}

func (s *Service) ownerConversation(ctx context.Context, q db.Querier, o Owner, id string) (*Conversation, error) {
	if !validUUID(id) {
		return nil, httpx.ErrNotFound
	}
	c, err := scanOwnerConversation(q.QueryRow(ctx, ownerConversationSQL+` WHERE c.id = $1 AND c.`+o.column()+` = $2`, id, o.ID, o.Kind))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	return c, err
}

// OwnerConversation is one of the owner's conversations.
func (s *Service) OwnerConversation(ctx context.Context, o Owner, id string) (*Conversation, error) {
	return s.ownerConversation(ctx, s.pool, o, id)
}

// OwnerConversations lists the owner's conversations, newest first.
func (s *Service) OwnerConversations(ctx context.Context, o Owner) ([]Conversation, error) {
	rows, err := s.pool.Query(ctx, ownerConversationSQL+` WHERE c.`+o.column()+` = $1 ORDER BY c.updated_at DESC LIMIT 50`,
		o.ID, o.ID, o.Kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Conversation{}
	for rows.Next() {
		c, err := scanOwnerConversation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// MarkRead records that the owner has read up to seq.
func (s *Service) MarkRead(ctx context.Context, o Owner, conversationID string, seq int64) error {
	c, err := s.ownerConversation(ctx, s.pool, o, conversationID)
	if err != nil {
		return err
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := markRead(ctx, tx, c.ID, o.Kind, o.ID, min(seq, c.LastSeq)); err != nil {
			return err
		}
		return notify(ctx, tx, Event{Kind: "read", Conversation: c.ID, Actor: o.Kind})
	})
}

func markRead(ctx context.Context, q db.Querier, conversationID, readerKind, readerID string, seq int64) error {
	_, err := q.Exec(ctx, `INSERT INTO support_read_positions (conversation_id, reader_kind, reader_id, last_read_seq) VALUES ($1, $2, $3, $4)
		ON CONFLICT (conversation_id, reader_kind, reader_id) DO UPDATE
		SET last_read_seq = greatest(support_read_positions.last_read_seq, EXCLUDED.last_read_seq), updated_at = now()`,
		conversationID, readerKind, readerID, seq)
	return err
}

// Typing announces that the owner is typing. Never stored; throttled.
func (s *Service) Typing(ctx context.Context, o Owner, conversationID string) error {
	if _, err := s.ownerConversation(ctx, s.pool, o, conversationID); err != nil {
		return err
	}
	if !s.typing.allow(conversationID + "|" + o.key()) {
		return nil
	}
	return notify(ctx, s.pool, Event{Kind: "typing", Conversation: conversationID, Actor: o.Kind})
}

// FeedbackInput rates a conversation.
type FeedbackInput struct {
	Rating  int    `json:"rating"`
	Comment string `json:"comment"`
}

func (s *Service) Feedback(ctx context.Context, o Owner, conversationID string, in FeedbackInput) error {
	if _, err := s.ownerConversation(ctx, s.pool, o, conversationID); err != nil {
		return err
	}
	in.Comment = strings.TrimSpace(in.Comment)
	if in.Rating < 1 || in.Rating > 5 || utf8.RuneCountInString(in.Comment) > 1000 {
		return httpx.ValidationError([]httpx.FieldError{{Field: "rating", Code: "invalid", Message: "Choose a rating from 1 to 5."}})
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO support_feedback (conversation_id, rating, comment) VALUES ($1, $2, $3)
		ON CONFLICT (conversation_id) DO UPDATE SET rating = EXCLUDED.rating, comment = EXCLUDED.comment, updated_at = now()`,
		conversationID, in.Rating, in.Comment)
	return err
}

// ---------------------------------------------------------------- events

func addEvent(ctx context.Context, q db.Querier, conversationID, typ string, detail map[string]any, actor string) error {
	if detail == nil {
		detail = map[string]any{}
	}
	b, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	var a *string
	if actor != "" {
		a = &actor
	}
	_, err = q.Exec(ctx, `INSERT INTO support_conversation_events (conversation_id, type, detail, actor_id) VALUES ($1, $2, $3, $4)`,
		conversationID, typ, b, a)
	return err
}

// queueReplyEmail asks for one "you have a reply" email per conversation per
// cool-down window. It is sent a little later, and skipped if the owner has
// read the reply by then.
func (s *Service) queueReplyEmail(ctx context.Context, tx pgx.Tx, conversationID string, seq int64) error {
	var cooldown int
	if err := tx.QueryRow(ctx, `SELECT email_cooldown_minutes FROM support_chat_settings`).Scan(&cooldown); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		cooldown = 30
	}
	window := s.Now().Unix() / int64(cooldown*60)
	var id int64
	err := tx.QueryRow(ctx, `INSERT INTO support_chat_notifications (conversation_id, dedupe_key, seq) VALUES ($1, $2, $3)
		ON CONFLICT (dedupe_key) DO NOTHING RETURNING id`, conversationID, conversationID+":"+itoa(window), seq).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // already queued in this window
	}
	if err != nil {
		return err
	}
	// Sent a little later, so a customer reading the open chat is not emailed too.
	payload, err := json.Marshal(EmailJob{NotificationID: id})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO jobs (kind, payload, run_at) VALUES ($1, $2, now() + $3::interval)`, JobEmail, payload, EmailDelay)
	return err
}

// EmailDelay is how long a reply email waits (a PostgreSQL interval).
var EmailDelay = "2 minutes"

// EmailJob is the payload of JobEmail.
type EmailJob struct {
	NotificationID int64 `json:"notification_id"`
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
