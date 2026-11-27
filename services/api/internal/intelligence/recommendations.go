package intelligence

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/httpx"
)

// Recommendation is a stored item for review. Reviewing it changes only
// the item: any operational action goes through the existing workflows.
type Recommendation struct {
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	Basis      string          `json:"basis"`
	Domain     string          `json:"domain"`
	SourceKey  string          `json:"source_key"`
	Title      string          `json:"title"`
	Summary    string          `json:"summary"`
	Evidence   json.RawMessage `json:"evidence"`
	ReviewHref string          `json:"review_href"`
	Status     string          `json:"status"`
	AssignedTo *string         `json:"assigned_to"`
	RunID      *string         `json:"run_id"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
	Events     []RecEvent      `json:"events"`
}

// RecEvent is one review step.
type RecEvent struct {
	Action     string    `json:"action"`
	FromStatus string    `json:"from_status"`
	ToStatus   string    `json:"to_status"`
	Note       string    `json:"note"`
	Actor      *string   `json:"actor"`
	CreatedAt  time.Time `json:"created_at"`
}

// Advisory is shown with every recommendation.
const Advisory = "Advisory only. Mise Intelligence cannot charge, refund, change prices, approve purchase orders, change production, publish meals, " +
	"override quality holds, send campaigns, hire, move money or change supplier contracts. Act through the existing workflows."

const recCols = `id, kind, basis, domain, source_key, title, summary, evidence, review_href, status, assigned_to, run_id, created_at, updated_at`

func scanRec(row pgx.Row) (*Recommendation, error) {
	var r Recommendation
	var ev []byte
	if err := row.Scan(&r.ID, &r.Kind, &r.Basis, &r.Domain, &r.SourceKey, &r.Title, &r.Summary, &ev, &r.ReviewHref, &r.Status, &r.AssignedTo, &r.RunID,
		&r.CreatedAt, &r.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, httpx.ErrNotFound
		}
		return nil, err
	}
	r.Evidence = ev
	r.Events = []RecEvent{}
	return &r, nil
}

func domainsOf(p *identity.Principal) []string {
	out := []string{}
	for d, perm := range DomainPermission {
		if p.Can(perm) {
			out = append(out, d)
		}
	}
	return out
}

// Recommendations lists items in the caller's domains.
func (b *Toolbox) Recommendations(ctx context.Context, p *identity.Principal, status string) ([]*Recommendation, error) {
	rows, err := b.Pool.Query(ctx, `SELECT `+recCols+` FROM intelligence_recommendations WHERE domain = ANY($1) AND ($2 = '' OR status = $2)
		ORDER BY created_at DESC LIMIT 200`, domainsOf(p), status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Recommendation{}
	for rows.Next() {
		r, err := scanRec(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Recommendation returns one item with its history, if in the caller's domains.
func (b *Toolbox) Recommendation(ctx context.Context, p *identity.Principal, id string) (*Recommendation, error) {
	if !uuidRe.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	r, err := scanRec(b.Pool.QueryRow(ctx, `SELECT `+recCols+` FROM intelligence_recommendations WHERE id = $1 AND domain = ANY($2)`, id, domainsOf(p)))
	if err != nil {
		return nil, err
	}
	rows, err := b.Pool.Query(ctx, `SELECT action, from_status, to_status, note, actor, created_at FROM intelligence_recommendation_events
		WHERE recommendation_id = $1 ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e RecEvent
		if err := rows.Scan(&e.Action, &e.FromStatus, &e.ToStatus, &e.Note, &e.Actor, &e.CreatedAt); err != nil {
			return nil, err
		}
		r.Events = append(r.Events, e)
	}
	return r, rows.Err()
}

func (b *Toolbox) insertRec(ctx context.Context, p *identity.Principal, r Recommendation) (*Recommendation, bool, error) {
	if r.Evidence == nil {
		r.Evidence = json.RawMessage(`{}`)
	}
	tx, err := b.Pool.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback(ctx)
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO intelligence_recommendations (kind, basis, domain, source_key, title, summary, evidence, review_href, run_id, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11) RETURNING id`,
		r.Kind, r.Basis, r.Domain, r.SourceKey, r.Title, r.Summary, string(r.Evidence), r.ReviewHref, r.RunID, p.UserID, b.Now()).Scan(&id)
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		// Already saved and still open: return that one.
		_ = tx.Rollback(ctx)
		var existing string
		if err := b.Pool.QueryRow(ctx, `SELECT id FROM intelligence_recommendations WHERE source_key = $1 AND status IN ('open', 'saved', 'assigned')`, r.SourceKey).Scan(&existing); err != nil {
			return nil, false, err
		}
		out, err := b.Recommendation(ctx, p, existing)
		return out, false, err
	}
	if err != nil {
		return nil, false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO intelligence_recommendation_events (recommendation_id, action, to_status, actor) VALUES ($1, 'created', 'open', $2)`, id, p.UserID); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	out, err := b.Recommendation(ctx, p, id)
	return out, true, err
}

// SaveRisk stores a triggered risk for review, from a fresh evaluation.
func (b *Toolbox) SaveRisk(ctx context.Context, p *identity.Principal, key, from, to string) (*Recommendation, bool, error) {
	risks, err := b.Risks(ctx, p, from, to)
	if err != nil {
		return nil, false, err
	}
	for _, r := range risks {
		if r.Key != key {
			continue
		}
		if r.Status != Triggered {
			return nil, false, httpx.NewError(http.StatusConflict, "risk_not_triggered", "This indicator is not triggered for the period, so there is nothing to save.")
		}
		ev, _ := json.Marshal(r)
		return b.insertRec(ctx, p, Recommendation{Kind: "risk", Basis: BasisObserved, Domain: r.Domain, SourceKey: "risk:" + key + ":" + r.From + ":" + r.To,
			Title: r.Title + " (" + r.Severity + ")", Summary: r.Explanation + " Review: " + r.ReviewAction, Evidence: ev, ReviewHref: r.ReviewHref})
	}
	return nil, false, httpx.ErrNotFound
}

// SaveOpportunity stores an observed opportunity for review.
func (b *Toolbox) SaveOpportunity(ctx context.Context, p *identity.Principal, key, from, to string) (*Recommendation, bool, error) {
	ops, err := b.Opportunities(ctx, p, from, to)
	if err != nil {
		return nil, false, err
	}
	for _, o := range ops {
		if o.Key != key {
			continue
		}
		ev, _ := json.Marshal(o)
		return b.insertRec(ctx, p, Recommendation{Kind: "opportunity", Basis: BasisObserved, Domain: o.Domain, SourceKey: "opportunity:" + key,
			Title: o.Title, Summary: o.Summary + " Review: " + o.Review, Evidence: ev, ReviewHref: o.ReviewHref})
	}
	return nil, false, httpx.ErrNotFound
}

// Propose stores a business proposal written by a person.
func (b *Toolbox) Propose(ctx context.Context, p *identity.Principal, domain, title, summary string) (*Recommendation, error) {
	title, summary = strings.TrimSpace(title), strings.TrimSpace(summary)
	perm, ok := DomainPermission[domain]
	if !ok || !p.Can(perm) {
		return nil, fieldErr("domain", "Choose a domain you have access to.")
	}
	if title == "" || len([]rune(title)) > 200 || len([]rune(summary)) > 2000 {
		return nil, fieldErr("title", "A title (at most 200 characters) and a summary (at most 2,000).")
	}
	r, _, err := b.insertRec(ctx, p, Recommendation{Kind: "proposal", Basis: BasisProposal, Domain: domain, Title: title, Summary: summary})
	return r, err
}

var recTransitions = map[string]struct {
	from []string
	to   string
}{
	"save":     {[]string{"open"}, "saved"},
	"assign":   {[]string{"open", "saved", "assigned"}, "assigned"},
	"dismiss":  {[]string{"open", "saved", "assigned"}, "dismissed"},
	"done":     {[]string{"saved", "assigned"}, "done"},
	"validate": {[]string{"open", "saved", "assigned"}, ""},
}

// ReviewRecommendation applies a review action: save, assign, dismiss,
// done, or validate (a reviewer confirms the evidence; the status stays).
func (b *Toolbox) ReviewRecommendation(ctx context.Context, p *identity.Principal, id, action, note, assignee string) (*Recommendation, error) {
	t, ok := recTransitions[action]
	if !ok {
		return nil, httpx.ErrNotFound
	}
	r, err := b.Recommendation(ctx, p, id)
	if err != nil {
		return nil, err
	}
	note = strings.TrimSpace(note)
	if len(note) > 1000 {
		return nil, fieldErr("note", "At most 1,000 characters.")
	}
	allowed := false
	for _, f := range t.from {
		allowed = allowed || f == r.Status
	}
	if !allowed {
		return nil, httpx.NewError(http.StatusConflict, "invalid_transition", "This item cannot be "+action+"d from "+r.Status+".")
	}
	to, basis := t.to, r.Basis
	if to == "" {
		to = r.Status
	}
	var assigned *string = r.AssignedTo
	switch action {
	case "dismiss", "validate":
		if note == "" {
			return nil, fieldErr("note", "Say why.")
		}
		if action == "validate" {
			if r.Basis == BasisValidated {
				return nil, httpx.NewError(http.StatusConflict, "already_validated", "Already validated.")
			}
			basis = BasisValidated
		}
	case "assign":
		var ok bool
		if err := b.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_key = ur.role_key
			WHERE ur.user_id = $1::uuid AND rp.permission_key = 'intelligence.read')`, assignee).Scan(&ok); err != nil || !ok {
			return nil, fieldErr("assignee", "Assign to a staff member who can open Mise Intelligence.")
		}
		assigned = &assignee
	}
	tx, err := b.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE intelligence_recommendations SET status = $2, basis = $3, assigned_to = $4, updated_at = $5 WHERE id = $1`,
		id, to, basis, assigned, b.Now()); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO intelligence_recommendation_events (recommendation_id, action, from_status, to_status, note, actor) VALUES ($1, $2, $3, $4, $5, $6)`,
		id, action, r.Status, to, note, p.UserID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return b.Recommendation(ctx, p, id)
}
