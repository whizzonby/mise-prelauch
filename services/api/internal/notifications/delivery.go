package notifications

import (
	"strings"
	"mise.tt/api/internal/appnotify"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/customers"
	"mise.tt/api/internal/delivery"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/jobs"
	"mise.tt/api/internal/platform/mail"
)

// DeliverySender sends customer delivery emails and records each outcome
// on its delivery_notifications row: sent only when the mail provider
// accepted it, skipped (with the reason) when the wording is not yet
// approved or the customer turned delivery updates off, failed otherwise.
type DeliverySender struct {
	DB       *pgxpool.Pool
	Identity *identity.Service
	Sender   *Sender
	AppURL   string
	// Inbox, when set, also records the update in the app's notification centre.
	Inbox *appnotify.Service
}

func (d *DeliverySender) Register(r *jobs.Runner) { r.Register(delivery.JobEmail, d.handle) }

func deliveryCompose(f *delivery.EmailFacts) commerceEmailData {
	e := commerceEmailData{}
	when := ""
	if f.ServiceDate != nil {
		when = *f.ServiceDate
		if f.Window != nil {
			when += ", " + *f.Window
		}
	}
	switch f.Kind {
	case "scheduled":
		e.Subject, e.Heading = "Your Mise delivery is scheduled", "Delivery scheduled"
		e.Paragraphs = []string{"Your order " + f.OrderNumber + " is packed and scheduled for delivery."}
	case "rescheduled":
		e.Subject, e.Heading = "Your Mise delivery date has changed", "New delivery date"
		e.Paragraphs = []string{"We've moved the delivery of order " + f.OrderNumber + " as agreed."}
	case "dispatched":
		e.Subject, e.Heading = "Your Mise order is on its way", "Out for delivery"
		e.Paragraphs = []string{"Order " + f.OrderNumber + " is with our delivery team. Your delivery code is in your account if you're asked for it."}
	case "delivered":
		e.Subject, e.Heading = "Your Mise order was delivered", "Delivered"
		e.Paragraphs = []string{"Order " + f.OrderNumber + " was delivered. If anything isn't right, reply from your account."}
	case "failed":
		e.Subject, e.Heading = "We couldn't deliver your Mise order", "Delivery unsuccessful"
		e.Paragraphs = []string{"We weren't able to complete the delivery of order " + f.OrderNumber + ". Our team will contact you about the next step."}
	case "delayed":
		e.Subject, e.Heading = "Your Mise delivery is delayed", "Delivery delayed"
		e.Paragraphs = []string{"The delivery of order " + f.OrderNumber + " is running late. We'll update you in your account."}
	}
	if when != "" && f.Kind != "delivered" && f.Kind != "failed" {
		e.Facts = []fact{{"Delivery", when}}
	}
	e.ButtonLabel = "View delivery"
	return e
}

func (d *DeliverySender) handle(ctx context.Context, payload json.RawMessage) error {
	f, err := delivery.EmailFor(ctx, d.DB, payload)
	if err != nil {
		return err
	}
	if f.Status != "queued" && f.Status != "failed" {
		return nil
	}
	if !f.Approved {
		return delivery.MarkEmail(ctx, d.DB, f.ID, "skipped", "Delivery email wording is not approved yet; nothing was sent.")
	}
	if !f.OptedIn {
		return delivery.MarkEmail(ctx, d.DB, f.ID, "skipped", "The customer turned delivery updates off.")
	}
	uid, err := customers.UserIDOf(ctx, d.DB, f.CustomerID)
	if err != nil {
		return err
	}
	who, err := d.Identity.ContactOf(ctx, uid)
	if err != nil {
		return err
	}
	if !who.Active {
		return delivery.MarkEmail(ctx, d.DB, f.ID, "skipped", "The customer's account is not active.")
	}
	data := deliveryCompose(f)
	typ := "delivery.update"
	if f.Kind == "delivered" {
		typ = "delivery.delivered"
	}
	if err := record(ctx, d.Inbox, d.DB, uid, appnotify.Note{
		Type: typ, Title: data.Heading, Body: strings.Join(data.Paragraphs, " "), Link: "/orders/" + f.OrderID + "/delivery",
		PushTitle: "Mise", PushBody: pushDelivery, DedupKey: "delivery:" + f.ID,
	}); err != nil {
		return err
	}
	data.FirstName, data.SiteURL = who.FirstName, d.Sender.SiteURL
	data.ButtonURL = fmt.Sprintf("%s/orders/%s", d.AppURL, f.OrderID)
	var html, text bytes.Buffer
	if err := htmlTemplates.ExecuteTemplate(&html, "commerce.html", data); err != nil {
		return fmt.Errorf("render commerce.html: %w", err)
	}
	if err := textTemplates.ExecuteTemplate(&text, "commerce.txt", data); err != nil {
		return fmt.Errorf("render commerce.txt: %w", err)
	}
	if err := d.Sender.Mailer.Send(ctx, mail.Message{To: who.Email, Subject: data.Subject, HTML: html.String(), Text: text.String()}); err != nil {
		_ = delivery.MarkEmail(ctx, d.DB, f.ID, "failed", err.Error())
		return err
	}
	slog.InfoContext(ctx, "email sent", "event", "email.sent", "template", "delivery", "kind", f.Kind)
	return delivery.MarkEmail(ctx, d.DB, f.ID, "sent", "")
}
