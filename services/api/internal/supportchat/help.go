package supportchat

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
)

// The help centre: articles written by staff, reviewed, then published.
// Only published articles are public. Bodies are plain text (paragraphs
// separated by blank lines): no HTML is accepted or rendered.

type HelpCategory struct {
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	Articles int    `json:"articles"`
}

type HelpArticleSummary struct {
	Slug        string     `json:"slug"`
	Category    string     `json:"category"`
	Title       string     `json:"title"`
	Summary     string     `json:"summary"`
	PublishedAt *time.Time `json:"published_at"`
}

type HelpArticle struct {
	HelpArticleSummary
	Body      string    `json:"body"`
	UpdatedAt time.Time `json:"updated_at"`
}

// HelpCategories lists categories with their number of published articles.
func (s *Service) HelpCategories(ctx context.Context) ([]HelpCategory, error) {
	rows, err := s.pool.Query(ctx, `SELECT c.slug, c.name, count(a.id) FROM help_categories c
		LEFT JOIN help_articles a ON a.category_slug = c.slug AND a.status = 'published' GROUP BY c.slug, c.name, c.position ORDER BY c.position, c.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HelpCategory{}
	for rows.Next() {
		var c HelpCategory
		if err := rows.Scan(&c.Slug, &c.Name, &c.Articles); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// HelpSearch lists published articles, optionally in one category and
// matching a search (ranked by relevance).
func (s *Service) HelpSearch(ctx context.Context, q, category string) ([]HelpArticleSummary, error) {
	q = strings.TrimSpace(q)
	if utf8.RuneCountInString(q) > 100 {
		q = string([]rune(q)[:100])
	}
	rows, err := s.pool.Query(ctx, `SELECT slug, category_slug, title, summary, published_at FROM help_articles
		WHERE status = 'published' AND ($1 = '' OR category_slug = $1)
		AND ($2 = '' OR search @@ websearch_to_tsquery('english', $2) OR title ILIKE '%' || $3 || '%')
		ORDER BY CASE WHEN $2 = '' THEN 0 ELSE ts_rank(search, websearch_to_tsquery('english', $2)) END DESC, title LIMIT 50`,
		category, q, likeEscape(q))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HelpArticleSummary{}
	for rows.Next() {
		var a HelpArticleSummary
		if err := rows.Scan(&a.Slug, &a.Category, &a.Title, &a.Summary, &a.PublishedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// HelpArticleBySlug is one published article.
func (s *Service) HelpArticleBySlug(ctx context.Context, slug string) (*HelpArticle, error) {
	if !slugPattern.MatchString(slug) {
		return nil, httpx.ErrNotFound
	}
	var a HelpArticle
	err := s.pool.QueryRow(ctx, `SELECT slug, category_slug, title, summary, published_at, body, updated_at FROM help_articles
		WHERE slug = $1 AND status = 'published'`, slug).Scan(&a.Slug, &a.Category, &a.Title, &a.Summary, &a.PublishedAt, &a.Body, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	return &a, err
}

// ---------------------------------------------------------------- editing

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// StaffArticle is an article in any state, for the editor.
type StaffArticle struct {
	ID          string         `json:"id"`
	Slug        string         `json:"slug"`
	Category    string         `json:"category"`
	Title       string         `json:"title"`
	Summary     string         `json:"summary"`
	Body        string         `json:"body"`
	Status      string         `json:"status"`
	Version     int            `json:"version"`
	PublishedAt *time.Time     `json:"published_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	History     []ArticleEvent `json:"history"`
}

type ArticleEvent struct {
	Action    string    `json:"action"`
	Version   int       `json:"version"`
	Note      string    `json:"note"`
	ActorName string    `json:"actor_name"`
	CreatedAt time.Time `json:"created_at"`
}

type ArticleInput struct {
	Slug     *string `json:"slug"`
	Category *string `json:"category"`
	Title    *string `json:"title"`
	Summary  *string `json:"summary"`
	Body     *string `json:"body"`
	Note     string  `json:"note"`
}

const staffArticleCols = `id, slug, category_slug, title, summary, body, status, version, published_at, updated_at`

func scanStaffArticle(row pgx.Row) (*StaffArticle, error) {
	var a StaffArticle
	err := row.Scan(&a.ID, &a.Slug, &a.Category, &a.Title, &a.Summary, &a.Body, &a.Status, &a.Version, &a.PublishedAt, &a.UpdatedAt)
	a.History = []ArticleEvent{}
	return &a, err
}

func (s *Service) StaffArticles(ctx context.Context, status string) ([]StaffArticle, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+staffArticleCols+` FROM help_articles WHERE $1 = '' OR status = $1 ORDER BY updated_at DESC LIMIT 500`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StaffArticle{}
	for rows.Next() {
		a, err := scanStaffArticle(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (s *Service) StaffArticle(ctx context.Context, id string) (*StaffArticle, error) {
	if !validUUID(id) {
		return nil, httpx.ErrNotFound
	}
	a, err := scanStaffArticle(s.pool.QueryRow(ctx, `SELECT `+staffArticleCols+` FROM help_articles WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT e.action, e.version, e.note, coalesce(nullif(trim(u.first_name || ' ' || u.last_name), ''), u.email, ''), e.created_at
		FROM help_article_events e LEFT JOIN users u ON u.id = e.actor_id WHERE e.article_id = $1 ORDER BY e.id DESC LIMIT 50`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e ArticleEvent
		if err := rows.Scan(&e.Action, &e.Version, &e.Note, &e.ActorName, &e.CreatedAt); err != nil {
			return nil, err
		}
		a.History = append(a.History, e)
	}
	return a, rows.Err()
}

func validateArticle(a *StaffArticle) error {
	var f []httpx.FieldError
	bad := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	if !slugPattern.MatchString(a.Slug) || len(a.Slug) > 100 {
		bad("slug", "Use lowercase words joined by hyphens (up to 100 characters).")
	}
	if n := utf8.RuneCountInString(a.Title); n < 3 || n > 160 {
		bad("title", "Give the article a title (3 to 160 characters).")
	}
	if utf8.RuneCountInString(a.Summary) > 300 {
		bad("summary", "Keep the summary under 300 characters.")
	}
	if utf8.RuneCountInString(a.Body) > 20000 {
		bad("body", "Keep the article under 20,000 characters.")
	}
	if strings.ContainsAny(a.Body+a.Title+a.Summary, "<>") {
		bad("body", "Use plain text: angle brackets are not allowed.")
	}
	if len(f) > 0 {
		return httpx.ValidationError(f)
	}
	return nil
}

func articleEvent(ctx context.Context, q db.Querier, id, action string, version int, note, actor string) error {
	_, err := q.Exec(ctx, `INSERT INTO help_article_events (article_id, action, version, note, actor_id) VALUES ($1, $2, $3, $4, $5)`,
		id, action, version, strings.TrimSpace(note), actor)
	return err
}

func articleConflict(err error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		switch pe.Code {
		case "23505":
			return httpx.ValidationError([]httpx.FieldError{{Field: "slug", Code: "taken", Message: "Another article uses this address."}})
		case "23503":
			return httpx.ValidationError([]httpx.FieldError{{Field: "category", Code: "invalid", Message: "Choose a category."}})
		}
	}
	return err
}

func (s *Service) CreateArticle(ctx context.Context, p *identity.Principal, in ArticleInput) (*StaffArticle, error) {
	a := &StaffArticle{}
	for _, v := range []struct {
		dst *string
		src *string
	}{{&a.Slug, in.Slug}, {&a.Category, in.Category}, {&a.Title, in.Title}, {&a.Summary, in.Summary}, {&a.Body, in.Body}} {
		if v.src != nil {
			*v.dst = strings.TrimSpace(*v.src)
		}
	}
	if err := validateArticle(a); err != nil {
		return nil, err
	}
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO help_articles (slug, category_slug, title, summary, body, created_by, updated_by)
			VALUES ($1, $2, $3, $4, $5, $6, $6) RETURNING id`, a.Slug, a.Category, a.Title, a.Summary, a.Body, p.UserID).Scan(&a.ID); err != nil {
			return articleConflict(err)
		}
		return articleEvent(ctx, tx, a.ID, "created", 1, in.Note, p.UserID)
	})
	if err != nil {
		return nil, err
	}
	return s.StaffArticle(ctx, a.ID)
}

// UpdateArticle edits a draft. A published or archived article is first
// returned to draft with Transition("revise"), which takes it off the site.
func (s *Service) UpdateArticle(ctx context.Context, p *identity.Principal, id string, in ArticleInput) (*StaffArticle, error) {
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if !validUUID(id) {
			return httpx.ErrNotFound
		}
		a, err := scanStaffArticle(tx.QueryRow(ctx, `SELECT `+staffArticleCols+` FROM help_articles WHERE id = $1 FOR UPDATE`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		if a.Status != "draft" {
			return httpx.NewError(http.StatusConflict, "not_a_draft", "Only drafts can be edited. Return the article to draft first.")
		}
		for _, v := range []struct {
			dst *string
			src *string
		}{{&a.Slug, in.Slug}, {&a.Category, in.Category}, {&a.Title, in.Title}, {&a.Summary, in.Summary}, {&a.Body, in.Body}} {
			if v.src != nil {
				*v.dst = strings.TrimSpace(*v.src)
			}
		}
		if err := validateArticle(a); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE help_articles SET slug = $2, category_slug = $3, title = $4, summary = $5, body = $6,
			updated_by = $7, updated_at = now() WHERE id = $1`, id, a.Slug, a.Category, a.Title, a.Summary, a.Body, p.UserID); err != nil {
			return articleConflict(err)
		}
		return articleEvent(ctx, tx, id, "edited", a.Version, in.Note, p.UserID)
	})
	if err != nil {
		return nil, err
	}
	return s.StaffArticle(ctx, id)
}

// Transitions: submit (draft → in_review), return (in_review → draft),
// publish (in_review → published), archive (published → archived), revise
// (published or archived → draft, a new version). Publishing needs
// help_centre.publish; the route checks it.
var transitions = map[string]struct {
	from    []string
	to      string
	action  string
	publish bool
}{
	"submit":  {[]string{"draft"}, "in_review", "submitted", false},
	"return":  {[]string{"in_review"}, "draft", "returned", true},
	"publish": {[]string{"in_review"}, "published", "published", true},
	"archive": {[]string{"published"}, "archived", "archived", true},
	"revise":  {[]string{"published", "archived"}, "draft", "edited", false},
}

// NeedsPublish reports whether a transition needs help_centre.publish.
func NeedsPublish(action string) bool { return transitions[action].publish }

func (s *Service) TransitionArticle(ctx context.Context, p *identity.Principal, id, action, note string) (*StaffArticle, error) {
	t, ok := transitions[action]
	if !ok {
		return nil, httpx.ErrNotFound
	}
	if t.publish && !p.Can(identity.PermHelpCentrePublish) {
		return nil, httpx.ErrForbidden
	}
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if !validUUID(id) {
			return httpx.ErrNotFound
		}
		a, err := scanStaffArticle(tx.QueryRow(ctx, `SELECT `+staffArticleCols+` FROM help_articles WHERE id = $1 FOR UPDATE`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		allowed := false
		for _, f := range t.from {
			allowed = allowed || f == a.Status
		}
		if !allowed {
			return httpx.NewError(http.StatusConflict, "invalid_transition", "This article is "+strings.ReplaceAll(a.Status, "_", " ")+": that step does not apply.")
		}
		if action == "publish" && strings.TrimSpace(a.Body) == "" {
			return httpx.NewError(http.StatusConflict, "empty_article", "Write the article before publishing it.")
		}
		version := a.Version
		set := `status = $2, updated_by = $3, updated_at = now()`
		switch action {
		case "submit":
			set += `, submitted_by = $3`
		case "publish":
			set += `, published_by = $3, published_at = now()`
		case "revise":
			version++
			set += `, version = version + 1, published_at = NULL`
		}
		if _, err := tx.Exec(ctx, `UPDATE help_articles SET `+set+` WHERE id = $1`, id, t.to, p.UserID); err != nil {
			return err
		}
		if err := articleEvent(ctx, tx, id, t.action, version, note, p.UserID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: "help_article." + action, TargetType: "help_article", TargetID: id,
			Metadata: map[string]any{"from": a.Status, "to": t.to, "version": version}})
	})
	if err != nil {
		return nil, err
	}
	return s.StaffArticle(ctx, id)
}
