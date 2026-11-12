package identity

import (
	"context"
	"errors"
	"net/http"
	"slices"
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

// Staff administration (Sprint 4). The rules that stop privilege escalation:
//   - Only a super_admin grants or removes super_admin or admin, and only a
//     super_admin disables an account that holds either.
//   - Nobody changes their own roles, or disables or signs out themselves here.
//   - The last active super_admin cannot lose the role or be disabled.
//   - The customer role is never granted from the console.
// Every change is audited with the roles before and after.

// JobStaffInvite emails a staff invitation (the token is made at send time).
const JobStaffInvite = "staff.invite"

const invitationTTL = 7 * 24 * time.Hour

// PrivilegedRoles may only be granted, removed or disabled by a super_admin.
var PrivilegedRoles = []string{"super_admin", "admin"}

// StaffMember is a staff account as the console shows it.
type StaffMember struct {
	ID          string     `json:"id"`
	Email       string     `json:"email"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	Roles       []string   `json:"roles"`
	MFAEnrolled bool       `json:"mfa_enrolled"`
	LastLoginAt *time.Time `json:"last_login_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

func (s *Service) ListStaff(ctx context.Context) ([]StaffMember, error) {
	rows, err := store.New(s.pool).ListStaff(ctx)
	if err != nil {
		return nil, err
	}
	out := []StaffMember{}
	for _, r := range rows {
		out = append(out, StaffMember{ID: r.ID, Email: r.Email, Name: strings.TrimSpace(r.FirstName + " " + r.LastName), Status: r.Status,
			Roles: r.Roles, MFAEnrolled: r.MfaEnrolled, LastLoginAt: r.LastLoginAt, CreatedAt: r.CreatedAt})
	}
	return out, nil
}

// Role is a role and its permissions (read-only in the console).
type Role struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

type PermissionInfo struct {
	Key         string `json:"key"`
	Description string `json:"description"`
}

func (s *Service) Roles(ctx context.Context) ([]Role, []PermissionInfo, error) {
	rows, err := store.New(s.pool).ListRoles(ctx)
	if err != nil {
		return nil, nil, err
	}
	roles := []Role{}
	for _, r := range rows {
		roles = append(roles, Role{Key: r.Key, Name: r.Name, Description: r.Description, Permissions: r.Permissions})
	}
	perms, err := store.New(s.pool).ListPermissions(ctx)
	if err != nil {
		return nil, nil, err
	}
	out := []PermissionInfo{}
	for _, p := range perms {
		out = append(out, PermissionInfo{Key: p.Key, Description: p.Description})
	}
	return roles, out, nil
}

var (
	errSelf       = httpx.NewError(http.StatusForbidden, "not_on_yourself", "You can't change your own access. Ask another administrator.")
	errPrivileged = httpx.NewError(http.StatusForbidden, "super_admin_required", "Only a super administrator can do this for administrator roles.")
	errLastSuper  = httpx.NewError(http.StatusConflict, "last_super_admin", "This is the last active super administrator.")
)

func checkRole(ctx context.Context, q *store.Queries, role string) error {
	if role == RoleCustomer {
		return httpx.ValidationError([]httpx.FieldError{{Field: "role", Code: "invalid", Message: "Customer is not a staff role."}})
	}
	ok, err := q.RoleExists(ctx, role)
	if err != nil {
		return err
	}
	if !ok {
		return httpx.ValidationError([]httpx.FieldError{{Field: "role", Code: "invalid", Message: "Choose a role from the list."}})
	}
	return nil
}

func (s *Service) staffTarget(ctx context.Context, q *store.Queries, actor *Principal, userID string) (store.User, []string, error) {
	if actor.UserID == userID {
		return store.User{}, nil, errSelf
	}
	u, err := q.GetUserByID(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.User{}, nil, httpx.ErrNotFound
	}
	if err != nil {
		return store.User{}, nil, err
	}
	staff, err := q.IsStaff(ctx, userID)
	if err != nil {
		return store.User{}, nil, err
	}
	if !staff {
		return store.User{}, nil, httpx.ErrNotFound
	}
	roles, err := q.UserRoles(ctx, userID)
	return u, roles, err
}

func privileged(roles ...string) bool {
	for _, r := range roles {
		if slices.Contains(PrivilegedRoles, r) {
			return true
		}
	}
	return false
}

func (s *Service) lastSuperAdmin(ctx context.Context, q *store.Queries, roles []string) (bool, error) {
	if !slices.Contains(roles, "super_admin") {
		return false, nil
	}
	n, err := q.CountActiveWithRole(ctx, "super_admin")
	return n <= 1, err
}

// GrantStaffRole gives a staff member another role.
func (s *Service) GrantStaffRole(ctx context.Context, actor *Principal, userID, role, reason string) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		if err := checkRole(ctx, q, role); err != nil {
			return err
		}
		_, before, err := s.staffTarget(ctx, q, actor, userID)
		if err != nil {
			return err
		}
		if privileged(role) && !actor.HasRole("super_admin") {
			return errPrivileged
		}
		if err := q.GrantRole(ctx, store.GrantRoleParams{UserID: userID, RoleKey: role, GrantedBy: &actor.UserID}); err != nil {
			return err
		}
		after, err := q.UserRoles(ctx, userID)
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor.UserID, Action: "staff.role_granted", TargetType: "user", TargetID: userID,
			Reason: reason, Metadata: map[string]any{"role": role, "before": before, "after": after}})
	})
}

// RevokeStaffRole removes a role. The change applies at the person's next
// request, because access is checked on every request.
func (s *Service) RevokeStaffRole(ctx context.Context, actor *Principal, userID, role, reason string) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		_, before, err := s.staffTarget(ctx, q, actor, userID)
		if err != nil {
			return err
		}
		if !slices.Contains(before, role) {
			return httpx.ErrNotFound
		}
		if privileged(role) && !actor.HasRole("super_admin") {
			return errPrivileged
		}
		if role == "super_admin" {
			last, err := s.lastSuperAdmin(ctx, q, before)
			if err != nil {
				return err
			}
			if last {
				return errLastSuper
			}
		}
		if _, err := q.RevokeRole(ctx, store.RevokeRoleParams{UserID: userID, RoleKey: role}); err != nil {
			return err
		}
		after, err := q.UserRoles(ctx, userID)
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor.UserID, Action: "staff.role_revoked", TargetType: "user", TargetID: userID,
			Reason: reason, Metadata: map[string]any{"role": role, "before": before, "after": after}})
	})
}

// SetStaffDisabled disables (and signs out everywhere) or re-enables a staff account.
func (s *Service) SetStaffDisabled(ctx context.Context, actor *Principal, userID string, disabled bool, reason string) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		u, roles, err := s.staffTarget(ctx, q, actor, userID)
		if err != nil {
			return err
		}
		if privileged(roles...) && !actor.HasRole("super_admin") {
			return errPrivileged
		}
		status := "active"
		if disabled {
			status = "disabled"
			last, err := s.lastSuperAdmin(ctx, q, roles)
			if err != nil {
				return err
			}
			if last && u.Status == "active" {
				return errLastSuper
			}
		}
		if _, err := q.SetUserStatus(ctx, store.SetUserStatusParams{ID: userID, Status: status}); err != nil {
			return err
		}
		if disabled {
			if _, err := q.RevokeUserSessions(ctx, store.RevokeUserSessionsParams{UserID: userID, Reason: ptr("account_disabled")}); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor.UserID, Action: "staff." + map[bool]string{true: "disabled", false: "enabled"}[disabled],
			TargetType: "user", TargetID: userID, Reason: reason, Metadata: map[string]any{"from": u.Status, "to": status}})
	})
}

// SignOutStaff ends every session of a staff member.
func (s *Service) SignOutStaff(ctx context.Context, actor *Principal, userID, reason string) (int64, error) {
	var n int64
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		_, roles, err := s.staffTarget(ctx, q, actor, userID)
		if err != nil {
			return err
		}
		if privileged(roles...) && !actor.HasRole("super_admin") {
			return errPrivileged
		}
		n, err = q.RevokeUserSessions(ctx, store.RevokeUserSessionsParams{UserID: userID, Reason: ptr("admin_revoked")})
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor.UserID, Action: "staff.signed_out", TargetType: "user", TargetID: userID,
			Reason: reason, Metadata: map[string]any{"sessions": n}})
	})
	return n, err
}

// ---------------------------------------------------------------- invitations

// Invitation is an open staff invitation.
type Invitation struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

func invitationView(i store.StaffInvitation) Invitation {
	return Invitation{ID: i.ID, Email: i.Email, Name: i.Name, Role: i.RoleKey, ExpiresAt: i.ExpiresAt, CreatedAt: i.CreatedAt}
}

func (s *Service) OpenInvitations(ctx context.Context) ([]Invitation, error) {
	rows, err := store.New(s.pool).ListOpenInvitations(ctx)
	if err != nil {
		return nil, err
	}
	out := []Invitation{}
	for _, r := range rows {
		out = append(out, invitationView(r))
	}
	return out, nil
}

// InviteInput is a new staff invitation.
type InviteInput struct {
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}

// Invite records an invitation and queues its email. The link token is made
// when the email is sent, so it never waits in the jobs table.
func (s *Service) Invite(ctx context.Context, actor *Principal, in InviteInput) (*Invitation, error) {
	address, canonical, ok := platformemail.Normalize(in.Email)
	name := strings.Join(strings.Fields(in.Name), " ")
	var f []httpx.FieldError
	if !ok {
		f = append(f, httpx.FieldError{Field: "email", Code: "invalid", Message: "Enter a valid email address."})
	}
	if name == "" || len([]rune(name)) > 80 {
		f = append(f, httpx.FieldError{Field: "name", Code: "length", Message: "Enter a name of up to 80 characters."})
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	var out Invitation
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		if err := checkRole(ctx, q, in.Role); err != nil {
			return err
		}
		if privileged(in.Role) && !actor.HasRole("super_admin") {
			return errPrivileged
		}
		if _, err := q.GetUserByCanonicalEmail(ctx, canonical); err == nil {
			return httpx.ValidationError([]httpx.FieldError{{Field: "email", Code: "taken",
				Message: "This address already has a Mise account. Grant a role to the existing staff member instead."}})
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		_, hash, err := newToken()
		if err != nil {
			return err
		}
		inv, err := q.CreateInvitation(ctx, store.CreateInvitationParams{Email: address, EmailCanonical: canonical, Name: name, RoleKey: in.Role,
			TokenHash: hash, InvitedBy: &actor.UserID, ExpiresAt: s.now().Add(invitationTTL)})
		if db.IsUniqueViolation(err, "staff_invitations_one_open") {
			return httpx.ValidationError([]httpx.FieldError{{Field: "email", Code: "taken", Message: "This address already has an open invitation."}})
		}
		if err != nil {
			return err
		}
		out = invitationView(inv)
		if err := jobs.Enqueue(ctx, tx, JobStaffInvite, map[string]string{"invitation_id": inv.ID}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor.UserID, Action: "staff.invited", TargetType: "staff_invitation", TargetID: inv.ID,
			Metadata: map[string]any{"role": in.Role}})
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *Service) RevokeInvitation(ctx context.Context, actor *Principal, id string) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		n, err := store.New(tx).RevokeInvitation(ctx, id)
		if err != nil {
			return err
		}
		if n == 0 {
			return httpx.ErrNotFound
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor.UserID, Action: "staff.invitation_revoked", TargetType: "staff_invitation", TargetID: id})
	})
}

// PrepareInvitationEmail makes a fresh link token for an open invitation and
// returns what to send, or nil if it was accepted or revoked meanwhile.
func (s *Service) PrepareInvitationEmail(ctx context.Context, invitationID string) (*AccountEmail, error) {
	var out *AccountEmail
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		inv, err := q.GetInvitation(ctx, invitationID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil || inv.AcceptedAt != nil || inv.RevokedAt != nil {
			return err
		}
		raw, hash, err := newToken()
		if err != nil {
			return err
		}
		if err := q.SetInvitationToken(ctx, store.SetInvitationTokenParams{ID: inv.ID, TokenHash: hash, ExpiresAt: s.now().Add(invitationTTL)}); err != nil {
			return err
		}
		out = &AccountEmail{To: inv.Email, FirstName: inv.Name, ActionURL: s.opts.AdminAppURL + "/accept-invite?token=" + raw, LinkTTL: invitationTTL}
		return nil
	})
	return out, err
}

var errInvitation = httpx.NewError(http.StatusBadRequest, "token_invalid", "This invitation is not valid or has expired. Ask for a new one.")

// AcceptInvitation turns an invitation into a staff account with the
// chosen password. The address is verified by the link itself. Outside
// local development the person sets up MFA at their first sign-in.
func (s *Service) AcceptInvitation(ctx context.Context, token, password string) error {
	if msg := CheckPassword(password); msg != "" {
		return httpx.ValidationError([]httpx.FieldError{{Field: "password", Code: "weak", Message: msg}})
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		inv, err := q.InvitationByTokenHash(ctx, hashToken(token))
		if errors.Is(err, pgx.ErrNoRows) {
			return errInvitation
		}
		if err != nil {
			return err
		}
		if inv.AcceptedAt != nil || inv.RevokedAt != nil || !s.now().Before(inv.ExpiresAt) {
			return errInvitation
		}
		if _, err := q.GetUserByCanonicalEmail(ctx, inv.EmailCanonical); err == nil {
			return errInvitation
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		now := s.now()
		first, last := splitName(inv.Name)
		u, err := q.CreateUser(ctx, store.CreateUserParams{Email: inv.Email, EmailCanonical: inv.EmailCanonical, FirstName: first, LastName: last, EmailVerifiedAt: &now})
		if err != nil {
			return err
		}
		if err := q.SetPassword(ctx, store.SetPasswordParams{UserID: u.ID, PasswordHash: hash}); err != nil {
			return err
		}
		if err := q.GrantRole(ctx, store.GrantRoleParams{UserID: u.ID, RoleKey: inv.RoleKey, GrantedBy: inv.InvitedBy}); err != nil {
			return err
		}
		if err := q.AcceptInvitation(ctx, store.AcceptInvitationParams{ID: inv.ID, UserID: &u.ID}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: u.ID, Action: "staff.invitation_accepted", TargetType: "user", TargetID: u.ID,
			Metadata: map[string]any{"role": inv.RoleKey, "invitation_id": inv.ID}})
	})
}

func splitName(name string) (string, string) {
	parts := strings.Fields(name)
	if len(parts) < 2 {
		return name, ""
	}
	return strings.Join(parts[:len(parts)-1], " "), parts[len(parts)-1]
}
