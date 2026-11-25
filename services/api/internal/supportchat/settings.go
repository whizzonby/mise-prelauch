package supportchat

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
)

// Hours is one opening period on a weekday (0 = Sunday), local time.
type Hours struct {
	Day   int    `json:"day"`
	Open  string `json:"open"`
	Close string `json:"close"`
}

// Holiday is a closed day.
type Holiday struct {
	Day  string `json:"day"` // YYYY-MM-DD
	Name string `json:"name"`
}

// Settings is how chat presents availability. Everything ships empty.
type Settings struct {
	Enabled              bool      `json:"enabled"`
	Timezone             string    `json:"timezone"`
	BusinessHours        []Hours   `json:"business_hours"`
	Holidays             []Holiday `json:"holidays"`
	OnlineMessage        string    `json:"online_message"`
	OfflineMessage       string    `json:"offline_message"`
	ResponseExpectation  string    `json:"response_expectation"`
	EmailCooldownMinutes int       `json:"email_cooldown_minutes"`
	UpdatedAt            time.Time `json:"updated_at"`
}

func defaults() Settings {
	return Settings{Enabled: true, Timezone: "America/Port_of_Spain", BusinessHours: []Hours{}, Holidays: []Holiday{}, EmailCooldownMinutes: 30}
}

func (s *Service) Settings(ctx context.Context) (*Settings, error) {
	st := defaults()
	var hours []byte
	err := s.pool.QueryRow(ctx, `SELECT enabled, timezone, business_hours, online_message, offline_message, response_expectation,
		email_cooldown_minutes, updated_at FROM support_chat_settings`).Scan(&st.Enabled, &st.Timezone, &hours, &st.OnlineMessage,
		&st.OfflineMessage, &st.ResponseExpectation, &st.EmailCooldownMinutes, &st.UpdatedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if len(hours) > 0 {
		if err := json.Unmarshal(hours, &st.BusinessHours); err != nil {
			return nil, err
		}
	}
	rows, err := s.pool.Query(ctx, `SELECT to_char(day, 'YYYY-MM-DD'), name FROM support_holidays WHERE day >= current_date - 1 ORDER BY day`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var h Holiday
		if err := rows.Scan(&h.Day, &h.Name); err != nil {
			return nil, err
		}
		st.Holidays = append(st.Holidays, h)
	}
	return &st, rows.Err()
}

func (s *Service) requireEnabled(ctx context.Context) error {
	st, err := s.Settings(ctx)
	if err != nil {
		return err
	}
	if !st.Enabled {
		return ErrUnavailable
	}
	return nil
}

var clock = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// UpdateSettings replaces the chat settings (support_chat.manage).
func (s *Service) UpdateSettings(ctx context.Context, p *identity.Principal, in Settings) (*Settings, error) {
	var f []httpx.FieldError
	bad := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	if _, err := time.LoadLocation(in.Timezone); err != nil || in.Timezone == "" {
		bad("timezone", "Choose a time zone such as America/Port_of_Spain.")
	}
	if in.BusinessHours == nil {
		in.BusinessHours = []Hours{}
	}
	for _, h := range in.BusinessHours {
		if h.Day < 0 || h.Day > 6 || !clock.MatchString(h.Open) || !clock.MatchString(h.Close) || h.Open >= h.Close {
			bad("business_hours", "Each period needs a weekday and an opening time before its closing time (HH:MM).")
			break
		}
	}
	seen := map[string]bool{}
	for _, h := range in.Holidays {
		if _, err := time.Parse("2006-01-02", h.Day); err != nil || seen[h.Day] || utf8.RuneCountInString(strings.TrimSpace(h.Name)) < 1 || utf8.RuneCountInString(h.Name) > 100 {
			bad("holidays", "Each closed day needs a date (YYYY-MM-DD), once, and a name.")
			break
		}
		seen[h.Day] = true
	}
	if utf8.RuneCountInString(in.OnlineMessage) > 500 || utf8.RuneCountInString(in.OfflineMessage) > 500 {
		bad("offline_message", "Keep messages under 500 characters.")
	}
	if utf8.RuneCountInString(in.ResponseExpectation) > 300 {
		bad("response_expectation", "Keep this under 300 characters.")
	}
	if in.EmailCooldownMinutes < 5 || in.EmailCooldownMinutes > 1440 {
		bad("email_cooldown_minutes", "Between 5 and 1,440 minutes.")
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	sort.Slice(in.BusinessHours, func(i, j int) bool {
		a, b := in.BusinessHours[i], in.BusinessHours[j]
		return a.Day < b.Day || (a.Day == b.Day && a.Open < b.Open)
	})
	hours, err := json.Marshal(in.BusinessHours)
	if err != nil {
		return nil, err
	}
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO support_chat_settings (id) VALUES (true) ON CONFLICT DO NOTHING`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE support_chat_settings SET enabled = $1, timezone = $2, business_hours = $3, online_message = $4,
			offline_message = $5, response_expectation = $6, email_cooldown_minutes = $7, updated_by = $8, updated_at = now()`,
			in.Enabled, in.Timezone, hours, strings.TrimSpace(in.OnlineMessage), strings.TrimSpace(in.OfflineMessage),
			strings.TrimSpace(in.ResponseExpectation), in.EmailCooldownMinutes, p.UserID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM support_holidays`); err != nil {
			return err
		}
		for _, h := range in.Holidays {
			if _, err := tx.Exec(ctx, `INSERT INTO support_holidays (day, name) VALUES ($1, $2)`, h.Day, strings.TrimSpace(h.Name)); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: "support_chat.settings_updated", TargetType: "support_chat_settings",
			TargetID: "chat", Metadata: map[string]any{"enabled": in.Enabled, "timezone": in.Timezone, "periods": len(in.BusinessHours), "holidays": len(in.Holidays)}})
	})
	if err != nil {
		return nil, err
	}
	return s.Settings(ctx)
}

// Availability is what the widget shows before someone writes.
type Availability struct {
	Enabled bool `json:"enabled"`
	// Online is true or false only when business hours are configured;
	// null means no hours are set and nothing is claimed.
	Online              *bool  `json:"online"`
	Message             string `json:"message"`
	ResponseExpectation string `json:"response_expectation"`
}

// DefaultExpectation is shown when the business has not written its own.
const DefaultExpectation = "We'll reply as soon as we can."

func (s *Service) Availability(ctx context.Context) (*Availability, error) {
	st, err := s.Settings(ctx)
	if err != nil {
		return nil, err
	}
	a := &Availability{Enabled: st.Enabled, ResponseExpectation: st.ResponseExpectation}
	if a.ResponseExpectation == "" {
		a.ResponseExpectation = DefaultExpectation
	}
	if len(st.BusinessHours) > 0 {
		online := openAt(st, s.Now())
		a.Online = &online
	}
	if a.Online != nil && *a.Online {
		a.Message = st.OnlineMessage
	} else if a.Online != nil {
		a.Message = st.OfflineMessage
	}
	return a, nil
}

// openAt reports whether now falls inside the configured hours and is not a holiday.
func openAt(st *Settings, now time.Time) bool {
	loc, err := time.LoadLocation(st.Timezone)
	if err != nil {
		return false
	}
	local := now.In(loc)
	today := local.Format("2006-01-02")
	for _, h := range st.Holidays {
		if h.Day == today {
			return false
		}
	}
	hm := local.Format("15:04")
	for _, h := range st.BusinessHours {
		if h.Day == int(local.Weekday()) && hm >= h.Open && hm < h.Close {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- saved replies

type SavedReply struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Archived  bool      `json:"archived"`
	UpdatedAt time.Time `json:"updated_at"`
}

type SavedReplyInput struct {
	Title    *string `json:"title"`
	Body     *string `json:"body"`
	Archived *bool   `json:"archived"`
}

func (s *Service) SavedReplies(ctx context.Context, includeArchived bool) ([]SavedReply, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, title, body, archived, updated_at FROM support_saved_replies WHERE $1 OR NOT archived ORDER BY title`, includeArchived)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SavedReply{}
	for rows.Next() {
		var r SavedReply
		if err := rows.Scan(&r.ID, &r.Title, &r.Body, &r.Archived, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func validReply(title, body string) error {
	if n := utf8.RuneCountInString(strings.TrimSpace(title)); n < 1 || n > 100 {
		return httpx.ValidationError([]httpx.FieldError{{Field: "title", Code: "invalid", Message: "Give it a title (up to 100 characters)."}})
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(body)); n < 1 || n > 4000 {
		return httpx.ValidationError([]httpx.FieldError{{Field: "body", Code: "invalid", Message: "Write the reply (up to 4,000 characters)."}})
	}
	return nil
}

func (s *Service) CreateSavedReply(ctx context.Context, p *identity.Principal, in SavedReplyInput) (*SavedReply, error) {
	if in.Title == nil || in.Body == nil {
		return nil, validReply("", "")
	}
	if err := validReply(*in.Title, *in.Body); err != nil {
		return nil, err
	}
	var r SavedReply
	err := s.pool.QueryRow(ctx, `INSERT INTO support_saved_replies (title, body, created_by, updated_by) VALUES ($1, $2, $3, $3)
		RETURNING id, title, body, archived, updated_at`, strings.TrimSpace(*in.Title), strings.TrimSpace(*in.Body), p.UserID).
		Scan(&r.ID, &r.Title, &r.Body, &r.Archived, &r.UpdatedAt)
	return &r, err
}

func (s *Service) UpdateSavedReply(ctx context.Context, p *identity.Principal, id string, in SavedReplyInput) (*SavedReply, error) {
	if !validUUID(id) {
		return nil, httpx.ErrNotFound
	}
	var r SavedReply
	err := s.pool.QueryRow(ctx, `SELECT id, title, body, archived, updated_at FROM support_saved_replies WHERE id = $1`, id).
		Scan(&r.ID, &r.Title, &r.Body, &r.Archived, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if in.Title != nil {
		r.Title = strings.TrimSpace(*in.Title)
	}
	if in.Body != nil {
		r.Body = strings.TrimSpace(*in.Body)
	}
	if in.Archived != nil {
		r.Archived = *in.Archived
	}
	if err := validReply(r.Title, r.Body); err != nil {
		return nil, err
	}
	err = s.pool.QueryRow(ctx, `UPDATE support_saved_replies SET title = $2, body = $3, archived = $4, updated_by = $5, updated_at = now()
		WHERE id = $1 RETURNING updated_at`, id, r.Title, r.Body, r.Archived, p.UserID).Scan(&r.UpdatedAt)
	return &r, err
}
