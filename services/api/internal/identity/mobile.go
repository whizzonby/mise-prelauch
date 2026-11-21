package identity

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/identity/store"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
)

// First-party mobile sign-in (Sprint 10). The Flutter app is not a BFF, so it
// holds tokens itself, in the platform keystore:
//
//   - an access token: the bearer token of a `customer` session with client
//     'mobile', valid for MobileAccessTTL;
//   - a refresh token, valid for MobileRefreshTTL from issue and exchanged
//     exactly once for a new pair (rotation).
//
// Both are stored only as SHA-256 hashes. The session row is the device: it
// shows in /me/sessions with its device name, and revoking it (sign out,
// sign out everywhere, password reset) also ends refresh. Exchanging a refresh
// token that was already exchanged is treated as theft: the session and every
// refresh token of it are revoked, and the caller must sign in again.
//
// Mobile tokens are customer tokens: they can never open an admin route,
// because admin routes authenticate with the admin audience.

const (
	MobileAccessTTL  = 15 * time.Minute
	MobileRefreshTTL = 30 * 24 * time.Hour
	// MobileSessionTTL is the absolute life of one device sign-in.
	MobileSessionTTL = 180 * 24 * time.Hour
)

var (
	errRefreshInvalid = httpx.NewError(http.StatusUnauthorized, "refresh_invalid", "Please sign in again.")
	installationIDRe  = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
)

// MobileDevice describes the installation signing in. Both fields are chosen
// by the app; neither is a hardware identifier.
type MobileDevice struct {
	Name           string
	InstallationID string
}

// MobileGrant is a token pair for the app.
type MobileGrant struct {
	AccessToken      string
	AccessExpiresAt  time.Time
	RefreshToken     string
	RefreshExpiresAt time.Time
	SessionID        string
	Principal        *Principal
}

func cleanDevice(d MobileDevice) (name, installation *string, err error) {
	if n := strings.TrimSpace(d.Name); n != "" {
		if len([]rune(n)) > 80 {
			n = string([]rune(n)[:80])
		}
		name = &n
	}
	if id := strings.TrimSpace(d.InstallationID); id != "" {
		if !installationIDRe.MatchString(id) {
			return nil, nil, httpx.ValidationError([]httpx.FieldError{{Field: "installation_id", Code: "invalid", Message: "Use 8 to 64 letters, digits, - or _."}})
		}
		installation = &id
	}
	return name, installation, nil
}

// MobileLogin checks a customer's password and opens a device session.
// Signing in again on the same installation ends that installation's
// earlier session, so a device never accumulates sign-ins.
func (s *Service) MobileLogin(ctx context.Context, rawEmail, password string, dev MobileDevice, meta ClientMeta) (*MobileGrant, error) {
	name, installation, err := cleanDevice(dev)
	if err != nil {
		return nil, err
	}
	p, err := s.verifyCredentials(ctx, AudienceCustomer, rawEmail, password)
	if err != nil {
		return nil, err
	}
	access, accessHash, err := newToken()
	if err != nil {
		return nil, err
	}
	refresh, refreshHash, err := newToken()
	if err != nil {
		return nil, err
	}
	now := s.now()
	g := &MobileGrant{
		AccessToken: access, AccessExpiresAt: now.Add(MobileAccessTTL),
		RefreshToken: refresh, RefreshExpiresAt: now.Add(MobileRefreshTTL),
		Principal: p,
	}
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		if installation != nil {
			if _, err := q.RevokeInstallationSessions(ctx, store.RevokeInstallationSessionsParams{UserID: p.UserID, InstallationID: installation}); err != nil {
				return err
			}
		}
		id, err := q.CreateMobileSession(ctx, store.CreateMobileSessionParams{
			UserID: p.UserID, TokenHash: accessHash, UserAgent: truncatedUA(meta.UserAgent), IpHash: s.ipHash(meta.IP),
			ExpiresAt: now.Add(MobileSessionTTL), AccessExpiresAt: &g.AccessExpiresAt,
			DeviceName: name, InstallationID: installation,
		})
		if err != nil {
			return err
		}
		if err := q.InsertRefreshToken(ctx, store.InsertRefreshTokenParams{SessionID: id, TokenHash: refreshHash, ExpiresAt: g.RefreshExpiresAt}); err != nil {
			return err
		}
		if err := q.TouchLogin(ctx, p.UserID); err != nil {
			return err
		}
		g.SessionID, p.SessionID = id, id
		return audit.Record(ctx, tx, audit.Entry{
			ActorUserID: p.UserID, Action: "auth.login", TargetType: "user", TargetID: p.UserID,
			Metadata: map[string]any{"audience": string(AudienceCustomer), "client": "mobile"},
		})
	})
	if err != nil {
		return nil, err
	}
	if err := store.New(s.pool).DeleteStaleRefreshTokens(ctx); err != nil {
		slog.WarnContext(ctx, "delete stale refresh tokens", "error", err.Error())
	}
	return g, nil
}

// MobileRefresh exchanges a refresh token for a new pair. The old refresh
// token and the old access token stop working. A spent token presented
// again revokes the whole device session.
func (s *Service) MobileRefresh(ctx context.Context, refreshToken string) (*MobileGrant, error) {
	if refreshToken == "" || len(refreshToken) > 128 {
		return nil, errRefreshInvalid
	}
	access, accessHash, err := newToken()
	if err != nil {
		return nil, err
	}
	refresh, refreshHash, err := newToken()
	if err != nil {
		return nil, err
	}
	now := s.now()
	var g *MobileGrant
	var reused *store.LockRefreshTokenRow
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		rt, err := q.LockRefreshToken(ctx, hashToken(refreshToken))
		if errors.Is(err, pgx.ErrNoRows) {
			return errRefreshInvalid
		}
		if err != nil {
			return err
		}
		if rt.UsedAt != nil {
			// Reuse: someone else may hold the newer token. End the device.
			if err := q.RevokeSessionByID(ctx, store.RevokeSessionByIDParams{ID: rt.SessionID, Reason: ptr("refresh_reuse")}); err != nil {
				return err
			}
			if err := q.RevokeSessionRefreshTokens(ctx, rt.SessionID); err != nil {
				return err
			}
			reused = &rt
			return nil
		}
		if rt.RevokedAt != nil || rt.SessionRevokedAt != nil || !rt.ExpiresAt.After(now) ||
			!rt.SessionExpiresAt.After(now) || rt.UserStatus != "active" {
			return errRefreshInvalid
		}
		if err := q.MarkRefreshTokenUsed(ctx, rt.ID); err != nil {
			return err
		}
		accessExp := now.Add(MobileAccessTTL)
		if accessExp.After(rt.SessionExpiresAt) {
			accessExp = rt.SessionExpiresAt
		}
		refreshExp := now.Add(MobileRefreshTTL)
		if refreshExp.After(rt.SessionExpiresAt) {
			refreshExp = rt.SessionExpiresAt
		}
		n, err := q.RotateMobileAccess(ctx, store.RotateMobileAccessParams{ID: rt.SessionID, TokenHash: accessHash, AccessExpiresAt: &accessExp})
		if err != nil {
			return err
		}
		if n == 0 {
			return errRefreshInvalid
		}
		if err := q.InsertRefreshToken(ctx, store.InsertRefreshTokenParams{SessionID: rt.SessionID, TokenHash: refreshHash, ExpiresAt: refreshExp}); err != nil {
			return err
		}
		g = &MobileGrant{
			AccessToken: access, AccessExpiresAt: accessExp, RefreshToken: refresh, RefreshExpiresAt: refreshExp,
			SessionID: rt.SessionID,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if reused != nil {
		slog.WarnContext(ctx, "refresh token reuse; device session revoked", "event", "auth.refresh_reuse", "session_id", reused.SessionID)
		_ = audit.Record(ctx, s.pool, audit.Entry{
			ActorUserID: reused.UserID, Action: "auth.refresh_reuse", TargetType: "session", TargetID: reused.SessionID,
		})
		return nil, errRefreshInvalid
	}
	return g, nil
}

// MobileLogout ends the device session that owns a refresh token. It works
// with an expired access token and always succeeds from the caller's view:
// an unknown token is already signed out.
func (s *Service) MobileLogout(ctx context.Context, refreshToken string) error {
	if refreshToken == "" || len(refreshToken) > 128 {
		return nil
	}
	var ended *store.LockRefreshTokenRow
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		rt, err := q.LockRefreshToken(ctx, hashToken(refreshToken))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := q.RevokeSessionByID(ctx, store.RevokeSessionByIDParams{ID: rt.SessionID, Reason: ptr("logout")}); err != nil {
			return err
		}
		if err := q.RevokeSessionRefreshTokens(ctx, rt.SessionID); err != nil {
			return err
		}
		ended = &rt
		return nil
	})
	if err != nil || ended == nil {
		return err
	}
	if s.sessionEnded != nil {
		s.sessionEnded(ctx, ended.UserID, ended.SessionID)
	}
	return audit.Record(ctx, s.pool, audit.Entry{
		ActorUserID: ended.UserID, Action: "auth.logout", TargetType: "session", TargetID: ended.SessionID,
		Metadata: map[string]any{"client": "mobile"},
	})
}

// ---------------------------------------------------------------- handlers

type mobileGrantView struct {
	TokenType        string        `json:"token_type"`
	AccessToken      string        `json:"access_token"`
	AccessExpiresAt  time.Time     `json:"access_expires_at"`
	ExpiresIn        int64         `json:"expires_in"`
	RefreshToken     string        `json:"refresh_token"`
	RefreshExpiresAt time.Time     `json:"refresh_expires_at"`
	SessionID        string        `json:"session_id"`
	User             *CustomerView `json:"user,omitempty"`
}

func (h *Handler) mobileGrant(g *MobileGrant) mobileGrantView {
	v := mobileGrantView{
		TokenType: "Bearer", AccessToken: g.AccessToken, AccessExpiresAt: g.AccessExpiresAt,
		ExpiresIn:    int64(g.AccessExpiresAt.Sub(h.Service.now()).Seconds()),
		RefreshToken: g.RefreshToken, RefreshExpiresAt: g.RefreshExpiresAt, SessionID: g.SessionID,
	}
	if g.Principal != nil {
		cv := customerView(g.Principal)
		v.User = &cv
	}
	return v
}

func (h *Handler) mobileLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email          string `json:"email"`
		Password       string `json:"password"`
		DeviceName     string `json:"device_name"`
		InstallationID string `json:"installation_id"`
	}
	if err := httpx.Decode(w, r, &in, maxBody); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if !h.loginByEmail.Allow(strings.ToLower(strings.TrimSpace(in.Email))) {
		httpx.Fail(w, r, httpx.ErrRateLimited)
		return
	}
	g, err := h.Service.MobileLogin(r.Context(), in.Email, in.Password, MobileDevice{Name: in.DeviceName, InstallationID: in.InstallationID}, clientMeta(r))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, r, http.StatusOK, h.mobileGrant(g))
}

func (h *Handler) mobileRefresh(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := httpx.Decode(w, r, &in, maxBody); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	g, err := h.Service.MobileRefresh(r.Context(), in.RefreshToken)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, r, http.StatusOK, h.mobileGrant(g))
}

func (h *Handler) mobileLogout(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := httpx.Decode(w, r, &in, maxBody); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if err := h.Service.MobileLogout(r.Context(), in.RefreshToken); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, map[string]bool{"signed_out": true})
}
