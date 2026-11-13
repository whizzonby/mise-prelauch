package server_test

import (
	"bufio"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/config"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/ratelimit"
	"mise.tt/api/internal/server"
)

// Integration tests for Sprint 1 identity: customer accounts, admin MFA,
// database RBAC, sessions, the lead link and the admin-account migration.
// Like the rest of this package they run against a real PostgreSQL.

const customerPassword = "plantain and saltfish 2026"

func registerBody(email string) map[string]any {
	return map[string]any{
		"email": email, "password": customerPassword,
		"first_name": "Kavita", "last_name": "Ramdass", "accept_terms": true,
	}
}

var anyToken = regexp.MustCompile(`token=([A-Za-z0-9_\-]+)`)

// accountLink runs the worker and returns the token from the latest email to
// an address whose link goes to path ("/verify-email", "/reset-password").
func (h *harness) accountLink(email, path string) string {
	h.t.Helper()
	h.drainJobs()
	msg := h.mailer.lastTo(h.t, email)
	idx := strings.Index(msg.Text, path+"?")
	if idx < 0 {
		h.t.Fatalf("latest email to %s has no %s link; subject %q", email, path, msg.Subject)
	}
	m := anyToken.FindStringSubmatch(msg.Text[idx:])
	if m == nil {
		h.t.Fatalf("%s link has no token", path)
	}
	return m[1]
}

// customer registers, verifies and signs in, returning the session token.
func (h *harness) customer(email string) string {
	h.t.Helper()
	h.wantStatus(h.do("POST", "/api/v1/auth/register", registerBody(email)), http.StatusAccepted)
	h.wantStatus(h.do("POST", "/api/v1/auth/verify-email", map[string]string{"token": h.accountLink(email, "/verify-email")}), http.StatusOK)
	return h.customerLogin(email, customerPassword)
}

func (h *harness) customerLogin(email, password string) string {
	h.t.Helper()
	res := h.do("POST", "/api/v1/auth/login", map[string]string{"email": email, "password": password})
	h.wantStatus(res, http.StatusOK)
	return res.Data["token"].(string)
}

func (h *harness) wantError(res response, status int, code string) {
	h.t.Helper()
	h.wantStatus(res, status)
	if res.Error.Code != code {
		h.t.Fatalf("error code = %q, want %q; body: %s", res.Error.Code, code, res.Raw)
	}
}

func TestCustomerRegistrationVerificationAndLogin(t *testing.T) {
	h := newHarness(t)
	const email = "kavita@example.com"

	h.wantStatus(h.do("POST", "/api/v1/auth/register", registerBody(email)), http.StatusAccepted)
	if n := h.count(`SELECT count(*) FROM users WHERE email = $1 AND email_verified_at IS NULL`, email); n != 1 {
		t.Fatalf("unverified users = %d, want 1", n)
	}
	if n := h.count(`SELECT count(*) FROM customers c JOIN users u ON u.id = c.user_id WHERE u.email = $1 AND c.terms_version = 'test-terms'`, email); n != 1 {
		t.Fatal("customer record with the terms version was not created")
	}

	// Signing in before confirming the address is refused, and a new link is sent.
	res := h.do("POST", "/api/v1/auth/login", map[string]string{"email": email, "password": customerPassword})
	h.wantError(res, http.StatusForbidden, "email_not_verified")

	tok := h.accountLink(email, "/verify-email")
	if n := h.count(`SELECT count(*) FROM auth_tokens WHERE consumed_at IS NULL AND purpose = 'verify_email'`); n != 1 {
		t.Errorf("open verification tokens = %d, want 1 (a new link replaces the old)", n)
	}
	h.wantStatus(h.do("POST", "/api/v1/auth/verify-email", map[string]string{"token": tok}), http.StatusOK)
	h.wantError(h.do("POST", "/api/v1/auth/verify-email", map[string]string{"token": tok}), http.StatusBadRequest, "token_invalid")

	// The raw token is never stored.
	if n := h.count(`SELECT count(*) FROM auth_tokens WHERE token_hash = convert_to($1, 'UTF8')`, tok); n != 0 {
		t.Error("raw email token found in the database")
	}

	session := h.customerLogin(email, customerPassword)
	res = h.do("GET", "/api/v1/me", nil, bearer(session))
	h.wantStatus(res, http.StatusOK)
	if res.Data["email"] != email || res.Data["email_verified"] != true || res.Data["first_name"] != "Kavita" {
		t.Errorf("me = %v", res.Data)
	}

	h.wantError(h.do("POST", "/api/v1/auth/login", map[string]string{"email": email, "password": "not the password at all"}),
		http.StatusUnauthorized, "invalid_credentials")
	h.wantError(h.do("POST", "/api/v1/auth/login", map[string]string{"email": "nobody@example.com", "password": customerPassword}),
		http.StatusUnauthorized, "invalid_credentials")

	h.wantStatus(h.do("POST", "/api/v1/auth/logout", nil, bearer(session)), http.StatusOK)
	h.wantStatus(h.do("GET", "/api/v1/me", nil, bearer(session)), http.StatusUnauthorized)
}

func TestRegistrationDoesNotRevealExistingAccounts(t *testing.T) {
	h := newHarness(t)
	h.customer("kavita@example.com")

	// Same inbox, different spelling: same answer as a new address, no new
	// account, and the owner is told by email instead.
	res := h.do("POST", "/api/v1/auth/register", registerBody("Kavita+again@Example.com"))
	h.wantStatus(res, http.StatusAccepted)
	if res.Data["status"] != "check_email" {
		t.Errorf("register answer = %v", res.Data)
	}
	if n := h.count(`SELECT count(*) FROM users`); n != 1 {
		t.Errorf("users = %d, want 1", n)
	}
	h.drainJobs()
	if msg := h.mailer.lastTo(t, "kavita@example.com"); !strings.Contains(msg.Subject, "already have") {
		t.Errorf("latest email subject = %q", msg.Subject)
	}
}

func TestRegistrationValidationAndClosedRegistration(t *testing.T) {
	h := newHarness(t)
	res := h.do("POST", "/api/v1/auth/register", map[string]any{
		"email": "not-an-email", "password": "short", "first_name": "", "last_name": "R", "accept_terms": false,
	})
	h.wantError(res, http.StatusUnprocessableEntity, "validation_failed")
	var fields []string
	for _, f := range res.Error.Fields {
		fields = append(fields, f.Field)
	}
	for _, want := range []string{"email", "password", "first_name", "accept_terms"} {
		if !slices.Contains(fields, want) {
			t.Errorf("no error for %s; got %v", want, fields)
		}
	}

	closed := newHarness(t, func(c *config.Config, _ *server.Limits) { c.CustomerRegistrationEnabled = false })
	closed.wantError(closed.do("POST", "/api/v1/auth/register", registerBody("kavita@example.com")), http.StatusForbidden, "registration_closed")
	if n := closed.count(`SELECT count(*) FROM users`); n != 0 {
		t.Errorf("users = %d after a closed registration", n)
	}
}

func TestVerifiedAccountIsLinkedToItsLead(t *testing.T) {
	h := newHarness(t)

	// Two people on the waitlist; the second was invited by the first, from a
	// campaign, so there is referral and attribution history to preserve.
	referrer, _ := h.join("ravi@example.com", nil)
	h.join("asha@example.com", map[string]any{
		"referral_code": referrer["referral_code"],
		"attribution":   map[string]any{"first": map[string]any{"utm_source": "instagram", "utm_campaign": "prelaunch"}},
	}, fromIP("203.0.113.9"))
	var leadID, statusBefore string
	if err := h.pool.QueryRow(context.Background(), `SELECT id, status FROM leads WHERE email = 'asha@example.com'`).Scan(&leadID, &statusBefore); err != nil {
		t.Fatal(err)
	}

	// Registering is not enough: the link waits for the inbox to be proved.
	h.wantStatus(h.do("POST", "/api/v1/auth/register", registerBody("Asha+mise@example.com")), http.StatusAccepted)
	if n := h.count(`SELECT count(*) FROM customers WHERE lead_id IS NOT NULL`); n != 0 {
		t.Fatal("lead linked before the email was verified")
	}
	h.wantStatus(h.do("POST", "/api/v1/auth/verify-email", map[string]string{"token": h.accountLink("asha+mise@example.com", "/verify-email")}), http.StatusOK)

	if n := h.count(`SELECT count(*) FROM customers WHERE lead_id = $1 AND lead_linked_at IS NOT NULL`, leadID); n != 1 {
		t.Fatal("verified account was not linked to its lead")
	}
	// The lead itself is untouched, its history kept and extended.
	var statusAfter string
	_ = h.pool.QueryRow(context.Background(), `SELECT status FROM leads WHERE id = $1`, leadID).Scan(&statusAfter)
	if statusAfter != statusBefore {
		t.Errorf("lead status changed from %s to %s", statusBefore, statusAfter)
	}
	if n := h.count(`SELECT count(*) FROM referrals WHERE referred_lead_id = $1 AND status = 'converted'`, leadID); n != 1 {
		t.Error("referral attribution lost")
	}
	if n := h.count(`SELECT count(*) FROM lead_attribution WHERE lead_id = $1 AND utm_source = 'instagram'`, leadID); n != 1 {
		t.Error("marketing source lost")
	}
	if n := h.count(`SELECT count(*) FROM lead_events WHERE lead_id = $1 AND event_type = 'account.linked'`, leadID); n != 1 {
		t.Error("no account.linked event on the lead")
	}

	// The admin lead page shows the account.
	adm := bearer(h.adminLogin("admin@mise.test", "admin"))
	res := h.do("GET", "/api/v1/admin/leads/"+leadID, nil, adm)
	h.wantStatus(res, http.StatusOK)
	if res.Data["account"] == nil {
		t.Error("admin lead detail does not show the linked account")
	}

	// Erasing the lead clears the link and keeps the customer.
	h.wantStatus(h.do("DELETE", "/api/v1/admin/leads/"+leadID, nil, adm), http.StatusOK)
	if n := h.count(`SELECT count(*) FROM customers WHERE lead_id IS NULL`); n != 1 {
		t.Error("customer was not kept, or still points at the erased lead")
	}
}

func TestBlockedLeadIsNotLinked(t *testing.T) {
	h := newHarness(t)
	h.join("asha@example.com", nil)
	if _, err := h.pool.Exec(context.Background(), `UPDATE leads SET status = 'BLOCKED' WHERE email = 'asha@example.com'`); err != nil {
		t.Fatal(err)
	}
	h.customer("asha@example.com")
	if n := h.count(`SELECT count(*) FROM customers WHERE lead_id IS NOT NULL`); n != 0 {
		t.Error("account was linked to a blocked lead")
	}
}

func TestPasswordResetEndsEverySession(t *testing.T) {
	h := newHarness(t)
	const email = "kavita@example.com"
	first := h.customer(email)
	second := h.customerLogin(email, customerPassword)

	h.wantStatus(h.do("POST", "/api/v1/auth/password/forgot", map[string]string{"email": "KAVITA@example.com"}), http.StatusAccepted)
	tok := h.accountLink(email, "/reset-password")

	h.wantError(h.do("POST", "/api/v1/auth/password/reset", map[string]string{"token": tok, "password": "short"}),
		http.StatusUnprocessableEntity, "validation_failed")
	const newPassword = "callaloo on a sunday 99"
	h.wantStatus(h.do("POST", "/api/v1/auth/password/reset", map[string]string{"token": tok, "password": newPassword}), http.StatusOK)
	h.wantError(h.do("POST", "/api/v1/auth/password/reset", map[string]string{"token": tok, "password": newPassword}),
		http.StatusBadRequest, "token_invalid")

	for _, s := range []string{first, second} {
		h.wantStatus(h.do("GET", "/api/v1/me", nil, bearer(s)), http.StatusUnauthorized)
	}
	h.wantStatus(h.do("POST", "/api/v1/auth/login", map[string]string{"email": email, "password": customerPassword}), http.StatusUnauthorized)
	h.customerLogin(email, newPassword)
}

func TestPasswordResetIsNotOfferedToStaffOrUnknownAddresses(t *testing.T) {
	h := newHarness(t)
	h.adminLogin("admin@mise.test", "admin")
	for _, email := range []string{"admin@mise.test", "nobody@example.com"} {
		h.wantStatus(h.do("POST", "/api/v1/auth/password/forgot", map[string]string{"email": email}), http.StatusAccepted)
	}
	h.drainJobs()
	if n := h.mailer.countTo("admin@mise.test") + h.mailer.countTo("nobody@example.com"); n != 0 {
		t.Errorf("%d reset emails sent; staff are reset by an operator and unknown addresses get nothing", n)
	}
}

func TestCustomerSessionsListAndRevoke(t *testing.T) {
	h := newHarness(t)
	phone := h.customer("kavita@example.com")
	laptop := h.customerLogin("kavita@example.com", customerPassword)
	tablet := h.customerLogin("kavita@example.com", customerPassword)

	res := h.do("GET", "/api/v1/me/sessions", nil, bearer(laptop))
	h.wantStatus(res, http.StatusOK)
	items := res.Data["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("sessions = %d, want 3", len(items))
	}
	var phoneID string
	current := 0
	for _, it := range items {
		s := it.(map[string]any)
		if s["current"] == true {
			current++
		}
	}
	if current != 1 {
		t.Errorf("%d sessions marked current, want 1", current)
	}
	if err := h.pool.QueryRow(context.Background(), `SELECT id FROM sessions WHERE token_hash = sha256(convert_to($1, 'UTF8'))`, phone).Scan(&phoneID); err != nil {
		t.Fatal(err)
	}

	// Revoke one session from another.
	h.wantStatus(h.do("DELETE", "/api/v1/me/sessions/"+phoneID, nil, bearer(laptop)), http.StatusOK)
	h.wantStatus(h.do("GET", "/api/v1/me", nil, bearer(phone)), http.StatusUnauthorized)

	// Nobody can end someone else's session.
	other := h.customer("ravi@example.com")
	var tabletID string
	_ = h.pool.QueryRow(context.Background(), `SELECT id FROM sessions WHERE token_hash = sha256(convert_to($1, 'UTF8'))`, tablet).Scan(&tabletID)
	h.wantStatus(h.do("DELETE", "/api/v1/me/sessions/"+tabletID, nil, bearer(other)), http.StatusNotFound)
	h.wantStatus(h.do("GET", "/api/v1/me", nil, bearer(tablet)), http.StatusOK)

	// Sign out everywhere else.
	h.wantStatus(h.do("DELETE", "/api/v1/me/sessions", nil, bearer(laptop)), http.StatusOK)
	h.wantStatus(h.do("GET", "/api/v1/me", nil, bearer(tablet)), http.StatusUnauthorized)
	h.wantStatus(h.do("GET", "/api/v1/me", nil, bearer(laptop)), http.StatusOK)
}

func TestSessionsEndWhenIdleOrExpired(t *testing.T) {
	h := newHarness(t)
	idle := h.customer("kavita@example.com")
	expired := h.customerLogin("kavita@example.com", customerPassword)
	ctx := context.Background()
	if _, err := h.pool.Exec(ctx, `UPDATE sessions SET last_seen_at = now() - interval '25 hours' WHERE token_hash = sha256(convert_to($1, 'UTF8'))`, idle); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(ctx, `UPDATE sessions SET expires_at = now() - interval '1 second' WHERE token_hash = sha256(convert_to($1, 'UTF8'))`, expired); err != nil {
		t.Fatal(err)
	}
	h.wantStatus(h.do("GET", "/api/v1/me", nil, bearer(idle)), http.StatusUnauthorized)
	h.wantStatus(h.do("GET", "/api/v1/me", nil, bearer(expired)), http.StatusUnauthorized)
}

func TestSessionAudiencesAreSeparate(t *testing.T) {
	h := newHarness(t)
	cust := h.customer("kavita@example.com")
	adm := h.adminLogin("admin@mise.test", "admin")

	// A customer session never reaches the admin API, and the reverse.
	h.wantStatus(h.do("GET", "/api/v1/admin/leads", nil, bearer(cust)), http.StatusUnauthorized)
	h.wantStatus(h.do("GET", "/api/v1/admin/auth/me", nil, bearer(cust)), http.StatusUnauthorized)
	h.wantStatus(h.do("GET", "/api/v1/me", nil, bearer(adm)), http.StatusUnauthorized)

	// A customer's password does not open the admin console.
	h.wantError(h.do("POST", "/api/v1/admin/auth/login", map[string]string{"email": "kavita@example.com", "password": customerPassword}),
		http.StatusUnauthorized, "invalid_credentials")
	// And staff without a customer role cannot use the customer sign-in.
	h.wantError(h.do("POST", "/api/v1/auth/login", map[string]string{"email": "admin@mise.test", "password": "correct horse battery staple"}),
		http.StatusUnauthorized, "invalid_credentials")
}

func TestAdminPermissionsComeFromTheDatabase(t *testing.T) {
	h := newHarness(t)
	support := bearer(h.adminLogin("support@mise.test", "support"))
	h.wantStatus(h.do("GET", "/api/v1/admin/leads", nil, support), http.StatusOK)
	h.wantStatus(h.do("GET", "/api/v1/admin/leads/export.csv", nil, support), http.StatusForbidden)
	h.wantStatus(h.do("GET", "/api/v1/admin/stats/overview", nil, support), http.StatusForbidden)

	res := h.do("GET", "/api/v1/admin/auth/me", nil, support)
	h.wantStatus(res, http.StatusOK)
	if res.Data["role"] != "support" || res.Data["mfa"] != "not_enrolled" {
		t.Errorf("me = %v", res.Data)
	}

	// Granting a role widens access on the next request, without signing in again.
	if err := h.ids.GrantRole(context.Background(), "support@mise.test", "marketing"); err != nil {
		t.Fatal(err)
	}
	h.wantStatus(h.do("GET", "/api/v1/admin/leads/export.csv", nil, support), http.StatusOK)

	// Removing every staff role closes the live session at once.
	for _, role := range []string{"support", "marketing"} {
		if err := h.ids.RevokeRole(context.Background(), "support@mise.test", role); err != nil {
			t.Fatal(err)
		}
	}
	h.wantStatus(h.do("GET", "/api/v1/admin/leads", nil, support), http.StatusUnauthorized)
}

func TestScopedRolesDoNotGrantGlobalAccess(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	// A farmer holds no admin.access, so the admin console is closed to them.
	// (Chefs gained console access with the Sprint 5 recipe workspace.)
	if err := h.ids.UpsertStaff(ctx, "farmer@mise.test", "Farmer", "farmer", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	login := func() int {
		return h.do("POST", "/api/v1/admin/auth/login", map[string]string{"email": "farmer@mise.test", "password": "correct horse battery staple"}).Status
	}
	if got := login(); got != http.StatusUnauthorized {
		t.Fatalf("farmer admin login = %d, want 401", got)
	}
	// An admin role held only inside one kitchen grants nothing globally.
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO user_roles (user_id, role_key, scope_type, scope_id)
		SELECT id, 'admin', 'kitchen', gen_random_uuid() FROM users WHERE email = 'farmer@mise.test'`); err != nil {
		t.Fatal(err)
	}
	if got := login(); got != http.StatusUnauthorized {
		t.Errorf("scoped admin role opened the console: %d", got)
	}
}

func TestDisabledAccountsAreSignedOut(t *testing.T) {
	h := newHarness(t)
	adm := bearer(h.adminLogin("admin@mise.test", "admin"))
	if err := h.ids.SetDisabled(context.Background(), "admin@mise.test", true); err != nil {
		t.Fatal(err)
	}
	h.wantStatus(h.do("GET", "/api/v1/admin/leads", nil, adm), http.StatusUnauthorized)
	h.wantStatus(h.do("POST", "/api/v1/admin/auth/login", map[string]string{"email": "admin@mise.test", "password": "correct horse battery staple"}), http.StatusUnauthorized)
}

func TestPermissionsInCodeExistInTheDatabase(t *testing.T) {
	h := newHarness(t)
	missing, err := h.ids.MissingPermissions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) > 0 {
		t.Errorf("permissions checked in code but not seeded by a migration: %v", missing)
	}
}

// ---------------------------------------------------------------- MFA

func mfaHarness(t *testing.T) *harness {
	return newHarness(t, func(c *config.Config, _ *server.Limits) { c.AdminMFARequired = true })
}

func (h *harness) staffPasswordLogin(email string) response {
	h.t.Helper()
	res := h.do("POST", "/api/v1/admin/auth/login", map[string]string{"email": email, "password": "correct horse battery staple"})
	h.wantStatus(res, http.StatusOK)
	return res
}

func TestAdminMFAEnrolmentThenChallenge(t *testing.T) {
	h := mfaHarness(t)
	ctx := context.Background()
	if err := h.ids.UpsertStaff(ctx, "admin@mise.test", "Ada", "admin", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}

	// First sign-in: the password is right, but nothing works until MFA is set up.
	res := h.staffPasswordLogin("admin@mise.test")
	if res.Data["mfa"] != "enrolment_required" {
		t.Fatalf("mfa = %v, want enrolment_required", res.Data["mfa"])
	}
	pending := res.Data["token"].(string)
	if exp := res.Data["expires_in"].(float64); exp > 600 {
		t.Errorf("a session waiting for MFA lasts %vs; want at most 10 minutes", exp)
	}
	h.wantError(h.do("GET", "/api/v1/admin/leads", nil, bearer(pending)), http.StatusForbidden, "mfa_required")
	h.wantStatus(h.do("GET", "/api/v1/admin/auth/me", nil, bearer(pending)), http.StatusOK)

	res = h.do("POST", "/api/v1/admin/auth/mfa/setup", nil, bearer(pending))
	h.wantStatus(res, http.StatusOK)
	secret := res.Data["secret"].(string)
	// Asking again (a page reload) gives the same secret, so a scanned QR code stays valid.
	if again := h.do("POST", "/api/v1/admin/auth/mfa/setup", nil, bearer(pending)); again.Data["secret"] != secret {
		t.Fatalf("setup changed the secret on a second call: %v then %v", secret, again.Data["secret"])
	}
	if !strings.HasPrefix(res.Data["otpauth_url"].(string), "otpauth://totp/Mise:") || res.Data["qr_png_base64"] == "" {
		t.Errorf("setup = %v", res.Data)
	}
	// The secret is stored sealed, never in the clear.
	if n := h.count(`SELECT count(*) FROM mfa_totp WHERE position(convert_to($1, 'UTF8') in secret_ciphertext) > 0`, secret); n != 0 {
		t.Error("TOTP secret stored in the clear")
	}

	h.wantError(h.do("POST", "/api/v1/admin/auth/mfa/confirm", map[string]string{"code": "000000"}, bearer(pending)), http.StatusUnauthorized, "mfa_code_invalid")
	code, _ := totp.GenerateCode(secret, time.Now())
	res = h.do("POST", "/api/v1/admin/auth/mfa/confirm", map[string]string{"code": code}, bearer(pending))
	h.wantStatus(res, http.StatusOK)
	full := res.Data["token"].(string)
	if res.Data["mfa"] != "verified" || full == pending {
		t.Fatalf("confirm = %v", res.Data)
	}
	// The token from before the second factor no longer works.
	h.wantStatus(h.do("GET", "/api/v1/admin/auth/me", nil, bearer(pending)), http.StatusUnauthorized)
	h.wantStatus(h.do("GET", "/api/v1/admin/leads", nil, bearer(full)), http.StatusOK)
	h.wantError(h.do("POST", "/api/v1/admin/auth/mfa/setup", nil, bearer(full)), http.StatusConflict, "mfa_already_enrolled")

	// Next sign-in asks for a code. The code just used cannot be replayed.
	res = h.staffPasswordLogin("admin@mise.test")
	if res.Data["mfa"] != "challenge" {
		t.Fatalf("mfa = %v, want challenge", res.Data["mfa"])
	}
	challenge := res.Data["token"].(string)
	h.wantError(h.do("POST", "/api/v1/admin/auth/mfa/verify", map[string]string{"code": code}, bearer(challenge)), http.StatusUnauthorized, "mfa_code_invalid")
	next, _ := totp.GenerateCode(secret, time.Now().Add(30*time.Second))
	res = h.do("POST", "/api/v1/admin/auth/mfa/verify", map[string]string{"code": next}, bearer(challenge))
	h.wantStatus(res, http.StatusOK)
	h.wantStatus(h.do("GET", "/api/v1/admin/stats/overview", nil, bearer(res.Data["token"].(string))), http.StatusOK)

	if n := h.count(`SELECT count(*) FROM audit_log WHERE action IN ('auth.mfa_enrolled', 'auth.mfa_verified')`); n != 2 {
		t.Errorf("MFA audit entries = %d, want 2", n)
	}
}

func TestAdminMFALocksAfterRepeatedWrongCodes(t *testing.T) {
	h := mfaHarness(t)
	ctx := context.Background()
	if err := h.ids.UpsertStaff(ctx, "admin@mise.test", "Ada", "admin", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	pending := h.staffPasswordLogin("admin@mise.test").Data["token"].(string)
	secret := h.do("POST", "/api/v1/admin/auth/mfa/setup", nil, bearer(pending)).Data["secret"].(string)
	code, _ := totp.GenerateCode(secret, time.Now())
	h.wantStatus(h.do("POST", "/api/v1/admin/auth/mfa/confirm", map[string]string{"code": code}, bearer(pending)), http.StatusOK)

	challenge := h.staffPasswordLogin("admin@mise.test").Data["token"].(string)
	for i := 0; i < 5; i++ {
		h.wantError(h.do("POST", "/api/v1/admin/auth/mfa/verify", map[string]string{"code": "123456"}, bearer(challenge)), http.StatusUnauthorized, "mfa_code_invalid")
	}
	h.wantError(h.do("POST", "/api/v1/admin/auth/mfa/verify", map[string]string{"code": "123456"}, bearer(challenge)), http.StatusUnauthorized, "mfa_locked")
	h.wantStatus(h.do("GET", "/api/v1/admin/auth/me", nil, bearer(challenge)), http.StatusUnauthorized)

	// An operator resets a lost authenticator; the person enrols again.
	if err := h.ids.ResetMFA(ctx, "admin@mise.test"); err != nil {
		t.Fatal(err)
	}
	if got := h.staffPasswordLogin("admin@mise.test").Data["mfa"]; got != "enrolment_required" {
		t.Errorf("after reset mfa = %v", got)
	}
}

// ---------------------------------------------------------------- BFF forwarding

func TestBFFForwardedClientAddress(t *testing.T) {
	const key = "bff-test-key-bff-test-key-bff-test-key"
	h := newHarness(t, func(c *config.Config, l *server.Limits) {
		c.BFFSharedKey = key
		l.AdminLogin = ratelimit.New(2, time.Hour, 2)
	})
	login := func(ip string, withKey bool) int {
		body := map[string]string{"email": "x" + ip + "@example.com", "password": "wrong password, wrong password"}
		return h.do("POST", "/api/v1/auth/login", body, func(r *http.Request) {
			r.RemoteAddr = "10.0.0.5:4000" // the Next.js server
			r.Header.Set("X-Mise-Client-Ip", ip)
			if withKey {
				r.Header.Set("X-Mise-Bff-Key", key)
			}
		}).Status
	}
	// With the key, each end user has their own budget.
	for _, ip := range []string{"198.51.100.10", "198.51.100.11"} {
		for i := 0; i < 2; i++ {
			if got := login(ip, true); got != http.StatusUnauthorized {
				t.Fatalf("%s attempt %d: status %d, want 401", ip, i+1, got)
			}
		}
	}
	// Without the key the header is ignored: all of it is the server's address.
	if got := login("198.51.100.12", false); got != http.StatusUnauthorized {
		t.Fatalf("first unkeyed attempt: %d", got)
	}
	if got := login("198.51.100.13", false); got != http.StatusUnauthorized {
		t.Fatalf("second unkeyed attempt: %d", got)
	}
	if got := login("198.51.100.14", false); got != http.StatusTooManyRequests {
		t.Fatalf("third unkeyed attempt from the same server: %d, want 429", got)
	}
}

// ---------------------------------------------------------------- migration

// TestMigrationCopiesPrelaunchAdmins loads admins into the pre-launch schema
// (version 3), applies 00004, and checks every admin keeps their password,
// role and audit history, and that rolling back leaves admin_users intact.
func TestMigrationCopiesPrelaunchAdmins(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping database tests")
	}
	ctx := context.Background()
	setup, err := db.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer setup.Close()
	if _, err := setup.Exec(ctx, `DROP SCHEMA IF EXISTS migration_test CASCADE; CREATE SCHEMA migration_test`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = setup.Exec(context.Background(), `DROP SCHEMA IF EXISTS migration_test CASCADE`) })

	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	pool, err := db.Open(ctx, url+sep+"search_path=migration_test")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := db.MigrateTo(ctx, pool, 3); err != nil {
		t.Fatalf("migrate to 3: %v", err)
	}

	hash, err := identity.HashPassword("pre-launch admin password")
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`INSERT INTO admin_users (id, email, name, role, password_hash) VALUES
			('11111111-1111-1111-1111-111111111111', 'owner@mise.tt', 'Owner', 'super_admin', $1),
			('22222222-2222-2222-2222-222222222222', 'm.arketing+team@gmail.com', 'Marketing', 'marketing', $1)`,
		`INSERT INTO admin_users (id, email, name, role, password_hash, disabled_at) VALUES
			('33333333-3333-3333-3333-333333333333', 'former@mise.tt', 'Former', 'support', $1, now())`,
	} {
		if _, err := pool.Exec(ctx, stmt, hash); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO audit_log (actor_admin_id, action) VALUES ('11111111-1111-1111-1111-111111111111', 'leads.exported')`); err != nil {
		t.Fatal(err)
	}

	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	count := func(q string, args ...any) int {
		var n int
		if err := pool.QueryRow(ctx, q, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return n
	}
	if n := count(`SELECT count(*) FROM users u JOIN admin_users a ON a.id = u.id AND a.email = u.email`); n != 3 {
		t.Errorf("admins copied with their ids = %d, want 3", n)
	}
	if n := count(`SELECT count(*) FROM users WHERE email_canonical = 'marketing@gmail.com'`); n != 1 {
		t.Error("canonical email in SQL does not match the Go rule")
	}
	if n := count(`SELECT count(*) FROM users WHERE id = '33333333-3333-3333-3333-333333333333' AND status = 'disabled'`); n != 1 {
		t.Error("disabled admin was not copied as disabled")
	}
	if n := count(`SELECT count(*) FROM user_roles ur JOIN admin_users a ON a.id = ur.user_id AND a.role = ur.role_key`); n != 3 {
		t.Errorf("roles copied = %d, want 3", n)
	}
	if n := count(`SELECT count(*) FROM audit_log WHERE actor_user_id = '11111111-1111-1111-1111-111111111111'`); n != 1 {
		t.Error("audit history not attributed to the copied user")
	}

	// The copied hash still verifies the old password.
	var copied string
	if err := pool.QueryRow(ctx, `SELECT password_hash FROM password_credentials WHERE user_id = '11111111-1111-1111-1111-111111111111'`).Scan(&copied); err != nil {
		t.Fatal(err)
	}
	if !identity.VerifyPassword(copied, "pre-launch admin password") {
		t.Error("migrated admin's password no longer verifies")
	}

	// Rolling back to the pre-launch schema (00003) removes only what 00004
	// and later added.
	for {
		v, err := db.MigrationVersion(ctx, pool)
		if err != nil {
			t.Fatalf("migration version: %v", err)
		}
		if v <= 3 {
			break
		}
		if err := db.MigrateDown(ctx, pool); err != nil {
			t.Fatalf("migrate down from %d: %v", v, err)
		}
	}
	if n := count(`SELECT count(*) FROM admin_users`); n != 3 {
		t.Errorf("admin_users after rollback = %d, want 3", n)
	}
	if n := count(`SELECT count(*) FROM information_schema.tables WHERE table_schema = 'migration_test' AND table_name = 'users'`); n != 0 {
		t.Error("users table survived the rollback")
	}
}

// ---------------------------------------------------------------- contract

// TestRoutesMatchOpenAPI fails when a route is added without documenting it
// in docs/api/openapi.yaml, or documented without existing.
func TestRoutesMatchOpenAPI(t *testing.T) {
	h := newHarness(t)
	f, err := os.Open(filepath.Join("..", "..", "..", "..", "docs", "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("open contract: %v", err)
	}
	defer f.Close()

	pathLine := regexp.MustCompile(`^  (/api/v1/[^:]*):\s*$`)
	methodLine := regexp.MustCompile(`^    (get|post|put|patch|delete):\s*$`)
	var documented []string
	inPaths, current := false, ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "paths:":
			inPaths = true
		case inPaths && len(line) > 0 && line[0] != ' ' && line[0] != '#':
			inPaths = false
		case inPaths:
			if m := pathLine.FindStringSubmatch(line); m != nil {
				current = m[1]
			} else if m := methodLine.FindStringSubmatch(line); m != nil && current != "" {
				documented = append(documented, strings.ToUpper(m[1])+" "+current)
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}

	for _, r := range h.routes {
		if !slices.Contains(documented, r) {
			t.Errorf("route %q is served but not in openapi.yaml", r)
		}
	}
	for _, d := range documented {
		if !slices.Contains(h.routes, d) {
			t.Errorf("openapi.yaml documents %q, which the server does not serve", d)
		}
	}
}
