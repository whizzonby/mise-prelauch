package identity

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/identity/store"
	"mise.tt/api/internal/platform/db"
	platformemail "mise.tt/api/internal/platform/email"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/jobs"
)

// Account settings a signed-in customer changes themselves (Sprint 2): their
// name, password and email address.

const (
	purposeChangeEmail = "change_email"
	changeEmailTTL     = 24 * time.Hour
)

var (
	errWrongPassword = httpx.ValidationError([]httpx.FieldError{{
		Field: "current_password", Code: "incorrect", Message: "That is not your current password.",
	}})
	errEmailTaken = httpx.NewError(http.StatusConflict, "email_taken",
		"That address now belongs to another Mise account, so the change was not made.")
)

// SetName validates and stores a person's name, inside the caller's
// transaction. The customers module calls it when a profile is updated.
func SetName(ctx context.Context, q db.Querier, userID, rawFirst, rawLast string) error {
	var fields []httpx.FieldError
	first, ok := cleanName(rawFirst)
	if !ok {
		fields = append(fields, httpx.FieldError{Field: "first_name", Code: "invalid", Message: "Enter your first name."})
	}
	last, ok := cleanName(rawLast)
	if !ok {
		fields = append(fields, httpx.FieldError{Field: "last_name", Code: "invalid", Message: "Enter your last name."})
	}
	if len(fields) > 0 {
		return httpx.ValidationError(fields)
	}
	return store.New(q).UpdateUserName(ctx, store.UpdateUserNameParams{ID: userID, FirstName: first, LastName: last})
}

// PendingEmailChange returns the address an open change link would switch to,
// or "" when there is none.
func (s *Service) PendingEmailChange(ctx context.Context, q db.Querier, userID string) (string, error) {
	addr, err := store.New(q).PendingEmailChange(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return addr, err
}

func (s *Service) checkPassword(ctx context.Context, q *store.Queries, userID, password string) error {
	hash, err := q.GetPasswordHash(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return errWrongPassword
	}
	if err != nil {
		return err
	}
	if !VerifyPassword(hash, password) {
		return errWrongPassword
	}
	return nil
}

// ChangePassword replaces the password of a signed-in customer who knows the
// current one. Every other session ends; this one stays. The account's address
// gets a notice, so a change made by someone else does not go unnoticed.
func (s *Service) ChangePassword(ctx context.Context, p *Principal, current, next string) error {
	if problem := CheckPassword(next); problem != "" {
		return httpx.ValidationError([]httpx.FieldError{{Field: "new_password", Code: "weak", Message: problem}})
	}
	q := store.New(s.pool)
	if err := s.checkPassword(ctx, q, p.UserID, current); err != nil {
		return err
	}
	if current == next {
		return httpx.ValidationError([]httpx.FieldError{{Field: "new_password", Code: "unchanged", Message: "Choose a password different from your current one."}})
	}
	hash, err := HashPassword(next)
	if err != nil {
		return err
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		if err := q.SetPassword(ctx, store.SetPasswordParams{UserID: p.UserID, PasswordHash: hash}); err != nil {
			return err
		}
		if err := q.InvalidateAuthTokens(ctx, store.InvalidateAuthTokensParams{UserID: p.UserID, Purpose: purposeResetPassword}); err != nil {
			return err
		}
		keep := p.SessionID
		if _, err := q.RevokeUserSessions(ctx, store.RevokeUserSessionsParams{UserID: p.UserID, Reason: ptr("password_changed"), KeepID: &keep}); err != nil {
			return err
		}
		if err := jobs.Enqueue(ctx, tx, JobPasswordChanged, AccountJob{UserID: p.UserID}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: "account.password_changed", TargetType: "user", TargetID: p.UserID})
	})
}

// RequestEmailChange starts moving an account to a new address. Nothing
// changes until a link sent to the new address is followed. The answer is the
// same whether or not the new address already has an account: that address is
// told so by email instead, and the account holder is not.
func (s *Service) RequestEmailChange(ctx context.Context, p *Principal, rawEmail, password string) error {
	address, canonical, ok := platformemail.Normalize(rawEmail)
	if !ok {
		return httpx.ValidationError([]httpx.FieldError{{Field: "new_email", Code: "invalid", Message: "Enter a valid email address."}})
	}
	q := store.New(s.pool)
	if err := s.checkPassword(ctx, q, p.UserID, password); err != nil {
		return err
	}
	me, err := q.GetUserByID(ctx, p.UserID)
	if err != nil {
		return err
	}
	if me.EmailCanonical == canonical {
		return httpx.ValidationError([]httpx.FieldError{{Field: "new_email", Code: "unchanged", Message: "That is already your email address."}})
	}
	other, err := q.GetUserByCanonicalEmail(ctx, canonical)
	taken := err == nil && other.ID != p.UserID
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		job := AccountJob{UserID: p.UserID, NewEmail: address}
		if taken {
			if err := jobs.Enqueue(ctx, tx, JobEmailInUse, job); err != nil {
				return err
			}
		} else {
			if err := jobs.Enqueue(ctx, tx, JobEmailChange, job); err != nil {
				return err
			}
			if err := jobs.Enqueue(ctx, tx, JobEmailChangeNotice, job); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: "account.email_change_requested", TargetType: "user", TargetID: p.UserID})
	})
}

// ConfirmEmailChange follows a change link: the account moves to the new
// address, which counts as verified. If no waitlist lead was linked yet and
// the new address has one, it is linked now, as at first verification.
func (s *Service) ConfirmEmailChange(ctx context.Context, raw string) error {
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		row, err := q.ConsumeEmailChangeToken(ctx, hashToken(raw))
		if errors.Is(err, pgx.ErrNoRows) {
			return errTokenInvalid
		}
		if err != nil {
			return err
		}
		if row.NewEmail == nil {
			return errTokenInvalid
		}
		address, canonical, ok := platformemail.Normalize(*row.NewEmail)
		if !ok {
			return errTokenInvalid
		}
		u, err := q.GetUserByID(ctx, row.UserID)
		if err != nil {
			return err
		}
		if u.Status != "active" {
			return errTokenInvalid
		}
		other, err := q.GetUserByCanonicalEmail(ctx, canonical)
		if err == nil && other.ID != u.ID {
			return errEmailTaken
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err := q.SetUserEmail(ctx, store.SetUserEmailParams{ID: u.ID, Email: address, EmailCanonical: canonical}); err != nil {
			return err
		}
		if err := q.InvalidateAuthTokens(ctx, store.InvalidateAuthTokensParams{UserID: u.ID, Purpose: purposeChangeEmail}); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, audit.Entry{ActorUserID: u.ID, Action: "account.email_changed", TargetType: "user", TargetID: u.ID}); err != nil {
			return err
		}
		return s.hooks.EmailVerified(ctx, tx, u.ID, canonical)
	})
	if db.IsUniqueViolation(err, "users_email_canonical_key") {
		return errEmailTaken
	}
	return err
}

func (s *Service) issueEmailChangeToken(ctx context.Context, q *store.Queries, userID, newEmail string) (string, error) {
	if err := q.InvalidateAuthTokens(ctx, store.InvalidateAuthTokensParams{UserID: userID, Purpose: purposeChangeEmail}); err != nil {
		return "", err
	}
	raw, hashed, err := newToken()
	if err != nil {
		return "", err
	}
	err = q.CreateEmailChangeToken(ctx, store.CreateEmailChangeTokenParams{
		UserID: userID, TokenHash: hashed, ExpiresAt: s.now().Add(changeEmailTTL), NewEmail: &newEmail,
	})
	return raw, err
}

// MaskEmail shortens an address for a notice sent elsewhere: "k•••@example.com".
func MaskEmail(address string) string {
	local, domain, ok := strings.Cut(address, "@")
	if !ok || local == "" {
		return "a new address"
	}
	return string([]rune(local)[0]) + "•••@" + domain
}
