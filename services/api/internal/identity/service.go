// Package identity owns who people are and what they may do: users,
// passwords, sessions, email verification, password reset, MFA, roles and
// permissions. Every other module asks it "who is calling?" and "may they?".
//
// Sessions are opaque random tokens; only their SHA-256 hash is stored. The
// browser never holds one: each Next.js app keeps it in an httpOnly cookie on
// its own server and calls the API with it (docs/security/AUTHENTICATION.md).
package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/identity/store"
	"mise.tt/api/internal/platform/crypto"
	"mise.tt/api/internal/platform/db"
	platformemail "mise.tt/api/internal/platform/email"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/jobs"
	"mise.tt/api/internal/platform/ratelimit"
)

// Job kinds for account email, handled by the notifications module. Payloads
// carry the user id only; the token is created when the email is sent, so it
// never sits in the jobs table.
const (
	JobVerifyEmail       = "account.verify_email"
	JobPasswordReset     = "account.password_reset"
	JobAlreadyRegistered = "account.already_registered"
)

type AccountJob struct {
	UserID string `json:"user_id"`
}

const (
	purposeVerifyEmail   = "verify_email"
	purposeResetPassword = "reset_password"

	verifyEmailTTL   = 48 * time.Hour
	resetPasswordTTL = time.Hour
	// A session that has passed the password but not yet the second factor.
	mfaPendingTTL = 10 * time.Minute
	totpIssuer    = "Mise"
)

var (
	ErrBadCredentials = httpx.NewError(http.StatusUnauthorized, "invalid_credentials", "Email or password is incorrect.")
	ErrMFARequired    = httpx.NewError(http.StatusForbidden, "mfa_required", "Finish signing in with your authenticator app.")

	errEmailNotVerified = httpx.NewError(http.StatusForbidden, "email_not_verified",
		"Confirm your email address first. We have sent you a new link.")
	errRegistrationClosed = httpx.NewError(http.StatusForbidden, "registration_closed",
		"Mise accounts are not open yet. Join the waitlist and we will let you know.")
	errTokenInvalid = httpx.NewError(http.StatusBadRequest, "token_invalid",
		"This link is not valid or has expired. Ask for a new one.")
	errMFACodeInvalid = httpx.NewError(http.StatusUnauthorized, "mfa_code_invalid",
		"That code is not right. Check your authenticator app and try again.")
	errMFATooManyAttempts = httpx.NewError(http.StatusUnauthorized, "mfa_locked",
		"Too many wrong codes. Sign in again.")
	errMFAAlreadyEnrolled = httpx.NewError(http.StatusConflict, "mfa_already_enrolled",
		"An authenticator app is already set up for this account.")
	errMFANotStarted = httpx.NewError(http.StatusConflict, "mfa_not_started",
		"Start setting up your authenticator app first.")
	errMFANotNeeded = httpx.NewError(http.StatusConflict, "mfa_not_needed",
		"This session has already finished signing in.")
)

type Options struct {
	AdminSessionTTL        time.Duration
	AdminSessionIdleTTL    time.Duration
	CustomerSessionTTL     time.Duration
	CustomerSessionIdleTTL time.Duration
	AdminMFARequired       bool
	RegistrationEnabled    bool
	TermsVersion           string
	// CustomerAppURL is where account emails link to.
	CustomerAppURL string
}

// Hooks lets the customers module act on account events inside the same
// transaction, without identity depending on it.
type Hooks interface {
	CustomerRegistered(ctx context.Context, q db.Querier, userID, termsVersion string) error
	EmailVerified(ctx context.Context, q db.Querier, userID, emailCanonical string) error
}

type Service struct {
	pool   *pgxpool.Pool
	sealer *crypto.Sealer
	hashIP func(string) string
	hooks  Hooks
	opts   Options
	now    func() time.Time
	// mfaFailures counts wrong codes per session; past the limit the session ends.
	mfaFailures *ratelimit.Limiter
}

func NewService(pool *pgxpool.Pool, sealer *crypto.Sealer, hashIP func(string) string, hooks Hooks, opts Options) *Service {
	return &Service{
		pool:        pool,
		sealer:      sealer,
		hashIP:      hashIP,
		hooks:       hooks,
		opts:        opts,
		now:         time.Now,
		mfaFailures: ratelimit.New(5, 15*time.Minute, 5),
	}
}

// ClientMeta describes the device a request came from.
type ClientMeta struct {
	IP        string
	UserAgent string
}

// Grant is a new or renewed session.
type Grant struct {
	Token     string
	ExpiresAt time.Time
	Principal *Principal
}

func newToken() (raw string, hash []byte, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, hashToken(raw), nil
}

func hashToken(tok string) []byte {
	sum := sha256.Sum256([]byte(tok))
	return sum[:]
}

func ptr[T any](v T) *T { return &v }

// ---------------------------------------------------------------- principals

func (s *Service) idleTTL(a Audience) time.Duration {
	if a == AudienceAdmin {
		return s.opts.AdminSessionIdleTTL
	}
	return s.opts.CustomerSessionIdleTTL
}

func (s *Service) sessionTTL(a Audience) time.Duration {
	if a == AudienceAdmin {
		return s.opts.AdminSessionTTL
	}
	return s.opts.CustomerSessionTTL
}

func (s *Service) loadAccess(ctx context.Context, q *store.Queries, p *Principal) error {
	roles, err := q.UserRoles(ctx, p.UserID)
	if err != nil {
		return err
	}
	perms, err := q.UserPermissions(ctx, p.UserID)
	if err != nil {
		return err
	}
	p.Roles = roles
	p.Permissions = make([]Permission, len(perms))
	for i, k := range perms {
		p.Permissions[i] = Permission(k)
	}
	return nil
}

// adminMFAState works out the second-factor position of an admin session.
func (s *Service) adminMFAState(ctx context.Context, q *store.Queries, userID string, sessionVerifiedAt *time.Time) (MFAState, error) {
	if sessionVerifiedAt != nil {
		return MFAVerified, nil
	}
	t, err := q.GetTOTP(ctx, userID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	switch {
	case err == nil && t.ConfirmedAt != nil:
		// Once someone has MFA, it is always asked for, in every environment.
		return MFAChallenge, nil
	case s.opts.AdminMFARequired:
		return MFAEnrolmentRequired, nil
	default:
		return MFANotEnrolled, nil
	}
}

// Authenticate resolves a session token for one audience. It returns nil,
// nil when the token is missing, unknown, expired, idle, revoked, belongs to
// a disabled user, or no longer carries the access the audience needs.
func (s *Service) Authenticate(ctx context.Context, tok string, audience Audience) (*Principal, error) {
	if tok == "" || len(tok) > 128 {
		return nil, nil
	}
	q := store.New(s.pool)
	row, err := q.FindLiveSession(ctx, store.FindLiveSessionParams{
		TokenHash:   hashToken(tok),
		Audience:    string(audience),
		IdleSeconds: s.idleTTL(audience).Seconds(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p := &Principal{
		UserID:        row.UserID,
		Email:         row.Email,
		FirstName:     row.FirstName,
		LastName:      row.LastName,
		EmailVerified: row.EmailVerifiedAt != nil,
		SessionID:     row.ID,
		Audience:      audience,
		MFA:           MFANotApplicable,
	}
	if err := s.loadAccess(ctx, q, p); err != nil {
		return nil, err
	}
	// Access is re-checked on every request, so revoking a role takes effect
	// at once rather than when the session ends.
	switch audience {
	case AudienceAdmin:
		if !p.Can(PermAdminAccess) {
			return nil, nil
		}
		if p.MFA, err = s.adminMFAState(ctx, q, p.UserID, row.MfaVerifiedAt); err != nil {
			return nil, err
		}
	case AudienceCustomer:
		if !p.HasRole(RoleCustomer) {
			return nil, nil
		}
	}
	if err := q.TouchSession(ctx, row.ID); err != nil {
		return nil, err
	}
	return p, nil
}

// ---------------------------------------------------------------- sign-in

// Login checks a password and opens a session for one audience. For admins
// with MFA, the session is short-lived and limited to the second-factor step
// until a code is presented.
func (s *Service) Login(ctx context.Context, audience Audience, rawEmail, password string, meta ClientMeta) (*Grant, error) {
	q := store.New(s.pool)
	_, canonical, ok := platformemail.Normalize(rawEmail)
	if !ok {
		VerifyPassword(dummyHash, password)
		return nil, ErrBadCredentials
	}
	u, err := q.GetUserByCanonicalEmail(ctx, canonical)
	if errors.Is(err, pgx.ErrNoRows) {
		VerifyPassword(dummyHash, password)
		s.logFailedLogin(ctx, audience, "")
		return nil, ErrBadCredentials
	}
	if err != nil {
		return nil, err
	}
	hash, err := q.GetPasswordHash(ctx, u.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		VerifyPassword(dummyHash, password)
		s.logFailedLogin(ctx, audience, u.ID)
		return nil, ErrBadCredentials
	}
	if err != nil {
		return nil, err
	}
	if !VerifyPassword(hash, password) || u.Status != "active" {
		s.logFailedLogin(ctx, audience, u.ID)
		return nil, ErrBadCredentials
	}

	p := &Principal{
		UserID: u.ID, Email: u.Email, FirstName: u.FirstName, LastName: u.LastName,
		EmailVerified: u.EmailVerifiedAt != nil, Audience: audience, MFA: MFANotApplicable,
	}
	if err := s.loadAccess(ctx, q, p); err != nil {
		return nil, err
	}

	ttl := s.sessionTTL(audience)
	switch audience {
	case AudienceAdmin:
		// Someone without staff access gets the same answer as a wrong password.
		if !p.Can(PermAdminAccess) {
			s.logFailedLogin(ctx, audience, u.ID)
			return nil, ErrBadCredentials
		}
		if p.MFA, err = s.adminMFAState(ctx, q, u.ID, nil); err != nil {
			return nil, err
		}
		if !p.MFA.Complete() {
			ttl = mfaPendingTTL
		}
	case AudienceCustomer:
		if !p.HasRole(RoleCustomer) {
			s.logFailedLogin(ctx, audience, u.ID)
			return nil, ErrBadCredentials
		}
		if !p.EmailVerified {
			if err := jobs.Enqueue(ctx, s.pool, JobVerifyEmail, AccountJob{UserID: u.ID}); err != nil {
				return nil, err
			}
			return nil, errEmailNotVerified
		}
	}

	raw, hashed, err := newToken()
	if err != nil {
		return nil, err
	}
	expires := s.now().Add(ttl)
	var grant *Grant
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		tq := store.New(tx)
		sess, err := tq.CreateSession(ctx, store.CreateSessionParams{
			UserID:    u.ID,
			TokenHash: hashed,
			Audience:  string(audience),
			Client:    "web",
			UserAgent: truncatedUA(meta.UserAgent),
			IpHash:    s.ipHash(meta.IP),
			ExpiresAt: expires,
		})
		if err != nil {
			return err
		}
		if err := tq.TouchLogin(ctx, u.ID); err != nil {
			return err
		}
		p.SessionID = sess.ID
		grant = &Grant{Token: raw, ExpiresAt: expires, Principal: p}
		return audit.Record(ctx, tx, audit.Entry{
			ActorUserID: u.ID, Action: "auth.login", TargetType: "user", TargetID: u.ID,
			Metadata: map[string]any{"audience": string(audience), "mfa": string(p.MFA)},
		})
	})
	if err != nil {
		return nil, err
	}
	// Housekeeping without a scheduler.
	if err := q.DeleteStaleSessions(ctx); err != nil {
		slog.WarnContext(ctx, "delete stale sessions", "error", err.Error())
	}
	return grant, nil
}

func (s *Service) logFailedLogin(ctx context.Context, audience Audience, userID string) {
	slog.WarnContext(ctx, "login failed", "event", "auth.login_failed", "audience", string(audience))
	if userID == "" {
		return
	}
	// Failures against a real account are audited (no password, no email):
	// they are what an investigation of a targeted account looks for.
	if err := audit.Record(ctx, s.pool, audit.Entry{
		Action: "auth.login_failed", TargetType: "user", TargetID: userID,
		Metadata: map[string]any{"audience": string(audience)},
	}); err != nil {
		slog.ErrorContext(ctx, "audit failed login", "error", err.Error())
	}
}

func (s *Service) ipHash(ip string) *string {
	if ip == "" || s.hashIP == nil {
		return nil
	}
	return ptr(s.hashIP(ip))
}

func truncatedUA(ua string) *string {
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return nil
	}
	if len(ua) > 256 {
		ua = ua[:256]
	}
	return &ua
}

// Logout ends the caller's session.
func (s *Service) Logout(ctx context.Context, p *Principal) error {
	_, err := store.New(s.pool).RevokeSession(ctx, store.RevokeSessionParams{ID: p.SessionID, UserID: p.UserID, Reason: ptr("logout")})
	if err != nil {
		return err
	}
	return audit.Record(ctx, s.pool, audit.Entry{
		ActorUserID: p.UserID, Action: "auth.logout", TargetType: "session", TargetID: p.SessionID,
	})
}

// ---------------------------------------------------------------- MFA

// VerifyMFA completes an admin sign-in with a TOTP code. The session token is
// replaced; the old one stops working.
func (s *Service) VerifyMFA(ctx context.Context, p *Principal, code string) (*Grant, error) {
	if p.MFA != MFAChallenge {
		return nil, errMFANotNeeded
	}
	q := store.New(s.pool)
	t, err := q.GetTOTP(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	secret, err := s.sealer.Open(t.SecretCiphertext, []byte(p.UserID))
	if err != nil {
		return nil, err
	}
	step, ok := matchTOTP(string(secret), strings.TrimSpace(code), s.now())
	if ok {
		n, err := q.UseTOTPStep(ctx, store.UseTOTPStepParams{UserID: p.UserID, Step: &step})
		if err != nil {
			return nil, err
		}
		ok = n == 1 // a replayed code matches but was already used
	}
	if !ok {
		return nil, s.mfaFailed(ctx, p)
	}
	return s.elevate(ctx, p, "auth.mfa_verified")
}

func (s *Service) mfaFailed(ctx context.Context, p *Principal) error {
	slog.WarnContext(ctx, "mfa code rejected", "event", "auth.mfa_failed")
	if err := audit.Record(ctx, s.pool, audit.Entry{
		ActorUserID: p.UserID, Action: "auth.mfa_failed", TargetType: "user", TargetID: p.UserID,
	}); err != nil {
		return err
	}
	if s.mfaFailures.Allow(p.SessionID) {
		return errMFACodeInvalid
	}
	if _, err := store.New(s.pool).RevokeSession(ctx, store.RevokeSessionParams{ID: p.SessionID, UserID: p.UserID, Reason: ptr("mfa_failed")}); err != nil {
		return err
	}
	return errMFATooManyAttempts
}

// MFASetup is what an admin needs to add Mise to an authenticator app.
type MFASetup struct {
	Secret     string `json:"secret"`
	OTPAuthURL string `json:"otpauth_url"`
	QRPNG      string `json:"qr_png_base64"`
}

// StartMFAEnrolment returns the secret to add to an authenticator app. While
// enrolment is unconfirmed it returns the same secret every time, so
// reloading the page never invalidates a QR code someone has already scanned.
func (s *Service) StartMFAEnrolment(ctx context.Context, p *Principal) (*MFASetup, error) {
	if p.MFA == MFAChallenge || p.MFA == MFAVerified {
		return nil, errMFAAlreadyEnrolled
	}
	q := store.New(s.pool)
	existing, err := q.GetTOTP(ctx, p.UserID)
	switch {
	case err == nil && existing.ConfirmedAt != nil:
		return nil, errMFAAlreadyEnrolled
	case err == nil:
		secret, err := s.sealer.Open(existing.SecretCiphertext, []byte(p.UserID))
		if err != nil {
			return nil, err
		}
		key, err := existingTOTPKey(totpIssuer, p.Email, string(secret))
		if err != nil {
			return nil, err
		}
		return &MFASetup{Secret: key.Secret, OTPAuthURL: key.OTPAuthURL, QRPNG: key.QRPNG}, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, err
	}

	key, err := newTOTPKey(totpIssuer, p.Email)
	if err != nil {
		return nil, err
	}
	sealed, err := s.sealer.Seal([]byte(key.Secret), []byte(p.UserID))
	if err != nil {
		return nil, err
	}
	n, err := q.StartTOTPEnrolment(ctx, store.StartTOTPEnrolmentParams{UserID: p.UserID, SecretCiphertext: sealed})
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, errMFAAlreadyEnrolled
	}
	return &MFASetup{Secret: key.Secret, OTPAuthURL: key.OTPAuthURL, QRPNG: key.QRPNG}, nil
}

// ConfirmMFAEnrolment checks the first code from the app, turns MFA on for
// the account and completes the current session.
func (s *Service) ConfirmMFAEnrolment(ctx context.Context, p *Principal, code string) (*Grant, error) {
	if p.MFA == MFAChallenge || p.MFA == MFAVerified {
		return nil, errMFAAlreadyEnrolled
	}
	q := store.New(s.pool)
	t, err := q.GetTOTP(ctx, p.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errMFANotStarted
	}
	if err != nil {
		return nil, err
	}
	secret, err := s.sealer.Open(t.SecretCiphertext, []byte(p.UserID))
	if err != nil {
		return nil, err
	}
	step, ok := matchTOTP(string(secret), strings.TrimSpace(code), s.now())
	if !ok {
		return nil, s.mfaFailed(ctx, p)
	}
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		tq := store.New(tx)
		if err := tq.ConfirmTOTP(ctx, p.UserID); err != nil {
			return err
		}
		_, err := tq.UseTOTPStep(ctx, store.UseTOTPStepParams{UserID: p.UserID, Step: &step})
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.elevate(ctx, p, "auth.mfa_enrolled")
}

func (s *Service) elevate(ctx context.Context, p *Principal, action string) (*Grant, error) {
	raw, hashed, err := newToken()
	if err != nil {
		return nil, err
	}
	expires := s.now().Add(s.opts.AdminSessionTTL)
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := store.New(tx).ElevateSession(ctx, store.ElevateSessionParams{ID: p.SessionID, NewTokenHash: hashed, ExpiresAt: expires}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: action, TargetType: "user", TargetID: p.UserID})
	})
	if err != nil {
		return nil, err
	}
	next := *p
	next.MFA = MFAVerified
	return &Grant{Token: raw, ExpiresAt: expires, Principal: &next}, nil
}

// ---------------------------------------------------------------- customer accounts

type RegisterInput struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
	AcceptTerms bool   `json:"accept_terms"`
}

// Register creates a customer account and sends a verification email. The
// caller learns nothing about whether the address already had an account:
// that person is emailed instead.
func (s *Service) Register(ctx context.Context, in RegisterInput) error {
	if !s.opts.RegistrationEnabled {
		return errRegistrationClosed
	}
	var fields []httpx.FieldError
	address, canonical, ok := platformemail.Normalize(in.Email)
	if !ok {
		fields = append(fields, httpx.FieldError{Field: "email", Code: "invalid", Message: "Enter a valid email address."})
	}
	first, firstOK := cleanName(in.FirstName)
	if !firstOK {
		fields = append(fields, httpx.FieldError{Field: "first_name", Code: "invalid", Message: "Enter your first name."})
	}
	last, lastOK := cleanName(in.LastName)
	if !lastOK {
		fields = append(fields, httpx.FieldError{Field: "last_name", Code: "invalid", Message: "Enter your last name."})
	}
	if problem := CheckPassword(in.Password); problem != "" {
		fields = append(fields, httpx.FieldError{Field: "password", Code: "weak", Message: problem})
	}
	if !in.AcceptTerms {
		fields = append(fields, httpx.FieldError{Field: "accept_terms", Code: "required", Message: "Accept the terms to create an account."})
	}
	if len(fields) > 0 {
		return httpx.ValidationError(fields)
	}
	hash, err := HashPassword(in.Password)
	if err != nil {
		return err
	}

	register := func(tx pgx.Tx) error {
		q := store.New(tx)
		existing, err := q.GetUserByCanonicalEmail(ctx, canonical)
		if err == nil {
			if existing.Status != "active" {
				return nil
			}
			kind := JobAlreadyRegistered
			if existing.EmailVerifiedAt == nil {
				kind = JobVerifyEmail
			}
			return jobs.Enqueue(ctx, tx, kind, AccountJob{UserID: existing.ID})
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		u, err := q.CreateUser(ctx, store.CreateUserParams{Email: address, EmailCanonical: canonical, FirstName: first, LastName: last})
		if err != nil {
			return err
		}
		if err := q.SetPassword(ctx, store.SetPasswordParams{UserID: u.ID, PasswordHash: hash}); err != nil {
			return err
		}
		if err := q.GrantRole(ctx, store.GrantRoleParams{UserID: u.ID, RoleKey: RoleCustomer}); err != nil {
			return err
		}
		if err := s.hooks.CustomerRegistered(ctx, tx, u.ID, s.opts.TermsVersion); err != nil {
			return err
		}
		if err := jobs.Enqueue(ctx, tx, JobVerifyEmail, AccountJob{UserID: u.ID}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: u.ID, Action: "account.registered", TargetType: "user", TargetID: u.ID})
	}
	// Two registrations for one address at the same moment: the loser of the
	// unique constraint runs again and takes the "already registered" path.
	for attempt := 0; ; attempt++ {
		err := db.InTx(ctx, s.pool, register)
		if err != nil && attempt == 0 && db.IsUniqueViolation(err, "users_email_canonical_key") {
			continue
		}
		return err
	}
}

func cleanName(raw string) (string, bool) {
	name := strings.Join(strings.Fields(raw), " ")
	if name == "" || len([]rune(name)) > 80 {
		return "", false
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '<' || r == '>' {
			return "", false
		}
	}
	return name, true
}

// VerifyEmail consumes a verification link. For customers it also links the
// account to the waitlist lead with the same address, if there is one.
func (s *Service) VerifyEmail(ctx context.Context, raw string) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		userID, err := q.ConsumeAuthToken(ctx, store.ConsumeAuthTokenParams{TokenHash: hashToken(raw), Purpose: purposeVerifyEmail})
		if errors.Is(err, pgx.ErrNoRows) {
			return errTokenInvalid
		}
		if err != nil {
			return err
		}
		return s.markVerified(ctx, tx, userID, "account.email_verified")
	})
}

func (s *Service) markVerified(ctx context.Context, tx pgx.Tx, userID, action string) error {
	q := store.New(tx)
	before, err := q.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if before.Status != "active" {
		return errTokenInvalid
	}
	u, err := q.MarkEmailVerified(ctx, userID)
	if err != nil {
		return err
	}
	roles, err := q.UserRoles(ctx, userID)
	if err != nil {
		return err
	}
	if before.EmailVerifiedAt == nil && contains(roles, RoleCustomer) {
		if err := s.hooks.EmailVerified(ctx, tx, userID, u.EmailCanonical); err != nil {
			return err
		}
	}
	return audit.Record(ctx, tx, audit.Entry{ActorUserID: userID, Action: action, TargetType: "user", TargetID: userID})
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// RequestPasswordReset emails a reset link to a customer. Staff passwords are
// reset by an operator (misectl), never by email alone, so a compromised inbox
// cannot take over an account with admin access. The answer is the same
// whether or not the address has an account.
func (s *Service) RequestPasswordReset(ctx context.Context, rawEmail string) error {
	_, canonical, ok := platformemail.Normalize(rawEmail)
	if !ok {
		return httpx.ValidationError([]httpx.FieldError{{Field: "email", Code: "invalid", Message: "Enter a valid email address."}})
	}
	q := store.New(s.pool)
	u, err := q.GetUserByCanonicalEmail(ctx, canonical)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	p := &Principal{UserID: u.ID}
	if err := s.loadAccess(ctx, q, p); err != nil {
		return err
	}
	if u.Status != "active" || !p.HasRole(RoleCustomer) || p.Can(PermAdminAccess) {
		return nil
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := jobs.Enqueue(ctx, tx, JobPasswordReset, AccountJob{UserID: u.ID}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{Action: "account.password_reset_requested", TargetType: "user", TargetID: u.ID})
	})
}

// ResetPassword sets a new password from a reset link and ends every session.
func (s *Service) ResetPassword(ctx context.Context, raw, password string) error {
	if problem := CheckPassword(password); problem != "" {
		return httpx.ValidationError([]httpx.FieldError{{Field: "password", Code: "weak", Message: problem}})
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		userID, err := q.ConsumeAuthToken(ctx, store.ConsumeAuthTokenParams{TokenHash: hashToken(raw), Purpose: purposeResetPassword})
		if errors.Is(err, pgx.ErrNoRows) {
			return errTokenInvalid
		}
		if err != nil {
			return err
		}
		if err := q.SetPassword(ctx, store.SetPasswordParams{UserID: userID, PasswordHash: hash}); err != nil {
			return err
		}
		if err := q.InvalidateAuthTokens(ctx, store.InvalidateAuthTokensParams{UserID: userID, Purpose: purposeResetPassword}); err != nil {
			return err
		}
		if _, err := q.RevokeUserSessions(ctx, store.RevokeUserSessionsParams{UserID: userID, Reason: ptr("password_reset")}); err != nil {
			return err
		}
		// Following the link proves the inbox, so the address counts as verified.
		return s.markVerified(ctx, tx, userID, "account.password_reset")
	})
}

// ---------------------------------------------------------------- sessions (self-service)

type SessionInfo struct {
	ID         string    `json:"id"`
	Current    bool      `json:"current"`
	Client     string    `json:"client"`
	UserAgent  string    `json:"user_agent"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

func (s *Service) ListSessions(ctx context.Context, p *Principal) ([]SessionInfo, error) {
	rows, err := store.New(s.pool).ListLiveSessions(ctx, store.ListLiveSessionsParams{UserID: p.UserID, Audience: string(p.Audience)})
	if err != nil {
		return nil, err
	}
	out := make([]SessionInfo, 0, len(rows))
	for _, r := range rows {
		info := SessionInfo{ID: r.ID, Current: r.ID == p.SessionID, Client: r.Client, CreatedAt: r.CreatedAt, LastSeenAt: r.LastSeenAt, ExpiresAt: r.ExpiresAt}
		if r.UserAgent != nil {
			info.UserAgent = *r.UserAgent
		}
		out = append(out, info)
	}
	return out, nil
}

// RevokeSession ends one of the caller's own sessions. It reports false when
// no such live session belongs to them.
func (s *Service) RevokeSession(ctx context.Context, p *Principal, sessionID string) (bool, error) {
	n, err := store.New(s.pool).RevokeSession(ctx, store.RevokeSessionParams{ID: sessionID, UserID: p.UserID, Reason: ptr("user_revoked")})
	if err != nil || n == 0 {
		return false, err
	}
	return true, audit.Record(ctx, s.pool, audit.Entry{ActorUserID: p.UserID, Action: "auth.session_revoked", TargetType: "session", TargetID: sessionID})
}

// RevokeOtherSessions ends every session of the caller except this one.
func (s *Service) RevokeOtherSessions(ctx context.Context, p *Principal) (int64, error) {
	n, err := store.New(s.pool).RevokeUserSessions(ctx, store.RevokeUserSessionsParams{UserID: p.UserID, Reason: ptr("user_revoked"), KeepID: &p.SessionID})
	if err != nil {
		return 0, err
	}
	return n, audit.Record(ctx, s.pool, audit.Entry{
		ActorUserID: p.UserID, Action: "auth.sessions_revoked", TargetType: "user", TargetID: p.UserID,
		Metadata: map[string]any{"count": n},
	})
}

// ---------------------------------------------------------------- account email (worker)

// AccountEmail is what the notifications module needs to send one email.
type AccountEmail struct {
	To        string
	FirstName string
	ActionURL string
	// LinkTTL is how long ActionURL works, for the email copy. Zero if no token.
	LinkTTL time.Duration
}

// PrepareAccountEmail creates the single-use token for an account email (if
// the email needs one) and returns what to send. It returns nil when the email
// should no longer be sent, for example because the address was verified in
// the meantime or the account was disabled.
func (s *Service) PrepareAccountEmail(ctx context.Context, kind, userID string) (*AccountEmail, error) {
	var out *AccountEmail
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		u, err := q.GetUserByID(ctx, userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil || u.Status != "active" {
			return err
		}
		email := &AccountEmail{To: u.Email, FirstName: u.FirstName}
		base := s.opts.CustomerAppURL
		switch kind {
		case JobVerifyEmail:
			if u.EmailVerifiedAt != nil {
				return nil
			}
			tok, err := s.issueToken(ctx, q, u.ID, purposeVerifyEmail, verifyEmailTTL)
			if err != nil {
				return err
			}
			email.ActionURL, email.LinkTTL = base+"/verify-email?token="+tok, verifyEmailTTL
		case JobPasswordReset:
			tok, err := s.issueToken(ctx, q, u.ID, purposeResetPassword, resetPasswordTTL)
			if err != nil {
				return err
			}
			email.ActionURL, email.LinkTTL = base+"/reset-password?token="+tok, resetPasswordTTL
		case JobAlreadyRegistered:
			email.ActionURL = base + "/sign-in"
		default:
			return errors.New("unknown account email kind " + kind)
		}
		out = email
		return nil
	})
	return out, err
}

// issueToken replaces any open token of the same purpose, so only the most
// recent link in someone's inbox works.
func (s *Service) issueToken(ctx context.Context, q *store.Queries, userID, purpose string, ttl time.Duration) (string, error) {
	if err := q.InvalidateAuthTokens(ctx, store.InvalidateAuthTokensParams{UserID: userID, Purpose: purpose}); err != nil {
		return "", err
	}
	raw, hashed, err := newToken()
	if err != nil {
		return "", err
	}
	err = q.CreateAuthToken(ctx, store.CreateAuthTokenParams{UserID: userID, Purpose: purpose, TokenHash: hashed, ExpiresAt: s.now().Add(ttl)})
	return raw, err
}
