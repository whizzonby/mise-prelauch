// Package events ingests first-party analytics events. The taxonomy is an
// allow-list mirrored by packages/analytics; anything else is rejected, so the
// table cannot become a dumping ground.
package events

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
)

// Types is the complete event taxonomy.
var Types = map[string]bool{
	"page_view":             true,
	"section_viewed":        true,
	"hero_cta_clicked":      true,
	"waitlist_started":      true,
	"waitlist_completed":    true,
	"preferences_started":   true,
	"preferences_completed": true,
	"referral_link_copied":  true,
	"referral_shared":       true,
	"faq_opened":            true,
	"meal_viewed":           true,
	"plan_viewed":           true,
}

const (
	maxBody         = 32 << 10
	maxBatch        = 20
	maxMetadataKeys = 8
	maxValueLength  = 200
)

var (
	idPattern  = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
	keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
)

type Event struct {
	Type     string         `json:"type"`
	Metadata map[string]any `json:"metadata"`
}

type Batch struct {
	AnonymousID string  `json:"anonymous_id"`
	SessionID   string  `json:"session_id"`
	Events      []Event `json:"events"`
}

func validate(b Batch) error {
	var errs []httpx.FieldError
	add := func(field, code, msg string) {
		errs = append(errs, httpx.FieldError{Field: field, Code: code, Message: msg})
	}
	if !idPattern.MatchString(b.AnonymousID) {
		add("anonymous_id", "invalid", "anonymous_id must be 8 to 64 URL-safe characters.")
	}
	if !idPattern.MatchString(b.SessionID) {
		add("session_id", "invalid", "session_id must be 8 to 64 URL-safe characters.")
	}
	if len(b.Events) == 0 || len(b.Events) > maxBatch {
		add("events", "out_of_range", "Send between 1 and 20 events.")
	}
	for _, e := range b.Events {
		if !Types[e.Type] {
			add("events", "unknown_type", "Unknown event type: "+truncate(e.Type, 40)+".")
			break
		}
		if !validMetadata(e.Metadata) {
			add("events", "invalid_metadata", "Event metadata must be a small flat object of strings, numbers and booleans.")
			break
		}
	}
	if len(errs) > 0 {
		return httpx.ValidationError(errs)
	}
	return nil
}

func validMetadata(m map[string]any) bool {
	if len(m) > maxMetadataKeys {
		return false
	}
	for k, v := range m {
		if !keyPattern.MatchString(k) {
			return false
		}
		switch val := v.(type) {
		case string:
			if utf8.RuneCountInString(val) > maxValueLength {
				return false
			}
		case float64, bool:
		default:
			return false
		}
	}
	return true
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// LeadResolver turns a bearer token into a lead id. It returns an error when
// the token is missing or invalid, in which case events stay anonymous.
type LeadResolver func(token string) (string, error)

type Handler struct {
	DB          db.Querier
	ResolveLead LeadResolver
	BearerToken func(*http.Request) string
}

// Ingest serves POST /api/v1/events.
func (h *Handler) Ingest(w http.ResponseWriter, r *http.Request) {
	var batch Batch
	if err := httpx.Decode(w, r, &batch, maxBody); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if err := validate(batch); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	var leadID *string
	if tok := h.BearerToken(r); tok != "" {
		if id, err := h.ResolveLead(tok); err == nil {
			leadID = &id
		}
	}

	if err := insert(r.Context(), h.DB, batch, leadID); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusAccepted, map[string]int{"accepted": len(batch.Events)})
}

func insert(ctx context.Context, q db.Querier, b Batch, leadID *string) error {
	batch := &pgx.Batch{}
	for _, e := range b.Events {
		metadata := e.Metadata
		if metadata == nil {
			metadata = map[string]any{}
		}
		body, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		// A deleted lead's still-valid token must not break ingestion, so the
		// lead id is only kept if the lead exists.
		batch.Queue(`
			INSERT INTO lead_events (lead_id, anonymous_id, event_type, session_id, metadata)
			VALUES ((SELECT id FROM leads WHERE id = $1::uuid), $2, $3, $4, $5::jsonb)`,
			leadID, b.AnonymousID, e.Type, b.SessionID, body)
	}
	sender, ok := q.(interface {
		SendBatch(context.Context, *pgx.Batch) pgx.BatchResults
	})
	if !ok {
		for _, item := range batch.QueuedQueries {
			if _, err := q.Exec(ctx, item.SQL, item.Arguments...); err != nil {
				return err
			}
		}
		return nil
	}
	return sender.SendBatch(ctx, batch).Close()
}
