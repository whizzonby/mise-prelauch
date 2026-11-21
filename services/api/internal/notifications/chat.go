package notifications

import (
	"mise.tt/api/internal/appnotify"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/customers"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/jobs"
	"mise.tt/api/internal/platform/mail"
	"mise.tt/api/internal/supportchat"
)

// ChatSender emails a customer or guest that the team replied in chat: at
// most one email per conversation per cool-down window (the row is unique
// per window), never the message text, and nothing if the reply was read
// before the email was due.
type ChatSender struct {
	DB       *pgxpool.Pool
	Identity *identity.Service
	Sender   *Sender
	AppURL   string
	// Inbox, when set, also records the reply in the app's notification centre.
	Inbox *appnotify.Service
}

func (c *ChatSender) Register(r *jobs.Runner) { r.Register(supportchat.JobEmail, c.handle) }

func (c *ChatSender) handle(ctx context.Context, payload json.RawMessage) error {
	f, err := supportchat.EmailFor(ctx, c.DB, payload)
	if err != nil {
		return err
	}
	if f.Status != "queued" && f.Status != "failed" {
		return nil
	}
	if f.ReadSeq >= f.Seq {
		return supportchat.MarkEmail(ctx, c.DB, f.NotificationID, "skipped", "The reply was read in chat before the email was due.")
	}
	data := commerceEmailData{SiteURL: c.Sender.SiteURL, Subject: "You have a reply from Mise", Heading: "New reply",
		ButtonLabel: "Read the reply"}
	var to string
	if f.OwnerKind == "customer" && f.CustomerID != nil {
		uid, err := customers.UserIDOf(ctx, c.DB, *f.CustomerID)
		if err != nil {
			return err
		}
		who, err := c.Identity.ContactOf(ctx, uid)
		if err != nil {
			return err
		}
		if !who.Active {
			return supportchat.MarkEmail(ctx, c.DB, f.NotificationID, "skipped", "The customer's account is not active.")
		}
		if err := record(ctx, c.Inbox, c.DB, uid, appnotify.Note{
			Type: "chat.reply", Title: "New reply from Mise", Body: fmt.Sprintf("Our team replied in conversation %d.", f.Number),
			Link: "/support/conversations/" + f.ConversationID, PushTitle: "Mise", PushBody: pushChat, DedupKey: fmt.Sprintf("chat:%08d", f.NotificationID),
		}); err != nil {
			return err
		}
		to, data.FirstName = who.Email, who.FirstName
		data.Paragraphs = []string{fmt.Sprintf("Our team replied to your conversation %d. Sign in to read it and reply.", f.Number)}
		data.ButtonURL = c.AppURL + "/support/chat/" + f.ConversationID
	} else {
		to, data.FirstName = f.GuestEmail, f.GuestName
		// The guest's chat lives in the browser that started it; the email never carries the token.
		data.Paragraphs = []string{fmt.Sprintf("Our team replied to your conversation %d. Open chat on the Mise website in the same browser you used to write to us.", f.Number)}
		data.ButtonURL = c.Sender.SiteURL + "/?chat=open"
	}
	var html, text bytes.Buffer
	if err := htmlTemplates.ExecuteTemplate(&html, "commerce.html", data); err != nil {
		return fmt.Errorf("render commerce.html: %w", err)
	}
	if err := textTemplates.ExecuteTemplate(&text, "commerce.txt", data); err != nil {
		return fmt.Errorf("render commerce.txt: %w", err)
	}
	if err := c.Sender.Mailer.Send(ctx, mail.Message{To: to, Subject: data.Subject, HTML: html.String(), Text: text.String()}); err != nil {
		_ = supportchat.MarkEmail(ctx, c.DB, f.NotificationID, "failed", err.Error())
		return err
	}
	slog.InfoContext(ctx, "email sent", "event", "email.sent", "template", "support_chat", "owner", f.OwnerKind)
	return supportchat.MarkEmail(ctx, c.DB, f.NotificationID, "sent", "")
}
