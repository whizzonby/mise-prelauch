package admin

import (
	"encoding/csv"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/leads"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/ratelimit"
)

const maxBody = 8 << 10

// optionPattern matches stored option values such as packaging choices.
var optionPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type Handler struct {
	Pool *pgxpool.Pool
	Auth *Auth
	// loginByEmail slows password guessing against one account, whatever
	// address the attempts come from.
	loginByEmail *ratelimit.Limiter
}

func NewHandler(pool *pgxpool.Pool, auth *Auth) *Handler {
	return &Handler{Pool: pool, Auth: auth, loginByEmail: ratelimit.New(10, 15*time.Minute, 5)}
}

// Routes registers every admin endpoint under /api/v1/admin.
func (h *Handler) Routes(mux *http.ServeMux, loginLimit func(http.Handler) http.Handler) {
	mux.Handle("POST /api/v1/admin/auth/login", loginLimit(http.HandlerFunc(h.login)))
	mux.Handle("POST /api/v1/admin/auth/logout", h.Auth.Require("", h.logout))
	mux.Handle("GET /api/v1/admin/auth/me", h.Auth.Require("", h.me))
	mux.Handle("GET /api/v1/admin/stats/overview", h.Auth.Require(PermStatsRead, h.overview))
	mux.Handle("GET /api/v1/admin/leads", h.Auth.Require(PermLeadsRead, h.list))
	mux.Handle("GET /api/v1/admin/leads/export.csv", h.Auth.Require(PermLeadsExport, h.export))
	mux.Handle("GET /api/v1/admin/leads/{id}", h.Auth.Require(PermLeadsRead, h.detail))
	mux.Handle("PATCH /api/v1/admin/leads/{id}", h.Auth.Require(PermLeadsWrite, h.updateStatus))
	mux.Handle("DELETE /api/v1/admin/leads/{id}", h.Auth.Require(PermLeadsErase, h.erase))
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := httpx.Decode(w, r, &in, maxBody); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if !h.loginByEmail.Allow(strings.ToLower(strings.TrimSpace(in.Email))) {
		httpx.Fail(w, r, httpx.ErrRateLimited)
		return
	}
	tok, principal, err := h.Auth.Login(r.Context(), in.Email, in.Password)
	if err != nil {
		if err == errBadCredentials {
			slog.WarnContext(r.Context(), "admin login failed", "event", "admin.login_failed")
		}
		httpx.Fail(w, r, err)
		return
	}
	if err := audit(r.Context(), h.Pool, principal.ID, "admin.login", "admin_user", principal.ID, nil); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, map[string]any{
		"token":      tok,
		"expires_in": int(h.Auth.sessionTTL.Seconds()),
		"admin":      principal,
	})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if err := h.Auth.Logout(r.Context(), bearer(r)); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, map[string]bool{"signed_out": true})
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, r, http.StatusOK, principalFrom(r.Context()))
}

func (h *Handler) overview(w http.ResponseWriter, r *http.Request) {
	days := 30
	if v := r.URL.Query().Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 7 || n > 180 {
			httpx.Fail(w, r, badParam("days", "days must be between 7 and 180."))
			return
		}
		days = n
	}
	o, err := overview(r.Context(), h.Pool, days)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, o)
}

func badParam(field, message string) error {
	return httpx.ValidationError([]httpx.FieldError{{Field: field, Code: "invalid", Message: message}})
}

// parseFilter reads list/export filters from the query string.
func parseFilter(r *http.Request) (LeadFilter, error) {
	q := r.URL.Query()
	f := LeadFilter{
		Query:    q.Get("q"),
		Status:   strings.ToUpper(q.Get("status")),
		Location: q.Get("location"),
		Source:   q.Get("source"),
		Referral: q.Get("referral"),
		// Packaging is an option value, or "none".
		Packaging: q.Get("packaging"),
		Page:      1,
		PageSize:  25,
	}
	if len(f.Query) > 100 {
		return f, badParam("q", "Search text is too long.")
	}
	if f.Status != "" && !leads.Status(f.Status).Valid() {
		return f, badParam("status", "Unknown status.")
	}
	if f.Packaging != "" && !optionPattern.MatchString(f.Packaging) {
		return f, badParam("packaging", "Unknown packaging option.")
	}
	if f.Referral != "" && f.Referral != "referrer" && f.Referral != "referred" {
		return f, badParam("referral", "referral must be referrer or referred.")
	}
	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.DateOnly, v)
		if err != nil {
			return f, badParam("from", "Use the date format YYYY-MM-DD.")
		}
		f.From = &t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.DateOnly, v)
		if err != nil {
			return f, badParam("to", "Use the date format YYYY-MM-DD.")
		}
		// "to" is inclusive of the whole day.
		end := t.AddDate(0, 0, 1)
		f.To = &end
	}
	if v := q.Get("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100000 {
			return f, badParam("page", "page must be a positive number.")
		}
		f.Page = n
	}
	if v := q.Get("page_size"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			return f, badParam("page_size", "page_size must be between 1 and 100.")
		}
		f.PageSize = n
	}
	return f, nil
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	f, err := parseFilter(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	page, err := listLeads(r.Context(), h.Pool, f)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, page)
}

func leadID(r *http.Request) (string, error) {
	id := strings.ToLower(r.PathValue("id"))
	if !uuidPattern.MatchString(id) {
		return "", httpx.ErrNotFound
	}
	return id, nil
}

func (h *Handler) detail(w http.ResponseWriter, r *http.Request) {
	id, err := leadID(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	d, err := leadDetail(r.Context(), h.Pool, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if d == nil {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	httpx.JSON(w, r, http.StatusOK, d)
}

func (h *Handler) updateStatus(w http.ResponseWriter, r *http.Request) {
	id, err := leadID(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	var in struct {
		Status string `json:"status"`
	}
	if err := httpx.Decode(w, r, &in, maxBody); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	status := leads.Status(strings.ToUpper(in.Status))
	if !adminSettableStatuses[status] {
		httpx.Fail(w, r, badParam("status", "Status must be VERIFIED, QUALIFIED, UNSUBSCRIBED or BLOCKED."))
		return
	}
	actor := principalFrom(r.Context())
	found := false
	err = db.InTx(r.Context(), h.Pool, func(tx pgx.Tx) error {
		previous, ok, err := setLeadStatus(r.Context(), tx, id, status)
		if err != nil || !ok {
			return err
		}
		found = true
		return audit(r.Context(), tx, actor.ID, "lead.status_changed", "lead", id,
			map[string]any{"from": previous, "to": string(status)})
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if !found {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	h.detail(w, r)
}

// erase permanently deletes a lead (a data-subject erasure request). The audit
// entry records that it happened and who did it, but no personal data.
func (h *Handler) erase(w http.ResponseWriter, r *http.Request) {
	id, err := leadID(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	actor := principalFrom(r.Context())
	found := false
	err = db.InTx(r.Context(), h.Pool, func(tx pgx.Tx) error {
		// Audit entries about the lead may hold nothing personal, but they are
		// keyed by the lead id; remove them and leave a single erasure record.
		if _, err := tx.Exec(r.Context(), `DELETE FROM audit_log WHERE target_type = 'lead' AND target_id = $1`, id); err != nil {
			return err
		}
		ok, err := leads.Erase(r.Context(), tx, id)
		if err != nil || !ok {
			return err
		}
		found = true
		return audit(r.Context(), tx, actor.ID, "lead.erased", "lead", "", nil)
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if !found {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	httpx.JSON(w, r, http.StatusOK, map[string]bool{"erased": true})
}

const exportLimit = 50000

// export streams the filtered leads as CSV. Every export is audited with the
// filter used and the number of rows requested.
func (h *Handler) export(w http.ResponseWriter, r *http.Request) {
	f, err := parseFilter(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	where, args := f.where()

	// The audit entry is written before any data is sent: if it cannot be
	// recorded, nothing is exported.
	var total int
	if err := h.Pool.QueryRow(r.Context(), `SELECT count(*) `+leadFrom+where, args...).Scan(&total); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	actor := principalFrom(r.Context())
	if err := audit(r.Context(), h.Pool, actor.ID, "leads.exported", "lead", "", map[string]any{
		"rows":     min(total, exportLimit),
		"status":   f.Status,
		"location": f.Location,
		"source":   f.Source,
		"referral": f.Referral,
		"searched": f.Query != "",
	}); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	rows, err := h.Pool.Query(r.Context(), `
		SELECT l.id, l.first_name, l.email, COALESCE(l.phone, ''), l.location, l.status, l.referral_code,
			`+sourceExpr+`, COALESCE(a.utm_medium, ''), COALESCE(a.utm_campaign, ''),
			COALESCE(p.household_size::text, ''), COALESCE(p.meals_per_week::text, ''),
			array_to_string(p.dietary_preferences, '|'), array_to_string(p.meal_interests, '|'),
			COALESCE(p.cooking_frequency, ''), COALESCE(p.packaging_preference, ''),
			(SELECT count(*) FROM referrals r WHERE r.referrer_lead_id = l.id AND r.status = 'converted'),
			l.email_verified_at, l.consent_at, l.created_at
		`+leadFrom+where+fmt.Sprintf(" ORDER BY l.created_at, l.id LIMIT %d", exportLimit), args...)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	defer rows.Close()

	filename := "mise-leads-" + time.Now().UTC().Format("20060102-1504") + ".csv"
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Cache-Control", "no-store")

	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "first_name", "email", "phone", "location", "status", "referral_code",
		"source", "utm_medium", "utm_campaign", "household_size", "meals_per_week", "dietary_preferences",
		"meal_interests", "cooking_frequency", "packaging_preference", "referrals_converted", "email_verified_at", "consent_at", "created_at"})

	for rows.Next() {
		var (
			id, firstName, email, phone, location, status, code, source, medium, campaign string
			household, meals, cooking, packaging                                          string
			dietary, interests                                                            *string
			converted                                                                     int
			verifiedAt                                                                    *time.Time
			consentAt, createdAt                                                          time.Time
		)
		if err := rows.Scan(&id, &firstName, &email, &phone, &location, &status, &code, &source, &medium,
			&campaign, &household, &meals, &dietary, &interests, &cooking, &packaging, &converted, &verifiedAt,
			&consentAt, &createdAt); err != nil {
			slog.ErrorContext(r.Context(), "export scan failed", "error", err.Error())
			break
		}
		verified := ""
		if verifiedAt != nil {
			verified = verifiedAt.UTC().Format(time.RFC3339)
		}
		record := []string{id, firstName, email, phone, location, status, code, source, medium, campaign,
			household, meals, deref(dietary), deref(interests), cooking, packaging, strconv.Itoa(converted), verified,
			consentAt.UTC().Format(time.RFC3339), createdAt.UTC().Format(time.RFC3339)}
		for i, cell := range record {
			record[i] = csvSafe(cell)
		}
		_ = cw.Write(record)
	}
	cw.Flush()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// csvSafe neutralises spreadsheet formula injection: a cell that starts with
// =, +, - or @ is prefixed so Excel and Sheets treat it as text.
func csvSafe(cell string) string {
	if cell == "" {
		return cell
	}
	switch cell[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + cell
	}
	return cell
}
