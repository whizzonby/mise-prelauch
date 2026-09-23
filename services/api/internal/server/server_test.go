package server_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/admin"
	"mise.tt/api/internal/notifications"
	"mise.tt/api/internal/platform/config"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/jobs"
	"mise.tt/api/internal/platform/mail"
	"mise.tt/api/internal/platform/ratelimit"
	"mise.tt/api/internal/server"
)

// These tests exercise the real HTTP handler against a real PostgreSQL
// database. They are skipped unless TEST_DATABASE_URL is set, e.g.
//
//	TEST_DATABASE_URL=postgres://mise:mise@localhost:5460/mise_test?sslmode=disable

type captureMailer struct {
	mu   sync.Mutex
	sent []mail.Message
}

func (c *captureMailer) Send(_ context.Context, msg mail.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, msg)
	return nil
}

// lastTo returns the most recent message sent to an address.
func (c *captureMailer) lastTo(t *testing.T, to string) mail.Message {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := len(c.sent) - 1; i >= 0; i-- {
		if c.sent[i].To == to {
			return c.sent[i]
		}
	}
	t.Fatalf("no email was sent to %s (%d sent in total)", to, len(c.sent))
	return mail.Message{}
}

func (c *captureMailer) countTo(to string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, m := range c.sent {
		if m.To == to {
			n++
		}
	}
	return n
}

type harness struct {
	t       *testing.T
	pool    *pgxpool.Pool
	handler http.Handler
	runner  *jobs.Runner
	mailer  *captureMailer
	cfg     config.Config
}

func openLimits() server.Limits {
	big := func() *ratelimit.Limiter { return ratelimit.New(1_000_000, time.Minute, 1_000_000) }
	return server.Limits{Signup: big(), Token: big(), Lookup: big(), Events: big(), AdminLogin: big(), General: big()}
}

func newHarness(t *testing.T, mutate ...func(*config.Config, *server.Limits)) *harness {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping database tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, url)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE leads, jobs, lead_events, admin_users, audit_log RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	cfg := config.Config{
		Env:                       "local",
		PublicSiteURL:             "http://site.test",
		CORSOrigins:               []string{"http://site.test"},
		TokenSigningKey:           []byte("test-signing-key-test-signing-key-123"),
		EmailVerificationRequired: true,
		ConsentVersion:            "test",
		AdminSessionTTL:           time.Hour,
	}
	limits := openLimits()
	for _, m := range mutate {
		m(&cfg, &limits)
	}

	mailer := &captureMailer{}
	runner := jobs.NewRunner(pool)
	(&notifications.Sender{DB: pool, Mailer: mailer, Links: server.NewLeadService(cfg, pool), SiteURL: cfg.PublicSiteURL}).Register(runner)

	return &harness{t: t, pool: pool, handler: server.New(cfg, pool, limits), runner: runner, mailer: mailer, cfg: cfg}
}

// drainJobs runs queued jobs until none are ready, like the worker would.
func (h *harness) drainJobs() {
	h.t.Helper()
	for {
		worked, err := h.runner.RunOne(context.Background())
		if err != nil {
			h.t.Fatalf("run job: %v", err)
		}
		if !worked {
			return
		}
	}
}

type response struct {
	Status int
	Header http.Header
	Raw    []byte
	Data   map[string]any
	Error  struct {
		Code   string
		Fields []struct{ Field, Code string }
	}
}

type reqOpt func(*http.Request)

func fromIP(ip string) reqOpt { return func(r *http.Request) { r.RemoteAddr = ip + ":40000" } }
func bearer(tok string) reqOpt {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+tok) }
}

func (h *harness) do(method, path string, body any, opts ...reqOpt) response {
	h.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			h.t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.RemoteAddr = "198.51.100.1:40000"
	for _, o := range opts {
		o(req)
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)

	res := response{Status: rec.Code, Header: rec.Header(), Raw: rec.Body.Bytes()}
	if strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		var env struct {
			Data  json.RawMessage `json:"data"`
			Error json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(res.Raw, &env); err != nil {
			h.t.Fatalf("%s %s: response is not JSON: %s", method, path, res.Raw)
		}
		if len(env.Data) > 0 {
			_ = json.Unmarshal(env.Data, &res.Data)
		}
		if len(env.Error) > 0 {
			_ = json.Unmarshal(env.Error, &res.Error)
		}
	}
	return res
}

func signupBody(email string, extra map[string]any) map[string]any {
	body := map[string]any{
		"first_name":        "Asha",
		"email":             email,
		"location":          "port-of-spain",
		"dietary_interests": []string{"vegetarian"},
		"household_size":    3,
		"consent":           true,
		"elapsed_ms":        8000,
	}
	for k, v := range extra {
		body[k] = v
	}
	return body
}

func (h *harness) wantStatus(res response, want int) {
	h.t.Helper()
	if res.Status != want {
		h.t.Fatalf("status = %d, want %d; body: %s", res.Status, want, res.Raw)
	}
}

var tokenInURL = regexp.MustCompile(`token=([A-Za-z0-9_.\-]+)`)

// verifyTokenFor runs the worker and pulls the verification token out of the
// email, exactly as a person following the link would.
func (h *harness) verifyTokenFor(email string) string {
	h.t.Helper()
	h.drainJobs()
	msg := h.mailer.lastTo(h.t, email)
	idx := strings.Index(msg.Text, "/verify?")
	if idx < 0 {
		h.t.Fatalf("latest email to %s has no verification link; subject %q", email, msg.Subject)
	}
	m := tokenInURL.FindStringSubmatch(msg.Text[idx:])
	if m == nil {
		h.t.Fatal("verification link has no token")
	}
	return m[1]
}

// join signs a person up and confirms their email. It returns the lead
// summary and their profile token.
func (h *harness) join(email string, extra map[string]any, opts ...reqOpt) (map[string]any, string) {
	h.t.Helper()
	res := h.do("POST", "/api/v1/leads", signupBody(email, extra), opts...)
	h.wantStatus(res, http.StatusCreated)
	res = h.do("POST", "/api/v1/leads/verify", map[string]string{"token": h.verifyTokenFor(email)})
	h.wantStatus(res, http.StatusOK)
	return res.Data["lead"].(map[string]any), res.Data["profile_token"].(string)
}

func (h *harness) leadStatus(email string) string {
	h.t.Helper()
	var status string
	if err := h.pool.QueryRow(context.Background(), `SELECT status FROM leads WHERE email = $1`, email).Scan(&status); err != nil {
		h.t.Fatalf("load lead %s: %v", email, err)
	}
	return status
}

func (h *harness) count(query string, args ...any) int {
	h.t.Helper()
	var n int
	if err := h.pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		h.t.Fatalf("count: %v", err)
	}
	return n
}

func TestHealthAndReady(t *testing.T) {
	h := newHarness(t)
	h.wantStatus(h.do("GET", "/api/v1/health", nil), http.StatusOK)
	res := h.do("GET", "/api/v1/ready", nil)
	h.wantStatus(res, http.StatusOK)
	if res.Header.Get("X-Request-Id") == "" {
		t.Error("response has no X-Request-Id")
	}
	h.wantStatus(h.do("GET", "/api/v1/nope", nil), http.StatusNotFound)
}

func TestSignupVerifyAndWelcome(t *testing.T) {
	h := newHarness(t)

	res := h.do("POST", "/api/v1/leads", signupBody("Asha@Example.com", map[string]any{
		"phone": "+1 (868) 555-0100",
		"attribution": map[string]any{
			"first":  map[string]any{"utm_source": "instagram", "utm_campaign": "launch", "landing_page": "/"},
			"latest": map[string]any{"utm_source": "newsletter", "landing_page": "/#waitlist"},
		},
	}))
	h.wantStatus(res, http.StatusCreated)
	if res.Data["outcome"] != "created" {
		t.Fatalf("outcome = %v", res.Data["outcome"])
	}
	lead := res.Data["lead"].(map[string]any)
	if lead["status"] != "PENDING" || lead["verification_required"] != true {
		t.Fatalf("new lead = %v; want PENDING and verification required", lead)
	}
	code := lead["referral_code"].(string)
	if lead["referral_url"] != "http://site.test/join/"+code {
		t.Errorf("referral_url = %v", lead["referral_url"])
	}
	if res.Data["profile_token"] == "" {
		t.Error("no profile token returned")
	}
	if got := h.leadStatus("asha@example.com"); got != "PENDING" {
		t.Fatalf("stored status = %s", got)
	}
	if n := h.count(`SELECT count(*) FROM lead_attribution a JOIN leads l ON l.id = a.lead_id WHERE l.email = 'asha@example.com'`); n != 2 {
		t.Errorf("attribution rows = %d, want 2 (first and latest)", n)
	}
	var phone string
	_ = h.pool.QueryRow(context.Background(), `SELECT phone FROM leads WHERE email = 'asha@example.com'`).Scan(&phone)
	if phone != "+18685550100" {
		t.Errorf("phone = %q, want normalised +18685550100", phone)
	}

	tok := h.verifyTokenFor("asha@example.com")
	res = h.do("POST", "/api/v1/leads/verify", map[string]string{"token": tok})
	h.wantStatus(res, http.StatusOK)
	if got := h.leadStatus("asha@example.com"); got != "VERIFIED" {
		t.Fatalf("status after verify = %s", got)
	}

	// Following the link a second time is fine and sends nothing new.
	h.drainJobs()
	sent := h.mailer.countTo("asha@example.com")
	h.wantStatus(h.do("POST", "/api/v1/leads/verify", map[string]string{"token": tok}), http.StatusOK)
	h.drainJobs()
	if got := h.mailer.countTo("asha@example.com"); got != sent {
		t.Errorf("second verify sent %d more email(s)", got-sent)
	}

	welcome := h.mailer.lastTo(t, "asha@example.com")
	if welcome.Subject != "You're on the Mise list" {
		t.Errorf("welcome subject = %q", welcome.Subject)
	}
	if !strings.Contains(welcome.Text, "/join/"+code) || !strings.Contains(welcome.HTML, "/join/"+code) {
		t.Error("welcome email is missing the referral link")
	}
	if !strings.Contains(welcome.Headers["List-Unsubscribe"], "/unsubscribe?token=") {
		t.Error("welcome email has no List-Unsubscribe header")
	}

	h.wantStatus(h.do("POST", "/api/v1/leads/verify", map[string]string{"token": "garbage"}), http.StatusBadRequest)
}

func TestDuplicateSignup(t *testing.T) {
	h := newHarness(t)
	h.join("sam.cooper@gmail.com", nil)
	h.drainJobs()
	before := h.mailer.countTo("sam.cooper@gmail.com")

	// Same inbox written three other ways.
	for _, variant := range []string{"sam.cooper@gmail.com", "SamCooper+mise@gmail.com", "s.a.m.cooper@googlemail.com"} {
		res := h.do("POST", "/api/v1/leads", signupBody(variant, nil))
		h.wantStatus(res, http.StatusOK)
		if res.Data["outcome"] != "already_on_list" {
			t.Fatalf("%s: outcome = %v", variant, res.Data["outcome"])
		}
		if res.Data["lead"] != nil || res.Data["profile_token"] != nil {
			t.Fatalf("%s: duplicate response leaked lead data: %s", variant, res.Raw)
		}
	}
	if n := h.count(`SELECT count(*) FROM leads`); n != 1 {
		t.Fatalf("leads = %d, want 1", n)
	}

	// The real owner is told, once: repeats inside ten minutes are suppressed.
	h.drainJobs()
	if got := h.mailer.countTo("sam.cooper@gmail.com") - before; got != 1 {
		t.Errorf("duplicate signups sent %d emails, want 1", got)
	}
	if msg := h.mailer.lastTo(t, "sam.cooper@gmail.com"); msg.Subject != "You're already on the Mise list" {
		t.Errorf("subject = %q", msg.Subject)
	}
}

func TestSignupValidation(t *testing.T) {
	h := newHarness(t)
	res := h.do("POST", "/api/v1/leads", map[string]any{
		"first_name":        "Visit http://evil.example",
		"email":             "not-an-email",
		"phone":             "call me",
		"location":          "",
		"dietary_interests": []string{"Not A Slug"},
		"household_size":    40,
		"consent":           false,
		"elapsed_ms":        8000,
	})
	h.wantStatus(res, http.StatusUnprocessableEntity)
	if res.Error.Code != "validation_failed" {
		t.Fatalf("error code = %q", res.Error.Code)
	}
	got := map[string]bool{}
	for _, f := range res.Error.Fields {
		got[f.Field] = true
	}
	for _, field := range []string{"first_name", "email", "phone", "location", "dietary_interests", "household_size", "consent"} {
		if !got[field] {
			t.Errorf("no validation error for %s", field)
		}
	}
	if n := h.count(`SELECT count(*) FROM leads`); n != 0 {
		t.Errorf("invalid signup created %d lead(s)", n)
	}

	// Malformed and unexpected payloads.
	h.wantStatus(h.do("POST", "/api/v1/leads", map[string]any{"unexpected": 1}), http.StatusBadRequest)
	req := httptest.NewRequest("POST", "/api/v1/leads", strings.NewReader("first_name=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("form post status = %d, want 415", rec.Code)
	}
}

func TestBotChecks(t *testing.T) {
	h := newHarness(t)
	for name, extra := range map[string]map[string]any{
		"honeypot filled": {"website": "http://spam.example"},
		"submitted fast":  {"elapsed_ms": 200},
	} {
		res := h.do("POST", "/api/v1/leads", signupBody("bot@example.com", extra))
		if res.Status != http.StatusUnprocessableEntity || res.Error.Code != "signup_rejected" {
			t.Errorf("%s: status %d code %q, want 422 signup_rejected", name, res.Status, res.Error.Code)
		}
	}
	if n := h.count(`SELECT count(*) FROM leads`); n != 0 {
		t.Errorf("bot signups created %d lead(s)", n)
	}
}

func TestSignupRateLimit(t *testing.T) {
	h := newHarness(t, func(_ *config.Config, l *server.Limits) {
		l.Signup = ratelimit.New(2, time.Hour, 2)
	})
	for i, want := range []int{http.StatusCreated, http.StatusCreated, http.StatusTooManyRequests} {
		res := h.do("POST", "/api/v1/leads", signupBody("person"+string(rune('a'+i))+"@example.com", nil))
		if res.Status != want {
			t.Fatalf("signup %d: status = %d, want %d", i+1, res.Status, want)
		}
	}
	// A different address has its own budget.
	res := h.do("POST", "/api/v1/leads", signupBody("other@example.com", nil), fromIP("203.0.113.77"))
	h.wantStatus(res, http.StatusCreated)
}

func TestReferralCountsOnlyVerifiedFriends(t *testing.T) {
	h := newHarness(t)
	asha, ashaToken := h.join("asha@example.com", nil, fromIP("198.51.100.10"))
	code := asha["referral_code"].(string)

	// The invitation page can greet the friend by the referrer's first name.
	res := h.do("GET", "/api/v1/referrals/"+strings.ToLower(code), nil)
	h.wantStatus(res, http.StatusOK)
	if res.Data["referrer_first_name"] != "Asha" {
		t.Errorf("referrer_first_name = %v", res.Data["referrer_first_name"])
	}
	h.wantStatus(h.do("GET", "/api/v1/referrals/ZZZZZZZ", nil), http.StatusNotFound)

	counts := func() (pending, converted float64) {
		res := h.do("GET", "/api/v1/leads/me", nil, bearer(ashaToken))
		h.wantStatus(res, http.StatusOK)
		r := res.Data["referrals"].(map[string]any)
		return r["pending"].(float64), r["converted"].(float64)
	}

	// Ben signs up with Asha's code but has not confirmed his email yet.
	res = h.do("POST", "/api/v1/leads", signupBody("ben@example.com", map[string]any{"first_name": "Ben", "referral_code": code}), fromIP("198.51.100.20"))
	h.wantStatus(res, http.StatusCreated)
	if p, c := counts(); p != 1 || c != 0 {
		t.Fatalf("before Ben verifies: pending=%v converted=%v, want 1 and 0", p, c)
	}

	// Once Ben confirms, the referral counts.
	h.wantStatus(h.do("POST", "/api/v1/leads/verify", map[string]string{"token": h.verifyTokenFor("ben@example.com")}), http.StatusOK)
	if p, c := counts(); p != 0 || c != 1 {
		t.Fatalf("after Ben verifies: pending=%v converted=%v, want 0 and 1", p, c)
	}

	// Asha invites "herself" from her own network: recorded, flagged, never counted.
	h.join("asha.second@example.com", map[string]any{"referral_code": code}, fromIP("198.51.100.10"))
	if p, c := counts(); p != 0 || c != 1 {
		t.Fatalf("after same-network signup: pending=%v converted=%v, want 0 and 1", p, c)
	}
	if n := h.count(`SELECT count(*) FROM referrals WHERE status = 'flagged'`); n != 1 {
		t.Errorf("flagged referrals = %d, want 1", n)
	}

	// An unknown code is ignored rather than failing the signup.
	res = h.do("POST", "/api/v1/leads", signupBody("cara@example.com", map[string]any{"referral_code": "NOPE123"}))
	h.wantStatus(res, http.StatusCreated)
	if n := h.count(`SELECT count(*) FROM referrals`); n != 2 {
		t.Errorf("referrals = %d, want 2", n)
	}
}

func TestReferralMilestoneEmail(t *testing.T) {
	h := newHarness(t, func(c *config.Config, _ *server.Limits) { c.ReferralMilestones = []int{2} })
	asha, _ := h.join("asha@example.com", nil, fromIP("198.51.100.10"))
	code := asha["referral_code"].(string)
	h.join("ben@example.com", map[string]any{"referral_code": code}, fromIP("198.51.100.20"))
	h.drainJobs()
	if msg := h.mailer.lastTo(t, "asha@example.com"); strings.Contains(msg.Subject, "friends have joined") {
		t.Fatal("milestone email sent after one referral; milestone is two")
	}
	h.join("cara@example.com", map[string]any{"referral_code": code}, fromIP("198.51.100.30"))
	h.drainJobs()
	if msg := h.mailer.lastTo(t, "asha@example.com"); msg.Subject != "2 friends have joined Mise through you" {
		t.Fatalf("subject = %q, want the milestone email", msg.Subject)
	}
}

func TestVerificationDisabled(t *testing.T) {
	h := newHarness(t, func(c *config.Config, _ *server.Limits) { c.EmailVerificationRequired = false })
	res := h.do("POST", "/api/v1/leads", signupBody("dana@example.com", nil))
	h.wantStatus(res, http.StatusCreated)
	lead := res.Data["lead"].(map[string]any)
	if lead["status"] != "VERIFIED" || lead["verification_required"] != false {
		t.Fatalf("lead = %v; want VERIFIED without verification", lead)
	}
	h.drainJobs()
	if msg := h.mailer.lastTo(t, "dana@example.com"); msg.Subject != "You're on the Mise list" {
		t.Errorf("subject = %q", msg.Subject)
	}
}

func TestPreferences(t *testing.T) {
	h := newHarness(t)
	_, tok := h.join("asha@example.com", nil)

	h.wantStatus(h.do("PATCH", "/api/v1/leads/preferences", map[string]any{"meals_per_week": 3}), http.StatusUnauthorized)
	h.wantStatus(h.do("PATCH", "/api/v1/leads/preferences", map[string]any{"meals_per_week": 3}, bearer("forged.token")), http.StatusUnauthorized)

	res := h.do("PATCH", "/api/v1/leads/preferences", map[string]any{"meals_per_week": 99}, bearer(tok))
	h.wantStatus(res, http.StatusUnprocessableEntity)

	res = h.do("PATCH", "/api/v1/leads/preferences", map[string]any{
		"meals_per_week":    3,
		"cooking_frequency": "most-days",
		"meal_interests":    []string{"caribbean-classics", "fitness"},
		"household_type":    "family",
	}, bearer(tok))
	h.wantStatus(res, http.StatusOK)
	if res.Data["profile_completed"] != true {
		t.Errorf("profile_completed = %v", res.Data["profile_completed"])
	}

	var meals, household int
	var dietary, interests []string
	var householdType string
	err := h.pool.QueryRow(context.Background(), `
		SELECT meals_per_week, household_size, dietary_preferences, meal_interests, metadata->>'household_type'
		FROM lead_preferences`).Scan(&meals, &household, &dietary, &interests, &householdType)
	if err != nil {
		t.Fatal(err)
	}
	// Answers given at signup survive a partial update.
	if meals != 3 || household != 3 || len(dietary) != 1 || dietary[0] != "vegetarian" || len(interests) != 2 || householdType != "family" {
		t.Errorf("stored preferences: meals=%d household=%d dietary=%v interests=%v type=%q", meals, household, dietary, interests, householdType)
	}
}

func TestUnsubscribeStopsEmail(t *testing.T) {
	h := newHarness(t)
	h.join("asha@example.com", nil)
	h.drainJobs()

	welcome := h.mailer.lastTo(t, "asha@example.com")
	unsub := tokenInURL.FindStringSubmatch(welcome.Headers["List-Unsubscribe"])
	if unsub == nil {
		t.Fatal("no unsubscribe token in List-Unsubscribe header")
	}
	// A verification token must not work as an unsubscribe token.
	h.wantStatus(h.do("POST", "/api/v1/leads/unsubscribe", map[string]string{"token": "abc.def"}), http.StatusBadRequest)
	h.wantStatus(h.do("POST", "/api/v1/leads/unsubscribe", map[string]string{"token": unsub[1]}), http.StatusOK)
	if got := h.leadStatus("asha@example.com"); got != "UNSUBSCRIBED" {
		t.Fatalf("status = %s, want UNSUBSCRIBED", got)
	}

	// A queued email for an unsubscribed lead is dropped, not sent.
	sent := h.mailer.countTo("asha@example.com")
	var id string
	_ = h.pool.QueryRow(context.Background(), `SELECT id FROM leads`).Scan(&id)
	if err := jobs.Enqueue(context.Background(), h.pool, "email.welcome", map[string]string{"lead_id": id}); err != nil {
		t.Fatal(err)
	}
	h.drainJobs()
	if got := h.mailer.countTo("asha@example.com"); got != sent {
		t.Errorf("an unsubscribed lead was sent %d email(s)", got-sent)
	}

	// Signing up again with consent is a fresh opt-in.
	h.wantStatus(h.do("POST", "/api/v1/leads", signupBody("asha@example.com", nil)), http.StatusOK)
	if got := h.leadStatus("asha@example.com"); got != "VERIFIED" {
		t.Errorf("status after re-signup = %s, want VERIFIED", got)
	}
}

func TestEvents(t *testing.T) {
	h := newHarness(t)
	batch := func(events ...map[string]any) map[string]any {
		return map[string]any{"anonymous_id": "anon-12345678", "session_id": "sess-12345678", "events": events}
	}

	res := h.do("POST", "/api/v1/events", batch(
		map[string]any{"type": "page_view", "metadata": map[string]any{"path": "/"}},
		map[string]any{"type": "hero_cta_clicked"},
	))
	h.wantStatus(res, http.StatusAccepted)
	if n := h.count(`SELECT count(*) FROM lead_events WHERE anonymous_id = 'anon-12345678' AND lead_id IS NULL`); n != 2 {
		t.Fatalf("stored events = %d, want 2", n)
	}

	h.wantStatus(h.do("POST", "/api/v1/events", batch(map[string]any{"type": "keystroke"})), http.StatusUnprocessableEntity)
	h.wantStatus(h.do("POST", "/api/v1/events", batch(map[string]any{
		"type": "page_view", "metadata": map[string]any{"nested": map[string]any{"email": "x@example.com"}},
	})), http.StatusUnprocessableEntity)

	// Signing up links the visitor's earlier anonymous events to the new lead.
	res = h.do("POST", "/api/v1/leads", signupBody("asha@example.com", map[string]any{"anonymous_id": "anon-12345678"}))
	h.wantStatus(res, http.StatusCreated)
	if n := h.count(`SELECT count(*) FROM lead_events e JOIN leads l ON l.id = e.lead_id WHERE l.email = 'asha@example.com'`); n != 2 {
		t.Errorf("events linked to the lead = %d, want 2", n)
	}

	// Events sent with a profile token are attached to the lead directly.
	res = h.do("POST", "/api/v1/events", batch(map[string]any{"type": "referral_link_copied"}), bearer(res.Data["profile_token"].(string)))
	h.wantStatus(res, http.StatusAccepted)
	if n := h.count(`SELECT count(*) FROM lead_events WHERE event_type = 'referral_link_copied' AND lead_id IS NOT NULL`); n != 1 {
		t.Errorf("authenticated event not linked to lead")
	}
}

func TestCORS(t *testing.T) {
	h := newHarness(t)
	preflight := func(origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("OPTIONS", "/api/v1/leads", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", "POST")
		rec := httptest.NewRecorder()
		h.handler.ServeHTTP(rec, req)
		return rec
	}
	if got := preflight("http://site.test").Header().Get("Access-Control-Allow-Origin"); got != "http://site.test" {
		t.Errorf("allowed origin: Access-Control-Allow-Origin = %q", got)
	}
	if got := preflight("http://evil.test").Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("disallowed origin was granted: %q", got)
	}
}

func (h *harness) adminLogin(email, role string) string {
	h.t.Helper()
	const password = "correct horse battery staple"
	auth := admin.NewAuth(h.pool, time.Hour)
	if err := auth.CreateUser(context.Background(), email, "Test Admin", role, password); err != nil {
		h.t.Fatalf("create admin: %v", err)
	}
	res := h.do("POST", "/api/v1/admin/auth/login", map[string]string{"email": email, "password": password})
	h.wantStatus(res, http.StatusOK)
	return res.Data["token"].(string)
}

func TestAdminAuth(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{"/api/v1/admin/leads", "/api/v1/admin/stats/overview", "/api/v1/admin/leads/export.csv", "/api/v1/admin/auth/me"} {
		h.wantStatus(h.do("GET", path, nil), http.StatusUnauthorized)
		h.wantStatus(h.do("GET", path, nil, bearer("made-up-token")), http.StatusUnauthorized)
	}

	tok := h.adminLogin("admin@mise.test", "admin")
	res := h.do("GET", "/api/v1/admin/auth/me", nil, bearer(tok))
	h.wantStatus(res, http.StatusOK)
	if res.Data["email"] != "admin@mise.test" || res.Data["role"] != "admin" {
		t.Errorf("me = %v", res.Data)
	}

	res = h.do("POST", "/api/v1/admin/auth/login", map[string]string{"email": "admin@mise.test", "password": "wrong password entirely"})
	h.wantStatus(res, http.StatusUnauthorized)
	res = h.do("POST", "/api/v1/admin/auth/login", map[string]string{"email": "nobody@mise.test", "password": "wrong password entirely"})
	h.wantStatus(res, http.StatusUnauthorized)

	// A lead's profile token is not an admin credential.
	_, leadToken := h.join("asha@example.com", nil)
	h.wantStatus(h.do("GET", "/api/v1/admin/leads", nil, bearer(leadToken)), http.StatusUnauthorized)

	h.wantStatus(h.do("POST", "/api/v1/admin/auth/logout", nil, bearer(tok)), http.StatusOK)
	h.wantStatus(h.do("GET", "/api/v1/admin/auth/me", nil, bearer(tok)), http.StatusUnauthorized)

	// The session token is stored hashed.
	if n := h.count(`SELECT count(*) FROM admin_sessions WHERE token_hash = convert_to($1, 'UTF8')`, tok); n != 0 {
		t.Error("raw session token found in the database")
	}
}

func TestAdminLeadsStatsExportAndErase(t *testing.T) {
	h := newHarness(t)
	asha, _ := h.join("asha@example.com", map[string]any{
		"attribution": map[string]any{"first": map[string]any{"utm_source": "instagram"}},
	}, fromIP("198.51.100.10"))
	h.join("ben@example.com", map[string]any{
		"first_name": "Ben", "location": "san-fernando", "referral_code": asha["referral_code"], "dietary_interests": []string{"high-protein"},
	}, fromIP("198.51.100.20"))
	res := h.do("POST", "/api/v1/leads", signupBody("=cmd@example.com", map[string]any{"first_name": "Cara", "household_size": 1}))
	h.wantStatus(res, http.StatusCreated)

	tok := h.adminLogin("admin@mise.test", "admin")
	auth := bearer(tok)

	res = h.do("GET", "/api/v1/admin/stats/overview", nil, auth)
	h.wantStatus(res, http.StatusOK)
	for key, want := range map[string]float64{"total_leads": 3, "verified_leads": 2, "pending_leads": 1, "referral_conversions": 1, "referred_signups": 1} {
		if res.Data[key] != want {
			t.Errorf("overview %s = %v, want %v", key, res.Data[key], want)
		}
	}
	if days := res.Data["signups_by_day"].([]any); len(days) != 30 || days[29].(map[string]any)["count"] != float64(3) {
		t.Errorf("signups_by_day: %d days, last = %v; want 30 days ending with today's 3", len(days), days[len(days)-1])
	}
	sources := map[string]float64{}
	for _, b := range res.Data["sources"].([]any) {
		sources[b.(map[string]any)["label"].(string)] = b.(map[string]any)["count"].(float64)
	}
	if sources["instagram"] != 1 || sources["referral"] != 1 || sources["direct"] != 1 {
		t.Errorf("sources = %v", sources)
	}

	list := func(query string) []any {
		res := h.do("GET", "/api/v1/admin/leads"+query, nil, auth)
		h.wantStatus(res, http.StatusOK)
		return res.Data["items"].([]any)
	}
	for query, want := range map[string]int{
		"":                       3,
		"?q=BEN":                 1,
		"?q=example.com":         3,
		"?q=%25":                 0, // a literal percent sign, not a wildcard
		"?status=pending":        1,
		"?location=san-fernando": 1,
		"?source=instagram":      1,
		"?referral=referrer":     1,
		"?referral=referred":     1,
		"?from=2999-01-01":       0,
		"?page_size=2":           2,
	} {
		if got := len(list(query)); got != want {
			t.Errorf("list%s returned %d leads, want %d", query, got, want)
		}
	}
	h.wantStatus(h.do("GET", "/api/v1/admin/leads?status=bogus", nil, auth), http.StatusUnprocessableEntity)

	benID := list("?q=ben")[0].(map[string]any)["id"].(string)
	res = h.do("GET", "/api/v1/admin/leads/"+benID, nil, auth)
	h.wantStatus(res, http.StatusOK)
	if res.Data["referred_by"].(map[string]any)["first_name"] != "Asha" {
		t.Errorf("referred_by = %v", res.Data["referred_by"])
	}
	if res.Data["preferences"].(map[string]any)["dietary_preferences"].([]any)[0] != "high-protein" {
		t.Errorf("preferences = %v", res.Data["preferences"])
	}
	h.wantStatus(h.do("GET", "/api/v1/admin/leads/not-a-uuid", nil, auth), http.StatusNotFound)
	h.wantStatus(h.do("GET", "/api/v1/admin/leads/00000000-0000-0000-0000-000000000000", nil, auth), http.StatusNotFound)

	// Status changes are limited to the hand-set statuses and are audited.
	h.wantStatus(h.do("PATCH", "/api/v1/admin/leads/"+benID, map[string]string{"status": "CONVERTED"}, auth), http.StatusUnprocessableEntity)
	res = h.do("PATCH", "/api/v1/admin/leads/"+benID, map[string]string{"status": "QUALIFIED"}, auth)
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != "QUALIFIED" {
		t.Errorf("status after patch = %v", res.Data["status"])
	}
	if n := h.count(`SELECT count(*) FROM audit_log WHERE action = 'lead.status_changed' AND target_id = $1 AND metadata->>'to' = 'QUALIFIED'`, benID); n != 1 {
		t.Errorf("status change audit entries = %d, want 1", n)
	}

	// Export: CSV, formula-safe, audited.
	res = h.do("GET", "/api/v1/admin/leads/export.csv", nil, auth)
	h.wantStatus(res, http.StatusOK)
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("export content type = %q", ct)
	}
	records, err := csv.NewReader(bytes.NewReader(res.Raw)).ReadAll()
	if err != nil {
		t.Fatalf("export is not valid CSV: %v", err)
	}
	if len(records) != 4 {
		t.Fatalf("export has %d rows, want header + 3", len(records))
	}
	emailCol := -1
	for i, name := range records[0] {
		if name == "email" {
			emailCol = i
		}
	}
	foundSafe := false
	for _, rec := range records[1:] {
		if strings.HasPrefix(rec[emailCol], "=") {
			t.Errorf("export cell %q could run as a spreadsheet formula", rec[emailCol])
		}
		if rec[emailCol] == "'=cmd@example.com" {
			foundSafe = true
		}
	}
	if !foundSafe {
		t.Error("formula-leading cell was not neutralised")
	}
	if n := h.count(`SELECT count(*) FROM audit_log WHERE action = 'leads.exported' AND (metadata->>'rows')::int = 3`); n != 1 {
		t.Errorf("export audit entries = %d, want 1", n)
	}

	// Permissions: support can read leads but cannot export or erase.
	support := bearer(h.adminLogin("support@mise.test", "support"))
	h.wantStatus(h.do("GET", "/api/v1/admin/leads", nil, support), http.StatusOK)
	h.wantStatus(h.do("GET", "/api/v1/admin/leads/export.csv", nil, support), http.StatusForbidden)
	h.wantStatus(h.do("DELETE", "/api/v1/admin/leads/"+benID, nil, support), http.StatusForbidden)
	h.wantStatus(h.do("GET", "/api/v1/admin/stats/overview", nil, support), http.StatusForbidden)

	// Erasure removes the lead and everything that hangs off it.
	h.wantStatus(h.do("DELETE", "/api/v1/admin/leads/"+benID, nil, auth), http.StatusOK)
	h.wantStatus(h.do("GET", "/api/v1/admin/leads/"+benID, nil, auth), http.StatusNotFound)
	for table, query := range map[string]string{
		"lead_preferences": `SELECT count(*) FROM lead_preferences WHERE lead_id = $1`,
		"referrals":        `SELECT count(*) FROM referrals WHERE referred_lead_id = $1`,
		"audit_log":        `SELECT count(*) FROM audit_log WHERE target_id = $1::text`,
	} {
		if n := h.count(query, benID); n != 0 {
			t.Errorf("%s still has %d row(s) for the erased lead", table, n)
		}
	}
	if n := h.count(`SELECT count(*) FROM audit_log WHERE action = 'lead.erased'`); n != 1 {
		t.Errorf("erasure audit entries = %d, want 1", n)
	}
}
