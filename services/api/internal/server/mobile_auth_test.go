package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// Sprint 10: first-party mobile sign-in for the Flutter customer app.

func (h *harness) mobileLogin(email, installation string) map[string]any {
	h.t.Helper()
	res := h.do("POST", "/api/v1/auth/mobile/login", map[string]any{
		"email": email, "password": customerPassword, "device_name": "Pixel 7", "installation_id": installation,
	})
	h.wantStatus(res, http.StatusOK)
	return res.Data
}

func tokens(grant map[string]any) (access, refresh string) {
	return grant["access_token"].(string), grant["refresh_token"].(string)
}

func TestMobileLoginRefreshRotationAndReuse(t *testing.T) {
	h := newHarness(t)
	h.customer("app-user@example.com")

	g := h.mobileLogin("app-user@example.com", "install-aaaa-1111")
	if g["token_type"] != "Bearer" || g["user"] == nil || g["session_id"] == "" {
		t.Fatalf("grant = %v", g)
	}
	if n := g["expires_in"].(float64); n <= 0 || n > 15*60 {
		t.Fatalf("expires_in = %v, want at most 15 minutes", n)
	}
	access1, refresh1 := tokens(g)
	h.wantStatus(h.do("GET", "/api/v1/me", nil, bearer(access1)), http.StatusOK)

	// The device shows in the customer's session list with its name.
	list := h.do("GET", "/api/v1/me/sessions", nil, bearer(access1))
	h.wantStatus(list, http.StatusOK)
	found := false
	for _, it := range list.Data["items"].([]any) {
		s := it.(map[string]any)
		if s["client"] == "mobile" && s["device_name"] == "Pixel 7" && s["current"] == true {
			found = true
		}
	}
	if !found {
		t.Fatalf("mobile session not listed: %s", list.Raw)
	}

	// Rotation: a new pair; the old access token stops working at once.
	r := h.do("POST", "/api/v1/auth/mobile/refresh", map[string]string{"refresh_token": refresh1})
	h.wantStatus(r, http.StatusOK)
	access2, refresh2 := tokens(r.Data)
	if access2 == access1 || refresh2 == refresh1 || r.Data["session_id"] != g["session_id"] {
		t.Fatalf("refresh did not rotate within the same session: %v", r.Data)
	}
	h.wantStatus(h.do("GET", "/api/v1/me", nil, bearer(access1)), http.StatusUnauthorized)
	h.wantStatus(h.do("GET", "/api/v1/me", nil, bearer(access2)), http.StatusOK)

	// Reuse of the spent refresh token ends the whole device session.
	h.wantError(h.do("POST", "/api/v1/auth/mobile/refresh", map[string]string{"refresh_token": refresh1}), http.StatusUnauthorized, "refresh_invalid")
	h.wantStatus(h.do("GET", "/api/v1/me", nil, bearer(access2)), http.StatusUnauthorized)
	h.wantError(h.do("POST", "/api/v1/auth/mobile/refresh", map[string]string{"refresh_token": refresh2}), http.StatusUnauthorized, "refresh_invalid")
	if n := h.count(`SELECT count(*) FROM sessions WHERE id = $1 AND revoked_reason = 'refresh_reuse'`, g["session_id"]); n != 1 {
		t.Fatalf("session not revoked for reuse (%d)", n)
	}
	if n := h.count(`SELECT count(*) FROM audit_log WHERE action = 'auth.refresh_reuse'`); n != 1 {
		t.Fatalf("reuse not audited (%d)", n)
	}

	// Tokens are stored only as hashes.
	if n := h.count(`SELECT count(*) FROM mobile_refresh_tokens WHERE token_hash = convert_to($1, 'UTF8')`, refresh2); n != 0 {
		t.Fatal("a refresh token is stored in plain text")
	}
}

func TestMobileAccessTokenExpiresAndRefreshes(t *testing.T) {
	h := newHarness(t)
	h.customer("expiry@example.com")
	g := h.mobileLogin("expiry@example.com", "install-bbbb-2222")
	access, refresh := tokens(g)

	if _, err := h.pool.Exec(context.Background(), `UPDATE sessions SET access_expires_at = now() - interval '1 second' WHERE id = $1`, g["session_id"]); err != nil {
		t.Fatal(err)
	}
	h.wantStatus(h.do("GET", "/api/v1/me", nil, bearer(access)), http.StatusUnauthorized)
	r := h.do("POST", "/api/v1/auth/mobile/refresh", map[string]string{"refresh_token": refresh})
	h.wantStatus(r, http.StatusOK)
	access2, _ := tokens(r.Data)
	h.wantStatus(h.do("GET", "/api/v1/me", nil, bearer(access2)), http.StatusOK)

	// An expired refresh token is refused.
	_, refresh2 := tokens(r.Data)
	if _, err := h.pool.Exec(context.Background(), `UPDATE mobile_refresh_tokens SET created_at = now() - interval '1 day', expires_at = now() - interval '1 second' WHERE used_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	h.wantError(h.do("POST", "/api/v1/auth/mobile/refresh", map[string]string{"refresh_token": refresh2}), http.StatusUnauthorized, "refresh_invalid")
}

func TestMobileLogoutAndRevocation(t *testing.T) {
	h := newHarness(t)
	web := h.customer("revoke@example.com")

	// Logout with the refresh token works even without a valid access token.
	a, r := tokens(h.mobileLogin("revoke@example.com", "install-cccc-3333"))
	res := h.do("POST", "/api/v1/auth/mobile/logout", map[string]string{"refresh_token": r})
	h.wantStatus(res, http.StatusOK)
	h.wantStatus(h.do("GET", "/api/v1/me", nil, bearer(a)), http.StatusUnauthorized)
	h.wantError(h.do("POST", "/api/v1/auth/mobile/refresh", map[string]string{"refresh_token": r}), http.StatusUnauthorized, "refresh_invalid")
	// Unknown tokens are already signed out.
	h.wantStatus(h.do("POST", "/api/v1/auth/mobile/logout", map[string]string{"refresh_token": "not-a-token"}), http.StatusOK)

	// "Sign out other devices" from the web ends the phone, refresh included.
	_, r2 := tokens(h.mobileLogin("revoke@example.com", "install-cccc-3333"))
	h.wantStatus(h.do("DELETE", "/api/v1/me/sessions", nil, bearer(web)), http.StatusOK)
	h.wantError(h.do("POST", "/api/v1/auth/mobile/refresh", map[string]string{"refresh_token": r2}), http.StatusUnauthorized, "refresh_invalid")

	// Signing in again on the same installation ends its earlier session.
	a3, _ := tokens(h.mobileLogin("revoke@example.com", "install-dddd-4444"))
	a4, _ := tokens(h.mobileLogin("revoke@example.com", "install-dddd-4444"))
	h.wantStatus(h.do("GET", "/api/v1/me", nil, bearer(a3)), http.StatusUnauthorized)
	h.wantStatus(h.do("GET", "/api/v1/me", nil, bearer(a4)), http.StatusOK)

	// Bearer logout (the web endpoint) also works for an app session.
	h.wantStatus(h.do("POST", "/api/v1/auth/logout", nil, bearer(a4)), http.StatusOK)
	h.wantStatus(h.do("GET", "/api/v1/me", nil, bearer(a4)), http.StatusUnauthorized)
}

func TestMobileLoginFailuresRevealNothing(t *testing.T) {
	h := newHarness(t)
	h.customer("known@example.com")
	wrong := h.do("POST", "/api/v1/auth/mobile/login", map[string]any{"email": "known@example.com", "password": "not the password at all"})
	unknown := h.do("POST", "/api/v1/auth/mobile/login", map[string]any{"email": "nobody@example.com", "password": "not the password at all"})
	h.wantError(wrong, http.StatusUnauthorized, "invalid_credentials")
	h.wantError(unknown, http.StatusUnauthorized, "invalid_credentials")
	if errorMessage(t, wrong.Raw) != errorMessage(t, unknown.Raw) {
		t.Fatalf("messages differ: %s vs %s", wrong.Raw, unknown.Raw)
	}
	bad := h.do("POST", "/api/v1/auth/mobile/login", map[string]any{"email": "known@example.com", "password": customerPassword, "installation_id": "no spaces allowed"})
	h.wantError(bad, http.StatusUnprocessableEntity, "validation_failed")

	// Staff accounts cannot get mobile tokens: the app is customer-only.
	h.adminLogin("staff-mobile@mise.test", "support")
	staff := h.do("POST", "/api/v1/auth/mobile/login", map[string]any{"email": "staff-mobile@mise.test", "password": customerPassword})
	if staff.Status != http.StatusUnauthorized {
		t.Fatalf("staff mobile login status = %d", staff.Status)
	}
}

// Brief §48: mobile customer credentials cannot reach any admin endpoint,
// intelligence and finance included.
func TestMobileTokensCannotReachAdminRoutes(t *testing.T) {
	h := newHarness(t)
	h.customer("nosy-app@example.com")
	access, _ := tokens(h.mobileLogin("nosy-app@example.com", "install-eeee-5555"))
	const fakeID = "00000000-0000-4000-8000-000000000000"
	param := regexp.MustCompile(`\{[^}]+\}`)
	checked := 0
	for _, rt := range h.routes {
		method, path, _ := strings.Cut(rt, " ")
		// Two admin routes take no session at all (a password, or an emailed
		// invitation token): a bearer token is ignored there, not honoured.
		if !strings.HasPrefix(path, "/api/v1/admin/") || path == "/api/v1/admin/auth/login" || path == "/api/v1/admin/auth/invitations/accept" {
			continue
		}
		res := h.do(method, param.ReplaceAllString(path, fakeID), map[string]any{}, bearer(access))
		if res.Status != http.StatusUnauthorized {
			t.Errorf("%s with a mobile customer token: status %d, want 401", rt, res.Status)
		}
		checked++
	}
	if checked < 300 {
		t.Fatalf("checked only %d admin routes", checked)
	}
}

func errorMessage(t *testing.T, raw []byte) string {
	t.Helper()
	var env struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	return env.Error.Message
}
