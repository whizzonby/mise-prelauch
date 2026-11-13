package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/identity/store"
	"mise.tt/api/internal/platform/db"
	platformemail "mise.tt/api/internal/platform/email"
)

// Operator actions on staff accounts, used by `misectl`. They run with
// database access on a trusted machine, so they are not permission-checked;
// each one is audited with "via": "misectl".

var errNoSuchUser = errors.New("no account with that email address")

func (s *Service) userByEmail(ctx context.Context, q *store.Queries, rawEmail string) (store.User, error) {
	_, canonical, ok := platformemail.Normalize(rawEmail)
	if !ok {
		return store.User{}, errors.New("a valid email address is required")
	}
	u, err := q.GetUserByCanonicalEmail(ctx, canonical)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.User{}, errNoSuchUser
	}
	return u, err
}

func operatorAudit(ctx context.Context, q db.Querier, action, userID string, meta map[string]any) error {
	if meta == nil {
		meta = map[string]any{}
	}
	meta["via"] = "misectl"
	return audit.Record(ctx, q, audit.Entry{Action: action, TargetType: "user", TargetID: userID, Metadata: meta})
}

// UpsertStaff creates a staff account, or updates the name and password of an
// existing one, re-enables it and grants role. It is how the first admin is made.
func (s *Service) UpsertStaff(ctx context.Context, rawEmail, name, role, password string) error {
	address, canonical, ok := platformemail.Normalize(rawEmail)
	if !ok {
		return errors.New("a valid email address is required")
	}
	name = strings.Join(strings.Fields(name), " ")
	if name == "" || len([]rune(name)) > 80 {
		return errors.New("a name of at most 80 characters is required")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		exists, err := q.RoleExists(ctx, role)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("unknown role %q", role)
		}
		u, err := q.GetUserByCanonicalEmail(ctx, canonical)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			now := s.now()
			// Staff are created by an operator who knows the address is theirs.
			u, err = q.CreateUser(ctx, store.CreateUserParams{Email: address, EmailCanonical: canonical, FirstName: name, EmailVerifiedAt: &now})
			if err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			if err := q.UpdateUserName(ctx, store.UpdateUserNameParams{ID: u.ID, FirstName: name, LastName: u.LastName}); err != nil {
				return err
			}
			if _, err := q.SetUserStatus(ctx, store.SetUserStatusParams{ID: u.ID, Status: "active"}); err != nil {
				return err
			}
			if _, err := q.MarkEmailVerified(ctx, u.ID); err != nil {
				return err
			}
			// A new password ends every existing session.
			if _, err := q.RevokeUserSessions(ctx, store.RevokeUserSessionsParams{UserID: u.ID, Reason: ptr("password_reset")}); err != nil {
				return err
			}
		}
		if err := q.SetPassword(ctx, store.SetPasswordParams{UserID: u.ID, PasswordHash: hash}); err != nil {
			return err
		}
		if err := q.GrantRole(ctx, store.GrantRoleParams{UserID: u.ID, RoleKey: role}); err != nil {
			return err
		}
		return operatorAudit(ctx, tx, "staff.upserted", u.ID, map[string]any{"role": role})
	})
}

// GrantRole gives an existing account a global role.
func (s *Service) GrantRole(ctx context.Context, rawEmail, role string) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		u, err := s.userByEmail(ctx, q, rawEmail)
		if err != nil {
			return err
		}
		exists, err := q.RoleExists(ctx, role)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("unknown role %q", role)
		}
		if err := q.GrantRole(ctx, store.GrantRoleParams{UserID: u.ID, RoleKey: role}); err != nil {
			return err
		}
		return operatorAudit(ctx, tx, "role.granted", u.ID, map[string]any{"role": role})
	})
}

// RevokeRole removes a global role. The change applies to live sessions at
// their next request, because access is checked on every request.
func (s *Service) RevokeRole(ctx context.Context, rawEmail, role string) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		u, err := s.userByEmail(ctx, q, rawEmail)
		if err != nil {
			return err
		}
		n, err := q.RevokeRole(ctx, store.RevokeRoleParams{UserID: u.ID, RoleKey: role})
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("%s does not hold the role %q", rawEmail, role)
		}
		return operatorAudit(ctx, tx, "role.revoked", u.ID, map[string]any{"role": role})
	})
}

// ResetMFA removes a lost authenticator and ends the account's sessions. The
// person enrols again at their next sign-in.
func (s *Service) ResetMFA(ctx context.Context, rawEmail string) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		u, err := s.userByEmail(ctx, q, rawEmail)
		if err != nil {
			return err
		}
		if _, err := q.DeleteTOTP(ctx, u.ID); err != nil {
			return err
		}
		if _, err := q.RevokeUserSessions(ctx, store.RevokeUserSessionsParams{UserID: u.ID, Reason: ptr("mfa_reset")}); err != nil {
			return err
		}
		return operatorAudit(ctx, tx, "mfa.reset", u.ID, nil)
	})
}

// SetDisabled disables (and signs out everywhere) or re-enables an account.
func (s *Service) SetDisabled(ctx context.Context, rawEmail string, disabled bool) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		u, err := s.userByEmail(ctx, q, rawEmail)
		if err != nil {
			return err
		}
		status, action := "active", "user.enabled"
		if disabled {
			status, action = "disabled", "user.disabled"
			if _, err := q.RevokeUserSessions(ctx, store.RevokeUserSessionsParams{UserID: u.ID, Reason: ptr("account_disabled")}); err != nil {
				return err
			}
		}
		if _, err := q.SetUserStatus(ctx, store.SetUserStatusParams{ID: u.ID, Status: status}); err != nil {
			return err
		}
		return operatorAudit(ctx, tx, action, u.ID, nil)
	})
}

// RevokeAllSessions signs an account out everywhere.
func (s *Service) RevokeAllSessions(ctx context.Context, rawEmail string) (int64, error) {
	var n int64
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		u, err := s.userByEmail(ctx, q, rawEmail)
		if err != nil {
			return err
		}
		if n, err = q.RevokeUserSessions(ctx, store.RevokeUserSessionsParams{UserID: u.ID, Reason: ptr("admin_revoked")}); err != nil {
			return err
		}
		return operatorAudit(ctx, tx, "sessions.revoked", u.ID, map[string]any{"count": n})
	})
	return n, err
}

// MissingPermissions returns permissions the code checks that are absent
// from the database. Used by tests and by `misectl doctor`.
func (s *Service) MissingPermissions(ctx context.Context) ([]Permission, error) {
	keys, err := store.New(s.pool).ListPermissionKeys(ctx)
	if err != nil {
		return nil, err
	}
	var missing []Permission
	for _, p := range AllPermissions {
		if !contains(keys, string(p)) {
			missing = append(missing, p)
		}
	}
	return missing, nil
}

// MarkEmailVerifiedByOperator confirms an account's email address without
// the emailed link. Only the local production demo seed uses it, for its own
// demo customers; it is audited like the other operator actions.
func (s *Service) MarkEmailVerifiedByOperator(ctx context.Context, rawEmail string) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		u, err := s.userByEmail(ctx, store.New(tx), rawEmail)
		if err != nil {
			return err
		}
		if err := s.markVerified(ctx, tx, u.ID, "account.email_verified"); err != nil {
			return err
		}
		return operatorAudit(ctx, tx, "account.email_verified_by_operator", u.ID, nil)
	})
}
