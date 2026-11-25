package supportchat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/support"
)

// InboxItem is a conversation as the support team sees it in the list.
type InboxItem struct {
	ID             string     `json:"id"`
	Number         int64      `json:"number"`
	Status         string     `json:"status"`
	Priority       string     `json:"priority"`
	Tags           []string   `json:"tags"`
	Subject        string     `json:"subject"`
	Source         string     `json:"source"`
	OwnerKind      string     `json:"owner_kind"`
	OwnerName      string     `json:"owner_name"`
	CustomerID     *string    `json:"customer_id"`
	AssigneeID     *string    `json:"assignee_id"`
	AssigneeName   string     `json:"assignee_name"`
	Team           *string    `json:"team"`
	LastSeq        int64      `json:"last_seq"`
	LastMessage    string     `json:"last_message"`
	LastAuthor     string     `json:"last_author"`
	LastMessageAt  *time.Time `json:"last_message_at"`
	Unread         int64      `json:"unread"`
	CaseID         *string    `json:"case_id"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	FirstResponse  *time.Time `json:"first_human_response_at"`
	ResolvedAt     *time.Time `json:"resolved_at"`
	FeedbackRating *int       `json:"feedback_rating"`
}

// InboxFilter selects conversations for the inbox.
type InboxFilter struct {
	View string // open (default), mine, unassigned, pending, resolved, all
	Q    string
}

var Views = []string{"open", "mine", "unassigned", "pending", "resolved", "all"}

const inboxSelect = `SELECT c.id, c.number, c.status, c.priority, c.tags, c.subject, c.source,
	CASE WHEN c.customer_id IS NOT NULL THEN 'customer' ELSE 'guest' END,
	coalesce(nullif(trim(cu.first_name || ' ' || cu.last_name), ''), cu.email, g.name, ''),
	c.customer_id, c.assignee_id, coalesce(nullif(trim(au.first_name || ' ' || au.last_name), ''), au.email, ''), c.team, c.last_seq,
	coalesce(lm.body, ''), coalesce(lm.author_kind, ''), c.last_message_at,
	(SELECT count(*) FROM support_messages m WHERE m.conversation_id = c.id AND m.author_kind IN ('customer', 'guest')
		AND m.seq > coalesce((SELECT last_read_seq FROM support_read_positions r WHERE r.conversation_id = c.id AND r.reader_kind = 'agent' AND r.reader_id = $1), 0)),
	c.case_id, c.created_at, c.updated_at, c.first_human_response_at, c.resolved_at,
	(SELECT rating FROM support_feedback f WHERE f.conversation_id = c.id)
	FROM support_conversations c
	LEFT JOIN customers cus ON cus.id = c.customer_id
	LEFT JOIN users cu ON cu.id = cus.user_id
	LEFT JOIN support_guest_sessions g ON g.id = c.guest_id
	LEFT JOIN users au ON au.id = c.assignee_id
	LEFT JOIN LATERAL (SELECT body, author_kind FROM support_messages WHERE conversation_id = c.id ORDER BY seq DESC LIMIT 1) lm ON true`

func scanInbox(row pgx.Row) (*InboxItem, error) {
	var it InboxItem
	var rating *int16
	if err := row.Scan(&it.ID, &it.Number, &it.Status, &it.Priority, &it.Tags, &it.Subject, &it.Source, &it.OwnerKind, &it.OwnerName,
		&it.CustomerID, &it.AssigneeID, &it.AssigneeName, &it.Team, &it.LastSeq, &it.LastMessage, &it.LastAuthor, &it.LastMessageAt,
		&it.Unread, &it.CaseID, &it.CreatedAt, &it.UpdatedAt, &it.FirstResponse, &it.ResolvedAt, &rating); err != nil {
		return nil, err
	}
	if rating != nil {
		r := int(*rating)
		it.FeedbackRating = &r
	}
	if utf8.RuneCountInString(it.LastMessage) > 140 {
		it.LastMessage = string([]rune(it.LastMessage)[:140]) + "…"
	}
	return &it, nil
}

// Inbox lists conversations for a view, most recently active first.
func (s *Service) Inbox(ctx context.Context, p *identity.Principal, f InboxFilter) ([]InboxItem, error) {
	where := []string{"true"}
	args := []any{p.UserID}
	switch f.View {
	case "mine":
		where = append(where, "c.assignee_id = $1", "c.status <> 'resolved'")
	case "unassigned":
		where = append(where, "c.assignee_id IS NULL", "c.status <> 'resolved'")
	case "pending":
		where = append(where, "c.status = 'pending'")
	case "resolved":
		where = append(where, "c.status = 'resolved'")
	case "all":
	default:
		where = append(where, "c.status = 'open'")
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		if len(q) > 100 {
			q = q[:100]
		}
		args = append(args, "%"+strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.ToLower(q))+"%")
		where = append(where, `(lower(c.subject) LIKE $2 OR lower(coalesce(cu.email, g.email, '')) LIKE $2
			OR lower(coalesce(cu.first_name || ' ' || cu.last_name, g.name, '')) LIKE $2 OR c.number::text = trim(both '%' from $2)
			OR EXISTS (SELECT 1 FROM support_messages sm WHERE sm.conversation_id = c.id AND lower(sm.body) LIKE $2))`)
	}
	order := "c.updated_at DESC"
	if f.View == "" || f.View == "open" || f.View == "unassigned" {
		// Oldest waiting customer first.
		order = "c.last_customer_message_at ASC NULLS LAST"
	}
	rows, err := s.pool.Query(ctx, inboxSelect+` WHERE `+strings.Join(where, " AND ")+` ORDER BY `+order+` LIMIT 200`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InboxItem{}
	for rows.Next() {
		it, err := scanInbox(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// Note is an internal note.
type Note struct {
	ID         int64     `json:"id"`
	AuthorID   *string   `json:"author_id"`
	AuthorName string    `json:"author_name"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
}

// ConvEvent is one entry of a conversation's history.
type ConvEvent struct {
	Type      string          `json:"type"`
	Detail    json.RawMessage `json:"detail"`
	ActorID   *string         `json:"actor_id"`
	ActorName string          `json:"actor_name"`
	CreatedAt time.Time       `json:"created_at"`
}

// Contact is who the conversation is with. Email shows for guests (they
// gave it to chat) and for customers only to staff with customers.read.
type Contact struct {
	Kind          string  `json:"kind"`
	Name          string  `json:"name"`
	Email         *string `json:"email"`
	CustomerID    *string `json:"customer_id"`
	EmailVerified *bool   `json:"email_verified"`
}

// Detail is a conversation for the agent workspace.
type Detail struct {
	InboxItem
	Contact        Contact     `json:"contact"`
	Messages       []Message   `json:"messages"`
	Notes          []Note      `json:"notes"`
	NotesVisible   bool        `json:"notes_visible"`
	Events         []ConvEvent `json:"events"`
	OwnerReadSeq   int64       `json:"owner_read_seq"`
	OrderID        *string     `json:"order_id"`
	SubscriptionID *string     `json:"subscription_id"`
	Feedback       *struct {
		Rating  int    `json:"rating"`
		Comment string `json:"comment"`
	} `json:"feedback"`
}

func (s *Service) item(ctx context.Context, q db.Querier, agentID, id string) (*InboxItem, error) {
	if !validUUID(id) {
		return nil, httpx.ErrNotFound
	}
	it, err := scanInbox(q.QueryRow(ctx, inboxSelect+` WHERE c.id = $2`, agentID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	return it, err
}

// Get is the agent view of one conversation. Notes are included only for
// staff who hold support_chat.internal_notes.
func (s *Service) Get(ctx context.Context, p *identity.Principal, id string) (*Detail, error) {
	it, err := s.item(ctx, s.pool, p.UserID, id)
	if err != nil {
		return nil, err
	}
	d := &Detail{InboxItem: *it, Notes: []Note{}, Events: []ConvEvent{}}
	var guestEmail, custEmail *string
	var verified *bool
	if err := s.pool.QueryRow(ctx, `SELECT g.email, u.email, u.email_verified_at IS NOT NULL, c.order_id, c.subscription_id,
		coalesce((SELECT max(last_read_seq) FROM support_read_positions r WHERE r.conversation_id = c.id AND r.reader_kind IN ('customer', 'guest')), 0)
		FROM support_conversations c LEFT JOIN support_guest_sessions g ON g.id = c.guest_id
		LEFT JOIN customers cus ON cus.id = c.customer_id LEFT JOIN users u ON u.id = cus.user_id WHERE c.id = $1`, id).
		Scan(&guestEmail, &custEmail, &verified, &d.OrderID, &d.SubscriptionID, &d.OwnerReadSeq); err != nil {
		return nil, err
	}
	d.Contact = Contact{Kind: it.OwnerKind, Name: it.OwnerName, CustomerID: it.CustomerID}
	if it.OwnerKind == "guest" {
		d.Contact.Email = guestEmail
	} else if p.Can(identity.PermCustomersRead) {
		d.Contact.Email, d.Contact.EmailVerified = custEmail, verified
	}
	if d.Messages, err = Messages(ctx, s.pool, id, 0, 200); err != nil {
		return nil, err
	}
	if p.Can(identity.PermSupportChatInternalNotes) {
		d.NotesVisible = true
		rows, err := s.pool.Query(ctx, `SELECT n.id, n.author_id, coalesce(nullif(trim(u.first_name || ' ' || u.last_name), ''), u.email, ''), n.body, n.created_at
			FROM support_internal_notes n LEFT JOIN users u ON u.id = n.author_id WHERE n.conversation_id = $1 ORDER BY n.id`, id)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var n Note
			if err := rows.Scan(&n.ID, &n.AuthorID, &n.AuthorName, &n.Body, &n.CreatedAt); err != nil {
				rows.Close()
				return nil, err
			}
			d.Notes = append(d.Notes, n)
		}
		rows.Close()
	}
	rows, err := s.pool.Query(ctx, `SELECT e.type, e.detail, e.actor_id, coalesce(nullif(trim(u.first_name || ' ' || u.last_name), ''), u.email, ''), e.created_at
		FROM support_conversation_events e LEFT JOIN users u ON u.id = e.actor_id WHERE e.conversation_id = $1 ORDER BY e.id`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var e ConvEvent
		if err := rows.Scan(&e.Type, &e.Detail, &e.ActorID, &e.ActorName, &e.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		d.Events = append(d.Events, e)
	}
	rows.Close()
	var rating *int16
	var comment string
	if err := s.pool.QueryRow(ctx, `SELECT rating, comment FROM support_feedback WHERE conversation_id = $1`, id).Scan(&rating, &comment); err == nil && rating != nil {
		d.Feedback = &struct {
			Rating  int    `json:"rating"`
			Comment string `json:"comment"`
		}{int(*rating), comment}
	}
	return d, nil
}

// AgentMessages lists messages after seq for the agent workspace.
func (s *Service) AgentMessages(ctx context.Context, id string, after int64) ([]Message, error) {
	if !validUUID(id) {
		return nil, httpx.ErrNotFound
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM support_conversations WHERE id = $1)`, id).Scan(&exists); err != nil || !exists {
		if err == nil {
			err = httpx.ErrNotFound
		}
		return nil, err
	}
	return Messages(ctx, s.pool, id, after, 200)
}

// lock locks a conversation for an agent change and returns its status and assignee.
func lock(ctx context.Context, tx pgx.Tx, id string) (status string, assignee *string, err error) {
	if !validUUID(id) {
		return "", nil, httpx.ErrNotFound
	}
	err = tx.QueryRow(ctx, `SELECT status, assignee_id FROM support_conversations WHERE id = $1 FOR UPDATE`, id).Scan(&status, &assignee)
	if errors.Is(err, pgx.ErrNoRows) {
		err = httpx.ErrNotFound
	}
	return
}

// Reply adds an agent message. The conversation then waits for the
// customer (pending), is assigned to the replying agent if nobody had it,
// and the owner is emailed at most once per cool-down window.
func (s *Service) Reply(ctx context.Context, p *identity.Principal, id string, in SendInput) (*Message, error) {
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
		status, assignee, err := lock(ctx, tx, id)
		if err != nil {
			return err
		}
		if cid != nil {
			if m, err := messageByClientID(ctx, tx, id, *cid); err == nil {
				msg = m
				return nil
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		if msg, err = s.insertMessage(ctx, tx, id, "agent", p.UserID, body, cid); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE support_conversations SET status = 'pending', resolved_at = NULL,
			first_human_response_at = coalesce(first_human_response_at, now()), assignee_id = coalesce(assignee_id, $2), updated_at = now()
			WHERE id = $1`, id, p.UserID); err != nil {
			return err
		}
		if assignee == nil {
			if err := addEvent(ctx, tx, id, "assigned", map[string]any{"to": p.UserID, "reason": "replied"}, p.UserID); err != nil {
				return err
			}
		}
		if status == "resolved" {
			if err := addEvent(ctx, tx, id, "reopened", map[string]any{"by": "agent"}, p.UserID); err != nil {
				return err
			}
		}
		if err := markRead(ctx, tx, id, "agent", p.UserID, msg.Seq); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE admin_alerts SET resolved_at = now() WHERE dedupe_key = $1 AND resolved_at IS NULL`,
			"support_chat.unassigned:"+id); err != nil {
			return err
		}
		return s.queueReplyEmail(ctx, tx, id, msg.Seq)
	})
	return msg, err
}

// AddNote adds an internal note (staff only; never shown to the customer).
func (s *Service) AddNote(ctx context.Context, p *identity.Principal, id, body string) (*Note, error) {
	body = strings.TrimSpace(body)
	if n := utf8.RuneCountInString(body); n < 1 || n > 4000 {
		return nil, httpx.ValidationError([]httpx.FieldError{{Field: "body", Code: "invalid", Message: "Write a note (up to 4,000 characters)."}})
	}
	n := &Note{Body: body, AuthorID: &p.UserID, AuthorName: p.Name()}
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, _, err := lock(ctx, tx, id); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO support_internal_notes (conversation_id, author_id, body) VALUES ($1, $2, $3) RETURNING id, created_at`,
			id, p.UserID, body).Scan(&n.ID, &n.CreatedAt); err != nil {
			return err
		}
		return notifyStaff(ctx, tx, id)
	})
	return n, err
}

// notifyStaff signals a change only staff streams care about.
func notifyStaff(ctx context.Context, q db.Querier, id string) error {
	return notify(ctx, q, Event{Kind: "status", Conversation: id, Actor: "agent"})
}

func (s *Service) staffWithPermission(ctx context.Context, q db.Querier, userID string, perm identity.Permission) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users u JOIN user_roles ur ON ur.user_id = u.id AND ur.scope_type IS NULL
		JOIN role_permissions rp ON rp.role_key = ur.role_key WHERE u.id = $1 AND u.status = 'active' AND rp.permission_key = $2)`,
		userID, string(perm)).Scan(&ok)
	return ok, err
}

// AssignInput assigns (or with an empty assignee, unassigns) a conversation.
type AssignInput struct {
	AssigneeID string `json:"assignee_id"`
	Team       string `json:"team"`
	Note       string `json:"note"`
}

// Assign sets the assignee. transfer marks a hand-over between agents
// (support_chat.transfer); the route decides which permission applies.
func (s *Service) Assign(ctx context.Context, p *identity.Principal, id string, in AssignInput, transfer bool) (*Detail, error) {
	in.Team = strings.TrimSpace(in.Team)
	if utf8.RuneCountInString(in.Team) > 60 || utf8.RuneCountInString(in.Note) > 1000 {
		return nil, httpx.ValidationError([]httpx.FieldError{{Field: "team", Code: "invalid", Message: "Keep the team under 60 characters and the note under 1,000."}})
	}
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		_, current, err := lock(ctx, tx, id)
		if err != nil {
			return err
		}
		var to *string
		if in.AssigneeID != "" {
			if !validUUID(in.AssigneeID) {
				return httpx.ValidationError([]httpx.FieldError{{Field: "assignee_id", Code: "invalid", Message: "Choose a support agent."}})
			}
			ok, err := s.staffWithPermission(ctx, tx, in.AssigneeID, identity.PermSupportChatReply)
			if err != nil {
				return err
			}
			if !ok {
				return httpx.ValidationError([]httpx.FieldError{{Field: "assignee_id", Code: "not_agent", Message: "That person cannot reply to chats."}})
			}
			to = &in.AssigneeID
		}
		if transfer && (current == nil || (to != nil && *current == *to)) && in.Team == "" {
			return httpx.NewError(http.StatusConflict, "nothing_to_transfer", "Transfer moves an assigned conversation to someone else or to a team.")
		}
		var team *string
		if in.Team != "" {
			team = &in.Team
		}
		if _, err := tx.Exec(ctx, `UPDATE support_conversations SET assignee_id = $2, team = coalesce($3, team), updated_at = now() WHERE id = $1`,
			id, to, team); err != nil {
			return err
		}
		typ := "assigned"
		switch {
		case transfer:
			typ = "transferred"
		case to == nil:
			typ = "unassigned"
		}
		detail := map[string]any{"from": current, "to": to, "note": strings.TrimSpace(in.Note)}
		if team != nil {
			detail["team"] = *team
		}
		if err := addEvent(ctx, tx, id, typ, detail, p.UserID); err != nil {
			return err
		}
		if to != nil {
			if _, err := tx.Exec(ctx, `UPDATE admin_alerts SET resolved_at = now() WHERE dedupe_key = $1 AND resolved_at IS NULL`,
				"support_chat.unassigned:"+id); err != nil {
				return err
			}
		}
		if err := audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: "support_chat." + typ, TargetType: "support_conversation",
			TargetID: id, Metadata: detail}); err != nil {
			return err
		}
		return notifyStaff(ctx, tx, id)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, p, id)
}

// SetStatus resolves or reopens a conversation.
func (s *Service) SetStatus(ctx context.Context, p *identity.Principal, id string, resolve bool, note string) (*Detail, error) {
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		status, _, err := lock(ctx, tx, id)
		if err != nil {
			return err
		}
		switch {
		case resolve && status == "resolved", !resolve && status != "resolved":
			return nil
		case resolve:
			_, err = tx.Exec(ctx, `UPDATE support_conversations SET status = 'resolved', resolved_at = now(), updated_at = now() WHERE id = $1`, id)
		default:
			_, err = tx.Exec(ctx, `UPDATE support_conversations SET status = 'open', resolved_at = NULL, updated_at = now() WHERE id = $1`, id)
		}
		if err != nil {
			return err
		}
		typ := map[bool]string{true: "resolved", false: "reopened"}[resolve]
		if err := addEvent(ctx, tx, id, typ, map[string]any{"note": strings.TrimSpace(note)}, p.UserID); err != nil {
			return err
		}
		// The customer sees the status change (and may rate the conversation once resolved).
		return notify(ctx, tx, Event{Kind: "status", Conversation: id, Actor: "agent"})
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, p, id)
}

// UpdateInput changes priority, tags and the order or subscription the
// conversation is about.
type UpdateInput struct {
	Priority       *string   `json:"priority"`
	Tags           *[]string `json:"tags"`
	OrderID        *string   `json:"order_id"`
	SubscriptionID *string   `json:"subscription_id"`
}

var tagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9 _-]{0,29}$`)

func (s *Service) Update(ctx context.Context, p *identity.Principal, id string, in UpdateInput) (*Detail, error) {
	var f []httpx.FieldError
	if in.Priority != nil && !slices.Contains(Priorities, *in.Priority) {
		f = append(f, httpx.FieldError{Field: "priority", Code: "invalid", Message: "Choose a priority."})
	}
	var tags []string
	if in.Tags != nil {
		for _, t := range *in.Tags {
			t = strings.ToLower(strings.TrimSpace(t))
			if t == "" {
				continue
			}
			if !tagPattern.MatchString(t) {
				f = append(f, httpx.FieldError{Field: "tags", Code: "invalid", Message: "Tags are up to 30 lowercase letters, digits, spaces, - or _."})
				break
			}
			if !slices.Contains(tags, t) {
				tags = append(tags, t)
			}
		}
		if len(tags) > 20 {
			f = append(f, httpx.FieldError{Field: "tags", Code: "invalid", Message: "At most 20 tags."})
		}
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, _, err := lock(ctx, tx, id); err != nil {
			return err
		}
		var customerID *string
		if err := tx.QueryRow(ctx, `SELECT customer_id FROM support_conversations WHERE id = $1`, id).Scan(&customerID); err != nil {
			return err
		}
		if in.Priority != nil {
			if _, err := tx.Exec(ctx, `UPDATE support_conversations SET priority = $2, updated_at = now() WHERE id = $1`, id, *in.Priority); err != nil {
				return err
			}
			if err := addEvent(ctx, tx, id, "priority", map[string]any{"to": *in.Priority}, p.UserID); err != nil {
				return err
			}
		}
		if in.Tags != nil {
			if tags == nil {
				tags = []string{}
			}
			if _, err := tx.Exec(ctx, `UPDATE support_conversations SET tags = $2, updated_at = now() WHERE id = $1`, id, tags); err != nil {
				return err
			}
			if err := addEvent(ctx, tx, id, "tags", map[string]any{"to": tags}, p.UserID); err != nil {
				return err
			}
		}
		// Links must belong to the conversation's customer: a guest conversation cannot point at an account's order.
		link := func(field, table, value string) error {
			var v *string
			if value != "" {
				if customerID == nil || !validUUID(value) {
					return httpx.ValidationError([]httpx.FieldError{{Field: field, Code: "invalid", Message: "Link only this customer's own records."}})
				}
				var ok bool
				if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM `+table+` WHERE id = $1 AND customer_id = $2)`, value, *customerID).Scan(&ok); err != nil {
					return err
				}
				if !ok {
					return httpx.ValidationError([]httpx.FieldError{{Field: field, Code: "invalid", Message: "Link only this customer's own records."}})
				}
				v = &value
			}
			if _, err := tx.Exec(ctx, `UPDATE support_conversations SET `+field+` = $2, updated_at = now() WHERE id = $1`, id, v); err != nil {
				return err
			}
			return addEvent(ctx, tx, id, "linked", map[string]any{field: v}, p.UserID)
		}
		if in.OrderID != nil {
			if err := link("order_id", "orders", *in.OrderID); err != nil {
				return err
			}
		}
		if in.SubscriptionID != nil {
			if err := link("subscription_id", "subscriptions", *in.SubscriptionID); err != nil {
				return err
			}
		}
		return notifyStaff(ctx, tx, id)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, p, id)
}

// EscalateInput turns a conversation into a support case.
type EscalateInput struct {
	Category string `json:"category"`
	Subject  string `json:"subject"`
	Note     string `json:"note"`
}

// Escalate opens a support case for the conversation (once) and links it.
func (s *Service) Escalate(ctx context.Context, p *identity.Principal, id string, in EscalateInput) (*Detail, error) {
	it, err := s.item(ctx, s.pool, p.UserID, id)
	if err != nil {
		return nil, err
	}
	if it.CaseID != nil {
		return nil, httpx.NewError(http.StatusConflict, "already_escalated", "This conversation already has a support case.")
	}
	subject := strings.TrimSpace(in.Subject)
	if subject == "" {
		subject = "Chat " + itoa(it.Number)
		if it.Subject != "" {
			subject += ": " + it.Subject
		}
		if utf8.RuneCountInString(subject) > 160 {
			subject = string([]rune(subject)[:160])
		}
	}
	note := strings.TrimSpace(in.Note)
	if it.OwnerKind == "guest" {
		note = strings.TrimSpace("From a guest chat (" + it.OwnerName + "). " + note)
	}
	cin := support.CreateInput{Subject: subject, Category: in.Category, Priority: it.Priority, Note: note}
	if it.CustomerID != nil {
		cin.CustomerID = *it.CustomerID
	}
	if it.AssigneeID != nil {
		cin.AssignedTo = *it.AssigneeID
	}
	c, err := s.Cases.Create(ctx, p.UserID, cin)
	if err != nil {
		return nil, err
	}
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE support_conversations SET case_id = $2, updated_at = now() WHERE id = $1 AND case_id IS NULL`, id, c.ID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.NewError(http.StatusConflict, "already_escalated", "This conversation already has a support case.")
		}
		if err := addEvent(ctx, tx, id, "escalated", map[string]any{"case_id": c.ID, "case_number": c.Number}, p.UserID); err != nil {
			return err
		}
		return notifyStaff(ctx, tx, id)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, p, id)
}

// AgentRead records the agent's read position.
func (s *Service) AgentRead(ctx context.Context, p *identity.Principal, id string, seq int64) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var last int64
		if !validUUID(id) {
			return httpx.ErrNotFound
		}
		err := tx.QueryRow(ctx, `SELECT last_seq FROM support_conversations WHERE id = $1`, id).Scan(&last)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := markRead(ctx, tx, id, "agent", p.UserID, min(seq, last)); err != nil {
			return err
		}
		return notify(ctx, tx, Event{Kind: "read", Conversation: id, Actor: "agent"})
	})
}

// AgentTyping announces that an agent is typing (never stored, throttled).
func (s *Service) AgentTyping(ctx context.Context, p *identity.Principal, id string) error {
	if !validUUID(id) {
		return httpx.ErrNotFound
	}
	if !s.typing.allow(id + "|agent:" + p.UserID) {
		return nil
	}
	return notify(ctx, s.pool, Event{Kind: "typing", Conversation: id, Actor: "agent"})
}

// Agent is someone conversations can be assigned to.
type Agent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Agents lists active staff who can reply to chats.
func (s *Service) Agents(ctx context.Context) ([]Agent, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT u.id, coalesce(nullif(trim(u.first_name || ' ' || u.last_name), ''), u.email)
		FROM users u JOIN user_roles ur ON ur.user_id = u.id AND ur.scope_type IS NULL JOIN role_permissions rp ON rp.role_key = ur.role_key
		WHERE u.status = 'active' AND rp.permission_key = 'support_chat.reply' ORDER BY 2`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Agent{}
	for rows.Next() {
		var a Agent
		if err := rows.Scan(&a.ID, &a.Name); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
