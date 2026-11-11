package identity

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"

	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/ratelimit"
)

const maxBody = 8 << 10

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// ---------------------------------------------------------------- middleware

type principalKey struct{}

// PrincipalFrom returns the caller set by a Require* middleware.
func PrincipalFrom(ctx context.Context) *Principal {
	p, _ := ctx.Value(principalKey{}).(*Principal)
	return p
}

// WithPrincipal is for tests of handlers that sit behind Require*.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// BearerToken reads "Authorization: Bearer <token>".
func BearerToken(r *http.Request) string {
	scheme, tok, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(tok)
}

func (s *Service) require(audience Audience, allowIncompleteMFA bool, perm Permission, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := s.Authenticate(r.Context(), BearerToken(r), audience)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		if p == nil {
			httpx.Fail(w, r, httpx.ErrUnauthorized)
			return
		}
		if !allowIncompleteMFA && !p.MFA.Complete() {
			httpx.Fail(w, r, ErrMFARequired)
			return
		}
		if perm != "" && !p.Can(perm) {
			httpx.Fail(w, r, httpx.ErrForbidden)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
	})
}

// RequireCustomer admits a signed-in customer.
func (s *Service) RequireCustomer(next http.HandlerFunc) http.Handler {
	return s.require(AudienceCustomer, false, "", next)
}

// RequireAdmin admits a staff member who has finished MFA (where required)
// and holds perm. An empty perm needs only a complete admin session.
func (s *Service) RequireAdmin(perm Permission, next http.HandlerFunc) http.Handler {
	return s.require(AudienceAdmin, false, perm, next)
}

// requireAdminSession admits any admin session, including one that still has
// to present or set up its second factor. Only the MFA, me and logout
// endpoints use it.
func (s *Service) requireAdminSession(next http.HandlerFunc) http.Handler {
	return s.require(AudienceAdmin, true, "", next)
}

func clientMeta(r *http.Request) ClientMeta {
	return ClientMeta{IP: httpx.ClientIP(r.Context()), UserAgent: httpx.ClientUserAgent(r.Context())}
}

// ---------------------------------------------------------------- handlers

// Limits are applied per client address by the router.
type Limits struct {
	// Auth guards password, registration and reset-request endpoints.
	Auth func(http.Handler) http.Handler
	// Token guards endpoints that take an emailed token or a TOTP code.
	Token func(http.Handler) http.Handler
}

type Handler struct {
	Service *Service
	// byEmail slows guessing against one account, and stops someone filling
	// one inbox with reset emails, whatever address the requests come from.
	loginByEmail *ratelimit.Limiter
	resetByEmail *ratelimit.Limiter
}

func NewHandler(s *Service) *Handler {
	return &Handler{
		Service:      s,
		loginByEmail: ratelimit.New(10, 15*time.Minute, 5),
		resetByEmail: ratelimit.New(3, time.Hour, 3),
	}
}

// Routes registers the customer account endpoints and the admin sign-in
// endpoints.
func (h *Handler) Routes(mux httpx.Router, l Limits) {
	s := h.Service
	// Customer accounts.
	mux.Handle("POST /api/v1/auth/register", l.Auth(http.HandlerFunc(h.register)))
	mux.Handle("POST /api/v1/auth/verify-email", l.Token(http.HandlerFunc(h.verifyEmail)))
	mux.Handle("POST /api/v1/auth/login", l.Auth(http.HandlerFunc(h.customerLogin)))
	mux.Handle("POST /api/v1/auth/logout", s.RequireCustomer(h.logout))
	mux.Handle("POST /api/v1/auth/password/forgot", l.Auth(http.HandlerFunc(h.forgotPassword)))
	mux.Handle("POST /api/v1/auth/password/reset", l.Token(http.HandlerFunc(h.resetPassword)))
	mux.Handle("GET /api/v1/me", s.RequireCustomer(h.customerMe))
	mux.Handle("GET /api/v1/me/sessions", s.RequireCustomer(h.listSessions))
	mux.Handle("DELETE /api/v1/me/sessions", s.RequireCustomer(h.revokeOtherSessions))
	mux.Handle("DELETE /api/v1/me/sessions/{id}", s.RequireCustomer(h.revokeSession))

	// Admin sign-in. The URLs are the pre-launch ones; MFA is new.
	mux.Handle("POST /api/v1/admin/auth/login", l.Auth(http.HandlerFunc(h.adminLogin)))
	mux.Handle("POST /api/v1/admin/auth/logout", s.requireAdminSession(h.logout))
	mux.Handle("GET /api/v1/admin/auth/me", s.requireAdminSession(h.adminMe))
	mux.Handle("POST /api/v1/admin/auth/mfa/verify", l.Token(s.requireAdminSession(h.mfaVerify)))
	mux.Handle("POST /api/v1/admin/auth/mfa/setup", s.requireAdminSession(h.mfaSetup))
	mux.Handle("POST /api/v1/admin/auth/mfa/confirm", l.Token(s.requireAdminSession(h.mfaConfirm)))
}

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) decodeCredentials(w http.ResponseWriter, r *http.Request) (credentials, bool) {
	var in credentials
	if err := httpx.Decode(w, r, &in, maxBody); err != nil {
		httpx.Fail(w, r, err)
		return in, false
	}
	if !h.loginByEmail.Allow(strings.ToLower(strings.TrimSpace(in.Email))) {
		httpx.Fail(w, r, httpx.ErrRateLimited)
		return in, false
	}
	return in, true
}

// CustomerView is what a customer sees about their own account.
type CustomerView struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	FirstName     string `json:"first_name"`
	LastName      string `json:"last_name"`
	EmailVerified bool   `json:"email_verified"`
}

func customerView(p *Principal) CustomerView {
	return CustomerView{ID: p.UserID, Email: p.Email, FirstName: p.FirstName, LastName: p.LastName, EmailVerified: p.EmailVerified}
}

// AdminView keeps the pre-launch admin principal fields (id, email, name,
// role, permissions) and adds roles and the MFA state.
type AdminView struct {
	ID          string       `json:"id"`
	Email       string       `json:"email"`
	Name        string       `json:"name"`
	Role        string       `json:"role"`
	Roles       []string     `json:"roles"`
	Permissions []Permission `json:"permissions"`
	MFA         MFAState     `json:"mfa"`
}

func adminView(p *Principal) AdminView {
	return AdminView{ID: p.UserID, Email: p.Email, Name: p.Name(), Role: p.PrimaryRole(), Roles: p.Roles, Permissions: p.Permissions, MFA: p.MFA}
}

func expiresIn(g *Grant, now time.Time) int {
	return int(g.ExpiresAt.Sub(now).Seconds())
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var in RegisterInput
	if err := httpx.Decode(w, r, &in, maxBody); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if err := h.Service.Register(r.Context(), in); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// The same answer whether the address was new or already had an account.
	httpx.JSON(w, r, http.StatusAccepted, map[string]string{"status": "check_email"})
}

type tokenInput struct {
	Token string `json:"token"`
}

func (h *Handler) verifyEmail(w http.ResponseWriter, r *http.Request) {
	var in tokenInput
	if err := httpx.Decode(w, r, &in, maxBody); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if err := h.Service.VerifyEmail(r.Context(), in.Token); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, map[string]bool{"verified": true})
}

func (h *Handler) customerLogin(w http.ResponseWriter, r *http.Request) {
	in, ok := h.decodeCredentials(w, r)
	if !ok {
		return
	}
	g, err := h.Service.Login(r.Context(), AudienceCustomer, in.Email, in.Password, clientMeta(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, map[string]any{
		"token":      g.Token,
		"expires_in": expiresIn(g, h.Service.now()),
		"user":       customerView(g.Principal),
	})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if err := h.Service.Logout(r.Context(), PrincipalFrom(r.Context())); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, map[string]bool{"signed_out": true})
}

func (h *Handler) forgotPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	if err := httpx.Decode(w, r, &in, maxBody); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// Over the per-address limit the answer is unchanged and nothing is sent,
	// so the limit itself reveals nothing about the account.
	if h.resetByEmail.Allow(strings.ToLower(strings.TrimSpace(in.Email))) {
		if err := h.Service.RequestPasswordReset(r.Context(), in.Email); err != nil {
			httpx.Fail(w, r, err)
			return
		}
	}
	httpx.JSON(w, r, http.StatusAccepted, map[string]string{"status": "check_email"})
}

func (h *Handler) resetPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := httpx.Decode(w, r, &in, maxBody); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if err := h.Service.ResetPassword(r.Context(), in.Token, in.Password); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, map[string]bool{"password_reset": true})
}

func (h *Handler) customerMe(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, r, http.StatusOK, customerView(PrincipalFrom(r.Context())))
}

func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) {
	list, err := h.Service.ListSessions(r.Context(), PrincipalFrom(r.Context()))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, map[string]any{"items": list})
}

func (h *Handler) revokeSession(w http.ResponseWriter, r *http.Request) {
	id := strings.ToLower(r.PathValue("id"))
	if !uuidPattern.MatchString(id) {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	ok, err := h.Service.RevokeSession(r.Context(), PrincipalFrom(r.Context()), id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if !ok {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	httpx.JSON(w, r, http.StatusOK, map[string]bool{"revoked": true})
}

func (h *Handler) revokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	n, err := h.Service.RevokeOtherSessions(r.Context(), PrincipalFrom(r.Context()))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, map[string]int64{"revoked": n})
}

// ---------------------------------------------------------------- admin

func (h *Handler) adminGrant(w http.ResponseWriter, r *http.Request, g *Grant) {
	httpx.JSON(w, r, http.StatusOK, map[string]any{
		"token":      g.Token,
		"expires_in": expiresIn(g, h.Service.now()),
		"admin":      adminView(g.Principal),
		"mfa":        g.Principal.MFA,
	})
}

func (h *Handler) adminLogin(w http.ResponseWriter, r *http.Request) {
	in, ok := h.decodeCredentials(w, r)
	if !ok {
		return
	}
	g, err := h.Service.Login(r.Context(), AudienceAdmin, in.Email, in.Password, clientMeta(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	h.adminGrant(w, r, g)
}

func (h *Handler) adminMe(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, r, http.StatusOK, adminView(PrincipalFrom(r.Context())))
}

type codeInput struct {
	Code string `json:"code"`
}

func (h *Handler) mfaVerify(w http.ResponseWriter, r *http.Request) {
	var in codeInput
	if err := httpx.Decode(w, r, &in, maxBody); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	g, err := h.Service.VerifyMFA(r.Context(), PrincipalFrom(r.Context()), in.Code)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	h.adminGrant(w, r, g)
}

func (h *Handler) mfaSetup(w http.ResponseWriter, r *http.Request) {
	setup, err := h.Service.StartMFAEnrolment(r.Context(), PrincipalFrom(r.Context()))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, setup)
}

func (h *Handler) mfaConfirm(w http.ResponseWriter, r *http.Request) {
	var in codeInput
	if err := httpx.Decode(w, r, &in, maxBody); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	g, err := h.Service.ConfirmMFAEnrolment(r.Context(), PrincipalFrom(r.Context()), in.Code)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	h.adminGrant(w, r, g)
}
