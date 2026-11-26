// Package behavior records first-party customer behaviour in the customer
// app (contract v1) and derives explainable customer intelligence from it
// and from existing business records: funnels, checkout abandonment,
// cancellation reasons and rule-based segments.
//
// Privacy (docs/analytics/BEHAVIOR_EVENTS.md): a fixed list of events, each
// with its own allowed properties (short identifiers only: no form
// contents, names, emails, addresses or card data); nothing is sent under
// Global Privacy Control or Do Not Track (the apps check both); no cookies.
// Business facts (payments, renewals, pauses, cancellations, support cases)
// are never re-recorded here: they are read where they already live.
package behavior

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/platform/httpx"
)

// ContractVersion is the current event contract.
const ContractVersion = 1

var (
	slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,99}$`)
	idPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
	uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// propKind is how a property is validated.
type propKind int

const (
	slug propKind = iota
	date
	smallInt
	choice
)

type propSpec struct {
	kind     propKind
	required bool
	choices  []string
}

// Contract is the complete v1 event list and each event's properties.
var Contract = map[string]map[string]propSpec{
	"catalogue_viewed": {"category": {kind: slug}, "filters": {kind: smallInt}},
	"meal_viewed":      {"meal": {kind: slug, required: true}},
	"plan_viewed":      {"plan": {kind: slug, required: true}},
	"plan_selected":    {"plan": {kind: slug, required: true}},
	"meal_selected":    {"meal": {kind: slug, required: true}, "week": {kind: date, required: true}},
	"checkout_started": {"kind": {kind: choice, required: true, choices: []string{"subscription", "week"}}, "plan": {kind: slug}},
}

// Event is one behaviour event as sent.
type Event struct {
	EventID    string         `json:"event_id"`
	Name       string         `json:"name"`
	Version    int            `json:"version"`
	Properties map[string]any `json:"properties"`
}

// Batch is what the app sends: up to 20 events for one actor.
type Batch struct {
	AnonymousID string  `json:"anonymous_id"`
	SessionID   string  `json:"session_id"`
	Events      []Event `json:"events"`
}

type Service struct {
	pool *pgxpool.Pool
	Now  func() time.Time
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool, Now: time.Now} }

func invalid(field, msg string) error {
	return httpx.ValidationError([]httpx.FieldError{{Field: field, Code: "invalid", Message: msg}})
}

// validate checks the batch against the contract. Unknown events, unknown
// properties and malformed values are refused, so nothing else can be stored.
func validate(b Batch, signedIn bool) error {
	if len(b.Events) == 0 || len(b.Events) > 20 {
		return invalid("events", "Send between 1 and 20 events.")
	}
	if b.AnonymousID != "" && !idPattern.MatchString(b.AnonymousID) {
		return invalid("anonymous_id", "Malformed anonymous id.")
	}
	if b.SessionID != "" && !idPattern.MatchString(b.SessionID) {
		return invalid("session_id", "Malformed session id.")
	}
	if !signedIn && b.AnonymousID == "" {
		return invalid("anonymous_id", "A visitor's events need an anonymous id.")
	}
	for i, e := range b.Events {
		field := fmt.Sprintf("events[%d]", i)
		spec, ok := Contract[e.Name]
		if !ok {
			return invalid(field+".name", "Unknown event.")
		}
		if e.Version != ContractVersion {
			return invalid(field+".version", fmt.Sprintf("Contract version %d is the only one accepted.", ContractVersion))
		}
		if !uuidPattern.MatchString(e.EventID) {
			return invalid(field+".event_id", "event_id must be a lowercase UUID.")
		}
		for k := range e.Properties {
			if _, ok := spec[k]; !ok {
				return invalid(field+".properties."+k, "Not part of this event's contract.")
			}
		}
		for k, p := range spec {
			v, present := e.Properties[k]
			if !present {
				if p.required {
					return invalid(field+".properties."+k, "Required.")
				}
				continue
			}
			okv := false
			switch p.kind {
			case slug:
				s, isStr := v.(string)
				okv = isStr && slugPattern.MatchString(s)
			case date:
				s, isStr := v.(string)
				if isStr && datePattern.MatchString(s) {
					_, err := time.Parse("2006-01-02", s)
					okv = err == nil
				}
			case smallInt:
				f, isNum := v.(float64)
				okv = isNum && f == float64(int(f)) && f >= 0 && f <= 50
			case choice:
				s, isStr := v.(string)
				for _, c := range p.choices {
					okv = okv || (isStr && s == c)
				}
			}
			if !okv {
				return invalid(field+".properties."+k, "Malformed value.")
			}
		}
	}
	return nil
}

// Record stores a batch. A repeated event_id is accepted and ignored
// (deduplicated). It returns how many events were new.
func (s *Service) Record(ctx context.Context, customerID string, b Batch) (int, error) {
	if err := validate(b, customerID != ""); err != nil {
		return 0, err
	}
	var cust, anon, sess *string
	if customerID != "" {
		cust = &customerID
	}
	if b.AnonymousID != "" {
		anon = &b.AnonymousID
	}
	if b.SessionID != "" {
		sess = &b.SessionID
	}
	batch := &pgx.Batch{}
	for _, e := range b.Events {
		props := e.Properties
		if props == nil {
			props = map[string]any{}
		}
		raw, err := json.Marshal(props)
		if err != nil {
			return 0, err
		}
		batch.Queue(`INSERT INTO behavior_events (event_id, name, version, customer_id, anonymous_id, session_id, properties, occurred_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT (event_id) DO NOTHING`,
			e.EventID, e.Name, e.Version, cust, anon, sess, raw, s.Now())
	}
	res := s.pool.SendBatch(ctx, batch)
	defer res.Close()
	added := 0
	for range b.Events {
		tag, err := res.Exec()
		if err != nil {
			return added, err
		}
		added += int(tag.RowsAffected())
	}
	return added, nil
}
