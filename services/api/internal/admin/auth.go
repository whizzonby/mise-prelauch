// Package admin is the internal back office: authentication, permissions,
// lead management, statistics, export and the audit log.
package admin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/argon2"

	"mise.tt/api/internal/platform/httpx"
)

// Permission is one thing an admin may do. Routes check permissions, never
// role names, so adding a role is a one-line change to rolePermissions.
type Permission string

const (
	PermStatsRead   Permission = "stats:read"
	PermLeadsRead   Permission = "leads:read"
	PermLeadsWrite  Permission = "leads:write"
	PermLeadsExport Permission = "leads:export"
	PermLeadsErase  Permission = "leads:erase"
)

// rolePermissions maps the roles that exist today. Future platform roles
// (chef, farmer, kitchen staff, finance, …) are added here with their own
// permissions as their features are built.
var rolePermissions = map[string][]Permission{
	"super_admin": {PermStatsRead, PermLeadsRead, PermLeadsWrite, PermLeadsExport, PermLeadsErase},
	"admin":       {PermStatsRead, PermLeadsRead, PermLeadsWrite, PermLeadsExport, PermLeadsErase},
	"marketing":   {PermStatsRead, PermLeadsRead, PermLeadsExport},
	"operations":  {PermStatsRead, PermLeadsRead},
	"support":     {PermLeadsRead, PermLeadsWrite},
}

func ValidRole(role string) bool {
	_, ok := rolePermissions[role]
	return ok
}

type Principal struct {
	ID          string       `json:"id"`
	Email       string       `json:"email"`
	Name        string       `json:"name"`
	Role        string       `json:"role"`
	Permissions []Permission `json:"permissions"`
}

func (p *Principal) Can(perm Permission) bool {
	for _, have := range p.Permissions {
		if have == perm {
			return true
		}
	}
	return false
}

// Argon2id parameters (OWASP baseline: 19 MiB, 2 passes, 1 lane).
const (
	argonTime    = 2
	argonMemory  = 19 * 1024
	argonThreads = 1
	argonKeyLen  = 32
	saltLen      = 16
)

const MinPasswordLength = 12

func HashPassword(password string) (string, error) {
	if len(password) < MinPasswordLength {
		return "", fmt.Errorf("password must be at least %d characters", MinPasswordLength)
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var version int
	var memory, passes uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &passes, &threads); err != nil {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := enc.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, passes, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// dummyHash is verified when the email is unknown, so a login attempt takes
// the same time whether or not the account exists.
var dummyHash, _ = HashPassword("mise-timing-equaliser-password")

var errBadCredentials = httpx.NewError(http.StatusUnauthorized, "invalid_credentials", "Email or password is incorrect.")

type Auth struct {
	pool       *pgxpool.Pool
	sessionTTL time.Duration
}

func NewAuth(pool *pgxpool.Pool, sessionTTL time.Duration) *Auth {
	return &Auth{pool: pool, sessionTTL: sessionTTL}
}

func hashToken(tok string) []byte {
	sum := sha256.Sum256([]byte(tok))
	return sum[:]
}

// CreateUser adds an admin. Used by `misectl admin create`.
func (a *Auth) CreateUser(ctx context.Context, email, name, role, password string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") {
		return errors.New("a valid email is required")
	}
	if strings.TrimSpace(name) == "" {
		return errors.New("a name is required")
	}
	if !ValidRole(role) {
		return fmt.Errorf("unknown role %q", role)
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	_, err = a.pool.Exec(ctx,
		`INSERT INTO admin_users (email, name, role, password_hash) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (email) DO UPDATE SET name = EXCLUDED.name, role = EXCLUDED.role,
			password_hash = EXCLUDED.password_hash, disabled_at = NULL, updated_at = now()`,
		email, strings.TrimSpace(name), role, hash)
	return err
}

// Login checks credentials and returns a new session token. The token is
// returned once and only its hash is stored.
func (a *Auth) Login(ctx context.Context, email, password string) (string, *Principal, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var p Principal
	var hash string
	var disabled *time.Time
	err := a.pool.QueryRow(ctx,
		`SELECT id, email, name, role, password_hash, disabled_at FROM admin_users WHERE email = $1`, email,
	).Scan(&p.ID, &p.Email, &p.Name, &p.Role, &hash, &disabled)
	if errors.Is(err, pgx.ErrNoRows) {
		VerifyPassword(dummyHash, password)
		return "", nil, errBadCredentials
	}
	if err != nil {
		return "", nil, err
	}
	if !VerifyPassword(hash, password) || disabled != nil {
		return "", nil, errBadCredentials
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	tok := base64.RawURLEncoding.EncodeToString(raw)
	if _, err := a.pool.Exec(ctx,
		`INSERT INTO admin_sessions (admin_user_id, token_hash, expires_at) VALUES ($1, $2, now() + $3 * interval '1 second')`,
		p.ID, hashToken(tok), int(a.sessionTTL.Seconds())); err != nil {
		return "", nil, err
	}
	if _, err := a.pool.Exec(ctx, `UPDATE admin_users SET last_login_at = now() WHERE id = $1`, p.ID); err != nil {
		return "", nil, err
	}
	// Opportunistic cleanup keeps the session table small without a scheduler.
	_, _ = a.pool.Exec(ctx, `DELETE FROM admin_sessions WHERE expires_at < now()`)

	p.Permissions = rolePermissions[p.Role]
	return tok, &p, nil
}

func (a *Auth) Logout(ctx context.Context, tok string) error {
	_, err := a.pool.Exec(ctx, `DELETE FROM admin_sessions WHERE token_hash = $1`, hashToken(tok))
	return err
}

// authenticate resolves a session token to its admin, or returns nil.
func (a *Auth) authenticate(ctx context.Context, tok string) (*Principal, error) {
	if tok == "" {
		return nil, nil
	}
	var p Principal
	err := a.pool.QueryRow(ctx, `
		UPDATE admin_sessions s SET last_seen_at = now()
		FROM admin_users u
		WHERE s.token_hash = $1 AND s.expires_at > now() AND u.id = s.admin_user_id AND u.disabled_at IS NULL
		RETURNING u.id, u.email, u.name, u.role`, hashToken(tok),
	).Scan(&p.ID, &p.Email, &p.Name, &p.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.Permissions = rolePermissions[p.Role]
	return &p, nil
}

type principalKey struct{}

func principalFrom(ctx context.Context) *Principal {
	p, _ := ctx.Value(principalKey{}).(*Principal)
	return p
}

// Require wraps a handler so it runs only for a signed-in admin holding perm.
// An empty perm requires only a valid session.
func (a *Auth) Require(perm Permission, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := a.authenticate(r.Context(), bearer(r))
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		if p == nil {
			httpx.Fail(w, r, httpx.ErrUnauthorized)
			return
		}
		if perm != "" && !p.Can(perm) {
			httpx.Fail(w, r, httpx.ErrForbidden)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
	})
}

func bearer(r *http.Request) string {
	scheme, tok, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(tok)
}
