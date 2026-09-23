// Package notifications renders and sends transactional email. Handlers are
// registered with the job runner; nothing here is called from an HTTP request.
package notifications

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	htmltemplate "html/template"
	"log/slog"
	texttemplate "text/template"

	"mise.tt/api/internal/leads"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/jobs"
	"mise.tt/api/internal/platform/mail"
	"mise.tt/api/internal/referrals"
)

//go:embed templates/*
var templateFS embed.FS

var (
	htmlTemplates = htmltemplate.Must(htmltemplate.New("").Funcs(htmltemplate.FuncMap{"dict": dict}).ParseFS(templateFS, "templates/*.html"))
	textTemplates = texttemplate.Must(texttemplate.ParseFS(templateFS, "templates/*.txt"))
)

// dict lets a template pass named arguments to a partial.
func dict(pairs ...any) (map[string]any, error) {
	if len(pairs)%2 != 0 {
		return nil, fmt.Errorf("dict needs key/value pairs")
	}
	m := make(map[string]any, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		key, ok := pairs[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict keys must be strings")
		}
		m[key] = pairs[i+1]
	}
	return m, nil
}

// Links builds the URLs that appear in emails.
type Links interface {
	ReferralURL(code string) string
	VerifyURL(leadID string) string
	UnsubscribeURL(leadID string) string
	WelcomeURL(leadID string) string
}

type Sender struct {
	DB     db.Querier
	Mailer mail.Mailer
	Links  Links
	// SiteURL is the marketing site origin, used for the logo link and footer.
	SiteURL string
}

// Register wires every email job kind to its handler.
func (s *Sender) Register(r *jobs.Runner) {
	r.Register(leads.JobEmailVerify, s.handle(emailSpec{
		template: "verify",
		subject:  func(emailData) string { return "Confirm your place on the Mise list" },
		// Verification must reach people who are still pending.
		allow: func(st leads.Status) bool { return st == leads.StatusPending },
	}))
	r.Register(leads.JobEmailWelcome, s.handle(emailSpec{
		template: "welcome",
		subject:  func(emailData) string { return "You're on the Mise list" },
		allow:    subscribed,
	}))
	r.Register(leads.JobEmailAlreadyOnList, s.handle(emailSpec{
		template: "already_on_list",
		subject:  func(emailData) string { return "You're already on the Mise list" },
		allow:    subscribed,
	}))
	r.Register(leads.JobEmailReferralMilestone, s.handle(emailSpec{
		template: "referral_milestone",
		subject: func(d emailData) string {
			return fmt.Sprintf("%d friends have joined Mise through you", d.Count)
		},
		allow: subscribed,
	}))
}

func subscribed(st leads.Status) bool {
	return st == leads.StatusVerified || st == leads.StatusQualified
}

type emailSpec struct {
	template string
	subject  func(emailData) string
	// allow decides, at send time, whether a lead in this status should still
	// get the email. Status can change between enqueue and send.
	allow func(leads.Status) bool
}

type emailData struct {
	FirstName      string
	Subject        string
	SiteURL        string
	VerifyURL      string
	ReferralURL    string
	WelcomeURL     string
	UnsubscribeURL string
	Count          int
	Referrals      referrals.Counts
}

func (s *Sender) handle(spec emailSpec) jobs.Handler {
	return func(ctx context.Context, payload json.RawMessage) error {
		var job leads.EmailJob
		if err := json.Unmarshal(payload, &job); err != nil {
			return fmt.Errorf("decode payload: %w", err)
		}
		lead, err := leads.GetByID(ctx, s.DB, job.LeadID)
		if err != nil {
			return err
		}
		// A lead that was erased, unsubscribed or blocked after the job was
		// queued gets nothing; the job is complete.
		if lead == nil || !spec.allow(lead.Status) {
			slog.InfoContext(ctx, "email skipped", "template", spec.template)
			return nil
		}
		counts, err := referrals.CountsFor(ctx, s.DB, lead.ID)
		if err != nil {
			return err
		}

		data := emailData{
			FirstName:      lead.FirstName,
			SiteURL:        s.SiteURL,
			VerifyURL:      s.Links.VerifyURL(lead.ID),
			ReferralURL:    s.Links.ReferralURL(lead.ReferralCode),
			WelcomeURL:     s.Links.WelcomeURL(lead.ID),
			UnsubscribeURL: s.Links.UnsubscribeURL(lead.ID),
			Count:          job.Count,
			Referrals:      counts,
		}
		data.Subject = spec.subject(data)

		msg, err := render(spec.template, data)
		if err != nil {
			return err
		}
		msg.To = lead.Email
		if err := s.Mailer.Send(ctx, msg); err != nil {
			return err
		}
		slog.InfoContext(ctx, "email sent", "event", "email.sent", "template", spec.template)
		return nil
	}
}

func render(name string, data emailData) (mail.Message, error) {
	var html, text bytes.Buffer
	if err := htmlTemplates.ExecuteTemplate(&html, name+".html", data); err != nil {
		return mail.Message{}, fmt.Errorf("render %s.html: %w", name, err)
	}
	if err := textTemplates.ExecuteTemplate(&text, name+".txt", data); err != nil {
		return mail.Message{}, fmt.Errorf("render %s.txt: %w", name, err)
	}
	return mail.Message{
		Subject: data.Subject,
		HTML:    html.String(),
		Text:    text.String(),
		Headers: map[string]string{
			"List-Unsubscribe": "<" + data.UnsubscribeURL + ">",
		},
	}, nil
}
