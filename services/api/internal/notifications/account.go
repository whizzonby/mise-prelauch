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
	PrepareAccountEmail(ctx context.Context, kind, userID string) (*identity.AccountEmail, error)
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
}

func (a *AccountSender) handle(kind string) jobs.Handler {
	spec := accountSpecs[kind]
	return func(ctx context.Context, payload json.RawMessage) error {
		var job identity.AccountJob
		if err := json.Unmarshal(payload, &job); err != nil {
			return fmt.Errorf("decode payload: %w", err)
		}
		email, err := a.Accounts.PrepareAccountEmail(ctx, kind, job.UserID)
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
