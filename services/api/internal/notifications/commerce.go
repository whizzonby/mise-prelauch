package notifications

import (
	"mise.tt/api/internal/appnotify"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"mise.tt/api/internal/commerce"
	"mise.tt/api/internal/customers"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/jobs"
	"mise.tt/api/internal/platform/mail"
)

// CommerceSender sends subscription, payment and meal-selection emails.
// Values were prepared when the email was queued (amounts, weeks, links);
// the recipient's name and address are looked up now, so a changed email
// address is respected and a disabled account gets nothing.
type CommerceSender struct {
	DB       db.Querier
	Identity *identity.Service
	Sender   *Sender
	// Inbox, when set, also records the event in the app's notification centre.
	Inbox *appnotify.Service
}

func (c *CommerceSender) Register(r *jobs.Runner) { r.Register(commerce.JobEmail, c.handle) }

type fact struct{ Label, Value string }

type commerceEmailData struct {
	FirstName   string
	Subject     string
	SiteURL     string
	Heading     string
	Paragraphs  []string
	Facts       []fact
	ButtonURL   string
	ButtonLabel string
}

// compose writes the email for a kind. Wording follows the brief: plain,
// specific about money and dates, and never promising what is not decided.
func compose(kind string, d map[string]string) (commerceEmailData, bool) {
	e := commerceEmailData{ButtonURL: d["link"]}
	switch kind {
	case commerce.EmailActivated:
		e.Subject, e.Heading = "Your Mise subscription has started", "You're subscribed"
		e.Paragraphs = []string{
			"Your first payment went through and your subscription is active.",
			"Your first delivery week is " + d["week"] + ". Choose your meals by " + d["cutoff"] + " (Trinidad and Tobago time).",
		}
		e.ButtonLabel = "Choose my meals"
	case commerce.EmailReceipt:
		e.Subject, e.Heading = "Your Mise receipt", "Payment received"
		e.Paragraphs = []string{"Thank you. We received your payment for the week of " + d["week"] + "."}
		e.Facts = []fact{{"Amount", d["amount"]}}
		if d["invoice"] != "" {
			e.Facts = append(e.Facts, fact{"Invoice", d["invoice"]})
		}
		e.ButtonLabel = "View invoice"
	case commerce.EmailPaymentFailed:
		e.Subject, e.Heading = "Your Mise payment didn't go through", "Payment didn't go through"
		e.Paragraphs = []string{
			"We couldn't take the payment of " + d["amount"] + " for the week of " + d["week"] + ".",
			"You can pay from your account until " + d["cutoff"] + ". If the week isn't paid by then, it won't be delivered and you won't be charged for it.",
		}
		e.ButtonLabel = "Pay now"
	case commerce.EmailPaymentDue:
		e.Subject, e.Heading = "Your next Mise week is ready to pay", "Payment due"
		e.Paragraphs = []string{
			"The week of " + d["week"] + " is ready to pay. Pay by " + d["cutoff"] + " to have it delivered.",
		}
		e.Facts = []fact{{"Amount", d["amount"]}}
		if d["invoice"] != "" {
			e.Facts = append(e.Facts, fact{"Invoice", d["invoice"]})
		}
		e.ButtonLabel = "Pay now"
	case commerce.EmailMenuAvailable:
		e.Subject, e.Heading = "This week's Mise menu is out", "The menu is out"
		e.Paragraphs = []string{
			"The menu for the week of " + d["week"] + " is ready. Choose your meals by " + d["cutoff"] + ".",
		}
		e.ButtonLabel = "Choose my meals"
	case commerce.EmailSelectionRemind:
		e.Subject, e.Heading = "Choose your Mise meals", "A reminder to choose your meals"
		e.Paragraphs = []string{
			"Your meals for the week of " + d["week"] + " aren't confirmed yet. Selection closes " + d["cutoff"] + ".",
			"If nothing is confirmed by then, we'll contact you before anything is prepared.",
		}
		e.ButtonLabel = "Choose my meals"
	case commerce.EmailSelectionConfirm:
		e.Subject, e.Heading = "Your Mise meals are confirmed", "Meals confirmed"
		e.Paragraphs = []string{
			"Your " + d["meals"] + " meals for the week of " + d["week"] + " are confirmed.",
			"You can still change them until " + d["cutoff"] + ".",
		}
		e.ButtonLabel = "See my meals"
	case commerce.EmailPaused:
		e.Subject, e.Heading = "Your Mise subscription is paused", "Subscription paused"
		e.Paragraphs = []string{
			"There will be no deliveries or charges from " + d["from"] + " to " + d["until"] + ".",
			"Deliveries start again with the week of " + d["resume"] + ". You can resume earlier from your account.",
		}
		e.ButtonLabel = "Manage subscription"
	case commerce.EmailResumed:
		e.Subject, e.Heading = "Your Mise subscription is active again", "Welcome back"
		e.Paragraphs = []string{"Your subscription is active again. The next week you can choose meals for is " + d["week"] + "."}
		e.ButtonLabel = "Choose my meals"
	case commerce.EmailCancelled:
		e.Subject, e.Heading = "Your Mise subscription is cancelled", "Subscription cancelled"
		e.Paragraphs = []string{"Your subscription has been cancelled. There will be no deliveries or charges from the week of " + d["week"] + "."}
		if d["paid_weeks"] != "" && d["paid_weeks"] != "0" {
			e.Paragraphs = append(e.Paragraphs, "Weeks you have already paid for are still delivered.")
		}
		e.Paragraphs = append(e.Paragraphs, "Your orders and invoices stay in your account.")
		e.ButtonLabel = "See plans"
	case commerce.EmailPlanChanged:
		e.Subject, e.Heading = "Your Mise plan has changed", "Plan changed"
		e.Paragraphs = []string{"From the week of " + d["week"] + ", you're on the " + d["plan"] + " plan: " + d["meals"] + " meals a week for " + d["amount"] + ". Weeks already paid keep their plan."}
		e.ButtonLabel = "Manage subscription"
	default:
		return e, false
	}
	return e, true
}

func (c *CommerceSender) handle(ctx context.Context, payload json.RawMessage) error {
	var job commerce.EmailJob
	if err := json.Unmarshal(payload, &job); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	data, ok := compose(job.Kind, job.Data)
	if !ok {
		slog.WarnContext(ctx, "unknown commerce email", "kind", job.Kind)
		return nil
	}
	uid, err := customers.UserIDOf(ctx, c.DB, job.CustomerID)
	if err != nil {
		return err
	}
	who, err := c.Identity.ContactOf(ctx, uid)
	if err != nil {
		return err
	}
	if !who.Active {
		slog.InfoContext(ctx, "email skipped", "template", "commerce", "kind", job.Kind)
		return nil
	}
	if note, ok := commerceNote(job.Kind, job.Data, data); ok {
		if err := record(ctx, c.Inbox, c.DB, uid, note); err != nil {
			return err
		}
	}
	data.FirstName, data.SiteURL = who.FirstName, c.Sender.SiteURL
	var html, text bytes.Buffer
	if err := htmlTemplates.ExecuteTemplate(&html, "commerce.html", data); err != nil {
		return fmt.Errorf("render commerce.html: %w", err)
	}
	if err := textTemplates.ExecuteTemplate(&text, "commerce.txt", data); err != nil {
		return fmt.Errorf("render commerce.txt: %w", err)
	}
	msg := mail.Message{To: who.Email, Subject: data.Subject, HTML: html.String(), Text: text.String()}
	if err := c.Sender.Mailer.Send(ctx, msg); err != nil {
		return err
	}
	slog.InfoContext(ctx, "email sent", "event", "email.sent", "template", "commerce", "kind", job.Kind)
	return nil
}
