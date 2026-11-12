package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/jobs"
)

// Accounts prepares account emails: it creates any single-use token at send
// time, so the token never waits in the jobs table. *identity.Service
// implements it.
type Accounts interface {
	PrepareAccountEmail(ctx context.Context, kind string, job identity.AccountJob) (*identity.AccountEmail, error)
}

type AccountSender struct {
	Accounts Accounts
	Sender   *Sender
}

type accountSpec struct {
	template string
	subject  string
}

var accountSpecs = map[string]accountSpec{
	identity.JobVerifyEmail:       {"account_verify", "Confirm your email for your Mise account"},
	identity.JobPasswordReset:     {"account_password_reset", "Reset your Mise password"},
	identity.JobAlreadyRegistered: {"account_exists", "You already have a Mise account"},
	identity.JobEmailChange:       {"account_email_change", "Confirm your new email address for Mise"},
	identity.JobEmailChangeNotice: {"account_email_change_notice", "Your Mise email address is being changed"},
	identity.JobEmailInUse:        {"account_email_in_use", "This address already has a Mise account"},
	identity.JobPasswordChanged:   {"account_password_changed", "Your Mise password was changed"},
}

// Register wires the account email job kinds to their handler.
func (a *AccountSender) Register(r *jobs.Runner) {
	for kind := range accountSpecs {
		r.Register(kind, a.handle(kind))
	}
}

type accountEmailData struct {
	FirstName string
	Subject   string
	SiteURL   string
	ActionURL string
	LinkTTL   string
	NewEmail  string
}

func (a *AccountSender) handle(kind string) jobs.Handler {
	spec := accountSpecs[kind]
	return func(ctx context.Context, payload json.RawMessage) error {
		var job identity.AccountJob
		if err := json.Unmarshal(payload, &job); err != nil {
			return fmt.Errorf("decode payload: %w", err)
		}
		email, err := a.Accounts.PrepareAccountEmail(ctx, kind, job)
		if err != nil {
			return err
		}
		if email == nil {
			slog.InfoContext(ctx, "email skipped", "template", spec.template)
			return nil
		}
		data := accountEmailData{
			FirstName: email.FirstName,
			Subject:   spec.subject,
			SiteURL:   a.Sender.SiteURL,
			ActionURL: email.ActionURL,
			LinkTTL:   describeTTL(email.LinkTTL),
			NewEmail:  email.NewEmail,
		}
		msg, err := renderAccount(spec.template, data)
		if err != nil {
			return err
		}
		msg.To = email.To
		if err := a.Sender.Mailer.Send(ctx, msg); err != nil {
			return err
		}
		slog.InfoContext(ctx, "email sent", "event", "email.sent", "template", spec.template)
		return nil
	}
}

func describeTTL(d time.Duration) string {
	switch {
	case d <= 0:
		return ""
	case d < 2*time.Hour:
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	default:
		return fmt.Sprintf("%d hours", int(d.Hours()))
	}
}

// StaffInviteSender emails staff invitations. The link token is made when
// the email is sent (identity.PrepareInvitationEmail).
type StaffInviteSender struct {
	Staff interface {
		PrepareInvitationEmail(ctx context.Context, invitationID string) (*identity.AccountEmail, error)
	}
	Sender *Sender
}

func (s *StaffInviteSender) Register(r *jobs.Runner) { r.Register(identity.JobStaffInvite, s.handle) }

func (s *StaffInviteSender) handle(ctx context.Context, payload json.RawMessage) error {
	var job struct {
		InvitationID string `json:"invitation_id"`
	}
	if err := json.Unmarshal(payload, &job); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	email, err := s.Staff.PrepareInvitationEmail(ctx, job.InvitationID)
	if err != nil {
		return err
	}
	if email == nil {
		slog.InfoContext(ctx, "email skipped", "template", "staff_invite")
		return nil
	}
	msg, err := renderAccount("staff_invite", accountEmailData{FirstName: email.FirstName, Subject: "You're invited to the Mise console",
		SiteURL: s.Sender.SiteURL, ActionURL: email.ActionURL, LinkTTL: describeTTL(email.LinkTTL)})
	if err != nil {
		return err
	}
	msg.To = email.To
	if err := s.Sender.Mailer.Send(ctx, msg); err != nil {
		return err
	}
	slog.InfoContext(ctx, "email sent", "event", "email.sent", "template", "staff_invite")
	return nil
}
