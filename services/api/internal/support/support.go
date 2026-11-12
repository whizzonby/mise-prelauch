// Package support is a lightweight internal case list: what a customer (or
// lead) needs help with, who is on it, and what happened. It is not a
// helpdesk: there is no customer-facing inbox and nothing is emailed from
// here.
package support

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/support/store"
)

var (
	Categories = []string{"subscription", "billing", "meal_selection", "delivery", "account_access", "product_feedback", "general"}
	Priorities = []string{"low", "normal", "high", "urgent"}
	Statuses   = []string{"open", "in_progress", "waiting_for_customer", "resolved", "closed"}
	// transitions: closed is final; everything else can move anywhere but
	// back to open from closed.
	transitions = map[string][]string{
		"open":                 {"in_progress", "waiting_for_customer", "resolved", "closed"},
		"in_progress":          {"open", "waiting_for_customer", "resolved", "closed"},
		"waiting_for_customer": {"open", "in_progress", "resolved", "closed"},
		"resolved":             {"open", "in_progress", "closed"},
		"closed":               {},
	}
	uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// Alerts raises a staff alert (internal/admin implements it).
type Alerts interface {
	Raise(ctx context.Context, q db.Querier, a Alert) error
}

// Alert is what support asks the alert centre to show.
type Alert struct {
	DedupeKey, Kind, Severity, Title, Body, ResourceType, ResourceID, Permission string
}

type Service struct {
	pool   *pgxpool.Pool
	Alerts Alerts
	Now    func() time.Time
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool, Now: time.Now} }

// Case is a support case.
type Case struct {
	ID           string     `json:"id"`
	Number       int64      `json:"number"`
	CustomerID   *string    `json:"customer_id"`
	LeadID       *string    `json:"lead_id"`
	Subject      string     `json:"subject"`
	Category     string     `json:"category"`
	Priority     string     `json:"priority"`
	Status       string     `json:"status"`
	AssignedTo   *string    `json:"assigned_to"`
	AssigneeName string     `json:"assignee_name"`
	Resolution   *string    `json:"resolution"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	ResolvedAt   *time.Time `json:"resolved_at"`
	ClosedAt     *time.Time `json:"closed_at"`
	Events       []Event    `json:"events,omitempty"`
}

type Event struct {
	Type      string    `json:"type"`
	Body      *string   `json:"body"`
	From      *string   `json:"from"`
	To        *string   `json:"to"`
	ActorName string    `json:"actor_name"`
	CreatedAt time.Time `json:"created_at"`
}

func view(c store.SupportCase) Case {
	return Case{ID: c.ID, Number: c.Number, CustomerID: c.CustomerID, LeadID: c.LeadID, Subject: c.Subject, Category: c.Category,
		Priority: c.Priority, Status: c.Status, AssignedTo: c.AssignedTo, Resolution: c.Resolution, CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt, ResolvedAt: c.ResolvedAt, ClosedAt: c.ClosedAt}
}

func ptr[T any](v T) *T { return &v }

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Filter narrows the case list.
type Filter struct {
	Status     string
	OpenOnly   bool
	CustomerID string
	AssignedTo string
	Priority   string
	Page, Size int
}

type Page struct {
	Items []Case `json:"items"`
	Total int    `json:"total"`
	Page  int    `json:"page"`
}

func (s *Service) List(ctx context.Context, f Filter) (*Page, error) {
	if f.Size < 1 || f.Size > 100 {
		f.Size = 50
	}
	if f.Page < 1 {
		f.Page = 1
	}
	p := store.ListCasesParams{OpenOnly: f.OpenOnly, Lim: int32(f.Size), Off: int32((f.Page - 1) * f.Size)}
	if slices.Contains(Statuses, f.Status) {
		p.Status = &f.Status
	}
	if uuidPattern.MatchString(f.CustomerID) {
		p.CustomerID = &f.CustomerID
	}
	if uuidPattern.MatchString(f.AssignedTo) {
		p.AssignedTo = &f.AssignedTo
	}
	if slices.Contains(Priorities, f.Priority) {
		p.Priority = &f.Priority
	}
	rows, err := store.New(s.pool).ListCases(ctx, p)
	if err != nil {
		return nil, err
	}
	out := &Page{Items: []Case{}, Page: f.Page}
	for _, r := range rows {
		c := view(store.SupportCase{ID: r.ID, Number: r.Number, CustomerID: r.CustomerID, LeadID: r.LeadID, Subject: r.Subject,
			Category: r.Category, Priority: r.Priority, Status: r.Status, AssignedTo: r.AssignedTo, Resolution: r.Resolution,
			CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, ResolvedAt: r.ResolvedAt, ClosedAt: r.ClosedAt})
		c.AssigneeName = r.AssigneeName
		out.Items = append(out.Items, c)
		out.Total = int(r.Total)
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, id string) (*Case, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	q := store.New(s.pool)
	row, err := q.GetCase(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	v := view(row.SupportCase)
	v.AssigneeName = row.AssigneeName
	events, err := q.CaseEvents(ctx, id)
	if err != nil {
		return nil, err
	}
	v.Events = []Event{}
	for _, e := range events {
		v.Events = append(v.Events, Event{Type: e.Type, Body: e.Body, From: e.FromValue, To: e.ToValue, ActorName: e.ActorName, CreatedAt: e.CreatedAt})
	}
	return &v, nil
}

// CreateInput opens a case.
type CreateInput struct {
	CustomerID string `json:"customer_id"`
	LeadID     string `json:"lead_id"`
	Subject    string `json:"subject"`
	Category   string `json:"category"`
	Priority   string `json:"priority"`
	AssignedTo string `json:"assigned_to"`
	Note       string `json:"note"`
}

func (s *Service) Create(ctx context.Context, actor string, in CreateInput) (*Case, error) {
	in.Subject = strings.TrimSpace(in.Subject)
	in.Note = strings.TrimSpace(in.Note)
	if in.Priority == "" {
		in.Priority = "normal"
	}
	var f []httpx.FieldError
	add := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	if n := len([]rune(in.Subject)); n < 1 || n > 160 {
		add("subject", "Give the case a subject (up to 160 characters).")
	}
	if !slices.Contains(Categories, in.Category) {
		add("category", "Choose a category.")
	}
	if !slices.Contains(Priorities, in.Priority) {
		add("priority", "Choose a priority.")
	}
	if len([]rune(in.Note)) > 2000 {
		add("note", "Keep the first note under 2,000 characters.")
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	var id string
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		params := store.CreateCaseParams{Subject: in.Subject, Category: in.Category, Priority: in.Priority, CreatedBy: nilIfEmpty(actor)}
		if in.CustomerID != "" {
			if ok, err := q.CustomerExists(ctx, in.CustomerID); err != nil || !uuidPattern.MatchString(in.CustomerID) || !ok {
				return orInvalid(err, "customer_id", "Customer not found.")
			}
			params.CustomerID = &in.CustomerID
		}
		if in.LeadID != "" {
			if ok, err := q.LeadExists(ctx, in.LeadID); err != nil || !uuidPattern.MatchString(in.LeadID) || !ok {
				return orInvalid(err, "lead_id", "Lead not found.")
			}
			params.LeadID = &in.LeadID
		}
		if in.AssignedTo != "" {
			if err := checkStaff(ctx, q, in.AssignedTo); err != nil {
				return err
			}
			params.AssignedTo = &in.AssignedTo
		}
		c, err := q.CreateCase(ctx, params)
		if err != nil {
			return err
		}
		id = c.ID
		if err := q.AddCaseEvent(ctx, store.AddCaseEventParams{CaseID: c.ID, Type: "created", Body: nilIfEmpty(in.Note), ToValue: ptr("open"), ActorID: nilIfEmpty(actor)}); err != nil {
			return err
		}
		if s.Alerts != nil {
			sev := "info"
			if in.Priority == "high" || in.Priority == "urgent" {
				sev = "warning"
			}
			if err := s.Alerts.Raise(ctx, tx, Alert{DedupeKey: "case:" + c.ID, Kind: "support_case", Severity: sev,
				Title: "New support case: " + c.Subject, ResourceType: "support_case", ResourceID: c.ID, Permission: "cases.read"}); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "case.created", TargetType: "support_case", TargetID: c.ID,
			Metadata: map[string]any{"category": c.Category, "priority": c.Priority}})
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

func orInvalid(err error, field, msg string) error {
	if err != nil {
		return err
	}
	return httpx.ValidationError([]httpx.FieldError{{Field: field, Code: "invalid", Message: msg}})
}

func checkStaff(ctx context.Context, q *store.Queries, id string) error {
	if !uuidPattern.MatchString(id) {
		return orInvalid(nil, "assigned_to", "Choose an active staff member.")
	}
	ok, err := q.StaffUserExists(ctx, id)
	if err != nil || !ok {
		return orInvalid(err, "assigned_to", "Choose an active staff member.")
	}
	return nil
}

// UpdateInput changes a case. Fields left nil keep their value.
type UpdateInput struct {
	Status     *string `json:"status"`
	Priority   *string `json:"priority"`
	AssignedTo *string `json:"assigned_to"` // "" unassigns
	Resolution *string `json:"resolution"`
}

func (s *Service) Update(ctx context.Context, actor, id string, in UpdateInput) (*Case, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		cur, err := q.GetCaseForUpdate(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		if cur.Status == "closed" {
			return httpx.NewError(http.StatusConflict, "case_closed", "This case is closed. Open a new case instead.")
		}
		status, priority, assignee, resolution := cur.Status, cur.Priority, cur.AssignedTo, cur.Resolution
		event := func(typ string, from, to *string, body *string) error {
			return q.AddCaseEvent(ctx, store.AddCaseEventParams{CaseID: id, Type: typ, FromValue: from, ToValue: to, Body: body, ActorID: nilIfEmpty(actor)})
		}
		if in.Resolution != nil {
			r := strings.TrimSpace(*in.Resolution)
			if len([]rune(r)) > 2000 {
				return orInvalid(nil, "resolution", "Keep the resolution under 2,000 characters.")
			}
			if r != "" {
				resolution = &r
			}
		}
		if in.Status != nil && *in.Status != status {
			if !slices.Contains(transitions[status], *in.Status) {
				return httpx.NewError(http.StatusConflict, "invalid_transition", "A case that is "+status+" can't become "+*in.Status+".")
			}
			if (*in.Status == "resolved" || *in.Status == "closed") && resolution == nil {
				return orInvalid(nil, "resolution", "Say how the case was resolved.")
			}
			if err := event("status", &status, in.Status, nil); err != nil {
				return err
			}
			status = *in.Status
		}
		if in.Priority != nil && *in.Priority != priority {
			if !slices.Contains(Priorities, *in.Priority) {
				return orInvalid(nil, "priority", "Choose a priority.")
			}
			if err := event("priority", &priority, in.Priority, nil); err != nil {
				return err
			}
			priority = *in.Priority
		}
		if in.AssignedTo != nil {
			var next *string
			if *in.AssignedTo != "" {
				if err := checkStaff(ctx, q, *in.AssignedTo); err != nil {
					return err
				}
				next = in.AssignedTo
			}
			if (next == nil) != (assignee == nil) || (next != nil && *next != *assignee) {
				if err := event("assignment", assignee, next, nil); err != nil {
					return err
				}
				assignee = next
			}
		}
		if in.Resolution != nil && resolution != nil && (cur.Resolution == nil || *cur.Resolution != *resolution) {
			if err := event("resolution", nil, nil, resolution); err != nil {
				return err
			}
		}
		if _, err := q.UpdateCase(ctx, store.UpdateCaseParams{ID: id, Status: status, Priority: priority, AssignedTo: assignee, Resolution: resolution}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "case.updated", TargetType: "support_case", TargetID: id,
			Metadata: map[string]any{"status_from": cur.Status, "status_to": status, "priority": priority}})
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// AddNote adds an internal note to a case.
func (s *Service) AddNote(ctx context.Context, actor, id, body string) (*Case, error) {
	body = strings.TrimSpace(body)
	if n := len([]rune(body)); n < 1 || n > 2000 {
		return nil, orInvalid(nil, "body", "Write a note of up to 2,000 characters.")
	}
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		cur, err := q.GetCaseForUpdate(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		if cur.Status == "closed" {
			return httpx.NewError(http.StatusConflict, "case_closed", "This case is closed.")
		}
		if err := q.AddCaseEvent(ctx, store.AddCaseEventParams{CaseID: id, Type: "note", Body: &body, ActorID: nilIfEmpty(actor)}); err != nil {
			return err
		}
		return q.TouchCase(ctx, id)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Stale lists open cases untouched since before (for retention alerts).
type StaleCase struct {
	ID, Subject string
	Number      int64
	CustomerID  *string
}

func (s *Service) Stale(ctx context.Context, q db.Querier, before time.Time) ([]StaleCase, error) {
	rows, err := store.New(q).StaleOpenCases(ctx, before)
	if err != nil {
		return nil, err
	}
	out := []StaleCase{}
	for _, r := range rows {
		out = append(out, StaleCase{ID: r.ID, Subject: r.Subject, Number: r.Number, CustomerID: r.CustomerID})
	}
	return out, nil
}
